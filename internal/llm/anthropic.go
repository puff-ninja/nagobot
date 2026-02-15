package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// AnthropicProvider implements the Provider interface using the Anthropic Messages API.
type AnthropicProvider struct {
	apiKey       string
	apiBase      string
	defaultModel string
	extraHeaders map[string]string
	client       *http.Client
}

// NewAnthropicProvider creates a new Anthropic-compatible provider.
func NewAnthropicProvider(apiKey, apiBase, defaultModel string, extraHeaders map[string]string) *AnthropicProvider {
	if apiBase == "" {
		apiBase = "https://api.anthropic.com"
	}
	if defaultModel == "" {
		defaultModel = "claude-sonnet-4-5-20250514"
	}
	return &AnthropicProvider{
		apiKey:       apiKey,
		apiBase:      apiBase,
		defaultModel: defaultModel,
		extraHeaders: extraHeaders,
		client:       &http.Client{},
	}
}

func (p *AnthropicProvider) DefaultModel() string {
	return p.defaultModel
}

func (p *AnthropicProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}

	// Extract system prompt and convert messages
	systemPrompt, messages := p.convertMessages(req.Messages)

	body := map[string]any{
		"model":      model,
		"max_tokens": maxTokens,
		"messages":   messages,
	}
	if systemPrompt != "" {
		body["system"] = systemPrompt
	}

	if len(req.Tools) > 0 {
		body["tools"] = p.convertTools(req.Tools)
		body["tool_choice"] = map[string]any{"type": "auto"}
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := p.apiBase + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	for k, v := range p.extraHeaders {
		httpReq.Header.Set(k, v)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return &ChatResponse{
			Content:      fmt.Sprintf("Error calling LLM (HTTP %d): %s", resp.StatusCode, string(respBody)),
			FinishReason: "error",
		}, nil
	}

	return p.parseResponse(respBody)
}

// convertMessages extracts the system prompt and converts OpenAI message format
// to Anthropic format.
func (p *AnthropicProvider) convertMessages(msgs []map[string]any) (string, []map[string]any) {
	var system string
	var result []map[string]any

	for _, msg := range msgs {
		role, _ := msg["role"].(string)

		switch role {
		case "system":
			content, _ := msg["content"].(string)
			if system != "" {
				system += "\n\n"
			}
			system += content

		case "assistant":
			converted := p.convertAssistantMessage(msg)
			result = append(result, converted)

		case "tool":
			// Anthropic expects tool results as user messages with tool_result content blocks.
			// Merge consecutive tool results into one user message.
			toolResult := map[string]any{
				"type":        "tool_result",
				"tool_use_id": msg["tool_call_id"],
				"content":     msg["content"],
			}
			// Try to merge into previous user message if it has tool_result blocks
			if len(result) > 0 {
				prev := result[len(result)-1]
				if prevRole, _ := prev["role"].(string); prevRole == "user" {
					if prevContent, ok := prev["content"].([]any); ok {
						prev["content"] = append(prevContent, toolResult)
						continue
					}
				}
			}
			result = append(result, map[string]any{
				"role":    "user",
				"content": []any{toolResult},
			})

		case "user":
			result = append(result, map[string]any{
				"role":    "user",
				"content": msg["content"],
			})
		}
	}

	// Anthropic requires messages to alternate user/assistant.
	// Merge consecutive same-role messages.
	result = mergeConsecutiveRoles(result)

	return system, result
}

