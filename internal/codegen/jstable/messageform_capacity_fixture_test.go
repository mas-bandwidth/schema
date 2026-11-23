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
// §3.3): 0, batch_too_large, and (R4) every value handed in at its declared
// default, not the sentinel. Storage for THREE sentinel values owes the same
// 0 and all three at their defaults after a framing refusal (the form byte
// changed), a body refusal (the last byte dropped) and a close refusal (a byte
// past the pad): slot 2 is past the batch's count, so only a reset of the whole
// array reaches it. Storage for two owes 2 and 40 in both. A caller's capacity is a resource limit (§6.5), so this row runs
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
one[0].clientBuild = 17;
const report = {};
const count = LoginRequestLoadMessages(one, v, wire, report);
if (count !== 0 || report.Verdict !== 1 || report.Reason !== "batch_too_large" || one[0].clientBuild !== new LoginRequest().clientBuild) {
  fail("storage for one: count=" + count + " client_build=" + one[0].clientBuild + " " + JSON.stringify(report));
}
const defaultBuild = new LoginRequest().clientBuild;
if (defaultBuild === 17) { fail("the sentinel is the default"); }
const damaged = {
  "framing": Uint8Array.from(wire, (x, i) => (i === 0 ? 3 : x)),
  "body": wire.subarray(0, wire.length - 1),
  "close": Uint8Array.from([...wire, 0]),
};
for (const name of Object.keys(damaged)) {
  const three = [new LoginRequest(), new LoginRequest(), new LoginRequest()];
  three.forEach((x) => { x.clientBuild = 17; });
  const rep3 = {};
  const got3 = LoginRequestLoadMessages(three, v, damaged[name], rep3);
  const dirty = three.map((x) => x.clientBuild).join(",");
  if (got3 !== 0 || (rep3.Verdict === 0 && !rep3.Damaged) || three.some((x) => x.clientBuild !== defaultBuild)) {
    fail(name + " refusal: count=" + got3 + " client_build=" + dirty + " " + JSON.stringify(rep3));
  }
}
const two = [new LoginRequest(), new LoginRequest()];
const loaded = {};
const got = LoginRequestLoadMessages(two, v, wire, loaded);
if (got !== 2 || loaded.Verdict !== 0 || two[0].clientBuild !== 40 || two[1].clientBuild !== 40) {
  fail("storage for two: count=" + got + " " + JSON.stringify(loaded) + " clientBuild=" + two[0].clientBuild + "," + two[1].clientBuild);
}
` + probeTail()
	out, err := jsRunMessageProbe(t, node, u, map[string][]byte{
		"ann.bin":                tablewire.Announce(u),
		"batch_capacity_two.bin": fixture,
	}, probe)
	if err != nil {
		t.Fatalf("capacity fixture: %v\n%s", err, out)
	}
}
