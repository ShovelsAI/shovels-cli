package cmd

import (
	"strings"

	"github.com/spf13/cobra"
)

// contractPath is the key a command's contract record is filed under: its
// command path with the root's own name dropped, so "shovels cities search"
// becomes "cities search". Root itself maps to the empty path — it carries no
// record, being both non-runnable and the one command that renders every flag.
func contractPath(cmd *cobra.Command) string {
	if cmd.Parent() == nil {
		return ""
	}
	return strings.TrimPrefix(cmd.CommandPath(), cmd.Root().Name()+" ")
}
