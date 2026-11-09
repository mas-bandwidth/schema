package mootable_test

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// cookWriteProbe loads inputs.wire through table_load.moo and cooks the value
// through table_cookwrite.moo in both byte orders, printing the little-endian
// file's hex, the big-endian file's hex, the measure, and whether the
// little-endian file opens and reads back (table_cookread.moo) to a value
// equal to the load's. A refusal prints "refuse <name> <field>".
func cookWriteProbe(g *generatedUnit, rootName string) string {
	return cookWriteProbeShared(g, rootName, "", "")
}

// cookWriteProbeShared is cookWriteProbe over a manifest recipe `input=<json>
// share=<from>:<to>` when from is not empty: the value is the canonical JSON
// text (inputs.text) read through the unit's json module, and the slot to is
// handed the slot from's very buffer, the in-memory graph Cook preserves and
// the wire and the text cannot spell (docs/SPEC-TABLES.md §2.5, §7.6), as the
// leg's conformance driver builds it (test/conformance/moo/cook.moo).
func cookWriteProbeShared(g *generatedUnit, rootName, from, to string) string {
	var b strings.Builder
	b.WriteString("--!strict\n" + valueDump + moorunHex)
	b.WriteString(g.requireAll())
	b.WriteString("local inputs = require(\"./inputs\")\nlocal load = require(\"./table_load\")\nlocal F = require(\"./table_fixed\")\nlocal St = require(\"./schema_tables\")\nlocal R = require(\"./table_cookread\")\nlocal W = require(\"./table_cookwrite\")\n")
	fmt.Fprintf(&b, `local report = load.newReport()
local d = St.lookup(%[1]q)
local value: any
if %[2]q ~= "" then
	local J = require("./json")
	local rec = d.new()
	if not J.fromJson(d, rec, inputs.text, {}) then print("input text did not read") return end
	local src: any, dst: any = nil, nil
	for _, fd in d.fields do
		if fd.name == %[2]q then src = fd elseif fd.name == %[3]q then dst = fd end
	end
	if src == nil or dst == nil or rec[src.slot] == nil then print("the share recipe names no two blob fields") return end
	rec[dst.slot] = rec[src.slot]
	value = rec
elseif d.fixed then
	local vals = F.load(d, F.lookup(%[1]q) :: any, inputs.wire, report)
	value = if vals ~= nil then vals[1] else nil
else
	value = load.load(%[1]q, inputs.wire, report)
end
local size, whyMeasure = W.measure(%[1]q, value)
if size == nil then print("refuse " .. whyMeasure.name .. " " .. tostring(whyMeasure.field)) return end
local le, whyLE = W.cook(%[1]q, value, false)
if le == nil then print("refuse " .. whyLE.name .. " " .. tostring(whyLE.field)) return end
local be, whyBE = W.cook(%[1]q, value, true)
if be == nil then print("refuse " .. whyBE.name .. " " .. tostring(whyBE.field)) return end
print(size)
hex(le)
hex(be)
local h, why = R.open(%[1]q, le, 0, buffer.len(le))
if h == nil then print("reopen " .. tostring(why)) return end
local back, refusal = R.read(h)
if back == nil then print("reread " .. refusal.name .. " " .. refusal.field) return end
print(if dump(back) == dump(value) then "reread ok" else "reread differs")
`, rootName, from, to)
	return b.String()
}

// moorunHex is the moo hex(b) printer the cook-write probe prints files with.
const moorunHex = `local function hex(b: buffer): ()
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	print(table.concat(parts))
end
`

// cookWriteRows is every cook-write line of the manifest with its instance:
// the little- and big-endian files `schema cook` wrote from the instance's
// wire.
type cookWriteRow struct {
	row    reportRow
	le, be string
	// input and share are the row's recipe (`input=<json> share=<from>:<to>`):
	// the value is built from the text with one buffer named from two slots,
	// which no wire spells (a blob_shared instance's wire is refused)
	input, from, to string
}

