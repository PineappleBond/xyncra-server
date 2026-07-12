# 实现计划: Client Function Agent Tools via WebSocket ReverseRPC

> 基于 [DESIGN_CLIENT_FUNCTION_AGENT_TOOLS.md](./DESIGN_CLIENT_FUNCTION_AGENT_TOOLS.md)，拆分为 8 个 Phase，每个 Phase 有明确验收标准。

## 依赖关系图

```text
Phase 1 (设备连接模型 + reqID UUID)
  ├── Phase 2 (函数清单协议 + ClientFunctionRegistry)
  │     └── Phase 6 (DynamicToolProvider 中间件)
  │           └── Phase 7 (Agent YAML 配置 + 端到端集成)
  ├── Phase 3 (Send 反馈 + 断连 Fail)
  ├── Phase 4 (幂等键 + Redis 持久化)
  │     └── Phase 5 (重连握手 + 请求补发)
  └── Phase 8 (客户端侧增强 + 自适应超时)
```

**关键路径**: Phase 1 → Phase 2 → Phase 6 → Phase 7

---

## Phase 1: 设备连接模型基础 + reqID UUID

**对应设计**: [5.1 连接模型变更](./DESIGN_CLIENT_FUNCTION_AGENT_TOOLS.md#51-连接模型变更-userid-deviceid), [5.8 reqID UUID](./DESIGN_CLIENT_FUNCTION_AGENT_TOOLS.md#58-reqid-原子计数器--uuid)

**目标**: 将连接管理从 `(userID, connID)` 扩展为 `(userID, deviceID, connID)`，实现定向发送；同时将 reqID 改为 UUID 避免重启冲突。

### 任务

1. **协议层: 连接 URL 新增 `device_id` 参数**
   - `pkg/client/connection.go`: `Connect()` URL 参数新增 `device_id`
   - 客户端需要生成或接受 `deviceID`（配置项）

2. **服务端: 新增 `clientsByDevice` 索引**
   - `internal/server/websocket_server.go`: 新增 `clientsByDevice map[string]map[string]*Client` (userID → deviceID → Client)
   - 连接建立时同时注册到 `clientsByUser` 和 `clientsByDevice`
   - 断开时同时清理两个索引

3. **设备替换逻辑**
   - 同 `(userID, deviceID)` 新连接到来时，向旧连接发送 Close Frame (reason: "replaced")
   - 旧连接立即清理，旧 pending 请求 fail

4. **新增 `sendToDevice()` 方法**
   - `sendToDevice(userID, deviceID, pkg)` — 定向发送到指定设备
   - 返回 `error`（设备不在线时返回 `ErrDeviceOffline`）
   - 保留 `sendToUser()` 用于广播场景（向后兼容）

5. **reqID: 原子计数器 → UUID**
   - `internal/server/reverse_rpc.go`: 移除 `nextReqID uint64` 和 `sync/atomic`
   - `reqID := uuid.New().String()`
   - 移除 `CancelAll()` 中依赖数字 ID 的逻辑（如有），改为遍历

6. **ReverseRPC 感知 deviceID**
   - `ServerRequest()` 签名新增 `deviceID` 参数: `ServerRequest(ctx, userID, deviceID, method, params, timeout)`
   - `sendFunc` 从 `sendToUser` 切换为 `sendToDevice`
   - 更新 `websocket_handler.go` 中 `DefaultMessageHandler` 的路由逻辑

### 验收标准

- [ ] 客户端连接 URL 包含 `device_id` 参数
- [ ] 服务端可通过 `(userID, deviceID)` 定位到具体连接
- [ ] `sendToDevice()` 发送到正确设备，其他设备不收到消息
- [ ] 同 `(userID, deviceID)` 新连接替换旧连接，旧连接收到 Close Frame
- [ ] 旧连接的 pending 请求立即返回错误（不等超时）
- [ ] reqID 为 UUID 格式（如 `550e8400-e29b-41d4-a716-446655440000`）
- [ ] 所有现有 ReverseRPC 测试通过（更新签名后）
- [ ] 新增测试: 设备替换、sendToDevice 定向发送、sendToDevice 设备不在线

---

## Phase 2: 函数清单协议 + ClientFunctionRegistry

**对应设计**: [6.1 函数清单协议](./DESIGN_CLIENT_FUNCTION_AGENT_TOOLS.md#61-函数清单协议-function-manifest), [6.2 ClientFunctionRegistry](./DESIGN_CLIENT_FUNCTION_AGENT_TOOLS.md#62-clientfunctionregistry-按-useriddeviceid-索引), [7.2 Function Manifest](./DESIGN_CLIENT_FUNCTION_AGENT_TOOLS.md#72-function-manifest)

**依赖**: Phase 1（设备连接模型）

**目标**: 客户端可声明自身函数能力，服务端按 `(userID, deviceID)` 存储和查询。

### 任务

1. **函数清单数据结构**
   - `internal/server/function_registry.go` 新建文件
   - 定义 `FunctionInfo`, `DeviceFunctions`, `RegisterFunctionsParams` 类型
   - `FunctionInfo` 包含: name, description, parameters (JSON Schema), returns, tags, timeout_ms

2. **ClientFunctionRegistry 实现**
   - 内存存储: `map[string]map[string]*DeviceFunctions` (userID → deviceID → functions)
   - `Register(userID, deviceID, deviceName, deviceType, functions)` — 注册/更新函数清单
   - `GetFunctions(userID, deviceID) []FunctionInfo` — 获取指定设备的函数
   - `Unregister(userID, deviceID)` — 设备断开时清理
   - 线程安全 (sync.RWMutex)

3. **`system.register_functions` 方法处理**
   - `internal/handler/` 新增 handler 或扩展现有 handler
   - 客户端连接后发送 `{method: "system.register_functions", params: {...}}`
   - 服务端解析并调用 `ClientFunctionRegistry.Register()`
   - 返回确认响应

4. **函数清单与连接生命周期联动**
   - 设备断开时自动调用 `Unregister()`
   - 设备替换时（Phase 1 逻辑），旧函数清单被新连接覆盖

5. **配置项**
   - `max_functions_per_device: 200` — 每个设备最大函数数量
   - 超过限制时返回错误

### 验收标准

- [ ] 客户端可通过 `system.register_functions` 注册函数清单
- [ ] `GetFunctions(userID, deviceID)` 返回正确函数列表
- [ ] `GetFunctions()` 对不存在的 `(userID, deviceID)` 返回空列表
- [ ] 设备断开后，`GetFunctions()` 返回空列表
- [ ] 函数数量超过 `max_functions_per_device` 时拒绝注册
- [ ] 并发注册/查询安全（race detector 通过）
- [ ] 单元测试覆盖: 注册、查询、注销、并发、超限

---

## Phase 3: Send 反馈增强 + 断连 Fail Pending

**对应设计**: [5.2 Send 反馈增强](./DESIGN_CLIENT_FUNCTION_AGENT_TOOLS.md#52-send-反馈增强), [5.3 连接断开 → 立即 Fail Pending](./DESIGN_CLIENT_FUNCTION_AGENT_TOOLS.md#53-连接断开--立即-fail-pending)

**依赖**: Phase 1（设备连接模型）

**目标**: Send 不再静默丢包，连接断开时 pending 请求立即失败。

### 任务

1. **`Client.Send()` 返回 error**
   - `internal/server/websocket_client.go`: 签名改为 `Send(msg []byte) error`
   - 连接已关闭: 返回 `ErrClientClosed`
   - buffer 满: 返回 `ErrSendBufferFull`（替代静默丢弃）
   - 成功: 返回 `nil`

2. **`sendToDevice()` 传播 Send 错误**
   - 当 `Send()` 返回 error 时，`sendToDevice()` 也返回 error
   - ReverseRPC 的 `ServerRequest()` 收到发送失败后，立即清理 pending entry 并返回 `ErrSendFailed`

3. **`CancelDevice(deviceID)` 实现**
   - `internal/server/reverse_rpc.go`: 新增 `CancelDevice(deviceID string)`
   - 遍历 pending map，找到目标设备的请求（需在 pending entry 中记录 deviceID）
   - 向每个 pending 的 `respCh` 发送合成错误 `ErrDeviceDisconnected`

4. **连接断开触发 CancelDevice**
   - `websocket_server.go` 连接清理逻辑中调用 `reverseRPC.CancelDevice(deviceID)`
   - 确保在 `Client.Close()` 之后、从索引中移除之前调用

5. **更新 `reverseRPCPending` 结构**
   - 新增 `deviceID string` 字段，用于按设备过滤

### 验收标准

- [ ] `Client.Send()` buffer 满时返回 `ErrSendBufferFull`（非静默丢弃）
- [ ] `Client.Send()` 连接关闭时返回 `ErrClientClosed`
- [ ] `ServerRequest()` 发送失败时立即返回 `ErrSendFailed`
- [ ] 连接断开时，该设备所有 pending 请求立即返回 `ErrDeviceDisconnected`
- [ ] pending 请求的阻塞调用方不再等待超时，立即收到错误
- [ ] 单元测试: Send 错误场景、CancelDevice、断连触发 fail

---

## Phase 4: 幂等键 + Redis 持久化

**对应设计**: [5.4 幂等键与 Redis 持久化](./DESIGN_CLIENT_FUNCTION_AGENT_TOOLS.md#54-幂等键与-redis-持久化)

**依赖**: Phase 1（设备模型 + UUID reqID）

**目标**: 超时请求持久化到 Redis，支持后续重连补发。

### 任务

1. **协议扩展: `PackageDataRequest` 新增字段**
   - `pkg/protocol/protocol.go`:
     ```go
     type PackageDataRequest struct {
         ID             string          `json:"id"`
         Method         string          `json:"method"`
         Params         json.RawMessage `json:"params"`
         IdempotencyKey string          `json:"idempotency_key,omitempty"`
         Seq            uint64          `json:"seq,omitempty"`
     }
     ```
   - `omitempty` 确保向后兼容

2. **per-device Seq 计数器**
   - `internal/server/reverse_rpc.go`: 新增 `deviceSeq map[string]uint64` (userID:deviceID → seq)
   - 每次发送请求时递增并分配 seq
   - 持久化到 Redis: `rrpc:device:seq:{userID}:{deviceID}`

3. **`PendingRequest` 数据结构**
   - 定义 `PendingRequest` 类型（包含 ID, UserID, DeviceID, Method, Params, IdempotencyKey, Seq, RetryCount, MaxRetries, CreatedAt）

4. **超时 → Redis 异步持久化**
   - `ServerRequest()` 超时后: `go persistToRedis(pending)`
   - Redis Key: `rrpc:pending:{userID}:{deviceID}` (List of PendingRequest JSON)
   - TTL: 24h（可配置）
   - 持久化不阻塞调用方，调用方仍收到 `ErrRequestTimeout`

5. **Redis 客户端接入**
   - 复用现有 Redis 连接模式（参考 `redis_connection_store.go`）
   - 新增 `ReverseRPCRedis` 或直接使用 `redis.Client`

6. **配置项**
   - `max_pending_per_device: 50`
   - `request_ttl: 24h`
   - `max_replay_retries: 3`

### 验收标准

- [ ] `PackageDataRequest` 包含 `idempotency_key` 和 `seq` 字段
- [ ] `omitempty` 确保旧客户端可正常解析（向后兼容）
- [ ] 超时请求异步写入 Redis，调用方立即收到 `ErrRequestTimeout`
- [ ] Redis 中可查看 pending 请求（JSON 格式正确）
- [ ] Redis key 有 TTL（24h 后自动清理）
- [ ] per-device seq 单调递增
- [ ] `max_pending_per_device` 限制生效
- [ ] 单元测试: Redis 持久化/读取/TTL/超限

---

## Phase 5: 重连握手 + 请求补发

**对应设计**: [5.5 重连握手与请求补发](./DESIGN_CLIENT_FUNCTION_AGENT_TOOLS.md#55-重连握手与请求补发)

**依赖**: Phase 4（幂等键 + Redis 持久化）, Phase 1（设备模型）

**目标**: 客户端重连后自动补发缺失请求。

### 任务

1. **`system.reconnect` 方法处理**
   - 客户端重连后发送 `{method: "system.reconnect", params: {last_seen_seq: N}}`
   - 服务端 handler 接收并处理

2. **缺失请求检测**
   - 查询 `rrpc:device:seq:{userID}:{deviceID}` 获取 `last_sent_seq`
   - 计算缺失范围: `[last_seen_seq+1, last_sent_seq]`
   - 从 `rrpc:pending:{userID}:{deviceID}` 查找对应的 pending 请求

3. **逐个补发**
   - 按 seq 顺序逐个重新发送到客户端
   - 每个补发等待响应（复用 ServerRequest 的阻塞逻辑）
   - 收到响应后从 Redis 移除该 pending

4. **重放重试计数**
   - 补发的请求如果再次超时: `retry_count++`
   - `retry_count > max_replay_retries` 时从 Redis 删除，放弃该请求
   - 记录日志

5. **seq 存储**
   - `rrpc:device:seq:{userID}:{deviceID}` 存储 last_sent_seq
   - 每次发送请求（包括补发）时更新

### 验收标准

- [ ] 客户端重连后发送 `system.reconnect`，携带 `last_seen_seq`
- [ ] 服务端正确计算缺失请求并逐个补发
- [ ] 补发的请求收到响应后从 Redis 移除
- [ ] 补发再次超时时 `retry_count` 递增
- [ ] 超过 `max_replay_retries` 后请求从 Redis 删除
- [ ] 补发结果记录日志（原调用方已离开，不返回结果）
- [ ] 集成测试: 断连 → 重连 → 补发成功
- [ ] 集成测试: 补发再次超时 → 重试计数 → 最终放弃

---

## Phase 6: DynamicToolProvider 中间件

**对应设计**: [6.3 DynamicToolProvider](./DESIGN_CLIENT_FUNCTION_AGENT_TOOLS.md#63-dynamictoolprovider-beforeagent-中间件), [6.4 工具创建](./DESIGN_CLIENT_FUNCTION_AGENT_TOOLS.md#64-工具创建-每个函数变成一个-invokabletool)

**依赖**: Phase 2（ClientFunctionRegistry）, Phase 3（Send 反馈，用于错误处理）

**目标**: Agent 运行时根据设备函数清单动态注入工具。

### 任务

1. **Context 传递 `(userID, deviceID)`**
   - Agent 会话创建时，从连接元数据提取 `(userID, deviceID)`
   - 通过 `context.WithValue()` 传递到 Agent 中间件
   - 定义 context key 类型（避免冲突）

2. **`DynamicToolProvider` 中间件实现**
   - `internal/agent/middleware_client_tools.go` 新建文件
   - 实现 Eino `BeforeAgent` 接口
   - `BeforeAgent()`:
     - 从 context 获取 `(userID, deviceID)`
     - 调用 `ClientFunctionRegistry.GetFunctions()`
     - 按配置过滤（tags, excluded）
     - 为每个函数创建 `InvokableTool`
     - 注入到 `runCtx.Tools`

3. **工具创建: 函数 → InvokableTool**
   - 工具名 = 函数名（无前缀，因为只有一个设备）
   - 工具描述 = 函数描述
   - 参数 Schema = 函数 parameters (JSON Schema)
   - 工具执行体 = 调用 `ReverseRPC.ServerRequest(userID, deviceID, funcName, params, timeout)`
   - 超时: 优先使用函数的 `timeout_ms`，否则使用配置的 `call_timeout`

4. **过滤逻辑**
   - `function_tags`: 只保留包含指定 tag 的函数（空 = 全部）
   - `excluded_functions`: 排除指定函数名

5. **错误处理**
   - 设备不在线: 工具执行返回 `ErrDeviceOffline`
   - 发送失败: 工具执行返回 `ErrSendFailed`
   - 超时: 工具执行返回 `ErrRequestTimeout`
   - 所有错误以 tool error 形式返回给 LLM

### 验收标准

- [ ] Agent 运行时，对应设备的函数自动注入为工具
   - [ ] 工具名、描述、参数 Schema 与函数清单一致
   - [ ] 工具执行通过 ReverseRPC 定向调用设备
- [ ] 设备无函数时，不注入任何工具（Agent 正常运行，无客户端工具）
- [ ] `function_tags` 过滤生效
- [ ] `excluded_functions` 过滤生效
- [ ] 工具执行失败时，LLM 收到有意义的错误信息
- [ ] 单元测试: 过滤逻辑、工具创建、context 传递

---

## Phase 7: Agent YAML 配置 + 端到端集成

**对应设计**: [6.5 Agent YAML 配置](./DESIGN_CLIENT_FUNCTION_AGENT_TOOLS.md#65-agent-yaml-配置), [6.6 中间件注册顺序](./DESIGN_CLIENT_FUNCTION_AGENT_TOOLS.md#66-中间件注册顺序), [7.4 新增配置字段](./DESIGN_CLIENT_FUNCTION_AGENT_TOOLS.md#74-新增配置字段)

**依赖**: Phase 6（DynamicToolProvider）, Phase 4（Redis 配置）

**目标**: 通过 YAML 配置控制客户端工具功能，完成端到端集成。

### 任务

1. **AgentConfig 扩展**
   - `internal/agent/config.go`:
     ```go
     type MiddlewareConfig struct {
         // 现有字段...
         EnableClientTools bool              `yaml:"enable_client_tools"`
         ClientTools       ClientToolsConfig `yaml:"client_tools"`
     }
     type ClientToolsConfig struct {
         FunctionTags      []string      `yaml:"function_tags"`
         ExcludedFunctions []string      `yaml:"excluded_functions"`
         CacheTTL          time.Duration `yaml:"cache_ttl"`
         CallTimeout       time.Duration `yaml:"call_timeout"`
     }
     ```

2. **中间件注册顺序**
   - `internal/agent/middleware.go` 的 `buildMiddleware()`:
   - `DynamicToolProvider` 在其他中间件**之前**注册（因为需要修改 `runCtx.Tools`）
   - 顺序: DynamicToolProvider → PatchToolCalls → Summarization → ToolReduction

3. **YAML 配置解析**
   - `agents/*.md` 的 frontmatter 支持 `enable_client_tools` 和 `client_tools` 配置
   - 默认值: `enable_client_tools: false`, `call_timeout: 30s`, `cache_ttl: 300s`

4. **服务端配置**
   - `configs/` 新增 `reverse_rpc` 和 `client_tools` 配置节
   - CLI flags 或环境变量:
     - `XYNCRA_RRPC_MAX_PENDING_PER_DEVICE`
     - `XYNCRA_RRPC_REQUEST_TIMEOUT`
     - `XYNCRA_RRPC_REQUEST_TTL`
     - `XYNCRA_RRPC_MAX_REPLAY_RETRIES`
     - `XYNCRA_CLIENT_TOOLS_DEFAULT_CACHE_TTL`
     - `XYNCRA_CLIENT_TOOLS_MAX_FUNCTIONS_PER_DEVICE`

5. **端到端集成测试**
   - 完整流程:
     1. 客户端连接（带 deviceID）
     2. 注册函数清单
     3. 发起 Agent 会话
     4. Agent 调用客户端函数
     5. 验证函数执行结果返回 LLM
   - 异常流程:
     1. 设备离线时 Agent 调用 → 错误返回
     2. 函数超时 → 错误返回
     3. 断连后重连 → 请求补发

### 验收标准

- [ ] YAML 配置 `enable_client_tools: true` 可启用客户端工具注入
- [ ] YAML 配置 `enable_client_tools: false`（默认）不注入
- [ ] `client_tools.function_tags` 过滤生效
- [ ] `client_tools.excluded_functions` 排除生效
- [ ] `client_tools.call_timeout` 控制工具调用超时
- [ ] 中间件注册顺序正确（DynamicToolProvider 最先）
- [ ] 服务端配置项可通过环境变量/CLI flags 覆盖
- [ ] **端到端测试通过**: 客户端注册 → Agent 调用 → 结果返回
- [ ] **异常测试通过**: 离线、超时、断连重连场景

---

## Phase 8: 客户端侧增强 + 自适应超时

**对应设计**: [5.6 客户端侧增强](./DESIGN_CLIENT_FUNCTION_AGENT_TOOLS.md#56-客户端侧增强), [5.7 自适应超时](./DESIGN_CLIENT_FUNCTION_AGENT_TOOLS.md#57-自适应超时)

**依赖**: Phase 1（设备模型）, Phase 5（重连协议）

**目标**: 客户端支持幂等缓存、响应重试队列、自适应超时。

### 任务

1. **客户端 deviceID 管理**
   - `pkg/client/connection.go`: 连接配置新增 `deviceID` 选项
   - `device_id: "auto"` 时自动生成（基于机器信息或随机 UUID）
   - 持久化到本地，确保重连时同一 deviceID

2. **幂等 key 缓存**
   - `pkg/client/client.go`: LRU 缓存最近 1000 个已处理的 `idempotency_key`
   - 收到请求时: 检查 key 是否在缓存中
   - 已存在: 直接返回缓存的响应（幂等去重）
   - 不存在: 正常处理，处理后写入缓存

3. **响应重试队列**
   - `SendPackage()` 失败时，响应入队（而非丢弃）
   - 队列大小: 可配置（默认 100）
   - 网络恢复后自动重发队列中的响应

4. **重连时发送 `system.reconnect`**
   - 客户端重连后自动发送 `{method: "system.reconnect", params: {last_seen_seq: N}}`
   - `last_seen_seq` 记录客户端最后收到的 seq

5. **函数清单自动注册**
   - 客户端提供 API 注册本地函数
   - 连接成功后自动发送 `system.register_functions`
   - 断连重连后重新注册

6. **自适应超时**
   - 客户端维护最近 10 次 RTT 记录
   - 根据 RTT 计算网络质量因子:
     - RTT < 200ms → 1.0x
     - 200ms-1s → 1.5x
     - 1s-5s → 2.0x
     - 有丢包 → 2.5x
   - 实际超时 = 基础超时 × 网络质量因子
   - 应用于 `Call()` 的超时计算

### 验收标准

- [ ] 客户端可配置 `deviceID`（固定值或自动生成）
- [ ] 自动生成 deviceID 在重启后保持不变（持久化）
- [ ] 相同 `idempotency_key` 的重复请求只执行一次
- [ ] LRU 缓存满时，最旧的 key 被淘汰
- [ ] `SendPackage()` 失败时响进入重试队列
- [ ] 网络恢复后重试队列中的响应自动发出
- [ ] 重连后自动发送 `system.reconnect` 消息
- [ ] 重连后自动重新注册函数清单
- [ ] 自适应超时根据 RTT 动态调整
- [ ] 单元测试: 幂等缓存、重试队列、自适应超时计算

---

## Phase 总结

| Phase | 名称 | 依赖 | 对应设计 | 预估复杂度 |
|-------|------|------|----------|-----------|
| 1 | 设备连接模型 + reqID UUID | 无 | 5.1, 5.8 | ★★★ |
| 2 | 函数清单协议 + Registry | P1 | 6.1, 6.2, 7.2 | ★★ |
| 3 | Send 反馈 + 断连 Fail | P1 | 5.2, 5.3 | ★★ |
| 4 | 幂等键 + Redis 持久化 | P1 | 5.4 | ★★★ |
| 5 | 重连握手 + 请求补发 | P4, P1 | 5.5 | ★★★★ |
| 6 | DynamicToolProvider 中间件 | P2, P3 | 6.3, 6.4 | ★★★ |
| 7 | Agent 配置 + 端到端集成 | P6, P4 | 6.5, 6.6, 7.4 | ★★★ |
| 8 | 客户端增强 + 自适应超时 | P1, P5 | 5.6, 5.7 | ★★★ |

**可并行的 Phase**:
- Phase 2 + Phase 3 + Phase 4 均仅依赖 Phase 1，可并行开发
- Phase 8 的大部分工作（除重连握手）可与 Phase 5 并行
