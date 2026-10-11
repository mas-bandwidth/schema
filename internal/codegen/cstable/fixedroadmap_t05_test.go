package cstable

// fixedroadmap_t05 — the cs leg's text row of docs/roadmap.sexp's node
// `fixed-tables` (ROADMAP.md "NEW Fixed Tables"): the direct assertions of the
// clauses no other test of this leg makes. Law: docs/SPEC-TABLES.md §3.4 (the
// fixed form's record — its text fields ride as a length word and a buffer),
// docs/FIXED-FORM-ALGORITHM.md §5.4 (the counters) and
// docs/FIXED-FORM-VERSIONING-TESTS.md (the text rows C3/C5/E9). One subtest per
// task id, and the assertion under each is the clause the task's own title
// states, read off the C# this leg emits or off the shared runtime it ships.
//
// This leg's read/write probe needs `dotnet`, which is absent in this checkout;
// the assertions below are made on the emitted source and the shipped runtime
// text alone, so they run and bite wherever the Go compiler runs.

import (
	"strings"
	"testing"
)

// t05Has is a required substring, named by rule. It fails rather than answering
// "" so a moved clause is red by name.
func t05Has(t *testing.T, got, want, what string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("%s: the emitted text does not carry %q", what, want)
	}
}

func TestFixedRoadmapCSharpT05Evolution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"cs/E9", t05E9},
		{"cs/C3", t05C3},
		{"cs/C5", t05C5},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t05E9: E9 [verify] "duplicate never raised". The fixed form is positional —
// one record is a hash and the fields in declared order — so no field or slot
// can repeat and no duplicate-key test exists to raise the report's Duplicate
// (§5.4's counter list, SPEC-TABLES §3.4). The report still DECLARES the member
// (it is the shared TableReport), so the clause is that nothing increments it.
func t05E9(t *testing.T) {
	full := generateCS(t, `package probe

fixed table Dup
{
    a uint32 = 1
    b uint32 = 2
}
`)
	// The unit's variable-form helpers carry their own duplicate tests; the
	// fixed form is the region from its own banner, and that is what the
	// positional record's clause is about.
	at := strings.Index(full, "// ---- THE FIXED FORM, form byte 3")
	if at < 0 {
		t.Fatal("E9: the emitted unit has no fixed form")
	}
	src := full[at:]
	if i := strings.Index(src, "Duplicate++"); i >= 0 {
		t.Errorf("E9: an increment reaches report.Duplicate at byte %d; a fixed record is positional and never raises a duplicate:\n%s", i, src[i-40:i+20])
	}
	if i := strings.Index(src, "Duplicate +="); i >= 0 {
		t.Errorf("E9: an add reaches report.Duplicate at byte %d; a fixed record is positional and never raises a duplicate:\n%s", i, src[i-40:i+20])
	}
	// The control: the report member is present, so the absence above is a
	// missing INCREMENT and not a missing field.
	t05Has(t, full, "Duplicate", "E9 the shared report still declares the counter the read never raises")
}

// t05C3: C3 [weak] "text length clamp". A text field rides as a count word and
// a buffer; the reader's Text op clamps a forged length to the buffer's capacity
// in the field's own units and counts ONE clamp (§5.4, SPEC-TABLES §3.4).
func t05C3(t *testing.T) {
	op := t05Op(t, "case Text:")
	t05Has(t, op, "uint cap = p.Size / unit;",
		"C3 the text length is bounded by the buffer's capacity")
	t05Has(t, op, "else if ((uint)v > cap) { v = (int)cap; if (report != null) report.Clamped++; }",
		"C3 a forged length past the capacity clamps at the capacity and counts once")
	t05Has(t, op, "if (v < 0) { v = 0; if (report != null) report.Clamped++; }",
		"C3 a negative length clamps to zero and counts once")
}

// t05C5: C5 [weak] "wide text code units". A wstring's length and capacity are
// counted in two-byte code units, not bytes: the op's unit is 2 for a wide text,
// the capacity is the byte buffer divided by that unit, and the buffer lands as
// chars, never as bytes (§5.4, SPEC-TABLES §3.4).
func t05C5(t *testing.T) {
	op := t05Op(t, "case Text:")
	t05Has(t, op, "uint unit = (p.Meta == TextWide) ? 2u : 1u;",
		"C5 a wide text counts two bytes to the code unit")
	t05Has(t, op, "slots[(int)p.Aux].SetChars?.Invoke(dst, textBytes, v);",
		"C5 a wide text lands as chars")
	t05Has(t, op, "slots[(int)p.Aux].SetBytes?.Invoke(dst, textBytes, v);",
		"C5 a narrow text lands as bytes")
	// The wide field's own emitted copy treats the buffer as chars.
	src := generateCS(t, `package probe

fixed table Wide
{
    label wstring(8)
}
`)
	t05Has(t, src, "MemoryMarshal.Cast<byte, char>(b)",
		"C5 the emitted wide-text slot reads its buffer as code units")
}

// t05Op is the body of one op in the shipped fixed runtime, from the case label
// to the next `case` at the same indentation. t.Fatal rather than "" so a moved
// op is red by name.
func t05Op(t *testing.T, label string) string {
	t.Helper()
	at := strings.Index(tableFixedWireSource, label)
	if at < 0 {
		t.Fatalf("the fixed runtime has no %q", label)
	}
	rest := tableFixedWireSource[at+len(label):]
	end := strings.Index(rest, "\n                    case ")
	if end < 0 {
		t.Fatalf("the fixed runtime's %q runs to the end of the switch", label)
	}
	return rest[:end]
}
