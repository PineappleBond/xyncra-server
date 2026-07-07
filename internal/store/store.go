package store

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/PineappleBond/xyncra-server/internal/store/model"
)

// maxSendMessageUpdates is the maximum number of user updates allowed in a
// single SendMessage call.
const maxSendMessageUpdates = 500

// StoreAPI defines the public interface for the Store, useful for dependency
// injection and mocking in tests.
type StoreAPI interface {
	// Sub-store access
	ConversationStore() *ConversationStore
	MessageStore() *MessageStore
	UserUpdateStore() *UserUpdateStore

	// Composite operations
	SendMessage(ctx context.Context, msg *model.Message, updates []model.UserUpdate, convID string, lastMessageAt time.Time, lastProcessedMessageID uint32) error

	// Transaction support
	Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error
	BeginTx(ctx context.Context) (*Tx, error)

	// Schema
	AutoMigrate(ctx context.Context) error

	// Health
	Ping(ctx context.Context) error
	HealthCheck(ctx context.Context) error
}

// Store is the top-level data access entry point for the Xyncra messaging
// system. It aggregates the individual domain stores and provides composite
// operations (e.g. SendMessage) that require atomic transactions.
type Store struct {
	db *gorm.DB

	// Conversations provides conversation-related operations.
	Conversations *ConversationStore

	// Messages provides message-related operations.
	Messages *MessageStore

	// UserUpdates provides user-update related operations.
	UserUpdates *UserUpdateStore
}

// Ensure Store implements StoreAPI at compile time.
var _ StoreAPI = (*Store)(nil)

// ConversationStore returns the ConversationStore.
func (s *Store) ConversationStore() *ConversationStore { return s.Conversations }

// MessageStore returns the MessageStore.
func (s *Store) MessageStore() *MessageStore { return s.Messages }

// UserUpdateStore returns the UserUpdateStore.
func (s *Store) UserUpdateStore() *UserUpdateStore { return s.UserUpdates }

// New creates a Store backed by the given *gorm.DB. It initialises all
// sub-stores so that callers can access them directly (e.g. store.Messages.Get).
func New(db *gorm.DB) *Store {
	return &Store{
		db:            db,
		Conversations: NewConversationStore(db),
		Messages:      NewMessageStore(db),
		UserUpdates:   NewUserUpdateStore(db),
	}
}

// NewFromDatabase creates a Store from an existing Database instance.
func NewFromDatabase(database *Database) *Store {
	return New(database.DB())
}

// AutoMigrate runs GORM's auto-migration for all known models, creating or
// updating tables and indexes as needed.
func (s *Store) AutoMigrate(ctx context.Context) error {
	if err := s.db.WithContext(ctx).AutoMigrate(
		&model.Conversation{},
		&model.Message{},
		&model.UserUpdate{},
	); err != nil {
		return fmt.Errorf("store: auto-migrate: %w", err)
	}
	return nil
}

// SendMessage atomically persists a message together with its fan-out user
// updates and the conversation's last-message metadata. All three writes
// happen inside a single database transaction.
//
// Parameters:
//   - msg: the message to insert.
//   - updates: the per-user update records (one per conversation member). Max 500.
//   - convID: the conversation whose LastMessageAt / LastProcessedMessageID
//     should be updated. Typically this equals msg.ConversationID.
//   - lastMessageAt: the timestamp to set as the conversation's latest message time.
//     Typically this equals msg.CreatedAt.
//   - lastProcessedMessageID: the message's MessageID to record on the conversation.
//     Typically this equals msg.MessageID.
//
// Note: convID, lastMessageAt, and lastProcessedMessageID are passed explicitly
// rather than derived from msg, so that callers can override them (e.g. to
// batch-update a conversation's metadata with a different timestamp). For the
// common case, pass msg.ConversationID, msg.CreatedAt, and msg.MessageID.
func (s *Store) SendMessage(
	ctx context.Context,
	msg *model.Message,
	updates []model.UserUpdate,
	convID string,
	lastMessageAt time.Time,
	lastProcessedMessageID uint32,
) error {
	if len(updates) > maxSendMessageUpdates {
		return fmt.Errorf("store: too many updates (%d), max is %d", len(updates), maxSendMessageUpdates)
	}

	return s.Transaction(ctx, func(tx *gorm.DB) error {
		// 1. Insert the message.
		if err := tx.Create(msg).Error; err != nil {
			return classifyError(fmt.Errorf("store: send message - insert message: %w", err))
		}

		// 2. Batch insert user updates (fan-out).
		if len(updates) > 0 {
			if err := tx.CreateInBatches(updates, 100).Error; err != nil {
				return classifyError(fmt.Errorf("store: send message - insert user updates: %w", err))
			}
		}

		// 3. Update conversation last-message metadata.
		result := tx.Model(&model.Conversation{}).
			Where("id = ?", convID).
			Updates(map[string]interface{}{
				"last_message_at":           lastMessageAt,
				"last_processed_message_id": lastProcessedMessageID,
			})
		if result.Error != nil {
			return classifyError(fmt.Errorf("store: send message - update conversation: %w", result.Error))
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}

		return nil
	})
}

// Transaction executes fn inside a database transaction. If fn returns an error,
// the transaction is rolled back; otherwise it is committed. Returns
// ErrContextDeadlineExceeded if the context is already expired.
func (s *Store) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return s.db.WithContext(ctx).Transaction(fn)
}

// BeginTx starts a new database transaction and returns a Tx handle.
// The caller is responsible for calling Commit or Rollback on the returned Tx.
func (s *Store) BeginTx(ctx context.Context) (*Tx, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	tx := s.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, fmt.Errorf("store: begin transaction: %w", tx.Error)
	}
	return &Tx{tx: tx}, nil
}

// Ping verifies that the database connection is still alive by pinging the
// underlying driver and executing a lightweight SELECT 1 query. This catches
// both connection-level failures and basic query-path problems (e.g. missing
// or corrupt schema).
func (s *Store) Ping(ctx context.Context) error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return fmt.Errorf("store: ping: %w", err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return classifyError(fmt.Errorf("store: ping: %w", err))
	}
	// Verify the query path works by executing a trivial select.
	var result int
	if err := s.db.WithContext(ctx).Raw("SELECT 1").Scan(&result).Error; err != nil {
		return classifyError(fmt.Errorf("store: ping query: %w", err))
	}
	return nil
}

// HealthCheck performs a comprehensive health check: it pings the database and
// verifies basic connectivity by running a simple query.
func (s *Store) HealthCheck(ctx context.Context) error {
	if err := s.Ping(ctx); err != nil {
		return err
	}
	// Verify we can run a query.
	var result int
	if err := s.db.WithContext(ctx).Raw("SELECT 1").Scan(&result).Error; err != nil {
		return fmt.Errorf("store: health check query failed: %w", err)
	}
	return nil
}
