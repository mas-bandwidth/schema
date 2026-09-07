import { createInterface } from "node:readline";
import { BlobFromJson, BlobToJson, TableReport } from "../../../build/table-base64/js/BytesTable.js";

const hex = bytes => bytes.length ? Buffer.from(bytes).toString("hex") : "-";
for await (const line of createInterface({ input: process.stdin })) {
  const report = new TableReport();
  const value = BlobFromJson(Buffer.from(line, "hex"), report);
  if (report.Malformed) { console.log("1 0 0 - -"); continue; }
  console.log(`0 ${report.Clamped} ${report.KindMismatch} ${hex(value.Payload.subarray(0, value.PayloadLength))} ${hex(Buffer.from(BlobToJson(value)))}`);
}
