---
last_updated: 2026-07-21
---

# 客户端注册与函数管理

本文档详细描述 Xyncra Server 中客户端注册与函数管理的完整业务流程，包括连接建立、函数注册/注销、Agent 配置热重载、动态工具注入等核心流程。

## 流程概览

```mermaid
graph TB
    subgraph "连接层"
        A[客户端连接注册] --> B[WebSocket 连接建立]
        B --> C[连接信息存储]
    end

    subgraph "函数管理层"
        D[函数注册] --> E[函数存储]
        F[函数注销] --> G[函数清理]
    end

    subgraph "Agent 层"
        H[Agent 配置热重载] --> I[配置更新]
        J[动态工具注入] --> K[Agent 执行]
    end

    subgraph "服务层"
        L[处理器注册] --> M[RPC 方法分发]
    end

    A --> D
    D --> J
    H --> J
    L --> A
    L --> D
    L --> H
    L --> J

    style A fill:#e1f5fe
    style D fill:#e1f5fe
    style F fill:#e1f5fe
    style H fill:#e1f5fe
    style J fill:#e1f5fe
    style L fill:#e1f5fe
```

---

## 1. 客户端连接注册流程 (client_connection_registration)

### 概述

客户端通过 WebSocket 连接到服务器，在 ConnectionStore 和本地索引中注册自身。这是所有后续函数管理流程的基础。

### 流程图

```mermaid
sequenceDiagram
    participant C as 客户端
    participant CS as ConnectionManager
    participant WS as WebSocket 服务
    participant RL as ReverseRPC
    participant CM as ConnectionStore
    participant FR as FunctionRegistry

    C->>CS: 启动 connectionMonitorWithInitialConnect
    CS->>WS: Connect() 建立 WebSocket 连接

    Note over WS: 1. HTTP 升级为 WebSocket
    Note over WS: 2. 从认证中提取 userID
    Note over WS: 3. 从查询参数提取 device_id

    alt device_id 缺失
        WS->>WS: 自动生成 UUID v4 (D-094)
    end

    WS->>WS: 检查 clientsByDevice[userID+deviceID]

    alt 存在旧连接
        WS->>RL: CancelDevice() 取消挂起请求
        RL-->>WS: 完成
    end

    WS->>WS: 在单次锁获取中执行以下操作
    Note over WS: 1. 从 clientsByDevice 删除旧连接引用<br/>2. 注册新连接到 clients/clientsByUser/clientsByDevice<br/>3. cancelPendingFuncCleanup(userID, deviceID)

    alt 存在旧连接
        WS->>WS: performDeviceReplacement 协程
        Note over WS: 1. 发送 4001 关闭帧
        Note over WS: 2. 等待 10ms 刷新 TCP
        Note over WS: 3. 关闭旧客户端
        Note over WS: 4. 等待最多 500ms 让协程退出
        Note over WS: 5. removeClient 清理旧连接本地映射
        Note over WS: 注意：ConnectionStore.Remove 由旧连接自身清理
    end

    WS->>CM: 注册 ConnectionInfo
    CM-->>WS: 完成

    WS->>WS: client.Run() 阻塞运行
    Note over WS: 读写泵开始工作

    Note over CS: 顺序执行重连握手（完成后再执行 FullSync）
    CS->>WS: system.register_functions (顺序, fail-open, 10s 超时)
    CS->>WS: system.reconnect (顺序, fail-open, 携带 last_seen_seq)
    CS->>CS: syncMgr.FullSync (分页拉取增量更新)

    Note over WS: 连接断开时
    WS->>CM: ConnectionStore.Remove()
    WS->>WS: removeClient() 清理本地映射
    WS->>FR: 清理函数注册表
```

### 详细步骤

1. **客户端启动连接监控**
   - 客户端调用 `XyncraClient.Start()` 启动 `connectionMonitorWithInitialConnect` 协程
   - 该协程负责监控连接状态并处理重连逻辑

2. **建立 WebSocket 连接**
   - 客户端的 `connectionManager.Connect()` 建立到服务器 `/ws` 端点的 WebSocket 连接
   - 连接过程中完成 HTTP 升级和认证

3. **服务器处理连接**
   - 从认证信息中提取 `userID`
   - 从查询参数中提取 `device_id`
   - 如果 `device_id` 缺失，服务器自动生成 UUID v4 (D-094)

