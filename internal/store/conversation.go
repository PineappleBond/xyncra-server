package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"gorm.io/gorm"

	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/internal/tracing"
)

// ConversationStore provides data access operations for the Conversation model.
type ConversationStore struct {
	db *gorm.DB
}

// NewConversationStore creates a ConversationStore backed by the given database.
func NewConversationStore(db *gorm.DB) *ConversationStore {
	return &ConversationStore{db: db}
}

// Create inserts a new conversation record into the database.
func (cs *ConversationStore) Create(ctx context.Context, conv *model.Conversation) (err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBConversationCreate)
	defer func() { finish(err) }()

	if err = cs.db.WithContext(ctx).Create(conv).Error; err != nil {
		return classifyError(err)
	}
	return nil
}

// Get retrieves a conversation by its primary key. Returns ErrNotFound if no
// record exists.
func (cs *ConversationStore) Get(ctx context.Context, id string) (result *model.Conversation, err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBConversationGet,
		attribute.String(tracing.AttrConversationID, id))
	defer func() { finish(err) }()

	var conv model.Conversation
	err = cs.db.WithContext(ctx).
		Where("id = ?", id).
		First(&conv).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, classifyError(fmt.Errorf("store: get conversation: %w", err))
	}
	return &conv, nil
}

// GetByUsers returns the 1-on-1 conversation between user1 and user2.
// It checks both (user1, user2) and (user2, user1) orderings.
// Returns ErrNotFound if no matching conversation exists.
func (cs *ConversationStore) GetByUsers(ctx context.Context, user1, user2 string) (result *model.Conversation, err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBConversationGetByUsers,
		attribute.String(tracing.AttrUserID, user1))
	defer func() { finish(err) }()

	var conv model.Conversation
	err = cs.db.WithContext(ctx).
		Where("(user_id1 = ? AND user_id2 = ?) OR (user_id1 = ? AND user_id2 = ?)", user1, user2, user2, user1).
		First(&conv).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, classifyError(fmt.Errorf("store: get conversation by users: %w", err))
	}
	return &conv, nil
}

// GetByUser returns conversations where the given user is either UserID1 or
// UserID2, ordered by LastMessageAt descending, with offset/limit pagination.
// Soft-deleted records are excluded automatically by GORM's soft-delete plugin.
// The query is split into two indexed sub-queries and merged for efficiency,
// avoiding the OR condition that prevents index usage on both columns.
//
// Known limitation: in extreme pagination scenarios (very large offset values),
// records that interleave across the user1/user2 boundary may be missed. Each
// side fetches (offset+limit) rows independently, so if the true merged result
// has more than (offset+limit) rows on one side before the other side
// contributes, some interleaved rows could be dropped after deduplication.
// This is acceptable in practice because a single user typically has far fewer
// than 1000 conversations.
func (cs *ConversationStore) GetByUser(ctx context.Context, userID string, offset, limit int) (result []*model.Conversation, err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBConversationGetByUser,
		attribute.String(tracing.AttrUserID, userID))
	defer func() { finish(err) }()

	// Allow up to 101 so callers can use the limit+1 probe technique for
	// has_more detection at the maximum page size (100).
	if limit <= 0 || limit > 101 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	// Fetch up to (offset+limit) from each side, then merge, deduplicate, and paginate.
	fetchLimit := offset + limit

	var asUser1 []*model.Conversation
	if err = cs.db.WithContext(ctx).
		Where("user_id1 = ?", userID).
		Order("last_message_at DESC").
		Limit(fetchLimit).
		Find(&asUser1).Error; err != nil {
		return nil, classifyError(fmt.Errorf("store: get conversations by user (user1): %w", err))
	}

	var asUser2 []*model.Conversation
	if err = cs.db.WithContext(ctx).
		Where("user_id2 = ? AND user_id2 != ''", userID).
		Order("last_message_at DESC").
		Limit(fetchLimit).
		Find(&asUser2).Error; err != nil {
		return nil, classifyError(fmt.Errorf("store: get conversations by user (user2): %w", err))
	}

	// Merge and deduplicate by ID, preserving LastMessageAt DESC order.
	seen := make(map[string]bool, len(asUser1)+len(asUser2))
	var merged []*model.Conversation
	for _, c := range asUser1 {
		if !seen[c.ID] {
			seen[c.ID] = true
			merged = append(merged, c)
		}
	}
	for _, c := range asUser2 {
		if !seen[c.ID] {
			seen[c.ID] = true
			merged = append(merged, c)
		}
	}

	// Stable sort by LastMessageAt DESC.
	sortConversationsByLastMessageAt(merged)

	// Apply pagination.
	if offset >= len(merged) {
		return []*model.Conversation{}, nil
	}
	end := offset + limit
	if end > len(merged) {
		end = len(merged)
	}
	return merged[offset:end], nil
}

