package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
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

// --- chat config ---

// ChatConfig holds display metadata for the chat TUI.
type ChatConfig struct {
	Model     string
	Workspace string
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

	history    []chatEntry
	waiting    bool
	cancelFunc context.CancelFunc

	loop *agent.Loop
	ctx  context.Context

	ready     bool
	width     int
	height    int
	model     string
	workspace string
}

func newChatModel(loop *agent.Loop, ctx context.Context, cfg ChatConfig) chatModel {
	ti := textinput.New()
	ti.Placeholder = "Type a message..."
	ti.Focus()
	ti.CharLimit = 0
	ti.Prompt = "❯ "
	ti.PromptStyle = lipgloss.NewStyle().Foreground(Accent)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(Accent)

	ws := cfg.Workspace
	if home, err := os.UserHomeDir(); err == nil {
		ws = strings.Replace(ws, home, "~", 1)
	}

	return chatModel{
		input:     ti,
		spinner:   sp,
		loop:      loop,
		ctx:       ctx,
		model:     cfg.Model,
		workspace: ws,
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
		// Layout: header(1) + divider(1) + viewport + divider(1) + input(1) + status(1) = 5 fixed
		vpHeight := msg.Height - 5
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
			msgCtx, cancel := context.WithCancel(m.ctx)
			m.cancelFunc = cancel
			m.viewport.SetContent(m.renderHistory())
			m.viewport.GotoBottom()
			return m, m.sendMessageWithCtx(msgCtx, input)
		case tea.KeyEsc:
			if m.waiting && m.cancelFunc != nil {
				m.cancelFunc()
				m.cancelFunc = nil
			}
			return m, nil
		case tea.KeyPgUp, tea.KeyPgDown, tea.KeyUp, tea.KeyDown:
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}

	case llmResponseMsg:
		m.waiting = false
		m.cancelFunc = nil
		focusCmd := m.input.Focus()
		if msg.err != nil {
			if errors.Is(msg.err, context.Canceled) {
				m.history = append(m.history, chatEntry{role: "assistant", content: "[Interrupted]"})
			} else {
				m.history = append(m.history, chatEntry{role: "error", content: msg.err.Error()})
			}
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
		inputLine = fmt.Sprintf(" %s Thinking... (Esc to stop)", m.spinner.View())
	} else {
		inputLine = " " + m.input.View()
	}

	statusBar := m.renderStatusBar()

	return header + "\n" +
		divider + "\n" +
		m.viewport.View() + "\n" +
		divider + "\n" +
		inputLine + "\n" +
		statusBar
}

func (m chatModel) renderHistory() string {
	if len(m.history) == 0 {
		return m.renderWelcome()
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

func (m chatModel) renderWelcome() string {
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(RenderBanner())
	sb.WriteString("\n")
	sb.WriteString("  " + BoldStyle.Render("Tips for getting started:") + "\n")
	sb.WriteString(DimStyle.Render("  1. /help for available commands") + "\n")
	sb.WriteString(DimStyle.Render("  2. Ask questions or give instructions") + "\n")
	sb.WriteString(DimStyle.Render("  3. /compact to compress context") + "\n")
	sb.WriteString(DimStyle.Render("  4. /context to check token usage") + "\n")
	return sb.String()
}

func (m chatModel) renderStatusBar() string {
	left := DimStyle.Render(" " + m.workspace)
	right := DimStyle.Render(m.model + " ")

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}

	return left + strings.Repeat(" ", gap) + right
}

func (m chatModel) sendMessageWithCtx(ctx context.Context, input string) tea.Cmd {
	return func() tea.Msg {
		resp, err := m.loop.ProcessDirect(ctx, input, "cli:default")
		return llmResponseMsg{content: resp, err: err}
	}
}

func isExitCmd(s string) bool {
	s = strings.ToLower(s)
	return s == "exit" || s == "quit" || s == "/exit" || s == "/quit" || s == ":q"
}

// RunChat starts the interactive chat TUI.
func RunChat(loop *agent.Loop, ctx context.Context, cfg ChatConfig) error {
	m := newChatModel(loop, ctx, cfg)
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
