package tool

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func resolvePath(path string, allowedDir string) (string, error) {
	expanded := path
	if strings.HasPrefix(expanded, "~/") {
		home, _ := os.UserHomeDir()
		expanded = filepath.Join(home, expanded[2:])
	}
	resolved, err := filepath.Abs(expanded)
	if err != nil {
		return "", err
	}
	if allowedDir != "" {
		absAllowed, _ := filepath.Abs(allowedDir)
		if !strings.HasPrefix(resolved, absAllowed) {
			return "", fmt.Errorf("path %s is outside allowed directory %s", path, allowedDir)
		}
	}
	return resolved, nil
}

// ReadFileTool reads file contents.
type ReadFileTool struct {
	AllowedDir string
	EmbedFS    fs.FS // optional fallback for embedded files (e.g. builtin skills)
}

func (t *ReadFileTool) Name() string        { return "read_file" }
func (t *ReadFileTool) Description() string { return "Read the contents of a file at the given path." }
func (t *ReadFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The file path to read",
			},
		},
		"required": []string{"path"},
	}
}

func (t *ReadFileTool) Execute(_ context.Context, params map[string]any) (ToolResult, error) {
	path, err := requireStringParam(params, "path")
	if err != nil {
		return ToolResult{}, err
	}
	resolved, err := resolvePath(path, t.AllowedDir)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("Error: %s", err)}, nil
	}
	info, err := os.Stat(resolved)
	if err != nil {
		// Try embedded FS fallback for paths like "builtin_skills/..."
		if t.EmbedFS != nil {
			if data, embedErr := fs.ReadFile(t.EmbedFS, path); embedErr == nil {
				return ToolResult{Content: string(data)}, nil
			}
		}
		return ToolResult{Content: fmt.Sprintf("Error: File not found: %s", path)}, nil
	}
	if info.IsDir() {
		return ToolResult{Content: fmt.Sprintf("Error: Not a file: %s", path)}, nil
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("Error reading file: %s", err)}, nil
	}
	return ToolResult{Content: string(data)}, nil
}

// WriteFileTool writes content to a file.
type WriteFileTool struct {
	AllowedDir string
}

func (t *WriteFileTool) Name() string        { return "write_file" }
func (t *WriteFileTool) Description() string { return "Write content to a file. Creates parent directories if needed." }
func (t *WriteFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The file path to write to",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "The content to write",
			},
		},
		"required": []string{"path", "content"},
	}
}

func (t *WriteFileTool) Execute(_ context.Context, params map[string]any) (ToolResult, error) {
	path, err := requireStringParam(params, "path")
	if err != nil {
		return ToolResult{}, err
	}
	content := getStringParam(params, "content")
	resolved, err := resolvePath(path, t.AllowedDir)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("Error: %s", err)}, nil
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return ToolResult{Content: fmt.Sprintf("Error creating directories: %s", err)}, nil
	}
	if err := os.WriteFile(resolved, []byte(content), 0o644); err != nil {
		return ToolResult{Content: fmt.Sprintf("Error writing file: %s", err)}, nil
	}
	return ToolResult{Content: fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), path)}, nil
}

// EditFileTool edits a file by replacing text.
type EditFileTool struct {
	AllowedDir string
}

func (t *EditFileTool) Name() string { return "edit_file" }
func (t *EditFileTool) Description() string {
	return "Edit a file by replacing old_text with new_text. The old_text must exist exactly in the file."
}
func (t *EditFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The file path to edit",
			},
			"old_text": map[string]any{
				"type":        "string",
				"description": "The exact text to find and replace",
			},
			"new_text": map[string]any{
				"type":        "string",
				"description": "The text to replace with",
			},
		},
		"required": []string{"path", "old_text", "new_text"},
	}
}

func (t *EditFileTool) Execute(_ context.Context, params map[string]any) (ToolResult, error) {
	path, err := requireStringParam(params, "path")
	if err != nil {
		return ToolResult{}, err
	}
	oldText, err := requireStringParam(params, "old_text")
	if err != nil {
		return ToolResult{}, err
	}
	newText := getStringParam(params, "new_text")

	resolved, err := resolvePath(path, t.AllowedDir)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("Error: %s", err)}, nil
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("Error: File not found: %s", path)}, nil
	}
	content := string(data)
	if !strings.Contains(content, oldText) {
		return ToolResult{Content: "Error: old_text not found in file. Make sure it matches exactly."}, nil
	}
	count := strings.Count(content, oldText)
	if count > 1 {
		return ToolResult{Content: fmt.Sprintf("Warning: old_text appears %d times. Please provide more context to make it unique.", count)}, nil
	}
	newContent := strings.Replace(content, oldText, newText, 1)
	if err := os.WriteFile(resolved, []byte(newContent), 0o644); err != nil {
		return ToolResult{Content: fmt.Sprintf("Error writing file: %s", err)}, nil
	}
	return ToolResult{Content: fmt.Sprintf("Successfully edited %s", path)}, nil
}

// ListDirTool lists directory contents.
type ListDirTool struct {
	AllowedDir string
}

func (t *ListDirTool) Name() string        { return "list_dir" }
func (t *ListDirTool) Description() string { return "List the contents of a directory." }
func (t *ListDirTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The directory path to list",
			},
		},
		"required": []string{"path"},
	}
}

func (t *ListDirTool) Execute(_ context.Context, params map[string]any) (ToolResult, error) {
	path, err := requireStringParam(params, "path")
	if err != nil {
		return ToolResult{}, err
	}
	resolved, err := resolvePath(path, t.AllowedDir)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("Error: %s", err)}, nil
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("Error: Directory not found: %s", path)}, nil
	}
	if !info.IsDir() {
		return ToolResult{Content: fmt.Sprintf("Error: Not a directory: %s", path)}, nil
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("Error listing directory: %s", err)}, nil
	}
	if len(entries) == 0 {
		return ToolResult{Content: fmt.Sprintf("Directory %s is empty", path)}, nil
	}

	// Sort entries
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	var lines []string
	for _, e := range entries {
		prefix := "[file] "
		if e.IsDir() {
			prefix = "[dir]  "
		}
		lines = append(lines, prefix+e.Name())
	}
	return ToolResult{Content: strings.Join(lines, "\n")}, nil
}
