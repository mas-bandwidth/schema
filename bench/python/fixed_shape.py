"""The Python leg's fixed-form code-shape measurement (docs/PORT-PYTHON.md, D1).

usage: python -I fixed_shape.py <generated dir> <corpus dir> [iterations]

D1 asks the leg to keep whichever of two shapes measures faster. For the
fixed form the two are both in the shipped code: the runtime's plan (one
reader closure per node, built once per lineage entry from the table's shape,
the path every older entry takes) and the generated straight-line reader
(_s_fl_<Root>: each field at its constant offset, runs of plain integers fused
into one struct.unpack_from), which the identity lane takes. This loads the
reference's own files through the public load_fixed both ways, checks the two
land equal values, and prints microseconds per load. It is a measurement,
not a test: it uses the clock, so it is a functional run (make
bench-python-fixed-shape), never part of make test.
"""

import sys
import time
from typing import Any, Callable

sys.path.insert(0, sys.argv[1])

from tabledemo import Keyed, Pack  # noqa: E402
from tblfx1 import FX1  # noqa: E402
from tblfx1._schema import equal  # noqa: E402

CORPUS = sys.argv[2]
ITERATIONS = int(sys.argv[3]) if len(sys.argv) > 3 else 2000


def timed(fn: Callable[[], Any]) -> float:
    fn()
    t0 = time.perf_counter()
    for _ in range(ITERATIONS):
        fn()
    return (time.perf_counter() - t0) / ITERATIONS * 1e6


def main() -> None:
    print(f"CPython {sys.version.split()[0]}, {ITERATIONS} loads each, microseconds per load")
    for name, cls, root, file in (
        ("fx1.bin (FxRoot, 2 records)", FX1.FxRoot, FX1._s_fixed_FxRoot, "fx1.bin"),
        ("pack.bin (PackConfig, 2 records)", Pack.PackConfig, Pack._s_fixed_PackConfig, "pack.bin"),
        ("keyed.bin (KeyedConfig, 2 records)", Keyed.KeyedConfig, Keyed._s_fixed_KeyedConfig, "keyed.bin"),
    ):
        with open(f"{CORPUS}/{file}", "rb") as fh:
            data = fh.read()
        line = cls.load_fixed(data)
        straight = timed(lambda: cls.load_fixed(data))
        identity, root.identity = root.identity, None
        try:
            planned = cls.load_fixed(data)
            plan = timed(lambda: cls.load_fixed(data))
        finally:
            root.identity = identity
        if not equal(line, planned):
            raise SystemExit(f"{name}: the straight-line reader lands other values than the plan")
        print(f"  {name}: plan {plan:.2f}, straight-line {straight:.2f} ({plan / straight:.2f}x)")


main()
