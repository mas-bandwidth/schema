package jstable

// EVERY JAVASCRIPT TABLE READ REFUSES A COUNT BELOW ITS MINIMUM (SF-01;
// docs/SPEC-TABLES.md section 22.2: a reader meeting a count below Min refuses
// count_over_bound in EVERY form). The 2026-11-22 audit found the variable
// binary read placing a [A..B] array of fewer than A elements, and the fixed
// read (the check pass and the decode) taking a count below A. Each case runs
// under node against the generated module; each lawful twin (a count at A and
// at B) still reads.
//
// Unit-test rules: no real time, no socket, no network.

import (
	"strings"
	"testing"
)

const jsReaderCountMinSchema = `package probe

fixed table Row
{
    items [2..4]int32
}

table Var
{
    items [2..4]int32
}
`

const jsReaderCountMinProbe = `import * as m from "./ProbeTable.js";
import * as v from "./ProbeVariable.js";

let failures = 0;
function fail(why) { console.error("FAIL: " + why); failures++; }

// ---- the variable binary read ----
function varWire(n) {
  const x = new v.Var();
  x.items = Array.from({ length: n }, (_, i) => i + 1);
  return v.VarSave(x);
}
// the same wire with its array body cut to k elements: L and the count are
// rewritten, the elements past k removed, so the one rule it breaks is A.
function trimmed(n, k) {
  const w = Array.from(varWire(n));
  const len = 2 + 4 * n;
  let at = -1;
  for (let i = 0; i + 1 + len <= w.length; i++) {
    if (w[i] === len && w[i + 2] === n && w[i + 3] === 1 && w[i + 4] === 0) { at = i; break; }
  }
  if (at < 0) { fail("the array body was not found in the wire"); return new Uint8Array(0); }
  const ek = w[at + 1];
  const cut = [ek, k];
  for (let i = 0; i < k; i++) { cut.push(i + 1, 0, 0, 0); }
  return Uint8Array.from([...w.slice(0, at), cut.length, ...cut, ...w.slice(at + 1 + len)]);
}
function varLoad(bytes) {
  const r = new v.TableVariableReport();
  const got = v.VarLoad(bytes, r);
  return { got, r };
}
for (const k of [0, 1]) {
  const { got, r } = varLoad(trimmed(4, k));
  if (got !== null && got !== undefined) { fail("variable read: a count of " + k + " below Min 2 was placed"); }
  if (r.refused !== 1 || !String(r.reason).startsWith("count_over_bound")) {
    fail("variable read: count " + k + " was not refused count_over_bound (refused " + r.refused + ", damaged " + r.damaged + ", reason " + r.reason + ")");
  }
}
for (const k of [2, 3, 4]) {
  const { got, r } = varLoad(trimmed(4, k));
  if (!got || r.refused || r.damaged || got.items.length !== k) { fail("variable read: a lawful count of " + k + " was refused"); }
}

// ---- the fixed read ----
function fixedBytes(count) {
  const x = new m.Row();
  m.InitRow(x);
  x.itemsCount = count;
  const bytes = new Uint8Array(m.RowFixedMeasure(1));
  if (m.RowFixedSave([x], 1, bytes) < 0) { fail("the fixed save refused a lawful count " + count); }
  return bytes;
}
// where the count sits: the one int32 lane that differs between counts 2 and 3
function countAt() {
  const a = fixedBytes(2), b = fixedBytes(3);
  for (let i = 0; i < a.length; i++) { if (a[i] !== b[i] && a[i] === 2 && b[i] === 3) { return i; } }
  fail("the count lane was not found"); return -1;
}
function fixedLoad(bytes) {
  const out = [new m.Row()];
  m.InitRow(out[0]);
  const r = new m.TableFixedReport();
  const n = m.RowFixedLoad(out, 1, bytes, bytes.length, m.RowFixedNewPlan(64, 64), r);
  return { n, r };
}
const at = countAt();
for (const k of [0, 1]) {
  const b = fixedBytes(2);
  b[at] = k;
  const { n, r } = fixedLoad(b);
  if (n >= 0 || r.refused !== m.TableFixedRefusal.CountOverBound) {
    fail("fixed read: a count of " + k + " below Min 2 was not refused count_over_bound (got " + n + ", refused " + r.refused + ", damaged " + r.damaged + ")");
  }
}
for (const k of [2, 3, 4]) {
  const b = fixedBytes(k);
  const { n, r } = fixedLoad(b);
  if (n !== 1 || r.refused || r.damaged) { fail("fixed read: a lawful count of " + k + " was refused (" + n + ")"); }
}

if (failures !== 0) { process.exit(1); }
console.log("ok");
`

func TestJsReadersRefuseCountBelowMin(t *testing.T) {
	t.Parallel()
	out, err := jsRunMessageProbe(t, jsNode(t), unitFrom(t, jsReaderCountMinSchema), nil, jsReaderCountMinProbe)
	if err != nil || strings.TrimSpace(string(out)) != "ok" {
		t.Fatalf("a JavaScript table read took a count below its minimum: %v\n%s", err, out)
	}
}
