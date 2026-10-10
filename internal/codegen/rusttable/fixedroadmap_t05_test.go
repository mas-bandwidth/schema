package rusttable

// THE ROADMAP'S FIELD-EVOLUTION, REPORT AND TEXT TASKS ON THE RUST LEG
// (docs/roadmap.sexp nodes `field-evolution/rust`, `reports/rust` and
// `text/rust`; docs/FIXED-FORM-ALGORITHM.md §4.5, §4.6, §5.4 and §5.8;
// docs/FIXED-FORM-VERSIONING-TESTS.md "The rows" and "The divergence rows";
// docs/SPEC-TABLES.md §3.4 and §5). One subtest per task id, table-driven,
// t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE BYTES THIS LEG EMITS and the shared runtime
// body it emits them with, so the harness is reachable on a tree that never
// built the C++ reference corpus and never carries a sibling serialize.rs. A
// clause that needs the emitted code to RUN is asserted at the emitted site,
// the shape fixedroadmap_t04_test.go states for the same reason.
//
// rust/E6 [verify]: a field's wire identity is fnv1a64 of the name its `was`
// declares, and the layout and the pairer both key on that id.
//
// rust/E8 [verify]: an append lands the reader's declared default through the
// prefill and a deprecation is not a version — it moves no layout byte.
//
// rust/R19 [verify]: a clean NEW-READS-OLD of a known field moves no counter;
// the census is the COMPILE's and lands once after the loop.
//
// rust/E9 [owed]: the fixed form raises every §4 counter but `duplicate`.
//
// rust/R16 [weak]: §5.4's counters exactly, per op and per condition.
//
// rust/R18 [owed]: a clamp that cannot fire is not emitted and nothing moves.
//
// rust/C3 [verify]: the text length clamp, in units, at the writer's span.
//
// rust/C4 [owed]: ill-formed text refuses BY NAME (`text_ill_formed`).
//
// rust/R24 [owed]: the content rule runs over the USED units; text is counted
// in bytes with the clamp in units; the slack is unspecified and not a refusal.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- fixtures and readers of this leg's output ------------------------------

// t05Fixed is the emitted fixed module of a unit (the one *_fixed.rs the table
// backend produced).
func t05Fixed(t *testing.T, tables map[string][]byte) string {
	t.Helper()
	for name, data := range tables {
		if strings.HasSuffix(name, "_fixed.rs") {
			return string(data)
		}
	}
	t.Fatalf("no *_fixed.rs module was emitted")
	return ""
}

// t05Fn is the emitted body of one `pub fn NAME(...)`, up to its column-zero
// closing brace.
func t05Fn(text, name string) string {
	at := strings.Index(text, "pub fn "+name+"(")
	if at < 0 {
		return ""
	}
	rest := text[at:]
	if end := strings.Index(rest, "\n}\n"); end >= 0 {
		return rest[:end]
	}
	return rest
}

// t05Has is a required substring of the emitted source, named by rule.
func t05Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// t05Between is the slice of s from `start` to the next `end`, or from `start`
// to the end when `end` does not follow it. It is how one match arm is read
// apart from its neighbours.
func t05Between(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	rest := s[i+len(start):]
	if j := strings.Index(rest, end); j >= 0 {
		return rest[:j]
	}
	return rest
}

// t05Generate is the no-lineage generation of a unit.
func t05Generate(t *testing.T, u *ir.Unit) map[string][]byte {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return files
}

// t05Lineage builds the versioning pair of one row and answers the emitted
// module: the reader is `newSrc`, handed `oldSrc`'s locked entry as its oldest
// lineage entry, exactly as fixedroadmap_t04_test.go's harness does.
func t05Lineage(t *testing.T, oldSrc, newSrc string) (text string, own *ir.Unit) {
	t.Helper()
	older := unitFrom(t, oldSrc)
	own = unitFrom(t, newSrc)
	lineage := map[string][]FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(older) {
		e, ok := FixedLineageOf(older, st.Name)
		if !ok {
			t.Fatalf("no lineage entry for %s", st.Name)
		}
		lineage[st.Name] = append(lineage[st.Name], e)
	}
	files, err := GenerateLineage(own, lineage)
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	return t05Fixed(t, files), own
}

