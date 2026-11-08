package luatable

// THE FIXED FORM'S EIGHT PER-ROW PROOFS ON THE LUA LEG, ported from
// internal/codegen/mootable/fixed_evolution_proof_test.go to the one read
// (docs/plans/strict-only.md, docs/FIXED-FORM-ALGORITHM.md §4.5, §4.6): the
// bounds pass REFUSES and never clamps, a bool or present byte other than 0 or
// 1 refuses, and an ordinal or tag past the writer's set refuses. Every file a
// row reads is framed here from the compiler's own layout and hash, or written
// by the generated Lua module of the writer's schema. Every row has a negative
// control that plants a break into the emitted fixed.lua and requires the row
// to go red. These are the rows the shared fixedproof suite leaves out.

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type luaEvoUnit struct {
	dir   string
	mod   string
	files map[string][]byte
	unit  *ir.Unit
}

func luaEvoSource(t *testing.T, src string) *ir.Unit {
	t.Helper()
	p := filepath.Join(t.TempDir(), "probe.schema")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return luarun.LoadUnit(t, p)
}

func luaEvoGen(t *testing.T, u *ir.Unit, lineage map[string][]FixedLineageEntry) *luaEvoUnit {
	t.Helper()
	var files map[string][]byte
	var err error
	if lineage == nil {
		files, err = Generate(u)
	} else {
		files, err = GenerateLineage(u, lineage)
	}
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if _, ok := files["fixed.lua"]; !ok {
		t.Fatal("the generated unit carries no fixed.lua")
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	return &luaEvoUnit{dir: dir, mod: ModuleName(u), files: files, unit: u}
}

func luaEvoEntry(t *testing.T, u *ir.Unit, root string) FixedLineageEntry {
	t.Helper()
	e, ok := FixedLineageOf(u, root)
	if !ok {
		t.Fatalf("no fixed root %s", root)
	}
	return e
}

func luaEvoLineage(t *testing.T, u *ir.Unit, root string) map[string][]FixedLineageEntry {
	return map[string][]FixedLineageEntry{root: {luaEvoEntry(t, u, root)}}
}

// luaEvoFile frames a form-3 file: the header, the layout, and each body
// behind the record hash.
func luaEvoFile(hash uint64, layout []byte, bodies ...[]byte) []byte {
	out := []byte{3, 0, 0, 0, 0, 0, 0, 0}
	out = binary.LittleEndian.AppendUint64(out, hash)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(layout)))
	out = append(out, layout...)
	for _, b := range bodies {
		out = binary.LittleEndian.AppendUint64(out, hash)
		out = append(out, b...)
	}
	return out
}

