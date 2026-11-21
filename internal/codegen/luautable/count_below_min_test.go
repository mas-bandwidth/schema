package luautable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The reader declares [2..4]; the writer under another build declares [..4],
// so it can lawfully send the count 1 the reader's Min excludes.
const (
	luauCountMinReader = "package probe\n\ntable P\n{\n    items [2..4]int32\n}\n\nfixed table M\n{\n    items [2..4]int32\n}\n"
	luauCountMinWriter = "package probe\n\ntable P\n{\n    items [..4]int32\n}\n\nfixed table M\n{\n    items [..4]int32\n}\n"
)

const luauCountMinProbe = `local inputs = require("./inputs")
local T = require("./ProbeTable")
local St = require("./schema_tables")
local save = require("./schema_save")
local R = require("./schema_refusal")
local M = require("./schema_message")
local F = require("./schema_fixed")
local J = require("./schema_json")
local TF = require("./schema_textform")

local function out(label: string, v: string) print(label .. "=" .. v) end
local function count(v: any): string
	if v == nil then return "nil" end
	if typeof(v.ItemsCount) == "number" then return tostring(v.ItemsCount) end
	local xs = v.Items
	if typeof(xs) == "buffer" then return tostring(buffer.len(xs) // 4) end
	if typeof(xs) == "table" then return tostring(xs.n or #xs) end
	return typeof(xs)
end
local P = St.lookup("P")

out("born_var", count(P.new()))
out("born_fixed", count(T.MDescriptor.new()))

local function var_load(label: string, wire: buffer)
	local r: any = {}
	local ok, v = pcall(R.ReadStrict, nil, "P", wire, r)
	out(label, if ok and v ~= nil then "read:" .. count(v) else "refused")
end
var_load("var_read_1", inputs.var1)
var_load("var_read_2", inputs.var2)

local function json_load(label: string, text: string)
	local r: any = {}
	local ok, v = pcall(J.FromJson, "P", buffer.fromstring(text), r)
	out(label, if ok and v ~= nil then "read:" .. count(v) else "refused")
end
json_load("json_read_1", '{"items":[7]}')
json_load("json_read_2", '{"items":[7,8]}')

local function text_load(label: string, text: string)
	local ok, _, v = pcall(TF.LoadTextForm, "P", text)
	out(label, if ok and v ~= nil then "read:" .. count(v) else "refused")
end
text_load("text_read_1", "(:items (7))")
text_load("text_read_2", "(:items (7 8))")

local function var_save(label: string, items: { number })
	local v: any = P.new()
	v.Items = items
	v.ItemsCount = #items
	local ok, b = pcall(save.save, P, v)
	out(label, if ok and b ~= nil then "written" else "refused")
end
var_save("var_write_1", { 7 })
var_save("var_write_2", { 7, 8 })

local fixed = { hash = T.MFixedHash, recordBytes = T.MFixedRecordBytes, layout = T.MFixedLayout }
local function fixed_save(label: string, items: { number }): buffer?
	local v: any = T.MDescriptor.new()
	v.Items = items
	v.ItemsCount = #items
	local ok, b = pcall(F.save, T.MDescriptor, fixed, { v })
	out(label, if ok and b ~= nil then "written" else "refused")
	return if ok then b else nil
end
fixed_save("fixed_write_1", { 7 })
local image = fixed_save("fixed_write_2", { 7, 8 })

-- THE FIXED READ: a lawful image of count 2, and the same image with its
-- count word forged to 1 (the dead slot past it zeroed).
do
	assert(image ~= nil, "the lawful fixed image")
	local s = buffer.tostring(image)
	local two = string.pack("<i4i4i4", 2, 7, 8)
	local at = string.find(s, two, 1, true)
	assert(at ~= nil, "count word not found")
	local forged = string.sub(s, 1, at - 1) .. string.pack("<i4i4i4", 1, 7, 0) .. string.sub(s, at + #two)
	for _, row in { { "fixed_read_2", s }, { "fixed_read_1", forged } } do
		local ok, rows = pcall(F.load, T.MDescriptor, fixed, buffer.fromstring(row[2]))
		out(row[1], if ok and rows ~= nil then "read:" .. count(rows[1]) else "refused")
	end
end

local voc = M.NewVocabulary()
assert(M.AnnounceRead(voc, inputs.announce, {}), "announce")
local function msg_load(label: string, batch: buffer)
	local r: any = {}
	local ok, n = pcall(M.LoadMessages, voc, "M", batch, 256, r)
	out(label, if ok and n == 1 then "read" else "refused")
end
msg_load("msg_read_1", inputs.msg1)
msg_load("msg_read_2", inputs.msg2)
`

