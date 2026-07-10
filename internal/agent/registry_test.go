package agent

import (
	"sync"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validFullContent is the raw text of a fully-populated agent definition.
const validFullContent = `---
id: test-bot
name: Test Bot
description: "A full test configuration"
model: gpt-4
api_key_env: TEST_API_KEY
base_url: "https://api.example.com/v1"
parameters:
  temperature: 0.5
  max_tokens: 1000
  top_p: 0.95
context:
  max_tokens: 4000
  max_messages: 10
tools:
  - search
  - calculator
---

You are a test bot.
Do testing things.
`

const validMinimalContent = `---
id: minimal-bot
name: Minimal Bot
model: gpt-3.5-turbo
api_key_env: MINIMAL_KEY
---
`

const validSecondContent = `---
id: weather-bot
name: Weather Bot
model: gpt-4
api_key_env: WEATHER_KEY
---

Check the weather.
`

const invalidYAMLContent = `---
id: [broken
  yaml: {bad
---
body
`

const missingIDContent = `---
name: No ID
model: gpt-4
api_key_env: KEY
---
body
`

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	assert.Equal(t, 0, r.Count())
	assert.Empty(t, r.ListAll())
}

func TestRegistry_Load_ValidConfigs(t *testing.T) {
	fsys := fstest.MapFS{
		"bot1.md": &fstest.MapFile{Data: []byte(validFullContent)},
		"bot2.md": &fstest.MapFile{Data: []byte(validMinimalContent)},
	}

	r := NewRegistry()
	err := r.Load(fsys)
	require.NoError(t, err)
	assert.Equal(t, 2, r.Count())

	cfg, ok := r.Get("test-bot")
	require.True(t, ok)
	assert.Equal(t, "Test Bot", cfg.Name)
	assert.Equal(t, "gpt-4", cfg.Model)

	cfg, ok = r.Get("minimal-bot")
	require.True(t, ok)
	assert.Equal(t, "Minimal Bot", cfg.Name)
}

func TestRegistry_Load_SkipsInvalid(t *testing.T) {
	fsys := fstest.MapFS{
		"valid.md":  &fstest.MapFile{Data: []byte(validFullContent)},
		"broken.md": &fstest.MapFile{Data: []byte(invalidYAMLContent)},
		"noid.md":   &fstest.MapFile{Data: []byte(missingIDContent)},
		"valid2.md": &fstest.MapFile{Data: []byte(validMinimalContent)},
	}

	r := NewRegistry()
	err := r.Load(fsys)
	require.NoError(t, err)

	// Only the two valid configs should be loaded.
	assert.Equal(t, 2, r.Count())
	_, ok := r.Get("test-bot")
	assert.True(t, ok)
	_, ok = r.Get("minimal-bot")
	assert.True(t, ok)
}

func TestRegistry_Load_DuplicateID(t *testing.T) {
	// Two files with the same ID — the first one wins.
	first := `---
id: dup-bot
name: First
model: gpt-4
api_key_env: KEY
---
first
`
	second := `---
id: dup-bot
name: Second
model: gpt-3.5-turbo
api_key_env: KEY
---
second
`
	fsys := fstest.MapFS{
		"a.md": &fstest.MapFile{Data: []byte(first)},
		"b.md": &fstest.MapFile{Data: []byte(second)},
	}

	r := NewRegistry()
	err := r.Load(fsys)
	require.NoError(t, err)

	assert.Equal(t, 1, r.Count())
	cfg, ok := r.Get("dup-bot")
	require.True(t, ok)
	// fstest.MapFS sorts by filename, so a.md loads before b.md.
	// Therefore "First" (from a.md) should win.
	assert.Equal(t, "First", cfg.Name)
}

func TestRegistry_Get_Exists(t *testing.T) {
	fsys := fstest.MapFS{
		"bot.md": &fstest.MapFile{Data: []byte(validFullContent)},
	}
	r := NewRegistry()
	require.NoError(t, r.Load(fsys))

	cfg, ok := r.Get("test-bot")
	assert.True(t, ok)
	assert.NotNil(t, cfg)
	assert.Equal(t, "test-bot", cfg.ID)
}

