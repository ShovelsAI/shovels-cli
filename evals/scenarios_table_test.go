//go:build eval

package evals

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// These tests exercise the scenario table and its validators directly. They
// spawn no agent and reach no network, so they run to completion even when
// TestEval skips for a missing API key or claude CLI.

// Captured shapes of the four searches the PropertiesNoSolar validator must
// tell apart. Each keeps the fields that carry the distinction and drops the
// rest of the row.

// absenceSearchOutput mirrors `shovels properties search --geo-id 92024
// --permit-tags=-solar --limit 1`: the row carries a populated trust object
// and the envelope carries meta.trust_summaries.
const absenceSearchOutput = `{"data":[{"id":"BJjCWAMtccQ","city":"ENCINITAS","zip_code":"92024","permit_count":11,"property_type":"commercial","legal_owner":"Olivenhain Municipal Water Dis","trust":{"unresolved_rate":0.07692307692307693,"coverage_tier":"high","data_horizon":"2026-01-12","footprint_basis":"matched","flags":["untagged_permits_present"]}}],"meta":{"count":1,"credits_used":1,"has_more":true,"trust_summaries":[{"rows_flagged":1,"row_weighted_unresolved_rate":0.07692307692307693,"expected_miss_rate":0.07692307692307693,"suppressed_scopes":0}]}}`

// presenceSearchOutput mirrors `shovels properties search --geo-id 92024
// --permit-tags solar --limit 1`: the same command without an absence filter,
// which leaves trust null and omits meta.trust_summaries.
const presenceSearchOutput = `{"data":[{"id":"BJjCWAMtccQ","city":"ENCINITAS","zip_code":"92024","permit_count":11,"property_type":"commercial","trust":null}],"meta":{"count":1,"credits_used":1,"has_more":true}}`

// permitsSearchOutput mirrors `shovels permits search --geo-id 92024`: permit
// rows carry no trust field at all.
const permitsSearchOutput = `{"data":[{"id":"cS8pFxKqR2A","number":"BLD-2024-00417","geo_id":"92024","job_value":4200000,"status":"final"}],"meta":{"count":1,"credits_used":1,"has_more":true}}`

// emptyAbsenceSearchOutput mirrors an absence search that matched nothing.
const emptyAbsenceSearchOutput = `{"data":[],"meta":{"count":0,"credits_used":1,"has_more":false,"trust_summaries":[{"rows_flagged":0,"row_weighted_unresolved_rate":0,"expected_miss_rate":0,"suppressed_scopes":0}]}}`

// rowTrustProblem is the substring checkNoSolarProperties reports when a
// populated row set carries no trust object. The meta.trust_summaries problem
// also contains the word "trust", so asserting on the bare word proves nothing.
const rowTrustProblem = "no row carries a non-null 'trust' object"

func scenarioByName(t *testing.T, name string) Scenario {
	t.Helper()
	for _, sc := range scenarios {
		if sc.Name == name {
			return sc
		}
	}
	t.Fatalf("no scenario named %q in the scenarios table", name)
	return Scenario{}
}

func TestPropertiesNoSolarIsDeclaredAsAnAbsenceSearchScenario(t *testing.T) {
	sc := scenarioByName(t, "PropertiesNoSolar")

	const wantTask = `Which properties in Encinitas have no solar permit?`
	if sc.Task != wantTask {
		t.Errorf("Task = %q, want %q", sc.Task, wantTask)
	}
	if sc.ValidateOutput == nil {
		t.Error("ValidateOutput is nil; the absence-search assertions would never run")
	}
	if !sc.EnforceUsability {
		t.Error("EnforceUsability is false; a usability rating below 4 must fail this scenario, not warn")
	}
}

// The blind-agent principle: the task states the goal and never the CLI
// surface, or the scenario stops testing whether help text is discoverable.
func TestPropertiesNoSolarTaskNamesNoCLIJargon(t *testing.T) {
	jargon := []string{
		"shovels", "--", "properties search", "permit-tags", "permit_tags",
		"geo-id", "geo_id", "subcommand", "command", "flag",
	}

	task := strings.ToLower(scenarioByName(t, "PropertiesNoSolar").Task)
	for _, term := range jargon {
		if strings.Contains(task, term) {
			t.Errorf("task names CLI jargon %q: %s", term, task)
		}
	}
}

// TestEval runs the custom validator or the default domain check, never both,
// so a scenario that sets Domain alongside ValidateOutput silently loses the
// domain assertion.
func TestScenariosWithACustomValidatorDeclareNoDomain(t *testing.T) {
	for _, sc := range scenarios {
		if sc.ValidateOutput != nil && sc.Domain != "" {
			t.Errorf("scenario %s sets ValidateOutput and Domain %q; TestEval would never run the Domain check",
				sc.Name, sc.Domain)
		}
	}
}

