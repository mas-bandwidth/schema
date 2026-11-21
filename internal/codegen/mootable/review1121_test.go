package mootable_test

// The 2026-11-21 review's moo findings and moo's cells of the cross-leg rules
// (docs/roadmap.sexp, work-set review-owed-2026-11-21):
//
//   - MOO-1, cross-leg negative-zero: a compressed float holding -0 is stored
//     as +0 by every writer (docs/SPEC-TABLES.md §3.3, ruling
//     fixed-compressed-negzero): the variable save (under a zero default, a
//     non-zero default and as an array element), the fixed save and the text
//     write and the message-form save each write exactly what they write
//     for +0.
//   - cross-leg float32-range: a float32 field's declared range is held at
//     float32 (docs/SPEC.md §4.6), so the float32 nearest an end float32
//     cannot spell exactly is in range on every write and read.
//   - MOO-2: a field's json key is emitted as a moo string literal, so every
//     key schema check admits keeps its bytes.
//
// Each runs the generated moo, so each is gated by slowtest.

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const review1121Prelude = `--!strict
local inputs = require("./inputs")
local load = require("./table_load")
local save = require("./table_save")
local F = require("./table_fixed")
local St = require("./schema_tables")
local P: any = require("./ProbeTable")
local _ = inputs
local function hex(b: buffer?): string
	if b == nil then return "nil" end
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
local function vsave(name: string, v: any): string
	local out, why = save.save(St.lookup(name), v)
	if out == nil then return "refused " .. tostring(why and why.name) end
	return hex(out)
end
local function fsave(name: string, v: any): string
	local d = St.lookup(name)
	local out, why = F.save(d, F.lookup(name) :: any, { v })
	if out == nil then return "refused " .. tostring(why and why.name) end
	return hex(out)
end
local function jwrite(root: string, v: any): string
	local n = P[root .. "ToJsonMeasure"](v)
	if n < 0 then return "refused" end
	local out = buffer.create(n)
	if P[root .. "ToJson"](v, out) ~= n then return "measure and write disagree" end
	return buffer.tostring(out)
end
`

// TestMooCompressedNegZeroStoresAsPlusZero is MOO-1: every moo writer stores
// a compressed float's -0 as +0, so a value holding -0 writes the same bytes
// as the value holding +0.
func TestMooCompressedNegZeroStoresAsPlusZero(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table writers)")
	t.Parallel()
	const src = "package probe\n" +
		"table V\n{\n" +
		"    a float32 | min = -1, max = 1, resolution = 0.01\n" +
		"    b float32 = 0.5 | min = -1, max = 1, resolution = 0.01\n" +
		"    c [..4]float32 | min = -1, max = 1, resolution = 0.01\n" +
		"}\n" +
		"fixed table X\n{\n" +
		"    a float32 | min = -1, max = 1, resolution = 0.01\n" +
		"    b float32 = 0.5 | min = -1, max = 1, resolution = 0.01\n" +
		"}\n"
	u := unitFromSource(t, src)
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	g := writeUnit(t, u, files)
	main := review1121Prelude + `
local function v(z: number): any
	local x: any = P.NewV()
	x.A = z
	x.B = z
	x.C[1] = 0x3f000000
	x.C[2] = z
	x.CCount = 2
	return x
end
local function x(z: number): any
	local r: any = P.NewX()
	r.A = z
	r.B = z
	return r
end
print("variable " .. vsave("V", v(0x80000000)) .. " " .. vsave("V", v(0)))
print("fixed " .. fsave("X", x(0x80000000)) .. " " .. fsave("X", x(0)))
print("json " .. hex(buffer.fromstring(jwrite("V", v(0x80000000)))) .. " " .. hex(buffer.fromstring(jwrite("V", v(0)))))
local MS = require("./table_message")
local function msave(r: any): string
	local rep: any = {}
	local out = MS.SaveMessages("X", { r }, rep)
	if out == nil then return "refused " .. tostring(rep.reason) end
	return hex(out)
end
print("message " .. msave(x(0x80000000)) .. " " .. msave(x(0)))
`
	out := g.run(t, main, nil)
	lines := strings.Split(out, "\n")
	if len(lines) != 4 {
		t.Fatalf("the probe printed %d lines, want 4:\n%s", len(lines), out)
	}
	for _, line := range lines {
		f := strings.Fields(line)
		if len(f) != 3 {
			t.Errorf("unexpected line %q", line)
			continue
		}
		if strings.HasPrefix(f[1], "refused") || strings.HasPrefix(f[2], "refused") {
			t.Errorf("%s: a writer refused: %q", f[0], line)
			continue
		}
		if f[1] != f[2] {
			t.Errorf("%s: the value holding -0 writes\n  %s\nwhere the value holding +0 writes\n  %s", f[0], f[1], f[2])
		}
	}
}

