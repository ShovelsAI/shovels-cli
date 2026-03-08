//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// =======================================================================
// Happy paths
// =======================================================================

// TestSchemaHintInContractorsSearchHelp verifies that
// `shovels contractors search --help` contains a tip pointing agents
// to the --schema flag for field introspection.
func TestSchemaHintInContractorsSearchHelp(t *testing.T) {
	result := runCLI(t, "contractors", "search", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if !strings.Contains(result.Stdout, "--schema") {
		t.Error("contractors search --help should contain a --schema hint")
	}
}

// TestSchemaHintInContractorsGetHelp verifies that
// `shovels contractors get --help` contains a tip pointing agents
// to the --schema flag for field introspection.
func TestSchemaHintInContractorsGetHelp(t *testing.T) {
	result := runCLI(t, "contractors", "get", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if !strings.Contains(result.Stdout, "--schema") {
		t.Error("contractors get --help should contain a --schema hint")
	}
}

// TestSchemaHintInPermitsSearchHelp verifies that
// `shovels permits search --help` contains a tip pointing agents
// to the --schema flag for field introspection.
func TestSchemaHintInPermitsSearchHelp(t *testing.T) {
	result := runCLI(t, "permits", "search", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if !strings.Contains(result.Stdout, "--schema") {
		t.Error("permits search --help should contain a --schema hint")
	}
}

// TestSchemaHintInRootHelp verifies that `shovels --help` contains a
// section about --schema for inspecting response fields.
func TestSchemaHintInRootHelp(t *testing.T) {
	result := runCLI(t, "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := result.Stdout

	if !strings.Contains(out, "--schema") {
		t.Error("root --help should mention --schema")
	}

	if !strings.Contains(out, "response field") {
		t.Error("root --help --schema section should describe inspecting response fields")
	}
}

// =======================================================================
// Edge cases
// =======================================================================

// TestSchemaHintDiscoverableEarlyInRootHelp verifies that the --schema
// hint appears in the root help near the resource list, so agents
// exploring the CLI for the first time discover it early.
func TestSchemaHintDiscoverableEarlyInRootHelp(t *testing.T) {
	result := runCLI(t, "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := result.Stdout

	// The --schema hint should appear before the "Authentication" section,
	// ensuring agents see it early during initial exploration.
	schemaIdx := strings.Index(out, "--schema")
	authIdx := strings.Index(out, "Authentication")

	if schemaIdx == -1 {
		t.Fatal("root --help should mention --schema")
	}
	if authIdx == -1 {
		t.Fatal("root --help should mention Authentication")
	}
	if schemaIdx >= authIdx {
		t.Error("--schema hint should appear before the Authentication section so agents discover it early")
	}
}

// =======================================================================
// Boundary conditions
// =======================================================================

// TestSchemaHintIsConcise verifies that the schema hint in each
// subcommand's help text is 1-2 lines, per brevity requirements.
func TestSchemaHintIsConcise(t *testing.T) {
	commands := []struct {
		name string
		args []string
	}{
		{"contractors search", []string{"contractors", "search", "--help"}},
		{"contractors get", []string{"contractors", "get", "--help"}},
		{"permits search", []string{"permits", "search", "--help"}},
	}

	for _, tc := range commands {
		t.Run(tc.name, func(t *testing.T) {
			result := runCLI(t, tc.args...)

			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
			}

			lines := strings.Split(result.Stdout, "\n")
			tipIdx := -1
			for i, line := range lines {
				if strings.Contains(line, "Tip:") && strings.Contains(line, "--schema") {
					tipIdx = i
					break
				}
			}
			if tipIdx == -1 {
				t.Fatalf("%s --help should contain a 'Tip:' line mentioning --schema", tc.name)
			}

			// Count the tip block: the "Tip:" line plus any continuation lines.
			hintLines := 1
			for i := tipIdx + 1; i < len(lines); i++ {
				trimmed := strings.TrimSpace(lines[i])
				if trimmed == "" {
					break
				}
				// Continuation lines are indented.
				if strings.HasPrefix(lines[i], " ") || strings.HasPrefix(lines[i], "\t") {
					hintLines++
				} else {
					break
				}
			}

			if hintLines > 2 {
				t.Errorf("%s schema hint should be 1-2 lines, got %d", tc.name, hintLines)
			}
		})
	}
}

// TestRootSchemaHintIsConcise verifies that the schema section in root
// help is 1-2 lines, per brevity requirements.
func TestRootSchemaHintIsConcise(t *testing.T) {
	result := runCLI(t, "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	lines := strings.Split(result.Stdout, "\n")
	startIdx := -1
	for i, line := range lines {
		if strings.Contains(line, "Inspect response field") {
			startIdx = i
			break
		}
	}
	if startIdx == -1 {
		t.Fatal("root --help should contain an 'Inspect response fields' section")
	}

	// Count non-empty lines in this section.
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

	if sectionLines > 2 {
		t.Errorf("root --schema hint section should be 1-2 lines, got %d", sectionLines)
	}
}
