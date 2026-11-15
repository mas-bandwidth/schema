"""The enum-keyed array kind [E]T (docs/SPEC-TABLES.md §2.4, §3.2, kind 16).

A keyed array rides as kind 16: its element kind, the count of PRESENT slots,
then one (key reference, L, element) triple per present slot, ascending by
variant ordinal. A slot at its default is elided, and a keyed array with no
present slot is not written at all. A read places each triple by its key's
wire id: a key this reader cannot name has its element walked by the
declaration-free wire check every skipped payload gets (load.py, THE SKIP
WALK) and is counted unknown, and a slot the writer never sent keeps its default. A key reference
of 0 or past the id table, a repeated key, an element kind outside the keyed
body's closed list, a slot whose element does not fill its L exactly, and a
byte left inside the body's L each refuse the whole load.
"""

from typing import Any, Iterator

from . import InvalidValue, Refused
from . import descriptor, kinds
from .descriptor import FieldDesc
from .kind_int import _width

# The element kinds a keyed body may open with (§3, THE ELEMENT KINDS ARE
# CLOSED): the array list without 12, 15 and 17.
_ELEMENT_KINDS = frozenset((*range(1, 12), 13, *range(18, 31)))


def _elem(fd: FieldDesc) -> FieldDesc:
    if fd.elem is None:
        raise InvalidValue(fd.name, "a keyed field without an element descriptor")
    return fd.elem


def _slots(fd: FieldDesc, value: Any, path: str) -> list[Any]:
    values = getattr(value, "_values", None)
    if type(values) is not list or len(values) != len(fd.keys):
        raise InvalidValue(path, f"not the keyed array of {len(fd.keys)} slots by {fd.key_enum}")
    return values


def _rollback(ctx: Any, mark: int) -> None:
    """Forget the ids first used after mark: an elided slot leaves no id."""
    for table_id in ctx.ids[mark:]:
        del ctx.refs[table_id]
    del ctx.ids[mark:]


def _damaged(detail: str) -> Refused:
    return Refused("wire_damaged", detail)


class _Keyed:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        elem = _elem(fd)
        values = _slots(fd, value, path)
        if elem.codec == "table":
            target = descriptor.by_name(elem.target)
            for slot, current in enumerate(values):
                if target is None or not isinstance(current, target):
                    raise InvalidValue(f"{path}[{slot + 1}]", f"not a {elem.target}")
        else:
            codec = kinds._REGISTRY[elem.codec]
            for slot, current in enumerate(values):
                codec.validate(elem, current, f"{path}[{slot + 1}]")
        for slot, key in enumerate(fd.keys):
            if key == 0:
                raise InvalidValue(f"{path}[{slot + 1}]", f"the key {slot + 1} names no variant of {fd.key_enum}")

    def tables(self, fd: FieldDesc, value: Any, path: str) -> Iterator[tuple[Any, str]]:
        """The tables a keyed array holds by value, for the save's walks."""
        if _elem(fd).codec != "table":
            return
        for slot, current in enumerate(_slots(fd, value, path)):
            yield current, f"{path}[{slot + 1}]"

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        from .load import Reader, _skip_content, read_body

        elem = _elem(fd)
        length = r.varint()
        payload = r.take(length)
        if length < 2:
            raise _damaged("a keyed body too short for its header")
        body = Reader(payload, 0, length, ctx.ids)
        elem_kind = body.byte()
        count = body.varint()
        if elem_kind not in _ELEMENT_KINDS:
            raise _damaged(f"a keyed element kind {elem_kind} outside the closed list")
        if elem_kind != elem.kind:
            raise Refused(
                "kind_mismatch",
                f"{fd.name} carries element kind {elem_kind}, not {elem.kind}",
            )
        # the owner being read was built fresh, so its default is a fresh keyed
        # array of this field's own class
        out = type(getattr(ctx.current, fd.name))()
        values = out._values
        target = descriptor.by_name(elem.target) if elem.codec == "table" else None
        codec = kinds._REGISTRY[elem.codec]
        seen: set[int] = set()
        for _ in range(count):
            reference = body.varint()
            key_id = body.id_at(reference)
            if reference in seen:
                raise _damaged("the same keyed slot twice in one keyed body")
            seen.add(reference)
            size = body.varint()
            element = body.take(size)
            try:
                slot = fd.keys.index(key_id)
            except ValueError:
                # SKIPPED IS NOT UNREAD (§4): a slot this reader cannot name is
                # walked by the same declaration-free wire check every skipped
                # payload gets, then counted
                _skip_content(element, elem_kind, ctx, ctx.depth)
                ctx.unknown += 1
                continue
            if target is not None:
                obj = target()
                read_body(element, obj, ctx)
                values[slot] = obj
                continue
            sub = Reader(element, 0, size, ctx.ids)
            values[slot] = codec.read(sub, elem, ctx)
            if sub.pos != sub.end:
                raise _damaged("bytes left inside a keyed slot after its element")
        if body.pos != body.end:
            raise _damaged("bytes left inside a keyed array's L after its slots")
        return out

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        from .save import Writer, write_body

        elem = _elem(fd)
        values = value._values
        # THE FIELD'S ID IS USED FIRST, then each present slot's key and its
        # element's ids, in the order the bytes ride (§3)
        mark = len(ctx.ids)
        reference = ctx.idref(fd.field_id)
        slots = bytearray()
        sw = Writer(slots)
        present = 0
        for slot in range(len(fd.keys)):
            current = values[slot]
            slot_mark = len(ctx.ids)
            key = ctx.idref(fd.keys[slot])
            element = bytearray()
            if elem.codec == "table":
                write_body(Writer(element), current, ctx, False)
                if len(element) <= 1:
                    _rollback(ctx, slot_mark)  # an all-default slot elides
                    continue
            else:
                if elem.is_default(current):
                    _rollback(ctx, slot_mark)  # a default slot elides
                    continue
                _write_scalar(Writer(element), elem, current)
            sw.varint(key)
            sw.varint(len(element))
            sw.raw(element)
            present += 1
        if present == 0:
            _rollback(ctx, mark)  # no present slot: the field is not written
            return
        inner = bytearray()
        iw = Writer(inner)
        iw.byte(elem.kind)
        iw.varint(present)
        iw.raw(slots)
        w.varint(reference)
        w.byte(fd.kind)
        w.varint(len(inner))
        w.raw(inner)


def _write_scalar(w: Any, elem: FieldDesc, value: Any) -> None:
    if elem.codec == "bool":
        w.byte(1 if value else 0)
        return
    w.raw(int(value).to_bytes(_width(elem), "little", signed=elem.lo < 0))


kinds.register("keyed", _Keyed())
