package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"crawler-ai/internal/config"
	"crawler-ai/internal/oauth"

	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:     "config",
	Aliases: []string{"cfg"},
	Short:   "View current configuration",
	Long:    "Display the merged configuration from all sources (user config, project config, environment).",
	RunE:    runConfig,
}

func init() {
	rootCmd.AddCommand(configCmd)
}

func runConfig(cmd *cobra.Command, args []string) error {
	cfg, err := loadCommandConfig()
	if err != nil {
		return err
	}
	writer := cmd.OutOrStdout()

	store := oauth.DefaultKeyStore()
	_ = store.Load()

	_, _ = fmt.Fprintln(writer, "crawler-ai configuration")
	_, _ = fmt.Fprintln(writer, strings.Repeat("─", 50))
	_, _ = fmt.Fprintf(writer, "Environment:    %s\n", cfg.Env)
	_, _ = fmt.Fprintf(writer, "Log Level:      %s\n", cfg.LogLevel)
	_, _ = fmt.Fprintf(writer, "Workspace Root: %s\n", cfg.WorkspaceRoot)
	_, _ = fmt.Fprintf(writer, "Yolo Mode:      %v\n", cfg.Yolo)
	if override := oauth.ConfigDirOverride(); override != "" {
		_, _ = fmt.Fprintf(writer, "Data Dir:       %s\n", override)
	}
	_, _ = fmt.Fprintln(writer)

	_, _ = fmt.Fprintln(writer, "Models:")
	printConfigProvider(writer, "  Orchestrator", cfg.Models.Orchestrator)
	printConfigProvider(writer, "  Worker", cfg.Models.Worker)
	_, _ = fmt.Fprintln(writer)

	if len(cfg.Permissions.AllowedTools) > 0 || len(cfg.Permissions.DisabledTools) > 0 {
		_, _ = fmt.Fprintln(writer, "Permissions:")
		if len(cfg.Permissions.AllowedTools) > 0 {
			_, _ = fmt.Fprintf(writer, "  Allowed: %s\n", strings.Join(cfg.Permissions.AllowedTools, ", "))
		}
		if len(cfg.Permissions.DisabledTools) > 0 {
			_, _ = fmt.Fprintf(writer, "  Disabled: %s\n", strings.Join(cfg.Permissions.DisabledTools, ", "))
		}
		_, _ = fmt.Fprintln(writer)
	}

	_, _ = fmt.Fprintln(writer, "Stored API Keys:")
	for _, p := range store.List() {
		_, _ = fmt.Fprintf(writer, "  %s: ✓\n", p)
	}
	if len(store.List()) == 0 {
		_, _ = fmt.Fprintln(writer, "  (none — run `crawler-ai login` to add keys)")
	}
	_, _ = fmt.Fprintln(writer)

	_, _ = fmt.Fprintln(writer, "Config File Locations (priority order):")
	_, _ = fmt.Fprintln(writer, "  1. Environment variables (CRAWLER_AI_*)")
	if override := oauth.ConfigDirOverride(); override != "" {
		_, _ = fmt.Fprintf(writer, "     data directory override: %s (flag or %s)\n", override, oauth.DataDirEnvVar)
	}
	_, _ = fmt.Fprintln(writer, "  2. .crawler-ai.json (project)")
	_, _ = fmt.Fprintln(writer, "  3. crawler-ai.json (project)")
	_, _ = fmt.Fprintf(writer, "  4. %s/config.json (user)\n", oauth.DefaultConfigDir())
	_, _ = fmt.Fprintf(writer, "  Keys: %s\n", store.Path())

	return nil
}

func printConfigProvider(writer io.Writer, label string, cfg config.ProviderConfig) {
	_, _ = fmt.Fprintf(writer, "%s:\n", label)
	_, _ = fmt.Fprintf(writer, "    provider: %s\n", cfg.Provider)
	_, _ = fmt.Fprintf(writer, "    model:    %s\n", cfg.Model)
	if cfg.BaseURL != "" {
		_, _ = fmt.Fprintf(writer, "    base_url: %s\n", cfg.BaseURL)
	}
	keyDisplay := "(not set)"
	if cfg.APIKey != "" {
		keyDisplay = maskKey(cfg.APIKey)
	}
	_, _ = fmt.Fprintf(writer, "    api_key:  %s\n", keyDisplay)
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "…" + key[len(key)-4:]
}

// ConfigJSON outputs the config as JSON (useful for piping).
var configJSONCmd = &cobra.Command{
	Use:     "json",
	Aliases: []string{"j"},
	Short:   "Output configuration as JSON",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadCommandConfig()
		if err != nil {
			return err
		}
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	},
}

func init() {
	configCmd.AddCommand(configJSONCmd)
}
