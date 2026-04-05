package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"crawler-ai/internal/config"
	"crawler-ai/internal/oauth"

	"github.com/spf13/cobra"
)

var (
	providersRole         string
	providersAll          bool
	providerMutationRole  string
	providerMutationScope string
	providerHeaderKey     string
	providerBodyKeyPath   string
	providerOptionKeyPath string
	providerMutationValue string
)

var providersCmd = &cobra.Command{
	Use:     "providers",
	Aliases: []string{"provider", "prov"},
	Short:   "Provider operator commands",
	Long:    "Validate provider configuration and refresh persisted OAuth credentials.",
}

var providersValidateCmd = &cobra.Command{
	Use:     "validate",
	Aliases: []string{"check", "test"},
	Short:   "Validate provider connectivity",
	Long:    "Validate the configured provider connection for one role or for both roles.",
	RunE:    runProvidersValidate,
}

var providersRefreshCmd = &cobra.Command{
	Use:     "refresh",
	Aliases: []string{"reauth", "token"},
	Short:   "Refresh an OAuth-backed provider token",
	Long:    "Refresh the persisted OAuth token for a configured role when that provider supports OAuth refresh.",
	RunE:    runProvidersRefresh,
}

var providersHeadersCmd = &cobra.Command{
	Use:     "headers",
	Aliases: []string{"header"},
	Short:   "Manage persisted provider headers",
}

var providersHeadersSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set a persisted provider header",
	RunE:  runProvidersHeadersSet,
}

var providersHeadersUnsetCmd = &cobra.Command{
	Use:   "unset",
	Short: "Remove a persisted provider header",
	RunE:  runProvidersHeadersUnset,
}

var providersBodyCmd = &cobra.Command{
	Use:     "body",
	Aliases: []string{"payload"},
	Short:   "Manage persisted provider extra body fields",
}

var providersBodySetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set a persisted provider extra body field",
	RunE:  runProvidersBodySet,
}

var providersBodyUnsetCmd = &cobra.Command{
	Use:   "unset",
	Short: "Remove a persisted provider extra body field",
	RunE:  runProvidersBodyUnset,
}

var providersOptionsCmd = &cobra.Command{
	Use:     "options",
	Aliases: []string{"option", "opt"},
	Short:   "Manage persisted provider options",
}

var providersOptionsSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set a persisted provider option",
	RunE:  runProvidersOptionsSet,
}

var providersOptionsUnsetCmd = &cobra.Command{
	Use:   "unset",
	Short: "Remove a persisted provider option",
	RunE:  runProvidersOptionsUnset,
}

