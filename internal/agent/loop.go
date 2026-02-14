package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/joebot/nagobot/internal/bus"
	"github.com/joebot/nagobot/internal/llm"
	"github.com/joebot/nagobot/internal/session"
	"github.com/joebot/nagobot/internal/tool"
)

// Loop is the core agent processing engine.
// It receives messages, builds context, calls the LLM, executes tools, and sends responses.
type Loop struct {
	bus           *bus.MessageBus
	provider      llm.Provider
	workspace     string
	model         string
	maxIterations int
	memoryWindow  int

	context   *ContextBuilder
	sessions  *session.Manager
	tools     *tool.Registry
	subagents *SubagentManager
}

// LoopConfig holds configuration for creating an agent loop.
type LoopConfig struct {
	Bus                 *bus.MessageBus
	Provider            llm.Provider
	Workspace           string
	Model               string
	MaxIterations       int
	MemoryWindow        int
	ExecTimeout         int
	RestrictToWorkspace bool
}

// NewLoop creates a new agent loop.
func NewLoop(cfg LoopConfig) *Loop {
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 20
	}
	if cfg.MemoryWindow <= 0 {
		cfg.MemoryWindow = 50
	}
	model := cfg.Model
	if model == "" {
		model = cfg.Provider.DefaultModel()
	}

	l := &Loop{
		bus:           cfg.Bus,
		provider:      cfg.Provider,
		workspace:     cfg.Workspace,
		model:         model,
		maxIterations: cfg.MaxIterations,
		memoryWindow:  cfg.MemoryWindow,
		context:       NewContextBuilder(cfg.Workspace),
		sessions:      session.NewManager(),
		tools:         tool.NewRegistry(),
		subagents: NewSubagentManager(
			cfg.Provider, cfg.Workspace, model, cfg.Bus,
			cfg.ExecTimeout, cfg.RestrictToWorkspace,
		),
	}

	l.registerDefaultTools(cfg)
	return l
}

func (l *Loop) registerDefaultTools(cfg LoopConfig) {
	allowedDir := ""
	if cfg.RestrictToWorkspace {
		allowedDir = cfg.Workspace
	}
	l.tools.Register(&tool.ReadFileTool{AllowedDir: allowedDir})
	l.tools.Register(&tool.WriteFileTool{AllowedDir: allowedDir})
	l.tools.Register(&tool.EditFileTool{AllowedDir: allowedDir})
	l.tools.Register(&tool.ListDirTool{AllowedDir: allowedDir})
	l.tools.Register(tool.NewShellTool(cfg.Workspace, cfg.ExecTimeout, cfg.RestrictToWorkspace))
	l.tools.Register(tool.NewMessageTool(cfg.Bus.PublishOutbound))
	l.tools.Register(tool.NewSpawnTool(l.subagents.Spawn))
}

// Run starts the agent loop, processing messages from the bus.
// Blocks until ctx is cancelled.
func (l *Loop) Run(ctx context.Context) {
	slog.Info("Agent loop started")
	for {
		select {
		case <-ctx.Done():
			slog.Info("Agent loop stopping")
			return
		case msg := <-l.bus.Inbound:
			resp, err := l.processMessage(ctx, msg)
			if err != nil {
				slog.Error("processing message", "err", err)
				l.bus.PublishOutbound(&bus.OutboundMessage{
					Channel: msg.Channel,
					ChatID:  msg.ChatID,
					Content: fmt.Sprintf("Sorry, I encountered an error: %s", err),
				})
				continue
			}
			if resp != nil {
				l.bus.PublishOutbound(resp)
			}
		}
	}
}

// ProcessDirect processes a message directly (for CLI usage).
func (l *Loop) ProcessDirect(ctx context.Context, content, sessionKey string) (string, error) {
	msg := &bus.InboundMessage{
		Channel:  "cli",
		SenderID: "user",
		ChatID:   "direct",
		Content:  content,
	}
	if sessionKey != "" {
		// Parse channel:chatID from session key
		if i := strings.Index(sessionKey, ":"); i >= 0 {
			msg.Channel = sessionKey[:i]
			msg.ChatID = sessionKey[i+1:]
		}
	}

	resp, err := l.processMessage(ctx, msg)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", nil
	}
	return resp.Content, nil
}

