package chat

import (
	"fmt"
	"strings"

	"crawler-ai/internal/domain"
	"crawler-ai/internal/tui/styles"
	"crawler-ai/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type sidebarCmp struct {
	width, height int
	tasks         []domain.Task
	activity      []ActivityEntry
	sessionTitle  string
}

func (s *sidebarCmp) Init() tea.Cmd { return nil }

func (s *sidebarCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case SetTasksMsg:
		s.tasks = msg.Tasks
	case AddActivityMsg:
		s.activity = append(s.activity, msg.Entry)
		if len(s.activity) > 50 {
			s.activity = s.activity[len(s.activity)-50:]
		}
	case SetSessionTitleMsg:
		s.sessionTitle = msg.Title
	}
	return s, nil
}

func (s *sidebarCmp) View() string {
	baseStyle := styles.BaseStyle()

	return baseStyle.
		Width(s.width).
		PaddingLeft(2).
		PaddingRight(1).
		Height(s.height).
		Render(
			lipgloss.JoinVertical(
				lipgloss.Top,
				s.header(),
				" ",
				s.sessionSection(),
				" ",
				s.tasksSection(),
				" ",
				s.activitySection(),
			),
		)
}

func (s *sidebarCmp) header() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	logo := fmt.Sprintf("%s %s", styles.CrawlerIcon, "crawler-ai")
	return baseStyle.
		Bold(true).
		Width(s.width - 3).
		Render(
			lipgloss.JoinHorizontal(
				lipgloss.Left,
				logo,
				" ",
				baseStyle.Foreground(t.TextMuted()).Render("v0.1"),
			),
		)
}

func (s *sidebarCmp) sessionSection() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	key := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Render("Session")

	title := s.sessionTitle
	if title == "" {
		title = "default"
	}

	value := baseStyle.
		Foreground(t.Text()).
		Render(fmt.Sprintf(": %s", title))

	return lipgloss.JoinHorizontal(lipgloss.Left, key, value)
}

func (s *sidebarCmp) tasksSection() string {
	if len(s.tasks) == 0 {
		return ""
	}
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	title := baseStyle.
		Foreground(t.TextEmphasized()).
		Bold(true).
		Render("Tasks")

	var lines []string
	lines = append(lines, title)

	maxShow := 8
	start := 0
	if len(s.tasks) > maxShow {
		start = len(s.tasks) - maxShow
	}
	for _, task := range s.tasks[start:] {
		icon := styles.EmptyDot
		fg := t.TextMuted()
		switch task.Status {
		case domain.TaskRunning:
			icon = styles.PendingIcon
			fg = t.Warning()
		case domain.TaskCompleted:
			icon = styles.CheckIcon
			fg = t.Success()
		case domain.TaskFailed:
			icon = styles.ErrorIcon
			fg = t.Error()
		case domain.TaskPending:
			icon = styles.EmptyDot
			fg = t.TextMuted()
		}

		w := s.width - 6
		if w < 5 {
			w = 5
		}
		label := task.Title
		if len(label) > w {
			label = label[:w-3] + "..."
		}

		line := lipgloss.NewStyle().
			Foreground(fg).
			Render(fmt.Sprintf(" %s %s", icon, label))
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func (s *sidebarCmp) activitySection() string {
	if len(s.activity) == 0 {
		return ""
	}
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	title := baseStyle.
		Foreground(t.TextEmphasized()).
		Bold(true).
		Render("Recent")

	var lines []string
	lines = append(lines, title)

	maxShow := 12
	start := 0
	if len(s.activity) > maxShow {
		start = len(s.activity) - maxShow
	}
	for _, a := range s.activity[start:] {
		icon := styles.DotIcon
		fg := t.TextMuted()
		switch a.Level {
		case ActivityPending:
			icon = styles.PendingIcon
			fg = t.Warning()
		case ActivitySuccess:
			icon = styles.CheckIcon
			fg = t.Success()
		case ActivityError:
			icon = styles.ErrorIcon
			fg = t.Error()
		}

		w := s.width - 6
		if w < 5 {
			w = 5
		}
		text := a.Label
		if a.Detail != "" {
			text += " " + a.Detail
		}
		if len(text) > w {
			text = text[:w-3] + "..."
		}

		line := lipgloss.NewStyle().
			Foreground(fg).
			Render(fmt.Sprintf(" %s %s", icon, text))
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func (s *sidebarCmp) SetSize(width, height int) tea.Cmd {
	s.width = width
	s.height = height
	return nil
}

func (s *sidebarCmp) GetSize() (int, int) {
	return s.width, s.height
}

func newSidebarCmp() *sidebarCmp {
	return &sidebarCmp{}
}