// TestMooFloat32RangeHeldAtFloat32 is moo's cell of the cross-leg
// float32-range rule: under min = -0.1, max = 0.1 the float32 values nearest
// the ends are in range, so the variable and fixed saves write them, the
// loads land them, and the text read takes "0.1" and "-0.1" without counting
// them out of range.
func TestMooFloat32RangeHeldAtFloat32(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table runtime)")
	t.Parallel()
	const src = "package probe\n" +
		"table V\n{\n" +
		"    r float32 | min = -0.1, max = 0.1, resolution = 0.001\n" +
		"    s [..2]float32 | min = -0.1, max = 0.1, resolution = 0.001\n" +
		"}\n" +
		"fixed table X\n{\n" +
		"    r float32 | min = -0.1, max = 0.1, resolution = 0.001\n" +
		"}\n"
	u := unitFromSource(t, src)
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	g := writeUnit(t, u, files)
	hi, lo := math.Float32bits(0.1), math.Float32bits(-0.1)
	main := review1121Prelude + fmt.Sprintf(`
for _, z in { 0x%08x, 0x%08x } do
	local x: any = P.NewV()
	x.R = z
	x.S[1] = z
	x.SCount = 1
	local out, why = save.save(St.lookup("V"), x)
	if out == nil then
		print("variable-save " .. tostring(why and why.name))
	else
		local rep = load.newReport()
		local back: any = load.load("V", out, rep)
		if back == nil or rep.refused > 0 then
			print("variable-load " .. tostring(rep.reason))
		else
			print("variable " .. string.format("%%08x %%08x", back.R, back.S[1]))
		end
	end
	local r: any = P.NewX()
	r.R = z
	local d = St.lookup("X")
	local fx = F.lookup("X") :: any
	local file, fwhy = F.save(d, fx, { r })
	if file == nil then
		print("fixed-save " .. tostring(fwhy and fwhy.name))
	else
		local frep = load.newReport()
		local values = F.load(d, fx, file, frep)
		if values == nil or #values ~= 1 then
			print("fixed-load " .. tostring(frep.reason))
		else
			print("fixed " .. string.format("%%08x", values[1].R))
		end
	end
	local text = jwrite("V", x)
	local y: any = P.NewV()
	local jrep: any = {}
	local ok = P.VFromJson(y, text, jrep)
	print("json " .. tostring(ok) .. " " .. tostring(jrep.outOfRange or 0) .. " " .. string.format("%%08x %%08x", y.R, y.S[1]))
end
`, hi, lo)
	out := g.run(t, main, nil)
	var want []string
	for _, z := range []uint32{hi, lo} {
		want = append(want,
			fmt.Sprintf("variable %08x %08x", z, z),
			fmt.Sprintf("fixed %08x", z),
			fmt.Sprintf("json true 0 %08x %08x", z, z))
	}
	got := strings.Split(out, "\n")
	if len(got) != len(want) {
		t.Fatalf("the probe printed %d lines, want %d:\n%s", len(got), len(want), out)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

// TestMooJsonKeyKeepsItsBytes is MOO-2: a json key holding bytes Go's %q
// would spell in an escape moo does not share keeps its bytes. The text the
// compiler's own engine writes reads with nothing unknown and writes back
// byte for byte.
func TestMooJsonKeyKeepsItsBytes(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted text form)")
	t.Parallel()
	keys := []string{
		"tag\U000E0041",   // a format character past U+FFFF
		"sep\u2028line",   // a line separator
		"del\x7f",         // DEL
		"back\\slash",     // a backslash (a schema string has no escapes)
		"bell\a",          // BEL
		"accent\u00e9ok",  // printable non-ASCII
		"emoji\U0001F600", // printable past U+FFFF
	}
	var b strings.Builder
	b.WriteString("package probe\ntable V\n{\n")
	for i, k := range keys {
		fmt.Fprintf(&b, "    f%d int32 | json = \"%s\"\n", i, k)
	}
	b.WriteString("}\n")
	u := unitFromSource(t, b.String())
	for i, f := range u.Tables["V"].Fields {
		if f.JsonKey != keys[i] {
			t.Fatalf("field %s: the unit holds json key %q, want %q", f.Name, f.JsonKey, keys[i])
		}
	}
	m := tabletext.NewModel(u)
	inst := m.New(u.Tables["V"])
	var text strings.Builder
	text.WriteString("{")
	for i, k := range keys {
		if i > 0 {
			text.WriteString(",")
		}
		key, err := json.Marshal(k)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&text, "%s:%d", key, i+1)
	}
	text.WriteString("}")
	var rep tabletext.Report
	if !m.Read(inst, []byte(text.String()), &rep) || rep.Refused || rep.Unknown != 0 {
		t.Fatalf("the Go engine does not read the probe text cleanly: %+v", rep)
	}
	want, err := m.Write(inst)
	if err != nil {
		t.Fatal(err)
	}
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	g := writeUnit(t, u, files)
	main := review1121Prelude + `
local y: any = P.NewV()
local rep: any = {}
local ok = P.VFromJson(y, inputs.text, rep)
print(tostring(ok) .. " " .. tostring(rep.unknown or 0))
print(hex(buffer.fromstring(jwrite("V", y))))
`
	out := g.run(t, main, map[string][]byte{"text": want})
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("the probe printed %d lines, want 2:\n%s", len(lines), out)
	}
	if lines[0] != "true 0" {
		t.Errorf("moo's read of the engine's text: %s, want true 0 (read, nothing unknown)", lines[0])
	}
	// moo's writer ends the text with a line feed (as the golden texts do);
	// the engine's Write does not
	if lines[1] != fmt.Sprintf("%x0a", want) {
		t.Errorf("moo writes back\n  %s\nwant the engine's, then a line feed\n  %x", lines[1], want)
	}
}

