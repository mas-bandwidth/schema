// The per-PR happy path for JavaScript: one ShipCreate written with the
// generated packet writer, read back with the generated reader, every field
// compared. Run by make/happy-path.mk after it generates build/happy/js.
import { WriteStream, ReadStream } from "../../../../serialize.js/src/index.js";
import * as happy from "../../../build/happy/js/Happy.js";

function fail(why) {
  console.error(`happy js: FAILED: ${why}`);
  process.exit(1);
}

const sent = new happy.ShipCreate();
sent.shipType = happy.ShipType.Corvette;
sent.position.x = 1.5;
sent.position.y = -2.25;
sent.position.z = 3.0;
sent.health = 777;
sent.alive = true;

const ws = new WriteStream(new Uint8Array(happy.ShipCreateMaxBytes + 8));
if (!happy.WriteShipCreate(ws, sent)) fail("WriteShipCreate returned false");
ws.flush();
const bytes = ws.data();

const got = new happy.ShipCreate();
if (!happy.ReadShipCreate(new ReadStream(bytes), got)) fail("ReadShipCreate returned false");

const checks = [
  ["ship_type", got.shipType, happy.ShipType.Corvette],
  ["position.x", got.position.x, 1.5],
  ["position.y", got.position.y, -2.25],
  ["position.z", got.position.z, 3.0],
  ["health", got.health, 777],
  ["alive", got.alive, true],
];
for (const [name, have, want] of checks) {
  if (have !== want) fail(`${name}: got ${have}, want ${want}`);
}
console.log(`happy js: ok (${bytes.length} bytes)`);
