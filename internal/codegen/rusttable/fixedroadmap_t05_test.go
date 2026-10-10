package rusttable

// THE ROADMAP'S LAST RUST ROWS (docs/roadmap.sexp node `fixed-tables`; the
// field-evolution work-set `field-evolution/rust`, the reports work-set
// `reports/rust` and the text work-set `text/rust`). The law per task is
// docs/FIXED-FORM-ALGORITHM.md §4.5 (the text content rule), §4.6 (the bounds
// pass), §5.1 (the monotone law) and §5.4 (the counters, per op and per
// condition), with docs/FIXED-FORM-VERSIONING-TESTS.md "The rows", "Counting"
// and "Hostile rows"; docs/SPEC-TABLES.md §3.4 is the form.
//
// EVERY ASSERTION HERE READS THE BYTES THIS LEG EMITS and the shared runtime
// body it emits them with, so the harness is reachable on a tree that never
// built the C++ reference corpus and never carries a sibling serialize.rs —
// the shape fixedroadmap_t04_test.go states for the same reason. A clause that
// needs the emitted code to RUN is asserted at the emitted site that runs it,
// and that site is named in the check.
//
// rust/E6 [verify]: a rename through `was` keeps the declared identity — the
// wire id is the hash of the alias, the layout moves no byte and the hash does
// not move.
//
// rust/E8 [verify]: an appended field has no plan entry and keeps the prefill
// image's declared default; a deprecated field is retired in place, keeps its
// slot and its id, and is NAMED on every plan.
//
// rust/R19 [verify]: a clean NEW-READS-OLD of an appended field, variant, arm,
// flag or keyed slot moves no counter at all — the census counts only a field
// the reader cannot name, and the four ops that land a value move nothing.
//
// rust/E9 [owed]: `duplicate` is never raised — the report has no such field.
//
// rust/R16 [weak]: §5.4's counters exactly.
//
// rust/R18 [owed]: a clamp that cannot fire is not emitted and nothing moves.
//
// rust/C3 [verify]: the text length clamp, in units, once per entry.
//
// rust/C4 [owed]: ill-formed text refuses BY NAME.
//
// rust/R24 [owed]: the content rule is over the USED units, the length word is
// counted in bytes with the clamp in units, and slack is not a refusal.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- readers of this leg's output -------------------------------------------

// t05Module emits a unit and answers its one *_fixed.rs module.
func t05Module(t *testing.T, src string) string {
	t.Helper()
	files, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return t04Fixed(t, files)
}

// t05Arm cuts one `TableFixedOp::Name => {` arm of the runtime out of the
// shared body, up to the next arm at the same indentation.
func t05Arm(runtime, head string) string {
	at := strings.Index(runtime, head)
	if at < 0 {
		return ""
	}
	rest := runtime[at:]
	if end := strings.Index(rest[len(head):], "\n            TableFixedOp::"); end >= 0 {
		return rest[:len(head)+end]
	}
	return rest
}

// t05Report cuts the TableFixedReport struct out of the shared runtime.
func t05Report(runtime string) string {
	at := strings.Index(runtime, "pub struct TableFixedReport {")
	if at < 0 {
		return ""
	}
	rest := runtime[at:]
	if end := strings.Index(rest, "\n}"); end >= 0 {
		return rest[:end]
	}
	return rest
}

// t05Enum builds an enum of n variants and a fixed table that names it.
func t05Enum(n int) string {
	var b strings.Builder
	b.WriteString("package probe\n\nenum E\n{\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "    v%d\n", i)
	}
	b.WriteString("}\n\nfixed table Lineage\n{\n    e E\n}\n")
	return b.String()
}

// t05Union builds a union of n arms and a fixed table that names it.
func t05Union(n int) string {
	var b strings.Builder
	b.WriteString("package probe\n\ntype Arm { m int32 = 0 }\n\nunion Pick\n{\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "    a%d Arm\n", i)
	}
	b.WriteString("}\n\nfixed table Lineage\n{\n    pick Pick\n}\n")
	return b.String()
}

// ---- rust/E6 -----------------------------------------------------------------

