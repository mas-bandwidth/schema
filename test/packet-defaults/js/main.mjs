import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
import { WriteStream, ReadStream } from "../../../../serialize.js/src/index.js";
import * as p from "../../../build/packet-defaults/js/plain/Plain.js";
const dir = process.argv[2];
const base = process.argv[3] ? pathToFileURL(process.argv[3] + "/") : new URL("../../../build/packet-defaults/js/defaults/", import.meta.url);
const d = await import(new URL("Defaults.js", base));
const flat = await import(new URL("DefaultsFlat.js", base));
const zero = () => Object.assign(new d.Sample(), {
  Name: new Uint8Array(6), NameLength: 0, Token: new Uint8Array(4), TokenLength: 0, Caps: 0n,
  EmptyName: new Uint8Array(3), EmptyNameLength: 0, EmptyToken: new Uint8Array(2), EmptyTokenLength: 0, EmptyCaps: 0n,
});
const sample = () => Object.assign(zero(), { Name: Uint8Array.from([195,169,240,144,128,128]), NameLength: 6,
  Token: Uint8Array.from([92,110,92,116]), TokenLength: 4, Caps: 5n });
const short = () => Object.assign(zero(), { Name: Uint8Array.from([65,0,0,0,0,0]), NameLength: 1,
  Token: Uint8Array.from([0,255,0,0]), TokenLength: 2, Caps: 2n });
const dirty = () => Object.assign(zero(), { Name: new Uint8Array(6).fill(100), NameLength: 6,
  Token: new Uint8Array(4).fill(161), TokenLength: 4, Caps: 7n, EmptyName: new Uint8Array(3).fill(111),
  EmptyNameLength: 3, EmptyToken: new Uint8Array(2).fill(177), EmptyTokenLength: 2, EmptyCaps: 7n });
function overlay(initial, sent) {
  initial.Name.set(sent.Name.subarray(0, sent.NameLength)); initial.NameLength = sent.NameLength;
  initial.Token.set(sent.Token.subarray(0, sent.TokenLength)); initial.TokenLength = sent.TokenLength;
  initial.Caps = sent.Caps; initial.EmptyNameLength = 0; initial.EmptyTokenLength = 0; initial.EmptyCaps = 0n;
  return initial;
}
function write(mod, type, value) {
  const stream = new WriteStream(new Uint8Array(4096));
  assert(mod["Write" + type](stream, value), "runtime write");
  const bits = stream.bitsProcessed(); stream.flush();
  return { bytes: stream.data().slice(), bits };
}
function flatValue(type, value) {
  return type === "Choice" ? Object.assign(new d.ChoiceEnvelope(), { Choice: value }) : value;
}
function flatType(type) { return type === "Choice" ? "ChoiceEnvelope" : type; }
function checkRead(tier, type, bytes, bits, initial, want) {
  if (tier === "runtime") {
    const stream = new ReadStream(bytes);
    assert(d["Read" + type](stream, initial), "runtime read");
    assert.equal(stream.bitsProcessed(), bits, "read consumed bits");
  } else {
    const buffer = new Uint8Array(bytes.length + 8); buffer.set(bytes);
    assert(flat["Read" + flatType(type) + "Flat"](flatValue(type, initial), new DataView(buffer.buffer), bits), "flat exact-bit read");
    assert.equal(flat["Read" + flatType(type) + "Flat"](flatValue(type, new d[type]()), new DataView(buffer.buffer), bits - 1), false, "flat one-bit-short refusal");
  }
  assert.deepEqual(initial, want, `${tier} ${type}: values and backing storage`);
}
function pin(type, name, value, initial, want) {
  const bytes = new Uint8Array(readFileSync(`${dir}/${name}.bin`));
  const bits = Number(readFileSync(`${dir}/${name}.bits`, "utf8").trim());
  assert.deepEqual(write(d, type, value), { bytes, bits }, `C++ pin ${name}`);
  const buffer = new Uint8Array(4096);
  const size = flat["Write" + flatType(type) + "Flat"](flatValue(type, value), new DataView(buffer.buffer));
  assert.deepEqual(buffer.slice(0, size), bytes, `flat C++ pin ${name}`);
  for (const tier of ["runtime", "flat"]) checkRead(tier, type, bytes, bits, initial(), want());
}

