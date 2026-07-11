# Agent E2E Test Scenarios (Phase 1-8)

> Date: 2026-07-11
> Strategy: Scenario-first + Skill-driven
> Infrastructure: `internal/e2e/` (server-side), Mock LLM, in-memory SQLite, Redis DB 15

## Overview

59 test scenarios across 11 categories, covering the complete Agent system (Phase 1-8).
All tests use mock LLM (no external API key), run in-process, and verify both WebSocket protocol and DB persistence.

## Test Infrastructure

- **Mock LLM**: `internal/e2e/mock_llm_test.go` — OpenAI-compatible HTTP mock server
- **Agent Helpers**: `internal/e2e/agent_helpers_test.go` — setupAgentE2E, sendToAgent, waitForAgentReply, etc.
- **Pre-loaded Agents**: `test-bot` (no tools), `tool-bot` (with weather/time/retrieve tools)

## Category A: Basic Flow (AE-BASIC)

| ID | Scenario | Verification | Decisions |
|----|----------|-------------|-----------|
| AE-BASIC-001 | User sends message to agent → agent replies | Message persisted, broadcast sent, correct sender_id | D-054, D-055, D-062 |
| AE-BASIC-002 | Agent reply message format correct | Message.SenderID = "agent/test-bot", Content non-empty | D-055 |
| AE-BASIC-003 | Agent reply retrievable via sync_updates | Offline user reconnects and gets agent reply | D-055, D-009 |
| AE-BASIC-004 | Non-agent users unaffected | Human-to-human messages flow normally | D-062 |
| AE-BASIC-005 | Agent-to-agent does not trigger processing | Agent messaging another agent does not trigger MQ | D-062 |

**Test file**: `internal/e2e/agent_basic_test.go`

## Category B: Streaming Output (AE-STREAM)

| ID | Scenario | Verification | Decisions |
|----|----------|-------------|-----------|
| AE-STREAM-001 | Typing indicator sent before first token | Ephemeral typing update (Seq=0) | D-050, D-065 |
| AE-STREAM-002 | Streaming tokens pushed in real-time | Ephemeral streaming updates (Seq=0, cumulative text) | D-050, D-051 |
| AE-STREAM-003 | is_done flag sent correctly | Last streaming update has is_done=true | D-052 |
| AE-STREAM-004 | Typing stops after first token arrives | typing=false sent after first chunk | D-065 |
| AE-STREAM-005 | Streaming messages not persisted | Streaming ephemeral updates not in sync_updates | D-050, D-051 |
| AE-STREAM-006 | Full message persisted after streaming ends | send_message executed after streaming completes | D-052 |

**Test file**: `internal/e2e/agent_streaming_test.go`

## Category C: Tool System (AE-TOOL)

| ID | Scenario | Verification | Decisions |
|----|----------|-------------|-----------|
| AE-TOOL-001 | Agent calls registered tool | LLM returns tool_calls → tool executes → result returned to LLM | D-078 |
| AE-TOOL-002 | Tool results reflected in reply | Agent reply contains tool execution result | D-078 |
| AE-TOOL-003 | Unregistered tool name skipped | Agent config references non-existent tool → agent still starts | D-078 |
| AE-TOOL-004 | Tool result truncation | Oversized tool output truncated, reference preserved | D-080 |
| AE-TOOL-005 | Truncated result retrieval | retrieve_tool_result fetches full content | D-080 |
| AE-TOOL-006 | Truncated result TTL expiry | After expiry returns "expired" message | D-080 |

**Test file**: `internal/e2e/agent_tools_test.go`

## Category D: Context Management (AE-CTX)

