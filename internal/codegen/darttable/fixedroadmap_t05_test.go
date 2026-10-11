package darttable

// THE DART LEG'S FIXED-TABLE text ROW (docs/roadmap.sexp node `fixed-tables`,
// work-set `text/dart`). Each subtest is one roadmap task id and asserts the
// clause its own title states, quoting the sentence it implements. Law:
// docs/SPEC-TABLES.md §3.4, docs/FIXED-FORM-ALGORITHM.md §5.4,
// ROADMAP.md "NEW Fixed Tables".
//
// The harness is TABLE-DRIVEN, one subtest per task id. A clause an existing
// test of this leg already holds is named by that test in the card's verdicts
// and is not repeated here.

import (
	"strings"
	"testing"
)

func TestFixedRoadmapDartT05Evolution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"dart/C3", t05C3},
		{"dart/C5", t05C5},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t05C3: dart/C3 "text length clamp". docs/FIXED-FORM-ALGORITHM.md §5.4: the
// `text` op moves `clamped` "once per entry per record, when the length or
// count was out of range", against THIS READER'S OWN BOUND. The generated
// runtime's one read loop carries that clamp: a used length past the reader's
// capacity is clamped to the capacity and counted, so a consumer never indexes
// past its own storage.
func t05C3(t *testing.T) {
	const src = `package probe

fixed table T
{
    label string(8)
}
`
	u := unitFrom(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("C3: generate: %v", err)
	}
	text := t02Source(t, files)

	// THE CLAMP IS ON THE READER'S OWN CAP, not the writer's declared length:
	// `cap = size ~/ unit`, and a `used` past it is set to `cap` and counted.
	if !strings.Contains(text, "final cap = size ~/ unit;") {
		t.Error("C3: the generated text op derives no capacity from the reader's own entry size — a text length is clamped to the reader's bound")
	}
	if !strings.Contains(text, "} else if (used > cap) {") ||
		!strings.Contains(text, "used = cap;\n          report.clamped++;") {
		t.Error("C3: the generated text op does not clamp a used length past the reader's cap and count it — `clamped` moves once per entry per record when the length was out of range")
	}
}

// t05C5: dart/C5 "wide text code units". docs/SPEC-TABLES.md §3.4 and
// docs/FIXED-FORM-ALGORITHM.md §5.4: the `text` op's flavour is `textUtf8`,
// `textWide` or `textBytes`, and a `wstring(N)` is N CODE UNITS of TWO BYTES
// each. This leg's plan carries the flavour on `meta` and the byte extent on
// `size`, so a wide field's entry spends `2*N` bytes and the loop's unit is 2.
func t05C5(t *testing.T) {
	const src = `package probe

fixed table T
{
    narrow string(8)
    wide   wstring(8)
    blob   bytes(8)
}
`
	u := unitFrom(t, src)
	st := findTable(t, u, "T")
	plan := fixedIdentityPlan(st)

	byNote := map[string]fixedPlanEntry{}
	for _, e := range plan {
		if e.op == fixedOpText {
			byNote[e.note] = e
		}
	}
	narrow, okN := byNote["narrow"]
	wide, okW := byNote["wide"]
	blob, okB := byNote["blob"]
	if !okN || !okW || !okB {
		t.Fatalf("C5: the identity plan carries text entries for narrow=%v wide=%v blob=%v", okN, okW, okB)
	}

	// A WIDE TEXT'S EXTENT IS TWO BYTES PER DECLARED UNIT — 8 code units are
	// 16 bytes — and its flavour is textWide.
	if wide.size != 2*8 {
		t.Errorf("C5: the wstring(8) entry's size is %d, want 16 — a wide code unit is two bytes", wide.size)
	}
	if wide.meta != fixedTextWide {
		t.Errorf("C5: the wstring(8) entry's flavour is %d, want textWide (%d)", wide.meta, fixedTextWide)
	}
	// A NARROW TEXT IS ONE BYTE PER UNIT AND textUtf8; a bytes(N) run is
	// textBytes, which is the flavour a text length must not confuse with a
	// code-unit count.
	if narrow.size != 8 || narrow.meta != fixedTextUtf8 {
		t.Errorf("C5: the string(8) entry is (size %d, flavour %d), want (8, textUtf8 %d)", narrow.size, narrow.meta, fixedTextUtf8)
	}
	if blob.size != 8 || blob.meta != fixedTextBytes {
		t.Errorf("C5: the bytes(8) entry is (size %d, flavour %d), want (8, textBytes %d)", blob.size, blob.meta, fixedTextBytes)
	}

	// THE LOOP READS THE FLAVOUR: the generated runtime takes a wide text's
	// unit as TWO, so its capacity is counted in code units and not bytes.
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("C5: generate: %v", err)
	}
	text := t02Source(t, files)
	if !strings.Contains(text, "plan[b + TableFixedLane.meta] == TableFixedOp.textWide\n            ? 2\n            : 1;") {
		t.Error("C5: the generated text op does not take a wide text's unit as two — wide text is counted in code units")
	}
}
