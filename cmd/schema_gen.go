//go:build ignore

// Schema generator: fetches the OpenAPI spec and merges YAML overrides to
// produce schema_data.go with embedded schema data for all CLI commands.
// Runs from the cmd/ directory via go generate.
//
// Usage: go generate ./cmd/...

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const openAPIURL = "https://api.shovels.ai/v2/openapi.json"

// schemaField mirrors cmd.SchemaField for code generation.
type schemaField struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Unit        string `json:"unit,omitempty"`
	Range       string `json:"range,omitempty"`
	Enum        string `json:"enum,omitempty"`
}

// overrideField mirrors cmd.OverrideField.
type overrideField struct {
	Description string `yaml:"description"`
	Unit        string `yaml:"unit"`
	Range       string `yaml:"range"`
	Enum        string `yaml:"enum"`
}

// overrideCommand holds overrides for a single command.
type overrideCommand struct {
	Fields map[string]overrideField `yaml:"fields"`
}

// commandDef maps a CLI command path to its OpenAPI endpoint and response schema.
type commandDef struct {
	Command        string // space-separated CLI path
	ResponseSchema string // OpenAPI $ref schema name for items
	Endpoint       string // API endpoint path
	FiltersFrom    string // source for filters: "search", "metrics_prop", "metrics_noprop", "get", "none"
}

// allCommands defines every data command that needs a schema entry.
// ResponseSchema values must match schema names in the OpenAPI spec's
// components/schemas section.
var allCommands = []commandDef{
	{"permits search", "PermitsRead", "/permits/search", "permits_search"},
	{"permits get", "PermitsRead", "/permits", "get"},
	{"contractors search", "ContractorsRead", "/contractors/search", "contractors_search"},
	{"contractors get", "ContractorsRead", "/contractors", "get"},
	{"contractors permits", "PermitsRead", "/contractors/{id}/permits", "none"},
	{"contractors employees", "Employees", "/contractors/{id}/employees", "none"},
	{"contractors metrics", "ContractorsMetricsMonthlyRead", "/contractors/{id}/metrics", "contractor_metrics"},
	{"cities search", "GeoEntitiesRead", "/cities/search", "geo_search"},
	{"cities metrics current", "CitiesMetricsCurrentRead", "/cities/{geo_id}/metrics/current", "metrics_prop"},
	{"cities metrics monthly", "CitiesMetricsMonthlyRead", "/cities/{geo_id}/metrics/monthly", "metrics_prop_monthly"},
	{"counties search", "GeoEntitiesRead", "/counties/search", "geo_search"},
	{"counties metrics current", "CountiesMetricsCurrentRead", "/counties/{geo_id}/metrics/current", "metrics_prop"},
	{"counties metrics monthly", "CountiesMetricsMonthlyRead", "/counties/{geo_id}/metrics/monthly", "metrics_prop_monthly"},
	{"jurisdictions search", "GeoEntitiesRead", "/jurisdictions/search", "geo_search"},
	{"jurisdictions metrics current", "JurisdictionsMetricsCurrentRead", "/jurisdictions/{geo_id}/metrics/current", "metrics_prop"},
	{"jurisdictions metrics monthly", "JurisdictionsMetricsMonthlyRead", "/jurisdictions/{geo_id}/metrics/monthly", "metrics_prop_monthly"},
	{"addresses search", "api__app__models__geo__AddressesRead", "/addresses/search", "geo_search"},
	{"addresses metrics current", "AddressesMetricsCurrentRead", "/addresses/{geo_id}/metrics/current", "metrics_noprop"},
	{"addresses metrics monthly", "AddressesMetricsMonthlyRead", "/addresses/{geo_id}/metrics/monthly", "metrics_noprop_monthly"},
	{"addresses residents", "ResidentsRead", "/addresses/{geo_id}/residents", "none"},
	{"zipcodes search", "Zipcodes", "/zipcodes/search", "geo_search"},
	{"states search", "States", "/states/search", "geo_search"},
	{"tags list", "Tags", "/list/tags", "none"},
}

