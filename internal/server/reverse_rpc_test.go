package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// noopLogger satisfies the Logger interface without producing output.
type noopLogger struct{}

func (noopLogger) Info(string, ...any)  {}
func (noopLogger) Error(string, ...any) {}
func (noopLogger) Debug(string, ...any) {}

// sendCall records a single invocation of the mock send function.
type sendCall struct {
	userID string
	pkg    *protocol.Package
}

// mockSendFunc is a thread-safe mock for the sendFunc callback.
type mockSendFunc struct {
	mu    sync.Mutex
	calls []sendCall
	err   error
}

func (m *mockSendFunc) Send(userID string, pkg *protocol.Package) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.calls = append(m.calls, sendCall{userID: userID, pkg: pkg})
	return nil
}

func (m *mockSendFunc) lastCall() sendCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls[len(m.calls)-1]
}

func (m *mockSendFunc) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// waitForCalls polls until the mock has received at least n calls or timeout.
func (m *mockSendFunc) waitForCalls(n int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if m.callCount() >= n {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return m.callCount() >= n
}

// newTestReverseRPC creates a ReverseRPC wired to the given mockSendFunc.
func newTestReverseRPC(ms *mockSendFunc) *ReverseRPC {
	return NewReverseRPC(ReverseRPCConfig{
		SendFunc: ms.Send,
		Logger:   noopLogger{},
	})
}

// extractRequestID unmarshals the Package.Data to read the request ID.
func extractRequestID(t *testing.T, pkg *protocol.Package) string {
	t.Helper()
	var req protocol.PackageDataRequest
	require.NoError(t, json.Unmarshal(pkg.Data, &req))
	return req.ID
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestReverseRPC_ServerRequest_BasicFlow(t *testing.T) {
	ms := &mockSendFunc{}
	rpc := newTestReverseRPC(ms)

	var result *protocol.PackageDataResponse
	var reqErr error
	done := make(chan struct{})

	go func() {
		defer close(done)
		result, reqErr = rpc.ServerRequest(context.Background(), "user-1", "ping", nil, 2*time.Second)
	}()

	// Wait for the send to happen.
	require.True(t, ms.waitForCalls(1, time.Second), "sendFunc was not called")

	reqID := extractRequestID(t, ms.lastCall().pkg)

	// Simulate client response.
	rpc.DispatchResponse(&protocol.PackageDataResponse{
		ID:   reqID,
		Code: 0,
		Msg:  "pong",
		Data: json.RawMessage(`{"reply":"ok"}`),
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ServerRequest did not return in time")
	}

	require.NoError(t, reqErr)
	require.NotNil(t, result)
	assert.Equal(t, reqID, result.ID)
	assert.Equal(t, protocol.ResponseCode(0), result.Code)
	assert.Equal(t, "pong", result.Msg)
}

func TestReverseRPC_ServerRequest_Timeout(t *testing.T) {
	ms := &mockSendFunc{}
	rpc := newTestReverseRPC(ms)

	_, err := rpc.ServerRequest(context.Background(), "user-1", "ping", nil, 100*time.Millisecond)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestReverseRPC_DispatchResponse_UnknownID(t *testing.T) {
	ms := &mockSendFunc{}
	rpc := newTestReverseRPC(ms)

	// Should not panic.
	assert.NotPanics(t, func() {
		rpc.DispatchResponse(&protocol.PackageDataResponse{
			ID:   "s-99999",
			Code: 0,
		})
	})
}

func TestReverseRPC_DispatchResponse_Duplicate(t *testing.T) {
	ms := &mockSendFunc{}
	rpc := newTestReverseRPC(ms)

	var firstResult *protocol.PackageDataResponse
	done := make(chan struct{})

	go func() {
		defer close(done)
		firstResult, _ = rpc.ServerRequest(context.Background(), "user-1", "ping", nil, 2*time.Second)
	}()

	require.True(t, ms.waitForCalls(1, time.Second))
	reqID := extractRequestID(t, ms.lastCall().pkg)

	resp := &protocol.PackageDataResponse{
		ID:   reqID,
		Code: 0,
		Msg:  "first",
	}

	// First dispatch should be accepted.
	rpc.DispatchResponse(resp)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ServerRequest did not return after first dispatch")
	}
	require.NotNil(t, firstResult)
	assert.Equal(t, "first", firstResult.Msg)

	// Second dispatch for the same ID should be silently ignored (no panic).
	assert.NotPanics(t, func() {
		rpc.DispatchResponse(&protocol.PackageDataResponse{
			ID:   reqID,
			Code: 0,
			Msg:  "second",
		})
	})
}

func TestReverseRPC_CancelAll(t *testing.T) {
	ms := &mockSendFunc{}
	rpc := newTestReverseRPC(ms)

	var result *protocol.PackageDataResponse
	var reqErr error
	done := make(chan struct{})

	go func() {
		defer close(done)
		result, reqErr = rpc.ServerRequest(context.Background(), "user-1", "ping", nil, 5*time.Second)
	}()

	require.True(t, ms.waitForCalls(1, time.Second))

	rpc.CancelAll()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ServerRequest did not return after CancelAll")
	}

	// CancelAll delivers a synthetic response with Code=-1 and Msg="reverse rpc cancelled"
	// via respCh, so ServerRequest returns it as a normal response (err == nil).
	// The caller is expected to inspect result.Code for errors.
	if reqErr != nil {
		// In a rare race the ctx.Done() branch may win first.
		assert.ErrorIs(t, reqErr, context.Canceled)
	} else {
		require.NotNil(t, result)
		assert.Equal(t, protocol.ResponseCode(-1), result.Code)
		assert.Contains(t, result.Msg, "reverse rpc cancelled")
	}
}

func TestReverseRPC_ConcurrentRequests(t *testing.T) {
	ms := &mockSendFunc{}
	rpc := newTestReverseRPC(ms)

	const n = 10
	type outcome struct {
		resp *protocol.PackageDataResponse
		err  error
	}
	results := make([]outcome, n)
	var wg sync.WaitGroup

	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			resp, err := rpc.ServerRequest(context.Background(), "user-1", "ping", nil, 5*time.Second)
			results[idx] = outcome{resp: resp, err: err}
		}(i)
	}

	// Wait for all sends, then dispatch responses for each.
	require.True(t, ms.waitForCalls(n, 2*time.Second))

	ms.mu.Lock()
	callsSnapshot := make([]sendCall, len(ms.calls))
	copy(callsSnapshot, ms.calls)
	ms.mu.Unlock()

	for _, call := range callsSnapshot {
		reqID := extractRequestID(t, call.pkg)
		rpc.DispatchResponse(&protocol.PackageDataResponse{
			ID:   reqID,
			Code: 0,
			Msg:  "ok-" + reqID,
		})
	}

	wg.Wait()

	for i, r := range results {
		assert.NoError(t, r.err, "request %d should not error", i)
		require.NotNil(t, r.resp, "request %d should have a response", i)
		assert.True(t, strings.HasPrefix(r.resp.Msg, "ok-s-"), "unexpected msg: %s", r.resp.Msg)
	}
}

