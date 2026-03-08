//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// =======================================================================
// Happy paths
// =======================================================================

// TestContractorsSearchHelpShowsScopingSection verifies that
// `shovels contractors search --help` contains a "Response field scoping"
// section explaining GLOBAL vs FILTERED fields.
func TestContractorsSearchHelpShowsScopingSection(t *testing.T) {
	result := runCLI(t, "contractors", "search", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := result.Stdout

	if !strings.Contains(out, "Response field scoping") {
		t.Error("contractors search --help should contain a 'Response field scoping' section")
	}

	if !strings.Contains(out, "GLOBAL") {
		t.Error("scoping section should mention GLOBAL fields")
	}

	if !strings.Contains(out, "NOT filtered") {
		t.Error("scoping section should explain that most fields are NOT filtered by search parameters")
	}
}

// TestContractorsSearchHelpScopingSectionExplainsTallySumming verifies that
// the scoping section tells agents to sum tag_tally or status_tally for
// local permit counts.
func TestContractorsSearchHelpScopingSectionExplainsTallySumming(t *testing.T) {
	result := runCLI(t, "contractors", "search", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := result.Stdout

	if !strings.Contains(out, "tag_tally") {
		t.Error("scoping section should mention tag_tally")
	}
	if !strings.Contains(out, "status_tally") {
		t.Error("scoping section should mention status_tally")
	}
	if !strings.Contains(out, "sum") {
		t.Error("scoping section should tell agents to sum tallies for local permit counts")
	}
}

// TestContractorsSearchHelpNoTalliesWarning verifies that the --no-tallies
// flag description warns that tallies are the only search-filtered counts.
func TestContractorsSearchHelpNoTalliesWarning(t *testing.T) {
	result := runCLI(t, "contractors", "search", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := result.Stdout

	// Find the --no-tallies flag description line (starts with whitespace + flag name,
	// not an example line which contains the flag as a CLI argument).
	found := false
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "--no-tallies") {
			continue
		}
		found = true
		if !strings.Contains(strings.ToLower(trimmed), "tallies are the only") {
			t.Errorf("--no-tallies description should warn tallies are the only search-filtered counts, got %q", trimmed)
		}
		break
	}
	if !found {
		t.Error("contractors search --help should contain --no-tallies flag")
	}
}

// =======================================================================
// Edge cases
// =======================================================================

// TestContractorsSearchHelpAloneSufficientForScoping verifies that the help
// text alone (without --schema) contains enough information to avoid scope
// confusion: it names the GLOBAL fields, names the FILTERED fields, and
// explains how to get local counts.
func TestContractorsSearchHelpAloneSufficientForScoping(t *testing.T) {
	result := runCLI(t, "contractors", "search", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := result.Stdout

	// Must mention specific GLOBAL fields so an agent knows which are unfiltered.
	globalFields := []string{"permit_count", "avg_job_value", "total_job_value"}
	for _, field := range globalFields {
		if !strings.Contains(out, field) {
			t.Errorf("help text should mention GLOBAL field %q so agents know it is unfiltered", field)
		}
	}

	// Must mention FILTERED fields.
	filteredFields := []string{"tag_tally", "status_tally"}
	for _, field := range filteredFields {
		if !strings.Contains(out, field) {
			t.Errorf("help text should mention FILTERED field %q", field)
		}
	}
}

// =======================================================================
// Boundary conditions
// =======================================================================

// TestContractorsSearchHelpScopingSectionIsConcise verifies that the scoping
// section is concise (3-5 lines), not a wall of text.
func TestContractorsSearchHelpScopingSectionIsConcise(t *testing.T) {
	result := runCLI(t, "contractors", "search", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := result.Stdout

	// Find the scoping section and count its lines.
	lines := strings.Split(out, "\n")
	startIdx := -1
	for i, line := range lines {
		if strings.Contains(line, "Response field scoping") {
			startIdx = i
			break
		}
	}
	if startIdx == -1 {
		t.Fatal("could not find 'Response field scoping' section")
	}

	// Count non-empty lines in the section (until next blank line after content or end).
	sectionLines := 0
	for i := startIdx; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" && sectionLines > 0 {
			break
		}
		if trimmed != "" {
			sectionLines++
		}
	}

	if sectionLines < 3 || sectionLines > 5 {
		t.Errorf("scoping section should be concise (3-5 lines), got %d lines", sectionLines)
	}
}

// TestContractorsSearchHelpNoTalliesWarningIsConcise verifies that the
// --no-tallies flag description is concise (1-2 lines per spec boundary).
func TestContractorsSearchHelpNoTalliesWarningIsConcise(t *testing.T) {
	result := runCLI(t, "contractors", "search", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	lines := strings.Split(result.Stdout, "\n")
	flagLineIdx := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--no-tallies") {
			flagLineIdx = i
			break
		}
	}
	if flagLineIdx == -1 {
		t.Fatal("could not find --no-tallies flag line in help output")
	}

	// Count continuation lines: cobra wraps long descriptions as indented
	// lines immediately following the flag line.
	descLines := 1
	for i := flagLineIdx + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		// A continuation line is non-empty and does not start with "--" (next flag).
		if trimmed == "" || strings.HasPrefix(trimmed, "--") || strings.HasPrefix(trimmed, "-") {
			break
		}
		descLines++
	}

	if descLines > 2 {
		t.Errorf("--no-tallies warning should be concise (1-2 lines), got %d lines", descLines)
	}
}
