"""The descriptor vocabulary the generated table classes carry.

A field's descriptor names what a generic walk needs: its schema name, its
table-wire id and kind, the codec that carries it, its declared range, and the
table (or union) whose value it holds. A table's descriptor holds its own type
id and its fields by declaration order and by wire id. The values are DATA,
built once at import, so the codec modules only ever read them.
"""

from __future__ import annotations

import struct
from typing import Any

# An array element is born at its kind's zero: an element carries no
# specified default (docs/SPEC.md §4.6; docs/SPEC-TABLES.md §2.9).
_ELEMENT_ZERO: dict[str, Any] = {
    "bool": False,
    "int": 0,
    "enum": 0,
    "float32": 0.0,
    "float64": 0.0,
    "string": "",
}


class FieldDesc:
    """One field of a generated table (docs/SPEC-TABLES.md §8.1).

    A container field names its element in elem, a descriptor of its own
    (its wire kind, codec, range, target and default); an enum-keyed array
    also carries keys, the key variants' wire ids in slot order, so slot i
    holds the variant whose value is i + 1 (§2.4, §3.2).

    A float field carries f32 (a float32 rather than a float64), and a
    COMPRESSED float32 (a declared min, max and resolution) carries
    float_range with its range in fmin and fmax.
    """

    __slots__ = (
        "name",
        "field_id",
        "kind",
        "codec",
        "inner",
        "ordinal",
        "lo",
        "hi",
        "target",
        "map_entry",
        "pointer",
        "optional",
        "array",
        "bound",
        "size",
        "key_enum",
        "array_kind",
        "array_min",
        "elem_kind",
        "variant_ids",
        "deprecated",
        "default",
        "elem_codec",
        "element",
        "elem",
        "keys",
        "f32",
        "fmin",
        "fmax",
        "float_range",
    )

    name: str
    field_id: int
    kind: int
    codec: str
    inner: str
    ordinal: int
    lo: int
    hi: int
    target: str
    map_entry: str
    pointer: bool
    optional: bool
    array: bool
    bound: int
    size: int
    key_enum: str
    array_kind: int
    array_min: int
    elem_kind: int
    variant_ids: tuple[int, ...]
    deprecated: frozenset[int]
    default: Any
    elem_codec: str
    element: FieldDesc | None
    elem: "FieldDesc | None"
    keys: tuple[int, ...]
    f32: bool
    fmin: float
    fmax: float
    float_range: bool

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
        size: int = 0,
        key_enum: str = "",
        array_kind: int = 0,
        array_min: int = 0,
        elem_kind: int = 0,
        variant_ids: tuple[int, ...] = (),
        deprecated: tuple[int, ...] = (),
        default: Any = None,
        inner: str = "",
        elem_codec: str = "",
        elem: "FieldDesc | None" = None,
        keys: tuple[int, ...] = (),
        f32: bool = False,
        fmin: float = 0.0,
        fmax: float = 0.0,
        float_range: bool = False,
    ) -> None:
        self.name = name
        self.field_id = field_id
        self.kind = kind
        self.codec = codec
        self.inner = inner
        self.ordinal = ordinal
        self.lo = lo
        self.hi = hi
        self.target = target
        self.map_entry = map_entry
        self.pointer = pointer
        self.optional = optional
        self.array = array
        self.bound = bound
        self.size = size
        self.key_enum = key_enum
        self.array_kind = array_kind
        self.array_min = array_min
        self.elem_kind = elem_kind
        self.variant_ids = variant_ids
        # the ordinals of the variants marked `| deprecated` (§2.15)
        self.deprecated = frozenset(deprecated)
        self.default = default
        self.elem_codec = elem_codec
        self.f32 = f32
        self.fmin = fmin
        self.fmax = fmax
        self.float_range = float_range
        self.element = (
            FieldDesc(
                name=name,
                field_id=0,
                kind=elem_kind,
                codec=elem_codec,
                lo=lo,
                hi=hi,
                target=target,
                pointer=pointer,
                size=size,
                variant_ids=variant_ids,
                deprecated=tuple(deprecated),
                default=_ELEMENT_ZERO.get(elem_codec),
                f32=f32,
                fmin=fmin,
                fmax=fmax,
                float_range=float_range,
            )
            if codec == "array" and elem_codec
            else None
        )
        self.elem = elem
        self.keys = keys

    def is_default(self, value: Any) -> bool:
        """Whether the writer elides this field (docs/SPEC-TABLES.md §3)."""
        if self.pointer or self.optional:
            return value is None
        if self.codec in ("bool", "int", "enum", "string", "unsupported"):
            return bool(value == self.default)
        if self.codec == "bytes":
            raw = _as_bytes(value)
            return raw is not None and bool(raw == self.default)
        if self.codec == "float32":
            return _float32_is_default(self, value)
        if self.codec == "float64":
            return _float64_is_default(self, value)
        if self.codec == "table":
            if value is None:
                return True
            if not self.target:
                return False
            # The engine elides a by-value table whose body is its terminator
            # alone (internal/tablewire/encode.go), which is every field of it
            # elided: judged field by field through the descriptors, never by
            # the generated __eq__, so a union inside it elides by its
            # selected arm and a user __eq__ cannot move a byte.
            fields: tuple[FieldDesc, ...] = value.descriptor.fields
            return all(field.is_default(getattr(value, field.name)) for field in fields)
        if self.codec == "union":
            # None is the absence of the field, and a union whose arm is None
            # (type 0) is the same value on the wire (docs/SPEC-TABLES.md §2.6)
            return value is None or value.type == 0
        if self.codec == "array":
            return _array_is_default(self, value)
        if self.codec == "union_array":
            # As the engine elides (internal/tablewire/encode.go): a counted
            # or unbounded array only when it holds nothing, a fixed array
            # when every element is at the declared default, judged per
            # element by its selected arm, never by == against a fresh value.
            if self.array_kind != 1:
                return len(value) == 0
            return all(element.type == 0 for element in value)
        if self.codec == "keyed":
            # A keyed array whose every slot elides writes no field
            # (kind_keyed.py): the same test, slot by slot, so a table that
            # holds one elides as its body does.
            elem = self.elem
            return elem is not None and all(elem.is_default(slot) for slot in value._values)
        return bool(value == self.default)


