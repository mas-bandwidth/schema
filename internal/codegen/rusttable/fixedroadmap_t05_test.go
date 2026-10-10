package rusttable

// THE ROADMAP'S FIELD-EVOLUTION, REPORT, AND TEXT TASKS ON THE RUST LEG
// (docs/roadmap.sexp node `fixed-tables`; docs/FIXED-FORM-ALGORITHM.md §4.5,
// §5.2 and §5.4; docs/FIXED-FORM-VERSIONING-TESTS.md "The rows" and
// "The divergence rows"; docs/SPEC-TABLES.md §3.4 and its SLACK RULE).
// One subtest per task id, table-driven, t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE BYTES THIS LEG EMITS and the shared runtime
// body it emits them with, so the harness is reachable on a tree that never
// built the C++ reference corpus and never carries a sibling serialize.rs —
// the shape fixedroadmap_t04_test.go states for the same reason. The rows that
// need the emitted code to RUN against the corpus are held by the
// fixedversioning_*_test.go probes, which a gate runs; the clauses those probes
// cannot read off the emitted source are the ones below.
//
// rust/E6 [verify]: renaming uses the declared identity.
//
// rust/E8 [verify]: append and deprecate under the backward-read contract.
//
// rust/R19 [verify]: a clean NEW-READS-OLD of an appended field, variant, arm,
// flag or keyed slot moves no counter at all.
//
// rust/E9 [owed]: duplicate never raised.
//
// rust/R16 [weak]: §5.4's counters exactly.
//
// rust/R18 [owed]: a clamp that cannot fire is not emitted and nothing moves.
//
// rust/C3 [verify]: text length clamp.
//
// rust/C4 [owed]: text content refuses BY NAME.
//
// rust/R24 [owed]: ill-formed text in the USED units refuses by name; the
// count is in the text's own units and the slack is not a refusal.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

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

// t05Pair builds the versioning pair of one row and answers the emitted module
// together with both units: the reader is newSrc, handed oldSrc's locked entry
// as its oldest lineage entry, exactly as the t04 harness does.
func t05Pair(t *testing.T, oldSrc, newSrc string) (text string, older, own *ir.Unit) {
	t.Helper()
	older = unitFrom(t, oldSrc)
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
	return t05Fixed(t, files), older, own
}

// t05Layout is one unit's OWN locked layout bytes.
func t05Layout(t *testing.T, u *ir.Unit, name string) []byte {
	t.Helper()
	st := u.Tables[name]
	if st == nil {
		t.Fatalf("the fixture declares no %s table", name)
	}
	return ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
}

// ---- rust/E6 -----------------------------------------------------------------

// t05E6 holds rust/E6 to SPEC-TABLES §3.4 and the bill's identity rule: a
// `| was = "a"` rename keeps the writer's WIRE ID, so the renamed field is
// matched by the declared identity and the layout does not move. The emitted
// hash is the old build's hash and the runtime's compiler matches on
// `tc.id == mc.id` and never on a field NAME.
func t05E6(t *testing.T) {
	text, older, own := t05Pair(t,
		"package probe\n\nfixed table Lineage\n{\n    a int32 = 0\n}\n",
		"package probe\n\nfixed table Lineage\n{\n    b int32 = 0 | was = \"a\"\n}\n")
	oldField := older.Tables["Lineage"].Fields[0]
	newField := own.Tables["Lineage"].Fields[0]
	if oldField.Name != "a" || newField.Name != "b" {
		t.Fatalf("E6: the fixture is not a rename: %q -> %q", oldField.Name, newField.Name)
	}
	if got := ir.TableFieldWireName(newField); got != "a" {
		t.Errorf("E6: the renamed field's wire name is %q, want a — the `was` alias IS the identity", got)
	}
	if ir.TableWireId(ir.TableFieldWireName(oldField)) != ir.TableWireId(ir.TableFieldWireName(newField)) {
		t.Error("E6: the rename moved the field's wire id")
	}
	// THE LAYOUT IS BYTE-IDENTICAL and the emitted hash is the older build's:
	// a rename moves no wire byte at all.
	oldLayout, newLayout := t05Layout(t, older, "Lineage"), t05Layout(t, own, "Lineage")
	if string(oldLayout) != string(newLayout) {
		t.Error("E6: the rename moved the layout bytes")
	}
	want := fmt.Sprintf("pub const LINEAGE_FIXED_HASH: u64 = 0x%016x;", ir.TableFixedLayoutHash(oldLayout, older.Tables["Lineage"]))
	t05Has(t, text, want, "E6 the emitted hash is the declared identity's")
	// AND THE COMPILER MATCHES BY ID, not by name.
	t05Has(t, string(fixedRuntimeBody), "if tc.id == mc.id {", "E6 the plan compiler matches the writer's field by its id")
}

