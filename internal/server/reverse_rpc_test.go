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
	userID   string
	deviceID string
	pkg      *protocol.Package
}

// mockSendFunc is a thread-safe mock for the sendFunc callback.
type mockSendFunc struct {
	mu    sync.Mutex
	calls []sendCall
	err   error
}

func (m *mockSendFunc) Send(userID, deviceID string, pkg *protocol.Package) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.calls = append(m.calls, sendCall{userID: userID, deviceID: deviceID, pkg: pkg})
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

// ---------------------------------------------------------------------------
// Mock PendingStore
// ---------------------------------------------------------------------------

// mockPendingStore is a mock implementation of PendingStore for testing.
type mockPendingStore struct {
	mu        sync.Mutex
	saved     []*PendingRequest
	saveErr   error
	saveDelay time.Duration // simulated delay for Save
}

func (m *mockPendingStore) Save(ctx context.Context, req *PendingRequest) error {
	if m.saveDelay > 0 {
		time.Sleep(m.saveDelay)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveErr != nil {
		return m.saveErr
	}
	m.saved = append(m.saved, req)
	return nil
}

func (m *mockPendingStore) List(ctx context.Context, userID, deviceID string) ([]*PendingRequest, error) {
	return nil, nil
}

func (m *mockPendingStore) Remove(ctx context.Context, userID, deviceID, requestID string) error {
	return nil
}

func (m *mockPendingStore) RemoveByDevice(ctx context.Context, userID, deviceID string) error {
	return nil
}

func (m *mockPendingStore) Update(ctx context.Context, req *PendingRequest) error {
	return nil
}

func (m *mockPendingStore) savedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.saved)
}

func (m *mockPendingStore) lastSaved() *PendingRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.saved) == 0 {
		return nil
	}
	return m.saved[len(m.saved)-1]
}

func (m *mockPendingStore) allSaved() []*PendingRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*PendingRequest, len(m.saved))
	copy(out, m.saved)
	return out
}

// newTestReverseRPCWithStore creates a ReverseRPC wired to the given mockSendFunc
// and mockPendingStore for testing.
func newTestReverseRPCWithStore(ms *mockSendFunc, ps *mockPendingStore) *ReverseRPC {
	return NewReverseRPC(ReverseRPCConfig{
		SendFunc:     ms.Send,
		Logger:       noopLogger{},
		PendingStore: ps,
	})
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
		result, reqErr = rpc.ServerRequest(context.Background(), "user-1", "device-1", "ping", nil, 2*time.Second)
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

	_, err := rpc.ServerRequest(context.Background(), "user-1", "device-1", "ping", nil, 100*time.Millisecond)
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
		firstResult, _ = rpc.ServerRequest(context.Background(), "user-1", "device-1", "ping", nil, 2*time.Second)
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
		result, reqErr = rpc.ServerRequest(context.Background(), "user-1", "device-1", "ping", nil, 5*time.Second)
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
			resp, err := rpc.ServerRequest(context.Background(), "user-1", "device-1", "ping", nil, 5*time.Second)
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

	_, err := rpc.ServerRequest(context.Background(), "user-1", "device-1", "ping", nil, time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "send request")
	assert.ErrorIs(t, err, expectedErr)
}

func TestReverseRPC_RequestIDNamespace(t *testing.T) {
	ms := &mockSendFunc{}
	rpc := newTestReverseRPC(ms)

	// Fire three requests with a short timeout so they exit quickly via
	// DeadlineExceeded. Use a WaitGroup to avoid goroutine leaks.
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = rpc.ServerRequest(context.Background(), "user-1", "device-1", "ping", nil, 100*time.Millisecond)
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

	// Wait for all goroutines to finish (they will exit via timeout).
	wg.Wait()
}

