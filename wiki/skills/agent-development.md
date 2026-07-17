---
last_updated: 2026-07-17
---

# Agent 开发指南

> last_updated: 2026-07-17

## 概述

Xyncra 使用 Eino 框架（`github.com/cloudwego/eino`）的 Agent Development Kit（ADK）构建 AI Agent。Agent 定义存储在 `agents/` 目录下的 Markdown 文件中，包含 YAML frontmatter 元数据和 system prompt。

## Agent 配置文件格式（Markdown + YAML Frontmatter）

Agent 配置使用 Markdown 文件作为载体，文件头部以 `---` 分隔的 YAML frontmatter 定义元数据，正文部分为 system prompt。

### 基本结构

每个 Agent 是一个 `.md` 文件，包含 YAML frontmatter 和 Markdown body：

```markdown
---
id: my-agent
name: 我的助手
description: 简短描述，说明这个 Agent 的用途
model: qwen3.7-plus
api_key_env: DASHSCOPE_API_KEY
base_url: "https://coding.dashscope.aliyuncs.com/v1"
parameters:
  temperature: 0.7
  max_tokens: 2000
  top_p: 0.9
context:
  max_tokens: 8000
  max_messages: 20
tools:
  - tool_name_1
  - tool_name_2
middleware:
  enable_client_tools: true
  enable_summarization: true
  enable_tool_reduction: true
  tool_reduction_max_chars: 50000
sub_agents:
  - child-agent-id
mcp_servers:
  - name: filesystem
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
---
You are an AI assistant. Your task is to...
```

### Frontmatter 字段详解

#### 基础字段

| 字段 | 必填 | 类型 | 说明 |
|------|------|------|------|
| `id` | 是 | string | Agent 唯一标识，用于内部引用和 sub_agents 关联 |
| `name` | 是 | string | 人类可读的 Agent 名称 |
| `description` | 是 | string | 简短描述，用于 Agent Registry 和用户选择 |
| `model` | 是 | string | LLM 模型名称（如 `qwen3.7-plus`、`gpt-4`） |
| `api_key_env` | 是 | string | 存储 API Key 的环境变量名 |
| `base_url` | 是 | string | LLM API 的 Base URL |

#### 模型参数

| 字段 | 必填 | 类型 | 默认值 | 说明 |
|------|------|------|--------|------|
| `parameters.temperature` | 否 | float | 0.7 | 生成随机性，0-1 之间 |
| `parameters.max_tokens` | 否 | int | 2000 | 单次响应的最大 token 数 |
| `parameters.top_p` | 否 | float | 0.9 | Nucleus sampling 参数 |

#### 上下文管理

| 字段 | 必填 | 类型 | 默认值 | 说明 |
|------|------|------|--------|------|
| `context.max_tokens` | 否 | int | 8000 | 历史消息保留的最大 token 数 |
| `context.max_messages` | 否 | int | 20 | 历史消息保留的最大条数 |

#### 工具配置

| 字段 | 必填 | 类型 | 说明 |
|------|------|------|------|
| `tools` | 否 | string[] | Agent 可用的内置工具列表 |
| `mcp_servers` | 否 | object[] | MCP（Model Context Protocol）外部工具服务器配置 |

#### Middleware 配置

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `enable_client_tools` | bool | false | 是否开启客户端动态工具 |
| `enable_patch_tool_calls` | bool | false | 是否开启工具调用结果合并 |
| `enable_summarization` | bool | false | 是否开启历史消息摘要 |
| `summarization_tokens` | int | - | 摘要触发阈值（超过该 token 数触发摘要） |
| `enable_tool_reduction` | bool | false | 是否开启工具结果精简 |
| `tool_reduction_max_chars` | int | - | 工具结果最大字符数 |

#### 子 Agent 配置

| 字段 | 必填 | 类型 | 说明 |
|------|------|------|------|
| `sub_agents` | 否 | string[] | 子 Agent ID 列表，用于 DeepAgent 模式 |

## 可用工具

### 内置工具

| 工具 ID | 说明 |
|---------|------|
| `get_weather` | 获取城市天气信息（参考 `weather-bot.md`） |
| `get_current_time` | 获取当前时间 |
| `ask_user` | 向用户发起确认询问（参考 `hitl-bot.md`） |
| `retrieve_tool_result` | 检索先前工具调用的结果 |

### 客户端动态工具

当 `enable_client_tools: true` 时，Agent 可以使用客户端注册的函数。这些函数通过 WebSocket 连接的 FunctionRegistry 动态注册和调用。

### MCP 外部工具

