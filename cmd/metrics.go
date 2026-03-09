package cmd

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/shovels-ai/shovels-cli/internal/client"
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

// metricsConfig describes the shared flags and endpoint for a geo metrics command.
type metricsConfig struct {
	// resource is the URL path segment (e.g., "cities", "counties", "addresses").
	resource string

	// requirePropertyType controls whether --property-type is a required flag.
	// Addresses metrics does not use property_type.
	requirePropertyType bool
}

// runMetricsCurrent validates flags, builds query params, calls the current
// metrics endpoint, and prints the paginated result. It handles tag and
// optional property-type validation, and rejects date flags.
func runMetricsCurrent(cfg metricsConfig) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if handled, err := handleSchemaFlag(cmd, commandPathFromCobra(cmd)); handled {
			return err
		}

		tag, _ := cmd.Flags().GetString("tag")

		var missing []string
		if tag == "" {
			missing = append(missing, "--tag")
		}
		if cfg.requirePropertyType {
			propertyType, _ := cmd.Flags().GetString("property-type")
			if propertyType == "" {
				missing = append(missing, "--property-type")
			}
		}
		if len(missing) > 0 {
			msg := fmt.Sprintf("required flag(s) missing: %s", strings.Join(missing, ", "))
			output.PrintErrorTyped(os.Stderr, msg, 1, client.ErrorTypeValidation)
			return &exitError{code: 1}
		}

		lc, err := parseLimitConfig(cmd)
		if err != nil {
			return err
		}

		q := url.Values{
			"tag": {tag},
		}
		if cfg.requirePropertyType {
			propertyType, _ := cmd.Flags().GetString("property-type")
			q.Set("property_type", propertyType)
		}
		setBoolFlag(cmd, "include-count", "include_count", q)

		endpoint := fmt.Sprintf("/%s/%s/metrics/current", cfg.resource, args[0])

		if _, err := validateTimeout(cmd); err != nil {
			return err
		}

		if isDryRun(cmd) {
			q.Set("size", fmt.Sprintf("%d", lc.FirstPageSize()))
			return printDryRun(cmd, endpoint, q)
		}

		cl, err := newClientFromFlags(cmd)
		if err != nil {
			return err
		}

		result, err := cl.Paginate(context.Background(), endpoint, q, lc)
		if err != nil {
			return handleAPIError(err)
		}

		output.PrintPaginated(cmd.OutOrStdout(), result.Items, result.HasMore, result.Credits, result.TotalCount)
		return nil
	}
}

// runMetricsMonthly validates flags (including date range), builds query params,
// calls the monthly metrics endpoint, and prints the paginated result.
func runMetricsMonthly(cfg metricsConfig) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if handled, err := handleSchemaFlag(cmd, commandPathFromCobra(cmd)); handled {
			return err
		}

		tag, _ := cmd.Flags().GetString("tag")
		metricFrom, _ := cmd.Flags().GetString("metric-from")
		metricTo, _ := cmd.Flags().GetString("metric-to")

		var missing []string
		if tag == "" {
			missing = append(missing, "--tag")
		}
		if cfg.requirePropertyType {
			propertyType, _ := cmd.Flags().GetString("property-type")
			if propertyType == "" {
				missing = append(missing, "--property-type")
			}
		}
		if metricFrom == "" {
			missing = append(missing, "--metric-from")
		}
		if metricTo == "" {
			missing = append(missing, "--metric-to")
		}
		if len(missing) > 0 {
			msg := fmt.Sprintf("required flag(s) missing: %s", strings.Join(missing, ", "))
			output.PrintErrorTyped(os.Stderr, msg, 1, client.ErrorTypeValidation)
			return &exitError{code: 1}
		}

		if !datePattern.MatchString(metricFrom) {
			output.PrintErrorTyped(os.Stderr, fmt.Sprintf("invalid date format for --metric-from: %q (expected YYYY-MM-DD)", metricFrom), 1, client.ErrorTypeValidation)
			return &exitError{code: 1}
		}
		if !datePattern.MatchString(metricTo) {
			output.PrintErrorTyped(os.Stderr, fmt.Sprintf("invalid date format for --metric-to: %q (expected YYYY-MM-DD)", metricTo), 1, client.ErrorTypeValidation)
			return &exitError{code: 1}
		}

		lc, err := parseLimitConfig(cmd)
		if err != nil {
			return err
		}

		q := url.Values{
			"tag":         {tag},
			"metric_from": {metricFrom},
			"metric_to":   {metricTo},
		}
		if cfg.requirePropertyType {
			propertyType, _ := cmd.Flags().GetString("property-type")
			q.Set("property_type", propertyType)
		}
		setBoolFlag(cmd, "include-count", "include_count", q)

		endpoint := fmt.Sprintf("/%s/%s/metrics/monthly", cfg.resource, args[0])

		if _, err := validateTimeout(cmd); err != nil {
			return err
		}

		if isDryRun(cmd) {
			q.Set("size", fmt.Sprintf("%d", lc.FirstPageSize()))
			return printDryRun(cmd, endpoint, q)
		}

		cl, err := newClientFromFlags(cmd)
		if err != nil {
			return err
		}

		result, err := cl.Paginate(context.Background(), endpoint, q, lc)
		if err != nil {
			return handleAPIError(err)
		}

		output.PrintPaginated(cmd.OutOrStdout(), result.Items, result.HasMore, result.Credits, result.TotalCount)
		return nil
	}
}

// registerMetricsCurrentFlags registers shared flags for a current metrics
// command: --tag, --include-count, and optionally --property-type.
// Also installs the date flag rejection handler.
func registerMetricsCurrentFlags(cmd *cobra.Command, withPropertyType bool) {
	cmd.Flags().String("tag", "", "Permit tag: solar, roofing, electrical, plumbing, etc. (required)")
	if withPropertyType {
		cmd.Flags().String("property-type", "", "Property type: residential, commercial, industrial, agricultural, vacant land, exempt, miscellaneous, office, recreational (required)")
	}
	cmd.Flags().Bool("include-count", false, "Request total result count (capped at 10,000). Returned as total_count in meta on first page")
	rejectDateFlagsOnCurrent(cmd)
}

// registerMetricsMonthlyFlags registers shared flags for a monthly metrics
// command: --tag, --metric-from, --metric-to, --include-count, and optionally
// --property-type.
func registerMetricsMonthlyFlags(cmd *cobra.Command, withPropertyType bool) {
	cmd.Flags().String("tag", "", "Permit tag: solar, roofing, electrical, plumbing, etc. (required)")
	if withPropertyType {
		cmd.Flags().String("property-type", "", "Property type: residential, commercial, industrial, agricultural, vacant land, exempt, miscellaneous, office, recreational (required)")
	}
	cmd.Flags().String("metric-from", "", "Start date in YYYY-MM-DD format (required)")
	cmd.Flags().String("metric-to", "", "End date in YYYY-MM-DD format (required)")
	cmd.Flags().Bool("include-count", false, "Request total result count (capped at 10,000). Returned as total_count in meta on first page")
}

// handleAPIError maps API errors to stderr output and exit codes.
func handleAPIError(err error) error {
	apiErr, ok := err.(*client.APIError)
	if ok {
		output.PrintErrorTyped(os.Stderr, apiErr.Message, apiErr.ExitCode, apiErr.ErrorType)
		return &exitError{code: apiErr.ExitCode}
	}
	output.PrintErrorTyped(os.Stderr, err.Error(), 1, client.ErrorTypeClient)
	return &exitError{code: 1}
}
