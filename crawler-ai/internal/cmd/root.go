package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"crawler-ai/internal/oauth"

	"github.com/spf13/cobra"
)

var (
	debug bool
	yolo  bool
	cwd   string
	dataDir string
)

var rootCmd = &cobra.Command{
	Use:   "crwlr",
	Aliases: []string{"crawler-ai"},
	Short: "CLI coding assistant",
	Long:  "crwlr is a CLI-first coding assistant for running prompts, managing sessions, and configuring models/providers.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		oauth.SetDefaultConfigDir(strings.TrimSpace(dataDir))
		if strings.TrimSpace(dataDir) != "" {
			dataDir = filepath.Clean(strings.TrimSpace(dataDir))
			oauth.SetDefaultConfigDir(dataDir)
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

type commandExitError struct {
	cause          error
	exitCode       int
	suppressStderr bool
}

func (e *commandExitError) Error() string {
	if e == nil || e.cause == nil {
		return "command failed"
	}
	return e.cause.Error()
}

func (e *commandExitError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&debug, "debug", "d", false, "Enable debug logging")
	rootCmd.PersistentFlags().BoolVar(&yolo, "yolo", false, "Skip all tool approval prompts")
	rootCmd.PersistentFlags().StringVarP(&cwd, "cwd", "c", "", "Set working directory")
	rootCmd.PersistentFlags().StringVar(&dataDir, "data-dir", "", "Override the crawler-ai app data directory for config, keys, sessions, provider catalog, and logs")
}

// Execute runs the root command. Called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		if !suppressCommandErrorOutput(err) {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		os.Exit(commandExitCode(err))
	}
}

func commandExitCode(err error) int {
	var exitErr *commandExitError
	if errors.As(err, &exitErr) && exitErr.exitCode > 0 {
		return exitErr.exitCode
	}
	return 1
}

func suppressCommandErrorOutput(err error) bool {
	var exitErr *commandExitError
	return errors.As(err, &exitErr) && exitErr.suppressStderr
}

// IsDebug returns whether debug mode is enabled.
func IsDebug() bool { return debug }

// IsYolo returns whether yolo (skip approvals) mode is enabled.
func IsYolo() bool { return yolo }

// WorkingDir returns the configured working directory, or empty for default.
func WorkingDir() string { return cwd }

// DataDir returns the configured app data directory override, or empty for default behavior.
func DataDir() string { return dataDir }
