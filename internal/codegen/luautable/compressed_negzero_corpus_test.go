package luautable_test

import (
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
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE COMPRESSED -0 WRITER CORPUS ON THE LUAU LEG (docs/SPEC-TABLES.md §3.3,
// docs/rulings.json fixed-compressed-negzero): a writer stores +0 for a
// compressed float's -0. The corpus is the Go reference engine's
// (testdata/conformance/feature-witnesses/compressed-negzero-go, pinned by
// internal/tablewire's TestCompressedNegZeroGoCorpus); this leg runs every row
// through the generated Luau unit: load <base>.json with the Luau text read,
// set <path> to -0 in the native value, save <form>, and owe
// <expect>.<form>.bin byte for byte.
//
// Beside each row, every other Luau writer the row's root has is held to the
// row's +0 twin (the same base saved untouched): the variable save, the
// retaining save (a variable root's), the fixed save and the message form (a
// fixed table's), the JSON writer and the cook writer. A compressed row's -0
// output equals its twin's; the plain `p` rows are the control, whose -0 rides
// bit-exact, so their output differs from the twin's.
//
// It runs the pinned interpreter, so it is a slow test; the conformance lane
// runs it (make/luau-compressed-negzero.mk). TestLuauCompressedFloatStoresAreGuarded
// is its fast twin.

const luauNegZeroCorpusDir = "../../../testdata/conformance/feature-witnesses/compressed-negzero-go"

// luauNegZeroNoWriter names the corpus rows whose <form> writer the Luau
// surface does not have, and why. The row's root still answers every twin
// its writers owe.
var luauNegZeroNoWriter = map[string]string{
	"var_fixed_table_leaf": "the Luau Save of a fixed table writes the fixed form (schema_save.luau S.save calls F.save): the leg has no variable-form writer for a fixed root",
}

type luauNegZeroRow struct {
	name, form, root, base, path, expect string
}

func luauNegZeroRows(t *testing.T) []luauNegZeroRow {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(luauNegZeroCorpusDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []luauNegZeroRow
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("cases.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		rows = append(rows, luauNegZeroRow{f[0], f[1], f[2], f[3], f[4], f[5]})
	}
	return rows
}

const luauNegZeroProbe = `
local T = require("./ProbeTable")
local St = require("./schema_tables")
local S = require("./schema_save")
local F = require("./schema_fixed")
local M = require("./schema_message")
local C = require("./schema_cook")
local R = require("./schema_retain")
local J = require("./schema_json")
local inputs = require("./inputs")

local NZ = buffer.readf32(inputs.negzero, 0)
assert(NZ == 0 and 1 / NZ < 0, "the probe's -0 is not -0")
C.BuildVersion = buffer.readinteger(inputs.build, 0)

local function hex(b: buffer?): string
	if b == nil then return "nil" end
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do parts[i + 1] = string.format("%02x", buffer.readu8(b, i)) end
	return table.concat(parts)
end

local function report(): any
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0, retained = 0, retainLost = 0 }
end

-- set writes -0 into the cell a corpus path names: field, field[index],
-- field[Variant] for an enum-keyed slot, or field.arm for a union's set arm
local function set(desc: any, v: any, path: string)
	local name, rest = string.match(path, "^([%w_]+)(.*)$")
	local fd: any = nil
	for _, f in desc.fields do
		if f.name == name then fd = f end
	end
	assert(fd ~= nil, "no field " .. tostring(name))
	if rest == "" then
		v[fd.slot] = NZ
		return
	end
	local arm = string.match(rest, "^%.([%w_]+)$")
	if arm ~= nil then
		local u = St.lookup(fd.t)
		for _, a in u.arms do
			if a.name == arm then
				assert(v[fd.slot].Type == a.tag, "the base does not set arm " .. arm)
				v[fd.slot][a.slot] = NZ
				return
			end
		end
		error("no arm " .. arm)
	end
	local index = string.match(rest, "^%[([%w_]+)%]$")
	local slot = tonumber(index)
	if slot ~= nil then
		slot = slot + 1
	else
		for i, kv in fd.keyvs do
			if kv[1] == index then slot = i end
		end
	end
	assert(slot ~= nil, "no slot " .. tostring(index))
	v[fd.slot][slot] = NZ
end

local function load(root: string, base: string): any
	local r = report()
	local v = J.FromJson(root, inputs[base .. ".json"], r)
	assert(v ~= nil, base .. ".json: the Luau text read refused it: " .. tostring(r.reason))
	return v
end

-- each writer the root has, over a native value
local function writers(root: string, base: string): { { any } }
	local desc = St.lookup(root)
	local out: { { any } } = {
		{ "json", function(v: any) return J.ToJson(root, v, {}) end },
		{ "cook", function(v: any) return (C.Cook(desc, v, 1)) end },
	}
	if desc.fixed then
		-- S.save of a fixed table writes the fixed form (it is F.save), so
		-- it is the "fixed" writer's twin, not a variable writer
		out[#out + 1] = { "save", function(v: any) return (S.save(desc, v)) end }
		local fixed = { hash = T[root .. "FixedHash"], recordBytes = T[root .. "FixedRecordBytes"], layout = T[root .. "FixedLayout"] }
		out[#out + 1] = { "fixed", function(v: any) return F.save(desc, fixed, { v }) end }
		out[#out + 1] = { "message", function(v: any)
			local voc = M.NewVocabulary()
			assert(M.AnnounceRead(voc, T.TableMessageAnnouncement, {}))
			return (M.SaveMessages(voc, root, { v }, {}))
		end }
	else
		out[#out + 1] = { "variable", function(v: any) return (S.save(desc, v)) end }
		out[#out + 1] = { "retain", function(v: any)
			-- the retaining save over the store its load kept (§6.6)
			local r = report()
			local _, store = R.LoadRetain(root, inputs[base .. ".variable.bin"], r, 65536)
			assert(store ~= nil, base .. ": the retaining load refused")
			return (R.SaveRetain(root, v, store, r))
		end }
	end
	return out
end

for _, row in ROWS do
	local name, form, root, base, path = row[1], row[2], row[3], row[4], row[5]
	local desc = St.lookup(root)
	for _, w in writers(root, base) do
		local plus = load(root, base)
		local minus = load(root, base)
		set(desc, minus, path)
		local okp, bp = pcall(w[2], plus)
		local okm, bm = pcall(w[2], minus)
		print(name .. " " .. w[1] .. " " .. (if okm then hex(bm) else "error") .. " " .. (if okp then hex(bp) else "error"))
	end
end
`

// TestLuauCompressedNegZeroCorpus runs the corpus on the Luau leg.
func TestLuauCompressedNegZeroCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	paths, err := compiler.GatherPaths([]string{filepath.Join(luauNegZeroCorpusDir, "Probe.schema")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)

	rows := luauNegZeroRows(t)
	inputs := map[string][]byte{"negzero": {0x00, 0x00, 0x00, 0x80}}
	build := make([]byte, 8)
	binary.LittleEndian.PutUint64(build, ir.BuildVersion(u))
	inputs["build"] = build
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(luauNegZeroCorpusDir, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	var lua strings.Builder
	lua.WriteString("--!nocheck\nlocal ROWS = {\n")
	for _, r := range rows {
		inputs[r.base+".json"] = read(r.base + ".json")
		if r.root == "Var" {
			inputs[r.base+".variable.bin"] = read(r.base + ".variable.bin")
		}
		fmt.Fprintf(&lua, "\t{ %q, %q, %q, %q, %q },\n", r.name, r.form, r.root, r.base, r.path)
	}
	lua.WriteString("}\n")
	lua.WriteString(luauNegZeroProbe)
	files["main.luau"] = []byte(lua.String())

	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := luaurun.Run(t, dir, "main.luau", inputs)

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
		if why, ok := luauNegZeroNoWriter[r.name]; ok {
			// held by name, never in silence: the row's writer must still be
			// absent, and its root's other writers still answer its twins
			if _, has := ws[r.form]; has {
				t.Errorf("%s: the Luau leg now has a %s writer for %s; drop the exemption (%s)", r.name, r.form, r.root, why)
			}
			t.Logf("%s: not carried on the Luau leg: %s", r.name, why)
			exempt++
		} else if p, ok := ws[r.form]; !ok {
			t.Errorf("%s: the Luau leg has no %s writer for %s", r.name, r.form, r.root)
		} else if p.minus != want {
			t.Errorf("%s: the Luau %s save of -0 at %s is not %s.%s.bin:\n have %s\n want %s", r.name, r.form, r.path, r.expect, r.form, p.minus, want)
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
	if held+exempt != len(rows) || exempt != len(luauNegZeroNoWriter) {
		t.Errorf("%d rows held and %d exempt of %d; %d exemptions named", held, exempt, len(rows), len(luauNegZeroNoWriter))
	}
	t.Logf("%d of %d corpus rows held (%d not carried, by name); %d writer twins held", held, len(rows), exempt, twins)
}
