# Design: Full Chain E2E Test

**Date**: 2026-07-13
**Status**: Proposed
**Build tag**: `real_llm`
**File**: `internal/e2e/fullchain_e2e_test.go`

## Goal

A single integration test that exercises the **entire Xyncra business chain** end-to-end using a real LLM (qwen3.7-plus) and the real `pkg/client` XyncraClient library. Covers Phases 1-8: device connection, function registration, conversation creation, message sending, agent processing with tool calls, HITL (ask user question), streaming output, and message persistence.

## Prerequisites

- Redis at `localhost:16379` (standard E2E requirement)
- `.env.test` with valid DashScope API key (`XYNCRA_TEST_REAL_LLM_ENABLED=true`)
- Run command: `go test -tags real_llm ./internal/e2e/ -run TestFullChainE2E -v -timeout 300s`

## Implementation Order

**Decision: Fill the client gap first, then write the full chain test.**

The XyncraClient has a real functional gap — it silently drops agent ephemeral events (`agent_question`, `agent_checkpoint_created`, `agent_status`). This is not test scaffolding; it's a legitimate Phase 8 enhancement that the client needs. We fill this gap first because:

1. The full chain test depends on observing HITL events via the client
2. The gap exists regardless of whether we write this test
3. The client extension is small and follows existing patterns (`TypingHandler`, `StreamingHandler`)

### Step 1: Client Extension (prerequisite)

Add `AgentQuestionHandler`, `AgentCheckpointHandler`, `AgentStatusHandler` interfaces to `pkg/client/options.go` and extend `notifyHandler()` in `pkg/client/sync.go`. See Section "Client Extension" below.

### Step 2: HITL Tool (test code)

Implement `askUserQuestionTool` using `tool.Interrupt(ctx, info)` in the test file. Register it in `DefaultRegistry`. See Section "HITL Tool" below.

### Step 3: Full Chain Test

Write `TestFullChainE2E` that uses the real `XyncraClient` with the new handler interfaces, exercises the entire chain from connection to final message.

---

## Architecture Overview

```
┌────────────────────┐         ┌──────────────────────────────────────────┐
│  XyncraClient      │  WS     │  Test Server (setupAgentE2E)             │
│  (real client lib) │◄───────►│                                          │
│                    │         │  WebSocket Server                        │
│  - SQLite local DB │         │  ├─ Message Handlers (send_message, etc) │
│  - RegisterRequest │         │  ├─ MQ Broker (Asynq + Redis)           │
│    Handler (mock   │         │  ├─ Agent Executor                      │
│    functions)      │         │  │  ├─ DynamicToolProvider              │
│  - StreamingHandler│         │  │  ├─ StreamBridge                    │
│  - AgentQuestion   │         │  │  └─ BroadcastHelper                  │
│    Handler (NEW)   │         │  ├─ FunctionRegistry                    │
│  - ReverseRPC via  │         │  ├─ ReverseRPC                          │
│    client.Call()   │         │  └─ Real LLM (DashScope qwen3.7-plus)  │
└────────────────────┘         └──────────────────────────────────────────┘
```

## Test Flow (Single Function: `TestFullChainE2E`)

### Step 1: Environment Setup

```go
env := setupAgentE2E(t)
```

Extends base E2E env with: mock LLM (replaced by real LLM config), AgentRegistry, AgentExecutor, BroadcastHelper, ContextManager, Redis-backed stores.

**Modifications needed**:
- Write agent YAML config with real LLM settings + `enable_client_tools: true` + `tools: [ask_user_question]`
- Register `ask_user_question` tool in the tool registry (see Section: HITL Tool)

### Step 2: Client Connection & Function Registration

