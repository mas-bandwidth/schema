"""The fixed form, form byte 3 (docs/SPEC-TABLES.md §3.4), for every fixed table.

docs/FIXED-FORM-ALGORITHM.md is the algorithm, as the strict-only contract
states it: one read, which returns every record or refuses with no value; the
one counter is ``widened``; every other condition is a refusal by name.

A generated module lays each fixed root down as data: its own wire hash and
record body size, its lineage (the lock's layouts, oldest first, its own last,
and the floor) and its SHAPE, the reader's own fields as nodes of this module.
The layout bytes and the hashes are the compiler's (ir.TableFixedLayoutBytes,
ir.TableFixedLayoutHash); this module never derives a hash.

A PLAN is the pairing of one lineage entry's layout (the writer, bytes from
the lock) with the reader's shape: one reader per node, matched by wire id,
built once per entry on first use and kept. The build's own entry is the
IDENTITY lane: the generated module's straight-line reader (_s_fl_<Root>),
which measured faster than the plan (docs/PORT-PYTHON.md, D1), holding the
same checks by the same names. An entry that is not a lawful widening of the reader keeps the
reason instead of a plan, and a file under its hash refuses
``layout_malformed``. A file's own layout is compared to the lock's bytes and
never walked.

A read checks and lands every record, then returns them all; a refusal at any
record returns nothing, so the caller holds nothing the read produced. A write
validates the whole batch against the reader's own contract and returns the
bytes, or raises InvalidValue with no bytes.
"""

import functools
import math
import struct
from typing import Any, Callable, Final, Sequence

from . import InvalidValue, Refused

__all__ = [
    "Arm",
    "Array",
    "Bool",
    "Bytes",
    "Entry",
    "Enum",
    "Field",
    "Float",
    "Int",
    "Keyed",
    "KeyedArray",
    "Report",
    "Root",
    "Table",
    "Text",
    "Union",
    "shape",
]

FORM: Final = 3
HEADER_BYTES: Final = 20
ENTRY_BYTES: Final = 17
HASH_BYTES: Final = 8
MAX_DEPTH: Final = 64

# the layout kind codes (docs/FIXED-FORM-ALGORITHM.md §1)
K_BOOL: Final = 1
K_TABLE: Final = 13
K_ARRAY: Final = 14
K_UNION: Final = 15
K_KEYED: Final = 16
K_U8: Final = 6
K_STRING: Final = 12
K_ENUM: Final = 30
K_VARIANT: Final = 32
K_WSTRING: Final = 33
K_OPTIONAL: Final = 35

_SIGNED_KINDS: Final = frozenset({2, 3, 4, 5, 18, 20, 21, 22, 23, 24})
# the widening ladder runs inside a family and upward only (§1)
_FAMILIES: Final = ((2, 3, 4, 5), (6, 7, 8, 9), (20, 21, 22, 23, 24), (25, 26, 27, 28, 29), (10, 11))

_U32: Final = struct.Struct("<I")
_I32: Final = struct.Struct("<i")
_U64: Final = struct.Struct("<Q")
_F32: Final = struct.Struct("<f")
_F64: Final = struct.Struct("<d")

shape = functools.cache
"""Caches a generated shape function: each node is built once, on first use,
after every module of the unit has loaded."""


def _ladder(writer: int, reader: int) -> bool:
    for family in _FAMILIES:
        if writer in family and reader in family:
            return family.index(writer) < family.index(reader)
    return False


# ---------------------------------------------------------------------------
# the reader's shape: one node per layout entry of its own walk


class Node:
    """One node of a fixed table's shape: its layout kind and constant size."""

    __slots__ = ("kind", "size")

    def __init__(self, kind: int, size: int) -> None:
        self.kind = kind
        self.size = size


class Bool(Node):
    """bool: one byte, 0 or 1."""

    __slots__ = ()

    def __init__(self) -> None:
        super().__init__(K_BOOL, 1)


class Int(Node):
    """An integer, a bits(N), a flags mask or a fixed(I, F) raw value.

    lo and hi are the declared raw bounds (a ranged integer, a fixed-point
    range shifted by F, a bits(N)'s 2^N - 1), or None.
    """

    __slots__ = ("lo", "hi")

    def __init__(self, kind: int, size: int, lo: int | None = None, hi: int | None = None) -> None:
        super().__init__(kind, size)
        self.lo = lo
        self.hi = hi


