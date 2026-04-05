package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crawler-ai/internal/config"
	"crawler-ai/internal/domain"
	apperrors "crawler-ai/internal/errors"
	"crawler-ai/internal/events"
	sessionpkg "crawler-ai/internal/session"
)

func TestHandlePromptUsesProviderForNaturalLanguage(t *testing.T) {
	t.Parallel()

	application, err := New(config.Config{
		Env:           config.EnvTest,
		LogLevel:      "debug",
		WorkspaceRoot: t.TempDir(),
		Models:        config.DefaultModelConfig(),
	})
	if err != nil {
		t.Fatalf("unexpected app error: %v", err)
	}

	var entries []domain.TranscriptEntry
	application.bus.Subscribe(events.EventTranscriptAdded, func(event events.Event) {
		entry, ok := event.Payload.(domain.TranscriptEntry)
		if ok {
			entries = append(entries, entry)
		}
	})

	if err := application.HandlePrompt(context.Background(), "hello there"); err != nil {
		t.Fatalf("unexpected handle prompt error: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 transcript entries, got %d", len(entries))
	}
	if entries[0].Kind != domain.TranscriptUser || entries[1].Kind != domain.TranscriptAssistant {
		t.Fatalf("expected user then assistant transcript kinds, got %s and %s", entries[0].Kind, entries[1].Kind)
	}
}

func TestHandlePromptRequestsApprovalForWrite(t *testing.T) {
	t.Parallel()

	application, err := New(config.Config{
		Env:           config.EnvTest,
		LogLevel:      "info",
		WorkspaceRoot: t.TempDir(),
		Models:        config.DefaultModelConfig(),
	})
	if err != nil {
		t.Fatalf("unexpected app error: %v", err)
	}

	approvalEvents := make(chan domain.ApprovalRequest, 1)
	application.bus.Subscribe(events.EventApprovalRequested, func(event events.Event) {
		request, ok := event.Payload.(domain.ApprovalRequest)
		if ok {
			approvalEvents <- request
		}
	})

	if err := application.HandlePrompt(context.Background(), "/write notes.txt::hello"); err != nil {
		t.Fatalf("unexpected handle prompt error: %v", err)
	}

	request := <-approvalEvents
	if request.Action != "write_file" {
		t.Fatalf("expected write_file approval, got %s", request.Action)
	}
	if request.ToolCallID == "" {
		t.Fatal("expected tool call id on approval request")
	}

	if err := application.ResolveApproval(context.Background(), request.ID, true); err != nil {
		t.Fatalf("unexpected resolve approval error: %v", err)
	}

	session, ok := application.sessions.Get(application.sessionID)
	if !ok {
		t.Fatal("expected session to exist")
	}
	if len(session.Transcript) < 2 {
		t.Fatalf("expected transcript entries after approval, got %d", len(session.Transcript))
	}
	last := session.Transcript[len(session.Transcript)-1]
	if last.Kind != domain.TranscriptTool || last.Metadata["tool_call_id"] != request.ToolCallID {
		t.Fatalf("expected tool transcript linked to approval call id, got %#v", last)
	}
}

