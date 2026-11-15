package mootable_test

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const writerContractSchema = `package probe

fixed table FixedRoot | forms = "wire,json"
{
	Range int32 = 0 | min = -5, max = 5
	Bits bits(3) = 0
	Compressed float32 = 0.0 | min = 0.0, max = 10.0, resolution = 0.01
	Small uint8 = 0
	Unsigned uint64 = 0
	Counted [..4]int32
	Text string(8)
	Bytes bytes(4)
}

table VariableRoot | forms = "wire,json"
{
	Range int32 = 0 | min = -5, max = 5
	Bits bits(3) = 0
	Compressed float32 = 0.0 | min = 0.0, max = 10.0, resolution = 0.01
	Small uint8 = 0
	Unsigned uint64 = 0
	Counted [..4]int32
	Text string(8)
	Bytes bytes(4)
}
`

type writerMutation struct {
	name, fixed, variable string
}

var writerMutations = []writerMutation{
	{"range-above", "v.Range = 6", "v.Range = 6"},
	{"range-below", "v.Range = -6", "v.Range = -6"},
	{"ranged-fraction", "v.Range = 1.5", "v.Range = 1.5"},
	{"bits-too-wide", "v.Bits = 8", "v.Bits = 8"},
	{"compressed-above", fmt.Sprintf("v.Compressed = 0x%08X", math.Float32bits(11)), fmt.Sprintf("v.Compressed = 0x%08X", math.Float32bits(11))},
	{"compressed-below", fmt.Sprintf("v.Compressed = 0x%08X", math.Float32bits(-1)), fmt.Sprintf("v.Compressed = 0x%08X", math.Float32bits(-1))},
	{"uint8-negative", "v.Small = -1", "v.Small = -1"},
	{"uint8-above", "v.Small = 256", "v.Small = 256"},
	{"uint64-negative-storage", "v.Unsigned = serialize.Int64.fromNumber(-1)", "v.Unsigned = serialize.Int64.fromNumber(-1)"},
	{"count-above", "v.CountedCount = 5", "v.CountedCount = 5"},
	{"count-negative", "v.CountedCount = -1", "v.CountedCount = -1"},
	{"count-fraction", "v.CountedCount = 1.5", "v.CountedCount = 1.5"},
	{"string-too-long", `v.Text = "123456789"`, `v.Text = "123456789"`},
	{"bytes-over-bound", "v.BytesLength = 5", "v.BytesLength = 5"},
	{"bytes-over-storage", "v.Bytes = buffer.create(1)\nv.BytesLength = 2", "v.Bytes = buffer.create(1)\nv.BytesLength = 2"},
}

const writerContractMain = `--!strict
local T = require("./ProbeTable")
local St = require("./schema_tables")
local F = require("./table_fixed")
local S = require("./table_save")
local MS = require("./table_message")
local serialize = require("./serialize")
local fixedDesc = St.lookup("FixedRoot")
local variableDesc = St.lookup("VariableRoot")
local fixedSpec = F.lookup("FixedRoot") :: any

local function fixedResult(v: any): string
	local ok, out = pcall(F.save, fixedDesc, fixedSpec, { v })
	if not ok then return "raised" end
	return if out == nil then "refused" else "written"
end

local function variableResult(v: any): string
	local ok, out = pcall(S.save, variableDesc, v)
	if not ok then return "raised" end
	return if out == nil then "refused" else "written"
end

local function messageResult(v: any): string
	local ok, out = pcall(MS.SaveMessages, "FixedRoot", { v }, {})
	if not ok then return "raised" end
	return if out == nil then "refused" else "written"
end

local function jsonResult(value: any, measure: (any) -> number, write: (any, buffer) -> number): string
	local ok, size = pcall(measure, value)
	if not ok then return "raised" end
	if size >= 0 then return "written" end
	local out = buffer.create(64)
	buffer.fill(out, 0, 0xA5)
	local wrote, n = pcall(write, value, out)
	if not wrote then return "raised" end
	if n < 0 then
		for i = 0, buffer.len(out) - 1 do
			if buffer.readu8(out, i) ~= 0xA5 then return "wrote-buffer" end
		end
		return "refused"
	end
	return "wrote-buffer"
end

local function check(name: string, fixedSet: (any) -> (), variableSet: (any) -> ())
	local fv = T.NewFixedRoot()
	local vv = T.NewVariableRoot()
	fixedSet(fv)
	variableSet(vv)
	print(name .. "\t" .. fixedResult(fv) .. "\t" .. variableResult(vv) .. "\t" .. messageResult(fv) ..
		"\t" .. jsonResult(fv, T.FixedRootToJsonMeasure, T.FixedRootToJson) ..
		"\t" .. jsonResult(vv, T.VariableRootToJsonMeasure, T.VariableRootToJson))
end

`

func writerContractProgram(g *generatedUnit, mutations []writerMutation) string {
	var b strings.Builder
	b.WriteString(strings.Replace(writerContractMain, "local fixedDesc", g.requireAll()+"local fixedDesc", 1))
	for _, m := range mutations {
		fmt.Fprintf(&b, "check(%q, function(v: any) %s end, function(v: any) %s end)\n", m.name, m.fixed, m.variable)
	}
	return b.String()
}

