package agent

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestAgentConfig_Validate_Valid(t *testing.T) {
	cfg := &AgentConfig{
		ID:        "test-bot",
		Name:      "Test Bot",
		Model:     "gpt-4",
		APIKeyEnv: "TEST_KEY",
	}
	assert.NoError(t, cfg.Validate())
}

func TestAgentConfig_Validate_MissingID(t *testing.T) {
	cfg := &AgentConfig{
		Name:      "Test Bot",
		Model:     "gpt-4",
		APIKeyEnv: "TEST_KEY",
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrMissingID))
}

func TestAgentConfig_Validate_MissingName(t *testing.T) {
	cfg := &AgentConfig{
		ID:        "test-bot",
		Model:     "gpt-4",
		APIKeyEnv: "TEST_KEY",
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrMissingName))
}

func TestAgentConfig_Validate_MissingModel(t *testing.T) {
	cfg := &AgentConfig{
		ID:        "test-bot",
		Name:      "Test Bot",
		APIKeyEnv: "TEST_KEY",
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrMissingModel))
}

func TestAgentConfig_Validate_ZeroValue(t *testing.T) {
	config := &AgentConfig{}
	err := config.Validate()
	assert.ErrorIs(t, err, ErrMissingID)
}

func TestAgentConfig_Validate_MissingAPIKeyEnv(t *testing.T) {
	cfg := &AgentConfig{
		ID:    "test-bot",
		Name:  "Test Bot",
		Model: "gpt-4",
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrMissingAPIKeyEnv))
}

// ---------------------------------------------------------------------------
// MCPServerConfig validation (Phase 8C, D-086)
// ---------------------------------------------------------------------------

// validBaseConfig returns a minimal AgentConfig that passes non-MCP validation.
func validBaseConfig() *AgentConfig {
	return &AgentConfig{
		ID:        "test-bot",
		Name:      "Test Bot",
		Model:     "gpt-4",
		APIKeyEnv: "TEST_KEY",
	}
}

func TestAgentConfig_Validate_MCPConfigs(t *testing.T) {
	tests := []struct {
		name    string
		mcpSrv  MCPServerConfig
		wantErr error
	}{
		{
			name: "valid SSE config",
			mcpSrv: MCPServerConfig{
				Name:      "remote-tools",
				Transport: "sse",
				URL:       "http://example.com/sse",
			},
			wantErr: nil,
		},
		{
			name: "valid stdio config",
			mcpSrv: MCPServerConfig{
				Name:      "local-tools",
				Transport: "stdio",
				Command:   "npx",
				Args:      []string{"-y", "@mcp/server"},
			},
			wantErr: nil,
		},
		{
			name: "missing name",
			mcpSrv: MCPServerConfig{
				Transport: "sse",
				URL:       "http://example.com/sse",
			},
			wantErr: ErrMCPMissingName,
		},
		{
			name: "invalid transport",
			mcpSrv: MCPServerConfig{
				Name:      "bad-transport",
				Transport: "grpc",
			},
			wantErr: ErrInvalidMCPTransport,
		},
		{
			name: "empty transport",
			mcpSrv: MCPServerConfig{
				Name: "no-transport",
			},
			wantErr: ErrInvalidMCPTransport,
		},
		{
			name: "SSE missing URL",
			mcpSrv: MCPServerConfig{
				Name:      "sse-no-url",
				Transport: "sse",
			},
			wantErr: ErrMCPMissingURL,
		},
		{
			name: "stdio missing command",
			mcpSrv: MCPServerConfig{
				Name:      "stdio-no-cmd",
				Transport: "stdio",
			},
			wantErr: ErrMCPMissingCommand,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validBaseConfig()
			cfg.MCPServers = []MCPServerConfig{tc.mcpSrv}
			err := cfg.Validate()
			if tc.wantErr == nil {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, tc.wantErr))
			}
		})
	}
}

func TestAgentConfig_Validate_MultipleMCPServers(t *testing.T) {
	cfg := validBaseConfig()
	cfg.MCPServers = []MCPServerConfig{
		{Name: "first", Transport: "sse", URL: "http://a.com/sse"},
		{Name: "second", Transport: "stdio", Command: "npx"},
	}
	assert.NoError(t, cfg.Validate())
}

func TestAgentConfig_Validate_MultipleMCPServers_SecondInvalid(t *testing.T) {
	cfg := validBaseConfig()
	cfg.MCPServers = []MCPServerConfig{
		{Name: "good", Transport: "sse", URL: "http://a.com/sse"},
		{Name: "", Transport: "sse", URL: "http://b.com/sse"}, // missing name
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrMCPMissingName))
}

func TestAgentConfig_Validate_MCPDuplicateName(t *testing.T) {
	cfg := validBaseConfig()
	cfg.MCPServers = []MCPServerConfig{
		{Name: "tools", Transport: "sse", URL: "http://a.com/sse"},
		{Name: "tools", Transport: "stdio", Command: "npx"}, // duplicate
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrMCPDuplicateName))
}

// ---------------------------------------------------------------------------
// Phase 6: MiddlewareConfig client tools (CFG-01, CFG-02)
// ---------------------------------------------------------------------------

func TestMiddlewareConfig_ClientTools_YAML(t *testing.T) {
	yamlData := `
enable_client_tools: true
client_tools:
  function_tags:
    - filesystem
    - network
  excluded_functions:
    - dangerous_fn
  call_timeout: 10s
`
	var cfg MiddlewareConfig
	err := yaml.Unmarshal([]byte(yamlData), &cfg)
	require.NoError(t, err)
	assert.True(t, cfg.EnableClientTools)
	assert.Equal(t, []string{"filesystem", "network"}, cfg.ClientTools.FunctionTags)
	assert.Equal(t, []string{"dangerous_fn"}, cfg.ClientTools.ExcludedFunctions)
	assert.Equal(t, 10*time.Second, cfg.ClientTools.CallTimeout)
}

func TestClientToolsConfig_Defaults(t *testing.T) {
	var cfg ClientToolsConfig
	assert.Nil(t, cfg.FunctionTags)
	assert.Nil(t, cfg.ExcludedFunctions)
	assert.Equal(t, time.Duration(0), cfg.CallTimeout)
}