// t05StructFields names the `pub name: type,` members of one emitted struct.
func t05StructFields(src, header string) []string {
	at := strings.Index(src, header)
	if at < 0 {
		return nil
	}
	rest := src[at+len(header):]
	if end := strings.Index(rest, "\n}"); end >= 0 {
		rest = rest[:end]
	}
	var out []string
	for _, line := range strings.Split(rest, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "pub ") {
			continue
		}
		line = strings.TrimPrefix(line, "pub ")
		if i := strings.Index(line, ":"); i > 0 {
			out = append(out, strings.TrimSpace(line[:i]))
		}
	}
	return out
}

// t05LE64 is a name's wire id as the eight little-endian bytes a layout entry
// carries (docs/FIXED-FORM-ALGORITHM.md §5.2).
func t05LE64(id uint64) []byte {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], id)
	return b[:]
}

// ---- rust/E6 -----------------------------------------------------------------

// t05E6 holds rust/E6 to docs/SPEC-TABLES.md §5: "`was = "old_name"` keeps a
// field's identity through a rename: the wire id stays the hash of the old
// name. A rename under `was` is therefore not a change to a fixed table at
// all". The id is `ir.TableFieldWireName` -> `ir.TableWireId`; the layout
// carries it, the NEW name's hash rides nowhere, and the plan compiler pairs
// children BY ID and never by position or by name (§5.9 #41).
func t05E6(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table Lineage
{
    renamed_to int32 = 0 | was = "renamed"
    keep int32
}
`)
	st := u.Tables["Lineage"]
	if st == nil {
		t.Fatal("E6: the fixture declares no Lineage table")
	}
	var renamed *ir.Field
	for _, f := range st.Fields {
		if f.Name == "renamed_to" {
			renamed = f
		}
	}
	if renamed == nil {
		t.Fatal("E6: the fixture has no renamed_to field")
	}
	if got := ir.TableFieldWireName(renamed); got != "renamed" {
		t.Errorf("E6: the declared identity is %q, want the `was` alias %q", got, "renamed")
	}
	oldID := ir.TableWireId("renamed")
	newID := ir.TableWireId("renamed_to")
	if oldID == newID {
		t.Fatalf("E6: the fixture's two names hash alike (%#x); the row proves nothing", oldID)
	}
	if got := ir.TableFieldWireId(renamed); got != oldID {
		t.Errorf("E6: the field's wire id is %#x, want fnv1a64(%q) = %#x", got, "renamed", oldID)
	}
	// THE LAYOUT CARRIES THE DECLARED IDENTITY and the NEW name's hash nowhere.
	block := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
	if !bytes.Contains(block, t05LE64(oldID)) {
		t.Errorf("E6: no layout entry carries fnv1a64(%q) = %#x — the declared identity is off the wire", "renamed", oldID)
	}
	if bytes.Contains(block, t05LE64(newID)) {
		t.Errorf("E6: a layout entry carries fnv1a64(%q) = %#x — the NEW name rides the wire", "renamed_to", newID)
	}
	// AND THE PAIRING IS BY ID: the writer's child is matched to the reader's by
	// `id`, and the census walks the reader's ids the same way.
	runtime := string(fixedRuntimeBody)
	t05Has(t, runtime, "if tc.id == mc.id {", "E6 the plan compiler pairs a writer child by wire id")
	t05Has(t, runtime, "if mine.entry(mc_at).id == tc.id {", "E6 the census pairs by wire id")
	// AND THE EMITTED BLOCK is the same bytes ir walks.
	text := t05Fixed(t, t05Generate(t, u))
	t05Has(t, text, "LINEAGE_FIXED_BLOCK: [u8;", "E6 the layout block is emitted")
}

// ---- rust/E8 -----------------------------------------------------------------

// t05E8 holds rust/E8 to the backward-read contract: an APPENDED field is a
// field the plan does not write, so the PREFILL lands its declared default
// (§4.3), and a DEPRECATED field is not a version at all — the lock records it
// and the wire does not move (bill §12.3: "a deprecation is not newer: the OLD
// reader READS the new file"). The census is per writer field the reader cannot
// name, never for the reader's own add.
func t05E8(t *testing.T) {
	// AN APPEND: the reader's own third field is a hole, and its declared
	// default rides the prefill image.
	text, own := t05Lineage(t,
		"package probe\n\nfixed table Lineage\n{\n    a int32 = 1\n    b int32 = 2\n}\n",
		"package probe\n\nfixed table Lineage\n{\n    a int32 = 1\n    b int32 = 2\n    c int32 = 7\n}\n")
	if own.Tables["Lineage"] == nil {
		t.Fatal("E8: the appended fixture declares no Lineage table")
	}
	if got := len(ir.TableFixedWalkRoot(own.Tables["Lineage"])); got != 4 {
		t.Errorf("E8: the appended walk has %d entries, want 4 (root + a + b + c)", got)
	}
	t05Has(t, text, "image[o..o + z].copy_from_slice(&LINEAGE_FIXED_DEFAULTS[o..o + z]);",
		"E8 an added field lands the declared default through the prefill")
	// The hole list is what the plan does NOT write, and the census is the
	// writer's fields the reader cannot name.
	t05Has(t, text, "table_fixed_holes(entries, &mut cover, &mut hole_buf);", "E8 the prefill is the unwritten ranges")
	t05Has(t, string(fixedRuntimeBody), "if !named && census {", "E8 unknown is one per writer field the reader cannot name")

	// A DEPRECATION: the marker moves no layout byte and no digest byte, so the
	// two layouts are ONE identity and the deprecated field still rides.
	plain := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    a int32 = 0\n    b int32 = 0\n}\n")
	dep := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    a int32 = 0\n    b int32 = 0 | deprecated\n}\n")
	lp := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(plain.Tables["Lineage"]))
	ld := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(dep.Tables["Lineage"]))
	if !bytes.Equal(lp, ld) {
		t.Error("E8: a deprecation moved a layout byte; bill §12.3 makes it not a version")
	}
	if ir.TableFixedLayoutHash(lp, plain.Tables["Lineage"]) != ir.TableFixedLayoutHash(ld, dep.Tables["Lineage"]) {
		t.Error("E8: a deprecation moved the layout hash")
	}
	if got := len(ir.TableFixedWalkRoot(dep.Tables["Lineage"])); got != 3 {
		t.Errorf("E8: the deprecated field is off the walk: %d entries, want 3", got)
	}
}

// ---- rust/R19 -----------------------------------------------------------------

// t05R19 holds rust/R19 to §5.4's last row: "a clean NEW-READS-OLD of an
// appended field, variant, arm, flag or keyed slot — none: every counter stays
// at zero: an append the reader knows is not an event." The ops that LAND a
// known field (copy, the guarded const, the clean ordinal remap) move no
// counter; only a widen, a clamp and the bounds pass do.
func t05R19(t *testing.T) {
	runtime := string(fixedRuntimeBody)
	run := t05Fn(runtime, "table_fixed_run")
	if run == "" {
		t.Fatal("R19: table_fixed_run is not emitted")
	}
	copyArm := t05Between(run, "TableFixedOp::Copy => {", "TableFixedOp::Count => {")
	if copyArm == "" {
		t.Fatal("R19: the Copy arm is not emitted")
	}
	for _, counter := range []string{"report.unknown", "report.kind_mismatch", "report.widened", "report.clamped"} {
		if strings.Contains(copyArm, counter) {
			t.Errorf("R19: Copy moves %s; a known field is not an event", counter)
		}
	}
	// THE COMPILE CENSUS IS THE ONLY unknown, AND IT LANDS ONCE, AFTER THE LOOP,
	// ON A READ THAT RETURNS (§5.9 #6) — never inside the record loop.
	if strings.Contains(run, "report.unknown") {
		t.Error("R19: the record loop raises unknown per record")
	}
	text, _ := t05Lineage(t,
		"package probe\n\nfixed table Lineage\n{\n    a int32 = 1\n    b int32 = 2\n}\n",
		"package probe\n\nfixed table Lineage\n{\n    a int32 = 1\n    b int32 = 2\n    c int32 = 7\n}\n")
	load := t05Fn(text, "lineage_fixed_load")
	if load == "" {
		t.Fatal("R19: lineage_fixed_load is not emitted")
	}
	if n := strings.Count(load, "report.unknown += census.0;"); n != 1 {
		t.Errorf("R19: the compile census lands %d times, want once", n)
	}
	if n := strings.Count(load, "report.kind_mismatch += census.1;"); n != 1 {
		t.Errorf("R19: the kind census lands %d times, want once", n)
	}
}

// ---- rust/E9 -----------------------------------------------------------------

// t05E9 holds rust/E9 to docs/FIXED-FORM-ALGORITHM.md:26: "The §4 counters are
// unknown, kind_mismatch, widened, clamped, duplicate and malformed; this form
// raises all but duplicate", and to §5.9's "duplicate is the TEXT form's — the
// fixed wire never raises it — so a leg whose report has no such member is not
// missing one." The report carries exactly the counters the wire raises, and
// `duplicate` appears nowhere.
func t05E9(t *testing.T) {
	runtime := string(fixedRuntimeBody)
	if strings.Contains(runtime, "duplicate") {
		t.Error("E9: the fixed runtime names `duplicate`; the fixed wire never raises it")
	}
	fields := t05StructFields(runtime, "pub struct TableFixedReport {")
	want := []string{"unknown", "kind_mismatch", "widened", "clamped", "malformed", "refused", "reason", "layout_hash"}
	if len(fields) != len(want) {
		t.Fatalf("E9: TableFixedReport carries %v, want exactly %v", fields, want)
	}
	for i := range want {
		if fields[i] != want[i] {
			t.Errorf("E9: TableFixedReport member %d is %q, want %q", i, fields[i], want[i])
		}
	}
}

// ---- rust/R16 -----------------------------------------------------------------

// t05R16 holds rust/R16 to §5.4's table, clause by clause: unknown is the
// COMPILE's, once per peer and never per record; widened is once per entry per
// record and a folded run is ONE; clamped is once per entry per record for a
// count and a text; the bounds pass counts a forged ordinal remapped to None on
// BOTH plans; copy, const, present and ordinal move nothing.
func t05R16(t *testing.T) {
	runtime := string(fixedRuntimeBody)
	run := t05Fn(runtime, "table_fixed_run")
	if run == "" {
		t.Fatal("R16: table_fixed_run is not emitted")
	}
	// UNKNOWN AND KIND_MISMATCH ARE THE COMPILE'S, never the loop's.
	if strings.Contains(run, "report.unknown") || strings.Contains(run, "report.kind_mismatch") {
		t.Error("R16: the read loop raises the COMPILE census per record")
	}
	if n := strings.Count(runtime, "report.unknown += 1;"); n != 1 {
		t.Errorf("R16: the compile census raises unknown %d times, want once per writer field", n)
	}
	// WIDENED: once per entry per record, and only Widen and WidenF raise it.
	if n := strings.Count(run, "report.widened += 1;"); n != 2 {
		t.Errorf("R16: widened is raised %d times in the loop, want 2 (Widen, WidenF)", n)
	}
	// A FOLDED ELEMENT RUN IS ONE: only Copy entries coalesce, and a widened run
	// is never folded (§5.9 #33).
	t05Has(t, runtime, "&& c.plan[out - 1].op == TableFixedOp::Copy", "R16 a folded run is Copy-only")
	// CLAMPED: once per entry per record for count and text.
	countArm := t05Between(run, "TableFixedOp::Count => {", "TableFixedOp::Text => {")
	if n := strings.Count(countArm, "report.clamped += 1;"); n != 1 {
		t.Errorf("R16: the count clamp is raised %d times, want once", n)
	}
	textArm := t05Between(run, "TableFixedOp::Text => {", "TableFixedOp::Ordinal => {")
	if n := strings.Count(textArm, "report.clamped += 1;"); n != 1 {
		t.Errorf("R16: the text clamp is raised %d times, want once", n)
	}
	// THE BOUNDS PASS ON THE COMPILED PLAN: an unguarded None const whose tag is
	// past the writer's arm count counts exactly one clamped in the op
	// (schema#1254, §5.8 row 12).
	constArm := t05Between(run, "TableFixedOp::Const => {", "\n            }\n        }\n    }\n}")
	if constArm == "" {
		t.Fatal("R16: the Const arm is not emitted")
	}
	t05Has(t, constArm, "if p.aux == 0 && p.dstsize != 0 && p.guard == TABLE_FIXED_NO_GUARD {",
		"R16 the forged-tag None path")
	if n := strings.Count(constArm, "report.clamped += 1;"); n != 1 {
		t.Errorf("R16: the None-tag bounds pass counts %d times, want exactly one", n)
	}
	// AND ON THE IDENTITY PLAN: the scatter closes the enum's range, once.
	u := unitFrom(t, "package probe\n\nenum Tier { Bronze, Silver, Gold, Platinum }\n\nfixed table Lineage\n{\n    tier Tier\n    seq int32\n}\n")
	scatter := t05Fn(t05Fixed(t, t05Generate(t, u)), "lineage_fixed_scatter")
	if scatter == "" {
		t.Fatal("R16: lineage_fixed_scatter is not emitted")
	}
	t05Has(t, scatter, "if raw > 4 {", "R16 the identity plan closes the enum's range")
	if n := strings.Count(scatter, "report.clamped += 1;"); n != 1 {
		t.Errorf("R16: the identity plan counts %d clamps, want exactly one", n)
	}
	// COPY, CONST, PRESENT AND ORDINAL MOVE NOTHING: the ordinal's counter is the
	// in-range-only None of the bounds pass and not a second count of the remap.
	ordinalArm := t05Between(run, "TableFixedOp::Ordinal => {", "TableFixedOp::Widen => {")
	if n := strings.Count(ordinalArm, "report.clamped += 1;"); n != 1 {
		t.Errorf("R16: the ordinal op counts %d times, want once — and never twice", n)
	}
	t05Has(t, ordinalArm, "Some(count) if raw != 0 && raw <= u64::from(*count) => {",
		"R16 a forged ordinal past the writer's count is the bounds pass's")
}

// ---- rust/R18 -----------------------------------------------------------------

// t05R18 holds rust/R18 to §5.4: "A clamp that cannot fire is not emitted, and
// nothing moves. An ordinal whose extent FILLS its storage width — 255 variants
// in a byte, 65535 in two — has no read-side clamp ... The elided check never
// clamped, so the counter it would have moved never moved either." The same
// rule covers a `bits(N)` that fills its storage and a ranged scalar whose
// declared end sits on its width's limit.
func t05R18(t *testing.T) {
	// 255 VARIANTS IN A BYTE: the extent fills the width, so no `if raw >`
	// comparison is emitted for it.
	var sb strings.Builder
	sb.WriteString("package probe\n\nenum Filled {\n")
	for i := 0; i < 255; i++ {
		fmt.Fprintf(&sb, "    V%d\n", i)
	}
	sb.WriteString("}\n\nfixed table Cap\n{\n    grade Filled\n    seq int32\n}\n")
	filled := unitFrom(t, sb.String())
	if got := filled.Enums["Filled"]; got == nil {
		t.Fatal("R18: the fixture declares no Filled enum")
	} else if got.Max != 255 {
		t.Fatalf("R18: the fixture's enum has Max %d, want 255 (one byte full)", got.Max)
	}
	text := t05Fixed(t, t05Generate(t, filled))
	scatter := t05Fn(text, "cap_fixed_scatter")
	if scatter == "" {
		t.Fatal("R18: cap_fixed_scatter is not emitted")
	}
	if strings.Contains(scatter, "if raw >") {
		t.Error("R18: a clamp that cannot fire is emitted for a byte-full enum")
	}
	if strings.Contains(scatter, "report.clamped") {
		t.Error("R18: the elided enum clamp still moves a counter")
	}
	// A bits(N) THAT FILLS ITS STORAGE: bits(32) at the four-byte storage the
	// fixed form gives a bits(N) has no clamp, so no clamp body is emitted.
	bits := unitFrom(t, "package probe\n\nfixed table Cap\n{\n    bits32 bits(32)\n}\n")
	bitsText := t05Fixed(t, t05Generate(t, bits))
	if strings.Contains(bitsText, "cap_fixed_clamp_body") {
		t.Error("R18: a clamp body is emitted for a bits(32) that fills its storage")
	}
	// A RANGED SCALAR WHOSE END SITS ON ITS WIDTH: min 0 and max 255 at a byte
	// are the type's own limits and no comparison can fail.
	ranged := unitFrom(t, "package probe\n\nfixed table Cap\n{\n    r uint8 | min = 0, max = 255\n}\n")
	rangedText := t05Fixed(t, t05Generate(t, ranged))
	if strings.Contains(rangedText, "cap_fixed_clamp_body") {
		t.Error("R18: a clamp body is emitted for a range that fills its byte")
	}
	if strings.Contains(rangedText, "report.clamped") {
		t.Error("R18: a range that fills its byte still moves a counter")
	}
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
