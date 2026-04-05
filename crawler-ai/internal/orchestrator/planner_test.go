package orchestrator

import (
	"strings"
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
	if tasks[1].Description == "Execute the main requested implementation work in the workspace." {
		t.Fatalf("expected implementation task to preserve original prompt context, got %q", tasks[1].Description)
	}
	if tasks[1].Description == "" || tasks[2].Description == "" || tasks[len(tasks)-1].Description == "" {
		t.Fatalf("expected non-empty task descriptions, got %#v", tasks)
	}
	if !strings.Contains(tasks[1].Description, "build feature and test it") {
		t.Fatalf("expected prompt text in implementation task description, got %q", tasks[1].Description)
	}
}
