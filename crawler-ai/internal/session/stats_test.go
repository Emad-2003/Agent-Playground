package session

import (
	"testing"
	"time"

	"crawler-ai/internal/domain"
)

func TestBuildStatsReportAggregatesTotalsAndSorting(t *testing.T) {
	now := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
	report := BuildStatsReport([]Session{
		{
			ID:            "session-b",
			WorkspaceRoot: "/workspace/b",
			CreatedAt:     time.Date(2026, 4, 2, 9, 0, 0, 0, time.UTC),
			UpdatedAt:     time.Date(2026, 4, 4, 10, 0, 0, 0, time.UTC),
			Usage: UsageTotals{
				InputTokens:     60,
				OutputTokens:    40,
				ResponseCount:   1,
				PricedResponses: 1,
				TotalCost:       0.03,
				LastProvider:    "openai",
				LastModel:       "gpt-4o-mini",
				UpdatedAt:       time.Date(2026, 4, 4, 10, 0, 0, 0, time.UTC),
			},
		},
		{
			ID:            "session-a",
			WorkspaceRoot: "/workspace/a",
			CreatedAt:     time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC),
			UpdatedAt:     time.Date(2026, 4, 4, 11, 0, 0, 0, time.UTC),
			Tasks:         []domain.Task{{ID: "task-1"}},
			Transcript:    []domain.TranscriptEntry{{ID: "entry-1"}, {ID: "entry-2"}},
			Usage: UsageTotals{
				InputTokens:       100,
				OutputTokens:      50,
				ResponseCount:     2,
				PricedResponses:   1,
				UnpricedResponses: 1,
				TotalCost:         0.02,
				LastProvider:      "anthropic",
				LastModel:         "claude-sonnet-4-20250514",
				UpdatedAt:         time.Date(2026, 4, 4, 11, 0, 0, 0, time.UTC),
			},
		},
	}, StatsOptions{SessionLimit: 1, Now: func() time.Time { return now }})

	if !report.GeneratedAt.Equal(now) {
		t.Fatalf("expected generated time %s, got %s", now, report.GeneratedAt)
	}
	if report.Total.TotalSessions != 2 || report.Total.SessionsWithUsage != 2 {
		t.Fatalf("unexpected session totals: %#v", report.Total)
	}
	if report.Total.TotalInputTokens != 160 || report.Total.TotalOutputTokens != 90 || report.Total.TotalTokens != 250 {
		t.Fatalf("unexpected token totals: %#v", report.Total)
	}
	if report.Total.TotalResponses != 3 || report.Total.PricedResponses != 2 || report.Total.UnpricedResponses != 1 {
		t.Fatalf("unexpected response totals: %#v", report.Total)
	}
	if report.Total.TotalEstimatedCost != 0.05 {
		t.Fatalf("expected total estimated cost 0.05, got %f", report.Total.TotalEstimatedCost)
	}
	if report.Total.PricingCoverage != 2.0/3.0 {
		t.Fatalf("expected pricing coverage 2/3, got %f", report.Total.PricingCoverage)
	}
	if report.Total.FirstSessionAt != time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC) {
		t.Fatalf("unexpected first session time: %s", report.Total.FirstSessionAt)
	}
	if report.Total.LastSessionAt != time.Date(2026, 4, 4, 11, 0, 0, 0, time.UTC) {
		t.Fatalf("unexpected last session time: %s", report.Total.LastSessionAt)
	}
	if report.Total.LastUsageAt != time.Date(2026, 4, 4, 11, 0, 0, 0, time.UTC) {
		t.Fatalf("unexpected last usage time: %s", report.Total.LastUsageAt)
	}
	if len(report.ByWorkspace) != 2 {
		t.Fatalf("expected 2 workspace rows, got %#v", report.ByWorkspace)
	}
	if report.ByWorkspace[0].WorkspaceRoot != "/workspace/a" || report.ByWorkspace[0].TotalTokens != 150 {
		t.Fatalf("expected workspace a first, got %#v", report.ByWorkspace)
	}
	if len(report.Sessions) != 1 {
		t.Fatalf("expected limited session summaries, got %#v", report.Sessions)
	}
	if report.Sessions[0].ID != "session-a" {
		t.Fatalf("expected newest session first, got %#v", report.Sessions)
	}
	if report.Sessions[0].TranscriptEntries != 2 || report.Sessions[0].TaskCount != 1 {
		t.Fatalf("expected transcript and task counts in session summary, got %#v", report.Sessions[0])
	}
	if len(report.Limitations) != 2 {
		t.Fatalf("expected limitations to be populated, got %#v", report.Limitations)
	}
}

func TestBuildStatsReportFiltersWorkspace(t *testing.T) {
	report := BuildStatsReport([]Session{
		{ID: "session-a", WorkspaceRoot: "C:/repo/a", UpdatedAt: time.Date(2026, 4, 4, 10, 0, 0, 0, time.UTC), Usage: UsageTotals{ResponseCount: 1, InputTokens: 10, OutputTokens: 5}},
		{ID: "session-b", WorkspaceRoot: "C:/repo/b", UpdatedAt: time.Date(2026, 4, 4, 11, 0, 0, 0, time.UTC), Usage: UsageTotals{ResponseCount: 2, InputTokens: 20, OutputTokens: 10}},
	}, StatsOptions{WorkspaceRoot: "C:/repo/b", SessionLimit: 10})

	if report.Total.TotalSessions != 1 {
		t.Fatalf("expected one filtered session, got %#v", report.Total)
	}
	if len(report.ByWorkspace) != 1 || report.ByWorkspace[0].WorkspaceRoot != "C:/repo/b" {
		t.Fatalf("unexpected filtered workspaces: %#v", report.ByWorkspace)
	}
	if len(report.Sessions) != 1 || report.Sessions[0].ID != "session-b" {
		t.Fatalf("unexpected filtered session summaries: %#v", report.Sessions)
	}
	if report.Scope.WorkspaceRoot != "C:/repo/b" {
		t.Fatalf("unexpected scope: %#v", report.Scope)
	}
	if report.Total.TotalTokens != 30 {
		t.Fatalf("unexpected filtered token total: %#v", report.Total)
	}
}
