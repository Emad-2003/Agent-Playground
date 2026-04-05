package styles

import (
	"crawler-ai/internal/tui/theme"

	"github.com/charmbracelet/lipgloss"
)

const (
	CrawlerIcon  string = "⌬"
	CheckIcon    string = "✓"
	CrossIcon    string = "✗"
	ErrorIcon    string = "✖"
	WarningIcon  string = "⚠"
	InfoIcon     string = ""
	HintIcon     string = "i"
	SpinnerIcon  string = "..."
	LoadingIcon  string = "⟳"
	PendingIcon  string = "◐"
	DotIcon      string = "●"
	EmptyDot     string = "○"
	DocumentIcon string = "🖼"
)

func BaseStyle() lipgloss.Style {
	t := theme.CurrentTheme()
	return lipgloss.NewStyle().
		Background(t.Background()).
		Foreground(t.Text())
}

func Regular() lipgloss.Style {
	return lipgloss.NewStyle()
}

func Bold() lipgloss.Style {
	return Regular().Bold(true)
}

func Padded() lipgloss.Style {
	return Regular().Padding(0, 1)
}

func Border() lipgloss.Style {
	t := theme.CurrentTheme()
	return Regular().
		Border(lipgloss.NormalBorder()).
		BorderForeground(t.BorderNormal())
}

func ThickBorder() lipgloss.Style {
	t := theme.CurrentTheme()
	return Regular().
		Border(lipgloss.ThickBorder()).
		BorderForeground(t.BorderNormal())
}

func DoubleBorder() lipgloss.Style {
	t := theme.CurrentTheme()
	return Regular().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(t.BorderNormal())
}

func FocusedBorder() lipgloss.Style {
	t := theme.CurrentTheme()
	return Regular().
		Border(lipgloss.NormalBorder()).
		BorderForeground(t.BorderFocused())
}

func DimBorder() lipgloss.Style {
	t := theme.CurrentTheme()
	return Regular().
		Border(lipgloss.NormalBorder()).
		BorderForeground(t.BorderDim())
}

func PrimaryColor() lipgloss.AdaptiveColor    { return theme.CurrentTheme().Primary() }
func SecondaryColor() lipgloss.AdaptiveColor  { return theme.CurrentTheme().Secondary() }
func AccentColor() lipgloss.AdaptiveColor     { return theme.CurrentTheme().Accent() }
func ErrorColor() lipgloss.AdaptiveColor      { return theme.CurrentTheme().Error() }
func WarningColor() lipgloss.AdaptiveColor    { return theme.CurrentTheme().Warning() }
func SuccessColor() lipgloss.AdaptiveColor    { return theme.CurrentTheme().Success() }
func InfoColor() lipgloss.AdaptiveColor       { return theme.CurrentTheme().Info() }
func TextColor() lipgloss.AdaptiveColor       { return theme.CurrentTheme().Text() }
func TextMutedColor() lipgloss.AdaptiveColor  { return theme.CurrentTheme().TextMuted() }
func BackgroundColor() lipgloss.AdaptiveColor { return theme.CurrentTheme().Background() }
