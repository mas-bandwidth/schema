"""The descriptor vocabulary the generated table classes carry.

A field's descriptor names what a generic walk needs: its schema name, its
table-wire id and kind, the codec that carries it, its declared range, and the
table (or union) whose value it holds. A table's descriptor holds its own type
id and its fields by declaration order and by wire id. The values are DATA,
built once at import, so the codec modules only ever read them.
"""

from __future__ import annotations

import struct
from typing import Any, Callable

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
        "at",
        "pieces",
        "elem_size",
        "elem_align",
        "name",
        "json_key",
        "required",
        "frac_bits",
        "json_variants",
        "json_keys",
        "json_flags",
        "unbounded",
        "json_text_list",
        "doc",
        "tags",
        "type_name",
        "counted",
        "count_offset",
        "present_offset",
        "unbounded_container",
        "array_bound",
        "wide",
        "guard",
        "field_deprecated",
        "variant_names",
        "key_names",
        "vocabulary_kind",
        "enum_max",
        "variant_name",
        "variant_id",
        "key_name",
        "key_id",
        "get",
        "set",
        "get_element",
        "set_element",
        "_target_desc",
        "_union_desc",
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
        "text_total",
        "key_enum",
        "keyed_list",
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
        "array_mode",
        "min_count",
        "guards",
        "f32",
        "fmin",
        "fmax",
        "float_range",
    )

    at: int
    pieces: tuple[tuple[int, int], ...]
    elem_size: int
    elem_align: int
    name: str
    json_key: str | None
    required: bool
    frac_bits: int
    json_variants: tuple[str, ...]
    json_keys: tuple[str, ...]
    json_flags: bool
    unbounded: bool
    json_text_list: bool
    doc: str
    tags: tuple[str, ...]
    type_name: str
    counted: bool
    count_offset: int
    present_offset: int
    unbounded_container: bool
    array_bound: int
    wide: tuple[int, int] | None
    guard: str
    field_deprecated: bool
    variant_names: tuple[str, ...]
    key_names: tuple[str, ...]
    vocabulary_kind: str
    enum_max: int
    variant_name: Callable[[int], str | None] | None
    variant_id: Callable[[int], int] | None
    key_name: Callable[[int], str | None] | None
    key_id: Callable[[int], int] | None
    get: Callable[[Any], Any]
    set: Callable[[Any, Any], None]
    get_element: Callable[[Any, int], Any]
    set_element: Callable[[Any, int, Any], None]
    _target_desc: TableDesc | None
    _union_desc: UnionDesc | None
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
    text_total: int
    key_enum: str
    keyed_list: bool
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
    array_mode: str
    min_count: int
    guards: tuple[tuple[str, bool], ...]
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
        json_key: str | None = None,
        required: bool = False,
        frac_bits: int = 0,
        json_variants: tuple[str, ...] = (),
        json_keys: tuple[str, ...] = (),
        json_flags: bool = False,
        unbounded: bool = False,
        json_text_list: bool = False,
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
        text_total: int = 0,
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
        array_mode: str = "",
        min_count: int = 0,
        guards: tuple[tuple[str, bool], ...] = (),
        f32: bool = False,
        fmin: float = 0.0,
        fmax: float = 0.0,
        float_range: bool = False,
        keyed_list: bool = False,
        doc: str = "",
        tags: tuple[str, ...] = (),
        type_name: str = "",
        at: int = 0,
        pieces: tuple[tuple[int, int], ...] = (),
        elem_size: int = 0,
        elem_align: int = 1,
        counted: bool = False,
        count_offset: int = -1,
        present_offset: int = -1,
        unbounded_container: bool = False,
        array_bound: int = 0,
        wide: tuple[int, int] | None = None,
        guard: str = "",
        field_deprecated: bool = False,
        variant_names: tuple[str, ...] = (),
        key_names: tuple[str, ...] = (),
        vocabulary_kind: str = "",
        enum_max: int = 0,
    ) -> None:
        self.variant_names = variant_names
        self.key_names = key_names
        self.vocabulary_kind = vocabulary_kind
        self.enum_max = enum_max
        self.variant_name = self._variant_name if vocabulary_kind else None
        self.variant_id = self._variant_id if vocabulary_kind in ("enum", "union") else None
        self.key_name = self._key_name if key_enum else None
        self.key_id = self._key_id if key_enum else None
        self.get = self._get
        self.set = self._set
        self.get_element = self._get_element
        self.set_element = self._set_element
        self.wide = wide
        self.guard = guard
        self.required = required
        self.field_deprecated = field_deprecated
        self.counted = counted
        self.count_offset = count_offset
        self.present_offset = present_offset
        self.unbounded_container = unbounded_container
        self.unbounded = unbounded
        self.array_bound = array_bound
        self.frac_bits = frac_bits
        self._target_desc = None
        self._union_desc = None
        self.type_name = type_name
        self.at = at
        self.pieces = pieces
        self.elem_size = elem_size
        self.elem_align = elem_align
        self.name = name
        self.json_key = json_key
        self.frac_bits = frac_bits
        self.json_variants = json_variants
        self.json_keys = json_keys
        self.json_flags = json_flags
        self.unbounded = unbounded
        self.json_text_list = json_text_list
        self.doc = doc
        self.tags = tags
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
        self.text_total = text_total
        self.key_enum = key_enum
        self.keyed_list = keyed_list
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
                variant_names=variant_names,
                vocabulary_kind=vocabulary_kind,
                enum_max=enum_max,
                deprecated=tuple(deprecated),
                default=_ELEMENT_ZERO.get(elem_codec),
                f32=f32,
                fmin=fmin,
                fmax=fmax,
                float_range=float_range,
                frac_bits=frac_bits,
                json_variants=json_variants,
                json_flags=json_flags,
                unbounded=unbounded,
                wide=wide,
            )
            if array and elem_codec
            else None
        )
        self.elem = elem
        if self.element is None:
            self.element = elem
        self.keys = keys
        self.array_mode = array_mode
        self.min_count = min_count
        self.guards = guards

    def guard_holds(self, owner: Any) -> bool:
        """Whether every enclosing field guard holds on owner (docs/SPEC-TABLES.md §4)."""
        for cond_name, want in self.guards:
            if (getattr(owner, cond_name) is True) != want:
                return False
        return True

    def _variant_name(self, value: int) -> str | None:
        if self.vocabulary_kind == "flags":
            return self.variant_names[value] if 0 <= value < len(self.variant_names) else None
        if value == 0 and self.vocabulary_kind in ("enum", "union"):
            return "None"
        if 1 <= value <= len(self.variant_names):
            return self.variant_names[value - 1]
        return None

    def _variant_id(self, value: int) -> int:
        if self.vocabulary_kind == "flags" or value <= 0 or value > len(self.variant_ids):
            return 0
        return self.variant_ids[value - 1]

    def _key_name(self, key: int) -> str | None:
        if key == 0 and self.key_enum:
            return "None"
        if 1 <= key <= len(self.key_names):
            return self.key_names[key - 1] or None
        return None

    def _key_id(self, key: int) -> int:
        if 1 <= key <= len(self.keys):
            return self.keys[key - 1]
        return 0

    def _get(self, value: Any) -> Any:
        return getattr(value, self.name)

    def _set(self, value: Any, field_value: Any) -> None:
        setattr(value, self.name, field_value)

    def _get_element(self, value: Any, index: int) -> Any:
        return getattr(value, self.name)[index]

    def _set_element(self, value: Any, index: int, element_value: Any) -> None:
        field_value = getattr(value, self.name)
        if isinstance(field_value, str):
            characters = list(field_value)
            characters[index] = element_value
            setattr(value, self.name, "".join(characters))
        elif isinstance(field_value, bytes) or (
            isinstance(field_value, memoryview) and field_value.readonly
        ):
            octets = bytearray(field_value)
            octets[index] = element_value
            setattr(value, self.name, bytes(octets))
        else:
            field_value[index] = element_value

    @property
    def target_desc(self) -> TableDesc | None:
        if self._target_desc is not None:
            return self._target_desc
        cls = by_name(self.map_entry or self.target)
        if cls is None:
            return None
        descriptor: TableDesc = cls.descriptor
        return descriptor

    @target_desc.setter
    def target_desc(self, value: TableDesc | None) -> None:
        self._target_desc = value

    @property
    def union_desc(self) -> UnionDesc | None:
        if self._union_desc is not None:
            return self._union_desc
        cls = _union_by_name(self.target)
        if cls is None:
            return None
        descriptor: UnionDesc = cls.descriptor
        return descriptor

    @union_desc.setter
    def union_desc(self, value: UnionDesc | None) -> None:
        self._union_desc = value

    def is_default(self, value: Any) -> bool:
        """Whether the writer elides this field (docs/SPEC-TABLES.md §3)."""
        if self.codec == "pointer_array":
            if type(value) is not list:
                return False
            if len(value) == 0:
                return True
            if self.array_mode != "fixed":
                return False
            return all(elem is None for elem in value)
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
            fields: tuple[FieldDesc, ...] = tuple(
                field for field in value.descriptor.fields if field.guard_holds(value)
            )
            return all(not field.required and field.is_default(getattr(value, field.name)) for field in fields)
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
            slots = value if self.keyed_list else value._values
            return elem is not None and all(elem.is_default(slot) for slot in slots)
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

    __slots__ = ("name", "json_array", "forms", "type_id", "variable", "fields", "by_id", "doc", "tags", "table", "size", "align", "cook_align", "region_align", "extent_start", "padding", "build_version", "bytes_blob_type_id", "string_blob_type_id", "reset")

    name: str
    type_id: int
    variable: bool
    fields: tuple[FieldDesc, ...]
    by_id: dict[int, FieldDesc]
    table: bool
    size: int
    align: int
    region_align: int
    cook_align: int
    extent_start: int
    padding: tuple[tuple[int, int], ...]
    build_version: int
    bytes_blob_type_id: int
    string_blob_type_id: int
    reset: Callable[[Any], None]
    doc: str
    tags: tuple[str, ...]
    json_array: bool
    forms: tuple[str, ...]

    def __init__(
        self,
        name: str,
        type_id: int,
        variable: bool,
        fields: tuple[FieldDesc, ...],
        json_array: bool = False,
        forms: tuple[str, ...] = ("wire", "json", "block", "cook"),
        doc: str = "",
        tags: tuple[str, ...] = (),
        table: bool = True,
        size: int = 0,
        align: int = 0,
        region_align: int = 0,
        cook_align: int = 1,
        extent_start: int = 0,
        padding: tuple[tuple[int, int], ...] = (),
        build_version: int = 0,
        bytes_blob_type_id: int = 0,
        string_blob_type_id: int = 0,
    ) -> None:
        self.reset = self._reset
        self.cook_align = cook_align
        self.extent_start = extent_start
        self.padding = padding
        self.build_version = build_version
        self.bytes_blob_type_id = bytes_blob_type_id
        self.string_blob_type_id = string_blob_type_id
        self.forms = forms
        self.table = table
        self.size = size
        self.align = align
        self.region_align = region_align
        self.name = name
        self.doc = doc
        self.tags = tags
        self.type_id = type_id
        self.variable = variable
        self.fields = fields
        self.json_array = json_array
        self.by_id = {field.field_id: field for field in fields}
        self.size = size
        self.align = align
        self.cook_align = cook_align
        self.build_version = build_version
        self.region_align = region_align
        self.bytes_blob_type_id = bytes_blob_type_id
        self.string_blob_type_id = string_blob_type_id
        self.padding = padding
        self.extent_start = extent_start

    def _reset(self, value: Any) -> None:
        """Restore one generated value's declared defaults in place."""
        value.__init__()

    def field(self, name: str) -> FieldDesc | None:
        for field in self.fields:
            if field.name == name:
                return field
        return None


