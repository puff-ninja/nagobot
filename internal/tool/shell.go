package tool

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// ShellTool executes shell commands.
type ShellTool struct {
	WorkingDir          string
	Timeout             int // seconds
	RestrictToWorkspace bool
	denyPatterns        []*regexp.Regexp
}

// NewShellTool creates a new shell execution tool.
func NewShellTool(workingDir string, timeout int, restrict bool) *ShellTool {
	patterns := []string{
		`\brm\s+-[rf]{1,2}\b`,
		`\bdel\s+/[fq]\b`,
		`\brmdir\s+/s\b`,
		`\b(format|mkfs|diskpart)\b`,
		`\bdd\s+if=`,
		`>\s*/dev/sd`,
		`\b(shutdown|reboot|poweroff)\b`,
		`:\(\)\s*\{.*\};\s*:`,
	}
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		if r, err := regexp.Compile(p); err == nil {
			compiled = append(compiled, r)
		}
	}
	if timeout <= 0 {
		timeout = 60
	}
	return &ShellTool{
		WorkingDir:          workingDir,
		Timeout:             timeout,
		RestrictToWorkspace: restrict,
		denyPatterns:        compiled,
	}
}

func (t *ShellTool) Name() string        { return "exec" }
func (t *ShellTool) Description() string { return "Execute a shell command and return its output." }
func (t *ShellTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The shell command to execute",
			},
			"working_dir": map[string]any{
				"type":        "string",
				"description": "Optional working directory for the command",
			},
		},
		"required": []string{"command"},
	}
}

func (t *ShellTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	command, err := requireStringParam(params, "command")
	if err != nil {
		return "", err
	}
	cwd := getStringParam(params, "working_dir")
	if cwd == "" {
		cwd = t.WorkingDir
	}

	if msg := t.guardCommand(command, cwd); msg != "" {
		return msg, nil
	}

	timeout := time.Duration(t.Timeout) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = cwd

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	var parts []string
	if stdout.Len() > 0 {
		parts = append(parts, stdout.String())
	}
	if stderr.Len() > 0 {
		s := strings.TrimSpace(stderr.String())
		if s != "" {
			parts = append(parts, "STDERR:\n"+s)
		}
	}

	if err != nil {
		if ctx.Err() != nil {
			return fmt.Sprintf("Error: Command timed out after %d seconds", t.Timeout), nil
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			parts = append(parts, fmt.Sprintf("\nExit code: %d", exitErr.ExitCode()))
		}
	}

	result := "(no output)"
	if len(parts) > 0 {
		result = strings.Join(parts, "\n")
	}

	return truncateString(result, 10000), nil
}

func (t *ShellTool) guardCommand(command, cwd string) string {
	lower := strings.ToLower(strings.TrimSpace(command))

	for _, p := range t.denyPatterns {
		if p.MatchString(lower) {
			return "Error: Command blocked by safety guard (dangerous pattern detected)"
		}
	}

	if t.RestrictToWorkspace {
		if strings.Contains(command, "../") || strings.Contains(command, `..\\`) {
			return "Error: Command blocked by safety guard (path traversal detected)"
		}
	}

	return ""
}
