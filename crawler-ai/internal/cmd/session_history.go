package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"crawler-ai/internal/domain"
	"crawler-ai/internal/session"

	"github.com/spf13/cobra"
)

var (
	sessionsHistoryFormat  string
	sessionsHistorySection string
	sessionsHistoryKind    string
)

var sessionsHistoryCmd = &cobra.Command{
	Use:   "history [session-id]",
	Short: "Inspect prompt and file history",
	Long:  "Inspect prompt-history and tracked file-history projections for one stored session.",
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
		section, err := normalizeSessionHistorySection(sessionsHistorySection)
		if err != nil {
			return err
		}
		kindFilter, includeUnknown, err := normalizeSessionHistoryKinds(sessionsHistoryKind)
		if err != nil {
			return err
		}
		output, err := buildSessionHistoryOutput(args[0], summary, services.PromptHistory, services.FileHistory, section, session.FileHistoryOptions{Kinds: kindFilter, IncludeUnknown: includeUnknown})
		if err != nil {
			return err
		}
		if strings.EqualFold(strings.TrimSpace(sessionsHistoryFormat), "json") {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(output)
		}
		return writeSessionHistoryText(cmd, output)
	},
}

type sessionHistoryOutput struct {
	SessionID     string                      `json:"session_id"`
	Summary       session.SessionSummary      `json:"summary"`
	PromptHistory []domain.TranscriptEntry    `json:"prompt_history,omitempty"`
	FileHistory   *session.SessionFileHistory `json:"file_history,omitempty"`
}

func init() {
	sessionsCmd.AddCommand(sessionsHistoryCmd)
	sessionsHistoryCmd.Flags().StringVar(&sessionsHistoryFormat, "format", "text", "Output format: text or json")
	sessionsHistoryCmd.Flags().StringVar(&sessionsHistorySection, "section", "all", "Projection to print: all, prompt, or files")
	sessionsHistoryCmd.Flags().StringVar(&sessionsHistoryKind, "kind", "", "Optional comma-separated file kinds for file history: workspace, fetched, history, unknown")
}

func normalizeSessionHistorySection(value string) (string, error) {
	section := strings.ToLower(strings.TrimSpace(value))
	if section == "" {
		section = "all"
	}
	switch section {
	case "all", "prompt", "files":
		return section, nil
	default:
		return "", fmt.Errorf("unsupported history section: %s", value)
	}
}

func normalizeSessionHistoryKinds(value string) ([]session.FileRecordKind, bool, error) {
	if strings.TrimSpace(value) == "" {
		return nil, false, nil
	}
	parts := strings.Split(value, ",")
	kinds := make([]session.FileRecordKind, 0, len(parts))
	seen := make(map[session.FileRecordKind]struct{}, len(parts))
	includeUnknown := false
	for _, part := range parts {
		normalized := strings.ToLower(strings.TrimSpace(part))
		switch normalized {
		case "workspace":
			if _, ok := seen[session.FileRecordWorkspace]; ok {
				continue
			}
			seen[session.FileRecordWorkspace] = struct{}{}
			kinds = append(kinds, session.FileRecordWorkspace)
		case "fetched":
			if _, ok := seen[session.FileRecordFetched]; ok {
				continue
			}
			seen[session.FileRecordFetched] = struct{}{}
			kinds = append(kinds, session.FileRecordFetched)
		case "history":
			if _, ok := seen[session.FileRecordHistory]; ok {
				continue
			}
			seen[session.FileRecordHistory] = struct{}{}
			kinds = append(kinds, session.FileRecordHistory)
		case "unknown":
			includeUnknown = true
		case "":
			continue
		default:
			return nil, false, fmt.Errorf("unsupported history kind filter: %s", part)
		}
	}
	return kinds, includeUnknown, nil
}

func buildSessionHistoryOutput(sessionID string, summary session.SessionSummary, prompts *session.PromptHistoryService, files *session.FileHistoryService, section string, fileOptions session.FileHistoryOptions) (sessionHistoryOutput, error) {
	output := sessionHistoryOutput{SessionID: sessionID, Summary: summary}
	switch section {
	case "prompt":
		prompt, err := prompts.PromptHistory(sessionID)
		if err != nil {
			return sessionHistoryOutput{}, err
		}
		output.PromptHistory = prompt
	case "files":
		fileHistory, err := files.FilteredFileHistory(sessionID, fileOptions)
		if err != nil {
			return sessionHistoryOutput{}, err
		}
		output.FileHistory = &fileHistory
	default:
		prompt, err := prompts.PromptHistory(sessionID)
		if err != nil {
			return sessionHistoryOutput{}, err
		}
		fileHistory, err := files.FilteredFileHistory(sessionID, fileOptions)
		if err != nil {
			return sessionHistoryOutput{}, err
		}
		output.PromptHistory = prompt
		output.FileHistory = &fileHistory
	}
	return output, nil
}

func writeSessionHistoryText(cmd *cobra.Command, output sessionHistoryOutput) error {
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Session history: %s\n", output.SessionID)
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Workspace: %s\n", output.Summary.WorkspaceRoot)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Summary: transcript=%d tasks=%d files=%d responses=%d\n", output.Summary.MessageCount, output.Summary.TaskCount, output.Summary.FileCount, output.Summary.Usage.ResponseCount)

	if len(output.PromptHistory) > 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Prompt history")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
		for _, entry := range output.PromptHistory {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s: %s\n", entry.CreatedAt.Format("15:04:05"), entry.Kind, truncate(entry.Message, 120))
		}
	}

	if output.FileHistory != nil {
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "File history")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Totals: files=%d workspace=%d fetched=%d snapshots=%d unknown=%d\n",
			len(output.FileHistory.Files),
			len(output.FileHistory.WorkspaceFiles),
			len(output.FileHistory.FetchedFiles),
			len(output.FileHistory.HistorySnapshots),
			len(output.FileHistory.FilesMissingKinds),
		)
		for _, item := range output.FileHistory.WorkspaceFiles {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "[workspace] %s tool=%s\n", item.Path, item.Tool)
		}
		for _, item := range output.FileHistory.FetchedFiles {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "[fetched] %s tool=%s\n", item.Path, item.Tool)
		}
		for _, item := range output.FileHistory.HistorySnapshots {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "[snapshot] %s source=%s\n", item.Path, item.Metadata["source_path"])
		}
		for _, item := range output.FileHistory.FilesMissingKinds {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "[unknown] %s tool=%s\n", item.Path, item.Tool)
		}
	}

	if len(output.PromptHistory) == 0 && output.FileHistory == nil {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No history found.")
	}
	return nil
}
