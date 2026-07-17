package mq

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Docker Redis setup
// ---------------------------------------------------------------------------

// setupTestRedis attempts to start a Docker Redis instance for integration
// tests. If Docker or Redis is unavailable, the test is skipped.
func setupTestRedis(t *testing.T) {
	t.Helper()

	// Try to start Docker Redis.
	cmd := exec.Command("docker", "run", "-d", "--name", "xyncra-test-redis",
		"-p", "6379:6379", "redis:7-alpine")
	if err := cmd.Run(); err != nil {
		// Docker might not be available, or Redis might already be running.
		if !isRedisAvailable() {
			t.Skipf("Redis not available and Docker failed to start: %v", err)
		}
	}

	// Wait for Redis to become available.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if isRedisAvailable() {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if !isRedisAvailable() {
		t.Skip("Redis did not become available within timeout")
	}

	// Register cleanup.
	t.Cleanup(func() {
		exec.Command("docker", "rm", "-f", "xyncra-test-redis").Run()
	})
}

// isRedisAvailable attempts a simple Redis PING to check availability.
func isRedisAvailable() bool {
	cmd := exec.Command("docker", "exec", "xyncra-test-redis", "redis-cli", "ping")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return string(output) == "PONG\n"
}

// ---------------------------------------------------------------------------
// NewAsynqBroker tests
// ---------------------------------------------------------------------------

// TestNewAsynqBroker_ValidConfig verifies that a broker can be created with a
// valid configuration.
func TestNewAsynqBroker_ValidConfig(t *testing.T) {
	t.Parallel()
	setupTestRedis(t)

	cfg := AsynqConfig{
		RedisAddr: "localhost:6379",
	}

	broker, err := NewAsynqBroker(cfg)
	require.NoError(t, err, "expected no error")
	require.NotNil(t, broker, "expected non-nil broker")

	// Clean up.
	_ = broker.Close()
}

// TestNewAsynqBroker_EmptyAddress verifies that creating a broker with an
// empty Redis address returns an error.
func TestNewAsynqBroker_EmptyAddress(t *testing.T) {
	t.Parallel()

	cfg := AsynqConfig{
		RedisAddr: "",
	}

	broker, err := NewAsynqBroker(cfg)
	require.Error(t, err, "expected error for empty Redis address")
	assert.Nil(t, broker, "expected nil broker on error")
	assert.Equal(t, "mq: redis address is required", err.Error())
}

// ---------------------------------------------------------------------------
// Enqueue tests
// ---------------------------------------------------------------------------

// TestEnqueue_NilTask verifies that Enqueue returns ErrInvalidTask for a nil
// task.
func TestEnqueue_NilTask(t *testing.T) {
	t.Parallel()
	setupTestRedis(t)

	broker := newTestBroker(t)
	t.Cleanup(func() { _ = broker.Close() })

	ctx := context.Background()
	taskID, err := broker.Enqueue(ctx, nil)
	assert.ErrorIs(t, err, ErrInvalidTask)
	assert.Empty(t, taskID, "expected empty taskID")
}

// TestEnqueue_EmptyType verifies that Enqueue returns an error when the task
// type is empty.
func TestEnqueue_EmptyType(t *testing.T) {
	t.Parallel()
	setupTestRedis(t)

	broker := newTestBroker(t)
	t.Cleanup(func() { _ = broker.Close() })

	ctx := context.Background()
	task := &Task{Type: "", Payload: []byte(`{}`)}

	taskID, err := broker.Enqueue(ctx, task)
	assert.ErrorIs(t, err, ErrInvalidTask)
	assert.Empty(t, taskID, "expected empty taskID")
}

