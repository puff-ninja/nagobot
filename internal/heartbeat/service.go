package heartbeat

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultInterval is the default heartbeat interval (30 minutes).
	DefaultInterval = 30 * time.Minute

	// heartbeatPrompt is sent to the agent during each tick.
	heartbeatPrompt = `Read HEARTBEAT.md in your workspace (if it exists).
Follow any instructions or tasks listed there.
If nothing needs attention, reply with just: HEARTBEAT_OK`

	// heartbeatOKToken indicates the agent found nothing to do.
	heartbeatOKToken = "HEARTBEAT_OK"
)

// OnHeartbeat is the callback invoked on each tick.
// It receives the prompt and a session key, and returns the agent's response.
type OnHeartbeat func(ctx context.Context, prompt, sessionKey string) (string, error)

// Service is a periodic heartbeat that wakes the agent to check for tasks.
type Service struct {
	workspace   string
	interval    time.Duration
	onHeartbeat OnHeartbeat
}

// NewService creates a new heartbeat service.
func NewService(workspace string, interval time.Duration, cb OnHeartbeat) *Service {
	if interval <= 0 {
		interval = DefaultInterval
	}
	return &Service{
		workspace:   workspace,
		interval:    interval,
		onHeartbeat: cb,
	}
}

// Run starts the heartbeat loop. It blocks until ctx is cancelled.
func (s *Service) Run(ctx context.Context) {
	slog.Info("Heartbeat started", "interval", s.interval)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("Heartbeat stopped")
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Service) tick(ctx context.Context) {
	content := s.readHeartbeatFile()
	if isHeartbeatEmpty(content) {
		slog.Debug("Heartbeat: no tasks (HEARTBEAT.md empty or absent)")
		return
	}

	slog.Info("Heartbeat: checking for tasks...")

	if s.onHeartbeat == nil {
		return
	}

	resp, err := s.onHeartbeat(ctx, heartbeatPrompt, "heartbeat:system")
	if err != nil {
		slog.Error("Heartbeat execution failed", "err", err)
		return
	}

	normalized := strings.ToUpper(strings.ReplaceAll(resp, "_", ""))
	if strings.Contains(normalized, strings.ReplaceAll(heartbeatOKToken, "_", "")) {
		slog.Info("Heartbeat: OK (no action needed)")
	} else {
		slog.Info("Heartbeat: completed task")
	}
}

func (s *Service) readHeartbeatFile() string {
	path := filepath.Join(s.workspace, "HEARTBEAT.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// isHeartbeatEmpty returns true if content has no actionable lines.
func isHeartbeatEmpty(content string) bool {
	if content == "" {
		return true
	}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "<!--") {
			continue
		}
		// Skip empty checkbox patterns
		if line == "- [ ]" || line == "* [ ]" || line == "- [x]" || line == "* [x]" {
			continue
		}
		return false // found actionable content
	}
	return true
}
