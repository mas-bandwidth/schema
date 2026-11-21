package mootable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestMooMessageLoadCapacityFixture reads the committed capacity fixture,
// testdata/batch_capacity_two.bin (two LoginRequest bodies of tables/backend,
// client_build 40, written by the Go engine and pinned to it by
// test/conformance/harness TestBatchCapacityFixtureIsTheGoEnginesInEveryLeg),
// through the emitted table_message.moo. A capacity of one owes "A refused
// batch returns a count of `0`" (docs/SPEC-TABLES.md §3.3): 0,
// batch_too_large, and no values; moo hands back a fresh list rather than
// writing caller storage, so "untouched" is the empty list. A capacity of two
// owes 2 bodies, each carrying 40. A caller's capacity is a resource limit
// (§6.5), so this row runs here, in the leg's own tests, never in the shared
// suites.
func TestMooMessageLoadCapacityFixture(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	fixture, err := os.ReadFile(filepath.Join("testdata", "batch_capacity_two.bin"))
	if err != nil {
		t.Fatal(err)
	}
	units, _ := readManifest(t)
	g := generateUnit(t, units, "backenddemo")
	if g.err != nil {
		t.Fatalf("unit backenddemo is refused: %v", g.err)
	}
	main := "--!strict\nlocal inputs = require(\"./inputs\")\nlocal MS = require(\"./table_message\")\n" + g.requireAll() + `
local v = MS.NewVocabulary()
assert(MS.AnnounceRead(v, inputs.conn, {}), "announce")
local rep: any = {}
local n, values = MS.LoadMessages(v, "LoginRequest", inputs.wire, 1, rep)
print(string.format("one %d %d %s", n, #values, tostring(rep.reason)))
local rep2: any = {}
local m, two = MS.LoadMessages(v, "LoginRequest", inputs.wire, 2, rep2)
print(string.format("two %d %d %s %s", m, #two, tostring(two[1] and two[1].client_build), tostring(two[2] and two[2].client_build)))
`
	out := g.run(t, main, map[string][]byte{"conn": readRoot(t, "testdata/wire/tables/backend_conn.bin"), "wire": fixture})
	lines := strings.Split(out, "\n")
	if len(lines) != 2 || lines[0] != "one 0 0 batch_too_large" || lines[1] != "two 2 2 40 40" {
		t.Fatalf("capacity fixture: want \"one 0 0 batch_too_large\" and \"two 2 2 40 40\", got:\n%s", out)
	}
}
