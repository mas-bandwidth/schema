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


def answer(name: str, type_name: str, verdict: str) -> str:
    cls = TYPES.get(type_name)
    if cls is None:
        return "unknown type " + type_name
    with open(name + ".bin", "rb") as stream:
        pin = stream.read()
    if verdict == "refuse":
        try:
            cls.read(pin)
        except Refused:
            return ""
        return "read accepted"
    if verdict != "accept":
        return "verdict " + verdict
    value = build(name)
    if value is None:
        return "unknown accept case"
    if type(value) is not cls:
        return "case type " + type_name + ", the driver builds another"
    got = value.write()
    if got != pin:
        return "write " + got.hex() + ", pin " + pin.hex()
    try:
        back = cls.read(pin)
    except Refused as exc:
        return "read refused: " + exc.reason
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
