"""Check packet construction and serialization against the C++ default pins."""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from packetdefaults import Batch, Caps, Choice, ChoiceType, Conditional, Sample, ZeroCount
from example import ProbeConfig, Weapon
from ludicrous import DegenerateProbe


def check(condition, name):
    if not condition:
        raise AssertionError(name)


def pin(name, value):
    with open(name + ".bin", "rb") as f:
        data = f.read()
    check(value.write() == data, name + " constructor wire")
    check(type(value).read(data).write() == data, name + " round trip")


config = ProbeConfig()
check(config.retries == -1 and config.preferred == Weapon.Railgun, "scalar and enum defaults")
fixed = DegenerateProbe()
check(fixed.locked_fixed == (-3 << 16) and fixed.locked_int == 7 and fixed.locked_wide == -12345678901234, "fixed and integer defaults")
sample = Sample()
check(sample.name == "é𐀀" and sample.token == b"\\n\\t", "constructor text bytes")
check(sample.caps == Caps.Jump | Caps.Fly, "constructor flags")
check(sample.empty_name == "" and sample.empty_token == b"" and sample.empty_caps == 0, "empty defaults")
pin("sample-defaults", sample)
empty = Sample()
empty.name, empty.token, empty.caps = "", b"", 0
pin("sample-empty", empty)
short = Sample()
short.name, short.token, short.caps = "A", b"\x00\xff", Caps.Crouch
pin("sample-short", short)
batch = Batch()
check(batch.head == sample and all(x == sample for x in batch.items), "fixed defaults")
check(len(batch.counted) == 1 and batch.counted[0] == sample, "counted defaults")
pin("batch-defaults", batch)
zero = ZeroCount()
check(len(zero.items) == 0, "zero count")
pin("zero-count", zero)
on = Conditional()
check(on.enabled and on.value == sample, "conditional defaults")
pin("conditional-on", on)
off = Conditional()
off.enabled = False
pin("conditional-off", off)
choice = Choice()
choice.type = ChoiceType.Sample
pin("choice-sample", choice)
print("ok")
