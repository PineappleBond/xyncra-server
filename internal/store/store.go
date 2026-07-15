package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/PineappleBond/xyncra-server/internal/store/model"
)

// maxSendMessageUpdates is the maximum number of user updates (conversation
// members) allowed in a single SendMessage call.
const maxSendMessageUpdates = 500

// SendMessageResult is returned by Store.SendMessage after a successful atomic
// persist. It contains the message with its allocated MessageID and the
// per-user update records with their allocated seq values. The caller uses
// these to build MQ push payloads.
type SendMessageResult struct {
	// Message is the persisted message with its allocated MessageID.
	Message *model.Message

	// Updates are the per-user update records with their allocated seq values.
	Updates []model.UserUpdate
}

// StoreAPI defines the public interface for the Store, useful for dependency
// injection and mocking in tests.
type StoreAPI interface {
	// Sub-store access
	ConversationStore() *ConversationStore
	MessageStore() *MessageStore
	UserUpdateStore() *UserUpdateStore
	QuestionStore() *QuestionStore

	// Composite operations
	SendMessage(ctx context.Context, msg *model.Message, memberIDs []string) (*SendMessageResult, error)

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

	// Questions provides question-related operations (HITL resilience).
	Questions *QuestionStore
}

// Ensure Store implements StoreAPI at compile time.
var _ StoreAPI = (*Store)(nil)

// ConversationStore returns the ConversationStore.
func (s *Store) ConversationStore() *ConversationStore { return s.Conversations }

// MessageStore returns the MessageStore.
func (s *Store) MessageStore() *MessageStore { return s.Messages }

// UserUpdateStore returns the UserUpdateStore.
func (s *Store) UserUpdateStore() *UserUpdateStore { return s.UserUpdates }

// QuestionStore returns the QuestionStore.
func (s *Store) QuestionStore() *QuestionStore { return s.Questions }

// New creates a Store backed by the given *gorm.DB. It initialises all
// sub-stores so that callers can access them directly (e.g. store.Messages.Get).
func New(db *gorm.DB) *Store {
	return &Store{
		db:            db,
		Conversations: NewConversationStore(db),
		Messages:      NewMessageStore(db),
		UserUpdates:   NewUserUpdateStore(db),
		Questions:     NewQuestionStore(db),
	}
}

// NewFromDatabase creates a Store from an existing Database instance.
func NewFromDatabase(database *Database) *Store {
	return New(database.DB())
}

// AutoMigrate runs GORM's auto-migration for all known models, creating or
// updating tables and indexes as needed. It also performs manual index
// migrations that GORM's AutoMigrate cannot handle (e.g. replacing a
// single-column index with a composite one).
func (s *Store) AutoMigrate(ctx context.Context) error {
	if err := s.db.WithContext(ctx).AutoMigrate(
		&model.Conversation{},
		&model.Message{},
		&model.UserUpdate{},
		&model.Question{},
	); err != nil {
		return fmt.Errorf("store: auto-migrate: %w", err)
	}

	// Migrate client_message_id index: replace the legacy single-column index
	// (idx_messages_client_message_id) with the composite unique index
	// (idx_msg_client_id_sender) on (client_message_id, sender_id).
	// GORM's AutoMigrate does not drop and recreate changed indexes, so we
	// must do this manually.
	s.db.Exec("DROP INDEX IF EXISTS idx_messages_client_message_id")
	s.db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_msg_client_id_sender ON messages(client_message_id, sender_id)")

	return nil
}

// SendMessage atomically allocates a MessageID (D-008), persists the message,
// allocates per-user seq values, creates fan-out UserUpdate records, and
// updates the conversation's last-message metadata. All reads and writes happen
// inside a single database transaction, eliminating the TOCTOU race that occurs
// when IDs are allocated outside the transaction.
//
// Parameters:
//   - msg: the message to insert. msg.MessageID must be zero; it is allocated
//     inside the transaction from the conversation's LastProcessedMessageID.
//   - memberIDs: the conversation member user IDs. One UserUpdate is created
//     per member. Must have at most 500 entries.
//
// Returns a SendMessageResult containing the message with its allocated
// MessageID and the UserUpdate records with their allocated seq values.
func (s *Store) SendMessage(
	ctx context.Context,
	msg *model.Message,
	memberIDs []string,
) (*SendMessageResult, error) {
	if len(memberIDs) > maxSendMessageUpdates {
		return nil, fmt.Errorf("store: too many members (%d), max is %d", len(memberIDs), maxSendMessageUpdates)
	}

	var result SendMessageResult

	err := s.Transaction(ctx, func(tx *gorm.DB) error {
		// 1. Read conversation inside the transaction to get the current
		//    LastProcessedMessageID. This is the critical section that prevents
		//    concurrent senders from allocating the same MessageID (D-008).
		var conv model.Conversation
		if err := tx.Where("id = ?", msg.ConversationID).First(&conv).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return classifyError(fmt.Errorf("store: send message - get conversation: %w", err))
		}

		// 2. Allocate MessageID atomically.
		msg.MessageID = conv.LastProcessedMessageID + 1

		// 3. Marshal the message (now with its allocated MessageID) for use
		//    as the UserUpdate payload.
		payload, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("store: send message - marshal message: %w", err)
		}

		// 4. Allocate per-user seq values and build UserUpdate records.
		now := time.Now()
		updates := make([]model.UserUpdate, 0, len(memberIDs))
		for _, memberID := range memberIDs {
			var latestSeq uint32
			if err := tx.Model(&model.UserUpdate{}).
				Where("user_id = ?", memberID).
				Select("COALESCE(MAX(seq), 0)").
				Scan(&latestSeq).Error; err != nil {
				return classifyError(fmt.Errorf("store: send message - get latest seq for user %s: %w", memberID, err))
			}

			update := model.UserUpdate{
				ID:        uuid.New().String(),
				UserID:    memberID,
				Seq:       latestSeq + 1,
				Type:      "message",
				Payload:   payload,
				CreatedAt: now,
			}
			updates = append(updates, update)
		}

		// 5. Insert the message.
		if err := tx.Create(msg).Error; err != nil {
			return classifyError(fmt.Errorf("store: send message - insert message: %w", err))
		}

		// 6. Batch insert user updates (fan-out).
		if len(updates) > 0 {
			if err := tx.CreateInBatches(updates, 100).Error; err != nil {
				return classifyError(fmt.Errorf("store: send message - insert user updates: %w", err))
			}
		}

		// 7. Update conversation last-message metadata.
		updateResult := tx.Model(&model.Conversation{}).
			Where("id = ?", msg.ConversationID).
			Updates(map[string]interface{}{
				"last_message_at":           msg.CreatedAt,
				"last_processed_message_id": msg.MessageID,
			})
		if updateResult.Error != nil {
			return classifyError(fmt.Errorf("store: send message - update conversation: %w", updateResult.Error))
		}
		if updateResult.RowsAffected == 0 {
			return ErrNotFound
		}

		// Capture results for the caller.
		result.Message = msg
		result.Updates = updates
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &result, nil
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
