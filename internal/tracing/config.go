package tracing

import (
	"os"
	"strconv"
	"strings"
)

// TracingConfig holds configuration for the OpenTelemetry tracing subsystem.
type TracingConfig struct {
	// Enabled controls whether tracing is active. When false, a no-op provider is installed.
	Enabled bool
	// ServiceName is the service.name resource attribute reported to the collector.
	ServiceName string
	// Endpoint is the OTLP/gRPC collector address (host:port).
	Endpoint string
	// Insecure disables TLS for the OTLP exporter.
	Insecure bool
	// SampleRate is the trace sampling ratio (0.0 to 1.0).
	SampleRate float64
	// DebugUsers is a list of user IDs for which debug-level sampling is forced.
	DebugUsers []string
	// DebugDevices is a list of device IDs for which debug-level sampling is forced.
	DebugDevices []string
}

// TracingOption is a functional option for overriding TracingConfig fields.
type TracingOption func(*TracingConfig)

// DefaultTracingConfig returns a TracingConfig populated from XYNCRA_TRACING_* environment variables.
func DefaultTracingConfig() TracingConfig {
	cfg := TracingConfig{
		Enabled:      envBool("XYNCRA_TRACING_ENABLED", false),
		ServiceName:  envString("XYNCRA_TRACING_SERVICE_NAME", "xyncra-server"),
		Endpoint:     envString("XYNCRA_TRACING_OTLP_ENDPOINT", "localhost:4317"),
		Insecure:     envBool("XYNCRA_TRACING_OTLP_INSECURE", true),
		SampleRate:   envFloat("XYNCRA_TRACING_SAMPLING_RATE", 1.0),
		DebugUsers:   envCSV("XYNCRA_TRACING_DEBUG_USERS"),
		DebugDevices: envCSV("XYNCRA_TRACING_DEBUG_DEVICES"),
	}
	return cfg
}

// Apply applies functional options to the config, allowing callers to override defaults.
func (c *TracingConfig) Apply(opts ...TracingOption) {
	for _, opt := range opts {
		opt(c)
	}
}

// WithEnabled returns a TracingOption that sets the Enabled field.
func WithEnabled(enabled bool) TracingOption {
	return func(c *TracingConfig) { c.Enabled = enabled }
}

// WithServiceName returns a TracingOption that sets the ServiceName field.
func WithServiceName(name string) TracingOption {
	return func(c *TracingConfig) { c.ServiceName = name }
}

// WithEndpoint returns a TracingOption that sets the Endpoint field.
func WithEndpoint(endpoint string) TracingOption {
	return func(c *TracingConfig) { c.Endpoint = endpoint }
}

// WithInsecure returns a TracingOption that sets the Insecure field.
func WithInsecure(insecure bool) TracingOption {
	return func(c *TracingConfig) { c.Insecure = insecure }
}

// WithSampleRate returns a TracingOption that sets the SampleRate field.
func WithSampleRate(rate float64) TracingOption {
	return func(c *TracingConfig) { c.SampleRate = rate }
}

// WithDebugUsers returns a TracingOption that sets the DebugUsers field.
func WithDebugUsers(users []string) TracingOption {
	return func(c *TracingConfig) { c.DebugUsers = users }
}

// WithDebugDevices returns a TracingOption that sets the DebugDevices field.
func WithDebugDevices(devices []string) TracingOption {
	return func(c *TracingConfig) { c.DebugDevices = devices }
}

// envString reads an environment variable or returns the default value.
func envString(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// envBool reads an environment variable as a boolean or returns the default value.
func envBool(key string, defaultVal bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return defaultVal
	}
	return b
}

// envFloat reads an environment variable as a float64 or returns the default value.
func envFloat(key string, defaultVal float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return defaultVal
	}
	return f
}

// envCSV reads an environment variable as a comma-separated list.
// Returns nil if the variable is empty or unset.
func envCSV(key string) []string {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
