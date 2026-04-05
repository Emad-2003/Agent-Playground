package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crawler-ai/internal/events"
	"crawler-ai/internal/provider"
	"crawler-ai/internal/runtime"
	"crawler-ai/internal/session"
)

func TestSessionToolExecutorReturnsStructuredRepairHintForMissingRequiredField(t *testing.T) {
	t.Parallel()

	executor := &sessionToolExecutor{}
	result, err := executor.ExecuteToolCall(context.Background(), "session-1", provider.ToolCall{
		ID:       "call-1",
		Name:     "write_file",
		Input:    `{"content":"hello"}`,
		Finished: true,
	})
	if err != nil {
		t.Fatalf("ExecuteToolCall() error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected structured repair hint error result, got %#v", result)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		t.Fatalf("expected JSON repair hint, got %q (%v)", result.Content, err)
	}
	if payload["status"] != "invalid_tool_call" {
		t.Fatalf("expected invalid_tool_call status, got %#v", payload)
	}
	if payload["tool"] != "write_file" {
		t.Fatalf("expected write_file tool in hint, got %#v", payload)
	}
	missing, ok := payload["missing_fields"].([]any)
	if !ok || len(missing) != 1 || missing[0] != "path" {
		t.Fatalf("expected missing path field, got %#v", payload)
	}
	example, ok := payload["example"].(map[string]any)
	if !ok || example["path"] != "README.md" {
		t.Fatalf("expected example payload with path, got %#v", payload)
	}
	received, ok := payload["received_input"].(map[string]any)
	if !ok || received["content"] != "hello" {
		t.Fatalf("expected received input echo, got %#v", payload)
	}
}

func TestRuntimeToolRequestFromCallAcceptsCrushStyleAliases(t *testing.T) {
	t.Parallel()

	request, err := runtimeToolRequestFromCall(provider.ToolCall{
		ID:    "call-1",
		Name:  "edit_file",
		Input: `{"file_path":"index.html","old_string":"before","new_string":"after"}`,
	})
	if err != nil {
		t.Fatalf("runtimeToolRequestFromCall() error: %v", err)
	}
	if request.Path != "index.html" || request.OldText != "before" || request.NewText != "after" {
		t.Fatalf("expected aliases to populate request, got %#v", request)
	}
}

func TestRuntimeToolRequestFromCallNormalizesEscapedMultilineContent(t *testing.T) {
	t.Parallel()

	request, err := runtimeToolRequestFromCall(provider.ToolCall{
		ID:    "call-escaped",
		Name:  "edit_file",
		Input: `{"path":"app.js","old_text":"line1\\nline2","new_text":"const value = 1;\\nconsole.log(value);"}`,
	})
	if err != nil {
		t.Fatalf("runtimeToolRequestFromCall() error: %v", err)
	}
	if request.OldText != "line1\nline2" {
		t.Fatalf("expected old_text to unescape newlines, got %q", request.OldText)
	}
	if request.NewText != "const value = 1;\nconsole.log(value);" {
		t.Fatalf("expected new_text to unescape newlines, got %q", request.NewText)
	}
}

func TestSessionToolExecutorAutoRepairsWriteFilePath(t *testing.T) {
	t.Parallel()

	executor, workspaceRoot := newExecutorTestHarness(t)
	result, err := executor.ExecuteToolCall(context.Background(), "session-1", provider.ToolCall{
		ID:    "call-1",
		Name:  "write_file",
		Input: `{"content":"# Tic-Tac-Toe\nA browser game."}`,
	})
	if err != nil {
		t.Fatalf("ExecuteToolCall() error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected auto-repaired write_file call to succeed, got %#v", result)
	}
	content, readErr := os.ReadFile(filepath.Join(workspaceRoot, "README.md"))
	if readErr != nil {
		t.Fatalf("expected inferred README.md to exist: %v", readErr)
	}
	if !strings.Contains(string(content), "Tic-Tac-Toe") {
		t.Fatalf("expected inferred README.md contents, got %q", string(content))
	}
	if !strings.Contains(result.Content, `Auto-repaired invalid tool call for write_file by filling path="README.md".`) {
		t.Fatalf("expected repair note in tool result, got %q", result.Content)
	}
}

