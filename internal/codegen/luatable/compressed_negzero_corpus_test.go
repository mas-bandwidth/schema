package luatable

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE COMPRESSED -0 WRITER CORPUS ON THE LUA LEG (docs/SPEC-TABLES.md §3.3,
// docs/rulings.json fixed-compressed-negzero): a writer stores +0 for a
// compressed float's -0. The corpus is the Go reference engine's
// (testdata/conformance/feature-witnesses/compressed-negzero-go, pinned by
// internal/tablewire's TestCompressedNegZeroGoCorpus); this leg runs every row
// through the generated Lua unit: load <base>.json with the Lua text read,
// set <path> to -0 in the native value, save <form>, and owe
// <expect>.<form>.bin byte for byte.
//
// Beside each row, every other Lua writer the row's root has is held to the
// row's +0 twin (the same base saved untouched): the variable save and the typed
// save (every root's: the Lua Save writes the variable form of a fixed table
// too), the retaining save (a variable root's), the fixed save and the
// message form (a fixed table's), the JSON writer and the cook writer. A
// compressed row's -0 output equals its twin's; the plain `p` rows are the
// control, whose -0 rides bit-exact, so their output differs from the twin's.
//
// It runs the pinned interpreter, so it is a slow test; the conformance lane
// runs it (make/lua-compressed-negzero.mk). TestLuaCompressedFloatStoresAreGuarded
// is its fast twin.

const luaNegZeroCorpusDir = "../../../testdata/conformance/feature-witnesses/compressed-negzero-go"

// luaNegZeroNoWriter names the corpus rows whose <form> writer the Lua
// surface does not have, and why. The row's root still answers every twin
// its writers owe. The Lua Save writes the variable form for a fixed root
// too, so every row is carried and the map is empty.
var luaNegZeroNoWriter = map[string]string{}

type luaNegZeroRow struct {
	name, form, root, base, path, expect string
}

func luaNegZeroRows(t *testing.T) []luaNegZeroRow {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(luaNegZeroCorpusDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []luaNegZeroRow
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("cases.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		rows = append(rows, luaNegZeroRow{f[0], f[1], f[2], f[3], f[4], f[5]})
	}
	return rows
}

const luaNegZeroProbe = `
local T = require("ProbeTables")
local D = require("descriptor")
local S = require("save")
local J = require("json")
local W = require("cookwrite")
local R = require("retain")
local inputs = require("inputs")

-- -0 from its bits, so no constant folding can hand back +0
local NZ = (string.unpack("<f", "\0\0\0\x80"))
assert(NZ == 0 and 1 / NZ < 0, "the probe's -0 is not -0")

local function hex(b)
  if b == nil then return "nil" end
  return (b:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end

local function report()
  return { damaged = false, refused = 0, unknown = 0, kindChanged = 0, outOfRange = 0, widened = 0, repeatedKey = 0, retained = 0, retainLost = 0 }
end

-- set writes -0 into the cell a corpus path names: field, field[index],
-- field[Variant] for an enum-keyed slot, or field.arm for a union's set arm
local function set(root, v, path)
  local name, rest = string.match(path, "^([%w_]+)(.*)$")
  local fd = D.field(T.Tables[root], name)
  assert(fd ~= nil, "no field " .. tostring(name))
  if rest == "" then
    v[name] = NZ
    return
  end
  local arm = string.match(rest, "^%.([%w_]+)$")
  if arm ~= nil then
    assert(v[name] ~= nil and v[name].tag ~= 0 and v[name][arm] ~= nil, "the base does not set arm " .. arm)
    v[name][arm] = NZ
    return
  end
  local index = string.match(rest, "^%[([%w_]+)%]$")
  local slot = tonumber(index)
  if slot ~= nil then
    slot = slot + 1
  else
    for i, kv in ipairs(fd.keyed or {}) do
      if kv[1] == index then slot = i end
    end
  end
  assert(slot ~= nil, "no slot " .. tostring(index))
  assert(v[name][slot] ~= nil, "the base has no element " .. tostring(index))
  v[name][slot] = NZ
end

local function load(root, base)
  local r = report()
  local v = J.FromJson(T, root, inputs[base .. ".json"], r)
  assert(v ~= nil, base .. ".json: the Lua text read refused it: " .. tostring(r.reason))
  return v
end

-- each writer the root has, over a native value
local function writers(root, base)
  local out = {
    { "json", function(v) return J.ToJson(T, root, v) end },
    { "cook", function(v) return (W.Cook(T, root, v, nil, 1)) end },
  }
  -- the variable form and the typed save, for a fixed root as well
  out[#out + 1] = { "variable", function(v) return (S.Save(T, root, v)) end }
  out[#out + 1] = { "typed", function(v) return (S.SaveTyped(T, root, v)) end }
  if T.Tables[root].variable then
    out[#out + 1] = { "retain", function(v)
      -- the retaining save over the store its load kept (§6.6)
      local store = { list = {}, used = 0 }
      local kept = R.LoadRetain(T, root, inputs[base .. ".variable.bin"], store, report())
      assert(kept ~= nil, base .. ": the retaining load refused")
      return (R.SaveRetain(T, root, v, store, report()))
    end }
  else
    out[#out + 1] = { "fixed", function(v) return T.FixedSave(root, { v }) end }
    out[#out + 1] = { "message", function(v)
      local vocab = {}
      assert(T.AnnounceRead(vocab, inputs.announce, {}), "announce")
      return (T.SaveMessages(root, vocab, { v }))
    end }
  end
  return out
end

for _, row in ipairs(ROWS) do
  local name, root, base, path = row[1], row[3], row[4], row[5]
  for _, w in ipairs(writers(root, base)) do
    local plus = load(root, base)
    local minus = load(root, base)
    set(root, minus, path)
    local okp, bp = pcall(w[2], plus)
    local okm, bm = pcall(w[2], minus)
    print(name .. " " .. w[1] .. " " .. (okm and hex(bm) or "error") .. " " .. (okp and hex(bp) or "error"))
  end
end
`

// TestLuaCompressedNegZeroCorpus runs the corpus on the Lua leg.
func TestLuaCompressedNegZeroCorpus(t *testing.T) {
	t.Parallel()
	u := luarun.LoadUnit(t, filepath.Join(luaNegZeroCorpusDir, "Probe.schema"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}

	rows := luaNegZeroRows(t)
	inputs := map[string][]byte{"announce": tablewire.Announce(u)}
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(luaNegZeroCorpusDir, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	var lua strings.Builder
	lua.WriteString("local ROWS = {\n")
	for _, r := range rows {
		inputs[r.base+".json"] = read(r.base + ".json")
		if r.root == "Var" {
			inputs[r.base+".variable.bin"] = read(r.base + ".variable.bin")
		}
		fmt.Fprintf(&lua, "  { %q, %q, %q, %q, %q },\n", r.name, r.form, r.root, r.base, r.path)
	}
	lua.WriteString("}\n")
	lua.WriteString(strings.Replace(luaNegZeroProbe, `require("ProbeTables")`, `require("`+ModuleName(u)+`")`, 1))

	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	writeFile(t, filepath.Join(dir, "main.lua"), lua.String())
	out := luarun.Run(t, dir, "main.lua", inputs)

	type pair struct{ minus, plus string }
	got := map[string]map[string]pair{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) != 4 {
			t.Fatalf("the probe printed %q, not a row, a writer and two outputs", line)
		}
		if got[f[0]] == nil {
			got[f[0]] = map[string]pair{}
		}
		got[f[0]][f[1]] = pair{f[2], f[3]}
	}

	held, twins, exempt := 0, 0, 0
	for _, r := range rows {
		ws := got[r.name]
		if ws == nil {
			t.Errorf("%s: the probe did not answer the row", r.name)
			continue
		}
		plain := r.path == "p"
		// THE CORPUS ROW: the -0 save of <form> is <expect>.<form>.bin
		want := hex.EncodeToString(read(r.expect + "." + r.form + ".bin"))
		if why, ok := luaNegZeroNoWriter[r.name]; ok {
			// held by name, never in silence: the row's writer must still be
			// absent, and its root's other writers still answer its twins
			if _, has := ws[r.form]; has {
				t.Errorf("%s: the Lua leg now has a %s writer for %s; drop the exemption (%s)", r.name, r.form, r.root, why)
			}
			t.Logf("%s: not carried on the Lua leg: %s", r.name, why)
			exempt++
		} else if p, ok := ws[r.form]; !ok {
			t.Errorf("%s: the Lua leg has no %s writer for %s", r.name, r.form, r.root)
		} else if p.minus != want {
			t.Errorf("%s: the Lua %s save of -0 at %s is not %s.%s.bin:\n have %s\n want %s", r.name, r.form, r.path, r.expect, r.form, p.minus, want)
		} else {
			held++
		}
		// THE TWINS: every writer the root has
		for w, p := range ws {
			if p.minus == "error" || p.plus == "error" || p.minus == "nil" || p.plus == "nil" {
				t.Errorf("%s/%s: a writer refused: -0 %s, +0 %s", r.name, w, p.minus, p.plus)
				continue
			}
			switch {
			case plain && p.minus == p.plus:
				t.Errorf("%s/%s: a plain float's -0 did not ride: %s", r.name, w, p.minus)
			case !plain && p.minus != p.plus:
				t.Errorf("%s/%s: a compressed float's -0 was not written as +0:\n -0 %s\n +0 %s", r.name, w, p.minus, p.plus)
			default:
				twins++
			}
		}
	}
	if len(rows) != 15 {
		t.Errorf("cases.tsv has %d rows, want 15", len(rows))
	}
	if held+exempt != len(rows) || exempt != len(luaNegZeroNoWriter) {
		t.Errorf("%d rows held and %d exempt of %d; %d exemptions named", held, exempt, len(rows), len(luaNegZeroNoWriter))
	}
	t.Logf("%d of %d corpus rows held (%d not carried, by name); %d writer twins held", held, len(rows), exempt, twins)
}