func cookWriteRows(t *testing.T) []cookWriteRow {
	t.Helper()
	instances := map[string]reportRow{}
	for _, r := range manifestRows(t) {
		if r.kind == "instance" {
			instances[r.caseName] = r
		}
	}
	var out []cookWriteRow
	for _, f := range manifestLines(t, "cook-write") {
		inst, ok := instances[f[1]]
		if !ok {
			t.Fatalf("cook-write %s names no instance", f[1])
		}
		cw := cookWriteRow{row: inst, le: f[2], be: f[3]}
		for _, kv := range f[4:] {
			if v, ok := strings.CutPrefix(kv, "input="); ok {
				cw.input = v
			} else if v, ok := strings.CutPrefix(kv, "share="); ok {
				cw.from, cw.to, _ = strings.Cut(v, ":")
			}
		}
		if inst.blobShared && (cw.input == "" || cw.from == "") {
			t.Fatalf("cook-write %s: its instance's wire is refused and the row names no input= and share= recipe", f[1])
		}
		out = append(out, cw)
	}
	if len(out) == 0 {
		t.Fatal("the manifest carries no cook-write row")
	}
	return out
}

// TestMooCookWriteMatchesTheTool is §7.6's gate on the moo leg, the harness's
// `cook-write` surface: each instance's wire loads through the emitted
// table_load.moo, is cooked by the emitted table_cookwrite.moo in BOTH byte
// orders, and the bytes equal the files `schema cook` wrote (the reference),
// byte for byte; CookMeasure answers each file's length; and the little-endian
// file opens and reads back, through table_cookread.moo, to the value that was
// cooked. Every row of the manifest is answered.
func TestMooCookWriteMatchesTheTool(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_cookwrite.moo)")
	t.Parallel()
	rows := cookWriteRows(t)
	fails, answered, cache := cookWriteFailures(t, rows, nil)
	for _, f := range fails {
		t.Error(f)
	}
	if want := 2 * len(rows); answered != want {
		t.Fatalf("answered %d of %d cook-write cases", answered, want)
	}
	t.Logf("%d cook-write cases (%d instances in both byte orders) equal the tool's files", answered, len(rows))
	for key, g := range cache {
		if out := g.analyze(t); out != "" {
			t.Errorf("unit %s: moo-analyze is not silent:\n%s", key, out)
		}
	}
}

// cookWriteFailures runs every cook-write row through the emitted runtime and
// answers what differs from the tool's files, the cases answered clean, and
// the generated units; mutate, when set, plants a defect in a unit's emitted
// files first (the negative controls).
func cookWriteFailures(t *testing.T, rows []cookWriteRow, mutate func(*testing.T, *generatedUnit)) (fails []string, answered int, cache map[string]*generatedUnit) {
	t.Helper()
	units, _ := readManifest(t)
	cache = map[string]*generatedUnit{}
	for _, cw := range rows {
		row := cw.row
		g, ok := cache[row.unit]
		if !ok {
			g = generateUnit(t, units, row.unit)
			if g.err != nil {
				t.Fatalf("unit %s: %v", row.unit, g.err)
			}
			if mutate != nil {
				mutate(t, g)
			}
			cache[row.unit] = g
		}
		if g.files["table_cookwrite.moo"] == nil {
			fails = append(fails, fmt.Sprintf("%s: unit %s emits no table_cookwrite.moo", row.caseName, row.unit))
			continue
		}
		le := readFile(t, cw.le)
		be := readFile(t, cw.be)
		var out []string
		if cw.from != "" {
			out = strings.Split(g.run(t, cookWriteProbeShared(g, row.root, cw.from, cw.to), map[string][]byte{"text": readFile(t, cw.input)}), "\n")
		} else {
			out = strings.Split(g.run(t, cookWriteProbe(g, row.root), map[string][]byte{"wire": rowWire(t, row)}), "\n")
		}
		switch {
		case len(out) != 4:
			fails = append(fails, fmt.Sprintf("%s (%s/%s): %s", row.caseName, row.unit, row.root, strings.Join(out, " | ")))
		case out[0] != fmt.Sprint(len(le)) || len(le) != len(be):
			fails = append(fails, fmt.Sprintf("%s: measure answers %s, the tool's files are %d and %d bytes", row.caseName, out[0], len(le), len(be)))
		case out[1] != hex.EncodeToString(le):
			fails = append(fails, fmt.Sprintf("%s: little-endian cook differs from the tool's at byte %d", row.caseName, hexDiff(out[1], hex.EncodeToString(le))/2))
		case out[2] != hex.EncodeToString(be):
			fails = append(fails, fmt.Sprintf("%s: big-endian cook differs from the tool's at byte %d", row.caseName, hexDiff(out[2], hex.EncodeToString(be))/2))
		case out[3] != "reread ok":
			fails = append(fails, fmt.Sprintf("%s: the cook this runtime wrote does not read back: %s", row.caseName, out[3]))
		default:
			answered += 2
		}
	}
	return fails, answered, cache
}

