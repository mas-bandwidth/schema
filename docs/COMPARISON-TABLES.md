# Tables against FlatBuffers and Protocol Buffers

Tables are what people use FlatBuffers and Protocol Buffers for: data that
outlives the build that wrote it. This page puts schema's tables beside both,
feature by feature, and answers one question:

> Would a team already using schema ever reach for FlatBuffers or Protocol
> Buffers to get one thing schema lacks?

The answer today is a short list, ranked, with a verdict on each. The page is
honest in both directions: where either format is ahead it says so, and where
schema is ahead it says how. Every claim about schema cites a section of
[SPEC-TABLES.md](SPEC-TABLES.md) (`§n`), [SPEC.md](SPEC.md) (`SPEC §n`) or
[USAGE.md](USAGE.md); every claim about the other two cites the page it was
read from, listed at the end.

**This page compares design and language features.** Measurements of the same
data as a schema table, a FlatBuffer and a Protobuf message, read, written and
opened on one machine, are the second half of the comparison and are not here
yet. [COMPARISON.md](COMPARISON.md) measures the type wire, not tables.

## One scene, three ways

The same data declared in each language: a scene of entities, each with a
position struct, a bounded name, an optional health, a union of two component
types, an array keyed by an equipment slot, and a thumbnail blob.

```
package scene

const MaxEntities = 1024
const MaxName     = 32
const MaxThumb    = 4096

enum Slot { Head, Body, Legs }           // implicit None = 0; rides by NAME on the wire

type Vec3 { x float32  y float32  z float32 }   // a plain struct, nested by value

type Light
{
    intensity float32 = 1.0
    color     Vec3
}

type Mesh
{
    asset_id uint32
    lod      uint8 | min = 0, max = 7     // the bound is part of the type; clamped on load
}

union Component
{
    light Light
    mesh  Mesh
}

table Item
{
    id    uint32
    count uint16 = 1
}

table Entity
{
    name      string(MaxName)            // bounded; no allocation
    position  Vec3
    health    ?int32                     // present or absent, by value
    component Component                  // None is the absence
    equipment [Slot]Item                 // one slot per named variant, by name
    thumbnail bytes(MaxThumb)            // inline at its bound
}

table Scene
{
    entities [..MaxEntities]Entity       // the root table IS the file format
}
```

`Scene` is fixed down to its leaves, so it is one plain struct with `Measure`,
`Save` and `Load`, a cook and a block form, and nothing on any path allocates
(§2.2, §6.1). `equipment` cannot be indexed by `None` or past the enum in any
build (§2.4). Every field is optional on the wire with a declared default, and
there is no presence machinery to spell (§4).

In FlatBuffers the same scene is a `struct Vec3`, tables for the rest, a
`union Component` with its implicit `_type` field, and `equipment` as a vector
of `SlotItem` tables sorted on a `(key)` field so `LookupByKey` can binary
search it. Strings and vectors are unbounded, `lod` has no bound, the fields are
append-only unless every one carries an `id`, and `file_identifier "SCEN"`
names the root.

In Protocol Buffers it is messages with hand-numbered fields, `oneof component`,
`map<int32, Item> equipment` (enum keys are refused, so the slot rides as its
number), `bytes thumbnail`, and `int32 health` with explicit presence. `Vec3` is
a length-delimited record rather than an inline struct, there is no `uint8` or
`uint16`, no bound, and no declared default in editions.

| construct | schema | FlatBuffers | Protocol Buffers |
|---|---|---|---|
| position struct | `type Vec3`, inline by value in every form | `struct`, inline, unversioned | `message`, length-delimited |
| name string | `string(32)`, fixed storage | `string`, unbounded, by offset | `string`, unbounded, allocated |
| optional health | `?int32` with `health_present` | `int = null` | explicit presence, `has_health()` |
| union | `union` with `None`, arms by name | `union` plus a `_type` field, append-only | `oneof`, by number; moving a field into one is unsafe |
| keyed by Slot | `[Slot]Item`, by name, refuses `None` | sorted `[SlotItem]` with `(key)`, or positional | `map<int32, Item>`, enum keys refused, order undefined |
| bytes blob | `bytes(4096)` inline | `[ubyte]`, unbounded | `bytes`, unbounded |
| the root | any table, no envelope | `root_type` and `file_identifier` | none |
| field identity | the name, hashed | vtable position or `id` | a number by hand |
| evolution guard | `tables.baseline` with reasons (§18) | `flatc --conform` | `buf breaking`, third party |

## Where they are ahead, and what happens about it

Ranked by the question at the top: how likely a team on schema is to pull in
the other format for this one thing.

