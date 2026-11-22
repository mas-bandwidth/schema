package mootable_test

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE COMPRESSED -0 WRITER CORPUS ON THE MOO LEG (docs/SPEC-TABLES.md §3.3,
// docs/rulings.json fixed-compressed-negzero): a writer stores +0 for a
// compressed float's -0. The corpus is the Go reference engine's
// (testdata/conformance/feature-witnesses/compressed-negzero-go, pinned by
// internal/tablewire's TestCompressedNegZeroGoCorpus); this leg runs every row
// through the generated moo unit: load <base>.json with the moo text read,
// set <path> to -0 in the native value, save <form>, and owe
// <expect>.<form>.bin byte for byte.
//
// Beside each row, every other moo writer the row's root has is held to the
// row's +0 twin (the same base saved untouched): the variable save, the JSON
// writer and the cook writer of either root, the retaining save of a variable
// root, and the fixed save and the message form of a fixed table. A
// compressed row's -0 output equals its twin's; the plain `p` rows are the
// control, whose -0 rides bit-exact, so their output differs from the twin's.
//
// It runs the pinned interpreter, so it is a slow test; the conformance lane
// runs it (make/moo-compressed-negzero.mk).
// TestMooCompressedFloatStoresAreGuarded is its fast twin.

const mooNegZeroCorpusDir = "../../../testdata/conformance/feature-witnesses/compressed-negzero-go"

// mooNegZeroNoWriter names the corpus rows whose <form> writer the moo
// surface does not have, and why. The row's root still answers every twin
// its writers owe. The moo leg carries every row.
var mooNegZeroNoWriter = map[string]string{}

type mooNegZeroRow struct {
	name, form, root, base, path, expect string
}

func mooNegZeroRows(t *testing.T) []mooNegZeroRow {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(mooNegZeroCorpusDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []mooNegZeroRow
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("cases.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		rows = append(rows, mooNegZeroRow{f[0], f[1], f[2], f[3], f[4], f[5]})
	}
	return rows
}

const mooNegZeroProbe = `
local St = require("./schema_tables")
local L = require("./table_load")
local S = require("./table_save")
local F = require("./table_fixed")
local MS = require("./table_message")
local W = require("./table_cookwrite")
local R = require("./table_retain")
local J = require("./json")
local inputs = require("./inputs")

-- a float32 is held as its binary32 pattern (docs/PORT-MOO.md): -0 is the
-- sign bit alone, the pattern every corpus cell is float32 at
local NZ = buffer.readu32(inputs.negzero, 0)
assert(NZ == 0x80000000, "the probe's -0 is not the binary32 -0")

local function hex(b: buffer?): string
	if b == nil then return "nil" end
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do parts[i + 1] = string.format("%02x", buffer.readu8(b, i)) end
	return table.concat(parts)
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

local function load(root: string, stem: string): any
	local d = St.lookup(root)
	local v = d.new()
	local r = L.newReport()
	assert(J.fromJson(d, v, inputs[stem .. ".json"], r), stem .. ".json: the moo text read refused it: " .. tostring(r.reason))
	return v
end

-- each writer the root has, over a native value
local function writers(root: string, stem: string): { { any } }
	local desc = St.lookup(root)
	local out: { { any } } = {
		{ "json", function(v: any) local s = J.toJson(desc, v) return if s == nil then nil else buffer.fromstring(s) end },
		{ "cook", function(v: any) return (W.cook(root, v, false)) end },
		{ "variable", function(v: any) return (S.save(desc, v)) end },
	}
	if desc.fixed then
		out[#out + 1] = { "fixed", function(v: any) return (F.save(desc, T[root .. "Fixed"], { v })) end }
		out[#out + 1] = { "message", function(v: any) return (MS.SaveMessages(root, { v }, {})) end }
	else
		out[#out + 1] = { "retain", function(v: any)
			-- the retaining save over the store its load kept (§6.6)
			local r = L.newReport()
			local store = R.newStore(65536, 1024)
			assert(R.LoadRetain(root, inputs[stem .. ".variable.bin"], r, store) ~= nil, stem .. ": the retaining load refused")
			return (R.SaveRetain(root, v, store, r))
		end }
	end
	return out
end

for _, row in ROWS do
	local name, root, stem, path = row[1], row[3], row[4], row[5]
	local desc = St.lookup(root)
	for _, w in writers(root, stem) do
		local plus = load(root, stem)
		local minus = load(root, stem)
		set(desc, minus, path)
		local okp, bp = pcall(w[2], plus)
		local okm, bm = pcall(w[2], minus)
		print(name .. " " .. w[1] .. " " .. (if okm then hex(bm) else "error") .. " " .. (if okp then hex(bp) else "error"))
	end
end
`

// TestMooCompressedNegZeroCorpus runs the corpus on the moo leg.
func TestMooCompressedNegZeroCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo (it runs the emitted table runtime)")
	paths, err := compiler.GatherPaths([]string{filepath.Join(mooNegZeroCorpusDir, "Probe.schema")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	g := writeUnit(t, u, files)
	mods := g.tableModules()
	if len(mods) != 1 {
		t.Fatalf("the probe unit emits %d table modules, want 1: %v", len(mods), mods)
	}

	rows := mooNegZeroRows(t)
	inputs := map[string][]byte{"negzero": {0x00, 0x00, 0x00, 0x80}}
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(mooNegZeroCorpusDir, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	var main strings.Builder
	main.WriteString("--!nocheck\n")
	main.WriteString(g.requireAll())
	fmt.Fprintf(&main, "local T = require(\"./%s\")\nlocal ROWS = {\n", mods[0])
	for _, r := range rows {
		inputs[r.base+".json"] = read(r.base + ".json")
		if r.root == "Var" {
			inputs[r.base+".variable.bin"] = read(r.base + ".variable.bin")
		}
		fmt.Fprintf(&main, "\t{ %q, %q, %q, %q, %q },\n", r.name, r.form, r.root, r.base, r.path)
	}
	main.WriteString("}\n")
	main.WriteString(mooNegZeroProbe)
	out := g.run(t, main.String(), inputs)

	type pair struct{ minus, plus string }
	got := map[string]map[string]pair{}
	for line := range strings.SplitSeq(out, "\n") {
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
		if why, ok := mooNegZeroNoWriter[r.name]; ok {
			if _, has := ws[r.form]; has {
				t.Errorf("%s: the moo leg now has a %s writer for %s; drop the exemption (%s)", r.name, r.form, r.root, why)
			}
			t.Logf("%s: not carried on the moo leg: %s", r.name, why)
			exempt++
		} else if p, ok := ws[r.form]; !ok {
			t.Errorf("%s: the moo leg has no %s writer for %s", r.name, r.form, r.root)
		} else if p.minus != want {
			t.Errorf("%s: the moo %s save of -0 at %s is not %s.%s.bin:\n have %s\n want %s", r.name, r.form, r.path, r.expect, r.form, p.minus, want)
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
	if held+exempt != len(rows) || exempt != len(mooNegZeroNoWriter) {
		t.Errorf("%d rows held and %d exempt of %d; %d exemptions named", held, exempt, len(rows), len(mooNegZeroNoWriter))
	}
	t.Logf("%d of %d corpus rows held (%d not carried, by name); %d writer twins held", held, len(rows), exempt, twins)
}
