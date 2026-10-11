package rusttable

// THE ROADMAP'S LAST RUST ROWS (docs/roadmap.sexp node `fixed-tables`; the
// rows `field-evolution/rust`, `reports/rust` and `text/rust`;
// docs/FIXED-FORM-ALGORITHM.md §5.4 counters and §5.4's "a clamp that cannot
// fire is not emitted"; docs/FIXED-FORM-VERSIONING-TESTS.md "The rows" and
// "§5.4's counters exactly"; docs/SPEC-TABLES.md §3.4). One subtest per task
// id, table-driven, t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE BYTES THIS LEG EMITS and the shared runtime
// body it emits them with, the shape fixedroadmap_t04_test.go states for the
// same reason: the harness is reachable on a tree that never built the C++
// reference corpus and never carries a sibling serialize.rs.
//
// rust/E6 [verify]: a `was =` rename keeps the wire id, so the two generations
// hash identically and read by identity.
//
// rust/E8 [verify]: append and deprecate under the backward-read contract — an
// appended field lands the prefill's declared default, and a field the reader
// does not name is dropped and counted once per peer through the compile census.
//
// rust/R19 [verify]: a clean NEW-READS-OLD of an appended field moves no counter
// at all — the plan run carries no `widened` and no `clamped` increment, and the
// census is the only number that lands.
//
// rust/E9 [owed]: this form raises all but `duplicate` (ALGORITHM §5.4, docs
// line 26): the report carries no `duplicate` field and the runtime never
// raises one.
//
// rust/R16 [weak]: §5.4's counters exactly, at their emitted sites — the
// compiled census once per peer, `widened`/`clamped` once per entry per record,
// the text clamp in the writer's units.
//
// rust/R18 [owed]: a clamp that cannot fire is not emitted and nothing moves —
// an end that sits on its width's limit, or an extent that fills its storage
// width, emits no comparison.
//
// rust/C3 [verify]: text length clamp — the used length is clamped into the
// writer's span in units and counts one clamped.
//
// rust/C4 [owed] and rust/R24 [owed]: ill-formed text in the USED units refuses
// BY NAME (`text_ill_formed`). NOT IMPLEMENTED on this leg at the start of this
// card: the emitter sets one `malformed` instead (fixedform.go emitFixedTextContent),
// which ALGORITHM §5.8 row 8 names as the reference's divergence and §5.4 names
// as a refusal by name. These two tasks are reported UNKNOWN.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// t05Fixed is the emitted fixed module of a unit.
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

// t05Emit generates one unit and answers its emitted module.
func t05Emit(t *testing.T, src string) string {
	t.Helper()
	files, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return t05Fixed(t, files)
}

// t05Has is a required substring, named by rule.
func t05Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// ---- rust/E6 -----------------------------------------------------------------

// t05E6 holds rust/E6 to ALGORITHM §5.2: a field's wire id is `fnv1a64(wire
// name)` and `was =` matches by the OLD name, so a rename through `was` moves
// no layout byte and the two generations carry the SAME wire hash and read by
// identity (§5.7, the `rename_without_was` row's "the hashes are equal").
func t05E6(t *testing.T) {
	older := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    speed float32\n}\n")
	renamed := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    velocity float32 | was = \"speed\"\n}\n")
	before, ok := FixedLineageOf(older, "Lineage")
	if !ok {
		t.Fatal("E6: no lineage entry for the old spelling")
	}
	after, ok := FixedLineageOf(renamed, "Lineage")
	if !ok {
		t.Fatal("E6: no lineage entry for the renamed spelling")
	}
	if before.Wire != after.Wire {
		t.Errorf("E6: a `was` rename moved the wire id: %#x -> %#x", before.Wire, after.Wire)
	}
	if string(before.Layout) != string(after.Layout) {
		t.Error("E6: a `was` rename moved the layout bytes")
	}
}

// ---- rust/E8 -----------------------------------------------------------------

