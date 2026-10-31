package luatable

// A DEPRECATED ENUM VARIANT READS AS THE FIELD DEFAULT AND IS REFUSED ON WRITE
// (docs/SPEC-TABLES.md §2.15) on the Lua leg, matching the Go reference
// (internal/codegen/gotable/variantdeprecated_test.go): on the id-table wire,
// form 3, the message form and JSON. The wire is written by the SAME schema
// with the variant LIVE, so the bytes are those of an older writer; the same
// bytes read with the variant live yield it (the reversed witness), proving
// the test sees the rule. The engine (internal/tablewire, internal/tabletext)
// reads the Lua-written bytes the same way, so the leg agrees with the oracle.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const luaVariantDeprecatedLiveSchema = `package probe
enum Ship {
    Fighter
    Bomber
    Scout
}
fixed table Root {
    ship  Ship = Scout
    fleet [..2]Ship
}
`

const luaVariantDeprecatedSchema = `package probe
enum Ship {
    Fighter
    Bomber | deprecated
    Scout
}
fixed table Root {
    ship  Ship = Scout
    fleet [..2]Ship
}
`

// luaVariantProbeHead is shared by both probes: the module, the runtimes, a
// fresh report, and a check that no counter moved.
const luaVariantProbeHead = `local T = require("ProbeTables")
local load = require("load")
local save = require("save")
local J = require("json")
local hexline = require("hexline")
local inputs = require("inputs")
local function fresh_report()
  return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0,
           clamped = 0, widened = 0, duplicate = 0 }
end
local function clean(r, what)
  assert(not r.malformed and r.unknown == 0 and r.kindMismatch == 0 and r.clamped == 0,
    what .. " counted an event: unknown=" .. tostring(r.unknown) .. " malformed=" .. tostring(r.malformed))
end
local function announced()
  local vocab = {}
  for i = 1, T.MessageEntries do vocab[i] = {} end
  local r = fresh_report()
  assert(T.AnnounceRead(vocab, inputs.announce or T.Announcement, r), "announcement refused")
  return vocab
end
`

const luaVariantLiveProbe = luaVariantProbeHead + `
local v = T.NewRoot()
v.ship = "Bomber"
v.fleet = { "Bomber" }
-- the id-table wire (form 1), form 3, the message form and JSON
local wire = assert(save.Save(T, "Root", v), "live save refused")
local fixed = T.WriteFixed("Root", v)
local vocab = announced()
local message = assert(T.SaveMessages("Root", vocab, { v }), "live message save refused")
local text = assert(J.ToJson(T, "Root", v), "live json refused")
hexline(wire); hexline(fixed); hexline(message); hexline(T.Announcement); hexline(text)

