// Package mq provides the message queue abstraction layer for the Xyncra
// messaging system. It defines a broker interface for asynchronous task
// processing and ships with an implementation backed by Asynq (Redis).
package mq

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// --------------------------------------------------------------------------
// Queue name constants
// --------------------------------------------------------------------------

// Standard queue names used for task priority routing.
const (
	// QueueCritical is the highest-priority queue for time-sensitive tasks
	// such as message delivery notifications.
	QueueCritical = "critical"

	// QueueDefault is the standard-priority queue for ordinary tasks.
	QueueDefault = "default"

	// QueueLow is the lowest-priority queue for background and batch tasks
	// such as analytics aggregation and cleanup jobs.
	QueueLow = "low"
)

// --------------------------------------------------------------------------
// Default constants
// --------------------------------------------------------------------------

// DefaultRetryCount is the default maximum number of retries applied to tasks
// that do not specify their own MaxRetry. This replaces Asynq's built-in
// default of 25.
const DefaultRetryCount = 3

// DefaultUniqueTTL is the default TTL for the deduplication lock when
// WithUnique is used without an explicit TTL.
const DefaultUniqueTTL = 5 * time.Minute

// retryUseBrokerDefault is the sentinel value for enqueueOptions.maxRetry
// indicating that the broker-level default should be used.
const retryUseBrokerDefault = -1

// DefaultQueuePriority returns a new map of default queue priority weights.
// Higher values are processed more frequently. A fresh copy is returned on
// every call to prevent callers from mutating shared state.
func DefaultQueuePriority() map[string]int {
	return map[string]int{
		QueueCritical: 6,
		QueueDefault:  3,
		QueueLow:      1,
	}
}

// --------------------------------------------------------------------------
// Task type constants
// --------------------------------------------------------------------------

// Well-known task types used by the Xyncra messaging system.
const (
	// TypeSendMessage is the task type for delivering a message to recipients.
	TypeSendMessage = "mq:send_message"

	// TypeSyncUpdates is the task type for fanning out user updates.
	TypeSyncUpdates = "mq:sync_updates"

	// TypePushNotification is the task type for sending push notifications.
	TypePushNotification = "mq:push_notification"

	// TypePresenceBroadcast is the task type for broadcasting presence changes.
	TypePresenceBroadcast = "mq:presence_broadcast"

	// TypeConversationSync is the task type for syncing conversation state.
	TypeConversationSync = "mq:conversation_sync"

	// TypeAgentProcess is the task type for triggering agent processing
	// of a message sent to an agent user.
	TypeAgentProcess = "mq:agent_process"

	// TypeAgentResume is the task type for resuming a paused agent after
	// HITL interrupt (Phase 8B / D-085).
	TypeAgentResume = "mq:agent_resume"
)

// --------------------------------------------------------------------------
// Task state constants
// --------------------------------------------------------------------------

// TaskState represents the lifecycle state of a task.
type TaskState int

const (
	// TaskStateUnknown indicates the task state could not be determined.
	TaskStateUnknown TaskState = iota

	// TaskStatePending indicates the task is waiting to be processed.
	TaskStatePending

	// TaskStateActive indicates the task is currently being processed.
	TaskStateActive

	// TaskStateCompleted indicates the task finished successfully.
	TaskStateCompleted

	// TaskStateRetry indicates the task failed and is awaiting retry.
	TaskStateRetry

	// TaskStateArchived indicates the task exhausted all retries.
	TaskStateArchived

	// TaskStateScheduled indicates the task is scheduled for future processing.
	TaskStateScheduled
)

// String returns a human-readable representation of the task state.
func (s TaskState) String() string {
	switch s {
	case TaskStatePending:
		return "pending"
	case TaskStateActive:
		return "active"
	case TaskStateCompleted:
		return "completed"
	case TaskStateRetry:
		return "retry"
	case TaskStateArchived:
		return "archived"
	case TaskStateScheduled:
		return "scheduled"
	default:
		return "unknown"
	}
}

// --------------------------------------------------------------------------
// Sentinel errors
// --------------------------------------------------------------------------

// Standard errors returned by the mq package.
var (
	// ErrQueueClosed indicates the broker has been shut down and no longer
	// accepts tasks.
	ErrQueueClosed = errors.New("mq: queue is closed")

	// ErrTaskTimeout indicates the task exceeded its processing deadline.
	ErrTaskTimeout = errors.New("mq: task processing timed out")

	// ErrTaskNotFound indicates the requested task does not exist.
	ErrTaskNotFound = errors.New("mq: task not found")

	// ErrHandlerNotRegistered indicates no handler is registered for the
	// given task type.
	ErrHandlerNotRegistered = errors.New("mq: handler not registered for task type")

	// ErrInvalidTask indicates the task is malformed or missing required fields.
	ErrInvalidTask = errors.New("mq: invalid task")

	// ErrInvalidConfig indicates the broker configuration is invalid.
	// Use errors.Is to check for this class of error; the wrapped message
	// describes the specific field that failed validation.
	ErrInvalidConfig = errors.New("mq: invalid config")
)

// --------------------------------------------------------------------------
// Core types
// --------------------------------------------------------------------------

// Task represents an asynchronous unit of work to be processed by a worker.
type Task struct {
	// Type identifies the kind of work to perform (e.g. "mq:send_message").
	// It is used by the broker to route the task to the correct handler.
	Type string

	// Payload is the task-specific data encoded as JSON.
	Payload json.RawMessage

	// ID is an optional caller-supplied identifier. When set, the broker uses
	// it as the task's unique ID; when empty the broker generates one.
	ID string

	// Queue is the target queue name. When empty the broker uses QueueDefault.
	Queue string

	// MaxRetry is the maximum number of times the task will be retried on
	// failure. A value of 0 means the broker's default is used.
	MaxRetry int

	// Timeout is the maximum duration allowed for a single processing attempt.
	// A value of 0 means the broker's default is used.
	Timeout time.Duration

	// Retention is how long the task is kept in the queue after successful
	// completion. A value of 0 means the broker's default is used.
	Retention time.Duration

	// ProcessIn delays task processing by the given duration after enqueue.
	// A value of 0 means immediate processing.
	ProcessIn time.Duration
}

// Broker is the core message queue interface. Implementations manage the
// lifecycle of task producers and consumers.
type Broker interface {
	// Enqueue adds a task to the queue for asynchronous processing. It
	// returns the unique task ID assigned by the broker. Options passed as
	// variadic arguments override fields set directly on the Task.
	Enqueue(ctx context.Context, task *Task, opts ...EnqueueOption) (string, error)

	// Start launches the worker pool and blocks until ctx is cancelled or
	// an unrecoverable error occurs. The provided Handler is invoked for
	// every dequeued task.
	Start(ctx context.Context, handler Handler) error

	// Stop performs a graceful shutdown of the worker pool, allowing
	// in-flight tasks to complete before returning.
	Stop()

	// GetTaskState returns the current state of a task by ID.
	GetTaskState(ctx context.Context, taskID string) (TaskState, error)
}

// Handler processes tasks dequeued by the Broker.
type Handler interface {
	// ProcessTask handles a single task. Returning a non-nil error signals
	// the broker that the task should be retried according to its retry
	// policy.
	ProcessTask(ctx context.Context, task *Task) error
}

// HandlerFunc is an adapter that allows ordinary functions to be used as
// Handler implementations.
type HandlerFunc func(ctx context.Context, task *Task) error

// ProcessTask calls f(ctx, task).
func (f HandlerFunc) ProcessTask(ctx context.Context, task *Task) error {
	return f(ctx, task)
}
