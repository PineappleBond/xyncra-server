package mq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/hibiken/asynq"

	"github.com/PineappleBond/xyncra-server/internal/tracing"
)

// Compile-time check: AsynqBroker must implement the Broker interface.
var _ Broker = (*AsynqBroker)(nil)

// --------------------------------------------------------------------------
// Configuration
// --------------------------------------------------------------------------

// AsynqConfig holds the configuration for an Asynq-backed Broker.
type AsynqConfig struct {
	// RedisAddr is the Redis server address in "host:port" format.
	RedisAddr string

	// RedisPassword is the password for authenticating with Redis. Leave
	// empty if the Redis instance does not require authentication.
	RedisPassword string

	// RedisDB is the Redis database index to use. Must be >= 0.
	RedisDB int

	// Concurrency is the maximum number of parallel worker goroutines.
	// A value of 0 uses the Asynq default (runtime.NumCPU()).
	Concurrency int

	// Queues maps queue names to their relative priority weights. Higher
	// values are dequeued more frequently. A nil map uses DefaultQueuePriority.
	Queues map[string]int

	// RetryCount is the default maximum number of retries applied to tasks
	// that do not specify their own MaxRetry via EnqueueOption. A value of 0
	// means the broker default (DefaultRetryCount = 3) is used.
	RetryCount int
}

// redisClientOpt builds the asynq.RedisClientOpt from the configuration.
func (c AsynqConfig) redisClientOpt() asynq.RedisClientOpt {
	return asynq.RedisClientOpt{
		Addr:     c.RedisAddr,
		Password: c.RedisPassword,
		DB:       c.RedisDB,
	}
}

// serverConfig builds the asynq.Config from the configuration.
func (c AsynqConfig) serverConfig() asynq.Config {
	queues := c.Queues
	if len(queues) == 0 {
		queues = DefaultQueuePriority()
	}

	return asynq.Config{
		Concurrency: c.Concurrency,
		Queues:      queues,
		Logger:      newAsynqLogger(),
	}
}

// --------------------------------------------------------------------------
// AsynqBroker
// --------------------------------------------------------------------------