4. **设备替换检测**
   - 服务器检查 `clientsByDevice[userID+"\x00"+deviceID]` 是否存在已有连接
   - 如果存在旧连接，触发设备替换流程

5. **取消挂起请求**
   - 调用 `reverseRPC.CancelDevice()` 立即失败所有挂起的请求

6. **原子注册新客户端**
   - 在单次锁获取中执行以下操作：
     - 从 `clientsByDevice[deviceKey]` 中删除旧连接引用
     - 注册新连接到三个映射：`clients[connID]`、`clientsByUser[userID][connID]`、`clientsByDevice[deviceKey][connID]`
     - 调用 `cancelPendingFuncCleanup(userID, deviceID)` 取消可能存在的函数清理定时器

7. **设备替换协程**
   - `performDeviceReplacement` 协程发送 `4001` 关闭帧给旧连接
   - 等待 10ms 刷新 TCP
   - 关闭旧客户端
   - 等待最多 500ms 让协程退出
   - 调用 `removeClient` 清理旧连接的本地映射
   - 注意：`ConnectionStore.Remove` 不在此处调用，由旧连接自身的 `handleWebSocket` 在 `client.Run()` 返回后执行

8. **存储连接信息**
   - 构建 `ConnectionInfo`（包含 connID、userID、deviceID、Protocol="websocket"、IPAddress、Status="active"）
   - `extractIP(r)` 从 HTTP 请求中提取客户端 IP（优先 `X-Forwarded-For`）
   - 如果配置了 `connectionInfoEnricher`，从 HTTP 请求填充额外字段
   - 在 `ConnectionStore`（Redis 或内存）中注册 `ConnectionInfo`

9. **运行连接**
   - `client.Run()` 阻塞运行（读写泵）
   - 当返回时执行清理：
     - `ConnectionStore.Remove()`
     - `removeClient()`
     - 函数注册表清理

10. **客户端侧握手与同步**
    - 连接后顺序执行 `performReconnectHandshake`（被 await，完成后再继续）：
      - 先发送 `system.register_functions`（确保服务端在 PendingStore 重放前有 handler；10 秒超时，fail-open）
      - 再发送 `system.reconnect`（携带 `last_seen_seq`；fail-open）
    - 握手完成后执行 `syncMgr.FullSync`（分页拉取增量更新，从本地 `localMaxSeq` 开始）

### 边缘场景

| 场景 | 处理方式 | 设计决策 |
|------|---------|---------|
| 认证失败（`authenticate` 返回错误） | 返回 HTTP 401 "authentication failed" | 连接不建立 |
| 认证成功但 `userID` 为空 | 返回 HTTP 401 "missing user id" | 连接不建立 |
| 查询参数中无 `device_id` | 服务器自动生成 UUID v4 | D-094：确保每个连接有唯一标识 |
| `device_id` 超过 255 个字符 | 返回 HTTP 400 "device_id too long" | 防止存储溢出 |
| WebSocket 升级失败 | 记录错误并返回，不注册连接 | `upgrader.Upgrade` 失败时直接退出 |
| `ConnectionStore.Add` 失败 | 客户端立即被关闭（`client.Close()`），调用 `removeClient` 清理本地映射，不执行函数清理 | 连接未完全建立，避免清理不一致状态 |
| 设备替换竞态 | 新连接注册时调用 `cancelPendingFuncCleanup`；旧连接断开时 `hasActiveConn` 为 true 跳过清理 | 双重保护：主动取消 + 被动检查 |
| 清理期间 Redis 不可达 | 5 秒有界上下文防止无限阻塞 | 最终一致性保证 |
| `4001` 关闭帧 | 旧客户端收到关闭码 4001 | D-111：触发优雅退出而非重连 |
| 服务器 GracefulStop | `closeAllClients()` 运行：(1) `CancelAll()` 取消所有挂起的反向 RPC；(2) 在单次锁获取中重置所有客户端索引映射；(3) 对每个客户端调用 `Close()`；(4) 等待最多 5 秒让所有 writePump 排空 | 客户端断开后触发各自的 defer 清理（`scheduleFuncCleanup` 延迟执行，进程退出前可能未完成） |
| 客户端不使用服务器分配的 `deviceID` | 客户端和服务器的 `deviceID` 可能不匹配 | 依赖客户端正确使用服务器返回的 deviceID |

---

## 2. 函数注册流程 (function_registration)

### 概述

