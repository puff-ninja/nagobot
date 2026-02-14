package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/joebot/nagobot/internal/bus"
)

// MessageTool sends messages to users on chat channels.
type MessageTool struct {
	sendFunc       func(msg *bus.OutboundMessage)
	defaultChannel string
	defaultChatID  string
}

// NewMessageTool creates a new message tool.
func NewMessageTool(sendFunc func(msg *bus.OutboundMessage)) *MessageTool {
	return &MessageTool{sendFunc: sendFunc}
}

// SetContext sets the current channel/chat context for this tool.
func (t *MessageTool) SetContext(channel, chatID string) {
	t.defaultChannel = channel
	t.defaultChatID = chatID
}

func (t *MessageTool) Name() string        { return "message" }
func (t *MessageTool) Description() string {
	return "Send a message to the user. Supports file attachments via the files parameter."
}
func (t *MessageTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content": map[string]any{
				"type":        "string",
				"description": "The message content to send",
			},
			"files": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional: list of absolute file paths to attach (images, documents, etc.)",
			},
			"channel": map[string]any{
				"type":        "string",
				"description": "Optional: target channel (discord, etc.)",
			},
			"chat_id": map[string]any{
				"type":        "string",
				"description": "Optional: target chat/user ID",
			},
		},
		"required": []string{"content"},
	}
}

func (t *MessageTool) Execute(_ context.Context, params map[string]any) (ToolResult, error) {
	content, err := requireStringParam(params, "content")
	if err != nil {
		return ToolResult{}, err
	}
	channel := getStringParam(params, "channel")
	chatID := getStringParam(params, "chat_id")
	if channel == "" {
		channel = t.defaultChannel
	}
	if chatID == "" {
		chatID = t.defaultChatID
	}
	if channel == "" || chatID == "" {
		return ToolResult{Content: "Error: No target channel/chat specified"}, nil
	}
	if t.sendFunc == nil {
		return ToolResult{Content: "Error: Message sending not configured"}, nil
	}

	media := parseStringList(params, "files")

	t.sendFunc(&bus.OutboundMessage{
		Channel: channel,
		ChatID:  chatID,
		Content: content,
		Media:   media,
	})
	return ToolResult{Content: fmt.Sprintf("Message sent to %s:%s (files: %d)", channel, chatID, len(media))}, nil
}

// parseStringList extracts a []string from a param that may be a []any, a
// []string, or a JSON-encoded string (LLMs sometimes stringify arrays).
func parseStringList(params map[string]any, key string) []string {
	v, ok := params[key]
	if !ok || v == nil {
		return nil
	}

	switch val := v.(type) {
	case []any:
		out := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return val
	case string:
		// LLM may pass a JSON-encoded array as a string
		var out []string
		if json.Unmarshal([]byte(val), &out) == nil {
			return out
		}
		if val != "" {
			return []string{val}
		}
	}
	return nil
}
