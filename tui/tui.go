package tui

import (
	"context"
	"fmt"
	"strings"

	"ai_oauth_proxy/engine"
	"ai_oauth_proxy/metrics"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type model struct {
	tracker         *metrics.TokenTracker
	viewport        viewport.Model
	textarea        textarea.Model
	messages        []engine.Message
	history         []string
	historyIndex    int
	currentResponse string
	isResponding    bool
	cancel          context.CancelFunc
	eventChan       <-chan engine.EngineEvent
	width           int
	height          int
	err             error
	status          string
}

// Msg definitions
type doneMsg struct{}

var (
	// Styling
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00E5FF")).
			Background(lipgloss.Color("#1E1E2F")).
			Padding(0, 2)

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#7D57C2")).
			Padding(0, 2)

	sidebarStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D57C2")).
			Padding(1)

	chatStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#00E5FF")).
			Padding(1)

	inputStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#90A4AE")).
			Padding(0, 1)

	shortcutStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#90A4AE"))

	activeShortcutStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7D57C2")).
				Bold(true)
)

func NewModel(tracker *metrics.TokenTracker) model {
	ta := textarea.New()
	ta.Placeholder = "Type a prompt to ask Claude (Ctrl+C to exit, Esc to interrupt)..."
	ta.Focus()
	ta.Prompt = "┃ "
	ta.CharLimit = 200000 // Handle extremely large inputs
	ta.SetWidth(50)
	ta.SetHeight(3)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()

	vp := viewport.New(50, 10)
	vp.SetContent("Welcome! Chat with Claude locally via pre-authenticated Claude Code CLI.\nType your prompt below and press Enter.")

	return model{
		tracker:      tracker,
		textarea:     ta,
		viewport:     vp,
		history:      []string{},
		historyIndex: -1,
		status:       "Idle",
	}
}

func (m model) Init() tea.Cmd {
	return textarea.Blink
}

func waitForEvent(ch <-chan engine.EngineEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return doneMsg{}
		}
		return ev
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd tea.Cmd
		vpCmd tea.Cmd
	)

	m.textarea, tiCmd = m.textarea.Update(msg)
	m.viewport, vpCmd = m.viewport.Update(msg)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.recalculateLayout()

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.isResponding {
				// Interrupt response
				if m.cancel != nil {
					m.cancel()
				}
				m.isResponding = false
				m.status = "Interrupted"
				m.currentResponse += "\n\n[Interrupted]"
				m.messages = append(m.messages, engine.Message{Role: "assistant", Content: m.currentResponse})
				m.currentResponse = ""
				m.viewport.SetContent(m.formatHistory())
				m.viewport.GotoBottom()
				return m, nil
			} else {
				// Clear draft or exit
				if m.textarea.Value() != "" {
					m.textarea.SetValue("")
					m.historyIndex = len(m.history)
				} else {
					return m, tea.Quit
				}
			}

		case "ctrl+l":
			// Force redraw by returning ClearScreen
			return m, tea.ClearScreen

		case "ctrl+r":
			// Reverse search / command history cycling
			if len(m.history) > 0 {
				if m.historyIndex == -1 {
					m.historyIndex = len(m.history) - 1
				} else {
					m.historyIndex--
					if m.historyIndex < 0 {
						m.historyIndex = len(m.history) - 1
					}
				}
				m.textarea.SetValue(m.history[m.historyIndex])
				m.textarea.CursorEnd()
			}

		case "esc":
			if m.isResponding {
				if m.cancel != nil {
					m.cancel()
				}
				m.isResponding = false
				m.status = "Interrupted"
				m.currentResponse += "\n\n[Interrupted]"
				m.messages = append(m.messages, engine.Message{Role: "assistant", Content: m.currentResponse})
				m.currentResponse = ""
				m.viewport.SetContent(m.formatHistory())
				m.viewport.GotoBottom()
				return m, nil
			}

		case "enter":
			// We submit the prompt if not empty and not responding
			if !m.isResponding && strings.TrimSpace(m.textarea.Value()) != "" {
				promptText := m.textarea.Value()
				m.textarea.SetValue("")
				m.history = append(m.history, promptText)
				m.historyIndex = len(m.history)

				// Append user message
				m.messages = append(m.messages, engine.Message{Role: "user", Content: promptText})
				m.currentResponse = ""
				m.isResponding = true
				m.status = "Responding..."
				m.viewport.SetContent(m.formatHistory())
				m.viewport.GotoBottom()

				// Flatten entire history
				systemPrompt, transcript := engine.FlattenMessages(m.messages)

				// Run Claude
				ctx, cancel := context.WithCancel(context.Background())
				m.cancel = cancel

				ch, err := engine.RunClaude(ctx, systemPrompt, transcript)
				if err != nil {
					m.err = err
					m.isResponding = false
					m.status = "Error"
					cancel()
					return m, nil
				}

				m.eventChan = ch
				return m, waitForEvent(ch)
			}
		}

	case engine.EngineEvent:
		switch msg.Type {
		case "delta":
			m.currentResponse += msg.Text
			m.viewport.SetContent(m.formatHistory())
			m.viewport.GotoBottom()
			return m, waitForEvent(m.eventChan)

		case "usage":
			m.tracker.AddUsage(msg.InputTokens, msg.OutputTokens, msg.CacheRead, msg.CacheCreate)
			return m, waitForEvent(m.eventChan)

		case "result":
			// Save the final text to messages
			if msg.Text != "" {
				m.currentResponse = msg.Text
			}
			m.messages = append(m.messages, engine.Message{Role: "assistant", Content: m.currentResponse})
			m.currentResponse = ""
			m.isResponding = false
			m.status = "Idle"
			m.viewport.SetContent(m.formatHistory())
			m.viewport.GotoBottom()
			return m, waitForEvent(m.eventChan)

		case "error":
			m.err = msg.Error
			m.isResponding = false
			m.status = "Error"
			return m, waitForEvent(m.eventChan)
		}

	case doneMsg:
		m.isResponding = false
		m.status = "Idle"
		m.cancel = nil
		m.eventChan = nil
		m.viewport.SetContent(m.formatHistory())
		m.viewport.GotoBottom()
		return m, nil
	}

	return m, tea.Batch(tiCmd, vpCmd)
}

