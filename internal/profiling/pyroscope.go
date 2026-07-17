package profiling

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/grafana/pyroscope-go"
)

// PyroscopeConfig holds Pyroscope agent configuration.
type PyroscopeConfig struct {
	Enabled bool
	Server  string // e.g. "http://pyroscope:4040"
	AppName string // application name for Pyroscope; defaults to "xyncra-server"
}

// DefaultPyroscopeConfig loads Pyroscope configuration from XYNCRA_PROFILING_*
// environment variables. When XYNCRA_PROFILING_ENABLED is not set, the agent
// is disabled. When XYNCRA_PROFILING_APP_NAME is not set, it defaults to
// "xyncra-server".
func DefaultPyroscopeConfig() PyroscopeConfig {
	return PyroscopeConfig{
		Enabled: envBool("XYNCRA_PROFILING_ENABLED", false),
		Server:  os.Getenv("XYNCRA_PROFILING_SERVER"),
		AppName: envOrDefault("XYNCRA_PROFILING_APP_NAME", "xyncra-server"),
	}
}

// StartPyroscope starts the Pyroscope agent for continuous profiling.
// It returns a cleanup function that should be called on application shutdown.
//
// When cfg.Enabled is false, StartPyroscope returns nil, nil immediately.
//
// This follows D-072 (fail-open strategy): when Pyroscope initialization fails
// (e.g. server unreachable, invalid configuration), the error is logged as a
// warning and the function returns nil, nil. The application continues to
// function normally without profiling data. This ensures profiling
// infrastructure failures do not impact application availability.
func StartPyroscope(cfg PyroscopeConfig) (cleanup func(), err error) {
	if !cfg.Enabled {
		return nil, nil
	}

	if cfg.Server == "" {
		slog.Warn("pyroscope enabled but XYNCRA_PROFILING_SERVER not set, skipping")
		return nil, nil
	}

	profiler, err := pyroscope.Start(pyroscope.Config{
		ApplicationName: cfg.AppName,
		ServerAddress:   cfg.Server,
	})
	if err != nil {
		// Fail-silent per D-072: log warning and continue without profiling.
		slog.Warn("pyroscope init failed (continuing without profiling)", "error", err)
		return nil, nil
	}

	return func() {
		if err := profiler.Stop(); err != nil {
			slog.Warn("pyroscope stop error", "error", err)
		}
	}, nil
}

// FormatPyroscopeStatus returns a human-readable status string for logging.
func FormatPyroscopeStatus(cfg PyroscopeConfig) string {
	if !cfg.Enabled {
		return "disabled"
	}
	if cfg.Server == "" {
		return "enabled but no server configured"
	}
	return fmt.Sprintf("enabled (server=%s, app=%s)", cfg.Server, cfg.AppName)
}
