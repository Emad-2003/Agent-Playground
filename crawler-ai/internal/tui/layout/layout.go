package layout

import (
	"reflect"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// Focusable is a component that can receive and lose focus.
type Focusable interface {
	Focus() tea.Cmd
	Blur() tea.Cmd
	IsFocused() bool
}

// Sizeable is a component with mutable dimensions.
type Sizeable interface {
	SetSize(width, height int) tea.Cmd
	GetSize() (int, int)
}

// Bindings exposes key bindings for help display.
type Bindings interface {
	BindingKeys() []key.Binding
}

// KeyMapToSlice converts a struct of key.Binding fields to a slice.
func KeyMapToSlice(keyMap interface{}) []key.Binding {
	v := reflect.ValueOf(keyMap)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	bindings := make([]key.Binding, 0)
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if b, ok := field.Interface().(key.Binding); ok {
			bindings = append(bindings, b)
		}
	}
	return bindings
}
