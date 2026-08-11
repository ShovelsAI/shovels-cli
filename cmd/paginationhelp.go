package cmd

import (
	"fmt"

	"github.com/shovels-ai/shovels-cli/internal/contract"
)

// serverCappedNote is the Long-text paragraph disclosing that a search's
// endpoint returns a fixed maximum number of results and no continuation
// cursor, so an agent can tell it apart from a cursor-paginated search without
// spending a credit to find out.
//
// The cap is read from the command's contract record — the same value the
// request path applies — so help cannot quote a number the CLI does not
// enforce. A path matching no capped record yields no paragraph at all, a
// silence the guard comparing help against the record reports.
func serverCappedNote(path string) string {
	record, ok := contract.Lookup(path)
	if !ok || record.Mode != contract.ModeServerCapped {
		return ""
	}
	return fmt.Sprintf(`
This search is not paginated: the endpoint returns at most %d results for a
query and no continuation cursor. --limit lowers the count below that cap
and no value raises it; narrow --query to reach the matches the cap leaves
out. The envelope reports "server_capped": %d with "has_more": false.
`, record.Cap, record.Cap)
}