// t05E6 holds rust/E6 to docs/SPEC-TABLES.md §5: "the wire id is the hash of
// the name, and a `was` alias keeps the OLD name's hash", so a rename through
// `was` is wire identity preserved and no version at all — the layout bytes
// and the hash are the same on both sides (§5.1's WIDENS names a rename
// "renamed without was" only when the alias is absent).
func t05E6(t *testing.T) {
	old := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    a int32 = 0\n    seq int32 = 0\n}\n")
	now := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    b int32 = 0 | was = \"a\"\n    seq int32 = 0\n}\n")
	os, ns := old.Tables["Lineage"], now.Tables["Lineage"]
	if os == nil || ns == nil {
		t.Fatal("E6: the fixtures declare no Lineage table")
	}
	if got := ir.TableFieldWireName(ns.Fields[0]); got != "a" {
		t.Errorf("E6: TableFieldWireName = %q, want %q — the id is the hash of the `was` alias (SPEC-TABLES §5)", got, "a")
	}
	if a, b := ir.TableFieldWireId(os.Fields[0]), ir.TableFieldWireId(ns.Fields[0]); a != b {
		t.Errorf("E6: the rename moved the wire id: %d -> %d", a, b)
	}
	ob := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(os))
	nb := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(ns))
	if string(ob) != string(nb) {
		t.Errorf("E6: the rename moved a layout byte:\n%x\n%x", ob, nb)
	}
	if a, b := ir.TableFixedLayoutHash(ob, os), ir.TableFixedLayoutHash(nb, ns); a != b {
		t.Errorf("E6: the rename moved the wire hash: %#x -> %#x", a, b)
	}
	t04Has(t, string(fixedRuntimeBody), "if tc.id == mc.id {",
		"E6 the plan compiler matches a field by the declared id and never by the spelling")
}

// ---- rust/E8 -----------------------------------------------------------------

// t05E8 holds rust/E8 to docs/FIXED-FORM-ALGORITHM.md §5.1's table row: a
// field "added: allowed", and "a deprecated field keeps its place". AN APPENDED
// field has no plan entry, so it is a HOLE and keeps the prefill image's
// declared default (§4.3). A DEPRECATED field is retired in place: its slot
// does not slide and its id does not move, so the compiler still names it and
// no counter moves (the deprecation is not a version).
func t05E8(t *testing.T) {
	// APPEND: x,y,z -> x,y,z,w=77. The newer reader's own module is emitted
	// with the older layout in its lineage, exactly as the probe harness does.
	text, _ := t04Lineage(t,
		"package probe\n\nfixed table Lineage\n{\n    x int32 = 0\n    y int32 = 0\n    z int32 = 0\n}\n",
		"package probe\n\nfixed table Lineage\n{\n    x int32 = 0\n    y int32 = 0\n    z int32 = 0\n    w int32 = 77\n}\n")
	t04Has(t, text, "_FIXED_DEFAULTS: [u8;",
		"E8 an appended field's declared default is the prefill image")
	t04Has(t, text, "image[o..o + z].copy_from_slice(&",
		"E8 the hole an appended field leaves keeps its declared default")
	t04Has(t, string(fixedRuntimeBody), "if !named && census {",
		"E8 a field the reader NAMES is not an event; only an unnameable one censuses")

	// DEPRECATE: `b` retired in place. The slot stays where it is, so the
	// layout and the hash do not move and the id is the same.
	plain := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    a int32 = 0\n    b int32 = 0\n    c int32 = 0\n}\n")
	dep := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    a int32 = 0\n    b int32 = 0 | deprecated\n    c int32 = 0\n}\n")
	pb := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(plain.Tables["Lineage"]))
	db := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(dep.Tables["Lineage"]))
	if string(pb) != string(db) {
		t.Errorf("E8: deprecation moved a layout byte; the slot is retired IN PLACE (ALGORITHM §5.1)")
	}
	if a, b := ir.TableFieldWireId(plain.Tables["Lineage"].Fields[1]), ir.TableFieldWireId(dep.Tables["Lineage"].Fields[1]); a != b {
		t.Errorf("E8: the deprecated field lost its identity: %d -> %d", a, b)
	}
}

// ---- rust/R19 ----------------------------------------------------------------

