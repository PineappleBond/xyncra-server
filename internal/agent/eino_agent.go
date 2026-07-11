package agent

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/claude"
	"github.com/cloudwego/eino-ext/components/model/ollama"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino-ext/components/model/qwen"
	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	xyncramodel "github.com/PineappleBond/xyncra-server/internal/store/model"
)

// LLMProvider abstracts LLM backend creation (D-066).
// Each provider knows how to construct a ChatModel for its specific LLM service
// and supplies a default BaseURL so agents work with zero configuration (D-064).
type LLMProvider interface {
	// CreateChatModel builds a ChatModel from the agent configuration.
	// The apiKey is provided separately so that providers can ignore it when
	// not needed (e.g. Ollama for local models).
	CreateChatModel(ctx context.Context, config *AgentConfig, apiKey string) (einomodel.BaseChatModel, error)

	// DefaultBaseURL returns the provider's default API endpoint (D-064).
	DefaultBaseURL() string
}

// ---------------------------------------------------------------------------
// OpenAI Provider
// ---------------------------------------------------------------------------

// OpenAIProvider creates ChatModel instances for OpenAI-compatible APIs.
type OpenAIProvider struct{}

// CreateChatModel builds an OpenAI ChatModel.
func (p *OpenAIProvider) CreateChatModel(ctx context.Context, config *AgentConfig, apiKey string) (einomodel.BaseChatModel, error) {
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = p.DefaultBaseURL()
	}

	cfg := &openai.ChatModelConfig{
		APIKey:  apiKey,
		Model:   config.Model,
		BaseURL: baseURL,
	}

	if config.Parameters.Temperature > 0 {
		t := float32(config.Parameters.Temperature)
		cfg.Temperature = &t
	}
	if config.Parameters.MaxTokens > 0 {
		cfg.MaxCompletionTokens = &config.Parameters.MaxTokens
	}

	cm, err := openai.NewChatModel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("openai: create chat model: %w", err)
	}
	return cm, nil
}

// DefaultBaseURL returns the OpenAI API endpoint (D-064).
func (p *OpenAIProvider) DefaultBaseURL() string {
	return "https://api.openai.com/v1"
}

// ---------------------------------------------------------------------------
// Claude Provider
// ---------------------------------------------------------------------------

// ClaudeProvider creates ChatModel instances for Anthropic Claude.
type ClaudeProvider struct{}

// CreateChatModel builds a Claude ChatModel.
func (p *ClaudeProvider) CreateChatModel(ctx context.Context, config *AgentConfig, apiKey string) (einomodel.BaseChatModel, error) {
	cfg := &claude.Config{
		APIKey:    apiKey,
		Model:     config.Model,
		MaxTokens: config.Parameters.MaxTokens,
	}

	if config.BaseURL != "" {
		cfg.BaseURL = &config.BaseURL
	}

	if config.Parameters.Temperature > 0 {
		t := float32(config.Parameters.Temperature)
		cfg.Temperature = &t
	}

	cm, err := claude.NewChatModel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("claude: create chat model: %w", err)
	}
	return cm, nil
}

// DefaultBaseURL returns the Anthropic API endpoint (D-064).
func (p *ClaudeProvider) DefaultBaseURL() string {
	return "https://api.anthropic.com"
}

// ---------------------------------------------------------------------------
// Ollama Provider
// ---------------------------------------------------------------------------

// OllamaProvider creates ChatModel instances for local Ollama servers.
type OllamaProvider struct{}

// CreateChatModel builds an Ollama ChatModel.
// The apiKey parameter is ignored because Ollama runs locally without authentication.
func (p *OllamaProvider) CreateChatModel(ctx context.Context, config *AgentConfig, _ string) (einomodel.BaseChatModel, error) {
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = p.DefaultBaseURL()
	}

	cfg := &ollama.ChatModelConfig{
		BaseURL: baseURL,
		Model:   config.Model,
	}

	if config.Parameters.Temperature > 0 || config.Parameters.TopP > 0 {
		opts := &ollama.Options{}
		if config.Parameters.Temperature > 0 {
			opts.Temperature = float32(config.Parameters.Temperature)
		}
		if config.Parameters.TopP > 0 {
			opts.TopP = float32(config.Parameters.TopP)
		}
		cfg.Options = opts
	}

	cm, err := ollama.NewChatModel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("ollama: create chat model: %w", err)
	}
	return cm, nil
}

// DefaultBaseURL returns the default Ollama endpoint (D-064).
func (p *OllamaProvider) DefaultBaseURL() string {
	return "http://localhost:11434"
}

// ---------------------------------------------------------------------------
// Qwen Provider
// ---------------------------------------------------------------------------

// QwenProvider creates ChatModel instances for Alibaba Qwen (DashScope).
type QwenProvider struct{}

// CreateChatModel builds a Qwen ChatModel.
func (p *QwenProvider) CreateChatModel(ctx context.Context, config *AgentConfig, apiKey string) (einomodel.BaseChatModel, error) {
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = p.DefaultBaseURL()
	}

	cfg := &qwen.ChatModelConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   config.Model,
	}

	if config.Parameters.Temperature > 0 {
		t := float32(config.Parameters.Temperature)
		cfg.Temperature = &t
	}
	if config.Parameters.MaxTokens > 0 {
		cfg.MaxTokens = &config.Parameters.MaxTokens
	}

	cm, err := qwen.NewChatModel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("qwen: create chat model: %w", err)
	}
	return cm, nil
}

