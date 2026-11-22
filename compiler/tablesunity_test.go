package compiler

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/unitytable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablenames"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// unityTableProbe is the reader every Unity table test generates: two roots
// the core carries (bool, and every integer shape up to 64 bits: the eight
// widths, ranged, and bits(N)) and one it does not (a
// string, a float and a nested table), which still has its struct and its
// descriptor.
const unityTableProbe = `package uprobe

table Toggles
{
    on  bool
    lit bool = true
}

table Numbers
{
    a       int8
    b       int16 = -7
    c       int32
    d       int64
    e       uint8
    f       uint16 = 9
    g       uint32
    h       uint64
    angle   int32 | min = -180, max = 180
    level   uint16 = 10 | min = 10, max = 2000
    mask    bits(6)
    wide    bits(40)
}

table Mixed
{
    n     int32
    name  string(16)
    ratio float32
    child Toggles
}

table WireOnly | forms = "wire"
{
    x int32
}
`

// The writers the oracle encodes with, each a different build of the same
// tables (the ids are the names, so the reader above names their fields):
// one whose ranges and bits are wider (out_of_range), one whose kinds are
// narrower and that carries fields and a node record the reader cannot name
// (widened, unknown), and one whose kinds are another signedness or shape
// (kind_mismatch).
const unityTableWideWriter = `package uprobe

table Numbers
{
    angle   int32 | min = -1000, max = 1000
    level   uint16 | min = 0, max = 5000
    mask    bits(8)
}
`

const unityTableNarrowWriter = `package uprobe

table Node
{
    v int32
}

table Toggles
{
    on   bool
    more int64
    next *Node
}

table Numbers
{
    a     int8
    b     int8
    c     int16
    d     int32
    e     uint8
    f     uint8
    g     uint16
    h     uint32
    angle int16 | min = -180, max = 180
    wide  bits(16)
    extra uint32
}
`

const unityTableMismatchWriter = `package uprobe

table Toggles
{
    on uint8
}

table Numbers
{
    a    uint8
    e    int8
    mask bits(16)
    f    uint32
}
`

// unityGenerate generates a unit for the Unity target in process.
func unityGenerate(t *testing.T, src string) map[string][]byte {
	t.Helper()
	files, err := New().Generate(unitFromSource(t, src), "unity", Options{})
	if err != nil {
		t.Fatalf("--lang unity refused the probe: %v", err)
	}
	return files
}

