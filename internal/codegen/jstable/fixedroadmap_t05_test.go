package jstable

// fixedroadmap_t05 — the js leg's field-evolution, reports and text rows of
// docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW Fixed Tables"), the
// assertions no other test of this leg makes. Law: docs/FIXED-FORM-ALGORITHM.md
// §5.4 (the §5.4 counters, exactly), §4.6 (a ranged scalar's clamp, and a clamp
// that cannot fire), docs/SPEC-TABLES.md §3.4 (count and text length clamp, the
// USED units; a rename's declared identity, an append's backward-read
// contract), and the titles in docs/roadmap.sexp. One subtest per task id this
// card proves; a task already held by a test that cannot RUN on this bench is
// proved here instead, by the leg's own writer and reader (§5.9 #18: the leg's
// own toolchain), never by the reference byte oracle the absent sibling
// runtime keeps unbuilt.
//
// NOTHING HERE READS THE C++ REFERENCE'S BYTE ORACLE: every clause is the
// runtime body or the decode projection this leg EMITS, read straight out of the
// generated module, or a record the leg's own writer produced and its own newer
// reader consumed. So the suite runs and BITES on a tree that never built the
// corpus.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestFixedRoadmapJsT05Evolution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"js/E6", t05E6},
		{"js/E8", t05E8},
		{"js/R19", t05R19},
		{"js/E9", t05E9},
		{"js/R16", t05R16},
		{"js/R18", t05R18},
		{"js/C3", t05C3},
		{"js/C4", t05C4},
		{"js/R24", t05R24},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t05Join is the §5.4 counter check every backward read owes: a clean
// NEW-READS-OLD moves NONE of the six events (§5.4, docs/FIXED-FORM-ALGORITHM.md
// §5.5). It is one JS predicate because R19's whole clause is its negation.
const t05CleanRead = `if (r.unknown !== 0 || r.kindMismatch !== 0 || r.clamped !== 0 || r.widened !== 0 || r.duplicate !== 0) {
  fail("§5.4: a clean backward read moved a counter: " + reason(r));
}`

// t05WriteDefault is the leg's OWN writer laying a fresh record with the OLDER
// unit: the value surface has no field set, so every value is the OLDER
// generation's declared default and the row's only content is what the NEWER
// reader does with the bytes. It is §5.9 #18's own-toolchain path, not the
// reference oracle.
const t05WriteDefault = `
const { writeFileSync } = await import("node:fs");
const v = new T(); InitT(v);
const buf = new Uint8Array(TMeasure(1));
if (TSave([v], 1, buf) !== buf.length) { fail("the older writer must save one record"); }
writeFileSync(%q, buf);
`

// t05ReadClean reads a file the older generation wrote, through the newer
// reader's plan: it must decode, must not refuse or malform, and must move no
// counter of §5.4.
const t05ReadClean = `
const data = readFileSync(%q);
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
const report = new TableFixedReport(); const r = report;
const plan = TNewPlan(4096, 4096);
plan.image.fill(0x5A);
const n = TLoad(back, back.length, data, data.length, plan, report);
if (!(n >= 1 && !r.malformed && r.refused === 0)) {
  fail("a clean NEW-READS-OLD is not a refusal: n=" + n + " " + reason(r));
}
%s
`

// t05RoundTrip is the shared body of the oracle-free column: the OLDER unit
// writes one default record, the NEWER unit reads it with the OLDER unit's
// locked entry handed in (or with none, a row whose edit moves no layout byte,
// §5.7), and `check` asserts what landed on `back[0]`. Both probes run the
// leg's own node.
func t05RoundTrip(t *testing.T, olderName, newerName, check string) {
	t.Helper()
	node := jsNode(t)
	older := jsReadSchema(t, olderName)
	newer := jsReadSchema(t, newerName)
	table := jsFixedRootName(t, older)
	oldUnit := jsUnitOf(t, older)
	newUnit := jsUnitOf(t, newer)
	olderEntry, ok := FixedLineageOf(oldUnit, table)
	if !ok {
		t.Fatalf("no lineage entry for %s", table)
	}
	own, ok := FixedLineageOf(newUnit, table)
	if !ok {
		t.Fatalf("no lineage entry for %s", table)
	}
	file := filepath.Join(t.TempDir(), "old.bin")
	if out, err := jsRunVersionProbe(t, node, older, nil, 0, table,
		fmt.Sprintf(t05WriteDefault, file)); err != nil {
		t.Fatalf("older writer, %s: %v\n%s", olderName, err, out)
	}
	// A row whose edit moves no layout byte has one hash on both sides (§5.7):
	// its file reads through the identity plan and owes no lineage entry. A row
	// whose edit moves the hash is a new version: the newer reader gets the
	// older entry as its lock.
	var lineage []string
	if olderEntry.Wire != own.Wire {
		lineage = []string{older}
	}
	// EVERY backward read owes the §5.4 clean-read predicate, so the round trip
	// emits it unconditionally: `check` is the row's own value clause on top
	// (empty for a lane whose whole clause IS the counter census, R19).
	read := fmt.Sprintf(t05ReadClean, file, t05CleanRead+check)
	if out, err := jsRunVersionProbe(t, node, newer, lineage, 0, table, read); err != nil {
		t.Fatalf("newer reader, %s: %v\n%s", newerName, err, out)
	}
}

