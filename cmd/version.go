package cmd

import (
	"context"
	"encoding/json"
	"time"

	"github.com/shovels-ai/shovels-cli/internal/client"
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	buildVersion = "dev"
	buildCommit  = "none"
	buildDate    = "unknown"
)

// metaFetchTimeout is the deadline for the /meta/release API call.
// Kept as a var so unit tests can verify the value.
var metaFetchTimeout = 2 * time.Second

// SetVersionInfo stores build-time version metadata for the version command.
func SetVersionInfo(version, commit, date string) {
	buildVersion = version
	buildCommit = commit
	buildDate = date
	rootCmd.Version = version
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print CLI version, git commit SHA, build date, and API data freshness as JSON",
	Long: `Print the CLI build version, git commit SHA, build date, and API data freshness
date as a JSON object. The data_release_date field shows when the Shovels API
data was last updated — useful for knowing how current query results are.

If no API key is configured or the API is unreachable, data_release_date is null
and the command still succeeds with build info only.

Example:
  shovels version

Output:
  {"data":{"version":"0.3.0","commit":"abc123","date":"2026-03-01","data_release_date":"2026-02-28"},"meta":{}}`,
	Run: func(cmd *cobra.Command, args []string) {
		var releaseDate *string

		cfg := ResolvedConfig()
		if cfg.APIKey != "" {
			releaseDate = fetchDataReleaseDate(cmd.Context(), cfg.APIKey, cfg.BaseURL)
		}

		data := map[string]any{
			"version":           buildVersion,
			"commit":            buildCommit,
			"date":              buildDate,
			"data_release_date": releaseDate,
		}
		output.PrintData(cmd.OutOrStdout(), data)
	},
}

// fetchDataReleaseDate calls GET /meta/release with a short timeout and returns
// the released_at date string. Returns nil on any failure — network errors, bad
// status codes, or malformed responses are all silently ignored so the version
// command never fails.
func fetchDataReleaseDate(ctx context.Context, apiKey, baseURL string) *string {
	cl := client.New(client.Options{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Timeout: metaFetchTimeout,
		NoRetry: true,
		Version: buildVersion,
	})

	resp, err := cl.Get(ctx, "/meta/release", nil)
	if err != nil {
		return nil
	}

	var body struct {
		ReleasedAt string `json:"released_at"`
	}
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		return nil
	}
	if body.ReleasedAt == "" {
		return nil
	}
	return &body.ReleasedAt
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
