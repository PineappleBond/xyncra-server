package client

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/PineappleBond/xyncra-server/pkg/store"
	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

// ---------------------------------------------------------------------------
// RequestHandlerFunc
// ---------------------------------------------------------------------------

// RequestHandlerFunc processes a server-initiated request and returns response data (D-092).
type RequestHandlerFunc func(ctx context.Context, req *protocol.PackageDataRequest) (json.RawMessage, error)

// ---------------------------------------------------------------------------
// XyncraClient
// ---------------------------------------------------------------------------

// XyncraClient is the high-level entry point for the xyncra-client library.
// It manages a WebSocket connection to a Xyncra server, synchronises data via
// the sync pipeline, retries failed RPCs, and exposes typed convenience methods
// for the supported RPC verbs.
type XyncraClient struct {
	opts     clientOptions
	db       *store.ClientDB
	connMgr  *connectionManager
	syncMgr  *syncManager
	retryMgr *retryManager

	// Phase 8: client-side enhancements.
	idempotencyCache *IdempotencyCache
	rttTracker       *RTTTracker
	respRetryQueue   *ResponseRetryQueue

	// lastReqSeq is the highest PackageDataRequest.Seq received from the server.
	// Tracks the server's reverse-RPC sequence space (uint64), separate from
	// the sync update sequence space (uint32).
	lastReqSeqMu sync.Mutex
	lastReqSeq   uint64

	// RPC dispatch state.
	mu      sync.Mutex
	pending map[string]chan *protocol.PackageDataResponse

	// Request handler registry (D-092).
	reqMu           sync.RWMutex
	requestHandlers map[string]RequestHandlerFunc

	// Lifecycle.
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	wg      sync.WaitGroup
	muState sync.Mutex
	closed  bool

	// replacedWake is closed by Reconnect() to wake the connection monitor
	// from its dormant state after a device replacement (D-111). When the
	// monitor detects replaced=true, it waits on this channel instead of
	// returning, allowing the daemon to resume connectivity when triggered
	// by an IPC command.
	replacedWake   chan struct{}
	replacedWakeMu sync.Mutex

	logger Logger
}

