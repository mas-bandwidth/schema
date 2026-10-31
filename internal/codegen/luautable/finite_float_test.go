package luautable_test

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE FINITE-FLOAT GATE OF THE LUAU TABLE FORMS (docs/SPEC-TABLES.md §3.3;
// Glenn, 2026-10-27: "Tables go finite-only too, same rule as packets. NaN/inf
// refused on write and read, -0 and subnormals kept"). One probe answers one
// row per rule and form, and every answer is held to schema-go's engine where
// the engine has the form (internal/tablewire, internal/tablecook): the
// variable read, tolerant and strict, and the variable save and measure; a
// retained unknown float (ruling 3); the message batch read and writers; the
// fixed form's read and write; the cook's open and write; the block accessor's
// latch (ruling 2); and the JSON writer. A finite value of every form still
// rides, so a refusal that over-reaches reds the twin rows.
//
// TestLuauFiniteFloatNegativeControl is the reversed witness: it removes one
// rule's check from the emitted runtime at a time, and the probe must red on
// exactly that rule's rows (make tables-luau-finite-float-negative-control).

const finiteSchema = `package ff

table Var
{
    a  float32
    b  float64
    xs [..4]float32
    ys [..4]float64
}

fixed table Fix
{
    f float32
    d float64
}

fixed table Msg
{
    a  float32
    b  float64
    xs [..4]float32
    ys [..4]float64
}
`

// finiteNewer is the writer one field newer, for the retained unknown float:
// the reader above has no name for z.
const finiteNewer = `package ffnew

table Var
{
    a  float32
    b  float64
    xs [..4]float32
    ys [..4]float64
    z  float64
}
`

// The finite twin: every float a pattern found nowhere else in its file, so a
// non-finite wire is the twin's bytes with one pattern replaced.
const (
	twinA  = 0x3FC00000         // 1.5
	twinB  = 0x4004000000000000 // 2.5
	twinXs = 0x40500000         // 3.25
	twinYs = 0x4013000000000000 // 4.75
	twinZ  = 0x4016000000000000 // 5.5
	nan32  = 0x7FC00001         // a quiet NaN with a payload
	ninf32 = 0xFF800000         // -inf
	inf64  = 0x7FF0000000000000 // +inf
	snan64 = 0x7FF0000000000001 // a signalling NaN
)

func le32(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }
func le64(v uint64) []byte { return binary.LittleEndian.AppendUint64(nil, v) }

// finiteProbe answers every row; ROW lines are "<row> <answer>".
const finiteProbe = `local inputs = require("./inputs")
local T = require("./FFTable")
local St = require("./schema_tables")
local load = require("./schema_load")
local save = require("./schema_save")
local R = require("./schema_refusal")
local Ret = require("./schema_retain")
local M = require("./schema_message")
local F = require("./schema_fixed")
local C = require("./schema_cook")
local CR = require("./schema_cookread")
local B = require("./schema_blockread")
local J = require("./schema_json")

local function row(name: string, answer: string) print(name .. " " .. answer) end
local function report(): any
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
local function hex(b: buffer?): string
	if b == nil then return "nil" end
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do parts[i + 1] = string.format("%02x", buffer.readu8(b, i)) end
	return table.concat(parts)
end
-- patch is b with the one occurrence of old replaced by new
local function patch(b: buffer, old: buffer, new: buffer): buffer
	local n, k = buffer.len(b), buffer.len(old)
	local at = -1
	for i = 0, n - k do
		if buffer.readstring(b, i, k) == buffer.tostring(old) then
			assert(at < 0, "the twin's pattern occurs twice")
			at = i
		end
	end
	assert(at >= 0, "the twin's pattern is absent")
	local out = buffer.create(n)
	buffer.copy(out, 0, b, 0, n)
	buffer.copy(out, at, new, 0, k)
	return out
end
local NAN32 = buffer.readf32(inputs.nan32, 0)
local NINF32 = buffer.readf32(inputs.ninf32, 0)
local INF64 = buffer.readf64(inputs.inf64, 0)
local SNAN64 = buffer.readf64(inputs.snan64, 0)
local function twin(): any
	local v: any = St.lookup("Var").new()
	v.A, v.B = 1.5, 2.5
	v.Xs[1], v.XsCount = 3.25, 1
	v.Ys[1], v.YsCount = 4.75, 1
	return v
end
local Var = St.lookup("Var")

-- THE VARIABLE READ: tolerant and strict, each refusing by name
for _, w in { "varA", "varB", "varXs", "varYs" } do
	local r = report()
	local v = load.loadTolerant("Var", inputs[w], r)
	row("var/read/" .. w, if v == nil then tostring(r.reason) else "loaded")
	local r2: any = {}
	local s = R.ReadStrict(nil, "Var", inputs[w], r2)
	row("var/strict/" .. w, if s == nil then tostring(r2.reason) .. " " .. tostring(r2.field) else "loaded")
end
do
	local r = report()
	local v = load.loadTolerant("Var", inputs.varTwin, r)
	row("var/read/twin", if v == nil then tostring(r.reason) else "loaded")
end

-- THE VARIABLE SAVE AND MEASURE
local writes: { { any } } = {
	{ "a", function(v: any) v.A = NAN32 end },
	{ "b", function(v: any) v.B = INF64 end },
	{ "xs", function(v: any) v.Xs[1] = NINF32 end },
	{ "ys", function(v: any) v.Ys[1] = SNAN64 end },
	{ "narrows-to-inf", function(v: any) v.A = 1e39 end },
}
for _, w in writes do
	local v = twin()
	w[2](v)
	local out, why = save.save(Var, v)
	local m, mwhy = save.measure(Var, v)
	row("var/write/" .. w[1], (if out == nil then tostring(why and why.name) .. " " .. tostring(why and why.field) else "written") ..
		" " .. (if m == nil then tostring(mwhy and mwhy.name) else "measured"))
end
row("var/write/twin", hex((save.save(Var, twin()))))

-- A RETAINED UNKNOWN FLOAT (ruling 3): the plain read skips it, the
-- retaining read refuses it
do
	local r = report()
	row("retain/plain", if load.loadTolerant("Var", inputs.retain, r) == nil then tostring(r.reason) else "loaded")
	local r2: any = report()
	local v = Ret.LoadRetain("Var", inputs.retain, r2)
	row("retain/read", if v == nil then tostring(r2.reason) .. " " .. tostring(r2.field) else "loaded")
	local r3: any = report()
	row("retain/twin", if Ret.LoadRetain("Var", inputs.retainTwin, r3) == nil then tostring(r3.reason) else "loaded")
end

-- THE MESSAGE FORM (a fixed root's, §3.3): the batch read, and the writers
-- over a body that carries one
do
	local voc = M.NewVocabulary()
	local ar: any = { malformed = false, verdict = 0, reason = "", unknown = 0 }
	assert(M.AnnounceRead(voc, inputs.ann, ar))
	for _, w in { "msgA", "msgYs" } do
		local r: any = {}
		local n = M.LoadMessages(voc, "Msg", inputs[w], 256, r)
		row("msg/read/" .. w, if n < 0 then tostring(r.reason) else "loaded")
	end
	local r: any = {}
	local n, bodies = M.LoadMessages(voc, "Msg", inputs.msgTwin, 256, r)
	assert(n == 1, "the twin batch did not load")
	local back = M.SaveMessages(voc, "Msg", bodies, r)
	row("msg/twin", if hex(back) == hex(inputs.msgTwin) then "same" else hex(back))
	local bad = { { buffer = inputs.msgA, from = bodies[1].from, to = bodies[1].to } }
	local rs: any = {}
	local out = M.SaveMessages(voc, "Msg", bad, rs)
	local rm: any = {}
	local size = M.MeasureMessages(voc, "Msg", bad, rm)
	row("msg/write", (if out == nil then tostring(rs.reason) .. " " .. tostring(rs.field) else "written") ..
		" " .. (if size < 0 then tostring(rm.reason) else "measured"))
end

-- THE FIXED FORM: the read refuses before the record lands, the write refuses
do
	local fixed = { hash = T.FixFixedHash, recordBytes = T.FixFixedRecordBytes, layout = T.FixFixedLayout }
	local tw: any = T.FixDescriptor.new()
	tw.F, tw.D = 1.5, 2.5
	local file = F.save(T.FixDescriptor, fixed, { tw })
	local back = F.load(T.FixDescriptor, fixed, file)
	row("fixed/twin", string.format("%s %s", tostring(back[1].F), tostring(back[1].D)))
	local function loads(name: string, b: buffer)
		local ok, err = pcall(F.load, T.FixDescriptor, fixed, b)
		local e: any = err
		row(name, if ok then "loaded" else tostring(e.name) .. " " .. tostring(e.field))
	end
	loads("fixed/read/f", patch(file, inputs.twinA, inputs.nan32))
	loads("fixed/read/d", patch(file, inputs.twinB, inputs.inf64))
	local function saves(name: string, f: number, d: number)
		local v: any = T.FixDescriptor.new()
		v.F, v.D = f, d
		local ok, err = pcall(F.save, T.FixDescriptor, fixed, { v })
		local e: any = err
		row(name, if ok then "written" else tostring(e.name) .. " " .. tostring(e.field))
	end
	saves("fixed/write/f", NAN32, 2.5)
	saves("fixed/write/d", 1.5, SNAN64)
end

-- THE COOK: the open refuses, the cook refuses
do
	C.BuildVersion = BUILD
	for _, w in { "cookA", "cookYs", "cookTwin" } do
		local v, why = CR.open("Var", inputs[w], BUILD)
		row("cook/read/" .. w, if v == nil then tostring(why) else "opened")
	end
	local v = twin()
	v.A = NAN32
	local c, why = C.Cook(Var, v, 1)
	local m, mwhy = C.CookMeasure(Var, v)
	row("cook/write", tostring(c == nil) .. " " .. tostring(why) .. " " .. tostring(m) .. " " .. tostring(mwhy))
	local ok = C.Cook(Var, twin(), 1)
	row("cook/write/twin", if ok == nil then "refused" else "cooked")
end

-- THE BLOCK ACCESSOR (ruling 2): it latches the refusal and answers +0
do
	local info: any = { name = "Probe", buildVersion = 7i, size = 64, align = 64, fields = {
		{ name = "f", offset = 32, size = 4, kind = 10 },
		{ name = "d", offset = 40, size = 8, kind = 11 },
	} }
	local function block(f: buffer, d: buffer): buffer
		local b = buffer.create(64)
		buffer.writeinteger(b, 0, 0x4B4C42414D484353i)
		buffer.writeinteger(b, 8, 7i)
		buffer.writeinteger(b, 16, 1i)
		buffer.copy(b, 32, f, 0, 4)
		buffer.copy(b, 40, d, 0, 8)
		return b
	end
	for _, c in { { "twin", inputs.twinA, inputs.twinB }, { "f", inputs.nan32, inputs.twinB }, { "d", inputs.twinA, inputs.inf64 } } do
		local blk = B.BlockOpen(info, block(c[2], c[3]), 0, 64)
		assert(blk ~= nil, "the probe block did not open")
		local p = B.Read(info, blk)
		row("block/" .. c[1], string.format("%s %s %s", tostring(p.f), tostring(p.d), tostring(B.NonFiniteFloat(blk))))
	end
end

-- THE JSON WRITER: a non-finite float has no JSON spelling (§16.3)
for _, w in writes do
	local v = twin()
	w[2](v)
	local r: any = report()
	local out = J.ToJson("Var", v, r)
	row("json/write/" .. w[1], if out == nil then tostring(r.reason) else "written")
end
`

// finiteControl is one rule's reversed witness: edits to one emitted runtime
// file, each matched exactly once, and the rows that must go red.
type finiteControl struct {
	name  string
	file  string
	edits [][2]string
	red   []string
}

var finiteControls = []finiteControl{
	{"variable-read", "schema_load.luau",
		[][2]string{{"if _nonFinite == nil then\n\t\t_nonFinite = _ownerName", "if false then\n\t\t_nonFinite = _ownerName"}},
		[]string{"var/read/varA", "var/read/varB", "var/read/varXs", "var/read/varYs",
			"var/strict/varA", "var/strict/varB", "var/strict/varXs", "var/strict/varYs"}},
	{"variable-write", "schema_save.luau",
		[][2]string{
			{"if bit32.band(bits, 0x7F800000) == 0x7F800000 then return refuse_non_finite(c, e) end", "-- SABOTAGED"},
			{"if val ~= val or val == math.huge or val == -math.huge then return refuse_non_finite(c, e) end", "-- SABOTAGED"},
		},
		[]string{"var/write/a", "var/write/b", "var/write/xs", "var/write/ys", "var/write/narrows-to-inf"}},
	{"retained-unknown", "schema_retain.luau",
		[][2]string{{"if _nonFinite == nil then _nonFinite = retainedFloatName(owner, id) end", "-- SABOTAGED"}},
		[]string{"retain/read"}},
	{"message-read", "schema_message.luau",
		[][2]string{{"\tif nonFinite ~= nil then\n\t\t-- the refusal outranks", "\tif false then\n\t\t-- the refusal outranks"}},
		[]string{"msg/read/msgA", "msg/read/msgYs"}},
	{"message-write", "schema_message.luau",
		[][2]string{
			{"\tif nonFinite ~= nil then\n\t\trefuseNonFinite(report, nonFinite, false)\n\t\treturn -1\n", "\tif false then\n\t\treturn -1\n"},
			{"\tif nonFinite ~= nil then\n\t\trefuseNonFinite(report, nonFinite, false)\n\t\treturn nil, -1\n", "\tif false then\n\t\treturn nil, -1\n"},
		},
		[]string{"msg/write"}},
	{"fixed-read", "schema_fixed.luau",
		[][2]string{
			{"local bits = buffer.readu32(buf, at)\n\t\t\tif bit32.band(bits, 0x7F800000) == 0x7F800000 then error(nonFinite(f), 0) end", "local bits = buffer.readu32(buf, at)"},
			{"if bit32.band(buffer.readu32(buf, at + 4), 0x7FF00000) == 0x7FF00000 then error(nonFinite(f), 0) end", "-- SABOTAGED"},
		},
		[]string{"fixed/read/f", "fixed/read/d"}},
	{"fixed-write", "schema_fixed.luau",
		[][2]string{
			{"local bits = doubleToF32(v)\n\t\t\tif bit32.band(bits, 0x7F800000) == 0x7F800000 then error(nonFinite(f), 0) end", "local bits = doubleToF32(v)"},
			{"if v ~= v or v == math.huge or v == -math.huge then error(nonFinite(f), 0) end", "-- SABOTAGED"},
		},
		[]string{"fixed/write/f", "fixed/write/d"}},
	{"cook-read", "schema_cookread.luau",
		[][2]string{{"if not f_finite(ctx.b, at, fd.f32 == true) then", "if false then"}},
		[]string{"cook/read/cookA", "cook/read/cookYs"}},
	{"cook-write", "schema_cook.luau",
		[][2]string{{"if not finite(f, e.f32 == true) then", "if false then"}},
		[]string{"cook/write"}},
	{"block-accessor", "schema_blockread.luau",
		[][2]string{
			{"if bit32.band(buffer.readu32(buf, at), 0x7F800000) == 0x7F800000 then return nonFinite(f) end", "-- SABOTAGED"},
			{"if bit32.band(buffer.readu32(buf, at + 4), 0x7FF00000) == 0x7FF00000 then return nonFinite(f) end", "-- SABOTAGED"},
		},
		[]string{"block/f", "block/d"}},
	{"json-write", "schema_json.luau",
		[][2]string{{`if x ~= x or x == math.huge or x == -math.huge then refuse_reason = "non_finite_float"; return false end`, "-- SABOTAGED"}},
		[]string{"json/write/a", "json/write/b", "json/write/xs", "json/write/ys", "json/write/narrows-to-inf"}},
}

// finiteFixture is the probe's unit, its inputs and every row's expected
// answer, computed from schema-go's engine.
type finiteFixture struct {
	files  map[string][]byte
	inputs map[string][]byte
	want   map[string]string
	probe  string
}

func finiteUnit(t *testing.T, src string) (*ir.Unit, *tabletext.Model) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "FF.schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("the probe unit does not load: %v", err)
	}
	return u, tabletext.NewModel(u)
}

