package cstable

// fixedroadmap_t03 — the cs leg's plan-selection, fixed-closure, retirement and
// retired-runtime rows of docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md
// "NEW Fixed Tables"): the assertions no other test of this leg makes.
//
// Law: docs/FIXED-FORM-ALGORITHM.md §5.2 (PLAN's record bound), §5.3 (LOAD),
// §5.5 (the closure), §5.6 (what is retired); the §5.9 #23 retirement print;
// docs/FIXED-FORM-VERSIONING-TESTS.md ("The floor and the hash", and "The old
// contract's tests, retired by name"); docs/SPEC-TABLES.md §2.2, §3.4.
//
// Every assertion here reads the C# this leg emits or the ir walk it renders,
// so the harness is reachable on a tree that never built the C++ reference
// corpus and never carries a dotnet SDK: the compile-and-run half of this leg's
// fixed form is fixedversioning_test.go and fixedroadmap_t01_test.go, which
// skip exactly there. A clause already held by an existing test of this leg is
// named by that test in the card's verdicts; this file carries the direct
// assertion of every clause that had none.
//
// [owed] tasks are asserted here red-first and implemented in the emitter or
// runtime; [verify] tasks prove the holder RUNS and BITES; [weak] tasks write
// the direct assertion their golden left open.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// t03Flat is the smallest declared fixed table: two adjacent int32 scalars.
const t03Flat = `package probe

fixed table T
{
    x int32
    y int32
}
`

func TestFixedRoadmapCSharpT03PlansVersions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"cs/R12", t03R12},
		{"cs/R6", t03R6},
		{"cs/R14", t03R14},
		{"cs/R22", t03R22},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// ---- readers of the emitted source ------------------------------------------

// t03Files generates one fixture and answers its files.
func t03Files(t *testing.T, src string) map[string][]byte {
	t.Helper()
	files, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatalf("t03: Generate: %v", err)
	}
	return files
}

// t03Home answers the generated file that carries the shared fixed runtime;
// for a single-file fixture it is also the file that carries the table's form.
func t03Home(t *testing.T, files map[string][]byte) string {
	t.Helper()
	for _, b := range files {
		if strings.Contains(string(b), "public static class TableFixedWire") {
			return string(b)
		}
	}
	t.Fatalf("t03: no emitted file carries TableFixedWire; files are %v", t02Names(files))
	return ""
}

// t03Method is one emitted method's body, from its signature to the next
// top-level member, or "" when the class has none.
func t03Method(src, sig string) string {
	i := strings.Index(src, sig)
	if i < 0 {
		return ""
	}
	rest := src[i:]
	if j := strings.Index(rest, "\n        public static "); j > 0 {
		return rest[:j]
	}
	return rest
}

// t03CheckErrs parses and checks one source and answers the checker's errors,
// so a closure refusal is read by name rather than caught as a fixture bug.
func t03CheckErrs(t *testing.T, src string) []error {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		return []error{perrs[0]}
	}
	_, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	return cerrs
}

// t03Older is `n` synthetic locked entries, oldest first, the first `retire`
// marked retired, which is what moves the floor.
func t03Older(n, retire int) []FixedLineageEntry {
	out := make([]FixedLineageEntry, n)
	for i := range out {
		out[i] = FixedLineageEntry{
			Wire:   0x1111000000000000 + uint64(i),
			Layout: []byte{byte(i), 0x00, 0x00, 0x00},
			Record: 12,
		}
		if i < retire {
			out[i].Retired = true
			out[i].Reason = "retired by the test's lock"
		}
	}
	return out
}

// ---- cs/R12 -----------------------------------------------------------------