class Float(Node):
    """float32 (kind 10) or float64 (kind 11); lo and hi a declared range, or None."""

    __slots__ = ("lo", "hi")

    def __init__(self, kind: int, lo: float | None = None, hi: float | None = None) -> None:
        super().__init__(kind, 4 if kind == 10 else 8)
        if kind == 10 and lo is not None and hi is not None:
            lo, hi = _round32(lo), _round32(hi)
        self.lo = lo
        self.hi = hi


class Enum(Node):
    """An enum: the ordinal at its storage width, and the variants' wire ids in declared order."""

    __slots__ = ("ids",)

    def __init__(self, size: int, ids: tuple[int, ...]) -> None:
        super().__init__(K_ENUM, size)
        self.ids = ids


class Text(Node):
    """string(N) (kind 12, a u32 length and N bytes) or wstring(N) (kind 33, N UTF-16 units)."""

    __slots__ = ()


class Bytes(Node):
    """bytes(N): the layout's array of u8, a u32 length and N bytes."""

    __slots__ = ()

    def __init__(self, size: int) -> None:
        super().__init__(K_ARRAY, size)


class Array(Node):
    """[N]T, or [..N]T when counted (a u32 count, then N elements).

    fill builds one element at its defaults, for the slots a shorter writer
    did not send.
    """

    __slots__ = ("counted", "n", "elem", "fill")

    def __init__(self, size: int, counted: bool, n: int, elem: Node, fill: Callable[[], Any]) -> None:
        super().__init__(K_ARRAY, size)
        self.counted = counted
        self.n = n
        self.elem = elem
        self.fill = fill


class Keyed(Node):
    """[E]T: one slot per variant of E, keyed by the variants' wire ids.

    wrap is True for a table's field, whose value is a KeyedArray, and False
    for a type's, whose value is a list.
    """

    __slots__ = ("key_id", "keys", "elem", "fill", "wrap")

    def __init__(self, size: int, key_id: int, keys: tuple[int, ...], elem: Node, fill: Callable[[], Any], wrap: bool) -> None:
        super().__init__(K_KEYED, size)
        self.key_id = key_id
        self.keys = keys
        self.elem = elem
        self.fill = fill
        self.wrap = wrap


class Arm:
    """One union arm: its wire id, its attribute and payload node (None when payload-free)."""

    __slots__ = ("id", "attr", "node")

    def __init__(self, id: int, attr: str | None, node: Node | None) -> None:
        self.id = id
        self.attr = attr
        self.node = node


class Union(Node):
    """A union: the tag at its width, then the widest arm."""

    __slots__ = ("tag", "cls", "arms")

    def __init__(self, size: int, tag: int, cls: Callable[[], Any], arms: tuple[Arm, ...]) -> None:
        super().__init__(K_UNION, size)
        self.tag = tag
        self.cls = cls
        self.arms = arms


class Field:
    """One field of a table or type: attribute, wire id, payload node, optional."""

    __slots__ = ("attr", "id", "node", "optional")

    def __init__(self, attr: str, id: int, node: Node, optional: bool = False) -> None:
        self.attr = attr
        self.id = id
        self.node = node
        self.optional = optional


class Table(Node):
    """A table or type by value: its fields in declared order, inline."""

    __slots__ = ("cls", "fields")

    def __init__(self, cls: Callable[[], Any], size: int, fields: tuple[Field, ...]) -> None:
        super().__init__(K_TABLE, size)
        self.cls = cls
        self.fields = fields


# ---------------------------------------------------------------------------
# values


class KeyedArray:
    """An enum-keyed array [E]T: one slot per variant, the element for variant k at index k - 1.

    Indexing refuses None (0) and any key past E.MAX with KeyError
    (docs/SPEC-TABLES.md §2.4); slots() is the list in key order.
    """

    __slots__ = ("_slots",)

    def __init__(self, slots: list[Any]) -> None:
        self._slots = slots

    def __getitem__(self, key: int) -> Any:
        return self._slots[self._index(key)]

    def __setitem__(self, key: int, value: Any) -> None:
        self._slots[self._index(key)] = value

    def __len__(self) -> int:
        return len(self._slots)

    def _index(self, key: int) -> int:
        if type(key) is not int or not 1 <= key <= len(self._slots):
            raise KeyError(key)
        return key - 1

    def slots(self) -> list[Any]:
        """The slots in key order: variant k at index k - 1."""
        return self._slots

    def __eq__(self, other: object) -> bool:
        from . import equal

        return isinstance(other, KeyedArray) and equal(self._slots, other._slots)

    __hash__ = None  # type: ignore[assignment]

    def __repr__(self) -> str:
        return f"KeyedArray({self._slots!r})"


