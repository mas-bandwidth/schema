package luatable

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaMessageLoadCapacityFixture reads the committed capacity fixture,
// testdata/batch_capacity_two.bin (two LoginRequest bodies of tables/backend,
// client_build 40, written by the Go engine and pinned to it by
// test/conformance/harness TestBatchCapacityFixtureIsTheGoEnginesInEveryLeg),
// through the generated Lua's LoadMessages. The Lua read takes no capacity: it
// hands back a fresh list sized to the wire's M, so there is no caller storage
// for a capacity refusal to protect (docs/SPEC-TABLES.md §3.3, "A refused batch
// returns a count of `0`", has no capacity case to answer here). The fixture
// owes both bodies, each carrying 40.
func TestLuaMessageLoadCapacityFixture(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	unit := luarun.LoadUnit(t, filepath.Join(repo, "tables", "backend", "Backend.schema"))
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	writeFile(t, filepath.Join(dir, "main.lua"), `
local T = require("`+ModuleName(unit)+`")
local inputs = require("inputs")
local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
assert(T.AnnounceRead(vocab, inputs.announce, { damaged = false, refused = 0 }))
local report = { damaged = false, refused = 0 }
local values = T.LoadMessages("LoginRequest", vocab, inputs.batch, report)
print("two " .. tostring(values and #values) .. " " .. tostring(values and values[1] and values[1].client_build) .. " " .. tostring(values and values[2] and values[2].client_build))
`)
	out := strings.TrimSpace(string(luarun.Run(t, dir, "main.lua", map[string][]byte{
		"announce": readFile(t, filepath.Join(repo, "testdata", "wire", "tables", "backend_conn.bin")),
		"batch":    readFile(t, filepath.Join("testdata", "batch_capacity_two.bin")),
	})))
	if out != "two 2 40 40" {
		t.Fatalf("capacity fixture: want \"two 2 40 40\", got %q", out)
	}
}
