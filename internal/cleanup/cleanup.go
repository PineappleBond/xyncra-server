// Package cleanup provides background maintenance tasks for the Xyncra server.
//
// The primary component is UserUpdateCleaner, which periodically removes expired
// UserUpdate records to keep storage bounded (see PRODUCT_DECISIONS.md D-016).
package cleanup

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/store"
)

// DefaultInterval is the default cleanup interval (1 hour).
const DefaultInterval = 1 * time.Hour

// Config holds configuration for the UserUpdateCleaner.
type Config struct {
	// Interval is the time between cleanup runs. Defaults to DefaultInterval
	// (1 hour) if zero.
	Interval time.Duration

	// Store provides access to the UserUpdate data. Must not be nil.
	Store *store.UserUpdateStore

	// Logger is used to report cleanup results. If nil, a default logger
	// writing to stderr with prefix "[cleanup] " is used.
	Logger *log.Logger
}

// UserUpdateCleaner periodically removes expired UserUpdate records from the
// database. Expired records are those older than store.DefaultCleanupRetention
// (30 days). See PRODUCT_DECISIONS.md D-016 for the rationale.
type UserUpdateCleaner struct {
	config Config
}

// NewUserUpdateCleaner creates a UserUpdateCleaner with the given config.
// Zero-value fields in cfg are filled with sensible defaults:
//
//   - Interval: DefaultInterval (1 hour)
//   - Logger:   stderr logger with prefix "[cleanup] "
func NewUserUpdateCleaner(cfg Config) *UserUpdateCleaner {
	if cfg.Store == nil {
		panic("cleanup: Store must not be nil")
	}
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultInterval
	}
	if cfg.Logger == nil {
		cfg.Logger = log.New(os.Stderr, "[cleanup] ", log.LstdFlags)
	}
	return &UserUpdateCleaner{config: cfg}
}

// Run starts the cleanup loop. It blocks until ctx is cancelled.
//
// On each tick, Run calls Store.CleanupExpired(ctx) and logs the outcome:
//
//   - On success with rows deleted > 0: logs the number of deleted rows.
//   - On success with rows deleted == 0: no log output (nothing to clean).
//   - On failure: logs the error.
//
// Cleanup failures are logged but do not interrupt the loop, consistent with
// the fire-and-forget philosophy (PRODUCT_DECISIONS.md D-007).
//
// The first cleanup does not run immediately; Run waits for the first tick.
func (c *UserUpdateCleaner) Run(ctx context.Context) {
	ticker := time.NewTicker(c.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			func() {
				defer func() {
					if r := recover(); r != nil {
						c.config.Logger.Printf("cleanup panic recovered: %v", r)
					}
				}()
				c.runOnce(ctx)
			}()
		}
	}
}

// runOnce executes a single cleanup cycle.
func (c *UserUpdateCleaner) runOnce(ctx context.Context) {
	deleted, err := c.config.Store.CleanupExpired(ctx)
	if err != nil {
		c.config.Logger.Printf("cleanup failed: %v", err)
		return
	}
	if deleted > 0 {
		c.config.Logger.Printf("cleaned up %d expired user update(s)", deleted)
	}
}
