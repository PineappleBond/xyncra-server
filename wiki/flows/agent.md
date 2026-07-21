# Agent 执行引擎业务流程

本文档描述 Xyncra Agent 执行引擎的核心业务流程，包括完整执行管线、HITL 交互、流式输出、子代理委托、动态工具注入等关键流程。

---

## 目录

1. [Agent 完整执行流程](#1-agent-完整执行流程)
2. [HITL 交互流程](#2-hitl-交互流程)
3. [HITL 超时清理流程](#3-hitl-超时清理流程)
4. [流式输出流程](#4-流式输出流程)
5. [子代理委托流程](#5-子代理委托流程)
6. [动态工具注入流程](#6-动态工具注入流程)
7. [Agent 配置注册流程](#7-agent-配置注册流程)
8. [广播推送流程](#8-广播推送流程)
9. [并发控制机制](#9-并发控制机制)
10. [幂等控制机制](#10-幂等控制机制)
11. [Agent Resume 流程](#11-agent-resume-流程)

---

## 1. Agent 完整执行流程

从 MQ 消费任务消息开始，经过并发控制、幂等检查、会话锁、上下文加载、Agent 构建、LLM 流式调用、广播推送、消息持久化的完整执行管线。

### 流程图

```mermaid
flowchart TD
    A[MQ TaskHandler 接收 TypeAgentProcess 任务] --> B[反序列化 AgentProcessPayload]
    B --> C{校验必填字段}
    C -->|缺少字段| C1[返回错误]
    C -->|校验通过| D[获取分布式会话锁 ConversationLock.Acquire]

    D -->|锁被占用| D1[返回 error, Asynq 指数退避重试]
    D -->|获取成功| E[两阶段幂等检查]

    E --> E1{Phase 1: agent:processed:MessageID?}
    E1 -->|已处理| E2[静默跳过, 释放锁, 返回 nil]
    E1 -->|未处理| E3{Phase 2: agent:processing:MessageID?}
    E3 -->|处理中| E2
    E3 -->|未处理| F[标记 processing, TTL=130s]

    F --> G[调用 executor.ExecuteWithErrorMessage]
    G --> H[注入 CallerDevice 到 context]
    H --> I[获取信号量 sem.Acquire]
    I -->|context 取消| I1[返回 ctx.Err]
    I -->|获取成功| J[创建 context, totalTimeout=120s]

    J --> K[从 AgentRegistry 查找 agent 配置]
    K -->|未找到| K1[返回 ErrAgentNotFound, 发送友好错误消息]
    K -->|找到| L[发送 typing=true 广播]

    L --> M[启动 typing timeout goroutine, 60s]
    M --> N[ContextManager 加载对话历史]
    N --> O[AgentBuilder.Build 构建 Agent]

    O --> P[convertMessages 转换消息格式]
    P --> Q[生成 streamID 和 checkpointID]
    Q --> R[Runner.Run 启动 LLM 流式推理]

    R --> S[StreamBridge.BridgeWithInterrupt 消费流式输出]
    S --> T{循环消费 chunk}

    T -->|首次 token| T1[清除 typing, 广播累积文本快照]
    T -->|后续 chunk| T2[广播累积文本快照]
    T -->|HITL interrupt| T3[走 HITL 流程, 返回 ErrHITLInterrupted]
    T -->|流结束| U[发送 is_done=true 广播]

    U --> V[持久化最终消息到 DB]
    V --> W[广播 MessageUpdate]
    W --> X[标记 processed, TTL=24h, 清理 processing key]
    X --> Y[释放会话锁]
    Y --> Z[失效上下文缓存]

    style A fill:#4CAF50,color:#fff
    style E2 fill:#FF9800,color:#fff
    style K1 fill:#f44336,color:#fff
    style I1 fill:#f44336,color:#fff
    style D1 fill:#FF9800,color:#fff
    style T3 fill:#2196F3,color:#fff
    style Z fill:#4CAF50,color:#fff
```

### 边缘场景

| 场景 | 处理方式 |
|------|----------|
| **信号量满** | Acquire 阻塞等待，context 取消时返回 `ctx.Err()` |
| **会话锁被占用** | 返回 error，由 Asynq 以指数退避重试 |
| **幂等命中** | 静默跳过，释放锁，返回 nil |
| **Agent 未注册** | 返回 `ErrAgentNotFound`，走 `classifyError` 发送友好错误消息 |
| **LLM 超时** | 映射为 `ErrLLMTimeout`，发送"暂时无法回复"消息，作为 transient error 返回给 MQ 重试 |
| **LLM 限流 (429)** | 映射为 `ErrLLMRateLimited`，同上 |
| **HTTP 500/502/503** | 映射为 `ErrLLMTimeout` (transient) |
| **流式中途错误** | 广播 `is_done=true` + 已累积的部分文本，持久化部分消息 |
| **持久化失败** | 流式中途错误时部分文本持久化为 fail-open；最终消息持久化失败时返回错误，触发 `ExecuteWithErrorMessage` 发送通用错误消息给用户 |
| **API Key 缺失** | 返回 `ErrAPIKeyMissing`，发送"配置有误"消息 |
| **MCP 服务不可用** | 跳过该 MCP server 的工具 (fail-open)，不阻断构建 |
| **context 超时** | 总超时 120s 后所有操作被取消 |

---

## 2. HITL 交互流程

Agent 在执行过程中通过 Eino 框架的 interrupt 机制暂停，等待用户回答后通过 resume 恢复执行。支持多轮 HITL（re-interrupt）。

### 流程图

```mermaid
flowchart TD
    subgraph interrupt_phase [Interrupt 阶段]
        A[Agent 调用 ask_user 工具] --> B[Eino 框架触发 interrupt]
        B --> C[StreamBridge 检测到 Action.Interrupted]
        C --> D[提取 InterruptInfo: Question + InterruptID]
        D --> E[Executor 收到 interruptCh 信号]
        E --> F[更新 Conversation 状态为 asking_user]
        F --> G[写入 checkpointID]
        G --> H[持久化 Question 到 DB]
        H --> I[关闭流式输出, is_done=true]
        I --> J[广播 conversation_update]
        J --> K[广播 agent_status = asking_user]
        K --> L[返回 ErrHITLInterrupted, 不释放会话锁]
    end

    subgraph resume_phase [Resume 阶段]
        M[用户回答] --> N[客户端调用 agent_resume RPC]
        N --> O[NewAgentResumeHandler 处理]
        O --> P[反序列化 AgentResumePayload]
        P --> Q[两阶段幂等检查]
        Q --> R[获取会话锁]
        R --> S[从 DB 读取已回答 Questions]
        S --> T[构建 targets map]
        T --> U[AgentBuilder.Build 重新构建 Agent]
        U --> V[Runner.ResumeWithParams 恢复执行]
        V --> W[桥接流式输出, 广播结果]
        W --> X[持久化最终消息]
        X --> Y[清理状态]
        Y --> Y1[ClearAgentStatus]
        Y1 --> Y2[Delete Questions]
        Y2 --> Y3[Delete Redis checkpoint]
        Y3 --> Z[广播 conversation_update]
        Z --> AA[释放会话锁]
    end

    L -.->|等待用户回答| M

    style A fill:#4CAF50,color:#fff
    style L fill:#FF9800,color:#fff
    style M fill:#2196F3,color:#fff
    style AA fill:#4CAF50,color:#fff
```

### 边缘场景

| 场景 | 处理方式 |
|------|----------|
| **checkpoint 过期/丢失** | 调用 `cleanupAfterResumeFailure`，发送"等待时间过长"消息 |
| **re-interrupt (多轮HITL)** | resume 后再次 interrupt，重新进入 `asking_user` 状态，不释放锁，删除 processing key 允许后续 resume |
| **resume 永久失败** | 清理状态 + 删除 checkpoint + 删除 questions + 发送错误消息 |
| **resume transient 失败** | 不自动 MQ 重试，而是通知用户自行决定是否重试 |
| **并发 resume** | 幂等检查确保同一 checkpoint 只 resume 一次 |
| **questionStore 为 nil** | 初始中断时 Question 创建被跳过 (nil-safe)；resume 时中止恢复流程并释放锁（非 nil-safe，需配置 questionStore） |

---

## 3. HITL 超时清理流程

后台定时任务扫描停留在 `asking_user` 状态的过期会话并清理资源。

### 流程图

```mermaid
flowchart TD
    A[HITLCleanupTask.Run 启动 ticker, 5min 间隔] --> B{每次 tick}
    B --> C[cleanupOnce]
    C --> D[查询 asking_user 状态且超过 MaxAge=24h 的会话]
    D --> E{遍历每个过期会话}

    E --> F[获取分布式锁 hitl:cleanup:ConversationID]
    F -->|获取失败| G[跳过, 其他节点处理中]
    F -->|获取成功| H{重新检查会话状态}

    H -->|状态已变更| I[跳过]
    H -->|仍为 asking_user| J[ClearAgentStatus 重置为 idle]
    J --> K[DeleteByCheckpoint 软删除 Questions]
    K --> L[删除 Redis checkpoint]
    L --> M[发送超时提示消息]
    M --> N[广播 agent_timeout + conversation_update]

    style A fill:#4CAF50,color:#fff
    style G fill:#FF9800,color:#fff
    style I fill:#FF9800,color:#fff
    style N fill:#2196F3,color:#fff
```

### 边缘场景

| 场景 | 处理方式 |
|------|----------|
| **并发清理** | 分布式锁确保同一会话只被一个节点处理 |
| **状态已变更** | re-check 发现不再是 `asking_user` 则跳过 |
| **单个会话 panic** | 每个会话独立 recover，不影响批次中其他会话 |
| **全局 panic** | `cleanupOnce` 外层也有 recover，不崩溃后台 goroutine |

---

## 4. 流式输出流程

将 Eino 的 AsyncIterator 流式输出转换为 Xyncra 的 StreamChunk，通过 50ms 节流控制帧率，累积文本快照推送给客户端。

### 流程图

```mermaid
flowchart TD
    subgraph producer [生产者 goroutine]
        A[消费 Eino AsyncIterator] --> B[iter.Next 获取 AgentEvent]
        B -->|context 取消| B1[flush buffer, 返回]
        B -->|获取成功| C{跳过 schema.Tool 角色?}
        C -->|是| A
        C -->|否| D{检测 HITL interrupt?}
        D -->|是| D1[发送 interrupt 信号]
        D -->|否| E{消息类型}
        E -->|流式 delta| F[发送文本到 textCh, buffer=64]
        E -->|非流式| G[发送完整文本到 textCh]
    end

    subgraph throttler [节流主循环]
        H[50ms ticker] --> I{收到文本 delta?}
        I -->|是| J[追加到 buffer]
        I -->|否| K{ticker 触发?}
        K -->|是| L{buffer 非空?}
        L -->|是| M[发送累积快照 StreamChunk]
        L -->|否| N[跳过]
        K -->|否| O{收到 done/err/interrupt?}
        O -->|是| P[flush buffer, 发送终态信号]
    end

    subgraph consumer [Executor 消费者]
        Q[消费 chunkCh] --> R{首次收到 content?}
        R -->|是| S[清除 typing indicator]
        R -->|否| T{IsDone?}
        S --> T
        T -->|否| U[广播 SendStreamUpdate 累积快照]
        U --> Q
        T -->|是| V[发送 is_done=true]
        V --> W[持久化最终消息]
    end

    F --> H
    M --> Q
    P --> Q

    style A fill:#4CAF50,color:#fff
    style D1 fill:#f44336,color:#fff
    style M fill:#2196F3,color:#fff
    style W fill:#4CAF50,color:#fff
```

### 边缘场景

| 场景 | 处理方式 |
|------|----------|
| **context 取消** | flush 剩余 buffer 后返回 |
| **iter.Next 阻塞** | 通过 goroutine + select 包装使其可取消 |
| **textCh 满 (64)** | 生产者阻塞，主循环 ticker 继续发送已累积的快照 |
| **流式中途错误** | 广播 `is_done` + 部分文本，持久化部分消息 |
| **无文本输出** | buffer 为空时不发送空 chunk |

---

## 5. 子代理委托流程

父 Agent 通过配置声明子 Agent，构建时将子 Agent 包装为 Eino AgentTool，作为普通工具注入父 Agent 的工具列表。

### 流程图

```mermaid
flowchart TD
    A[AgentBuilder.Build] --> B{config.SubAgents 非空?}
    B -->|否| Z[跳过, 继续构建]
    B -->|是| C[调用 resolveSubAgents]
    C --> D[遍历每个 subID]

    D --> E[从 AgentRegistry 查找子 Agent 配置]
    E -->|未找到| F[日志警告, 跳过]
    E -->|找到| G[深拷贝子配置]
    G --> H[清除 childConfig.SubAgents]
    H --> I[递归调用 AgentBuilder.Build]

    I -->|构建失败| J[记录错误, 跳过该子 Agent]
    I -->|构建成功| K[adk.NewAgentTool 包装为 tool]
    K --> L[追加到父 Agent 工具列表]

    D -->|遍历完成| M[返回所有子 Agent 工具]

    style A fill:#4CAF50,color:#fff
    style F fill:#FF9800,color:#fff
    style J fill:#f44336,color:#fff
    style M fill:#4CAF50,color:#fff
```

### 边缘场景

| 场景 | 处理方式 |
|------|----------|
| **子 Agent 未注册** | 日志警告并跳过 (fail-open) |
| **子 Agent 构建失败** | 记录错误，跳过该子 Agent，其他子 Agent 继续构建 |
| **递归深度** | 通过清除 `childConfig.SubAgents` 硬限制为 1 层 |
| **子 Agent 无 Name/Description** | 由 `AgentConfig.Validate` 保证非空 |

---

## 6. 动态工具注入流程

通过 Eino 中间件在每次 Agent 运行前动态注入客户端设备函数和注册表工具。两条注入路径独立：客户端工具依赖设备上下文，注册表工具不依赖。

### 流程图

```mermaid
flowchart TD
    A[DynamicToolProvider.BeforeAgent 中间件执行] --> B[初始化 merged 工具列表]

    subgraph client_path [客户端函数路径]
        C[从 context 提取 CallerDevice] --> D{设备信息存在?}
        D -->|否| D1[跳过客户端函数注入]
        D -->|是| E[ClientFunctionProvider.GetFunctions]
        E -->|失败| E1[日志跳过, fail-open]
        E -->|成功| F[applyFilters 过滤]
        F --> F1[排除 ExcludedFunctions]
        F1 --> F2[按 FunctionTags 过滤, OR 语义]
        F2 --> G[遍历每个函数]
        G --> H[创建 newClientFunctionTool]
        H --> H1[从 JSON Schema 构建 ToolInfo]
        H1 --> H2[utils.NewTool 包装为 InvokableTool]
        H2 --> H3[InvokableRun 调用 ClientCaller.ServerRequest]
        H3 --> I[追加到 merged 列表]
    end

    subgraph registry_path [注册表工具路径]
        J{toolRegistry 和 dynamicTools 非空?} -->|否| J1[跳过]
        J -->|是| K[toolRegistry.Create 实例化工具]
        K --> L[追加到 merged 列表]
    end

    B --> C
    B --> J
    I --> M[分配新 slice, 合并 runCtx.Tools + merged]
    L --> M

    style A fill:#4CAF50,color:#fff
    style D1 fill:#FF9800,color:#fff
    style E1 fill:#FF9800,color:#fff
    style J1 fill:#FF9800,color:#fff
    style M fill:#2196F3,color:#fff
```

### 边缘场景

| 场景 | 处理方式 |
|------|----------|
| **无设备上下文** | 跳过客户端函数注入，注册表工具仍可注入 |
| **GetFunctions 失败** | 日志跳过 (fail-open) |
| **单个函数创建失败** | 跳过该函数，其他函数继续 |
| **设备离线** | SoftFailure 返回 "device is offline"，LLM 可感知并调整 |
| **请求超时** | SoftFailure 返回 "request timed out" |
| **客户端返回业务错误** | SoftFailure 返回错误码和消息 |
| **空参数 schema** | 自动补全为 `{"type":"object","properties":{}}`，防止 LLM 格式层产生非法 schema |

---

## 7. Agent 配置注册流程

从 `.md` 文件目录加载 Agent 配置（front matter + system prompt），支持热重载。

### 流程图

```mermaid
flowchart TD
    A[AgentRegistry.Load dir] --> B{目录存在?}
    B -->|否| B1[返回 nil, 可选模块]
    B -->|是| C[清空现有 agents map]
    C --> D[扫描目录下 .md 文件]
    D --> E[遍历每个文件]

    E --> F[ParseFrontMatter 解析]
    F --> G[YAML front matter + Markdown body]
    G --> H{ID 是否重复?}
    H -->|是| H1[日志警告, 保留先加载的]
    H -->|否| I[写入 agents config.ID = config]

    E -->|遍历完成| J[注册完成]

    K[Get id / IsAgent userID] --> L[精确匹配查找]

    M[Reload] --> N[重新扫描同一目录]

    style A fill:#4CAF50,color:#fff
    style B1 fill:#FF9800,color:#fff
    style H1 fill:#FF9800,color:#fff
    style J fill:#4CAF50,color:#fff
```

### 边缘场景

| 场景 | 处理方式 |
|------|----------|
| **目录不存在** | 返回 nil (可选模块) |
| **无效配置** | 日志跳过，不中断其他文件加载 |
| **重复 ID** | 日志警告，保留先加载的 |
| **并发访问** | RWMutex 保护所有读写操作 |

---

## 8. 广播推送流程

通过 WebSocket 向用户推送实时更新，所有广播均为 fire-and-forget。

### 流程图

```mermaid
flowchart TD
    A[广播事件触发] --> B{事件类型}

    B -->|流式文本| C[SendStreamUpdate]
    C --> C1[Seq=0, ephemeral]
    C1 --> C2[推送给 humanUser]
    C2 --> C3[推送给 agentUser]

    B -->|输入状态| D[SendTyping]

    B -->|Agent 状态| E[SendAgentStatus]
    E --> E1[thinking / tool_calling / generating / idle / asking_user]

    B -->|超时通知| F[SendAgentTimeout]

    B -->|函数调用| F2[SendFunctionCall]
    F2 --> F3[两次调用: IsDone=false 前 / IsDone=true 后]
    F3 --> F4[推送给 humanUser]

    B -->|会话更新| G[SendConversationUpdate]
    G --> G1[pull notification pattern]

    B -->|持久化消息| H[BroadcastMessageUpdate]
    H --> H1[带 DB 分配的 Seq]

    C3 --> I[WebSocket 发送]
    D --> I
    E1 --> I
    F --> I
    F4 --> I
    G1 --> I
    H1 --> I

    I -->|失败| J[日志记录, 不返回错误]
    I -->|成功| K[发送完成]

    style A fill:#4CAF50,color:#fff
    style J fill:#FF9800,color:#fff
    style K fill:#4CAF50,color:#fff
```

### 边缘场景

| 场景 | 处理方式 |
|------|----------|
| **WebSocket 发送失败** | 日志记录但不返回错误 (fire-and-forget) |
| **JSON 序列化失败** | 日志记录并跳过 |
| **registry 为 nil** | `isAgent` 始终返回 false (nil-safe) |
| **函数调用广播** | `SendFunctionCall` 由 LoggingMiddleware 调用，每次函数调用发送两次：执行前 (`IsDone=false`, 携带 name + args) 和执行后 (`IsDone=true`, 携带 result 或 error) |

---

## 9. 并发控制机制

通过信号量限制 Agent 并发执行数，通过分布式会话锁保证同一会话串行执行。

### 流程图

```mermaid
flowchart TD
    subgraph semaphore [信号量控制]
        A[sem.Acquire] --> B{capacity <= 0?}
        B -->|是| C[立即返回, 无限并发]
        B -->|否| D[基于 buffered channel 获取]
        D --> E{context 取消?}
        E -->|是| F[返回 ctx.Err]
        E -->|否| G[获取成功]
        G --> H[更新 active/peak/totalAcquired 指标]
    end

    subgraph session_lock [会话锁控制]
        I[ConversationLock.Acquire] --> J[Redis SETNX, TTL=130s]
        J --> K{获取成功?}
        K -->|是| L[唯一 token 保证锁归属]
        K -->|否| M[返回 error, 由 MQ 重试]

        N[ConversationLock.Release] --> O[Lua 脚本原子化]
        O --> O1[检查 token]
        O1 --> O2{是 owner?}
        O2 -->|是| O3[DEL key]
        O2 -->|否| O4[忽略, 非 owner]
    end

    subgraph hitl_lock [HITL 锁保持]
        P[HITL interrupt] --> Q[不释放会话锁]
        Q --> R[等待 resume]
    end

    style A fill:#4CAF50,color:#fff
    style F fill:#f44336,color:#fff
    style M fill:#FF9800,color:#fff
    style Q fill:#2196F3,color:#fff
```

### 边缘场景

| 场景 | 处理方式 |
|------|----------|
| **信号量 nil** | Acquire/Release 均为 no-op |
| **Redis 不可用** | 会话锁获取失败时 fail-open，继续执行 |
| **锁过期 (TTL)** | 自然释放，不阻塞后续任务 |
| **非 owner 释放锁** | Lua 脚本检查 token，非 owner 的 DEL 被忽略 |

---

## 10. 幂等控制机制

两阶段幂等检查防止重复处理同一消息。Phase 1 防重放，Phase 2 防并发。Resume 路径使用相同的两阶段机制，但 key 格式不同。

### 流程图

```mermaid
flowchart TD
    A[幂等检查开始] --> B[Phase 1: 检查 agent:processed:MessageID]
    B --> C{已完全处理?}
    C -->|是| D[跳过, 返回 nil]
    C -->|否| E{Redis 查询失败?}
    E -->|是| F[fail-open, 继续]
    E -->|否| G[Phase 2: 检查 agent:processing:MessageID]

    G --> H{正在处理中?}
    H -->|是| D
    H -->|否| I{Redis 查询失败?}
    I -->|是| J[fail-open, 继续]
    I -->|否| K[标记 processing, TTL=130s]

    K --> L[执行任务]

    L --> M{执行结果}
    M -->|成功| N[标记 processed, TTL=24h]
    N --> O[删除 processing key]

    M -->|HITL 中断| P[不标记 processed]
    P --> Q[保留 processing key 自然过期]

    M -->|永久失败| R[标记 processed 防止重试]

    M -->|transient 失败| S[不标记 processed, 允许 MQ 重试]

    style A fill:#4CAF50,color:#fff
    style D fill:#FF9800,color:#fff
    style F fill:#FF9800,color:#fff
    style J fill:#FF9800,color:#fff
    style N fill:#4CAF50,color:#fff
    style P fill:#2196F3,color:#fff
    style R fill:#f44336,color:#fff
    style S fill:#FF9800,color:#fff
```

### 边缘场景

| 场景 | 处理方式 |
|------|----------|
| **Redis 不可用** | 幂等检查失败时继续执行 (fail-open) |
| **processing key 过期但任务仍在执行** | 新任务可能并发进入，被会话锁拦截 |
| **processed 和 processing key 都存在** | 跳过 (某处异常导致) |
| **Resume 路径 key 格式** | Phase 1: `agent:resume:CheckpointID`, Phase 2: `agent:resume:processing:CheckpointID`。成功后标记 processed (24h) 并删除 processing key；transient 失败时删除 processing key 允许立即重试；re-interrupt 时删除 processing key 允许后续 resume |
| **Agent 执行 vs Resume key 差异** | Agent 执行: `agent:processed:{MessageID}` / `agent:processing:{MessageID}`；Resume: `agent:resume:{CheckpointID}` / `agent:resume:processing:{CheckpointID}`。两套 key 独立，互不干扰 |

---

## 11. Agent Resume 流程

处理 HITL 恢复任务，从 DB 读取用户答案，通过 Eino 的 `ResumeWithParams` 恢复被中断的 Agent 执行。

### 流程图

```mermaid
flowchart TD
    A[agent_resume RPC 调用] --> B[反序列化 AgentResumePayload]
    B --> C[两阶段幂等检查 agent:resume:CheckpointID]
    C -->|已 resume| C1[跳过, 返回 nil]
    C -->|未 resume| D[获取会话锁]

    D -->|Redis 错误| D1[fail-open, 继续]
    D -->|锁被占用 (HITL 预期)| D2[weOwnLock=false, 继续]
    D -->|获取成功| E[从 DB 读取已回答 Questions]

    E --> F[构建 targets map]
    F --> G[AgentBuilder.Build 重新构建 Agent]

    G -->|构建失败| G1[cleanupAfterResumeFailure]
    G1 --> G2[发送错误消息 + 清理幂等 key]
    G -->|构建成功| H[Runner.ResumeWithParams 恢复执行]

    H --> I[桥接流式输出]
    I --> J[广播结果]
    J --> K[持久化最终消息]

    K --> L[清理状态]
    L --> L1[ClearAgentStatus]
    L1 --> L2[Delete Questions]
    L2 --> L3[Delete Redis checkpoint]

    L3 --> M[标记 processed (24h) + 删除 processing key]
    M --> M1[广播 conversation_update]
    M1 --> N[释放会话锁]

    H -->|再次 interrupt| O[重新进入 asking_user 状态]
    O --> O1[不释放锁]
    O1 --> O2[删除 processing key 允许后续 resume]

    style A fill:#4CAF50,color:#fff
    style C1 fill:#FF9800,color:#fff
    style D1 fill:#FF9800,color:#fff
    style D2 fill:#FF9800,color:#fff
    style G2 fill:#f44336,color:#fff
    style N fill:#4CAF50,color:#fff
    style O fill:#2196F3,color:#fff
```

### 边缘场景

| 场景 | 处理方式 |
|------|----------|
| **Resume 路径的 transient 错误** | 不自动 MQ 重试，而是发送"服务暂时不可用"消息通知用户，同时删除 processing key 允许立即重试。用户已投入交互成本，应自行决定是否重试 |
| **Eino 的 resume 路径** | 会用原 checkpointID 保存 re-interrupt 的 checkpoint |
| **成功 resume 后** | 执行完整清理：ClearAgentStatus + Delete Questions + Delete Redis checkpoint |
| **checkpoint 过期/丢失** | 调用 `cleanupAfterResumeFailure`，发送超时提示消息 |
| **并发 resume** | 幂等检查确保同一 checkpoint 只 resume 一次 |

---

## 关键组件关系

```mermaid
graph LR
    MQ[MQ TaskHandler] --> Executor[Agent Executor]
    Executor --> Lock[ConversationLock]
    Executor --> Idempotency[Idempotency Check]
    Executor --> Semaphore[Semaphore]
    Executor --> Registry[Agent Registry]
    Executor --> Builder[Agent Builder]
    Executor --> Context[Context Manager]
    Executor --> Broadcast[Broadcast]
    Executor --> DB[(Database)]
    Executor --> Redis[(Redis)]

    Builder --> SubAgents[Sub Agents]
    Builder --> Tools[Dynamic Tools]
    Builder --> MCP[MCP Servers]

    Tools --> ClientFunc[Client Functions]
    Tools --> RegistryTools[Registry Tools]

    Executor --> StreamBridge[Stream Bridge]
    StreamBridge --> Eino[Eino Runner]
    Eino --> LLM[LLM API]

    Executor --> HITL[HITL Cleanup]

    style MQ fill:#4CAF50,color:#fff
    style Executor fill:#2196F3,color:#fff
    style LLM fill:#f44336,color:#fff
    style DB fill:#FF9800,color:#fff
    style Redis fill:#FF9800,color:#fff
```

---

## 错误分类与处理策略

| 错误类型 | 分类 | 处理策略 |
|----------|------|----------|
| `ErrLLMTimeout` | Transient | MQ 重试 + 用户提示"暂时无法回复" |
| `ErrLLMRateLimited` | Transient | MQ 重试 + 用户提示"暂时无法回复" |
| HTTP 500/502/503 | Transient | MQ 重试 |
| `ErrAgentNotFound` | Permanent | 发送友好错误消息，标记 processed |
| `ErrAPIKeyMissing` | Permanent | 发送"配置有误"消息，标记 processed |
| `ErrHITLInterrupted` | Special | 不标记 processed，保留锁，等待 resume |
| 流式中途持久化失败 | Fail-open | 日志记录，不阻断主流程（部分文本已广播） |
| 最终消息持久化失败 | Permanent | 返回错误，发送通用错误消息给用户，标记 processed |
| MCP 不可用 | Fail-open | 跳过该 MCP server，不阻断构建 |
