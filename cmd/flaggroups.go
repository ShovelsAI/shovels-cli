package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// flagGroup defines a named category of flags for grouped help output.
type flagGroup struct {
	Title string
	Names []string
}

// setGroupedUsage overrides a command's usage function to render local flags
// organized into named groups instead of a single flat alphabetical list.
// Flags not listed in any group (including --help) appear in an "Other Flags"
// section. Inherited (global) flags render in the standard "Global Flags"
// section.
func setGroupedUsage(cmd *cobra.Command, groups []flagGroup) {
	cmd.SetUsageFunc(func(c *cobra.Command) error {
		w := c.OutOrStderr()
		writeGroupedUsage(w, c, groups)
		return nil
	})
}

// writeGroupedUsage renders the full usage output with grouped local flags.
func writeGroupedUsage(w io.Writer, cmd *cobra.Command, groups []flagGroup) {
	// Usage line
	if cmd.Runnable() {
		fmt.Fprintf(w, "Usage:\n  %s\n", cmd.UseLine())
	}

	// Long description is rendered by Cobra's help function before usage,
	// so we only render groups and global flags here.

	// Build a set of all flag names claimed by a group.
	claimed := map[string]bool{}
	for _, g := range groups {
		for _, name := range g.Names {
			claimed[name] = true
		}
	}

	// Render each group.
	for _, g := range groups {
		fs := buildFlagSet(cmd, g.Names)
		usage := fs.FlagUsages()
		if usage == "" {
			continue
		}
		fmt.Fprintf(w, "\n%s:\n%s", g.Title, usage)
	}

	// Render unclaimed local flags (e.g. --help).
	unclaimed := &pflag.FlagSet{}
	cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if !claimed[f.Name] {
			unclaimed.AddFlag(f)
		}
	})
	if unclaimedUsage := unclaimed.FlagUsages(); unclaimedUsage != "" {
		fmt.Fprintf(w, "\nOther Flags:\n%s", unclaimedUsage)
	}

	// Render inherited (global) flags.
	if cmd.HasAvailableInheritedFlags() {
		fmt.Fprintf(w, "\nGlobal Flags:\n%s",
			strings.TrimRight(cmd.InheritedFlags().FlagUsages(), " \n")+"\n")
	}
}

// buildFlagSet creates a pflag.FlagSet containing only the named flags from
// the command's local flags. Flags that do not exist on the command are
// silently skipped.
func buildFlagSet(cmd *cobra.Command, names []string) *pflag.FlagSet {
	fs := pflag.NewFlagSet("group", pflag.ContinueOnError)
	for _, name := range names {
		if f := cmd.LocalFlags().Lookup(name); f != nil {
			fs.AddFlag(f)
		}
	}
	return fs
}