// TestUnityTableSurfaceAndBoolIntFrame is the table core's gate. The surface:
// every table is a struct with a descriptor naming every field, a table the
// codecs carry has Load, Save and Measure and one they do not has neither,
// and a table-free unit emits no table file. Then, compiled in the Unity
// dialect and driven by the conformance driver frame, the reads and saves
// agree with the Go engine (internal/tablewire) on every wire of a seeded
// corpus — the oracle's own saves from four builds of the tables and their
// mutants — verdict, unknown and widened counters, and the re-saved bytes; the
// frame answers the wire surface byte for byte, a root it does not carry
// absent, and every other surface absent. Each refusal class the core adds is
// sabotaged in a copy of the generated code, and the agreement must go red on
// the case that class decides.
func TestUnityTableSurfaceAndBoolIntFrame(t *testing.T) {
	t.Parallel()
	files := unityGenerate(t, unityTableProbe)
	tables := string(files["Probe.Tables.cs"])
	runtime := string(files[unitytable.RuntimeFile])
	if tables == "" || runtime == "" {
		t.Fatalf("--lang unity emitted no table files for a unit with tables: %v", slices.Sorted(maps.Keys(files)))
	}
	for _, want := range []string{
		"public struct Toggles", "public struct Numbers", "public struct Mixed", "public struct WireOnly",
		"public static TableTypeInfo TogglesTableType =>", "public static TableTypeInfo NumbersTableType =>",
		"public static TableTypeInfo MixedTableType =>", "public static TableTypeInfo WireOnlyTableType =>",
		"public static bool NumbersLoad(ref Numbers value, ReadOnlySpan<byte> bytes, ref TableReport report)",
		"public static long NumbersSave(ref Numbers value, Span<byte> buffer)",
		"public static long NumbersMeasure(ref Numbers value)",
		"public static bool MixedLoad(ref Mixed value, ReadOnlySpan<byte> bytes, ref TableReport report)",
		"public static long MixedSave(ref Mixed value, Span<byte> buffer)",
		"public static long MixedMeasure(ref Mixed value)",
		"public static void InitNumbers(ref Numbers value)", "public static Numbers NewNumbers()",
		"public sbyte A;", "public short B;", "public int C;", "public long D;", "public byte E;", "public ushort F;",
		"public uint G;", "public ulong H;", "public uint Mask;", "public ulong Wide;",
		"public Buffer16<byte> Name;", "public int NameLength;", "public float Ratio;", "public Toggles Child;",
		"WriteValidRaw = delegate (ulong raw) { int v = unchecked((int)(long)raw); return v >= -180 && v <= 180; }",
	} {
		if !strings.Contains(tables, want) {
			t.Errorf("Probe.Tables.cs lacks %q", want)
		}
	}
	for _, unwanted := range []string{"WireOnlyFromJson", "WireOnlyToJson", "WireOnlyToJsonMeasure", "WireOnlyCook", "WireOnlyCookMeasure"} {
		if strings.Contains(tables, unwanted) {
			t.Errorf("Probe.Tables.cs carries %q for a table whose forms include wire only", unwanted)
		}
	}
	for _, want := range []string{"internal static partial class TableWire", "internal static class TableJson", "public struct TableReport", "public sealed class TableFieldInfo", "public sealed class TableTypeInfo", "public sealed class TableRetain", "ref struct Reader"} {
		if !strings.Contains(runtime, want) {
			t.Errorf("%s lacks %q", unitytable.RuntimeFile, want)
		}
	}
	// every field has a descriptor row
	for _, row := range []string{`Name = "on"`, `Name = "angle"`, `Name = "wide"`, `Name = "name"`, `Name = "ratio"`, `Name = "child"`, `Name = "n"`, `Name = "x"`} {
		if !strings.Contains(tables, row) {
			t.Errorf("Probe.Tables.cs has no descriptor row %q", row)
		}
	}
	allUnityGenerate := unityGenerate(t, packetSrc)
	if len(allUnityGenerate) == 0 {
		t.Fatal("unityGenerate(t, packetSrc) is empty: the loop below would check nothing")
	}
	for name := range allUnityGenerate {
		if strings.HasSuffix(name, ".Tables.cs") || name == unitytable.RuntimeFile {
			t.Errorf("a unit with no table emitted the table file %s", name)
		}
	}

	slowtest.Gate(t, "dotnet")
	dotnet := findDotnet()
	if dotnet == "" {
		t.Fatal("dotnet is not on PATH")
	}
	h := newUnityTableHarness(t, dotnet)
	gen := filepath.Join(h.tmp, "gen")
	writeFiles(t, gen, files)

	reader := unitFromSource(t, unityTableProbe)
	cases := unityTableCorpus(t, reader)
	list := filepath.Join(h.tmp, "verdicts.list")
	var lines strings.Builder
	for i, c := range cases {
		path := filepath.Join(h.tmp, "wires", fmt.Sprintf("%04d.bin", i))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, c.wire, 0o644); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&lines, "probe %s %s\n", c.root, path)
	}
	if err := os.WriteFile(list, []byte(lines.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	want := make([]string, len(cases))
	for i, c := range cases {
		want[i] = c.want
	}

	// the build under test, and each sabotage beside it, in parallel
	type sabotage struct {
		name, file, from, to, decides string
	}
	sabotages := []sabotage{
		{"first-use order", unitytable.RuntimeFile, "if (w.Fail == WireWhole && (w.OutOfOrder || w.Used != ids.Length / 8)) { w.Fail = WireDamaged; }", "if (w.Fail == WireWhole && (w.OutOfOrder && w.Used < 0)) { w.Fail = WireDamaged; }", "unused-id-entry"},
		{"form byte", unitytable.RuntimeFile, "if (bytes[0] != 1)", "if (bytes[0] == 255)", "form-3"},
		{"out_of_range", "Probe.Tables.cs", "r.Refused = true; r.Reason = \"out_of_range\";", "r.Refused = false;", "angle-out-of-range"},
		{"kind_mismatch", unitytable.RuntimeFile, "return Fail(report, \"kind_mismatch\");", "return true;", "on-kind-mismatch"},
		{"widened", unitytable.RuntimeFile, "if (Widen(from, to)) { report.Widened++; return true; }", "if (Widen(from, to)) { return true; }", "narrow-writer"},
		{"bool byte", unitytable.RuntimeFile, "if (kind == 1 && boolRaw > 1)", "if (kind == 0 && boolRaw > 1)", "bool-byte-2"},
		{"canonical LEB128", unitytable.RuntimeFile, "if (i > 0 && c == 0)", "if (i > 99 && c == 0)", "noncanonical-ref"},
		{"unreachable record", unitytable.RuntimeFile, "for (int k = 1; k < n; k++) { if (mark[k] == 0) { Damage(); return; } }", "for (int k = 1; k < n; k++) { if (mark[k] == 3) { Damage(); return; } }", "unreachable-record"},
	}
	// at most four builds at once
	slots := make(chan struct{}, 4)
	var wg sync.WaitGroup
	var driverErr error
	wg.Go(func() {
		slots <- struct{}{}
		defer func() { <-slots }()
		driverErr = h.buildDriver()
	})
	unitErrs := make([]error, len(sabotages)+1)
	for i := 0; i <= len(sabotages); i++ {
		dir := gen
		if i > 0 {
			s := sabotages[i-1]
			dir = filepath.Join(h.tmp, fmt.Sprintf("planted%d", i))
			planted := maps.Clone(files)
			next := strings.ReplaceAll(string(files[s.file]), s.from, s.to)
			if next == string(files[s.file]) {
				t.Fatalf("sabotage %s patched nothing in %s", s.name, s.file)
			}
			planted[s.file] = []byte(next)
			writeFiles(t, dir, planted)
		}
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			unitErrs[i] = h.buildUnit(dir, filepath.Join(h.tmp, fmt.Sprintf("units%d", i), "probe"), fmt.Sprintf("u%d", i))
		})
	}
	wg.Wait()
	if driverErr != nil {
		t.Fatal(driverErr)
	}
	for i, err := range unitErrs {
		if err != nil {
			t.Fatalf("build %d: %v", i, err)
		}
	}

	got := h.verdicts(filepath.Join(h.tmp, "units0"), list)
	if len(got) != len(cases) {
		t.Fatalf("the driver answered %d verdicts for %d wires", len(got), len(cases))
	}
	disagree := 0
	for i := range cases {
		if got[i] != want[i] {
			disagree++
			if disagree <= 10 {
				t.Errorf("case %s (%s): unity %q, the Go engine %q", cases[i].name, cases[i].root, got[i], want[i])
			}
		}
	}
	if disagree > 0 {
		t.Fatalf("unity disagrees with the Go engine on %d of %d wires", disagree, len(cases))
	}
	reads, refusals := 0, 0
	for _, w := range want {
		if strings.HasPrefix(w, "read ") {
			reads++
		} else if w == "refused" {
			refusals++
		}
	}
	t.Logf("unity agrees with the Go engine on %d wires: %d read, %d refused, %d absent", len(cases), reads, refusals, len(cases)-reads-refusals)

	h.wireSurface(cases)

	for i, s := range sabotages {
		planted := h.verdicts(filepath.Join(h.tmp, fmt.Sprintf("units%d", i+1)), list)
		named := false
		for j := range cases {
			if planted[j] != want[j] && strings.HasPrefix(cases[j].name, s.decides) {
				named = true
				break
			}
		}
		if !named {
			t.Errorf("NEGATIVE CONTROL: with the %s check sabotaged, unity still agrees with the Go engine on every %s case", s.name, s.decides)
			continue
		}
		t.Logf("negative control: the %s sabotage disagrees on %s, as it must", s.name, s.decides)
	}
}

