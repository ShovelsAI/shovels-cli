package cmd

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/shovels-ai/shovels-cli/internal/contract"
)

// schemaCapMetaKey is the meta envelope key a capped search's schema documents
// its endpoint's ceiling under.
const schemaCapMetaKey = "server_capped"

// noCursorClause is the part of the disclosure that keeps a ceiling from
// reading as a cursor someone has exhausted. Without it a reader has a number
// and no reason to believe a continuation is unreachable.
const noCursorClause = "exposes no continuation cursor"

// disclosedCapPattern matches the phrase the cap entry publishes its ceiling
// in. Reading the number back out of the description is what lets the guard
// compare the generated schema against the record rather than the record
// against itself.
var disclosedCapPattern = regexp.MustCompile(`at most (\d+) records`)

// disclosedCaps returns every ceiling a description publishes, in the order it
// states them.
func disclosedCaps(t *testing.T, description string) []int {
	t.Helper()

	var disclosed []int
	for _, match := range disclosedCapPattern.FindAllStringSubmatch(description, -1) {
		n, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("description states an unparseable ceiling %q", match[1])
		}
		disclosed = append(disclosed, n)
	}
	return disclosed
}

// schemaCapProblems reports every disagreement between what a command's schema
// discloses about its endpoint's ceiling and what its contract record carries: a
// capped command whose schema documents no ceiling, a description publishing no
// number or a number the record does not carry, a description omitting the
// missing-cursor clause, and a ceiling documented for a command whose record
// declares none. Both tables are parameters so a case can pose a schema and a
// record that disagree.
func schemaCapProblems(t *testing.T, schemas map[string]CommandSchema, records map[string]contract.Record) []string {
	t.Helper()

	var problems []string
	for _, path := range slices.Sorted(maps.Keys(schemas)) {
		record, ok := records[path]
		if !ok {
			continue
		}
		entry, documented := schemas[path].MetaFields[schemaCapMetaKey]

		if record.Mode != contract.ModeServerCapped {
			if documented {
				problems = append(problems, fmt.Sprintf("%s: schema documents %s, record declares no cap", path, schemaCapMetaKey))
			}
			continue
		}
		if !documented {
			problems = append(problems, fmt.Sprintf("%s: schema documents no %s, record caps at %d", path, schemaCapMetaKey, record.Cap))
			continue
		}

		disclosed := disclosedCaps(t, entry.Description)
		if len(disclosed) == 0 {
			problems = append(problems, fmt.Sprintf("%s: cap description publishes no ceiling, record caps at %d", path, record.Cap))
		}
		for _, n := range disclosed {
			if n != record.Cap {
				problems = append(problems, fmt.Sprintf("%s: schema states cap %d, record caps at %d", path, n, record.Cap))
			}
		}
		if !strings.Contains(entry.Description, noCursorClause) {
			problems = append(problems, fmt.Sprintf("%s: cap description omits %q", path, noCursorClause))
		}
	}

	slices.Sort(problems)
	return problems
}

// cappedSchema returns a schema whose cap entry carries the given description,
// standing in for one the generator produced.
func cappedSchema(description string) CommandSchema {
	return CommandSchema{
		MetaFields: map[string]SchemaField{
			schemaCapMetaKey: {Type: "integer", Description: description},
		},
	}
}

// --- Happy paths ---

func TestEverySchemaDisclosesTheCapItsRecordCarries(t *testing.T) {
	problems := schemaCapProblems(t, schemaRegistry, contract.All())

	if len(problems) > 0 {
		t.Errorf("generated schemas and contract table disagree:\n  %s", strings.Join(problems, "\n  "))
	}
}

// --- Error conditions ---

func TestSchemaCapGuardNamesACappedCommandDocumentingNoCeiling(t *testing.T) {
	schemas := map[string]CommandSchema{"cities search": {}}
	records := map[string]contract.Record{
		"cities search": {Honored: contract.APIOnlyFlags(), Mode: contract.ModeServerCapped, Cap: 15},
	}

	problems := schemaCapProblems(t, schemas, records)

	want := []string{"cities search: schema documents no server_capped, record caps at 15"}
	if !slices.Equal(problems, want) {
		t.Errorf("expected %v, got %v", want, problems)
	}
}

func TestSchemaCapGuardNamesACappedCommandDocumentingADifferentCeiling(t *testing.T) {
	schemas := map[string]CommandSchema{
		"cities search": cappedSchema("Returns at most 99 records and exposes no continuation cursor."),
	}
	records := map[string]contract.Record{
		"cities search": {Honored: contract.APIOnlyFlags(), Mode: contract.ModeServerCapped, Cap: 15},
	}

	problems := schemaCapProblems(t, schemas, records)

	want := []string{"cities search: schema states cap 99, record caps at 15"}
	if !slices.Equal(problems, want) {
		t.Errorf("expected %v, got %v", want, problems)
	}
}

func TestSchemaCapGuardNamesACeilingDescribedWithoutTheMissingCursor(t *testing.T) {
	schemas := map[string]CommandSchema{
		"cities search": cappedSchema("Returns at most 15 records for one query."),
	}
	records := map[string]contract.Record{
		"cities search": {Honored: contract.APIOnlyFlags(), Mode: contract.ModeServerCapped, Cap: 15},
	}

	problems := schemaCapProblems(t, schemas, records)

	want := []string{`cities search: cap description omits "exposes no continuation cursor"`}
	if !slices.Equal(problems, want) {
		t.Errorf("expected %v, got %v", want, problems)
	}
}

func TestSchemaCapGuardNamesAnUncappedCommandDocumentingACeiling(t *testing.T) {
	schemas := map[string]CommandSchema{
		"permits search": cappedSchema("Returns at most 15 records and exposes no continuation cursor."),
	}
	records := map[string]contract.Record{
		"permits search": {Honored: contract.APIOnlyFlags(), Mode: contract.ModeCursor},
	}

	problems := schemaCapProblems(t, schemas, records)

	want := []string{"permits search: schema documents server_capped, record declares no cap"}
	if !slices.Equal(problems, want) {
		t.Errorf("expected %v, got %v", want, problems)
	}
}
