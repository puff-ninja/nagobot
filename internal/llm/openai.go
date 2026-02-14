package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// OpenAIProvider implements the Provider interface using the OpenAI-compatible API.
// Works with OpenRouter, Anthropic (via proxy), DeepSeek, vLLM, etc.
type OpenAIProvider struct {
	apiKey       string
	apiBase      string
	defaultModel string
	extraHeaders map[string]string
	client       *http.Client
}

// NewOpenAIProvider creates a new OpenAI-compatible provider.
func NewOpenAIProvider(apiKey, apiBase, defaultModel string, extraHeaders map[string]string) *OpenAIProvider {
	if apiBase == "" {
		apiBase = "https://openrouter.ai/api/v1"
	}
	if defaultModel == "" {
		defaultModel = "anthropic/claude-sonnet-4-5"
	}
	return &OpenAIProvider{
		apiKey:       apiKey,
		apiBase:      apiBase,
		defaultModel: defaultModel,
		extraHeaders: extraHeaders,
		client:       &http.Client{},
	}
}

func (p *OpenAIProvider) DefaultModel() string {
	return p.defaultModel
}

func (p *OpenAIProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}
	temp := req.Temperature
	if temp == 0 {
		temp = 0.7
	}

	body := map[string]any{
		"model":       model,
		"messages":    req.Messages,
		"max_tokens":  maxTokens,
		"temperature": temp,
	}

	if len(req.Tools) > 0 {
		body["tools"] = req.Tools
		body["tool_choice"] = "auto"
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := p.apiBase + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
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

	return parseResponse(respBody)
}

func parseResponse(data []byte) (*ChatResponse, error) {
	var raw struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
				ToolCalls        []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
		Error *struct {
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

	if len(raw.Choices) == 0 {
		return &ChatResponse{
			Content:      "Error: no choices in LLM response",
			FinishReason: "error",
		}, nil
	}

	choice := raw.Choices[0]
	result := &ChatResponse{
		Content:          choice.Message.Content,
		FinishReason:     choice.FinishReason,
		ReasoningContent: choice.Message.ReasoningContent,
		Usage: map[string]int{
			"prompt_tokens":     raw.Usage.PromptTokens,
			"completion_tokens": raw.Usage.CompletionTokens,
			"total_tokens":      raw.Usage.TotalTokens,
		},
	}

	for _, tc := range choice.Message.ToolCalls {
		var args map[string]any
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			args = map[string]any{"raw": tc.Function.Arguments}
		}
		result.ToolCalls = append(result.ToolCalls, ToolCallRequest{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}

	return result, nil
}
