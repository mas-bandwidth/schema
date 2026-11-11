package jstable

import "testing"

// TestJsVariableSaveRefusesOffContract holds the JavaScript variable-form
// writer to the write contract (docs/SPEC.md §5.1): Save answers null and
// MeasureReason [-1, "invalid_value"] for a value its declaration does not
// admit, before any byte is produced, and never throws. It is checked at the
// top level and inside a nested table, with the boundary values written and
// round-tripped through Load.
func TestJsVariableSaveRefusesOffContract(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, jsVariableSaveOffContractSchema)
	jsDiscardRun(t, u, nil, jsVariableSaveOffContractProbe)
}

const jsVariableSaveOffContractSchema = `package saveprobe

flags Mask { A, B }

union Pick
{
    number int32
    text   string(4)
    truth  bool
}

table Inner
{
    ranged int32 | min = -5, max = 5
    nib    bits(6)
    enabled bool
    mandatory bool | required
    truthy  [..2]bool
    wide   uint64
    ival int32
    list   [..3]int32
    least  [2..3]int32
    label  string(4)
    blob   bytes(4)
    note   *string
    choice Pick
    ratio  float32 | min = 0, max = 10, resolution = 0.01
    mask   Mask
    scores map[int32]int32
}

table Root
{
    inner Inner
}
`