// t03R12: R12 [owed] "the per-record hash check is before the prefill: no_layout
// writes nothing" (§5.3 step 11, §5.8 row 9). The generated record loop tests the
// record's own hash word BEFORE it fills the plan's holes or runs a single plan
// entry: a mismatch is `no_layout`, returns at once, and has written no
// destination byte. The run half is TestFixedVersioningRefuseWritesNothing
// (cs/F10), which forges record 0's hash over a plan carrying a nonempty prefill
// and compares every poisoned field back.
func t03R12(t *testing.T) {
	load := t03Method(t03Home(t, t03Files(t, t03Flat)), "public static long TFixedLoad(\n            Span<T> values,")
	if load == "" {
		t.Fatal("R12: the emitted unit has no TFixedLoad(Span<T>, ...)")
	}
	check := strings.Index(load, "BinaryPrimitives.ReadUInt64LittleEndian(at) != hash")
	fill := strings.Index(load, "TableFixedWire.FillRun(")
	if check < 0 {
		t.Error("R12: the per-record hash check is not emitted in the record loop")
	}
	if fill < 0 {
		t.Error("R12: the prefill is not emitted in the record loop")
	}
	if check >= 0 && fill >= 0 && check > fill {
		t.Error("R12: the prefill runs before the per-record hash check, so a refused read writes the destination")
	}
	t02Has(t, t02Norm(load), `report.Reason = "no_layout"; report.Verdict = TableWire.Verdict.Refused;`,
		"R12 a failed per-record hash refuses no_layout by name")
}

// ---- cs/R6 ------------------------------------------------------------------

