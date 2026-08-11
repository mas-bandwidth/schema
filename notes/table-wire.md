# The table wire — evolution-tolerant encoding for `type` declarations

*2026-08-12. Glenn's requirement, verbatim: "If a new property is added or a
property is removed, as much as possible, we want the backend and the server
to be able to still read the more recent Config.bin, and work without
crashing and be as safe and permissive and as correct as is possible" —
"these tables for config and assets need a versioning strategy, like
flatbuffers has (or better)." This is the design, offered for his
correction; the pack tool and the C++/C# table codecs implement it.*

## Two wires, one language — the split that resolves the philosophy tension

- **Messages** (realtime): the dense bitpacked wire, exact-match protocol id,
  no evolution machinery — unchanged. Client and server ship together.
- **Tables** (`type` declarations — config, assets, events, settings): the
  TLV wire below. Data outlives builds (live config pushes reach running
  servers), so absence tolerance and unknown-field skipping are the
  requirements, and bit density is not.

## Field identity: name-hash ids — the "or better"

`field_id = fold16(fnv1a32(field_name))`, where `fold16(h) = (h ^ (h >> 16))
& 0xFFFF`, and an id of 0 (the terminator) rebounds to 1. Properties:

- **Order-free**: reordering declarations changes nothing on the wire.
- **Removal-free**: deleting a field needs no tombstone — its id simply stops
  being written; old readers default it, new readers skip it in old data.
- **No authoring discipline**: nothing to remember — flatbuffers' append-only
  convention and `deprecated` slots both exist to protect positional ids;
  name ids need neither.
- **Collision-refused**: the compiler and the pack tool verify id uniqueness
  per type and REFUSE on collision (rename one field; at table sizes a
  collision is astronomically rare, and the refusal is loud and at build
  time).
- **A rename is remove + add** — old data's value defaults under the new
  name. Same as flatbuffers' JSON reality; documented, not hidden.

## Encoding

All little-endian, byte-aligned. A TABLE VALUE is:

    table  := field* end
    field  := id:u16  kind:u8  payload
    end    := id 0x0000

Payload by kind:

    1  bool    1 byte (0/1)          8  u32    4 bytes
    2  i8      1 byte                9  u64    8 bytes
    3  i16     2 bytes              10  f32    4 bytes
    4  i32     4 bytes              11  f64    8 bytes
    5  i64     8 bytes              12  string u16 len + bytes
    6  u8      1 byte               13  table  u32 len + nested table value
    7  u16     2 bytes              14  array  u32 len + array value

    array value := elem_kind:u8  count:u16  elements
      fixed-kind elements   back to back
      string elements       each u16 len + bytes
      table elements        each u32 len + nested table value

Mappings from schema types: bounded and bare ints use their STORAGE width's
kind (the declared range still validates at pack/read); enums use their
storage width's unsigned kind; flags are u64; `string(N)` is kind 12;
`bytes(N)` is kind 14 of u8; every NAMED type field is kind 13 — nested
tables all the way down, so evolution reaches composites too (flatbuffers
structs are frozen; here nothing is, which is the second "or better").
Branch guards encode as ordinary bool fields and their guarded fields simply
appear or don't — TLV's native optionality carries the branch.

## Read semantics — the permissive contract

1. Reader pre-fills the output with DECLARED DEFAULTS (the generated
   initializers), then overlays fields present on the wire.
2. Unknown id: skip by kind/len. (A new field reaching an old reader.)
3. Known id, WRONG kind: skip by kind/len, count it — a changed type never
   misdecodes. (Kind acts as a type fingerprint.)
4. Known id and kind: decode; declared ranges clamp-and-count rather than
   reject (data must not crash a server), string/array overflow truncates
   to the declared bound and counts.
5. Malformed framing (lengths past the buffer): stop decoding THAT value,
   keep what decoded, report failure to the caller — the caller decides
   (a config push validates before install; a load-at-boot fatals).
6. Every reader returns a small report: fields_unknown, kind_mismatches,
   values_clamped — silence only when the data is exactly at the reader's
   schema.

## The container

SBN1 unchanged (magic, unit protocol id, fnv64a content hash, per-collection
u32 count + u32-length-prefixed instances) with ONE semantic change for
table bins: a protocol id mismatch is a WARNING, not a rejection — the TLV
wire carries its own compatibility. The content hash still gates corruption
absolutely.

## Decisions banked during implementation (2026-08-12)

- Branch guards are honored by every WRITER: both writers walk the wire tree
  (the dense encoder's own Items walk), so untaken-branch fields stay off
  the wire entirely and the reader's prefilled defaults stand in for the
  untaken side. Readers do not enforce guard consistency — consumers consult
  the guard bool. This is the exact mechanism the space gunner_settings
  presence seam rides (fbs null table ↔ guard false).
- Fixed arrays are POSITIONAL: writers pad to the declared bound (absent
  trailing JSON elements encode as the element default) and elide the field
  entirely when every element is at the default. Fixed arrays of tables
  always ride (no cheap element compare in C++; an all-default element costs
  6 bytes). Counted arrays elide only at count zero — a count of N default
  elements is data.
- bytes(N) travels as an array of u8: writers say elem-kind u8, readers
  expect exactly that.
- Types the wire cannot carry — int128/uint128, fixed(I, F), bits wider
  than 64 — get NO table functions in C++ (a comment names the reason,
  transitively through nesting); the Go backend refuses such units outright
  (no 128-bit surface); `schema pack` errors per-field.
- Read-side clamps, complete: declared int ranges, declared float ranges,
  bits(W) width overflow (a wider wire kind carrying more than W bits),
  string/array bounds, enum out-of-set -> None. All count in the report.
- The bare terminator: an instance at its declared defaults encodes as
  2 bytes, in every writer.

## What implements it

- `schema pack` (Go): TLV writer per the manifest — DONE, guard-honoring.
  The dense interpreter and its wire-golden test remain (the realtime
  wire's independent check).
- Go TLV decoder (internal/pack): the evolution TEST harness — encode under
  schema A, decode under edited schema B, assert defaults/skips/counts.
  DONE (internal/pack/table_test.go: evolution both directions, guards,
  bytes, clamps).
- C++ emitter: `<Base>Table.h` — TableWrite/TableRead per `type`, no
  serialize dependency (plain byte code), includable anywhere. DONE; corpus
  compiles under -Wall -Wextra -Werror, ODR-proven across two TUs.
- Go emitter: `<Base>Table.go` + TableRuntime.go — TableWriteX(value)
  []byte / TableReadX(data, &value, &report) bool, typed codecs in the
  generated package. DONE. This is what lets the Go backend
  (cmd/backend/backend.go in space) open the SAME Config.bin/Assets.bin the
  game server reads — one file, native readers in every language (Glenn,
  2026-08-12).
- Cross-language goldens: testdata/table/*.bin pinned by the C++ test
  (rigidbody moving/at-rest, probearray, testdata), byte-compared by the Go
  test; both sides also exercise the permissive contract on hand-built
  buffers (unknown id, changed kind, truncation, out-of-range). DONE.
- C# emitter: the same pair for Unity (writes UserSettings, reads
  config/assets/events). NEXT.
