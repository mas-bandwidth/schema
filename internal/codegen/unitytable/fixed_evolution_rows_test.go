package unitytable_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/unitytable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type unityRowResult struct {
	count    int
	reason   string
	fields   map[string]string
	resaved  []byte
	counters fixedproof.LoadCounters
}

func (r unityRowResult) head() string {
	if r.count < 0 {
		return "refused " + r.reason
	}
	return fmt.Sprintf("ok %d u%d w%d", r.count, r.counters.Unknown, r.counters.Widened)
}

func unityRowEntry(t *testing.T, u *ir.Unit, root string) fixedproof.LineageEntry {
	t.Helper()
	e, ok := fixedproof.LineageEntryOf(u, root)
	if !ok {
		t.Fatalf("missing lineage entry for %s", root)
	}
	return e
}

func unityRowLineage(t *testing.T, older *ir.Unit, root string) map[string][]fixedproof.LineageEntry {
	t.Helper()
	return map[string][]fixedproof.LineageEntry{root: {unityRowEntry(t, older, root)}}
}

func unityRowLoad(t *testing.T, u *ir.Unit, root string, lin map[string][]fixedproof.LineageEntry, wire []byte, mutate func(map[string][]byte)) unityRowResult {
	t.Helper()
	n, reason, fields, resaved, counters := unityLoadLineage(t, u, root, lin, wire, -1, mutate)
	return unityRowResult{count: n, reason: reason, fields: fields, resaved: resaved, counters: counters}
}

func unityRowSave(t *testing.T, u *ir.Unit, root, setup string, count int) []byte {
	t.Helper()
	files, err := unityGenerateFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	ns := unityNamespace(u.Package)
	probe := fmt.Sprintf(`using System;
using System.IO;
using Serialize;
using %[1]s;

static class Program
{
    static int Main()
    {
        ProbeCommon.InstallAssertTrap();
        %[2]s[] r = new %[2]s[%[3]d];
        for (int i = 0; i < r.Length; i++) { r[i] = Schema.New%[2]s(); }
%[4]s
        long need = Schema.%[2]sFixedMeasure(%[3]d);
        byte[] buf = new byte[need];
        long written = Schema.%[2]sFixedSave(r, buf);
        if (written != need) { return 2; }
        File.WriteAllBytes("out.bin", buf);
        return 0;
    }
}
`, ns, root, count, setup)
	out, code, wire, runErr := runUnityProbe(t, files, probe, nil)
	if runErr != nil {
		t.Fatal(runErr)
	}
	if code != 0 || wire == nil {
		t.Fatalf("unityRowSave(%s) failed (exit %d): %s", root, code, out)
	}
	return wire
}

func unityRowBodyOffset(file []byte, recordBytes int64, recIndex int) int {
	layoutLen := int(binary.LittleEndian.Uint32(file[16:20]))
	return 20 + layoutLen + recIndex*int(recordBytes) + 8
}

func unityRowFieldOffset(t *testing.T, u *ir.Unit, root, fieldName string) int {
	t.Helper()
	st := fixedproof.RootStruct(t, u, root)
	off := int64(0)
	for _, f := range st.Fields {
		if f.Name == fieldName {
			return int(off)
		}
		off += ir.TableFixedFieldBytes(f)
	}
	t.Fatalf("%s.%s not found", root, fieldName)
	return 0
}

func plantInFile(t *testing.T, fileName, oldSub, newSub string) func(map[string][]byte) {
	t.Helper()
	return func(files map[string][]byte) {
		data, ok := files[fileName]
		if !ok {
			t.Fatalf("plant target %s missing from generated files", fileName)
		}
		if !strings.Contains(string(data), oldSub) {
			t.Fatalf("plant pattern %q not found in %s", oldSub, fileName)
		}
		files[fileName] = []byte(strings.Replace(string(data), oldSub, newSub, 1))
	}
}

