package tracing

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSpanNames_NotEmpty(t *testing.T) {
	names := []string{
		SpanWSConnection, SpanWSMessageReceive, SpanWSMessageSend,
		SpanHandlerInvoke, SpanBrokerEnqueue, SpanBrokerProcess,
		SpanHandlerBroadcast, SpanAgentExecute, SpanAgentBuild,
		SpanAgentRun, SpanAgentLLMCall, SpanAgentToolCall,
		SpanAgentCheckpointSave, SpanAgentStream,
	}
	for _, name := range names {
		assert.NotEmpty(t, name, "span name constant must not be empty")
	}
}

func TestSpanNames_Unique(t *testing.T) {
	names := []string{
		SpanWSConnection, SpanWSMessageReceive, SpanWSMessageSend,
		SpanHandlerInvoke, SpanBrokerEnqueue, SpanBrokerProcess,
		SpanHandlerBroadcast, SpanAgentExecute, SpanAgentBuild,
		SpanAgentRun, SpanAgentLLMCall, SpanAgentToolCall,
		SpanAgentCheckpointSave, SpanAgentStream,
	}
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		assert.False(t, seen[name], "duplicate span name: %s", name)
		seen[name] = true
	}
}

func TestAttributeKeys_NotEmpty(t *testing.T) {
	keys := []string{
		AttrUserID, AttrDeviceID, AttrConnID, AttrMethod,
		AttrAgentID, AttrConversationID, AttrTaskType,
		AttrTaskID, AttrIteration, AttrToolName,
		AttrModel, AttrInputTokens, AttrOutputTokens,
		AttrTotalTokens, AttrDurationMs, AttrCheckpointID,
		AttrChunkCount, AttrTotalChars, AttrDebug,
	}
	for _, key := range keys {
		assert.NotEmpty(t, key, "attribute key constant must not be empty")
	}
}

func TestAttributeKeys_HaveXyncraPrefix(t *testing.T) {
	keys := []string{
		AttrUserID, AttrDeviceID, AttrConnID, AttrMethod,
		AttrAgentID, AttrConversationID, AttrTaskType,
		AttrTaskID, AttrIteration, AttrToolName,
		AttrModel, AttrInputTokens, AttrOutputTokens,
		AttrTotalTokens, AttrDurationMs, AttrCheckpointID,
		AttrChunkCount, AttrTotalChars, AttrDebug,
	}
	for _, key := range keys {
		assert.True(t, strings.HasPrefix(key, "xyncra."),
			"attribute key %q should have xyncra. prefix", key)
	}
}
