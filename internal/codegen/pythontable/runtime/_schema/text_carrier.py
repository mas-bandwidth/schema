"""Canonical Base64 carrier for a table's binary wire (§16.8)."""
from __future__ import annotations

import base64
import binascii

from . import Refused


def encode_text(wire: bytes) -> str:
    """Encode the complete wire as padded, unwrapped standard Base64."""
    return base64.b64encode(wire).decode("ascii")


def decode_text(text: str) -> bytes:
    """Decode only the exact canonical spelling, or refuse the whole read."""
    if not isinstance(text, str):
        raise Refused("base64_damaged")
    try:
        encoded = text.encode("ascii")
        wire = base64.b64decode(encoded, validate=True)
    except (UnicodeError, ValueError, binascii.Error) as exc:
        raise Refused("base64_damaged") from exc
    if base64.b64encode(wire) != encoded:
        raise Refused("base64_not_canonical")
    return wire
