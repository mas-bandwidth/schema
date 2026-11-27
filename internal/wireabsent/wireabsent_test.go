package wireabsent

import (
	"strconv"
	"testing"
)

// The clause the harness prints is the clause the gate reads, whole: its
// count and its ledger come back unchanged. Reword Clause without ClauseRE
// (", 24 ledgered seeds missing (…)") and this fails.
func TestTheClauseReadsBack(t *testing.T) {
	for _, tc := range []struct {
		n        int
		leg, arm string
	}{{24, "go", "plain"}, {1, "lua", "retain"}, {330, "cs-region", "plain"}} {
		ledger := LedgerPath(Dir, tc.leg, tc.arm)
		c := Clause(tc.n, ledger)
		m := ClauseRE.FindAllStringSubmatch("wire-fuzz: seed 1, 9 seeds over 3 roots"+c+", 5 enumerated + 1 random = 6 mutants, 0 divergences", -1)
		if len(m) != 1 || m[0][0] != c {
			t.Fatalf("the gate does not read the harness's clause %q back: %q", c, m)
		}
		if m[0][1] != strconv.Itoa(tc.n) || m[0][2] != ledger {
			t.Fatalf("the clause %q reads back as %s seeds in %s", c, m[0][1], m[0][2])
		}
	}
	if got := LedgerPath(Dir, "go", "builder"); got != "test/conformance/wire-absent/go-builder.tsv" {
		t.Fatalf("the ledger path moved: %s", got)
	}
}