func TestHandlePromptRequestsApprovalForEdit(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	application, err := New(config.Config{
		Env:           config.EnvTest,
		LogLevel:      "info",
		WorkspaceRoot: workspace,
		Models:        config.DefaultModelConfig(),
	})
	if err != nil {
		t.Fatalf("unexpected app error: %v", err)
	}
	if err := application.HandlePrompt(context.Background(), "/write notes.txt::hello"); err != nil {
		t.Fatalf("unexpected create prompt error: %v", err)
	}
	for id := range application.pending {
		if err := application.ResolveApproval(context.Background(), id, true); err != nil {
			t.Fatalf("unexpected resolve approval error: %v", err)
		}
		break
	}

	approvalEvents := make(chan domain.ApprovalRequest, 1)
	application.bus.Subscribe(events.EventApprovalRequested, func(event events.Event) {
		request, ok := event.Payload.(domain.ApprovalRequest)
		if ok {
			approvalEvents <- request
		}
	})

	if err := application.HandlePrompt(context.Background(), "/edit notes.txt::hello::hello world"); err != nil {
		t.Fatalf("unexpected edit prompt error: %v", err)
	}
	request := <-approvalEvents
	if request.Action != "edit_file" {
		t.Fatalf("expected edit_file approval, got %s", request.Action)
	}
	if err := application.ResolveApproval(context.Background(), request.ID, true); err != nil {
		t.Fatalf("unexpected resolve approval error: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(workspace, "notes.txt"))
	if err != nil {
		t.Fatalf("expected edited file to exist: %v", err)
	}
	if string(content) != "hello world" {
		t.Fatalf("expected edited content, got %q", string(content))
	}
	sess, _ := application.sessions.Get(application.sessionID)
	last := sess.Transcript[len(sess.Transcript)-1]
	if last.Metadata["history_path"] == "" {
		t.Fatalf("expected history metadata on edit transcript, got %#v", last)
	}
	if len(sess.Files) < 2 {
		t.Fatalf("expected tracked workspace and history files, got %#v", sess.Files)
	}
	var sawEditedFile bool
	var sawHistoryFile bool
	for _, file := range sess.Files {
		if file.Path == "notes.txt" && file.Tool == "edit_file" && file.Kind == sessionpkg.FileRecordWorkspace {
			sawEditedFile = true
		}
		if file.Path != "" && file.Kind == sessionpkg.FileRecordHistory {
			sawHistoryFile = true
		}
	}
	if !sawEditedFile {
		t.Fatalf("expected tracked edited file record, got %#v", sess.Files)
	}
	if !sawHistoryFile {
		t.Fatalf("expected tracked history snapshot record, got %#v", sess.Files)
	}
}

func TestHandlePromptRequestsApprovalForShell(t *testing.T) {
	t.Parallel()

	application, err := New(config.Config{
		Env:           config.EnvTest,
		LogLevel:      "info",
		WorkspaceRoot: t.TempDir(),
		Models:        config.DefaultModelConfig(),
	})
	if err != nil {
		t.Fatalf("unexpected app error: %v", err)
	}

	approvalEvents := make(chan domain.ApprovalRequest, 1)
	application.bus.Subscribe(events.EventApprovalRequested, func(event events.Event) {
		request, ok := event.Payload.(domain.ApprovalRequest)
		if ok {
			approvalEvents <- request
		}
	})

	if err := application.HandlePrompt(context.Background(), "/shell mkdir temp-dir"); err != nil {
		t.Fatalf("unexpected shell prompt error: %v", err)
	}

	request := <-approvalEvents
	if request.Action != "shell" {
		t.Fatalf("expected shell approval, got %s", request.Action)
	}
	if request.ToolCallID == "" {
		t.Fatal("expected shell approval to carry tool call id")
	}
	if request.Description != "/shell mkdir temp-dir" {
		t.Fatalf("expected original shell prompt in approval description, got %q", request.Description)
	}
	if len(application.pending) != 1 {
		t.Fatalf("expected 1 pending approval, got %d", len(application.pending))
	}
}

func TestHandlePromptRunsSafeShellWithoutApproval(t *testing.T) {
	t.Parallel()
	application, err := New(config.Config{
		Env:           config.EnvTest,
		LogLevel:      "info",
		WorkspaceRoot: t.TempDir(),
		Models:        config.DefaultModelConfig(),
	})
	if err != nil {
		t.Fatalf("unexpected app error: %v", err)
	}
	if err := application.HandlePrompt(context.Background(), "/shell echo hello"); err != nil {
		t.Fatalf("unexpected safe shell prompt error: %v", err)
	}
	if len(application.pending) != 0 {
		t.Fatalf("expected safe shell to avoid approval, got %d pending", len(application.pending))
	}
	session, _ := application.sessions.Get(application.sessionID)
	last := session.Transcript[len(session.Transcript)-1]
	if !strings.Contains(strings.ToLower(last.Message), "hello") {
		t.Fatalf("expected safe shell transcript to contain output, got %q", last.Message)
	}
}

func TestHandlePromptRequestsApprovalForFetch(t *testing.T) {
	t.Parallel()
	application, err := New(config.Config{
		Env:           config.EnvTest,
		LogLevel:      "info",
		WorkspaceRoot: t.TempDir(),
		Models:        config.DefaultModelConfig(),
	})
	if err != nil {
		t.Fatalf("unexpected app error: %v", err)
	}
	approvalEvents := make(chan domain.ApprovalRequest, 1)
	application.bus.Subscribe(events.EventApprovalRequested, func(event events.Event) {
		request, ok := event.Payload.(domain.ApprovalRequest)
		if ok {
			approvalEvents <- request
		}
	})
	if err := application.HandlePrompt(context.Background(), "/fetch https://example.com"); err != nil {
		t.Fatalf("unexpected fetch prompt error: %v", err)
	}
	request := <-approvalEvents
	if request.Action != "fetch" {
		t.Fatalf("expected fetch approval, got %s", request.Action)
	}
	if request.ToolCallID == "" {
		t.Fatal("expected fetch approval to carry tool call id")
	}
}

func TestHandlePromptRunsFetchAfterApproval(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello from fetch"))
	}))
	defer server.Close()
	application, err := New(config.Config{
		Env:           config.EnvTest,
		LogLevel:      "info",
		WorkspaceRoot: t.TempDir(),
		Models:        config.DefaultModelConfig(),
	})
	if err != nil {
		t.Fatalf("unexpected app error: %v", err)
	}
	if err := application.HandlePrompt(context.Background(), "/fetch "+server.URL); err != nil {
		t.Fatalf("unexpected fetch prompt error: %v", err)
	}
	var approvalID string
	for id := range application.pending {
		approvalID = id
		break
	}
	if approvalID == "" {
		t.Fatal("expected pending fetch approval")
	}
	if err := application.ResolveApproval(context.Background(), approvalID, true); err != nil {
		t.Fatalf("unexpected approval resolution error: %v", err)
	}
	session, _ := application.sessions.Get(application.sessionID)
	last := session.Transcript[len(session.Transcript)-1]
	if !strings.Contains(last.Message, "hello from fetch") {
		t.Fatalf("expected fetch transcript to contain fetched body, got %q", last.Message)
	}
}