class ArmDesc:
    """One arm of a table-side union."""

    __slots__ = ("name", "tag", "arm_id", "kind", "target", "pointer", "field", "size", "doc", "tags", "_target_desc")

    name: str
    tag: int
    arm_id: int
    kind: int
    target: str
    pointer: bool
    field: FieldDesc | None
    size: int
    doc: str
    tags: tuple[str, ...]
    _target_desc: TableDesc | None

    def __init__(
        self,
        name: str,
        arm_id: int,
        kind: int,
        target: str = "",
        pointer: bool = False,
        tag: int = 0,
        field: FieldDesc | None = None,
        size: int = 0,
        doc: str = "",
        tags: tuple[str, ...] = (),
    ) -> None:
        self.size = size
        self.doc = doc
        self.tags = tags
        self._target_desc = None
        self.name = name
        self.tag = tag
        self.arm_id = arm_id
        self.kind = kind
        self.target = target
        self.pointer = pointer
        self.field = field
        self.size = size

    @property
    def target_desc(self) -> TableDesc | None:
        if self._target_desc is not None:
            return self._target_desc
        cls = by_name(self.target)
        if cls is None:
            return None
        descriptor: TableDesc = cls.descriptor
        return descriptor

    @target_desc.setter
    def target_desc(self, value: TableDesc | None) -> None:
        self._target_desc = value


