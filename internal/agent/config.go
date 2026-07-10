package agent

// AgentConfig represents the configuration for an AI agent.
// Parsed from YAML front matter in agent definition files.
type AgentConfig struct {
	ID           string          `yaml:"id" json:"id"`
	Name         string          `yaml:"name" json:"name"`
	Description  string          `yaml:"description" json:"description"`
	Model        string          `yaml:"model" json:"model"`
	APIKeyEnv    string          `yaml:"api_key_env" json:"api_key_env"`
	BaseURL      string          `yaml:"base_url" json:"base_url"`
	Parameters   AgentParameters `yaml:"parameters" json:"parameters"`
	Context      AgentContext    `yaml:"context" json:"context"`
	Tools        []string        `yaml:"tools" json:"tools"`
	SystemPrompt string          `yaml:"-" json:"system_prompt"` // Markdown body
}

// AgentParameters holds model generation parameters.
type AgentParameters struct {
	Temperature float64 `yaml:"temperature,omitempty" json:"temperature,omitempty"`
	MaxTokens   int     `yaml:"max_tokens,omitempty" json:"max_tokens,omitempty"`
	TopP        float64 `yaml:"top_p,omitempty" json:"top_p,omitempty"`
}

// AgentContext holds context window configuration.
type AgentContext struct {
	MaxTokens   int `yaml:"max_tokens" json:"max_tokens"`
	MaxMessages int `yaml:"max_messages" json:"max_messages"`
}

// Validate checks that the AgentConfig has all required fields.
func (c *AgentConfig) Validate() error {
	if c.ID == "" {
		return ErrMissingID
	}
	if c.Name == "" {
		return ErrMissingName
	}
	if c.Model == "" {
		return ErrMissingModel
	}
	if c.APIKeyEnv == "" {
		return ErrMissingAPIKeyEnv
	}
	return nil
}
