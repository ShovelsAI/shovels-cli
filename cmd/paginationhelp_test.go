package cmd

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/shovels-ai/shovels-cli/internal/contract"
	"github.com/spf13/cobra"
)

// statedCapPattern matches the sentence a capped command's help publishes its
// cap in. Reading the number back out of the help text is what lets the guard
// compare help against the record rather than the record against itself.
var statedCapPattern = regexp.MustCompile(`at most (\d+) results`)

// cursorSentence is the note's opening clause, matched whole. "cursor-paginated"
// alone appears in --limit's own description, which every command honoring the
// flag renders — including the capped searches, whose help must NOT claim a
// cursor.
const cursorSentence = "This command is cursor-paginated"

// cursorKnobs are the flags a reader needs to act on the cursor sentence. The
// guard reads them back out of the help the way statedCaps reads the cap: an
// announcement that a command pages is worth nothing without the two flags that
// page it, so losing them must fail rather than pass on the opening words.
var cursorKnobs = []string{"--limit all", "--max-records"}

// paginationWord matches prose describing pagination. It is applied to Long
// alone: the record's paragraph reaches help through the template, never
// through Long, so a match there is a second hand-written voice — the shape
// that survives when a paragraph moves to the record and one copy is missed.
// A command with no Long has only its one-line Short, which has no room to
// state a contract twice.
var paginationWord = regexp.MustCompile(`(?i)paginat|cursor|at most \d+ results`)

// renderedHelp returns what `<command> --help` prints. The guards read this
// rather than cmd.Long because the pagination paragraph reaches the reader
// through the help template: a Long-only check would see neither the paragraph
// nor a cap hand-written into some other part of the output.
func renderedHelp(t *testing.T, cmd *cobra.Command) string {
	t.Helper()

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	defer cmd.SetOut(nil)

	if err := cmd.Help(); err != nil {
		t.Fatalf("rendering help for %q: %v", contractPath(cmd), err)
	}
	return buf.String()
}

// statedCaps returns every cap a help text publishes, in the order it states
// them.
func statedCaps(t *testing.T, help string) []int {
	t.Helper()

	var stated []int
	for _, match := range statedCapPattern.FindAllStringSubmatch(help, -1) {
		n, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("help states an unparseable cap %q", match[1])
		}
		stated = append(stated, n)
	}
	return stated
}

// paginationDisclosureProblems reports every disagreement between what a
// command's help says about pagination and what its record declares: a capped
// command publishing no cap or the wrong one, a cap published by a command whose
// record declares none, a cursor-paginated command that never says so, and a
// cursor claimed by a command that follows no cursor. The table is a parameter
// so a case can pose help and a record that disagree.
func paginationDisclosureProblems(t *testing.T, records map[string]contract.Record) []string {
	t.Helper()

	var problems []string
	for _, cmd := range runnableCommands(rootCmd) {
		path := contractPath(cmd)
		record, ok := records[path]
		if !ok {
			continue
		}
		help := renderedHelp(t, cmd)
		stated := statedCaps(t, help)
		claimsCursor := strings.Contains(help, cursorSentence)

		// Where the record supplies a paragraph, it must be the only voice
		// describing pagination: a second one can only disagree with it. A
		// command the record generates nothing for is left to its own prose,
		// which is then the single statement rather than a duplicate.
		if paginationNote(cmd) != "" && paginationWord.MatchString(cmd.Long) {
			problems = append(problems, fmt.Sprintf("%s: Long describes pagination the record's paragraph already states", path))
		}

		switch record.Mode {
		case contract.ModeServerCapped:
			if len(stated) == 0 {
				problems = append(problems, fmt.Sprintf("%s: help publishes no cap, record caps at %d", path, record.Cap))
			}
			for _, n := range stated {
				if n != record.Cap {
					problems = append(problems, fmt.Sprintf("%s: help states cap %d, record caps at %d", path, n, record.Cap))
				}
			}
			if claimsCursor {
				problems = append(problems, fmt.Sprintf("%s: help claims a cursor, record caps at %d", path, record.Cap))
			}
		case contract.ModeCursor:
			for _, n := range stated {
				problems = append(problems, fmt.Sprintf("%s: help states cap %d, record declares none", path, n))
			}
			if !claimsCursor {
				problems = append(problems, fmt.Sprintf("%s: help describes no pagination, record follows a cursor", path))
				break
			}
			for _, knob := range cursorKnobs {
				if !strings.Contains(help, knob) {
					problems = append(problems, fmt.Sprintf("%s: help says it pages but never names %s", path, knob))
				}
			}
		default:
			for _, n := range stated {
				problems = append(problems, fmt.Sprintf("%s: help states cap %d, record declares none", path, n))
			}
			if claimsCursor {
				problems = append(problems, fmt.Sprintf("%s: help claims a cursor, record collects no records", path))
			}
		}
	}

	slices.Sort(problems)
	return problems
}

