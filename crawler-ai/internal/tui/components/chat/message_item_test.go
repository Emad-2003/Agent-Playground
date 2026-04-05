package chat

import (
	"regexp"
	"strings"
	"testing"

	"crawler-ai/internal/tui/theme"
)

var messageItemANSISequence = regexp.MustCompile("\x1b\\[[0-9;]*m")

func TestRenderFramedMessageItemIncludesContentAndInfo(t *testing.T) {
	theme.SetTheme("crawler")
	rendered := renderFramedMessageItem("hello", theme.CurrentTheme().Primary(), 40, renderMessageInfoLine("meta", theme.CurrentTheme().TextMuted(), 40))
	for _, expected := range []string{"hello", "meta"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected rendered frame to contain %q, got %q", expected, rendered)
		}
	}
}

func TestRenderMarkdownFramedMessageItemRendersMarkdownSyntax(t *testing.T) {
	theme.SetTheme("crawler")
	rendered := renderMarkdownFramedMessageItem("# Title\n\n- item", theme.CurrentTheme().Primary(), 60)
	for _, expected := range []string{"Title", "item"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected markdown render to contain %q, got %q", expected, rendered)
		}
	}
	if strings.Contains(rendered, "# Title") {
		t.Fatalf("expected markdown syntax to be rendered, got raw heading in %q", rendered)
	}
}

func TestRenderMarkdownFramedMessageItemRendersCodeAndTables(t *testing.T) {
	theme.SetTheme("crawler")
	rendered := renderMarkdownFramedMessageItem("| A | B |\n| - | - |\n| 1 | 2 |\n\n```go\nfmt.Println(\"hi\")\n```", theme.CurrentTheme().Primary(), 80)
	plain := messageItemANSISequence.ReplaceAllString(rendered, "")
	for _, expected := range []string{"A", "B", "1", "2", "fmt.Println(\"hi\")"} {
		if !strings.Contains(plain, expected) {
			t.Fatalf("expected markdown render to contain %q, got %q", expected, plain)
		}
	}
	if strings.Contains(plain, "```go") {
		t.Fatalf("expected code fence to be rendered, got raw fence in %q", plain)
	}
}

func TestSummarizeToolPayloadStableOrdering(t *testing.T) {
	summary := summarizeToolPayload(`{"path":"README.md","pattern":"TODO"}`, 80)
	if summary != "path=README.md, pattern=TODO" {
		t.Fatalf("unexpected payload summary: %q", summary)
	}
}

func TestRenderToolResultBodyTruncatesLongOutput(t *testing.T) {
	result := strings.Join([]string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12"}, "\n")
	rendered := renderToolResultBody("shell", result, "completed", 60)
	for _, expected := range []string{"1", "10", "..."} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected rendered body to contain %q, got %q", expected, rendered)
		}
	}
	if strings.Contains(rendered, "12") {
		t.Fatalf("expected truncated output to omit final lines, got %q", rendered)
	}
}

func TestRenderToolResultBodyMarksErrors(t *testing.T) {
	rendered := renderToolResultBody("grep", "boom", "failed", 40)
	if !strings.Contains(rendered, "Error: boom") {
		t.Fatalf("expected error prefix, got %q", rendered)
	}
}

func TestFormatToolResultForDisplayUsesMarkdownForShell(t *testing.T) {
	formatted, markdown := formatToolResultForDisplay("shell", "echo hi")
	if !markdown {
		t.Fatal("expected shell output to use markdown formatting")
	}
	if !strings.Contains(formatted, "```bash") || !strings.Contains(formatted, "echo hi") {
		t.Fatalf("unexpected shell formatting: %q", formatted)
	}
}

func TestFormatToolResultForDisplayKeepsPlainGrepOutput(t *testing.T) {
	formatted, markdown := formatToolResultForDisplay("grep", "README.md:1: TODO")
	if markdown {
		t.Fatal("expected grep output to stay plain")
	}
	if formatted != "README.md:1: TODO" {
		t.Fatalf("unexpected grep formatting: %q", formatted)
	}
}

func TestFormatToolResultForDisplayUsesMarkdownForReadFile(t *testing.T) {
	formatted, markdown := formatToolResultForDisplay("read_file", "package main")
	if !markdown {
		t.Fatal("expected read_file output to use markdown formatting")
	}
	if !strings.Contains(formatted, "```text") || !strings.Contains(formatted, "package main") {
		t.Fatalf("unexpected read_file formatting: %q", formatted)
	}
}