func TestHandlePromptRunsBackgroundShellLifecycle(t *testing.T) {
	application, err := New(config.Config{
		Env:           config.EnvTest,
		LogLevel:      "info",
		WorkspaceRoot: t.TempDir(),
		Models:        config.DefaultModelConfig(),
		Yolo:          true,
	})
	if err != nil {
		t.Fatalf("unexpected app error: %v", err)
	}
	if err := application.HandlePrompt(context.Background(), "/shell-bg echo hello bg"); err != nil {
		t.Fatalf("unexpected shell-bg prompt error: %v", err)
	}
	session, _ := application.sessions.Get(application.sessionID)
	last := session.Transcript[len(session.Transcript)-1]
	jobID := last.Metadata["job_id"]
	if jobID == "" {
		t.Fatalf("expected background shell transcript to include job id, got %#v", last)
	}
	if err := application.HandlePrompt(context.Background(), "/job-output "+jobID); err != nil {
		t.Fatalf("unexpected job-output prompt error: %v", err)
	}
	if err := application.HandlePrompt(context.Background(), "/job-kill "+jobID); err != nil {
		t.Fatalf("unexpected job-kill prompt error: %v", err)
	}
}

func TestHandlePromptRunsGlobCommand(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "src"), 0o755); err != nil {
		t.Fatalf("unexpected mkdir error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "src", "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	application, err := New(config.Config{
		Env:           config.EnvTest,
		LogLevel:      "info",
		WorkspaceRoot: workspace,
		Models:        config.DefaultModelConfig(),
	})
	if err != nil {
		t.Fatalf("unexpected app error: %v", err)
	}
	if err := application.HandlePrompt(context.Background(), "/glob src/**/*.go"); err != nil {
		t.Fatalf("unexpected glob prompt error: %v", err)
	}
	session, _ := application.sessions.Get(application.sessionID)
	last := session.Transcript[len(session.Transcript)-1]
	if !strings.Contains(last.Message, "src/main.go") {
		t.Fatalf("expected glob transcript to include match, got %q", last.Message)
	}
}

func TestHandlePromptRunsViewCommand(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "notes.txt"), []byte("first\nsecond\nthird\nfourth"), 0o644); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	application, err := New(config.Config{
		Env:           config.EnvTest,
		LogLevel:      "info",
		WorkspaceRoot: workspace,
		Models:        config.DefaultModelConfig(),
	})
	if err != nil {
		t.Fatalf("unexpected app error: %v", err)
	}
	if err := application.HandlePrompt(context.Background(), "/view notes.txt::2::2"); err != nil {
		t.Fatalf("unexpected view prompt error: %v", err)
	}
	session, _ := application.sessions.Get(application.sessionID)
	last := session.Transcript[len(session.Transcript)-1]
	if !strings.Contains(last.Message, "     2|second") || !strings.Contains(last.Message, "     3|third") {
		t.Fatalf("expected line-numbered view transcript, got %q", last.Message)
	}
	if !strings.Contains(last.Message, "Use /view notes.txt::4::2 to continue") {
		t.Fatalf("expected continuation hint, got %q", last.Message)
	}
}