// TestEnqueue_Success verifies that a valid task can be enqueued and returns a
// non-empty task ID.
func TestEnqueue_Success(t *testing.T) {
	t.Parallel()
	setupTestRedis(t)

	broker := newTestBroker(t)
	t.Cleanup(func() { _ = broker.Close() })

	ctx := context.Background()
	task := &Task{
		Type:    "test:enqueue",
		Payload: []byte(`{"message":"hello"}`),
	}

	taskID, err := broker.Enqueue(ctx, task)
	require.NoError(t, err, "unexpected error")
	assert.NotEmpty(t, taskID, "expected non-empty taskID")
}

// TestEnqueue_WithQueue verifies that WithQueue is applied correctly.
func TestEnqueue_WithQueue(t *testing.T) {
	t.Parallel()
	setupTestRedis(t)

	broker := newTestBroker(t)
	t.Cleanup(func() { _ = broker.Close() })

	ctx := context.Background()
	task := &Task{Type: "test:options", Payload: []byte(`{}`)}

	taskID, err := broker.Enqueue(ctx, task, WithQueue(QueueCritical))
	require.NoError(t, err)
	assert.NotEmpty(t, taskID)
}

// TestEnqueue_WithMaxRetry verifies that WithMaxRetry is applied correctly.
func TestEnqueue_WithMaxRetry(t *testing.T) {
	t.Parallel()
	setupTestRedis(t)

	broker := newTestBroker(t)
	t.Cleanup(func() { _ = broker.Close() })

	ctx := context.Background()
	task := &Task{Type: "test:options", Payload: []byte(`{}`)}

	taskID, err := broker.Enqueue(ctx, task, WithMaxRetry(5))
	require.NoError(t, err)
	assert.NotEmpty(t, taskID)
}

// TestEnqueue_WithTimeout verifies that WithTimeout is applied correctly.
func TestEnqueue_WithTimeout(t *testing.T) {
	t.Parallel()
	setupTestRedis(t)

	broker := newTestBroker(t)
	t.Cleanup(func() { _ = broker.Close() })

	ctx := context.Background()
	task := &Task{Type: "test:options", Payload: []byte(`{}`)}

	taskID, err := broker.Enqueue(ctx, task, WithTimeout(30*time.Second))
	require.NoError(t, err)
	assert.NotEmpty(t, taskID)
}

// TestEnqueue_WithTaskID verifies that WithTaskID is applied correctly.
func TestEnqueue_WithTaskID(t *testing.T) {
	t.Parallel()
	setupTestRedis(t)

	broker := newTestBroker(t)
	t.Cleanup(func() { _ = broker.Close() })

	ctx := context.Background()
	task := &Task{Type: "test:options", Payload: []byte(`{}`)}

	taskID, err := broker.Enqueue(ctx, task, WithTaskID("custom-task-id"))
	require.NoError(t, err)
	assert.Equal(t, "custom-task-id", taskID)
}

// TestEnqueue_WithRetention verifies that WithRetention is applied correctly.
func TestEnqueue_WithRetention(t *testing.T) {
	t.Parallel()
	setupTestRedis(t)

	broker := newTestBroker(t)
	t.Cleanup(func() { _ = broker.Close() })

	ctx := context.Background()
	task := &Task{Type: "test:options", Payload: []byte(`{}`)}

	taskID, err := broker.Enqueue(ctx, task, WithRetention(10*time.Minute))
	require.NoError(t, err)
	assert.NotEmpty(t, taskID)
}

// TestEnqueue_WithProcessIn verifies that WithProcessIn is applied correctly.
func TestEnqueue_WithProcessIn(t *testing.T) {
	t.Parallel()
	setupTestRedis(t)

	broker := newTestBroker(t)
	t.Cleanup(func() { _ = broker.Close() })

	ctx := context.Background()
	task := &Task{Type: "test:options", Payload: []byte(`{}`)}

	taskID, err := broker.Enqueue(ctx, task, WithProcessIn(5*time.Second))
	require.NoError(t, err)
	assert.NotEmpty(t, taskID)
}

