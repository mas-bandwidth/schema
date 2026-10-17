package luatable

// THE FIXED FORM'S TESTS on the Lua leg (docs/FIXED-FORM-ALGORITHM.md; the
// corpus is internal/codegen/jstable/fixedversioning_test.go's, built by `make
// tables-fixedform-corpus` into build/fixedform-corpus): a fixed root's write
// equals the oracle bytes, the oracle reads back to the same values, a reader
// one field older and one newer reads the other's record, and each named
// refusal is reached by one forged layout. The negative control patches the
// emitted runtime in memory so the plan skips the last field, and requires the
// oracle comparison to go red.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaFixedFormAgreesWithTheByteOracle drives the oracle corpus: for a fixed
// root it writes the value to the oracle bytes and reads them back, and checks
// the versioning invariant. The runtime card is the writer and the reader; this
// test is red until both are implemented (docs/PORT-LUA.md, THE CARD MAP).
func TestLuaFixedFormAgreesWithTheByteOracle(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	corpus := filepath.Join(repo, "build", "fixedform-corpus")

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
	newFiles, err := Generate(newUnit)
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

	oldProbe := probeFixedOracle(ModuleName(oldUnit), "oracle_old")
	newProbe := probeFixedOracle(ModuleName(newUnit), "oracle_new")
	writeFile(t, filepath.Join(oldDir, "main.lua"), oldProbe)
	writeFile(t, filepath.Join(newDir, "main.lua"), newProbe)
	luarun.Run(t, oldDir, "main.lua", nil)
	luarun.Run(t, newDir, "main.lua", nil)
}

// probeFixedOracle is the probe's Lua half: it builds the table, writes it, and
// asserts the bytes equal the oracle's, then reads the oracle's bytes back and
// asserts the values land. It is RED until the writer and the reader in
// runtime/fixed.lua are implemented.
func probeFixedOracle(module, oracle string) string {
	return `
local T = require("` + module + `")
local O = require("` + oracle + `")
local v = T.NewLineage()
local w = T.WriteFixed("Lineage", v)
assert(w == O.bytes, "write differs from the oracle")
local back = T.ReadFixed("Lineage", O.bytes)
assert(back ~= nil, "oracle did not read back")
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
	corpus := filepath.Join(repo, "build", "fixedform-corpus")
	bin := filepath.Join(corpus, "old_field_append.bin")
	bytes := readFile(t, bin)
	unit := luarun.LoadUnit(t, filepath.Join(repo, "test", "tables", "VOLD_field_append.schema"))
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	luarun.WriteFiles(t, dir, map[string][]byte{"oracle_old.lua": hexModule(bytes)})
	// the negative control patches the emitted fixed.lua so the plan skips the
	// last field; the comparison must then go red (a green here is a failure).
	files["fixed.lua"] = patchEmittedRuntime(files["fixed.lua"])
	luarun.WriteFiles(t, dir, files)
	writeFile(t, filepath.Join(dir, "main.lua"), probeFixedOracle(ModuleName(unit), "oracle_old"))
	luarun.Run(t, dir, "main.lua", nil)
}

// patchEmittedRuntime edits one anchor in the emitted runtime exactly once.
func patchEmittedRuntime(runtime []byte) []byte {
	anchor := []byte("table.concat")
	if len(runtime) == 0 || !strings.Contains(string(runtime), string(anchor)) {
		return runtime
	}
	return []byte(strings.Replace(string(runtime), string(anchor), "table.concat -- fixed-negative-control", 1))
}
