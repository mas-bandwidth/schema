package luautable_test

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// f32EndsCorpusDir is the cross-port corpus for A COMPRESSED FLOAT'S RANGE IS
// HELD AT FLOAT32 (docs/SPEC-TABLES.md §3.3). Its cases.tsv says what each row
// owes; internal/tablewire's TestCompressedF32EndsCorpus runs it on the
// reference.
var f32EndsCorpusDir = filepath.Join("..", "..", "..", "testdata", "conformance", "feature-witnesses", "compressed-f32-ends")

// f32EndsProbe runs every cases.tsv row (inputs.cases, the data rows only)
// through the generated Luau and prints one line per row: "<case> refused" or
// "<case> saved <hex>", the save being in the row's form (a write) or the
// root's own form (a read).
const f32EndsProbe = `local inputs = require("./inputs")
local T = require("./ProbeTable")
local St = require("./schema_tables")
local save = require("./schema_save")
local R = require("./schema_refusal")
local F = require("./schema_fixed")
local J = require("./schema_json")

local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do parts[i + 1] = string.format("%02x", buffer.readu8(b, i)) end
	return table.concat(parts)
end
local own = { V = "variable", F = "fixed" }
local fixed = { hash = T.FFixedHash, recordBytes = T.FFixedRecordBytes, layout = T.FFixedLayout }
local function savein(form: string, root: string, v: any): buffer?
	local ok, b
	if form == "variable" then ok, b = pcall(save.save, St.lookup(root), v)
	else ok, b = pcall(F.save, T.FDescriptor, fixed, { v }) end
	if ok and type(b) == "buffer" then return b end
	return nil
end
local function read(form: string, root: string, src: buffer): any
	local ok, v
	if form == "json" then ok, v = pcall(J.FromJson, root, src, {})
	elseif form == "variable" then ok, v = pcall(R.ReadStrict, nil, root, src, {})
	else
		local rows
		ok, rows = pcall(F.load, T.FDescriptor, fixed, src)
		v = if ok and type(rows) == "table" and #rows == 1 then rows[1] else nil
	end
	if ok then return v end
	return nil
end
local function set(v: any, path: string, x: number)
	local name, slot = string.match(path, "^(%w+)%[(%d+)%]$")
	local field = string.upper(string.sub(name or path, 1, 1)) .. string.sub(name or path, 2)
	if name then v[field][(tonumber(slot) :: number) + 1] = x else v[field] = x end
end
for line in string.gmatch(buffer.tostring(inputs.cases), "[^\n]+") do
	local w = string.split(line, "\t")
	local name, op, form, root, input, path, bits = w[1], w[2], w[3], w[4], w[5], w[6], w[7]
	local out
	if op == "read" then
		local src = if form == "json" then inputs[input .. ".json"] else inputs[input .. "." .. form .. ".bin"]
		local v = read(form, root, src)
		if v == nil then out = "refused" else
			local b = savein(own[root], root, v)
			out = if b then "saved " .. hex(b) else "unsaved"
		end
	else
		local v = read("json", root, inputs[input .. ".json"])
		if v == nil then out = "unloaded" else
			local cell = buffer.create(4)
			buffer.writeu32(cell, 0, tonumber(bits, 16) :: number)
			set(v, path, buffer.readf32(cell, 0))
			local b = savein(form, root, v)
			out = if b then "saved " .. hex(b) else "refused"
		end
	end
	print(name .. " " .. out)
end
`

// TestLuauCompressedF32EndsCorpus runs the compressed-f32-ends corpus on the
// generated Luau: every read and write of a compressed float holds its range
// at float32, in the variable, fixed and JSON forms, and the saves match the
// reference byte for byte.
func TestLuauCompressedF32EndsCorpus(t *testing.T) {
	slowtest.Gate(t, "the pinned Luau interpreter")
	t.Parallel()
	inputs, rows := f32EndsCorpus(t)
	u := review1121Unit(t, filepath.Join(f32EndsCorpusDir, "Probe.schema"))
	probe := filepath.Join(t.TempDir(), "probe.luau")
	if err := os.WriteFile(probe, []byte(f32EndsProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := review1121Modules(t, u, probe)
	f32EndsCheck(t, inputs, rows, luaurun.Run(t, dir, "main.luau", inputs))
}

// f32EndsCorpus reads the corpus: every file by name, and the data rows of
// cases.tsv (also handed to the probe as inputs["cases"]).
func f32EndsCorpus(t *testing.T) (map[string][]byte, [][]string) {
	t.Helper()
	entries, err := os.ReadDir(f32EndsCorpusDir)
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(f32EndsCorpusDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		inputs[e.Name()] = b
	}
	rows := review1121Rows(t, filepath.Join(f32EndsCorpusDir, "cases.tsv"), 8)
	inputs["cases"] = review1121CaseLines(rows)
	return inputs, rows
}

// f32EndsCheck holds the probe's lines to cases.tsv: a `refused` row owes the
// refusal, a stem owes the save <stem>.<form>.bin byte for byte.
func f32EndsCheck(t *testing.T, inputs map[string][]byte, rows [][]string, raw []byte) {
	t.Helper()
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(raw), "\n"), "\n") {
		k, v, _ := strings.Cut(line, " ")
		got[k] = v
	}
	own := map[string]string{"V": "variable", "F": "fixed"}
	for _, r := range rows {
		name, op, form, root, expect := r[0], r[1], r[2], r[3], r[7]
		if expect == "refused" {
			if got[name] != "refused" {
				t.Errorf("%s: %q, want refused", name, got[name])
			}
			continue
		}
		saved := own[root]
		if op == "write" {
			saved = form
		}
		want := inputs[expect+"."+saved+".bin"]
		have, ok := strings.CutPrefix(got[name], "saved ")
		if b, err := hex.DecodeString(have); !ok || err != nil || !bytes.Equal(b, want) {
			t.Errorf("%s: %q, want saved %s", name, got[name], hex.EncodeToString(want))
		}
	}
	if t.Failed() {
		t.Logf("probe output:\n%s", raw)
	}
}
