package theme

import (
	"sync"

	"github.com/charmbracelet/lipgloss"
)

// Theme defines the color contract for the TUI.
type Theme interface {
	Name() string

	Primary() lipgloss.AdaptiveColor
	Secondary() lipgloss.AdaptiveColor
	Accent() lipgloss.AdaptiveColor

	Error() lipgloss.AdaptiveColor
	Warning() lipgloss.AdaptiveColor
	Success() lipgloss.AdaptiveColor
	Info() lipgloss.AdaptiveColor

	Text() lipgloss.AdaptiveColor
	TextSecondary() lipgloss.AdaptiveColor
	TextMuted() lipgloss.AdaptiveColor
	TextEmphasized() lipgloss.AdaptiveColor

	Background() lipgloss.AdaptiveColor
	BackgroundSecondary() lipgloss.AdaptiveColor
	BackgroundDarker() lipgloss.AdaptiveColor

	BorderNormal() lipgloss.AdaptiveColor
	BorderFocused() lipgloss.AdaptiveColor
	BorderDim() lipgloss.AdaptiveColor
}

// BaseTheme provides a concrete implementation that other themes embed.
type BaseTheme struct {
	ThemeName                string
	PrimaryColor             lipgloss.AdaptiveColor
	SecondaryColor           lipgloss.AdaptiveColor
	AccentColor              lipgloss.AdaptiveColor
	ErrorColor               lipgloss.AdaptiveColor
	WarningColor             lipgloss.AdaptiveColor
	SuccessColor             lipgloss.AdaptiveColor
	InfoColor                lipgloss.AdaptiveColor
	TextColor                lipgloss.AdaptiveColor
	TextSecondaryColor       lipgloss.AdaptiveColor
	TextMutedColor           lipgloss.AdaptiveColor
	TextEmphasizedColor      lipgloss.AdaptiveColor
	BackgroundColor          lipgloss.AdaptiveColor
	BackgroundSecondaryColor lipgloss.AdaptiveColor
	BackgroundDarkerColor    lipgloss.AdaptiveColor
	BorderNormalColor        lipgloss.AdaptiveColor
	BorderFocusedColor       lipgloss.AdaptiveColor
	BorderDimColor           lipgloss.AdaptiveColor
}

func (t *BaseTheme) Name() string                                { return t.ThemeName }
func (t *BaseTheme) Primary() lipgloss.AdaptiveColor             { return t.PrimaryColor }
func (t *BaseTheme) Secondary() lipgloss.AdaptiveColor           { return t.SecondaryColor }
func (t *BaseTheme) Accent() lipgloss.AdaptiveColor              { return t.AccentColor }
func (t *BaseTheme) Error() lipgloss.AdaptiveColor               { return t.ErrorColor }
func (t *BaseTheme) Warning() lipgloss.AdaptiveColor             { return t.WarningColor }
func (t *BaseTheme) Success() lipgloss.AdaptiveColor             { return t.SuccessColor }
func (t *BaseTheme) Info() lipgloss.AdaptiveColor                { return t.InfoColor }
func (t *BaseTheme) Text() lipgloss.AdaptiveColor                { return t.TextColor }
func (t *BaseTheme) TextSecondary() lipgloss.AdaptiveColor       { return t.TextSecondaryColor }
func (t *BaseTheme) TextMuted() lipgloss.AdaptiveColor           { return t.TextMutedColor }
func (t *BaseTheme) TextEmphasized() lipgloss.AdaptiveColor      { return t.TextEmphasizedColor }
func (t *BaseTheme) Background() lipgloss.AdaptiveColor          { return t.BackgroundColor }
func (t *BaseTheme) BackgroundSecondary() lipgloss.AdaptiveColor { return t.BackgroundSecondaryColor }
func (t *BaseTheme) BackgroundDarker() lipgloss.AdaptiveColor    { return t.BackgroundDarkerColor }
func (t *BaseTheme) BorderNormal() lipgloss.AdaptiveColor        { return t.BorderNormalColor }
func (t *BaseTheme) BorderFocused() lipgloss.AdaptiveColor       { return t.BorderFocusedColor }
func (t *BaseTheme) BorderDim() lipgloss.AdaptiveColor           { return t.BorderDimColor }