客户端设备通过 `system.register_functions` RPC 向服务器声明其可调用函数。函数存储在按 `(userID, deviceID)` 键控的设备级注册表中。这是一个全量替换操作 — 每次调用覆盖之前的函数列表。

### 流程图

```mermaid
sequenceDiagram
    participant C as 客户端
    participant WS as WebSocket 服务
    participant RH as registerFunctionsHandler
    participant FR as MemoryFunctionRegistry

    C->>WS: system.register_functions RPC
    Note over C,WS: RegisterFunctionsParams<br/>functions[]<br/>可选 device_info

    WS->>RH: HandleRequest(ctx, params)
    RH->>RH: 解析 JSON 参数

    Note over RH: 使用认证的 client.DeviceID()<br/>覆盖客户端提供的 deviceID (D-093)

    RH->>FR: RegisterFunctions(ctx, userID, deviceID, functions, deviceInfo)

    FR->>FR: 验证函数数量 <= MaxFunctionsPerDevice (500)
    FR->>FR: 验证每个名称非空
    FR->>FR: 验证每个名称 <= MaxFunctionNameLength (255)
    FR->>FR: 验证无重复名称

    alt 验证失败
        FR-->>RH: ErrFunctionNameEmpty/ErrFunctionNameTooLong/ErrFunctionNameDuplicate/ErrMaxFunctionsPerDevice
        RH-->>C: 验证错误
    end

    FR->>FR: 深拷贝 Functions 切片和 DeviceInfo 映射
    FR->>FR: 存储 DeviceFunctions 记录
    Note over FR: devices[userID][deviceID]<br/>附带 RegisteredAt 时间戳

    FR-->>RH: 成功
    RH-->>C: {status: "ok", count: N, device_id: "..."}
```

### 详细步骤

1. **发送注册请求**
   - 客户端发送 `system.register_functions` RPC
   - 包含 `RegisterFunctionsParams`（`functions[]` 和可选的 `device_info`）

2. **解析请求参数**
   - 服务器的 `registerFunctionsHandler.HandleRequest` 从 JSON 解析参数

3. **覆盖设备 ID**
   - 处理器用经过认证的客户端的 `client.DeviceID()` 覆盖 `deviceID` (D-093)
   - 客户端提供的 `deviceID` 被忽略

4. **验证函数列表**
   - `MemoryFunctionRegistry.RegisterFunctions` 验证：
     - 函数数量 <= `MaxFunctionsPerDevice`（默认 500）
     - 每个名称非空
     - 每个名称 <= `MaxFunctionNameLength`（默认 255）
     - 无重复名称

5. **深拷贝数据**
   - 深拷贝 `Functions` 切片和 `DeviceInfo` 映射以防止调用者修改内部状态

6. **存储函数记录**
   - 在 `devices[userID][deviceID]` 中存储 `DeviceFunctions` 记录
   - 附带 `RegisteredAt` 时间戳

7. **返回结果**
   - 返回 `{status: "ok", count: N, device_id: "..."}`

### 边缘场景

| 场景 | 处理方式 | 设计决策 |
|------|---------|---------|
| 空函数列表 | 有效，清除设备之前注册的函数 | 全量替换语义 |
| 函数名称验证失败 | 返回对应的验证错误 | ErrFunctionNameEmpty/ErrFunctionNameTooLong/ErrFunctionNameDuplicate/ErrMaxFunctionsPerDevice |
| 同一设备的并发注册 | 在互斥锁下后写入者获胜 | 无冲突检测 |
| `FunctionRegistry` 为 nil | `system.register_functions` 处理器不会注册 | D-063：nil-safe 设计 |
| 客户端在参数中提供的 `deviceID` | 被静默忽略 | D-093：安全措施，连接的 deviceID 是权威的 |
| 重连后重新注册 | 自动调用 `reregisterFunctions` | 在 `system.reconnect` 之前执行，确保服务器在 PendingStore 重放之前有处理器 |

---

## 3. 断开连接时的函数注销流程 (function_deregistration_on_disconnect)

### 概述

当客户端设备断开连接时，其注册的函数从注册表中清理，以防止过时的函数引用。清理过程对设备替换是竞态安全的。

### 流程图

