package client

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/PineappleBond/xyncra-server/pkg/store"
	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

// ---------------------------------------------------------------------------
// Error codes (D-027): client extension error codes
// ---------------------------------------------------------------------------

const (
	// ErrorCodeConnectionError indicates a WebSocket connection failure.
	ErrorCodeConnectionError protocol.ResponseCode = -400
	// ErrorCodeTimeoutError indicates an operation exceeded its deadline.
	ErrorCodeTimeoutError protocol.ResponseCode = -401
	// ErrorCodeSyncError indicates a data synchronisation failure.
	ErrorCodeSyncError protocol.ResponseCode = -402
)

// ---------------------------------------------------------------------------
// Default configuration constants
// ---------------------------------------------------------------------------

const (
	// defaultServerURL is the WebSocket endpoint used when none is supplied.
	defaultServerURL = "ws://localhost:8080/ws"
	// defaultWriteWait is the maximum duration allowed for a write to complete.
	defaultWriteWait = 10 * time.Second
	// defaultPongWait is the maximum time to wait for a pong from the server.
	defaultPongWait = 60 * time.Second
	// defaultPingPeriod is the interval between pings; must be less than pongWait.
	defaultPingPeriod = 54 * time.Second // (pongWait * 9) / 10
	// defaultSendBufSize is the capacity of the outbound message channel.
	defaultSendBufSize = 256
	// defaultMaxMessageSize is the maximum inbound message size in bytes.
	defaultMaxMessageSize = 64 * 1024
	// defaultRPCTimeout is the default timeout for a single RPC call.
	defaultRPCTimeout = 30 * time.Second
	// defaultHeartbeatInterval is the interval between heartbeat pings.
	defaultHeartbeatInterval = 30 * time.Second
	// defaultSyncBatchSize is the number of records fetched per sync pull batch.
	defaultSyncBatchSize = 100
	// defaultPullDebounce is the debounce window for coalescing pull requests.
	defaultPullDebounce = 500 * time.Millisecond
	// defaultRetryBaseDelay is the initial delay before the first retry attempt.
	defaultRetryBaseDelay = 1 * time.Second
	// defaultRetryMaxAttempts is the maximum number of RPC retry attempts.
	defaultRetryMaxAttempts = 5
	// defaultRetryPollInterval is the polling interval used during retry backoff.
	defaultRetryPollInterval = 1 * time.Second
	// defaultReconnectBaseDelay is the initial delay before the first reconnect.
	defaultReconnectBaseDelay = 1 * time.Second
	// defaultReconnectMaxDelay caps the exponential reconnect backoff.
	defaultReconnectMaxDelay = 30 * time.Second
	// defaultReconnectMaxRetries is the maximum reconnect attempts; 0 means unlimited.
	//
	// Deprecated: connectionMonitorWithInitialConnect retries indefinitely by
	// design (D-044). This constant is retained only to keep the struct field
	// initialiser valid; it has no runtime effect.
	defaultReconnectMaxRetries = 0
	// defaultIdempotencyCacheSize is the maximum number of idempotency keys to cache.
	defaultIdempotencyCacheSize = 1024
	// defaultRTTWindowSize is the sliding window size for RTT samples.
	defaultRTTWindowSize = 50
	// defaultAdaptiveTimeoutMin is the minimum adaptive timeout.
	defaultAdaptiveTimeoutMin = 5 * time.Second
	// defaultAdaptiveTimeoutMax is the maximum adaptive timeout.
	defaultAdaptiveTimeoutMax = 120 * time.Second
	// defaultResponseRetryMaxSize is the maximum size of the response retry queue.
	defaultResponseRetryMaxSize = 100
	// defaultResponseRetryMax is the maximum retry attempts per response.
	defaultResponseRetryMax = 3
)

// ---------------------------------------------------------------------------
// Logger interface and default implementation
// ---------------------------------------------------------------------------

// Logger is the interface used by the client for structured log output.
type Logger interface {
	// Info logs an informational message.
	Info(msg string, args ...any)
	// Error logs an error message.
	Error(msg string, args ...any)
	// Debug logs a debug-level message.
	Debug(msg string, args ...any)
}

