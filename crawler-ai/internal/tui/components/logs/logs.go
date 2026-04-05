package logs

import (
	"fmt"
	"strings"
	"time"

	"crawler-ai/internal/tui/styles"
	"crawler-ai/internal/tui/theme"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// LogEntry represents one log line.
type LogEntry struct {
	Time    time.Time
	Level   string
	Message string
	Fields  map[string]string
}

// AddLogMsg appends a log entry.
type AddLogMsg struct{ Entry LogEntry }

// ClearLogsMsg clears all logs.
type ClearLogsMsg struct{}

// SetLogFilterMsg changes the level filter.
type SetLogFilterMsg struct{ Level string }

// LogsPage displays structured log output.
type LogsPage struct {
	width  int
	height int

	logs         []LogEntry
	filter       string // empty = all
	scrollOffset int
	follow       bool
}

func NewLogsPage() *LogsPage {
	return &LogsPage{
		follow: true,
	}
}

type logsKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Follow key.Binding
	Clear  key.Binding
	Filter key.Binding
}

var logsKeys = logsKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "scroll up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "scroll down"),
	),
	Follow: key.NewBinding(
		key.WithKeys("f"),
		key.WithHelp("f", "toggle follow"),
	),
	Clear: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "clear"),
	),
	Filter: key.NewBinding(
		key.WithKeys("1", "2", "3", "4", "0"),
		key.WithHelp("0-4", "filter level"),
	),
}

func (l *LogsPage) Init() tea.Cmd { return nil }

func (l *LogsPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		l.width = msg.Width
		l.height = msg.Height
	case AddLogMsg:
		l.logs = append(l.logs, msg.Entry)
		if l.follow {
			l.scrollToBottom()
		}
	case ClearLogsMsg:
		l.logs = nil
		l.scrollOffset = 0
	case SetLogFilterMsg:
		l.filter = msg.Level
		l.scrollOffset = 0
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, logsKeys.Up):
			l.follow = false
			if l.scrollOffset > 0 {
				l.scrollOffset--
			}
		case key.Matches(msg, logsKeys.Down):
			l.scrollOffset++
			filtered := l.filteredLogs()
			maxScroll := len(filtered) - l.viewableLines()
			if maxScroll < 0 {
				maxScroll = 0
			}
			if l.scrollOffset > maxScroll {
				l.scrollOffset = maxScroll
			}
		case key.Matches(msg, logsKeys.Follow):
			l.follow = !l.follow
			if l.follow {
				l.scrollToBottom()
			}
		case key.Matches(msg, logsKeys.Clear):
			l.logs = nil
			l.scrollOffset = 0
		case msg.String() == "0":
			l.filter = ""
		case msg.String() == "1":
			l.filter = "debug"
		case msg.String() == "2":
			l.filter = "info"
		case msg.String() == "3":
			l.filter = "warn"
		case msg.String() == "4":
			l.filter = "error"
		}
	}
	return l, nil
}

func (l *LogsPage) View() string {
	t := theme.CurrentTheme()

	// Header
	headerLeft := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Bold(true).
		Render(fmt.Sprintf("%s Logs", styles.CrawlerIcon))

	followIcon := styles.CrossIcon
	followColor := t.TextSecondary()
	if l.follow {
		followIcon = styles.CheckIcon
		followColor = t.Success()
	}
	followStr := lipgloss.NewStyle().
		Foreground(followColor).
		Render(fmt.Sprintf(" follow %s", followIcon))

	filterStr := "all"
	if l.filter != "" {
		filterStr = l.filter
	}
	filterWidget := lipgloss.NewStyle().
		Foreground(t.Accent()).
		Render(fmt.Sprintf(" [%s]", filterStr))

	countStr := lipgloss.NewStyle().
		Foreground(t.TextSecondary()).
		Render(fmt.Sprintf(" %d entries", len(l.filteredLogs())))

	header := headerLeft + followStr + filterWidget + countStr

	// Help bar
	help := lipgloss.NewStyle().
		Foreground(t.TextMuted()).
		Render("[↑↓] scroll  [f] follow  [c] clear  [0] all [1] debug [2] info [3] warn [4] error  [ctrl+l] back")

	// Calculate content height
	contentHeight := l.height - 4 // header + help + borders
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Render filtered log entries
	filtered := l.filteredLogs()
	var lines []string

	start := l.scrollOffset
	end := start + contentHeight
	if end > len(filtered) {
		end = len(filtered)
	}
	if start > len(filtered) {
		start = len(filtered)
	}

	for i := start; i < end; i++ {
		entry := filtered[i]
		lines = append(lines, l.renderEntry(entry))
	}

	// Pad if less than content height
	for len(lines) < contentHeight {
		lines = append(lines, "")
	}

	content := strings.Join(lines, "\n")

	body := lipgloss.NewStyle().
		Width(l.width - 2).
		Height(contentHeight).
		Render(content)

	return lipgloss.JoinVertical(lipgloss.Left, header, body, help)
}

func (l *LogsPage) renderEntry(entry LogEntry) string {
	t := theme.CurrentTheme()

	ts := entry.Time.Format("15:04:05.000")
	tsStr := lipgloss.NewStyle().
		Foreground(t.TextMuted()).
		Render(ts)

	levelColor := t.TextSecondary()
	switch strings.ToLower(entry.Level) {
	case "debug":
		levelColor = t.TextMuted()
	case "info":
		levelColor = t.Info()
	case "warn":
		levelColor = t.Warning()
	case "error":
		levelColor = t.Error()
	}
	levelStr := lipgloss.NewStyle().
		Foreground(levelColor).
		Bold(true).
		Width(5).
		Render(strings.ToUpper(entry.Level))

	msgStr := lipgloss.NewStyle().
		Foreground(t.Text()).
		Render(entry.Message)

	// Compact field rendering
	var fieldParts []string
	for k, v := range entry.Fields {
		fieldParts = append(fieldParts,
			lipgloss.NewStyle().Foreground(t.TextSecondary()).Render(k+"=")+
				lipgloss.NewStyle().Foreground(t.TextMuted()).Render(v))
	}
	fields := ""
	if len(fieldParts) > 0 {
		fields = " " + strings.Join(fieldParts, " ")
	}

	return fmt.Sprintf("%s %s %s%s", tsStr, levelStr, msgStr, fields)
}

func (l *LogsPage) filteredLogs() []LogEntry {
	if l.filter == "" {
		return l.logs
	}
	var out []LogEntry
	for _, e := range l.logs {
		if strings.EqualFold(e.Level, l.filter) {
			out = append(out, e)
		}
	}
	return out
}

func (l *LogsPage) viewableLines() int {
	h := l.height - 4
	if h < 1 {
		return 1
	}
	return h
}

func (l *LogsPage) scrollToBottom() {
	filtered := l.filteredLogs()
	maxScroll := len(filtered) - l.viewableLines()
	if maxScroll < 0 {
		maxScroll = 0
	}
	l.scrollOffset = maxScroll
}
