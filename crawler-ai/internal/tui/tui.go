package tui

import (
	"crawler-ai/internal/domain"
	"crawler-ai/internal/tui/components/chat"
	"crawler-ai/internal/tui/components/core"
	"crawler-ai/internal/tui/components/dialog"
	"crawler-ai/internal/tui/components/logs"
	"crawler-ai/internal/tui/layout"
	"crawler-ai/internal/tui/page"
	"crawler-ai/internal/tui/theme"
	"crawler-ai/internal/tui/util"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Re-export message types so app.go can reference them via a single package.
type (
	AddTranscriptMsg    = chat.AddTranscriptMsg
	UpdateTranscriptMsg = chat.UpdateTranscriptMsg
	SetTranscriptMsg    = chat.SetTranscriptMsg
	StreamDeltaMsg      = chat.StreamDeltaMsg
	StreamDoneMsg       = chat.StreamDoneMsg
	SetStatusMsg        = chat.SetStatusMsg
	ShowApprovalMsg     = chat.ShowApprovalMsg
	ClearApprovalMsg    = chat.ClearApprovalMsg
	SetTasksMsg         = chat.SetTasksMsg
	AddActivityMsg      = chat.AddActivityMsg
	ActivityEntry       = chat.ActivityEntry
	ActivityLevel       = chat.ActivityLevel
	SetBusyMsg          = chat.SetBusyMsg
	SetSessionTitleMsg  = chat.SetSessionTitleMsg
	SetTokenUsageMsg    = core.SetTokenUsageMsg
)

const (
	ActivityInfo    = chat.ActivityInfo
	ActivityPending = chat.ActivityPending
	ActivitySuccess = chat.ActivitySuccess
	ActivityError   = chat.ActivityError
)

// Option configures the App model.
type Option func(*App)

// WithSubmitHandler sets the callback for prompt submission.
func WithSubmitHandler(handler func(string)) Option {
	return func(a *App) {
		a.submitHandler = handler
	}
}

// WithApprovalHandler sets the callback for approval resolution.
func WithApprovalHandler(handler func(domain.ApprovalRequest, bool)) Option {
	return func(a *App) {
		a.approvalHandler = handler
	}
}

// WithSessionSwitchHandler sets the callback when the user picks a different session.
func WithSessionSwitchHandler(handler func(id string)) Option {
	return func(a *App) {
		a.sessionSwitchHandler = handler
	}
}

// WithModelSwitchHandler sets the callback when the user picks a different model.
func WithModelSwitchHandler(handler func(provider, model string)) Option {
	return func(a *App) {
		a.modelSwitchHandler = handler
	}
}

type activeDialog int

const (
	dialogNone activeDialog = iota
	dialogHelp
	dialogQuit
	dialogSession
	dialogTheme
	dialogCommands
	dialogModel
)

// App is the top-level Bubble Tea model for the TUI.
type App struct {
	width  int
	height int

	activePage   page.PageID
	activeDialog activeDialog

	chatPage  *chat.ChatPage
	logsPage  *logs.LogsPage
	statusBar *core.StatusBar

	helpDialog     *dialog.HelpDialog
	quitDialog     *dialog.QuitDialog
	sessionDialog  *dialog.SessionDialog
	themeDialog    *dialog.ThemeDialog
	commandsDialog *dialog.CommandsDialog
	modelDialog    *dialog.ModelDialog

	submitHandler        func(string)
	approvalHandler      func(domain.ApprovalRequest, bool)
	sessionSwitchHandler func(id string)
	modelSwitchHandler   func(provider, model string)
}

// NewApp creates a new TUI application model.
func NewApp(options ...Option) App {
	a := App{
		activePage: page.ChatPage,

		statusBar: core.NewStatusBar(),
		logsPage:  logs.NewLogsPage(),

		helpDialog:    dialog.NewHelpDialog(),
		quitDialog:    dialog.NewQuitDialog(),
		themeDialog:   dialog.NewThemeDialog(),
		sessionDialog: dialog.NewSessionDialog(nil),
		modelDialog:   dialog.NewModelDialog(nil),
	}

	for _, opt := range options {
		opt(&a)
	}

	a.chatPage = chat.NewChatPage(
		func(prompt string) {
			if a.submitHandler != nil {
				a.submitHandler(prompt)
			}
		},
		func(req domain.ApprovalRequest, approved bool) {
			if a.approvalHandler != nil {
				a.approvalHandler(req, approved)
			}
		},
	)

	a.commandsDialog = dialog.NewCommandsDialog(a.buildCommands())

	return a
}

func (a App) Init() tea.Cmd {
	return tea.Batch(
		a.chatPage.Init(),
		a.logsPage.Init(),
		a.statusBar.Init(),
	)
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.propagateSize()

	case tea.KeyMsg:
		// If a dialog is active, route keys there first.
		if a.activeDialog != dialogNone {
			return a.updateActiveDialog(msg)
		}

		switch msg.String() {
		case "ctrl+c":
			a.activeDialog = dialogQuit
			a.propagateSize()
			return a, nil
		case "ctrl+h":
			a.toggleDialog(dialogHelp)
			return a, nil
		case "ctrl+l":
			if a.activePage == page.ChatPage {
				a.activePage = page.LogsPage
			} else {
				a.activePage = page.ChatPage
			}
			return a, nil
		case "ctrl+t":
			a.toggleDialog(dialogTheme)
			return a, nil
		case "ctrl+k":
			a.commandsDialog = dialog.NewCommandsDialog(a.buildCommands())
			a.toggleDialog(dialogCommands)
			return a, nil
		case "ctrl+o":
			a.toggleDialog(dialogModel)
			return a, nil
		}

	// Dialog result messages
	case dialog.QuitConfirmMsg:
		return a, tea.Quit
	case dialog.QuitCancelMsg:
		a.activeDialog = dialogNone
		return a, nil
	case dialog.ShowHelpMsg:
		if !msg.Show {
			a.activeDialog = dialogNone
		}
		return a, nil
	case dialog.ShowSessionDialogMsg:
		if !msg.Show {
			a.activeDialog = dialogNone
		}
		return a, nil
	case dialog.SessionSelectedMsg:
		a.activeDialog = dialogNone
		if a.sessionSwitchHandler != nil {
			a.sessionSwitchHandler(msg.ID)
		}
		return a, nil
	case dialog.ShowThemeDialogMsg:
		if !msg.Show {
			a.activeDialog = dialogNone
		}
		return a, nil
	case dialog.ThemeSelectedMsg:
		theme.SetTheme(msg.Name)
		a.activeDialog = dialogNone
		return a, nil
	case dialog.ShowCommandsMsg:
		if !msg.Show {
			a.activeDialog = dialogNone
		}
		return a, nil
	case dialog.ShowModelDialogMsg:
		if !msg.Show {
			a.activeDialog = dialogNone
		}
		return a, nil
	case dialog.ModelSelectedMsg:
		a.activeDialog = dialogNone
		if a.modelSwitchHandler != nil {
			a.modelSwitchHandler(msg.Provider, msg.Model)
		}
		return a, nil

	// Page change
	case page.PageChangeMsg:
		a.activePage = msg.ID
		return a, nil

	// Status bar info messages (from util)
	case util.InfoMsg:
		_, cmd := a.statusBar.Update(msg)
		cmds = append(cmds, cmd)
		return a, tea.Batch(cmds...)
	case util.ClearStatusMsg:
		a.statusBar.Update(msg)
		return a, nil

	// Convert chat.SetStatusMsg to util.InfoMsg for the status bar
	case chat.SetStatusMsg:
		infoMsg := util.InfoMsg{Type: util.InfoTypeInfo, Msg: msg.Status}
		_, cmd := a.statusBar.Update(infoMsg)
		cmds = append(cmds, cmd)
	}

	// Route to status bar
	_, statusCmd := a.statusBar.Update(msg)
	if statusCmd != nil {
		cmds = append(cmds, statusCmd)
	}

	// Route to active page
	switch a.activePage {
	case page.ChatPage:
		_, chatCmd := a.chatPage.Update(msg)
		if chatCmd != nil {
			cmds = append(cmds, chatCmd)
		}
	case page.LogsPage:
		_, logsCmd := a.logsPage.Update(msg)
		if logsCmd != nil {
			cmds = append(cmds, logsCmd)
		}
	}

	return a, tea.Batch(cmds...)
}

