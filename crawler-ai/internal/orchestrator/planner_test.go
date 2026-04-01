package orchestrator

import (
	"testing"
	"time"

	"crawler-ai/internal/domain"
)

func TestPlannerBuildCreatesPhasedTasks(t *testing.T) {
	t.Parallel()

	planner := NewPlanner()
	fixedNow := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	planner.now = func() time.Time { return fixedNow }

	tasks := planner.Build("build feature and test it")
	if len(tasks) < 4 {
		t.Fatalf("expected at least 4 tasks, got %d", len(tasks))
	}
	if tasks[0].Assignee != domain.RoleOrchestrator {
		t.Fatalf("expected first task to be orchestrator-owned, got %s", tasks[0].Assignee)
	}
	if tasks[len(tasks)-1].Assignee != domain.RoleReviewer {
		t.Fatalf("expected final task to be reviewer-owned, got %s", tasks[len(tasks)-1].Assignee)
	}
}