func TestRegistry_Get_NotExists(t *testing.T) {
	r := NewRegistry()
	cfg, ok := r.Get("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, cfg)
}

func TestRegistry_IsAgent_ValidAgent(t *testing.T) {
	fsys := fstest.MapFS{
		"weather.md": &fstest.MapFile{Data: []byte(validSecondContent)},
	}
	r := NewRegistry()
	require.NoError(t, r.Load(fsys))

	cfg, ok := r.IsAgent("agent/weather-bot")
	assert.True(t, ok)
	assert.NotNil(t, cfg)
	assert.Equal(t, "weather-bot", cfg.ID)
}

func TestRegistry_IsAgent_NormalUser(t *testing.T) {
	r := NewRegistry()
	cfg, ok := r.IsAgent("user/alice")
	assert.False(t, ok)
	assert.Nil(t, cfg)
}

func TestRegistry_IsAgent_UnknownAgent(t *testing.T) {
	fsys := fstest.MapFS{
		"bot.md": &fstest.MapFile{Data: []byte(validFullContent)},
	}
	r := NewRegistry()
	require.NoError(t, r.Load(fsys))

	cfg, ok := r.IsAgent("agent/unknown")
	assert.False(t, ok)
	assert.Nil(t, cfg)
}

func TestRegistry_IsAgent_EmptyString(t *testing.T) {
	r := NewRegistry()
	cfg, ok := r.IsAgent("")
	assert.False(t, ok)
	assert.Nil(t, cfg)
}

func TestRegistry_IsAgent_PrefixOnly(t *testing.T) {
	r := NewRegistry()
	cfg, ok := r.IsAgent("agent/")
	assert.False(t, ok)
	assert.Nil(t, cfg)
}

func TestRegistry_ListAll_ReturnsCopy(t *testing.T) {
	fsys := fstest.MapFS{
		"bot.md": &fstest.MapFile{Data: []byte(validFullContent)},
	}
	r := NewRegistry()
	require.NoError(t, r.Load(fsys))

	list := r.ListAll()
	require.Len(t, list, 1)

	// Clearing the returned slice must not affect the registry.
	list[0] = nil
	list = list[:0]

	assert.Equal(t, 1, r.Count())
	cfg, ok := r.Get("test-bot")
	assert.True(t, ok)
	assert.NotNil(t, cfg)
}

func TestRegistry_ConcurrentReads(t *testing.T) {
	fsys := fstest.MapFS{
		"bot1.md": &fstest.MapFile{Data: []byte(validFullContent)},
		"bot2.md": &fstest.MapFile{Data: []byte(validMinimalContent)},
		"bot3.md": &fstest.MapFile{Data: []byte(validSecondContent)},
	}
	r := NewRegistry()
	require.NoError(t, r.Load(fsys))

	const goroutines = 50
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := range goroutines {
		go func(i int) {
			defer wg.Done()
			for range iterations {
				if i%2 == 0 {
					r.Get("test-bot")
					r.Get("nonexistent")
				} else {
					r.IsAgent("agent/weather-bot")
					r.IsAgent("user/alice")
				}
				r.ListAll()
				r.Count()
			}
		}(g)
	}
	wg.Wait()
}

func TestRegistry_ConcurrentLoadAndGet(t *testing.T) {
	// Pre-load a registry so Get/IsAgent/ListAll/Count have data to read.
	fsys := fstest.MapFS{
		"bot1.md": &fstest.MapFile{Data: []byte(validFullContent)},
		"bot2.md": &fstest.MapFile{Data: []byte(validMinimalContent)},
	}
	r := NewRegistry()
	require.NoError(t, r.Load(fsys))

	const goroutines = 50
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Half the goroutines do read operations.
	for g := range goroutines {
		go func(i int) {
			defer wg.Done()
			for range iterations {
				switch i % 4 {
				case 0:
					r.Get("test-bot")
					r.Get("nonexistent")
				case 1:
					r.IsAgent("agent/test-bot")
					r.IsAgent("user/alice")
				case 2:
					r.ListAll()
				case 3:
					r.Count()
				}
			}
		}(g)
	}

	// The other half do Load (write) operations concurrently.
	for range goroutines {
		go func() {
			defer wg.Done()
			for range iterations {
				_ = r.Load(fsys)
			}
		}()
	}

	wg.Wait()
}
