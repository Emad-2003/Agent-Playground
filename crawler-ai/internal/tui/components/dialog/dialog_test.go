package dialog

import (
	"testing"

	"crawler-ai/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSessionDialogSelectsCurrentListItem(t *testing.T) {
	dialog := NewSessionDialog([]SessionEntry{{ID: "s1", Label: "one"}, {ID: "s2", Label: "two"}})
	_, _ = dialog.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, cmd := dialog.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected selection command")
	}
	msg := cmd()
	selected, ok := msg.(SessionSelectedMsg)
	if !ok || selected.ID != "s2" {
		t.Fatalf("unexpected selection: %#v", msg)
	}
}

func TestCommandsDialogFiltersAndRunsSelectedAction(t *testing.T) {
	dialog := NewCommandsDialog([]CommandEntry{
		{Label: "alpha", Action: func() tea.Msg { return ThemeSelectedMsg{Name: "alpha"} }},
		{Label: "beta", Action: func() tea.Msg { return ThemeSelectedMsg{Name: "beta"} }},
	})
	_, _ = dialog.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	_, cmd := dialog.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected action command")
	}
	msg := cmd()
	selected, ok := msg.(ThemeSelectedMsg)
	if !ok || selected.Name != "beta" {
		t.Fatalf("unexpected command action: %#v", msg)
	}
}

func TestThemeDialogUsesSharedListNavigation(t *testing.T) {
	theme.SetTheme("crawler")
	names := theme.AvailableThemes()
	if len(names) < 2 {
		t.Fatal("expected at least two themes")
	}
	dialog := NewThemeDialog()
	_, _ = dialog.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, cmd := dialog.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected selection command")
	}
	msg := cmd()
	selected, ok := msg.(ThemeSelectedMsg)
	if !ok || selected.Name != names[1] {
		t.Fatalf("unexpected theme selection: %#v", msg)
	}
}

func TestModelDialogSelectsSharedListItem(t *testing.T) {
	dialog := NewModelDialog([]ModelEntry{{Provider: "openai", Model: "gpt-4.1", Current: true}, {Provider: "anthropic", Model: "claude-sonnet"}})
	_, _ = dialog.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, cmd := dialog.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected selection command")
	}
	msg := cmd()
	selected, ok := msg.(ModelSelectedMsg)
	if !ok || selected.Provider != "anthropic" || selected.Model != "claude-sonnet" {
		t.Fatalf("unexpected model selection: %#v", msg)
	}
}