const jsVariableSaveOffContractProbe = `
function attempt(fn) {
  try { return fn(); } catch (e) { return "threw " + (e && e.name ? e.name : e); }
}

function innerLow() {
  const v = new m.Inner();
  v.ranged = -5;
  v.nib = 0;
  v.enabled = false;
  v.mandatory = false;
  v.truthy = [];
  v.wide = 0n;
  v.ival = -2147483648;
  v.list = [];
  v.least = [1, 2];
  v.label = "";
  v.blob = new Uint8Array([]);
  v.note = "";
  v.choice = { arm: "truth", value: false };
  v.ratio = 0;
  v.mask = 0n;
  v.scores = new Map();
  return v;
}

function innerHigh() {
  const v = new m.Inner();
  v.ranged = 5;
  v.nib = 63;
  v.enabled = true;
  v.mandatory = true;
  v.truthy = [false, true];
  v.wide = 18446744073709551615n;
  v.ival = 2147483647;
  v.list = [1, 2, 3];
  v.least = [1, 2, 3];
  v.label = "abcd";
  v.blob = new Uint8Array([1, 2, 3, 4]);
  v.note = "abcd";
  v.choice = { arm: "truth", value: true };
  v.ratio = 10;
  v.mask = 3n;
  v.scores = new Map([[1, 2], [3, 4]]);
  return v;
}

function invalidCases() {
  const at = (field, value) => { const v = innerHigh(); v[field] = value; return v; };
  return [
    ["ranged below min", at("ranged", -6)],
    ["ranged above max", at("ranged", 6)],
    ["bits past width", at("nib", 64)],
    ["bool as a string", at("enabled", "true")],
    ["required bool as a string", at("mandatory", "true")],
    ["array bool as a string", at("truthy", [true, "false"])],
    ["uint64 as a Number", at("wide", 5)],
    ["uint64 above its width", at("wide", 18446744073709551616n)],
    ["int32 fractional", at("ival", 1.5)],
    ["int32 above its width", at("ival", 2147483648)],
    ["int32 as a string", at("ival", "5")],
    ["counted array past Max", at("list", [1, 2, 3, 4])],
    ["counted array below Min", at("least", [1])],
    ["counted array past Max 2", at("least", [1, 2, 3, 4])],
    ["string past N", at("label", "abcde")],
    ["string with a lone surrogate", at("label", "\uD800")],
    ["string with a zero", at("label", "a\u0000")],
    ["bytes past N", at("blob", new Uint8Array(5))],
    ["bytes not a byte buffer", at("blob", [1, 2, 3, 4])],
    ["*string with a lone surrogate", at("note", "\uD800")],
    ["*string with a zero", at("note", "\u0000")],
    ["*string not text", at("note", 5)],
    ["union tag outside its arms", at("choice", { arm: "missing", value: 1 })],
    ["union bool as a string", at("choice", { arm: "truth", value: "true" })],
    ["compressed float below its range", at("ratio", -0.5)],
    ["compressed float above its range", at("ratio", 10.5)],
    ["flags as a Number", at("mask", 1)],
    ["flags below its width", at("mask", -1n)],
    ["map not the module map", at("scores", [])],
  ];
}

for (const [name, value] of invalidCases()) {
  let save = attempt(() => m.InnerSave(value));
  let why = attempt(() => m.InnerMeasureReason(value));
  if (save !== null) { fail(name + " (top): Save answered " + save); }
  if (!Array.isArray(why) || why[0] !== -1 || why[1] !== "invalid_value") {
    fail(name + " (top): MeasureReason answered " + JSON.stringify(why));
  }
  const root = new m.Root();
  root.inner = value;
  save = attempt(() => m.RootSave(root));
  why = attempt(() => m.RootMeasureReason(root));
  if (save !== null) { fail(name + " (nested): Save answered " + save); }
  if (!Array.isArray(why) || why[0] !== -1 || why[1] !== "invalid_value") {
    fail(name + " (nested): MeasureReason answered " + JSON.stringify(why));
  }
}

function sameBytes(a, b) {
  if (!(a instanceof Uint8Array) || !(b instanceof Uint8Array) || a.length !== b.length) { return false; }
  for (let i = 0; i < a.length; i++) { if (a[i] !== b[i]) { return false; } }
  return true;
}

function same(a, b) {
  if (typeof a === "bigint" || typeof b === "bigint") { return a === b; }
  if (a instanceof Uint8Array || b instanceof Uint8Array) { return sameBytes(a, b); }
  if (a instanceof Map || b instanceof Map) {
    if (!(a instanceof Map) || !(b instanceof Map) || a.size !== b.size) { return false; }
    for (const [k, v] of a) { if (!b.has(k) || !same(v, b.get(k))) { return false; } }
    return true;
  }
  if (Array.isArray(a) || Array.isArray(b)) {
    if (!Array.isArray(a) || !Array.isArray(b) || a.length !== b.length) { return false; }
    for (let i = 0; i < a.length; i++) { if (!same(a[i], b[i])) { return false; } }
    return true;
  }
  if (a !== null && b !== null && typeof a === "object" && typeof b === "object") {
    const ka = Object.keys(a), kb = Object.keys(b);
    if (ka.length !== kb.length) { return false; }
    for (const k of ka) { if (!Object.prototype.hasOwnProperty.call(b, k) || !same(a[k], b[k])) { return false; } }
    return true;
  }
  return Object.is(a, b);
}

function boundary(name, value) {
  let wire = attempt(() => m.InnerSave(value));
  if (wire === null || typeof wire === "string") { fail(name + " (top): Save refused a boundary value: " + wire); return; }
  let back = attempt(() => m.InnerLoad(wire, new m.TableVariableReport()));
  if (back === null || typeof back === "string") { fail(name + " (top): Load refused its boundary: " + back); return; }
  if (!same(value, back)) { fail(name + " (top): boundary changed through Load"); }
  const root = new m.Root();
  root.inner = value;
  wire = attempt(() => m.RootSave(root));
  if (wire === null || typeof wire === "string") { fail(name + " (nested): Save refused a boundary value: " + wire); return; }
  back = attempt(() => m.RootLoad(wire, new m.TableVariableReport()));
  if (back === null || typeof back === "string" || back.inner === null) { fail(name + " (nested): Load refused its boundary: " + back); return; }
  if (!same(value, back.inner)) { fail(name + " (nested): boundary changed through Load"); }
}

boundary("low", innerLow());
boundary("high", innerHigh());
`
