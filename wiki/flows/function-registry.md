# 函数注册 & 动态工具 & 反向 RPC

> 客户端函数的注册、发现、注入、远程调用的完整生命周期。

**相关文档**: 场景 5-7 的反向 RPC 详细机制见 [reverse-rpc.md](./reverse-rpc.md)，场景 6 的重连重放流程见 [reconnection.md](./reconnection.md)，场景 9 的热重载 CLI 流程见 [reload-agents.md](./reload-agents.md)。本文聚焦于函数注册生命周期和动态工具注入，反向 RPC 和重连仅从函数注册视角概述。

## 场景 1: 客户端函数注册

### 主流程

```mermaid
sequenceDiagram
    participant Client as 客户端设备
    participant WS as WebSocket 服务器
    participant Handler as registerFunctionsHandler
    participant Registry as MemoryFunctionRegistry

    Client->>WS: WebSocket 连接 (携带 user_id, device_id)
    WS->>WS: 建立连接, 存入 ConnectionStore
    Client->>WS: system.register_functions 请求
    WS->>Handler: HandleRequest(ctx, client, req)
    Handler->>Handler: 解析 RegisterFunctionsParams (JSON Unmarshal)
    alt JSON 解析失败
        Handler-->>WS: ValidationError("invalid params")
        WS-->>Client: 校验错误响应
    else 解析成功
        Handler->>Handler: 使用连接的 deviceID 覆盖客户端提供的 deviceID
        Handler->>Registry: RegisterFunctions(ctx, userID, deviceID, params)
        Registry->>Registry: 校验函数数量 <= MaxFunctionsPerDevice
        Registry->>Registry: 校验每个函数名 (非空, 长度, 无重复)
        Registry->>Registry: 浅拷贝 Functions (slice copy)，深拷贝 DeviceInfo
        Registry->>Registry: 写入 map[userID][deviceID] = DeviceFunctions
        Registry-->>Handler: nil (成功)
        Handler-->>WS: {"status":"ok", "count": N, "device_id": "..."}
        WS-->>Client: 响应包
    end
```

### 边缘场景

#### 1. 函数名为空

- 触发条件: 注册的 FunctionInfo 中 Name 字段为空字符串
- 处理逻辑: RegisterFunctions 遍历时检测到 `fn.Name == ""`，立即返回 `ErrFunctionNameEmpty`
- 最终结果: Handler 将其映射为 `protocol.NewValidationError`，客户端收到校验错误

#### 2. 函数名超长

- 触发条件: 函数名长度超过 `MaxFunctionNameLength`（默认 255）
- 处理逻辑: 返回 `ErrFunctionNameTooLong`
- 最终结果: 客户端收到校验错误

#### 3. 函数名重复

- 触发条件: 同一次注册请求中包含两个同名函数
- 处理逻辑: 使用 `seen` map 去重检测，返回 `ErrFunctionNameDuplicate`
- 最终结果: 客户端收到校验错误，注册不生效

#### 4. 超过设备函数数量上限

- 触发条件: 注册函数数量超过 `MaxFunctionsPerDevice`（默认 500）
- 处理逻辑: 在遍历校验前先检查 `len(params.Functions) > max`，返回 `ErrMaxFunctionsPerDevice`
- 最终结果: 客户端收到校验错误

#### 5. JSON 参数解析失败

- 触发条件: `req.Params` 无法反序列化为 `RegisterFunctionsParams`（格式错误、类型不匹配等）
- 处理逻辑: `json.Unmarshal` 返回错误，Handler 直接返回 `protocol.NewValidationError("invalid params")`
- 最终结果: 客户端收到校验错误，不进入注册逻辑

#### 6. deviceID 覆盖

- 触发条件: 客户端在 params 中提供了一个 deviceID
- 处理逻辑: Handler 忽略客户端提供的 deviceID，强制使用连接建立时认证的 `client.DeviceID()`
- 最终结果: 连接级 deviceID 始终为权威来源

### 涉及文件

- `internal/handler/register_functions.go`: 解析请求、deviceID 覆盖、调用注册表、错误映射
- `internal/server/function_registry.go`: 校验逻辑、内存存储、深拷贝隔离
- `pkg/protocol/function.go`: FunctionInfo / ReturnInfo 协议定义

---

## 场景 2: 函数全量替换 (更新)

### 主流程

