package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
)

// Client is a JSON-RPC 2.0 MCP client that works over any Transport.
type Client struct {
	name      string
	transport Transport
	nextID    atomic.Int64
}

// NewClient creates a new MCP client for the given server name.
func NewClient(name string, transport Transport) *Client {
	return &Client{
		name:      name,
		transport: transport,
	}
}

// --- JSON-RPC types ---

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      int64            `json:"id"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *jsonRPCError    `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// --- MCP protocol types ---

type initializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ClientInfo      clientInfo     `json:"clientInfo"`
}

type clientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// MCPTool is a tool definition returned by the MCP server.
type MCPTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type toolsListResult struct {
	Tools []MCPTool `json:"tools"`
}

// MCPContent is a content block returned by tools/call.
type MCPContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type toolCallResult struct {
	Content []MCPContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

// --- Client methods ---

// Initialize performs the MCP initialize handshake with the server.
func (c *Client) Initialize(ctx context.Context) error {
	if err := c.transport.Start(ctx); err != nil {
		return err
	}

	params := initializeParams{
		ProtocolVersion: "2024-11-05",
		Capabilities:    map[string]any{},
		ClientInfo: clientInfo{
			Name:    "nagobot",
			Version: "1.0.0",
		},
	}

	_, err := c.call(ctx, "initialize", params)
	if err != nil {
		return fmt.Errorf("mcp initialize %s: %w", c.name, err)
	}

	// Send initialized notification.
	return c.notify(ctx, "notifications/initialized", nil)
}

// ListTools calls tools/list and returns the available tools.
func (c *Client) ListTools(ctx context.Context) ([]MCPTool, error) {
	raw, err := c.call(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("mcp tools/list %s: %w", c.name, err)
	}

	var result toolsListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("mcp tools/list %s: parse: %w", c.name, err)
	}
	return result.Tools, nil
}

// CallTool invokes a tool on the MCP server.
func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (string, error) {
	raw, err := c.call(ctx, "tools/call", toolCallParams{
		Name:      name,
		Arguments: arguments,
	})
	if err != nil {
		return "", fmt.Errorf("mcp tools/call %s.%s: %w", c.name, name, err)
	}

	var result toolCallResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("mcp tools/call %s.%s: parse: %w", c.name, name, err)
	}

	// Concatenate text content blocks.
	var text string
	for _, block := range result.Content {
		if block.Type == "text" {
			if text != "" {
				text += "\n"
			}
			text += block.Text
		}
	}

	if result.IsError {
		return "", fmt.Errorf("mcp tool error: %s", text)
	}
	return text, nil
}

// Close shuts down the transport.
func (c *Client) Close() error {
	return c.transport.Close()
}

// Name returns the server name this client is connected to.
func (c *Client) Name() string {
	return c.name
}

// --- internal helpers ---

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	respData, err := c.transport.RoundTrip(ctx, data)
	if err != nil {
		return nil, err
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	return resp.Result, nil
}

func (c *Client) notify(ctx context.Context, method string, params any) error {
	n := jsonRPCNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	data, err := json.Marshal(n)
	if err != nil {
		return err
	}
	return c.transport.Notify(ctx, data)
}