func (a App) View() string {
	t := theme.CurrentTheme()

	if a.width == 0 || a.height == 0 {
		return ""
	}

	// Main layout: page + status bar at bottom (1 line).
	statusView := a.statusBar.View()
	statusHeight := lipgloss.Height(statusView)

	pageHeight := a.height - statusHeight
	if pageHeight < 1 {
		pageHeight = 1
	}

	var pageView string
	switch a.activePage {
	case page.ChatPage:
		pageView = a.chatPage.View()
	case page.LogsPage:
		pageView = a.logsPage.View()
	}

	pageView = lipgloss.NewStyle().
		Width(a.width).
		Height(pageHeight).
		Background(t.Background()).
		Render(pageView)

	base := lipgloss.JoinVertical(lipgloss.Left, pageView, statusView)

	// Overlay active dialog on top of the page using PlaceOverlay
	if a.activeDialog != dialogNone {
		dialogView := a.renderDialogContent()
		if dialogView != "" {
			dW := lipgloss.Width(dialogView)
			dH := lipgloss.Height(dialogView)
			x := (a.width - dW) / 2
			y := (a.height - dH) / 2
			if x < 0 {
				x = 0
			}
			if y < 0 {
				y = 0
			}
			base = layout.PlaceOverlay(x, y, dialogView, base, true)
		}
	}

	return base
}

