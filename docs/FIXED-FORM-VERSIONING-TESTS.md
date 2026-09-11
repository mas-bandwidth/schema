# The versioning tests: every row of the law, four ways

Glenn, 2026-09-11 02:25Z: "make sure you design a set of tests that exercise the various compiler errors
that we expect, and the various read refusals that we expect when old reads new." This is that design.
The bill is `FIXED-FORM-BILL-READS-BACKWARD.md`; the law is its §2 and §6; the procedures are
`FIXED-FORM-ALGORITHM.md` §5. Every row below is one definition change, and every row gets FOUR tests:

| column | what it proves | where it lives | red first |
|---|---|---|---|
| **LOCK-REFUSES** | the narrowing does not compile against the lock; the refusal names the table, the definition, the rule, the old and the new value | `internal/lockfile` (Go), one test per row | yes |
| **LOCK-ALLOWS** | the widening compiles and the lock's lineage gains one entry | `internal/lockfile` | no (a positive) |
| **NEW-READS-OLD** | the widened reader reads the older writer's file: every old value lands exactly, the reader's tail is the default, the counters are what §5.2 says | the C++ reference first (`test/tables/fixedform_main.cpp`, a lineage pair per row written by the dump), then every leg | yes, per leg |
| **OLD-REFUSES-NEW** | the older reader given the widened writer's file refuses `layout_newer` before any record, no counter moves, nothing decoded; the refusal names the row's definition where the language has text | the C++ reference first, then every leg | yes, per leg |

The two read columns share one corpus: for each row, `fixedform_dump.cpp` writes the OLD file (`vN_<row>.bin`)
and the NEW file (`vN1_<row>.bin`) from two schemas that differ by exactly that row. Each leg then has two
readers per row (the old build's and the new build's), or, where a leg cannot hold two builds of one table
in one binary, two schema names (`Old<Row>`, `New<Row>`) whose layouts are the pair. The C++ reference is
the byte authority for both files.

## The rows

Naming: `V_<row>`; the Go test is `TestLock<Row>Refuses` / `TestLock<Row>Allows`; the fixture case is
`<row>_case()`; the corpus files `old_<row>.bin` / `new_<row>.bin`.

