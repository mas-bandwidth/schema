package cpptable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE HEADER NAMES NO ROW (fixedform.go, NO NAME ON A ROW): a test finds a
// published dst or plan row by its place, and the place of a name is read
// from the same IR walks the emitter writes the rows from, in the same order.

// cppFixedPlanNotes names the root's identity plan rows, in published order:
// the width-split plan the emitter writes.
func cppFixedPlanNotes(u *ir.Unit, st *ir.Struct) []string {
	entries := ir.TableFixedWalkRoot(st)
	plan, guarded, _ := ir.TableFixedBuildPlanPresentDefaultsWithPool(u, st)
	var notes []string
	for _, e := range ir.TableFixedSplitByWidth(entries, plan, guarded).Plan {
		notes = append(notes, e.Note)
	}
	return notes
}

// cppFixedRow is the row of the array that opens at marker whose name, in
// notes, is note. A name two rows share, an array whose row count is not the
// walk's, or a missing array is red by name.
func cppFixedRow(t *testing.T, text, marker string, notes []string, note string) string {
	t.Helper()
	at := -1
	for i, n := range notes {
		if n == note {
			if at >= 0 {
				t.Fatalf("two rows are named %q; a row cannot be found by it", note)
			}
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("no row is named %q (the walk names %q)", note, notes)
	}
	open := strings.Index(text, marker)
	if open < 0 {
		t.Fatalf("the header publishes no %q", marker)
	}
	body, _, ok := strings.Cut(text[open+len(marker):], "};")
	if !ok {
		t.Fatalf("the header's %q has no closing brace", marker)
	}
	var rows []string
	for line := range strings.SplitSeq(body, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			rows = append(rows, line)
		}
	}
	if len(rows) != len(notes) {
		t.Fatalf("%q publishes %d rows, the walk names %d", marker, len(rows), len(notes))
	}
	return rows[at]
}

// cppFixedDstNotes names the root's destination rows, in published order:
// the layout walk the emitter writes them from.
func cppFixedDstNotes(st *ir.Struct) []string {
	var notes []string
	for _, e := range ir.TableFixedWalkRoot(st) {
		notes = append(notes, e.Note)
	}
	return notes
}

// cppOnlyFixedRoot is the unit's one fixed root.
func cppOnlyFixedRoot(t *testing.T, u *ir.Unit) *ir.Struct {
	t.Helper()
	roots := ir.TableFixedRoots(u)
	if len(roots) != 1 {
		t.Fatalf("want one fixed root, the unit has %d", len(roots))
	}
	return roots[0]
}
