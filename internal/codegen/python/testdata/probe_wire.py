"""The type-wire golden probe (internal/codegen/python, wire_test.go).

Builds every instance test/go/main.go builds before a goldenWire call, writes
it, and prints one line per golden: its name and the hex of every part it
wrote, in order. Each part is also read back and must write back byte for
byte, equal the value it was written from where every field is exact, refuse
when cut one byte short, and refuse with one byte too many. A failed check is
printed as FAILED: <golden>: <what>, and the probe exits 1.
"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from example import Clauses, Degenerate, Enums, Joins, Refused, Types, Wire  # noqa: E402

failed = False


def check(ok: bool, golden: str, what: str) -> None:
    global failed
    if not ok:
        print(f"FAILED: {golden}: {what}")
        failed = True


def part(golden: str, v: object, exact: bool = True) -> str:
    cls = type(v)
    data: bytes = v.write()  # type: ignore[attr-defined]
    try:
        back = cls.read(data)  # type: ignore[attr-defined]
    except Refused as e:
        check(False, golden, f"{cls.__name__} refused its own bytes: {e.reason}")
        return data.hex()
    check(back.write() == data, golden, f"{cls.__name__} does not write back byte for byte")
    if exact:
        check(back == v, golden, f"{cls.__name__} reads back {back!r}, wrote {v!r}")
    if data:
        try:
            cls.read(data[:-1])  # type: ignore[attr-defined]
            check(False, golden, f"{cls.__name__} cut one byte short read successfully")
        except Refused:
            pass
    try:
        cls.read(data + b"\x00")  # type: ignore[attr-defined]
        check(False, golden, f"{cls.__name__} with a trailing byte read successfully")
    except Refused as e:
        check(e.reason == "trailing_data", golden, f"a trailing byte refused as {e.reason}")
    return data.hex()


def emit(name: str, parts: list[str]) -> None:
    print(name, "".join(parts))


# shipcreate_flags
v = Types.ShipCreate()
v.ship_type = Enums.ShipType.Bomber
v.position.x, v.position.y, v.position.z = 1000, -2000, 3000
v.has_flags = True
v.flags = Enums.ShipFlags.Boosting | Enums.ShipFlags.Aiming
v.team = Enums.Team.Blue
v.health = 750
v.thrust = 55
emit("shipcreate_flags", [part("shipcreate_flags", v)])

# rigidbody_moving and rigidbody_at_rest
rb = Types.RigidBody()
rb.position.x, rb.position.y, rb.position.z = 1.5, -2.5, 3.25
rb.orientation.x, rb.orientation.y, rb.orientation.z, rb.orientation.w = 0.1, 0.2, 0.3, 0.9
rb.at_rest = False
rb.linear_velocity.x, rb.linear_velocity.y, rb.linear_velocity.z = 10.0, 20.0, -3.0
rb.angular_velocity.x, rb.angular_velocity.y, rb.angular_velocity.z = 0.25, 0.5, 0.75
emit("rigidbody_moving", [part("rigidbody_moving", rb)])
rb.at_rest = True
# the untaken branch reads back as zero values (SPEC §5), not as what was set
emit("rigidbody_at_rest", [part("rigidbody_at_rest", rb, exact=False)])

# chat
c = Wire.Chat()
c.text = "wire parity"
emit("chat", [part("chat", c)])

# probe_header
h = Wire.ProbeHeader()
h.version = 5
h.probe_id = 0x1122334455667788
emit("probe_header", [part("probe_header", h)])

# compressed_probe: the step index rides, so the value reads back quantized
cp = Wire.CompressedProbe()
cp.boundary = 0.005
cp.offset = -4.8585
emit("compressed_probe", [part("compressed_probe", cp, exact=False)])

# probebits
pb = Wire.ProbeBits()
pb.small = 0x1FF
pb.boundary = 0x1FFFFFFFF
pb.wide = 0xFEDCBA9876543210
pb.sensor = 4294967295
pb.nonce = 0xFFFFFFFFFFFFFFFF
emit("probebits", [part("probebits", pb)])

# probecollider
pc = Wire.ProbeCollider()
pc.armor = 7
pc.shape.type = Wire.ProbeShapeType.Slab
pc.shape.slab.width = 42
pc.shape.slab.height = 9
ring = Wire.ProbeShape()
ring.type = Wire.ProbeShapeType.Ring
ring.ring.radius = 777
pc.extras = [ring]
emit("probecollider", [part("probecollider", pc)])

# testdata: float32 values round on write, so the read is not the double set
td = Wire.TestData()
td.a, td.b, td.c = -100, 100, 149
td.d, td.e, td.f = 0x11, 0x22, 0x33
td.g = True
td.items = [0, 128, 255]
td.float_value = 3.1415926
td.compressed_float_value = 2.5
td.double_value = 1.0 / 3.0
td.int8_value = -128
td.int16_value = -32768
td.uint8_value = 255
td.uint16_value = 65535
td.uint32_value = 4294967295
td.uint64_value = 0xFFFFFFFFFFFFFFFF
td.int64_full = -(1 << 63)
td.int64_range = -999999999999
td.fixed_bytes = [i * 3 for i in range(17)]
td.text = "the quick brown fox"
emit("testdata", [part("testdata", td, exact=False)])

# inputpacket
ip = Types.InputPacket()
ip.synchronize_sequence = 7
ip.current_frame = 123456789
ip.start_frame = 123456780
i0, i1 = Types.Input(), Types.Input()
i0.throttle = 0.5
i0.fire = True
i1.stick_x = -0.25
i1.boost = True
ip.inputs = [i0, i1]
emit("inputpacket", [part("inputpacket", ip)])

# probearray: the compressed orientation reads back quantized
pa = Wire.ProbeArray()
s0, s1 = pa.samples
s0.orientation = 90.0
s0.raw_delta = -5
s0.big_delta = -1234567890123
s0.weapon = Wire.Weapon.Laser
s0.has_target = True
s0.target_id = 777
s0.samples = [42]
s1.active = False
s1.orientation = -45.5
s1.raw_delta = 7
s1.big_delta = 99
s1.idle_ticks = 1000
s1.samples = [7, 8]
pa.config.retries = 3
pa.config.preferred = Wire.Weapon.Missile
emit("probearray", [part("probearray", pa, exact=False)])

# degenerate
v1 = Degenerate.Vec2()
v1.x, v1.y = 1.5, -2.25
v2 = Degenerate.SpanF64()
v2.values = [3.5, -4.75]
v3 = Degenerate.SpanU64()
v3.values = [0xDEADBEEFCAFEBABE, 1]
v4 = Degenerate.SpanI64()
v4.values = [-1234567890123, 42]
v5 = Degenerate.SpanOne()
v5.values = [0x0123456789ABCDEF]
v6 = Degenerate.SpanChunk()
v6.values = [0x1111, 0x2222, 0x3333, 0x4444]
v7 = Degenerate.SpanTail()
v7.values = [6.125, -7.0]
v7.tail = 0xFEEDFACE
v8 = Degenerate.SpanTwice()
v8.a, v8.b = [8.5, 9.5], [-10.5, -11.5]
v9 = Degenerate.Trio()
v9.a, v9.b, v9.c = 0xABCDE, 0x12345, 0xFFFFF
v10 = Degenerate.TrioSole()
v10.inner.a, v10.inner.b, v10.inner.c = 1, 2, 3
v11 = Degenerate.TrioFirst()
v11.inner.a, v11.inner.b, v11.inner.c = 0xAAAAA, 0x55555, 0xF0F0F
v11.trailer = 0xBEEF
v12 = Degenerate.TrioStraddle()
v12.pad0, v12.pad1, v12.pad2 = 0x0011223344556677, 0x8899AABBCCDDEEFF, 0xFFFFFFFFFFFFFFFF
v12.pad3, v12.pad4, v12.pad5 = 0, 0x123456789ABCDEF0, 0xABCDEF
v12.inner.a, v12.inner.b, v12.inner.c = 0x11111, 0x22222, 0x33333
emit("degenerate", [part("degenerate", x) for x in (v1, v2, v3, v4, v5, v6, v7, v8, v9, v10, v11, v12)])

# clauses
cl: list[str] = []


def counted(cls: type, n: int, fill: object) -> None:
    x = cls()
    x.items = [fill(i) for i in range(n)]  # type: ignore[operator]
    cl.append(part("clauses", x))


for n in (0, 1, 3, 4, 5, 7, 12):
    counted(Clauses.W13, n, lambda i: 8191 - i * 733)
for n in (0, 1, 2, 3, 4, 9):
    counted(Clauses.W17, n, lambda i: 131071 - i * 11117)
for n in (0, 1, 2, 3, 6):
    counted(Clauses.W26, n, lambda i: 67108863 - i * 5555555)
for n in (0, 1, 3, 4, 5, 20):
    counted(Clauses.W1, n, lambda i: i % 2)
for n in range(4):
    counted(Clauses.W52, n, lambda i: 4503599627370495 - i * 123456789)
for n in range(4):
    counted(Clauses.W50, n, lambda i: 1125899906842623 - i * 987654321)
f13 = Clauses.F13()
f13.items = [8191 - i * 911 for i in range(7)]
cl.append(part("clauses", f13))
for n in (0, 1, 3, 4, 5, 10):
    a3 = Clauses.ArrTri3()
    a3.items = [Clauses.Tri3() for _ in range(n)]
    for i, t in enumerate(a3.items):
        t.a, t.b = i % 2, i % 4
    cl.append(part("clauses", a3))
ae = Clauses.ArrEleven()
for i, el in enumerate(ae.items):
    el.a, el.b = i % 8, 255 - i * 17
cl.append(part("clauses", ae))
for k in range(3):
    he = Clauses.HoldsEmptyUnion()
    he.lead, he.tail = 21, 99
    he.u.type = (0, Clauses.EmptyUnionType.A, Clauses.EmptyUnionType.B)[k]
    cl.append(part("clauses", he))
st = Clauses.Strs()
st.lead, st.tail = 21, 5
cl.append(part("clauses", st))
full = Clauses.Strs()
full.lead, full.tail, full.s = 21, 5, "abcdefgh"
full.b = bytes(0xF0 + i for i in range(8))
cl.append(part("clauses", full))
pt = Clauses.Strs()
pt.lead, pt.tail, pt.s, pt.b = 21, 5, "xyz", b"\x01\x02\x03"
cl.append(part("clauses", pt))
for n in range(5):
    an = Clauses.ArrNested()
    an.lead, an.tail = 21, 5
    an.items = [Clauses.Eleven() for _ in range(n)]
    for i, el in enumerate(an.items):
        el.a, el.b = i % 8, 200 - i * 7
    cl.append(part("clauses", an))
so = Clauses.Sole()
so.only = 5555
cl.append(part("clauses", so))
emit("clauses", cl)

# joins: an untaken branch reads back as zero values, so equality is not asked
jn: list[str] = []


def join(x: object) -> None:
    jn.append(part("joins", x, exact=False))


for flag in (False, True):
    ag = Joins.ArmsAgree()
    ag.lead, ag.flag, ag.a, ag.b, ag.tail = 21, flag, 1234, 1500, 99
    join(ag)
    dg = Joins.ArmsDisagree()
    dg.lead, dg.flag, dg.a, dg.b, dg.tail = 21, flag, 1234, 5, 99
    join(dg)
    em = Joins.ArmEmpty()
    em.lead, em.flag, em.a, em.tail = 21, flag, 456789, 99
    join(em)
    al = Joins.ArmAlign()
    al.lead, al.flag, al.s, al.b, al.tail = 21, flag, "abcd", 1000, 99
    join(al)
    ale = Joins.ArmAlign()
    ale.lead, ale.flag, ale.b, ale.tail = 21, flag, 1000, 99
    join(ale)
for outer in (False, True):
    for inner in (False, True):
        ne = Joins.ArmsNested()
        ne.lead, ne.outer, ne.inner = 5, outer, inner
        ne.x, ne.y, ne.z, ne.tail = 500000000, 17, 4000, 33
        join(ne)
for flag in (False, True):
    for n in range(4):
        aa = Joins.ArmArray()
        aa.lead, aa.flag, aa.b, aa.tail = 21, flag, 300, 99
        aa.items = [8191 - i * 777 for i in range(n)]
        join(aa)
hu = Joins.HoldsUneven()
hu.lead, hu.tail = 21, 1500
join(hu)
hn = Joins.HoldsUneven()
hn.lead, hn.tail = 21, 1500
hn.u.type = Joins.UnevenType.Narrow
hn.u.narrow.n = 5
join(hn)
hw = Joins.HoldsUneven()
hw.lead, hw.tail = 21, 1500
hw.u.type = Joins.UnevenType.Wide
hw.u.wide.w = 123456789012
join(hw)
for n in range(4):
    au = Joins.ArrUneven()
    au.lead, au.tail = 21, 5
    au.items = [Joins.Uneven() for _ in range(n)]
    for i, un in enumerate(au.items):
        if i % 2 == 0:
            un.type = Joins.UnevenType.Narrow
            un.narrow.n = i % 8
        else:
            un.type = Joins.UnevenType.Wide
            un.wide.w = 99887766554 + i
    join(au)
for n in range(4):
    for sl in (0, 4):
        ra = Joins.RegainAfterAlign()
        ra.lead = 21
        ra.items = [8191 - i * 999 for i in range(n)]
        ra.s = "wxyz" if sl else ""
        ra.p, ra.q, ra.r, ra.tail = 0xDEADBEEF, (1 << 29) - 7, (1 << 19) - 3, 9
        join(ra)
emit("joins", jn)

sys.exit(1 if failed else 0)
