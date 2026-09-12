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
| **OLD-REFUSES-NEW** | the older reader given the widened writer's file refuses `layout_newer` before any record, no counter moves, nothing decoded; the refusal carries the file's hash and nothing else (bill §12.4) | the C++ reference first, then every leg | yes, per leg |

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
| `field_deprecate` | `{a,b,c}` | `{a,b deprecated,c}` | `{a,c}` (removed instead of deprecated): "field removed" | a,b,c exact; b read on every plan, no counter (bill §12.3) | (a deprecation is not newer: the OLD reader READS the new file) |
| `field_undeprecate` | `{a,b deprecated}` | — | `{a,b}`: "undeprecated" (one way, bill §12.3) | — | — |
| `field_modify` | `{a int32}` | — | `{a int64}` is `int_widen`; `{a string(8)}`: "kind changed" | — | — |
| `enum_append` | `Tier {bronze silver gold}` | `+ platinum` | `{bronze gold silver}`: "variant reordered"; `{bronze silver}`: "variant removed"; `{bronze platinum silver gold}`: "variant inserted mid-list"; `{bronze silver golden}`: "variant renamed without was" | ordinals equal; a gold record reads gold | the variant platinum |
| `enum_width` | 255 variants | 256 variants (width 1 → 2) | a width cannot be set by hand: covered by `enum_append` at the boundary | width 1 ordinal WIDENED into width 2, `widened` += 1; a union crossing 255 arms the same, and the guard compares at the tag's width (bill §12.7) | the hash |
| `union_append` | `Pick {a b}` | `+ c` | reordered / removed / inserted / payload changed: four refusals | an `a` record lands a; the tag width equal | the arm c |
| `union_arm_payload_widen` | arm `a: {x}` | arm `a: {x,y}` | arm `a: {}`: "field removed" | x exact, y default | the field y under arm a |
| `flags_append` | `F {a b}` | `F {a b c}` | `{b a}`: "flag moved"; `{a}`: "flag removed" | mask equal | the flag c |
| `array_bounded_grow` | `[..4]int32` | `[..8]int32` | `[..2]`: "bound narrowed (4 -> 2)" | count and 4 elements exact; slots 4..7 default | the bound 8 |
| `array_fixed_grow` | `[4]Vec` | `[8]Vec` | `[2]`: "bound narrowed" | 4 exact, 4 at the ELEMENT DEFAULT (a nested Vec's own defaults, bill §12.6) | the hash |
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
| `cfloat_res_refine` | `float32 \| min = -1, max = 1, resolution = 0.1` | `resolution = 0.01` | — (the REFINEMENT is the widening; its reverse is `cfloat_res_coarsen`) | every old value lands EXACTLY: the old writer quantized to `0.1`, and `0.1` is a whole multiple of this reader's `0.01`, so nothing requantizes and `clamped == 0` | the hash — the resolution is in the DEFINITIONS DIGEST (`'Q'`, bill §13), so the wire hash moves when the step does and the older reader refuses `layout_newer` |
| `cfloat_res_coarsen` | `float32 \| min = -1, max = 1, resolution = 0.01` | — | `resolution = 0.1`: "resolution coarsened (0.01 -> 0.1)" | — | — |
| `cfloat_range_widen` | `float32 \| min = -1, max = 1, resolution = 0.01` | `min = -2, max = 2, resolution = 0.01` | `min = -1, max = 0.5`: "range narrowed" | `1.0` lands, `clamped == 0` — a compressed float RIDES AS THE FLOAT (SPEC §3.4), so the bounds follow the ranged-scalar rule | the range, by the hash |
| `bits_grow` | `bits(8)` | `bits(12)` | `bits(4)`: "narrowed" | exact | the width |
| `fixed_I_grow` | `fixed(8,4)` | `fixed(16,4)` | `fixed(8,8)`: "F changed"; `fixed(4,4)`: "I narrowed (8 -> 4)"; `fixed(12,4)` from `fixed(8,8)`: "F changed" (I + F EQUALS a storage width per SPEC §4.6, so with F held a narrowed I IS a narrowed kind, and the only same-storage move is I against F) | the raw scaled value exact | the width |
| `fixed_I_grow_element` | `[4]fixed(12,4)` | `[4]fixed(28,4)` | `[4]fixed(12,4)` from `[4]fixed(28,4)`: "element I narrowed (28 -> 12)" | as the scalar row, per slot | the element width |
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

36 rows × up to 4 columns, the four DIVERGENCE rows below (§5.8's 4, 9, 11, 12), the floor and hash
tests, on the reference and nine legs. The reference first,
red first; then the legs from the corpus, algorithm not reference; the swarm takes the mechanical rows with
the target and the corpus file named on the card.

## Hostile rows, per plan (bill §12.5)

For every bounded row above, one more test on the reference: a forged value in the OLD file past the OLD
writer's bound (a count of 7 where the old writer declared `[..4]`, an ordinal 5 where the old enum had 3,
a tag past the old arm count, a scalar past the old range), read by the NEW reader whose own bound is
wider: it clamps or lands `None` against the WRITER's bound carried by the plan, and counts, never landing
a value the old writer could not have written. Named `<row>_hostile_case()`.

**The compressed-float rows take the same hostile pass, and it clamps to the WRITER's range and COUNTS.**
`hostile_cfloat_range_widen.bin` is `old_cfloat_range_widen.bin` with `aim` set to a float OUTSIDE the old
writer's `[-1, 1]` but inside the new reader's `[-2, 2]`: the new reader clamps it to the OLD writer's bound
the plan carries — never its own wider one, never the forged value — and `clamped == 1` exactly, every other
counter `0`. The resolution takes no hostile row of its own: a value off the old grid is still a float32 the
reader lands, and the law's refusal for a moved step is the LOCK's and the HASH's, not a counter's.

**"A min that moves outward changes what the stored value MEANS" is form 1's concern, not this wire's.** In
the variable form a compressed float rides as a QUANTIZED INDEX — the integer `round((v - min) / res)` — so
moving `min` or `res` reads every stored index as a different number and nothing can be refused after the
fact. In the FIXED form the field rides as the float32 itself (SPEC §3.4: "it rides as the float, not as a
quantized index"), so min, max and resolution are DEFINITIONS in the digest: the stored bytes mean the same
float whatever the bounds say, which is exactly why the range may WIDEN and the resolution may REFINE here
and may not there.

## The divergence rows: §5.8's 4, 9, 11, 12

Four more rows, one per divergence of `FIXED-FORM-ALGORITHM.md` §5.8 that NO row above can reach — a
row whose subject is not a definition change but a place the reference and this page disagree. They are
written the same way and they owe the same things: the schema pair or the forged file, the expected verdict,
the expected counters, and WHICH of the four ways apply — a `—` is a column the row cannot have rather than
one nobody wrote. **The counters are asserted EXACTLY and never `>= 1`**: every one of these four is a row
where the right number counted in the wrong place, or counted twice, is the bug, so a `>=` passes the read it
exists to catch. The reference and the dump are Johnny's; **every other leg builds the fixture from THIS TEXT
and the row's manifest line, never from `fixedform_dump.cpp`** (§5.9 #32, #37).

| row | the pair / the forged file | the read | the verdict | the counters | the four ways |
|---|---|---|---|---|---|
| `writer_bound_count` (§5.8 row 4) | `VOLD_/VNEW_array_bounded_grow` as they stand — `[..4]int32` → `[..8]int32`, the reader's bound WIDER than the writer's — plus one forged file, `hostile_array_bounded_grow.bin`: `old_array_bounded_grow.bin` with the count word set to `7`, a value BETWEEN the writer's `4` and the reader's `8` | the NEW build reads the forged OLD file through its lineage, so the plan is a COMPILED one | `n == 1`; `vals_count == 4` — the WRITER's bound carried by the plan, never the reader's `8` and never the forged `7`; `vals[0..3]` exactly `1000, 1001, 1002, 1003`; `vals[4..7]` the reader's declared default; `lead == 0xAAAAAAAA` and `trail == 0xBBBBBBBB` | `clamped == 1` EXACTLY — the `count` op, once per entry per record (§5.4) — and `unknown == 0`, `kind_mismatch == 0`, `widened == 0`, `malformed` false, `refused` false | NEW-READS-OLD only. LOCK-REFUSES / LOCK-ALLOWS are `array_bounded_grow`'s own and are not repeated; OLD-REFUSES-NEW is `—`: the forged file carries the OLD layout, which the OLD reader takes by identity |
| `refuse_writes_nothing` (§5.8 row 9, the PRE-PASS row) | `VOLD_/VNEW_nested_append` as they stand — the appended nested field `Vec.w = 88` is a NONZERO prefill, which is the only thing that can tell a prefill that ran from one that did not — plus one forged file, `nolayout_nested_append.bin`: **SIXTY-FOUR records written by the OLD build**, with the PER-RECORD hash word of **RECORD 7** (that record's first eight bytes) inverted and every other record left alone, the header's hash untouched | the NEW build reads it: the header's hash selects the OLD lineage entry, so a COMPILED plan with a NONEMPTY fill list is in hand, and **records 0 through 6 are perfectly good** — the whole point of the row. §5.3 STEP 10b's PRE-PASS walks all 64 records' hashes before step 11 lands the first one, so nothing is written; the check INSIDE the landing loop, which is what every leg shipped before this row, would have prefilled, run and bounded seven rows before refusing | `n == -1`, `refused` true, `reason == no_layout`, `malformed` FALSE (the joint assertion of §5.3), `layout_hash` untouched — **and the caller's storage, all 64 rows of it, POISONED with `0x5A` before the load, is `0x5A` in EVERY BYTE after it**: not the prefill's `88` in `v.w` of row 0, not a zero anywhere, nothing, in row 0 and row 6 as much as in row 63 | every counter `0` — REFUSE IS TOTAL | OLD-REFUSES-NEW's sibling, run as its own column. NEW-READS-OLD is `nested_append`'s own; LOCK-* are `—` (no definition changed) |
| `unknown_census` (§5.8 row 11) | A NEW PAIR, `VOLD_/VNEW_unknown_census`: OLD `fixed table Item { a int32 = 0, drop int32 = 0 }` and `fixed table Census { lead uint32 = 1, items [4]Item, trail uint32 = 2 }`, NEW the same with `Item.drop` REMOVED. **The pair is deliberately UNLAWFUL** — §5.1 refuses a removal, which is the row's LOCK column — so the lineage entry is HANDED IN by the probe (§5.9 #1's `GenerateLineage(u, lineage)`, the lock played in one line) and never read from a lock. One file, `old_unknown_census.bin`, four elements, every `a` and every `drop` set | the NEW build reads the OLD file through the handed-in entry | `n == 1`; `items[0..3].a` exactly `10, 11, 12, 13`; `lead`/`trail` stand; `Item.drop` lands nowhere | **`unknown == 1`** — once per FIELD per peer, NOT once per element: `Item.drop` is one field of one peer however many of the four elements carry it, and `4` is the divergence. `kind_mismatch == 0`, `widened == 0`, `clamped == 0`, `malformed` and `refused` false. The census lands ONCE, after the record loop, on a read that returns (§5.9 #6), so a SECOND read of the same peer reports `unknown == 1` again | LOCK-REFUSES: `Item { a }` from `Item { a, drop }` names the table, `Item.drop`, "field removed", and the two field lists. NEW-READS-OLD as above. LOCK-ALLOWS and OLD-REFUSES-NEW are `—`: there is no lawful widening here and no lawful newer file |
| `forged_ordinal_both_plans` (§5.8 row 12) | `VOLD_/VNEW_enum_append` as they stand — `Tier { Bronze, Silver, Gold }` → `+ Platinum` — plus one forged file, `hostile_enum_append.bin`: `old_enum_append.bin` with `r0.tier` set to `4`, an ordinal PAST the writer's three variants and a name the reader does have | **the SAME bytes read TWICE**: once by the NEW build (the COMPILED plan, selected through the lineage) and once by the OLD build (the IDENTITY plan, its own hash) | both reads `n == 1`; both land `tier == None` and never `Platinum` — the plan's variant count is the WRITER's three; both leave `seq == 9` | **`clamped == 1` on BOTH plans, the same number on both** — the BOUNDS pass's count, once per field (§5.4), and the equality of the two is the row's whole proof. `== 1` and not `>= 1`: a leg that counts in the `ordinal` op AS WELL as in the bounds pass lands `2` and is wrong (§5.4's "counts twice"). Every other counter `0` | NEW-READS-OLD (the compiled column) and a second column on the OLD build (the identity one). LOCK-* are `enum_append`'s; OLD-REFUSES-NEW `—` |

**Row 4's three other lanes read from the same rule and are already on the corpus.** The count is the lane the
row above pins; the ranges are `range_widen_hostile` (the writer's `| 0..100`, the reader's `| 0..200`, a forged
`150` landing `100` with `clamped == 1`), the variant count is the row below, and the arm count is
`union_append_hostile` (a forged tag `3` against the writer's two arms landing `None` with `clamped == 1`).
Those three exist as reference cases today asserting `r.clamped >= 1`; **this section's number is `== 1`** and a
leg asserts the exact one.

**What `fixedform_dump.cpp` owes, so nine legs read the same bytes** (§5.9 #32's manifest, `build/fixedform-corpus/manifest.txt`):

| file | what the dump writes | the manifest line |
|---|---|---|
| `hostile_array_bounded_grow.bin` | `old_array_bounded_grow.bin`'s bytes, then the `vals_count` word OVERWRITTEN with `7` after the save | `file=hostile_array_bounded_grow.bin row=array_bounded_grow side=hostile root=ArrayBoundedGrow records=1 forged=r0.vals_count@<abs byte>=7 values=r0.lead=2863311530,r0.vals_count=4,r0.vals[0]=1000,r0.vals[1]=1001,r0.vals[2]=1002,r0.vals[3]=1003,r0.trail=3149642683` |
| `nolayout_nested_append.bin` | `old_nested_append.bin`'s bytes, then record 0's first eight bytes (its per-record hash) bitwise INVERTED; the header's hash at file+8 untouched | `file=nolayout_nested_append.bin row=nested_append side=nolayout root=Lineage records=1 forged=r0.record_hash@<abs byte>=~<hash> values=r0.v.x=…,r0.v.y=…,r0.v.z=…,r0.seq=…` |
| `hostile_enum_append.bin` | `old_enum_append.bin`'s bytes, then `r0.tier` OVERWRITTEN with `4` | `file=hostile_enum_append.bin row=enum_append side=hostile root=Lineage records=1 forged=r0.tier@<abs byte>=4 values=r0.tier=3,r0.seq=9` |
| `old_unknown_census.bin` | the OLD build, one record: `lead`, `trail`, four `items` with `a` = `10 + i` and `drop` = `900 + i` | `file=old_unknown_census.bin row=unknown_census side=old root=Census records=1 values=r0.lead=…,r0.items[0].a=10,r0.items[0].drop=900,…,r0.trail=…` |

**Two additions to #32's grammar, and nothing else moves.** `side=` gains **`hostile`** and **`nolayout`** —
a leg's parser takes the side as a word and not as one of six — and a forged file carries one more field,
**`forged=<path>@<absolute byte offset>=<wire value>`**, before `values=`, so a leg forges the same byte at the
same place without computing a record offset out of the dump. **`values=` on a forged file is what the WRITER
wrote, before the forge** — the wire's lawful values — **and the forged byte is the `forged=` field's;
the VERDICT is this page's row and never the manifest's**, which is the one place a hostile row departs from
"assert the manifest" (§5.9 #37): the manifest is the oracle for what went ONTO the wire, this table is the
oracle for what comes BACK. `manifest_case` still holds — every `.bin` in the corpus has a line and every
`root=` names a table the generator emitted.

**Row 9's corpus files carry ONE record each, deliberately.** §5.3's loop lands record `k` before it checks
record `k+1`'s hash, so in a two-record file the refusal arrives with record 0 already written and "not one
destination byte" cannot be read literally. A one-record file has no such question, and **what a multi-record
`no_layout` may leave in `out[0..k-1]` is an OPEN QUESTION for the bill, not a thing a leg should guess** —
named here rather than answered.

**Row 11's handed-in entry is the probe's, in one line, and it is the only unlawful thing in the corpus.**
§5.9 #30 withdrew the owed lawful row because no lawful lineage can move the census; the entry is therefore
handed to `GenerateLineage` directly. Under §5.8 row 1's INTERIM — the backend loading its lineage from sibling
schema files by the `VOLD_`/`VNEW_` convention — naming the two files the convention's way is all the handing-in
the row needs, because the convention never runs `BASELINE`. **The day row 1 closes and the lineage comes from
the lock, this row needs the explicit test-only entry**, and a leg says which of the two its probe used.
