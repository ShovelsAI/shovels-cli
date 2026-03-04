//go:generate go run schema_gen.go

package cmd

import "sort"

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
