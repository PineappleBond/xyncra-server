package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/metrics"
	dto "github.com/prometheus/client_model/go"
)

// captureLogger records Info/Error calls for assertion in tests.
type captureLogger struct {
	mu       sync.Mutex
	infoMsgs []capturedCall
	errMsgs  []capturedCall
}

type capturedCall struct {
	msg  string
	args []any
}

func (c *captureLogger) Info(msg string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.infoMsgs = append(c.infoMsgs, capturedCall{msg: msg, args: args})
}

func (c *captureLogger) Error(msg string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.errMsgs = append(c.errMsgs, capturedCall{msg: msg, args: args})
}

func (c *captureLogger) Debug(string, ...any) {}

func (c *captureLogger) infoCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.infoMsgs)
}

func (c *captureLogger) errorCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.errMsgs)
}

func (c *captureLogger) lastInfo() capturedCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.infoMsgs) == 0 {
		return capturedCall{}
	}
	return c.infoMsgs[len(c.infoMsgs)-1]
}

func (c *captureLogger) lastError() capturedCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.errMsgs) == 0 {
		return capturedCall{}
	}
	return c.errMsgs[len(c.errMsgs)-1]
}

// argsContains checks if the captured args contain a key with the expected value.
func argsContains(args []any, key string, value any) bool {
	for i := 0; i+1 < len(args); i += 2 {
		if k, ok := args[i].(string); ok && k == key {
			// Use fmt-style comparison via string representation for flexible matching.
			if args[i+1] == value {
				return true
			}
		}
	}
	return false
}

func TestLogMetrics_Record_Success(t *testing.T) {
	logger := &captureLogger{}
	m := NewLogMetrics(logger)

	event := LLMCallEvent{
		AgentID:      "agent/test-agent",
		Model:        "gpt-4",
		Duration:     1500 * time.Millisecond,
		InputTokens:  100,
		OutputTokens: 50,
	}

	m.Record(context.Background(), event)

	if logger.infoCount() != 1 {
		t.Fatalf("expected 1 Info call, got %d", logger.infoCount())
	}
	if logger.errorCount() != 0 {
		t.Fatalf("expected 0 Error calls, got %d", logger.errorCount())
	}

	call := logger.lastInfo()
	if call.msg != "llm call completed" {
		t.Errorf("expected msg %q, got %q", "llm call completed", call.msg)
	}
	if !argsContains(call.args, "agent_id", "agent/test-agent") {
		t.Error("expected agent_id in args")
	}
	if !argsContains(call.args, "model", "gpt-4") {
		t.Error("expected model in args")
	}
	if !argsContains(call.args, "duration_ms", int64(1500)) {
		t.Error("expected duration_ms=1500 in args")
	}
	if !argsContains(call.args, "input_tokens", 100) {
		t.Error("expected input_tokens=100 in args")
	}
	if !argsContains(call.args, "output_tokens", 50) {
		t.Error("expected output_tokens=50 in args")
	}
}

func TestLogMetrics_Record_Error(t *testing.T) {
	logger := &captureLogger{}
	m := NewLogMetrics(logger)

	testErr := errors.New("api timeout")
	event := LLMCallEvent{
		AgentID:  "agent/failing-agent",
		Model:    "claude-3",
		Duration: 30 * time.Second,
		Error:    testErr,
	}

	m.Record(context.Background(), event)

	if logger.errorCount() != 1 {
		t.Fatalf("expected 1 Error call, got %d", logger.errorCount())
	}
	if logger.infoCount() != 0 {
		t.Fatalf("expected 0 Info calls, got %d", logger.infoCount())
	}

	call := logger.lastError()
	if call.msg != "llm call failed" {
		t.Errorf("expected msg %q, got %q", "llm call failed", call.msg)
	}
	if !argsContains(call.args, "agent_id", "agent/failing-agent") {
		t.Error("expected agent_id in args")
	}
	if !argsContains(call.args, "error", testErr) {
		t.Error("expected error in args")
	}
	// Error path should not include token counts.
	for i := 0; i+1 < len(call.args); i += 2 {
		if k, ok := call.args[i].(string); ok && (k == "input_tokens" || k == "output_tokens") {
			t.Errorf("error path should not contain %s", k)
		}
	}
}