```mermaid
sequenceDiagram
    participant C as 客户端
    participant WS as WebSocket 服务
    participant CS as ConnectionStore
    participant FR as FunctionRegistry
    participant RR as ReverseRPC

    Note over C,WS: client.Run() 返回
    Note over C,WS: (连接正常关闭或出错)

    WS->>CS: ConnectionStore.Remove(cleanupCtx, connID)
    Note over CS: 超时 5 秒

    WS->>WS: removeClient(connID, userID, deviceID)
    Note over WS: 从三个本地映射移除<br/>clients、clientsByUser、clientsByDevice

    alt functionRegistry != nil
        WS->>WS: 检查 clientsByDevice[deviceKey]
        alt !hasActiveConn (该设备无其他连接)
            WS->>WS: scheduleFuncCleanup(userID, deviceID)
            Note over WS: 启动 grace period 定时器 (默认 10s)
            alt grace period 内设备重连
                WS->>WS: cancelPendingFuncCleanup(userID, deviceID)
                Note over WS: 取消清理, 函数保留
            else grace period 超时
                WS->>WS: 再次检查 hasActiveConn (竞态防护)
                alt 仍无活跃连接
                    WS->>FR: OnDeviceDisconnect(ctx, userID, deviceID)
                    FR->>FR: 从 devices[userID][deviceID] 移除
                    FR->>FR: 清理用户级映射条目 (如果无其他设备)
                end
            end
        end
    end

    Note over WS: 独立检查，不嵌套在函数清理中
    alt reverseRPC != nil 且 !hasActiveConn
        WS->>RR: CancelDeviceWithReason(userID, deviceID, "device disconnected")
        RR-->>WS: 挂起的反向 RPC 请求失败
    end
```

### 详细步骤

1. **连接断开触发**
   - WebSocket `client.Run()` 返回（连接正常关闭或出错）

2. **清理连接存储**
   - 服务器在延迟代码中运行清理：`ConnectionStore.Remove(cleanupCtx, connID)`
   - 超时 5 秒

3. **移除本地映射**
   - `removeClient(connID, userID, deviceID)` 从三个本地映射中移除：
     - `clients`
     - `clientsByUser`
     - `clientsByDevice`

4. **检查函数注册表**
   - 服务器检查 `functionRegistry != nil`
   - 查找 `clientsByDevice[deviceKey]`

5. **延迟清理函数注册**
   - 如果 `functionRegistry != nil` 且 `!hasActiveConn`（该设备无其他连接）：
     - 调用 `scheduleFuncCleanup(userID, deviceID)` 启动 grace period 定时器（默认 10 秒）
     - `scheduleFuncCleanup` 内部首先取消该设备已有的待处理清理（安全措施），然后创建新的 `context.Background()` 可取消 context
     - 注意：使用 `context.Background()` 而非调用者的 context，因为 `handleWebSocket` 返回时会取消其 context，这会立即取消延迟清理
     - 如果设备在 grace period 内重连，`cancelPendingFuncCleanup` 取消清理，函数注册保留
     - 如果 grace period 超时，goroutine 再次检查 `hasActiveConn`（竞态防护）
     - 如果仍无活跃连接，调用 `OnDeviceDisconnect(ctx, userID, deviceID)`
     - 从 `devices[userID][deviceID]` 中移除设备条目
     - 如果该用户不再有设备，清理用户级映射条目以防止内存泄漏
     - 清理完成后从 `pendingFuncCleanup` 映射中删除该条目

6. **取消挂起请求**（独立于函数清理）
   - 如果 `reverseRPC != nil` 且 `!hasActiveConn`：
     - `reverseRPC.CancelDeviceWithReason(userID, deviceID, "device disconnected")`
     - 使挂起的反向 RPC 请求失败

### 边缘场景

