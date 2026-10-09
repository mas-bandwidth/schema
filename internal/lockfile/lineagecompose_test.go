// THE RED TEAM AGAINST THE LOCK'S LINEAGE, BY COMPOSITION.
//
// lineage_test.go proves ONE widening at a time. This file proves what a
// release actually looks like: SEVERAL lawful widenings between one lock and
// the next, and three generations of them in a row. The lineage is one entry
// per LAYOUT, not one per change, and an older entry's LINE must come out of a
// re-lock byte for byte — a rewritten line is a rewritten history, and no
// command can recover the layout bytes of a record nobody declares any more
// (docs/FIXED-FORM-ALGORITHM.md §5.2).
package lockfile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
)

// lineageLines is the lock's lineage text for one table, the lines a hand could
// edit and the parse holds to its own recomputation.
func lineageLines(t *testing.T, path, table string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	in := false
	for line := range strings.SplitSeq(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "fixed table "):
			in = strings.Fields(trimmed)[2] == table
		case in && strings.HasPrefix(trimmed, "lineage "):
			out = append(out, trimmed)
		}
	}
	return out
}

const composeBefore = `enum Tier
{
    bronze
    silver
}
`

const composeAfter = `enum Tier
{
    bronze
    silver
    gold
}
`

// TestLockLineageSeveralWideningsInOneStepAddOneEntry: a release that appends a
// field AND grows an enum AND grows an array bound is ONE new layout, so it
// earns ONE lineage entry — and the entry that was there comes back verbatim.
func TestLockLineageSeveralWideningsInOneStepAddOneEntry(t *testing.T) {
	dir, paths := fixture(t, rowWith(composeBefore, "    t Tier\n    a [..2]int32\n    x int32"))
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, lockfile.FileName)
	first := lineageLines(t, path, "Row")
	if len(first) != 1 {
		t.Fatalf("the first lock holds one lineage line, got %d", len(first))
	}

	// THREE lawful widenings, in one step.
	if err := os.WriteFile(filepath.Join(dir, "Lock.schema"),
		[]byte(rowWith(composeAfter, "    t Tier\n    a [..4]int32\n    x int32\n    w int32 = 77")), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, rewrote, err := lockfile.Update(load(t, paths), paths); err != nil || !rewrote {
		t.Fatalf("`schema lock` takes three widenings at once: rewrote=%v err=%v", rewrote, err)
	}

	second := lineageLines(t, path, "Row")
	if len(second) != 2 {
		t.Fatalf("several widenings in one step are ONE layout: want 2 lineage lines, got %d:\n%s",
			len(second), strings.Join(second, "\n"))
	}
	if second[0] != first[0] {
		t.Errorf("the older entry's line was REWRITTEN:\n  before %s\n  after  %s", first[0], second[0])
	}
	if second[1] == first[0] {
		t.Error("the new entry is the same line as the old one; the fixture proves nothing")
	}
	// The lock must read back: the per-line recomputation and the set's roll-up
	// both hold after a composed step.
	if got := lineageOf(t, path, "Row"); len(got) != 2 || got[0].Retired {
		t.Fatalf("the committed lock does not parse back into two live entries: %d", len(got))
	}

	// A third generation: another composed step. The first two lines stay.
	if err := os.WriteFile(filepath.Join(dir, "Lock.schema"),
		[]byte(rowWith(composeAfter, "    t Tier\n    a [..8]int32\n    x int64\n    w int32 = 77\n    v int32 = 88")), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	third := lineageLines(t, path, "Row")
	if len(third) != 3 {
		t.Fatalf("a third generation is a third line, got %d", len(third))
	}
	if third[0] != first[0] || third[1] != second[1] {
		t.Errorf("the lineage only grows, and never rewrites:\n  %s\n  %s", strings.Join(second, "\n  "), strings.Join(third, "\n  "))
	}
}

// TestLockDeprecatedThenSameNameAppendRefused: a deprecated field keeps its
// slot and its name forever (bill §12.3), so a later release that APPENDS a
// field of the same name is two fields with one id — the plan would have two
// entries claiming one source — and it must not compile.
func TestLockDeprecatedThenSameNameAppendRefused(t *testing.T) {
	dir, paths := fixture(t, rowTable("    a int32\n    b int32 | deprecated\n    c int32"))
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Lock.schema"),
		[]byte(rowTable("    a int32\n    b int32 | deprecated\n    c int32\n    b int32")), 0o644); err != nil {
		t.Fatal(err)
	}
	// The re-declaration is refused BEFORE the lock is consulted: a duplicate
	// field name is not a versioning question.
	if _, err := compiler.New().Load(paths); err == nil {
		t.Fatal("a field whose name a deprecated field already holds must not compile")
	}
}

// TestLockDeprecatedThenOtherNameAppendAllowed is the control: the same shape
// with a DIFFERENT name is an ordinary append over a deprecated slot.
func TestLockDeprecatedThenOtherNameAppendAllowed(t *testing.T) {
	rowAllows(t,
		rowTable("    a int32\n    b int32 | deprecated\n    c int32"),
		rowTable("    a int32\n    b int32 | deprecated\n    c int32\n    d int32"),
		"Row")
}