// New creates a XyncraClient configured by the supplied functional options.
// It validates required fields, instantiates the connection, sync and retry
// managers, and returns a client that is ready to be started with Start.
func New(opts ...ClientOption) (*XyncraClient, error) {
	o := clientOptions{
		serverURL:            defaultServerURL,
		rpcTimeout:           defaultRPCTimeout,
		heartbeatInterval:    defaultHeartbeatInterval,
		syncBatchSize:        defaultSyncBatchSize,
		pullDebounce:         defaultPullDebounce,
		retryBaseDelay:       defaultRetryBaseDelay,
		retryMaxAttempts:     defaultRetryMaxAttempts,
		retryPollInterval:    defaultRetryPollInterval,
		reconnectBaseDelay:   defaultReconnectBaseDelay,
		reconnectMaxDelay:    defaultReconnectMaxDelay,
		reconnectMaxRetries:  defaultReconnectMaxRetries,
		idempotencyCacheSize: defaultIdempotencyCacheSize,
		rttWindowSize:        defaultRTTWindowSize,
		adaptiveTimeoutMin:   defaultAdaptiveTimeoutMin,
		adaptiveTimeoutMax:   defaultAdaptiveTimeoutMax,
		responseRetryMaxSize: defaultResponseRetryMaxSize,
		responseRetryMax:     defaultResponseRetryMax,
	}
	for _, fn := range opts {
		fn(&o)
	}

	if o.serverURL == "" {
		return nil, fmt.Errorf("client: serverURL is required")
	}
	if o.userID == "" {
		return nil, fmt.Errorf("client: userID is required")
	}
	if o.db == nil {
		return nil, fmt.Errorf("client: db is required")
	}
	if o.logger == nil {
		o.logger = newStdLogger()
	}

	c := &XyncraClient{
		opts:            o,
		db:              o.db,
		pending:         make(map[string]chan *protocol.PackageDataResponse),
		requestHandlers: make(map[string]RequestHandlerFunc),
		done:            make(chan struct{}),
		replacedWake:    make(chan struct{}),
		logger:          o.logger,
	}

	c.idempotencyCache = NewIdempotencyCache(o.idempotencyCacheSize)
	c.rttTracker = NewRTTTracker(o.rttWindowSize)
	c.respRetryQueue = NewResponseRetryQueue(o.responseRetryMaxSize, o.responseRetryMax, o.logger)

	// Connection manager with callbacks wired into the dispatch layer.
	c.connMgr = newConnectionManager(o, connectionCallbacks{
		onResponse: c.dispatchResponse,
		onUpdates:  c.dispatchUpdates,
		onRequest:  func(req *protocol.PackageDataRequest) { c.handleIncomingRequest(req) },
		onConnect:  func() { c.logger.Info("connection established") },
		onDisconnect: func(replaced bool) {
			if replaced {
				c.logger.Info("connection replaced by newer instance (4001)")
			} else {
				c.logger.Info("connection lost")
			}
		},
	})

	// Sync manager — uses Call via a typed wrapper.
	c.syncMgr = newSyncManager(
		o.db,
		o.updateHandler,
		o.userID,
		func(ctx context.Context, method string, params any) (json.RawMessage, error) {
			return c.Call(ctx, method, params)
		},
		o.syncBatchSize,
		o.pullDebounce,
		o.logger,
	)

	// Retry manager — uses Call via a wrapper that passes json.RawMessage.
	c.retryMgr = newRetryManager(
		o.db,
		func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
			return c.Call(ctx, method, params)
		},
		o.retryBaseDelay,
		o.retryMaxAttempts,
		o.retryPollInterval,
		o.logger,
	)

	return c, nil
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// Start launches background goroutines for heartbeat and connection monitoring,
// starts the sync and retry managers, and blocks until ctx is cancelled.
// The initial WebSocket connection is established asynchronously inside the
// connection monitor goroutine, which retries indefinitely on failure — the
// daemon never exits due to an unreachable server (see D-044).
func (c *XyncraClient) Start(ctx context.Context) error {
	c.muState.Lock()
	if c.closed {
		c.muState.Unlock()
		return fmt.Errorf("client: already closed")
	}
	c.ctx, c.cancel = context.WithCancel(ctx)
	c.muState.Unlock()

	// 1. Start sync and retry managers first so they are ready to handle
	//    updates that may arrive as soon as the connection is established.
	c.syncMgr.Start(c.ctx)
	c.retryMgr.Start(c.ctx)

	// 1b. Response retry loop.
	c.wg.Add(1)
	go c.responseRetryLoop()

	// 2. Heartbeat goroutine.
	c.wg.Add(1)
	go c.heartbeatLoop()

	// 3. Connection monitor goroutine — handles initial connection (with
	//    infinite retries) and subsequent reconnection after disconnects.
	c.wg.Add(1)
	go c.connectionMonitorWithInitialConnect()

	// 4. Block until the context is done.
	<-c.ctx.Done()

	// 5. Cleanup.
	c.shutdown()
	return nil
}

// Stop gracefully shuts down the client, stopping all background goroutines
// and closing the underlying connection. It is idempotent.
func (c *XyncraClient) Stop() {
	c.muState.Lock()
	if c.closed {
		c.muState.Unlock()
		return
	}
	c.closed = true
	if c.cancel != nil {
		c.cancel()
	}
	c.muState.Unlock()
	c.shutdown()
}

// Close is an alias for Stop.
func (c *XyncraClient) Close() {
	c.Stop()
}

// Reconnect wakes the connection monitor from its dormant state after a device
// replacement (D-111). When the connection was replaced (4001), the monitor
// enters a dormant state; calling Reconnect triggers it to re-establish the
// WebSocket connection. This is safe to call even when already connected — it
// is a no-op if the connection is active.
//
// It is typically called by IPC handlers (e.g. sync_updates) that need an
// active server connection.
func (c *XyncraClient) Reconnect() {
	c.replacedWakeMu.Lock()
	defer c.replacedWakeMu.Unlock()
	select {
	case <-c.replacedWake:
		// Already closed and reset — nothing to do.
	default:
		// Close to wake the monitor, then create a fresh channel for the
		// next dormancy cycle. This ensures the channel lifecycle:
		// open → closed → new open → closed → new open ...
		close(c.replacedWake)
		c.replacedWake = make(chan struct{})
	}
}

