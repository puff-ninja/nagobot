package cli

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/joebot/nagobot/internal/config"
)

// --- onboard selection model ---

type onboardChoice int

const (
	choiceUpgrade onboardChoice = iota
	choiceOverwrite
	choiceSkip
)

type onboardModel struct {
	choices []string
	cursor  int
	chosen  bool
	choice  onboardChoice
}

func (m onboardModel) Init() tea.Cmd { return nil }

func (m onboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			m.choice = choiceSkip
			m.chosen = true
			return m, tea.Quit
		case tea.KeyUp, tea.KeyShiftTab:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.KeyDown, tea.KeyTab:
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}
		case tea.KeyEnter:
			m.choice = onboardChoice(m.cursor)
			m.chosen = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m onboardModel) View() string {
	if m.chosen {
		return ""
	}

	s := "\n"
	s += fmt.Sprintf("  Config already exists at %s\n\n", DimStyle.Render(config.ConfigPath()))

	for i, choice := range m.choices {
		cursor := "  "
		if i == m.cursor {
			cursor = BotLabel.Render("❯ ")
		}
		s += "  " + cursor + choice + "\n"
	}

	s += "\n" + DimStyle.Render("  ↑/↓ navigate · enter select · ctrl+c cancel") + "\n"
	return s
}

// RunOnboard runs the onboard wizard.
func RunOnboard() {
	cfgPath := config.ConfigPath()
	var cfg *config.Config

	fmt.Println()
	fmt.Println(TitleStyle.Render(fmt.Sprintf("  %s nagobot Onboard", Logo)))

	if _, err := os.Stat(cfgPath); err == nil {
		// Config exists — ask what to do
		m := onboardModel{
			choices: []string{
				"Upgrade — add new fields, keep existing values",
				"Overwrite — replace with fresh defaults",
				"Skip — do not modify config",
			},
		}
		p := tea.NewProgram(m)
		final, err := p.Run()
		if err != nil {
			fmt.Println("  " + ErrStyle.Render("Error: "+err.Error()))
			os.Exit(1)
		}
		fm := final.(onboardModel)

		fmt.Println()
		switch fm.choice {
		case choiceUpgrade:
			upgraded, err := config.Upgrade()
			if err != nil {
				fmt.Println("  " + ErrStyle.Render("Error: "+err.Error()))
				os.Exit(1)
			}
			cfg = upgraded
			fmt.Println("  " + OkStyle.Render("✓") + " Upgraded config")
		case choiceOverwrite:
			cfg = config.DefaultConfig()
			if err := config.Save(cfg); err != nil {
				fmt.Println("  " + ErrStyle.Render("Error: "+err.Error()))
				os.Exit(1)
			}
			fmt.Println("  " + OkStyle.Render("✓") + " Overwritten config")
		default:
			fmt.Println("  " + DimStyle.Render("Config unchanged"))
			cfg, _ = config.Load()
		}
	} else {
		cfg = config.DefaultConfig()
		if err := config.Save(cfg); err != nil {
			fmt.Println("  " + ErrStyle.Render("Error: "+err.Error()))
			os.Exit(1)
		}
		fmt.Println()
		fmt.Println("  " + OkStyle.Render("✓") + " Created config at " + DimStyle.Render(cfgPath))
	}

	ws := cfg.WorkspacePath()
	os.MkdirAll(ws, 0o755)
	fmt.Println("  " + OkStyle.Render("✓") + " Workspace at " + DimStyle.Render(ws))

	createWorkspaceTemplates(ws)

	fmt.Println()
	fmt.Println(OkStyle.Render("  nagobot is ready!"))
	fmt.Println()
	fmt.Println(DimStyle.Render("  Next steps:"))
	fmt.Println(DimStyle.Render("  1. Add your API key to ~/.nagobot/config.json"))
	fmt.Println(DimStyle.Render("  2. Chat: nagobot agent -m \"Hello!\""))
	fmt.Println()
}

func createWorkspaceTemplates(workspace string) {
	templates := map[string]string{
		"AGENTS.md": `# Agent Instructions

You are a helpful AI assistant. Be concise, accurate, and friendly.

## Guidelines

- Always explain what you're doing before taking actions
- Ask for clarification when the request is ambiguous
- Use tools to help accomplish tasks
- Remember important information in your memory files
`,
		"SOUL.md": `# Soul

I am nagobot, a lightweight AI assistant.

## Personality

- Helpful and friendly
- Concise and to the point
- Curious and eager to learn
`,
		"USER.md": `# User

Information about the user goes here.

## Preferences

- Communication style: (casual/formal)
- Timezone: (your timezone)
- Language: (your preferred language)
`,
	}

	for filename, content := range templates {
		path := filepath.Join(workspace, filename)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			os.WriteFile(path, []byte(content), 0o644)
			fmt.Println("    " + DimStyle.Render("created "+filename))
		}
	}

	memDir := filepath.Join(workspace, "memory")
	os.MkdirAll(memDir, 0o755)
	memFile := filepath.Join(memDir, "MEMORY.md")
	if _, err := os.Stat(memFile); os.IsNotExist(err) {
		os.WriteFile(memFile, []byte(`# Long-term Memory

This file stores important information that should persist across sessions.
`), 0o644)
		fmt.Println("    " + DimStyle.Render("created memory/MEMORY.md"))
	}

	skillsDir := filepath.Join(workspace, "skills")
	os.MkdirAll(skillsDir, 0o755)
}
