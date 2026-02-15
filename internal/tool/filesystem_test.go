package tool

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestExtractEmbedPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"builtin_skills/weather/SKILL.md", "builtin_skills/weather/SKILL.md"},
		{"/Users/joe/.nagobot/workspace/builtin_skills/weather/SKILL.md", "builtin_skills/weather/SKILL.md"},
		{"/home/user/builtin_skills/github/SKILL.md", "builtin_skills/github/SKILL.md"},
		{"/Users/joe/.nagobot/workspace/skills/weather/SKILL.md", ""},
		{"some/other/file.txt", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := extractEmbedPath(tt.input)
		if got != tt.want {
			t.Errorf("extractEmbedPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestReadFileTool_EmbedFSFallback(t *testing.T) {
	// Create a mock embedded FS
	mockFS := fstest.MapFS{
		"builtin_skills/weather/SKILL.md": &fstest.MapFile{
			Data: []byte("# Weather Skill\nThis is the weather skill content."),
		},
	}

	tool := &ReadFileTool{
		AllowedDir: "",
		EmbedFS:    mockFS,
	}

	ctx := context.Background()

	// Test 1: Direct embed path
	result, err := tool.Execute(ctx, map[string]any{"path": "builtin_skills/weather/SKILL.md"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "# Weather Skill\nThis is the weather skill content." {
		t.Errorf("Test 1 (direct path): got %q", result.Content)
	}

	// Test 2: Workspace-prefixed path should also find embedded file
	result, err = tool.Execute(ctx, map[string]any{"path": "/Users/joe/.nagobot/workspace/builtin_skills/weather/SKILL.md"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "# Weather Skill\nThis is the weather skill content." {
		t.Errorf("Test 2 (prefixed path): got %q", result.Content)
	}

	// Test 3: Non-embed path that doesn't exist returns error
	result, err = tool.Execute(ctx, map[string]any{"path": "/nonexistent/file.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content == "" || result.Content == "# Weather Skill\nThis is the weather skill content." {
		t.Errorf("Test 3 (nonexistent): should return error message, got %q", result.Content)
	}
}

func TestReadFileTool_EmbedWithRestrictToWorkspace(t *testing.T) {
	// Create a temp workspace directory
	tmpDir := t.TempDir()

	mockFS := fstest.MapFS{
		"builtin_skills/weather/SKILL.md": &fstest.MapFile{
			Data: []byte("# Weather"),
		},
	}

	tool := &ReadFileTool{
		AllowedDir: tmpDir,
		EmbedFS:    mockFS,
	}

	ctx := context.Background()

	// Embedded path should still work even with RestrictToWorkspace
	result, err := tool.Execute(ctx, map[string]any{"path": "builtin_skills/weather/SKILL.md"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "# Weather" {
		t.Errorf("embed with RestrictToWorkspace: got %q", result.Content)
	}

	// Workspace-prefixed embed path should also work
	result, err = tool.Execute(ctx, map[string]any{"path": filepath.Join(tmpDir, "builtin_skills/weather/SKILL.md")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "# Weather" {
		t.Errorf("prefixed embed with RestrictToWorkspace: got %q", result.Content)
	}

	// Regular file within workspace should still work
	testFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(testFile, []byte("hello"), 0644)
	result, err = tool.Execute(ctx, map[string]any{"path": testFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "hello" {
		t.Errorf("regular file: got %q", result.Content)
	}
}
