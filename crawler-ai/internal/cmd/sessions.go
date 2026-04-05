package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"crawler-ai/internal/domain"
	"crawler-ai/internal/session"

	"github.com/spf13/cobra"
)

var sessionsCmd = &cobra.Command{
	Use:     "sessions",
	Aliases: []string{"session"},
	Short:   "Manage sessions",
	Long:    "List, inspect, and manage conversation sessions.",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			return runSessionsShow(cmd, args)
		}
		return cmd.Help()
	},
}

var (
	sessionsShowFormat     string
	sessionsChildrenFormat string
	sessionsTasksFormat    string
	sessionsTasksStatus    string
	sessionsUsageFormat    string
	sessionsDeleteYes      bool
	sessionsExportFormat   string
	sessionsExportOutput   string
	sessionsExportFilter   string
)

var sessionsTasksCmd = &cobra.Command{
	Use:   "tasks [session-id]",
	Short: "Inspect persisted tasks",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		services, err := loadStoredSessionServices()
		if err != nil {
			return err
		}
		statuses, err := normalizeSessionTaskStatuses(sessionsTasksStatus)
		if err != nil {
			return err
		}
		projection, err := services.Tasks.FilteredProjection(args[0], session.TaskQueryOptions{Statuses: statuses})
		if err != nil {
			return fmt.Errorf("session not found: %s", args[0])
		}
		if strings.EqualFold(strings.TrimSpace(sessionsTasksFormat), "json") {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(projection)
		}
		return writeSessionTasksText(cmd, projection)
	},
}

var sessionsUsageCmd = &cobra.Command{
	Use:   "usage [session-id]",
	Short: "Inspect persisted usage totals",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		services, err := loadStoredSessionServices()
		if err != nil {
			return err
		}
		projection, err := services.Usage.Projection(args[0])
		if err != nil {
			return fmt.Errorf("session not found: %s", args[0])
		}
		if strings.EqualFold(strings.TrimSpace(sessionsUsageFormat), "json") {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(projection)
		}
		return writeSessionUsageText(cmd, projection)
	},
}

var sessionsListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List root sessions",
	RunE: func(cmd *cobra.Command, args []string) error {
		services, err := loadStoredSessionServices()
		if err != nil {
			return err
		}
		summaries, err := services.Queries.ListRootSummaries()
		if err != nil {
			return err
		}
		if len(summaries) == 0 {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No sessions found.")
			return nil
		}

		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Sessions")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
		for _, s := range summaries {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s  workspace=%s  created=%s  transcript=%d entries\n",
				s.ID, s.WorkspaceRoot, s.CreatedAt.Format("2006-01-02 15:04"), s.MessageCount)
		}
		return nil
	},
}

var sessionsShowCmd = &cobra.Command{
	Use:   "show [session-id]",
	Short: "Show session details",
	Args:  cobra.ExactArgs(1),
	RunE:  runSessionsShow,
}

var sessionsChildrenCmd = &cobra.Command{
	Use:   "children [session-id]",
	Short: "List child sessions for one stored session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		services, err := loadStoredSessionServices()
		if err != nil {
			return err
		}
		summary, err := services.Queries.Summary(args[0])
		if err != nil {
			return fmt.Errorf("session not found: %s", args[0])
		}
		children, err := services.Queries.ChildSummaries(args[0])
		if err != nil {
			return err
		}
		output := sessionChildrenOutput{SessionID: args[0], Summary: summary, Children: children}
		if strings.EqualFold(strings.TrimSpace(sessionsChildrenFormat), "json") {
			return writeSessionJSON(cmd.OutOrStdout(), output)
		}
		return writeSessionChildrenText(cmd, output)
	},
}

var sessionsDeleteCmd = &cobra.Command{
	Use:   "delete [session-id]",
	Short: "Delete a stored session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if !sessionsDeleteYes {
			return fmt.Errorf("sessions delete is destructive; rerun with --yes to confirm")
		}

		services, err := loadStoredSessionServices()
		if err != nil {
			return err
		}
		if _, err := services.Queries.Summary(args[0]); err != nil {
			return fmt.Errorf("session not found: %s", args[0])
		}
		if err := services.Lifecycle.Delete(args[0]); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Deleted session: %s\n", args[0])
		return nil
	},
}

