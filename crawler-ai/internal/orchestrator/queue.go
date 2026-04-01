package orchestrator

import (
	"time"

	"crawler-ai/internal/domain"
	apperrors "crawler-ai/internal/errors"
)

type Queue struct {
	tasks map[string]domain.Task
	order []string
	now   func() time.Time
}

func NewQueue(tasks []domain.Task) *Queue {
	items := make(map[string]domain.Task, len(tasks))
	order := make([]string, 0, len(tasks))
	for _, task := range tasks {
		items[task.ID] = task
		order = append(order, task.ID)
	}

	return &Queue{
		tasks: items,
		order: order,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (q *Queue) List() []domain.Task {
	items := make([]domain.Task, 0, len(q.order))
	for _, id := range q.order {
		items = append(items, q.tasks[id])
	}
	return items
}

func (q *Queue) NextReady() (domain.Task, bool) {
	for _, id := range q.order {
		task := q.tasks[id]
		if task.Status != domain.TaskPending {
			continue
		}
		if q.dependenciesComplete(task) {
			return task, true
		}
	}
	return domain.Task{}, false
}

func (q *Queue) Start(id string) error {
	return q.update(id, domain.TaskRunning, "")
}

func (q *Queue) Complete(id, result string) error {
	return q.update(id, domain.TaskCompleted, result)
}

func (q *Queue) Fail(id, result string) error {
	return q.update(id, domain.TaskFailed, result)
}

func (q *Queue) IsFinished() bool {
	for _, id := range q.order {
		status := q.tasks[id].Status
		if status != domain.TaskCompleted && status != domain.TaskFailed {
			return false
		}
	}
	return true
}

func (q *Queue) update(id string, status domain.TaskStatus, result string) error {
	task, ok := q.tasks[id]
	if !ok {
		return apperrors.New("orchestrator.Queue.update", apperrors.CodeInvalidArgument, "task not found")
	}
	task.Status = status
	task.Result = result
	task.UpdatedAt = q.now()
	q.tasks[id] = task
	return nil
}

func (q *Queue) dependenciesComplete(task domain.Task) bool {
	for _, dependencyID := range task.DependsOn {
		dependency, ok := q.tasks[dependencyID]
		if !ok || dependency.Status != domain.TaskCompleted {
			return false
		}
	}
	return true
}
