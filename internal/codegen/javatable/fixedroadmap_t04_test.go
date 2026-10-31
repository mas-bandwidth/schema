package javatable

// fixedroadmap_t04 — the java leg's field-evolution, reports and text rows of
// docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW Fixed Tables"), the
// assertions no other test of this leg makes. Law:
// docs/FIXED-FORM-ALGORITHM.md §5.4 (the counters), §4.5 (the scatter, fix 11's
// text content rule), §5.9 #29/#30 (duplicate never raised, the census is
// silent on a lawful lineage), docs/SPEC-TABLES.md §3.4 (the record, the text
// kinds, the slack rule). One subtest per task id, table-driven, t.Parallel()
// first.
//
// EVERY ASSERTION HERE READS THE JAVA THIS LEG EMITS — the shared TableFixed
// runtime body and the per-table surface — so the harness is reachable on a
// tree that never built the C++ reference corpus and never carries a sibling
// runtime checkout or a JDK: the compile-and-run half of this leg's fixed form
// is fixedversioning_test.go, which skips exactly where a JDK or the corpus is
// absent. A [verify] task is proved here by its clause's own site in the
// emitted source; a [weak] task by the direct assertion of every clause; an
// [owed] task red-first, the missing behaviour implemented in this leg's
// emitter or runtime.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// t04Has is a required substring of the emitted source, named by rule.
func t04Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !javaCarries(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// t04No is a forbidden substring of the emitted source, named by rule.
func t04No(t *testing.T, src, bad, what string) {
	t.Helper()
	if javaCarries(src, bad) {
		t.Errorf("%s: the emitted source still carries %q", what, bad)
	}
}

// t04Runtime is this leg's shared runtime body: the one TableFixed class every
// generated fixed surface reaches for its counters and its text rules.
func t04Runtime() string { return tableFixedRuntime }

// t04Table is one declared fixed table out of a unit.
func t04Table(t *testing.T, u *ir.Unit, name string) *ir.Struct {
	t.Helper()
	return findTable(t, u, name)
}

// t04Surface is the emitted fixed surface of a unit: the one <Root>Fixed.java
// the table backend produced, never the shared TableFixed.java beside it.
func t04Surface(t *testing.T, files map[string][]byte) string {
	t.Helper()
	for name, data := range files {
		if name != "TableFixed.java" && strings.HasSuffix(name, "Fixed.java") {
			return string(data)
		}
	}
	t.Fatal("no *Fixed.java surface was emitted")
	return ""
}

// ---- java/E6 ---------------------------------------------------------------

// t04E6 holds java/E6 to SPEC-TABLES §5: a field's table-wire id is the name it
// was, so the layout bytes of `b int32 | was = "a"` are byte for byte the layout
// bytes of a plain `a int32`, and the runtime needs no rename rule of its own.
func t04E6(t *testing.T) {
	renamed := t04Table(t, unitFrom(t, `package probe

fixed table T
{
    b int32 | was = "a"
}
`), "T")
	f := renamed.Fields[0]
	if f.Name != "b" {
		t.Fatalf("E6: the fixture's field is named %q, want b", f.Name)
	}
	if got := ir.TableFieldWireName(f); got != "a" {
		t.Fatalf("E6: the renamed field's declared identity = %q, want a", got)
	}
	was := t04Table(t, unitFrom(t, `package probe

fixed table T
{
    a int32
}
`), "T")
	renamedLayout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(renamed))
	wasLayout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(was))
	if string(renamedLayout) != string(wasLayout) {
		t.Errorf("E6: the rename moved the layout bytes:\n renamed % x\n was     % x", renamedLayout, wasLayout)
	}
	if ir.TableFieldWireId(f) != ir.TableFieldWireId(was.Fields[0]) {
		t.Error("E6: the renamed field's wire id differs from the old name's")
	}
}

// ---- java/E8 ---------------------------------------------------------------

