// Package profiling provides performance profiling capabilities for Xyncra Server.
//
// It integrates two profiling systems:
//   - pprof: Go's standard profiling toolkit, exposed via HTTP on a dedicated port
//   - Pyroscope: Continuous profiling agent for long-term performance monitoring
//
// # Security Model
//
// Following D-003 (internal deployment model), pprof is bound to localhost only
// (127.0.0.1:6060 by default) and never exposed to 0.0.0.0. This prevents
// accidental exposure of sensitive profiling data (goroutine stacks, heap dumps)
// that may contain secrets or internal state.
//
// # Fail-Open Strategy
//
// Following D-072 (fail-open strategy), both profiling systems are optional and
// fail-silent. When Pyroscope is unavailable or pprof is disabled, the application
// continues to function normally without profiling data. This ensures profiling
// infrastructure failures do not impact application availability.
//
// # Configuration
//
// Profiling is controlled via environment variables:
//
//	XYNCRA_PPROF_ENABLED=true     # Enable pprof HTTP server
//	XYNCRA_PPROF_ADDR=127.0.0.1:6060  # pprof listen address
//	XYNCRA_PROFILING_ENABLED=true      # Enable Pyroscope agent
//	XYNCRA_PROFILING_SERVER=http://pyroscope:4040  # Pyroscope server URL
//	XYNCRA_PROFILING_APP_NAME=xyncra-server  # Pyroscope application name
//
// # Usage
//
//	import "github.com/PineappleBond/xyncra-server/internal/profiling"
//
//	// Start pprof (blocking, run in goroutine)
//	pprofCfg := profiling.DefaultPprofConfig()
//	if pprofCfg.Enabled {
//	    go profiling.StartPprof(ctx, pprofCfg)
//	}
//
//	// Start Pyroscope (non-blocking, returns cleanup function)
//	pyroscopeCfg := profiling.DefaultPyroscopeConfig()
//	if pyroscopeCfg.Enabled {
//	    cleanup, err := profiling.StartPyroscope(pyroscopeCfg)
//	    if err != nil {
//	        slog.Warn("pyroscope init failed", "error", err)
//	    } else if cleanup != nil {
//	        defer cleanup()
//	    }
//	}
package profiling
