//go:build ignore

// Schema generator: reads the OpenAPI spec and merges YAML overrides to
// produce schema_data.go with embedded schema data for all CLI commands.
// Runs from the cmd/ directory via go generate. The spec comes from
// SCHEMA_GEN_SPEC_FILE when that is set, and from the live API otherwise.
//
// Three checked-in artifacts are pinned against each other — the spec copy
// this reads, the schema data it writes, and the --schema output the binary
// then prints — so refreshing the schemas is one sequence, run from the
// module root:
//
//	# 1. refresh the pinned spec
//	curl -s https://api.shovels.ai/v2/openapi.json > cmd/testdata/openapi.json
//	# 2. regenerate the embedded schema data from it
//	SCHEMA_GEN_SPEC_FILE="$PWD/cmd/testdata/openapi.json" go generate ./cmd/...
//	# 3. repin every command's --schema output, then review that diff
//	go test -tags=e2e ./e2e/... -run TestSchemaOutputMatchesGolden -update-schema-golden
//
// Step 2 on its own reads the live spec rather than the pinned copy, which
// leaves the two disagreeing whenever the API has moved on.
// e2e/schema_generate_test.go is what fails when they do.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
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

	// ExpandPaths names response fields whose object children are documented
	// individually as dotted child paths. Expansion is opt-in per command:
	// most nested references (a permit's address, a contractor's address) are
	// self-explanatory under their schema name and read better as one entry
	// than as a dozen flattened ones.
	ExpandPaths []string

	// MetaArrays names arrays the CLI adds to its own meta envelope, each
	// documented from the OpenAPI schema of a single element. These have no
	// property in any endpoint response schema — only the element shape is
	// the API's.
	MetaArrays []metaArrayDef
}

// metaArrayDef ties one meta envelope array to the OpenAPI schema describing
// one of its elements.
type metaArrayDef struct {
	Name   string // meta key, e.g. "trust_summaries"
	Schema string // OpenAPI schema name of one element
}