// t04E8 holds java/E8 to the backward-read contract: a `| deprecated` field is
// RETIRED IN PLACE (its slot stays and the walk keeps its entry), and an
// APPENDED field rides the prefill to its declared default while the census
// stays silent on a lawful lineage.
func t04E8(t *testing.T) {
	dep := t04Table(t, unitFrom(t, `package probe

fixed table T
{
    a int32
    b int32 | deprecated
    c int32
}
`), "T")
	if len(dep.Fields) != 3 {
		t.Fatalf("E8: the deprecated field's slot did not stay: %d fields", len(dep.Fields))
	}
	if !dep.Fields[1].Deprecated {
		t.Error("E8: the `| deprecated` marker is not on the IR field")
	}
	plain := t04Table(t, unitFrom(t, `package probe

fixed table T
{
    a int32
    b int32
    c int32
}
`), "T")
	if got, want := len(ir.TableFixedWalkRoot(dep)), len(ir.TableFixedWalkRoot(plain)); got != want {
		t.Errorf("E8: the deprecated field's slot did not stay: %d walked entries, want the variable table's %d", got, want)
	}
	// THE APPEND lands its declared default in the prefill image.
	root := t04Table(t, unitFrom(t, `package probe

fixed table T
{
    x int32 = 7
}
`), "T")
	image := fixedDefaultImage(root)
	if got := int32(image[0]) | int32(image[1])<<8 | int32(image[2])<<16 | int32(image[3])<<24; got != 7 {
		t.Errorf("E8: the appended x = %d, want its declared default 7", got)
	}
	out, err := Generate(unitFrom(t, `package probe

fixed table T
{
    a int32
}
`))
	if err != nil {
		t.Fatalf("E8: Generate: %v", err)
	}
	t04Has(t, t04Surface(t, out), "lands zero, always (§5.9 #30)", "E8 the census is silent on a lawful lineage")
}

// ---- java/R19 --------------------------------------------------------------

// t04R19 holds java/R19: a clean NEW-READS-OLD moves no counter at all. The
// report resets to zero, the census adds the plan's own number (zero on a
// lawful lineage), and no op bumps a counter on a clean read.
func t04R19(t *testing.T) {
	runtime := t04Runtime()
	for _, want := range []string{
		"unknown = 0; kindMismatch = 0; widened = 0; clamped = 0;",
		"malformed = false; refused = false; reason = Reason.none; layoutHash = 0;",
	} {
		t04Has(t, runtime, want, "R19 a clean read moves nothing")
	}
	out, err := Generate(unitFrom(t, `package probe

fixed table T
{
    a int32
}
`))
	if err != nil {
		t.Fatalf("R19: Generate: %v", err)
	}
	surface := t04Surface(t, out)
	for _, want := range []string{
		"report.unknown += censusUnknown;",
		"report.kindMismatch += censusKind;",
		"lands zero, always (§5.9 #30)",
	} {
		t04Has(t, surface, want, "R19 the census adds the plan's own number once")
	}
}

// ---- java/E9 ---------------------------------------------------------------

// t04E9 holds java/E9: `duplicate` never raised. The fixed wire never raises the
// text form's `duplicate` counter (§5.9 #29), so the report carries no such
// member and no emitted surface names it.
func t04E9(t *testing.T) {
	runtime := t04Runtime()
	t04No(t, runtime, "duplicate", "E9 the runtime never carries a duplicate counter")
	out, err := Generate(unitFrom(t, `package probe

fixed table T
{
    x int32
}
`))
	if err != nil {
		t.Fatalf("E9: Generate: %v", err)
	}
	for name, data := range out {
		// A fixed unit also emits secondary variable codecs. Duplicate belongs
		// to their map/text reports; E9 constrains the fixed surface alone.
		if name == "TableVariable.java" || name == "TableRetain.java" || name == "TableJson.java" || strings.HasSuffix(name, "Variable.java") || strings.HasSuffix(name, "Retain.java") {
			continue
		}
		if strings.Contains(string(data), "duplicate") {
			t.Errorf("E9: %s names duplicate, which the fixed wire never raises", name)
		}
	}
}

// ---- java/R16 --------------------------------------------------------------