```go
// Create XyncraClient
db := store.NewInMemory(t)
client, _ := client.New(
    client.WithServerURL("ws://" + env.addr + "/ws"),
    client.WithUserID(userID),
    client.WithDeviceID(deviceID),
    client.WithDB(db),
    client.WithUpdateHandler(testUpdateHandler),
)
go client.Start(ctx)

// Wait for connection
waitForClientConnected(t, client)

// Register 5 mock functions via RPC
client.Call(ctx, "system.register_functions", map[string]any{
    "device_id":   deviceID,
    "device_name": "Test Device",
    "device_type": "desktop",
    "functions": []protocol.FunctionInfo{
        {Name: "add_item", Description: "Add an item to todo list", ...},
        {Name: "delete_item", Description: "Delete an item from todo list", ...},
        {Name: "edit_item", Description: "Edit an item in todo list", ...},
        {Name: "get_item", Description: "Get an item by ID", ...},
        {Name: "list_items", Description: "List all items", ...},
    },
})

// Register request handlers for each function (ReverseRPC targets)
client.RegisterRequestHandler("add_item", func(ctx, req) (json.RawMessage, error) {
    return json.RawMessage(`{"id":"item-1","status":"added"}`), nil
})
// ... similar for delete_item, edit_item, get_item, list_items
```

### Step 3: Create Conversation

```go
resp, _ := client.CreateConversation(ctx, "agent/fullchain-bot", "Full Chain Test")
convID := extractConvID(resp)
```

### Step 4: Send Message (Triggers Agent via MQ)

```go
client.SendMessage(ctx, convID,
    "Please add 'Buy milk' to my todo list, then list all items. "+
    "Before finishing, ask me for confirmation.",
    "msg-fc-001", 0)
```

This triggers:
1. Server persists message
2. MQ fan-out (`TypeSendMessage`) → broadcasts message echo to client
3. Agent detection → MQ `TypeAgentProcess` → AgentExecutor.Execute()

### Step 5: Observe Agent Processing (Streaming + Tool Calls)

The test observes events via `testUpdateHandler` callbacks:

```go
type testUpdateHandler struct {
    mu             sync.Mutex
    streamingTexts []string  // streaming chunks received
    typingEvents   []bool    // typing on/off
    agentQuestions []string  // agent_question payloads
    checkpoints    []string  // checkpoint_created payloads
    messages       []string  // final persisted messages
    streamDone     bool      // is_done received
}
```

Expected event sequence:
1. **typing_start** → `OnTyping(userID, convID, true)`
2. **streaming** → `OnStreaming(userID, convID, streamID, text, false)` (multiple times)
3. Agent calls `add_item` → ReverseRPC to client → handler returns mock response
4. Agent calls `list_items` → ReverseRPC to client → handler returns mock response
5. Agent calls `ask_user_question` → **HITL interrupt**
6. **agent_question** → `OnAgentQuestion(...)` (NEW handler)
7. **agent_checkpoint_created** → `OnAgentCheckpoint(...)` (NEW handler)
8. **streaming is_done** → `OnStreaming(..., isDone=true)`

### Step 6: HITL Resume

Test extracts `checkpoint_id` and `interrupt_id` from the `agent_question` event, then sends:

```go
client.Call(ctx, "agent_resume", map[string]any{
    "conversation_id": convID,
    "checkpoint_id":   checkpointID,
    "interrupt_id":    interruptID,
    "answer":          "Yes, confirmed. You can finish now.",
    "agent_id":        "agent/fullchain-bot",
})
```

This triggers:
1. Server enqueues `TypeAgentResume` MQ task
2. Resume handler loads checkpoint from Redis
3. Agent resumes → `tool.GetResumeContext[string](ctx)` returns "Yes, confirmed..."
4. Agent continues processing (possibly calls `delete_item` or produces final text)

### Step 7: Observe Completion