func TestReverseRPC_ServerRequest_ZeroTimeout(t *testing.T) {
	ms := &mockSendFunc{}
	rpc := newTestReverseRPC(ms)

	_, err := rpc.ServerRequest(context.Background(), "user-1", "device-1", "ping", nil, 0)
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
		_, reqErr = rpc.ServerRequest(ctx, "user-1", "device-1", "ping", nil, 5*time.Second)
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

// ---------------------------------------------------------------------------
// CancelDevice tests
// ---------------------------------------------------------------------------

// TestReverseRPC_CancelDevice verifies that CancelDevice cancels pending
// requests for a specific device while leaving other devices' requests intact.
func TestReverseRPC_CancelDevice(t *testing.T) {
	ms := &mockSendFunc{}
	rpc := newTestReverseRPC(ms)

	// Launch two requests: one for device-1 and one for device-2.
	var result1 *protocol.PackageDataResponse
	var err1 error
	done1 := make(chan struct{})
	go func() {
		defer close(done1)
		result1, err1 = rpc.ServerRequest(context.Background(), "user-1", "device-1", "ping", nil, 5*time.Second)
	}()

	var result2 *protocol.PackageDataResponse
	var err2 error
	done2 := make(chan struct{})
	go func() {
		defer close(done2)
		result2, err2 = rpc.ServerRequest(context.Background(), "user-1", "device-2", "ping", nil, 5*time.Second)
	}()

	// Wait for both sends to happen.
	require.True(t, ms.waitForCalls(2, 2*time.Second), "both requests should be sent")

	// Cancel device-1's pending requests.
	rpc.CancelDevice("user-1", "device-1")

	// device-1's request should return with a "device replaced" response.
	select {
	case <-done1:
	case <-time.After(2 * time.Second):
		t.Fatal("device-1 request did not return after CancelDevice")
	}

	// CancelDevice delivers a synthetic response via respCh, so err is nil
	// but result.Code is -1.
	if err1 != nil {
		// Rare race: ctx.Done() may win first.
		assert.ErrorIs(t, err1, context.Canceled)
	} else {
		require.NotNil(t, result1)
		assert.Equal(t, protocol.ResponseCode(-1), result1.Code)
		assert.Contains(t, result1.Msg, "device replaced")
	}

	// device-2's request should NOT be affected. Verify it is still pending.
	select {
	case <-done2:
		t.Fatal("device-2 request should still be pending")
	case <-time.After(200 * time.Millisecond):
		// Expected: device-2 is still waiting.
	}

	// Dispatch a response for device-2 to clean up.
	ms.mu.Lock()
	var device2ReqID string
	for _, call := range ms.calls {
		var req protocol.PackageDataRequest
		require.NoError(t, json.Unmarshal(call.pkg.Data, &req))
		if call.deviceID == "device-2" {
			device2ReqID = req.ID
			break
		}
	}
	ms.mu.Unlock()

	require.NotEmpty(t, device2ReqID, "device-2 request should have been sent")
	rpc.DispatchResponse(&protocol.PackageDataResponse{
		ID:   device2ReqID,
		Code: 0,
		Msg:  "pong",
	})

	select {
	case <-done2:
	case <-time.After(2 * time.Second):
		t.Fatal("device-2 request did not return after DispatchResponse")
	}

	require.NoError(t, err2)
	require.NotNil(t, result2)
	assert.Equal(t, "pong", result2.Msg)
}

// TestReverseRPC_CancelDevice_NoPending verifies that CancelDevice does not
// panic when there are no pending requests for the specified device.
func TestReverseRPC_CancelDevice_NoPending(t *testing.T) {
	ms := &mockSendFunc{}
	rpc := newTestReverseRPC(ms)

	// No pending requests exist. CancelDevice should not panic.
	assert.NotPanics(t, func() {
		rpc.CancelDevice("user-1", "device-1")
	})
}

// TestReverseRPC_CancelDevice_CrossUserIsolation verifies that CancelDevice
// for one user does NOT cancel pending requests belonging to a different user,
// even when both users share the same deviceID.
func TestReverseRPC_CancelDevice_CrossUserIsolation(t *testing.T) {
	ms := &mockSendFunc{}
	rpc := newTestReverseRPC(ms)

	// Launch a request for user-A, device-1.
	var errA error
	doneA := make(chan struct{})
	go func() {
		defer close(doneA)
		_, errA = rpc.ServerRequest(context.Background(), "user-A", "device-1", "ping", nil, 5*time.Second)
	}()

	// Launch a request for user-B, device-1.
	var resultB *protocol.PackageDataResponse
	var errB error
	doneB := make(chan struct{})
	go func() {
		defer close(doneB)
		resultB, errB = rpc.ServerRequest(context.Background(), "user-B", "device-1", "ping", nil, 5*time.Second)
	}()

	require.True(t, ms.waitForCalls(2, 2*time.Second), "both requests should be sent")

	// Cancel user-A's device. This must NOT affect user-B.
	rpc.CancelDevice("user-A", "device-1")

	// user-A's request should return with a "device replaced" response.
	select {
	case <-doneA:
	case <-time.After(2 * time.Second):
		t.Fatal("user-A request did not return after CancelDevice")
	}
	if errA != nil {
		assert.ErrorIs(t, errA, context.Canceled)
	}

	// user-B's request should still be pending (not cancelled).
	select {
	case <-doneB:
		t.Fatal("user-B request should still be pending after user-A's device was cancelled")
	case <-time.After(200 * time.Millisecond):
		// Expected: user-B is still waiting.
	}

	// Dispatch response for user-B to clean up.
	ms.mu.Lock()
	var userBReqID string
	for _, call := range ms.calls {
		if call.userID == "user-B" {
			var req protocol.PackageDataRequest
			require.NoError(t, json.Unmarshal(call.pkg.Data, &req))
			userBReqID = req.ID
			break
		}
	}
	ms.mu.Unlock()

	require.NotEmpty(t, userBReqID, "user-B request should have been sent")
	rpc.DispatchResponse(&protocol.PackageDataResponse{
		ID:   userBReqID,
		Code: 0,
		Msg:  "pong",
	})

	select {
	case <-doneB:
	case <-time.After(2 * time.Second):
		t.Fatal("user-B request did not return after DispatchResponse")
	}

	require.NoError(t, errB)
	require.NotNil(t, resultB)
	assert.Equal(t, "pong", resultB.Msg)
}

// TestReverseRPC_ReqID_IsUUID verifies that the request ID generated by
// ServerRequest has the format "s-" followed by a valid UUID v4.
func TestReverseRPC_ReqID_IsUUID(t *testing.T) {
	ms := &mockSendFunc{}
	rpc := newTestReverseRPC(ms)

	// Launch a request to capture the sent package.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = rpc.ServerRequest(context.Background(), "user-1", "device-1", "ping", nil, 2*time.Second)
	}()

	require.True(t, ms.waitForCalls(1, time.Second), "sendFunc was not called")

	call := ms.lastCall()
	var req protocol.PackageDataRequest
	require.NoError(t, json.Unmarshal(call.pkg.Data, &req))

	// Verify format: "s-" + UUID v4.
	// UUID v4 regex: [0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}
	expectedPattern := `^s-[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`
	assert.Regexp(t, expectedPattern, req.ID, "reqID should match 's-' + UUID v4 format")

	// Dispatch response to clean up.
	rpc.DispatchResponse(&protocol.PackageDataResponse{
		ID:   req.ID,
		Code: 0,
		Msg:  "ok",
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ServerRequest did not return")
	}
}

// TestReverseRPC_ServerRequest_WithDeviceID verifies that when a deviceID is
// specified, the sendFunc is called with the correct deviceID.
func TestReverseRPC_ServerRequest_WithDeviceID(t *testing.T) {
	ms := &mockSendFunc{}
	rpc := newTestReverseRPC(ms)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = rpc.ServerRequest(context.Background(), "user-1", "device-42", "ping", nil, 2*time.Second)
	}()

	require.True(t, ms.waitForCalls(1, time.Second), "sendFunc was not called")

	ms.mu.Lock()
	lastCall := ms.calls[len(ms.calls)-1]
	ms.mu.Unlock()

	assert.Equal(t, "user-1", lastCall.userID, "userID should be passed to sendFunc")
	assert.Equal(t, "device-42", lastCall.deviceID, "deviceID should be passed to sendFunc")
	assert.Equal(t, protocol.PackageTypeRequest, lastCall.pkg.Type)

	// Dispatch response to clean up.
	var req protocol.PackageDataRequest
	require.NoError(t, json.Unmarshal(lastCall.pkg.Data, &req))
	rpc.DispatchResponse(&protocol.PackageDataResponse{
		ID:   req.ID,
		Code: 0,
		Msg:  "ok",
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ServerRequest did not return")
	}
}

// ---------------------------------------------------------------------------
// CancelDevice / CancelDeviceWithReason tests (Phase 3)
// ---------------------------------------------------------------------------

// TestReverseRPC_CancelDevice_Idempotent verifies that calling CancelDevice
// multiple times does not panic and only the first call delivers a response.
func TestReverseRPC_CancelDevice_Idempotent(t *testing.T) {
	ms := &mockSendFunc{}
	rpc := newTestReverseRPC(ms)

	var result *protocol.PackageDataResponse
	var reqErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		result, reqErr = rpc.ServerRequest(context.Background(), "user-1", "device-1", "ping", nil, 5*time.Second)
	}()

	require.True(t, ms.waitForCalls(1, time.Second), "sendFunc was not called")

	// Call CancelDevice three times; only the first should have effect.
	assert.NotPanics(t, func() {
		rpc.CancelDevice("user-1", "device-1")
		rpc.CancelDevice("user-1", "device-1")
		rpc.CancelDevice("user-1", "device-1")
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ServerRequest did not return after CancelDevice")
	}

	// The first CancelDevice should have delivered a response.
	if reqErr != nil {
		assert.ErrorIs(t, reqErr, context.Canceled)
	} else {
		require.NotNil(t, result)
		assert.Equal(t, protocol.ResponseCode(-1), result.Code)
		assert.Contains(t, result.Msg, "device replaced")
	}
}

// TestReverseRPC_CancelDevice_MultiplePending verifies that CancelDevice
// cancels all pending requests for the same device.
func TestReverseRPC_CancelDevice_MultiplePending(t *testing.T) {
	ms := &mockSendFunc{}
	rpc := newTestReverseRPC(ms)

	const numRequests = 3
	type outcome struct {
		resp *protocol.PackageDataResponse
		err  error
	}
	results := make([]outcome, numRequests)
	dones := make([]chan struct{}, numRequests)

	for i := range numRequests {
		dones[i] = make(chan struct{})
		go func(idx int) {
			defer close(dones[idx])
			results[idx].resp, results[idx].err = rpc.ServerRequest(
				context.Background(), "user-1", "device-1", "ping", nil, 5*time.Second,
			)
		}(i)
	}

	require.True(t, ms.waitForCalls(numRequests, 2*time.Second), "all requests should be sent")

	// Cancel all pending requests for device-1.
	rpc.CancelDevice("user-1", "device-1")

	// All requests should return.
	for i := range numRequests {
		select {
		case <-dones[i]:
		case <-time.After(2 * time.Second):
			t.Fatalf("request %d did not return after CancelDevice", i)
		}

		if results[i].err != nil {
			// Rare race: ctx.Done() may win first.
			assert.ErrorIs(t, results[i].err, context.Canceled)
		} else {
			require.NotNil(t, results[i].resp)
			assert.Equal(t, protocol.ResponseCode(-1), results[i].resp.Code)
			assert.Equal(t, "device replaced", results[i].resp.Msg)
		}
	}
}

// TestReverseRPC_CancelDeviceWithReason_NormalDisconnect verifies that
// CancelDeviceWithReason delivers the custom reason in the response.
func TestReverseRPC_CancelDeviceWithReason_NormalDisconnect(t *testing.T) {
	ms := &mockSendFunc{}
	rpc := newTestReverseRPC(ms)

	var result *protocol.PackageDataResponse
	var reqErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		result, reqErr = rpc.ServerRequest(context.Background(), "user-1", "device-1", "ping", nil, 5*time.Second)
	}()

	require.True(t, ms.waitForCalls(1, time.Second), "sendFunc was not called")

	// Cancel with a custom reason (simulating a normal disconnect).
	rpc.CancelDeviceWithReason("user-1", "device-1", "device disconnected")

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ServerRequest did not return after CancelDeviceWithReason")
	}

	if reqErr != nil {
		assert.ErrorIs(t, reqErr, context.Canceled)
	} else {
		require.NotNil(t, result)
		assert.Equal(t, protocol.ResponseCode(-1), result.Code)
		assert.Equal(t, "device disconnected", result.Msg)
	}
}

