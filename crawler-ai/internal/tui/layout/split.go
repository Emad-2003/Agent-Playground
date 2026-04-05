package layout

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"crawler-ai/internal/tui/theme"
)

// SplitPaneLayout arranges up to 3 panels: left, right, and bottom.
// Horizontal split is left|right, then vertical split above bottom.
type SplitPaneLayout struct {
	width  int
	height int

	leftPanel   tea.Model
	rightPanel  tea.Model
	bottomPanel tea.Model

	horizontalRatio float64 // fraction for left panel (0..1)
	verticalRatio   float64 // fraction for top row (0..1)
}

type SplitOption func(*SplitPaneLayout)

func WithLeftPanel(m tea.Model) SplitOption {
	return func(s *SplitPaneLayout) { s.leftPanel = m }
}

func WithRightPanel(m tea.Model) SplitOption {
	return func(s *SplitPaneLayout) { s.rightPanel = m }
}

func WithBottomPanel(m tea.Model) SplitOption {
	return func(s *SplitPaneLayout) { s.bottomPanel = m }
}

func WithRatio(r float64) SplitOption {
	return func(s *SplitPaneLayout) { s.horizontalRatio = r }
}

func WithVerticalRatio(r float64) SplitOption {
	return func(s *SplitPaneLayout) { s.verticalRatio = r }
}

func NewSplitPaneLayout(opts ...SplitOption) *SplitPaneLayout {
	s := &SplitPaneLayout{
		horizontalRatio: 0.7,
		verticalRatio:   0.85,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *SplitPaneLayout) Init() tea.Cmd {
	var cmds []tea.Cmd
	if s.leftPanel != nil {
		cmds = append(cmds, s.leftPanel.Init())
	}
	if s.rightPanel != nil {
		cmds = append(cmds, s.rightPanel.Init())
	}
	if s.bottomPanel != nil {
		cmds = append(cmds, s.bottomPanel.Init())
	}
	return tea.Batch(cmds...)
}

func (s *SplitPaneLayout) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		s.propagateSize()
	}

	if s.leftPanel != nil {
		var cmd tea.Cmd
		s.leftPanel, cmd = s.leftPanel.Update(msg)
		cmds = append(cmds, cmd)
	}
	if s.rightPanel != nil {
		var cmd tea.Cmd
		s.rightPanel, cmd = s.rightPanel.Update(msg)
		cmds = append(cmds, cmd)
	}
	if s.bottomPanel != nil {
		var cmd tea.Cmd
		s.bottomPanel, cmd = s.bottomPanel.Update(msg)
		cmds = append(cmds, cmd)
	}

	return s, tea.Batch(cmds...)
}

func (s *SplitPaneLayout) View() string {
	t := theme.CurrentTheme()
	baseStyle := lipgloss.NewStyle().Background(t.Background())

	topHeight, bottomHeight := s.verticalSizes()
	leftWidth, rightWidth := s.horizontalSizes()

	// Build top row
	var topRow string
	if s.leftPanel != nil && s.rightPanel != nil {
		left := baseStyle.Width(leftWidth).Height(topHeight).Render(s.leftPanel.View())
		right := baseStyle.Width(rightWidth).Height(topHeight).Render(s.rightPanel.View())
		topRow = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	} else if s.leftPanel != nil {
		topRow = baseStyle.Width(s.width).Height(topHeight).Render(s.leftPanel.View())
	} else if s.rightPanel != nil {
		topRow = baseStyle.Width(s.width).Height(topHeight).Render(s.rightPanel.View())
	}

	if s.bottomPanel == nil || bottomHeight <= 0 {
		return topRow
	}

	bottom := baseStyle.Width(s.width).Height(bottomHeight).Render(s.bottomPanel.View())
	return lipgloss.JoinVertical(lipgloss.Left, topRow, bottom)
}

func (s *SplitPaneLayout) SetSize(width, height int) tea.Cmd {
	s.width = width
	s.height = height
	s.propagateSize()
	return nil
}

func (s *SplitPaneLayout) GetSize() (int, int) {
	return s.width, s.height
}

func (s *SplitPaneLayout) BindingKeys() []key.Binding {
	var bindings []key.Binding
	if b, ok := s.leftPanel.(Bindings); ok {
		bindings = append(bindings, b.BindingKeys()...)
	}
	if b, ok := s.rightPanel.(Bindings); ok {
		bindings = append(bindings, b.BindingKeys()...)
	}
	if b, ok := s.bottomPanel.(Bindings); ok {
		bindings = append(bindings, b.BindingKeys()...)
	}
	return bindings
}

func (s *SplitPaneLayout) SetLeftPanel(m tea.Model)   { s.leftPanel = m }
func (s *SplitPaneLayout) SetRightPanel(m tea.Model)  { s.rightPanel = m }
func (s *SplitPaneLayout) SetBottomPanel(m tea.Model) { s.bottomPanel = m }
func (s *SplitPaneLayout) ClearRightPanel()           { s.rightPanel = nil }
func (s *SplitPaneLayout) ClearBottomPanel()          { s.bottomPanel = nil }

func (s *SplitPaneLayout) propagateSize() {
	topHeight, bottomHeight := s.verticalSizes()
	leftWidth, rightWidth := s.horizontalSizes()

	if s.leftPanel != nil {
		if sz, ok := s.leftPanel.(Sizeable); ok {
			w := leftWidth
			if s.rightPanel == nil {
				w = s.width
			}
			sz.SetSize(w, topHeight)
		}
	}
	if s.rightPanel != nil {
		if sz, ok := s.rightPanel.(Sizeable); ok {
			sz.SetSize(rightWidth, topHeight)
		}
	}
	if s.bottomPanel != nil {
		if sz, ok := s.bottomPanel.(Sizeable); ok {
			sz.SetSize(s.width, bottomHeight)
		}
	}
}

func (s *SplitPaneLayout) verticalSizes() (int, int) {
	if s.bottomPanel == nil {
		return s.height, 0
	}
	topH := int(float64(s.height) * s.verticalRatio)
	bottomH := s.height - topH
	return topH, bottomH
}

func (s *SplitPaneLayout) horizontalSizes() (int, int) {
	if s.rightPanel == nil {
		return s.width, 0
	}
	leftW := int(float64(s.width) * s.horizontalRatio)
	rightW := s.width - leftW
	return leftW, rightW
}
