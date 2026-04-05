package dialog

import (
	utilcomponents "crawler-ai/internal/tui/components/util"
	"crawler-ai/internal/tui/styles"
	"crawler-ai/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ShowModelDialogMsg opens the model picker.
type ShowModelDialogMsg struct{ Show bool }

// ModelSelectedMsg signals a model was chosen.
type ModelSelectedMsg struct {
	Provider string
	Model    string
}

// ModelEntry represents one model option.
type ModelEntry struct {
	Provider string
	Model    string
	Current  bool
}

// ModelDialog is a model picker.
type ModelDialog struct {
	width   int
	height  int
	entries []ModelEntry
	list    utilcomponents.SimpleList[modelListItem]
}

func NewModelDialog(entries []ModelEntry) *ModelDialog {
	items := make([]modelListItem, 0, len(entries))
	selected := 0
	for i, e := range entries {
		if e.Current {
			selected = i
		}
		items = append(items, modelListItem{entry: e})
	}
	list := utilcomponents.NewSimpleList(items, 10, "No models configured", true)
	for i := 0; i < selected; i++ {
		_, _ = list.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	return &ModelDialog{entries: entries, list: list}
}

func (m *ModelDialog) SetEntries(entries []ModelEntry) {
	m.entries = entries
	items := make([]modelListItem, 0, len(entries))
	selected := 0
	for i, entry := range entries {
		if entry.Current {
			selected = i
		}
		items = append(items, modelListItem{entry: entry})
	}
	m.list.SetItems(items)
	for i := 0; i < selected; i++ {
		_, _ = m.list.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
}

func (m *ModelDialog) Init() tea.Cmd { return nil }

func (m *ModelDialog) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		_, cmd := m.list.Update(msg)
		return m, cmd
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			selected, idx := m.list.GetSelectedItem()
			if idx >= 0 {
				e := selected.entry
				return m, func() tea.Msg {
					return ModelSelectedMsg{Provider: e.Provider, Model: e.Model}
				}
			}
		case "esc":
			return m, func() tea.Msg { return ShowModelDialogMsg{Show: false} }
		}
	}
	_, cmd := m.list.Update(msg)
	return m, cmd
}

func (m *ModelDialog) View() string {
	t := theme.CurrentTheme()

	title := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Bold(true).
		Render(styles.CrawlerIcon + " Models")

	if len(m.entries) == 0 {
		empty := lipgloss.NewStyle().
			Foreground(t.TextSecondary()).
			Italic(true).
			Render("No models configured")
		content := lipgloss.JoinVertical(lipgloss.Left, title, "", empty)
		return m.renderBox(content)
	}

	hint := lipgloss.NewStyle().
		Foreground(t.TextSecondary()).
		Italic(true).
		Render("[↑↓] navigate  [enter] select  [esc] close")

	m.list.SetMaxWidth(42)
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", m.list.View(), "", hint)
	return m.renderBox(content)
}

func (m *ModelDialog) renderBoxContent(content string) string {
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

func (m *ModelDialog) renderBox(content string) string {
	t := theme.CurrentTheme()
	box := m.renderBoxContent(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceBackground(t.Background()))
}

// RenderBox returns just the dialog box without full-screen placement.
func (m *ModelDialog) RenderBox() string {
	t := theme.CurrentTheme()

	title := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Bold(true).
		Render(styles.CrawlerIcon + " Models")

	if len(m.entries) == 0 {
		empty := lipgloss.NewStyle().
			Foreground(t.TextSecondary()).
			Italic(true).
			Render("No models configured")
		content := lipgloss.JoinVertical(lipgloss.Left, title, "", empty)
		return m.renderBoxContent(content)
	}

	hint := lipgloss.NewStyle().
		Foreground(t.TextSecondary()).
		Italic(true).
		Render("[↑↓] navigate  [enter] select  [esc] close")

	m.list.SetMaxWidth(42)
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", m.list.View(), "", hint)
	return m.renderBoxContent(content)
}

type modelListItem struct {
	entry ModelEntry
}

func (item modelListItem) Render(selected bool, width int) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()
	itemStyle := baseStyle.Width(width).Padding(0, 1)
	secondaryStyle := baseStyle.Foreground(t.TextSecondary())
	if selected {
		itemStyle = itemStyle.Background(t.Primary()).Foreground(t.Background()).Bold(true)
		secondaryStyle = secondaryStyle.Background(t.Primary()).Foreground(t.Background())
	}
	line := lipgloss.JoinHorizontal(
		lipgloss.Left,
		secondaryStyle.Render(item.entry.Provider+"/"),
		itemStyle.Render(item.entry.Model),
	)
	if item.entry.Current {
		line = lipgloss.JoinHorizontal(lipgloss.Left, line, secondaryStyle.Render(" ●"))
	}
	return line
}
