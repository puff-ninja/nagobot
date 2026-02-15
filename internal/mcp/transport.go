package mcp

import "context"

// Transport abstracts the communication layer between MCP client and server.
// Implementations handle stdio (subprocess) or HTTP (Streamable HTTP / SSE).
type Transport interface {
	// Start initializes the transport (e.g. spawns process or validates endpoint).
	Start(ctx context.Context) error
	// RoundTrip sends a JSON-RPC request and returns the matching response.
	RoundTrip(ctx context.Context, request []byte) ([]byte, error)
	// Notify sends a JSON-RPC notification (no response expected).
	Notify(ctx context.Context, notification []byte) error
	// Close shuts down the transport.
	Close() error
}
