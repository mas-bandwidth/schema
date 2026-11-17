## THE PACKET ARRAYS

The codec carries fixed `[N]T` and counted `[A..B]T` arrays of any element
kind it carries; the core cut's list of carried and refused kinds
([PORT-PYTHON.md](../PORT-PYTHON.md)) marks them carried. A keyed `[E]T`
array and a list `[]T` stay refused by name.

- **An array is a `list`, and a counted array's length is its count.** The
  array kind (`kind_array.go`) writes a counted array's count first, as the
  ranged integer in [A, B] it is, then each element through the element
  kind's own emitter, so a kind that lands later composes with no change to
  arrays. A write refuses anything but a `list` of exactly N (`[N]T`) or of a
  length in [A, B] (`[A..B]T`) with `InvalidValue` at the field
  (`Fleet.ships`), and a bad element at its index (`Fleet.ships[3].hull`). A
  read checks the count against [A, B] before it builds an element, so a
  hostile count is `Refused` with no allocation past B. The SPEC §5 zero form
  of an untaken branch is an empty counted array and a fixed array of zero
  elements; the zero form never carries a counted array's birth at A.

The array kind keeps the packet core's straight-line shape
([PORT-PYTHON.md](../PORT-PYTHON.md), the core cut's measurement). Its
instrument is a `BenchArrays` fixture (a ranged `[16]int32`, a `[..16]uint16`
holding 12, and an `[8]bool`; 384 wire bits), read and written back 50,000
iterations after a 1,000-iteration warmup, over seven interleaved sittings per
interpreter, against a prototype descriptor walk over the same three fields (a
tuple per field, `getattr` and `setattr`, the element kind dispatched per
element). The bench host was shared and loaded, so the spread is wide; the
minimum and the median are both given:

| interpreter | shape | read min / median | write min / median |
|---|---|---|---|
| CPython 3.11.18 (floor) | straight-line generated code (kept) | 18.71 / 28.96 µs/op | 19.47 / 27.32 µs/op |
| CPython 3.11.18 (floor) | descriptor walk (prototype loop) | 20.06 / 29.93 µs/op | 19.89 / 27.55 µs/op |
| CPython 3.15.2 (current) | straight-line generated code (kept) | 22.82 / 30.71 µs/op | 17.19 / 27.42 µs/op |
| CPython 3.15.2 (current) | descriptor walk (prototype loop) | 22.85 / 30.52 µs/op | 20.88 / 28.16 µs/op |

The generated loop is faster or level on every row: the reader by 3–7% on
the floor and level on the current pin (inside the host's noise), the writer
by 1–18%. The per-element cost is the bit layer's call, which both shapes
pay, so the generator keeps one shape for the whole packet wire and no
descriptor walk ships.

The gates are `TestPythonPacketArrays` (fixture schemas of its own and the
C++-pinned goldens whose kinds are all carried, under both pinned
interpreters) and `TestPythonPacketArraysNegativeControl`, run by
`make python-packet-arrays-negative-control`.
