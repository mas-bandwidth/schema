// The Goose leg's per-PR happy path (make/happy-path-goose.mk): one ShipCreate
// written and read back through the generated module. The recipe copies this
// file beside the generated modules (Goose imports are file-relative).
import os;
import .Happy;
import .serialize;

var wbuf: u8[32] = [0; 32];
var rbuf: u8[48] = [0; 48];

fn fail(why: const u8[:]) {
    print("happy goose: FAILED: ", why);
    exit(1);
}

fn main() {
    var v = happy::NewShipCreate();
    v.ShipType = happy::ShipTypeCorvette;
    v.Position.X = 1.5;
    v.Position.Y = -2.25;
    v.Position.Z = 3.0;
    v.Health = 777;
    v.Alive = true;

    var w = serialize_write_stream_init(wbuf);
    let refusal = happy::WriteShipCreate(w, v);
    if refusal != SERIALIZE_ERROR_NONE { fail(str("WriteShipCreate refused: ", serialize_refusal_name(refusal))); }
    serialize_flush(w);
    let n = serialize_bytes_processed(w);

    var got = happy::NewShipCreate();
    var r = serialize_read_stream_init_padded(rbuf, wbuf[0..n]);
    if !happy::ReadShipCreate(r, got) { fail(str("ReadShipCreate refused: ", serialize_refusal_name(serialize_error(r)))); }

    if got.ShipType != happy::ShipTypeCorvette { fail(str("ship_type: got ", got.ShipType)); }
    if got.Position.X != 1.5 { fail(str("position.x: got ", got.Position.X)); }
    if got.Position.Y != -2.25 { fail(str("position.y: got ", got.Position.Y)); }
    if got.Position.Z != 3.0 { fail(str("position.z: got ", got.Position.Z)); }
    if got.Health != 777 { fail(str("health: got ", got.Health)); }
    if !got.Alive { fail("alive: got false"); }

    print("happy goose: ok (", n, " bytes)");
}
