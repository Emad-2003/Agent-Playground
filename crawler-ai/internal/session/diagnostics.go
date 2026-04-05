package session

import (
	"sort"
	"strings"
	"time"

	"crawler-ai/internal/domain"
	apperrors "crawler-ai/internal/errors"
)

type PersistedQueryReader interface {
	ReadPersistedSummary(id string) (SessionSummary, error)
	ListPersistedSummaries() ([]SessionSummary, error)
	ReadPersistedMessages(id string) ([]domain.TranscriptEntry, error)
	ReadPersistedTasks(id string) ([]domain.Task, error)
	ReadPersistedFiles(id string) ([]FileRecord, error)
	ReadPersistedUsage(id string) (UsageTotals, error)
	ReadStorageHealth() (StorageHealth, error)
}

type DiagnosticCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type DiagnosticFinding struct {
	Category string `json:"category"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type DiagnosticSectionError struct {
	Section string `json:"section"`
	Error   string `json:"error"`
}

type MessageDiagnostics struct {
	Total               int               `json:"total"`
	ByKind              []DiagnosticCount `json:"by_kind,omitempty"`
	AssistantResponses  int               `json:"assistant_responses"`
	ReasoningEntries    int               `json:"reasoning_entries"`
	ProviderInProgress  int               `json:"provider_in_progress"`
	MissingFinishReason int               `json:"missing_finish_reason"`
	FinishReasons       []DiagnosticCount `json:"finish_reasons,omitempty"`
	ToolStatuses        []DiagnosticCount `json:"tool_statuses,omitempty"`
	LastUpdatedAt       time.Time         `json:"last_updated_at,omitempty"`
}

type FileDiagnostics struct {
	Total             int               `json:"total"`
	ByKind            []DiagnosticCount `json:"by_kind,omitempty"`
	ByTool            []DiagnosticCount `json:"by_tool,omitempty"`
	HistorySnapshots  int               `json:"history_snapshots"`
	MissingSourcePath int               `json:"missing_source_path"`
	LastTrackedAt     time.Time         `json:"last_tracked_at,omitempty"`
}

type StorageRecordCounts struct {
	Sessions    int `json:"sessions"`
	Messages    int `json:"messages"`
	Tasks       int `json:"tasks"`
	Files       int `json:"files"`
	UsageTotals int `json:"usage_totals"`
}

type StorageHealth struct {
	Backend             string              `json:"backend"`
	DataDir             string              `json:"data_dir,omitempty"`
	Path                string              `json:"path,omitempty"`
	SchemaVersion       int                 `json:"schema_version,omitempty"`
	LatestSchemaVersion int                 `json:"latest_schema_version,omitempty"`
	Counts              StorageRecordCounts `json:"counts"`
	Notes               []string            `json:"notes,omitempty"`
}

type SessionDiagnosticsReport struct {
	GeneratedAt   time.Time                `json:"generated_at"`
	SessionID     string                   `json:"session_id"`
	Summary       SessionSummary           `json:"summary"`
	Storage       *StorageHealth           `json:"storage,omitempty"`
	Messages      *MessageDiagnostics      `json:"messages,omitempty"`
	Files         *FileDiagnostics         `json:"files,omitempty"`
	Findings      []DiagnosticFinding      `json:"findings,omitempty"`
	SectionErrors []DiagnosticSectionError `json:"section_errors,omitempty"`
}

type QueryService struct {
	reader        PersistedQueryReader
	history       *HistoryQueryService
	promptHistory *PromptHistoryService
	fileHistory   *FileHistoryService
	codingContext *CodingContextService
	tasks         *TaskQueryService
	usage         *UsageQueryService
	now           func() time.Time
}

func NewQueryService(reader PersistedQueryReader) *QueryService {
	history := NewHistoryQueryService(reader)
	promptHistory := NewPromptHistoryService(reader)
	fileHistory := NewFileHistoryService(reader)
	codingContext := NewCodingContextService(reader, NewCommandWorkspaceDiagnosticsProvider())
	tasks := NewTaskQueryService(reader)
	usage := NewUsageQueryService(reader)
	return &QueryService{
		reader:        reader,
		history:       history,
		promptHistory: promptHistory,
		fileHistory:   fileHistory,
		codingContext: codingContext,
		tasks:         tasks,
		usage:         usage,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (s *QueryService) Summary(id string) (SessionSummary, error) {
	if s == nil || s.reader == nil {
		return SessionSummary{}, apperrors.New("session.QueryService.Summary", apperrors.CodeStartupFailed, "persisted query service is not configured")
	}
	return s.reader.ReadPersistedSummary(id)
}

func (s *QueryService) ListSummaries() ([]SessionSummary, error) {
	if s == nil || s.reader == nil {
		return nil, apperrors.New("session.QueryService.ListSummaries", apperrors.CodeStartupFailed, "persisted query service is not configured")
	}
	return s.reader.ListPersistedSummaries()
}

func (s *QueryService) Usage(id string) (UsageTotals, error) {
	if s == nil || s.usage == nil {
		return UsageTotals{}, apperrors.New("session.QueryService.Usage", apperrors.CodeStartupFailed, "persisted query service is not configured")
	}
	return s.usage.Usage(id)
}

func (s *QueryService) Diagnostics(id string) (SessionDiagnosticsReport, error) {
	if s == nil || s.reader == nil {
		return SessionDiagnosticsReport{}, apperrors.New("session.QueryService.Diagnostics", apperrors.CodeStartupFailed, "persisted query service is not configured")
	}

	summary, err := s.reader.ReadPersistedSummary(id)
	if err != nil {
		return SessionDiagnosticsReport{}, err
	}

	report := SessionDiagnosticsReport{
		GeneratedAt: s.now(),
		SessionID:   id,
		Summary:     summary,
		Findings:    make([]DiagnosticFinding, 0),
	}

	if health, healthErr := s.reader.ReadStorageHealth(); healthErr != nil {
		report.addSectionError("storage", healthErr)
		report.addFinding("storage", "warning", "storage health inspection failed: "+healthErr.Error())
	} else {
		report.Storage = &health
		if health.SchemaVersion > 0 && health.LatestSchemaVersion > 0 && health.SchemaVersion != health.LatestSchemaVersion {
			report.addFinding("storage", "warning", "storage schema version does not match the latest supported schema version")
		}
	}

	if messages, messagesErr := s.promptHistory.Messages(id); messagesErr != nil {
		report.addSectionError("messages", messagesErr)
		report.addFinding("storage", "warning", "persisted message inspection failed: "+messagesErr.Error())
	} else {
		diagnostics, findings := buildMessageDiagnostics(summary, messages)
		report.Messages = &diagnostics
		report.Findings = append(report.Findings, findings...)
	}

	if fileHistory, filesErr := s.fileHistory.FileHistory(id); filesErr != nil {
		report.addSectionError("files", filesErr)
		report.addFinding("storage", "warning", "tracked file inspection failed: "+filesErr.Error())
	} else {
		diagnostics, findings := buildFileDiagnostics(summary, fileHistory.Files)
		report.Files = &diagnostics
		report.Findings = append(report.Findings, findings...)
	}

	sort.SliceStable(report.Findings, func(i, j int) bool {
		if severityRank(report.Findings[i].Severity) != severityRank(report.Findings[j].Severity) {
			return severityRank(report.Findings[i].Severity) < severityRank(report.Findings[j].Severity)
		}
		if report.Findings[i].Category != report.Findings[j].Category {
			return report.Findings[i].Category < report.Findings[j].Category
		}
		return report.Findings[i].Message < report.Findings[j].Message
	})

	return report, nil
}

func buildMessageDiagnostics(summary SessionSummary, messages []domain.TranscriptEntry) (MessageDiagnostics, []DiagnosticFinding) {
	diagnostics := MessageDiagnostics{
		Total: messagesCount(messages),
	}
	kindCounts := make(map[string]int)
	finishCounts := make(map[string]int)
	toolStatusCounts := make(map[string]int)
	findings := make([]DiagnosticFinding, 0)

	for _, entry := range messages {
		kindCounts[string(entry.Kind)]++
		lastAt := entry.CreatedAt
		if entry.UpdatedAt.After(lastAt) {
			lastAt = entry.UpdatedAt
		}
		if lastAt.After(diagnostics.LastUpdatedAt) {
			diagnostics.LastUpdatedAt = lastAt
		}

		switch entry.Kind {
		case domain.TranscriptAssistant:
			diagnostics.AssistantResponses++
			if strings.EqualFold(strings.TrimSpace(entry.Metadata["status"]), "in_progress") {
				diagnostics.ProviderInProgress++
			}
			finishReason := strings.TrimSpace(entry.Metadata["finish_reason"])
			if finishReason == "" {
				diagnostics.MissingFinishReason++
			} else {
				finishCounts[finishReason]++
			}
		case domain.TranscriptReasoning:
			diagnostics.ReasoningEntries++
			if strings.EqualFold(strings.TrimSpace(entry.Metadata["status"]), "in_progress") {
				diagnostics.ProviderInProgress++
			}
			finishReason := strings.TrimSpace(entry.Metadata["finish_reason"])
			if finishReason != "" {
				finishCounts[finishReason]++
			}
		case domain.TranscriptTool:
			status := strings.TrimSpace(entry.Metadata["status"])
			if status == "" {
				status = "unknown"
			}
			toolStatusCounts[status]++
		}
	}

	diagnostics.ByKind = sortedDiagnosticCounts(kindCounts)
	diagnostics.FinishReasons = sortedDiagnosticCounts(finishCounts)
	diagnostics.ToolStatuses = sortedDiagnosticCounts(toolStatusCounts)

	if summary.MessageCount != diagnostics.Total {
		findings = append(findings, DiagnosticFinding{
			Category: "storage",
			Severity: "warning",
			Message:  "persisted message count does not match the stored session summary",
		})
	}
	if diagnostics.ProviderInProgress > 0 {
		findings = append(findings, DiagnosticFinding{
			Category: "provider",
			Severity: "warning",
			Message:  "provider transcript contains in-progress assistant or reasoning entries, which suggests an interrupted or incomplete response",
		})
	}
	if diagnostics.MissingFinishReason > 0 {
		findings = append(findings, DiagnosticFinding{
			Category: "provider",
			Severity: "warning",
			Message:  "one or more assistant responses are missing finish reasons in persisted transcript records",
		})
	}
	if summary.Usage.ResponseCount > 0 && diagnostics.AssistantResponses == 0 {
		findings = append(findings, DiagnosticFinding{
			Category: "provider",
			Severity: "warning",
			Message:  "usage totals show provider responses but no persisted assistant transcript entries were found",
		})
	}
	if count := toolStatusCounts["pending"] + toolStatusCounts["running"] + toolStatusCounts["unknown"]; count > 0 {
		findings = append(findings, DiagnosticFinding{
			Category: "tool",
			Severity: "warning",
			Message:  "tool transcript lifecycle contains pending, running, or unknown states that may indicate interrupted tool execution",
		})
	}
	if toolStatusCounts["failed"] > 0 {
		findings = append(findings, DiagnosticFinding{
			Category: "tool",
			Severity: "info",
			Message:  "persisted tool transcript includes failed tool calls; inspect tool output to separate runtime failures from storage issues",
		})
	}

	return diagnostics, findings
}

func buildFileDiagnostics(summary SessionSummary, files []FileRecord) (FileDiagnostics, []DiagnosticFinding) {
	diagnostics := FileDiagnostics{
		Total: len(files),
	}
	kindCounts := make(map[string]int)
	toolCounts := make(map[string]int)
	findings := make([]DiagnosticFinding, 0)

	for _, file := range files {
		kind := strings.TrimSpace(string(file.Kind))
		if kind == "" {
			kind = "unknown"
		}
		kindCounts[kind]++

		tool := strings.TrimSpace(file.Tool)
		if tool == "" {
			tool = "unknown"
		}
		toolCounts[tool]++

		lastAt := file.CreatedAt
		if file.UpdatedAt.After(lastAt) {
			lastAt = file.UpdatedAt
		}
		if lastAt.After(diagnostics.LastTrackedAt) {
			diagnostics.LastTrackedAt = lastAt
		}

		if file.Kind == FileRecordHistory {
			diagnostics.HistorySnapshots++
			if strings.TrimSpace(file.Metadata["source_path"]) == "" {
				diagnostics.MissingSourcePath++
			}
		}
	}

	diagnostics.ByKind = sortedDiagnosticCounts(kindCounts)
	diagnostics.ByTool = sortedDiagnosticCounts(toolCounts)

	if summary.FileCount != diagnostics.Total {
		findings = append(findings, DiagnosticFinding{
			Category: "storage",
			Severity: "warning",
			Message:  "tracked file count does not match the stored session summary",
		})
	}
	if diagnostics.MissingSourcePath > 0 {
		findings = append(findings, DiagnosticFinding{
			Category: "tool",
			Severity: "warning",
			Message:  "one or more persisted history-snapshot file records are missing source-path metadata",
		})
	}

	return diagnostics, findings
}

func (r *SessionDiagnosticsReport) addSectionError(section string, err error) {
	if r == nil || err == nil {
		return
	}
	r.SectionErrors = append(r.SectionErrors, DiagnosticSectionError{
		Section: section,
		Error:   err.Error(),
	})
}

func (r *SessionDiagnosticsReport) addFinding(category, severity, message string) {
	if r == nil || strings.TrimSpace(message) == "" {
		return
	}
	r.Findings = append(r.Findings, DiagnosticFinding{
		Category: category,
		Severity: severity,
		Message:  message,
	})
}

func messagesCount(messages []domain.TranscriptEntry) int {
	return len(messages)
}

func sortedDiagnosticCounts(counts map[string]int) []DiagnosticCount {
	if len(counts) == 0 {
		return nil
	}
	items := make([]DiagnosticCount, 0, len(counts))
	for name, count := range counts {
		items = append(items, DiagnosticCount{Name: name, Count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		return items[i].Name < items[j].Name
	})
	return items
}

func severityRank(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "error":
		return 0
	case "warning":
		return 1
	case "info":
		return 2
	default:
		return 3
	}
}
