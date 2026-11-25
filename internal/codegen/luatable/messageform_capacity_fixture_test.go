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
// through the generated Lua's LoadMessages. A read with no storage handed in
// gets a fresh list sized to the wire's M: the fixture owes both bodies, each
// carrying 40.
//
// THE CALLER'S CAPACITY (docs/SPEC-TABLES.md §3.3, "M above the caller's
// capacity on the read side is a refusal by name"): the caller's storage is
// the into table it hands in. Two bodies into storage of one refuse
// batch_too_large with nothing decoded, and the one value handed in is reset
// (§6.5, THE CALLER'S VALUES AFTER A REFUSAL; R4); into storage of two they
// read.
//
// EVERY REFUSAL ANSWERS THE SAME SHAPE (ruling R4): the fixture with its form
// byte changed (a framing refusal) and the fixture with its last byte dropped
// (a body or close refusal) each hand back nil, never an empty list for one
// and nil for the other.
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
local framing = T.LoadMessages("LoginRequest", vocab, "\3" .. inputs.batch:sub(2), { damaged = false, refused = 0 })
local body = T.LoadMessages("LoginRequest", vocab, inputs.batch:sub(1, #inputs.batch - 1), { damaged = false, refused = 0 })
print("framing " .. type(framing) .. " body " .. type(body))
local one = { T.NewLoginRequest() }
one[1].client_build = 7
local crep = { damaged = false, refused = 0 }
local over = T.LoadMessages("LoginRequest", vocab, inputs.batch, crep, one)
print("one " .. type(over) .. " " .. tostring(crep.reason) .. " " .. tostring(one[1].client_build))
local two = { T.NewLoginRequest(), T.NewLoginRequest() }
local fit = T.LoadMessages("LoginRequest", vocab, inputs.batch, { damaged = false, refused = 0 }, two)
print("fit " .. tostring(fit and #fit) .. " " .. tostring(two[1].client_build) .. " " .. tostring(two[2].client_build))
`)
	out := strings.TrimSpace(string(luarun.Run(t, dir, "main.lua", map[string][]byte{
		"announce": readFile(t, filepath.Join(repo, "testdata", "wire", "tables", "backend_conn.bin")),
		"batch":    readFile(t, filepath.Join("testdata", "batch_capacity_two.bin")),
	})))
	const want = "two 2 40 40\nframing nil body nil\none nil batch_too_large 0\nfit 2 40 40"
	if out != want {
		t.Fatalf("capacity fixture: want %q, got %q", want, out)
	}
}
