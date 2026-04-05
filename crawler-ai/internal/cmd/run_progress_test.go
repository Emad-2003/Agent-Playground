package cmd

import (
	"bytes"
	"strings"
	"testing"

	"crawler-ai/internal/app"
)

func TestRunProgressRendererDeduplicatesStatusAndReasoning(t *testing.T) {
	t.Parallel()

	var buffer bytes.Buffer
	renderer := newRunProgressRenderer(&buffer)

	renderer.Handle(app.NonInteractiveProgressEvent{Kind: app.NonInteractiveProgressStatus, Timestamp: "2026-04-05T12:00:00Z", Message: "Starting run"})
	renderer.Handle(app.NonInteractiveProgressEvent{Kind: app.NonInteractiveProgressStatus, Timestamp: "2026-04-05T12:00:01Z", Message: "Starting run"})
	renderer.Handle(app.NonInteractiveProgressEvent{Kind: app.NonInteractiveProgressReasoning, Timestamp: "2026-04-05T12:00:02Z", ID: "reasoning-1", Message: "Inspecting"})
	renderer.Handle(app.NonInteractiveProgressEvent{Kind: app.NonInteractiveProgressReasoning, Timestamp: "2026-04-05T12:00:03Z", ID: "reasoning-1", Message: "Inspecting files"})

	output := buffer.String()
	if strings.Count(output, "status") != 1 {
		t.Fatalf("expected one status line, got %q", output)
	}
	if !strings.Contains(output, "think    Inspecting") || !strings.Contains(output, "think    files") {
		t.Fatalf("expected reasoning delta output, got %q", output)
	}
}

func TestRunProgressRendererFormatsToolAndUsageEvents(t *testing.T) {
	t.Parallel()

	var buffer bytes.Buffer
	renderer := newRunProgressRenderer(&buffer)

	renderer.Handle(app.NonInteractiveProgressEvent{Kind: app.NonInteractiveProgressToolStarted, Timestamp: "2026-04-05T12:01:00Z", Tool: "write_file", Path: "index.html"})
	renderer.Handle(app.NonInteractiveProgressEvent{Kind: app.NonInteractiveProgressTokenUsage, Timestamp: "2026-04-05T12:01:01Z", InputTokens: 1200, OutputTokens: 450, TotalCost: 0.0123})

	output := buffer.String()
	if !strings.Contains(output, "tool     start write_file index.html") {
		t.Fatalf("expected tool progress line, got %q", output)
	}
	if !strings.Contains(output, "usage    in=1.2K out=450 cost=$0.0123") {
		t.Fatalf("expected usage progress line, got %q", output)
	}
}

func TestRootCommandUsesCrwlrName(t *testing.T) {
	t.Parallel()

	if rootCmd.Use != "crwlr" {
		t.Fatalf("expected root command name to be crwlr, got %q", rootCmd.Use)
	}
}
