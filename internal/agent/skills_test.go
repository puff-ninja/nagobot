package agent

import (
	"strings"
	"testing"
)

func TestSkillsLoader_ListBuiltinSkills(t *testing.T) {
	loader := NewSkillsLoader(t.TempDir())

	skills := loader.ListSkills()
	if len(skills) == 0 {
		t.Fatal("expected at least one builtin skill, got none")
	}

	var foundWeather bool
	for _, s := range skills {
		if s.Name == "weather" {
			foundWeather = true
			if s.Source != "builtin" {
				t.Errorf("weather skill source = %q, want \"builtin\"", s.Source)
			}
			if s.Path != "builtin_skills/weather/SKILL.md" {
				t.Errorf("weather skill path = %q, want \"builtin_skills/weather/SKILL.md\"", s.Path)
			}
		}
	}
	if !foundWeather {
		t.Errorf("weather skill not found in listed skills: %+v", skills)
	}
}

func TestSkillsLoader_LoadWeatherSkill(t *testing.T) {
	loader := NewSkillsLoader(t.TempDir())

	content := loader.LoadSkill("weather")
	if content == "" {
		t.Fatal("weather skill content is empty")
	}
	if !strings.Contains(content, "wttr.in") {
		t.Error("weather skill content should mention wttr.in")
	}
}

func TestSkillsLoader_BuildSkillsSummary(t *testing.T) {
	loader := NewSkillsLoader(t.TempDir())

	summary := loader.BuildSkillsSummary()
	if summary == "" {
		t.Fatal("skills summary is empty")
	}
	if !strings.Contains(summary, "<skill") {
		t.Error("summary should contain <skill> tags")
	}
	if !strings.Contains(summary, "weather") {
		t.Error("summary should list weather skill")
	}
}

func TestSkillsLoader_GetAvailableSkills(t *testing.T) {
	loader := NewSkillsLoader(t.TempDir())

	available := loader.GetAvailableSkills()
	// weather requires curl which should be available on macOS
	var hasWeather bool
	for _, name := range available {
		if name == "weather" {
			hasWeather = true
		}
	}
	if !hasWeather {
		t.Error("weather should be in available skills (curl is present)")
	}
}

func TestSkillsLoader_LoadSkillsForContext(t *testing.T) {
	loader := NewSkillsLoader(t.TempDir())

	content := loader.LoadSkillsForContext([]string{"weather"})
	if content == "" {
		t.Fatal("LoadSkillsForContext returned empty")
	}
	if !strings.Contains(content, "### Skill: weather") {
		t.Error("should contain skill header")
	}
	if !strings.Contains(content, "wttr.in") {
		t.Error("should contain skill body with wttr.in")
	}
	// Should NOT contain frontmatter
	if strings.Contains(content, "---\nname:") {
		t.Error("should strip frontmatter")
	}
}
