## THE PACKET LAYOUT

The codec carries the `if`/`else` branch and the three storage-less items
`const(Value, Bits)`, `reserved(Bits)` and `align` (P4's layout rows); the core
cut's list of refused items ([PORT-PYTHON.md](../PORT-PYTHON.md), THE PACKET
CORE CUT) no longer includes them. Each is its own emitter file
(`item_branch.go`, `item_const.go`, `item_reserved.go`, `item_align.go`)
registered in the core's item table in `codec.go`, so a later layout construct
adds a file and one registration line. The core's refusal walk and the golden
harness's blocker no longer name them.

- **A branch stores both sides.** A write emits the taken side and a read
  reads it; the untaken side is set to its zero values, not its declared
  defaults (SPEC §5), and the same holds in the other direction. A negated
  condition (`if !on`) is one condition string, not a second shape. The
  emitter numbers its read temporaries across both arms and past the branch,
  so each temporary is bound once and mypy sees a single type per local.
- **`const` is checked on the wire.** A write emits the declared value in the
  declared width (a width of 32 bits or more rides the same `serialize_bits`
  call); a read reads that width and refuses any other value as `rejected`.
  It carries no storage, so there is nothing to validate or zero.
- **`reserved` is written zero and refused when set.** A write emits the
  declared width as zero bits; a read reads the width and refuses a nonzero
  value as `rejected`. It carries no storage either.
- **`align` pads to the next byte boundary with zero bits.** A write pads with
  zero bits and a read refuses nonzero padding as `nonzero_padding` at the
  boundary. The pad is at most seven bits and ends on a byte boundary, so in a
  byte-built stream that reaches the padding's first bit the whole pad is
  present: a pad never runs past the end.
- **No byte prefix ends inside align padding.** The padding runs from the read
  position to the next byte boundary, and a byte-built stream holds a whole
  number of bytes, so any prefix that reaches the padding's first bit holds
  all of it. A cut before the padding fails in the construct the cut is in, and
  a cut at or after it fails in the construct after the padding, in Go and in
  Python alike. The align's hostile inputs are therefore the set-padding cases,
  not a truncated pad.
- **The fixture is the gate.** `TestPythonPacketLayout` builds its own fixture
  schema in `python_layout_test.go`: `Layout` pins a one-byte wire and
  `Wide` a >32-bit const and a 40-bit reserved run, with `Nested` putting the
  layout inside another type. The probe runs under both pinned interpreters,
  checks each declared default in the constructor, holds every write to its
  pinned Go-reference bytes and every read to its pinned values (including the
  untaken side's zero values, SPEC §5), refuses every proper prefix of each
  pinned wire as `past_end` at the bit of the construct the cut stops in, and
  refuses the wrong const, the set reserved bit and the set padding at their
  exact bit. Every out-of-contract write raises `InvalidValue`. The same probe
  is mypy `--strict --python-version 3.11` clean.
- **The control plants each fault in the generated code.**
  `TestPythonPacketLayoutNegativeControl` copies the generated module and, one
  at a time, removes the const comparison, the reserved comparison, the
  alignment read and the untaken side's zeroing; the probe must go red by name
  on each. `make python-packet-layout-negative-control` runs it.
- **There is no second code shape to measure.** These items are storage-less
  beside the core's straight-line reads and writes; they add no hot path the
  core's form does not already have, so D1 has nothing to compare on this row.
- **The goldens wait for the packet close.** `probe_header`, `bench_packet`,
  `bench_mixed` and `testdata` mix these constructs with `bits`, strings,
  arrays and other kinds another packet card carries, so they stay on the
  golden harness's skip list, naming that kind, until `py-packet-close-r` reads
  every golden with an empty skip list.
