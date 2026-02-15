package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
)

// StdioTransport communicates with an MCP server via a subprocess's stdin/stdout.
type StdioTransport struct {
	command string
	args    []string
	env     map[string]string

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	mu       sync.Mutex
	nextID   atomic.Int64
	pending  map[int64]chan []byte
	done     chan struct{}
	closeErr error
}

// NewStdioTransport creates a transport that spawns a subprocess.
func NewStdioTransport(command string, args []string, env map[string]string) *StdioTransport {
	return &StdioTransport{
		command: command,
		args:    args,
		env:     env,
		pending: make(map[int64]chan []byte),
		done:    make(chan struct{}),
	}
}

func (t *StdioTransport) Start(ctx context.Context) error {
	t.cmd = exec.CommandContext(ctx, t.command, t.args...)
	t.cmd.Stderr = os.Stderr

	// Inherit current env, then overlay configured env vars.
	t.cmd.Env = os.Environ()
	for k, v := range t.env {
		t.cmd.Env = append(t.cmd.Env, k+"="+v)
	}

	var err error
	t.stdin, err = t.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("mcp stdio: stdin pipe: %w", err)
	}
	t.stdout, err = t.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("mcp stdio: stdout pipe: %w", err)
	}

	if err := t.cmd.Start(); err != nil {
		return fmt.Errorf("mcp stdio: start %s: %w", t.command, err)
	}

	go t.readLoop()
	return nil
}

// readLoop reads newline-delimited JSON-RPC messages from stdout and dispatches
// responses to waiting callers by request ID.
func (t *StdioTransport) readLoop() {
	defer close(t.done)
	scanner := bufio.NewScanner(t.stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // up to 10MB per line

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Extract the "id" field to route the response.
		var envelope struct {
			ID *int64 `json:"id"`
		}
		if err := json.Unmarshal(line, &envelope); err != nil || envelope.ID == nil {
			// Notification from server or parse error — ignore.
			continue
		}

		t.mu.Lock()
		ch, ok := t.pending[*envelope.ID]
		if ok {
			delete(t.pending, *envelope.ID)
		}
		t.mu.Unlock()

		if ok {
			// Make a copy since scanner reuses the buffer.
			msg := make([]byte, len(line))
			copy(msg, line)
			ch <- msg
		}
	}
}

func (t *StdioTransport) RoundTrip(ctx context.Context, request []byte) ([]byte, error) {
	// Extract request ID.
	var envelope struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(request, &envelope); err != nil {
		return nil, fmt.Errorf("mcp stdio: parse request id: %w", err)
	}

	ch := make(chan []byte, 1)
	t.mu.Lock()
	t.pending[envelope.ID] = ch
	t.mu.Unlock()

	// Write request + newline.
	t.mu.Lock()
	_, err := t.stdin.Write(append(request, '\n'))
	t.mu.Unlock()
	if err != nil {
		t.mu.Lock()
		delete(t.pending, envelope.ID)
		t.mu.Unlock()
		return nil, fmt.Errorf("mcp stdio: write: %w", err)
	}

	select {
	case resp := <-ch:
		return resp, nil
	case <-t.done:
		return nil, fmt.Errorf("mcp stdio: transport closed")
	case <-ctx.Done():
		t.mu.Lock()
		delete(t.pending, envelope.ID)
		t.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (t *StdioTransport) Notify(ctx context.Context, notification []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, err := t.stdin.Write(append(notification, '\n'))
	if err != nil {
		return fmt.Errorf("mcp stdio: notify: %w", err)
	}
	return nil
}

func (t *StdioTransport) Close() error {
	if t.stdin != nil {
		t.stdin.Close()
	}
	if t.cmd != nil && t.cmd.Process != nil {
		t.cmd.Process.Signal(os.Interrupt)
		t.closeErr = t.cmd.Wait()
	}
	return nil
}
