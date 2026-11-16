"""CRC-32C trailer for a root's variable table file (SPEC-TABLES §2.14)."""

from . import Refused
from .load import buffer_view


def _table() -> tuple[int, ...]:
    values = []
    for n in range(256):
        crc = n
        for _ in range(8):
            crc = (crc >> 1) ^ (0x82F63B78 if crc & 1 else 0)
        values.append(crc)
    return tuple(values)


_TABLE = _table()


def crc32c(data: bytes | memoryview) -> int:
    value = 0xFFFFFFFF
    for byte in data:
        value = _TABLE[(value ^ byte) & 0xFF] ^ (value >> 8)
    return value ^ 0xFFFFFFFF


def seal_crc(data: bytes) -> bytes:
    return data + crc32c(data).to_bytes(4, "little")


def open_crc(data: bytes | bytearray | memoryview, form: int = 1) -> memoryview:
    view = buffer_view(data)
    # Form recognition precedes the trailer. Each form keeps its own names.
    if not view:
        raise Refused("truncated" if form == 3 else "wire_damaged", "empty file")
    if view[0] != form:
        if form == 1:
            reason = "message_form_as_file" if view[0] == 2 else "fixed_form_as_file" if view[0] == 3 else "newer_form"
        elif form == 3:
            reason = "previous_form" if view[0] == 1 else "message_form_as_file" if view[0] == 2 else "newer_form"
        else:
            reason = "newer_form"
        raise Refused(reason, "wrong form")
    if len(view) < (6 if form == 2 else 5):
        raise Refused("integrity_mismatch", "file is shorter than its checksum")
    body = view[:-4]
    if int.from_bytes(view[-4:], "little") != crc32c(body):
        raise Refused("integrity_mismatch", "CRC-32C does not match")
    return body
