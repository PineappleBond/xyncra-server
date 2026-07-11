package agent

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
)

// StreamChunk represents a single piece of streaming output.
// Content holds a cumulative text snapshot (D-051): each emitted chunk
// contains the full text accumulated so far, not just the delta. This lets
// receivers replace their display buffer directly without maintaining state.
type StreamChunk struct {
	Content string // Cumulative text snapshot (D-051)
	IsDone  bool   // True when stream completed normally
	Err     error  // Non-nil if stream ended with error
}

// StreamBridge converts Eino's streaming output into Xyncra StreamChunks,
// applying a 50ms throttle for ~20fps streaming (D-051).
type StreamBridge struct {
	throttleInterval time.Duration
}

// NewStreamBridge creates a StreamBridge with the default 50ms throttle
// interval (D-051: 20 frames per second).
func NewStreamBridge() *StreamBridge {
	return &StreamBridge{
		throttleInterval: 50 * time.Millisecond,
	}
}

// Bridge reads events from the Eino AsyncIterator and emits cumulative
// StreamChunks into outCh. The method blocks until the iterator is exhausted,
// an error occurs, or ctx is cancelled. outCh is always closed on return.
//
// Throttling ensures at most ~20 snapshots per second are emitted (D-051).
// Each StreamChunk.Content is the full accumulated text, so dropped frames
// do not affect correctness — the receiver simply overwrites its buffer.
func (sb *StreamBridge) Bridge(ctx context.Context, iter *adk.AsyncIterator[*adk.AgentEvent], outCh chan<- StreamChunk) {
	defer close(outCh)

	var buffer strings.Builder
	ticker := time.NewTicker(sb.throttleInterval)
	defer ticker.Stop()

	// textEvent carries accumulated text (or terminal signals) from the
	// event-reading goroutine to the throttle loop below.
	type textEvent struct {
		text string
		done bool
		err  error
	}
	textCh := make(chan textEvent, 64)

	// Goroutine: consume events from the iterator and forward text deltas.
	go func() {
		defer close(textCh)
		for {
			// Wrap iter.Next() to make it cancellable via context.
			type iterResult struct {
				event *adk.AgentEvent
				ok    bool
			}
			ch := make(chan iterResult, 1)
			go func() {
				e, ok := iter.Next()
				ch <- iterResult{e, ok}
			}()
			select {
			case <-ctx.Done():
				return
			case r := <-ch:
				if !r.ok {
					textCh <- textEvent{done: true}
					return
				}
				if r.event.Err != nil {
					textCh <- textEvent{err: r.event.Err}
					return
				}
				if r.event.Output != nil && r.event.Output.MessageOutput != nil {
					mv := r.event.Output.MessageOutput
					if mv.IsStreaming {
						for {
							chunk, recvErr := mv.MessageStream.Recv()
							if errors.Is(recvErr, io.EOF) {
								break
							}
							if recvErr != nil {
								textCh <- textEvent{err: recvErr}
								return
							}
							if chunk != nil && chunk.Content != "" {
								textCh <- textEvent{text: chunk.Content}
							}
						}
					} else {
						if mv.Message != nil && mv.Message.Content != "" {
							textCh <- textEvent{text: mv.Message.Content}
						}
					}
				}
			}
		}
	}()

	// Main loop: throttle and emit cumulative snapshots (D-051).
	for {
		select {
		case <-ctx.Done():
			// Flush remaining buffer on cancellation.
			if buffer.Len() > 0 {
				outCh <- StreamChunk{Content: buffer.String()}
			}
			return
		case te, ok := <-textCh:
			if !ok {
				// Channel closed without an explicit done signal — treat as done.
				if buffer.Len() > 0 {
					outCh <- StreamChunk{Content: buffer.String()}
				}
				outCh <- StreamChunk{IsDone: true}
				return
			}
			if te.err != nil {
				if buffer.Len() > 0 {
					outCh <- StreamChunk{Content: buffer.String()}
				}
				outCh <- StreamChunk{Err: te.err}
				return
			}
			if te.done {
				if buffer.Len() > 0 {
					outCh <- StreamChunk{Content: buffer.String()}
				}
				outCh <- StreamChunk{IsDone: true}
				return
			}
			buffer.WriteString(te.text)
		case <-ticker.C:
			if buffer.Len() > 0 {
				outCh <- StreamChunk{Content: buffer.String()}
			}
		}
	}
}
