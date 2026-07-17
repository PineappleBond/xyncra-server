package client

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/PineappleBond/xyncra-server/pkg/store"
)

// ---------------------------------------------------------------------------
// ClientError tests
// ---------------------------------------------------------------------------

func TestClientError_ErrorFormat(t *testing.T) {
	err := &ClientError{Code: ErrorCodeConnectionError, Message: "connection error"}
	want := "client: [-400] connection error"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestClientError_WithUnderlying(t *testing.T) {
	underlying := errors.New("dial tcp: connection refused")
	err := &ClientError{
		Code:    ErrorCodeConnectionError,
		Message: "connection error",
		Err:     underlying,
	}
	want := "client: [-400] connection error: dial tcp: connection refused"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestClientError_WithoutUnderlying(t *testing.T) {
	err := &ClientError{Code: ErrorCodeTimeoutError, Message: "timeout error"}
	want := "client: [-401] timeout error"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestClientError_Unwrap(t *testing.T) {
	underlying := errors.New("root cause")
	err := &ClientError{
		Code:    ErrorCodeSyncError,
		Message: "sync error",
		Err:     underlying,
	}
	if !errors.Is(err, underlying) {
		t.Error("errors.Is should match underlying error")
	}

	var ce *ClientError
	if !errors.As(err, &ce) {
		t.Error("errors.As should match *ClientError")
	}
	if ce.Code != ErrorCodeSyncError {
		t.Errorf("unwrapped code = %d, want %d", ce.Code, ErrorCodeSyncError)
	}
}

func TestClientError_UnwrapNil(t *testing.T) {
	err := &ClientError{Code: ErrorCodeConnectionError, Message: "no underlying"}
	if err.Unwrap() != nil {
		t.Error("Unwrap() should return nil when Err is nil")
	}
}

func TestNewConnectionError(t *testing.T) {
	underlying := errors.New("dial failed")
	err := NewConnectionError(underlying)
	if err.Code != ErrorCodeConnectionError {
		t.Errorf("Code = %d, want %d", err.Code, ErrorCodeConnectionError)
	}
	if err.Message != "connection error" {
		t.Errorf("Message = %q, want %q", err.Message, "connection error")
	}
	if !errors.Is(err, underlying) {
		t.Error("should wrap underlying error")
	}
}

func TestNewTimeoutError(t *testing.T) {
	underlying := errors.New("deadline exceeded")
	err := NewTimeoutError(underlying)
	if err.Code != ErrorCodeTimeoutError {
		t.Errorf("Code = %d, want %d", err.Code, ErrorCodeTimeoutError)
	}
	if err.Message != "timeout error" {
		t.Errorf("Message = %q, want %q", err.Message, "timeout error")
	}
}

func TestNewSyncError(t *testing.T) {
	underlying := errors.New("sync failed")
	err := NewSyncError(underlying)
	if err.Code != ErrorCodeSyncError {
		t.Errorf("Code = %d, want %d", err.Code, ErrorCodeSyncError)
	}
	if err.Message != "sync error" {
		t.Errorf("Message = %q, want %q", err.Message, "sync error")
	}
}

// ---------------------------------------------------------------------------
// Default constants tests
// ---------------------------------------------------------------------------

func TestDefaultConstants(t *testing.T) {
	tests := []struct {
		name string
		got  any
		want any
	}{
		{"defaultServerURL", defaultServerURL, "ws://localhost:8080/ws"},
		{"defaultWriteWait", defaultWriteWait, 10 * time.Second},
		{"defaultPongWait", defaultPongWait, 60 * time.Second},
		{"defaultPingPeriod", defaultPingPeriod, 54 * time.Second},
		{"defaultSendBufSize", defaultSendBufSize, 256},
		{"defaultMaxMessageSize", defaultMaxMessageSize, 64 * 1024},
		{"defaultRPCTimeout", defaultRPCTimeout, 30 * time.Second},
		{"defaultHeartbeatInterval", defaultHeartbeatInterval, 30 * time.Second},
		{"defaultSyncBatchSize", defaultSyncBatchSize, 100},
		{"defaultPullDebounce", defaultPullDebounce, 500 * time.Millisecond},
		{"defaultRetryBaseDelay", defaultRetryBaseDelay, 1 * time.Second},
		{"defaultRetryMaxAttempts", defaultRetryMaxAttempts, 5},
		{"defaultRetryPollInterval", defaultRetryPollInterval, 1 * time.Second},
		{"defaultReconnectBaseDelay", defaultReconnectBaseDelay, 1 * time.Second},
		{"defaultReconnectMaxDelay", defaultReconnectMaxDelay, 30 * time.Second},
		{"defaultReconnectMaxRetries", defaultReconnectMaxRetries, 0},
		{"defaultIdempotencyCacheSize", defaultIdempotencyCacheSize, 1024},
		{"defaultRTTWindowSize", defaultRTTWindowSize, 50},
		{"defaultAdaptiveTimeoutMin", defaultAdaptiveTimeoutMin, 5 * time.Second},
		{"defaultAdaptiveTimeoutMax", defaultAdaptiveTimeoutMax, 120 * time.Second},
		{"defaultResponseRetryMaxSize", defaultResponseRetryMaxSize, 100},
		{"defaultResponseRetryMax", defaultResponseRetryMax, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Functional option tests
// ---------------------------------------------------------------------------

func TestWithServerURL(t *testing.T) {
	var opts clientOptions
	WithServerURL("ws://example.com/ws")(&opts)
	if opts.serverURL != "ws://example.com/ws" {
		t.Errorf("serverURL = %q, want %q", opts.serverURL, "ws://example.com/ws")
	}
}

func TestWithUserID(t *testing.T) {
	var opts clientOptions
	WithUserID("user-123")(&opts)
	if opts.userID != "user-123" {
		t.Errorf("userID = %q, want %q", opts.userID, "user-123")
	}
}

func TestWithRPCTimeout(t *testing.T) {
	var opts clientOptions
	WithRPCTimeout(15 * time.Second)(&opts)
	if opts.rpcTimeout != 15*time.Second {
		t.Errorf("rpcTimeout = %v, want %v", opts.rpcTimeout, 15*time.Second)
	}
}

func TestWithHeartbeatInterval(t *testing.T) {
	var opts clientOptions
	WithHeartbeatInterval(10 * time.Second)(&opts)
	if opts.heartbeatInterval != 10*time.Second {
		t.Errorf("heartbeatInterval = %v, want %v", opts.heartbeatInterval, 10*time.Second)
	}
}

func TestWithSyncBatchSize(t *testing.T) {
	var opts clientOptions
	WithSyncBatchSize(50)(&opts)
	if opts.syncBatchSize != 50 {
		t.Errorf("syncBatchSize = %d, want %d", opts.syncBatchSize, 50)
	}
}

func TestWithPullDebounce(t *testing.T) {
	var opts clientOptions
	WithPullDebounce(200 * time.Millisecond)(&opts)
	if opts.pullDebounce != 200*time.Millisecond {
		t.Errorf("pullDebounce = %v, want %v", opts.pullDebounce, 200*time.Millisecond)
	}
}

func TestWithRetryOptions(t *testing.T) {
	var opts clientOptions
	WithRetryBaseDelay(2 * time.Second)(&opts)
	WithRetryMaxAttempts(10)(&opts)
	WithRetryPollInterval(500 * time.Millisecond)(&opts)

	if opts.retryBaseDelay != 2*time.Second {
		t.Errorf("retryBaseDelay = %v, want %v", opts.retryBaseDelay, 2*time.Second)
	}
	if opts.retryMaxAttempts != 10 {
		t.Errorf("retryMaxAttempts = %d, want %d", opts.retryMaxAttempts, 10)
	}
	if opts.retryPollInterval != 500*time.Millisecond {
		t.Errorf("retryPollInterval = %v, want %v", opts.retryPollInterval, 500*time.Millisecond)
	}
}

func TestWithReconnectOptions(t *testing.T) {
	var opts clientOptions
	WithReconnectBaseDelay(500 * time.Millisecond)(&opts)
	WithReconnectMaxDelay(1 * time.Minute)(&opts)
	WithReconnectMaxRetries(3)(&opts)

	if opts.reconnectBaseDelay != 500*time.Millisecond {
		t.Errorf("reconnectBaseDelay = %v, want %v", opts.reconnectBaseDelay, 500*time.Millisecond)
	}
	if opts.reconnectMaxDelay != 1*time.Minute {
		t.Errorf("reconnectMaxDelay = %v, want %v", opts.reconnectMaxDelay, 1*time.Minute)
	}
	if opts.reconnectMaxRetries != 3 {
		t.Errorf("reconnectMaxRetries = %d, want %d", opts.reconnectMaxRetries, 3)
	}
}

func TestWithIdempotencyCacheSize(t *testing.T) {
	var opts clientOptions
	WithIdempotencyCacheSize(2048)(&opts)
	if opts.idempotencyCacheSize != 2048 {
		t.Errorf("idempotencyCacheSize = %d, want %d", opts.idempotencyCacheSize, 2048)
	}
}

func TestWithRTTWindowSize(t *testing.T) {
	var opts clientOptions
	WithRTTWindowSize(100)(&opts)
	if opts.rttWindowSize != 100 {
		t.Errorf("rttWindowSize = %d, want %d", opts.rttWindowSize, 100)
	}
}

func TestWithAdaptiveTimeoutMin(t *testing.T) {
	var opts clientOptions
	WithAdaptiveTimeoutMin(10 * time.Second)(&opts)
	if opts.adaptiveTimeoutMin != 10*time.Second {
		t.Errorf("adaptiveTimeoutMin = %v, want %v", opts.adaptiveTimeoutMin, 10*time.Second)
	}
}

func TestWithAdaptiveTimeoutMax(t *testing.T) {
	var opts clientOptions
	WithAdaptiveTimeoutMax(60 * time.Second)(&opts)
	if opts.adaptiveTimeoutMax != 60*time.Second {
		t.Errorf("adaptiveTimeoutMax = %v, want %v", opts.adaptiveTimeoutMax, 60*time.Second)
	}
}

func TestWithResponseRetryMaxSize(t *testing.T) {
	var opts clientOptions
	WithResponseRetryMaxSize(200)(&opts)
	if opts.responseRetryMaxSize != 200 {
		t.Errorf("responseRetryMaxSize = %d, want %d", opts.responseRetryMaxSize, 200)
	}
}

func TestWithResponseRetryMax(t *testing.T) {
	var opts clientOptions
	WithResponseRetryMax(5)(&opts)
	if opts.responseRetryMax != 5 {
		t.Errorf("responseRetryMax = %d, want %d", opts.responseRetryMax, 5)
	}
}

func TestWithDB(t *testing.T) {
	db, err := store.NewInMemory("test_with_db")
	if err != nil {
		t.Fatalf("failed to create in-memory db: %v", err)
	}
	defer db.Close()

	var opts clientOptions
	WithDB(db)(&opts)
	if opts.db != db {
		t.Error("db not set correctly")
	}
}

func TestWithLogger(t *testing.T) {
	l := newStdLogger()
	var opts clientOptions
	WithLogger(l)(&opts)
	if opts.logger != l {
		t.Error("logger not set correctly")
	}
}

// ---------------------------------------------------------------------------
// stdLogger and formatArgs tests
// ---------------------------------------------------------------------------

func TestStdLogger(t *testing.T) {
	var buf bytes.Buffer
	slogger := slog.New(slog.NewTextHandler(&buf, nil))
	l := &stdLogger{logger: slogger}

	l.Info("hello", "key", "value")
	output := buf.String()
	if !strings.Contains(output, "hello") {
		t.Errorf("Info output = %q, want to contain hello", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Errorf("Info output = %q, want to contain key=value", output)
	}

	buf.Reset()
	l.Error("oops", "code", 500)
	output = buf.String()
	if !strings.Contains(output, "oops") {
		t.Errorf("Error output = %q, want to contain oops", output)
	}

	buf.Reset()
	l.Debug("trace")
	output = buf.String()
	if !strings.Contains(output, "trace") {
		t.Errorf("Debug output = %q, want to contain trace", output)
	}
}

func TestFormatArgs(t *testing.T) {
	tests := []struct {
		name string
		args []any
		want string
	}{
		{"empty", nil, ""},
		{"single_pair", []any{"key", "value"}, " key=value"},
		{"multiple_pairs", []any{"a", 1, "b", 2}, " a=1 b=2"},
		{"odd_args", []any{"key1", "val1", "key2"}, " key1=val1 key2=MISSING"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatArgs(tc.args)
			if got != tc.want {
				t.Errorf("formatArgs(%v) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ErrorCode constants
// ---------------------------------------------------------------------------

func TestErrorCodeConstants(t *testing.T) {
	if ErrorCodeConnectionError != protocol.ResponseCode(-400) {
		t.Errorf("ErrorCodeConnectionError = %d, want -400", ErrorCodeConnectionError)
	}
	if ErrorCodeTimeoutError != protocol.ResponseCode(-401) {
		t.Errorf("ErrorCodeTimeoutError = %d, want -401", ErrorCodeTimeoutError)
	}
	if ErrorCodeSyncError != protocol.ResponseCode(-402) {
		t.Errorf("ErrorCodeSyncError = %d, want -402", ErrorCodeSyncError)
	}
}
