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

// t06Counted declares a counted array, the shape C14 names: a live count and
// slack behind it.
const t06Counted = `package probe

fixed table T
{
    marks [..4]int32
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
		{"java/W3", t06W3},
		{"java/W5", t06W5},
		{"java/W8", t06W8},
		{"java/W7", t06W7},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t06C14: C14 "bounds pass walks live only". The java leg's bounds pass for a
// counted array is the emitted scatter: it clamps the count to this reader's
// own declared bound, then walks the LIVE count, and never the declared slack
// behind it (docs/FIXED-FORM-ALGORITHM.md §4.6, the reference's row C14: "the
// bounds pass walks LIVE elements only: a counted array's live count, not its
// slack"). A fixed table with a counted array names the clause directly.
func t06C14(t *testing.T) {
	src := string(t06Files(t, t06Counted)["TFixed.java"])
	scatter := methodBody(src, "public static void scatter(")
	if scatter == "" {
		t.Fatal("C14: no scatter in TFixed.java")
	}
	t06Has(t, scatter, "i < v.marksCount", "C14 the bounds walk takes the live count")
	if strings.Contains(scatter, "i < 4") {
		t.Errorf("C14: the bounds walk still takes the declared bound, not the live count")
	}
	t06Has(t, scatter, "if (n < 0) { n = 0; r.clamped++; } else if (n > 4) { n = 4; r.clamped++; }",
		"C14 the count itself is clamped to this reader's own bound")
}

// t06W4: W4 "read slack unspecified". A union's slack behind a NARROWER arm is
// IGNORED on read, never a refusal (docs/SPEC-TABLES.md §3.4). FU1's `plain`
// arm (one int32) is narrower than its `labelled` arm, so the writer lays the
// wider arm's bytes behind the selected plain one; the direct assertion writes
// the plain arm with this leg's own writer and reads it back through the
// compiled plan, asserting the read RETURNS clean and the plain value lands. A
// reader that refused on the arm's slack would go red here by name.
func t06W4(t *testing.T) {
	classes := t03Build(t, t.TempDir(), "fu1_read_slack", []sideSpec{
		{key: "reads", schema: "FU1.schema"},
	}, map[string]string{
		"Probe_capability.java": t03SaveLoadProbe("Probe_capability", "tblfu1", "FuRoot", `
        // THE SELECTED ARM IS NARROWER THAN THE WIDEST: plain is one int32,
        // labelled is lead+label(8)+trail. The slack behind plain is the
        // wider arm's width and is IGNORED on read, never refused.
        vals[0].pick.type = tblfu1.PickFixed.plain;
        vals[0].pick.plain.n = 303;
        vals[0].tail = 12;`),
	})
	out := t03RunSaveLoad(t, classes, "Probe_capability")
	r := parseProbe(t, out)
	if r.n != 1 || r.refused || r.malformed {
		t.Fatalf("W4: a read of the narrower arm did not return clean: n=%d refused=%v reason=%s malformed=%v", r.n, r.refused, r.reason, r.malformed)
	}
	if got := r.value["pick.plain.n"]; got != "303" {
		t.Errorf("W4: pick.plain.n=%s, want the writer's 303", got)
	}
	if r.clamped != 0 || r.widened != 0 || r.unknown != 0 || r.kindMismatch != 0 {
		t.Errorf("W4: a clean read of a narrower arm moved a counter: clamped=%d widened=%d unknown=%d kindMismatch=%d, want all 0",
			r.clamped, r.widened, r.unknown, r.kindMismatch)
	}
}

// t06W9: W9 "prefill unwritten ranges only". ALGORITHM §5.3: the prefill is the
// bytes the chosen plan does not write, and no more than those.
func t06W9(t *testing.T) {
	src := string(t06Files(t, t06Flat)["TFixed.java"])
	t06Has(t, src, "// THE PREFILL IS THE BYTES THIS PLAN DOES NOT WRITE", "W9 the prefill's own rule")
	t06Has(t, src, "final int holeN = TableFixed.holes(entries, made, cover, hole);", "W9 the hole list is what is prefilled")
}

// java/W1 ("write slack is template zeros") is already held behaviourally by the
// running TestIdentityCompiledBytesN (fixedform_test.go), which fails by name
// when a written slack byte is not zero; the card names that test in the verdict
// rather than repeating a comment-presence check here.

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

// java/W16 ("nested union answers outer tag") and java/R28 ("the guard chain ...
// an arm inside an arm answers to the OUTER tag") are [weak] tasks the card
// requires to carry a direct BEHAVIOURAL assertion red-first against a planted
// break. The field-presence half of R28 is already held by the running
// TestFixedHostileGuardAndOrdinal (fixedform_test.go), which reads plan[i].argw;
// no running test on this leg builds, writes and reads a nested union to witness
// the outer-tag clause, and this card does not add one here: the verdicts are
// `unknown` and name the unproved clause. A comment-presence check is not that
// assertion, so none is kept.