// varInstance is the Var twin in the Go engine's model, with set applied.
func varInstance(t *testing.T, m *tabletext.Model, set func(*tabletext.Instance)) *tabletext.Instance {
	t.Helper()
	return twinInstance(t, m, "Var", set)
}

// twinInstance is the twin of a table with Var's fields (Var, or the fixed Msg
// the message form carries) in the Go engine's model, with set applied.
func twinInstance(t *testing.T, m *tabletext.Model, root string, set func(*tabletext.Instance)) *tabletext.Instance {
	t.Helper()
	inst := m.New(m.Lookup(root))
	for i := range inst.Fields {
		f := &inst.Fields[i]
		switch f.Def.Name {
		case "a":
			f.Cell.F = float64(math.Float32frombits(twinA))
		case "b":
			f.Cell.F = math.Float64frombits(twinB)
		case "xs":
			f.Count = 1
			f.Elems[0].F = float64(math.Float32frombits(twinXs))
		case "ys":
			f.Count = 1
			f.Elems[0].F = math.Float64frombits(twinYs)
		case "z":
			f.Cell.F = math.Float64frombits(twinZ)
		}
	}
	if set != nil {
		set(inst)
	}
	return inst
}

func setVar(name string, v float64) func(*tabletext.Instance) {
	return func(inst *tabletext.Instance) {
		for i := range inst.Fields {
			f := &inst.Fields[i]
			if f.Def.Name == name {
				if len(f.Elems) > 0 {
					f.Elems[0].F = v
				} else {
					f.Cell.F = v
				}
			}
		}
	}
}

