package agent

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

//go:embed builtin_skills
var builtinSkillsFS embed.FS

// SkillInfo holds metadata about a discovered skill.
type SkillInfo struct {
	Name   string
	Path   string // absolute file path (workspace) or embed path (builtin)
	Source string // "workspace" or "builtin"
}

// SkillMeta holds parsed frontmatter from a SKILL.md file.
type SkillMeta struct {
	Name        string
	Description string
	Always      bool
	Metadata    string // raw JSON string
}

// nagobot-specific metadata parsed from the "metadata" JSON field.
type skillRequirements struct {
	Bins []string `json:"bins"`
	Env  []string `json:"env"`
}

type nagobotMeta struct {
	Requires skillRequirements `json:"requires"`
}

type metadataWrapper struct {
	Nagobot nagobotMeta `json:"nagobot"`
}

// SkillsLoader discovers and loads agent skills from workspace and builtin directories.
type SkillsLoader struct {
	workspace       string
	workspaceSkills string
}

// NewSkillsLoader creates a new skills loader.
func NewSkillsLoader(workspace string) *SkillsLoader {
	return &SkillsLoader{
		workspace:       workspace,
		workspaceSkills: filepath.Join(workspace, "skills"),
	}
}

// ListSkills returns all discovered skills (workspace overrides builtin).
func (s *SkillsLoader) ListSkills() []SkillInfo {
	seen := make(map[string]bool)
	var skills []SkillInfo

	// Workspace skills (highest priority)
	if entries, err := os.ReadDir(s.workspaceSkills); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			skillFile := filepath.Join(s.workspaceSkills, e.Name(), "SKILL.md")
			if _, err := os.Stat(skillFile); err == nil {
				skills = append(skills, SkillInfo{
					Name:   e.Name(),
					Path:   skillFile,
					Source: "workspace",
				})
				seen[e.Name()] = true
			}
		}
	}

	// Builtin skills
	entries, err := fs.ReadDir(builtinSkillsFS, "builtin_skills")
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() || seen[e.Name()] {
				continue
			}
			embedPath := "builtin_skills/" + e.Name() + "/SKILL.md"
			if _, err := builtinSkillsFS.ReadFile(embedPath); err == nil {
				skills = append(skills, SkillInfo{
					Name:   e.Name(),
					Path:   embedPath,
					Source: "builtin",
				})
			}
		}
	}

	return skills
}

// LoadSkill reads a skill's SKILL.md content by name.
func (s *SkillsLoader) LoadSkill(name string) string {
	// Check workspace first
	wsPath := filepath.Join(s.workspaceSkills, name, "SKILL.md")
	if data, err := os.ReadFile(wsPath); err == nil {
		return string(data)
	}

	// Check builtin
	embedPath := "builtin_skills/" + name + "/SKILL.md"
	if data, err := builtinSkillsFS.ReadFile(embedPath); err == nil {
		return string(data)
	}

	return ""
}

// GetAlwaysSkills returns names of skills marked always=true that have met requirements.
func (s *SkillsLoader) GetAlwaysSkills() []string {
	var result []string
	for _, skill := range s.ListSkills() {
		meta := s.parseSkillMeta(skill)
		if meta.Always && s.checkRequirements(meta) {
			result = append(result, skill.Name)
		}
	}
	return result
}

