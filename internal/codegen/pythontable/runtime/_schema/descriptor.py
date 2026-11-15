"""The descriptor vocabulary the generated table classes carry.

A field's descriptor names what a generic walk needs: its schema name, its
table-wire id and kind, the codec that carries it, its declared range, and the
table (or union) whose value it holds. An ARRAY carries its element's own
descriptor under ``elem``, and an ENUM carries its variant names and wire ids
and the ordinals of its variants marked ``| deprecated`` under ``retired``.
A table's descriptor holds its own type id and its fields by declaration order
and by wire id. The values are DATA, built once at import, so the codec
modules only ever read them.
"""

from __future__ import annotations

import struct
from typing import Any


class FieldDesc:
    """One field of a generated table (docs/SPEC-TABLES.md §8.1)."""

    __slots__ = (
        "name",
        "field_id",
        "kind",
        "codec",
        "ordinal",
        "lo",
        "hi",
        "target",
        "map_entry",
        "pointer",
        "optional",
        "array",
        "bound",
        "key_enum",
        "default",
        "elem",
        "variants",
        "retired",
        "f32",
        "fmin",
        "fmax",
        "float_range",
        "array_kind",
        "array_min",
    )

    name: str
    field_id: int
    kind: int
    codec: str
    ordinal: int
    lo: int
    hi: int
    target: str
    map_entry: str
    pointer: bool
    optional: bool
    array: bool
    bound: int
    key_enum: str
    default: Any
    elem: FieldDesc | None
    variants: tuple[tuple[str, int], ...]
    retired: frozenset[int]
    f32: bool
    fmin: float
    fmax: float
    float_range: bool
    array_kind: str
    array_min: int

    def __init__(
        self,
        name: str,
        field_id: int,
        kind: int,
        codec: str,
        ordinal: int = 0,
        lo: int = 0,
        hi: int = 0,
        target: str = "",
        map_entry: str = "",
        pointer: bool = False,
        optional: bool = False,
        array: bool = False,
        bound: int = 0,
        key_enum: str = "",
        default: Any = None,
        elem: FieldDesc | None = None,
        variants: tuple[tuple[str, int], ...] = (),
        retired: tuple[int, ...] = (),
        f32: bool = False,
        fmin: float = 0.0,
        fmax: float = 0.0,
        float_range: bool = False,
        array_kind: str = "",
        array_min: int = 0,
    ) -> None:
        self.name = name
        self.field_id = field_id
        self.kind = kind
        self.codec = codec
        self.ordinal = ordinal
        self.lo = lo
        self.hi = hi
        self.target = target
        self.map_entry = map_entry
        self.pointer = pointer
        self.optional = optional
        self.array = array
        self.bound = bound
        self.key_enum = key_enum
        self.default = default
        self.elem = elem
        self.variants = variants
        self.retired = frozenset(retired)
        self.f32 = f32
        self.fmin = fmin
        self.fmax = fmax
        self.float_range = float_range
        self.array_kind = array_kind
        self.array_min = array_min

    def is_default(self, value: Any) -> bool:
        """Whether the writer elides this field (docs/SPEC-TABLES.md §3)."""
        if self.pointer or self.optional:
            return value is None
        if self.codec == "array":
            return _array_is_default(self, value)
        if self.codec == "bytes":
            return _bytes_equal(value, self.default)
        if self.codec == "string":
            return bool(value == self.default)
        if self.codec in ("bool", "int", "enum", "unsupported"):
            return bool(value == self.default)
        if self.codec == "float32":
            return _float32_is_default(self, value)
        if self.codec == "float64":
            return _float64_is_default(self, value)
        if self.codec == "table":
            # A BY-VALUE TABLE IS NEVER ELIDED HERE. Its elision is decided by
            # its encoded body, as the engine decides it
            # (internal/tablewire/encode.go, kind 13): the body is written,
            # and it is dropped, with every id it interned, exactly when it
            # is its terminator alone (kind_table.py). No value compare, and
            # so no __eq__, takes part.
            return False
        return bool(value == self.default)


def _bytes_equal(value: Any, default: Any) -> bool:
    try:
        return bytes(value) == bytes(default)
    except (TypeError, ValueError):
        return False


def _float32_bits(value: float) -> bytes:
    return struct.pack("<f", value)


def _float64_bits(value: float) -> bytes:
    return struct.pack("<d", value)