func main() {
	spec, err := fetchOpenAPI(openAPIURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch OpenAPI spec: %v\n", err)
		os.Exit(1)
	}

	overrides, err := loadOverrides("schema_overrides.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load overrides: %v\n", err)
		os.Exit(1)
	}

	schemas := make(map[string]commandSchemaData)
	for _, def := range allCommands {
		fields := extractFields(spec, def.ResponseSchema)
		if cmdOverride, ok := overrides[def.Command]; ok {
			mergeFields(fields, cmdOverride.Fields)
		}
		filters := buildFilters(def)
		fieldIndex := buildFieldIndex(fields)

		schemas[def.Command] = commandSchemaData{
			ResponseFields: fields,
			FieldIndex:     fieldIndex,
			Filters:        filters,
		}
	}

	if err := writeSchemaData("schema_data.go", schemas); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write schema_data.go: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Generated schema_data.go with %d command schemas\n", len(schemas))
}

type commandSchemaData struct {
	ResponseFields map[string]schemaField
	FieldIndex     []string
	Filters        map[string]schemaField
}

// fetchOpenAPI downloads and parses the OpenAPI JSON spec.
func fetchOpenAPI(url string) (map[string]any, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var spec map[string]any
	if err := json.Unmarshal(body, &spec); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return spec, nil
}

// loadOverrides reads and parses the YAML overrides file.
func loadOverrides(path string) (map[string]overrideCommand, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}

	var overrides map[string]overrideCommand
	if err := yaml.Unmarshal(data, &overrides); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}

	return overrides, nil
}

// extractFields pulls response field definitions from the OpenAPI spec for a
// given schema name. It resolves $ref pointers within the components/schemas
// section and maps JSON Schema types to simpler type strings.
func extractFields(spec map[string]any, schemaName string) map[string]schemaField {
	fields := make(map[string]schemaField)

	components, ok := spec["components"].(map[string]any)
	if !ok {
		return fields
	}
	schemasMap, ok := components["schemas"].(map[string]any)
	if !ok {
		return fields
	}

	schema, ok := schemasMap[schemaName].(map[string]any)
	if !ok {
		return fields
	}

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return fields
	}

	for name, propRaw := range properties {
		prop, ok := propRaw.(map[string]any)
		if !ok {
			continue
		}

		f := schemaField{
			Type: resolveType(prop, schemasMap),
		}

		if desc, ok := prop["description"].(string); ok {
			f.Description = desc
		}
		if title, ok := prop["title"].(string); ok && f.Description == "" {
			f.Description = title
		}

		fields[name] = f
	}

	return fields
}

// resolveType determines a human-readable type string from an OpenAPI property.
func resolveType(prop map[string]any, schemas map[string]any) string {
	// Handle anyOf / oneOf (nullable types)
	for _, key := range []string{"anyOf", "oneOf"} {
		if variants, ok := prop[key].([]any); ok {
			for _, v := range variants {
				vm, ok := v.(map[string]any)
				if !ok {
					continue
				}
				if t, ok := vm["type"].(string); ok && t != "null" {
					if t == "array" {
						return resolveArrayType(vm, schemas)
					}
					return mapType(t, vm)
				}
				if ref, ok := vm["$ref"].(string); ok {
					return refToName(ref)
				}
			}
			return "string"
		}
	}

	// Handle $ref
	if ref, ok := prop["$ref"].(string); ok {
		return refToName(ref)
	}

	// Handle allOf
	if allOf, ok := prop["allOf"].([]any); ok {
		for _, item := range allOf {
			im, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if ref, ok := im["$ref"].(string); ok {
				return refToName(ref)
			}
		}
	}

	t, _ := prop["type"].(string)
	if t == "array" {
		return resolveArrayType(prop, schemas)
	}
	return mapType(t, prop)
}

// resolveArrayType determines the element type for array properties.
func resolveArrayType(prop map[string]any, schemas map[string]any) string {
	items, ok := prop["items"].(map[string]any)
	if !ok {
		return "array"
	}
	if ref, ok := items["$ref"].(string); ok {
		return refToName(ref) + "[]"
	}
	if t, ok := items["type"].(string); ok {
		return t + "[]"
	}
	return "array"
}