// ---- rust/E8 -----------------------------------------------------------------

// t05E8 holds rust/E8 to the backward-read contract of SPEC-TABLES §3.4 and
// FIXED-FORM-VERSIONING-TESTS.md's `field_append` and `field_deprecate` rows:
// an APPENDED field has no plan entry, so the PREFILL lands its declared
// default, and a DEPRECATED field keeps its slot, its id and its declared
// default, so a deprecation moves the layout not at all and the older reader
// reads the newer file (a deprecation is not newer).
func t05E8(t *testing.T) {
	// APPEND: the appended `w = 77` sits in the prefill image and the reader's
	// holes are filled from it.
	text, _, _ := t05Pair(t,
		"package probe\n\nfixed table Lineage\n{\n    a int32 = 0\n    seq int32 = 0\n}\n",
		"package probe\n\nfixed table Lineage\n{\n    a int32 = 0\n    seq int32 = 0\n    w int32 = 77\n}\n")
	t05Has(t, text, "0x4d, 0x00, 0x00, 0x00,", "E8 the appended field's declared default rides the prefill image")
	t05Has(t, text, "table_fixed_holes(entries, &mut cover, &mut hole_buf);", "E8 the reader's holes are what the plan does not write")
	t05Has(t, text, "image[o..o + z].copy_from_slice(&LINEAGE_FIXED_DEFAULTS[o..o + z]);", "E8 an appended field lands the declared default")

	// DEPRECATE: the pair is `{a,b,c}` -> `{a,b deprecated,c}`. The layout is
	// byte-identical and the hash does NOT move, which is the row's whole
	// point: a deprecation is not newer. The slot still rides and the reader
	// still lands it.
	oldSrc := "package probe\n\nfixed table Lineage\n{\n    a int32 = 0\n    b int32 = 5\n    c int32 = 0\n}\n"
	newSrc := "package probe\n\nfixed table Lineage\n{\n    a int32 = 0\n    b int32 = 5 | deprecated\n    c int32 = 0\n}\n"
	older, own := unitFrom(t, oldSrc), unitFrom(t, newSrc)
	if !own.Tables["Lineage"].Fields[1].Deprecated {
		t.Fatal("E8: the fixture's second field is not marked deprecated")
	}
	oldLayout, newLayout := t05Layout(t, older, "Lineage"), t05Layout(t, own, "Lineage")
	if string(oldLayout) != string(newLayout) {
		t.Error("E8: a deprecation moved the layout bytes; the slot stays forever")
	}
	if ir.TableFixedLayoutHash(oldLayout, older.Tables["Lineage"]) != ir.TableFixedLayoutHash(newLayout, own.Tables["Lineage"]) {
		t.Error("E8: a deprecation moved the hash; the older reader must still read the newer file")
	}
	depFiles, err := Generate(own)
	if err != nil {
		t.Fatalf("E8: Generate: %v", err)
	}
	dep := t05Fixed(t, depFiles)
	t05Has(t, dep, "0x05, 0x00, 0x00, 0x00,", "E8 the deprecated slot's declared default rides the prefill image")
	t05Has(t, dep, "value.b as u32).to_le_bytes()", "E8 the deprecated slot still rides the wire")
	// NOTHING COUNTS: the compile census counts only a WRITER field the reader
	// cannot name; a deprecation names the field on both sides.
	t05Has(t, string(fixedRuntimeBody), "if !named && census {", "E8 a deprecation is not a loss and nothing counts")
}

// ---- rust/R19 -----------------------------------------------------------------

