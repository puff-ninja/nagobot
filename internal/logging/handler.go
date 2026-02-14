package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
)

// ANSI color codes.
const (
	ansiReset  = "\033[0m"
	ansiRed    = "\033[31m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
	ansiGray   = "\033[90m"

	padding = "  " // left padding to align with TUI header
)

// Block attributes are rendered as indented blocks below the log line
// instead of inline key=value pairs.
var blockKeys = map[string]bool{
	"preview": true,
	"content": true,
}

// Options configures a Handler.
type Options struct {
	Level slog.Level
	Color bool
}

// Handler is a compact, optionally colored slog handler.
type Handler struct {
	w     io.Writer
	mu    *sync.Mutex
	level slog.Level
	color bool
	attrs []slog.Attr
}

// NewHandler creates a new log handler.
func NewHandler(w io.Writer, opts *Options) *Handler {
	if opts == nil {
		opts = &Options{}
	}
	return &Handler{
		w:     w,
		mu:    &sync.Mutex{},
		level: opts.Level,
		color: opts.Color,
	}
}

func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *Handler) Handle(_ context.Context, r slog.Record) error {
	// Timestamp: short for terminal, full for file.
	var ts string
	if h.color {
		ts = r.Time.Format("15:04:05")
	} else {
		ts = r.Time.Format("2006-01-02 15:04:05")
	}

	lvl := levelLabel(r.Level)

	// Separate inline attrs from block attrs (preview, content).
	var inline string
	var blocks []string
	for _, a := range h.attrs {
		if blockKeys[a.Key] {
			blocks = append(blocks, a.Value.String())
		} else {
			inline += h.fmtAttr(a)
		}
	}
	r.Attrs(func(a slog.Attr) bool {
		if blockKeys[a.Key] {
			blocks = append(blocks, a.Value.String())
		} else {
			inline += h.fmtAttr(a)
		}
		return true
	})

	// Build main line.
	var sb strings.Builder
	if h.color {
		sb.WriteString(fmt.Sprintf("%s%s%s%s %s %s%s\n",
			padding,
			ansiGray, ts, ansiReset,
			colorLevel(r.Level, lvl),
			r.Message, inline))
	} else {
		sb.WriteString(fmt.Sprintf("%s%s %s %s%s\n", padding, ts, lvl, r.Message, inline))
	}

	// Render block content below the log line.
	for _, text := range blocks {
		for _, line := range strings.Split(text, "\n") {
			if h.color {
				sb.WriteString(fmt.Sprintf("%s  %s│%s %s\n",
					padding, ansiGray, ansiReset, line))
			} else {
				sb.WriteString(fmt.Sprintf("%s  | %s\n", padding, line))
			}
		}
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.w.Write([]byte(sb.String()))
	return err
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	combined := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(combined, h.attrs)
	copy(combined[len(h.attrs):], attrs)
	return &Handler{w: h.w, mu: h.mu, level: h.level, color: h.color, attrs: combined}
}

func (h *Handler) WithGroup(string) slog.Handler {
	return h
}

func (h *Handler) fmtAttr(a slog.Attr) string {
	if h.color {
		return fmt.Sprintf(" %s%s%s=%s", ansiGray, a.Key, ansiReset, a.Value.String())
	}
	return fmt.Sprintf(" %s=%s", a.Key, a.Value.String())
}

func levelLabel(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return "ERR"
	case level >= slog.LevelWarn:
		return "WRN"
	case level >= slog.LevelInfo:
		return "INF"
	default:
		return "DBG"
	}
}

func colorLevel(level slog.Level, label string) string {
	switch {
	case level >= slog.LevelError:
		return ansiRed + label + ansiReset
	case level >= slog.LevelWarn:
		return ansiYellow + label + ansiReset
	case level >= slog.LevelInfo:
		return ansiCyan + label + ansiReset
	default:
		return ansiGray + label + ansiReset
	}
}
