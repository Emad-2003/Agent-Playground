package cmd

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crawler-ai/internal/domain"
	"crawler-ai/internal/oauth"
	"crawler-ai/internal/session"
)

func TestSessionsShowJSONIncludesStructuredTranscript(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	mgr := session.NewManager()
	mgr.SetDataDir(filepath.Join(oauth.DefaultConfigDir(), "sessions"))
	created, err := mgr.Create("session-1", "/workspace")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	entries := []domain.TranscriptEntry{
		{ID: "assistant-1", Kind: domain.TranscriptAssistant, Message: "Answer body.", CreatedAt: time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC), Metadata: map[string]string{domain.TranscriptMetadataResponseID: "resp-1", "finish_reason": "stop"}},
		{ID: "reasoning-1", Kind: domain.TranscriptReasoning, Message: "Inspecting repository state.", CreatedAt: time.Date(2026, 4, 3, 12, 0, 1, 0, time.UTC), Metadata: map[string]string{domain.TranscriptMetadataResponseID: "resp-1"}},
		{ID: "tool-1", Kind: domain.TranscriptTool, Message: "Tool: grep\n\nInput:\nTODO\n\nResult:\nREADME.md:1: TODO", CreatedAt: time.Date(2026, 4, 3, 12, 0, 2, 0, time.UTC), Metadata: map[string]string{domain.TranscriptMetadataResponseID: "resp-1", "tool": "grep", "tool_call_id": "call-1", "status": "completed"}},
	}
	for _, entry := range entries {
		if err := mgr.AppendTranscript(created.ID, entry); err != nil {
			t.Fatalf("AppendTranscript(%s) error: %v", entry.ID, err)
		}
	}
	if err := mgr.Save(created.ID); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	stdout, _, err := executeRootCommandForTest(t, "sessions", "show", created.ID, "--format", "json")
	if err != nil {
		t.Fatalf("sessions show error: %v", err)
	}

	var shown session.SessionReadProjection
	if err := json.Unmarshal([]byte(stdout), &shown); err != nil {
		t.Fatalf("unmarshal json output: %v\noutput: %s", err, stdout)
	}
	if shown.Summary.ID != created.ID || shown.Summary.WorkspaceRoot != "/workspace" {
		t.Fatalf("unexpected session payload: %#v", shown)
	}
	if len(shown.Transcript) != len(entries) {
		t.Fatalf("expected %d transcript entries, got %#v", len(entries), shown.Transcript)
	}
	if len(shown.Tasks) != 0 || len(shown.Files) != 0 {
		t.Fatalf("expected empty task/file projections, got %#v", shown)
	}
	if shown.Usage != (session.UsageTotals{}) {
		t.Fatalf("expected empty usage totals, got %#v", shown.Usage)
	}
	if shown.Transcript[0].Metadata[domain.TranscriptMetadataResponseID] != "resp-1" {
		t.Fatalf("expected response metadata in json output, got %#v", shown.Transcript[0].Metadata)
	}
	if shown.Transcript[2].Metadata["tool_call_id"] != "call-1" || shown.Transcript[2].Metadata["status"] != "completed" {
		t.Fatalf("expected tool metadata in json output, got %#v", shown.Transcript[2].Metadata)
	}
	if shown.Transcript[2].Message != entries[2].Message {
		t.Fatalf("expected full tool transcript message in json output, got %q", shown.Transcript[2].Message)
	}
}