// mapType converts JSON Schema types to simpler type strings.
func mapType(jsonType string, prop map[string]any) string {
	switch jsonType {
	case "integer":
		return "integer"
	case "number":
		return "number"
	case "boolean":
		return "boolean"
	case "string":
		if format, ok := prop["format"].(string); ok {
			if format == "date" || format == "date-time" {
				return "date"
			}
		}
		return "string"
	case "object":
		return "object"
	case "":
		return "string"
	default:
		return jsonType
	}
}

// refToName extracts and cleans the schema name from a $ref string like
// "#/components/schemas/AddressesEmbedded". Internal Python-namespaced
// schemas (e.g., "api__app__models__permits__AddressesRead") are reduced
// to their final component (e.g., "AddressesRead").
func refToName(ref string) string {
	parts := strings.Split(ref, "/")
	name := parts[len(parts)-1]
	// Clean up internal Python-namespaced schema names.
	if strings.Contains(name, "__") {
		segments := strings.Split(name, "__")
		name = segments[len(segments)-1]
	}
	return name
}

// mergeFields applies overrides to base fields. Only fields present in
// base are modified; unknown override fields are ignored.
func mergeFields(base map[string]schemaField, overrides map[string]overrideField) {
	for name, override := range overrides {
		f, ok := base[name]
		if !ok {
			continue
		}
		if override.Description != "" {
			f.Description = override.Description
		}
		if override.Unit != "" {
			f.Unit = override.Unit
		}
		if override.Range != "" {
			f.Range = override.Range
		}
		if override.Enum != "" {
			f.Enum = override.Enum
		}
		base[name] = f
	}
}

// buildFieldIndex creates the jq-style field path index for a command.
func buildFieldIndex(fields map[string]schemaField) []string {
	var index []string
	for name := range fields {
		index = append(index, "data[]."+name)
	}
	sort.Strings(index)

	// Add standard meta fields.
	index = append(index, "meta.count", "meta.has_more", "meta.credits_used", "meta.credits_remaining")
	return index
}

// addSearchFilters populates the shared search filters that registerSearchFlags
// exposes on both permits search and contractors search. Each entry mirrors
// a flag registered in cmd/searchflags.go to prevent schema drift.
func addSearchFilters(filters map[string]schemaField) {
	// Required filters
	filters["--geo-id"] = schemaField{Type: "string", Description: "Geographic area: zip code, state abbreviation, or resolved Shovels geo_id"}
	filters["--permit-from"] = schemaField{Type: "date", Description: "Start date in YYYY-MM-DD format"}
	filters["--permit-to"] = schemaField{Type: "date", Description: "End date in YYYY-MM-DD format"}

	// Permit filters
	filters["--tags"] = schemaField{Type: "string[]", Description: "Permit tags, AND logic, prefix with - to exclude"}
	filters["--query"] = schemaField{Type: "string", Description: "Substring search in permit description, max 50 chars"}
	filters["--status"] = schemaField{Type: "string[]", Description: "Permit status: final, in_review, inactive, active"}
	filters["--min-approval-duration"] = schemaField{Type: "integer", Description: "Minimum approval duration in days"}
	filters["--min-construction-duration"] = schemaField{Type: "integer", Description: "Minimum construction duration in days"}
	filters["--min-inspection-pr"] = schemaField{Type: "integer", Description: "Minimum inspection pass rate, 0-100"}
	filters["--min-job-value"] = schemaField{Type: "integer", Description: "Minimum job value in cents (5000000 = $50,000)", Unit: "cents"}
	filters["--min-fees"] = schemaField{Type: "integer", Description: "Minimum permit fees in cents (100000 = $1,000)", Unit: "cents"}

	// Property filters
	filters["--property-type"] = schemaField{Type: "string", Description: "Property type: residential, commercial, industrial"}
	filters["--property-min-market-value"] = schemaField{Type: "integer", Description: "Minimum assessed market value in cents (50000000 = $500,000)", Unit: "cents"}
	filters["--property-min-building-area"] = schemaField{Type: "integer", Description: "Minimum building area in square feet"}
	filters["--property-min-lot-size"] = schemaField{Type: "integer", Description: "Minimum lot size in square feet"}
	filters["--property-min-story-count"] = schemaField{Type: "integer", Description: "Minimum number of stories"}
	filters["--property-min-unit-count"] = schemaField{Type: "integer", Description: "Minimum number of units"}

	// Contractor filters
	filters["--contractor-classification"] = schemaField{Type: "string[]", Description: "Contractor classification, AND logic, prefix with - to exclude"}
	filters["--contractor-name"] = schemaField{Type: "string", Description: "Filter by contractor name or partial name"}
	filters["--contractor-website"] = schemaField{Type: "string", Description: "Filter by contractor website domain"}
	filters["--contractor-min-total-job-value"] = schemaField{Type: "integer", Description: "Minimum lifetime contractor job value in cents (10000000 = $100,000)", Unit: "cents"}
	filters["--contractor-min-total-permits-count"] = schemaField{Type: "integer", Description: "Minimum lifetime permits count"}
	filters["--contractor-min-inspection-pr"] = schemaField{Type: "integer", Description: "Minimum lifetime inspection pass rate, 0-100"}
	filters["--contractor-license"] = schemaField{Type: "string", Description: "Filter by contractor license number"}

	// Response options
	filters["--include-count"] = schemaField{Type: "boolean", Description: "Request total result count in meta.total_count"}
}

