package tracing

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// cleanEnv unsets all XYNCRA_TRACING_* environment variables to ensure
// test isolation. Call it at the start of every test that reads env vars.
func cleanEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"XYNCRA_TRACING_ENABLED",
		"XYNCRA_TRACING_SERVICE_NAME",
		"XYNCRA_TRACING_OTLP_ENDPOINT",
		"XYNCRA_TRACING_OTLP_INSECURE",
		"XYNCRA_TRACING_SAMPLING_RATE",
		"XYNCRA_TRACING_DEBUG_USERS",
		"XYNCRA_TRACING_DEBUG_DEVICES",
	} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
}

func TestDefaultTracingConfig_Defaults(t *testing.T) {
	cleanEnv(t)

	cfg := DefaultTracingConfig()
	assert.False(t, cfg.Enabled)
	assert.Equal(t, "localhost:4317", cfg.Endpoint)
	assert.Equal(t, "xyncra-server", cfg.ServiceName)
	assert.Equal(t, 1.0, cfg.SampleRate)
	assert.True(t, cfg.Insecure)
	assert.Empty(t, cfg.DebugUsers)
	assert.Empty(t, cfg.DebugDevices)
}

func TestDefaultTracingConfig_EnvVars(t *testing.T) {
	cleanEnv(t)
	t.Setenv("XYNCRA_TRACING_ENABLED", "true")
	t.Setenv("XYNCRA_TRACING_OTLP_ENDPOINT", "collector:4317")
	t.Setenv("XYNCRA_TRACING_SERVICE_NAME", "my-service")
	t.Setenv("XYNCRA_TRACING_OTLP_INSECURE", "false")
	t.Setenv("XYNCRA_TRACING_SAMPLING_RATE", "0.5")
	t.Setenv("XYNCRA_TRACING_DEBUG_USERS", "alice,bob")
	t.Setenv("XYNCRA_TRACING_DEBUG_DEVICES", "dev1,dev2")

	cfg := DefaultTracingConfig()
	assert.True(t, cfg.Enabled)
	assert.Equal(t, "collector:4317", cfg.Endpoint)
	assert.Equal(t, "my-service", cfg.ServiceName)
	assert.False(t, cfg.Insecure)
	assert.Equal(t, 0.5, cfg.SampleRate)
	assert.Equal(t, []string{"alice", "bob"}, cfg.DebugUsers)
	assert.Equal(t, []string{"dev1", "dev2"}, cfg.DebugDevices)
}

func TestDefaultTracingConfig_DebugUsersParsing(t *testing.T) {
	cleanEnv(t)
	t.Setenv("XYNCRA_TRACING_DEBUG_USERS", " u1 , u2 , u3 ")

	cfg := DefaultTracingConfig()
	assert.Equal(t, []string{"u1", "u2", "u3"}, cfg.DebugUsers)
}

func TestDefaultTracingConfig_DebugDevicesParsing(t *testing.T) {
	cleanEnv(t)
	t.Setenv("XYNCRA_TRACING_DEBUG_DEVICES", "dev-a,dev-b")

	cfg := DefaultTracingConfig()
	assert.Equal(t, []string{"dev-a", "dev-b"}, cfg.DebugDevices)
}

func TestDefaultTracingConfig_InvalidSampleRate(t *testing.T) {
	cleanEnv(t)
	t.Setenv("XYNCRA_TRACING_SAMPLING_RATE", "not-a-number")

	cfg := DefaultTracingConfig()
	assert.Equal(t, 1.0, cfg.SampleRate, "invalid float should fall back to default")
}

func TestDefaultTracingConfig_EmptyDebugUsers(t *testing.T) {
	cleanEnv(t)
	t.Setenv("XYNCRA_TRACING_DEBUG_USERS", "")

	cfg := DefaultTracingConfig()
	assert.Nil(t, cfg.DebugUsers)
}

func TestDefaultTracingConfig_EmptyDebugUsers_CommasOnly(t *testing.T) {
	cleanEnv(t)
	t.Setenv("XYNCRA_TRACING_DEBUG_USERS", ",,,")

	cfg := DefaultTracingConfig()
	assert.Nil(t, cfg.DebugUsers, "commas-only should produce nil (empty tokens are filtered)")
}

func TestDefaultTracingConfig_SingleDebugUser(t *testing.T) {
	cleanEnv(t)
	t.Setenv("XYNCRA_TRACING_DEBUG_USERS", "alice")

	cfg := DefaultTracingConfig()
	assert.Equal(t, []string{"alice"}, cfg.DebugUsers)
}

func TestTracingConfig_Apply(t *testing.T) {
	cfg := TracingConfig{}
	cfg.Apply(WithEnabled(true))
	assert.True(t, cfg.Enabled)
}

func TestTracingConfig_ApplyMultiple(t *testing.T) {
	cfg := TracingConfig{}
	cfg.Apply(
		WithEnabled(true),
		WithServiceName("svc"),
		WithEndpoint("ep:4317"),
		WithInsecure(false),
		WithSampleRate(0.25),
		WithDebugUsers([]string{"a"}),
		WithDebugDevices([]string{"d"}),
	)
	assert.True(t, cfg.Enabled)
	assert.Equal(t, "svc", cfg.ServiceName)
	assert.Equal(t, "ep:4317", cfg.Endpoint)
	assert.False(t, cfg.Insecure)
	assert.Equal(t, 0.25, cfg.SampleRate)
	assert.Equal(t, []string{"a"}, cfg.DebugUsers)
	assert.Equal(t, []string{"d"}, cfg.DebugDevices)
}

func TestDefaultTracingConfig_BoolParsing(t *testing.T) {
	cases := map[string]bool{
		"true":  true,
		"True":  true,
		"TRUE":  true,
		"1":     true,
		"false": false,
		"False": false,
		"0":     false,
	}
	for input, want := range cases {
		t.Run(input, func(t *testing.T) {
			cleanEnv(t)
			t.Setenv("XYNCRA_TRACING_ENABLED", input)
			cfg := DefaultTracingConfig()
			assert.Equal(t, want, cfg.Enabled)
		})
	}
}

func TestDefaultTracingConfig_BoolParsing_Invalid(t *testing.T) {
	cleanEnv(t)
	t.Setenv("XYNCRA_TRACING_ENABLED", "invalid")

	cfg := DefaultTracingConfig()
	assert.False(t, cfg.Enabled, "invalid bool should fall back to default (false)")
}

func TestDefaultTracingConfig_FloatParsing(t *testing.T) {
	cleanEnv(t)
	t.Setenv("XYNCRA_TRACING_SAMPLING_RATE", "0.75")

	cfg := DefaultTracingConfig()
	assert.Equal(t, 0.75, cfg.SampleRate)
}

func TestDefaultTracingConfig_FloatParsing_Empty(t *testing.T) {
	cleanEnv(t)
	t.Setenv("XYNCRA_TRACING_SAMPLING_RATE", "")

	cfg := DefaultTracingConfig()
	assert.Equal(t, 1.0, cfg.SampleRate, "empty string should return default")
}
