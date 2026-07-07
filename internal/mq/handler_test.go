package mq

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// NewTaskHandler tests
// ---------------------------------------------------------------------------

// TestNewTaskHandler_EmptyRegistry verifies the constructor returns a handler
// with an empty registry that is immediately usable.
func TestNewTaskHandler_EmptyRegistry(t *testing.T) {
	t.Parallel()

	th := NewTaskHandler()
	require.NotNil(t, th, "expected non-nil TaskHandler")

	types := th.RegisteredTypes()
	assert.Empty(t, types, "expected 0 registered types")
}

// TestNewTaskHandler_InstancesAreIndependent verifies that multiple instances
// do not share state.
func TestNewTaskHandler_InstancesAreIndependent(t *testing.T) {
	t.Parallel()

	th1 := NewTaskHandler()
	th2 := NewTaskHandler()

	th1.Register("type:a", func(ctx context.Context, task *Task) error { return nil })

	assert.True(t, th1.HasHandler("type:a"), "th1 should have handler")
	assert.False(t, th2.HasHandler("type:a"), "th2 should not have handler registered in th1")
}

// ---------------------------------------------------------------------------
// Register tests
// ---------------------------------------------------------------------------

// TestRegister_NewHandler verifies that a handler can be registered and
// detected via HasHandler.
func TestRegister_NewHandler(t *testing.T) {
	t.Parallel()

	th := NewTaskHandler()
	handler := func(ctx context.Context, task *Task) error { return nil }

	th.Register("test:type", handler)

	assert.True(t, th.HasHandler("test:type"), "expected HasHandler to return true after Register")
}

// TestRegister_OverwritesExistingHandler verifies that registering a handler
// for an already-registered type silently replaces the previous handler.
func TestRegister_OverwritesExistingHandler(t *testing.T) {
	t.Parallel()

	th := NewTaskHandler()

	callCount := 0
	firstHandler := func(ctx context.Context, task *Task) error {
		callCount++
		return nil
	}
	secondHandler := func(ctx context.Context, task *Task) error {
		callCount += 10
		return nil
	}

	th.Register("test:type", firstHandler)
	th.Register("test:type", secondHandler)

	err := th.ProcessTask(context.Background(), &Task{Type: "test:type"})
	require.NoError(t, err, "ProcessTask should not error")

	// The second handler should have been called (adds 10, not 1).
	assert.Equal(t, 10, callCount, "expected second handler to be called")
}

// TestRegister_MultipleDistinctTypes verifies that multiple distinct types can
// be registered independently.
func TestRegister_MultipleDistinctTypes(t *testing.T) {
	t.Parallel()

	th := NewTaskHandler()
	handler := func(ctx context.Context, task *Task) error { return nil }

	th.Register("type:a", handler)
	th.Register("type:b", handler)
	th.Register("type:c", handler)

	assert.True(t, th.HasHandler("type:a"), "expected type:a to be registered")
	assert.True(t, th.HasHandler("type:b"), "expected type:b to be registered")
	assert.True(t, th.HasHandler("type:c"), "expected type:c to be registered")
}

// ---------------------------------------------------------------------------
// Unregister tests
// ---------------------------------------------------------------------------

// TestUnregister_RemovesRegisteredHandler verifies that a registered handler
// can be removed.
func TestUnregister_RemovesRegisteredHandler(t *testing.T) {
	t.Parallel()

	th := NewTaskHandler()
	th.Register("test:type", func(ctx context.Context, task *Task) error { return nil })

	require.True(t, th.HasHandler("test:type"), "handler should be registered before Unregister")

	th.Unregister("test:type")

	assert.False(t, th.HasHandler("test:type"), "expected HasHandler to return false after Unregister")
}

// TestUnregister_NoOpForUnregisteredType verifies that Unregister is a no-op
// when the type was never registered.
func TestUnregister_NoOpForUnregisteredType(t *testing.T) {
	t.Parallel()

	th := NewTaskHandler()
	// Should not panic.
	th.Unregister("nonexistent:type")

	assert.False(t, th.HasHandler("nonexistent:type"), "expected false for never-registered type")
}