| 场景 | 处理方式 | 设计决策 |
|------|---------|---------|
| 设备替换（旧连接断开清理之前新连接已建立） | 新连接注册时调用 `cancelPendingFuncCleanup` 取消 grace period 定时器；若已超时则 `hasActiveConn` 为 true 跳过清理 | 双重保护：grace period 内重连取消清理 + `hasActiveConn` 防止替换连接的函数被删除的竞态 |
| 对未知设备调用 `OnDeviceDisconnect` | 幂等，返回 `(nil, nil)` | 无副作用 |
| `ConnectionStore.Remove` 期间 Redis 不可达 | 5 秒超时，错误被记录但清理继续 | 清理不因存储故障阻塞 |
| `functionRegistry` 为 nil | 整个函数清理块被跳过 | D-063：nil-safe 设计 |
| `reverseRPC` 为 nil | `CancelDeviceWithReason` 调用被跳过 | D-063：nil-safe 设计 |
| 设备替换时 `ConnectionStore.Remove` | `performDeviceReplacement` 不调用 `ConnectionStore.Remove`，由旧连接自身的 `handleWebSocket` 在 `client.Run()` 返回后执行 | 避免冗余 Redis 调用 |
| 服务器关闭 | `closeAllClients()` 关闭所有连接；每个客户端的 `handleWebSocket` defer 清理会触发 `scheduleFuncCleanup` | 函数清理被延迟（grace period），进程退出前可能未完成；`context.Background()` 保证清理不依赖已取消的服务器 context |
| 同一设备的多个连接 | 最后一次断开触发 `scheduleFuncCleanup` | D-095 替换逻辑后不应发生，但防御性处理 |
| Grace period 内设备重连 | `cancelPendingFuncCleanup` 取消定时器，函数保留 | 避免页面导航期间（短暂断开后立即重连）函数被误删 |
| Grace period 超时（默认 10s） | `OnDeviceDisconnect` 执行实际清理 | 双重检查 `hasActiveConn` 防止竞态 |
| `scheduleFuncCleanup` 使用 `context.Background()` | 延迟清理不依赖调用者 context | 防止 `handleWebSocket` 返回时 context 取消导致清理立即失效 |
| `scheduleFuncCleanup` 对同一设备重复调用 | 首先取消已有的待处理清理，再创建新的清理定时器 | 安全措施，防止并发断开场景下的重复清理 |

---

## 4. Agent 配置热重载流程 (agent_reload)

### 概述

从磁盘热重载代理配置，无需重启服务器。重新扫描 agents 目录并原子替换所有已加载的配置。

### 流程图

```mermaid
sequenceDiagram
    participant C as 客户端/CLI
    participant WS as WebSocket 服务
    participant RH as reloadAgentsHandler
    participant AR as AgentRegistry

    alt RPC 调用
        C->>WS: reload_agents RPC
    else CLI 调用
        C->>WS: xyncra-server reload-agents CLI
    end

    WS->>RH: HandleRequest(ctx, params)

    alt registry == nil
        RH-->>C: {count: 0} (D-063)
    end

    RH->>AR: Reload()
    AR->>AR: 获取读锁获取当前 dir
    AR->>AR: 调用 Load(dir)

    AR->>AR: 获取写锁
    AR->>AR: 清空 agents 映射
    AR->>AR: 读取目录条目

    loop 每个 .md 文件
        AR->>AR: 读取文件内容
        AR->>AR: ParseFrontMatter 提取 AgentConfig

        alt 无效文件
            Note over AR: 跳过：非 .md 文件、目录、<br/>不可读文件、无效前置数据、<br/>重复代理 ID
        else 有效文件
            AR->>AR: 存储 agents[config.ID]
        end
    end

    AR-->>RH: 返回已加载代理的数量
    RH-->>C: {count: N}
```

### 详细步骤

1. **触发重载**
   - 客户端发送 `reload_agents` RPC
   - 或服务器操作员运行 `xyncra-server reload-agents` CLI 命令

2. **检查注册表**
   - `reloadAgentsHandler.HandleRequest` 检查 `registry` 是否为 nil
   - 如果是，返回 `{count: 0}` (D-063 nil-safe)

3. **执行重载**
   - `AgentRegistry.Reload()` 获取读锁获取当前 `dir`
   - 调用 `Load(dir)`

4. **加载配置**
   - `Load` 获取写锁
   - 清空 `agents` 映射
   - 读取目录条目

5. **解析配置文件**
   - 对每个 `.md` 文件：
     - 读取内容
     - 调用 `ParseFrontMatter` 提取 `AgentConfig`

6. **过滤无效文件**
   - 跳过：非 `.md` 文件、目录、不可读文件（记录日志）、无效前置数据（记录日志）、重复代理 ID（记录日志）

7. **存储有效配置**
   - 将有效配置存储在 `agents[config.ID]` 中

8. **返回结果**
   - 返回已加载代理的数量

### 边缘场景

