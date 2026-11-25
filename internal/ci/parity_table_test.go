package ci

import (
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testinputs"
)

// TestTheParityTableIsCovered holds the parity table
// (test/testguard/parity.sh): every shared test family against every leg is
// covered, and a family or leg with no runner is red by its pair. It ran as
// the ci-policy job's testguard coverage table; with the policy tests
// registry rows of the pr group, it is one of them. The table lists no gap,
// so every pair of rows x columns must be among the pairs.
func TestTheParityTableIsCovered(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	testinputs.Files(t, "test/testguard/parity.sh", "make/wire-fuzz-release.mk", "test/conformance/wire-fuzz-owed.txt")
	list := func(what string) []string {
		// the script lists internal/codegen/*table and test/conformance/<leg>
		// as directories; the files it reads are registered above
		cmd := exec.Command("sh", "test/testguard/parity.sh", what)
		cmd.Dir = root
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("sh test/testguard/parity.sh %s: %v", what, err)
		}
		return strings.Fields(strings.ReplaceAll(string(out), "\t", " "))
	}
	rows, columns := list("rows"), list("columns")
	var pairs []string
	fields := list("pairs")
	if len(fields)%2 != 0 {
		t.Fatalf("parity.sh pairs printed an odd number of fields: %v", fields)
	}
	for i := 0; i < len(fields); i += 2 {
		pairs = append(pairs, fields[i]+" "+fields[i+1])
	}
	if len(rows) == 0 || len(columns) == 0 {
		t.Fatalf("the parity table is empty: rows %v, columns %v", rows, columns)
	}
	if missing := parityUncovered(rows, columns, pairs); len(missing) > 0 {
		t.Fatalf("parity pairs no runner covers (family leg): %s", strings.Join(missing, "; "))
	}
	// negative control: the same table without one pair is red by that pair
	if got := parityUncovered(rows, columns, pairs[1:]); !slices.Equal(got, pairs[:1]) {
		t.Fatalf("NEGATIVE CONTROL FAILED: dropping pair %q reported %v", pairs[0], got)
	}
}

// parityUncovered is every "row column" pair of the table not among pairs,
// in row then column order.
func parityUncovered(rows, columns, pairs []string) []string {
	have := map[string]bool{}
	for _, p := range pairs {
		have[p] = true
	}
	var out []string
	for _, r := range rows {
		for _, c := range columns {
			if !have[r+" "+c] {
				out = append(out, r+" "+c)
			}
		}
	}
	return out
}
