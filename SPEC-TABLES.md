# schema — tables

Tables are schema's declarations for **data that outlives builds**: config
files, asset archives, tool output, editor state — bytes written by one
build of one tool and read by another build of another program, possibly
years apart. The packet wire (`type`, SPEC.md) is hardcoded and guarded by
the protocol id: same-or-refuse. The table wire is the opposite contract:
**any reader reads any data**, and the differences are reported, never
fatal.

The two contracts never mix. Flatbuffers- and protobuf-class evolution
ideas apply here, to tables — they do not apply to `type`, whose wire is
hardcoded under the protocol id, and nothing in this document changes that.

**Backend status: C++.** Every other backend refuses a unit that declares
tables, by name, with this document cited. Per-language backends are named
follow-ons.

## 1. Purpose

- **Users build their own formats.** A table declaration plus nesting is a
  complete file format: declare a root table, nest subtables, and the
  generated code saves and loads it. schema imposes no envelope, no
  directory, no document concept, no prescribed root — a config-file
  format and an asset-archive format are both just tables someone
  declared.
- **Tools and editors walk fields by name.** Generated reflection
  descriptors carry every field's name, kind, offset, bounds and nesting —
  enough to walk, print, diff, edit or bind any table value at runtime
  with no RTTI and no schema files on hand.
- **The wire is version-tolerant** (§4), so tools and games on different
  builds exchange data freely: old readers skip what they cannot name, new
  readers default what old writers never wrote.

## 2. Declaration

```
table ShipConfig
{
    name        string(64)
    class       ShipClass
    health      int32 | min = 0, max = MaxHealth
    hardpoints  [..8]Hardpoint

    physics table
    {
        mass    float32 | min = 0.1, max = 100000.0
        drag    float32
    }
}
```

A table body is a type body — the full field grammar of SPEC §4.2, hosted
by `table`: bare and ranged integers, `bits(N)`, `bool`, floats and
compressed floats, `fixed`/`ufixed`, enums, flags, strings, bytes, bounded
arrays, unions, `if` branches, and declared types as field groups. Two
additions:

- **Tables nest.** A field may be an inline anonymous subtable (above) or
  a named table used as a field type. Nesting is by value; a bounded array
  of tables is a collection. A table may not contain itself, directly or
  through any chain — recursion is refused with the cycle named.
- **`was` — the rename attribute** (§5).

One exclusion, inherited from the wire kinds (§3): the 128-bit family has
no table-wire kind and is refused in table bodies by name.

## 3. The wire

**The wire is neutral.** It carries none of schema's packing opinions — no
bitpacking, no range compression, no back-referenced branches. It is the
encoding a third party could implement from this section alone, without
schema's codebase:

- Little-endian, byte-oriented throughout.
- A table value is a sequence of **fields**, each `id (u16), kind (u8),
  payload`, terminated by an end marker. The id is the field's name hash
  (§5); the kind is one of the closed kind set: bool, i8..i64, u8..u64,
  f32, f64, string, array, union, table.
- Variable-length payloads — strings, bytes, arrays, unions, nested
  tables — are **length-prefixed**, so any reader can skip any field
  without understanding it, and any parent can hand each nested table to a
  different worker (§7).
- Schema's declaration-side types map onto the neutral kinds: a ranged
  integer rides as its storage-width integer kind, `bits(N)` as the
  narrowest unsigned kind that holds it, compressed floats as f32/f64,
  fixed point as its integer storage, enums and flags as their unsigned
  storage. The bounds, resolutions and enum vocabularies stay on the
  DECLARATION side, where they validate and clamp on load (§4) — they
  never change what the bytes look like.

The same bytes serve every use: a file on disk, a blob in memory, a
payload handed from a tool to a game. Save and load are symmetric over
caller-provided buffers; generated code allocates nothing.

## 4. Versioning is wire tolerance

There are no version declarations — no fences, no version numbers, no
retained lineage. **The wire itself is evolution-tolerant**, and that
tolerance is the versioning model:

- **Unknown field** (newer writer): skipped by its length, counted.
- **Absent field** (older writer): the reader's value takes the field's
  default — the specified default, else zero.
- **Kind mismatch** (a field changed type between builds): skipped, never
  misdecoded, counted.
- **Out-of-range value** (bounds tightened since the writer): clamped to
  the reader's declared bounds, counted.
- **Framing damage**: decode stops, the partial result stands, and the
  report says malformed.

Every event lands in the **read report** — unknown, kind_mismatch,
clamped, malformed. Silence (all zero) means the data matched this
reader's schema exactly. Tools surface the report; games decide their own
policy over it. Nothing crashes on data from a different schema version,
in either direction, and that property is held by a both-directions
evolution test in the corpus.

