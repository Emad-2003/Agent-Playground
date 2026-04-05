package dialog

import (
	"strings"

	"crawler-ai/internal/tui/styles"
	"crawler-ai/internal/tui/theme"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ShowHelpMsg toggles the help dialog.
type ShowHelpMsg struct{ Show bool }

// HelpDialog renders keyboard shortcuts.
type HelpDialog struct {
	width  int
	height int
}

func NewHelpDialog() *HelpDialog {
	return &HelpDialog{}
}

func (h *HelpDialog) Init() tea.Cmd { return nil }

func (h *HelpDialog) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h.width = msg.Width
		h.height = msg.Height
	}
	return h, nil
}

type helpEntry struct {
	key  string
	desc string
}

func (h *HelpDialog) View() string {
	t := theme.CurrentTheme()
	box := h.RenderBox()
	return lipgloss.Place(h.width, h.height, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceBackground(t.Background()))
}

// RenderBox returns just the dialog box without full-screen placement.
func (h *HelpDialog) RenderBox() string {
	t := theme.CurrentTheme()

	entries := []helpEntry{
		{"ctrl+s", "Submit message"},
		{"ctrl+c", "Quit"},
		{"ctrl+?", "Toggle help"},
		{"ctrl+l", "View logs"},
		{"ctrl+k", "Commands"},
		{"ctrl+o", "Model selection"},
		{"ctrl+t", "Switch theme"},
		{"ctrl+↑/↓", "Scroll messages"},
		{"y/n", "Approve/reject (when prompted)"},
	}

	maxKeyLen := 0
	for _, e := range entries {
		if len(e.key) > maxKeyLen {
			maxKeyLen = len(e.key)
		}
	}

	var lines []string
	for _, e := range entries {
		keyStr := lipgloss.NewStyle().
			Foreground(t.Accent()).
			Bold(true).
			Width(maxKeyLen + 2).
			Render(e.key)
		descStr := lipgloss.NewStyle().
			Foreground(t.Text()).
			Render(e.desc)
		lines = append(lines, keyStr+descStr)
	}

	title := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Bold(true).
		Render(styles.CrawlerIcon + " Keyboard Shortcuts")

	content := lipgloss.JoinVertical(lipgloss.Left, title, "", strings.Join(lines, "\n"))

	boxWidth := 45
	return lipgloss.NewStyle().
		Width(boxWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.BorderFocused()).
		Background(t.BackgroundSecondary()).
		Padding(1, 2).
		Render(content)
}

// HelpKey binding
var HelpKey = key.NewBinding(
	key.WithKeys("ctrl+_", "ctrl+h"),
	key.WithHelp("ctrl+?", "toggle help"),
)