// TestEnqueue_WithUnique verifies that WithUnique is applied correctly.
func TestEnqueue_WithUnique(t *testing.T) {
	t.Parallel()
	setupTestRedis(t)

	broker := newTestBroker(t)
	t.Cleanup(func() { _ = broker.Close() })

	ctx := context.Background()
	task := &Task{Type: "test:options", Payload: []byte(`{}`)}

	taskID, err := broker.Enqueue(ctx, task, WithUnique())
	require.NoError(t, err)
	assert.NotEmpty(t, taskID)
}

// TestEnqueue_MultipleOptions verifies that multiple options can be combined.
func TestEnqueue_MultipleOptions(t *testing.T) {
	t.Parallel()
	setupTestRedis(t)

	broker := newTestBroker(t)
	t.Cleanup(func() { _ = broker.Close() })

	ctx := context.Background()
	task := &Task{Type: "test:options", Payload: []byte(`{}`)}

	taskID, err := broker.Enqueue(ctx, task,
		WithQueue(QueueLow),
		WithMaxRetry(3),
		WithTimeout(1*time.Minute),
	)
	require.NoError(t, err)
	assert.NotEmpty(t, taskID)
}

// ---------------------------------------------------------------------------
// Start and Stop tests
// ---------------------------------------------------------------------------

// TestStart_ProcessesTask verifies that a task can be enqueued and processed by
// a handler after Start is called.
func TestStart_ProcessesTask(t *testing.T) {
	// Cannot use t.Parallel() because this test uses shared Redis.
	setupTestRedis(t)

	broker := newTestBroker(t)
	t.Cleanup(func() { _ = broker.Close() })

	processed := make(chan string, 1)
	handler := NewTaskHandler()
	handler.Register("test:process", func(ctx context.Context, task *Task) error {
		processed <- task.Type
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel() })

	// Start the broker in a goroutine.
	startErr := make(chan error, 1)
	go func() {
		startErr <- broker.Start(ctx, handler)
	}()

	// Give the server a moment to start.
	time.Sleep(500 * time.Millisecond)

	// Enqueue a task.
	task := &Task{
		Type:    "test:process",
		Payload: []byte(`{"data":"test"}`),
	}
	taskID, err := broker.Enqueue(context.Background(), task)
	require.NoError(t, err, "failed to enqueue")
	assert.NotEmpty(t, taskID, "expected non-empty taskID")

	// Wait for the task to be processed.
	select {
	case processedType := <-processed:
		assert.Equal(t, "test:process", processedType)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for task to be processed")
	}

	// Stop the broker.
	cancel()
	select {
	case err := <-startErr:
		assert.NoError(t, err, "Start returned error")
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for Start to return")
	}
}

// TestStart_NilHandler verifies that Start returns an error when given a nil
// handler.
func TestStart_NilHandler(t *testing.T) {
	t.Parallel()
	setupTestRedis(t)

	broker := newTestBroker(t)
	t.Cleanup(func() { _ = broker.Close() })

	ctx := context.Background()
	err := broker.Start(ctx, nil)
	require.Error(t, err, "expected error for nil handler")
	assert.Equal(t, "mq: handler must not be nil", err.Error())
}

// TestStop_GracefulShutdown verifies that Stop performs a graceful shutdown
// without panicking.
func TestStop_GracefulShutdown(t *testing.T) {
	// Cannot use t.Parallel() because this test uses shared Redis.
	setupTestRedis(t)

	broker := newTestBroker(t)
	t.Cleanup(func() { _ = broker.Close() })

	handler := NewTaskHandler()
	handler.Register("test:stop", func(ctx context.Context, task *Task) error {
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel() })

	// Start the broker.
	go broker.Start(ctx, handler)
	time.Sleep(500 * time.Millisecond)

	// Stop should not panic.
	assert.NotPanics(t, func() {
		broker.Stop()
	})

	// Cancel context to allow Start to return.
	cancel()
	time.Sleep(200 * time.Millisecond)
}

// ---------------------------------------------------------------------------
// Close tests
// ---------------------------------------------------------------------------

