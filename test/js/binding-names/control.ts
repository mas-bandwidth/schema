import { Clean, Number as SchemaNumber } from "./Control.js";
import { ReadCleanFlat, WriteCleanFlat } from "./ControlFlat.js";

const collision = new SchemaNumber();
collision.value = 42;
const source = new Clean();
source.value = 0.5;
const wire = new DataView(new ArrayBuffer(16));
const bytes: number = WriteCleanFlat(wire, source);
const copy = new Clean();
if (collision.value !== 42 || bytes !== 4 || !ReadCleanFlat(wire, 8 * bytes, copy) || copy.value !== 0.5) {
    throw new Error("generated Number or float32 round trip failed");
}