func (l *Loop) processMessage(ctx context.Context, msg *bus.InboundMessage) (*bus.OutboundMessage, error) {
	// Handle system messages (subagent completion announcements)
	if msg.Channel == "system" {
		originChannel, originChatID, response := ProcessSystemMessage(
			ctx, l.provider, l.model, l.context, l.tools,
			msg.ChatID, msg.Content, l.maxIterations,
		)
		return &bus.OutboundMessage{
			Channel: originChannel,
			ChatID:  originChatID,
			Content: response,
		}, nil
	}

	preview := msg.Content
	if len(preview) > 80 {
		preview = preview[:80] + "..."
	}
	slog.Info("Processing message", "channel", msg.Channel, "sender", msg.SenderID, "preview", preview)

	// Get or create session
	sess := l.sessions.GetOrCreate(msg.SessionKey())

	// Handle slash commands
	cmd := strings.TrimSpace(strings.ToLower(msg.Content))
	switch cmd {
	case "/new":
		l.consolidateMemory(ctx, sess, true)
		sess.Clear()
		l.sessions.Save(sess)
		return &bus.OutboundMessage{
			Channel: msg.Channel,
			ChatID:  msg.ChatID,
			Content: "New session started. Memory consolidated.",
		}, nil
	case "/help":
		return &bus.OutboundMessage{
			Channel: msg.Channel,
			ChatID:  msg.ChatID,
			Content: "nagobot commands:\n/new — Start a new conversation\n/help — Show available commands",
		}, nil
	}

	// Consolidate memory if session is too large
	if len(sess.Messages) > l.memoryWindow {
		l.consolidateMemory(ctx, sess, false)
	}

	// Set message tool context
	if mt, ok := l.tools.Get("message").(*tool.MessageTool); ok {
		mt.SetContext(msg.Channel, msg.ChatID)
	}

	// Set spawn tool context
	if st, ok := l.tools.Get("spawn").(*tool.SpawnTool); ok {
		st.SetContext(msg.Channel, msg.ChatID)
	}

	// Build initial messages
	messages := l.context.BuildMessages(
		sess.GetHistory(l.memoryWindow),
		msg.Content,
		msg.Channel,
		msg.ChatID,
	)

	// ReAct loop
	var finalContent string
	var toolsUsed []string
	for i := 0; i < l.maxIterations; i++ {
		resp, err := l.provider.Chat(ctx, llm.ChatRequest{
			Messages: messages,
			Tools:    l.tools.Definitions(),
			Model:    l.model,
		})
		if err != nil {
			return nil, fmt.Errorf("LLM call: %w", err)
		}

		if resp.HasToolCalls() {
			// Build tool call dicts for the assistant message
			toolCallDicts := make([]map[string]any, len(resp.ToolCalls))
			for i, tc := range resp.ToolCalls {
				argsJSON, _ := json.Marshal(tc.Arguments)
				toolCallDicts[i] = map[string]any{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]any{
						"name":      tc.Name,
						"arguments": string(argsJSON),
					},
				}
			}

			messages = l.context.AddAssistantMessage(messages, resp.Content, toolCallDicts, resp.ReasoningContent)

			// Execute tools sequentially
			for _, tc := range resp.ToolCalls {
				toolsUsed = append(toolsUsed, tc.Name)
				argsJSON, _ := json.Marshal(tc.Arguments)
				slog.Info("Tool call", "tool", tc.Name, "args", truncate(string(argsJSON), 200))
				result := l.tools.Execute(ctx, tc.Name, tc.Arguments)
				messages = l.context.AddToolResult(messages, tc.ID, tc.Name, result)
			}

			// Interleaved CoT: reflect before next action
			messages = append(messages, map[string]any{
				"role":    "user",
				"content": "Reflect on the results and decide next steps.",
			})
		} else {
			finalContent = resp.Content
			break
		}
	}

	if finalContent == "" {
		finalContent = "I've completed processing but have no response to give."
	}

	// Log response preview
	slog.Info("Response", "channel", msg.Channel, "preview", truncate(finalContent, 120))

	// Save to session (only user/assistant, not tool intermediates)
	sess.AddMessage("user", msg.Content)
	sess.AddMessage("assistant", finalContent, toolsUsed...)
	l.sessions.Save(sess)

	return &bus.OutboundMessage{
		Channel:  msg.Channel,
		ChatID:   msg.ChatID,
		Content:  finalContent,
		Metadata: msg.Metadata,
	}, nil
}

