package session

import (
	"testing"
	"time"

	"crawler-ai/internal/domain"
	apperrors "crawler-ai/internal/errors"
)

func TestCreateAndGetSession(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	created, err := manager.Create("session-1", "/workspace")
	if err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}

	loaded, ok := manager.Get(created.ID)
	if !ok {
		t.Fatal("expected session to exist")
	}

	if loaded.WorkspaceRoot != "/workspace" {
		t.Fatalf("expected workspace root /workspace, got %s", loaded.WorkspaceRoot)
	}
}

func TestCreateRejectsDuplicateID(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	if _, err := manager.Create("session-1", "/workspace"); err != nil {
		t.Fatalf("unexpected first create error: %v", err)
	}

	_, err := manager.Create("session-1", "/workspace")
	if err == nil {
		t.Fatal("expected duplicate create error")
	}

	if !apperrors.IsCode(err, apperrors.CodeInvalidArgument) {
		t.Fatalf("expected invalid argument error, got %v", err)
	}
}

func TestAppendTranscriptUpdatesSession(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	baseTime := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	step := 0
	manager.now = func() time.Time {
		step++
		return baseTime.Add(time.Duration(step) * time.Minute)
	}

	created, err := manager.Create("session-1", "/workspace")
	if err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}

	err = manager.AppendTranscript(created.ID, domain.TranscriptEntry{
		ID:      "entry-1",
		Kind:    domain.TranscriptUser,
		Message: "hello",
	})
	if err != nil {
		t.Fatalf("unexpected append error: %v", err)
	}

	loaded, _ := manager.Get(created.ID)
	if len(loaded.Transcript) != 1 {
		t.Fatalf("expected transcript length 1, got %d", len(loaded.Transcript))
	}
	if !loaded.UpdatedAt.After(loaded.CreatedAt) {
		t.Fatalf("expected updated time after created time, got %s and %s", loaded.UpdatedAt, loaded.CreatedAt)
	}
}

func TestSetActiveTasksClonesInput(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	created, err := manager.Create("session-1", "/workspace")
	if err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}

	tasks := []string{"task-1", "task-2"}
	if err := manager.SetActiveTasks(created.ID, tasks); err != nil {
		t.Fatalf("unexpected set active tasks error: %v", err)
	}

	tasks[0] = "mutated"
	loaded, _ := manager.Get(created.ID)
	if loaded.ActiveTaskIDs[0] != "task-1" {
		t.Fatalf("expected stored task id to remain task-1, got %s", loaded.ActiveTaskIDs[0])
	}
}

func TestSetTasksPersistsTrackedTasks(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	created, err := manager.Create("session-1", "/workspace")
	if err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}

	tasks := []domain.Task{{ID: "task-1", Title: "One", DependsOn: []string{"task-0"}}}
	if err := manager.SetTasks(created.ID, tasks); err != nil {
		t.Fatalf("unexpected set tasks error: %v", err)
	}

	tasks[0].Title = "mutated"
	tasks[0].DependsOn[0] = "changed"
	loaded, _ := manager.Get(created.ID)
	if len(loaded.Tasks) != 1 {
		t.Fatalf("expected 1 tracked task, got %d", len(loaded.Tasks))
	}
	if loaded.Tasks[0].Title != "One" {
		t.Fatalf("expected cloned task title, got %q", loaded.Tasks[0].Title)
	}
	if loaded.Tasks[0].DependsOn[0] != "task-0" {
		t.Fatalf("expected cloned dependency, got %q", loaded.Tasks[0].DependsOn[0])
	}
	if len(loaded.ActiveTaskIDs) != 1 || loaded.ActiveTaskIDs[0] != "task-1" {
		t.Fatalf("expected active task ids derived from tasks, got %#v", loaded.ActiveTaskIDs)
	}
}

func TestRecordUsageAccumulatesTotals(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	created, err := manager.Create("session-1", "/workspace")
	if err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}

	totals, err := manager.RecordUsage(created.ID, UsageUpdate{InputTokens: 11, OutputTokens: 7, TotalCost: 0.01, PricingKnown: true, Provider: "openai", Model: "gpt-4o"})
	if err != nil {
		t.Fatalf("unexpected record usage error: %v", err)
	}
	if totals.InputTokens != 11 || totals.OutputTokens != 7 || totals.ResponseCount != 1 || totals.PricedResponses != 1 || totals.UnpricedResponses != 0 {
		t.Fatalf("unexpected usage totals after first record: %#v", totals)
	}

	totals, err = manager.RecordUsage(created.ID, UsageUpdate{InputTokens: 3, OutputTokens: 2, PricingKnown: false, Provider: "openai", Model: "gpt-4o"})
	if err != nil {
		t.Fatalf("unexpected second record usage error: %v", err)
	}
	if totals.InputTokens != 14 || totals.OutputTokens != 9 || totals.ResponseCount != 2 || totals.PricedResponses != 1 || totals.UnpricedResponses != 1 {
		t.Fatalf("unexpected usage totals after second record: %#v", totals)
	}
	loaded, _ := manager.Get(created.ID)
	if loaded.Usage.LastProvider != "openai" || loaded.Usage.LastModel != "gpt-4o" {
		t.Fatalf("expected last provider/model metadata, got %#v", loaded.Usage)
	}
	if loaded.Usage.TotalCost != 0.01 {
		t.Fatalf("expected only priced responses to contribute to total cost, got %#v", loaded.Usage)
	}
}

func TestUpdateTranscriptReplacesMatchingEntry(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	created, err := manager.Create("session-1", "/workspace")
	if err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}

	entry := domain.TranscriptEntry{ID: "entry-1", Kind: domain.TranscriptAssistant, Message: "hello"}
	if err := manager.AppendTranscript(created.ID, entry); err != nil {
		t.Fatalf("unexpected append error: %v", err)
	}

	entry.Message = "hello world"
	if err := manager.UpdateTranscript(created.ID, entry); err != nil {
		t.Fatalf("unexpected update error: %v", err)
	}

	loaded, _ := manager.Get(created.ID)
	if loaded.Transcript[0].Message != "hello world" {
		t.Fatalf("expected updated message, got %q", loaded.Transcript[0].Message)
	}
}

func TestReplaceTranscriptClonesEntries(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	created, err := manager.Create("session-1", "/workspace")
	if err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}

	entries := []domain.TranscriptEntry{{ID: "summary-1", Kind: domain.TranscriptSystem, Message: "summary"}}
	if err := manager.ReplaceTranscript(created.ID, entries); err != nil {
		t.Fatalf("unexpected replace error: %v", err)
	}

	entries[0].Message = "mutated"
	loaded, _ := manager.Get(created.ID)
	if loaded.Transcript[0].Message != "summary" {
		t.Fatalf("expected stored transcript to be cloned, got %q", loaded.Transcript[0].Message)
	}
}
