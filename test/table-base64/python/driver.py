"""Exercise a generated Python JSON codec through the shared Base64 harness."""
import sys

sys.path.insert(0, sys.argv[1])
from base64test import Blob, Refused  # noqa: E402


def packed(data: bytes) -> str:
    return data.hex() if data else "-"


for line in sys.stdin.buffer:
    try:
        text = bytes.fromhex(line.decode("ascii")).decode("utf-8")
        value = Blob.from_json(text)
    except (Refused, UnicodeError):
        print("1 - -")
    else:
        print("0", packed(value.payload), packed(value.to_json().encode("utf-8")))
