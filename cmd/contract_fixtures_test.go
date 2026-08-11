//go:build e2e

package cmd

import (
	"testing"

	"github.com/shovels-ai/shovels-cli/internal/contract"
)

// The fixture commands exist only under the e2e build tag, so their records are
// present only under it too. That the tree walk sees them classified at all is
// TestEveryRunnableCommandHasAValidContractRecord run under this tag; what those
// records say is here.

// --- Boundary conditions ---

// No fixture implements a dry-run branch, so recording one as honoring
// --dry-run would bake accept-then-ignore into the authoritative table.
func TestNoFixtureCommandHonorsDryRun(t *testing.T) {
	for _, path := range []string{"_test-paginate", "_test-single", "_test-http", "_test-auth"} {
		t.Run(path, func(t *testing.T) {
			record, ok := contract.Lookup(path)
			if !ok {
				t.Fatalf("%s has no contract record", path)
			}

			if record.Honors(contract.FlagDryRun) {
				t.Errorf("%s reads no --dry-run flag, so its record must not honor it", path)
			}
		})
	}
}
