import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from packetvoidpython import (
    Empty,
    EmptyType,
    Frame,
    InvalidValue,
    Mixed,
    MixedType,
    Payload,
    Refused,
    Single,
    SingleType,
    Tags,
    TagsType,
)


def fail(message: str) -> None:
    print("FAILED: " + message)
    raise SystemExit(1)


def read_goldens() -> "dict[str, bytes]":
    cases = {}
    with open("goldens.txt", encoding="utf-8") as stream:
        for line in stream:
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            name, wire = line.split()
            cases[name] = b"" if wire == "--" else bytes.fromhex(wire)
    return cases


def build(name: str) -> object:
    if name == "mixed-none":
        return Mixed()
    if name == "mixed-ping":
        value = Mixed()
        value.type = MixedType.Ping
        return value
    if name == "mixed-pong":
        value = Mixed()
        value.type = MixedType.Pong
        return value
    if name == "mixed-payload":
        value = Mixed()
        value.type = MixedType.Payload
        return value
    if name == "frame-ping":
        value = Frame()
        value.lead = True
        value.choice.type = MixedType.Ping
        value.tail = True
        return value
    if name == "tags-none":
        return Tags()
    if name == "tags-pong":
        value = Tags()
        value.type = TagsType.Pong
        return value
    if name == "single-ack":
        value = Single()
        value.type = SingleType.Ack
        return value
    if name == "empty-none":
        return Empty()
    fail("unknown golden " + name)
    raise SystemExit(1)


def expect_invalid(label: str, value: object) -> None:
    try:
        value.write()
    except InvalidValue:
        return
    fail("write " + label + " was accepted")


def expect_refused(label: str, cls: object, data: bytes) -> None:
    try:
        cls.read(data)
    except Refused:
        return
    fail("read " + label + " was accepted")


def main() -> None:
    if MixedType.None_ != 0 or MixedType.Ping != 1 or MixedType.Payload != 2 or MixedType.Pong != 3 or MixedType.Max != 3:
        fail("MixedType tag numbers")
    if MixedType.NAMES != {0: "None", 1: "ping", 2: "payload", 3: "pong"}:
        fail("MixedType name table")
    if TagsType.Max != 2 or TagsType.Pong != 2 or SingleType.Max != 1 or SingleType.Ack != 1 or EmptyType.Max != 0:
        fail("payload-free tag extents")
    if Mixed.__slots__ != ("type", "payload") or Tags.__slots__ != ("type",):
        fail("union slots")

    fresh = Mixed()
    if fresh.type != MixedType.None_ or not isinstance(fresh.payload, Payload):
        fail("union construction")
    if fresh.payload.count != -1 or fresh.payload.marker is not True:
        fail("payload arm declared defaults")
    if Frame().choice.type != MixedType.None_:
        fail("union field construction")

    for name, wire in read_goldens().items():
        value = build(name)
        got = value.write()
        if got != wire:
            fail("write " + name + ": got " + got.hex() + " want " + wire.hex())
        try:
            back = type(value).read(wire)
        except Refused as exc:
            fail("read " + name + " was refused: " + exc.reason)
        if back != value:
            fail("read " + name + " value differs: " + repr(back))
        again = back.write()
        if again != wire:
            fail("read " + name + " write-back differs")
        if name.startswith("mixed-"):
            payload = back.payload
            if not isinstance(payload, Payload) or payload.count != -1 or payload.marker is not True:
                fail("mixed arm defaults after " + name)

    # A union read is a fresh value, so an unselected payload is at its
    # declared defaults.
    selected = Mixed.read(b"\x01")
    if selected.type != MixedType.Ping or selected.payload.count != -1 or selected.payload.marker is not True:
        fail("payload defaults after a void selection")

    bad = Mixed()
    bad.type = 4
    expect_invalid("mixed-tag-4", bad)
    bad = Mixed()
    bad.type = -1
    expect_invalid("mixed-tag-negative", bad)
    bad = Mixed()
    bad.type = True
    expect_invalid("mixed-tag-bool", bad)
    bad = Mixed()
    bad.type = MixedType.Payload
    bad.payload = 1
    expect_invalid("mixed-payload-type", bad)
    bad = Mixed()
    bad.type = MixedType.Payload
    bad.payload.count = 40000
    expect_invalid("mixed-payload-range", bad)
    bad = Mixed()
    bad.type = MixedType.Payload
    bad.payload.marker = 1
    expect_invalid("mixed-payload-bool", bad)
    bad = Tags()
    bad.type = 3
    expect_invalid("tags-tag-3", bad)
    bad = Empty()
    bad.type = 1
    expect_invalid("empty-tag", bad)

    # Mixed's 2-bit tag uses all four values, so it has no unused tag: 0x04 is
    # tag 0 with a set bit past the value.
    expect_refused("mixed-trailing-bit", Mixed, b"\x04")
    expect_refused("mixed-empty", Mixed, b"")
    expect_refused("mixed-short-payload", Mixed, bytes.fromhex("feff"))
    expect_refused("mixed-trailing", Mixed, bytes.fromhex("0100"))
    expect_refused("frame-empty", Frame, b"")
    expect_refused("tags-tag-3", Tags, b"\x03")
    expect_refused("tags-trailing-bit", Tags, b"\x04")
    expect_refused("single-empty", Single, b"")
    expect_refused("empty-trailing", Empty, b"\x00")

    if Empty().write() != b"" or Empty.read(b"") != Empty():
        fail("empty union wire")
    print("ok")


main()