// t05R19 holds rust/R19 to §5.4's last line: "a clean NEW-READS-OLD of an
// appended field, variant, arm, flag or keyed slot — every counter stays at
// zero: an append the reader knows is not an event." The appended thing is a
// reader-side field the WRITER does not carry, so the compile census (which
// counts the other direction) names nothing, and the load's holes carry the
// declared default. Each fixture is driven through the emitted module.
func t05R19(t *testing.T) {
	// A FIELD APPEND: no ranged scalar and no text, so the whole read carries
	// no counter at all — the emitted module never mentions one.
	text, _, _ := t05Pair(t,
		"package probe\n\nfixed table Lineage\n{\n    a int32 = 0\n    seq int32 = 0\n}\n",
		"package probe\n\nfixed table Lineage\n{\n    a int32 = 0\n    seq int32 = 0\n    w int32 = 77\n}\n")
	for _, counter := range []string{"report.clamped", "report.widened", "report.unknown += 1", "report.kind_mismatch += 1"} {
		if strings.Contains(text, counter) {
			t.Errorf("R19: a clean append moved %q in the emitted read", counter)
		}
	}
	t05Has(t, text, "table_fixed_holes(entries, &mut cover, &mut hole_buf);", "R19 an appended field is a hole the prefill covers")

	// A VARIANT, AN ARM, A FLAG AND A KEYED SLOT APPEND: each is a reader-side
	// extent the writer lacks. The emitted read still carries only the compile
	// census and the prefill, and the census is the WRITER-direction one.
	rows := []struct {
		id             string
		oldSrc, newSrc string
	}{
		{"enum variant", "package probe\n\nenum Tier { Bronze, Silver, Gold }\n\nfixed table Lineage\n{\n    e Tier = Bronze\n    seq int32 = 0\n}\n",
			"package probe\n\nenum Tier { Bronze, Silver, Gold, Platinum }\n\nfixed table Lineage\n{\n    e Tier = Bronze\n    seq int32 = 0\n}\n"},
		{"union arm", "package probe\n\ntype Alpha { m int32 = 0 }\ntype Beta { n int32 = 0 }\ntype Gamma { p int32 = 0 }\n\nunion Pick\n{\n    alpha Alpha\n    beta Beta\n}\n\nfixed table Lineage\n{\n    pick Pick\n    seq int32 = 0\n}\n",
			"package probe\n\ntype Alpha { m int32 = 0 }\ntype Beta { n int32 = 0 }\ntype Gamma { p int32 = 0 }\n\nunion Pick\n{\n    alpha Alpha\n    beta Beta\n    gamma Gamma\n}\n\nfixed table Lineage\n{\n    pick Pick\n    seq int32 = 0\n}\n"},
		{"flag", "package probe\n\nflags Caps { Jump, Crouch }\n\nfixed table Lineage\n{\n    caps Caps\n    seq int32 = 0\n}\n",
			"package probe\n\nflags Caps { Jump, Crouch, Prone }\n\nfixed table Lineage\n{\n    caps Caps\n    seq int32 = 0\n}\n"},
		{"keyed slot", "package probe\n\nenum Key { a, b, c }\n\nfixed table Lineage\n{\n    slots [Key]int32\n    seq int32 = 0\n}\n",
			"package probe\n\nenum Key { a, b, c, d }\n\nfixed table Lineage\n{\n    slots [Key]int32\n    seq int32 = 0\n}\n"},
	}
	for _, row := range rows {
		text, _, _ := t05Pair(t, row.oldSrc, row.newSrc)
		t05Has(t, text, "table_fixed_holes(entries, &mut cover, &mut hole_buf);", "R19 "+row.id+": the reader's new extent is prefilled")
		if strings.Contains(text, "report.widened += 1") {
			t.Errorf("R19 %s: the append widened a value", row.id)
		}
	}
	// THE COUNTER SITES THEMSELVES: the census is the compile-time, writer-
	// direction one and lands once after the loop; widened rides only the two
	// widen ops, so no append reaches it.
	runtime := string(fixedRuntimeBody)
	t05Has(t, runtime, "if !named && census {", "R19 only a writer field the reader cannot name censuses")
	if n := strings.Count(runtime, "report.widened += 1;"); n != 2 {
		t.Errorf("R19: widened is raised at %d sites, want the two widen ops", n)
	}
	// THE CENSUS LANDS ONCE, after the record loop, on a read that RETURNS.
	load := t05Fn(text, "lineage_fixed_load")
	if load == "" {
		t.Fatal("R19: lineage_fixed_load is not emitted")
	}
	t05Has(t, load, "report.unknown += census.0;", "R19 the census lands once, after the record loop")
}

// ---- rust/E9 -----------------------------------------------------------------

