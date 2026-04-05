package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"crawler-ai/internal/session"

	"github.com/spf13/cobra"
)

var (
	sessionsDiagnoseFormat  string
	sessionsDiagnoseSection string
)

var sessionsDiagnoseCmd = &cobra.Command{
	Use:     "diagnose [session-id]",
	Aliases: []string{"inspect"},
	Short:   "Inspect persisted session diagnostics",
	Long:    "Inspect persisted transcript, tracked file, and storage-health diagnostics for one stored session.",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		services, err := loadStoredSessionServices()
		if err != nil {
			return err
		}
		report, err := services.Queries.Diagnostics(args[0])
		if err != nil {
			return fmt.Errorf("session not found: %s", args[0])
		}

		section, err := normalizeSessionDiagnosticsSection(sessionsDiagnoseSection)
		if err != nil {
			return err
		}
		output := filterSessionDiagnosticsReport(report, section)

		if strings.EqualFold(strings.TrimSpace(sessionsDiagnoseFormat), "json") {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(output)
		}
		return writeSessionDiagnosticsText(cmd, output)
	},
}

type sessionDiagnosticsOutput struct {
	GeneratedAt   string                           `json:"generated_at"`
	SessionID     string                           `json:"session_id"`
	Summary       session.SessionSummary           `json:"summary"`
	Storage       *session.StorageHealth           `json:"storage,omitempty"`
	Messages      *session.MessageDiagnostics      `json:"messages,omitempty"`
	Files         *session.FileDiagnostics         `json:"files,omitempty"`
	Findings      []session.DiagnosticFinding      `json:"findings,omitempty"`
	SectionErrors []session.DiagnosticSectionError `json:"section_errors,omitempty"`
}

func init() {
	sessionsCmd.AddCommand(sessionsDiagnoseCmd)
	sessionsDiagnoseCmd.Flags().StringVar(&sessionsDiagnoseFormat, "format", "text", "Output format: text or json")
	sessionsDiagnoseCmd.Flags().StringVar(&sessionsDiagnoseSection, "section", "all", "Projection to print: all, messages, files, or storage")
}

func normalizeSessionDiagnosticsSection(value string) (string, error) {
	section := strings.ToLower(strings.TrimSpace(value))
	if section == "" {
		section = "all"
	}
	switch section {
	case "all", "messages", "files", "storage":
		return section, nil
	default:
		return "", fmt.Errorf("unsupported diagnostics section: %s", value)
	}
}

func filterSessionDiagnosticsReport(report session.SessionDiagnosticsReport, section string) sessionDiagnosticsOutput {
	output := sessionDiagnosticsOutput{
		GeneratedAt:   report.GeneratedAt.Format("2006-01-02T15:04:05Z07:00"),
		SessionID:     report.SessionID,
		Summary:       report.Summary,
		Findings:      append([]session.DiagnosticFinding(nil), report.Findings...),
		SectionErrors: append([]session.DiagnosticSectionError(nil), report.SectionErrors...),
	}
	switch section {
	case "messages":
		output.Messages = report.Messages
	case "files":
		output.Files = report.Files
	case "storage":
		output.Storage = report.Storage
	default:
		output.Storage = report.Storage
		output.Messages = report.Messages
		output.Files = report.Files
	}
	return output
}

func writeSessionDiagnosticsText(cmd *cobra.Command, output sessionDiagnosticsOutput) error {
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Session diagnostics: %s\n", output.SessionID)
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Workspace: %s\n", output.Summary.WorkspaceRoot)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Generated: %s\n", output.GeneratedAt)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Summary: messages=%d tasks=%d files=%d responses=%d estimated_cost=$%.4f\n",
		output.Summary.MessageCount,
		output.Summary.TaskCount,
		output.Summary.FileCount,
		output.Summary.Usage.ResponseCount,
		output.Summary.Usage.TotalCost,
	)

	if output.Storage != nil {
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Storage health")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Backend: %s\n", output.Storage.Backend)
		if strings.TrimSpace(output.Storage.Path) != "" {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Path: %s\n", output.Storage.Path)
		}
		if output.Storage.SchemaVersion > 0 || output.Storage.LatestSchemaVersion > 0 {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Schema: %d/%d\n", output.Storage.SchemaVersion, output.Storage.LatestSchemaVersion)
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Counts: sessions=%d messages=%d tasks=%d files=%d usage_rows=%d\n",
			output.Storage.Counts.Sessions,
			output.Storage.Counts.Messages,
			output.Storage.Counts.Tasks,
			output.Storage.Counts.Files,
			output.Storage.Counts.UsageTotals,
		)
		for _, note := range output.Storage.Notes {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Note: %s\n", note)
		}
	}

	if output.Messages != nil {
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Persisted messages")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Totals: entries=%d assistant=%d reasoning=%d provider_in_progress=%d missing_finish_reason=%d\n",
			output.Messages.Total,
			output.Messages.AssistantResponses,
			output.Messages.ReasoningEntries,
			output.Messages.ProviderInProgress,
			output.Messages.MissingFinishReason,
		)
		writeDiagnosticCounts(cmd, "By kind", output.Messages.ByKind)
		writeDiagnosticCounts(cmd, "Finish reasons", output.Messages.FinishReasons)
		writeDiagnosticCounts(cmd, "Tool statuses", output.Messages.ToolStatuses)
		if !output.Messages.LastUpdatedAt.IsZero() {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Last update: %s\n", output.Messages.LastUpdatedAt.Format("2006-01-02 15:04:05"))
		}
	}

	if output.Files != nil {
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Tracked files")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Totals: files=%d history_snapshots=%d missing_source_path=%d\n",
			output.Files.Total,
			output.Files.HistorySnapshots,
			output.Files.MissingSourcePath,
		)
		writeDiagnosticCounts(cmd, "By kind", output.Files.ByKind)
		writeDiagnosticCounts(cmd, "By tool", output.Files.ByTool)
		if !output.Files.LastTrackedAt.IsZero() {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Last tracked: %s\n", output.Files.LastTrackedAt.Format("2006-01-02 15:04:05"))
		}
	}

	if len(output.SectionErrors) > 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Section errors")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
		for _, item := range output.SectionErrors {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s\n", item.Section, item.Error)
		}
	}

	_, _ = fmt.Fprintln(cmd.OutOrStdout())
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Findings")
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
	if len(output.Findings) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No findings.")
		return nil
	}
	for _, finding := range output.Findings {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "[%s/%s] %s\n", finding.Category, finding.Severity, finding.Message)
	}
	return nil
}

func writeDiagnosticCounts(cmd *cobra.Command, label string, counts []session.DiagnosticCount) {
	if len(counts) == 0 {
		return
	}
	parts := make([]string, 0, len(counts))
	for _, item := range counts {
		parts = append(parts, fmt.Sprintf("%s=%d", item.Name, item.Count))
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", label, strings.Join(parts, " "))
}