// asynqTaskPayload is the JSON envelope stored inside each asynq.Task's
// payload bytes. It carries our domain Task metadata alongside the raw
// application payload.
type asynqTaskPayload struct {
	Type     string            `json:"type"`
	Payload  json.RawMessage   `json:"payload,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// TaskIDFromContext returns the broker-assigned task ID from the handler
// context. It returns an empty string if the context does not carry a task
// ID (e.g. when called outside of a handler). This delegates to
// asynq.GetTaskID which Asynq injects automatically.
func TaskIDFromContext(ctx context.Context) string {
	id, _ := asynq.GetTaskID(ctx)
	return id
}

// AsynqBroker implements the Broker interface using the Asynq library
// (Redis-backed distributed task queue).
type AsynqBroker struct {
	client    *asynq.Client
	server    *asynq.Server
	inspector *asynq.Inspector
	config    AsynqConfig

	mu      sync.Mutex
	running bool
	closed  bool

	// cancel cancels the derived context used by Start to block until
	// shutdown. Calling Stop triggers this cancel, which unblocks Start.
	cancel   context.CancelFunc
	cancelMu sync.Mutex

	// closeOnce ensures Close runs its cleanup at most once.
	closeOnce sync.Once

	// done is closed by Start when it returns, allowing Close to wait for
	// a graceful shutdown to complete.
	done chan struct{}
}

// NewAsynqBroker creates a new Asynq-backed Broker. It establishes the Redis
// connection for the client, server, and inspector.
//
// NOTE: resource creation is not fully atomic — if the client succeeds but
// the server or inspector constructor fails, the already-created client is
// not closed. This is acceptable for now because these constructors do not
// perform network I/O and failures are extremely unlikely.
func NewAsynqBroker(cfg AsynqConfig) (*AsynqBroker, error) {
	if cfg.RedisAddr == "" {
		return nil, fmt.Errorf("mq: redis address is required")
	}
	if cfg.RedisDB < 0 {
		return nil, fmt.Errorf("redis db must be >= 0: %w", ErrInvalidConfig)
	}
	if cfg.Concurrency < 0 {
		return nil, fmt.Errorf("concurrency must be >= 0: %w", ErrInvalidConfig)
	}

	// Apply the broker-level default retry count.
	if cfg.RetryCount == 0 {
		cfg.RetryCount = DefaultRetryCount
	}

	opt := cfg.redisClientOpt()

	client := asynq.NewClient(opt)
	server := asynq.NewServer(opt, cfg.serverConfig())
	inspector := asynq.NewInspector(opt)

	return &AsynqBroker{
		client:    client,
		server:    server,
		inspector: inspector,
		config:    cfg,
	}, nil
}

// Enqueue adds a task to the Redis-backed queue and returns the unique task
// ID assigned by the broker. Variadic EnqueueOption arguments override fields
// set directly on the Task.
func (b *AsynqBroker) Enqueue(ctx context.Context, task *Task, opts ...EnqueueOption) (string, error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return "", ErrQueueClosed
	}
	b.mu.Unlock()

	if task == nil {
		return "", ErrInvalidTask
	}
	if task.Type == "" {
		return "", fmt.Errorf("%w: type is required", ErrInvalidTask)
	}

	// Resolve options.
	resolved := defaultEnqueueOptions()
	resolved.applyTaskDefaults(task)
	for _, opt := range opts {
		opt(&resolved)
	}

	// Apply the broker-level default retry count when neither the task nor
	// the caller specified one.
	if resolved.maxRetry < 0 && b.config.RetryCount > 0 {
		resolved.maxRetry = b.config.RetryCount
	}

	// Marshal the domain task into the asynq payload envelope.
	//
	// NOTE: We wrap the payload in an envelope to carry metadata (type) alongside
	// the raw application payload. A future optimization could use Asynq's task type
	// directly to avoid the extra serialization layer.

	// Inject trace context into metadata for MQ propagation.
	metadata := tracing.InjectTraceContext(ctx)
	if len(metadata) == 0 {
		metadata = nil // omit empty metadata from JSON
	}

	env, err := json.Marshal(asynqTaskPayload{
		Type:     task.Type,
		Payload:  task.Payload,
		Metadata: metadata,
	})
	if err != nil {
		return "", fmt.Errorf("mq: marshal task payload: %w", err)
	}

	aTask := asynq.NewTask(task.Type, env)

	// Build asynq options.
	aOpts := buildAsynqOptions(resolved)

	info, err := b.client.EnqueueContext(ctx, aTask, aOpts...)
	if err != nil {
		return "", fmt.Errorf("mq: enqueue task: %w", err)
	}

	return info.ID, nil
}

// GetTaskState returns the current state of a task by ID. It searches all
// known queues to locate the task.
func (b *AsynqBroker) GetTaskState(ctx context.Context, taskID string) (TaskState, error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return TaskStateUnknown, ErrQueueClosed
	}
	b.mu.Unlock()

	if taskID == "" {
		return TaskStateUnknown, fmt.Errorf("%w: task id is required", ErrInvalidTask)
	}

	queues, err := b.inspector.Queues()
	if err != nil {
		return TaskStateUnknown, fmt.Errorf("mq: list queues: %w", err)
	}

	for _, q := range queues {
		info, err := b.inspector.GetTaskInfo(q, taskID)
		if err != nil {
			// Task not in this queue — continue searching.
			continue
		}
		if info == nil {
			continue
		}
		return mapAsynqTaskState(info.State), nil
	}

	return TaskStateUnknown, ErrTaskNotFound
}

// mapAsynqTaskState converts an asynq.TaskState to our domain TaskState.
func mapAsynqTaskState(s asynq.TaskState) TaskState {
	switch s {
	case asynq.TaskStatePending:
		return TaskStatePending
	case asynq.TaskStateActive:
		return TaskStateActive
	case asynq.TaskStateCompleted:
		return TaskStateCompleted
	case asynq.TaskStateRetry:
		return TaskStateRetry
	case asynq.TaskStateArchived:
		return TaskStateArchived
	case asynq.TaskStateScheduled:
		return TaskStateScheduled
	default:
		return TaskStateUnknown
	}
}

// buildAsynqOptions translates resolved enqueueOptions into asynq.Option values.
func buildAsynqOptions(o enqueueOptions) []asynq.Option {
	var aOpts []asynq.Option

	if o.queue != "" {
		aOpts = append(aOpts, asynq.Queue(o.queue))
	}
	if o.maxRetry >= 0 {
		aOpts = append(aOpts, asynq.MaxRetry(o.maxRetry))
	}
	if o.timeout > 0 {
		aOpts = append(aOpts, asynq.Timeout(o.timeout))
	}
	if o.taskID != "" {
		aOpts = append(aOpts, asynq.TaskID(o.taskID))
	}
	if o.retention > 0 {
		aOpts = append(aOpts, asynq.Retention(o.retention))
	}
	if o.processIn > 0 {
		aOpts = append(aOpts, asynq.ProcessIn(o.processIn))
	}
	if !o.deadline.IsZero() {
		aOpts = append(aOpts, asynq.Deadline(o.deadline))
	}
	if o.unique {
		if o.uniqueTTL > 0 {
			aOpts = append(aOpts, asynq.Unique(o.uniqueTTL))
		} else {
			aOpts = append(aOpts, asynq.Unique(DefaultUniqueTTL))
		}
	}

	return aOpts
}

// Start launches the Asynq worker pool. It blocks until ctx is cancelled or
// Stop is called, or an unrecoverable error occurs. The provided Handler is
// invoked for every dequeued task.
//
// Start may only be called once per AsynqBroker instance. After Start returns
// (either normally or due to an error), the broker's running flag is cleared
// and the done channel is closed, signalling Close that shutdown is complete.
func (b *AsynqBroker) Start(ctx context.Context, handler Handler) error {
	// Check nil handler BEFORE touching running state (#9).
	if handler == nil {
		return fmt.Errorf("mq: handler must not be nil")
	}

	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return fmt.Errorf("mq: server is already running")
	}
	b.done = make(chan struct{})
	b.running = true
	b.mu.Unlock()

	// Wrap the caller's context with a cancel that Stop() can trigger.
	// This coordinates the lifecycle between Start, Stop, and Close.
	b.cancelMu.Lock()
	ctx, b.cancel = context.WithCancel(ctx)
	b.cancelMu.Unlock()

	// Wrap the domain Handler into an asynq.HandlerFunc.
	asynqHandler := asynq.HandlerFunc(func(ctx context.Context, aTask *asynq.Task) error {
		task, err := decodeAsynqTask(aTask)
		if err != nil {
			return fmt.Errorf("mq: decode task: %w", err)
		}

		// Restore trace context from task metadata for distributed tracing.
		if task.Metadata != nil {
			ctx = tracing.ExtractTraceContext(task.Metadata)
		}

		// Create mq.process span for worker-side tracing.
		ctx, processFinish := startMQProcessSpan(ctx, task.Type)
		defer processFinish(nil)

		// Asynq injects the task ID into the context automatically.
		// Handlers can retrieve it via TaskIDFromContext.
		return handler.ProcessTask(ctx, task)
	})

	// Start the server in a non-blocking manner. It returns immediately
	// once all internal goroutines are running.
	if err := b.server.Start(asynqHandler); err != nil {
		b.mu.Lock()
		b.running = false
		b.mu.Unlock()
		close(b.done)
		return fmt.Errorf("mq: server start: %w", err)
	}

	// Block until the context is cancelled (either by the caller or by
	// Stop), then gracefully shut down.
	<-ctx.Done()
	b.server.Shutdown()

	b.mu.Lock()
	b.running = false
	b.mu.Unlock()

	close(b.done)

	return nil
}

// Stop signals the Asynq server to begin graceful shutdown by cancelling
// the internal context. The actual shutdown is performed by Start(), which
// calls asynq.Server.Shutdown() (which does not accept a context). For
// timeout-based shutdown, use Close() which waits for Start() to complete.
func (b *AsynqBroker) Stop() {
	b.cancelMu.Lock()
	defer b.cancelMu.Unlock()
	if b.cancel != nil {
		b.cancel()
	}
}

// Close releases all resources held by the broker: it signals Start to stop,
// waits for Start to finish its graceful shutdown, then closes the client
// connection and the inspector. After Close returns, the broker must not be
// reused. Close is safe to call multiple times; only the first call has
// effect.
func (b *AsynqBroker) Close() error {
	var firstErr error

	b.closeOnce.Do(func() {
		// Mark the broker as closed so Enqueue/GetTaskState return ErrQueueClosed.
		b.mu.Lock()
		b.closed = true
		b.mu.Unlock()

		// Signal Start to stop if it is running.
		b.cancelMu.Lock()
		if b.cancel != nil {
			b.cancel()
		}
		b.cancelMu.Unlock()

		// Wait for Start to finish its graceful shutdown.
		b.mu.Lock()
		isRunning := b.running
		done := b.done
		b.mu.Unlock()

		if isRunning && done != nil {
			<-done
		}

		if err := b.client.Close(); err != nil {
			firstErr = fmt.Errorf("mq: close client: %w", err)
		}
		if err := b.inspector.Close(); err != nil {
			firstErr = errors.Join(firstErr, fmt.Errorf("mq: close inspector: %w", err))
		}
	})

	return firstErr
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// decodeAsynqTask converts an asynq.Task back into a domain Task by
// unmarshalling the payload envelope. It validates that the Type field is
// non-empty, returning ErrInvalidTask if the envelope is malformed.
func decodeAsynqTask(aTask *asynq.Task) (*Task, error) {
	var env asynqTaskPayload
	if err := json.Unmarshal(aTask.Payload(), &env); err != nil {
		return nil, fmt.Errorf("unmarshal payload envelope: %w", err)
	}

	if env.Type == "" {
		return nil, fmt.Errorf("%w: type is required", ErrInvalidTask)
	}

	return &Task{
		Type:     env.Type,
		Payload:  env.Payload,
		Metadata: env.Metadata,
	}, nil
}

// --------------------------------------------------------------------------
// slogAsynqLogger adapts slog to asynq's Logger interface.
// --------------------------------------------------------------------------

// slogAsynqLogger wraps *slog.Logger to satisfy asynq.Logger.
type slogAsynqLogger struct {
	inner *slog.Logger
}

// newAsynqLogger returns a logger suitable for Asynq's internal logging.
func newAsynqLogger() *slogAsynqLogger {
	return &slogAsynqLogger{
		inner: slog.Default().With("component", "asynq"),
	}
}

// Debug logs a message at debug level.
func (l *slogAsynqLogger) Debug(args ...interface{}) {
	l.inner.Debug(fmt.Sprint(args...))
}

// Info logs a message at info level.
func (l *slogAsynqLogger) Info(args ...interface{}) {
	l.inner.Info(fmt.Sprint(args...))
}

// Warn logs a message at warning level.
func (l *slogAsynqLogger) Warn(args ...interface{}) {
	l.inner.Warn(fmt.Sprint(args...))
}

// Error logs a message at error level.
func (l *slogAsynqLogger) Error(args ...interface{}) {
	l.inner.Error(fmt.Sprint(args...))
}

// Fatal logs a message at error level without exiting the process.
// Unlike log.Fatal, this method does NOT call os.Exit(1); it simply
// records the message so that the caller can handle the failure.
func (l *slogAsynqLogger) Fatal(args ...interface{}) {
	l.inner.Error(fmt.Sprint(args...))
}
