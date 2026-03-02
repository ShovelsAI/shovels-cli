package cmd

import (
	"os"

	"github.com/shovels-ai/shovels-cli/internal/client"
	"github.com/shovels-ai/shovels-cli/internal/config"
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

// AnnotationRequiresAuth is the cobra annotation key used to mark commands
// that need a valid API key. Commands without this annotation skip auth checks.
const AnnotationRequiresAuth = "requires_auth"

// resolvedConfig holds the config resolved during PersistentPreRun, available
// to all subcommands within the same execution.
var resolvedConfig config.Config

// ResolvedConfig returns the config resolved during PersistentPreRun.
func ResolvedConfig() config.Config {
	return resolvedConfig
}

// flagErrPrinted tracks whether SetFlagErrorFunc already emitted JSON to
// stderr, preventing Execute from printing a duplicate error.
var flagErrPrinted bool

// exitError carries a specific exit code through cobra's error chain.
type exitError struct {
	code int
}

func (e *exitError) Error() string { return "" }

var rootCmd = &cobra.Command{
	Use:   "shovels",
	Short: "Query the Shovels building permit and contractor database from the command line",
	Long: `shovels is a CLI for the Shovels REST API. It searches building permits,
contractors, and addresses across the United States.

Every command outputs valid JSON to stdout. Errors go to stderr as JSON.
Pipe output to jq, parse it programmatically, or feed it to another AI agent.

Available resources:
  permits       Search and retrieve building permits by location, date, type, and contractor
  contractors   Search contractors, retrieve details, list their permits/employees/metrics
  addresses     Search addresses by street, city, state, or zip code
  usage         Check API credit consumption for the authenticated account
  config        Read and write persistent settings (API key, base URL)
  version       Print CLI version, git commit, and build date

Authentication (checked in this order):
  1. --api-key flag
  2. SHOVELS_API_KEY environment variable
  3. ~/.config/shovels/config.yaml

Quick start:
  shovels config set api-key YOUR_API_KEY
  shovels permits search --geo-id ZIP_90210 --from 2024-01-01 --to 2024-12-31
  shovels contractors search --geo-id ZIP_90210 --from 2024-01-01 --to 2024-12-31 --tags solar`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		flagAPIKey, _ := cmd.Flags().GetString("api-key")
		flagBaseURL, _ := cmd.Flags().GetString("base-url")

		o := config.Overrides{
			APIKey:     flagAPIKey,
			APIKeySet:  cmd.Flags().Changed("api-key"),
			BaseURL:    flagBaseURL,
			BaseURLSet: cmd.Flags().Changed("base-url"),
		}

		cfg, err := config.Resolve(o)
		if err != nil {
			output.PrintErrorTyped(os.Stderr, err.Error(), 1, client.ErrorTypeClient)
			return &exitError{code: 1}
		}
		resolvedConfig = cfg

		if requiresAuth(cmd) && cfg.APIKey == "" {
			msg := "API key not configured. Set SHOVELS_API_KEY or run: shovels config set api-key <key>"
			output.PrintErrorTyped(os.Stderr, msg, 2, client.ErrorTypeAuth)
			return &exitError{code: 2}
		}

		return nil
	},
}

// requiresAuth checks whether the command (or any of its parents) is
// annotated as requiring authentication.
func requiresAuth(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Annotations != nil {
			if _, ok := c.Annotations[AnnotationRequiresAuth]; ok {
				return true
			}
		}
	}
	return false
}

func init() {
	flags := rootCmd.PersistentFlags()
	flags.String("api-key", "", "API key for authentication (overrides SHOVELS_API_KEY env var and config file)")
	flags.String("limit", "50", `Maximum records to return: integer 1-100000 or "all" (default "50")`)
	flags.Int("max-records", 10000, "Upper bound when --limit=all, range 1-100000 (default 10000)")
	flags.String("base-url", "https://api.shovels.ai/v2", "API base URL (default https://api.shovels.ai/v2)")
	flags.Bool("no-retry", false, "Disable automatic retry on HTTP 429 rate-limit responses")
	flags.String("timeout", "30s", "Per-request timeout as a Go duration, e.g. 10s, 1m, 2m30s (default 30s)")

	// Emit JSON to stderr on flag-parsing errors instead of cobra's plain text.
	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		flagErrPrinted = true
		output.PrintErrorTyped(os.Stderr, err.Error(), 1, client.ErrorTypeClient)
		return err
	})
}

// Execute runs the root command and returns the exit code.
func Execute() int {
	flagErrPrinted = false
	if err := rootCmd.Execute(); err != nil {
		if e, ok := err.(*exitError); ok {
			return e.code
		}
		if !flagErrPrinted {
			output.PrintErrorTyped(os.Stderr, err.Error(), 1, client.ErrorTypeClient)
		}
		return 1
	}
	return 0
}
