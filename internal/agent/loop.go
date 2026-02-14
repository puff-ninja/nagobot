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

	context  *ContextBuilder
	sessions *session.Manager
	tools    *tool.Registry
}

// LoopConfig holds configuration for creating an agent loop.
type LoopConfig struct {
	Bus                 *bus.MessageBus
	Provider            llm.Provider
	Workspace           string
	Model               string
	MaxIterations       int
	ExecTimeout         int
	RestrictToWorkspace bool
}

// NewLoop creates a new agent loop.
func NewLoop(cfg LoopConfig) *Loop {
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 20
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
		context:       NewContextBuilder(cfg.Workspace),
		sessions:      session.NewManager(),
		tools:         tool.NewRegistry(),
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
	preview := msg.Content
	if len(preview) > 80 {
		preview = preview[:80] + "..."
	}
	slog.Info("Processing message", "channel", msg.Channel, "sender", msg.SenderID, "preview", preview)

	// Get or create session
	sess := l.sessions.GetOrCreate(msg.SessionKey())

	// Set message tool context
	if mt, ok := l.tools.Get("message").(*tool.MessageTool); ok {
		mt.SetContext(msg.Channel, msg.ChatID)
	}

	// Build initial messages
	messages := l.context.BuildMessages(
		sess.GetHistory(50),
		msg.Content,
		msg.Channel,
		msg.ChatID,
	)

	// ReAct loop
	var finalContent string
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
	sess.AddMessage("assistant", finalContent)
	l.sessions.Save(sess)

	return &bus.OutboundMessage{
		Channel:  msg.Channel,
		ChatID:   msg.ChatID,
		Content:  finalContent,
		Metadata: msg.Metadata,
	}, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
