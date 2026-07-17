package agent

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/claude"
	"github.com/cloudwego/eino-ext/components/model/ollama"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino-ext/components/model/qwen"
	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	agenttools "github.com/PineappleBond/xyncra-server/internal/agent/tools"
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
	llmFactory             *LLMClientFactory
	toolRegistry           *agenttools.Registry
	registry               *AgentRegistry          // for sub-agent resolution (D-081)
	checkpointStore        compose.CheckPointStore // for HITL checkpoint persistence (D-083)
	mcpBridge              *agenttools.MCPBridge   // for MCP server connections (D-086)
	clientFunctionProvider ClientFunctionProvider  // Phase 6 (D-101)
	clientCaller           ClientCaller            // Phase 6 (D-101)
	llmLogger              *LLMLogger              // optional: dedicated LLM call logger
	tracingEnabled         bool                    // when true, TracingMiddleware is appended to the middleware chain
	tracingDebugUsers      []string                // debug user IDs for LLM content capture
	tracingDebugDevices    []string                // debug device IDs for LLM content capture
}

// NewAgentBuilder creates an AgentBuilder backed by the given LLM factory.
func NewAgentBuilder(factory *LLMClientFactory) *AgentBuilder {
	return &AgentBuilder{llmFactory: factory}
}

// SetToolRegistry sets the tool registry used to create tools during Build.
// If not set, no tools are created (backward compatible).
func (b *AgentBuilder) SetToolRegistry(registry *agenttools.Registry) {
	b.toolRegistry = registry
}

// SetRegistry sets the agent registry used for sub-agent resolution (D-081).
// If not set, sub-agents are not resolved.
func (b *AgentBuilder) SetRegistry(registry *AgentRegistry) {
	b.registry = registry
}

// SetCheckPointStore sets the checkpoint store for HITL support (D-083).
// If not set, checkpoint persistence is disabled and HITL is not available.
func (b *AgentBuilder) SetCheckPointStore(store compose.CheckPointStore) {
	b.checkpointStore = store
}

// CheckPointStore returns the checkpoint store, or nil if not set.
func (b *AgentBuilder) CheckPointStore() compose.CheckPointStore {
	return b.checkpointStore
}

// SetMCPBridge sets the MCP bridge used to connect to MCP servers during Build (D-086).
// If not set, MCP servers configured in AgentConfig are ignored.
func (b *AgentBuilder) SetMCPBridge(bridge *agenttools.MCPBridge) {
	b.mcpBridge = bridge
}

// SetClientFunctionProvider sets the function registry used to retrieve
// client device functions for dynamic tool injection (D-101).
// If not set, client tools are not available.
func (b *AgentBuilder) SetClientFunctionProvider(provider ClientFunctionProvider) {
	b.clientFunctionProvider = provider
}

// SetClientCaller sets the caller used to invoke remote client functions
// via ReverseRPC (D-101).
// If not set, client tools are not available.
func (b *AgentBuilder) SetClientCaller(caller ClientCaller) {
	b.clientCaller = caller
}

// SetLLMLogger sets the dedicated LLM call logger. When set, a LoggingMiddleware
// is appended to the middleware chain in Build(), recording all LLM requests,
// responses, tool calls, and agent events to the logger's output.
// When nil, no LLM logging middleware is added (default).
func (b *AgentBuilder) SetLLMLogger(logger *LLMLogger) {
	b.llmLogger = logger
}

// SetTracingEnabled controls whether a TracingMiddleware is appended to the
// middleware chain during Build(). When enabled, each LLM call and tool call
// produces an OpenTelemetry span. When disabled (the default), no tracing
// middleware is added and there is zero overhead.
func (b *AgentBuilder) SetTracingEnabled(enabled bool) {
	b.tracingEnabled = enabled
}

// SetTracingDebugFilter configures which users/devices get full LLM content
// recorded in tracing spans. When non-empty, matching callers (OR logic)
// have their request/response content added as span events on agent.llm.call.
func (b *AgentBuilder) SetTracingDebugFilter(users, devices []string) {
	b.tracingDebugUsers = users
	b.tracingDebugDevices = devices
}

