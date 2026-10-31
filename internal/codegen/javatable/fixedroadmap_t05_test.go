package javatable

// fixedroadmap_t05 — the java leg's scalar-bounds, ordinals, normalization and
// array-bounds rows of docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW
// Fixed Tables"), the assertions no other test of this leg makes. Law:
// docs/FIXED-FORM-ALGORITHM.md §4.1 (the full-width lanes), §4.5 (the scatter),
// §4.6 (the bounds pass), §5.1 (the compressed float), §5.8 (the remap table),
// docs/SPEC-TABLES.md §3.4, docs/FIXED-FORM-VERSIONING-TESTS.md. One subtest per
// task id, and each subtest asserts the clause its task's own title states.
//
// A clause already held by an existing test of this leg is named by that test in
// the card's verdicts and is not repeated here; this file carries the direct
// assertion of every clause that had none. [owed] tasks are asserted here
// red-first; [weak] tasks get the direct assertion of every clause; [verify]
// tasks prove the holder RUNS and BITES.
//
// The assertion is read off the EMITTED Java source this leg writes — the
// straight-line scatter the emitter writes and the static plan's lanes — the way
// fixedroadmap_t02 reads the plan and the digest. The compiled probes this
// leg's own toolchain builds and runs are the runtime half for the two clauses
// whose bite is a VALUE (java/R30's rides-as-the-float) and not a literal.

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// t05Files is every emitted file of one schema, keyed by file name.
func t05Files(t *testing.T, schema string) map[string]string {
	t.Helper()
	u := versioningSchemaUnit(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate(%s): %v", schema, err)
	}
	out := map[string]string{}
	for name, src := range files {
		out[name] = string(src)
	}
	return out
}

// t05Has is a required substring of one emitted file, named by rule and by the
// page sentence the clause implements.
func t05Has(t *testing.T, files map[string]string, file, want, what string) {
	t.Helper()
	src, ok := files[file]
	if !ok {
		t.Fatalf("%s: the emitter wrote no %s", what, file)
	}
	if !javaCarries(src, want) {
		t.Errorf("%s: %s does not carry %q", what, file, want)
	}
}

func TestFixedRoadmapJavaT05Bounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"java/C1", t05C1},
		{"java/C2", t05C2},
		{"java/C7", t05C7},
		{"java/C9", t05C9},
		{"java/C10", t05C10},
		{"java/C12", t05C12},
		{"java/C13", t05C13},
		{"java/R17", t05R17},
		{"java/R30", t05R30},
		{"java/R31", t05R31},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t05C1 is C1 "count clamp v<0": ALGORITHM §4.5's `count` op, "if v < 0 then
// v := 0, COUNT clamped". The emitted scatter reads a count into a local and
// clamps the negative end to 0 before it walks.
func t05C1(t *testing.T) {
	files := t05Files(t, "VNEW_array_bounded_grow.schema")
	t05Has(t, files, "ArrayBoundedGrowFixed.java",
		"if (n < 0) { n = 0; r.clamped++; }",
		"C1 the negative end of the count clamp")
}

// t05C2 is C2 "count clamp v>Max": the same op's "else if v > size then v :=
// size, COUNT clamped", against the bound THE DECLARATION CARRIES — the OLD
// unit's `[..4]` and the NEW unit's `[..8]` — never a bound of the endpoint's.
func t05C2(t *testing.T) {
	old := t05Files(t, "VOLD_array_bounded_grow.schema")
	t05Has(t, old, "ArrayBoundedGrowFixed.java",
		"else if (n > 4) { n = 4; r.clamped++; }",
		"C2 the high end at the OLD declaration's bound 4")
	newer := t05Files(t, "VNEW_array_bounded_grow.schema")
	t05Has(t, newer, "ArrayBoundedGrowFixed.java",
		"else if (n > 8) { n = 8; r.clamped++; }",
		"C2 the high end at the NEW declaration's bound 8")
}

// t05C7 is C7 "ranged scalar clamp": ALGORITHM §4.6, "a RANGED SCALAR clamps to
// the min and max the plan carries, COUNT clamped". The emitted scatter clamps
// both ends of a `int32 | min = 0, max = 200` field to its own declaration.
func t05C7(t *testing.T) {
	files := t05Files(t, "VNEW_range_widen.schema")
	t05Has(t, files, "RangeWidenFixed.java",
		"if (q < 0L) { q = 0L; r.clamped++; } else if (q > 200L) { q = 200L; r.clamped++; }",
		"C7 the ranged scalar clamps to its declared min and max")
}

