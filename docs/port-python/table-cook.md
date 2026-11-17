# Cooked table form

The Python table runtime exposes `open_cook`, `check_cook`, `cook_measure` and
`cook` in the generated package's `_schema.cook` module. Open matches the
header and base placement without walking the region. Check additionally scans
the attribution directory and the storage it describes, without following
references. A write validates the value before producing bytes, numbers the
reachable graph and writes either byte order. A root whose forms declaration
does not carry `cook` is refused; nested tables are governed by the root's
operation.

A shared blob is explicit: assign the same package `BlobRef` to the slots that
name one node. Ordinary `str`, `bytes`, `bytearray`, and `memoryview` values are
per-slot values. The wire's shared-blob refusal remains separate from cook's
ability to preserve the graph. See [the BlobRef contract](table-blobref.md).

The cook writer keeps a descriptor walk for general records and emits a
straight-line store helper for unguarded records of scalar booleans, integers
and enums. `TestPythonCookShapeFunctional` compares full cooks from both shapes
on the same ten-field integer fixture, checks their bytes and plants a fault
in the generated store. On a shared Linux bench before final integration, best
of three measurements were 13,225.1 ns generated versus 15,280.7 ns descriptor
on CPython 3.15.2 and 13,504.7 ns versus 15,441.8 ns on CPython 3.11.18.

Re-measured after integration, on main at 7a7d4071d1 (the cook runtime
unchanged since), by `TestPythonCookShapeFunctional` alone (`-p 1`) on a
32-core Linux x86-64 fleet bench under other load, best of three rounds of
2,000 cooks of the same fixture, in nanoseconds per cook:

| pin | generated | descriptor | generated faster by |
|---|---:|---:|---:|
| CPython 3.15.2 | 50,154.1 | 57,877.4 | 13% |
| CPython 3.11.18 | 52,239.2 | 62,096.9 | 16% |

The absolute figures carry the bench's load; the ordering held in both runs,
so the generated store stays. On the same bench `TestPythonCookOpenFunctionalScale`
opened sparse 1 MiB, 100 MiB and 1 GiB cooks in 5,879, 5,678 and 5,959 ns
(3.15.2) and 7,196, 7,359 and 7,351 ns (3.11.18): open does no per-size work.
