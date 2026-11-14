package jstable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestJsSaveMessagesRefusesOffContract holds the message writer to docs/SPEC.md
// §5.1 and docs/SPEC-TABLES.md §3.3: JavaScript refuses in every build, so a
// ranged value off its bounds, an enum ordinal past its last variant, a used
// length past the text's bound and ill-formed text are refused by
// MeasureMessages and SaveMessages with -1 and the reason invalid_value, never
// masked, wrapped or clamped into the field's width, and SaveMessages leaves the
// caller's buffer untouched. Each lawful twin at the same boundary is written and
// reads back, so a writer that refused everything fails here too.
func TestJsSaveMessagesRefusesOffContract(t *testing.T) {
	t.Parallel()

	node := jsNode(t)
	u := loadSchemaString(t, "savemessages_offcontract.schema", `package savemessagesoffcontract

enum Coin
{
    Gold, Gems
}

type Score
{
    points int32 | min = 0, max = 1000
}

fixed table Probe
{
    quantity int32 = 1 | min = 1, max = 99
    sku string(4)
    coin Coin
    best Score
}
`)
	st := findTable(t, u, "Probe")
	probe := messageProbeHeader(u, st, "")
	probe += `
const enc = new TextEncoder();
function lawful() {
  const p = new Probe();
  p.quantity = 5;
  p.sku.set(enc.encode("ab"));
  p.skuLength = 2;
  p.coin = 1;
  p.best.points = 7;
  return p;
}
function setSku(p, bytes) { p.sku.fill(0); p.sku.set(bytes.subarray(0, Math.min(bytes.length, p.sku.length))); p.skuLength = bytes.length; }
const rows = [
  ["quantity high", (p) => { p.quantity = 100; }, (p) => { p.quantity = 99; }],
  ["quantity low", (p) => { p.quantity = 0; }, (p) => { p.quantity = 1; }],
  ["coin ordinal", (p) => { p.coin = 3; }, (p) => { p.coin = 2; }],
  ["sku length", (p) => { p.sku.fill(0x61); p.skuLength = 5; }, (p) => { p.sku.fill(0x61); p.skuLength = 4; }],
  ["sku ill-formed", (p) => { setSku(p, new Uint8Array([0x61, 0xff, 0x62])); }, (p) => { setSku(p, new Uint8Array([0x61, 0xc3, 0xa9])); }],
  ["sku zero byte", (p) => { setSku(p, new Uint8Array([0x61, 0x00, 0x62])); }, null],
  ["nested points", (p) => { p.best.points = 1001; }, (p) => { p.best.points = 1000; }],
];
for (const [name, bad, good] of rows) {
  const p = lawful();
  bad(p);
  const mr = {};
  const m = ProbeMeasureMessages([p], mr);
  if (m !== -1 || mr.Reason !== "invalid_value") { fail(name + ": MeasureMessages answered " + m + " " + JSON.stringify(mr)); }
  const buf = new Uint8Array(256).fill(0xa5);
  const sr = {};
  const n = ProbeSaveMessages([p], buf, sr);
  if (n !== -1 || sr.Reason !== "invalid_value") { fail(name + ": SaveMessages answered " + n + " " + JSON.stringify(sr)); }
  if (buf.some((b) => b !== 0xa5)) { fail(name + ": SaveMessages wrote into the caller's buffer"); }
  if (good === null) { continue; }
  const q = lawful();
  good(q);
  const size = ProbeMeasureMessages([q], {});
  if (size < 0) { fail(name + ": the lawful twin was refused by MeasureMessages"); continue; }
  const out = new Uint8Array(size);
  if (ProbeSaveMessages([q], out, {}) !== size) { fail(name + ": the lawful twin was refused by SaveMessages"); continue; }
  const back = [new Probe()];
  const lr = {};
  if (ProbeLoadMessages(back, v, out, lr) !== 1) { fail(name + ": the lawful twin did not read back " + JSON.stringify(lr)); }
}
`
	probe += probeTail()
	out, err := jsRunMessageProbe(t, node, u, map[string][]byte{
		"ann.bin": tablewire.Announce(u),
	}, probe)
	if err != nil {
		t.Fatalf("the message writer did not hold §5.1: %v\n%s", err, out)
	}
}
