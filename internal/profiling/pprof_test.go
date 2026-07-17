package profiling

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// freePort asks the OS for a free TCP port on 127.0.0.1 and returns it.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

// waitForServer polls the given URL until it responds or timeout expires.
func waitForServer(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 1 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("server at %s did not start within %v", url, timeout)
}

func TestStartPprof_Disabled(t *testing.T) {
	// When Enabled is false, StartPprof should return nil immediately.
	cfg := PprofConfig{Enabled: false, Addr: "127.0.0.1:0"}
	err := StartPprof(context.Background(), cfg)
	if err != nil {
		t.Fatalf("expected nil error when disabled, got: %v", err)
	}
}

func TestStartPprof_ServesDebugEndpoints(t *testing.T) {
	port := freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	cfg := PprofConfig{Enabled: true, Addr: addr}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- StartPprof(ctx, cfg)
	}()

	baseURL := fmt.Sprintf("http://%s", addr)
	if err := waitForServer(baseURL+"/debug/pprof/", 3*time.Second); err != nil {
		t.Fatal(err)
	}

	// Test /debug/pprof/ returns 200.
	resp, err := http.Get(baseURL + "/debug/pprof/")
	if err != nil {
		t.Fatalf("GET /debug/pprof/ failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Test /debug/pprof/heap returns data.
	resp2, err := http.Get(baseURL + "/debug/pprof/heap")
	if err != nil {
		t.Fatalf("GET /debug/pprof/heap failed: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for heap, got %d", resp2.StatusCode)
	}
	body, _ := io.ReadAll(resp2.Body)
	if len(body) == 0 {
		t.Fatal("expected non-empty heap profile data")
	}

	// Test /debug/pprof/cmdline.
	resp3, err := http.Get(baseURL + "/debug/pprof/cmdline")
	if err != nil {
		t.Fatalf("GET /debug/pprof/cmdline failed: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for cmdline, got %d", resp3.StatusCode)
	}

	// Test /debug/pprof/symbol.
	resp4, err := http.Get(baseURL + "/debug/pprof/symbol")
	if err != nil {
		t.Fatalf("GET /debug/pprof/symbol failed: %v", err)
	}
	defer resp4.Body.Close()
	if resp4.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for symbol, got %d", resp4.StatusCode)
	}

	// Cancel context and verify server shuts down.
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected error after shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pprof server did not shut down within timeout")
	}

	// After shutdown, the server should refuse connections.
	_, err = http.Get(baseURL + "/debug/pprof/")
	if err == nil {
		t.Fatal("expected error after server shutdown, got nil")
	}
}

func TestStartPprof_DefaultAddrIsLocalhost(t *testing.T) {
	// Verify DefaultPprofConfig binds to 127.0.0.1, not 0.0.0.0.
	cfg := DefaultPprofConfig()
	if cfg.Addr != "127.0.0.1:6060" {
		t.Fatalf("expected default addr '127.0.0.1:6060', got %q", cfg.Addr)
	}
}

func TestDefaultPprofConfig_EnvVars(t *testing.T) {
	// Test that environment variables are respected.
	t.Setenv("XYNCRA_PPROF_ENABLED", "true")
	t.Setenv("XYNCRA_PPROF_ADDR", "127.0.0.1:7070")

	cfg := DefaultPprofConfig()
	if !cfg.Enabled {
		t.Fatal("expected Enabled=true from env")
	}
	if cfg.Addr != "127.0.0.1:7070" {
		t.Fatalf("expected addr '127.0.0.1:7070', got %q", cfg.Addr)
	}
}

func TestDefaultPprofConfig_Defaults(t *testing.T) {
	// Clear env vars to test defaults.
	t.Setenv("XYNCRA_PPROF_ENABLED", "")
	t.Setenv("XYNCRA_PPROF_ADDR", "")

	cfg := DefaultPprofConfig()
	if cfg.Enabled {
		t.Fatal("expected Enabled=false by default")
	}
	if cfg.Addr != "127.0.0.1:6060" {
		t.Fatalf("expected default addr '127.0.0.1:6060', got %q", cfg.Addr)
	}
}

// ---------------------------------------------------------------------------
// Alias tests for acceptance-criteria naming convention.
// These wrap the existing tests under the exact names from the test matrix,
// ensuring grep-based traceability from requirements to tests.
// ---------------------------------------------------------------------------

// TestStartPprof_ListensOnPort verifies that when pprof is enabled,
// GET /debug/pprof/ returns 200.
//
// Acceptance criteria:
//   - Start pprof server
//   - GET /debug/pprof/ returns 200
func TestStartPprof_ListensOnPort(t *testing.T) {
	port := freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	cfg := PprofConfig{Enabled: true, Addr: addr}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = StartPprof(ctx, cfg) }()

	baseURL := fmt.Sprintf("http://%s", addr)
	if err := waitForServer(baseURL+"/debug/pprof/", 3*time.Second); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(baseURL + "/debug/pprof/")
	if err != nil {
		t.Fatalf("GET /debug/pprof/ failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// TestStartPprof_BindsToLocalhost verifies that the default pprof address
// binds to 127.0.0.1, NOT 0.0.0.0.
//
// Acceptance criteria:
//   - Listen address is 127.0.0.1, not 0.0.0.0
func TestStartPprof_BindsToLocalhost(t *testing.T) {
	cfg := DefaultPprofConfig()
	if !strings.HasPrefix(cfg.Addr, "127.0.0.1:") {
		t.Fatalf("expected addr starting with '127.0.0.1:', got %q", cfg.Addr)
	}
	if strings.HasPrefix(cfg.Addr, "0.0.0.0:") {
		t.Fatalf("addr must not bind to 0.0.0.0 (security risk): got %q", cfg.Addr)
	}
}
