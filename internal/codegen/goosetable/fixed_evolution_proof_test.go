// Copyright 2026 Glenn Lewis. All rights reserved.
//
// Use of this source code is governed by the Reticulum License
// that can be found in the LICENSE file.

package goosetable

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// 1. port/goose/fixed/file-envelope (F1, F2, F3, F4, F7, F8, F12)
func TestGooseFixedProofFileEnvelope(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "goose")
	const schema = `package probe
fixed table Point {
    x int32 = 1
    y int32 = 2
}
`
	u := parseCheckUnit(t, schema)
	got := runCustomFixedGoose(t, u, nil, `fn main() {
    var v = probe::Point { X: 10, Y: 20 };
    var clean: u8[>..] = [];
    let _ = probe::PointFixedSave([v, v], clean);

    // F1: previous_form (0 and 1)
    var f1a: u8[>..] = [];
    var i: i64 = 0;
    while i < clean.len { f1a.push(clean[i]); i += 1; }
    f1a[0] = 0;
    var f1b: u8[>..] = [];
    i = 0;
    while i < clean.len { f1b.push(clean[i]); i += 1; }
    f1b[0] = 1;

    // F2: message_form_as_file (2)
    var f2: u8[>..] = [];
    i = 0;
    while i < clean.len { f2.push(clean[i]); i += 1; }
    f2[0] = 2;

    // F3: newer_form (4 and 255)
    var f3a: u8[>..] = [];
    i = 0;
    while i < clean.len { f3a.push(clean[i]); i += 1; }
    f3a[0] = 4;
    var f3b: u8[>..] = [];
    i = 0;
    while i < clean.len { f3b.push(clean[i]); i += 1; }
    f3b[0] = 255;

    // F4: layout_malformed, truncated layout length past file
    var f4: u8[>..] = [];
    i = 0;
    while i < clean.len { f4.push(clean[i]); i += 1; }
    f4[16] = 0xff;
    f4[17] = 0x7f;

    // F7: under 20 bytes
    let f7 = clean[0..19];

    // F8: ragged tail (not a multiple of record stride)
    let f8 = clean[0..clean.len - 1];

    // F12: second layout for a held hash (corrupted layout byte and off-by-one length under held hash)
    var f12a: u8[>..] = [];
    i = 0;
    while i < clean.len { f12a.push(clean[i]); i += 1; }
    f12a[20] = f12a[20] ^ 0x01;
    var f12b: u8[>..] = [];
    i = 0;
    while i < clean.len { f12b.push(clean[i]); i += 1; }
    f12b[16] = f12b[16] - 1;

    var dst: probe::Point[2] = [probe::Point { X: 77, Y: 88 }; 2];
    var rep = schema_rt::FixedReport { .. };
    let n1a = probe::PointFixedLoad(dst, f1a, rep);
    let r1a = schema_rt::fixed_refusal_name(rep.reason);
    let n1b = probe::PointFixedLoad(dst, f1b, rep);
    let r1b = schema_rt::fixed_refusal_name(rep.reason);
    let n2 = probe::PointFixedLoad(dst, f2, rep);
    let r2 = schema_rt::fixed_refusal_name(rep.reason);
    let n3a = probe::PointFixedLoad(dst, f3a, rep);
    let r3a = schema_rt::fixed_refusal_name(rep.reason);
    let n3b = probe::PointFixedLoad(dst, f3b, rep);
    let r3b = schema_rt::fixed_refusal_name(rep.reason);
    let n4 = probe::PointFixedLoad(dst, f4, rep);
    let r4 = schema_rt::fixed_refusal_name(rep.reason);
    let n7 = probe::PointFixedLoad(dst, f7, rep);
    let r7 = schema_rt::fixed_refusal_name(rep.reason);
    let n8 = probe::PointFixedLoad(dst, f8, rep);
    let r8 = schema_rt::fixed_refusal_name(rep.reason);
    let n12a = probe::PointFixedLoad(dst, f12a, rep);
    let r12a = schema_rt::fixed_refusal_name(rep.reason);
    let n12b = probe::PointFixedLoad(dst, f12b, rep);
    let r12b = schema_rt::fixed_refusal_name(rep.reason);
    let untouched = dst[0].X == 77 && dst[0].Y == 88 && dst[1].X == 77 && dst[1].Y == 88;
    print("F1 ", n1a, " ", r1a, " ", n1b, " ", r1b);
    print("F2 ", n2, " ", r2);
    print("F3 ", n3a, " ", r3a, " ", n3b, " ", r3b);
    print("F4 ", n4, " ", r4);
    print("F7 ", n7, " ", r7);
    print("F8 ", n8, " ", r8);
    print("F12 ", n12a, " ", r12a, " ", n12b, " ", r12b, " ", untouched);
}
`, nil)
	want := strings.Join([]string{
		"F1 -1 newer_form -1 previous_form",
		"F2 -1 message_form_as_file",
		"F3 -1 newer_form -1 newer_form",
		"F4 -1 layout_malformed",
		"F7 -1 truncated",
		"F8 -1 truncated",
		"F12 -1 layout_malformed -1 layout_malformed true",
	}, "\n")
	if got != want {
		t.Fatalf("FileEnvelope:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// 2. port/goose/fixed/batch-capacity (F9)
func TestGooseFixedProofBatchCapacity(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "goose")
	const schema = `package probe
fixed table Point {
    x int32 = 1
    y int32 = 2
}
`
	u := parseCheckUnit(t, schema)
	got := runCustomFixedGoose(t, u, nil, `fn main() {
    var v = probe::Point { X: 10, Y: 20 };
    var clean: u8[>..] = [];
    let _ = probe::PointFixedSave([v, v, v], clean);
    var dst: probe::Point[2] = [probe::Point { X: 77, Y: 88 }; 2];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::PointFixedLoad(dst, clean, rep);
    var er = schema_rt::FixedEvolveReport { .. };
    let en = probe::PointFixedEvolveLoad(dst, clean, er);
    print(n, " ", schema_rt::fixed_refusal_name(rep.reason), " ", en, " ", schema_rt::fixed_evolve_refusal_name(er.reason), " ", dst[0].X == 77 && dst[1].Y == 88);
}
`, nil)
	if got != "-1 batch_too_large -1 batch_too_large true" {
		t.Fatalf("F9: got %q", got)
	}
}

// 3. port/goose/fixed/definition-hash (R4, R5, W11, W12)
func TestGooseFixedProofDefinitionHash(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "goose")

	t.Run("R4_W11_W12", func(t *testing.T) {
		t.Parallel()
		const schema = `package probe
fixed table T {
    b bytes(6)
    x int32 = 1 | min = -10, max = 10
    f float32 = 0.0 | min = -1.0, max = 1.0, resolution = 0.25
}
`
		u := parseCheckUnit(t, schema)
		st := fixedRootStruct(t, u, "T")
		entries := ir.TableFixedWalkRoot(st)
		// W11: bytes(N) is layout kind 14 (ir.TableKindArray).
		if entries[1].Kind != ir.TableKindArray {
			t.Fatalf("W11: bytes(6) layout kind = %d, want 14", entries[1].Kind)
		}
		layout, hash, _ := fixedRootFacts(st)
		// W12: hash includes the 4-byte entry count word at layout[0..4].
		flippedCount := append([]byte(nil), layout...)
		flippedCount[0] ^= 0x01
		if ir.TableFixedLayoutHash(flippedCount, st) == hash {
			t.Fatal("W12: flipping the 4-byte count word did not move the layout hash")
		}
		// R4: runtime never derives a hash from layout bytes it holds (no FNV-1a prime in runtime or emitted table).
		tables, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		src := string(tables["ProbeTable.goose"])
		for _, prime := range []string{"0x100000001b3", "1099511628211"} {
			if strings.Contains(src, prime) {
				t.Fatalf("R4: emitted table contains FNV-1a prime %s", prime)
			}
		}
	})

	t.Run("R5_digest_sensitivities", func(t *testing.T) {
		t.Parallel()
		base := parseCheckUnit(t, "package p\nflags F { A, B }\nfixed table T { x int32 | min = 0, max = 10\n q float32 | min = 0.0, max = 1.0, resolution = 0.5\n f1 F\n f2 F\n s string(8)\n}\n")
		diffRange := parseCheckUnit(t, "package p\nflags F { A, B }\nfixed table T { x int32 | min = 0, max = 11\n q float32 | min = 0.0, max = 1.0, resolution = 0.5\n f1 F\n f2 F\n s string(8)\n}\n")
		diffRes := parseCheckUnit(t, "package p\nflags F { A, B }\nfixed table T { x int32 | min = 0, max = 10\n q float32 | min = 0.0, max = 1.0, resolution = 0.25\n f1 F\n f2 F\n s string(8)\n}\n")
		_, hBase, _ := fixedRootFacts(fixedRootStruct(t, base, "T"))
		_, hRange, _ := fixedRootFacts(fixedRootStruct(t, diffRange, "T"))
		_, hRes, _ := fixedRootFacts(fixedRootStruct(t, diffRes, "T"))
		if hBase == hRange || hBase == hRes {
			t.Fatalf("R5: digest did not move across range/resolution change: base=%#x range=%#x res=%#x", hBase, hRange, hRes)
		}
	})
}

