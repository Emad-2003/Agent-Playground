package session

import (
	"strings"
	"time"

	"crawler-ai/internal/domain"
	apperrors "crawler-ai/internal/errors"
)

type SessionShowProjection struct {
	Summary    SessionSummary           `json:"summary"`
	Transcript []domain.TranscriptEntry `json:"transcript,omitempty"`
	Children   []SessionSummary         `json:"children,omitempty"`
}

type SessionReadProjection struct {
	Summary       SessionSummary           `json:"summary"`
	Transcript    []domain.TranscriptEntry `json:"transcript,omitempty"`
	Children      []SessionSummary         `json:"children,omitempty"`
	Tasks         []domain.Task            `json:"tasks,omitempty"`
	ActiveTaskIDs []string                 `json:"active_task_ids,omitempty"`
	Files         []FileRecord             `json:"files,omitempty"`
	Usage         UsageTotals              `json:"usage"`
}

type SessionExportProjection struct {
	Filter        string                   `json:"filter"`
	SessionID     string                   `json:"session_id"`
	WorkspaceRoot string                   `json:"workspace_root,omitempty"`
	CreatedAt     time.Time                `json:"created_at"`
	UpdatedAt     time.Time                `json:"updated_at"`
	Transcript    []domain.TranscriptEntry `json:"transcript,omitempty"`
	Tasks         []domain.Task            `json:"tasks,omitempty"`
	ActiveTaskIDs []string                 `json:"active_task_ids,omitempty"`
	Usage         *UsageTotals             `json:"usage,omitempty"`
}

func (s *QueryService) History() *HistoryQueryService {
	if s == nil {
		return nil
	}
	return s.history
}

func (s *QueryService) PromptHistoryService() *PromptHistoryService {
	if s == nil {
		return nil
	}
	return s.promptHistory
}

func (s *QueryService) FileHistoryService() *FileHistoryService {
	if s == nil {
		return nil
	}
	return s.fileHistory
}

func (s *QueryService) CodingContextService() *CodingContextService {
	if s == nil {
		return nil
	}
	return s.codingContext
}

func (s *QueryService) Tasks() *TaskQueryService {
	if s == nil {
		return nil
	}
	return s.tasks
}

func (s *QueryService) UsageService() *UsageQueryService {
	if s == nil {
		return nil
	}
	return s.usage
}

func (s *QueryService) Messages(id string) ([]domain.TranscriptEntry, error) {
	if s == nil || s.promptHistory == nil {
		return nil, apperrors.New("session.QueryService.Messages", apperrors.CodeStartupFailed, "persisted query service is not configured")
	}
	return s.promptHistory.Messages(id)
}

func (s *QueryService) Show(id string) (SessionShowProjection, error) {
	summary, err := s.Summary(id)
	if err != nil {
		return SessionShowProjection{}, err
	}
	transcript, err := s.Messages(id)
	if err != nil {
		return SessionShowProjection{}, err
	}
	children, err := s.ChildSummaries(id)
	if err != nil {
		return SessionShowProjection{}, err
	}
	return SessionShowProjection{Summary: summary, Transcript: transcript, Children: children}, nil
}

func (s *QueryService) FullSession(id string) (SessionReadProjection, error) {
	summary, err := s.Summary(id)
	if err != nil {
		return SessionReadProjection{}, err
	}
	transcript, err := s.Messages(id)
	if err != nil {
		return SessionReadProjection{}, err
	}
	children, err := s.ChildSummaries(id)
	if err != nil {
		return SessionReadProjection{}, err
	}
	taskProjection, err := s.tasks.Projection(id)
	if err != nil {
		return SessionReadProjection{}, err
	}
	fileHistory, err := s.fileHistory.FileHistory(id)
	if err != nil {
		return SessionReadProjection{}, err
	}
	usageProjection, err := s.usage.Projection(id)
	if err != nil {
		return SessionReadProjection{}, err
	}
	return SessionReadProjection{
		Summary:       summary,
		Transcript:    transcript,
		Children:      append([]SessionSummary(nil), children...),
		Tasks:         cloneTasks(taskProjection.Tasks),
		ActiveTaskIDs: append([]string(nil), taskProjection.ActiveTaskIDs...),
		Files:         cloneFiles(fileHistory.Files),
		Usage:         usageProjection.Usage,
	}, nil
}

func (s *QueryService) Export(id, filter string) (SessionExportProjection, error) {
	summary, err := s.Summary(id)
	if err != nil {
		return SessionExportProjection{}, err
	}
	transcript, err := s.Messages(id)
	if err != nil {
		return SessionExportProjection{}, err
	}
	taskProjection, err := s.tasks.Projection(id)
	if err != nil {
		return SessionExportProjection{}, err
	}
	usage, err := s.Usage(id)
	if err != nil {
		return SessionExportProjection{}, err
	}
	return buildSessionExportProjection(summary, transcript, taskProjection, usage, filter), nil
}

func buildSessionExportProjection(summary SessionSummary, transcript []domain.TranscriptEntry, taskProjection TaskProjection, usage UsageTotals, filter string) SessionExportProjection {
	projection := SessionExportProjection{
		Filter:        filter,
		SessionID:     summary.ID,
		WorkspaceRoot: summary.WorkspaceRoot,
		CreatedAt:     summary.CreatedAt,
		UpdatedAt:     summary.UpdatedAt,
	}
	switch filter {
	case "transcript":
		projection.Transcript = cloneTranscript(transcript)
	case "usage":
		projection.Usage = cloneUsageTotalsProjection(usage)
	case "redacted":
		projection.WorkspaceRoot = "[redacted]"
		projection.Transcript = redactTranscriptForExport(transcript)
		projection.Tasks = redactTasksForExport(taskProjection.Tasks)
		projection.ActiveTaskIDs = append([]string(nil), taskProjection.ActiveTaskIDs...)
		projection.Usage = cloneUsageTotalsProjection(usage)
	default:
		projection.Transcript = cloneTranscript(transcript)
		projection.Tasks = cloneTasks(taskProjection.Tasks)
		projection.ActiveTaskIDs = append([]string(nil), taskProjection.ActiveTaskIDs...)
		projection.Usage = cloneUsageTotalsProjection(usage)
	}
	return projection
}

func cloneUsageTotalsProjection(usage UsageTotals) *UsageTotals {
	cloned := usage
	if cloned == (UsageTotals{}) {
		return nil
	}
	return &cloned
}

func redactTranscriptForExport(entries []domain.TranscriptEntry) []domain.TranscriptEntry {
	cloned := cloneTranscript(entries)
	for index := range cloned {
		if strings.TrimSpace(cloned[index].Message) != "" {
			cloned[index].Message = "[redacted]"
		}
	}
	return cloned
}

func redactTasksForExport(tasks []domain.Task) []domain.Task {
	cloned := cloneTasks(tasks)
	for index := range cloned {
		if strings.TrimSpace(cloned[index].Title) != "" {
			cloned[index].Title = "[redacted]"
		}
		if strings.TrimSpace(cloned[index].Description) != "" {
			cloned[index].Description = "[redacted]"
		}
		if strings.TrimSpace(cloned[index].Result) != "" {
			cloned[index].Result = "[redacted]"
		}
	}
	return cloned
}
