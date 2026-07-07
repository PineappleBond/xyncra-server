package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/PineappleBond/xyncra-server/internal/store/model"
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
func (cs *ConversationStore) Create(ctx context.Context, conv *model.Conversation) error {
	if err := cs.db.WithContext(ctx).Create(conv).Error; err != nil {
		return classifyError(err)
	}
	return nil
}

// Get retrieves a conversation by its primary key. Returns ErrNotFound if no
// record exists.
func (cs *ConversationStore) Get(ctx context.Context, id string) (*model.Conversation, error) {
	var conv model.Conversation
	err := cs.db.WithContext(ctx).
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
func (cs *ConversationStore) GetByUsers(ctx context.Context, user1, user2 string) (*model.Conversation, error) {
	var conv model.Conversation
	err := cs.db.WithContext(ctx).
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
func (cs *ConversationStore) GetByUser(ctx context.Context, userID string, offset, limit int) ([]*model.Conversation, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	// Fetch up to (offset+limit) from each side, then merge, deduplicate, and paginate.
	fetchLimit := offset + limit

	var asUser1 []*model.Conversation
	if err := cs.db.WithContext(ctx).
		Where("user_id1 = ?", userID).
		Order("last_message_at DESC").
		Limit(fetchLimit).
		Find(&asUser1).Error; err != nil {
		return nil, classifyError(fmt.Errorf("store: get conversations by user (user1): %w", err))
	}

	var asUser2 []*model.Conversation
	if err := cs.db.WithContext(ctx).
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
func (cs *ConversationStore) Update(ctx context.Context, conv *model.Conversation) error {
	if err := cs.db.WithContext(ctx).Save(conv).Error; err != nil {
		return classifyError(err)
	}
	return nil
}

// Delete performs a soft delete on the conversation identified by id.
func (cs *ConversationStore) Delete(ctx context.Context, id string) error {
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
func (cs *ConversationStore) Restore(ctx context.Context, id string) error {
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
func (cs *ConversationStore) UpdateLastMessage(ctx context.Context, convID string, lastMessageAt time.Time, lastProcessedMessageID uint32) error {
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
func (cs *ConversationStore) SearchByTitle(ctx context.Context, userID, title string, limit int) ([]*model.Conversation, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if title == "" {
		return []*model.Conversation{}, nil
	}

	like := "%" + escapeLikePattern(title) + "%"

	var convs []*model.Conversation
	err := cs.db.WithContext(ctx).
		Where("(user_id1 = ? OR user_id2 = ?) AND title LIKE ?", userID, userID, like).
		Order("last_message_at DESC").
		Limit(limit).
		Find(&convs).Error
	if err != nil {
		return nil, classifyError(fmt.Errorf("store: search conversations by title: %w", err))
	}
	return convs, nil
}

// escapeLikePattern escapes special LIKE characters (%, _, \) in the input so
// they are treated as literal characters in LIKE expressions.
func escapeLikePattern(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}
