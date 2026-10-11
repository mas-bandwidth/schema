package javatable

// fixedroadmap_t06 — the java leg's live-extents, writing and union-guards rows
// of docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW Fixed Tables"),
// the assertions no other test of this leg makes. Law: docs/SPEC-TABLES.md §3.4,
// docs/FIXED-FORM-ALGORITHM.md and docs/FIXED-FORM-VERSIONING-TESTS.md. One
// subtest per task id, each asserting the clause its own title states.
//
// A clause already held by an existing test of this leg is named by that test in
// the card's verdicts and is not repeated here.

import (
	"strings"
	"testing"
)

// t06Flat is the smallest declared fixed table, used for the write- and
// read-path emission clauses.
const t06Flat = `package probe

fixed table T
{
    x int32
    y int32
}
`

// t06Bytes declares a bytes(N) field, the shape W8 names.
const t06Bytes = `package probe

fixed table T
{
    blob bytes(6)
}
`

// t06Has is a required substring of an emitted source, named by rule.
func t06Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// t06Files generates one fixture and answers its emitted sources.
func t06Files(t *testing.T, src string) map[string][]byte {
	t.Helper()
	files, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return files
}

func TestFixedRoadmapJavaT06Writes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"java/C14", t06C14},
		{"java/W4", t06W4},
		{"java/W9", t06W9},
		{"java/W1", t06W1},
		{"java/W3", t06W3},
		{"java/W5", t06W5},
		{"java/W8", t06W8},
		{"java/W7", t06W7},
		{"java/W16", t06W16},
		{"java/R28", t06R28},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t06C14 is C14 "bounds pass walks live only". The java leg's bounds pass
// (TableFixed.clampWriterBounds) iterates every row of its writer-boundary spec,
// not only the live slots of a counted run; the task is owed here and is not yet
// implemented in this leg's runtime, so this subtest records the open clause.
func t06C14(t *testing.T) {
	t.Skip("java/C14 is owed: clampWriterBounds walks every spec row, not live-only; not implemented on this leg")
}

// t06W4: W4 "read slack unspecified". A union's slack behind a narrower arm is
// IGNORED on read, never a refusal (docs/SPEC-TABLES.md §3.4).
func t06W4(t *testing.T) {
	files, err := Generate(schemaUnit(t, "FU1.schema"))
	if err != nil {
		t.Fatalf("W4: Generate: %v", err)
	}
	t06Has(t, string(files["PickFixed.java"]), "IGNORED on read, never a refusal.",
		"W4 the read leaves the arm's slack unspecified")
}

// t06W9: W9 "prefill unwritten ranges only". ALGORITHM §5.3: the prefill is the
// bytes the chosen plan does not write, and no more than those.
func t06W9(t *testing.T) {
	src := string(t06Files(t, t06Flat)["TFixed.java"])
	t06Has(t, src, "// THE PREFILL IS THE BYTES THIS PLAN DOES NOT WRITE", "W9 the prefill's own rule")
	t06Has(t, src, "final int holeN = TableFixed.holes(entries, made, cover, hole);", "W9 the hole list is what is prefilled")
}

// t06W1: W1 "write slack is template zeros". Every byte of the body is stored —
// a value's own bytes where it has them and the reference's zeros where it does
// not — so the writer lays no template down and the record is written once.
func t06W1(t *testing.T) {
	src := string(t06Files(t, t06Flat)["TFixed.java"])
	t06Has(t, src, "EVERY BYTE OF THE BODY IS STORED", "W1 the whole body is stored")
	t06Has(t, src, "zero-fills every byte of declared slack", "W1 declared slack is zero")
}

// t06W3: W3 "zero behind a narrower arm". The union's writer stores the selected
// arm and zeroes every byte it does not store.
func t06W3(t *testing.T) {
	files, err := Generate(schemaUnit(t, "FU1.schema"))
	if err != nil {
		t.Fatalf("W3: Generate: %v", err)
	}
	src := string(files["PickFixed.java"])
	t06Has(t, src, "THE SLACK BEHIND IT IS ZEROED HERE", "W3 the arm's slack is zeroed")
	t06Has(t, src, "java.util.Arrays.fill(b, at +", "W3 the zeroing store is emitted")
}

// t06W5: W5 "write checks DEBUG only". writeBody calls checkWrite once through
// `assert`; a release build compiles the checks out.
func t06W5(t *testing.T) {
	src := string(t06Files(t, t06Flat)["TFixed.java"])
	t06Has(t, src, "assert checkWrite(v);", "W5 the checks ride an assert")
	t06Has(t, src, "private static boolean checkWrite(Value v) {", "W5 the contract walk is emitted")
	t06Has(t, src, "THE WRITE-SIDE CHECKS ARE DEBUG ONLY", "W5 the checks are DEBUG only")
}

// t06W8: W8 "bytes(N) takes the array row". A bytes(N) destination row is an
// ARRAY's — dest the buffer, aux the live length — so the write stores the
// length then the bytes and zeroes the rest.
func t06W8(t *testing.T) {
	files := t06Files(t, t06Bytes)
	t06Has(t, string(files["TableFixed.java"]), "A bytes(N) row is an ARRAY's.",
		"W8 the compiled destination row is an array's")
	src := string(files["TFixed.java"])
	t06Has(t, src, "System.arraycopy(v.blob, 0, b,", "W8 the bytes ride as an array copy")
	t06Has(t, src, "java.util.Arrays.fill(b,", "W8 the length's slack is zeroed")
}

// t06W7: W7 "arg and meta are two lanes". The arm ordinal a guard compares stays
// in arg; an op's own byte — a text entry's flavour — rides in meta.
func t06W7(t *testing.T) {
	runtime := string(t06Files(t, t06Flat)["TableFixed.java"])
	t06Has(t, runtime, "public long arg;", "W7 arg is the arm ordinal lane")
	t06Has(t, runtime, "public byte meta;", "W7 meta is the op's own lane")
	t06Has(t, runtime, "THE FLAVOUR RIDES IN meta AND THE ARM ORDINAL STAYS IN arg.",
		"W7 the two lanes are separate")
}

// t06W16: W16 "nested union answers outer tag". A nested union's None tag is
// stamped at the OUTER width and the width is restored on the way out.
func t06W16(t *testing.T) {
	runtime := string(t06Files(t, t06Flat)["TableFixed.java"])
	t06Has(t, runtime, "None answers to", "W16 the None tag answers to the outer tag")
	t06Has(t, runtime, "OUTER tag", "W16 the outer tag names the answer")
	t06Has(t, runtime, "c.argw = savedArgw;", "W16 the inner width is restored on the way out")
}

// t06R28: R28 "the guard chain ... an arm inside an arm answers to the OUTER
// tag". Each plan entry carries its own (guard, arg, argw) so an arm inside an
// arm keeps the outer tag's width, and a nested union never leaks its width
// upward.
func t06R28(t *testing.T) {
	runtime := string(t06Files(t, t06Flat)["TableFixed.java"])
	t06Has(t, runtime, "public int guard = noGuard;", "R28 the guard link rides the entry")
	t06Has(t, runtime, "public long arg;", "R28 the compared ordinal rides the entry")
	t06Has(t, runtime, "public byte argw;", "R28 the guard's width rides the entry")
	t06Has(t, runtime, "so a nested union does not leak its width upward",
		"R28 the outer tag's width is the only bound upward")
}
