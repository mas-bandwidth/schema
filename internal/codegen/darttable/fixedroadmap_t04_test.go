package darttable

// THE ROADMAP'S VERSIONING TASKS ON THE DART LEG (docs/roadmap.sexp node
// `fixed-tables`; docs/FIXED-FORM-ALGORITHM.md §5.2 to §5.7;
// docs/FIXED-FORM-VERSIONING-TESTS.md "The floor and the hash" and
// "The old contract's tests, retired by name"; docs/SPEC-TABLES.md §3.4 and
// §21.2). One subtest per task id, table-driven, t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE DART BYTES THIS LEG EMITS and the shared
// runtime body it emits them with, so the harness is reachable on a tree that
// never built the C++ reference corpus and never carries the Dart SDK. A clause
// that needs the emitted code to RUN is asserted at the emitted site that runs
// it, and that site is named in the check — the shape fixedroadmap_t02_test.go
// and fixedroadmap_t03_test.go state for the same reason.
//
// THE FLOOR'S OWN RUNTIME IS ALREADY HELD by fixedversioning_test.go's
// TestFixedVersioningFloor/{floor_at,floor_below,floor_raise_live} probes,
// which a gate runs (make tables-dart-versioning). The subtests below carry
// the clauses those probes cannot read off the bytes: the floor's ARITHMETIC
// in the emitter, and the refusal's position and purity in the emitted load.
//
// dart/R3 [verify]: the floor is 1 + the highest retired index (0 when none);
// below the floor is layout_unsupported, reporting the file's hash.
//
// dart/R32 [weak]: retire for real: a retired version is refused by name,
// once, idempotently.
//
// dart/R10 [weak]: the run-time walk of a stranger's layout and the recompute
// of the header's hash are retired.
//
// dart/R15 [verify]: the four forward-read clamps are retired — count clamp
// across bounds, range clamp across versions, remap of an unknown variant to
// None, drop-and-count of an unknown field: each is layout_newer now.
//
// dart/E3/other-required-widens [capability]: all other required widen-ladder
// cases.
//
// dart/C8 [owed]: fixed-point F-shift / bits(N).
//
// dart/R20 [weak]: §21.2's landing rules.
//
// dart/R21 [owed]: widenf is the bit-exact widening — signalling NaNs kept, the
// quiet bit carried as the writer wrote it.
//
// dart/R29 [owed]: the band case: the widening across the 65536 ceiling, and
// the bounds pass clamping to the writer's bounds.
//
// dart/E5 [verify]: ?T vs plain nesting.
//
// dart/W2 [verify]: absent optional skips store.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- fixtures and readers of this leg's output ------------------------------

// t04Fixed is the emitted fixed module of a unit (the one *Fixed.dart the table
// backend produced).
func t04Fixed(t *testing.T, tables map[string][]byte) string {
	t.Helper()
	for name, data := range tables {
		if strings.HasSuffix(name, "Fixed.dart") {
			return string(data)
		}
	}
	t.Fatalf("no *Fixed.dart module was emitted")
	return ""
}

