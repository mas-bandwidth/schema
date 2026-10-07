package rusttable

// THE ROADMAP'S VERSIONING TASKS ON THE RUST LEG (docs/roadmap.sexp node
// `fixed-tables`; docs/FIXED-FORM-ALGORITHM.md §5.2 to §5.7;
// docs/FIXED-FORM-VERSIONING-TESTS.md "The floor and the hash" and
// "The old contract's tests, retired by name"; docs/SPEC-TABLES.md §3.4 and
// §21.2). One subtest per task id, table-driven, t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE BYTES THIS LEG EMITS and the shared runtime
// body it emits them with, so the harness is reachable on a tree that never
// built the C++ reference corpus and never carries a sibling serialize.rs. A
// clause that needs the emitted code to RUN is asserted at the emitted site
// that runs it, and that site is named in the check — the shape
// fixedroadmap_t02_test.go states for the same reason.
//
// THE FLOOR'S OWN RUNTIME IS ALREADY HELD by fixedversioning_test.go's
// `TestFixedVersioningFloor/{floor_at,floor_below,floor_raise_live}` probes,
// which a gate runs (`make tables-rust-versioning`). The subtests below carry
// the clauses those probes cannot read off the bytes: the floor's ARITHMETIC
// in the emitter, and the refusal's position and purity in the emitted load.
//
// rust/R3 [verify]: the floor is 1 + the highest retired index (0 when none);
// below the floor is layout_unsupported, reporting the file's hash.
//
// rust/R32 [verify]: retire for real: a retired version is refused by name,
// once, idempotently.
//
// rust/R10 [verify]: the run-time walk of a stranger's layout and the recompute
// of the header's hash are retired.
//
// rust/R15 [verify]: the four forward-read clamps are retired — count clamp
// across bounds, range clamp across versions, remap of an unknown variant to
// None, drop-and-count of an unknown field: each is layout_newer now.
//
// rust/E3/other-required-widens [capability]: all other required widen-ladder
// cases.
//
// rust/C8 [weak]: fixed-point F-shift / bits(N).
//
// rust/R20 [verify]: §21.2's landing rules.
//
// rust/R21 [owed]: widenf is the bit-exact widening — signalling NaNs kept, the
// quiet bit carried as the writer wrote it.
//
// rust/R29 [owed]: the band case: the widening across the 65536 ceiling, and
// the bounds pass clamping to the writer's bounds.
//
// rust/E5 [verify]: ?T vs plain nesting.
//
// rust/W2 [owed]: absent optional skips store.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- fixtures and readers of this leg's output ------------------------------

// t04Fixed is the emitted fixed module of a unit (the one *_fixed.rs the table
// backend produced).
func t04Fixed(t *testing.T, tables map[string][]byte) string {
	t.Helper()
	for name, data := range tables {
		if strings.HasSuffix(name, "_fixed.rs") {
			return string(data)
		}
	}
	t.Fatalf("no *_fixed.rs module was emitted")
	return ""
}