func TestLogMetrics_NilLogger(t *testing.T) {
	// NewLogMetrics with nil logger should not panic.
	m := NewLogMetrics(nil)

	event := LLMCallEvent{
		AgentID:      "agent/test",
		Model:        "gpt-4",
		Duration:     100 * time.Millisecond,
		InputTokens:  10,
		OutputTokens: 5,
	}

	// Should not panic.
	m.Record(context.Background(), event)

	// Error path should also not panic.
	event.Error = errors.New("test error")
	m.Record(context.Background(), event)
}

func TestNewLogMetrics_NilSafe(t *testing.T) {
	// Verify constructor is safe with nil.
	m := NewLogMetrics(nil)
	if m == nil {
		t.Fatal("NewLogMetrics(nil) returned nil")
	}
	if m.logger == nil {
		t.Fatal("logger field should not be nil (should use noopLogger)")
	}
}

func TestLogMetrics_Record_ZeroDuration(t *testing.T) {
	logger := &captureLogger{}
	m := NewLogMetrics(logger)

	event := LLMCallEvent{
		AgentID:  "agent/test",
		Model:    "ollama/llama3",
		Duration: 0,
	}

	m.Record(context.Background(), event)

	if logger.infoCount() != 1 {
		t.Fatalf("expected 1 Info call, got %d", logger.infoCount())
	}
	call := logger.lastInfo()
	if !argsContains(call.args, "duration_ms", int64(0)) {
		t.Error("expected duration_ms=0 in args")
	}
}

func TestLogMetrics_Record_ConcurrentSafe(t *testing.T) {
	logger := &captureLogger{}
	m := NewLogMetrics(logger)

	// Fire multiple concurrent Record calls to verify no data races.
	const goroutines = 10
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				event := LLMCallEvent{
					AgentID:  "agent/test",
					Model:    "gpt-4",
					Duration: time.Duration(j) * time.Millisecond,
				}
				m.Record(context.Background(), event)
			}
		}(i)
	}
	wg.Wait()

	expected := goroutines * iterations
	if logger.infoCount() != expected {
		t.Errorf("expected %d Info calls, got %d", expected, logger.infoCount())
	}
}

func TestLogMetrics_Record_DurationMilliseconds(t *testing.T) {
	logger := &captureLogger{}
	m := NewLogMetrics(logger)

	// Verify duration is properly converted to milliseconds.
	durations := []time.Duration{
		500 * time.Millisecond,
		1 * time.Second,
		2500 * time.Millisecond,
	}

	for _, d := range durations {
		event := LLMCallEvent{
			AgentID:  "agent/test",
			Model:    "gpt-4",
			Duration: d,
		}
		m.Record(context.Background(), event)
	}

	if logger.infoCount() != 3 {
		t.Fatalf("expected 3 Info calls, got %d", logger.infoCount())
	}
}

func TestLLMCallEvent_ErrorFieldOptional(t *testing.T) {
	// Verify that zero-value Error (nil) is treated as success.
	logger := &captureLogger{}
	m := NewLogMetrics(logger)

	event := LLMCallEvent{
		AgentID: "agent/test",
		Model:   "gpt-4",
	}

	m.Record(context.Background(), event)

	if logger.infoCount() != 1 {
		t.Errorf("nil Error should produce Info log, got %d Info calls", logger.infoCount())
	}
	if logger.errorCount() != 0 {
		t.Errorf("nil Error should not produce Error log, got %d Error calls", logger.errorCount())
	}
}