def _float32_is_default(fd: FieldDesc, value: Any) -> bool:
    if type(value) is not float:
        return False
    if fd.float_range:
        return float(struct.unpack("<f", _float32_bits(value))[0]) == float(
            struct.unpack("<f", _float32_bits(fd.default))[0]
        )
    return _float32_bits(value) == _float32_bits(fd.default)


def _float64_is_default(fd: FieldDesc, value: Any) -> bool:
    if type(value) is not float:
        return False
    if fd.float_range:
        return bool(value == fd.default)
    return _float64_bits(value) == _float64_bits(fd.default)


def _elem_is_default(fd: FieldDesc, value: Any) -> bool:
    if fd.codec == "table":
        return False
    if fd.codec == "bytes":
        return _bytes_equal(value, fd.default)
    if fd.codec == "string":
        return bool(value == fd.default)
    if fd.codec in ("bool", "int", "enum"):
        return bool(value == fd.default)
    if fd.codec == "float32":
        return _float32_is_default(fd, value)
    if fd.codec == "float64":
        return _float64_is_default(fd, value)
    return False


def _array_is_default(fd: FieldDesc, value: Any) -> bool:
    if not isinstance(value, list):
        return False
    if fd.array_kind != "fixed":
        return len(value) == 0
    elem = fd.elem
    if elem is None:
        return False
    for item in value:
        if not _elem_is_default(elem, item):
            return False
    return True


class TableDesc:
    """One generated table: its name, type id and fields."""

    __slots__ = ("name", "type_id", "variable", "fields", "by_id")

    name: str
    type_id: int
    variable: bool
    fields: tuple[FieldDesc, ...]
    by_id: dict[int, FieldDesc]

    def __init__(
        self,
        name: str,
        type_id: int,
        variable: bool,
        fields: tuple[FieldDesc, ...],
    ) -> None:
        self.name = name
        self.type_id = type_id
        self.variable = variable
        self.fields = fields
        self.by_id = {field.field_id: field for field in fields}

    def field(self, name: str) -> FieldDesc | None:
        for field in self.fields:
            if field.name == name:
                return field
        return None


class ArmDesc:
    """One arm of a table-side union."""

    __slots__ = ("name", "arm_id", "kind", "target", "pointer")

    name: str
    arm_id: int
    kind: int
    target: str
    pointer: bool

    def __init__(
        self,
        name: str,
        arm_id: int,
        kind: int,
        target: str = "",
        pointer: bool = False,
    ) -> None:
        self.name = name
        self.arm_id = arm_id
        self.kind = kind
        self.target = target
        self.pointer = pointer


class UnionDesc:
    """One generated table-side union: its arms, in declaration order."""

    __slots__ = ("name", "arms", "by_id")

    name: str
    arms: tuple[ArmDesc, ...]
    by_id: dict[int, ArmDesc]

    def __init__(self, name: str, arms: tuple[ArmDesc, ...]) -> None:
        self.name = name
        self.arms = arms
        self.by_id = {arm.arm_id: arm for arm in arms}


_REGISTRY_BY_ID: dict[int, type[Any]] = {}
_REGISTRY_BY_NAME: dict[str, type[Any]] = {}
_REGISTRY_UNION_BY_NAME: dict[str, type[Any]] = {}


def register(cls: type[Any]) -> None:
    """Register a generated table class by its wire type id and its name."""
    descriptor: TableDesc = cls.descriptor
    _REGISTRY_BY_ID[descriptor.type_id] = cls
    _REGISTRY_BY_NAME[descriptor.name] = cls


def by_id(type_id: int) -> type[Any] | None:
    return _REGISTRY_BY_ID.get(type_id)


def by_name(name: str) -> type[Any] | None:
    return _REGISTRY_BY_NAME.get(name)


def register_union(cls: type[Any]) -> None:
    """Register a generated table-side union class by its name."""
    descriptor: UnionDesc = cls.descriptor
    _REGISTRY_UNION_BY_NAME[descriptor.name] = cls


def new_by_name(name: str) -> Any:
    cls = _REGISTRY_BY_NAME.get(name)
    if cls is None:
        raise KeyError(f"the table {name} is not registered in this package")
    return cls()


def new_union_by_name(name: str) -> Any:
    cls = _REGISTRY_UNION_BY_NAME.get(name)
    if cls is None:
        raise KeyError(f"the union {name} is not registered in this package")
    return cls()