// t03R6: R6 [owed] "a table past §3.4's 65536 ceiling is not a fixed-form root:
// the refusal names the table, no form is emitted, and no lineage entry is
// parsed for it even when the lock carries one" (§5.9 #48). The ceiling is the
// FORM's and not the class's: the table keeps form 1 and is named, no fixed
// surface or known layout is written for it, and a lock row handed in for it is
// never parsed.
func t03R6(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table Small
{
    keep uint32 = 0
}

fixed table Big
{
    payload bytes(70000)
}
`)
	big := u.Tables["Big"]
	if big == nil {
		t.Fatal("R6: the unit declares no Big")
	}
	if n := ir.TableFixedTypeBytes(big); n <= ir.TableFixedRecordMaxBytes {
		t.Fatalf("R6: Big's body = %d, not past the %d ceiling", n, ir.TableFixedRecordMaxBytes)
	}

	// (1) NOT A FIXED-FORM ROOT.
	var roots []string
	for _, st := range ir.TableFixedFormRoots(u) {
		roots = append(roots, st.Name)
	}
	if len(roots) != 1 || roots[0] != "Small" {
		t.Errorf("R6: ir's fixed-form roots = %v, want [Small]", roots)
	}

	// (2) THE REFUSAL NAMES THE TABLE AND THE CEILING.
	warnings, errs := ir.TableFixedRecordBounds(u, 0)
	if len(errs) != 0 {
		t.Errorf("R6: past the ceiling is a warning and not a refusal: %v", errs)
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "Big") || !strings.Contains(joined, "ceiling") {
		t.Errorf("R6: the refusal does not name the table and the ceiling: %s", joined)
	}

	// (3) NO FORM IS EMITTED AND NO LINEAGE ENTRY IS PARSED, even when the lock
	// carries one: handing it in must not fail the build.
	own, ok := FixedLineageOf(u, "Small")
	if !ok {
		t.Fatal("R6: no lineage entry for Small")
	}
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{
		"Small": {own},
		"Big":   {{Wire: 0x1122334455667788, Layout: []byte{1, 2, 3, 4}, Record: 70008}},
	})
	if err != nil {
		t.Fatalf("R6: a lineage entry for a table past the ceiling must not fail the build: %v", err)
	}
	text := ""
	for _, b := range files {
		text += string(b)
	}
	for _, bad := range []string{"BigFixed", "1122334455667788"} {
		if strings.Contains(text, bad) {
			t.Errorf("R6: %q was emitted for a table past the ceiling", bad)
		}
	}
	if !strings.Contains(text, "SmallFixedKnown") {
		t.Error("R6: the ceiling refuses Big alone; Small keeps its form")
	}
}

// ---- cs/R14 -----------------------------------------------------------------

// t03R14: R14 [owed] "layout_record_too_large for an entry reaching past the
// writer's declared record, not only for the 65536 bound and a zero root"
// (§5.2 PLAN: "record := x.root.size — EVERY entry is bounded by this, and by
// nothing the file said ... if an entry reached past `record`: REFUSE
// layout_record_too_large"; §5.9 #48). The peer's root size is the bound every
// compiled entry's source extent is held to, and one entry past it refuses the
// plan WHOLE by name rather than landing a short read.
func t03R14(t *testing.T) {
	home := t03Home(t, t03Files(t, t03Flat))
	for _, want := range []string{
		"public const int CompileRecordTooLarge = -2;",
		"private static ulong OpReach(byte op, uint size)",
		"public bool TooLarge;",
		"public uint Record;",
		"if (Record != 0 && reach > Record) { TooLarge = true; return; }",
		"if (c.TooLarge) { return CompileRecordTooLarge; }",
		`if (made == CompileRecordTooLarge) { lane.Why = "layout_record_too_large"; break; }`,
	} {
		t02Has(t, home, want, "R14 the runtime bounds every entry to the writer's declared record")
	}
	// THE BOUND IS THE WRITER'S OWN DECLARED ROOT SIZE, read from the peer
	// layout, and each op reaches its own number of bytes: a Count reads the
	// four-byte count word, a Text its length word plus its units, a WidenF the
	// four bytes it reads, every other op its byte run.
	t02Has(t, home, "Record = EntryAt(theirs, 0).Size,", "R14 the writer's declared record is the bound")
	for _, want := range []string{
		"case TableFixedWire.Count: return 4;",
		"case TableFixedWire.Text: return 4 + (ulong)size;",
		"case TableFixedWire.WidenF: return 4;",
		"default: return size;",
	} {
		t02Has(t, home, want, "R14 one op kind's reach into the writer's record")
	}
	// The 65536 checks stay where they are: the record bound is ADDED, not a
	// replacement for the wire's own ceiling or the zero root.
	t02Has(t, home, "if (e.Size > RecordMaxBytes) { c.Fail(\"layout_record_too_large\"); return 0; }",
		"R14 the 65536 bound survives beside the writer's record bound")
}

// ---- cs/R22 -----------------------------------------------------------------

// t03R22: R22 [owed] "the closure rule: every table or type reached by value is
// itself fixed; a pointer, map or unbounded array in the closure is a compile
// refusal; T is never in its own closure" (§5.5, §2.2). A nested fixed table is
// a closure member and not a break; a plain table held by value, a pointer, a
// map and an unbounded array are each refused naming the construct, and a table
// that reaches itself by value is a composition cycle.
func t03R22(t *testing.T) {
	fixed := unitFrom(t, `package probe

fixed table Inner { a int32 }
fixed table T { inner Inner }
`)
	if breaks := ir.FixedClosureBreaks(func(name string) *ir.Struct { return fixed.Tables[name] }, "T"); len(breaks) != 0 {
		t.Errorf("R22: a fixed table nested by value is a closure break: %v", breaks)
	}

	for _, tc := range []struct {
		name, src, want string
	}{
		{"plain table", "package probe\ntable Node { x int32 }\nfixed table T { node Node }\n", "holds the plain table Node by value"},
		{"pointer", "package probe\ntable Node { x int32 }\nfixed table Scene { head *Node }\n", "is a pointer"},
		{"map", "package probe\nfixed table Fleet { ships map[uint32]int32 }\n", "is a map"},
		{"unbounded array", "package probe\nfixed table Log { entries []int32 }\n", "is an unbounded array"},
	} {
		errs := t03CheckErrs(t, tc.src)
		if len(errs) == 0 {
			t.Errorf("R22 %s: the closure break is not a compile refusal", tc.name)
			continue
		}
		got := errs[0].Error()
		if !strings.Contains(got, tc.want) || !strings.Contains(got, "docs/SPEC-TABLES.md") {
			t.Errorf("R22 %s: refusal = %q, want it to name %q and the spec section", tc.name, got, tc.want)
		}
	}

	// T is never in its own closure: a table that reaches itself by value is a
	// composition cycle, so no table is its own closure member.
	if errs := t03CheckErrs(t, "package probe\nfixed table T { next T }\n"); len(errs) == 0 {
		t.Error("R22: a fixed table that reaches itself by value is not refused")
	}
}