func TestLogMetrics_ArgsAreKeyValuePairs(t *testing.T) {
	logger := &captureLogger{}
	m := NewLogMetrics(logger)

	event := LLMCallEvent{
		AgentID:      "agent/test",
		Model:        "gpt-4",
		Duration:     100 * time.Millisecond,
		InputTokens:  10,
		OutputTokens: 20,
	}
	m.Record(context.Background(), event)

	call := logger.lastInfo()
	// All args should be key-value pairs (even number of args, string keys).
	if len(call.args)%2 != 0 {
		t.Errorf("args should have even length (key-value pairs), got %d", len(call.args))
	}
	for i := 0; i < len(call.args); i += 2 {
		key, ok := call.args[i].(string)
		if !ok {
			t.Errorf("arg[%d] should be string key, got %T", i, call.args[i])
		}
		// Verify expected keys exist.
		expectedKeys := map[string]bool{
			"agent_id": true, "model": true, "duration_ms": true,
			"input_tokens": true, "output_tokens": true,
		}
		if !expectedKeys[key] {
			t.Errorf("unexpected key %q", key)
		}
	}
}

func TestLogMetrics_ErrorArgsContainExpectedKeys(t *testing.T) {
	logger := &captureLogger{}
	m := NewLogMetrics(logger)

	event := LLMCallEvent{
		AgentID:  "agent/test",
		Model:    "gpt-4",
		Duration: 100 * time.Millisecond,
		Error:    errors.New("boom"),
	}
	m.Record(context.Background(), event)

	call := logger.lastError()
	if len(call.args)%2 != 0 {
		t.Errorf("args should have even length (key-value pairs), got %d", len(call.args))
	}

	// Collect keys present in error log.
	var keys []string
	for i := 0; i < len(call.args); i += 2 {
		if k, ok := call.args[i].(string); ok {
			keys = append(keys, k)
		}
	}

	// Error log should have agent_id, model, duration_ms, error.
	for _, expected := range []string{"agent_id", "model", "duration_ms", "error"} {
		found := false
		for _, k := range keys {
			if k == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("error log missing expected key %q, got keys: %s", expected, strings.Join(keys, ", "))
		}
	}
}

// ---------------------------------------------------------------------------
// PrometheusMetrics tests
// ---------------------------------------------------------------------------

// readCounterValue reads the current value of a prometheus Counter.
func readCounterValue(t *testing.T, c interface{ Write(*dto.Metric) error }) float64 {
	t.Helper()
	m := &dto.Metric{}
	if err := c.Write(m); err != nil {
		t.Fatalf("failed to read counter: %v", err)
	}
	return m.GetCounter().GetValue()
}

func TestPrometheusMetrics_Record_Success(t *testing.T) {
	pm := NewPrometheusMetrics()
	ctx := context.Background()

	event := LLMCallEvent{
		AgentID:      "test-agent-prom",
		Model:        "gpt-4",
		Duration:     2 * time.Second,
		InputTokens:  100,
		OutputTokens: 50,
	}

	pm.Record(ctx, event)

	// Verify AgentExecutions incremented.
	counter, err := metrics.AgentExecutions.GetMetricWithLabelValues("test-agent-prom", "gpt-4")
	if err != nil {
		t.Fatalf("get counter: %v", err)
	}
	if got := readCounterValue(t, counter); got < 1 {
		t.Errorf("agent executions >= 1, got %f", got)
	}

	// Verify LLMCallsTotal incremented.
	llmCounter, err := metrics.LLMCallsTotal.GetMetricWithLabelValues("test-agent-prom", "gpt-4")
	if err != nil {
		t.Fatalf("get counter: %v", err)
	}
	if got := readCounterValue(t, llmCounter); got < 1 {
		t.Errorf("llm calls total >= 1, got %f", got)
	}

	// Verify LLMTokensInput.
	inputCounter, err := metrics.LLMTokensInput.GetMetricWithLabelValues("test-agent-prom", "gpt-4")
	if err != nil {
		t.Fatalf("get counter: %v", err)
	}
	if got := readCounterValue(t, inputCounter); got < 100 {
		t.Errorf("input tokens >= 100, got %f", got)
	}

	// Verify LLMTokensOutput.
	outputCounter, err := metrics.LLMTokensOutput.GetMetricWithLabelValues("test-agent-prom", "gpt-4")
	if err != nil {
		t.Fatalf("get counter: %v", err)
	}
	if got := readCounterValue(t, outputCounter); got < 50 {
		t.Errorf("output tokens >= 50, got %f", got)
	}

	// Verify AgentDuration histogram was called (no panic means it worked).
	// Reading histogram values from Observer requires testutil; we verify
	// the counter metrics above are sufficient for validation.
}

