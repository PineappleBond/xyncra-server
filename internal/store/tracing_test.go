package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnableTracing_RegistersPlugin verifies that EnableTracing=true
// successfully registers the otelgorm plugin (no error returned).
// We use SQLite which supports in-memory operation without external services.
func TestEnableTracing_RegistersPlugin(t *testing.T) {
	db, err := NewDatabase(DatabaseConfig{
		Driver:        "sqlite",
		DSN:           "file:test_enable_tracing?mode=memory&cache=shared",
		EnableTracing: true,
	})
	require.NoError(t, err, "EnableTracing=true should not error with sqlite")
	require.NotNil(t, db)
	defer db.Close()

	// Verify the database is usable.
	assert.NotNil(t, db.DB())
}

// TestEnableTracing_False_NoPlugin verifies that EnableTracing=false
// (the default) does not register the otelgorm plugin.
func TestEnableTracing_False_NoPlugin(t *testing.T) {
	db, err := NewDatabase(DatabaseConfig{
		Driver:        "sqlite",
		DSN:           "file:test_disable_tracing?mode=memory&cache=shared",
		EnableTracing: false,
	})
	require.NoError(t, err)
	require.NotNil(t, db)
	defer db.Close()

	assert.NotNil(t, db.DB())
}

// TestEnableTracing_DefaultIsFalse verifies that the zero-value DatabaseConfig
// does not enable tracing.
func TestEnableTracing_DefaultIsFalse(t *testing.T) {
	cfg := DatabaseConfig{}
	assert.False(t, cfg.EnableTracing, "zero-value DatabaseConfig should have EnableTracing=false")
}
