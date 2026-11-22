package luatable

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// countedBirthSchema holds one counted array with a positive minimum per
// element kind (a scalar, a declared type whose field carries a default, a
// 128-bit integer and an enum), on a fixed root (the message form) and on a
// variable root (the variable form); and the wide fixed shapes of F-167 on a
// fixed root: a `[2]uint128`, an enum-keyed `[Tone]uint128` and a 128-bit
// fixed-point `[2]fixed(112, 16)` with a range.
const countedBirthSchema = `package birth

type Rec
{
    n int32 = 55
    on bool
}

enum Tone
{
    Low
    Mid
    High
}

fixed table FixI32 { few [2..4]int32 }
fixed table FixRec { few [2..4]Rec }
fixed table FixU128 { few [2..4]uint128 }
fixed table FixTone { few [2..4]Tone }
table VarI32 { few [2..4]int32 }
table VarRec { few [2..4]Rec }
table VarU128 { few [2..4]uint128 }
table VarTone { few [2..4]Tone }

fixed table FixWide { cells [2]uint128 }
fixed table FixKeyed { cells [Tone]uint128 }
fixed table FixPoint { cells [2]fixed(112, 16) | min = -1000000, max = 1000000 }
`

// countedBirthCase is one value: the engine places it from its JSON text
// (fresh when empty) and the probe from Lua statements on a fresh value v.
// A case with no Var root is the message form alone.
type countedBirthCase struct {
	name, fix, vari, text, place string
}

func countedBirthCases() []countedBirthCase {
	return []countedBirthCase{
		{"fresh", "FixI32", "VarI32", "", ""},
		{"min_default", "FixI32", "VarI32", `{"few":[0,0]}`, "v.few = { 0, 0 }"},
		{"min_one", "FixI32", "VarI32", `{"few":[0,7]}`, "v.few[2] = 7"},
		{"min_plus1_default", "FixI32", "VarI32", `{"few":[0,0,0]}`, "v.few[3] = 0"},
		{"fresh", "FixRec", "VarRec", "", ""},
		{"min_default", "FixRec", "VarRec", `{"few":[{},{}]}`, "v.few = { T.NewRec(), T.NewRec() }"},
		{"min_one", "FixRec", "VarRec", `{"few":[{},{"n":56}]}`, "v.few[2].n = 56"},
		{"min_plus1_default", "FixRec", "VarRec", `{"few":[{},{},{}]}`, "v.few[3] = T.NewRec()"},
		{"fresh", "FixU128", "VarU128", "", ""},
		{"min_default", "FixU128", "VarU128", `{"few":["0","0"]}`, "v.few = { { lo = 0, hi = 0 }, { lo = 0, hi = 0 } }"},
		{"min_one", "FixU128", "VarU128", `{"few":["0","18446744073709551616"]}`, "v.few[2] = { lo = 0, hi = 1 }"},
		{"min_plus1_default", "FixU128", "VarU128", `{"few":["0","0","0"]}`, "v.few[3] = { lo = 0, hi = 0 }"},
		{"fresh", "FixTone", "VarTone", "", ""},
		{"min_none", "FixTone", "VarTone", `{"few":["None","None"]}`, "v.few = { false, false }"},
		{"min_one", "FixTone", "VarTone", `{"few":["None","High"]}`, `v.few[2] = "High"`},
		{"fresh", "FixWide", "", "", ""},
		{"first_low", "FixWide", "", `{"cells":["1","0"]}`, "v.cells[1] = { lo = 1, hi = 0 }"},
		{"second_high", "FixWide", "", `{"cells":["0","18446744073709551616"]}`, "v.cells[2] = { lo = 0, hi = 1 }"},
		{"fresh", "FixKeyed", "", "", ""},
		{"low_set", "FixKeyed", "", `{"cells":{"Low":"1"}}`, "v.cells[1] = { lo = 1, hi = 0 }"},
		{"fresh", "FixPoint", "", "", ""},
	}
}

// countedBirthReference is the engine's bytes for one case: the variable
// file of its variable root (nil when it has none) and the message batch of
// its fixed root holding that value.
func countedBirthReference(t *testing.T, model *tabletext.Model, c countedBirthCase) (variable, message []byte) {
	t.Helper()
	place := func(root string) *tabletext.Instance {
		inst := model.New(model.Lookup(root))
		if c.text != "" {
			var rep tabletext.Report
			if !model.Read(inst, []byte(c.text), &rep) {
				t.Fatalf("%s %s: the engine refused %s: %+v", root, c.name, c.text, rep)
			}
		}
		return inst
	}
	var err error
	if c.vari != "" {
		if variable, err = tablewire.EncodeVariable(model, place(c.vari)); err != nil {
			t.Fatal(err)
		}
	}
	if message, err = tablewire.EncodeMessages(model, []*tabletext.Instance{place(c.fix)}); err != nil {
		t.Fatal(err)
	}
	return variable, message
}