// ---------------------------------------------------------------------------
// Phase 4: PendingStore persistence tests (D-103, D-104, D-105, D-106)
// ---------------------------------------------------------------------------

// TestServerRequest_Timeout_PersistsToPendingStore verifies that a timed-out
// request is persisted to the PendingStore with correct fields (D-103).
func TestServerRequest_Timeout_PersistsToPendingStore(t *testing.T) {
	ms := &mockSendFunc{}
	ps := &mockPendingStore{}
	rpc := newTestReverseRPCWithStore(ms, ps)

	params := json.RawMessage(`{"key":"value"}`)
	_, err := rpc.ServerRequest(context.Background(), "user-1", "device-1", "test.method", params, 50*time.Millisecond)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	// persistAsync runs in a goroutine; wait for Save to be called.
	require.Eventually(t, func() bool {
		return ps.savedCount() == 1
	}, 2*time.Second, 5*time.Millisecond, "Save should be called after timeout")

	saved := ps.lastSaved()
	require.NotNil(t, saved)
	assert.Equal(t, "user-1", saved.UserID)
	assert.Equal(t, "device-1", saved.DeviceID)
	assert.Equal(t, "test.method", saved.Method)
	assert.Equal(t, params, saved.Params)
	assert.Equal(t, saved.ID, saved.IdempotencyKey) // D-097: idempotency key = reqID
	assert.True(t, saved.Seq > 0, "seq should be > 0, got %d", saved.Seq)
	assert.Equal(t, 0, saved.RetryCount)
	assert.Equal(t, defaultMaxReplayRetries, saved.MaxRetries)
	assert.False(t, saved.CreatedAt.IsZero(), "CreatedAt should be set")
}

