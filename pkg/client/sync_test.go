package client

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/PineappleBond/xyncra-server/pkg/store"
	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

// ---------------------------------------------------------------------------
// ApplyUpdate tests
// ---------------------------------------------------------------------------

// TestApplyUpdate_SequenceHappyPath verifies that updates with consecutive
// sequence numbers (1, 2, 3) are all processed successfully.
func TestApplyUpdate_SequenceHappyPath(t *testing.T) {
	db := newTestStore(t)
	handler := &mockUpdateHandler{}
	logger := &testLogger{t: t}
	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		return nil, nil
	}
	sm := newSyncManager(db, handler, "test-user", rpcFn, 100, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	defer sm.Stop()

	ctx := context.Background()

	// Build three gap-type updates with sequential seq numbers.
	for i := uint32(1); i <= 3; i++ {
		update := newTestUpdate(i, protocol.UpdateTypeGap, json.RawMessage(`{}`))
		if err := sm.ApplyUpdate(ctx, &update); err != nil {
			t.Fatalf("ApplyUpdate seq=%d: unexpected error: %v", i, err)
		}
	}

	assertSyncState(t, db, "local_max_seq", 3)
}

// TestApplyUpdate_SequenceGap verifies that a gap in the sequence (1, 3)
// returns errSeqGap.
func TestApplyUpdate_SequenceGap(t *testing.T) {
	db := newTestStore(t)
	handler := &mockUpdateHandler{}
	logger := &testLogger{t: t}
	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		return nil, nil
	}
	sm := newSyncManager(db, handler, "test-user", rpcFn, 100, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	defer sm.Stop()

	ctx := context.Background()

	// Apply seq=1 successfully.
	update1 := newTestUpdate(1, protocol.UpdateTypeGap, json.RawMessage(`{}`))
	if err := sm.ApplyUpdate(ctx, &update1); err != nil {
		t.Fatalf("ApplyUpdate seq=1: %v", err)
	}

	// Apply seq=3 — should return errSeqGap.
	update3 := newTestUpdate(3, protocol.UpdateTypeGap, json.RawMessage(`{}`))
	err := sm.ApplyUpdate(ctx, &update3)
	if !errors.Is(err, errSeqGap) {
		t.Fatalf("expected errSeqGap, got: %v", err)
	}

	// local_max_seq should remain 1.
	assertSyncState(t, db, "local_max_seq", 1)
}

// TestApplyUpdate_SequenceDuplicate verifies that a duplicate update (seq=1
// twice) is silently skipped the second time.
func TestApplyUpdate_SequenceDuplicate(t *testing.T) {
	db := newTestStore(t)
	handler := &mockUpdateHandler{}
	logger := &testLogger{t: t}
	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		return nil, nil
	}
	sm := newSyncManager(db, handler, "test-user", rpcFn, 100, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	defer sm.Stop()

	ctx := context.Background()

	update := newTestUpdate(1, protocol.UpdateTypeGap, json.RawMessage(`{}`))

	// First application succeeds.
	if err := sm.ApplyUpdate(ctx, &update); err != nil {
		t.Fatalf("ApplyUpdate seq=1 (first): %v", err)
	}

	// Second application of same seq — should be skipped silently.
	if err := sm.ApplyUpdate(ctx, &update); err != nil {
		t.Fatalf("ApplyUpdate seq=1 (duplicate): %v", err)
	}

	assertSyncState(t, db, "local_max_seq", 1)
}

