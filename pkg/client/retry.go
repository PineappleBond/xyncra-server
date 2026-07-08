package client

import (
	"context"
	"encoding/json"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/store"
	"github.com/PineappleBond/xyncra-server/pkg/store/model"
	"github.com/google/uuid"
)

// retryManager manages the retry queue for failed RPC calls.
type retryManager struct {
	db           *store.ClientDB
	rpcFn        func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)
	baseDelay    time.Duration
	maxAttempts  int
	pollInterval time.Duration
	logger       Logger
	ctx          context.Context
	cancel       context.CancelFunc
	done         chan struct{}
}

// newRetryManager creates a new retry manager instance.
func newRetryManager(db *store.ClientDB, rpcFn func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error), baseDelay time.Duration, maxAttempts int, pollInterval time.Duration, logger Logger) *retryManager {
	return &retryManager{
		db:           db,
		rpcFn:        rpcFn,
		baseDelay:    baseDelay,
		maxAttempts:  maxAttempts,
		pollInterval: pollInterval,
		logger:       logger,
		done:         make(chan struct{}),
	}
}

// Start begins the retry polling loop in a background goroutine.
func (rm *retryManager) Start(ctx context.Context) {
	rm.ctx, rm.cancel = context.WithCancel(ctx)
	go func() {
		defer close(rm.done)
		rm.pollLoop()
	}()
}

// Stop cancels the retry loop and waits for it to finish with a 5s timeout.
func (rm *retryManager) Stop() {
	if rm.cancel != nil {
		rm.cancel()
	}
	select {
	case <-rm.done:
	case <-time.After(5 * time.Second):
		rm.logger.Error("retry manager stop timeout")
	}
}

// Enqueue adds a failed RPC call to the retry queue.
func (rm *retryManager) Enqueue(ctx context.Context, method string, params json.RawMessage) error {
	task := &model.RetryTask{
		ID:          uuid.New().String(),
		Method:      method,
		Params:      []byte(params),
		Attempt:     0,
		MaxAttempts: rm.maxAttempts,
		NextRetry:   time.Now(),
		Status:      "pending",
	}
	return rm.db.Queue.Save(ctx, task)
}

// pollLoop continuously checks for pending retry tasks at the configured interval.
func (rm *retryManager) pollLoop() {
	ticker := time.NewTicker(rm.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rm.ctx.Done():
			return
		case <-ticker.C:
			tasks, err := rm.db.Queue.ListPending(rm.ctx, 50)
			if err != nil {
				rm.logger.Error("list pending tasks", "error", err)
				continue
			}
			for _, task := range tasks {
				if rm.ctx.Err() != nil {
					return
				}
				rm.executeTask(rm.ctx, task)
			}
		}
	}
}

// executeTask attempts to retry a single task and updates its state based on the result.
func (rm *retryManager) executeTask(ctx context.Context, task *model.RetryTask) {
	task.Attempt++

	// Execute RPC
	_, err := rm.rpcFn(ctx, task.Method, json.RawMessage(task.Params))
	if err == nil {
		// Success: delete task
		if delErr := rm.db.Queue.Delete(ctx, task.ID); delErr != nil {
			rm.logger.Error("delete completed task", "error", delErr)
		}
		return
	}

	// Failed: update attempt count
	task.LastError = err.Error()

	if task.Attempt >= task.MaxAttempts {
		// Max attempts reached: mark as failed permanently
		task.Status = "failed"
		if updateErr := rm.db.Queue.Update(ctx, task); updateErr != nil {
			rm.logger.Error("mark task failed", "error", updateErr)
		}
		return
	}

	// Calculate next retry time with exponential backoff
	delay := backoffDelay(task.Attempt, rm.baseDelay, 30*time.Second)
	task.NextRetry = time.Now().Add(delay)

	if updateErr := rm.db.Queue.Update(ctx, task); updateErr != nil {
		rm.logger.Error("update task retry", "error", updateErr)
	}
}