// 1. numeric-evolution (E3, C8, R20, R21): adjacent ladders + signed/float
// widening + 64->128 Form-3 refusal + negative controls.
func TestUnityFixedRowNumericEvolution(t *testing.T) {
	slowtest.Gate(t, "dotnet and the C++ oracle")
	t.Parallel()

	older := fixedproof.ParseCheckUnit(t, "package probe\nfixed table N {\n    lead uint32 = 1\n    v int16 = 0\n    f float32 = 0.0\n    trail uint32 = 2\n}\n")
	newer := fixedproof.ParseCheckUnit(t, "package probe\nfixed table N {\n    lead uint32 = 1\n    v int32 = 0\n    f float64 = 0.0\n    trail uint32 = 2\n    added int32 = 77\n}\n")
	eo := unityRowEntry(t, older, "N")
	body := binary.LittleEndian.AppendUint32(nil, 5)
	body = binary.LittleEndian.AppendUint16(body, 0xFFF9) // -7
	body = binary.LittleEndian.AppendUint32(body, 0xC49A5200)
	body = binary.LittleEndian.AppendUint32(body, 6)
	file := fixedproof.MakeFixedFile(eo.Wire, eo.Layout, body, body)
	lin := unityRowLineage(t, older, "N")
	got := unityRowLoad(t, newer, "N", lin, file, nil)
	if got.head() != "ok 2 u0 w4" {
		t.Errorf("widening read: %s, want ok 2 u0 w4", got.head())
	}
	for k, w := range map[string]string{"r0.lead": "5", "r0.v": "-7", "r0.trail": "6", "r0.added": "77", "r1.v": "-7", "r1.added": "77"} {
		if got.fields[k] != w {
			t.Errorf("%s landed %q, want %s", k, got.fields[k], w)
		}
	}
	en := unityRowEntry(t, newer, "N")
	fAt := unityRowFieldOffset(t, newer, "N", "f")
	for i := range 2 {
		at := unityRowBodyOffset(got.resaved, en.Record, i) + fAt
		if b := binary.LittleEndian.Uint64(got.resaved[at:]); b != math.Float64bits(float64(math.Float32frombits(0xC49A5200))) || b != 0xC0934A4000000000 {
			t.Errorf("r%d.f widened to %#x, want 0xC0934A4000000000", i, b)
		}
	}

	// Plain int64 -> int128 and uint64 -> uint128 are NOT lawful Form-3 widenings,
	// whereas fixed(48,16) -> fixed(112,16) and ufixed(48,16) -> ufixed(112,16) ARE.
	for _, pair := range []struct {
		oldT, newT string
		want       string
	}{
		{"int8", "int16", "ok 1 u0 w1"},
		{"int16", "int32", "ok 1 u0 w1"},
		{"int32", "int64", "ok 1 u0 w1"},
		{"int64", "int128 | min = -100, max = 100", "refused layout_malformed"},
		{"uint8", "uint16", "ok 1 u0 w1"},
		{"uint16", "uint32", "ok 1 u0 w1"},
		{"uint32", "uint64", "ok 1 u0 w1"},
		{"uint64", "uint128", "refused layout_malformed"},
		{"fixed(48, 16) | min = -10, max = 10", "fixed(112, 16) | min = -10, max = 10", "ok 1 u0 w1"},
		{"ufixed(48, 16) | min = 0, max = 10", "ufixed(112, 16) | min = 0, max = 10", "ok 1 u0 w1"},
	} {
		uo := fixedproof.ParseCheckUnit(t, fmt.Sprintf("package probe\nfixed table T {\n    x %s\n}\n", pair.oldT))
		un := fixedproof.ParseCheckUnit(t, fmt.Sprintf("package probe\nfixed table T {\n    x %s\n}\n", pair.newT))
		w := unityRowSave(t, uo, "T", "", 1)
		res := unityRowLoad(t, un, "T", unityRowLineage(t, uo, "T"), w, nil)
		if res.head() != pair.want {
			t.Errorf("ladder %s -> %s: got %s, want %s", pair.oldT, pair.newT, res.head(), pair.want)
		}
	}

	// Negative control 1: drop widened count on unguarded lane.
	nc := unityRowLoad(t, newer, "N", lin, file, plantInFile(t, "probe.Tables.cs",
		"report.Widened = lane.Widens * n;", "report.Widened = 0;"))
	if nc.head() == got.head() {
		t.Errorf("negative control: dropped widened count still produced %s", nc.head())
	}
	// Negative control 2: zero-extend instead of sign-extend on FixedWidenS.
	nc = unityRowLoad(t, newer, "N", lin, file, plantInFile(t, unitytable.RuntimeFile,
		"FixedPut(image, o.Dst, o.Width, unchecked((ulong)FixedSigned(FixedGet(body, o.Src, o.Size), o.Size)));",
		"FixedPut(image, o.Dst, o.Width, FixedGet(body, o.Src, o.Size));"))
	if nc.count == 2 && nc.fields["r0.v"] == "-7" {
		t.Errorf("negative control: zero-extended int16 still landed -7")
	}
}

