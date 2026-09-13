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

Cells use three states: **empty — missing/not started**, **↻ — in progress or
partial**, **✅ — verified**. Implementation that is built but still needs checks is
in progress. Verification and evidence reconciliation are also unfinished work;
nothing is complete until its required acceptance is verified.

Source assessments use `e3e88a46`, landed as `9785a76c`; later fixes receive credit
when their evidence is reconciled. Detailed implementation findings, test
references, remaining work and subtask counts live inside each cell in the source
data.

Language completion is green features divided by all features, not an average of
partial-cell percentages. Ordinary valid-data checks do not close the separate
hostile-input, evolution, performance, platform, compiler, lock or integration
gates. The total is a verified lower bound while audit reconciliation remains open.

<!-- nova-work:fixed-tables:start -->

| feature | cpp | c | cs | go | rust | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|---|
| File framing and layout announcements | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Bounded batches | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Select known layouts and refuse unsupported input | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Static plans, record sizes and caller capacity | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Layout and definition hashes | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Fixed closure and record limits | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Retirement floors and supported versions | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Remove obsolete runtime and forward-read paths | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Numeric widening and backward-read landing rules | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Optional values and absent payloads | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Renaming, appending and deprecating fields | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Exact counters and report semantics | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Array counts and writer bounds | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Text lengths, code units and named refusals | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Scalar bounds and compressed floats | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Full-width enum and union ordinals | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Bool and present-byte normalization | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Live extents, read slack and prefill | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Fixed-image writes and zeroed slack | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Nested union guards and independent metadata | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Partitioned plans and identity-path equivalence | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Shared byte oracle and round-trip conformance | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Hostile-input checks and negative controls | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Boolean values | ↻ | ✅ | ✅ | ✅ | ✅ | ↻ | ✅ | ✅ | ↻ |
| Signed integers: 8, 16, 32 and 64 bits | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Unsigned integers: 8, 16, 32 and 64 bits | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Signed 128-bit integers | ↻ | ↻ | ✅ | ↻ | ↻ | ↻ | ✅ | ✅ | ↻ |
| Unsigned 128-bit integers | ↻ | ↻ | ✅ | ↻ | ↻ | ↻ | ✅ | ✅ | ↻ |
| Ranged integer fields | ↻ | ✅ | ✅ | ↻ | ✅ | ↻ | ✅ | ✅ | ↻ |
| bits(N) fields | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ✅ | ✅ | ↻ |
| 32-bit floating-point fields | ✅ | ↻ | ↻ | ↻ | ✅ | ↻ | ✅ | ✅ | ↻ |
| 64-bit floating-point fields | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ✅ | ✅ | ↻ |
| Compressed-float declarations stored as float32 | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ✅ | ✅ | ↻ |
| Signed fixed-point fields | ↻ | ↻ | ✅ | ↻ | ↻ | ↻ | ✅ | ✅ | ↻ |
| Unsigned fixed-point fields | ↻ | ↻ | ✅ | ↻ | ↻ | ↻ | ✅ | ✅ | ↻ |
| Flags masks | ✅ | ↻ | ↻ | ↻ | ↻ | ✅ | ↻ | ↻ | ↻ |
| Enums and None | ↻ | ↻ | ✅ | ↻ | ↻ | ✅ | ✅ | ✅ | ✅ |
| Bounded UTF-8 string fields | ✅ | ✅ | ✅ | ↻ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Bounded UTF-16 string fields | ✅ | ↻ | ↻ | ↻ |  |  |  | ✅ |  |
| Bounded byte buffers | ✅ | ↻ | ✅ | ↻ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Nested types by value | ✅ | ✅ | ↻ | ↻ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Nested fixed tables by value | ✅ | ✅ | ✅ | ↻ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Fixed-length arrays | ✅ | ✅ | ✅ | ↻ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Bounded arrays with a live count | ✅ | ✅ | ✅ | ↻ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Enum-keyed arrays | ✅ | ↻ | ↻ | ↻ | ✅ | ✅ | ↻ | ✅ | ✅ |
| Nested enum-keyed arrays | ↻ | ↻ | ↻ | ↻ | ✅ | ✅ | ↻ | ✅ | ✅ |
| Tagged unions with type or fixed-table payloads | ✅ | ✅ | ✅ | ↻ | ✅ | ✅ | ✅ | ✅ | ↻ |
| Union arms holding scalar, text or array fields | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Payload-free union arms |  |  |  |  |  |  |  |  |  |
| Arrays of unions | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Optional scalar and enum fields | ↻ | ↻ | ↻ | ↻ | ↻ | ✅ | ✅ | ↻ | ↻ |
| Optional nested values | ↻ | ↻ | ✅ | ↻ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Optional arrays | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Scalar and enum defaults | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| String, byte-buffer and flags defaults | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ | ↻ |
| Save and load fixed-form files | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Constant body size and file-size measurement | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ↻ |
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
