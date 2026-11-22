package jstable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestJsWriterRefusesMoreThan4096Ids holds the JavaScript variable-form
// writer to the id table's maximum (docs/SPEC-TABLES.md §3, ruling R8): every
// reader refuses an id table of more than 4096 entries, so RootSave answers
// null and RootMeasureReason [-1, "invalid_value"] for a value that needs
// 4097, and the values that need 4095 and 4096 are written, carry that count
// word, and read back. The unit is tablewire.IdCapSchema.
func TestJsWriterRefusesMoreThan4096Ids(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, tablewire.IdCapSchema())
	jsDiscardRun(t, u, nil, `
function value(x, y) {
  const v = new m.Root();
  v.keys = new globalThis.Array(v.keys.length).fill(1);
  v.x = x; v.y = y;
  return v;
}
function count(b) {
  let n = 0;
  for (let i = 7; i >= 0; i--) { n = n * 256 + b[b.length - 8 + i]; }
  return n;
}
for (const [x, y, ids] of [[0, 0, 4095], [1, 0, 4096]]) {
  const b = m.RootSave(value(x, y));
  if (b === null) { fail(ids + " ids: save refused"); continue; }
  if (count(b) !== ids) { fail(ids + " ids: count word " + count(b)); }
  const r = m.RootLoad(b);
  if (r === null || (Array.isArray(r) && r[0] === null)) { fail(ids + " ids: read refused"); }
}
const over = value(1, 1);
if (m.RootSave(over) !== null) { fail("4097 ids: save wrote a file"); }
const why = m.RootMeasureReason(over);
if (!Array.isArray(why) || why[0] !== -1 || why[1] !== "invalid_value") { fail("4097 ids: measure answered " + JSON.stringify(why)); }
`)
}
