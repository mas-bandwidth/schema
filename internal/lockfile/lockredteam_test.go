// THE LOCK, RED TEAMED: two pairs the law allowed and the read could not hold
// (the red-team pass of 2026-09-11, against docs/FIXED-FORM-BILL-READS-BACKWARD.md
// §2 and §6 and docs/FIXED-FORM-VERSIONING-TESTS.md).
//
// Both are the same mistake in two places: a fact the lock compares ENTRY BY
// ENTRY, when the fact belongs to something the entry only points at.
//
//   - an array's ELEMENT WIDTH belongs to the element's own block, so an
//     appended field in `Vec` was read as a change to every `[4]Vec` — the lock
//     REFUSED a widening the bill allows, in a sentence that named no change
//     ("element kind changed (table -> table)"), and a widening the lock refuses
//     is a table that cannot be versioned at all;
//   - a record's SIZE belongs to the FORM, and past §3.4's 65536 the fixed form
//     is not carried. Every monotone fact widened legally while the wire changed
//     underneath, so the lock ALLOWED a bound grown across the ceiling and the
//     lineage gained an entry for a layout nothing writes.
//
// The idiom is lockrows_test.go's, and the shapes are its shapes.
package lockfile_test

import (
	"strings"
	"testing"
)

// ---- the element of an array is a named type: its width is its own block's ----

// TestLockArrayOfNestedAppendAllows guards the bill's `nested_append` row ONE
// LEVEL IN: the law for a type a fixed record reaches by value is that type's
// own law, recursively, and an ARRAY of it is reached by value just as surely as
// a single field is. So `Vec` gaining a field is an append in Vec's block and
// nothing at all in `[4]Vec`'s entry, whose width moved because the element's
// did.
func TestLockArrayOfNestedAppendAllows(t *testing.T) {
	for _, shape := range []string{"[4]Vec", "[..4]Vec"} {
		t.Run(shape, func(t *testing.T) {
			rowAllows(t,
				rowWith(vecType("    x int32\n    y int32"), "    v "+shape),
				rowWith(vecType("    x int32\n    y int32\n    z int32"), "    v "+shape),
				"Vec")
		})
	}
}

// TestLockArrayOfNestedRemoveRefuses is the other half, and it is why the half
// above is safe: a NARROWING of the element is refused where the element's own
// law lives, naming the type and the field, not the array that holds it.
func TestLockArrayOfNestedRemoveRefuses(t *testing.T) {
	rowRefuses(t,
		rowWith(vecType("    x int32\n    y int32"), "    v [4]Vec"),
		rowWith(vecType("    x int32"), "    v [4]Vec"),
		"type Vec", "field y", "field removed")
}

// TestLockArrayOfScalarNarrowStillRefuses holds the fix to its own scope: an
// array whose element is a SCALAR has no block of its own to be refused in, so
// the element ladder is still the law there (`array_elem_widen`).
func TestLockArrayOfScalarNarrowStillRefuses(t *testing.T) {
	rowRefuses(t, rowTable("    a [..4]int32"), rowTable("    a [..4]int16"),
		"fixed table Row", "field a", "element narrowed")
}

// ---- the record's size is the FORM's, and the form has a ceiling ----

// TestLockRecordPastCeilingRefuses guards §3.4's 65536 AS A MONOTONE FACT. Past
// it a fixed table does not carry the fixed form — it keeps form 1 — so a bound
// or a capacity grown across the ceiling is not a widening every reader can hold
// but a CHANGE OF FORM, which the bill spells out is "a different form, not a
// version". Every fact the lock compares widened legally, so without this
// refusal `schema lock` wrote the crossing down and the readers compiled from
// the lineage were left holding a layout nothing writes.
func TestLockRecordPastCeilingRefuses(t *testing.T) {
	t.Run("a bound grown across it", func(t *testing.T) {
		rowRefuses(t, rowTable("    a [4]int32"), rowTable("    a [20000]int32"),
			"fixed table Row", "record past the form's 65536-byte ceiling (16 -> 80000)")
	})
	t.Run("a capacity grown across it", func(t *testing.T) {
		rowRefuses(t, rowTable("    a string(8)"), rowTable("    a string(70000)"),
			"fixed table Row", "record past the form's 65536-byte ceiling (12 -> 70004)")
	})
}

// TestLockRecordAlreadyPastCeilingAllows is the asymmetry the ceiling needs: a
// table that was ALWAYS past it never carried the fixed form, so it crosses
// nothing and the lock says nothing — §12.1's render frame and §2.8's `WideBlob`
// are declared `fixed table` for the CLASS and were never form-3 tables.
func TestLockRecordAlreadyPastCeilingAllows(t *testing.T) {
	rowAllows(t, rowTable("    a [20000]int32"), rowTable("    a [30000]int32"), "Row")
}

// TestLockRecordInsideCeilingAllows keeps the refusal off the ordinary grow: a
// bound that grows and stays inside the ceiling is the `array_fixed_grow` row
// and nothing else.
func TestLockRecordInsideCeilingAllows(t *testing.T) {
	rowAllows(t, rowTable("    a [4]int32"), rowTable("    a [8]int32"), "Row")
}

// TestLockCeilingRefusalIsAlsoScrewedDownInLock is the LOCK-REFUSES column's
// other half for this row: `schema lock` itself must not write the crossing, or
// the one command that moves the file would be the one that breaks the law.
func TestLockCeilingRefusesUnderSchemaLock(t *testing.T) {
	errs := lockThen(t, rowTable("    a [4]int32"), rowTable("    a [20000]int32"))
	if len(errs) != 1 {
		t.Fatalf("want one refusal, got %d: %v", len(errs), errs)
	}
	if strings.Contains(errs[0].Error(), "write it with `schema lock`") {
		t.Fatalf("the ceiling is a refusal, never a stale lock: %v", errs[0])
	}
}
