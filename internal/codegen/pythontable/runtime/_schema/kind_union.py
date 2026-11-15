"""The union field kind (docs/SPEC-TABLES.md §3, kind 15).

A union rides an arm id reference, the arm's kind byte, its length and its
payload. An arm that names a declared type carries a kind-13 table body, which
is how a packet type by value in a union arm rides the form-1 table wire
(docs/PORT-PYTHON.md, THE TABLE FRAME). A payload-free arm rides kind 32 with
a zero length; every other arm kind is outside this cut and refuses by name.
"""

from typing import Any

from . import InvalidValue, Refused
from . import descriptor, kinds
from .descriptor import ArmDesc, FieldDesc


def _union(fd: FieldDesc) -> type[Any]:
    cls = descriptor._union_by_name(fd.target)
    if cls is None:
        raise InvalidValue(fd.name, f"the union {fd.target} is not registered")
    return cls


class _Union:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        cls = _union(fd)
        if not isinstance(value, cls):
            raise InvalidValue(path, f"not a {fd.target}")
        if value.type < 0 or value.type > len(cls.descriptor.arms):
            raise InvalidValue(
                path, f"arm {value.type} is outside [0, {len(cls.descriptor.arms)}]"
            )
        if value.type == 0:
            return
        arm = cls.descriptor.arms[value.type - 1]
        if arm.kind == 32:
            return
        if arm.kind != 13 or not arm.target:
            raise InvalidValue(
                f"{path}.{arm.name}", f"the union arm kind {arm.kind} is not carried yet"
            )
        target = descriptor.by_name(arm.target)
        if target is None:
            raise InvalidValue(path, f"the table {arm.target} is not registered")
        payload = getattr(value, arm.name)
        if not isinstance(payload, target):
            raise InvalidValue(f"{path}.{arm.name}", f"not a {arm.target}")
        from .save import _validate

        _validate(payload, payload.descriptor, f"{path}.{arm.name}", set())

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        from .load import read_body

        cls = _union(fd)
        union = cls()
        reference = r.varint()
        if reference == 0:
            union.type = 0
            return union
        arm_id = r.id_at(reference)
        arm: ArmDesc | None = cls.descriptor.by_id.get(arm_id)
        if arm is None:
            raise Refused("wire_damaged", f"the arm {arm_id:#x} is not in {fd.target}")
        ordinal = 0
        for index, candidate in enumerate(cls.descriptor.arms):
            if candidate.arm_id == arm_id:
                ordinal = index + 1
                break
        union.type = ordinal
        kind = r.byte()
        if kind != arm.kind:
            raise Refused(
                "kind_mismatch",
                f"{fd.target}.{arm.name} rides under kind {kind}, not {arm.kind}",
            )
        length = r.varint()
        payload = r.take(length)
        if arm.kind == 32:
            return union
        if arm.kind != 13 or not arm.target:
            raise Refused(
                "unsupported_kind", f"the union arm kind {arm.kind} is not carried yet"
            )
        target = descriptor.by_name(arm.target)
        if target is None:
            raise Refused("wire_damaged", f"the table {arm.target} is not registered")
        value = target()
        read_body(payload, value, ctx)
        setattr(union, arm.name, value)
        return union

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        from .save import Writer, write_body

        cls = _union(fd)
        w.varint(ctx.idref(fd.field_id))
        w.byte(fd.kind)
        if value.type == 0:
            w.varint(0)
            return
        arm = cls.descriptor.arms[value.type - 1]
        w.varint(ctx.idref(arm.arm_id))
        w.byte(arm.kind)
        if arm.kind == 32:
            w.varint(0)
            return
        if arm.kind != 13 or not arm.target:
            raise InvalidValue(
                f"{fd.name}.{arm.name}", f"the union arm kind {arm.kind} is not carried yet"
            )
        payload = bytearray()
        write_body(Writer(payload), getattr(value, arm.name), ctx, False)
        w.varint(len(payload))
        w.raw(payload)


kinds.register("union", _Union())