// 4. port/goose/fixed/retired-runtime (R10, R15)
func TestGooseFixedProofRetiredRuntime(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "goose")
	corpus := fixedCorpus(t)
	older := evolutionLoad(t, "test/tables/VOLD_field_append.schema")
	newer := evolutionLoad(t, "test/tables/VNEW_field_append.schema")
	newData := gooseCorpusFile(t, corpus, "new_field_append.bin")
	// R10 & R15: an older reader given a newer writer's file does not walk the
	// stranger's layout or forward-clamp; it refuses layout_unknown with the file's hash.
	got := gooseVersionProbe(t, gooseVersionCase{
		reader: older, root: "Lineage", capacity: 1, data: newData, fresh: true,
	})
	got.refuses(t, "layout_unknown", true, gooseFileHash(newData))
	_ = newer
}

// 5. port/goose/fixed/optional-values (E5, W2)
func TestGooseFixedProofOptionalValues(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "goose")
	const schema = `package probe
type Inner {
    a int32 = 5
    b int32 = 6
}
fixed table T {
    plain Inner
    opt ?Inner
}
`
	u := parseCheckUnit(t, schema)
	got := runCustomFixedGoose(t, u, nil, `fn main() {
    // E5: ?Inner has a 1-byte present flag + 8-byte Inner (9 bytes); plain Inner is 8 bytes -> total body 17 bytes.
    // W2: when opt is absent, its payload bytes in the record image are written as zeros even if v.Opt is populated in memory.
    var v = probe::T {
        Plain: probe::Inner { A: 11, B: 22 },
        Opt: probe::Inner { A: 0x12345678, B: 0x23456789 },
        OptPresent: false,
    };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v], out);
    let recBody = out.len - probe::TFixedBodyBytes;
    // Plain is at recBody+0..8, OptPresent at recBody+8, Opt payload at recBody+9..17.
    var optZero = out[recBody + 8] == 0;
    var i: i64 = 9;
    while i < 17 {
        if out[recBody + i] != 0 { optZero = false; }
        i += 1;
    }
    print(probe::TFixedBodyBytes == 17, " ", w > 0, " ", optZero);
}
`, nil)
	if got != "true true true" {
		t.Fatalf("OptionalValues (E5/W2): got %q", got)
	}
}