| | what they have | pull-in? | verdict |
|---|---|---|---|
| 1 | **A language schema does not ship.** Both have Python, TypeScript, Swift and Kotlin. | likely | Not a format feature. The four are tracked as [#381](https://github.com/mas-bandwidth/schema/issues/381), type wire first, then every tables row. |
| 2 | **A service or SDK that only speaks Protobuf.** | certain, for that one edge | Never removable. A foreign endpoint's format is what you use at that endpoint. Tables inside, the foreign format only at the boundary. |
| 3 | **Maps with string or integer keys.** Protobuf has `map<K,V>`; FlatBuffers has the sorted-vector idiom. | likely for a team raised on Protobuf | After 3.0.0, in tables only, never in types, and a map makes the table variable: it rides with the pointers, never in a fixed record ([#380](https://github.com/mas-bandwidth/schema/issues/380)). Enum keys are already better served by `[E]T` (§2.4). |
| 4 | **Unbounded strings and bytes at used size.** | likely for an asset archive | [#259](https://github.com/mas-bandwidth/schema/issues/259): the byte-buffer primitive, so an image lives inside a table at its own size and a mapped cook points at it with no copy. Before 3.0.0, since it moves the wire. |
| 5 | **A verifier for untrusted bytes.** FlatBuffers ships a separate `Verifier` pass, in C++ and Swift, with a basic one in C, of its thirteen languages. | possible | Table reads are untrusted: they arrive over the network and carry the type wire's security posture; only the cook open is trusted. So the tolerant read IS the verifier, in every language, and [#391](https://github.com/mas-bandwidth/schema/issues/391) is the fuzzer with an independent oracle that proves it in CI. Before 3.0.0, since the release makes the claim. |
| 6 | **Every type in tables.** Nothing here is ahead of schema, but §11 refuses `fixed`, `ufixed` and the 128-bit integers in a table field today, and a deterministic simulation's save holds exactly those. | possible | [#390](https://github.com/mas-bandwidth/schema/issues/390): every type the type wire carries rides in a table. Before 3.0.0. |
| 7 | **Sorted lookup inside a buffer.** FlatBuffers' `key` attribute and `LookupByKey`. | possible, for a large catalog opened by id | Library-side, after 3.0.0: a sort the cook writer holds and a generated `Find` over the cooked rows. Never stored semantics. |
| 8 | **Reflection in more languages.** | possible, for an editor in Python or C# | Descriptors are built into every table's generated code with no schema files and no RTTI (§8). They reach every language with the parity matrix on [#366](https://github.com/mas-bandwidth/schema/issues/366). |
| 9 | **Doc comments** that reach generated code and tooling. | unlikely to move anyone | Designed and deferred (SPEC §4.1); one column in the view when it lands. |
| 10 | **Engine packages.** A Unity package, an Unreal plugin. | friction, not capability | Neither competitor ships first-party engine packages either. An ecosystem item for later; the generated C# is netstandard2.1 and runs on Unity today. |
| 11 | **Lint, IDE support, a schema registry.** | comfort | `schemafmt` and the checker are the lint. A registry is not needed: schemas live in the game's repo and the generated code is yours. An editor grammar is cheap and later. |
| 12 | **Spellings both have trivially**: arrays of pointers, arrays of unions, optional arrays and strings. | unlikely; the diagnostic names the workaround | Named follow-ons in §15, each with its wire question stated. |

One rule sits under every verdict above (§1): **table is king.** A file is a
tree of nodes and every node is a table, so anything the data needs is
expressed as tables of tables, and where it cannot be, `table` gets better
rather than gaining a construct beside it. Maps, blobs and every type the
type wire carries land under that rule, and the recursion gaps that remain
(a union whose arms are tables, an array of pointers, an array of unions, an
optional array) are tracked as one closure item, [#392](https://github.com/mas-bandwidth/schema/issues/392).

Two things Protocol Buffers has were judged not needed on purpose:

- **Unknown fields preserved on write.** Everything in schema has a schema. A
  writer writes exactly the structured data it declared ahead of time, and a
  tool built from an older schema that rewrites a newer file drops what it does
  not know and the read report counts it (§4). Carrying bytes with no schema is
  the thing the name says this library does not do. Rebuild the tool.
- **A file that says what it is.** A table is a table and can be the root of
  anything; no document root is prescribed (§17.3). When a file needs identity,
  it is a field on the root table or lives beside the file, never an envelope.

## Where schema is ahead

Each with the section that defines it and the reason a game cares.

| beyond | where | why it matters |
|---|---|---|
| **Bounds in the type.** `\| min, max` is part of the type; the type wire sizes bits from it and the table wire clamps and counts on load. | SPEC §4.3, §4 | A health capped at 1000 is ten bits on a packet, and a save cannot carry an out-of-range value. |
| **Two classes derived from the declaration.** A table with no pointer is a plain struct; one with a pointer gets an arena. A build-failing gate holds the fixed class to zero cost. | §2.2 | A config struct pays nothing for the machinery a scene graph needs. |
| **No allocation on read, every class, every form, nine languages.** | ladder, §6.5 | No GC pressure and no allocator on the load path. |
| **`Measure` equals `Save` at exact capacity**, held by a mandatory battery. | §9 | Caller-owned buffers, parallel scatter writes, never a realloc. |
| **Pointers with sharing preserved.** A node written once however many times it is named; a flat node table, so a 260-node chain is not a recursion; cycles refused by name. | §2.1, §3.1 | Trees, graphs, palettes shared by many nodes. Neither competitor shares a node, and Protobuf has no pointer at all. |
| **Enum-keyed arrays that ride by name** and refuse a bad key in every build. | §2.4, §3.2 | Per-ship-type config survives inserting a ship type in the middle. |
| **Evolution by name.** Fields, enum variants and union arms are identified by their name's hash. Add anywhere, remove, reorder. `was` renames; a collision is refused at compile time. | §5 | No append-only rule and no numbers to assign by hand. |
| **A read report instead of pass or fail.** `unknown`, `kind_mismatch`, `clamped`, `duplicate`, `malformed`; never fatal on data from another build; a damaged level stops only itself. | §4 | Tools surface it, games set policy, and a corrupt sub-table does not kill the file. |
| **The silent-edit class enumerated: exactly three**, with a compile-time baseline that refuses them and keeps a reasoned history. | §4.1, §18 | Saves from years ago read right, and when one does not the history says why. |
| **A build version computed by the compiler.** Every fact a cook's bytes depend on, digested; `(asset hash, build version)` is a build-cache key. Split from the protocol id, so a table edit never forces a lockstep redeploy. | §10, §20 | Distributed cooking with no version numbers by hand; ship tools and game on different days. |
| **The cook.** `Open` is a header match and a pointer: O(1), mmap-friendly, byte order settled offline, attribution separable. In every port, not only C++. | §7 | "Don't parse, just point" at a gigabyte catalog. |
| **The block form.** A third projection of a fixed table: rows at a pitch the compiler computes and both languages' generated code asserts, filled wide by many threads by obligation. | §19, §12.1 | Render data C++ writes and C# reads at 60 Hz with no marshaling. This is the render-data case FlatBuffers was tried on first and replaced (§12.1). |
| **Optional by value.** `?T` costs a bool and changes nothing else; `T` and `?T` share one framing. | §2.3 | An optional settings block is not a pointer. |
| **64-bit clean.** Eight-byte region references, 64-bit cook part lengths, no aggregate wire ceiling. | §6.3, §7.1 | Catalogs past 2 GiB without a per-language attribute. FlatBuffers' `vector64` is not in every port; Protobuf stops at 2 GiB. |
| **Reflection for free.** Descriptors in the generated header; a game build compiles none of the view. | §8 | An editor walks anything; the shipped build carries nothing. |
| **One compiler, nine languages, byte-identical goldens and one hostile corpus in CI.** | SPEC §1, SECURITY.md | A divergence between languages is a bug CI sees, not a user. |
| **C-like C++ output**, no STL, hooks for assert, fatal, allocate and release. | §13.9 | Reads like the engine's own code, compiles fast, takes your allocator. |
| **No envelope.** A root table is a format. | §1, §17.3 | Your magic, your hash, your file layout. |
| **Flags and constants as language constructs**, exported into every target with names. | USAGE | A mask with a renderer for its log line, not an int and a comment. |

## Judged not needed, with the reason

| feature | who has it | why not |
|---|---|---|
| `Any` and dynamic typing | Protobuf | A union whose arms are tables is the typed bag and evolves by name (§2.6); C++ carries table arms, and the ports are named follow-ons (§15). |
| In-place mutation of a serialized buffer | FlatBuffers | A cook is immutable and regenerated by a cache; the block form is the every-frame mutable answer. |
| Streaming and size-prefixed messages | both | A root has no outer length and the transport frames it (§3). |
| Extensions and custom options | both | The attribute vocabulary is closed on purpose (SPEC §4.2); type tags are the claiming mechanism when one is wanted. |
| Schema-less values (FlexBuffers, `Struct`) | both | A schema language's users have schemas; a JSON text in a `bytes` field is the escape. |
| Well-known types | Protobuf | A `type Timestamp` is three lines and the language pre-defines nothing. |
| Required fields | FlatBuffers | Both competitors' own guidance says not to; every field optional with a declared default is the rule. |
| Explicit or sparse enum values | both | On the table wire a variant rides by name, so its number is invisible (SPEC §9). |
| Varint compaction | Protobuf | The type wire is the compact wire; tables trade bytes for tolerance by design, and the ladder states the price. |
| Deprecation markers | both | Removal is free and reported, which is what deprecation exists to fake elsewhere. |
| Reserved numbers | Protobuf | Ids are name hashes and cannot be reused by number. Re-adding a retired name with a new meaning is the baseline's job, and a retired-names list is a small addition to it after 3.0.0. |
| RPC and gRPC | both | Out of scope, by the owner's word. Add it on top if you care. |

## The feature table

The full comparison, in six groups. "—" means the feature does not exist
there. Citations for FlatBuffers and Protocol Buffers use the short names in
the source list at the end.

### Declarations and the schema language

| feature | schema tables | FlatBuffers | Protocol Buffers |
|---|---|---|---|
| Declaration kinds | `const`, `enum`, `flags`, `type`, `union`, `table`; one `package` per unit (SPEC §4.2) | `table`, `struct`, `enum`, `union`, `namespace`, `root_type`, `file_identifier`, `rpc_service`, `attribute` (FB-schema) | `message`, `enum`, `service`, `oneof`, `map`, `extend`, `option`, `package`, `import` (PB-editions) |
| Scalar types | int8 to int64, uint8 to uint64, `bits(N)`, `bool`, float32, float64, compressed float; on the type wire also int128, uint128, `fixed`, `ufixed` (SPEC §4.3). Tables refuse the last four today, [#390](https://github.com/mas-bandwidth/schema/issues/390) | byte to ulong, float, double, bool (FB-schema) | int32, int64, uint32, uint64, sint, fixed, sfixed, float, double, bool, string, bytes (PB-proto3) |
| Declared numeric bounds | `\| min, max` in the type; bits on the type wire, clamp and count on the table wire (SPEC §4.3, §4) | — | — |
| Fixed point | type wire: `fixed(I,F)`, `ufixed(I,F)`, exact round trip (SPEC §4.3); tables: #390 | — | — |
| Constants | `const`, typed or inferred, exported to every language, usable in bounds and extents | — | — |
| Enums | implicit `None = 0`, dense from 1, `Max`; on the table wire a value rides as the hash of its name (§3) | explicit values on a declared integer type; `bit_flags` (FB-schema) | 32-bit values, first must be 0, aliases, reserved values, open or closed per edition (PB-enum) |
| Flags | `flags`, up to 64 bits, `uint64` storage everywhere, `.Count`; rides as a mask (§3) | `enum (bit_flags)` | — |
| Inline struct | a `type` nested by value; a whole fixed table is a plain struct (§2.2, §6.1) | `struct`: scalars and structs only, inline, unversioned (FB-internals) | — ; every message is length-delimited (PB-encoding) |
| Nested composites | tables nest by value; a by-value cycle is refused naming it (§2) | tables by offset; depth bounded by the verifier's `max_depth` (FB-cpp) | unlimited nesting; parser recursion limit 100 (PB-limits) |
| Pointers, graphs, sharing | `*T` to a declared table: recursion, sharing preserved end to end, null; a reachable cycle refused at save (§2.1, §3.1) | forward offsets only; an `Offset` may be reused by the builder; cycles impossible (FB-internals) | tree only; no sharing, no cycles (PB-encoding) |
| Optional fields | `?T` on nested table, type, enum, flags, scalar: value plus a `_present` bool, fixed size, no allocation (§2.3); on arrays, strings, bytes a follow-on (§15) | optional scalars via `= null`; references null when absent (FB-schema) | explicit presence: proto2 all, proto3 `optional`, editions explicit by default (PB-presence) |
| Defaults | `= v` on scalars; part of the wire contract because a default is elided; changing one is a silent edit the baseline refuses (§4, §4.1, §18) | scalar defaults; "don't change existing default values" (FB-evolution) | proto2 `[default]`; proto3 zero, not serialized; editions serialize set defaults (PB-presence) |
| Union | `union` with implicit `None`; arms by name hash, so add, remove, reorder freely; arms may be tables (§2.6, §5) | `union` of tables; structs and strings experimental; vectors of unions C++ only (FB-schema) | `oneof`; `Any` for open typing (PB-proto3) |
| Arrays | always bounded: `[N]T`, `[..N]T`, `[A..B]T`; the bound is not wire identity (§2, §4) | `[T]` unbounded; `[T:N]` in structs only (FB-schema) | `repeated`, unbounded, packed scalars (PB-proto3) |
| Enum-keyed arrays | `[E]T`: one slot per variant, no `None` slot, bad keys refused in every build, slots ride by name (§2.4, §3.2) | — | — |
| Maps | after 3.0.0, tables only, makes the table variable (#380) | sorted vector of tables with `key` plus `LookupByKey` (FB-cpp) | `map<K,V>`, integral or string keys, order undefined (PB-proto3) |
| Sorted lookup in a buffer | after 3.0.0, library-side (§15) | `key`, `CreateVectorOfSortedTables`, `LookupByKey` (FB-cpp) | — |
| Strings | `string(N)`: bounded, no interior NUL, fixed storage plus used length (SPEC §4.7); on the wire length plus bytes, no terminator (§3) | zero-terminated, unbounded (FB-internals) | UTF-8 validated (PB-features), unbounded to 2 GiB (PB-limits) |
| Bytes | `bytes(N)` inline; unbounded at used size is #259 (§2.5) | `[ubyte]` unbounded; `nested_flatbuffer` (FB-schema) | `bytes`, unbounded to 2 GiB (PB-encoding) |
| Conditional groups | `if guard { }` elided when false (SPEC §4.4, §3) | — | — |
| Units and includes | one `package` per unit, all files compiled together, order-free; cross-file table references form a DAG (SPEC §3.2, §11) | `include`, nested `namespace` (FB-schema) | `import`, `import public`, `package` (PB-editions) |
| Attributes | closed typed vocabulary right of `\|`; unknown is a compile error; type tags inert until claimed (SPEC §4.2) | user attributes declarable, read via reflection (FB-schema) | custom options with retention and targets (PB-editions) |
| Doc comments | deferred, design pinned (SPEC §4.1) | `///` into generated code and the binary schema (FB-flatc) | preserved in descriptors' source info |
| Reserved names | after 3.0.0, a retired list in the baseline | — ; never remove, deprecate instead (FB-evolution) | `reserved` numbers and names (PB-proto3) |
| Deprecation | — ; removal is free and counted | `(deprecated)`: accessors dropped, slot kept (FB-schema) | `[deprecated = true]` (PB-proto3) |
| Required | — ; every field optional with a default (§4) | `(required)`, verifier-checked (FB-schema) | removed; `LEGACY_REQUIRED` only (PB-ed-overview) |
| Rename | `\| was = "old"` keeps the id; bare rename refused (§5) | free; names not serialized (FB-evolution) | free on binary; reserve the old name for JSON (PB-proto3) |
| Field identity | `fold16(fnv1a32(name))`; collisions refused at compile time (§5, §3.1) | vtable position or explicit `id` (FB-evolution) | hand-assigned number 1 to 536,870,911 (PB-proto3) |
| Root identity | none prescribed; any table is a root (§1, §17.3) | `root_type`, four-char `file_identifier`, size prefix (FB-schema) | none; `Any` carries a type URL (PB-proto3) |
| RPC | — ; out of scope | `rpc_service`, `flatc --grpc` (FB-flatc) | `service` (PB-3p) |
| Schema-less data | — | FlexBuffers (FB-flex) | `Struct`, `Value` (PB-wkt) |
| Well-known types | — ; types are the user's | — | Timestamp, Duration, wrappers, Any, Struct (PB-wkt) |
| Formatter | `schemafmt`, one style; the CLI formats before reading (SPEC §7.4) | — | `buf format` (buf) |

### The wire

| feature | schema tables | FlatBuffers | Protocol Buffers |
|---|---|---|---|
| Encoding model | byte TLV: `id u16, kind u8, payload`, u16 terminator; a closed set of kinds a third party could implement from §3 alone | root offset, table, vtable of u16 offsets; references by u32 offset; structs inline (FB-internals) | tag varint `(field << 3) \| type`, six wire types, varints, LEN records (PB-encoding) |
| Field identity on the wire | name hash (§3) | vtable position (FB-internals) | field number (PB-encoding) |
| Self-describing | partly: kinds and lengths ride, so any reader skips anything; names ride as hashes (§3) | no; needs the binary schema (FB-IR) | no; needs descriptors (PB-techniques) |
| Elision | default scalars, empty strings and arrays, all-default nested tables, empty unions, false-guarded groups are not written; a present `?T` and a non-null `*T` always are (§3) | scalar defaults omitted unless forced (FB-flatc) | implicit-presence defaults omitted (PB-presence) |
| Order and duplicates | declaration order written; any order decodes; last wins, counted (§3) | order irrelevant (FB-internals) | any order; last wins; nested messages merge (PB-encoding) |
| Byte order | little-endian wire (§3); a cook is written in the target's order and a foreign order is refused at `Open` (§7.1) | little-endian; accessors swap on big-endian hosts (FB-cpp) | fixed types little-endian; varints order-free (PB-encoding) |
| Alignment | none on the wire; the cook and block are the aligned forms (§3, §7.2, §19.3) | every scalar aligned to its size; `force_align` (FB-internals) | none |
| Framing overhead | `TableMixed`: 2391 bytes against the type wire's 438; the difference is ids, kinds, lengths, counts, terminators (the ladder) | vtable, offsets, padding | one or two byte tags plus varints |
| Length prefixes | every nested table, string, array and union arm carries a u32 length; the root has no outer length (§3, §17.3) | not self-delimiting; `--size-prefixed` adds one (FB-flatc) | not self-delimiting; write the size first (PB-techniques) |
| Pointer encoding | flat node table under a reserved id, u32 node index per pointer, 0 null, 1 root, one record per node however many times named (§3.1) | inline forward offsets (FB-internals) | — |
| Size ceilings | node body up to 4 GiB, refused at save; no aggregate ceiling; 8-byte region references, 64-bit cook part lengths (§2.1, §6.3, §7.1) | 32-bit offsets: 2 GiB; `vector64` for tail vectors, not in every port (FB-64) | 2 GiB (PB-limits); "for data that exceeds a few megabytes, consider a different solution" (PB-overview) |
| Packed scalar arrays | elements back to back at fixed width (§3) | contiguous (FB-internals) | packed varints in one record (PB-encoding) |
| Varint compaction | — ; the type wire's job, via bounds | — | varints, zigzag (PB-encoding) |
| Byte-identical output across implementations | the tool and every backend are held to §3 by goldens; `Measure` equals `Save` (§9, §17.1) | no cross-language claim | "don't assume serialization stability across builds" (PB-dos) |

### Evolution

| feature | schema tables | FlatBuffers | Protocol Buffers |
|---|---|---|---|
| Add a field | anywhere; old readers skip and count (§4) | at the end only, or `id` on every field (FB-evolution) | anywhere with an unused number (PB-proto3) |
| Remove a field | freely; reads as its default (§4) | never; deprecate (FB-evolution) | remove and reserve the number (PB-proto3) |
| Reorder | free (§4) | breaks unless `id` (FB-evolution) | free (PB-encoding) |
| Rename | `was` (§5) | free (FB-evolution) | free on binary; reserve for JSON (PB-proto3) |
| Change a type | `kind_mismatch`: skipped, counted, never misdecoded (§4); same-kind respellings are the silent class the baseline refuses (§4.1) | only at identical width, "with careful handling" (FB-evolution) | a fixed list of compatible pairs; anything else misdecodes silently (PB-proto3) |
| Change a default | silent on the wire; the baseline refuses it until moved with a reason (§18) | "don't" (FB-evolution) | proto3 has none; proto2 reader-side (PB-proto2) |
| Enum evolution | add anywhere, remove, reorder; unknown reads as `None` and counts (§5) | append or explicit values; code handles unknowns itself (FB-schema) | add values; open enums keep the int, closed enums move it to unknown fields (PB-enum) |
| Union evolution | arms by name; add anywhere, remove, reorder (§2.6, §5) | append or explicit discriminant (FB-evolution) | adding is fine; moving an existing field into a oneof is unsafe (PB-editions) |
| Flags evolution | append only, retire in place; the baseline refuses the rest (§4.1) | explicit values, any order (FB-schema) | — |
| Array bound change | prefix kept, `clamped` counted; a short array fills with defaults (§4) | unbounded | unbounded |
| `T` to `?T` to `*T` | `T` and `?T` are byte-identical for non-default content; to or from `*T` is a counted mismatch (§2.3, §4) | changes default semantics; required/optional changes break (FB-schema) | presence changes round-trip of defaults (PB-presence) |
| Unknown fields on read | skipped by length, counted (§3, §4) | ignored (FB-evolution) | retained in the unknown set (PB-proto3) |
| Unknown fields on rewrite | dropped and counted, by design; the writer has a schema | the buffer keeps them if forwarded whole (FB-evolution) | preserved and re-serialized (PB-proto3) |
| Read report | `unknown`, `kind_mismatch`, `clamped`, `duplicate`, `malformed`; nothing fatal from another build (§4) | verifier pass or fail (FB-cpp) | success or failure, plus the unknown set (PB-message) |
| Silent edits enumerated | exactly three, each with its answer (§4.1) | scattered warnings (FB-evolution) | scattered (PB-dos) |
| Compile-time guard | `tables.baseline`: refuses the silent three plus kind, spelling and key changes; `--update --reason` keeps a dated history (§18) | `flatc --conform` (FB-flatc) | `buf breaking`, third party (buf) |
| Same-build fast forms | cook and block are same-build by construction: build version match or refuse (§7, §19.4, §20) | the buffer is always the evolvable form | — |
| Version identity | protocol id and build version, both computed by the compiler; a table edit moves only the latter (§10, §20) | `file_identifier`, by hand (FB-schema) | none; editions version the language (PB-ed-overview) |

### Runtime forms, allocation, performance

| feature | schema tables | FlatBuffers | Protocol Buffers |
|---|---|---|---|
| Point at the bytes | the cook: `Open` is O(1), mmap-friendly, order settled offline (§7); the block: rows at a fixed pitch (§19); the tolerant wire always parses | always (FB-home) | never (PB-overview) |
| Parse into a struct | `Load` into caller-owned storage; a fixed table is a struct (§6.1) | object API `UnPack()` into STL-backed classes (FB-cpp) | generated message classes (PB-message) |
| Allocation on read | none, every class, every form (§6.5) | none in place; the object API allocates (FB-cpp) | per message, string and repeated field; arenas mitigate (PB-message) |
| Allocation on write | fixed class none; variable class an arena in bulk, thread-local, never per node; block takes a caller allocator (§6.4, §6.5, §19.1) | the builder grows a buffer; custom allocator (FB-cpp) | into caller memory; the message objects allocate (PB-message) |
| Exact size before writing | `Measure` equals `Save`, held by a battery (§9) | after `Finish` | `ByteSizeLong()` (PB-message) |
| Mutate in place | — ; regenerate | `--gen-mutable` scalars; reflection resizes (FB-flatc) | — |
| Multi-threaded build | thread-local arenas; block fill by N workers is an obligation checked under TSan (§6.4, §19.5) | one builder per thread | — |
| Relocatable | a fixed struct is memcpy-able; a locked region relocates by memcpy with self-relative references (§6.3, §9) | position-independent (FB-internals) | bytes |
| Cross-language rows at a pitch | the block form: layout computed by the compiler, asserted by generated code on both sides, refused at open on mismatch (§19, §19.3) | a vector of structs is inline, but the schema does not assert both sides' native layout (FB-internals) | — |
| Offline cook for a build | `schema cook` and generated `Cook`; `(asset hash, build version)` is the cache tuple; attribution separable (§7, §7.6, §20.6) | the buffer is the cooked form, little-endian, build-independent (FB-internals) | — |
| Classes | fixed and variable derived from the declaration, held by a gate (§2.2) | struct and table declared; a table is always by offset (FB-schema) | one |
| Performance obligations | the ladder: a fixed table beside its type on the ledger; the block matches hand-written scatter both sides; unexplained slowness is a defect (§12.1) | a benchmarks page | "fast parsing" (PB-overview) |
| Big-endian targets | C++ proves wire, cook and block on an emulated target; Go, Rust, Java checked | accessors swap (FB-cpp) | neutral by varints (PB-encoding) |
| Generated code dependencies | C++ tables: C-like, no STL, C headers, hooks for assert, fatal, allocate, release (§13.9) | header-only runtime; STL in the object API (FB-cpp) | `libprotobuf` (PB-message) |

### Validation of untrusted data

| feature | schema tables | FlatBuffers | Protocol Buffers |
|---|---|---|---|
| Untrusted bytes on the tolerant wire | the read is the validator: every length bounds-checked against its body, counts against the body, ranges clamped, a bad level stops itself; `LoadMeasure` lets the caller refuse a region size before allocating (§4, §6.5). The independent-oracle fuzzer that proves it in every language is #391 | a separate `Verifier`, required before access, in C++ and Swift with a basic one in C (FB-support); `max_depth` 64, `max_tables` 1M (FB-cpp) | the parser validates structure; recursion limit 100; UTF-8 verify (PB-features) |
| Trusted fast path | cook and block are trusted by design; `Open` checks identity, not hostility; a signature over the file is the integrity answer (§7, §13.4) | skip the verifier for trusted buffers (FB-cpp) | — |
| Value-range enforcement | clamp and count on the table wire; reject on the type wire (§4, SPEC §5) | — | — |
| Depth and DoS bounds | by-value depth is fixed by the schema; a pointer graph is flat, so a chain is not a depth (§3.1) | verifier caps (FB-cpp) | recursion depth 100 (PB-limits) |
| Cross-language agreement on refusal | every backend held to one conformance corpus with hostile cases; a divergence is a security bug (SPEC §1, SECURITY.md) | a verifier per language, varying (FB-support) | a conformance suite |

### Reflection, text, tooling

| feature | schema tables | FlatBuffers | Protocol Buffers |
|---|---|---|---|
| Runtime reflection | descriptors in every table's generated header: name, kind, id, offset, bounds, guards, nesting; no schema files, no RTTI (§8.1); the type view and registry specified (§8) | binary schema plus `reflection.h` in C++, basic in C; mini-reflection (FB-IR, FB-support) | descriptors, `Reflection`, `DynamicMessage` (PB-message) |
| Reflection cost | on the side; a game build never compiles the view (§8.4) | 2 to 6 bytes per field for mini-reflection (FB-cpp) | in the full runtime always |
| Text form | JSON in and out by one walk, mapping pinned per kind, `\| json = "key"`, report counters; a shared node labeled `&node` (§16.7, landing with [#388](https://github.com/mas-bandwidth/schema/pull/388)) | JSON in `flatc`; parsing in C++ and C (FB-support) | ProtoJSON in every runtime; text format (PB-json, PB-text) |
| Whole-tree packing | `schema pack` and `unpack`: a directory mirrors the root; keyed arrays as one file per variant; byte-stable both ways (§17) | `flatc -b` (FB-flatc) | `protoc --encode` and `--decode` |
| Dump, diff, check | `cook-check`, `uncook`, `build-version --facts`, `projection` (§7, §20.7); generic dump and diff over the registry a follow-on (§15) | `flatc --json`, `FlatBufferToString` (FB-flatc) | `DebugString`, third-party explorers (PB-3p) |
| Lint, breaking, registry | the baseline and the checker; no registry by design (§18) | `--conform` (FB-flatc) | `buf lint`, `buf breaking`, the BSR (buf) |
| Editor support | none yet | plugins exist | VS Code, IntelliJ, Vim (PB-3p) |
| Debug names | `EnumName`, `FlagNames` in every target, allocation-free (USAGE) | `--gen-name-strings` (FB-flatc) | via descriptors (PB-message) |
| Embeddable compiler | a Go library; `cmd/schema` is a thin client; your generator walks the IR (USAGE) | `libflatbuffers` | protoc plugins (PB-3p) |
| Converters | — | `flatc --proto` reads `.proto` (FB-flatc) | — |
| Languages | nine on the type wire held byte-identical in CI; tables landing per the matrix on #366; four more tracked on #381 | thirteen listed, coverage uneven (FB-support) | ten first-party, forty-plus third-party (PB-3p) |

## Sources

Every FlatBuffers and Protocol Buffers claim above was read from one of these
pages on 2026-09-03.

| short | page |
|---|---|
| FB-schema | https://flatbuffers.dev/schema/ |
| FB-evolution | https://flatbuffers.dev/evolution/ |
| FB-internals | https://flatbuffers.dev/internals/ |
| FB-cpp | https://flatbuffers.dev/languages/cpp/ |
| FB-flatc | https://flatbuffers.dev/flatc/ |
| FB-flex | https://flatbuffers.dev/flexbuffers/ |
| FB-support | https://flatbuffers.dev/support/ |
| FB-home | https://flatbuffers.dev/ |
| FB-IR | https://flatbuffers.dev/intermediate_representation/ |
| FB-64 | https://github.com/google/flatbuffers/issues/7537 and https://github.com/google/flatbuffers/issues/8555 |
| PB-proto3 | https://protobuf.dev/programming-guides/proto3/ |
| PB-proto2 | https://protobuf.dev/programming-guides/proto2/ |
| PB-editions | https://protobuf.dev/programming-guides/editions/ |
| PB-ed-overview | https://protobuf.dev/editions/overview/ |
| PB-features | https://protobuf.dev/editions/features/ |
| PB-encoding | https://protobuf.dev/programming-guides/encoding/ |
| PB-presence | https://protobuf.dev/programming-guides/field_presence/ |
| PB-json | https://protobuf.dev/programming-guides/json/ |
| PB-text | https://protobuf.dev/reference/protobuf/textformat-spec/ |
| PB-techniques | https://protobuf.dev/programming-guides/techniques/ |
| PB-wkt | https://protobuf.dev/reference/protobuf/google.protobuf/ |
| PB-enum | https://protobuf.dev/programming-guides/enum/ |
| PB-limits | https://protobuf.dev/programming-guides/proto-limits/ |
| PB-dos | https://protobuf.dev/programming-guides/dos-donts/ |
| PB-message | https://protobuf.dev/reference/cpp/api-docs/google.protobuf.message/ |
| PB-overview | https://protobuf.dev/overview/ |
| PB-3p | https://github.com/protocolbuffers/protobuf/blob/main/docs/third_party.md |
| buf | https://buf.build/docs/breaking/ |
