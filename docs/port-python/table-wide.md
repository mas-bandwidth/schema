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
- **The row.** `TestPythonVtWideRows` answers the roadmap row over the
  conformance manifest, not only over constructs. Every `instance` row whose
  unit's table closure carries a wide kind is held: `scalars_full`,
  `scalars_default` and `scalars_edges` (a `fixed table` root, read as form 3
  through the generated fixed form) and `widelist_wide` (a variable root, read
  as form 1). Each is read once and written back byte-identical to the
  canonical wire with its measure equal to the length and the loaded value
  unchanged, or refused with the caller's `bytearray` untouched, under both
  pinned interpreters. The instances are selected from the units' own
  `ir.TableWideFields`, so a corpus addition that uses the kinds is held
  without a list here; the test also fails if the selection stops covering both
  forms. The report rows in the same units (`scalars_full_as_2`,
  `scalars_edges_as_2`, `field_scalar_widen` and the previous-form rows) are
  the widening cell's, and stay green there. The control plants the `uint128`
  width in `kind_int.py` (the form-1 instance goes red) and the fixed form's
  signed read in `fixedform.py` (the form-3 instances go red), each in a copy
  of the emitted runtime. The wide kinds add no hot path of their own: they
  ride the integer codec the table core's bench already measures.