// variableSaveContractSchema holds every row of the write contract
// (docs/SPEC.md §5.1) a variable table can carry, on fields that are neither
// required nor top-level only: an enum, a ranged array element, a counted
// array's minimum, an optional, plain floats, inline and buffer text, a
// union's tag and its arm's payload, and a keyed array's element.
const variableSaveContractSchema = `package probe
enum Color { red, green }
union U
{
    n int32 | min = -5, max = 5
    s string(4)
}
table W
{
    e Color
    arr [..4]int32 | min = -5, max = 5
    mn [2..4]int32
    m map[uint32]U
    o ?int32 | min = -5, max = 5
    p float32
    q float64
    s string(8)
    t *string
    u U
    ka [Color]int32 | min = -5, max = 5
    au [..2]U
}
`

var variableSaveMutations = []struct{ name, set string }{
	{"enum-outside-set", "v.E = 7"},
	{"array-element-off-range", "v.Arr[1] = 6\nv.ArrCount = 1"},
	{"count-below-min", "v.MnCount = 1"},
	{"count-above-max", "v.MnCount = 5"},
	{"optional-off-range", "v.O = 6\nv.OPresent = true"},
	{"plain-float32-nan", "v.P = 0x7fc00000"},
	{"plain-float32-infinity", "v.P = 0x7f800000"},
	{"plain-float64-nan", "v.Q = serialize.Uint64.new(0, 0x7ff80000)"},
	{"string-not-utf8", `v.S = "\xff"`},
	{"string-zero-byte", `v.S = "a\0b"`},
	{"string-too-long", `v.S = "123456789"`},
	{"blob-string-not-utf8", `v.T = buffer.fromstring("\xc3")`},
	{"union-tag-outside-set", "v.U.Type = 9"},
	{"union-arm-off-range", "v.U.Type = 1\nv.U.N = 6"},
	{"union-arm-string-too-long", `v.U.Type = 2` + "\n" + `v.U.S = "12345"`},
	{"map-value-arm-off-range", "local e = T.NewWMEntry()\ne.Key = 1\ne.Value.Type = 1\ne.Value.N = 6\nv.M = { e }"},
	{"keyed-element-off-range", "v.Ka[2] = -6"},
	{"array-arm-off-range", "v.Au[1].Type = 1\nv.Au[1].N = 6\nv.AuCount = 1"},
}

// TestMooVariableSaveRefusesTheWriteContract is moo's cell of the cross-leg
// variable-save-refuses rule: the variable-form Save and its Measure refuse
// every value docs/SPEC.md §5.1 lists, on every field that carries it, and the
// probe value they are mutated from saves.
func TestMooVariableSaveRefusesTheWriteContract(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table writers)")
	t.Parallel()
	g := generateFiles(t, map[string]string{"Probe.schema": variableSaveContractSchema})
	var b strings.Builder
	b.WriteString("--!strict\nlocal T: any = require(\"./ProbeTable\")\nlocal St = require(\"./schema_tables\")\nlocal S = require(\"./table_save\")\nlocal serialize = require(\"./serialize\")\nlocal _ = serialize\n")
	b.WriteString(`local d = St.lookup("W")
local function verdict(v: any): string
	local ok, out = pcall(S.save, d, v)
	local okm, n = pcall(S.measure, d, v)
	local s = if not ok then "raised" elseif out == nil then "refused" else "written"
	local m = if not okm then "raised" elseif n == nil or n < 0 then "refused" else "written"
	return s .. "/" .. m
end
print("base\t" .. verdict(T.NewW()))
`)
	for _, m := range variableSaveMutations {
		fmt.Fprintf(&b, "do\n\tlocal v: any = T.NewW()\n\t%s\n\tprint(%q .. \"\\t\" .. verdict(v))\nend\n", strings.ReplaceAll(m.set, "\n", "\n\t"), m.name)
	}
	got := strings.Split(g.run(t, b.String(), nil), "\n")
	if len(got) != len(variableSaveMutations)+1 {
		t.Fatalf("the probe printed %d lines for %d mutations:\n%s", len(got), len(variableSaveMutations), strings.Join(got, "\n"))
	}
	if got[0] != "base\twritten/written" {
		t.Fatalf("the unmutated value: %q, want written by Save and Measure", got[0])
	}
	for i, m := range variableSaveMutations {
		if want := m.name + "\trefused/refused"; got[i+1] != want {
			t.Errorf("%q, want %q", got[i+1], want)
		}
	}
}

