"""The dispatch table of field codecs.

One entry per field kind. A later card adds a module kind_<name>.py under
_schema/, which registers itself (_load_every_kind below imports it); the
walk in load.py and save.py never changes. The registry maps the descriptor's codec name to one object with the four
operations a kind owns: validate, read, write and skip is not a kind's (a
field the reader cannot name is skipped by the wire kind alone).
"""

from typing import Any, Protocol

from .descriptor import FieldDesc


class FieldKind(Protocol):
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None: ...

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any: ...

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None: ...


class ElementKind(FieldKind, Protocol):
    """A kind that can also be an array element: its value without the
    field header."""

    def read_element(self, r: Any, fd: FieldDesc, ctx: Any) -> Any: ...

    def write_element(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None: ...


_REGISTRY: dict[str, FieldKind] = {}


def register(name: str, kind: FieldKind) -> None:
    _REGISTRY[name] = kind


def _load_every_kind() -> None:
    """Import every kind_<name> module beside this one, in name order. Each
    registers itself on import, so a card adds its module and no import line
    anywhere else; the order is the modules' names, the same on every run."""
    import importlib
    import pkgutil
    import sys

    package = __package__
    assert package is not None
    names = sorted(m.name for m in pkgutil.iter_modules(sys.modules[package].__path__))
    for name in names:
        if name.startswith("kind_"):
            importlib.import_module(f"{package}.{name}")


_load_every_kind()
