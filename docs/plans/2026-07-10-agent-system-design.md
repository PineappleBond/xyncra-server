# Xyncra AI Agent 系统设计文档

**日期**：2026-07-10  
**版本**：v1.0  
**状态**：已批准

---

## 目录

- [1. 概述](#1-概述)
- [2. 架构设计](#2-架构设计)
- [3. Eino 框架集成](#3-eino-框架集成)
- [4. Agent 配置系统](#4-agent-配置系统)
- [5. 上下文管理](#5-上下文管理)
- [6. 流式输出处理](#6-流式输出处理)
- [7. 消息协议兼容性](#7-消息协议兼容性)
- [8. 实施阶段](#8-实施阶段)
- [9. 产品决策](#9-产品决策)
- [10. 关键文件清单](#10-关键文件清单)

---

## 1. 概述

### 1.1 项目目标

为 Xyncra 即时通讯系统添加 AI Agent 功能，使用户可以与 AI 助手进行对话。核心需求：

1. **Agent 作为特殊用户**：Agent 有 User ID，用户给 Agent 发消息，Agent 调用 LLM 处理后回复
2. **流式输出**：Agent 的回复实时流式推送给用户（类似 ChatGPT 的打字效果）
3. **上下文管理**：支持多轮对话，记住之前的对话内容
4. **配置化**：通过 Markdown+YAML 文件定义 Agent（User ID、名字、描述、system prompt 等）
5. **零协议改动（MVP）**：Phase 1 不修改现有消息协议，复用现有机制

### 1.2 技术选型决策

经过调研，选择以下技术方案：

| 决策点 | 选择 | 理由 |
|--------|------|------|
| **Agent 框架** | Eino (github.com/cloudwego/eino) | Go 原生、功能完整、12.2k stars、字节跳动维护 |
| **LLM Provider** | Anthropic Claude / OpenAI | 通过 eino-ext 支持，灵活切换 |
| **集成方式** | 进程内集成（非 subprocess） | 零额外进程开销，纯 Go 依赖 |
| **上下文存储** | DB + 内存缓存 | 从 messages 表读取，sync.Map 缓存热路径 |
| **流式输出** | 复用现有 stream_text 机制 | 零协议改动，客户端已有处理逻辑 |

**为什么不用 Claude Code / Codex CLI？**
- 无 Go SDK，需要 subprocess 集成（200-500ms 启动延迟 + 50-100MB 内存开销）
- 部署复杂（需要安装 Node.js 或 Rust binary）
- 不适合 IM 系统的低延迟场景

**为什么不用直接调用 LLM API？**
- 需要自己实现上下文管理、tools、sub-agents、MCP 等功能
- 工作量巨大，重复造轮子

**为什么选 Eino？**
- Go 原生，零语言鸿沟
- 提供 ChatModelAgent、DeepAgent（sub-agents）、graph orchestration、streaming、tools、sessions
- 流式输出是核心强项，可直接对接 Xyncra 的 WebSocket streaming
- 纯 Go 依赖，部署简单

**Eino 的唯一缺陷**：无原生 MCP 支持，但可以通过 Tool 系统桥接，不是 blocker。

### 1.3 核心设计原则

1. **最小改动原则**：Phase 1 不修改任何协议定义，仅通过 UserID 约定实现
2. **向后兼容**：所有增强均为可选，旧客户端不受影响
3. **复用优先**：stream_text 和 set_typing 已满足 Agent 80% 的需求
4. **渐进增强**：从 MVP 到生产的平滑过渡路径

---

## 2. 架构设计

### 2.1 整体架构

```
┌─────────────────────────────────────────────────────────────────┐
│                    xyncra-server 进程                            │
│                                                                 │
│  ┌──────────────┐  ┌───────────────┐  ┌─────────────────────┐  │
│  │ WebSocket    │  │ AgentRegistry │  │ AgentTriggerHandler │  │
│  │ Server       │  │ (配置加载)     │  │ (消息检测)          │  │
│  └──────┬───────┘  └───────────────┘  └──────────┬──────────┘  │
│         │                                         │             │
│         │        ┌─────────────────────────┐      │             │
│         │        │ MQ (Asynq)              │      │             │
│         │        │ TypeAgentProcess task   │◄─────┘             │
│         │        └────────────┬────────────┘                    │
│         │                     │                                  │
│         │        ┌────────────▼────────────┐                    │
│         │        │ AgentTaskHandler        │                    │
│         │        │ ┌─────────────────────┐ │                    │
│         │        │ │  Eino Framework     │ │                    │
│         │        │ │  - ChatModelAgent   │ │                    │
│         │        │ │  - DeepAgent        │ │                    │
│         │        │ │  - Graph Orch.      │ │                    │
│         │        │ │  - Streaming Engine │ │                    │
│         │        │ └──────────┬──────────┘ │                    │
│         │        │            │             │                    │
│         │        │ ┌──────────▼──────────┐ │                    │
│         │        │ │ ContextManager      │ │                    │
│         │        │ │ (DB + Cache)        │ │                    │
│         │        │ └─────────────────────┘ │                    │
│         │        └──────────┬──────────────┘                    │
│         │                   │                                    │
│  ┌──────▼───────────────────▼─────────────────────┐            │
│  │ Redis + Store + LLM API (Anthropic/OpenAI)     │            │
│  └────────────────────────────────────────────────┘            │
└─────────────────────────────────────────────────────────────────┘
```

### 2.2 数据流

**用户发送消息 → Agent 响应 → 流式推送**：

```
1. 用户发送消息
   Client → send_message RPC → Server
   
2. 消息持久化
   send_message handler → Store.SendMessage() → DB
   
3. 检测 Agent 目标
   send_message handler 检查 receiver 是否是 Agent (agent/ 前缀)
   → Enqueue TypeAgentProcess task (fire-and-forget, D-007)
   
4. Agent 处理
   AgentTaskHandler 消费 task:
   a. set_typing(true) 广播 "Agent 在思考"
   b. ContextManager.GetContext() 加载对话历史
   c. Eino Agent 调用 LLM (流式)
   d. 每个 chunk → broadcastFn streaming update (Seq=0)
   e. 流式结束 → set_typing(false)
   f. Store.SendMessage() 持久化 Agent 回复
   g. MQ broadcast 回复消息给用户
   
5. 客户端接收
   Client 收到 streaming update (Seq=0) → OnStreaming 回调 → UI 实时显示
   Client 收到 message update (Seq>0) → 持久化到本地 DB
```

### 2.3 关键组件

#### AgentRegistry
- 从 `agents/` 目录加载 YAML 配置
- 管理 Agent 配置的生命周期
- 提供 `IsAgent(userID string) bool` 查询

#### AgentTaskHandler
- MQ task handler，处理 `TypeAgentProcess` 任务
- 使用 Eino 框架调用 LLM
- 通过 `broadcastFn` 流式推送给客户端

#### ContextManager
- 从 `messages` 表读取对话历史
- `sync.Map` 缓存热路径
- Token 计数裁剪（`tiktoken-go`）+ 固定消息数 fallback
- per-conversation worker 串行处理

#### LLMClient (Eino 封装)
- 封装 Eino 的 ChatModel 接口
- 支持 Anthropic Claude 和 OpenAI
- 提供流式输出能力

---

## 3. Eino 框架集成

### 3.1 Eino 核心概念

Eino 提供以下核心能力：

- **ChatModelAgent**：带 tool use 的 agent
- **DeepAgent**：任务分解、sub-agent 委派
- **Graph Orchestration**：图编排（节点、边、编译、执行）
- **Streaming**：全链路流式处理
- **Tools**：自定义 tools + GraphTool
- **Sessions**：持久对话支持
- **Interrupt/Resume**：Human-in-the-loop

### 3.2 集成代码示例

```go
// internal/agent/eino_agent.go

package agent

import (
    "context"
    "io"
    
    "github.com/cloudwego/eino/adk"
    "github.com/cloudwego/eino/components/model"
    "github.com/cloudwego/eino-ext/components/model/openai"
)

// EinoAgent 封装 Eino 框架
type EinoAgent struct {
    agent   adk.Agent
    config  *AgentConfig
}

// NewEinoAgent 创建 Eino Agent
func NewEinoAgent(config *AgentConfig) (*EinoAgent, error) {
    // 1. 创建 LLM client
    llm, err := openai.NewChatModel(&openai.ChatModelConfig{
        APIKey:  config.APIKey,
        Model:   config.Model,
        BaseURL: config.BaseURL, // 可选：自定义 endpoint
    })
    if err != nil {
        return nil, err
    }
    
    // 2. 创建 ChatModelAgent
    agent, err := adk.NewChatModelAgent(&adk.ChatModelAgentConfig{
        Model:        llm,
        SystemPrompt: config.SystemPrompt,
        Tools:        config.Tools, // 可选：自定义 tools
    })
    if err != nil {
        return nil, err
    }
    
    return &EinoAgent{
        agent:  agent,
        config: config,
    }, nil
}

// StreamChat 流式调用 LLM
func (a *EinoAgent) StreamChat(ctx context.Context, messages []LLMMessage) (<-chan StreamChunk, error) {
    // 1. 构建 Eino 的 messages
    einoMsgs := convertToEinoMessages(messages)
    
    // 2. 创建 runnable（带 streaming）
    runner, err := a.agent.AsRunnable(ctx, adk.WithStreaming())
    if err != nil {
        return nil, err
    }
    
    // 3. 执行并返回 stream
    stream, err := runner.Invoke(ctx, einoMsgs)
    if err != nil {
        return nil, err
    }
    
    // 4. 转换为 channel
    ch := make(chan StreamChunk, 16)
    go func() {
        defer close(ch)
        for {
            chunk, err := stream.Recv()
            if err == io.EOF {
                ch <- StreamChunk{Done: true}
                break
            }
            if err != nil {
                ch <- StreamChunk{Error: err}
                break
            }
            ch <- StreamChunk{Text: chunk.Text}
        }
    }()
    
    return ch, nil
}
```

### 3.3 流式输出桥接

Eino 的流式输出通过 `broadcastFn` 桥接到 Xyncra 的 streaming update 通道：

```go
// internal/agent/task_handler.go

func (h *AgentTaskHandler) processTask(ctx context.Context, task *AgentTask) error {
    // 1. 加载上下文
    messages, err := h.ctxManager.GetContext(ctx, task.ConversationID, task.AgentID)
    if err != nil {
        return err
    }
    
    // 2. 设置 typing 状态
    h.broadcastTyping(task.AgentID, task.ConversationID, true)
    defer h.broadcastTyping(task.AgentID, task.ConversationID, false)
    
    // 3. 调用 LLM（流式）
    stream, err := h.einoAgent.StreamChat(ctx, messages)
    if err != nil {
        return err
    }
    
    // 4. 流式广播给客户端
    streamID := uuid.New().String()
    var fullText strings.Builder
    
    for chunk := range stream {
        if chunk.Error != nil {
            return chunk.Error
        }
        if chunk.Done {
            break
        }
        
        fullText.WriteString(chunk.Text)
        
        // 广播流式 chunk（复用 stream_text 机制）
        h.broadcastStreaming(task.AgentID, task.ConversationID, streamID, fullText.String(), false)
        
        // Rate limiting: 50ms/20fps
        time.Sleep(50 * time.Millisecond)
    }
    
    // 5. 流式结束
    h.broadcastStreaming(task.AgentID, task.ConversationID, streamID, fullText.String(), true)
    
    // 6. 持久化 Agent 回复
    msg := &model.Message{
        ConversationID: task.ConversationID,
        SenderID:       task.AgentID,
        Content:        fullText.String(),
        Type:           "text",
    }
    _, err = h.store.SendMessage(ctx, msg, []string{task.UserID})
    if err != nil {
        return err
    }
    
    return nil
}
```

---

## 4. Agent 配置系统

### 4.1 配置文件格式

Agent 通过 YAML 文件定义，存放于 `agents/` 目录：

```yaml
# agents/weather-bot.yaml
id: weather-bot
name: Weather Bot
description: "Provides weather information"
model: "claude-3-5-sonnet-20241022"  # 或 "gpt-4"
api_key_env: "ANTHROPIC_API_KEY"     # 从环境变量读取 API key
base_url: ""                          # 可选：自定义 endpoint
system_prompt_file: "./prompts/weather-bot.md"
parameters:
  temperature: 0.7
  max_tokens: 4096
context:
  max_tokens: 4096
  max_messages: 20
tools: []  # 可选：自定义 tools
```

### 4.2 System Prompt 文件

```markdown
# agents/prompts/weather-bot.md

You are a helpful weather assistant. You provide accurate weather information.

Current time: {{current_time}}
User location: {{user_location}}

Be concise and friendly.
```

### 4.3 AgentRegistry 实现

```go
// internal/agent/registry.go

package agent

import (
    "os"
    "path/filepath"
    "sync"
    
    "gopkg.in/yaml.v3"
)

// AgentConfig Agent 配置
type AgentConfig struct {
    ID               string            `yaml:"id"`
    Name             string            `yaml:"name"`
    Description      string            `yaml:"description"`
    Model            string            `yaml:"model"`
    APIKeyEnv        string            `yaml:"api_key_env"`
    BaseURL          string            `yaml:"base_url"`
    SystemPromptFile string            `yaml:"system_prompt_file"`
    SystemPrompt     string            `yaml:"-"` // 加载后填充
    Parameters       LLMParameters     `yaml:"parameters"`
    Context          ContextConfig     `yaml:"context"`
    Tools            []ToolConfig      `yaml:"tools"`
}

type LLMParameters struct {
    Temperature float64 `yaml:"temperature"`
    MaxTokens   int     `yaml:"max_tokens"`
}

type ContextConfig struct {
    MaxTokens   int `yaml:"max_tokens"`
    MaxMessages int `yaml:"max_messages"`
}

// AgentRegistry 管理 Agent 配置
type AgentRegistry struct {
    agents sync.Map // agentID -> *AgentConfig
    dir    string
}

// NewAgentRegistry 创建 AgentRegistry
func NewAgentRegistry(dir string) *AgentRegistry {
    return &AgentRegistry{dir: dir}
}

// Load 加载所有 Agent 配置
func (r *AgentRegistry) Load() error {
    files, err := os.ReadDir(r.dir)
    if err != nil {
        if os.IsNotExist(err) {
            return nil // agents/ 目录不存在，正常
        }
        return err
    }
    
    for _, file := range files {
        if !file.IsDir() && filepath.Ext(file.Name()) == ".yaml" {
            config, err := r.loadConfig(filepath.Join(r.dir, file.Name()))
            if err != nil {
                continue // 跳过无效配置
            }
            r.agents.Store(config.ID, config)
        }
    }
    
    return nil
}

// IsAgent 检查是否是 Agent
func (r *AgentRegistry) IsAgent(userID string) bool {
    agentID := strings.TrimPrefix(userID, "agent/")
    _, ok := r.agents.Load(agentID)
    return ok
}

// GetConfig 获取 Agent 配置
func (r *AgentRegistry) GetConfig(agentID string) (*AgentConfig, bool) {
    val, ok := r.agents.Load(agentID)
    if !ok {
        return nil, false
    }
    return val.(*AgentConfig), true
}

func (r *AgentRegistry) loadConfig(path string) (*AgentConfig, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }
    
    var config AgentConfig
    if err := yaml.Unmarshal(data, &config); err != nil {
        return nil, err
    }
    
    // 加载 system prompt 文件
    if config.SystemPromptFile != "" {
        promptData, err := os.ReadFile(config.SystemPromptFile)
        if err != nil {
            return nil, err
        }
        config.SystemPrompt = string(promptData)
    }
    
    // 读取 API key
    if config.APIKeyEnv != "" {
        config.Parameters.APIKey = os.Getenv(config.APIKeyEnv)
    }
    
    return &config, nil
}
```

---

## 5. 上下文管理

### 5.1 设计原则

- **DB 存储 + 内存缓存**：从 `messages` 表读取历史（数据已存在），`sync.Map` 缓存热路径
- **Token 计数裁剪**：使用 `tiktoken-go` 计算 token 数，保留最近的消息直到达到上限
- **per-conversation 串行处理**：保证同一对话的消息串行处理，避免上下文不一致

### 5.2 ContextManager 接口

```go
// internal/agent/context.go

package agent

import (
    "context"
    "time"
)

// LLMMessage 是发送给 LLM 的消息格式
type LLMMessage struct {
    Role    string // "system", "user", "assistant"
    Content string
}

// ContextManager 管理 Agent 对话上下文
type ContextManager interface {
    // GetContext 获取指定对话的 LLM 上下文
    GetContext(ctx context.Context, convID string, agentID string) ([]LLMMessage, error)
    
    // AddMessage 添加一条消息到对话上下文
    AddMessage(ctx context.Context, convID string, msg LLMMessage, senderID string) error
    
    // Invalidate 清除指定对话的缓存
    Invalidate(convID string)
    
    // Cleanup 清理过期的缓存条目
    Cleanup(olderThan time.Time)
}
```

### 5.3 DBContextManager 实现

```go
// internal/agent/db_context_manager.go

package agent

import (
    "context"
    "sync"
    "time"
    
    "github.com/PineappleBond/xyncra-server/internal/store"
    "github.com/PineappleBond/xyncra-server/internal/store/model"
)

// conversationContext 缓存单个对话的消息上下文
type conversationContext struct {
    mu       sync.Mutex
    messages []model.Message
    loadedAt time.Time
}

// DBContextManager 基于 DB 的 ContextManager 实现
type DBContextManager struct {
    store       store.StoreAPI
    tokenizer   TokenCounter
    maxTokens   int
    maxMessages int
    cache       sync.Map // convID -> *conversationContext
    cacheTTL    time.Duration
}

// GetContext 实现 ContextManager.GetContext
func (m *DBContextManager) GetContext(ctx context.Context, convID string, agentID string) ([]LLMMessage, error) {
    // 1. 获取或创建缓存条目
    cc := m.getOrCreateContext(convID)
    
    cc.mu.Lock()
    defer cc.mu.Unlock()
    
    // 2. 检查缓存是否需要刷新
    if m.needsRefresh(cc) {
        if err := m.loadFromDB(ctx, cc, convID, agentID); err != nil {
            return nil, err
        }
    }
    
    // 3. 裁剪到窗口限制
    return m.trimToWindow(cc.messages, agentID)
}

// loadFromDB 从 DB 加载最近的消息到缓存
func (m *DBContextManager) loadFromDB(ctx context.Context, cc *conversationContext, convID string, agentID string) error {
    fetchLimit := m.maxMessages * 2
    
    msgs, err := m.listRecentMessages(ctx, convID, fetchLimit)
    if err != nil {
        return err
    }
    
    cc.messages = msgs
    cc.loadedAt = time.Now()
    return nil
}

// listRecentMessages 获取最近的 N 条消息（按 MessageID ASC 排序）
func (m *DBContextManager) listRecentMessages(ctx context.Context, convID string, limit int) ([]model.Message, error) {
    // 使用两步查询：先获取最新的 N 条（DESC），再反转为 ASC
    // 需要新增 MessageStore.ListRecentByConversation() 方法
    
    allMsgs, err := m.store.MessageStore().ListByConversation(ctx, convID, 0, 1000)
    if err != nil {
        return nil, err
    }
    
    if len(allMsgs) <= limit {
        return allMsgs, nil
    }
    return allMsgs[len(allMsgs)-limit:], nil
}

// trimToWindow 将消息裁剪到 token 窗口内
func (m *DBContextManager) trimToWindow(messages []model.Message, agentID string) ([]LLMMessage, error) {
    if len(messages) == 0 {
        return nil, nil
    }
    
    // 从最新消息开始向前累加 token，直到达到上限
    var totalTokens int
    cutIdx := len(messages)
    
    for i := len(messages) - 1; i >= 0; i-- {
        msgTokens, err := m.countMessageTokens(messages[i])
        if err != nil {
            return m.trimByCount(messages, agentID), nil // fallback
        }
        if totalTokens+msgTokens > m.maxTokens {
            break
        }
        totalTokens += msgTokens
        cutIdx = i
    }
    
    // 转换为 LLMMessage 格式
    result := make([]LLMMessage, 0, len(messages)-cutIdx)
    for i := cutIdx; i < len(messages); i++ {
        role := "user"
        if messages[i].SenderID == agentID {
            role = "assistant"
        }
        result = append(result, LLMMessage{
            Role:    role,
            Content: messages[i].Content,
        })
    }
    return result, nil
}
```

### 5.4 Store 层扩展

需要新增 `MessageStore.ListRecentByConversation()` 方法：

```go
// internal/store/message.go

// ListRecentByConversation 返回指定对话中最近的 limit 条消息，
// 按 MessageID DESC 排序（最新在前）。
func (ms *MessageStore) ListRecentByConversation(ctx context.Context, convID string, limit int) ([]*model.Message, error) {
    if limit <= 0 || limit > 500 {
        limit = 50
    }
    
    var msgs []*model.Message
    err := ms.db.WithContext(ctx).
        Where("conversation_id = ?", convID).
        Order("message_id DESC").
        Limit(limit).
        Find(&msgs).Error
    if err != nil {
        return nil, err
    }
    
    // 反转为 MessageID ASC
    for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
        msgs[i], msgs[j] = msgs[j], msgs[i]
    }
    
    return msgs, nil
}
```

---

## 6. 流式输出处理

### 6.1 复用现有机制

Agent 的流式输出完全复用 `stream_text` RPC（D-051）和累积文本模式：

- **协议层**：使用 `UpdateTypeStreaming` (Seq=0, ephemeral)
- **广播函数**：通过 `BroadcastUpdates` 推送给会话成员
- **客户端处理**：复用 `StreamingHandler.OnStreaming` 回调

### 6.2 流式广播实现

```go
// internal/agent/broadcast.go

func (h *AgentTaskHandler) broadcastStreaming(agentID, convID, streamID, text string, isDone bool) {
    payload := map[string]interface{}{
        "stream_id":       streamID,
        "user_id":         agentID,
        "conversation_id": convID,
        "text":            text,
        "is_done":         isDone,
        "timestamp":       time.Now().Unix(),
    }
    
    update := &protocol.PackageDataUpdates{
        Seq:     0, // ephemeral
        Type:    protocol.UpdateTypeStreaming,
        Payload: payload,
    }
    
    // 广播给会话所有成员
    h.broadcastFn(convID, update)
}

func (h *AgentTaskHandler) broadcastTyping(agentID, convID string, isTyping bool) {
    payload := map[string]interface{}{
        "user_id":         agentID,
        "conversation_id": convID,
        "is_typing":       isTyping,
        "timestamp":       time.Now().Unix(),
    }
    
    update := &protocol.PackageDataUpdates{
        Seq:     0, // ephemeral
        Type:    protocol.UpdateTypeTyping,
        Payload: payload,
    }
    
    h.broadcastFn(convID, update)
}
```

### 6.3 Rate Limiting

复用现有 50ms/20fps rate limiter，避免 Eino 产出过快导致客户端卡顿：

```go
for chunk := range stream {
    fullText.WriteString(chunk.Text)
    h.broadcastStreaming(task.AgentID, task.ConversationID, streamID, fullText.String(), false)
    time.Sleep(50 * time.Millisecond) // 20fps
}
```

---

## 7. 消息协议兼容性

### 7.1 Phase 1（MVP）：零协议改动

**核心原则**：Agent 就是特殊的 UserID，复用所有现有机制。

#### Agent UserID 命名约定

```
agent/weather-bot
agent/code-reviewer
agent/translator
```

- `agent/` 前缀为系统保留命名空间
- Agent 在协议层与普通用户完全等价
- 客户端通过 `strings.HasPrefix(userID, "agent/")` 识别

#### 客户端改动（MVP）

客户端仅需新增一个 helper 函数：

```go
// pkg/client/agent.go

func isAgentUser(userID string) bool {
    return strings.HasPrefix(userID, "agent/")
}
```

在 `TypingHandler.OnTyping` 和 `StreamingHandler.OnStreaming` 中根据此函数决定 UI 展示：

```go
func (h *MyTypingHandler) OnTyping(userID, convID string, isTyping bool) {
    if isAgentUser(userID) {
        // 显示 "Agent is thinking..."
    } else {
        // 显示 "User is typing..."
    }
}
```

### 7.2 Phase 2（可选增强）

#### 新增 agent_status ephemeral push

```go
// pkg/protocol/protocol.go

const (
    UpdateTypeAgentStatus = "agent_status" // ephemeral: Seq=0
)
```

Payload：

```json
{
  "agent_id": "agent/weather-bot",
  "conversation_id": "conv-xxx",
  "status": "thinking" | "tool_calling" | "generating",
  "detail": "Searching weather data...",
  "timestamp": 1720612800
}
```

#### 新增 reload_agents RPC

```json
{
  "id": "req-006",
  "method": "reload_agents",
  "params": {}
}
```

---

## 8. 实施阶段

### Phase 1: MVP（预计 1-2 周）

**目标**：实现基本 Agent 功能，零协议改动

**任务**：

1. ✅ 新建 `internal/agent/` 包
2. ✅ 实现 `AgentRegistry`（从 YAML 加载配置）
3. ✅ 实现 `EinoAgent`（封装 Eino 框架）
4. ✅ 实现 `ContextManager`（DB 存储 + 简单缓存）
5. ✅ 实现 `AgentTaskHandler` 注册为 `TypeAgentProcess` MQ handler
6. ✅ 在 `send_message` handler 中检测 Agent 目标，enqueue task
7. ✅ 新增 `MessageStore.ListRecentByConversation()` 方法
8. ✅ Agent 配置目录 `agents/`（可选，默认无 Agent）
9. ✅ 客户端新增 `isAgentUser()` helper（仅 UI 层）

**协议改动**：**零**

### Phase 2: 生产化（预计 1 周）

**目标**：优化和监控

**任务**：

1. ✅ Token 计数裁剪（集成 tiktoken-go）
2. ✅ per-conversation worker 串行队列
3. ✅ 配置热更新（`reload_agents` RPC）
4. ✅ 并发控制（semaphore）和超时配置
5. ✅ 监控和日志（LLM 调用延迟、token 使用量）

**可选增强**：

- `agent_status` ephemeral push（thinking/tool_calling 状态）
- System prompt 动态注入（当前时间、用户信息等）

### Phase 3: 高级功能（可选）

**目标**：扩展 Agent 能力

**任务**：

1. ✅ Sub-agents（DeepAgent）
2. ✅ 自定义 tools（天气查询、数据库查询等）
3. ✅ Graph orchestration（复杂工作流）
4. ✅ MCP 桥接（如果需要使用 MCP tools）

---

## 9. 产品决策

建议新增以下产品决策：

### D-054: Agent UserID 命名约定

Agent 使用 `agent/<agent-id>` 格式的 UserID。`agent/` 前缀为系统保留命名空间。Agent 在协议层与普通用户完全等价，客户端通过前缀识别。

### D-055: Agent 消息格式复用

Agent 的消息与普通用户消息格式完全相同。不新增 Message.Type 枚举值，不新增 Package 类型。Agent 通过 `agent/` 前缀的 UserID 标识。

### D-056: Agent 流式输出复用 stream_text

Agent 的流式输出完全复用 `stream_text` RPC（D-051）和累积文本模式。客户端通过 broadcast payload 中的 `user_id` 前缀判断来源。

### D-057: Agent 思考状态复用 set_typing

Agent 的思考状态使用 `set_typing` RPC（D-050）。客户端通过 `user_id` 前缀区分 "typing" 和 "thinking" 的 UI 展示。

### D-058: Agent 配置格式

Agent 通过 YAML 文件定义，存放于 `agents/` 目录。server 启动时加载。配置文件包含 id、name、model、system_prompt_file、parameters 等。

### D-059: Agent 框架选型

使用 Eino 框架（github.com/cloudwego/eino）作为 AI Agent 的核心框架。Eino 提供 ChatModelAgent、DeepAgent、graph orchestration、streaming、tools、sessions 等能力，Go 原生集成。

### D-060: Agent 上下文策略

Agent 使用 DB 存储 + 内存缓存的上下文管理策略。从 `messages` 表读取历史消息，使用 Token 计数裁剪（fallback 为固定消息数）。per-conversation worker 串行处理保证一致性。

---

## 10. 关键文件清单

### 新增文件

- `internal/agent/registry.go` - Agent 配置注册表
- `internal/agent/eino_agent.go` - Eino Agent 封装
- `internal/agent/context.go` - ContextManager 接口
- `internal/agent/db_context_manager.go` - DB 实现
- `internal/agent/task_handler.go` - AgentTaskHandler
- `internal/agent/broadcast.go` - 流式广播辅助函数
- `internal/agent/agent.go` - Agent 核心逻辑
- `agents/` - 配置文件目录
- `pkg/client/agent.go` - 客户端 `isAgentUser()` helper

### 修改文件

- `internal/mq/mq.go` - 新增 `TypeAgentProcess` task type
- `internal/handler/send_message.go` - 检测 Agent 目标，enqueue task
- `internal/store/message.go` - 新增 `ListRecentByConversation()` 方法
- `cmd/xyncra-server/main.go` - 初始化 AgentRegistry 和 AgentTaskHandler

---

## 附录：风险与缓解

| 风险 | 缓解措施 |
|------|---------|
| LLM 调用超时/失败 | MQ 自动重试（Asynq retry 机制） |
| LLM 调用阻塞 server | MQ worker 隔离，semaphore 并发控制 |
| Token 超限 | Token 计数裁剪 + 单条消息截断 |
| 并发冲突 | per-conversation worker 串行处理 |
| Agent 配置错误 | 启动时验证，运行时忽略无效配置 |
| 内存泄漏 | 缓存 TTL 清理机制 |
| Eino 框架学习曲线 | 有中文文档和示例，社区活跃 |

---

## 下一步行动

1. ✅ 创建设计文档（本文档）
2. ⏳ 提交设计文档到 git
3. ⏳ 创建实施计划（使用 writing-plans skill）
4. ⏳ Phase 1 实施

---

**文档版本历史**：

| 日期 | 版本 | 变更 |
|------|------|------|
| 2026-07-10 | v1.0 | 初始版本，基于四位专家调研综合 |