func readFile(t *testing.T, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func hexDiff(a, b string) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// cookFromSource is the tool's cook of a JSON text over a schema source, both
// orders: the reference for the constructs the manifest carries no instance
// of. The wire is what moo loads.
func cookFromSource(t *testing.T, src, rootName, text string) (wire, le, be []byte, g *generatedUnit) {
	t.Helper()
	u := unitFromSource(t, src)
	m := tabletext.NewModel(u)
	inst := m.New(u.Tables[rootName])
	var rep tabletext.Report
	if !m.Read(inst, []byte(text), &rep) {
		t.Fatalf("the Go engine does not read the JSON: %+v", rep)
	}
	var err error
	if wire, err = tablewire.Encode(m, inst); err != nil {
		t.Fatal(err)
	}
	if le, err = tablecook.Cook(m, inst, tablecook.Options{}); err != nil {
		t.Fatalf("the tool does not cook the instance: %v", err)
	}
	if be, err = tablecook.Cook(m, inst, tablecook.Options{Big: true}); err != nil {
		t.Fatal(err)
	}
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return wire, le, be, writeUnit(t, u, files)
}

// cookConstructs is a unit of the constructs the manifest carries no
// instance of: unbounded arrays at every depth (a list of tables that hold
// lists, of scalars, of wide kinds, of enums, of text, of unions, of
// pointers), a union arm that is a table holding a list, a value that holds a
// list beside a pointed-at holder, and unbounded text (§2.13.1) as list
// elements and map keys, empty and absent values among them. The tool cooks it (L1's shapes plus the
// element classes), and the runtime must write its bytes.
const cookConstructs = `package cookprobe

enum Mode { Read, Write }

fixed table Sample
{
    v    int32
    wide uint128
    fx   fixed(48, 16) | min = -1000, max = 1000
    tag  ?Mode
}

table Row
{
    items []Sample
    label int32
    names []string(8)
    lines []string
    maybe []?string
}

table Wrap
{
    run []Sample
    n   int32
}

union Item
{
    sample Sample
    count  int32
    note   string(12)
    wrap   Wrap
    ack
}

table Save
{
    rows   []Row
    marks  []int32
    modes  []Mode
    wides  []uint128
    items  []Item
    linked []*Row
    pinned *Row
    one    Item
    past   int32 | was = "after"
    byname map[string]int32
}
`

const cookConstructsJSON = `{
  "rows": [
    {"items": [{"v": 1, "wide": 340282366920938463463374607431768211455, "fx": 2.5, "tag": "Write"}, {"v": -2, "fx": -999.25}], "label": 11, "names": ["a", "bcd"], "lines": ["", "an unbounded line"], "maybe": [null, "", "kept"]},
    {"items": [], "label": 12, "names": []},
    {"items": [{"v": 3}], "label": 13, "names": ["eight888"]}
  ],
  "marks": [7, -7, 1048576],
  "modes": ["Write", "Read", "Write"],
  "wides": [1, 2, 340282366920938463463374607431768211455],
  "items": [{"sample": {"v": 5, "wide": 6}}, {"count": -9}, {"note": "hello"}, {"wrap": {"run": [{"v": 8}, {"v": 9, "fx": 1.5}], "n": 4}}, {"ack": null}, {}],
  "linked": [{"items": [{"v": 21}], "label": 22, "names": ["z"]}, null],
  "pinned": {"items": [], "label": 31, "names": []},
  "one": {"wrap": {"run": [{"v": 41}], "n": 5}},
  "past": 51,
  "byname": {"a key well past any short bound": 2, "k": 1}
}`

// TestMooCookWriteConstructsMatchTheTool: the constructs no manifest instance
// carries (lists of every element class, a list arm, optional and wide
// elements inside the extent) cooked by the tool from the text and by the
// runtime from the wire, byte for byte in both orders.
func TestMooCookWriteConstructsMatchTheTool(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_cookwrite.moo)")
	t.Parallel()
	fails, g := constructFailures(t, nil)
	for _, f := range fails {
		t.Error(f)
	}
	if a := g.analyze(t); a != "" {
		t.Fatalf("moo-analyze is not silent:\n%s", a)
	}
}

