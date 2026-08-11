package cmd

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/shovels-ai/shovels-cli/internal/contract"
)

// statedCapPattern matches the sentence a capped command's help publishes its
// cap in. Reading the number back out of the help text is what lets the guard
// compare help against the record rather than the record against itself.
var statedCapPattern = regexp.MustCompile(`at most (\d+) results`)

// statedCaps returns every cap a help text publishes, in the order it states
// them.
func statedCaps(t *testing.T, long string) []int {
	t.Helper()

	var stated []int
	for _, match := range statedCapPattern.FindAllStringSubmatch(long, -1) {
		n, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("help states an unparseable cap %q", match[1])
		}
		stated = append(stated, n)
	}
	return stated
}

// capDisclosureProblems reports every disagreement between the cap a command's
// help publishes and the cap its record carries: a capped command that
// publishes none, a published number the record does not carry, and a cap
// published by a command whose record declares none. The table is a parameter
// so a case can pose help and a record that disagree.
func capDisclosureProblems(t *testing.T, records map[string]contract.Record) []string {
	t.Helper()

	var problems []string
	for _, cmd := range runnableCommands(rootCmd) {
		path := contractPath(cmd)
		record, ok := records[path]
		if !ok {
			continue
		}
		stated := statedCaps(t, cmd.Long)

		if record.Mode != contract.ModeServerCapped {
			for _, n := range stated {
				problems = append(problems, fmt.Sprintf("%s: help states cap %d, record declares none", path, n))
			}
			continue
		}
		if len(stated) == 0 {
			problems = append(problems, fmt.Sprintf("%s: help publishes no cap, record caps at %d", path, record.Cap))
		}
		for _, n := range stated {
			if n != record.Cap {
				problems = append(problems, fmt.Sprintf("%s: help states cap %d, record caps at %d", path, n, record.Cap))
			}
		}
	}

	slices.Sort(problems)
	return problems
}

// --- Happy paths ---

func TestEveryCommandPublishesTheCapItsRecordCarries(t *testing.T) {
	problems := capDisclosureProblems(t, contract.All())

	if len(problems) > 0 {
		t.Errorf("help text and contract table disagree:\n  %s", strings.Join(problems, "\n  "))
	}
}

// --- Error conditions ---

func TestGuardNamesACappedCommandWhoseHelpStatesADifferentCap(t *testing.T) {
	records := map[string]contract.Record{
		"cities search": {Honored: contract.APIOnlyFlags(), Mode: contract.ModeServerCapped, Cap: 99},
	}

	problems := capDisclosureProblems(t, records)

	want := []string{"cities search: help states cap 15, record caps at 99"}
	if !slices.Equal(problems, want) {
		t.Errorf("expected %v, got %v", want, problems)
	}
}

func TestGuardNamesACappedCommandWhoseHelpPublishesNoCap(t *testing.T) {
	records := map[string]contract.Record{
		"permits search": {Honored: contract.APIOnlyFlags(), Mode: contract.ModeServerCapped, Cap: 15},
	}

	problems := capDisclosureProblems(t, records)

	want := []string{"permits search: help publishes no cap, record caps at 15"}
	if !slices.Equal(problems, want) {
		t.Errorf("expected %v, got %v", want, problems)
	}
}

func TestGuardNamesAnUncappedCommandWhoseHelpPublishesACap(t *testing.T) {
	records := map[string]contract.Record{
		"cities search": {Honored: contract.APIOnlyFlags(), Mode: contract.ModeCursor},
	}

	problems := capDisclosureProblems(t, records)

	want := []string{"cities search: help states cap 15, record declares none"}
	if !slices.Equal(problems, want) {
		t.Errorf("expected %v, got %v", want, problems)
	}
}