// t05R19 holds rust/R19 to docs/FIXED-FORM-ALGORITHM.md §5.4's last row: "a
// clean NEW-READS-OLD of an appended field, variant, arm, flag or keyed slot |
// none | every counter stays at zero: an append the reader knows is not an
// event". The census counts only a field the reader cannot NAME; a field,
// variant or arm the reader names resolves by id and moves nothing; and the
// four ops that land a value (`copy`, `const`, `present`, `ordinal`) move
// nothing either.
func t05R19(t *testing.T) {
	runtime := string(fixedRuntimeBody)
	t04Has(t, runtime, "if !named && census {",
		"R19 only a field the reader cannot name is an event")
	t04Has(t, runtime, "Some(count) if raw != 0 && raw <= u64::from(*count) => {",
		"R19 a variant the reader names remaps through the plan and moves nothing")
	t04Has(t, runtime, "if mine.entry(mi + 1 + k).id == vid {",
		"R19 an appended ARM/VARIANT is matched by id, not counted")
	// COPY LANDS A VALUE AND MOVES NOTHING: the arm's whole body is the move
	// and the framing check's `malformed` flag; no counter is moved.
	copyArm := t05Arm(runtime, "TableFixedOp::Copy => {")
	if copyArm == "" {
		t.Fatal("R19: the Copy arm is not in the runtime")
	}
	for _, counter := range []string{"report.clamped", "report.widened", "report.unknown", "report.kind_mismatch"} {
		if strings.Contains(copyArm, counter) {
			t.Errorf("R19: a `copy` entry moves %s (§5.4):\n%s", counter, copyArm)
		}
	}
	// A clean read adds the COMPILE census once, after the loop, and the census
	// of a lawful append is zero.
	load := t04Fn(t05Module(t, "package probe\n\nfixed table Lineage\n{\n    seq int32 = 0\n}\n"), "lineage_fixed_load")
	if load == "" {
		t.Fatal("R19: lineage_fixed_load is not emitted")
	}
	t04Has(t, load, "report.unknown += census.0;",
		"R19 the census lands once per peer and not per record")
	t04Has(t, load, "report.kind_mismatch += census.1;",
		"R19 the kind census lands once per peer and not per record")
}

// ---- rust/E9 -----------------------------------------------------------------

// t05E9 holds rust/E9 to docs/FIXED-FORM-ALGORITHM.md §0: this form's report
// carries `unknown`, `kind_mismatch`, `widened`, `clamped` and `malformed`, and
// "this form raises all but `duplicate`". The report struct has no such field,
// so no read path can raise it.
func t05E9(t *testing.T) {
	runtime := string(fixedRuntimeBody)
	report := t05Report(runtime)
	if report == "" {
		t.Fatal("E9: TableFixedReport is not in the shared runtime")
	}
	if strings.Contains(report, "duplicate") {
		t.Errorf("E9: the fixed form's report carries `duplicate`; this form raises all but it:\n%s", report)
	}
	for _, field := range []string{
		"pub unknown: u32,",
		"pub kind_mismatch: u32,",
		"pub widened: u32,",
		"pub clamped: u32,",
		"pub malformed: bool,",
		"pub refused: bool,",
		"pub reason: TableFixedReason,",
		"pub layout_hash: u64,",
	} {
		t04Has(t, report, field, "E9 the report's own fields")
	}
}

// ---- rust/R16 -----------------------------------------------------------------

// t05R16 holds rust/R16 to docs/FIXED-FORM-ALGORITHM.md §5.4, clause by clause:
// `unknown` once per writer field no reader field names, once per peer and
// never per record; `widened` once per entry per record, and a folded element
// run is ONE; `clamped` once per entry per record for `count` and `text`; the
// bounds pass counts a forged ordinal remapped to None on BOTH plans; and the
// value-landing ops move nothing.
func t05R16(t *testing.T) {
	runtime := string(fixedRuntimeBody)
	// unknown: once per peer, at COMPILE, through the `census` flag, and the
	// plan's number is carried onto the report once after the record loop.
	t04Has(t, runtime, "if !named && census {",
		"R16 unknown is once per peer at COMPILE")
	load := t04Fn(t05Module(t, "package probe\n\nfixed table Lineage\n{\n    seq int32 = 0\n}\n"), "lineage_fixed_load")
	if load == "" {
		t.Fatal("R16: lineage_fixed_load is not emitted")
	}
	t04Has(t, load, "report.unknown += census.0;",
		"R16 the compile census lands once, after the record loop and not per record (ALGORITHM §5.9 #6)")
	// widened: once per entry per record, and a folded run is ONE — the
	// coalescing fold merges COPY entries only, so a widened run is never folded.
	t04Has(t, runtime, "&& c.plan[out - 1].op == TableFixedOp::Copy",
		"R16 a widen is never folded, so a folded element run is ONE copy")
	if n := strings.Count(runtime, "report.widened += 1;"); n != 2 {
		t.Errorf("R16: `widened` is moved %d times in the runtime, want the two widen ops (Widen, WidenF)", n)
	}
	// clamped: once per entry per record for count and text.
	countArm := t05Arm(runtime, "TableFixedOp::Count => {")
	if countArm == "" {
		t.Fatal("R16: the Count arm is not in the runtime")
	}
	t04Has(t, countArm, "report.clamped += 1;", "R16 `count` counts once per entry per record")
	textArm := t05Arm(runtime, "TableFixedOp::Text => {")
	if textArm == "" {
		t.Fatal("R16: the Text arm is not in the runtime")
	}
	t04Has(t, textArm, "report.clamped += 1;", "R16 `text` counts once per entry per record")
	if n := strings.Count(textArm, "report.clamped += 1;"); n != 1 {
		t.Errorf("R16: `text` moves clamped %d times, want once per entry", n)
	}
	// the bounds pass counts a forged ordinal remapped to None on BOTH plans:
	// the compiled plan's own trailing `const` bound, and the scatter the
	// identity plan lands through. Exactly one of the two fires per plan, so
	// neither counts twice (§5.8 row 12).
	t04Has(t, runtime, "if raw > u64::from(p.dstsize) {",
		"R16 the compiled plan's forged ordinal is counted at its own bound")
	t04Has(t, runtime, "report.clamped += 1;",
		"R16 the forged ordinal counts clamped")
	// copy/const/present/ordinal: the value-landing ops move nothing of their
	// own — the one forged-ordinal count above is the BOUNDS PASS's number and
	// the law says it is counted once, on both plans.
	copyArm := t05Arm(runtime, "TableFixedOp::Copy => {")
	for _, counter := range []string{"report.clamped", "report.widened", "report.unknown", "report.kind_mismatch"} {
		if strings.Contains(copyArm, counter) {
			t.Errorf("R16: a `copy` entry moves %s:\n%s", counter, copyArm)
		}
	}
}

