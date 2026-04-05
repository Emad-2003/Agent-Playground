package chat

import (
	"fmt"

	"crawler-ai/internal/domain"
	"crawler-ai/internal/tui/layout"
	"crawler-ai/internal/tui/theme"

	"github.com/charmbracelet/lipgloss"
)

// renderApproval overlays an approval dialog on top of base content.
func renderApproval(base string, approval *domain.ApprovalRequest, width, height int) string {
	if approval == nil {
		return base
	}

	t := theme.CurrentTheme()

	overlayWidth := 60
	if width < 70 {
		overlayWidth = width - 10
	}
	if overlayWidth < 20 {
		overlayWidth = 20
	}

	title := lipgloss.NewStyle().
		Foreground(t.Warning()).
		Bold(true).
		Width(overlayWidth - 4).
		Align(lipgloss.Center).
		Render("⚠ Approval Required")

	body := lipgloss.NewStyle().
		Foreground(t.Text()).
		Width(overlayWidth - 4).
		Render(fmt.Sprintf("Action: %s\n\n%s", approval.Action, approval.Description))

	hint := lipgloss.NewStyle().
		Foreground(t.TextMuted()).
		Width(overlayWidth - 4).
		Align(lipgloss.Center).
		Render("[y] approve  [n] reject")

	inner := lipgloss.JoinVertical(lipgloss.Left, "", title, "", body, "", hint, "")

	box := lipgloss.NewStyle().
		Width(overlayWidth).
		Border(lipgloss.DoubleBorder()).
		BorderForeground(t.Warning()).
		Background(t.BackgroundSecondary()).
		Padding(1, 2).
		Render(inner)

	boxH := lipgloss.Height(box)
	x := (width - overlayWidth) / 2
	y := (height - boxH) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	return layout.PlaceOverlay(x, y, box, base, true)
}