class Report:
    """What a successful load counts: widened, once per widened value per record."""

    __slots__ = ("widened",)

    def __init__(self) -> None:
        self.widened = 0


# ---------------------------------------------------------------------------
# the writer's layout, from the lock's bytes


class _W:
    """One entry of a lineage layout: id, kind, size, and its children."""

    __slots__ = ("id", "kind", "size", "children")

    def __init__(self, id: int, kind: int, size: int) -> None:
        self.id = id
        self.kind = kind
        self.size = size
        self.children: list[_W] = []


def _parse(layout: bytes) -> "_W":
    """The layout's tree, walked pre-order. A layout the lock holds is checked
    when the lock is written; this answers ValueError on one that does not
    frame, which makes its entry's lane ``layout_malformed``."""
    if len(layout) < 4:
        raise ValueError("a layout shorter than its count")
    (count,) = _U32.unpack_from(layout, 0)
    if count == 0 or 4 + ENTRY_BYTES * count != len(layout):
        raise ValueError("a layout whose count does not fit its length")
    entries = []
    for i in range(count):
        at = 4 + ENTRY_BYTES * i
        (id,) = _U64.unpack_from(layout, at)
        kind = layout[at + 8]
        (size,) = _U32.unpack_from(layout, at + 9)
        (children,) = _U32.unpack_from(layout, at + 13)
        entries.append((id, kind, size, children))
    pos = 0

    def walk(depth: int) -> _W:
        nonlocal pos
        if pos >= count:
            raise ValueError("the walk ran past the last entry")
        if depth > MAX_DEPTH:
            raise ValueError("the layout nests past 64")
        id, kind, size, children = entries[pos]
        pos += 1
        node = _W(id, kind, size)
        for _ in range(children):
            node.children.append(walk(depth + 1))
        return node

    root = walk(0)
    if pos != count or root.kind != K_TABLE:
        raise ValueError("the walk does not close on the root table")
    return root


class _Unlawful(Exception):
    """The writer's layout is not a lawful widening of the reader's."""


# ---------------------------------------------------------------------------
# the plan: one reader per node pair


Reader = Callable[[bytes, int, Report], Any]


def _refuse(reason: str, detail: str = "") -> Refused:
    return Refused(reason, detail)


def _round32(v: float) -> float:
    (r,) = _F32.unpack(_F32.pack(v))
    return float(r)


def _int_reader(w: _W, r: Int, path: str, ranges: dict[int, tuple[Any, Any]] | None, fid: int) -> Reader:
    if w.kind == r.kind and w.size == r.size:
        widened = False
        signed = r.kind in _SIGNED_KINDS
    elif w.kind == r.kind and w.size < r.size <= 8:
        widened = True  # a same-kind widen zero-extends (§5.2)
        signed = False
    elif _ladder(w.kind, r.kind):
        widened = True
        signed = w.kind in _SIGNED_KINDS
    else:
        raise _Unlawful(f"{path}: kind {w.kind} into kind {r.kind}")
    lo, hi = r.lo, r.hi
    if ranges is not None and fid in ranges:
        lo, hi = ranges[fid]
    size = w.size
    if size == 16:
        def read128(buf: bytes, off: int, rep: Report) -> Any:
            lo64, hi64 = struct.unpack_from("<QQ", buf, off)
            v = lo64 | (hi64 << 64)
            if signed and v >= 1 << 127:
                v -= 1 << 128
            if (lo is not None and v < lo) or (hi is not None and v > hi):
                raise _refuse("out_of_range", path)
            return v
        return read128
    fmt = struct.Struct("<" + {1: "b", 2: "h", 4: "i", 8: "q"}[size] if signed else "<" + {1: "B", 2: "H", 4: "I", 8: "Q"}[size])
    unpack = fmt.unpack_from
    if lo is None and hi is None:
        if widened:
            def read_widen(buf: bytes, off: int, rep: Report) -> Any:
                rep.widened += 1
                return unpack(buf, off)[0]
            return read_widen

        def read_plain(buf: bytes, off: int, rep: Report) -> Any:
            return unpack(buf, off)[0]
        return read_plain

    def read_ranged(buf: bytes, off: int, rep: Report) -> Any:
        v = unpack(buf, off)[0]
        if (lo is not None and v < lo) or (hi is not None and v > hi):
            raise _refuse("out_of_range", path)
        if widened:
            rep.widened += 1
        return v
    return read_ranged


