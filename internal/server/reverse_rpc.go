package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// ReverseRPC enables the server to send requests to clients and await responses.
type ReverseRPC struct {
	mu        sync.Mutex
	pending   map[string]*reverseRPCPending // reqID → pending
	nextReqID uint64                        // atomic counter

	sendFunc func(userID string, pkg *protocol.Package) error
	logger   Logger
}

type reverseRPCPending struct {
	respCh chan *protocol.PackageDataResponse // buffered cap=1
	cancel context.CancelFunc
}

// ReverseRPCConfig configures a ReverseRPC during construction.
type ReverseRPCConfig struct {
	SendFunc func(userID string, pkg *protocol.Package) error
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

// ServerRequest sends a request to all connections of userID and blocks until
// a response arrives, the context is cancelled, or the timeout expires.
// Returns error if user has no active connections.
func (r *ReverseRPC) ServerRequest(ctx context.Context, userID string, method string, params json.RawMessage, timeout time.Duration) (*protocol.PackageDataResponse, error) {
	reqID := fmt.Sprintf("s-%d", atomic.AddUint64(&r.nextReqID, 1))

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	pending := &reverseRPCPending{
		respCh: make(chan *protocol.PackageDataResponse, 1),
		cancel: cancel,
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

	if err := r.sendFunc(userID, pkg); err != nil {
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
		p.cancel()
	}
}
