package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"crawler-ai/internal/app"

	"github.com/spf13/cobra"
)

var (
	runSessionID  string
	runContinue   bool
	runFormat     string
	runAutonomous bool
)

var runCmd = &cobra.Command{
	Use:   "run [prompt...]",
	Short: "Run a single non-interactive prompt",
	Long:  "Run a single prompt without launching the interactive TUI. The prompt can be supplied as arguments, stdin, or both.",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := normalizeRunFormat(runFormat)
		if err != nil {
			return err
		}

		prompt, err := readRunPrompt(cmd.InOrStdin(), args)
		if err != nil {
			if format == "json" {
				return writeStructuredRunFailure(cmd.OutOrStdout(), app.NonInteractiveRunResult{}, err)
			}
			return err
		}

		cfg, err := resolveConfig()
		if err != nil {
			if format == "json" {
				return writeStructuredRunFailure(cmd.OutOrStdout(), app.NonInteractiveRunResult{}, err)
			}
			return err
		}
		if runAutonomous {
			cfg.Yolo = true
		}

		application, err := app.New(cfg)
		if err != nil {
			startupErr := fmt.Errorf("startup error: %w", err)
			if format == "json" {
				return writeStructuredRunFailure(cmd.OutOrStdout(), app.NonInteractiveRunResult{}, startupErr)
			}
			return startupErr
		}
		defer application.Close()

		writer := cmd.OutOrStdout()
		var progressRenderer *runProgressRenderer
		if format == "json" {
			writer = io.Discard
		} else {
			progressRenderer = newRunProgressRenderer(cmd.ErrOrStderr())
		}

		result, err := application.RunNonInteractive(cmd.Context(), writer, prompt, app.NonInteractiveRunOptions{
			SessionID:    runSessionID,
			ContinueLast: runContinue,
			Progress: func(event app.NonInteractiveProgressEvent) {
				if progressRenderer != nil {
					progressRenderer.Handle(event)
				}
			},
		})
		if err != nil {
			if format == "json" {
				return writeStructuredRunFailure(cmd.OutOrStdout(), result, err)
			}
			return err
		}
		if format == "json" {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return nil
	},
}

func init() {
	runCmd.Flags().StringVarP(&runSessionID, "session", "s", "", "Continue a previous session by ID")
	runCmd.Flags().BoolVarP(&runContinue, "continue", "C", false, "Continue the most recent session for the current workspace")
	runCmd.Flags().StringVar(&runFormat, "format", "text", "Output format: text or json")
	runCmd.Flags().BoolVarP(&runAutonomous, "autonomous", "a", false, "Run with autonomous approval bypass for tool actions")
	runCmd.MarkFlagsMutuallyExclusive("session", "continue")
	rootCmd.AddCommand(runCmd)
}

func normalizeRunFormat(value string) (string, error) {
	format := strings.ToLower(strings.TrimSpace(value))
	if format == "" {
		format = "text"
	}
	switch format {
	case "text", "json":
		return format, nil
	default:
		return "", fmt.Errorf("unsupported run format: %s", value)
	}
}

func writeStructuredRunFailure(w io.Writer, result app.NonInteractiveRunResult, err error) error {
	result = ensureStructuredRunFailure(result, err)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if encodeErr := encoder.Encode(result); encodeErr != nil {
		return encodeErr
	}
	return &commandExitError{cause: err, exitCode: result.ExitCode, suppressStderr: true}
}

func ensureStructuredRunFailure(result app.NonInteractiveRunResult, err error) app.NonInteractiveRunResult {
	if err == nil {
		return result
	}
	if result.Status == "" {
		result = app.NonInteractiveRunResult{
			Status:   app.NonInteractiveRunStatusError,
			ExitCode: app.NonInteractiveRunExitError,
			Failure: &app.NonInteractiveRunFailure{
				Type:    string(app.NonInteractiveRunStatusError),
				Message: err.Error(),
			},
		}
	}
	if result.ExitCode == 0 {
		result.ExitCode = app.NonInteractiveRunExitError
	}
	if result.Failure == nil {
		result.Failure = &app.NonInteractiveRunFailure{
			Type:    string(result.Status),
			Message: err.Error(),
		}
	}
	var exitErr *commandExitError
	if errors.As(err, &exitErr) && exitErr.exitCode > 0 {
		result.ExitCode = exitErr.exitCode
	}
	if result.Failure.Message == "" {
		result.Failure.Message = err.Error()
	}
	return result
}

func readRunPrompt(input io.Reader, args []string) (string, error) {
	argPrompt := strings.TrimSpace(strings.Join(args, " "))
	stdinPrompt := ""
	if shouldReadRunStdin(input) {
		data, err := io.ReadAll(input)
		if err != nil {
			return "", err
		}
		stdinPrompt = strings.TrimSpace(string(data))
	}

	switch {
	case argPrompt != "" && stdinPrompt != "":
		return argPrompt + "\n\n" + stdinPrompt, nil
	case argPrompt != "":
		return argPrompt, nil
	case stdinPrompt != "":
		return stdinPrompt, nil
	default:
		return "", fmt.Errorf("no prompt provided")
	}
}

func shouldReadRunStdin(input io.Reader) bool {
	file, ok := input.(*os.File)
	if !ok {
		return true
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice == 0
}