def _array_is_default(fd: FieldDesc, value: Any) -> bool:
    """Whether a positional array of a declared type elides (docs/SPEC-TABLES.md §3).

    A fixed array of a declared type writes every element, because position is
    identity there; a fixed array of any other element elides when every
    element is at its zero, parity with the reader's prefill; a counted or
    unbounded one elides only when it is empty, exactly as the engine's writer
    does.
    """
    if fd.array_kind == 1:  # [N]T
        element = fd.element
        if fd.elem_kind == 13 or element is None:
            return False
        return all(element.is_default(item) for item in value)
    return len(value) == 0


def _float32_is_default(fd: FieldDesc, value: Any) -> bool:
    """A float32 at its default: by its float32 bits, so -0.0 is not 0.0; a
    compressed float by its float32 value, because its -0 is +0 (§3.3)."""
    if type(value) is not float:
        return False
    if fd.float_range:
        return float(struct.unpack("<f", struct.pack("<f", value))[0]) == float(
            struct.unpack("<f", struct.pack("<f", fd.default))[0]
        )
    return struct.pack("<f", value) == struct.pack("<f", fd.default)


def _float64_is_default(fd: FieldDesc, value: Any) -> bool:
    """A float64 at its default, by its bits."""
    if type(value) is not float:
        return False
    if fd.float_range:
        return bool(value == fd.default)
    return struct.pack("<d", value) == struct.pack("<d", fd.default)


def _as_bytes(value: Any) -> bytes | None:
    """The bytes a caller's byte value holds, or None when it is not one."""
    if isinstance(value, bytes):
        return value
    if isinstance(value, bytearray):
        return bytes(value)
    if isinstance(value, memoryview):
        if value.format != "B":
            return None
        return value.tobytes()
    return None


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

    __slots__ = ("name", "tag", "arm_id", "kind", "target", "pointer", "field")

    name: str
    tag: int
    arm_id: int
    kind: int
    target: str
    pointer: bool
    field: FieldDesc | None

    def __init__(
        self,
        name: str,
        arm_id: int,
        kind: int,
        target: str = "",
        pointer: bool = False,
        tag: int = 0,
        field: FieldDesc | None = None,
    ) -> None:
        self.name = name
        self.tag = tag
        self.arm_id = arm_id
        self.kind = kind
        self.target = target
        self.pointer = pointer
        self.field = field


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


def _register_value(cls: type[Any]) -> None:
    """Register a hosted packet class by name only.

    A packet type by value never rides the node table's type ids, so it takes
    no id slot; it is reachable by name from the descriptor that carries it.
    """
    descriptor: TableDesc = cls.descriptor
    _REGISTRY_BY_NAME[descriptor.name] = cls


def _union_by_name(name: str) -> type[Any] | None:
    return _REGISTRY_UNION_BY_NAME.get(name)


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
