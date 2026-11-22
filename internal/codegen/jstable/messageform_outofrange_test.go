package jstable

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestJsMessageLoadRefusesOutOfRangeBatch reads the conformance corpus's
// batch_range_in_second wire (two StorePurchase bodies of tables/backend, a
// ranged value past its maximum in the second) through the generated
// JavaScript's StorePurchaseLoadMessages. "A refused batch returns a count of
// `0`" (docs/SPEC-TABLES.md §3.3): a value outside its declared range refuses
// the batch by name as out_of_range, the count is 0 and no body is delivered.
// Before the fix the loader clamped the value and answered 2.
func TestJsMessageLoadRefusesOutOfRangeBatch(t *testing.T) {
	t.Parallel()
	node := jsNode(t)
	fixture, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "conformance", "tables", "message-batch", "batch_range_in_second.bin"))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitPaths(t, []string{filepath.Join("..", "..", "..", "tables", "backend")})
	st := findTable(t, u, "StorePurchase")
	probe := messageProbeHeader(u, st, "") + `
const wire = new Uint8Array(readFileSync("./batch_range_in_second.bin"));
const values = Array.from({ length: 256 }, () => new StorePurchase());
const report = {};
const count = StorePurchaseLoadMessages(values, v, wire, report);
if (count !== 0 || report.Verdict !== 1 || report.Reason !== "out_of_range") {
  fail("out-of-range batch: count=" + count + " " + JSON.stringify(report));
}
` + probeTail()
	out, err := jsRunMessageProbe(t, node, u, map[string][]byte{
		"ann.bin":                   tablewire.Announce(u),
		"batch_range_in_second.bin": fixture,
	}, probe)
	if err != nil {
		t.Fatalf("out-of-range batch: %v\n%s", err, out)
	}
}
