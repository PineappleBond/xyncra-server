package agent

import (
	"context"
	"strings"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xyncramodel "github.com/PineappleBond/xyncra-server/internal/store/model"
)

// ---------------------------------------------------------------------------
// detectProvider
// ---------------------------------------------------------------------------

func TestDetectProvider(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		baseURL  string
		expected string
	}{
		// BaseURL-based detection (highest priority)
		{"baseURL with anthropic", "gpt-4", "https://api.anthropic.com/v1", "claude"},
		{"baseURL with claude keyword", "gpt-4", "https://claude.example.com", "claude"},
		{"baseURL with 11434", "gpt-4", "http://localhost:11434", "ollama"},
		{"baseURL with ollama keyword", "gpt-4", "http://ollama.local:11434", "ollama"},
		{"baseURL with port 11434 in proxy URL", "gpt-4", "http://my-proxy.com:11434/v1", "ollama"},
		{"baseURL with dashscope", "gpt-4", "https://dashscope.aliyuncs.com/compatible-mode/v1", "qwen"},
		{"baseURL with qwen keyword", "gpt-4", "https://qwen.example.com", "qwen"},

		// Model-name-based detection
		{"model claude-3", "claude-3-opus-20240229", "", "claude"},
		{"model claude-sonnet", "claude-sonnet-4-20250514", "", "claude"},
		{"model qwen-plus", "qwen-plus", "", "qwen"},
		{"model qwen-max", "qwen-max", "", "qwen"},
		{"model llama3", "llama3", "", "ollama"},
		{"model mistral", "mistral-7b", "", "ollama"},
		{"model gemma", "gemma-2b", "", "ollama"},

		// Default fallback
		{"model gpt-4 defaults to openai", "gpt-4", "", "openai"},
		{"model gpt-3.5 defaults to openai", "gpt-3.5-turbo", "", "openai"},
		{"empty model + empty baseURL defaults to openai", "", "", "openai"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := detectProvider(tc.model, tc.baseURL)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// ---------------------------------------------------------------------------
// convertMessages
// ---------------------------------------------------------------------------

func TestConvertMessages(t *testing.T) {
	t.Run("empty messages", func(t *testing.T) {
		result := convertMessages(nil)
		assert.Empty(t, result)
	})

	t.Run("human message becomes user role", func(t *testing.T) {
		msgs := []*xyncramodel.Message{
			{SenderID: "user/123", Content: "Hello"},
		}
		result := convertMessages(msgs)
		require.Len(t, result, 1)
		assert.Equal(t, schema.User, result[0].Role)
		assert.Equal(t, "Hello", result[0].Content)
	})

	t.Run("agent message becomes assistant role", func(t *testing.T) {
		msgs := []*xyncramodel.Message{
			{SenderID: "agent/weather-bot", Content: "It's sunny"},
		}
		result := convertMessages(msgs)
		require.Len(t, result, 1)
		assert.Equal(t, schema.Assistant, result[0].Role)
		assert.Equal(t, "It's sunny", result[0].Content)
	})

	t.Run("mixed messages maintain correct roles", func(t *testing.T) {
		msgs := []*xyncramodel.Message{
			{SenderID: "user/alice", Content: "What's the weather?"},
			{SenderID: "agent/weather-bot", Content: "It's sunny"},
			{SenderID: "user/alice", Content: "Thanks!"},
			{SenderID: "agent/other-agent", Content: "I can help too"},
		}
		result := convertMessages(msgs)
		require.Len(t, result, 4)

		assert.Equal(t, schema.User, result[0].Role)
		assert.Equal(t, "What's the weather?", result[0].Content)

		assert.Equal(t, schema.Assistant, result[1].Role)
		assert.Equal(t, "It's sunny", result[1].Content)

		assert.Equal(t, schema.User, result[2].Role)
		assert.Equal(t, "Thanks!", result[2].Content)

		assert.Equal(t, schema.Assistant, result[3].Role)
		assert.Equal(t, "I can help too", result[3].Content)
	})

	t.Run("non-agent prefix treated as user", func(t *testing.T) {
		msgs := []*xyncramodel.Message{
			{SenderID: "bob", Content: "Hi"},
		}
		result := convertMessages(msgs)
		require.Len(t, result, 1)
		assert.Equal(t, schema.User, result[0].Role)
	})

	t.Run("empty content produces user role with empty string", func(t *testing.T) {
		msgs := []*xyncramodel.Message{
			{SenderID: "user/alice", Content: ""},
		}
		result := convertMessages(msgs)
		require.Len(t, result, 1)
		assert.Equal(t, schema.User, result[0].Role)
		assert.Equal(t, "", result[0].Content)
	})
}

// ---------------------------------------------------------------------------
// LLMClientFactory
// ---------------------------------------------------------------------------

func TestNewLLMClientFactory_HasAllProviders(t *testing.T) {
	factory := NewLLMClientFactory()

	expectedProviders := []string{"openai", "claude", "ollama", "qwen"}
	for _, name := range expectedProviders {
		_, ok := factory.providers[name]
		assert.True(t, ok, "provider %q should be registered", name)
	}
	assert.Len(t, factory.providers, 4)
}

func TestLLMClientFactory_RegisterProvider_Replaces(t *testing.T) {
	factory := NewLLMClientFactory()

	// Register a custom provider that replaces "openai".
	custom := &mockLLMProvider{defaultBaseURL: "http://custom.example.com"}
	factory.RegisterProvider("openai", custom)

	p, ok := factory.providers["openai"]
	require.True(t, ok)
	assert.Equal(t, "http://custom.example.com", p.DefaultBaseURL())
}

func TestLLMClientFactory_Create_MissingAPIKey(t *testing.T) {
	factory := NewLLMClientFactory()

	config := &AgentConfig{
		Model:     "gpt-4",
		APIKeyEnv: "XYNCRA_TEST_NONEXISTENT_KEY_12345",
	}

	_, err := factory.Create(context.Background(), config)
	require.ErrorIs(t, err, ErrAPIKeyMissing)
}

func TestLLMClientFactory_Create_UnregisteredProvider(t *testing.T) {
	// Create a factory with an empty providers map — no providers registered.
	factory := &LLMClientFactory{providers: make(map[string]LLMProvider)}

	config := &AgentConfig{
		Model:     "gpt-4",
		APIKeyEnv: "",
	}

	_, err := factory.Create(context.Background(), config)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedModel, "empty factory should return ErrUnsupportedModel")
}

func TestLLMClientFactory_Create_UnknownModelDefaultsToOpenAI(t *testing.T) {
	factory := NewLLMClientFactory()

	// Replace openai with a mock to avoid real API calls.
	mockProvider := &mockLLMProvider{defaultBaseURL: "http://mock.openai.com"}
	factory.RegisterProvider("openai", mockProvider)

	config := &AgentConfig{
		Model:     "some-unknown-model-xyz",
		APIKeyEnv: "", // no API key required (ollama-like)
	}

	cm, err := factory.Create(context.Background(), config)
	require.NoError(t, err)
	assert.NotNil(t, cm)
}

func TestLLMClientFactory_Create_APIKeyNeverInErrorMessage(t *testing.T) {
	// Register a provider that always fails, to test that the API key
	// never leaks into error messages (security test).
	factory := NewLLMClientFactory()
	factory.RegisterProvider("openai", &mockLLMProvider{
		defaultBaseURL: "http://mock.openai.com",
		createErr:      assert.AnError,
	})

	// Set a known API key value in the environment.
	t.Setenv("XYNCRA_TEST_SECRET_KEY_99999", "super-secret-api-key-12345")

	config := &AgentConfig{
		Model:     "gpt-4",
		APIKeyEnv: "XYNCRA_TEST_SECRET_KEY_99999",
	}

	_, err := factory.Create(context.Background(), config)
	require.Error(t, err)

	// The API key value must never appear in the error message.
	assert.NotContains(t, err.Error(), "super-secret-api-key-12345",
		"API key must never appear in error messages")
}

// ---------------------------------------------------------------------------
// Provider DefaultBaseURL (D-064)
// ---------------------------------------------------------------------------

func TestProviderDefaultBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		provider LLMProvider
		expected string
	}{
		{"OpenAI", &OpenAIProvider{}, "https://api.openai.com/v1"},
		{"Claude", &ClaudeProvider{}, "https://api.anthropic.com"},
		{"Ollama", &OllamaProvider{}, "http://localhost:11434"},
		{"Qwen", &QwenProvider{}, "https://dashscope.aliyuncs.com/compatible-mode/v1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.provider.DefaultBaseURL())
		})
	}
}

