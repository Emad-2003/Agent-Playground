package app

import (
	"context"
	"strings"
	"testing"

	"crawler-ai/internal/config"
	"crawler-ai/internal/domain"
	apperrors "crawler-ai/internal/errors"
	"crawler-ai/internal/events"
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

	if err := application.HandlePrompt(context.Background(), "/shell echo hello"); err != nil {
		t.Fatalf("unexpected shell prompt error: %v", err)
	}

	request := <-approvalEvents
	if request.Action != "shell" {
		t.Fatalf("expected shell approval, got %s", request.Action)
	}
	if request.Description != "/shell echo hello" {
		t.Fatalf("expected original shell prompt in approval description, got %q", request.Description)
	}
	if len(application.pending) != 1 {
		t.Fatalf("expected 1 pending approval, got %d", len(application.pending))
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

	if len(application.tasks) == 0 {
		t.Fatal("expected tasks to be tracked after run plan")
	}
	for _, task := range application.tasks {
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