// t04Fn is the emitted body of one Dart function, up to its column-zero
// closing brace.
func t04Fn(text, name string) string {
	at := strings.Index(text, name+"(")
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
// emitted module together with the floor it published.
func t04Retire(t *testing.T, retired int) (text string, floor int) {
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
	return t04Fixed(t, files), retired
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

// t04HexBytes reads one emitted `const <int>[ ... ]` byte run by its opening
// marker, so a value assertion names the constant and not a random hex byte.
func t04HexBytes(t *testing.T, text, marker string) []byte {
	t.Helper()
	at := strings.Index(text, marker)
	if at < 0 {
		t.Fatalf("the emitted source carries no %q", marker)
	}
	rest := text[at+len(marker):]
	end := strings.Index(rest, "]);")
	if end < 0 {
		t.Fatalf("the emitted byte run after %q is not closed", marker)
	}
	var out []byte
	for field := range strings.FieldsSeq(rest[:end]) {
		field = strings.TrimSuffix(field, ",")
		if field == "" || !strings.HasPrefix(field, "0x") {
			continue
		}
		var v int
		for _, c := range field[2:] {
			v = v*16 + strings.IndexByte("0123456789abcdef", byte(c))
		}
		out = append(out, byte(v))
	}
	return out
}


// ---- dart/R3 -----------------------------------------------------------------

// t04R3 holds dart/R3 to §5.2's one number: "R.floor := 1 + the highest index
// marked RETIRED in lock.lineage(T), or 0 when none is", emitted beside the
// lineage, and to LOAD step 6: "if i < R.floor: REFUSE layout_unsupported,
// reporting h". The runtime half is TestFixedVersioningFloor/floor_below; the
// arithmetic and the emitted site are read here.
func t04R3(t *testing.T) {
	for _, tc := range []struct {
		retired int
		want    string
	}{
		{0, "const int lineageFixedFloor = 0;"},
		{1, "const int lineageFixedFloor = 1;"},
		{2, "const int lineageFixedFloor = 2;"},
	} {
		text, _ := t04Retire(t, tc.retired)
		t04Has(t, text, tc.want, "R3 1 + the highest retired index")
	}
	text, _ := t04Retire(t, 2)
	t04Has(t, text, "1 + the highest RETIRED index, 0 when none is", "R3 the emitter states its own arithmetic")
	load := t04Fn(text, "lineageFixedLoad")
	if load == "" {
		t.Fatal("R3: lineageFixedLoad is not emitted")
	}
	t04Has(t, load, "if (pick < lineageFixedFloor) {", "R3 the floor check")
	t04Has(t, load, "report.refused = TableFixedRefusal.layoutUnsupported;", "R3 below the floor is layout_unsupported")
	t04Has(t, load, "report.layoutHash = hash;", "R3 below the floor reports the file's hash")
	// NOTHING RETIRED is a floor of ZERO: the emitter still publishes the
	// number, and the comparison it writes is one no pick can satisfy because
	// the hash select already refused every pick below zero on the line above.
	none, _ := t04Retire(t, 0)
	noneLoad := t04Fn(none, "lineageFixedLoad")
	if !strings.Contains(noneLoad, "if (pick < 0) {") {
		t.Error("R3: the floor-0 reader carries no layout_newer select, so the floor check cannot be read against it")
	}
	if strings.Count(noneLoad, "if (pick < lineageFixedFloor) {") != 1 {
		t.Error("R3: a floor of 0 must still emit the one comparison, whose pick is never below it")
	}
}


// ---- dart/R32 ----------------------------------------------------------------

// t04R32 holds dart/R32 to §5.2's retirement, read off the emitted load: the
// operator's mark is laid down per retired entry, the floor check sits ONCE,
// after the hash select and BEFORE the byte comparison or any record, and its
// whole body is the named refusal — a pure read of `pick`, so a second load of
// the same file answers the same name. REFUSE is TOTAL: the report is reset at
// the top of the load, so no counter survives into the refusal.
func t04R32(t *testing.T) {
	text, _ := t04Retire(t, 1)
	t04Has(t, text, "// RETIRED: the operator retired this layout", "R32 the retired entry's mark is laid down")
	// THE CURRENT ENTRY CARRIES NO MARK: the third generation is not retired.
	if strings.Count(text, "// RETIRED:") != 1 {
		t.Errorf("R32: the module carries %d retired marks, want the one the lock retired", strings.Count(text, "// RETIRED:"))
	}
	load := t04Fn(text, "lineageFixedLoad")
	if load == "" {
		t.Fatal("R32: lineageFixedLoad is not emitted")
	}
	if n := strings.Count(load, "if (pick < lineageFixedFloor) {"); n != 1 {
		t.Errorf("R32: the floor is tested %d times, want once", n)
	}
	at := strings.Index(load, "if (pick < lineageFixedFloor) {")
	known := strings.Index(load, "final known = lineageFixedKnown[pick];")
	bytes := strings.Index(load, "if (layoutBytes != known.layoutBytes) {")
	records := strings.Index(load, "for (var k = 0; k < n; k++) {")
	if at < 0 || known < 0 || bytes < 0 || records < 0 {
		t.Fatalf("R32: the load's shape moved (%d, %d, %d, %d)", at, known, bytes, records)
	}
	if !(at < known && known < bytes && bytes < records) {
		t.Errorf("R32: the floor check is not before the byte comparison and the record loop (%d, %d, %d, %d)", at, known, bytes, records)
	}
	// ONCE AND IDEMPOTENT: the guarded body reads `pick` and returns the named
	// refusal; nothing before it mutates the report or the destination.
	body := load[at:]
	if end := strings.Index(body, "\n  }\n"); end >= 0 {
		body = body[:end]
	}
	if strings.Contains(body, "report.unknown") || strings.Contains(body, "plan.image") || strings.Contains(body, "values[") {
		t.Errorf("R32: the floor refusal is not a pure named refusal:\n%s", body)
	}
	for _, zero := range []string{
		"refused = TableFixedRefusal.none;",
		"unknown = 0;",
		"kindMismatch = 0;",
		"widened = 0;",
		"clamped = 0;",
		"malformed = false;",
		"layoutHash = 0;",
	} {
		t04Has(t, fixedRuntime, zero, "R32 REFUSE is TOTAL")
	}
}


// ---- dart/R10 -----------------------------------------------------------------

// t04R10 holds dart/R10 to §5.6 and §5.3 step 7: "the hash is looked up, the
// floor is checked, and the layout is COMPARED — the seven rules do not fire at
// run time". The emitted load parses no layout (the lock's bytes were parsed by
// tableFixedLineagePlans, built once and never again), walks no stranger's
// bytes, and never recomputes the header's hash: the header's eight bytes are
// taken as given and matched on.
func t04R10(t *testing.T) {
	text, _ := t04Retire(t, 0)
	load := t04Fn(text, "lineageFixedLoad")
	if load == "" {
		t.Fatal("R10: lineageFixedLoad is not emitted")
	}
	t04Has(t, load, "if (layoutBytes != known.layoutBytes) {", "R10 the layout's length is a byte comparison")
	t04Has(t, load, "bytes[TableFixedLimits.layoutAt + i] != known.layout[i]", "R10 the layout's bytes are compared")
	t04Has(t, load, "report.refused = TableFixedRefusal.layoutMalformed;", "R10 a lie about a known version is layout_malformed")
	if strings.Contains(load, ".parse(") {
		t.Error("R10: the load walks a stranger's layout; the walk is the build's TableFixedLineagePlans")
	}
	if strings.Contains(load, "hashOf(") {
		t.Error("R10: the load recomputes the header's hash")
	}
	if strings.Contains(load, "checkEntry(") || strings.Contains(load, "knownKind(") || strings.Contains(load, "leafSize(") {
		t.Error("R10: the load carries §1.1's layout rules into the run time")
	}
	// THE HASH IS STILL A WIRE IDENTITY: the function survives for the writer
	// side and for any later reader, but no load path calls it.
	t04Has(t, fixedRuntime, "static int hashOf(Uint8List bytes, int at, int length) {", "R10 the hash function")
}


// ---- dart/R15 -----------------------------------------------------------------

// t04R15 holds dart/R15 to §5.6's retirement list and §5.3 step 5: the four
// forward-read clamps are gone, and each is `layout_newer` now. A hash the
// lineage does not hold is refused BEFORE any record and before any plan
// selection, on the file's hash; the count and text clamps that REMAIN are the
// writer's bounds carried by the plan (bill §12.5), which only a backward read
// over a locked entry can reach; and the unknown-field count lands from the
// COMPILE census after a returning read, never per record.
func t04R15(t *testing.T) {
	text, _ := t04Retire(t, 0)
	load := t04Fn(text, "lineageFixedLoad")
	if load == "" {
		t.Fatal("R15: lineageFixedLoad is not emitted")
	}
	miss := strings.Index(load, "report.refused = TableFixedRefusal.layoutNewer;")
	records := strings.Index(load, "for (var k = 0; k < n; k++) {")
	if miss < 0 || records < 0 {
		t.Fatalf("R15: the load's refusals moved (%d, %d)", miss, records)
	}
	if miss > records {
		t.Error("R15: an unknown hash is not refused before the first record")
	}
	if strings.Contains(load, "report.unknown++") {
		t.Error("R15: an unknown field is dropped-and-counted per record on the read path")
	}
	t04Has(t, load, "report.unknown += censusUnknown;", "R15 unknown is the compile census")
	t04Has(t, fixedRuntime, "counted > size", "R15 the count clamp is the writer's bound")
	t04Has(t, fixedRuntime, "used > cap", "R15 the text clamp is the writer's span")
	if strings.Contains(fixedRuntime, "remap.get(") {
		t.Error("R15: a forward-read remap survives in the runtime")
	}
}


// ---- dart/E3 ------------------------------------------------------------------

// t04E3 holds dart/E3 to §5.2's EMIT ladder, in full: "if LADDER(te.kind,
// me.kind): emit widenf when te is f32 else widen, sign := te.kind is a signed
// integer or a signed fixed-point". Every required rung is present — 2..5, 6..9,
// 20..24, 25..29 and 10 -> 11 — the float rung is widenFloat, the sign is the
// WRITER's kind, and a same-kind widen zero-extends. The non-default and
// boundary values are written and read by the gate's corpus rows
// (int_widen, uint_widen, float_widen, array_elem_widen, enum_width).
func t04E3(t *testing.T) {
	for _, rung := range []string{
		"if (from >= 6 && from <= 9 && to >= 6 && to <= 9) {",
		"if (from >= 2 && from <= 5 && to >= 2 && to <= 5) {",
		"if (from >= 20 && from <= 24 && to >= 20 && to <= 24) {",
		"if (from >= 25 && from <= 29 && to >= 25 && to <= 29) {",
		"return from == 10 && to == 11; // f32 -> f64",
	} {
		t04Has(t, fixedRuntime, rung, "E3 the widen ladder")
	}
	t04Has(t, fixedRuntime, "final op = theirKind == 10", "E3 the float rung is chosen by the writer's kind")
	t04Has(t, fixedRuntime, "TableFixedOp.widenFloat", "E3 the float rung is widenFloat")
	t04Has(t, fixedRuntime, "(mySize & 0xff) | (signedKind(theirKind) ? 0x100 : 0)", "E3 a ladder widen's sign is the writer's kind")
	t04Has(t, fixedRuntime, "static bool signedKind(int kind) =>", "E3 the sign helper is emitted")
}


// ---- dart/C8 ------------------------------------------------------------------

// t04C8 holds dart/C8 to §3.4's constant-size table and §5.2's fixed-point
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
	decode := t04Fn(t04Fixed(t, files), "lineageFixedDecode")
	if decode == "" {
		t.Fatal("C8: lineageFixedDecode is not emitted")
	}
	t04Has(t, decode, "if (value.e > 4095) {", "C8 bits(12)'s own ceiling")
	t04Has(t, decode, "// the bits(12) WIDTH clamp", "C8 the width clamp is the bits(N) rule")
	t04Has(t, fixedRuntime, "case 20: // fixed8", "C8 the fixed-point ladder's low rung")
	t04Has(t, fixedRuntime, "case 25: // ufixed8", "C8 the unsigned fixed-point family")
	t04Has(t, fixedRuntime, "case 24:", "C8 the fixed-point ladder's top rung")
	t04Has(t, fixedRuntime, "case 29:", "C8 the unsigned fixed-point ladder's top rung")
}


// ---- dart/R20 -----------------------------------------------------------------

// t04R20 holds dart/R20 to SPEC-TABLES §21.2, clause by clause, on the emitted
// sites: an added field lands the prefill's declared default; a deprecated field
// is dropped and counted once per plan through the compile census; a narrower
// integer or float is widened exactly and counts widened; a shorter array or
// string lands with the reader's slack as the template's zeros; an older enum's
// ordinals are the reader's, its list a prefix.
func t04R20(t *testing.T) {
	text, _ := t04Retire(t, 0)
	t04Has(t, text, "final Uint8List lineageFixedPrefill = Uint8List.fromList(const <int>[", "R20 the prefill image is emitted")
	t04Has(t, text, "tableFixedFillRun(fills, fillCount, lineageFixedPrefill, plan.image);", "R20 the prefill lands the holes the plan does not")
	// AN ADDED FIELD LANDS ITS DECLARED DEFAULT: the appended `w int32 = 77`
	// rides in the prefill as the writer's declared default, and the appended
	// field's four bytes are the only non-zero run of the image.
	added, _ := t04Lineage(t,
		"package probe\n\nfixed table Lineage\n{\n    x int32\n}\n",
		"package probe\n\nfixed table Lineage\n{\n    x int32\n    w int32 = 77\n}\n")
	prefill := t04HexBytes(t, added, "final Uint8List lineageFixedPrefill = Uint8List.fromList(const <int>[")
	if len(prefill) != 8 {
		t.Fatalf("R20: the added-field fixture's prefill is %d bytes, want the 8-byte body", len(prefill))
	}
	if prefill[4] != 0x4d || prefill[5] != 0 || prefill[6] != 0 || prefill[7] != 0 {
		t.Errorf("R20: the added field's bytes are % x, want 4d 00 00 00 — the declared default 77", prefill[4:8])
	}
	// A DEPRECATED FIELD, counted ONCE per plan: the census flag is the first
	// time the peer's field is seen, and an array's element 0 is the only one
	// that censuses.
	t04Has(t, fixedRuntime, "if (!named && census) {", "R20 an unknown field is counted")
	t04Has(t, fixedRuntime, "census && i == 0,", "R20 once per FIELD per peer, not per element")
	t04Has(t, text, "report.unknown += censusUnknown;", "R20 the census lands once, after the loop")
	// A NARROWER INTEGER OR FLOAT: widened exactly, and counted.
	t04Has(t, fixedRuntime, "report.widened++;", "R20 a widen counts")
	// A SHORTER ARRAY OR STRING: the reader's slack is the prefill's zeros, and
	// the plan writes only the writer's live extent.
	t04Has(t, text, "final Uint8List lineageFixedPrefill = Uint8List.fromList(const <int>[", "R20 the reader's slack is the template's zeros")
	// AN OLDER ENUM: the remap is by NAME and its list is the writer's count, so
	// a prefix maps position for position.
	t04Has(t, fixedRuntime, "raw > remap[remapAt]", "R20 the ordinal is resolved through the plan's remap")
	t04Has(t, fixedRuntime, "landed = remap[remapAt + raw];", "R20 the older enum's ordinals are the reader's")
}


// ---- dart/R21 -----------------------------------------------------------------

// t04R21 holds dart/R21 to §4's float rung and §5.2's table: "widenf is the f32
// at src as an f64 at dst ... both are exact by construction, NaN payloads
// included". The hardware f32->f64 conversion QUIETS a signalling NaN and can
// drop a payload bit, so the emitted runtime assembles every all-ones exponent
// by hand — f64's all-ones exponent and the 23-bit mantissa shifted left by 29,
// which carries the quiet bit as the writer wrote it.
func t04R21(t *testing.T) {
	t04Has(t, fixedRuntime, "(bits & 0x7f800000) == 0x7f800000 && (bits & 0x007fffff) != 0", "R21 the all-ones exponent is handled on the bits")
	t04Has(t, fixedRuntime, "0x7ff0000000000000", "R21 f64's all-ones exponent")
	t04Has(t, fixedRuntime, "<< 29", "R21 the 23-bit mantissa becomes the 52-bit one")
	t04Has(t, fixedRuntime, "sourceView.getFloat32(s, Endian.little)", "R21 every other exponent converts exactly")
	// THE BIT ASSEMBLY IS THE WIDENF OP THE TABLE NAMES, not a hardware
	// conversion dressed as one: the all-ones exponent is chosen before any
	// getFloat32 call in the op.
	at := strings.Index(fixedRuntime, "case TableFixedOp.widenFloat:")
	if at < 0 {
		t.Fatal("R21: the runtime has no widenFloat op")
	}
	rest := fixedRuntime[at:]
	if end := strings.Index(rest, "case TableFixedOp.constant:"); end >= 0 {
		rest = rest[:end]
	}
	if !strings.Contains(rest, "(bits & 0x7f800000) == 0x7f800000") {
		t.Error("R21: the widenFloat op widens through the hardware conversion alone")
	}
}


// ---- dart/R29 -----------------------------------------------------------------

// t04R29 holds dart/R29 to the 65536 band and to bill §12.5's carried writer
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
	t04Has(t, fixedRuntime, "size(0) == 0 || size(0) > TableFixedLimits.recordMaxBytes", "R29 the reader holds the peer's layout to the same ceiling")
	t04Has(t, fixedRuntime, "plan.record != 0 && end > plan.record", "R29 an entry past the writer's declared record")
	t04Has(t, fixedRuntime, "counted > size", "R29 the count clamps to the writer's bound")
	t04Has(t, fixedRuntime, "used > cap", "R29 the text clamps to the writer's span")
	// THE COUNT'S BOUND IS THE WRITER'S: the plan compiles the count op with
	// the WRITER's element count, never the reader's wider one.
	t04Has(t, fixedRuntime, "final theirN = theirElem != 0 ? (theirSize - head) ~/ theirElem : 0;", "R29 the count's bound is the writer's")
	t04Has(t, fixedRuntime, "final elems = theirN < myN ? theirN : myN;", "R29 the walk is the smaller of the two bounds")
}


