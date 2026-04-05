package chat

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"crawler-ai/internal/tui/styles"
	"crawler-ai/internal/tui/theme"

	"github.com/charmbracelet/glamour"
	glamouransi "github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const maxRenderedToolResultLines = 10

func renderFramedMessageItem(message string, accent lipgloss.AdaptiveColor, width int, info ...string) string {
	contentWidth := max(8, width-2)
	parts := []string{
		styles.BaseStyle().
			Width(contentWidth).
			Render(strings.TrimSuffix(message, "\n")),
	}
	for _, extra := range info {
		if strings.TrimSpace(extra) == "" {
			continue
		}
		parts = append(parts, extra)
	}
	return styles.BaseStyle().
		Width(max(10, width-1)).
		BorderLeft(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(accent).
		PaddingLeft(1).
		Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}

func renderMarkdownFramedMessageItem(message string, accent lipgloss.AdaptiveColor, width int, info ...string) string {
	return renderFramedMessageItem(renderMarkdownContent(message, width-2), accent, width, info...)
}

func renderMessageInfoLine(text string, color lipgloss.AdaptiveColor, width int) string {
	return styles.BaseStyle().
		Width(max(8, width)).
		Foreground(color).
		Render(text)
}

func renderMarkdownContent(message string, width int) string {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return ""
	}
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(chatMarkdownStyleConfig()),
		glamour.WithWordWrap(max(8, width)),
	)
	if err != nil {
		return styles.BaseStyle().Width(max(8, width)).Render(trimmed)
	}
	rendered, err := renderer.Render(trimmed)
	if err != nil {
		return styles.BaseStyle().Width(max(8, width)).Render(trimmed)
	}
	rendered = strings.TrimSuffix(rendered, "\n")
	return styles.ForceReplaceBackgroundWithLipgloss(rendered, theme.CurrentTheme().Background())
}

func chatMarkdownStyleConfig() glamouransi.StyleConfig {
	t := theme.CurrentTheme()
	trueValue := true
	margin := uint(1)
	indent := uint(1)
	listIndent := uint(2)
	blockQuotePrefix := "│ "
	codePrefix := " "
	codeBackground := adaptiveColorString(t.BackgroundSecondary())
	codeForeground := adaptiveColorString(t.Text())
	textForeground := adaptiveColorString(t.Text())
	mutedForeground := adaptiveColorString(t.TextSecondary())
	headline := adaptiveColorString(t.Primary())
	accent := adaptiveColorString(t.Accent())
	secondary := adaptiveColorString(t.Secondary())
	errorColor := adaptiveColorString(t.Error())
	borderColor := adaptiveColorString(t.BorderNormal())

	return glamouransi.StyleConfig{
		Document: glamouransi.StyleBlock{
			StylePrimitive: glamouransi.StylePrimitive{Color: &textForeground},
		},
		BlockQuote: glamouransi.StyleBlock{
			StylePrimitive: glamouransi.StylePrimitive{Color: &mutedForeground},
			Indent:         &indent,
			IndentToken:    &blockQuotePrefix,
		},
		List: glamouransi.StyleList{LevelIndent: listIndent},
		Heading: glamouransi.StyleBlock{
			StylePrimitive: glamouransi.StylePrimitive{
				Color: &headline,
				Bold:  &trueValue,
			},
		},
		H1:          glamouransi.StyleBlock{StylePrimitive: glamouransi.StylePrimitive{Prefix: "# ", Color: &headline, Bold: &trueValue}},
		H2:          glamouransi.StyleBlock{StylePrimitive: glamouransi.StylePrimitive{Prefix: "## ", Color: &headline, Bold: &trueValue}},
		H3:          glamouransi.StyleBlock{StylePrimitive: glamouransi.StylePrimitive{Prefix: "### ", Color: &accent, Bold: &trueValue}},
		H4:          glamouransi.StyleBlock{StylePrimitive: glamouransi.StylePrimitive{Prefix: "#### ", Color: &accent, Bold: &trueValue}},
		H5:          glamouransi.StyleBlock{StylePrimitive: glamouransi.StylePrimitive{Prefix: "##### ", Color: &secondary}},
		H6:          glamouransi.StyleBlock{StylePrimitive: glamouransi.StylePrimitive{Prefix: "###### ", Color: &secondary}},
		Emph:        glamouransi.StylePrimitive{Italic: &trueValue},
		Strong:      glamouransi.StylePrimitive{Bold: &trueValue},
		Item:        glamouransi.StylePrimitive{BlockPrefix: "• ", Color: &textForeground},
		Enumeration: glamouransi.StylePrimitive{BlockPrefix: ". ", Color: &textForeground},
		Link:        glamouransi.StylePrimitive{Color: &accent, Underline: &trueValue},
		LinkText:    glamouransi.StylePrimitive{Color: &secondary, Bold: &trueValue},
		Code: glamouransi.StyleBlock{
			StylePrimitive: glamouransi.StylePrimitive{
				Prefix:          " ",
				Suffix:          " ",
				Color:           &secondary,
				BackgroundColor: &codeBackground,
			},
		},
		CodeBlock: glamouransi.StyleCodeBlock{
			StyleBlock: glamouransi.StyleBlock{
				StylePrimitive: glamouransi.StylePrimitive{
					Prefix:          codePrefix,
					Color:           &codeForeground,
					BackgroundColor: &codeBackground,
				},
				Margin: &margin,
			},
			Chroma: &glamouransi.Chroma{
				Text:              glamouransi.StylePrimitive{Color: &codeForeground},
				Comment:           glamouransi.StylePrimitive{Color: &mutedForeground},
				Keyword:           glamouransi.StylePrimitive{Color: &accent},
				KeywordType:       glamouransi.StylePrimitive{Color: &secondary},
				NameFunction:      glamouransi.StylePrimitive{Color: &headline},
				LiteralString:     glamouransi.StylePrimitive{Color: &secondary},
				LiteralNumber:     glamouransi.StylePrimitive{Color: &accent},
				Operator:          glamouransi.StylePrimitive{Color: &textForeground},
				Punctuation:       glamouransi.StylePrimitive{Color: &textForeground},
				GenericDeleted:    glamouransi.StylePrimitive{Color: &errorColor},
				GenericInserted:   glamouransi.StylePrimitive{Color: &secondary},
				Background:        glamouransi.StylePrimitive{BackgroundColor: &codeBackground},
				GenericStrong:     glamouransi.StylePrimitive{Bold: &trueValue},
				GenericEmph:       glamouransi.StylePrimitive{Italic: &trueValue},
				GenericSubheading: glamouransi.StylePrimitive{Color: &mutedForeground},
			},
		},
		Table: glamouransi.StyleTable{
			StyleBlock: glamouransi.StyleBlock{
				StylePrimitive: glamouransi.StylePrimitive{Color: &textForeground},
			},
			CenterSeparator: &blockQuotePrefix,
			ColumnSeparator: &blockQuotePrefix,
			RowSeparator:    &blockQuotePrefix,
		},
		HorizontalRule: glamouransi.StylePrimitive{Color: &borderColor, Format: "\n────────\n"},
	}
}

