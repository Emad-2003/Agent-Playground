package dialog

import (
	utilcomponents "crawler-ai/internal/tui/components/util"
	"crawler-ai/internal/tui/styles"
	"crawler-ai/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ShowSessionDialogMsg opens the session picker.
type ShowSessionDialogMsg struct{ Show bool }

// SessionSelectedMsg signals a session was chosen.
type SessionSelectedMsg struct{ ID string }

// SessionEntry represents one session.
type SessionEntry struct {
	ID        string
	Label     string
	Timestamp string
	Active    bool
}

// SessionDialog is a paginated session picker.
type SessionDialog struct {
	width   int
	height  int
	entries []SessionEntry
	list    utilcomponents.SimpleList[sessionListItem]
}

func NewSessionDialog(entries []SessionEntry) *SessionDialog {
	list := utilcomponents.NewSimpleList([]sessionListItem{}, 10, "No sessions found", true)
	dialog := &SessionDialog{entries: entries, list: list}
	dialog.SetEntries(entries)
	return dialog
}

func (s *SessionDialog) SetEntries(entries []SessionEntry) {
	s.entries = entries
	items := make([]sessionListItem, 0, len(entries))
	for _, entry := range entries {
		items = append(items, sessionListItem{entry: entry})
	}
	s.list.SetItems(items)
}

func (s *SessionDialog) Init() tea.Cmd { return nil }

func (s *SessionDialog) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		_, cmd := s.list.Update(msg)
		return s, cmd
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			selected, idx := s.list.GetSelectedItem()
			if idx >= 0 {
				id := selected.entry.ID
				return s, func() tea.Msg { return SessionSelectedMsg{ID: id} }
			}
		case "esc":
			return s, func() tea.Msg { return ShowSessionDialogMsg{Show: false} }
		}
	}
	_, cmd := s.list.Update(msg)
	return s, cmd
}

func (s *SessionDialog) View() string {
	t := theme.CurrentTheme()

	title := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Bold(true).
		Render(styles.CrawlerIcon + " Sessions")

	if len(s.entries) == 0 {
		empty := lipgloss.NewStyle().
			Foreground(t.TextSecondary()).
			Italic(true).
			Render("No sessions found")
		content := lipgloss.JoinVertical(lipgloss.Left, title, "", empty)
		return s.renderBox(content)
	}

	hint := lipgloss.NewStyle().
		Foreground(t.TextSecondary()).
		Italic(true).
		Render("[↑↓] navigate  [enter] select  [esc] close")

	content := lipgloss.JoinVertical(lipgloss.Left, title, "", s.renderList(), "", hint)
	return s.renderBox(content)
}

func (s *SessionDialog) renderBoxContent(content string) string {
	t := theme.CurrentTheme()
	boxWidth := 50
	return lipgloss.NewStyle().
		Width(boxWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.BorderFocused()).
		Background(t.BackgroundSecondary()).
		Padding(1, 2).
		Render(content)
}

func (s *SessionDialog) renderBox(content string) string {
	t := theme.CurrentTheme()
	box := s.renderBoxContent(content)
	return lipgloss.Place(s.width, s.height, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceBackground(t.Background()))
}

// RenderBox returns just the dialog box without full-screen placement.
func (s *SessionDialog) RenderBox() string {
	t := theme.CurrentTheme()

	title := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Bold(true).
		Render(styles.CrawlerIcon + " Sessions")

	if len(s.entries) == 0 {
		empty := lipgloss.NewStyle().
			Foreground(t.TextSecondary()).
			Italic(true).
			Render("No sessions found")
		content := lipgloss.JoinVertical(lipgloss.Left, title, "", empty)
		return s.renderBoxContent(content)
	}

	hint := lipgloss.NewStyle().
		Foreground(t.TextSecondary()).
		Italic(true).
		Render("[↑↓] navigate  [enter] select  [esc] close")

	content := lipgloss.JoinVertical(lipgloss.Left, title, "", s.renderList(), "", hint)
	return s.renderBoxContent(content)
}

func (s *SessionDialog) renderList() string {
	maxWidth := 40
	for _, entry := range s.entries {
		if len(entry.Label) > maxWidth-4 {
			maxWidth = len(entry.Label) + 4
		}
	}
	if maxWidth < 30 {
		maxWidth = 30
	}
	s.list.SetMaxWidth(maxWidth)
	return s.list.View()
}

type sessionListItem struct {
	entry SessionEntry
}

func (item sessionListItem) Render(selected bool, width int) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()
	itemStyle := baseStyle.Width(width).Padding(0, 1)
	secondaryStyle := baseStyle.Foreground(t.TextSecondary())
	if selected {
		itemStyle = itemStyle.Background(t.Primary()).Foreground(t.Background()).Bold(true)
		secondaryStyle = secondaryStyle.Background(t.Primary()).Foreground(t.Background())
	}
	label := item.entry.Label
	if label == "" {
		label = item.entry.ID[:min(12, len(item.entry.ID))]
	}
	line := itemStyle.Render(label)
	if item.entry.Active {
		line = lipgloss.JoinHorizontal(lipgloss.Left, line, secondaryStyle.Render(" ●"))
	}
	if item.entry.Timestamp != "" {
		line = lipgloss.JoinHorizontal(lipgloss.Left, line, secondaryStyle.Render(" "+item.entry.Timestamp))
	}
	return line
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