assert.deepEqual(new d.Sample(), sample(), "packet-default constructor bytes");
const reused = dirty(), nameBuffer = reused.Name, tokenBuffer = reused.Token;
d.InitSample(reused); assert.deepEqual(reused, sample());
assert.equal(reused.Name, nameBuffer); assert.equal(reused.Token, tokenBuffer);
d.ZeroSample(reused); assert.deepEqual(reused, zero());
const empty = new d.EmptyOnly();
assert.deepEqual([empty.NameLength, empty.TokenLength, empty.Caps, ...empty.Name, ...empty.Token], [0,0,0n,0,0,0]);
const prefix = new d.Prefix();
assert.deepEqual([...prefix.Name, prefix.NameLength, ...prefix.Token, prefix.TokenLength], [195,169,0,0,0,2,92,110,0,0,0,2]);
prefix.Name.fill(255); prefix.Token.fill(255); d.InitPrefix(prefix); assert.deepEqual(prefix, new d.Prefix());
const wide = new d.WideMask(); assert.equal(wide.High, 1n << 63n); assert.equal(wide.All, (1n << 64n) - 1n);
const wideBytes = new Uint8Array(16).fill(255, 8); wideBytes[7] = 128;
assert.deepEqual(write(d, "WideMask", wide), { bytes: wideBytes, bits: 128 });
const split = Object.assign(new d.SplitMask(), { Lead: 5, Mask: 1n << 32n, Tail: 2 });
assert.deepEqual(write(d, "SplitMask", split), { bytes: Uint8Array.from([5,0,0,0,40]), bits: 38 });
for (const tier of ["runtime", "flat"]) {
  checkRead(tier, "WideMask", wideBytes, 128, Object.assign(new d.WideMask(), { High: 0n, All: 0n }), wide);
  checkRead(tier, "SplitMask", Uint8Array.from([5,0,0,0,40]), 38, new d.SplitMask(), split);
}
const plain = Object.assign(new p.Sample(), sample());
assert.deepEqual(write(p, "Sample", plain), write(d, "Sample", new d.Sample()), "defaultless twin");
pin("Sample", "sample-defaults", new d.Sample(), zero, sample);
const batch = new d.Batch(); assert.equal(batch.CountedCount, 1); assert.deepEqual(batch.Head, sample());
for (const s of [...batch.Items, ...batch.Counted]) assert.deepEqual(s, sample());
function reusedBatch() { const b = new d.Batch(); b.Counted[1] = dirty(); b.Counted[2] = short(); return b; }
pin("Batch", "batch-defaults", batch, reusedBatch, reusedBatch);
const z = new d.ZeroCount(); assert.equal(z.ItemsCount, 0); assert.deepEqual(z.Items, [sample(), sample()]);
pin("ZeroCount", "zero-count", z, () => Object.assign(new d.ZeroCount(), { Items: [dirty(),short()], ItemsCount: 2 }),
  () => Object.assign(new d.ZeroCount(), { Items: [dirty(),short()], ItemsCount: 0 }));
assert.equal(new d.Conditional().Enabled, true); assert.deepEqual(new d.Conditional().Value, sample());
pin("Conditional", "conditional-on", new d.Conditional(), () => Object.assign(new d.Conditional(), { Enabled: false, Value: zero() }), () => new d.Conditional());
pin("Conditional", "conditional-off", Object.assign(new d.Conditional(), { Enabled: false }),
  () => Object.assign(new d.Conditional(), { Value: dirty() }), () => Object.assign(new d.Conditional(), { Enabled: false, Value: zero() }));
const choice = () => Object.assign(new d.Choice(), { Type: d.ChoiceType.Sample });
pin("Choice", "choice-sample", choice(), () => Object.assign(new d.Choice(), { Type: d.ChoiceType.Conditional }), choice);
pin("Sample", "sample-short", short(), dirty, () => overlay(dirty(), short()));
pin("Sample", "sample-empty", zero(), dirty, () => overlay(dirty(), zero()));
for (const sent of [short(), zero()]) {
  const value = choice(); value.Sample = sent; const wire = write(d, "Choice", value);
  for (const tier of ["runtime", "flat"]) {
    const output = Object.assign(new d.Choice(), { Type: d.ChoiceType.Conditional });
    const arm = output.Sample, name = arm.Name, token = arm.Token;
    for (let attempt = 0; attempt < 2; attempt++) {
      arm.Name.fill(100); arm.Token.fill(161); arm.EmptyName.fill(111); arm.EmptyToken.fill(177);
      const want = choice(); want.Sample = overlay(sample(), sent);
      checkRead(tier, "Choice", wire.bytes, wire.bits, output, want);
      assert.equal(output.Sample, arm); assert.equal(arm.Name, name); assert.equal(arm.Token, token);
    }
  }
}
const offChoice = Object.assign(new d.Choice(), { Type: d.ChoiceType.Conditional }); offChoice.Conditional.Enabled = false;
const offWire = write(d, "Choice", offChoice);
for (const tier of ["runtime", "flat"]) {
  const output = choice();
  for (let attempt = 0; attempt < 2; attempt++) {
    output.Conditional.Enabled = true; output.Conditional.Value = dirty();
    const want = Object.assign(new d.Choice(), { Type: d.ChoiceType.Conditional });
    want.Conditional.Enabled = false; want.Conditional.Value = zero();
    checkRead(tier, "Choice", offWire.bytes, offWire.bits, output, want);
  }
}
console.log("packet defaults JavaScript: constructors, eight C++ goldens, both tiers and reused storage OK");