// DeviceID returns the device identifier used by this client.
func (c *XyncraClient) DeviceID() string {
	return c.connMgr.DeviceID()
}

// shutdown performs the ordered teardown of all subsystems. It is safe to call
// multiple times; the muState guard ensures only one caller proceeds.
func (c *XyncraClient) shutdown() {
	c.muState.Lock()
	if c.closed {
		c.muState.Unlock()
		return
	}
	c.closed = true
	if c.cancel != nil {
		c.cancel()
	}
	c.muState.Unlock()

	// 1. Close the connection (stops readPump/writePump).
	c.connMgr.Close()

	// 2. Stop sync and retry managers.
	c.syncMgr.Stop()
	c.retryMgr.Stop()

	// 3. Fail all pending RPCs.
	c.mu.Lock()
	for id, ch := range c.pending {
		ch <- &protocol.PackageDataResponse{
			ID:   id,
			Code: ErrorCodeConnectionError,
			Msg:  "client shutting down",
		}
		delete(c.pending, id)
	}
	c.mu.Unlock()

	// 4. Wait for goroutines with a timeout.
	finished := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		c.logger.Error("shutdown: goroutine wait timed out")
	}

	c.logger.Info("client stopped")
}

// ---------------------------------------------------------------------------
// Background loops
// ---------------------------------------------------------------------------

// heartbeatLoop sends periodic heartbeat RPCs to keep the server-side session
// alive. It exits when the client context is cancelled.
func (c *XyncraClient) heartbeatLoop() {
	defer c.wg.Done()
	ticker := time.NewTicker(c.opts.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			// Best-effort; errors are logged inside Call.
			_, _ = c.Call(c.ctx, "heartbeat", nil)
		}
	}
}

// connectionMonitorWithInitialConnect establishes the initial WebSocket
// connection, retrying indefinitely on failure (D-044: daemon never exits due
// to an unreachable server). Once connected it falls through to the standard
// reconnect loop that watches for unexpected disconnections and reconnects
// with exponential backoff, performing a full sync after every successful
// reconnection.
func (c *XyncraClient) connectionMonitorWithInitialConnect() {
	defer c.wg.Done()

	// Phase 1 — initial connection with infinite retries.
	for {
		if c.ctx.Err() != nil {
			return
		}
		err := c.connMgr.Connect(c.ctx)
		if err == nil {
			c.logger.Info("initial connection established")
			c.performReconnectHandshake(c.ctx)
			if syncErr := c.syncMgr.FullSync(c.ctx); syncErr != nil {
				c.logger.Error("initial full sync failed", "error", syncErr)
			}
			break
		}
		c.logger.Error("initial connection failed, retrying...", "error", err)
		select {
		case <-c.ctx.Done():
			return
		case <-time.After(c.opts.reconnectBaseDelay):
		}
	}

	// Phase 2 — standard reconnect loop (disconnect → reconnect with backoff).
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-c.connMgr.Disconnected():
			// If this connection was replaced by a newer instance from the
			// same device (server sent 4001), enter dormant state — do not
			// reconnect immediately. The daemon stays alive with IPC
			// available (D-044, D-111). When an IPC command triggers
			// Reconnect(), the monitor wakes up and resumes connectivity.
			if c.connMgr.Replaced() {
				c.logger.Info("connection replaced by newer device instance, entering dormant state (D-111)")
				// Capture the wake channel reference under the lock so that
				// Reconnect() (which closes + replaces the channel) cannot
				// race with the select below.
				c.replacedWakeMu.Lock()
				wakeCh := c.replacedWake
				c.replacedWakeMu.Unlock()
				select {
				case <-c.ctx.Done():
					return
				case <-wakeCh:
					c.logger.Info("woken up for reconnection after replacement")
					// Fall through to the reconnect loop below.
				}
			}
			c.logger.Info("connection lost, reconnecting...")
			for {
				if c.ctx.Err() != nil {
					return
				}
				err := c.connMgr.Reconnect(c.ctx)
				if err == nil {
					c.logger.Info("reconnected successfully")
					c.performReconnectHandshake(c.ctx)
					if syncErr := c.syncMgr.FullSync(c.ctx); syncErr != nil {
						c.logger.Error("full sync after reconnect", "error", syncErr)
					}
					break // back to outer loop waiting for next disconnect
				}
				c.logger.Error("reconnect failed", "error", err)
			}
		}
	}
}

