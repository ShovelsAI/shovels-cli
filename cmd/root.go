package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/shovels-ai/shovels-cli/internal/client"
	"github.com/shovels-ai/shovels-cli/internal/config"
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/shovels-ai/shovels-cli/internal/update"
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

// updateResultCh receives the background autoupdate result. Nil when
// autoupdate is disabled or was not started for this invocation.
var updateResultCh chan *update.Result

// updateCancel cancels the background update goroutine's context.
var updateCancel context.CancelFunc

// updateStartTime records when the update goroutine was launched,
// allowing waitForUpdate to calculate remaining timeout budget.
var updateStartTime time.Time

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
  permits         Search and retrieve building permits by location, date, type, and contractor
  contractors     Search contractors, retrieve details, list their permits/employees/metrics
  addresses       Search addresses by street, city, state, or zip code
  cities          Search cities to resolve geo_ids for city-level queries
  counties        Search counties to resolve geo_ids for county-level queries
  jurisdictions   Search jurisdictions to resolve geo_ids for jurisdiction-level queries
  zipcodes        Search zip codes to find geo_ids for use with --geo-id
  states          Search US states to find 2-letter abbreviation geo_ids
  tags            List valid permit tags for use in --tags filters
  schema          Show annotated JSON response schema for any command (offline, no API call)
  usage           Check API credit consumption for the authenticated account
  config          Read and write persistent settings (API key, base URL)
  version         Print CLI version, git commit, build date, and API data freshness

Authentication (checked in this order):
  1. SHOVELS_API_KEY environment variable
  2. ~/.config/shovels/config.yaml

Quick start:
  export SHOVELS_API_KEY=your-key-here
  # or: shovels config set api-key YOUR_API_KEY
  shovels permits search --geo-id 92024 --permit-from 2024-01-01 --permit-to 2024-12-31
  shovels contractors search --geo-id 78701 --permit-from 2024-01-01 --permit-to 2024-12-31 --tags solar

Resolve a city to a geo_id, then search:
  GEO=$(shovels cities search -q "Miami" | jq -r '.data[0].geo_id')
  shovels permits search --geo-id "$GEO" --permit-from 2024-01-01 --permit-to 2024-12-31`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		flagBaseURL, _ := cmd.Flags().GetString("base-url")

		o := config.Overrides{
			BaseURL:    flagBaseURL,
			BaseURLSet: cmd.Flags().Changed("base-url"),
		}

		cfg, err := config.Resolve(o)
		if err != nil {
			// Version must never fail, even with a malformed config file.
			// All other commands surface the config error so users know
			// their config needs fixing.
			if cmd.Name() == "version" {
				resolvedConfig = config.FallbackConfig(o)
				return nil
			}
			output.PrintErrorTyped(os.Stderr, err.Error(), 1, client.ErrorTypeClient)
			return &exitError{code: 1}
		}
		resolvedConfig = cfg

		dryRun, _ := cmd.Flags().GetBool("dry-run")
		schema := isSchema(cmd)
		if requiresAuth(cmd) && cfg.APIKey == "" && !dryRun && !schema {
			msg := "API key not configured. Set SHOVELS_API_KEY or run: shovels config set api-key <key>"
			output.PrintErrorTyped(os.Stderr, msg, 2, client.ErrorTypeAuth)
			return &exitError{code: 2}
		}

		maybeStartUpdate(cfg)
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

// autoupdateDisabled returns true when autoupdate should not run.
func autoupdateDisabled(cfg config.Config) bool {
	if !cfg.AutoupdateEnabled() {
		return true
	}
	if buildVersion == "dev" {
		return true
	}
	if os.Getenv("CI") != "" {
		return true
	}
	return false
}

// maybeStartUpdate launches the background update goroutine if
// autoupdate is enabled and the cache is stale.
func maybeStartUpdate(cfg config.Config) {
	if autoupdateDisabled(cfg) {
		return
	}

	cfgDir, err := config.ConfigDir()
	if err != nil {
		return
	}

	// Check cache freshness before spawning a goroutine so that a
	// fresh cache (< 24h) incurs zero overhead on every invocation.
	if !update.CacheExpired(cfgDir, nil) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), update.Timeout)
	updateCancel = cancel
	updateStartTime = time.Now()
	updateResultCh = make(chan *update.Result, 1)

	go func() {
		defer cancel()
		result := update.Check(ctx, update.Options{
			CurrentVersion: buildVersion,
			ConfigDir:      cfgDir,
		})
		updateResultCh <- result
	}()
}

// waitForUpdate waits for the background update goroutine to finish
// (up to the remaining 10s budget) and prints the notice to stderr.
func waitForUpdate() {
	if updateResultCh == nil {
		return
	}

	remaining := update.Timeout - time.Since(updateStartTime)
	if remaining <= 0 {
		if updateCancel != nil {
			updateCancel()
		}
		return
	}

	timer := time.NewTimer(remaining)
	defer timer.Stop()

	var result *update.Result
	select {
	case result = <-updateResultCh:
	case <-timer.C:
	}

	if updateCancel != nil {
		updateCancel()
	}

	if msg := update.NoticeMessage(result); msg != "" {
		fmt.Fprint(os.Stderr, msg)
	}
}

func init() {
	flags := rootCmd.PersistentFlags()
	flags.String("limit", "50", `Maximum records to return: integer 1-100000 or "all" (default "50")`)
	flags.Int("max-records", 10000, "Upper bound when --limit=all, range 1-100000 (default 10000)")
	flags.String("base-url", "https://api.shovels.ai/v2", "API base URL (default https://api.shovels.ai/v2)")
	flags.Bool("no-retry", false, "Disable automatic retry on HTTP 429 rate-limit responses")
	flags.String("timeout", "30s", "Per-request timeout as a Go duration, e.g. 10s, 1m, 2m30s (default 30s)")
	flags.Bool("dry-run", false, "Print the resolved HTTP request as JSON without calling the API or consuming credits")

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
	defer waitForUpdate()
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
