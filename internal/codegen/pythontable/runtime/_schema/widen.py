"""Widening on read (docs/SPEC-TABLES.md §4).

A narrower writer's integer lands in a wider reader's field when the two kinds
sit on one signedness ladder: the signed ladder is kinds 2, 3, 4, 5, 18 and
the unsigned one 6, 7, 8, 9, 19, and each kind accepts every kind below it on
its own ladder (ir.TableKindWidens). The payload is decoded at the WIRE kind's
width, sign-extended on the signed ladder and zero-extended on the unsigned
one, so the value is the writer's own exactly; it is then held to the reader's
declared range like any other value, and one widened counts for the field.

Every other pair is a kind mismatch, and the reverse of every widening is one:
a narrowing, a cross-ladder pair, a fixed-point kind, a bool. The rule is
decided by the kind pair and by nothing else. The float rung (10 into 11)
belongs to the float kind, which this runtime does not carry yet.
"""

from typing import Any, Final

from . import Refused
from .descriptor import FieldDesc

_NONE: Final[frozenset[int]] = frozenset()

# The declared kind, and the narrower wire kinds it accepts.
_RUNGS: Final[dict[int, frozenset[int]]] = {
    3: frozenset({2}),
    4: frozenset({2, 3}),
    5: frozenset({2, 3, 4}),
    18: frozenset({2, 3, 4, 5}),
    7: frozenset({6}),
    8: frozenset({6, 7}),
    9: frozenset({6, 7, 8}),
    19: frozenset({6, 7, 8, 9}),
}

_WIDTH: Final[dict[int, int]] = {2: 1, 3: 2, 4: 4, 5: 8, 6: 1, 7: 2, 8: 4, 9: 8}
_SIGNED: Final[frozenset[int]] = frozenset({2, 3, 4, 5})


def _accepts(fd: FieldDesc, kind: int) -> bool:
    """Whether a payload under kind widens into the integer field fd."""
    return fd.codec == "int" and kind in _RUNGS.get(fd.kind, _NONE)


def _read(r: Any, fd: FieldDesc, kind: int, ctx: Any) -> int:
    """Decode a payload under the narrower kind into fd, counting widened."""
    raw = int.from_bytes(r.take(_WIDTH[kind]), "little", signed=kind in _SIGNED)
    if raw < fd.lo or raw > fd.hi:
        raise Refused("out_of_range", f"{raw} is outside [{fd.lo}, {fd.hi}]")
    ctx.widened += 1
    return raw
