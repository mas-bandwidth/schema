"""Equality and repr over a value graph without recursion (docs/PORT-PYTHON.md).

A table value is a graph: a pointer may share a node or, in a value the caller
built, close a cycle, and a chain of pointers may be longer than the
interpreter's recursion limit. The generated __eq__ and __repr__ of every
table, table-side union and enum-keyed array call these two walks, which keep
their work on an explicit stack, so both are linear in the size of the graph
and neither ever raises RecursionError.

Equality is structural: two graphs are equal when every table, list and dict
reached from one matches the other, field by field. A pair already under
comparison is taken as equal while it is open, so two cycles of the same shape
are equal and a cycle compares in finite time.

A repr spells each generated object the first time the walk reaches it; every
later reach of the same object, shared or closing a cycle, is spelled
Name(...), so the text is bounded by the size of the graph.
"""

from typing import Any

from .descriptor import TableDesc, UnionDesc


def _fields(value: Any) -> tuple[str, ...] | None:
    """The slots of a generated object, or None for anything else."""
    cls = type(value)
    if isinstance(getattr(cls, "descriptor", None), (TableDesc, UnionDesc)):
        slots: tuple[str, ...] = cls.__slots__
        return slots
    if "key_max" in cls.__dict__:
        return ("_values",)
    return None


def _graph_eq(a: Any, b: Any) -> bool:
    assumed: set[tuple[int, int]] = set()
    stack: list[tuple[Any, Any]] = [(a, b)]
    while stack:
        x, y = stack.pop()
        if x is y:
            continue
        if type(x) is not type(y):
            if _fields(x) is not None or _fields(y) is not None:
                return False
            if isinstance(x, (list, dict)) or isinstance(y, (list, dict)):
                return False
            if x != y:
                return False
            continue
        slots = _fields(x)
        if slots is not None:
            pair = (id(x), id(y))
            if pair in assumed:
                continue
            assumed.add(pair)
            for name in slots:
                stack.append((getattr(x, name), getattr(y, name)))
        elif type(x) is list:
            if len(x) != len(y):
                return False
            stack.extend(zip(x, y))
        elif type(x) is dict:
            if x.keys() != y.keys():
                return False
            for key in x:
                stack.append((x[key], y[key]))
        elif x != y:
            return False
    return True


class _Text:
    """A piece of literal text on the repr stack."""

    __slots__ = ("text",)

    def __init__(self, text: str) -> None:
        self.text = text


def _graph_repr(value: Any) -> str:
    out: list[str] = []
    seen: set[int] = set()
    stack: list[Any] = [value]
    while stack:
        item = stack.pop()
        if type(item) is _Text:
            out.append(item.text)
            continue
        slots = _fields(item)
        if slots is not None:
            name = type(item).__name__
            if id(item) in seen:
                out.append(f"{name}(...)")
                continue
            seen.add(id(item))
            if slots == ("_values",):
                pieces: list[Any] = [_Text(f"{name}("), item._values, _Text(")")]
            else:
                pieces = [_Text(f"{name}(")]
                for index, slot in enumerate(slots):
                    sep = ", " if index else ""
                    pieces.append(_Text(f"{sep}{slot}="))
                    pieces.append(getattr(item, slot))
                pieces.append(_Text(")"))
            stack.extend(reversed(pieces))
        elif type(item) is list:
            if id(item) in seen:
                out.append("[...]")
                continue
            seen.add(id(item))
            pieces = [_Text("[")]
            for index, elem in enumerate(item):
                if index:
                    pieces.append(_Text(", "))
                pieces.append(elem)
            pieces.append(_Text("]"))
            stack.extend(reversed(pieces))
        elif type(item) is dict:
            if id(item) in seen:
                out.append("{...}")
                continue
            seen.add(id(item))
            pieces = [_Text("{")]
            for index, (key, elem) in enumerate(item.items()):
                sep = ", " if index else ""
                pieces.append(_Text(f"{sep}{key!r}: "))
                pieces.append(elem)
            pieces.append(_Text("}"))
            stack.extend(reversed(pieces))
        else:
            out.append(repr(item))
    return "".join(out)