func writeFiles(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// unityTableCase is one wire the driver reads, and the Go engine's answer.
type unityTableCase struct {
	name, root string
	wire       []byte
	want       string
}

// oracleAnswer is the Go engine's read of wire into the reader's root, in the
// driver's --verdicts spelling.
func oracleAnswer(t *testing.T, reader *ir.Unit, root string, wire []byte) string {
	t.Helper()
	m := tabletext.NewModel(reader)
	inst := m.New(reader.Tables[root])
	var report tabletext.Report
	ok, _ := tablewire.Decode(m, inst, wire, &report)
	if !ok {
		return "refused"
	}
	saved, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the Go engine cannot re-save its own read of %s: %v", root, err)
	}
	return fmt.Sprintf("read %d %d %s", report.Unknown, report.Widened, hex.EncodeToString(saved))
}

// fieldRange is the range a writer may put in an integer field.
func fieldRange(f *ir.Field) (lo, hi *big.Int) {
	switch f.Type.Kind {
	case ir.TInt, ir.TFixed:
		if l, h, ok := ir.TableRawRange(f); ok {
			return l, h
		}
	case ir.TBits:
		return new(big.Int), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
	}
	bits := 8 * ir.TableKindWidth(ir.TableWireFieldKind(f))
	one := big.NewInt(1)
	if f.Type.Signed {
		return new(big.Int).Neg(new(big.Int).Lsh(one, uint(bits-1))), new(big.Int).Sub(new(big.Int).Lsh(one, uint(bits-1)), one)
	}
	return new(big.Int), new(big.Int).Sub(new(big.Int).Lsh(one, uint(bits)), one)
}

