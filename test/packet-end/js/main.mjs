// The packet-end driver for the JavaScript leg (docs/SPEC.md §5, "Read
// termination"): both generated read tiers must accept the exact packet and
// refuse a whole byte left after the top-level object and non-zero unused bits
// in the final byte. The shared vectors live under testdata/wire/packet-end.
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
import { ReadStream } from "../../../../serialize.js/src/index.js";

const vectorDir = process.argv[2]
  ? pathToFileURL(process.argv[2] + "/")
  : new URL("../../../testdata/wire/packet-end/", import.meta.url);
const generated = process.argv[3]
  ? pathToFileURL(process.argv[3] + "/")
  : new URL("../../../build/packet-end/js/", import.meta.url);

const runtime = await import(new URL("End.js", generated));
const flat = await import(new URL("EndFlat.js", generated));

let failures = 0;
function fail(message) {
  failures++;
  console.error(`FAILED: packet end ${message}`);
}

function readRuntime(bytes) {
  const value = new runtime.End();
  const ok = runtime.ReadEnd(new ReadStream(Uint8Array.from(bytes)), value);
  return { ok, value: value.value };
}

function readFlat(bytes) {
  const buffer = new Uint8Array(bytes.length + flat.FlatReadSlack);
  buffer.set(bytes);
  const value = new runtime.End();
  const ok = flat.ReadEndFlat(new DataView(buffer.buffer), bytes.length * 8, value);
  return { ok, value: value.value };
}

const vectors = [
  { name: "exact.bin", accepted: true },
  { name: "trailing.bin", accepted: false },
  { name: "padding.bin", accepted: false },
];

for (const vector of vectors) {
  const bytes = new Uint8Array(readFileSync(new URL(vector.name, vectorDir)));
  for (const [tier, read] of [["runtime", readRuntime], ["flat", readFlat]]) {
    const result = read(bytes);
    if (result.ok !== vector.accepted) {
      fail(`${tier} ${vector.name} ${result.ok ? "accepted" : "refused"}`);
      continue;
    }
    if (vector.accepted && result.value !== 3) {
      fail(`${tier} ${vector.name} read value ${result.value}, want 3`);
    }
  }
}

if (failures !== 0) {
  process.exit(1);
}
console.log("packet end JavaScript: both tiers accept the exact packet and refuse trailing bytes and non-zero padding");
