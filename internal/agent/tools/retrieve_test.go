package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// TestToolResultStore_StoreAndRetrieve
// ---------------------------------------------------------------------------

func TestToolResultStore_StoreAndRetrieve(t *testing.T) {
	store := NewToolResultStore(100, 5*time.Second)
	content := strings.Repeat("a", 60000)

	truncated, rid := store.Store("", content)
	if rid == "" {
		t.Fatal("expected non-empty retrieval ID for large content")
	}
	if truncated == content {
		t.Error("expected content to be truncated")
	}

	retrieved, ok := store.Retrieve(rid)
	if !ok {
		t.Fatal("Retrieve returned false for stored ID")
	}
	if retrieved != content {
		t.Errorf("retrieved content length = %d, want %d", len(retrieved), len(content))
	}
}

// ---------------------------------------------------------------------------
// TestToolResultStore_SmallContentNotTruncated
// ---------------------------------------------------------------------------

func TestToolResultStore_SmallContentNotTruncated(t *testing.T) {
	store := NewToolResultStore(100, 5*time.Second)
	content := "small content"

	truncated, rid := store.Store("", content)
	if rid != "" {
		t.Errorf("expected empty retrieval ID for small content, got %q", rid)
	}
	if truncated != content {
		t.Errorf("expected content as-is, got %q", truncated)
	}
}

// ---------------------------------------------------------------------------
// TestToolResultStore_TTLExpiry
// ---------------------------------------------------------------------------

func TestToolResultStore_TTLExpiry(t *testing.T) {
	store := NewToolResultStore(100, 50*time.Millisecond)
	content := strings.Repeat("b", 60000)

	_, rid := store.Store("", content)
	if rid == "" {
		t.Fatal("expected non-empty retrieval ID")
	}

	time.Sleep(100 * time.Millisecond)

	_, ok := store.Retrieve(rid)
	if ok {
		t.Error("expected Retrieve to return false after TTL expiry")
	}
}

// ---------------------------------------------------------------------------
// TestToolResultStore_NotFound
// ---------------------------------------------------------------------------

func TestToolResultStore_NotFound(t *testing.T) {
	store := NewToolResultStore(100, 5*time.Second)
	_, ok := store.Retrieve("nonexistent-id")
	if ok {
		t.Error("expected false for non-existent ID")
	}
}

// ---------------------------------------------------------------------------
// TestToolResultStore_UTF8Safe
// ---------------------------------------------------------------------------

func TestToolResultStore_UTF8Safe(t *testing.T) {
	store := NewToolResultStore(100, 5*time.Second)
	// Each emoji is 4 bytes but 1 rune. 60000 runes of emoji.
	content := strings.Repeat("😀", 60000)
	if utf8.RuneCountInString(content) != 60000 {
		t.Fatalf("setup: expected 60000 runes, got %d", utf8.RuneCountInString(content))
	}

	truncated, rid := store.Store("", content)
	if rid == "" {
		t.Fatal("expected non-empty retrieval ID")
	}
	// truncated must be valid UTF-8.
	if !utf8.ValidString(truncated) {
		t.Error("truncated content is not valid UTF-8")
	}

	retrieved, ok := store.Retrieve(rid)
	if !ok {
		t.Fatal("Retrieve returned false")
	}
	if retrieved != content {
		t.Errorf("retrieved rune count = %d, want %d", utf8.RuneCountInString(retrieved), utf8.RuneCountInString(content))
	}
}

// ---------------------------------------------------------------------------
// TestToolResultStore_MaxSizeEviction
// ---------------------------------------------------------------------------

func TestToolResultStore_MaxSizeEviction(t *testing.T) {
	store := NewToolResultStore(3, 5*time.Second)
	content := strings.Repeat("c", 60000)

	// Fill 4 entries; maxSize is 3 so oldest should be evicted.
	var rids []string
	for range 4 {
		_, rid := store.Store("", content)
		rids = append(rids, rid)
	}

	// The oldest (rids[0]) should have been evicted.
	_, ok := store.Retrieve(rids[0])
	if ok {
		t.Error("expected oldest entry to be evicted")
	}

	// The latest should still be present.
	_, ok = store.Retrieve(rids[3])
	if !ok {
		t.Error("expected latest entry to be present")
	}

	if store.Len() != 3 {
		t.Errorf("Len() = %d, want 3", store.Len())
	}
}