def _float_reader(w: _W, r: Float, path: str, ranges: dict[int, tuple[Any, Any]] | None, fid: int) -> Reader:
    if w.kind == r.kind:
        widened = False
    elif w.kind == 10 and r.kind == 11:
        widened = True
    else:
        raise _Unlawful(f"{path}: kind {w.kind} into kind {r.kind}")
    lo, hi = r.lo, r.hi
    if ranges is not None and fid in ranges:
        lo, hi = ranges[fid]
        if w.kind == 10 and lo is not None and hi is not None:
            lo, hi = _round32(lo), _round32(hi)
    if w.kind == 10:
        def read32(buf: bytes, off: int, rep: Report) -> Any:
            (bits,) = _U32.unpack_from(buf, off)
            if bits & 0x7F800000 == 0x7F800000:
                raise _refuse("non_finite_float", path)
            v: float = _F32.unpack_from(buf, off)[0]
            if (lo is not None and v < lo) or (hi is not None and v > hi):
                raise _refuse("out_of_range", path)
            if widened:
                rep.widened += 1
            return v
        return read32

    def read64(buf: bytes, off: int, rep: Report) -> Any:
        (bits,) = _U64.unpack_from(buf, off)
        if bits & 0x7FF0000000000000 == 0x7FF0000000000000:
            raise _refuse("non_finite_float", path)
        v: float = _F64.unpack_from(buf, off)[0]
        if (lo is not None and v < lo) or (hi is not None and v > hi):
            raise _refuse("out_of_range", path)
        return v
    return read64


def _enum_reader(w: _W, r: Enum, path: str) -> Reader:
    if w.kind != K_ENUM or w.size > r.size or w.size not in (1, 2, 4, 8):
        raise _Unlawful(f"{path}: an enum that moved")
    position = {id: i + 1 for i, id in enumerate(r.ids)}
    remap = [0]
    for c in w.children:
        if c.id not in position:
            raise _Unlawful(f"{path}: a variant this reader does not name")
        remap.append(position[c.id])
    n = len(remap) - 1
    widened = w.size < r.size
    unpack = struct.Struct("<" + {1: "B", 2: "H", 4: "I", 8: "Q"}[w.size]).unpack_from

    def read(buf: bytes, off: int, rep: Report) -> Any:
        raw = unpack(buf, off)[0]
        if raw > n:
            raise _refuse("out_of_range", path)
        if widened:
            rep.widened += 1
        return remap[raw]
    return read


def _text_reader(w: _W, r: Text, path: str) -> Reader:
    if w.kind != r.kind or w.size < 4 or w.size > r.size:
        raise _Unlawful(f"{path}: text that moved or narrowed")
    if r.kind == K_STRING:
        cap = w.size - 4

        def read_utf8(buf: bytes, off: int, rep: Report) -> Any:
            (n,) = _I32.unpack_from(buf, off)
            if n < 0 or n > cap:
                raise _refuse("length_over_bound", path)
            raw = buf[off + 4 : off + 4 + n]
            if b"\x00" in raw:
                raise _refuse("text_ill_formed", path)
            try:
                return raw.decode("utf-8")
            except UnicodeDecodeError:
                raise _refuse("text_ill_formed", path) from None
        return read_utf8
    if (w.size - 4) % 2:
        raise _Unlawful(f"{path}: a wide span that is not whole units")
    wcap = (w.size - 4) // 2

    def read_wide(buf: bytes, off: int, rep: Report) -> Any:
        (n,) = _I32.unpack_from(buf, off)
        if n < 0 or n > wcap:
            raise _refuse("length_over_bound", path)
        raw = buf[off + 4 : off + 4 + 2 * n]
        try:
            text = raw.decode("utf-16-le")
        except UnicodeDecodeError:
            raise _refuse("text_ill_formed", path) from None
        if "\x00" in text:
            raise _refuse("text_ill_formed", path)
        return text
    return read_wide


def _bytes_reader(w: _W, r: Bytes, path: str) -> Reader:
    if w.kind != K_ARRAY or len(w.children) != 1 or w.children[0].kind != K_U8 or w.size < 4 or w.size > r.size:
        raise _Unlawful(f"{path}: bytes that moved or narrowed")
    cap = w.size - 4

    def read(buf: bytes, off: int, rep: Report) -> Any:
        (n,) = _I32.unpack_from(buf, off)
        if n < 0 or n > cap:
            raise _refuse("count_over_bound", path)
        return bytes(buf[off + 4 : off + 4 + n])
    return read


