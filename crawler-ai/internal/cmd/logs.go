package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"crawler-ai/internal/logging"

	"github.com/spf13/cobra"
)

var (
	logsFollow bool
	logsTail   int
	logsLevel  string
)

var logsCmd = &cobra.Command{
	Use:     "logs",
	Aliases: []string{"log"},
	Short:   "View structured logs",
	Long:    "Tail structured JSON logs from the crawler-ai log file.",
	RunE:    runLogs,
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Follow log output (like tail -f)")
	logsCmd.Flags().IntVarP(&logsTail, "tail", "n", 50, "Number of lines to show")
	logsCmd.Flags().StringVar(&logsLevel, "level", "", "Filter by log level (debug, info, warn, error)")
	rootCmd.AddCommand(logsCmd)
}

func runLogs(cmd *cobra.Command, args []string) error {
	logPath := logging.DefaultLogPath()
	writer := cmd.OutOrStdout()
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		_, _ = fmt.Fprintf(writer, "No log file found at %s\n", logPath)
		_, _ = fmt.Fprintln(writer, "Run crawler-ai with --debug to enable file logging.")
		return nil
	}

	if logsFollow {
		return followLogs(logPath, writer)
	}

	return tailLogs(logPath, writer)
}

func tailLogs(path string, writer io.Writer) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if logsLevel != "" && !matchesLevel(line, logsLevel) {
			continue
		}
		lines = append(lines, line)
	}

	// Show last N lines
	start := 0
	if len(lines) > logsTail {
		start = len(lines) - logsTail
	}

	for _, line := range lines[start:] {
		_, _ = fmt.Fprintln(writer, formatLogLine(line))
	}

	return scanner.Err()
}

func followLogs(path string, writer io.Writer) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer file.Close()

	// Seek to end
	if _, err := file.Seek(0, 2); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(writer, "Following %s (Ctrl+C to stop)\n", path)
	scanner := bufio.NewScanner(file)

	for {
		for scanner.Scan() {
			line := scanner.Text()
			if logsLevel != "" && !matchesLevel(line, logsLevel) {
				continue
			}
			_, _ = fmt.Fprintln(writer, formatLogLine(line))
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func matchesLevel(line, level string) bool {
	// Try JSON parsing
	var entry map[string]any
	if err := json.Unmarshal([]byte(line), &entry); err == nil {
		if lvl, ok := entry["level"].(string); ok {
			return strings.EqualFold(lvl, level)
		}
	}

	// Fallback: text search
	return strings.Contains(strings.ToUpper(line), strings.ToUpper(level))
}

func formatLogLine(line string) string {
	var entry map[string]any
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return line // Not JSON, return as-is
	}

	ts := ""
	if t, ok := entry["time"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339Nano, t); err == nil {
			ts = parsed.Format("15:04:05.000")
		}
	}

	level := ""
	if l, ok := entry["level"].(string); ok {
		level = strings.ToUpper(l)
	}

	msg := ""
	if m, ok := entry["msg"].(string); ok {
		msg = m
	}

	// Collect remaining fields
	var extra []string
	for k, v := range entry {
		if k == "time" || k == "level" || k == "msg" || k == "source" {
			continue
		}
		extra = append(extra, fmt.Sprintf("%s=%v", k, v))
	}

	result := fmt.Sprintf("%s %-5s %s", ts, level, msg)
	if len(extra) > 0 {
		result += "  " + strings.Join(extra, " ")
	}

	return result
}
