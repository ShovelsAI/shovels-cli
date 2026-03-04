package cmd

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/shovels-ai/shovels-cli/internal/client"
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

var tagsCmd = &cobra.Command{
	Use:   "tags",
	Short: "List permit tags for use in --tags filters",
	Long: `Discover valid permit tag values for use in --tags flags on permit and
contractor searches.

Available subcommands:
  list   List all permit tags with their descriptions

Every response is a JSON envelope: {"data": [...], "meta": {...}}`,
}

var tagsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all permit tags with their descriptions",
	Long: `List all valid permit tags from the Shovels database. Each tag has an id
and a human-readable description. Use the id value in --tags flags on
permits and contractors search commands.

Examples:
  List all tags (first 50 by default):
    shovels tags list

  List all tags across all pages:
    shovels tags list --limit all

  List a small sample:
    shovels tags list --limit 5

Workflow -- discover tags, then search permits:
  TAG=$(shovels tags list | jq -r '.data[0].id')
  shovels permits search --geo-id 92024 --tags "$TAG" --permit-from 2024-01-01 --permit-to 2024-12-31

Response: {"data": [{"id": "solar", "description": "Solar panel installation"}, ...], "meta": {"count": N, "has_more": bool, ...}}`,
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runTagsList,
}

func runTagsList(cmd *cobra.Command, args []string) error {
	lc, err := parseLimitConfig(cmd)
	if err != nil {
		return err
	}

	if _, err := validateTimeout(cmd); err != nil {
		return err
	}

	if isDryRun(cmd) {
		q := url.Values{}
		q.Set("size", fmt.Sprintf("%d", lc.FirstPageSize()))
		return printDryRun(cmd, "/list/tags", q)
	}

	cl, err := newClientFromFlags(cmd)
	if err != nil {
		return err
	}

	result, err := cl.Paginate(context.Background(), "/list/tags", url.Values{}, lc)
	if err != nil {
		apiErr, ok := err.(*client.APIError)
		if ok {
			output.PrintErrorTyped(os.Stderr, apiErr.Message, apiErr.ExitCode, apiErr.ErrorType)
			return &exitError{code: apiErr.ExitCode}
		}
		output.PrintErrorTyped(os.Stderr, err.Error(), 1, client.ErrorTypeClient)
		return &exitError{code: 1}
	}

	output.PrintPaginated(cmd.OutOrStdout(), result.Items, result.HasMore, result.Credits, nil)
	return nil
}

func init() {
	tagsCmd.AddCommand(tagsListCmd)
	rootCmd.AddCommand(tagsCmd)
}
