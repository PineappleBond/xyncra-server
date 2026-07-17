package metrics

import "os"

// Config holds configuration for the metrics subsystem.
type Config struct {
	// Enabled controls whether the /metrics endpoint and runtime collector
	// are active. When false, no metrics goroutines are started (D-063).
	Enabled bool
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Enabled: os.Getenv("XYNCRA_METRICS_ENABLED") == "true",
	}
}
