package cmd

import (
	"fmt"
	"strings"

	"github.com/shovels-ai/shovels-cli/internal/contract"
	"github.com/spf13/cobra"
)

// longDescription is the expression cobra's default help template renders a
// command's Long text with. Appending to it is what puts the pagination
// paragraph on every command that has one, from the record alone: a command
// added later is described without anyone remembering to describe it.
const longDescription = "{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}"

func init() {
	cobra.AddTemplateFunc("paginationNote", paginationNote)
	rootCmd.SetHelpTemplate(strings.Replace(
		rootCmd.HelpTemplate(), longDescription, longDescription+"{{paginationNote $}}", 1))
}

// paginationNote is the help paragraph stating how many records a command can
// return and how to reach the rest, so an agent can tell a capped search from a
// cursor-paginated one without spending a credit to find out.
//
// Both paragraphs come from the command's contract record — the same record the
// request path reads — so help cannot describe pagination the CLI does not
// perform. A command whose record declares neither mode gets no paragraph, and
// the guards comparing help against the record report either silence.
func paginationNote(cmd *cobra.Command) string {
	record, ok := contract.Lookup(contractPath(cmd))
	if !ok {
		return ""
	}

	// Each paragraph opens on a blank line and closes without one: the help
	// template trims the Long text and supplies the break that follows.
	switch record.Mode {
	case contract.ModeServerCapped:
		return fmt.Sprintf(`

This search is not paginated: the endpoint returns at most %d results for a
query and no continuation cursor. --limit lowers the count below that cap
and no value raises it; narrow --query to reach the matches the cap leaves
out. The envelope reports "server_capped": %d with "has_more": false.`, record.Cap, record.Cap)
	case contract.ModeCursor:
		return `

This command is cursor-paginated: --limit N stops after N records, and
--limit all follows the cursor to the --max-records ceiling. "has_more":
true means the server holds more — raise --limit to reach them, or
--max-records when --limit is already all.`
	default:
		return ""
	}
}
