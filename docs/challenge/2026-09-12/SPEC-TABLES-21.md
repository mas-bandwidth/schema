## 21. The fixed table reads backward, never forward

**A fixed table's file is read by a build at least as new as the one that wrote it, and by no other.**
A newer reader reads every older file. An older reader given a newer file refuses it by name, before any
record, and reads nothing. There is no partial read of a fixed table: no clamp across versions, no value
landed as `None` for want of a name, no field dropped because the reader lacked it. (Glenn, 2026-09-10:
"Newer versions of the fixed table should be able to read OLD versions. But old versions CANNOT read new
versions, and complain loudly and refuse. Nothing else makes sense." The bill is
`docs/FIXED-FORM-BILL-READS-BACKWARD.md`; the algorithm is `docs/FIXED-FORM-ALGORITHM.md` §5.)

### 21.1 The law: everything that inputs into a fixed table only widens

A fixed table's closure is a tree of fixed things (§21.4), and every definition in it may only WIDEN over
time. The baseline (§18) refuses any commit that narrows, naming the definition and the rule. Widening is
defined by the default it fills: a change with no default to fill is not a widening.

| definition | widens by | never |
|---|---|---|
| a fixed table, a `type` or `table` in its closure | a field ADDED AT THE END; a field deprecated in place | a field modified, removed, or inserted elsewhere |
| an enum | a variant ADDED AT THE END; deprecated in place | a variant removed, reordered, inserted mid-list, or renamed without `was` |
| a union | an arm ADDED AT THE END | an arm removed, reordered, or its payload changed |
| `flags` | a flag ADDED | a flag removed or moved |
| an array `[N]`, `[..N]`, a `string(N)`, `wstring(N)`, `bytes(N)`, and any constant behind them | a LARGER N | a smaller N; a shape change (`[N]` to `[..N]`, a key enum swapped); a text kind change |
| an element type | the widening ladder, as a field | narrowed |
| an integer or float | wider, same ladder, same signedness (`int32` to `int64`, `f32` to `f64`) | narrower, the other ladder, the other signedness |
| a ranged scalar | a range moved OUTWARD, or REMOVED | moved inward; a range ADDED where none was |
| a compressed float | it RIDES AS THE FLOAT here (§3.4), so min, max and resolution are DEFINITIONS and never wire: the range moved OUTWARD like any ranged scalar's, and the resolution moved FINER — a writer quantizes to its own step, so an older file's values sit on a COARSER grid this reader lands exactly | a range moved inward; a resolution COARSENED (the old values are off this reader's grid); anything else |
| `bits(N)` | a larger N | smaller |
| `fixed(I,F)` | a larger I | F changed |
| an optional | `T` to `?T` (old values land present) | `?T` to `T` |
| a field's kind | — | changed |
| a specified default | — | changed (§18: it defines what every old file means) |
| the `fixed` keyword | — | added or removed (a different form, not a version) |
| a reader-side limit a table declares | larger | smaller |
| a deprecated field | stays written, in its place, read on every plan | leaving the layout; undeprecating (one way, bill §12.3) |

### 21.2 What a newer reader does with an older file

A field the reader added: its declared default. A field the reader deprecated: dropped, counted once under
`unknown` per plan. A narrower integer or float: widened exactly, `widened` counts. A shorter array or
string: landed, the reader's slack is template zeros. An older enum: its ordinals are the reader's, the
list being a prefix. Everything else: copied.

### 21.3 What an older reader does with a newer file

Refuses, by name, at load, before any record: **`layout_newer`**. No counter moves. REFUSE is total. The
refusal carries what the reader could not hold, as its language can name it.

### 21.4 The closure rule

Every table or type a fixed table reaches by value is itself declared `fixed`, with its own layout and its
own lineage. A pointer, a map, or an unbounded array in the closure is refused, so a fixed table never
reaches itself.

### 21.5 The lineage, the floor, and the plans

`schema.lock` keeps each fixed table's LINEAGE: every layout it has had, in order, with its hash and its
layout bytes. The layout hash is the version; nothing on the wire carries a number. A FLOOR per table names
the oldest supported entry; the reader accepts the lineage from the floor to its own layout and refuses
older ones as `layout_unsupported`. Because the accepted set is finite and known at build time, the plan
for each supported version is compiled at build time from the lock and shipped as static data; at load the
file's hash selects a plan, the file's layout bytes are compared to the known bytes for that hash, and a
mismatch is `layout_malformed`. The reader never parses a layout it has not seen before.

### 21.6 The deployment rule, and the lifecycle

Readers ship before writers, by a margin a reader can roll back across; the floor is the delta between the
live clients and the backend. When a table has grown past sense: create a new table, deprecate the old,
clean out the cruft, ship the backend reading both, then the client, and when nothing live speaks the old
table, remove it.
