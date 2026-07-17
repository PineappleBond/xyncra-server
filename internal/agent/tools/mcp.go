package tools

import (
	"context"
	"fmt"
	"log"
	"sync"

	mcpp "github.com/cloudwego/eino-ext/components/tool/mcp"
	"github.com/cloudwego/eino/components/tool"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// MCPBridge manages connections to MCP servers and provides their tools.
// It supports both SSE and stdio transports (D-086).
//
// The bridge accepts separate ConnectSSE and ConnectStdio methods rather
// than a single method that takes a config struct. This avoids a circular
// import between the agent and agent/tools packages.
type MCPBridge struct {
	mu      sync.Mutex
	clients map[string]client.MCPClient // name -> mcp-go client
	logger  *log.Logger
}

// NewMCPBridge creates an MCPBridge.
// If logger is nil, log.Default() is used.
func NewMCPBridge(logger *log.Logger) *MCPBridge {
	if logger == nil {
		logger = log.Default()
	}
	return &MCPBridge{
		clients: make(map[string]client.MCPClient),
		logger:  logger,
	}
}

// ConnectSSE establishes a connection to an MCP server over SSE transport
// and returns its tools as Eino tool.BaseTool slice.
// If toolFilter is non-empty, only those tools are returned.
func (b *MCPBridge) ConnectSSE(ctx context.Context, name, url string, toolFilter []string) ([]tool.BaseTool, error) {
	cli, err := client.NewSSEMCPClient(url)
	if err != nil {
		return nil, fmt.Errorf("mcp: create SSE client for %q: %w", name, err)
	}

	if err := cli.Start(ctx); err != nil {
		return nil, fmt.Errorf("mcp: start SSE client for %q: %w", name, err)
	}

	// Initialize (MCP handshake) without storing in the map yet (D-086 review).
	if err := b.initializeClient(ctx, name, cli); err != nil {
		_ = cli.Close()
		return nil, err
	}

	// getTools must succeed before we track the client (avoids orphan connections).
	tools, err := b.getTools(ctx, name, cli, toolFilter)
	if err != nil {
		_ = cli.Close()
		return nil, err
	}

	// All steps succeeded — register the client in the map.
	b.mu.Lock()
	if old, exists := b.clients[name]; exists {
		_ = old.Close()
	}
	b.clients[name] = cli
	b.mu.Unlock()

	return tools, nil
}

// ConnectStdio establishes a connection to an MCP server over stdio transport
// and returns its tools as Eino tool.BaseTool slice.
// If toolFilter is non-empty, only those tools are returned.
func (b *MCPBridge) ConnectStdio(ctx context.Context, name, command string, args, env []string, toolFilter []string) ([]tool.BaseTool, error) {
	cli, err := client.NewStdioMCPClient(command, env, args...)
	if err != nil {
		return nil, fmt.Errorf("mcp: create stdio client for %q: %w", name, err)
	}

	// Initialize (MCP handshake) without storing in the map yet (D-086 review).
	if err := b.initializeClient(ctx, name, cli); err != nil {
		_ = cli.Close()
		return nil, err
	}

	// getTools must succeed before we track the client (avoids orphan connections).
	tools, err := b.getTools(ctx, name, cli, toolFilter)
	if err != nil {
		_ = cli.Close()
		return nil, err
	}

	// All steps succeeded — register the client in the map.
	b.mu.Lock()
	if old, exists := b.clients[name]; exists {
		_ = old.Close()
	}
	b.clients[name] = cli
	b.mu.Unlock()

	return tools, nil
}

// CloseAll closes all MCP client connections and cleans up resources.
func (b *MCPBridge) CloseAll() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for name, cli := range b.clients {
		if err := cli.Close(); err != nil {
			b.logger.Printf("[WARN] mcp: close %q: %v", name, err)
		}
		delete(b.clients, name)
	}
}

// initializeClient sends the MCP Initialize request to the server.
// It does NOT store the client in the bridge's map — callers must do that
// after getTools succeeds (D-086 review: prevents orphan connections).
func (b *MCPBridge) initializeClient(ctx context.Context, name string, cli *client.Client) error {
	req := mcp.InitializeRequest{}
	req.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	req.Params.ClientInfo = mcp.Implementation{
		Name:    "xyncra",
		Version: "1.0.0",
	}

	if _, err := cli.Initialize(ctx, req); err != nil {
		return fmt.Errorf("mcp: initialize %q: %w", name, err)
	}

	return nil
}

// getTools retrieves tools from an initialized MCP client using eino-ext's bridge.
func (b *MCPBridge) getTools(ctx context.Context, name string, cli *client.Client, toolFilter []string) ([]tool.BaseTool, error) {
	cfg := &mcpp.Config{
		Cli:          cli,
		ToolNameList: toolFilter,
	}

	tools, err := mcpp.GetTools(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("mcp: get tools from %q: %w", name, err)
	}

	return tools, nil
}
