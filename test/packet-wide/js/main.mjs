import assert from "node:assert/strict";
import { createInterface } from "node:readline";
import { pathToFileURL } from "node:url";
import { ReadStream, WriteStream } from "../../../../serialize.js/src/index.js";
const tier = process.argv[2];
assert(tier === "runtime" || tier === "flat");
const contracts = process.argv[3] === "--contracts";
const base = process.argv[3] && !contracts ? pathToFileURL(process.argv[3] + "/") : new URL("../../../build/packet-wide/js/", import.meta.url);
const d = await import(new URL("WideText.js", base));
const f = await import(new URL("WideTextFlat.js", base));
const hex = bytes => bytes.length ? Buffer.from(bytes).toString("hex") : "-";
function encode(v, name, mod = d, flat = f) {
  const w = new WriteStream(new Uint8Array(1024));
  assert(mod[`Write${name}`](w, v));
  const bits = w.bitsProcessed(); w.flush();
  let bytes = w.data();
  if (tier === "flat") {
    const buffer = new Uint8Array(1024);
    const count = flat[`Write${name}Flat`](new DataView(buffer.buffer), v);
    assert.equal(count, Math.ceil(bits / 8));
    bytes = buffer.subarray(0, count);
    assert.deepEqual(bytes, w.data());
  }
  return { bytes, bits };
}
function decode(v, name, bytes, bits = bytes.length * 8, mod = d, flat = f) {
  if (tier === "runtime") return mod[`Read${name}`](new ReadStream(bytes), v);
  const padded = new Uint8Array(bytes.length + 8); padded.set(bytes);
  return flat[`Read${name}Flat`](new DataView(padded.buffer), bits, v);
}
// WRITE CONTRACTS ARE THE CHECKED BUILD'S (docs/CONTRIBUTING.md rule 4,
// SPEC.md's checked/production fork): the flat writer's wstring length check
// is emitted through the debug guard, so only NODE_ENV=development holds it to
// -1; production compiles the check out and is not asked.
const checked = process.env.NODE_ENV !== "production";
if (contracts) {
  const v = new d.WideSeven();
  for (const n of [-1, 8, 1, 0.5, NaN]) {
    v.textLength = n;
    assert.equal(d.WriteWideSeven(new WriteStream(new Uint8Array(256)), v), false);
    if (checked) { assert.equal(f.WriteWideSevenFlat(new DataView(new ArrayBuffer(256)), v), -1); }
  }
  v.textLength = 1; v.text[0] = 0xd800;
  let { bytes, bits } = encode(v, "WideSeven");
  assert.equal(decode(v, "WideSeven", bytes, bits), false);
  v.text[0] = 0xffff;
  ({ bytes, bits } = encode(v, "WideSeven"));
  v.text.fill(0x7f7f);
  assert(decode(v, "WideSeven", bytes, bits));
  assert.deepEqual(Array.from(v.text), [0xffff, 0x7f7f, 0x7f7f, 0x7f7f, 0x7f7f, 0x7f7f, 0x7f7f]);
  const p = await import(new URL("shapes/Shapes.js", base));
  const pf = await import(new URL("shapes/ShapesFlat.js", base));
  const c = new p.Conditional(); c.enabled = true; c.textLength = 2; c.text.set([0xd800, 0xdc00]);
  ({ bytes, bits } = encode(c, "Conditional", p, pf));
  assert.equal(bits, 68); assert.equal(bytes[0] & 15, 5);
  const co = new p.Conditional();
  assert(decode(co, "Conditional", bytes, bits, p, pf)); assert.deepEqual(co, c);
  c.enabled = false; ({ bytes, bits } = encode(c, "Conditional", p, pf));
  assert(decode(co, "Conditional", bytes, bits, p, pf)); assert.deepEqual(co, new p.Conditional());
  const box = new p.Box(); assert.equal(box.countedCount, 1);
  box.items[0].valueLength = 1; box.items[0].value[0] = 0xffff;
  box.counted[0].valueLength = 1; box.counted[0].value[0] = 65;
  box.choice.type = p.ChoiceType.Text; box.choice.text.valueLength = 1; box.choice.text.value[0] = 66;
  ({ bytes, bits } = encode(box, "Box", p, pf));
  const bo = new p.Box(); bo.counted[1].value[0] = 0x7f7f;
  for (let i = 0; i < 2; i++) {
    bo.choice.text.value[3] = 0x7f7f;
    assert(decode(bo, "Box", bytes, bits, p, pf));
    assert.deepEqual(bo.items, box.items); assert.deepEqual(bo.counted[0], box.counted[0]);
    assert.equal(bo.countedCount, 1); assert.equal(bo.counted[1].value[0], 0x7f7f);
    assert.deepEqual(bo.choice.text, box.choice.text);
  }
  if (tier === "flat") assert.equal(decode(bo, "Box", bytes, bits - 1, p, pf), false);
} else for await (const line of createInterface({ input: process.stdin, crlfDelay: Infinity })) {
  const [bound, raw] = line.split(" ");
  const name = bound === "7" ? "WideSeven" : bound === "4" ? "WideFour" : null;
  assert(name);
  const wire = Uint8Array.from(Buffer.from(raw === "-" ? "" : raw, "hex"));
  const value = new d[name](); value.text.fill(0x7f7f);
  if (!decode(value, name, wire)) { console.log("REFUSE"); continue; }
  const { bytes, bits } = encode(value, name);
  const payload = Array.from(value.text.subarray(0, value.textLength), u => u.toString(16).padStart(4, "0")).join("") || "-";
  if (tier === "flat") {
    assert(decode(new d[name](), name, wire, bits));
    assert.equal(decode(new d[name](), name, wire, bits - 1), false);
  } else {
    const r = new ReadStream(wire); assert(d[`Read${name}`](r, value)); assert.equal(r.bitsProcessed(), bits);
  }
  console.log(`OK ${bits} ${payload} ${bits} ${hex(bytes)}`);
}
