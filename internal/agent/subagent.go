package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/joebot/nagobot/internal/bus"
	"github.com/joebot/nagobot/internal/llm"
	"github.com/joebot/nagobot/internal/tool"
)

// SubagentManager manages background subagent execution.
// Subagents are lightweight agent instances that run in goroutines
// with isolated tool sets (no message/spawn tools to prevent recursion).
type SubagentManager struct {
	provider            llm.Provider
	workspace           string
	model               string
	bus                 *bus.MessageBus
	execTimeout         int
	restrictToWorkspace bool

	mu    sync.Mutex
	tasks map[string]context.CancelFunc
}

// NewSubagentManager creates a new subagent manager.
func NewSubagentManager(
	provider llm.Provider,
	workspace string,
	model string,
	msgBus *bus.MessageBus,
	execTimeout int,
	restrictToWorkspace bool,
) *SubagentManager {
	return &SubagentManager{
		provider:            provider,
		workspace:           workspace,
		model:               model,
		bus:                 msgBus,
		execTimeout:         execTimeout,
		restrictToWorkspace: restrictToWorkspace,
		tasks:               make(map[string]context.CancelFunc),
	}
}

// Spawn starts a subagent in the background to execute a task.
// Returns a status message immediately.
func (m *SubagentManager) Spawn(
	ctx context.Context,
	task string,
	label string,
	originChannel string,
	originChatID string,
) string {
	taskID := fmt.Sprintf("%x", time.Now().UnixNano()%0xFFFFFFFF)[:8]
	if label == "" {
		label = task
		if len(label) > 30 {
			label = label[:30] + "..."
		}
	}

	subCtx, cancel := context.WithCancel(ctx)

	m.mu.Lock()
	m.tasks[taskID] = cancel
	m.mu.Unlock()

	go m.run(subCtx, taskID, task, label, originChannel, originChatID)

	slog.Info("Spawned subagent", "id", taskID, "label", label)
	return fmt.Sprintf("Subagent [%s] started (id: %s). I'll notify you when it completes.", label, taskID)
}

// RunningCount returns the number of active subagents.
func (m *SubagentManager) RunningCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.tasks)
}

func (m *SubagentManager) run(
	ctx context.Context,
	taskID, task, label, originChannel, originChatID string,
) {
	defer func() {
		m.mu.Lock()
		delete(m.tasks, taskID)
		m.mu.Unlock()
	}()

	slog.Info("Subagent starting", "id", taskID, "label", label)

	result, status := m.executeTask(ctx, taskID, task)

	m.announceResult(taskID, label, task, result, originChannel, originChatID, status)
}

func (m *SubagentManager) executeTask(ctx context.Context, taskID, task string) (string, string) {
	// Build isolated tool registry (no message, no spawn)
	tools := tool.NewRegistry()
	allowedDir := ""
	if m.restrictToWorkspace {
		allowedDir = m.workspace
	}
	tools.Register(&tool.ReadFileTool{AllowedDir: allowedDir, EmbedFS: builtinSkillsFS})
	tools.Register(&tool.WriteFileTool{AllowedDir: allowedDir})
	tools.Register(&tool.EditFileTool{AllowedDir: allowedDir})
	tools.Register(&tool.ListDirTool{AllowedDir: allowedDir})
	tools.Register(tool.NewShellTool(m.workspace, m.execTimeout, m.restrictToWorkspace))

	systemPrompt := m.buildPrompt()
	messages := []map[string]any{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": task},
	}

	maxIterations := 15
	for i := 0; i < maxIterations; i++ {
		resp, err := chatWithRetry(ctx, m.provider, llm.ChatRequest{
			Messages: messages,
			Tools:    tools.Definitions(),
			Model:    m.model,
		})
		if err != nil {
			return fmt.Sprintf("Error: %s", err), "error"
		}

		if !resp.HasToolCalls() {
			if resp.Content == "" {
				return "Task completed but no final response was generated.", "ok"
			}
			return resp.Content, "ok"
		}

		// Build assistant message with tool calls
		toolCallDicts := make([]map[string]any, len(resp.ToolCalls))
		for j, tc := range resp.ToolCalls {
			argsJSON, _ := json.Marshal(tc.Arguments)
			toolCallDicts[j] = map[string]any{
				"id":   tc.ID,
				"type": "function",
				"function": map[string]any{
					"name":      tc.Name,
					"arguments": string(argsJSON),
				},
			}
		}

		msg := map[string]any{"role": "assistant", "content": resp.Content}
		if len(toolCallDicts) > 0 {
			msg["tool_calls"] = toolCallDicts
		}
		messages = append(messages, msg)

		// Execute tools
		for _, tc := range resp.ToolCalls {
			argsJSON, _ := json.Marshal(tc.Arguments)
			slog.Debug("Subagent tool call", "id", taskID, "tool", tc.Name, "args", truncate(string(argsJSON), 200))
			result := tools.Execute(ctx, tc.Name, tc.Arguments)
			messages = append(messages, map[string]any{
				"role":         "tool",
				"tool_call_id": tc.ID,
				"name":         tc.Name,
				"content":      result.Content,
			})
		}
	}

	return "Task completed (max iterations reached).", "ok"
}

