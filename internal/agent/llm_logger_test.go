package agent

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLLMLogger_Write(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLLMLogger(&buf, false)

	logger.write(LogRecord{Phase: "test_basic"})
	assert.Contains(t, buf.String(), `"phase":"test_basic"`)
}

func TestConvertMessage_Truncate(t *testing.T) {
	longContent := strings.Repeat("x", 5000)
	msg := &schema.Message{
		Role:    schema.User,
		Content: longContent,
	}

	snap := convertMessage(msg)
	assert.True(t, len(snap.Content) <= 4096+20, "content should be truncated")
	assert.Contains(t, snap.Content, "...[truncated]")
}

func TestConvertMessage_NoTruncate(t *testing.T) {
	shortContent := "hello world"
	msg := &schema.Message{
		Role:    schema.User,
		Content: shortContent,
	}

	snap := convertMessage(msg)
	assert.Equal(t, shortContent, snap.Content)
}

func TestConvertMessage_Nil(t *testing.T) {
	snap := convertMessage(nil)
	assert.Equal(t, "", snap.Role)
	assert.Equal(t, "", snap.Content)
}

func TestToSet(t *testing.T) {
	// Nil input
	assert.Nil(t, toSet(nil))

	// Empty slice
	assert.Nil(t, toSet([]string{}))

	// Non-empty input
	s := toSet([]string{"a", "b", "a"})
	require.NotNil(t, s)
	assert.True(t, s["a"])
	assert.True(t, s["b"])
	assert.False(t, s["c"])
	assert.Len(t, s, 2)
}
