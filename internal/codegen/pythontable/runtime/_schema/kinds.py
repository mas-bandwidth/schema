"""The dispatch table of field codecs.

One entry per field kind. A later card adds a module under _schema/ and one
registration line there; the walk in load.py and save.py never changes. The
registry maps the descriptor's codec name to one object with the four
operations a kind owns: validate, read, write and skip is not a kind's (a
field the reader cannot name is skipped by the wire kind alone).
"""

from typing import Any, Protocol

from .descriptor import FieldDesc


class Kind(Protocol):
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None: ...

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any: ...

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None: ...


REGISTRY: dict[str, Kind] = {}


def register(name: str, kind: Kind) -> None:
    REGISTRY[name] = kind
