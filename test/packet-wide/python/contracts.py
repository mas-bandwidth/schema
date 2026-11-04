"""Packet wide contracts, the Python leg (test/packet-wide/README.md).

usage: python -I contracts.py <generated dir>

The writer refuses a zero unit, an unpaired surrogate and text past its
bound; the reader refuses an unpaired surrogate; 0xFFFF is a code unit; a
wstring rides unaligned, its length at the bit it starts; an untaken branch
reads back zero; arrays, a counted array born at its minimum, and a selected
union arm compose. Prints FAILED: <what> and exits 1, or prints OK.
"""

import sys

sys.path.insert(0, sys.argv[1])

from wide import InvalidValue as WideInvalid  # noqa: E402
from wide import Refused as WideRefused  # noqa: E402
from wide import WideText as w  # noqa: E402
from wide._schema.serialize import Ref, WriteStream  # noqa: E402
from wideprobe import Shapes as s  # noqa: E402

failed = 0


def check(ok: bool, what: str) -> None:
    global failed
    if not ok:
        print(f"FAILED: {what}")
        failed += 1


def refuses_write(text: str, what: str) -> None:
    v = w.WideSeven()
    v.text = text
    try:
        v.write()
        check(False, what)
    except WideInvalid:
        pass


refuses_write("\x00", "the writer refuses a zero unit")
refuses_write("\ud800", "the writer refuses an unpaired high surrogate")
refuses_write("\udc00", "the writer refuses an unpaired low surrogate")
refuses_write("abcdefgh", "the writer refuses eight units in a wstring(7)")
refuses_write("abcdef\U0001F600", "the writer refuses a pair that takes it past its bound")

crafted = WriteStream(16)
crafted.serialize_bits(Ref(1), 3)
crafted.serialize_bits(Ref(0xD800), 32)
crafted.flush()
try:
    w.WideSeven.read(crafted.data())
    check(False, "the reader refuses an unpaired surrogate")
except WideRefused:
    pass

v = w.WideSeven()
v.text = "￿"
data = v.write()
check(w.WideSeven.read(data).text == "￿", "0xffff is a code unit, and reads back")

c = s.Conditional()
c.enabled = True
c.text = "\U00010000"
cb = c.write()
check(len(cb) * 8 >= 68 and len(cb) == 9, "the conditional is 68 bits")
check(cb[0] & 15 == 5, "the length rides at bit 1, unaligned")
check(s.Conditional.read(cb) == c, "the conditional reads back")
c.enabled = False
check(s.Conditional.read(c.write()) == s.Conditional(), "the untaken side reads back to zero")

box = s.Box()
check(len(box.counted) == 1, "the counted array is born at its minimum")
box.items[0].value = "￿"
box.counted[0].value = "A"
box.choice.type = s.ChoiceType.Text
box.choice.text.value = "B"
back = s.Box.read(box.write())
check(back == box, "the box reads back: arrays, the counted array and the selected arm")

if failed:
    sys.exit(1)
print("packet wide python contracts: writer and reader refusals, alignment, conditional, arrays and union OK")