def _array_reader(w: _W, r: Array, path: str) -> Reader:
    if w.kind != K_ARRAY or len(w.children) != 1:
        raise _Unlawful(f"{path}: an array that moved")
    head = 4 if r.counted else 0
    welem = w.children[0]
    if welem.size == 0 or (w.size - head) % welem.size:
        raise _Unlawful(f"{path}: an array whose size is not whole elements")
    their_n = (w.size - head) // welem.size
    if their_n > r.n:
        raise _Unlawful(f"{path}: a bound that narrowed")
    elem = _compile(welem, r.elem, path + "[]", None, 0)
    stride = welem.size
    fill = r.fill
    if r.counted:
        def read_counted(buf: bytes, off: int, rep: Report) -> Any:
            (n,) = _I32.unpack_from(buf, off)
            if n < 0 or n > their_n:
                raise _refuse("count_over_bound", path)
            at = off + 4
            return [elem(buf, at + i * stride, rep) for i in range(n)]
        return read_counted
    more = r.n - their_n

    def read_fixed(buf: bytes, off: int, rep: Report) -> Any:
        out = [elem(buf, off + i * stride, rep) for i in range(their_n)]
        for _ in range(more):
            out.append(fill())
        return out
    return read_fixed


def _keyed_reader(w: _W, r: Keyed, path: str) -> Reader:
    if w.kind != K_KEYED or len(w.children) != 2 or w.children[0].kind != K_ENUM:
        raise _Unlawful(f"{path}: a keyed array that moved")
    wkey, welem = w.children
    if wkey.id != r.key_id:
        raise _Unlawful(f"{path}: a key enum that changed")
    wkeys = [c.id for c in wkey.children]
    for k in wkeys:
        if k not in r.keys:
            raise _Unlawful(f"{path}: a key variant this reader does not name")
    stride = welem.size
    elem = _compile(welem, r.elem, path + "[]", None, 0)
    slots = [wkeys.index(k) if k in wkeys else -1 for k in r.keys]
    fill = r.fill
    wrap = r.wrap

    def read(buf: bytes, off: int, rep: Report) -> Any:
        out = [elem(buf, off + j * stride, rep) if j >= 0 else fill() for j in slots]
        return KeyedArray(out) if wrap else out
    return read


def _union_reader(w: _W, r: Union, path: str) -> Reader:
    if w.kind != K_UNION or not w.children:
        raise _Unlawful(f"{path}: a union that moved")
    widest = max(c.size for c in w.children)
    tag = w.size - widest
    if tag not in (1, 2, 4, 8):
        raise _Unlawful(f"{path}: a union tag of {tag} bytes")
    mine = {a.id: (i + 1, a) for i, a in enumerate(r.arms)}
    arms: list[tuple[int, str | None, Reader | None]] = [(0, None, None)]
    for c in w.children:
        if c.id not in mine:
            raise _Unlawful(f"{path}: an arm this reader does not name")
        ordinal, arm = mine[c.id]
        if (c.kind == K_VARIANT) != (arm.node is None):
            raise _Unlawful(f"{path}: an arm whose payload moved")
        reader = None if arm.node is None else _compile(c, arm.node, f"{path}.{arm.attr}", None, 0)
        arms.append((ordinal, arm.attr, reader))
    n = len(arms) - 1
    unpack = struct.Struct("<" + {1: "B", 2: "H", 4: "I", 8: "Q"}[tag]).unpack_from
    cls = r.cls

    def read(buf: bytes, off: int, rep: Report) -> Any:
        t = unpack(buf, off)[0]
        if t > n:
            raise _refuse("out_of_range", path)
        v = cls()
        ordinal, attr, reader = arms[t]
        v.type = ordinal
        if reader is not None and attr is not None:
            setattr(v, attr, reader(buf, off + tag, rep))
        return v
    return read


def _optional_reader(w: _W, r: Node, path: str, ranges: dict[int, tuple[Any, Any]] | None, fid: int) -> Reader:
    if w.kind == K_OPTIONAL:
        if len(w.children) != 1:
            raise _Unlawful(f"{path}: an optional without one payload")
        payload = _compile(w.children[0], r, path, ranges, fid)

        def read(buf: bytes, off: int, rep: Report) -> Any:
            present = buf[off]
            if present == 0:
                return None
            if present != 1:
                raise _refuse("wire_damaged", path)
            return payload(buf, off + 1, rep)
        return read
    # T into ?T (bill §12.8): the writer's value is always present
    return _compile(w, r, path, ranges, fid)