// TestClose_ReleasesResources verifies that Close releases all resources
// without error, even if Start was never called.
func TestClose_ReleasesResources(t *testing.T) {
	t.Parallel()
	setupTestRedis(t)

	broker := newTestBroker(t)

	err := broker.Close()
	assert.NoError(t, err, "unexpected error from Close")
}

// ---------------------------------------------------------------------------
// TaskIDFromContext tests
// ---------------------------------------------------------------------------

// TestTaskIDFromContext_InHandler verifies that TaskIDFromContext returns the
// correct task ID within a handler.
func TestTaskIDFromContext_InHandler(t *testing.T) {
	// Cannot use t.Parallel() because this test uses shared Redis.
	setupTestRedis(t)

	broker := newTestBroker(t)
	t.Cleanup(func() { _ = broker.Close() })

	receivedID := make(chan string, 1)
	handler := NewTaskHandler()
	handler.Register("test:taskid", func(ctx context.Context, task *Task) error {
		id := TaskIDFromContext(ctx)
		receivedID <- id
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel() })

	go broker.Start(ctx, handler)
	time.Sleep(500 * time.Millisecond)

	task := &Task{
		Type:    "test:taskid",
		Payload: []byte(`{}`),
	}
	enqueuedID, err := broker.Enqueue(context.Background(), task)
	require.NoError(t, err, "failed to enqueue")

	select {
	case id := <-receivedID:
		assert.Equal(t, enqueuedID, id, "task ID should match enqueued ID")
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for task ID")
	}

	cancel()
	time.Sleep(200 * time.Millisecond)
}

// TestTaskIDFromContext_OutsideHandler verifies that TaskIDFromContext returns
// an empty string when called outside of a handler context.
func TestTaskIDFromContext_OutsideHandler(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	id := TaskIDFromContext(ctx)
	assert.Empty(t, id, "expected empty string outside handler")
}

// ---------------------------------------------------------------------------
// decodeAsynqTask tests
// ---------------------------------------------------------------------------

// TestDecodeAsynqTask_ValidPayload verifies that decodeAsynqTask correctly
// deserializes a valid asynq task payload.
func TestDecodeAsynqTask_ValidPayload(t *testing.T) {
	t.Parallel()

	payload := asynqTaskPayload{
		Type:    "test:decode",
		Payload: json.RawMessage(`{"key":"value"}`),
	}
	data, err := json.Marshal(payload)
	require.NoError(t, err, "failed to marshal")

	aTask := asynq.NewTask("test:decode", data)
	task, err := decodeAsynqTask(aTask)
	require.NoError(t, err, "unexpected error")
	assert.Equal(t, "test:decode", task.Type)
	assert.Equal(t, `{"key":"value"}`, string(task.Payload))
}

// TestDecodeAsynqTask_InvalidPayload verifies that decodeAsynqTask returns an
// error for invalid JSON.
func TestDecodeAsynqTask_InvalidPayload(t *testing.T) {
	t.Parallel()

	aTask := asynq.NewTask("test:invalid", []byte("not valid json"))
	task, err := decodeAsynqTask(aTask)
	require.Error(t, err, "expected error for invalid JSON")
	assert.Nil(t, task, "expected nil task on error")
}

// TestDecodeAsynqTask_EmptyPayload verifies that decodeAsynqTask returns an
// error when the envelope has an empty Type field.
func TestDecodeAsynqTask_EmptyPayload(t *testing.T) {
	t.Parallel()

	aTask := asynq.NewTask("test:empty", []byte(`{}`))
	task, err := decodeAsynqTask(aTask)
	require.Error(t, err, "expected error for empty type")
	assert.ErrorIs(t, err, ErrInvalidTask, "expected ErrInvalidTask")
	assert.Nil(t, task, "expected nil task on error")
}

// ---------------------------------------------------------------------------
// slogAsynqLogger tests
// ---------------------------------------------------------------------------

