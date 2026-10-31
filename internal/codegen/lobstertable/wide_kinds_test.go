package lobstertable_test

import (
	"encoding/binary"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// wideKindsSchema declares every construct this card carries: a timestamp
// (kind 36, SPEC.md §4.13, docs/SPEC-TABLES.md §2.11), a one-sided minimum
// and a one-sided maximum range (§4.3), an enum (kind 30), wide text (kind
// 33) and a plain float64 whose -0 and subnormal ride bit for bit (a NaN or an
// infinity is refused, finite_test.go). A single table holds them so one wire
// exercises them all.
const wideKindsSchema = `package widekinds

enum Mode { Idle, Busy }

table Sample
{
    opened timestamp
    lower  int64  | min = 0
    upper  uint32 | max = 100
    mode   Mode
    label  wstring(4)
    plain  float64
}
`

// wideWire is one synthetic wire built through the Go reference writer.
func wideWire(t *testing.T, g *loadUnit, opened uint64, lower int64, upper uint64, mode uint64, units []uint16, f float64) []byte {
	t.Helper()
	m := tabletext.NewModel(g.u)
	inst := m.New(m.Lookup("Sample"))
	for i := range inst.Fields {
		switch inst.Fields[i].Def.Name {
		case "opened":
			inst.Fields[i].Cell.I = int64(opened)
			inst.Fields[i].Cell.U = opened
		case "lower":
			inst.Fields[i].Cell.I = lower
			inst.Fields[i].Cell.U = uint64(lower)
		case "upper":
			inst.Fields[i].Cell.U = upper
		case "mode":
			inst.Fields[i].Cell.U = mode
		case "label":
			inst.Fields[i].Cell.Units = units
		case "plain":
			inst.Fields[i].Cell.F = f
		}
	}
	out, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the reference writer refuses the wide-kind sample: %v", err)
	}
	return out
}