// t05E8 holds rust/E8 to ALGORITHM §5.4 and §5.2: an APPENDED field is not on
// the writer's wire, so the reader covers it from the DEFAULTS image (the
// prefill's declared default); a field the reader does not name lands from the
// COMPILE census, once per peer. Both sites are in the emitted load.
func t05E8(t *testing.T) {
	text := t05Emit(t, "package probe\n\nfixed table Lineage\n{\n    x int32\n    y int32\n    z int32 = 88\n}\n")
	t05Has(t, text, "_FIXED_DEFAULTS: [u8;", "E8 the prefill image carries the declared defaults")
	t05Has(t, text, "image[o..o + z].copy_from_slice(&", "E8 an appended field lands the declared default")
	t05Has(t, text, "report.unknown += census.0;", "E8 an unknown field is the compile census, once per peer")
	t05Has(t, text, "report.kind_mismatch += census.1;", "E8 a kind that moved off every ladder is the compile census")
}

// ---- rust/R19 -----------------------------------------------------------------

// t05R19 holds rust/R19 to ALGORITHM §5.4's last row: "a clean NEW-READS-OLD of
// an appended field, variant, arm, flag or keyed slot | none | every counter
// stays at zero: an append the reader knows is not an event". The op family the
// plan run uses for an append is copy/const/present, and none of them moves a
// counter; the only number that lands is the census after the loop.
func t05R19(t *testing.T) {
	// The plan-run lanes that land a known append move no counter: Copy is the
	// appended field's lane, Const the present companion's, and neither touches
	// `clamped`, `widened` or `unknown` on the value it lands.
	runtime := string(fixedRuntimeBody)
	copyArm := t05Between(runtime, "TableFixedOp::Copy => {", "TableFixedOp::Count => {")
	if copyArm == "" {
		t.Fatal("R19: the runtime has no Copy arm")
	}
	for _, counter := range []string{"report.clamped", "report.widened", "report.unknown"} {
		if strings.Contains(copyArm, counter) {
			t.Errorf("R19: the Copy lane moves %s on a clean append", counter)
		}
	}
	constArm := t05Between(runtime, "TableFixedOp::Const => {", "\n        }\n    }\n}")
	if constArm != "" {
		for _, counter := range []string{"report.widened", "report.unknown"} {
			if strings.Contains(constArm, counter) {
				t.Errorf("R19: the present/const lane moves %s on a clean append", counter)
			}
		}
	}
	// The census is the only number, and it lands after the record loop.
	text := t05Emit(t, "package probe\n\nfixed table Lineage\n{\n    x int32\n    y int32\n    z int32\n}\n")
	census := strings.Index(text, "report.unknown += census.0;")
	loop := strings.Index(text, "for k in 0..n {")
	if census < 0 || loop < 0 {
		t.Fatalf("R19: the census or the record loop is missing (%d, %d)", census, loop)
	}
	if census < loop {
		t.Error("R19: the census lands before the record loop")
	}
}

// t05Between is the runtime text from the start marker to the next end marker.
func t05Between(runtime, start, end string) string {
	at := strings.Index(runtime, start)
	if at < 0 {
		return ""
	}
	rest := runtime[at:]
	if to := strings.Index(rest[len(start):], end); to >= 0 {
		return rest[:len(start)+to]
	}
	return rest
}

// ---- rust/E9 -----------------------------------------------------------------

// t05E9 holds rust/E9 to ALGORITHM §5.4 (docs line 26): "this form raises all
// but `duplicate`". The report carries no `duplicate` member and the runtime
// never raises one — a port that added a duplicate channel would be raising a
// counter this form does not have.
func t05E9(t *testing.T) {
	runtime := string(fixedRuntimeBody)
	if strings.Contains(runtime, "duplicate") {
		t.Error("E9: the runtime raises a `duplicate` this form does not have")
	}
	text := t05Emit(t, "package probe\n\nfixed table Lineage\n{\n    x int32\n}\n")
	t05Has(t, runtime, "pub struct TableFixedReport", "E9 the report is emitted")
	if strings.Contains(text, "duplicate") {
		t.Error("E9: the emitted report carries a `duplicate` member")
	}
}

// ---- rust/R16 -----------------------------------------------------------------