func constructFailures(t *testing.T, mutate func(*testing.T, *generatedUnit)) ([]string, *generatedUnit) {
	t.Helper()
	wire, le, be, g := cookFromSource(t, cookConstructs, "Save", cookConstructsJSON)
	if mutate != nil {
		mutate(t, g)
	}
	out := strings.Split(g.run(t, cookWriteProbe(g, "Save"), map[string][]byte{"wire": wire}), "\n")
	if len(out) != 4 {
		return []string{"the runtime refuses: " + strings.Join(out, " | ")}, g
	}
	var fails []string
	if out[1] != hex.EncodeToString(le) {
		fails = append(fails, fmt.Sprintf("little-endian cook differs from the tool's at byte %d", hexDiff(out[1], hex.EncodeToString(le))/2))
	}
	if out[2] != hex.EncodeToString(be) {
		fails = append(fails, fmt.Sprintf("big-endian cook differs from the tool's at byte %d", hexDiff(out[2], hex.EncodeToString(be))/2))
	}
	if out[3] != "reread ok" {
		fails = append(fails, "the cook this runtime wrote does not read back: "+out[3])
	}
	return fails, g
}

// refusalSource is the unit the write's refusals are asked of.
const refusalSource = `package refuseprobe

table Node
{
    id   int32
    next *Node
}

table Holder
{
    refs [..2]*Node
}

table Wide
{
    a float32
    b float64
}

table Squad
{
    roster map[uint8]Node
}

table Teams
{
    squads [..2]Squad
}

table Bag
{
    xs []int32
}

table Opt
{
    held ?Bag
}

table Gate
{
    on bool
    if on
    {
        xs []int32
    }
}
`

