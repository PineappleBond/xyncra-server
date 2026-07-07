package mq

import (
	"context"
	"errors"
	"testing"
)

// ---------------------------------------------------------------------------
// TaskState.String() tests
// ---------------------------------------------------------------------------

// TestTaskState_String_AllStates verifies every TaskState constant maps to the
// expected human-readable string, including the zero value and out-of-range
// values.
func TestTaskState_String_AllStates(t *testing.T) {
	tests := []struct {
		name string
		s    TaskState
		want string
	}{
		{"unknown (zero value)", TaskStateUnknown, "unknown"},
		{"pending", TaskStatePending, "pending"},
		{"active", TaskStateActive, "active"},
		{"completed", TaskStateCompleted, "completed"},
		{"retry", TaskStateRetry, "retry"},
		{"archived", TaskStateArchived, "archived"},
		{"scheduled", TaskStateScheduled, "scheduled"},
		{"out of range negative", TaskState(-1), "unknown"},
		{"out of range high", TaskState(100), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.s.String()
			if got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// HandlerFunc adapter tests
// ---------------------------------------------------------------------------

// TestHandlerFunc_ProcessTask_CallsUnderlying verifies the adapter correctly
// delegates to the wrapped function and returns its result.
func TestHandlerFunc_ProcessTask_CallsUnderlying(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		called := false
		var receivedCtx context.Context
		var receivedTask *Task

		fn := HandlerFunc(func(ctx context.Context, task *Task) error {
			called = true
			receivedCtx = ctx
			receivedTask = task
			return nil
		})

		ctx := context.Background()
		task := &Task{Type: "test:task", Payload: []byte(`{"key":"val"}`)}

		err := fn.ProcessTask(ctx, task)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if !called {
			t.Fatal("expected underlying function to be called")
		}
		if receivedCtx != ctx {
			t.Error("expected context to be forwarded")
		}
		if receivedTask != task {
			t.Error("expected task to be forwarded")
		}
	})

	t.Run("error propagation", func(t *testing.T) {
		expectedErr := errors.New("handler error")
		fn := HandlerFunc(func(ctx context.Context, task *Task) error {
			return expectedErr
		})

		err := fn.ProcessTask(context.Background(), &Task{Type: "test"})
		if !errors.Is(err, expectedErr) {
			t.Errorf("expected error %v, got %v", expectedErr, err)
		}
	})
}

// TestHandlerFunc_ImplementsHandler verifies that HandlerFunc satisfies the
// Handler interface at compile time.
func TestHandlerFunc_ImplementsHandler(t *testing.T) {
	var _ Handler = HandlerFunc(func(ctx context.Context, task *Task) error {
		return nil
	})
}

// ---------------------------------------------------------------------------
// Sentinel error tests
// ---------------------------------------------------------------------------

// TestSentinelErrors verifies all sentinel errors are non-nil and carry the
// expected messages.
func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"ErrQueueClosed", ErrQueueClosed, "mq: queue is closed"},
		{"ErrTaskTimeout", ErrTaskTimeout, "mq: task processing timed out"},
		{"ErrTaskNotFound", ErrTaskNotFound, "mq: task not found"},
		{"ErrHandlerNotRegistered", ErrHandlerNotRegistered, "mq: handler not registered for task type"},
		{"ErrInvalidTask", ErrInvalidTask, "mq: invalid task"},
		{"ErrInvalidConfig", ErrInvalidConfig, "mq: invalid config"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil {
				t.Fatal("expected non-nil error")
			}
			if tt.err.Error() != tt.want {
				t.Errorf("expected %q, got %q", tt.want, tt.err.Error())
			}
		})
	}
}

// TestSentinelErrors_Unwrap verifies wrapped sentinel errors can be detected
// with errors.Is.
func TestSentinelErrors_Unwrap(t *testing.T) {
	wrapped := errors.Join(errors.New("outer"), ErrInvalidTask)
	if !errors.Is(wrapped, ErrInvalidTask) {
		t.Error("expected errors.Is to find ErrInvalidTask in joined error")
	}
}

// ---------------------------------------------------------------------------
// Constants tests
// ---------------------------------------------------------------------------

// TestQueueConstants verifies the queue name constants have expected values.
func TestQueueConstants(t *testing.T) {
	if QueueCritical != "critical" {
		t.Errorf("expected QueueCritical = %q, got %q", "critical", QueueCritical)
	}
	if QueueDefault != "default" {
		t.Errorf("expected QueueDefault = %q, got %q", "default", QueueDefault)
	}
	if QueueLow != "low" {
		t.Errorf("expected QueueLow = %q, got %q", "low", QueueLow)
	}
}

// TestTaskTypeConstants verifies well-known task type strings.
func TestTaskTypeConstants(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"TypeSendMessage", TypeSendMessage, "mq:send_message"},
		{"TypeSyncUpdates", TypeSyncUpdates, "mq:sync_updates"},
		{"TypePushNotification", TypePushNotification, "mq:push_notification"},
		{"TypePresenceBroadcast", TypePresenceBroadcast, "mq:presence_broadcast"},
		{"TypeConversationSync", TypeConversationSync, "mq:conversation_sync"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, tt.got)
			}
		})
	}
}

// TestDefaultQueuePriority verifies priority weights are ordered correctly.
func TestDefaultQueuePriority(t *testing.T) {
	p := DefaultQueuePriority()
	if p[QueueCritical] <= p[QueueDefault] {
		t.Error("expected critical priority > default priority")
	}
	if p[QueueDefault] <= p[QueueLow] {
		t.Error("expected default priority > low priority")
	}
}