// performReconnectHandshake sends system.reconnect followed by
// system.register_functions after a (re)connect. Errors are logged but do
// not prevent FullSync from proceeding (graceful degradation, D-072).
// Runs asynchronously so the reconnect handshake timeout does not block
// FullSync.
func (c *XyncraClient) performReconnectHandshake(ctx context.Context) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		// Step 1: system.reconnect
		c.lastReqSeqMu.Lock()
		lastSeq := c.lastReqSeq
		c.lastReqSeqMu.Unlock()

		params, _ := json.Marshal(map[string]uint64{"last_seen_seq": lastSeq})
		_, err := c.Call(ctx, "system.reconnect", json.RawMessage(params))
		if err != nil {
			c.logger.Error("system.reconnect handshake failed", "error", err)
		}

		// Step 2: re-register functions.
		c.reregisterFunctions(ctx)
	}()
}

// reregisterFunctions re-sends system.register_functions after reconnect.
// Currently a placeholder — logs the intent. Full implementation pending
// client-side function manifest API.
func (c *XyncraClient) reregisterFunctions(ctx context.Context) {
	c.reqMu.RLock()
	count := len(c.requestHandlers)
	c.reqMu.RUnlock()

	if count == 0 {
		return
	}

	c.logger.Info("function re-registration pending", "count", count)
}

// ---------------------------------------------------------------------------
// RPC dispatch
// ---------------------------------------------------------------------------

