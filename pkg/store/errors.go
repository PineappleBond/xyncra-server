// Package store provides the client-side data access layer for the Xyncra
// messaging system, backed by SQLite via GORM.
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

	// ErrDatabaseLocked indicates the SQLite database is locked by another writer.
	ErrDatabaseLocked = errors.New("store: database is locked")
)

// classifyError translates GORM and driver-level errors into store-level errors.
// It matches human-readable fragments common to SQLite error messages.
func classifyError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	msg := err.Error()

	// Duplicate key patterns.
	if strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "duplicate key") {
		return ErrDuplicateKey
	}

	// Foreign key violation patterns.
	if strings.Contains(msg, "FOREIGN KEY constraint failed") {
		return ErrForeignKeyViolation
	}

	// Database locked patterns (SQLite-specific).
	if strings.Contains(msg, "database is locked") {
		return ErrDatabaseLocked
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