func TestMooWritersRefuseOffContract(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo (it runs the emitted table writers)")
	g := generateFiles(t, map[string]string{"Probe.schema": writerContractSchema})
	got := strings.Split(g.run(t, writerContractProgram(g, writerMutations), nil), "\n")
	if len(got) != len(writerMutations) {
		t.Fatalf("the probe returned %d rows for %d mutations:\n%s", len(got), len(writerMutations), strings.Join(got, "\n"))
	}
	boundaryProgram := strings.Replace(writerContractMain, "local fixedDesc", g.requireAll()+"local L = require(\"./table_load\")\nlocal fixedDesc", 1) + `
local vocabulary = MS.NewVocabulary()
if not MS.AnnounceRead(vocabulary, T.TableMessageAnnouncement, {}) then error("announcement refused") end
for _, value in { -5, 5 } do
	local fv = T.NewFixedRoot()
	local vv = T.NewVariableRoot()
	fv.Range, vv.Range = value, value
	local high = value == 5
	for _, obj in {fv, vv} do
		obj.Bits = if high then 7 else 0
		obj.Compressed = if high then 0x41200000 else 0
		obj.Small = if high then 255 else 0
		obj.CountedCount = if high then 4 else 0
		obj.Text = if high then "12345678" else ""
		obj.BytesLength = if high then 4 else 0
	end
	local function matches(obj: any): boolean
		return obj.Range == value and obj.Bits == (if high then 7 else 0) and obj.Compressed == (if high then 0x41200000 else 0) and obj.Small == (if high then 255 else 0) and obj.CountedCount == (if high then 4 else 0) and obj.Text == (if high then "12345678" else "") and obj.BytesLength == (if high then 4 else 0)
	end
	local f = F.save(fixedDesc, fixedSpec, {fv})
	local v = S.save(variableDesc, vv)
	local m = MS.SaveMessages("FixedRoot", {fv}, {})
	if f == nil or v == nil or m == nil then error("boundary refused") end
	local fr = F.load(fixedDesc, fixedSpec, f, {})
	local vr = L.load("VariableRoot", v, L.newReport())
	local count, mr = MS.LoadMessages(vocabulary, "FixedRoot", m, 1, {})
	if fr == nil or not matches(fr[1]) or vr == nil or not matches(vr) or count ~= 1 or not matches(mr[1]) then error("boundary read mismatch") end
	for _, root in {"FixedRoot", "VariableRoot"} do
		local obj = if root == "FixedRoot" then fv else vv
		local size = T[root .. "ToJsonMeasure"](obj)
		if size < 0 then error("json boundary refused") end
		local buf = buffer.create(size)
		if T[root .. "ToJson"](obj, buf) < 0 then error("json boundary write refused") end
		local back = T["New" .. root]()
		if not T[root .. "FromJson"](back, buf, {}) or not matches(back) then error("json boundary read mismatch") end
	end
end
print("bounds read back")
`
	if gotBounds := g.run(t, boundaryProgram, nil); gotBounds != "bounds read back" {
		t.Errorf("boundaries: %s", gotBounds)
	}
	for i, m := range writerMutations {
		want := fmt.Sprintf("%s\trefused\trefused\trefused\trefused\trefused", m.name)
		if got[i] != want {
			t.Errorf("%s: writers returned %q, want %q", m.name, got[i], want)
		}
	}
}

func TestMooWritersRefuseOffContractNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo (it runs the emitted table writers)")
	g := generateFiles(t, map[string]string{"Probe.schema": writerContractSchema})
	const anchor = "\tif fd.lo ~= nil and (V.rawCmp(x, fd.lo, kw, signed) < 0 or V.rawCmp(x, fd.hi, kw, signed) > 0) then return nil end\n"
	path := filepath.Join(g.dir, "table_values.moo")
	src := string(g.files["table_values.moo"])
	if strings.Count(src, anchor) != 1 {
		t.Fatalf("the narrow-kind range check anchor matched %d times, want once", strings.Count(src, anchor))
	}
	if err := os.WriteFile(path, []byte(strings.Replace(src, anchor, "", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	ranged := []writerMutation{writerMutations[0], writerMutations[1]}
	got := strings.Split(g.run(t, writerContractProgram(g, ranged), nil), "\n")
	if len(got) != len(ranged) {
		t.Fatalf("the patched probe returned %d rows for %d mutations:\n%s", len(got), len(ranged), strings.Join(got, "\n"))
	}
	for i, m := range ranged {
		f := strings.Split(got[i], "\t")
		if len(f) != 6 {
			t.Errorf("%s: malformed row: %q", m.name, got[i])
			continue
		}
		for _, verdict := range f[1:] {
			if verdict != "written" {
				t.Errorf("%s: removing the emitted range check did not make every writer accept it: %q", m.name, got[i])
				break
			}
		}
	}
}