var sessionsExportCmd = &cobra.Command{
	Use:   "export [session-id]",
	Short: "Export a stored session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		services, err := loadStoredSessionServices()
		if err != nil {
			return err
		}
		format, err := normalizeSessionExportFormat(sessionsExportFormat)
		if err != nil {
			return err
		}
		filter, err := normalizeSessionExportFilter(sessionsExportFilter)
		if err != nil {
			return err
		}
		projection, err := services.Queries.Export(args[0], filter)
		if err != nil {
			return fmt.Errorf("session not found: %s", args[0])
		}
		content, err := encodeSessionExport(projection, format)
		if err != nil {
			return err
		}

		outputPath := strings.TrimSpace(sessionsExportOutput)
		if outputPath == "" {
			_, err = cmd.OutOrStdout().Write(content)
			return err
		}

		outputPath = filepath.Clean(outputPath)
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(outputPath, content, 0o644); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Exported session %s to %s\n", projection.SessionID, outputPath)
		return nil
	},
}

func init() {
	sessionsCmd.AddCommand(sessionsListCmd)
	sessionsCmd.AddCommand(sessionsShowCmd)
	sessionsCmd.AddCommand(sessionsChildrenCmd)
	sessionsCmd.AddCommand(sessionsTasksCmd)
	sessionsCmd.AddCommand(sessionsUsageCmd)
	sessionsCmd.AddCommand(sessionsDeleteCmd)
	sessionsCmd.AddCommand(sessionsExportCmd)
	sessionsShowCmd.Flags().StringVar(&sessionsShowFormat, "format", "text", "Output format: text or json")
	sessionsChildrenCmd.Flags().StringVar(&sessionsChildrenFormat, "format", "text", "Output format: text or json")
	sessionsTasksCmd.Flags().StringVar(&sessionsTasksFormat, "format", "text", "Output format: text or json")
	sessionsTasksCmd.Flags().StringVar(&sessionsTasksStatus, "status", "", "Optional comma-separated task statuses: pending, running, completed, failed, blocked")
	sessionsUsageCmd.Flags().StringVar(&sessionsUsageFormat, "format", "text", "Output format: text or json")
	sessionsDeleteCmd.Flags().BoolVarP(&sessionsDeleteYes, "yes", "y", false, "Confirm deletion without prompting")
	sessionsExportCmd.Flags().StringVar(&sessionsExportFormat, "format", "json", "Export format: json or markdown")
	sessionsExportCmd.Flags().StringVar(&sessionsExportFilter, "filter", "full", "Export filter: full, transcript, usage, or redacted")
	sessionsExportCmd.Flags().StringVarP(&sessionsExportOutput, "output", "o", "", "Write the export to a file instead of stdout")
	rootCmd.AddCommand(sessionsCmd)
}

func runSessionsShow(cmd *cobra.Command, args []string) error {
	services, err := loadStoredSessionServices()
	if err != nil {
		return err
	}

	if strings.EqualFold(strings.TrimSpace(sessionsShowFormat), "json") {
		projection, err := services.Queries.FullSession(args[0])
		if err != nil {
			return fmt.Errorf("session not found: %s", args[0])
		}
		return writeSessionJSON(cmd.OutOrStdout(), projection)
	}

	view, err := services.Queries.Show(args[0])
	if err != nil {
		return fmt.Errorf("session not found: %s", args[0])
	}
	summary := view.Summary

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Session: %s\n", summary.ID)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Workspace: %s\n", summary.WorkspaceRoot)
	if strings.TrimSpace(summary.ParentSessionID) != "" {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Parent: %s\n", summary.ParentSessionID)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Created: %s\n", summary.CreatedAt.Format("2006-01-02 15:04:05"))
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Updated: %s\n", summary.UpdatedAt.Format("2006-01-02 15:04:05"))
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Tasks: %d tracked (%d active ids)\n", summary.TaskCount, len(summary.ActiveTaskIDs))
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Files: %d tracked\n", summary.FileCount)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Transcript: %d entries\n", summary.MessageCount)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Usage: input=%d output=%d responses=%d priced=%d unpriced=%d estimated_cost=$%.2f\n", summary.Usage.InputTokens, summary.Usage.OutputTokens, summary.Usage.ResponseCount, summary.Usage.PricedResponses, summary.Usage.UnpricedResponses, summary.Usage.TotalCost)
	if len(view.Children) > 0 {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Child sessions: %d\n", len(view.Children))
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout())

	if len(view.Children) > 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Child sessions:")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
		for _, child := range view.Children {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s  updated=%s  transcript=%d tasks=%d files=%d\n", child.ID, child.UpdatedAt.Format("2006-01-02 15:04"), child.MessageCount, child.TaskCount, child.FileCount)
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
	}

	if len(view.Transcript) > 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Transcript:")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
		for _, entry := range view.Transcript {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s: %s\n", entry.CreatedAt.Format("15:04:05"), entry.Kind, truncate(entry.Message, 120))
		}
	}

	return nil
}

