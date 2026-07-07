package mq

import "time"

// enqueueOptions holds the resolved options for an Enqueue call.
type enqueueOptions struct {
	queue     string
	maxRetry  int
	timeout   time.Duration
	taskID    string
	retention time.Duration
	processIn time.Duration
	deadline  time.Time
	unique    bool
	uniqueTTL time.Duration
}

// defaultEnqueueOptions returns the baseline enqueue options used before any
// With... option is applied.
func defaultEnqueueOptions() enqueueOptions {
	return enqueueOptions{
		queue:    QueueDefault,
		maxRetry: retryUseBrokerDefault, // sentinel: use broker default
	}
}

// apply merges the task-level fields into the enqueue options. Fields that
// are explicitly set on the Task take effect as defaults; per-call options
// (passed via EnqueueOption) override them.
//
// Design note: the Queue comparison intentionally treats the default queue
// name as "unset" — a Task that specifies QueueDefault is indistinguishable
// from one that left Queue empty, so an explicit WithQueue("custom") option
// always wins. This keeps the precedence rules simple: option > task field >
// broker default.
func (o *enqueueOptions) applyTaskDefaults(task *Task) {
	if task == nil {
		return
	}
	// Only override the queue when the resolved value is still the broker
	// default; an explicit WithQueue already took precedence.
	if task.Queue != "" && o.queue == QueueDefault {
		o.queue = task.Queue
	}
	if task.MaxRetry > 0 && o.maxRetry < 0 {
		o.maxRetry = task.MaxRetry
	}
	if task.Timeout > 0 && o.timeout == 0 {
		o.timeout = task.Timeout
	}
	if task.ID != "" && o.taskID == "" {
		o.taskID = task.ID
	}
	if task.Retention > 0 && o.retention == 0 {
		o.retention = task.Retention
	}
	if task.ProcessIn > 0 && o.processIn == 0 {
		o.processIn = task.ProcessIn
	}
}

// --------------------------------------------------------------------------
// Functional options
// --------------------------------------------------------------------------

// EnqueueOption configures the behaviour of a single Enqueue call.
type EnqueueOption func(*enqueueOptions)

// WithQueue overrides the target queue for the enqueued task.
func WithQueue(name string) EnqueueOption {
	return func(o *enqueueOptions) {
		if name != "" {
			o.queue = name
		}
	}
}

// WithMaxRetry overrides the maximum number of retries for the enqueued task.
// A value of 0 disables retries entirely.
func WithMaxRetry(n int) EnqueueOption {
	return func(o *enqueueOptions) {
		if n >= 0 {
			o.maxRetry = n
		}
	}
}

// WithTimeout overrides the per-attempt processing timeout for the enqueued
// task.
func WithTimeout(d time.Duration) EnqueueOption {
	return func(o *enqueueOptions) {
		if d > 0 {
			o.timeout = d
		}
	}
}

// WithTaskID sets an explicit task ID. If a task with the same ID is already
// pending, the broker may reject the enqueue with a duplicate-key error.
func WithTaskID(id string) EnqueueOption {
	return func(o *enqueueOptions) {
		if id != "" {
			o.taskID = id
		}
	}
}

// WithRetention sets the duration for which the completed task is retained in
// the queue after successful processing.
func WithRetention(d time.Duration) EnqueueOption {
	return func(o *enqueueOptions) {
		if d > 0 {
			o.retention = d
		}
	}
}

// WithProcessIn delays the task so it is not picked up until the given
// duration has elapsed.
func WithProcessIn(d time.Duration) EnqueueOption {
	return func(o *enqueueOptions) {
		if d > 0 {
			o.processIn = d
		}
	}
}

// WithDeadline sets an absolute deadline for task completion. The task will
// be abandoned if not processed before this time. A zero-value time.Time is
// ignored, preserving whatever deadline was previously set.
func WithDeadline(t time.Time) EnqueueOption {
	return func(o *enqueueOptions) {
		if !t.IsZero() {
			o.deadline = t
		}
	}
}

// WithUnique deduplicates the task: if an identical task is already pending
// in the queue, the new enqueue is rejected.
func WithUnique() EnqueueOption {
	return func(o *enqueueOptions) {
		o.unique = true
	}
}

// WithUniqueTTL is like WithUnique but also sets the TTL for the
// deduplication lock.
func WithUniqueTTL(ttl time.Duration) EnqueueOption {
	return func(o *enqueueOptions) {
		o.unique = true
		if ttl > 0 {
			o.uniqueTTL = ttl
		}
	}
}
