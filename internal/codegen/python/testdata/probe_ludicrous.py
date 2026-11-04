"""The ludicrous probe (internal/codegen/python, wire_test.go): examples128.

Fixed point rides as its raw scaled int and the 128-bit kinds as plain ints.
The four C++-pinned goldens are written byte for byte and read back, hostile
offsets and truncation refuse with their reasons, the untaken branch reads
back zero, and a degenerate field holding another value is refused on write.
Prints OK, or FAILED: <what> lines and exits 1.
"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from ludicrous import InvalidValue, Ludicrous, Refused  # noqa: E402

here = os.path.dirname(os.path.abspath(__file__))
failed = False


def check(ok: bool, what: str) -> None:
    global failed
    if not ok:
        print(f"FAILED: {what}")
        failed = True


def golden(name: str) -> bytes:
    with open(os.path.join(here, name + ".bin"), "rb") as f:
        return f.read()


def set_bits(data: bytes, pos: int, n: int) -> bytes:
    b = bytearray(data)
    for i in range(pos, pos + n):
        b[i // 8] |= 1 << (i % 8)
    return bytes(b)


def refused(cls: type, data: bytes, reason: str, what: str) -> None:
    try:
        cls.read(data)  # type: ignore[attr-defined]
        check(False, what + ": read successfully")
    except Refused as e:
        check(e.reason == reason, f"{what}: refused {e.reason}, want {reason}")


Q = 1 << 16  # one whole unit of an F = 16 fixed value


def make_state() -> Ludicrous.LudicrousState:
    v = Ludicrous.LudicrousState()
    v.mode = Ludicrous.DriveMode.Ludicrous
    v.probe.angle = 455 * Q // 10
    v.probe.position = -123461875 * Q // 10000
    v.probe.reach = 1000000 * Q - 1
    v.probe.ticks = 777777
    v.probe.samples = [-8 * Q, 8 * Q]
    v.wide.entity_id = 0x0123456789ABCDEFFEDCBA9876543210
    v.wide.energy = 4999999999
    v.wide.flux = (1 << 99) + 7
    v.keys = [1, 1 << 127]
    v.has_target = True
    v.target_id = 42
    return v


check(Ludicrous.FixedProbe.MAX_BITS == 156, "FixedProbe worst case")
check(Ludicrous.WideProbe.MAX_BITS == 403, "WideProbe worst case")
check(Ludicrous.LudicrousState.MAX_BITS == 1205, "LudicrousState worst case")

z = Ludicrous.LudicrousState()
check(z.mode == Ludicrous.DriveMode.NONE, "mode starts NONE")
check(z.wide.bias == -250 and z.wide.seed == 1 << 65, "the 128-bit defaults")
check(z.keys == [], "keys start empty")

v = make_state()
taken = v.write()
check(taken == golden("ludicrous_state"), "ludicrous_state golden")
back = Ludicrous.LudicrousState.read(taken)
check(back == v, f"LudicrousState reads back {back!r}")
check(back.wide.bias == -250 and back.wide.seed == 1 << 65, "the defaults ride")

v.has_target = False
untaken = v.write()
check(untaken == golden("ludicrous_state_untargeted"), "ludicrous_state_untargeted golden")
check(Ludicrous.LudicrousState.read(untaken).target_id == 0, "the untaken field reads back zero")

refused(Ludicrous.LudicrousState, set_bits(taken, 2, 25), "value_out_of_range", "a smuggled fixed offset")
refused(Ludicrous.LudicrousState, set_bits(taken, 286, 34), "value_out_of_range", "a smuggled int128 offset")
refused(Ludicrous.LudicrousState, taken[:4], "past_end", "truncation")

check(Ludicrous.DegenerateProbe.MAX_BITS == 8, "degenerate fields cost zero bits")
d = Ludicrous.DegenerateProbe()
check(d.locked_fixed == -3 * Q and d.locked_int == 7 and d.locked_wide == -12345678901234, "degenerate defaults")
d.tail = 0xA5
db = d.write()
check(db == golden("degenerate_probe"), "degenerate_probe golden")
check(Ludicrous.DegenerateProbe.read(db) == d, "DegenerateProbe round trip")

check(Ludicrous.UnsignedProbe.MAX_BITS == 196, "UnsignedProbe worst case")
u = Ludicrous.UnsignedProbe()
u.angle = 455 * Q // 10
u.span = 0xFFFFFFFFFFFF0000
u.reach = 131071999999
u.ticks = 777777
u.samples = [0, 16 * Q]
u.tail = 0xA5
check(u.locked == 3 * Q, "the degenerate ufixed default")
ub = u.write()
check(ub == golden("unsigned_probe"), "unsigned_probe golden")
check(Ludicrous.UnsignedProbe.read(ub) == u, "UnsignedProbe round trip")
refused(Ludicrous.UnsignedProbe, set_bits(ub, 25, 64), "value_out_of_range", "a smuggled ufixed high-half offset")
bad = Ludicrous.UnsignedProbe()
bad.locked = 3 * Q + 1
try:
    bad.write()
    check(False, "a wrong degenerate ufixed wrote")
except InvalidValue as e:
    check(e.path == "UnsignedProbe.locked", f"the refusal names {e.path}")

if failed:
    sys.exit(1)
print("OK")