```mermaid
flowchart TD
    A[客户端发送 register_functions] --> B{该 deviceID 是否已注册?}
    B -->|否| C[创建新的 DeviceFunctions 记录]
    B -->|是| D[用新数据完全替换旧记录]
    C --> E[记录 RegisteredAt = now]
    D --> E
    E --> F[返回成功响应]
    F --> G[旧函数列表被丢弃]
```

### 边缘场景

#### 1. 空函数列表清除注册

- 触发条件: 客户端发送空的 Functions 数组
- 处理逻辑: 空数组通过校验，写入后该 deviceID 的函数列表为空
- 最终结果: DeviceFunctions 记录仍存在（DeviceInfo 保留），但函数列表为空

#### 2. 并发写同一设备

- 触发条件: 多个 goroutine 同时对同一 (userID, deviceID) 调用 RegisterFunctions
- 处理逻辑: `sync.RWMutex` 的写锁保证互斥，last-writer-wins 语义
- 最终结果: 注册完成后只有一个版本生效，不会出现数据损坏

### 涉及文件

- `internal/server/function_registry.go`: RegisterFunctions 的全量替换实现

---

## 场景 3: 客户端断开后函数清理

### 主流程

```mermaid
sequenceDiagram
    participant Client as 客户端设备
    participant WS as WebSocket 服务器
    participant CS as ConnectionStore
    participant Registry as FunctionRegistry
    participant RPC as ReverseRPC

    Client->>WS: WebSocket 连接
    Client->>WS: register_functions (注册 N 个函数)
    Note over Registry: 函数已存储
    Client--xWS: 连接断开 (网络异常/主动关闭)
    WS->>WS: handleWebSocket defer 清理
    WS->>CS: Remove(connID) [5s 超时]
    WS->>WS: removeClient(connID, userID, deviceID)

    alt functionRegistry != nil 且设备无其他活跃连接 (!hasActiveConn)
        WS->>WS: scheduleFuncCleanup(userID, deviceID)
        Note over WS: 启动 grace period 定时器 (默认 10s)
        alt grace period 内设备重连
            Note over WS: 新连接的 handleWebSocket 中调用 cancelPendingFuncCleanup
            Note over WS: 取消清理, 函数保留
        else grace period 超时且无活跃连接
            WS->>Registry: OnDeviceDisconnect(userID, deviceID)
            Registry->>Registry: 删除 map[userID][deviceID]
            Registry->>Registry: 若该 user 无其他设备, 删除 user 级 map entry
            Note over Registry: 函数已清理
        end
    end

    alt reverseRPC != nil 且设备无其他活跃连接 (!hasActiveConn)
        WS->>RPC: CancelDeviceWithReason(userID, deviceID, "device disconnected")
    end
```

### 边缘场景

#### 1. 幂等断开清理

- 触发条件: OnDeviceDisconnect 被多次调用（例如竞态条件）
- 处理逻辑: 若 deviceID 不存在，直接返回 `(nil, nil)`，不报错
- 最终结果: 幂等安全，多次调用不会 panic 或返回错误

#### 2. 断开不影响其他设备

- 触发条件: 同一用户下有多个设备，其中一个断开
- 处理逻辑: OnDeviceDisconnect 只删除目标 deviceID 的条目，其他设备不受影响
- 最终结果: 其他设备的函数注册保持不变

#### 3. 设备替换场景下的函数保留

- 触发条件: 同一 (userID, deviceID) 的新连接建立时旧连接被踢出
- 处理逻辑: 新连接的 `handleWebSocket` 在注册新连接时调用 `cancelPendingFuncCleanup` 取消旧连接的 grace period 定时器，避免函数被误删。此外，`CancelDevice` 在 Upgrade 前执行，取消旧连接的所有 pending RPC 请求（reason: "device replaced"）。这是两个独立机制：前者保护函数注册，后者保护 RPC 请求
- 最终结果: 函数注册在 grace period 内被保留。新连接通常会重新注册函数（通过 `system.register_functions`），即使旧连接的延迟清理先于新注册执行，`scheduleFuncCleanup` 中的 `hasActiveConn` 二次检查也会阻止清理属于新连接的函数

#### 4. Grace period 机制

- 触发条件: 设备断开连接后，函数清理不立即执行
- 处理逻辑: `scheduleFuncCleanup` 启动一个带有 grace period 的定时器。如果设备在 grace period 内重连，`cancelPendingFuncCleanup` 取消清理。只有 grace period 超时且无活跃连接时，才调用 `OnDeviceDisconnect`
- 最终结果: 避免页面导航期间（短暂断开后立即重连）函数被误删

