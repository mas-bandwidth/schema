package main

import (
	"strings"
	"testing"
)

// A two-sided conflict on one register row: ours changed the go cell, theirs
// changed the java cell and added a column. Merged cell by cell by column name.
const twoSided = `### M5 — keyed

<<<<<<< ours
| cpp | go | java |
|---|---|---|
| ✅ a | ✅ go landed | ❌ #1 |
||||||| base
| cpp | go | java |
|---|---|---|
| ✅ a | ❌ #2 | ❌ #1 |
=======
| cpp | go | java | moo |
|---|---|---|---|
| ✅ a | ❌ #2 | ✅ java landed | — |
>>>>>>> theirs
`

// Both sides changed the same cell differently: no merge is exact, so the hunk
// stays whole for a person.
const sameCell = `<<<<<<< ours
| cpp | go |
|---|---|
| ✅ a | ✅ one way |
||||||| base
| cpp | go |
|---|---|
| ✅ a | ❌ #2 |
=======
| cpp | go |
|---|---|
| ✅ a | ✅ another way |
>>>>>>> theirs
`

// Prose is not a table row; the hunk stays whole.
const prose = `<<<<<<< ours
The method reads one way.
||||||| base
The method reads.
=======
The method reads another way.
>>>>>>> theirs
`

func TestResolveMergesATwoSidedRowCellByColumn(t *testing.T) {
	out, resolved, left := Resolve(twoSided)
	if resolved != 1 || len(left) != 0 {
		t.Fatalf("resolved %d, left %v; want 1 resolved, none left\n%s", resolved, left, out)
	}
	want := "| cpp | go | java | moo |\n|---|---|---|---|\n| ✅ a | ✅ go landed | ✅ java landed | — |\n"
	if !strings.Contains(out, want) {
		t.Fatalf("merged row is not both sides' cells by column:\n%s", out)
	}
	if strings.Contains(out, "<<<<<<<") || strings.Contains(out, ">>>>>>>") {
		t.Fatalf("conflict markers remain:\n%s", out)
	}
}

func TestResolveLeavesAHunkItCannotMergeExactly(t *testing.T) {
	for name, src := range map[string]string{"same cell": sameCell, "prose": prose} {
		out, resolved, left := Resolve(src)
		if resolved != 0 || len(left) != 1 || left[0] != 1 {
			t.Errorf("%s: resolved %d, left %v; want the hunk at line 1 left", name, resolved, left)
		}
		if out != src {
			t.Errorf("%s: the hunk was rewritten:\n%s", name, out)
		}
	}
}
