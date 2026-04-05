package cmd

import (
	"context"
	"fmt"
	"os"

	"crawler-ai/internal/app"
	"crawler-ai/internal/config"

	"github.com/spf13/cobra"
)

var interactiveCmd = &cobra.Command{
	Use:    "interactive",
	Short:  "Launch the interactive terminal UI",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInteractive()
	},
}

func init() {
	rootCmd.AddCommand(interactiveCmd)
}

func runInteractive() error {
	cfg, err := resolveConfig()
	if err != nil {
		return err
	}

	application, err := app.New(cfg)
	if err != nil {
		return fmt.Errorf("startup error: %w", err)
	}

	return application.Run(context.Background())
}

func resolveConfig() (config.Config, error) {
	// If --cwd was passed, override workspace root
	if cwd != "" {
		if err := os.Chdir(cwd); err != nil {
			return config.Config{}, fmt.Errorf("cannot change to directory %s: %w", cwd, err)
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return config.Config{}, err
	}

	// Apply CLI flag overrides
	if debug {
		cfg.LogLevel = "debug"
	}
	if yolo {
		cfg.Yolo = true
	}

	return cfg, nil
}