func TestHandlePromptRejectsMalformedWriteCommand(t *testing.T) {
	t.Parallel()

	application, err := New(config.Config{
		Env:           config.EnvTest,
		LogLevel:      "info",
		WorkspaceRoot: t.TempDir(),
		Models:        config.DefaultModelConfig(),
	})
	if err != nil {
		t.Fatalf("unexpected app error: %v", err)
	}

	err = application.HandlePrompt(context.Background(), "/write notes.txt")
	if err == nil {
		t.Fatal("expected malformed write command error")
	}
	if !apperrors.IsCode(err, apperrors.CodeInvalidArgument) {
		t.Fatalf("expected invalid argument error, got %v", err)
	}
}

func TestHandlePromptRejectsMalformedEditCommand(t *testing.T) {
	t.Parallel()

	application, err := New(config.Config{
		Env:           config.EnvTest,
		LogLevel:      "info",
		WorkspaceRoot: t.TempDir(),
		Models:        config.DefaultModelConfig(),
	})
	if err != nil {
		t.Fatalf("unexpected app error: %v", err)
	}

	err = application.HandlePrompt(context.Background(), "/edit notes.txt::hello")
	if err == nil {
		t.Fatal("expected malformed edit command error")
	}
	if !apperrors.IsCode(err, apperrors.CodeInvalidArgument) {
		t.Fatalf("expected invalid argument error, got %v", err)
	}
}

func TestResolveApprovalRejectsUnknownID(t *testing.T) {
	t.Parallel()

	application, err := New(config.Config{
		Env:           config.EnvTest,
		LogLevel:      "info",
		WorkspaceRoot: t.TempDir(),
		Models:        config.DefaultModelConfig(),
	})
	if err != nil {
		t.Fatalf("unexpected app error: %v", err)
	}

	err = application.ResolveApproval(context.Background(), "missing", true)
	if err == nil {
		t.Fatal("expected missing approval error")
	}
	if !apperrors.IsCode(err, apperrors.CodeInvalidArgument) {
		t.Fatalf("expected invalid argument error, got %v", err)
	}
}

func TestHandlePromptRunsReadCommand(t *testing.T) {
	t.Parallel()

	application, err := New(config.Config{
		Env:           config.EnvTest,
		LogLevel:      "info",
		WorkspaceRoot: t.TempDir(),
		Models:        config.DefaultModelConfig(),
	})
	if err != nil {
		t.Fatalf("unexpected app error: %v", err)
	}

	if err := application.HandlePrompt(context.Background(), "/write notes.txt::hello world"); err != nil {
		t.Fatalf("unexpected write prompt error: %v", err)
	}
	var approvalID string
	for id := range application.pending {
		approvalID = id
		break
	}
	if approvalID == "" {
		t.Fatal("expected pending approval id")
	}
	if err := application.ResolveApproval(context.Background(), approvalID, true); err != nil {
		t.Fatalf("unexpected resolve approval error: %v", err)
	}

	if err := application.HandlePrompt(context.Background(), "/read notes.txt"); err != nil {
		t.Fatalf("unexpected read prompt error: %v", err)
	}

	session, _ := application.sessions.Get(application.sessionID)
	last := session.Transcript[len(session.Transcript)-1]
	if !strings.Contains(last.Message, "hello world") {
		t.Fatalf("expected read transcript to contain file contents, got %q", last.Message)
	}
	if last.Metadata["tool_call_id"] == "" {
		t.Fatalf("expected read transcript to have stable tool call id, got %#v", last)
	}
}

