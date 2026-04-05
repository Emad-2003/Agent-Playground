package chat

import (
	"strings"

	"crawler-ai/internal/tui/layout"
	"crawler-ai/internal/tui/styles"
	"crawler-ai/internal/tui/theme"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SubmitMsg signals the user submitted a prompt.
type SubmitMsg struct{ Text string }

// EditorKeyMaps bindings for the editor.
type EditorKeyMaps struct {
	Send key.Binding
}

var editorMaps = EditorKeyMaps{
	Send: key.NewBinding(
		key.WithKeys("enter", "ctrl+s"),
		key.WithHelp("enter", "send message"),
	),
}

type editorCmp struct {
	width    int
	height   int
	textarea textarea.Model
	busy     bool
}

func (e *editorCmp) Init() tea.Cmd {
	return textarea.Blink
}

func (e *editorCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case SetBusyMsg:
		e.busy = bool(msg)
		return e, nil
	case tea.KeyMsg:
		if key.Matches(msg, messageKeys.PageUp) || key.Matches(msg, messageKeys.PageDown) ||
			key.Matches(msg, messageKeys.HalfPageUp) || key.Matches(msg, messageKeys.HalfPageDown) {
			return e, nil
		}
		if e.textarea.Focused() && key.Matches(msg, editorMaps.Send) {
			if e.busy {
				return e, nil
			}
			value := e.textarea.Value()
			if len(value) > 0 && value[len(value)-1] == '\\' {
				e.textarea.SetValue(value[:len(value)-1] + "\n")
				return e, nil
			}
			e.textarea.Reset()
			if strings.TrimSpace(value) == "" {
				return e, nil
			}
			return e, func() tea.Msg {
				return SubmitMsg{Text: value}
			}
		}
	}

	e.textarea, cmd = e.textarea.Update(msg)
	return e, cmd
}

func (e *editorCmp) View() string {
	t := theme.CurrentTheme()
	prompt := lipgloss.NewStyle().
		Padding(0, 0, 0, 1).
		Bold(true).
		Foreground(t.Primary()).
		Render(">")
	return lipgloss.JoinHorizontal(lipgloss.Top, prompt, e.textarea.View())
}

func (e *editorCmp) SetSize(width, height int) tea.Cmd {
	e.width = width
	e.height = height
	e.textarea.SetWidth(width - 3)
	e.textarea.SetHeight(height)
	return nil
}

func (e *editorCmp) GetSize() (int, int) {
	return e.width, e.height
}

func (e *editorCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(editorMaps)
}

func createTextArea(existing *textarea.Model) textarea.Model {
	t := theme.CurrentTheme()
	bgColor := t.Background()
	textColor := t.Text()
	textMutedColor := t.TextMuted()

	ta := textarea.New()
	ta.BlurredStyle.Base = styles.BaseStyle().Background(bgColor).Foreground(textColor)
	ta.BlurredStyle.CursorLine = styles.BaseStyle().Background(bgColor)
	ta.BlurredStyle.Placeholder = styles.BaseStyle().Background(bgColor).Foreground(textMutedColor)
	ta.BlurredStyle.Text = styles.BaseStyle().Background(bgColor).Foreground(textColor)
	ta.FocusedStyle.Base = styles.BaseStyle().Background(bgColor).Foreground(textColor)
	ta.FocusedStyle.CursorLine = styles.BaseStyle().Background(bgColor)
	ta.FocusedStyle.Placeholder = styles.BaseStyle().Background(bgColor).Foreground(textMutedColor)
	ta.FocusedStyle.Text = styles.BaseStyle().Background(bgColor).Foreground(textColor)

	ta.Prompt = " "
	ta.Placeholder = "Type a message... (enter to send, \\ + enter for newline)"
	ta.ShowLineNumbers = false
	ta.CharLimit = -1

	if existing != nil {
		ta.SetValue(existing.Value())
		ta.SetWidth(existing.Width())
		ta.SetHeight(existing.Height())
	}

	ta.Focus()
	return ta
}

func newEditorCmp() *editorCmp {
	ta := createTextArea(nil)
	return &editorCmp{textarea: ta}
}
