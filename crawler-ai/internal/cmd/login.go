package cmd

import (
	"fmt"

	"crawler-ai/internal/oauth"

	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Configure API keys for providers",
	Long:  "Interactively set up API keys for AI providers. Keys are stored securely in the active crawler-ai data directory with restricted permissions (0600).",
	RunE:  runLogin,
}

func init() {
	rootCmd.AddCommand(loginCmd)
}

func runLogin(cmd *cobra.Command, args []string) error {
	store := oauth.DefaultKeyStore()
	if err := store.Load(); err != nil {
		return fmt.Errorf("failed to load existing keys: %w", err)
	}

	// Select provider
	provider, err := oauth.PromptForProvider(oauth.Stdin(), oauth.Stdout())
	if err != nil {
		return fmt.Errorf("provider selection failed: %w", err)
	}

	// Check for existing key
	if store.HasKey(provider) {
		overwrite, err := oauth.ConfirmOverwrite(provider, oauth.Stdin(), oauth.Stdout())
		if err != nil {
			return err
		}
		if !overwrite {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	// Prompt for key
	key, err := oauth.PromptForKey(provider)
	if err != nil {
		return fmt.Errorf("key input failed: %w", err)
	}

	if key == "" {
		return fmt.Errorf("API key cannot be empty")
	}

	// Store and save
	store.Set(provider, key)
	if err := store.Save(); err != nil {
		return fmt.Errorf("failed to save key: %w", err)
	}

	fmt.Printf("API key for %s stored at %s\n", provider, store.Path())

	// Show env var hint
	envVar := oauth.EnvVarForProvider(provider)
	fmt.Printf("You can also set this via environment variable: %s\n", envVar)

	return nil
}
