// Package store provides the client-side data access layer for the Xyncra
// messaging system, backed by SQLite via GORM.
//
// # Architecture
//
// The package centers on ClientDB, which aggregates eight domain-specific
// sub-stores (Conversations, Messages, UserUpdates, SyncStates, Drafts, Queue,
// RPCLogs, NotificationLogs). Each sub-store encapsulates all CRUD and query
// operations for its domain model.
//
// This package mirrors the server-side internal/store package but is placed
// under pkg/ so that external client applications can import it. The three
// shared models (Conversation, Message, UserUpdate) are copied from
// internal/store/model since Go's internal/ packages cannot be imported
// externally.
//
// # SQLite Configuration
//
// The database is opened with PRAGMAs optimized for single-writer WAL mode:
//
//   - journal_mode=WAL   — concurrent reads during writes
//   - busy_timeout=5000  — wait up to 5s for lock acquisition
//   - cache_size=-8000   — 8 MB page cache
//   - synchronous=NORMAL — safe with WAL
//   - foreign_keys=ON    — enforce referential integrity
//
// MaxOpenConns is set to 1 because SQLite uses file-level locking; multiple
// write connections would cause "database is locked" errors. WAL mode allows
// concurrent reads via a separate reader path.
//
// # Auto-Migration
//
// AutoMigrate runs automatically during New() / NewInMemory() (D-023),
// ensuring the schema is always up to date when the client starts.
package store
