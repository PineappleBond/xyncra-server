# Xyncra Server

Xyncra Server is the backend for the **Xyncra** distributed instant messaging system. It provides a bidirectional RPC protocol over a single long-lived connection (WebSocket/TCP), enabling both client-to-server and server-to-client calls, plus server-initiated update pushes for message sync and multi-device synchronisation.

## Architecture

The project follows the standard Go layout with `internal/` for private packages and `pkg/` for public, importable libraries.

```
xyncra-server/
├── internal/
│   ├── server/   # WebSocket server, connection lifecycle, connection store (Redis-backed)
│   ├── mq/       # Message queue abstraction for async tasks and remote push routing (Asynq/Redis)
│   └── store/    # Data persistence layer (GORM + PostgreSQL/MySQL/SQLite) and Redis cache
│       └── model/  # Core data models: Conversation, Message, UserUpdate
├── pkg/
│   └── protocol/ # Wire protocol types (Package, Request, Response, Updates) — importable by clients
├── go.mod
└── go.sum
```

### Package responsibilities

- **`internal/server`** — WebSocket server that manages client connections, dispatches bidirectional RPC requests/responses, and pushes `Updates`. Includes a Redis-backed `ConnectionStore` for distributed deployments.
- **`internal/mq`** — Message queue layer backed by [Asynq](https://github.com/hibiken/asynq) (Redis). Handles RPC response routing across nodes, remote `Updates` delivery, and generic async tasks with priority queues (`critical`, `default`, `low`).
- **`internal/store`** — Persistence layer using GORM. Supports PostgreSQL (primary), MySQL, and SQLite. Provides a `StoreAPI` interface with sub-stores for conversations, messages, and user updates, plus transactional composite operations like `SendMessage`.
- **`internal/store/model`** — Core data models. The system uses a **userless-schema design** — there is no `users` table; users are identified by string IDs referenced loosely by other tables.
- **`pkg/protocol`** — Wire protocol types shared with clients:
  - `Package` — outer envelope, discriminated by `PackageType`
  - `PackageDataRequest` — bidirectional RPC request (both sides can call)
  - `PackageDataResponse` — RPC response matched by request ID
  - `PackageDataUpdates` — server-only push channel for data sync (message delivery + multi-device sync)

## Getting started

### Prerequisites

- Go 1.26+
- Redis (for connection store and message queue)
- PostgreSQL, MySQL, or SQLite (for data persistence)

### Build

```bash
go build -o xyncra-server ./...
```

### Test

```bash
# Unit tests (SQLite-backed, no external services needed)
go test ./...

# Skip tests that require Redis
go test -short ./...
```

### Vet

```bash
go vet ./...
```

## Documentation

Each package has its own detailed design document (in Chinese):

- [Protocol](pkg/protocol/README-ZH.md)
- [Server](internal/server/README-ZH.md)
- [Message Queue](internal/mq/README-ZH.md)
- [Store](internal/store/README-ZH.md)
- [Data Models](internal/store/model/README-ZH.md)

Usage guides (`TESTING_USAGE.md`) are available inside `internal/mq/`, `internal/server/`, and `internal/store/` with runnable examples for each component.

## License

See [LICENSE](LICENSE).