// allCommands defines every data command that needs a schema entry.
// ResponseSchema values must match schema names in the OpenAPI spec's
// components/schemas section.
var allCommands = []commandDef{
	{Command: "permits search", ResponseSchema: "PermitsRead", Endpoint: "/permits/search", FiltersFrom: "permits_search"},
	{Command: "permits get", ResponseSchema: "PermitsRead", Endpoint: "/permits", FiltersFrom: "get"},
	{
		Command: "properties search", ResponseSchema: "PropertiesRead", Endpoint: "/properties/search", FiltersFrom: "properties_search",
		ExpandPaths: []string{"trust"},
		MetaArrays:  []metaArrayDef{{Name: "trust_summaries", Schema: "TrustSummary"}},
	},
	{
		Command: "properties get", ResponseSchema: "PropertiesRead", Endpoint: "/properties", FiltersFrom: "get",
		ExpandPaths: []string{"trust"},
	},
	{Command: "decisions search", ResponseSchema: "DecisionsRead", Endpoint: "/decisions/search", FiltersFrom: "decisions_search"},
	{Command: "decisions get", ResponseSchema: "DecisionsRead", Endpoint: "/decisions", FiltersFrom: "get"},
	{Command: "contractors search", ResponseSchema: "ContractorsRead", Endpoint: "/contractors/search", FiltersFrom: "contractors_search"},
	{Command: "contractors get", ResponseSchema: "ContractorsRead", Endpoint: "/contractors", FiltersFrom: "get"},
	{Command: "contractors permits", ResponseSchema: "PermitsRead", Endpoint: "/contractors/{id}/permits", FiltersFrom: "none"},
	{Command: "contractors employees", ResponseSchema: "Employees", Endpoint: "/contractors/{id}/employees", FiltersFrom: "none"},
	{Command: "contractors metrics", ResponseSchema: "ContractorsMetricsMonthlyRead", Endpoint: "/contractors/{id}/metrics", FiltersFrom: "contractor_metrics"},
	{Command: "cities search", ResponseSchema: "GeoEntitiesRead", Endpoint: "/cities/search", FiltersFrom: "geo_search"},
	{Command: "cities metrics current", ResponseSchema: "CitiesMetricsCurrentRead", Endpoint: "/cities/{geo_id}/metrics/current", FiltersFrom: "metrics_prop"},
	{Command: "cities metrics monthly", ResponseSchema: "CitiesMetricsMonthlyRead", Endpoint: "/cities/{geo_id}/metrics/monthly", FiltersFrom: "metrics_prop_monthly"},
	{Command: "counties search", ResponseSchema: "GeoEntitiesRead", Endpoint: "/counties/search", FiltersFrom: "geo_search"},
	{Command: "counties metrics current", ResponseSchema: "CountiesMetricsCurrentRead", Endpoint: "/counties/{geo_id}/metrics/current", FiltersFrom: "metrics_prop"},
	{Command: "counties metrics monthly", ResponseSchema: "CountiesMetricsMonthlyRead", Endpoint: "/counties/{geo_id}/metrics/monthly", FiltersFrom: "metrics_prop_monthly"},
	{Command: "jurisdictions search", ResponseSchema: "GeoEntitiesRead", Endpoint: "/jurisdictions/search", FiltersFrom: "geo_search"},
	{Command: "jurisdictions metrics current", ResponseSchema: "JurisdictionsMetricsCurrentRead", Endpoint: "/jurisdictions/{geo_id}/metrics/current", FiltersFrom: "metrics_prop"},
	{Command: "jurisdictions metrics monthly", ResponseSchema: "JurisdictionsMetricsMonthlyRead", Endpoint: "/jurisdictions/{geo_id}/metrics/monthly", FiltersFrom: "metrics_prop_monthly"},
	{Command: "addresses search", ResponseSchema: "api__app__models__geo__AddressesRead", Endpoint: "/addresses/search", FiltersFrom: "geo_search"},
	{Command: "addresses metrics current", ResponseSchema: "AddressesMetricsCurrentRead", Endpoint: "/addresses/{geo_id}/metrics/current", FiltersFrom: "metrics_noprop"},
	{Command: "addresses metrics monthly", ResponseSchema: "AddressesMetricsMonthlyRead", Endpoint: "/addresses/{geo_id}/metrics/monthly", FiltersFrom: "metrics_noprop_monthly"},
	{Command: "addresses residents", ResponseSchema: "ResidentsRead", Endpoint: "/addresses/{geo_id}/residents", FiltersFrom: "none"},
	{Command: "zipcodes search", ResponseSchema: "Zipcodes", Endpoint: "/zipcodes/search", FiltersFrom: "geo_search"},
	{Command: "states search", ResponseSchema: "States", Endpoint: "/states/search", FiltersFrom: "geo_search"},
	{Command: "cities coverage", ResponseSchema: "CoverageItem", Endpoint: "/meta/coverage", FiltersFrom: "coverage"},
	{Command: "counties coverage", ResponseSchema: "CoverageItem", Endpoint: "/meta/coverage", FiltersFrom: "coverage"},
	{Command: "jurisdictions coverage", ResponseSchema: "CoverageItem", Endpoint: "/meta/coverage", FiltersFrom: "coverage"},
	{Command: "states coverage", ResponseSchema: "CoverageItem", Endpoint: "/meta/coverage", FiltersFrom: "coverage"},
	{Command: "zipcodes coverage", ResponseSchema: "CoverageItem", Endpoint: "/meta/coverage", FiltersFrom: "coverage"},
	{Command: "tags list", ResponseSchema: "Tags", Endpoint: "/list/tags", FiltersFrom: "none"},
}

