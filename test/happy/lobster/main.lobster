// The per-PR happy path, the Lobster leg (make/happy-path-lobster.mk): one
// ShipCreate written with the generated write, read back into a fresh value
// with the generated read, every field compared. Any failure prints
// "FAILED: <what>" and exits 1.
import .Happy
import .serialize

var failures = 0
def ck(ok:bool, what:string):
    if not ok:
        failures += 1
        print("FAILED: " + what)

let v = happy.NewShipCreate()
v.ShipType = happy.ShipTypeCorvette
v.Position.X = 1.5
v.Position.Y = -2.25
v.Position.Z = 3.0
v.Health = 777
v.Alive = true

let buf = ensure_size(string_with_capacity(32), 32, 0)
let w = serialize_write_stream(buf)
ck(happy.WriteShipCreate(w, v), "write")
w.flush()
let n = w.bytes_processed()
let r = serialize_read_stream_padded(substring(buf, 0, n))
let back = happy.NewShipCreate()
ck(happy.ReadShipCreate(r, back), "read")
ck(back.ShipType == happy.ShipTypeCorvette, "ship_type")
ck(back.Position.X == 1.5, "position.x")
ck(back.Position.Y == -2.25, "position.y")
ck(back.Position.Z == 3.0, "position.z")
ck(back.Health == 777, "health")
ck(back.Alive == true, "alive")
if failures != 0:
    set_exit_code(1)
    return from program
print("happy lobster: ok (" + string(n) + " bytes)")
