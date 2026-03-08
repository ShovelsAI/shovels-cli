//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// =======================================================================
// Happy paths
// =======================================================================

// TestPositionalArgHintPresent verifies that each command accepting an ID
// as a positional argument includes a note in its Long help text explaining
// that ID is positional, with a correct and an incorrect usage example.
func TestPositionalArgHintPresent(t *testing.T) {
	commands := []struct {
		name    string
		args    []string
		correct string
		wrong   string
	}{
		{
			name:    "contractors permits",
			args:    []string{"contractors", "permits", "--help"},
			correct: "shovels contractors permits ABC123",
			wrong:   "shovels contractors permits --id ABC123",
		},
		{
			name:    "contractors employees",
			args:    []string{"contractors", "employees", "--help"},
			correct: "shovels contractors employees ABC123",
			wrong:   "shovels contractors employees --id ABC123",
		},
		{
			name:    "contractors metrics",
			args:    []string{"contractors", "metrics", "--help"},
			correct: "shovels contractors metrics ABC123",
			wrong:   "shovels contractors metrics --id ABC123",
		},
		{
			name:    "contractors get",
			args:    []string{"contractors", "get", "--help"},
			correct: "shovels contractors get C123",
			wrong:   "shovels contractors get --id C123",
		},
		{
			name:    "permits get",
			args:    []string{"permits", "get", "--help"},
			correct: "shovels permits get P123",
			wrong:   "shovels permits get --id P123",
		},
	}

	for _, tc := range commands {
		t.Run(tc.name, func(t *testing.T) {
			result := runCLI(t, tc.args...)

			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
			}

			out := result.Stdout

			if !strings.Contains(out, "positional argument, not a flag") {
				t.Errorf("%s --help should state ID is a positional argument, not a flag", tc.name)
			}

			if !strings.Contains(out, tc.correct) {
				t.Errorf("%s --help should show correct example %q", tc.name, tc.correct)
			}

			if !strings.Contains(out, tc.wrong) {
				t.Errorf("%s --help should show incorrect example %q to warn against --id usage", tc.name, tc.wrong)
			}
		})
	}
}

// =======================================================================
// Edge cases
// =======================================================================

// TestPositionalArgHintReinforcesUseLine verifies that the Use line already
// shows positional syntax (e.g., "permits ID") and the note reinforces
// the same message without contradicting it.
func TestPositionalArgHintReinforcesUseLine(t *testing.T) {
	commands := []struct {
		name    string
		args    []string
		useLine string
	}{
		{"contractors permits", []string{"contractors", "permits", "--help"}, "permits ID"},
		{"contractors employees", []string{"contractors", "employees", "--help"}, "employees ID"},
		{"contractors metrics", []string{"contractors", "metrics", "--help"}, "metrics ID"},
		{"contractors get", []string{"contractors", "get", "--help"}, "get ID"},
		{"permits get", []string{"permits", "get", "--help"}, "get ID"},
	}

	for _, tc := range commands {
		t.Run(tc.name, func(t *testing.T) {
			result := runCLI(t, tc.args...)

			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
			}

			out := result.Stdout

			// Use line shows positional syntax.
			if !strings.Contains(out, tc.useLine) {
				t.Errorf("%s --help Usage line should contain %q", tc.name, tc.useLine)
			}

			// Both the Use line and the note mention ID as positional — no contradiction.
			if !strings.Contains(out, "positional argument") {
				t.Errorf("%s --help should reinforce positional usage with a note", tc.name)
			}
		})
	}
}

// =======================================================================
// Boundary conditions
// =======================================================================

// TestPositionalArgHintIsConcise verifies that the positional arg hint is
// 2-3 lines (the "Note:" header plus correct/incorrect examples).
func TestPositionalArgHintIsConcise(t *testing.T) {
	commands := []struct {
		name string
		args []string
	}{
		{"contractors permits", []string{"contractors", "permits", "--help"}},
		{"contractors employees", []string{"contractors", "employees", "--help"}},
		{"contractors metrics", []string{"contractors", "metrics", "--help"}},
		{"contractors get", []string{"contractors", "get", "--help"}},
		{"permits get", []string{"permits", "get", "--help"}},
	}

	for _, tc := range commands {
		t.Run(tc.name, func(t *testing.T) {
			result := runCLI(t, tc.args...)

			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
			}

			lines := strings.Split(result.Stdout, "\n")
			noteIdx := -1
			for i, line := range lines {
				if strings.Contains(line, "Note:") && strings.Contains(line, "positional argument") {
					noteIdx = i
					break
				}
			}
			if noteIdx == -1 {
				t.Fatalf("%s --help should contain a 'Note:' line about positional arguments", tc.name)
			}

			// Count the note block: the "Note:" line plus indented continuation lines.
			hintLines := 1
			for i := noteIdx + 1; i < len(lines); i++ {
				trimmed := strings.TrimSpace(lines[i])
				if trimmed == "" {
					break
				}
				// Continuation lines are indented (Correct:/Incorrect: examples).
				if strings.HasPrefix(lines[i], "  ") {
					hintLines++
				} else {
					break
				}
			}

			if hintLines < 2 || hintLines > 3 {
				t.Errorf("%s positional arg hint should be 2-3 lines, got %d", tc.name, hintLines)
			}
		})
	}
}