func TestSessionsShowJSONIncludesChildSessions(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	mgr := session.NewManager()
	mgr.SetDataDir(filepath.Join(oauth.DefaultConfigDir(), "sessions"))
	root, err := mgr.Create("session-root", "/workspace")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	child, err := mgr.CreateChild("session-child", "/workspace", root.ID)
	if err != nil {
		t.Fatalf("CreateChild() error: %v", err)
	}
	if err := mgr.AppendTranscript(child.ID, domain.TranscriptEntry{ID: "assistant-1", Kind: domain.TranscriptAssistant, Message: "delegated result", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("AppendTranscript(child) error: %v", err)
	}
	if err := mgr.SaveAll(); err != nil {
		t.Fatalf("SaveAll() error: %v", err)
	}

	stdout, _, err := executeRootCommandForTest(t, "sessions", "show", root.ID, "--format", "json")
	if err != nil {
		t.Fatalf("sessions show error: %v", err)
	}

	var shown session.SessionReadProjection
	if err := json.Unmarshal([]byte(stdout), &shown); err != nil {
		t.Fatalf("unmarshal json output: %v\noutput: %s", err, stdout)
	}
	if len(shown.Children) != 1 || shown.Children[0].ID != child.ID {
		t.Fatalf("expected child session in show output, got %#v", shown.Children)
	}
}

func TestSessionsDeleteRemovesPersistedSession(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	mgr := session.NewManager()
	mgr.SetDataDir(filepath.Join(oauth.DefaultConfigDir(), "sessions"))
	created, err := mgr.Create("session-delete", "/workspace")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if err := mgr.AppendTranscript(created.ID, domain.TranscriptEntry{ID: "assistant-1", Kind: domain.TranscriptAssistant, Message: "hello", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("AppendTranscript() error: %v", err)
	}
	if err := mgr.Save(created.ID); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	stdout, _, err := executeRootCommandForTest(t, "sessions", "delete", created.ID, "--yes")
	if err != nil {
		t.Fatalf("sessions delete error: %v", err)
	}
	if !strings.Contains(stdout, "Deleted session: "+created.ID) {
		t.Fatalf("expected delete confirmation, got %q", stdout)
	}

	mgr = session.NewManager()
	mgr.SetDataDir(filepath.Join(oauth.DefaultConfigDir(), "sessions"))
	if err := mgr.LoadAll(); err != nil {
		t.Fatalf("LoadAll() error: %v", err)
	}
	if _, ok := mgr.Get(created.ID); ok {
		t.Fatalf("expected session %s to be deleted", created.ID)
	}
}

func TestSessionAliasSupportsDirectShow(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	mgr := session.NewManager()
	mgr.SetDataDir(filepath.Join(oauth.DefaultConfigDir(), "sessions"))
	created, err := mgr.Create("session-direct", "/workspace")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if err := mgr.AppendTranscript(created.ID, domain.TranscriptEntry{ID: "assistant-1", Kind: domain.TranscriptAssistant, Message: "hello direct", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("AppendTranscript() error: %v", err)
	}
	if err := mgr.Save(created.ID); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	stdout, _, err := executeRootCommandForTest(t, "session", created.ID)
	if err != nil {
		t.Fatalf("session direct show error: %v", err)
	}
	if !strings.Contains(stdout, "Session: "+created.ID) {
		t.Fatalf("expected direct session output, got %q", stdout)
	}
	if !strings.Contains(stdout, "Transcript:") {
		t.Fatalf("expected transcript section, got %q", stdout)
	}
}

func TestSessionListSupportsLsAlias(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	mgr := session.NewManager()
	mgr.SetDataDir(filepath.Join(oauth.DefaultConfigDir(), "sessions"))
	created, err := mgr.Create("session-ls", "/workspace")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if err := mgr.AppendTranscript(created.ID, domain.TranscriptEntry{ID: "assistant-1", Kind: domain.TranscriptAssistant, Message: "hello ls", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("AppendTranscript() error: %v", err)
	}
	if err := mgr.Save(created.ID); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	stdout, _, err := executeRootCommandForTest(t, "session", "ls")
	if err != nil {
		t.Fatalf("session ls error: %v", err)
	}
	if !strings.Contains(stdout, "Sessions") || !strings.Contains(stdout, created.ID) {
		t.Fatalf("expected session list output, got %q", stdout)
	}
}

func TestSessionTranscriptCommandPrintsFullEntries(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	mgr := session.NewManager()
	mgr.SetDataDir(filepath.Join(oauth.DefaultConfigDir(), "sessions"))
	created, err := mgr.Create("session-transcript", "/workspace")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	entries := []domain.TranscriptEntry{
		{ID: "user-1", Kind: domain.TranscriptUser, Message: "build tic tac toe", CreatedAt: time.Date(2026, 4, 4, 10, 0, 0, 0, time.UTC)},
		{ID: "tool-1", Kind: domain.TranscriptTool, Message: "Tool output body", CreatedAt: time.Date(2026, 4, 4, 10, 0, 1, 0, time.UTC), Metadata: map[string]string{"tool": "write_file", "status": "completed"}},
	}
	for _, entry := range entries {
		if err := mgr.AppendTranscript(created.ID, entry); err != nil {
			t.Fatalf("AppendTranscript(%s) error: %v", entry.ID, err)
		}
	}
	if err := mgr.Save(created.ID); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	stdout, _, err := executeRootCommandForTest(t, "session", "transcript", created.ID)
	if err != nil {
		t.Fatalf("session transcript error: %v", err)
	}
	if !strings.Contains(stdout, "Session transcript: "+created.ID) {
		t.Fatalf("expected transcript header, got %q", stdout)
	}
	if !strings.Contains(stdout, "build tic tac toe") || !strings.Contains(stdout, "tool=write_file") {
		t.Fatalf("expected full transcript output, got %q", stdout)
	}
}

func TestSessionsExportWritesMarkdownFile(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	created := seedSessionExportFixture(t)

	outputPath := filepath.Join(t.TempDir(), "session.md")
	stdout, _, err := executeRootCommandForTest(t, "sessions", "export", created.ID, "--format", "markdown", "--output", outputPath)
	if err != nil {
		t.Fatalf("sessions export error: %v", err)
	}
	if !strings.Contains(stdout, "Exported session "+created.ID+" to "+outputPath) {
		t.Fatalf("expected export confirmation, got %q", stdout)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "# Session "+created.ID) {
		t.Fatalf("expected session title in export, got %q", content)
	}
	if !strings.Contains(content, "export body") {
		t.Fatalf("expected transcript body in export, got %q", content)
	}
	if !strings.Contains(content, "estimated_cost=$0.0100") {
		t.Fatalf("expected usage summary in export, got %q", content)
	}
}

func TestSessionsExportTranscriptFilterJSON(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	created := seedSessionExportFixture(t)

	stdout, _, err := executeRootCommandForTest(t, "sessions", "export", created.ID, "--format", "json", "--filter", "transcript")
	if err != nil {
		t.Fatalf("sessions export error: %v", err)
	}

	var payload session.SessionExportProjection
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("unmarshal transcript export: %v\noutput: %s", err, stdout)
	}
	if payload.Filter != "transcript" {
		t.Fatalf("expected transcript filter, got %#v", payload)
	}
	if len(payload.Transcript) == 0 {
		t.Fatalf("expected transcript entries, got %#v", payload)
	}
	if payload.Usage != nil {
		t.Fatalf("expected transcript filter to omit usage, got %#v", payload)
	}
	if len(payload.Tasks) != 0 {
		t.Fatalf("expected transcript filter to omit tasks, got %#v", payload)
	}
}

func TestSessionsExportUsageFilterJSON(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	created := seedSessionExportFixture(t)

	stdout, _, err := executeRootCommandForTest(t, "sessions", "export", created.ID, "--format", "json", "--filter", "usage")
	if err != nil {
		t.Fatalf("sessions export error: %v", err)
	}

	var payload session.SessionExportProjection
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("unmarshal usage export: %v\noutput: %s", err, stdout)
	}
	if payload.Filter != "usage" {
		t.Fatalf("expected usage filter, got %#v", payload)
	}
	if payload.Usage == nil || payload.Usage.TotalCost != 0.01 {
		t.Fatalf("expected usage payload, got %#v", payload)
	}
	if len(payload.Transcript) != 0 {
		t.Fatalf("expected usage filter to omit transcript, got %#v", payload)
	}
}

func TestSessionsExportRedactedFilterJSON(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	created := seedSessionExportFixture(t)

	stdout, _, err := executeRootCommandForTest(t, "sessions", "export", created.ID, "--format", "json", "--filter", "redacted")
	if err != nil {
		t.Fatalf("sessions export error: %v", err)
	}

	var payload session.SessionExportProjection
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("unmarshal redacted export: %v\noutput: %s", err, stdout)
	}
	if payload.Filter != "redacted" {
		t.Fatalf("expected redacted filter, got %#v", payload)
	}
	if payload.WorkspaceRoot != "[redacted]" {
		t.Fatalf("expected redacted workspace, got %#v", payload)
	}
	if len(payload.Transcript) == 0 || payload.Transcript[0].Message != "[redacted]" {
		t.Fatalf("expected redacted transcript message, got %#v", payload)
	}
	if len(payload.Tasks) == 0 || payload.Tasks[0].Title != "[redacted]" || payload.Tasks[0].Description != "[redacted]" || payload.Tasks[0].Result != "[redacted]" {
		t.Fatalf("expected redacted tasks, got %#v", payload)
	}
	if payload.Usage == nil {
		t.Fatalf("expected redacted export to preserve usage totals, got %#v", payload)
	}
}

func TestSessionsListUsesPersistedSummariesWhenMessagesAreBroken(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	mgr := session.NewManager()
	dataDir := filepath.Join(oauth.DefaultConfigDir(), "sessions")
	mgr.SetDataDir(dataDir)
	created, err := mgr.Create("session-list", "/workspace")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if err := mgr.AppendTranscript(created.ID, domain.TranscriptEntry{ID: "assistant-1", Kind: domain.TranscriptAssistant, Message: "hello", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("AppendTranscript() error: %v", err)
	}
	if _, err := mgr.RecordUsage(created.ID, session.UsageUpdate{InputTokens: 2, OutputTokens: 1, TotalCost: 0.01, PricingKnown: true, Provider: "openai", Model: "gpt-4o-mini"}); err != nil {
		t.Fatalf("RecordUsage() error: %v", err)
	}
	if err := mgr.Save(created.ID); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	if err := dropSessionsTestMessagesTable(dataDir); err != nil {
		t.Fatalf("dropSessionsTestMessagesTable() error: %v", err)
	}

	stdout, _, err := executeRootCommandForTest(t, "sessions", "list")
	if err != nil {
		t.Fatalf("sessions list error: %v", err)
	}
	if !strings.Contains(stdout, created.ID) {
		t.Fatalf("expected summary-backed list output, got %q", stdout)
	}
}

func TestSessionsListOmitsChildSessions(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	mgr := session.NewManager()
	dataDir := filepath.Join(oauth.DefaultConfigDir(), "sessions")
	mgr.SetDataDir(dataDir)
	root, err := mgr.Create("session-root-list", "/workspace")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	child, err := mgr.CreateChild("session-child-list", "/workspace", root.ID)
	if err != nil {
		t.Fatalf("CreateChild() error: %v", err)
	}
	if err := mgr.SaveAll(); err != nil {
		t.Fatalf("SaveAll() error: %v", err)
	}

	stdout, _, err := executeRootCommandForTest(t, "sessions", "list")
	if err != nil {
		t.Fatalf("sessions list error: %v", err)
	}
	if !strings.Contains(stdout, root.ID) {
		t.Fatalf("expected root session in list output, got %q", stdout)
	}
	if strings.Contains(stdout, child.ID) {
		t.Fatalf("expected child session to be omitted from list output, got %q", stdout)
	}
}

func TestSessionsChildrenJSONListsChildSessions(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	mgr := session.NewManager()
	mgr.SetDataDir(filepath.Join(oauth.DefaultConfigDir(), "sessions"))
	root, err := mgr.Create("session-root-children", "/workspace")
	if err != nil {
		t.Fatalf("Create(root) error: %v", err)
	}
	child, err := mgr.CreateChild("session-child-children", "/workspace", root.ID)
	if err != nil {
		t.Fatalf("CreateChild(child) error: %v", err)
	}
	if err := mgr.AppendTranscript(child.ID, domain.TranscriptEntry{ID: "assistant-1", Kind: domain.TranscriptAssistant, Message: "delegated result", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("AppendTranscript(child) error: %v", err)
	}
	if err := mgr.SaveAll(); err != nil {
		t.Fatalf("SaveAll() error: %v", err)
	}

	stdout, _, err := executeRootCommandForTest(t, "sessions", "children", root.ID, "--format", "json")
	if err != nil {
		t.Fatalf("sessions children error: %v", err)
	}

	var output sessionChildrenOutput
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("unmarshal children output: %v\noutput: %s", err, stdout)
	}
	if output.SessionID != root.ID {
		t.Fatalf("unexpected parent session id in output: %#v", output)
	}
	if len(output.Children) != 1 || output.Children[0].ID != child.ID {
		t.Fatalf("unexpected child sessions in output: %#v", output)
	}
	if output.Children[0].ParentSessionID != root.ID {
		t.Fatalf("unexpected child parent_session_id in output: %#v", output.Children[0])
	}
}

func TestSessionsDiagnoseJSONIncludesDiagnosticsAndFindings(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	mgr := session.NewManager()
	dataDir := filepath.Join(oauth.DefaultConfigDir(), "sessions")
	mgr.SetDataDir(dataDir)
	created, err := mgr.Create("session-diagnose", "/workspace")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if err := mgr.AppendTranscript(created.ID, domain.TranscriptEntry{ID: "assistant-1", Kind: domain.TranscriptAssistant, Message: "partial", CreatedAt: time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC), Metadata: map[string]string{domain.TranscriptMetadataResponseID: "resp-1", "status": "in_progress"}}); err != nil {
		t.Fatalf("AppendTranscript() error: %v", err)
	}
	if err := mgr.TrackFile(created.ID, session.FileRecord{ID: "history-1", Kind: session.FileRecordHistory, Path: "/workspace/.crawler-ai/history/main.go", Tool: "edit", CreatedAt: time.Date(2026, 4, 4, 12, 0, 1, 0, time.UTC)}); err != nil {
		t.Fatalf("TrackFile() error: %v", err)
	}
	if err := mgr.Save(created.ID); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	stdout, _, err := executeRootCommandForTest(t, "sessions", "diagnose", created.ID, "--format", "json")
	if err != nil {
		t.Fatalf("sessions diagnose error: %v", err)
	}

	var output sessionDiagnosticsOutput
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("unmarshal diagnostics output: %v\noutput: %s", err, stdout)
	}
	if output.SessionID != created.ID || output.Messages == nil || output.Storage == nil || output.Files == nil {
		t.Fatalf("unexpected diagnostics payload: %#v", output)
	}
	if len(output.Findings) == 0 {
		t.Fatalf("expected findings in diagnostics output: %#v", output)
	}
}

