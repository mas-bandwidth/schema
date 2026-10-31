package luatable

// THE FIXED FORM'S TESTS on the Lua leg (docs/FIXED-FORM-ALGORITHM.md; the
// corpus is internal/codegen/jstable/fixedversioning_test.go's, built by `make
// tables-fixedform-corpus` into build/fixedform-corpus): a fixed root's write
// equals the oracle bytes, the oracle reads back to the same values, a newer
// reader reads an older record (the versioning invariant, §5), and the
// negative control patches the emitted runtime in memory so the plan skips
// the last field, requiring the oracle comparison to go red.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// luaFixedCorpus is the C++ reference's byte oracle, build/fixedform-corpus,
// exactly as the other legs resolve it (jstable's jsFixedCorpus, cpptable's
// cppFixedCorpus). A bare `go test` on a tree that never built the oracle has
// nothing to read and SKIPS, saying so; under SCHEMA_REQUIRE_CORPUS (make
// test-lua builds the corpus first and sets it) a missing file is a FAILURE,
// because a suite that skips itself there would report green over §5 having
// never run.
func luaFixedCorpus(t *testing.T, repo string) string {
	t.Helper()
	dir := filepath.Join(repo, "build", "fixedform-corpus")
	if _, err := os.Stat(filepath.Join(dir, "old_field_append.bin")); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and the reference's byte oracle is not in %s: %v", dir, err)
		}
		t.Skip("the reference's byte oracle is not built: make tables-fixedform-corpus")
	}
	return dir
}

// TestLuaFixedFormAgreesWithTheByteOracle drives the oracle corpus: for a fixed
// root it writes the value to the oracle bytes and reads them back, and checks
// that a newer reader reads the older record (docs/FIXED-FORM-ALGORITHM.md §5).
func TestLuaFixedFormAgreesWithTheByteOracle(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	corpus := luaFixedCorpus(t, repo)

	// the field_append pair: an older and a newer generation of one table
	oldBin := filepath.Join(corpus, "old_field_append.bin")
	newBin := filepath.Join(corpus, "new_field_append.bin")
	oldBytes := readFile(t, oldBin)
	newBytes := readFile(t, newBin)

	oldUnit := luarun.LoadUnit(t, filepath.Join(repo, "test", "tables", "VOLD_field_append.schema"))
	newUnit := luarun.LoadUnit(t, filepath.Join(repo, "test", "tables", "VNEW_field_append.schema"))
	oldFiles, err := Generate(oldUnit)
	if err != nil {
		t.Fatal(err)
	}
	// the NEWER reader reads the OLDER writer's records under ITS layout
	// (docs/FIXED-FORM-ALGORITHM.md §5.2), so the newer unit is generated with
	// the older entry in its lineage.
	lineage := map[string][]FixedLineageEntry{}
	oldEntry, ok := FixedLineageOf(oldUnit, "Lineage")
	if !ok {
		t.Fatal("no lineage entry for Lineage")
	}
	lineage["Lineage"] = []FixedLineageEntry{oldEntry}
	newFiles, err := GenerateLineage(newUnit, lineage)
	if err != nil {
		t.Fatal(err)
	}
	oldDir := t.TempDir()
	luarun.WriteFiles(t, oldDir, oldFiles)
	newDir := t.TempDir()
	luarun.WriteFiles(t, newDir, newFiles)

	// the oracle bytes ride in as a module for the probe to compare against
	luarun.WriteFiles(t, oldDir, map[string][]byte{"oracle_old.lua": hexModule(oldBytes)})
	luarun.WriteFiles(t, newDir, map[string][]byte{"oracle_new.lua": hexModule(newBytes)})
	luarun.WriteFiles(t, newDir, map[string][]byte{"oracle_old.lua": hexModule(oldBytes)})

	oldProbe := probeFixedOracle(ModuleName(oldUnit), "oracle_old", `
v.x = 11; v.y = 22; v.z = 33
local want = { x = 11, y = 22, z = 33 }`)
	newProbe := probeFixedOracle(ModuleName(newUnit), "oracle_new", `
v.x = 44; v.y = 55; v.z = 66; v.w = 777
local want = { x = 44, y = 55, z = 66, w = 777 }`)
	// THE VERSIONING INVARIANT (§5): the newer reader reads the older writer's
	// file, every old value lands exactly, and the appended field is its
	// declared default (77), never the older record's bytes.
	newProbe += `
local old = T.ReadFixed("Lineage", require("oracle_old").bytes)
assert(old.x == 11 and old.y == 22 and old.z == 33, "the old values did not land")
assert(old.w == 77, "the appended field is not its declared default")
print("fixed-old-ok")
`
	// THE NAMED REFUSALS, reached by one forged layout each (docs/
	// FIXED-FORM-ALGORITHM.md §1.1, §2, §4.2).
	oldProbe += `
local function refusal(kind, b)
  local ok, err = pcall(function() T.ReadFixed("Lineage", b) end)
  assert(not ok and tostring(err):find(kind, 1, true), "expected " .. kind .. ", got " .. tostring(err))
end
local base = O.bytes
local layoutLen = string.unpack("<I4", base, 17)
local L = 21 + layoutLen
refusal("previous_form", "\1" .. base:sub(2))
refusal("layout_malformed", base:sub(1, 20) .. string.pack("<I4", 0) .. base:sub(25))
refusal("layout_malformed", base:sub(1, 33) .. string.pack("<I4", 65537) .. base:sub(38))
refusal("plan_too_large", base:sub(1, L - 1) .. string.rep(base:sub(L), 4097))
print("fixed-refusals-ok")
`
	writeFile(t, filepath.Join(oldDir, "main.lua"), oldProbe)
	writeFile(t, filepath.Join(newDir, "main.lua"), newProbe)
	luarun.Run(t, oldDir, "main.lua", nil)
	luarun.Run(t, newDir, "main.lua", nil)
}

