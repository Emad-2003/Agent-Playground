package util

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type InfoType int

const (
	InfoTypeInfo InfoType = iota
	InfoTypeWarn
	InfoTypeError
)

// InfoMsg is a status bar message with optional TTL.
type InfoMsg struct {
	Type InfoType
	Msg  string
	TTL  time.Duration
}

// ClearStatusMsg clears the status bar message.
type ClearStatusMsg struct{}

// CmdHandler wraps a message as a Cmd.
func CmdHandler(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
}

// ReportError creates a command that shows an error in the status bar.
func ReportError(err error) tea.Cmd {
	return CmdHandler(InfoMsg{Type: InfoTypeError, Msg: err.Error()})
}

// ReportInfo creates a command that shows an info message.
func ReportInfo(msg string) tea.Cmd {
	return CmdHandler(InfoMsg{Type: InfoTypeInfo, Msg: msg})
}

// ReportWarn creates a command that shows a warning message.
func ReportWarn(msg string) tea.Cmd {
	return CmdHandler(InfoMsg{Type: InfoTypeWarn, Msg: msg})
}

// Clamp restricts v to the range [lo, hi].
func Clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
