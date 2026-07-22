// Package agent provides the AI agent configuration and registry system.
//
// Agents are defined using Markdown files with YAML front matter. The front
// matter contains configuration (ID, model, parameters), and the body is
// the agent's system prompt.
//
// Agent identity is determined by exact-match lookup in the AgentRegistry;
// the "agent/{id}" format is a convention, not enforced by the server (D-054 revised).
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
// typing indicators and streaming updates to conversation participants via
// the SyncBroadcast hub. SendStreamUpdate broadcasts to both the human user
// and the agent user; SendTyping broadcasts to the human user only. Cumulative
// text snapshots use is_done=false during streaming and is_done=true on
// completion (D-052). Typing indicators are ephemeral: sent true before agent
// work begins and cleared either on the first token (D-065) or on exit.
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
// AgentProcessPayload, validates required fields, checks two-phase idempotency
// (D-121, fail-open on Redis errors), and calls
// AgentExecutor.ExecuteWithErrorMessage. On normal completion the handler
// invalidates the conversation context cache so that retried tasks (e.g.
// ones that were requeued because the conversation lock was held) load fresh
// messages from the database. The handler returns nil for permanent errors
// (bad payload, agent not found — retry won't help) and returns an error for
// transient failures (LLM timeout, rate limit) or when the conversation lock
// is held by another task (Asynq retries with exponential backoff until the
// lock is released).
//
// Two-phase idempotency (D-121):
//
//	The handler uses two-phase idempotency:
//	  1. agent:processing:{messageID} (TTL 130s): SETNX before execution, prevents concurrent duplicates
//	  2. agent:processed:{messageID} (TTL 24h): SET after success, prevents replay duplicates
//
//	On crash: the processing key expires after 130s, allowing Asynq retry to re-execute.
//	On HITL interrupt: the processing key expires naturally, no processed key is set.
//	On transient error: the processing key expires naturally, error returned for Asynq retry.
//
// Resume handler idempotency (D-121):
//
//	The resume handler uses the same two-phase pattern:
//	  1. agent:resume:processing:{checkpointID} (TTL 130s): SETNX before execution
//	  2. agent:resume:{checkpointID} (TTL 24h): SET after successful resume
//
//	On HITL re-interrupt: the processing key is deleted to allow subsequent resume.
//	On transient error: the processing key is deleted to allow immediate retry.
//	On permanent failure: processing key deleted + processed key set (24h) to prevent replay.
//
// RedisIdempotencyStore: Redis-based deduplication using SETNX with TTL. The
// IdempotencyStore interface provides atomic check-and-set semantics. The key
// formats are "agent:processing:{messageID}" (130s), "agent:processed:{messageID}"
// (24h), "agent:resume:processing:{checkpointID}" (130s), and
// "agent:resume:{checkpointID}" (24h). Fail-open: if Redis is unavailable,
// processing continues (logged but not blocked).
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
//	MQ task → AgentTaskHandler → two-phase idempotency check (D-121)
//	→ AgentExecutor.ExecuteWithErrorMessage → typing=true broadcast
//	→ context loading (DBContextManager) → agent building (AgentBuilder)
//	→ LLM streaming (Eino Runner) → stream bridge (cumulative snapshots)
//	→ broadcast chunks (BroadcastHelper) → is_done=true broadcast
//	→ persist message → return nil to MQ (D-067)
//
// # Phase 6: Client Device Function Invocation
//
// DynamicToolProvider (dynamic_tool_provider.go): DynamicToolProvider is an Eino
// ChatModelAgentMiddleware that dynamically injects client-device functions as
// InvokableTool instances before each agent run. BeforeAgent extracts the
// CallerDevice from context, queries the device's registered functions via
// ClientFunctionProvider, applies tag and exclusion filters, creates tools via
// newClientFunctionTool, and merges them into runCtx.Tools. All errors are
// fail-open: logged and skipped, never blocking agent execution (D-072 spirit).
//
// ClientFunctionProvider (dynamic_tool_provider.go): Interface for retrieving
// function declarations registered by a client device. Defined here to avoid
// circular dependency on the server package (D-101).
//
// CallerDevice (context_keys.go): CallerDevice holds the UserID and DeviceID of
// the client device that initiated the conversation. It is injected into the
// context by AgentExecutor.Execute and AgentResumeHandler when DeviceID is
// present in the payload. DynamicToolProvider reads it from context to determine
// which device's functions to expose as tools (D-102).
//
// ClientToolsConfig: Per-agent configuration for dynamic client tool injection.
// FunctionTags filters which functions are exposed (empty = all). ExcludedFunctions
// is a deny-list checked first. CallTimeout sets the default timeout for client
// function calls. Zero/negative CallTimeout defaults to 30 seconds.
//
// AgentBuilder Integration: AgentBuilder exposes SetClientFunctionProvider.
// When configured and an agent's Middleware config has EnableClientTools=true,
// buildMiddleware creates a DynamicToolProvider and inserts it as the first
// middleware (before PatchToolCalls, Summarization, and ToolReduction) so
// injected tools are visible to all downstream middleware.
//
// Client functions use the RemoteCalling interrupt-resume pattern (D-137):
// tool.Interrupt triggers an interrupt, executor creates a RemoteCalling record,
// client processes it asynchronously, and agent resumes with the result.
//
// # Phase 7: Production Hardening
//
// Semaphore (semaphore.go): Semaphore limits concurrent agent executions using
// a channel-based counter. Capacity > 0 creates a bounded semaphore; capacity
// <= 0 returns an unlimited semaphore where Acquire always succeeds. Tracks
// active count, peak usage, and total acquisitions via Stats(). When the
// executor is created with maxConcurrent > 0, a Semaphore is attached to
// bound parallel LLM calls.
//
// ConversationLock (conversation_lock.go, D-075): ConversationLock is the
// interface for per-conversation distributed locking. RedisConversationLock
// implements it via Redis SETNX with a configurable TTL (default 130s, covering
// the 120s total timeout plus buffer). Release uses a Lua script that verifies
// the lock value before deletion, preventing one owner from releasing another's
// lock. Fail-open: Redis errors are logged but do not block execution. When a
// task fails to acquire the lock it returns an error so Asynq requeues it with
// exponential backoff; once the active task finishes and invalidates the context
// cache, the retried task loads the latest messages (including those queued
// while the lock was held) and proceeds normally. The lock reuses the same
// dedicated redis.Client as the idempotency store (D-074).
//
// LLMMetrics (monitoring.go): LLMMetrics is the interface for recording LLM
// call metrics (agent ID, model, duration, token counts, error). LogMetrics
// is the default implementation that logs each call event via the structured
// Logger. The executor records a metrics event around every agent Build step
// when WithLLMMetrics is configured.
//
// StartCleanup (db_context_manager.go): DBContextManager.StartCleanup begins a
// background goroutine that periodically evicts expired entries from the
// in-memory conversation context cache (sync.Map). It runs until the parent
// context is cancelled. Cache cleanup is independent of server lifecycle and
// ensures stale contexts do not accumulate memory indefinitely.
//
// Reload (registry.go, D-076, D-077): AgentRegistry.Reload re-scans the
// directory previously passed to Load and replaces all agent configurations
// atomically. The reload_agents RPC handler (D-076) invokes Reload to support
// runtime hot-reloading of agent configs without server restart. AgentRegistry
// also exposes a Dir() method returning the directory path (used by the reload
// handler for error messages) and a SetLogger method to wire in the server's
// structured logger (Phase 7 review).
//
// Structured Logger (executor.go): Logger is a structured logging interface
// (Info, Error, Debug) compatible with server.Logger. All agent components
// (AgentExecutor, AgentRegistry, BroadcastHelper, AgentTaskHandler) accept a
// Logger and default to noopLogger when nil is provided. This replaces
// ad-hoc *log.Logger usage and provides consistent key-value log output.
//
// Functional Options (executor.go): ExecutorOption is the functional options
// pattern for AgentExecutor configuration. WithTotalTimeout overrides the
// default 120s total execution timeout. WithTypingTimeout overrides the default
// 60s wait for the first LLM token. WithLLMMetrics attaches an LLMMetrics
// recorder. All options ignore non-positive durations (zero/negative) to
// preserve safe defaults.
//
// # Phase 8A: Tool System and Middleware
//
// Tool Registry (D-078): The tools sub-package provides a Registry that maps
// tool names to ToolFactory functions. Built-in tools (get_weather,
// get_current_time, retrieve_tool_result) are registered in DefaultRegistry
// via init(). Agent configs reference tools by name in the YAML tools list.
// Unknown tool names are logged and skipped (fail-open).
//
// Built-in Tools: get_weather returns mock weather data, get_current_time
// returns the current time in a given timezone, retrieve_tool_result retrieves
// full content of previously truncated tool results from the in-memory
// ToolResultStore (D-080).
//
// ToolResultStore (D-080): Stores truncated tool results in memory with TTL
// (default 1 hour). UTF-8-safe rune-based truncation at 50000 characters.
// A background cleanup goroutine (StartCleanup) removes expired entries
// periodically.
//
// Middleware Chain (D-079): buildMiddleware constructs the middleware chain
// from AgentConfig.Middleware settings. Fixed order: PatchToolCalls →
// Summarization → ToolReduction. Each middleware init failure is logged and
// skipped (fail-open). Summarization uses the agent's chat model; ToolReduction
// uses an in-memory filesystem backend.
//
// Enhanced Build(): AgentBuilder.Build() now creates tools from the registry
// (when set via SetToolRegistry) and builds the middleware chain, passing both
// to adk.ChatModelAgentConfig.
//
// # Phase 8B: RemoteCalling Cleanup Task
//
// RemoteCallingCleanupTask (remote_calling_cleanup.go, D-123, D-137):
// RemoteCallingCleanupTask is a background goroutine that periodically cleans
// up conversations stuck in asking_user/tool_calling status and individual
// expired RemoteCallings. Two-layer cleanup: (1) conversation-level scans
// conversations exceeding MaxAge (default 24h) and performs full cleanup
// (clear status, soft-delete RemoteCallings, delete checkpoint, send timeout
// message, broadcast agent_timeout); (2) RemoteCalling-level scans individual
// RemoteCallings with expires_at < NOW() and marks them expired. When all
// RemoteCallings for a checkpoint expire (pending=0), the task enqueues a
// TypeAgentResume MQ task so the agent can handle the timeout gracefully.
// All cleanup steps are non-fatal (D-007): errors are logged but do not
// interrupt processing of other conversations. Distributed locking via Redis
// SETNX prevents duplicate cleanup across nodes.
//
// # Phase 8C: MCP Integration
//
// MCPBridge (D-086): MCPBridge manages connections to external MCP (Model
// Context Protocol) servers. It supports SSE (Server-Sent Events) and stdio
// transports. ConnectSSE and ConnectStdio perform the MCP handshake and tool
// discovery, returning Eino tool.BaseTool slices that integrate seamlessly
// with the agent's tool set. The bridge tracks all client connections for
// lifecycle management via CloseAll.
//
// MCPServerConfig: Agent configs reference MCP servers in the mcp_servers
// YAML list. Each entry specifies a name, transport ("sse" or "stdio"),
// connection details (url for SSE, command/args/env for stdio), and optional
// tools filter to restrict which tools are exposed to the agent.
//
// Build() Integration: AgentBuilder.Build() connects to all configured MCP
// servers during agent construction. Connection failures are logged and
// skipped (fail-open), ensuring the agent still starts even if an MCP server
// is unavailable. The MCP tools are appended to the agent's tool set.
//
// Shutdown: main.go calls mcpBridge.CloseAll() after srv.GracefulStop() to
// release all MCP client connections once in-flight requests have finished.
package agent
