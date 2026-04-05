package layout

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"crawler-ai/internal/tui/theme"
)

type Container interface {
	tea.Model
	Sizeable
	Bindings
}

type ContainerOption func(*containerModel)

type containerModel struct {
	content     tea.Model
	width       int
	height      int
	paddingTop  int
	paddingBot  int
	paddingL    int
	paddingR    int
	hasBorder   bool
	borderStyle lipgloss.Border
}

func WithPaddingTop(n int) ContainerOption {
	return func(c *containerModel) { c.paddingTop = n }
}

func WithPaddingBottom(n int) ContainerOption {
	return func(c *containerModel) { c.paddingBot = n }
}

func WithPaddingLeft(n int) ContainerOption {
	return func(c *containerModel) { c.paddingL = n }
}

func WithPaddingRight(n int) ContainerOption {
	return func(c *containerModel) { c.paddingR = n }
}

func WithPadding(top, right, bottom, left int) ContainerOption {
	return func(c *containerModel) {
		c.paddingTop = top
		c.paddingR = right
		c.paddingBot = bottom
		c.paddingL = left
	}
}

func WithBorder() ContainerOption {
	return func(c *containerModel) {
		c.hasBorder = true
		c.borderStyle = lipgloss.NormalBorder()
	}
}

func WithBorderRounded() ContainerOption {
	return func(c *containerModel) {
		c.hasBorder = true
		c.borderStyle = lipgloss.RoundedBorder()
	}
}

func WithBorderThick() ContainerOption {
	return func(c *containerModel) {
		c.hasBorder = true
		c.borderStyle = lipgloss.ThickBorder()
	}
}

func NewContainer(content tea.Model, opts ...ContainerOption) Container {
	c := &containerModel{content: content}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *containerModel) Init() tea.Cmd {
	return c.content.Init()
}

func (c *containerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		c.width = msg.Width
		c.height = msg.Height
		innerW, innerH := c.innerSize()
		if s, ok := c.content.(Sizeable); ok {
			s.SetSize(innerW, innerH)
		}
	}
	var cmd tea.Cmd
	c.content, cmd = c.content.Update(msg)
	return c, cmd
}

func (c *containerModel) View() string {
	t := theme.CurrentTheme()

	style := lipgloss.NewStyle().
		Width(c.width).
		Height(c.height).
		Background(t.Background()).
		Padding(c.paddingTop, c.paddingR, c.paddingBot, c.paddingL)

	if c.hasBorder {
		style = style.
			Border(c.borderStyle).
			BorderForeground(t.BorderNormal()).
			BorderBackground(t.Background())
	}

	return style.Render(c.content.View())
}

func (c *containerModel) SetSize(width, height int) tea.Cmd {
	c.width = width
	c.height = height
	innerW, innerH := c.innerSize()
	if s, ok := c.content.(Sizeable); ok {
		return s.SetSize(innerW, innerH)
	}
	return nil
}

func (c *containerModel) GetSize() (int, int) {
	return c.width, c.height
}

func (c *containerModel) BindingKeys() []key.Binding {
	if b, ok := c.content.(Bindings); ok {
		return b.BindingKeys()
	}
	return nil
}

func (c *containerModel) innerSize() (int, int) {
	w := c.width - c.paddingL - c.paddingR
	h := c.height - c.paddingTop - c.paddingBot
	if c.hasBorder {
		w -= 2
		h -= 2
	}
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	return w, h
}
