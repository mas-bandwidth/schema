// The packet-write bare-integer driver for the JavaScript leg (docs/SPEC.md
// §5.1). It writes every case of the shared list (../cases.txt, BareInt.schema)
// through the runtime tier and the flat tier and requires each refused case
// refused and each lawful case written and read back exactly. JavaScript
// storage holds any Number or BigInt, so the writer is the check: a refusal
// in every build. The driver adds this leg's own cases beside the shared
// list: a Number that is not an integer, and the same check reached through
// an array element and a union arm payload (Shapes.schema). The gate runs
// this file under both NODE_ENV settings.
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
import { WriteStream, ReadStream } from "../../../../serialize.js/src/index.js";

const casesURL = process.argv[2]
  ? pathToFileURL(process.argv[2])
  : new URL("../cases.txt", import.meta.url);
const generated = process.argv[3]
  ? pathToFileURL(process.argv[3] + "/")
  : new URL("../../../build/packet-write-bareint/js/", import.meta.url);

const runtime = await import(new URL("bareint/BareInt.js", generated));
const flat = await import(new URL("bareint/BareIntFlat.js", generated));
const shapes = await import(new URL("shapes/Shapes.js", generated));
const shapesFlat = await import(new URL("shapes/ShapesFlat.js", generated));
const mode = process.env.NODE_ENV === "production" ? "production" : "checked";

let failures = 0;
function fail(message) {
  failures++;
  console.error(`FAILED: bareint ${mode}/${message}`);
}

const wide = new Set(["I64", "U64"]);

// One case per line: <name> <type> <value> <verdict>.
function readCases() {
  const cases = [];
  for (const raw of readFileSync(casesURL, "utf8").split("\n")) {
    const line = raw.trim();
    if (line === "" || line.startsWith("#")) continue;
    const f = line.split(/\s+/);
    cases.push({
      name: f[0],
      type: f[1],
      value: wide.has(f[1]) ? BigInt(f[2]) : Number(f[2]),
      written: f[3] === "written",
    });
  }
  return cases;
}

// This leg's own cases: Number storage holds values no integer type can.
const ownCases = [
  { name: "int8-fraction", type: "I8", value: 1.5, written: false },
  { name: "uint32-fraction", type: "U32", value: 1.5, written: false },
  { name: "uint16-nan", type: "U16", value: NaN, written: false },
  { name: "int32-infinity", type: "I32", value: Infinity, written: false },
];

function writeRuntime(type, value) {
  const stream = new WriteStream(new Uint8Array(64));
  const input = new runtime[type]();
  input.value = value;
  if (!runtime["Write" + type](stream, input)) return null;
  stream.flush();
  return { bytes: stream.data().slice(), bits: stream.bitsProcessed() };
}

function readRuntime(type, wire) {
  const stream = new ReadStream(wire.bytes);
  const output = new runtime[type]();
  if (!runtime["Read" + type](stream, output)) return null;
  return output.value;
}

function writeFlat(type, value) {
  const view = new DataView(new ArrayBuffer(64));
  const size = flat["Write" + type + "Flat"](view, { value });
  if (size < 0) return null;
  return { bytes: new Uint8Array(view.buffer.slice(0, size)), bits: size * 8 };
}

function readFlat(type, wire) {
  const buffer = new Uint8Array(wire.bytes.length + flat.FlatReadSlack);
  buffer.set(wire.bytes);
  const output = new runtime[type]();
  if (!flat["Read" + type + "Flat"](new DataView(buffer.buffer), wire.bits, output)) return null;
  return output.value;
}

const tiers = [
  { name: "runtime", write: writeRuntime, read: readRuntime },
  { name: "flat", write: writeFlat, read: readFlat },
];

for (const c of [...readCases(), ...ownCases]) {
  for (const tier of tiers) {
    let wire;
    try {
      wire = tier.write(c.type, c.value);
    } catch (err) {
      fail(`${tier.name} ${c.name}: threw ${err.name}: ${err.message}`);
      continue;
    }
    if (!c.written) {
      if (wire !== null) fail(`${tier.name} ${c.name}: wrote a value its width refuses`);
      continue;
    }
    if (wire === null) {
      fail(`${tier.name} ${c.name}: refused a lawful value`);
      continue;
    }
    let got;
    try {
      got = tier.read(c.type, wire);
    } catch (err) {
      fail(`${tier.name} ${c.name}: threw on the read-back ${err.name}: ${err.message}`);
      continue;
    }
    if (got === null) {
      fail(`${tier.name} ${c.name}: the reader refused the writer's bytes`);
    } else if (got !== c.value) {
      fail(`${tier.name} ${c.name}: read back ${got}, want ${c.value}`);
    }
  }
}

// The same check through an array element and a union arm payload. Each
// shape is lawful as built; one value is then moved off its width.
function lawfulElems() {
  const e = new shapes.Elems();
  e.values = [-32768, 0, 32767];
  e.counts = [4294967295, 0];
  e.countsCount = 2;
  e.wide = [-(1n << 63n), (1n << 63n) - 1n];
  return e;
}
function lawfulHolder() {
  const h = new shapes.Holder();
  h.choice.type = shapes.ChoiceType.Arm;
  h.choice.arm.value = -128;
  return h;
}
const shapeCases = [
  { name: "array-int16-element-above-max", type: "Elems", make: () => { const e = lawfulElems(); e.values[2] = 32768; return e; } },
  { name: "array-uint32-element-negative", type: "Elems", make: () => { const e = lawfulElems(); e.counts[1] = -1; return e; } },
  { name: "array-int64-element-above-max", type: "Elems", make: () => { const e = lawfulElems(); e.wide[1] = 1n << 63n; return e; } },
  { name: "arm-int8-payload-above-max", type: "Holder", make: () => { const h = lawfulHolder(); h.choice.arm.value = 300; return h; } },
];

function shapeRuntime(type, value) {
  const stream = new WriteStream(new Uint8Array(256));
  if (!shapes["Write" + type](stream, value)) return false;
  stream.flush();
  return true;
}
function shapeFlat(type, value) {
  return shapesFlat["Write" + type + "Flat"](new DataView(new ArrayBuffer(256)), value) >= 0;
}
const shapeTiers = [
  { name: "runtime", write: shapeRuntime },
  { name: "flat", write: shapeFlat },
];

for (const tier of shapeTiers) {
  for (const [name, type, make] of [["array-lawful", "Elems", lawfulElems], ["arm-lawful", "Holder", lawfulHolder]]) {
    try {
      if (!tier.write(type, make())) fail(`${tier.name} ${name}: refused a lawful value`);
    } catch (err) {
      fail(`${tier.name} ${name}: threw ${err.name}: ${err.message}`);
    }
  }
  for (const c of shapeCases) {
    try {
      if (tier.write(c.type, c.make())) fail(`${tier.name} ${c.name}: wrote a value its width refuses`);
    } catch (err) {
      fail(`${tier.name} ${c.name}: threw ${err.name}: ${err.message}`);
    }
  }
}

if (failures !== 0) process.exit(1);
console.log(`packet write bare integer JavaScript: ${mode} build, every case holds in the runtime and flat tiers`);
