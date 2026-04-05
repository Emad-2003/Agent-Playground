package session

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"crawler-ai/internal/domain"
	apperrors "crawler-ai/internal/errors"
)

func TestPersistRoundTrip(t *testing.T) {
	dir := t.TempDir()

	m1 := NewManager()
	m1.SetDataDir(dir)

	sess, err := m1.Create("s1", "/workspace")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	entry := domain.TranscriptEntry{
		ID:        "e1",
		Kind:      domain.TranscriptUser,
		Message:   "hello world",
		CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := m1.AppendTranscript(sess.ID, entry); err != nil {
		t.Fatalf("AppendTranscript: %v", err)
	}
	if err := m1.SetTasks(sess.ID, []domain.Task{{ID: "task-1", Title: "Index workspace", Status: domain.TaskRunning}}); err != nil {
		t.Fatalf("SetTasks: %v", err)
	}
	if _, err := m1.RecordUsage(sess.ID, UsageUpdate{InputTokens: 5, OutputTokens: 2, TotalCost: 0.02, PricingKnown: true, Provider: "mock", Model: "mock-orchestrator-v1"}); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	if err := m1.TrackFile(sess.ID, FileRecord{ID: "file-1", Kind: FileRecordWorkspace, Path: "notes/todo.txt", Tool: "write_file", CreatedAt: time.Date(2025, 1, 1, 0, 0, 1, 0, time.UTC)}); err != nil {
		t.Fatalf("TrackFile: %v", err)
	}

	if err := m1.Save("s1"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify the database was created.
	path := NewSQLiteStore(dir).DatabasePath()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("session database not found: %v", err)
	}

	// Load into a fresh manager.
	m2 := NewManager()
	m2.SetDataDir(dir)
	if err := m2.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	loaded, ok := m2.Get("s1")
	if !ok {
		t.Fatal("session not found after LoadAll")
	}
	if loaded.WorkspaceRoot != "/workspace" {
		t.Errorf("workspace root = %q, want %q", loaded.WorkspaceRoot, "/workspace")
	}
	if len(loaded.Transcript) != 1 {
		t.Fatalf("transcript length = %d, want 1", len(loaded.Transcript))
	}
	if loaded.Transcript[0].Message != "hello world" {
		t.Errorf("transcript message = %q, want %q", loaded.Transcript[0].Message, "hello world")
	}
	if len(loaded.Tasks) != 1 || loaded.Tasks[0].ID != "task-1" {
		t.Fatalf("expected persisted tasks, got %#v", loaded.Tasks)
	}
	if len(loaded.Files) != 1 || loaded.Files[0].Path != "notes/todo.txt" || loaded.Files[0].Tool != "write_file" {
		t.Fatalf("expected persisted file tracking, got %#v", loaded.Files)
	}
	if loaded.Usage.InputTokens != 5 || loaded.Usage.OutputTokens != 2 || loaded.Usage.ResponseCount != 1 || loaded.Usage.PricedResponses != 1 || loaded.Usage.TotalCost != 0.02 {
		t.Fatalf("expected persisted usage totals, got %#v", loaded.Usage)
	}
}

func TestPersistRoundTripMixedTranscriptContent(t *testing.T) {
	dir := t.TempDir()

	m1 := NewManager()
	m1.SetDataDir(dir)

	if _, err := m1.Create("s1", "/workspace"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	entries := []domain.TranscriptEntry{
		{ID: "assistant-1", Kind: domain.TranscriptAssistant, Message: "Answer body.", CreatedAt: time.Date(2026, 4, 3, 10, 0, 0, 0, time.UTC), Metadata: map[string]string{domain.TranscriptMetadataResponseID: "resp-1", "finish_reason": "stop"}},
		{ID: "reasoning-1", Kind: domain.TranscriptReasoning, Message: "Inspecting files.", CreatedAt: time.Date(2026, 4, 3, 10, 0, 1, 0, time.UTC), Metadata: map[string]string{domain.TranscriptMetadataResponseID: "resp-1"}},
		{ID: "tool-call-1", Kind: domain.TranscriptTool, Message: "Tool: grep\n\nInput:\nTODO\n\nResult:\nREADME.md:1: TODO", CreatedAt: time.Date(2026, 4, 3, 10, 0, 2, 0, time.UTC), Metadata: map[string]string{domain.TranscriptMetadataResponseID: "resp-1", "tool_call_id": "call-1", "tool": "grep", "status": "completed"}},
	}
	for _, entry := range entries {
		if err := m1.AppendTranscript("s1", entry); err != nil {
			t.Fatalf("AppendTranscript(%s): %v", entry.ID, err)
		}
	}

	if err := m1.Save("s1"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	m2 := NewManager()
	m2.SetDataDir(dir)
	if err := m2.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	loaded, ok := m2.Get("s1")
	if !ok {
		t.Fatal("session not found after LoadAll")
	}
	if len(loaded.Transcript) != len(entries) {
		t.Fatalf("transcript length = %d, want %d", len(loaded.Transcript), len(entries))
	}
	for index, want := range entries {
		got := loaded.Transcript[index]
		if got.ID != want.ID || got.Kind != want.Kind || got.Message != want.Message {
			t.Fatalf("entry %d mismatch: got %#v want %#v", index, got, want)
		}
		if got.Metadata[domain.TranscriptMetadataResponseID] != want.Metadata[domain.TranscriptMetadataResponseID] {
			t.Fatalf("entry %d response metadata mismatch: got %#v want %#v", index, got.Metadata, want.Metadata)
		}
	}
	if loaded.Transcript[2].Metadata["tool_call_id"] != "call-1" || loaded.Transcript[2].Metadata["status"] != "completed" {
		t.Fatalf("tool transcript metadata mismatch after load: %#v", loaded.Transcript[2].Metadata)
	}
}

func TestSaveAllAndDelete(t *testing.T) {
	dir := t.TempDir()

	m := NewManager()
	m.SetDataDir(dir)

	if _, err := m.Create("a", "/w"); err != nil {
		t.Fatalf("Create a: %v", err)
	}
	if _, err := m.Create("b", "/w"); err != nil {
		t.Fatalf("Create b: %v", err)
	}

	if err := m.SaveAll(); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}

	summaries, err := m.ListPersistedSummaries()
	if err != nil {
		t.Fatalf("ListPersistedSummaries: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 persisted sessions, got %d", len(summaries))
	}

	if err := m.Delete("a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := m.ReadPersistedSummary("a"); err == nil {
		t.Fatal("expected deleted session to disappear from persisted summaries")
	}

	// Verify the session is gone from memory.
	if _, ok := m.Get("a"); ok {
		t.Error("session 'a' should not exist after Delete")
	}
}

func TestDeletePersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()

	mgr := NewManager()
	mgr.SetDataDir(dir)
	created, err := mgr.Create("delete-restart", "/workspace")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := mgr.AppendTranscript(created.ID, domain.TranscriptEntry{ID: "m1", Kind: domain.TranscriptAssistant, Message: "hello", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("AppendTranscript: %v", err)
	}
	if err := mgr.Save(created.ID); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := mgr.Delete(created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	reloaded := NewManager()
	reloaded.SetDataDir(dir)
	if err := reloaded.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if _, ok := reloaded.Get(created.ID); ok {
		t.Fatal("deleted session reappeared after restart")
	}
	if _, err := reloaded.ReadPersistedSummary(created.ID); err == nil {
		t.Fatal("expected deleted session summary to stay absent after restart")
	}
}

func TestChildSessionsReloadUnderCorrectParentAfterRestart(t *testing.T) {
	dir := t.TempDir()

	mgr := NewManager()
	mgr.SetDataDir(dir)
	root, err := mgr.Create("lineage-root", "/workspace")
	if err != nil {
		t.Fatalf("Create(root): %v", err)
	}
	child, err := mgr.CreateChild("lineage-child", "/workspace", root.ID)
	if err != nil {
		t.Fatalf("CreateChild(child): %v", err)
	}
	if err := mgr.AppendTranscript(child.ID, domain.TranscriptEntry{ID: "child-msg", Kind: domain.TranscriptAssistant, Message: "delegated result", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("AppendTranscript(child): %v", err)
	}
	if err := mgr.SaveAll(); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}

	reloaded := NewManager()
	reloaded.SetDataDir(dir)
	if err := reloaded.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	reloadedChild, ok := reloaded.Get(child.ID)
	if !ok {
		t.Fatal("child session missing after restart")
	}
	if reloadedChild.ParentSessionID != root.ID {
		t.Fatalf("child parent_session_id = %q, want %q", reloadedChild.ParentSessionID, root.ID)
	}

	queries := NewQueryService(reloaded)
	children, err := queries.ChildSummaries(root.ID)
	if err != nil {
		t.Fatalf("ChildSummaries: %v", err)
	}
	if len(children) != 1 || children[0].ID != child.ID {
		t.Fatalf("unexpected child summaries after restart: %#v", children)
	}
	if children[0].ParentSessionID != root.ID {
		t.Fatalf("child summary parent_session_id = %q, want %q", children[0].ParentSessionID, root.ID)
	}
}

func TestDeleteRetainsInMemorySessionOnStoreFailure(t *testing.T) {
	mgr := NewManager()
	created, err := mgr.Create("delete-failure", "/workspace")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	deleteErr := fmt.Errorf("delete failed")
	mgr.SetStore(&failingDeleteStore{deleteErr: deleteErr})

	err = mgr.Delete(created.ID)
	if err == nil {
		t.Fatal("expected delete error")
	}
	if err.Error() != deleteErr.Error() {
		t.Fatalf("Delete error = %v, want %v", err, deleteErr)
	}
	if _, ok := mgr.Get(created.ID); !ok {
		t.Fatal("session should remain in memory after failed store delete")
	}
}

func TestSaveNoDataDir(t *testing.T) {
	m := NewManager()
	if _, err := m.Create("x", "/w"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Save with no data dir should be a no-op.
	if err := m.Save("x"); err != nil {
		t.Errorf("Save with no data dir should succeed, got %v", err)
	}
}

func TestLoadAllEmptyDir(t *testing.T) {
	m := NewManager()
	m.SetDataDir(t.TempDir())
	if err := m.LoadAll(); err != nil {
		t.Errorf("LoadAll on empty dir should succeed, got %v", err)
	}
}

func TestLoadAllNonExistentDir(t *testing.T) {
	m := NewManager()
	m.SetDataDir(filepath.Join(t.TempDir(), "nonexistent"))
	if err := m.LoadAll(); err != nil {
		t.Errorf("LoadAll on nonexistent dir should succeed, got %v", err)
	}
}

func TestPersistedQueryReadsComponentsAndSummaries(t *testing.T) {
	dir := t.TempDir()

	mgr := NewManager()
	mgr.SetDataDir(dir)
	created, err := mgr.Create("query-session", "/workspace")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := mgr.AppendTranscript(created.ID, domain.TranscriptEntry{ID: "m1", Kind: domain.TranscriptAssistant, Message: "hello", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("AppendTranscript: %v", err)
	}
	if err := mgr.SetTasks(created.ID, []domain.Task{{ID: "task-1", Title: "Index workspace", Status: domain.TaskCompleted}}); err != nil {
		t.Fatalf("SetTasks: %v", err)
	}
	if err := mgr.TrackFile(created.ID, FileRecord{ID: "file-1", Kind: FileRecordWorkspace, Path: "notes/todo.txt", Tool: "write_file", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("TrackFile: %v", err)
	}
	if _, err := mgr.RecordUsage(created.ID, UsageUpdate{InputTokens: 7, OutputTokens: 3, TotalCost: 0.01, PricingKnown: true, Provider: "mock", Model: "mock-orchestrator-v1"}); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	if err := mgr.Save(created.ID); err != nil {
		t.Fatalf("Save: %v", err)
	}

	summary, err := mgr.ReadPersistedSummary(created.ID)
	if err != nil {
		t.Fatalf("ReadPersistedSummary: %v", err)
	}
	if summary.MessageCount != 1 || summary.TaskCount != 1 || summary.FileCount != 1 {
		t.Fatalf("unexpected summary counts: %#v", summary)
	}
	if summary.Usage.TotalCost != 0.01 || summary.Usage.ResponseCount != 1 {
		t.Fatalf("unexpected summary usage: %#v", summary)
	}

	messages, err := mgr.ReadPersistedMessages(created.ID)
	if err != nil {
		t.Fatalf("ReadPersistedMessages: %v", err)
	}
	if len(messages) != 1 || messages[0].Message != "hello" {
		t.Fatalf("unexpected persisted messages: %#v", messages)
	}
	tasks, err := mgr.ReadPersistedTasks(created.ID)
	if err != nil {
		t.Fatalf("ReadPersistedTasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "task-1" {
		t.Fatalf("unexpected persisted tasks: %#v", tasks)
	}
	files, err := mgr.ReadPersistedFiles(created.ID)
	if err != nil {
		t.Fatalf("ReadPersistedFiles: %v", err)
	}
	if len(files) != 1 || files[0].Path != "notes/todo.txt" {
		t.Fatalf("unexpected persisted files: %#v", files)
	}
	usage, err := mgr.ReadPersistedUsage(created.ID)
	if err != nil {
		t.Fatalf("ReadPersistedUsage: %v", err)
	}
	if usage.TotalCost != 0.01 || usage.InputTokens != 7 || usage.OutputTokens != 3 {
		t.Fatalf("unexpected persisted usage: %#v", usage)
	}
}

func TestPersistedSummaryReadDoesNotDependOnMessagesFile(t *testing.T) {
	dir := t.TempDir()

	mgr := NewManager()
	mgr.SetDataDir(dir)
	created, err := mgr.Create("summary-session", "/workspace")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := mgr.AppendTranscript(created.ID, domain.TranscriptEntry{ID: "m1", Kind: domain.TranscriptAssistant, Message: "hello", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("AppendTranscript: %v", err)
	}
	if _, err := mgr.RecordUsage(created.ID, UsageUpdate{InputTokens: 5, OutputTokens: 2, TotalCost: 0.02, PricingKnown: true, Provider: "mock", Model: "mock-orchestrator-v1"}); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	if err := mgr.Save(created.ID); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := dropSQLiteMessagesTable(dir); err != nil {
		t.Fatalf("dropSQLiteMessagesTable: %v", err)
	}

	summaries, err := mgr.ListPersistedSummaries()
	if err != nil {
		t.Fatalf("ListPersistedSummaries: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != created.ID {
		t.Fatalf("unexpected summaries: %#v", summaries)
	}
	if summaries[0].Usage.TotalCost != 0.02 {
		t.Fatalf("expected summary usage despite broken messages file, got %#v", summaries[0])
	}
}

func TestSQLiteStoreImportsNormalizedJSONSessions(t *testing.T) {
	dir := t.TempDir()

	legacy := NewManager()
	legacy.SetStore(NewJSONStore(dir))
	created, err := legacy.Create("legacy-session", "/workspace")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := legacy.AppendTranscript(created.ID, domain.TranscriptEntry{ID: "m1", Kind: domain.TranscriptAssistant, Message: "hello", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("AppendTranscript: %v", err)
	}
	if _, err := legacy.RecordUsage(created.ID, UsageUpdate{InputTokens: 4, OutputTokens: 2, TotalCost: 0.01, PricingKnown: true, Provider: "mock", Model: "mock-orchestrator-v1"}); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	if err := legacy.Save(created.ID); err != nil {
		t.Fatalf("Save: %v", err)
	}

	migrated := NewManager()
	migrated.SetDataDir(dir)
	summary, err := migrated.ReadPersistedSummary(created.ID)
	if err != nil {
		t.Fatalf("ReadPersistedSummary: %v", err)
	}
	if summary.ID != created.ID || summary.MessageCount != 1 || summary.Usage.TotalCost != 0.01 {
		t.Fatalf("unexpected imported summary: %#v", summary)
	}

	if _, err := os.Stat(NewSQLiteStore(dir).DatabasePath()); err != nil {
		t.Fatalf("expected sqlite database after import: %v", err)
	}

	if err := migrated.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	loaded, ok := migrated.Get(created.ID)
	if !ok {
		t.Fatal("expected imported session after LoadAll")
	}
	if len(loaded.Transcript) != 1 || loaded.Usage.TotalCost != 0.01 {
		t.Fatalf("unexpected imported session: %#v", loaded)
	}
}

func TestSQLiteStoreImportsChildSessionsUnderCorrectParent(t *testing.T) {
	dir := t.TempDir()

	legacy := NewManager()
	legacy.SetStore(NewJSONStore(dir))
	root, err := legacy.Create("legacy-lineage-root", "/workspace")
	if err != nil {
		t.Fatalf("Create(root): %v", err)
	}
	child, err := legacy.CreateChild("legacy-lineage-child", "/workspace", root.ID)
	if err != nil {
		t.Fatalf("CreateChild(child): %v", err)
	}
	if err := legacy.AppendTranscript(child.ID, domain.TranscriptEntry{ID: "child-msg", Kind: domain.TranscriptAssistant, Message: "imported delegated result", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("AppendTranscript(child): %v", err)
	}
	if err := legacy.SaveAll(); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}

	migrated := NewManager()
	migrated.SetDataDir(dir)
	childSummary, err := migrated.ReadPersistedSummary(child.ID)
	if err != nil {
		t.Fatalf("ReadPersistedSummary(child): %v", err)
	}
	if childSummary.ParentSessionID != root.ID {
		t.Fatalf("imported child parent_session_id = %q, want %q", childSummary.ParentSessionID, root.ID)
	}

	if err := migrated.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	loadedChild, ok := migrated.Get(child.ID)
	if !ok {
		t.Fatal("imported child session missing after LoadAll")
	}
	if loadedChild.ParentSessionID != root.ID {
		t.Fatalf("loaded imported child parent_session_id = %q, want %q", loadedChild.ParentSessionID, root.ID)
	}

	queries := NewQueryService(migrated)
	children, err := queries.ChildSummaries(root.ID)
	if err != nil {
		t.Fatalf("ChildSummaries: %v", err)
	}
	if len(children) != 1 || children[0].ID != child.ID {
		t.Fatalf("unexpected imported child summaries: %#v", children)
	}
}

func TestSQLiteStoreBootstrapsLatestSchemaVersion(t *testing.T) {
	dir := t.TempDir()

	mgr := NewManager()
	mgr.SetDataDir(dir)

	summaries, err := mgr.ListPersistedSummaries()
	if err != nil {
		t.Fatalf("ListPersistedSummaries: %v", err)
	}
	if len(summaries) != 0 {
		t.Fatalf("expected no summaries in fresh store, got %d", len(summaries))
	}

	version, err := readSQLiteSchemaVersionForTest(dir)
	if err != nil {
		t.Fatalf("readSQLiteSchemaVersionForTest: %v", err)
	}
	if version != sqliteLatestSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, sqliteLatestSchemaVersion)
	}
}

func TestSQLiteStoreMigratesLegacySchemaInPlace(t *testing.T) {
	dir := t.TempDir()

	persisted := PersistedSession{
		Session: SessionRecord{
			ID:            "legacy-session",
			WorkspaceRoot: "/workspace",
			CreatedAt:     time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			UpdatedAt:     time.Date(2025, 1, 1, 0, 0, 1, 0, time.UTC),
			ActiveTaskIDs: []string{"task-1"},
		},
		Messages: []MessageRecord{{ID: "m1", Kind: domain.TranscriptAssistant, Message: "hello", CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}},
		Tasks:    []TaskRecord{{ID: "task-1", Title: "Index workspace", Status: domain.TaskCompleted, CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}},
		Files:    []FileRecord{{ID: "file-1", Kind: FileRecordWorkspace, Path: "notes/todo.txt", Tool: "write_file", CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}},
		Usage:    UsageTotals{InputTokens: 4, OutputTokens: 2, ResponseCount: 1, PricedResponses: 1, TotalCost: 0.01, LastProvider: "mock", LastModel: "mock-orchestrator-v1", UpdatedAt: time.Date(2025, 1, 1, 0, 0, 2, 0, time.UTC)},
	}
	if err := createLegacySQLiteStore(dir, persisted); err != nil {
		t.Fatalf("createLegacySQLiteStore: %v", err)
	}

	mgr := NewManager()
	mgr.SetDataDir(dir)

	summary, err := mgr.ReadPersistedSummary(persisted.Session.ID)
	if err != nil {
		t.Fatalf("ReadPersistedSummary: %v", err)
	}
	if summary.ID != persisted.Session.ID || summary.MessageCount != 1 || summary.TaskCount != 1 || summary.FileCount != 1 {
		t.Fatalf("unexpected migrated summary: %#v", summary)
	}
	if summary.Usage.TotalCost != 0.01 || summary.Usage.ResponseCount != 1 {
		t.Fatalf("unexpected migrated usage: %#v", summary.Usage)
	}

	version, err := readSQLiteSchemaVersionForTest(dir)
	if err != nil {
		t.Fatalf("readSQLiteSchemaVersionForTest: %v", err)
	}
	if version != sqliteLatestSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, sqliteLatestSchemaVersion)
	}
}

func TestSQLiteStoreRejectsPartialLegacySchema(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", NewSQLiteStore(dir).DatabasePath())
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatalf("create partial schema: %v", err)
	}

	mgr := NewManager()
	mgr.SetDataDir(dir)

	_, err = mgr.ListPersistedSummaries()
	if err == nil {
		t.Fatal("expected partial legacy sqlite schema to be rejected")
	}
	if !apperrors.IsCode(err, apperrors.CodeToolFailed) {
		t.Fatalf("expected tool_failed error, got %v", err)
	}
	if !strings.Contains(err.Error(), "missing required columns") {
		t.Fatalf("expected partial schema error message, got %v", err)
	}
}

func TestSQLiteStoreConcurrentSavesSurviveReload(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager()
	mgr.SetDataDir(dir)

	const sessionCount = 8
	expectedMessages := make(map[string]string, sessionCount)
	for index := 0; index < sessionCount; index++ {
		id := fmt.Sprintf("concurrent-%d", index)
		message := fmt.Sprintf("message-%d", index)
		expectedMessages[id] = message

		created, err := mgr.Create(id, "/workspace")
		if err != nil {
			t.Fatalf("Create(%s): %v", id, err)
		}
		if err := mgr.AppendTranscript(created.ID, domain.TranscriptEntry{ID: fmt.Sprintf("m-%d", index), Kind: domain.TranscriptAssistant, Message: message, CreatedAt: time.Now().UTC()}); err != nil {
			t.Fatalf("AppendTranscript(%s): %v", id, err)
		}
	}

	var wg sync.WaitGroup
	errCh := make(chan error, sessionCount)
	for id := range expectedMessages {
		wg.Add(1)
		go func(sessionID string) {
			defer wg.Done()
			errCh <- mgr.Save(sessionID)
		}(id)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent Save: %v", err)
		}
	}

	reloaded := NewManager()
	reloaded.SetDataDir(dir)
	if err := reloaded.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	loadedSessions := reloaded.List()
	if len(loadedSessions) != sessionCount {
		t.Fatalf("loaded session count = %d, want %d", len(loadedSessions), sessionCount)
	}
	for id, message := range expectedMessages {
		loaded, ok := reloaded.Get(id)
		if !ok {
			t.Fatalf("session %s missing after concurrent saves", id)
		}
		if len(loaded.Transcript) != 1 || loaded.Transcript[0].Message != message {
			t.Fatalf("unexpected transcript for %s: %#v", id, loaded.Transcript)
		}
	}
}

func TestTrackedFileKindsAndMetadataPersistAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager()
	mgr.SetDataDir(dir)

	created, err := mgr.Create("file-kinds", "/workspace")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	trackedFiles := []FileRecord{
		{ID: "workspace-1", Kind: FileRecordWorkspace, Path: "notes/todo.txt", Tool: "edit_file", CreatedAt: time.Now().UTC()},
		{ID: "history-1", Kind: FileRecordHistory, Path: ".crawler-ai/history/notes_todo.txt.20260404T010203.000000000Z.bak", Tool: "edit_file", CreatedAt: time.Now().UTC(), Metadata: map[string]string{"source_path": "notes/todo.txt"}},
		{ID: "fetched-1", Kind: FileRecordFetched, Path: "reference/api.md", Tool: "fetch", CreatedAt: time.Now().UTC(), Metadata: map[string]string{"source_url": "https://example.test/api"}},
	}
	for _, record := range trackedFiles {
		if err := mgr.TrackFile(created.ID, record); err != nil {
			t.Fatalf("TrackFile(%s): %v", record.ID, err)
		}
	}
	if err := mgr.Save(created.ID); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded := NewManager()
	reloaded.SetDataDir(dir)
	if err := reloaded.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	loaded, ok := reloaded.Get(created.ID)
	if !ok {
		t.Fatal("session missing after restart")
	}
	if len(loaded.Files) != len(trackedFiles) {
		t.Fatalf("loaded file count = %d, want %d", len(loaded.Files), len(trackedFiles))
	}

	filesByID := make(map[string]FileRecord, len(loaded.Files))
	for _, file := range loaded.Files {
		filesByID[file.ID] = file
	}
	if filesByID["workspace-1"].Kind != FileRecordWorkspace || filesByID["workspace-1"].Path != "notes/todo.txt" {
		t.Fatalf("unexpected workspace record: %#v", filesByID["workspace-1"])
	}
	if filesByID["history-1"].Kind != FileRecordHistory || filesByID["history-1"].Metadata["source_path"] != "notes/todo.txt" {
		t.Fatalf("unexpected history record: %#v", filesByID["history-1"])
	}
	if filesByID["fetched-1"].Kind != FileRecordFetched || filesByID["fetched-1"].Metadata["source_url"] != "https://example.test/api" {
		t.Fatalf("unexpected fetched record: %#v", filesByID["fetched-1"])
	}

	persistedFiles, err := reloaded.ReadPersistedFiles(created.ID)
	if err != nil {
		t.Fatalf("ReadPersistedFiles: %v", err)
	}
	if len(persistedFiles) != len(trackedFiles) {
		t.Fatalf("persisted file count = %d, want %d", len(persistedFiles), len(trackedFiles))
	}
}

func dropSQLiteMessagesTable(dir string) error {
	db, err := sql.Open("sqlite", NewSQLiteStore(dir).DatabasePath())
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`DROP TABLE messages`)
	return err
}

func readSQLiteSchemaVersionForTest(dir string) (int, error) {
	db, err := sql.Open("sqlite", NewSQLiteStore(dir).DatabasePath())
	if err != nil {
		return 0, err
	}
	defer db.Close()
	return readSQLiteSchemaVersion(db)
}

func createLegacySQLiteStore(dir string, persisted PersistedSession) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", NewSQLiteStore(dir).DatabasePath())
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
		return err
	}
	if _, err := db.Exec(sqliteSchemaV1); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if err := savePersistedSessionTx(tx, persisted); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

type failingDeleteStore struct {
	deleteErr error
}

func (s *failingDeleteStore) Save(PersistedSession) error { return nil }

func (s *failingDeleteStore) LoadAll() ([]PersistedSession, error) { return nil, nil }

func (s *failingDeleteStore) LoadSession(string) (PersistedSession, error) {
	return PersistedSession{}, nil
}

func (s *failingDeleteStore) LoadSummary(string) (SessionSummary, error) {
	return SessionSummary{}, nil
}

func (s *failingDeleteStore) ListSummaries() ([]SessionSummary, error) { return nil, nil }

func (s *failingDeleteStore) LoadMessages(string) ([]MessageRecord, error) { return nil, nil }

func (s *failingDeleteStore) LoadTasks(string) ([]TaskRecord, error) { return nil, nil }

func (s *failingDeleteStore) LoadFiles(string) ([]FileRecord, error) { return nil, nil }

func (s *failingDeleteStore) LoadUsage(string) (UsageTotals, error) { return UsageTotals{}, nil }

func (s *failingDeleteStore) Delete(string) error { return s.deleteErr }
