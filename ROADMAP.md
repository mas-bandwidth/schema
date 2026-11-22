# Schema roadmap

Perhaps the defining feature of schema is that we promise complete and total feature parity across all languages. Here is how we track that.

## Packet wire

Constants, enums, flags and types. Types are bitpacked structs exchanged between client and server with exactly the same protocol version for absolute minimal overhead.

<!-- nova-work:packet-wire:start -->

| feature | cpp | c | cs | go | rust | java | js | dart | elixir | goose | luau | lua | squirrel | moo | lobster | python | unity | ruby |
|---|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|
| constants and compile-time expressions | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| enums | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| flags | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| types, bitpacked structs | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| composition of types by value | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| tagged unions of type payloads | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| named payload-free union arms | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| bool | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| full-width integers, 8 to 64 bits | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| ranged integers | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| bits(N) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| raw uint128 and ranged int128 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| raw float32 and float64 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| compressed float32 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| fixed(I, F) and ufixed(I, F) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| bytes(N) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| string(N) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| string(N) refuses malformed UTF-8 on read (#519) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| fixed arrays [N]T | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| counted arrays [A..B]T | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| if / else | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| on-wire const(Value, Bits) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| reserved(Bits) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| align | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| defaults for scalar, enum and fixed fields | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| string, bytes and flags defaults (#396) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| the protocol id | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| complete | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% |

[Source data](docs/roadmap.sexp)

<!-- nova-work:packet-wire:end -->

## Fixed Tables

Fixed tables are like types but with minimal versioning so that a new reader can always read an old writer. They are good for messages between the game server and a backend, or the backend and the website where the reader and writer have different versions of the protocol.

<!-- nova-work:fixed-tables:start -->

| feature | cpp | c | cs | go | rust | java | js | dart | elixir | goose | luau | lua | squirrel | moo | lobster | python | unity | ruby |
|---|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|
| File framing and layout announcements | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Bounded batches | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Select known layouts and refuse unsupported input | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Static plans, record sizes and caller capacity | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Layout and definition hashes | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Fixed closure and record limits | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Retirement floors and supported versions | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Remove obsolete runtime and forward-read paths | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Numeric widening and backward-read landing rules | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Optional values and absent payloads | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Renaming, appending and deprecating fields | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Exact counters and report semantics | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Array counts and writer bounds | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Text lengths, code units and named refusals | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Scalar bounds and compressed floats | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Full-width enum and union ordinals | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Bool and present-byte normalization | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Live extents, read slack and prefill | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Fixed-image writes and zeroed slack | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Nested union guards and independent metadata | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Partitioned plans and identity-path equivalence | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Shared byte oracle and round-trip conformance | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Hostile-input checks and negative controls | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Boolean values | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Signed integers: 8, 16, 32 and 64 bits | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Unsigned integers: 8, 16, 32 and 64 bits | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Signed 128-bit integers | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Unsigned 128-bit integers | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Ranged integer fields | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| bits(N) fields | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| 32-bit floating-point fields | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| 64-bit floating-point fields | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Compressed-float declarations stored as float32 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Signed fixed-point fields | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Unsigned fixed-point fields | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Flags masks | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Enums and None | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Bounded UTF-8 string fields | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Bounded byte buffers | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Nested types by value | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Nested fixed tables by value | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Fixed-length arrays | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Bounded arrays with a live count | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Enum-keyed arrays | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Nested enum-keyed arrays | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Tagged unions with type or fixed-table payloads | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Union arms holding scalar, text or array fields | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Payload-free union arms | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Arrays of unions | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Optional scalar and enum fields | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Optional nested values | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Optional arrays | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Scalar and enum defaults | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| String, byte-buffer and flags defaults | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Save and load fixed-form files | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Constant body size and file-size measurement | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| complete | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% |

[Source data](docs/roadmap.sexp)

<!-- nova-work:fixed-tables:end -->

## Variable tables

Variable tables have pointers to other tables, maps and other features that give them dynamic sizing. They are designed for complicated data structures like game assets.

<!-- nova-work:variable-tables:start -->

| feature | cpp | c | cs | go | rust | java | js | dart | elixir | goose | luau | lua | squirrel | moo | lobster | python | unity | ruby |
|---|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|
| fixed class on the table wire | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| optional fields ?T | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| enum-keyed arrays [E]T | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| text form: JSON in and out of a table | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| reflection descriptors | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| block form, read side | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cook open | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| build version | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| variable class (pointers, the flat node table) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| text form, variable class | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| block form, build side | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cook write in the runtime | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| fixed-point and 128-bit scalars on the table wire | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| unions whose arms are tables | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| arrays of pointers | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| arrays of unions | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| optional arrays | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| blobs (*bytes, *string) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| a caller's equal strings in two blob slots save by value | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| the wire fuzzer gate | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| maps | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| union arms of any field type | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| string, bytes and flags defaults, and renaming a table | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| renaming variants, arms and type fields | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| the 64-bit id-table wire, enum and escape kinds | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| unbounded arrays | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| the message form (form byte 2, a bitpacked body) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| retain-unknown | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| doc comments and tags in the descriptors | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| the unit registry, UnitView | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| widening on read, and the refusal reasons | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cards: the sprint's card record as a variable table (tables/cards) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| complete | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% |

[Source data](docs/roadmap.sexp)

<!-- nova-work:variable-tables:end -->

## Fixes

<!-- nova-work:fixes:start -->

| Fix | Kind | Languages | Branches and PRs | Rule ids and rows |
|---|---|---|---|---|
| A 4097-entry announce must refuse; it is written, or it throws instead of refusing | runtime defect | all but Python and Ruby: Go, Rust, C#/Unity, JS, Lua family, Goose, Lobster (throws in C#/Unity) | record branches under rowan/: surf-go-1122 (1396894d30), surf-rust-1122 (6f78b82f13), surf-cs-unity-1122 (02c6d60894), surf-js-1122-part (c2dc0239af), surf-lua-sq-moo-1122 (a5558627dc), surf-goose-1122 (e63790b416), surf-lobster-1122 (f2900fe404), surf-c-cpp-1122 (76fabdc525) | FL-38 |
| A duplicate map key must refuse, never be written | runtime defect | Go, C, C++, Java, Dart, Rust, Squirrel | surf-go-1122, surf-c-cpp-1122, surf-java-dart-1122 (fd91777b7d), surf-rust-1122, surf-lua-sq-moo-1122 | FL-22; rows sref_map_dupkey, sjref_map_dupkey |
| A cook writer must enforce the Min-count and refuse a count below it | runtime defect | most ports: Go, Java, Dart, Rust, Lobster, Goose, Lua family | surf-go-1122, surf-java-dart-1122, surf-rust-1122, surf-lobster-1122, surf-goose-1122, surf-lua-sq-moo-1122 | SF-29; row cref_count_below_min |
| A writer must refuse a wrong element count, in the text writer, variable save and fixed arrays over their [N] length | runtime defect | C#, Unity, Java, Dart, Rust, JS, Lua, Luau, Squirrel, Moo, Lobster | surf-cs-unity-1122, surf-java-dart-1122, surf-rust-1122, surf-js-1122-part, surf-lua-sq-moo-1122, surf-lobster-1122 | SF-34, SF-35; rows elems |
| A saved flag or mask bit above the declared width must refuse, never be kept; Dart variable save must not write ints outside the declared width | runtime defect | C#, Unity, Java, Dart, Rust, JS, Lua family | surf-cs-unity-1122, surf-java-dart-1122, surf-rust-1122, surf-js-1122-part, surf-lua-sq-moo-1122 | FL-06; row smref_mask_unnamed; SaveMessages |
| A refused read must reset the caller values and report, and a read-into must fill the caller values, never return new ones; Rust reads take no caller value | runtime defect | 14 legs; named: Java, Dart, C#/Unity, Lua family, Goose, Lobster, Python, Ruby, Rust | surf-java-dart-1122, surf-cs-unity-1122, surf-lua-sq-moo-1122, surf-goose-1122, surf-lobster-1122, surf-rust-1122, surf-python-1122-part (read-into held on intel as rowan/surf-python-1122-readinto-held), surf-ruby-1122-part (04be73fde0) | FL-02, FL-03; rows ri_fixed, ri_message, ri_variable, ri_json |
| A read of bad input must return a refusal, never raise | runtime defect | Python, Ruby | surf-python-1122-part (674112b196), surf-ruby-1122-part (04be73fde0) | FL-29 |
| fixed_load with no capacity must refuse, never default to 4096 | runtime defect | Python, Ruby | surf-python-1122-part, surf-ruby-1122-part | FL-17 |
| A block is built from a value and a refused write refuses: the builder must see the value; write-refusal-block absent (blockbuild.Begin takes counts only) | runtime defect | absent in Go, Java, Dart, JS, Python, Ruby; builder never sees the value in C#/Unity, Lobster, Goose, Lua family | surf-go-1122, surf-java-dart-1122, surf-js-1122-part, surf-python-1122-part, surf-ruby-1122-part, surf-cs-unity-1122, surf-lobster-1122, surf-goose-1122, surf-lua-sq-moo-1122 | SF-31; rows write-refusal-block; bref_length and bref_range are written elsewhere |
| The Big message writer must be emitted and its smref_big rows must pass | runtime defect | Rust, Goose (7 rows absent); also absent in Java, Dart, JS; C and C++ wait on PR 4744 | surf-rust-1122, surf-goose-1122, surf-java-dart-1122, surf-js-1122-part; PR 4744 | SF-05, SF-06; rows smref_big_* |
| The JSON reader must accept a 128-deep chain, never refuse it as malformed | runtime defect | Python, Ruby | surf-python-1122-part, surf-ruby-1122-part | row surf_chain128_vals |
| A write that cannot be done must refuse with a named reason, never crash on an index error or throw | runtime defect | Lobster (index error on some writes), C#/Unity (announce throws) | surf-lobster-1122, surf-cs-unity-1122 | see FL-38 |
| The announcement string must be handled whole: JS must not refuse its own announcement in strict mode, the Lua parser must not overflow on it | runtime defect | JS, Lua | surf-js-1122-part, surf-lua-sq-moo-1122 | none named |
| A text writer must not write a NUL past its text | runtime defect | C++ (the only port reported) | surf-c-cpp-1122 | FL-15 |
| write-refusal-json rows 43 and 44, and surf_holder_null_required on the wire and json-read legs, must pass | runtime defect | Goose (the only port reported) | surf-goose-1122 (e63790b416) | rows write-refusal-json 43/44, surf_holder_null_required |
| Generated code for a 4096 or 4097-entry announce must compile: Java says code too large, Dart says too many nested expressions | generator defect | Java, Dart | surf-java-dart-1122 | none named |
| Land the spec streams: PR 4501 (36 merged fixes; the R14, R15, R16 child is stacked on it), 4636 (corpus stream into review-1121), 4706 fix ledger (with 4751, 4752, 4754, 4757 into it), 4740 silent-failure audit (with 4743, 4744, 4753, 4771, 4772 into it; SF rows for 11 rules), 4750 spec index (109 rules: 18 done on main, 52 in PRs, 39 lack a row) | open PRs to land | all ports (spec) | PRs 4501, 4636, 4706, 4751, 4752, 4754, 4757, 4740, 4743, 4744, 4753, 4771, 4772, 4750 | spec index rules |
| Land the R14 to R17 stream and make its legs green: PR 4764 (R14, R15, R16 into review-1121; red legs: c report stops at the old row varevo_v1_as_v2, js message-prior stops at the old row batch_range_in_second, goose message-prior fails prior_scalar_inside, likely the 5 new Prior fields; message-prior absent on C#, Dart, Elixir, Java, Rust), then 4769 (R17: the baseline passes every widening; int32 to int64 and the other widenings are done there) and 4773 (R17: remove the old-reads-new machinery, escape kind 31 refused, flag bits past W refused; head db13fd3984, rowan/old-reads-new on rowan/r17-widening) | open PRs to land | C, JS, Goose red; C#, Dart, Elixir, Java, Rust absent on message-prior; all 18 targets for 4773 | PRs 4764 (head a4de9af3c3), 4769, 4773 | R14, R15, R16, R17; rows r17_flags_past_width, r17_fixed_flags_past_width, prior_flags_past, escape_* |
| Land the conformance and rulings PRs: 4732 (a must-refuse row is credited only beside an answered lawful twin), 4733, 4721 (driver stdout is protocol), 4700 (dead fixed-form rows and the C++ tables binary run in the PR lane), 4688 (spec rows 1122), 4689 (draft; roots for counted minimum, float32 bound, deprecated enum element), 4690 (R4: every value handed in holds its default after a read refusal, 18 ports), 4680 (rulings R2, R5, R9), 4712 (R10), 4685 (R8 follow-up: C#/Unity retain writers keep the 4096-id budget), 4667 and 4658 (write-refusal surfaces and negative controls), 4648 (luau wire fuzz drives the message form), 4598 (luau packet finite gate), 4691 (check refuses every name some target cannot build) | open PRs to land | all ports; named: C, C++, C#, Unity, Rust, Java, Dart, Python, Ruby, Luau, Squirrel | PRs 4732, 4733, 4721, 4700, 4688, 4689, 4690, 4680, 4712, 4685, 4667, 4658, 4648, 4598, 4691 | R2, R4, R5, R8, R9, R10; SPEC 4.6 |
| Land the CI and test-gate stream: 4763 (input accounting: every reader of a changed input runs, accounted and canaried), 4766 (nightly-only gates get fast forms; the Ruby nightly may go red), 4758 (SKIPPED-SLOW is red; DIRTY), 4580 (ci-fast runs each leg gates), 4571 (nightly-red announced on one issue), 4530 and 4589 (lang-clean c and elixir as shards; 4589 into elixir-ci-fast), 4520 (every Go test assigned to a lane), 4741 (recipescan), 4739 (bench-check), 4738 (nothing fails silently in tests, cmd, tools), 4742 (goldens rewrite on the PR lane), 4734 (pre-push hook tests), 4747 (strict c11 build of the retain bridge) | open PRs to land | all ports (CI); C, Elixir (lang-clean shards) | PRs 4763, 4766, 4758, 4580, 4571, 4530, 4589, 4520, 4741, 4739, 4738, 4742, 4734, 4747 | none |
| Land the port fix PRs: 4756 (Dart text read resets labelsSeen and labelsOpen), 4762 (Goose carries the three no-root arms), 4765 (wire-fuzz refuses vectors outside the index; packet-finite-squirrel), 4767 (C++ retain trailer bounded by id_table_max), 4651 (C++ -0 at any token length, no NUL past ToJson), 4748 (packet-defaults pins), 4749 (three red checks), and the rest of queue2; also 4550 (kill the surviving JS and Lobster mutants) and 4531 (draft: tools/mutation) | open PRs to land | Dart, Goose, Squirrel, C++, JS, Lobster and others (per PR) | PRs 4756, 4762, 4765, 4767, 4651, 4748, 4749, 4550, 4531, queue2 | none |
| Land v3.0.2 parity (PR 4450: every target in the default build, Python packet and table fixes, every-target gates and docs) and the /v3 module path (PR 4445), then cut the final release | open PRs to land | all ports | PRs 4450, 4445 | none |
| PR 4763 will be red until fixed: the C driver never opens the 2 new packet-defaults pins (4748), the C packet-end driver opens no .bits file, C packet-write-text never reads Text.schema (the wrong-answer canary stayed green); the conformance-corpus row has never run; hostile corpus accounting is owed (no PR-tier row) | finding on an open PR | C (drivers); all ports (accounting) | PR 4763 (head 2a70a82), PR 4748 | packet-defaults pins, packet-end, packet-write-text, hostile corpus |
| A variable save must not write bytes(N) past N in a bytes(4) arm, and a union arm that holds an array must let the driver set a count | runtime defect | Java (SF-09, SF-13 bytes(4) arm), Squirrel (SF-13 arm array: the driver cannot set a count) | none yet; shapes in PR 4771 | SF-09, SF-13 |
| A fixed save must refuse a count below Min | runtime defect | C, C++ | PR 4771 shapes; no fix branch | SF-21 |
| A writer must not write a deprecated variant | runtime defect | C, C++, Java, JS, Elixir | PR 4771 shapes; no fix branch | SF-26 |
| write-refusal-packet must learn the ratio op for compressed float32 rows | runtime defect | C, C++, C#, Unity, Rust, Java, Dart (the PR 4658 surface) | PRs 4658, 4772 | SF-08 |
| SF rows 17, 18, 19, 20 and 25 are not written yet; they ride the surfaces base branch | surfaces work | all ports | rowan/announce-surfaces-1122 | SF-17, SF-18, SF-19, SF-20, SF-25 |
| Red tests and lint seen on the stream base a4de9af3c3 (before the widening and removal PRs): Goose message-prior and write-refusal, TestGooseMessageFormAnnounceRead, TestGooseTableWireMatchesTheCorpus, TestContext7ScansOnlyTheMainDocs, TestLuaWireCheckNegativeControl, Ruby rubocop at cook.rb:884, TestCppVariableWriterEvolution (3 subtests), TestPythonVtMapsRows (lacks the R14 and R16 rows and now the two R17 rows); make test-run does not exist on that base | finding without a PR | Goose, Lua, Ruby, C++, Python (and the Go test tree) | none | see the tests named |
| One spec-only PR lands the nine owed wording edits of the old-reads-new audit (R17): VERSIONING promise 1 and lines 119-121, 247-249, the never-clobber rule (also SPEC-TABLES 6.6 and COMPETITION), Debug an old file, Optional is a widening; SPEC-TABLES 873-876 and 9357-9359; USAGE 199-202. Each carries a SPEC-CONFORMANCE note: no row, wording only. Then each code PR links up in the owed-code table | spec wording | all ports (spec text; behaviour unchanged) | one spec-only PR not yet opened; code PRs 4769, 4773 | R17; audit finds 3 to 11 |
| NOVA-TOOLS ROADMAP MATERIAL, NOT SCHEMA: a rented machine must be returned automatically, or raise an alarm, when its work is done; the auto-return or alarm is owed in nova-tools | nova-tools roadmap | none (nova-tools) | none; belongs in the nova-tools roadmap | none |
| The --retain wire fuzz must agree on id_table_4096 (graphdemo.Scene); C++ seed 1 diverges | finding without a PR | C++ (the only port reported) | none; a child was working on it | seed 1, id_table_4096 |
| schema-lua-version-differential must be green on main (red in run 44994938828) | finding without a PR | Lua | held in PR 4766 | schema-lua-version-differential |
| schema-moo-runs-hostile-corpus.yml must complete a run; it never has | finding without a PR | Moo | none | schema-moo-runs-hostile-corpus |
| make deps leaves a stale ~/w/serialize sibling alone; amd-3 has none | finding without a PR | none (tooling) | none | make deps |
| Top-level testdata/wire pins: the readers are hand-typed per driver and must be derived | finding without a PR | all ports (per driver) | none | testdata/wire |
| Hostile corpus per-port runs happen on push to main and nightly only; they must run on the PR | finding without a PR | all ports | none | hostile corpus |
| The C tablegen driver builds one main.c: the same slow single-core compile PR 4755 fixed for C++ | finding without a PR | C (C++ fixed by PR 4755) | PR 4755 for C++ | none |
| The pointer-array depth fix in PR 4754 must be isolated: array check versus keyed-slot change | finding without a PR | all ports (spec fix) | PR 4754 | none |
| Spec: the open brace after a pipe rule waits for a ruling | finding without a PR | all ports (spec) | none | none |
| Make the surfaces base branch green: the Go engine write-refusal walk lacks the new kinds, caseRows has no roadmap row for cook and block, the Damaged field name is retired, wire dash rows trip the current-form test, TestRustDriverAnswersWriteRefusal needs surf-rust merged, TestSharedFixturesCanonical fails on framing files (check main) | surfaces work | all ports (harness) | rowan/announce-surfaces-1122 (f358c44a1d): 5 new surfaces, new write-refusal kinds, 3 units, goldens, docs; no PR | TestRustDriverAnswersWriteRefusal, TestSharedFixturesCanonical |
| Merge the leg branches into the surfaces base; C++ json-write-into was 0 of 5 on the last full run | surfaces work | all ports | rowan/surf-c-cpp-1122 (76fabdc525), rowan/surf-ex-goose-lob-1122 (72fd9bdecd), and the section A branches into rowan/announce-surfaces-1122 | json-write-into |
| SF-27 text-form vector: extend the text-form contract, then add the vector | surfaces work | all ports (spec) | rowan/announce-surfaces-1122 | SF-27 |
| Negative controls owed on the new surfaces | surfaces work | C, C++, Elixir, Goose, Lobster | rowan/surf-c-cpp-1122, rowan/surf-ex-goose-lob-1122, rowan/surf-goose-1122, rowan/surf-lobster-1122 | none |

[Source data](docs/roadmap.sexp)

<!-- nova-work:fixes:end -->

## Fund this work

Schema is built by Glenn Fiedler, together with AI collaborators that do much of the building, testing and porting. Glenn owns every design decision. Every month a [public ledger](https://github.com/mas-bandwidth/patreon#public-ledgers) shows where the AI collaborator's tokens went, by repository, and what they bought.

If you'd like to support this work, you can **[become a supporter](https://www.patreon.com/MasBandwidth/membership)**