func writeSessionJSON(w io.Writer, s any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(s)
}

type sessionChildrenOutput struct {
	SessionID string                   `json:"session_id"`
	Summary   session.SessionSummary   `json:"summary"`
	Children  []session.SessionSummary `json:"children,omitempty"`
}

func writeSessionChildrenText(cmd *cobra.Command, output sessionChildrenOutput) error {
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Session children: %s\n", output.SessionID)
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Workspace: %s\n", output.Summary.WorkspaceRoot)
	if strings.TrimSpace(output.Summary.ParentSessionID) != "" {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Parent: %s\n", output.Summary.ParentSessionID)
	}
	if len(output.Children) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No child sessions found.")
		return nil
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Children: %d\n", len(output.Children))
	for _, child := range output.Children {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s  updated=%s  transcript=%d tasks=%d files=%d\n", child.ID, child.UpdatedAt.Format("2006-01-02 15:04"), child.MessageCount, child.TaskCount, child.FileCount)
	}
	return nil
}

func writeSessionTasksText(cmd *cobra.Command, projection session.TaskProjection) error {
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Session tasks: %s\n", projection.SessionID)
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
	active := strings.Join(projection.ActiveTaskIDs, ", ")
	if active == "" {
		active = "none"
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Active task ids: %s\n", active)
	if len(projection.Tasks) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No tasks found.")
		return nil
	}
	for _, task := range projection.Tasks {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s assignee=%s\n", task.Status, task.Title, task.Assignee)
		if strings.TrimSpace(task.Description) != "" {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  description: %s\n", task.Description)
		}
		if strings.TrimSpace(task.Result) != "" {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  result: %s\n", task.Result)
		}
	}
	return nil
}

func writeSessionUsageText(cmd *cobra.Command, projection session.UsageProjection) error {
	usage := projection.Usage
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Session usage: %s\n", projection.SessionID)
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Tokens: input=%d output=%d total=%d\n", usage.InputTokens, usage.OutputTokens, usage.InputTokens+usage.OutputTokens)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Responses: total=%d priced=%d unpriced=%d\n", usage.ResponseCount, usage.PricedResponses, usage.UnpricedResponses)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Estimated cost: $%.4f\n", usage.TotalCost)
	if strings.TrimSpace(usage.LastProvider) != "" || strings.TrimSpace(usage.LastModel) != "" {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Last model: %s/%s\n", usage.LastProvider, usage.LastModel)
	}
	if !usage.UpdatedAt.IsZero() {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Updated: %s\n", usage.UpdatedAt.Format("2006-01-02 15:04:05"))
	}
	return nil
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func normalizeSessionExportFormat(value string) (string, error) {
	format := strings.ToLower(strings.TrimSpace(value))
	if format == "" {
		format = "json"
	}
	switch format {
	case "json", "markdown":
		return format, nil
	default:
		return "", fmt.Errorf("unsupported export format: %s", value)
	}
}