// wideLEB is the one canonical LEB128 spelling of a length or id reference.
func wideLEB(v uint64) []byte {
	var out []byte
	for v >= 0x80 {
		out = append(out, byte(v&0x7f)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

// withEscape prepends one unknown field of the ESCAPE KIND, 31 (§3: L then L
// opaque bytes), carrying a fresh id in the id table. It is the only way a
// writer of this major meets the escape, because no writer emits one.
func withEscape(t *testing.T, wire, payload []byte) []byte {
	t.Helper()
	if len(wire) < 9 || wire[0] != 1 {
		t.Fatalf("the sample's wire is not form 1: %x", wire)
	}
	count := binary.LittleEndian.Uint64(wire[len(wire)-8:])
	id := uint64(0x5EED5EED5EED5EED)
	header := append(wideLEB(count+1), byte(31))
	header = append(header, wideLEB(uint64(len(payload)))...)
	header = append(header, payload...)
	rest := append([]byte{}, wire[:len(wire)-8]...)
	rest = binary.LittleEndian.AppendUint64(rest, id)
	rest = binary.LittleEndian.AppendUint64(rest, count+1)
	out := append([]byte{wire[0]}, header...)
	return append(out, rest[1:]...)
}

// wideRowWire is a row's wire, read from its file when the row has no data.
func wideRowWire(t *testing.T, row loadRow) []byte {
	t.Helper()
	if row.data != nil {
		return row.data
	}
	wire, err := os.ReadFile(filepath.Join(root, row.wire))
	if err != nil {
		t.Fatalf("%s: %v", row.name, err)
	}
	return wire
}

// compareWideRows holds load, save and re-load to the Go reference engine for
// every row: the report and the value dump of the load, the save's bytes
// (internal/tablewire.Encode of the reference's decoded instance), and the
// dump of the re-loaded saved bytes.
func compareWideRows(t *testing.T, g *loadUnit, rows []loadRow, constructs []string, counts map[string]int, refusals map[string]int) {
	t.Helper()
	label := strings.Join(constructs, ", ")
	var kept []loadRow
	var want [][]byte
	for _, row := range rows {
		wire := wideRowWire(t, row)
		saved, err := goSave(g, row.root, wire)
		if err != nil {
			t.Logf("%s %s: the Go writer refuses it and it is not compared: %v", label, row.name, err)
			for _, construct := range constructs {
				refusals[construct]++
			}
			continue
		}
		kept = append(kept, row)
		want = append(want, saved)
	}
	if len(kept) == 0 {
		return
	}
	load := runUnit(t, g, kept)
	save := runSaveRows(t, g, kept)
	for i, row := range kept {
		if load.lob[2*i] != load.goLine[i] {
			t.Errorf("%s %s: the report is not the reference's:\nlobster: %s\ngo:      %s", label, row.name, load.lob[2*i], load.goLine[i])
			continue
		}
		if load.lob[2*i+1] != load.goDump[i] {
			t.Errorf("%s %s: the loaded value is not the reference's at byte %s", label, row.name, divergence(load.lob[2*i+1], load.goDump[i]))
			continue
		}
		if got, wantHex := save.saved[i], hex.EncodeToString(want[i]); got != wantHex {
			t.Errorf("%s %s: the saved bytes are not the reference's:\nlobster: %s\ngo:      %s", label, row.name, got, wantHex)
			continue
		}
		_, goDump := goLoad(g.u, row.root, want[i])
		if save.dump[i] != goDump {
			t.Errorf("%s %s: the re-loaded value is not the reference's at byte %s", label, row.name, divergence(save.dump[i], goDump))
			continue
		}
		for _, construct := range constructs {
			counts[construct]++
		}
	}
}

// wideUnitConstructs names the wide kinds a unit's declarations use: a
// timestamp (kind 36), a one-sided integer range, an enum (kind 30) and
// wstring(N) (kind 33). TestLobsterWideKinds generates every manifest unit
// whose declarations name one, so no construct rides an ungenerated unit.
func wideUnitConstructs(u *ir.Unit) []string {
	seen := map[string]bool{}
	scan := func(f *ir.Field) {
		if f == nil {
			return
		}
		if f.Type.Kind == ir.TInt && f.Type.Timestamp {
			seen["timestamp"] = true
		}
		if f.Type.Kind == ir.TWString {
			seen["wstring"] = true
		}
		if f.Type.Kind == ir.TNamed {
			if _, ok := f.Type.Ref.(*ir.Enum); ok {
				seen["enum"] = true
			}
		}
		if f.RangeOneSided {
			seen["one-sided-range (minimum and maximum)"] = true
		}
	}
	walkStruct := func(st *ir.Struct) {
		for _, f := range st.Fields {
			scan(f)
		}
	}
	walkUnion := func(un *ir.Union) {
		for _, v := range un.Variants {
			scan(v.F)
		}
	}
	for _, st := range u.Tables {
		walkStruct(st)
	}
	for _, st := range u.Structs {
		walkStruct(st)
	}
	for _, un := range u.Unions {
		walkUnion(un)
	}
	for _, un := range u.TableUnions {
		walkUnion(un)
	}
	out := make([]string, 0, len(seen))
	for construct := range seen {
		out = append(out, construct)
	}
	sort.Strings(out)
	return out
}

// TestLobsterWideKinds carries the wide kinds across the variable table wire
// (docs/SPEC-TABLES.md §2.11, §3, §4; SPEC.md §4.13; docs/PORT-LOBSTER.md,
// THE LOAD): a timestamp, both one-sided ranges, an enum, an escape and
// wstring(N) on every manifest unit that uses them, plus the card's own
// sample whose plain float64 carries -0 and a subnormal. Each construct is
// named with its count, and load, save and re-load agree with Go's engine
// byte for byte and report for report.
func TestLobsterWideKinds(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	byUnit := map[string][]loadRow{}
	var order []string
	for _, row := range loadRows(t) {
		if _, seen := byUnit[row.unit]; !seen {
			order = append(order, row.unit)
		}
		byUnit[row.unit] = append(byUnit[row.unit], row)
	}
	counts := map[string]int{}
	refusals := map[string]int{}
	for _, key := range order {
		rows := byUnit[key]
		paths, err := compiler.GatherPaths(prefix(units[key]))
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		g, err := generateLoadUnit(t, paths)
		if err != nil {
			t.Logf("unit %s is refused, so its rows are not compared: %v", key, err)
			continue
		}
		constructs := wideUnitConstructs(g.u)
		if len(constructs) == 0 {
			continue
		}
		compareWideRows(t, g, rows, constructs, counts, refusals)
	}

	src := filepath.Join(t.TempDir(), "WideKinds.schema")
	if err := os.WriteFile(src, []byte(wideKindsSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{src})
	if err != nil {
		t.Fatalf("the surface refuses the wide-kind sample: %v", err)
	}
	if len(g.mods) != 1 {
		t.Fatalf("the sample generated %d table modules, want 1", len(g.mods))
	}
	module, err := os.ReadFile(filepath.Join(g.dir, g.mods[0]+".lobster"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(module), "import .wide_kinds") {
		t.Fatalf("the table module does not import wide_kinds, so a program never loads the timestamp shape")
	}
	var negativeTime int64 = -1234567890123
	opened := uint64(negativeTime)
	labelUnits := []uint16{'A', 0xD83D, 0xDE00}
	sample := func(name string, f float64) loadRow {
		return loadRow{kind: "instance", name: name, unit: "widekinds", root: "Sample",
			data: wideWire(t, g, opened, -5, 500, 2, labelUnits, f)}
	}
	finite := []loadRow{
		sample("negative_zero", math.Copysign(0, -1)),
		sample("subnormal", math.Float64frombits(1)),
	}
	// every finite-float row carries the sample's timestamp, both one-sided
	// ranges, its enum and its wstring, so one pass names all six. A NaN or
	// an infinity is refused by every read and the save (finite_test.go).
	compareWideRows(t, g, finite, []string{"finite-float (-0, subnormal)", "timestamp", "one-sided-range (minimum and maximum)", "enum", "wstring"}, counts, refusals)

	escape := sample("escape", 1.0)
	escape.data = withEscape(t, escape.data, []byte{0xde, 0xad, 0xbe, 0xef})
	line, _ := goLoad(g.u, "Sample", escape.data)
	if !strings.HasPrefix(line, "1,") {
		t.Fatalf("the reference does not count the escape as one unknown field (%s): the wire does not exercise kind 31", line)
	}
	compareWideRows(t, g, []loadRow{escape}, []string{"escape"}, counts, refusals)

	for _, name := range []string{"timestamp", "one-sided-range (minimum and maximum)", "enum", "escape", "wstring", "finite-float (-0, subnormal)"} {
		if counts[name] == 0 {
			t.Errorf("%s: no row was compared, so the construct is not carried", name)
		}
		t.Logf("%s: %d rows agree with Go on load, save and re-load", name, counts[name])
	}
	var refused int
	for _, n := range refusals {
		refused += n
	}
	if refused > 0 {
		t.Logf("%d manifest rows were refused by the Go writer and not compared", refused)
	}
}

// TestLobsterWideKindsNegativeControl breaks the timestamp decode in the
// emitted runtime and requires the wide-kind comparison to go red on the
// sample whose timestamp is negative.
func TestLobsterWideKindsNegativeControl(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "WideKinds.schema")
	if err := os.WriteFile(src, []byte(wideKindsSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{src})
	if err != nil {
		t.Fatalf("the surface refuses the wide-kind sample: %v", err)
	}
	row := loadRow{kind: "instance", name: "negative_time", unit: "widekinds", root: "Sample",
		data: wideWire(t, g, negativeTimeOpened(), 1, 2, 1, []uint16{'A'}, 1.0)}
	if got, want := runUnit(t, g, []loadRow{row}).lob[1], goLoadDump(g, row); got != want {
		t.Fatalf("the control, unpatched: the dump is not the reference's at byte %s", divergence(got, want))
	}
	path := filepath.Join(g.dir, "wide_kinds.lobster")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "    cell.i = read_int64_le(r.b, r.off)\n"
	if n := strings.Count(string(data), anchor); n != 1 {
		t.Fatalf("the timestamp anchor is in the emitted runtime %d times, want 1", n)
	}
	patched := strings.Replace(string(data), anchor, "    cell.i = read_int64_le(r.b, r.off) + 1\n", 1)
	if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	got, want := runUnit(t, g, []loadRow{row}).lob[1], goLoadDump(g, row)
	if got == want {
		t.Fatalf("the patched runtime still matches the reference: %s", got)
	}
	t.Logf("the timestamp comparison went red under the patch, as required")
}

func negativeTimeOpened() uint64 {
	var v int64 = -1234567890123
	return uint64(v)
}

// goLoadDump is the reference's value dump for one row's already-built wire.
func goLoadDump(g *loadUnit, row loadRow) string {
	_, dump := goLoad(g.u, row.root, row.data)
	return dump
}
