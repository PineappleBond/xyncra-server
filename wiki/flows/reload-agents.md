# Reload Agents 业务流程

本文档描述 `reload_agents` RPC 方法的完整业务流程，包括主流程、边缘场景和依赖关系。

---

## 目录

- [概述](#概述)
- [主流程](#主流程)
- [边缘场景](#边缘场景)
- [依赖关系](#依赖关系)
- [关键设计决策](#关键设计决策)

---

## 概述

`reload_agents` 是一个运维 RPC 方法，用于在运行时热重载 Agent 配置。它会重新扫描磁盘上的 agents 目录，加载所有 Agent 配置文件，更新 AgentRegistry。

### 触发条件

- 运维人员修改了 Agent 配置文件后，需要热更新
- CLI 调用 `reload-agents` 命令
- API 调用 `reload_agents` RPC 方法

### 关键特性

- **Hot reload**：无需重启服务器
- **Full replacement**：完全替换现有配置（清空后重新加载）
- **Nil-safe**：AgentRegistry 为 nil 时返回 count=0，不报错
- **Error propagation**：加载失败时返回错误（含目录路径信息）

---

## 主流程

```mermaid
sequenceDiagram
    participant C as CLI/API
    participant WS as WebSocket Server
    participant H as ReloadAgentsHandler
    participant R as AgentRegistry
    participant FS as 文件系统

    C->>WS: reload_agents {}
    WS->>H: HandleRequest(ctx, client, req)

    alt AgentRegistry 为 nil
        H-->>WS: 返回 {count: 0}
        WS-->>C: 成功响应
    end

    H->>R: Reload()
    R->>R: 清空旧配置，更新 dir
    R->>FS: os.ReadDir 扫描 agents 目录
    FS-->>R: 返回目录条目
    loop 每个 .md 文件
        R->>FS: os.ReadFile 读取文件
        R->>R: ParseFrontMatter（解析 YAML + 验证 + 提取 SystemPrompt）
        R->>R: 检查 ID 重复，加入 agents map
    end

    alt 加载失败
        R-->>H: 返回错误
        H-->>WS: 返回错误（含目录路径）
        WS-->>C: 错误响应
    end

    R-->>H: 成功
    H->>R: ListAll()
    R-->>H: 返回 []*AgentConfig
    H->>H: len(ListAll())
    H-->>WS: 返回 {count: N}
    WS-->>C: 成功响应
```

### 详细步骤

1. **检查 AgentRegistry**：如果为 nil，直接返回 `{count: 0}`（json.Marshal 序列化）
2. **调用 Reload()**：用 `RLock` 读取 `dir` 字段，释放锁后调用 `Load(dir)`
3. **Load() 加写锁**：`Lock()` 保护整个加载过程
4. **清空旧配置**：`r.agents = make(map[string]*AgentConfig)`，同时更新 `r.dir`
5. **扫描目录**：`os.ReadDir(dir)` 遍历目录，跳过子目录和非 `.md` 文件
6. **逐文件处理**：对每个 `.md` 文件：
   - `os.ReadFile` 读取文件内容（失败则 skip + 日志）
   - `ParseFrontMatter` 解析 YAML front matter（缺少 `---` 分隔符返回 `ErrNoFrontMatter`；YAML 解析失败返回 `ErrInvalidFrontMatter`）
   - `Validate()` 检查必填字段和 MCP 配置（失败则返回错误，该文件被跳过）
   - 提取 Markdown body 为 `SystemPrompt`
   - 检查 ID 重复（重复则 skip + 日志，保留先加载的）
7. **返回结果**：`Load()` 成功后，handler 调用 `len(h.registry.ListAll())` 获取数量
8. **错误包装**：`Reload()` 失败时，handler 用 `fmt.Errorf("reload agents from %q: %w", h.registry.Dir(), err)` 包装错误
9. **返回响应**：成功返回 `{count: N}`

---

## 边缘场景

### 1. AgentRegistry 为 nil

```mermaid
flowchart TD
    A[检查 AgentRegistry] --> B{为 nil?}
    B -->|是| C[返回 {count: 0}]
    B -->|否| D[继续处理]
```

| 场景 | 处理方式 |
|------|----------|
| AgentRegistry 未初始化 | 返回 `{count: 0}`，不报错 |

**设计原因**：Agent 是可选功能，nil 表示禁用。

### 2. 目录不存在

```mermaid
flowchart TD
    A[扫描目录] --> B{目录存在?}
    B -->|否且 IsNotExist| C[返回 nil（可选模块，D-063）]
    B -->|否且其他错误| D[返回错误]
    B -->|是| E[继续处理]
```

| 场景 | 处理方式 |
|------|----------|
| agents 目录不存在 | 返回 nil（可选模块，agent 功能禁用时允许） |
| 目录无读取权限 | 返回错误 |

### 3. 配置文件解析失败

```mermaid
flowchart TD
    A[解析 YAML front matter] --> B{解析成功?}
    B -->|失败| C[记录错误, 继续下一个]
    B -->|成功| D[验证必填字段 + MCP 配置]
    D --> E{验证通过?}
    E -->|否| F[记录错误, 继续下一个]
    E -->|是| G[提取 Markdown body 为 SystemPrompt]
    G --> H[添加到配置列表]
```

| 场景 | 处理方式 |
| ---- | ---- |
| 无 front matter（缺少 `---` 分隔符） | 跳过该文件，返回 `ErrNoFrontMatter` |
| YAML 格式错误 | 跳过该文件，返回 `ErrInvalidFrontMatter`（wrap 原始错误） |
| 必填字段缺失（id/name/model/api_key_env） | 跳过该配置，返回对应的 `ErrMissing*` 错误 |
| MCP server 缺少 name | 跳过该配置，返回 `ErrMCPMissingName` |
| MCP server name 重复 | 跳过该配置，返回 `ErrMCPDuplicateName` |
| MCP transport 非 "sse"/"stdio" | 跳过该配置，返回 `ErrInvalidMCPTransport` |
| MCP sse 缺少 url | 跳过该配置，返回 `ErrMCPMissingURL` |
| MCP stdio 缺少 command | 跳过该配置，返回 `ErrMCPMissingCommand` |
| 文件读取失败 | 跳过该文件，记录 Info 级日志 |

### 4. 配置冲突

```mermaid
flowchart TD
    A[加载配置] --> B{ID 已存在?}
    B -->|是| C[记录日志, 跳过（保留先加载的）]
    B -->|否| D[正常添加]
```

| 场景 | 处理方式 |
|------|----------|
| 多个文件定义同名 Agent ID | 先加载的保留，后加载的被跳过并记录日志 |
| Agent ID 来源 | 配置 front matter 中的 `id` 字段（非文件名） |

### 5. 并发访问

```mermaid
flowchart TD
    A[Reload 进行中] --> B[Agent 执行请求到达]
    B --> C[使用旧配置执行]
    C --> D[Reload 完成]
    D --> E[后续请求使用新配置]
```

| 场景 | 处理方式 |
| ---- | ---- |
| Reload 期间有 Agent 执行 | 使用旧配置执行，不影响正在进行的任务 |
| 多个 Reload 并发 | AgentRegistry 内部使用 RWMutex 保护；`Reload()` 先用 RLock 读 dir，再用 Lock 调 `Load()` |

### 6. 目录在首次加载后被删除

| 场景 | 处理方式 |
| ---- | ---- |
| agents 目录在 `Load()` 成功后被删除 | 下次 `Reload()` 调用 `Load(dir)` 时 `os.ReadDir` 返回 `IsNotExist` 错误，`Load()` 返回 nil（视为模块禁用） |
| 结果 | 返回 `{count: 0}`，不报错 |

**行为说明**：这与首次加载时目录不存在的行为一致（D-063）。

**部分加载失败**：`Load()` 对单个文件错误（读取失败、解析失败、验证失败、ID 重复）采取 skip + 继续策略，不影响其他文件。只有目录级别的错误（如权限不足）才会使 `Load()` 返回 error。如果所有文件都被跳过，`Load()` 仍返回 nil，handler 报告 `{count: 0}`。

### 7. Reload 在 Load 之前调用（dir 为空）

| 场景 | 处理方式 |
| ---- | ---- |
| `NewRegistry()` 后直接调用 `Reload()` | `dir` 为空字符串，`Load("")` 尝试读取空路径，`os.ReadDir` 返回错误 |
| 结果 | handler 返回错误：`reload agents from "": read agents dir: ...` |

---

## 依赖关系

### 内部依赖

| 组件 | 用途 |
|------|------|
| `agent.AgentRegistry` | 管理 Agent 配置 |

### 外部依赖

| 组件 | 用途 |
|------|------|
| 文件系统 | 读取 Agent 配置文件 |

### 文件系统操作

| 操作 | 路径 | 说明 |
|------|------|------|
| READDIR | agents/ | 扫描目录 |
| READ | agents/*.md | 读取配置文件（YAML front matter + Markdown body） |

---

## 关键设计决策

### 1. Full Replacement

完全替换而非增量更新：
- **优点**：实现简单，无需处理删除逻辑
- **优点**：保证配置一致性
- **缺点**：重载期间可能有短暂的不一致
- **权衡**：在实际场景中，重载是低频操作，短暂不一致可接受

### 2. Nil-safe

AgentRegistry 为 nil 时返回 count=0：
- **原因**：Agent 是可选功能
- **行为**：不报错，返回空结果
- **适用场景**：未配置 Agent 功能的部署

### 3. Error Propagation

加载失败时返回错误：
- **原因**：让调用者知道重载失败
- **行为**：返回具体错误信息
- **客户端处理**：显示错误消息，检查配置文件

### 4. Hot Reload

无需重启服务器：
- **原因**：提高运维效率
- **实现**：`Reload()` 先用 `RLock` 读取目录路径，再调用 `Load()` 用 `Lock` 清空并重新加载配置
- **副作用**：正在进行的 Agent 执行使用旧配置（Build 时已拷贝 config）

---

## 配置文件格式

配置文件使用 `.md` 格式，包含 YAML front matter 和 Markdown body。

### 示例配置

```markdown
---
id: my-agent
name: My Agent
model: gpt-4
api_key_env: OPENAI_API_KEY
parameters:
  temperature: 0.7
  max_tokens: 2048
tools:
  - get_weather
  - get_current_time
---

你是一个 helpful assistant.
```

### 必填字段（Validate 检查）

| 字段 | 类型 | 说明 |
| ---- | ---- | ---- |
| `id` | string | Agent 唯一标识（front matter 中） |
| `name` | string | Agent 显示名称 |
| `model` | string | LLM 模型名称（用于自动检测 provider） |
| `api_key_env` | string | API Key 对应的环境变量名 |

### 可选字段

| 字段 | 类型 | 说明 |
| ---- | ---- | ---- |
| `description` | string | Agent 描述 |
| `base_url` | string | 自定义 LLM API 端点 |
| `parameters.temperature` | float | 生成温度 |
| `parameters.max_tokens` | int | 最大 token 数 |
| `parameters.top_p` | float | nucleus sampling 参数 |
| `context.max_tokens` | int | 上下文窗口最大 token 数 |
| `context.max_messages` | int | 上下文最大消息数 |
| `tools` | list | 工具名称列表（从 tool registry 解析） |
| `dynamic_tools` | list | 运行时动态解析的工具名称 |
| `tool_config` | map | 每个工具的独立配置（key 为工具名） |
| `middleware` | object | Eino 中间件开关（见下表） |
| `sub_agents` | list | 子 Agent ID 列表 |
| `mcp_servers` | list | MCP 服务器连接配置（见下表） |
| Markdown body | string | 作为 SystemPrompt（非 YAML 字段） |

### middleware 子字段

| 字段 | 类型 | 默认值 | 说明 |
| ---- | ---- | ------ | ---- |
| `enable_summarization` | bool | false | 启用上下文摘要中间件 |
| `summarization_tokens` | int | 160000 | 触发摘要的 token 阈值 |
| `enable_tool_reduction` | bool | false | 启用工具缩减中间件 |
| `tool_reduction_max_chars` | int | 50000 | 工具描述最大字符数 |
| `enable_patch_tool_calls` | bool | false | 启用工具调用修补 |
| `enable_client_tools` | bool | false | 启用客户端设备函数注入为 Agent 工具 |
| `client_tools` | object | - | 客户端工具配置（function_tags, excluded_functions, call_timeout） |

### mcp_servers 子字段

| 字段 | 类型 | 说明 |
| ---- | ---- | ---- |
| `name` | string | **必填**，服务器名称（不可重复） |
| `transport` | string | **必填**，传输方式：`"sse"` 或 `"stdio"` |
| `url` | string | SSE 端点地址（transport=sse 时必填） |
| `command` | string | stdio 命令（transport=stdio 时必填） |
| `args` | list | stdio 命令参数 |
| `env` | list | stdio 环境变量 |
| `tools` | list | 过滤特定工具（空 = 全部） |

---

## 相关文档

- [Agent 执行流程](agent-execution.md)
- [CLI 命令](cli-ipc.md)
- [设计决策](../architecture/design-decisions.md)