// sortConversationsByLastMessageAt sorts conversations by LastMessageAt descending
// using a simple insertion sort (sufficient for the small slices we deal with).
func sortConversationsByLastMessageAt(convs []*model.Conversation) {
	for i := 1; i < len(convs); i++ {
		for j := i; j > 0 && convs[j].LastMessageAt.After(convs[j-1].LastMessageAt); j-- {
			convs[j], convs[j-1] = convs[j-1], convs[j]
		}
	}
}

// Update saves all fields of the conversation back to the database.
func (cs *ConversationStore) Update(ctx context.Context, conv *model.Conversation) (err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBConversationUpdate)
	defer func() { finish(err) }()

	if err = cs.db.WithContext(ctx).Save(conv).Error; err != nil {
		return classifyError(err)
	}
	return nil
}

// Delete performs a soft delete on the conversation identified by id.
func (cs *ConversationStore) Delete(ctx context.Context, id string) (err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBConversationDelete,
		attribute.String(tracing.AttrConversationID, id))
	defer func() { finish(err) }()

	result := cs.db.WithContext(ctx).Delete(&model.Conversation{}, "id = ?", id)
	if result.Error != nil {
		return classifyError(fmt.Errorf("store: delete conversation: %w", result.Error))
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Restore undeletes a soft-deleted conversation identified by id.
func (cs *ConversationStore) Restore(ctx context.Context, id string) (err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBConversationRestore,
		attribute.String(tracing.AttrConversationID, id))
	defer func() { finish(err) }()

	result := cs.db.WithContext(ctx).
		Unscoped().
		Model(&model.Conversation{}).
		Where("id = ? AND deleted_at IS NOT NULL", id).
		Update("deleted_at", nil)
	if result.Error != nil {
		return classifyError(fmt.Errorf("store: restore conversation: %w", result.Error))
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateLastMessage updates the LastMessageAt and LastProcessedMessageID fields
// of the conversation identified by convID.
func (cs *ConversationStore) UpdateLastMessage(ctx context.Context, convID string, lastMessageAt time.Time, lastProcessedMessageID uint32) (err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBConversationUpdateLastMessage,
		attribute.String(tracing.AttrConversationID, convID))
	defer func() { finish(err) }()

	result := cs.db.WithContext(ctx).
		Model(&model.Conversation{}).
		Where("id = ?", convID).
		Updates(map[string]any{
			"last_message_at":           lastMessageAt,
			"last_processed_message_id": lastProcessedMessageID,
		})
	if result.Error != nil {
		return classifyError(fmt.Errorf("store: update last message: %w", result.Error))
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// SearchByTitle searches conversations for the given user that contain the
// specified title substring (case-insensitive via LIKE), ordered by
// LastMessageAt descending, limited to at most limit rows.
func (cs *ConversationStore) SearchByTitle(ctx context.Context, userID, title string, limit int) (result []*model.Conversation, err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBConversationSearchByTitle,
		attribute.String(tracing.AttrUserID, userID))
	defer func() { finish(err) }()

	// Allow up to 101 so callers can use the limit+1 probe technique.
	if limit <= 0 || limit > 101 {
		limit = 20
	}
	if title == "" {
		return []*model.Conversation{}, nil
	}

	like := "%" + escapeLikePattern(title) + "%"

	var convs []*model.Conversation
	err = cs.db.WithContext(ctx).
		Where("(user_id1 = ? OR user_id2 = ?) AND title LIKE ? ESCAPE '|'", userID, userID, like).
		Order("last_message_at DESC").
		Limit(limit).
		Find(&convs).Error
	if err != nil {
		return nil, classifyError(fmt.Errorf("store: search conversations by title: %w", err))
	}
	return convs, nil
}

// GetUnscoped retrieves a conversation including soft-deleted records.
// Returns ErrNotFound if no record exists.
func (cs *ConversationStore) GetUnscoped(ctx context.Context, id string) (result *model.Conversation, err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBConversationGetUnscoped,
		attribute.String(tracing.AttrConversationID, id))
	defer func() { finish(err) }()

	var conv model.Conversation
	err = cs.db.WithContext(ctx).
		Unscoped().
		Where("id = ?", id).
		First(&conv).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, classifyError(fmt.Errorf("store: get unscoped conversation: %w", err))
	}
	return &conv, nil
}