// refusalCases are values asked of the write: each refusal a value outside the
// cook's contract, refused by name and never written partly (§7.6), beside
// the clean value it departs from, which cooks. "cooked same" also cooks the
// value `other` and requires the two files equal byte for byte.
var refusalCases = []struct{ name, body, want string }{
	{"a finite pair", `local root = "Wide"
v = T.NewWide()
v.A = 0x3fc00000
v.B = require("./serialize").Uint64.new(0, 0x3ff80000)`, "cooked"},
	{"an infinite float32", `local root = "Wide"
v = T.NewWide()
v.A = 0x7f800000`, "refuse non_finite_float Wide.a"},
	{"a NaN float64", `local root = "Wide"
v = T.NewWide()
v.B = require("./serialize").Uint64.new(0, 0x7ff80000)`, "refuse non_finite_float Wide.b"},
	{"a chain", `local root = "Node"
v = T.NewNode()
v.Next = T.NewNode()`, "cooked"},
	{"a cycle", `local root = "Node"
v = T.NewNode()
v.Next = v`, "refuse cycle Node"},
	{"a pointer in a live slot", `local root = "Holder"
v = T.NewHolder()
v.RefsCount = 2
v.Refs[2] = T.NewNode()`, "cooked"},
	{"a pointer in a dead slot", `local root = "Holder"
v = T.NewHolder()
v.RefsCount = 1
v.Refs[2] = T.NewNode()`, "refuse unnumbered_reference Holder.refs"},
	{"a count past its bound", `local root = "Holder"
v = T.NewHolder()
v.RefsCount = 3`, "refuse invalid_value Holder.refs"},
	{"an empty map", `local root = "Squad"
v = T.NewSquad()`, "cooked"},
	// THE ENTRIES ARE PLACED IN ASCENDING KEY ORDER (§2.8, §7.4) whatever
	// order the value holds them in: the cook a Find can search
	{"a map's entries out of order", `local root = "Squad"
v = T.NewSquad()
v.Roster = { entry(1, 10), entry(2, 20), entry(3, 30) }
other = T.NewSquad()
other.Roster = { entry(3, 30), entry(1, 10), entry(2, 20) }`, "cooked same"},
	{"a repeated key", `local root = "Squad"
v = T.NewSquad()
v.Roster = { entry(1, 10), entry(1, 20) }`, "refuse invalid_value Squad.roster"},
	{"a map entry that is not a record", `local root = "Squad"
v = T.NewSquad()
v.Roster = { entry(1, 10), 7 }`, "refuse invalid_value Squad.roster"},
	{"a map in a live slot", `local root = "Teams"
v = T.NewTeams()
v.SquadsCount = 2
v.Squads[2].Roster = { entry(4, 40) }`, "cooked"},
	{"a map in a dead slot", `local root = "Teams"
v = T.NewTeams()
v.SquadsCount = 1
v.Squads[2].Roster = { entry(4, 40) }`, "refuse invalid_value Teams.squads"},
	// A FIELD THE SAVE ELIDES IS NOT REACHED (§2.3, §7.6): an absent optional
	// and a field under a false guard keep their slot zero, so a list in
	// either names bytes the region will not hold
	{"a present optional holding a list", `local root = "Opt"
v = T.NewOpt()
v.Held = T.NewBag()
v.Held.Xs = { 1, 2 }
v.HeldPresent = true`, "cooked"},
	{"an absent optional holding a list", `local root = "Opt"
v = T.NewOpt()
v.Held = T.NewBag()
v.Held.Xs = { 1, 2 }
v.HeldPresent = false`, "refuse invalid_value Opt.held"},
	{"a guarded list", `local root = "Gate"
v = T.NewGate()
v.On = true
v.Xs = { 1, 2 }`, "cooked"},
	{"an unguarded list", `local root = "Gate"
v = T.NewGate()
v.On = false
v.Xs = { 1, 2 }`, "refuse invalid_value Gate.xs"},
}