// t05E6: E6 [verify] "Renaming uses the declared identity". A field renamed
// through `was = "a"` keeps the OLD name's wire identity (docs/SPEC-TABLES.md
// §2.10, §3.4): the layout is byte-identical, the hash is one hash on both
// sides, and the pair reads in both directions with no counter (§5.7). The
// proving assertion is the OLDER writer's file read on the NEWER build — the
// renamed field's NEW spelling lands the OLD name's bytes.
func t05E6(t *testing.T) {
	t05RoundTrip(t, "VOLD_rename_without_was", "VNEW_rename_without_was", `
if (back[0].b !== 0 || back[0].seq !== 0) {
  fail("E6: the renamed field did not read the old name's bytes: " + show(back[0]));
}
`)
}

// t05E8: E8 [verify] "Append and deprecate under the backward-read contract".
// docs/FIXED-FORM-VERSIONING-TESTS.md's LISTS rows: an APPEND is a new hash
// whose appended field takes ITS DECLARED DEFAULT on a backward read
// (`field_append`: `w` = 77), and a DEPRECATION is not newer — the slot stays
// where it is, the OLD reader reads the new file, and no counter moves
// (`field_deprecate`: one hash, both directions).
func t05E8(t *testing.T) {
	t.Run("append", func(t *testing.T) {
		t.Parallel()
		t05RoundTrip(t, "VOLD_field_append", "VNEW_field_append", `
if (back[0].x !== 0 || back[0].y !== 0 || back[0].z !== 0) {
  fail("E8: the old writer's values did not land: " + show(back[0]));
}
if (back[0].w !== 77) {
  fail("E8: the appended field is not its declared default: " + show(back[0]));
}
`)
	})
	t.Run("deprecate", func(t *testing.T) {
		t.Parallel()
		t05RoundTrip(t, "VOLD_field_deprecate", "VNEW_field_deprecate", `
if (back[0].a !== 0 || back[0].b !== 0 || back[0].c !== 0) {
  fail("E8: a deprecated slot moved a neighbour or the field: " + show(back[0]));
}
`)
	})
}

// t05R19: R19 [verify] "a clean NEW-READS-OLD of an appended field, variant,
// arm, flag or keyed slot moves no counter at all" (docs/FIXED-FORM-ALGORITHM.md
// §5.4, §5.5; the append rows of docs/FIXED-FORM-VERSIONING-TESTS.md). Every
// append row is one lane: the appended field (`field_append`), the appended
// variant (`enum_append`), the appended arm (`union_append`), the appended flag
// (`flags_append`) and the appended keyed slot (`keyed_array_enum_append`). The
// assertion is the same six-counter zero on every lane, red the moment any of
// them counts.
func t05R19(t *testing.T) {
	rows := []struct{ name string }{
		{"field_append"},
		{"enum_append"},
		{"union_append"},
		{"flags_append"},
		{"keyed_array_enum_append"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			t05RoundTrip(t, "VOLD_"+row.name, "VNEW_"+row.name, "")
		})
	}
}