Fields may be added anywhere, removed freely, and reordered freely —
identity is the name, not the position. The one edit the wire cannot
survive alone is a rename, and §5 closes it.

## 5. Identity: the name hash, `was`, and the collision refusal

A field's wire id is the hash of its name. Two consequences, both handled
at compile time:

- **Renames are refused unless declared.** A bare rename would silently
  orphan every byte ever written under the old name — the field would
  quietly default forever. The compiler retains nothing and cannot see
  renames on its own; instead, the `was` attribute declares one:

  ```
  speed float32 | was = "velocity"
  ```

  The field's wire id is the hash of the OLD name, so identity survives
  the rename and old data loads into the new field. `was` naming the
  field's own name is refused; `was` outside a table body is refused
  (renaming a `type` field is free — positional wire, SPEC §3.1).
- **Id collisions are refused.** Two fields of one table whose ids
  collide — by hash accident, or a `was` colliding with a live field —
  are a compile error naming both fields. This is the failure hand-rolled
  tag systems cannot see; the compiler sees every id in the closure and
  refuses once, forever.

## 6. Reflection

For every table in the unit's closure the generated header carries static
descriptors: field name, type name, wire id and kind, storage offset and
element size, array bound and count-companion offset, declared bounds,
enum max and a value→name function, branch guards, and the nested table's
descriptor. `TableType<X>()` returns X's descriptor. This is the surface
editors and tools build on — walk properties by name, print a value, diff
two, bind a property grid — with no RTTI and no schema files at runtime.

## 7. Relocatable, and built wide

Two properties are load-bearing for real content pipelines, and both are
held by construction:

- **Relocatable.** Generated table structs are trivially copyable and
  standard-layout — nesting by value, bounded arrays inline with their
  count companions, no pointers anywhere. A loaded value is one
  memcpy-able region. The generated header enforces this with static
  asserts, so a change that breaks relocatability breaks the build.
- **Parallel generation.** Encoding splits into **measure** and **save**:
  measure computes a value's exact encoded size writing nothing; save
  writes into a caller-provided buffer. Because nested tables are
  length-prefixed, a build can measure subtables from N workers,
  prefix-sum the offsets, and scatter-write disjoint ranges in parallel —
  and a reader can fan nested-table decodes out the same way. The framing
  guarantees the option; callers choose whether to take it.

## 8. Independence from the packet wire

Table declarations do not enter the unit's wire-shape projection. Adding,
editing or deleting a table moves no `ProtocolId` and no generated packet
byte: peers whose packets did not change are never forced into a lockstep
redeploy by a content edit. This independence is held by test.

## 9. Refused by name

- `table` bodies containing the 128-bit family (§2 — no wire kind).
- Recursive nesting (§2 — the cycle is named).
- A bare rename hazard: `was` naming the field's own name (§5).
- Id collisions, hash or `was`-induced (§5).
- `was` outside a table body (§5).
- Tables under any backend but C++ (status, above) — refused with the
  follow-on named, never silently ignored.

## 10. The expressiveness gate

The feature's acceptance test: a real game's binary config and asset
archive formats — a root table of nested collections of typed records,
built by tools, loaded by the game — must be expressible as declared
tables with nothing left over, without schema prescribing any of their
structure. The corpus carries a config-format example holding that gate.

## 11. Rulings, recorded

Owner rulings, 2026-09-01, in the order given. The fenced/append-only
draft this document previously carried is dead; these replace it.

- **The model**: "wire itself being evolution tolerant is what I thought
  versioning was … when you consider the wire also being save/load to
  memory/disk or whatever."
- **The purpose**: "tables are for data, tools, version evolving
  structures from tools to game, editor where you need to walk properties
  by name and print" — the game's binary config and asset formats
  "generalized … so that we can put them in schema, and people can create
  their own formats with them."
- **Self-contained**: no document concept at root — "you can have a root
  table, that has subtables … boom. you've made your own Config.bin."
- **Neutral wire**: no lock-in to schema's types or "our own packing
  methods."
- **The two contracts**: "Flatbuffers and protobufs ideas can apply here
  to tables, but they don't apply to types (wire hardcoded only, protocol
  id). The difference is that when building data structures with tables
  they must be able to go wide (multithreaded) in generating the data
  structure, and must be relocatable."

## 12. Named follow-ons

- Per-language backends beyond C++ (the refusal in §9 names this).
- A generic dump/diff tool over the reflection surface.
- Keyed lookup conveniences over loaded collections (library-side, never
  stored semantics).
- 128-bit table-wire kinds, if a need ever materializes.
