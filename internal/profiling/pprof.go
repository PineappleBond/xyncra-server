package profiling

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"strings"
	"time"
)

// PprofConfig holds pprof server configuration.
type PprofConfig struct {
	Enabled bool
	Addr    string // listen address; defaults to "127.0.0.1:6060"
}

// DefaultPprofConfig loads pprof configuration from XYNCRA_PPROF_* environment
// variables. When XYNCRA_PPROF_ENABLED is not set, the server is disabled.
// When XYNCRA_PPROF_ADDR is not set, it defaults to "127.0.0.1:6060".
//
// The default address binds to localhost only (not 0.0.0.0) per D-003
// (internal deployment model) to prevent accidental exposure of sensitive
// profiling data over the network.
func DefaultPprofConfig() PprofConfig {
	cfg := PprofConfig{
		Enabled: envBool("XYNCRA_PPROF_ENABLED", false),
		Addr:    envOrDefault("XYNCRA_PPROF_ADDR", "127.0.0.1:6060"),
	}
	return cfg
}

// StartPprof starts the pprof HTTP server on a dedicated mux, separate from
// the application's default mux. It blocks until ctx is cancelled, then
// gracefully shuts down the HTTP server.
//
// IMPORTANT: Addr must default to "127.0.0.1:6060" (NOT ":6060") for security.
// pprof exposes goroutine stacks and heap dumps that may contain secrets.
//
// When cfg.Enabled is false, StartPprof returns nil immediately.
//
// This follows D-003 (internal deployment model) and D-063 (optional module
// pattern): the pprof server is only started when explicitly enabled.
func StartPprof(ctx context.Context, cfg PprofConfig) error {
	if !cfg.Enabled {
		return nil
	}

	// Build a dedicated mux to avoid polluting http.DefaultServeMux.
	// Importing net/http/pprof registers handlers on DefaultServeMux as a
	// side effect, but we use the exported handler functions directly.
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Channel to capture server errors from Serve.
	errCh := make(chan error, 1)
	go func() {
		slog.Info("pprof server starting", "addr", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("pprof server: %w", err)
		}
		close(errCh)
	}()

	// Wait for ctx cancellation or server error.
	select {
	case <-ctx.Done():
		slog.Info("pprof server shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("pprof server shutdown: %w", err)
		}
		return nil
	case err := <-errCh:
		return err
	}
}

// envBool returns the boolean value of the environment variable identified by
// key, or fallback if the variable is empty, unset, or cannot be parsed.
func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	default:
		return fallback
	}
}

// envOrDefault returns the value of the environment variable identified by
// key, or fallback if the variable is empty or unset.
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
