package mq

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// TaskHandler manages a registry of handler functions keyed by task type.
// It implements the Handler interface and routes each incoming task to the
// appropriate registered function.
//
// The zero value is not usable; use NewTaskHandler to create an instance.
type TaskHandler struct {
	mu       sync.RWMutex
	handlers map[string]func(ctx context.Context, task *Task) error
}

// NewTaskHandler creates a ready-to-use TaskHandler with an empty handler
// registry.
func NewTaskHandler() *TaskHandler {
	return &TaskHandler{
		handlers: make(map[string]func(ctx context.Context, task *Task) error),
	}
}

// Register adds a handler function for the given task type. If a handler for
// the same type already exists it is replaced and a warning is logged. The
// returned bool is true when the registration was new and false when an
// existing handler was overwritten.
func (th *TaskHandler) Register(taskType string, fn func(ctx context.Context, task *Task) error) bool {
	th.mu.Lock()
	defer th.mu.Unlock()
	_, exists := th.handlers[taskType]
	if exists {
		slog.Warn("mq: overwriting existing handler", "task_type", taskType)
	}
	th.handlers[taskType] = fn
	return !exists // true if this was a new registration, false if overwritten
}

// Unregister removes the handler for the given task type. It is a no-op if
// no handler was registered.
func (th *TaskHandler) Unregister(taskType string) {
	th.mu.Lock()
	defer th.mu.Unlock()
	delete(th.handlers, taskType)
}

// ProcessTask dispatches the task to the registered handler for its type. It
// returns ErrHandlerNotRegistered if no handler exists for task.Type.
func (th *TaskHandler) ProcessTask(ctx context.Context, task *Task) error {
	if task == nil {
		return ErrInvalidTask
	}

	th.mu.RLock()
	fn, ok := th.handlers[task.Type]
	th.mu.RUnlock()

	if !ok {
		return fmt.Errorf("%w: %s", ErrHandlerNotRegistered, task.Type)
	}

	return fn(ctx, task)
}

// HasHandler reports whether a handler is registered for the given task type.
func (th *TaskHandler) HasHandler(taskType string) bool {
	th.mu.RLock()
	defer th.mu.RUnlock()
	_, ok := th.handlers[taskType]
	return ok
}

// RegisteredTypes returns a copy of all currently registered task types.
func (th *TaskHandler) RegisteredTypes() []string {
	th.mu.RLock()
	defer th.mu.RUnlock()
	types := make([]string, 0, len(th.handlers))
	for t := range th.handlers {
		types = append(types, t)
	}
	return types
}
