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
// The wire's first n bits, every later bit zero, with the flat reader's slack.
const prefix = (wire, n) => {
  const padded = new Uint8Array(wire.length + 8);
  padded.set(wire.subarray(0, n >>> 3));
  if ((n & 7) !== 0) {
    padded[n >>> 3] = wire[n >>> 3] & ((1 << (n & 7)) - 1);
  }
  return new DataView(padded.buffer);
};
// This corpus compares UTF-8 text only; the packet-end corpus owns the end
// check. The runtime tier reads the object through the composable bits read.
// The flat tier has only the one-packet read, so its object read is the
// shortest prefix it accepts: that read consumes exactly those bits.
const readFlat = wire => {
  for (let n = 0; n <= wire.length * 8; n++) {
    const value = new d.Narrow();
    if (f.ReadNarrowFlat(prefix(wire, n), n, value)) {
      return { consumed: n, value };
    }
  }
  return { consumed: -1, value: null };
};
const readRuntime = wire => {
  const r = new ReadStream(wire);
  const value = new d.Narrow();
  return d.ReadNarrow(r, value, true) ? { consumed: r.bitsProcessed(), value } : { consumed: -1, value: null };
};
for await (const line of createInterface({ input: process.stdin, crlfDelay: Infinity })) {
  const wire = Uint8Array.from(Buffer.from(line === "-" ? "" : line, "hex"));
  const { consumed, value } = tier === "runtime" ? readRuntime(wire) : readFlat(wire);
  if (consumed < 0) {
    console.log("REFUSE");
    continue;
  }
  const w = new WriteStream(new Uint8Array(256));
  assert(d.WriteNarrow(w, value));
  const bits = w.bitsProcessed(); w.flush();
  let encoded = w.data();
  assert.equal(consumed, bits);
  if (tier === "flat") {
    assert(f.ReadNarrowFlat(prefix(wire, bits), bits, new d.Narrow()));
    assert.equal(f.ReadNarrowFlat(prefix(wire, bits), bits - 1, new d.Narrow()), false);
    const bytes = new Uint8Array(256);
    const count = f.WriteNarrowFlat(new DataView(bytes.buffer), value);
    assert.equal(count, Math.ceil(bits / 8));
    encoded = bytes.subarray(0, count);
  }
  console.log(`OK ${bits} ${hex(value.text.subarray(0, value.textLength))} ${bits} ${hex(encoded)}`);
}