// t05E9: E9 [verify] "duplicate never raised" (docs/FIXED-FORM-ALGORITHM.md
// §5.4: `duplicate` is a counter this form defines and never fires — the fixed
// form's plan has one entry per field, so two entries can never claim one
// destination byte and the duplicate event does not exist here). The emitted
// report declares `duplicate`, resets it to zero on both the constructor and the
// reset, and NO site in the whole runtime ever increments it.
func t05E9(t *testing.T) {
	_, src := t02Table(t, t02Flat)
	if n := strings.Count(src, "duplicate++"); n != 0 {
		t.Errorf("E9: the runtime raises `duplicate` %d time(s); the fixed form never raises it (§5.4)", n)
	}
	if n := strings.Count(src, "this.duplicate = 0;"); n != 1 {
		t.Errorf("E9: the fresh report declares `duplicate` %d time(s), want once", n)
	}
	if n := strings.Count(src, "report.duplicate = 0;"); n != 1 {
		t.Errorf("E9: the reset clears `duplicate` %d time(s), want once", n)
	}
}

// t05R16: R16 [weak] — the §5.4 counters a plan moves, EXACTLY, and the ones it
// does not. Every clause of the title is one lane of the emitted runtime
// (docs/FIXED-FORM-ALGORITHM.md §5.4):
//
//   - widened once per entry per record: the widen op moves it, once, and no
//     fold across a widen run;
//   - clamped once per entry per record for a count and for a text length;
//   - a forged ordinal remapped to None counts on BOTH plans: the compiled
//     plan's ordinal op and the identity projection each count it once, and the
//     same forged bytes land the same `clamped == 1` on either;
//   - copy and const move nothing on the clean path.
func t05R16(t *testing.T) {
	_, src := t02Table(t, `package probe

enum Tier { Bronze, Silver, Gold }

fixed table T
{
    rank Tier
}
`)
	run := jsFn(src, "TableFixedRun")
	if run == "" {
		t.Fatal("R16: the emitted runtime has no TableFixedRun")
	}

	// widened once per entry per record: the widen op and the float-widen op
	// each move it once, and nowhere else.
	if n := strings.Count(run, "report.widened++;"); n != 2 {
		t.Errorf("R16: report.widened++ occurs %d time(s) in the run loop, want the two widen ops once each", n)
	}
	// The per-ENTRY number, and the fold that never crosses a widen (§5.4 line
	// 958, §5.9 #33): the array op sends min(their_n, my_n) element entries, and
	// the ONE coalescer folds only COPY entries, so a widened run is one entry
	// per element and never one folded entry.
	compile := jsFn(src, "TableFixedCompileEntry")
	if !strings.Contains(t02Norm(compile), "const n = theirN < myN ? theirN : myN;") {
		t.Error("R16: the array op does not send min(their_n, my_n) element entries (§5.4, §5.9 #33)")
	}
	coal := jsFn(src, "TableFixedCoalesce")
	if n := strings.Count(coal, "TableFixedOpCopy"); n != 2 {
		t.Errorf("R16: the coalescer names op COPY %d time(s), want the two fold sides and no other op", n)
	}
	if strings.Contains(coal, "TableFixedOpWiden") {
		t.Error("R16: the coalescer folds a widen run; §5.9 #33 forbids a fold across a widen")
	}

	// `unknown` once per endpoint at COMPILE and never per record (§5.4 line 956):
	// the MATCH carries the one census site, and the record loop carries none.
	match := jsFn(src, "TableFixedMatchChildren")
	if n := strings.Count(match, "report.unknown++"); n != 1 {
		t.Errorf("R16: the MATCH at COMPILE carries report.unknown++ %d time(s), want the one site that runs once per unnamed writer's field", n)
	}
	if n := strings.Count(run, "report.unknown++"); n != 0 {
		t.Errorf("R16: the record loop moves `unknown` %d time(s); §5.4 counts it once per endpoint and never per record", n)
	}

	// clamped once per entry, per record: a count clamp and a text clamp each
	// count once, at the one site that reads the number.
	if !strings.Contains(t02Norm(run), "else if (v > size) { v = size; report.clamped++; }") {
		t.Error("R16: the count op does not clamp once to the writer's bound the plan carries")
	}
	if !strings.Contains(t02Norm(run), "const cap = size | 0;") ||
		!strings.Contains(t02Norm(run), "else if (v > cap) { v = cap; report.clamped++; }") {
		t.Error("R16: the text op does not clamp its length once to the byte span (§3.4)")
	}

	// A forged ordinal remapped to None counts on BOTH plans: the compiled
	// plan's ordinal op counts it, and the identity projection counts it too.
	if !strings.Contains(t02Norm(run), "if (raw > arms) { report.clamped++; }") {
		t.Error("R16: the compiled plan does not count a forged ordinal remapped to None")
	}
	if !strings.Contains(t02Norm(jsFn(src, "TFixedDecode")), "if (value.rank > 3) { value.rank = 0; report.clamped++; }") {
		t.Error("R16: the identity projection does not count a forged ordinal remapped to None")
	}

	// copy moves nothing on the clean path: its case body names no counter.
	copyCase := t02Block(t, run, "case TableFixedOpCopy: {", "break;")
	if strings.Contains(copyCase, "report.") {
		t.Error("R16: the COPY op moves a counter; a clean copy moves nothing")
	}
	// present moves nothing: the optional wrapper's present byte is a COPY at
	// the wrapper's own lane (§5.4 line 961), and the copy body above is the one
	// that moves nothing.
	if !strings.Contains(t02Norm(compile), "TableFixedPush(plan, TableFixedOpCopy, theirAt, auxAt, 1, 0, guard, arg, 0);") {
		t.Error("R16: the optional wrapper's present byte is not a COPY that moves nothing (§5.4)")
	}
	// const moves nothing except the forged-ordinal guard it carries.
	constCase := t02Block(t, run, "case TableFixedOpConst: {", "break;")
	if n := strings.Count(constCase, "report.clamped++"); n != 1 {
		t.Errorf("R16: the CONST op counts %d time(s), want only the forged-ordinal guard", n)
	}
	if n := strings.Count(constCase, "report.widened++") + strings.Count(constCase, "report.unknown++"); n != 0 {
		t.Error("R16: the CONST op moves widened or unknown; a const moves neither")
	}
	// ordinal lands its value and moves nothing: the remap to None is the bounds
	// pass's to count (§5.4 line 961), and the single counter in the case body IS
	// that guard — no second count, and no other op counter.
	ordinalCase := t02Block(t, run, "case TableFixedOpOrdinal: {", "break;")
	if n := strings.Count(ordinalCase, "report.clamped++"); n != 1 {
		t.Errorf("R16: the ORDINAL op carries %d clamped sites, want only the bounds pass's forged guard", n)
	}
	if n := strings.Count(ordinalCase, "report.widened") + strings.Count(ordinalCase, "report.unknown") +
		strings.Count(ordinalCase, "report.kindMismatch") + strings.Count(ordinalCase, "report.duplicate"); n != 0 {
		t.Error("R16: the ORDINAL op moves a counter beyond the bounds pass; the op lands its value and moves nothing (§5.4)")
	}
}