After resume:
1. More **streaming** events
2. **streaming is_done**
3. **message** update (agent's final response persisted to DB)

### Step 8: Assertions

```go
// 1. Functions were registered
assert.True(t, env.funcRegistry.GetFunctions(userID, deviceID) has 5 functions)

// 2. Tool calls happened (ReverseRPC was invoked)
assert.True(t, add_item_called)
assert.True(t, list_items_called)

// 3. Streaming events received
assert.True(t, len(streamingTexts) > 0)
assert.True(t, streamDone)

// 4. HITL interrupt occurred
assert.True(t, len(agentQuestions) > 0)
assert.True(t, len(checkpoints) > 0)

// 5. Agent resumed and completed
assert.True(t, len(messages) > 0)  // final agent message in DB

// 6. Message format correct
assert.Equal(t, "agent/fullchain-bot", agentMsg.SenderID)
assert.Equal(t, convID, agentMsg.ConversationID)
assert.NotEmpty(t, agentMsg.Content)
```

---

## New Code Required

### 1. HITL Tool: `ask_user_question`

**File**: `internal/e2e/fullchain_e2e_test.go` (or `internal/agent/tools/hitl_ask_user.go` if reusable)

Based on research findings, this tool uses `tool.Interrupt(ctx, info)` and `tool.GetResumeContext[string](ctx)`:

```go
type askUserQuestionTool struct{}

func (t *askUserQuestionTool) Info(ctx context.Context) (*tool.Info, error) {
    return &tool.Info{
        Name: "ask_user_question",
        Desc: "Ask the user a question and wait for their response. " +
              "Use this when you need confirmation or clarification from the user.",
        ParamsOneOf: tool.NewParamsOneOfFromSchemas(map[string]*schema.Schema{
            "question": {
                Type: schema.String,
                Desc: "The question to ask the user",
            },
        }, []string{"question"}),
    }, nil
}

func (t *askUserQuestionTool) InvokableRun(
    ctx context.Context, input string, opts ...tool.Option,
) (string, error) {
    // Parse input to extract question
    var params struct {
        Question string `json:"question"`
    }
    json.Unmarshal([]byte(input), &params)

    // Check if this is a resume flow
    if wasInterrupted, _, _ := tool.GetInterruptState[any](ctx); wasInterrupted {
        if isResume, hasData, data := tool.GetResumeContext[string](ctx); isResume && hasData {
            return fmt.Sprintf("User answered: %s", data), nil
        }
        // Re-interrupt if no resume data yet
        return "", tool.Interrupt(ctx, params.Question)
    }

    // First call: trigger interrupt
    return "", tool.Interrupt(ctx, params.Question)
}
```

**Registration**: Register in `DefaultRegistry` in the test setup:

```go
agenttools.DefaultRegistry.Register("ask_user_question", func(ctx context.Context, config map[string]any) (tool.BaseTool, error) {
    return &askUserQuestionTool{}, nil
})
```

### 2. Client Extension: `AgentQuestionHandler`

**File**: `pkg/client/options.go`

Add new handler interfaces (following the existing `TypingHandler`/`StreamingHandler` pattern):

```go
// AgentQuestionHandler is an optional interface that UpdateHandler
// implementations may adopt to receive agent HITL question events.
type AgentQuestionHandler interface {
    OnAgentQuestion(ctx context.Context, userID, conversationID, question, checkpointID, interruptID string) error
}

// AgentCheckpointHandler is an optional interface for checkpoint created events.
type AgentCheckpointHandler interface {
    OnAgentCheckpointCreated(ctx context.Context, userID, conversationID, checkpointID string) error
}

// AgentStatusHandler is an optional interface for agent status events.
type AgentStatusHandler interface {
    OnAgentStatus(ctx context.Context, userID, conversationID, status string) error
}
```

**File**: `pkg/client/sync.go` — extend `notifyHandler()`:

```go
case protocol.UpdateTypeAgentQuestion:
    var qp agentQuestionPayload
    if err := json.Unmarshal(update.Payload, &qp); err == nil {
        if qh, ok := sm.handler.(AgentQuestionHandler); ok {
            _ = qh.OnAgentQuestion(ctx, qp.UserID, qp.ConversationID,
                qp.Question, qp.CheckpointID, qp.InterruptID)
        }
    }
case protocol.UpdateTypeAgentCheckpointCreated:
    var cp agentCheckpointPayload
    if err := json.Unmarshal(update.Payload, &cp); err == nil {
        if ch, ok := sm.handler.(AgentCheckpointHandler); ok {
            _ = ch.OnAgentCheckpointCreated(ctx, cp.UserID, cp.ConversationID, cp.CheckpointID)
        }
    }
case protocol.UpdateTypeAgentStatus:
    var sp agentStatusPayload
    if err := json.Unmarshal(update.Payload, &sp); err == nil {
        if sh, ok := sm.handler.(AgentStatusHandler); ok {
            _ = sh.OnAgentStatus(ctx, sp.UserID, sp.ConversationID, sp.Status)
        }
    }
```

### 3. Agent YAML Config

Written in test setup:

```yaml
---
id: fullchain-bot
name: Full Chain Test Bot
model: qwen3.7-plus
api_key_env: XYNCRA_TEST_REAL_API_KEY
base_url: https://coding.dashscope.aliyuncs.com/v1
parameters:
  temperature: 0.3
  max_tokens: 1000
context:
  max_tokens: 8000
  max_messages: 10
middleware:
  enable_client_tools: true
tools:
  - ask_user_question
---
You are a helpful assistant with access to the user's todo list functions
(add_item, delete_item, edit_item, get_item, list_items).
You also have an ask_user_question tool for asking the user for confirmation.

When the user asks you to do something that requires confirmation,
use ask_user_question before proceeding.
Always use the available tools to complete the user's request step by step.
Keep your responses concise.
```

### 4. Test Update Handler

```go
type fullChainUpdateHandler struct {
    mu             sync.Mutex
    streamingTexts []streamingEvent
    typingEvents   []typingEvent
    questions      []questionEvent
    checkpoints    []checkpointEvent
    messages       []*model.Message
    streamDone     bool
}

type streamingEvent struct {
    UserID, ConvID, StreamID, Text string
    IsDone                         bool
}
// ... similar for other event types

func (h *fullChainUpdateHandler) OnMessage(ctx context.Context, msg *model.Message) error {
    h.mu.Lock()
    defer h.mu.Unlock()
    h.messages = append(h.messages, msg)
    return nil
}

func (h *fullChainUpdateHandler) OnStreaming(ctx context.Context, userID, convID, streamID, text string, isDone bool) error {
    h.mu.Lock()
    defer h.mu.Unlock()
    h.streamingTexts = append(h.streamingTexts, streamingEvent{userID, convID, streamID, text, isDone})
    if isDone {
        h.streamDone = true
    }
    return nil
}

// ... OnTyping, OnAgentQuestion, OnAgentCheckpointCreated
```

---

## Mock Functions

| Function | Description | Parameters (JSON Schema) | Mock Response |
|----------|-------------|-------------------------|---------------|
| `add_item` | Add an item to todo list | `{name: string}` | `{"id":"item-1","status":"added"}` |
| `delete_item` | Delete an item from todo list | `{id: string}` | `{"status":"deleted"}` |
| `edit_item` | Edit an item in todo list | `{id: string, name: string}` | `{"id":"item-1","status":"updated"}` |
| `get_item` | Get an item by ID | `{id: string}` | `{"id":"item-1","name":"Buy milk","done":false}` |
| `list_items` | List all items | `{}` | `[{"id":"item-1","name":"Buy milk"}]` |

---

## Design Decisions

### D-1: HITL tool is server-side, not a client function

**Rationale**: `tool.Interrupt(ctx, info)` must be called within the Eino tool execution context (same goroutine as the agent runner). A client function goes through ReverseRPC — the actual execution happens on the client device, not in the Eino tool context. The interrupt error would not propagate correctly through the ReverseRPC boundary.

**Decision**: `ask_user_question` is a built-in server-side tool registered in `DefaultRegistry`. It coexists with DynamicToolProvider-injected client functions.

### D-2: Client extension for agent ephemeral events

**Rationale**: The XyncraClient's `notifyHandler()` silently drops `agent_question`, `agent_checkpoint_created`, and `agent_status` events. This is a gap in the Phase 8B client implementation. Adding handler interfaces follows the existing pattern (`TypingHandler`, `StreamingHandler`) and is a natural, small enhancement.

**Decision**: Add `AgentQuestionHandler`, `AgentCheckpointHandler`, `AgentStatusHandler` interfaces to `pkg/client/options.go` and extend `notifyHandler()` in `pkg/client/sync.go`.

### D-3: Use `client.Call()` for function registration

**Rationale**: The `XyncraClient.reregisterFunctions()` is still a placeholder. Rather than implementing the full function manifest API (out of scope), the test directly calls `system.register_functions` via `client.Call()`. This is the same approach used by `mockClientDevice.registerFunctions()`.

### D-4: Real LLM with retry

**Rationale**: Real LLM responses are non-deterministic. The test uses `retryRealLLM(t, 2, fn)` for transient failures (timeouts, rate limits). Assertions are fuzzy (structural, not content-matching).

### D-5: Agent prompt instructs tool usage and HITL

**Rationale**: The system prompt explicitly instructs the agent to:
1. Use the available client functions (add_item, etc.)
2. Use ask_user_question when confirmation is needed
3. Keep responses concise

This guides the real LLM to exercise both the tool call chain and the HITL mechanism.

### D-6: Implementation order — fill client gap first, then write test

**Rationale**: The XyncraClient has a real functional gap — it silently drops agent ephemeral events (`agent_question`, `agent_checkpoint_created`, `agent_status`). This is not test scaffolding; it's a legitimate Phase 8 enhancement that the client needs regardless. We fill this gap first because:

1. The full chain test depends on observing HITL events via the client
2. The gap exists independently of this test
3. The client extension is small and follows existing patterns (`TypingHandler`, `StreamingHandler`)

**Decision**: Two-step implementation:

1. **Step 1**: Add `AgentQuestionHandler`, `AgentCheckpointHandler`, `AgentStatusHandler` interfaces to `pkg/client/options.go`, extend `notifyHandler()` in `pkg/client/sync.go`
2. **Step 2**: Write `TestFullChainE2E` with HITL tool and full chain test

---

## Phases Covered

| Phase | What's Tested | How |
|-------|--------------|-----|
| Phase 1 | Device connection model + reqID UUID | Client connects with `device_id`, server registers `(userID, deviceID)` |
| Phase 2 | Function manifest + ClientFunctionRegistry | `client.Call("system.register_functions", ...)` with 5 functions |
| Phase 3 | Send feedback + disconnect fail | `client.SendMessage()` gets response; ReverseRPC to client functions |
| Phase 4 | Idempotency key + Redis persistence | Handled by agent task handler (Redis SETNX) |
| Phase 5 | Reconnect handshake + request replay | Client performs reconnect handshake on initial connect |
| Phase 6 | DynamicToolProvider middleware | Agent's LLM receives client functions as tools, calls them via ReverseRPC |
| Phase 7 | Agent YAML config + E2E integration | Agent loaded from YAML with `enable_client_tools: true` |
| Phase 8 | Client-side enhancements + adaptive timeout | Real XyncraClient with idempotency cache, RTT tracker, streaming handler |

---

## Risk Factors & Mitigations

| Risk | Impact | Mitigation |
|------|--------|-----------|
| Real LLM doesn't call tools | HITL not triggered | System prompt explicitly instructs tool use; retry up to 2 times |
| Real LLM doesn't call ask_user_question | HITL not triggered | Prompt explicitly says "ask me for confirmation"; if LLM doesn't, test still validates the rest of the chain |
| Tool.Interrupt propagation fails | HITL checkpoint not created | Framework-level feature (verified in Eino ADK tests); if fails, test fails with clear error |
| agent_question events not received | Test can't detect HITL | Client extension adds AgentQuestionHandler; test also verifies checkpoint_created |
| LLM timeout / rate limit | Test fails | retryRealLLM with 2 attempts; configurable timeout via XYNCRA_TEST_REAL_LLM_TIMEOUT |
| Non-deterministic LLM output | Assertions fail | Fuzzy assertions: check structure, not content; check tool was called, not specific arguments |

---

## Files Changed/Created

| File | Change |
|------|--------|
| `internal/e2e/fullchain_e2e_test.go` | **NEW** — Full chain E2E test + HITL tool + test update handler |
| `pkg/client/options.go` | **MODIFY** — Add `AgentQuestionHandler`, `AgentCheckpointHandler`, `AgentStatusHandler` interfaces |
| `pkg/client/sync.go` | **MODIFY** — Extend `notifyHandler()` with agent ephemeral event cases |

---

## Alternative Approaches Considered

### A1: Use mockClientDevice instead of XyncraClient
**Rejected**: User explicitly requested real XyncraClient for maximum fidelity.

### A2: Skip HITL, only test tool calls + streaming
**Rejected**: User explicitly requested HITL (ask user question) as part of the chain.

### A3: Client function for HITL (ask_user as ReverseRPC)
**Rejected**: `tool.Interrupt()` must execute in the Eino tool context. ReverseRPC sends execution to the client device; the interrupt error wouldn't propagate back to the Runner correctly.

### A4: Separate tests for HITL and tool calls
**Rejected**: User explicitly requested a SINGLE test covering the entire chain.
