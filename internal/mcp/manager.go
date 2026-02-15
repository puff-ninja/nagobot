package mcp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/joebot/nagobot/internal/config"
	"github.com/joebot/nagobot/internal/tool"
)

// Manager manages the lifecycle of multiple MCP server connections.
type Manager struct {
	clients []*Client
	tools   []*MCPBridgeTool
}

// NewManager creates a Manager and connects to all configured MCP servers.
func NewManager(ctx context.Context, cfg config.MCPConfig) (*Manager, error) {
	m := &Manager{}

	for name, serverCfg := range cfg.Servers {
		var transport Transport
		switch {
		case serverCfg.Command != "":
			transport = NewStdioTransport(serverCfg.Command, serverCfg.Args, serverCfg.Env)
		case serverCfg.URL != "":
			transport = NewHTTPTransport(serverCfg.URL, serverCfg.Headers)
		default:
			slog.Warn("MCP server has no command or url, skipping", "server", name)
			continue
		}

		client := NewClient(name, transport)
		if err := client.Initialize(ctx); err != nil {
			slog.Error("MCP server initialize failed", "server", name, "err", err)
			client.Close()
			continue
		}

		mcpTools, err := client.ListTools(ctx)
		if err != nil {
			slog.Error("MCP server tools/list failed", "server", name, "err", err)
			client.Close()
			continue
		}

		m.clients = append(m.clients, client)
		for _, mt := range mcpTools {
			m.tools = append(m.tools, NewMCPBridgeTool(client, mt))
		}

		slog.Info("MCP server connected", "server", name, "tools", len(mcpTools))
	}

	return m, nil
}

// RegisterTools registers all MCP tools into the given tool registry.
func (m *Manager) RegisterTools(registry *tool.Registry) {
	for _, t := range m.tools {
		registry.Register(t)
	}
}

// ToolCount returns the total number of MCP tools available.
func (m *Manager) ToolCount() int {
	return len(m.tools)
}

// ServerNames returns the names of connected servers.
func (m *Manager) ServerNames() []string {
	names := make([]string, len(m.clients))
	for i, c := range m.clients {
		names[i] = c.Name()
	}
	return names
}

// Close shuts down all MCP server connections.
func (m *Manager) Close() {
	for _, c := range m.clients {
		if err := c.Close(); err != nil {
			slog.Warn("MCP server close error", "server", c.Name(), "err", err)
		}
	}
}

// --- MCPBridgeTool: adapts an MCP tool to the tool.Tool interface ---

// MCPBridgeTool wraps an MCP tool so it can be registered in tool.Registry.
type MCPBridgeTool struct {
	client  *Client
	mcpTool MCPTool
	name    string
}

// NewMCPBridgeTool creates an adapter from an MCP tool definition.
func NewMCPBridgeTool(client *Client, mcpTool MCPTool) *MCPBridgeTool {
	return &MCPBridgeTool{
		client:  client,
		mcpTool: mcpTool,
		name:    fmt.Sprintf("mcp__%s__%s", client.Name(), mcpTool.Name),
	}
}

func (t *MCPBridgeTool) Name() string        { return t.name }
func (t *MCPBridgeTool) Description() string  { return t.mcpTool.Description }
func (t *MCPBridgeTool) Parameters() map[string]any { return t.mcpTool.InputSchema }

func (t *MCPBridgeTool) Execute(ctx context.Context, params map[string]any) (tool.ToolResult, error) {
	text, err := t.client.CallTool(ctx, t.mcpTool.Name, params)
	if err != nil {
		return tool.ToolResult{Content: fmt.Sprintf("MCP tool error: %s", err)}, nil
	}
	return tool.ToolResult{Content: text}, nil
}