// --- Happy paths ---

func TestEveryCommandPublishesThePaginationItsRecordCarries(t *testing.T) {
	problems := paginationDisclosureProblems(t, contract.All())

	if len(problems) > 0 {
		t.Errorf("help text and contract table disagree:\n  %s", strings.Join(problems, "\n  "))
	}
}

// --- Error conditions ---

func TestGuardNamesACappedCommandWhoseHelpStatesADifferentCap(t *testing.T) {
	records := map[string]contract.Record{
		"cities search": {Honored: contract.APIOnlyFlags(), Mode: contract.ModeServerCapped, Cap: 99},
	}

	problems := paginationDisclosureProblems(t, records)

	want := []string{"cities search: help states cap 15, record caps at 99"}
	if !slices.Equal(problems, want) {
		t.Errorf("expected %v, got %v", want, problems)
	}
}

func TestGuardNamesACappedCommandWhoseHelpPublishesNoCap(t *testing.T) {
	records := map[string]contract.Record{
		"permits search": {Honored: contract.APIOnlyFlags(), Mode: contract.ModeServerCapped, Cap: 15},
	}

	problems := paginationDisclosureProblems(t, records)

	want := []string{
		"permits search: help claims a cursor, record caps at 15",
		"permits search: help publishes no cap, record caps at 15",
	}
	if !slices.Equal(problems, want) {
		t.Errorf("expected %v, got %v", want, problems)
	}
}

func TestGuardNamesAnUncappedCommandWhoseHelpPublishesACap(t *testing.T) {
	records := map[string]contract.Record{
		"cities search": {Honored: contract.APIOnlyFlags(), Mode: contract.ModeCursor},
	}

	problems := paginationDisclosureProblems(t, records)

	want := []string{
		"cities search: help describes no pagination, record follows a cursor",
		"cities search: help states cap 15, record declares none",
	}
	if !slices.Equal(problems, want) {
		t.Errorf("expected %v, got %v", want, problems)
	}
}

func TestGuardNamesACursorCommandWhoseHelpDescribesNoPagination(t *testing.T) {
	records := map[string]contract.Record{
		"usage": {Honored: contract.APIOnlyFlags(), Mode: contract.ModeCursor},
	}

	problems := paginationDisclosureProblems(t, records)

	want := []string{"usage: help describes no pagination, record follows a cursor"}
	if !slices.Equal(problems, want) {
		t.Errorf("expected %v, got %v", want, problems)
	}
}

func TestGuardNamesAnUnpaginatedCommandWhoseHelpClaimsACursor(t *testing.T) {
	records := map[string]contract.Record{
		"permits search": {Honored: contract.APIOnlyFlags(), Mode: contract.ModeNone},
	}

	problems := paginationDisclosureProblems(t, records)

	want := []string{"permits search: help claims a cursor, record collects no records"}
	if !slices.Equal(problems, want) {
		t.Errorf("expected %v, got %v", want, problems)
	}
}

func TestGuardNamesAnUnpaginatedCommandWhoseHelpPublishesACap(t *testing.T) {
	records := map[string]contract.Record{
		"cities search": {Honored: contract.APIOnlyFlags(), Mode: contract.ModeNone},
	}

	problems := paginationDisclosureProblems(t, records)

	want := []string{"cities search: help states cap 15, record declares none"}
	if !slices.Equal(problems, want) {
		t.Errorf("expected %v, got %v", want, problems)
	}
}