#### 5. scheduleFuncCleanup 实现细节

- **使用 `context.Background()`**: 定时器使用 `context.Background()` 而非调用方的 context，因为调用方 context（handleWebSocket 的 cleanupCtx）在 handleWebSocket 返回时会被取消，会导致 grace period 立即失效
- **安全取消**: 创建新的 cleanup 前，先取消同一设备已有的 pending cleanup（防止重复调度）
- **双重检查**: grace period 超时后，再次检查 `clientsByDevice` 确认无活跃连接，才执行清理。这防止了 grace period 期间新连接注册但 cancelPendingFuncCleanup 未被调用的极端竞态

### 涉及文件

- `internal/server/function_registry.go`: OnDeviceDisconnect 实现
- `internal/server/websocket_server.go`: scheduleFuncCleanup、cancelPendingFuncCleanup、removeClient、performDeviceReplacement
- `internal/server/function_lifecycle_test.go`: 断开清理、设备替换、多设备隔离测试

---

## 场景 4: 动态工具注入 (Agent 运行时)

### 主流程

```mermaid
sequenceDiagram
    participant Agent as Eino Agent
    participant DTP as DynamicToolProvider
    participant FR as FunctionRegistry
    participant TR as ToolRegistry
    participant CTX as AgentContext

    Agent->>DTP: BeforeAgent(ctx, runCtx)
    DTP->>DTP: CallerDeviceFromContext(ctx) 获取设备信息

    alt 有设备上下文
        DTP->>FR: GetFunctions(userID, deviceID)
        FR-->>DTP: []FunctionInfo
        DTP->>DTP: applyFilters(funcs) 过滤
        loop 每个函数
            DTP->>DTP: newClientFunctionTool(fn, caller, ...)
        end
    end

    alt toolRegistry != nil 且 AgentConfig.DynamicTools 非空
        DTP->>TR: Create(ctx, dynamicTools, nil)
        TR-->>DTP: []BaseTool (按名称从注册表解析)
    end

    DTP->>DTP: 合并所有工具到 runCtx.Tools
    DTP-->>Agent: 返回更新后的 context
```

### 边缘场景

#### 1. 无设备上下文时跳过客户端函数

- 触发条件: CallerDeviceFromContext 返回 false（非设备发起的调用）
- 处理逻辑: 跳过整个客户端函数注入分支，只处理 dynamicTools
- 最终结果: Agent 正常运行，仅使用静态工具

#### 2. GetFunctions 失败 (fail-open)

- 触发条件: `FunctionRegistry.GetFunctions` 查询出错
- 处理逻辑: 记录错误日志，跳过客户端函数注入，不阻塞 Agent 执行
- 最终结果: Agent 继续运行，只是没有客户端工具

#### 2b. 设备未注册函数 (正常情况)

- 触发条件: `GetFunctions` 返回 `(nil, nil)`——设备从未调用过 `system.register_functions`
- 处理逻辑: 不是错误，`applyFilters(nil)` 返回空列表，`len(funcs) > 0` 为 false，跳过工具创建
- 最终结果: Agent 正常运行，无客户端工具注入（也不产生日志）

#### 3. 单个工具创建失败 (fail-open per function)

- 触发条件: `buildToolInfo` 失败（JSON Marshal 参数、JSON Schema 解析、或其他构建错误）
- 处理逻辑: 记录该函数的错误日志，continue 跳过，其他函数正常创建
- 最终结果: 部分工具可用，不影响其他工具

#### 4. 函数过滤：标签匹配

- 触发条件: AgentConfig 中配置了 `function_tags`
- 处理逻辑: applyFilters 使用 OR 语义，函数只要有一个 tag 匹配即保留
- 最终结果: 只有匹配标签的函数被注入为工具

#### 5. 函数过滤：排除列表

- 触发条件: AgentConfig 中配置了 `excluded_functions`
- 处理逻辑: 排除检查优先于标签检查，精确匹配函数名
- 最终结果: 被排除的函数不会出现在工具列表中

#### 6. 空 Parameters 处理

- 触发条件: FunctionInfo.Parameters 为 nil 或空 map
- 处理逻辑: buildToolInfo 将其规范化为 `{"type":"object","properties":{}}`
- 最终结果: 避免 LLM 层将空 schema 转为 `parameters: true` 导致 400 错误