// TestUnregister_OnlyRemovesSpecifiedType verifies that Unregister removes only
// the specified type and leaves others intact.
func TestUnregister_OnlyRemovesSpecifiedType(t *testing.T) {
	t.Parallel()

	th := NewTaskHandler()
	handler := func(ctx context.Context, task *Task) error { return nil }
	th.Register("type:a", handler)
	th.Register("type:b", handler)

	th.Unregister("type:a")

	assert.False(t, th.HasHandler("type:a"), "expected type:a to be removed")
	assert.True(t, th.HasHandler("type:b"), "expected type:b to still be registered")
}

// ---------------------------------------------------------------------------
// ProcessTask routing tests
// ---------------------------------------------------------------------------

// TestProcessTask_RoutesToCorrectHandler verifies that ProcessTask dispatches
// the task to the correct registered handler based on task type.
func TestProcessTask_RoutesToCorrectHandler(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("routes to handler A", func(t *testing.T) {
		t.Parallel()
		var got string
		localHandler := NewTaskHandler()
		localHandler.Register("route:a", func(ctx context.Context, task *Task) error {
			got = task.Type
			return nil
		})
		err := localHandler.ProcessTask(ctx, &Task{Type: "route:a"})
		require.NoError(t, err)
		assert.Equal(t, "route:a", got)
	})

	t.Run("routes to handler B", func(t *testing.T) {
		t.Parallel()
		var got string
		localHandler := NewTaskHandler()
		localHandler.Register("route:b", func(ctx context.Context, task *Task) error {
			got = task.Type + "-other"
			return nil
		})
		err := localHandler.ProcessTask(ctx, &Task{Type: "route:b"})
		require.NoError(t, err)
		assert.Equal(t, "route:b-other", got)
	})
}

// TestProcessTask_ForwardsContextAndTask verifies that ProcessTask passes the
// context and task through to the handler unchanged.
func TestProcessTask_ForwardsContextAndTask(t *testing.T) {
	t.Parallel()

	th := NewTaskHandler()

	var receivedCtx context.Context
	var receivedTask *Task
	th.Register("check:forward", func(ctx context.Context, task *Task) error {
		receivedCtx = ctx
		receivedTask = task
		return nil
	})

	type ctxKey struct{}
	ctxWithValue := context.WithValue(context.Background(), ctxKey{}, "testval")
	task := &Task{Type: "check:forward", Payload: []byte(`{"hello":"world"}`)}

	err := th.ProcessTask(ctxWithValue, task)
	require.NoError(t, err)
	assert.Equal(t, ctxWithValue, receivedCtx, "expected context to be forwarded")
	assert.Equal(t, task, receivedTask, "expected task to be forwarded")
}

// ---------------------------------------------------------------------------
// ProcessTask error tests
// ---------------------------------------------------------------------------

// TestProcessTask_NilTask_ReturnsErrInvalidTask verifies that ProcessTask
// returns ErrInvalidTask when given a nil task.
func TestProcessTask_NilTask_ReturnsErrInvalidTask(t *testing.T) {
	t.Parallel()

	th := NewTaskHandler()
	th.Register("some:type", func(ctx context.Context, task *Task) error { return nil })

	err := th.ProcessTask(context.Background(), nil)
	assert.ErrorIs(t, err, ErrInvalidTask, "expected ErrInvalidTask")
}

// TestProcessTask_UnregisteredType_ReturnsErrHandlerNotRegistered verifies that
// ProcessTask returns ErrHandlerNotRegistered and that the error message
// includes the task type name.
func TestProcessTask_UnregisteredType_ReturnsErrHandlerNotRegistered(t *testing.T) {
	t.Parallel()

	th := NewTaskHandler()

	err := th.ProcessTask(context.Background(), &Task{Type: "unknown:type"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrHandlerNotRegistered)
	assert.Contains(t, err.Error(), "unknown:type", "error should contain task type name")
}

// TestProcessTask_HandlerErrorPropagation verifies that errors returned by the
// handler are propagated to the caller.
func TestProcessTask_HandlerErrorPropagation(t *testing.T) {
	t.Parallel()

	th := NewTaskHandler()
	expectedErr := errors.New("handler processing failed")
	th.Register("fail:type", func(ctx context.Context, task *Task) error {
		return expectedErr
	})

	err := th.ProcessTask(context.Background(), &Task{Type: "fail:type"})
	assert.ErrorIs(t, err, expectedErr, "expected handler error to propagate")
}

// ---------------------------------------------------------------------------
// HasHandler tests
// ---------------------------------------------------------------------------

// TestHasHandler verifies the HasHandler method for various registration states.
func TestHasHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		setup    func(th *TaskHandler)
		taskType string
		want     bool
	}{
		{
			name:     "registered type returns true",
			setup:    func(th *TaskHandler) { th.Register("exists", noopHandler) },
			taskType: "exists",
			want:     true,
		},
		{
			name:     "unregistered type returns false",
			setup:    func(th *TaskHandler) {},
			taskType: "missing",
			want:     false,
		},
		{
			name:     "unregistered after unregister returns false",
			setup:    func(th *TaskHandler) { th.Register("temp", noopHandler); th.Unregister("temp") },
			taskType: "temp",
			want:     false,
		},
		{
			name:     "empty string type",
			setup:    func(th *TaskHandler) {},
			taskType: "",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			th := NewTaskHandler()
			tt.setup(th)
			got := th.HasHandler(tt.taskType)
			assert.Equal(t, tt.want, got, "HasHandler(%q)", tt.taskType)
		})
	}
}

