package squirreltable_test

// vt/fixed-class, vt/optional-fields, vt/enum-keyed-arrays, vt/wide-scalars,
// vt/defaults-and-table-rename, vt/renames and vt/wstring
// (docs/roadmap-evidence/squirrel.sexp): the strict read (ReadStrict: the
// whole value or a refusal) for the variable-shape cells. Clean wires built
// by the Go engine read whole and re-save byte-for-byte; an out-of-range
// wire refuses whole; keyed wires with a repeated slot, bytes left inside
// the keyed L, or a slot payload past its width refuse whole. The negative
// control plants its fault in the emitted load runtime (generated from the
// real runtime artifact) and requires the repeated slot to start reading.
// Functional: runs the pinned Squirrel interpreter via sqrun.

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const vtShapeSchema = `package probe

enum Color { Red, Green, Blue }

flags Caps { Jump, Crouch, Fly }

enum Grade
{
    Bronze,
    Argent | was = "Silver"
    Gold
}

type Buff
{
    mult float32 = 1.0 | was = "multiplier"
}

type Boost { power int32 = 0 }
type Ward { charge float32 = 0.0 }

union Effect
{
    boost Boost
    shield Ward | was = "ward"
    pong | was = "ping"
    amount int32
}

fixed table Cell
{
    power int32 = 0 | min = 0, max = 1000
    label string(8) = "cell"
}

fixed table Ship | was = "Vessel"
{
    name string(32) = "untitled"
    tag bytes(4) = "ab"
    caps Caps = { Jump, Fly }
    hull int32 = 100 | min = 0, max = 1000
}

table Root
{
    cell Cell
    ship Ship
    flag *Ship
    opt ?int32 | min = 0, max = 10
    optCell ?Cell
    keyed [Color]int32 | min = 0, max = 100
    keyedCells [Color]Cell
    fp fixed(16, 16) = 1.0 | min = -8, max = 8
    ufp ufixed(16, 16) | min = 0, max = 10
    bigfp fixed(48, 16) | min = -1000, max = 1000
    i128v int128 = -250 | min = -5000000000, max = 5000000000
    u128v uint128
    wideText string(32)
    title string(32) = "hi" | was = "name"
    data bytes(4) = "ab"
    caps Caps = { Jump }
    grade Grade
    effect Effect
    buff Buff
    clamped int32 = 1 | min = 0, max = 10
}
`

const vtShapeKeyedSchema = `package probe

enum Color { Red, Green, Blue }

table Root
{
    keyed [Color]int32
}
`

func vtShapeOracle(t *testing.T, schema, text string) []byte {
	t.Helper()
	u := loadSource(t, schema)
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Root"))
	var rep tabletext.Report
	if !m.Read(inst, []byte(text), &rep) {
		t.Fatalf("the oracle did not read the probe text cleanly: %+v", rep)
	}
	w, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the oracle did not encode the probe value: %v", err)
	}
	return w
}

func vtShapeLeb(v uint64) []byte {
	var o []byte
	for v >= 0x80 {
		o = append(o, byte(v)|0x80)
		v >>= 7
	}
	return append(o, byte(v))
}

func vtShapeKeyedWire(entries [][2]int32, trailing, elemExtra bool) []byte {
	var body []byte
	body = append(body, 4) // element kind i32
	body = append(body, vtShapeLeb(uint64(len(entries)))...)
	for _, e := range entries {
		body = append(body, vtShapeLeb(uint64(e[0]))...)
		payload := make([]byte, 4)
		binary.LittleEndian.PutUint32(payload, uint32(e[1]))
		if elemExtra {
			payload = append(payload, 0xFF)
		}
		body = append(body, vtShapeLeb(uint64(len(payload)))...)
		body = append(body, payload...)
	}
	if trailing {
		body = append(body, 0xFF)
	}
	wire := []byte{1}
	wire = append(wire, vtShapeLeb(1)...)
	wire = append(wire, 16) // TableKindKeyed
	wire = append(wire, vtShapeLeb(uint64(len(body)))...)
	wire = append(wire, body...)
	wire = append(wire, 0)
	ids := []uint64{ir.TableWireId("keyed"), ir.TableWireId("Red"), ir.TableWireId("Green"), ir.TableWireId("Blue")}
	for _, id := range ids {
		wire = binary.LittleEndian.AppendUint64(wire, id)
	}
	return binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
}

const vtShapeProbeHead = `local inputs = dofile("inputs.nut", true);
local HEXB = array(256);
for (local i = 0; i < 256; i++) HEXB[i] = format("%02x", i);
local hexOf = function(b) { if (b == null) return "null"; local s=""; for (local i=0;i<b.len();i++) s+=HEXB[b[i]&0xff]; return s; };
`

func vtShapeRun(t *testing.T, schema, probeBody string, inputs map[string][]byte, patchLoad func(string) string) string {
	t.Helper()
	dir := t.TempDir()
	u := loadSource(t, schema)
	mod := emitUnit(t, dir, "probe", u)
	if patchLoad != nil {
		p := filepath.Join(dir, "probe", "schema_table_load.nut")
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, p, []byte(patchLoad(string(b))))
	}
	main := strings.ReplaceAll(vtShapeProbeHead+probeBody, "__MOD__", mod)
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(main))
	return strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", inputs)), "\n")
}