// t05E9 holds rust/E9 to FIXED-FORM-ALGORITHM.md §4 and §29: the fixed form
// "raises all but `duplicate`" — `duplicate` is the TEXT form's counter for a
// repeated map key, and a fixed table refuses a map field outright (its only
// keyed structure is the enum-keyed array, indexed by variant with no key on
// the wire). This leg's report carries the fixed form's own members and no
// `duplicate`, and the emitted module never raises one.
func t05E9(t *testing.T) {
	runtime := string(fixedRuntimeBody)
	for _, member := range []string{
		"pub unknown: u32,", "pub kind_mismatch: u32,", "pub widened: u32,",
		"pub clamped: u32,", "pub malformed: bool,", "pub refused: bool,",
	} {
		t05Has(t, runtime, member, "E9 the fixed form's own counter set")
	}
	if strings.Contains(runtime, "duplicate") {
		t.Error("E9: the fixed runtime carries a `duplicate` member; §4 raises all but duplicate")
	}
	files, err := Generate(unitFrom(t, "package probe\n\nenum Key { a, b, c }\n\nfixed table Lineage\n{\n    slots [Key]int32\n    seq int32 = 0\n}\n"))
	if err != nil {
		t.Fatalf("E9: Generate: %v", err)
	}
	if text := t05Fixed(t, files); strings.Contains(text, "duplicate") {
		t.Error("E9: the emitted fixed module raises `duplicate`")
	}
}

// ---- rust/R16 -----------------------------------------------------------------

// t05R16 holds rust/R16 to §5.4's table, clause by clause: the COMPILE census
// is once per field per peer and never per record (the array and keyed walks
// census only element 0); `widened` is once per entry per record and a folded
// element run is ONE, which this leg gets by folding Copy entries only and
// never a widen; `clamped` is once per entry per record for `count`/`text` and
// the bounds pass owns the forged ordinal; copy, const, present and ordinal
// land a value and move nothing.
func t05R16(t *testing.T) {
	// A PAIR drives the emitted load, where the compile census LANDS.
	pair, _, _ := t05Pair(t,
		"package probe\n\nfixed table Lineage\n{\n    a int32 = 0\n    seq int32 = 0\n}\n",
		"package probe\n\nfixed table Lineage\n{\n    a int32 = 0\n    seq int32 = 0\n    w int32 = 77\n}\n")
	load := t05Fn(pair, "lineage_fixed_load")
	if load == "" {
		t.Fatal("R16: lineage_fixed_load is not emitted")
	}
	runtime := string(fixedRuntimeBody)
	// THE CENSUS IS PER FIELD PER PEER, at COMPILE.
	t05Has(t, runtime, "if !named && census {", "R16 unknown is the compile census of an unnamed writer field")
	t05Has(t, runtime, "report.unknown += 1;", "R16 the census counts one")
	t05Has(t, runtime, "census && i == 0,", "R16 a counted array censuses element 0 only")
	t05Has(t, runtime, "census && k == 0,", "R16 a keyed array censuses slot 0 only")
	t05Has(t, load, "report.unknown += census.0;", "R16 the census lands ONCE per peer, never per record")
	t05Has(t, load, "report.kind_mismatch += census.1;", "R16 kind_mismatch is the compile census too")
	// WIDENED: once per entry per record, and only a Copy run folds.
	if n := strings.Count(runtime, "report.widened += 1;"); n != 2 {
		t.Errorf("R16: widened is raised at %d sites, want the widen and widenF ops", n)
	}
	t05Has(t, runtime, "c.plan[out - 1].op == TableFixedOp::Copy", "R16 only a Copy run folds, so a widened run is never folded")
	t05Has(t, runtime, "c.plan[i].op == TableFixedOp::Copy", "R16 the fold needs BOTH neighbours to be Copy")
	// CLAMPED: once per entry per record for count and text.
	t05Has(t, runtime, "let held = raw.clamp(0, p.size as i32);", "R16 the count op clamps once per entry")
	t05Has(t, runtime, "let held = raw.clamp(0, p.aux as i32);", "R16 the text op clamps once per entry")
	// THE BOUNDS PASS COUNTS A FORGED ORDINAL REMAPPED TO None on the same
	// bytes for either plan: the compiled plan's exact entry and the identity
	// plan's scatter both land `0` and count ONE, so the second sight of the
	// remapped `0` cannot count again.
	t05Has(t, runtime, "if raw > u64::from(*count) {", "R16 the ordinal past the writer's count counts one")
	// COPY, CONST, PRESENT AND ORDINAL MOVE NOTHING: the Copy arm carries no
	// counter, and the only counts in the runtime are the ones above.
	copyArm := t05Arm(runtime, "TableFixedOp::Copy => {", "TableFixedOp::Count => {")
	if copyArm == "" {
		t.Fatal("R16: the Copy arm is not emitted")
	}
	for _, counter := range []string{"report.clamped", "report.widened", "report.unknown"} {
		if strings.Contains(copyArm, counter) {
			t.Errorf("R16: the Copy op moves %q; copy lands a value and moves nothing", counter)
		}
	}
}