// TestServerRequest_Timeout_PersistsAsync_NonBlocking verifies that a slow Save
// does not block ServerRequest from returning (D-103 async persistence).
func TestServerRequest_Timeout_PersistsAsync_NonBlocking(t *testing.T) {
	ms := &mockSendFunc{}
	ps := &mockPendingStore{saveDelay: 500 * time.Millisecond}
	rpc := newTestReverseRPCWithStore(ms, ps)

	start := time.Now()
	_, err := rpc.ServerRequest(context.Background(), "user-1", "device-1", "ping", nil, 50*time.Millisecond)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	// ServerRequest should return almost immediately after timeout (well under 500ms).
	assert.Less(t, elapsed, 1*time.Second, "ServerRequest should not block on Save")

	// Save should still be called eventually.
	require.Eventually(t, func() bool {
		return ps.savedCount() == 1
	}, 2*time.Second, 5*time.Millisecond, "Save should be called asynchronously")
}

// TestServerRequest_Success_NoPersist verifies that a successful request
// (response received before timeout) does NOT persist to the PendingStore.
func TestServerRequest_Success_NoPersist(t *testing.T) {
	ms := &mockSendFunc{}
	ps := &mockPendingStore{}
	rpc := newTestReverseRPCWithStore(ms, ps)

	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, err := rpc.ServerRequest(context.Background(), "user-1", "device-1", "ping", nil, 2*time.Second)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	}()

	require.True(t, ms.waitForCalls(1, time.Second))
	reqID := extractRequestID(t, ms.lastCall().pkg)

	// Dispatch a response before timeout.
	rpc.DispatchResponse(&protocol.PackageDataResponse{ID: reqID, Code: 0, Msg: "ok"})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ServerRequest did not return")
	}

	// Give a small window for any spurious async persist.
	require.Never(t, func() bool {
		return ps.savedCount() > 0
	}, 200*time.Millisecond, 10*time.Millisecond, "should not persist")
}