// ---- dart/E5 ------------------------------------------------------------------

// t04E5 holds dart/E5 to SPEC-TABLES §3.4's kind 35: "ON THIS FORM THEY ARE
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
	t04Has(t, fixedRuntime, "case 35: // the OPTIONAL WRAPPER: one child, one present byte in front", "E5 the optional wrapper is a layout kind")
	t04Has(t, fixedRuntime, "if (mySize != sum + 1) {", "E5 the wrapper is one present byte plus its child")
	t04Has(t, fixedRuntime, "if (myKind == 35 && theirKind != 35) {", "E5 T into ?T is its own emit")
	t04Has(t, fixedRuntime, "image[d] = 1;", "E5 the present companion lands a constant 1")
}


// ---- dart/W2 ------------------------------------------------------------------

// t04W2 holds dart/W2 to SPEC-TABLES §3.4's present rule: "the payload rides
// WHOLE whether or not it is present; ZERO on write when the flag is 0". The
// emitted write body stores the flag and then the payload behind ONE `if`, so
// an absent optional costs the branch and not one store — the template's zeros
// are what rides, and a caller's untouched payload storage never reaches the
// wire.
func t04W2(t *testing.T) {
	text, _ := t04Lineage(t,
		"package probe\n\nfixed table Lineage\n{\n    link int32\n}\n",
		"package probe\n\nfixed table Lineage\n{\n    link ?int32\n}\n")
	write := t04Fn(text, "lineageFixedWriteBody")
	if write == "" {
		t.Fatal("W2: lineageFixedWriteBody is not emitted")
	}
	t04Has(t, write, "value.linkPresent ? 1 : 0", "W2 the present flag is stored")
	guard := strings.Index(write, "if (value.linkPresent) {")
	if guard < 0 {
		t.Fatalf("W2: an absent optional does not guard its store:\n%s", write)
	}
	// THE PAYLOAD'S STORE IS BEHIND THE GUARD and never before it: the first
	// payload byte written must come after the `if`.
	rest := write[guard:]
	if end := strings.Index(rest, "\n  }\n"); end >= 0 {
		rest = rest[:end]
	}
	if !strings.Contains(rest, "value.link, Endian.little)") {
		t.Errorf("W2: the guard does not wrap the payload's store:\n%s", write)
	}
	if strings.Count(write, "if (value.linkPresent) {") != 1 {
		t.Errorf("W2: the absent-optional guard appears %d times, want once", strings.Count(write, "if (value.linkPresent) {"))
	}
}



// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapDartT04Versions(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "dart/R3", check: t04R3},
		{id: "dart/R32", check: t04R32},
		{id: "dart/R10", check: t04R10},
		{id: "dart/R15", check: t04R15},
		{id: "dart/E3/other-required-widens", check: t04E3},
		{id: "dart/C8", check: t04C8},
		{id: "dart/R20", check: t04R20},
		{id: "dart/R21", check: t04R21},
		{id: "dart/R29", check: t04R29},
		{id: "dart/E5", check: t04E5},
		{id: "dart/W2", check: t04W2},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
