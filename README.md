# Xyncra Server

**A distributed messaging backend with built-in AI agent support.**

> Bidirectional RPC over WebSocket · Multi-device sync · Offline resilience · Streaming · Human-in-the-Loop

[Architecture](#architecture) · [Quick Start](#quick-start) · [Protocol](#protocol) · [Agent System](#agent-system) · [Documentation](#documentation) · [Contributing](#contributing) · [License](#license)

---

## Why Xyncra?

Most messaging systems force you to choose between **real-time infrastructure** and **AI agent integration**. Xyncra gives you both in a single, zero-config server:

| You need...                  | Xyncra delivers                                                                       |
| ---------------------------- | ------------------------------------------------------------------------------------- |
| Real-time messaging          | WebSocket-based bidirectional RPC with server-initiated pushes                        |
| AI agents that talk to users | Built-in agent runtime with streaming, tool calls, and HITL                           |
| Multi-device everywhere      | Per-device connection tracking with offline sync and gap filling                      |
| Production resilience        | Redis-backed distributed state, MQ task queue, fail-open design                       |
| Zero operational overhead    | SQLite by default, one binary, sensible defaults for everything                       |

---

## Architecture

```text
                        ┌─────────────────────────────────┐
                        │         Your Application        │
                        │   (reverse proxy + auth layer)  │
                        └──────────────┬──────────────────┘
                                       │  injects user_id
                        ┌──────────────▼──────────────────┐
                        │         Xyncra Server           │
                        │                                 │
    ┌───────┐  WebSocket│  ┌───────────┐  ┌───────────┐  │   ┌───────┐
    │ User A│◄─────────►│  │  Conn     │  │  Agent    │  │◄─►│ Redis │
    │ Device│  RPC+Push │  │  Store    │  │  Runtime  │  │   └───┬───┘
    └───────┘           │  └─────┬─────┘  └─────┬─────┘  │       │
                        │        │               │        │   ┌───▼───┐
    ┌───────┐  WebSocket│  ┌─────▼─────┐  ┌─────▼─────┐  │   │  MQ   │
    │ User B│◄─────────►│  │  Handler  │  │  Tool     │  │   │(Asynq)│
    │Device1│  RPC+Push │  │  Registry │  │  Provider │  │   └───────┘
    └───────┘           │  └─────┬─────┘  └───────────┘  │
                        │        │                        │
    ┌───────┐  WebSocket│  ┌─────▼─────────────────────┐  │
    │ User B│◄─────────►│  │        Store Layer        │  │
    │Device2│  RPC+Push │  │  (SQLite/PostgreSQL/MySQL)│  │
    └───────┘           │  └───────────────────────────┘  │
                        └─────────────────────────────────┘
```

**Three layers, one binary:**

- **Connection Layer** — WebSocket server with per-(user, device) connection tracking, Redis Pub/Sub for cross-node broadcasting, and device replacement protocol
- **Messaging Layer** — Bidirectional RPC (client↔server), persistent update sync with sequence-based gap filling, ephemeral pushes for typing/streaming/presence
- **Agent Layer** — YAML-configured AI agents with multi-LLM support (OpenAI, Claude, Ollama, Qwen), MCP tool integration, client-side tool invocation via ReverseRPC, sub-agent delegation, and human-in-the-loop checkpoints

---

## Features

### 💬 Messaging Core

- **Bidirectional RPC** — Both client and server can initiate calls over a single WebSocket connection
- **Persistent updates** — Sequence-numbered update log with cursor-based pagination (`sync_updates`)
- **Offline resilience** — Clients reconnect and fetch missed updates; gap placeholders ensure no silent data loss
- **Multi-device sync** — Per-device connection tracking (`user_id` + `device_id` + `conn_id`)
- **Soft delete** — Conversations and messages support delete/restore with cascade semantics

### 🤖 AI Agent Runtime

- **Declarative agents** — Define agents in a single Markdown file with YAML front matter (model, tools, middleware, system prompt)
- **Multi-LLM** — Pluggable providers: OpenAI, Anthropic Claude, Ollama, Qwen — or bring your own
- **Streaming responses** — Real-time text streaming via ephemeral push (`stream_text`), cumulative snapshot model
- **Tool execution** — Server-side tools (code, search) + client-side tools (ReverseRPC to device) + MCP server integration
- **Human-in-the-Loop** — Agents pause and ask users for confirmation via `ask_user`; questions persist to database, multi-device sync via pull-notification pattern (conversation update as lightweight signal), idempotent `agent_resume` with 409 conflict detection, and crash recovery (answers in DB, checkpoints in Redis with 24h TTL, stale conversation cleanup)
- **Sub-agent delegation** — Agents can invoke other agents, each with isolated context
- **Context management** — Token-aware truncation with optional summarization middleware

### 🏗️ Infrastructure

- **Zero-config startup** — SQLite + Redis localhost defaults, one command to run
- **Flexible storage** — SQLite (embedded), PostgreSQL, or MySQL via GORM
- **Distributed-ready** — Redis Pub/Sub for cross-node push, Asynq for async task queue with priority levels
- **Fail-open design** — MQ enqueue failures don't block message persistence; Redis outages don't crash agents
- **Ephemeral events** — Typing indicators, streaming text, agent status — never persisted, never replayed, always real-time

---

## Quick Start

### Prerequisites

- **Go 1.26+**
- **Redis** running on `localhost:6379` (default)

### Build & Run

```bash
# Clone
git clone https://github.com/PineappleBond/xyncra-server.git
cd xyncra-server

# Build
make build

# Start server (zero-config: SQLite + Redis localhost:6379)
./bin/xyncra-server
```

That's it. The server is listening on `:8080`.

### Docker

```bash
docker compose up -d
```

### Configuration

Override defaults via CLI flags or `XYNCRA_` environment variables:

| Flag              | Env Var                     | Default          | Description                             |
| ----------------- | --------------------------- | ---------------- | --------------------------------------- |
| `-addr`           | `XYNCRA_ADDR`               | `:8080`          | WebSocket listen address                |
| `-redis-addr`     | `XYNCRA_REDIS_ADDR`         | `localhost:6379` | Redis address                           |
| `-redis-password` | `XYNCRA_REDIS_PASSWORD`     |                  | Redis AUTH password                     |
| `-db-driver`      | `XYNCRA_DB_DRIVER`          | `sqlite`         | `sqlite` / `postgres` / `mysql`         |
| `-db-dsn`         | `XYNCRA_DB_DSN`             | `xyncra.db`      | Database connection string              |
| `-max-conns`      | `XYNCRA_MAX_CONNS_PER_USER` | `0` (unlimited)  | Max connections per user                |
| `-redis-db`       | `XYNCRA_REDIS_DB`            | `0`              | Redis database number                   |
| `-agents-dir`     | `XYNCRA_AGENTS_DIR`          | `agents`         | Path to agent definitions directory     |
| `-max-functions-per-device` | `XYNCRA_MAX_FUNCTIONS_PER_DEVICE` | `200` | Max registered functions per device  |

---

## Client CLI

Xyncra includes a full-featured CLI client (`xyncra-client`) for interacting with the server.

```bash
# Start the daemon (maintains persistent WebSocket connection)
./bin/xyncra-client listen --user-id alice --device-id laptop

# Create a conversation
./bin/xyncra-client create-conversation --peer-id bob

# Send a message
./bin/xyncra-client send --conversation-id <conv-id> --content "Hello!"

# Query local data (offline-capable, reads from local SQLite)
./bin/xyncra-client list-conversations
./bin/xyncra-client get-messages --conversation-id <conv-id>
./bin/xyncra-client search-messages --conversation-id <conv-id> --query "hello"
```

The daemon auto-registers built-in functions (`ping`, `get_device_info`, `get_time`) that agents can invoke via ReverseRPC. Custom device metadata can be attached via `--device-info`.

---

## Protocol

All communication uses a **3-level envelope** over WebSocket:

```jsonc
// Client → Server (Request, type=0)
{"type": 0, "data": {"id": "req-1", "method": "send_message", "params": {...}}}

// Server → Client (Response, type=1)
{"type": 1, "data": {"id": "req-1", "code": 0, "msg": "ok", "data": {...}}}

// Server → Client (Push Updates, type=2)
{"type": 2, "data": {"updates": [{"seq": 1, "type": "message", "payload": {...}}]}}
```

### RPC Methods

| Method                | Description                                                 |
| --------------------- | ----------------------------------------------------------- |
| `heartbeat`           | Keep-alive, refreshes connection TTL                        |
| `send_message`        | Send a message (idempotent via `client_message_id`)         |
| `sync_updates`        | Cursor-based update sync with gap filling                   |
| `create_conversation` | Find-or-create 1-on-1 conversation                          |
| `get_conversation`    | Get a single conversation with unread count and HITL questions |
| `list_conversations`  | List conversations (ordered by `last_message_at` DESC)      |
| `get_messages`        | Paginated message history                                   |
| `search_messages`     | Text search (LIKE-based) within a conversation              |
| `mark_as_read`        | Update read cursor (MAX semantics)                          |
| `delete_conversation` | Soft-delete conversation + messages                         |
| `restore_conversation`| Restore soft-deleted conversation                           |
| `delete_message`      | Soft-delete message (sender only)                           |
| `set_typing`          | Ephemeral typing indicator (Seq=0)                          |
| `stream_text`         | Ephemeral streaming text (Seq=0, cumulative snapshot)       |
| `agent_resume`        | Resume a HITL-interrupted agent                             |
| `reload_agents`       | Hot-reload agent configurations                             |
| `system.register_functions` | Register device function capabilities (ReverseRPC)    |
| `system.reconnect`    | Reconnect handshake with request replay                     |

### Push Update Types

**Persisted** (Seq > 0, delivered via `sync_updates`):

| Type             | Description                                     |
| ---------------- | ----------------------------------------------- |
| `message`        | New message                                     |
| `delete_message` | Message deleted                                 |
| `mark_read`      | Read cursor updated                             |
| `conversation`   | Conversation state changed (incl. HITL)         |
| `gap`            | Synthetic gap filler (runtime only)             |

**Ephemeral** (Seq = 0, real-time only, never replayed):

| Type                         | Description                                                                    |
| ---------------------------- | ------------------------------------------------------------------------------ |
| `typing`                     | User typing indicator                                                          |
| `streaming`                  | Cumulative text stream from agent                                              |
| `agent_status`               | Agent state: thinking / tool_calling / generating / idle / asking_user         |
| `agent_timeout`              | Agent timed out                                                                |

📖 Full protocol specification: [docs/API.md](docs/API.md)

---

## Agent System

Agents are defined as **single Markdown files** with YAML front matter — no code required.

### Example: Weather Bot

```markdown
---
id: weather-bot
name: Weather Bot
model: qwen3.7-plus
api_key_env: DASHSCOPE_API_KEY
tools:
  - get_weather
  - get_current_time
middleware:
  enable_client_tools: true
  enable_summarization: true
---

You are a helpful weather assistant. Provide current weather
information, forecasts, and weather-related advice.
```

Drop this file in the `agents/` directory and hot-reload with `reload_agents` RPC.

### Agent Capabilities

| Feature                | Description                                                                     |
| ---------------------- | ------------------------------------------------------------------------------- |
| **Multi-LLM**          | OpenAI, Claude, Ollama, Qwen — pluggable `LLMProvider` interface                |
| **Tool calling**       | Server-side tools, client-side tools (ReverseRPC), MCP servers                  |
| **Streaming**          | Real-time text streaming with cumulative snapshot model                         |
| **HITL**               | Persistent questions with multi-device sync, offline resilience, crash recovery via checkpoint + DB |
| **Sub-agents**         | Delegate to other agents with isolated contexts                                 |
| **Middleware**         | Client tools, tool-call patching, summarization, tool result reduction          |
| **Context management** | Token-aware truncation, message count fallback, configurable limits             |

### Agent Interaction Flow

```text
User                 Server              Agent              LLM
 │  send_message      │                    │                  │
 │───────────────────►│  enqueue MQ task   │                  │
 │                    │───────────────────►│  prompt + context │
 │                    │                    │─────────────────►│
 │  typing (Seq=0)    │◄─── ephemeral ────│                  │
 │◄───────────────────│                    │  tool calls       │
 │  agent_status      │                    │─────────────────►│
 │◄───────────────────│◄─── ephemeral ────│                  │
 │  streaming (Seq=0) │                    │  response         │
 │◄───────────────────│◄───────────────────│◄─────────────────│
 │  message (Seq=N)   │                    │                  │
 │◄───────────────────│◄── persisted ──────│                  │
```

### HITL Resilience

The agent runtime implements a **conversation state machine** with 6 defined states (`idle`, `thinking`, `tool_calling`, `generating`, `asking_user`, `timeout`). Only `asking_user` and `idle` are persisted to the database — intermediate states (`thinking`, `tool_calling`, `generating`) are ephemeral WebSocket broadcasts for UI display only:

```text
                 ┌──────────────────────────────────────────────┐
                 │  Conversation State Machine                  │
                 │                                              │
                 │  Ephemeral (broadcast only, not persisted):  │
                 │  thinking → tool_calling → generating        │
                 │                                              │
                 │  Persisted (database):                       │
 idle ──────────────────────────────────────────► asking_user   │
                                                   │            │
                                                   │ resume     │
                                                   │ (all       │
                                                   │  answered) │
                                                   ◄────────────┘
                 timeout (background cleanup resets to idle)
```

**Pull-notification pattern** — When an agent pauses for HITL:

1. Questions are persisted to the `Question` table (DB)
2. Conversation `agent_status` transitions to `asking_user`
3. A lightweight `conversation` update is broadcast (just `conversation_id` + `updated_at`)
4. Clients pull the full conversation state — questions, status, checkpoint — on demand
5. Ephemeral `agent_status` pushes are also sent for online clients
6. CLI daemon's `OnConversation` handler detects `agent_status == "asking_user"` and displays HITL info as `[hitl]` format (checkpoint_id, interrupt_id, question_text)

**Crash recovery** — Answers live in DB, checkpoints in Redis (24h TTL). Server restarts during HITL wait are safe: the user can still answer, and the resume handler reads answers from DB to rebuild the targets map.

**Idempotency** — `agent_resume` uses `UPDATE ... WHERE status='pending'` for atomic answer claiming. If another device already answered, returns 409 (`question_already_answered`). Multi-question checkpoints track partial progress — resume only fires when ALL questions are answered.

**Stale cleanup** — A background task (Redis distributed lock per conversation) detects conversations stuck in `asking_user` past a configurable threshold, resets them to `idle`, and cleans up pending questions.

📖 Full scenario analysis: [docs/DESIGN_HITL_RESILIENCE.md](docs/DESIGN_HITL_RESILIENCE.md)

📖 Agent configuration details: [docs/PRODUCT_DECISIONS.md](docs/PRODUCT_DECISIONS.md) (D-054 through D-124) and [docs/PRODUCT_DECISIONS_DETAILS.md](docs/PRODUCT_DECISIONS_DETAILS.md)

---

## Deployment Model

Xyncra is designed for **internal network deployment** behind a reverse proxy:

```text
         Internet
            │
     ┌──────▼──────┐
     │   Nginx /   │  ← TLS termination, CORS, Rate Limit
     │   Envoy     │
     └──────┬──────┘
            │ Internal Network
     ┌──────▼──────┐
     │   Your App  │  ← Authentication, business logic
     │   Server    │     Injects authenticated user_id
     └──────┬──────┘
            │
     ┌──────▼──────┐
     │   Xyncra    │  ← Messaging + Agents
     │   Server    │
     └─────────────┘
```

**What Xyncra intentionally does NOT include:**

- ❌ Authentication — handled by your app server via reverse proxy
- ❌ TLS termination — handled by your reverse proxy
- ❌ CORS / Rate Limiting — handled by your reverse proxy
- ❌ CSRF protection — not needed in internal deployment

**What you get out of the box:**

- ✅ `user_id` query parameter auth (dev default, override via `WSWithAuthenticate`)
- ✅ Accepts any Origin (internal deployment model)
- ✅ Functional options for all configuration overrides

📖 Design rationale: [docs/PRODUCT_DECISIONS.md](docs/PRODUCT_DECISIONS.md) (D-001 through D-005)

---

## Project Structure

```text
xyncra-server/
├── cmd/
│   ├── xyncra-server/        # Server entry point
│   └── xyncra-client/        # CLI client entry point
├── agents/                   # Agent definitions (Markdown with YAML front matter)
├── internal/
│   ├── server/               # WebSocket server, connection lifecycle
│   ├── handler/              # RPC method handlers
│   ├── agent/                # Agent runtime, executor, tool providers
│   │   └── tools/            # Built-in tool implementations
│   ├── cli/                  # CLI client implementation (commands, output)
│   ├── mq/                   # Message queue (Asynq/Redis)
│   ├── store/                # Persistence layer (GORM)
│   │   └── model/            # Data models
│   ├── cleanup/              # Expired update cleanup
│   └── e2e/                  # End-to-end integration tests
├── pkg/
│   ├── protocol/             # Wire protocol types (importable)
│   ├── client/               # Go client SDK
│   └── store/                # Client-side local storage (SQLite via GORM)
│       └── model/            # Client data models
├── configs/                  # Configuration templates
├── scripts/                  # Shell scripts
├── docs/
│   ├── API.md                    # WebSocket protocol reference
│   ├── PRODUCT_DECISIONS.md      # Architecture decisions
│   ├── PRODUCT_DECISIONS_DETAILS.md # Detailed decision specifications
│   ├── DEVELOPER_GUIDE.md        # Developer onboarding guide
│   ├── DEVELOPER_REFERENCE.md    # Developer reference
│   ├── DESIGN_HITL_RESILIENCE.md # HITL failure scenarios & recovery design
│   └── manual-test-cases/        # Manual test case documents
├── Dockerfile
├── docker-compose.yml
└── docker-compose.e2e.yml    # E2E test environment
```

---

## Development

```bash
# Unit tests (no Redis required)
make test

# E2E tests (Redis on port 16379 required)
make test-e2e

# All tests
make test-all

# Format & lint
make fmt
make vet
```

---

## Documentation

| Document                                                   | Description                                           |
| ---------------------------------------------------------- | ----------------------------------------------------- |
| [API Reference](docs/API.md)                               | Complete WebSocket protocol specification             |
| [Product Decisions](docs/PRODUCT_DECISIONS.md)             | Architecture decisions (D-001 to D-124, 111 defined) |
| [Product Decisions Details](docs/PRODUCT_DECISIONS_DETAILS.md) | Detailed decision specifications                  |
| [Developer Guide](docs/DEVELOPER_GUIDE.md)                 | Project structure, coding conventions, how-to guides  |
| [Developer Reference](docs/DEVELOPER_REFERENCE.md)         | Developer reference documentation                     |
| [HITL Resilience Design](docs/DESIGN_HITL_RESILIENCE.md)   | HITL failure scenarios, recovery matrix, data model   |
| [Manual Test Cases](docs/manual-test-cases/)               | End-to-end manual test scenarios (in Chinese)         |
| [Package Docs](internal/)                                  | Per-package design documents (in Chinese)             |

---

## Contributing

Contributions are welcome! Here's how to get started:

1. **Report bugs** — Open an issue with reproduction steps
2. **Suggest features** — Open an issue describing your use case
3. **Submit PRs** — Fork, branch, implement, test, submit

When contributing code:

- Follow existing patterns and naming conventions (see [Developer Guide](docs/DEVELOPER_GUIDE.md))
- Reference product decision IDs in comments (e.g., `D-011`)
- Write tests — handler tests use in-memory stores, E2E tests require Redis
- Use `fmt.Errorf("context: %w", err)` for error wrapping

---

## License

[MIT](LICENSE) — Copyright (c) 2026 PineappleBond