def _table_reader(w: _W, r: Table, path: str, ranges: dict[int, tuple[Any, Any]] | None) -> Reader:
    if w.kind != K_TABLE:
        raise _Unlawful(f"{path}: kind {w.kind} into a table")
    mine = {f.id: f for f in r.fields}
    steps: list[tuple[str, int, Reader]] = []
    at = 0
    for c in w.children:
        f = mine.get(c.id)
        if f is None:
            raise _Unlawful(f"{path}: a writer field this reader does not name (0x{c.id:016x})")
        fpath = f"{path}.{f.attr}"
        if f.optional:
            reader = _optional_reader(c, f.node, fpath, ranges, f.id)
        elif c.kind == K_OPTIONAL:
            raise _Unlawful(f"{fpath}: an optional the reader does not declare optional")
        else:
            reader = _compile(c, f.node, fpath, ranges, f.id)
        steps.append((f.attr, at, reader))
        at += c.size
    if at != w.size:
        raise _Unlawful(f"{path}: fields that do not sum to the table's size")
    cls = r.cls

    def read(buf: bytes, off: int, rep: Report) -> Any:
        v = cls()
        for attr, delta, reader in steps:
            setattr(v, attr, reader(buf, off + delta, rep))
        return v
    return read


def _compile(w: _W, r: Node, path: str, ranges: dict[int, tuple[Any, Any]] | None, fid: int) -> Reader:
    if isinstance(r, Table):
        return _table_reader(w, r, path, None)
    if isinstance(r, Bool):
        if w.kind != K_BOOL or w.size != 1:
            raise _Unlawful(f"{path}: kind {w.kind} into bool")

        def read_bool(buf: bytes, off: int, rep: Report) -> Any:
            b = buf[off]
            if b > 1:
                raise _refuse("wire_damaged", path)
            return b == 1
        return read_bool
    if isinstance(r, Int):
        return _int_reader(w, r, path, ranges, fid)
    if isinstance(r, Float):
        return _float_reader(w, r, path, ranges, fid)
    if isinstance(r, Enum):
        return _enum_reader(w, r, path)
    if isinstance(r, Text):
        return _text_reader(w, r, path)
    if isinstance(r, Bytes):
        return _bytes_reader(w, r, path)
    if isinstance(r, Array):
        return _array_reader(w, r, path)
    if isinstance(r, Keyed):
        return _keyed_reader(w, r, path)
    if isinstance(r, Union):
        return _union_reader(w, r, path)
    raise _Unlawful(f"{path}: a node this runtime does not know")


# ---------------------------------------------------------------------------
# the writer: the reader's own shape, validated, onto a zeroed template


def _invalid(path: str, rule: str) -> InvalidValue:
    return InvalidValue(path, rule)