// Call performs a synchronous RPC call to the server. It generates a unique
// request ID, sends a PackageTypeRequest, and waits for the matching response
// or a timeout. The response payload (resp.Data) is returned on success.
func (c *XyncraClient) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.muState.Lock()
	if c.closed {
		c.muState.Unlock()
		return nil, &ClientError{Code: ErrorCodeConnectionError, Message: "client is closed"}
	}
	c.muState.Unlock()

	// Generate a unique request ID (D-097: UUID, no prefix).
	reqID := uuid.New().String()

	// Serialize params.
	var paramsBytes []byte
	if params == nil {
		paramsBytes = []byte("{}")
	} else {
		var err error
		paramsBytes, err = json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("client: marshal params: %w", err)
		}
	}

	// Register a pending channel before sending so the response cannot arrive
	// before we are ready to receive it.
	respCh := make(chan *protocol.PackageDataResponse, 1)
	c.mu.Lock()
	c.pending[reqID] = respCh
	c.mu.Unlock()

	// Ensure cleanup of the pending entry when we return.
	defer func() {
		c.mu.Lock()
		delete(c.pending, reqID)
		c.mu.Unlock()
	}()

	// Build and send the request package.
	req := protocol.PackageDataRequest{
		ID:     reqID,
		Method: method,
		Params: json.RawMessage(paramsBytes),
	}
	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("client: marshal request: %w", err)
	}
	pkg := &protocol.Package{
		Version: 1,
		Type:    protocol.PackageTypeRequest,
		Data:    reqData,
	}

	startTime := time.Now()

	// Best-effort extract conversation_id from params for logging.
	var conversationID string
	var paramsMap map[string]json.RawMessage
	if json.Unmarshal(paramsBytes, &paramsMap) == nil {
		if raw, ok := paramsMap["conversation_id"]; ok {
			_ = json.Unmarshal(raw, &conversationID)
		}
	}

	if err := c.connMgr.SendPackage(pkg); err != nil {
		// Enqueue for retry on connection error (transient failure).
		_ = c.retryMgr.Enqueue(ctx, method, paramsBytes)
		return nil, NewConnectionError(fmt.Errorf("send package: %w", err))
	}

	// Persist an initial RPC log entry (status 0 = in-flight).
	rpcLog := &model.RPCLog{
		ID:             uuid.New().String(),
		Type:           "request",
		RequestID:      reqID,
		Method:         method,
		Params:         paramsBytes,
		ConversationID: conversationID,
		CreatedAt:      startTime,
	}
	// Best-effort save; errors are not fatal to the RPC.
	_ = c.db.RPCLogs.Save(ctx, rpcLog)

	// Wait for response, context cancellation, or timeout.
	adaptiveTimeout := c.rttTracker.AdaptiveTimeout(
		c.opts.rpcTimeout,
		c.opts.adaptiveTimeoutMin,
		c.opts.adaptiveTimeoutMax,
	)
	var resp *protocol.PackageDataResponse
	select {
	case resp = <-respCh:
		rtt := time.Since(startTime)
		c.rttTracker.Record(rtt)
	case <-ctx.Done():
		rpcLog.Duration = time.Since(startTime)
		rpcLog.ErrorMsg = ctx.Err().Error()
		rpcLog.StatusCode = int(ErrorCodeTimeoutError)
		rpcLog.Type = "response"
		_ = c.db.RPCLogs.Update(ctx, rpcLog)
		// Enqueue for retry on context cancellation (transient failure).
		_ = c.retryMgr.Enqueue(ctx, method, paramsBytes)
		return nil, NewTimeoutError(ctx.Err())
	case <-time.After(adaptiveTimeout):
		rpcLog.Duration = time.Since(startTime)
		rpcLog.ErrorMsg = fmt.Sprintf("rpc %s timed out", method)
		rpcLog.StatusCode = int(ErrorCodeTimeoutError)
		rpcLog.Type = "response"
		_ = c.db.RPCLogs.Update(ctx, rpcLog)
		// Enqueue for retry on timeout (transient failure).
		_ = c.retryMgr.Enqueue(ctx, method, paramsBytes)
		return nil, NewTimeoutError(fmt.Errorf("rpc %s timed out", method))
	}

	rpcLog.Duration = time.Since(startTime)
	rpcLog.Type = "response"

	if resp.Code == protocol.ResponseCodeOK {
		rpcLog.StatusCode = 0
		rpcLog.Response = resp.Data
		_ = c.db.RPCLogs.Update(ctx, rpcLog)
		return resp.Data, nil
	}

	// Server returned an error.
	rpcLog.StatusCode = int(resp.Code)
	rpcLog.ErrorMsg = resp.Msg
	_ = c.db.RPCLogs.Update(ctx, rpcLog)
	return nil, &ClientError{Code: resp.Code, Message: resp.Msg}
}

// ---------------------------------------------------------------------------
// Internal dispatch
// ---------------------------------------------------------------------------

// dispatchResponse routes an incoming server response to the pending RPC caller
// identified by the response's ID. If no matching caller is found (e.g. because
// the caller timed out and cleaned up), the response is silently dropped.
func (c *XyncraClient) dispatchResponse(resp *protocol.PackageDataResponse) {
	c.mu.Lock()
	ch, ok := c.pending[resp.ID]
	if ok {
		delete(c.pending, resp.ID)
	}
	c.mu.Unlock()
	if ok {
		ch <- resp
	}
}

// dispatchUpdates forwards a batch of server-pushed updates to the sync
// manager for processing.
func (c *XyncraClient) dispatchUpdates(updates *protocol.PackageDataUpdates) {
	if err := c.syncMgr.ApplyUpdates(c.ctx, updates.Updates); err != nil {
		c.logger.Error("apply updates", "error", err)
	}
}

// ---------------------------------------------------------------------------
// Server-initiated request handling (D-092)
// ---------------------------------------------------------------------------