// UpdateLastRead updates the last-read message ID for the specified user.
// Uses MAX semantics: only advances forward, never backward (D-012).
func (cs *ConversationStore) UpdateLastRead(ctx context.Context, convID, userID string, messageID uint32) (err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBConversationUpdateLastRead,
		attribute.String(tracing.AttrConversationID, convID),
		attribute.String(tracing.AttrUserID, userID))
	defer func() { finish(err) }()

	// 1. Get conversation to determine which field to update.
	conv, err := cs.Get(ctx, convID)
	if err != nil {
		return err
	}

	// 2. Determine which column to update.
	var column string
	switch {
	case conv.UserID1 == userID:
		column = "last_read_message_id1"
	case conv.UserID2 == userID:
		column = "last_read_message_id2"
	default:
		return ErrNotFound // not a member
	}

	// 3. Use MAX semantics: only advance forward, never backward (D-012).
	// Use CASE WHEN which is standard SQL and works across SQLite, PostgreSQL,
	// and MySQL (unlike MAX(a,b) scalar which is SQLite-only).
	result := cs.db.WithContext(ctx).
		Model(&model.Conversation{}).
		Where("id = ?", convID).
		Update(column, gorm.Expr("CASE WHEN ? > ? THEN ? ELSE ? END", gorm.Expr(column), messageID, gorm.Expr(column), messageID))
	if result.Error != nil {
		return classifyError(fmt.Errorf("store: update last read: %w", result.Error))
	}
	return nil
}

// UpdateAgentStatus updates conversation agent state machine fields.
// This is called when the agent transitions to a new status (e.g., idle → thinking → asking_user).
// The agent_last_activity and updated_at fields are automatically set to the current time.
// Returns the timestamp used for the update so callers can use it for broadcasts.
func (cs *ConversationStore) UpdateAgentStatus(ctx context.Context, conversationID, agentStatus, agentID, checkpointID string) (ts time.Time, err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBConversationUpdateAgentStatus,
		attribute.String(tracing.AttrConversationID, conversationID))
	defer func() { finish(err) }()

	now := time.Now()
	result := cs.db.WithContext(ctx).Model(&model.Conversation{}).
		Where("id = ?", conversationID).
		Updates(map[string]any{
			"agent_status":        agentStatus,
			"agent_id":            agentID,
			"checkpoint_id":       checkpointID,
			"agent_last_activity": now,
			"updated_at":          now,
		})
	if result.Error != nil {
		return time.Time{}, classifyError(fmt.Errorf("store: update agent status: %w", result.Error))
	}
	if result.RowsAffected == 0 {
		return time.Time{}, ErrNotFound
	}
	return now, nil
}

// ClearAgentStatus resets conversation agent state to idle.
// This is called when the agent completes execution or when HITL is resolved.
// It clears agent_id, checkpoint_id and resets agent_status to "idle".
// Returns the timestamp used for the update so callers can use it for broadcasts.
func (cs *ConversationStore) ClearAgentStatus(ctx context.Context, conversationID string) (ts time.Time, err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBConversationClearAgentStatus,
		attribute.String(tracing.AttrConversationID, conversationID))
	defer func() { finish(err) }()

	now := time.Now()
	result := cs.db.WithContext(ctx).Model(&model.Conversation{}).
		Where("id = ?", conversationID).
		Updates(map[string]any{
			"agent_status":        model.AgentStatusIdle,
			"agent_id":            "",
			"checkpoint_id":       "",
			"agent_last_activity": now,
			"updated_at":          now,
		})
	if result.Error != nil {
		return time.Time{}, classifyError(fmt.Errorf("store: clear agent status: %w", result.Error))
	}
	if result.RowsAffected == 0 {
		return time.Time{}, ErrNotFound
	}
	return now, nil
}

// ListStaleHITLConversations returns conversations stuck in asking_user or
// tool_calling status with updated_at older than maxAge. Results are limited
// to the given count. Used by the HITL timeout cleanup task (D-123 / D-137).
func (cs *ConversationStore) ListStaleHITLConversations(ctx context.Context, maxAge time.Duration, limit int) (result []*model.Conversation, err error) {
	ctx, finish := startSpan(ctx, tracing.SpanDBConversationListStaleHITL)
	defer func() { finish(err) }()

	cutoff := time.Now().Add(-maxAge)
	var conversations []*model.Conversation
	err = cs.db.WithContext(ctx).
		Where("agent_status IN (?, ?) AND agent_last_activity < ?",
			model.AgentStatusAskingUser, model.AgentStatusToolCalling, cutoff).
		Order("agent_last_activity ASC").
		Limit(limit).
		Find(&conversations).Error
	if err != nil {
		return nil, classifyError(fmt.Errorf("store: list stale HITL conversations: %w", err))
	}
	return conversations, nil
}

// escapeLikePattern escapes special LIKE characters (%, _, |) in the input so
// they are treated as literal characters in LIKE expressions.  The pipe
// character '|' is used as the escape character (passed via ESCAPE '|' in the
// SQL query) because it works consistently across SQLite, PostgreSQL, and
// MySQL — unlike backslash, whose escaping rules vary by dialect.
func escapeLikePattern(s string) string {
	s = strings.ReplaceAll(s, "|", "||")
	s = strings.ReplaceAll(s, "%", "|%")
	s = strings.ReplaceAll(s, "_", "|_")
	return s
}