#### 7. 动态工具注册表创建失败

- 触发条件: `toolRegistry.Create(ctx, dynamicTools, nil)` 中部分工具工厂函数失败
- 处理逻辑: `Create` 有两种降级行为——(1) 未注册的工具名被跳过并记录警告，不进入返回列表；(2) 工厂函数出错的工具被跳过并记录错误，错误被收集并在最后以 `errors.Join` 返回。**已成功创建的工具仍被返回**（`Create` 返回 `([]tool.BaseTool, error)` 元组）。`BeforeAgent` 记录错误日志后，仍会注入已成功创建的工具（`len(staticTools) > 0` 检查）
- 最终结果: Agent 继续运行，部分动态工具可用。与客户端函数的逐个 fail-open 行为一致

### 涉及文件

- `internal/agent/dynamic_tool_provider.go`: BeforeAgent 中间件、applyFilters 过滤逻辑
- `internal/agent/client_function_tool.go`: buildToolInfo 构建 schema、executeClientFunction 执行调用
- `internal/agent/config.go`: MiddlewareConfig、ClientToolsConfig 配置定义
- `internal/agent/context_keys.go`: CallerDevice 上下文传递

---

## 场景 5: 反向 RPC 调用 (正常路径)

### 主流程

```mermaid
sequenceDiagram
    participant Agent as Agent/Tool
    participant RPC as ReverseRPC
    participant WS as WebSocket 连接
    participant Client as 客户端设备

    Agent->>RPC: ServerRequest(ctx, userID, deviceID, method, params, timeout)
    RPC->>RPC: 生成 reqID = "s-{uuid}"
    RPC->>RPC: 分配 seq (per-device 单调递增)
    RPC->>RPC: 创建 pending 记录 (respCh, cancel)
    RPC->>RPC: 注册到 pending map
    RPC->>RPC: 构造 PackageDataRequest (含 IdempotencyKey, Seq)
    RPC->>WS: sendFunc(userID, deviceID, pkg)
    WS->>Client: WebSocket 发送请求包
    Client->>Client: 处理请求
    Client->>WS: 返回 PackageDataResponse
    WS->>RPC: DispatchResponse(resp)
    RPC->>RPC: 从 pending map 取出记录
    RPC->>RPC: 写入 respCh
    RPC->>RPC: 删除 pending 记录
    RPC-->>Agent: 返回响应
```

### 边缘场景

#### 1. 超时处理

- 触发条件: 客户端未在 timeout 内响应
- 处理逻辑: `ctx.Done()` 触发，若为 `DeadlineExceeded` 且配置了 PendingStore，异步持久化请求。注意：仅 `DeadlineExceeded` 触发持久化，`context.Canceled`（父 context 取消）不持久化
- 最终结果: 返回 `context.DeadlineExceeded` 错误，请求被保存供后续重播

#### 2. 发送失败

- 触发条件: sendFunc 返回错误（设备离线/连接断开）
- 处理逻辑: 若错误为 `ErrDeviceOffline` 且配置了 PendingStore，请求先通过 `persistAsync` 异步持久化后再返回错误（与场景 6 的超时持久化类似）；否则立即返回错误，不进入 select 等待
- 最终结果: 直接报错（离线时请求已保存供后续重播）

#### 3. 响应到达时 pending 已清理 (迟到响应)

- 触发条件: 超时后 pending 记录被 defer 删除，此时客户端响应才到达
- 处理逻辑: DispatchResponse 在 pending map 中找不到对应 ID，静默忽略
- 最终结果: 无副作用，不 panic

#### 4. respCh 已满时写入

- 触发条件: respCh（buffered cap=1）已有数据时再次写入
- 处理逻辑: select-default 模式，写不进去就跳过
- 最终结果: 不阻塞 DispatchResponse 的调用方

#### 5. 请求序列化失败

- 触发条件: `json.Marshal(req)` 失败（PackageDataRequest 包含不可序列化的字段）
- 处理逻辑: 立即返回 `marshal request` 错误，pending 记录被 defer 清理
- 最终结果: 请求不会发送到客户端，直接报错

### 涉及文件

- `internal/server/reverse_rpc.go`: ServerRequest、DispatchResponse、persistAsync
- `internal/agent/client_function_tool.go`: executeClientFunction 调用 ServerRequest