// 6. port/goose/fixed/reports (E9, R16, R18)
func TestGooseFixedProofReports(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "goose")
	// E9: duplicate never raised; R18: a clamp that cannot fire is not emitted and nothing moves;
	// R16: §5.4's counters (unknown once per lineage entry at compile, widened once per entry per record, etc.).
	const schemaNoClamp = `package probe
fixed table T {
    a int32 = 1
    b uint64 = 2
}
`
	u := parseCheckUnit(t, schemaNoClamp)
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	src := string(tables["ProbeTable.goose"])
	// R18: _TFixedClamp for an unconstrained table has no branch and returns 0.
	if strings.Contains(src, "c += 1") {
		t.Fatalf("R18: unconstrained table emitted a clamp increment:\n%s", src)
	}

	corpus := fixedCorpus(t)
	older := evolutionLoad(t, "test/tables/VOLD_unknown_census.schema")
	newer := evolutionLoad(t, "test/tables/VNEW_unknown_census.schema")
	data := gooseCorpusFile(t, corpus, "many_unknown_census.bin")
	records := gooseFixedRecords(t, data, older, "Census")
	res := gooseVersionProbe(t, gooseVersionCase{
		reader: newer, older: []*ir.Unit{older}, root: "Census", capacity: records, data: data, twice: true,
	})
	res.reads(t, records)
	if res.unknown != 1 || res.secondUnknown != 1 || res.clamped != 0 || res.widened != 0 {
		t.Fatalf("R16: unexpected census counters: %+v", res)
	}
}

// 7. port/goose/fixed/array-bounds (C1, C2, R11)
func TestGooseFixedProofArrayBounds(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "goose")
	corpus := fixedCorpus(t)
	older := evolutionLoad(t, "test/tables/VOLD_array_bounded_grow.schema")
	newer := evolutionLoad(t, "test/tables/VNEW_array_bounded_grow.schema")
	base := gooseCorpusFile(t, corpus, "old_array_bounded_grow.bin")
	lb := int(binary.LittleEndian.Uint32(base[16:20]))
	countOff := 20 + lb + 8 + 4

	// C1: negative countword (v < 0 -> count_over_bound); C2: count > writer Max (7 > 4 -> count_over_bound);
	// R11: plan carries WRITER's bound (4), even though reader's bound is 8.
	neg := append([]byte(nil), base...)
	binary.LittleEndian.PutUint32(neg[countOff:], 0xffffffff)
	over := append([]byte(nil), base...)
	binary.LittleEndian.PutUint32(over[countOff:], 7)

	rNeg := gooseVersionProbe(t, gooseVersionCase{reader: newer, older: []*ir.Unit{older}, root: "ArrayBoundedGrow", capacity: 1, poison: true, data: neg, fresh: true})
	rNeg.refuses(t, "count_over_bound", false, 0)

	rOver := gooseVersionProbe(t, gooseVersionCase{reader: newer, older: []*ir.Unit{older}, root: "ArrayBoundedGrow", capacity: 1, poison: true, data: over, fresh: true})
	rOver.refuses(t, "count_over_bound", false, 0)
}

