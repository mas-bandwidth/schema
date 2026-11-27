// The JavaScript compressed-float endpoint gate. Both generated packet paths
// use the shared endpoint oracle: runtime streams and the flat writer/reader
// must encode exactly seven logical bits into the pinned padded byte.
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
import { WriteStream, ReadStream } from "../../../../serialize.js/src/index.js";

const casesURL = process.argv[2]
  ? pathToFileURL(process.argv[2])
  : new URL("../ratio-endpoints.txt", import.meta.url);
const generated = process.argv[3]
  ? pathToFileURL(process.argv[3] + "/")
  : new URL("../../../build/packet-cfloat-endpoints/js/", import.meta.url);

const runtime = await import(new URL("PacketRatio.js", generated));
const flat = await import(new URL("PacketRatioFlat.js", generated));

let failures = 0;
function check(ok, message) {
  if (ok) return;
  failures++;
  console.error(`FAILED: packet cfloat endpoint ${message}`);
}

const cases = [];
for (const raw of readFileSync(casesURL, "utf8").split("\n")) {
  const line = raw.trim();
  if (line === "" || line.startsWith("#")) continue;
  const fields = line.split(/\s+/);
  if (fields.length !== 3) throw new Error(`invalid endpoint row: ${line}`);
  cases.push({ name: fields[0], value: Number(fields[1]), byte: Number.parseInt(fields[2], 16) });
}

check(cases.length === 2, `fixture has ${cases.length} cases, want 2`);
check(runtime.RatioEndpointMaxBits === 7, `generated maximum is ${runtime.RatioEndpointMaxBits} bits, want 7`);

for (const c of cases) {
  const input = new runtime.RatioEndpoint();
  input.ratio = c.value;

  const stream = new WriteStream(new Uint8Array(runtime.RatioEndpointMaxBytes));
  check(runtime.WriteRatioEndpoint(stream, input), `runtime ${c.name}: writer refused`);
  check(stream.bitsProcessed() === 7, `runtime ${c.name}: wrote ${stream.bitsProcessed()} bits, want 7`);
  stream.flush();
  const runtimeWire = stream.data();
  check(runtimeWire.length === 1 && runtimeWire[0] === c.byte,
    `runtime ${c.name}: wire ${Buffer.from(runtimeWire).toString("hex")}, want ${c.byte.toString(16).padStart(2, "0")}`);
  const runtimeOutput = new runtime.RatioEndpoint();
  const reader = new ReadStream(runtimeWire);
  check(runtime.ReadRatioEndpoint(reader, runtimeOutput), `runtime ${c.name}: reader refused`);
  check(runtimeOutput.ratio === c.value, `runtime ${c.name}: read ${runtimeOutput.ratio}, want ${c.value}`);

  const flatBuffer = new Uint8Array(runtime.RatioEndpointMaxBytes);
  const flatBytes = flat.WriteRatioEndpointFlat(new DataView(flatBuffer.buffer), input);
  check(flatBytes === 1, `flat ${c.name}: wrote ${flatBytes} bytes, want 1`);
  check(flatBuffer[0] === c.byte,
    `flat ${c.name}: wire ${flatBuffer[0].toString(16).padStart(2, "0")}, want ${c.byte.toString(16).padStart(2, "0")}`);
  const flatReadBuffer = new Uint8Array(1 + flat.FlatReadSlack);
  flatReadBuffer[0] = flatBuffer[0];
  const flatOutput = new runtime.RatioEndpoint();
  check(flat.ReadRatioEndpointFlat(new DataView(flatReadBuffer.buffer), 7, flatOutput),
    `flat ${c.name}: reader refused seven logical bits`);
  check(flatOutput.ratio === c.value, `flat ${c.name}: read ${flatOutput.ratio}, want ${c.value}`);
}

console.log(`packet compressed-float JavaScript endpoints: ran ${cases.length * 2}, failed ${failures}`);
if (failures !== 0 || cases.length !== 2) process.exit(1);