def _put(node: Node, v: Any, out: bytearray, off: int, path: str) -> None:
    if isinstance(node, Table):
        if not isinstance(v, node.cls):  # type: ignore[arg-type]
            raise _invalid(path, f"{v!r} is not a {getattr(node.cls, '__name__', 'value')}")
        at = off
        for f in node.fields:
            fv = getattr(v, f.attr)
            fpath = f"{path}.{f.attr}"
            if f.optional:
                if fv is None:
                    out[at] = 0
                else:
                    out[at] = 1
                    _put(f.node, fv, out, at + 1, fpath)
                at += 1 + f.node.size
            else:
                _put(f.node, fv, out, at, fpath)
                at += f.node.size
        return
    if isinstance(node, Bool):
        if type(v) is not bool:
            raise _invalid(path, f"{v!r} is not a bool")
        out[off] = 1 if v else 0
        return
    if isinstance(node, Int):
        if type(v) is not int:
            raise _invalid(path, f"{v!r} is not an int")
        bits = node.size * 8
        signed = node.kind in _SIGNED_KINDS
        lo = -(1 << (bits - 1)) if signed else 0
        hi = (1 << (bits - 1)) - 1 if signed else (1 << bits) - 1
        if node.lo is not None:
            lo = max(lo, node.lo)
        if node.hi is not None:
            hi = min(hi, node.hi)
        if not lo <= v <= hi:
            raise _invalid(path, f"{v} is outside [{lo}, {hi}]")
        out[off : off + node.size] = (v & ((1 << bits) - 1)).to_bytes(node.size, "little")
        return
    if isinstance(node, Float):
        if type(v) is not float:
            raise _invalid(path, f"{v!r} is not a float")
        if not math.isfinite(v):
            raise _invalid(path, "non_finite_float")
        if node.kind == 10:
            try:
                packed = _F32.pack(v)
            except OverflowError:
                raise _invalid(path, f"{v!r} overflows a float32") from None
            stored = _F32.unpack(packed)[0]
        else:
            packed = _F64.pack(v)
            stored = v
        if (node.lo is not None and stored < node.lo) or (node.hi is not None and stored > node.hi):
            raise _invalid(path, f"{v!r} is outside [{node.lo}, {node.hi}]")
        out[off : off + node.size] = packed
        return
    if isinstance(node, Enum):
        if type(v) is not int or not 0 <= v <= len(node.ids):
            raise _invalid(path, f"{v!r} is no variant of the enum (0 to {len(node.ids)})")
        out[off : off + node.size] = v.to_bytes(node.size, "little")
        return
    if isinstance(node, Text):
        if type(v) is not str:
            raise _invalid(path, f"{v!r} is not a str")
        if "\x00" in v:
            raise _invalid(path, "the text holds a zero code unit")
        if node.kind == K_STRING:
            try:
                data = v.encode("utf-8")
            except UnicodeEncodeError:
                raise _invalid(path, "the text holds a lone surrogate") from None
            n = len(data)
            if n > node.size - 4:
                raise _invalid(path, f"{n} bytes do not fit string({node.size - 4})")
        else:
            try:
                data = v.encode("utf-16-le")
            except UnicodeEncodeError:
                raise _invalid(path, "the text holds a lone surrogate") from None
            n = len(data) // 2
            if n > (node.size - 4) // 2:
                raise _invalid(path, f"{n} units do not fit wstring({(node.size - 4) // 2})")
        out[off : off + 4] = _I32.pack(n)
        out[off + 4 : off + 4 + len(data)] = data
        return
    if isinstance(node, Bytes):
        if isinstance(v, (bytes, bytearray)):
            data = bytes(v)
        elif isinstance(v, memoryview) and v.format == "B":
            data = v.tobytes()
        else:
            raise _invalid(path, f"{v!r} is not bytes")
        if len(data) > node.size - 4:
            raise _invalid(path, f"{len(data)} bytes do not fit bytes({node.size - 4})")
        out[off : off + 4] = _I32.pack(len(data))
        out[off + 4 : off + 4 + len(data)] = data
        return
    if isinstance(node, Array):
        if not isinstance(v, list):
            raise _invalid(path, f"{v!r} is not a list")
        stride = node.elem.size
        if node.counted:
            if len(v) > node.n:
                raise _invalid(path, f"{len(v)} elements do not fit [..{node.n}]")
            out[off : off + 4] = _I32.pack(len(v))
            at = off + 4
        else:
            if len(v) != node.n:
                raise _invalid(path, f"{len(v)} elements, the array holds exactly {node.n}")
            at = off
        for i, e in enumerate(v):
            _put(node.elem, e, out, at + i * stride, f"{path}[{i}]")
        return
    if isinstance(node, Keyed):
        if node.wrap:
            if not isinstance(v, KeyedArray):
                raise _invalid(path, f"{v!r} is not a KeyedArray")
            items = v.slots()
        elif isinstance(v, list):
            items = v
        else:
            raise _invalid(path, f"{v!r} is not a list")
        if len(items) != len(node.keys):
            raise _invalid(path, f"{len(items)} slots, the key enum has {len(node.keys)}")
        stride = node.elem.size
        for i, e in enumerate(items):
            _put(node.elem, e, out, off + i * stride, f"{path}[{i + 1}]")
        return
    if isinstance(node, Union):
        if not isinstance(v, node.cls):  # type: ignore[arg-type]
            raise _invalid(path, f"{v!r} is not a {getattr(node.cls, '__name__', 'union')}")
        t = getattr(v, "type")
        if type(t) is not int or not 0 <= t <= len(node.arms):
            raise _invalid(path, f"type {t!r} is no arm of the union (0 to {len(node.arms)})")
        out[off : off + node.tag] = t.to_bytes(node.tag, "little")
        if t > 0:
            arm = node.arms[t - 1]
            if arm.node is not None and arm.attr is not None:
                _put(arm.node, getattr(v, arm.attr), out, off + node.tag, f"{path}.{arm.attr}")
        return
    raise _invalid(path, "a node this runtime does not know")


# ---------------------------------------------------------------------------
# the root: identity, lineage, LOAD and SAVE


class Entry:
    """One lineage entry: the wire hash, the record BODY size, the layout
    bytes, the retired mark, and the writer's ranges by root field id when the
    build holds the writer's declaration."""

    __slots__ = ("hash", "body", "layout", "retired", "ranges")

    def __init__(self, hash: int, body: int, layout: bytes, retired: bool = False, ranges: dict[int, tuple[Any, Any]] | None = None) -> None:
        self.hash = hash
        self.body = body
        self.layout = layout
        self.retired = retired
        self.ranges = ranges


