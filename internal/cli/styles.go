package cli

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const Logo = "◆"
const Version = "0.1.0"

var bannerLines = []string{
	`  _   _    _    ____  ___  ____   ___ _____ `,
	` | \ | |  / \  / ___|/ _ \| __ ) / _ \_   _|`,
	` |  \| | / _ \| |  _| | | |  _ \| | | || |  `,
	` | |\  |/ ___ \ |_| | |_| | |_) | |_| || |  `,
	` |_| \_/_/   \_\____|\___/|____/ \___/ |_|  `,
}

var bannerColors = []lipgloss.Color{
	"#9B6BCD",
	"#8878D8",
	"#6B8BE3",
	"#4EA8ED",
	"#00D4FF",
}

var (
	Accent = lipgloss.Color("#00D4FF")
	Subtle = lipgloss.Color("#555555")
	Green  = lipgloss.Color("#04B575")
	Red    = lipgloss.Color("#FF4444")

	TitleStyle = lipgloss.NewStyle().Bold(true).Foreground(Accent)
	BoldStyle  = lipgloss.NewStyle().Bold(true)
	BotLabel   = lipgloss.NewStyle().Bold(true).Foreground(Accent)
	UserLabel  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#AAAAAA"))
	ErrStyle   = lipgloss.NewStyle().Foreground(Red)
	OkStyle    = lipgloss.NewStyle().Foreground(Green).Bold(true)
	DimStyle   = lipgloss.NewStyle().Foreground(Subtle)
)

// RenderBanner returns the ASCII art banner with gradient coloring.
func RenderBanner() string {
	var sb strings.Builder
	for i, line := range bannerLines {
		color := bannerColors[i%len(bannerColors)]
		style := lipgloss.NewStyle().Bold(true).Foreground(color)
		sb.WriteString(style.Render(line))
		sb.WriteString("\n")
	}
	return sb.String()
}

func StatusBadge(ok bool) string {
	if ok {
		return OkStyle.Render("✓")
	}
	return DimStyle.Render("✗")
}