---

## 场景 6: 反向 RPC 超时持久化与重播

### 主流程

```mermaid
flowchart TD
    A[ServerRequest 超时] --> B{是 DeadlineExceeded?}
    B -->|否| C[直接返回 context 错误]
    B -->|是| D{PendingStore 已配置?}
    D -->|否| C
    D -->|是| E[persistAsync: 异步保存到 Redis]
    E --> F[PendingRequest 记录含 ID/Method/Params/Seq/IdempotencyKey]

    G[设备重连] --> H[reconnect handler]
    H --> I[PendingStore.List 获取待重播请求]
    I --> J[过滤: Seq > last_seen_seq AND RetryCount < MaxRetries]
    J --> K[立即返回 replayed/total 计数]
    K --> L[每个请求启动独立 goroutine 并发重播]
    L --> M2[ReplayRequest: 新 reqID, 保留原 IdempotencyKey, context.Background() 10s 超时]
    M2 --> M{响应到达?}
    M -->|成功| N[PendingStore.Remove 移除]
    M -->|超时| O{RetryCount < MaxRetries?}
    O -->|是| P[Update RetryCount, 等下次重连]
    O -->|否| Q[移除, 放弃重试]
```

### 边缘场景

#### 1. PendingStore 保存失败

- 触发条件: Redis 不可用或网络异常
- 处理逻辑: persistAsync 在独立 goroutine 中执行，5s 超时，失败仅记录日志（fail-open）
- 最终结果: 请求丢失但不影响主流程

#### 2. 幂等性保证

- 触发条件: 同一请求被重播多次
- 处理逻辑: IdempotencyKey（等于原始 reqID）保持不变，客户端侧去重
- 最终结果: 客户端对相同 IdempotencyKey 只执行一次

#### 3. 达到最大重试次数

- 触发条件: RetryCount >= MaxRetries（默认 3）
- 处理逻辑: 请求被移除，不再重播
- 最终结果: 持久化请求被丢弃

#### 4. last_seen_seq 过滤

- 触发条件: 客户端在 system.reconnect 请求中携带 last_seen_seq 参数
- 处理逻辑: 只重播 Seq > last_seen_seq 的请求，跳过客户端已见过的请求
- 最终结果: 避免重复执行客户端已处理过的请求（即使服务端未收到响应）

#### 5. PendingStore 容量限制

- 触发条件: 单设备 pending 请求超过 MaxPendingPerDevice（默认 50）
- 处理逻辑: Redis RPush + LTrim 保留最新的 N 条，旧的被丢弃
- 最终结果: 防止单设备无限堆积

#### 5b. PendingStore TTL 过期

- 触发条件: pending 请求在 Redis 中存在超过 RequestTTL（默认 24h）
- 处理逻辑: 每次 Save/Remove/Update 操作都会刷新 key 的 Expire 时间。超时后 Redis 自动删除整个 list
- 最终结果: 长期未重连的设备的 pending 请求自动过期清理，避免 Redis 内存泄漏

#### 6. 重播立即返回

- 触发条件: reconnect handler 收到 system.reconnect 请求
- 处理逻辑: 过滤后立即返回 `{status:"ok", replayed:N, total:M}`，每个待重播请求在独立 goroutine 中异步执行
- 最终结果: 客户端不阻塞等待重播完成，replayed 计数不代表完成确认

#### 7. 重播使用 context.Background()

- 触发条件: replayOne 执行重放
- 处理逻辑: 使用 `context.Background()` 而非 handler 的 context，确保客户端断连不会取消正在进行的重放
- 最终结果: 重放请求有独立的 10 秒超时，不受 handler 生命周期影响

#### 8. 重放失败判定

- 触发条件: `err != nil || resp == nil || resp.Code != 0`
- 处理逻辑: 三种情况均视为失败（发送错误、超时、客户端返回非零 Code），递增 RetryCount
- 最终结果: 非 Code == 0 的响应也会触发重试，而非仅超时

### 涉及文件

- `internal/server/reverse_rpc.go`: persistAsync、ReplayRequest
- `internal/server/pending_store.go`: PendingStore 接口、PendingRequest 模型
- `internal/server/redis_pending_store.go`: RedisPendingStore 实现
- `internal/handler/reconnect.go`: reconnect handler 调用 List + ReplayRequest

---

