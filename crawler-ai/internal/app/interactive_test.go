package app

import (
	"regexp"
	"strings"
	"testing"

	"crawler-ai/internal/config"
	"crawler-ai/internal/domain"
	"crawler-ai/internal/events"
	"crawler-ai/internal/provider"
	"crawler-ai/internal/session"
	"crawler-ai/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
)

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestBootstrapInteractiveModelSeedsTranscriptAndStatus(t *testing.T) {
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

	model, err := application.bootstrapInteractiveModel(tui.NewApp())
	if err != nil {
		t.Fatalf("unexpected bootstrap error: %v", err)
	}

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view := updated.(tui.App).View()
	if !strings.Contains(view, "Interactive mode ready") {
		t.Fatalf("expected bootstrapped UI to contain ready message, got %s", view)
	}

	session, ok := application.sessions.Get(application.sessionID)
	if !ok {
		t.Fatal("expected session to exist")
	}
	if len(session.Transcript) != 1 {
		t.Fatalf("expected 1 seeded transcript entry, got %d", len(session.Transcript))
	}
	if session.Transcript[0].Kind != domain.TranscriptSystem {
		t.Fatalf("expected seeded transcript kind %q, got %q", domain.TranscriptSystem, session.Transcript[0].Kind)
	}
	if session.Transcript[0].Message != interactiveReadyMessage {
		t.Fatalf("expected seeded transcript message %q, got %q", interactiveReadyMessage, session.Transcript[0].Message)
	}
}

func TestBootstrapInteractiveModelHydratesExistingSessionState(t *testing.T) {
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

	entry := domain.TranscriptEntry{ID: "assistant-1", Kind: domain.TranscriptAssistant, Message: "restored transcript"}
	if err := application.sessions.AppendTranscript(application.sessionID, entry); err != nil {
		t.Fatalf("AppendTranscript() error: %v", err)
	}
	if err := application.sessions.SetTasks(application.sessionID, []domain.Task{{ID: "task-1", Title: "Restored task", Status: domain.TaskCompleted}}); err != nil {
		t.Fatalf("SetTasks() error: %v", err)
	}
	if _, err := application.sessions.RecordUsage(application.sessionID, session.UsageUpdate{InputTokens: 9, OutputTokens: 4, Provider: "mock", Model: "mock-orchestrator-v1"}); err != nil {
		t.Fatalf("RecordUsage() error: %v", err)
	}

	model, err := application.bootstrapInteractiveModel(tui.NewApp())
	if err != nil {
		t.Fatalf("unexpected bootstrap error: %v", err)
	}

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view := stripANSI(updated.(tui.App).View())
	if !strings.Contains(view, "restored transcript") {
		t.Fatalf("expected hydrated transcript in UI, got %s", view)
	}
	if !strings.Contains(view, "Restored task") {
		t.Fatalf("expected hydrated tasks in UI, got %s", view)
	}

	loaded, ok := application.sessions.Get(application.sessionID)
	if !ok {
		t.Fatal("expected session to exist")
	}
	if len(loaded.Transcript) != 1 {
		t.Fatalf("expected existing transcript to be preserved without duplicate ready entry, got %d entries", len(loaded.Transcript))
	}
}

func stripANSI(value string) string {
	return ansiEscapePattern.ReplaceAllString(value, "")
}

func TestSwitchSessionPublishesHydratedState(t *testing.T) {
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

	if _, err := application.sessions.Create("session-2", application.config.WorkspaceRoot); err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	entry := domain.TranscriptEntry{ID: "assistant-2", Kind: domain.TranscriptAssistant, Message: "switched transcript"}
	if err := application.sessions.AppendTranscript("session-2", entry); err != nil {
		t.Fatalf("AppendTranscript() error: %v", err)
	}
	tasks := []domain.Task{{ID: "task-2", Title: "Switched task", Status: domain.TaskRunning}}
	if err := application.sessions.SetTasks("session-2", tasks); err != nil {
		t.Fatalf("SetTasks() error: %v", err)
	}
	if _, err := application.sessions.RecordUsage("session-2", session.UsageUpdate{InputTokens: 7, OutputTokens: 3}); err != nil {
		t.Fatalf("RecordUsage() error: %v", err)
	}

	var resetTranscript []domain.TranscriptEntry
	var resetTasks []domain.Task
	var switchedID string
	var usagePayload map[string]any
	application.bus.Subscribe(events.EventTranscriptReset, func(event events.Event) {
		if entries, ok := event.Payload.([]domain.TranscriptEntry); ok {
			resetTranscript = append([]domain.TranscriptEntry(nil), entries...)
		}
	})
	application.bus.Subscribe(events.EventTasksUpdated, func(event events.Event) {
		if published, ok := event.Payload.([]domain.Task); ok {
			resetTasks = append([]domain.Task(nil), published...)
		}
	})
	application.bus.Subscribe(events.EventSessionChanged, func(event events.Event) {
		if id, ok := event.Payload.(string); ok {
			switchedID = id
		}
	})
	application.bus.Subscribe(events.EventTokenUsage, func(event events.Event) {
		if payload, ok := event.Payload.(map[string]any); ok {
			usagePayload = payload
		}
	})

	if err := application.switchSession("session-2"); err != nil {
		t.Fatalf("switchSession() error: %v", err)
	}
	if application.sessionID != "session-2" {
		t.Fatalf("expected active session to change, got %s", application.sessionID)
	}
	if switchedID != "session-2" {
		t.Fatalf("expected session change event for session-2, got %q", switchedID)
	}
	if len(resetTranscript) != 1 || resetTranscript[0].Message != "switched transcript" {
		t.Fatalf("expected hydrated transcript reset, got %#v", resetTranscript)
	}
	if len(resetTasks) != 1 || resetTasks[0].Title != "Switched task" {
		t.Fatalf("expected hydrated tasks reset, got %#v", resetTasks)
	}
	if usagePayload["total_input_tokens"] != int64(7) || usagePayload["total_output_tokens"] != int64(3) {
		t.Fatalf("expected hydrated usage totals, got %#v", usagePayload)
	}
}

func TestSwitchModelPersistsPreferredSelection(t *testing.T) {
	workspaceDir := t.TempDir()
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "secret")

	application, err := New(config.Config{
		Env:           config.EnvTest,
		LogLevel:      "info",
		WorkspaceRoot: workspaceDir,
		Models:        config.DefaultModelConfig(),
	})
	if err != nil {
		t.Fatalf("unexpected app error: %v", err)
	}

	if err := application.switchModel(provider.RoleOrchestrator, "openai", "gpt-4o"); err != nil {
		t.Fatalf("unexpected switchModel error: %v", err)
	}

	loaded, err := config.LoadForWorkingDir(workspaceDir)
	if err != nil {
		t.Fatalf("LoadForWorkingDir() error: %v", err)
	}
	if loaded.Models.Orchestrator.Provider != "openai" || loaded.Models.Orchestrator.Model != "gpt-4o" {
		t.Fatalf("expected persisted orchestrator selection, got %+v", loaded.Models.Orchestrator)
	}
	if len(loaded.RecentModels.Orchestrator) == 0 {
		t.Fatal("expected recent orchestrator model to be recorded")
	}
}