func TestReverseRPC_SendFuncError(t *testing.T) {
	expectedErr := errors.New("send failed")
	ms := &mockSendFunc{err: expectedErr}
	rpc := newTestReverseRPC(ms)

	_, err := rpc.ServerRequest(context.Background(), "user-1", "ping", nil, time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "send request")
	assert.ErrorIs(t, err, expectedErr)
}

func TestReverseRPC_RequestIDNamespace(t *testing.T) {
	ms := &mockSendFunc{}
	rpc := newTestReverseRPC(ms)

	// Fire three requests to confirm sequential IDs.
	for range 3 {
		go func() {
			_, _ = rpc.ServerRequest(context.Background(), "user-1", "ping", nil, 5*time.Second)
		}()
	}

	require.True(t, ms.waitForCalls(3, time.Second))

	ms.mu.Lock()
	defer ms.mu.Unlock()
	for _, call := range ms.calls {
		var req protocol.PackageDataRequest
		require.NoError(t, json.Unmarshal(call.pkg.Data, &req))
		assert.True(t, strings.HasPrefix(req.ID, "s-"), "expected ID with 's-' prefix, got %q", req.ID)
		assert.Equal(t, protocol.PackageTypeRequest, call.pkg.Type)
	}
}

func TestReverseRPC_ServerRequest_ZeroTimeout(t *testing.T) {
	ms := &mockSendFunc{}
	rpc := newTestReverseRPC(ms)

	_, err := rpc.ServerRequest(context.Background(), "user-1", "ping", nil, 0)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestReverseRPC_ContextCancellation(t *testing.T) {
	ms := &mockSendFunc{}
	rpc := newTestReverseRPC(ms)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var reqErr error

	go func() {
		defer close(done)
		_, reqErr = rpc.ServerRequest(ctx, "user-1", "ping", nil, 5*time.Second)
	}()

	require.True(t, ms.waitForCalls(1, time.Second))

	// Cancel the parent context.
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ServerRequest did not return after context cancellation")
	}

	require.Error(t, reqErr)
	assert.ErrorIs(t, reqErr, context.Canceled)
}
