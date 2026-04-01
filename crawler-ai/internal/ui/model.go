package ui

import (
	"fmt"
	"strings"

	"crawler-ai/internal/domain"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type AddTranscriptMsg struct {
	Entry domain.TranscriptEntry
}

type SetTasksMsg struct {
	Tasks []domain.Task
}

type SetStatusMsg struct {
	Status string
}

type ShowApprovalMsg struct {
	Request domain.ApprovalRequest
}

type ClearApprovalMsg struct{}

type Option func(*Model)

type Model struct {
	width      int
	height     int
	status     string
	transcript []domain.TranscriptEntry
	tasks      []domain.Task
	pending    *domain.ApprovalRequest
	input      textarea.Model
	styles     styles
	onSubmit   func(string)
	onApprove  func(domain.ApprovalRequest, bool)
}

type styles struct {
	app        lipgloss.Style
	pane       lipgloss.Style
	title      lipgloss.Style
	muted      lipgloss.Style
	status     lipgloss.Style
	taskDone   lipgloss.Style
	taskActive lipgloss.Style
	taskFailed lipgloss.Style
	taskIdle   lipgloss.Style
	overlay    lipgloss.Style
}

func NewModel(options ...Option) Model {
	input := textarea.New()
	input.Placeholder = "Ask crawler-ai to inspect, edit, or plan..."
	input.Focus()
	input.Prompt = "> "
	input.SetHeight(3)
	input.ShowLineNumbers = false

	model := Model{
		status:     "Foundation ready",
		transcript: make([]domain.TranscriptEntry, 0),
		tasks:      make([]domain.Task, 0),
		input:      input,
		styles:     defaultStyles(),
	}

	for _, option := range options {
		option(&model)
	}

	return model
}

func WithSubmitHandler(handler func(string)) Option {
	return func(model *Model) {
		model.onSubmit = handler
	}
}

func WithApprovalHandler(handler func(domain.ApprovalRequest, bool)) Option {
	return func(model *Model) {
		model.onApprove = handler
	}
}

func defaultStyles() styles {
	borderColor := lipgloss.Color("63")
	mutedColor := lipgloss.Color("241")

	return styles{
		app: lipgloss.NewStyle().
			Padding(1, 2),
		pane: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Padding(0, 1),
		title: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("212")),
		muted: lipgloss.NewStyle().
			Foreground(mutedColor),
		status: lipgloss.NewStyle().
			Foreground(lipgloss.Color("86")).
			Bold(true),
		taskDone:   lipgloss.NewStyle().Foreground(lipgloss.Color("42")),
		taskActive: lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
		taskFailed: lipgloss.NewStyle().Foreground(lipgloss.Color("203")),
		taskIdle:   lipgloss.NewStyle().Foreground(lipgloss.Color("252")),
		overlay: lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color("205")).
			Padding(1, 2).
			Background(lipgloss.Color("235")).
			Foreground(lipgloss.Color("230")),
	}
}

func (m Model) Init() tea.Cmd {
	return textarea.Blink
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = typed.Width
		m.height = typed.Height
		return m, nil
	case tea.KeyMsg:
		if m.pending != nil {
			switch typed.String() {
			case "y", "Y":
				if m.onApprove != nil {
					m.onApprove(*m.pending, true)
				}
				m.pending = nil
				m.status = "Approval granted"
				return m, nil
			case "n", "N", "esc":
				if m.onApprove != nil {
					m.onApprove(*m.pending, false)
				}
				m.pending = nil
				m.status = "Approval rejected"
				return m, nil
			}
		}

		switch typed.String() {
		case "ctrl+c", "ctrl+d":
			return m, tea.Quit
		case "ctrl+s":
			prompt := strings.TrimSpace(m.input.Value())
			if prompt == "" {
				return m, nil
			}
			if m.onSubmit != nil {
				m.onSubmit(prompt)
			}
			m.input.Reset()
			m.status = "Prompt submitted"
			return m, nil
		}
	case AddTranscriptMsg:
		m.transcript = append(m.transcript, typed.Entry)
		return m, nil
	case SetTasksMsg:
		m.tasks = append([]domain.Task(nil), typed.Tasks...)
		return m, nil
	case SetStatusMsg:
		m.status = typed.Status
		return m, nil
	case ShowApprovalMsg:
		request := typed.Request
		m.pending = &request
		m.status = "Approval required"
		return m, nil
	case ClearApprovalMsg:
		m.pending = nil
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading crawler-ai UI..."
	}

	leftWidth := max(40, (m.width*2/3)-4)
	rightWidth := max(24, m.width-leftWidth-8)
	contentHeight := max(12, m.height-8)

	transcriptPane := m.renderTranscript(leftWidth, contentHeight-6)
	inputPane := m.renderInput(leftWidth)
	left := lipgloss.JoinVertical(lipgloss.Left, transcriptPane, inputPane)

	right := lipgloss.JoinVertical(
		lipgloss.Left,
		m.renderTasks(rightWidth, contentHeight-2),
		m.renderStatus(rightWidth),
	)

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	view := m.styles.app.Render(body)
	if m.pending == nil {
		return view
	}

	overlay := m.renderApprovalOverlay(min(max(56, m.width/2), m.width-8))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay)
}