// t05Arm slices one emitted match arm out of the runtime body, from `open` to
// `next`, answering "" when the shape moved.
func t05Arm(body, open, next string) string {
	at := strings.Index(body, open)
	if at < 0 {
		return ""
	}
	end := strings.Index(body[at:], next)
	if end < 0 {
		return ""
	}
	return body[at : at+end]
}

// ---- rust/R18 -----------------------------------------------------------------

// t05R18 holds rust/R18 to §5.4's closing paragraph: "A clamp that cannot fire
// is not emitted, and nothing moves." An ordinal whose extent FILLS its storage
// width — 255 variants in a byte — has no read-side comparison to make, and a
// union whose every tag value names an arm has none either. The elided check
// never clamped, so the counter it would have moved never moves. A `bits(N)`
// that fills its own lane is the same rule at the width clamp.
func t05R18(t *testing.T) {
	// 255 VARIANTS IN A BYTE: tagMax(1) is 255, so the extent is full.
	var e strings.Builder
	e.WriteString("package probe\n\nenum Wide {")
	for i := 1; i <= 255; i++ {
		fmt.Fprintf(&e, " v%d,", i)
	}
	e.WriteString(" }\n\nfixed table Lineage\n{\n    e Wide = v1\n}\n")
	u := unitFrom(t, e.String())
	f := u.Tables["Lineage"].Fields[0]
	if extent, ok := fixedEnumExtent(f, 1); ok {
		t.Errorf("R18: a 255-variant byte enum answers extent %d, want no check at all", extent)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("R18: Generate: %v", err)
	}
	emitted := t05Fixed(t, files)
	if strings.Contains(emitted, "report.clamped") {
		t.Error("R18: a full-width enum emitted a clamp that cannot fire")
	}
	scatter := t05Fn(emitted, "lineage_fixed_scatter")
	if scatter == "" {
		t.Fatal("R18: lineage_fixed_scatter is not emitted")
	}
	if strings.Contains(scatter, "if raw >") {
		t.Error("R18: a full-width enum still emits its useless comparison")
	}
	t05Has(t, scatter, "value.e = raw;", "R18 the full-width ordinal rides verbatim")

	// 255 ARMS AT WIDTH 1: every tag value names an arm, so there is no
	// out-of-range tag to clamp.
	var ub strings.Builder
	ub.WriteString("package probe\n\n")
	for i := 1; i <= 255; i++ {
		fmt.Fprintf(&ub, "type A%d { m int32 = 0 }\n", i)
	}
	ub.WriteString("\nunion Pick {\n")
	for i := 1; i <= 255; i++ {
		fmt.Fprintf(&ub, "    a%d A%d\n", i, i)
	}
	ub.WriteString("}\n\nfixed table Lineage\n{\n    pick Pick\n}\n")
	uu := unitFrom(t, ub.String())
	if got := len(uu.Unions["Pick"].Variants); got != 255 {
		t.Fatalf("R18: the fixture carries %d arms, want 255", got)
	}
	if tagMax(1) != 255 {
		t.Fatalf("R18: tagMax(1) is %d, want 255", tagMax(1))
	}
	ufiles, err := Generate(uu)
	if err != nil {
		t.Fatalf("R18: Generate union: %v", err)
	}
	uEmitted := t05Fixed(t, ufiles)
	if strings.Contains(uEmitted, "if tag_raw >") {
		t.Error("R18: a full-width union tag still emits its useless comparison")
	}
	t05Has(t, t05Fn(uEmitted, "pick_fixed_scatter"), "value.tag = tag_raw;", "R18 a full-width tag rides verbatim")

	// `bits(32)` FILLS ITS OWN LANE: the check is elided the same way.
	bfiles, err := Generate(unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    n bits(32)\n}\n"))
	if err != nil {
		t.Fatalf("R18: Generate bits: %v", err)
	}
	if text := t05Fixed(t, bfiles); strings.Contains(text, "report.clamped") {
		t.Error("R18: bits(32) fills its lane exactly and must emit NO clamp")
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
