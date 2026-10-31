package ctable

// THE ROADMAP'S VERSIONING TASKS ON THE C LEG (docs/roadmap.sexp node
// `fixed-tables`; docs/FIXED-FORM-ALGORITHM.md §5.2 to §5.7;
// docs/FIXED-FORM-VERSIONING-TESTS.md "The floor and the hash"; SPEC-TABLES
// §3.4 and §21.2). One subtest per task id, table-driven, t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE C THIS LEG EMITS and the shared runtime body it
// emits with, so the harness is reachable on a tree that never built the C++
// reference corpus. A clause that needs the emitted code to RUN is asserted at
// the emitted site that runs it, and the run itself is named in the check.
//
// THE FLOOR'S OWN RUNTIME IS ALREADY HELD by fixedversioning_test.go's
// TestFixedVersioningFloor, which a gate runs (`make tables-c-versioning`). The
// subtests below carry the clauses those probes cannot read off the bytes: the
// floor's ARITHMETIC in the emitter, and the refusal's position and purity in
// the emitted load.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- fixtures and readers of this leg's output ------------------------------

// cT04Header is every emitted <Base>Table.h, concatenated: the one file the
// fixed form's static data and static functions ride in.
func cT04Header(t *testing.T, files map[string][]byte) string {
	t.Helper()
	var b strings.Builder
	for name, data := range files {
		if strings.HasSuffix(name, "Table.h") {
			b.Write(data)
		}
	}
	if b.Len() == 0 {
		t.Fatalf("no *Table.h was emitted")
	}
	return b.String()
}

// cT04Fn is one emitted function's body, from its `name(` to the column-zero
// closing brace.
func cT04Fn(text, name string) string {
	at := strings.Index(text, name+"(")
	if at < 0 {
		return ""
	}
	rest := text[at:]
	if open := strings.Index(rest, "\n{\n"); open >= 0 {
		rest = rest[open:]
	}
	if before, _, ok := strings.Cut(rest, "\n}\n"); ok {
		return before
	}
	return rest
}

// cT04Has is a required substring of the emitted source, named by rule.
func cT04Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// cT04Retire builds THREE generations of one table — x, x+y, x+y+z — marks the
// first `retired` of the first two in the lock's lineage, and answers the emitted
// header together with the floor it computed.
func cT04Retire(t *testing.T, retired int) string {
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
		e1.Reason = "the operator retired this layout"
	}
	if retired > 1 {
		e2.Retired = true
		e2.Reason = "the operator retired this layout"
	}
	files, err := GenerateLineage(own, map[string][]FixedLineageEntry{"Lineage": {e1, e2}})
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	return cT04Header(t, files)
}

// cT04RetireMiddle builds the same THREE generations but retires ONLY the
// middle entry (index 1), the oldest staying live. §5.2's floor is 1 + the
// HIGHEST retired index = 2 here, where counting the retired entries would
// answer 1; a prefix-only fixture cannot tell those two rules apart.
func cT04RetireMiddle(t *testing.T) string {
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
	e2.Retired = true
	e2.Reason = "the operator retired this layout"
	files, err := GenerateLineage(own, map[string][]FixedLineageEntry{"Lineage": {e1, e2}})
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	return cT04Header(t, files)
}

// cT04Lineage builds the versioning pair of one row and answers the emitted
// header: the reader is `newSrc`, handed `oldSrc`'s locked entry as its oldest
// lineage entry, exactly as fixedversioning_test.go's probe harness does.
func cT04Lineage(t *testing.T, oldSrc, newSrc string) (text string, own *ir.Unit) {
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
	return cT04Header(t, files), own
}

// ---- c/R3 -------------------------------------------------------------------

