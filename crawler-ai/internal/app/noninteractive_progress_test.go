package app

import (
	"testing"
	"time"

	"crawler-ai/internal/domain"
	"crawler-ai/internal/events"
	"crawler-ai/internal/runtime"
)

func TestNonInteractiveReasoningProgressEventMapsReasoningEntries(t *testing.T) {
	t.Parallel()

	event := events.Event{
		Name:      events.EventTranscriptUpdated,
		Timestamp: time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC),
		Payload: domain.TranscriptEntry{
			ID:      "reasoning-1",
			Kind:    domain.TranscriptReasoning,
			Message: "Inspecting workspace structure.",
		},
	}

	progress, ok := nonInteractiveReasoningProgressEvent(event)
	if !ok {
		t.Fatal("expected reasoning event to map")
	}
	if progress.Kind != NonInteractiveProgressReasoning {
		t.Fatalf("expected reasoning kind, got %#v", progress)
	}
	if progress.ID != "reasoning-1" || progress.Message != "Inspecting workspace structure." {
		t.Fatalf("unexpected reasoning payload: %#v", progress)
	}
}

func TestNonInteractiveToolProgressEventMapsRuntimeEvents(t *testing.T) {
	t.Parallel()

	event := events.Event{
		Name:      runtime.EventToolCompleted,
		Timestamp: time.Date(2026, 4, 5, 12, 1, 0, 0, time.UTC),
		Payload: map[string]any{
			"call_id": "call-1",
			"tool":    "write_file",
			"path":    "index.html",
		},
	}

	progress, ok := nonInteractiveToolProgressEvent(event, NonInteractiveProgressToolCompleted)
	if !ok {
		t.Fatal("expected tool event to map")
	}
	if progress.Kind != NonInteractiveProgressToolCompleted {
		t.Fatalf("expected completed kind, got %#v", progress)
	}
	if progress.Tool != "write_file" || progress.Path != "index.html" || progress.ID != "call-1" {
		t.Fatalf("unexpected tool payload: %#v", progress)
	}
}

func TestSummarizeNonInteractiveTasksPrefersRunningTask(t *testing.T) {
	t.Parallel()

	message := summarizeNonInteractiveTasks([]domain.Task{{Title: "Inspect files", Status: domain.TaskRunning}, {Title: "Write app", Status: domain.TaskPending}})
	if message != "Running task: Inspect files" {
		t.Fatalf("unexpected running task summary: %q", message)
	}
}
