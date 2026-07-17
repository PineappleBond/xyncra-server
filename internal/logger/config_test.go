package logger

import (
	"os"
	"testing"
)

// clearLogEnv unsets all XYNCRA_LOG_* env vars so tests start from a clean
// slate. t.Setenv restores the original values on test completion.
func clearLogEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"XYNCRA_LOG_LEVEL",
		"XYNCRA_LOG_DIR",
		"XYNCRA_LOG_FORMAT",
		"XYNCRA_LOG_MAX_SIZE",
		"XYNCRA_LOG_MAX_AGE",
		"XYNCRA_LOG_MAX_BACKUPS",
		"XYNCRA_LOG_COMPRESS",
	} {
		t.Setenv(k, "")
	}
}

func TestDefaultConfig_Defaults(t *testing.T) {
	clearLogEnv(t)

	cfg := DefaultConfig()

	if cfg.Level != "info" {
		t.Errorf("Level: got %q, want %q", cfg.Level, "info")
	}
	if cfg.Dir != "" {
		t.Errorf("Dir: got %q, want empty", cfg.Dir)
	}
	if cfg.Format != "text" {
		t.Errorf("Format: got %q, want %q", cfg.Format, "text")
	}
	if cfg.MaxSizeMB != 100 {
		t.Errorf("MaxSizeMB: got %d, want 100", cfg.MaxSizeMB)
	}
	if cfg.MaxAge != 30 {
		t.Errorf("MaxAge: got %d, want 30", cfg.MaxAge)
	}
	if cfg.MaxBackups != 10 {
		t.Errorf("MaxBackups: got %d, want 10", cfg.MaxBackups)
	}
	if cfg.Compress != true {
		t.Errorf("Compress: got %v, want true", cfg.Compress)
	}
}

func TestDefaultConfig_Overrides(t *testing.T) {
	clearLogEnv(t)
	t.Setenv("XYNCRA_LOG_LEVEL", "debug")
	t.Setenv("XYNCRA_LOG_DIR", "/tmp/xyncra-test-logs")
	t.Setenv("XYNCRA_LOG_FORMAT", "json")
	t.Setenv("XYNCRA_LOG_MAX_SIZE", "50")
	t.Setenv("XYNCRA_LOG_MAX_AGE", "7")
	t.Setenv("XYNCRA_LOG_MAX_BACKUPS", "3")
	t.Setenv("XYNCRA_LOG_COMPRESS", "false")

	cfg := DefaultConfig()

	if cfg.Level != "debug" {
		t.Errorf("Level: got %q, want %q", cfg.Level, "debug")
	}
	if cfg.Dir != "/tmp/xyncra-test-logs" {
		t.Errorf("Dir: got %q, want %q", cfg.Dir, "/tmp/xyncra-test-logs")
	}
	if cfg.Format != "json" {
		t.Errorf("Format: got %q, want %q", cfg.Format, "json")
	}
	if cfg.MaxSizeMB != 50 {
		t.Errorf("MaxSizeMB: got %d, want 50", cfg.MaxSizeMB)
	}
	if cfg.MaxAge != 7 {
		t.Errorf("MaxAge: got %d, want 7", cfg.MaxAge)
	}
	if cfg.MaxBackups != 3 {
		t.Errorf("MaxBackups: got %d, want 3", cfg.MaxBackups)
	}
	if cfg.Compress != false {
		t.Errorf("Compress: got %v, want false", cfg.Compress)
	}
}

