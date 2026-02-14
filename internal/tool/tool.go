package tool

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// ToolResult holds the output of a tool execution.
type ToolResult struct {
	Content string   // textual result for the LLM
	Media   []string // file paths to attach to the outbound message
}

// Tool is the interface for agent tools.
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any // JSON Schema
	Execute(ctx context.Context, params map[string]any) (ToolResult, error)
}

// Registry manages tool registration and execution.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry creates a new tool registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool to the registry.
func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
}

// Get returns a tool by name, or nil if not found.
func (r *Registry) Get(name string) Tool {
	return r.tools[name]
}

// Definitions returns all tools in OpenAI function schema format.
func (r *Registry) Definitions() []map[string]any {
	defs := make([]map[string]any, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name(),
				"description": t.Description(),
				"parameters":  t.Parameters(),
			},
		})
	}
	return defs
}

// Execute runs a tool by name with the given parameters.
// Errors are returned as strings (error isolation — lets LLM decide recovery).
func (r *Registry) Execute(ctx context.Context, name string, params map[string]any) ToolResult {
	t := r.tools[name]
	if t == nil {
		return ToolResult{Content: fmt.Sprintf("Error: Tool '%s' not found", name)}
	}

	result, err := t.Execute(ctx, params)
	if err != nil {
		slog.Error("tool execution error", "tool", name, "err", err)
		return ToolResult{Content: fmt.Sprintf("Error executing %s: %s", name, err)}
	}
	return result
}

// Names returns all registered tool names.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	return names
}

// getStringParam extracts a string parameter, returning empty string if missing.
func getStringParam(params map[string]any, key string) string {
	v, ok := params[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}

// requireStringParam extracts a required string parameter.
func requireStringParam(params map[string]any, key string) (string, error) {
	s := getStringParam(params, key)
	if s == "" {
		return "", fmt.Errorf("missing required parameter: %s", key)
	}
	return s, nil
}

// truncateString truncates a string to maxLen, adding a suffix.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	remaining := len(s) - maxLen
	return s[:maxLen] + fmt.Sprintf("\n... (truncated, %d more chars)", remaining)
}

// safeFilename converts a string to a safe filename.
func safeFilename(name string) string {
	unsafe := `<>:"/\|?*`
	for _, c := range unsafe {
		name = strings.ReplaceAll(name, string(c), "_")
	}
	return strings.TrimSpace(name)
}
