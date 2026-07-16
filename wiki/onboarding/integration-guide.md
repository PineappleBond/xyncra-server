# 集成指南

> 本文档面向需要将 Xyncra 集成到现有系统中的开发者和架构师。
> 涵盖架构概览、协议集成、最佳实践和常见场景。

---

## 目录

- [架构总览](#架构总览)
- [部署架构](#部署架构)
- [协议集成步骤](#协议集成步骤)
- [客户端 SDK](#客户端-sdk)
- [WebSocket 连接管理](#websocket-连接管理)
- [错误处理策略](#错误处理策略)
- [数据同步策略](#数据同步策略)
- [Agent 集成](#agent-集成)
- [安全考虑](#安全考虑)
- [性能与扩展](#性能与扩展)
- [常见集成场景](#常见集成场景)

---

## 架构总览

```
                          ┌──────────────────────────────────┐
                          │      业务应用服务器                │
                          │  ┌────────────┐ ┌──────────────┐ │
                          │  │ 认证/授权  │ │ 业务逻辑     │ │
                          │  └──────┬─────┘ └──────┬───────┘ │
                          └─────────┼──────────────┼─────────┘
                                    │              │
                                    │  注入        │  查询
                                    │  user_id     │  REST/GraphQL
                          ┌─────────▼──────────────▼─────────┐
                          │           Xyncra Server            │
                          │                                    │
  ┌──────────────┐        │  ┌──────────┐  ┌───────────────┐  │
  │   Web 应用   │───────▶│  │  Conn    │  │  Agent        │  │
  │  (WASM SDK)  │        │  │  Store   │  │  Runtime      │  │
  └──────────────┘        │  └────┬─────┘  └───────┬───────┘  │
                          │       │                 │          │
  ┌──────────────┐        │  ┌────▼─────┐  ┌───────▼───────┐  │  ┌───────┐
  │  移动 App    │───────▶│  │ Handler  │  │  Tool         │──│─▶│ Redis │
  │  (WebSocket) │        │  │ Registry │  │  Provider     │  │  └───────┘
  └──────────────┘        │  └────┬─────┘  └───────────────┘  │
                          │       │                            │  ┌────────┐
  ┌──────────────┐        │  ┌────▼────────────────────────┐   │  │  MQ    │
  │  CLI Client  │───────▶│  │       Store Layer            │───│─▶│(Asynq)│
  │  (Go SDK)    │        │  │  (SQLite/PostgreSQL/MySQL)   │   │  └────────┘
  └──────────────┘        │  └───────────────────────────────┘   │
                          └──────────────────────────────────────┘
```

### 核心组件

| 组件 | 说明 | 关键技术 |
|------|------|----------|
| **WebSocket Server** | 连接管理、消息路由 | gorilla/websocket |
| **Connection Store** | 连接跟踪、设备管理 | Redis / 内存 |
| **Handler Registry** | RPC 方法分发 | 注册表模式 |
| **Agent Runtime** | AI Agent 执行引擎 | Eino 框架 |
| **Tool Provider** | 工具注册与调用 | MCP / ReverseRPC |
| **Store Layer** | 数据持久化 | GORM（SQLite/PG/MySQL）|
| **Message Queue** | 异步任务处理 | Asynq（Redis）|
| **Node Broadcaster** | 跨节点推送 | Redis Pub/Sub |

### 数据流

```
用户发送消息的数据流：

1. Client → WebSocket: send_message(conversation_id, content, client_message_id)
2. Server → Store: INSERT message (persist)
3. Server → MQ: ENQUEUE push task (async, fire-and-forget)
4. Server → WebSocket: Response(code=0, data={message, duplicate})
5. MQ → Server: DEQUEUE push task
6. Server → Redis Pub/Sub: BROADCAST update
7. Server → WebSocket: Push(PackageTypeUpdates, seq=N, type=message)
8. Recipient → Handler: OnMessage(message)
```

---

## 部署架构

### 单节点架构

```
┌──────────┐     ┌───────────┐     ┌──────────┐
│  Clients  │────▶│  Xyncra   │────▶│  Redis   │
│ (WebSocket)│    │  Server   │    │          │
└──────────┘     │           │     ├──────────┤
                 │  SQLite   │     │  Local   │
                 │  or PG    │     └──────────┘
                 └───────────┘
```

适合：开发环境、小团队（< 100 并发用户）

### 多节点集群

```
                  ┌────────────────────────────┐
                  │        负载均衡器           │
                  │   (Nginx/Envoy/HAProxy)    │
                  │   TLS termination + 认证   │
                  └────────┬────────┬──────────┘
                           │        │
              ┌────────────▼──┐ ┌──▼────────────┐
              │  Xyncra Node1 │ │  Xyncra Node2 │
              │  ┌──────────┐ │ │  ┌──────────┐ │
              │  │ Conn     │ │ │  │ Conn     │ │
              │  │ Store    │ │ │  │ Store    │ │
              │  └────┬─────┘ │ │  └────┬─────┘ │
              │       │       │ │       │       │
              └───────┼───────┘ └───────┼───────┘
                      │                 │
              ┌───────▼─────────────────▼───────┐
              │           Redis 集群              │
              │  ┌──────────┐ ┌──────────┐       │
              │  │  Conn    │ │  Pub/Sub │       │
              │  │  Store   │ │  + MQ    │       │
              │  └──────────┘ └──────────┘       │
              └──────────────────────────────────┘

              ┌──────────────────────────────────┐
              │       PostgreSQL 主从             │
              └──────────────────────────────────┘
```

适合：生产环境、大规模部署（1000+ 并发用户）

### 部署重要说明

**Xyncra 不处理以下事项**（由反向代理层负责）：

| 功能 | 推荐方案 |
|------|----------|
| TLS 终止 | Nginx/Envoy/Caddy 处理 HTTPS/WSS |
| 用户认证 | 业务服务器认证后注入 `user_id` |
| 权限控制 | 业务层验证后转发给 Xyncra |
| 速率限制 | 反向代理层配置 |
| CORS | 反向代理层配置 |
| CSRF 防护 | 内部部署模型下无需 |

---

## 协议集成步骤

要将 Xyncra 协议集成到自定义客户端（Web 前端、移动 App、桌面应用），
按以下步骤操作：

### 第 1 步：建立 WebSocket 连接

```javascript
// JavaScript 示例
const ws = new WebSocket(
  'ws://server:8080/ws?user_id=alice&device_id=chrome-browser'
);

ws.onopen = () => {
  console.log('Connected to Xyncra');
  // 注册设备函数
  sendRequest('system.register_functions', {
    functions: [
      {
        name: 'custom_function',
        description: 'A custom client function',
        parameters: { type: 'object', properties: {} }
      }
    ]
  });
};
```

### 第 2 步：发送 RPC 请求

```javascript
function sendRequest(method, params) {
  const id = generateUUID();
  const request = {
    version: 1,
    type: 0,  // Request
    data: {
      id: id,
      method: method,
      params: params
    }
  };
  ws.send(JSON.stringify(request));
  return id;  // 用于关联响应
}
```

### 第 3 步：处理响应

```javascript
ws.onmessage = (event) => {
  const pkg = JSON.parse(event.data);

  switch (pkg.type) {
    case 1:  // Response
      const response = pkg.data;
      const requestId = response.id;
      if (response.code === 0) {
        // 成功处理响应
        console.log('Success:', response.data);
      } else {
        // 错误处理
        console.error('Error:', response.code, response.msg);
      }
      break;

    case 2:  // Updates
      const updates = pkg.data.updates;
      for (const update of updates) {
        switch (update.type) {
          case 'message':
            handleNewMessage(update.payload);
            break;
          case 'typing':
            handleTypingIndicator(update.payload);
            break;
          // ... 其他更新类型
        }
      }
      break;
  }
};
```

### 第 4 步：心跳维持

```javascript
// 每 30 秒发送一次心跳
setInterval(() => {
  sendRequest('heartbeat', {});
}, 30000);
```

### 第 5 步：数据同步

```javascript
// 连接后或重连后进行完整同步
function syncUpdates(afterSeq) {
  sendRequest('sync_updates', {
    after_seq: afterSeq,
    limit: 100
  });
  // 响应中包含 has_more 字段，如果为 true 则继续翻页
}
```

---

## 客户端 SDK

### Go SDK（官方）

`pkg/client` 包提供了完整的 Go 客户端 SDK：

```go
import "github.com/PineappleBond/xyncra-server/pkg/client"

// 创建客户端
xc, err := client.New(
    client.WithServerURL("ws://localhost:8080/ws"),
    client.WithUserID("alice"),
    client.WithDeviceID("my-app"),
    client.WithDB(db),
    client.WithUpdateHandler(myHandler),
)

// 启动
go xc.Start(ctx)

// 发送消息
result, err := xc.SendMessage(ctx, "conv-id", "Hello!", "", 0)

// 停止
xc.Stop()
```

### 其他语言

Xyncra 的 WebSocket 协议基于标准 JSON，任何语言的 WebSocket 库都可以集成：

| 语言 | 推荐 WebSocket 库 | 说明 |
|------|------------------|------|
| JavaScript/TypeScript | 原生 WebSocket API | 浏览器直接支持 |
| Python | `websockets` | pip install websockets |
| Java | `Java-WebSocket` | Maven: org.java-websocket |
| Swift | `URLSessionWebSocketTask` | iOS 原生 |
| Kotlin | `OkHttp WebSocket` | Android 常用 |
| Dart/Flutter | `web_socket_channel` | Flutter 项目 |

### 最小集成示例（Python）

```python
import json
import asyncio
import websockets

async def xyncra_client():
    uri = "ws://localhost:8080/ws?user_id=alice&device_id=python-client"
    async with websockets.connect(uri) as ws:
        # 创建会话
        req_id = "req-1"
        await ws.send(json.dumps({
            "version": 1, "type": 0,
            "data": {
                "id": req_id,
                "method": "create_conversation",
                "params": {"user_id": "bob"}
            }
        }))

        # 等响应
        resp = json.loads(await ws.recv())
        conv_id = resp["data"]["data"]["conversation"]["ID"]
        print(f"Conversation: {conv_id}")

        # 发送消息
        await ws.send(json.dumps({
            "version": 1, "type": 0,
            "data": {
                "id": "req-2",
                "method": "send_message",
                "params": {
                    "conversation_id": conv_id,
                    "content": "Hello from Python!",
                    "client_message_id": "py-001"
                }
            }
        }))

        resp = json.loads(await ws.recv())
        print(f"Message sent: {resp['data']['msg']}")

asyncio.run(xyncra_client())
```

---

## WebSocket 连接管理

### 重连策略

```
连接断开
    │
    ├── 4001 Close Frame（设备被替换）→ 优雅退出，不重连
    │
    └── 其他原因断开
         │
         ┌─▼──────────────┐
         │  延迟: baseDelay│   (指数退避: base × 2^(attempt-1))
         │  (默认 1s)     │
         └─┬──────────────┘
           │
         ┌─▼──────────────┐
         │  尝试重连       │── 成功 → 重置计数器 → 恢复通信
         └─┬──────────────┘
           │ 失败
           │
         ┌─▼──────────────┐
         │  延迟加倍       │   (上限 maxDelay = 30s, 含 ±25% 随机抖动)
         └─┬──────────────┘
           │
           └────→ 继续尝试（无限重试，D-044）
```

实现要点：

```javascript
// 重连实现建议
class XyncraConnection {
  constructor(url) {
    this.url = url;
    this.baseDelay = 1000;  // 1s
    this.maxDelay = 30000;  // 30s
    this.attempt = 0;
  }

  async connectWithRetry() {
    while (true) {
      try {
        await this.connect();
        this.attempt = 0;  // 重置
        return;
      } catch (err) {
        if (err.code === 4001) {
          // 设备被替换，不重连
          console.log('Replaced by newer device');
          return;
        }
        this.attempt++;
        const delay = this.backoffDelay();
        await this.sleep(delay);
      }
    }
  }

  backoffDelay() {
    const exp = Math.min(this.attempt - 1, 30);
    let delay = this.baseDelay * Math.pow(2, exp);
    delay = Math.min(delay, this.maxDelay);
    // ±25% 随机抖动
    const jitter = (Math.random() - 0.5) * delay * 0.5;
    return delay + jitter;
  }
}
```

### 重连握手

每次重连后需执行：

1. **`system.reconnect`** — 通知服务器本设备重连，触发 Pending request 补发
2. **`system.register_functions`** — 重新注册设备函数
3. **`sync_updates`** — 拉取离线期间的增量更新

```javascript
async function reconnectHandshake(lastSeq) {
  // Step 1: Reconnect handshake
  await call('system.reconnect', { last_seen_seq: lastSeq });

  // Step 2: Re-register functions
  await call('system.register_functions', { functions: [...] });

  // Step 3: Full sync
  let afterSeq = 0;
  while (true) {
    const result = await call('sync_updates', {
      after_seq: afterSeq,
      limit: 100
    });
    // 处理 result.updates
    if (!result.has_more) break;
    afterSeq = result.updates[result.updates.length - 1].seq;
  }
}
```

### 连接状态管理

推荐的状态机：

```
         ┌──────────┐
         │  DISCONN │
         └────┬─────┘
              │ connect
         ┌────▼─────┐
         │ CONNECT  │── 连接失败 → DISCONN（重试）
         └────┬─────┘
              │ 注册函数 + 全量同步
         ┌────▼─────┐
         │  SYNC    │
         └────┬─────┘
              │ 同步完成
         ┌────▼─────┐
         │ READY    │── 实时接收推送
         └────┬─────┘
              │ 断开连接
         ┌────▼─────┐
         │ RECONN   │── 指数退避重试
         └────┬─────┘
              │ 重连成功 → CONNECT（重走流程）
```

---

## 错误处理策略

### 错误码处理矩阵

| code | 含义 | 客户端行为 |
|------|------|-----------|
| 0 | OK | 正常处理 |
| -100 | 参数错误 | 修复参数后重试，不自动重试 |
| -101 | 资源不存在 | 检查资源 ID，不自动重试 |
| -200 | 权限不足 | 提示用户，不自动重试 |
| -300 | 服务器内部错误 | 指数退避重试（最多 5 次） |
| -409 | 资源冲突 | 提示用户（如 HITL 问题已被其他设备回答）|

### 一般重试策略

```go
// Go SDK 中的重试实现（pkg/client/retry.go）
// 使用指数退避 + jitter
func retryWithBackoff(operation func() error) error {
    var lastErr error
    for attempt := 1; attempt <= maxRetries; attempt++ {
        if err := operation(); err != nil {
            if !isRetryable(err) {
                return err  // 不可重试的错误直接返回
            }
            lastErr = err
            delay := calculateBackoff(attempt)
            time.Sleep(delay)
            continue
        }
        return nil  // 成功
    }
    return lastErr
}

func isRetryable(err error) bool {
    // 可重试：超时、连接错误、服务器内部错误
    // 不可重试：参数错误、权限不足、资源不存在
    var ce *client.ClientError
    if errors.As(err, &ce) {
        return ce.Code >= -300  // -300 及以上的错误可重试
    }
    return false
}
```

### 幂等性

消息发送使用 `client_message_id` 实现幂等：
- 客户端每次发送消息前生成一个 UUID v4
- 服务端使用这个 `client_message_id` 去重
- 相同的 `client_message_id` 返回已持久化的消息（`duplicate=true`），不会创建重复消息

重试场景建议：
- 发送消息超时 → 用相同 `client_message_id` 重试 → 幂等安全
- 创建会话超时 → find-or-create 模式 → 天然幂等（D-011）
- 标记已读超时 → MAX 语义 → 幂等安全（D-012）

---

## 数据同步策略

### 双模式同步

| 模式 | 说明 | 触发时机 |
|------|------|----------|
| 实时推送 | 服务端主动推送增量更新 | 有新数据时立即推送 |
| 拉取同步 | 客户端主动拉取缺失更新 | 启动、重连、手动触发 |

### Seq 连续性保证

```
客户端本地 seq 状态：
  localMaxSeq: 100  （已处理的最高 seq）

断线期间服务端产生了 seq 101-150
重连后调用 sync_updates(after_seq=100)：
  → 返回 seq 101-150 的更新

如果服务端产生了 seq 101-105、107-150（缺少 106）：
  → seq 106 类型为 "gap"（补空占位）
  → 客户端递增 localMaxSeq 但跳过内容处理
  → 确保客户端不会因为 seq 跳跃而静默丢失数据
```

### 页面拉取

```javascript
async function syncAll() {
  let afterSeq = localMaxSeq;
  while (true) {
    const result = await call('sync_updates', {
      after_seq: afterSeq,
      limit: 100
    });
    for (const update of result.updates) {
      processUpdate(update);
    }
    if (!result.has_more) break;
    afterSeq = result.updates[result.updates.length - 1].seq;
  }
  // 更新 localMaxSeq = result.latest_seq
}
```

---

## Agent 集成

### Agent 调用模式

```
用户发送消息到 Agent 会话：

1. send_message(conv_id=AGENT_CONV, content="北京的天气")
2. 服务端检测到会话目标为 Agent 用户
3. 服务端将消息入队 MQ（Type: agent_process）
4. MQ Worker 获取消息：
   a. 从 Agent Registry 查找 agent_id
   b. 构建 LLM 调用上下文
   c. 调用 LLM（流式）
   d. 通过 stream_text push 流式输出
   e. 通过 send_message 持久化最终消息
5. 消息接收方收到更新推送
```

### 客户端工具（ReverseRPC）

Agent 可以调用客户端设备的注册函数：

```
Agent 想要调用客户端的 get_device_info：

1. Agent 框架发起 ReverseRPC 请求
2. 服务端通过 ConnectionStore 查找设备连接
3. 服务端发送 PackageTypeRequest 到设备
4. 设备执行函数，返回结果
5. 服务端将结果提供给 Agent 框架
```

要支持客户端工具，需要在 `system.register_functions` 中注册函数能力，
Agent 配置中需要 `enable_client_tools: true`。

### MCP 工具集成

Agent 可以通过 MCP（Model Context Protocol）集成外部工具服务器：

```go
// 服务端启动时建立 MCP 连接
mcpBridge := agenttools.NewMCPBridge(nil)
agentBuilder.SetMCPBridge(mcpBridge)

// Agent 配置文件中引用 MCP 工具
// tools:
//   - mcp:sql-database
//   - mcp:file-system
```

---

## 安全考虑

### 部署安全清单

| 安全项目 | 建议 |
|---------|------|
| 网络隔离 | Xyncra Server 部署在内部网络，不直接暴露到公网 |
| 认证 | 反向代理层完成认证，注入已认证的 user_id |
| TLS | 反向代理层处理 WSS，内部通信可走 ws |
| Redis 密码 | 生产环境设置 `XYNCRA_REDIS_PASSWORD` |
| 数据库密码 | PostgreSQL/MySQL 使用强密码，限制网络访问 |
| 速率限制 | 反向代理层限制每秒连接数 |
| 连接上限 | 设置 `XYNCRA_MAX_CONNS_PER_USER` 防止连接滥用 |

### 禁止事项

- 不要在 Agent 配置中直接写入 API Key，使用 `api_key_env` 引用环境变量
- 不要将 Xyncra Server 直接暴露到公网（不内置认证）
- 不要在客户端代码中硬编码用户凭证

---

## 性能与扩展

### 水平扩展

```text
节点数 = 预期并发连接 / 单节点容量

单节点参考容量（估算）：
- 10,000 同时 WebSocket 连接
- 1,000 msg/s 发送吞吐
- Redis Pub/Sub 延迟 < 10ms

扩展瓶颈：
1. Redis — Pub/Sub 广播随节点数线性增长
2. 数据库 — 消息写入负载
3. Agent — LLM 调用的并发限制
```

### 关键配置优化

| 配置 | 推荐值 | 说明 |
|------|--------|------|
| `maxConcurrent`（Agent） | 10-50 | 限制并行 LLM 调用数 |
| 连接 TTL（ConnectionStore） | 30min | 心跳续期，30 秒发送一次 |
| sync_updates limit | 100 | 单次拉取最大条数 |
| 客户端重连 baseDelay | 1s | 首次重连等待时间 |
| 客户端重连 maxDelay | 30s | 最大重连等待时间 |

---

## 常见集成场景

### 场景 1：Web 前端即时通讯

```javascript
// 推荐使用原生 WebSocket API
class ChatClient {
  constructor(userId, deviceId, serverUrl) {
    this.userId = userId;
    this.deviceId = deviceId;
    this.serverUrl = serverUrl;
    this.seq = 0;
    this.messageCallbacks = new Map();
    this.pendingReqs = new Map();
  }

  connect() {
    const url = `${this.serverUrl}/ws?user_id=${this.userId}&device_id=${this.deviceId}`;
    this.ws = new WebSocket(url);

    this.ws.onopen = () => {
      this.sendRequest('system.register_functions', { functions: [] });
      this.syncAfterReconnect();
      this.startHeartbeat();
    };

    this.ws.onmessage = (event) => {
      this.handlePackage(JSON.parse(event.data));
    };

    this.ws.onclose = (event) => {
      if (event.code === 4001) return; // 被替换，不重连
      this.reconnect();
    };
  }

  sendMessage(convId, content) {
    const clientMsgId = crypto.randomUUID();
    return this.sendRequest('send_message', {
      conversation_id: convId,
      content: content,
      client_message_id: clientMsgId
    });
  }
}
```

### 场景 2：移动 App 集成

```dart
// Flutter 示例
import 'package:web_socket_channel/web_socket_channel.dart';

class XyncraMobileClient {
  WebSocketChannel? channel;

  void connect(String userId, String deviceId) {
    final uri = Uri.parse('ws://server:8080/ws')
        .replace(queryParameters: {
      'user_id': userId,
      'device_id': deviceId,
    });

    channel = WebSocketChannel.connect(uri);

    channel!.stream.listen(
      (data) => handleMessage(jsonDecode(data)),
      onDone: () => handleDisconnect(),
    );

    // 注册设备函数（如相机、GPS 等原生能力）
    registerDeviceFunctions();
  }

  void registerDeviceFunctions() {
    send('system.register_functions', {
      'functions': [
        {
          'name': 'take_photo',
          'description': 'Take a photo using device camera',
          'parameters': { 'type': 'object' }
        }
      ]
    });
  }
}
```

### 场景 3：服务端到服务端集成

```go
// Go 服务端使用 pkg/client 库
type BotService struct {
    client *client.XyncraClient
}

func (s *BotService) Start() error {
    xc, err := client.New(
        client.WithServerURL("ws://xyncra:8080/ws"),
        client.WithUserID("bot-service"),
        client.WithDeviceID("worker-1"),
        client.WithDB(db),
        client.WithUpdateHandler(s.updateHandler),
    )
    if err != nil {
        return err
    }
    s.client = xc
    return xc.Start(context.Background())
}

// 监听消息并自动回复
func (s *BotService) updateHandler() client.UpdateHandler {
    return &myHandler{
        onMessage: func(ctx context.Context, msg *model.Message) error {
            if strings.HasPrefix(msg.Content, "!ping") {
                _, err := s.client.SendMessage(ctx, msg.ConversationID, "pong!", "", 0)
                return err
            }
            return nil
        },
    }
}
```

### 场景 4：与现有认证系统集成

```nginx
# Nginx 配置示例
server {
    listen 443 ssl;
    server_name chat.example.com;

    location /ws {
        # 验证 JWT token
        auth_request /auth;

        # 将认证后的 user_id 注入请求头
        auth_request_set $user_id $upstream_http_x_user_id;

        # 转发到 Xyncra（内部网络，无 TLS）
        proxy_pass http://10.0.0.10:8080/ws?user_id=$user_id;

        # WebSocket 必须
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
    }

    location /auth {
        internal;
        proxy_pass http://auth-service:9000/verify;
        proxy_pass_request_body off;
        proxy_set_header Content-Length "";
        proxy_set_header X-Original-URI $request_uri;
    }
}
```
