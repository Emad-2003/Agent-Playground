package session

import (
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

type StatsOptions struct {
	WorkspaceRoot string
	SessionLimit  int
	Now           func() time.Time
}

type StatsReport struct {
	GeneratedAt time.Time        `json:"generated_at"`
	Scope       StatsScope       `json:"scope"`
	Total       StatsTotals      `json:"total"`
	ByWorkspace []WorkspaceStats `json:"by_workspace,omitempty"`
	Sessions    []SessionStats   `json:"sessions,omitempty"`
	Limitations []string         `json:"limitations,omitempty"`
}

type StatsScope struct {
	WorkspaceRoot string `json:"workspace_root,omitempty"`
	SessionLimit  int    `json:"session_limit"`
}

type StatsTotals struct {
	TotalSessions      int       `json:"total_sessions"`
	SessionsWithUsage  int       `json:"sessions_with_usage"`
	TotalInputTokens   int64     `json:"total_input_tokens"`
	TotalOutputTokens  int64     `json:"total_output_tokens"`
	TotalTokens        int64     `json:"total_tokens"`
	TotalResponses     int64     `json:"total_responses"`
	PricedResponses    int64     `json:"priced_responses"`
	UnpricedResponses  int64     `json:"unpriced_responses"`
	TotalEstimatedCost float64   `json:"total_estimated_cost"`
	PricingCoverage    float64   `json:"pricing_coverage"`
	FirstSessionAt     time.Time `json:"first_session_at,omitempty"`
	LastSessionAt      time.Time `json:"last_session_at,omitempty"`
	LastUsageAt        time.Time `json:"last_usage_at,omitempty"`
}

type WorkspaceStats struct {
	WorkspaceRoot      string    `json:"workspace_root"`
	SessionCount       int       `json:"session_count"`
	SessionsWithUsage  int       `json:"sessions_with_usage"`
	TotalInputTokens   int64     `json:"total_input_tokens"`
	TotalOutputTokens  int64     `json:"total_output_tokens"`
	TotalTokens        int64     `json:"total_tokens"`
	TotalResponses     int64     `json:"total_responses"`
	PricedResponses    int64     `json:"priced_responses"`
	UnpricedResponses  int64     `json:"unpriced_responses"`
	TotalEstimatedCost float64   `json:"total_estimated_cost"`
	LastSessionAt      time.Time `json:"last_session_at,omitempty"`
	LastUsageAt        time.Time `json:"last_usage_at,omitempty"`
}

type SessionStats struct {
	ID                 string    `json:"id"`
	WorkspaceRoot      string    `json:"workspace_root"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	LastUsageAt        time.Time `json:"last_usage_at,omitempty"`
	LastProvider       string    `json:"last_provider,omitempty"`
	LastModel          string    `json:"last_model,omitempty"`
	TranscriptEntries  int       `json:"transcript_entries"`
	TaskCount          int       `json:"task_count"`
	InputTokens        int64     `json:"input_tokens"`
	OutputTokens       int64     `json:"output_tokens"`
	TotalTokens        int64     `json:"total_tokens"`
	ResponseCount      int64     `json:"response_count"`
	PricedResponses    int64     `json:"priced_responses"`
	UnpricedResponses  int64     `json:"unpriced_responses"`
	TotalEstimatedCost float64   `json:"total_estimated_cost"`
}

func BuildStatsReport(sessions []Session, options StatsOptions) StatsReport {
	views := make([]statsSessionView, 0, len(sessions))
	for _, sess := range sessions {
		views = append(views, statsSessionView{
			ID:                sess.ID,
			WorkspaceRoot:     sess.WorkspaceRoot,
			CreatedAt:         sess.CreatedAt,
			UpdatedAt:         sess.UpdatedAt,
			LastUsageAt:       sess.Usage.UpdatedAt,
			LastProvider:      sess.Usage.LastProvider,
			LastModel:         sess.Usage.LastModel,
			TranscriptEntries: len(sess.Transcript),
			TaskCount:         len(sess.Tasks),
			Usage:             sess.Usage,
		})
	}
	return buildStatsReport(views, options)
}

func BuildStatsReportFromSummaries(summaries []SessionSummary, options StatsOptions) StatsReport {
	views := make([]statsSessionView, 0, len(summaries))
	for _, summary := range summaries {
		views = append(views, statsSessionView{
			ID:                summary.ID,
			WorkspaceRoot:     summary.WorkspaceRoot,
			CreatedAt:         summary.CreatedAt,
			UpdatedAt:         summary.UpdatedAt,
			LastUsageAt:       summary.Usage.UpdatedAt,
			LastProvider:      summary.Usage.LastProvider,
			LastModel:         summary.Usage.LastModel,
			TranscriptEntries: summary.MessageCount,
			TaskCount:         summary.TaskCount,
			Usage:             summary.Usage,
		})
	}
	return buildStatsReport(views, options)
}

type statsSessionView struct {
	ID                string
	WorkspaceRoot     string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	LastUsageAt       time.Time
	LastProvider      string
	LastModel         string
	TranscriptEntries int
	TaskCount         int
	Usage             UsageTotals
}

func buildStatsReport(views []statsSessionView, options StatsOptions) StatsReport {
	now := time.Now().UTC()
	if options.Now != nil {
		now = options.Now().UTC()
	}

	limit := options.SessionLimit
	if limit < 0 {
		limit = 0
	}

	filtered := make([]statsSessionView, 0, len(views))
	for _, sess := range views {
		if options.WorkspaceRoot != "" && !sameWorkspace(sess.WorkspaceRoot, options.WorkspaceRoot) {
			continue
		}
		filtered = append(filtered, sess)
	}

	sort.Slice(filtered, func(i, j int) bool {
		if !filtered[i].UpdatedAt.Equal(filtered[j].UpdatedAt) {
			return filtered[i].UpdatedAt.After(filtered[j].UpdatedAt)
		}
		if !filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
		}
		return filtered[i].ID < filtered[j].ID
	})

	report := StatsReport{
		GeneratedAt: now,
		Scope: StatsScope{
			WorkspaceRoot: strings.TrimSpace(options.WorkspaceRoot),
			SessionLimit:  limit,
		},
		ByWorkspace: make([]WorkspaceStats, 0),
		Sessions:    make([]SessionStats, 0),
		Limitations: []string{
			"Estimated cost uses the local provider catalog and is not yet reconciled against provider invoices.",
			"Aggregate provider/model breakdowns are intentionally omitted until per-response usage records are stored instead of session-only totals.",
		},
	}

	workspaceStats := make(map[string]*WorkspaceStats)
	for _, sess := range filtered {
		usage := sess.Usage
		report.Total.TotalSessions++
		report.Total.TotalInputTokens += usage.InputTokens
		report.Total.TotalOutputTokens += usage.OutputTokens
		report.Total.TotalResponses += usage.ResponseCount
		report.Total.PricedResponses += usage.PricedResponses
		report.Total.UnpricedResponses += usage.UnpricedResponses
		report.Total.TotalEstimatedCost += usage.TotalCost

		if report.Total.FirstSessionAt.IsZero() || sess.CreatedAt.Before(report.Total.FirstSessionAt) {
			report.Total.FirstSessionAt = sess.CreatedAt
		}
		if report.Total.LastSessionAt.IsZero() || sess.UpdatedAt.After(report.Total.LastSessionAt) {
			report.Total.LastSessionAt = sess.UpdatedAt
		}
		if !usage.UpdatedAt.IsZero() && (report.Total.LastUsageAt.IsZero() || usage.UpdatedAt.After(report.Total.LastUsageAt)) {
			report.Total.LastUsageAt = usage.UpdatedAt
		}
		if usageTotalsPresent(usage) {
			report.Total.SessionsWithUsage++
		}

		workspaceRoot := sess.WorkspaceRoot
		if workspaceRoot == "" {
			workspaceRoot = "."
		}
		current := workspaceStats[workspaceRoot]
		if current == nil {
			current = &WorkspaceStats{WorkspaceRoot: workspaceRoot}
			workspaceStats[workspaceRoot] = current
		}
		current.SessionCount++
		current.TotalInputTokens += usage.InputTokens
		current.TotalOutputTokens += usage.OutputTokens
		current.TotalResponses += usage.ResponseCount
		current.PricedResponses += usage.PricedResponses
		current.UnpricedResponses += usage.UnpricedResponses
		current.TotalEstimatedCost += usage.TotalCost
		if usageTotalsPresent(usage) {
			current.SessionsWithUsage++
		}
		if current.LastSessionAt.IsZero() || sess.UpdatedAt.After(current.LastSessionAt) {
			current.LastSessionAt = sess.UpdatedAt
		}
		if !usage.UpdatedAt.IsZero() && (current.LastUsageAt.IsZero() || usage.UpdatedAt.After(current.LastUsageAt)) {
			current.LastUsageAt = usage.UpdatedAt
		}
	}

	report.Total.TotalTokens = report.Total.TotalInputTokens + report.Total.TotalOutputTokens
	if report.Total.TotalResponses > 0 {
		report.Total.PricingCoverage = float64(report.Total.PricedResponses) / float64(report.Total.TotalResponses)
	}

	report.ByWorkspace = make([]WorkspaceStats, 0, len(workspaceStats))
	for _, current := range workspaceStats {
		current.TotalTokens = current.TotalInputTokens + current.TotalOutputTokens
		report.ByWorkspace = append(report.ByWorkspace, *current)
	}
	sort.Slice(report.ByWorkspace, func(i, j int) bool {
		if report.ByWorkspace[i].TotalTokens != report.ByWorkspace[j].TotalTokens {
			return report.ByWorkspace[i].TotalTokens > report.ByWorkspace[j].TotalTokens
		}
		if report.ByWorkspace[i].TotalEstimatedCost != report.ByWorkspace[j].TotalEstimatedCost {
			return report.ByWorkspace[i].TotalEstimatedCost > report.ByWorkspace[j].TotalEstimatedCost
		}
		if report.ByWorkspace[i].SessionCount != report.ByWorkspace[j].SessionCount {
			return report.ByWorkspace[i].SessionCount > report.ByWorkspace[j].SessionCount
		}
		return report.ByWorkspace[i].WorkspaceRoot < report.ByWorkspace[j].WorkspaceRoot
	})

	for _, sess := range filtered {
		usage := sess.Usage
		report.Sessions = append(report.Sessions, SessionStats{
			ID:                 sess.ID,
			WorkspaceRoot:      sess.WorkspaceRoot,
			CreatedAt:          sess.CreatedAt,
			UpdatedAt:          sess.UpdatedAt,
			LastUsageAt:        sess.LastUsageAt,
			LastProvider:       sess.LastProvider,
			LastModel:          sess.LastModel,
			TranscriptEntries:  sess.TranscriptEntries,
			TaskCount:          sess.TaskCount,
			InputTokens:        usage.InputTokens,
			OutputTokens:       usage.OutputTokens,
			TotalTokens:        usage.InputTokens + usage.OutputTokens,
			ResponseCount:      usage.ResponseCount,
			PricedResponses:    usage.PricedResponses,
			UnpricedResponses:  usage.UnpricedResponses,
			TotalEstimatedCost: usage.TotalCost,
		})
	}
	if limit > 0 && len(report.Sessions) > limit {
		report.Sessions = report.Sessions[:limit]
	}

	return report
}

func usageTotalsPresent(usage UsageTotals) bool {
	return usage.InputTokens > 0 || usage.OutputTokens > 0 || usage.ResponseCount > 0 || usage.PricedResponses > 0 || usage.UnpricedResponses > 0 || usage.TotalCost > 0
}

func sameWorkspace(left, right string) bool {
	left = normalizeWorkspacePath(left)
	right = normalizeWorkspacePath(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func normalizeWorkspacePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}