class UnionDesc:
    """One generated table-side union: its arms, in declaration order."""

    __slots__ = ("name", "arms", "by_id", "size", "tag_size", "arm_offset", "doc", "tags")

    name: str
    arms: tuple[ArmDesc, ...]
    by_id: dict[int, ArmDesc]
    size: int
    tag_size: int
    arm_offset: int
    doc: str
    tags: tuple[str, ...]

    def __init__(self, name: str, arms: tuple[ArmDesc, ...], size: int = 0,
                 tag_size: int = 0, arm_offset: int = 0, doc: str = "",
                 tags: tuple[str, ...] = ()) -> None:
        self.size = size
        self.tag_size = tag_size
        self.arm_offset = arm_offset
        self.doc = doc
        self.tags = tags
        self.name = name
        self.arms = arms
        self.by_id = {arm.arm_id: arm for arm in arms}
        self.size = size
        self.tag_size = tag_size
        self.arm_offset = arm_offset


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


class BlockField:
    """One field of a block record (docs/SPEC-TABLES.md §19.2, §19.3).

    The columns are the GENERIC ROW WALK's: where the field starts, how many
    slots it holds, how wide one is, and where its count and presence
    companions live. An out-of-line array's field is its sixteen-byte triple.
    """

    __slots__ = (
        "name",
        "kind",
        "offset",
        "size",
        "out_of_line",
        "counted",
        "optional",
        "array",
        "bound",
        "elem_size",
        "count_offset",
        "present_offset",
        "target",
        "offset_of_offset",
        "stride_offset",
        "stride",
    )

    name: str
    kind: int
    offset: int
    size: int
    out_of_line: bool
    counted: bool
    optional: bool
    array: bool
    bound: int
    elem_size: int
    count_offset: int
    present_offset: int
    target: str
    offset_of_offset: int
    stride_offset: int
    stride: int

    def __init__(
        self,
        name: str,
        kind: int,
        offset: int,
        size: int,
        out_of_line: bool = False,
        counted: bool = False,
        optional: bool = False,
        array: bool = False,
        bound: int = 0,
        elem_size: int = 0,
        count_offset: int = -1,
        present_offset: int = -1,
        target: str = "",
        offset_of_offset: int = -1,
        stride_offset: int = -1,
        stride: int = 0,
    ) -> None:
        self.name = name
        self.kind = kind
        self.offset = offset
        self.size = size
        self.out_of_line = out_of_line
        self.counted = counted
        self.optional = optional
        self.array = array
        self.bound = bound
        self.elem_size = elem_size
        self.count_offset = count_offset
        self.present_offset = present_offset
        self.target = target
        self.offset_of_offset = offset_of_offset
        self.stride_offset = stride_offset
        self.stride = stride


