// The packet-write compressed-float driver for the JavaScript leg
// (docs/SPEC.md §5.1 row 10, docs/PACKET-WIRE-ALGORITHM.md §2.5). It writes
// every case of the shared list through the flat tier and the runtime tier
// and requires each refused case refused and each lawful case written and
// read back to its quantized value. The runtime's checked/production variant
// is selected at module load, so the gate runs this file under both NODE_ENV
// settings and the flat tier is held to the same verdicts in each.
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
import { WriteStream, ReadStream } from "../../../../serialize.js/src/index.js";

const casesURL = process.argv[2]
  ? pathToFileURL(process.argv[2])
  : new URL("../cases.txt", import.meta.url);
const generated = process.argv[3]
  ? pathToFileURL(process.argv[3] + "/")
  : new URL("../../../build/packet-write-cfloat/js/", import.meta.url);

const runtime = await import(new URL("CFloat.js", generated));
const flat = await import(new URL("CFloatFlat.js", generated));
const mode = process.env.NODE_ENV === "production" ? "production" : "checked";

let failures = 0;
function fail(message) {
  failures++;
  console.error(`FAILED: cfloat ${mode}/${message}`);
}

function parseValue(text) {
  switch (text) {
    case "NaN": return NaN;
    case "+Inf": return Infinity;
    case "-Inf": return -Infinity;
    default: return Number(text);
  }
}

// One case per line: <name> <value> <verdict> [reads-back <value>].
function readCases() {
  const cases = [];
  for (const raw of readFileSync(casesURL, "utf8").split("\n")) {
    const line = raw.trim();
    if (line === "" || line.startsWith("#")) continue;
    const f = line.split(/\s+/);
    cases.push({
      name: f[0],
      value: parseValue(f[1]),
      written: f[2] === "written",
      readsBack: f[3] === "reads-back" ? parseValue(f[4]) : undefined,
    });
  }
  return cases;
}

// Bit-exact equality, including the sign of zero: the shared list says a
// written -0 reads back +0.
function sameNumber(actual, expected) {
  if (Number.isNaN(actual) || Number.isNaN(expected)) return false;
  return actual === expected && Object.is(actual, -0) === Object.is(expected, -0);
}

function writeRuntime(value) {
  const stream = new WriteStream(new Uint8Array(64));
  const input = new runtime.CFloat();
  input.x = value;
  if (!runtime.WriteCFloat(stream, input)) return null;
  stream.flush();
  return { bytes: stream.data().slice(), bits: stream.bitsProcessed() };
}

function readRuntime(wire) {
  const stream = new ReadStream(wire.bytes);
  const output = new runtime.CFloat();
  if (!runtime.ReadCFloat(stream, output)) return null;
  return output.x;
}

function writeFlat(value) {
  const view = new DataView(new ArrayBuffer(64));
  const size = flat.WriteCFloatFlat(view, { x: value });
  if (size < 0) return null;
  return { bytes: new Uint8Array(view.buffer.slice(0, size)), bits: size * 8 };
}

function readFlat(wire) {
  const buffer = new Uint8Array(wire.bytes.length + flat.FlatReadSlack);
  buffer.set(wire.bytes);
  const output = new runtime.CFloat();
  if (!flat.ReadCFloatFlat(new DataView(buffer.buffer), wire.bits, output)) return null;
  return output.x;
}

const tiers = [
  { name: "runtime", write: writeRuntime, read: readRuntime },
  { name: "flat", write: writeFlat, read: readFlat },
];

for (const c of readCases()) {
  for (const tier of tiers) {
    let wire;
    try {
      wire = tier.write(c.value);
    } catch (err) {
      fail(`${tier.name} ${c.name}: threw ${err.name}: ${err.message}`);
      continue;
    }
    if (!c.written) {
      if (wire !== null) fail(`${tier.name} ${c.name}: wrote a value the range refuses`);
      continue;
    }
    if (wire === null) {
      fail(`${tier.name} ${c.name}: refused a lawful value`);
      continue;
    }
    let got;
    try {
      got = tier.read(wire);
    } catch (err) {
      fail(`${tier.name} ${c.name}: threw on the read-back ${err.name}: ${err.message}`);
      continue;
    }
    const want = c.readsBack === undefined ? c.value : c.readsBack;
    if (got === null) {
      fail(`${tier.name} ${c.name}: the reader refused the writer's bytes`);
    } else if (!sameNumber(got, want)) {
      fail(`${tier.name} ${c.name}: read back ${got}, want ${want}`);
    }
  }
}

if (failures !== 0) process.exit(1);
console.log(`packet write compressed float JavaScript: ${mode} build, every case holds in the runtime and flat tiers`);