// BuiltAgent wraps an Eino Runner together with the config it was built from.
// The Agent field holds the underlying agent for sub-agent wrapping (D-081).
type BuiltAgent struct {
	Runner *adk.Runner
	Config *AgentConfig
	Agent  adk.Agent // underlying agent, used by NewAgentTool for sub-agents
}

// Build creates a fully configured agent ready for execution.
//
// The method performs these steps:
//  1. Creates a ChatModel via the LLMClientFactory.
//  2. Creates tools from the tool registry if configured (D-078).
//  3. Resolves sub-agents and appends them as tools (D-081).
//  4. Builds the middleware chain (D-079).
//  5. Wraps everything in a ChatModelAgent with the agent's system prompt as instruction.
//  6. Creates a Runner with streaming enabled and optional CheckPointStore (D-083).
func (b *AgentBuilder) Build(ctx context.Context, config *AgentConfig) (built *BuiltAgent, err error) {
	// Create agent.build span for distributed tracing.
	ctx, buildFinish := startAgentBuildSpan(ctx, config.ID)
	defer func() { buildFinish(err) }()

	chatModel, err := b.llmFactory.Create(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAgentBuild, err)
	}

	// Create tools from registry (D-078).
	var einoTools []tool.BaseTool
	if b.toolRegistry != nil && len(config.Tools) > 0 {
		created, err := b.toolRegistry.Create(ctx, config.Tools, config.ToolConfig)
		if err != nil {
			log.Default().Printf("agent %s: some tools failed to create: %v", config.ID, err)
		}
		einoTools = created
	}

	// Resolve sub-agents and wrap them as tools (D-081).
	if b.registry != nil && len(config.SubAgents) > 0 {
		subTools, err := b.resolveSubAgents(ctx, config)
		if err != nil {
			log.Default().Printf("agent %s: sub-agent resolution had errors: %v", config.ID, err)
		}
		einoTools = append(einoTools, subTools...)
	}

	// Connect MCP servers and add their tools (D-086).
	if b.mcpBridge != nil && len(config.MCPServers) > 0 {
		for _, mcpCfg := range config.MCPServers {
			var mcpTools []tool.BaseTool
			var err error
			switch mcpCfg.Transport {
			case "sse":
				mcpTools, err = b.mcpBridge.ConnectSSE(ctx, mcpCfg.Name, mcpCfg.URL, mcpCfg.Tools)
			case "stdio":
				mcpTools, err = b.mcpBridge.ConnectStdio(ctx, mcpCfg.Name, mcpCfg.Command, mcpCfg.Args, mcpCfg.Env, mcpCfg.Tools)
			default:
				err = fmt.Errorf("mcp: unsupported transport %q for server %q", mcpCfg.Transport, mcpCfg.Name)
			}
			if err != nil {
				log.Default().Printf("[WARN] agent %s: MCP connect failed for %q, skipping: %v", config.ID, mcpCfg.Name, err)
				continue // fail-open (D-086)
			}
			einoTools = append(einoTools, mcpTools...)
		}
	}

	// Build middleware chain (D-079).
	handlers := b.buildMiddleware(ctx, config, chatModel)

	agentCfg := &adk.ChatModelAgentConfig{
		Name:        config.ID,
		Description: config.Description,
		Instruction: config.SystemPrompt,
		Model:       chatModel,
	}

	if len(einoTools) > 0 {
		agentCfg.ToolsConfig = adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: einoTools,
			},
		}
	}

	if len(handlers) > 0 {
		agentCfg.Handlers = handlers
	}

	agent, err := adk.NewChatModelAgent(ctx, agentCfg)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAgentBuild, err)
	}

	runnerCfg := adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming: true,
	}
	// Wire CheckPointStore for HITL support (D-083).
	if b.checkpointStore != nil {
		runnerCfg.CheckPointStore = b.checkpointStore
	}

	runner := adk.NewRunner(ctx, runnerCfg)

	return &BuiltAgent{
		Runner: runner,
		Config: config,
		Agent:  agent,
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