func TestHandlePromptYoloSkipsApprovalForWrite(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	application, err := New(config.Config{
		Env:           config.EnvTest,
		LogLevel:      "info",
		WorkspaceRoot: workspace,
		Models:        config.DefaultModelConfig(),
		Yolo:          true,
	})
	if err != nil {
		t.Fatalf("unexpected app error: %v", err)
	}

	if err := application.HandlePrompt(context.Background(), "/write notes.txt::hello world"); err != nil {
		t.Fatalf("unexpected write prompt error: %v", err)
	}

	if len(application.pending) != 0 {
		t.Fatalf("expected no pending approvals in yolo mode, got %d", len(application.pending))
	}

	content, err := os.ReadFile(filepath.Join(workspace, "notes.txt"))
	if err != nil {
		t.Fatalf("expected file to be written immediately: %v", err)
	}
	if string(content) != "hello world" {
		t.Fatalf("expected written content to match, got %q", string(content))
	}
}

func TestHandlePromptRejectsBlindOverwriteViaWrite(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	application, err := New(config.Config{
		Env:           config.EnvTest,
		LogLevel:      "info",
		WorkspaceRoot: workspace,
		Models:        config.DefaultModelConfig(),
		Yolo:          true,
	})
	if err != nil {
		t.Fatalf("unexpected app error: %v", err)
	}
	if err := application.HandlePrompt(context.Background(), "/write notes.txt::hello world"); err != nil {
		t.Fatalf("unexpected initial write prompt error: %v", err)
	}
	err = application.HandlePrompt(context.Background(), "/write notes.txt::changed")
	if err == nil {
		t.Fatal("expected overwrite to be rejected")
	}
	content, readErr := os.ReadFile(filepath.Join(workspace, "notes.txt"))
	if readErr != nil {
		t.Fatalf("expected original file to remain readable: %v", readErr)
	}
	if string(content) != "hello world" {
		t.Fatalf("expected original content to remain unchanged, got %q", string(content))
	}
}

