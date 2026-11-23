"""Check packet construction and serialization against the C++ default pins."""
import glob
import importlib
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from packetdefaults import Batch, Caps, Choice, ChoiceType, Conditional, Sample, ZeroCount
from example import ProbeConfig, Weapon
from ludicrous import DegenerateProbe


def check(condition, name):
    if not condition:
        raise AssertionError(name)


pins_read = set()


def pin(name, value):
    pins_read.add(name)
    with open(name + ".bin", "rb") as f:
        data = f.read()
    check(value.write() == data, name + " constructor wire")
    check(type(value).read(data).write() == data, name + " round trip")


def constant_pin(name):
    """A fresh Value (defaults from constants) writes the pinned bytes, and the
    pin reads back to a fresh Value. Both constant schemas declare package
    arithmetic, so each is generated into its own tree (constant-<name>/) and
    its modules are purged from the import cache after the check."""
    here = os.path.dirname(os.path.abspath(__file__))
    tree = os.path.join(here, "constant-" + name)
    check(os.path.isdir(tree), "the generated tree constant-" + name + " exists")
    sys.path.insert(0, tree)
    try:
        value_type = importlib.import_module("arithmetic.Defaults").Value
        fresh = value_type()
        with open(name + ".bin", "rb") as f:
            data = f.read()
        pins_read.add(name)
        check(len(data) > 0, name + " pin is not empty")
        check(fresh.write() == data, name + " constructor wire")
        check(value_type.read(data) == fresh, name + " reads back to a fresh Value")
        check(value_type.read(data).write() == data, name + " round trip")
    finally:
        sys.path.remove(tree)
        for module in [m for m in sys.modules if m == "arithmetic" or m.startswith("arithmetic.")]:
            del sys.modules[module]


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
constant_pin("hexadecimal-addition")
constant_pin("unsigned-subtraction")
# Every .bin handed to the driver must have been read: a pin the driver does not
# know fails loudly, never skipped.
bins = sorted(os.path.basename(f)[:-4] for f in glob.glob("*.bin"))
check(len(bins) > 0, "the pins directory is not empty")
for name in bins:
    check(name in pins_read, "pin " + name + " was never read by the driver")
print("ok")
