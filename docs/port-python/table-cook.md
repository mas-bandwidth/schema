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
on CPython 3.15.2 and 13,504.7 ns versus 15,441.8 ns on CPython 3.11.18. Final
exact-head measurements and the complete acceptance suite are still pending.