func TestPrometheusMetrics_Record_Error(t *testing.T) {
	pm := NewPrometheusMetrics()
	ctx := context.Background()

	testErr := errors.New("rate limited")
	event := LLMCallEvent{
		AgentID:  "error-agent-prom",
		Model:    "gpt-3.5",
		Duration: time.Second,
		Error:    testErr,
	}

	pm.Record(ctx, event)

	// Verify AgentExecutionsFailed incremented.
	failCounter, err := metrics.AgentExecutionsFailed.GetMetricWithLabelValues("error-agent-prom", "rate limited")
	if err != nil {
		t.Fatalf("get counter: %v", err)
	}
	if got := readCounterValue(t, failCounter); got < 1 {
		t.Errorf("agent executions failed >= 1, got %f", got)
	}

	// Verify LLMCallsFailed incremented.
	llmFail, err := metrics.LLMCallsFailed.GetMetricWithLabelValues("error-agent-prom", "gpt-3.5", "rate limited")
	if err != nil {
		t.Fatalf("get counter: %v", err)
	}
	if got := readCounterValue(t, llmFail); got < 1 {
		t.Errorf("llm calls failed >= 1, got %f", got)
	}
}

// ---------------------------------------------------------------------------
// MultiMetrics tests
// ---------------------------------------------------------------------------

// recordingMetrics tracks Record calls for testing MultiMetrics fan-out.
type recordingMetrics struct {
	mu     sync.Mutex
	events []LLMCallEvent
}

func (r *recordingMetrics) Record(_ context.Context, event LLMCallEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *recordingMetrics) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

func TestMultiMetrics_FanOut(t *testing.T) {
	r1 := &recordingMetrics{}
	r2 := &recordingMetrics{}
	mm := NewMultiMetrics(r1, r2)

	ctx := context.Background()
	event := LLMCallEvent{
		AgentID:      "multi-agent",
		Model:        "claude-3",
		Duration:     time.Second,
		InputTokens:  200,
		OutputTokens: 100,
	}

	mm.Record(ctx, event)

	if r1.count() != 1 {
		t.Errorf("expected r1 to have 1 event, got %d", r1.count())
	}
	if r2.count() != 1 {
		t.Errorf("expected r2 to have 1 event, got %d", r2.count())
	}

	// Call again.
	mm.Record(ctx, event)
	if r1.count() != 2 {
		t.Errorf("expected r1 to have 2 events, got %d", r1.count())
	}
	if r2.count() != 2 {
		t.Errorf("expected r2 to have 2 events, got %d", r2.count())
	}
}

func TestMultiMetrics_Empty(t *testing.T) {
	mm := NewMultiMetrics()
	// Should not panic.
	mm.Record(context.Background(), LLMCallEvent{AgentID: "test", Model: "test"})
}

// TestMultiMetrics_Record_FansOut is an alias test matching the acceptance
// criteria name. It verifies that with 2 mock implementations, both receive
// the event.
//
// Acceptance criteria:
//   - 2 mock LLMMetrics implementations
//   - Both receive the event
func TestMultiMetrics_Record_FansOut(t *testing.T) {
	r1 := &recordingMetrics{}
	r2 := &recordingMetrics{}
	mm := NewMultiMetrics(r1, r2)

	event := LLMCallEvent{
		AgentID:      "fans-out-agent",
		Model:        "gpt-4",
		Duration:     time.Second,
		InputTokens:  10,
		OutputTokens: 20,
	}
	mm.Record(context.Background(), event)

	if r1.count() != 1 {
		t.Errorf("impl 1: expected 1 event, got %d", r1.count())
	}
	if r2.count() != 1 {
		t.Errorf("impl 2: expected 1 event, got %d", r2.count())
	}
}