| row | old | new (the widening) | the narrowing LOCK-REFUSES (new = the reverse) | NEW-READS-OLD lands | OLD-REFUSES-NEW names |
|---|---|---|---|---|---|
| `field_append` | `vec {x,y,z}` | `vec {x,y,z,w}` | `{x,y}`: "field removed"; `{x,w,y,z}`: "field inserted not at the end"; `{y,x,z}`: "fields reordered" | x,y,z exact; w = default | the field w |
| `field_deprecate` | `{a,b,c}` | `{a,b deprecated,c}` | `{a,c}` (removed instead of deprecated): "field removed" | a,c exact; b dropped, `unknown` += 1 per plan | (a deprecation is not newer: the OLD reader READS the new file, b landing as written) |
| `field_undeprecate` | `{a,b deprecated}` | `{a,b}` | — (allowed both ways; a test that both compile) | a,b exact | reads |
| `field_modify` | `{a int32}` | — | `{a int64}` is `int_widen`; `{a string(8)}`: "kind changed" | — | — |
| `enum_append` | `Tier {bronze silver gold}` | `+ platinum` | `{bronze gold silver}`: "variant reordered"; `{bronze silver}`: "variant removed"; `{bronze platinum silver gold}`: "variant inserted mid-list"; `{bronze silver golden}`: "variant renamed without was" | ordinals equal; a gold record reads gold | the variant platinum |
| `enum_width` | 255 variants | 256 variants (width 1 → 2)... per the ordinal width rule | a width cannot be set by hand: covered by `enum_append` at the boundary | width 1 ordinal widened into width 2, `widened` += 1 | the layout's enum width |
| `union_append` | `Pick {a b}` | `+ c` | reordered / removed / inserted / payload changed: four refusals | an `a` record lands a; the tag width equal | the arm c |
| `union_arm_payload_widen` | arm `a: {x}` | arm `a: {x,y}` | arm `a: {}`: "field removed" | x exact, y default | the field y under arm a |
| `flags_append` | `F {a b}` | `F {a b c}` | `{b a}`: "flag moved"; `{a}`: "flag removed" | mask equal | the flag c |
| `array_bounded_grow` | `[..4]int32` | `[..8]int32` | `[..2]`: "bound narrowed (4 -> 2)" | count and 4 elements exact; slots 4..7 default | the bound 8 |
| `array_fixed_grow` | `[4]int32` | `[8]int32` | `[2]`: "bound narrowed" | 4 exact, 4 default | the bound |
| `array_shape` | `[4]T` | — | `[..4]T`: "shape changed"; `[Enum]T`: "shape changed" | — | — |
| `array_elem_widen` | `[..4]int16` | `[..4]int32` | `[..4]int8`: "element narrowed" | 4 exact, widened | the element width |
| `keyed_array_enum_append` | `[Tier]int32`, Tier 3 | Tier 4 | Tier reordered: "variant reordered" (the array follows) | 3 slots exact, slot 4 default | the variant |
| `constant_grow` | `const N = 4; [..N]` | `N = 8` | `N = 2`: "bound narrowed (4 -> 2)" (the lock records the EVALUATED bound) | as `array_bounded_grow` | the bound |
| `string_grow` | `string(8)` | `string(16)` | `string(4)`: "capacity narrowed" | length and bytes exact, slack zero | the capacity |
| `wstring_grow` | `wstring(8)` | `wstring(16)` | `wstring(4)` | as string, in code units | the capacity |
| `bytes_grow` | `bytes(8)` | `bytes(16)` | `bytes(4)` | as string | the capacity |
| `text_kind` | `string(8)` | — | `wstring(8)`: "kind changed"; `bytes(8)`: "kind changed" | — | — |
| `int_widen` | `int16` | `int32`, `int64` | `int8`: "narrowed"; `uint16`: "signedness"; `float32`: "ladder" | sign-extended exact (-1, MIN, MAX), `widened` += 1 | the width |
| `uint_widen` | `uint16` | `uint32` | `uint8`; `int16` | zero-extended | the width |
| `float_widen` | `float32` | `float64` | `float32` from `float64`: "narrowed"; `int32`: "ladder" | the bits, NaN payloads included (per the FU ruling on the quiet bit) | the width |
| `range_widen` | `int32 \| 0..100` | `\| 0..200`; unranged | `\| 0..50`: "range narrowed"; `\| 10..100`: "range narrowed" | 100 lands, `clamped` == 0 | the range |
| `range_added` | `int32` | — | `int32 \| 0..100`: "range added where none was" | — | — |
| `bits_grow` | `bits(8)` | `bits(12)` | `bits(4)`: "narrowed" | exact | the width |
| `fixed_I_grow` | `fixed(8,4)` | `fixed(16,4)` | `fixed(8,8)`: "F changed"; `fixed(4,4)`: "I narrowed" | the raw scaled value exact | the width |
| `optional_add` | `T` | `?T` | `T` from `?T`: "optional removed" | present == 1, value exact | the present byte |
| `nested_append` | `Root { v Vec }`, `Vec {x,y,z}` | `Vec {x,y,z,w}` | `Vec {x,y}`: "field removed" (in the nested type, named as `Root: Vec.z`) | as `field_append`, inside Root | the field Vec.w |
| `default_change` | `a int32 = 1` | — | `a int32 = 2`: "default changed" | — | — |
| `keyword_change` | `fixed table` | — | `table`: "fixed removed"; the reverse: "fixed added" | — | (a different form, `previous_form` / `newer_form`, not `layout_newer`) |
| `closure_plain_table` | — | — | a plain `table` reached by value: compile refusal (check_fixed) | — | — |
| `closure_variable_kind` | — | — | a pointer, a map, an unbounded array in the closure: compile refusal | — | — |
| `closure_self` | — | — | a fixed table reaching itself by value, in an array, through a type, through a pointer: compile refusal | — | — |
| `rename_without_was` | `a` | `a was = b` (allowed) | `b` alone: "renamed without was" | reads | reads |

## The floor and the hash

| test | what it proves |
|---|---|
| `floor_at` | a file at the floor reads |
| `floor_below` | a file one below the floor refuses `layout_unsupported`, no counter, nothing decoded |
| `floor_raise_live` | the floor raised by one: the file that read yesterday refuses today, by name, and the lock's diff says which |
| `hash_unknown` | a hash in no lineage refuses `layout_newer` |
| `hash_known_bytes_differ` | a known hash whose layout bytes differ from the lock's refuses `layout_malformed`; the seven §1.1 cases each written against a KNOWN hash land here, not in a runtime walk |
| `hash_identity` | the reader's own hash selects the identity plan; no plan compiler runs (asserted by shape: no compile symbol reachable from load) |
| `lineage_merge` | two branches append different fields; after the merge both pre-merge files read on the merged build (the name-subset rule, bill §8a.1) |

## The old contract's tests, retired by name

Each of these asserts a forward read and is REMOVED with the runtime that did it, its row above being the
replacement: the count clamp across bounds (C1/C2's cross-version half; the hostile half stays), the range
clamp across versions (C7's cross-version half), the remap of an unknown variant to `None` (card 18), the
drop-and-count of an unknown field (E1), FX1/FX2 read "both directions" (§7 row 2 becomes NEW-READS-OLD
only, and OLD-REFUSES-NEW for the other direction).

## Counting

33 rows × up to 4 columns, the floor and hash tests, on the reference and nine legs. The reference first,
red first; then the legs from the corpus, algorithm not reference; the swarm takes the mechanical rows with
the target and the corpus file named on the card.
