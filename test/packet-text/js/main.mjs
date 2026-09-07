import assert from "node:assert/strict";
import { createInterface } from "node:readline";
import { pathToFileURL } from "node:url";
import { ReadStream, WriteStream } from "../../../../serialize.js/src/index.js";
const tier = process.argv[2];
assert(tier === "runtime" || tier === "flat");
const base = process.argv[3] ? pathToFileURL(process.argv[3] + "/") : new URL("../../../build/packet-text/js/", import.meta.url);
const d = await import(new URL("Narrow.js", base));
const f = await import(new URL("NarrowFlat.js", base));
const hex = bytes => bytes.length ? Buffer.from(bytes).toString("hex") : "-";
for await (const line of createInterface({ input: process.stdin, crlfDelay: Infinity })) {
  const wire = Uint8Array.from(Buffer.from(line === "-" ? "" : line, "hex"));
  const value = new d.Narrow();
  const padded = new Uint8Array(wire.length + 8); padded.set(wire);
  const view = new DataView(padded.buffer);
  const r = new ReadStream(wire);
  const ok = tier === "runtime" ? d.ReadNarrow(r, value) : f.ReadNarrowFlat(value, view, wire.length * 8);
  if (!ok) { console.log("REFUSE"); continue; }
  const w = new WriteStream(new Uint8Array(256));
  assert(d.WriteNarrow(w, value));
  const bits = w.bitsProcessed(); w.flush();
  let encoded = w.data();
  if (tier === "runtime") {
    assert.equal(r.bitsProcessed(), bits);
  } else {
    assert(f.ReadNarrowFlat(new d.Narrow(), view, bits));
    assert.equal(f.ReadNarrowFlat(new d.Narrow(), view, bits - 1), false);
    const bytes = new Uint8Array(256);
    const count = f.WriteNarrowFlat(value, new DataView(bytes.buffer));
    assert.equal(count, Math.ceil(bits / 8));
    encoded = bytes.subarray(0, count);
  }
  console.log(`OK ${bits} ${hex(value.Text.subarray(0, value.TextLength))} ${bits} ${hex(encoded)}`);
}
