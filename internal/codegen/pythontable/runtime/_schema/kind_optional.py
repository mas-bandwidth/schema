"""The optional field kind (docs/SPEC-TABLES.md §2.3).

`?T` on this wire is PRESENCE: an absent optional writes no field and reads as
None, and a present one rides the wrapped kind's framing, its default included.
The descriptor names the wrapped codec in `inner`, so this module owns the
presence gate and the inner kind owns the bytes; the paths in save.py gate the
node numbering on the same presence, so an absent value owns no node.
"""

from typing import Any

from . import kinds
from .descriptor import FieldDesc


class _Optional:
    def _inner(self, fd: FieldDesc) -> Any:
        return kinds._REGISTRY[fd.inner]

    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        if value is None:
            return
        self._inner(fd).validate(fd, value, path)

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        return self._inner(fd).read(r, fd, ctx)

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        self._inner(fd).write(w, fd, value, ctx)


kinds.register("optional", _Optional())