// 2. scalar-bounds (C7, R30): ranged int32, int128, uint16, bits(N), compressed
// float finite + range + 'Q' digest + resolution evolution + negative control.
func TestUnityFixedRowScalarBounds(t *testing.T) {
	slowtest.Gate(t, "dotnet")
	t.Parallel()

	u := fixedproof.ParseCheckUnit(t, "package probe\nfixed table T {\n    x int32 = 0 | min = -5, max = 5\n    s int128 = 0 | min = -500, max = 500\n    u uint16 = 10 | min = 10, max = 1000\n    b bits(3)\n}\n")
	e := unityRowEntry(t, u, "T")
	body := func(x int32, s int64, v uint16, bits uint32) []byte {
		b := binary.LittleEndian.AppendUint32(nil, uint32(x))
		b = binary.LittleEndian.AppendUint64(b, uint64(s))
		hi := uint64(0)
		if s < 0 {
			hi = ^uint64(0)
		}
		b = binary.LittleEndian.AppendUint64(b, hi)
		b = binary.LittleEndian.AppendUint16(b, v)
		return binary.LittleEndian.AppendUint32(b, bits)
	}
	in := fixedproof.MakeFixedFile(e.Wire, e.Layout, body(3, -42, 500, 7))
	got := unityRowLoad(t, u, "T", nil, in, nil)
	if got.head() != "ok 1 u0 w0" || got.fields["r0.x"] != "3" || got.fields["r0.u"] != "500" || got.fields["r0.b"] != "7" || !bytes.Equal(got.resaved, in) {
		t.Errorf("C7 in range: %s %v", got.head(), got.fields)
	}
	for _, c := range []struct {
		name string
		b    []byte
	}{
		{"x below", body(-6, 0, 10, 0)},
		{"x above", body(6, 0, 10, 0)},
		{"s below", body(0, -501, 10, 0)},
		{"s above", body(0, 501, 10, 0)},
		{"u below", body(0, 0, 9, 0)},
		{"u above", body(0, 0, 1001, 0)},
		{"bits above", body(0, 0, 10, 8)},
	} {
		if res := unityRowLoad(t, u, "T", nil, fixedproof.MakeFixedFile(e.Wire, e.Layout, c.b), nil); res.head() != "refused out_of_range" {
			t.Errorf("C7 %s: %s, want refused out_of_range", c.name, res.head())
		}
	}

	// Negative control: disable FixedRangeS in runtime.
	nc := unityRowLoad(t, u, "T", nil, fixedproof.MakeFixedFile(e.Wire, e.Layout, body(-6, 0, 10, 0)),
		plantInFile(t, unitytable.RuntimeFile,
			"if (v < unchecked((long)c.Lo) || v > unchecked((long)c.Hi)) { return TableReport.OutOfRange; }",
			"if (c.Src < -1) { return TableReport.OutOfRange; }"))
	if nc.head() == "refused out_of_range" {
		t.Errorf("negative control: dropped FixedRangeS check still refused out_of_range")
	}

	const cf = "package probe\nfixed table Reading {\n    lead uint32 = 1\n    aim float32 = 0.0%s\n    trail uint32 = 2\n}\n"
	old := fixedproof.ParseCheckUnit(t, fmt.Sprintf(cf, " | min = -1.0, max = 2.0, resolution = 0.1"))
	if d := ir.TableFixedDefinitionsDigest(old.Tables["Reading"]); !strings.Contains(string(d), "Q") {
		t.Errorf("R30: digest missing resolution under 'Q'")
	}
	saved := unityRowSave(t, old, "Reading", "        r[0].Aim = 0.3f;", 1)
	at := len(saved) - 8
	if b := binary.LittleEndian.Uint32(saved[at:]); b != 0x3e99999a {
		t.Errorf("R30: compressed float bits = %#x, want 0x3e99999a", b)
	}
	for _, c := range []struct {
		bits uint32
		want string
	}{
		{0x7fc00000, "refused non_finite_float"},
		{0x7f800000, "refused non_finite_float"},
		{0xc0000000, "refused out_of_range"},
		{0x40800000, "refused out_of_range"},
		{0x80000000, "ok 1 u0 w0"},
		{0x3e99999a, "ok 1 u0 w0"},
	} {
		f := slices.Clone(saved)
		binary.LittleEndian.PutUint32(f[at:], c.bits)
		res := unityRowLoad(t, old, "Reading", nil, f, nil)
		canonical := c.bits
		if canonical == 0x80000000 {
			canonical = 0
		}
		wantBytes := slices.Clone(f)
		binary.LittleEndian.PutUint32(wantBytes[at:], canonical)
		if res.head() != c.want || (res.count == 1 && !bytes.Equal(res.resaved, wantBytes)) {
			t.Errorf("R30 %#08x: got %s, want %s", c.bits, res.head(), c.want)
		}
	}
	lin := unityRowLineage(t, old, "Reading")
	for _, c := range []struct{ name, src string }{
		{"finer", fmt.Sprintf(cf, " | min = -1.0, max = 2.0, resolution = 0.01")},
		{"dropped", fmt.Sprintf(cf, "")},
	} {
		nu := fixedproof.ParseCheckUnit(t, c.src)
		if unityRowEntry(t, nu, "Reading").Wire == unityRowEntry(t, old, "Reading").Wire {
			t.Errorf("R30 %s: resolution did not move hash", c.name)
		}
		res := unityRowLoad(t, nu, "Reading", lin, saved, nil)
		if res.head() != "ok 1 u0 w0" || binary.LittleEndian.Uint32(res.resaved[len(res.resaved)-8:]) != 0x3e99999a {
			t.Errorf("R30 %s: %s", c.name, res.head())
		}
	}
}

