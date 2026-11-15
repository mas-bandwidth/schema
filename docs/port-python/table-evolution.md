## THE TABLE EVOLUTION KINDS

- **Scope.** `kind_string.py`, `kind_bytes.py` and `kind_enum.py` carry
  `string(N)`, `bytes(N)` and an enum value on the form-1 wire, and a flags
  mask is an integer of the u64 kind and rides `kind_int.py`, with their
  declared defaults: a value at the default elides, an absent field reads back
  as the default, and a renamed table, table field or enum variant keeps the
  old name's hash under `was`. `compiler/carries_python_evolution.go` is the
  record: value defaults and `was` rows name no carry key, so it registers
  nothing.
- **The shape is measured.** A table whose fields are all scalars (bools,
  integers and flags masks, strings, bytes and enums) gets the straight-line
  generated save/load pair, unrolled with each field descriptor bound to a
  local; every other table, and every construct a later card adds, keeps the
  descriptor walk over `_schema/kinds.py`. The two shapes run on the same
  table, under both pinned interpreters, in `TestPythonTableCoreShapeBench`
  (integers) and `TestPythonEvolutionKindsShapeBench` (strings, bytes and
  enums), both in internal/codegen/pythontable. The straight-line pair is kept:
  for the integers its save is faster on both pins and its load is faster on
  the floor, and for the evolution kinds both its save and its load are faster
  on both pins (the bench below).
- **Evolution.** Every wire id is the FNV-1a hash of a declared or renamed
  name, never its position: a table's node type id is `fnv1a64( WireName )`,
  a field's id is `fnv1a64( TableFieldWireName )`, an enum value rides as the
  reference to `fnv1a64( VariantWireName )`, and a union arm's header carries
  `fnv1a64( WireName )`. The emitter reads those helpers for every id, so a
  table, table field or enum variant declared `| was = "old"` reads an old
  writer's bytes in silence and saves them back byte for byte. A string, bytes
  or flags default is DATA in the descriptor: the fresh value is born at it, a
  field at it elides, and a field the old writer omitted reads back as it.
  Union arm ids follow the same law in the union descriptor; their value codec
  is the unions card's.
- **Skipped is not unread** (SPEC-TABLES §4, §6.5). A field, a nested field or
  a node record the reader cannot name is walked by the engine's
  declaration-free wire rules (`internal/tablewire/wirecheck.go`) before it
  is counted `unknown`: framing, text (no NUL, valid UTF-8), bool bytes,
  finite floats, the closed element kinds of arrays and keyed bodies,
  references against the id table, the count cap and the nesting bound. Its
  pointers join the one pointer graph, judged over every slot, known or
  skipped: an index past the node table, a cycle (a pointer to the root is
  one), a record no pointer reaches and a blob named twice refuse. A record
  whose type this root cannot place keeps its index, is counted `unknown`
  under a root that places nodes, and reads null from a slot that names it;
  a slot that names a record of a type the reader declares but the slot does
  not target is damage. `TestPythonVtEvolution` holds each case to the
  engine's verdict.
- **A deprecated enum variant** (SPEC-TABLES §2.15) keeps its id and ordinal
  in the descriptor's `deprecated` set: a read that meets it yields the
  field's declared default and counts nothing, and a write of it raises
  `InvalidValue`, as the engine's decode and encode do.

### Evolution kinds bench

The evolution kinds' bench runs a six-field table (two strings, two bytes
fields and two enums, every one off its declared default) under both pins,
on a fleet host that was SHARED with other work at
the time (load average 25 to 36 on 16 cores), so the absolute figures are
high and the ratios are the record. Each figure is the minimum of five rounds
of 10,000 iterations after warm-up, in nanoseconds per operation, as the
range over three sittings:

| operation | 3.15.2 generated | 3.15.2 descriptor | 3.11.18 generated | 3.11.18 descriptor |
|---|---:|---:|---:|---:|
| save | 17,542–18,689 | 19,382–19,838 | 18,029–18,904 | 18,799–19,392 |
| load | 18,803–19,489 | 19,884–20,657 | 21,505–21,810 | 22,032–23,089 |

The generated pair was faster in every sitting, on both operations and both
pins: by 4% to 12% for the save and 3% to 6% for the load on 3.15.2, and by
2% to 4% for the save and 2% to 7% for the load on 3.11.18. So a table of
strings, bytes and enums takes the straight-line pair beside the integer
tables. `TestPythonEvolutionKindsShapeBench` re-measures it and holds both
shapes to the same value.

