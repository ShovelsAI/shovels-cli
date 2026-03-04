//go:generate go run schema_gen.go

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/shovels-ai/shovels-cli/internal/client"
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

// SchemaField describes a single field in an API response.
type SchemaField struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Unit        string `json:"unit,omitempty"`
	Range       string `json:"range,omitempty"`
	Enum        string `json:"enum,omitempty"`
}

// CommandSchema holds the complete schema for a single CLI command's response.
type CommandSchema struct {
	SchemaVersion  int                    `json:"schema_version"`
	Command        string                 `json:"command"`
	ResponseFields map[string]SchemaField `json:"response_fields"`
	FieldIndex     []string               `json:"field_index"`
	Filters        map[string]SchemaField `json:"filters"`
}

// schemaRegistry maps space-separated command paths to their schemas.
// Populated by the generated schema_data.go file.
var schemaRegistry map[string]CommandSchema

// SchemaCommands returns all registered command paths sorted alphabetically.
func SchemaCommands() []string {
	paths := make([]string, 0, len(schemaRegistry))
	for p := range schemaRegistry {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// LookupSchema returns the schema for the given command path, or nil if not found.
func LookupSchema(command string) *CommandSchema {
	s, ok := schemaRegistry[command]
	if !ok {
		return nil
	}
	return &s
}

// OverrideField holds enrichment data from YAML overrides for a single field.
type OverrideField struct {
	Description string `yaml:"description"`
	Unit        string `yaml:"unit"`
	Range       string `yaml:"range"`
	Enum        string `yaml:"enum"`
}

// OverrideCommand holds the override fields for a single command.
type OverrideCommand struct {
	Fields map[string]OverrideField `yaml:"fields"`
}

// MergeField applies override enrichments to an OpenAPI-derived SchemaField.
// Override values take precedence over OpenAPI values when non-empty.
func MergeField(base SchemaField, override OverrideField) SchemaField {
	if override.Description != "" {
		base.Description = override.Description
	}
	if override.Unit != "" {
		base.Unit = override.Unit
	}
	if override.Range != "" {
		base.Range = override.Range
	}
	if override.Enum != "" {
		base.Enum = override.Enum
	}
	return base
}

// MergeFields merges override fields into OpenAPI-derived fields. Only fields
// that exist in the base map are updated; override fields not present in
// the base are ignored (no phantom fields).
func MergeFields(base map[string]SchemaField, overrides map[string]OverrideField) map[string]SchemaField {
	for name, override := range overrides {
		if field, ok := base[name]; ok {
			base[name] = MergeField(field, override)
		}
	}
	return base
}

// schemaCommandPaths builds a formatted list of available command paths for
// use in --help text and error messages.
func schemaCommandPaths() string {
	cmds := SchemaCommands()
	var b strings.Builder
	for _, c := range cmds {
		b.WriteString("    ")
		b.WriteString(c)
		b.WriteByte('\n')
	}
	return b.String()
}

var schemaCmd = &cobra.Command{
	Use:   "schema [command-path...]",
	Short: "Show annotated JSON response schema for a CLI command (offline, no API call)",
	// Long is set lazily in init via SetHelpFunc so that schemaRegistry
	// (populated in schema_data.go init) is available for the command list.
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          runSchema,
}

// schemaLong builds the Long description for the schema command. Called after
// all init() functions have run so schemaRegistry is fully populated.
func schemaLong() string {
	return fmt.Sprintf(`Introspect the response shape of any data command without making an API call
or requiring authentication. Useful for LLM agents constructing jq pipelines.

With no arguments, lists all available command paths:
  shovels schema

With a command path, outputs the annotated JSON schema:
  shovels schema permits search

The schema includes:
  schema_version   Schema format version (currently 1)
  command          The command path this schema describes
  response_fields  Map of field name to type, description, unit, range, and enum
  field_index      Ordered list of jq-compatible field paths (data[].field, meta.field)
  filters          Map of CLI flag to type and description

Available command paths:
%s
Example output:
  shovels schema permits search
  {
    "schema_version": 1,
    "command": "permits search",
    "response_fields": {
      "id": {"type": "string", "description": "Unique permit identifier"},
      "job_value": {"type": "integer", "unit": "cents", "description": "Total project value"}
    },
    "field_index": ["data[].id", "data[].job_value", "meta.count", "meta.has_more"],
    "filters": {
      "--geo-id": {"type": "string", "description": "Geographic area ID (required)"}
    }
  }

Alias: any data command also accepts --schema to output its own schema:
  shovels permits search --schema`, schemaCommandPaths())
}

func runSchema(cmd *cobra.Command, args []string) error {
	// No arguments: list all available command paths.
	if len(args) == 0 {
		cmds := SchemaCommands()
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(cmds); err != nil {
			return fmt.Errorf("failed to encode command list: %w", err)
		}
		return nil
	}

	path := strings.Join(args, " ")
	return printSchemaForPath(cmd, path)
}

// printSchemaForPath looks up and prints the schema for the given command
// path. Returns an error (with stderr JSON) for invalid or partial paths.
func printSchemaForPath(cmd *cobra.Command, path string) error {
	s := LookupSchema(path)
	if s != nil {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(s); err != nil {
			return fmt.Errorf("failed to encode schema: %w", err)
		}
		return nil
	}

	// Check if the path is a word-boundary prefix of registered commands
	// (e.g., "permits" matches "permits search" but "permit" does not).
	prefix := path + " "
	var suggestions []string
	for _, c := range SchemaCommands() {
		if strings.HasPrefix(c, prefix) {
			suggestions = append(suggestions, c)
		}
	}

	if len(suggestions) > 0 {
		msg := fmt.Sprintf("incomplete path %q — available: %s", path, strings.Join(suggestions, ", "))
		output.PrintErrorTyped(os.Stderr, msg, 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}

	// Completely invalid path.
	msg := fmt.Sprintf("unknown command path %q — valid paths: %s", path, strings.Join(SchemaCommands(), ", "))
	output.PrintErrorTyped(os.Stderr, msg, 1, client.ErrorTypeValidation)
	return &exitError{code: 1}
}

// isSchema returns true when the --schema flag is set on the command.
func isSchema(cmd *cobra.Command) bool {
	f := cmd.Flags().Lookup("schema")
	if f == nil {
		return false
	}
	return f.Value.String() == "true"
}

// handleSchemaFlag checks if --schema is set and, if so, prints the schema
// for the given command path and returns true. Callers should return nil
// immediately when this returns true.
func handleSchemaFlag(cmd *cobra.Command, commandPath string) (bool, error) {
	if !isSchema(cmd) {
		return false, nil
	}
	return true, printSchemaForPath(cmd, commandPath)
}

// registerSchemaFlag adds the --schema flag to a data command.
func registerSchemaFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("schema", false, "Print the annotated JSON response schema for this command (offline, no API call, no auth required)")
}

// commandPathFromCobra derives the space-separated schema registry key from
// a cobra command's chain, excluding the root "shovels" prefix.
func commandPathFromCobra(cmd *cobra.Command) string {
	var parts []string
	for c := cmd; c != nil && c.Parent() != nil; c = c.Parent() {
		parts = append([]string{c.Name()}, parts...)
	}
	return strings.Join(parts, " ")
}

// exactArgsUnlessSchema returns a cobra.Args validator that requires exactly
// n args unless --schema is set, in which case any arg count is accepted.
func exactArgsUnlessSchema(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if isSchema(cmd) {
			return nil
		}
		return cobra.ExactArgs(n)(cmd, args)
	}
}

func init() {
	rootCmd.AddCommand(schemaCmd)

	// Wrap cobra's default help to set Long lazily. At var-init time,
	// schemaRegistry is nil (populated by schema_data.go init), so
	// the command path list can only be built after all init functions run.
	defaultHelp := schemaCmd.HelpFunc()
	schemaCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		cmd.Long = schemaLong()
		defaultHelp(cmd, args)
	})
}
