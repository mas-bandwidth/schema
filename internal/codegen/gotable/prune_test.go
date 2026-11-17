package gotable

import (
	"strings"
	"testing"
)

func TestPruneDropsDeclarationsWithTheirComments(t *testing.T) {
	t.Parallel()
	src := `package probe

import goruntime "example.com/goruntime"

const _ = uint64(goruntime.Version - 1)

// tableDeadShim names a runtime constant.
const tableDeadShim = goruntime.DeadShim

const tableLiveShim = goruntime.LiveShim

const (
	tableFirst = iota
	tableSecond
	tableThird
)

// tableDeadRule reads as the rule.
func tableDeadRule() int {
	// the first pass: every check
	x := 1
	// the second pass
	return x
}

// Use is the API.
func Use() int { return tableLiveShim + tableThird }
`
	files, err := pruneUnreferencedDeclarations(map[string][]byte{"probe.go": []byte(src)}, map[string]bool{})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	out := string(files["probe.go"])
	for _, gone := range []string{
		"tableDeadRule", "reads as the rule", "the first pass", "the second pass",
		"tableDeadShim", "names a runtime constant", "goruntime.DeadShim",
	} {
		if strings.Contains(out, gone) {
			t.Errorf("pruned unit still has %q:\n%s", gone, out)
		}
	}
	for _, kept := range []string{
		"const _ = uint64(goruntime.Version - 1)",
		"const tableLiveShim = goruntime.LiveShim",
		"tableThird",
		"// Use is the API.",
	} {
		if !strings.Contains(out, kept) {
			t.Errorf("pruned unit lost %q:\n%s", kept, out)
		}
	}
	// an iota group keeps every position: an unused member becomes blank
	if !strings.Contains(out, "\t_ = iota\n\t_\n\ttableThird\n") {
		t.Errorf("iota group lost a position:\n%s", out)
	}
}