func adaptiveColorString(color lipgloss.AdaptiveColor) string {
	if color.Dark != "" {
		return color.Dark
	}
	return color.Light
}

func summarizeToolPayload(input string, width int) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ""
	}
	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return ansi.Truncate(strings.ReplaceAll(trimmed, "\n", " "), max(12, width), "...")
	}
	summary := summarizeStructuredValue(decoded)
	if summary == "" {
		summary = strings.ReplaceAll(trimmed, "\n", " ")
	}
	return ansi.Truncate(summary, max(12, width), "...")
}

func summarizeStructuredValue(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, key+"="+summarizeStructuredValue(typed[key]))
		}
		return strings.Join(parts, ", ")
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, summarizeStructuredValue(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case string:
		return strings.ReplaceAll(typed, "\n", " ")
	case nil:
		return "null"
	default:
		return fmt.Sprint(typed)
	}
}

func renderToolResultBody(toolName string, result string, status string, width int) string {
	trimmed := strings.TrimSpace(result)
	if trimmed == "" {
		return ""
	}
	content := truncateRenderedLines(trimmed, maxRenderedToolResultLines)
	if strings.EqualFold(strings.TrimSpace(status), "failed") {
		content = "Error: " + content
	}
	formatted, markdown := formatToolResultForDisplay(toolName, content)
	if markdown {
		return renderMarkdownContent(formatted, width)
	}
	return styleToolResultContent(toolName, formatted, width)
}

func formatToolResultForDisplay(toolName string, content string) (string, bool) {
	trimmedTool := strings.ToLower(strings.TrimSpace(toolName))
	switch trimmedTool {
	case "shell", "bash":
		return "```bash\n" + content + "\n```", true
	case "read_file", "write_file", "view", "edit", "fetch":
		return "```text\n" + content + "\n```", true
	default:
		return content, false
	}
}

func styleToolResultContent(toolName string, content string, width int) string {
	t := theme.CurrentTheme()
	trimmedTool := strings.ToLower(strings.TrimSpace(toolName))
	styled := styles.BaseStyle().Width(max(8, width)).Render(content)
	switch trimmedTool {
	case "shell", "bash", "read_file", "write_file", "fetch", "view", "edit":
		return lipgloss.NewStyle().Foreground(t.TextMuted()).Width(max(8, width)).Render(content)
	case "grep", "list_files", "glob", "ls":
		return lipgloss.NewStyle().Foreground(t.TextSecondary()).Width(max(8, width)).Render(content)
	default:
		return styled
	}
}

func truncateRenderedLines(content string, maxLines int) string {
	if maxLines <= 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	if len(lines) <= maxLines {
		return content
	}
	return strings.Join(lines[:maxLines], "\n") + "\n..."
}
