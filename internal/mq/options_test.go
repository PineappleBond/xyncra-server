package mq

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// defaultEnqueueOptions tests
// ---------------------------------------------------------------------------

// TestDefaultEnqueueOptions verifies the baseline values returned before any
// With... option is applied.
func TestDefaultEnqueueOptions(t *testing.T) {
	opts := defaultEnqueueOptions()

	if opts.queue != QueueDefault {
		t.Errorf("expected queue = %q, got %q", QueueDefault, opts.queue)
	}
	if opts.maxRetry != -1 {
		t.Errorf("expected maxRetry = -1 (sentinel), got %d", opts.maxRetry)
	}
	if opts.timeout != 0 {
		t.Errorf("expected timeout = 0, got %v", opts.timeout)
	}
	if opts.taskID != "" {
		t.Errorf("expected taskID = %q, got %q", "", opts.taskID)
	}
	if opts.retention != 0 {
		t.Errorf("expected retention = 0, got %v", opts.retention)
	}
	if opts.processIn != 0 {
		t.Errorf("expected processIn = 0, got %v", opts.processIn)
	}
	if !opts.deadline.IsZero() {
		t.Errorf("expected zero deadline, got %v", opts.deadline)
	}
	if opts.unique {
		t.Error("expected unique = false")
	}
	if opts.uniqueTTL != 0 {
		t.Errorf("expected uniqueTTL = 0, got %v", opts.uniqueTTL)
	}
}

// ---------------------------------------------------------------------------
// WithQueue tests
// ---------------------------------------------------------------------------

func TestWithQueue(t *testing.T) {
	t.Run("sets non-empty name", func(t *testing.T) {
		opts := defaultEnqueueOptions()
		WithQueue("high")(&opts)
		if opts.queue != "high" {
			t.Errorf("expected queue = %q, got %q", "high", opts.queue)
		}
	})

	t.Run("ignores empty name", func(t *testing.T) {
		opts := defaultEnqueueOptions()
		WithQueue("")(&opts)
		if opts.queue != QueueDefault {
			t.Errorf("expected queue unchanged (%q), got %q", QueueDefault, opts.queue)
		}
	})

	t.Run("overrides previous value", func(t *testing.T) {
		opts := defaultEnqueueOptions()
		WithQueue("first")(&opts)
		WithQueue("second")(&opts)
		if opts.queue != "second" {
			t.Errorf("expected queue = %q, got %q", "second", opts.queue)
		}
	})
}

// ---------------------------------------------------------------------------
// WithMaxRetry tests
// ---------------------------------------------------------------------------

