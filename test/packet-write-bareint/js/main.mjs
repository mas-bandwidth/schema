// The packet-write bare-integer driver for the JavaScript leg (docs/SPEC.md
// §5.1). It writes every case of the shared list (../cases.txt, BareInt.schema)
// through the runtime tier and the flat tier and requires each refused case
// refused and each lawful case written and read back exactly. JavaScript
// storage holds any value, so the writer is the check: a refusal in every
// build. A width of 32 bits or fewer takes a Number that is an integer; a
// wider one (int64, uint64, uint128) takes a BigInt only. The driver adds
// this leg's own cases beside the shared list: a value of the wrong type
// for its width (a Number where a BigInt is required, a numeric string, a
// boolean, null, NaN, a fraction, an object with valueOf), uint128 and the
// wide boundaries the list leaves out, and the same check reached through an
// array element and a union arm payload (Shapes.schema). The gate runs this
// file under both NODE_ENV settings.
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

// This leg's own cases: storage holds values no integer type can.
const ownCases = [
  { name: "int8-fraction", type: "I8", value: 1.5, written: false },
  { name: "uint32-fraction", type: "U32", value: 1.5, written: false },
  { name: "uint16-nan", type: "U16", value: NaN, written: false },
  { name: "int32-infinity", type: "I32", value: Infinity, written: false },
  { name: "int32-numeric-string", type: "I32", value: "5", written: false },
  { name: "uint8-boolean", type: "U8", value: true, written: false },
  { name: "uint16-null", type: "U16", value: null, written: false },
  { name: "int16-bigint", type: "I16", value: 5n, written: false },
  { name: "uint32-valueof-object", type: "U32", value: { valueOf() { return 5; } }, written: false },
  { name: "int64-below-min", type: "I64", value: -(1n << 63n) - 1n, written: false },
  { name: "uint64-min", type: "U64", value: 0n, written: true },
  { name: "uint128-min", type: "U128", value: 0n, written: true },
  { name: "uint128-max", type: "U128", value: (1n << 128n) - 1n, written: true },
  { name: "uint128-above-max", type: "U128", value: 1n << 128n, written: false },
  { name: "uint128-negative", type: "U128", value: -1n, written: false },
];

// A wide width takes a BigInt only: every other value is refused, in range
// or not, before the range test could compare it with a BigInt bound.
const wideAboveMaxNumber = { I64: 2 ** 64, U64: 2 ** 65, U128: 2 ** 129 };
for (const [type, prefix] of [["I64", "int64"], ["U64", "uint64"], ["U128", "uint128"]]) {
  ownCases.push(
    { name: prefix + "-number-in-range", type, value: 5, written: false },
    { name: prefix + "-number-zero", type, value: 0, written: false },
    { name: prefix + "-number-out-of-range", type, value: wideAboveMaxNumber[type], written: false },
    { name: prefix + "-numeric-string", type, value: "5", written: false },
    { name: prefix + "-empty-string", type, value: "", written: false },
    { name: prefix + "-true", type, value: true, written: false },
    { name: prefix + "-false", type, value: false, written: false },
    { name: prefix + "-null", type, value: null, written: false },
    { name: prefix + "-undefined", type, value: undefined, written: false },
    { name: prefix + "-nan", type, value: NaN, written: false },
    { name: prefix + "-fraction", type, value: 1.5, written: false },
    { name: prefix + "-valueof-object", type, value: { valueOf() { return 5n; } }, written: false },
  );
}

// The shared list's types live in BareInt.schema; U128 lives in Shapes.schema.
function modulesOf(type) {
  return type === "U128" ? { rt: shapes, fl: shapesFlat } : { rt: runtime, fl: flat };
}

function writeRuntime(type, value) {
  const stream = new WriteStream(new Uint8Array(64));
  const { rt } = modulesOf(type);
  const input = new rt[type]();
  input.value = value;
  if (!rt["Write" + type](stream, input)) return null;
  stream.flush();
  return { bytes: stream.data().slice(), bits: stream.bitsProcessed() };
}

function readRuntime(type, wire) {
  const stream = new ReadStream(wire.bytes);
  const { rt } = modulesOf(type);
  const output = new rt[type]();
  if (!rt["Read" + type](stream, output)) return null;
  return output.value;
}

function writeFlat(type, value) {
  const view = new DataView(new ArrayBuffer(64));
  const { fl } = modulesOf(type);
  const size = fl["Write" + type + "Flat"](view, { value });
  if (size < 0) return null;
  return { bytes: new Uint8Array(view.buffer.slice(0, size)), bits: size * 8 };
}

function readFlat(type, wire) {
  const { rt, fl } = modulesOf(type);
  const buffer = new Uint8Array(wire.bytes.length + fl.FlatReadSlack);
  buffer.set(wire.bytes);
  const output = new rt[type]();
  if (!fl["Read" + type + "Flat"](new DataView(buffer.buffer), wire.bits, output)) return null;
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
  { name: "array-int64-element-number", type: "Elems", make: () => { const e = lawfulElems(); e.wide[1] = 5; return e; } },
  { name: "array-int64-element-numeric-string", type: "Elems", make: () => { const e = lawfulElems(); e.wide[1] = "5"; return e; } },
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