// ---------------------------------------------------------------------------
// RegisteredTypes tests
// ---------------------------------------------------------------------------

// TestRegisteredTypes_EmptyRegistry verifies that an empty registry returns an
// empty slice.
func TestRegisteredTypes_EmptyRegistry(t *testing.T) {
	t.Parallel()

	th := NewTaskHandler()
	types := th.RegisteredTypes()
	assert.Empty(t, types, "expected 0 types")
}

// TestRegisteredTypes_ReturnsAllTypes verifies that RegisteredTypes returns all
// registered type names.
func TestRegisteredTypes_ReturnsAllTypes(t *testing.T) {
	t.Parallel()

	th := NewTaskHandler()
	th.Register("type:a", noopHandler)
	th.Register("type:b", noopHandler)
	th.Register("type:c", noopHandler)

	types := th.RegisteredTypes()
	require.Len(t, types, 3, "expected 3 types")

	sort.Strings(types)
	assert.Equal(t, []string{"type:a", "type:b", "type:c"}, types)
}

// TestRegisteredTypes_ReflectsUnregistrations verifies that the returned list
// reflects types that have been unregistered.
func TestRegisteredTypes_ReflectsUnregistrations(t *testing.T) {
	t.Parallel()

	th := NewTaskHandler()
	th.Register("type:x", noopHandler)
	th.Register("type:y", noopHandler)
	th.Unregister("type:x")

	types := th.RegisteredTypes()
	require.Len(t, types, 1)
	assert.Equal(t, "type:y", types[0])
}

// TestRegisteredTypes_ReturnsACopy verifies that the returned slice is a copy
// and does not affect internal state when mutated.
func TestRegisteredTypes_ReturnsACopy(t *testing.T) {
	t.Parallel()

	th := NewTaskHandler()
	th.Register("type:z", noopHandler)

	types1 := th.RegisteredTypes()
	types1[0] = "mutated" // modify the returned slice

	types2 := th.RegisteredTypes()
	assert.Equal(t, "type:z", types2[0], "internal state should not be mutated")
}

// ---------------------------------------------------------------------------
// Concurrency tests
// ---------------------------------------------------------------------------

// TestTaskHandler_ConcurrentAccess verifies that concurrent Register and
// ProcessTask calls do not cause data races or panics.
func TestTaskHandler_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	th := NewTaskHandler()
	const goroutines = 50
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 3) // register, process, hasHandler goroutines

	// Concurrent registrations.
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				taskType := fmt.Sprintf("concurrent:type:%d:%d", id, i)
				th.Register(taskType, noopHandler)
			}
		}(g)
	}

	// Concurrent ProcessTask calls.
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				taskType := fmt.Sprintf("concurrent:type:%d:%d", id, i)
				_ = th.ProcessTask(context.Background(), &Task{Type: taskType})
			}
		}(g)
	}

	// Concurrent HasHandler calls.
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				taskType := fmt.Sprintf("concurrent:type:%d:%d", id, i)
				_ = th.HasHandler(taskType)
			}
		}(g)
	}

	wg.Wait()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// noopHandler is a handler that does nothing and returns nil.
func noopHandler(ctx context.Context, task *Task) error {
	return nil
}
