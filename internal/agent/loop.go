package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/joebot/nagobot/internal/bus"
	"github.com/joebot/nagobot/internal/command"
	"github.com/joebot/nagobot/internal/llm"
	"github.com/joebot/nagobot/internal/session"
	"github.com/joebot/nagobot/internal/tool"
)

// slashHandler is the callback signature for a slash command.
type slashHandler func(ctx context.Context, sess *session.Session, msg *bus.InboundMessage) (*bus.OutboundMessage, error)

// Loop is the core agent processing engine.
// It receives messages, builds context, calls the LLM, executes tools, and sends responses.
type Loop struct {
	bus           *bus.MessageBus
	provider      llm.Provider
	workspace     string
	model         string
	maxIterations int
	memoryWindow  int
	contextLimit  int

	context   *ContextBuilder
	sessions  *session.Manager
	tools     *tool.Registry
	subagents *SubagentManager

	slashDefs  []command.Command
	slashIndex map[string]slashHandler
}

// LoopConfig holds configuration for creating an agent loop.
type LoopConfig struct {
	Bus                 *bus.MessageBus
	Provider            llm.Provider
	Workspace           string
	Model               string
	MaxIterations       int
	MemoryWindow        int
	ContextLimit        int
	ExecTimeout         int
	RestrictToWorkspace bool
	BraveAPIKey         string
}

// NewLoop creates a new agent loop.
func NewLoop(cfg LoopConfig) *Loop {
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 20
	}
	if cfg.MemoryWindow <= 0 {
		cfg.MemoryWindow = 50
	}
	if cfg.ContextLimit <= 0 {
		cfg.ContextLimit = 80000
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
		contextLimit:  cfg.ContextLimit,
		context:       NewContextBuilder(cfg.Workspace),
		sessions:      session.NewManager(),
		tools:         tool.NewRegistry(),
		subagents: NewSubagentManager(
			cfg.Provider, cfg.Workspace, model, cfg.Bus,
			cfg.ExecTimeout, cfg.RestrictToWorkspace,
		),
		slashIndex: make(map[string]slashHandler),
	}

	l.registerDefaultTools(cfg)
	l.registerSlashCommands()
	return l
}

func (l *Loop) registerDefaultTools(cfg LoopConfig) {
	allowedDir := ""
	if cfg.RestrictToWorkspace {
		allowedDir = cfg.Workspace
	}
	l.tools.Register(&tool.ReadFileTool{AllowedDir: allowedDir, EmbedFS: builtinSkillsFS})
	l.tools.Register(&tool.WriteFileTool{AllowedDir: allowedDir})
	l.tools.Register(&tool.EditFileTool{AllowedDir: allowedDir})
	l.tools.Register(&tool.ListDirTool{AllowedDir: allowedDir})
	l.tools.Register(tool.NewShellTool(cfg.Workspace, cfg.ExecTimeout, cfg.RestrictToWorkspace))
	l.tools.Register(tool.NewMessageTool(cfg.Bus.PublishOutbound))
	l.tools.Register(tool.NewSpawnTool(l.subagents.Spawn))
	if cfg.BraveAPIKey != "" {
		l.tools.Register(tool.NewWebSearchTool(cfg.BraveAPIKey))
	}
	l.tools.Register(tool.NewWebFetchTool())
}

func (l *Loop) registerCommand(name, description string, handler slashHandler) {
	l.slashDefs = append(l.slashDefs, command.Command{Name: name, Description: description})
	l.slashIndex[name] = handler
}

// Commands returns the registered slash command definitions.
func (l *Loop) Commands() []command.Command {
	return l.slashDefs
}

// ToolRegistry returns the tool registry for external tool registration.
func (l *Loop) ToolRegistry() *tool.Registry {
	return l.tools
}

func (l *Loop) registerSlashCommands() {
	l.registerCommand("new", "Start a new conversation", l.handleNew)
	l.registerCommand("compact", "Compress current context", l.handleCompact)
	l.registerCommand("context", "Show current context usage", l.handleContext)
	l.registerCommand("help", "Show available commands", l.handleHelp)
}

func (l *Loop) handleNew(ctx context.Context, sess *session.Session, msg *bus.InboundMessage) (*bus.OutboundMessage, error) {
	l.consolidateMemory(ctx, sess, true)
	sess.Clear()
	l.sessions.Save(sess)
	return &bus.OutboundMessage{
		Channel: msg.Channel,
		ChatID:  msg.ChatID,
		Content: "New session started. Memory consolidated.",
	}, nil
}