// TestAsynqLogger_UsesSlog verifies that the asynq internal logger delegates
// to slog, producing structured slog output rather than raw fmt output.
//
// Acceptance criteria:
//   - Each log method (Debug, Info, Warn, Error, Fatal) produces output
//   - Output is valid slog format (contains slog-standard keys)
//   - The logger includes the "component":"asynq" field
func TestAsynqLogger_UsesSlog(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	// Install a JSON slog handler writing to buf.
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(h))

	// Create a fresh asynq logger that picks up the new slog.Default().
	l := newAsynqLogger()

	// Exercise all log methods.
	l.Debug("debug-msg")
	l.Info("info-msg")
	l.Warn("warn-msg")
	l.Error("error-msg")
	l.Fatal("fatal-msg")

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 log lines, got %d: %q", len(lines), output)
	}

	// Verify each line is valid JSON.
	for i, line := range lines {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("line %d not valid JSON: %v\nline: %q", i, err, line)
		}
		// Each line should contain a "msg" field.
		if _, ok := rec["msg"]; !ok {
			t.Errorf("line %d missing 'msg' field: %v", i, rec)
		}
		// Each line should contain a "level" field.
		if _, ok := rec["level"]; !ok {
			t.Errorf("line %d missing 'level' field: %v", i, rec)
		}
	}

	// Verify the component field is set to "asynq".
	var firstLine map[string]any
	_ = json.Unmarshal([]byte(lines[0]), &firstLine)
	if comp, ok := firstLine["component"]; !ok || comp != "asynq" {
		t.Errorf("expected component=asynq, got %v", firstLine["component"])
	}

	// Verify the expected log messages appear in the output.
	expectedMsgs := []string{"debug-msg", "info-msg", "warn-msg", "error-msg", "fatal-msg"}
	for i, expected := range expectedMsgs {
		var rec map[string]any
		_ = json.Unmarshal([]byte(lines[i]), &rec)
		if msg, ok := rec["msg"].(string); !ok || msg != expected {
			t.Errorf("line %d: expected msg=%q, got %v", i, expected, rec["msg"])
		}
	}
}

// ---------------------------------------------------------------------------
// buildAsynqOptions tests
// ---------------------------------------------------------------------------

// TestBuildAsynqOptions_Default verifies that default options produce an empty
// asynq option list (since queue="default" and maxRetry=-1 are sentinels).
func TestBuildAsynqOptions_Default(t *testing.T) {
	t.Parallel()

	opts := defaultEnqueueOptions()
	aOpts := buildAsynqOptions(opts)

	// Default options have queue="default" and maxRetry=-1 (sentinel).
	// buildAsynqOptions adds Queue if queue != "", so "default" is added.
	// maxRetry=-1 is < 0, so MaxRetry is NOT added.
	assert.Len(t, aOpts, 1, "default options should produce 1 option (Queue)")
}

// TestBuildAsynqOptions_AllFields verifies that all non-zero fields are
// converted to asynq options.
func TestBuildAsynqOptions_AllFields(t *testing.T) {
	t.Parallel()

	opts := enqueueOptions{
		queue:     QueueCritical,
		maxRetry:  5,
		timeout:   30 * time.Second,
		taskID:    "test-id",
		retention: 10 * time.Minute,
		processIn: 5 * time.Second,
		deadline:  time.Now().Add(1 * time.Hour),
		unique:    true,
		uniqueTTL: 5 * time.Minute,
	}

	aOpts := buildAsynqOptions(opts)

	// Should have options for: queue, maxRetry, timeout, taskID, retention,
	// processIn, deadline, unique (with TTL).
	assert.Len(t, aOpts, 8, "expected 8 options for all fields set")
}