func TestWithMaxRetry(t *testing.T) {
	tests := []struct {
		name  string
		n     int
		want  int
		changed bool
	}{
		{"positive value", 5, 5, true},
		{"zero disables retries", 0, 0, true},
		{"negative value ignored", -1, -1, false}, // stays at sentinel
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := defaultEnqueueOptions()
			WithMaxRetry(tt.n)(&opts)
			if opts.maxRetry != tt.want {
				t.Errorf("expected maxRetry = %d, got %d", tt.want, opts.maxRetry)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// WithTimeout tests
// ---------------------------------------------------------------------------

func TestWithTimeout(t *testing.T) {
	t.Run("positive duration", func(t *testing.T) {
		opts := defaultEnqueueOptions()
		WithTimeout(30 * time.Second)(&opts)
		if opts.timeout != 30*time.Second {
			t.Errorf("expected timeout = 30s, got %v", opts.timeout)
		}
	})

	t.Run("zero duration ignored", func(t *testing.T) {
		opts := defaultEnqueueOptions()
		WithTimeout(0)(&opts)
		if opts.timeout != 0 {
			t.Errorf("expected timeout unchanged (0), got %v", opts.timeout)
		}
	})

	t.Run("negative duration ignored", func(t *testing.T) {
		opts := defaultEnqueueOptions()
		WithTimeout(-5 * time.Second)(&opts)
		if opts.timeout != 0 {
			t.Errorf("expected timeout unchanged (0), got %v", opts.timeout)
		}
	})
}

// ---------------------------------------------------------------------------
// WithTaskID tests
// ---------------------------------------------------------------------------

func TestWithTaskID(t *testing.T) {
	t.Run("sets non-empty id", func(t *testing.T) {
		opts := defaultEnqueueOptions()
		WithTaskID("my-task-123")(&opts)
		if opts.taskID != "my-task-123" {
			t.Errorf("expected taskID = %q, got %q", "my-task-123", opts.taskID)
		}
	})

	t.Run("ignores empty id", func(t *testing.T) {
		opts := defaultEnqueueOptions()
		WithTaskID("")(&opts)
		if opts.taskID != "" {
			t.Errorf("expected taskID unchanged (%q), got %q", "", opts.taskID)
		}
	})
}

// ---------------------------------------------------------------------------
// WithRetention tests
// ---------------------------------------------------------------------------

func TestWithRetention(t *testing.T) {
	t.Run("positive duration", func(t *testing.T) {
		opts := defaultEnqueueOptions()
		WithRetention(10 * time.Minute)(&opts)
		if opts.retention != 10*time.Minute {
			t.Errorf("expected retention = 10m, got %v", opts.retention)
		}
	})

	t.Run("zero duration ignored", func(t *testing.T) {
		opts := defaultEnqueueOptions()
		WithRetention(0)(&opts)
		if opts.retention != 0 {
			t.Errorf("expected retention unchanged (0), got %v", opts.retention)
		}
	})

	t.Run("negative duration ignored", func(t *testing.T) {
		opts := defaultEnqueueOptions()
		WithRetention(-1 * time.Second)(&opts)
		if opts.retention != 0 {
			t.Errorf("expected retention unchanged (0), got %v", opts.retention)
		}
	})
}

// ---------------------------------------------------------------------------
// WithProcessIn tests
// ---------------------------------------------------------------------------

func TestWithProcessIn(t *testing.T) {
	t.Run("positive duration", func(t *testing.T) {
		opts := defaultEnqueueOptions()
		WithProcessIn(5 * time.Second)(&opts)
		if opts.processIn != 5*time.Second {
			t.Errorf("expected processIn = 5s, got %v", opts.processIn)
		}
	})

	t.Run("zero duration ignored", func(t *testing.T) {
		opts := defaultEnqueueOptions()
		WithProcessIn(0)(&opts)
		if opts.processIn != 0 {
			t.Errorf("expected processIn unchanged (0), got %v", opts.processIn)
		}
	})

	t.Run("negative duration ignored", func(t *testing.T) {
		opts := defaultEnqueueOptions()
		WithProcessIn(-2 * time.Second)(&opts)
		if opts.processIn != 0 {
			t.Errorf("expected processIn unchanged (0), got %v", opts.processIn)
		}
	})
}

// ---------------------------------------------------------------------------
// WithDeadline tests
// ---------------------------------------------------------------------------

func TestWithDeadline(t *testing.T) {
	t.Run("sets deadline", func(t *testing.T) {
		opts := defaultEnqueueOptions()
		deadline := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
		WithDeadline(deadline)(&opts)
		if !opts.deadline.Equal(deadline) {
			t.Errorf("expected deadline = %v, got %v", deadline, opts.deadline)
		}
	})

	t.Run("zero time is accepted", func(t *testing.T) {
		opts := defaultEnqueueOptions()
		WithDeadline(time.Time{})(&opts)
		if !opts.deadline.IsZero() {
			t.Errorf("expected zero deadline, got %v", opts.deadline)
		}
	})
}

// ---------------------------------------------------------------------------
// WithUnique tests
// ---------------------------------------------------------------------------

func TestWithUnique(t *testing.T) {
	opts := defaultEnqueueOptions()
	if opts.unique {
		t.Fatal("expected unique = false by default")
	}

	WithUnique()(&opts)
	if !opts.unique {
		t.Error("expected unique = true after WithUnique()")
	}
}

// ---------------------------------------------------------------------------
// WithUniqueTTL tests
// ---------------------------------------------------------------------------

func TestWithUniqueTTL(t *testing.T) {
	t.Run("sets unique and positive TTL", func(t *testing.T) {
		opts := defaultEnqueueOptions()
		WithUniqueTTL(30 * time.Second)(&opts)
		if !opts.unique {
			t.Error("expected unique = true")
		}
		if opts.uniqueTTL != 30*time.Second {
			t.Errorf("expected uniqueTTL = 30s, got %v", opts.uniqueTTL)
		}
	})

	t.Run("sets unique but ignores non-positive TTL", func(t *testing.T) {
		opts := defaultEnqueueOptions()
		WithUniqueTTL(0)(&opts)
		if !opts.unique {
			t.Error("expected unique = true")
		}
		if opts.uniqueTTL != 0 {
			t.Errorf("expected uniqueTTL = 0, got %v", opts.uniqueTTL)
		}
	})

	t.Run("sets unique but ignores negative TTL", func(t *testing.T) {
		opts := defaultEnqueueOptions()
		WithUniqueTTL(-5 * time.Second)(&opts)
		if !opts.unique {
			t.Error("expected unique = true")
		}
		if opts.uniqueTTL != 0 {
			t.Errorf("expected uniqueTTL = 0, got %v", opts.uniqueTTL)
		}
	})
}

// ---------------------------------------------------------------------------
// applyTaskDefaults tests
// ---------------------------------------------------------------------------

// TestApplyTaskDefaults verifies the task-level fields are merged correctly
// into the resolved options.
func TestApplyTaskDefaults(t *testing.T) {
	t.Run("all fields set on task", func(t *testing.T) {
		opts := defaultEnqueueOptions()
		task := &Task{
			Queue:     QueueCritical,
			MaxRetry:  3,
			Timeout:   10 * time.Second,
			ID:        "task-42",
			Retention: 5 * time.Minute,
			ProcessIn: 2 * time.Second,
		}
		opts.applyTaskDefaults(task)

		if opts.queue != QueueCritical {
			t.Errorf("expected queue = %q, got %q", QueueCritical, opts.queue)
		}
		if opts.maxRetry != 3 {
			t.Errorf("expected maxRetry = 3, got %d", opts.maxRetry)
		}
		if opts.timeout != 10*time.Second {
			t.Errorf("expected timeout = 10s, got %v", opts.timeout)
		}
		if opts.taskID != "task-42" {
			t.Errorf("expected taskID = %q, got %q", "task-42", opts.taskID)
		}
		if opts.retention != 5*time.Minute {
			t.Errorf("expected retention = 5m, got %v", opts.retention)
		}
		if opts.processIn != 2*time.Second {
			t.Errorf("expected processIn = 2s, got %v", opts.processIn)
		}
	})

	t.Run("empty task does not override defaults", func(t *testing.T) {
		opts := defaultEnqueueOptions()
		task := &Task{}
		opts.applyTaskDefaults(task)

		if opts.queue != QueueDefault {
			t.Errorf("expected queue = %q, got %q", QueueDefault, opts.queue)
		}
		if opts.maxRetry != -1 {
			t.Errorf("expected maxRetry = -1 (sentinel), got %d", opts.maxRetry)
		}
		if opts.timeout != 0 {
			t.Errorf("expected timeout = 0, got %v", opts.timeout)
		}
		if opts.taskID != "" {
			t.Errorf("expected taskID = %q, got %q", "", opts.taskID)
		}
	})

	t.Run("option-set values override task defaults", func(t *testing.T) {
		opts := defaultEnqueueOptions()
		// Simulate: caller already set options via With...
		opts.queue = "custom-queue"
		opts.maxRetry = 10
		opts.taskID = "option-id"

		task := &Task{
			Queue:    QueueLow,
			MaxRetry: 5,
			ID:       "task-id",
		}
		opts.applyTaskDefaults(task)

		// Queue was explicitly set to "custom-queue", not QueueDefault, so
		// task.Queue should NOT override it.
		if opts.queue != "custom-queue" {
			t.Errorf("expected queue = %q (option should win), got %q", "custom-queue", opts.queue)
		}
		// maxRetry was explicitly set to 10 (>=0), so task.MaxRetry should
		// NOT override it.
		if opts.maxRetry != 10 {
			t.Errorf("expected maxRetry = 10 (option should win), got %d", opts.maxRetry)
		}
		// taskID was already set, so task.ID should NOT override.
		if opts.taskID != "option-id" {
			t.Errorf("expected taskID = %q (option should win), got %q", "option-id", opts.taskID)
		}
	})

	t.Run("task Queue does not override non-default queue option", func(t *testing.T) {
		opts := defaultEnqueueOptions()
		opts.queue = QueueCritical
		task := &Task{Queue: QueueLow}
		opts.applyTaskDefaults(task)

		if opts.queue != QueueCritical {
			t.Errorf("expected queue = %q, got %q", QueueCritical, opts.queue)
		}
	})

	t.Run("task MaxRetry zero does not override sentinel", func(t *testing.T) {
		opts := defaultEnqueueOptions()
		task := &Task{MaxRetry: 0}
		opts.applyTaskDefaults(task)

		if opts.maxRetry != -1 {
			t.Errorf("expected maxRetry = -1 (sentinel unchanged), got %d", opts.maxRetry)
		}
	})

	t.Run("nil task is a no-op", func(t *testing.T) {
		opts := defaultEnqueueOptions()
		// Should not panic.
		opts.applyTaskDefaults(nil)
		if opts.queue != QueueDefault {
			t.Errorf("expected queue = %q, got %q", QueueDefault, opts.queue)
		}
	})
}

// ---------------------------------------------------------------------------
// Combined options test
// ---------------------------------------------------------------------------

// TestMultipleOptions verifies that multiple options can be applied in
// sequence and all take effect.
func TestMultipleOptions(t *testing.T) {
	opts := defaultEnqueueOptions()

	applyAll := []EnqueueOption{
		WithQueue(QueueCritical),
		WithMaxRetry(7),
		WithTimeout(15 * time.Second),
		WithTaskID("combo-id"),
		WithRetention(1 * time.Hour),
		WithProcessIn(3 * time.Second),
		WithDeadline(time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)),
		WithUnique(),
	}

	for _, o := range applyAll {
		o(&opts)
	}

	if opts.queue != QueueCritical {
		t.Errorf("expected queue = %q, got %q", QueueCritical, opts.queue)
	}
	if opts.maxRetry != 7 {
		t.Errorf("expected maxRetry = 7, got %d", opts.maxRetry)
	}
	if opts.timeout != 15*time.Second {
		t.Errorf("expected timeout = 15s, got %v", opts.timeout)
	}
	if opts.taskID != "combo-id" {
		t.Errorf("expected taskID = %q, got %q", "combo-id", opts.taskID)
	}
	if opts.retention != 1*time.Hour {
		t.Errorf("expected retention = 1h, got %v", opts.retention)
	}
	if opts.processIn != 3*time.Second {
		t.Errorf("expected processIn = 3s, got %v", opts.processIn)
	}
	if !opts.unique {
		t.Error("expected unique = true")
	}
}
