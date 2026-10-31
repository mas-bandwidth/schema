package gotable

// THE ROADMAP'S FIXED-CLOSURE, RETIREMENT, RETIRED-RUNTIME AND
// NUMERIC-EVOLUTION TASKS ON THE GO LEG (docs/roadmap.sexp node `fixed-tables`,
// ROADMAP.md "NEW Fixed Tables"). Law: docs/FIXED-FORM-ALGORITHM.md §1, §4.6,
// §5.2, §5.3, §5.5, §5.6, docs/FIXED-FORM-VERSIONING-TESTS.md ("The floor and
// the hash", "The old contract's tests, retired by name"), docs/SPEC-TABLES.md
// §3.4 and §21.2. One subtest per task id, table-driven, t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE GO THIS LEG EMITS and the shared fixed runtime
// it emits it with, so the harness runs under a bare
// `go test ./internal/codegen/gotable/` with no corpus, no sibling runtime and
// no foreign toolchain: a clause that needs the emitted code to RUN is read at
// the emitted site that runs it, the shape fixedroadmap_t02_test.go states for
// the same reason (a slowtest-gated harness skips every subtest on a default
// run, and a verdict over an assertion that never executed is the failure this
// package already paid for).
//
// A clause already held by an existing test of this leg is named by that test in
// the card's verdicts; the subtests below carry the direct assertion of the
// clause in the emitted emitter or runtime.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// t03GoRetireReason is the operator mark the retirement subtests hand in.
const t03GoRetireReason = "the operator retired this layout"

// t03GoTable resolves one declared table of a checked unit by name.
func t03GoTable(t *testing.T, u *ir.Unit, name string) *ir.Struct {
	t.Helper()
	st := u.Tables[name]
	if st == nil {
		t.Fatalf("the unit declares no table %s", name)
	}
	return st
}

// t03GoLoad is the emitted body of one `<T>FixedLoadTolerant(` up to its column-zero
// closing brace.
func t03GoLoad(t *testing.T, src, name string) string {
	t.Helper()
	body := funcSource(src, "func "+name+"FixedLoadTolerant(")
	if body == "" {
		t.Fatalf("the emitted module has no %sFixedLoad", name)
	}
	return body
}

// t03GoBetween is the text between two markers, so an assertion can be scoped
// to ONE arm of the runtime instead of matching anywhere in it.
func t03GoBetween(src, start, end string) string {
	_, after, ok := strings.Cut(src, start)
	if !ok {
		return ""
	}
	rest := after
	if before, _, ok := strings.Cut(rest, end); ok {
		return before
	}
	return rest
}

// t03GoHas is a required substring of the emitted source, named by rule.
func t03GoHas(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// t03GoHasNot is a banned substring, named by rule.
func t03GoHasNot(t *testing.T, src, bad, what string) {
	t.Helper()
	if strings.Contains(src, bad) {
		t.Errorf("%s: the emitted source carries %q", what, bad)
	}
}

// t03GoCheckErrs parses and checks one source and answers the checker's errors,
// so a closure refusal is read by name rather than caught as a fixture bug.
func t03GoCheckErrs(t *testing.T, src string) []error {
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

// t03GoRetire builds THREE generations of one table — x, x+y, x+y+z — hands the
// first `retired` of the first two in as the lock's lineage, and answers the
// emitted module together with the entries it was handed.
func t03GoRetire(t *testing.T, retired int) (string, []FixedLineageEntry) {
	t.Helper()
	older := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    x int32\n}\n")
	middle := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    x int32\n    y int32\n}\n")
	own := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    x int32\n    y int32\n    z int32\n}\n")
	e1, ok := FixedLineageOf(older, "Lineage")
	if !ok {
		t.Fatal("no lineage entry for the first generation")
	}
	e2, ok := FixedLineageOf(middle, "Lineage")
	if !ok {
		t.Fatal("no lineage entry for the second generation")
	}
	if retired > 0 {
		e1.Retired = true
		e1.Reason = t03GoRetireReason
	}
	if retired > 1 {
		e2.Retired = true
		e2.Reason = t03GoRetireReason
	}
	files, err := GenerateLineage(own, map[string][]FixedLineageEntry{"Lineage": {e1, e2}})
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	return tableFile(files), []FixedLineageEntry{e1, e2}
}

// ---- go/R6 ------------------------------------------------------------------

