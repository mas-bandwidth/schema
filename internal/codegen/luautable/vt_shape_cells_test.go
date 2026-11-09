package luautable_test

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// vtShapeSchema is the probe unit for the six variable-shape cells: a fixed
// table held by value (fixed-class), optional fields, enum-keyed arrays,
// fixed-point and 128-bit scalars (wide-scalars), string/bytes/flags defaults
// on a table renamed under `was` (defaults-and-table-rename), and fields,
// enum variants, union arms and a type field renamed under `was` (renames).
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
    wide fixed(48, 16) | min = -1000, max = 1000
    i128v int128 = -250 | min = -5000000000, max = 5000000000
    u128v uint128
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
	dir := t.TempDir()
	path := filepath.Join(dir, "probe.schema")
	if err := os.WriteFile(path, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("the probe unit does not load: %v", err)
	}
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

func vtShapeU64(v uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, v)
	return b
}

// vtShapeKeyedWire forges a form-1 keyed wire: clean, with the same keyed
// slot twice, with a byte left inside the keyed L after its slots, or with
// a slot payload longer than its scalar width.
func vtShapeKeyedWire(entries [][2]int32, trailing, elemExtra bool) []byte {
	var body []byte
	body = append(body, 4)
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
	wire = append(wire, 16)
	wire = append(wire, vtShapeLeb(uint64(len(body)))...)
	wire = append(wire, body...)
	wire = append(wire, 0)
	ids := []uint64{ir.TableWireId("keyed"), ir.TableWireId("Red"), ir.TableWireId("Green"), ir.TableWireId("Blue")}
	for _, id := range ids {
		wire = append(wire, vtShapeU64(id)...)
	}
	return append(wire, vtShapeU64(uint64(len(ids)))...)
}

const vtShapeProbe = `local inputs = require("./inputs")
local T = require("./ProbeTable")
local R = require("./schema_refusal")
local S = require("./schema_save")
local d = T.RootDescriptor
local function hexstr(b)
	local p = {}
	for i = 0, buffer.len(b) - 1 do p[i + 1] = string.format("%02x", buffer.readu8(b, i)) end
	return table.concat(p)
end
local function one(name)
	local rep = {}
	local v = R.ReadStrict(T, "Root", inputs[name], rep)
	if v == nil then
		print(name .. "=refused:" .. tostring(rep.reason))
	else
		print(name .. "=read|" .. hexstr(S.save(d, v)))
	end
end
`

