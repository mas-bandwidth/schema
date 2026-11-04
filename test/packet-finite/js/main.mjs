// Verify SPEC §4.3's finite float rule on the JavaScript flat tier's writer:
// Write<T>Flat refuses a NaN or an infinity with -1 in every build, NODE_ENV
// production included (docs/SPEC.md §5.1: JavaScript has no assert idiom, so
// the writer refuses in every build), and carries negative zero, the
// subnormals and the largest finite value bit exact through Read<T>Flat. Flat
// holds a float32, a float64 and a bool in one run; Single32 and Single64 hold
// one float each.
//
// A JavaScript number holds no NaN payload a writer could keep, so every NaN
// pattern below reaches the writer as the one NaN: the vectors are named for
// the bits they were made from, as the other legs name theirs.
//
// usage: node main.mjs <generated dir>
//
// Each failing vector prints one line, "FAILED: <vector>: <detail>". The
// negative controls compare the set of failing vector names against
// write.expected beside this file.
import { pathToFileURL } from "node:url";
import { resolve } from "node:path";

const dir = process.argv[2];
if (dir === undefined) {
  process.stderr.write("usage: node main.mjs <generated dir>\n");
  process.exit(2);
}
const f = await import(pathToFileURL(resolve(dir, "FiniteFlat.js")).href);

let failed = false;
function fail(name, detail) {
  process.stderr.write("FAILED: " + name + ": " + detail + "\n");
  failed = true;
}

const scratch = new DataView(new ArrayBuffer(8));
function f32(bits) { scratch.setUint32(0, bits, true); return scratch.getFloat32(0, true); }
function f64(bits) { scratch.setBigUint64(0, bits, true); return scratch.getFloat64(0, true); }
function bits32(x) { scratch.setFloat32(0, x, true); return scratch.getUint32(0, true); }
function bits64(x) { scratch.setFloat64(0, x, true); return scratch.getBigUint64(0, true); }

const nonFinite32 = [
  ["quiet-nan", 0x7fc00000], ["signalling-nan", 0x7f800001], ["negative-nan", 0xffc00000],
  ["payload-nan", 0x7fc0dead], ["positive-inf", 0x7f800000], ["negative-inf", 0xff800000],
];
const nonFinite64 = [
  ["quiet-nan", 0x7ff8000000000000n], ["signalling-nan", 0x7ff0000000000001n], ["negative-nan", 0xfff8000000000000n],
  ["payload-nan", 0x7ff800000000deadn], ["positive-inf", 0x7ff0000000000000n], ["negative-inf", 0xfff0000000000000n],
];
const finite32 = [
  ["negative-zero", 0x80000000], ["min-subnormal", 0x00000001], ["max-subnormal", 0x007fffff],
  ["negative-subnormal", 0x80000001], ["max-finite", 0x7f7fffff], ["negative-max-finite", 0xff7fffff],
];
const finite64 = [
  ["negative-zero", 0x8000000000000000n], ["min-subnormal", 0x0000000000000001n], ["max-subnormal", 0x000fffffffffffffn],
  ["negative-subnormal", 0x8000000000000001n], ["max-finite", 0x7fefffffffffffffn], ["negative-max-finite", 0xffefffffffffffffn],
];

// a buffer past every shape's MaxBytes and the read slack. The flat writer
// stores each field as it goes, so a refused write answers -1 with the fields
// before the refused one already stored: the buffer then holds no packet.
function buffer() { return new DataView(new ArrayBuffer(64)); }

function checkRefused(name, write, value) {
  const view = buffer();
  let n;
  try {
    n = write(view, value);
  } catch (e) {
    fail(name, "the writer threw " + e);
    return;
  }
  if (n !== -1) { fail(name, "write accepted a non-finite float (" + n + " bytes)"); }
}

function checkRoundTrip(name, write, read, value, fresh, want, same) {
  const view = buffer();
  const n = write(view, value);
  if (n < 0) { fail(name, "write refused a finite value"); return; }
  const got = Array.from(new Uint8Array(view.buffer, 0, n));
  if (got.length !== want.length || got.some((b, i) => b !== want[i])) {
    fail(name, "wire " + got.join(",") + ", want " + want.join(",") + ": the pattern did not ride bit exact");
    return;
  }
  const back = fresh();
  if (!read(view, n * 8, back)) { fail(name, "read refused a finite pattern"); return; }
  if (!same(back)) { fail(name, "read back a value that is not the pattern written"); }
}

function le32(b) { return [b & 0xff, (b >>> 8) & 0xff, (b >>> 16) & 0xff, (b >>> 24) & 0xff]; }
function le64(b) { const out = []; for (let i = 0n; i < 64n; i += 8n) { out.push(Number((b >> i) & 0xffn)); } return out; }

// Flat's wire: a's 32 bits, b's 64 bits, then c's one bit, padded to a byte
function flatWire(a, b) { return [...le32(a), ...le64(b), 1]; }

for (const [pattern, b] of nonFinite32) {
  checkRefused("flat-f32/write/" + pattern, f.WriteFlatFlat, { a: f32(b), b: 1.5, c: true });
  checkRefused("single32/write/" + pattern, f.WriteSingle32Flat, { x: f32(b) });
}
for (const [pattern, b] of nonFinite64) {
  checkRefused("flat-f64/write/" + pattern, f.WriteFlatFlat, { a: 1.5, b: f64(b), c: true });
  checkRefused("single64/write/" + pattern, f.WriteSingle64Flat, { x: f64(b) });
}
// a finite double past float32's range rounds to an infinity: refused too
checkRefused("flat-f32/write/past-float32", f.WriteFlatFlat, { a: 1e300, b: 1.5, c: true });
checkRefused("single32/write/past-float32", f.WriteSingle32Flat, { x: -1e300 });

const fresh = () => ({ a: 0, b: 0, c: false, x: 0 });
for (const [pattern, b] of finite32) {
  checkRoundTrip("flat-f32/roundtrip/" + pattern, f.WriteFlatFlat, f.ReadFlatFlat,
    { a: f32(b), b: 1.5, c: true }, fresh, flatWire(b, bits64(1.5)), (v) => bits32(v.a) === b);
  checkRoundTrip("single32/roundtrip/" + pattern, f.WriteSingle32Flat, f.ReadSingle32Flat,
    { x: f32(b) }, fresh, le32(b), (v) => bits32(v.x) === b);
}
for (const [pattern, b] of finite64) {
  checkRoundTrip("flat-f64/roundtrip/" + pattern, f.WriteFlatFlat, f.ReadFlatFlat,
    { a: 1.5, b: f64(b), c: true }, fresh, flatWire(bits32(1.5), b), (v) => bits64(v.b) === b);
  checkRoundTrip("single64/roundtrip/" + pattern, f.WriteSingle64Flat, f.ReadSingle64Flat,
    { x: f64(b) }, fresh, le64(b), (v) => bits64(v.x) === b);
}

if (failed) { process.exit(1); }
console.log("packet-finite js: the flat writer refuses every non-finite float (NODE_ENV=" + (process.env.NODE_ENV ?? "") + ")");