// t04Fn is the emitted body of one `pub fn NAME(...)`, up to its column-zero
// closing brace.
func t04Fn(text, name string) string {
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

// t04Has is a required substring of the emitted source, named by rule.
func t04Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// t04Retire builds THREE generations of one table — x, x+y, x+y+z — hands the
// first `retired` of the first two in as the lock's lineage, and answers the
// emitted module together with the entries it was handed.
func t04Retire(t *testing.T, retired int) string {
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
	return t04Fixed(t, files)
}

// t04Lineage builds the versioning pair of one row and answers the emitted
// module: the reader is `newSrc`, handed `oldSrc`'s locked entry as its oldest
// lineage entry, exactly as fixedversioning_test.go's probe harness does.
func t04Lineage(t *testing.T, oldSrc, newSrc string) (text string, own *ir.Unit) {
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
	return t04Fixed(t, files), own
}

// ---- rust/R3 -----------------------------------------------------------------

// t04R3 holds rust/R3 to §5.2's one number: "R.floor := 1 + the highest index
// marked RETIRED in lock.lineage(T), or 0 when none is", emitted beside the
// lineage, and to LOAD step 6: "if i < R.floor: REFUSE layout_unsupported,
// reporting h". The runtime half is TestFixedVersioningFloor/floor_below; the
// arithmetic and the emitted site are read here.
func t04R3(t *testing.T) {
	text := t04Retire(t, 2)
	t04Has(t, text, "LINEAGE_FIXED_FLOOR: usize = 2;", "R3 1 + the highest retired index")
	load := t04Fn(text, "lineage_fixed_load")
	if load == "" {
		t.Fatal("R3: lineage_fixed_load is not emitted")
	}
	t04Has(t, load, "if found < LINEAGE_FIXED_FLOOR {", "R3 the floor check")
	t04Has(t, load, "return report.refuse_layout(TableFixedReason::LayoutUnsupported, hash);", "R3 below the floor is layout_unsupported")
	if !strings.Contains(string(fixedRuntimeBody), "self.layout_hash = hash;") {
		t.Error("R3: refuse_layout does not report the file's hash")
	}

	// NOTHING RETIRED is a floor of ZERO and a comparison the emitted code does
	// not carry, exactly as §5.2 computes it and as the emitted comment says.
	none := t04Retire(t, 0)
	t04Has(t, none, "LINEAGE_FIXED_FLOOR: usize = 0;", "R3 the floor is 0 when none is retired")
	noneLoad := t04Fn(none, "lineage_fixed_load")
	if strings.Contains(noneLoad, "found < LINEAGE_FIXED_FLOOR") {
		t.Error("R3: a floor of 0 still emits a comparison no value can satisfy")
	}
}

// ---- rust/R32 ----------------------------------------------------------------

// t04R32 holds rust/R32 to §5.2's retirement, read off the emitted load: the
// operator's mark is laid down per entry (`retired: true`), the floor check
// sits ONCE, after the hash select and BEFORE the byte comparison or any
// record, and its whole body is the named refusal — a pure read of `found`, so
// a second load of the same file answers the same name. REFUSE is TOTAL: the
// runtime's refuse zeroes every counter and no plan runs.
func t04R32(t *testing.T) {
	text := t04Retire(t, 1)
	t04Has(t, text, "retired: true,", "R32 the retired entry's mark is laid down")
	t04Has(t, text, "retired: false,", "R32 the current entry is not retired")
	load := t04Fn(text, "lineage_fixed_load")
	if load == "" {
		t.Fatal("R32: lineage_fixed_load is not emitted")
	}
	if n := strings.Count(load, "found < LINEAGE_FIXED_FLOOR"); n != 1 {
		t.Errorf("R32: the floor is tested %d times, want once", n)
	}
	at := strings.Index(load, "found < LINEAGE_FIXED_FLOOR")
	known := strings.Index(load, "let known = &LINEAGE_FIXED_LINEAGE[found];")
	bytes := strings.Index(load, "if block != known.layout {")
	records := strings.Index(load, "for k in 0..n {")
	if at < 0 || known < 0 || bytes < 0 || records < 0 {
		t.Fatalf("R32: the load's shape moved (%d, %d, %d, %d)", at, known, bytes, records)
	}
	if !(at < known && known < bytes && bytes < records) {
		t.Errorf("R32: the floor check is not before the byte comparison and the record loop (%d, %d, %d, %d)", at, known, bytes, records)
	}
	// ONCE AND IDEMPOTENT: the guarded body reads `found` and returns the named
	// refusal; nothing before it mutates the report or the destination.
	body := load[at:]
	if end := strings.Index(body, "\n    }"); end >= 0 {
		body = body[:end]
	}
	if strings.Contains(body, "report.unknown") || strings.Contains(body, "image[") || strings.Contains(body, "values[") {
		t.Errorf("R32: the floor refusal is not a pure named refusal:\n%s", body)
	}
	runtime := string(fixedRuntimeBody)
	for _, zero := range []string{"self.unknown = 0;", "self.kind_mismatch = 0;", "self.widened = 0;", "self.clamped = 0;", "self.malformed = false;"} {
		t04Has(t, runtime, zero, "R32 REFUSE is TOTAL")
	}
}

// ---- rust/R10 -----------------------------------------------------------------

// t04R10 holds rust/R10 to §5.6 and §5.3 step 7: "the hash is looked up, the
// floor is checked, and the layout is COMPARED — the seven rules do not fire at
// run time". The emitted load parses no layout (`TableFixedBlock::parse` is the
// build-time LazyLock's), walks no stranger's bytes, and never recomputes the
// header's hash: the header's eight bytes are taken as given and matched on.
func t04R10(t *testing.T) {
	text := t04Retire(t, 0)
	load := t04Fn(text, "lineage_fixed_load")
	if load == "" {
		t.Fatal("R10: lineage_fixed_load is not emitted")
	}
	t04Has(t, load, "if block != known.layout {", "R10 the layout is a byte comparison")
	t04Has(t, load, "return report.refuse(TableFixedReason::LayoutMalformed);", "R10 a lie about a known version is layout_malformed")
	if strings.Contains(load, "TableFixedBlock::parse") {
		t.Error("R10: the load walks a stranger's layout; the walk is the build's LazyLock")
	}
	if strings.Contains(load, "table_fixed_hash(") {
		t.Error("R10: the load recomputes the header's hash")
	}
	if strings.Contains(load, "check_entry") || strings.Contains(load, "known_kind") || strings.Contains(load, "leaf_size") {
		t.Error("R10: the load carries §1.1's layout rules into the run time")
	}
	// THE HASH IS STILL A WIRE IDENTITY: the function survives for the writer side
	// and for any later reader, but no load path calls it.
	t04Has(t, string(fixedRuntimeBody), "pub fn table_fixed_hash(block: &[u8]) -> u64 {", "R10 the hash function")
}

// ---- rust/R15 -----------------------------------------------------------------

// t04R15 holds rust/R15 to §5.6's retirement list and §5.3 step 5: the four
// forward-read clamps are gone, and each is `layout_newer` now. A hash the
// lineage does not hold is refused BEFORE any record and before any plan
// selection, on the file's hash; the count and text clamps that REMAIN are the
// writer's bounds carried by the plan (bill §12.5), which only a backward read
// over a locked entry can reach; and the unknown-field count lands from the
// COMPILE census after a returning read, never per record.
func t04R15(t *testing.T) {
	text := t04Retire(t, 0)
	load := t04Fn(text, "lineage_fixed_load")
	if load == "" {
		t.Fatal("R15: lineage_fixed_load is not emitted")
	}
	miss := strings.Index(load, "return report.refuse_layout(TableFixedReason::LayoutNewer, hash);")
	records := strings.Index(load, "for k in 0..n {")
	if miss < 0 || records < 0 {
		t.Fatalf("R15: the load's refusals moved (%d, %d)", miss, records)
	}
	if miss > records {
		t.Error("R15: an unknown hash is not refused before the first record")
	}
	if strings.Contains(load, "let identity = found ==") && strings.Index(load, "let identity") < miss {
		t.Error("R15: a plan is selected before the hash is known")
	}
	if strings.Contains(load, "report.unknown += 1") {
		t.Error("R15: an unknown field is dropped-and-counted per record on the read path")
	}
	t04Has(t, load, "report.unknown += census.0;", "R15 unknown is the compile census")
	runtime := string(fixedRuntimeBody)
	t04Has(t, runtime, "let held = raw.clamp(0, p.size as i32);", "R15 the count clamp is the writer's bound")
	t04Has(t, runtime, "let held = raw.clamp(0, p.aux as i32);", "R15 the text clamp is the writer's span")
	if strings.Contains(runtime, "remap.get(base).unwrap_or") {
		t.Error("R15: a forward-read remap survives in the runtime")
	}
}

// ---- rust/E3 ------------------------------------------------------------------

// t04E3 holds rust/E3 to §5.2's EMIT ladder, in full: "if LADDER(te.kind,
// me.kind): emit widenf when te is f32 else widen, sign := te.kind is a signed
// integer or a signed fixed-point". Every required rung is present — 2..5, 6..9,
// 20..24, 25..29 and 10 -> 11 — the float rung is `widenf`, the sign is the
// WRITER's kind, and a same-kind widen zero-extends. The non-default and
// boundary values are written and read by the gate's corpus rows
// (`int_widen`, `uint_widen`, `float_widen`, `array_elem_widen`, `enum_width`).
func t04E3(t *testing.T) {
	runtime := string(fixedRuntimeBody)
	for _, rung := range []string{
		"if (6..=9).contains(&from) && (6..=9).contains(&to) {",
		"if (2..=5).contains(&from) && (2..=5).contains(&to) {",
		"if (20..=24).contains(&from) && (20..=24).contains(&to) {",
		"if (25..=29).contains(&from) && (25..=29).contains(&to) {",
		"from == 10 && to == 11 // f32 -> f64",
	} {
		t04Has(t, runtime, rung, "E3 the widen ladder")
	}
	t04Has(t, runtime, "op: if te.kind == 10 {", "E3 the float rung is chosen by the writer's kind")
	t04Has(t, runtime, "TableFixedOp::WidenF", "E3 the float rung is widenF")
	t04Has(t, runtime, "sign: u8::from(signed_kind(te.kind)),", "E3 a ladder widen's sign is the writer's kind")
	t04Has(t, runtime, "sign: 0,", "E3 a same-kind widen zero-extends")
}

// ---- rust/C8 ------------------------------------------------------------------

// t04C8 holds rust/C8 to §3.4's constant-size table and §5.2's fixed-point
// family: a `fixed(I,F)` rides as the raw scaled integer at its storage width,
// `I + F`, and the KIND encodes that width and the family and NOT `F` — so an
// `F`-only edit is a definitions-digest change the lock refuses, never a widen.
// `bits(N)` rides at its declared storage width and is clamped at `N`'s own
// ceiling, which is not a storage limit.
func t04C8(t *testing.T) {
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
	clamp := t04Fn(t04Fixed(t, files), "lineage_fixed_clamp_body")
	if clamp == "" {
		t.Fatal("C8: lineage_fixed_clamp_body is not emitted")
	}
	t04Has(t, clamp, "4095", "C8 bits(12)'s own ceiling")
	t04Has(t, string(fixedRuntimeBody), "20 | 25 => size == 1,", "C8 the fixed-point ladder's storage widths")
	t04Has(t, string(fixedRuntimeBody), "24 | 29 => size == 16,", "C8 the fixed-point ladder's top rung")
}

// ---- rust/R20 -----------------------------------------------------------------

// t04R20 holds rust/R20 to SPEC-TABLES §21.2, clause by clause, on the emitted
// sites: an added field lands the prefill's declared default; a deprecated field
// is dropped and counted once per plan through the compile census; a narrower
// integer or float is widened exactly and counts widened; a shorter array or
// string lands with the reader's slack as the template's zeros; an older enum's
// ordinals are the reader's, its list a prefix.
func t04R20(t *testing.T) {
	text := t04Retire(t, 0)
	runtime := string(fixedRuntimeBody)
	// AN ADDED FIELD: the holes are covered from the DEFAULTS image, and the
	// image is zeroed first so a shorter array or string keeps template zeros.
	t04Has(t, text, "image[o..o + z].copy_from_slice(&", "R20 an added field lands the declared default")
	t04Has(t, text, "_FIXED_DEFAULTS: [u8;", "R20 the prefill image is emitted")
	// A DEPRECATED FIELD, counted ONCE per plan: the census flag is the first
	// time the peer's field is seen, and an array's element 0 is the only one
	// that censuses.
	t04Has(t, runtime, "if !named && census {", "R20 an unknown field is counted")
	t04Has(t, runtime, "census && i == 0,", "R20 once per FIELD per peer, not per element")
	t04Has(t, text, "report.unknown += census.0;", "R20 the census lands once, after the loop")
	// A NARROWER INTEGER OR FLOAT: widened exactly, and counted.
	t04Has(t, runtime, "report.widened += 1;", "R20 a widen counts")
	// A SHORTER ARRAY OR STRING: the image is the template's zeros, and the plan
	// writes only the writer's live extent.
	t04Has(t, text, "let mut image = [0u8;", "R20 the reader's slack is the template's zeros")
	t04Has(t, text, "table_fixed_holes(entries, &mut cover, &mut hole_buf);", "R20 the holes are what the plan does not write")
	// AN OLDER ENUM: the remap is by NAME and its list is the writer's count, so
	// a prefix maps position for position.
	t04Has(t, runtime, "Some(count) if raw != 0 && raw <= u64::from(*count) => {", "R20 the ordinal is resolved through the plan's remap")
	t04Has(t, runtime, "u64::from(remap[base + raw as usize])", "R20 the older enum's ordinals are the reader's")
}

// ---- rust/R21 -----------------------------------------------------------------

// t04R21 holds rust/R21 to §4's float rung and §5.2's table: "widenf is the f32
// at src as an f64 at dst ... both are exact by construction, NaN payloads
// included". The hardware f64::from(f32) QUIETS a signalling NaN and can drop a
// payload bit, so the emitted runtime assembles every all-ones exponent by hand
// — the sign carried, f64's all-ones exponent, and the 23-bit mantissa shifted
// left by 29, which carries the quiet bit as the writer wrote it.
func t04R21(t *testing.T) {
	runtime := string(fixedRuntimeBody)
	t04Has(t, runtime, "bits & 0x7f80_0000 == 0x7f80_0000 && bits & 0x007f_ffff != 0", "R21 the all-ones exponent is handled on the bits")
	t04Has(t, runtime, "| 0x7ff0_0000_0000_0000", "R21 f64's all-ones exponent")
	t04Has(t, runtime, "<< 29", "R21 the 23-bit mantissa becomes the 52-bit one")
	t04Has(t, runtime, "f64::from(f32::from_bits(bits)).to_bits()", "R21 every other exponent converts exactly")
	if strings.Contains(runtime, "f64::from(f).to_le_bytes()") {
		t.Error("R21: the float rung still widens through the hardware conversion")
	}
}

// ---- rust/R29 -----------------------------------------------------------------

// t04R29 holds rust/R29 to the 65536 band and to bill §12.5's carried writer
// bounds. THE BAND: a record body AT 65536 is a fixed root and one byte past it
// is not (SPEC-TABLES §3.4, "65536 BYTES OF RECORD BODY: THE FORM IS NOT
// EMITTED, AND THE TABLE IS NAMED"), and a layout entry reaching past the
// writer's own declared record refuses the plan WHOLE under
// layout_record_too_large. THE WRITER'S BOUNDS: the count op clamps to the
// writer's element bound carried by the plan and the text op to the writer's
// span, so a value forged past what the WRITER could have written clamps and
// counts while a value the reader merely widened does not.
func t04R29(t *testing.T) {
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
	runtime := string(fixedRuntimeBody)
	t04Has(t, runtime, "if root.size == 0 || root.size > 65536 {", "R29 the reader holds the peer's layout to the same ceiling")
	t04Has(t, runtime, "if self.record != 0 && end > self.record as u64 {", "R29 an entry past the writer's declared record")
	t04Has(t, runtime, "let held = raw.clamp(0, p.size as i32);", "R29 the count clamps to the writer's bound")
	t04Has(t, runtime, "let held = raw.clamp(0, p.aux as i32);", "R29 the text clamps to the writer's span")
	t04Has(t, runtime, "size: their_n.min(my_n),", "R29 the count's bound is the writer's")
	t04Has(t, runtime, "aux: bytes.checked_div(unit).unwrap_or(0),", "R29 the text's cap is the writer's span in units")
}

// ---- rust/E5 ------------------------------------------------------------------

// t04E5 holds rust/E5 to SPEC-TABLES §3.4's kind 35: "ON THIS FORM THEY ARE
// ONE BYTE APART ... the edit reads as kind_mismatch and the field takes its
// declared default", so `?T` and a plain `T` nesting are distinguishable in the
// layout, the wrapper is one present byte plus its child, and `T` into `?T`
// lands a CONSTANT present 1 under the row's own guard.
func t04E5(t *testing.T) {
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
	runtime := string(fixedRuntimeBody)
	t04Has(t, runtime, "35 => {", "E5 the optional wrapper is a layout kind")
	t04Has(t, runtime, "if e.size as u64 != sum + 1 {", "E5 the wrapper is one present byte plus its child")
	t04Has(t, runtime, "if me.kind == 35 && te.kind != 35 {", "E5 T into ?T is its own emit")
	t04Has(t, runtime, "aux: 1,", "E5 the present companion lands a constant 1")
}

// ---- rust/W2 ------------------------------------------------------------------

// t04W2 holds rust/W2 to SPEC-TABLES §3.4's present rule: "the payload rides
// WHOLE whether or not it is present; ZERO on write when the flag is 0". The
// emitted write body stores the flag and then the payload behind ONE `if`, so
// an absent optional costs the branch and not one store — the template's zeros
// are what rides, and a caller's untouched payload storage never reaches the
// wire.
func t04W2(t *testing.T) {
	text, _ := t04Lineage(t,
		"package probe\n\nfixed table Lineage\n{\n    link int32\n}\n",
		"package probe\n\nfixed table Lineage\n{\n    link ?int32\n}\n")
	write := t04Fn(text, "lineage_fixed_write_body")
	if write == "" {
		t.Fatal("W2: lineage_fixed_write_body is not emitted")
	}
	t04Has(t, write, "u8::from(value.link_present);", "W2 the present flag is stored")
	guard := strings.Index(write, "if value.link_present {")
	if guard < 0 {
		t.Fatalf("W2: an absent optional does not guard its store:\n%s", write)
	}
	// THE PAYLOAD'S STORE IS BEHIND THE GUARD and never before it: the first
	// payload byte written must come after the `if`.
	rest := write[guard:]
	if end := strings.Index(rest, "\n    }"); end >= 0 {
		rest = rest[:end]
	}
	if !strings.Contains(rest, "copy_from_slice") && !strings.Contains(rest, "b[") {
		t.Errorf("W2: the guard does not wrap the payload's store:\n%s", write)
	}
	if strings.Count(write, "if value.link_present {") != 1 {
		t.Errorf("W2: the absent-optional guard appears %d times, want once", strings.Count(write, "if value.link_present {"))
	}
}

// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapT04Versions(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "rust/R3", check: t04R3},
		{id: "rust/R32", check: t04R32},
		{id: "rust/R10", check: t04R10},
		{id: "rust/R15", check: t04R15},
		{id: "rust/E3/other-required-widens", check: t04E3},
		{id: "rust/C8", check: t04C8},
		{id: "rust/R20", check: t04R20},
		{id: "rust/R21", check: t04R21},
		{id: "rust/R29", check: t04R29},
		{id: "rust/E5", check: t04E5},
		{id: "rust/W2", check: t04W2},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
