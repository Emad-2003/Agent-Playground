package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"crawler-ai/internal/domain"

	"github.com/spf13/cobra"
)

var sessionsTranscriptFormat string

var sessionsTranscriptCmd = &cobra.Command{
	Use:   "transcript [session-id]",
	Short: "Print the full session transcript",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		services, err := loadStoredSessionServices()
		if err != nil {
			return err
		}
		messages, err := services.Queries.Messages(args[0])
		if err != nil {
			return fmt.Errorf("session not found: %s", args[0])
		}
		if strings.EqualFold(strings.TrimSpace(sessionsTranscriptFormat), "json") {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(messages)
		}
		return writeSessionTranscriptText(cmd, args[0], messages)
	},
}

func init() {
	sessionsCmd.AddCommand(sessionsTranscriptCmd)
	sessionsTranscriptCmd.Flags().StringVar(&sessionsTranscriptFormat, "format", "text", "Output format: text or json")
}

func writeSessionTranscriptText(cmd *cobra.Command, sessionID string, messages []domain.TranscriptEntry) error {
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Session transcript: %s\n", sessionID)
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
	if len(messages) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No transcript entries found.")
		return nil
	}
	for _, entry := range messages {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s", entry.CreatedAt.Format("2006-01-02 15:04:05"), entry.Kind)
		if toolName := strings.TrimSpace(entry.Metadata["tool"]); toolName != "" {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), " tool=%s", toolName)
		}
		if status := strings.TrimSpace(entry.Metadata["status"]); status != "" {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), " status=%s", status)
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), entry.Message)
		if !strings.HasSuffix(entry.Message, "\n") {
			_, _ = fmt.Fprintln(cmd.OutOrStdout())
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
	}
	return nil
}
