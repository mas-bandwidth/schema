package luautable_test

// THE FIXED FORM'S EIGHT PER-ROW PROOFS ON THE LUAU LEG, ported from
// internal/codegen/luatable/fixed_evolution_proof_test.go (itself moo's) to
// the one read (docs/plans/strict-only.md, docs/FIXED-FORM-ALGORITHM.md §4.5,
// §4.6): the bounds pass REFUSES and never clamps, a bool or present byte
// other than 0 or 1 refuses, and an ordinal or tag past the writer's set
// refuses. Every file a row reads is framed here from the compiler's own
// layout and hash, or written by the generated Luau module of the writer's
// schema. Every row has a negative control that plants a break into the
// emitted schema_fixed.luau and requires the row to go red.

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type luauCutUnit struct {
	dir   string
	files map[string][]byte
	unit  *ir.Unit
}

// luauCutSource loads src as Probe.schema, so the unit's module is
// ProbeTable whatever its package.
func luauCutSource(t *testing.T, src string) *ir.Unit {
	t.Helper()
	p := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{p})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return u
}

func luauCutCorpus(t *testing.T, name string) *ir.Unit {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(root, "test", "tables", name+".schema"))
	if err != nil {
		t.Fatal(err)
	}
	return luauCutSource(t, string(src))
}