// 3. ordinals (C9, C10, deprecated enum variant normalization + write refusal + negative controls).
func TestUnityFixedRowOrdinals(t *testing.T) {
	slowtest.Gate(t, "dotnet")
	t.Parallel()

	const oldSrc = "package probe\nenum Tier { Low, High }\nunion Pick {\n    a int32\n    b int32\n}\nfixed table Lineage {\n    tier Tier\n    pick Pick\n    seq uint32\n}\n"
	const newSrc = "package probe\nenum Tier { Low, High, Ultra }\nunion Pick {\n    a int32\n    b int32\n    c int64\n}\nfixed table Lineage {\n    tier Tier\n    pick Pick\n    seq uint32\n}\n"
	uOld := fixedproof.ParseCheckUnit(t, oldSrc)
	uNew := fixedproof.ParseCheckUnit(t, newSrc)
	lin := unityRowLineage(t, uOld, "Lineage")
	clean := unityRowSave(t, uOld, "Lineage", "        r[0].Tier = Tier.High;\n        r[0].Pick.Type = PickType.B;\n        r[0].Pick.B = 9;\n        r[0].Seq = 42U;", 1)
	bodyOff := unityRowBodyOffset(clean, unityRowEntry(t, uOld, "Lineage").Record, 0)
	badTier := slices.Clone(clean)
	badTier[bodyOff] = 3 // past writer's 2 variants (inside reader's 3!)
	badTag := slices.Clone(clean)
	badTag[bodyOff+1] = 3 // past writer's 2 arms (inside reader's 3!)

	for _, plan := range []struct {
		name string
		u    *ir.Unit
		lin  map[string][]fixedproof.LineageEntry
	}{{"identity", uOld, nil}, {"compiled", uNew, lin}} {
		c := unityRowLoad(t, plan.u, "Lineage", plan.lin, clean, nil)
		if c.head() != "ok 1 u0 w0" || c.fields["r0.tier"] != "2" || c.fields["r0.pick.type"] != "2" || c.fields["r0.pick.b"] != "9" || c.fields["r0.seq"] != "42" {
			t.Errorf("%s clean: %s %v", plan.name, c.head(), c.fields)
		}
		if got := unityRowLoad(t, plan.u, "Lineage", plan.lin, badTier, nil).head(); got != "refused out_of_range" {
			t.Errorf("C9 %s: %s, want refused out_of_range", plan.name, got)
		}
		if got := unityRowLoad(t, plan.u, "Lineage", plan.lin, badTag, nil).head(); got != "refused out_of_range" {
			t.Errorf("C10 %s: %s, want refused out_of_range", plan.name, got)
		}
	}

	// Deprecated enum variant: decodes to declared default on scalar field, None (0) inside array, and refuses write.
	depU := fixedproof.ParseCheckUnit(t, "package probe\nenum Mode {\n    Fast\n    Legacy | deprecated\n    Safe\n}\nfixed table D {\n    m Mode = Safe\n    arr [2]Mode\n}\n")
	depE := unityRowEntry(t, depU, "D")
	// Legacy is ordinal 2; Safe is ordinal 3; Fast is ordinal 1.
	depWire := fixedproof.MakeFixedFile(depE.Wire, depE.Layout, []byte{2, 2, 1})
	depGot := unityRowLoad(t, depU, "D", nil, depWire, nil)
	if depGot.head() != "ok 1 u0 w0" || depGot.fields["r0.m"] != "3" || depGot.fields["r0.arr[0]"] != "0" || depGot.fields["r0.arr[1]"] != "1" {
		t.Errorf("deprecated enum decode: %s %v", depGot.head(), depGot.fields)
	}

	// Negative control: disable FixedRangeU so forged ordinal/tag 3 slips past writer's 2.
	nc := unityRowLoad(t, uNew, "Lineage", lin, badTier, plantInFile(t, unitytable.RuntimeFile,
		"if (v < c.Lo || v > c.Hi) { return TableReport.OutOfRange; }",
		"if (c.Src < -1) { return TableReport.OutOfRange; }"))
	if nc.head() == "refused out_of_range" {
		t.Errorf("negative control: dropped FixedRangeU still refused out_of_range")
	}
}

