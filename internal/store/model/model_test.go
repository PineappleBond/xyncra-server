package model

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConversation_JSONTags(t *testing.T) {
	conv := &Conversation{
		ID:                     "conv-1",
		UserID1:                "alice",
		UserID2:                "bob",
		Type:                   "1-on-1",
		Title:                  "Test",
		Pinned:                 true,
		Muted:                  false,
		AvatarURL:              "https://example.com/avatar.png",
		Description:            "A test conversation",
		LastProcessedMessageID: 42,
		CreatedAt:              time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:              time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		LastMessageAt:          time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		LastReadMessageID1:     10,
		LastReadMessageID2:     5,
		AgentStatus:            "idle",
	}
	data, err := json.Marshal(conv)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))

	// Verify snake_case keys exist
	expectedKeys := []string{
		"id", "user_id1", "user_id2", "type", "title",
		"pinned", "muted", "avatar_url", "description",
		"last_processed_message_id", "created_at", "updated_at",
		"last_message_at", "last_read_message_id1", "last_read_message_id2",
		"agent_status",
	}
	for _, key := range expectedKeys {
		assert.Contains(t, m, key, "JSON should contain snake_case key %q", key)
	}

	// Verify PascalCase keys do NOT exist
	pascalKeys := []string{"ID", "UserID1", "UserID2", "Type", "Title", "Pinned", "Muted", "AvatarURL", "Description", "LastProcessedMessageID"}
	for _, key := range pascalKeys {
		assert.NotContains(t, m, key, "JSON should NOT contain PascalCase key %q", key)
	}
}

func TestMessage_JSONTags(t *testing.T) {
	msg := &Message{
		ID:              "msg-1",
		ClientMessageID: "client-msg-1",
		ConversationID:  "conv-1",
		MessageID:       1,
		SenderID:        "alice",
		Content:         "Hello",
		Type:            "text",
		ReplyTo:         0,
		Status:          "sent",
		CreatedAt:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	data, err := json.Marshal(msg)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))

	expectedKeys := []string{
		"id", "client_message_id", "conversation_id", "message_id",
		"sender_id", "content", "type", "reply_to", "status", "created_at",
	}
	for _, key := range expectedKeys {
		assert.Contains(t, m, key, "JSON should contain snake_case key %q", key)
	}

	pascalKeys := []string{"ID", "ClientMessageID", "ConversationID", "MessageID", "SenderID", "Content", "Type", "ReplyTo", "Status"}
	for _, key := range pascalKeys {
		assert.NotContains(t, m, key, "JSON should NOT contain PascalCase key %q", key)
	}
}