// t05R18: R18 [owed] "a clamp that cannot fire is not emitted and nothing moves"
// (docs/FIXED-FORM-ALGORITHM.md §4.6; fixedClampEnds answers which ends a
// DataView load can actually lose). A bound sitting on the storage width's own
// limit is a comparison no loaded value can fail, so no clamp statement is
// emitted for it and no `clamped` is ever owed; a bound INSIDE the storage
// width is emitted, on the end that can fire.
func t05R18(t *testing.T) {
	// Vacuous: min = 0 and max = the uint32 ceiling are the storage width's own
	// ends, so neither comparison can be lost.
	_, vacuous := t02Table(t, `package probe

fixed table T
{
    x uint32 | min = 0, max = 4294967295
}
`)
	if decode := jsFn(vacuous, "TFixedDecode"); strings.Contains(decode, "report.clamped") {
		t.Errorf("R18: a clamp that cannot fire was emitted:\n%s", decode)
	}

	// Not vacuous: min = 1 is inside the width, so the low clamp IS emitted.
	_, live := t02Table(t, `package probe

fixed table T
{
    x uint32 = 1 | min = 1, max = 2
}
`)
	if decode := jsFn(live, "TFixedDecode"); !strings.Contains(decode, "report.clamped") {
		t.Errorf("R18: a clamp that CAN fire was elided:\n%s", decode)
	}
}