func TestHandlePromptRejectsStaleEdit(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	application, err := New(config.Config{
		Env:           config.EnvTest,
		LogLevel:      "info",
		WorkspaceRoot: workspace,
		Models:        config.DefaultModelConfig(),
		Yolo:          true,
	})
	if err != nil {
		t.Fatalf("unexpected app error: %v", err)
	}
	if err := application.HandlePrompt(context.Background(), "/write notes.txt::hello world"); err != nil {
		t.Fatalf("unexpected initial write prompt error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "notes.txt"), []byte("changed elsewhere"), 0o644); err != nil {
		t.Fatalf("unexpected external write error: %v", err)
	}
	err = application.HandlePrompt(context.Background(), "/edit notes.txt::hello world::hello crawler")
	if err == nil {
		t.Fatal("expected stale edit error")
	}
	if !strings.Contains(err.Error(), "changed since it was last read") {
		t.Fatalf("expected staleness error, got %v", err)
	}
}

func TestHandlePromptRejectsDisabledTool(t *testing.T) {
	t.Parallel()

	application, err := New(config.Config{
		Env:           config.EnvTest,
		LogLevel:      "info",
		WorkspaceRoot: t.TempDir(),
		Models:        config.DefaultModelConfig(),
		Permissions: config.PermissionsConfig{
			DisabledTools: []string{"shell"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected app error: %v", err)
	}

	err = application.HandlePrompt(context.Background(), "/shell echo hello")
	if err == nil {
		t.Fatal("expected permission error for disabled tool")
	}
	if !apperrors.IsCode(err, apperrors.CodePermissionDenied) {
		t.Fatalf("expected permission denied error, got %v", err)
	}
	if len(application.pending) != 0 {
		t.Fatalf("expected no approval request for disabled tool, got %d", len(application.pending))
	}
}

func TestHandlePromptRejectsToolOutsideAllowedSet(t *testing.T) {
	t.Parallel()

	application, err := New(config.Config{
		Env:           config.EnvTest,
		LogLevel:      "info",
		WorkspaceRoot: t.TempDir(),
		Models:        config.DefaultModelConfig(),
		Permissions: config.PermissionsConfig{
			AllowedTools: []string{"read_file"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected app error: %v", err)
	}

	err = application.HandlePrompt(context.Background(), "/grep hello")
	if err == nil {
		t.Fatal("expected permission error for disallowed tool")
	}
	if !apperrors.IsCode(err, apperrors.CodePermissionDenied) {
		t.Fatalf("expected permission denied error, got %v", err)
	}
}

func TestHandlePromptCreatesPlan(t *testing.T) {
	t.Parallel()

	application, err := New(config.Config{
		Env:           config.EnvTest,
		LogLevel:      "info",
		WorkspaceRoot: t.TempDir(),
		Models:        config.DefaultModelConfig(),
	})
	if err != nil {
		t.Fatalf("unexpected app error: %v", err)
	}

	updates := make(chan []domain.Task, 1)
	application.bus.Subscribe(events.EventTasksUpdated, func(event events.Event) {
		tasks, ok := event.Payload.([]domain.Task)
		if ok {
			updates <- tasks
		}
	})

	if err := application.HandlePrompt(context.Background(), "/plan build and test a feature"); err != nil {
		t.Fatalf("unexpected handle prompt error: %v", err)
	}

	tasks := <-updates
	if len(tasks) < 3 {
		t.Fatalf("expected planned tasks, got %d", len(tasks))
	}
	if tasks[0].Title != "Analyze request" {
		t.Fatalf("expected first task to analyze request, got %s", tasks[0].Title)
	}
}

func TestHandlePromptRunsPlan(t *testing.T) {
	t.Parallel()

	application, err := New(config.Config{
		Env:           config.EnvTest,
		LogLevel:      "info",
		WorkspaceRoot: t.TempDir(),
		Models:        config.DefaultModelConfig(),
	})
	if err != nil {
		t.Fatalf("unexpected app error: %v", err)
	}

	if err := application.HandlePrompt(context.Background(), "/run build and test a feature"); err != nil {
		t.Fatalf("unexpected run plan error: %v", err)
	}

	sessionState, _ := application.sessions.Get(application.sessionID)
	if len(sessionState.Tasks) == 0 {
		t.Fatal("expected tasks to be tracked after run plan")
	}
	for _, task := range sessionState.Tasks {
		if task.Status != domain.TaskCompleted {
			t.Fatalf("expected task %s to be completed, got %s", task.ID, task.Status)
		}
		if strings.TrimSpace(task.Result) == "" {
			t.Fatalf("expected task %s to include a result", task.ID)
		}
	}

	session, _ := application.sessions.Get(application.sessionID)
	if len(session.Transcript) < 3 {
		t.Fatalf("expected run plan to append transcript entries, got %d", len(session.Transcript))
	}
}

func TestSetTasksPersistsSessionState(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	application, err := New(config.Config{
		Env:           config.EnvTest,
		LogLevel:      "info",
		WorkspaceRoot: t.TempDir(),
		Models:        config.DefaultModelConfig(),
	})
	if err != nil {
		t.Fatalf("unexpected app error: %v", err)
	}
	application.sessions.SetDataDir(dataDir)

	application.setTasks(application.sessionID, []domain.Task{{ID: "task-1", Title: "Analyze", Status: domain.TaskPending}})

	reloaded := sessionpkg.NewManager()
	reloaded.SetDataDir(dataDir)
	tasks, err := reloaded.ReadPersistedTasks(application.sessionID)
	if err != nil {
		t.Fatalf("ReadPersistedTasks() error: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "task-1" {
		t.Fatalf("expected persisted task after task update, got %#v", tasks)
	}
}