// cT04R3 holds c/R3 to §5.2's one number: "the floor is 1 + the highest retired
// index (0 when none)", emitted beside the lineage, and to LOAD step 6: below
// the floor is layout_unsupported, reporting the file's hash. The runtime half
// is TestFixedVersioningFloor/floor_below; the arithmetic and the emitted site
// are read here.
func cT04R3(t *testing.T) {
	text := cT04Retire(t, 2)
	cT04Has(t, text, "lineage_fixed_floor = 2;", "R3 1 + the highest retired index")
	load := cT04Fn(text, "lineage_fixed_load_tolerant")
	if load == "" {
		t.Fatal("R3: lineage_fixed_load is not emitted")
	}
	cT04Has(t, load, "if ( pick < lineage_fixed_floor )", "R3 the floor check")
	cT04Has(t, load, "table_fixed_refuse_hash( report, SCHEMA_TABLE_LAYOUT_UNSUPPORTED, hash )", "R3 below the floor is layout_unsupported")
	if !strings.Contains(string(tableFixedRuntime), "report->layout_hash = hash;") {
		t.Error("R3: refuse_hash does not report the file's hash")
	}

	// NOTHING RETIRED is a floor of ZERO, exactly as §5.2 computes it.
	none := cT04Retire(t, 0)
	cT04Has(t, none, "lineage_fixed_floor = 0;", "R3 the floor is 0 when none is retired")

	// ONLY A NON-TRAILING entry retired: the floor is 1 + the HIGHEST retired
	// index, NOT the number of retired entries. The middle generation is
	// retired and the oldest stays live, so §5.2 answers 2 where a count of
	// retired entries would answer 1.
	onlyMid := cT04RetireMiddle(t)
	cT04Has(t, onlyMid, "lineage_fixed_floor = 2;", "R3 1 + the highest retired index, not a count of retired entries")
}

// ---- c/R32 ------------------------------------------------------------------

// cT04R32 holds c/R32 to §5.2's retirement, read off the emitted load: the
// operator's mark is laid down per entry, the floor check sits ONCE, after the
// hash select and BEFORE the byte comparison or any record, and its whole body
// is the named refusal — a pure read of `pick`, so a second load of the same
// file answers the same name. REFUSE is TOTAL: the load zeroes its report and no
// plan runs.
func cT04R32(t *testing.T) {
	text := cT04Retire(t, 1)
	cT04Has(t, text, "RETIRED:", "R32 the retired entry's mark is laid down")
	load := cT04Fn(text, "lineage_fixed_load_tolerant")
	if load == "" {
		t.Fatal("R32: lineage_fixed_load is not emitted")
	}
	if n := strings.Count(load, "pick < lineage_fixed_floor"); n != 1 {
		t.Errorf("R32: the floor is tested %d times, want once", n)
	}
	at := strings.Index(load, "pick < lineage_fixed_floor")
	known := strings.Index(load, "memcmp( layout, lineage_fixed_known[pick].layout")
	records := strings.Index(load, "for ( k = 0; k < count; ++k )")
	if at < 0 || known < 0 || records < 0 {
		t.Fatalf("R32: the load's shape moved (%d, %d, %d)", at, known, records)
	}
	if at >= known || known >= records {
		t.Errorf("R32: the floor check is not before the byte comparison and the record loop (%d, %d, %d)", at, known, records)
	}
	// ONCE AND IDEMPOTENT: the guarded body reads `pick` and returns the named
	// refusal; nothing before it mutates the report or the destination.
	body := load[at:]
	if end := strings.Index(body, "\n"); end >= 0 {
		body = body[:end]
	}
	if strings.Contains(body, "unknown") || strings.Contains(body, "values[") {
		t.Errorf("R32: the floor refusal is not a pure named refusal:\n%s", body)
	}
	cT04Has(t, load, "memset( &local, 0, sizeof( local ) );", "R32 REFUSE is TOTAL: the report starts zeroed")
	cT04Has(t, string(tableFixedRuntime), "report->refused = 1;", "R32 REFUSE sets refused")
	cT04Has(t, string(tableFixedRuntime), "report->reason = reason;", "R32 REFUSE sets the reason")
}

