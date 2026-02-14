package tool

import (
	"context"
	"fmt"
)

// SpawnFunc is the function signature for spawning a subagent.
type SpawnFunc func(ctx context.Context, task, label, channel, chatID string) string

// SpawnTool spawns a subagent to handle a task in the background.
type SpawnTool struct {
	spawnFunc      SpawnFunc
	defaultChannel string
	defaultChatID  string
}

// NewSpawnTool creates a new spawn tool.
func NewSpawnTool(spawnFunc SpawnFunc) *SpawnTool {
	return &SpawnTool{spawnFunc: spawnFunc}
}

// SetContext sets the origin channel/chat for subagent announcements.
func (t *SpawnTool) SetContext(channel, chatID string) {
	t.defaultChannel = channel
	t.defaultChatID = chatID
}

func (t *SpawnTool) Name() string { return "spawn" }
func (t *SpawnTool) Description() string {
	return "Spawn a subagent to handle a task in the background. " +
		"Use this for complex or time-consuming tasks that can run independently. " +
		"The subagent will complete the task and report back when done."
}

func (t *SpawnTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task": map[string]any{
				"type":        "string",
				"description": "The task for the subagent to complete",
			},
			"label": map[string]any{
				"type":        "string",
				"description": "Optional short label for the task (for display)",
			},
		},
		"required": []string{"task"},
	}
}

func (t *SpawnTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	task, err := requireStringParam(params, "task")
	if err != nil {
		return "", err
	}
	label := getStringParam(params, "label")

	channel := t.defaultChannel
	chatID := t.defaultChatID
	if channel == "" {
		channel = "cli"
	}
	if chatID == "" {
		chatID = "direct"
	}

	result := t.spawnFunc(ctx, task, label, channel, chatID)
	return fmt.Sprintf("%s", result), nil
}