func normalizeSessionTaskStatuses(value string) ([]domain.TaskStatus, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	statuses := make([]domain.TaskStatus, 0, len(parts))
	seen := make(map[domain.TaskStatus]struct{}, len(parts))
	for _, part := range parts {
		status := domain.TaskStatus(strings.ToLower(strings.TrimSpace(part)))
		switch status {
		case domain.TaskPending, domain.TaskRunning, domain.TaskCompleted, domain.TaskFailed, domain.TaskBlocked:
			if _, ok := seen[status]; ok {
				continue
			}
			seen[status] = struct{}{}
			statuses = append(statuses, status)
		case "":
			continue
		default:
			return nil, fmt.Errorf("unsupported task status filter: %s", part)
		}
	}
	return statuses, nil
}

func normalizeSessionExportFilter(value string) (string, error) {
	filter := strings.ToLower(strings.TrimSpace(value))
	if filter == "" {
		filter = "full"
	}
	switch filter {
	case "full", "transcript", "usage", "redacted":
		return filter, nil
	default:
		return "", fmt.Errorf("unsupported export filter: %s", value)
	}
}

func encodeSessionExport(payload session.SessionExportProjection, format string) ([]byte, error) {
	switch format {
	case "json":
		return json.MarshalIndent(payload, "", "  ")
	case "markdown":
		return []byte(renderSessionMarkdown(payload)), nil
	default:
		return nil, fmt.Errorf("unsupported export format: %s", format)
	}
}

func renderSessionMarkdown(payload session.SessionExportProjection) string {
	var builder strings.Builder
	builder.WriteString("# Session ")
	builder.WriteString(payload.SessionID)
	builder.WriteString("\n\n")
	builder.WriteString("- Filter: ")
	builder.WriteString(payload.Filter)
	builder.WriteString("\n")
	builder.WriteString("- Workspace: ")
	builder.WriteString(payload.WorkspaceRoot)
	builder.WriteString("\n")
	builder.WriteString("- Created: ")
	builder.WriteString(payload.CreatedAt.Format(time.RFC3339))
	builder.WriteString("\n")
	builder.WriteString("- Updated: ")
	builder.WriteString(payload.UpdatedAt.Format(time.RFC3339))
	builder.WriteString("\n")
	if payload.Usage != nil {
		builder.WriteString("- Usage: input=")
		builder.WriteString(fmt.Sprintf("%d", payload.Usage.InputTokens))
		builder.WriteString(" output=")
		builder.WriteString(fmt.Sprintf("%d", payload.Usage.OutputTokens))
		builder.WriteString(" responses=")
		builder.WriteString(fmt.Sprintf("%d", payload.Usage.ResponseCount))
		builder.WriteString(" estimated_cost=$")
		builder.WriteString(fmt.Sprintf("%.4f", payload.Usage.TotalCost))
		builder.WriteString("\n")
	}
	if len(payload.Tasks) > 0 {
		builder.WriteString("- Tasks: ")
		builder.WriteString(fmt.Sprintf("%d", len(payload.Tasks)))
		builder.WriteString("\n")
	}
	if len(payload.Transcript) > 0 {
		builder.WriteString("- Transcript entries: ")
		builder.WriteString(fmt.Sprintf("%d", len(payload.Transcript)))
		builder.WriteString("\n")
	}
	builder.WriteString("\n")
	if payload.Usage != nil {
		builder.WriteString("## Usage\n\n")
		builder.WriteString("- Last provider: ")
		builder.WriteString(payload.Usage.LastProvider)
		builder.WriteString("\n")
		builder.WriteString("- Last model: ")
		builder.WriteString(payload.Usage.LastModel)
		builder.WriteString("\n\n")
	}
	if len(payload.Transcript) > 0 {
		builder.WriteString("## Transcript\n\n")
	}
	for _, entry := range payload.Transcript {
		builder.WriteString("### ")
		builder.WriteString(entry.CreatedAt.Format("15:04:05"))
		builder.WriteString(" ")
		builder.WriteString(string(entry.Kind))
		builder.WriteString("\n\n")
		builder.WriteString("```")
		builder.WriteString("\n")
		builder.WriteString(entry.Message)
		if !strings.HasSuffix(entry.Message, "\n") {
			builder.WriteString("\n")
		}
		builder.WriteString("```")
		builder.WriteString("\n\n")
	}
	return builder.String()
}