func (m *SubagentManager) announceResult(
	taskID, label, task, result, originChannel, originChatID, status string,
) {
	statusText := "completed successfully"
	if status != "ok" {
		statusText = "failed"
	}

	content := fmt.Sprintf(`[Subagent '%s' %s]

Task: %s

Result:
%s

Summarize this naturally for the user. Keep it brief (1-2 sentences). Do not mention technical details like "subagent" or task IDs.`,
		label, statusText, task, result)

	// Inject as system message to trigger main agent
	m.bus.PublishInbound(&bus.InboundMessage{
		Channel:  "system",
		SenderID: "subagent",
		ChatID:   fmt.Sprintf("%s:%s", originChannel, originChatID),
		Content:  content,
	})

	slog.Info("Subagent announced result", "id", taskID, "status", status)
}

func (m *SubagentManager) buildPrompt() string {
	now := time.Now().Format("2006-01-02 15:04 (Monday)")
	tz := time.Now().Format("MST")

	return fmt.Sprintf(`# Subagent

## Current Time
%s (%s)

You are a subagent spawned by the main agent to complete a specific task.

## Rules
1. Stay focused — complete only the assigned task, nothing else
2. Your final response will be reported back to the main agent
3. Do not initiate conversations or take on side tasks
4. Be concise but informative in your findings

## What You Can Do
- Read and write files in the workspace
- Execute shell commands
- Complete the task thoroughly

## What You Cannot Do
- Send messages directly to users (no message tool)
- Spawn other subagents
- Access the main agent's conversation history

## Workspace
%s
Skills: %s/skills/ (read SKILL.md files as needed)

When you have completed the task, provide a clear summary of your findings or actions.`,
		now, tz, m.workspace, m.workspace)
}

// processSystemMessage is called by the main loop to handle subagent completion announcements.
// It runs the LLM on the announce content in the context of the origin session
// and returns a user-facing response.
func ProcessSystemMessage(
	ctx context.Context,
	provider llm.Provider,
	model string,
	contextBuilder *ContextBuilder,
	tools *tool.Registry,
	chatID string,
	content string,
	maxIterations int,
) (originChannel, originChatID, response string) {
	// Parse origin from chat_id (format: "channel:chat_id")
	originChannel = "cli"
	originChatID = chatID
	if i := strings.Index(chatID, ":"); i >= 0 {
		originChannel = chatID[:i]
		originChatID = chatID[i+1:]
	}

	messages := contextBuilder.BuildMessages(nil, content, originChannel, originChatID)

	var finalContent string
	for i := 0; i < maxIterations; i++ {
		resp, err := chatWithRetry(ctx, provider, llm.ChatRequest{
			Messages: messages,
			Tools:    tools.Definitions(),
			Model:    model,
		})
		if err != nil {
			return originChannel, originChatID, fmt.Sprintf("Error processing subagent result: %s", err)
		}

		if !resp.HasToolCalls() {
			finalContent = resp.Content
			break
		}

		toolCallDicts := make([]map[string]any, len(resp.ToolCalls))
		for j, tc := range resp.ToolCalls {
			argsJSON, _ := json.Marshal(tc.Arguments)
			toolCallDicts[j] = map[string]any{
				"id":   tc.ID,
				"type": "function",
				"function": map[string]any{
					"name":      tc.Name,
					"arguments": string(argsJSON),
				},
			}
		}
		msg := map[string]any{"role": "assistant", "content": resp.Content}
		if len(toolCallDicts) > 0 {
			msg["tool_calls"] = toolCallDicts
		}
		messages = append(messages, msg)

		for _, tc := range resp.ToolCalls {
			result := tools.Execute(ctx, tc.Name, tc.Arguments)
			messages = append(messages, map[string]any{
				"role":         "tool",
				"tool_call_id": tc.ID,
				"name":         tc.Name,
				"content":      result.Content,
			})
		}
	}

	if finalContent == "" {
		finalContent = "Background task completed."
	}
	return originChannel, originChatID, finalContent
}
