package metrics

import (
	"os"
	"testing"
)

// TestDefaultConfig verifies that DefaultConfig returns the expected values
// based on environment variables.
func TestDefaultConfig(t *testing.T) {
	tests := []struct {
		name     string
		envVal   string
		expected bool
	}{
		{
			name:     "enabled when env is true",
			envVal:   "true",
			expected: true,
		},
		{
			name:     "disabled when env is empty",
			envVal:   "",
			expected: false,
		},
		{
			name:     "disabled when env is false",
			envVal:   "false",
			expected: false,
		},
		{
			name:     "disabled when env is random string",
			envVal:   "yes",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envVal == "" {
				os.Unsetenv("XYNCRA_METRICS_ENABLED")
			} else {
				os.Setenv("XYNCRA_METRICS_ENABLED", tt.envVal)
			}
			defer os.Unsetenv("XYNCRA_METRICS_ENABLED")

			cfg := DefaultConfig()
			if cfg.Enabled != tt.expected {
				t.Errorf("DefaultConfig().Enabled = %v, want %v (env=%q)", cfg.Enabled, tt.expected, tt.envVal)
			}
		})
	}
}
