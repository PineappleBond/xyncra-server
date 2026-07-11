package tools

import (
	"context"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// newMockMCPServer creates an MCP server with two test tools for SSE testing.
func newMockMCPServer() *server.MCPServer {
	svr := server.NewMCPServer("test-server", "1.0.0")
	svr.AddTool(
		mcp.NewTool("test_tool",
			mcp.WithDescription("A test tool"),
			mcp.WithString("input", mcp.Required()),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("test result"), nil
		},
	)
	svr.AddTool(
		mcp.NewTool("another_tool",
			mcp.WithDescription("Another test tool"),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("another result"), nil
		},
	)
	return svr
}

// ---------------------------------------------------------------------------
// TestNewMCPBridge
// ---------------------------------------------------------------------------

func TestNewMCPBridge(t *testing.T) {
	t.Run("nil logger uses log.Default()", func(t *testing.T) {
		bridge := NewMCPBridge(nil)
		if bridge == nil {
			t.Fatal("NewMCPBridge(nil) returned nil")
		}
		if bridge.logger == nil {
			t.Fatal("expected non-nil logger after nil input")
		}
		if bridge.clients == nil {
			t.Fatal("expected non-nil clients map")
		}
	})

	t.Run("custom logger preserved", func(t *testing.T) {
		custom := log.New(log.Default().Writer(), "CUSTOM ", 0)
		bridge := NewMCPBridge(custom)
		if bridge.logger != custom {
			t.Fatal("expected custom logger to be preserved")
		}
	})
}

// ---------------------------------------------------------------------------
// TestMCPBridge_ConnectSSE_ServerUnreachable
// ---------------------------------------------------------------------------

func TestMCPBridge_ConnectSSE_ServerUnreachable(t *testing.T) {
	bridge := NewMCPBridge(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Port 1 is almost certainly not listening.
	_, err := bridge.ConnectSSE(ctx, "unreachable", "http://127.0.0.1:1/sse", nil)
	if err == nil {
		t.Fatal("expected error for unreachable server, got nil")
	}
}

// ---------------------------------------------------------------------------
// TestMCPBridge_ConnectSSE_InvalidURL
// ---------------------------------------------------------------------------

func TestMCPBridge_ConnectSSE_InvalidURL(t *testing.T) {
	bridge := NewMCPBridge(nil)
	ctx := context.Background()

	_, err := bridge.ConnectSSE(ctx, "bad-url", "", nil)
	if err == nil {
		t.Fatal("expected error for empty URL, got nil")
	}
}

// ---------------------------------------------------------------------------
// TestMCPBridge_ConnectStdio_CommandNotFound
// ---------------------------------------------------------------------------

func TestMCPBridge_ConnectStdio_CommandNotFound(t *testing.T) {
	bridge := NewMCPBridge(nil)
	ctx := context.Background()

	_, err := bridge.ConnectStdio(ctx, "bad-cmd", "nonexistent_command_xyz_12345", nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for non-existent command, got nil")
	}
}

// ---------------------------------------------------------------------------
// TestMCPBridge_CloseAll_Empty
// ---------------------------------------------------------------------------

func TestMCPBridge_CloseAll_Empty(t *testing.T) {
	bridge := NewMCPBridge(nil)
	// Should not panic on empty bridge.
	bridge.CloseAll()
}

// ---------------------------------------------------------------------------
// TestMCPBridge_ConnectSSE_MockServer
// ---------------------------------------------------------------------------

func TestMCPBridge_ConnectSSE_MockServer(t *testing.T) {
	mcpSvr := newMockMCPServer()
	testSvr := server.NewTestServer(mcpSvr)
	defer testSvr.Close()

	bridge := NewMCPBridge(nil)
	defer bridge.CloseAll()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sseURL := testSvr.URL + "/sse"
	tools, err := bridge.ConnectSSE(ctx, "mock-sse", sseURL, nil)
	if err != nil {
		t.Fatalf("ConnectSSE failed: %v", err)
	}
	if len(tools) == 0 {
		t.Fatal("expected at least one tool from mock server, got 0")
	}

	// Verify tool names are present.
	names := make(map[string]bool)
	for _, tl := range tools {
		info, err := tl.Info(ctx)
		if err != nil {
			t.Fatalf("tool.Info() failed: %v", err)
		}
		names[info.Name] = true
	}
	if !names["test_tool"] {
		t.Error("expected test_tool in tool list")
	}
	if !names["another_tool"] {
		t.Error("expected another_tool in tool list")
	}
}

// ---------------------------------------------------------------------------
// TestMCPBridge_ConnectSSE_ToolFilter
// ---------------------------------------------------------------------------

func TestMCPBridge_ConnectSSE_ToolFilter(t *testing.T) {
	mcpSvr := newMockMCPServer()
	testSvr := server.NewTestServer(mcpSvr)
	defer testSvr.Close()

	bridge := NewMCPBridge(nil)
	defer bridge.CloseAll()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sseURL := testSvr.URL + "/sse"
	// Only request "test_tool", should not include "another_tool".
	tools, err := bridge.ConnectSSE(ctx, "mock-filtered", sseURL, []string{"test_tool"})
	if err != nil {
		t.Fatalf("ConnectSSE with filter failed: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected exactly 1 tool after filter, got %d", len(tools))
	}

	info, err := tools[0].Info(ctx)
	if err != nil {
		t.Fatalf("tool.Info() failed: %v", err)
	}
	if info.Name != "test_tool" {
		t.Errorf("expected tool name 'test_tool', got %q", info.Name)
	}
}

// ---------------------------------------------------------------------------
// TestMCPBridge_CloseAll_AfterConnect
// ---------------------------------------------------------------------------

func TestMCPBridge_CloseAll_AfterConnect(t *testing.T) {
	mcpSvr := newMockMCPServer()
	testSvr := server.NewTestServer(mcpSvr)
	defer testSvr.Close()

	bridge := NewMCPBridge(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sseURL := testSvr.URL + "/sse"
	_, err := bridge.ConnectSSE(ctx, "cleanup-test", sseURL, nil)
	if err != nil {
		t.Fatalf("ConnectSSE failed: %v", err)
	}

	// Verify client is tracked.
	bridge.mu.Lock()
	if len(bridge.clients) != 1 {
		t.Errorf("expected 1 client tracked, got %d", len(bridge.clients))
	}
	bridge.mu.Unlock()

	// CloseAll should clean up.
	bridge.CloseAll()

	bridge.mu.Lock()
	if len(bridge.clients) != 0 {
		t.Errorf("expected 0 clients after CloseAll, got %d", len(bridge.clients))
	}
	bridge.mu.Unlock()
}

// ---------------------------------------------------------------------------
// TestMCPBridge_ConnectSSE_ErrorDoesNotTrackClient
// ---------------------------------------------------------------------------

func TestMCPBridge_ConnectSSE_ErrorDoesNotTrackClient(t *testing.T) {
	bridge := NewMCPBridge(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _ = bridge.ConnectSSE(ctx, "fail", "http://127.0.0.1:1/sse", nil)

	bridge.mu.Lock()
	if len(bridge.clients) != 0 {
		t.Errorf("expected 0 clients after failed connect, got %d", len(bridge.clients))
	}
	bridge.mu.Unlock()
}

// ---------------------------------------------------------------------------
// TestMCPBridge_ConnectSSE_ToolFilter_NoMatch
// ---------------------------------------------------------------------------

func TestMCPBridge_ConnectSSE_ToolFilter_NoMatch(t *testing.T) {
	mcpSvr := newMockMCPServer()
	testSvr := server.NewTestServer(mcpSvr)
	defer testSvr.Close()

	bridge := NewMCPBridge(nil)
	defer bridge.CloseAll()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sseURL := testSvr.URL + "/sse"
	// Request a tool name that doesn't exist on the server.
	tools, err := bridge.ConnectSSE(ctx, "no-match", sseURL, []string{"nonexistent_tool"})
	if err != nil {
		t.Fatalf("ConnectSSE with non-matching filter should not error: %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("expected 0 tools for non-matching filter, got %d", len(tools))
	}
}

// ---------------------------------------------------------------------------
// TestMCPBridge_ConnectSSE_InvalidURLFormat
// ---------------------------------------------------------------------------

func TestMCPBridge_ConnectSSE_InvalidURLFormat(t *testing.T) {
	bridge := NewMCPBridge(nil)
	ctx := context.Background()

	_, err := bridge.ConnectSSE(ctx, "bad-format", "://not-a-url", nil)
	if err == nil {
		t.Fatal("expected error for malformed URL, got nil")
	}
	if !strings.Contains(err.Error(), "bad-format") {
		t.Errorf("expected error to contain server name, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestMCPBridge_ConnectSSE_Concurrent
// ---------------------------------------------------------------------------

// TestMCPBridge_ConnectSSE_Concurrent verifies that concurrent ConnectSSE
// calls are safe under -race and that all clients are tracked correctly.
func TestMCPBridge_ConnectSSE_Concurrent(t *testing.T) {
	mcpSvr := newMockMCPServer()
	testSvr := server.NewTestServer(mcpSvr)
	defer testSvr.Close()

	bridge := NewMCPBridge(nil)
	defer bridge.CloseAll()

	const n = 8
	errs := make([]error, n)

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := "concurrent-" + string(rune('a'+idx))
			_, err := bridge.ConnectSSE(context.Background(), name, testSvr.URL+"/sse", nil)
			errs[idx] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d failed: %v", i, err)
		}
	}

	bridge.mu.Lock()
	count := len(bridge.clients)
	bridge.mu.Unlock()
	if count != n {
		t.Errorf("expected %d clients tracked, got %d", n, count)
	}
}

// ---------------------------------------------------------------------------
// TestMCPBridge_ConnectSSE_GetToolsFails_NoLeak
// ---------------------------------------------------------------------------

// TestMCPBridge_ConnectSSE_GetToolsFails_NoLeak verifies that when getTools
// fails (e.g., context cancelled after initialize but before tool retrieval),
// the client is not leaked in the bridge's map.
func TestMCPBridge_ConnectSSE_GetToolsFails_NoLeak(t *testing.T) {
	mcpSvr := newMockMCPServer()
	testSvr := server.NewTestServer(mcpSvr)
	defer testSvr.Close()

	bridge := NewMCPBridge(nil)
	defer bridge.CloseAll()

	// Use a cancelled context so getTools fails after initialize succeeds.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := bridge.ConnectSSE(ctx, "leak-test", testSvr.URL+"/sse", nil)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}

	bridge.mu.Lock()
	count := len(bridge.clients)
	bridge.mu.Unlock()
	if count != 0 {
		t.Errorf("expected 0 clients after failed getTools, got %d (leak!)", count)
	}
}