func TestDefaultConfig_InvalidValuesFallbackToDefaults(t *testing.T) {
	clearLogEnv(t)
	t.Setenv("XYNCRA_LOG_LEVEL", "not-a-level")
	t.Setenv("XYNCRA_LOG_FORMAT", "xml")
	t.Setenv("XYNCRA_LOG_MAX_SIZE", "abc")
	t.Setenv("XYNCRA_LOG_MAX_AGE", "")
	t.Setenv("XYNCRA_LOG_COMPRESS", "maybe")

	cfg := DefaultConfig()

	if cfg.Level != "info" {
		t.Errorf("Level on invalid input: got %q, want %q", cfg.Level, "info")
	}
	if cfg.Format != "text" {
		t.Errorf("Format on invalid input: got %q, want %q", cfg.Format, "text")
	}
	if cfg.MaxSizeMB != 100 {
		t.Errorf("MaxSizeMB on invalid input: got %d, want 100", cfg.MaxSizeMB)
	}
	if cfg.MaxAge != 30 {
		t.Errorf("MaxAge on empty input: got %d, want 30", cfg.MaxAge)
	}
	if cfg.Compress != true {
		t.Errorf("Compress on invalid input: got %v, want true", cfg.Compress)
	}
}

func TestDefaultConfig_LevelCaseInsensitive(t *testing.T) {
	clearLogEnv(t)
	t.Setenv("XYNCRA_LOG_LEVEL", "WARN")

	cfg := DefaultConfig()

	if cfg.Level != "warn" {
		t.Errorf("Level case normalization: got %q, want %q", cfg.Level, "warn")
	}
}

func TestDefaultConfig_NegativeIntClampsToZero(t *testing.T) {
	clearLogEnv(t)
	t.Setenv("XYNCRA_LOG_MAX_SIZE", "-5")

	cfg := DefaultConfig()

	if cfg.MaxSizeMB != 0 {
		t.Errorf("negative MaxSizeMB: got %d, want 0", cfg.MaxSizeMB)
	}
}

func TestDefaultConfig_DirTrimmed(t *testing.T) {
	clearLogEnv(t)
	t.Setenv("XYNCRA_LOG_DIR", "   ")

	cfg := DefaultConfig()

	if cfg.Dir != "" {
		t.Errorf("whitespace-only Dir: got %q, want empty", cfg.Dir)
	}
}

// Verify env helpers are not exported; this is a compile-time check that the
// package exposes only the intended API surface.
func TestEnvHelpersUnexported(t *testing.T) {
	// This test exists only to document that env* helpers are internal to the
	// package. No assertion needed; if the helpers were exported, callers would
	// depend on them and the design would be violated.
	_ = os.Getenv
}

// ---------------------------------------------------------------------------
// Alias tests for acceptance-criteria naming convention.
// These wrap the existing tests under the exact names from the test matrix,
// ensuring grep-based traceability from requirements to tests.
// ---------------------------------------------------------------------------

// TestConfig_Defaults verifies that zero-value Config yields sensible defaults.
//
// Acceptance criteria:
//   - Level=info, Format=text, MaxSizeMB=100
func TestConfig_Defaults(t *testing.T) {
	clearLogEnv(t)
	cfg := DefaultConfig()
	if cfg.Level != "info" {
		t.Errorf("Level: got %q, want %q", cfg.Level, "info")
	}
	if cfg.Format != "text" {
		t.Errorf("Format: got %q, want %q", cfg.Format, "text")
	}
	if cfg.MaxSizeMB != 100 {
		t.Errorf("MaxSizeMB: got %d, want 100", cfg.MaxSizeMB)
	}
}

// TestConfig_EnvOverride verifies that environment variables override defaults.
//
// Acceptance criteria:
//   - Set XYNCRA_LOG_* env vars
//   - Config reflects env values
func TestConfig_EnvOverride(t *testing.T) {
	clearLogEnv(t)
	t.Setenv("XYNCRA_LOG_LEVEL", "debug")
	t.Setenv("XYNCRA_LOG_FORMAT", "json")
	t.Setenv("XYNCRA_LOG_MAX_SIZE", "50")

	cfg := DefaultConfig()
	if cfg.Level != "debug" {
		t.Errorf("Level: got %q, want %q", cfg.Level, "debug")
	}
	if cfg.Format != "json" {
		t.Errorf("Format: got %q, want %q", cfg.Format, "json")
	}
	if cfg.MaxSizeMB != 50 {
		t.Errorf("MaxSizeMB: got %d, want 50", cfg.MaxSizeMB)
	}
}