func init() {
	rootCmd.AddCommand(providersCmd)
	providersCmd.AddCommand(providersValidateCmd)
	providersCmd.AddCommand(providersRefreshCmd)
	providersCmd.AddCommand(providersHeadersCmd)
	providersCmd.AddCommand(providersBodyCmd)
	providersCmd.AddCommand(providersOptionsCmd)
	providersHeadersCmd.AddCommand(providersHeadersSetCmd)
	providersHeadersCmd.AddCommand(providersHeadersUnsetCmd)
	providersBodyCmd.AddCommand(providersBodySetCmd)
	providersBodyCmd.AddCommand(providersBodyUnsetCmd)
	providersOptionsCmd.AddCommand(providersOptionsSetCmd)
	providersOptionsCmd.AddCommand(providersOptionsUnsetCmd)

	providersValidateCmd.Flags().StringVar(&providersRole, "role", "", "Role to validate: orchestrator or worker")
	providersValidateCmd.Flags().BoolVar(&providersAll, "all", false, "Validate both orchestrator and worker roles")

	providersRefreshCmd.Flags().StringVar(&providersRole, "role", "", "Role to refresh: orchestrator or worker")
	_ = providersRefreshCmd.MarkFlagRequired("role")

	bindProviderMutationFlags(providersHeadersSetCmd)
	providersHeadersSetCmd.Flags().StringVar(&providerHeaderKey, "key", "", "Header key to persist")
	providersHeadersSetCmd.Flags().StringVar(&providerMutationValue, "value", "", "Header value to persist")
	_ = providersHeadersSetCmd.MarkFlagRequired("role")
	_ = providersHeadersSetCmd.MarkFlagRequired("key")
	_ = providersHeadersSetCmd.MarkFlagRequired("value")

	bindProviderMutationFlags(providersHeadersUnsetCmd)
	providersHeadersUnsetCmd.Flags().StringVar(&providerHeaderKey, "key", "", "Header key to remove")
	_ = providersHeadersUnsetCmd.MarkFlagRequired("role")
	_ = providersHeadersUnsetCmd.MarkFlagRequired("key")

	bindProviderMutationFlags(providersBodySetCmd)
	providersBodySetCmd.Flags().StringVar(&providerBodyKeyPath, "key", "", "Dot-delimited extra body key path")
	providersBodySetCmd.Flags().StringVar(&providerMutationValue, "value", "", "JSON value to persist; plain strings are accepted")
	_ = providersBodySetCmd.MarkFlagRequired("role")
	_ = providersBodySetCmd.MarkFlagRequired("key")
	_ = providersBodySetCmd.MarkFlagRequired("value")

	bindProviderMutationFlags(providersBodyUnsetCmd)
	providersBodyUnsetCmd.Flags().StringVar(&providerBodyKeyPath, "key", "", "Dot-delimited extra body key path to remove")
	_ = providersBodyUnsetCmd.MarkFlagRequired("role")
	_ = providersBodyUnsetCmd.MarkFlagRequired("key")

	bindProviderMutationFlags(providersOptionsSetCmd)
	providersOptionsSetCmd.Flags().StringVar(&providerOptionKeyPath, "key", "", "Dot-delimited provider option key path")
	providersOptionsSetCmd.Flags().StringVar(&providerMutationValue, "value", "", "JSON value to persist; plain strings are accepted")
	_ = providersOptionsSetCmd.MarkFlagRequired("role")
	_ = providersOptionsSetCmd.MarkFlagRequired("key")
	_ = providersOptionsSetCmd.MarkFlagRequired("value")

	bindProviderMutationFlags(providersOptionsUnsetCmd)
	providersOptionsUnsetCmd.Flags().StringVar(&providerOptionKeyPath, "key", "", "Dot-delimited provider option key path to remove")
	_ = providersOptionsUnsetCmd.MarkFlagRequired("role")
	_ = providersOptionsUnsetCmd.MarkFlagRequired("key")
}

func runProvidersValidate(cmd *cobra.Command, args []string) error {
	roles, err := resolveProviderRoles(providersRole, providersAll)
	if err != nil {
		return err
	}

	store, err := openConfigStore()
	if err != nil {
		return err
	}

	keyStore := oauth.DefaultKeyStore()
	_ = keyStore.Load()

	for _, role := range roles {
		providerCfg, err := store.ProviderConfigForRole(role)
		if err != nil {
			return err
		}
		if err := providerCfg.TestConnection(context.Background(), keyStore); err != nil {
			return fmt.Errorf("%s validation failed: %w", role, err)
		}
		cmd.Printf("%s: validated %s/%s\n", role, providerCfg.Provider, providerCfg.Model)
	}

	return nil
}

func runProvidersRefresh(cmd *cobra.Command, args []string) error {
	role, err := normalizeProviderRole(providersRole)
	if err != nil {
		return err
	}

	store, err := openConfigStore()
	if err != nil {
		return err
	}
	scope, err := store.ScopeForRole(role)
	if err != nil {
		return err
	}
	if err := store.RefreshProviderOAuthToken(context.Background(), scope, role); err != nil {
		return err
	}
	providerCfg, err := store.ProviderConfigForRole(role)
	if err != nil {
		return err
	}
	cmd.Printf("%s: refreshed OAuth token for %s/%s\n", role, providerCfg.Provider, providerCfg.Model)
	return nil
}

func runProvidersHeadersSet(cmd *cobra.Command, args []string) error {
	return runProviderScopedMutation(func(store *config.Store, scope config.Scope, role string) error {
		return store.SetProviderHeader(scope, role, strings.TrimSpace(providerHeaderKey), providerMutationValue)
	}, func(scope config.Scope, role string) {
		cmd.Printf("%s: set header %s in %s config\n", role, strings.TrimSpace(providerHeaderKey), scope)
	})
}