// t05C9 is C9 "union tag past arms → None": ALGORITHM §4.6, "a UNION TAG past
// the plan's arm count ... lands None and COUNT clamped". The emitted union
// scatter reads the tag and lands `none` (the union's own 0) past the last arm.
func t05C9(t *testing.T) {
	files := t05Files(t, "VNEW_union_append.schema")
	t05Has(t, files, "PickFixed.java",
		"if (v.type > 3) { v.type = none; r.clamped++; }",
		"C9 a tag past the arm count lands None")
}

// t05C10 is C10 "enum ordinal past top → None": the emitted enum scatter reads
// the ordinal through a wide temporary and lands position 0, `None`, past the
// last variant.
func t05C10(t *testing.T) {
	files := t05Files(t, "VNEW_enum_append.schema")
	t05Has(t, files, "LineageFixed.java",
		"long q = (b[at + 0] & 0xFF);",
		"C10 the enum ordinal read into a temporary")
	t05Has(t, files, "LineageFixed.java",
		"if (q > 4L) { q = 0L; r.clamped++; }",
		"C10 an ordinal past the top variant lands None")
}

// t05C12 is C12 "bool byte != 0": ALGORITHM §4.5, "a bool ... lands as `byte !=
// 0`, normalised to the language's own true. 0x02 is not a bool a reader stores
// verbatim". The emitted scatter writes the field from `!= 0`.
func t05C12(t *testing.T) {
	files := t05Files(t, "VNEW_hostile_bool.schema")
	t05Has(t, files, "HbbFixed.java",
		"v.flag = b[at + 0] != 0;",
		"C12 a bool byte lands as `byte != 0`")
}

// t05C13 is C13 "present flag byte != 0": the same rule for an optional's
// present flag, which rides as one byte and lands by the same `!= 0`.
func t05C13(t *testing.T) {
	files := t05Files(t, "VNEW_hostile_bool.schema")
	t05Has(t, files, "HbbFixed.java",
		"v.linkPresent = b[at + 1] != 0;",
		"C13 a present flag byte lands as `byte != 0`")
}

