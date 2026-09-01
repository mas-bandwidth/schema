# schema — tables

**Schema has two wires: types and tables.** Types are the hardcoded,
same-build wire; tables are the evolution-tolerant one. This document
specifies the second.

Tables are schema's declarations for **data that crosses builds**: config
files, asset archives, tool output, editor state — and just as much,
**messages**: bytes produced by one build of one program and consumed by
another build of another, whether the trip is a disk file read years later
or a datagram read milliseconds later by a peer that deployed last week.
The hardcoded wire (`type`, SPEC.md) is the same-build contract, guarded by
the protocol id: same-or-refuse. Packets are its loudest user, not its
definition — any data whose writer and reader share a schema build belongs
there. The table wire is the opposite contract: **any reader reads any
data**, and the differences are reported, never fatal.

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
table Physics
{
    mass float32 | min = 0.1, max = 100000.0
    drag float32
}

table ShipConfig
{
    name        string(64)
    class       ShipClass
    health      int32 | min = 0, max = MaxHealth
    hardpoints  [..8]Hardpoint
    physics     Physics
}
```

A table body is a type body — the field grammar of SPEC §4.2, hosted by
`table`: bare and ranged integers, `bits(N)`, `bool`, floats and
compressed floats, enums, flags, strings, bytes, bounded arrays, unions,
`if` branches, and declared types as field groups. Two additions:

- **Tables nest.** A named table is a field type (above); nesting is by
  value, and a bounded array of tables is a collection. A table may not
  contain itself, directly or through any chain — recursion is refused
  with the cycle named. (Inline anonymous subtables are a spelling
  follow-on; the named form is the feature.)
- **`was` — the rename attribute** (§5).

The exclusions, each refused by name: `fixed`/`ufixed` and the 128-bit
family have no table-wire kind; `const`/`reserved`/`align` describe bit
positions, and the table wire has none; arrays of unions are a named
follow-on; and string/bytes/array extents past 65535 exceed the wire's
u16 lengths and counts (§3).

## 3. The wire

**The wire is neutral.** It carries none of schema's packing opinions — no
bitpacking, no range compression, no back-referenced branches. It is the
encoding a third party could implement from this section alone, without
schema's codebase:

- Little-endian, byte-oriented throughout.
- A table value is a sequence of **fields**, each `id (u16), kind (u8),
  payload`, terminated by a **u16 zero terminator**. The id is the
  field's name hash — fnv1a32 over the name, xor-folded to 16 bits, with
  0 mapping to 1 so the terminator can never collide (§5). The kind is
  one of the closed set: bool, i8..i64, u8..u64, f32, f64, string,
  array, union, table.
- Scalar payloads are their kind's fixed width. Strings carry a **u16
  byte length** then the bytes. Arrays, unions and nested tables carry a
  **u32 byte length** then the body — so any reader can skip any field
  without understanding it, and any parent can hand each nested table to
  a different worker (§7). An array body opens with its **element kind
  (u8) and count (u16)**; a union body opens with its **tag byte**
  (variant ordinal + 1, 0 = empty) then the arm as a nested table body.
  Fixed-extent scalar arrays are positional: absent trailing elements
  pad to the declared bound.
- **Writers elide what readers default**: a field holding its default, an
  empty string or array, and an all-default nested table are not written
  at all (fixed arrays of tables keep their elements — position is
  identity there). Elision is why old readers and new writers meet
  cleanly, and why measure and save agree byte for byte (§7).
- Schema's declaration-side types map onto the neutral kinds: a ranged
  integer rides as its storage-width integer kind, `bits(N)` as the
  narrowest unsigned kind that holds it, compressed floats as f32, enums
  and flags as their unsigned storage. The bounds, resolutions and enum
  vocabularies stay on the DECLARATION side, where they validate and
  clamp on load (§4) — they never change what the bytes look like.

The same bytes serve every use: a file on disk, a blob in memory, a
payload handed from a tool to a game, a message between services whose
deploys never align. Save and load are symmetric over caller-provided
buffers — message-ready by construction; generated code allocates
nothing.

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
- **Framing damage**: decode stops the damaged nesting level, keeps what
  it decoded there, flags malformed, and the parent continues past the
  field's declared length — one bad subtable never takes down the rest
  of the file. Array elements decode **inside their field's body
  bounds**: a count the body cannot cover yields the bounded prefix and
  the malformed flag, never values fabricated from a neighbor's bytes.

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

- **Relocatable, where possible.** Generated table structs are trivially
  copyable and standard-layout — nesting by value, bounded arrays inline
  with their count companions, no pointers anywhere. A loaded value is
  one memcpy-able region, and the generated header enforces it with
  static asserts. The owner's ruling holds this as a goal, not an
  absolute: a construct that genuinely cannot be relocatable is flagged
  and decided, never contorted around.
- **Parallel generation.** Encoding splits into **measure** and **save**:
  measure computes a value's exact encoded size writing nothing; save
  writes into a caller-provided buffer. Because nested tables are
  length-prefixed, a build can measure subtables from N workers,
  prefix-sum the offsets, and scatter-write disjoint ranges in parallel —
  and a reader can fan nested-table decodes out the same way. The framing
  guarantees the option; callers choose whether to take it.

## 8. Independence from the hardcoded wire

Table declarations do not enter the unit's wire-shape projection. Adding,
editing or deleting a table moves no `ProtocolId` and no generated `type`
byte: peers whose hardcoded wire did not change are never forced into a
lockstep redeploy by a table edit. This independence is held by test.

## 9. Refused by name

- `table` bodies containing `fixed`/`ufixed` or the 128-bit family (§2 —
  no wire kind), `const`/`reserved`/`align` (§2 — no bit positions),
  arrays of unions (§2 — named follow-on), or extents past 65535 (§2 —
  u16 lengths).
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
- **The governing statement**: "The end goal is to provide opinion free
  (as much as possible) options to let people author simple version safe
  tables, or even whole data structures loaded from tools into the game
  and vice versa. Schema is to be an un-opinionated library … able to
  express the data structures that we create … for Config.bin and
  Assets.bin as a requirement, but not enforce any packing structure, or
  our design calls in those bin files … no hard-enforced opinions,
  except what is needed to make versioning safe, multi-threaded generate
  work, and relocatability work (if possible)."

## 12. Named follow-ons

- Per-language backends beyond C++ (the refusal in §9 names this).
- A generic dump/diff tool over the reflection surface.
- Keyed lookup conveniences over loaded collections (library-side, never
  stored semantics).
- Arrays of unions in table bodies.
- `fixed` and 128-bit table-wire kinds, if a need ever materializes.
- A field-level name-mapping attribute for text-format tooling (the
  JSON-authoring surface real pipelines pair with binary tables) —
  tool-side, never wire.