func main() {
	spec, err := loadSpec()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load OpenAPI spec: %v\n", err)
		os.Exit(1)
	}

	overrides, err := loadOverrides("schema_overrides.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load overrides: %v\n", err)
		os.Exit(1)
	}

	components := componentSchemas(spec)

	schemas := make(map[string]commandSchemaData)
	for _, def := range allCommands {
		data, err := buildCommandSchema(components, overrides, def)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		schemas[def.Command] = data
	}

	if err := writeSchemaData("schema_data.go", schemas); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write schema_data.go: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Generated schema_data.go with %d command schemas\n", len(schemas))
}

type commandSchemaData struct {
	ResponseFields map[string]schemaField
	MetaFields     map[string]schemaField
	FieldIndex     []string
	Filters        map[string]schemaField
}

// buildCommandSchema assembles one command's schema data from the spec's
// component schemas and the hand-written overrides.
func buildCommandSchema(components map[string]any, overrides map[string]overrideCommand, def commandDef) (commandSchemaData, error) {
	fields := extractFields(components, def.ResponseSchema)
	if err := checkSchemaResolved(def, fields); err != nil {
		return commandSchemaData{}, err
	}
	if err := expandNested(components, def, fields); err != nil {
		return commandSchemaData{}, err
	}
	metaFields, err := buildMetaFields(components, def)
	if err != nil {
		return commandSchemaData{}, err
	}
	if cmdOverride, ok := overrides[def.Command]; ok {
		mergeFields(fields, cmdOverride.Fields)
	}

	return commandSchemaData{
		ResponseFields: fields,
		MetaFields:     metaFields,
		FieldIndex:     buildFieldIndex(fields, metaFields, def),
		Filters:        buildFilters(def),
	}, nil
}

// checkSchemaResolved enforces the loud-fail guard: a command whose named
// response schema resolves to zero fields (typo, renamed, or removed schema)
// must fail generation rather than silently emit an empty schema. The error
// names both the command and the schema so the mismatch is actionable.
func checkSchemaResolved(def commandDef, fields map[string]schemaField) error {
	if len(fields) == 0 {
		return fmt.Errorf("command %q response schema %q resolved to zero fields", def.Command, def.ResponseSchema)
	}
	return nil
}

// loadSpec returns the OpenAPI spec. By default it fetches the live spec over
// HTTP; if SCHEMA_GEN_SPEC_FILE is set it reads a local JSON file instead,
// allowing offline, deterministic generator tests.
func loadSpec() (map[string]any, error) {
	if path := os.Getenv("SCHEMA_GEN_SPEC_FILE"); path != "" {
		return readOpenAPIFile(path)
	}
	return fetchOpenAPI(openAPIURL)
}