func luauCutGen(t *testing.T, u *ir.Unit, lineage map[string][]luautable.FixedLineageEntry) *luauCutUnit {
	t.Helper()
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var tables map[string][]byte
	if lineage == nil {
		tables, err = luautable.Generate(u)
	} else {
		tables, err = luautable.GenerateLineage(u, lineage)
	}
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	maps.Copy(files, tables)
	if files["schema_fixed.luau"] == nil || files["ProbeTable.luau"] == nil {
		t.Fatal("the generated unit carries no schema_fixed.luau or ProbeTable.luau")
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &luauCutUnit{dir: dir, files: files, unit: u}
}

func luauCutEntry(t *testing.T, u *ir.Unit, name string) luautable.FixedLineageEntry {
	t.Helper()
	e, ok := luautable.FixedLineageOf(u, name)
	if !ok {
		t.Fatalf("no fixed root %s", name)
	}
	return e
}

func luauCutLineage(t *testing.T, u *ir.Unit, name string) map[string][]luautable.FixedLineageEntry {
	return map[string][]luautable.FixedLineageEntry{name: {luauCutEntry(t, u, name)}}
}

// luauCutFile frames a form-3 file: the header, the layout, and each body
// behind the record hash.
func luauCutFile(hash uint64, layout []byte, bodies ...[]byte) []byte {
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

// plant replaces anchor, which must sit in the emitted schema_fixed.luau
// exactly once, in this unit's copy.
func (g *luauCutUnit) plant(t *testing.T, anchor, with string) {
	t.Helper()
	src := string(g.files["schema_fixed.luau"])
	if strings.Count(src, anchor) != 1 {
		t.Fatalf("the anchor %q is not in schema_fixed.luau exactly once", anchor)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "schema_fixed.luau"), []byte(strings.Replace(src, anchor, with, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// run answers main's output; a script that raises answers its output and
// the error, so a negative control sees a crash as a different answer.
func (g *luauCutUnit) run(t *testing.T, main string, inputs map[string][]byte) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runLuauErr(t, g.dir, "main.luau", inputs)
	if err != nil {
		return strings.TrimSpace(string(out)) + "\nerror: " + err.Error()
	}
	return strings.TrimSpace(string(out))
}

// luauCutPrelude requires the unit, the fixed runtime and the inputs, names
// the root's fixed facts, and dumps a value: every number as p=<integer> when
// it is one and p#=<its float64 bits> always, an integer and a 128-bit half in
// decimal, a buffer in hex.
const luauCutPrelude = `local T = require("./ProbeTable")
local F = require("./schema_fixed")
local I = require("./inputs")
local _ = I
local function fixedOf(name: string): any
	return { hash = T[name .. "FixedHash"], recordBytes = T[name .. "FixedRecordBytes"], layout = T[name .. "FixedLayout"],
		lineage = T[name .. "FixedLineage"], floor = T[name .. "FixedFloor"] }
end
local scratch = buffer.create(8)
local function bits64(x: number): string
	buffer.writef64(scratch, 0, x)
	return string.format("0x%08X%08X", buffer.readu32(scratch, 4), buffer.readu32(scratch, 0))
end
local function dump(p: string, v: any)
	local t = typeof(v)
	if t == "table" then
		local keys = {}
		for k in v do table.insert(keys, k) end
		table.sort(keys, function(a, b) return tostring(a) < tostring(b) end)
		for _, k in keys do dump(p .. "." .. tostring(k), v[k]) end
	elseif t == "buffer" then
		local s = {}
		for i = 0, buffer.len(v) - 1 do table.insert(s, string.format("%02x", buffer.readu8(v, i))) end
		print(p .. "=" .. table.concat(s))
	elseif t == "number" then
		if v == math.floor(v) and math.abs(v) < 2 ^ 53 then print(p .. "=" .. string.format("%d", v)) end
		print(p .. "#=" .. bits64(v))
	elseif t == "integer" then
		print(p .. "=" .. string.format("%d", integer.tonumber(v)))
	else
		print(p .. "=" .. tostring(v))
	end
end
local _ = dump
`

type luauCutRun struct {
	head string
	dump map[string]string
}

// load reads file at root through the plain strict F.read: the head is
// "ok <n> w<widened> u<unknown>" or "refused <name>", then the records.
func (g *luauCutUnit) load(t *testing.T, root string, file []byte) luauCutRun {
	t.Helper()
	main := luauCutPrelude + fmt.Sprintf(`local rep: any = {}
local rows = F.read(T.%[1]sDescriptor, fixedOf(%[1]q), I.file, rep)
if rows == nil then
	print("refused " .. tostring(rep.reason))
else
	print(string.format("ok %%d w%%d u%%d", #rows, rep.widened or 0, rep.unknown or 0))
	for i, r in rows do dump("r" .. (i - 1), r) end
end
`, root)
	lines := strings.Split(g.run(t, main, map[string][]byte{"file": file}), "\n")
	r := luauCutRun{head: lines[0], dump: map[string]string{}}
	for _, l := range lines[1:] {
		k, v, _ := strings.Cut(l, "=")
		r.dump[k] = v
	}
	return r
}

// save runs setup (Luau that fills `values`) and saves values at root.
func (g *luauCutUnit) save(t *testing.T, root, setup string) []byte {
	t.Helper()
	main := luauCutPrelude + fmt.Sprintf(`local values: { any } = {}
%[2]s
local out = F.save(T.%[1]sDescriptor, fixedOf(%[1]q), values)
local s = {}
for i = 0, buffer.len(out) - 1 do table.insert(s, string.format("%%02x", buffer.readu8(out, i))) end
print(table.concat(s))
`, root, setup)
	out := g.run(t, main, nil)
	b, err := hex.DecodeString(out)
	if err != nil {
		t.Fatalf("the save: %v: %s", err, out)
	}
	return b
}

func luauCutClone(b []byte) []byte { return append([]byte(nil), b...) }

// TestLuauFixedEvolutionNumericWidening is numeric-evolution (E3, C8, R20): an
// older writer's int16 into an int32 and float32 into a float64 decode
// exactly, the int16 sign-extended, each counted widened once per record, and
// a field the writer never carried lands its declared default. The negative
// controls drop the widened count and zero-extend the int16.
func TestLuauFixedEvolutionNumericWidening(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	older := luauCutSource(t, "package probe\nfixed table N\n{\n    lead uint32 = 1\n    v int16 = 0\n    f float32 = 0.0\n    trail uint32 = 2\n}\n")
	newer := luauCutSource(t, "package probe\nfixed table N\n{\n    lead uint32 = 1\n    v int32 = 0\n    f float64 = 0.0\n    trail uint32 = 2\n    added int32 = 77\n}\n")
	eo := luauCutEntry(t, older, "N")
	body := binary.LittleEndian.AppendUint32(nil, 5)
	body = binary.LittleEndian.AppendUint16(body, 0xFFF9) // -7
	body = binary.LittleEndian.AppendUint32(body, 0xC49A5200)
	body = binary.LittleEndian.AppendUint32(body, 6)
	file := luauCutFile(eo.Wire, eo.Layout, body, body)
	lineage := luauCutLineage(t, older, "N")
	got := luauCutGen(t, newer, lineage).load(t, "N", file)
	if got.head != "ok 2 w4 u0" {
		t.Errorf("the widening read: %s, want two widened entries per record", got.head)
	}
	for k, w := range map[string]string{"r0.Lead": "5", "r0.V": "-7", "r0.F#": "0xC0934A4000000000", "r0.Trail": "6", "r0.Added": "77", "r1.V": "-7"} {
		if got.dump[k] != w {
			t.Errorf("%s landed %q, want %s", k, got.dump[k], w)
		}
	}
	g := luauCutGen(t, newer, lineage)
	g.plant(t, "\t\tif ladder(w.kind, r.kind) then\n\t\t\tcx.widened += 1\n", "\t\tif ladder(w.kind, r.kind) then\n")
	if g.load(t, "N", file).head == got.head {
		t.Errorf("the negative control: a widening not counted still reads the same")
	}
	g = luauCutGen(t, newer, lineage)
	g.plant(t, "v = signed(k) and buffer.readi16(buf, at) or buffer.readu16(buf, at)", "v = buffer.readu16(buf, at)")
	if g.load(t, "N", file).dump["r0.V"] == "-7" {
		t.Errorf("the negative control: a zero-extended int16 still lands -7")
	}
}

// TestLuauFixedEvolutionReports is reports (E9, R16, R18): the fixed runtime
// carries no repeated-key counter; a full-width range that cannot be broken
// reads 256 records with nothing refused; and the unknown census over an older
// writer's dropped nested field is counted once per declared field, the same
// on a second load (the counters are each read's own). The negative control
// drops the census.
func TestLuauFixedEvolutionReports(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	g := luauCutGen(t, luauCutSource(t, "package probe\nfixed table R\n{\n    a int8 = 0 | min = -128, max = 127\n    b uint8\n}\n"), nil)
	if src := string(g.files["schema_fixed.luau"]); strings.Contains(src, "repeatedKey") || strings.Contains(src, "duplicate") {
		t.Errorf("E9: the fixed runtime counts a repeated key")
	}
	var bodies [][]byte
	for x := range 256 {
		bodies = append(bodies, []byte{byte(x), byte(x)})
	}
	e := luauCutEntry(t, g.unit, "R")
	if got := g.load(t, "R", luauCutFile(e.Wire, e.Layout, bodies...)).head; got != "ok 256 w0 u0" {
		t.Errorf("R18: a range that cannot be broken refused or moved: %s", got)
	}

	older := luauCutCorpus(t, "VOLD_unknown_census")
	newer := luauCutCorpus(t, "VNEW_unknown_census")
	old := luauCutGen(t, older, nil).save(t, "Census", "for i = 1, 3 do local v = T.NewCensus(); v.Lead = i; v.Items[1].A = 10 * i; v.Items[1].Drop = 99; values[i] = v end")
	census := func(g *luauCutUnit) string {
		return strings.Join(strings.Fields(g.run(t, luauCutPrelude+`for _ = 1, 2 do
	local rep: any = {}
	local v = F.read(T.CensusDescriptor, fixedOf("Census"), I.file, rep)
	if v == nil then
		print("refused " .. tostring(rep.reason))
	else
		print(string.format("%d %d %d %d", #v, rep.widened, rep.unknown, v[3].Items[1].A))
	end
end
`, map[string][]byte{"file": old})), " ")
	}
	lineage := luauCutLineage(t, older, "Census")
	if got := census(luauCutGen(t, newer, lineage)); got != "3 0 1 30 3 0 1 30" {
		t.Errorf("R16: the census over three records, loaded twice: %q", got)
	}
	gc := luauCutGen(t, newer, lineage)
	gc.plant(t, "if rById[wc.id] == nil then c.unknown += 1 end", "")
	if got := census(gc); got == "3 0 1 30 3 0 1 30" {
		t.Errorf("the negative control: the dropped census still counts")
	}
}

// TestLuauFixedEvolutionArrayBounds is array-bounds (C1, C2, R11): the plan
// carries the WRITER's bound: a counted array the old writer bounded at 4 and
// the reader at 8 refuses a count of -1 and of 7, and reads at exactly 4. The
// negative control bounds the count by the reader's 8.
func TestLuauFixedEvolutionArrayBounds(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	older := luauCutCorpus(t, "VOLD_array_bounded_grow")
	newer := luauCutCorpus(t, "VNEW_array_bounded_grow")
	base := luauCutGen(t, older, nil).save(t, "ArrayBoundedGrow", "local v = T.NewArrayBoundedGrow(); v.Lead = 1; v.Vals = { 10, 20, 30, 0 }; v.ValsCount = 3; v.Trail = 2; values[1] = v")
	countAt := 20 + int(binary.LittleEndian.Uint32(base[16:20])) + 8 + 4
	with := func(n uint32) []byte {
		b := luauCutClone(base)
		binary.LittleEndian.PutUint32(b[countAt:], n)
		return b
	}
	lineage := luauCutLineage(t, older, "ArrayBoundedGrow")
	g := luauCutGen(t, newer, lineage)
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
	if got := g.load(t, "ArrayBoundedGrow", base); got.dump["r0.Vals.3"] != "30" || got.dump["r0.ValsCount"] != "3" || got.dump["r0.Trail"] != "2" {
		t.Errorf("the control's values: %v", got.dump)
	}
	gc := luauCutGen(t, newer, lineage)
	gc.plant(t, `if count > theirs then error(refuse("count_over_bound"), 0) end`, `if count > mine then error(refuse("count_over_bound"), 0) end`)
	if got := gc.load(t, "ArrayBoundedGrow", with(7)).head; got == "refused count_over_bound" {
		t.Errorf("the negative control: the reader's bound still refuses the writer's overrun")
	}
}

// TestLuauFixedEvolutionText is text (C3, C4, C5, R24): a length past the
// field's capacity refuses; ill-formed UTF-8 or a zero in the USED bytes and an
// unpaired surrogate in a wstring's used units refuse; a hostile byte in the
// slack past the used length is not read and not a refusal. The negative
// control drops the UTF-8 check.
func TestLuauFixedEvolutionText(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	g := luauCutGen(t, luauCutSource(t, "package probe\nfixed table T\n{\n    s string(4)\n}\n"), nil)
	clean := g.save(t, "T", "local v = T.NewT(); buffer.writestring(v.S, 0, \"ab\"); v.SLength = 2; values[1] = v")
	body := len(clean) - 8
	cut := func(f func(b []byte)) []byte { b := luauCutClone(clean); f(b); return b }
	cases := []struct {
		name string
		file []byte
		want string
	}{
		{"C3 length 5 of 4", cut(func(b []byte) { b[body] = 5 }), "refused length_over_bound"},
		{"C4 0xff in the used bytes", cut(func(b []byte) { b[body+4] = 0xff }), "refused text_ill_formed"},
		{"C4 a zero in the used bytes", cut(func(b []byte) { b[body+5] = 0 }), "refused text_ill_formed"},
		{"R24 0xff in the slack", cut(func(b []byte) { b[body+6], b[body+7] = 0xff, 0xff }), "ok 1 w0 u0"},
	}
	for _, c := range cases {
		if got := g.load(t, "T", c.file).head; got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	if got := g.load(t, "T", clean); got.head != "ok 1 w0 u0" || got.dump["r0.SLength"] != "2" || !strings.HasPrefix(got.dump["r0.S"], "6162") {
		t.Errorf("the control: %s %v", got.head, got.dump)
	}
	gc := luauCutGen(t, g.unit, nil)
	gc.plant(t, "elseif utf8IllFormed(buf, w.off + at + COUNT, length) then", "elseif false then")
	if got := gc.load(t, "T", cases[1].file).head; got == cases[1].want {
		t.Errorf("the negative control: ill-formed text still refuses")
	}
}

// TestLuauFixedEvolutionScalarBounds is scalar-bounds (C7, R30): a ranged
// int32, int128 and uint16 each refuse below and above their ranges, alone and
// together, and read in range; a compressed float rides as the float32 itself,
// saved unquantized, refuses past either end, and a -0 lands as +0 (SPEC-TABLES
// §3.3); its resolution is in the definitions digest under 'Q', so a finer or a
// dropped triple reads the old writer's bits unchanged. The negative controls
// drop the integer bound and the 128-bit bound.
func TestLuauFixedEvolutionScalarBounds(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	u := luauCutSource(t, "package probe\nfixed table T\n{\n    x int32 = 0 | min = -5, max = 5\n    s int128 = 0 | min = -500, max = 500\n    u uint16 = 10 | min = 10, max = 1000\n}\n")
	e := luauCutEntry(t, u, "T")
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
	g := luauCutGen(t, u, nil)
	got := g.load(t, "T", luauCutFile(e.Wire, e.Layout, body(3, -42, 500)))
	if got.head != "ok 1 w0 u0" || got.dump["r0.X"] != "3" || got.dump["r0.S.lo"] != "-42" || got.dump["r0.S.hi"] != "-1" || got.dump["r0.U"] != "500" {
		t.Errorf("C7 in range: %s %v", got.head, got.dump)
	}
	outside := []struct {
		name string
		b    []byte
	}{
		{"all below", body(-100, -600, 5)}, {"all above", body(100, 600, 2000)},
		{"x below", body(-6, 0, 10)}, {"x above", body(6, 0, 10)},
		{"s below", body(0, -501, 10)}, {"s above", body(0, 501, 10)},
		{"u below", body(0, 0, 9)}, {"u above", body(0, 0, 1001)},
	}
	for _, c := range outside {
		if got := g.load(t, "T", luauCutFile(e.Wire, e.Layout, c.b)).head; got != "refused out_of_range" {
			t.Errorf("C7 %s: %s, want refused out_of_range", c.name, got)
		}
	}
	gc := luauCutGen(t, u, nil)
	gc.plant(t, "elseif lessThan(v, lo, isSigned) or lessThan(hi, v, isSigned) then", "elseif false then")
	if got := gc.load(t, "T", luauCutFile(e.Wire, e.Layout, body(-6, 0, 10))).head; got == "refused out_of_range" {
		t.Errorf("the negative control: the dropped bound still refuses")
	}
	gc = luauCutGen(t, u, nil)
	gc.plant(t, "if cmp128(v, lo, unsigned) < 0 or cmp128(v, hi, unsigned) > 0 then", "if false then")
	if got := gc.load(t, "T", luauCutFile(e.Wire, e.Layout, body(0, 501, 10))).head; got == "refused out_of_range" {
		t.Errorf("the negative control: the dropped 128-bit bound still refuses")
	}

	const cf = "package probe\nfixed table Reading\n{\n    lead uint32 = 1\n    aim float32 = 0.0%s\n    trail uint32 = 2\n}\n"
	old := luauCutSource(t, fmt.Sprintf(cf, " | min = -1.0, max = 2.0, resolution = 0.1"))
	if d := ir.TableFixedDefinitionsDigest(old.Tables["Reading"]); !bytes.Contains(d, []byte{'Q'}) {
		t.Errorf("R30: the digest does not carry the resolution under 'Q'")
	}
	gr := luauCutGen(t, old, nil)
	saved := gr.save(t, "Reading", "local v = T.NewReading(); local b = buffer.create(4); buffer.writeu32(b, 0, 0x3e99999a); v.Aim = buffer.readf32(b, 0); values[1] = v")
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
		f := luauCutClone(saved)
		binary.LittleEndian.PutUint32(f[at:], c.bits)
		if got := gr.load(t, "Reading", f); got.head != c.head || got.dump["r0.Aim#"] != c.aim {
			t.Errorf("R30 0x%08x: %s aim=%s, want %s %s", c.bits, got.head, got.dump["r0.Aim#"], c.head, c.aim)
		}
	}
	lin := luauCutLineage(t, old, "Reading")
	for name, src := range map[string]string{"finer": fmt.Sprintf(cf, " | min = -1.0, max = 2.0, resolution = 0.01"), "dropped": fmt.Sprintf(cf, "")} {
		gn := luauCutGen(t, luauCutSource(t, src), lin)
		if got := gn.load(t, "Reading", saved); got.head != "ok 1 w0 u0" || got.dump["r0.Aim#"] != "0x3FD3333340000000" {
			t.Errorf("R30 %s: %s aim=%s", name, got.head, got.dump["r0.Aim#"])
		}
	}
}

const luauCutOrdinalOld = "package probe\nenum Tier { Bronze, Silver }\nunion Pick\n{\n    a int32\n    b int32\n}\nfixed table Lineage\n{\n    tier Tier\n    pick Pick\n    seq int32\n}\n"

// TestLuauFixedEvolutionOrdinals is ordinals (C9, C10, R31): an enum ordinal
// past the writer's variants and a union tag past its arms each refuse
// out_of_range, alone and together, on the identity plan and on a compiled
// plan whose reader declares MORE variants and arms (the writer's count
// governs); in-set ordinals read. The negative controls drop the tag check on
// each plan and the enum check.
func TestLuauFixedEvolutionOrdinals(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	uOld := luauCutSource(t, luauCutOrdinalOld)
	uNew := luauCutSource(t, strings.Replace(strings.Replace(luauCutOrdinalOld, "Silver }", "Silver, Gold }", 1), "    b int32\n", "    b int32\n    c int32\n", 1))
	eo := luauCutEntry(t, uOld, "Lineage")
	both := luauCutFile(eo.Wire, eo.Layout, []byte{3, 3, 1, 0, 0, 0, 42, 0, 0, 0})
	tier := luauCutFile(eo.Wire, eo.Layout, []byte{3, 2, 9, 0, 0, 0, 42, 0, 0, 0})
	tag := luauCutFile(eo.Wire, eo.Layout, []byte{2, 3, 9, 0, 0, 0, 42, 0, 0, 0})
	clean := luauCutFile(eo.Wire, eo.Layout, []byte{2, 2, 9, 0, 0, 0, 42, 0, 0, 0})
	lin := luauCutLineage(t, uOld, "Lineage")
	for name, g := range map[string]*luauCutUnit{"identity": luauCutGen(t, uOld, nil), "compiled": luauCutGen(t, uNew, lin)} {
		for fname, f := range map[string][]byte{"both": both, "tier": tier, "tag": tag} {
			if got := g.load(t, "Lineage", f).head; got != "refused out_of_range" {
				t.Errorf("%s %s: %s, want refused out_of_range", name, fname, got)
			}
		}
		if c := g.load(t, "Lineage", clean); c.head != "ok 1 w0 u0" || c.dump["r0.Tier"] != "2" || c.dump["r0.Pick.Type"] != "2" || c.dump["r0.Pick.B"] != "9" || c.dump["r0.Seq"] != "42" {
			t.Errorf("%s, the control: %s %v", name, c.head, c.dump)
		}
	}
	const tagCheck = "if tag > #w.children then"
	g := luauCutGen(t, uOld, nil)
	g.plant(t, tagCheck, "if false then")
	if got := g.load(t, "Lineage", tag).head; got == "refused out_of_range" {
		t.Errorf("the negative control: an unchecked identity tag still refuses")
	}
	g = luauCutGen(t, uNew, lin)
	g.plant(t, tagCheck, "if false then")
	if got := g.load(t, "Lineage", tag).head; got == "refused out_of_range" {
		t.Errorf("the negative control: an unchecked compiled tag still refuses")
	}
	g = luauCutGen(t, uOld, nil)
	g.plant(t, "if raw > #w.children then", "if false then")
	if got := g.load(t, "Lineage", tier).head; got == "refused out_of_range" {
		t.Errorf("the negative control: an unchecked ordinal still refuses")
	}
}

// TestLuauFixedEvolutionNormalization is normalization (C12, C13, R17) under
// the one read: a bool byte or a present byte other than 0 or 1 refuses
// wire_damaged (docs/FIXED-FORM-ALGORITHM.md §4.5), never normalized; 0 and 1
// read, and the payload behind a present byte of 1 lands. The negative
// control drops the byte check.
func TestLuauFixedEvolutionNormalization(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	g := luauCutGen(t, luauCutSource(t, "package probe\nfixed table T\n{\n    flag bool\n    opt ?int32\n}\n"), nil)
	file := g.save(t, "T", "local v = T.NewT(); v.Opt = 99; v.OptPresent = true; values[1] = v")
	body := len(file) - 6
	set := func(flag, present, payload byte) []byte {
		b := luauCutClone(file)
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
	if got := g.load(t, "T", set(1, 1, 42)); got.head != "ok 1 w0 u0" || got.dump["r0.Flag"] != "true" || got.dump["r0.OptPresent"] != "true" || got.dump["r0.Opt"] != "42" {
		t.Errorf("the control: %s %v", got.head, got.dump)
	}
	if got := g.load(t, "T", set(0, 0, 42)); got.head != "ok 1 w0 u0" || got.dump["r0.Flag"] != "false" || got.dump["r0.OptPresent"] != "false" {
		t.Errorf("the control, absent: %s %v", got.head, got.dump)
	}
	gc := luauCutGen(t, g.unit, nil)
	gc.plant(t, `if b > 1 then error({ name = "wire_damaged"`, `if false then error({ name = "wire_damaged"`)
	if got := gc.load(t, "T", set(0x7f, 1, 42)).head; got == "refused wire_damaged" {
		t.Errorf("the negative control: an unchecked bool byte still refuses")
	}
}

const luauCutHostSchema = "package probe\nfixed table Host\n{\n    small int32 = 5 | min = 0, max = 1000\n    marks [..4]int32 | min = 0, max = 1000\n    note ?int32 | min = 0, max = 1000\n    tail int32 = 7\n}\n"

// luauCutHostSweep poisons every byte of the record six ways and reads each
// image: a refusal must be by a name the fixed form declares, a read must
// land every live value in its bound. It prints the failures and the cases.
const luauCutHostSweep = `local names = { out_of_range = true, wire_damaged = true, count_over_bound = true, length_over_bound = true, no_layout = true }
local function inBound(v: any): boolean
	if v.Small < 0 or v.Small > 1000 then return false end
	if v.MarksCount > 4 then return false end
	for i = 1, v.MarksCount do
		if v.Marks[i] < 0 or v.Marks[i] > 1000 then return false end
	end
	if v.NotePresent and (v.Note < 0 or v.Note > 1000) then return false end
	return true
end
local v = T.NewHost()
v.Small = 5
v.Marks[1], v.Marks[2], v.MarksCount = 10, 20, 2
v.NotePresent = true
v.Note = 30
local clean = F.save(T.HostDescriptor, fixedOf("Host"), { v })
local n = buffer.len(clean)
local bad, cases = 0, 0
for at = n - %d, n - 1 do
	for _, p in { 0x00, 0x01, 0x02, 0x7f, 0x80, 0xff } do
		local b = buffer.create(n)
		buffer.copy(b, 0, clean, 0, n)
		buffer.writeu8(b, at, p)
		local rep: any = {}
		local got = F.read(T.HostDescriptor, fixedOf("Host"), b, rep)
		if got == nil then
			if not names[rep.reason] then bad += 1; print("unnamed " .. tostring(rep.reason)) end
		elseif #got ~= 1 or not inBound(got[1]) then
			bad += 1
		end
		cases += 1
	end
end
print(bad, cases)
`

// TestLuauFixedEvolutionHostileInput is hostile-input (P3, P4): every byte of
// a record poisoned six ways either refuses by a declared name or lands every
// live value in its bound; a bad hash on the LAST record of a batch refuses
// the whole load; and the wrong plan goes red: B's reader refuses A's file,
// and A's file announced under B's hash is refused, never read through B's
// plan. The negative control drops the signed range check and requires the
// sweep to find out-of-bound values.
func TestLuauFixedEvolutionHostileInput(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	u := luauCutSource(t, luauCutHostSchema)
	record := luauCutEntry(t, u, "Host").Record
	g := luauCutGen(t, u, nil)
	sweep := luauCutPrelude + fmt.Sprintf(luauCutHostSweep, record)
	if got := strings.Join(strings.Fields(g.run(t, sweep, nil)), " "); got != fmt.Sprintf("0 %d", 6*record) {
		t.Errorf("P3: the sweep: %q, want 0 failures of %d", got, 6*record)
	}
	two := g.save(t, "Host", "local v = T.NewHost(); values = { v, v }")
	two[len(two)-int(record)] ^= 1
	if got := g.load(t, "Host", two).head; got != "refused no_layout" {
		t.Errorf("P3: the last record's hash: %s", got)
	}
	gc := luauCutGen(t, u, nil)
	gc.plant(t, "elseif lessThan(v, lo, isSigned) or lessThan(hi, v, isSigned) then", "elseif false then")
	if got := strings.Fields(gc.run(t, sweep, nil)); len(got) > 0 && got[0] == "0" {
		t.Errorf("the negative control: the sweep without the bound finds nothing: %v", got)
	}

	ab := luauCutSource(t, "package probe\nfixed table A\n{\n    lead uint32 = 0\n    x int32 = 0\n    y int32 = 0\n    trail uint32 = 0\n}\nfixed table B\n{\n    p int8 = 0\n    q int8 = 0\n}\n")
	gab := luauCutGen(t, ab, nil)
	aFile := gab.save(t, "A", "local v = T.NewA(); v.Lead = 0xAABBCCDD; v.X = 111; v.Y = 222; v.Trail = 0x11223344; values[1] = v")
	if got := gab.load(t, "B", aFile).head; !strings.HasPrefix(got, "refused ") {
		t.Errorf("P4: B's reader on A's file: %s, want a refusal", got)
	}
	asB := luauCutClone(aFile)
	binary.LittleEndian.PutUint64(asB[8:], luauCutEntry(t, ab, "B").Wire)
	if got := gab.load(t, "B", asB).head; !strings.HasPrefix(got, "refused ") {
		t.Errorf("P4: A's file announced as B: %s, want a refusal", got)
	}
	if got := gab.load(t, "A", aFile); got.head != "ok 1 w0 u0" || got.dump["r0.X"] != "111" || got.dump["r0.Trail"] != "287454020" {
		t.Errorf("P4: the right plan: %s %v", got.head, got.dump)
	}
}