// RegisterRequestHandler registers a handler for server-initiated requests
// with the given method name. When the server sends a request with a matching
// method, the handler is invoked and its result is sent back as a response.
// This enables the client to respond to server-initiated RPCs (D-092).
func (c *XyncraClient) RegisterRequestHandler(method string, h RequestHandlerFunc) {
	c.reqMu.Lock()
	defer c.reqMu.Unlock()
	c.requestHandlers[method] = h
}

// handleIncomingRequest processes a server-initiated request by looking up
// the registered handler, invoking it, and sending back a response package.
// If no handler is found, an error response is sent. This runs in the
// readPump goroutine; handlers should be fast or spawn their own goroutines.
func (c *XyncraClient) handleIncomingRequest(req *protocol.PackageDataRequest) {
	// Track highest request seq (for system.reconnect).
	if req.Seq > 0 {
		c.lastReqSeqMu.Lock()
		if req.Seq > c.lastReqSeq {
			c.lastReqSeq = req.Seq
		}
		c.lastReqSeqMu.Unlock()
	}

	// Idempotency dedup check.
	if req.IdempotencyKey != "" && c.idempotencyCache.Contains(req.IdempotencyKey) {
		c.logger.Debug("deduplicating replayed request",
			"idempotency_key", req.IdempotencyKey, "method", req.Method)
		resp := &protocol.PackageDataResponse{
			ID:   req.ID,
			Code: protocol.ResponseCodeOK,
			Msg:  "duplicate (idempotency cache hit)",
		}
		respData, _ := json.Marshal(resp)
		pkg := &protocol.Package{Type: protocol.PackageTypeResponse, Data: respData}
		if err := c.connMgr.SendPackage(pkg); err != nil {
			c.respRetryQueue.Enqueue(resp)
		}
		return
	}

	c.reqMu.RLock()
	handler, ok := c.requestHandlers[req.Method]
	c.reqMu.RUnlock()

	var resp protocol.PackageDataResponse
	resp.ID = req.ID

	ctx := c.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	if !ok {
		resp.Code = protocol.ResponseCodeError
		resp.Msg = fmt.Sprintf("unknown method: %s", req.Method)
	} else {
		data, err := handler(ctx, req)
		if err != nil {
			resp.Code = protocol.ResponseCodeError
			resp.Msg = err.Error()
		} else {
			resp.Code = protocol.ResponseCodeOK
			resp.Msg = "ok"
			resp.Data = data
		}
	}

	// Record idempotency key on successful processing.
	if req.IdempotencyKey != "" {
		c.idempotencyCache.Put(req.IdempotencyKey)
	}

	respData, err := json.Marshal(resp)
	if err != nil {
		c.logger.Error("marshal response to server request", "error", err)
		return
	}
	pkg := &protocol.Package{
		Type: protocol.PackageTypeResponse,
		Data: respData,
	}
	if err := c.connMgr.SendPackage(pkg); err != nil {
		c.respRetryQueue.Enqueue(&resp)
		c.logger.Error("send response failed, queued for retry", "error", err)
	}
}

