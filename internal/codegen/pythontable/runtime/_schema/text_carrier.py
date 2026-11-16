"""Canonical Base64 carrier for a table's binary wire (§16.8)."""
from __future__ import annotations

import base64
import binascii
import re

from . import Refused

_BASE64 = re.compile(rb"(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?\Z")


def encode_text(wire: bytes) -> str:
    """Encode the complete wire as padded, unwrapped standard Base64."""
    return base64.b64encode(wire).decode("ascii")


def decode_text(text: str) -> bytes:
    """Decode only the exact canonical spelling, or refuse the whole read."""
    if not isinstance(text, str):
        raise Refused("base64_damaged")
    try:
        encoded = text.encode("ascii")
        without_newlines = encoded.replace(b"\r", b"").replace(b"\n", b"")
        if _BASE64.fullmatch(without_newlines) is None:
            raise Refused("base64_damaged")
        wire = base64.b64decode(without_newlines, validate=True)
    except (UnicodeError, ValueError, binascii.Error) as exc:
        raise Refused("base64_damaged") from exc
    if base64.b64encode(wire) != encoded:
        raise Refused("base64_not_canonical")
    return wire
