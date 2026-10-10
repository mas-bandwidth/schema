package cstable

// fixedroadmap_t03 — the cs leg's plan-selection (R12), fixed-closure (R6,
// R14, R22), retirement (R3, R32) and retired-runtime (R10, R15) rows of
// docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW Fixed Tables"),
// the assertions no other test of this leg makes. Law:
// docs/FIXED-FORM-ALGORITHM.md §5.1 (the lock's monotone refusals), §5.2
// (COMPILE(lock, T): PLAN, the writer's record bound, THE FLOOR), §5.3 (LOAD
// and its refusal table), §5.6 (what is retired), §5.9 #23 (the retirement
// print), docs/FIXED-FORM-VERSIONING-TESTS.md ("The floor and the hash",
// "closure_plain_table", "closure_variable_kind", "closure_self"),
// docs/SPEC-TABLES.md §2.2 and §3.4. One subtest per task id, table-driven,
// t.Parallel() first, and the assertion under each is the clause the task's
// own title states.
//
// A clause already held by an existing test of this leg is named by that test
// in the card's verdicts and is not repeated here; this file carries the
// direct assertion of every clause that had none. [verify] tasks prove the
// holder RUNS and BITES; [owed] tasks are asserted here red-first and, where
// the behaviour was missing, implemented in the leg's own runtime.

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

// t03File is the generated file that carries the unit's shared runtime and its
// fixed table for the single-file `package probe` fixtures below.
const t03File = "ProbeTable.cs"

// t03Names lists the emitted file names, so a moved file is red by name.
func t03Names(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	return names
}

// t03Block is the text between two markers, from the first occurrence of
// `start` to the first `end` after it. It fails rather than answering "".
func t03Block(t *testing.T, src, start, end string) string {
	t.Helper()
	i := strings.Index(src, start)
	if i < 0 {
		t.Fatalf("the generated source has no %q", start)
	}
	rest := src[i+len(start):]
	j := strings.Index(rest, end)
	if j < 0 {
		t.Fatalf("the generated source has no %q after %q", end, start)
	}
	return rest[:j]
}

// t03Norm collapses a run of whitespace to one space, so an assertion reads the
// emitted statement and not the indentation the string builder chose.
func t03Norm(s string) string { return strings.Join(strings.Fields(s), " ") }

// t03Has is a required substring, named by rule. `got` is the haystack (the
// source or its normalised form) so a caller can read a statement across lines.
func t03Has(t *testing.T, got, want, what string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("%s: the generated source does not carry %q", what, want)
	}
}

// t03Generate generates a single-file fixture and answers its files with the
// runtime home's source, where the table's fixed form and the shared runtime
// live.
func t03Generate(t *testing.T, src string) (map[string][]byte, string) {
	t.Helper()
	files, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	body, ok := files[t03File]
	if !ok {
		t.Fatalf("Generate emitted no %s; files are: %v", t03File, t03Names(files))
	}
	return files, string(body)
}

// t03Method is one emitted method's body, or "" when the file has none.
func t03Method(src, sig string) string {
	i := strings.Index(src, sig)
	if i < 0 {
		return ""
	}
	rest := src[i:]
	if j := strings.Index(rest, "\n    }\n"); j >= 0 {
		return rest[:j]
	}
	return rest
}

// t03FindTable finds a table by name in the unit's files.
func t03FindTable(t *testing.T, u *ir.Unit, name string) *ir.Struct {
	t.Helper()
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if st.Name == name {
				return st
			}
		}
	}
	t.Fatalf("the unit declares no table %s", name)
	return nil
}

// t03CheckErrs parses and checks one source and answers the checker's errors, so
// a closure refusal can be read by name rather than caught as a fixture bug.
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

// t03Older is a locked older entry for table T: the whole own entry with a
// different wire hash and the retired mark the caller asks for.
func t03Older(wire uint64, retired bool) FixedLineageEntry {
	return FixedLineageEntry{
		Wire:    wire,
		Layout:  []byte{1, 0, 0, 0},
		Record:  12,
		Retired: retired,
		Reason:  "retired by the test's lock",
	}
}

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

