package slowtest

import (
	"strings"
	"testing"
)

// SkipRow ends a table row that does not run, and says why.
//
// A row that a test cannot run is a SKIP the skip ledger counts
// (.github/skip-ledger.txt), never a t.Logf and a continue: a logged row is
// invisible to every lane, and the table reads as if it had been run. The
// reason is the ledger's text, so it is required: an empty one fails here
// rather than skipping with nothing for the ledger to read.
func SkipRow(t testing.TB, reason string) {
	t.Helper()
	if strings.TrimSpace(reason) == "" {
		t.Fatalf("SkipRow was given no reason: a skipped row names what is owed or why it does not apply, in words the skip ledger matches")
		return
	}
	t.Skip(reason)
}
