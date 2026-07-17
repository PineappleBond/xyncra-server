package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// freeTCPPort asks the OS for a free TCP port on 127.0.0.1 and returns it.
func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

// waitHTTP polls the URL until it responds or timeout expires.
func waitHTTP(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("server at %s not ready within %v", url, timeout)
}

// TestWSWithExtraRoutes_RegistersHandler verifies that routes registered via
// WSWithExtraRoutes are reachable on the running server's HTTP mux. This
// covers the /metrics-style route registration pattern used in production.
//
// Acceptance criteria:
//   - Server starts without error
//   - GET request to the registered route returns the expected response
//   - The route handler is invoked correctly
func TestWSWithExtraRoutes_RegistersHandler(t *testing.T) {
	port := freeTCPPort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	// Create a simple handler that returns a known response.
	mux := http.NewServeMux()
	mux.HandleFunc("/custom-endpoint", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test", "extra-routes")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("custom-response"))
	})

	srv, err := NewWebSocketServer(
		WSWithAddr(addr),
		WSWithConnectionStore(NewMemoryConnectionStore(0)),
		WSWithStore(&mockStore{}),
		WSWithBroker(&mockBroker{}),
		WSWithExtraRoutes(Route{
			Pattern: "/custom-endpoint",
			Handler: mux,
		}),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	baseURL := fmt.Sprintf("http://%s", addr)
	require.NoError(t, waitHTTP(baseURL+"/health", 3*time.Second))

	// GET the custom endpoint.
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(baseURL + "/custom-endpoint")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode, "expected 200 from custom route")
	require.Equal(t, "extra-routes", resp.Header.Get("X-Test"))

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "custom-response", string(body))

	// Also verify the default /health endpoint still works.
	resp2, err := client.Get(baseURL + "/health")
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode, "health endpoint should still work")
}

// TestWSWithExtraRoutes_NilRoutes verifies that passing nil or no extra
// routes does not cause panics and the server still starts and serves
// default endpoints.
//
// Acceptance criteria:
//   - Server starts without panic when extra routes are empty
//   - Health endpoint remains functional
//   - No extra routes are registered (default handlers only)
func TestWSWithExtraRoutes_NilRoutes(t *testing.T) {
	port := freeTCPPort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	// Build options with no extra routes.
	srv, err := NewWebSocketServer(
		WSWithAddr(addr),
		WSWithConnectionStore(NewMemoryConnectionStore(0)),
		WSWithStore(&mockStore{}),
		WSWithBroker(&mockBroker{}),
		WSWithExtraRoutes(), // zero routes
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	baseURL := fmt.Sprintf("http://%s", addr)
	require.NoError(t, waitHTTP(baseURL+"/health", 3*time.Second))

	// /health should still work.
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(baseURL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// An arbitrary path should return 404 (not registered).
	resp2, err := client.Get(baseURL + "/nonexistent")
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusNotFound, resp2.StatusCode)
}