// TestLuaCountedArrayBornAtItsMinimum holds the counted-array birth value
// (docs/SPEC-TABLES.md §22.2, ir.Field.BornCount; F-115) and the wide fixed
// element's default (F-167): a fresh `[2..4]T` holds two default elements,
// and every Lua writer — the variable save, the retaining save, the save
// after a text read, and the message save — elides the field at exactly that
// value, as the engine does (`020000` for the one-field fixed root, the
// 10-byte variable file), and stores it at count 2 with one live element and
// at count 3 of defaults; a fixed `[2]uint128`, a keyed `[Tone]uint128` and a
// `[2]fixed(112, 16)` at zero elide on the message form where Lua wrote 35,
// 36 and 12 bytes, and ride with one live half.
func TestLuaCountedArrayBornAtItsMinimum(t *testing.T) {
	t.Parallel()
	work := t.TempDir()
	path := filepath.Join(work, "birth.schema")
	if err := os.WriteFile(path, []byte(countedBirthSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, path)
	model := tabletext.NewModel(u)
	var probe strings.Builder
	probe.WriteString(`local T = require("BirthTables")
local S = require("save")
local R = require("retain")
local J = require("json")
local L = require("load")
local voc = {}
assert(T.AnnounceRead(voc, T.Announcement, {}), "own AnnounceRead")

local function hex(s)
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end
local function wide_zero(e) return type(e) == "table" and e.lo == 0 and e.hi == 0 end
for _, r in ipairs({ "FixI32", "VarI32" }) do
  local born = T["New" .. r]().few
  assert(#born == 2 and born[1] == 0 and born[2] == 0, r .. " born")
end
for _, r in ipairs({ "FixU128", "VarU128" }) do
  local born = T["New" .. r]().few
  assert(#born == 2 and wide_zero(born[1]) and wide_zero(born[2]), r .. " born")
end
for _, r in ipairs({ "FixTone", "VarTone" }) do
  local born = T["New" .. r]().few
  assert(#born == 2 and born[1] == false and born[2] == false, r .. " born")
end
for _, r in ipairs({ "FixRec", "VarRec" }) do
  local born = T["New" .. r]().few
  assert(#born == 2 and born[1].n == 55 and born[2].n == 55 and born[1] ~= born[2], r .. " born")
end

local function emit(name, fix, fv, vari, vv)
  local out = {}
  if vari ~= "" then
    local wire = assert(S.Save(T, vari, vv))
    out[#out + 1] = hex(wire)
    local store = { cap = 262144, id_cap = 8192 }
    local again = assert(R.LoadRetain(T, vari, wire, store, {}))
    out[#out + 1] = hex(assert(R.SaveRetain(T, vari, again, store, {})))
    local back = assert(J.FromJson(T, vari, assert(J.ToJson(T, vari, vv)), {}))
    out[#out + 1] = hex(assert(S.Save(T, vari, back)))
  end
  out[#out + 1] = hex(assert(T.SaveMessages(fix, voc, { fv })))
  local fixed = assert(J.FromJson(T, fix, assert(J.ToJson(T, fix, fv)), {}))
  out[#out + 1] = hex(assert(T.SaveMessages(fix, voc, { fixed })))
  print(name .. " " .. fix .. " " .. table.concat(out, " "))
end
local v, var
`)
	cases := countedBirthCases()
	for _, c := range cases {
		if c.vari != "" {
			fmt.Fprintf(&probe, "v = T.New%s()\n%s\nvar = v\n", c.vari, c.place)
		} else {
			probe.WriteString("var = nil\n")
		}
		fmt.Fprintf(&probe, "v = T.New%s()\n%s\nemit(%q, %q, v, %q, var)\n", c.fix, c.place, c.name, c.fix, c.vari)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luarun.Run(t, dir, "main.lua", nil))
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("%d lines for %d cases:\n%s", len(lines), len(cases), out)
	}
	for i, c := range cases {
		variable, message := countedBirthReference(t, model, c)
		if c.fix == "FixI32" && (c.name == "fresh" || c.name == "min_default") {
			// the construct: the engine elides the birth value whole
			if hex.EncodeToString(message) != "020000" || len(variable) != 10 {
				t.Fatalf("construct: the engine writes %x / %d bytes for the birth value", message, len(variable))
			}
		}
		want := []string{c.name, c.fix}
		if c.vari != "" {
			wv := hex.EncodeToString(variable)
			want = append(want, wv, wv, wv)
		}
		wm := hex.EncodeToString(message)
		want = append(want, wm, wm)
		if lines[i] != strings.Join(want, " ") {
			t.Errorf("%s %s:\n got  %s\n want %s (variable save, retaining save, save after text, message save, message save after text)", c.fix, c.name, lines[i], strings.Join(want, " "))
		}
	}
}
