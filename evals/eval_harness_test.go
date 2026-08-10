//go:build eval

package evals

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestAgentReportSchemaIsValidJSON(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(agentReportSchema), &schema); err != nil {
		t.Fatalf("agent report schema is invalid JSON: %v", err)
	}
}

func TestAgentReportSchemaMatchesGoTypes(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(agentReportSchema), &schema); err != nil {
		t.Fatalf("agent report schema is invalid JSON: %v", err)
	}

	assertSchemaMatchesType(t, schema, reflect.TypeOf(AgentReport{}))
	properties := schema["properties"].(map[string]any)
	steps := properties["steps"].(map[string]any)
	assertSchemaMatchesType(t, steps["items"].(map[string]any), reflect.TypeOf(AgentStep{}))
	issues := properties["issues"].(map[string]any)
	assertSchemaMatchesType(t, issues["items"].(map[string]any), reflect.TypeOf(AgentIssue{}))
}

func TestAgentReportSchemaDefinesReproducibleFinalEvidence(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(agentReportSchema), &schema); err != nil {
		t.Fatalf("agent report schema is invalid JSON: %v", err)
	}

	properties := schema["properties"].(map[string]any)
	commandDescription := properties["final_command"].(map[string]any)["description"].(string)
	outputDescription := properties["final_output"].(map[string]any)["description"].(string)

	for _, want := range []string{"exact complete", "pipeline", "jq"} {
		if !strings.Contains(commandDescription, want) {
			t.Errorf("final_command description does not mention %q: %s", want, commandDescription)
		}
	}
	for _, want := range []string{"exact unmodified stdout", "valid JSON", "without prose"} {
		if !strings.Contains(outputDescription, want) {
			t.Errorf("final_output description does not mention %q: %s", want, outputDescription)
		}
	}
}

func TestSystemPromptRequiresJSONPipelineEvidence(t *testing.T) {
	for _, want := range []string{
		"use jq rather than Python",
		"exact complete shell command or pipeline",
		"exact, unmodified stdout",
		"do not paraphrase it, truncate it, use placeholders, or wrap it in Markdown",
	} {
		if !strings.Contains(systemPromptTmpl, want) {
			t.Errorf("system prompt does not contain evidence rule %q", want)
		}
	}
}

func assertSchemaMatchesType(t *testing.T, schema map[string]any, typ reflect.Type) {
	t.Helper()

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema for %s has no properties object", typ.Name())
	}
	schemaFields := make([]string, 0, len(properties))
	for name := range properties {
		schemaFields = append(schemaFields, name)
	}
	sort.Strings(schemaFields)

	requiredValues, ok := schema["required"].([]any)
	if !ok {
		t.Fatalf("schema for %s has no required array", typ.Name())
	}
	requiredFields := make([]string, 0, len(requiredValues))
	for _, value := range requiredValues {
		name, ok := value.(string)
		if !ok {
			t.Fatalf("schema for %s has non-string required field %v", typ.Name(), value)
		}
		requiredFields = append(requiredFields, name)
	}
	sort.Strings(requiredFields)

	goFields := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		goFields = append(goFields, name)
	}
	sort.Strings(goFields)

	if !reflect.DeepEqual(schemaFields, goFields) {
		t.Errorf("schema properties for %s = %v, Go JSON fields = %v", typ.Name(), schemaFields, goFields)
	}
	if !reflect.DeepEqual(requiredFields, goFields) {
		t.Errorf("schema required fields for %s = %v, Go JSON fields = %v", typ.Name(), requiredFields, goFields)
	}
}

func TestParseAgentReportReadsStructuredOutput(t *testing.T) {
	raw := `{
  "type": "result",
  "structured_output": {
    "steps": [{"command": "shovels --help", "purpose": "discover commands"}],
    "final_command": "shovels permits search --help",
    "final_output": "{\"data\":[],\"meta\":{\"count\":0}}",
    "success": true,
    "usability_rating": 4,
    "usability_notes": "clear",
    "issues": [{"description": "sorting is client-side", "severity": "low"}]
  }
}`

	report, err := parseAgentReport([]byte(raw))
	if err != nil {
		t.Fatalf("parseAgentReport returned error: %v", err)
	}
	if report.UsabilityRating != 4 {
		t.Errorf("usability rating = %d, want 4", report.UsabilityRating)
	}
	if len(report.Issues) != 1 || report.Issues[0].Severity != "low" {
		t.Errorf("issues = %#v, want one low-severity issue", report.Issues)
	}
}

func TestParseAgentReportRejectsMissingStructuredOutput(t *testing.T) {
	_, err := parseAgentReport([]byte(`{"type":"result","result":"plain text"}`))
	if err == nil {
		t.Fatal("parseAgentReport accepted an envelope without structured_output")
	}
	if !strings.Contains(err.Error(), "no structured_output") {
		t.Errorf("error = %q, want missing structured_output detail", err)
	}
}
