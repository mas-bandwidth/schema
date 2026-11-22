// THE WRITE-REFUSAL-PACKET PROBE for the JavaScript leg (docs/SPEC.md §5.1;
// test/conformance/write-refusal-packet/rows.txt).
//
// usage: node writerefusalpacket.mjs <rows.txt> <generated dir> <serialize.js index.js> <runtime|flat>
//
// For every row: a fresh LAWFUL PacketWriteRefusal, ONE op from the row applied
// to it, then the generated packet writer over a 256-byte buffer, in the tier
// the last argument names. JavaScript refuses in every build (§5.1), and the
// refusal of both tiers is a return value, never a throw: the runtime tier's
// WritePacketWriteRefusal answers `false`, the flat tier's
// WritePacketWriteRefusalFlat answers -1. The probe prints `<name> refused` for
// exactly that answer and `<name> wrote` for a completed write. An exception is
// NOT a refusal: it is not caught, so a crash or a typo fails the run instead of
// passing as one. The driver sets the field and reports; it checks no value.
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

const [rowsPath, generatedDir, serializeIndex, tier] = process.argv.slice(2);
if (tier !== "runtime" && tier !== "flat") {
  throw new Error("tier must be runtime or flat, got " + JSON.stringify(tier));
}
const { WriteStream } = await import(pathToFileURL(serializeIndex).href);
const generated = pathToFileURL(generatedDir + "/");
const runtime = await import(new URL("PacketWriteRefusal.js", generated).href);
const flat = await import(new URL("PacketWriteRefusalFlat.js", generated).href);

function hexBytes(text) {
  if (text.length % 2 !== 0 || !/^[0-9A-Fa-f]*$/.test(text)) { throw new Error(JSON.stringify(text) + " is not hex"); }
  const out = new Uint8Array(text.length / 2);
  for (let i = 0; i < out.length; i++) { out[i] = parseInt(text.slice(2 * i, 2 * i + 2), 16); }
  return out;
}

// real parses a float argument; nan, inf and -inf are spelled out.
function real(text) {
  if (text === "nan") return NaN;
  if (text === "inf") return Infinity;
  if (text === "-inf") return -Infinity;
  const x = Number(text);
  if (Number.isNaN(x)) { throw new Error(JSON.stringify(text) + " is not a number"); }
  return x;
}

// apply sets the one field the op names, in the storage the writer reads.
function apply(value, op, arg) {
  const n = Number(arg);
  switch (op) {
    case "scalar": value.scalar = n; break;
    case "narrow": value.narrow = n; break;
    case "grade": value.grade = n; break;
    case "shot_tag": value.shot.type = n; break;
    case "window_count": value.windowCount = n; break;
    case "text_length": value.textLength = n; break;
    case "text_bytes": {
      const bytes = hexBytes(arg);
      value.text.fill(0);
      value.text.set(bytes);
      value.textLength = bytes.length;
      break;
    }
    case "text_fill": value.text.fill(0x61, 0, n); value.textLength = n; break;
    case "fixed_constant": value.fixedConstant = n; break;
    case "reserved_zero": value.reservedZero = n; break;
    case "ratio": value.ratio = real(arg); break;
    default: throw new Error("unknown op " + JSON.stringify(op));
  }
}

// refused answers whether the tier's writer returned its refusal.
function refused(value) {
  if (tier === "flat") {
    const size = flat.WritePacketWriteRefusalFlat(new DataView(new ArrayBuffer(256)), value);
    if (size === -1) return true;
    if (!Number.isInteger(size) || size < 0) { throw new Error("the flat writer answered " + String(size)); }
    return false;
  }
  const stream = new WriteStream(new Uint8Array(256));
  const ok = runtime.WritePacketWriteRefusal(stream, value);
  if (ok === false) return true;
  if (ok !== true) { throw new Error("the runtime writer answered " + String(ok)); }
  return false;
}

for (const raw of readFileSync(rowsPath, "utf8").split("\n")) {
  const line = raw.trim();
  if (line === "" || line.startsWith("#")) continue;
  const [name, , op, arg] = line.split(/\s+/);
  const value = new runtime.PacketWriteRefusal();
  apply(value, op, arg);
  console.log(name + " " + (refused(value) ? "refused" : "wrote"));
}