// readOpenAPIFile parses an OpenAPI JSON spec from a local file.
func readOpenAPIFile(path string) (map[string]any, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	var spec map[string]any
	if err := json.Unmarshal(body, &spec); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	return spec, nil
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

// componentSchemas returns the spec's components/schemas map, which every
// schema name in this generator resolves against.
func componentSchemas(spec map[string]any) map[string]any {
	components, ok := spec["components"].(map[string]any)
	if !ok {
		return nil
	}
	schemas, _ := components["schemas"].(map[string]any)
	return schemas
}

// schemaProperties returns the properties of a named component schema, or nil
// when the schema is absent or carries no properties.
func schemaProperties(components map[string]any, schemaName string) map[string]any {
	schema, ok := components[schemaName].(map[string]any)
	if !ok {
		return nil
	}
	properties, _ := schema["properties"].(map[string]any)
	return properties
}

// fieldFromProperty derives one schema field from an OpenAPI property,
// preferring the property's description and falling back to its title.
func fieldFromProperty(prop, components map[string]any) schemaField {
	f := schemaField{Type: resolveType(prop, components)}
	if desc, ok := prop["description"].(string); ok {
		f.Description = desc
	}
	if title, ok := prop["title"].(string); ok && f.Description == "" {
		f.Description = title
	}
	return f
}

// extractFields pulls response field definitions from the OpenAPI spec for a
// given schema name. It resolves $ref pointers within the components/schemas
// section and maps JSON Schema types to simpler type strings.
func extractFields(components map[string]any, schemaName string) map[string]schemaField {
	fields := make(map[string]schemaField)
	for name, propRaw := range schemaProperties(components, schemaName) {
		prop, ok := propRaw.(map[string]any)
		if !ok {
			continue
		}
		fields[name] = fieldFromProperty(prop, components)
	}
	return fields
}

// expandNested documents the direct children of each opted-in response field
// as dotted child paths, so an agent reading the schema learns the shape of a
// nested object without a second lookup. Expansion goes exactly one level: a
// child that is itself a reference keeps its schema name as its type and is
// not expanded in turn. The parent entry stays, typed "object", since a JSON
// consumer learns more from the child entries than from the schema name the
// reference carries.
func expandNested(components map[string]any, def commandDef, fields map[string]schemaField) error {
	properties := schemaProperties(components, def.ResponseSchema)
	for _, path := range def.ExpandPaths {
		prop, ok := properties[path].(map[string]any)
		if !ok {
			return fmt.Errorf("command %q expand path %q is not a field of schema %q", def.Command, path, def.ResponseSchema)
		}
		children := schemaProperties(components, nestedSchemaRef(prop))
		if len(children) == 0 {
			return fmt.Errorf("command %q expand path %q resolved to zero nested fields", def.Command, path)
		}
		for name, childRaw := range children {
			child, ok := childRaw.(map[string]any)
			if !ok {
				continue
			}
			fields[path+"."+name] = fieldFromProperty(child, components)
		}
		parent := fields[path]
		parent.Type = "object"
		fields[path] = parent
	}
	return nil
}

// buildMetaFields documents the elements of each meta envelope array the
// command adds. Commands that add none get a nil map, which the schema output
// omits entirely.
func buildMetaFields(components map[string]any, def commandDef) (map[string]schemaField, error) {
	if len(def.MetaArrays) == 0 {
		return nil, nil
	}
	meta := make(map[string]schemaField)
	for _, arr := range def.MetaArrays {
		elements := schemaProperties(components, arr.Schema)
		if len(elements) == 0 {
			return nil, fmt.Errorf("command %q meta array %q schema %q resolved to zero fields", def.Command, arr.Name, arr.Schema)
		}
		for name, elemRaw := range elements {
			elem, ok := elemRaw.(map[string]any)
			if !ok {
				continue
			}
			meta[arr.Name+"[]."+name] = fieldFromProperty(elem, components)
		}
	}
	return meta, nil
}

// nestedSchemaRef returns the component schema name a property points at,
// covering the plain, nullable (anyOf/oneOf), and allOf spellings. Unlike
// refToName it keeps the full name, which is what indexes components/schemas.
// It returns "" when the property is not a reference.
func nestedSchemaRef(prop map[string]any) string {
	if ref, ok := prop["$ref"].(string); ok {
		return refBase(ref)
	}
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		variants, ok := prop[key].([]any)
		if !ok {
			continue
		}
		for _, v := range variants {
			vm, ok := v.(map[string]any)
			if !ok {
				continue
			}
			if ref, ok := vm["$ref"].(string); ok {
				return refBase(ref)
			}
		}
	}
	return ""
}