// buildFilters creates the filter definitions for a command based on its type.
func buildFilters(def commandDef) map[string]schemaField {
	filters := make(map[string]schemaField)

	switch def.FiltersFrom {
	case "permits_search":
		addSearchFilters(filters)
		filters["--has-contractor"] = schemaField{Type: "boolean", Description: "Include only permits linked to a known contractor"}
	case "contractors_search":
		addSearchFilters(filters)
		filters["--no-tallies"] = schemaField{Type: "boolean", Description: "Omit tag_tally and status_tally arrays for faster response. Warning: tallies are the only contractor search fields filtered by your date/geo/tag query — all other permit counts (permit_count, etc.) are lifetime global totals"}
	case "get":
		filters["ID"] = schemaField{Type: "string", Description: "One or more IDs as positional arguments (max 50)"}
	case "geo_search":
		filters["--query"] = schemaField{Type: "string", Description: "Search query string"}
	case "metrics_prop":
		filters["GEO_ID"] = schemaField{Type: "string", Description: "Geographic ID as positional argument"}
		filters["--tag"] = schemaField{Type: "string", Description: "Permit tag: solar, roofing, electrical, etc."}
		filters["--property-type"] = schemaField{Type: "string", Description: "Property type: residential, commercial, industrial"}
		filters["--include-count"] = schemaField{Type: "boolean", Description: "Request total result count in meta.total_count"}
	case "metrics_prop_monthly":
		filters["GEO_ID"] = schemaField{Type: "string", Description: "Geographic ID as positional argument"}
		filters["--tag"] = schemaField{Type: "string", Description: "Permit tag: solar, roofing, electrical, etc."}
		filters["--property-type"] = schemaField{Type: "string", Description: "Property type: residential, commercial, industrial"}
		filters["--metric-from"] = schemaField{Type: "date", Description: "Start date in YYYY-MM-DD format"}
		filters["--metric-to"] = schemaField{Type: "date", Description: "End date in YYYY-MM-DD format"}
		filters["--include-count"] = schemaField{Type: "boolean", Description: "Request total result count in meta.total_count"}
	case "metrics_noprop":
		filters["GEO_ID"] = schemaField{Type: "string", Description: "Geographic ID as positional argument"}
		filters["--tag"] = schemaField{Type: "string", Description: "Permit tag: solar, roofing, electrical, etc."}
		filters["--include-count"] = schemaField{Type: "boolean", Description: "Request total result count in meta.total_count"}
	case "metrics_noprop_monthly":
		filters["GEO_ID"] = schemaField{Type: "string", Description: "Geographic ID as positional argument"}
		filters["--tag"] = schemaField{Type: "string", Description: "Permit tag: solar, roofing, electrical, etc."}
		filters["--metric-from"] = schemaField{Type: "date", Description: "Start date in YYYY-MM-DD format"}
		filters["--metric-to"] = schemaField{Type: "date", Description: "End date in YYYY-MM-DD format"}
		filters["--include-count"] = schemaField{Type: "boolean", Description: "Request total result count in meta.total_count"}
	case "contractor_metrics":
		filters["ID"] = schemaField{Type: "string", Description: "Contractor ID as positional argument"}
		filters["--tag"] = schemaField{Type: "string", Description: "Permit tag: solar, roofing, electrical, etc."}
		filters["--property-type"] = schemaField{Type: "string", Description: "Property type: residential, commercial, industrial"}
		filters["--metric-from"] = schemaField{Type: "date", Description: "Start date in YYYY-MM-DD format"}
		filters["--metric-to"] = schemaField{Type: "date", Description: "End date in YYYY-MM-DD format"}
	case "none":
		// No filters beyond pagination globals.
	}

	return filters
}