func TestSessionsDiagnoseReportsSectionErrorsWhenMessagesAreBroken(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	mgr := session.NewManager()
	dataDir := filepath.Join(oauth.DefaultConfigDir(), "sessions")
	mgr.SetDataDir(dataDir)
	created, err := mgr.Create("session-diagnose-broken", "/workspace")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if err := mgr.AppendTranscript(created.ID, domain.TranscriptEntry{ID: "assistant-1", Kind: domain.TranscriptAssistant, Message: "hello", CreatedAt: time.Now().UTC(), Metadata: map[string]string{"finish_reason": "completed"}}); err != nil {
		t.Fatalf("AppendTranscript() error: %v", err)
	}
	if err := mgr.Save(created.ID); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	if err := dropSessionsTestMessagesTable(dataDir); err != nil {
		t.Fatalf("dropSessionsTestMessagesTable() error: %v", err)
	}

	stdout, _, err := executeRootCommandForTest(t, "sessions", "diagnose", created.ID)
	if err != nil {
		t.Fatalf("sessions diagnose error: %v", err)
	}
	if !strings.Contains(stdout, "Section errors") || !strings.Contains(stdout, "[messages]") {
		t.Fatalf("expected section errors in diagnostics output, got %q", stdout)
	}
	if !strings.Contains(stdout, "persisted message inspection failed") {
		t.Fatalf("expected storage finding in diagnostics output, got %q", stdout)
	}
}

