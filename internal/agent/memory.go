package agent

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MemoryStore manages file-based agent memory.
type MemoryStore struct {
	workspace string
	memoryDir string
}

// NewMemoryStore creates a new memory store for the given workspace.
func NewMemoryStore(workspace string) *MemoryStore {
	dir := filepath.Join(workspace, "memory")
	os.MkdirAll(dir, 0o755)
	return &MemoryStore{
		workspace: workspace,
		memoryDir: dir,
	}
}

// ReadLongTerm reads MEMORY.md.
func (m *MemoryStore) ReadLongTerm() string {
	data, err := os.ReadFile(filepath.Join(m.memoryDir, "MEMORY.md"))
	if err != nil {
		return ""
	}
	return string(data)
}

// ReadToday reads today's notes file.
func (m *MemoryStore) ReadToday() string {
	path := filepath.Join(m.memoryDir, todayDate()+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// GetMemoryContext returns formatted memory context for the system prompt.
func (m *MemoryStore) GetMemoryContext() string {
	var parts []string

	longTerm := m.ReadLongTerm()
	if longTerm != "" {
		parts = append(parts, "## Long-term Memory\n"+longTerm)
	}

	today := m.ReadToday()
	if today != "" {
		parts = append(parts, "## Today's Notes\n"+today)
	}

	return strings.Join(parts, "\n\n")
}

func todayDate() string {
	return time.Now().Format("2006-01-02")
}