func (m *model) recalculateLayout() {
	// Header: 1 row + spacing
	headerHeight := 3

	// Input block: textarea height (3) + inputStyle borders (2) + padding
	inputHeight := 5

	// Left Pane (Chat history Viewport): Total Height - Header - Input - Borders (2)
	chatH := m.height - headerHeight - inputHeight - 2
	if chatH < 5 {
		chatH = 5
	}

	// Width distribution: 75% chat, 25% sidebar
	sidebarW := m.width / 4
	if sidebarW < 28 {
		sidebarW = 28
	}
	if sidebarW > 45 {
		sidebarW = 45
	}
	chatW := m.width - sidebarW - 2
	if chatW < 20 {
		chatW = 20
	}

	m.viewport.Width = chatW - 2
	m.viewport.Height = chatH

	m.textarea.SetWidth(chatW - 2)
}

func (m model) formatHistory() string {
	var sb strings.Builder
	for _, msg := range m.messages {
		if msg.Role == "user" {
			sb.WriteString("\033[1;35m● User:\033[0m\n")
			sb.WriteString(msg.Content)
			sb.WriteString("\n\n")
		} else if msg.Role == "assistant" {
			sb.WriteString("\033[1;36m▲ Claude:\033[0m\n")
			sb.WriteString(msg.Content)
			sb.WriteString("\n\n")
		}
	}
	if m.currentResponse != "" {
		sb.WriteString("\033[1;36m▲ Claude:\033[0m\n")
		sb.WriteString(m.currentResponse)
	}
	return sb.String()
}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Initializing premium terminal layout..."
	}

	// 1. Header View
	statusColor := "#00FF66"
	if m.isResponding {
		statusColor = "#FFB300"
	} else if m.status == "Error" {
		statusColor = "#FF1744"
	}
	
	headerL := fmt.Sprintf(" %s  %s",
		titleStyle.Render("AI OAUTH PROXY"),
		headerStyle.Render(fmt.Sprintf("MODEL: claude-sonnet-4-6  |  STATUS: %s", lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor)).Render(m.status))),
	)
	
	headerView := lipgloss.NewStyle().Width(m.width).Background(lipgloss.Color("#1E1E2F")).Render(headerL)

	// 2. Sidebar View
	inputT, outputT, cost := m.tracker.GetStats()
	
	// Estimated daily stats
	var projectedCost float64
	var projectedTokens int64
	// Simple session token/cost tracker
	totalT := inputT + outputT

	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D57C2")).Render(" SESSION STATISTICS") + "\n")
	sb.WriteString(fmt.Sprintf("Sent:   \033[1;37m%d\033[0m tokens\n", inputT))
	sb.WriteString(fmt.Sprintf("Recv:   \033[1;37m%d\033[0m tokens\n", outputT))
	sb.WriteString(fmt.Sprintf("Total:  \033[1;37m%d\033[0m tokens\n\n", totalT))

	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00E5FF")).Render(" ESTIMATED BILLING") + "\n")
	sb.WriteString(fmt.Sprintf("Cost:   \033[1;32m$%.5f\033[0m USD\n\n", cost))

	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFD54F")).Render(" DAILY PROJECTION") + "\n")
	sb.WriteString(fmt.Sprintf("Tokens: \033[0;90m%d\033[0m t/day\n", projectedTokens))
	sb.WriteString(fmt.Sprintf("Cost:   \033[1;32m$%.2f\033[0m/day\n\n", projectedCost))

	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#90A4AE")).Render(" SHORTCUTS") + "\n")
	sb.WriteString(fmt.Sprintf("%s Interrupt/Exit\n", activeShortcutStyle.Render("^C")))
	sb.WriteString(fmt.Sprintf("%s Force Redraw\n", activeShortcutStyle.Render("^L")))
	sb.WriteString(fmt.Sprintf("%s History Search\n", activeShortcutStyle.Render("^R")))
	sb.WriteString(fmt.Sprintf("%s Stop AI Stream\n", activeShortcutStyle.Render("Esc")))

	if m.err != nil {
		sb.WriteString("\n" + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF1744")).Render("LAST ERROR:") + "\n")
		sb.WriteString(fmt.Sprintf("\033[0;31m%v\033[0m", m.err))
	}

	sidebarH := m.height - 5
	sidebarContent := sidebarStyle.
		Width(m.width - m.viewport.Width - 5).
		Height(sidebarH).
		Render(sb.String())

	// 3. Main Chat Viewport & Input Textarea
	chatBox := chatStyle.Width(m.viewport.Width + 2).Height(m.viewport.Height + 2).Render(m.viewport.View())
	inputBox := inputStyle.Width(m.viewport.Width + 2).Render(m.textarea.View())

	mainLayout := lipgloss.JoinVertical(lipgloss.Left, chatBox, inputBox)

	// Combine layout horizontally
	body := lipgloss.JoinHorizontal(lipgloss.Top, mainLayout, sidebarContent)

	// 4. Return everything stacked
	return lipgloss.JoinVertical(lipgloss.Left, headerView, body)
}

func StartTUI(tracker *metrics.TokenTracker) error {
	p := tea.NewProgram(NewModel(tracker), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