// ---- c/R10 ------------------------------------------------------------------

// cT04R10 holds c/R10 to §5.6 and §5.3 step 7: "the hash is looked up, the floor
// is checked, and the layout is COMPARED — the seven rules do not fire at run
// time". The emitted load parses no layout (the parse is the build-time walk of
// the LOCK's own bytes), walks no stranger's bytes, and never recomputes the
// header's hash: the header's eight bytes are taken as given and matched on.
func cT04R10(t *testing.T) {
	text := cT04Retire(t, 0)
	load := cT04Fn(text, "lineage_fixed_load_tolerant")
	if load == "" {
		t.Fatal("R10: lineage_fixed_load is not emitted")
	}
	cT04Has(t, load, "memcmp( layout, lineage_fixed_known[pick].layout", "R10 the layout is a byte comparison")
	cT04Has(t, load, "report->reason = SCHEMA_TABLE_LAYOUT_MALFORMED;", "R10 a lie about a known version is layout_malformed")
	if strings.Contains(load, "table_fixed_parse_layout") {
		t.Error("R10: the load walks a stranger's layout; the walk belongs to the lock's bytes")
	}
	if strings.Contains(load, "table_fixed_hash_of") {
		t.Error("R10: the load recomputes the header's hash")
	}
	if strings.Contains(load, "table_fixed_check_entry") || strings.Contains(load, "table_fixed_leaf_size") {
		t.Error("R10: the load carries §1.1's layout rules into the run time")
	}
	// THE HASH IS STILL A WIRE IDENTITY: the function survives for the build side,
	// but no load path calls it.
	cT04Has(t, string(tableFixedRuntime), "static SCHEMA_UNUSED uint64_t table_fixed_hash_of(", "R10 the hash function")
}

// ---- c/R15 ------------------------------------------------------------------

// cT04R15 holds c/R15 to §5.6's retirement list and §5.3 step 5: the four
// forward-read clamps are gone, and each is `layout_newer` now. A hash the
// lineage does not hold is refused BEFORE any record and before any plan
// selection, on the file's hash; the count and text clamps that REMAIN are the
// writer's bounds carried by the plan, which only a backward read over a locked
// entry can reach; and the unknown-field count lands from the COMPILE census
// after a returning read, never per record.
func cT04R15(t *testing.T) {
	text := cT04Retire(t, 0)
	load := cT04Fn(text, "lineage_fixed_load_tolerant")
	if load == "" {
		t.Fatal("R15: lineage_fixed_load is not emitted")
	}
	miss := strings.Index(load, "table_fixed_refuse_hash( report, SCHEMA_TABLE_LAYOUT_NEWER, hash )")
	records := strings.Index(load, "for ( k = 0; k < count; ++k )")
	if miss < 0 || records < 0 {
		t.Fatalf("R15: the load's refusals moved (%d, %d)", miss, records)
	}
	if miss > records {
		t.Error("R15: an unknown hash is not refused before the first record")
	}
	cT04Has(t, load, "census_unknown = lineage_fixed_lineage[pick].unknown;", "R15 unknown is the compile census")
	runtime := string(tableFixedRuntime)
	cT04Has(t, runtime, "else if ( v > (int32_t) p->size )", "R15 the count clamp is the writer's bound")
	cT04Has(t, runtime, "else if ( (uint32_t) v > p->size )", "R15 the text clamp is the writer's span")
}

// ---- c/E3 -------------------------------------------------------------------