| 场景 | 处理方式 | 设计决策 |
|------|---------|---------|
| `registry` 为 nil | 返回 `{count: 0}` 且无错误 | D-063：nil-safe 设计 |
| 目录不存在 | `Load` 返回 nil | D-063：可选模块，agents 映射被清空，实际上卸载所有代理 |
| `dir` 为空字符串（从未加载） | `os.ReadDir("")` 失败 | 错误被包装并返回 |
| 跨文件的重复代理 ID | 先到先得 | 后续重复项被记录日志并跳过 |
| `.md` 文件中的无效前置数据 | 该文件被跳过 | 其他文件继续加载 |
| 并发 `Reload` 调用 | `Load` 中的写锁序列化它们 | 第二次调用完全覆盖第一次 |
| 代理配置在请求中被加载 | `IsAgent` 查找使用读锁 | 看到的要么是旧配置要么是新配置，永远不会是部分状态 |
| 文件读取错误（权限、I/O） | 单个文件被跳过并记录日志 | 其他文件继续 |
| `Load` 在加载前清空现有代理 | `agents` 映射为空的短暂窗口 | 在写锁下，短暂不一致 |

---

## 5. 动态工具注入流程 (dynamic_tool_injection)

### 概述

在每次代理执行之前，`DynamicToolProvider` 中间件动态注入客户端设备函数作为可调用工具。这弥合了客户端注册函数与代理工具执行模型之间的差距。

### 流程图

```mermaid
sequenceDiagram
    participant AE as Agent 执行器
    participant DTP as DynamicToolProvider
    participant FR as FunctionRegistry
    participant TR as ToolRegistry
    participant Agent as Agent 实例

    AE->>DTP: BeforeAgent(ctx, runCtx)

    DTP->>DTP: CallerDeviceFromContext(ctx)

    alt 设备上下文存在
        DTP->>FR: GetFunctions(ctx, device.UserID, device.DeviceID)
        FR-->>DTP: 函数列表

        DTP->>DTP: applyFilters(fn)
        Note over DTP: 1. 移除被排除的函数<br/>2. 应用标签过滤

        loop 每个过滤后的函数
            DTP->>DTP: newClientFunctionTool(fn, caller, userID, deviceID, timeout)
            Note over DTP: 创建 InvokableTool
        end
    end

    alt toolRegistry 和 dynamicTools 已配置
        DTP->>TR: Create(ctx, dynamicTools, nil)
        TR-->>DTP: 静态工具列表
    end

    DTP->>DTP: 追加工具到 runCtx.Tools
    DTP-->>AE: (ctx, runCtx, nil)

    Note over AE,Agent: 任何步骤的错误都被记录并跳过<br/>(fail-open, D-072)

    AE->>Agent: 使用注入的工具执行
```

### 详细步骤

1. **触发中间件**
   - 代理执行触发 `DynamicToolProvider.BeforeAgent(ctx, runCtx)` 中间件钩子

2. **提取设备上下文**
   - 提取 `CallerDeviceFromContext(ctx)` 获取调用设备的 `userID` 和 `deviceID`

3. **获取注册函数**
   - 如果设备上下文存在：调用 `funcRegistry.GetFunctions(ctx, device.UserID, device.DeviceID)`
   - 获取已注册函数

4. **过滤函数**
   - `applyFilters` 过滤函数：
     - 首先移除被排除的函数（通过 `ExcludedFunctions` 配置精确名称匹配）
     - 然后应用标签过滤（空 `FunctionTags` = 接受全部；非空 = 函数标签上的 OR 语义）

5. **创建工具实例**
   - 对每个过滤后的函数：`newClientFunctionTool(fn, caller, userID, deviceID, timeout)`
   - `buildToolInfo` 从 `FunctionInfo` 构建 `schema.ToolInfo`；如果 `Parameters` 为空，自动规范化为有效的 object schema（防止 OpenAI 兼容端点返回 400）
   - 创建一个 `InvokableTool`，当被代理调用时，向客户端设备发送 `ServerRequest`（反向 RPC）
   - `executeClientFunction` 支持每个函数的 `TimeoutMs` 覆盖默认超时；错误通过 `formatClientToolError` 映射为 LLM 友好消息（超时 → "request timed out"，设备离线 → "device is offline"，其他 → "unable to reach the device"），作为 soft failure 返回而非 Go error，让 LLM 可以自行重试或通知用户

6. **注入静态工具**
   - 如果 `toolRegistry` 和 `dynamicTools` 已配置：`toolRegistry.Create(ctx, dynamicTools, nil)`
   - 从内置注册表解析静态工具

7. **合并工具列表**
   - 所有注入的工具通过新的切片分配追加到 `runCtx.Tools`（防止别名）

