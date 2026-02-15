package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ContextBuilder assembles the system prompt and message list for the LLM.
type ContextBuilder struct {
	workspace string
	memory    *MemoryStore
	skills    *SkillsLoader
}

var bootstrapFiles = []string{"AGENTS.md", "SOUL.md", "USER.md", "TOOLS.md", "IDENTITY.md"}

// NewContextBuilder creates a new context builder.
func NewContextBuilder(workspace string) *ContextBuilder {
	return &ContextBuilder{
		workspace: workspace,
		memory:    NewMemoryStore(workspace),
		skills:    NewSkillsLoader(workspace),
	}
}

// BuildSystemPrompt constructs the full system prompt.
func (c *ContextBuilder) BuildSystemPrompt() string {
	var parts []string

	parts = append(parts, c.getIdentity())

	if bootstrap := c.loadBootstrapFiles(); bootstrap != "" {
		parts = append(parts, bootstrap)
	}

	if mem := c.memory.GetMemoryContext(); mem != "" {
		parts = append(parts, "# Memory\n\n"+mem)
	}

	// Load all available skills directly into the system prompt.
	if availableSkills := c.skills.GetAvailableSkills(); len(availableSkills) > 0 {
		if content := c.skills.LoadSkillsForContext(availableSkills); content != "" {
			parts = append(parts, "# Skills\n\nThe following skills extend your capabilities. When a user's request matches a skill, you MUST follow the skill's instructions and use the tools it specifies (e.g. exec with curl). Do NOT substitute with other tools like web_fetch unless the skill's method fails.\n\n"+content)
		}
	}

	// Show summary for unavailable skills (missing dependencies).
	if summary := c.skills.BuildUnavailableSkillsSummary(); summary != "" {
		parts = append(parts, "# Unavailable Skills\n\nThese skills need dependencies installed before use.\n\n"+summary)
	}

	return strings.Join(parts, "\n\n---\n\n")
}

// BuildMessages constructs the complete message list for an LLM call.
func (c *ContextBuilder) BuildMessages(
	history []map[string]any,
	currentMessage string,
	channel string,
	chatID string,
) []map[string]any {
	var messages []map[string]any

	systemPrompt := c.BuildSystemPrompt()
	if channel != "" && chatID != "" {
		systemPrompt += fmt.Sprintf("\n\n## Current Session\nChannel: %s\nChat ID: %s", channel, chatID)
	}
	messages = append(messages, map[string]any{"role": "system", "content": systemPrompt})

	messages = append(messages, history...)

	messages = append(messages, map[string]any{"role": "user", "content": currentMessage})

	return messages
}

// AddToolResult appends a tool result message.
func (c *ContextBuilder) AddToolResult(messages []map[string]any, toolCallID, toolName, result string) []map[string]any {
	return append(messages, map[string]any{
		"role":         "tool",
		"tool_call_id": toolCallID,
		"name":         toolName,
		"content":      result,
	})
}

// AddAssistantMessage appends an assistant message (optionally with tool calls).
func (c *ContextBuilder) AddAssistantMessage(messages []map[string]any, content string, toolCalls []map[string]any, reasoningContent string) []map[string]any {
	msg := map[string]any{"role": "assistant", "content": content}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
	}
	if reasoningContent != "" {
		msg["reasoning_content"] = reasoningContent
	}
	return append(messages, msg)
}

func (c *ContextBuilder) getIdentity() string {
	now := time.Now().Format("2006-01-02 15:04 (Monday)")
	tz := time.Now().Format("MST")
	ws, _ := filepath.Abs(c.workspace)
	osName := runtime.GOOS
	if osName == "darwin" {
		osName = "macOS"
	}
	rt := fmt.Sprintf("%s %s, Go %s", osName, runtime.GOARCH, runtime.Version())

	return fmt.Sprintf(`# nagobot

You are nagobot, a helpful AI assistant. You have access to tools that allow you to:
- Read, write, and edit files
- Execute shell commands
- Send messages to users on chat channels

## Current Time
%s (%s)

## Runtime
%s

## Workspace
Your workspace is at: %s
- Long-term memory: %s/memory/MEMORY.md
- History log: %s/memory/HISTORY.md (grep-searchable)
- Custom skills: %s/skills/{skill-name}/SKILL.md

IMPORTANT: When responding to direct questions or conversations, reply directly with your text response.
Only use the 'message' tool when you need to send a message to a specific chat channel.
For normal conversation, just respond with text - do not call the message tool.
EXCEPTION: To send files (images, documents, audio) to the user, you MUST use the 'message' tool with the 'files' parameter containing absolute file paths. Your text response alone cannot deliver files — always call the 'message' tool for file delivery.

Always be helpful, accurate, and concise. When using tools, think step by step.
When remembering something important, write to %s/memory/MEMORY.md
To recall past events, grep %s/memory/HISTORY.md`, now, tz, rt, ws, ws, ws, ws, ws, ws)
}

func (c *ContextBuilder) loadBootstrapFiles() string {
	var parts []string
	for _, filename := range bootstrapFiles {
		path := filepath.Join(c.workspace, filename)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("## %s\n\n%s", filename, string(data)))
	}
	return strings.Join(parts, "\n\n")
}
