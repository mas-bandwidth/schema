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
The [feature survey](docs/FIXED-TABLES-SURVEY.md) records the full scope, including
ordinary capabilities as well as the original audit families.

`✅ 100%` means the cell's required acceptance has a source-matched test receipt.
`Built; verify` means implementation was found, with specific acceptance work still
open. `Partial` means only part of the feature is implemented; `Missing` means the
current compiler refuses it. `?` remains only where audit evidence has not yet been
reconciled. Source assessments use `e3e88a46`, landed as `9785a76c`; later fixes
receive credit when their evidence is reconciled. Test references and remaining
work live inside each cell in the source data.

Language completion is green features divided by all features, not an average of
partial-cell percentages. Ordinary valid-data checks do not close the separate
hostile-input, evolution, performance, platform, compiler, lock or integration
gates. The total is a verified lower bound while audit reconciliation remains open.

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
| Boolean values | Built; verify | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% | Built; verify | ✅ 100% | ✅ 100% | Built; verify |
| Signed integers: 8, 16, 32 and 64 bits | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify |
| Unsigned integers: 8, 16, 32 and 64 bits | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify |
| Signed 128-bit integers | Built; verify | Built; verify | ✅ 100% | Built; verify | Built; verify | Built; verify | ✅ 100% | ✅ 100% | Built; verify |
| Unsigned 128-bit integers | Built; verify | Built; verify | ✅ 100% | Built; verify | Built; verify | Built; verify | ✅ 100% | ✅ 100% | Built; verify |
| Ranged integer fields | Built; verify | ✅ 100% | ✅ 100% | Built; verify | ✅ 100% | Built; verify | ✅ 100% | ✅ 100% | Built; verify |
| bits(N) fields | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | ✅ 100% | ✅ 100% | Built; verify |
| 32-bit floating-point fields | ✅ 100% | Built; verify | Built; verify | Built; verify | ✅ 100% | Built; verify | ✅ 100% | ✅ 100% | Built; verify |
| 64-bit floating-point fields | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | ✅ 100% | ✅ 100% | Built; verify |
| Compressed-float declarations stored as float32 | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | ✅ 100% | ✅ 100% | Built; verify |
| Signed fixed-point fields | Built; verify | Built; verify | ✅ 100% | Built; verify | Built; verify | Built; verify | ✅ 100% | ✅ 100% | Built; verify |
| Unsigned fixed-point fields | Built; verify | Built; verify | ✅ 100% | Built; verify | Built; verify | Built; verify | ✅ 100% | ✅ 100% | Built; verify |
| Flags masks | ✅ 100% | Built; verify | Built; verify | Built; verify | Built; verify | ✅ 100% | Built; verify | Built; verify | Built; verify |
| Enums and None | Built; verify | Built; verify | ✅ 100% | Built; verify | Built; verify | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% |
| Bounded UTF-8 string fields | ✅ 100% | ✅ 100% | ✅ 100% | Built; verify | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% |
| Bounded UTF-16 string fields | ✅ 100% | Built; verify | Built; verify | Built; verify | Missing | Missing | Missing | ✅ 100% | Missing |
| Bounded byte buffers | ✅ 100% | Built; verify | ✅ 100% | Built; verify | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% |
| Nested types by value | ✅ 100% | ✅ 100% | Built; verify | Built; verify | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% |
| Nested fixed tables by value | ✅ 100% | ✅ 100% | ✅ 100% | Built; verify | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% |
| Fixed-length arrays | ✅ 100% | ✅ 100% | ✅ 100% | Built; verify | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% |
| Bounded arrays with a live count | ✅ 100% | ✅ 100% | ✅ 100% | Built; verify | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% |
| Enum-keyed arrays | ✅ 100% | Built; verify | Built; verify | Built; verify | ✅ 100% | ✅ 100% | Built; verify | ✅ 100% | ✅ 100% |
| Nested enum-keyed arrays | Built; verify | Built; verify | Built; verify | Built; verify | ✅ 100% | ✅ 100% | Built; verify | ✅ 100% | ✅ 100% |
| Tagged unions with type or fixed-table payloads | ✅ 100% | ✅ 100% | ✅ 100% | Built; verify | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% | Built; verify |
| Union arms holding scalar, text or array fields | Partial | Partial | Partial | Partial | Partial | Partial | Partial | Partial | Partial |
| Payload-free union arms | Missing | Missing | Missing | Missing | Missing | Missing | Missing | Missing | Missing |
| Arrays of unions | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify |
| Optional scalar and enum fields | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | ✅ 100% | ✅ 100% | Built; verify | Built; verify |
| Optional nested values | Built; verify | Built; verify | ✅ 100% | Built; verify | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% |
| Optional arrays | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify |
| Scalar and enum defaults | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify |
| String, byte-buffer and flags defaults | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify | Built; verify |
| Save and load fixed-form files | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% |
| Constant body size and file-size measurement | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% | ✅ 100% | Built; verify |
| complete | ≥ 13/57 (≥ 22%) | ≥ 10/57 (≥ 17%) | ≥ 16/57 (≥ 28%) | ≥ 3/57 (≥ 5%) | ≥ 15/57 (≥ 26%) | ≥ 15/57 (≥ 26%) | ≥ 22/57 (≥ 38%) | ≥ 24/57 (≥ 42%) | ≥ 11/57 (≥ 19%) |

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