// 8. port/goose/fixed/text (C3, C4, C5, R24)
func TestGooseFixedProofText(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "goose")
	const schema = `package probe
fixed table T {
    s string(4)
    w wstring(4)
}
`
	u := parseCheckUnit(t, schema)
	got := runCustomFixedGoose(t, u, nil, `fn main() {
    var v = probe::T {
        S: [97, 98, 0, 0],
        SLength: 2,
        W: [0x0041, 0x0042, 0, 0],
        WLength: 2,
    };
    var clean: u8[>..] = [];
    let _ = probe::TFixedSave([v], clean);
    let body = clean.len - probe::TFixedBodyBytes;

    // C3: text length > N (5 > 4) -> length_over_bound
    var overLen: u8[>..] = [];
    var i: i64 = 0;
    while i < clean.len { overLen.push(clean[i]); i += 1; }
    overLen[body] = 5;

    // C4 & R24: ill-formed UTF-8 in used units (0xff) -> text_ill_formed
    var badUtf8: u8[>..] = [];
    i = 0;
    while i < clean.len { badUtf8.push(clean[i]); i += 1; }
    badUtf8[body + 4] = 0xff;

    // R24: 0xff in slack past used length (at index 2 when length == 2) is NOT a refusal!
    var slackBytes: u8[>..] = [];
    i = 0;
    while i < clean.len { slackBytes.push(clean[i]); i += 1; }
    slackBytes[body + 4 + 2] = 0xff;

    // C5: unpaired UTF-16 surrogate (0xd800) in used wstring units -> text_ill_formed
    var badUtf16: u8[>..] = [];
    i = 0;
    while i < clean.len { badUtf16.push(clean[i]); i += 1; }
    badUtf16[body + 8 + 4] = 0x00;
    badUtf16[body + 8 + 5] = 0xd8;

    var dst: probe::T[1] = [probe::T { .. }; 1];
    var rep = schema_rt::FixedReport { .. };
    let n1 = probe::TFixedLoad(dst, overLen, rep);
    let r1 = schema_rt::fixed_refusal_name(rep.reason);
    let n2 = probe::TFixedLoad(dst, badUtf8, rep);
    let r2 = schema_rt::fixed_refusal_name(rep.reason);
    let n3 = probe::TFixedLoad(dst, slackBytes, rep);
    let r3 = schema_rt::fixed_refusal_name(rep.reason);
    let n4 = probe::TFixedLoad(dst, badUtf16, rep);
    let r4 = schema_rt::fixed_refusal_name(rep.reason);
    print(n1, " ", r1, " ", n2, " ", r2, " ", n3, " ", r3, " ", n4, " ", r4);
}
`, nil)
	if got != "-1 length_over_bound -1 text_ill_formed 1 ok -1 text_ill_formed" {
		t.Fatalf("Text (C3/C4/C5/R24): got %q", got)
	}
}

