package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// Default truncation and TTL settings for the ToolResultStore (D-080).
const (
	DefaultTruncationThreshold = 50000 // characters (runes)
	DefaultTTL                 = 1 * time.Hour
	DefaultMaxSize             = 10000 // max entries before oldest eviction
)

// storedResult holds a full tool result with its creation timestamp.
type storedResult struct {
	content   string
	createdAt time.Time
}

// ToolResultStore stores truncated tool results in memory with TTL (D-080).
// It is safe for concurrent use.
type ToolResultStore struct {
	mu        sync.RWMutex
	results   map[string]storedResult
	maxSize   int
	ttl       time.Duration
	threshold int
}

// NewToolResultStore creates a ToolResultStore.
//
//   - maxSize: maximum number of stored entries; oldest are evicted when full
//   - ttl: time-to-live for each entry
//   - threshold: content length (in runes) below which content is returned
//     as-is from Store without generating a retrieval ID
func NewToolResultStore(maxSize int, ttl time.Duration) *ToolResultStore {
	if maxSize <= 0 {
		maxSize = DefaultMaxSize
	}
	return &ToolResultStore{
		results:   make(map[string]storedResult),
		maxSize:   maxSize,
		ttl:       ttl,
		threshold: DefaultTruncationThreshold,
	}
}

// SetThreshold overrides the truncation threshold (in runes).
func (s *ToolResultStore) SetThreshold(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.threshold = n
}

// Store saves content and returns a truncated version plus a retrieval ID.
//
// The id parameter is an optional caller-supplied hint for the key; if a
// collision occurs a random suffix is appended. The returned truncated string
// contains the first threshold runes of content. If the full content fits
// within the threshold, it is returned as-is with an empty retrievalID.
func (s *ToolResultStore) Store(id, content string) (truncated string, retrievalID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// If content is within threshold, return as-is.
	runeCount := utf8.RuneCountInString(content)
	if runeCount <= s.threshold {
		return content, ""
	}

	// Generate a retrieval ID.
	rid := id
	if rid == "" {
		rid = generateID()
	}
	// Ensure uniqueness.
	for {
		if _, exists := s.results[rid]; !exists {
			break
		}
		rid = rid + "-" + generateID()
	}

	// Truncate using rune-based slicing for UTF-8 safety.
	truncated = truncateRunes(content, s.threshold)

	// Store the full content.
	s.results[rid] = storedResult{
		content:   content,
		createdAt: time.Now(),
	}

	// Evict oldest if over capacity.
	if len(s.results) > s.maxSize {
		s.evictOldest()
	}

	// Build truncated marker.
	truncatedMsg := fmt.Sprintf(
		"%s\n\n[result truncated: %d/%d runes shown, retrieval_id=%q — use retrieve_tool_result to get full content]",
		truncated,
		runeCount,
		runeCount,
		rid,
	)

	return truncatedMsg, rid
}

// Retrieve returns the full content for the given retrieval ID.
// Returns ("", false) if the ID is not found or has expired.
func (s *ToolResultStore) Retrieve(id string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	r, ok := s.results[id]
	if !ok {
		return "", false
	}
	if time.Since(r.createdAt) > s.ttl {
		return "", false
	}
	return r.content, true
}

// Cleanup removes expired entries. Call periodically from a background
// goroutine.
func (s *ToolResultStore) Cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, r := range s.results {
		if now.Sub(r.createdAt) > s.ttl {
			delete(s.results, id)
		}
	}
}

// StartCleanup begins a background goroutine that periodically removes expired
// entries from the store. It runs until ctx is cancelled.
func (s *ToolResultStore) StartCleanup(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Cleanup()
		}
	}
}

// Len returns the number of stored entries (for testing / monitoring).
func (s *ToolResultStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.results)
}

// evictOldest removes the oldest entry. Caller must hold the write lock.
func (s *ToolResultStore) evictOldest() {
	var oldestID string
	var oldestTime time.Time
	first := true
	for id, r := range s.results {
		if first || r.createdAt.Before(oldestTime) {
			oldestID = id
			oldestTime = r.createdAt
			first = false
		}
	}
	if oldestID != "" {
		delete(s.results, oldestID)
	}
}

// truncateRunes returns the first n runes of s. If s is shorter than n runes,
// the entire string is returned. The function is UTF-8 safe.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	count := 0
	for i := range s {
		if count >= n {
			return s[:i]
		}
		count++
	}
	return s
}

// generateID creates a random 8-byte hex ID.
func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---------------------------------------------------------------------------
// retrieve_tool_result tool
// ---------------------------------------------------------------------------

// DefaultToolResultStore is the global store used by retrieve_tool_result
// when no explicit store is provided.
var DefaultToolResultStore = NewToolResultStore(DefaultMaxSize, DefaultTTL)

// RetrieveInput is the input schema for the retrieve_tool_result tool.
type RetrieveInput struct {
	ResultID string `json:"result_id" jsonschema:"description=The retrieval ID returned in a truncated tool result"`
}

// RetrieveOutput is the output schema for the retrieve_tool_result tool.
type RetrieveOutput struct {
	Content string `json:"content"`
}

// NewRetrieveTool creates a retrieve_tool_result tool backed by the given
// ToolResultStore.
func NewRetrieveTool(store *ToolResultStore) (tool.InvokableTool, error) {
		if store == nil {
			store = DefaultToolResultStore
		}
		return utils.InferTool(
			"retrieve_tool_result",
			"Retrieve the full content of a previously truncated tool result by its retrieval ID (D-080). Returns a JSON envelope {\"success\":true,\"data\":{...}} on success or {\"success\":false,\"error\":\"...\"} on failure.",
			func(ctx context.Context, input RetrieveInput) (string, error) {
				if input.ResultID == "" {
					return SoftFailure("result_id is required"), nil
				}
				content, ok := store.Retrieve(input.ResultID)
				if !ok {
					return SoftFailure(fmt.Sprintf("result %q not found or expired. The result may have been garbage-collected or the ID is incorrect.", input.ResultID)), nil
				}
				return SuccessResult(&RetrieveOutput{Content: content})
			},
		)
	}