// refusalFailures asks every refusal case of the emitted runtime and answers
// each answer that is not the case's; mutate, when set, plants a defect in the
// unit's emitted files first (the negative controls).
func refusalFailures(t *testing.T, mutate func(*testing.T, *generatedUnit)) ([]string, *generatedUnit) {
	t.Helper()
	g := generateFiles(t, map[string]string{"Probe.schema": refusalSource})
	if mutate != nil {
		mutate(t, g)
	}
	var fails []string
	for _, c := range refusalCases {
		main := "--!strict\n" + g.requireAll() + `local T = require("./ProbeTable")
local St = require("./schema_tables")
local W = require("./table_cookwrite")
local function entry(key: number, id: number): any
	local e = St.lookup("SquadRosterEntry").new()
	e.Key = key
	e.Value = T.NewNode()
	e.Value.Id = id
	return e
end
local v: any = nil
local other: any = nil
` + c.body + `
local size, why = W.measure(root, v)
local file, whyWrite = W.cook(root, v, false)
if file == nil then
	print("refuse " .. whyWrite.name .. " " .. tostring(whyWrite.field))
else
	local tag = "cooked"
	if other ~= nil then
		local o = W.cook(root, other, false)
		tag = if o ~= nil and buffer.tostring(o) == buffer.tostring(file) then "cooked same" else "cooked differs"
	end
	print(tag .. " " .. buffer.len(file) .. " " .. tostring(size))
end
`
		got := g.run(t, main, nil)
		if strings.HasPrefix(c.want, "cooked") {
			f := strings.Fields(strings.TrimPrefix(got, c.want))
			if !strings.HasPrefix(got, c.want+" ") || len(f) != 2 || f[0] != f[1] {
				fails = append(fails, fmt.Sprintf("%s: want %s with the measure equal to the file, got %q", c.name, c.want, got))
			}
			continue
		}
		if got != c.want {
			fails = append(fails, fmt.Sprintf("%s: got %q, want %q", c.name, got, c.want))
		}
	}
	return fails, g
}

// TestMooCookWriteRefusals: a value outside the cook's contract is refused by
// name, never written partly (§7.6): a non-finite float of either width, a
// data cycle, a pointer or a map's entries in a dead counted slot (storage
// the numbering does not reach), a list under an absent optional or a false
// guard, and a map whose keys repeat (a cook Find cannot search, §7.4). Each
// asked of a clean value first, which cooks; and a map's entries are cooked
// in key order whatever order the value holds them in.
func TestMooCookWriteRefusals(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_cookwrite.moo)")
	t.Parallel()
	fails, g := refusalFailures(t, nil)
	for _, f := range fails {
		t.Error(f)
	}
	if a := g.analyze(t); a != "" {
		t.Fatalf("moo-analyze is not silent:\n%s", a)
	}
}