func mustPatch(t *testing.T, b, old, new []byte) []byte {
	t.Helper()
	if n := bytes.Count(b, old); n != 1 {
		t.Fatalf("the twin's pattern %x occurs %d times in %x, want once", old, n, b)
	}
	return bytes.Replace(b, old, new, 1)
}

// patchBits replaces the one occurrence of old's width bits with new's in a
// bit stream written low bit first (the message form's bitpacker).
func patchBits(t *testing.T, b []byte, old, new uint64, width int) []byte {
	t.Helper()
	bit := func(i int) uint64 { return uint64(b[i/8]>>(i%8)) & 1 }
	at := -1
	for i := 0; i+width <= len(b)*8; i++ {
		match := true
		for k := 0; k < width && match; k++ {
			match = bit(i+k) == (old>>k)&1
		}
		if match {
			if at >= 0 {
				t.Fatalf("the twin's %d-bit pattern %x occurs twice in the batch", width, old)
			}
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("the twin's %d-bit pattern %x is absent from the batch", width, old)
	}
	out := slices.Clone(b)
	for k := range width {
		i := at + k
		out[i/8] = out[i/8]&^(1<<(i%8)) | byte((new>>k)&1)<<(i%8)
	}
	return out
}

func newFiniteFixture(t *testing.T) *finiteFixture {
	t.Helper()
	u, m := finiteUnit(t, finiteSchema)
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	fx := &finiteFixture{files: files, inputs: map[string][]byte{}, want: map[string]string{}}
	in := fx.inputs
	in["nan32"], in["ninf32"], in["inf64"], in["snan64"] = le32(nan32), le32(ninf32), le64(inf64), le64(snan64)
	in["twinA"], in["twinB"] = le32(twinA), le64(twinB)

	// THE VARIABLE FORM, against tablewire
	twin, err := tablewire.Encode(m, varInstance(t, m, nil))
	if err != nil {
		t.Fatalf("the Go writer refused the twin: %v", err)
	}
	in["varTwin"] = twin
	reads := []struct {
		name string
		old  []byte
		new  []byte
	}{
		{"varA", le32(twinA), le32(nan32)},
		{"varB", le64(twinB), le64(inf64)},
		{"varXs", le32(twinXs), le32(ninf32)},
		{"varYs", le64(twinYs), le64(snan64)},
	}
	for _, r := range reads {
		wire := mustPatch(t, twin, r.old, r.new)
		in[r.name] = wire
		var rep tabletext.Report
		_, derr := tablewire.Decode(m, m.New(m.Lookup("Var")), wire, &rep)
		var rf *tablewire.NonFiniteRefusal
		if !errors.As(derr, &rf) {
			t.Fatalf("%s: the Go reader answered %v, want a NonFiniteRefusal", r.name, derr)
		}
		fx.want["var/read/"+r.name] = rep.Reason
		fx.want["var/strict/"+r.name] = "non_finite_float " + rf.Field
	}
	fx.want["var/read/twin"] = "loaded"
	writes := []struct {
		name  string
		field string
		v     float64
	}{
		{"a", "a", float64(math.Float32frombits(nan32))},
		{"b", "b", math.Float64frombits(inf64)},
		{"xs", "xs", float64(math.Float32frombits(ninf32))},
		{"ys", "ys", math.Float64frombits(snan64)},
	}
	for _, w := range writes {
		_, eerr := tablewire.Encode(m, varInstance(t, m, setVar(w.field, w.v)))
		var nf *tablewire.NonFiniteRefusal
		if !errors.As(eerr, &nf) {
			t.Fatalf("var write %s: the Go writer answered %v, want a NonFiniteRefusal", w.name, eerr)
		}
		fx.want["var/write/"+w.name] = "non_finite_float " + nf.Field + " non_finite_float"
		fx.want["json/write/"+w.name] = "non_finite_float"
	}
	// a finite double past FLT_MAX narrows to an infinity in a float32 field;
	// the packet rule (SPEC.md §4.3, "after narrowing") refuses it, and so does
	// every Luau table writer: the bits it would write are an infinity
	fx.want["var/write/narrows-to-inf"] = "non_finite_float Var.a non_finite_float"
	fx.want["json/write/narrows-to-inf"] = "non_finite_float"
	fx.want["var/write/twin"] = hex.EncodeToString(twin)

	// A RETAINED UNKNOWN FLOAT: the newer writer's z, against tablewire
	_, mNew := finiteUnit(t, finiteNewer)
	newTwin, err := tablewire.Encode(mNew, varInstance(t, mNew, nil))
	if err != nil {
		t.Fatalf("the Go writer refused the newer twin: %v", err)
	}
	in["retainTwin"] = newTwin
	in["retain"] = mustPatch(t, newTwin, le64(twinZ), le64(snan64))
	var rep tabletext.Report
	if ok, derr := tablewire.Decode(m, m.New(m.Lookup("Var")), in["retain"], &rep); derr != nil || !ok {
		t.Fatalf("the Go plain read refused an unknown non-finite float it skips: %v", derr)
	}
	fx.want["retain/plain"] = "loaded"
	fx.want["retain/twin"] = "loaded"
	// retain/read is held by pattern below: the id is the field-name hash

	// THE MESSAGE FORM, against tablewire
	in["ann"] = tablewire.Announce(u)
	batch, err := tablewire.EncodeMessages(m, []*tabletext.Instance{twinInstance(t, m, "Msg", nil)})
	if err != nil {
		t.Fatalf("the Go message writer refused the twin: %v", err)
	}
	in["msgTwin"] = batch
	in["msgA"] = patchBits(t, batch, twinA, nan32, 32)
	in["msgYs"] = patchBits(t, batch, twinYs, snan64, 64)
	var voc tablewire.Vocabulary
	var arep tabletext.Report
	if err := voc.AnnounceRead(in["ann"], &arep); err != nil {
		t.Fatalf("the Go vocabulary refused the announcement: %v", err)
	}
	for _, w := range []string{"msgA", "msgYs"} {
		var mrep tabletext.Report
		_, _, derr := tablewire.DecodeMessages(m, []*tabletext.Instance{m.New(m.Lookup("Msg"))}, in[w], &voc, &mrep)
		if _, ok := errors.AsType[*tablewire.NonFiniteRefusal](derr); !ok {
			t.Fatalf("%s: the Go batch reader answered %v, want a NonFiniteRefusal", w, derr)
		}
		fx.want["msg/read/"+w] = mrep.Reason
	}
	fx.want["msg/twin"] = "same"
	fx.want["msg/write"] = "non_finite_float Msg.a non_finite_float"

	// THE FIXED FORM: the Luau writer's own twin is the file the reads patch
	fx.want["fixed/twin"] = "1.5 2.5"
	fx.want["fixed/read/f"] = "non_finite_float Fix.f"
	fx.want["fixed/read/d"] = "non_finite_float Fix.d"
	fx.want["fixed/write/f"] = "non_finite_float Fix.f"
	fx.want["fixed/write/d"] = "non_finite_float Fix.d"

	// THE COOK, against tablecook
	cooked, err := tablecook.Cook(m, varInstance(t, m, nil), tablecook.Options{})
	if err != nil {
		t.Fatalf("the Go cook refused the twin: %v", err)
	}
	in["cookTwin"] = cooked
	in["cookA"] = mustPatch(t, cooked, le32(twinA), le32(nan32))
	in["cookYs"] = mustPatch(t, cooked, le64(twinYs), le64(snan64))
	for _, w := range []string{"cookA", "cookYs"} {
		if _, err := tablecook.Uncook(m, m.Lookup("Var"), in[w]); err == nil {
			t.Fatalf("%s: the Go uncook accepted a non-finite float", w)
		}
	}
	if _, err := tablecook.Cook(m, varInstance(t, m, setVar("a", math.NaN())), tablecook.Options{}); err == nil {
		t.Fatal("the Go cook accepted a NaN")
	}
	fx.want["cook/read/cookA"] = "non_finite_float: Var.a"
	fx.want["cook/read/cookYs"] = "non_finite_float: Var.ys"
	fx.want["cook/read/cookTwin"] = "opened"
	fx.want["cook/write"] = "true non_finite_float: Var.a -1 non_finite_float: Var.a"
	fx.want["cook/write/twin"] = "cooked"

	// THE BLOCK ACCESSOR: +0 and the latched name
	fx.want["block/twin"] = "1.5 2.5 nil"
	fx.want["block/f"] = "0 2.5 non_finite_float: Probe.f"
	fx.want["block/d"] = "1.5 0 non_finite_float: Probe.d"

	var mods []string
	for name := range files {
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	var probe strings.Builder
	fmt.Fprintf(&probe, "local BUILD = %s\n", luau.IntegerLit(new(big.Int).SetUint64(ir.BuildVersion(u))))
	for _, mod := range mods {
		fmt.Fprintf(&probe, "require(\"./%s\")\n", mod)
	}
	probe.WriteString(finiteProbe)
	fx.probe = probe.String()
	return fx
}

var retainReadAnswer = regexp.MustCompile(`^non_finite_float Var\.0x[0-9a-f]{16}$`)

// run writes the unit with files' overrides and answers every row.
func (fx *finiteFixture) run(t *testing.T, override map[string][]byte) map[string]string {
	t.Helper()
	dir := t.TempDir()
	files := maps.Clone(fx.files)
	maps.Copy(files, override)
	files["main.luau"] = []byte(fx.probe)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := luaurun.Run(t, dir, "main.luau", fx.inputs)
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		name, answer, ok := strings.Cut(line, " ")
		if !ok {
			t.Fatalf("the probe printed %q, not a row and its answer", line)
		}
		got[name] = answer
	}
	return got
}

// red is the set of rows whose answer is not the gate's.
func (fx *finiteFixture) red(got map[string]string) []string {
	var out []string
	for name, want := range fx.want {
		if got[name] != want {
			out = append(out, name)
		}
	}
	if !retainReadAnswer.MatchString(got["retain/read"]) {
		out = append(out, "retain/read")
	}
	sort.Strings(out)
	return out
}

func TestLuauTablesAreFiniteOnly(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	fx := newFiniteFixture(t)
	got := fx.run(t, nil)
	if len(got) != len(fx.want)+1 {
		t.Errorf("the probe answered %d rows, want %d", len(got), len(fx.want)+1)
	}
	for _, name := range fx.red(got) {
		want := fx.want[name]
		if name == "retain/read" {
			want = retainReadAnswer.String()
		}
		t.Errorf("%s: the Luau leg answered %q, want %q", name, got[name], want)
	}
	t.Logf("%d finite-float rows held", len(got))
}

func TestLuauFiniteFloatNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	fx := newFiniteFixture(t)
	covered := map[string]bool{}
	for _, c := range finiteControls {
		t.Run(c.name, func(t *testing.T) {
			src := string(fx.files[c.file])
			for _, e := range c.edits {
				if n := strings.Count(src, e[0]); n != 1 {
					t.Fatalf("the %s anchor matches %d times in %s, want once: %q", c.name, n, c.file, e[0])
				}
				src = strings.Replace(src, e[0], e[1], 1)
			}
			red := fx.red(fx.run(t, map[string][]byte{c.file: []byte(src)}))
			want := slices.Sorted(slices.Values(c.red))
			if !slices.Equal(red, want) {
				t.Errorf("NEGATIVE CONTROL FAILED: removing the %s check reds %v, want exactly %v", c.name, red, want)
			}
		})
		for _, r := range c.red {
			covered[r] = true
		}
	}
	// every refusal row is some control's: a rule with no witness is a gap
	for name, want := range fx.want {
		if strings.Contains(want, "non_finite_float") && !covered[name] {
			t.Errorf("the refusal row %s has no negative control", name)
		}
	}
}
