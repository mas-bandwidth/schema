"""The placeholder for a construct this card's runtime does not carry yet.

The descriptor still describes the field (its name, wire id, kind and bounds),
so a tool can walk it; a read or write reaches this module and refuses by name
rather than emitting a second wire. A later card replaces the entry with its
own module and one registration line.
"""

from typing import Any

from . import InvalidValue, Refused
from . import kinds
from .descriptor import FieldDesc

_RULE = "the Python table runtime does not carry this construct yet (docs/PORT-PYTHON.md, THE CARD MAP)"


class _Unsupported:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        raise InvalidValue(path, _RULE)

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        raise Refused("unsupported_kind", _RULE)

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        raise InvalidValue(fd.name, _RULE)


kinds.register("unsupported", _Unsupported())
