package cmd

import (
	"fmt"
	"io"
	"strings"

	"crawler-ai/internal/config"
	"crawler-ai/internal/oauth"
	"crawler-ai/internal/providercatalog"

	"github.com/spf13/cobra"
)

var modelsCmd = &cobra.Command{
	Use:   "models",
	Aliases: []string{"model"},
	Short: "List available models per provider",
	Long:  "Shows configured models for each provider role (orchestrator, worker) and known model options.",
	RunE:  runModels,
}

var (
	modelsSetRole     string
	modelsSetProvider string
	modelsSetModel    string
	modelsSetBaseURL  string
	modelsSetScope    string
)

var modelsRecentCmd = &cobra.Command{
	Use:   "recent",
	Short: "Show recent model selections",
	Long:  "Show recently persisted orchestrator and worker model selections.",
	RunE:  runModelsRecent,
}

var modelsSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Persist a provider/model selection",
	Long:  "Persist a provider/model selection for the orchestrator or worker role in user or workspace config.",
	RunE:  runModelsSet,
}

func init() {
	rootCmd.AddCommand(modelsCmd)
	modelsCmd.AddCommand(modelsSetCmd)
	modelsCmd.AddCommand(modelsRecentCmd)
	modelsSetCmd.Flags().StringVar(&modelsSetRole, "role", "", "Role to update: orchestrator or worker")
	modelsSetCmd.Flags().StringVar(&modelsSetProvider, "provider", "", "Provider ID to persist")
	modelsSetCmd.Flags().StringVar(&modelsSetModel, "model", "", "Model ID to persist")
	modelsSetCmd.Flags().StringVar(&modelsSetBaseURL, "base-url", "", "Base URL override to persist")
	modelsSetCmd.Flags().StringVar(&modelsSetScope, "scope", string(config.ScopeUser), "Config scope: user or workspace")
	_ = modelsSetCmd.MarkFlagRequired("role")
}

func runModels(cmd *cobra.Command, args []string) error {
	cfg, err := loadCommandConfig()
	if err != nil {
		return err
	}
	writer := cmd.OutOrStdout()

	store := oauth.DefaultKeyStore()
	_ = store.Load()

	_, _ = fmt.Fprintln(writer, "Configured Models")
	_, _ = fmt.Fprintln(writer, strings.Repeat("─", 50))

	printProviderRole(writer, "Orchestrator", cfg.Models.Orchestrator, store)
	_, _ = fmt.Fprintln(writer)
	printProviderRole(writer, "Worker", cfg.Models.Worker, store)

	_, _ = fmt.Fprintln(writer)
	_, _ = fmt.Fprintln(writer, strings.Repeat("─", 50))
	_, _ = fmt.Fprintln(writer, "Known Providers & Models")
	_, _ = fmt.Fprintln(writer)

	for _, definition := range providercatalog.List() {
		keyStatus := "✗ no key"
		if oauth.ResolveProviderKey(store, definition.ID, "") != "" {
			keyStatus = "✓ key set"
		}
		_, _ = fmt.Fprintf(writer, "  %s [%s] (%s)\n", definition.DisplayName, definition.ID, keyStatus)
		for _, model := range definition.Models {
			line := "    • " + model.ID
			if model.Pricing.InputPerMillionTokensUSD > 0 || model.Pricing.OutputPerMillionTokensUSD > 0 {
				line += fmt.Sprintf(" [$%.2f in / $%.2f out per 1M]", model.Pricing.InputPerMillionTokensUSD, model.Pricing.OutputPerMillionTokensUSD)
			} else {
				line += " [pricing not configured]"
			}
			_, _ = fmt.Fprintln(writer, line)
		}
		_, _ = fmt.Fprintln(writer)
	}

	return nil
}

func runModelsSet(cmd *cobra.Command, args []string) error {
	if strings.TrimSpace(modelsSetProvider) == "" && strings.TrimSpace(modelsSetModel) == "" && strings.TrimSpace(modelsSetBaseURL) == "" {
		return fmt.Errorf("at least one of --provider, --model, or --base-url must be set")
	}

	scope, err := config.ParseScope(modelsSetScope)
	if err != nil {
		return err
	}

	store, err := openConfigStore()
	if err != nil {
		return err
	}

	current := store.Config()
	var providerCfg config.ProviderConfig
	switch strings.ToLower(strings.TrimSpace(modelsSetRole)) {
	case "orchestrator":
		providerCfg = current.Models.Orchestrator
	case "worker":
		providerCfg = current.Models.Worker
	default:
		return fmt.Errorf("role must be orchestrator or worker")
	}

	if strings.TrimSpace(modelsSetProvider) != "" {
		providerCfg.Provider = strings.TrimSpace(modelsSetProvider)
	}
	if strings.TrimSpace(modelsSetModel) != "" {
		providerCfg.Model = strings.TrimSpace(modelsSetModel)
	}
	if cmd.Flags().Changed("base-url") {
		providerCfg.BaseURL = strings.TrimSpace(modelsSetBaseURL)
	}

	if err := store.UpdatePreferredModel(scope, modelsSetRole, providerCfg); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Updated %s in %s config\n", strings.ToLower(strings.TrimSpace(modelsSetRole)), scope)
	printConfigProvider(cmd.OutOrStdout(), "  Persisted", providerCfg)
	return nil
}

func runModelsRecent(cmd *cobra.Command, args []string) error {
	store, err := openConfigStore()
	if err != nil {
		return err
	}
	writer := cmd.OutOrStdout()

	roles := []string{"orchestrator", "worker"}
	for index, role := range roles {
		recent, err := store.RecentModelsForRole(role)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(writer, "%s recent models:\n", strings.Title(role))
		if len(recent) == 0 {
			_, _ = fmt.Fprintln(writer, "  (none)")
		} else {
			for _, item := range recent {
				line := fmt.Sprintf("  %s/%s", item.Provider, item.Model)
				if item.BaseURL != "" {
					line += " [" + item.BaseURL + "]"
				}
				_, _ = fmt.Fprintln(writer, line)
			}
		}
		if index < len(roles)-1 {
			_, _ = fmt.Fprintln(writer)
		}
	}
	return nil
}

func printProviderRole(writer io.Writer, role string, cfg config.ProviderConfig, store *oauth.KeyStore) {
	keyStatus := "✗"
	if oauth.ResolveProviderKey(store, cfg.Provider, cfg.APIKey) != "" {
		keyStatus = "✓"
	}
	_, _ = fmt.Fprintf(writer, "  %s:\n", role)
	_, _ = fmt.Fprintf(writer, "    Provider: %s [%s]\n", cfg.Provider, keyStatus)
	_, _ = fmt.Fprintf(writer, "    Model:    %s\n", cfg.Model)
	if cfg.BaseURL != "" {
		_, _ = fmt.Fprintf(writer, "    Base URL: %s\n", cfg.BaseURL)
	}
}