class Root:
    """One fixed root's static data and its plans, built on first use."""

    __slots__ = ("name", "hash", "body", "floor", "lineage", "shape", "identity", "_plans", "_hashes", "_own")

    def __init__(
        self,
        name: str,
        hash: int,
        body: int,
        floor: int,
        lineage: Sequence[Entry],
        shape: Callable[[], Table],
        identity: Callable[[bytes, int], Any] | None = None,
    ) -> None:
        self.name = name
        self.hash = hash
        self.body = body
        self.floor = floor
        self.lineage = tuple(lineage)
        self.shape = shape
        self.identity = identity
        self._plans: dict[int, Reader | str] = {}
        self._hashes = {e.hash: i for i, e in reversed(list(enumerate(self.lineage)))}
        own = [e for e in self.lineage if e.hash == hash]
        if not own:
            raise ValueError(f"fixed table {name}: the lineage does not end on its own layout")
        self._own = own[-1]

    def plan(self, i: int) -> Reader | str:
        """The reader for lineage entry i, or the reason its lane refuses."""
        got = self._plans.get(i)
        if got is None:
            e = self.lineage[i]
            try:
                got = _table_reader(_parse(e.layout), self.shape(), self.name, e.ranges)
            except (_Unlawful, ValueError) as err:
                got = str(err)
            self._plans[i] = got
        return got

    def load(self, data: object, report: Report | None = None, capacity: int | None = None) -> list[Any]:
        """Every record of a form-3 file, or Refused with no value (§5.3)."""
        b = _input(data)
        if len(b) < 1:
            raise _refuse("truncated")
        if b[0] != FORM:
            raise _refuse({1: "previous_form", 2: "message_form_as_file"}.get(b[0], "newer_form"))
        if len(b) < HEADER_BYTES:
            raise _refuse("truncated")
        if any(b[1:8]):
            raise _refuse("reserved_not_zero")
        (length,) = _U32.unpack_from(b, 16)
        if HEADER_BYTES + length > len(b):
            raise _refuse("layout_malformed", "the layout runs past the file")
        (h,) = _U64.unpack_from(b, 8)
        i = self._hashes.get(h)
        if i is None:
            err = _refuse("layout_unknown", f"0x{h:016x}")
            err.layout_hash = h  # type: ignore[attr-defined]
            raise err
        if i < self.floor:
            err = _refuse("layout_unsupported", f"0x{h:016x}")
            err.layout_hash = h  # type: ignore[attr-defined]
            raise err
        e = self.lineage[i]
        if b[HEADER_BYTES : HEADER_BYTES + length] != e.layout:
            raise _refuse("layout_malformed", "the layout is not the lock's for its hash")
        identity = self.identity if e.hash == self.hash else None
        plan: Reader | str | None = None
        if identity is None:
            plan = self.plan(i)
            if isinstance(plan, str):
                raise _refuse("layout_malformed", plan)
        record = HASH_BYTES + e.body
        start = HEADER_BYTES + length
        rest = len(b) - start
        if record <= HASH_BYTES or rest % record:
            raise _refuse("truncated")
        n = rest // record
        if capacity is not None and n > capacity:
            raise _refuse("batch_too_large")
        tag = b[8:16]
        rep = Report()
        out = []
        for k in range(n):
            at = start + k * record
            if b[at : at + HASH_BYTES] != tag:
                raise _refuse("no_layout")
            if identity is not None:
                out.append(identity(b, at + HASH_BYTES))
            elif plan is not None and not isinstance(plan, str):
                out.append(plan(b, at + HASH_BYTES, rep))
        if report is not None:
            report.widened = rep.widened
        return out

    def save(self, values: Sequence[Any]) -> bytes:
        """The form-3 file of values, or InvalidValue with no bytes (§3.1)."""
        shape = self.shape()
        own = self._own
        record = HASH_BYTES + self.body
        head = bytes([FORM, 0, 0, 0, 0, 0, 0, 0]) + _U64.pack(self.hash) + _U32.pack(len(own.layout)) + own.layout
        out = bytearray(len(head) + record * len(values))
        out[: len(head)] = head
        tag = _U64.pack(self.hash)
        for k, v in enumerate(values):
            at = len(head) + k * record
            out[at : at + HASH_BYTES] = tag
            _put(shape, v, out, at + HASH_BYTES, f"{self.name}[{k}]")
        return bytes(out)


def _input(data: object) -> bytes:
    if isinstance(data, bytes):
        return data
    if isinstance(data, bytearray):
        return bytes(data)
    if isinstance(data, memoryview) and data.format == "B":
        return data.tobytes()
    raise Refused("truncated", "a read takes bytes, a bytearray or a byte memoryview")