// probeFixedOracle is the probe's Lua half: it builds the table, writes it, and
// asserts the bytes equal the oracle's, then reads the oracle's bytes back and
// asserts the values land.
func probeFixedOracle(module, oracle, setup string) string {
	return `
local T = require("` + module + `")
local O = require("` + oracle + `")
local v = T.NewLineage()
` + setup + `
local w = T.WriteFixed("Lineage", v)
assert(w == O.bytes, "write differs from the oracle")
local back = T.ReadFixed("Lineage", O.bytes)
assert(back ~= nil, "oracle did not read back")
for k, x in pairs(want) do assert(back[k] == x, "read back " .. k) end
print("fixed-ok")
`
}

// hexModule renders bytes as a Lua module holding them in a string.
func hexModule(b []byte) []byte {
	var sb strings.Builder
	sb.WriteString("return { bytes = \"")
	for _, c := range b {
		fmt.Fprintf(&sb, "\\%d", c)
	}
	sb.WriteString("\" }\n")
	return []byte(sb.String())
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestLuaFixedFormNegativeControl patches the emitted runtime in memory (one
// anchor, matched exactly once) so the plan skips the last field, and requires
// the oracle comparison to go red.
func TestLuaFixedFormNegativeControl(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	corpus := luaFixedCorpus(t, repo)
	bin := filepath.Join(corpus, "old_field_append.bin")
	bytes := readFile(t, bin)
	unit := luarun.LoadUnit(t, filepath.Join(repo, "test", "tables", "VOLD_field_append.schema"))
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	// the negative control patches the emitted fixed.lua so the plan skips the
	// last field; the comparison must then go red (a green here is a failure).
	files["fixed.lua"] = patchEmittedRuntime(files["fixed.lua"])
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	luarun.WriteFiles(t, dir, map[string][]byte{"oracle_old.lua": hexModule(bytes)})
	writeFile(t, filepath.Join(dir, "main.lua"), `
local T = require("`+ModuleName(unit)+`")
local O = require("oracle_old")
local v = T.NewLineage()
v.x = 11; v.y = 22; v.z = 33
local ok = pcall(function()
  local w = T.WriteFixed("Lineage", v)
  assert(w == O.bytes, "write differs from the oracle")
end)
if ok then print("green") else print("red") end
`)
	out := string(luarun.Run(t, dir, "main.lua", nil))
	if !strings.Contains(out, "red") {
		t.Fatalf("the sabotaged runtime still matched the oracle: %q", out)
	}
}

// patchEmittedRuntime skips the last field in the emitted runtime's table walk.
func patchEmittedRuntime(runtime []byte) []byte {
	anchor := "local parts = {}\n  for i, f in ipairs(desc.fields) do\n    local fs"
	repl := "local parts = {}\n  for i, f in ipairs(desc.fields) do\n    if i == #desc.fields then break end\n    local fs"
	s := string(runtime)
	if strings.Count(s, anchor) != 1 {
		return runtime
	}
	return []byte(strings.Replace(s, anchor, repl, 1))
}
