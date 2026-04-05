package session

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"crawler-ai/internal/domain"
)

func TestQueryServiceDiagnosticsClassifiesProviderToolAndFileIssues(t *testing.T) {
	dataDir := t.TempDir()
	mgr := NewManager()
	mgr.SetDataDir(dataDir)
	created, err := mgr.Create("diag-session", "/workspace")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	assistantAt := time.Date(2026, 4, 4, 11, 0, 0, 0, time.UTC)
	if err := mgr.AppendTranscript(created.ID, domain.TranscriptEntry{ID: "assistant-1", Kind: domain.TranscriptAssistant, Message: "partial", CreatedAt: assistantAt, UpdatedAt: assistantAt, Metadata: map[string]string{domain.TranscriptMetadataResponseID: "resp-1", "status": "in_progress"}}); err != nil {
		t.Fatalf("AppendTranscript(assistant) error: %v", err)
	}
	if err := mgr.AppendTranscript(created.ID, domain.TranscriptEntry{ID: "tool-1", Kind: domain.TranscriptTool, Message: "Tool: edit", CreatedAt: assistantAt.Add(time.Second), UpdatedAt: assistantAt.Add(time.Second), Metadata: map[string]string{"tool": "edit", "tool_call_id": "call-1", "status": "running"}}); err != nil {
		t.Fatalf("AppendTranscript(tool) error: %v", err)
	}
	if _, err := mgr.RecordUsage(created.ID, UsageUpdate{InputTokens: 4, OutputTokens: 2, TotalCost: 0.01, PricingKnown: true, Provider: "openai", Model: "gpt-4o-mini"}); err != nil {
		t.Fatalf("RecordUsage() error: %v", err)
	}
	if err := mgr.TrackFile(created.ID, FileRecord{ID: "file-1", Kind: FileRecordWorkspace, Path: "/workspace/main.go", Tool: "edit", CreatedAt: assistantAt, UpdatedAt: assistantAt}); err != nil {
		t.Fatalf("TrackFile(workspace) error: %v", err)
	}
	if err := mgr.TrackFile(created.ID, FileRecord{ID: "history-1", Kind: FileRecordHistory, Path: "/workspace/.crawler-ai/history/main.go", Tool: "edit", CreatedAt: assistantAt.Add(2 * time.Second), UpdatedAt: assistantAt.Add(2 * time.Second)}); err != nil {
		t.Fatalf("TrackFile(history) error: %v", err)
	}
	if err := mgr.Save(created.ID); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	queries := NewQueryService(mgr)
	queries.now = func() time.Time { return time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC) }
	report, err := queries.Diagnostics(created.ID)
	if err != nil {
		t.Fatalf("Diagnostics() error: %v", err)
	}
	if report.Storage == nil || report.Storage.Backend != "sqlite" {
		t.Fatalf("expected sqlite storage health, got %#v", report.Storage)
	}
	if report.Messages == nil || report.Messages.Total != 2 {
		t.Fatalf("expected message diagnostics, got %#v", report.Messages)
	}
	if report.Messages.ProviderInProgress != 1 || report.Messages.MissingFinishReason != 1 {
		t.Fatalf("unexpected provider diagnostics: %#v", report.Messages)
	}
	if report.Files == nil || report.Files.Total != 2 || report.Files.MissingSourcePath != 1 {
		t.Fatalf("unexpected file diagnostics: %#v", report.Files)
	}
	if !hasDiagnosticFinding(report.Findings, "provider", "warning", "in-progress assistant or reasoning entries") {
		t.Fatalf("expected provider finding, got %#v", report.Findings)
	}
	if !hasDiagnosticFinding(report.Findings, "tool", "warning", "history-snapshot file records are missing source-path metadata") {
		t.Fatalf("expected tool file finding, got %#v", report.Findings)
	}
}

