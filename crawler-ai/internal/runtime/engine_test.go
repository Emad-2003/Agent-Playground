package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "crawler-ai/internal/errors"
	"crawler-ai/internal/events"
)

func TestExecuteWriteFilePublishesEvents(t *testing.T) {
	t.Parallel()

	bus := events.NewBus()
	started := make(chan struct{}, 1)
	completed := make(chan struct{}, 1)
	bus.Subscribe(EventToolStarted, func(event events.Event) {
		payload, _ := event.Payload.(map[string]any)
		if payload["call_id"] != "call-1" {
			t.Errorf("expected call_id in started payload, got %#v", payload)
		}
		started <- struct{}{}
	})
	bus.Subscribe(EventToolCompleted, func(event events.Event) {
		payload, _ := event.Payload.(map[string]any)
		if payload["call_id"] != "call-1" {
			t.Errorf("expected call_id in completed payload, got %#v", payload)
		}
		completed <- struct{}{}
	})

	engine, err := NewEngine(t.TempDir(), bus)
	if err != nil {
		t.Fatalf("unexpected engine error: %v", err)
	}

	_, err = engine.Execute(context.Background(), ToolRequest{
		CallID:  "call-1",
		Name:    "write_file",
		Path:    "notes.txt",
		Content: "hello",
	})
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}

	<-started
	<-completed
}

func TestExecuteEditFilePersistsHistoryMetadata(t *testing.T) {
	t.Parallel()

	engine, err := NewEngine(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("unexpected engine error: %v", err)
	}
	if _, err := engine.Execute(context.Background(), ToolRequest{Name: "write_file", Path: "notes.txt", Content: "hello"}); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	result, err := engine.Execute(context.Background(), ToolRequest{Name: "edit_file", Path: "notes.txt", OldText: "hello", NewText: "hello world"})
	if err != nil {
		t.Fatalf("unexpected edit error: %v", err)
	}
	if result.Path != "notes.txt" {
		t.Fatalf("unexpected result path: %#v", result)
	}
	if !strings.Contains(result.Output, ".crawler-ai") {
		t.Fatalf("expected history path in output, got %q", result.Output)
	}
}

func TestExecuteFetch(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello fetch"))
	}))
	defer server.Close()
	engine, err := NewEngine(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("unexpected engine error: %v", err)
	}
	result, err := engine.Execute(context.Background(), ToolRequest{Name: "fetch", URL: server.URL})
	if err != nil {
		t.Fatalf("unexpected fetch error: %v", err)
	}
	if !strings.Contains(result.Output, "hello fetch") {
		t.Fatalf("expected fetch output, got %q", result.Output)
	}
}

func TestExecuteGlobAndView(t *testing.T) {
	t.Parallel()
	engine, err := NewEngine(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("unexpected engine error: %v", err)
	}
	if _, err := engine.Execute(context.Background(), ToolRequest{Name: "write_file", Path: "src/main.go", Content: "package main\n\nfunc main() {}"}); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	globbed, err := engine.Execute(context.Background(), ToolRequest{Name: "glob", Pattern: "src/**/*.go"})
	if err != nil {
		t.Fatalf("unexpected glob error: %v", err)
	}
	if !strings.Contains(globbed.Output, "src/main.go") {
		t.Fatalf("expected glob output to include file, got %q", globbed.Output)
	}
	viewed, err := engine.Execute(context.Background(), ToolRequest{Name: "view", Path: "src/main.go", StartLine: 1, Limit: 1})
	if err != nil {
		t.Fatalf("unexpected view error: %v", err)
	}
	if !strings.Contains(viewed.Output, "     1|package main") {
		t.Fatalf("expected line-numbered view output, got %q", viewed.Output)
	}
	if viewed.Path != "src/main.go" {
		t.Fatalf("expected view path, got %#v", viewed)
	}
}

func TestExecuteGrep(t *testing.T) {
	t.Parallel()
	engine, err := NewEngine(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("unexpected engine error: %v", err)
	}
	if _, err := engine.Execute(context.Background(), ToolRequest{Name: "write_file", Path: "src/app.txt", Content: "alpha\nbeta"}); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	result, err := engine.Execute(context.Background(), ToolRequest{Name: "grep", Pattern: "alpha"})
	if err != nil {
		t.Fatalf("unexpected grep error: %v", err)
	}
	if !strings.Contains(result.Output, "Found 1 matches") || !strings.Contains(result.Output, "src/app.txt") {
		t.Fatalf("expected grouped grep output, got %q", result.Output)
	}
}

func TestExecuteBackgroundShellLifecycle(t *testing.T) {
	engine, err := NewEngine(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("unexpected engine error: %v", err)
	}
	started, err := engine.Execute(context.Background(), ToolRequest{Name: "shell_bg", Command: "echo hello bg"})
	if err != nil {
		t.Fatalf("unexpected background start error: %v", err)
	}
	jobID := started.Extra["job_id"]
	if jobID == "" {
		t.Fatalf("expected job id, got %#v", started)
	}
	output, err := engine.Execute(context.Background(), ToolRequest{Name: "job_output", JobID: jobID})
	if err != nil {
		t.Fatalf("unexpected job output error: %v", err)
	}
	if !strings.Contains(output.Output, jobID) {
		t.Fatalf("expected job output to mention job id, got %q", output.Output)
	}
	if _, err := engine.Execute(context.Background(), ToolRequest{Name: "job_kill", JobID: jobID}); err != nil {
		t.Fatalf("unexpected job kill error: %v", err)
	}
}

func TestExecuteRejectsUnknownTool(t *testing.T) {
	t.Parallel()

	engine, err := NewEngine(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("unexpected engine error: %v", err)
	}

	_, err = engine.Execute(context.Background(), ToolRequest{Name: "unknown"})
	if err == nil {
		t.Fatal("expected unknown tool error")
	}
	if !apperrors.IsCode(err, apperrors.CodeInvalidArgument) {
		t.Fatalf("expected invalid argument error, got %v", err)
	}
}