通过 MCP 服务器连接的外部工具，支持两种传输方式：

**STDIO 传输**（本地工具）：
```yaml
mcp_servers:
  - name: filesystem
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
    tools: ["read_file", "write_file"]
```

**SSE 传输**（远程工具）：
```yaml
mcp_servers:
  - name: remote-tools
    transport: sse
    url: http://localhost:3000/sse
```

## Agent 类型

### ChatModelAgent（标准 ReAct Agent）

LLM 驱动推理循环：思考 → 调用工具 → 处理结果 → 继续或结束。这是最通用的 Agent 类型。

**适用场景**：
- 需要工具调用的对话助手
- 需要访问外部数据的查询代理
- 需要多步推理的复杂任务

### DeepAgent（子 Agent 编排）

预构建的 Agent 类型，支持：
- 任务规划（Plan）
- 子 Agent 委派（Delegate）
- 文件系统操作
- Skill 执行

**适用场景**：
- 需要分解为子任务的工作流
- 需要多个专业 Agent 协作的场景
- 需要规划-执行-验证循环的任务

### TurnLoop（多轮执行循环）

推式事件循环，支持：
- 抢占式执行控制
- 生命周期管理
- 多轮交互

**适用场景**：
- 需要长时运行的 Agent 任务
- 需要外部事件驱动的交互
- 需要细粒度执行控制

## Agent 开发流程

### 第一步：定义 Agent 配置

```bash
# 创建新的 Agent 配置文件
touch agents/my-new-agent.md
```

编写 frontmatter 和 system prompt。参考现有 Agent 如 `weather-bot.md` 保持风格一致。

### 第二步：选择模型和 Provider

支持的 Provider：
- **DashScope（阿里云通义千问）**：`base_url: https://coding.dashscope.aliyuncs.com/v1`
- **OpenAI**：`base_url: https://api.openai.com/v1`
- **Ollama**（本地部署）：通过 Eino 的 Ollama 组件

通过 `api_key_env` 字段指定存储 API Key 的环境变量名。

### 第三步：编写 System Prompt

System Prompt 是 Agent 行为的核心。好的 System Prompt 应：

1. **定义角色**：Agent 是什么、能做什么
2. **设定边界**：什么不该做
3. **提供示例**：关键操作的使用方式
4. **指定输出格式**：如果输出需要结构化

### 第四步：配置工具

根据 Agent 的职责选择所需工具：
- 信息查询类：配置 MCP 服务器或内置工具
- 操作执行类：配置客户端工具或子 Agent
- 人机协作类：使用 `ask_user` 工具

### 第五步：测试 Agent

1. 启动服务器：`make build && ./bin/xyncra-server`
2. 通过 WebSocket 连接并调用 Agent
3. 观察日志输出，检查行为是否符合预期

### 第六步：注册到 Agent Registry

Agent 配置文件放在 `agents/` 目录后，服务器启动时会通过 `agent.NewRegistry().Load(agentsDir)` 自动加载。

## 高级模式

### Human-in-the-Loop（HITL）

实现需要用户确认的操作流程：

1. Agent 检测到需要确认的操作
2. 调用 `ask_user` 工具向用户发起询问
3. Agent 暂停执行，等待用户响应
4. 用户确认后继续执行

参考 `hitl-bot.md`、`hitl-parent.md`、`hitl-child-a.md`、`hitl-child-b.md` 的 HITL 示例。

### 子 Agent 编排（Parent-Child）

父 Agent 可以同时委派任务给多个子 Agent 并行执行：

```yaml
sub_agents:
  - hitl-child-a
  - hitl-child-b
```

父 Agent 的 system prompt 应描述何时以及如何委派任务给子 Agent。

### 流式文本（Streaming Text）

Agent 的增量输出通过 `UpdateTypeStreaming` 类型推送给客户端。这允许客户端实时显示 Agent 的思考过程。

## 常见问题

### Agent 未加载

检查：
- 配置文件是否为 `.md` 格式
- YAML frontmatter 格式是否正确（`---` 分隔）
- `id` 字段是否唯一
- 服务器日志输出是否正确

### 工具调用失败

检查：
- 工具名称是否拼写正确
- 环境变量中 API Key 是否有效
- MCP 服务器是否可达
- 客户端是否已连接并注册了函数

### 内存消耗过高

调整 `context.max_tokens` 和 `context.max_messages` 参数，或启用 `enable_summarization` 中间件。

### 响应速度慢

考虑：
- 使用更快的模型
- 启用 `enable_tool_reduction` 减少工具结果大小
- 限制 `max_tokens` 减少生成长度
- 检查网络连接延迟