func TestSessionToolExecutorAutoRepairsEditFilePathFromWorkspace(t *testing.T) {
	t.Parallel()

	executor, workspaceRoot := newExecutorTestHarness(t)
	initial := "<div id=\"game\"></div>"
	if err := os.WriteFile(filepath.Join(workspaceRoot, "index.html"), []byte(initial), 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
	result, err := executor.ExecuteToolCall(context.Background(), "session-1", provider.ToolCall{
		ID:    "call-2",
		Name:  "edit_file",
		Input: `{"old_text":"<div id=\"game\"></div>","new_text":"<div id=\"game\"><button>Restart</button></div>"}`,
	})
	if err != nil {
		t.Fatalf("ExecuteToolCall() error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected auto-repaired edit_file call to succeed, got %#v", result)
	}
	content, readErr := os.ReadFile(filepath.Join(workspaceRoot, "index.html"))
	if readErr != nil {
		t.Fatalf("read edited file: %v", readErr)
	}
	if !strings.Contains(string(content), "Restart") {
		t.Fatalf("expected inferred index.html edit to apply, got %q", string(content))
	}
	if !strings.Contains(result.Content, `Auto-repaired invalid tool call for edit_file by filling path="index.html".`) {
		t.Fatalf("expected repair note in tool result, got %q", result.Content)
	}
}

func TestSessionToolExecutorAutoRepairsWholeFileEditFromReplacementContent(t *testing.T) {
	t.Parallel()

	executor, workspaceRoot := newExecutorTestHarness(t)
	if err := os.WriteFile(filepath.Join(workspaceRoot, "index.html"), []byte("<h1>Old</h1>"), 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
	result, err := executor.ExecuteToolCall(context.Background(), "session-1", provider.ToolCall{
		ID:    "call-3",
		Name:  "edit_file",
		Input: `{"new_text":"<!DOCTYPE html><html><body><h1>New</h1></body></html>"}`,
	})
	if err != nil {
		t.Fatalf("ExecuteToolCall() error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected whole-file edit repair to succeed, got %#v", result)
	}
	content, readErr := os.ReadFile(filepath.Join(workspaceRoot, "index.html"))
	if readErr != nil {
		t.Fatalf("read edited file: %v", readErr)
	}
	if string(content) != "<!DOCTYPE html><html><body><h1>New</h1></body></html>" {
		t.Fatalf("expected index.html replacement to apply, got %q", string(content))
	}
	if !strings.Contains(result.Content, "old_text=current file contents") {
		t.Fatalf("expected repair note to mention old_text hydration, got %q", result.Content)
	}
}

func TestSessionToolExecutorRetriesStaleEditWithCurrentContents(t *testing.T) {
	t.Parallel()

	executor, workspaceRoot := newExecutorTestHarness(t)
	if err := os.WriteFile(filepath.Join(workspaceRoot, "index.html"), []byte("<h1>Current</h1>"), 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
	result, err := executor.ExecuteToolCall(context.Background(), "session-1", provider.ToolCall{
		ID:    "call-4",
		Name:  "edit_file",
		Input: `{"path":"index.html","old_text":"<h1>Stale</h1>","new_text":"<!DOCTYPE html><html><body><h1>Refined</h1></body></html>"}`,
	})
	if err != nil {
		t.Fatalf("ExecuteToolCall() error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected stale edit retry to succeed, got %#v", result)
	}
	content, readErr := os.ReadFile(filepath.Join(workspaceRoot, "index.html"))
	if readErr != nil {
		t.Fatalf("read edited file: %v", readErr)
	}
	if string(content) != "<!DOCTYPE html><html><body><h1>Refined</h1></body></html>" {
		t.Fatalf("expected stale retry to replace file, got %q", string(content))
	}
}

func TestSessionToolExecutorDoesNotReplaceWholeFileWithSnippet(t *testing.T) {
	t.Parallel()

	executor, workspaceRoot := newExecutorTestHarness(t)
	original := "<!DOCTYPE html><html><body><h1>Current</h1></body></html>"
	if err := os.WriteFile(filepath.Join(workspaceRoot, "index.html"), []byte(original), 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
	result, err := executor.ExecuteToolCall(context.Background(), "session-1", provider.ToolCall{
		ID:    "call-5",
		Name:  "edit_file",
		Input: `{"path":"index.html","old_text":"<h1>Stale</h1>","new_text":"<div id=\"status\">Current Turn: X</div>"}`,
	})
	if err != nil {
		t.Fatalf("ExecuteToolCall() error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected snippet edit to stay rejected, got %#v", result)
	}
	content, readErr := os.ReadFile(filepath.Join(workspaceRoot, "index.html"))
	if readErr != nil {
		t.Fatalf("read original file: %v", readErr)
	}
	if string(content) != original {
		t.Fatalf("expected original file to remain unchanged, got %q", string(content))
	}
}

func newExecutorTestHarness(t *testing.T) (*sessionToolExecutor, string) {
	t.Helper()

	workspaceRoot := t.TempDir()
	bus := events.NewBus()
	sessions := session.NewManager()
	if _, err := sessions.Create("session-1", workspaceRoot); err != nil {
		t.Fatalf("sessions.Create() error: %v", err)
	}
	services := newSessionStateServices(sessions, bus, nil)
	runtimeEngine, err := runtime.NewEngine(workspaceRoot, bus)
	if err != nil {
		t.Fatalf("runtime.NewEngine() error: %v", err)
	}
	return newSessionToolExecutor(runtimeEngine, nil, services.messages, services.fileTracker, sessions, func(prefix string) string {
		return prefix + "-1"
	}, func() time.Time {
		return time.Now().UTC()
	}, nil), workspaceRoot
}