// responseRetryLoop periodically drains the response retry queue and
// attempts to re-send failed responses. Exits when the client context
// is cancelled.
func (c *XyncraClient) responseRetryLoop() {
	defer c.wg.Done()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			entries := c.respRetryQueue.Drain(time.Now())
			for _, entry := range entries {
				respData, _ := json.Marshal(entry.Response())
				pkg := &protocol.Package{Type: protocol.PackageTypeResponse, Data: respData}
				if err := c.connMgr.SendPackage(pkg); err != nil {
					entry.attempts++
					if entry.attempts < entry.maxRetry {
						c.respRetryQueue.EnqueueWithBackoff(entry)
					} else {
						c.logger.Error("response retry exhausted",
							"response_id", entry.Response().ID,
							"attempts", entry.attempts)
					}
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Result types
// ---------------------------------------------------------------------------

// SendMessageResult is the response payload for the SendMessage RPC.
type SendMessageResult struct {
	Message   *model.Message `json:"message"`
	Duplicate bool           `json:"duplicate"`
}

// SyncUpdatesResult is the response payload for the SyncUpdates RPC.
type SyncUpdatesResult struct {
	Updates   []protocol.PackageDataUpdate `json:"updates"`
	HasMore   bool                         `json:"has_more"`
	LatestSeq uint32                       `json:"latest_seq"`
}

// CreateConversationResult is the response payload for the CreateConversation RPC.
type CreateConversationResult struct {
	Conversation *model.Conversation `json:"conversation"`
	Duplicate    bool                `json:"duplicate"`
}

// ListConversationsResult is the response payload for the ListConversations RPC.
type ListConversationsResult struct {
	Conversations []model.Conversation `json:"conversations"`
	HasMore       bool                 `json:"has_more"`
}

// GetMessagesResult is the response payload for the GetMessages RPC.
type GetMessagesResult struct {
	Messages []model.Message `json:"messages"`
	HasMore  bool            `json:"has_more"`
}

// SearchMessagesResult is the response payload for the SearchMessages RPC.
type SearchMessagesResult struct {
	Messages []model.Message `json:"messages"`
	HasMore  bool            `json:"has_more"`
}

// GetConversationResult is the response payload for the GetConversation RPC.
type GetConversationResult struct {
	Conversation *model.Conversation `json:"conversation"`
	UnreadCount  int64               `json:"unread_count"`
}

// DeleteConversationResult holds the result of a delete_conversation RPC.
type DeleteConversationResult struct {
	Status              string `json:"status"`
	DeletedMessageCount int64  `json:"deleted_message_count"`
}

// RestoreConversationResult holds the result of a restore_conversation RPC.
type RestoreConversationResult struct {
	Conversation         *model.Conversation `json:"conversation"`
	RestoredMessageCount int64               `json:"restored_message_count"`
}

// ---------------------------------------------------------------------------
// RPC convenience methods
// ---------------------------------------------------------------------------

// Heartbeat sends a heartbeat ping to the server. It is a convenience wrapper
// around Call("heartbeat", nil).
func (c *XyncraClient) Heartbeat(ctx context.Context) error {
	_, err := c.Call(ctx, "heartbeat", nil)
	return err
}

// SendMessage sends a chat message to the server. clientMsgID is a UUID v4
// used for idempotency — the same clientMsgID will not create duplicate
// messages on the server.
func (c *XyncraClient) SendMessage(ctx context.Context, convID, content, clientMsgID string, replyTo uint32) (*SendMessageResult, error) {
	if clientMsgID == "" {
		clientMsgID = uuid.New().String()
	}
	params := map[string]any{
		"conversation_id":   convID,
		"content":           content,
		"client_message_id": clientMsgID,
		"reply_to":          replyTo,
	}
	data, err := c.Call(ctx, "send_message", params)
	if err != nil {
		return nil, err
	}
	var result SendMessageResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("client: unmarshal send_message result: %w", err)
	}
	return &result, nil
}

// SyncUpdates fetches incremental updates from the server starting after the
// given sequence number, limited to at most limit records.
func (c *XyncraClient) SyncUpdates(ctx context.Context, afterSeq uint32, limit int) (*SyncUpdatesResult, error) {
	params := map[string]any{
		"after_seq": afterSeq,
		"limit":     limit,
	}
	data, err := c.Call(ctx, "sync_updates", params)
	if err != nil {
		return nil, err
	}
	var result SyncUpdatesResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("client: unmarshal sync_updates result: %w", err)
	}
	return &result, nil
}

// CreateConversation creates a new 1-on-1 conversation with the specified user.
func (c *XyncraClient) CreateConversation(ctx context.Context, userID2, title string) (*CreateConversationResult, error) {
	params := map[string]any{
		"user_id": userID2,
		"title":   title,
	}
	data, err := c.Call(ctx, "create_conversation", params)
	if err != nil {
		return nil, err
	}
	var result CreateConversationResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("client: unmarshal create_conversation result: %w", err)
	}
	return &result, nil
}