8. **返回结果**
   - 返回 `(ctx, runCtx, nil)`
   - 任何步骤的错误都被记录并跳过（fail-open, D-072）

### 边缘场景

| 场景 | 处理方式 | 设计决策 |
|------|---------|---------|
| ctx 中无设备上下文 | 客户端函数注入完全跳过 | 仅注入基于注册表的动态工具（如果有） |
| `GetFunctions` 返回错误 | 记录日志，客户端函数被跳过 | 代理在没有它们的情况下继续 |
| `GetFunctions` 返回空列表 | 不注入客户端工具 | 代理仅使用其静态 + 动态注册表工具运行 |
| `newClientFunctionTool` 对特定函数失败 | 该函数被跳过 | 逐函数 fail-open，其他函数继续 |
| 所有函数被过滤掉 | 不注入客户端工具 | 无副作用 |
| `toolRegistry` 为 nil 或 `dynamicTools` 为空 | 跳过基于注册表的工具注入 | 无副作用 |
| 默认调用超时 | 如果 `config.CallTimeout` 未设置或 <= 0，则为 30 秒 | 合理的默认值 |
| 函数级 `TimeoutMs` 覆盖 | 如果 `FunctionInfo.TimeoutMs > 0`，使用函数级超时而非默认超时 | 支持不同函数的差异化超时 |
| `FunctionInfo.Parameters` 为空 | `buildToolInfo` 自动规范化为 `{type: "object", properties: {}}` | 防止 OpenAI 兼容端点将空 schema 转为 `parameters: true` 导致 400 错误 |
| `runCtx.Tools` 为 nil | 分配新切片 | 注入无论初始状态如何都能工作 |
| Eino 框架 0->非零工具转换 | 运行时从注册表解析的动态工具触发图重建 | Eino 架构要求 |
| 客户端函数调用超时 | `formatClientToolError` 返回 "request timed out" 作为 soft failure | LLM 可自行决定重试或通知用户 |
| 客户端设备离线 | `formatClientToolError` 返回 "device is offline" 作为 soft failure | LLM 可自行决定通知用户 |
| 客户端返回业务错误（`resp.Code < 0`） | 返回 "client returned error (code N): msg" 作为 soft failure | LLM 可看到具体错误原因并适配 |

---

## 6. 服务端处理器注册流程 (server_handler_registration)

### 概述

`RegisterAll` 在服务器启动时将所有 RPC 方法处理器连接到 `DefaultMessageHandler`。这是使上述所有流程可达的引导流程。

### 流程图

```mermaid
sequenceDiagram
    participant M as main.go
    participant RH as RegisterAll
    participant MH as DefaultMessageHandler
    participant Deps as Dependencies

    M->>M: 创建 Dependencies 结构体
    Note over M: 包含所有必需存储、<br/>注册表和代理

    M->>RH: RegisterAll(handler, deps)

    RH->>MH: RegisterMethod("heartbeat", handler)
    RH->>MH: RegisterMethod("send_message", handler)
    RH->>MH: RegisterMethod("create_conversation", handler)
    RH->>MH: RegisterMethod("get_messages", handler)
    RH->>MH: RegisterMethod("agent_resume", handler)
    Note over RH,MH: ... 共 16 个无条件方法处理器

    alt deps.FunctionRegistry != nil
        RH->>MH: RegisterMethod("system.register_functions", handler)
    end

    alt deps.ReverseRPC != nil && deps.ReverseRPC.PendingStore() != nil
        RH->>MH: RegisterMethod("system.reconnect", handler)
    end

    Note over MH: 处理器被存储在方法分发映射中
```

### 详细步骤

1. **创建依赖结构体**
   - `main.go` 创建包含所有必需存储、注册表和代理的 `Dependencies` 结构体

2. **注册所有处理器**
   - 调用 `RegisterAll(handler, deps)` 注册 16 个无条件方法处理器 + 2 个条件注册（共 18 个）
   - 无条件方法包括：`heartbeat`、`send_message`、`sync_updates`、`create_conversation`、`list_conversations`、`get_messages`、`search_messages`、`get_conversation`、`delete_conversation`、`restore_conversation`、`delete_message`、`mark_as_read`、`set_typing`、`stream_text`、`reload_agents`、`agent_resume`
   - 条件注册：`system.register_functions`（需 `FunctionRegistry`）、`system.reconnect`（需 `ReverseRPC` 和 `PendingStore`）