// t03GoR6 holds R6 to SPEC-TABLES §3.4 and ALGORITHM §5.2: a table whose record
// body is past the 65536-byte ceiling has NO FORM EMITTED, is NAMED by the
// compiler (a warning, not a refusal of the unit — it keeps form 1), and no
// lineage entry handed in for it is parsed even when the lock carries one.
func t03GoR6(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table Small
{
    keep uint32
}

fixed table Big
{
    blob bytes(70000)
}
`)
	big := t03GoTable(t, u, "Big")
	if got := ir.TableFixedTypeBytes(big); got <= ir.TableFixedRecordMaxBytes {
		t.Fatalf("R6: the fixture body = %d, not past the %d ceiling", got, ir.TableFixedRecordMaxBytes)
	}
	if ir.TableFixedEmitted(u, big) {
		t.Error("R6: a table past the ceiling is a fixed-form root")
	}
	for _, r := range ir.TableFixedFormRoots(u) {
		if r.Name == "Big" {
			t.Error("R6: TableFixedFormRoots keeps a table past the ceiling")
		}
	}
	warnings, errs := ir.TableFixedRecordBounds(u, 0)
	if len(errs) != 0 {
		t.Errorf("R6: past the ceiling is a warning, not a refusal: %v", errs)
	}
	if joined := strings.Join(warnings, "\n"); !strings.Contains(joined, "Big") || !strings.Contains(joined, "ceiling") {
		t.Errorf("R6: the refusal does not name the table and the ceiling: %s", joined)
	}
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{
		"Big": {{Wire: 0x1122334455667788, Layout: []byte{1, 2, 3, 4}, Record: 70008}},
	})
	if err != nil {
		t.Fatalf("R6: GenerateLineage: %v", err)
	}
	emitted := false
	for name, body := range files {
		text := string(body)
		if strings.Contains(text, "SmallFixedHash") {
			emitted = true
		}
		if strings.Contains(text, "BigFixed") {
			t.Errorf("R6: %s emits a form for a table past the ceiling", name)
		}
		if strings.Contains(text, "0x1122334455667788") {
			t.Errorf("R6: %s parsed the lock's lineage entry for a table past the ceiling", name)
		}
	}
	if !emitted {
		t.Fatal("R6: the positive control Small emitted no form, so the assertion is vacuous")
	}
}

// ---- go/R14 -----------------------------------------------------------------

// t03GoR14 holds R14 to ALGORITHM §5.2's PLAN: the runtime names
// layout_record_too_large for the zero root and the 65536 bound, and — the
// clause the task adds — for a single entry whose source extent reaches past the
// writer's OWN declared record size, refusing the plan WHOLE and never landing a
// short read.
func t03GoR14(t *testing.T) {
	runtime := tableFixedRuntime
	t03GoHas(t, runtime, "if root.Size == 0 || root.Size > tableFixedRecordMaxBytes {", "R14 the zero root and the 65536 bound")
	t03GoHas(t, runtime, `return out, "layout_record_too_large"`, "R14 the reader's own layout names the rule")
	t03GoHas(t, runtime, "if e.Size > tableFixedRecordMaxBytes {", "R14 a single entry past the 65536 bound")
	t03GoHas(t, runtime, `c.fail("layout_record_too_large")`, "R14 a single entry past the ceiling names the rule")
	t03GoHas(t, runtime, "if sum > uint64(tableFixedRecordMaxBytes) {", "R14 a subtree's sum past the 65536 bound")
	t03GoHas(t, runtime, "c.record != 0 && tableFixedSrcEnd(e) > uint64(c.record) {", "R14 an entry reaching past the writer's declared record")
	t03GoHas(t, runtime, "c.tooLarge = true", "R14 the over-reach is recorded")
	t03GoHas(t, runtime, "if c.tooLarge {", "R14 the plan is refused WHOLE")
	t03GoHas(t, runtime, "return -3", "R14 the compile answers layout_record_too_large")
	t03GoHas(t, runtime, `out[i].Why = TableRefuseLayoutRecordTooLarge.Error()`, "R14 the lane carries the name")
	// The generated module carries the runtime, so the ban is on the emitted
	// build and not on this package's constant alone.
	t03GoHas(t, tableFile(generate(t, t03GoFlat())), "tableFixedSrcEnd", "R14 the emitted build carries the entry bound")
}

// t03GoFlat is the smallest declared fixed table with a lineage: two adjacent
// scalars.
func t03GoFlat() string {
	return `package probe

fixed table T
{
    x int32
    y int32
}
`
}

// ---- go/R22 -----------------------------------------------------------------

// t03GoR22 holds R22 to ALGORITHM §5.5 and SPEC-TABLES §2.2: a table or type
// reached by value from a fixed table is itself fixed (a nested FIXED table is a
// closure member, not a break), a pointer, a map or an unbounded array in that
// closure is a compile refusal naming the construct, and a table that reaches
// itself by value — a table in its own closure — is refused.
func t03GoR22(t *testing.T) {
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
		{"variable table", "package probe\ntable Node { x int32 }\nfixed table T { node Node }\n", "holds the variable table Node by value"},
		{"pointer", "package probe\ntable Node { x int32 }\nfixed table Scene { head *Node }\n", "is a pointer"},
		{"map", "package probe\nfixed table Fleet { ships map[uint32]int32 }\n", "is a map"},
		{"unbounded array", "package probe\nfixed table Log { entries []int32 }\n", "is an unbounded array"},
	} {
		errs := t03GoCheckErrs(t, tc.src)
		if len(errs) == 0 {
			t.Errorf("R22 %s: the closure break is not a compile refusal", tc.name)
			continue
		}
		got := errs[0].Error()
		if !strings.Contains(got, tc.want) || !strings.Contains(got, "docs/SPEC-TABLES.md") {
			t.Errorf("R22 %s: refusal = %q, want it to name %q and §2.2", tc.name, got, tc.want)
		}
	}

	// T is never in its own closure: a fixed table that reaches itself by value
	// is refused, so a table cannot be its own closure member.
	if errs := t03GoCheckErrs(t, "package probe\nfixed table T { next T }\n"); len(errs) == 0 {
		t.Error("R22: a fixed table that reaches itself by value is not refused")
	}

	// AND THE FORM'S OWN CLOSURE TEST agrees with the checker for the constructs
	// a variable table may hold: none is a closure this form lays out, so none
	// gets a form-3 emitter.
	for _, tc := range []struct{ src, table string }{
		{"package probe\ntable Node { x int32 }\ntable Scene { head *Node }\n", "Scene"},
		{"package probe\ntable Fleet { ships map[uint32]int32 }\n", "Fleet"},
		{"package probe\ntable Log { entries []int32 }\n", "Log"},
	} {
		u := unitFrom(t, tc.src)
		if ir.TableFixedSupported(t03GoTable(t, u, tc.table), 0) {
			t.Errorf("R22: TableFixedSupported admits %s, whose closure holds a pointer, a map or an unbounded array", tc.table)
		}
	}
}

// ---- go/R3 ------------------------------------------------------------------

// t03GoR3 holds R3 to §5.2's one number: "R.floor := 1 + the highest index
// marked RETIRED in lock.lineage(T), or 0 when none is", emitted beside the
// lineage, and to LOAD step 6: "if i < R.floor: REFUSE layout_unsupported,
// reporting h". The runtime half is TestFixedVersioningFloor/floor_below on the
// corpus; the arithmetic and the emitted site are read here.
func t03GoR3(t *testing.T) {
	for _, tc := range []struct{ retire, want int }{
		{0, 0}, // none retired: the floor is 0
		{1, 1}, // entry 0 retired: the floor is 1
		{2, 2}, // entries 0 and 1 retired: 1 + the HIGHEST retired index
	} {
		text, _ := t03GoRetire(t, tc.retire)
		t03GoHas(t, text, fmt.Sprintf("const LineageFixedFloor = %d", tc.want), "R3 1 + the highest retired index")
		load := t03GoLoad(t, text, "Lineage")
		t03GoHas(t, load, "if pick < LineageFixedFloor {", "R3 below the floor is a comparison of the index")
		t03GoHas(t, load, `return tableFixedRefuseHash(report, TableRefuseLayoutUnsupported.Error(), hash)`, "R3 below the floor is layout_unsupported")
	}
}

// ---- go/R32 -----------------------------------------------------------------

// t03GoR32 holds R32 to §5.2's retirement, read off the emitted load: the
// operator's mark is laid down per entry (`// RETIRED:`), the floor check is
// placed ONCE, after the hash select and BEFORE the byte comparison or any
// record, and its whole body is the named refusal — a pure read of the picked
// index, so a second load of the same file answers the same name. REFUSE is
// TOTAL: the runtime's refuse zeroes every counter.
func t03GoR32(t *testing.T) {
	text, entries := t03GoRetire(t, 1)
	t03GoHas(t, text, "// RETIRED: "+t03GoRetireReason, "R32 the retired entry's mark is laid down")
	t03GoHas(t, text, fmt.Sprintf("0x%016x", entries[0].Wire), "R32 the retired entry keeps its hash in the lineage")
	load := t03GoLoad(t, text, "Lineage")
	if n := strings.Count(load, "if pick < LineageFixedFloor {"); n != 1 {
		t.Errorf("R32: the floor is tested %d times, want once", n)
	}
	at := strings.Index(load, "if pick < LineageFixedFloor {")
	known := strings.Index(load, "known := &LineageFixedKnown[pick]")
	bytes := strings.Index(load, "layout[i] != known.Layout[i]")
	records := strings.Index(load, "for k := range n {")
	if at < 0 || known < 0 || bytes < 0 || records < 0 {
		t.Fatalf("R32: the load's shape moved (%d, %d, %d, %d)", at, known, bytes, records)
	}
	if at >= known || known >= bytes || bytes >= records {
		t.Errorf("R32: the floor check is not before the byte comparison and the record loop (%d, %d, %d, %d)", at, known, bytes, records)
	}
	// ONCE AND IDEMPOTENT: the guarded body reads the picked index and returns
	// the named refusal; nothing before it moves a counter or writes a byte.
	body := load[at:]
	if end := strings.Index(body, "\n\t}"); end >= 0 {
		body = body[:end]
	}
	for _, banned := range []string{"report.Unknown", "report.Widened", "values[", "copy("} {
		if strings.Contains(body, banned) {
			t.Errorf("R32: the floor refusal is not a pure named refusal (%q):\n%s", banned, body)
		}
	}
	for _, zero := range []string{"*report = TableReport{}", "report.Verdict = TableOpenRefused", "report.Reason = reason"} {
		t03GoHas(t, tableFixedRuntime, zero, "R32 REFUSE is TOTAL")
	}
}

// ---- go/R10 -----------------------------------------------------------------

// t03GoR10 holds R10 to §5.6 and §5.3 step 7: "the hash is looked up, the floor
// is checked, and the layout is COMPARED — the seven rules do not fire at run
// time". The emitted load parses no layout, walks no stranger's bytes, and never
// recomputes the header's hash: the header's eight bytes are taken as given and
// matched on.
func t03GoR10(t *testing.T) {
	text, _ := t03GoRetire(t, 0)
	load := t03GoLoad(t, text, "Lineage")
	t03GoHas(t, load, "layout[i] != known.Layout[i]", "R10 the layout is a byte comparison")
	t03GoHas(t, load, `return tableFixedRefuse(report, TableRefuseLayoutMalformed.Error())`, "R10 a lie about a known version is layout_malformed")
	t03GoHas(t, load, "hash := tableFixedGet64(data[TableFixedHashAt:])", "R10 the header's hash is taken as given")
	t03GoHasNot(t, load, "tableFixedParseLayout(", "R10 the load walks a stranger's layout")
	t03GoHasNot(t, load, "tableFixedCompile(", "R10 the load compiles on the load path")
	t03GoHasNot(t, load, "tableFixedHash(", "R10 the load recomputes the header's hash")
	// THE HASH IS STILL A WIRE IDENTITY: the fnv site survives for the build's
	// own constant, but no emitted module computes one at run time.
	if strings.Contains(text, "0xcbf29ce484222325") || strings.Contains(text, "0x100000001b3") {
		t.Error("R10: the emitted module holds an FNV site a runtime could compute with")
	}
	t03GoHas(t, tableFixedRuntime, "func tableFixedParseLayout(", "R10 the layout walk survives for the build-time plans")
}

// ---- go/R15 -----------------------------------------------------------------

// t03GoR15 holds R15 to §5.6's retirement list and §5.3 step 5: the four
// forward-read clamps are gone, and each is `layout_unknown` now. A hash the
// lineage does not hold is refused BEFORE any record and before any plan
// selection, on the file's hash; the count and text clamps that REMAIN are the
// writer's bounds carried by the plan (bill §12.5), which only a backward read
// over a locked entry can reach; and the unknown-field count lands from the
// COMPILE census after a returning read, never per record.
func t03GoR15(t *testing.T) {
	// The four clauses' rows, so a row quietly dropped from the corpus suite is
	// red by name: field_append (drop-and-count of an unknown field),
	// array_bounded_grow (count clamp), range_widen (range clamp),
	// enum_append (remap of an unknown variant to None).
	for _, row := range []string{"field_append", "array_bounded_grow", "range_widen", "enum_append"} {
		found := false
		for _, r := range versionRows {
			if r.row == row {
				found = true
			}
		}
		if !found {
			t.Errorf("R15: %s is not in TestFixedVersioningNewReadsOld's rows: its OLD-REFUSES-NEW column is where the retired clamp is layout_unknown by name", row)
		}
	}
	text, _ := t03GoRetire(t, 0)
	load := t03GoLoad(t, text, "Lineage")
	miss := strings.Index(load, `return tableFixedRefuseHash(report, TableRefuseLayoutUnknown.Error(), hash)`)
	records := strings.Index(load, "for k := range n {")
	if miss < 0 || records < 0 {
		t.Fatalf("R15: the load's refusals moved (%d, %d)", miss, records)
	}
	if miss > records {
		t.Error("R15: an unknown hash is not refused before the first record")
	}
	if strings.Contains(load, "report.Unknown++") {
		t.Error("R15: an unknown field is dropped-and-counted per record on the read path")
	}
	t03GoHas(t, load, "report.Unknown += censusUnknown", "R15 unknown is the compile census, once per plan")
	t03GoHas(t, load, "censusUnknown, censusKind = lane.Unknown, lane.KindMismatch", "R15 the census is the lane's, landed after the record loop")
	// THE CLAMPS THAT REMAIN ARE THE WRITER'S BOUNDS, reached only over a
	// locked entry — never a forward read of a stranger's layout.
	runtime := tableFixedRuntime
	t03GoHas(t, runtime, "v > int32(p.Size)", "R15 the count clamp is the writer's bound")
	t03GoHas(t, runtime, "uint32(v) > capn", "R15 the text clamp is the writer's span")
}

// ---- go/E3/other-required-widens --------------------------------------------

// t03GoE3 holds E3 to §5.2's EMIT ladder, in full: "if LADDER(te.kind,
// me.kind): emit widenf when te is f32 else widen, sign := te.kind is a signed
// integer or a signed fixed-point". Every required rung is present — 2..5, 6..9,
// 20..24, 25..29 and 10 -> 11 — the float rung is `widenf`, the sign is the
// WRITER's kind, and a same-kind widen zero-extends. The non-default and
// boundary values are written and read by the gate's corpus rows (`int_widen`,
// `uint_widen`, `float_widen`, `array_elem_widen`, `enum_width` in versionRows).
func t03GoE3(t *testing.T) {
	runtime := tableFixedRuntime
	for _, rung := range []string{
		"if from >= 6 && from <= 9 && to >= 6 && to <= 9 {",
		"if from >= 2 && from <= 5 && to >= 2 && to <= 5 {",
		"if from >= 20 && from <= 24 && to >= 20 && to <= 24 {",
		"if from >= 25 && from <= 29 && to >= 25 && to <= 29 {",
		"return from == 10 && to == 11",
	} {
		t03GoHas(t, runtime, rung, "E3 the widen ladder")
	}
	t03GoHas(t, runtime, "if te.Kind == 10 {", "E3 the float rung is chosen by the writer's kind")
	t03GoHas(t, runtime, "e.Op = tableFixedWidenF", "E3 the float rung is widenF")
	t03GoHas(t, runtime, "if tableFixedSignedKind(te.Kind) {", "E3 a ladder widen's sign is the writer's kind")
	t03GoHas(t, runtime, "return (kind >= 2 && kind <= 5) || (kind >= 20 && kind <= 24)", "E3 the signed ladder, and only it, sign-extends")
	// The ladder's rungs are the integer and fixed-point families of §1's kind
	// table, and the float rung is f32 into f64.
	if ir.TableKindI8 != 2 || ir.TableKindI64 != 5 || ir.TableKindU8 != 6 || ir.TableKindU64 != 9 {
		t.Errorf("E3: the integer ladder's kinds moved: i8=%d i64=%d u8=%d u64=%d", ir.TableKindI8, ir.TableKindI64, ir.TableKindU8, ir.TableKindU64)
	}
	if ir.TableKindFixed8 != 20 || ir.TableKindFixed128 != 24 || ir.TableKindUFixed8 != 25 || ir.TableKindUFixed128 != 29 {
		t.Errorf("E3: the fixed-point ladder's kinds moved: fixed8=%d fixed128=%d ufixed8=%d ufixed128=%d", ir.TableKindFixed8, ir.TableKindFixed128, ir.TableKindUFixed8, ir.TableKindUFixed128)
	}
	if ir.TableKindF32 != 10 || ir.TableKindF64 != 11 {
		t.Errorf("E3: the float rung's kinds moved: f32=%d f64=%d", ir.TableKindF32, ir.TableKindF64)
	}
}

// ---- go/C8 ------------------------------------------------------------------

// t03GoC8 holds C8 to §3.4's constant-size table and §4.6's bounds: a
// `fixed(I,F)` rides as the raw scaled integer at its storage width, `I + F`,
// and the KIND encodes that width and the family and NOT `F` — so an `F`-only
// edit is a definitions-digest change the lock refuses, never a widen; the
// read-side pass shifts both declared ends by `F` onto the raw scale; and
// `bits(N)` rides at its declared storage width and is clamped at `N`'s own
// ceiling, which is not a storage limit.
func t03GoC8(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table Lineage
{
    a fixed(4, 4) | min = -4, max = 3
    b fixed(2, 6) | min = -1, max = 0
    c fixed(8, 8) | min = -128, max = 127
    d ufixed(4, 4) | min = 0, max = 15
    e bits(12)
}
`)
	st := t03GoTable(t, u, "Lineage")
	kind := map[string]int{}
	for _, f := range st.Fields {
		kind[f.Name] = ir.TableScalarKind(f)
	}
	if kind["a"] != 20 || kind["b"] != 20 {
		t.Errorf("C8: fixed(4,4) and fixed(2,6) are kinds %d and %d, want the same kind 20 — F does not ride in the kind", kind["a"], kind["b"])
	}
	if kind["c"] != 21 {
		t.Errorf("C8: fixed(8,8) is kind %d, want 21 (a 16-bit storage width)", kind["c"])
	}
	if kind["d"] != 25 {
		t.Errorf("C8: ufixed(4,4) is kind %d, want 25 (the unsigned family)", kind["d"])
	}
	if kind["e"] != 7 {
		t.Errorf("C8: bits(12) is kind %d, want 7 — a bits(N) rides at its storage width's integer kind", kind["e"])
	}
	if got := ir.TableFixedStorageBytes(st.Fields[4].Type); got != 4 {
		t.Errorf("C8: bits(12) storage = %d, want 4", got)
	}
	// THE BOUNDS ARE IN VALUE UNITS AND THE STORAGE IS RAW: min = -4, max = 3
	// shifted by F = 4 are the raw ends -64 and 48 the emitted pass carries, and
	// bits(12)'s own top 2^12 - 1 = 4095 is a bound the four storage bytes do
	// not enforce.
	clamp := funcSource(tableFile(generate(t, `package probe

fixed table Lineage
{
    a fixed(4, 4) | min = -4, max = 3
    b bits(12)
}
`)), "func LineageFixedClampBody(")
	if clamp == "" {
		t.Fatal("C8: LineageFixedClampBody is not emitted")
	}
	t03GoHas(t, clamp, "value.A < -64", "C8 the fixed-point floor shifted by F")
	t03GoHas(t, clamp, "value.A > 48", "C8 the fixed-point ceiling shifted by F")
	t03GoHas(t, clamp, "value.B > 4095", "C8 bits(12)'s own ceiling")
	t03GoHas(t, tableFixedRuntime, "case 20, 25: // fixed8, ufixed8", "C8 the fixed-point family's storage widths")
	t03GoHas(t, tableFixedRuntime, "case 24, 29:", "C8 the fixed-point ladder's top rung")
}

// ---- go/R20 -----------------------------------------------------------------

// t03GoR20 holds R20 to SPEC-TABLES §21.2, clause by clause, on the emitted
// sites: an added field lands the prefill's declared default; a deprecated field
// is dropped and counted once per plan through the compile census; a narrower
// integer or float is widened exactly and counts widened; a shorter array or
// string lands with the reader's slack as the template's zeros; an older enum's
// ordinals are the reader's, its list a prefix.
func t03GoR20(t *testing.T) {
	text, _ := t03GoRetire(t, 0)
	load := t03GoLoad(t, text, "Lineage")
	// AN ADDED FIELD: the holes are covered from the defaults image, and the
	// image is a zeroed Reset, laid once at init, so a shorter array or string
	// keeps template zeros without the load path computing either per call (§4.3).
	t03GoHas(t, load, "defBytes := LineageFixedResetImage", "R20 the default image is the declared defaults, laid once")
	t03GoHas(t, load, "holes = LineageFixedLineageHoles[pick]", "R20 a lineage plan's holes are laid once, never per call")
	t03GoHas(t, load, "copy(dst[h.Off:h.Off+h.Size], defBytes[h.Off:h.Off+h.Size])", "R20 an added field lands the declared default")
	// A DEPRECATED FIELD, counted ONCE per plan: the census is the lane's, and
	// an array's element 0 is the only one that censuses.
	t03GoHas(t, load, "report.Unknown += censusUnknown", "R20 the census lands once, after the loop")
	t03GoHas(t, tableFixedRuntime, "if !named && census && c.report != nil {", "R20 an unknown field is counted")
	t03GoHas(t, tableFixedRuntime, "census && i == 0)", "R20 once per FIELD per endpoint, not per element")
	t03GoHasNot(t, load, "report.Unknown++", "R20 the census is never per record")
	// A NARROWER INTEGER OR FLOAT: widened exactly, and counted.
	t03GoHas(t, tableFixedRuntime, "report.Widened++", "R20 a widen counts")
	// A SHORTER ARRAY OR STRING: the plan writes only the writer's live extent
	// and the slack keeps the image's zeros.
	t03GoHas(t, tableFixedRuntime, "func tableFixedHoles(", "R20 the reader's slack is the template's zeros")
	t03GoHas(t, tableFixedRuntime, "v > int32(p.Size)", "R20 a shorter array lands the writer's live extent")
	t03GoHas(t, tableFixedRuntime, "uint32(v) > capn", "R20 a shorter string lands the writer's live extent")
	// AN OLDER ENUM: the remap is by NAME and its list is the writer's count, so
	// a prefix maps position for position.
	t03GoHas(t, tableFixedRuntime, "case tableFixedOrdinal:", "R20 the ordinal is resolved through the plan's remap")
	t03GoHas(t, tableFixedRuntime, "n := te.Children", "R20 the remap table is as long as the writer's variant count")
	t03GoHas(t, tableFixedRuntime, "landed = uint16(k + 1)", "R20 the older enum's ordinals are the reader's")
}

// ---- go/R21 -----------------------------------------------------------------

// t03GoR21 holds R21 to §4's float rung under the finite rule of 2026-10-27
// (docs/SPEC-TABLES.md §3.3, §21.2): "widenF ... by the ordinary conversion
// (every finite f32 is exact in an f64; a non-finite source is refused as
// non_finite_float)". The arm is the ordinary conversion, which keeps a NaN a
// NaN and an infinity an infinity, and the read's finite pass over storage
// (<T>FixedFiniteBody, emitted per type) is what refuses one afterwards; the
// NaN bit surgery that used to carry a payload is gone.
func t03GoR21(t *testing.T) {
	runtime := tableFixedRuntime
	widenF := t03GoBetween(runtime, "case tableFixedWidenF:", "case tableFixedConst:")
	if widenF == "" {
		t.Fatal("R21: the runtime has no tableFixedWidenF arm")
	}
	t03GoHas(t, widenF, "math.Float64bits(float64(math.Float32frombits(tableFixedGet32(src[p.Src:]))))", "R21 the rung is the ordinary conversion")
	t03GoHasNot(t, widenF, "<<29", "R21 the retired NaN payload surgery")
	t03GoHasNot(t, widenF, "0x7ff0000000000000", "R21 the retired hand-assembled exponent")
	// THE CLAUSE EXECUTED, not merely present as text: the emitted arm's own
	// body is lifted out of tableFixedRuntime and RUN over finite patterns,
	// -0 and the subnormals included, which must land exactly, and over the
	// non-finite ones, which must stay non-finite for the finite pass to see.
	t03GoR21Exec(t, widenF)
}

// t03GoR21Exec compiles and runs the exact text of the emitted
// tableFixedWidenF arm, wiring the few names the arm reads (src, dst, p,
// report, tableFixedGet32, tableFixedPut64, math) around it, so a planted
// edit to the arm turns this red by the bits it lands, not by an absent
// substring (docs/SPEC-TABLES.md §3.3, §21.2).
func t03GoR21Exec(t *testing.T, arm string) {
	t.Helper()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated widen arm)")
	probe := "package probe\n\n" +
		"import (\n\t\"encoding/binary\"\n\t\"math\"\n\t\"testing\"\n)\n\n" +
		"const tableFixedWidenF uint8 = 6\n\n" +
		"type TableFixedEntry struct{ Src, Dst uint32 }\n" +
		"type TableReport struct{ Widened int32 }\n\n" +
		"func tableFixedGet32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }\n" +
		"func tableFixedPut64(b []byte, v uint64) { binary.LittleEndian.PutUint64(b, v) }\n\n" +
		"func widenF(src, dst []byte, p TableFixedEntry, report *TableReport) {\n" +
		arm + "\n}\n\n" +
		"func TestWidenF(t *testing.T) {\n" +
		"\tfor _, c := range []struct {\n\t\tin   uint32\n\t\twant uint64\n\t\twhat string\n\t}{\n" +
		"\t\t{0x3f800000, 0x3ff0000000000000, \"finite widens exactly\"},\n" +
		"\t\t{0x80000000, 0x8000000000000000, \"-0 keeps its sign\"},\n" +
		"\t\t{0x007fffff, 0x380fffffc0000000, \"the largest subnormal is exact\"},\n" +
		"\t\t{0x80000001, 0xb6a0000000000000, \"the smallest negative subnormal is exact\"},\n" +
		"\t\t{0x7f7fffff, 0x47efffffe0000000, \"the largest finite float32 is exact\"},\n" +
		"\t} {\n" +
		"\t\tsrc := make([]byte, 4)\n\t\tdst := make([]byte, 8)\n" +
		"\t\tbinary.LittleEndian.PutUint32(src, c.in)\n" +
		"\t\tvar r TableReport\n" +
		"\t\twidenF(src, dst, TableFixedEntry{}, &r)\n" +
		"\t\tif got := binary.LittleEndian.Uint64(dst); got != c.want {\n" +
		"\t\t\tt.Errorf(\"%s: widenF(0x%08x) landed 0x%016x, want 0x%016x\", c.what, c.in, got, c.want)\n\t\t}\n" +
		"\t\tif r.Widened != 1 {\n" +
		"\t\t\tt.Errorf(\"%s: the widen did not count: Widened=%d\", c.what, r.Widened)\n\t\t}\n" +
		"\t}\n" +
		"\tfor _, in := range []uint32{0x7f800001, 0x7fc00000, 0xffa5a5a5, 0x7f800000, 0xff800000} {\n" +
		"\t\tsrc := make([]byte, 4)\n\t\tdst := make([]byte, 8)\n" +
		"\t\tbinary.LittleEndian.PutUint32(src, in)\n" +
		"\t\twidenF(src, dst, TableFixedEntry{}, &TableReport{})\n" +
		"\t\tif v := math.Float64frombits(binary.LittleEndian.Uint64(dst)); v-v == 0 {\n" +
		"\t\t\tt.Errorf(\"widenF(0x%08x) landed the finite %v: the finite pass could not refuse it\", in, v)\n\t\t}\n" +
		"\t}\n}\n"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module probe\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "widen_test.go"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("R21: the emitted widen arm failed to run bit-exact:\n%v\n%s", err, out)
	}
}

// ---- go/R29 -----------------------------------------------------------------

// t03GoR29 holds R29 to the 65536 band and to bill §12.5's carried writer
// bounds. THE BAND: a record body AT 65536 is a fixed root and one byte past it
// is not (SPEC-TABLES §3.4, "65536 BYTES OF RECORD BODY: THE FORM IS NOT
// EMITTED, AND THE TABLE IS NAMED"), a u16 top widens into a u32 zero-extended
// across that ceiling, and a layout entry reaching past the writer's own
// declared record refuses the plan WHOLE under layout_record_too_large. THE
// WRITER'S BOUNDS: the count op clamps to the writer's element bound carried by
// the plan and the text op to the writer's span, so a value forged past what the
// WRITER could have written clamps and counts while a value the reader merely
// widened does not.
func t03GoR29(t *testing.T) {
	if ir.TableFixedRecordMaxBytes != 65536 {
		t.Fatalf("R29: the ceiling is %d, want 65536", ir.TableFixedRecordMaxBytes)
	}
	at := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    blob bytes(65532)\n}\n")
	past := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    blob bytes(65533)\n}\n")
	if got := ir.TableFixedTypeBytes(at.Tables["Lineage"]); got != 65536 {
		t.Fatalf("R29: the AT fixture body is %d, want 65536", got)
	}
	found := false
	for _, r := range ir.TableFixedFormRoots(at) {
		if r.Name == "Lineage" {
			found = true
		}
	}
	if !found {
		t.Error("R29: a body AT 65536 is not a fixed-form root")
	}
	for _, r := range ir.TableFixedFormRoots(past) {
		if r.Name == "Lineage" {
			t.Error("R29: a body one past 65536 is still a fixed-form root")
		}
	}
	warnings, errs := ir.TableFixedRecordBounds(past, 0)
	if len(errs) != 0 {
		t.Errorf("R29: a body past the ceiling is a warning, not a refusal: %v", errs)
	}
	if joined := strings.Join(warnings, "\n"); !strings.Contains(joined, "Lineage") {
		t.Errorf("R29: the warning does not name the table: %s", joined)
	}
	runtime := tableFixedRuntime
	t03GoHas(t, runtime, "if root.Size == 0 || root.Size > tableFixedRecordMaxBytes {", "R29 the reader holds the endpoint's layout to the same ceiling")
	t03GoHas(t, runtime, "if e.Size > tableFixedRecordMaxBytes {", "R29 a single entry past the ceiling")
	t03GoHas(t, runtime, "c.record != 0 && tableFixedSrcEnd(e) > uint64(c.record) {", "R29 an entry past the writer's declared record")
	t03GoHas(t, runtime, "v > int32(p.Size)", "R29 the count clamps to the writer's bound")
	t03GoHas(t, runtime, "uint32(v) > capn", "R29 the text clamps to the writer's span")
	t03GoHas(t, runtime, "n := theirN", "R29 the count's bound is the writer's")
	t03GoHas(t, runtime, "n = min(n, myN)", "R29 the count's bound is held to the reader's storage")
	t03GoHas(t, runtime, "Size: n,", "R29 the count entry carries the writer's bound")
	// THE BAND EDGE IS ZERO-EXTENDED, NOT SIGN-EXTENDED: a u16 top widened
	// into a u32 has the WRITER's kind 7, which is unsigned, so no sign bit is
	// set and the raw 65535 lands 65535 and not -1.
	t03GoHas(t, runtime, "if tableFixedSignedKind(te.Kind) {", "R29 the widen's sign is the writer's kind")
	if ir.TableKindSigned(ir.TableKindU16) {
		t.Error("R29: the u16 kind is signed, so the band edge would sign-extend across the ceiling")
	}
}

// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapGoT03PlansVersions(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "go/R6", check: t03GoR6},
		{id: "go/R14", check: t03GoR14},
		{id: "go/R22", check: t03GoR22},
		{id: "go/R3", check: t03GoR3},
		{id: "go/R32", check: t03GoR32},
		{id: "go/R10", check: t03GoR10},
		{id: "go/R15", check: t03GoR15},
		{id: "go/E3/other-required-widens", check: t03GoE3},
		{id: "go/C8", check: t03GoC8},
		{id: "go/R20", check: t03GoR20},
		{id: "go/R21", check: t03GoR21},
		{id: "go/R29", check: t03GoR29},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
