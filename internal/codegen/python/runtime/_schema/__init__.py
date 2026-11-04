"""The schema runtime every generated Python package carries.

The errors every generated read and write raises (docs/PORT-PYTHON.md,
ERRORS), the helpers the generated codecs share, and serialize.py, the bit
layer, vendored beside this module in serialize/. The package is emitted
verbatim into every generated package as its _schema subpackage and is
reached only by relative import, so two generated packages never share or
shadow each other's runtime.
"""

import struct
from typing import Final

from .serialize import WriteMisuse, compressed_float_params, float32_round

__all__ = [
    "InvalidValue",
    "Refused",
    "compressed_float",
    "describe",
    "equal",
    "packet_input",
    "packet_bytes",
    "wide_text",
    "wide_units",
]


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
    """A write refused a value outside its contract. No byte of it is returned.

    path names the field (Player.name, ProbeArray.samples.weapon) and rule what
    it broke.
    """

    path: str
    rule: str

    def __init__(self, path: str, rule: str) -> None:
        super().__init__(f"{path}: {rule}")
        self.path = path
        self.rule = rule

    def within(self, owner: str) -> "InvalidValue":
        """The same refusal, its path prefixed with the field or type holding it."""
        return InvalidValue(f"{owner}.{self.path}" if self.path else owner, self.rule)


_F64: Final = struct.Struct("<d")


def equal(a: object, b: object) -> bool:
    """Generated value equality: the same class, and every field equal.

    Floats compare by their bits, so -0.0 and 0.0 differ, as their bytes on
    the wire do. Lists compare element by element; a generated value compares
    every slot.
    """
    if type(a) is not type(b):
        return False
    if isinstance(a, float):
        assert isinstance(b, float)
        return _F64.pack(a) == _F64.pack(b)
    if isinstance(a, list):
        assert isinstance(b, list)
        return len(a) == len(b) and all(equal(x, y) for x, y in zip(a, b))
    slots: object = getattr(type(a), "__slots__", None)
    if isinstance(slots, tuple) and not isinstance(a, (int, str, bytes)):
        return all(equal(getattr(a, n), getattr(b, n)) for n in slots)
    return a == b


def describe(value: object) -> str:
    """A generated value as Name(field=value, ...)."""
    slots: object = getattr(type(value), "__slots__", ())
    names = slots if isinstance(slots, tuple) else ()
    inner = ", ".join(f"{n}={getattr(value, n)!r}" for n in names)
    return f"{type(value).__name__}({inner})"


def packet_input(data: object) -> bytes:
    """A read's input as bytes: bytes, bytearray or a "B"-format memoryview.

    Anything else is refused rather than reinterpreted.
    """
    if isinstance(data, bytes):
        return data
    if isinstance(data, bytearray):
        return bytes(data)
    if isinstance(data, memoryview) and data.format == "B":
        return data.tobytes()
    raise Refused("value_out_of_range", "a read takes bytes, a bytearray or a byte memoryview")


def packet_bytes(value: object, size: int) -> bytes:
    """A bytes(N) field's value as bytes, at most size of them, or WriteMisuse."""
    if isinstance(value, bytes):
        data = value
    elif isinstance(value, bytearray):
        data = bytes(value)
    elif isinstance(value, memoryview) and value.format == "B":
        data = value.tobytes()
    else:
        raise WriteMisuse(f"{value!r} is not bytes, a bytearray or a byte memoryview")
    if len(data) > size:
        raise WriteMisuse(f"{len(data)} bytes do not fit bytes({size})")
    return data


def wide_units(value: object, size: int) -> list[int]:
    """A wstring(N) field's UTF-16 code units, or WriteMisuse.

    The text is a str with no zero code unit and no lone surrogate (which
    UTF-16 cannot encode), at most size units (SPEC §4.12).
    """
    if not isinstance(value, str):
        raise WriteMisuse(f"{value!r} is not a str")
    if "\x00" in value:
        raise WriteMisuse("the text holds a zero code unit")
    try:
        data = value.encode("utf-16-le")
    except UnicodeEncodeError:
        raise WriteMisuse("the text holds a lone surrogate") from None
    units = [data[i] | (data[i + 1] << 8) for i in range(0, len(data), 2)]
    if len(units) > size:
        raise WriteMisuse(f"{len(units)} UTF-16 code units do not fit wstring({size})")
    return units


def wide_text(units: list[int]) -> str | None:
    """Read code units as text: None for a zero unit or an unpaired surrogate."""
    if 0 in units:
        return None
    data = b"".join(u.to_bytes(2, "little") for u in units)
    try:
        return data.decode("utf-16-le")
    except UnicodeDecodeError:
        return None


def compressed_float(min: float, max: float, res: float) -> tuple[int, int, float, float]:
    """A compressed float declaration's precomputed constants.

    (max_integer_value, bits, delta, min) from serialize.py's own float32
    arithmetic, computed once when the module loads, so the steps are every
    port's.
    """
    params = compressed_float_params(float32_round(min), float32_round(max), float32_round(res))
    if params is None:
        raise ValueError(f"({min!r}, {max!r}, {res!r}) is not a compressed float declaration")
    max_integer_value, bits, delta = params
    return max_integer_value, bits, delta, float32_round(min)
