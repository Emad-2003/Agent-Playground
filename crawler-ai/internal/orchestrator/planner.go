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
	trimmedPrompt := strings.TrimSpace(prompt)
	normalized := strings.ToLower(trimmedPrompt)
	now := p.now()
	tasks := []domain.Task{
		newTask("task-1", "Analyze request", taskDescriptionWithPrompt("Break down the user request and determine the implementation approach.", trimmedPrompt), domain.RoleOrchestrator, nil, now),
	}

	lastDependency := []string{"task-1"}
	index := 2

	if containsAny(normalized, "build", "create", "write", "implement", "fix", "refactor") {
		id := fmt.Sprintf("task-%d", index)
		tasks = append(tasks, newTask(id, "Implement change", taskDescriptionWithPrompt("Execute the main requested implementation work in the workspace.", trimmedPrompt), domain.RoleWorker, lastDependency, now))
		lastDependency = []string{id}
		index++
	}

	if containsAny(normalized, "test", "verify", "validate", "check") {
		id := fmt.Sprintf("task-%d", index)
		tasks = append(tasks, newTask(id, "Verify outcome", taskDescriptionWithPrompt("Validate the implementation results and check for regressions.", trimmedPrompt), domain.RoleWorker, lastDependency, now))
		lastDependency = []string{id}
		index++
	}

	id := fmt.Sprintf("task-%d", index)
	tasks = append(tasks, newTask(id, "Summarize result", taskDescriptionWithPrompt("Summarize what changed, what succeeded, and any next actions.", trimmedPrompt), domain.RoleReviewer, lastDependency, now))
	return tasks
}

func taskDescriptionWithPrompt(baseDescription, prompt string) string {
	if strings.TrimSpace(prompt) == "" {
		return baseDescription
	}
	return baseDescription + " Original request: " + prompt
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
