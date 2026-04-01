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
