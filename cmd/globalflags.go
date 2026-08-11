package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/shovels-ai/shovels-cli/internal/client"
	"github.com/shovels-ai/shovels-cli/internal/contract"
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// inheritedFlagsUsage is the expression cobra's default usage template uses to
// render the Global Flags section. Substituting it is what routes that template
// through the same filter as writeGroupedUsage; the rest of cobra's template is
// left alone.
const inheritedFlagsUsage = "{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}"

func init() {
	cobra.AddTemplateFunc("globalFlagUsages", globalFlagUsages)
	rootCmd.SetUsageTemplate(strings.Replace(
		rootCmd.UsageTemplate(), inheritedFlagsUsage, "{{globalFlagUsages .}}", 1))
}

// globalFlagUsages renders the body of a command's Global Flags section: the
// API-only flags its contract record honors, plus every other inherited flag.
//
// The filter runs at render time rather than marking flags Hidden because
// InheritedFlags hands back root's own *pflag.Flag pointers — hiding one hides
// it on every command.
func globalFlagUsages(cmd *cobra.Command) string {
	apiOnly := nameSet(contract.APIOnlyFlags())
	advertised := advertisedAPIOnlyFlags(cmd)

	fs := pflag.NewFlagSet("global", pflag.ContinueOnError)
	cmd.InheritedFlags().VisitAll(func(f *pflag.Flag) {
		if apiOnly[f.Name] && !advertised[f.Name] {
			return
		}
		fs.AddFlag(f)
	})
	return strings.TrimRight(fs.FlagUsages(), " \n")
}

// advertisedAPIOnlyFlags returns which of the API-only flags a command's help
// shows. Rejection reads the same answer, so for a runnable command this is
// equally the set it accepts.
//
// Root shows all five: it is where they are registered, and where an agent
// discovers they exist. A command with a record shows what that record honors.
// A command with no record shows all five, so an unclassified command errs
// towards advertising a flag that works rather than hiding one. A non-runnable
// parent has no record of its own, so it shows the union of its descendants' —
// what an agent can reach by naming a subcommand.
func advertisedAPIOnlyFlags(cmd *cobra.Command) map[string]bool {
	if cmd.Parent() == nil {
		return nameSet(contract.APIOnlyFlags())
	}
	if record, ok := contract.Lookup(contractPath(cmd)); ok {
		return nameSet(record.Honored)
	}
	if cmd.Runnable() {
		return nameSet(contract.APIOnlyFlags())
	}

	union := map[string]bool{}
	for _, sub := range cmd.Commands() {
		for name := range advertisedAPIOnlyFlags(sub) {
			union[name] = true
		}
	}
	return union
}

// rejectUnhonoredAPIOnlyFlags fails a command that was passed an API-only flag
// it does not honor, so an agent gets an error instead of a result computed as
// if the flag had never been typed.
//
// It reads the same advertised set the help filter renders, which is what makes
// "advertised" and "accepted" the same set rather than two that can drift.
// PersistentPreRunE is cobra's earliest per-command hook and does not run for a
// non-runnable parent, so the contract this enforces reaches runnable commands
// only.
func rejectUnhonoredAPIOnlyFlags(cmd *cobra.Command) error {
	unhonored := unhonoredAPIOnlyFlags(cmd)
	if len(unhonored) == 0 {
		return nil
	}

	verb := "is"
	if len(unhonored) > 1 {
		verb = "are"
	}
	msg := fmt.Sprintf("%s %s not supported by %q", strings.Join(unhonored, ", "), verb, contractPath(cmd))
	output.PrintErrorTyped(os.Stderr, msg, 1, client.ErrorTypeValidation)
	return &exitError{code: 1}
}

// unhonoredAPIOnlyFlags returns the dashed names of the API-only flags
// explicitly passed to a command whose contract does not cover them, in root's
// registration order so a message naming several is identical between runs.
//
// The check is Changed rather than presence: every one of these flags is
// registered on root and therefore present on every command, holding its
// default until someone types it.
func unhonoredAPIOnlyFlags(cmd *cobra.Command) []string {
	advertised := advertisedAPIOnlyFlags(cmd)

	var unhonored []string
	for _, name := range contract.APIOnlyFlags() {
		if !advertised[name] && cmd.Flags().Changed(name) {
			unhonored = append(unhonored, "--"+name)
		}
	}
	return unhonored
}

// nameSet indexes flag names for membership tests.
func nameSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}