// TestApplyUpdate_TypeMessage verifies that a "message" update is persisted
// and the conversation's last-message pointer is updated.
func TestApplyUpdate_TypeMessage(t *testing.T) {
	db := newTestStore(t)
	handler := &mockUpdateHandler{}
	logger := &testLogger{t: t}
	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		return nil, nil
	}
	sm := newSyncManager(db, handler, "test-user", rpcFn, 100, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	defer sm.Stop()

	ctx := context.Background()

	// Pre-create the conversation so UpdateLastMessage does not fail.
	now := time.Now().Truncate(time.Second)
	conv := &model.Conversation{
		ID:        "conv-1",
		UserID1:   "test-user",
		UserID2:   "other-user",
		Type:      "1-on-1",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Conversations.Create(ctx, conv); err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	msg := model.Message{
		ID:              "msg-1",
		ClientMessageID: "cmid-1",
		ConversationID:  "conv-1",
		MessageID:       42,
		SenderID:        "other-user",
		Content:         "hello",
		Type:            "text",
		Status:          "sent",
		CreatedAt:       now,
	}
	payload, _ := json.Marshal(msg)
	update := newTestUpdate(1, protocol.UpdateTypeMessage, payload)

	if err := sm.ApplyUpdate(ctx, &update); err != nil {
		t.Fatalf("ApplyUpdate message: %v", err)
	}

	// Verify message was persisted.
	got, err := db.Messages.Get(ctx, "msg-1")
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if got.Content != "hello" {
		t.Errorf("message content: got=%q want=%q", got.Content, "hello")
	}

	// Verify conversation last-message pointer was updated.
	gotConv, err := db.Conversations.Get(ctx, "conv-1")
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if gotConv.LastProcessedMessageID != 42 {
		t.Errorf("LastProcessedMessageID: got=%d want=42", gotConv.LastProcessedMessageID)
	}

	// Verify handler was called.
	handler.mu.Lock()
	if len(handler.messages) != 1 {
		t.Errorf("handler messages count: got=%d want=1", len(handler.messages))
	}
	handler.mu.Unlock()

	assertSyncState(t, db, "local_max_seq", 1)
}

// TestApplyUpdate_TypeDeleteMessage verifies that a "delete_message" update
// triggers a soft delete of the local message.
func TestApplyUpdate_TypeDeleteMessage(t *testing.T) {
	db := newTestStore(t)
	handler := &mockUpdateHandler{}
	logger := &testLogger{t: t}
	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		return nil, nil
	}
	sm := newSyncManager(db, handler, "test-user", rpcFn, 100, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	defer sm.Stop()

	ctx := context.Background()

	// Pre-create the message to be deleted.
	now := time.Now().Truncate(time.Second)
	msg := &model.Message{
		ID:              "msg-del-1",
		ClientMessageID: "cmid-del-1",
		ConversationID:  "conv-1",
		MessageID:       10,
		SenderID:        "test-user",
		Content:         "to be deleted",
		Type:            "text",
		Status:          "sent",
		CreatedAt:       now,
	}
	if err := db.Messages.Create(ctx, msg); err != nil {
		t.Fatalf("create message: %v", err)
	}

	dp := deleteMessagePayload{
		MessageID:      "msg-del-1",
		ConversationID: "conv-1",
	}
	payload, _ := json.Marshal(dp)
	update := newTestUpdate(1, protocol.UpdateTypeDeleteMessage, payload)

	if err := sm.ApplyUpdate(ctx, &update); err != nil {
		t.Fatalf("ApplyUpdate delete_message: %v", err)
	}

	// Verify message was soft-deleted (Get returns ErrNotFound for soft-deleted).
	_, err := db.Messages.Get(ctx, "msg-del-1")
	if err == nil {
		t.Error("expected message to be soft-deleted, but Get succeeded")
	}

	// Verify handler was called.
	handler.mu.Lock()
	if len(handler.deletes) != 1 {
		t.Errorf("handler deletes count: got=%d want=1", len(handler.deletes))
	} else if handler.deletes[0].MessageID != "msg-del-1" {
		t.Errorf("handler delete MessageID: got=%q want=%q", handler.deletes[0].MessageID, "msg-del-1")
	}
	handler.mu.Unlock()

	assertSyncState(t, db, "local_max_seq", 1)
}

// TestApplyUpdate_TypeMarkRead verifies that a "mark_read" update advances the
// conversation read cursor for the current user.
func TestApplyUpdate_TypeMarkRead(t *testing.T) {
	db := newTestStore(t)
	handler := &mockUpdateHandler{}
	logger := &testLogger{t: t}
	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		return nil, nil
	}
	sm := newSyncManager(db, handler, "test-user", rpcFn, 100, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	defer sm.Stop()

	ctx := context.Background()

	// Pre-create the conversation.
	now := time.Now().Truncate(time.Second)
	conv := &model.Conversation{
		ID:        "conv-mr",
		UserID1:   "test-user",
		UserID2:   "other-user",
		Type:      "1-on-1",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Conversations.Create(ctx, conv); err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	mp := markReadPayload{
		ConversationID:    "conv-mr",
		LastReadMessageID: 99,
	}
	payload, _ := json.Marshal(mp)
	update := newTestUpdate(1, protocol.UpdateTypeMarkRead, payload)

	if err := sm.ApplyUpdate(ctx, &update); err != nil {
		t.Fatalf("ApplyUpdate mark_read: %v", err)
	}

	// Verify conversation read cursor was advanced.
	gotConv, err := db.Conversations.Get(ctx, "conv-mr")
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	// test-user is UserID1, so LastReadMessageID1 should be updated.
	if gotConv.LastReadMessageID1 != 99 {
		t.Errorf("LastReadMessageID1: got=%d want=99", gotConv.LastReadMessageID1)
	}

	// Verify handler was called.
	handler.mu.Lock()
	if len(handler.markReads) != 1 {
		t.Errorf("handler markReads count: got=%d want=1", len(handler.markReads))
	}
	handler.mu.Unlock()

	assertSyncState(t, db, "local_max_seq", 1)
}

// TestApplyUpdate_TypeConversation verifies that a "conversation" update
// creates or updates the local conversation record.
func TestApplyUpdate_TypeConversation(t *testing.T) {
	db := newTestStore(t)
	handler := &mockUpdateHandler{}
	logger := &testLogger{t: t}
	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		return nil, nil
	}
	sm := newSyncManager(db, handler, "test-user", rpcFn, 100, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	defer sm.Stop()

	ctx := context.Background()

	conv := model.Conversation{
		ID:        "conv-new",
		UserID1:   "test-user",
		UserID2:   "other-user",
		Type:      "1-on-1",
		Title:     "Test Chat",
		CreatedAt: time.Now().Truncate(time.Second),
		UpdatedAt: time.Now().Truncate(time.Second),
	}
	payload, _ := json.Marshal(conv)
	update := newTestUpdate(1, protocol.UpdateTypeConversation, payload)

	if err := sm.ApplyUpdate(ctx, &update); err != nil {
		t.Fatalf("ApplyUpdate conversation: %v", err)
	}

	// Verify conversation was created.
	got, err := db.Conversations.Get(ctx, "conv-new")
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if got.Title != "Test Chat" {
		t.Errorf("conversation title: got=%q want=%q", got.Title, "Test Chat")
	}

	// Verify handler was called.
	handler.mu.Lock()
	if len(handler.conversations) != 1 {
		t.Errorf("handler conversations count: got=%d want=1", len(handler.conversations))
	}
	handler.mu.Unlock()

	assertSyncState(t, db, "local_max_seq", 1)
}

// TestApplyUpdate_TypeGap verifies that a "gap" type update only advances
// the sequence number without writing any data.
func TestApplyUpdate_TypeGap(t *testing.T) {
	db := newTestStore(t)
	handler := &mockUpdateHandler{}
	logger := &testLogger{t: t}
	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		return nil, nil
	}
	sm := newSyncManager(db, handler, "test-user", rpcFn, 100, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	defer sm.Stop()

	ctx := context.Background()

	update := newTestUpdate(1, protocol.UpdateTypeGap, json.RawMessage(`{}`))
	if err := sm.ApplyUpdate(ctx, &update); err != nil {
		t.Fatalf("ApplyUpdate gap: %v", err)
	}

	assertSyncState(t, db, "local_max_seq", 1)

	// Verify handler OnGap was called.
	handler.mu.Lock()
	if len(handler.gaps) != 1 || handler.gaps[0] != 1 {
		t.Errorf("handler gaps: got=%v want=[1]", handler.gaps)
	}
	handler.mu.Unlock()
}

// TestApplyUpdate_NotificationLogDedup verifies that the notification log
// deduplication prevents duplicate processing even when the same seq is
// submitted twice with different types.
func TestApplyUpdate_NotificationLogDedup(t *testing.T) {
	db := newTestStore(t)
	handler := &mockUpdateHandler{}
	logger := &testLogger{t: t}
	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		return nil, nil
	}
	sm := newSyncManager(db, handler, "test-user", rpcFn, 100, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	defer sm.Stop()

	ctx := context.Background()

	// First update — seq=1, type=gap.
	update1 := newTestUpdate(1, protocol.UpdateTypeGap, json.RawMessage(`{}`))
	if err := sm.ApplyUpdate(ctx, &update1); err != nil {
		t.Fatalf("ApplyUpdate seq=1 (first): %v", err)
	}

	// Second update — same seq=1, type=gap. The NotificationLog uniqueIndex
	// on Seq should cause a duplicate key error, which is handled gracefully.
	update2 := newTestUpdate(1, protocol.UpdateTypeGap, json.RawMessage(`{}`))
	if err := sm.ApplyUpdate(ctx, &update2); err != nil {
		t.Fatalf("ApplyUpdate seq=1 (dedup): unexpected error: %v", err)
	}

	// Handler should have been called only once.
	handler.mu.Lock()
	if len(handler.gaps) != 1 {
		t.Errorf("handler gaps count: got=%d want=1 (dedup failed)", len(handler.gaps))
	}
	handler.mu.Unlock()

	assertSyncState(t, db, "local_max_seq", 1)
}

// ---------------------------------------------------------------------------
// ApplyUpdates tests
// ---------------------------------------------------------------------------

// TestApplyUpdates_AllApplied verifies that a batch of updates is applied
// entirely when there are no gaps.
func TestApplyUpdates_AllApplied(t *testing.T) {
	db := newTestStore(t)
	handler := &mockUpdateHandler{}
	logger := &testLogger{t: t}
	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		return nil, nil
	}
	sm := newSyncManager(db, handler, "test-user", rpcFn, 100, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	defer sm.Stop()

	ctx := context.Background()

	updates := []protocol.PackageDataUpdate{
		newTestUpdate(1, protocol.UpdateTypeGap, json.RawMessage(`{}`)),
		newTestUpdate(2, protocol.UpdateTypeGap, json.RawMessage(`{}`)),
		newTestUpdate(3, protocol.UpdateTypeGap, json.RawMessage(`{}`)),
	}

	if err := sm.ApplyUpdates(ctx, updates); err != nil {
		t.Fatalf("ApplyUpdates: %v", err)
	}

	assertSyncState(t, db, "local_max_seq", 3)
}

// TestApplyUpdates_StopOnGap verifies that processing stops at the first gap
// and the remaining updates are not applied.
func TestApplyUpdates_StopOnGap(t *testing.T) {
	db := newTestStore(t)
	handler := &mockUpdateHandler{}
	logger := &testLogger{t: t}
	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		return nil, nil
	}
	sm := newSyncManager(db, handler, "test-user", rpcFn, 100, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	defer sm.Stop()

	ctx := context.Background()

	updates := []protocol.PackageDataUpdate{
		newTestUpdate(1, protocol.UpdateTypeGap, json.RawMessage(`{}`)),
		newTestUpdate(3, protocol.UpdateTypeGap, json.RawMessage(`{}`)), // gap
		newTestUpdate(4, protocol.UpdateTypeGap, json.RawMessage(`{}`)), // should not be processed
	}

	err := sm.ApplyUpdates(ctx, updates)
	if !errors.Is(err, errSeqGap) {
		t.Fatalf("expected errSeqGap, got: %v", err)
	}

	// Only seq=1 should have been applied.
	assertSyncState(t, db, "local_max_seq", 1)
}

// ---------------------------------------------------------------------------
// Debounced pull tests
// ---------------------------------------------------------------------------

// TestDebouncedPull_SingleTrigger verifies that a single scheduleDebouncedPull
// triggers exactly one RPC call after the debounce window.
func TestDebouncedPull_SingleTrigger(t *testing.T) {
	db := newTestStore(t)
	handler := &mockUpdateHandler{}
	logger := &testLogger{t: t}

	var rpcCalls int32
	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		atomic.AddInt32(&rpcCalls, 1)
		resp := syncUpdatesResponse{
			Updates:   []protocol.PackageDataUpdate{},
			HasMore:   false,
			LatestSeq: 0,
		}
		data, _ := json.Marshal(resp)
		return data, nil
	}
	sm := newSyncManager(db, handler, "test-user", rpcFn, 100, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	defer sm.Stop()

	sm.scheduleDebouncedPull()

	// Wait for debounce + processing.
	time.Sleep(200 * time.Millisecond)

	if got := atomic.LoadInt32(&rpcCalls); got != 1 {
		t.Errorf("rpc calls: got=%d want=1", got)
	}
}

// TestDebouncedPull_MergedTriggers verifies that multiple rapid
// scheduleDebouncedPull calls are coalesced into a single RPC call.
func TestDebouncedPull_MergedTriggers(t *testing.T) {
	db := newTestStore(t)
	handler := &mockUpdateHandler{}
	logger := &testLogger{t: t}

	var rpcCalls int32
	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		atomic.AddInt32(&rpcCalls, 1)
		resp := syncUpdatesResponse{
			Updates:   []protocol.PackageDataUpdate{},
			HasMore:   false,
			LatestSeq: 0,
		}
		data, _ := json.Marshal(resp)
		return data, nil
	}
	sm := newSyncManager(db, handler, "test-user", rpcFn, 100, 100*time.Millisecond, logger)
	sm.Start(context.Background())
	defer sm.Stop()

	// Fire 5 triggers in rapid succession.
	for i := 0; i < 5; i++ {
		sm.scheduleDebouncedPull()
	}

	// Wait for debounce + processing.
	time.Sleep(300 * time.Millisecond)

	if got := atomic.LoadInt32(&rpcCalls); got != 1 {
		t.Errorf("rpc calls: got=%d want=1 (coalescing failed)", got)
	}
}

// TestDebouncedPull_HasMore verifies that when the server returns has_more=true,
// a chained pull is scheduled automatically.
func TestDebouncedPull_HasMore(t *testing.T) {
	db := newTestStore(t)
	handler := &mockUpdateHandler{}
	logger := &testLogger{t: t}

	var mu sync.Mutex
	callCount := 0
	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		mu.Lock()
		callCount++
		c := callCount
		mu.Unlock()

		resp := syncUpdatesResponse{
			Updates:   []protocol.PackageDataUpdate{},
			HasMore:   c < 3, // first two calls return has_more=true
			LatestSeq: uint32(c),
		}
		data, _ := json.Marshal(resp)
		return data, nil
	}
	sm := newSyncManager(db, handler, "test-user", rpcFn, 100, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	defer sm.Stop()

	sm.scheduleDebouncedPull()

	// Wait for chained pulls.
	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	got := callCount
	mu.Unlock()
	if got < 3 {
		t.Errorf("rpc calls: got=%d want>=3 (has_more chaining failed)", got)
	}
}

// ---------------------------------------------------------------------------
// FullSync tests
// ---------------------------------------------------------------------------

// TestFullSync_PaginationLoop verifies that FullSync paginates through
// multiple pages of updates until has_more becomes false.
func TestFullSync_PaginationLoop(t *testing.T) {
	db := newTestStore(t)
	handler := &mockUpdateHandler{}
	logger := &testLogger{t: t}

	var mu sync.Mutex
	callCount := 0
	batchSize := 100

	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		mu.Lock()
		callCount++
		c := callCount
		mu.Unlock()

		// Simulate 250 updates total → 100 + 100 + 50.
		var updates []protocol.PackageDataUpdate
		baseSeq := uint32((c - 1) * batchSize)
		count := batchSize
		if c == 3 {
			count = 50
		}
		for i := 0; i < count; i++ {
			seq := baseSeq + uint32(i) + 1
			u := newTestUpdate(seq, protocol.UpdateTypeGap, json.RawMessage(`{}`))
			updates = append(updates, u)
		}

		resp := syncUpdatesResponse{
			Updates:   updates,
			HasMore:   c < 3,
			LatestSeq: baseSeq + uint32(count),
		}
		data, _ := json.Marshal(resp)
		return data, nil
	}
	sm := newSyncManager(db, handler, "test-user", rpcFn, batchSize, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	defer sm.Stop()

	ctx := context.Background()
	if err := sm.FullSync(ctx); err != nil {
		t.Fatalf("FullSync: %v", err)
	}

	// All 250 updates should have been processed.
	assertSyncState(t, db, "local_max_seq", 250)
}

// TestFullSync_FromLocalMaxSeq verifies that FullSync starts from the
// existing localMaxSeq, not from 0.
func TestFullSync_FromLocalMaxSeq(t *testing.T) {
	db := newTestStore(t)
	handler := &mockUpdateHandler{}
	logger := &testLogger{t: t}

	ctx := context.Background()

	// Pre-set localMaxSeq to 50.
	if err := db.SyncStates.SetLocalMaxSeq(ctx, 50); err != nil {
		t.Fatalf("set local max seq: %v", err)
	}

	var mu sync.Mutex
	var gotAfterSeq uint32
	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		p := params.(map[string]any)
		mu.Lock()
		gotAfterSeq = p["after_seq"].(uint32)
		mu.Unlock()

		resp := syncUpdatesResponse{
			Updates:   []protocol.PackageDataUpdate{},
			HasMore:   false,
			LatestSeq: 50,
		}
		data, _ := json.Marshal(resp)
		return data, nil
	}
	sm := newSyncManager(db, handler, "test-user", rpcFn, 100, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	defer sm.Stop()

	if err := sm.FullSync(ctx); err != nil {
		t.Fatalf("FullSync: %v", err)
	}

	mu.Lock()
	if gotAfterSeq != 50 {
		t.Errorf("after_seq in rpc call: got=%d want=50", gotAfterSeq)
	}
	mu.Unlock()
}

// TestFullSync_EmptyServer verifies that FullSync handles the case where the
// server has no updates to return.
func TestFullSync_EmptyServer(t *testing.T) {
	db := newTestStore(t)
	handler := &mockUpdateHandler{}
	logger := &testLogger{t: t}

	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		resp := syncUpdatesResponse{
			Updates:   []protocol.PackageDataUpdate{},
			HasMore:   false,
			LatestSeq: 0,
		}
		data, _ := json.Marshal(resp)
		return data, nil
	}
	sm := newSyncManager(db, handler, "test-user", rpcFn, 100, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	defer sm.Stop()

	ctx := context.Background()
	if err := sm.FullSync(ctx); err != nil {
		t.Fatalf("FullSync: %v", err)
	}

	assertSyncState(t, db, "local_max_seq", 0)
}

// TestFullSync_ContextCancelled verifies that FullSync returns immediately
// when the context is cancelled.
func TestFullSync_ContextCancelled(t *testing.T) {
	db := newTestStore(t)
	handler := &mockUpdateHandler{}
	logger := &testLogger{t: t}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		resp := syncUpdatesResponse{
			Updates:   []protocol.PackageDataUpdate{},
			HasMore:   true, // Would cause infinite loop without context check.
			LatestSeq: 1,
		}
		data, _ := json.Marshal(resp)
		return data, nil
	}
	sm := newSyncManager(db, handler, "test-user", rpcFn, 100, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	defer sm.Stop()

	err := sm.FullSync(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

// ---------------------------------------------------------------------------
// scheduleDebouncedPull test
// ---------------------------------------------------------------------------

// TestScheduleDebouncedPull verifies that the pullPending flag prevents
// multiple timers from being armed simultaneously.
func TestScheduleDebouncedPull(t *testing.T) {
	db := newTestStore(t)
	handler := &mockUpdateHandler{}
	logger := &testLogger{t: t}
	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		return nil, nil
	}
	sm := newSyncManager(db, handler, "test-user", rpcFn, 100, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	defer sm.Stop()

	// First schedule should arm the timer.
	sm.scheduleDebouncedPull()

	sm.mu.Lock()
	if !sm.pullPending {
		t.Error("pullPending should be true after first schedule")
	}
	firstTimer := sm.pullTimer
	sm.mu.Unlock()

	// Second schedule should be coalesced (same timer).
	sm.scheduleDebouncedPull()

	sm.mu.Lock()
	if sm.pullTimer != firstTimer {
		t.Error("pullTimer should not change on coalesced schedule")
	}
	sm.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Bug 1 verification: handleConversation payload mismatch (D-013, D-015)
// ---------------------------------------------------------------------------

// seedConversation inserts a conversation and a message into the local DB so
// that delete/restore sync events have data to operate on.
func seedConversation(t *testing.T, db *store.ClientDB, convID string) {
	t.Helper()
	ctx := context.Background()
	conv := &model.Conversation{
		ID:      convID,
		UserID1: "test-user",
		UserID2: "peer-user",
		Type:    "1-on-1",
		Title:   "test conversation",
	}
	if err := db.Conversations.Create(ctx, conv); err != nil {
		t.Fatalf("seed conversation create: %v", err)
	}
	msg := &model.Message{
		ID:             "msg-1",
		ConversationID: convID,
		SenderID:       "peer-user",
		Content:        "hello",
		MessageID:      1,
	}
	if err := db.Messages.Create(ctx, msg); err != nil {
		t.Fatalf("seed message create: %v", err)
	}
}

// TestApplyUpdate_ConversationDelete verifies that a "delete" conversation
// update soft-deletes the local conversation and its messages rather than
// creating a ghost record with an empty ID.
func TestApplyUpdate_ConversationDelete(t *testing.T) {
	db := newTestStore(t)
	handler := &mockUpdateHandler{}
	logger := &testLogger{t: t}
	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		return nil, nil
	}
	sm := newSyncManager(db, handler, "test-user", rpcFn, 100, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	defer sm.Stop()

	ctx := context.Background()
	convID := "conv-abc-123"
	seedConversation(t, db, convID)

	// The server sends delete_conversation updates with the shape:
	// {"conversation_id": "...", "action": "delete"}
	payload := json.RawMessage(`{"conversation_id":"` + convID + `","action":"delete"}`)
	update := newTestUpdate(1, protocol.UpdateTypeConversation, payload)
	if err := sm.ApplyUpdate(ctx, &update); err != nil {
		t.Fatalf("ApplyUpdate conversation delete: %v", err)
	}

	// Conversation must be soft-deleted, not replaced by a ghost record.
	_, err := db.Conversations.Get(ctx, convID)
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("conversation Get: expected ErrNotFound after delete, got: %v", err)
	}

	// There must be no conversation with an empty ID.
	_, err = db.Conversations.Get(ctx, "")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("ghost conversation with empty ID should not exist, got: %v", err)
	}

	// Messages in the conversation must be soft-deleted.
	_, err = db.Messages.Get(ctx, "msg-1")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("message Get: expected ErrNotFound after cascade delete, got: %v", err)
	}
}

// TestApplyUpdate_ConversationRestore verifies that a "restore" conversation
// update restores a soft-deleted conversation and its messages.
func TestApplyUpdate_ConversationRestore(t *testing.T) {
	db := newTestStore(t)
	handler := &mockUpdateHandler{}
	logger := &testLogger{t: t}
	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		return nil, nil
	}
	sm := newSyncManager(db, handler, "test-user", rpcFn, 100, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	defer sm.Stop()

	ctx := context.Background()
	convID := "conv-abc-123"
	seedConversation(t, db, convID)

	// Delete first so we can restore.
	if err := db.Conversations.Delete(ctx, convID); err != nil {
		t.Fatalf("seed delete: %v", err)
	}

	payload := json.RawMessage(`{"conversation_id":"` + convID + `","action":"restore"}`)
	update := newTestUpdate(1, protocol.UpdateTypeConversation, payload)
	if err := sm.ApplyUpdate(ctx, &update); err != nil {
		t.Fatalf("ApplyUpdate conversation restore: %v", err)
	}

	// Conversation must be visible again.
	conv, err := db.Conversations.Get(ctx, convID)
	if err != nil {
		t.Fatalf("conversation Get after restore: %v", err)
	}
	if conv.ID != convID {
		t.Errorf("restored conversation ID: got=%q want=%q", conv.ID, convID)
	}

	// Messages must be restored too.
	msg, err := db.Messages.Get(ctx, "msg-1")
	if err != nil {
		t.Fatalf("message Get after restore: %v", err)
	}
	if msg.Content != "hello" {
		t.Errorf("restored message content: got=%q want=%q", msg.Content, "hello")
	}
}

// ---------------------------------------------------------------------------
// Bug #4: JSON deserialization validation and handleMarkRead UserID2
// ---------------------------------------------------------------------------

// TestApplyUpdate_InvalidJSONPayload verifies that an invalid JSON payload
// returns a SyncError rather than panicking.
func TestApplyUpdate_InvalidJSONPayload(t *testing.T) {
	db := newTestStore(t)
	handler := &mockUpdateHandler{}
	logger := &testLogger{t: t}
	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		return nil, nil
	}
	sm := newSyncManager(db, handler, "test-user", rpcFn, 100, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	defer sm.Stop()

	ctx := context.Background()

	// Send a message update with invalid JSON payload.
	update := newTestUpdate(1, protocol.UpdateTypeMessage, json.RawMessage(`{invalid json`))
	err := sm.ApplyUpdate(ctx, &update)
	if err == nil {
		t.Fatal("expected error for invalid JSON payload, got nil")
	}
}

// TestApplyUpdate_TypeMarkRead_UserID2 verifies that handleMarkRead updates
// the correct column (LastReadMessageID2) when the current user is UserID2.
func TestApplyUpdate_TypeMarkRead_UserID2(t *testing.T) {
	db := newTestStore(t)
	handler := &mockUpdateHandler{}
	logger := &testLogger{t: t}
	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		return nil, nil
	}
	// The current user is "user-b", who is UserID2 in the conversation.
	sm := newSyncManager(db, handler, "user-b", rpcFn, 100, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	defer sm.Stop()

	ctx := context.Background()

	// Pre-create the conversation with user-a as UserID1, user-b as UserID2.
	now := time.Now().Truncate(time.Second)
	conv := &model.Conversation{
		ID:        "conv-mr-u2",
		UserID1:   "user-a",
		UserID2:   "user-b",
		Type:      "1-on-1",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Conversations.Create(ctx, conv); err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	mp := markReadPayload{
		ConversationID:    "conv-mr-u2",
		LastReadMessageID: 77,
	}
	payload, _ := json.Marshal(mp)
	update := newTestUpdate(1, protocol.UpdateTypeMarkRead, payload)

	if err := sm.ApplyUpdate(ctx, &update); err != nil {
		t.Fatalf("ApplyUpdate mark_read: %v", err)
	}

	// Verify that LastReadMessageID2 (not ID1) was updated for user-b.
	gotConv, err := db.Conversations.Get(ctx, "conv-mr-u2")
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if gotConv.LastReadMessageID1 != 0 {
		t.Errorf("LastReadMessageID1 should remain 0 (user-a not affected), got=%d", gotConv.LastReadMessageID1)
	}
	if gotConv.LastReadMessageID2 != 77 {
		t.Errorf("LastReadMessageID2: got=%d want=77", gotConv.LastReadMessageID2)
	}

	// Verify handler was called.
	handler.mu.Lock()
	if len(handler.markReads) != 1 {
		t.Errorf("handler markReads count: got=%d want=1", len(handler.markReads))
	} else if handler.markReads[0].MessageID != 77 {
		t.Errorf("handler markRead MessageID: got=%d want=77", handler.markReads[0].MessageID)
	}
	handler.mu.Unlock()
}

// TestApplyUpdate_TypeConversationCreate verifies that a "conversation" type
// update with action="create" upserts the conversation into the local store.
func TestApplyUpdate_TypeConversationCreate(t *testing.T) {
	db := newTestStore(t)
	handler := &mockUpdateHandler{}
	logger := &testLogger{t: t}
	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		return nil, nil
	}
	sm := newSyncManager(db, handler, "test-user", rpcFn, 100, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	defer sm.Stop()

	ctx := context.Background()

	conv := model.Conversation{
		ID:        "conv-create-action",
		UserID1:   "test-user",
		UserID2:   "other-user",
		Type:      "1-on-1",
		Title:     "Created via action",
		CreatedAt: time.Now().Truncate(time.Second),
		UpdatedAt: time.Now().Truncate(time.Second),
	}
	convPayload, _ := json.Marshal(conv)
	// handleConversationCreate unmarshals the raw payload as model.Conversation.
	update := newTestUpdate(1, protocol.UpdateTypeConversation, convPayload)

	if err := sm.ApplyUpdate(ctx, &update); err != nil {
		t.Fatalf("ApplyUpdate conversation create: %v", err)
	}

	// Verify conversation was created via Upsert.
	got, err := db.Conversations.Get(ctx, "conv-create-action")
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if got.Title != "Created via action" {
		t.Errorf("conversation title: got=%q want=%q", got.Title, "Created via action")
	}

	// Verify handler was called.
	handler.mu.Lock()
	if len(handler.conversations) != 1 {
		t.Errorf("handler conversations count: got=%d want=1", len(handler.conversations))
	}
	handler.mu.Unlock()
}

// TestApplyUpdate_MessageDuplicateIdempotent verifies that receiving the same
// message update twice does not create duplicate records.
func TestApplyUpdate_MessageDuplicateIdempotent(t *testing.T) {
	db := newTestStore(t)
	handler := &mockUpdateHandler{}
	logger := &testLogger{t: t}
	rpcFn := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		return nil, nil
	}
	sm := newSyncManager(db, handler, "test-user", rpcFn, 100, 50*time.Millisecond, logger)
	sm.Start(context.Background())
	defer sm.Stop()

	ctx := context.Background()

	// Pre-create conversation.
	now := time.Now().Truncate(time.Second)
	conv := &model.Conversation{
		ID:        "conv-msg-dup",
		UserID1:   "test-user",
		UserID2:   "other-user",
		Type:      "1-on-1",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Conversations.Create(ctx, conv); err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	msg := model.Message{
		ID:              "msg-dup-1",
		ClientMessageID: "cmid-dup-1",
		ConversationID:  "conv-msg-dup",
		MessageID:       1,
		SenderID:        "other-user",
		Content:         "duplicate test",
		Type:            "text",
		Status:          "sent",
		CreatedAt:       now,
	}
	payload, _ := json.Marshal(msg)
	update := newTestUpdate(1, protocol.UpdateTypeMessage, payload)

	// Apply first time.
	if err := sm.ApplyUpdate(ctx, &update); err != nil {
		t.Fatalf("ApplyUpdate message (first): %v", err)
	}

	// Apply second time with same seq — should be skipped (dedup).
	update2 := newTestUpdate(1, protocol.UpdateTypeMessage, payload)
	if err := sm.ApplyUpdate(ctx, &update2); err != nil {
		t.Fatalf("ApplyUpdate message (dedup): %v", err)
	}

	// Only one message should exist.
	got, err := db.Messages.Get(ctx, "msg-dup-1")
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if got.Content != "duplicate test" {
		t.Errorf("message content: got=%q want=%q", got.Content, "duplicate test")
	}
}
