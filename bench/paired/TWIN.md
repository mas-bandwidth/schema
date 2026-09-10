# C / C++ fixed-form twin map

`make tables-fixed-twin` emits both paired Fixed Table headers, normalises
the fixed-form runtimes through the token map below, and `diff`s what is
left. Any leftover line that is not one of the three named differences is
a failure.

The C runtime is the twin of the C++ runtime: same wire, same ops, same
partition, same refusals, same counters. Glenn: the two should agree on
100%. The map is the spelling. The three named differences are the C form
of one C++ mechanism each, not a second algorithm.

## What is compared

From `generated/bench/paired/{c,cpp}/FixedTableTable.h`:

1. The package-scoped fixed runtime (`tableFixedRuntime` /
   `tableFixedRuntime128` in `internal/codegen/{ctable,cpptable}/fixedruntime.go`).
2. The identity plan, the layout bytes, and the plan count / guarded split —
   the numbers the schema compiler laid down, which no port gets to recompute.

Write / clamp / save / load bodies ride the same map so a store offset or a
`.as.` arm that drifted is a leftover line.

## Token map (C → canonical ← C++)

Applied before the diff. Longest match first. These are spelling, not
behaviour.

| C | C++ | canonical |
|---|---|---|
| `static SCHEMA_UNUSED` | `inline` | `inline` |
| `SCHEMA_BENCH_TABLE_INLINE` | `TABLE_FIXED_INLINE` | `inline` |
| `table_fixed_foo_bar` | `TableFixedFooBar` | `TableFixedFooBar` |
| `table_fixed_putf32` / `putf64` | `TableFixedPutF32` / `PutF64` | `TableFixedPutF32` / `PutF64` |
| `SCHEMA_TABLE_FIXED_NO_GUARD` | `kTableFixedNoGuard` | `kTableFixedNoGuard` |
| `SCHEMA_TABLE_RESTRICT` | `TABLE_RESTRICT` | `TABLE_RESTRICT` |
| `SCHEMA_TABLE_LAYOUT_*` / `SCHEMA_TABLE_NO_LAYOUT` / … | `layout_*` / `no_layout` / … | the C++ enumerator |
| `enum { kName = N }` / combined enumerators | `constexpr T kName = N` | `kName = N` |
| `enum : uint8_t { … }` | `enum { … }` | enumerators |
| `typedef struct T { … } T` | `struct T { … }` | `struct T` |
| default-member-initializer-less fields + `table_fixed_*_zero` | `T x = 0` / `= kTableFixedNoGuard` | fields without defaults; drop the C zeroing helpers |
| `int` 0/1 predicates (`want_guarded`, `overflow`, `hostile`, `named`, `bad`, `is_leaf`, `kids_are_variants`) | `bool` / `true` / `false` | `bool` |
| `int why` / `c.reason` | `TableMessageReason why` / `c.why` | `c.why` |
| `ptr->field` | `ref.field` | `.` |
| `int32_t * clamped` / `(*clamped)++` | `int32_t & clamped` / `clamped++` | reference spelling |
| `const T * p` in the runtime | `const T & p` | `T REF` |
| `int i; for ( i =` | `for ( int i =` | C++ for-init |
| `remap` (ordinal table) | `table` | `table` |
| `wide` (f32→f64 local) | `d` | `d` |
| `slots` (lay-table pointer) | `dst` | `dst` |
| `.as.arm` | `.arm` | `.arm` (see named difference 3) |
| `offsetof` | `__builtin_offsetof` | `__builtin_offsetof` |
| `SCHEMA_TABLE_STATIC_ASSERT(tag, cond, msg)` | `static_assert(cond, msg)` | `static_assert` |
| `/* … */` | `// …` | stripped |
| trailing commas in enumerators | trailing commas | dropped |
| C restates the refusal names inside the runtime (`SCHEMA_TABLE_MESSAGE_REASONS` and the form-3 reason enum) because it has no `TableMessageReason` in scope yet | those names live above the runtime | dropped from the C runtime extract |
| restrict / force-inline feature tests | restrict / force-inline feature tests | dropped; the names they define are mapped |

A line the map rewrites to equality is not a remaining difference.

## Named remaining differences

After the map, only these may remain. Each leftover line must belong to
one of them. Anything else reds by line.

1. **RAII guard vs inc/dec.** C++ spends `struct TableFixedDepth` on the
   compile-entry recursion cap. C has no destructor, so the same cap is
   `c->depth++; do { … } while (0); c->depth--;`, and the kind-mismatch
   early exits are `break` out of the `do` rather than `return`.
2. **The two 128-bit store functions.** C++ overloads `TableFixedPut128`
   on `serialize::int128_t` / `uint128_t`. C has neither overloading nor a
   builtin 128-bit integer, so the same two stores are
   `table_fixed_put128_i` / `table_fixed_put128_u` over serialize.h's
   lo/hi pair. C also spells the bounds compare as `table_fixed_cmp128_*`
   because those types are structs; C++ uses `<` / `>`.
3. **snake_case behind `SCHEMA_UNUSED`, the `as.` union prefix.** Every
   C runtime function is `static SCHEMA_UNUSED table_fixed_…` because that
   is how the rest of the C backend reads; C++ is `inline TableFixed…`. A
   union arm in C storage sits behind `as` (`offsetof(MixedEvent, as)`,
   `value->game_event.as.hit`); C++ uses an anonymous union
   (`offsetof(MixedEvent, hit)`, `value.game_event.hit`). Same bytes.

## Paired rows

The paired runner accepts `--twin-tolerance` (default 10). A C row and a
C++ row of the same wire and path that sit within that percent do not fail
the twin check. Wider than that is a leftover, the same way a leftover
line is. It does not invent a wire and it does not change bytes.
