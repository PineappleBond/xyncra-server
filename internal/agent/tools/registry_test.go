package tools

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// newStubInvokableTool creates a minimal InvokableTool for testing.
func newStubInvokableTool(name string) tool.InvokableTool {
	t, _ := utils.InferTool(name, "stub tool", func(ctx context.Context, input struct{}) (*struct{}, error) {
		return &struct{}{}, nil
	})
	return t
}

// ---------------------------------------------------------------------------
// TestRegistry_RegisterAndCreate
// ---------------------------------------------------------------------------

func TestRegistry_RegisterAndCreate(t *testing.T) {
	reg := NewRegistry()
	reg.Register("my_tool", func(ctx context.Context, _ map[string]any) (tool.BaseTool, error) {
		return newStubInvokableTool("my_tool"), nil
	})

	tools, err := reg.Create(context.Background(), []string{"my_tool"}, nil)
	if err != nil {
		t.Fatalf("Create returned unexpected error: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
}

// ---------------------------------------------------------------------------
// TestRegistry_UnregisteredToolSkipped
// ---------------------------------------------------------------------------

func TestRegistry_UnregisteredToolSkipped(t *testing.T) {
	reg := NewRegistry()
	tools, err := reg.Create(context.Background(), []string{"nonexistent_tool"}, nil)
	if err != nil {
		t.Fatalf("expected no error for unregistered tool (fail-open D-078), got: %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("expected 0 tools for unregistered name, got %d", len(tools))
	}
}

// ---------------------------------------------------------------------------
// TestRegistry_FactoryError
// ---------------------------------------------------------------------------

func TestRegistry_FactoryError(t *testing.T) {
	reg := NewRegistry()
	reg.Register("bad_tool", func(ctx context.Context, _ map[string]any) (tool.BaseTool, error) {
		return nil, fmt.Errorf("factory boom")
	})

	tools, err := reg.Create(context.Background(), []string{"bad_tool"}, nil)
	if err == nil {
		t.Fatal("expected error from factory, got nil")
	}
	if len(tools) != 0 {
		t.Errorf("expected 0 tools on factory error, got %d", len(tools))
	}
}

// ---------------------------------------------------------------------------
// TestRegistry_ConcurrentAccess
// ---------------------------------------------------------------------------

func TestRegistry_ConcurrentAccess(t *testing.T) {
	reg := NewRegistry()

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("tool_%d", i)
			reg.Register(name, func(ctx context.Context, _ map[string]any) (tool.BaseTool, error) {
				return newStubInvokableTool(name), nil
			})
		}(i)
	}
	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("tool_%d", i)
			_, _ = reg.Create(context.Background(), []string{name}, nil)
		}(i)
	}

	wg.Wait()

	names := reg.ListNames()
	if len(names) != 50 {
		t.Errorf("expected 50 registered tools, got %d", len(names))
	}
}

// ---------------------------------------------------------------------------
// TestRegistry_ListNames
// ---------------------------------------------------------------------------

func TestRegistry_ListNames(t *testing.T) {
	reg := NewRegistry()
	reg.Register("zebra", func(ctx context.Context, _ map[string]any) (tool.BaseTool, error) {
		return newStubInvokableTool("zebra"), nil
	})
	reg.Register("apple", func(ctx context.Context, _ map[string]any) (tool.BaseTool, error) {
		return newStubInvokableTool("apple"), nil
	})
	reg.Register("mango", func(ctx context.Context, _ map[string]any) (tool.BaseTool, error) {
		return newStubInvokableTool("mango"), nil
	})

	names := reg.ListNames()
	expected := []string{"apple", "mango", "zebra"}
	if len(names) != len(expected) {
		t.Fatalf("expected %d names, got %d", len(expected), len(names))
	}
	for i, n := range expected {
		if names[i] != n {
			t.Errorf("names[%d] = %q, want %q", i, names[i], n)
		}
	}
}

// ---------------------------------------------------------------------------
// TestDefaultRegistry_HasBuiltinTools
// ---------------------------------------------------------------------------

func TestDefaultRegistry_HasBuiltinTools(t *testing.T) {
	expectedTools := []string{"get_weather", "get_current_time", "retrieve_tool_result"}
	names := DefaultRegistry.ListNames()
	for _, name := range expectedTools {
		if !slices.Contains(names, name) {
			t.Errorf("DefaultRegistry missing built-in tool %q", name)
		}
	}
}

// ---------------------------------------------------------------------------
// TestRegistry_Create_NilNames
// ---------------------------------------------------------------------------

func TestRegistry_Create_NilNames(t *testing.T) {
	reg := NewRegistry()
	tools, err := reg.Create(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(tools))
	}
}

// ---------------------------------------------------------------------------
// TestRegistry_Create_PartialFailure
// ---------------------------------------------------------------------------

func TestRegistry_Create_PartialFailure(t *testing.T) {
	reg := NewRegistry()
	reg.Register("good", func(ctx context.Context, _ map[string]any) (tool.BaseTool, error) {
		return newStubInvokableTool("good"), nil
	})
	reg.Register("bad", func(ctx context.Context, _ map[string]any) (tool.BaseTool, error) {
		return nil, fmt.Errorf("factory error")
	})
	tools, err := reg.Create(context.Background(), []string{"good", "bad"}, nil)
	if err == nil {
		t.Fatal("expected error for partial failure")
	}
	if len(tools) != 1 {
		t.Errorf("expected 1 successful tool, got %d", len(tools))
	}
}