// LoadSkillsForContext loads and formats skills content for inclusion in the system prompt.
func (s *SkillsLoader) LoadSkillsForContext(names []string) string {
	var parts []string
	for _, name := range names {
		content := s.LoadSkill(name)
		if content == "" {
			continue
		}
		content = stripFrontmatter(content)
		parts = append(parts, fmt.Sprintf("### Skill: %s\n\n%s", name, content))
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// BuildSkillsSummary generates an XML-formatted summary of all skills for the system prompt.
func (s *SkillsLoader) BuildSkillsSummary() string {
	allSkills := s.ListSkills()
	if len(allSkills) == 0 {
		return ""
	}

	var lines []string
	lines = append(lines, "<skills>")
	for _, skill := range allSkills {
		meta := s.parseSkillMeta(skill)
		available := s.checkRequirements(meta)
		desc := meta.Description
		if desc == "" {
			desc = skill.Name
		}

		lines = append(lines, fmt.Sprintf("  <skill available=\"%v\">", available))
		lines = append(lines, fmt.Sprintf("    <name>%s</name>", escapeXML(skill.Name)))
		lines = append(lines, fmt.Sprintf("    <description>%s</description>", escapeXML(desc)))
		lines = append(lines, fmt.Sprintf("    <location>%s</location>", skill.Path))

		if !available {
			if missing := s.getMissingRequirements(meta); missing != "" {
				lines = append(lines, fmt.Sprintf("    <requires>%s</requires>", escapeXML(missing)))
			}
		}

		lines = append(lines, "  </skill>")
	}
	lines = append(lines, "</skills>")

	return strings.Join(lines, "\n")
}

// parseSkillMeta parses the YAML frontmatter from a skill's SKILL.md.
func (s *SkillsLoader) parseSkillMeta(skill SkillInfo) SkillMeta {
	content := s.LoadSkill(skill.Name)
	return parseFrontmatter(content)
}

// checkRequirements verifies that a skill's binary and env requirements are met.
func (s *SkillsLoader) checkRequirements(meta SkillMeta) bool {
	reqs := parseNagobotRequirements(meta.Metadata)
	for _, bin := range reqs.Bins {
		if _, err := exec.LookPath(bin); err != nil {
			return false
		}
	}
	for _, env := range reqs.Env {
		if os.Getenv(env) == "" {
			return false
		}
	}
	return true
}

// getMissingRequirements returns a description of unmet requirements.
func (s *SkillsLoader) getMissingRequirements(meta SkillMeta) string {
	reqs := parseNagobotRequirements(meta.Metadata)
	var missing []string
	for _, bin := range reqs.Bins {
		if _, err := exec.LookPath(bin); err != nil {
			missing = append(missing, "CLI: "+bin)
		}
	}
	for _, env := range reqs.Env {
		if os.Getenv(env) == "" {
			missing = append(missing, "ENV: "+env)
		}
	}
	return strings.Join(missing, ", ")
}

// --- helpers ---

var frontmatterRe = regexp.MustCompile(`(?s)\A---\n(.*?)\n---\n`)

// parseFrontmatter extracts metadata from YAML frontmatter.
// Uses simple key: value parsing (no full YAML library).
func parseFrontmatter(content string) SkillMeta {
	var meta SkillMeta
	match := frontmatterRe.FindStringSubmatch(content)
	if match == nil {
		return meta
	}

	for _, line := range strings.Split(match[1], "\n") {
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		// Strip surrounding quotes
		value = strings.Trim(value, "\"'")

		switch key {
		case "name":
			meta.Name = value
		case "description":
			meta.Description = value
		case "always":
			meta.Always = value == "true"
		case "metadata":
			// metadata value is JSON, might contain colons — rejoin
			meta.Metadata = strings.TrimSpace(line[idx+1:])
			meta.Metadata = strings.Trim(meta.Metadata, "\"'")
		}
	}
	return meta
}

// stripFrontmatter removes YAML frontmatter from content.
func stripFrontmatter(content string) string {
	loc := frontmatterRe.FindStringIndex(content)
	if loc == nil {
		return content
	}
	return strings.TrimSpace(content[loc[1]:])
}

// parseNagobotRequirements extracts requirements from the metadata JSON field.
func parseNagobotRequirements(metadataJSON string) skillRequirements {
	if metadataJSON == "" {
		return skillRequirements{}
	}
	var wrapper metadataWrapper
	if err := json.Unmarshal([]byte(metadataJSON), &wrapper); err != nil {
		return skillRequirements{}
	}
	return wrapper.Nagobot.Requires
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
