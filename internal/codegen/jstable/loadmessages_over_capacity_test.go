package jstable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestJsLoadMessagesOverCapacityReturnsZero holds docs/SPEC-TABLES.md §3.3's
// rule that a batch whose count is above the caller's storage is refused by
// name and returns 0, as every refused batch does: the report says refused and
// nothing in the caller's storage changes.
func TestJsLoadMessagesOverCapacityReturnsZero(t *testing.T) {
	t.Parallel()

	node := jsNode(t)
	u := loadSchemaString(t, "loadmessages_over_capacity.schema", `package loadmessagesovercapacity

fixed table Probe
{
    value int32
}
`)
	st := findTable(t, u, "Probe")
	probe := messageProbeHeader(u, st, "")
	probe += `
const bodies = [new Probe(), new Probe(), new Probe()];
bodies[0].value = 11;
bodies[1].value = 22;
bodies[2].value = 33;
const buf = new Uint8Array(65536);
const saveReport = {};
const n = ProbeSaveMessages(bodies, buf, saveReport);
if (n < 0 || saveReport.Verdict !== 0) { fail("SaveMessages: " + JSON.stringify(saveReport)); }
const batch = buf.subarray(0, n);

for (const capacity of [1, 2]) {
  const storage = Array.from({ length: capacity }, () => new Probe());
  for (let i = 0; i < capacity; i++) { storage[i].value = 900 + i; }
  const before = JSON.stringify(storage);
  const report = {};
  const count = ProbeLoadMessages(storage, v, batch, report);
  if (count !== 0 || report.Verdict !== 1 || report.Reason !== "batch_too_large" || report.Damaged) {
    fail("LoadMessages above capacity " + capacity + ": count=" + count + " " + JSON.stringify(report));
  }
  if (JSON.stringify(storage) !== before) {
    fail("LoadMessages above capacity " + capacity + " changed the caller's storage: " + JSON.stringify(storage));
  }
}

const full = Array.from({ length: 3 }, () => new Probe());
const fullReport = {};
const fullCount = ProbeLoadMessages(full, v, batch, fullReport);
if (fullCount !== 3 || fullReport.Verdict !== 0 || fullReport.Damaged) {
  fail("LoadMessages into capacity 3: count=" + fullCount + " " + JSON.stringify(fullReport));
}
if (full[0].value !== 11 || full[1].value !== 22 || full[2].value !== 33) {
  fail("LoadMessages into capacity 3 filled " + JSON.stringify(full));
}
`
	probe += probeTail()
	out, err := jsRunMessageProbe(t, node, u, map[string][]byte{
		"ann.bin": tablewire.Announce(u),
	}, probe)
	if err != nil {
		t.Fatalf("over capacity: %v\n%s", err, out)
	}
}
