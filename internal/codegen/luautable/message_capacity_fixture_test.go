package luautable_test

import (
	"encoding/hex"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
)

// TestLuauMessageLoadCapacityFixture reads the committed capacity fixture,
// testdata/batch_capacity_two.bin (two LoginRequest bodies of tables/backend,
// client_build 40, written by the Go engine and pinned to it by
// test/conformance/harness TestBatchCapacityFixtureIsTheGoEnginesInEveryLeg),
// through schema_message.luau's LoadMessages. A capacity of one owes "A
// refused batch returns a count of `0`" (docs/SPEC-TABLES.md §3.3): 0,
// batch_too_large, and no bodies; Luau hands back a fresh list rather than
// writing caller storage, so "untouched" is the empty list. A capacity of two
// owes both bodies, and saving them reproduces the fixture. A caller's
// capacity is a resource limit (§6.5), so this row runs here, in the leg's own
// tests, never in the shared suites.
func TestLuauMessageLoadCapacityFixture(t *testing.T) {
	t.Parallel()
	fixture, err := os.ReadFile(filepath.Join("testdata", "batch_capacity_two.bin"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{filepath.Join("..", "..", "..", "tables", "backend", "Backend.schema")})
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
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	main := `local M = require("./schema_message")
require("./BackendTable")
local inputs = require("./inputs")
local v = M.NewVocabulary()
assert(M.AnnounceRead(v, inputs.conn, { malformed = false, verdict = 0, reason = "", unknown = 0 }), "announce")
local function hex(b)
	local p = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		p[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(p)
end
local rep = { malformed = false, verdict = 0, reason = "", unknown = 0 }
local n, one = M.LoadMessages(v, "LoginRequest", inputs.wire, 1, rep)
print("one " .. tostring(n) .. " " .. tostring(#one) .. " " .. tostring(rep.reason))
local rep2 = { malformed = false, verdict = 0, reason = "", unknown = 0 }
local m, two = M.LoadMessages(v, "LoginRequest", inputs.wire, 2, rep2)
local out = M.SaveMessages(v, "LoginRequest", two, { malformed = false, verdict = 0, reason = "", unknown = 0 })
print("two " .. tostring(m) .. " " .. tostring(#two) .. " " .. (if out ~= nil then hex(out) else "nil"))
`
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	conn, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", "tables", "backend_conn.bin"))
	if err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", map[string][]byte{"conn": conn, "wire": fixture})))
	want := "one 0 0 batch_too_large\ntwo 2 2 " + hex.EncodeToString(fixture)
	if out != want {
		t.Fatalf("capacity fixture:\n got  %s\n want %s", out, want)
	}
}