func (m Model) renderTranscript(width, height int) string {
	lines := make([]string, 0, len(m.transcript)+1)
	if len(m.transcript) == 0 {
		lines = append(lines, m.styles.muted.Render("No transcript yet. Runtime and providers can stream into this pane."))
	} else {
		for _, entry := range m.transcript {
			prefix := strings.ToUpper(string(entry.Kind))
			lines = append(lines, fmt.Sprintf("[%s] %s", prefix, entry.Message))
		}
	}

	body := strings.Join(lines, "\n")
	content := lipgloss.NewStyle().Width(width - 4).Height(height).Render(body)
	return m.styles.pane.Width(width).Render(
		lipgloss.JoinVertical(lipgloss.Left,
			m.styles.title.Render("Transcript"),
			content,
		),
	)
}

func (m Model) renderInput(width int) string {
	input := m.input.View()
	return m.styles.pane.Width(width).Render(
		lipgloss.JoinVertical(lipgloss.Left,
			m.styles.title.Render("Composer"),
			input,
		),
	)
}

func (m Model) renderTasks(width, height int) string {
	lines := make([]string, 0, len(m.tasks)+1)
	if len(m.tasks) == 0 {
		lines = append(lines, m.styles.muted.Render("No tasks yet."))
	} else {
		for _, task := range m.tasks {
			style := m.styles.taskIdle
			switch task.Status {
			case domain.TaskCompleted:
				style = m.styles.taskDone
			case domain.TaskRunning:
				style = m.styles.taskActive
			case domain.TaskFailed:
				style = m.styles.taskFailed
			}

			line := fmt.Sprintf("- %s [%s] <%s>", task.Title, task.Status, task.Assignee)
			if len(task.DependsOn) > 0 {
				line += fmt.Sprintf(" deps:%d", len(task.DependsOn))
			}
			lines = append(lines, style.Render(line))
			if summary := summarizeText(task.Result, max(24, width-16)); summary != "" {
				lines = append(lines, m.styles.muted.Render("  result: "+summary))
			}
		}
	}

	body := lipgloss.NewStyle().Width(width - 4).Height(height).Render(strings.Join(lines, "\n"))
	return m.styles.pane.Width(width).Render(
		lipgloss.JoinVertical(lipgloss.Left,
			m.styles.title.Render("Tasks"),
			body,
		),
	)
}

func (m Model) renderStatus(width int) string {
	body := m.styles.status.Render(m.status)
	return m.styles.pane.Width(width).Render(
		lipgloss.JoinVertical(lipgloss.Left,
			m.styles.title.Render("Status"),
			body,
		),
	)
}

func (m Model) renderApprovalOverlay(width int) string {
	if m.pending == nil {
		return ""
	}

	body := lipgloss.JoinVertical(
		lipgloss.Left,
		m.styles.title.Render("Approval Required"),
		m.pending.Action,
		m.styles.muted.Render(m.pending.Description),
		"",
		m.styles.muted.Render("Press y to approve, n to reject."),
	)

	return m.styles.overlay.Width(width).Render(body)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func summarizeText(value string, limit int) string {
	compact := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if compact == "" {
		return ""
	}
	if limit <= 0 || len(compact) <= limit {
		return compact
	}
	if limit <= 3 {
		return compact[:limit]
	}
	return compact[:limit-3] + "..."
}
