package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"crawler-ai/internal/session"

	"github.com/spf13/cobra"
)

var (
	sessionsContextFormat  string
	sessionsContextSection string
)

var sessionsContextCmd = &cobra.Command{
	Use:   "context [session-id]",
	Short: "Inspect persisted coding context",
	Long:  "Inspect the persisted coding-context projection, including tracked files, diagnostics, and context notes for one stored session.",
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
		section, err := normalizeSessionContextSection(sessionsContextSection)
		if err != nil {
			return err
		}
		snapshot, err := services.Queries.CodingContextService().Snapshot(args[0])
		if err != nil {
			return err
		}
		output := filterSessionContextOutput(summary, snapshot, section)
		if strings.EqualFold(strings.TrimSpace(sessionsContextFormat), "json") {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(output)
		}
		return writeSessionContextText(cmd, output)
	},
}

type sessionContextOutput struct {
	SessionID     string                            `json:"session_id"`
	Summary       session.SessionSummary            `json:"summary"`
	GeneratedAt   string                            `json:"generated_at"`
	WorkspaceRoot string                            `json:"workspace_root,omitempty"`
	Files         []session.CodingContextFile       `json:"files,omitempty"`
	Diagnostics   []session.CodingContextDiagnostic `json:"diagnostics,omitempty"`
	Notes         []string                          `json:"notes,omitempty"`
}

func init() {
	sessionsCmd.AddCommand(sessionsContextCmd)
	sessionsContextCmd.Flags().StringVar(&sessionsContextFormat, "format", "text", "Output format: text or json")
	sessionsContextCmd.Flags().StringVar(&sessionsContextSection, "section", "all", "Projection to print: all, files, diagnostics, or notes")
}

func normalizeSessionContextSection(value string) (string, error) {
	section := strings.ToLower(strings.TrimSpace(value))
	if section == "" {
		section = "all"
	}
	switch section {
	case "all", "files", "diagnostics", "notes":
		return section, nil
	default:
		return "", fmt.Errorf("unsupported context section: %s", value)
	}
}

func filterSessionContextOutput(summary session.SessionSummary, snapshot session.CodingContextSnapshot, section string) sessionContextOutput {
	output := sessionContextOutput{
		SessionID:     snapshot.SessionID,
		Summary:       summary,
		GeneratedAt:   snapshot.GeneratedAt.Format("2006-01-02T15:04:05Z07:00"),
		WorkspaceRoot: snapshot.WorkspaceRoot,
	}
	switch section {
	case "files":
		output.Files = append([]session.CodingContextFile(nil), snapshot.Files...)
	case "diagnostics":
		output.Diagnostics = append([]session.CodingContextDiagnostic(nil), snapshot.Diagnostics...)
	case "notes":
		output.Notes = append([]string(nil), snapshot.Notes...)
	default:
		output.Files = append([]session.CodingContextFile(nil), snapshot.Files...)
		output.Diagnostics = append([]session.CodingContextDiagnostic(nil), snapshot.Diagnostics...)
		output.Notes = append([]string(nil), snapshot.Notes...)
	}
	return output
}

func writeSessionContextText(cmd *cobra.Command, output sessionContextOutput) error {
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Session context: %s\n", output.SessionID)
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Workspace: %s\n", output.WorkspaceRoot)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Generated: %s\n", output.GeneratedAt)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Summary: files=%d diagnostics=%d notes=%d\n", len(output.Files), len(output.Diagnostics), len(output.Notes))

	if len(output.Files) > 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Tracked context files")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
		for _, file := range output.Files {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s kind=%s tool=%s\n", file.Path, file.Kind, file.Tool)
			if strings.TrimSpace(file.SnapshotPath) != "" {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  snapshot=%s\n", file.SnapshotPath)
			}
		}
	}

	if len(output.Diagnostics) > 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Diagnostics")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
		for _, diagnostic := range output.Diagnostics {
			location := diagnostic.Path
			if diagnostic.Line > 0 {
				location = fmt.Sprintf("%s:%d", location, diagnostic.Line)
				if diagnostic.Column > 0 {
					location = fmt.Sprintf("%s:%d", location, diagnostic.Column)
				}
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s %s\n", diagnostic.Severity, strings.TrimSpace(location), diagnostic.Message)
		}
	}

	if len(output.Notes) > 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Notes")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
		for _, note := range output.Notes {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", note)
		}
	}

	if len(output.Files) == 0 && len(output.Diagnostics) == 0 && len(output.Notes) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No coding context found.")
	}
	return nil
}