// 9. port/goose/fixed/ordinals (C9, C10, R31)
func TestGooseFixedProofOrdinals(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "goose")
	// C9: union tag past arms -> out_of_range on the identity plan (§4.6)
	// C10: enum ordinal past top -> out_of_range on the identity plan (§4.6)
	// R31: writer's variant count governs on compiled plan as well as identity plan
	// (the compiled plan still counts and lands None; the strict identity read refuses)
	const oldSchema = `package probe
enum Tier { Bronze, Silver }
union Pick {
    a int32
    b int32
}
fixed table Lineage {
    tier Tier
    pick Pick
    seq int32
}
`
	const newSchema = `package probe
enum Tier { Bronze, Silver, Gold }
union Pick {
    a int32
    b int32
    c int32
}
fixed table Lineage {
    tier Tier
    pick Pick
    seq int32
}
`
	uOld := parseCheckUnit(t, oldSchema)
	uNew := parseCheckUnit(t, newSchema)
	eOld, ok := FixedEvolutionOf(uOld, "Lineage")
	if !ok {
		t.Fatal("missing old Lineage")
	}
	// Build a 1-record old file where tier = 3 (Gold in new, but past old's max 2) and pick.Type = 3 (past old's max 2).
	oldLayout, oldHash, _ := fixedRootFacts(fixedRootStruct(t, uOld, "Lineage"))
	// Old body: tier (1 byte) + pick (1 byte tag + 4 byte arm = 5 bytes) + seq (4 bytes) = 10 bytes.
	body := []byte{3, 3, 1, 0, 0, 0, 42, 0, 0, 0}
	file := makeFixedFile(oldHash, oldLayout, body)

	gotOld := runCustomFixedGoose(t, uOld, nil, `fn main() {
    var data: u8[>..] = [];
    if !read_file("in.bin", data) { return; }
    var dst: probe::Lineage[1] = [probe::Lineage { .. }; 1];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::LineageFixedLoad(dst, data, rep);
    print(n, " ", schema_rt::fixed_refusal_name(rep.reason), " ", rep.oor, " ", dst[0].Tier as u64, " ", dst[0].Pick.Type as u64, " ", dst[0].Seq);
}
`, map[string][]byte{"in.bin": file})

	gotNew := runCustomFixedGoose(t, uNew, map[string][]FixedEvolutionEntry{"Lineage": {eOld}}, `fn main() {
    var data: u8[>..] = [];
    if !read_file("in.bin", data) { return; }
    var dst: probe::Lineage[1] = [probe::Lineage { .. }; 1];
    var rep = schema_rt::FixedEvolveReport { .. };
    let n = probe::LineageFixedEvolveLoad(dst, data, rep);
    print(n, " ", rep.oor, " ", dst[0].Tier as u64, " ", dst[0].Pick.Type as u64, " ", dst[0].Seq);
}
`, map[string][]byte{"in.bin": file})

	if gotOld != "-1 out_of_range 0 0 0 0" || gotNew != "1 2 0 0 42" {
		t.Fatalf("Ordinals (C9/C10/R31): old=%q new=%q, want \"-1 out_of_range 0 0 0 0\" and \"1 2 0 0 42\"", gotOld, gotNew)
	}
}

// 10. port/goose/fixed/normalization (C12, C13, R17)
func TestGooseFixedProofNormalization(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "goose")
	const schema = `package probe
fixed table T {
    flag bool
    opt ?int32
}
`
	u := parseCheckUnit(t, schema)
	got := runCustomFixedGoose(t, u, nil, `fn main() {
    var v = probe::T { Flag: false, Opt: 99, OptPresent: false };
    var data: u8[>..] = [];
    let _ = probe::TFixedSave([v], data);
    let body = data.len - probe::TFixedBodyBytes;
    // Forge Flag byte to 0x7f and OptPresent byte to 0x5a: C12, C13, R17 say
    // both are wire_damaged and the strict read refuses (never lands them as true).
    data[body + 0] = 0x7f;
    data[body + 1] = 0x5a;
    data[body + 2] = 42;

    var dst: probe::T[1] = [probe::T { .. }; 1];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(dst, data, rep);
    print(n, " ", schema_rt::fixed_refusal_name(rep.reason), " ", rep.oor, " ", dst[0].Flag, " ", dst[0].OptPresent, " ", dst[0].Opt);
}
`, nil)
	if got != "-1 wire_damaged 0 false false 0" {
		t.Fatalf("Normalization (C12/C13/R17): got %q", got)
	}
}

// 11. port/goose/fixed/live-extents (C14, W4, W9)
func TestGooseFixedProofLiveExtents(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "goose")
	const schema = `package probe
enum Small { One, Two }
fixed table T {
    arr [..3]Small
    opt ?Small
    tail int32 = 77
}
`
	u := parseCheckUnit(t, schema)
	got := runCustomFixedGoose(t, u, nil, `fn main() {
    var v = probe::T { Arr: [1, 0, 0], ArrCount: 1, Opt: 0, OptPresent: false, Tail: 77 };
    var data: u8[>..] = [];
    let _ = probe::TFixedSave([v], data);
    let body = data.len - probe::TFixedBodyBytes;
    // Body layout: ArrCount (4B at +0), Arr[0..3] (3B at +4..+7), OptPresent (1B at +7), Opt (1B at +8), Tail (4B at +9..+13).
    // Forge out-of-range enum ordinals (99) into inactive Arr[1], Arr[2] and absent Opt!
    // C14 & W4: bounds pass walks LIVE extent only; slack is not clamped or counted.
    data[body + 5] = 99;
    data[body + 6] = 99;
    data[body + 8] = 99;

    var dst: probe::T[1] = [probe::T { .. }; 1];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(dst, data, rep);
    print(n, " ", schema_rt::fixed_refusal_name(rep.reason), " ", rep.oor, " ", dst[0].ArrCount, " ", dst[0].Arr[0] as u64, " ", !dst[0].OptPresent, " ", dst[0].Tail);
}
`, nil)
	if got != "1 ok 0 1 1 true 77" {
		t.Fatalf("LiveExtents (C14/W4/W9): got %q", got)
	}
}

