package dialog

import (
	"strings"

	utilcomponents "crawler-ai/internal/tui/components/util"
	"crawler-ai/internal/tui/styles"
	"crawler-ai/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ShowCommandsMsg opens the command palette.
type ShowCommandsMsg struct{ Show bool }

// CommandEntry is a single command in the palette.
type CommandEntry struct {
	Label    string
	Shortcut string
	Action   func() tea.Msg
}

// CommandsDialog is a filterable command palette.
type CommandsDialog struct {
	width    int
	height   int
	entries  []CommandEntry
	filtered []commandListItem
	filter   string
	list     utilcomponents.SimpleList[commandListItem]
}

func NewCommandsDialog(entries []CommandEntry) *CommandsDialog {
	list := utilcomponents.NewSimpleList([]commandListItem{}, 12, "No matching commands", false)
	dialog := &CommandsDialog{entries: entries, list: list}
	dialog.applyFilter()
	return dialog
}

func (c *CommandsDialog) Init() tea.Cmd { return nil }

func (c *CommandsDialog) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		c.width = msg.Width
		c.height = msg.Height
		_, cmd := c.list.Update(msg)
		return c, cmd
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+p":
			_, cmd := c.list.Update(tea.KeyMsg{Type: tea.KeyUp})
			return c, cmd
		case "ctrl+n":
			_, cmd := c.list.Update(tea.KeyMsg{Type: tea.KeyDown})
			return c, cmd
		case "enter":
			selected, idx := c.list.GetSelectedItem()
			if idx >= 0 && selected.entry.Action != nil {
				return c, func() tea.Msg { return selected.entry.Action() }
			}
		case "esc":
			return c, func() tea.Msg { return ShowCommandsMsg{Show: false} }
		case "backspace":
			if len(c.filter) > 0 {
				c.filter = c.filter[:len(c.filter)-1]
				c.applyFilter()
			}
		default:
			if len(msg.String()) == 1 {
				c.filter += msg.String()
				c.applyFilter()
				return c, nil
			}
		}
	}
	_, cmd := c.list.Update(msg)
	return c, cmd
}

func (c *CommandsDialog) applyFilter() {
	filtered := make([]commandListItem, 0, len(c.entries))
	if c.filter == "" {
		for _, entry := range c.entries {
			filtered = append(filtered, commandListItem{entry: entry})
		}
	} else {
		lower := strings.ToLower(c.filter)
		for _, e := range c.entries {
			if strings.Contains(strings.ToLower(e.Label), lower) {
				filtered = append(filtered, commandListItem{entry: e})
			}
		}
	}
	c.filtered = filtered
	c.list.SetItems(filtered)
}

func (c *CommandsDialog) View() string {
	t := theme.CurrentTheme()

	title := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Bold(true).
		Render(styles.CrawlerIcon + " Commands")

	// Filter input
	filterStr := ""
	if c.filter != "" {
		filterStr = lipgloss.NewStyle().
			Foreground(t.Accent()).
			Render("> " + c.filter + "▌")
	} else {
		filterStr = lipgloss.NewStyle().
			Foreground(t.TextSecondary()).
			Italic(true).
			Render("> type to filter...")
	}

	hint := lipgloss.NewStyle().
		Foreground(t.TextSecondary()).
		Italic(true).
		Render("[↑↓] navigate  [enter] run  [esc] close")

	c.list.SetMaxWidth(42)

	content := lipgloss.JoinVertical(lipgloss.Left,
		title, "", filterStr, "", c.list.View(), "", hint)

	boxWidth := 50
	box := lipgloss.NewStyle().
		Width(boxWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.BorderFocused()).
		Background(t.BackgroundSecondary()).
		Padding(1, 2).
		Render(content)

	return lipgloss.Place(c.width, c.height, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceBackground(t.Background()))
}

// RenderBox returns just the dialog box without full-screen placement.
func (c *CommandsDialog) RenderBox() string {
	t := theme.CurrentTheme()

	title := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Bold(true).
		Render(styles.CrawlerIcon + " Commands")

	filterStr := ""
	if c.filter != "" {
		filterStr = lipgloss.NewStyle().
			Foreground(t.Accent()).
			Render("> " + c.filter + "▌")
	} else {
		filterStr = lipgloss.NewStyle().
			Foreground(t.TextSecondary()).
			Italic(true).
			Render("> type to filter...")
	}

	hint := lipgloss.NewStyle().
		Foreground(t.TextSecondary()).
		Italic(true).
		Render("[↑↓] navigate  [enter] run  [esc] close")

	c.list.SetMaxWidth(42)

	content := lipgloss.JoinVertical(lipgloss.Left,
		title, "", filterStr, "", c.list.View(), "", hint)

	boxWidth := 50
	return lipgloss.NewStyle().
		Width(boxWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.BorderFocused()).
		Background(t.BackgroundSecondary()).
		Padding(1, 2).
		Render(content)
}

type commandListItem struct {
	entry CommandEntry
}

func (item commandListItem) Render(selected bool, width int) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()
	itemStyle := baseStyle.Width(width).Padding(0, 1)
	secondaryStyle := baseStyle.Foreground(t.TextSecondary())
	if selected {
		itemStyle = itemStyle.Background(t.Primary()).Foreground(t.Background()).Bold(true)
		secondaryStyle = secondaryStyle.Background(t.Primary()).Foreground(t.Background())
	}
	line := itemStyle.Render(item.entry.Label)
	if item.entry.Shortcut != "" {
		line = lipgloss.JoinHorizontal(lipgloss.Left, line, secondaryStyle.Render("  "+item.entry.Shortcut))
	}
	return line
}