class BlockRecord:
    """One record the block form touches, as data (docs/SPEC-TABLES.md §19.3)."""

    __slots__ = ("name", "size", "align", "fields")

    name: str
    size: int
    align: int
    fields: tuple[BlockField, ...]

    def __init__(self, name: str, size: int, align: int, fields: tuple[BlockField, ...]) -> None:
        self.name = name
        self.size = size
        self.align = align
        self.fields = fields


class BlockArray:
    """One out-of-line array of a block-form table (docs/SPEC-TABLES.md §19.1)."""

    __slots__ = ("name", "offset", "maximum", "stride", "elem_align", "element")

    name: str
    offset: int
    maximum: int
    stride: int
    elem_align: int
    element: str

    def __init__(self, name: str, offset: int, maximum: int, stride: int, elem_align: int, element: str) -> None:
        self.name = name
        self.offset = offset
        self.maximum = maximum
        self.stride = stride
        self.elem_align = elem_align
        self.element = element


class BlockTable:
    """A table's block form: its projection, arrays and maximum-sized extent."""

    __slots__ = ("name", "projection", "arrays", "max_bytes")

    name: str
    projection: BlockRecord
    arrays: tuple[BlockArray, ...]
    max_bytes: int

    def __init__(
        self,
        name: str,
        projection: BlockRecord,
        arrays: tuple[BlockArray, ...],
        max_bytes: int,
    ) -> None:
        self.name = name
        self.projection = projection
        self.arrays = arrays
        self.max_bytes = max_bytes
