package cli

import "github.com/charmbracelet/lipgloss"

const Logo = "🤖"
const Version = "0.1.0"

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

func StatusBadge(ok bool) string {
	if ok {
		return OkStyle.Render("✓")
	}
	return DimStyle.Render("✗")
}