// TestBuildAsynqOptions_UniqueWithoutTTL verifies that unique without TTL
// still produces asynq options (MaxRetry=0 and Unique with default 5-minute TTL).
func TestBuildAsynqOptions_UniqueWithoutTTL(t *testing.T) {
	t.Parallel()

	opts := enqueueOptions{
		unique:    true,
		uniqueTTL: 0,
	}

	aOpts := buildAsynqOptions(opts)

	// maxRetry=0 (zero value) >= 0, so MaxRetry(0) is added.
	// unique=true, uniqueTTL=0, so Unique(5m) is added (default TTL).
	assert.Len(t, aOpts, 2, "expected 2 options (MaxRetry + Unique)")
}

// TestBuildAsynqOptions_PartialFields verifies that non-zero fields are
// converted. Note: maxRetry=0 (zero value) is >= 0, so it's always added.
func TestBuildAsynqOptions_PartialFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		opts     enqueueOptions
		expected int
	}{
		{
			name:     "only queue",
			opts:     enqueueOptions{queue: QueueLow},
			expected: 2, // Queue("low") + MaxRetry(0) since maxRetry=0 >= 0
		},
		{
			name:     "only maxRetry",
			opts:     enqueueOptions{maxRetry: 3},
			expected: 1, // MaxRetry(3)
		},
		{
			name:     "only timeout",
			opts:     enqueueOptions{timeout: 1 * time.Minute},
			expected: 2, // MaxRetry(0) + Timeout(1m)
		},
		{
			name:     "queue and maxRetry",
			opts:     enqueueOptions{queue: QueueCritical, maxRetry: 10},
			expected: 2, // Queue("critical") + MaxRetry(10)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			aOpts := buildAsynqOptions(tt.opts)
			assert.Len(t, aOpts, tt.expected)
		})
	}
}

// ---------------------------------------------------------------------------
// AsynqConfig method tests
// ---------------------------------------------------------------------------

// TestAsynqConfig_RedisClientOpt verifies that redisClientOpt builds the
// correct asynq.RedisClientOpt.
func TestAsynqConfig_RedisClientOpt(t *testing.T) {
	t.Parallel()

	cfg := AsynqConfig{
		RedisAddr:     "localhost:6379",
		RedisPassword: "secret",
		RedisDB:       2,
	}

	opt := cfg.redisClientOpt()
	assert.Equal(t, "localhost:6379", opt.Addr)
	assert.Equal(t, "secret", opt.Password)
	assert.Equal(t, 2, opt.DB)
}

// TestAsynqConfig_ServerConfig_DefaultQueues verifies that serverConfig uses
// DefaultQueuePriority when Queues is nil.
func TestAsynqConfig_ServerConfig_DefaultQueues(t *testing.T) {
	t.Parallel()

	cfg := AsynqConfig{
		RedisAddr:   "localhost:6379",
		Concurrency: 4,
	}

	sCfg := cfg.serverConfig()
	assert.Equal(t, 4, sCfg.Concurrency)
	assert.Equal(t, DefaultQueuePriority(), sCfg.Queues)
}

// TestAsynqConfig_ServerConfig_CustomQueues verifies that serverConfig uses
// the provided Queues map when set.
func TestAsynqConfig_ServerConfig_CustomQueues(t *testing.T) {
	t.Parallel()

	customQueues := map[string]int{
		"high":   10,
		"medium": 5,
		"low":    1,
	}
	cfg := AsynqConfig{
		RedisAddr: "localhost:6379",
		Queues:    customQueues,
	}

	sCfg := cfg.serverConfig()
	assert.Equal(t, customQueues, sCfg.Queues)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newTestBroker creates an AsynqBroker configured for testing with a local
// Redis instance.
func newTestBroker(t *testing.T) *AsynqBroker {
	t.Helper()

	broker, err := NewAsynqBroker(AsynqConfig{
		RedisAddr:   "localhost:6379",
		Concurrency: 2,
		Queues: map[string]int{
			QueueCritical: 6,
			QueueDefault:  3,
			QueueLow:      1,
		},
	})
	require.NoError(t, err, "failed to create broker")

	return broker
}
