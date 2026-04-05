package session

import (
	"crawler-ai/internal/domain"
	apperrors "crawler-ai/internal/errors"
)

type TaskQueryReader interface {
	ReadPersistedTasks(id string) ([]domain.Task, error)
	ReadPersistedSummary(id string) (SessionSummary, error)
}

type TaskProjection struct {
	SessionID     string        `json:"session_id"`
	Tasks         []domain.Task `json:"tasks,omitempty"`
	ActiveTaskIDs []string      `json:"active_task_ids,omitempty"`
}

type TaskQueryOptions struct {
	Statuses []domain.TaskStatus
}

type TaskQueryService struct {
	reader TaskQueryReader
}

func NewTaskQueryService(reader TaskQueryReader) *TaskQueryService {
	return &TaskQueryService{reader: reader}
}

func (s *TaskQueryService) Tasks(id string) ([]domain.Task, error) {
	if s == nil || s.reader == nil {
		return nil, apperrors.New("session.TaskQueryService.Tasks", apperrors.CodeStartupFailed, "task query service is not configured")
	}
	items, err := s.reader.ReadPersistedTasks(id)
	if err != nil {
		return nil, err
	}
	return cloneTasks(items), nil
}

func (s *TaskQueryService) Projection(id string) (TaskProjection, error) {
	return s.FilteredProjection(id, TaskQueryOptions{})
}

func (s *TaskQueryService) FilteredProjection(id string, options TaskQueryOptions) (TaskProjection, error) {
	if s == nil || s.reader == nil {
		return TaskProjection{}, apperrors.New("session.TaskQueryService.FilteredProjection", apperrors.CodeStartupFailed, "task query service is not configured")
	}
	summary, err := s.reader.ReadPersistedSummary(id)
	if err != nil {
		return TaskProjection{}, err
	}
	tasks, err := s.Tasks(id)
	if err != nil {
		return TaskProjection{}, err
	}
	filteredTasks := filterTasksByStatus(tasks, options.Statuses)
	return TaskProjection{
		SessionID:     id,
		Tasks:         filteredTasks,
		ActiveTaskIDs: filterActiveTaskIDs(summary.ActiveTaskIDs, filteredTasks),
	}, nil
}

func filterTasksByStatus(tasks []domain.Task, statuses []domain.TaskStatus) []domain.Task {
	if len(statuses) == 0 {
		return cloneTasks(tasks)
	}
	allowed := make(map[domain.TaskStatus]struct{}, len(statuses))
	for _, status := range statuses {
		allowed[status] = struct{}{}
	}
	filtered := make([]domain.Task, 0, len(tasks))
	for _, task := range tasks {
		if _, ok := allowed[task.Status]; !ok {
			continue
		}
		filtered = append(filtered, cloneTasks([]domain.Task{task})...)
	}
	return filtered
}

func filterActiveTaskIDs(activeTaskIDs []string, tasks []domain.Task) []string {
	if len(activeTaskIDs) == 0 || len(tasks) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		allowed[task.ID] = struct{}{}
	}
	filtered := make([]string, 0, len(activeTaskIDs))
	for _, id := range activeTaskIDs {
		if _, ok := allowed[id]; ok {
			filtered = append(filtered, id)
		}
	}
	return filtered
}
