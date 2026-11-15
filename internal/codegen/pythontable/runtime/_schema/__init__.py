"""The schema table runtime every generated Python package carries.

The errors every generated read and write raises (docs/PORT-PYTHON.md,
ERRORS), the load report, and the form-1 table codec that walks a descriptor.
The package is emitted verbatim into every generated package as its _schema
subpackage and is reached only by relative import, so two generated packages
never share or shadow each other's runtime.
"""

import struct
from typing import Final

__all__: Final = ["InvalidValue", "LoadReport", "Refused"]


class Refused(ValueError):
    """A read refused its input. It carries no value.

    reason is the refusal's name (past_end, wire_damaged, ...), a diagnostic
    that is never compared across languages; only the verdict is.
    """

    reason: str

    def __init__(self, reason: str, detail: str = "") -> None:
        super().__init__(f"{reason}: {detail}" if detail else reason)
        self.reason = reason


class InvalidValue(ValueError):
    """A write refused a value outside its contract, before writing a byte.

    path names the field (Player.name, Fleet.ships[3].hull) and rule what it
    broke.
    """

    path: str
    rule: str

    def __init__(self, path: str, rule: str) -> None:
        super().__init__(f"{path}: {rule}")
        self.path = path
        self.rule = rule


class LoadReport:
    """The counters a successful load fills (docs/PORT-PYTHON.md, ERRORS).

    A refused load leaves the report exactly as it was; the whole value or
    Refused is the one read.
    """

    __slots__ = ("unknown", "widened", "retained", "retain_lost")

    unknown: int
    widened: int
    retained: int
    retain_lost: int

    def __init__(self) -> None:
        self.reset()

    def reset(self) -> None:
        self.unknown = 0
        self.widened = 0
        self.retained = 0
        self.retain_lost = 0


def _validate_int(value: object, path: str, min: int, max: int) -> None:
    """Hold an integer field to its declared range (the value mapping).

    type(value) is int, so a bool is refused where an integer is declared.
    """
    if type(value) is not int:
        raise InvalidValue(path, "expected int")
    if value < min or value > max:
        raise InvalidValue(path, f"{value} is outside [{min}, {max}]")


def _validate_bool(value: object, path: str) -> None:
    """Hold a bool field to True or False; 0 and 1 are refused."""
    if type(value) is not bool:
        raise InvalidValue(path, "expected bool")


def _wire_input(data: object) -> bytes:
    """The packet read entry point's accepted inputs, copied so the value never
    pins the caller's buffer. A hosted packet class carries this codec beside
    the table codec (docs/PORT-PYTHON.md, THE TABLE FRAME)."""
    if isinstance(data, bytes):
        return data
    if isinstance(data, bytearray):
        return bytes(data)
    if isinstance(data, memoryview):
        if data.format != "B":
            raise TypeError(f"a memoryview read needs format 'B', got {data.format!r}")
        return data.tobytes()
    raise TypeError(f"a read takes bytes, bytearray or a 'B' memoryview, got {type(data).__name__}")


def _float32_bits_eq(a: object, b: object) -> bool:
    """Equality by the float32 bits, so -0.0 and 0.0 differ."""
    if type(a) is not float or type(b) is not float:
        return a == b
    try:
        return struct.pack("<f", a) == struct.pack("<f", b)
    except OverflowError:
        return struct.pack("<d", a) == struct.pack("<d", b)


def _float64_bits_eq(a: object, b: object) -> bool:
    """Equality by the float64 bits, so -0.0 and 0.0 differ."""
    if type(a) is not float or type(b) is not float:
        return a == b
    return struct.pack("<d", a) == struct.pack("<d", b)
