package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// ReverseRPC enables the server to send requests to clients and await responses.
type ReverseRPC struct {
	mu      sync.Mutex
	pending map[string]*reverseRPCPending // reqID → pending

	sendFunc func(userID, deviceID string, pkg *protocol.Package) error
	logger   Logger
}

type reverseRPCPending struct {
	respCh   chan *protocol.PackageDataResponse // buffered cap=1
	cancel   context.CancelFunc
	userID   string // for CancelDevice cross-user safety
	deviceID string // for CancelDevice per-device filtering
}

// ReverseRPCConfig configures a ReverseRPC during construction.
type ReverseRPCConfig struct {
	SendFunc func(userID, deviceID string, pkg *protocol.Package) error
	Logger   Logger
}

// NewReverseRPC creates a new ReverseRPC instance.
func NewReverseRPC(cfg ReverseRPCConfig) *ReverseRPC {
	return &ReverseRPC{
		pending:  make(map[string]*reverseRPCPending),
		sendFunc: cfg.SendFunc,
		logger:   cfg.Logger,
	}
}

// ServerRequest sends a request to a specific user's device and blocks until
// a response arrives, the context is cancelled, or the timeout expires.
// If deviceID is empty, the request is broadcast to all connections of the user.
// Returns error if user has no active connections (or the device is offline).
func (r *ReverseRPC) ServerRequest(ctx context.Context, userID, deviceID string, method string, params json.RawMessage, timeout time.Duration) (*protocol.PackageDataResponse, error) {
	reqID := fmt.Sprintf("s-%s", uuid.New().String())

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	pending := &reverseRPCPending{
		respCh:   make(chan *protocol.PackageDataResponse, 1),
		cancel:   cancel,
		userID:   userID,
		deviceID: deviceID,
	}

	r.mu.Lock()
	r.pending[reqID] = pending
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		delete(r.pending, reqID)
		r.mu.Unlock()
	}()

	req := &protocol.PackageDataRequest{
		ID:     reqID,
		Method: method,
		Params: params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	pkg := &protocol.Package{
		Version: 1,
		Type:    protocol.PackageTypeRequest,
		Data:    data,
	}

	if err := r.sendFunc(userID, deviceID, pkg); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	select {
	case resp := <-pending.respCh:
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// DispatchResponse routes an incoming response to the matching pending caller.
// Silently ignores unknown IDs (timeout-late responses).
func (r *ReverseRPC) DispatchResponse(resp *protocol.PackageDataResponse) {
	r.mu.Lock()
	pending, ok := r.pending[resp.ID]
	if ok {
		delete(r.pending, resp.ID)
	}
	r.mu.Unlock()

	if !ok {
		return
	}

	select {
	case pending.respCh <- resp:
	default:
	}
}

// CancelDeviceWithReason cancels all pending reverse-RPC requests for the given
// device and sends a synthetic response with the specified reason message.
func (r *ReverseRPC) CancelDeviceWithReason(userID, deviceID, reason string) {
	r.mu.Lock()
	var toCancel []*reverseRPCPending
	for id, p := range r.pending {
		if p.userID == userID && p.deviceID == deviceID {
			delete(r.pending, id)
			toCancel = append(toCancel, p)
		}
	}
	r.mu.Unlock()
	for _, p := range toCancel {
		select {
		case p.respCh <- &protocol.PackageDataResponse{Code: -1, Msg: reason}:
		default:
		}
		// Do NOT call p.cancel() here: the respCh write above is already in
		// the select, and calling cancel() would make ctx.Done() race with
		// respCh in ServerRequest's select. Let ServerRequest's defer cancel()
		// clean up after it receives the respCh response.
	}
}

// CancelDevice cancels all pending reverse-RPC requests for the given device.
// It is a convenience wrapper around CancelDeviceWithReason with "device replaced"
// as the default reason (D-095).
func (r *ReverseRPC) CancelDevice(userID, deviceID string) {
	r.CancelDeviceWithReason(userID, deviceID, "device replaced")
}

// CancelAll fails all pending requests (called on shutdown).
func (r *ReverseRPC) CancelAll() {
	r.mu.Lock()
	pending := r.pending
	r.pending = make(map[string]*reverseRPCPending)
	r.mu.Unlock()

	for _, p := range pending {
		select {
		case p.respCh <- &protocol.PackageDataResponse{
			Code: -1,
			Msg:  "reverse rpc cancelled",
		}:
		default:
		}
		// Do NOT call p.cancel() here: see CancelDeviceWithReason for the
		// rationale. Let ServerRequest's defer cancel() handle cleanup.
	}
}
