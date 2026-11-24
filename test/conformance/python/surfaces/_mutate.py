"""The 2026-11-22 write-refusal kinds and the fresh-value wire `-`
(test/conformance/README.md, "The 2026-11-22 surfaces"; FORMAT.md).

Every write-refusal surface of this leg applies these kinds the same way, so
the four writers are handed the same value. A kind this leg's storage cannot
hold answers UNREPRESENTABLE: the driver never answers `refused` for the
runtime.
"""

from copy import deepcopy

from _common import read_bytes

UNREPRESENTABLE = object()

SPECIAL = ("elems", "mask", "dupkey", "point")


def fresh_or_load(cls, wire):
    """The row's starting value: `-` is the root's fresh value (SF-05, SF-06:
    a root with no file form), anything else the root's own form's read."""
    if wire == "-":
        return cls()
    return cls.load(read_bytes(wire))


def split(path):
    return path.replace("[", ".").replace("]", "").split(".")


def _step(node, part):
    if part.isdigit():
        idx = int(part)
        return node._values[idx] if hasattr(node, "_values") else node[idx]
    return getattr(node, part)


def resolve(value, path):
    node = value
    for part in split(path):
        node = _step(node, part)
    return node


def _owner(value, path):
    parts = split(path)
    owner = value
    for part in parts[:-1]:
        owner = _step(owner, part)
    return owner, parts[-1]


def _get(owner, last):
    if last.isdigit():
        return _step(owner, last)
    return getattr(owner, last)


def _put(owner, last, new):
    if last.isdigit():
        slots = owner._values if hasattr(owner, "_values") else owner
        slots[int(last)] = new
    else:
        setattr(owner, last, new)


def apply(value, path, rule, token):
    """Apply one of the 2026-11-22 kinds, or `count -1`. Returns True when the
    kind was applied, UNREPRESENTABLE when this leg's storage cannot hold the
    value, and False when the kind is not one of these (the caller's own
    mutation runs)."""
    if rule not in SPECIAL and not (rule == "count" and int(token) < 0):
        return False
    owner, last = _owner(value, path)
    if rule == "elems":
        # `[N]T` is a Python list and `[E]T` a class over a list (_values):
        # both hold any element count, so the writer is the one to judge it.
        current = _get(owner, last)
        count = int(token)
        if count < 0:
            raise ValueError("elems takes a nonnegative count: " + token)
        if hasattr(current, "_values"):
            slots = current._values
            current._values = slots[:count] + [deepcopy(slots[0]) for _ in range(count - len(slots))]
            return True
        if type(current) is not list:
            raise TypeError(path + ": elems names no fixed or keyed array")
        _put(owner, last, current[:count] + [deepcopy(current[0]) for _ in range(count - len(current))])
        return True
    if rule == "mask":
        # a flags field is a Python int: any raw bits are held
        _put(owner, last, int(token))
        return True
    if rule == "dupkey":
        current = _get(owner, last)
        if type(current) is not dict:
            raise TypeError(path + ": dupkey names no map")
        if token == "0":
            return True
        if token == "1":
            # a map is a Python dict, which holds each key once: a key held
            # twice has no spelling in this leg's storage
            return UNREPRESENTABLE
        raise ValueError("dupkey takes 0 or 1: " + token)
    if rule == "point":
        # the pointer at <path> takes the very node the pointer at <token>
        # points at (FL-28: pointed at an enclosing node it is a cycle)
        _put(owner, last, resolve(value, token))
        return True
    if rule == "count" and int(token) < 0:
        # SF-28: an array is a native list, so a negative count is a value
        # that is no list
        _put(owner, last, int(token))
        return True
    return False