func TestScenarioNamesAreUnique(t *testing.T) {
	seen := make(map[string]bool, len(scenarios))
	for _, sc := range scenarios {
		if seen[sc.Name] {
			t.Errorf("duplicate scenario name %q; TestEval subtest names must be unique", sc.Name)
		}
		seen[sc.Name] = true
	}
}

// The eval notes in CLAUDE.md quote a scenario count that readers use to
// judge runtime and cost, so it tracks the table rather than drifting from it.
func TestCLAUDEMDScenarioCountMatchesTheTable(t *testing.T) {
	path := filepath.Join(moduleRoot(), "CLAUDE.md")
	doc, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}

	countLine := regexp.MustCompile(`(?m)^- (\d+) scenarios:`)
	match := countLine.FindSubmatch(doc)
	if match == nil {
		t.Fatalf("%s has no `- N scenarios:` line in the LLM Evals notes", path)
	}

	documented, err := strconv.Atoi(string(match[1]))
	if err != nil {
		t.Fatalf("cannot parse scenario count %q: %v", match[1], err)
	}
	if documented != len(scenarios) {
		t.Errorf("%s documents %d scenarios, table has %d", path, documented, len(scenarios))
	}
}

// Encinitas can be scoped by its resolved city geo_id or by ZIP 92024. Both
// are legitimate routes to the same envelope, so validation reads the output
// and never the command that produced it. Driving the scenario's own
// ValidateOutput also proves the table entry reaches the validator.
func TestNoSolarValidatorAcceptsAbsenceOutputFromEitherGeoRoute(t *testing.T) {
	validate := scenarioByName(t, "PropertiesNoSolar").ValidateOutput

	routes := []struct {
		name         string
		finalCommand string
	}{
		{"resolved city geo_id", `shovels properties search --geo-id RMjg6rIIh2k --permit-tags=-solar --limit 10`},
		{"ZIP", `shovels properties search --geo-id 92024 --permit-tags=-solar --limit 10`},
	}

	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			validate(t, AgentReport{FinalCommand: route.finalCommand, FinalOutput: absenceSearchOutput})
		})
	}
}

func TestNoSolarValidatorRejectsOutputLackingAbsenceEvidence(t *testing.T) {
	tests := []struct {
		name         string
		output       string
		wantMentions []string
	}{
		{
			name:         "presence-only properties search leaves trust null",
			output:       presenceSearchOutput,
			wantMentions: []string{rowTrustProblem, "meta.trust_summaries"},
		},
		{
			name:         "permits search rows carry no trust field",
			output:       permitsSearchOutput,
			wantMentions: []string{rowTrustProblem, "meta.trust_summaries"},
		},
		{
			name:   "absence search matching nothing names the manual disambiguation",
			output: emptyAbsenceSearchOutput,
			wantMentions: []string{
				"meta.count",
				"changed upstream",
				`shovels properties search --geo-id 92024 --permit-tags=-solar --limit 1`,
			},
		},
		{
			name:         "envelope without a data array",
			output:       `{"meta":{"count":1,"trust_summaries":[]}}`,
			wantMentions: []string{"'data' array"},
		},
		{
			name:         "envelope without meta.count",
			output:       `{"data":[{"trust":{"coverage_tier":"high"}}],"meta":{"trust_summaries":[]}}`,
			wantMentions: []string{"meta.count"},
		},
		{
			name:         "prose carrying no JSON at all",
			output:       "I found 42 properties in Encinitas without a solar permit.",
			wantMentions: []string{"no JSON object"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			problems := checkNoSolarProperties(tc.output)
			if len(problems) == 0 {
				t.Fatal("output accepted; want at least one problem reported")
			}

			joined := strings.Join(problems, "\n")
			for _, want := range tc.wantMentions {
				if !strings.Contains(joined, want) {
					t.Errorf("no problem mentions %q; got:\n%s", want, joined)
				}
			}
		})
	}
}

// A zero-row result is a hard failure rather than a skip, so the signal is
// never silently lost — but it must not also be blamed on a missing absence
// filter, which the empty row set makes vacuously true.
func TestNoSolarValidatorBlamesNoAbsenceFilterOnlyWhenRowsExist(t *testing.T) {
	for _, problem := range checkNoSolarProperties(emptyAbsenceSearchOutput) {
		if strings.Contains(problem, rowTrustProblem) {
			t.Errorf("empty result blamed on a missing absence filter: %s", problem)
		}
	}

	problems := strings.Join(checkNoSolarProperties(presenceSearchOutput), "\n")
	if !strings.Contains(problems, rowTrustProblem) {
		t.Errorf("populated rows with null trust not reported; got:\n%s", problems)
	}
}
