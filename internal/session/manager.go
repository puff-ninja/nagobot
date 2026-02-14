package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Message is a single message in a session.
type Message struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp,omitempty"`
}

// Session holds conversation history for a channel:chat_id pair.
type Session struct {
	Key       string
	Messages  []Message
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AddMessage appends a message to the session.
func (s *Session) AddMessage(role, content string) {
	s.Messages = append(s.Messages, Message{
		Role:      role,
		Content:   content,
		Timestamp: time.Now().Format(time.RFC3339),
	})
	s.UpdatedAt = time.Now()
}

// GetHistory returns the last maxMessages in LLM-friendly format.
func (s *Session) GetHistory(maxMessages int) []map[string]any {
	msgs := s.Messages
	if len(msgs) > maxMessages {
		msgs = msgs[len(msgs)-maxMessages:]
	}
	history := make([]map[string]any, len(msgs))
	for i, m := range msgs {
		history[i] = map[string]any{"role": m.Role, "content": m.Content}
	}
	return history
}

// Clear removes all messages.
func (s *Session) Clear() {
	s.Messages = nil
	s.UpdatedAt = time.Now()
}

// Manager manages conversation sessions with JSONL persistence.
type Manager struct {
	sessionsDir string
	cache       map[string]*Session
	mu          sync.Mutex
}

// NewManager creates a new session manager.
func NewManager() *Manager {
	dir := filepath.Join(homeDir(), ".nagobot", "sessions")
	os.MkdirAll(dir, 0o755)
	return &Manager{
		sessionsDir: dir,
		cache:       make(map[string]*Session),
	}
}

// GetOrCreate returns an existing session or creates a new one.
func (m *Manager) GetOrCreate(key string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.cache[key]; ok {
		return s
	}

	s := m.load(key)
	if s == nil {
		s = &Session{
			Key:       key,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
	}
	m.cache[key] = s
	return s
}

// Save persists a session to disk.
func (m *Manager) Save(s *Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cache[s.Key] = s
	path := m.sessionPath(s.Key)

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create session file: %w", err)
	}
	defer f.Close()

	// Metadata line
	meta := map[string]any{
		"_type":      "metadata",
		"created_at": s.CreatedAt.Format(time.RFC3339),
		"updated_at": s.UpdatedAt.Format(time.RFC3339),
	}
	metaJSON, _ := json.Marshal(meta)
	f.Write(metaJSON)
	f.WriteString("\n")

	// Message lines
	for _, msg := range s.Messages {
		line, _ := json.Marshal(msg)
		f.Write(line)
		f.WriteString("\n")
	}

	return nil
}

// Delete removes a session from cache and disk.
func (m *Manager) Delete(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.cache, key)
	path := m.sessionPath(key)
	if err := os.Remove(path); err != nil {
		return false
	}
	return true
}

// List returns info about all sessions, sorted by updated time (newest first).
func (m *Manager) List() []map[string]string {
	entries, err := os.ReadDir(m.sessionsDir)
	if err != nil {
		return nil
	}

	var sessions []map[string]string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(m.sessionsDir, e.Name())
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		if scanner.Scan() {
			var meta map[string]any
			if json.Unmarshal([]byte(scanner.Text()), &meta) == nil {
				if meta["_type"] == "metadata" {
					key := strings.TrimSuffix(e.Name(), ".jsonl")
					key = strings.ReplaceAll(key, "_", ":")
					sessions = append(sessions, map[string]string{
						"key":        key,
						"created_at": fmt.Sprint(meta["created_at"]),
						"updated_at": fmt.Sprint(meta["updated_at"]),
					})
				}
			}
		}
		f.Close()
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i]["updated_at"] > sessions[j]["updated_at"]
	})

	return sessions
}

func (m *Manager) load(key string) *Session {
	path := m.sessionPath(key)
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var messages []Message
	var createdAt time.Time

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		if raw["_type"] == "metadata" {
			if ts, ok := raw["created_at"].(string); ok {
				createdAt, _ = time.Parse(time.RFC3339, ts)
			}
		} else {
			var msg Message
			if json.Unmarshal([]byte(line), &msg) == nil {
				messages = append(messages, msg)
			}
		}
	}

	if createdAt.IsZero() {
		createdAt = time.Now()
	}

	return &Session{
		Key:       key,
		Messages:  messages,
		CreatedAt: createdAt,
		UpdatedAt: time.Now(),
	}
}

func (m *Manager) sessionPath(key string) string {
	safe := strings.ReplaceAll(key, ":", "_")
	return filepath.Join(m.sessionsDir, safeFilename(safe)+".jsonl")
}

func safeFilename(name string) string {
	unsafe := `<>:"/\|?*`
	for _, c := range unsafe {
		name = strings.ReplaceAll(name, string(c), "_")
	}
	return strings.TrimSpace(name)
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp"
	}
	return home
}