// 4. normalization (C12, C13, R17, compressed float -0.0 -> +0.0 + negative control).
func TestUnityFixedRowNormalization(t *testing.T) {
	slowtest.Gate(t, "dotnet")
	t.Parallel()

	u := fixedproof.ParseCheckUnit(t, "package probe\nfixed table T {\n    flag bool\n    opt ?int32\n    raw float32\n    comp float32 | min = -1.0, max = 1.0, resolution = 0.1\n}\n")
	e := unityRowEntry(t, u, "T")
	if e.Record != 8+14 {
		t.Fatalf("record size = %d, want 22", e.Record)
	}
	mkBody := func(flag, pres byte, opt uint32, rawBits, compBits uint32) []byte {
		b := []byte{flag, pres}
		b = binary.LittleEndian.AppendUint32(b, opt)
		b = binary.LittleEndian.AppendUint32(b, rawBits)
		return binary.LittleEndian.AppendUint32(b, compBits)
	}
	for _, c := range []struct {
		name string
		b    []byte
	}{
		{"C12 bool 0x7f", mkBody(0x7f, 1, 42, 0, 0)},
		{"C12 bool 0x02", mkBody(2, 1, 42, 0, 0)},
		{"C13 present 0x5a", mkBody(1, 0x5a, 42, 0, 0)},
		{"C13 present 0xff", mkBody(0, 0xff, 0, 0, 0)},
		{"R17 non-zero absent payload", mkBody(0, 0, 0xffffffff, 0, 0)},
	} {
		if got := unityRowLoad(t, u, "T", nil, fixedproof.MakeFixedFile(e.Wire, e.Layout, c.b), nil).head(); got != "refused wire_damaged" {
			t.Errorf("%s: %s, want refused wire_damaged", c.name, got)
		}
	}

	// Uncompressed float32 preserves -0.0 (0x80000000); compressed float32 normalizes -0.0 -> +0.0 (0x00000000).
	negZeroIn := fixedproof.MakeFixedFile(e.Wire, e.Layout, mkBody(1, 1, 42, 0x80000000, 0x80000000))
	got := unityRowLoad(t, u, "T", nil, negZeroIn, nil)
	if got.head() != "ok 1 u0 w0" || got.fields["r0.flag"] != "true" || got.fields["r0.opt_present"] != "true" || got.fields["r0.opt"] != "42" {
		t.Errorf("R17 lawful bytes: %s %v", got.head(), got.fields)
	}
	resBody := got.resaved[len(got.resaved)-14:]
	if rawOut := binary.LittleEndian.Uint32(resBody[6:10]); rawOut != 0x80000000 {
		t.Errorf("uncompressed float32 -0.0 changed to %#08x", rawOut)
	}
	if compOut := binary.LittleEndian.Uint32(resBody[10:14]); compOut != 0x00000000 {
		t.Errorf("compressed float32 -0.0 did not normalize to +0.0: %#08x", compOut)
	}

	// Negative control: widen FixedBool check from > 1 to > 255.
	nc := unityRowLoad(t, u, "T", nil, fixedproof.MakeFixedFile(e.Wire, e.Layout, mkBody(0x7f, 1, 42, 0, 0)),
		plantInFile(t, unitytable.RuntimeFile,
			"if (body[c.Src] > 1) { return TableReport.WireDamaged; }",
			"if (body[c.Src] > 255) { return TableReport.WireDamaged; }"))
	if nc.head() == "refused wire_damaged" {
		t.Errorf("negative control: widened bool check still refused 0x7f")
	}
}

// 5. reports (R15, R16, R27, R28): stale report reset, once-per-endpoint Unknown
// census over multiple records and array elements, LayoutHash on unknown/retired,
// guarded dynamic widened count, zero counters on refusal + negative control.
func TestUnityFixedRowReports(t *testing.T) {
	slowtest.Gate(t, "dotnet")
	t.Parallel()

	older := fixedproof.ParseCheckUnit(t, "package probe\ntype Item {\n    a int32\n    drop int32\n}\nfixed table C {\n    lead uint32 = 1\n    items [4]Item\n    extra uint16\n    trail uint32 = 2\n}\n")
	newer := fixedproof.ParseCheckUnit(t, "package probe\ntype Item {\n    a int32\n}\nfixed table C {\n    lead uint32 = 1\n    items [4]Item\n    trail uint32 = 2\n}\n")
	three := unityRowSave(t, older, "C", "        r[2].Lead = 5U;\n        r[2].Items.E3.A = 99;\n        r[2].Trail = 6U;", 3)
	lin := unityRowLineage(t, older, "C")
	got := unityRowLoad(t, newer, "C", lin, three, nil)
	// Two unknown fields in writer closure (Item.drop counted ONCE across [4]Item, plus C.extra = 2 total across 3 records!).
	if got.head() != "ok 3 u2 w0" || got.fields["r2.lead"] != "5" || got.fields["r2.items[3].a"] != "99" || got.fields["r2.trail"] != "6" {
		t.Errorf("R16 census over 3 records and 4 elements: %s %v, want ok 3 u2 w0", got.head(), got.fields)
	}

	// LayoutHash populated on layout_unknown and layout_unsupported, 0 on clean and other refusals.
	retiredEntry := unityRowEntry(t, older, "C")
	retiredEntry.Retired = true
	retLin := map[string][]fixedproof.LineageEntry{"C": {retiredEntry}}
	files, err := unityGenerateFiles(newer, retLin)
	if err != nil {
		t.Fatal(err)
	}
	probe := `using System;
using System.IO;
using Probe;
using static Probe.Schema;

static class Program
{
    static int Main()
    {
        byte[] retiredWire = File.ReadAllBytes("retired.bin");
        byte[] unknownWire = (byte[])retiredWire.Clone();
        unknownWire[8] ^= 0x01;
        C[] dst = new C[4];
        TableReport r = new TableReport { Unknown = 77, Widened = 88, LayoutHash = 99 };
        long n1 = Schema.CFixedLoad(dst, retiredWire, ref r);
        if (n1 != -1 || !r.Refused || r.Reason != TableReport.LayoutUnsupported || r.LayoutHash == 0 || r.Unknown != 0 || r.Widened != 0)
        {
            Console.WriteLine("FAIL retired: " + r.Reason + " hash=" + r.LayoutHash);
            return 1;
        }
        r = new TableReport { Unknown = 77, Widened = 88, LayoutHash = 0 };
        long n2 = Schema.CFixedLoad(dst, unknownWire, ref r);
        if (n2 != -1 || !r.Refused || r.Reason != TableReport.LayoutUnknown || r.LayoutHash == 0 || r.Unknown != 0 || r.Widened != 0)
        {
            Console.WriteLine("FAIL unknown: " + r.Reason + " hash=" + r.LayoutHash);
            return 1;
        }
        r = new TableReport { Unknown = 77, Widened = 88, LayoutHash = 12345UL };
        long n3 = Schema.CFixedLoad(dst, new byte[] { 3, 0, 0 }, ref r);
        if (n3 != -1 || !r.Refused || r.Reason != TableReport.Truncated || r.LayoutHash != 0 || r.Unknown != 0 || r.Widened != 0)
        {
            Console.WriteLine("FAIL truncated: " + r.Reason + " hash=" + r.LayoutHash);
            return 1;
        }
        Console.WriteLine("OK");
        return 0;
    }
}
`
	out, code, _, runErr := runUnityProbe(t, files, probe, map[string][]byte{"retired.bin": three})
	if runErr != nil || code != 0 || strings.TrimSpace(out) != "OK" {
		t.Fatalf("LayoutHash report probe failed (code=%d err=%v): %s", code, runErr, out)
	}

	// Negative control: remove report = default; reset on entry (stale counters 99/99 survive on identity lane).
	ownClean := unityRowSave(t, newer, "C", "", 1)
	if cleanRes := unityRowLoad(t, newer, "C", lin, ownClean, nil); cleanRes.head() != "ok 1 u0 w0" {
		t.Fatalf("clean own load: %s, want ok 1 u0 w0", cleanRes.head())
	}
	nc := unityRowLoad(t, newer, "C", lin, ownClean, plantInFile(t, "probe.Tables.cs",
		"CFixedLoadWalk(Span<C> values, ReadOnlySpan<byte> data, ref TableReport report, int planCapacity)\n        {\n            report = default;",
		"CFixedLoadWalk(Span<C> values, ReadOnlySpan<byte> data, ref TableReport report, int planCapacity)\n        {\n            // report = default;"))
	if nc.head() == "ok 1 u0 w0" {
		t.Errorf("negative control: un-reset report still produced %s", nc.head())
	}
}

