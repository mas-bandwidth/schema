"""The schema runtime every generated Python package carries.

The errors every generated read and write raises (docs/PORT-PYTHON.md,
ERRORS), the value checks the generated writers share, and serialize.py, the
bit layer, vendored beside this module in serialize/. The package is emitted
verbatim into every generated package as its _schema subpackage and is reached
only by relative import, so two generated packages never share or shadow each
other's runtime.
"""

import struct

__all__ = ["InvalidValue", "Refused"]


class Refused(ValueError):
    """A read refused its input. It carries no value.

    reason is the refusal's name (past_end, non_finite_float, ...), a
    diagnostic that is never compared across languages; only the verdict is.
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


def validate_int(value: object, path: str, min: int, max: int) -> None:
    """Hold an integer field to its declared range (the value mapping).

    type(value) is int, so a bool is refused where an integer is declared.
    """
    if type(value) is not int:
        raise InvalidValue(path, "expected int")
    if value < min or value > max:
        raise InvalidValue(path, f"{value} is outside [{min}, {max}]")


def validate_bool(value: object, path: str) -> None:
    """Hold a bool field to True or False; 0 and 1 are refused."""
    if type(value) is not bool:
        raise InvalidValue(path, "expected bool")


def wire_input(data: object) -> bytes:
    """The read entry point's accepted inputs: bytes, bytearray or a "B"
    memoryview, copied so the value never pins the caller's buffer."""
    if isinstance(data, bytes):
        return data
    if isinstance(data, bytearray):
        return bytes(data)
    if isinstance(data, memoryview):
        if data.format != "B":
            raise TypeError(f"a memoryview read needs format 'B', got {data.format!r}")
        return data.tobytes()
    raise TypeError(f"a read takes bytes, bytearray or a 'B' memoryview, got {type(data).__name__}")


def float32_bits_eq(a: float, b: float) -> bool:
    """Equality by the float32 bits, so -0.0 and 0.0 differ."""
    return struct.pack("<f", a) == struct.pack("<f", b)


def float64_bits_eq(a: float, b: float) -> bool:
    """Equality by the float64 bits, so -0.0 and 0.0 differ."""
    return struct.pack("<d", a) == struct.pack("<d", b)
