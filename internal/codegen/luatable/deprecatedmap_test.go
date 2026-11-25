package luatable

// A MAP VALUE NAMING A DEPRECATED VARIANT IS STILL AN ENTRY (docs/SPEC-TABLES.md
// §2.8, §2.15, §16.2): it reads as the value's declared default, None, which a
// Lua map holds as false (a key cannot hold nil), never as a vanished key. The
// wire is written by the same schema with the variant LIVE, so the bytes are
// an older writer's. The Lua wire read and the Lua JSON read keep all three
// entries; the Lua save and JSON write of what they read are byte-equal to
// the Go oracle's (internal/tablewire, internal/tabletext) for the same
// input. A None value the read never saw deprecated (a JSON "None") takes the
// same path, and the cooked form's read (uncook) keeps the entry the same way. This is the unit form of the conformance rows sp-enum-mood-map
// and sp-enum-elem-deprecated-map (test/tables/moodmap).

import (
	"bytes"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const luaDeprecatedMapLiveSchema = `package probe
enum Mood {
    Calm
    Old
    Fierce
}
table Root {
    by map[string(8)]Mood
}
`

const luaDeprecatedMapSchema = `package probe
enum Mood {
    Calm
    Old | deprecated
    Fierce
}
table Root {
    by map[string(8)]Mood
}
`

const luaDeprecatedMapLiveProbe = `local T = require("ProbeTables")
local save = require("save")
local J = require("json")
local cookwrite = require("cookwrite")
local hexline = require("hexline")
local v = T.NewRoot()
v.by = { a = "Calm", b = "Old", c = "Fierce" }
hexline(assert(save.Save(T, "Root", v)))
hexline(assert(J.ToJson(T, "Root", v)))
hexline(assert(cookwrite.Cook(T, "Root", v, nil, 1)))
`

const luaDeprecatedMapProbe = `local T = require("ProbeTables")
local load = require("load")
local save = require("save")
local J = require("json")
local cook = require("cook")
local cookwrite = require("cookwrite")
local uncook = require("uncook")
local hexline = require("hexline")
local inputs = require("inputs")
local function fresh_report()
  return { damaged = false, refused = 0, unknown = 0, kindChanged = 0,
           outOfRange = 0, widened = 0, repeatedKey = 0 }
end
local function check(what, v, r)
  assert(v ~= nil and not r.damaged, what .. " refused")
  assert(r.unknown == 0, what .. " counted unknown")
  assert(v.by.a == "Calm" and v.by.c == "Fierce", what .. ": a live entry moved")
  assert(v.by.b == false, what .. ": the deprecated entry is " .. tostring(v.by.b) .. ", want false (None)")
end
-- the wire, an older writer's bytes
local r = fresh_report()
local wv = load.Load(T, "Root", inputs.wire, r)
check("wire read", wv, r)
hexline(assert(save.Save(T, "Root", wv)))
hexline(assert(J.ToJson(T, "Root", wv)))
-- the older writer's JSON text (b is "Old")
r = fresh_report()
local jv = J.FromJson(T, "Root", inputs.json, r)
check("json read", jv, r)
hexline(assert(save.Save(T, "Root", jv)))
-- a None the text spells
r = fresh_report()
local nv = J.FromJson(T, "Root", '{"by":{"a":"Calm","b":"None","c":"Fierce"}}', r)
check("json None read", nv, r)
hexline(assert(save.Save(T, "Root", nv)))
hexline(assert(J.ToJson(T, "Root", nv)))
-- THE COOKED FORM: the older writer's cook, and the cook of the None entry
local function opened(what, bytes)
  local h, why = cook.Open(T, "Root", bytes)
  assert(h ~= nil, what .. " open refused: " .. tostring(why))
  local cv, cwhy = uncook.Read(T, h)
  assert(cv ~= nil, what .. " read refused: " .. tostring(cwhy))
  check(what, cv, fresh_report())
end
opened("cook read", inputs.cook)
opened("None cook read", assert(cookwrite.Cook(T, "Root", nv, nil, 1)))
print("cook-ok")
`

func TestLuaDeprecatedMapValueReadsNone(t *testing.T) {
	t.Parallel()

	live := luaVariantUnit(t, luaDeprecatedMapLiveSchema)
	lines := luarun.HexLines(t, luaVariantRun(t, live, luaDeprecatedMapLiveProbe, map[string][]byte{}))
	if len(lines) != 3 {
		t.Fatalf("the live probe printed %d byte strings, want 3", len(lines))
	}
	wire, text, cooked := lines[0], lines[1], lines[2]
	if !bytes.Contains(text, []byte(`"Old"`)) {
		t.Fatalf("the live text does not carry the variant:\n%s", text)
	}

	// THE ORACLE: the deprecated schema reads the older writer's wire, keeps
	// the entry at None, and writes these bytes and this text.
	deprecated := luaVariantUnit(t, luaDeprecatedMapSchema)
	m := tabletext.NewModel(deprecated)
	inst := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if ok, err := tablewire.Decode(m, inst, wire, &report); !ok || err != nil || report.Unknown != 0 {
		t.Fatalf("oracle wire read: %v %v %+v", ok, err, report)
	}
	wantWire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("oracle wire write: %v", err)
	}
	wantText, err := m.Write(inst)
	if err != nil {
		t.Fatalf("oracle json write: %v", err)
	}
	// the document ends in one newline, as the pinned corpus text does
	// (testdata/conformance/tables/json/sp_enum_mood_map.json); Model.Write
	// answers the text without it
	wantText = append(wantText, '\n')
	if !bytes.Contains(wantText, []byte(`"b": "None"`)) {
		t.Fatalf("the oracle did not keep the entry at None:\n%s", wantText)
	}

	raw := luaVariantRun(t, deprecated, luaDeprecatedMapProbe, map[string][]byte{
		"wire": wire, "json": text, "cook": cooked,
	})
	if !bytes.HasSuffix(raw, []byte("cook-ok\n")) {
		t.Fatalf("the deprecated probe did not end with cook-ok:\n%s", raw)
	}
	out := luarun.HexLines(t, bytes.TrimSuffix(raw, []byte("cook-ok\n")))
	names := []string{"wire read, wire write", "wire read, json write", "json read, wire write",
		"json None read, wire write", "json None read, json write"}
	wants := [][]byte{wantWire, wantText, wantWire, wantWire, wantText}
	if len(out) != len(wants) {
		t.Fatalf("the deprecated probe printed %d byte strings, want %d", len(out), len(wants))
	}
	for i, want := range wants {
		if !bytes.Equal(out[i], want) {
			t.Errorf("%s: the Lua leg wrote\n%q\nthe oracle wrote\n%q", names[i], out[i], want)
		}
	}
}