func vtShapeRun(t *testing.T, schema, main string, inputs map[string][]byte, from, to string) string {
	t.Helper()
	g := generateFiles(t, map[string]string{"Probe.schema": schema})
	if from != "" {
		p := filepath.Join(g.dir, "schema_load.luau")
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(src), from) != 1 {
			t.Fatalf("the sabotage anchor %q does not occur exactly once in the real artifact", from)
		}
		if err := os.WriteFile(p, []byte(strings.Replace(string(src), from, to, 1)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.TrimRight(string(luaurun.Run(t, g.dir, "main.luau", inputs)), "\n")
}

func vtShapeSuite(t *testing.T, from, to string) string {
	t.Helper()
	v1 := vtShapeOracle(t, vtShapeSchema, `{"cell":{"power":5,"label":"a"},"ship":{"name":"s","hull":7},"flag":{"name":"f","hull":3},"opt":3,"optCell":{"power":2},"keyed":{"Red":1,"Blue":3},"keyedCells":{"Green":{"power":9}},"fp":1.5,"ufp":2.5,"wide":-12.25,"i128v":-12345,"u128v":12345,"title":"hello","grade":"Argent","effect":{"shield":{"charge":1.5}},"buff":{"mult":2.0},"clamped":5}`)
	v2 := vtShapeOracle(t, vtShapeSchema, `{}`)
	loose := strings.NewReplacer("max = 1000", "max = 100000", "max = 10", "max = 10000", "max = 8", "max = 8000", "max = 100", "max = 10000", "max = 5000000000", "max = 500000000000").Replace(vtShapeSchema)
	clampedWire := vtShapeOracle(t, loose, `{"cell":{"power":5000},"opt":500,"keyed":{"Red":5000},"fp":100.0,"wide":5000.0,"i128v":99999999999,"clamped":500}`)
	main := vtShapeProbe + "one(\"v1\")\none(\"v2\")\none(\"clampedWire\")\n"
	var b strings.Builder
	b.WriteString(vtShapeRun(t, vtShapeSchema, main, map[string][]byte{"v1": v1, "v2": v2, "clampedWire": clampedWire}, from, to))
	b.WriteString("\n")
	kmain := `local inputs = require("./inputs")
local T = require("./ProbeTable")
local R = require("./schema_refusal")
local function one(name)
	local rep = {}
	local v = R.ReadStrict(T, "Root", inputs[name], rep)
	if v == nil then
		print(name .. "=refused:" .. tostring(rep.reason))
	else
		print(name .. "=read|" .. tostring(v.Keyed[1]) .. "," .. tostring(v.Keyed[3]))
	end
end
one("clean")
one("dup")
one("trailing")
one("elemExtra")
`
	b.WriteString(vtShapeRun(t, vtShapeKeyedSchema, kmain, map[string][]byte{
		"clean":     vtShapeKeyedWire([][2]int32{{2, 11}, {4, 33}}, false, false),
		"dup":       vtShapeKeyedWire([][2]int32{{2, 11}, {2, 22}}, false, false),
		"trailing":  vtShapeKeyedWire([][2]int32{{2, 11}}, true, false),
		"elemExtra": vtShapeKeyedWire([][2]int32{{2, 11}}, false, true),
	}, from, to))
	return b.String()
}

func vtShapeWant(t *testing.T) string {
	t.Helper()
	v1 := vtShapeOracle(t, vtShapeSchema, `{"cell":{"power":5,"label":"a"},"ship":{"name":"s","hull":7},"flag":{"name":"f","hull":3},"opt":3,"optCell":{"power":2},"keyed":{"Red":1,"Blue":3},"keyedCells":{"Green":{"power":9}},"fp":1.5,"ufp":2.5,"wide":-12.25,"i128v":-12345,"u128v":12345,"title":"hello","grade":"Argent","effect":{"shield":{"charge":1.5}},"buff":{"mult":2.0},"clamped":5}`)
	v2 := vtShapeOracle(t, vtShapeSchema, `{}`)
	return fmt.Sprintf("v1=read|%x\nv2=read|%x\nclampedWire=refused:clamped\nclean=read|11,33\ndup=refused:wire_damaged\ntrailing=refused:wire_damaged\nelemExtra=refused:wire_damaged", v1, v2)
}

// TestLuauVtShapeCells holds the strict read (one read: the whole value or
// a refusal) for the fixed-class, optional-fields, enum-keyed-arrays,
// wide-scalars, defaults-and-table-rename and renames cells. Clean wires
// built by schema-go's engine read whole and re-save byte-for-byte; an
// out-of-bounds wire refuses as clamped; and keyed wires with a repeated
// slot, bytes left inside the keyed L, or a slot payload past its width
// refuse as wire_damaged, as internal/tablewire's wirecheck requires.
func TestLuauVtShapeCells(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	got := vtShapeSuite(t, "", "")
	if want := vtShapeWant(t); got != want {
		t.Fatalf("the strict read's verdicts:\n%s\nwant\n%s", got, want)
	}
}

// TestLuauVtShapeCellsNegativeControl plants its fault in the real
// artifact: it drops the repeated-keyed-slot refusal from the generated
// schema_load.luau's wire-alone judge, and requires the repeated slot,
// which must refuse as wire_damaged, to start reading.
func TestLuauVtShapeCellsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	const from = "if seenKeys[key] then return true end"
	const to = "if false then return true end"
	got := vtShapeSuite(t, from, to)
	if want := vtShapeWant(t); got == want {
		t.Fatalf("the planted defect left the row green:\n%s", got)
	}
	if !strings.Contains(got, "dup=read") {
		t.Fatalf("the planted defect did not reach the repeated keyed slot:\n%s", got)
	}
}
