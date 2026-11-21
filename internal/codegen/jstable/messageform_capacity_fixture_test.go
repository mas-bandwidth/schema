package jstable

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestJsMessageLoadCapacityFixture reads the committed capacity fixture,
// testdata/batch_capacity_two.bin (two LoginRequest bodies of tables/backend,
// client_build 40, written by the Go engine and pinned to it by
// test/conformance/harness TestBatchCapacityFixtureIsTheGoEnginesInEveryLeg),
// through the generated JavaScript's LoginRequestLoadMessages. Storage for one
// body whose client_build is the sentinel 17 owes "A refused batch returns a
// count of `0`" and "refuses before it decodes anything" (docs/SPEC-TABLES.md
// §3.3): 0, batch_too_large, the sentinel untouched. Storage for two owes 2 and
// 40 in both. A caller's capacity is a resource limit (§6.5), so this row runs
// here, in the leg's own tests, never in the shared suites.
func TestJsMessageLoadCapacityFixture(t *testing.T) {
	t.Parallel()
	node := jsNode(t)
	fixture, err := os.ReadFile(filepath.Join("testdata", "batch_capacity_two.bin"))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitPaths(t, []string{filepath.Join("..", "..", "..", "tables", "backend")})
	st := findTable(t, u, "LoginRequest")
	probe := messageProbeHeader(u, st, "") + `
const wire = new Uint8Array(readFileSync("./batch_capacity_two.bin"));
const one = [new LoginRequest()];
one[0].client_build = 17;
const report = {};
const count = LoginRequestLoadMessages(one, v, wire, report);
if (count !== 0 || report.Verdict !== 1 || report.Reason !== "batch_too_large" || one[0].client_build !== 17) {
  fail("storage for one: count=" + count + " client_build=" + one[0].client_build + " " + JSON.stringify(report));
}
const two = [new LoginRequest(), new LoginRequest()];
const loaded = {};
const got = LoginRequestLoadMessages(two, v, wire, loaded);
if (got !== 2 || loaded.Verdict !== 0 || two[0].client_build !== 40 || two[1].client_build !== 40) {
  fail("storage for two: count=" + got + " " + JSON.stringify(loaded) + " " + JSON.stringify(two));
}
` + probeTail()
	out, err := jsRunMessageProbe(t, node, u, map[string][]byte{
		"ann.bin":                 tablewire.Announce(u),
		"batch_capacity_two.bin": fixture,
	}, probe)
	if err != nil {
		t.Fatalf("capacity fixture: %v\n%s", err, out)
	}
}