// t04R16 holds java/R16 to §5.4's counters, each at the site that owns it: the
// census ONCE per endpoint after the loop, the widen and clamp bumps of the plan's
// run, the bounds pass's forged-ordinal clamp, and copy/const/present/ordinal
// moving nothing.
func t04R16(t *testing.T) {
	runtime := t04Runtime()
	for _, want := range []string{
		// unknown: ONCE PER ENDPOINT, after the record loop (§5.9 #6).
		"if (!named && census && c.report != null) { c.report.unknown++; }",
		// widened: once per entry per record.
		"report.widened++;",
		// clamped: once per entry per record, count and text.
		"else if (v > p.size) { v = p.size; report.clamped++; }",
		"else if (v > cap) { v = cap; report.clamped++; }",
		// the bounds pass counts a forged ordinal remapped to None on BOTH plans.
		"if (raw > (p.dstsize & 0xFFL)) { report.clamped++; }",
	} {
		t04Has(t, runtime, want, "R16 §5.4's counters")
	}
	// the census lands ONCE, after the loop, on a read that returns.
	out, err := Generate(unitFrom(t, `package probe

fixed table T
{
    a int32
}
`))
	if err != nil {
		t.Fatalf("R16: Generate: %v", err)
	}
	t04Has(t, t04Surface(t, out), "report.unknown += censusUnknown;", "R16 unknown lands once per endpoint")
	// widened is EXACTLY min(their_n, my_n) (§5.9 #33). The figure is asserted
	// two ways: the versioning fixture's array_elem_widen row declares widened ==
	// 4 and widenedError holds the runtime to that exact number — a folded read
	// would report fewer and fail, so a bare `widened > 0` cannot slip through —
	// and the compile-shape checks below prove the "never folded" half, that the
	// array case emits the element min(their_n, my_n) times, each element its own
	// widen entry.
	widenRow := versioningRow{name: "array_elem_widen", widen: true, widened: 4}
	if msg := widenedError(widenRow, probeRun{widened: 1}); msg == "" {
		t.Error("R16: a folded read (widened=1) must fail the exact count, not pass it")
	}
	if msg := widenedError(widenRow, probeRun{widened: 4}); msg != "" {
		t.Errorf("R16: widened=4 must pass the exact count, got %q", msg)
	}
	// THE FIXTURE ITSELF DECLARES THE FIGURE, so the exact count is what the
	// versioning run asserts and not merely nonzero: a row that loses `widened`
	// falls back to the loose check §5.9 #33 rejects.
	for _, row := range versioningRows {
		if row.name != "array_elem_widen" {
			continue
		}
		if !row.widen || row.widened != 4 {
			t.Errorf("R16: array_elem_widen must declare widen:true and widened:4 (§5.9 #33), got widen=%v widened=%d", row.widen, row.widened)
		}
	}
	for _, want := range []string{
		"final int n = Math.min(theirN, myN);",
		"compileEntry(c, theirs, ti + 1, theirAt + head + i * theirElem,",
	} {
		t04Has(t, runtime, want, "R16 the array case emits element by element, never a folded run (§5.9 #33)")
	}
	// COPY, CONST, PRESENT AND ORDINAL MOVE NOTHING: the opCopy case carries no
	// counter bump at all (only its framing-damage `malformed` guard), so the
	// only counters the run loop can move are the widen, count/text clamp and
	// forged-ordinal bumps the clauses above name.
	runtime = javaTokens(runtime)
	start := strings.Index(runtime, javaTokens("case opCopy: {"))
	end := strings.Index(runtime, javaTokens("case opCount: {"))
	if start < 0 || end < 0 || end < start {
		t.Fatal("R16: the run loop has no opCopy/opCount cases to bound")
	}
	copyBody := runtime[start:end]
	for _, bad := range []string{"clamped++", "widened++", "unknown++", "kindMismatch++"} {
		if strings.Contains(copyBody, bad) {
			t.Errorf("R16: opCopy moves a counter (%s); copy lands its bytes and moves nothing", bad)
		}
	}
}

// ---- java/R18 --------------------------------------------------------------

// t04R18 holds java/R18: a clamp that cannot fire is not emitted and nothing
// moves. A bits(N) whose width FILLS its storage width has no read-side clamp
// (the comparison would be `> 2^N - 1` against a value of that very width); a
// bits(N) under it does.
func t04R18(t *testing.T) {
	full, err := Generate(unitFrom(t, `package probe

fixed table T
{
    x bits(32)
}
`))
	if err != nil {
		t.Fatalf("R18: Generate: %v", err)
	}
	t04No(t, t04Surface(t, full), "clamps to 2^N - 1", "R18 a bits(32) clamp that cannot fire is not emitted")
	partial, err := Generate(unitFrom(t, `package probe

fixed table T
{
    x bits(12)
}
`))
	if err != nil {
		t.Fatalf("R18: Generate: %v", err)
	}
	t04Has(t, t04Surface(t, partial), "clamps to 2^N - 1", "R18 a bits(12) clamp that can fire is emitted")
}

