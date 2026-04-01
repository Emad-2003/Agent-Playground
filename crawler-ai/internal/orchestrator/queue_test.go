package orchestrator

import (
	"testing"

	"crawler-ai/internal/domain"
)

func TestQueueNextReadyRespectsDependencies(t *testing.T) {
	t.Parallel()

	queue := NewQueue([]domain.Task{
		{ID: "task-1", Title: "Analyze", Status: domain.TaskPending},
		{ID: "task-2", Title: "Implement", Status: domain.TaskPending, DependsOn: []string{"task-1"}},
	})

	next, ok := queue.NextReady()
	if !ok || next.ID != "task-1" {
		t.Fatalf("expected task-1 first, got %#v", next)
	}

	if err := queue.Start("task-1"); err != nil {
		t.Fatalf("unexpected start error: %v", err)
	}
	if _, ok := queue.NextReady(); ok {
		t.Fatal("expected no ready task while dependency is running")
	}

	if err := queue.Complete("task-1", "done"); err != nil {
		t.Fatalf("unexpected complete error: %v", err)
	}
	next, ok = queue.NextReady()
	if !ok || next.ID != "task-2" {
		t.Fatalf("expected task-2 after dependency completion, got %#v", next)
	}
}
