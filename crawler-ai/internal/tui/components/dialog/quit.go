package dialog

import (
	"crawler-ai/internal/tui/styles"
	"crawler-ai/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// QuitRequestMsg signals intent to quit.
type QuitRequestMsg struct{}

// QuitConfirmMsg signals confirmed quit.
type QuitConfirmMsg struct{}

// QuitCancelMsg cancels the quit dialog.
type QuitCancelMsg struct{}

// QuitDialog renders a confirmation prompt.
type QuitDialog struct {
	width  int
	height int
}

func NewQuitDialog() *QuitDialog {
	return &QuitDialog{}
}

func (q *QuitDialog) Init() tea.Cmd { return nil }

func (q *QuitDialog) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		q.width = msg.Width
		q.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "Y", "enter":
			return q, func() tea.Msg { return QuitConfirmMsg{} }
		case "n", "N", "esc", "q":
			return q, func() tea.Msg { return QuitCancelMsg{} }
		}
	}
	return q, nil
}

func (q *QuitDialog) RenderBox() string {
	t := theme.CurrentTheme()

	title := lipgloss.NewStyle().
		Foreground(t.Error()).
		Bold(true).
		Render(styles.CrawlerIcon + " Quit?")

	body := lipgloss.NewStyle().
		Foreground(t.Text()).
		Render("Are you sure you want to exit?")

	hint := lipgloss.NewStyle().
		Foreground(t.TextSecondary()).
		Italic(true).
		Render("[y] yes  [n] no")

	content := lipgloss.JoinVertical(lipgloss.Center, title, "", body, "", hint)

	boxWidth := 38
	return lipgloss.NewStyle().
		Width(boxWidth).
		Border(lipgloss.DoubleBorder()).
		BorderForeground(t.Error()).
		Background(t.BackgroundSecondary()).
		Padding(1, 2).
		Align(lipgloss.Center).
		Render(content)
}

func (q *QuitDialog) View() string {
	t := theme.CurrentTheme()
	box := q.RenderBox()
	return lipgloss.Place(q.width, q.height, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceBackground(t.Background()))
}
