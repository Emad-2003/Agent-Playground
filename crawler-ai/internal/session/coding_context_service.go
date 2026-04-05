package session

import (
	"fmt"
	"sort"
	"strings"
	"time"

	apperrors "crawler-ai/internal/errors"
)

type codingContextReader interface {
	ReadPersistedSummary(id string) (SessionSummary, error)
	ReadPersistedFiles(id string) ([]FileRecord, error)
}

type WorkspaceDiagnosticsResult struct {
	Diagnostics []CodingContextDiagnostic `json:"diagnostics,omitempty"`
	Notes       []string                  `json:"notes,omitempty"`
}

type WorkspaceDiagnosticsProvider interface {
	Diagnostics(workspaceRoot string, files []string) (WorkspaceDiagnosticsResult, error)
}

type CodingContextFile struct {
	Path          string         `json:"path"`
	Kind          FileRecordKind `json:"kind"`
	Tool          string         `json:"tool,omitempty"`
	SnapshotPath  string         `json:"snapshot_path,omitempty"`
	LastTrackedAt time.Time      `json:"last_tracked_at,omitempty"`
}

type CodingContextDiagnostic struct {
	Path     string `json:"path,omitempty"`
	Severity string `json:"severity,omitempty"`
	Message  string `json:"message"`
	Source   string `json:"source,omitempty"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
}

type CodingContextSnapshot struct {
	GeneratedAt   time.Time                 `json:"generated_at"`
	SessionID     string                    `json:"session_id"`
	WorkspaceRoot string                    `json:"workspace_root,omitempty"`
	Files         []CodingContextFile       `json:"files,omitempty"`
	Diagnostics   []CodingContextDiagnostic `json:"diagnostics,omitempty"`
	Notes         []string                  `json:"notes,omitempty"`
}

type CodingContextService struct {
	reader      codingContextReader
	files       *FileHistoryService
	diagnostics WorkspaceDiagnosticsProvider
	now         func() time.Time
}

func NewCodingContextService(reader codingContextReader, diagnostics WorkspaceDiagnosticsProvider) *CodingContextService {
	return &CodingContextService{
		reader:      reader,
		files:       NewFileHistoryService(reader),
		diagnostics: diagnostics,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (s *CodingContextService) Snapshot(sessionID string) (CodingContextSnapshot, error) {
	if s == nil || s.reader == nil || s.files == nil {
		return CodingContextSnapshot{}, apperrors.New("session.CodingContextService.Snapshot", apperrors.CodeStartupFailed, "coding context service is not configured")
	}
	summary, err := s.reader.ReadPersistedSummary(sessionID)
	if err != nil {
		return CodingContextSnapshot{}, err
	}
	fileHistory, err := s.files.FileHistory(sessionID)
	if err != nil {
		return CodingContextSnapshot{}, err
	}

	snapshot := CodingContextSnapshot{
		GeneratedAt:   s.now(),
		SessionID:     sessionID,
		WorkspaceRoot: summary.WorkspaceRoot,
		Files:         buildCodingContextFiles(fileHistory.Files),
		Notes:         buildCodingContextNotes(fileHistory.Files),
	}
	if s.diagnostics == nil {
		return snapshot, nil
	}
	paths := make([]string, 0, len(snapshot.Files))
	for _, file := range snapshot.Files {
		if strings.TrimSpace(file.Path) == "" {
			continue
		}
		paths = append(paths, file.Path)
	}
	result, err := s.diagnostics.Diagnostics(summary.WorkspaceRoot, paths)
	if len(result.Notes) > 0 {
		snapshot.Notes = append(snapshot.Notes, append([]string(nil), result.Notes...)...)
	}
	if len(result.Diagnostics) > 0 {
		snapshot.Diagnostics = cloneCodingContextDiagnostics(result.Diagnostics)
	}
	if err != nil {
		snapshot.Notes = append(snapshot.Notes, "diagnostics unavailable: "+err.Error())
		return snapshot, nil
	}
	return snapshot, nil
}

func (s *CodingContextService) Prompt(sessionID string) (string, error) {
	snapshot, err := s.Snapshot(sessionID)
	if err != nil {
		return "", err
	}
	var builder strings.Builder
	if len(snapshot.Files) > 0 {
		builder.WriteString("Recent workspace files:\n")
		for _, file := range snapshot.Files[:minCodingContextInt(len(snapshot.Files), 6)] {
			builder.WriteString("- ")
			builder.WriteString(file.Path)
			if strings.TrimSpace(file.Tool) != "" {
				builder.WriteString(" via ")
				builder.WriteString(file.Tool)
			}
			if strings.TrimSpace(file.SnapshotPath) != "" {
				builder.WriteString(" (snapshot ")
				builder.WriteString(file.SnapshotPath)
				builder.WriteString(")")
			}
			builder.WriteString("\n")
		}
	}
	if len(snapshot.Diagnostics) > 0 {
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString("Workspace diagnostics:\n")
		for _, diagnostic := range snapshot.Diagnostics[:minCodingContextInt(len(snapshot.Diagnostics), 6)] {
			builder.WriteString("- ")
			severity := strings.TrimSpace(diagnostic.Severity)
			if severity != "" {
				builder.WriteString("[")
				builder.WriteString(severity)
				builder.WriteString("] ")
			}
			location := strings.TrimSpace(diagnostic.Path)
			if location != "" {
				builder.WriteString(location)
				if diagnostic.Line > 0 {
					builder.WriteString(fmt.Sprintf(":%d", diagnostic.Line))
				}
				builder.WriteString(" ")
			}
			builder.WriteString(strings.TrimSpace(diagnostic.Message))
			builder.WriteString("\n")
		}
	}
	return strings.TrimSpace(builder.String()), nil
}

func buildCodingContextFiles(files []FileRecord) []CodingContextFile {
	items := make(map[string]CodingContextFile, len(files))
	for _, file := range files {
		path := strings.TrimSpace(file.Path)
		if file.Kind == FileRecordHistory {
			if sourcePath := strings.TrimSpace(file.Metadata["source_path"]); sourcePath != "" {
				path = sourcePath
			}
		}
		if path == "" {
			continue
		}
		trackedAt := file.CreatedAt
		if file.UpdatedAt.After(trackedAt) {
			trackedAt = file.UpdatedAt
		}
		candidate := CodingContextFile{
			Path:          path,
			Kind:          file.Kind,
			Tool:          strings.TrimSpace(file.Tool),
			LastTrackedAt: trackedAt,
		}
		if file.Kind == FileRecordHistory {
			candidate.SnapshotPath = strings.TrimSpace(file.Path)
		}
		existing, ok := items[path]
		if !ok || candidate.LastTrackedAt.After(existing.LastTrackedAt) || (existing.SnapshotPath == "" && candidate.SnapshotPath != "") {
			items[path] = candidate
		}
	}
	ordered := make([]CodingContextFile, 0, len(items))
	for _, file := range items {
		ordered = append(ordered, file)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if !ordered[i].LastTrackedAt.Equal(ordered[j].LastTrackedAt) {
			return ordered[i].LastTrackedAt.After(ordered[j].LastTrackedAt)
		}
		return ordered[i].Path < ordered[j].Path
	})
	return ordered
}

func buildCodingContextNotes(files []FileRecord) []string {
	notes := make([]string, 0)
	for _, file := range files {
		if file.Kind == FileRecordHistory && strings.TrimSpace(file.Metadata["source_path"]) == "" {
			notes = append(notes, "history snapshot without source_path: "+strings.TrimSpace(file.Path))
		}
	}
	sort.Strings(notes)
	return notes
}

func cloneCodingContextDiagnostics(items []CodingContextDiagnostic) []CodingContextDiagnostic {
	cloned := make([]CodingContextDiagnostic, 0, len(items))
	for _, item := range items {
		cloned = append(cloned, item)
	}
	return cloned
}

func minCodingContextInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