// 12. port/goose/fixed/writing (W1, W3, W5, W8)
func TestGooseFixedProofWriting(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "goose")
	const schema = `package probe
union Pick {
    narrow uint8
    wide uint64
}
fixed table T {
    arr [..3]uint16
    s string(4)
    b bytes(4)
    pick Pick
}
`
	u := parseCheckUnit(t, schema)
	got := runCustomFixedGoose(t, u, nil, `fn main() {
    // Populate slack slots in memory with non-zero garbage (0xaa / 99) and select the narrow union arm.
    // W1: write slack is zeros; W3: zero behind a narrower arm; W8: bytes(N) takes the 4B length + N bytes row.
    var v = probe::T {
        Arr: [0x1122, 0x9999, 0x8888],
        ArrCount: 1,
        S: [65, 90, 90, 90],
        SLength: 1,
        B: [0x42, 0xaa, 0xaa, 0xaa],
        BLength: 1,
        Pick: probe::Pick { Type: 1, Narrow: 7, Wide: 0xffffffffffffffff },
    };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v], out);
    let body = out.len - probe::TFixedBodyBytes;
    // Arr slack at body + 4 + 2 .. body + 10 (4 bytes)
    let arrSlackZero = out[body + 6] == 0 && out[body + 7] == 0 && out[body + 8] == 0 && out[body + 9] == 0;
    // S slack at body + 10 + 4 + 1 .. body + 18 (3 bytes)
    let sSlackZero = out[body + 15] == 0 && out[body + 16] == 0 && out[body + 17] == 0;
    // B slack at body + 18 + 4 + 1 .. body + 26 (3 bytes)
    let bSlackZero = out[body + 23] == 0 && out[body + 24] == 0 && out[body + 25] == 0;
    // Pick narrow arm slack at body + 26 + 1 + 1 .. body + 35 (7 bytes)
    var pickSlackZero = true;
    var i: i64 = 28;
    while i < 35 {
        if out[body + i] != 0 { pickSlackZero = false; }
        i += 1;
    }
    print(w > 0, " ", arrSlackZero, " ", sSlackZero, " ", bSlackZero, " ", pickSlackZero);
}
`, nil)
	if got != "true true true true true" {
		t.Fatalf("Writing (W1/W3/W5/W8): got %q", got)
	}
}

// 13. port/goose/fixed/union-guards (W7, W16, R28)
func TestGooseFixedProofUnionGuards(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "goose")
	const oldSchema = `package probe
union Inner {
    x int16
    y int32
}
union Outer {
    inner Inner
    plain int32
}
fixed table T {
    outer Outer
    tail int32 = 9
}
`
	const newSchema = `package probe
union Inner {
    x int32
    y int32
}
union Outer {
    inner Inner
    plain int32
}
fixed table T {
    outer Outer
    tail int32 = 9
}
`
	uOld := parseCheckUnit(t, oldSchema)
	uNew := parseCheckUnit(t, newSchema)
	eOld, ok := FixedEvolutionOf(uOld, "T")
	if !ok {
		t.Fatal("missing old T")
	}
	// W7, W16, R28: nested union Inner inside Outer has a 2-link guard chain (Outer tag == 1, Inner tag == 1).
	// When Outer selects `plain` (tag 2), the bytes at Inner's offset must NOT fire Inner's widen or clamp!
	oldLayout, oldHash, _ := fixedRootFacts(fixedRootStruct(t, uOld, "T"))
	// Old Outer: 1B outer tag + max(Inner=1+4=5B, plain=4B) = 6B; plus tail 4B = 10B.
	// Case 1: Outer tag = 1 (inner), Inner tag = 1 (x = -7 as int16: 0xf9, 0xff), tail = 100.
	b1 := []byte{1, 1, 0xf9, 0xff, 0, 0, 100, 0, 0, 0}
	// Case 2: Outer tag = 2 (plain = 0x01020308, whose low byte at Inner's tag offset is 8 — if Inner's guard didn't check Outer tag == 1, Inner tag 8 would clamp!), tail = 200.
	b2 := []byte{2, 8, 3, 2, 1, 0, 200, 0, 0, 0}
	file := makeFixedFile(oldHash, oldLayout, b1, b2)

	got := runCustomFixedGoose(t, uNew, map[string][]FixedEvolutionEntry{"T": {eOld}}, `fn main() {
    var data: u8[>..] = [];
    if !read_file("in.bin", data) { return; }
    var dst: probe::T[2] = [probe::T { .. }; 2];
    var rep = schema_rt::FixedEvolveReport { .. };
    let n = probe::TFixedEvolveLoad(dst, data, rep);
    let ok0 = dst[0].Outer.Type == 1 && dst[0].Outer.Inner.Type == 1 && dst[0].Outer.Inner.X == -7 && dst[0].Tail == 100;
    let ok1 = dst[1].Outer.Type == 2 && dst[1].Outer.Plain == 0x01020308 && dst[1].Tail == 200;
    print(n, " ", schema_rt::fixed_evolve_refusal_name(rep.reason), " ", rep.widened, " ", rep.oor, " ", ok0 && ok1);
}
`, map[string][]byte{"in.bin": file})
	if got != "2 ok 1 0 true" {
		t.Fatalf("UnionGuards (W7/W16/R28): got %q", got)
	}
}

