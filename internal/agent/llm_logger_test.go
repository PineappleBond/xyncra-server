package agent

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLLMLogger_NoFilter_LogsAll(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLLMLogger(&buf, false)

	// Without CallerDevice
	ctx := context.Background()
	logger.write(ctx, LogRecord{Phase: "test_no_device"})
	assert.Contains(t, buf.String(), `"phase":"test_no_device"`)

	// With CallerDevice (should still log when no filter)
	buf.Reset()
	ctx2 := ContextWithCallerDevice(context.Background(), CallerDevice{
		UserID: "user-1", DeviceID: "device-1",
	})
	logger.write(ctx2, LogRecord{Phase: "test_with_device"})
	assert.Contains(t, buf.String(), `"phase":"test_with_device"`)
}

func TestLLMLogger_FilterMatch_User(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLLMLogger(&buf, false)
	logger.SetDebugFilter([]string{"user-1"}, nil)

	ctx := ContextWithCallerDevice(context.Background(), CallerDevice{
		UserID: "user-1", DeviceID: "device-x",
	})
	logger.write(ctx, LogRecord{Phase: "test"})
	assert.Contains(t, buf.String(), `"phase":"test"`)
}

func TestLLMLogger_FilterMatch_Device(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLLMLogger(&buf, false)
	logger.SetDebugFilter(nil, []string{"device-1"})

	ctx := ContextWithCallerDevice(context.Background(), CallerDevice{
		UserID: "user-x", DeviceID: "device-1",
	})
	logger.write(ctx, LogRecord{Phase: "test"})
	assert.Contains(t, buf.String(), `"phase":"test"`)
}

func TestLLMLogger_FilterNoMatch_Skips(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLLMLogger(&buf, false)
	logger.SetDebugFilter([]string{"user-1"}, []string{"device-1"})

	ctx := ContextWithCallerDevice(context.Background(), CallerDevice{
		UserID: "user-2", DeviceID: "device-2",
	})
	logger.write(ctx, LogRecord{Phase: "test"})
	assert.Empty(t, buf.String())
}

func TestLLMLogger_FilterNoCallerDevice_Skips(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLLMLogger(&buf, false)
	logger.SetDebugFilter([]string{"user-1"}, nil)

	ctx := context.Background()
	logger.write(ctx, LogRecord{Phase: "test"})
	assert.Empty(t, buf.String())
}

func TestLLMLogger_HasFilter(t *testing.T) {
	logger := NewLLMLogger(&bytes.Buffer{}, false)

	// Fresh logger has no filter
	assert.False(t, logger.HasFilter())

	// After setting non-empty filter
	logger.SetDebugFilter([]string{"user-1"}, nil)
	assert.True(t, logger.HasFilter())

	// After setting with empty slices — filter cleared
	logger.SetDebugFilter(nil, nil)
	assert.False(t, logger.HasFilter())

	// Devices only
	logger.SetDebugFilter(nil, []string{"device-1"})
	assert.True(t, logger.HasFilter())
}

func TestLLMLogger_SetDebugFilter_OverridesPrevious(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLLMLogger(&buf, false)

	// Set initial filter for user-1
	logger.SetDebugFilter([]string{"user-1"}, nil)

	// user-1 should match
	ctx1 := ContextWithCallerDevice(context.Background(), CallerDevice{
		UserID: "user-1", DeviceID: "device-x",
	})
	logger.write(ctx1, LogRecord{Phase: "first"})
	require.Contains(t, buf.String(), `"phase":"first"`)

	// Override filter to only user-2
	buf.Reset()
	logger.SetDebugFilter([]string{"user-2"}, nil)

	// user-1 should no longer match
	logger.write(ctx1, LogRecord{Phase: "second"})
	assert.Empty(t, buf.String())

	// user-2 should now match
	ctx2 := ContextWithCallerDevice(context.Background(), CallerDevice{
		UserID: "user-2", DeviceID: "device-y",
	})
	logger.write(ctx2, LogRecord{Phase: "third"})
	assert.Contains(t, buf.String(), `"phase":"third"`)
}

func TestConvertMessage_NoTruncate(t *testing.T) {
	longContent := strings.Repeat("x", 5000)
	msg := &schema.Message{
		Role:    schema.User,
		Content: longContent,
	}

	// With truncation (noTruncate=false)
	snapTruncated := convertMessage(msg, false)
	assert.True(t, len(snapTruncated.Content) <= 4096+20, "content should be truncated")
	assert.Contains(t, snapTruncated.Content, "...[truncated]")

	// Without truncation (noTruncate=true)
	snapFull := convertMessage(msg, true)
	assert.Equal(t, longContent, snapFull.Content)
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