// t05C3: C3 [verify] "text length clamp" (docs/SPEC-TABLES.md §3.4: a `string(N)`
// or `bytes(N)` field rides a used length then that many units; the reader
// clamps the length to ITS OWN declared capacity, counting one `clamped`
// (docs/FIXED-FORM-ALGORITHM.md §5.4)). The emitted decode reads the length once
// and clamps it at both ends against the reader's own `N`.
func t05C3(t *testing.T) {
	_, src := t02Table(t, `package probe

fixed table T
{
    label string(8)
}
`)
	decode := jsFn(src, "TFixedDecode")
	want := "if (n < 0) { n = 0; report.clamped++; } else if (n > 8) { n = 8; report.clamped++; }"
	if !strings.Contains(t02Norm(decode), want) {
		t.Errorf("C3: the text length is not clamped once at both ends to the reader's own 8:\n%s", decode)
	}
}

// t05C4: C4 [owed] "text content refuses BY NAME" (docs/FIXED-FORM-ALGORITHM.md
// §4.5, the `text` op: "**A content violation REFUSES BY NAME, the verdict the
// packet reader gives**, this form having no `L` to continue past (fix 11)").
// The emitted runtime names the refusal `text_ill_formed`; the compiled plan's
// text op refuses by name on a content violation; and the identity projection
// of a byte string does too. The check holds the USED units.
func t05C4(t *testing.T) {
	_, src := t02Table(t, `package probe

fixed table T
{
    label string(8)
}
`)
	t02Has(t, src, "TextIllFormed", "C4 the named content refusal exists")
	t02Has(t, t02Norm(src),
		`case TableFixedRefusal.TextIllFormed: return "text_ill_formed";`,
		"C4 the refusal is named text_ill_formed")
	t02Has(t, t02Norm(src),
		"if (!TableFixedTextOk(TableFixedTextUtf8, srcView, s + 4, v)) { report.refused = TableFixedRefusal.TextIllFormed; return; }",
		"C4 the compiled text op refuses by name on a content violation")
	// UTF-8 that is over-range, a surrogate, truncated or overlong is damage and
	// refuses; the packet reader refuses the same bytes.
	t02Has(t, t02Norm(src),
		"if (cp < min || cp > 0x10FFFF || (cp >= 0xD800 && cp <= 0xDFFF)) { return false; }",
		"C4 over-range UTF-8 refuses by name")
	// A zero byte among the used bytes is damage too.
	t02Has(t, t02Norm(src), "if (b0 === 0) { return false; }", "C4 a zero byte among the used units refuses")
	// THE IDENTITY PROJECTION refuses by name as well: the body is one COPY, so
	// no text op ran, and the decode is where the byte string's content is held.
	t02Has(t, t02Norm(src),
		"if (!TableFixedTextOk(TableFixedTextUtf8, view, at + 4, n)) { report.refused = TableFixedRefusal.TextIllFormed; return; }",
		"C4 the byte string refuses by name in the projection")
}

// t05R24: R24 [owed] "ill-formed text in the USED units refuses by name
// (text_ill_formed); text is counted in bytes with the clamp in units; slack is
// unspecified on read and not a refusal" (docs/FIXED-FORM-ALGORITHM.md §4.5 fix
// 11; docs/SPEC-TABLES.md §3.4). Text is UTF-8, so its unit is the byte. All
// three clauses are sites of the emitted runtime: the content refusal by name,
// the clamp to the byte span (`cap = size`), and the check over the used
// bytes (`v`) and never the slack (`size`).
func t05R24(t *testing.T) {
	_, src := t02Table(t, `package probe

fixed table T
{
    label string(8)
}
`)
	t02Has(t, t02Norm(src), "report.refused = TableFixedRefusal.TextIllFormed", "R24 ill-formed text refuses by name")
	t02Has(t, t02Norm(src), "cp > 0x10FFFF", "R24 ill-formed text past U+10FFFF is a refusal, not a default")
	// THE CLAMP IS THE BYTE SPAN: the declared bound `size` is the cap the used
	// length is clamped to.
	t02Has(t, t02Norm(src), "const cap = size | 0;", "R24 the clamp is the byte span")
	t02Has(t, t02Norm(src), "else if (v > cap) { v = cap; report.clamped++; }", "R24 the length clamps once")
	// THE CONTENT CHECK HOLDS THE USED BYTES (v) AND NOT THE SLACK (size): slack
	// is unspecified on read and is not a refusal.
	t02Has(t, t02Norm(src), "srcView, s + 4, v)) { report.refused = TableFixedRefusal.TextIllFormed", "R24 the content check holds the used units and not the slack")
}