// t05R17 is R17 "a bool or present byte that is not 0/1 normalises and counts
// nothing", asserted on the VALUE and the COUNTERS a real read lands, never on
// the source text. ALGORITHM §4.5: "a bool ... lands as `byte != 0`, normalised
// to the language's own true. 0x02 is not a bool a reader stores verbatim".
// The leg's own OLD writer saves one Hbb record; the scalar `flag` byte, the
// optional's present byte and one `flags` element are forged to 0x02 at the
// body offsets §2 and §3 fix (the 20-byte header, the layout of LE(4, b+16)
// bytes, the record's u64 hash, then the body IN DECLARED ORDER); and both
// readers take the forged file: the OLD build by identity and the NEW build
// through a plan COMPILED from the OLD layout. Each must land exactly what it
// lands from the unforged file — `true` — and move no counter.
func t05R17(t *testing.T) {
	pkgOld := javaPackageOf(versioningSchemaUnit(t, "VOLD_hostile_bool.schema"))
	if pkgOld == "" {
		t.Fatal("R17: the OLD side declares no package")
	}
	classes := t03Build(t, t.TempDir(), "hostile_bool", []sideSpec{
		{key: "reads", schema: "VNEW_hostile_bool.schema", older: []string{"VOLD_hostile_bool.schema"}},
		{key: "refuses", schema: "VOLD_hostile_bool.schema"},
	}, map[string]string{
		"Probe_capability.java": t03SaveLoadProbe("Probe_capability", pkgOld, "Hbb", `
        vals[0].flag = true;
        vals[0].linkPresent = true;
        vals[0].flags[3] = true;`),
	})
	clean := t03BytesOf(t, t03RunSaveLoad(t, classes, "Probe_capability"))
	raw, err := os.ReadFile(clean)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 20 {
		t.Fatalf("R17: the saved file is %d bytes, shorter than the header", len(raw))
	}
	// §2: the layout's byte length is LE(4, b+16); §2's record is the u64 hash
	// then the body; §3's body is the fields in declared order at their
	// constant sizes: flag @0, link's present @1, link.n @2, lead @6,
	// flags @10, trail @18.
	body := 20 + int(binary.LittleEndian.Uint32(raw[16:20])) + 8
	if body+22 != len(raw) || raw[body] != 1 || raw[body+1] != 1 || raw[body+13] != 1 ||
		binary.LittleEndian.Uint32(raw[body+6:]) != 1 || binary.LittleEndian.Uint32(raw[body+18:]) != 2 {
		t.Fatalf("R17: the saved record is not the declared-order body this test forges (body at %d of %d): % x", body, len(raw), raw[min(body, len(raw)):])
	}
	forged := append([]byte(nil), raw...)
	forged[body] = 0x02    // flag
	forged[body+1] = 0x02  // link's present byte
	forged[body+13] = 0x02 // flags[3]
	path := filepath.Join(t.TempDir(), "hostile_bool.bin")
	if err := os.WriteFile(path, forged, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, probe := range []string{"Probe_refuses", "Probe_reads"} {
		want := runProbe(t, classes, probe, clean)
		got := runProbe(t, classes, probe, path)
		if got.n != 1 || got.refused || got.malformed {
			t.Fatalf("R17 %s: the forged 0x02 bytes must read, never damage: %+v", probe, got)
		}
		// NORMALISES: 0x02 lands as the language's own true.
		for _, k := range []string{"flag", "linkPresent"} {
			if got.value[k] != "true" {
				t.Errorf("R17 %s: %s landed %q from a 0x02 byte, want true — a bool lands as `byte != 0`", probe, k, got.value[k])
			}
		}
		// ... and exactly as the lawful 0x01 lands: every field of the record.
		for k, v := range want.value {
			if got.value[k] != v {
				t.Errorf("R17 %s: %s landed %q from the forged file, want %q as from the lawful one", probe, k, got.value[k], v)
			}
		}
		// COUNTS NOTHING: a non-0/1 bool or present byte is no clamp and no
		// damage, so every counter stands at zero.
		if got.clamped != 0 || got.widened != 0 || got.unknown != 0 || got.kindMismatch != 0 {
			t.Errorf("R17 %s: a 0x02 bool or present byte moved a counter: clamped=%d widened=%d unknown=%d kindMismatch=%d, want all 0",
				probe, got.clamped, got.widened, got.unknown, got.kindMismatch)
		}
	}
}

// t05R30 is R30 "compressed float rides as the float: min/max/resolution are
// definitions, the step is in the digest under 'Q', finer widens, coarser
// refuses by name, dropping the triple widens, fp-contract off on every leg".
//
// The leg's own clauses are asserted here: the writer stores the raw float bits
// and never a quantized index (SPEC-TABLES §3.4: "it rides as the float, not as
// a quantized index"), the reader lands those bits whole and holds the READER's
// own bounds, and the resolution sits in the definitions digest under 'Q' — a
// refined step moves the wire hash and never the record. The BASELINE clauses
// (finer widens, coarser refuses by name, dropping the triple widens) are held
// by internal/lockfile/cfloatrange_test.go, and the rides-as-the-float VALUE is
// proved end to end by the leg's own writer and reader.
func t05R30(t *testing.T) {
	files := t05Files(t, "VNEW_cfloat_range_widen.schema")
	// RIDES AS THE FLOAT: the write is the raw bits, never a quantized index.
	t05Has(t, files, "CfloatRangeWidenFixed.java",
		"Float.floatToRawIntBits(v.aim)",
		"R30 the writer stores the raw float32 bits, never a quantized index (SPEC-TABLES §3.4)")
	t05Has(t, files, "CfloatRangeWidenFixed.java",
		"final int fbits = TableFixed.get32(b, at + 4);",
		"R30 the reader reads the float32's raw bits whole before its bounds pass")
	t05Has(t, files, "CfloatRangeWidenFixed.java",
		"r.refuseNonFinite(\"CfloatRangeWiden.aim\");",
		"R30 a NaN or an infinity is refused by name before the bounds pass, never clamped (SPEC-TABLES §3.3)")
	t05Has(t, files, "CfloatRangeWidenFixed.java",
		"float q = Float.intBitsToFloat(fbits);",
		"R30 the reader lands the float32 whole before its bounds pass")
	// THE READER'S OWN BOUNDS, at the widened range's min and max.
	t05Has(t, files, "CfloatRangeWidenFixed.java",
		"Float.intBitsToFloat(0xc0000000)",
		"R30 the bounds pass holds the reader's own min (-2.0)")
	t05Has(t, files, "CfloatRangeWidenFixed.java",
		"Float.intBitsToFloat(0x40000000)",
		"R30 the bounds pass holds the reader's own max (2.0)")

	// THE STEP IS IN THE DIGEST UNDER 'Q' (bill §13): refining the resolution
	// moves the wire hash and never the record, and the range moves the hash
	// independently of the step — min, max and resolution are definitions.
	oldRes := versioningSchemaUnit(t, "VOLD_cfloat_res_refine.schema")
	older, ok := FixedLineageOf(oldRes, "CfloatResRefine")
	if !ok {
		t.Fatal("R30: no lineage entry for CfloatResRefine (old)")
	}
	fineRes := versioningSchemaUnit(t, "VNEW_cfloat_res_refine.schema")
	finer, ok := FixedLineageOf(fineRes, "CfloatResRefine")
	if !ok {
		t.Fatal("R30: no lineage entry for CfloatResRefine (new)")
	}
	if older.Wire == finer.Wire {
		t.Error("R30: refining the resolution did not move the wire hash — the step is not in the digest under 'Q'")
	}
	if older.Record != finer.Record {
		t.Errorf("R30: the record size moved on a resolution change (%d -> %d) — the float rides whole and min/max/resolution are definitions, never record bytes", older.Record, finer.Record)
	}
	oldRange := versioningSchemaUnit(t, "VOLD_cfloat_range_widen.schema")
	oRange, _ := FixedLineageOf(oldRange, "CfloatRangeWiden")
	newRange := versioningSchemaUnit(t, "VNEW_cfloat_range_widen.schema")
	nRange, _ := FixedLineageOf(newRange, "CfloatRangeWiden")
	if oRange.Wire == nRange.Wire {
		t.Error("R30: widening the range did not move the wire hash — the range is not in the digest under 'R'")
	}
	if oRange.Record != nRange.Record {
		t.Errorf("R30: the record size moved on a range widening (%d -> %d) — the range is a definition, never a record byte", oRange.Record, nRange.Record)
	}

	// THE VALUE, END TO END: the leg's own writer saves an off-grid float32 and
	// its own reader lands the same bits, no clamp — a writer that quantized by
	// the packet wire's rule would move 0.3 off its grid.
	pkg := javaPackageOf(newRange)
	if pkg == "" {
		t.Fatal("R30: the NEW side declares no package")
	}
	classes := t03Build(t, t.TempDir(), "cfloat_range_widen", []sideSpec{
		{key: "reads", schema: "VNEW_cfloat_range_widen.schema", older: []string{"VOLD_cfloat_range_widen.schema"}},
		{key: "refuses", schema: "VOLD_cfloat_range_widen.schema"},
	}, map[string]string{
		"Probe_capability.java": t03SaveLoadProbe("Probe_capability", pkg, "CfloatRangeWiden", `
        vals[0].lead = 1;
        vals[0].aim = Float.intBitsToFloat(0x3E99999A); // 0.3, OFF the 0.1 grid
        vals[0].trail = 2;`),
	})
	out := t03RunSaveLoad(t, classes, "Probe_capability")
	r := parseProbe(t, out)
	if r.n != 1 || r.refused || r.malformed {
		t.Fatalf("R30: the round trip did not read: %+v", r)
	}
	if got := parseDumpedFloatBits(t, r.value["aim"]); got != 0x3E99999A {
		t.Errorf("R30: aim landed %#x, want the writer's own bits 0x3E99999A — the float rides whole, never requantized", got)
	}
	if r.clamped != 0 {
		t.Errorf("R30: clamped=%d, want 0 — an off-grid value inside the reader's range rides whole and counts nothing", r.clamped)
	}
}

// t05R31 is R31 "full-width lanes: arg/arg2 full width in the emitted static
// plan with the compare at the tag's own width; a 64-bit ordinal temporary; the
// remap table as long as the WRITER's variant count, refusing by name past the
// length word". Every clause is a fact about the emitted runtime's own source,
// so it is pinned there: the guard's ordinal lane is a long AND SO IS arg2 (the
// OUTER tag an arm inside an arm also answers to, ALGORITHM §4.1), the guard
// width rides a lane of its own and the compare is made at it on BOTH lanes,
// the ordinal is read through a 64-bit temporary, the remap table carries its
// length in slot 0, and past the uint16 length word the plan refuses by name.
func t05R31(t *testing.T) {
	files := t05Files(t, "VNEW_enum_width.schema")
	src, ok := files["TableFixed.java"]
	if !ok {
		t.Fatal("R31: the emitter wrote no TableFixed.java")
	}
	// FULL-WIDTH LANES: arg is a long, never a byte lane, and so is arg2 — the
	// OUTER tag an arm inside an arm also answers to (ALGORITHM §4.1: "arg IS
	// FULL WIDTH AND SO IS arg2: no byte lane anywhere").
	for _, want := range []string{
		"public long arg;",
		"public byte argw;",
		"public int guard2 = noGuard;",
		"public long arg2;",
		"public byte argw2;",
	} {
		if !javaCarries(src, want) {
			t.Errorf("R31: the plan's lanes are not full width — %q is not emitted", want)
		}
	}
	// THE COMPARE AT THE TAG'S OWN WIDTH, never on a prefix, on BOTH guard
	// lanes: the inner tag at argw and the OUTER tag at argw2, CONJOINED (an
	// entry runs when both hold their ordinal, §4.1).
	for _, want := range []string{
		"final int gw = guardWidth(p.argw);",
		"if (tagAt(src, srcAt + p.guard, gw) != p.arg) { continue; }",
		"final int gw2 = guardWidth(p.argw2);",
		"if (tagAt(src, srcAt + p.guard2, gw2) != p.arg2) { continue; }",
	} {
		if !javaCarries(src, want) {
			t.Errorf("R31: the guard compare is not at the tag's own width — %q is not emitted", want)
		}
	}
	// THE CONJOINED LANE IS STAMPED ON THE COMPILED WALK: a nested union's arm
	// entries answer to the OUTER tag on the second lane, laid from the guard
	// chain's innermost two, and a third nested union rides the chain's pool
	// rather than drop a condition (§4.1, the guard chain; §5.8 row 6).
	for _, want := range []string{
		"e.guard2 = chainGuard[chainLen - 2];",
		"e.arg2 = chainArg[chainLen - 2];",
		"e.argw2 = chainArgw[chainLen - 2];",
		"if (chainAt < 0) { chainAt = layGuards(); }",
		"if (p.gcount > 2 && !chainHolds(p, src, srcAt, srcLength, report)) {",
	} {
		if !javaCarries(src, want) {
			t.Errorf("R31: the outer tag is not conjoined on a second guard lane — %q is not emitted", want)
		}
	}
	if strings.Contains(src, "c.recordTooLarge = true; break; }") {
		t.Error("R31: a third nested union is still refused instead of riding the guard chain")
	}
	// A 64-BIT ORDINAL TEMPORARY (ALGORITHM §4.5: "through a 64-bit temporary").
	if !strings.Contains(src, "final long raw = getUint(src, srcAt + p.src, p.size);") {
		t.Error("R31: the ordinal is not read through a 64-bit temporary")
	}
	// THE REMAP TABLE'S LENGTH WORD, and the table laid as long as the writer's
	// variant count (ALGORITHM §5.8 row 14: "as long as the WRITER's variant
	// count, with a length word"). Past the uint16 length word the plan refuses
	// by name rather than answering a wrong value (a truncated length word
	// would silently remap the 65536th variant to None).
	for _, want := range []string{
		"final int n = remap[p.aux] & 0xFFFF;",
		"remap[at] = (short) n;",
		"if (n < 0 || n > 65535) { overflow = true; return 0; }",
	} {
		if !javaCarries(src, want) {
			t.Errorf("R31: the remap table's length word is not emitted — %q is not present", want)
		}
	}
	// THE WIDE ORDINAL END TO END: the 256th variant, whose ordinal the old
	// one-byte width could not hold, rides a two-byte ordinal and lands exactly
	// through the leg's own writer and reader.
	pkg := javaPackageOf(versioningSchemaUnit(t, "VNEW_enum_width.schema"))
	if pkg == "" {
		t.Fatal("R31: the NEW side declares no package")
	}
	classes := t03Build(t, t.TempDir(), "enum_width", []sideSpec{
		{key: "reads", schema: "VNEW_enum_width.schema", older: []string{"VOLD_enum_width.schema"}},
		{key: "refuses", schema: "VOLD_enum_width.schema"},
	}, map[string]string{
		"Probe_capability.java": t03SaveLoadProbe("Probe_capability", pkg, "Lineage", `
        vals[0].tier = 256;
        vals[0].seq = 14;`),
	})
	out := t03RunSaveLoad(t, classes, "Probe_capability")
	r := parseProbe(t, out)
	if r.n != 1 || r.refused || r.malformed {
		t.Fatalf("R31: the round trip did not read: %+v", r)
	}
	if got := r.value["tier"]; got != "256" {
		t.Errorf("R31: tier landed %s, want 256 — the 256th variant rides the full-width ordinal and lands exactly", got)
	}
	if r.clamped != 0 || r.widened != 0 {
		t.Errorf("R31: a clean wide-ordinal round trip moved a counter: clamped=%d widened=%d", r.clamped, r.widened)
	}
}