// setInt puts v in an integer cell the way the engine holds it.
func setInt(fv *tabletext.Field, v *big.Int) {
	if ir.TableKindWide(ir.TableScalarKind(fv.Def)) {
		fv.Cell.Wide = new(big.Int).Set(v)
		return
	}
	if fv.Def.Type.Signed && fv.Def.Type.Kind != ir.TBits {
		fv.Cell.I = v.Int64()
		fv.Cell.U = uint64(v.Int64())
		return
	}
	fv.Cell.U = v.Uint64()
	fv.Cell.I = int64(v.Uint64())
}

// randomInst fills every bool and integer field of a fresh instance: a third
// left at its default, the rest anywhere in range, the ends twice as often.
func randomInst(m *tabletext.Model, st *ir.Struct, r *rand.Rand) *tabletext.Instance {
	inst := m.New(st)
	for i := range inst.Fields {
		fv := &inst.Fields[i]
		f := fv.Def
		if r.IntN(3) == 0 || f.Type.Pointer || f.Array != ir.ArrayNone {
			continue
		}
		switch f.Type.Kind {
		case ir.TBool:
			fv.Cell.B = r.IntN(2) == 1
		case ir.TInt, ir.TFixed, ir.TBits:
			lo, hi := fieldRange(f)
			var v *big.Int
			switch r.IntN(5) {
			case 0:
				v = lo
			case 1:
				v = hi
			default:
				span := new(big.Int).Add(new(big.Int).Sub(hi, lo), big.NewInt(1))
				pick := new(big.Int).SetUint64(r.Uint64())
				pick.Lsh(pick, 64).Or(pick, new(big.Int).SetUint64(r.Uint64()))
				v = new(big.Int).Add(lo, pick.Mod(pick, span))
			}
			setInt(fv, v)
		}
	}
	return inst
}

func fieldNamed(inst *tabletext.Instance, name string) *tabletext.Field {
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return &inst.Fields[i]
		}
	}
	return nil
}

func encodeOrFail(t *testing.T, m *tabletext.Model, inst *tabletext.Instance) []byte {
	t.Helper()
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the Go engine refused to save %s: %v", inst.Def.Name, err)
	}
	return wire
}

