package chat

import (
	"time"

	"crawler-ai/internal/domain"
	"crawler-ai/internal/tui/layout"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// Messages for chat state management.
type (
	AddTranscriptMsg    struct{ Entry domain.TranscriptEntry }
	UpdateTranscriptMsg struct{ Entry domain.TranscriptEntry }
	SetTranscriptMsg    struct{ Entries []domain.TranscriptEntry }
	StreamDeltaMsg      struct{ Text string }
	StreamDoneMsg       struct{}
	SetStatusMsg        struct{ Status string }
	ShowApprovalMsg     struct{ Request domain.ApprovalRequest }
	ClearApprovalMsg    struct{}
	SetTasksMsg         struct{ Tasks []domain.Task }
	AddActivityMsg      struct{ Entry ActivityEntry }
	SetBusyMsg          bool
	SetSessionTitleMsg  struct{ Title string }
)

// ActivityEntry represents an item in the activity rail.
type ActivityEntry struct {
	Label     string
	Detail    string
	Level     ActivityLevel
	CreatedAt time.Time
}

type ActivityLevel int

const (
	ActivityInfo ActivityLevel = iota
	ActivityPending
	ActivitySuccess
	ActivityError
)

// SubmitHandler is called when the user submits a prompt.
type SubmitHandler func(prompt string)

// ApprovalHandler is called when the user resolves an approval.
type ApprovalHandler func(request domain.ApprovalRequest, approved bool)

type chatKeyMap struct {
	Cancel  key.Binding
	NewLine key.Binding
}

var chatKeys = chatKeyMap{
	Cancel: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel"),
	),
	NewLine: key.NewBinding(
		key.WithKeys("ctrl+n"),
		key.WithHelp("ctrl+n", "new session"),
	),
}

// ChatPage is the main chat interface using a split pane layout.
type ChatPage struct {
	width  int
	height int

	splitLayout *layout.SplitPaneLayout
	messages    *messagesCmp
	editor      *editorCmp
	sidebar     *sidebarCmp

	approval *domain.ApprovalRequest
	busy     bool

	submitHandler   SubmitHandler
	approvalHandler ApprovalHandler
}

func NewChatPage(submit SubmitHandler, approval ApprovalHandler) *ChatPage {
	msgs := newMessagesCmp()
	editor := newEditorCmp()
	sidebar := newSidebarCmp()

	sp := layout.NewSplitPaneLayout(
		layout.WithLeftPanel(msgs),
		layout.WithRightPanel(sidebar),
		layout.WithBottomPanel(editor),
		layout.WithRatio(0.72),
		layout.WithVerticalRatio(0.85),
	)

	return &ChatPage{
		splitLayout:     sp,
		messages:        msgs,
		editor:          editor,
		sidebar:         sidebar,
		submitHandler:   submit,
		approvalHandler: approval,
	}
}

func (c *ChatPage) Init() tea.Cmd {
	return tea.Batch(
		c.messages.Init(),
		c.editor.Init(),
		c.sidebar.Init(),
	)
}

func (c *ChatPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		c.width = msg.Width
		c.height = msg.Height
		c.splitLayout.SetSize(msg.Width, msg.Height)
		return c, nil

	case tea.KeyMsg:
		if c.approval != nil {
			return c.handleApprovalKey(msg)
		}
		if key.Matches(msg, chatKeys.Cancel) && c.busy {
			// Esc during busy: ignore (could add cancellation later)
			return c, nil
		}

	case SubmitMsg:
		if c.submitHandler != nil {
			c.submitHandler(msg.Text)
		}
		return c, nil

	case AddTranscriptMsg:
		_, cmd := c.messages.Update(msg)
		cmds = append(cmds, cmd)
		return c, tea.Batch(cmds...)

	case UpdateTranscriptMsg:
		_, cmd := c.messages.Update(msg)
		cmds = append(cmds, cmd)
		return c, tea.Batch(cmds...)

	case SetTranscriptMsg:
		_, cmd := c.messages.Update(msg)
		cmds = append(cmds, cmd)
		return c, tea.Batch(cmds...)

	case StreamDeltaMsg:
		_, cmd := c.messages.Update(msg)
		cmds = append(cmds, cmd)
		return c, tea.Batch(cmds...)

	case StreamDoneMsg:
		_, cmd := c.messages.Update(msg)
		cmds = append(cmds, cmd)
		return c, tea.Batch(cmds...)

	case SetStatusMsg:
		// Handled by status bar, pass through
		return c, nil

	case ShowApprovalMsg:
		c.approval = &msg.Request
		return c, nil

	case ClearApprovalMsg:
		c.approval = nil
		return c, nil

	case SetTasksMsg:
		_, cmd := c.sidebar.Update(msg)
		cmds = append(cmds, cmd)
		return c, tea.Batch(cmds...)

	case AddActivityMsg:
		_, cmd := c.sidebar.Update(msg)
		cmds = append(cmds, cmd)
		return c, tea.Batch(cmds...)

	case SetBusyMsg:
		c.busy = bool(msg)
		c.messages.Update(msg)
		c.editor.Update(msg)
		return c, nil

	case SetSessionTitleMsg:
		c.sidebar.Update(msg)
		return c, nil
	}

	// Forward to sub-components
	_, editorCmd := c.editor.Update(msg)
	cmds = append(cmds, editorCmd)

	_, msgCmd := c.messages.Update(msg)
	cmds = append(cmds, msgCmd)

	return c, tea.Batch(cmds...)
}

func (c *ChatPage) handleApprovalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		if c.approvalHandler != nil && c.approval != nil {
			c.approvalHandler(*c.approval, true)
		}
		c.approval = nil
	case "n", "esc":
		if c.approvalHandler != nil && c.approval != nil {
			c.approvalHandler(*c.approval, false)
		}
		c.approval = nil
	}
	return c, nil
}

func (c *ChatPage) View() string {
	view := c.splitLayout.View()

	if c.approval != nil {
		return c.renderApprovalOverlay(view)
	}
	return view
}

func (c *ChatPage) renderApprovalOverlay(base string) string {
	return renderApproval(base, c.approval, c.width, c.height)
}

func (c *ChatPage) SetSize(width, height int) tea.Cmd {
	c.width = width
	c.height = height
	c.splitLayout.SetSize(width, height)
	return nil
}

func (c *ChatPage) GetSize() (int, int) {
	return c.width, c.height
}

func (c *ChatPage) Focus() tea.Cmd  { return nil }
func (c *ChatPage) Blur() tea.Cmd   { return nil }
func (c *ChatPage) IsFocused() bool { return true }
