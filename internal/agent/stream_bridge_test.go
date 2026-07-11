package agent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// StreamBridge construction
// ---------------------------------------------------------------------------

func TestNewStreamBridge_DefaultThrottle(t *testing.T) {
	sb := NewStreamBridge()
	assert.Equal(t, 50*time.Millisecond, sb.throttleInterval)
}

// ---------------------------------------------------------------------------
// StreamChunk struct
// ---------------------------------------------------------------------------

func TestStreamChunk_Fields(t *testing.T) {
	chunk := StreamChunk{
		Content: "hello world",
		IsDone:  false,
		Err:     nil,
	}
	assert.Equal(t, "hello world", chunk.Content)
	assert.False(t, chunk.IsDone)
	assert.NoError(t, chunk.Err)

	errChunk := StreamChunk{
		Content: "",
		IsDone:  false,
		Err:     fmt.Errorf("stream error"),
	}
	assert.Error(t, errChunk.Err)
}

// ---------------------------------------------------------------------------
// Bridge with immediate done (empty iterator)
// ---------------------------------------------------------------------------

func TestBridge_ImmediateDone(t *testing.T) {
	sb := NewStreamBridge()
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	gen.Close() // Close immediately — no events.

	ctx := context.Background()
	outCh := make(chan StreamChunk, 64)

	go sb.Bridge(ctx, iter, outCh)

	var chunks []StreamChunk
	for chunk := range outCh {
		chunks = append(chunks, chunk)
	}

	// Expect exactly one chunk: IsDone=true.
	require.Len(t, chunks, 1)
	assert.True(t, chunks[0].IsDone)
	assert.Empty(t, chunks[0].Content)
	assert.NoError(t, chunks[0].Err)
}

// ---------------------------------------------------------------------------
// Bridge with context cancellation
// ---------------------------------------------------------------------------

func TestBridge_ContextCancellation(t *testing.T) {
	sb := NewStreamBridge()
	iter, _ := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	// Do NOT close the generator — iterator stays open.

	ctx, cancel := context.WithCancel(context.Background())
	outCh := make(chan StreamChunk, 64)

	go sb.Bridge(ctx, iter, outCh)

	// Let the bridge start, then cancel.
	time.Sleep(20 * time.Millisecond)
	cancel()

	// outCh should be closed after Bridge returns.
	var chunks []StreamChunk
	for chunk := range outCh {
		chunks = append(chunks, chunk)
	}

	// No panic occurred, and the channel was closed.
	// The bridge may or may not emit chunks before cancellation.
	assert.True(t, true, "Bridge exited cleanly after context cancellation")
}

// ---------------------------------------------------------------------------
// Bridge with error event
// ---------------------------------------------------------------------------

func TestBridge_ErrorEvent(t *testing.T) {
	sb := NewStreamBridge()
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	ctx := context.Background()
	outCh := make(chan StreamChunk, 64)

	go sb.Bridge(ctx, iter, outCh)

	// Send an error event.
	gen.Send(&adk.AgentEvent{
		Err: fmt.Errorf("LLM exploded"),
	})
	gen.Close()

	var chunks []StreamChunk
	for chunk := range outCh {
		chunks = append(chunks, chunk)
	}

	require.GreaterOrEqual(t, len(chunks), 1)

	// The last chunk should have the error.
	lastChunk := chunks[len(chunks)-1]
	assert.Error(t, lastChunk.Err)
	assert.Contains(t, lastChunk.Err.Error(), "LLM exploded")
}

// ---------------------------------------------------------------------------
// Bridge with non-streaming message content
// ---------------------------------------------------------------------------

func TestBridge_NonStreamingMessage(t *testing.T) {
	sb := NewStreamBridge()
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	ctx := context.Background()
	outCh := make(chan StreamChunk, 64)

	go sb.Bridge(ctx, iter, outCh)

	// Send a non-streaming event with a complete message.
	gen.Send(&adk.AgentEvent{
		Output: &adk.AgentOutput{
			MessageOutput: &adk.MessageVariant{
				IsStreaming: false,
				Message:     &schema.Message{Content: "complete response"},
			},
		},
	})
	gen.Close()

	var chunks []StreamChunk
	for chunk := range outCh {
		chunks = append(chunks, chunk)
	}

	// Expect at least one content chunk + IsDone.
	hasContent := false
	hasDone := false
	for _, chunk := range chunks {
		if chunk.Content == "complete response" {
			hasContent = true
		}
		if chunk.IsDone {
			hasDone = true
		}
	}
	assert.True(t, hasContent, "should emit the non-streaming message content")
	assert.True(t, hasDone, "should emit IsDone after iterator closes")
}

// ---------------------------------------------------------------------------
// Bridge with streaming content (cumulative snapshots, D-051)
// ---------------------------------------------------------------------------