// TestServerRequest_ContextCanceled_NoPersist verifies that a parent context
// cancellation (not DeadlineExceeded) does NOT trigger persistence (D-103).
func TestServerRequest_ContextCanceled_NoPersist(t *testing.T) {
	ms := &mockSendFunc{}
	ps := &mockPendingStore{}
	rpc := newTestReverseRPCWithStore(ms, ps)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := rpc.ServerRequest(ctx, "user-1", "device-1", "ping", nil, 5*time.Second)
		assert.ErrorIs(t, err, context.Canceled)
	}()

	require.True(t, ms.waitForCalls(1, time.Second))

	// Cancel the parent context.
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ServerRequest did not return after context cancellation")
	}

	require.Never(t, func() bool {
		return ps.savedCount() > 0
	}, 200*time.Millisecond, 10*time.Millisecond, "should not persist")
}

// TestServerRequest_CancelDevice_NoPersist verifies that CancelDevice does NOT
// trigger persistence (D-105).
func TestServerRequest_CancelDevice_NoPersist(t *testing.T) {
	ms := &mockSendFunc{}
	ps := &mockPendingStore{}
	rpc := newTestReverseRPCWithStore(ms, ps)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := rpc.ServerRequest(context.Background(), "user-1", "device-1", "ping", nil, 5*time.Second)
		// CancelDevice delivers a synthetic response via respCh, so err is nil
		// or context.Canceled in a rare race.
		_ = err
	}()

	require.True(t, ms.waitForCalls(1, time.Second))

	// CancelDevice before timeout.
	rpc.CancelDevice("user-1", "device-1")

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ServerRequest did not return after CancelDevice")
	}

	require.Never(t, func() bool {
		return ps.savedCount() > 0
	}, 200*time.Millisecond, 10*time.Millisecond, "should not persist")
}

// TestServerRequest_PendingStoreNil_NoPersist verifies that a nil PendingStore
// does not cause a panic on timeout (D-103 nil-safe design).
func TestServerRequest_PendingStoreNil_NoPersist(t *testing.T) {
	ms := &mockSendFunc{}
	rpc := newTestReverseRPC(ms) // no PendingStore

	assert.NotPanics(t, func() {
		_, err := rpc.ServerRequest(context.Background(), "user-1", "device-1", "ping", nil, 50*time.Millisecond)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})

	// Small delay to ensure no background goroutine panics.
	time.Sleep(100 * time.Millisecond)
}

// TestServerRequest_PersistError_FailOpen verifies that a Save error does not
// affect the error returned by ServerRequest (fail-open, D-103).
func TestServerRequest_PersistError_FailOpen(t *testing.T) {
	ms := &mockSendFunc{}
	ps := &mockPendingStore{saveErr: errors.New("redis connection lost")}
	rpc := newTestReverseRPCWithStore(ms, ps)

	_, err := rpc.ServerRequest(context.Background(), "user-1", "device-1", "ping", nil, 50*time.Millisecond)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded, "ServerRequest should return DeadlineExceeded even if Save fails")

	// Wait for the async persist attempt to complete.
	time.Sleep(100 * time.Millisecond)
	// The Save was called but returned an error; savedCount should remain 0.
	assert.Equal(t, 0, ps.savedCount(), "failed Save should not add to saved list")
}

// TestServerRequest_Seq_MonotonicallyIncreasing verifies that seq numbers are
// strictly increasing for the same (userID, deviceID) pair (D-106).
func TestServerRequest_Seq_MonotonicallyIncreasing(t *testing.T) {
	ms := &mockSendFunc{}
	ps := &mockPendingStore{}
	rpc := newTestReverseRPCWithStore(ms, ps)

	// Issue 3 sequential timeouts.
	for range 3 {
		_, err := rpc.ServerRequest(context.Background(), "user-1", "device-1", "ping", nil, 50*time.Millisecond)
		require.ErrorIs(t, err, context.DeadlineExceeded)
	}

	// Wait for all 3 async persists.
	require.Eventually(t, func() bool {
		return ps.savedCount() == 3
	}, 2*time.Second, 5*time.Millisecond, "all 3 Saves should be called")

	saved := ps.allSaved()
	require.Len(t, saved, 3)
	assert.Equal(t, uint64(1), saved[0].Seq, "first seq should be 1")
	assert.Equal(t, uint64(2), saved[1].Seq, "second seq should be 2")
	assert.Equal(t, uint64(3), saved[2].Seq, "third seq should be 3")
}

// TestServerRequest_Race_TimeoutVsLateResponse exercises the race between
// timeout-triggered persistence and a late DispatchResponse (D-103/D-105).
func TestServerRequest_Race_TimeoutVsLateResponse(t *testing.T) {
	t.Parallel()

	ms := &mockSendFunc{}
	ps := &mockPendingStore{}
	rpc := newTestReverseRPCWithStore(ms, ps)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = rpc.ServerRequest(context.Background(), "user-1", "device-1", "ping", nil, 100*time.Millisecond)
	}()

	// Wait for the send to happen.
	require.True(t, ms.waitForCalls(1, time.Second))
	reqID := extractRequestID(t, ms.lastCall().pkg)

	// Dispatch a late response after the timeout fires.
	go func() {
		time.Sleep(200 * time.Millisecond)
		rpc.DispatchResponse(&protocol.PackageDataResponse{ID: reqID, Code: 0, Msg: "late"})
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("ServerRequest did not return")
	}
	// No panic under -race.
}