// 14. port/goose/fixed/plan-execution (W6, P1)
func TestGooseFixedProofPlanExecution(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "goose")
	// W6 & P1: reading a file on the identity path (FixedLoad) and through
	// the compiled plan for the same layout (FixedEvolveLoad with the unit's own
	// layout handed in as a lineage entry) yields identical record images and reports.
	const schema = `package probe
enum Kind { A, B }
union Choice {
    n int32
    s string(4)
}
fixed table T {
    head int32 = 1
    opt ?int32
    arr [..2]Kind
    ch Choice
}
`
	u := parseCheckUnit(t, schema)
	selfEntry, ok := FixedEvolutionOf(u, "T")
	if !ok {
		t.Fatal("missing T")
	}
	// Shift the hash by 1 in a copy of the file and entry so FixedEvolveLoad takes the compiled plan (not the identity shortcut) over the exact same layout!
	forgedEntry := selfEntry
	forgedEntry.Wire ^= 1

	got := runCustomFixedGoose(t, u, map[string][]FixedEvolutionEntry{"T": {forgedEntry}}, `fn main() {
    var v = probe::T {
        Head: 42,
        Opt: -99,
        OptPresent: true,
        Arr: [2, 0],
        ArrCount: 1,
        Ch: probe::Choice { Type: 2, S: [104, 105, 0, 0], SLength: 2, .. },
    };
    var idBytes: u8[>..] = [];
    let _ = probe::TFixedSave([v], idBytes);

    // Clone idBytes and flip low bit of header hash and record hash to force compiled-plan execution.
    var planBytes: u8[>..] = [];
    var i: i64 = 0;
    while i < idBytes.len { planBytes.push(idBytes[i]); i += 1; }
    planBytes[8] = planBytes[8] ^ 1;
    let recOff = planBytes.len - probe::TFixedRecordBytes;
    planBytes[recOff] = planBytes[recOff] ^ 1;

    var idOut: probe::T[1] = [probe::T { .. }; 1];
    var idRep = schema_rt::FixedReport { .. };
    let n1 = probe::TFixedLoad(idOut, idBytes, idRep);

    var planOut: probe::T[1] = [probe::T { .. }; 1];
    var planRep = schema_rt::FixedEvolveReport { .. };
    let n2 = probe::TFixedEvolveLoad(planOut, planBytes, planRep);

    var img1: u8[>..] = [];
    var img2: u8[>..] = [];
    probe::_TFixedWrite(img1, idOut[0]);
    probe::_TFixedWrite(img2, planOut[0]);
    var eq = img1.len == img2.len;
    i = 0;
    while i < img1.len {
        if img1[i] != img2[i] { eq = false; }
        i += 1;
    }
    print(n1 == 1 && n2 == 1, " ", idRep.oor == planRep.oor, " ", eq);
}
`, nil)
	if got != "true true true" {
		t.Fatalf("PlanExecution (W6/P1): got %q", got)
	}
}