func TestSessionsContextJSONIncludesTrackedFilesAndNotes(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	mgr := session.NewManager()
	dataDir := filepath.Join(oauth.DefaultConfigDir(), "sessions")
	mgr.SetDataDir(dataDir)
	created, err := mgr.Create("session-context", "/workspace")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if err := mgr.TrackFile(created.ID, session.FileRecord{ID: "workspace-1", Kind: session.FileRecordWorkspace, Path: "README.md", Tool: "write_file", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("TrackFile(workspace) error: %v", err)
	}
	if err := mgr.TrackFile(created.ID, session.FileRecord{ID: "history-1", Kind: session.FileRecordHistory, Path: ".crawler-ai/history/readme.md.bak", Tool: "edit_file", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("TrackFile(history) error: %v", err)
	}
	if err := mgr.Save(created.ID); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	stdout, _, err := executeRootCommandForTest(t, "sessions", "context", created.ID, "--format", "json")
	if err != nil {
		t.Fatalf("sessions context error: %v", err)
	}

	var output sessionContextOutput
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("unmarshal context output: %v\noutput: %s", err, stdout)
	}
	if output.SessionID != created.ID || len(output.Files) != 2 {
		t.Fatalf("unexpected context payload: %#v", output)
	}
	if output.Files[0].Path != ".crawler-ai/history/readme.md.bak" || output.Files[1].Path != "README.md" {
		t.Fatalf("expected tracked workspace and orphaned history files, got %#v", output.Files)
	}
	if len(output.Notes) == 0 || !strings.Contains(output.Notes[0], "history snapshot without source_path") {
		t.Fatalf("expected coding-context note, got %#v", output.Notes)
	}
}

func TestSessionsHistoryJSONIncludesPromptAndFileHistory(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	mgr := session.NewManager()
	dataDir := filepath.Join(oauth.DefaultConfigDir(), "sessions")
	mgr.SetDataDir(dataDir)
	created, err := mgr.Create("session-history", "/workspace")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if err := mgr.AppendTranscript(created.ID, domain.TranscriptEntry{ID: "user-1", Kind: domain.TranscriptUser, Message: "inspect repo", CreatedAt: time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatalf("AppendTranscript(user) error: %v", err)
	}
	if err := mgr.AppendTranscript(created.ID, domain.TranscriptEntry{ID: "reasoning-1", Kind: domain.TranscriptReasoning, Message: "thinking", CreatedAt: time.Date(2026, 4, 4, 12, 0, 1, 0, time.UTC)}); err != nil {
		t.Fatalf("AppendTranscript(reasoning) error: %v", err)
	}
	if err := mgr.AppendTranscript(created.ID, domain.TranscriptEntry{ID: "assistant-1", Kind: domain.TranscriptAssistant, Message: "partial", CreatedAt: time.Date(2026, 4, 4, 12, 0, 2, 0, time.UTC), Metadata: map[string]string{"status": "in_progress"}}); err != nil {
		t.Fatalf("AppendTranscript(assistant) error: %v", err)
	}
	if err := mgr.TrackFile(created.ID, session.FileRecord{ID: "workspace-1", Kind: session.FileRecordWorkspace, Path: "notes/todo.txt", Tool: "write_file", CreatedAt: time.Date(2026, 4, 4, 12, 0, 3, 0, time.UTC)}); err != nil {
		t.Fatalf("TrackFile(workspace) error: %v", err)
	}
	if err := mgr.TrackFile(created.ID, session.FileRecord{ID: "history-1", Kind: session.FileRecordHistory, Path: ".crawler-ai/history/notes_todo.txt.bak", Tool: "edit_file", CreatedAt: time.Date(2026, 4, 4, 12, 0, 4, 0, time.UTC), Metadata: map[string]string{"source_path": "notes/todo.txt"}}); err != nil {
		t.Fatalf("TrackFile(history) error: %v", err)
	}
	if err := mgr.Save(created.ID); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	stdout, _, err := executeRootCommandForTest(t, "sessions", "history", created.ID, "--format", "json")
	if err != nil {
		t.Fatalf("sessions history error: %v", err)
	}

	var output sessionHistoryOutput
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("unmarshal history output: %v\noutput: %s", err, stdout)
	}
	if output.SessionID != created.ID || len(output.PromptHistory) != 1 || output.FileHistory == nil {
		t.Fatalf("unexpected history output: %#v", output)
	}
	if len(output.FileHistory.WorkspaceFiles) != 1 || len(output.FileHistory.HistorySnapshots) != 1 {
		t.Fatalf("unexpected file history projection: %#v", output.FileHistory)
	}
}

func TestSessionsHistoryTextSupportsFileSection(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	created := seedSessionExportFixture(t)

	stdout, _, err := executeRootCommandForTest(t, "sessions", "history", created.ID, "--section", "files")
	if err != nil {
		t.Fatalf("sessions history error: %v", err)
	}
	if !strings.Contains(stdout, "File history") || !strings.Contains(stdout, "snapshot") {
		t.Fatalf("expected file history output, got %q", stdout)
	}
}

func TestSessionsHistoryJSONSupportsKindFilter(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	mgr := session.NewManager()
	dataDir := filepath.Join(oauth.DefaultConfigDir(), "sessions")
	mgr.SetDataDir(dataDir)
	created, err := mgr.Create("session-history-filter", "/workspace")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if err := mgr.AppendTranscript(created.ID, domain.TranscriptEntry{ID: "user-1", Kind: domain.TranscriptUser, Message: "inspect files", CreatedAt: time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatalf("AppendTranscript() error: %v", err)
	}
	for _, file := range []session.FileRecord{
		{ID: "workspace-1", Kind: session.FileRecordWorkspace, Path: "notes/todo.txt", Tool: "write_file", CreatedAt: time.Date(2026, 4, 4, 12, 0, 1, 0, time.UTC)},
		{ID: "history-1", Kind: session.FileRecordHistory, Path: ".crawler-ai/history/notes_todo.txt.bak", Tool: "edit_file", CreatedAt: time.Date(2026, 4, 4, 12, 0, 2, 0, time.UTC), Metadata: map[string]string{"source_path": "notes/todo.txt"}},
	} {
		if err := mgr.TrackFile(created.ID, file); err != nil {
			t.Fatalf("TrackFile(%s) error: %v", file.ID, err)
		}
	}
	if err := mgr.Save(created.ID); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	stdout, _, err := executeRootCommandForTest(t, "sessions", "history", created.ID, "--format", "json", "--section", "files", "--kind", "history")
	if err != nil {
		t.Fatalf("sessions history error: %v", err)
	}

	var output sessionHistoryOutput
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("unmarshal filtered history output: %v\noutput: %s", err, stdout)
	}
	if output.FileHistory == nil || len(output.FileHistory.Files) != 1 || output.FileHistory.Files[0].ID != "history-1" {
		t.Fatalf("expected history snapshot only, got %#v", output.FileHistory)
	}
	if len(output.FileHistory.WorkspaceFiles) != 0 || len(output.FileHistory.HistorySnapshots) != 1 {
		t.Fatalf("expected filtered file buckets, got %#v", output.FileHistory)
	}
}

func TestSessionsTasksJSONUsesTaskQueryProjection(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	created := seedSessionExportFixture(t)

	stdout, _, err := executeRootCommandForTest(t, "sessions", "tasks", created.ID, "--format", "json")
	if err != nil {
		t.Fatalf("sessions tasks error: %v", err)
	}

	var projection session.TaskProjection
	if err := json.Unmarshal([]byte(stdout), &projection); err != nil {
		t.Fatalf("unmarshal tasks output: %v\noutput: %s", err, stdout)
	}
	if projection.SessionID != created.ID || len(projection.Tasks) != 1 || len(projection.ActiveTaskIDs) != 1 {
		t.Fatalf("unexpected task projection: %#v", projection)
	}
}

func TestSessionsTasksJSONSupportsStatusFilter(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	mgr := session.NewManager()
	mgr.SetDataDir(filepath.Join(oauth.DefaultConfigDir(), "sessions"))
	created, err := mgr.Create("session-task-filter", "/workspace")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if err := mgr.SetTasks(created.ID, []domain.Task{
		{ID: "task-running", Title: "active task", Status: domain.TaskRunning, Assignee: domain.RoleWorker, CreatedAt: time.Date(2026, 4, 4, 9, 0, 0, 0, time.UTC)},
		{ID: "task-completed", Title: "done task", Status: domain.TaskCompleted, Assignee: domain.RoleOrchestrator, CreatedAt: time.Date(2026, 4, 4, 9, 5, 0, 0, time.UTC)},
	}); err != nil {
		t.Fatalf("SetTasks() error: %v", err)
	}
	created.ActiveTaskIDs = []string{"task-running"}
	if err := mgr.Save(created.ID); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	stdout, _, err := executeRootCommandForTest(t, "sessions", "tasks", created.ID, "--format", "json", "--status", "running")
	if err != nil {
		t.Fatalf("sessions tasks error: %v", err)
	}

	var projection session.TaskProjection
	if err := json.Unmarshal([]byte(stdout), &projection); err != nil {
		t.Fatalf("unmarshal filtered tasks output: %v\noutput: %s", err, stdout)
	}
	if len(projection.Tasks) != 1 || projection.Tasks[0].ID != "task-running" {
		t.Fatalf("expected running task only, got %#v", projection)
	}
	if len(projection.ActiveTaskIDs) != 1 || projection.ActiveTaskIDs[0] != "task-running" {
		t.Fatalf("expected filtered active task ids, got %#v", projection.ActiveTaskIDs)
	}
}

func TestSessionsUsageTextUsesUsageQueryProjection(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	created := seedSessionExportFixture(t)

	stdout, _, err := executeRootCommandForTest(t, "sessions", "usage", created.ID)
	if err != nil {
		t.Fatalf("sessions usage error: %v", err)
	}
	if !strings.Contains(stdout, "Session usage: "+created.ID) || !strings.Contains(stdout, "Estimated cost: $0.0100") {
		t.Fatalf("expected usage projection output, got %q", stdout)
	}
}

func seedSessionExportFixture(t *testing.T) session.Session {
	t.Helper()

	mgr := session.NewManager()
	mgr.SetDataDir(filepath.Join(oauth.DefaultConfigDir(), "sessions"))
	created, err := mgr.Create("session-export", "/workspace")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if err := mgr.AppendTranscript(created.ID, domain.TranscriptEntry{ID: "assistant-1", Kind: domain.TranscriptAssistant, Message: "export body", CreatedAt: time.Date(2026, 4, 4, 10, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatalf("AppendTranscript() error: %v", err)
	}
	if err := mgr.SetTasks(created.ID, []domain.Task{{ID: "task-1", Title: "secret title", Description: "secret description", Result: "secret result", Status: domain.TaskCompleted, Assignee: domain.RoleOrchestrator, CreatedAt: time.Date(2026, 4, 4, 9, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 4, 4, 9, 5, 0, 0, time.UTC)}}); err != nil {
		t.Fatalf("SetTasks() error: %v", err)
	}
	if _, err := mgr.RecordUsage(created.ID, session.UsageUpdate{InputTokens: 2, OutputTokens: 3, TotalCost: 0.01, PricingKnown: true, Provider: "openai", Model: "gpt-4o-mini"}); err != nil {
		t.Fatalf("RecordUsage() error: %v", err)
	}
	if err := mgr.TrackFile(created.ID, session.FileRecord{ID: "workspace-1", Kind: session.FileRecordWorkspace, Path: "notes/todo.txt", Tool: "write_file", CreatedAt: time.Date(2026, 4, 4, 10, 1, 0, 0, time.UTC)}); err != nil {
		t.Fatalf("TrackFile(workspace) error: %v", err)
	}
	if err := mgr.TrackFile(created.ID, session.FileRecord{ID: "history-1", Kind: session.FileRecordHistory, Path: ".crawler-ai/history/notes_todo.txt.bak", Tool: "edit_file", CreatedAt: time.Date(2026, 4, 4, 10, 2, 0, 0, time.UTC), Metadata: map[string]string{"source_path": "notes/todo.txt"}}); err != nil {
		t.Fatalf("TrackFile(history) error: %v", err)
	}
	if err := mgr.Save(created.ID); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	loaded, ok := mgr.Get(created.ID)
	if !ok {
		t.Fatalf("expected persisted session %s", created.ID)
	}
	return loaded
}

func dropSessionsTestMessagesTable(dataDir string) error {
	db, err := sql.Open("sqlite", session.NewSQLiteStore(dataDir).DatabasePath())
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`DROP TABLE messages`)
	return err
}
