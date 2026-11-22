"""Exercise a generated Python JSON codec through the shared Base64 harness."""
import sys

sys.path.insert(0, sys.argv[1])
from base64test import Blob, Refused  # noqa: E402


def packed(data: bytes) -> str:
    return data.hex() if data else "-"


for line in sys.stdin.buffer:
    raw = bytes.fromhex(line.decode("ascii"))
    # Ill-formed UTF-8 input is refused here, before the codec. Inside the
    # codec only its own Refused is a refusal: a UnicodeError raised there is
    # a codec bug, and must crash the driver rather than read as a verdict.
    try:
        text = raw.decode("utf-8")
    except UnicodeError:
        print("1 - -")
        continue
    try:
        value = Blob.from_json(text)
    except Refused:
        print("1 - -")
    else:
        print("0", packed(value.payload), packed(value.to_json().encode("utf-8")))