// TestMooCookWriteNegativeControls plants one defect per property into the
// emitted table_cookwrite.moo (or the numbering it shares) and requires the
// gate that holds the property to go red: the byte order ignored, a
// reference delta one slot long, an unbounded array's alignment dropped, a
// shared byte buffer refused as the wire refuses it, a wide string's code
// units written low byte first in a big-endian cook, a map's slot delta one
// slot long, the arrays a map's entries hold left uncarved, a map's entries
// left in the value's order, a repeated key let through, a field the save
// elides carved as if it rode, an unbounded text's slot delta one slot long,
// an empty unbounded text given a blob record instead of a null slot, and the
// reader landing a present null slot as absent rather than the empty string.
// The unsabotaged runs above are each control's reversed witness.
func TestMooCookWriteNegativeControls(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_cookwrite.moo)")
	t.Parallel()
	plant := func(file, anchor, repl string) func(*testing.T, *generatedUnit) {
		return func(t *testing.T, g *generatedUnit) {
			t.Helper()
			path := filepath.Join(g.dir, file)
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(src), anchor) != 1 {
				t.Fatalf("the anchor %q is not in %s exactly once", anchor, file)
			}
			if err := os.WriteFile(path, []byte(strings.Replace(string(src), anchor, repl, 1)), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, c := range []struct {
		name       string
		mutate     func(*testing.T, *generatedUnit)
		manifest   bool
		constructs bool
		refusals   bool
	}{
		{"order", plant("table_cookwrite.moo", "\tif ctx.big then\n\t\tbuffer.writeu32(b, at, bit32.byteswap(hi))", "\tif false then\n\t\tbuffer.writeu32(b, at, bit32.byteswap(hi))"), true, true, false},
		{"delta", plant("table_cookwrite.moo", "putNum64(ctx, at, to - at)", "putNum64(ctx, at, to - at + 8)"), true, true, false},
		{"align", plant("table_cookwrite.moo", "\tc.at = align_up(c.at, l.ealign)\n\tlocal start = c.at\n\tc.at += count * size\n\tif c.write then\n\t\tlocal array = c.extentAt + start\n\t\t-- an empty list's", "\tlocal start = c.at\n\tc.at += count * size\n\tif c.write then\n\t\tlocal array = c.extentAt + start\n\t\t-- an empty list's"), false, true, false},
		{"shared", plant("table_cookwrite.moo", "Save.number(d, value, true)", "Save.number(d, value, false)"), true, false, false},
		{"wideorder", plant("table_cookwrite.moo", "buffer.writeu8(b, at + i * 2, u // 256)\n\t\t\t\tbuffer.writeu8(b, at + i * 2 + 1, u % 256)", "buffer.writeu8(b, at + i * 2, u % 256)\n\t\t\t\tbuffer.writeu8(b, at + i * 2 + 1, u // 256)"), true, false, false},
		{"mapslot", plant("table_cookwrite.moo", "-- an empty map's reference is null in every encoding (§2.8)\n\t\tputNum64(ctx, at, if count > 0 then array - at else 0)", "putNum64(ctx, at, if count > 0 then array - at + 8 else 0)"), true, false, false},
		{"mapextent", plant("table_cookwrite.moo", "if hasExtentRecord(d) then\n\t\tfor i = 1, count do\n\t\t\tif not carveRecord(", "if false then\n\t\tfor i = 1, count do\n\t\t\tif not carveRecord("), true, false, false},
		{"maporder", plant("table_cookwrite.moo", "pcall(Save.mapOrder, fd, val)", "pcall(table.clone, val)"), false, false, true},
		{"maprepeat", plant("table_cookwrite.moo", "if not Save.keyLess(keyfd, order[i - 1], order[i]) then", "if false then"), false, false, true},
		{"liveness", plant("table_cookwrite.moo", "if Save.guardHolds(fd, v) and (fd.present == nil or v[fd.present] == true) then", "if true then"), false, false, true},
		{"textslot", plant("table_cookwrite.moo", "putNum64(ctx, at, blob - at)", "putNum64(ctx, at, blob - at + 8)"), true, true, false},
		{"textnull", plant("table_cookwrite.moo", "if x == nil or x == \"\" then\n\t\treturn true", "if x == nil then\n\t\treturn true"), false, true, false},
		{"textempty", plant("table_cookread.moo", "v[slot] = if blob == nil then \"\" else buffer.tostring(blob)", "v[slot] = if blob == nil then nil else buffer.tostring(blob)"), false, true, false},
	} {
		if c.manifest {
			fails, _, _ := cookWriteFailures(t, cookWriteRows(t), c.mutate)
			if len(fails) == 0 {
				t.Errorf("%s: the manifest gate is still green with the defect planted", c.name)
			}
		}
		if c.constructs {
			if fails, _ := constructFailures(t, c.mutate); len(fails) == 0 {
				t.Errorf("%s: the constructs gate is still green with the defect planted", c.name)
			}
		}
		if c.refusals {
			if fails, _ := refusalFailures(t, c.mutate); len(fails) == 0 {
				t.Errorf("%s: the refusals gate is still green with the defect planted", c.name)
			}
		}
	}
}

// The span itself is eight-aligned, but a 128-bit element requires a
// sixteen-aligned node and extent start. Keep both immutable corpus pins.
func TestMooCookWideListExtent(t *testing.T) {
	slowtest.Gate(t, "moo (wide list cooked extent alignment)")
	var rows []cookWriteRow
	for _, row := range cookWriteRows(t) {
		if row.row.caseName == "widelist_wide" {
			rows = append(rows, row)
		}
	}
	if len(rows) != 1 {
		t.Fatalf("wide list corpus row count %d", len(rows))
	}
	fails, answered, cache := cookWriteFailures(t, rows, nil)
	for _, f := range fails {
		t.Error(f)
	}
	if answered != 2 {
		t.Fatalf("wide list answered %d of 2 byte orders", answered)
	}
	for key, g := range cache {
		if out := g.analyze(t); out != "" {
			t.Errorf("%s analyze: %s", key, out)
		}
	}
}
