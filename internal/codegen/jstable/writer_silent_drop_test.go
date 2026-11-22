package jstable

// NO JAVASCRIPT WRITER SILENTLY DROPS A HELD VALUE (docs/SPEC.md §5.1: no leg
// silently writes a value off the contract; JavaScript refuses in every
// build). Three holes the 2026-11-22 silent-failure audit found, each run
// under node against the generated module:
//
//   - SF-19: SaveMessages masked a flags value to its declared width W, so a
//     bit at or above W, or a negative BigInt, was dropped and the save
//     succeeded (docs/SPEC-TABLES.md §3.3, "the write refuses it rather than
//     drop it"; docs/rulings.json message-flags-width-is-caller-error).
//   - SF-20: FixedSave and SaveMessages wrote the first N elements of a [N]T
//     (and an enum-keyed array) whose length was more than N, dropping the
//     rest; the variable Save refuses it (TableVariableArrayValid: exactly N).
//   - SF-21: FixedSave accepted a counted array's count below its Min (§5.1
//     row 3).
//   - SF-38: FixedFromText passed over a field that is not array-like and left
//     the fresh defaults, so the writer wrote a value nobody held.
//
// Each refusal answers -1 before a byte is written; each lawful twin saves.
// Unit-test rules: no real time, no socket, no network.

import (
	"strings"
	"testing"
)

const jsSilentDropSchema = `package probe

flags Caps { Read, Write, Exec }

enum Key { A, B }

fixed table Row
{
    caps Caps
    trio [3]int32
    keyed [Key]int32
    items [2..4]int32
}
`

const jsSilentDropProbe = `import * as m from "./ProbeTable.js";

let failures = 0;
function fail(why) { console.error("FAIL: " + why); failures++; }

function fresh() {
  const v = new m.Row();
  m.InitRow(v);
  v.itemsCount = 2;
  return v;
}

function fixedSave(v) {
  const bytes = new Uint8Array(m.RowFixedMeasure(1) + 16);
  bytes.fill(0xa5);
  let got;
  try { got = m.RowFixedSave([v], 1, bytes); } catch (e) { return "threw " + e.name; }
  if (got === -1) {
    for (let i = 0; i < bytes.length; i++) { if (bytes[i] !== 0xa5) { return "refused but changed byte " + i; } }
  }
  return got;
}

function messageSave(v) {
  const r = {};
  let n, s;
  try {
    n = m.RowMeasureMessages([v], r);
    s = m.RowSaveMessages([v], new Uint8Array(256), {});
  } catch (e) { return "threw " + e.name; }
  if (n === -1 && s === -1) { return -1; }
  if (n === -1 || s === -1) { return "measure " + n + " save " + s + " disagree"; }
  return s;
}

function refuses(label, form, mutate) {
  const v = fresh();
  mutate(v);
  const got = form === "fixed" ? fixedSave(v) : messageSave(v);
  if (got !== -1) { fail(form + ": " + label + " was not refused (" + got + ")"); }
}

function saves(label, form, mutate) {
  const v = fresh();
  mutate(v);
  const got = form === "fixed" ? fixedSave(v) : messageSave(v);
  if (typeof got !== "number" || got < 0) { fail(form + ": " + label + " was refused (" + got + ")"); }
}

// SF-19: the message form's flags width.
refuses("flags bit 3", "message", (v) => { v.caps = 8n; });
refuses("flags top bit", "message", (v) => { v.caps = 1n << 63n; });
refuses("negative flags", "message", (v) => { v.caps = -1n; });
saves("every declared flags bit", "message", (v) => { v.caps = 7n; });

// SF-20: a [N]T and an enum-keyed array hold exactly N, in both writers.
for (const form of ["fixed", "message"]) {
  refuses("[3]T holding 4", form, (v) => { v.trio = [1, 2, 3, 4]; });
  refuses("[3]T holding 2", form, (v) => { v.trio = [1, 2]; });
  refuses("[Key]T holding 3", form, (v) => { v.keyed = [1, 2, 3]; });
  saves("[3]T holding 3 and [Key]T holding 2", form, (v) => { v.trio = [1, 2, 3]; v.keyed = [4, 5]; });
}

// SF-21: FixedSave holds a counted array's count to its Min.
refuses("count below Min", "fixed", (v) => { v.itemsCount = 1; });
refuses("count zero below Min", "fixed", (v) => { v.itemsCount = 0; });
saves("count at Min", "fixed", (v) => { v.itemsCount = 2; });
refuses("count below Min", "message", (v) => { v.itemsCount = 1; });

// SF-38: a text-placed value whose array field is not array-like (a number, a
// string, an object with no length) is carried as held and refused by the check,
// never replaced by the fresh defaults.
for (const [label, src] of [
  ["a [3]T holding a number", { trio: 5 }],
  ["a [Key]T holding an object with no length", { keyed: { A: 1, B: 2 } }],
  ["a [2..4]T holding a string", { items: "xy" }],
]) {
  const dst = fresh();
  m.RowFixedFromText(src, dst);
  if (m.RowFixedCheckBody(dst) !== false) { fail("FixedFromText: " + label + " was not refused"); }
}
{
  const dst = fresh();
  m.RowFixedFromText({ trio: [1, 2, 3], keyed: [4, 5], items: [7, 8] }, dst);
  if (m.RowFixedCheckBody(dst) !== true) { fail("FixedFromText: a lawful text value was refused"); }
}

if (failures !== 0) { process.exit(1); }
console.log("ok");
`

func TestJsWritersRefuseSilentDrops(t *testing.T) {
	t.Parallel()
	out, err := jsRunMessageProbe(t, jsNode(t), unitFrom(t, jsSilentDropSchema), nil, jsSilentDropProbe)
	if err != nil || strings.TrimSpace(string(out)) != "ok" {
		t.Fatalf("a JavaScript writer silently dropped a held value: %v\n%s", err, out)
	}
}
