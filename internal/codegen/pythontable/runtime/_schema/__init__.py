"""The schema table runtime every generated Python package carries.

The errors every generated read and write raises (docs/PORT-PYTHON.md,
ERRORS), the load report, and the form-1 table codec that walks a descriptor.
The package is emitted verbatim into every generated package as its _schema
subpackage and is reached only by relative import, so two generated packages
never share or shadow each other's runtime.
"""

import math
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



def _validate_array(value: object, path: str, min: int, max: int) -> None:
    """Hold an array field to a list whose length is in [min, max]: exactly N
    for [N]T, the count's range for [A..B]T."""
    if type(value) is not list:
        raise InvalidValue(path, "expected list")
    if len(value) < min or len(value) > max:
        if min == max:
            raise InvalidValue(path, f"{len(value)} elements, expected {max}")
        raise InvalidValue(path, f"{len(value)} elements is outside [{min}, {max}]")


def _validate_string(value: object, path: str, size: int) -> None:
    """Hold a string(N) field to its contract (docs/PORT-PYTHON.md, TEXT AND
    BYTES): a str, no NUL, encodable as UTF-8 (a lone surrogate is not), and
    at most size bytes of UTF-8. A valid value returns; the generated writer
    calls this for every value its inline ASCII check does not pass.
    """
    if type(value) is not str:
        raise InvalidValue(path, "expected str")
    if "\x00" in value:
        raise InvalidValue(path, "holds a NUL")
    try:
        data = value.encode("utf-8")
    except UnicodeEncodeError:
        raise InvalidValue(path, "is not encodable as UTF-8 (a lone surrogate)") from None
    if len(data) > size:
        raise InvalidValue(path, f"{len(data)} bytes of UTF-8 exceed the bound {size}")


def _validate_bytes(value: object, path: str, size: int) -> None:
    """Hold a bytes(N) field to its contract: bytes, a bytearray or a
    one-dimensional "B" memoryview, of at most size bytes. Any other
    memoryview is refused rather than reinterpreted. A valid value returns.
    """
    if isinstance(value, memoryview):
        if value.format != "B" or value.ndim != 1:
            raise InvalidValue(path, f"a memoryview needs format 'B' and one dimension, got {value.format!r} in {value.ndim}")
        n = value.nbytes
    elif type(value) is bytes or type(value) is bytearray:
        n = len(value)
    else:
        raise InvalidValue(path, "expected bytes, bytearray or a 'B' memoryview")
    if n > size:
        raise InvalidValue(path, f"{n} bytes exceed the bound {size}")

def _validate_bool(value: object, path: str) -> None:
    """Hold a bool field to True or False; 0 and 1 are refused."""
    if type(value) is not bool:
        raise InvalidValue(path, "expected bool")


def _finite_float32(value: object, path: str) -> float:
    """value rounded to float32, or InvalidValue: not a float, not finite, or
    a finite double that rounds past the largest float32."""
    if type(value) is not float:
        raise InvalidValue(path, "expected float")
    if not math.isfinite(value):
        raise InvalidValue(path, f"{value!r} is not finite")
    try:
        rounded: float = struct.unpack("<f", struct.pack("<f", value))[0]
    except OverflowError:
        raise InvalidValue(path, f"{value!r} rounds to an infinity in float32") from None
    return rounded


def _validate_float32(value: object, path: str) -> None:
    """Hold a float32 field to a finite float that rounds to a finite float32.

    Rounding is not a refusal: a double rides as its nearest float32. An int
    is refused where a float is declared.
    """
    _finite_float32(value, path)


def _validate_float64(value: object, path: str) -> None:
    """Hold a float64 field to a finite float; an int is refused."""
    if type(value) is not float:
        raise InvalidValue(path, "expected float")
    if not math.isfinite(value):
        raise InvalidValue(path, f"{value!r} is not finite")


def _validate_compressed_float(value: object, path: str, min: float, max: float) -> None:
    """Hold a compressed float32 field to a finite float whose float32 value is
    inside the declared [min, max] (min and max are float32 values)."""
    rounded = _finite_float32(value, path)
    if rounded < min or rounded > max:
        raise InvalidValue(path, f"{value!r} is outside [{min!r}, {max!r}]")



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
