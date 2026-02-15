package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// HTTPTransport communicates with an MCP server via Streamable HTTP (with SSE fallback).
type HTTPTransport struct {
	url     string
	headers map[string]string
	client  *http.Client

	mu        sync.Mutex
	sessionID string // Mcp-Session-Id for session continuity
}

// NewHTTPTransport creates a transport that connects to a remote MCP server over HTTP.
func NewHTTPTransport(url string, headers map[string]string) *HTTPTransport {
	return &HTTPTransport{
		url:     url,
		headers: headers,
		client:  &http.Client{},
	}
}

func (t *HTTPTransport) Start(ctx context.Context) error {
	return nil
}

func (t *HTTPTransport) RoundTrip(ctx context.Context, request []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(request))
	if err != nil {
		return nil, fmt.Errorf("mcp http: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	t.mu.Lock()
	if t.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", t.sessionID)
	}
	t.mu.Unlock()

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mcp http: request: %w", err)
	}
	defer resp.Body.Close()

	// Capture session ID from response.
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.mu.Lock()
		t.sessionID = sid
		t.mu.Unlock()
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("mcp http: status %d: %s", resp.StatusCode, string(body))
	}

	ct := resp.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "text/event-stream") {
		return t.readSSEResponse(resp.Body, request)
	}

	// Plain JSON response.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("mcp http: read response: %w", err)
	}
	return body, nil
}

// readSSEResponse reads an SSE stream and returns the JSON-RPC response that
// matches the request ID.
func (t *HTTPTransport) readSSEResponse(r io.Reader, request []byte) ([]byte, error) {
	var reqEnv struct {
		ID int64 `json:"id"`
	}
	json.Unmarshal(request, &reqEnv)

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "" {
			continue
		}

		// Check if this is our response.
		var env struct {
			ID *int64 `json:"id"`
		}
		if err := json.Unmarshal([]byte(data), &env); err != nil {
			continue
		}
		if env.ID != nil && *env.ID == reqEnv.ID {
			return []byte(data), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("mcp http: read sse: %w", err)
	}
	return nil, fmt.Errorf("mcp http: sse stream ended without response")
}

func (t *HTTPTransport) Notify(ctx context.Context, notification []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(notification))
	if err != nil {
		return fmt.Errorf("mcp http: create notification: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	t.mu.Lock()
	if t.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", t.sessionID)
	}
	t.mu.Unlock()

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("mcp http: notify: %w", err)
	}
	resp.Body.Close()
	return nil
}

func (t *HTTPTransport) Close() error {
	return nil
}