// unityTableCorpus is every wire the agreement runs over, seeded: the
// reader's own saves, the three writers' saves, the hand-made cases each
// sabotage is held to, and bit flips, truncations and extensions of all of
// them.
func unityTableCorpus(t *testing.T, reader *ir.Unit) []unityTableCase {
	t.Helper()
	r := rand.New(rand.NewPCG(0x5eed, 0x0c0de))
	var cases []unityTableCase
	add := func(name, root string, wire []byte) {
		cases = append(cases, unityTableCase{name: name, root: root, wire: wire, want: oracleAnswer(t, reader, root, wire)})
	}
	var seeds []unityTableCase
	seed := func(name, root string, wire []byte) {
		add(name, root, wire)
		seeds = append(seeds, cases[len(cases)-1])
	}
	writers := []struct {
		name string
		unit *ir.Unit
	}{
		{"reader", reader},
		{"wide-writer", unitFromSource(t, unityTableWideWriter)},
		{"narrow-writer", unitFromSource(t, unityTableNarrowWriter)},
		{"mismatch-writer", unitFromSource(t, unityTableMismatchWriter)},
	}
	for _, w := range writers {
		m := tabletext.NewModel(w.unit)
		for _, root := range []string{"Toggles", "Numbers"} {
			st := w.unit.Tables[root]
			if st == nil {
				continue
			}
			for i := range 24 {
				inst := randomInst(m, st, r)
				if w.name == "narrow-writer" && root == "Toggles" && i%2 == 0 {
					node := m.New(w.unit.Tables["Node"])
					node.Fields[0].Cell.I, node.Fields[0].Cell.U = int64(i), uint64(i)
					fieldNamed(inst, "next").Cell.Node = node
				}
				seed(fmt.Sprintf("%s-%s-%d", w.name, root, i), root, encodeOrFail(t, m, inst))
			}
		}
	}

	rm := tabletext.NewModel(reader)
	numbers := rm.New(reader.Tables["Numbers"])
	setInt(fieldNamed(numbers, "a"), big.NewInt(-3))
	setInt(fieldNamed(numbers, "c"), big.NewInt(77))
	valid := encodeOrFail(t, rm, numbers)
	// one id-table entry no reference names: only first-use order refuses it
	{
		count := binary.LittleEndian.Uint64(valid[len(valid)-8:])
		body := valid[:len(valid)-8]
		wire := binary.LittleEndian.AppendUint64(append([]byte(nil), body...), ir.TableWireId("nobody"))
		wire = binary.LittleEndian.AppendUint64(wire, count+1)
		add("unused-id-entry", "Numbers", wire)
		// and two entries swapped: first-use order again
		swapped := append([]byte(nil), valid...)
		first := len(valid) - 8 - int(count)*8
		copy(swapped[first:first+8], valid[first+8:first+16])
		copy(swapped[first+8:first+16], valid[first:first+8])
		add("swapped-id-entries", "Numbers", swapped)
	}
	for _, form := range []byte{0, 2, 3} {
		wire := append([]byte(nil), valid...)
		wire[0] = form
		add(fmt.Sprintf("form-%d", form), "Numbers", wire)
	}
	// a non-canonical first reference: 0x01 spelled 0x81 0x00
	add("noncanonical-ref", "Numbers", append([]byte{valid[0], 0x81, 0x00}, valid[2:]...))

	wm := tabletext.NewModel(writers[1].unit)
	wide := wm.New(writers[1].unit.Tables["Numbers"])
	setInt(fieldNamed(wide, "angle"), big.NewInt(500))
	add("angle-out-of-range", "Numbers", encodeOrFail(t, wm, wide))
	wide = wm.New(writers[1].unit.Tables["Numbers"])
	setInt(fieldNamed(wide, "mask"), big.NewInt(200))
	add("mask-out-of-range", "Numbers", encodeOrFail(t, wm, wide))
	wide = wm.New(writers[1].unit.Tables["Numbers"])
	setInt(fieldNamed(wide, "level"), big.NewInt(5))
	add("level-out-of-range", "Numbers", encodeOrFail(t, wm, wide))

	mm := tabletext.NewModel(writers[3].unit)
	mis := mm.New(writers[3].unit.Tables["Toggles"])
	setInt(fieldNamed(mis, "on"), big.NewInt(1))
	add("on-kind-mismatch", "Toggles", encodeOrFail(t, mm, mis))

	toggles := rm.New(reader.Tables["Toggles"])
	fieldNamed(toggles, "on").Cell.B = true
	tw := encodeOrFail(t, rm, toggles)
	// form, ref 1, kind 1, the bool byte
	if tw[2] != byte(ir.TableKindBool) || tw[3] != 1 {
		t.Fatalf("the Toggles wire is not laid out as expected: % x", tw)
	}
	bad := append([]byte(nil), tw...)
	bad[3] = 2
	add("bool-byte-2", "Toggles", bad)

	// a pointer the reader cannot name, and its record: patched to null, the
	// record is unreachable
	nm := tabletext.NewModel(writers[2].unit)
	pointing := nm.New(writers[2].unit.Tables["Toggles"])
	fieldNamed(pointing, "next").Cell.Node = nm.New(writers[2].unit.Tables["Node"])
	setInt(&fieldNamed(pointing, "next").Cell.Node.Fields[0], big.NewInt(5))
	pw := encodeOrFail(t, nm, pointing)
	add("narrow-writer-pointer", "Toggles", pw)
	if pw[2] != byte(ir.TableKindPointer) || pw[3] != 2 {
		t.Fatalf("the pointer wire is not laid out as expected: % x", pw)
	}
	unreachable := append([]byte(nil), pw...)
	unreachable[3] = 0
	add("unreachable-record", "Toggles", unreachable)
	cycle := append([]byte(nil), pw...)
	cycle[3] = 1
	add("cycle-to-root", "Toggles", cycle)

	// an undeclared root answers absent
	wireOnly := rm.New(reader.Tables["WireOnly"])
	cases = append(cases, unityTableCase{name: "undeclared-absent", root: "UndeclaredRoot", wire: encodeOrFail(t, rm, wireOnly), want: "absent"})

	// mutants of every seed
	for _, s := range seeds {
		for k := range 6 {
			wire := append([]byte(nil), s.wire...)
			at := r.IntN(len(wire))
			wire[at] ^= byte(1 << r.IntN(8))
			add(fmt.Sprintf("%s-flip%d", s.name, k), s.root, wire)
		}
		add(s.name+"-cut", s.root, s.wire[:r.IntN(len(s.wire))])
		add(s.name+"-grown", s.root, append(append([]byte(nil), s.wire[:1]...), append([]byte{0}, s.wire[1:]...)...))
	}
	return cases
}

