package ctable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE HEADER NAMES NO ROW (fixedform.go, NO NAME ON A ROW): a test finds a
// published dst or plan row by its place, and the place of a name is read
// from the same IR walks the emitter writes the rows from, in the same order.

// cNamedFixedRoot is the unit's fixed root named name.
func cNamedFixedRoot(t *testing.T, u *ir.Unit, name string) *ir.Struct {
	t.Helper()
	for _, st := range ir.TableFixedRoots(u) {
		if st.Name == name {
			return st
		}
	}
	t.Fatalf("the unit has no fixed root %s", name)
	return nil
}

// cFixedPlanNotes names the root's identity plan rows, in published order:
// the plan the emitter writes, its union tags split out, then split by width.
func cFixedPlanNotes(u *ir.Unit, st *ir.Struct) []string {
	entries := ir.TableFixedWalkRoot(st)
	plan, guarded, _ := ir.TableFixedBuildPlanPresentDefaultsWithPool(u, st)
	// the emitter keeps union tags as validation boundaries before the width split
	plan, guarded = fixedDeadMetaOf(u, st).splitUnionTags(plan, guarded)
	var notes []string
	for _, e := range ir.TableFixedSplitByWidth(entries, plan, guarded).Plan {
		notes = append(notes, e.Note)
	}
	return notes
}

// cFixedRow is the row of the array that opens at marker whose name, in
// notes, is note. A name two rows share, an array whose row count is not the
// walk's, or a missing array is red by name.
func cFixedRow(t *testing.T, text, marker string, notes []string, note string) string {
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
