package luatable

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// The reader declares [2..4]; the writer under another build declares [..4],
// so it can lawfully send the count 1 the reader's Min excludes.
const (
	luaCountMinReader = "package probe\n\ntable P\n{\n    items [2..4]int32\n}\n\nfixed table M\n{\n    items [2..4]int32\n}\n"
	luaCountMinWriter = "package probe\n\ntable P\n{\n    items [..4]int32\n}\n\nfixed table M\n{\n    items [..4]int32\n}\n"
)

const luaCountMinProbe = `local T = require(MODULE)
local S = require("save")
local L = require("load")
local J = require("json")
local TF = require("textform")
local inputs = require("inputs")
local function out(label, v) print(label .. "=" .. v) end
local function silent(r)
  return not r.damaged and (r.refused or 0) == 0 and (r.outOfRange or 0) == 0
end
local function count(v) return v == nil and "nil" or tostring(#v.items) end

out("born_var", tostring(#T.NewP().items))
out("born_fixed", tostring(#T.NewM().items))

local function var_load(label, wire)
  local r = {}
  local v = L.Load(T, "P", wire, r)
  out(label, (v ~= nil and silent(r)) and ("read:" .. count(v)) or "refused")
end
var_load("var_read_1", inputs.var1)
var_load("var_read_2", inputs.var2)

local function json_load(label, text)
  local r = {}
  local v = J.FromJson(T, "P", text, r)
  out(label, (v ~= nil and silent(r)) and ("read:" .. count(v)) or "refused")
end
json_load("json_read_1", '{"items":[7]}')
json_load("json_read_2", '{"items":[7,8]}')

local function text_load(label, text)
  local ok, v = pcall(TF.Load, T, "P", text)
  out(label, (ok and v ~= nil) and ("read:" .. count(v)) or "refused")
end
text_load("text_read_1", "(:items (7))")
text_load("text_read_2", "(:items (7 8))")

local function var_save(label, items)
  local v = T.NewP()
  v.items = items
  local ok, b = pcall(S.Save, T, "P", v)
  out(label, (ok and b ~= nil) and "written" or "refused")
end
var_save("var_write_1", { 7 })
var_save("var_write_2", { 7, 8 })

local function fixed_save(label, items)
  local v = T.NewM()
  v.items = items
  local ok, b = pcall(T.FixedSave, "M", { v })
  out(label, (ok and b ~= nil) and "written" or "refused")
end
fixed_save("fixed_write_1", { 7 })
fixed_save("fixed_write_2", { 7, 8 })

-- THE FIXED READ: a lawful image of count 2, and the same image with its
-- count word forged to 1 (the dead slot past it zeroed).
do
  local v = T.NewM()
  v.items = { 7, 8 }
  local image = T.FixedSave("M", { v })
  local two = string.pack("<i4i4i4", 2, 7, 8)
  local at = image:find(two, 1, true)
  assert(at ~= nil, "count word not found")
  local forged = image:sub(1, at - 1) .. string.pack("<i4i4i4", 1, 7, 0) .. image:sub(at + #two)
  for label, b in pairs({ fixed_read_2 = image, fixed_read_1 = forged }) do
    local ok, rows = pcall(T.FixedLoad, "M", b)
    out(label, (ok and rows ~= nil) and ("read:" .. count(rows[1])) or "refused")
  end
end

local vocab = {}
assert(T.AnnounceRead(vocab, inputs.announce, {}), "announce")
local function msg_load(label, batch)
  local r = {}
  local back = T.LoadMessages("M", vocab, batch, r)
  local read = back ~= nil and #back == 1 and not r.damaged and (r.refused or 0) == 0
  out(label, read and ("read:" .. count(back[1])) or "refused")
end
msg_load("msg_read_1", inputs.msg1)
msg_load("msg_read_2", inputs.msg2)
`

// TestLuaCountBelowMinRefused pins SPEC §5.1 row 3 ("a counted array's count
// past its Max or below its Min") and §4.2 (a fresh value is born at its
// Min) on the Lua table runtime (review 2026-11-21, cross-leg
// counted-array-min): the variable, JSON, text, fixed and message reads each refuse
// a count below the reader's Min, as the Go engine refuses count_over_bound;
// the variable and fixed writes refuse one; a new value holds Min elements.
// The lawful twin of every case reads or writes.
func TestLuaCountBelowMinRefused(t *testing.T) {
	t.Parallel()
	rdir, wdir := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(rdir, "probe.schema"), luaCountMinReader)
	writeFile(t, filepath.Join(wdir, "probe.schema"), luaCountMinWriter)
	reader := luarun.LoadUnit(t, rdir)
	writer := luarun.LoadUnit(t, wdir)
	files, err := Generate(reader)
	if err != nil {
		t.Fatalf("generate: %v", err)
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
	probe := strings.Replace(luaCountMinProbe, "MODULE", `"`+ModuleName(reader)+`"`, 1)
	out, stderr, err := try(t, files, probe, map[string][]byte{
		"announce": tablewire.Announce(writer),
		"var1":     varWire(`{"items":[7]}`),
		"var2":     varWire(`{"items":[7,8]}`),
		"msg1":     msgBatch(`{"items":[7]}`),
		"msg2":     msgBatch(`{"items":[7,8]}`),
	})
	if err != nil {
		t.Fatalf("probe: %v\nout=%s\nstderr=%s", err, out, stderr)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		k, v, _ := strings.Cut(line, "=")
		got[k] = v
	}
	want := []struct{ row, verdict string }{
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
		{"msg_read_2", "read:2"},
	}
	for _, w := range want {
		if got[w.row] != w.verdict {
			t.Errorf("%s: got %q, want %q", w.row, got[w.row], w.verdict)
		}
	}
}