// refBase returns the final path segment of a $ref pointer.
func refBase(ref string) string {
	parts := strings.Split(ref, "/")
	return parts[len(parts)-1]
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
	name := refBase(ref)
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

// buildFieldIndex creates the jq-style field path index for a command. The
// meta fields appended depend on the command's response shape:
//   - coverage commands are non-paginated and credit-exempt (empty meta
//     envelope), so no meta fields are added.
//   - batch-get commands return a non-paginated batch envelope with count,
//     credits, and a missing list of unresolved IDs, and never has_more.
//   - all other commands are paginated, with count, has_more, and credits.
//
// Any meta fields the command adds beyond that standard set follow, sorted.
func buildFieldIndex(fields, metaFields map[string]schemaField, def commandDef) []string {
	var index []string
	for name := range fields {
		index = append(index, "data[]."+name)
	}
	sort.Strings(index)

	switch def.FiltersFrom {
	case "coverage":
		// Non-paginated, credit-exempt: empty meta envelope.
	case "get":
		index = append(index, "meta.count", "meta.missing", "meta.credits_used", "meta.credits_remaining")
	default:
		index = append(index, "meta.count", "meta.has_more", "meta.credits_used", "meta.credits_remaining")
	}

	var extra []string
	for name := range metaFields {
		extra = append(extra, "meta."+name)
	}
	sort.Strings(extra)
	return append(index, extra...)
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
	filters["--property-type"] = schemaField{Type: "string", Description: "Property type: residential, commercial, industrial, agricultural, vacant land, exempt, miscellaneous, office, recreational"}
	filters["--property-min-market-value"] = schemaField{Type: "integer", Description: "Minimum assessed market value in cents (50000000 = $500,000)", Unit: "cents"}
	filters["--property-min-building-area"] = schemaField{Type: "integer", Description: "Minimum building area in square feet"}
	filters["--property-min-lot-size"] = schemaField{Type: "integer", Description: "Minimum lot size in square feet"}
	filters["--property-min-story-count"] = schemaField{Type: "integer", Description: "Minimum number of stories"}
	filters["--property-min-unit-count"] = schemaField{Type: "integer", Description: "Minimum number of units"}

	// Contractor filters
	filters["--contractor-classification"] = schemaField{Type: "string[]", Description: "Contractor classification, AND logic, prefix with - to exclude. Valid values: concrete_and_paving, demolition_and_excavation, electrical, fencing_and_glazing, framing_and_carpentry, general_building_contractor, general_engineering_contractor, hvac, landscaping_and_outdoor_work, other, plumbing, roofing, specialty_trades"}
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
	case "decisions_search":
		// Required filters
		filters["--geo-id"] = schemaField{Type: "string", Description: "Geographic area: 2-letter state code or a resolved Shovels geo_id. ZIP codes are not supported for decisions"}
		filters["--decision-from"] = schemaField{Type: "date", Description: "Start date in YYYY-MM-DD format"}
		filters["--decision-to"] = schemaField{Type: "date", Description: "End date in YYYY-MM-DD format"}

		// Decision filters
		filters["--asset-class"] = schemaField{Type: "string[]", Description: "Asset class, repeat or comma-separate for multiple (e.g. Residential, Commercial)"}
		filters["--category"] = schemaField{Type: "string[]", Description: "Decision category, repeat or comma-separate for multiple (e.g. Rezoning, Variance)"}
		filters["--subcategory"] = schemaField{Type: "string[]", Description: "Decision subcategory, repeat or comma-separate for multiple"}
		filters["--property-type"] = schemaField{Type: "string[]", Description: "Property type, repeat or comma-separate for multiple"}
		filters["--min-project-value"] = schemaField{Type: "integer", Description: "Minimum project value in cents (100000000 = $1,000,000)", Unit: "cents"}
		filters["--max-project-value"] = schemaField{Type: "integer", Description: "Maximum project value in cents (100000000 = $1,000,000)", Unit: "cents"}
		filters["--query"] = schemaField{Type: "string", Description: "Substring search in decision text, case-insensitive, max 100 characters"}

		// Response options
		filters["--include-count"] = schemaField{Type: "boolean", Description: "Request total result count in meta.total_count"}
	case "properties_search":
		// Required scope: at least one of geo-id / legal-owner.
		filters["--geo-id"] = schemaField{Type: "string", Description: "Geographic scope: 5-digit zip code, zip+4, 2-letter state code, or a resolved Shovels city, county, or address geo_id. Jurisdiction geo_ids are rejected by this endpoint. Required unless --legal-owner is given"}
		filters["--legal-owner"] = schemaField{Type: "string[]", Description: "Property legal owner, repeat the flag for up to 10 owners. Values are never split on commas, so \"SMITH, JOHN\" is one owner. Without --geo-id this searches the owner nationwide"}

		// Permit filters
		filters["--permit-tags"] = schemaField{Type: "string[]", Description: "Canonical permit tags, repeat or comma-separate for multiple. A bare tag keeps properties that have it, a - prefix keeps properties WITHOUT it. Several positive tags require every tag, though not all on the same permit. An unknown tag is rejected with an error rather than returning an empty result: run tags list --limit all for the canonical set"}
		filters["--permit-status"] = schemaField{Type: "string[]", Description: "Permit status, repeat or comma-separate for multiple: final, in_review, inactive, active"}
		filters["--permit-from"] = schemaField{Type: "date", Description: "Bind the tag, status, and absence filters to this date in YYYY-MM-DD format. This endpoint has no --permit-to: use permits search for a closed date window"}
		filters["--permit-tags-unfinaled"] = schemaField{Type: "string[]", Description: "Keep properties with an unfinaled permit of each named tag, repeat or comma-separate for multiple (e.g. solar,roofing)"}

		// Property filters. Attribute data covers roughly 60-70% of
		// properties, so each of these narrows results to the covered set.
		filters["--property-type"] = schemaField{Type: "string[]", Description: "Property type, repeat or comma-separate for multiple: residential, commercial, industrial, agricultural, vacant land, exempt, miscellaneous, office, recreational"}
		filters["--property-min-market-value"] = schemaField{Type: "integer", Description: "Minimum assessed market value in integer cents (50000000 = $500,000)", Unit: "cents"}
		filters["--property-max-market-value"] = schemaField{Type: "integer", Description: "Maximum assessed market value in integer cents (100000000 = $1,000,000)", Unit: "cents"}
		filters["--property-min-lot-size"] = schemaField{Type: "integer", Description: "Minimum lot size in square feet", Unit: "square feet"}
		filters["--property-max-lot-size"] = schemaField{Type: "integer", Description: "Maximum lot size in square feet", Unit: "square feet"}
		filters["--property-min-building-area"] = schemaField{Type: "integer", Description: "Minimum building area in square feet", Unit: "square feet"}
		filters["--property-max-building-area"] = schemaField{Type: "integer", Description: "Maximum building area in square feet", Unit: "square feet"}
		filters["--property-min-unit-count"] = schemaField{Type: "integer", Description: "Minimum number of units"}
		filters["--property-max-unit-count"] = schemaField{Type: "integer", Description: "Maximum number of units"}
		filters["--property-min-year-built"] = schemaField{Type: "integer", Description: "Minimum year built (e.g. 1990)"}
		filters["--property-max-year-built"] = schemaField{Type: "integer", Description: "Maximum year built (e.g. 1990)"}

		// Response options
		filters["--include-count"] = schemaField{Type: "boolean", Description: "Request total result count in meta.total_count, capped at 10,000. Omitted from meta when the count query times out"}
	case "get":
		filters["ID"] = schemaField{Type: "string", Description: "One or more IDs as positional arguments (max 50)"}
	case "geo_search":
		filters["--query"] = schemaField{Type: "string", Description: "Search query string"}
	case "metrics_prop":
		filters["GEO_ID"] = schemaField{Type: "string", Description: "Geographic ID as positional argument"}
		filters["--tag"] = schemaField{Type: "string", Description: "Permit tag: solar, roofing, electrical, etc."}
		filters["--property-type"] = schemaField{Type: "string", Description: "Property type: residential, commercial, industrial, agricultural, vacant land, exempt, miscellaneous, office, recreational"}
		filters["--include-count"] = schemaField{Type: "boolean", Description: "Request total result count in meta.total_count"}
	case "metrics_prop_monthly":
		filters["GEO_ID"] = schemaField{Type: "string", Description: "Geographic ID as positional argument"}
		filters["--tag"] = schemaField{Type: "string", Description: "Permit tag: solar, roofing, electrical, etc."}
		filters["--property-type"] = schemaField{Type: "string", Description: "Property type: residential, commercial, industrial, agricultural, vacant land, exempt, miscellaneous, office, recreational"}
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
		filters["--property-type"] = schemaField{Type: "string", Description: "Property type: residential, commercial, industrial, agricultural, vacant land, exempt, miscellaneous, office, recreational"}
		filters["--metric-from"] = schemaField{Type: "date", Description: "Start date in YYYY-MM-DD format"}
		filters["--metric-to"] = schemaField{Type: "date", Description: "End date in YYYY-MM-DD format"}
	case "coverage":
		filters["GEO_ID"] = schemaField{Type: "string", Description: "Geographic ID as positional argument"}
		filters["--coverage-from"] = schemaField{Type: "date", Description: "Start date in YYYY-MM-DD format"}
		filters["--coverage-to"] = schemaField{Type: "date", Description: "End date in YYYY-MM-DD format"}
	case "none":
		// No filters beyond pagination globals.
	}

	return filters
}