func runProvidersHeadersUnset(cmd *cobra.Command, args []string) error {
	return runProviderScopedMutation(func(store *config.Store, scope config.Scope, role string) error {
		return store.RemoveProviderHeader(scope, role, strings.TrimSpace(providerHeaderKey))
	}, func(scope config.Scope, role string) {
		cmd.Printf("%s: removed header %s from %s config\n", role, strings.TrimSpace(providerHeaderKey), scope)
	})
}

func runProvidersBodySet(cmd *cobra.Command, args []string) error {
	value, err := parseMutationValue(providerMutationValue)
	if err != nil {
		return err
	}
	return runProviderScopedMutation(func(store *config.Store, scope config.Scope, role string) error {
		return store.SetProviderBodyValue(scope, role, strings.TrimSpace(providerBodyKeyPath), value)
	}, func(scope config.Scope, role string) {
		cmd.Printf("%s: set extra body %s in %s config\n", role, strings.TrimSpace(providerBodyKeyPath), scope)
	})
}

func runProvidersBodyUnset(cmd *cobra.Command, args []string) error {
	return runProviderScopedMutation(func(store *config.Store, scope config.Scope, role string) error {
		return store.RemoveProviderBodyValue(scope, role, strings.TrimSpace(providerBodyKeyPath))
	}, func(scope config.Scope, role string) {
		cmd.Printf("%s: removed extra body %s from %s config\n", role, strings.TrimSpace(providerBodyKeyPath), scope)
	})
}

func runProvidersOptionsSet(cmd *cobra.Command, args []string) error {
	value, err := parseMutationValue(providerMutationValue)
	if err != nil {
		return err
	}
	return runProviderScopedMutation(func(store *config.Store, scope config.Scope, role string) error {
		return store.SetProviderOptionValue(scope, role, strings.TrimSpace(providerOptionKeyPath), value)
	}, func(scope config.Scope, role string) {
		cmd.Printf("%s: set provider option %s in %s config\n", role, strings.TrimSpace(providerOptionKeyPath), scope)
	})
}

func runProvidersOptionsUnset(cmd *cobra.Command, args []string) error {
	return runProviderScopedMutation(func(store *config.Store, scope config.Scope, role string) error {
		return store.RemoveProviderOptionValue(scope, role, strings.TrimSpace(providerOptionKeyPath))
	}, func(scope config.Scope, role string) {
		cmd.Printf("%s: removed provider option %s from %s config\n", role, strings.TrimSpace(providerOptionKeyPath), scope)
	})
}

func bindProviderMutationFlags(command *cobra.Command) {
	command.Flags().StringVar(&providerMutationRole, "role", "", "Role to update: orchestrator or worker")
	command.Flags().StringVar(&providerMutationScope, "scope", string(config.ScopeUser), "Config scope: user or workspace")
}

func runProviderScopedMutation(mutate func(store *config.Store, scope config.Scope, role string) error, onSuccess func(scope config.Scope, role string)) error {
	role, err := normalizeProviderRole(providerMutationRole)
	if err != nil {
		return err
	}
	scope, err := config.ParseScope(providerMutationScope)
	if err != nil {
		return err
	}
	store, err := openConfigStore()
	if err != nil {
		return err
	}
	if err := mutate(store, scope, role); err != nil {
		return err
	}
	onSuccess(scope, role)
	return nil
}

func parseMutationValue(raw string) (any, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}

	var value any
	if err := json.Unmarshal([]byte(trimmed), &value); err == nil {
		return value, nil
	}
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") || trimmed == "true" || trimmed == "false" || trimmed == "null" {
		return nil, fmt.Errorf("invalid JSON value: %s", trimmed)
	}
	return raw, nil
}

func resolveProviderRoles(role string, all bool) ([]string, error) {
	if all {
		return []string{"orchestrator", "worker"}, nil
	}
	if strings.TrimSpace(role) == "" {
		return []string{"orchestrator", "worker"}, nil
	}
	normalized, err := normalizeProviderRole(role)
	if err != nil {
		return nil, err
	}
	return []string{normalized}, nil
}

func normalizeProviderRole(role string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "orchestrator", "worker":
		return strings.ToLower(strings.TrimSpace(role)), nil
	default:
		return "", fmt.Errorf("role must be orchestrator or worker")
	}
}