// ---------------------------------------------------------------------------
// Mock LLM Provider for testing
// ---------------------------------------------------------------------------

// mockLLMProvider is a test double for LLMProvider.
type mockLLMProvider struct {
	defaultBaseURL string
	createErr      error
	createResult   einomodel.BaseChatModel
}

func (m *mockLLMProvider) CreateChatModel(_ context.Context, _ *AgentConfig, _ string) (einomodel.BaseChatModel, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	if m.createResult != nil {
		return m.createResult, nil
	}
	// Return a minimal mock that satisfies the interface.
	return &mockChatModel{}, nil
}

func (m *mockLLMProvider) DefaultBaseURL() string {
	return m.defaultBaseURL
}

// mockChatModel is a minimal test double for BaseChatModel.
type mockChatModel struct{}

func (m *mockChatModel) Generate(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	return &schema.Message{Content: "mock response"}, nil
}

func (m *mockChatModel) Stream(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	sr, sw := schema.Pipe[*schema.Message](1)
	go func() {
		sw.Send(&schema.Message{Content: "mock"}, nil)
		sw.Close()
	}()
	return sr, nil
}

// Ensure the API key env var name is properly checked but the value is not leaked.
func TestLLMClientFactory_Create_EmptyAPIKeyEnv_NoError(t *testing.T) {
	factory := NewLLMClientFactory()

	// Replace openai with a mock to avoid real API calls.
	mockProvider := &mockLLMProvider{defaultBaseURL: "http://mock.openai.com"}
	factory.RegisterProvider("openai", mockProvider)

	// No APIKeyEnv set — should succeed without checking env vars.
	config := &AgentConfig{
		Model:     "gpt-4",
		APIKeyEnv: "",
	}

	cm, err := factory.Create(context.Background(), config)
	require.NoError(t, err)
	assert.NotNil(t, cm)
}

// Verify that detectProvider is case-insensitive for baseURL and model.
func TestDetectProvider_CaseInsensitive(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		baseURL  string
		expected string
	}{
		{"uppercase ANTHROPIC in baseURL", "gpt-4", "https://ANTHROPIC.example.com", "claude"},
		{"mixed case Claude in baseURL", "gpt-4", "https://CLAUDE.example.com", "claude"},
		{"uppercase CLAUDE model", "CLAUDE-3", "", "claude"},
		{"uppercase QWEN model", "QWEN-PLUS", "", "qwen"},
		{"uppercase LLAMA model", "LLAMA3", "", "ollama"},
		{"uppercase MISTRAL model", "MISTRAL-7B", "", "ollama"},
		{"uppercase GEMMA model", "GEMMA-2B", "", "ollama"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := detectProvider(tc.model, tc.baseURL)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// Verify error message for ErrAPIKeyMissing is descriptive.
func TestErrAPIKeyMissing_Message(t *testing.T) {
	assert.True(t, strings.Contains(ErrAPIKeyMissing.Error(), "API key"))
}