func (l *Loop) handleCompact(ctx context.Context, sess *session.Session, msg *bus.InboundMessage) (*bus.OutboundMessage, error) {
	history := sess.GetHistory(len(sess.Messages))
	if len(history) < 5 {
		return &bus.OutboundMessage{
			Channel: msg.Channel,
			ChatID:  msg.ChatID,
			Content: "Not enough context to compress.",
		}, nil
	}
	messages := make([]map[string]any, 0, len(history)+1)
	messages = append(messages, map[string]any{"role": "system", "content": l.context.BuildSystemPrompt()})
	messages = append(messages, history...)

	tokensBefore := estimateTokens(messages)
	compressed := l.compressMessages(ctx, messages)
	tokensAfter := estimateTokens(compressed)

	if tokensAfter >= tokensBefore {
		return &bus.OutboundMessage{
			Channel: msg.Channel,
			ChatID:  msg.ChatID,
			Content: "Context is already compact, no further compression possible.",
		}, nil
	}

	sess.Messages = nil
	for _, m := range compressed[1:] {
		role, _ := m["role"].(string)
		content, _ := m["content"].(string)
		sess.AddMessage(role, content)
	}
	l.sessions.Save(sess)

	return &bus.OutboundMessage{
		Channel: msg.Channel,
		ChatID:  msg.ChatID,
		Content: fmt.Sprintf("Context compressed. Tokens: %d → %d (%.0f%% reduction)",
			tokensBefore, tokensAfter,
			float64(tokensBefore-tokensAfter)/float64(tokensBefore)*100),
	}, nil
}

func (l *Loop) handleContext(_ context.Context, sess *session.Session, msg *bus.InboundMessage) (*bus.OutboundMessage, error) {
	history := sess.GetHistory(len(sess.Messages))
	messages := make([]map[string]any, 0, len(history)+1)
	messages = append(messages, map[string]any{"role": "system", "content": l.context.BuildSystemPrompt()})
	messages = append(messages, history...)
	tokens := estimateTokens(messages)
	usage := float64(tokens) / float64(l.contextLimit) * 100
	return &bus.OutboundMessage{
		Channel: msg.Channel,
		ChatID:  msg.ChatID,
		Content: fmt.Sprintf("Context: ~%d tokens (%.0f%% of %d limit), %d messages",
			tokens, usage, l.contextLimit, len(sess.Messages)),
	}, nil
}