// cT04E3 holds c/E3 to §5.2's EMIT ladder, in full: every required rung is
// present — 2..5, 6..9, 20..24, 25..29 and 10 -> 11 — the float rung is
// kTableFixedWidenF, the sign is the WRITER's kind, and a same-kind widen
// zero-extends. The non-default and boundary values are written and read by the
// gate's corpus rows (`int_widen`, `uint_widen`, `float_widen`,
// `array_elem_widen`, `enum_width`) through TestFixedVersioningNewReadsOld,
// which `make tables-c-versioning` runs.
func cT04E3(t *testing.T) {
	runtime := string(tableFixedRuntime)
	for _, rung := range []string{
		"if ( from >= 6 && from <= 9 && to >= 6 && to <= 9 ) { return to > from; }",
		"if ( from >= 2 && from <= 5 && to >= 2 && to <= 5 ) { return to > from; }",
		"if ( from >= 20 && from <= 24 && to >= 20 && to <= 24 ) { return to > from; }",
		"if ( from >= 25 && from <= 29 && to >= 25 && to <= 29 ) { return to > from; }",
		"return from == 10 && to == 11;",
	} {
		cT04Has(t, runtime, rung, "E3 the widen ladder")
	}
	cT04Has(t, runtime, "e.op = ( te.kind == 10 ) ? (uint8_t) kTableFixedWidenF : (uint8_t) kTableFixedWiden;", "E3 the float rung is chosen by the writer's kind")
	cT04Has(t, runtime, "e.sign = table_fixed_signed_kind( te.kind ) ? (uint8_t) 1 : (uint8_t) 0;", "E3 a ladder widen's sign is the writer's kind")
	cT04Has(t, runtime, "static SCHEMA_UNUSED int table_fixed_signed_kind( uint8_t kind ) { return ( kind >= 2 && kind <= 5 ) || ( kind >= 20 && kind <= 24 ); }", "E3 a same-kind widen zero-extends")
}

// ---- c/C8 -------------------------------------------------------------------

