package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

// JSON-RPC 2.0 protocol types for inter-process communication.
// Protocol: Unix domain socket, newline-delimited JSON (D-030).

// IPCRequest is a JSON-RPC 2.0 request message.
type IPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// IPCResponse is a JSON-RPC 2.0 response message.
type IPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *IPCError       `json:"error,omitempty"`
}

// IPCError represents a JSON-RPC 2.0 error object.
type IPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error implements the error interface for IPCError.
func (e *IPCError) Error() string {
	return fmt.Sprintf("ipc error %d: %s", e.Code, e.Message)
}

// NewIPCRequest constructs a new IPCRequest with an auto-generated UUID v4 id.
func NewIPCRequest(method string, params any) (*IPCRequest, error) {
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("new ipc request marshal params: %w", err)
		}
		raw = b
	}
	return &IPCRequest{
		JSONRPC: "2.0",
		ID:      uuid.New().String(),
		Method:  method,
		Params:  raw,
	}, nil
}

// NewIPCResponse constructs a successful IPCResponse with the given id and result.
func NewIPCResponse(id string, result any) (*IPCResponse, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("new ipc response marshal result: %w", err)
	}
	return &IPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  raw,
	}, nil
}

// NewIPCErrorResponse constructs an IPCResponse carrying an error.
func NewIPCErrorResponse(id string, code int, message string) *IPCResponse {
	return &IPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &IPCError{
			Code:    code,
			Message: message,
		},
	}
}

// IPCHandlerFunc is the signature for a method handler on the IPC server.
type IPCHandlerFunc func(ctx context.Context, req *IPCRequest) (*IPCResponse, error)

// IPCServer is a Unix domain socket server that dispatches JSON-RPC 2.0
// requests to registered method handlers.
//
// Handlers must be registered via Register before Start is called.
// At runtime the handlers map is read-only and requires no locking.
type IPCServer struct {
	sockPath string
	handlers map[string]IPCHandlerFunc
	listener net.Listener
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// NewIPCServer creates a new IPCServer bound to the given Unix socket path.
func NewIPCServer(sockPath string) *IPCServer {
	return &IPCServer{
		sockPath: sockPath,
		handlers: make(map[string]IPCHandlerFunc),
	}
}

// Register binds a handler to a method name. Must be called before Start.
func (s *IPCServer) Register(method string, handler IPCHandlerFunc) {
	s.handlers[method] = handler
}

// Start begins accepting connections on the Unix socket. It is non-blocking:
// the listener loop runs in a separate goroutine.
//
// Before listening it removes any stale socket at the path. After the
// listener is created the socket file is chmod'd to 0600.
func (s *IPCServer) Start(ctx context.Context) error {
	// Remove stale socket.
	if err := os.Remove(s.sockPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("ipc server remove stale socket: %w", err)
	}

	ln, err := net.Listen("unix", s.sockPath)
	if err != nil {
		return fmt.Errorf("ipc server listen: %w", err)
	}

	if err := os.Chmod(s.sockPath, 0600); err != nil {
		ln.Close()
		return fmt.Errorf("ipc server chmod socket: %w", err)
	}

	s.listener = ln
	s.ctx, s.cancel = context.WithCancel(ctx)

	s.wg.Add(1)
	go s.acceptLoop()

	return nil
}

// acceptLoop accepts connections until the listener is closed.
func (s *IPCServer) acceptLoop() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			// Listener closed — check context and exit.
			select {
			case <-s.ctx.Done():
				return
			default:
				// Backoff to avoid CPU spin on transient accept errors.
				time.Sleep(100 * time.Millisecond)
				continue
			}
		}
		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

// handleConn reads newline-delimited JSON requests from a single connection,
// dispatches them to handlers, and writes back JSON responses.
func (s *IPCServer) handleConn(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		resp := s.dispatch(line)
		b, err := json.Marshal(resp)
		if err != nil {
			// Should not happen with well-formed IPCResponse.
			continue
		}
		if _, err := conn.Write(append(b, '\n')); err != nil {
			return
		}
	}
	_ = scanner.Err()
}

// dispatch parses a single JSON-RPC request line and invokes the handler.
func (s *IPCServer) dispatch(line []byte) *IPCResponse {
	var req IPCRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return NewIPCErrorResponse("", -32700, "Parse error")
	}
	if req.JSONRPC != "2.0" {
		return NewIPCErrorResponse(req.ID, -32600, "Invalid Request")
	}

	handler, ok := s.handlers[req.Method]
	if !ok {
		return NewIPCErrorResponse(req.ID, -32601, "Method not found")
	}

	resp, err := handler(s.ctx, &req)
	if err != nil {
		return NewIPCErrorResponse(req.ID, -32000, err.Error())
	}
	return resp
}

// Stop closes the listener and waits for all in-flight connections to finish.
func (s *IPCServer) Stop() error {
	if s.cancel != nil {
		s.cancel()
	}
	var err error
	if s.listener != nil {
		err = s.listener.Close()
	}
	s.wg.Wait()
	return err
}

// IPCClient is a client for the Unix socket IPC server. Each Call opens a
// fresh connection, sends one request, reads one response, and closes.
type IPCClient struct {
	sockPath string
	timeout  time.Duration
}

// NewIPCClient creates a new IPCClient for the given socket path with a
// connection timeout.
func NewIPCClient(sockPath string, timeout time.Duration) *IPCClient {
	return &IPCClient{
		sockPath: sockPath,
		timeout:  timeout,
	}
}

// Call sends a single JSON-RPC request to the IPC server and returns the
// response. It opens a new connection per call.
func (c *IPCClient) Call(ctx context.Context, method string, params any) (*IPCResponse, error) {
	conn, err := net.DialTimeout("unix", c.sockPath, c.timeout)
	if err != nil {
		return nil, fmt.Errorf("ipc client dial: %w", err)
	}
	defer conn.Close()

	// Set read deadline so we don't block indefinitely if the server never responds.
	if err := conn.SetReadDeadline(time.Now().Add(c.timeout)); err != nil {
		return nil, fmt.Errorf("ipc client set read deadline: %w", err)
	}

	req, err := NewIPCRequest(method, params)
	if err != nil {
		return nil, fmt.Errorf("ipc client build request: %w", err)
	}

	b, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("ipc client marshal request: %w", err)
	}
	b = append(b, '\n')

	if _, err := conn.Write(b); err != nil {
		return nil, fmt.Errorf("ipc client write request: %w", err)
	}

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("ipc client read response: %w", err)
		}
		return nil, fmt.Errorf("ipc client read response: no response received")
	}

	var resp IPCResponse
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("ipc client unmarshal response: %w", err)
	}
	return &resp, nil
}