// ---- java/C3 ---------------------------------------------------------------

// t04C3 holds java/C3: a text length is clamped ONCE, counting one `clamped`, at
// the field's declared capacity — the compiled plan's text op and the identity
// path's scatter count alike.
func t04C3(t *testing.T) {
	runtime := t04Runtime()
	t04Has(t, runtime, "else if (v > cap) { v = cap; report.clamped++; }", "C3 the text op clamps the length")
	out, err := Generate(unitFrom(t, `package probe

fixed table T
{
    label string(8)
}
`))
	if err != nil {
		t.Fatalf("C3: Generate: %v", err)
	}
	surface := t04Surface(t, out)
	t04Has(t, surface, "if (n < 0) { n = 0; r.clamped++; } else if (n > 8) { n = 8; r.clamped++; }", "C3 a string(N)'s length clamps to its bound")
}

// ---- java/C4 ---------------------------------------------------------------

// t04C4 holds java/C4 to §4.5 fix 11: text content refuses BY NAME. Both text
// kinds and the compiled plan's text op refuse the named `textIllFormed` on a
// content violation, never landing the default.
func t04C4(t *testing.T) {
	runtime := t04Runtime()
	t04Has(t, runtime, "textIllFormed", "C4 the named refusal exists")
	t04Has(t, runtime, "if (!textOk(p.meta, src, srcAt + p.src + 4, v)) { report.refuse(Reason.textIllFormed); return; }", "C4 the compiled text op refuses by name")
	t04Has(t, runtime, "if (cp < min || cp > 0x10FFFF || (cp >= 0xD800 && cp <= 0xDFFF)) { return false; }", "C4 over-range UTF-8 (a code point past U+10FFFF) refuses by name")
	out, err := Generate(unitFrom(t, `package probe

fixed table T
{
    label string(8)
}
`))
	if err != nil {
		t.Fatalf("C4: Generate: %v", err)
	}
	surface := t04Surface(t, out)
	t04Has(t, surface, "if (!TableFixed.textOk(TableFixed.textUtf8, b, at + 4, v.labelLength)) { r.refuse(TableFixed.Reason.textIllFormed); return; }", "C4 a byte string refuses by name")
}

// ---- java/R24 --------------------------------------------------------------

// t04R24 holds java/R24 to all three clauses: ill-formed text in the USED units
// refuses by name (`textIllFormed`); the length and its clamp are counted in
// BYTES; and slack is unspecified on read and not a refusal.
func t04R24(t *testing.T) {
	runtime := t04Runtime()
	// (1) the used units alone refuse by name.
	t04Has(t, runtime, "report.refuse(Reason.textIllFormed)", "R24 ill-formed text refuses by name")
	t04Has(t, runtime, "cp > 0x10FFFF", "R24 ill-formed text past U+10FFFF is a refusal, not a default")
	// (2) the length is in bytes and the clamp is the writer's span.
	t04Has(t, runtime, "final int cap = p.size;", "R24 the clamp is the writer's span")
	// (3) slack is unspecified: the content check holds the USED bytes (v) and
	// never the declared bound p.size.
	t04Has(t, runtime, "if (!textOk(p.meta, src, srcAt + p.src + 4, v)) {", "R24 the content check holds the used units and not the slack")
}

// ---- the card's test -------------------------------------------------------

func TestFixedRoadmapJavaT04Evolution(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "java/E6", check: t04E6},
		{id: "java/E8", check: t04E8},
		{id: "java/R19", check: t04R19},
		{id: "java/E9", check: t04E9},
		{id: "java/R16", check: t04R16},
		{id: "java/R18", check: t04R18},
		{id: "java/C3", check: t04C3},
		{id: "java/C4", check: t04C4},
		{id: "java/R24", check: t04R24},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
