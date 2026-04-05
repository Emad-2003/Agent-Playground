package dialog

import (
	utilcomponents "crawler-ai/internal/tui/components/util"
	"crawler-ai/internal/tui/styles"
	"crawler-ai/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ShowThemeDialogMsg opens the theme picker.
type ShowThemeDialogMsg struct{ Show bool }

// ThemeSelectedMsg signals a theme was chosen.
type ThemeSelectedMsg struct{ Name string }

// ThemeDialog is a theme picker.
type ThemeDialog struct {
	width  int
	height int
	names  []string
	list   utilcomponents.SimpleList[themeListItem]
}

func NewThemeDialog() *ThemeDialog {
	names := theme.AvailableThemes()
	current := theme.CurrentTheme().Name()
	items := make([]themeListItem, 0, len(names))
	selected := 0
	for i, n := range names {
		if n == current {
			selected = i
		}
		items = append(items, themeListItem{name: n, current: n == current})
	}
	list := utilcomponents.NewSimpleList(items, 10, "No themes available", true)
	for i := 0; i < selected; i++ {
		_, _ = list.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	return &ThemeDialog{names: names, list: list}
}

func (td *ThemeDialog) Init() tea.Cmd { return nil }

func (td *ThemeDialog) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		td.width = msg.Width
		td.height = msg.Height
		_, cmd := td.list.Update(msg)
		return td, cmd
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			selected, idx := td.list.GetSelectedItem()
			if idx >= 0 {
				name := selected.name
				return td, func() tea.Msg { return ThemeSelectedMsg{Name: name} }
			}
		case "esc":
			return td, func() tea.Msg { return ShowThemeDialogMsg{Show: false} }
		}
	}
	_, cmd := td.list.Update(msg)
	return td, cmd
}

func (td *ThemeDialog) View() string {
	t := theme.CurrentTheme()

	title := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Bold(true).
		Render(styles.CrawlerIcon + " Theme")

	hint := lipgloss.NewStyle().
		Foreground(t.TextSecondary()).
		Italic(true).
		Render("[↑↓] navigate  [enter] select  [esc] close")

	td.list.SetMaxWidth(32)
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", td.list.View(), "", hint)

	boxWidth := 40
	box := lipgloss.NewStyle().
		Width(boxWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.BorderFocused()).
		Background(t.BackgroundSecondary()).
		Padding(1, 2).
		Render(content)

	return lipgloss.Place(td.width, td.height, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceBackground(t.Background()))
}

// RenderBox returns just the dialog box without full-screen placement.
func (td *ThemeDialog) RenderBox() string {
	t := theme.CurrentTheme()

	title := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Bold(true).
		Render(styles.CrawlerIcon + " Theme")

	hint := lipgloss.NewStyle().
		Foreground(t.TextSecondary()).
		Italic(true).
		Render("[↑↓] navigate  [enter] select  [esc] close")

	td.list.SetMaxWidth(32)
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", td.list.View(), "", hint)

	boxWidth := 40
	return lipgloss.NewStyle().
		Width(boxWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.BorderFocused()).
		Background(t.BackgroundSecondary()).
		Padding(1, 2).
		Render(content)
}

type themeListItem struct {
	name    string
	current bool
}

func (item themeListItem) Render(selected bool, width int) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()
	itemStyle := baseStyle.Width(width).Padding(0, 1)
	secondaryStyle := baseStyle.Foreground(t.TextSecondary())
	if selected {
		itemStyle = itemStyle.Background(t.Primary()).Foreground(t.Background()).Bold(true)
		secondaryStyle = secondaryStyle.Background(t.Primary()).Foreground(t.Background())
	}
	line := itemStyle.Render(item.name)
	if item.current {
		line = lipgloss.JoinHorizontal(lipgloss.Left, line, secondaryStyle.Render(" (active)"))
	}
	return line
}
