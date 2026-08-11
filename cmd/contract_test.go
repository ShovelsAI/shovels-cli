package cmd

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/shovels-ai/shovels-cli/internal/contract"
	"github.com/spf13/cobra"
)

// runnableCommands returns every runnable command reachable from root.
//
// help and the completion leaves are registered lazily during Execute, so the
// walk forces both first — without that, five real user-facing commands would
// never be classified. Cobra's completion plumbing (__complete, and
// __completeNoDesc as its alias) is excluded: it is not user-facing surface and
// carries no contract.
func runnableCommands(root *cobra.Command) []*cobra.Command {
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()

	var found []*cobra.Command
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Runnable() && c.Name() != cobra.ShellCompRequestCmd {
			found = append(found, c)
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
	return found
}

// contractProblems reports every disagreement between the command tree and the
// contract table: a runnable command with no record, a record naming no runnable
// command, and a record that is not internally consistent. The table is a
// parameter so a case can pose a tree and a table that do not match.
func contractProblems(root *cobra.Command, records map[string]contract.Record) []string {
	var problems []string
	classified := map[string]bool{}

	for _, cmd := range runnableCommands(root) {
		path := contractPath(cmd)
		record, ok := records[path]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: runnable command has no contract record", path))
			continue
		}
		classified[path] = true
		if err := record.Validate(); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", path, err))
		}
	}

	for path := range records {
		if !classified[path] {
			problems = append(problems, fmt.Sprintf("%s: contract record names no runnable command", path))
		}
	}

	slices.Sort(problems)
	return problems
}

// --- Happy paths ---

func TestEveryRunnableCommandHasAValidContractRecord(t *testing.T) {
	problems := contractProblems(rootCmd, contract.All())

	if len(problems) > 0 {
		t.Errorf("command tree and contract table disagree:\n  %s", strings.Join(problems, "\n  "))
	}
}

// --- Edge cases ---

func TestNonRunnableParentsCarryNoContractRecord(t *testing.T) {
	var parents []string
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if !c.Runnable() && c.Parent() != nil {
			if _, ok := contract.Lookup(contractPath(c)); ok {
				parents = append(parents, contractPath(c))
			}
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)

	if len(parents) > 0 {
		t.Errorf("parents carry no contract; found records for %v", parents)
	}
}

func TestWalkCoversTheLazilyRegisteredCommands(t *testing.T) {
	var paths []string
	for _, cmd := range runnableCommands(rootCmd) {
		paths = append(paths, contractPath(cmd))
	}

	for _, path := range []string{"help", "completion bash", "completion fish", "completion zsh", "completion powershell"} {
		if !slices.Contains(paths, path) {
			t.Errorf("walk should cover the lazily registered %q, got %v", path, paths)
		}
	}
}

// --- Error conditions ---

func TestGuardNamesARunnableCommandWithNoRecord(t *testing.T) {
	root := &cobra.Command{Use: "shovels"}
	root.AddCommand(&cobra.Command{Use: "unclassified", Run: func(*cobra.Command, []string) {}})

	problems := contractProblems(root, map[string]contract.Record{})

	if !slices.Contains(problems, "unclassified: runnable command has no contract record") {
		t.Errorf("guard should name the unclassified command, got %v", problems)
	}
}

func TestGuardNamesARecordWithNoCommand(t *testing.T) {
	root := &cobra.Command{Use: "shovels"}
	records := map[string]contract.Record{"retired search": {Mode: contract.ModeNone}}

	problems := contractProblems(root, records)

	want := []string{"retired search: contract record names no runnable command"}
	if !slices.Equal(problems, want) {
		t.Errorf("expected %v, got %v", want, problems)
	}
}

func TestGuardNamesACommandWhoseRecordIsInconsistent(t *testing.T) {
	root := &cobra.Command{Use: "shovels"}
	root.AddCommand(&cobra.Command{Use: "capped", Run: func(*cobra.Command, []string) {}})
	records := map[string]contract.Record{
		"capped": {Honored: contract.APIOnlyFlags(), Mode: contract.ModeServerCapped},
	}

	problems := contractProblems(root, records)

	if !slices.ContainsFunc(problems, func(p string) bool { return strings.HasPrefix(p, "capped: ") }) {
		t.Errorf("guard should name the command with the inconsistent record, got %v", problems)
	}
}