// t05R16 holds rust/R16 to ALGORITHM §5.4's table, clause by clause, at the
// emitted site that moves each number: `unknown` ONCE at COMPILE, never per
// record; `widened` once per entry per record; `clamped` once per entry per
// record for count/text, in the WRITER's units.
func t05R16(t *testing.T) {
	runtime := string(fixedRuntimeBody)
	text := t05Emit(t, "package probe\n\nfixed table Lineage\n{\n    label string(8)\n    vals [..4]int32\n}\n")
	// unknown at COMPILE, once per peer, after the loop.
	t05Has(t, text, "report.unknown += census.0;", "R16 unknown is the compile census, once per peer")
	if strings.Contains(text, "report.unknown += 1") {
		t.Error("R16: a counter is incremented per record on the read path")
	}
	// widened once per entry per record.
	t05Has(t, runtime, "report.widened += 1;", "R16 a widen counts once per entry per record")
	// clamped for count: the bound is the WRITER's, in elements.
	t05Has(t, runtime, "let held = raw.clamp(0, p.size as i32);", "R16 the count clamps to the writer's bound")
	t05Has(t, runtime, "report.clamped += 1;", "R16 a fired clamp counts once")
	// clamped for text: the writer's span in units, counted in bytes.
	t05Has(t, runtime, "let held = raw.clamp(0, p.aux as i32);", "R16 the text clamps to the writer's span in units")
	// copy/const/present/ordinal move nothing on the value lane (the forged
	// ordinal is the bounds pass's, not the op's — §5.4's "counts twice").
	if arm := t05Between(runtime, "TableFixedOp::Copy => {", "TableFixedOp::Count => {"); strings.Contains(arm, "report.clamped") || strings.Contains(arm, "report.widened") {
		t.Error("R16: the Copy lane moves a counter; copy moves nothing")
	}
}

// ---- rust/R18 -----------------------------------------------------------------

// t05R18 holds rust/R18 to ALGORITHM §5.4: "A clamp that cannot fire is not
// emitted, and nothing moves. An ordinal whose extent FILLS its storage width
// ... has no read-side clamp ... The same rule already applies to a ranged
// scalar whose declared end sits on its width's limit." The emitter answers the
// same way: a scalar at its width limit needs no pass, and neither does a type
// whose only bounds are at the limit.
func t05R18(t *testing.T) {
	// A ranged scalar whose declared end sits on its width's limit needs no
	// clamp; one short of it does.
	atLimit := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    a int8 | min = -128, max = 127\n}\n")
	short := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    a int8 | min = -128, max = 100\n}\n")
	if fixedRangeClampNeeded(atLimit.Tables["Lineage"]) {
		t.Error("R18: a scalar at its width's limit still emits a clamp that cannot fire")
	}
	if !fixedRangeClampNeeded(short.Tables["Lineage"]) {
		t.Error("R18: a clamp that CAN fire is not emitted")
	}
	if got := t05Emit(t, "package probe\n\nfixed table Lineage\n{\n    a int8 | min = -128, max = 127\n}\n"); strings.Contains(got, "fixed_clamp_body") {
		t.Error("R18: the tautological clamp body is emitted")
	}
	// An extent that FILLS its storage width has no out-of-range ordinal; one
	// short of it does.
	full := &ir.Field{Type: ir.FieldType{Kind: ir.TNamed, Ref: &ir.Enum{Max: 255}}}
	shortEnum := &ir.Field{Type: ir.FieldType{Kind: ir.TNamed, Ref: &ir.Enum{Max: 254}}}
	if _, ranged := fixedEnumExtent(full, 1); ranged {
		t.Error("R18: an extent filling its width still emits a range check")
	}
	if _, ranged := fixedEnumExtent(shortEnum, 1); !ranged {
		t.Error("R18: a range check that CAN fire is not emitted")
	}
}

// ---- rust/C3 -----------------------------------------------------------------

// t05C3 holds rust/C3 to ALGORITHM §5.4's `text` row: "v := SLE(4, record+src)
// clamped into [0, cap], COUNT clamped if it fired", cap = the writer's span in
// units. The emitted runtime clamps the length word to `p.aux` and counts.
func t05C3(t *testing.T) {
	runtime := string(fixedRuntimeBody)
	t05Has(t, runtime, "let held = raw.clamp(0, p.aux as i32);", "C3 the text length is clamped")
	t05Has(t, runtime, "image[d..d + 4].copy_from_slice(&held.to_le_bytes());", "C3 the clamped length is what rides")
	if !strings.Contains(runtime, "if held != raw {\n                    report.clamped += 1;") {
		t.Error("C3: the text clamp does not count one clamped")
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
		{id: "rust/C3", check: t05C3},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