// 6. array-bounds (C1, C2, R11): writer's counted-array bound held across widening + negative control.
func TestUnityFixedRowArrayBounds(t *testing.T) {
	slowtest.Gate(t, "dotnet")
	t.Parallel()

	older := fixedproof.ParseCheckUnit(t, "package probe\nfixed table G {\n    lead uint32 = 1\n    vals [..4]int32\n    trail uint32 = 2\n}\n")
	newer := fixedproof.ParseCheckUnit(t, "package probe\nfixed table G {\n    lead uint32 = 1\n    vals [..8]int32\n    trail uint32 = 2\n}\n")
	base := unityRowSave(t, older, "G", "        r[0].ValsCount = 4;\n        r[0].Vals[0] = 10;\n        r[0].Vals[1] = 20;\n        r[0].Vals[2] = 30;\n        r[0].Vals[3] = 40;", 1)
	countAt := unityRowBodyOffset(base, unityRowEntry(t, older, "G").Record, 0) + unityRowFieldOffset(t, older, "G", "vals")
	withCount := func(n uint32) []byte {
		b := slices.Clone(base)
		binary.LittleEndian.PutUint32(b[countAt:], n)
		return b
	}
	lin := unityRowLineage(t, older, "G")
	for _, c := range []struct {
		name  string
		count uint32
		want  string
	}{
		{"C1 count -1", 0xFFFFFFFF, "refused count_over_bound"},
		{"C2 count 7 past writer's 4", 7, "refused count_over_bound"},
		{"C2 count 5 past writer's 4", 5, "refused count_over_bound"},
		{"R11 count 4 writer's bound", 4, "ok 1 u0 w0"},
	} {
		got := unityRowLoad(t, newer, "G", lin, withCount(c.count), nil)
		if got.head() != c.want || (c.count == 4 && (got.fields["r0.vals_count"] != "4" || got.fields["r0.vals[3]"] != "40")) {
			t.Errorf("%s: %s %v, want %s", c.name, got.head(), got.fields, c.want)
		}
	}

	// Negative control: widen FixedCountCheck upper bound to reader's 8.
	nc := unityRowLoad(t, newer, "G", lin, withCount(5), plantInFile(t, unitytable.RuntimeFile,
		"if (cnt < (long)c.Lo || cnt > (long)c.Hi) { return TableReport.CountOverBound; }",
		"if (cnt < (long)c.Lo || cnt > 8L) { return TableReport.CountOverBound; }"))
	if nc.head() == "refused count_over_bound" {
		t.Errorf("negative control: reader's bound 8 still refused writer's count 5")
	}
}

