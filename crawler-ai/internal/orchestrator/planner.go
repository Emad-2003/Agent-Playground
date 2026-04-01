package orchestrator

import (
	"fmt"
	"strings"
	"time"

	"crawler-ai/internal/domain"
)

type Planner struct {
	now func() time.Time
}

func NewPlanner() *Planner {
	return &Planner{
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (p *Planner) Build(prompt string) []domain.Task {
	normalized := strings.ToLower(strings.TrimSpace(prompt))
	now := p.now()
	tasks := []domain.Task{
		newTask("task-1", "Analyze request", "Break down the user request and determine the implementation approach.", domain.RoleOrchestrator, nil, now),
	}

	lastDependency := []string{"task-1"}
	index := 2

	if containsAny(normalized, "build", "create", "write", "implement", "fix", "refactor") {
		id := fmt.Sprintf("task-%d", index)
		tasks = append(tasks, newTask(id, "Implement change", "Execute the main requested implementation work in the workspace.", domain.RoleWorker, lastDependency, now))
		lastDependency = []string{id}
		index++
	}

	if containsAny(normalized, "test", "verify", "validate", "check") {
		id := fmt.Sprintf("task-%d", index)
		tasks = append(tasks, newTask(id, "Verify outcome", "Validate the implementation results and check for regressions.", domain.RoleWorker, lastDependency, now))
		lastDependency = []string{id}
		index++
	}

	id := fmt.Sprintf("task-%d", index)
	tasks = append(tasks, newTask(id, "Summarize result", "Summarize what changed, what succeeded, and any next actions.", domain.RoleReviewer, lastDependency, now))
	return tasks
}

func newTask(id, title, description string, role domain.AgentRole, dependsOn []string, now time.Time) domain.Task {
	clonedDependsOn := append([]string(nil), dependsOn...)
	return domain.Task{
		ID:          id,
		Title:       title,
		Description: description,
		Status:      domain.TaskPending,
		Assignee:    role,
		DependsOn:   clonedDependsOn,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func containsAny(input string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(input, candidate) {
			return true
		}
	}
	return false
}
