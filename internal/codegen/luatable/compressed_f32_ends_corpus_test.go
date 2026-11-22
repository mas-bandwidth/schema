package luatable

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// f32EndsCorpusDir is the cross-port corpus for A COMPRESSED FLOAT'S RANGE IS
// HELD AT FLOAT32 (docs/SPEC-TABLES.md §3.3). Its cases.tsv says what each row
// owes; internal/tablewire's TestCompressedF32EndsCorpus runs it on the
// reference.
const f32EndsCorpusDir = "../../../testdata/conformance/feature-witnesses/compressed-f32-ends"

// f32EndsProbe runs every cases.tsv row (inputs.cases, the data rows only)
// through the generated Lua and prints one line per row: "<case> refused" or
// "<case> saved <hex>", the save being in the row's form (a write) or the
// root's own form (a read).
const f32EndsProbe = `local T = require("ProbeTables")
local S = require("save")
local L = require("load")
local J = require("json")
local inputs = require("inputs")
local function hex(s) return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end)) end
local own = { V = "variable", F = "fixed" }
local function new(root) if root == "V" then return T.NewV() end return T.NewF() end
local function save(form, root, v)
  local ok, b
  if form == "variable" then ok, b = pcall(S.Save, T, root, v)
  else ok, b = pcall(T.FixedSave, root, { v }) end
  if ok and type(b) == "string" then return b end
  return nil
end
local function read(form, root, src)
  local ok, v
  if form == "json" then ok, v = pcall(J.FromJson, T, root, src, {})
  elseif form == "variable" then ok, v = pcall(L.Load, T, root, src, {})
  else
    local rows
    ok, rows = pcall(T.FixedLoad, root, src)
    v = ok and type(rows) == "table" and #rows == 1 and rows[1] or nil
  end
  if ok then return v end
  return nil
end
local function set(v, path, x)
  local name, slot = path:match("^(%w+)%[(%d+)%]$")
  if name then v[name][tonumber(slot) + 1] = x else v[path] = x end
end
for line in inputs.cases:gmatch("[^\n]+") do
  local name, op, form, root, input, path, bits = line:match("^([^\t]+)\t([^\t]+)\t([^\t]+)\t([^\t]+)\t([^\t]+)\t([^\t]+)\t([^\t]+)\t")
  local out
  if op == "read" then
    local src = form == "json" and inputs[input .. ".json"] or inputs[input .. "." .. form .. ".bin"]
    local v = read(form, root, src)
    if v == nil then out = "refused" else
      local b = save(own[root], root, v)
      out = b and ("saved " .. hex(b)) or "unsaved"
    end
  else
    local v = read("json", root, inputs[input .. ".json"])
    if v == nil then out = "unloaded" else
      set(v, path, (string.unpack("<f", string.pack("<I4", tonumber(bits, 16)))))
      local b = save(form, root, v)
      out = b and ("saved " .. hex(b)) or "refused"
    end
  end
  print(name .. " " .. out)
end
`

// TestLuaCompressedF32EndsCorpus runs the compressed-f32-ends corpus on the
// generated Lua: every read and write of a compressed float holds its range
// at float32, in the variable, fixed and JSON forms, and the saves match the
// reference byte for byte.
func TestLuaCompressedF32EndsCorpus(t *testing.T) {
	slowtest.Gate(t, "the pinned Lua interpreter")
	t.Parallel()
	inputs, rows := f32EndsCorpus(t)
	u := luarun.LoadUnit(t, filepath.Join(f32EndsCorpusDir, "Probe.schema"))
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	writeFile(t, filepath.Join(dir, "main.lua"), strings.Replace(f32EndsProbe, `require("ProbeTables")`, `require("`+ModuleName(u)+`")`, 1))
	f32EndsCheck(t, inputs, rows, luarun.Run(t, dir, "main.lua", inputs))
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
	var rows [][]string
	var cases strings.Builder
	for line := range strings.SplitSeq(string(inputs["cases.tsv"]), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 8 {
			t.Fatalf("cases.tsv: %q has %d fields, want 8", line, len(f))
		}
		rows = append(rows, f)
		cases.WriteString(line + "\n")
	}
	if len(rows) == 0 {
		t.Fatal("cases.tsv holds no rows")
	}
	inputs["cases"] = []byte(cases.String())
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
