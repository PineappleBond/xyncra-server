// Package agent provides the AI agent configuration and registry system.
//
// Agents are defined using Markdown files with YAML front matter. The front
// matter contains configuration (ID, model, parameters), and the body is
// the agent's system prompt.
//
// Agent userIDs follow the format "agent/{id}" (D-054).
//
// # Context Management
//
// The ContextManager interface provides conversation history loading with
// token-based trimming and in-memory caching. DBContextManager is the
// default implementation, backed by the MessageStore with sync.Map caching.
//
// # Phase 4: LLM Provider and Agent Building
//
// LLMProvider and LLMClientFactory (D-064, D-066): LLMProvider is the interface
// each backend (OpenAI, Claude, Ollama, Qwen) implements to construct a
// ChatModel. LLMClientFactory holds provider registrations and selects the
// right one via model-name / base-URL heuristics (detectProvider). Each
// provider supplies a DefaultBaseURL so agents work with zero configuration
// (D-064). API keys are read from environment variables and never appear in
// error messages or logs.
//
// AgentBuilder and BuiltAgent: AgentBuilder wraps LLMClientFactory and produces
// a BuiltAgent (an Eino Runner + the config it was built from). Build performs
// three steps: create a ChatModel via the factory, wrap it in a ChatModelAgent
// with the agent's system prompt as instruction, then create a Runner with
// streaming enabled.
//
// # Phase 4: Streaming and Broadcasting
//
// StreamBridge (D-051, 50ms throttle, cumulative text): StreamBridge converts
// Eino AsyncIterator events into StreamChunks. Each StreamChunk.Content is a
// cumulative text snapshot (not a delta), so receivers replace their display
// buffer directly without maintaining state. A 50ms throttle yields ~20fps
// streaming; dropped frames do not affect correctness. The event-reading
// goroutine wraps iter.Next() in a select so it is cancellable via context.
//
// BroadcastHelper (D-050 ephemeral, dual broadcast): BroadcastHelper sends
// typing indicators and streaming updates to the human user via the
// SyncBroadcast hub. SendStreamUpdate broadcasts cumulative text snapshots
// with is_done=false during streaming and is_done=true on completion (D-052).
// Typing indicators are ephemeral: sent true before agent work begins and
// cleared either on the first token (D-065) or on exit.
//
// # Phase 4: Execution Pipeline
//
// AgentExecutor (D-062, D-065, D-067 orchestration pipeline): AgentExecutor
// orchestrates the full execution pipeline: acquire semaphore (optional
// concurrency limit), apply 120s total timeout, look up agent config from
// registry, send typing=true, load conversation context, build the agent,
// convert messages to Eino schema, run the agent, bridge the stream, broadcast
// chunks, send is_done=true, and persist the final message. ExecuteWithErrorMessage
// wraps Execute and sends a user-friendly Chinese error message on failure
// (D-067), classifying sentinel errors into appropriate responses.
//
// # Phase 5: MQ Integration and Idempotency
//
// AgentTaskHandler (task_handler.go): AgentTaskHandler is the MQ task handler
// adapter layer that converts MQ tasks to ExecutePayload. It unmarshals the
// AgentProcessPayload, validates required fields, checks idempotency via Redis
// SETNX (24h TTL, fail-open on Redis errors), and calls
// AgentExecutor.ExecuteWithErrorMessage. The handler always returns nil to MQ
// (D-067: errors are persisted as user-friendly messages, so retry won't help).
//
// RedisIdempotencyStore: Redis-based deduplication using SETNX with TTL. The
// IdempotencyStore interface provides atomic check-and-set semantics. The key
// format is "agent:processed:{messageID}" with a 24-hour TTL. SetNX returns
// true if the key was set (first time), false if it already existed (duplicate).
// Fail-open: if Redis is unavailable, processing continues (logged but not blocked).
//
// # Phase 5: main.go Wiring
//
// The complete agent pipeline is wired in main.go after handler.RegisterAll:
//
//	LLMClientFactory → AgentBuilder → AgentExecutor
//	StreamBridge + BroadcastHelper + DBContextManager → AgentExecutor
//	AgentExecutor + IdempotencyStore → AgentTaskHandler → MQ registration
//
// A dedicated redis.Client is created for the idempotency store (D-Phase5-5),
// separate from the node broadcaster client. The AgentExecutor is configured
// with maxConcurrent=10 to limit parallel LLM calls.
//
// # Phase 5: Data Flow
//
// The end-to-end data flow for agent message processing:
//
//	MQ task → AgentTaskHandler → idempotency check (Redis SETNX)
//	→ AgentExecutor.ExecuteWithErrorMessage → typing=true broadcast
//	→ context loading (DBContextManager) → agent building (AgentBuilder)
//	→ LLM streaming (Eino Runner) → stream bridge (cumulative snapshots)
//	→ broadcast chunks (BroadcastHelper) → is_done=true broadcast
//	→ persist message → return nil to MQ (D-067)
package agent
