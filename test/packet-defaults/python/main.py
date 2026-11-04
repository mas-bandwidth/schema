"""The Python packet-defaults gate (test/packet-defaults/README.md).

usage: python -I main.py <generated dir> <pin dir>

Loads packetdefaults (Defaults.schema) and packetplain (Plain.schema) from the
generated directory and checks: constructors at the declared string, bytes and
flags defaults, explicit-empty and short literals, the bit-63 and full 64-bit
masks, the 33-bit mask between other fields, the defaultless twin writing the
same bytes, counted arrays born at their minimum, and the eight C++ byte and
bit pins of testdata/wire/packet-defaults written and read back. A read always
builds a fresh value, so every union selection takes fresh declared defaults
before the received fields overlay them, and an untaken branch reads back as
zero values.

Prints FAILED: <what> for each failure and exits 1, or prints OK.
"""

import os
import sys

sys.path.insert(0, sys.argv[1])

from packetdefaults import Defaults as d  # noqa: E402
from packetdefaults import Refused  # noqa: E402
from packetplain import Plain as p  # noqa: E402

pins = sys.argv[2]
failed = 0


def check(ok: bool, what: str) -> None:
    global failed
    if not ok:
        print(f"FAILED: {what}")
        failed += 1


def sample() -> d.Sample:
    """Sample at the declared defaults, set field by field."""
    v = d.Sample()
    v.name, v.token, v.caps = "é\U00010000", b"\\n\\t", d.Caps.Jump | d.Caps.Fly
    v.empty_name, v.empty_token, v.empty_caps = "", b"", 0
    return v


def zero() -> d.Sample:
    v = d.Sample()
    v.name, v.token, v.caps = "", b"", 0
    v.empty_name, v.empty_token, v.empty_caps = "", b"", 0
    return v


def short() -> d.Sample:
    v = zero()
    v.name, v.token, v.caps = "A", b"\x00\xff", d.Caps.Crouch
    return v


def fields(v: object) -> dict[str, object]:
    return {n: getattr(v, n) for n in type(v).__slots__}  # type: ignore[attr-defined]


def pin(name: str, value: object, want: object) -> None:
    with open(os.path.join(pins, name + ".bin"), "rb") as f:
        data = f.read()
    with open(os.path.join(pins, name + ".bits")) as f:
        bits = int(f.read().split()[0])
    got: bytes = value.write()  # type: ignore[attr-defined]
    check(got == data, f"C++ pin {name}: bytes {got.hex()}, want {data.hex()}")
    check(len(data) == (bits + 7) // 8, f"C++ pin {name}: {len(data)} bytes hold {bits} bits")
    cls = type(value)
    try:
        back = cls.read(data)  # type: ignore[attr-defined]
        check(back == want, f"C++ pin {name}: read {back!r}, want {want!r}")
    except Refused as e:
        check(False, f"C++ pin {name}: read refused {e.reason}")
    if bits % 8:
        # the first bit past the value is padding: setting it must refuse,
        # which holds the read to exactly `bits` bits
        poisoned = bytearray(data)
        poisoned[bits // 8] |= 1 << (bits % 8)
        try:
            cls.read(bytes(poisoned))  # type: ignore[attr-defined]
            check(False, f"C++ pin {name}: a set bit after the {bits} bits read successfully")
        except Refused as e:
            check(e.reason == "nonzero_padding", f"C++ pin {name}: a set bit after the value refused {e.reason}")


check(fields(d.Sample()) == fields(sample()), "packet-default constructor values")
e = d.EmptyOnly()
check((e.name, e.token, e.caps) == ("", b"", 0), "explicit empty defaults")
pr = d.Prefix()
check((pr.name, pr.token) == ("é", b"\\n"), "short literals")

wide = d.WideMask()
check(wide.high == 1 << 63, "WideMask high is bit 63")
check(wide.all == (1 << 64) - 1, "WideMask all is every bit")
wb = wide.write()
check(wb == bytes.fromhex("0000000000000080ffffffffffffffff"), f"WideMask wire {wb.hex()}")
check(d.WideMask.read(wb) == wide, "WideMask read")

split = d.SplitMask()
split.lead, split.mask, split.tail = 5, 1 << 32, 2
sb = split.write()
check(sb == bytes.fromhex("0500000028"), f"SplitMask wire {sb.hex()}")
check(d.SplitMask.read(sb) == split, "SplitMask read")

twin = p.Sample()
twin.name, twin.token, twin.caps = "é\U00010000", b"\\n\\t", p.Caps.Jump | p.Caps.Fly
check(twin.write() == d.Sample().write(), "defaultless twin")

pin("sample-defaults", d.Sample(), sample())

batch = d.Batch()
check(len(batch.counted) == 1, "Batch counted is born at its minimum count")
check(fields(batch.head) == fields(sample()), "Batch head at its defaults")
check(all(fields(x) == fields(sample()) for x in batch.items + batch.counted), "Batch elements at their defaults")
check(batch.items[0] is not batch.items[1], "Batch elements are distinct objects")
pin("batch-defaults", batch, d.Batch())

zc = d.ZeroCount()
check(zc.items == [], "ZeroCount is born at count zero")
pin("zero-count", zc, d.ZeroCount())

on = d.Conditional()
check(on.enabled is True and fields(on.value) == fields(sample()), "Conditional construction")
pin("conditional-on", on, d.Conditional())
off = d.Conditional()
off.enabled = False
off_want = d.Conditional()
off_want.enabled = False
off_want.value = zero()
pin("conditional-off", off, off_want)

envelope = d.ChoiceEnvelope()
envelope.choice.type = d.ChoiceType.Sample
pin("choice-sample", envelope, envelope)

pin("sample-short", short(), short())
pin("sample-empty", zero(), zero())

# a selected arm takes fresh declared defaults, then the received fields: a
# Conditional arm with enabled false reads back its value as zero values
for sent in (short(), zero()):
    ch = d.ChoiceEnvelope()
    ch.choice.type = d.ChoiceType.Sample
    ch.choice.sample = sent
    back = d.ChoiceEnvelope.read(ch.write())
    check(back.choice.type == d.ChoiceType.Sample and fields(back.choice.sample) == fields(sent), "Sample selection")
    check(fields(back.choice.conditional.value) == fields(sample()), "the unselected arm keeps its fresh defaults")
oc = d.ChoiceEnvelope()
oc.choice.type = d.ChoiceType.Conditional
oc.choice.conditional.enabled = False
back = d.ChoiceEnvelope.read(oc.write())
check(back.choice.conditional.enabled is False and fields(back.choice.conditional.value) == fields(zero()),
      "an untaken branch inside a selected arm reads back zero")

if failed:
    sys.exit(1)
print("OK")
