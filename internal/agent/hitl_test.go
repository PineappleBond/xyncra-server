package agent

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// resolveSubAgents tests
// ---------------------------------------------------------------------------

func TestResolveSubAgents_EmptyList(t *testing.T) {
	b := &AgentBuilder{}
	tools, err := b.resolveSubAgents(context.Background(), &AgentConfig{ID: "parent"})
	require.NoError(t, err)
	assert.Nil(t, tools)
}

func TestResolveSubAgents_UnknownID_Skipped(t *testing.T) {
	reg := NewRegistry() // empty registry
	b := &AgentBuilder{registry: reg}

	config := &AgentConfig{
		ID:        "parent",
		SubAgents: []string{"nonexistent"},
	}
	tools, err := b.resolveSubAgents(context.Background(), config)
	require.NoError(t, err)
	assert.Empty(t, tools, "unknown sub-agent should be skipped (fail-open)")
}

// ---------------------------------------------------------------------------
// StreamBridge BridgeWithInterrupt tests
// ---------------------------------------------------------------------------

func TestBridgeWithInterrupt_NoInterrupt_NormalCompletion(t *testing.T) {
	sb := NewStreamBridge()
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		gen.Send(&adk.AgentEvent{
			Output: &adk.AgentOutput{
				MessageOutput: &adk.MessageVariant{
					Message: &schema.Message{Content: "hello"},
				},
			},
		})
		gen.Close()
	}()

	outCh := make(chan StreamChunk, 10)
	interruptCh := make(chan *InterruptInfo, 1)

	sb.BridgeWithInterrupt(context.Background(), iter, outCh, interruptCh)

	var chunks []StreamChunk
	for c := range outCh {
		chunks = append(chunks, c)
	}

	require.NotEmpty(t, chunks)
	last := chunks[len(chunks)-1]
	assert.True(t, last.IsDone, "last chunk should be IsDone")

	// No interrupt should have been signalled (channel is closed but empty).
	info, ok := <-interruptCh
	assert.False(t, ok || info != nil, "interruptCh should have no interrupt on normal completion")
}

func TestBridgeWithInterrupt_InterruptDetected(t *testing.T) {
	sb := NewStreamBridge()
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		gen.Send(&adk.AgentEvent{
			Output: &adk.AgentOutput{
				MessageOutput: &adk.MessageVariant{
					Message: &schema.Message{Content: "partial"},
				},
			},
		})
		gen.Send(&adk.AgentEvent{
			Action: &adk.AgentAction{
				Interrupted: &adk.InterruptInfo{
					Data: "What color?",
				},
			},
		})
		gen.Close()
	}()

	outCh := make(chan StreamChunk, 10)
	interruptCh := make(chan *InterruptInfo, 1)

	sb.BridgeWithInterrupt(context.Background(), iter, outCh, interruptCh)

	// Drain outCh.
	for range outCh {
	}

	info := <-interruptCh
	require.NotNil(t, info)
	assert.Equal(t, "What color?", info.Question)
}

func TestBridgeWithInterrupt_InterruptFlushesBuffer(t *testing.T) {
	sb := NewStreamBridge()
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		gen.Send(&adk.AgentEvent{
			Output: &adk.AgentOutput{
				MessageOutput: &adk.MessageVariant{
					Message: &schema.Message{Content: "buffered-text"},
				},
			},
		})
		gen.Send(&adk.AgentEvent{
			Action: &adk.AgentAction{
				Interrupted: &adk.InterruptInfo{Data: "q"},
			},
		})
		gen.Close()
	}()

	outCh := make(chan StreamChunk, 10)
	interruptCh := make(chan *InterruptInfo, 1)

	sb.BridgeWithInterrupt(context.Background(), iter, outCh, interruptCh)

	var hasContent bool
	for c := range outCh {
		if c.Content == "buffered-text" {
			hasContent = true
		}
	}
	assert.True(t, hasContent, "buffered text should be flushed before interrupt")
}

func TestBridgeWithInterrupt_BothChannelsClosed(t *testing.T) {
	sb := NewStreamBridge()
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	gen.Close()

	outCh := make(chan StreamChunk, 10)
	interruptCh := make(chan *InterruptInfo, 1)

	sb.BridgeWithInterrupt(context.Background(), iter, outCh, interruptCh)

	// Drain outCh fully — should get IsDone chunk, then channel closed.
	for range outCh {
	}
	// After draining, outCh is closed.
	_, outOk := <-outCh
	assert.False(t, outOk, "outCh should be closed after draining")
	_, intOk := <-interruptCh
	assert.False(t, intOk, "interruptCh should be closed")
}

func TestBridgeWithInterrupt_NonStringData(t *testing.T) {
	sb := NewStreamBridge()
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		gen.Send(&adk.AgentEvent{
			Action: &adk.AgentAction{
				Interrupted: &adk.InterruptInfo{Data: 42},
			},
		})
		gen.Close()
	}()

	outCh := make(chan StreamChunk, 10)
	interruptCh := make(chan *InterruptInfo, 1)

	sb.BridgeWithInterrupt(context.Background(), iter, outCh, interruptCh)

	for range outCh {
	}

	info := <-interruptCh
	require.NotNil(t, info)
	assert.Empty(t, info.Question, "non-string Data should produce empty question")
}

// ---------------------------------------------------------------------------
// HITL error sentinel tests
// ---------------------------------------------------------------------------

func TestErrHITLInterrupted_Defined(t *testing.T) {
	assert.NotNil(t, ErrHITLInterrupted)
	assert.Contains(t, ErrHITLInterrupted.Error(), "HITL")
}

func TestErrCheckpointStoreSet_Defined(t *testing.T) {
	assert.NotNil(t, ErrCheckpointStoreSet)
}

func TestErrCheckpointNotFound_Defined(t *testing.T) {
	assert.NotNil(t, ErrCheckpointNotFound)
}

// ---------------------------------------------------------------------------
// Protocol constants
// ---------------------------------------------------------------------------

func TestProtocol_AgentUpdateTypes_Defined(t *testing.T) {
	// D-125: removed UpdateTypeAgentQuestion and UpdateTypeAgentCheckpointCreated
	// (HITL info now delivered via conversation update + get_conversation RPC).
	types := []string{
		protocol.UpdateTypeAgentStatus,
		protocol.UpdateTypeAgentTimeout,
	}
	for _, typ := range types {
		assert.NotEmpty(t, typ)
	}
}
