package agent

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFrontMatter_ValidFull(t *testing.T) {
	data, err := os.ReadFile("testdata/valid-full.md")
	require.NoError(t, err)

	config, err := ParseFrontMatter(data)
	require.NoError(t, err)
	require.NotNil(t, config)

	assert.Equal(t, "test-bot", config.ID)
	assert.Equal(t, "Test Bot", config.Name)
	assert.Equal(t, "A full test configuration", config.Description)
	assert.Equal(t, "gpt-4", config.Model)
	assert.Equal(t, "TEST_API_KEY", config.APIKeyEnv)
	assert.Equal(t, "https://api.example.com/v1", config.BaseURL)

	assert.InDelta(t, 0.5, config.Parameters.Temperature, 0.001)
	assert.Equal(t, 1000, config.Parameters.MaxTokens)
	assert.InDelta(t, 0.95, config.Parameters.TopP, 0.001)

	assert.Equal(t, 4000, config.Context.MaxTokens)
	assert.Equal(t, 10, config.Context.MaxMessages)

	assert.Equal(t, []string{"search", "calculator"}, config.Tools)

	assert.NotEmpty(t, config.SystemPrompt)
	assert.Contains(t, config.SystemPrompt, "You are a test bot.")
	assert.Contains(t, config.SystemPrompt, "Do testing things.")
}

func TestParseFrontMatter_ValidMinimal(t *testing.T) {
	data, err := os.ReadFile("testdata/valid-minimal.md")
	require.NoError(t, err)

	config, err := ParseFrontMatter(data)
	require.NoError(t, err)
	require.NotNil(t, config)

	assert.Equal(t, "minimal-bot", config.ID)
	assert.Equal(t, "Minimal Bot", config.Name)
	assert.Equal(t, "gpt-3.5-turbo", config.Model)
	assert.Equal(t, "MINIMAL_KEY", config.APIKeyEnv)

	// Optional fields should be zero values.
	assert.Empty(t, config.Description)
	assert.Empty(t, config.BaseURL)
	assert.Zero(t, config.Parameters.Temperature)
	assert.Zero(t, config.Parameters.MaxTokens)
	assert.Zero(t, config.Parameters.TopP)
	assert.Zero(t, config.Context.MaxTokens)
	assert.Zero(t, config.Context.MaxMessages)
	assert.Nil(t, config.Tools)
}

func TestParseFrontMatter_InvalidYAML(t *testing.T) {
	data, err := os.ReadFile("testdata/invalid-yaml.md")
	require.NoError(t, err)

	_, err = ParseFrontMatter(data)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidFrontMatter), "expected ErrInvalidFrontMatter, got: %v", err)
}

func TestParseFrontMatter_MissingID(t *testing.T) {
	data, err := os.ReadFile("testdata/missing-id.md")
	require.NoError(t, err)

	_, err = ParseFrontMatter(data)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMissingID), "expected ErrMissingID, got: %v", err)
}

func TestParseFrontMatter_MissingModel(t *testing.T) {
	data, err := os.ReadFile("testdata/missing-model.md")
	require.NoError(t, err)

	_, err = ParseFrontMatter(data)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMissingModel), "expected ErrMissingModel, got: %v", err)
}

func TestParseFrontMatter_NoFrontMatter(t *testing.T) {
	data, err := os.ReadFile("testdata/no-frontmatter.md")
	require.NoError(t, err)

	_, err = ParseFrontMatter(data)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoFrontMatter), "expected ErrNoFrontMatter, got: %v", err)
}

func TestParseFrontMatter_EmptyBody(t *testing.T) {
	data, err := os.ReadFile("testdata/empty-body.md")
	require.NoError(t, err)

	config, err := ParseFrontMatter(data)
	require.NoError(t, err)
	require.NotNil(t, config)

	assert.Equal(t, "empty-body-bot", config.ID)
	assert.Equal(t, "", config.SystemPrompt)
}

func TestParseFrontMatter_UnicodeContent(t *testing.T) {
	data := []byte(`---
id: unicode-bot
name: 机器人
model: gpt-4
api_key_env: KEY
---

你是一个测试机器人。
请做测试相关的事情。`)

	config, err := ParseFrontMatter(data)
	require.NoError(t, err)
	require.NotNil(t, config)

	assert.Equal(t, "unicode-bot", config.ID)
	assert.Equal(t, "机器人", config.Name)
	assert.Contains(t, config.SystemPrompt, "你是一个测试机器人。")
	assert.Contains(t, config.SystemPrompt, "请做测试相关的事情。")
}

func TestParseFrontMatter_EmptyInput(t *testing.T) {
	_, err := ParseFrontMatter([]byte{})
	assert.ErrorIs(t, err, ErrNoFrontMatter)

	_, err = ParseFrontMatter(nil)
	assert.ErrorIs(t, err, ErrNoFrontMatter)
}

func TestParseFrontMatter_ContentBeforeFirstDash(t *testing.T) {
	data := []byte("# Some comment\n---\nid: test\nname: Test\nmodel: gpt-4\napi_key_env: KEY\n---\nbody")
	config, err := ParseFrontMatter(data)
	require.NoError(t, err)
	assert.Equal(t, "test", config.ID)
}

func TestParseFrontMatter_EmptyYAML(t *testing.T) {
	data := []byte("---\n---\nbody")
	_, err := ParseFrontMatter(data)
	assert.ErrorIs(t, err, ErrMissingID)
}

func TestParseFrontMatter_SingleDash(t *testing.T) {
	data := []byte("---\nid: test\nname: Test\n")
	_, err := ParseFrontMatter(data)
	assert.ErrorIs(t, err, ErrNoFrontMatter)
}

func TestParseFrontMatter_MultipleDashes(t *testing.T) {
	data := []byte("---\nid: test\nname: Test\nmodel: gpt-4\napi_key_env: KEY\n---\nbody\n---\nmore body")
	config, err := ParseFrontMatter(data)
	require.NoError(t, err)
	assert.Contains(t, config.SystemPrompt, "---")
	assert.Contains(t, config.SystemPrompt, "more body")
}

func TestParseFrontMatter_ExtraWhitespace(t *testing.T) {
	data := []byte(`---
id: ws-bot
name: Whitespace Bot
model: gpt-4
api_key_env: KEY
---


  Hello, world!


`)

	config, err := ParseFrontMatter(data)
	require.NoError(t, err)
	require.NotNil(t, config)

	// ParseFrontMatter trims the body with strings.TrimSpace.
	assert.Equal(t, "Hello, world!", config.SystemPrompt)
}
