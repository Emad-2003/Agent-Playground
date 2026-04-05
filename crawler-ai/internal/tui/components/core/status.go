package core

import (
	"fmt"
	"strings"
	"time"

	"crawler-ai/internal/tui/styles"
	"crawler-ai/internal/tui/theme"
	"crawler-ai/internal/tui/util"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SetTokenUsageMsg updates token counters.
type SetTokenUsageMsg struct {
	InputTokens       int64
	OutputTokens      int64
	TotalCost         float64
	PricedResponses   int64
	UnpricedResponses int64
	Replace           bool
}

// SetModelMsg sets the model display name.
type SetModelMsg struct{ Model string }

// StatusBar is the bottom status bar component.
type StatusBar struct {
	width             int
	info              util.InfoMsg
	modelName         string
	ttl               time.Duration
	inputTokens       int64
	outputTokens      int64
	totalCost         float64
	pricedResponses   int64
	unpricedResponses int64
}

func NewStatusBar() *StatusBar {
	return &StatusBar{
		modelName: "mock",
		ttl:       10 * time.Second,
	}
}

func (s *StatusBar) Init() tea.Cmd {
	return nil
}

func (s *StatusBar) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
	case util.InfoMsg:
		s.info = msg
		ttl := msg.TTL
		if ttl == 0 {
			ttl = s.ttl
		}
		return s, tea.Tick(ttl, func(time.Time) tea.Msg {
			return util.ClearStatusMsg{}
		})
	case util.ClearStatusMsg:
		s.info = util.InfoMsg{}
	case SetModelMsg:
		s.modelName = msg.Model
	case SetTokenUsageMsg:
		if msg.Replace {
			s.inputTokens = msg.InputTokens
			s.outputTokens = msg.OutputTokens
			s.totalCost = msg.TotalCost
			s.pricedResponses = msg.PricedResponses
			s.unpricedResponses = msg.UnpricedResponses
		} else {
			s.inputTokens += msg.InputTokens
			s.outputTokens += msg.OutputTokens
			s.totalCost += msg.TotalCost
			s.pricedResponses += msg.PricedResponses
			s.unpricedResponses += msg.UnpricedResponses
		}
	}
	return s, nil
}

func (s *StatusBar) View() string {
	t := theme.CurrentTheme()

	// Help widget (left)
	helpWidget := styles.Padded().
		Background(t.TextMuted()).
		Foreground(t.BackgroundDarker()).
		Bold(true).
		Render("ctrl+? help")

	// Model widget (right)
	modelWidget := styles.Padded().
		Background(t.Secondary()).
		Foreground(t.Background()).
		Render(s.modelName)

	// Token usage widget
	var tokenWidget string
	totalTokens := s.inputTokens + s.outputTokens
	if totalTokens > 0 {
		tokens := formatTokensAndCost(totalTokens, s.totalCost, s.unpricedResponses)
		tokenWidget = styles.Padded().
			Background(t.Text()).
			Foreground(t.BackgroundSecondary()).
			Render(tokens)
	}

	// Calculate available width for status message
	helpWidth := lipgloss.Width(helpWidget)
	modelWidth := lipgloss.Width(modelWidget)
	tokenWidth := lipgloss.Width(tokenWidget)
	availableWidth := s.width - helpWidth - modelWidth - tokenWidth
	if availableWidth < 0 {
		availableWidth = 0
	}

	// Info/status area (middle)
	var infoWidget string
	if s.info.Msg != "" {
		infoStyle := styles.Padded().
			Foreground(t.Background()).
			Width(availableWidth)

		switch s.info.Type {
		case util.InfoTypeInfo:
			infoStyle = infoStyle.Background(t.Info())
		case util.InfoTypeWarn:
			infoStyle = infoStyle.Background(t.Warning())
		case util.InfoTypeError:
			infoStyle = infoStyle.Background(t.Error())
		}

		msg := s.info.Msg
		maxLen := availableWidth - 4
		if maxLen > 0 && len(msg) > maxLen {
			msg = msg[:maxLen] + "..."
		}
		infoWidget = infoStyle.Render(msg)
	} else {
		infoWidget = styles.Padded().
			Foreground(t.Text()).
			Background(t.BackgroundSecondary()).
			Width(availableWidth).
			Render("")
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, helpWidget, infoWidget, tokenWidget, modelWidget)
}

func (s *StatusBar) SetWidth(w int) {
	s.width = w
}

func formatTokensAndCost(tokens int64, cost float64, unpricedResponses int64) string {
	var formattedTokens string
	switch {
	case tokens >= 1_000_000:
		formattedTokens = fmt.Sprintf("%.1fM", float64(tokens)/1_000_000)
	case tokens >= 1_000:
		formattedTokens = fmt.Sprintf("%.1fK", float64(tokens)/1_000)
	default:
		formattedTokens = fmt.Sprintf("%d", tokens)
	}
	formattedTokens = strings.Replace(formattedTokens, ".0K", "K", 1)
	formattedTokens = strings.Replace(formattedTokens, ".0M", "M", 1)

	formattedCost := fmt.Sprintf("$%.2f", cost)
	if unpricedResponses > 0 {
		formattedCost = fmt.Sprintf("~$%.2f (+%d unpriced)", cost, unpricedResponses)
	}
	return fmt.Sprintf("Context: %s, Cost: %s", formattedTokens, formattedCost)
}
