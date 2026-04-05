package session

import (
	"strings"
	"testing"
	"time"
)

type workspaceDiagnosticsProviderStub struct {
	result WorkspaceDiagnosticsResult
	err    error
}

func (s workspaceDiagnosticsProviderStub) Diagnostics(string, []string) (WorkspaceDiagnosticsResult, error) {
	if s.err != nil {
		return WorkspaceDiagnosticsResult{}, s.err
	}
	cloned := WorkspaceDiagnosticsResult{
		Diagnostics: append([]CodingContextDiagnostic(nil), s.result.Diagnostics...),
		Notes:       append([]string(nil), s.result.Notes...),
	}
	return cloned, nil
}

func TestCodingContextServiceBuildsPromptFromTrackedFilesAndDiagnostics(t *testing.T) {
	mgr := NewManager()
	created, err := mgr.Create("context-session", "/workspace")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if err := mgr.TrackFile(created.ID, FileRecord{ID: "workspace-1", Kind: FileRecordWorkspace, Path: "cmd/main.go", Tool: "write_file", CreatedAt: time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatalf("TrackFile(workspace) error: %v", err)
	}
	if err := mgr.TrackFile(created.ID, FileRecord{ID: "history-1", Kind: FileRecordHistory, Path: ".crawler-ai/history/cmd_main.go.bak", Tool: "edit_file", CreatedAt: time.Date(2026, 4, 4, 12, 1, 0, 0, time.UTC), Metadata: map[string]string{"source_path": "cmd/main.go"}}); err != nil {
		t.Fatalf("TrackFile(history) error: %v", err)
	}

	service := NewCodingContextService(mgr, workspaceDiagnosticsProviderStub{result: WorkspaceDiagnosticsResult{Diagnostics: []CodingContextDiagnostic{{Path: "cmd/main.go", Severity: "error", Message: "undefined: playMove", Line: 14, Source: "gopls"}}}})
	snapshot, err := service.Snapshot(created.ID)
	if err != nil {
		t.Fatalf("Snapshot() error: %v", err)
	}
	if len(snapshot.Files) != 1 {
		t.Fatalf("expected deduplicated context file list, got %#v", snapshot.Files)
	}
	if snapshot.Files[0].Path != "cmd/main.go" || snapshot.Files[0].SnapshotPath != ".crawler-ai/history/cmd_main.go.bak" {
		t.Fatalf("unexpected coding context file: %#v", snapshot.Files[0])
	}
	if len(snapshot.Diagnostics) != 1 || snapshot.Diagnostics[0].Message != "undefined: playMove" {
		t.Fatalf("unexpected coding context diagnostics: %#v", snapshot.Diagnostics)
	}

	prompt, err := service.Prompt(created.ID)
	if err != nil {
		t.Fatalf("Prompt() error: %v", err)
	}
	if !strings.Contains(prompt, "Recent workspace files:") || !strings.Contains(prompt, "cmd/main.go") {
		t.Fatalf("expected tracked file prompt context, got %q", prompt)
	}
	if !strings.Contains(prompt, "Workspace diagnostics:") || !strings.Contains(prompt, "undefined: playMove") {
		t.Fatalf("expected diagnostic prompt context, got %q", prompt)
	}
}
