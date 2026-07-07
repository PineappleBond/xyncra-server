package store

import (
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	gsqlite "github.com/glebarez/sqlite"
)

// DatabaseConfig holds the configuration for the database connection.
type DatabaseConfig struct {
	// Driver is the database driver name: "postgres", "mysql", or "sqlite".
	Driver string

	// DSN is the data source name (connection string).
	DSN string

	// MaxIdleConns is the maximum number of idle connections in the pool.
	// A value of 0 means no idle connections are retained. Default: 5.
	MaxIdleConns int

	// MaxOpenConns is the maximum number of open connections to the database.
	// A value of 0 means unlimited open connections. Default: 25.
	MaxOpenConns int

	// ConnMaxLifetime is the maximum amount of time a connection may be reused.
	// A value of 0 means connections are reused forever.
	ConnMaxLifetime time.Duration

	// SlowQueryThreshold is the duration after which a query is logged as slow.
	// A value of 0 disables slow query logging. Default: 200ms.
	SlowQueryThreshold time.Duration
}

// Database wraps a *gorm.DB connection and provides connection lifecycle management.
type Database struct {
	db *gorm.DB
}

// NewDatabase opens a database connection using the provided configuration,
// configures the connection pool, and returns a Database instance.
// It returns an error if the driver is unsupported or the connection fails.
func NewDatabase(cfg DatabaseConfig) (*Database, error) {
	dialector, err := openDriver(cfg.Driver, cfg.DSN)
	if err != nil {
		return nil, err
	}

	// Configure GORM logger with slow query support.
	slowThreshold := cfg.SlowQueryThreshold
	if slowThreshold == 0 {
		slowThreshold = 200 * time.Millisecond
	}
	gormLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             slowThreshold,
			LogLevel:                  logger.Info,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: gormLogger,
	})
	if err != nil {
		return nil, fmt.Errorf("store: failed to open database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("store: failed to get underlying db: %w", err)
	}

	// Apply connection pool defaults (reduced from 100/10 to 25/5).
	maxIdle := cfg.MaxIdleConns
	if maxIdle == 0 {
		maxIdle = 5
	}
	maxOpen := cfg.MaxOpenConns
	if maxOpen == 0 {
		maxOpen = 25
	}
	sqlDB.SetMaxIdleConns(maxIdle)
	sqlDB.SetMaxOpenConns(maxOpen)
	if cfg.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}

	return &Database{db: db}, nil
}

// DB returns the underlying *gorm.DB instance.
func (d *Database) DB() *gorm.DB {
	return d.db
}

// Close closes the underlying database connection pool.
func (d *Database) Close() error {
	sqlDB, err := d.db.DB()
	if err != nil {
		return fmt.Errorf("store: failed to get underlying db: %w", err)
	}
	return sqlDB.Close()
}

// openDriver returns the appropriate GORM dialector for the given driver name and DSN.
func openDriver(driver, dsn string) (gorm.Dialector, error) {
	switch driver {
	case "postgres", "postgresql":
		return postgres.Open(dsn), nil
	case "mysql":
		return mysql.Open(dsn), nil
	case "sqlite", "sqlite3":
		return gsqlite.Open(dsn), nil
	default:
		return nil, fmt.Errorf("store: unsupported database driver: %s", driver)
	}
}

// Tx represents a database transaction handle.
type Tx struct {
	tx *gorm.DB
}

// Commit commits the transaction.
func (t *Tx) Commit() error {
	if err := t.tx.Commit().Error; err != nil {
		return fmt.Errorf("store: commit transaction: %w", err)
	}
	return nil
}

// Rollback aborts the transaction.
func (t *Tx) Rollback() {
	t.tx.Rollback()
}

// DB returns the *gorm.DB scoped to this transaction.
func (t *Tx) DB() *gorm.DB {
	return t.tx
}