## 场景 7: 设备断开时取消所有待处理 RPC

### 主流程

```mermaid
sequenceDiagram
    participant Client as 客户端设备
    participant WS as WebSocket 服务器
    participant RPC as ReverseRPC
    participant Caller1 as 调用方 1
    participant Caller2 as 调用方 2

    Caller1->>RPC: ServerRequest (等待中)
    Caller2->>RPC: ServerRequest (等待中)
    Client--xWS: 连接断开
    WS->>WS: removeClient + 检查 hasActiveConn
    alt !hasActiveConn (无替代连接)
        WS->>RPC: CancelDeviceWithReason(userID, deviceID, "device disconnected")
    end
    RPC->>RPC: 遍历 pending map, 筛选该设备的所有请求
    RPC->>RPC: 从 pending map 中删除
    RPC->>Caller1: respCh 写入 {Code:-1, Msg:"device disconnected"}
    RPC->>Caller2: respCh 写入 {Code:-1, Msg:"device disconnected"}
    Note over Caller1,Caller2: 收到软失败响应, LLM 可自纠正
```

### 边缘场景

#### 1. 设备替换时的取消

- 触发条件: 同一 (userID, deviceID) 新连接建立
- 处理逻辑: CancelDevice 使用默认 reason "device replaced"
- 最终结果: 旧连接的所有待处理请求被取消

#### 2. 服务器关闭时取消所有

- 触发条件: CancelAll 被调用（shutdown）
- 处理逻辑: 遍历所有 pending 请求，写入 `{Code:-1, Msg:"reverse rpc cancelled"}`
- 最终结果: 所有待处理请求被取消

#### 3. 软失败映射

- 触发条件: executeClientFunction 收到 Code < 0 的响应
- 处理逻辑: 使用 `agenttools.SoftFailure` 包装，返回内容而非 Go error
- 最终结果: LLM 看到错误原因并可自行决定重试或通知用户

#### 4. 不调用 p.cancel()

- 触发条件: CancelDeviceWithReason 向 respCh 写入合成响应后
- 处理逻辑: **不调用** `p.cancel()`。ServerRequest 的 `select` 有两个分支（respCh 和 ctx.Done()），如果同时触发，Go 的 select 会随机选择。调用 cancel() 会导致 ctx.Done() 触发，与 respCh 写入竞争
- 最终结果: 不调用 cancel() 保证 ServerRequest 一定通过 respCh 收到合成响应，返回有意义的错误信息而非 context.Canceled

### 涉及文件

- `internal/server/reverse_rpc.go`: CancelDeviceWithReason、CancelDevice、CancelAll
- `internal/agent/client_function_tool.go`: executeClientFunction 中的软失败处理

---

## 场景 8: 客户端函数工具执行 (Agent 调用远程函数)

### 主流程

```mermaid
sequenceDiagram
    participant LLM as LLM
    participant Tool as ClientFunctionTool
    participant RPC as ReverseRPC
    participant Client as 客户端设备

    LLM->>Tool: InvokableRun(ctx, input JSON)
    Tool->>Tool: 确定 timeout (函数级 > 默认级 > 30s)
    Tool->>RPC: ServerRequest(ctx, userID, deviceID, funcName, input, timeout)

    alt 成功
        RPC-->>Tool: PackageDataResponse (Code >= 0)
        Tool-->>LLM: 返回 Data 字符串
    else 业务错误
        RPC-->>Tool: PackageDataResponse (Code < 0)
        Tool->>Tool: SoftFailure("client returned error (code N): msg")
        Tool-->>LLM: 返回软失败内容
    else 网络/超时错误
        RPC-->>Tool: Go error
        Tool->>Tool: formatClientToolError(err)
        Tool->>Tool: SoftFailure(友好错误消息)
        Tool-->>LLM: 返回软失败内容
    end
```

### 边缘场景

#### 1. 超时优先级

- 触发条件: 函数定义了 timeout_ms，同时 Agent 配置了 `client_tools.call_timeout`
- 处理逻辑: 函数级 `FunctionInfo.TimeoutMs` > `ClientToolsConfig.CallTimeout` > 30s 硬编码兜底（三者均 <= 0 时取下一级）
- 最终结果: 使用最具体的超时值

#### 2. LLM 友好的错误消息

- 触发条件: 底层错误为 deadline exceeded 或 device offline
- 处理逻辑: formatClientToolError 将底层错误映射为 LLM 可理解的描述性消息
- 最终结果: LLM 能根据错误消息决定重试或告知用户

