package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/joebot/nagobot/internal/agent"
)

// --- message types ---

type llmResponseMsg struct {
	content string
	err     error
}

// --- chat entry ---

type chatEntry struct {
	role    string // "user", "assistant", "error"
	content string
}

// --- interactive chat model ---

type chatModel struct {
	input    textinput.Model
	viewport viewport.Model
	spinner  spinner.Model

	history []chatEntry
	waiting bool

	loop *agent.Loop
	ctx  context.Context

	ready  bool
	width  int
	height int
}

func newChatModel(loop *agent.Loop, ctx context.Context) chatModel {
	ti := textinput.New()
	ti.Placeholder = "Type a message..."
	ti.Focus()
	ti.CharLimit = 0
	ti.Prompt = "❯ "
	ti.PromptStyle = lipgloss.NewStyle().Foreground(Accent)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(Accent)

	return chatModel{
		input:   ti,
		spinner: sp,
		loop:    loop,
		ctx:     ctx,
	}
}

func (m chatModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m chatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		vpHeight := msg.Height - 4 // header + 2 dividers + input
		if vpHeight < 1 {
			vpHeight = 1
		}
		if !m.ready {
			m.viewport = viewport.New(msg.Width, vpHeight)
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = vpHeight
		}
		m.input.Width = msg.Width - 4
		m.viewport.SetContent(m.renderHistory())
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyCtrlD:
			return m, tea.Quit
		case tea.KeyEnter:
			if m.waiting {
				return m, nil
			}
			input := strings.TrimSpace(m.input.Value())
			if input == "" {
				return m, nil
			}
			if isExitCmd(input) {
				return m, tea.Quit
			}
			m.history = append(m.history, chatEntry{role: "user", content: input})
			m.input.SetValue("")
			m.input.Blur()
			m.waiting = true
			m.viewport.SetContent(m.renderHistory())
			m.viewport.GotoBottom()
			return m, m.sendMessage(input)
		case tea.KeyPgUp, tea.KeyPgDown, tea.KeyUp, tea.KeyDown:
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}

	case llmResponseMsg:
		m.waiting = false
		focusCmd := m.input.Focus()
		if msg.err != nil {
			m.history = append(m.history, chatEntry{role: "error", content: msg.err.Error()})
		} else {
			m.history = append(m.history, chatEntry{role: "assistant", content: msg.content})
		}
		m.viewport.SetContent(m.renderHistory())
		m.viewport.GotoBottom()
		return m, focusCmd

	case spinner.TickMsg:
		if m.waiting {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	// Route remaining events to input when not waiting
	if !m.waiting {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m chatModel) View() string {
	if !m.ready {
		return "\n  Initializing..."
	}

	header := TitleStyle.Render(fmt.Sprintf(" %s nagobot", Logo))
	divider := DimStyle.Render(strings.Repeat("─", m.width))

	var inputLine string
	if m.waiting {
		inputLine = fmt.Sprintf(" %s Thinking...", m.spinner.View())
	} else {
		inputLine = " " + m.input.View()
	}

	return header + "\n" + divider + "\n" + m.viewport.View() + "\n" + divider + "\n" + inputLine
}

func (m chatModel) renderHistory() string {
	if len(m.history) == 0 {
		return DimStyle.Render("\n  Send a message to start chatting.\n")
	}

	var sb strings.Builder
	for _, entry := range m.history {
		sb.WriteString("\n")
		switch entry.role {
		case "user":
			sb.WriteString("  " + UserLabel.Render("You") + "\n")
			for _, line := range strings.Split(entry.content, "\n") {
				sb.WriteString("  " + line + "\n")
			}
		case "assistant":
			sb.WriteString("  " + BotLabel.Render("nagobot") + "\n")
			for _, line := range strings.Split(entry.content, "\n") {
				sb.WriteString("  " + line + "\n")
			}
		case "error":
			sb.WriteString("  " + ErrStyle.Render("Error: "+entry.content) + "\n")
		}
	}

	return sb.String()
}

func (m chatModel) sendMessage(input string) tea.Cmd {
	return func() tea.Msg {
		resp, err := m.loop.ProcessDirect(m.ctx, input, "cli:default")
		return llmResponseMsg{content: resp, err: err}
	}
}

func isExitCmd(s string) bool {
	s = strings.ToLower(s)
	return s == "exit" || s == "quit" || s == "/exit" || s == "/quit" || s == ":q"
}

// RunChat starts the interactive chat TUI.
func RunChat(loop *agent.Loop, ctx context.Context) error {
	m := newChatModel(loop, ctx)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// --- single message model ---

type singleModel struct {
	spinner spinner.Model
	loop    *agent.Loop
	ctx     context.Context
	message string
	result  string
	err     error
	done    bool
}

func (m singleModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			resp, err := m.loop.ProcessDirect(m.ctx, m.message, "cli:default")
			return llmResponseMsg{content: resp, err: err}
		},
	)
}

func (m singleModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
	case llmResponseMsg:
		m.result = msg.content
		m.err = msg.err
		m.done = true
		return m, tea.Quit
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m singleModel) View() string {
	if m.done {
		return ""
	}
	return fmt.Sprintf("\n %s Processing...\n", m.spinner.View())
}

// RunSingleMessage processes one message with a spinner, then prints the result.
func RunSingleMessage(loop *agent.Loop, ctx context.Context, message string) error {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(Accent)

	m := singleModel{
		spinner: sp,
		loop:    loop,
		ctx:     ctx,
		message: message,
	}

	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return err
	}

	fm := final.(singleModel)
	if fm.err != nil {
		fmt.Println(ErrStyle.Render("\n  Error: " + fm.err.Error()))
		return fm.err
	}

	fmt.Println()
	fmt.Println("  " + BotLabel.Render("nagobot"))
	for _, line := range strings.Split(fm.result, "\n") {
		fmt.Println("  " + line)
	}
	fmt.Println()
	return nil
}
