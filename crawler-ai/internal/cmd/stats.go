package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"crawler-ai/internal/session"

	"github.com/spf13/cobra"
)

var (
	statsFormat    string
	statsWorkspace string
	statsLimit     int
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show persisted usage statistics",
	Long:  "Aggregate persisted session usage totals into operator-facing usage and cost reports.",
	RunE: func(cmd *cobra.Command, args []string) error {
		services, err := loadStoredSessionServices()
		if err != nil {
			return err
		}
		summaries, err := services.Queries.ListSummaries()
		if err != nil {
			return err
		}

		report := session.BuildStatsReportFromSummaries(summaries, session.StatsOptions{
			WorkspaceRoot: strings.TrimSpace(statsWorkspace),
			SessionLimit:  statsLimit,
		})

		if strings.EqualFold(strings.TrimSpace(statsFormat), "json") {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(report)
		}

		if report.Total.TotalSessions == 0 {
			if strings.TrimSpace(statsWorkspace) != "" {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "No sessions found for workspace: %s\n", strings.TrimSpace(statsWorkspace))
				return nil
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No sessions found.")
			return nil
		}

		return writeStatsText(cmd, report)
	},
}

func init() {
	statsCmd.Flags().StringVar(&statsFormat, "format", "text", "Output format: text or json")
	statsCmd.Flags().StringVar(&statsWorkspace, "workspace", "", "Only include sessions for the given workspace path")
	statsCmd.Flags().IntVar(&statsLimit, "limit", 10, "Maximum number of session summaries to print")
	rootCmd.AddCommand(statsCmd)
}

func writeStatsText(cmd *cobra.Command, report session.StatsReport) error {
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Stats")
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
	if report.Scope.WorkspaceRoot != "" {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Workspace filter: %s\n", report.Scope.WorkspaceRoot)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Sessions: %d total (%d with usage)\n", report.Total.TotalSessions, report.Total.SessionsWithUsage)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Tokens: input=%d output=%d total=%d\n", report.Total.TotalInputTokens, report.Total.TotalOutputTokens, report.Total.TotalTokens)
	if report.Total.TotalResponses > 0 {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Responses: %d total (priced=%d unpriced=%d coverage=%.1f%%)\n", report.Total.TotalResponses, report.Total.PricedResponses, report.Total.UnpricedResponses, report.Total.PricingCoverage*100)
	} else {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Responses: 0 total")
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Estimated cost: $%.4f\n", report.Total.TotalEstimatedCost)
	if !report.Total.FirstSessionAt.IsZero() {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "First session: %s\n", report.Total.FirstSessionAt.Format("2006-01-02 15:04:05"))
	}
	if !report.Total.LastSessionAt.IsZero() {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Last session update: %s\n", report.Total.LastSessionAt.Format("2006-01-02 15:04:05"))
	}
	if !report.Total.LastUsageAt.IsZero() {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Last usage update: %s\n", report.Total.LastUsageAt.Format("2006-01-02 15:04:05"))
	}

	if len(report.ByWorkspace) > 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "By workspace")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
		for _, item := range report.ByWorkspace {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s  sessions=%d usage=%d responses=%d tokens=%d estimated_cost=$%.4f\n",
				item.WorkspaceRoot,
				item.SessionCount,
				item.SessionsWithUsage,
				item.TotalResponses,
				item.TotalTokens,
				item.TotalEstimatedCost,
			)
		}
	}

	if len(report.Sessions) > 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Recent sessions")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
		for _, item := range report.Sessions {
			lastModel := item.LastModel
			if strings.TrimSpace(lastModel) == "" {
				lastModel = "n/a"
			}
			lastProvider := item.LastProvider
			if strings.TrimSpace(lastProvider) == "" {
				lastProvider = "n/a"
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s  responses=%d tokens=%d estimated_cost=$%.4f last=%s/%s updated=%s\n",
				item.ID,
				item.ResponseCount,
				item.TotalTokens,
				item.TotalEstimatedCost,
				lastProvider,
				lastModel,
				item.UpdatedAt.Format("2006-01-02 15:04:05"),
			)
		}
	}

	if len(report.Limitations) > 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Notes")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
		for _, limitation := range report.Limitations {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", limitation)
		}
	}

	return nil
}
