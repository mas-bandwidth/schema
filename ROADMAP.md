# Schema roadmap

### Packet wire

| feature | cpp | c | cs | go | rust | java | js | dart | elixir | swift | ts | lua | clojure | python | ruby | kotlin | gdscript | zig | odin | haxe |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| constants and compile-time expressions | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| enums | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| flags | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| types, bitpacked structs | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| composition of types by value | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| tagged unions of type payloads | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| named payload-free union arms | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| bool | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| full-width integers, 8 to 64 bits | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ranged integers | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| bits(N) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| raw uint128 and ranged int128 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| raw float32 and float64 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| compressed float32 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| fixed(I, F) and ufixed(I, F) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| bytes(N) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| string(N) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| string(N) refuses malformed UTF-8 on read (#519) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| wstring(N) (#188) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| fixed arrays [N]T | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| counted arrays [A..B]T | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| if / else | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| on-wire const(Value, Bits) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| reserved(Bits) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| align | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| defaults for scalar, enum and fixed fields | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| string, bytes and flags defaults (#396) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| the protocol id | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

### NEW Fixed Tables

The active work is [#898](https://github.com/mas-bandwidth/schema/issues/898),
on [`fixed-table-form`](https://github.com/mas-bandwidth/schema/tree/fixed-table-form)
([integration PR #836](https://github.com/mas-bandwidth/schema/pull/836)).
It is not yet released on `main`.

This matrix is generated from [recursive work data](docs/roadmap.sexp).
The [feature survey](docs/FIXED-TABLES-SURVEY.md) records the scope correction:
ordinary capabilities are appended to the original audit families. Current evidence
reconciliation is unfinished; this is not yet a certified implementation percentage.
Each feature/language cell contains required subtasks. A cell is green only when
all are verified; language completion is green features divided by total features,
not an average of partial-cell percentages. `?` means current evidence remains
unreconciled, not that implementation is absent. Any numeric lower bound counts
only verified work. Shared compiler, lock and final integration gates remain
required outside the per-language percentages; passing CI alone does not close them.

<!-- nova-work:fixed-tables:start -->

| feature | cpp | c | cs | go | rust | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|---|
| File framing and layout announcements | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Bounded batches | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Select known layouts and refuse unsupported input | ? (≥ 14%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Static plans, record sizes and caller capacity | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Layout and definition hashes | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Fixed closure and record limits | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Retirement floors and supported versions | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Remove obsolete runtime and forward-read paths | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Numeric widening and backward-read landing rules | ? (≥ 16%) | ? (≥ 16%) | ? (≥ 16%) | ? (≥ 16%) | ? (≥ 16%) | ? (≥ 16%) | ? (≥ 16%) | ? (≥ 16%) | ? (≥ 16%) |
| Optional values and absent payloads | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Renaming, appending and deprecating fields | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Exact counters and report semantics | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Array counts and writer bounds | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Text lengths, code units and named refusals | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Scalar bounds and compressed floats | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Full-width enum and union ordinals | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Bool and present-byte normalization | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Live extents, read slack and prefill | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Fixed-image writes and zeroed slack | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Nested union guards and independent metadata | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Partitioned plans and identity-path equivalence | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Shared byte oracle and round-trip conformance | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Hostile-input checks and negative controls | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Boolean values | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Signed integers: 8, 16, 32 and 64 bits | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Unsigned integers: 8, 16, 32 and 64 bits | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Signed 128-bit integers | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Unsigned 128-bit integers | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Ranged integer fields | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| bits(N) fields | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| 32-bit floating-point fields | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| 64-bit floating-point fields | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Compressed-float declarations stored as float32 | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Signed fixed-point fields | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Unsigned fixed-point fields | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Flags masks | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Enums and None | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Bounded UTF-8 string fields | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Bounded UTF-16 string fields | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | 0% | 0% | 0% | ? (≥ 0%) | 0% |
| Bounded byte buffers | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Nested types by value | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Nested fixed tables by value | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Fixed-length arrays | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Bounded arrays with a live count | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Enum-keyed arrays | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Nested enum-keyed arrays | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Tagged unions with type or fixed-table payloads | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Union arms holding scalar, text or array fields | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Payload-free union arms | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Arrays of unions | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Optional scalar and enum fields | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Optional nested values | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Optional arrays | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Scalar and enum defaults | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| String, byte-buffer and flags defaults | ? (≥ 50%) | ? (≥ 50%) | ? (≥ 50%) | ? (≥ 50%) | ? (≥ 50%) | ? (≥ 50%) | ? (≥ 50%) | ? (≥ 50%) | ? (≥ 50%) |
| Save and load fixed-form files | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| Constant body size and file-size measurement | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) | ? (≥ 0%) |
| complete | ≥ 0/57 (≥ 0%) | ≥ 0/57 (≥ 0%) | ≥ 0/57 (≥ 0%) | ≥ 0/57 (≥ 0%) | ≥ 0/57 (≥ 0%) | ≥ 0/57 (≥ 0%) | ≥ 0/57 (≥ 0%) | ≥ 0/57 (≥ 0%) | ≥ 0/57 (≥ 0%) |

[Source data](docs/roadmap.sexp)

<!-- nova-work:fixed-tables:end -->

### Future

These capabilities are outside the active NEW Fixed Tables work set. Existing
marks below describe the earlier table implementation; they do not certify the
new fixed form. Save games is an additional future product feature.

| feature | cpp | c | cs | go | rust | java | js | dart | elixir | swift | ts | lua | clojure | python | ruby | kotlin | gdscript | zig | odin | haxe |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| fixed class on the table wire | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| optional fields ?T | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| enum-keyed arrays [E]T | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| text form: JSON in and out of a table | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| reflection descriptors | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| block form, read side | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| cook open | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| build version | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| variable class (pointers, the flat node table) | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| text form, variable class | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| block form, build side | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| cook write in the runtime | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| fixed-point and 128-bit scalars on the table wire | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| unions whose arms are tables | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| arrays of pointers | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| arrays of unions | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| optional arrays | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| blobs (*bytes, *string) | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| the wire fuzzer gate | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| maps | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| union arms of any field type | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| string, bytes and flags defaults, and renaming a table | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| renaming variants, arms and type fields | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| the 64-bit id-table wire, enum and escape kinds | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| wstring(N) on the table wire (#522) | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| unbounded arrays | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| the message form (form byte 2, a bitpacked body) | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| retain-unknown | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| doc comments and tags in the descriptors | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| the unit registry, UnitView | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| widening on read, and the refusal reasons | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| save games | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

## How the work is done

Schema is built by Glenn Fiedler, together with AI collaborators that do much of 
the building, testing and porting. Glenn owns every design decision. Every month a
[public ledger](https://github.com/mas-bandwidth/patreon#public-ledgers) shows
where the AI collaborator's tokens went, by repository, and what they bought.

## Fund this work

If you write games in more than one language, this is being built for you. If
you have ever kept two schema systems in step by hand, or shipped a client and
a server that disagreed about one field, this is the fix we are building.

Your support pays for the tokens the AI collaborator runs on and the machines
the benchmarks run on, and the ledger shows you where every one of them went.

**[Become a supporter](https://www.patreon.com/MasBandwidth/membership)**
