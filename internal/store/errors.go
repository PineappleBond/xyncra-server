// Package store provides the data access layer for the Xyncra messaging system.
package store

import (
	"errors"
	"strings"

	"gorm.io/gorm"
)

// Standard errors returned by store operations.
var (
	// ErrNotFound indicates that the requested record does not exist.
	ErrNotFound = errors.New("store: record not found")

	// ErrDuplicateKey indicates a unique constraint violation.
	ErrDuplicateKey = errors.New("store: duplicate key")

	// ErrForeignKeyViolation indicates a foreign key constraint violation.
	ErrForeignKeyViolation = errors.New("store: foreign key violation")

	// ErrConnectionFailed indicates a database connection failure.
	ErrConnectionFailed = errors.New("store: connection failed")

	// ErrContextDeadlineExceeded indicates the context deadline was exceeded.
	ErrContextDeadlineExceeded = errors.New("store: context deadline exceeded")
)

// classifyError translates GORM and driver-level errors into store-level errors.
// It matches human-readable fragments common to PostgreSQL, MySQL, and SQLite
// error messages. Numeric MySQL error codes (1062, 1451, 1452) are omitted
// because the corresponding text patterns already cover them, and bare numbers
// risk false positives in unrelated error messages.
func classifyError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	msg := err.Error()

	// Duplicate key patterns across dialects.
	if strings.Contains(msg, "duplicate key") || // PostgreSQL
		strings.Contains(msg, "UNIQUE constraint failed") || // SQLite
		strings.Contains(msg, "Duplicate entry") { // MySQL
		return ErrDuplicateKey
	}

	// Foreign key violation patterns across dialects.
	if strings.Contains(msg, "foreign key constraint") || // PostgreSQL
		strings.Contains(msg, "FOREIGN KEY constraint failed") || // SQLite
		strings.Contains(msg, "Cannot add or update a child row") || // MySQL 1452
		strings.Contains(msg, "Cannot delete or update a parent row") { // MySQL 1451
		return ErrForeignKeyViolation
	}

	// Connection failure patterns.
	if strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "dial tcp") {
		return ErrConnectionFailed
	}

	// Context deadline patterns.
	if strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "Query timed out") {
		return ErrContextDeadlineExceeded
	}

	return err
}