var (
	mu      sync.RWMutex
	current Theme = DefaultTheme()
)

func CurrentTheme() Theme {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// SetTheme sets the current theme by name. If the name is unknown, it is a no-op.
func SetTheme(name string) {
	mu.Lock()
	defer mu.Unlock()
	for _, t := range themeRegistry() {
		if t.Name() == name {
			current = t
			return
		}
	}
}

// AvailableThemes returns the names of all registered themes.
func AvailableThemes() []string {
	names := make([]string, 0, 3)
	for _, t := range themeRegistry() {
		names = append(names, t.Name())
	}
	return names
}

func themeRegistry() []Theme {
	return []Theme{
		DefaultTheme(),
		CatppuccinTheme(),
		TokyoNightTheme(),
	}
}

// DefaultTheme — crawler-ai brand theme (dark, cyan/blue accent)
func DefaultTheme() Theme {
	return &BaseTheme{
		ThemeName:                "crawler",
		PrimaryColor:             lipgloss.AdaptiveColor{Light: "#0066CC", Dark: "#58a6ff"},
		SecondaryColor:           lipgloss.AdaptiveColor{Light: "#6e40c9", Dark: "#bc8cff"},
		AccentColor:              lipgloss.AdaptiveColor{Light: "#0891b2", Dark: "#22d3ee"},
		ErrorColor:               lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f85149"},
		WarningColor:             lipgloss.AdaptiveColor{Light: "#bf8700", Dark: "#d29922"},
		SuccessColor:             lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#3fb950"},
		InfoColor:                lipgloss.AdaptiveColor{Light: "#0550ae", Dark: "#58a6ff"},
		TextColor:                lipgloss.AdaptiveColor{Light: "#1F2328", Dark: "#e6edf3"},
		TextSecondaryColor:       lipgloss.AdaptiveColor{Light: "#57606a", Dark: "#8b949e"},
		TextMutedColor:           lipgloss.AdaptiveColor{Light: "#656d76", Dark: "#7d8590"},
		TextEmphasizedColor:      lipgloss.AdaptiveColor{Light: "#1F2328", Dark: "#ffffff"},
		BackgroundColor:          lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#0d1117"},
		BackgroundSecondaryColor: lipgloss.AdaptiveColor{Light: "#f6f8fa", Dark: "#161b22"},
		BackgroundDarkerColor:    lipgloss.AdaptiveColor{Light: "#eaeef2", Dark: "#010409"},
		BorderNormalColor:        lipgloss.AdaptiveColor{Light: "#d0d7de", Dark: "#30363d"},
		BorderFocusedColor:       lipgloss.AdaptiveColor{Light: "#0066CC", Dark: "#58a6ff"},
		BorderDimColor:           lipgloss.AdaptiveColor{Light: "#eaeef2", Dark: "#21262d"},
	}
}

// CatppuccinTheme — Catppuccin Mocha palette
func CatppuccinTheme() Theme {
	return &BaseTheme{
		ThemeName:                "catppuccin",
		PrimaryColor:             lipgloss.AdaptiveColor{Light: "#8839ef", Dark: "#cba6f7"},
		SecondaryColor:           lipgloss.AdaptiveColor{Light: "#1e66f5", Dark: "#89b4fa"},
		AccentColor:              lipgloss.AdaptiveColor{Light: "#179299", Dark: "#94e2d5"},
		ErrorColor:               lipgloss.AdaptiveColor{Light: "#d20f39", Dark: "#f38ba8"},
		WarningColor:             lipgloss.AdaptiveColor{Light: "#df8e1d", Dark: "#fab387"},
		SuccessColor:             lipgloss.AdaptiveColor{Light: "#40a02b", Dark: "#a6e3a1"},
		InfoColor:                lipgloss.AdaptiveColor{Light: "#1e66f5", Dark: "#89b4fa"},
		TextColor:                lipgloss.AdaptiveColor{Light: "#4c4f69", Dark: "#cdd6f4"},
		TextSecondaryColor:       lipgloss.AdaptiveColor{Light: "#6c6f85", Dark: "#a6adc8"},
		TextMutedColor:           lipgloss.AdaptiveColor{Light: "#8c8fa1", Dark: "#6c7086"},
		TextEmphasizedColor:      lipgloss.AdaptiveColor{Light: "#4c4f69", Dark: "#ffffff"},
		BackgroundColor:          lipgloss.AdaptiveColor{Light: "#eff1f5", Dark: "#1e1e2e"},
		BackgroundSecondaryColor: lipgloss.AdaptiveColor{Light: "#e6e9ef", Dark: "#181825"},
		BackgroundDarkerColor:    lipgloss.AdaptiveColor{Light: "#dce0e8", Dark: "#11111b"},
		BorderNormalColor:        lipgloss.AdaptiveColor{Light: "#ccd0da", Dark: "#313244"},
		BorderFocusedColor:       lipgloss.AdaptiveColor{Light: "#8839ef", Dark: "#cba6f7"},
		BorderDimColor:           lipgloss.AdaptiveColor{Light: "#e6e9ef", Dark: "#1e1e2e"},
	}
}

// TokyoNightTheme — Tokyo Night Storm palette
func TokyoNightTheme() Theme {
	return &BaseTheme{
		ThemeName:                "tokyonight",
		PrimaryColor:             lipgloss.AdaptiveColor{Light: "#2e7de9", Dark: "#7aa2f7"},
		SecondaryColor:           lipgloss.AdaptiveColor{Light: "#9854f1", Dark: "#bb9af7"},
		AccentColor:              lipgloss.AdaptiveColor{Light: "#007197", Dark: "#7dcfff"},
		ErrorColor:               lipgloss.AdaptiveColor{Light: "#f52a65", Dark: "#f7768e"},
		WarningColor:             lipgloss.AdaptiveColor{Light: "#8c6c3e", Dark: "#e0af68"},
		SuccessColor:             lipgloss.AdaptiveColor{Light: "#587539", Dark: "#9ece6a"},
		InfoColor:                lipgloss.AdaptiveColor{Light: "#2e7de9", Dark: "#7aa2f7"},
		TextColor:                lipgloss.AdaptiveColor{Light: "#3760bf", Dark: "#c0caf5"},
		TextSecondaryColor:       lipgloss.AdaptiveColor{Light: "#6172b0", Dark: "#9aa5ce"},
		TextMutedColor:           lipgloss.AdaptiveColor{Light: "#8990b3", Dark: "#565f89"},
		TextEmphasizedColor:      lipgloss.AdaptiveColor{Light: "#3760bf", Dark: "#ffffff"},
		BackgroundColor:          lipgloss.AdaptiveColor{Light: "#d5d6db", Dark: "#24283b"},
		BackgroundSecondaryColor: lipgloss.AdaptiveColor{Light: "#c4c8da", Dark: "#1f2335"},
		BackgroundDarkerColor:    lipgloss.AdaptiveColor{Light: "#b4b5b9", Dark: "#1a1b26"},
		BorderNormalColor:        lipgloss.AdaptiveColor{Light: "#a9b1d6", Dark: "#3b4261"},
		BorderFocusedColor:       lipgloss.AdaptiveColor{Light: "#2e7de9", Dark: "#7aa2f7"},
		BorderDimColor:           lipgloss.AdaptiveColor{Light: "#c4c8da", Dark: "#292e42"},
	}
}