// ListConversations returns a paginated list of conversations for the current user.
func (c *XyncraClient) ListConversations(ctx context.Context, offset, limit int) (*ListConversationsResult, error) {
	params := map[string]any{
		"offset": offset,
		"limit":  limit,
	}
	data, err := c.Call(ctx, "list_conversations", params)
	if err != nil {
		return nil, err
	}
	var result ListConversationsResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("client: unmarshal list_conversations result: %w", err)
	}
	return &result, nil
}

// GetMessages returns messages for the given conversation, optionally starting
// after the specified message ID.
func (c *XyncraClient) GetMessages(ctx context.Context, convID string, afterMsgID uint32, limit int) (*GetMessagesResult, error) {
	params := map[string]any{
		"conversation_id": convID,
		"after_msg_id":    afterMsgID,
		"limit":           limit,
	}
	data, err := c.Call(ctx, "get_messages", params)
	if err != nil {
		return nil, err
	}
	var result GetMessagesResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("client: unmarshal get_messages result: %w", err)
	}
	return &result, nil
}

// SearchMessages searches for messages matching the given query within a
// conversation.
func (c *XyncraClient) SearchMessages(ctx context.Context, convID, query string, afterMsgID uint32, limit int) (*SearchMessagesResult, error) {
	params := map[string]any{
		"conversation_id": convID,
		"query":           query,
		"after_msg_id":    afterMsgID,
		"limit":           limit,
	}
	data, err := c.Call(ctx, "search_messages", params)
	if err != nil {
		return nil, err
	}
	var result SearchMessagesResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("client: unmarshal search_messages result: %w", err)
	}
	return &result, nil
}

// GetConversation returns the conversation identified by convID, including the
// current unread count.
func (c *XyncraClient) GetConversation(ctx context.Context, convID string) (*GetConversationResult, error) {
	params := map[string]any{
		"conversation_id": convID,
	}
	data, err := c.Call(ctx, "get_conversation", params)
	if err != nil {
		return nil, err
	}
	var result GetConversationResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("client: unmarshal get_conversation result: %w", err)
	}
	return &result, nil
}

// DeleteConversation soft-deletes the conversation identified by convID and
// returns the number of messages that were cascade-deleted.
func (c *XyncraClient) DeleteConversation(ctx context.Context, convID string) (*DeleteConversationResult, error) {
	params := map[string]any{
		"conversation_id": convID,
	}
	data, err := c.Call(ctx, "delete_conversation", params)
	if err != nil {
		return nil, err
	}
	var result DeleteConversationResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("client: unmarshal delete_conversation result: %w", err)
	}
	return &result, nil
}

// RestoreConversation restores a previously soft-deleted conversation and
// returns the number of messages that were cascade-restored.
func (c *XyncraClient) RestoreConversation(ctx context.Context, convID string) (*RestoreConversationResult, error) {
	params := map[string]any{
		"conversation_id": convID,
	}
	data, err := c.Call(ctx, "restore_conversation", params)
	if err != nil {
		return nil, err
	}
	var result RestoreConversationResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("client: unmarshal restore_conversation result: %w", err)
	}
	return &result, nil
}

// DeleteMessage soft-deletes the message identified by messageID.
func (c *XyncraClient) DeleteMessage(ctx context.Context, messageID string) error {
	params := map[string]any{
		"message_id": messageID,
	}
	_, err := c.Call(ctx, "delete_message", params)
	return err
}

// MarkAsRead advances the read cursor for the current user in the given
// conversation to the specified message ID.
func (c *XyncraClient) MarkAsRead(ctx context.Context, convID string, messageID uint32) error {
	params := map[string]any{
		"conversation_id": convID,
		"message_id":      messageID,
	}
	_, err := c.Call(ctx, "mark_as_read", params)
	return err
}

// FullSync triggers a blocking, paginated synchronization with the server,
// fetching all updates after the current localMaxSeq until has_more is false.
// Exposed for IPC handlers (e.g. sync-updates CLI command) to trigger sync
// through the daemon's existing pipeline.
func (c *XyncraClient) FullSync(ctx context.Context) error {
	return c.syncMgr.FullSync(ctx)
}