// plant replaces anchor, which must sit in the emitted fixed.lua exactly
// once, in this unit's copy.
func (g *luaEvoUnit) plant(t *testing.T, anchor, with string) {
	t.Helper()
	src := string(g.files["fixed.lua"])
	if strings.Count(src, anchor) != 1 {
		t.Fatalf("the anchor %q is not in fixed.lua exactly once", anchor)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "fixed.lua"), []byte(strings.Replace(src, anchor, with, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (g *luaEvoUnit) run(t *testing.T, main string, inputs map[string][]byte) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.dir, "main.lua"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(luarun.Run(t, g.dir, "main.lua", inputs)))
}

const luaEvoDumper = `
local function bits(x)
  if math.type(x) == "float" then return string.format("0x%016X", string.unpack("<I8", string.pack("<d", x))) end
  return tostring(x)
end
local function dump(p, v)
  if type(v) == "table" then
    local keys = {}
    for k in pairs(v) do keys[#keys + 1] = k end
    table.sort(keys, function(a, b) return tostring(a) < tostring(b) end)
    for _, k in ipairs(keys) do dump(p .. "." .. tostring(k), v[k]) end
  elseif type(v) == "string" then
    print(p .. "=" .. string.format("%q", v))
  else
    print(p .. "=" .. bits(v))
  end
end
`

type luaEvoRun struct {
	head string
	dump map[string]string
}

// load reads file at root through the plain FixedLoad: the head is
// "ok <n> w<widened> u<unknown>" or "refused <name>", then the records.
func (g *luaEvoUnit) load(t *testing.T, root string, file []byte) luaEvoRun {
	t.Helper()
	main := fmt.Sprintf(`local T = require(%q)
local I = require("inputs")
%s
local ok, rows, _, _, w, u = pcall(T.FixedLoad, %q, I.file)
if not ok then
  print("refused " .. (tostring(rows):match("([%%w_]+)$") or tostring(rows)))
  return
end
print(string.format("ok %%d w%%d u%%d", #rows, w or 0, u or 0))
for i, r in ipairs(rows) do dump("r" .. (i - 1), r) end
`, g.mod, luaEvoDumper, root)
	lines := strings.Split(g.run(t, main, map[string][]byte{"file": file}), "\n")
	r := luaEvoRun{head: lines[0], dump: map[string]string{}}
	for _, l := range lines[1:] {
		k, v, _ := strings.Cut(l, "=")
		r.dump[k] = v
	}
	return r
}

// save runs setup (Lua that fills `values`, with T the unit's module) and
// saves values at root.
func (g *luaEvoUnit) save(t *testing.T, root, setup string) []byte {
	t.Helper()
	main := fmt.Sprintf(`local T = require(%q)
local values = {}
%s
local out = T.FixedSave(%q, values)
io.write((out:gsub(".", function(c) return string.format("%%02x", c:byte()) end)))
`, g.mod, setup, root)
	b, err := hex.DecodeString(g.run(t, main, nil))
	if err != nil {
		t.Fatalf("the save: %v", err)
	}
	return b
}

func luaEvoClone(b []byte) []byte { return append([]byte(nil), b...) }

// TestLuaFixedEvolutionNumericWidening is numeric-evolution (E3, C8, R20): an
// older writer's int16 into an int32 and float32 into a float64 decode
// exactly, the int16 sign-extended, each counted widened once per record, and
// a field the writer never carried lands its declared default. The negative
// controls drop the widened count and zero-extend the int16.
func TestLuaFixedEvolutionNumericWidening(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua (it runs the emitted fixed-form runtime)")
	older := luaEvoSource(t, "package probe\nfixed table N\n{\n    lead uint32 = 1\n    v int16 = 0\n    f float32 = 0.0\n    trail uint32 = 2\n}\n")
	newer := luaEvoSource(t, "package probe\nfixed table N\n{\n    lead uint32 = 1\n    v int32 = 0\n    f float64 = 0.0\n    trail uint32 = 2\n    added int32 = 77\n}\n")
	eo := luaEvoEntry(t, older, "N")
	body := binary.LittleEndian.AppendUint32(nil, 5)
	body = binary.LittleEndian.AppendUint16(body, 0xFFF9) // -7
	body = binary.LittleEndian.AppendUint32(body, 0xC49A5200)
	body = binary.LittleEndian.AppendUint32(body, 6)
	file := luaEvoFile(eo.Wire, eo.Layout, body, body)
	lineage := luaEvoLineage(t, older, "N")
	got := luaEvoGen(t, newer, lineage).load(t, "N", file)
	if got.head != "ok 2 w4 u0" {
		t.Errorf("the widening read: %s, want two widened entries per record", got.head)
	}
	for k, w := range map[string]string{"r0.lead": "5", "r0.v": "-7", "r0.f": "0xC0934A4000000000", "r0.trail": "6", "r0.added": "77", "r1.v": "-7"} {
		if got.dump[k] != w {
			t.Errorf("%s landed %q, want %s", k, got.dump[k], w)
		}
	}
	g := luaEvoGen(t, newer, lineage)
	g.plant(t, "    if rep and node.size < w then rep.widened = rep.widened + 1 end\n    local signed", "    local signed")
	if g.load(t, "N", file).head == got.head {
		t.Errorf("the negative control: an integer widening not counted still reads the same")
	}
	g = luaEvoGen(t, newer, lineage)
	g.plant(t, "    local signed = (node.kind >= 2 and node.kind <= 5) or d.timestamp\n", "    local signed = false\n")
	if g.load(t, "N", file).dump["r0.v"] == "-7" {
		t.Errorf("the negative control: a zero-extended int16 still lands -7")
	}
}

// TestLuaFixedEvolutionReports is reports (E9, R16, R18): the fixed runtime
// carries no repeated-key counter; a full-width range that cannot be broken
// reads 256 records with nothing refused; and the unknown census over an older
// writer's dropped nested field is counted once per declared field, the same
// on a second load (the counters are each read's own). The negative control
// drops the census.
func TestLuaFixedEvolutionReports(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua (it runs the emitted fixed-form runtime)")
	g := luaEvoGen(t, luaEvoSource(t, "package probe\nfixed table R\n{\n    a int8 = 0 | min = -128, max = 127\n    b uint8\n}\n"), nil)
	if src := string(g.files["fixed.lua"]); strings.Contains(src, "repeatedKey") || strings.Contains(src, "duplicate") {
		t.Errorf("E9: the fixed runtime counts a repeated key")
	}
	var bodies [][]byte
	for x := range 256 {
		bodies = append(bodies, []byte{byte(x), byte(x)})
	}
	e := luaEvoEntry(t, g.unit, "R")
	if got := g.load(t, "R", luaEvoFile(e.Wire, e.Layout, bodies...)).head; got != "ok 256 w0 u0" {
		t.Errorf("R18: a range that cannot be broken refused or moved: %s", got)
	}

	repo := filepath.Join("..", "..", "..")
	older := luarun.LoadUnit(t, filepath.Join(repo, "test", "tables", "VOLD_unknown_census.schema"))
	newer := luarun.LoadUnit(t, filepath.Join(repo, "test", "tables", "VNEW_unknown_census.schema"))
	old := luaEvoGen(t, older, nil).save(t, "Census", "for i = 1, 3 do local v = T.NewCensus(); v.lead = i; v.items[1].a = 10 * i; v.items[1].drop = 99; values[i] = v end")
	census := func(g *luaEvoUnit) string {
		return strings.Join(strings.Fields(g.run(t, fmt.Sprintf(`local T = require(%q)
local I = require("inputs")
for _ = 1, 2 do
  local v, _, _, w, u = T.FixedLoad("Census", I.file)
  io.write(#v, " ", w, " ", u, " ", v[3].items[1].a, " ")
end
`, g.mod), map[string][]byte{"file": old})), " ")
	}
	lineage := luaEvoLineage(t, older, "Census")
	if got := census(luaEvoGen(t, newer, lineage)); got != "3 0 1 30 3 0 1 30" {
		t.Errorf("R16: the census over three records, loaded twice: %q", got)
	}
	gc := luaEvoGen(t, newer, lineage)
	gc.plant(t, "      entry.unknown = census_value(", "      entry.unknown = 0 * census_value(")
	if got := census(gc); got == "3 0 1 30 3 0 1 30" {
		t.Errorf("the negative control: the dropped census still counts")
	}
}

// TestLuaFixedEvolutionArrayBounds is array-bounds (C1, C2, R11): the plan
// carries the WRITER's bound: a counted array the old writer bounded at 4 and
// the reader at 8 refuses a count of -1 and of 7, and reads at exactly 4. The
// negative control bounds the count by the reader's 8.
func TestLuaFixedEvolutionArrayBounds(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua (it runs the emitted fixed-form runtime)")
	repo := filepath.Join("..", "..", "..")
	older := luarun.LoadUnit(t, filepath.Join(repo, "test", "tables", "VOLD_array_bounded_grow.schema"))
	newer := luarun.LoadUnit(t, filepath.Join(repo, "test", "tables", "VNEW_array_bounded_grow.schema"))
	base := luaEvoGen(t, older, nil).save(t, "ArrayBoundedGrow", "local v = T.NewArrayBoundedGrow(); v.lead = 1; v.vals = { 10, 20, 30 }; v.trail = 2; values[1] = v")
	countAt := 20 + int(binary.LittleEndian.Uint32(base[16:20])) + 8 + 4
	with := func(n uint32) []byte {
		b := luaEvoClone(base)
		binary.LittleEndian.PutUint32(b[countAt:], n)
		return b
	}
	lineage := luaEvoLineage(t, older, "ArrayBoundedGrow")
	g := luaEvoGen(t, newer, lineage)
	for _, c := range []struct {
		name  string
		count uint32
		want  string
	}{
		{"C1 count -1", 0xFFFFFFFF, "refused count_over_bound"},
		{"C2 count 7 past the writer's 4", 7, "refused count_over_bound"},
		{"R11 count 4, the writer's bound", 4, "ok 1 w0 u0"},
		{"the control, count 3", 3, "ok 1 w0 u0"},
	} {
		if got := g.load(t, "ArrayBoundedGrow", with(c.count)).head; got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	if got := g.load(t, "ArrayBoundedGrow", base); got.dump["r0.vals.3"] != "30" || got.dump["r0.trail"] != "2" {
		t.Errorf("the control's values: %v", got.dump)
	}
	gc := luaEvoGen(t, newer, lineage)
	gc.plant(t, "    local cap = es > 0 and math.floor((node.size - 4) / es) or 0\n    if n < 0 or n > cap then", "    if n < 0 or n > d.bound then")
	if got := gc.load(t, "ArrayBoundedGrow", with(7)).head; got == "refused count_over_bound" {
		t.Errorf("the negative control: the reader's bound still refuses the writer's overrun")
	}
}

// TestLuaFixedEvolutionText is text (C3, C4, C5, R24): a length past the
// field's capacity refuses; ill-formed UTF-8 or a zero in the USED bytes and an
// unpaired surrogate in a wstring's used units refuse; a hostile byte in the
// slack past the used length is not read and not a refusal. The negative
// control drops the UTF-8 check.
func TestLuaFixedEvolutionText(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua (it runs the emitted fixed-form runtime)")
	g := luaEvoGen(t, luaEvoSource(t, "package probe\nfixed table T\n{\n    s string(4)\n    w wstring(4)\n}\n"), nil)
	clean := g.save(t, "T", "local v = T.NewT(); v.s = \"ab\"; v.w = { 0x41, 0x42 }; values[1] = v")
	body := len(clean) - 20
	cut := func(f func(b []byte)) []byte { b := luaEvoClone(clean); f(b); return b }
	cases := []struct {
		name string
		file []byte
		want string
	}{
		{"C3 length 5 of 4", cut(func(b []byte) { b[body] = 5 }), "refused length_over_bound"},
		{"C4 0xff in the used bytes", cut(func(b []byte) { b[body+4] = 0xff }), "refused text_ill_formed"},
		{"C4 a zero in the used bytes", cut(func(b []byte) { b[body+5] = 0 }), "refused text_ill_formed"},
		{"R24 0xff in the slack", cut(func(b []byte) { b[body+6], b[body+7] = 0xff, 0xff }), "ok 1 w0 u0"},
		{"C5 an unpaired surrogate", cut(func(b []byte) { b[body+12], b[body+13] = 0x00, 0xd8 }), "refused text_ill_formed"},
		{"C5 a wide length past the capacity", cut(func(b []byte) { b[body+8] = 5 }), "refused length_over_bound"},
	}
	for _, c := range cases {
		if got := g.load(t, "T", c.file).head; got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	if got := g.load(t, "T", clean); got.head != "ok 1 w0 u0" || got.dump["r0.s"] != `"ab"` || got.dump["r0.w.2"] != "66" {
		t.Errorf("the control: %s %v", got.head, got.dump)
	}
	gc := luaEvoGen(t, g.unit, nil)
	gc.plant(t, `  if strfind(s, "\0", 1, true) or utf8len(s) == nil then error("text_ill_formed", 0) end`, "")
	if got := gc.load(t, "T", cases[1].file).head; got == cases[1].want {
		t.Errorf("the negative control: ill-formed text still refuses")
	}
}

// TestLuaFixedEvolutionScalarBounds is scalar-bounds (C7, R30): a ranged
// int32, int128 and uint16 each refuse below and above their ranges, alone and
// together, and read in range; a compressed float rides as the float32 itself,
// saved unquantized, refuses past either end, and a -0 lands as +0 (SPEC-TABLES
// §3.3); its resolution is in the definitions digest under 'Q', so a finer or a
// dropped triple reads the old writer's bits unchanged. The negative control
// drops the signed integer bound.
func TestLuaFixedEvolutionScalarBounds(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua (it runs the emitted fixed-form runtime)")
	u := luaEvoSource(t, "package probe\nfixed table T\n{\n    x int32 = 0 | min = -5, max = 5\n    s int128 = 0 | min = -500, max = 500\n    u uint16 = 10 | min = 10, max = 1000\n}\n")
	e := luaEvoEntry(t, u, "T")
	body := func(x int32, s int64, v uint16) []byte {
		b := binary.LittleEndian.AppendUint32(nil, uint32(x))
		b = binary.LittleEndian.AppendUint64(b, uint64(s))
		hi := uint64(0)
		if s < 0 {
			hi = ^uint64(0)
		}
		b = binary.LittleEndian.AppendUint64(b, hi)
		return binary.LittleEndian.AppendUint16(b, v)
	}
	if e.Record != 8+int64(len(body(0, 0, 0))) {
		t.Fatalf("the record body is %d, the test frames %d", e.Record-8, len(body(0, 0, 0)))
	}
	g := luaEvoGen(t, u, nil)
	got := g.load(t, "T", luaEvoFile(e.Wire, e.Layout, body(3, -42, 500)))
	if got.head != "ok 1 w0 u0" || got.dump["r0.x"] != "3" || got.dump["r0.s.lo"] != "-42" || got.dump["r0.s.hi"] != "-1" || got.dump["r0.u"] != "500" {
		t.Errorf("C7 in range: %s %v", got.head, got.dump)
	}
	for _, c := range []struct {
		name string
		b    []byte
	}{
		{"all below", body(-100, -600, 5)}, {"all above", body(100, 600, 2000)},
		{"x below", body(-6, 0, 10)}, {"x above", body(6, 0, 10)},
		{"s below", body(0, -501, 10)}, {"s above", body(0, 501, 10)},
		{"u below", body(0, 0, 9)}, {"u above", body(0, 0, 1001)},
	} {
		if got := g.load(t, "T", luaEvoFile(e.Wire, e.Layout, c.b)).head; got != "refused out_of_range" {
			t.Errorf("C7 %s: %s, want refused out_of_range", c.name, got)
		}
	}
	gc := luaEvoGen(t, u, nil)
	gc.plant(t, "        if v < d.lo or v > d.hi then refuse_out_of_range() end\n      elseif", "        if false then refuse_out_of_range() end\n      elseif")
	if got := gc.load(t, "T", luaEvoFile(e.Wire, e.Layout, body(-6, 0, 10))).head; got == "refused out_of_range" {
		t.Errorf("the negative control: the dropped bound still refuses")
	}

	const cf = "package probe\nfixed table Reading\n{\n    lead uint32 = 1\n    aim float32 = 0.0%s\n    trail uint32 = 2\n}\n"
	old := luaEvoSource(t, fmt.Sprintf(cf, " | min = -1.0, max = 2.0, resolution = 0.1"))
	if d := ir.TableFixedDefinitionsDigest(old.Tables["Reading"]); !bytes.Contains(d, []byte{'Q'}) {
		t.Errorf("R30: the digest does not carry the resolution under 'Q'")
	}
	gr := luaEvoGen(t, old, nil)
	saved := gr.save(t, "Reading", "local v = T.NewReading(); v.aim = string.unpack(\"<f\", string.pack(\"<I4\", 0x3e99999a)); values[1] = v")
	at := len(saved) - 8
	if binary.LittleEndian.Uint32(saved[at:]) != 0x3e99999a {
		t.Errorf("R30: the compressed float is not saved as its float32 bits: %x", saved[at:])
	}
	for _, c := range []struct {
		bits      uint32
		head, aim string
	}{
		{0xc0000000, "refused out_of_range", ""},
		{0x40800000, "refused out_of_range", ""},
		{0x80000000, "ok 1 w0 u0", "0x0000000000000000"},
		{0x3f800000, "ok 1 w0 u0", "0x3FF0000000000000"},
	} {
		f := luaEvoClone(saved)
		binary.LittleEndian.PutUint32(f[at:], c.bits)
		if got := gr.load(t, "Reading", f); got.head != c.head || got.dump["r0.aim"] != c.aim {
			t.Errorf("R30 0x%08x: %s aim=%s, want %s %s", c.bits, got.head, got.dump["r0.aim"], c.head, c.aim)
		}
	}
	lin := luaEvoLineage(t, old, "Reading")
	for name, src := range map[string]string{"finer": fmt.Sprintf(cf, " | min = -1.0, max = 2.0, resolution = 0.01"), "dropped": fmt.Sprintf(cf, "")} {
		gn := luaEvoGen(t, luaEvoSource(t, src), lin)
		if got := gn.load(t, "Reading", saved); got.head != "ok 1 w0 u0" || got.dump["r0.aim"] != "0x3FD3333340000000" {
			t.Errorf("R30 %s: %s aim=%s", name, got.head, got.dump["r0.aim"])
		}
	}
}

const luaOrdinalOld = "package probe\nenum Tier { Bronze, Silver }\nunion Pick\n{\n    a int32\n    b int32\n}\nfixed table Lineage\n{\n    tier Tier\n    pick Pick\n    seq int32\n}\n"

// TestLuaFixedEvolutionOrdinals is ordinals (C9, C10, R31): an enum ordinal
// past the writer's variants and a union tag past its arms each refuse
// out_of_range, alone and together, on the identity plan and on a compiled
// plan whose reader declares MORE variants and arms (the writer's count
// governs); in-set ordinals read. The negative controls drop the tag check on
// each plan and the identity enum check.
func TestLuaFixedEvolutionOrdinals(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua (it runs the emitted fixed-form runtime)")
	uOld := luaEvoSource(t, luaOrdinalOld)
	uNew := luaEvoSource(t, strings.Replace(strings.Replace(luaOrdinalOld, "Silver }", "Silver, Gold }", 1), "    b int32\n", "    b int32\n    c int32\n", 1))
	eo := luaEvoEntry(t, uOld, "Lineage")
	both := luaEvoFile(eo.Wire, eo.Layout, []byte{3, 3, 1, 0, 0, 0, 42, 0, 0, 0})
	tier := luaEvoFile(eo.Wire, eo.Layout, []byte{3, 2, 9, 0, 0, 0, 42, 0, 0, 0})
	tag := luaEvoFile(eo.Wire, eo.Layout, []byte{2, 3, 9, 0, 0, 0, 42, 0, 0, 0})
	clean := luaEvoFile(eo.Wire, eo.Layout, []byte{2, 2, 9, 0, 0, 0, 42, 0, 0, 0})
	lin := luaEvoLineage(t, uOld, "Lineage")
	for name, g := range map[string]*luaEvoUnit{"identity": luaEvoGen(t, uOld, nil), "compiled": luaEvoGen(t, uNew, lin)} {
		for fname, f := range map[string][]byte{"both": both, "tier": tier, "tag": tag} {
			if got := g.load(t, "Lineage", f).head; got != "refused out_of_range" {
				t.Errorf("%s %s: %s, want refused out_of_range", name, fname, got)
			}
		}
		if c := g.load(t, "Lineage", clean); c.head != "ok 1 w0 u0" || c.dump["r0.tier"] != `"Silver"` || c.dump["r0.pick.tag"] != "2" || c.dump["r0.seq"] != "42" {
			t.Errorf("%s, the control: %s %v", name, c.head, c.dump)
		}
	}
	g := luaEvoGen(t, uOld, nil)
	g.plant(t, "    if tag > #ud.arms then refuse_out_of_range() end\n", "")
	if got := g.load(t, "Lineage", tag).head; got == "refused out_of_range" {
		t.Errorf("the negative control: an unchecked identity tag still refuses")
	}
	g = luaEvoGen(t, uOld, nil)
	g.plant(t, "    if raw > #d.variants then refuse_out_of_range() end\n", "")
	if got := g.load(t, "Lineage", tier).head; got == "refused out_of_range" {
		t.Errorf("the negative control: an unchecked identity ordinal still refuses")
	}
	g = luaEvoGen(t, uNew, lin)
	g.plant(t, "    if tag > nArms then refuse_out_of_range() end\n", "")
	if got := g.load(t, "Lineage", tag).head; got == "refused out_of_range" {
		t.Errorf("the negative control: a compiled tag held to the reader's arms still refuses")
	}
}

// TestLuaFixedEvolutionNormalization is normalization (C12, C13, R17) under
// the one read: a bool byte or a present byte other than 0 or 1 refuses
// wire_damaged (docs/FIXED-FORM-ALGORITHM.md §4.5), never normalized; 0 and 1
// read, and the payload behind a present byte of 1 lands. The negative
// control drops the byte check.
func TestLuaFixedEvolutionNormalization(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua (it runs the emitted fixed-form runtime)")
	g := luaEvoGen(t, luaEvoSource(t, "package probe\nfixed table T\n{\n    flag bool\n    opt ?int32\n}\n"), nil)
	file := g.save(t, "T", "local v = T.NewT(); v.opt = 99; v.opt_present = true; values[1] = v")
	body := len(file) - 6
	set := func(flag, present, payload byte) []byte {
		b := luaEvoClone(file)
		b[body], b[body+1], b[body+2] = flag, present, payload
		return b
	}
	for _, c := range []struct {
		name string
		file []byte
	}{
		{"bool 0x7f", set(0x7f, 1, 42)}, {"bool 0x02", set(2, 1, 42)}, {"bool 0xff", set(0xff, 0, 0)},
		{"present 0x5a", set(1, 0x5a, 42)}, {"present 0x02", set(0, 2, 42)},
	} {
		if got := g.load(t, "T", c.file).head; got != "refused wire_damaged" {
			t.Errorf("%s: %s, want refused wire_damaged", c.name, got)
		}
	}
	if got := g.load(t, "T", set(1, 1, 42)); got.head != "ok 1 w0 u0" || got.dump["r0.flag"] != "true" || got.dump["r0.opt_present"] != "true" || got.dump["r0.opt"] != "42" {
		t.Errorf("the control: %s %v", got.head, got.dump)
	}
	if got := g.load(t, "T", set(0, 0, 42)); got.head != "ok 1 w0 u0" || got.dump["r0.flag"] != "false" || got.dump["r0.opt_present"] != "false" {
		t.Errorf("the control, absent: %s %v", got.head, got.dump)
	}
	gc := luaEvoGen(t, g.unit, nil)
	gc.plant(t, `  if b > 1 then error("wire_damaged", 0) end`, "")
	if got := gc.load(t, "T", set(0x7f, 1, 42)).head; got == "refused wire_damaged" {
		t.Errorf("the negative control: an unchecked bool byte still refuses")
	}
}

const luaHostSchema = "package probe\nfixed table Host\n{\n    small int32 = 5 | min = 0, max = 1000\n    marks [..4]int32 | min = 0, max = 1000\n    note ?int32 | min = 0, max = 1000\n    tail int32 = 7\n}\n"

// luaHostSweep poisons every byte of the record six ways and loads each image:
// a refusal must be by a name the fixed form declares, a read must land every
// live value in its bound. It prints the failures and the cases.
const luaHostSweep = `local T = require(%q)
local names = { out_of_range = true, wire_damaged = true, count_over_bound = true, length_over_bound = true, no_layout = true }
local function inBound(v)
  if v.small < 0 or v.small > 1000 then return false end
  if #v.marks > 4 then return false end
  for i = 1, #v.marks do
    if v.marks[i] < 0 or v.marks[i] > 1000 then return false end
  end
  if v.note_present and (v.note < 0 or v.note > 1000) then return false end
  return true
end
local v = T.NewHost()
v.small = 5
v.marks = { 10, 20 }
v.note_present = true
v.note = 30
local clean = T.FixedSave("Host", { v })
local n = #clean
local rec = n - %d + 1
local bad, cases = 0, 0
for at = rec, n do
  for _, p in ipairs({ 0x00, 0x01, 0x02, 0x7f, 0x80, 0xff }) do
    local b = clean:sub(1, at - 1) .. string.char(p) .. clean:sub(at + 1)
    local ok, got = pcall(T.FixedLoad, "Host", b)
    if not ok then
      if not names[tostring(got):match("([%%w_]+)$")] then bad = bad + 1 end
    elseif #got ~= 1 or not inBound(got[1]) then
      bad = bad + 1
    end
    cases = cases + 1
  end
end
print(bad, cases)
`

// TestLuaFixedEvolutionHostileInput is hostile-input (P3, P4): every byte of a
// record poisoned six ways either refuses by a declared name or lands every
// live value in its bound; a bad hash on the LAST record of a batch refuses
// the whole load; and the wrong plan goes red: B's reader refuses A's file,
// and A's file announced under B's hash is refused, never read through B's
// plan. The negative control drops the signed range check and requires the
// sweep to find out-of-bound values.
func TestLuaFixedEvolutionHostileInput(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua (it runs the emitted fixed-form runtime)")
	u := luaEvoSource(t, luaHostSchema)
	record := luaEvoEntry(t, u, "Host").Record
	g := luaEvoGen(t, u, nil)
	sweep := fmt.Sprintf(luaHostSweep, g.mod, record)
	if got := strings.Join(strings.Fields(g.run(t, sweep, nil)), " "); got != fmt.Sprintf("0 %d", 6*record) {
		t.Errorf("P3: the sweep: %q, want 0 failures of %d", got, 6*record)
	}
	two := g.save(t, "Host", "local v = T.NewHost(); values = { v, v }")
	two[len(two)-int(record)] ^= 1
	if got := g.load(t, "Host", two).head; got != "refused no_layout" {
		t.Errorf("P3: the last record's hash: %s", got)
	}
	gc := luaEvoGen(t, u, nil)
	gc.plant(t, "        if v < d.lo or v > d.hi then refuse_out_of_range() end\n      elseif", "        if false then refuse_out_of_range() end\n      elseif")
	if got := gc.run(t, sweep, nil); strings.HasPrefix(got, "0\t") {
		t.Errorf("the negative control: the sweep without the bound finds nothing: %s", got)
	}

	ab := luaEvoSource(t, "package probe\nfixed table A\n{\n    lead uint32 = 0\n    x int32 = 0\n    y int32 = 0\n    trail uint32 = 0\n}\nfixed table B\n{\n    p int8 = 0\n    q int8 = 0\n}\n")
	gab := luaEvoGen(t, ab, nil)
	aFile := gab.save(t, "A", "local v = T.NewA(); v.lead = 0xAABBCCDD; v.x = 111; v.y = 222; v.trail = 0x11223344; values[1] = v")
	if got := gab.load(t, "B", aFile).head; !strings.HasPrefix(got, "refused ") {
		t.Errorf("P4: B's reader on A's file: %s, want a refusal", got)
	}
	asB := luaEvoClone(aFile)
	binary.LittleEndian.PutUint64(asB[8:], luaEvoEntry(t, ab, "B").Wire)
	if got := gab.load(t, "B", asB).head; !strings.HasPrefix(got, "refused ") {
		t.Errorf("P4: A's file announced as B: %s, want a refusal", got)
	}
	if got := gab.load(t, "A", aFile); got.head != "ok 1 w0 u0" || got.dump["r0.x"] != "111" || got.dump["r0.trail"] != "287454020" {
		t.Errorf("P4: the right plan: %s %v", got.head, got.dump)
	}
}