// ---------------------------------------------------------------------------
// TestToolResultStore_Cleanup
// ---------------------------------------------------------------------------

func TestToolResultStore_Cleanup(t *testing.T) {
	store := NewToolResultStore(100, 50*time.Millisecond)
	content := strings.Repeat("d", 60000)

	store.Store("", content)
	store.Store("", content)

	if store.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", store.Len())
	}

	time.Sleep(100 * time.Millisecond)
	store.Cleanup()

	if store.Len() != 0 {
		t.Errorf("after Cleanup, Len() = %d, want 0", store.Len())
	}
}

// ---------------------------------------------------------------------------
// TestRetrieveTool_Invoke
// ---------------------------------------------------------------------------

func TestRetrieveTool_Invoke(t *testing.T) {
	store := NewToolResultStore(100, 5*time.Second)
	content := strings.Repeat("e", 60000)
	_, rid := store.Store("", content)

	tl, err := NewRetrieveTool(store)
	if err != nil {
		t.Fatalf("NewRetrieveTool: %v", err)
	}
	result, err := tl.InvokableRun(context.Background(), `{"result_id":"`+rid+`"}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if !strings.Contains(result, content) {
		t.Error("expected result to contain full content")
	}
}

// ---------------------------------------------------------------------------
// TestRetrieveTool_NotFound
// ---------------------------------------------------------------------------

func TestRetrieveTool_NotFound(t *testing.T) {
	store := NewToolResultStore(100, 5*time.Second)
	tl, err := NewRetrieveTool(store)
	if err != nil {
		t.Fatalf("NewRetrieveTool: %v", err)
	}
	_, err = tl.InvokableRun(context.Background(), `{"result_id":"nonexistent"}`)
	if err == nil {
		t.Fatal("expected error for non-existent ID, got nil")
	}
}

// ---------------------------------------------------------------------------
// TestToolResultStore_ConcurrentAccess
// ---------------------------------------------------------------------------

func TestToolResultStore_ConcurrentAccess(t *testing.T) {
	store := NewToolResultStore(100, time.Second)
	content := strings.Repeat("x", 60000)

	var wg sync.WaitGroup
	// Concurrent stores
	for i := range 20 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("concurrent-%d", i)
			store.Store(id, content)
		}(i)
	}
	// Concurrent retrieves
	for i := range 20 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			store.Retrieve(fmt.Sprintf("concurrent-%d", i))
		}(i)
	}
	// Concurrent cleanup
	wg.Add(1)
	go func() {
		defer wg.Done()
		store.Cleanup()
	}()

	wg.Wait()
}

// ---------------------------------------------------------------------------
// TestToolResultStore_ContentExactlyAtThreshold
// ---------------------------------------------------------------------------

func TestToolResultStore_ContentExactlyAtThreshold(t *testing.T) {
	store := NewToolResultStore(100, time.Minute)
	store.SetThreshold(10)
	content := strings.Repeat("a", 10) // exactly at threshold
	truncated, rid := store.Store("", content)
	if rid != "" {
		t.Error("expected empty retrieval ID for content at threshold")
	}
	if truncated != content {
		t.Errorf("expected content as-is, got truncated version")
	}
}

// ---------------------------------------------------------------------------
// TestRetrieveTool_EmptyResultID
// ---------------------------------------------------------------------------

func TestRetrieveTool_EmptyResultID(t *testing.T) {
	store := NewToolResultStore(100, time.Minute)
	tl, err := NewRetrieveTool(store)
	if err != nil {
		t.Fatalf("NewRetrieveTool: %v", err)
	}
	_, err = tl.InvokableRun(context.Background(), `{"result_id":""}`)
	if err == nil {
		t.Fatal("expected error for empty result_id, got nil")
	}
}

// ---------------------------------------------------------------------------
// TestNewRetrieveTool_NilStore_DefaultsToGlobal
// ---------------------------------------------------------------------------

func TestNewRetrieveTool_NilStore_DefaultsToGlobal(t *testing.T) {
	tl, err := NewRetrieveTool(nil)
	if err != nil {
		t.Fatalf("NewRetrieveTool(nil): %v", err)
	}
	if tl == nil {
		t.Fatal("expected non-nil tool")
	}
}