// TestLuauCountBelowMinRefused pins SPEC §5.1 row 3 ("a counted array's
// count past its Max or below its Min") and §4.2 (a fresh value is born at
// its Min) on the Luau table runtime (review 2026-11-21, cross-leg
// counted-array-min): the variable, JSON, text, fixed and message reads each
// refuse a count below the reader's Min, as the Go engine refuses
// count_over_bound; the variable and fixed writes refuse one; a new value
// holds Min elements. The lawful twin of every case reads or writes.
func TestLuauCountBelowMinRefused(t *testing.T) {
	t.Parallel()
	load := func(text string) *ir.Unit {
		p := filepath.Join(t.TempDir(), "Probe.schema")
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		u, err := compiler.New().Load([]string{p})
		if err != nil {
			t.Fatalf("the unit does not load: %v", err)
		}
		return u
	}
	reader, writer := load(luauCountMinReader), load(luauCountMinWriter)
	dir := t.TempDir()
	for _, gen := range []func() (map[string][]byte, error){
		func() (map[string][]byte, error) { return luau.Generate(reader) },
		func() (map[string][]byte, error) { return luautable.Generate(reader) },
	} {
		files, err := gen()
		if err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(luauCountMinProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(writer)
	instance := func(root, text string) *tabletext.Instance {
		inst := model.New(model.Lookup(root))
		var report tabletext.Report
		if !model.Read(inst, []byte(text), &report) || !report.Silent() {
			t.Fatalf("writer value %s %s: %+v", root, text, report)
		}
		return inst
	}
	varWire := func(text string) []byte {
		b, err := tablewire.Encode(model, instance("P", text))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	msgBatch := func(text string) []byte {
		b, err := tablewire.EncodeMessages(model, []*tabletext.Instance{instance("M", text)})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	raw := luaurun.Run(t, dir, "main.luau", map[string][]byte{
		"announce": tablewire.Announce(writer),
		"var1":     varWire(`{"items":[7]}`),
		"var2":     varWire(`{"items":[7,8]}`),
		"msg1":     msgBatch(`{"items":[7]}`),
		"msg2":     msgBatch(`{"items":[7,8]}`),
	})
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		k, v, _ := strings.Cut(line, "=")
		got[k] = v
	}
	for _, w := range []struct{ row, verdict string }{
		{"born_var", "2"},
		{"born_fixed", "2"},
		{"var_read_1", "refused"},
		{"var_read_2", "read:2"},
		{"json_read_1", "refused"},
		{"json_read_2", "read:2"},
		{"text_read_1", "refused"},
		{"text_read_2", "read:2"},
		{"var_write_1", "refused"},
		{"var_write_2", "written"},
		{"fixed_write_1", "refused"},
		{"fixed_write_2", "written"},
		{"fixed_read_1", "refused"},
		{"fixed_read_2", "read:2"},
		{"msg_read_1", "refused"},
		{"msg_read_2", "read"},
	} {
		if got[w.row] != w.verdict {
			t.Errorf("%s: got %q, want %q", w.row, got[w.row], w.verdict)
		}
	}
}