// TestServerRequest_Race_TimeoutVsCancelDevice exercises the race between
// timeout-triggered persistence and CancelDevice (D-105).
func TestServerRequest_Race_TimeoutVsCancelDevice(t *testing.T) {
	t.Parallel()

	ms := &mockSendFunc{}
	ps := &mockPendingStore{}
	rpc := newTestReverseRPCWithStore(ms, ps)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = rpc.ServerRequest(context.Background(), "user-1", "device-1", "ping", nil, 100*time.Millisecond)
	}()

	require.True(t, ms.waitForCalls(1, time.Second))

	// CancelDevice after the timeout fires but before/after persist.
	go func() {
		time.Sleep(200 * time.Millisecond)
		rpc.CancelDevice("user-1", "device-1")
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("ServerRequest did not return")
	}

	// Under -race: no panic, no data corruption.
}

// TestServerRequest_ConcurrentPersist verifies that 10 concurrent timeouts
// each persist exactly once with unique seq values (D-106).
func TestServerRequest_ConcurrentPersist(t *testing.T) {
	ms := &mockSendFunc{}
	ps := &mockPendingStore{}
	rpc := newTestReverseRPCWithStore(ms, ps)

	const n = 10
	var wg sync.WaitGroup

	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := rpc.ServerRequest(context.Background(), "user-1", "device-1", "ping", nil, 50*time.Millisecond)
			assert.ErrorIs(t, err, context.DeadlineExceeded)
		}()
	}

	wg.Wait()

	// Wait for all async persists.
	require.Eventually(t, func() bool {
		return ps.savedCount() == n
	}, 3*time.Second, 10*time.Millisecond, "all %d Saves should be called", n)

	saved := ps.allSaved()
	require.Len(t, saved, n)

	// All seqs should be unique.
	seqs := make(map[uint64]bool, n)
	for _, req := range saved {
		assert.False(t, seqs[req.Seq], "duplicate seq: %d", req.Seq)
		seqs[req.Seq] = true
	}
	assert.Len(t, seqs, n, "should have %d unique seqs", n)
}

// ---------------------------------------------------------------------------
// ReplayRequest tests (Phase 5, D-107, D-108)
// ---------------------------------------------------------------------------

