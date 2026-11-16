import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
import { ReadStream, WriteStream } from "../../../../serialize.js/src/index.js";

const base = process.argv[2]
  ? pathToFileURL(process.argv[2] + "/")
  : new URL("../../../build/packet-write-text/js/", import.meta.url);
const runtime = await import(new URL("Text.js", base));
const flat = await import(new URL("TextFlat.js", base));

const cases = [];
for (const line of readFileSync(new URL("../cases.txt", import.meta.url), "utf8").split("\n")) {
  const text = line.trim();
  if (text === "" || text.startsWith("#")) continue;
  const [name, hex, verdict] = text.split("|").map((part) => part.trim());
  const bytes = hex === "-"
    ? new Uint8Array(0)
    : Uint8Array.from(Buffer.from(hex.replaceAll(" ", ""), "hex"));
  cases.push({ name, bytes, written: verdict === "written" });
}

let failures = 0;
function fail(message) {
  failures++;
  console.error(`FAILED: packet write text ${message}`);
}

function value(bytes) {
  const out = new runtime.Payload();
  out.text.set(bytes);
  out.textLength = bytes.length;
  return out;
}

function writeRuntime(bytes) {
  const stream = new WriteStream(new Uint8Array(256));
  if (!runtime.WritePayload(stream, value(bytes))) return { ok: false };
  const bits = stream.bitsProcessed();
  stream.flush();
  return { ok: true, bytes: stream.data().slice(), bits };
}

function writeFlat(bytes) {
  const buffer = new Uint8Array(256);
  const count = flat.WritePayloadFlat(new DataView(buffer.buffer), value(bytes));
  if (count < 0) return { ok: false };
  return { ok: true, bytes: buffer.slice(0, count) };
}

function readRuntime(bytes) {
  const out = new runtime.Payload();
  const ok = runtime.ReadPayload(new ReadStream(Uint8Array.from(bytes)), out);
  return { ok, value: out };
}

function readFlat(bytes) {
  const buffer = new Uint8Array(bytes.length + flat.FlatReadSlack);
  buffer.set(bytes);
  const out = new runtime.Payload();
  const ok = flat.ReadPayloadFlat(new DataView(buffer.buffer), bytes.length * 8, out);
  return { ok, value: out };
}

function same(a, b) {
  return a.length === b.length && a.every((byte, index) => byte === b[index]);
}

function checkRead(tier, name, bytes, wire) {
  const result = tier === "runtime" ? readRuntime(wire) : readFlat(wire);
  if (!result.ok) {
    fail(`${name} ${tier} reader refused the written packet`);
    return;
  }
  const used = result.value.text.subarray(0, result.value.textLength);
  if (!same(used, bytes) || result.value.textLength !== bytes.length) {
    fail(`${name} ${tier} read back different used bytes`);
  }
}

for (const testCase of cases) {
  const rt = writeRuntime(testCase.bytes);
  const ft = writeFlat(testCase.bytes);
  if (testCase.written) {
    if (!rt.ok) fail(`${testCase.name} runtime writer refused well-formed text`);
    if (!ft.ok) fail(`${testCase.name} flat writer refused well-formed text`);
    if (!rt.ok || !ft.ok) continue;
    if (!same(rt.bytes, ft.bytes)) fail(`${testCase.name} tiers wrote different packets`);
    checkRead("runtime", testCase.name, testCase.bytes, rt.bytes);
    checkRead("flat", testCase.name, testCase.bytes, rt.bytes);
  } else {
    if (rt.ok) fail(`${testCase.name} runtime writer accepted ill-formed text`);
    if (ft.ok) fail(`${testCase.name} flat writer accepted ill-formed text`);
  }
}

if (failures !== 0) process.exit(1);
console.log("packet write text JavaScript: both tiers refuse ill-formed text and round-trip well-formed text");