func TestBridge_StreamingContent(t *testing.T) {
	sb := NewStreamBridge()
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	ctx := context.Background()
	outCh := make(chan StreamChunk, 64)

	go sb.Bridge(ctx, iter, outCh)

	// Create a streaming message using schema.Pipe.
	sr, sw := schema.Pipe[*schema.Message](10)
	go func() {
		sw.Send(&schema.Message{Content: "Hello"}, nil)
		sw.Send(&schema.Message{Content: " World"}, nil)
		sw.Close() // Sends io.EOF
	}()

	// Send a streaming event.
	gen.Send(&adk.AgentEvent{
		Output: &adk.AgentOutput{
			MessageOutput: &adk.MessageVariant{
				IsStreaming:   true,
				MessageStream: sr,
			},
		},
	})
	gen.Close()

	var chunks []StreamChunk
	for chunk := range outCh {
		chunks = append(chunks, chunk)
	}

	// Collect all content chunks and verify cumulative text.
	var contentChunks []StreamChunk
	hasDone := false
	for _, chunk := range chunks {
		if chunk.IsDone {
			hasDone = true
		}
		if chunk.Content != "" {
			contentChunks = append(contentChunks, chunk)
		}
	}

	assert.True(t, hasDone, "should emit IsDone")
	require.GreaterOrEqual(t, len(contentChunks), 1, "should emit at least one content chunk")

	// The final content chunk should contain the full accumulated text.
	lastContent := contentChunks[len(contentChunks)-1].Content
	assert.Contains(t, lastContent, "Hello")
	assert.Contains(t, lastContent, "World")
}

// ---------------------------------------------------------------------------
// Bridge outCh is always closed
// ---------------------------------------------------------------------------

func TestBridge_OutChAlwaysClosed(t *testing.T) {
	sb := NewStreamBridge()
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	gen.Close()

	ctx := context.Background()
	outCh := make(chan StreamChunk, 64)

	done := make(chan struct{})
	go func() {
		sb.Bridge(ctx, iter, outCh)
		close(done)
	}()

	// Drain the channel.
	for range outCh {
	}

	// Verify Bridge returned (done channel closed).
	select {
	case <-done:
		// Success.
	case <-time.After(2 * time.Second):
		t.Fatal("Bridge did not return within timeout")
	}
}

// ---------------------------------------------------------------------------
// Bridge flushes accumulated buffer before emitting error
// ---------------------------------------------------------------------------

func TestBridge_FlushesBufferOnError(t *testing.T) {
	sb := NewStreamBridge()
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	ctx := context.Background()
	outCh := make(chan StreamChunk, 64)

	go sb.Bridge(ctx, iter, outCh)

	// Send a streaming event with text content.
	sr, sw := schema.Pipe[*schema.Message](10)
	go func() {
		sw.Send(&schema.Message{Content: "partial text"}, nil)
		sw.Close()
	}()

	gen.Send(&adk.AgentEvent{
		Output: &adk.AgentOutput{
			MessageOutput: &adk.MessageVariant{
				IsStreaming:   true,
				MessageStream: sr,
			},
		},
	})

	// Give the goroutine time to consume the streaming event before sending error.
	time.Sleep(50 * time.Millisecond)

	// Now send an error event.
	gen.Send(&adk.AgentEvent{
		Err: fmt.Errorf("upstream failure"),
	})
	gen.Close()

	var chunks []StreamChunk
	for chunk := range outCh {
		chunks = append(chunks, chunk)
	}

	// Verify: at least one content chunk was emitted before the error chunk.
	var contentChunk *StreamChunk
	var errorChunk *StreamChunk
	for i := range chunks {
		if chunks[i].Content != "" && contentChunk == nil {
			contentChunk = &chunks[i]
		}
		if chunks[i].Err != nil && errorChunk == nil {
			errorChunk = &chunks[i]
		}
	}

	require.NotNil(t, contentChunk, "should emit content chunk before error")
	assert.Contains(t, contentChunk.Content, "partial text")
	require.NotNil(t, errorChunk, "should emit error chunk")
	assert.Error(t, errorChunk.Err)
	assert.Contains(t, errorChunk.Err.Error(), "upstream failure")
}

// ---------------------------------------------------------------------------
// Bridge flushes buffer on context cancellation
// ---------------------------------------------------------------------------

func TestBridge_FlushesBufferOnContextCancel(t *testing.T) {
	sb := NewStreamBridge()
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	ctx, cancel := context.WithCancel(context.Background())
	outCh := make(chan StreamChunk, 64)

	go sb.Bridge(ctx, iter, outCh)

	// Send a non-streaming message with text content.
	gen.Send(&adk.AgentEvent{
		Output: &adk.AgentOutput{
			MessageOutput: &adk.MessageVariant{
				IsStreaming: false,
				Message:     &schema.Message{Content: "accumulated text before cancel"},
			},
		},
	})

	// Give the goroutine time to process the event.
	time.Sleep(100 * time.Millisecond)

	// Cancel the context — Bridge should flush the buffer.
	cancel()

	var chunks []StreamChunk
	for chunk := range outCh {
		chunks = append(chunks, chunk)
	}

	// The channel should be closed (Bridge returned).
	// The last content chunk should contain the accumulated text.
	var lastContent string
	for _, chunk := range chunks {
		if chunk.Content != "" {
			lastContent = chunk.Content
		}
	}
	assert.Contains(t, lastContent, "accumulated text before cancel",
		"Bridge should flush buffer on context cancellation")
}
