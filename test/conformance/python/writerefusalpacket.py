# The Python leg's write-refusal-packet probe (docs/SPEC.md section 5.1;
# test/conformance/write-refusal-packet/rows.txt). It builds a fresh lawful
# PacketWriteRefusal, applies the ONE op of each row, calls the generated
# write() and prints "<name> refused" when the write raised InvalidValue (the
# leg's own refusal) and "<name> wrote" when it returned the bytes. It catches
# InvalidValue and nothing else: any other exception is a crash and fails the
# run, never a refusal.
import sys

from packetrefusal import InvalidValue, PacketWriteRefusal


def apply(v, op, arg):
    if op == "scalar":
        v.scalar = int(arg)
    elif op == "narrow":
        v.narrow = int(arg)
    elif op == "grade":
        v.grade = int(arg)
    elif op == "shot_tag":
        v.shot.type = int(arg)
    elif op == "window_count":
        v.window = [0] * int(arg)
    elif op == "text_length":
        v.text = "a" * int(arg)
    elif op == "text_bytes":
        # a str holds code points, not bytes: surrogateescape maps an
        # undecodable byte (ff) to a lone surrogate, the str that is not UTF-8.
        v.text = bytes.fromhex(arg).decode("utf-8", "surrogateescape")
    elif op == "text_fill":
        v.text = "a" * int(arg)
    elif op == "fixed_constant":
        v.fixed_constant = int(arg)
    elif op == "reserved_zero":
        v.reserved_zero = int(arg)
    else:
        raise SystemExit("unknown op " + op)


with open(sys.argv[1], encoding="utf-8") as rows:
    for line in rows:
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        name, _verdict, op, arg = line.split()
        value = PacketWriteRefusal()
        apply(value, op, arg)
        try:
            data = value.write()
        except InvalidValue:
            print(name, "refused")
        else:
            if not isinstance(data, bytes) or len(data) > 256:
                raise SystemExit(name + ": write returned " + repr(data))
            print(name, "wrote")
