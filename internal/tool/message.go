package tool

import (
	"context"
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
func (t *MessageTool) Description() string { return "Send a message to the user." }
func (t *MessageTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content": map[string]any{
				"type":        "string",
				"description": "The message content to send",
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

func (t *MessageTool) Execute(_ context.Context, params map[string]any) (string, error) {
	content, err := requireStringParam(params, "content")
	if err != nil {
		return "", err
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
		return "Error: No target channel/chat specified", nil
	}
	if t.sendFunc == nil {
		return "Error: Message sending not configured", nil
	}
	t.sendFunc(&bus.OutboundMessage{
		Channel: channel,
		ChatID:  chatID,
		Content: content,
	})
	return fmt.Sprintf("Message sent to %s:%s", channel, chatID), nil
}
