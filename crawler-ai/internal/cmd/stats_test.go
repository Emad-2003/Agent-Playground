package cmd

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crawler-ai/internal/domain"
	"crawler-ai/internal/oauth"
	"crawler-ai/internal/session"
)

func TestStatsCommandTextOutput(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	mgr := session.NewManager()
	mgr.SetDataDir(filepath.Join(oauth.DefaultConfigDir(), "sessions"))
	created, err := mgr.Create("session-1", "/workspace")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if _, err := mgr.RecordUsage(created.ID, session.UsageUpdate{InputTokens: 12, OutputTokens: 8, TotalCost: 0.015, PricingKnown: true, Provider: "openai", Model: "gpt-4o-mini"}); err != nil {
		t.Fatalf("RecordUsage() error: %v", err)
	}
	if err := mgr.Save(created.ID); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	stdout, _, err := executeRootCommandForTest(t, "stats")
	if err != nil {
		t.Fatalf("stats error: %v", err)
	}
	if !strings.Contains(stdout, "Sessions: 1 total (1 with usage)") {
		t.Fatalf("expected sessions summary, got %q", stdout)
	}
	if !strings.Contains(stdout, "Responses: 1 total (priced=1 unpriced=0 coverage=100.0%)") {
		t.Fatalf("expected response coverage, got %q", stdout)
	}
	if !strings.Contains(stdout, "/workspace  sessions=1 usage=1 responses=1 tokens=20 estimated_cost=$0.0150") {
		t.Fatalf("expected workspace summary, got %q", stdout)
	}
	if !strings.Contains(stdout, "session-1  responses=1 tokens=20 estimated_cost=$0.0150 last=openai/gpt-4o-mini") {
		t.Fatalf("expected session summary, got %q", stdout)
	}
	if !strings.Contains(stdout, "Aggregate provider/model breakdowns are intentionally omitted") {
		t.Fatalf("expected limitations note, got %q", stdout)
	}
}

func TestStatsCommandJSONOutputAndWorkspaceFilter(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	mgr := session.NewManager()
	mgr.SetDataDir(filepath.Join(oauth.DefaultConfigDir(), "sessions"))
	first, err := mgr.Create("session-1", "C:/repo/a")
	if err != nil {
		t.Fatalf("Create(session-1) error: %v", err)
	}
	second, err := mgr.Create("session-2", "C:/repo/b")
	if err != nil {
		t.Fatalf("Create(session-2) error: %v", err)
	}
	if _, err := mgr.RecordUsage(first.ID, session.UsageUpdate{InputTokens: 10, OutputTokens: 5, PricingKnown: false, Provider: "openai", Model: "gpt-4o"}); err != nil {
		t.Fatalf("RecordUsage(session-1) error: %v", err)
	}
	if _, err := mgr.RecordUsage(second.ID, session.UsageUpdate{InputTokens: 20, OutputTokens: 10, TotalCost: 0.02, PricingKnown: true, Provider: "openai", Model: "gpt-4o-mini"}); err != nil {
		t.Fatalf("RecordUsage(session-2) error: %v", err)
	}
	for _, id := range []string{first.ID, second.ID} {
		if err := mgr.Save(id); err != nil {
			t.Fatalf("Save(%s) error: %v", id, err)
		}
	}

	stdout, _, err := executeRootCommandForTest(t, "stats", "--format", "json", "--workspace", "C:/repo/b", "--limit", "1")
	if err != nil {
		t.Fatalf("stats json error: %v", err)
	}

	var report session.StatsReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("unmarshal json output: %v\noutput: %s", err, stdout)
	}
	if report.Scope.WorkspaceRoot != "C:/repo/b" || report.Scope.SessionLimit != 1 {
		t.Fatalf("unexpected report scope: %#v", report.Scope)
	}
	if report.Total.TotalSessions != 1 || report.Total.TotalTokens != 30 || report.Total.TotalEstimatedCost != 0.02 {
		t.Fatalf("unexpected total stats: %#v", report.Total)
	}
	if len(report.ByWorkspace) != 1 || report.ByWorkspace[0].WorkspaceRoot != "C:/repo/b" {
		t.Fatalf("unexpected workspace stats: %#v", report.ByWorkspace)
	}
	if len(report.Sessions) != 1 || report.Sessions[0].ID != "session-2" {
		t.Fatalf("unexpected session summaries: %#v", report.Sessions)
	}
	if report.Total.PricingCoverage != 1 {
		t.Fatalf("expected full pricing coverage, got %#v", report.Total)
	}
	if len(report.Limitations) == 0 {
		t.Fatalf("expected limitations in json output")
	}
	if report.Sessions[0].LastUsageAt.IsZero() {
		t.Fatalf("expected last usage timestamp in session summary: %#v", report.Sessions[0])
	}
	if report.GeneratedAt.Before(time.Now().Add(-time.Minute)) {
		t.Fatalf("expected recent generation timestamp, got %s", report.GeneratedAt)
	}
}

func TestStatsCommandUsesPersistedSummariesWhenMessagesAreBroken(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	mgr := session.NewManager()
	dataDir := filepath.Join(oauth.DefaultConfigDir(), "sessions")
	mgr.SetDataDir(dataDir)
	created, err := mgr.Create("session-stats", "/workspace")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if err := mgr.AppendTranscript(created.ID, domain.TranscriptEntry{ID: "assistant-1", Kind: domain.TranscriptAssistant, Message: "hello", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("AppendTranscript() error: %v", err)
	}
	if _, err := mgr.RecordUsage(created.ID, session.UsageUpdate{InputTokens: 12, OutputTokens: 8, TotalCost: 0.015, PricingKnown: true, Provider: "openai", Model: "gpt-4o-mini"}); err != nil {
		t.Fatalf("RecordUsage() error: %v", err)
	}
	if err := mgr.Save(created.ID); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	if err := dropStatsTestMessagesTable(dataDir); err != nil {
		t.Fatalf("dropStatsTestMessagesTable() error: %v", err)
	}

	stdout, _, err := executeRootCommandForTest(t, "stats")
	if err != nil {
		t.Fatalf("stats error: %v", err)
	}
	if !strings.Contains(stdout, "Sessions: 1 total (1 with usage)") {
		t.Fatalf("expected summary-backed stats output, got %q", stdout)
	}
	if !strings.Contains(stdout, "session-stats  responses=1 tokens=20 estimated_cost=$0.0150") {
		t.Fatalf("expected session stats despite broken messages file, got %q", stdout)
	}
}

func dropStatsTestMessagesTable(dataDir string) error {
	db, err := sql.Open("sqlite", session.NewSQLiteStore(dataDir).DatabasePath())
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`DROP TABLE messages`)
	return err
}
