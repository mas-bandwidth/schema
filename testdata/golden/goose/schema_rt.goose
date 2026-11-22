// schema_rt.goose — the Goose port's shared runtime (docs/PORT-GOOSE.md, THE
// RUNTIME LAYOUT): emitted beside every generated module and imported as
// `import .schema_rt;`. It declares into its own namespace, schema_rt, so no
// schema declaration can collide with it.
//
// SPDX-License-Identifier: NONE — emitted into your output; the schema
// compiler is AGPL-3.0, its output is not.
namespace schema_rt;

// Int128 is every 128-bit value of the port (docs/PORT-GOOSE.md, THE NUMBER
// MODEL): Goose has no 128-bit integer, so the value is two u64 lanes, the
// low lane first as the wire orders it. Signedness is the field's, not the
// value's: a signed value is its two's-complement bits.
struct Int128 { lo: u64, hi: u64 }

// int128 builds an Int128 from its lanes.
fn int128(lo: u64, hi: u64) -> Int128 { Int128 { lo: lo, hi: hi } }