// writeSchemaData generates the schema_data.go file.
func writeSchemaData(path string, schemas map[string]commandSchemaData) error {
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "// Code generated by schema_gen.go; DO NOT EDIT.\n\n")
	fmt.Fprintf(&buf, "package cmd\n\n")
	fmt.Fprintf(&buf, "func init() {\n")
	fmt.Fprintf(&buf, "\tschemaRegistry = map[string]CommandSchema{\n")

	// Sort command names for deterministic output.
	names := make([]string, 0, len(schemas))
	for name := range schemas {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		data := schemas[name]
		fmt.Fprintf(&buf, "\t\t%q: {\n", name)
		fmt.Fprintf(&buf, "\t\t\tSchemaVersion: 1,\n")
		fmt.Fprintf(&buf, "\t\t\tCommand:       %q,\n", name)

		writeFieldMap(&buf, "ResponseFields", data.ResponseFields)

		// A command with no meta additions emits no MetaFields entry, which
		// keeps meta_fields out of its schema output entirely.
		if len(data.MetaFields) > 0 {
			writeFieldMap(&buf, "MetaFields", data.MetaFields)
		}

		// Field index
		fmt.Fprintf(&buf, "\t\t\tFieldIndex: []string{\n")
		for _, idx := range data.FieldIndex {
			fmt.Fprintf(&buf, "\t\t\t\t%q,\n", idx)
		}
		fmt.Fprintf(&buf, "\t\t\t},\n")

		writeFieldMap(&buf, "Filters", data.Filters)

		fmt.Fprintf(&buf, "\t\t},\n")
	}

	fmt.Fprintf(&buf, "\t}\n")
	fmt.Fprintf(&buf, "}\n")

	// Apply gofmt-compatible formatting to the generated source.
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return fmt.Errorf("gofmt failed on generated code: %w", err)
	}

	return os.WriteFile(path, formatted, 0644)
}

// writeFieldMap emits one named map[string]SchemaField literal, keys sorted
// so regeneration is deterministic.
func writeFieldMap(buf *bytes.Buffer, name string, fields map[string]schemaField) {
	fmt.Fprintf(buf, "\t\t\t%s: map[string]SchemaField{\n", name)
	for _, key := range sortedKeys(fields) {
		field := fields[key]
		fmt.Fprintf(buf, "\t\t\t\t%q: {Type: %q", key, field.Type)
		if field.Description != "" {
			fmt.Fprintf(buf, ", Description: %q", field.Description)
		}
		if field.Unit != "" {
			fmt.Fprintf(buf, ", Unit: %q", field.Unit)
		}
		if field.Range != "" {
			fmt.Fprintf(buf, ", Range: %q", field.Range)
		}
		if field.Enum != "" {
			fmt.Fprintf(buf, ", Enum: %q", field.Enum)
		}
		fmt.Fprintf(buf, "},\n")
	}
	fmt.Fprintf(buf, "\t\t\t},\n")
}

func sortedKeys(m map[string]schemaField) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
