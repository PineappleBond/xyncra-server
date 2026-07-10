package agent

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
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