func TestQueryServiceDiagnosticsCapturesSectionErrors(t *testing.T) {
	dataDir := t.TempDir()
	mgr := NewManager()
	mgr.SetDataDir(dataDir)
	created, err := mgr.Create("diag-broken", "/workspace")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if err := mgr.AppendTranscript(created.ID, domain.TranscriptEntry{ID: "assistant-1", Kind: domain.TranscriptAssistant, Message: "hello", CreatedAt: time.Now().UTC(), Metadata: map[string]string{"finish_reason": "completed"}}); err != nil {
		t.Fatalf("AppendTranscript() error: %v", err)
	}
	if err := mgr.Save(created.ID); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	db, err := sql.Open("sqlite", NewSQLiteStore(dataDir).DatabasePath())
	if err != nil {
		t.Fatalf("sql.Open() error: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`DROP TABLE messages`); err != nil {
		t.Fatalf("DROP TABLE messages error: %v", err)
	}

	report, err := NewQueryService(mgr).Diagnostics(created.ID)
	if err != nil {
		t.Fatalf("Diagnostics() error: %v", err)
	}
	if !hasDiagnosticSection(report.SectionErrors, "messages") {
		t.Fatalf("expected messages section error, got %#v", report.SectionErrors)
	}
	if !hasDiagnosticFinding(report.Findings, "storage", "warning", "persisted message inspection failed") {
		t.Fatalf("expected storage warning, got %#v", report.Findings)
	}
}

func TestHistoryQueryServiceSeparatesPromptAndFileHistory(t *testing.T) {
	dataDir := t.TempDir()
	mgr := NewManager()
	mgr.SetDataDir(dataDir)
	created, err := mgr.Create("history-query", "/workspace")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if err := mgr.AppendTranscript(created.ID, domain.TranscriptEntry{ID: "user-1", Kind: domain.TranscriptUser, Message: "do work", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("AppendTranscript(user) error: %v", err)
	}
	if err := mgr.AppendTranscript(created.ID, domain.TranscriptEntry{ID: "reasoning-1", Kind: domain.TranscriptReasoning, Message: "thinking", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("AppendTranscript(reasoning) error: %v", err)
	}
	if err := mgr.AppendTranscript(created.ID, domain.TranscriptEntry{ID: "assistant-1", Kind: domain.TranscriptAssistant, Message: "partial", CreatedAt: time.Now().UTC(), Metadata: map[string]string{"status": "in_progress"}}); err != nil {
		t.Fatalf("AppendTranscript(assistant) error: %v", err)
	}
	for _, file := range []FileRecord{
		{ID: "workspace-1", Kind: FileRecordWorkspace, Path: "notes/todo.txt", Tool: "write_file", CreatedAt: time.Now().UTC()},
		{ID: "history-1", Kind: FileRecordHistory, Path: ".crawler-ai/history/notes_todo.txt.bak", Tool: "edit_file", CreatedAt: time.Now().UTC(), Metadata: map[string]string{"source_path": "notes/todo.txt"}},
	} {
		if err := mgr.TrackFile(created.ID, file); err != nil {
			t.Fatalf("TrackFile(%s) error: %v", file.ID, err)
		}
	}
	if err := mgr.Save(created.ID); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	history := NewHistoryQueryService(mgr)
	promptHistory, err := history.PromptHistory(created.ID)
	if err != nil {
		t.Fatalf("PromptHistory() error: %v", err)
	}
	if len(promptHistory) != 1 || promptHistory[0].Kind != domain.TranscriptUser {
		t.Fatalf("unexpected prompt history: %#v", promptHistory)
	}
	fileHistory, err := history.FileHistory(created.ID)
	if err != nil {
		t.Fatalf("FileHistory() error: %v", err)
	}
	if len(fileHistory.Files) != 2 || len(fileHistory.WorkspaceFiles) != 1 || len(fileHistory.HistorySnapshots) != 1 {
		t.Fatalf("unexpected file history projection: %#v", fileHistory)
	}
}

func TestHistoryQueryServiceFiltersFileHistoryByKind(t *testing.T) {
	dataDir := t.TempDir()
	mgr := NewManager()
	mgr.SetDataDir(dataDir)
	created, err := mgr.Create("history-filter", "/workspace")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	for _, file := range []FileRecord{
		{ID: "workspace-1", Kind: FileRecordWorkspace, Path: "notes/todo.txt", Tool: "write_file", CreatedAt: time.Now().UTC()},
		{ID: "history-1", Kind: FileRecordHistory, Path: ".crawler-ai/history/notes_todo.txt.bak", Tool: "edit_file", CreatedAt: time.Now().UTC(), Metadata: map[string]string{"source_path": "notes/todo.txt"}},
	} {
		if err := mgr.TrackFile(created.ID, file); err != nil {
			t.Fatalf("TrackFile(%s) error: %v", file.ID, err)
		}
	}
	if err := mgr.Save(created.ID); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	fileHistory, err := NewHistoryQueryService(mgr).FilteredFileHistory(created.ID, FileHistoryOptions{Kinds: []FileRecordKind{FileRecordHistory}})
	if err != nil {
		t.Fatalf("FilteredFileHistory() error: %v", err)
	}
	if len(fileHistory.Files) != 1 || fileHistory.Files[0].ID != "history-1" {
		t.Fatalf("expected history-only files, got %#v", fileHistory)
	}
	if len(fileHistory.WorkspaceFiles) != 0 || len(fileHistory.HistorySnapshots) != 1 {
		t.Fatalf("expected filtered history buckets, got %#v", fileHistory)
	}
}

func TestTaskQueryServiceFiltersProjectionByStatus(t *testing.T) {
	dataDir := t.TempDir()
	mgr := NewManager()
	mgr.SetDataDir(dataDir)
	created, err := mgr.Create("task-filter", "/workspace")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if err := mgr.SetTasks(created.ID, []domain.Task{
		{ID: "task-running", Title: "active", Status: domain.TaskRunning},
		{ID: "task-completed", Title: "done", Status: domain.TaskCompleted},
	}); err != nil {
		t.Fatalf("SetTasks() error: %v", err)
	}
	created.ActiveTaskIDs = []string{"task-running"}
	if err := mgr.Save(created.ID); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	projection, err := NewTaskQueryService(mgr).FilteredProjection(created.ID, TaskQueryOptions{Statuses: []domain.TaskStatus{domain.TaskRunning}})
	if err != nil {
		t.Fatalf("FilteredProjection() error: %v", err)
	}
	if len(projection.Tasks) != 1 || projection.Tasks[0].ID != "task-running" {
		t.Fatalf("expected running task only, got %#v", projection)
	}
	if len(projection.ActiveTaskIDs) != 1 || projection.ActiveTaskIDs[0] != "task-running" {
		t.Fatalf("expected filtered active ids, got %#v", projection.ActiveTaskIDs)
	}
}

func TestQueryServiceShowAndExportUseQueryOwnedProjections(t *testing.T) {
	dataDir := t.TempDir()
	mgr := NewManager()
	mgr.SetDataDir(dataDir)
	created, err := mgr.Create("query-views", "/workspace")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if err := mgr.AppendTranscript(created.ID, domain.TranscriptEntry{ID: "assistant-1", Kind: domain.TranscriptAssistant, Message: "hello", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("AppendTranscript() error: %v", err)
	}
	if err := mgr.SetTasks(created.ID, []domain.Task{{ID: "task-1", Title: "secret title", Description: "secret description", Result: "secret result", Status: domain.TaskCompleted}}); err != nil {
		t.Fatalf("SetTasks() error: %v", err)
	}
	if _, err := mgr.RecordUsage(created.ID, UsageUpdate{InputTokens: 3, OutputTokens: 4, TotalCost: 0.02, PricingKnown: true, Provider: "openai", Model: "gpt-4o-mini"}); err != nil {
		t.Fatalf("RecordUsage() error: %v", err)
	}
	if err := mgr.Save(created.ID); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	queries := NewQueryService(mgr)
	show, err := queries.Show(created.ID)
	if err != nil {
		t.Fatalf("Show() error: %v", err)
	}
	if show.Summary.ID != created.ID || len(show.Transcript) != 1 || show.Transcript[0].Message != "hello" {
		t.Fatalf("unexpected show projection: %#v", show)
	}
	redacted, err := queries.Export(created.ID, "redacted")
	if err != nil {
		t.Fatalf("Export() error: %v", err)
	}
	if redacted.WorkspaceRoot != "[redacted]" || len(redacted.Transcript) != 1 || redacted.Transcript[0].Message != "[redacted]" {
		t.Fatalf("unexpected redacted export: %#v", redacted)
	}
	if len(redacted.Tasks) != 1 || redacted.Tasks[0].Title != "[redacted]" || redacted.Usage == nil || redacted.Usage.TotalCost != 0.02 {
		t.Fatalf("unexpected export projection: %#v", redacted)
	}
}

func hasDiagnosticFinding(findings []DiagnosticFinding, category, severity, contains string) bool {
	for _, finding := range findings {
		if finding.Category == category && finding.Severity == severity && strings.Contains(finding.Message, contains) {
			return true
		}
	}
	return false
}

func hasDiagnosticSection(items []DiagnosticSectionError, section string) bool {
	for _, item := range items {
		if item.Section == section {
			return true
		}
	}
	return false
}