// unityTableHarness builds and runs the Unity driver frame and the generated
// units in a scratch directory, so no build writes into the tree.
type unityTableHarness struct {
	t         *testing.T
	dotnet    string
	root      string
	tmp       string
	serialize string
	env       []string
	driver    string
}

func newUnityTableHarness(t *testing.T, dotnet string) *unityTableHarness {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	h := &unityTableHarness{t: t, dotnet: dotnet, root: root, tmp: t.TempDir()}
	// The dialect compiles serialize.cs's sources as their own assembly; a
	// scratch copy is read at one fixed moment and the sibling is never
	// touched (the packet gate's rule, unitypacket_test.go).
	src := filepath.Join(root, "..", "serialize.cs", "src")
	if _, err := os.Stat(filepath.Join(src, "Serialize.cs")); err != nil {
		t.Fatalf("serialize.cs is not staged beside the tree (make deps): %v", err)
	}
	h.serialize = filepath.Join(h.tmp, "serialize.cs")
	if err := os.MkdirAll(filepath.Join(h.serialize, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Serialize.cs", "Int128Pair.cs", "Serialize.csproj"} {
		b, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(h.serialize, "src", name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	h.env = append(os.Environ(),
		"DOTNET_CLI_TELEMETRY_OPTOUT=1",
		"DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1",
		"DOTNET_NOLOGO=1",
		"NUGET_CERT_REVOCATION_MODE=offline",
	)
	return h
}

func (h *unityTableHarness) build(project, out, artifacts string, props ...string) error {
	args := []string{"build", filepath.Join(h.root, project), "-c", "Debug", "-o", out,
		"--artifacts-path", filepath.Join(h.tmp, "artifacts", artifacts), "--nologo", "-v", "q",
		"-p:RestoreLockedMode=false", "-p:SerializeCsRoot=" + h.serialize}
	args = append(args, props...)
	cmd := testproc.Command(h.t, h.dotnet, args...)
	cmd.Dir = h.root
	cmd.Env = h.env
	if msg, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("dotnet build %s: %w\n%s", project, err, msg)
	}
	return nil
}

func (h *unityTableHarness) buildDriver() error {
	h.driver = filepath.Join(h.tmp, "driver")
	return h.build("test/conformance/unity/unityconformance.csproj", h.driver, "driver")
}

func (h *unityTableHarness) buildUnit(generated, out, artifacts string) error {
	return h.build("test/conformance/unity/unit/schemaunit.csproj", out, artifacts, "-p:UnityUnitDir="+generated)
}

func (h *unityTableHarness) run(env []string, args ...string) (string, int) {
	cmd := testproc.Command(h.t, h.dotnet, append([]string{filepath.Join(h.driver, "unityconformance.dll")}, args...)...)
	cmd.Dir = h.root
	cmd.Env = append(append([]string(nil), h.env...), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			return string(out), exit.ExitCode()
		}
		h.t.Fatalf("run the driver: %v\n%s", err, out)
	}
	return string(out), 0
}

func (h *unityTableHarness) verdicts(units, list string) []string {
	h.t.Helper()
	out := filepath.Join(h.tmp, "verdicts-"+filepath.Base(units)+".txt")
	if msg, code := h.run(nil, "--verdicts", units, list, out); code != 0 {
		h.t.Fatalf("--verdicts exited %d:\n%s", code, msg)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		h.t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// wireSurface drives the frame the way the harness does: `list` names the
// wire surface alone, every other surface is absent (exit 2), and the wire
// surface writes the oracle's own save back byte for byte for every root it
// carries and <case>.absent for the one it does not.
func (h *unityTableHarness) wireSurface(cases []unityTableCase) {
	h.t.Helper()
	manifest := filepath.Join(h.tmp, "MANIFEST.txt")
	var b strings.Builder
	var rows []unityTableCase
	for _, c := range cases {
		if (strings.HasPrefix(c.name, "reader-") && !strings.Contains(c.name, "-flip") && !strings.HasSuffix(c.name, "-cut") && !strings.HasSuffix(c.name, "-grown")) || c.name == "undeclared-absent" {
			rows = append(rows, c)
		}
	}
	undeclared := 0
	for _, c := range rows {
		if c.root == "UndeclaredRoot" {
			undeclared++
		}
	}
	if undeclared != 1 || len(rows) < 2 {
		h.t.Fatalf("wire surface: %d rows with %d undeclared; want the undeclared root once and at least one reader row", len(rows), undeclared)
	}
	for i, c := range rows {
		path := filepath.Join(h.tmp, "surface", fmt.Sprintf("%s.bin", c.name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			h.t.Fatal(err)
		}
		if err := os.WriteFile(path, c.wire, 0o644); err != nil {
			h.t.Fatal(err)
		}
		fmt.Fprintf(&b, "instance case%d probe %s %s\n", i, c.root, path)
	}
	if err := os.WriteFile(manifest, []byte(b.String()), 0o644); err != nil {
		h.t.Fatal(err)
	}
	wantSurfaces := []string{
		"wire", "message", "report", "framing", "retain", "retain-save",
		"json-read", "json-write", "json-hostile",
		"cook", "cook-write", "cook-foreign",
		"block", "block-foreign", "block-dump",
		"forgery", "cook-forgery",
		"write-refusal", "write-refusal-fixed", "write-refusal-message", "write-refusal-json",
	}
	if out, code := h.run(nil, manifest, "list"); code != 0 || !slices.Equal(strings.Fields(out), wantSurfaces) {
		h.t.Errorf("list: exit %d, %q; want %v", code, out, wantSurfaces)
	}
	if out, code := h.run(nil, manifest, "unknown-surface", filepath.Join(h.tmp, "absent-unknown")); code != 2 {
		h.t.Errorf("unknown-surface: exit %d, want 2 (absent):\n%s", code, out)
	}
	outdir := filepath.Join(h.tmp, "wire-out")
	if out, code := h.run([]string{"UNITY_CONFORMANCE_UNITS=" + filepath.Join(h.tmp, "units0")}, manifest, "wire", outdir); code != 0 {
		h.t.Fatalf("wire surface: exit %d\n%s", code, out)
	}
	for i, c := range rows {
		got, err := os.ReadFile(filepath.Join(outdir, fmt.Sprintf("case%d", i)))
		if c.root == "UndeclaredRoot" {
			if _, aerr := os.Stat(filepath.Join(outdir, fmt.Sprintf("case%d.absent", i))); aerr != nil || err == nil {
				h.t.Errorf("wire surface: UndeclaredRoot was not answered absent")
			}
			continue
		}
		if err != nil || !bytes.Equal(got, c.wire) {
			h.t.Errorf("wire surface %s: wrote % x, want the oracle's % x (%v)", c.name, got, c.wire, err)
		}
	}
	h.t.Logf("wire surface: %d cases answered, the undeclared root absent", len(rows))
}

func TestUnityTableCarrySet(t *testing.T) {
	t.Parallel()
	carried, ok := Carried("unity")
	if !ok {
		t.Fatal("unity has no carries registration")
	}
	want := slices.Clone(carryConstructs)
	slices.Sort(want)
	got := slices.Clone(carried)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("unity carries %v, want %v", got, want)
	}
	u := unitFromSource(t, unityTableProbe)
	if !unitytable.Carried(u.Tables["Numbers"]) || !unitytable.Carried(u.Tables["Mixed"]) || !unitytable.Carried(u.Tables["WireOnly"]) {
		t.Error("Carried must hold Numbers, Mixed and WireOnly")
	}
	if unitytable.Carried(nil) {
		t.Error("Carried must refuse nil")
	}
}

func TestUnityTableRuntimeNamesAreClaimed(t *testing.T) {
	t.Parallel()
	source := runtimeSrc + `
table NativeRoot
{
    next *NativeRoot
    values []int32
    keys map[uint32]int32
}
`
	files, err := New().Generate(unitFromSource(t, source), "unity", Options{})
	if err != nil {
		t.Fatal(err)
	}
	namespace := capitalizeFirst(unitFromSource(t, runtimeSrc).Package)
	emitted := csEmittedNames(files, namespace)
	if len(emitted) == 0 {
		t.Fatal("the scan found no Table* identifier in the emitted Unity C#")
	}
	for name := range emitted {
		if !tablenames.Registered(name) {
			t.Errorf("the Unity table emitter spells %s and internal/tablenames does not register it", name)
		}
	}
	allDefinedBy := tablenames.DefinedBy(tablenames.Unity)
	if len(allDefinedBy) == 0 {
		t.Fatal("tablenames.DefinedBy(tablenames.Unity) is empty: the loop below would check nothing")
	}
	for _, name := range allDefinedBy {
		if !emitted[name] {
			t.Errorf("internal/tablenames says Unity defines %s, and nothing emitted spells it", name)
		}
	}

	sabotaged := maps.Clone(files)
	rt, ok := sabotaged[unitytable.RuntimeFile]
	if !ok {
		t.Fatalf("--lang unity emitted no %s", unitytable.RuntimeFile)
	}
	sabotaged[unitytable.RuntimeFile] = append([]byte("namespace Probe\n{\n    public struct TableProbe { }\n    public enum TableProbeKind { A, B }\n}\n\n"), rt...)
	planted := csEmittedNames(sabotaged, namespace)
	if !planted["TableProbe"] || !planted["TableProbeKind"] {
		t.Fatal("the Unity runtime-name scan did not see planted TableProbe / TableProbeKind")
	}
	if tablenames.Registered("TableProbe") || tablenames.Registered("TableProbeKind") {
		t.Fatal("TableProbe / TableProbeKind is registered, so the negative control proves nothing")
	}
}
