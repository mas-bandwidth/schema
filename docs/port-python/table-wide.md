## THE TABLE WIDE KINDS

- **Scope.** The table runtime carries the fixed-point family at their raw
  storage widths beside the full-width integers, for both the fixed and the
  variable class.
- **The wide kinds ride the integer codec.** `int128`, `uint128`, `fixed(I, F)`
  and `ufixed(I, F)` need no module of their own: `kind_int.py` already names
  every storage width, sixteen bytes for kinds 18 and 19 and the fixed family's
  own 1, 2, 4, 8 or 16 (kinds 20 to 29). A read takes the declared width as a
  little-endian two's-complement integer, signed by the descriptor's lower
  bound, and refuses a value outside `[lo, hi]`; a write validates the raw
  scaled integer first and writes exactly those bytes. The bounds are the raw
  scaled range (`ir.TableRawRange`), so a `fixed(112, 16)` is its 128-bit raw
  integer and never a float. `TestPythonVtWide` holds the kind alone and beside
  bool, the integers, bits, a nested table and a union-free pointer closure to
  the Go engine's canonical wire under both pins.
- **The id table is numbered by the walk.** A field's id reference is taken
  before its payload is written, so a nested table's own field id precedes the
  ids inside its body, the order the Go engine and the C++ reference use
  (SPEC-TABLES §3.1). Taking it after the body renumbers a nested table's
  fields and breaks the byte-for-byte read-then-write contract.