// DefaultBaseURL returns the DashScope compatible-mode endpoint (D-064).
func (p *QwenProvider) DefaultBaseURL() string {
	return "https://dashscope.aliyuncs.com/compatible-mode/v1"
}

// ---------------------------------------------------------------------------
// LLMClientFactory
// ---------------------------------------------------------------------------

// LLMClientFactory manages LLMProvider registrations and creates ChatModel
// instances based on AgentConfig. Provider selection uses model name and
// BaseURL heuristics (see detectProvider).
type LLMClientFactory struct {
	providers map[string]LLMProvider
}

// NewLLMClientFactory creates a factory with all built-in providers registered.
func NewLLMClientFactory() *LLMClientFactory {
	f := &LLMClientFactory{
		providers: make(map[string]LLMProvider),
	}
	f.RegisterProvider("openai", &OpenAIProvider{})
	f.RegisterProvider("claude", &ClaudeProvider{})
	f.RegisterProvider("ollama", &OllamaProvider{})
	f.RegisterProvider("qwen", &QwenProvider{})
	return f
}

// RegisterProvider adds or replaces a provider under the given name.
func (f *LLMClientFactory) RegisterProvider(name string, provider LLMProvider) {
	f.providers[name] = provider
}

// Create resolves the appropriate provider for the config and builds a ChatModel.
// API keys are read from the environment variable named by config.APIKeyEnv.
// The API key value is never included in error messages or logs (security).
func (f *LLMClientFactory) Create(ctx context.Context, config *AgentConfig) (einomodel.BaseChatModel, error) {
	providerName := detectProvider(config.Model, config.BaseURL)

	provider, ok := f.providers[providerName]
	if !ok {
		return nil, fmt.Errorf("%w: provider %q not registered", ErrUnsupportedModel, providerName)
	}

	var apiKey string
	if config.APIKeyEnv != "" {
		apiKey = os.Getenv(config.APIKeyEnv)
		if apiKey == "" {
			return nil, ErrAPIKeyMissing
		}
	}

	cm, err := provider.CreateChatModel(ctx, config, apiKey)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAgentBuild, err)
	}
	return cm, nil
}

// detectProvider determines which LLM provider to use based on the model name
// and optional base URL. Priority: baseURL keywords > model name heuristics > "openai".
func detectProvider(modelName, baseURL string) string {
	lowerBase := strings.ToLower(baseURL)

	// BaseURL-based detection (highest priority).
	if strings.Contains(lowerBase, "anthropic") || strings.Contains(lowerBase, "claude") {
		return "claude"
	}
	if strings.Contains(lowerBase, "ollama") || strings.Contains(lowerBase, "11434") {
		return "ollama"
	}
	if strings.Contains(lowerBase, "dashscope") || strings.Contains(lowerBase, "qwen") {
		return "qwen"
	}

	// Model-name-based detection.
	lowerModel := strings.ToLower(modelName)
	switch {
	case strings.HasPrefix(lowerModel, "claude"):
		return "claude"
	case strings.HasPrefix(lowerModel, "qwen"):
		return "qwen"
	case strings.HasPrefix(lowerModel, "llama"),
		strings.HasPrefix(lowerModel, "mistral"),
		strings.HasPrefix(lowerModel, "gemma"):
		return "ollama"
	}

	return "openai"
}

// ---------------------------------------------------------------------------
// AgentBuilder / BuiltAgent
// ---------------------------------------------------------------------------

// AgentBuilder constructs runnable agents from AgentConfig using the Eino ADK.
type AgentBuilder struct {
	llmFactory *LLMClientFactory
}

// NewAgentBuilder creates an AgentBuilder backed by the given LLM factory.
func NewAgentBuilder(factory *LLMClientFactory) *AgentBuilder {
	return &AgentBuilder{llmFactory: factory}
}

// BuiltAgent wraps an Eino Runner together with the config it was built from.
type BuiltAgent struct {
	Runner *adk.Runner
	Config *AgentConfig
}

// Build creates a fully configured agent ready for execution.
//
// The method performs three steps:
//  1. Creates a ChatModel via the LLMClientFactory.
//  2. Wraps it in a ChatModelAgent with the agent's system prompt as instruction.
//  3. Creates a Runner with streaming enabled.
func (b *AgentBuilder) Build(ctx context.Context, config *AgentConfig) (*BuiltAgent, error) {
	chatModel, err := b.llmFactory.Create(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAgentBuild, err)
	}

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        config.ID,
		Description: config.Description,
		Instruction: config.SystemPrompt,
		Model:       chatModel,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAgentBuild, err)
	}

	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming: true,
	})

	return &BuiltAgent{
		Runner: runner,
		Config: config,
	}, nil
}

// ---------------------------------------------------------------------------
// Message conversion
// ---------------------------------------------------------------------------

// convertMessages maps Xyncra model.Message slices to Eino schema.Message
// slices suitable for passing to an LLM. The role mapping uses the SenderID
// convention from D-054: messages from "agent/*" senders become assistant
// messages; all others become user messages.
func convertMessages(messages []*xyncramodel.Message) []*schema.Message {
	result := make([]*schema.Message, 0, len(messages))
	for _, msg := range messages {
		if strings.HasPrefix(msg.SenderID, "agent/") {
			result = append(result, schema.AssistantMessage(msg.Content, nil))
		} else {
			result = append(result, schema.UserMessage(msg.Content))
		}
	}
	return result
}