func (l *Loop) handleHelp(_ context.Context, _ *session.Session, msg *bus.InboundMessage) (*bus.OutboundMessage, error) {
	var sb strings.Builder
	sb.WriteString("nagobot commands:\n")
	for _, cmd := range l.slashDefs {
		sb.WriteString(fmt.Sprintf("/%s — %s\n", cmd.Name, cmd.Description))
	}
	return &bus.OutboundMessage{
		Channel: msg.Channel,
		ChatID:  msg.ChatID,
		Content: strings.TrimRight(sb.String(), "\n"),
	}, nil
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
					Content: "Sorry, I ran into a technical issue while processing your message. Please try again, or start a new session with /new if the problem persists.",
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

// chatWithRetry wraps provider.Chat with automatic retries for transient errors
// (network issues, rate limits, overloaded models). Uses exponential backoff.
func chatWithRetry(ctx context.Context, provider llm.Provider, req llm.ChatRequest) (*llm.ChatResponse, error) {
	const maxRetries = 2
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt) * 2 * time.Second
			slog.Warn("LLM call failed, retrying", "attempt", attempt, "err", lastErr)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		resp, err := provider.Chat(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}
	return nil, lastErr
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
	cmdName := strings.TrimSpace(strings.ToLower(msg.Content))
	if strings.HasPrefix(cmdName, "/") {
		if handler, ok := l.slashIndex[cmdName[1:]]; ok {
			return handler(ctx, sess, msg)
		}
	}

	// Consolidate memory if session is too large
	if len(sess.Messages) > l.memoryWindow {
		emitProgress(msg, "Consolidating memory...")
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
	var mediaFiles []string
	for i := 0; i < l.maxIterations; i++ {
		// Compress context before each LLM call if approaching the limit.
		if estimateTokens(messages) > l.contextLimit {
			emitProgress(msg, "Compressing context...")
			messages = l.compressMessages(ctx, messages)
		}

		emitProgress(msg, "Thinking...")
		resp, err := chatWithRetry(ctx, l.provider, llm.ChatRequest{
			Messages: messages,
			Tools:    l.tools.Definitions(),
			Model:    l.model,
		})
		if err != nil {
			return nil, fmt.Errorf("LLM call: %w", err)
		}

		// Handle LLM error responses (e.g. context length exceeded).
		// Try compressing and retrying once before giving up.
		if resp.FinishReason == "error" {
			slog.Warn("LLM returned error", "content", truncate(resp.Content, 200))
			compressed := l.compressMessages(ctx, messages)
			tokensBefore := estimateTokens(messages)
			tokensAfter := estimateTokens(compressed)
			if tokensAfter < tokensBefore {
				messages = compressed
				slog.Info("Retrying after context compression",
					"tokens_before", tokensBefore,
					"tokens_after", tokensAfter)
				continue // retry this iteration
			}
			return nil, fmt.Errorf("LLM error: %s", truncate(resp.Content, 500))
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
				emitProgress(msg, fmt.Sprintf("Running tool: %s", tc.Name))
				argsJSON, _ := json.Marshal(tc.Arguments)
				slog.Info("Tool call", "tool", tc.Name, "args", truncate(string(argsJSON), 200))
				result := l.tools.Execute(ctx, tc.Name, tc.Arguments)
				if len(result.Media) > 0 {
					mediaFiles = append(mediaFiles, result.Media...)
				}
				messages = l.context.AddToolResult(messages, tc.ID, tc.Name, result.Content)
			}

			// Interleaved CoT: reflect before next action (only in multi-step chains)
			if i > 0 {
				messages = append(messages, map[string]any{
					"role":    "user",
					"content": "[SYSTEM] Review the tool results above. If you have enough information, respond directly to the user's original request. If not, make additional tool calls. Do NOT output any reflection or meta-commentary — just answer the user or call tools.",
				})
			}
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
		Media:    mediaFiles,
		Metadata: msg.Metadata,
	}, nil
}

// estimateTokens returns a rough token count for a message array.
// Uses JSON byte length / 3 as a heuristic (~3 bytes per token on average).
// estimateTokens returns a rough token count for a message array.
// Uses JSON byte length / 4 as a heuristic. This is intentionally
// conservative (overestimates token count) so that compression
// triggers before hitting the actual model limit.
func estimateTokens(messages []map[string]any) int {
	data, _ := json.Marshal(messages)
	return len(data) / 4
}

// compressMessages uses the LLM to summarize older messages when the context
// grows too large during a ReAct loop. It keeps the system prompt and recent
// messages, replacing everything in between with a concise summary.
func (l *Loop) compressMessages(ctx context.Context, messages []map[string]any) []map[string]any {
	if len(messages) < 6 {
		return messages
	}

	// Walk backwards to find a split point (a "user" message) that leaves
	// at least 4 messages in the tail.
	splitIdx := -1
	for i := len(messages) - 4; i > 1; i-- {
		if role, _ := messages[i]["role"].(string); role == "user" {
			splitIdx = i
			break
		}
	}
	if splitIdx <= 1 {
		return messages
	}

	systemMsg := messages[0]
	toSummarize := messages[1:splitIdx]
	tail := messages[splitIdx:]

	// Format the messages to summarize
	var sb strings.Builder
	for _, m := range toSummarize {
		role, _ := m["role"].(string)
		content, _ := m["content"].(string)
		if content == "" {
			continue
		}
		if len(content) > 2000 {
			content = content[:2000] + "... [truncated]"
		}
		sb.WriteString(fmt.Sprintf("[%s]: %s\n\n", strings.ToUpper(role), content))
	}

	prompt := fmt.Sprintf(`Summarize this conversation concisely. Preserve: key facts, user requests, decisions made, tool results, and important context needed to continue the conversation.

%s
Reply with ONLY the summary, no preamble.`, sb.String())

	resp, err := l.provider.Chat(ctx, llm.ChatRequest{
		Messages: []map[string]any{
			{"role": "system", "content": "You are a conversation summarizer. Create a concise summary preserving key information needed to continue the conversation."},
			{"role": "user", "content": prompt},
		},
		Model: l.model,
	})
	if err != nil {
		slog.Error("Context compression failed", "err", err)
		return messages
	}

	compressed := make([]map[string]any, 0, 3+len(tail))
	compressed = append(compressed, systemMsg)
	compressed = append(compressed, map[string]any{
		"role":    "user",
		"content": "[Earlier conversation summary]\n" + resp.Content,
	})
	compressed = append(compressed, map[string]any{
		"role":    "assistant",
		"content": "Understood. I have the context from the earlier conversation and will continue from here.",
	})
	compressed = append(compressed, tail...)

	slog.Info("Context compressed",
		"original_msgs", len(messages),
		"summarized", len(toSummarize),
		"compressed_msgs", len(compressed),
		"original_tokens", estimateTokens(messages),
		"compressed_tokens", estimateTokens(compressed),
	)

	return compressed
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

// emitProgress calls the inbound message's progress callback if set.
func emitProgress(msg *bus.InboundMessage, status string) {
	if msg.ProgressFunc != nil {
		msg.ProgressFunc(status)
	}
}
