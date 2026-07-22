package store

import (
	"context"
	"fmt"

	gsqlite "github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

// ClientDB is the top-level data access entry point for the Xyncra client.
// It aggregates the individual domain stores and provides SQLite-specific
// initialization with appropriate PRAGMAs for single-writer access.
type ClientDB struct {
	db *gorm.DB

	// Conversations provides conversation-related operations.
	Conversations *ConversationStore

	// Messages provides message-related operations.
	Messages *MessageStore

	// UserUpdates provides user-update related operations.
	UserUpdates *UserUpdateStore

	// SyncStates provides sync state key-value operations.
	SyncStates *SyncStateStore

	// Drafts provides message draft operations.
	Drafts *DraftStore

	// Queue provides retry task queue operations.
	Queue *QueueStore

	// RPCLogs provides RPC log operations.
	RPCLogs *RPCLogStore

	// NotificationLogs provides notification log operations.
	NotificationLogs *NotificationLogStore

	// RemoteCallings provides remote calling operations (D-137).
	RemoteCallings *RemoteCallingStore
}

// New opens a SQLite database at the given path, configures PRAGMAs for
// single-writer WAL mode, initializes all sub-stores, and runs AutoMigrate.
// This is the primary constructor for production use (D-001, D-023).
func New(dbPath string) (*ClientDB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=cache_size(-8000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)", dbPath)

	db, err := gorm.Open(gsqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("store: failed to open database: %w", err)
	}

	return newClientDB(db)
}

// NewInMemory creates an in-memory SQLite database for testing.
// The name parameter is used to create a named shared memory database.
func NewInMemory(name string) (*ClientDB, error) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)", name)

	db, err := gorm.Open(gsqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("store: failed to open in-memory database: %w", err)
	}

	return newClientDB(db)
}

// newClientDB configures the connection pool, initializes sub-stores, and
// runs AutoMigrate on the given *gorm.DB.
func newClientDB(db *gorm.DB) (*ClientDB, error) {
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("store: failed to get underlying db: %w", err)
	}

	// SQLite file-level lock: single writer connection is sufficient.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(0)

	c := &ClientDB{
		db:               db,
		Conversations:    &ConversationStore{db: db},
		Messages:         &MessageStore{db: db},
		UserUpdates:      &UserUpdateStore{db: db},
		SyncStates:       &SyncStateStore{db: db},
		Drafts:           &DraftStore{db: db},
		Queue:            &QueueStore{db: db},
		RPCLogs:          &RPCLogStore{db: db},
		NotificationLogs: &NotificationLogStore{db: db},
		RemoteCallings:   &RemoteCallingStore{db: db},
	}

	// AutoMigrate all models (D-023).
	if err := c.AutoMigrate(context.Background()); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("store: auto-migrate: %w", err)
	}

	return c, nil
}

// AutoMigrate runs GORM's auto-migration for all known models, creating or
// updating tables and indexes as needed.
func (c *ClientDB) AutoMigrate(ctx context.Context) error {
	return c.db.WithContext(ctx).AutoMigrate(
		&model.Conversation{},
		&model.Message{},
		&model.UserUpdate{},
		&model.SyncState{},
		&model.Draft{},
		&model.RetryTask{},
		&model.RPCLog{},
		&model.NotificationLog{},
		&model.RemoteCalling{},
	)
}

// Close closes the underlying database connection pool.
func (c *ClientDB) Close() error {
	sqlDB, err := c.db.DB()
	if err != nil {
		return fmt.Errorf("store: failed to get underlying db: %w", err)
	}
	return sqlDB.Close()
}

// Transaction executes fn inside a database transaction. If fn returns an error,
// the transaction is rolled back; otherwise it is committed.
func (c *ClientDB) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return c.db.WithContext(ctx).Transaction(fn)
}

// Ping verifies that the database connection is alive by executing a trivial
// query.
func (c *ClientDB) Ping(ctx context.Context) error {
	sqlDB, err := c.db.DB()
	if err != nil {
		return fmt.Errorf("store: ping: %w", err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return classifyError(fmt.Errorf("store: ping: %w", err))
	}
	var result int
	if err := c.db.WithContext(ctx).Raw("SELECT 1").Scan(&result).Error; err != nil {
		return classifyError(fmt.Errorf("store: ping query: %w", err))
	}
	return nil
}