#### 3. 软失败不中断 Agent

- 触发条件: 任何工具调用失败（网络/业务/超时）
- 处理逻辑: 所有失败都通过 SoftFailure 返回内容而非 Go error
- 最终结果: Agent 运行不中断，LLM 可自行处理失败

#### 4. 设备离线重试

- 触发条件: ServerRequest 返回离线错误（`isDeviceOfflineError` 检测 "device offline"、"no connections"、"device is offline" 子串）
- 处理逻辑: 等待 3 秒后重试一次（设备可能正在重连中），若 ctx 取消则立即返回 SoftFailure
- 最终结果: 若重试成功则正常返回，若仍失败则通过 SoftFailure 返回 "device is offline"

### 涉及文件

- `internal/agent/client_function_tool.go`: newClientFunctionTool、executeClientFunction、buildToolInfo、formatClientToolError

---

## 场景 9: Agent 配置热重载

### 主流程

```mermaid
sequenceDiagram
    participant Admin as 管理员
    participant Handler as reloadAgentsHandler
    participant Registry as AgentRegistry

    Admin->>Handler: reload_agents 请求
    Handler->>Registry: Reload()
    Registry->>Registry: RLock 读取 dir, 释放锁
    Registry->>Registry: Load(dir)
    Registry->>Registry: Lock, 清空现有 agents map, 更新 dir
    Registry->>Registry: 扫描 dir 下所有 .md 文件
    loop 每个 .md 文件
        Registry->>Registry: ReadFile
        Registry->>Registry: ParseFrontMatter (解析 YAML + Validate + 提取 SystemPrompt)
        alt 校验失败 (缺少必填字段/MCP 配置错误)
            Registry->>Registry: 跳过, 记录 Info 日志
        else ID 重复
            Registry->>Registry: 跳过, 记录 Info 日志
        else 正常
            Registry->>Registry: 注册到 agents map
        end
    end
    Registry-->>Handler: nil
    Handler-->>Admin: {"count": N}
```

### 边缘场景

#### 1. 目录不存在

- 触发条件: agents 目录路径不存在
- 处理逻辑: `os.IsNotExist(err)` 返回 true 时 Load 返回 nil（可选模块）
- 最终结果: 不报错，count 为 0

#### 2. registry 为 nil

- 触发条件: reloadAgentsHandler 的 registry 字段为 nil
- 处理逻辑: 直接返回 `{"count": 0}`，不报错
- 最终结果: 兼容未配置 Agent 的场景

#### 3. Agent ID 重复

- 触发条件: 多个 .md 文件解析出相同的 AgentConfig.ID
- 处理逻辑: 后续的被跳过，记录日志
- 最终结果: 先加载的生效

#### 4. MCP 配置校验

- 触发条件: Agent 配置中包含 mcp_servers
- 处理逻辑: Validate 检查 MCP name 非空且唯一、transport 有效、URL/Command 必填
- 最终结果: 无效 MCP 配置会导致整个 Agent 被跳过

### 涉及文件

- `internal/handler/reload_agents.go`: reloadAgentsHandler
- `internal/agent/registry.go`: AgentRegistry.Load、Reload、IsAgent
- `internal/agent/config.go`: AgentConfig.Validate、MCPServerConfig 校验

---

## 场景 10: Per-Device 序列号分配

### 主流程

```mermaid
flowchart LR
    A[ServerRequest 调用] --> B[nextSeq userID deviceID]
    B --> C[deviceSeqMu 加锁]
    C --> D["key = userID + '\\x00' + deviceID"]
    D --> E["deviceSeq[key]++"]
    E --> F[返回递增后的 seq]
    F --> G[写入 PackageDataRequest.Seq]
```

### 边缘场景

#### 1. 不同设备独立计数

- 触发条件: 同一用户的两个不同设备各自发起 RPC
- 处理逻辑: key 包含 userID 和 deviceID，各自独立递增
- 最终结果: 每个设备的 seq 从 1 开始单调递增

#### 2. 并发安全

- 触发条件: 多个 goroutine 同时调用 nextSeq
- 处理逻辑: `deviceSeqMu` 互斥锁保护
- 最终结果: seq 不会出现重复或跳号

### 涉及文件

- `internal/server/reverse_rpc.go`: nextSeq 方法
