package cmd

import (
	"os"

	"github.com/shovels-ai/shovels-cli/internal/client"
	"github.com/shovels-ai/shovels-cli/internal/config"
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Read and write persistent CLI settings (API key, base URL)",
	Long: `Manage persistent CLI configuration stored in ~/.config/shovels/config.yaml.

Available subcommands:
  set    Save a configuration key (api-key, base-url) to the config file
  show   Display the resolved configuration as JSON (API key is masked)`,
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Display the resolved configuration as JSON (API key masked)",
	Long: `Print the resolved configuration as a JSON object to stdout. The API key
is masked for security (first 4 and last 4 characters shown). Values
reflect the full precedence chain: SHOVELS_API_KEY env > config file > default.

Example:
  shovels config show`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := ResolvedConfig()
		data := map[string]any{
			"api_key":       config.MaskAPIKey(cfg.APIKey),
			"base_url":      cfg.BaseURL,
			"default_limit": cfg.MaxLimit,
		}
		output.PrintData(cmd.OutOrStdout(), data)
		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set KEY VALUE",
	Short: "Save a configuration key to ~/.config/shovels/config.yaml",
	Long: `Write a configuration key-value pair to ~/.config/shovels/config.yaml.

Supported keys:
  api-key     Your Shovels API key (e.g. sk-abc123)
  base-url    API base URL (default: https://api.shovels.ai/v2)

Examples:
  Save an API key:
    shovels config set api-key sk-your-api-key-here

  Override the base URL:
    shovels config set base-url https://staging.shovels.ai/v2`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := args[0]
		value := args[1]

		// Map CLI key names to YAML field names.
		yamlKey, ok := keyMapping[key]
		if !ok {
			msg := "unknown config key: " + key + ". Supported keys: api-key, base-url"
			output.PrintErrorTyped(os.Stderr, msg, 1, client.ErrorTypeClient)
			return &exitError{code: 1}
		}

		if err := config.SaveToFile(yamlKey, value); err != nil {
			output.PrintErrorTyped(os.Stderr, err.Error(), 1, client.ErrorTypeClient)
			return &exitError{code: 1}
		}

		output.PrintData(cmd.OutOrStdout(), map[string]string{
			"status": "ok",
			"key":    key,
		})
		return nil
	},
}

// keyMapping translates CLI-friendly key names to YAML config field names.
var keyMapping = map[string]string{
	"api-key":  "api_key",
	"base-url": "base_url",
}

func init() {
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configSetCmd)
	rootCmd.AddCommand(configCmd)
}
