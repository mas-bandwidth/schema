"""The Python leg of test/packet-void (SPEC §4.8).

Answers every case of cases.txt (testdata/wire/packet-void, copied beside
this file with its pins by TestPythonPacketVoid), one
"case <name> ok|FAILED: <why>" line each; the Go test judges the lines with
internal/casegate. The pins are the Go oracle's bytes, never this writer's.
"""
import os
import sys
from typing import Any

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from packetvoid import (
    Batch,
    Empty,
    Frame,
    InvalidValue,
    Late,
    LateType,
    Mixed,
    MixedType,
    Refused,
    Single,
    SingleType,
    Tags,
    TagsType,
)

TYPES: "dict[str, Any]" = {
    "Mixed": Mixed,
    "Late": Late,
    "Tags": Tags,
    "Single": Single,
    "Empty": Empty,
    "Frame": Frame,
    "Batch": Batch,
}


def mixed(tag: int) -> Mixed:
    value = Mixed()
    value.type = tag
    return value


def build(name: str) -> Any:
    """The value of each accept case, by name; None for a name this driver
    does not know, which is FAILED and never written with another value."""
    if name == "mixed-none":
        return mixed(MixedType.None_)
    if name == "mixed-ping":
        return mixed(MixedType.Ping)
    if name == "mixed-payload":
        return mixed(MixedType.Payload)
    if name in ("late-payload", "late-pong"):
        late = Late()
        late.type = LateType.Payload if name == "late-payload" else LateType.Pong
        return late
    if name in ("tags-none", "tags-ping", "tags-pong"):
        tags = Tags()
        tags.type = {"tags-none": TagsType.None_, "tags-ping": TagsType.Ping, "tags-pong": TagsType.Pong}[name]
        return tags
    if name in ("single-none", "single-ack"):
        single = Single()
        single.type = SingleType.Ack if name == "single-ack" else SingleType.None_
        return single
    if name == "empty-none":
        return Empty()
    if name == "frame-ping":
        frame = Frame()
        frame.lead = 5
        frame.choice = mixed(MixedType.Ping)
        frame.tail = 6
        return frame
    if name in ("batch-empty", "batch-ping", "batch-ping-payload"):
        batch = Batch()
        batch.values = {
            "batch-empty": [],
            "batch-ping": [mixed(MixedType.Ping)],
            "batch-ping-payload": [mixed(MixedType.Ping), mixed(MixedType.Payload)],
        }[name]
        return batch
    return None


def build_refused(name: str) -> Any:
    """The type's default value with the one change each refuse-write case
    names; None for a name this driver does not know, which is FAILED. The
    values are Any: a text tag and an integer payload are exactly what the
    writer must refuse, and Python can hold them."""
    value: Any
    if name.startswith("mixed-write-"):
        value = Mixed()
        if name == "mixed-write-tag-negative":
            value.type = -1
        elif name == "mixed-write-tag-text":
            value.type = "ping"
        elif name == "mixed-write-tag-3":
            value.type = 3
        elif name == "mixed-write-payload-type":
            value.type = MixedType.Payload
            value.payload = 1
        elif name == "mixed-write-marker-8":
            value.type = MixedType.Payload
            value.payload.marker = 8
        else:
            return None
        return value
    if name == "tags-write-tag-3":
        value = Tags()
        value.type = 3
        return value
    if name == "empty-write-tag-1":
        value = Empty()
        value.type = 1
        return value
    return None


def select_payload() -> "tuple[bytes, str]":
    """Reads mixed-ping's pin, then selects the payload arm the Python API's
    way, on the value read: set the tag alone. The union keeps its payload
    object, so the write shows what the read of the void arm left there."""
    with open("mixed-ping.bin", "rb") as stream:
        source = stream.read()
    try:
        value = Mixed.read(source)
    except Refused as exc:
        return b"", "read mixed-ping refused: " + str(exc.reason)
    if value.type != MixedType.Ping:
        return b"", "read mixed-ping: tag " + repr(value.type)
    value.type = MixedType.Payload
    return value.write(), ""


def answer(name: str, type_name: str, verdict: str) -> str:
    cls = TYPES.get(type_name)
    if cls is None:
        return "unknown type " + type_name
    if verdict == "refuse-write":
        bad = build_refused(name)
        if bad is None:
            return "unknown refuse-write case"
        if type(bad) is not cls:
            return "case type " + type_name + ", the driver builds another"
        try:
            wrote = bad.write()
        except InvalidValue:
            return ""
        except Exception as exc:  # the writer's own refusal is InvalidValue, never another error
            return "write raised " + type(exc).__name__ + ": " + str(exc) + ", want InvalidValue"
        return "write accepted: " + bytes(wrote).hex()
    with open(name + ".bin", "rb") as stream:
        pin = stream.read()
    if verdict == "refuse":
        try:
            cls.read(pin)
        except Refused:
            return ""
        return "read accepted"
    if verdict == "select":
        if name != "mixed-ping-select-payload":
            return "unknown select case"
        got, why = select_payload()
        if why:
            return why
        if got != pin:
            return "write " + got.hex() + ", pin " + pin.hex()
        return ""
    if verdict != "accept":
        return "verdict " + verdict
    value = build(name)
    if value is None:
        return "unknown accept case"
    if type(value) is not cls:
        return "case type " + type_name + ", the driver builds another"
    got = bytes(value.write())
    if got != pin:
        return "write " + got.hex() + ", pin " + pin.hex()
    try:
        back = cls.read(pin)
    except Refused as exc:
        return "read refused: " + str(exc.reason)
    if back != value:
        return "read value differs: " + repr(back)
    if back.write() != pin:
        return "write what was read differs"
    return ""


def main() -> None:
    with open("cases.txt", encoding="utf-8") as stream:
        lines = stream.read().splitlines()
    for line in lines:
        fields = line.split()
        if not fields or fields[0].startswith("#"):
            continue
        if len(fields) < 4:
            print("case " + fields[0] + " FAILED: a case line needs <name> <type> <verdict> <bits>")
            continue
        why = answer(fields[0], fields[1], fields[2])
        print("case " + fields[0] + (" FAILED: " + why if why else " ok"))


main()
