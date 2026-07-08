package protocol

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test 1: HandlerError.Error() returns Message
// ---------------------------------------------------------------------------

func TestHandlerError_Error(t *testing.T) {
	err := &HandlerError{Code: ResponseCodeNotFound, Message: "user not found"}
	assert.Equal(t, "user not found", err.Error())
}

// ---------------------------------------------------------------------------
// Test 2: HandlerError.Error() includes wrapped error info
// ---------------------------------------------------------------------------

func TestHandlerError_ErrorWithWrapped(t *testing.T) {
	cause := errors.New("database timeout")
	err := &HandlerError{Code: ResponseCodeInternalError, Message: "internal error", Err: cause}
	assert.Contains(t, err.Error(), "internal error")
	assert.Contains(t, err.Error(), "database timeout")
}

// ---------------------------------------------------------------------------
// Test 3: HandlerError.Unwrap() returns the underlying error
// ---------------------------------------------------------------------------

func TestHandlerError_Unwrap(t *testing.T) {
	cause := errors.New("database timeout")
	err := &HandlerError{Code: ResponseCodeInternalError, Message: "internal error", Err: cause}
	assert.Equal(t, cause, err.Unwrap())
}

// ---------------------------------------------------------------------------
// Test 4: HandlerError.Unwrap() returns nil when no underlying error
// ---------------------------------------------------------------------------

func TestHandlerError_UnwrapNil(t *testing.T) {
	err := &HandlerError{Code: ResponseCodeNotFound, Message: "user not found"}
	assert.Nil(t, err.Unwrap())
}

// ---------------------------------------------------------------------------
// Test 5: errors.As extracts HandlerError from a direct error
// ---------------------------------------------------------------------------

func TestErrorsAs_HandlerError(t *testing.T) {
	err := NewNotFoundError("user not found")
	var handlerErr *HandlerError
	require.True(t, errors.As(err, &handlerErr))
	assert.Equal(t, ResponseCodeNotFound, handlerErr.Code)
	assert.Equal(t, "user not found", handlerErr.Message)
}

// ---------------------------------------------------------------------------
// Test 6: errors.As extracts HandlerError from a wrapped chain
// ---------------------------------------------------------------------------

func TestErrorsAs_WrappedHandlerError(t *testing.T) {
	inner := NewNotFoundError("user not found")
	wrapped := fmt.Errorf("operation failed: %w", inner)
	var handlerErr *HandlerError
	require.True(t, errors.As(wrapped, &handlerErr))
	assert.Equal(t, ResponseCodeNotFound, handlerErr.Code)
}

// ---------------------------------------------------------------------------
// Test 7: NewValidationError returns Code -100
// ---------------------------------------------------------------------------

func TestNewValidationError(t *testing.T) {
	err := NewValidationError("missing field")
	assert.Equal(t, ResponseCodeValidationError, err.Code)
	assert.Equal(t, "missing field", err.Message)
}

// ---------------------------------------------------------------------------
// Test 8: NewNotFoundError returns Code -101
// ---------------------------------------------------------------------------

func TestNewNotFoundError(t *testing.T) {
	err := NewNotFoundError("not found")
	assert.Equal(t, ResponseCodeNotFound, err.Code)
	assert.Equal(t, "not found", err.Message)
}

// ---------------------------------------------------------------------------
// Test 9: NewDuplicateError returns Code -102
// ---------------------------------------------------------------------------

func TestNewDuplicateError(t *testing.T) {
	err := NewDuplicateError("already exists")
	assert.Equal(t, ResponseCodeDuplicate, err.Code)
	assert.Equal(t, "already exists", err.Message)
}

// ---------------------------------------------------------------------------
// Test 10: NewPermissionDeniedError returns Code -200
// ---------------------------------------------------------------------------

func TestNewPermissionDeniedError(t *testing.T) {
	err := NewPermissionDeniedError("forbidden")
	assert.Equal(t, ResponseCodePermissionDenied, err.Code)
	assert.Equal(t, "forbidden", err.Message)
}

// ---------------------------------------------------------------------------
// Test 11: NewInternalError returns Code -300 and wraps the original error
// ---------------------------------------------------------------------------

func TestNewInternalError(t *testing.T) {
	cause := errors.New("disk full")
	err := NewInternalError(cause)
	assert.Equal(t, ResponseCodeInternalError, err.Code)
	assert.ErrorIs(t, err, cause)
}

// ---------------------------------------------------------------------------
// Test 12: NewInternalError — errors.Is can find the wrapped cause
// ---------------------------------------------------------------------------

func TestNewInternalError_WrapsError(t *testing.T) {
	cause := errors.New("database timeout")
	err := NewInternalError(cause)
	assert.Equal(t, ResponseCodeInternalError, err.Code)
	assert.ErrorIs(t, err, cause)
}