| ID | Scenario | Verification | Decisions |
|----|----------|-------------|-----------|
| AE-CTX-001 | Multi-turn conversation maintains context | Second message's reply references first message content | D-060 |
| AE-CTX-002 | Token truncation works correctly | Long conversation exceeding max_tokens has old messages trimmed | D-060 |
| AE-CTX-003 | Message count fallback | max_tokens=0 falls back to max_messages trimming | D-060 |
| AE-CTX-004 | Cache hit avoids DB query | Same conversation requested multiple times in short period, only first hits DB | D-060 |
| AE-CTX-005 | Cache TTL expiry triggers reload | After cache expires, next request reloads from DB | D-060 |

**Test file**: `internal/e2e/agent_context_test.go`

## Category E: HITL (AE-HITL)

| ID | Scenario | Verification | Decisions |
|----|----------|-------------|-----------|
| AE-HITL-001 | Agent interrupts and asks question | agent_question ephemeral update sent to client | D-085, D-087 |
| AE-HITL-002 | Checkpoint saved to Redis | After checkpoint creation, Redis contains corresponding key | D-083 |
| AE-HITL-003 | Agent continues after user answers | agent_resume RPC → TypeAgentResume MQ → execution resumes | D-084, D-085 |
| AE-HITL-004 | Conversation lock held during HITL | During interrupt, new messages don't trigger new agent processing | D-084 |
| AE-HITL-005 | CheckpointStore failure aborts | When Redis unavailable, HITL aborts with error message | D-083 |
| AE-HITL-006 | agent_checkpoint_created notification | Ephemeral notification sent after checkpoint creation | D-087 |

**Test file**: `internal/e2e/agent_hitl_test.go`

## Category F: Sub-agent (AE-SUB)

| ID | Scenario | Verification | Decisions |
|----|----------|-------------|-----------|
| AE-SUB-001 | Sub-agent delegation succeeds | Parent agent config sub_agents → child agent executes → result returns to parent | D-081 |
| AE-SUB-002 | Sub-agent output merged into parent reply | Final reply contains sub-agent's work result | D-081 |
| AE-SUB-003 | Sub-agent depth limit enforced | 3-layer nesting rejected or degraded | D-081 |
| AE-SUB-004 | Non-existent sub-agent skipped | References unregistered agent ID → skipped with warning | D-081 |

**Test file**: `internal/e2e/agent_subagent_test.go`

## Category G: Middleware (AE-MW)

| ID | Scenario | Verification | Decisions |
|----|----------|-------------|-----------|
| AE-MW-001 | Summarization middleware triggers | Long conversation triggers summary compression | D-079 |
| AE-MW-002 | ToolReduction middleware triggers | Large tool results cleaned up | D-079 |
| AE-MW-003 | PatchToolCalls middleware repairs | Dangling tool calls repaired | D-079 |
| AE-MW-004 | Middleware order correct | PatchToolCalls → Summarization → ToolReduction | D-079 |
| AE-MW-005 | Middleware creation failure skipped | Invalid middleware config → agent still starts | D-079 |

**Test file**: `internal/e2e/agent_middleware_test.go`

## Category H: Concurrency & Idempotency (AE-CONC)

| ID | Scenario | Verification | Decisions |
|----|----------|-------------|-----------|
| AE-CONC-001 | Same conversation processed serially | Concurrent messages to same conv, second waits for first to complete | D-075 |
| AE-CONC-002 | Different conversations processed in parallel | Messages to different conversations processed concurrently | D-075 |
| AE-CONC-003 | Idempotency prevents duplicate execution | Same MessageID task executes only once | D-071 |
| AE-CONC-004 | Idempotency fail-open | When Redis unavailable, skip check and continue | D-072 |
| AE-CONC-005 | Semaphore limits concurrency | Tasks exceeding max concurrency queue | D-075 |
| AE-CONC-006 | Lock TTL expiry releases | After lock timeout, new tasks can acquire | D-075 |
| AE-CONC-007 | Lua script prevents accidental lock deletion | Only release locks you own | D-075 |

**Test file**: `internal/e2e/agent_concurrent_test.go`

## Category I: Error Handling (AE-ERR)