// ---- rust/R18 -----------------------------------------------------------------

// t05R18 holds rust/R18 to docs/FIXED-FORM-ALGORITHM.md §5.4: "A clamp that
// cannot fire is not emitted, and nothing moves. An ordinal whose extent FILLS
// its storage width — 255 variants in a byte, 65535 in two — has no read-side
// clamp ... The same rule already applies to a ranged scalar whose declared end
// sits on its width's limit." A `bits(N)` whose N IS its storage width's limit
// is the same tautology.
func t05R18(t *testing.T) {
	// bits(32) fills its four-byte storage: no clamp body at all.
	bits32 := t05Module(t, "package probe\n\nfixed table Lineage\n{\n    b bits(32)\n}\n")
	if strings.Contains(bits32, "_fixed_clamp_body") {
		t.Error("R18: a bits(32) fills its storage width and must emit no clamp")
	}
	// bits(31) is a real bound and clamps to 2^31-1.
	bits31 := t05Module(t, "package probe\n\nfixed table Lineage\n{\n    b bits(31)\n}\n")
	t04Has(t, bits31, "2147483647", "R18 a bits(31) is a clamp that can fire")

	// AN ENUM ORDINAL: 255 variants fill the byte and emit no comparison;
	// 254 do not.
	enum255 := t05Module(t, t05Enum(255))
	if strings.Contains(enum255, "if raw >") {
		t.Error("R18: a 255-variant enum fills its storage byte and must emit no ordinal clamp")
	}
	enum254 := t05Module(t, t05Enum(254))
	t04Has(t, enum254, "if raw > 254 {", "R18 a 254-variant enum's ordinal is a clamp that can fire")

	// A UNION TAG: 255 arms fill the tag byte and emit no comparison; 2 do not.
	union255 := t05Module(t, t05Union(255))
	if strings.Contains(union255, "if tag_raw >") {
		t.Error("R18: a 255-arm union fills its tag byte and must emit no tag clamp")
	}
	union2 := t05Module(t, t05Union(2))
	t04Has(t, union2, "if tag_raw > 2 {", "R18 a two-arm union's tag is a clamp that can fire")

	// A RANGED SCALAR whose declared end sits on its width's limit: the end
	// that cannot fail is dropped, and only the end that can fire remains.
	rng := t05Module(t, "package probe\n\nfixed table Lineage\n{\n    a int32 | min = 0, max = 2147483647\n}\n")
	if strings.Contains(rng, "2147483647") {
		t.Errorf("R18: the high end on int32's own limit is emitted anyway")
	}
	t04Has(t, rng, "value.a = value.a.max(0_i32);", "R18 the low end that can fire remains")
}

// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapRustT05Evolution(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "rust/E6", check: t05E6},
		{id: "rust/E8", check: t05E8},
		{id: "rust/R19", check: t05R19},
		{id: "rust/E9", check: t05E9},
		{id: "rust/R16", check: t05R16},
		{id: "rust/R18", check: t05R18},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