// TestMooCountedArrayBelowMinRefusedOnRead is moo's cell of the cross-leg
// counted-array-min rule on the read side: bytes a [..4] declaration writes
// with one element are refused by the variable, fixed, text and message
// reads of the
// same field declared [2..4] (docs/SPEC.md §5.1 row 3, read answer
// "refused"), and a fresh value is born at the minimum.
func TestMooCountedArrayBelowMinRefusedOnRead(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table runtime)")
	t.Parallel()
	const decl = "package probe\ntable W\n{\n    mn %s\n}\nfixed table X\n{\n    mn %s\n}\n"
	wide := unitFromSource(t, fmt.Sprintf(decl, "[..4]int32", "[..4]int32"))
	narrow := unitFromSource(t, fmt.Sprintf(decl, "[2..4]int32", "[2..4]int32"))
	build := func(u *ir.Unit) *generatedUnit {
		files, err := generate(u)
		if err != nil {
			t.Fatal(err)
		}
		return writeUnit(t, u, files)
	}
	gw, gn := build(wide), build(narrow)
	out := gw.run(t, review1121Prelude+`
local w: any = P.NewW()
w.Mn[1] = 7
w.MnCount = 1
print(vsave("W", w))
local x: any = P.NewX()
x.Mn[1] = 7
x.MnCount = 1
print(fsave("X", x))
local MS = require("./table_message")
print(hex(P.TableMessageAnnouncement))
print(hex(MS.SaveMessages("X", { x }, {})))
`, nil)
	lines := strings.Split(out, "\n")
	if len(lines) != 4 || strings.HasPrefix(lines[0], "refused") || strings.HasPrefix(lines[1], "refused") || lines[3] == "nil" {
		t.Fatalf("the [..4] writers did not write one element:\n%s", out)
	}
	variable, err := hex.DecodeString(lines[0])
	if err != nil {
		t.Fatal(err)
	}
	fixed, err := hex.DecodeString(lines[1])
	if err != nil {
		t.Fatal(err)
	}
	conn, err := hex.DecodeString(lines[2])
	if err != nil {
		t.Fatal(err)
	}
	message, err := hex.DecodeString(lines[3])
	if err != nil {
		t.Fatal(err)
	}
	got := gn.run(t, review1121Prelude+`
local rep = load.newReport()
local v: any = load.load("W", inputs.variable, rep)
print("variable " .. (if v == nil or rep.refused > 0 then "refused" else "landed " .. tostring(v.MnCount)))
local frep = load.newReport()
local d = St.lookup("X")
local values = F.load(d, F.lookup("X") :: any, inputs.fixed, frep)
print("fixed " .. (if values == nil then "refused" else "landed " .. tostring(values[1].MnCount)))
local y: any = P.NewW()
local jrep: any = {}
local ok = P.WFromJson(y, inputs.text, jrep)
print("json " .. (if ok and not jrep.damaged then "landed " .. tostring(y.MnCount) else "refused"))
local MS = require("./table_message")
local vocabulary = MS.NewVocabulary()
local mrep: any = {}
if not MS.AnnounceRead(vocabulary, inputs.conn, mrep) then
	print("message announcement refused " .. tostring(mrep.reason))
else
	local n = MS.LoadMessages(vocabulary, "X", inputs.message, 1, mrep)
	print("message " .. (if n < 1 then "refused" else "landed"))
end
print("born " .. tostring(P.NewW().MnCount) .. " " .. tostring(P.NewX().MnCount))
`, map[string][]byte{"variable": variable, "fixed": fixed, "text": []byte(`{"mn":[7]}`), "conn": conn, "message": message})
	want := "variable refused\nfixed refused\njson refused\nmessage refused\nborn 2 2"
	if got != want {
		t.Errorf("the [2..4] reads of one element:\n%s\nwant\n%s", got, want)
	}
}