| ID | Scenario | Verification | Decisions |
|----|----------|-------------|-----------|
| AE-ERR-001 | LLM API error → error message | LLM returns error → persist "temporarily unable to reply" message | D-067 |
| AE-ERR-002 | API key missing → config error message | api_key_env not set → persist "configuration error" message | D-067 |
| AE-ERR-003 | Context loading failure → error message | DB exception → persist "cannot read conversation history" message | D-067 |
| AE-ERR-004 | Unknown error → generic error message | Unclassified error → persist "encountered issue" message | D-067 |
| AE-ERR-005 | Tool execution failure → error message | Tool panic/timeout → persist "tool call failed" message | D-082 |
| AE-ERR-006 | TaskHandler always returns nil | Even on execution failure, MQ doesn't retry | D-073 |

**Test file**: `internal/e2e/agent_error_test.go`

## Category J: Config Reload (AE-RELOAD)

| ID | Scenario | Verification | Decisions |
|----|----------|-------------|-----------|
| AE-RELOAD-001 | reload_agents loads new config | Add new agent file → reload → new agent available | D-076, D-077 |
| AE-RELOAD-002 | reload_agents removes deleted config | Delete agent file → reload → old agent unavailable | D-076 |
| AE-RELOAD-003 | reload_agents skips invalid files | Invalid YAML → skip and log, doesn't affect others | D-076 |
| AE-RELOAD-004 | Reload doesn't affect executing tasks | During reload, in-progress agent tasks complete normally | D-076 |

**Test file**: `internal/e2e/agent_reload_test.go`

## Category K: Ephemeral Updates (AE-EPH)

| ID | Scenario | Verification | Decisions |
|----|----------|-------------|-----------|
| AE-EPH-001 | agent_status thinking | agent_status=thinking sent at start of processing | D-087 |
| AE-EPH-002 | agent_status tool_calling | agent_status=tool_calling sent during tool calls | D-087 |
| AE-EPH-003 | agent_timeout notification | agent_timeout sent after processing timeout | D-087 |
| AE-EPH-004 | Ephemeral updates not persisted | All agent ephemeral updates don't appear in sync_updates | D-050, D-087 |
| AE-EPH-005 | Old clients ignore unknown types | Old clients receiving agent_status don't error | D-087 |

**Test file**: `internal/e2e/agent_ephemeral_test.go`

## Statistics

| Category | Scenarios | Test File |
|----------|-----------|-----------|
| A: Basic Flow | 5 | agent_basic_test.go |
| B: Streaming | 6 | agent_streaming_test.go |
| C: Tools | 6 | agent_tools_test.go |
| D: Context | 5 | agent_context_test.go |
| E: HITL | 6 | agent_hitl_test.go |
| F: Sub-agent | 4 | agent_subagent_test.go |
| G: Middleware | 5 | agent_middleware_test.go |
| H: Concurrency | 7 | agent_concurrent_test.go |
| I: Error Handling | 6 | agent_error_test.go |
| J: Config Reload | 4 | agent_reload_test.go |
| K: Ephemeral | 5 | agent_ephemeral_test.go |
| **Total** | **59** | **11 files** |

## Design Decisions

1. **Server-side tests (`internal/e2e/`)** — Can inject mock LLM HTTP server, directly test WebSocket protocol, verify ephemeral updates, better isolation (in-memory SQLite, random ports)
2. **Mock LLM over real API** — No external API key dependency, deterministic responses, can test error/edge scenarios
3. **Scenario-first** — Write scenario doc before code, scenarios serve as implementation checklist and review reference
4. **Skill-driven** — Sub-agents learn Eino SKILLs before implementation, reducing trial-and-error

## Code Conventions

- Comments: English, godoc style
- Errors: `fmt.Errorf` + `%w`
- Naming: follows existing patterns
- Test functions: `TestAgent<Category>_<ScenarioID>`
- Each test starts with comment: scenario ID and verification points