func vtShapeSuite(t *testing.T, patchLoad func(string) string) string {
	t.Helper()
	v1 := vtShapeOracle(t, vtShapeSchema, `{"cell":{"power":5,"label":"a"},"ship":{"name":"s","hull":7},"flag":{"name":"f","hull":3},"opt":3,"optCell":{"power":2},"keyed":{"Red":1,"Blue":3},"keyedCells":{"Green":{"power":9}},"fp":1.5,"ufp":2.5,"bigfp":-12.25,"i128v":-12345,"u128v":12345,"wideText":"héllo","title":"hello","grade":"Argent","effect":{"shield":{"charge":1.5}},"buff":{"mult":2.0},"clamped":5}`)
	v2 := vtShapeOracle(t, vtShapeSchema, `{}`)
	loose := strings.NewReplacer("max = 1000", "max = 100000", "max = 10", "max = 10000", "max = 8", "max = 8000", "max = 100", "max = 10000", "max = 5000000000", "max = 500000000000").Replace(vtShapeSchema)
	clampedWire := vtShapeOracle(t, loose, `{"cell":{"power":5000},"opt":500,"keyed":{"Red":5000},"fp":100.0,"bigfp":5000.0,"i128v":99999999999,"clamped":500}`)
	main := `local schema = dofile("probe/schema_module.nut", true);
local T = schema.require("__MOD__");
local R = schema.require("schema_table_refusal");
local S = schema.require("schema_table_save");
local one = function(name) {
  local rep = {};
  local v = R.ReadStrict(T, "Root", inputs[name], rep);
  if (v == null) print(name + "=refused:" + rep.reason + "\n");
  else print(name + "=read|" + hexOf(S.save(T, "Root", v)) + "\n");
};
one("v1"); one("v2"); one("clampedWire");
`
	var b strings.Builder
	b.WriteString(vtShapeRun(t, vtShapeSchema, main, map[string][]byte{"v1": v1, "v2": v2, "clampedWire": clampedWire}, patchLoad))
	b.WriteString("\n")
	kmain := `local schema = dofile("probe/schema_module.nut", true);
local T = schema.require("__MOD__");
local R = schema.require("schema_table_refusal");
local one = function(name) {
  local rep = {};
  local v = R.ReadStrict(T, "Root", inputs[name], rep);
  if (v == null) print(name + "=refused:" + rep.reason + "\n");
  else print(name + "=read|" + v.Keyed[0] + "," + v.Keyed[2] + "\n");
};
one("clean"); one("dup"); one("trailing"); one("elemExtra");
`
	b.WriteString(vtShapeRun(t, vtShapeKeyedSchema, kmain, map[string][]byte{
		"clean":     vtShapeKeyedWire([][2]int32{{2, 11}, {4, 33}}, false, false),
		"dup":       vtShapeKeyedWire([][2]int32{{2, 11}, {2, 22}}, false, false),
		"trailing":  vtShapeKeyedWire([][2]int32{{2, 11}}, true, false),
		"elemExtra": vtShapeKeyedWire([][2]int32{{2, 11}}, false, true),
	}, patchLoad))
	return b.String()
}

func vtShapeWant(t *testing.T) string {
	t.Helper()
	v1 := vtShapeOracle(t, vtShapeSchema, `{"cell":{"power":5,"label":"a"},"ship":{"name":"s","hull":7},"flag":{"name":"f","hull":3},"opt":3,"optCell":{"power":2},"keyed":{"Red":1,"Blue":3},"keyedCells":{"Green":{"power":9}},"fp":1.5,"ufp":2.5,"bigfp":-12.25,"i128v":-12345,"u128v":12345,"wideText":"héllo","title":"hello","grade":"Argent","effect":{"shield":{"charge":1.5}},"buff":{"mult":2.0},"clamped":5}`)
	v2 := vtShapeOracle(t, vtShapeSchema, `{}`)
	return fmt.Sprintf("v1=read|%x\nv2=read|%x\nclampedWire=refused:out_of_range\nclean=read|11,33\ndup=refused:malformed\ntrailing=refused:malformed\nelemExtra=refused:wire_damaged", v1, v2)
}

func TestSquirrelVtShapeCells(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	got := vtShapeSuite(t, nil)
	if want := vtShapeWant(t); got != want {
		t.Fatalf("the strict read's verdicts:\n%s\nwant\n%s", got, want)
	}
}

func TestSquirrelVtShapeCellsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	// both repeated-slot judges: the wire-alone check and the decode walk
	anchors := []string{
		`if (key in seen) return wcDamage(c);`,
		`if (keyRef in refsSeen) { report.bodyDamaged = true; break; }`,
	}
	got := vtShapeSuite(t, func(src string) string {
		for _, anchor := range anchors {
			if strings.Count(src, anchor) != 1 {
				t.Fatalf("the sabotage anchor %q does not occur exactly once in the emitted load runtime", anchor)
			}
			src = strings.Replace(src, anchor, strings.Replace(anchor, "if (", "if (false && ", 1), 1)
		}
		return src
	})
	if want := vtShapeWant(t); got == want {
		t.Fatalf("NEGATIVE CONTROL FAILED: disabling the repeated-keyed-slot refusal left the row green:\n%s", got)
	}
	if !strings.Contains(got, "dup=read") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the planted defect did not reach the repeated keyed slot:\n%s", got)
	}
}
