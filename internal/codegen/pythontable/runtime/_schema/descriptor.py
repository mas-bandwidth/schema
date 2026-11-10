"""The descriptor vocabulary the generated table classes carry.

A field's descriptor names what a generic walk needs: its schema name, its
table-wire id and kind, the codec that carries it, its declared range, and the
table (or union) whose value it holds. A table's descriptor holds its own type
id and its fields by declaration order and by wire id. The values are DATA,
built once at import, so the codec modules only ever read them.
"""

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
        "pointer",
        "optional",
        "array",
        "bound",
        "key_enum",
        "default",
    )

    name: str
    field_id: int
    kind: int
    codec: str
    ordinal: int
    lo: int
    hi: int
    target: str
    pointer: bool
    optional: bool
    array: bool
    bound: int
    key_enum: str
    default: Any

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
        pointer: bool = False,
        optional: bool = False,
        array: bool = False,
        bound: int = 0,
        key_enum: str = "",
        default: Any = None,
    ) -> None:
        self.name = name
        self.field_id = field_id
        self.kind = kind
        self.codec = codec
        self.ordinal = ordinal
        self.lo = lo
        self.hi = hi
        self.target = target
        self.pointer = pointer
        self.optional = optional
        self.array = array
        self.bound = bound
        self.key_enum = key_enum
        self.default = default

    def is_default(self, value: Any) -> bool:
        """Whether the writer elides this field (docs/SPEC-TABLES.md §3)."""
        if self.pointer or self.optional:
            return value is None
        if self.codec in ("bool", "int", "unsupported"):
            return bool(value == self.default)
        if self.codec == "table":
            if value is None:
                return True
            if not self.target:
                return False
            return bool(value == new_by_name(self.target))
        return bool(value == self.default)


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


def register(cls: type[Any]) -> None:
    """Register a generated table class by its wire type id and its name."""
    descriptor: TableDesc = cls.descriptor
    _REGISTRY_BY_ID[descriptor.type_id] = cls
    _REGISTRY_BY_NAME[descriptor.name] = cls


def by_id(type_id: int) -> type[Any] | None:
    return _REGISTRY_BY_ID.get(type_id)


def by_name(name: str) -> type[Any] | None:
    return _REGISTRY_BY_NAME.get(name)


def new_by_name(name: str) -> Any:
    cls = _REGISTRY_BY_NAME.get(name)
    if cls is None:
        raise KeyError(f"the table {name} is not registered in this package")
    return cls()