// TestReverseRPC_ReplayRequest_Success verifies basic replay flow.
func TestReverseRPC_ReplayRequest_Success(t *testing.T) {
	ms := &mockSendFunc{}
	rpc := newTestReverseRPC(ms)

	preq := &PendingRequest{
		ID:             "s-orig-uuid",
		UserID:         "user-1",
		DeviceID:       "device-1",
		Method:         "test.method",
		Params:         json.RawMessage(`{"key":"value"}`),
		IdempotencyKey: "s-orig-uuid",
		Seq:            5,
		RetryCount:     0,
		MaxRetries:     3,
	}

	var result *protocol.PackageDataResponse
	var reqErr error
	done := make(chan struct{})

	go func() {
		defer close(done)
		result, reqErr = rpc.ReplayRequest(context.Background(), preq, 2*time.Second)
	}()

	require.True(t, ms.waitForCalls(1, time.Second), "sendFunc was not called")

	// Extract the replay request ID.
	call := ms.lastCall()
	var req protocol.PackageDataRequest
	require.NoError(t, json.Unmarshal(call.pkg.Data, &req))

	// Verify it has s-replay- prefix.
	assert.True(t, strings.HasPrefix(req.ID, "s-replay-"), "expected s-replay- prefix, got %q", req.ID)

	// Dispatch response using the replay ID.
	rpc.DispatchResponse(&protocol.PackageDataResponse{
		ID:   req.ID,
		Code: 0,
		Msg:  "replayed",
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ReplayRequest did not return")
	}

	require.NoError(t, reqErr)
	require.NotNil(t, result)
	assert.Equal(t, "replayed", result.Msg)
}

// TestReverseRPC_ReplayRequest_Timeout verifies timeout returns DeadlineExceeded.
func TestReverseRPC_ReplayRequest_Timeout(t *testing.T) {
	ms := &mockSendFunc{}
	rpc := newTestReverseRPC(ms)

	preq := &PendingRequest{
		ID:       "s-orig-1",
		UserID:   "user-1",
		DeviceID: "device-1",
		Method:   "ping",
	}

	_, err := rpc.ReplayRequest(context.Background(), preq, 100*time.Millisecond)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

// TestReverseRPC_ReplayRequest_SendFuncError verifies send error propagation.
func TestReverseRPC_ReplayRequest_SendFuncError(t *testing.T) {
	expectedErr := errors.New("send failed")
	ms := &mockSendFunc{err: expectedErr}
	rpc := newTestReverseRPC(ms)

	preq := &PendingRequest{
		ID:       "s-orig-2",
		UserID:   "user-1",
		DeviceID: "device-1",
		Method:   "ping",
	}

	_, err := rpc.ReplayRequest(context.Background(), preq, time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "send replay request")
	assert.ErrorIs(t, err, expectedErr)
}

// TestReverseRPC_ReplayRequest_NewReqID verifies unique s-replay- IDs.
func TestReverseRPC_ReplayRequest_NewReqID(t *testing.T) {
	ms := &mockSendFunc{}
	rpc := newTestReverseRPC(ms)

	preq := &PendingRequest{
		ID:       "s-orig-3",
		UserID:   "user-1",
		DeviceID: "device-1",
		Method:   "ping",
	}

	ids := make(map[string]bool)
	for range 5 {
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = rpc.ReplayRequest(context.Background(), preq, 2*time.Second)
		}()

		require.True(t, ms.waitForCalls(1, time.Second))
		call := ms.lastCall()
		var req protocol.PackageDataRequest
		require.NoError(t, json.Unmarshal(call.pkg.Data, &req))
		assert.True(t, strings.HasPrefix(req.ID, "s-replay-"))
		assert.False(t, ids[req.ID], "duplicate replay ID: %s", req.ID)
		ids[req.ID] = true

		// Clean up by dispatching response.
		rpc.DispatchResponse(&protocol.PackageDataResponse{ID: req.ID, Code: 0})
		<-done

		// Reset mock for next iteration.
		ms.mu.Lock()
		ms.calls = nil
		ms.mu.Unlock()
	}
}

// TestReverseRPC_ReplayRequest_PreservesIdempotencyKey verifies fields preserved.
func TestReverseRPC_ReplayRequest_PreservesIdempotencyKey(t *testing.T) {
	ms := &mockSendFunc{}
	rpc := newTestReverseRPC(ms)

	preq := &PendingRequest{
		ID:             "s-orig-4",
		UserID:         "user-1",
		DeviceID:       "device-1",
		Method:         "my.method",
		Params:         json.RawMessage(`{"hello":"world"}`),
		IdempotencyKey: "original-idempotency-key",
		Seq:            99,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = rpc.ReplayRequest(context.Background(), preq, 2*time.Second)
	}()

	require.True(t, ms.waitForCalls(1, time.Second))

	var req protocol.PackageDataRequest
	require.NoError(t, json.Unmarshal(ms.lastCall().pkg.Data, &req))

	assert.Equal(t, "my.method", req.Method)
	assert.Equal(t, "original-idempotency-key", req.IdempotencyKey)
	assert.Equal(t, uint64(99), req.Seq)
	assert.Equal(t, json.RawMessage(`{"hello":"world"}`), req.Params)
	assert.True(t, strings.HasPrefix(req.ID, "s-replay-"), "reqID should be new replay ID")
	assert.NotEqual(t, "s-orig-4", req.ID, "replay ID should differ from original")

	// Clean up.
	rpc.DispatchResponse(&protocol.PackageDataResponse{ID: req.ID, Code: 0})
	<-done
}

// TestReverseRPC_ReplayRequest_DispatchResponseRouting verifies correct routing.
func TestReverseRPC_ReplayRequest_DispatchResponseRouting(t *testing.T) {
	ms := &mockSendFunc{}
	rpc := newTestReverseRPC(ms)

	preq1 := &PendingRequest{ID: "orig-1", UserID: "user-1", DeviceID: "device-1", Method: "m1"}
	preq2 := &PendingRequest{ID: "orig-2", UserID: "user-1", DeviceID: "device-1", Method: "m2"}

	var result1, result2 *protocol.PackageDataResponse
	done1 := make(chan struct{})
	done2 := make(chan struct{})

	go func() {
		defer close(done1)
		result1, _ = rpc.ReplayRequest(context.Background(), preq1, 5*time.Second)
	}()
	go func() {
		defer close(done2)
		result2, _ = rpc.ReplayRequest(context.Background(), preq2, 5*time.Second)
	}()

	require.True(t, ms.waitForCalls(2, time.Second))

	// Extract both replay IDs.
	ms.mu.Lock()
	var id1, id2 string
	for _, call := range ms.calls {
		var req protocol.PackageDataRequest
		require.NoError(t, json.Unmarshal(call.pkg.Data, &req))
		var req2 protocol.PackageDataRequest
		require.NoError(t, json.Unmarshal(call.pkg.Data, &req2))
		if req.Method == "m1" {
			id1 = req.ID
		} else {
			id2 = req.ID
		}
	}
	ms.mu.Unlock()

	require.NotEmpty(t, id1)
	require.NotEmpty(t, id2)
	require.NotEqual(t, id1, id2)

	// Dispatch responses — routing should be correct.
	rpc.DispatchResponse(&protocol.PackageDataResponse{ID: id2, Code: 0, Msg: "resp-2"})
	rpc.DispatchResponse(&protocol.PackageDataResponse{ID: id1, Code: 0, Msg: "resp-1"})

	select {
	case <-done1:
	case <-time.After(2 * time.Second):
		t.Fatal("replay 1 did not return")
	}
	select {
	case <-done2:
	case <-time.After(2 * time.Second):
		t.Fatal("replay 2 did not return")
	}

	require.NotNil(t, result1)
	require.NotNil(t, result2)
	assert.Equal(t, "resp-1", result1.Msg)
	assert.Equal(t, "resp-2", result2.Msg)
}

// TestReverseRPC_ReplayRequest_CancelDevice verifies CancelDevice cancels replay.
func TestReverseRPC_ReplayRequest_CancelDevice(t *testing.T) {
	ms := &mockSendFunc{}
	rpc := newTestReverseRPC(ms)

	preq := &PendingRequest{
		ID:       "orig-cancel",
		UserID:   "user-1",
		DeviceID: "device-1",
		Method:   "ping",
	}

	var reqErr error
	var result *protocol.PackageDataResponse
	done := make(chan struct{})

	go func() {
		defer close(done)
		result, reqErr = rpc.ReplayRequest(context.Background(), preq, 5*time.Second)
	}()

	require.True(t, ms.waitForCalls(1, time.Second))

	rpc.CancelDevice("user-1", "device-1")

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ReplayRequest did not return after CancelDevice")
	}

	// CancelDevice delivers Code=-1 via respCh.
	if reqErr != nil {
		assert.ErrorIs(t, reqErr, context.Canceled)
	} else {
		require.NotNil(t, result)
		assert.Equal(t, protocol.ResponseCode(-1), result.Code)
	}
}