// stdLogger is the default Logger implementation backed by the standard log package.
type stdLogger struct {
	logger *log.Logger
}

// newStdLogger creates a stdLogger that writes to stderr.
func newStdLogger() *stdLogger {
	return &stdLogger{logger: log.New(os.Stderr, "", log.LstdFlags)}
}

// Info logs an informational message.
func (l *stdLogger) Info(msg string, args ...any) {
	l.logger.Printf("[INFO] %s%s", msg, formatArgs(args))
}

// Error logs an error message.
func (l *stdLogger) Error(msg string, args ...any) {
	l.logger.Printf("[ERROR] %s%s", msg, formatArgs(args))
}

// Debug logs a debug-level message.
func (l *stdLogger) Debug(msg string, args ...any) {
	l.logger.Printf("[DEBUG] %s%s", msg, formatArgs(args))
}

// formatArgs converts a variadic key-value slice into a " key=value ..." string.
// If args has an odd number of elements the trailing key is rendered with a
// MISSING value placeholder.
func formatArgs(args []any) string {
	if len(args) == 0 {
		return ""
	}
	var b strings.Builder
	for i := 0; i < len(args); i += 2 {
		b.WriteByte(' ')
		b.WriteString(fmt.Sprintf("%v", args[i]))
		b.WriteByte('=')
		if i+1 < len(args) {
			b.WriteString(fmt.Sprintf("%v", args[i+1]))
		} else {
			b.WriteString("MISSING")
		}
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// ClientError
// ---------------------------------------------------------------------------

// ClientError represents an error returned by the client layer, carrying one of
// the extension error codes defined in D-027.
type ClientError struct {
	// Code is the protocol-level error code identifying the error category.
	Code protocol.ResponseCode
	// Message is a short human-readable description of the error.
	Message string
	// Err is the underlying error that caused this ClientError, if any.
	Err error
}

// Error returns a formatted string: "client: [CODE] message" or
// "client: [CODE] message: underlying" when an underlying error is present.
func (e *ClientError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("client: [%d] %s: %s", e.Code, e.Message, e.Err.Error())
	}
	return fmt.Sprintf("client: [%d] %s", e.Code, e.Message)
}

// Unwrap returns the underlying error for use with errors.Is / errors.As.
func (e *ClientError) Unwrap() error {
	return e.Err
}

// NewConnectionError creates a ClientError with ErrorCodeConnectionError.
func NewConnectionError(err error) *ClientError {
	return &ClientError{
		Code:    ErrorCodeConnectionError,
		Message: "connection error",
		Err:     err,
	}
}

// NewTimeoutError creates a ClientError with ErrorCodeTimeoutError.
func NewTimeoutError(err error) *ClientError {
	return &ClientError{
		Code:    ErrorCodeTimeoutError,
		Message: "timeout error",
		Err:     err,
	}
}

// NewSyncError creates a ClientError with ErrorCodeSyncError.
func NewSyncError(err error) *ClientError {
	return &ClientError{
		Code:    ErrorCodeSyncError,
		Message: "sync error",
		Err:     err,
	}
}

// ---------------------------------------------------------------------------
// UpdateHandler — processes data updates received from the sync pipeline
// ---------------------------------------------------------------------------

// UpdateHandler receives processed data updates from the sync pipeline.
// Implementations are provided by the caller to apply updates to a local store
// or in-memory cache.
type UpdateHandler interface {
	// OnMessage is called when a new or updated message is received.
	OnMessage(ctx context.Context, msg *model.Message) error
	// OnDeleteMessage is called when a message deletion is received.
	OnDeleteMessage(ctx context.Context, messageID string, conversationID string) error
	// OnMarkRead is called when a read cursor advance is received.
	OnMarkRead(ctx context.Context, conversationID string, messageID uint32) error
	// OnConversation is called when a conversation state change is received.
	OnConversation(ctx context.Context, conv *model.Conversation) error
	// OnGap is called when a sequence gap is detected during sync.
	OnGap(ctx context.Context, seq uint32) error
}

// TypingHandler is an optional interface that UpdateHandler implementations
// may adopt to receive ephemeral typing indicators.
type TypingHandler interface {
	OnTyping(ctx context.Context, userID string, conversationID string, isTyping bool) error
}

// StreamingHandler is an optional interface that UpdateHandler implementations
// may adopt to receive ephemeral streaming text updates (D-051).
type StreamingHandler interface {
	OnStreaming(ctx context.Context, userID string, conversationID string, streamID string, text string, isDone bool) error
}

// AgentQuestionHandler is an optional interface that UpdateHandler implementations
// may adopt to receive agent HITL question events (D-087).
type AgentQuestionHandler interface {
	OnAgentQuestion(ctx context.Context, userID, conversationID, question, checkpointID, interruptID string) error
}

// AgentCheckpointHandler is an optional interface that UpdateHandler implementations
// may adopt to receive agent checkpoint created events (D-087).
type AgentCheckpointHandler interface {
	OnAgentCheckpointCreated(ctx context.Context, userID, conversationID, checkpointID string) error
}

// AgentStatusHandler is an optional interface that UpdateHandler implementations
// may adopt to receive agent status change events (D-087).
type AgentStatusHandler interface {
	OnAgentStatus(ctx context.Context, userID, conversationID, status string) error
}

// AgentTimeoutHandler is an optional interface that UpdateHandler implementations
// may adopt to receive agent timeout ephemeral update events (D-087).
type AgentTimeoutHandler interface {
	OnAgentTimeout(ctx context.Context, userID, conversationID, reason string) error
}

// ---------------------------------------------------------------------------
// clientOptions and Functional Options
// ---------------------------------------------------------------------------

// clientOptions holds the full set of configuration values for a Client.
// Default values are applied by New() before user-supplied options override them.
type clientOptions struct {
	serverURL          string
	userID             string
	deviceID           string
	rpcTimeout         time.Duration
	heartbeatInterval  time.Duration
	syncBatchSize      int
	pullDebounce       time.Duration
	retryBaseDelay     time.Duration
	retryMaxAttempts   int
	retryPollInterval  time.Duration
	reconnectBaseDelay time.Duration
	reconnectMaxDelay  time.Duration
	// reconnectMaxRetries is no longer used: connectionMonitorWithInitialConnect
	// retries indefinitely by design (D-044). The field is retained to avoid
	// breaking the public option API; it has no runtime effect.
	//
	// Deprecated: retained for API compatibility only.
	reconnectMaxRetries int
	db                  *store.ClientDB
	updateHandler       UpdateHandler
	logger              Logger

	idempotencyCacheSize int
	rttWindowSize        int
	adaptiveTimeoutMin   time.Duration
	adaptiveTimeoutMax   time.Duration
	responseRetryMaxSize int
	responseRetryMax     int

	// DynamicToolProvider: device metadata and function manifest for
	// auto-registration on connect/reconnect (D-098, D-101).
	deviceName string
	deviceType string
	functions  []protocol.FunctionInfo
}

// ClientOption configures a Client instance via the functional options pattern.
type ClientOption func(*clientOptions)

// WithServerURL sets the WebSocket server URL the client will connect to.
func WithServerURL(url string) ClientOption {
	return func(o *clientOptions) { o.serverURL = url }
}

// WithUserID sets the user identifier used for authentication (D-005).
func WithUserID(id string) ClientOption {
	return func(o *clientOptions) { o.userID = id }
}

// WithDeviceID sets the device identifier for this client.
// If not set, a random UUID is generated at client creation time (D-033).
func WithDeviceID(id string) ClientOption {
	return func(o *clientOptions) { o.deviceID = id }
}

// WithRPCTimeout sets the maximum duration for a single RPC call.
func WithRPCTimeout(d time.Duration) ClientOption {
	return func(o *clientOptions) { o.rpcTimeout = d }
}

// WithHeartbeatInterval sets the interval between heartbeat pings.
func WithHeartbeatInterval(d time.Duration) ClientOption {
	return func(o *clientOptions) { o.heartbeatInterval = d }
}

// WithSyncBatchSize sets the number of records fetched per sync pull batch.
func WithSyncBatchSize(n int) ClientOption {
	return func(o *clientOptions) { o.syncBatchSize = n }
}

// WithPullDebounce sets the debounce window for coalescing pull requests.
func WithPullDebounce(d time.Duration) ClientOption {
	return func(o *clientOptions) { o.pullDebounce = d }
}

// WithRetryBaseDelay sets the initial delay before the first RPC retry attempt.
func WithRetryBaseDelay(d time.Duration) ClientOption {
	return func(o *clientOptions) { o.retryBaseDelay = d }
}

// WithRetryMaxAttempts sets the maximum number of RPC retry attempts.
func WithRetryMaxAttempts(n int) ClientOption {
	return func(o *clientOptions) { o.retryMaxAttempts = n }
}

// WithRetryPollInterval sets the polling interval used during retry backoff.
func WithRetryPollInterval(d time.Duration) ClientOption {
	return func(o *clientOptions) { o.retryPollInterval = d }
}

// WithReconnectBaseDelay sets the initial delay before the first reconnect attempt.
func WithReconnectBaseDelay(d time.Duration) ClientOption {
	return func(o *clientOptions) { o.reconnectBaseDelay = d }
}

// WithReconnectMaxDelay caps the exponential reconnect backoff duration.
func WithReconnectMaxDelay(d time.Duration) ClientOption {
	return func(o *clientOptions) { o.reconnectMaxDelay = d }
}

// WithReconnectMaxRetries sets the maximum number of reconnect attempts.
// A value of 0 means unlimited.
//
// Deprecated: connectionMonitorWithInitialConnect retries indefinitely by
// design (D-044). This option has no runtime effect and is retained only for
// API compatibility.
func WithReconnectMaxRetries(n int) ClientOption {
	return func(o *clientOptions) { o.reconnectMaxRetries = n }
}

// WithDB provides the ClientDB instance used for local data persistence.
func WithDB(db *store.ClientDB) ClientOption {
	return func(o *clientOptions) { o.db = db }
}

// WithUpdateHandler sets the handler that receives processed data updates.
func WithUpdateHandler(h UpdateHandler) ClientOption {
	return func(o *clientOptions) { o.updateHandler = h }
}

// WithLogger sets the Logger used for client diagnostic output.
func WithLogger(l Logger) ClientOption {
	return func(o *clientOptions) { o.logger = l }
}

// WithIdempotencyCacheSize sets the maximum number of idempotency keys to cache.
func WithIdempotencyCacheSize(n int) ClientOption {
	return func(o *clientOptions) {
		o.idempotencyCacheSize = n
	}
}

// WithRTTWindowSize sets the sliding window size for RTT samples.
func WithRTTWindowSize(n int) ClientOption {
	return func(o *clientOptions) {
		o.rttWindowSize = n
	}
}

// WithAdaptiveTimeoutMin sets the minimum adaptive timeout.
func WithAdaptiveTimeoutMin(d time.Duration) ClientOption {
	return func(o *clientOptions) {
		o.adaptiveTimeoutMin = d
	}
}

// WithAdaptiveTimeoutMax sets the maximum adaptive timeout.
func WithAdaptiveTimeoutMax(d time.Duration) ClientOption {
	return func(o *clientOptions) {
		o.adaptiveTimeoutMax = d
	}
}

// WithResponseRetryMaxSize sets the maximum size of the response retry queue.
func WithResponseRetryMaxSize(n int) ClientOption {
	return func(o *clientOptions) {
		o.responseRetryMaxSize = n
	}
}

// WithResponseRetryMax sets the maximum retry attempts per response.
func WithResponseRetryMax(n int) ClientOption {
	return func(o *clientOptions) {
		o.responseRetryMax = n
	}
}

// WithDeviceName sets the human-readable device name for function registration.
func WithDeviceName(name string) ClientOption {
	return func(o *clientOptions) { o.deviceName = name }
}

// WithDeviceType sets the device type (e.g. "cli", "browser") for function registration.
func WithDeviceType(dtype string) ClientOption {
	return func(o *clientOptions) { o.deviceType = dtype }
}

// WithFunctions provides the list of functions to auto-register on connect/reconnect.
func WithFunctions(fns []protocol.FunctionInfo) ClientOption {
	return func(o *clientOptions) { o.functions = fns }
}
