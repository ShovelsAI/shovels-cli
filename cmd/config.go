package cmd

import (
	"os"

	"github.com/shovels-ai/shovels-cli/internal/config"
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage CLI configuration (API key, base URL, defaults)",
	Long: `Read and write persistent CLI configuration stored in ~/.config/shovels/config.yaml.

Subcommands:
  set    Write a configuration key (e.g. api-key, base-url)
  show   Display current resolved configuration as JSON`,
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Display resolved configuration as JSON to stdout",
	Long: `Prints the current resolved configuration as a JSON object.
The API key is masked for security (first 4 + last 4 characters shown).
Config values reflect the full precedence chain: flag > env > file > default.`,
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
	Use:   "set <key> <value>",
	Short: "Set a configuration value persistently",
	Long: `Write a configuration key to ~/.config/shovels/config.yaml.

Supported keys:
  api-key     Your Shovels API key
  base-url    API base URL (default: https://api.shovels.ai/v2)

Example:
  shovels config set api-key sk-your-api-key-here`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := args[0]
		value := args[1]

		// Map CLI key names to YAML field names.
		yamlKey, ok := keyMapping[key]
		if !ok {
			msg := "unknown config key: " + key + ". Supported keys: api-key, base-url"
			output.PrintError(os.Stderr, msg, 1)
			return &exitError{code: 1}
		}

		if err := config.SaveToFile(yamlKey, value); err != nil {
			output.PrintError(os.Stderr, err.Error(), 1)
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