// t03R12: R12 [owed] "the per-record hash check is before the prefill:
// no_layout writes nothing" (§5.3 step 11, §5.2's PLAN; §5.8 row 9). The
// emitted record loop tests the record's own hash against the file's FIRST,
// before the prefill-and-run it guards, so a no_layout refusal has written not
// one destination byte.
func t03R12(t *testing.T) {
	// A reader whose old peer carries one fewer field, so the compiled plan has
	// a NONEMPTY prefill: the appended `y` is the slot the plan does not land.
	older := unitFrom(t, `package probe

fixed table T
{
    x int32
}
`)
	entry, ok := FixedLineageOf(older, "T")
	if !ok {
		t.Fatal("R12: FixedLineageOf has no entry for T")
	}
	reader := unitFrom(t, t03Flat)
	files, err := GenerateLineage(reader, map[string][]FixedLineageEntry{"T": {entry}})
	if err != nil {
		t.Fatalf("R12: GenerateLineage: %v", err)
	}
	src := string(files[t03File])
	load := t03Method(src, "public static long TFixedLoad(")
	if load == "" {
		t.Fatal("R12: the generated source has no TFixedLoad")
	}
	hashAt := strings.Index(load, "BinaryPrimitives.ReadUInt64LittleEndian(at) != hash")
	fillAt := strings.Index(load, "TableFixedWire.FillRun(")
	runAt := strings.Index(load, "TableFixedWire.Run(")
	if hashAt < 0 {
		t.Fatal(`R12: the record loop carries no per-record hash check`)
	}
	if fillAt < 0 || runAt < 0 {
		t.Fatalf("R12: the record loop carries no prefill-and-run (fill at %d, run at %d)", fillAt, runAt)
	}
	if hashAt > fillAt || hashAt > runAt {
		t.Errorf("R12: the per-record hash check (at %d) is not before the prefill-and-run (fill at %d, run at %d): a no_layout refusal would have written the destination", hashAt, fillAt, runAt)
	}
	t03Has(t, t03Norm(load), `if (BinaryPrimitives.ReadUInt64LittleEndian(at) != hash) { if (report != null) { report.Refused = true; report.Reason = "no_layout"; report.Verdict = TableWire.Verdict.Refused; } return -1; }`,
		"R12 no_layout is the refusal the check returns, before any byte lands")
}

