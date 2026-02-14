package llm

import "context"

// ToolCallRequest represents a tool call from the LLM.
type ToolCallRequest struct {
	ID        string
	Name      string
	Arguments map[string]any
}

// ChatResponse is the response from an LLM chat completion.
type ChatResponse struct {
	Content          string
	ToolCalls        []ToolCallRequest
	FinishReason     string
	Usage            map[string]int
	ReasoningContent string
}

// HasToolCalls returns true if the response contains tool calls.
func (r *ChatResponse) HasToolCalls() bool {
	return len(r.ToolCalls) > 0
}

// ChatRequest holds parameters for an LLM chat request.
type ChatRequest struct {
	Messages    []map[string]any
	Tools       []map[string]any
	Model       string
	MaxTokens   int
	Temperature float64
}

// Provider is the interface for LLM providers.
type Provider interface {
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	DefaultModel() string
}
