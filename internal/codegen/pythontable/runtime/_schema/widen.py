"""Widening on read (docs/SPEC-TABLES.md §4).

A narrower writer's value lands in a wider reader's field when the two kinds
sit on one ladder: the signed integer ladder is kinds 2, 3, 4, 5, 18 and the
unsigned one 6, 7, 8, 9, 19, and each kind accepts every kind below it on its
own ladder (ir.TableKindWidens). The float rung is kind 10 into kind 11. The
rule is decided by the KIND PAIR and by nothing else, so it holds wherever a
kind is compared: a field's own kind, an arm's, an array's element kind and a
map's key kind.

An integer payload is decoded at the WIRE kind's width, sign-extended on the
signed ladder and zero-extended on the unsigned one, so the value is the
writer's own exactly; it is then held to the reader's declared range like any
other value, and one widened counts for the field. A float32 payload decodes
to the same finite float64 (every finite float32 is exact there) and counts
one widened; a non-finite source refuses non_finite_float.

Every other pair is a kind mismatch, and the reverse of every widening is one:
a narrowing, a cross-ladder pair, a fixed-point kind, a bool. An array or a
map counts one widened for the field that carries it.
"""

import math
import struct
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
    11: frozenset({10}),
}

_WIDTH: Final[dict[int, int]] = {2: 1, 3: 2, 4: 4, 5: 8, 6: 1, 7: 2, 8: 4, 9: 8}
_SIGNED: Final[frozenset[int]] = frozenset({2, 3, 4, 5})


def _accepts(fd: FieldDesc, kind: int) -> bool:
    """Whether a payload under kind widens into the field fd."""
    return kind in _RUNGS.get(fd.kind, _NONE)


def _read_float(r: Any) -> float:
    """Decode a finite float32 widened into a float64."""
    raw = float(struct.unpack("<f", bytes(r.take(4)))[0])
    if not math.isfinite(raw):
        raise Refused("non_finite_float", f"{raw!r} is not a finite float")
    return raw


def _read(r: Any, fd: FieldDesc, kind: int, ctx: Any) -> int | float:
    """Decode a payload under the narrower kind into fd, counting widened."""
    if fd.kind == 11:
        raw: int | float = _read_float(r)
    else:
        raw = int.from_bytes(r.take(_WIDTH[kind]), "little", signed=kind in _SIGNED)
        if raw < fd.lo or raw > fd.hi:
            raise Refused("out_of_range", f"{raw} is outside [{fd.lo}, {fd.hi}]")
    ctx.widened += 1
    return raw