// writeSchemaData generates the schema_data.go file.
func writeSchemaData(path string, schemas map[string]commandSchemaData) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintf(f, "// Code generated by schema_gen.go; DO NOT EDIT.\n\n")
	fmt.Fprintf(f, "package cmd\n\n")
	fmt.Fprintf(f, "func init() {\n")
	fmt.Fprintf(f, "\tschemaRegistry = map[string]CommandSchema{\n")

	// Sort command names for deterministic output.
	names := make([]string, 0, len(schemas))
	for name := range schemas {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		data := schemas[name]
		fmt.Fprintf(f, "\t\t%q: {\n", name)
		fmt.Fprintf(f, "\t\t\tSchemaVersion: 1,\n")
		fmt.Fprintf(f, "\t\t\tCommand:       %q,\n", name)

		// Response fields
		fmt.Fprintf(f, "\t\t\tResponseFields: map[string]SchemaField{\n")
		fieldNames := sortedKeys(data.ResponseFields)
		for _, fn := range fieldNames {
			field := data.ResponseFields[fn]
			fmt.Fprintf(f, "\t\t\t\t%q: {Type: %q", fn, field.Type)
			if field.Description != "" {
				fmt.Fprintf(f, ", Description: %q", field.Description)
			}
			if field.Unit != "" {
				fmt.Fprintf(f, ", Unit: %q", field.Unit)
			}
			if field.Range != "" {
				fmt.Fprintf(f, ", Range: %q", field.Range)
			}
			if field.Enum != "" {
				fmt.Fprintf(f, ", Enum: %q", field.Enum)
			}
			fmt.Fprintf(f, "},\n")
		}
		fmt.Fprintf(f, "\t\t\t},\n")

		// Field index
		fmt.Fprintf(f, "\t\t\tFieldIndex: []string{\n")
		for _, idx := range data.FieldIndex {
			fmt.Fprintf(f, "\t\t\t\t%q,\n", idx)
		}
		fmt.Fprintf(f, "\t\t\t},\n")

		// Filters
		fmt.Fprintf(f, "\t\t\tFilters: map[string]SchemaField{\n")
		filterNames := sortedKeys(data.Filters)
		for _, fn := range filterNames {
			filter := data.Filters[fn]
			fmt.Fprintf(f, "\t\t\t\t%q: {Type: %q", fn, filter.Type)
			if filter.Description != "" {
				fmt.Fprintf(f, ", Description: %q", filter.Description)
			}
			if filter.Unit != "" {
				fmt.Fprintf(f, ", Unit: %q", filter.Unit)
			}
			fmt.Fprintf(f, "},\n")
		}
		fmt.Fprintf(f, "\t\t\t},\n")

		fmt.Fprintf(f, "\t\t},\n")
	}

	fmt.Fprintf(f, "\t}\n")
	fmt.Fprintf(f, "}\n")

	return nil
}

func sortedKeys(m map[string]schemaField) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