func (a *App) propagateSize() {
	pageHeight := a.height - 1
	if pageHeight < 1 {
		pageHeight = 1
	}

	a.chatPage.SetSize(a.width, pageHeight)
	a.logsPage.Update(tea.WindowSizeMsg{Width: a.width, Height: pageHeight})
	a.statusBar.SetWidth(a.width)
}

func (a *App) toggleDialog(d activeDialog) {
	if a.activeDialog == d {
		a.activeDialog = dialogNone
	} else {
		a.activeDialog = d
	}
}

func (a App) updateActiveDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch a.activeDialog {
	case dialogHelp:
		if msg.String() == "esc" || msg.String() == "ctrl+h" {
			a.activeDialog = dialogNone
			return a, nil
		}
		_, cmd = a.helpDialog.Update(msg)
	case dialogQuit:
		_, cmd = a.quitDialog.Update(msg)
	case dialogSession:
		_, cmd = a.sessionDialog.Update(msg)
	case dialogTheme:
		_, cmd = a.themeDialog.Update(msg)
	case dialogCommands:
		_, cmd = a.commandsDialog.Update(msg)
	case dialogModel:
		_, cmd = a.modelDialog.Update(msg)
	}
	return a, cmd
}

// renderDialogContent returns JUST the dialog box content (not full-screen).
func (a App) renderDialogContent() string {
	switch a.activeDialog {
	case dialogHelp:
		return a.helpDialog.RenderBox()
	case dialogQuit:
		return a.quitDialog.RenderBox()
	case dialogSession:
		return a.sessionDialog.RenderBox()
	case dialogTheme:
		return a.themeDialog.RenderBox()
	case dialogCommands:
		return a.commandsDialog.RenderBox()
	case dialogModel:
		return a.modelDialog.RenderBox()
	default:
		return ""
	}
}

func (a *App) buildCommands() []dialog.CommandEntry {
	return []dialog.CommandEntry{
		{Label: "Toggle Help", Shortcut: "ctrl+h", Action: func() tea.Msg { return dialog.ShowHelpMsg{Show: true} }},
		{Label: "View Logs", Shortcut: "ctrl+l", Action: func() tea.Msg { return page.PageChangeMsg{ID: page.LogsPage} }},
		{Label: "View Chat", Shortcut: "ctrl+l", Action: func() tea.Msg { return page.PageChangeMsg{ID: page.ChatPage} }},
		{Label: "Switch Theme", Shortcut: "ctrl+t", Action: func() tea.Msg { return dialog.ShowThemeDialogMsg{Show: true} }},
		{Label: "Switch Model", Shortcut: "ctrl+o", Action: func() tea.Msg { return dialog.ShowModelDialogMsg{Show: true} }},
		{Label: "Sessions", Shortcut: "", Action: func() tea.Msg { return dialog.ShowSessionDialogMsg{Show: true} }},
		{Label: "Quit", Shortcut: "ctrl+c", Action: func() tea.Msg { return dialog.QuitRequestMsg{} }},
	}
}