// TestReverseRPC_PendingStore_Accessor verifies the PendingStore() accessor.
func TestReverseRPC_PendingStore_Accessor(t *testing.T) {
	// With store.
	ps := &mockPendingStore{}
	ms := &mockSendFunc{}
	rpc := newTestReverseRPCWithStore(ms, ps)
	assert.NotNil(t, rpc.PendingStore())

	// Without store.
	rpc2 := newTestReverseRPC(ms)
	assert.Nil(t, rpc2.PendingStore())
}

// ---------------------------------------------------------------------------
// Phase 4: Device-offline persistence tests
// ---------------------------------------------------------------------------

// TestServerRequest_DeviceOffline_PersistsToPendingStore verifies that when
// sendFunc returns ErrDeviceOffline and a PendingStore is configured, the
// request is persisted for later replay.
func TestServerRequest_DeviceOffline_PersistsToPendingStore(t *testing.T) {
	ms := &mockSendFunc{err: ErrDeviceOffline}
	ps := &mockPendingStore{}
	rpc := newTestReverseRPCWithStore(ms, ps)

	params := json.RawMessage(`{"key":"value"}`)
	_, err := rpc.ServerRequest(context.Background(), "user-1", "device-1", "test.method", params, 2*time.Second)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDeviceOffline)
	assert.Contains(t, err.Error(), "persisted for replay")

	// persistAsync runs in a goroutine; wait for Save to be called.
	require.Eventually(t, func() bool {
		return ps.savedCount() == 1
	}, 2*time.Second, 5*time.Millisecond, "Save should be called after device offline")

	saved := ps.lastSaved()
	require.NotNil(t, saved)
	assert.Equal(t, "user-1", saved.UserID)
	assert.Equal(t, "device-1", saved.DeviceID)
	assert.Equal(t, "test.method", saved.Method)
	assert.Equal(t, params, saved.Params)
	assert.Equal(t, saved.ID, saved.IdempotencyKey)
	assert.True(t, saved.Seq > 0, "seq should be > 0, got %d", saved.Seq)
	assert.Equal(t, 0, saved.RetryCount)
	assert.Equal(t, defaultMaxReplayRetries, saved.MaxRetries)
	assert.False(t, saved.CreatedAt.IsZero(), "CreatedAt should be set")
}

// TestServerRequest_DeviceOffline_NoPendingStore_NoPersist verifies that when
// sendFunc returns ErrDeviceOffline but no PendingStore is configured, the
// error is returned without persistence (no panic).
func TestServerRequest_DeviceOffline_NoPendingStore_NoPersist(t *testing.T) {
	ms := &mockSendFunc{err: ErrDeviceOffline}
	rpc := newTestReverseRPC(ms) // no PendingStore

	_, err := rpc.ServerRequest(context.Background(), "user-1", "device-1", "ping", nil, 2*time.Second)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDeviceOffline)
	assert.NotContains(t, err.Error(), "persisted for replay")
}

// TestServerRequest_OtherSendError_NoPersist verifies that a non-offline
// sendFunc error does NOT trigger persistence.
func TestServerRequest_OtherSendError_NoPersist(t *testing.T) {
	ms := &mockSendFunc{err: errors.New("some other send error")}
	ps := &mockPendingStore{}
	rpc := newTestReverseRPCWithStore(ms, ps)

	_, err := rpc.ServerRequest(context.Background(), "user-1", "device-1", "ping", nil, 2*time.Second)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrDeviceOffline)
	assert.NotContains(t, err.Error(), "persisted for replay")

	// Small delay to ensure no spurious async persist.
	require.Never(t, func() bool {
		return ps.savedCount() > 0
	}, 200*time.Millisecond, 10*time.Millisecond, "should not persist for non-offline errors")
}
