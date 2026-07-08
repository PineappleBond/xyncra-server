package protocol

import "fmt"

// Error codes for API responses.
// Segments:
//
//	-100 to -199: client errors (validation, not_found, duplicate)
//	-200 to -299: permission errors (permission_denied, forbidden)
//	-300 to -399: server errors (internal, unavailable)
const (
	// Client errors (-100s)
	ResponseCodeValidationError ResponseCode = -100
	ResponseCodeNotFound        ResponseCode = -101
	ResponseCodeDuplicate       ResponseCode = -102

	// Permission errors (-200s)
	ResponseCodePermissionDenied ResponseCode = -200
	ResponseCodeForbidden        ResponseCode = -201

	// Server errors (-300s)
	ResponseCodeInternalError ResponseCode = -300
	ResponseCodeUnavailable   ResponseCode = -301
)

// HandlerError is a typed error that carries a ResponseCode.
// Handlers return HandlerError to communicate structured errors to clients.
type HandlerError struct {
	Code    ResponseCode
	Message string
	Err     error // underlying error, for wrapping
}

func (e *HandlerError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *HandlerError) Unwrap() error { return e.Err }

// NewHandlerError creates a HandlerError with the given code and message.
func NewHandlerError(code ResponseCode, message string) *HandlerError {
	return &HandlerError{Code: code, Message: message}
}

// WrapError wraps an existing error as a HandlerError.
func WrapError(code ResponseCode, err error) *HandlerError {
	return &HandlerError{Code: code, Message: err.Error(), Err: err}
}

// NewValidationError creates a -100 error for parameter validation failures.
func NewValidationError(msg string) *HandlerError {
	return NewHandlerError(ResponseCodeValidationError, msg)
}

// NewNotFoundError creates a -101 error for missing resources.
func NewNotFoundError(msg string) *HandlerError {
	return NewHandlerError(ResponseCodeNotFound, msg)
}

// NewDuplicateError creates a -102 error for duplicate resources.
func NewDuplicateError(msg string) *HandlerError {
	return NewHandlerError(ResponseCodeDuplicate, msg)
}

// NewPermissionDeniedError creates a -200 error for authorization failures.
func NewPermissionDeniedError(msg string) *HandlerError {
	return NewHandlerError(ResponseCodePermissionDenied, msg)
}

// NewInternalError wraps an unexpected error as a -300 error.
func NewInternalError(err error) *HandlerError {
	return WrapError(ResponseCodeInternalError, err)
}