// consolidateMemory uses the LLM to condense old session messages into
// MEMORY.md (long-term facts) and HISTORY.md (grep-searchable log),
// then trims the session.
func (l *Loop) consolidateMemory(ctx context.Context, sess *session.Session, archiveAll bool) {
	if len(sess.Messages) == 0 {
		return
	}

	memory := NewMemoryStore(l.workspace)

	var oldMessages []session.Message
	var keepCount int
	if archiveAll {
		oldMessages = sess.Messages
	} else {
		keepCount = l.memoryWindow / 2
		if keepCount < 2 {
			keepCount = 2
		}
		if keepCount > 10 {
			keepCount = 10
		}
		if len(sess.Messages) <= keepCount {
			return
		}
		oldMessages = sess.Messages[:len(sess.Messages)-keepCount]
	}

	if len(oldMessages) == 0 {
		return
	}

	slog.Info("Memory consolidation started",
		"total", len(sess.Messages),
		"archiving", len(oldMessages),
		"keeping", keepCount,
	)

	// Format messages for LLM
	var lines []string
	for _, m := range oldMessages {
		if m.Content == "" {
			continue
		}
		ts := m.Timestamp
		if len(ts) > 16 {
			ts = ts[:16]
		}
		toolInfo := ""
		if len(m.ToolsUsed) > 0 {
			toolInfo = fmt.Sprintf(" [tools: %s]", strings.Join(m.ToolsUsed, ", "))
		}
		lines = append(lines, fmt.Sprintf("[%s] %s%s: %s", ts, strings.ToUpper(m.Role), toolInfo, m.Content))
	}
	conversation := strings.Join(lines, "\n")
	currentMemory := memory.ReadLongTerm()

	prompt := fmt.Sprintf(`You are a memory consolidation agent. Process this conversation and return a JSON object with exactly two keys:

1. "history_entry": A paragraph (2-5 sentences) summarizing the key events/decisions/topics. Start with a timestamp like [YYYY-MM-DD HH:MM]. Include enough detail to be useful when found by grep search later.

2. "memory_update": The updated long-term memory content. Add any new facts: user location, preferences, personal info, habits, project context, technical decisions, tools/services used. If nothing new, return the existing content unchanged.

## Current Long-term Memory
%s

## Conversation to Process
%s

Respond with ONLY valid JSON, no markdown fences.`, orDefault(currentMemory, "(empty)"), conversation)

	resp, err := l.provider.Chat(ctx, llm.ChatRequest{
		Messages: []map[string]any{
			{"role": "system", "content": "You are a memory consolidation agent. Respond only with valid JSON."},
			{"role": "user", "content": prompt},
		},
		Model: l.model,
	})
	if err != nil {
		slog.Error("Memory consolidation LLM call failed", "err", err)
		return
	}

	text := strings.TrimSpace(resp.Content)
	// Strip markdown fences if present
	if strings.HasPrefix(text, "```") {
		if i := strings.Index(text, "\n"); i >= 0 {
			text = text[i+1:]
		}
		if i := strings.LastIndex(text, "```"); i >= 0 {
			text = text[:i]
		}
		text = strings.TrimSpace(text)
	}

	var result map[string]string
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		slog.Error("Memory consolidation parse failed", "err", err)
		return
	}

	if entry, ok := result["history_entry"]; ok && entry != "" {
		if err := memory.AppendHistory(entry); err != nil {
			slog.Error("Failed to append history", "err", err)
		}
	}
	if update, ok := result["memory_update"]; ok && update != "" && update != currentMemory {
		if err := memory.WriteLongTerm(update); err != nil {
			slog.Error("Failed to write memory", "err", err)
		}
	}

	if archiveAll {
		sess.Messages = nil
	} else {
		sess.Messages = sess.Messages[len(sess.Messages)-keepCount:]
	}
	l.sessions.Save(sess)
	slog.Info("Memory consolidation done", "remaining", len(sess.Messages))
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