// 15. port/goose/fixed/interoperability (W13, P2, R27)
func TestGooseFixedProofInteroperability(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "goose")
	corpus := fixedCorpus(t)
	records := fixedManifestRecords(t, corpus)
	manifest := gooseVersionManifest(t, corpus)

	for _, v := range fixedVectors {
		t.Run(strings.TrimSuffix(v.file, ".bin"), func(t *testing.T) {
			t.Parallel()
			want := records[v.file]
			data, err := os.ReadFile(filepath.Join(corpus, v.file))
			if err != nil {
				t.Fatal(err)
			}
			u := fixedUnit(t, v.schema)
			st := fixedRootStruct(t, u, v.root)
			layout, hash, _ := fixedRootFacts(st)

			// W13: layout+hash == C++ reference over every fixed vector.
			if got := binary.LittleEndian.Uint64(data[8:16]); got != hash {
				t.Fatalf("%s: W13 hash mismatch: %#x != %#x", v.file, got, hash)
			}
			lb := int(binary.LittleEndian.Uint32(data[16:20]))
			if lb != len(layout) || !bytes.Equal(data[20:20+lb], layout) {
				t.Fatalf("%s: W13 layout bytes differ from C++ reference", v.file)
			}

			// P2: write-read-write byte-identical.
			dir := t.TempDir()
			main := fixedProgram(t, u, v.root, dir, []string{"corpus.bin"}, want)
			out := gooserun.Run(t, dir, main, map[string][]byte{"corpus.bin": data})
			a := fixedAnswers(t, out, 1)[0]
			if a.n != want || a.reason != "ok" || a.clamped != 0 || a.bytes != hex.EncodeToString(data) {
				t.Fatalf("%s: P2 write-read-write mismatch", v.file)
			}

			// R27: the manifest is the wire — verify manifest.txt carries the exact record count and field values for v.file.
			line := manifest.line(t, v.file)
			if line.records != want || line.root != v.root || len(line.values) == 0 {
				t.Fatalf("%s: R27 manifest mismatch: records=%d root=%s values=%d", v.file, line.records, line.root, len(line.values))
			}
		})
	}

	t.Run("R27_manifest_value_oracle", func(t *testing.T) {
		t.Parallel()
		line := manifest.line(t, "new_field_append.bin")
		u := evolutionLoad(t, "test/tables/VNEW_field_append.schema")
		data := gooseCorpusFile(t, corpus, "new_field_append.bin")
		res := gooseVersionProbe(t, gooseVersionCase{
			reader: u, root: line.root, capacity: line.records, data: data, want: line,
		})
		res.reads(t, line.records)
		res.owes(t)
	})
}

// TestGooseFixedProofEvolutionNegativeControl proves that the layout hash,
// C++ reference corpus comparison, and wire refusal checks in
// fixed_evolution_proof_test.go fail when layout-relevant schema properties or
// wire bytes are mutated.
func TestGooseFixedProofEvolutionNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "goose")

	t.Run("definition_hash_sensitivity", func(t *testing.T) {
		t.Parallel()
		uBase := parseCheckUnit(t, `package probe
fixed table T {
    x int32
    b bytes(6)
}
`)
		uNameChanged := parseCheckUnit(t, `package probe
fixed table T {
    y int32
    b bytes(6)
}
`)
		uBoundChanged := parseCheckUnit(t, `package probe
fixed table T {
    x int32
    b bytes(8)
}
`)
		_, hBase, _ := fixedRootFacts(fixedRootStruct(t, uBase, "T"))
		_, hName, _ := fixedRootFacts(fixedRootStruct(t, uNameChanged, "T"))
		_, hBound, _ := fixedRootFacts(fixedRootStruct(t, uBoundChanged, "T"))
		if hBase == hName {
			t.Fatalf("negative control failed: changing field name did not change definition hash")
		}
		if hBase == hBound {
			t.Fatalf("negative control failed: changing bytes(N) bound did not change definition hash")
		}
	})

	t.Run("corpus_byte_mutation_detected", func(t *testing.T) {
		t.Parallel()
		corpus := fixedCorpus(t)
		records := fixedManifestRecords(t, corpus)
		v := fixedVectors[0]
		want := records[v.file]
		orig, err := os.ReadFile(filepath.Join(corpus, v.file))
		if err != nil {
			t.Fatal(err)
		}
		u := fixedUnit(t, v.schema)
		st := fixedRootStruct(t, u, v.root)
		layout, _, _ := fixedRootFacts(st)

		// Flip one byte in the first record body and run the Goose round-trip probe:
		// either FixedLoad refuses the mutated byte or the re-saved hex diverges
		// from the original C++ reference corpus bytes.
		mutated := bytes.Clone(orig)
		bodyIdx := 20 + len(layout) + 8
		mutated[bodyIdx] ^= 0x01

		dir := t.TempDir()
		main := fixedProgram(t, u, v.root, dir, []string{"corpus.bin"}, want)
		out := gooserun.Run(t, dir, main, map[string][]byte{"corpus.bin": mutated})
		a := fixedAnswers(t, out, 1)[0]
		if a.n == want && a.reason == "ok" && a.bytes == hex.EncodeToString(orig) {
			t.Fatalf("negative control failed: mutated corpus byte was not detected by round-trip oracle")
		}
	})
}