// 7. text (C3, C4, R24): UTF-8 validity, NUL in used bytes, length bound, dead slack zero, writer contract + negative control.
func TestUnityFixedRowText(t *testing.T) {
	slowtest.Gate(t, "dotnet")
	t.Parallel()

	u := fixedproof.ParseCheckUnit(t, "package probe\nfixed table T {\n    s string(4)\n}\n")
	clean := unityRowSave(t, u, "T", "        ProbeCommon.SetUtf8(r[0].S.AsSpan(), out r[0].SLength, \"é\");", 1)
	body := len(clean) - 8
	cut := func(fn func(b []byte)) []byte { b := slices.Clone(clean); fn(b); return b }
	for _, c := range []struct {
		name string
		file []byte
		want string
	}{
		{"C3 length 5 of 4", cut(func(b []byte) { b[body] = 5 }), "refused length_over_bound"},
		{"C3 length -1", cut(func(b []byte) { b[body], b[body+1], b[body+2], b[body+3] = 0xff, 0xff, 0xff, 0xff }), "refused length_over_bound"},
		{"C4 0xff in used bytes", cut(func(b []byte) { b[body+4] = 0xff }), "refused text_ill_formed"},
		{"C4 NUL in used bytes", cut(func(b []byte) { b[body+4], b[body+5] = 'a', 0 }), "refused text_ill_formed"},
		{"R24 0xff in slack", cut(func(b []byte) { b[body+6], b[body+7] = 0xff, 0xff }), "refused wire_damaged"},
	} {
		if got := unityRowLoad(t, u, "T", nil, c.file, nil).head(); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	gotClean := unityRowLoad(t, u, "T", nil, clean, nil)
	if gotClean.fields["r0.s"] != `"é"` || gotClean.fields["r0.s_length"] != "2" || !bytes.Equal(gotClean.resaved, clean) {
		t.Errorf("clean UTF-8 record: %s %v", gotClean.head(), gotClean.fields)
	}

	// Negative control: disable TextValid in FixedStringCheck and writer assert.
	badUtf8 := cut(func(b []byte) { b[body+4] = 0xff })
	nc := unityRowLoad(t, u, "T", nil, badUtf8, func(files map[string][]byte) {
		plantInFile(t, unitytable.RuntimeFile,
			"if (c.Code == FixedStringCheck && !TextValid(body.Slice(c.Src + 4, (int)len))) { return TableReport.TextIllFormed; }",
			"if (c.Src < -1) { return TableReport.TextIllFormed; }")(files)
		plantInFile(t, "probe.Tables.cs",
			"Debug.Assert(TableWire.TextValid(sbuf.AsSpan().Slice(0, slen)), \"s is not valid UTF-8\");",
			"")(files)
	})
	if nc.count != 1 {
		t.Errorf("negative control: dropped UTF-8 check still refused: %s", nc.head())
	}
}

// 8. hostile-input (P3, P4, CRC-32C integrity, address-space ceiling + negative control).
func TestUnityFixedRowHostileInput(t *testing.T) {
	slowtest.Gate(t, "dotnet")
	t.Parallel()
	fixedproof.SetMemoryCeiling(t)

	const hostSchema = "package probe\nfixed table Host {\n    small int32 = 5 | min = 0, max = 1000\n    marks [..4]int32 | min = 0, max = 1000\n    note ?int32 | min = 0, max = 1000\n    flag bool\n    tail int32 = 7\n}\n"
	u := fixedproof.ParseCheckUnit(t, hostSchema)
	bodyBytes := unityRowEntry(t, u, "Host").Record - 8
	wantCases := 6 * bodyBytes

	runSweep := func(mutate func(map[string][]byte)) string {
		files, err := unityGenerateFiles(u, nil)
		if err != nil {
			t.Fatal(err)
		}
		if mutate != nil {
			mutate(files)
		}
		probe := `using System;
using System.Buffers.Binary;
using Probe;
using static Probe.Schema;

static class Program
{
    static int Main()
    {
        ProbeCommon.InstallAssertTrap();
        Host v = Schema.NewHost();
        v.Small = 500;
        v.MarksCount = 2;
        v.Marks[0] = 1;
        v.Marks[1] = 2;
        v.NotePresent = true;
        v.Note = 3;
        v.Flag = true;
        byte[] wire = new byte[Schema.HostFixedMeasure(1)];
        Schema.HostFixedSave(new Host[] { v }, wire);
        int body = 20 + (int)BinaryPrimitives.ReadUInt32LittleEndian(wire.AsSpan(16, 4)) + 8;
        byte[] pokes = new byte[] { 0x00, 0x01, 0x02, 0x7f, 0x80, 0xff };
        int fails = 0;
        int cases = 0;
        for (int off = 0; off < Schema.HostFixedBodyBytes; off++)
        {
            foreach (byte b in pokes)
            {
                cases++;
                byte[] bad = (byte[])wire.Clone();
                bad[body + off] = b;
                Host[] back = new Host[] { Schema.NewHost() };
                back[0].Small = 4242;
                back[0].Tail = 4242;
                TableReport report = default;
                long n = Schema.HostFixedLoad(back, bad, ref report);
                bool ok = true;
                if (n == -1)
                {
                    string rname = TableReport.ReasonName(report.Reason);
                    if (rname != "out_of_range" && rname != "count_over_bound" && rname != "wire_damaged")
                    {
                        ok = false;
                    }
                    // R4: a refusal resets every row handed in to its declared default (small 5, tail 7).
                    if (back[0].Small != 5 || back[0].Tail != 7 || back[0].MarksCount != 0 || back[0].NotePresent || back[0].Flag)
                    {
                        ok = false;
                    }
                }
                else if (n == 1)
                {
                    Host h = back[0];
                    if (h.Small < 0 || h.Small > 1000 || h.MarksCount < 0 || h.MarksCount > 4) { ok = false; }
                    else
                    {
                        for (int k = 0; k < h.MarksCount; k++)
                        {
                            if (h.Marks[k] < 0 || h.Marks[k] > 1000) { ok = false; }
                        }
                    }
                    if (h.NotePresent && (h.Note < 0 || h.Note > 1000)) { ok = false; }
                }
                else
                {
                    ok = false;
                }
                if (!ok) { fails++; }
            }
        }
        Console.WriteLine(fails + " " + cases);
        return 0;
    }
}
`
		out, code, _, runErr := runUnityProbe(t, files, probe, nil)
		if runErr != nil || code != 0 {
			t.Fatalf("hostile sweep failed (code=%d err=%v): %s", code, runErr, out)
		}
		return strings.TrimSpace(out)
	}

	if got, want := runSweep(nil), fmt.Sprintf("0 %d", wantCases); got != want {
		t.Errorf("P3 hostile sweep: got %q, want %q", got, want)
	}
	if got := runSweep(plantInFile(t, unitytable.RuntimeFile,
		"if (v < unchecked((long)c.Lo) || v > unchecked((long)c.Hi)) { return TableReport.OutOfRange; }",
		"if (c.Src < -1) { return TableReport.OutOfRange; }")); strings.HasPrefix(got, "0 ") {
		t.Errorf("negative control: hostile sweep without FixedRangeS found 0 failures: %s", got)
	}

	two := unityRowSave(t, u, "Host", "", 2)
	two[len(two)-int(unityRowEntry(t, u, "Host").Record)] ^= 1
	if got := unityRowLoad(t, u, "Host", nil, two, nil).head(); got != "refused no_layout" {
		t.Errorf("P3 last record hash: %s, want refused no_layout", got)
	}

	ab := fixedproof.ParseCheckUnit(t, "package probe\nfixed table A {\n    lead uint32 = 0\n    x int32 = 0\n    y int32 = 0\n    trail uint32 = 0\n}\nfixed table B {\n    p int8 = 0\n    q int8 = 0\n}\n")
	aFile := unityRowSave(t, ab, "A", "        r[0].Lead = 0xAABBCCDDU;\n        r[0].X = 111;\n        r[0].Y = 222;\n        r[0].Trail = 0x11223344U;", 1)
	if got := unityRowLoad(t, ab, "B", nil, aFile, nil).head(); got != "refused layout_unknown" {
		t.Errorf("P4 B on A's file: %s, want refused layout_unknown", got)
	}
	asB := slices.Clone(aFile)
	binary.LittleEndian.PutUint64(asB[8:], unityRowEntry(t, ab, "B").Wire)
	if got := unityRowLoad(t, ab, "B", nil, asB, nil).head(); got != "refused layout_malformed" {
		t.Errorf("P4 A announced as B: %s, want refused layout_malformed", got)
	}

	// CRC-32C | integrity = "crc32c" roundtrip + damage refusal.
	crcU := fixedproof.ParseCheckUnit(t, "package probe\nfixed table Sealed | integrity = \"crc32c\" {\n    a int32 = 7\n    b uint64 = 99\n}\n")
	crcClean := unityRowSave(t, crcU, "Sealed", "        r[0].A = -42;\n        r[0].B = 0x123456789ABCDEF0UL;", 1)
	crcGot := unityRowLoad(t, crcU, "Sealed", nil, crcClean, nil)
	if crcGot.head() != "ok 1 u0 w0" || crcGot.fields["r0.a"] != "-42" || !bytes.Equal(crcGot.resaved, crcClean) {
		t.Errorf("CRC-32C clean roundtrip: %s %v", crcGot.head(), crcGot.fields)
	}
	crcDamaged := slices.Clone(crcClean)
	crcDamaged[len(crcDamaged)-1] ^= 0x01
	if got := unityRowLoad(t, crcU, "Sealed", nil, crcDamaged, nil).head(); got != "refused integrity_mismatch" {
		t.Errorf("CRC-32C damaged trailer: %s, want refused integrity_mismatch", got)
	}
}
