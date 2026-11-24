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

const runtime = await import(new URL("PacketCFloat.js", generated));
const flat = await import(new URL("PacketCFloatFlat.js", generated));
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

// One case per line: <name> <value> <verdict> [reads-back <value>]
// [type RatioEndpoint wire <hex> bits <n>]; the modifiers come in pairs.
function readCases() {
  const cases = [];
  for (const raw of readFileSync(casesURL, "utf8").split("\n")) {
    const line = raw.trim();
    if (line === "" || line.startsWith("#")) continue;
    const f = line.split(/\s+/);
    if (f[2] !== "written" && f[2] !== "refused") {
      throw new Error(`cases.txt: ${f[0]}: verdict "${f[2]}" is neither written nor refused`);
    }
    if (f.length % 2 !== 1) throw new Error(`cases.txt: ${f[0]}: a modifier has no operand`);
    const c = { name: f[0], value: parseValue(f[1]), written: f[2] === "written", readsBack: undefined, ratio: false, wire: undefined, bits: undefined };
    for (let i = 3; i + 1 < f.length; i += 2) {
      if (f[i] === "reads-back") c.readsBack = parseValue(f[i + 1]);
      else if (f[i] === "type" && f[i + 1] === "RatioEndpoint") c.ratio = true;
      else if (f[i] === "wire") c.wire = f[i + 1].toLowerCase();
      else if (f[i] === "bits" && /^[0-9]+$/.test(f[i + 1])) c.bits = Number(f[i + 1]);
      else throw new Error(`cases.txt: ${f[0]}: unknown modifier ${f[i]} ${f[i + 1]}`);
    }
    cases.push(c);
  }
  return cases;
}

function hex(bytes) {
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

// Bit-exact equality, including the sign of zero: the shared list says a
// written -0 reads back +0.
function sameNumber(actual, expected) {
  if (Number.isNaN(actual) || Number.isNaN(expected)) return false;
  return actual === expected && Object.is(actual, -0) === Object.is(expected, -0);
}

function writeRuntime(c) {
  const stream = new WriteStream(new Uint8Array(64));
  if (c.ratio) {
    const input = new runtime.RatioEndpoint();
    input.ratio = c.value;
    if (!runtime.WriteRatioEndpoint(stream, input)) return null;
  } else {
    const input = new runtime.CFloat();
    input.x = c.value;
    if (!runtime.WriteCFloat(stream, input)) return null;
  }
  // the bits the write emitted, before the flush pads the last byte
  const bits = stream.bitsProcessed();
  stream.flush();
  return { bytes: stream.data().slice(), bits };
}

function readRuntime(c, wire) {
  const stream = new ReadStream(wire.bytes);
  if (c.ratio) {
    const output = new runtime.RatioEndpoint();
    if (!runtime.ReadRatioEndpoint(stream, output)) return null;
    return output.ratio;
  }
  const output = new runtime.CFloat();
  if (!runtime.ReadCFloat(stream, output)) return null;
  return output.x;
}

function writeFlat(c) {
  const view = new DataView(new ArrayBuffer(64));
  const size = c.ratio ? flat.WriteRatioEndpointFlat(view, { ratio: c.value }) : flat.WriteCFloatFlat(view, { x: c.value });
  if (size < 0) return null;
  return { bytes: new Uint8Array(view.buffer.slice(0, size)), bits: size * 8 };
}

function readFlat(c, wire) {
  const buffer = new Uint8Array(wire.bytes.length + flat.FlatReadSlack);
  buffer.set(wire.bytes);
  if (c.ratio) {
    const output = new runtime.RatioEndpoint();
    if (!flat.ReadRatioEndpointFlat(new DataView(buffer.buffer), wire.bits, output)) return null;
    return output.ratio;
  }
  const output = new runtime.CFloat();
  if (!flat.ReadCFloatFlat(new DataView(buffer.buffer), wire.bits, output)) return null;
  return output.x;
}

const tiers = [
  { name: "runtime", write: writeRuntime, read: readRuntime },
  { name: "flat", write: writeFlat, read: readFlat },
];

const cases = readCases();
for (const c of cases) {
  const failuresBefore = failures;
  runCase(c);
  // the case's answer tools/casegate reads (the FAILED lines on stderr say why)
  console.log(`case ${c.name} ${failures === failuresBefore ? "ok" : "FAILED: see the FAILED lines above"}`);
}

// The flat writer returns bytes, not bits: for a case that pins its bits it
// is held to the declared width (the generated <T>MaxBits) and its bytes
// must read back through exactly that many bits.
function runCase(c) {
  const maxBits = c.ratio ? runtime.RatioEndpointMaxBits : runtime.CFloatMaxBits;
  for (const tier of tiers) {
    let wire;
    try {
      wire = tier.write(c);
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
    if (c.wire !== undefined && hex(wire.bytes) !== c.wire) {
      fail(`${tier.name} ${c.name}: wrote ${hex(wire.bytes)}, want ${c.wire}`);
      continue;
    }
    if (c.bits !== undefined) {
      if (tier.name === "flat") {
        if (maxBits !== c.bits) {
          fail(`${tier.name} ${c.name}: the declared width is ${maxBits} bits, want ${c.bits}`);
          continue;
        }
        wire = { bytes: wire.bytes, bits: c.bits };
      } else if (wire.bits !== c.bits) {
        fail(`${tier.name} ${c.name}: wrote ${wire.bits} bits before the flush, want ${c.bits}`);
        continue;
      }
    }
    let got;
    try {
      got = tier.read(c, wire);
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

// A summary; tools/casegate holds every case of the file to an answer.
console.log(`packet write compressed float JavaScript, ${mode} build: ran ${cases.length}, failed ${failures}`);
if (failures !== 0 || cases.length === 0) process.exit(1);