-- THE REVERSED WITNESS: the same bytes read with Bomber LIVE yield Bomber.
local r = fresh_report()
local w = load.LoadTolerant(T, "Root", wire, r)
clean(r, "live wire")
assert(w.ship == "Bomber" and #w.fleet == 1 and w.fleet[1] == "Bomber", "live wire witness")
local fv = T.ReadFixed("Root", fixed)
assert(fv.ship == "Bomber" and fv.fleet[1] == "Bomber", "live fixed witness")
r = fresh_report()
local decoded = assert(T.LoadMessages("Root", announced(), message, r), "live message refused")
clean(r, "live message")
assert(decoded[1].ship == "Bomber" and decoded[1].fleet[1] == "Bomber", "live message witness")
r = fresh_report()
local jv = assert(J.FromJson(T, "Root", text, r), "live json read refused")
clean(r, "live json")
assert(jv.ship == "Bomber" and jv.fleet[1] == "Bomber", "live json witness")
`

const luaVariantDeprecatedProbe = luaVariantProbeHead + `
local cookwrite = require("cookwrite")
local Text = require("text")
-- THE ID-TABLE WIRE: the field reads its declared default (Scout), the array
-- element reads the ELEMENT default (None, false in a sequence), and nothing
-- is unknown.
local r = fresh_report()
local v = assert(load.LoadTolerant(T, "Root", inputs.wire, r), "deprecated wire refused")
clean(r, "deprecated wire")
assert(v.ship == "Scout", "deprecated wire field: " .. tostring(v.ship))
assert(#v.fleet == 1 and v.fleet[1] == false, "deprecated wire element: " .. tostring(v.fleet[1]))
-- FORM 3
local fv = T.ReadFixed("Root", inputs.fixed)
assert(fv.ship == "Scout", "deprecated fixed field: " .. tostring(fv.ship))
assert(not fv.fleet[1], "deprecated fixed element: " .. tostring(fv.fleet[1]))
-- THE MESSAGE FORM
r = fresh_report()
local decoded = assert(T.LoadMessages("Root", announced(), inputs.message, r), "deprecated message refused")
clean(r, "deprecated message")
assert(decoded[1].ship == "Scout", "deprecated message field: " .. tostring(decoded[1].ship))
assert(not decoded[1].fleet[1], "deprecated message element: " .. tostring(decoded[1].fleet[1]))
-- JSON
r = fresh_report()
local jv = assert(J.FromJson(T, "Root", inputs.json, r), "deprecated json refused")
clean(r, "deprecated json")
assert(jv.ship == "Scout", "deprecated json field: " .. tostring(jv.ship))
assert(jv.fleet[1] == false, "deprecated json element: " .. tostring(jv.fleet[1]))
print("reads-ok")

-- EVERY WRITER REFUSES A VALUE HOLDING THE DEPRECATED VARIANT, invalid_value
-- naming Root.ship (Root.fleet for the element).
local function refuses(what, want, ok, a, b)
  local reason
  if ok then
    assert(a == nil or a == -1, what .. " accepted a deprecated variant")
    reason = b
  else
    reason = a
  end
  assert(type(reason) == "string" and reason:find("invalid_value", 1, true) and reason:find(want, 1, true),
    what .. ": want invalid_value naming " .. want .. ", got " .. tostring(reason))
end
for _, case in ipairs({ { field = "ship", want = "Root.ship" }, { field = "fleet", want = "Root.fleet" } }) do
  local bad = T.NewRoot()
  if case.field == "ship" then bad.ship = "Bomber" else bad.fleet = { "Bomber" } end
  refuses("Save", case.want, pcall(save.Save, T, "Root", bad))
  refuses("Measure", case.want, pcall(save.Measure, T, "Root", bad))
  refuses("SaveText", case.want, pcall(Text.SaveText, T, "Root", bad))
  refuses("WriteFixed", case.want, pcall(T.WriteFixed, "Root", bad))
  refuses("MeasureMessages", case.want, pcall(T.MeasureMessages, "Root", announced(), { bad }))
  refuses("SaveMessages", case.want, pcall(T.SaveMessages, "Root", announced(), { bad }))
  refuses("ToJson", case.want, pcall(J.ToJson, T, "Root", bad))
  refuses("CookMeasure", case.want, pcall(cookwrite.CookMeasure, T, "Root", bad))
  refuses("Cook", case.want, pcall(cookwrite.Cook, T, "Root", bad))
end
print("writes-refused-ok")
`

// luaVariantCost is what the slow half pays for this test (internal/slowtest).
const luaVariantCost = "the pinned Lua interpreter (dist/lua-5.4.8, make dist-lua) over two generated units"

func luaVariantUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), "probe.schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return luarun.LoadUnit(t, path)
}

func luaVariantRun(t *testing.T, u *ir.Unit, probe string, inputs map[string][]byte) []byte {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	luarun.WriteHexProbe(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	return luarun.Run(t, dir, "main.lua", inputs)
}

func TestDeprecatedVariantReadsDefaultWriteRefused(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, luaVariantCost)

	live := luaVariantUnit(t, luaVariantDeprecatedLiveSchema)
	lines := luarun.HexLines(t, luaVariantRun(t, live, luaVariantLiveProbe, map[string][]byte{}))
	if len(lines) != 5 {
		t.Fatalf("the live probe printed %d byte strings, want 5", len(lines))
	}
	wire, fixed, message, announce, text := lines[0], lines[1], lines[2], lines[3], lines[4]

	deprecated := luaVariantUnit(t, luaVariantDeprecatedSchema)
	out := string(luaVariantRun(t, deprecated, luaVariantDeprecatedProbe, map[string][]byte{
		"wire": wire, "fixed": fixed, "message": message, "announce": announce, "json": text,
	}))
	for _, want := range []string{"reads-ok", "writes-refused-ok"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the deprecated probe did not print %s:\n%s", want, out)
		}
	}

	// THE ORACLE AGREES: the engine reads the Lua leg's older-writer bytes as
	// the field default, with no unknown counted, on the wire, JSON and the
	// message form — the same answers the Lua reads gave above.
	m := tabletext.NewModel(deprecated)
	scout := uint64(tabletext.EnumValue(deprecated.Enums["Ship"], "Scout"))
	inst := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if ok, err := tablewire.Decode(m, inst, wire, &report); !ok || err != nil {
		t.Fatalf("oracle wire: %v %+v", err, report)
	}
	if report.Unknown != 0 || report.Malformed || inst.Fields[0].Cell.U != scout || inst.Fields[1].Elems[0].U != 0 {
		t.Fatalf("oracle wire: %+v %+v", report, inst.Fields)
	}
	inst = m.New(m.Lookup("Root"))
	report = tabletext.Report{}
	if !m.Read(inst, text, &report) || report.Unknown != 0 || inst.Fields[0].Cell.U != scout || inst.Fields[1].Elems[0].U != 0 {
		t.Fatalf("oracle json: %+v %+v", report, inst.Fields)
	}
	var vocab tablewire.Vocabulary
	report = tabletext.Report{}
	if err := vocab.AnnounceRead(announce, &report); err != nil {
		t.Fatalf("oracle announcement: %v", err)
	}
	inst = m.New(m.Lookup("Root"))
	report = tabletext.Report{}
	if n, ok, err := tablewire.DecodeMessages(m, []*tabletext.Instance{inst}, message, &vocab, &report); err != nil || !ok || n != 1 ||
		inst.Fields[0].Cell.U != scout || inst.Fields[1].Elems[0].U != 0 {
		t.Fatalf("oracle message: %d %v %v %+v", n, ok, err, inst.Fields)
	}
}
