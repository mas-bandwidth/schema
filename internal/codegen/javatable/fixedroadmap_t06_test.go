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

// t06Bytes declares a bytes(N) field, the shape W1 and W8 name.
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
	if !javaCarries(src, want) {
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

// t06W9: W9 "prefill unwritten ranges only". ALGORITHM §4.3: "Prefill the bytes
// the plan does not write. The plan compiler computes the UNWRITTEN RANGES —
// the destination bytes no entry covers — and only those take the declared
// defaults before the loop". The emitted load prefills exactly the hole list
// TableFixed.holes computes, copying each hole's OWN range, and never a
// whole-record copy that would overwrite a range the plan lands.
func t06W9(t *testing.T) {
	load := string(t06Files(t, t06Flat)["TFixed.java"])
	t06Has(t, load, "final int holeN = TableFixed.holes(entries, made, cover, hole);",
		"W9 the prefill covers the plan's hole list")
	t06Has(t, load, "for (int h = 0; h < holeN; h++) {",
		"W9 the prefill runs over the hole list")
	t06Has(t, load, "System.arraycopy(defaults, off, image, off, hn);",
		"W9 each hole's own range takes the defaults, so a range the plan lands is untouched")
	if strings.Contains(load, "System.arraycopy(defaults, 0, image, 0,") {
		t.Error("W9: the load still prefills the whole record, overwriting the ranges the plan writes")
	}
}

// t06W1: W1 "write slack is template zeros". ALGORITHM §3.1: "The template is a
// compile-time constant — the hash, then zero everywhere a value lands — which
// zero-fills every byte of declared slack without the writer touching it." This
// leg's writer lays the zeros by storing the live extent and then zeroing the
// slack behind it, so a stale byte in the buffer never reaches the wire. The
// behavioural twin is TestIdentityCompiledBytesN.
func t06W1(t *testing.T) {
	src := string(t06Files(t, t06Bytes)["TFixed.java"])
	t06Has(t, src, "System.arraycopy(v.blob, 0, b, at + 4, v.blobLength);",
		"W1 the writer copies only the live bytes extent")
	t06Has(t, src, "java.util.Arrays.fill(b, at + 4 + v.blobLength, at + 10, (byte) 0);",
		"W1 the slack behind the live extent is the template's zeros")
}

// t06W3: W3 "zero behind a narrower arm". ALGORITHM §3.1: "A union writes the
// tag and the taken arm only, zero behind a narrower one." In FU1 the `plain`
// arm is one int32 (four bytes) and the union body is the one-byte tag then the
// wider `labelled` arm (twenty bytes): the emitted writer fills from the end of
// the plain arm to the end of the body, so every byte the narrower arm does not
// store is zero, and the `default` (None) arm zeroes the whole body.
func t06W3(t *testing.T) {
	files, err := Generate(schemaUnit(t, "FU1.schema"))
	if err != nil {
		t.Fatalf("W3: Generate: %v", err)
	}
	src := string(files["PickFixed.java"])
	t06Has(t, src, "TableFixed.put8(b, at + 0, v.type);",
		"W3 the tag is stored first")
	t06Has(t, src, "java.util.Arrays.fill(b, at + 5, at + 21, (byte) 0);",
		"W3 the slack behind the narrower plain arm (tag + four bytes) is zeroed")
	t06Has(t, src, "java.util.Arrays.fill(b, at + 1, at + 21, (byte) 0);",
		"W3 None zeroes the whole body")
}

// t06W5: W5 "write checks DEBUG only". ALGORITHM §3.1: "Write-side bound checks
// are DEBUG ONLY, by rule. `count <= Max` and `length <= N` are a caller
// contract; a release build removes them exactly as it removes `assert`, pays
// nothing, and clamps nothing." The contract walk is reached through exactly
// one `assert` and through nothing else, and the write path carries no counter.
func t06W5(t *testing.T) {
	src := string(t06Files(t, t06Flat)["TFixed.java"])
	t06Has(t, src, "assert checkWrite(v);",
		"W5 the checks ride the assert, which a release build removes")
	if n := strings.Count(src, "checkWrite(v)"); n != 1 {
		t.Errorf("W5: checkWrite is invoked %d times, want exactly the one `assert` call a release build removes", n)
	}
	for _, fn := range []string{"public static void writeBody(", "public static int save("} {
		body := methodBody(src, fn)
		if body == "" {
			t.Fatalf("W5: no %s in TFixed.java", fn)
		}
		if strings.Contains(body, "report.") || strings.Contains(body, ".clamped") {
			t.Errorf("W5: %s carries a write-side check or counter; write checks are DEBUG only and a release build clamps nothing", fn)
		}
	}
}

// t06W8: W8 "bytes(N) takes the array row". SPEC-TABLES §3.4, the dest rows: a
// bytes(N) destination row is the COUNTED ARRAY's — dest the element base
// behind the u32 count, stride the element width, aux where the count lands,
// counted, flavour bytes — and not the TEXT row (dest the length, aux the
// buffer). The emitted row for the single bytes(6) field is dest=4, stride=1,
// aux=0, counted=1, arg=bytes(3).
func t06W8(t *testing.T) {
	src := string(t06Files(t, t06Bytes)["TFixed.java"])
	t06Has(t, src, "4, 1, 0, 1, 3, // 1 blob",
		"W8 the bytes(N) dst row: element base 4, stride 1, count aux 0, counted, bytes flavour")
}

// t06W7: W7 "arg and meta are two lanes". ALGORITHM §4.1: "`arg` is THE GUARD'S
// ORDINAL and nothing else; `meta` is THE OP'S OWN ARGUMENT (a text flavour)."
// The emitted guard compares the tag against arg, and the emitted text push
// lands the flavour in meta while arg still carries the arm's ordinal, so a
// text field under a union arm is guarded and flavoured at once.
func t06W7(t *testing.T) {
	runtime := string(t06Files(t, t06Flat)["TableFixed.java"])
	t06Has(t, runtime, "public long arg;", "W7 arg is the arm ordinal lane")
	t06Has(t, runtime, "public byte meta;", "W7 meta is the op's own lane")
	t06Has(t, runtime, "if (tagAt(src, srcAt + p.guard, gw) != p.arg) { continue; }",
		"W7 the guard compares its ordinal in arg, never in meta")
	t06Has(t, runtime, "final byte meta = flavour != 0 ? flavour : textUtf8;",
		"W7 the text op computes its flavour into meta")
	t06Has(t, runtime, "c.push(theirAt, at, units, auxAt, guard, opText, arg, (byte) 0, (byte) 0, meta);",
		"W7 the text push writes the flavour to meta and leaves arg the arm's ordinal")
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