// convertAssistantMessage converts an OpenAI assistant message (with optional tool_calls)
// to Anthropic format (content blocks).
func (p *AnthropicProvider) convertAssistantMessage(msg map[string]any) map[string]any {
	var contentBlocks []any

	// Add text content if present
	if content, _ := msg["content"].(string); content != "" {
		contentBlocks = append(contentBlocks, map[string]any{
			"type": "text",
			"text": content,
		})
	}

	// Convert tool_calls to tool_use content blocks
	if toolCalls, ok := msg["tool_calls"].([]map[string]any); ok {
		for _, tc := range toolCalls {
			fn, _ := tc["function"].(map[string]any)
			name, _ := fn["name"].(string)
			argsStr, _ := fn["arguments"].(string)

			var input map[string]any
			if err := json.Unmarshal([]byte(argsStr), &input); err != nil {
				input = map[string]any{"raw": argsStr}
			}

			contentBlocks = append(contentBlocks, map[string]any{
				"type":  "tool_use",
				"id":    tc["id"],
				"name":  name,
				"input": input,
			})
		}
	}

	// Also handle []any for tool_calls (from JSON unmarshalling)
	if toolCalls, ok := msg["tool_calls"].([]any); ok {
		for _, tcRaw := range toolCalls {
			tc, ok := tcRaw.(map[string]any)
			if !ok {
				continue
			}
			fn, _ := tc["function"].(map[string]any)
			name, _ := fn["name"].(string)
			argsStr, _ := fn["arguments"].(string)

			var input map[string]any
			if err := json.Unmarshal([]byte(argsStr), &input); err != nil {
				input = map[string]any{"raw": argsStr}
			}

			contentBlocks = append(contentBlocks, map[string]any{
				"type":  "tool_use",
				"id":    tc["id"],
				"name":  name,
				"input": input,
			})
		}
	}

	if len(contentBlocks) == 0 {
		contentBlocks = append(contentBlocks, map[string]any{
			"type": "text",
			"text": "",
		})
	}

	return map[string]any{
		"role":    "assistant",
		"content": contentBlocks,
	}
}

// convertTools converts OpenAI tool definitions to Anthropic format.
func (p *AnthropicProvider) convertTools(tools []map[string]any) []map[string]any {
	var result []map[string]any
	for _, t := range tools {
		fn, _ := t["function"].(map[string]any)
		if fn == nil {
			continue
		}
		result = append(result, map[string]any{
			"name":         fn["name"],
			"description":  fn["description"],
			"input_schema": fn["parameters"],
		})
	}
	return result
}

func (p *AnthropicProvider) parseResponse(data []byte) (*ChatResponse, error) {
	var raw struct {
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		Error *struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if raw.Error != nil {
		return &ChatResponse{
			Content:      "Error calling LLM: " + raw.Error.Message,
			FinishReason: "error",
		}, nil
	}

	result := &ChatResponse{
		FinishReason: raw.StopReason,
		Usage: map[string]int{
			"prompt_tokens":     raw.Usage.InputTokens,
			"completion_tokens": raw.Usage.OutputTokens,
			"total_tokens":      raw.Usage.InputTokens + raw.Usage.OutputTokens,
		},
	}

	var textParts []string
	for _, block := range raw.Content {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "tool_use":
			var args map[string]any
			if err := json.Unmarshal(block.Input, &args); err != nil {
				slog.Warn("failed to parse tool call arguments", "tool", block.Name, "input", string(block.Input), "err", err)
				args = map[string]any{"raw": string(block.Input)}
			} else if len(args) == 0 {
				slog.Warn("LLM returned empty arguments for tool call", "tool", block.Name)
			}
			result.ToolCalls = append(result.ToolCalls, ToolCallRequest{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: args,
			})
		}
	}
	result.Content = joinNonEmpty(textParts, "\n")

	// Map Anthropic stop_reason to OpenAI finish_reason
	switch result.FinishReason {
	case "end_turn":
		result.FinishReason = "stop"
	case "tool_use":
		result.FinishReason = "tool_calls"
	}

	return result, nil
}

// mergeConsecutiveRoles ensures messages alternate between user and assistant
// by merging consecutive same-role messages.
func mergeConsecutiveRoles(msgs []map[string]any) []map[string]any {
	if len(msgs) == 0 {
		return msgs
	}
	var result []map[string]any
	result = append(result, msgs[0])

	for i := 1; i < len(msgs); i++ {
		curr := msgs[i]
		prev := result[len(result)-1]
		currRole, _ := curr["role"].(string)
		prevRole, _ := prev["role"].(string)

		if currRole == prevRole {
			// Merge content
			prevContent := toContentBlocks(prev["content"])
			currContent := toContentBlocks(curr["content"])
			prev["content"] = append(prevContent, currContent...)
		} else {
			result = append(result, curr)
		}
	}
	return result
}

func toContentBlocks(v any) []any {
	switch val := v.(type) {
	case []any:
		return val
	case string:
		return []any{map[string]any{"type": "text", "text": val}}
	default:
		return []any{}
	}
}

func joinNonEmpty(parts []string, sep string) string {
	var non []string
	for _, p := range parts {
		if p != "" {
			non = append(non, p)
		}
	}
	if len(non) == 0 {
		return ""
	}
	result := non[0]
	for _, p := range non[1:] {
		result += sep + p
	}
	return result
}
