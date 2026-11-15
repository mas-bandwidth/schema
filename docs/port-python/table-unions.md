## THE TABLE UNIONS

- **A union arm is a field line.** `kind_union.py` frames the arm id, the
  arm's kind and its `L`, and reads or writes the payload through the arm's own
  kind module: a table body, a scalar, a pointer index or a nested union. An
  array of unions is kind `14` over element kind `15`, one union payload per
  element in place, a `None` element the single zero reference. The selected
  table and pointer arms are graph edges, so the writer numbers them and the
  validator descends them.
- **The engine's verdict is the leg's.** Every union read and write agrees with
  the Go engine (internal/tablewire), held case by case in
  `TestPythonVtUnions`, which asks the engine in the test itself. A union
  field whose arm reference is `0` reads as `None`, as the engine reads it; an
  arm this reader cannot name reads as `None`, counted unknown, and its
  payload is judged by the engine's wire-alone rules, as is any skipped field
  (`load._check_content`, the port of `wirecheck.go`). The engine judges the
  whole file's framing before its decode compares a byte with a declaration,
  so a refusal that is not damage runs that walk over the file first, on the
  refusal path only, and damage anywhere outranks it. A count past the bound
  is `count_over_bound`; a counted array below its minimum is read as it
  stands, born empty when absent, and refused only by the writer.
- **Elision is per element, never by `==`.** A counted array of unions is left
  out only when it is empty, a fixed one only when every element's selected
  arm is `None`, and a by-value table only when every field of it is left out,
  judged through the descriptors (`FieldDesc.is_default`), so an inactive arm
  member that is set, or a user `__eq__`, moves no byte.
- **An array of packet unions waits for the packet layer.** The target calls
  the shared `refuseUnported` chain and registers as a union-array carrier
  (compiler/carries_python_unions.go), so an array of table-side unions (a
  union with a general arm) is carried; its one addition to the chain,
  `pythonRefusePacketUnionArrays` (compiler/target_python.go), names an array
  of a union whose arms are all packet types until P4 carries packet types
  into tables.
- **Not here.** A scalar arm under a kind its declared kind widens to is the
  widening card's; this leg refuses it `kind_mismatch` until that lands.

### Unions bench

A table that holds a union is variable, so its load and save are the
descriptor walk. Inside it the union arm has two shapes each way. On load, a
length kind's arm (a table or an array) is either read in place, by a reader
bounded to the arm that starts at the arm's `L`, which is the field's own `L`,
or copied into a new buffer behind a fresh length prefix and read from there.
On save, a table arm is either written straight into the caller's writer,
because its field line is the arm header byte for byte, or built in a
temporary buffer whose header is then split from its payload, the shape every
fixed-width and pointer arm still takes. `TestPythonUnionShapeBench`
(internal/codegen/pythontable) times both on a `[..32]Pick` holding a third
each of table, `int32` and `*Leaf` arms beside one union field, under both
pins. Each figure is the minimum of five rounds of 2,000 iterations after
warm-up, taken over six sittings, in microseconds per operation, on a shared
fleet bench whose load average stood between 10 and 16 on 16 cores:

| operation | 3.15.2 | 3.11.18 |
|---|---:|---:|
| save, table arms written in place (kept) | 131.2 | 132.5 |
| save, every arm through a temporary buffer | 135.9 | 139.3 |
| load, arms read in place (kept) | 105.6 | 122.5 |
| load, length-kind arms copied and re-framed | 114.3 | 130.3 |

The in-place save is 3% faster under 3.15.2 and 5% under 3.11.18, and the
in-place load 8% and 6%; in 22 of the 24 comparisons across both pins the
kept shape was the faster. `kind_union.py` binds the `load` and `save`
modules once at import rather than importing inside each call: an earlier
build that imported per call measured 173.5 µs to save and 135.9 µs to load
under 3.15.2 on the same bench (minima of six sittings at a load average
between 3 and 23), and its in-place load was then slower than the copy, so
the per-call import, not the shape, was the cost.