// cT04C8 holds c/C8 to §3.4's constant-size table and §5.2's fixed-point family:
// a `fixed(I,F)` rides as the raw scaled integer at its storage width, `I + F`,
// and the KIND encodes that width and the family and NOT `F` — so an `F`-only
// edit is a definitions-digest change the lock refuses, never a widen. `bits(N)`
// rides at its declared storage width and is clamped at `N`'s own ceiling, which
// is not a storage limit.
func cT04C8(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table Lineage
{
    a fixed(4, 4) | min = -8, max = 7
    b fixed(2, 6) | min = -2, max = 1
    c fixed(8, 8) | min = -128, max = 127
    d ufixed(4, 4) | min = 0, max = 15
    e bits(12)
}
`)
	st := u.Tables["Lineage"]
	if st == nil {
		t.Fatal("C8: the fixture declares no Lineage table")
	}
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
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("C8: Generate: %v", err)
	}
	clamp := cT04Fn(cT04Header(t, files), "schema_probe_lineage_fixed_clamp_body_")
	if clamp == "" {
		t.Fatal("C8: lineage_fixed_clamp_body is not emitted")
	}
	cT04Has(t, clamp, "4095", "C8 bits(12)'s own ceiling")
	runtime := string(tableFixedRuntime)
	cT04Has(t, runtime, "case 20: case 25: return size == 1u;", "C8 the fixed-point ladder's storage widths")
	cT04Has(t, runtime, "case 24: case 29: return size == 16u;", "C8 the fixed-point ladder's top rung")
}

// ---- c/R20 ------------------------------------------------------------------

// cT04R20 holds c/R20 to SPEC-TABLES §21.2, clause by clause, on the emitted
// sites: an added field lands the prefill's declared default; a deprecated field
// is dropped and counted once per plan through the compile census; a narrower
// integer or float is widened exactly and counts widened; a shorter array or
// string lands with the reader's slack as the template's zeros; an older enum's
// ordinals are the reader's, its list a prefix.
func cT04R20(t *testing.T) {
	text := cT04Retire(t, 0)
	runtime := string(tableFixedRuntime)
	// AN ADDED FIELD: the holes are covered from the DEFAULTS image, and the
	// image is zeroed first so a shorter array or string keeps template zeros.
	cT04Has(t, text, "lineage_fixed_cover[]", "R20 the prefill image is emitted")
	cT04Has(t, text, "table_fixed_fill_run( fill, fill_count, (const uint8_t *) &defaults,", "R20 an added field lands the declared default")
	cT04Has(t, text, "memset( (void *) &defaults, 0, sizeof( defaults ) );", "R20 the reader's slack is the template's zeros")
	// A DEPRECATED FIELD, counted ONCE per plan: an array's element 0 is the only
	// one that censuses.
	cT04Has(t, runtime, "if ( !named && census && c->report != NULL ) { c->report->unknown++; }", "R20 an unknown field is counted once per FIELD per endpoint")
	cT04Has(t, runtime, "census && i == 0", "R20 once per FIELD per endpoint, not per element")
	// A NARROWER INTEGER OR FLOAT: widened exactly, and counted.
	cT04Has(t, runtime, "(*widened)++;", "R20 a widen counts")
	// AN OLDER ENUM: the ordinal is remapped through the plan's own table, whose
	// list is the writer's count, so a prefix maps position for position.
	cT04Has(t, runtime, "if ( raw != 0 && raw <= (uint64_t) table[0] ) { v = table[raw]; }", "R20 the older enum's ordinals are the reader's")
}

// ---- c/R21 ------------------------------------------------------------------

// cT04R21 holds c/R21 to §4's float rung: "widenf is the f32 at src as an f64
// at dst". The C leg widens through the hardware conversion, matching the C++
// twin's widenf (an f32 stays an f32 and the value rides the cast). A NaN or
// an infinity never lands: the finite pass refuses it over storage before the
// bounds pass (docs/SPEC-TABLES.md §3.3), so every value the rung carries is
// finite and the ordinary conversion is exact.
func cT04R21(t *testing.T) {
	runtime := string(tableFixedRuntime)
	cT04Has(t, runtime, "const double d = (double) f;", "R21 the float rung widens through the hardware conversion")
	if strings.Contains(runtime, "0x7ff0000000000000ull") && strings.Contains(runtime, "<< 29") {
		t.Error("R21: the float rung still assembles every all-ones exponent by hand")
	}
}

// ---- c/R29 ------------------------------------------------------------------

// cT04R29 holds c/R29 to the 65536 band and to bill §12.5's carried writer
// bounds. THE BAND: a record body AT 65536 is a fixed root and one byte past it
// is not (SPEC-TABLES §3.4, "65536 BYTES OF RECORD BODY: THE FORM IS NOT
// EMITTED, AND THE TABLE IS NAMED"), and a layout entry reaching past the
// writer's own declared record refuses the plan WHOLE under
// layout_record_too_large. THE WRITER'S BOUNDS: the count op clamps to the
// writer's element bound carried by the plan and the text op to the writer's
// span, so a value forged past what the WRITER could have written clamps and
// counts while a value the reader merely widened does not.
func cT04R29(t *testing.T) {
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
	warnings, errs := ir.TableFixedRecordBounds(at, 0)
	if len(errs) != 0 {
		t.Errorf("R29: a body at the ceiling is a warning and not a refusal: %v", errs)
	}
	if joined := strings.Join(warnings, "\n"); !strings.Contains(joined, "Lineage") {
		t.Errorf("R29: the warning does not name the table: %s", joined)
	}
	runtime := string(tableFixedRuntime)
	cT04Has(t, runtime, "kTableFixedRecordMaxBytes = 65536", "R29 the reader holds the endpoint's layout to the same ceiling")
	cT04Has(t, runtime, "if ( root.size == 0u || root.size > kTableFixedRecordMaxBytes )", "R29 a layout root past the ceiling")
	cT04Has(t, runtime, "if ( e.size > kTableFixedRecordMaxBytes )", "R29 an entry past the writer's declared record")
	cT04Has(t, runtime, "else if ( v > (int32_t) p->size )", "R29 the count clamps to the writer's bound")
	cT04Has(t, runtime, "else if ( (uint32_t) v > p->size )", "R29 the text clamps to the writer's span")
	cT04Has(t, runtime, "n = their_n < my_n ? their_n : my_n;", "R29 the count's bound is the writer's")
}

// ---- c/E5 -------------------------------------------------------------------

// cT04E5 holds c/E5 to SPEC-TABLES §3.4's kind 35: the optional wrapper is one
// present byte plus its child and is distinguishable from a plain nesting in the
// layout, and `T` into `?T` lands a CONSTANT present 1 under the row's own guard.
func cT04E5(t *testing.T) {
	if ir.TableKindOptional != 35 {
		t.Fatalf("E5: TableKindOptional = %d, want 35", ir.TableKindOptional)
	}
	plain := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    link int32\n}\n")
	optional := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    link ?int32\n}\n")
	if got := ir.TableFixedTypeBytes(plain.Tables["Lineage"]); got != 4 {
		t.Errorf("E5: a plain int32 nested body is %d, want 4", got)
	}
	if got := ir.TableFixedTypeBytes(optional.Tables["Lineage"]); got != 5 {
		t.Errorf("E5: a ?int32 body is %d, want 5 — one present byte plus the payload", got)
	}
	runtime := string(tableFixedRuntime)
	cT04Has(t, runtime, "case 35:", "E5 the optional wrapper is a layout kind")
	cT04Has(t, runtime, "if ( (uint64_t) e.size != sum + 1u )", "E5 the wrapper is one present byte plus its child")
	cT04Has(t, runtime, "if ( me.kind == 35 && te.kind != 35 )", "E5 T into ?T is its own emit")
	cT04Has(t, runtime, "e.op = kTableFixedPresent;", "E5 the present companion lands a constant 1")
}

// ---- c/W2 -------------------------------------------------------------------

// cT04W2 holds c/W2 to SPEC-TABLES §3.4's present rule: "the payload rides WHOLE
// whether or not it is present; ZERO on write when the flag is 0". The emitted
// write body stores the flag and then the payload behind ONE `if`, so an absent
// optional costs the branch and not one store — the template's zeros are what
// rides, and a caller's untouched payload storage never reaches the wire.
func cT04W2(t *testing.T) {
	text, _ := cT04Lineage(t,
		"package probe\n\nfixed table Lineage\n{\n    link int32\n}\n",
		"package probe\n\nfixed table Lineage\n{\n    link ?int32\n}\n")
	write := cT04Fn(text, "schema_probe_lineage_fixed_write_body_")
	if write == "" {
		t.Fatal("W2: lineage_fixed_write_body is not emitted")
	}
	cT04Has(t, write, "value->link_present ? 1 : 0", "W2 the present flag is stored")
	guard := strings.Index(write, "if ( value->link_present )")
	if guard < 0 {
		t.Fatalf("W2: an absent optional does not guard its store:\n%s", write)
	}
	rest := write[guard:]
	if end := strings.Index(rest, "\n    }"); end >= 0 {
		rest = rest[:end]
	}
	if !strings.Contains(rest, "table_fixed_put32") && !strings.Contains(rest, "memcpy") {
		t.Errorf("W2: the guard does not wrap the payload's store:\n%s", write)
	}
	if n := strings.Count(write, "if ( value->link_present )"); n != 1 {
		t.Errorf("W2: the absent-optional guard appears %d times, want once", n)
	}
}

// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapCT04Versions(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "c/R3", check: cT04R3},
		{id: "c/R32", check: cT04R32},
		{id: "c/R10", check: cT04R10},
		{id: "c/R15", check: cT04R15},
		{id: "c/E3/other-required-widens", check: cT04E3},
		{id: "c/C8", check: cT04C8},
		{id: "c/R20", check: cT04R20},
		{id: "c/R21", check: cT04R21},
		{id: "c/R29", check: cT04R29},
		{id: "c/E5", check: cT04E5},
		{id: "c/W2", check: cT04W2},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