3. **存储处理器**
   - 每个 `RegisterMethod(methodName, handler)` 将处理器存储在 `DefaultMessageHandler` 的方法分发映射中

4. **条件注册**
   - 仅在 `deps.FunctionRegistry != nil` 时注册 `system.register_functions`
   - 仅在 `deps.ReverseRPC != nil` 且 `deps.ReverseRPC.PendingStore() != nil` 时注册 `system.reconnect`

### 边缘场景

| 场景 | 处理方式 | 设计决策 |
|------|---------|---------|
| `FunctionRegistry` 为 nil | `system.register_functions` 未注册 | 客户端对此方法的 RPC 返回 method not found |
| `AgentRegistry` 为 nil | `reload_agents` 处理器仍被注册（返回 `{count: 0}`） | 但 `send_message` 跳过代理检测 |
| `ReverseRPC` 为 nil 或 `PendingStore` 为 nil | `system.reconnect` 未注册 | 无法处理断线重连 |
| 同一方法的重复注册 | 后注册者覆盖先注册者 | 映射语义 |
| `RegisterAll` 在 `handler.Start()` 之前调用 | 处理器被存储但尚未分发 | 无竞态 |

---

## 流程间依赖关系

```mermaid
graph TD
    A[6. 服务端处理器注册] --> B[1. 客户端连接注册]
    B --> C[2. 函数注册]
    B --> D[3. 函数注销]
    C --> E[5. 动态工具注入]
    F[4. Agent 配置热重载] --> E

    B -->|performReconnectHandshake| C
    B -->|连接断开| D
    C -->|函数列表| E
    F -->|代理配置| E

    style A fill:#ffcdd2
    style B fill:#c8e6c9
    style C fill:#c8e6c9
    style D fill:#c8e6c9
    style E fill:#bbdefb
    style F fill:#bbdefb
```

### 依赖说明

1. **服务端处理器注册** 是基础
   - 必须在服务器启动时完成
   - 决定了哪些 RPC 方法可用

2. **客户端连接注册** 依赖处理器注册
   - 需要 `handleWebSocket` 处理器已注册
   - 是后续所有流程的前提

3. **函数注册** 依赖客户端连接
   - 客户端必须先建立连接
   - 在 `performReconnectHandshake` 中自动触发

4. **函数注销** 依赖客户端连接
   - 当连接断开时自动触发
   - 清理过时的函数引用

5. **动态工具注入** 依赖函数注册和 Agent 配置
   - 从 `FunctionRegistry` 获取已注册函数
   - 从 `AgentRegistry` 获取代理配置

6. **Agent 配置热重载** 独立于其他流程
   - 可随时触发
   - 影响后续的动态工具注入

---

## 关键设计决策

| 编号 | 决策 | 理由 |
|------|------|------|
| D-063 | nil-safe 可选模块设计 | Agent、FunctionRegistry 等组件可选注入，nil 即禁用，避免强制依赖 |
| D-093 | 客户端提供的 deviceID 被忽略 | 安全措施，防止客户端伪造设备身份 |
| D-094 | 服务器自动生成 UUID v4 作为 deviceID | 确保每个连接有唯一标识 |
| D-095 | 设备替换检测和处理 | 支持同一设备的重新连接，避免僵尸连接 |
| D-072 | fail-open 错误处理策略 | 动态工具注入失败不应阻塞 Agent 执行 |
| D-101 | 注册的函数自动注入为 Agent 工具 | 弥合客户端函数与 Agent 工具执行模型的差距 |
| D-111 | 4001 关闭码触发优雅退出 | 设备被替换时，旧客户端应优雅退出而非重连 |
| D-103 | 超时请求持久化到 PendingStore | 支持断线重连后的请求重放 |

---

## 相关文档

- [业务流程索引](./index.md)
- [系统架构概览](../architecture/system-architecture.md)
- [协议设计](../architecture/protocol-design.md)
- [组件关系](../architecture/component-relationships.md)
- [WebSocket 连接管理](./websocket-connection.md) -- 更详细的连接建立、设备替换、心跳、断开清理、优雅关闭流程
- [函数注册与动态工具](./function-registry.md) -- 更详细的函数注册/注销、动态工具注入、反向 RPC 调用流程
- [断线重连与同步](./reconnection.md) -- 客户端重连协调、增量同步、心跳保活的完整流程
