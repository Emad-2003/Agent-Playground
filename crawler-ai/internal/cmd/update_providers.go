package cmd

import (
	"strings"

	"crawler-ai/internal/providercatalog"

	"github.com/spf13/cobra"
)

var updateProvidersCmd = &cobra.Command{
	Use:   "update-providers [path-or-url]",
	Short: "Refresh the persisted provider catalog",
	Long:  "Refresh the provider catalog from the embedded defaults, a local JSON file, or a remote JSON URL.",
	RunE:  runUpdateProviders,
}

func init() {
	rootCmd.AddCommand(updateProvidersCmd)
}

func runUpdateProviders(cmd *cobra.Command, args []string) error {
	pathOrURL := ""
	if len(args) > 0 {
		pathOrURL = args[0]
	}

	result, err := providercatalog.Update(pathOrURL)
	if err != nil {
		return err
	}

	cmd.Printf("updated provider catalog from %s\n", result.Source)
	cmd.Printf("applied %d provider definitions\n", result.Applied)
	if len(result.Ignored) > 0 {
		cmd.Printf("ignored unsupported providers: %s\n", strings.Join(result.Ignored, ", "))
	}
	return nil
}