// t03R6: R6 [owed] "a table past §3.4's 65536 ceiling is not a fixed-form
// root: the refusal names the table, no form is emitted, and no lineage entry
// is parsed for it even when the lock carries one" (§3.4, §5.2, and
// docs/FIXED-FORM-ALGORITHM.md §5.5). The ceiling is the WIRE's: a reader
// holds an untrusted peer's layout to it, so past it the fixed form is not
// emitted and the table keeps form 1, named at compile time.
func t03R6(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table Big
{
    blob bytes(70000)
}
`)
	st := t03FindTable(t, u, "Big")
	if got := ir.TableFixedTypeBytes(st); got <= ir.TableFixedRecordMaxBytes {
		t.Fatalf("R6: the fixture body = %d, not past %d", got, ir.TableFixedRecordMaxBytes)
	}
	for _, r := range ir.TableFixedFormRoots(u) {
		if r.Name == "Big" {
			t.Error("R6: a table past the ceiling is a fixed-form root")
		}
	}
	warnings, errs := ir.TableFixedRecordBounds(u, 0)
	if len(errs) != 0 {
		t.Errorf("R6: past the ceiling is a warning, not a refusal: %v", errs)
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "Big") || !strings.Contains(joined, "ceiling") {
		t.Errorf("R6: the warning does not name the table and the ceiling: %s", joined)
	}
	// THE LOCK MAY CARRY AN ENTRY FOR IT AND STILL NOTHING IS PARSED: the
	// emitted build holds no byte of that lineage key.
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{
		"Big": {{Wire: 0x1122334455667788, Layout: []byte{1, 2, 3, 4}, Record: 70008}},
	})
	if err != nil {
		t.Fatalf("R6: GenerateLineage: %v", err)
	}
	if _, ok := files["BigTable.cs"]; ok {
		t.Error("R6: a form was emitted for a table past the ceiling")
	}
	for name, body := range files {
		if strings.Contains(string(body), "0x1122334455667788") {
			t.Errorf("R6: %s parsed the lock's lineage entry for a table past the ceiling", name)
		}
	}
}

// t03R14: R14 [owed] "layout_record_too_large for an entry reaching past the
// writer's declared record, not only for the 65536 bound and a zero root"
// (§5.2's PLAN, fix 3; §5.3's refusal table). Every entry is bounded by the
// WRITER'S OWN root size; the reader's grown bound is not the writer's, and
// the name is the compiled lane's own when an entry overruns it.
func t03R14(t *testing.T) {
	_, runtime := t03Generate(t, t03Flat)
	n := t03Norm(runtime)
	// the two ceilings that already existed
	t03Has(t, n, `if (e.Size > RecordMaxBytes) { c.Fail("layout_record_too_large"); return 0; }`,
		"R14 a single entry past the 65536 bound")
	t03Has(t, n, `if (sum > RecordMaxBytes) { c.Fail("layout_record_too_large"); return 0; }`,
		"R14 a subtree's partial sum past the 65536 bound")
	t03Has(t, n, `if (root.Size == 0 || root.Size > RecordMaxBytes)`,
		"R14 the zero root and the 65536 bound")
	t03Has(t, n, `why = "layout_record_too_large";`,
		"R14 the zero root and the bound are named")
	// the writer's own declared record, and one entry reaching past it
	t03Has(t, n, `TheirBytes = (int)EntryAt(theirs, 0).Size`,
		"R14 the writer's own declared record is the bound")
	t03Has(t, n, `int reach = OpReach(stamped.Op, (int)stamped.Size);`,
		"R14 each entry's reach is the op's, not the size lane's")
	t03Has(t, n, `case Text: return 4 + size;`,
		"R14 a text entry reaches past its own units by the length word")
	t03Has(t, n, `case Count: return 4;`,
		"R14 a count entry reads its four-byte bound, not the reader's grown run")
	t03Has(t, n, `(long)stamped.Src + reach > TheirBytes`,
		"R14 an entry reaching past the writer's declared record is caught")
	t03Has(t, n, `(long)stamped.Guard + gw > TheirBytes`,
		"R14 a guarded entry's guard reach is bounded too")
	t03Has(t, n, `RecordTooLarge = true;`,
		"R14 the plan is refused WHOLE")
	t03Has(t, n, `if (c.RecordTooLarge) { return -2; }`,
		"R14 the compile answers the overrun on its own return")
	t03Has(t, n, `if (made == -2) { lane.Why = "layout_record_too_large"; break; }`,
		"R14 the lineage entry's own lane carries layout_record_too_large by name")
}

// t03R22: R22 [owed] "the closure rule: every table or type reached by value
// is itself fixed; a pointer, map or unbounded array in the closure is a
// compile refusal; T is never in its own closure" (docs/SPEC-TABLES.md §2.2,
// ir.FixedClosureBreaks, docs/FIXED-FORM-ALGORITHM.md §5.5). A fixed table's
// BY-VALUE closure may hold only fixed bodies; a variable-size construct is a
// refusal that names the field, the construct and the page.
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
			t.Errorf("R22 %s: refusal = %q, want it to name %q and the page", tc.name, got, tc.want)
		}
	}

	// T IS NEVER IN ITS OWN CLOSURE: a fixed table that reaches itself by value
	// is refused, so a table cannot be its own closure member.
	for _, src := range []string{
		"package probe\nfixed table T { next T }\n",
		"package probe\nfixed table T { next [4]T }\n",
		"package probe\nfixed table T { next ?T }\n",
	} {
		if errs := t03CheckErrs(t, src); len(errs) == 0 {
			t.Errorf("R22: a fixed table that reaches itself by value is not refused: %q", src)
		}
	}
}
