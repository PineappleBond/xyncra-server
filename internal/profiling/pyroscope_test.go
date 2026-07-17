package profiling

import (
	"testing"
)

func TestStartPyroscope_Disabled(t *testing.T) {
	// When Enabled is false, StartPyroscope should return nil cleanup, nil error.
	cfg := PyroscopeConfig{
		Enabled: false,
		Server:  "http://localhost:4040",
		AppName: "test-app",
	}

	cleanup, err := StartPyroscope(cfg)
	if err != nil {
		t.Fatalf("expected nil error when disabled, got: %v", err)
	}
	if cleanup != nil {
		t.Fatal("expected nil cleanup when disabled")
	}
}

func TestStartPyroscope_NoServer(t *testing.T) {
	// When Enabled but Server is empty, should return nil cleanup, nil error.
	cfg := PyroscopeConfig{
		Enabled: true,
		Server:  "",
		AppName: "test-app",
	}

	cleanup, err := StartPyroscope(cfg)
	if err != nil {
		t.Fatalf("expected nil error when server empty, got: %v", err)
	}
	if cleanup != nil {
		t.Fatal("expected nil cleanup when server empty")
	}
}

func TestStartPyroscope_UnreachableServer(t *testing.T) {
	// When server is unreachable but URL is valid, pyroscope.Start still succeeds
	// (it's designed to be resilient and buffer data). The cleanup function should
	// be callable without panic.
	cfg := PyroscopeConfig{
		Enabled: true,
		Server:  "http://127.0.0.1:1", // Port 1 is almost certainly unreachable
		AppName: "test-app",
	}

	// Should not panic.
	cleanup, err := StartPyroscope(cfg)
	if err != nil {
		t.Fatalf("expected nil error (fail-silent), got: %v", err)
	}
	// Pyroscope is resilient - it starts even if server is unreachable.
	// The cleanup function should be non-nil and callable.
	if cleanup == nil {
		t.Fatal("expected non-nil cleanup (pyroscope is resilient to unreachable servers)")
	}
	// Cleanup should not panic.
	cleanup()
}

func TestDefaultPyroscopeConfig_EnvVars(t *testing.T) {
	t.Setenv("XYNCRA_PROFILING_ENABLED", "true")
	t.Setenv("XYNCRA_PROFILING_SERVER", "http://pyroscope:4040")
	t.Setenv("XYNCRA_PROFILING_APP_NAME", "custom-app")

	cfg := DefaultPyroscopeConfig()
	if !cfg.Enabled {
		t.Fatal("expected Enabled=true from env")
	}
	if cfg.Server != "http://pyroscope:4040" {
		t.Fatalf("expected server 'http://pyroscope:4040', got %q", cfg.Server)
	}
	if cfg.AppName != "custom-app" {
		t.Fatalf("expected app name 'custom-app', got %q", cfg.AppName)
	}
}

func TestDefaultPyroscopeConfig_Defaults(t *testing.T) {
	t.Setenv("XYNCRA_PROFILING_ENABLED", "")
	t.Setenv("XYNCRA_PROFILING_SERVER", "")
	t.Setenv("XYNCRA_PROFILING_APP_NAME", "")

	cfg := DefaultPyroscopeConfig()
	if cfg.Enabled {
		t.Fatal("expected Enabled=false by default")
	}
	if cfg.Server != "" {
		t.Fatalf("expected empty server by default, got %q", cfg.Server)
	}
	if cfg.AppName != "xyncra-server" {
		t.Fatalf("expected default app name 'xyncra-server', got %q", cfg.AppName)
	}
}

func TestFormatPyroscopeStatus(t *testing.T) {
	tests := []struct {
		name     string
		cfg      PyroscopeConfig
		expected string
	}{
		{
			name:     "disabled",
			cfg:      PyroscopeConfig{Enabled: false},
			expected: "disabled",
		},
		{
			name:     "enabled no server",
			cfg:      PyroscopeConfig{Enabled: true, Server: ""},
			expected: "enabled but no server configured",
		},
		{
			name:     "enabled with server",
			cfg:      PyroscopeConfig{Enabled: true, Server: "http://pyroscope:4040", AppName: "myapp"},
			expected: "enabled (server=http://pyroscope:4040, app=myapp)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatPyroscopeStatus(tt.cfg)
			if got != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}
