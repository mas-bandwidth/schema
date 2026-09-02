# schema — tables

**Schema has two wires: types and tables — and the types wire leads.**
Types are serialized bitpacked, versioned by the protocol id: the
hardcoded, same-build wire that is the key feature of the library. Tables
are for when you need things that version — more flexible structures with
in-wire versioning; data structures, if you will. The founding line for
the split: types remain VALUE semantics; tables ALLOW POINTER semantics —
"we can't be a generic system if we don't have pointers to tables." The
compiler DERIVES two classes of table from that freedom, and nobody
declares which: FIXED-SIZE (no pointer anywhere in the by-value closure —
a plain struct of known `sizeof`, saved and loaded by value) and
VARIABLE-LENGTH (a pointer anywhere in that closure — built through an
arena, read through a region and a root pointer, never held by value).
A fixed-size table pays for none of the variable-length machinery: that
property is a gate, held by test, not an intention (§2.2).

Tables are schema's declarations for **data that outlives the build that
wrote it**: config files, asset archives, tool output, editor state,
**save games** — a file written by a build nobody has any more and read by
a build its writer never saw, years apart — and just as much, **messages**:
bytes produced by one build of one program and consumed by another build of
another, whether the trip is a disk file read years later or a datagram
read milliseconds later by a peer that deployed last week. Save games and
tool output are one case, not two, and the whole of this document serves
it. The hardcoded wire (`type`, SPEC.md) is the same-build contract,
guarded by the protocol id: same-or-refuse. Packets are its loudest user,
not its definition — any data whose writer and reader share a schema build
belongs there. The table wire is the opposite contract: **any reader reads
any data**, and the differences are reported, never fatal.

The two contracts never mix. Flatbuffers- and protobuf-class evolution
ideas apply here, to tables — they do not apply to `type`, whose wire is
hardcoded under the protocol id, and nothing in this document changes that.

## The performance ladder

**Tables are LESS performant than types**, and that is the trade they exist
to make: types buy speed with a hardcoded, same-build wire; tables buy
tolerance, reflection and evolution with a neutral byte wire, length
prefixes and — in one narrow case — an allocation. A chosen cost is
licensed. Unexplained slowness is still a defect, in every rung.

- **A type is a raw struct.** Its generated storage IS the struct a person
  would have written, and its codec is measured against a hand-written
  serialize of that struct. The fastest-correct mission applies to types
  without qualification, and the standing ledger holds them to it.
- **A fixed-size table is a raw struct with a tolerant byte codec around
  it**, and is expected to run as close to its equivalent type as the
  neutral wire allows. That is a bench obligation, not an intention: a
  fixed table sits beside its equivalent type on the ledger, and the gap
  is explained or closed. The zero-cost gate (§2.2) holds the storage
  half of it; the bench leg holds the codec half.
- **The variable-length class pays for exactly what it buys** — pointers,
  an arena on the building side, a region on the reading side, a
  reference indirection per edge. This is where "less performant" lives,
  and it is deliberate.

## What allocates, and what never does

- **A type never allocates**, in any backend, on any path. This rung does
  not move.
- **A fixed-size table with no union allocates nowhere, in any backend.**
  It is a struct; measure, save and load work over caller-owned buffers.
- **A fixed-size table WITH a union may allocate for the arm, and only
  where the LANGUAGE needs it.** The carve-out is keyed on the backend
  language, never on the table's class: a backend with no native union
  builds a pseudo-union — one reference per arm, the set arm non-null,
  allocated on read and write — and a backend with a native union (C++)
  allocates nothing for the same declaration.
- **The variable-length class allocates by nature**, and assuming
  otherwise is foolish: the arena on build, the region on load. C++ keeps
  the caller-owned form — `LoadMeasure` sizes the region and the caller
  supplies it, `Builder` grows storage the caller owns — which is
  allocation with the caller holding the pointer, not its absence. Other
  backends allocate inside their runtime and say so. No port contorts
  itself toward zero allocation for a variable-length table.

**Backend status: C++, and C# for the FIXED class.** C++ carries both classes;
C# carries the fixed class (§6.1) — optionals, enum-keyed arrays and all — and
refuses a unit whose closure declares a pointer, naming its variable class as a
follow-on. Every other backend refuses a unit that declares tables at all, by
name, with this document cited. The remaining per-language backends are named
follow-ons (§15).

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
`if` branches, and declared types as field groups. Six additions:

- **Tables nest.** A named table is a field type (above); nesting is by
  value, and a bounded array of tables is a collection. A table may not
  contain itself BY VALUE, directly or through any chain — that recursion
  is refused with the cycle named, because a by-value cycle has infinite
  size. (Inline anonymous subtables are a spelling follow-on; the named
  form is the feature.)
- **Tables point** (§2.1). `next *Node` declares a POINTER to a table.
  Recursion THROUGH a pointer edge is legal and expected — the by-value
  cycle rule exempts pointer edges, because a pointer carries no size.
- **Fields may be OPTIONAL** (§2.3). `settings ?GunnerSettings` is present
  or absent by value, with no pointer and no change of mode.
- **An array may be ENUM-KEYED** (§2.4). `ships [ShipType]ShipConfig` has
  exactly one slot per variant, indexed by the variant.
- **A blob is a node** (§2.5). `data *bytes` declares an unbounded byte
  buffer at exactly its used size, and `*string` is its sibling.
- **A union arm may be a table** (§2.6), which is what makes an evolvable
  message set expressible.
- **`was` — the rename attribute** (§5).

### 2.1 Pointers

```
table Node
{
    value int32
    next  *Node          // a pointer: legal recursion, finite size
}

table Scene
{
    head    *Node
    palette *Palette     // shared and large: one copy, pointed at
}
```

**A pointer is for recursion, sharing and size — not for optionality.**
Every field on this wire is already optional: absence is the reader's
default (§4), so nothing has to be pointed at to be left out. The spelling
for an OPTIONAL SECTION is `?T` (§2.3):

```
table Scene
{
    settings ?Settings   // an optional section: off the wire when it is absent
}
```

No pointer, no allocation and no change of mode. Reach for `*` when
the structure genuinely needs it: a table that refers to ITSELF (a chain, a
tree), a large subtree you would rather not carry by value, or one node
several parents name (sharing is a named follow-on, §15 — v1 writes a
tree). One pointer anywhere in a table's by-value closure flips it to
VARIABLE-LENGTH (§2.2) and with it the whole builder lifecycle, so the
choice is a real one.

The spelling is C's, deliberately: it reads as what it is. The rules,
each refused by name (§11):

- **A pointer targets a `table`, or one of the two buffer primitives.**
  `*Node` names a declared table; `*bytes` and `*string` name an unbounded
  buffer at its used size (§2.5). Everything else is refused — `*SomeType`,
  `*SomeEnum`, `*SomeUnion` — because value-semantics data has no
  independent identity to point at. Nest it by value instead.
- **A pointer is declared inside a table body, and nowhere else.** A
  `type` body refuses one: types remain value semantics, and that is the
  founding line of the split.
- **An array of pointers is a named follow-on** (§15). Declare a bounded
  array of tables by value, or a pointer to a table that holds the array.
- **A pointer field takes no specified default.** A fresh pointer is
  null, and null is the only value a default could name.

A pointer's STORAGE is a four-byte relocatable reference — never a
machine address — which is what keeps §9's relocatability true with
pointers in the struct. Its meaning depends on the form it sits in
(§6.3).

### 2.2 The mode is derived, never declared

The compiler works out which class a table belongs to; the schema never
says. The rule is a least-fixed-point over BY-VALUE edges:

- A table is **VARIABLE-LENGTH** if it declares a pointer — including
  `*bytes` and `*string` (§2.5) — or if anything it nests by value is
  variable-length. "Nests by value" reaches through every by-value edge
  there is: a plain nested table, an element of a bounded array, an
  element of an enum-keyed array, a member of a guarded (`if`) group, an
  optional's value (§2.3), and a UNION ARM that is a table (§2.6).
- Every other table is **FIXED-SIZE**.

Pointer edges do not propagate the mode: a table that is merely POINTED
AT stays fixed-size if it holds no pointer of its own. It gains an
allocation and a resolution entry, and nothing else.

**A fixed-size table pays nothing for the VARIABLE-LENGTH machinery**, and
that is a gate, not a hope: in a unit whose tables are all fixed-size the
generated output carries no builder, no arena, no reference type, no
lifecycle surface, not one descriptor column that exists to describe a
pointer, not one branch in a codec that exists to follow one, and not one
extra `#include`. The build fails if a single symbol of the pointer
machinery appears in a pointer-free unit's generated header.

The gate is scoped to that machinery and to nothing else. A LANGUAGE
FEATURE the fixed class itself can use — an optional's presence companion,
an enum-keyed array's key columns, the wire id a variant rides under — is
emitted in every unit, whatever its mode, because a fixed-size table can
declare it and a tool walking a fixed-size table has to find it. The gate
asks "did the pointer world leak in?", never "did the descriptor grow?".

The assumption behind the split, stated so nobody quietly designs against
it: size and mode correlate in practice. Value-only tables are assumed
SMALL — messages, records, config fragments — so they are passed by
struct and loaded directly, and no large-flat-struct machinery is
warranted for them without a real case forcing it. Pointer-bearing tables
are where size lives, and the arena and the region are the size answer.

The exclusions, each refused by name: `fixed`/`ufixed` and the 128-bit
family have no table-wire kind; `const`/`reserved`/`align` describe bit
positions, and the table wire has none; and arrays of unions are a named
follow-on. **Extents have no wire ceiling**: lengths and counts ride as u32
(§3), so the only limit is the language's own — a string, bytes or array
extent lives in int32 storage (SPEC §4.3), and that cap is what a too-large
extent is refused against.

### 2.3 Optional fields: `settings ?GunnerSettings`

```
table ShipConfig
{
    health   float32 = 100.0
    settings ?GunnerSettings   // present or absent, by value
    tier     ?int32            // scalars too: present, and the value
}
```

`?T` declares a field that is PRESENT or ABSENT. Its storage is the value
plus a generated `<field>_present` bool, so the holder stays FIXED-SIZE:
an optional costs one bool, one branch and no allocation, and a table of
optionals is still a plain struct (§2.2's gate is untouched).

**PRESENCE decides whether the field rides, never content.** An absent
optional is not written; a present one is ALWAYS written, even when its
value is entirely default — the pointer's rule (§3.1), for the same
reason: otherwise "absent" and "present with nothing to say" would be one
value on the wire. On load, a field that rode sets `present = true`; a
field that did not leaves `present = false` with the value at its declared
defaults.

The framing is exactly the non-optional field's, which makes `?T`, `*T`
and a plain `T` nesting **one framing with three spellings**: for any value
that is not entirely default, a schema may move a field among the three and
no byte moves, in either direction.

**The one difference is at the empty end**, and it follows from the two
elision rules above: a plain `T` holding nothing but defaults is not
written at all, while a PRESENT `?T` and a non-null `*T` are written even
then. No direction misdecodes — an absent field reads as the declared
default by value, as null through a pointer, and as absent through an
optional, each of which is the right answer — but the bytes are not
identical for an all-default value, and an implementer should not be
promised that they are. The corpus holds all six directions, and the
byte-identity of the three writers **over non-default content**.

`?` applies to a nested table, a nested `type`, an enum, a `flags` mask
and any scalar. It is refused, by name, on:

- **a `type` body** — a type's wire is positional and every field always
  rides, so there is no absence to express;
- **a pointer** — a pointer is already optional, and null rides exactly
  as an absent optional does;
- **a union** — its `None` arm IS the absence, and an empty union already
  elides;
- **an array, a string or `bytes`** — each already carries a count or
  length whose zero is emptiness, and a second presence bit beside it
  would be two answers to one question. Wrap it in a table and make that
  optional; the general form is a named follow-on (§15).
- **a specified default** — presence is the only default an optional has.

### 2.4 Enum-keyed arrays: `ships [ShipType]ShipConfig`

```
enum ShipType { Fighter, Bomber, Scout }

table Fleet
{
    ships [ShipType]ShipConfig   // one slot per variant, indexed by the variant
}
```

An array bound that NAMES A DECLARED ENUM is an enum-keyed array. The two
spellings never overlap, because an enum is declared: `[Name]` naming a
constant is the fixed array it has always been, and `[Name]` naming an
enum is keyed.

- **Storage** is a generated KEYED-ARRAY TYPE wrapping `T slots[E.Max + 1]`
  — one slot per variant, indexed by the enum value, no count companion
  because every slot exists. The wrapper is what gives the accessors below
  a home; the array inside it is an ordinary array, so the type stays
  trivially copyable and standard-layout (§9).
- **SLOT 0 EXISTS AND IS NEVER VALID.** `None` is the enum's null, so it
  keys nothing: only `E.Max` slots can ever hold data. Slot 0 is kept in
  storage so indexing stays unbiased, and reaching it is an ERROR. The two
  accessors put that error as early as each can:
  - `operator[]( E )` takes a runtime key and ASSERTS that it is not
    `None`;
  - a compile-time accessor beside it — `Slot<Key>()` in C++ — takes the
    key as a constant and REFUSES `None` with a `static_assert`, so the
    common case where the key is written literally never reaches a runtime
    check at all.

  ```cpp
  fleet.ships[ ShipType::Bomber ]        // runtime key: asserts key != None
  fleet.ships.Slot<ShipType::Bomber>()   // constant key: static_asserts
  ```

  A port provides both wherever its language expresses both, and the
  runtime half wherever it does not. **The compile-time accessor is the
  form that holds in a release build**: an assert is compiled out under
  `NDEBUG` and its equivalent elsewhere, so a runtime key carries no guard
  at all in the configuration a shipped game runs. `Slot<Key>()` costs
  nothing and cannot be disabled — prefer it wherever the key is written
  literally, which is most places. The wire enforces the rule from the
  other side regardless: a `None` key never rides (§3.2).
- **In a `type` body the same spelling is a PLAIN ARRAY.** `per_team [Team]int32`
  inside a `type` generates `int32_t per_team[4]` — no wrapper, no keyed
  accessor, and no `None` guard of any kind. The type wire is positional
  (below), so there is no key to check and nothing to protect: slot 0 is an
  ordinary element a `type` may read and write. **The guard exists only
  where the keyed storage type is generated, which is table bodies**, and
  only the TABLE-wire ENCODING is keyed. A porter reading this section
  should not emit the wrapper for a `type`.
- **On the TABLE wire the slots ride by NAME** (§3.2): the body carries
  `(variant id, element)` pairs, so inserting, removing or reordering
  variants leaves every surviving slot in its own home. This is the whole
  point of the construct: an ordinal-indexed array is the last positional
  vocabulary the table wire had, and it failed silently.
- **On the TYPE wire the spelling IS `[E.Max + 1]T`** — positional,
  bitpacked, same-build, the protocol id moving exactly as that spelling
  moves it. The two spellings project identically and share one protocol
  id, and that is held by test.
- **Fixed-size when `T` is**, so the zero-cost gate holds.

**The two spellings are ONE FIELD on the type wire and TWO DIFFERENT
ENCODINGS on the table wire.** `[E]T` is kind `16` and `[E.Max + 1]T` is
kind `14` (§3), so changing a TABLE field from one spelling to the other
is a wire break, not a refactor: an old file read under the new spelling
is a kind mismatch — skipped, counted, never misdecoded (§4) — and the
committed baseline refuses the edit at compile time (§18.2). Giving the
keyed body its own kind is what buys that; the two encodings are not
mutually decodable and nothing should let them be tried.

**A KEY ENUM IS IN THE TABLE CLOSURE'S VOCABULARY**, and the closure's
rules reach it through the keying field. An enum that a table closure
reaches ONLY as an array key — never as a field type — still rides by
variant name on this wire (§3.2), so it takes the closure's two vocabulary
refusals (§5): **`| max = K` headroom is refused**, because a headroom
value is reserved by number and named by nothing, and **variant id
collisions are refused**, naming both variants. Both diagnostics name the
KEYING FIELD as the edge that pulled the enum in, because that edge is the
only reason the rule applies and a person looking at the enum alone would
not otherwise see it.

Refused by name: a bound naming a `flags` declaration (a mask holds any
set of bits at once, so it names no single slot); a bounded keyed array,
`[..E]` or `[A..E]` (a keyed array is COMPLETE by construction); an
element that is a pointer, as for any array (§15); and an index of
`E::None`, which names no slot.

### 2.5 Byte buffers: `data *bytes`

```
table Asset
{
    format  AssetFormat
    data    *bytes        // an unbounded blob, stored at exactly its used size
    label   *string       // the sibling: a string node at its used length
}
```

`bytes(N)` is fixed inline storage of N bytes in every instance. `*bytes`
is a POINTER to a byte buffer that has no declared bound and occupies
exactly the size it is given. The distinction is what makes large content
expressible: in a table of a million nodes, a `bytes(65536)` field costs
64 KB per node whether it is used or not, because the region packs storage
verbatim, while a `*bytes` field costs the four-byte reference plus what
each node actually holds.

- **Storage** is the four-byte relocatable reference (§2.1) beside a u32
  used length. Declaring one makes the holder VARIABLE-LENGTH (§2.2), like
  any other pointer.
- **In the arena**, `builder.AllocBytes( n )` returns a node of exactly `n`
  bytes; `builder.AllocString( n )` is the sibling.
- **In a region** it is packed at its used length, its reference
  self-relative like every other (§6.3).
- **On the wire it is framed exactly as `bytes(N)` is** — kind `14`, an
  array of element kind `6` (u8) at its used count — and `*string` is
  framed exactly as `string(N)` is, kind `12`. No new kind, no new skip
  rule, and one useful consequence: `bytes(N)` and `*bytes` share one
  framing, as `T`, `?T` and `*T` do (§3.1), so a field that outgrows its
  inline bound moves to a blob and no byte changes **for any non-empty
  value**. The one difference is the empty end, the same asymmetry the
  other family has: an empty `bytes(N)` elides, while a non-null
  ZERO-LENGTH `*bytes` writes an empty payload, because presence decides
  for a pointer-shaped spelling. A null blob is absent, exactly as a null
  pointer is.
- **In the cooked form**, `Open`'s walk bounds the blob by its length as it
  bounds every node, so a pointer into a mapped file IS the asset: no copy,
  no parse (§7).

Two sentences that must travel together: **a tolerant wire load COPIES the
blob** — a gigabyte on the wire path is a gigabyte read — and **the cooked
path is the zero-copy one.** Both are true and neither is the whole story.

schema does not interpret the bytes. A format tag beside the blob is an
ordinary field the user declares. A pattern falls out and is worth naming
though it is no construct: a sub-document, or a rarely needed arm, can be
stored as its own wire bytes inside a `*bytes` and decoded only when
something asks for it — which keeps a very large file's resident memory
proportional to what is touched rather than to what exists.

Refused by name: `*bytes` or `*string` outside a table body; a specified
default on one (a fresh reference is null); an array of them, as for any
pointer (§15); `?` on one, because null already IS absence (§2.3).

### 2.6 Union arms may be tables

Inside a TABLE closure a union's arm may name a `table`, not only a
declared `type`:

```
table OpenDocument  { path string(256), line uint32 }
table SaveDocument  { path string(256), force bool   }

union ToolBody
{
    open OpenDocument
    save SaveDocument
}

table ToolMessage
{
    sequence uint32
    body     ToolBody
}
```

That is an evolvable MESSAGE SET by construction, and it is the case this
document's opening promises. Arms ride under their own name hashes (§5),
so adding a message is adding an arm; a reader that lacks the arm reads
the union as empty, skips the body by its length and counts `unknown`
(§4); arms may be removed and reordered freely. A message may therefore
carry a nested table, a collection, or anything else a table body holds —
which is what a message set of documents needs and what a `type`-only
payload could never express.

- **A union declared for the TYPE wire keeps refusing table arms.** Types
  are value semantics and their wire is positional; a table arm is a
  table-closure construct and is refused by name outside one.
- **Mode derivation runs through arms** (§2.2): an arm that is a
  variable-length table makes the union's holder variable-length. A union
  of fixed-size table arms leaves its holder fixed-size, and the zero-cost
  gate holds.
- **On the wire an arm's body is framed exactly as a nested table's is**
  (§3, kind `15`): `arm id (u16)`, then `L`, then `L` bytes of table body.
  A `type` arm and a `table` arm are the same three bytes of framing, so a
  reader needs no new rule and an arm may change between the two forms
  without the framing moving.
- **A backend without a native union may allocate for the arm** (the
  ladder, above): the carve-out is the language's, not the table's.

## 3. The wire

**The wire is neutral.** It carries none of schema's packing opinions — no
bitpacking, no range compression, no back-referenced branches. It is the
encoding a third party could implement from this section alone, without
schema's codebase:

- Little-endian, byte-oriented throughout.
- A table value is a sequence of **fields**, each `id (u16), kind (u8),
  payload`, terminated by a **u16 zero terminator**. The id is the
  field's name hash — fnv1a32 over the name, xor-folded to 16 bits, with
  0 mapping to 1 so the terminator can never collide (§5).
- **The kinds are a closed set**, and these are their numbers: `1` bool,
  `2` i8, `3` i16, `4` i32, `5` i64, `6` u8, `7` u16, `8` u32, `9` u64,
  `10` f32, `11` f64, `12` string, `13` table, `14` array, `15` union,
  `16` enum-keyed array.
- **Payloads**, one row per kind. `L` is a u32 byte length; `N` is a u32
  element count. Nothing is aligned and nothing is padded.

  | kind | payload |
  |---|---|
  | `1` bool | 1 byte, `0` or `1` |
  | `2`–`5` i8/i16/i32/i64 | 1/2/4/8 bytes, two's complement |
  | `6`–`9` u8/u16/u32/u64 | 1/2/4/8 bytes |
  | `10` f32, `11` f64 | 4/8 bytes, the IEEE-754 bit pattern |
  | `12` string | `L`, then `L` bytes. No terminator; no encoding imposed |
  | `13` table | `L`, then `L` bytes of table body (fields, then the u16 zero terminator) |
  | `14` array | `L`, then the array body: `element kind (u8)`, `N`, then the elements |
  | `15` union | `arm id (u16)`, and when it is not 0, `L` then `L` bytes of arm body |
  | `16` enum-keyed array | `L`, then the body: `element kind (u8)`, `N` = the number of SLOTS PRESENT, then N pairs of `variant id (u16)`, `L (u32)`, `L` bytes of element (§3.2) |

  **An array's ELEMENT KIND is part of its identity, not only its framing.**
  For kinds `14` and `16` a reader compares the element kind it declares
  against the one in the body and, when they differ, skips the field and
  counts a kind mismatch (§4) — exactly as it does for the field's own
  kind. Without that rule a `[3]int32` body would decode into a
  `[3]float32` field as three reinterpreted bit patterns, reported by
  nothing: the field-level silent-corruption class, one level down.

  **The union carries NO outer length** — its `arm id` sits where the other
  three containers put theirs, and the length that follows frames the arm
  alone. It is the one payload whose framing a skipper has to know (below).
  An arm's body is a table body whether the arm names a declared `type` or
  a `table` (§2.6): fields, then the u16 zero terminator, the same bytes a
  kind `13` payload carries.

  **Spellings that add no row, and the one way they differ.** A `?T`
  optional field is framed exactly as the non-optional `T` (§2.3); `*T` is
  framed exactly as a by-value `T` (§3.1); and `*bytes` and `*string` are
  framed exactly as `bytes(N)` and `string(N)` (§2.5). Each family is ONE
  FRAMING under several declaration spellings.

  **What differs inside a family is ELISION, and only at the empty end.**
  Content decides for the by-value spellings and presence decides for the
  pointer-shaped ones (above), so a by-value `T` at its defaults writes
  nothing while a present `?T` at its defaults writes its body, and an
  empty `bytes(N)` writes nothing while a non-null zero-length `*bytes`
  writes an empty payload. **For any content that is not entirely default,
  the spellings in a family are byte-identical**, and that is the scope of
  the claim: a schema may move a field among them and no byte moves for
  such a value. At the empty end the bytes differ and no reader
  misdecodes — an elided field reads as absent (`?T`), null (`*T`) or the
  declared default (`T`), which is correct in every direction.
  - **Array elements.** For a scalar element kind the elements sit back to
    back at that kind's fixed width. For element kind `13` (table) each
    element is preceded by its own `L`. `bytes(N)` rides as an array of
    element kind `6` (u8). A fixed-extent array writes all its declared
    elements, so a reader that decodes fewer than its own bound leaves the
    tail at its declared defaults.
  - **An arm id of 0 is the empty union** and carries nothing after it.
    This writer never emits it — an empty union elides (below) — but a
    reader accepts it.
- **Skipping a field you cannot name** needs the kind byte and nothing
  else, which is what makes an unknown field survivable (§4). Three rules
  cover the set: kinds `1`–`11` skip their fixed width; kinds `12`, `13`,
  `14` and `16` read `L` and skip `L` bytes; kind `15` reads the `u16` arm
  id and stops there if it is 0, else reads `L` and skips `L` bytes.
  **A kind a reader does not know at all is not skippable** and is framing
  damage, which is why the set is closed and why a new kind is a wire
  change rather than an addition.
- **Field ORDER within a body is not part of the contract.** This
  implementation writes fields in declaration order, and a reader must not
  rely on it: every field is found by its id, so any order decodes the same
  value, and a body carrying an id more than once is legal input — the last
  occurrence wins. An encoder written from this section is therefore free
  to order fields as it likes, and byte-identical output against this
  implementation requires matching its declaration order as well as its
  framing.
- **Writers elide what readers default**: a field holding its default, an
  empty string or array, an all-default FIXED array, an empty union and an
  all-default nested table are not written at all (fixed arrays of tables
  keep their elements — position is identity there; an ENUM-KEYED array
  elides per slot instead, because identity there is the key, §3.2).
  Elision is why old readers and new writers meet cleanly, and why measure
  and save agree byte for byte (§7). Elision makes the DECLARED DEFAULT
  part of the wire contract: see §4.
  **A field under a FALSE GUARD is elided too**, whatever its storage
  holds: an `if` branch that does not run writes none of its fields, so a
  guarded group rides only when its guard is true. That is what makes a
  guard an optional SECTION on the wire and not merely in the language, and
  the text form defers to this rule rather than restating it (§16.2).
  **PRESENCE, not content, decides the three pointer-shaped spellings.** An
  absent `?T`, a null `*T` and a null `*bytes` are not written; a present
  optional, a non-null pointer and a non-null blob are ALWAYS written, even
  when the value is entirely default and even when the blob is zero bytes
  long — otherwise "absent" and "present with nothing to say" would be one
  value on the wire (§2.3, §2.5, §3.1).
- Schema's declaration-side types map onto the neutral kinds: a ranged
  integer rides as its storage-width integer kind, `bits(N)` as the
  narrowest unsigned kind that holds it, compressed floats as f32, and a
  `flags` mask as `u64`. The bounds and resolutions stay on the
  DECLARATION side, where they validate and clamp on load (§4) — they
  never change what the bytes look like.
- **An enum value rides as u16 — kind `7` — carrying the hash of its
  VARIANT NAME**, folded exactly as a field id is, whatever the
  declaration-side storage width. The implicit `None` rides as `0`, the
  one reserved id, which the fold's rebound keeps free of every declared
  name. Variant identity by name is what makes §4's "add anywhere, remove
  freely, reorder freely" true of an enum's variants and a union's arms,
  and not only of a table's fields (§5).
- **A `flags` value rides as its raw unsigned storage — a u64 of bits, kind
  `9`.** A set of bits has no cheap name-identified form, so flags are the
  wire's one POSITIONAL vocabulary, and the rule that follows from it is in
  §4: variants are appended at the END, never inserted or reordered.

### 3.1 Pointers on the wire: a tree

A pointer rides as its pointee's table body, framed exactly as a
by-value nesting is: `id (u16), kind = table, u32 byte length, body`.
Three consequences, all deliberate:

- **A null pointer is not written at all.** Absence decodes as null, so
  an old reader that never knew the field is already correct, and a
  reader whose writer had nothing to point at is correct too.
- **A non-null pointer ALWAYS rides**, even when its pointee is entirely
  default. Elision is decided by presence, not by content — otherwise
  null and "points at an empty node" would be one value on the wire.
- **A pointer field, an OPTIONAL field and a by-value nesting share one
  framing** — three spellings of one field (§2.3). A schema may change any
  of them into any other and, for content that is not entirely default, no
  byte moves: old readers take the first body by value, pointer readers
  allocate one node from it, optional readers mark it present. **At the
  empty end they differ**, because content decides elision by value and
  presence decides it here: an all-default `T` writes nothing, a present
  `?T` and a non-null `*T` write a body. Every direction still reads
  correctly; only the byte-identity claim is scoped. The corpus holds all
  six directions, and the byte-identity of the three writers over
  non-default content.

**Wire v1 is a TREE, and identity is not preserved.** Two pointers to one
node write two bodies and load as two nodes. Aliasing, sharing and DAG
identity are a named follow-on (§15); nothing in v1 pretends otherwise,
and the corpus asserts the tree semantics rather than hiding them.

A pointer chain's nesting depth on the wire EQUALS its length: a chain of
N nodes is N levels deep. Both sides therefore cap nesting at a declared
depth (128 in the C++ backend).

**ONLY POINTER EDGES CHARGE DEPTH.** By-value nesting — a table inside a
table, or a bounded array of them — charges nothing, because by-value
composition is already finite: §2 refuses by-value cycles, so its nesting
is bounded by the schema and cannot be driven by data. The rule must hold
identically in ALL FOUR walks — measure, save, load and pack — or the
forms disagree about which structures are legal and a structure that
locks and cooks is refused by the wire. That agreement is held by test.

The cap in force:

- **Writing**, a graph deeper than the cap — including any data cycle —
  is a save ERROR. Measure and save return failure; nothing recurses
  away, and no cycle detection state is needed to guarantee it.
- **Reading**, a body nested past the cap is refused: the subtree is
  skipped, the pointer stays null, the read is flagged malformed, and the
  parent reads on. This is the framing-damage rule of §4 applied to
  depth.

Wide structures are unbounded; deep ones are capped. Lifting the cap
wants a flat, indexed node encoding — a named follow-on (§15).

### 3.2 Enum-keyed arrays on the wire: slots by name

An enum-keyed array (§2.4) rides as **kind `16`, its own kind**, and its
body opens as an array's does — `element kind (u8)`, then a u32 count.
Kind `16` exists so that the keyed and the positional spellings cannot be
mistaken for one another: they are different encodings, a reader meeting
the wrong one reports a kind mismatch instead of decoding a body under the
wrong rule, and no reader ever has to guess which layout a `14` carries.
What differs from a plain array is what the count counts and what each
element carries:

- **`N` is the number of SLOTS PRESENT**, not the array's extent. A slot
  whose element holds its default is elided, exactly as a defaulted field
  is, and an array with no present slot is not written at all. The upper
  bound on `N` is therefore `E.Max`, never `E.Max + 1`.
- **A `None` key NEVER RIDES.** `None` is the enum's null and keys no slot
  (§2.4), so slot 0 is not written whatever it holds, and **a stored key of
  `0` is MALFORMED** — not an unknown variant, because `0` is the reserved
  id no declared name can ever fold to (§5), so a body carrying one is
  damaged rather than merely foreign. The reader stops that body, keeps
  what it decoded, and flags malformed (§4).
- **Slots are written in ascending variant ordinal**, and a reader must
  not rely on it — the field-order rule (§3) one level down. Every slot is
  found by its key, so any order decodes the same value; byte-identical
  output against this implementation requires matching the ascending order
  as well as the framing.
- **Each element is a pair**: `variant id (u16)`, then `L (u32)`, then `L`
  bytes of element. The variant id is the key's name hash, folded exactly as
  a field id is (§5). The length rides for EVERY element kind, scalars
  included, so one rule skips an unknown key whatever the element is.
- **On load, each pair is placed by its variant id.** A key this reader can
  name lands in that variant's slot; a key it cannot name is skipped by its
  length and counted `unknown`; a slot the writer never sent keeps its
  declared default. Insert a variant, remove one, reorder them — every
  surviving slot still finds its home, in both directions.
- **A repeated key is legal input and the last occurrence wins**, the
  field-level rule (§3) applied inside the body.
- **A key with no name on the WRITING side is a save error**, not a silent
  `None`: measure and save return failure, exactly as they refuse an
  unnameable enum value or an out-of-range union tag (§5).

The contrast is the point. A plain `[E.Max + 1]T` array is POSITIONAL:
insert a variant in the middle and every later slot lands one place off,
with nothing on the wire to say so and no report event that could fire.
The keyed spelling costs `2 + 4` bytes per present slot and closes that
class. The corpus holds it with a middle insert and a removal in one
generation step, and the negative control — encoding the slots
positionally — turns the middle-insert test red.

**And the two spellings do not decode each other.** A `16` body read as a
`14`, or the reverse, would take keys for values and values for keys — the
same silent corruption in a different costume — so the distinct kind is
what makes changing spelling a REPORTED edit (§4) rather than a quiet one.
A reader meeting the other kind skips the field, counts `kind_mismatch`,
and leaves the array at its declared defaults.

The same bytes serve every use: a file on disk, a blob in memory, a
payload handed from a tool to a game, a message between services whose
deploys never align. Save and load are symmetric over caller-provided
buffers — message-ready by construction; generated code allocates nothing
on the reading path, with the two carve-outs the ladder names: the
variable-length class allocates by nature, and a union arm may allocate in
a backend whose language has no native union.

## 4. Versioning is wire tolerance

There are no version declarations — no fences, no version numbers, no
retained lineage. **The wire itself is evolution-tolerant**, and that
tolerance is the versioning model:

- **Unknown field** (newer writer): skipped by its length, counted.
- **Absent field** (older writer): the reader's value takes the field's
  default — the specified default, else zero.
- **Unknown enum variant, union arm or KEYED SLOT** (a name this reader
  does not have): the enum value reads as `None`, the union reads as empty,
  a keyed array's slot is dropped and the rest of its slots land normally,
  the body is skipped by its length, and the event is counted as
  **unknown** — the same counter an unknown field id uses, because it is
  the same event: the writer named something this reader cannot name.
- **A keyed slot this reader has but the writer never sent**: the slot
  keeps its declared default, exactly as an absent field does (§3.2).
- **An OPTIONAL field the writer did not send** reads as ABSENT, with the
  value at its declared defaults; one that rode reads as PRESENT, whatever
  the content (§2.3). A field moved between `?T`, `*T` and a plain nesting
  is not an evolution event at all — the bytes do not move (§3.1).
- **Kind mismatch** (a field changed type between builds): skipped, never
  misdecoded, counted. The kinds are a coarser vocabulary than the
  declaration side, so this catches a change of KIND, not every change of
  type: an enum field and a plain `uint16` field are both kind `7`, so an
  edit between the two is not a kind mismatch — the raw value is read as a
  variant hash, and lands on `None` unless it happens to name one. **An
  array changed between the keyed and the positional spelling IS a kind
  mismatch** (`16` against `14`, §3.2), which is exactly why the keyed body
  has a kind of its own — **and so is an array whose ELEMENT kind differs**,
  which is the same event one level in (§3): `[3]int32` read into a
  `[3]float32` field is skipped and counted, never reinterpreted.
- **A changed array BOUND** (a literal, a constant, or an `E.Max + 1`
  expression that moved): the array still loads, and the bound is not part
  of identity — a field is its name hash and its kind, and neither carries
  an extent. A count past the READER's bound keeps the bounded prefix and
  counts **clamped**; a count short of it fills what the writer sent and
  leaves the reader's tail at its declared defaults. `malformed` is
  reserved for a count the BODY cannot cover, which is framing damage and a
  different thing. The storage struct's size changes with the constant; the
  bytes do not.
- **Out-of-range value** (bounds tightened since the writer): clamped to
  the reader's declared bounds, counted.
- **A GUARD added or removed around an existing field**: the READ is
  faithful in both directions — a field is found by its id whatever branch
  now encloses it, so a reader whose build added `if g { x }` still loads
  `x` out of an old file, with every counter zero and nothing lost. What
  changes is the WRITE: that reader will elide `x` on its next save if `g`
  is false (§3), so a load-edit-store cycle drops a value the load itself
  read correctly. Adding or removing a guard is therefore not a decoding
  hazard but a ROUND-TRIP one, and it is the one edit whose cost lands on
  the tool that rewrites the file rather than on the one that reads it.
- **Framing damage**: decode stops the damaged nesting level, keeps what
  it decoded there, flags malformed, and the parent continues past the
  field's declared length — one bad subtable never takes down the rest
  of the file. Array elements decode **inside their field's body
  bounds**: a count the body cannot cover yields the bounded prefix and
  the malformed flag, never values fabricated from a neighbor's bytes.

Every event lands in the **read report**, whose counters are `unknown`,
`kind_mismatch`, `clamped`, `duplicate` and the `malformed` flag.
`unknown` counts every id this reader cannot name: a field id, an enum
variant id, a union arm id, a keyed slot's key. Silence (all zero) means
the data matched this reader's schema exactly. Tools surface the report;
games decide their own policy over it. Nothing crashes on data from a
different schema version, in either direction, and that property is held by
a both-directions evolution test in the corpus.

**`duplicate` is the TEXT FORM's counter and the wire never raises it**
(§16.2). It rides on the same report struct because a caller has one report
type, not two — so every backend carries the counter, and a wire read
always leaves it zero: a body carrying an id twice is legal input whose
last occurrence wins, silently, by §3.

Fields may be added anywhere, removed freely, and reordered freely —
identity is the name, not the position. **So may an enum's variants and a
union's arms** (§3, §5): they ride under their own name hashes, so
inserting `Silver` between `Bronze` and `Gold` leaves every stored `Gold`
reading back as `Gold`. The one edit the wire cannot survive alone is a
rename, and §5 closes it.

**Flags are the exception, and the rule for them is APPEND AT THE END.** A
flags value rides as its raw mask (§3), so a variant's identity is its BIT
POSITION. Inserting or reordering variants remaps every stored file
silently — nothing on the wire says the bits moved, and no report event can
fire:

```
flags Perks { Shielded, Cloaked, Turbo }          // v1: bits 0, 1, 2

flags Perks { Shielded, Hardened, Cloaked, Turbo } // v2: WRONG — an insert
// every stored Cloaked (bit 1) now reads as Hardened, and Turbo reads as
// Cloaked. The compiler retains no history and cannot see this.

flags Perks { Shielded, Cloaked, Turbo, Hardened } // v2: RIGHT — appended
// every stored bit keeps its meaning; an old reader ignores bit 3, a new
// reader sees it clear in old data.
```

Removing a flags variant frees no bit either: retire the name, keep the
position (a spent bit stays spent). The 64-bit storage is the ceiling.

**A DEFAULT IS PART OF THE WIRE CONTRACT.** A field holding its declared
default is elided (§3), so the reader's default is what the absence means —
and changing a default therefore rewrites the meaning of every file already
written. A v1 writer elided `damage = 21.0`; a v2 reader whose declaration
says `damage = 25.0` loads 25.0 out of that same file, and no report event
fires, because nothing was lost or skipped. **A default change is a
semantic edit to every stored file**, and `was` does not cover it: `was`
preserves an identity, not a value. Change a default the way you would
change data, or add a new field and leave the old one alone.

### 4.1 The silent class, in full

Almost every edit lands in the read report. **Exactly two do not**, and
naming the whole set is the point of this subsection — a person reading a
save game that came back wrong needs to know there is no third:

1. **A specified DEFAULT changed, added or removed.** An elided field means
   "the reader's declared default", so the same bytes now mean something
   else. Nothing was lost or skipped, so no counter can fire.
2. **A FLAGS variant inserted, removed, reordered, or renamed in place.** A
   mask is the wire's one positional vocabulary (§3), so a variant's
   identity is its bit position; moving one remaps every stored file and
   the wire carries nothing that could say so.

Everything else is either reported or safe. Fields may be added, removed,
reordered and renamed under `was`; enum variants and union arms may be
added anywhere, removed and reordered; array bounds may move; a field may
change between `T`, `?T` and `*T`, or between `bytes(N)` and `*bytes` —
all of it either invisible to the wire or counted in the report.

**Two edits that would otherwise be silent are made REPORTABLE by
construction, and it is worth saying how, because the claim above depends
on it.**

- **An enum-ordinal-indexed array** was the last positional vocabulary
  besides flags: insert a variant in the middle and every later slot lands
  one place off. `[E]T` (§2.4) closes it — keyed slots ride by name, so a
  middle insert moves no slot.
- **Changing a table field between `[E]T` and `[E.Max + 1]T`** would then
  have replaced it: two encodings under one kind would have let a reader
  decode keys as values and report nothing. The keyed body's own kind `16`
  (§3.2) turns that edit into a kind mismatch, counted like any other.

**One edit is adjacent to this class without belonging to it, and the
difference is worth stating rather than leaving to the reader.** Adding or
removing a GUARD around an existing field (§4) reads correctly in both
directions — the value comes back, the report is silent, and the silence is
honest, because nothing was lost on the way in. The loss, if it comes, is
on the way OUT: a reader whose guard is false elides the field on its next
save. So it is not a silent decoding edit, and the enumeration above stays
at two; it is a round-trip hazard, and a tool that loads, edits and stores
a file — the save-game cycle §18 exists for — should be read as carrying
it. A person whose file came back wrong needs the two above; a person whose
tool rewrote a file needs this one.

Each of the two real ones has its own answer:

- **Flags** are answered by DISCIPLINE, stated as law: **append at the end,
  never insert or reorder**, and retire a name in place rather than freeing
  its bit.
- **Both** are answered by MACHINERY, opt-in: the committed tables baseline
  (§18) is the history the compiler does not keep, and it refuses either
  edit until the baseline moves with a recorded reason. It refuses the
  spelling change above too, at compile time, ahead of the reader's report.

## 5. Identity: the name hash, `was`, and the collision refusal

**One hash serves three vocabularies.** A field's wire id, an enum
variant's id and a union arm's id are all `fold16(fnv1a32(name))` — the
fnv1a32 of the name, xored with its own high half and truncated to 16 bits,
with a result of 0 rebounding to 1. The rebound reserves `0`: it is the
field terminator, the enum's `None` and the union's empty arm, and no
declared name can ever land on it.

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

**Variants carry the same identity, and the same refusals.** An enum's
values and a union's arms ride under their own name hashes (§3), so:

- **Variants may be added anywhere, removed, and reordered** — the edit
  §4's field rule always allowed, now true of a vocabulary too. What a
  reader cannot name reads as `None` (enum) or empty (union), counted.
- **Renaming a variant is a wire change**, and there is no `was` for one:
  `was` is a field attribute. A renamed variant is a NEW variant, and old
  data carrying the old name reads as unknown. Rename a variant only when
  that is what you mean.
- **Two variants of one enum, or two arms of one union, whose ids collide
  are a compile error naming both** — the field rule, applied to the
  vocabulary. Scoped to the TABLE CLOSURE: the packet wire identifies a
  variant by its ordinal and is untouched.
- **A closure is reached by every edge, including an ARRAY KEY.** An enum
  that a table closure reaches only as an enum-keyed array's key (§2.4)
  rides by variant name exactly as a field-typed one does, so both rules
  above apply to it, and the diagnostic names the keying field as the edge
  that pulled it in.
- **`| max = K` headroom on an enum in a table closure is refused**, key
  enums included. A headroom value is reserved by number and named by
  nothing, so it has no identity to ride under — and the table wire needs no headroom, because a
  variant may be added anywhere. A stored value no variant names is
  likewise refused on the way out: measure and save return failure rather
  than writing `None` over it, exactly as they refuse an out-of-range union
  tag.
- **Flags are the exception**: a mask is positional, and §4's append-at-the-
  end rule is the whole of its evolution story.

**And a default is identity's companion on the wire** (§4): an elided field
means "the reader's declared default", so changing a default is a semantic
edit to every file ever written, and `was` does not cover it.

## 6. The two lives of a table

The mode (§2.2) decides the shape of the API, because the two classes are
genuinely different things at runtime and neither should be contorted to
look like the other.

### 6.1 Fixed-size: a value

A fixed-size table is a struct. Its whole surface is three free
functions, name first:

```cpp
ChatMessage msg;
int64_t size = ChatMessageMeasure( msg );          // exact wire bytes
ChatMessageSave( msg, buffer, size );
ChatMessageLoad( msg, buffer, size, &report );     // destination first
```

`sizeof( ChatMessage )` is the memory answer and `ChatMessageMeasure` is
the wire answer; schema generates no size constants of any kind, because
those two already exist.

### 6.2 Variable-length: a lifecycle

A file-format-scale structure is not a struct you copy. It is built
mutable, then locked const, and read through a region and a root pointer.
The state machine is MONOTONIC — there is no unlock:

```cpp
SceneBuilder builder;                        // MUTABLE: an arena
Scene * root = builder.GetRoot();
Node * n = builder.Alloc<Node>();            // usable as node AND as reference
root->head = builder.Alloc<Node>();
builder.Lock();                              // ONE WAY, and it is the compaction
const Scene * scene = builder.AsConst();       // CONST: one packed region
```

- **Builder** — a growable arena. `Alloc<T>()` hands back a node that is
  usable both as the pointer to write fields through and as the reference
  to store in a pointer field. Growth never invalidates anything already
  allocated (§6.4).
- **`Lock()`** — one way, and it IS the compaction: the arena is walked,
  measured exactly, and laid back to back into ONE region with zero
  slack, the root at its base. The mutable life is released. Locking
  twice is a no-op, and there is no unlock: re-editing means loading the
  const form into a fresh builder, which is a copy and says so.
- **Const** — one region, one root pointer. `Load` produces the same
  representation `Lock` does, so a locked structure and a loaded one are
  read through one view API.

Reading a pointer is `NodeAt( node->next )` — one add (§6.3), NULL when
the reference is null.

### 6.3 Two reference encodings, one slot

A pointer's four-byte slot means different things in the two forms, and
the form in hand always says which:

- **In the arena**, it is the node's arena offset.
- **In a region**, it is the SELF-RELATIVE byte delta from the slot's own
  address. A deref is therefore one add and needs no base pointer at all,
  and a whole region relocates by plain `memcpy` with zero fix-up — which
  is the strongest form §9's relocatability can take.

Lock and Load both produce the region encoding; Lock is already
rewriting layout, so the conversion is free.

Region references are always POSITIVE: a region is packed depth-first, so
a child always sits after the slot that names it. A packed region
therefore cannot contain a cycle, which is what lets §7's validation walk
be cycle-free with no visited set.

### 6.4 Building on many threads

The builder is designed to go wide, lock-free by ownership:

- Allocation is thread-local. Each worker owns its own front into the
  arena and allocates privately within it; synchronization happens once
  per large slab of arena, not once per node.
- Nothing ever moves. The arena grows by appending storage, never by
  relocating what exists, so a `T*` taken from `Alloc` stays valid for
  the arena's whole life while other workers allocate and while the arena
  grows, and a reference stays correct forever.
- **The contract, stated plainly.** Allocating on YOUR OWN worker is safe
  concurrently with any other worker's. Writing fields of a node another
  worker allocated is your own synchronization problem; this runtime does
  not arbitrate it. `Lock`, `Save`, `Cook` and `Open` are
  single-threaded — call them after the workers have joined. The
  reflection descriptors (§8) are immutable constant data and carry no
  first-use state, so reading them needs no synchronization at all.
- Slack is bounded and stated: one partial slab per worker, plus one per
  arena segment. That is the price of never synchronizing per node, and
  `Lock` removes all of it.

### 6.5 The two load paths

- **Into a region** — the game's path, for `Config.bin` and
  `Assets.bin`. `<Name>LoadMeasure( wire, size )` computes the EXACT
  region bytes from the wire's framing alone, reading no field values, so
  the caller owns the allocation; `<Name>Load( region, region_size, wire,
  wire_size, &report )` decodes into it and returns the root. The load
  path allocates nothing.
- **Into a builder** — the tool's path. The same tolerant decode into a
  fresh builder, so loaded data can be edited and locked again.

Contract split, stated once: the AUTHORING path (builder growth, `Lock`)
may allocate; the reading path allocates nothing of its own — the caller
owns the region, and `LoadMeasure` exists so it can.

**The one carve-out, and it belongs to the LANGUAGE, not to the table.** A
UNION inside a table may allocate per arm, on read and on write, in a
backend whose language has no native union: the arm becomes a pseudo-union
— one reference per arm, the set arm non-null, allocated when it is read
or written. C++ has a native union and allocates nothing for the same
declaration; a backend that has one must not allocate either. The rule is
stated here once and inherited by every port rather than rediscovered as a
per-backend accident, and it does not widen: the type wire allocates
nowhere, in any language, and a fixed-size table with no union allocates
nowhere either.

## 7. The cooked form

The wire (§3) is the generic form: it allocates, it walks, it parses, and
any build reads any data. That generality is the point of it, and it is
the format of record.

The COOKED form answers a different requirement: load a big file, point
at its root, without copying it and without parsing it. It is the
structure laid out exactly as the runtime reads it — `Lock`'s region,
written verbatim behind a small header.

```cpp
int64_t size = SceneCookMeasure( builder );
SceneCook( builder, buffer, size );          // write it

const Scene * scene = SceneOpen( bytes, size ); // point at it, or NULL
```

- **It is an accelerator, not an archive.** A cooked file is BUILD-LOCKED
  and regenerated whenever the schema moves. The tolerant wire remains
  the format of record; a cooked file is a cache beside it. Both
  sentences are load-bearing.
- **The header build-locks it**: a magic (which is also the byte-order
  check), a LAYOUT ID, and the region's length. The reserved words are
  reserved: a non-zero one means a writer used a form this build does not
  understand, and Open refuses rather than ignoring it.
  The layout id is a compile-time digest mixing the schema's
  packed-layout facts with this build's own `sizeof` AND `offsetof` for
  every type and field in the closure, so schema drift and ABI drift both
  refuse — `sizeof` alone cannot see a member that moved inside an
  unchanged total, and the packed form is read by offset. It is the
  region form's twin of the protocol id.

  **It keys a field by its WIRE ID, not its source name.** That is the
  identity `was` preserves (§5), and a `was` rename moves no byte — so it
  must not invalidate every cooked file in existence. Reordering two
  same-shaped fields does move bytes, and it moves which id sits at which
  offset, so it still refuses. Both directions are held by test.
- **Reads validate, always.** Before handing out the root, `Open` walks
  the REFERENCE GRAPH: every pointer slot, and the count companions that
  bound a traversal — including those of fixed-size tables and plain
  types nested by value, whose counts bound a walker just as a table's
  do. It reads no field value and decodes no payload; that is the
  distinction between validating before pointing and parsing the whole
  thing. There is no trust-mode bypass.
- **A member's walk lives in the file that DECLARES it.** A variable table
  may nest a plain type, a fixed table or another variable table declared
  anywhere in the unit, and the walk for each is emitted once, by its
  declaring file — including by a file that declares no variable table of
  its own, and for a member nothing points at. The referencing file picks it
  up through the header it already includes. Emitting per referencing file
  would define each walk twice; emitting only where pointers are declared
  leaves the by-value members of a value-only file undefined.
- **The walk is LINEAR IN THE REGION, and that takes a proof.** Bounds
  and forwardness alone do not give it: a forged file whose references
  alias forward — every node's second pointer aimed at its first child —
  is a legal-looking DAG that a stateless walk explores exponentially
  (measured on an earlier revision: 26 nodes, 312 ms; ~60 nodes, never).
  The walk therefore carries a HIGH-WATER MARK. Packing lays nodes out in
  pre-order by bump allocation and the walk visits them in the same
  order, so in a genuine region every reference lands at or past the end
  of everything visited so far; requiring that, and advancing the mark
  past each node, makes the walk consume region bytes monotonically. It
  visits each byte at most once, it terminates, and it also rules out the
  mid-node overlaps a range check alone admits. **The pack order and the
  walk order are one invariant and neither may change alone** — the
  generated code says so at both sites.
- **Every refusal is loud and the fallback is a real wire load**: wrong
  magic or byte order, a layout id this build did not produce, a
  truncated region, an unaligned base, a reference that leaves the
  region, a backward reference (impossible in a packed region), a
  misaligned reference, a count companion outside its declared bound.
- **Alignment.** The header pads the root to the region's alignment, so a
  base the allocator or `mmap` gave you is already aligned; `mmap` gives
  page alignment for free.
- **Endianness** is recorded and a mismatch REFUSES in v1. An in-place,
  descriptor-driven fix-up pass sharing `Open`'s traversal is a named
  follow-on (§15).

Prior art gets one sentence, and it is the contrast: systems that made
pointed-at access their ONLY wire coupled access to evolution and paid
for it. Here the tolerant wire stays the format of record and the cooked
form is a build-locked accelerator beside it. The two-form split is the
design.

## 8. Reflection

For every table in the unit's closure the generated header carries static
descriptors: field name, type name, wire id and kind, storage offset and
element size, array bound and count-companion offset, declared bounds,
branch guards, and the nested table's descriptor. `<Name>TableType()`
returns that table's descriptor.

**A type descriptor also carries a RESET hook** — put one instance back at
its declared defaults, in place. A generic walker that FILLS a value has to
be able to establish the defaults an absent field takes, and it holds no
type to spell; this is the one thing the columns cannot express without a
function. It does what the wire's read path does, with no temporary.

**A field carries its TEXT KEY** beside its name — the `json = "..."`
attribute's value, else the field's own name (§16.4) — so a walker over the
text form spells keys without a second table.

**A `bits(N)` field carries its IMPLIED RANGE.** `bits(N)` declares
`[0, 2^N − 1]` by its width rather than by an attribute, and the codec has
always clamped to it (§4); carrying it in the descriptor is what lets a
generic walker apply the same bound without re-deriving it from a type
name.

**An optional field carries its presence companion.** `optional` marks the
field and `present_offset` names the `<field>_present` bool, exactly as
`counted` and `count_offset` name a count companion — so a walker can read
and write presence without knowing the spelling that produced it, and can
tell "absent" from "present and default" (§2.3).

**An enum-keyed array carries its KEY's vocabulary.** `key_type_name` names
the keying enum, and `key_name` and `key_id` map a slot index to that
variant's name and to the wire id it rides under — so a tool prints
`ships[Bomber]` rather than `ships[2]`, with no schema files on hand. The
element's own vocabulary columns are unaffected: a keyed array OF enums
carries both (§2.4). A positional array leaves all three NULL.

**A vocabulary field carries its vocabulary and the ids it rides under.**
An enum field and a union field both describe a named set indexed by
`[0, enum_max]` — an enum's values, a union's arms — with a value→name
function beside a **value→wire-id** function, so a tool can enumerate the
names AND the ids without the schema files (§5).

**A union field also names each arm's PAYLOAD**, by that type's descriptor,
whether the arm is a declared `type` or a `table` (§2.6), **beside the
TAG's own offset and width** and each arm's offset within the union
storage. Without those a walker can name the arm a value holds and can
neither read the tag nor enter the payload, which stops every generic
walk — the text form (§16) among them — at the union. Arms are indexed
`[0, enum_max]`, and index 0 is the EMPTY arm, which carries no payload.

**A flags field carries its BIT NAMES**: a bit-index→name function bounded
by **the highest declared BIT INDEX** — not a count, so a walker loops
`[0, enum_max]` inclusive, exactly as it does for an enum's values and a
union's arms. It carries no per-variant wire id, because a mask's variants
ride by position and have none (§4); a null id function beside a non-null
name function is what identifies a flags field.

**An enum-keyed array's slot 0 is marked invalid** (§2.4), and the
descriptor says so in the column a walker is already reading:
**`key_id( 0 )` is `0`**, the reserved id no declared name can fold to
(§5), with `key_name( 0 )` reading `"None"` beside it. So a walker
enumerating `[0, array_bound)` skips the slot whose key id is 0 rather than
printing a `None` row, and it needs no rule about slot indices to do it —
the same reserved id that keeps `None` off the wire keeps it out of a
listing.

**A `*bytes` or `*string` field** carries its used-length companion's
offset beside the reference, so a walker reads the blob's extent the way
it reads an array's count (§2.5).

These columns exist in every unit, whatever its mode — they describe the
LANGUAGE, and a fixed-size table can declare all of them. Only the two
POINTER columns below are conditional (§2.2).

A unit that has pointers carries two more facts, and a unit that has none
carries neither (§2.2): a field's **`is_pointer`** flag — whose `table`
member then names the TARGET table's descriptor, NULL for a `*bytes` or
`*string` because a buffer is not a declared table, and whose `elem_size`
is the reference slot's width — and a type's derived **`variable`** mode, so
a tool can tell at runtime which of §6's two lives a table has without
being told. A self-referential pointer resolves to its own type's
descriptor. Where pointers exist the descriptors are CONSTANT-INITIALISED
data and a target is the ADDRESS of another descriptor, so `Node` naming
itself through `*Node` is expressible directly: no first-use link, which
could not have been written both race-free and recursion-safe, and no
mutable state anywhere on the surface. This is the surface
editors and tools build on — walk properties by name, print a value, diff
two, bind a property grid — with no RTTI and no schema files at runtime.

## 9. Relocatable, and built wide

Two properties are load-bearing for real content pipelines, and both are
held by construction:

- **Relocatable, where possible — and it holds in BOTH forms.** Generated
  table structs are trivially copyable and standard-layout, and the
  generated header enforces that with static asserts. A pointer FIELD is
  a four-byte reference, never a machine address, so the property
  survives pointers: a fixed-size table is one memcpy-able struct, and a
  packed region (§6.3) is one memcpy-able BLOCK whose references are
  self-relative and therefore need no fix-up at a new address. A
  non-compacted builder is relocatable only per storage segment — which
  is exactly why `Lock` exists and produces the single contiguous form.
  The owner's ruling holds relocatability as a goal, not an absolute: a
  construct that genuinely cannot be relocatable is flagged and decided,
  never contorted around.
- **Parallel generation.** Encoding splits into **measure** and **save**:
  measure computes a value's exact encoded size writing nothing; save
  writes into a caller-provided buffer. Because nested tables are
  length-prefixed, a build can measure subtables from N workers,
  prefix-sum the offsets, and scatter-write disjoint ranges in parallel —
  and a reader can fan nested-table decodes out the same way. The framing
  guarantees the option; callers choose whether to take it.
  **measure == save at exact capacity** is a hard invariant, held by a
  mandatory battery across the corpus and across pointer graphs: saving
  into a buffer of exactly measure's answer always succeeds and
  byte-matches a roomy save, and one byte short always refuses.
- **Going wide on the BUILDING side** is §6.4: allocation is thread-local
  and nothing ever moves, so N workers fill one arena with no lock and no
  per-node atomic.

## 10. Independence from the hardcoded wire

Table declarations do not enter the unit's wire-shape projection. Adding,
editing or deleting a table moves no `ProtocolId` and no generated `type`
byte: peers whose hardcoded wire did not change are never forced into a
lockstep redeploy by a table edit. This independence is held by test.

## 11. Refused by name

- `table` bodies containing `fixed`/`ufixed` or the 128-bit family (§2 —
  no wire kind), `const`/`reserved`/`align` (§2 — no bit positions), or
  arrays of unions (§2 — named follow-on). Extents have no wire ceiling
  (§3); an extent past the language's own int32 storage cap is refused
  there, not here.
- Recursive nesting (§2 — the cycle is named).
- A bare rename hazard: `was` naming the field's own name (§5).
- Id collisions, hash or `was`-induced (§5).
- `was` outside a table body (§5).
- **Variant id collisions** — two variants of one enum, or two arms of one
  union, whose name hashes collide, with both named (§5). An enum reaching
  the closure only as an ARRAY KEY is in scope, and the diagnostic names
  the keying field as the reaching edge (§2.4).
- **`| max = K` headroom on an enum in a table closure** — a headroom value
  has no name, and the table wire identifies a variant by name (§5). Key
  enums are in scope on the same terms.
- Tables under a backend that carries none (status, above) — refused with the
  follow-on named, never silently ignored.
- **A VARIABLE-LENGTH table under the C# backend** — the C# port carries the
  fixed class; its variable class (pointers, arena, region, cooked) is a named
  follow-on, and a pointered unit is refused naming the tables, never emitted
  with them missing.
- **Pointers** (§2.1): `*T` where T is a `type`, enum, flags or union —
  value-semantics data has no identity to point at; a pointer declared
  outside a table body; an array of pointers (§15); a specified default
  on a pointer field.
- **Optional fields** (§2.3): `?T` outside a table body; `?` on a pointer
  (already optional); `?` on a union (its `None` IS the absence); `?` on an
  array, a string or `bytes` (a named follow-on, §15 — the count or length
  already carries emptiness); a specified default on an optional; and a
  field whose name collides with an optional's `<field>_present` companion.
- **Enum-keyed arrays** (§2.4): a bound naming a `flags` declaration (a mask
  names no single slot); a bounded keyed array, `[..E]` or `[A..E]` (a keyed
  array is complete by construction); an element that is a pointer, as for
  any array (§15); an index of `E::None`, which names no slot — refused at
  compile time through the constant accessor (`Slot<Key>()`) and asserted
  through `operator[]( E )`; and, on the KEY ENUM itself because a key is a
  reaching edge into the table closure, `| max = K` headroom and variant id
  collisions, each diagnostic naming the keying field that pulled the enum
  in. A slot value no variant names is a SAVE failure, not a silent `None`
  (§3.2).
- **Byte buffers** (§2.5): `*bytes` or `*string` outside a table body; a
  specified default on one; an array of them (§15); `?` on one, because a
  null reference already IS absence.
- **A `table` union arm outside a table closure** (§2.6) — a union declared
  for the type wire takes `type` payloads only, because types are value
  semantics.
- **The text form's key** (§16): `json = "..."` on a field no table closure
  reaches — keys are a table-wire construct; and two fields of one table
  whose JSON keys collide, naming both, as wire ids do (§5).
- **An edit the committed TABLES BASELINE forbids** (§18), when a unit has
  one: a specified default changed, added or removed; a flags variant
  inserted, removed, reordered or renamed in place; a field's wire kind or
  an array's ELEMENT kind changed; an array changed between the keyed and
  the positional spelling; an enum-keyed array's key enum swapped; a
  field's referent dropped, or swapped for one whose identities do not
  ride. Overridden only by moving the baseline with a recorded reason.
- **A save-time data cycle or over-deep graph** (§3.1): measure, save,
  cook and `Lock` all return failure. Nothing recurses away.
- **A read-time nesting past the depth cap** (§3.1): the subtree is
  refused, the pointer stays null, and the read is flagged malformed.
- **A cooked file this build cannot point at** (§7): wrong magic or byte
  order, a foreign layout id, truncation, an unaligned base, or an offset
  graph that leaves the region, goes backward, misaligns, or is bounded
  by an out-of-range count. `Open` returns NULL; the caller falls back to
  a wire load.
- **A declaration colliding with a generated table spelling.** Tables and
  types share one symbol table (§13.1), which is what makes the generated
  surface unprefixed and collision-free — so every name a closure member
  claims is refused to everything else. A member `X` claims `X` followed by
  each of these **26 suffixes**, and a declaration spelling one of them is
  refused naming the collision:

  ```
  Measure  MeasureBody  Save  SaveBody  Load  LoadBody
  LoadMeasure  LoadMeasureBody  LoadBuilder  TableType  Builder
  At  Root  Emplace  Pack  PackMeasure  OpenWalk
  Cook  CookMeasure  Open  LayoutId  TableFields  TableInfo
  FromJson  ToJson  ToJsonMeasure
  ```

  The set is claimed for EVERY closure member, not only pointer-bearing
  ones: a table gains or loses pointers as an edit, and a name that was
  free yesterday must not become a collision tomorrow.
- **A table named after a member of its generated builder** — a member
  function hides the type name it shares, so the header would not
  compile. The two accessors a real schema would plausibly hit were
  renamed instead (`GetRoot`, `AsConst`), so `table Root`, this
  document's own canonical example, stays legal.
- **A cross-file reference CYCLE among table declarations.** The rule is
  language-neutral: **a unit's table files form a DAG by reference** — if a
  declaration in file A reaches one in file B, nothing in B may reach back
  into A. Same-file recursion through pointers is fine; move a declaration
  so the cross-file graph is acyclic. (In C++ the consequence is concrete —
  the generated `<A>Table.h` and `<B>Table.h` would have to include each
  other, and neither could compile — but the requirement is not C++'s: a
  port whose modules import by file inherits the same acyclicity, and the
  rule is stated so it does not have to rediscover the reason.)

## 12. The expressiveness gate

The feature's acceptance test is a DOGFOOD, not a thought experiment: a
real game's binary config and asset archive formats — a root table of
nested collections of typed records, built by tools, loaded by the game —
must be expressible as declared tables with nothing left over, and without
schema prescribing any of their structure. The corpus carries a
config-format example holding that gate.

**The shape the gate is held to.** `Config.bin` and `Assets.bin` are each
ONE root table, and each root is FIXED-SIZE down to the leaves: no pointer
anywhere in the closure. `?T` (§2.3) expresses an optional section by
value and `[E]T` (§2.4) expresses the enum-keyed collection as language
rather than as convention, so neither forces a pointer. A fixed root is
the strong form of the gate: it says the whole content pipeline runs on
the ladder's second rung, with no arena, no region and no allocation on
either side.

**The gate is a per-language obligation, not a one-language one.** A game
whose engine runtime is not the language its tools are written in has to
read the same bytes from the same declarations, with the same report, on
both sides — so a backend clears the gate only in its own language, and
the per-language backends are named follow-ons (§15).

## 13. Rulings, recorded

Owner rulings, 2026-09-01, in the order given.

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

### 13.1 Pointer semantics, ruled

- **The founding line**: "types can remain value semantics. tables should
  ALLOW pointer semantics" — "we can't be a generic system if we don't
  have pointers to tables."
- **The spelling**: "literally same C++-like syntax for pointers is fine."
- **The mode is the compiler's job**: "i wouldn't want to manually have
  to specify this… the compiler can work it out and go, oh it's the
  variable table mode."
- **The mutable form**: "i think variable tables need to live in a
  growable array."
- **Const is a lifecycle, not a marking**: "maybe you 'lock' a table and
  it is constant from that point forward… since how else will you
  construct Assets.bin and Config.bin."
- **The two forms are genuinely different**: "mutable vs. non-mutable
  tables may be different at runtime."
- **Building goes wide, without locks**: "the builder needs to be able to
  be multithreaded"; then "I prefer lockless if possible."
- **The generated surface**: name first — `ChatMessageMeasure`,
  `ChatMessageSave`, `ChatMessageLoad`, `ChatMessageBuilder` with
  `Alloc<T>()` and `Lock()`. Tables and types share one symbol table:
  "do tables and types share the same symbol table then, i vote yes" —
  which is exactly what makes the unprefixed surface collision-free, and
  the checker refuses a declaration colliding with any generated
  spelling.
- **Small messages pay nothing**: "make sure we don't pay this cost when
  we have nice, small messages… when the table is inferred to be value
  types only."
- **Size and mode correlate**: "let's assume larger types will probably
  have pointers to things."
- **The big class is not a struct**: file-format-scale tables "can't be a
  struct" — pointer-bearing tables are never held by value.
- **No generated size constants**: "you don't need to recreate sizeof."
- **Point at a file, do not parse it**: the requirement behind §7 — load
  a big file and point at its root, without copying it and without
  parsing the whole thing.

### 13.2 Cost and allocation, ruled

The performance ladder and the allocation rules in this document's opening
are these rulings, in the owner's words:

- **The trade**: "Tables are 'less' performant than types."
- **The top rung**: "Types are expected to match the equivalent of raw
  structs."
- **The second rung**: "Fixed tables should be as performant, when
  possible…" — which this document reads as a bench obligation, a fixed
  table beside its equivalent type on the ledger.
- **The union carve-out, keyed on the LANGUAGE**: "but for example, if
  fixed with a union, it's OK to alloc (if the language needs it)"; and
  for the ported backends, "I have no problem for the message and table
  case (not types) if you do some allocations while you read or write for
  unions, this way you can have a pseudo-union in golang."
- **The closing rung**: "variable tables, you'll obviously need to alloc
  and assuming otherwise is foolish."
- **Why very large tables want a blob node** (§2.5): "the key with variable
  tables is imagine they could be very large, like a gigabyte. you'd not in
  that case want to blow out memory with extra union tables you don't need
  and so on" — and the primitive itself, "yes, I like byte buffer as
  primitive", so that "we can include say, images or assets inside them."

### 13.3 The fixed-class constructs, ruled

- **Optional fields** (§2.3): "If you want to have optional fields without
  needing pointers too, and that's elegant, then go for it" — and, once
  they existed, "it's cool to keep the Config.bin and Assets.bin fixed
  tables, for now. No pointers", which is the fixed-down-to-root shape §12
  holds the gate to.
- **Enum-keyed arrays** (§2.4): "I like the enum keyed arrays. That is
  cool… It's a really good, unique language feature, that is optional."
  Optional is part of the design: `[E.Max + 1]T` stays legal and
  positional; `[E]T` is the keyed form a user chooses.
- **Union arms as tables** (§2.6): "if you had a set of messages for
  tooling, you'd want to safely evolve those tables / messages."
- **The text form** (§16): "the general idea of reading JSON file into a
  table can move into schema… that's not opinionated. but only one table at
  a time? So we separate packing from table reading JSON, the actual
  linking up by enums has to be replicant only since that is opinionated."
  One table, one text; the ruling's second half is what §17 answers, and it
  answers it by removing the opinion rather than by keeping the tool out.
- **Packing** (§17): "It also may make it possible to move the table
  reading and packing entirely down into schema now… which is a WIN", with
  the directory rule taken as it stands and revisited against a real packed
  corpus rather than in advance.
- **The baseline** (§18): "Practically, consider the save game example.
  You'd have to just be careful, and if the compiler helped you be careful
  -- that would be good"; the invariant, "the save games would have to not
  ever break compatibility with stuff already written"; the same for tool
  output, "same for example, for tooling"; and the override, "or
  *explicitly* override it, saying, ok fine, from this version on, we
  intentionally break compat and that's OK" — which is why `--update`
  without `--reason` is refused.

## 14. Design notes: the models weighed

Recorded because the rejected options are the useful part.

**The builder's storage.** Four models were weighed against the owner's
criterion — lockless if possible, and minimize copying:

1. **One buffer under a lock, grown by `realloc` — REJECTED.** The hazard
   is specific and worth naming: a `realloc` moves the buffer under a
   worker that is mid-write. Offsets fix identity but they do not fix the
   raw references already resolved from them, and the resulting
   corruption is invisible until much later. This design does not admit
   that bug class.
2. **Sharded builders merged at Lock — considered.** Each worker owns a
   private growable array; references are (worker, slot) handles;
   `Lock` merges the shards and resolves every handle. Contention-free,
   but it pays a full memcpy of every node at merge and a handle-to-offset
   resolution pass on top of it.
3. **Reserve-max with an atomic bump — considered.** One buffer sized to
   a declared bound; allocation is one atomic add and nothing ever
   resizes. It is the allocate-max law dissolving the lock, and it is the
   right answer for a caller who genuinely knows the bound; it is kept as
   the documented alternative rather than the default, because a general
   builder cannot require one.
4. **A segmented slab arena — THE DESIGN OF RECORD.** One logical arena
   made of equal-size segments; a worker takes a slab with a single
   atomic and then allocates privately inside it. One synchronization per
   thousands of nodes, nodes born at final offsets, no handles and no
   resolution pass, growth by appending a segment, and nothing ever
   moves.

`Lock` remains a full copy, because it is the compaction: it produces the
single contiguous exact-packed region that §6.2 and §7 both need, and it
is the ONLY copy in the lifecycle besides `Save` (which is inherent — it
writes a wire). A non-compacted arena is relocatable only per segment;
that is the reason `Lock` compacts rather than merely freezing.

**Reference encoding.** The arena's offset is segment-indexed, so a deref
there is a small table load plus an add. The region's is self-relative,
so a deref there is one add with no base pointer — and it makes the
region relocatable by pure `memcpy`. Two encodings, converted where a
copy already happens.

**Depth versus a visited set.** A depth cap, not cycle detection: it
needs no state, it bounds the C stack against hostile data, and a packed
region cannot contain a cycle at all (§6.3).

**Why the cooked walk still needs order state.** Acyclicity is not
enough. A forged region can be a legal DAG — forward, in range, aligned —
and a walk with no memory of where it has been re-visits shared subtrees
exponentially. The fix is not a visited SET, which would need allocation
proportional to the graph; it is a single monotonic high-water mark,
which works precisely because packing is pre-order and the walk follows
it (§7). One integer buys linearity, termination and overlap rejection
together.

## 15. Named follow-ons

- **TABLE WIRE V2 — the flat indexed node encoding.** One design answers
  three of the entries that follow it: a pointered root writes every
  reachable node ONCE into a node table and a pointer field rides as an
  index into it, which lifts the depth cap, preserves DAG identity, and
  makes an array of pointers an array of indices. It reaches past §3.1 into
  the region and the cooked form, and it is drafted spec-first, ahead of
  any emitter, because it changes what the bytes mean wherever a pointer
  appears. **The pointer-free classes are untouched by it**, byte for byte,
  so nothing in the fixed class waits on it.
  - **Graph and DAG identity**: preserving aliasing and sharing across the
    wire, so two references to one node stay one node. Wire v1 is a tree
    (§3.1) and says so.
  - **Lifting the depth cap**, so a pointer chain's length stops being a
    nesting depth (§3.1).
  - **Arrays of pointers** (§2.1).
- **Per-language backends beyond C++ and C#** (the refusal in §11 names this).
  C# came first, because the dogfood's game engine reads the same config and
  asset bytes the C++ tools write (§12), and the FIXED class is what that
  needs: storage structs, measure/save/load over caller-owned buffers, the
  report, the reflection descriptors, `?T`, `[E]T` and name-hashed
  vocabularies — the text form and the variable class are still ahead of it.
  A port mirrors this document and invents no contract of
  its own; where a language forces a shape — a pseudo-union for a language
  with no native union — the ladder above already says what is licensed.
- **The C# VARIABLE class** — the arena, the region, the cooked form and the
  pointer surface, on top of the fixed class C# carries today (§11). And the
  C# text form (§16), whose walk C++ carries alone.
- **The variable class in a ported backend** — the arena, the region, the
  cooked form and the pointer surface — after that port's fixed class.
- **The TEXT FORM for the variable class** (§16.1) — a second walker that
  fills a builder rather than an instance, emitted only in units that
  declare a pointer, so a pointer-free unit carries nothing for it. The
  surface is designed and stated; no backend emits it, and a pointered unit
  is refused by name until one does.
- **`?` on an array, a string or `bytes`** (§2.3): a presence bit beside an
  existing count or length wants a decision about what the pair means before
  it becomes wire. Wrap the field in a table and make that optional today.
- **An array of `?T`** — the same question one level down: an element's
  presence bit beside the array's own count.
- **Cross-endian `Open`**: an in-place, descriptor-driven byte-swap pass
  sharing `Open`'s existing traversal — the same nodes and fields are
  already visited, and kinds and offsets are already in the descriptors.
  v1 refuses a foreign byte order instead (§7).
- **A hash-guarded fallback loader** — open the cooked form, else load
  the wire — as a convenience helper.
- A generic dump/diff tool over the reflection surface.
- Keyed lookup conveniences over loaded collections (library-side, never
  stored semantics).
- Arrays of unions in table bodies.
- `fixed` and 128-bit table-wire kinds, if a need ever materializes.
- **An `--envelope` shape for `schema pack`** (§17), if one recurring
  wrapper — a magic, a content hash, a protocol id — earns being schema's
  rather than each caller's.

## 16. The text form: JSON in and out of one table

Reading a JSON text into a table, and writing one out, is not an opinion:
it is one table, one text, one walk over the reflection descriptors (§8),
the same for every table in the closure. schema owns it. Everything AROUND
it is an opinion and belongs to whatever tool holds it — which file goes
with which instance, what key an instance is filed under, how instances are
linked into a root table's collections, what envelope wraps the bytes. A
packer (§17) calls this section once per text and does the rest itself.

**This section states RULES, not one implementation.** A generic walk over
the descriptors is what makes the form cheap enough to exist at all, and
the C++ backend holds it as one walker whose source is byte-identical in
every generated header — but that is C++'s way of meeting the rules, not
the definition of them. Another backend, and the compiler's own packer
(§17), implement the same rules over the same IR, and goldens are what make
the implementations one form: for every instance in the corpus, every
implementation's text is byte-identical and every implementation's read of
that text produces the same wire bytes.

**The walk rides in every table closure's header, the fixed class
included.** It is not gated on a unit's mode, because a fixed-size table is
exactly the kind a tool authors as text; there is no opt-out.

### 16.1 The surface

For every FIXED-SIZE table in a unit's closure, name first, C++:

```cpp
TableReport report;
ShipConfig ship;
if ( !ShipConfigFromJson( ship, text, text_bytes, &report ) )
{
    // the text is not JSON (report.malformed) — the instance holds what
    // was placed before the stop
}

int64_t size = ShipConfigToJsonMeasure( ship );      // exact bytes, writes nothing
ShipConfigToJson( ship, buffer, size );              // returns size; -1 = refused
```

A VARIABLE-LENGTH table reads through its builder, because that is where
its storage comes from (§6.5):

```cpp
SceneFromJson( builder, text, text_bytes, &report );
```

**Backend status for this section: the FIXED class.** No backend implements
the variable class's text form yet — a pointered unit gets no `FromJson`,
refused by name with this section cited, never emitted with the function
missing — and the second walker it needs, emitted only in units that
declare a pointer, is tracked as schema#275.

- **`FromJson` fills ONE instance from ONE text.** The instance is the
  caller's; the read path allocates nothing beyond it. Fields the text does
  not mention keep their storage defaults (SPEC §4.2: zero, or the
  specified default), exactly as an absent field on the wire does (§4).
- **`ToJson` writes ONE instance as ONE text**, every field, in declaration
  order, defaults included — a text is for people and tools, and a text
  that elides is a text a reader has to know the schema to complete.
  Measure and write agree byte for byte, the wire's invariant (§9) carried
  across.
- **`ToJson` is PRETTY-PRINTED**, and the shape is part of the contract:
  one entry per line, two-space indent per nesting level. Measure must
  equal write byte for byte and `unpack` → `pack` must be byte-stable
  (§17.2), and neither is checkable while the shape is unstated. It is the
  form's one formatting opinion, and it is held because a text these files
  exist for is read and diffed by people.

### 16.2 The mapping, field kind by field kind

The JSON form of a table is an object whose keys are field KEYS — a
field's name, or the `json` attribute's value where one is declared (§16.4).
Per kind:

| declaration | JSON | notes |
|---|---|---|
| integers, `bits(N)` | number | see **Numbers** below; a `bits(N)` value over its implied `[0, 2^N − 1]` clamps and counts |
| `float32`, `float64`, compressed floats | number | a value a float32 field cannot hold is `kind_mismatch`, never stored as infinity |
| `bool` | `true` / `false` | |
| `string(N)`, `*string` | string | longer than N is CLAMPED to N bytes at a code point boundary, counted (`*string` has no bound to clamp against) |
| `bytes(N)`, `*bytes` | string, base64 | standard alphabet, PADDED on write; padded and unpadded both read. Longer than N is clamped, counted |
| enum | string, the variant NAME | `"Silver"`; `None` writes as `"None"`; an unknown name → None, counted |
| flags | array of variant names | `["Shielded", "Turbo"]`; an empty mask writes as `[]`; an unknown name is skipped, counted |
| `[N]T` fixed array | array | fewer elements pad with defaults; more are dropped, counted |
| `[..N]T` bounded array | array | count = length; more than N are dropped, counted |
| `[E]T` enum-keyed array | object keyed by VARIANT NAME | `{ "Fighter": {...}, "Bomber": {...} }`; an absent key keeps that slot's defaults; an unknown key is skipped and counted, and **`"None"` is such a key** — it names no slot (§2.4) |
| nested `type` / `table` | object | the same walk, recursively |
| `?T` optional | the value, or the key absent | **presence of the KEY is presence**: a key present sets the field present, whatever its value; an absent key leaves it absent. `ToJson` writes present optionals only |
| union | object with ONE key, the arm name | `{ "buff": { "multiplier": 2.0 } }`; `None` writes as `{}`; `{}` or absent reads as None; two keys is malformed. A `table` arm (§2.6) is the same object form |
| pointer `*T` | object, or `null` | the pointee's object in place; `null` is a null pointer |

**Three of the entries above describe constructs no declaration reaches
yet**: the `*string` and `*bytes` halves of their rows land with those
declarations (schema#259), and the `table` union arm named in the union row
lands with its own (schema#258). They are stated here rather than added
later because a text mapping is a property of the KIND — each one lands as
its declaration lands, not as a second decision about text. The `*T`
pointer row is the variable class's, covered by the status in §16.1.

**Numbers.** JSON has ONE number type, so an integer field accepts any
token whose VALUE is integral — `2`, `2.0` and `1e3` are the integers 2, 2
and 1000 — because that is how every library that round-trips numbers
through a double writes them, and meeting an existing text is what §16.4
exists for. A token with a genuinely fractional value in an integer field
is `kind_mismatch`, skipped and counted. A value outside the field's
declared or implied range clamps and counts (**Bounds**, below); a float
value the field's width cannot represent is `kind_mismatch`, so an
infinity is never stored. **A token that is not a JSON number at all** —
`1-2`, `5+`, `1.2.3`, `--3` — is `malformed`: the token grammar is RFC
8259's, and a typo in an authoring file is a diagnostic, never a clamped
value.

**`null`** is `kind_mismatch` for every kind except the two where absence
is a value: a `?T` reads `null` as ABSENT, and a pointer reads it as null.

**Guarded fields** (`if guard { ... }`) are ordinary fields, and the walk
infers nothing from them:

- **Writing** elides a field whose guard is false, exactly as the wire does
  (§3), so a text and a wire of the same instance describe the same fields.
- **Reading** places every key it can name, in whatever order the text
  gives them, and never lets a guard's position in the object decide
  whether a key is honoured. A field placed under a false guard is elided
  again on the way out, so the wire never sees it.

That order-independence is the whole reason the rule is stated this way: a
text whose guard key follows the fields it guards must not silently lose
them. A field that is genuinely present-or-absent is `?T`, which the walk
DOES model, and that is the difference between the two constructs.

**Bounds.** A number outside a field's declared `[min, max]` is clamped and
counted, never refused — the wire's rule (§4), so a text and a wire loaded
from the same data land the same instance.

**Unknown keys** are skipped and counted in `report.unknown`. **Duplicate
keys**: the last occurrence wins and the repeat is counted in
`report.duplicate` (§4). Last-wins applies to a WHOLE value: a repeated
array key replaces the array outright rather than overlaying a prefix on
what the first occurrence left, and a repeated table or union key
re-establishes the whole value at its defaults before placing it.
**Trailing commas** in objects and arrays are accepted on read — the
authoring files this section exists for carry them — and never written.
Comments are not JSON and are refused.

**The report** (§4): `unknown`, `kind_mismatch` (a key present with the
wrong JSON type — a string where a number was declared — is skipped, never
coerced), `clamped`, `duplicate`, `malformed`. Silence means the text
matched the schema exactly, and it means it honestly: no value a read calls
clean can be one the writer would refuse.

### 16.3 What the writer refuses

`ToJsonMeasure` and `ToJson` return **-1** rather than write a text that
does not say what the instance holds. This is §5's save-failure rule
carried across: an unnameable value is refused on the way out rather than
written as something else.

- **An enum value no variant names**, and **a set flags bit no variant
  names** — refused by the NAME, not by a bound, so a vocabulary that
  disagrees with its own extent cannot slip through as a placeholder.
- **A union tag outside its arm range.**
- **A non-finite float.** The read path cannot produce one (above), so an
  instance that came from a text is always writable; one built in code with
  an infinity in it is refused, and says so.

`-1` therefore has two meanings on `ToJson` — a buffer too small, and a
value that cannot be written — and a caller that distinguishes them calls
`ToJsonMeasure` first, which fails only for the second reason.

**The writer always emits valid JSON and valid UTF-8.** A stored byte
sequence that is not well-formed UTF-8 — which the storage permits, and
which a text can introduce through a lone surrogate escape — is written as
the replacement character `U+FFFD`, one per ill-formed sequence, rather
than passed through. RFC 8259 requires a JSON text to be valid UTF-8, and a
text this form emits must be readable by any conforming parser, not only by
schema's own reader.

### 16.4 The key: `json`

A field's JSON key is its name. The one attribute this form adds lets a
declaration meet an existing text:

```
ship_type ShipType | json = "type"
```

The field reads from and writes to `"type"`. Two fields of one table whose
keys collide are refused at compile time, naming both, as wire ids are
(§5); `json` on a field no table closure reaches is refused (§11). **The
attribute changes no byte on the wire**: keys are the text's business, ids
are the wire's, and a schema may add, change or remove a `json` key without
touching a stored file.

### 16.5 Held by test

- Every table in the corpus round-trips `ToJson` → `FromJson` → `Save` and
  byte-matches the wire of the original instance.
- **A PINNED TEXT.** `ToJson` of a known instance equals a known literal
  text, checked as bytes. A round trip alone cannot see a vocabulary error
  — reader and writer share the name function, so a wrong spelling
  round-trips perfectly — and this is the test that closes that class for
  enums, flags, union arms and `None`.
- A hostile battery: wrong JSON types at every kind, malformed number
  tokens, overflow at every integer width and float width, nesting past the
  depth cap, unknown keys, duplicate keys at every kind including arrays,
  clamped strings at a multi-byte boundary, a union with two keys, an
  enum-keyed object keyed `"None"`, a lone surrogate.
- Guards in every configuration, including nested and negated ones, and a
  text whose keys precede their guards.
- **Negative control:** with the walker's offset arithmetic sabotaged by
  one field, the round-trip goes red on the first table that has two
  fields.
- The one-walk gate: within a backend that holds the form as a single
  walker, that walker's source is identical in every generated header.

### 16.6 What this is not

Not a file format, not a directory layout, not a manifest, not an envelope.
Those are a packer's business; §17 is one packer, and it is a TOOL over
this section rather than a second definition of it.

## 17. Packing: a directory tree that mirrors a root table

`schema pack` assembles ONE table instance from a directory tree and writes
the root's wire bytes. It adds no format: the tree is the table, the text
in it is §16's, and the output is §3's.

```
schema pack   --root Config --out Config.bin  configs/
schema unpack --root Config --in  Config.bin  configs/
```

**Why this is not the opinion the text form's ruling kept out.** What made
packing an opinion was linking instances into a collection by an enum key,
and the key field inside each instance that made the link possible. With
`[E]T` (§2.4) the link IS the declaration and the key is the slot, so there
is no manifest, no collection concept and no key field to invent. What is
left is a structural convention about where a value's text lives, which is
the part that prescribes nothing about content.

### 17.1 The engine

**`schema pack` carries its own implementation of §16's rules and §3's
wire**, inside the compiler, driven by the same IR the emitters are driven
by. It does not call generated code: the compiler is a Go program and the
generated walk is the target language's, so there is no path from the one
to the other, and building one would make the compiler depend on a C++
toolchain to pack a file.

That means the tree holds **two implementations of one wire**, and goldens
are what make them one:

- for every corpus tree, `schema pack`'s bytes equal a backend's `Save` of
  the same instance built by hand;
- `schema pack` → a backend's `Load` → that backend's `ToJson` →
  `schema unpack` is byte-stable;
- every text `schema unpack` writes is byte-identical to the one the
  backend's `ToJson` writes for the same instance.

Every backend that implements the text form inherits that obligation
against the same corpus, which is what keeps one wire and one text form as
the number of implementations grows.

### 17.2 The directory rule

The tree MIRRORS the root table's shape, and nothing else:

- a **directory named after a field** holds that field's value;
- for an **enum-keyed array** (§2.4), one file per variant,
  `<Variant>.json` — and there is no `None.json`, because `None` keys no
  slot;
- for a **bounded array**, files in name order become the elements, or one
  `<field>.json` holds the whole array;
- for a **nested table**, either `<field>.json` or a directory of its
  fields;
- a plain **`<field>.json` at any level** is that field's value verbatim;
- the **root may simply be one `<Root>.json`**.

Each file's content is read under §16's rules, so everything about kinds,
presence, numbers, clamping and the report is that section's and is not
restated here. The tree rule is structural only — it says where a value
lives, never what a value means.

### 17.3 What comes out

**The output is the root table's wire bytes and nothing else**: no magic,
no content hash, no protocol id, no length prefix around the whole. A
caller that wants an envelope writes its own few lines around these bytes,
which is §1's promise that schema imposes no envelope. `unpack` is the
inverse — it writes the tree back out of a `.bin` — which is the tool round
trip §1 promises, and `unpack` → `pack` is byte-stable.

### 17.4 Refusals and the report

A tree that does not mirror the table is reported rather than guessed at: a
directory or file naming no field, two files claiming one enum-keyed slot,
a variant name the enum does not have, a `None.json`, a file that is not
JSON. Everything §16 counts is counted here too, aggregated across the
tree, so a pack of a hundred files reports once.

### 17.5 Held by test

A directory corpus packs to bytes identical to `Save` of the same instance
built by hand; `unpack` → `pack` is byte-stable; the goldens of §17.1 hold
the engine to every backend that implements the form; and the hostile tree
above is refused or counted per §16's rules.

## 18. The tables baseline

**An optional committed projection of a unit's table closure, and the check
that refuses the edits the wire cannot report.** §4.1 names those edits:
exactly two, a changed specified default and a moved flags variant. The
compiler retains no history and cannot see either on its own. The baseline
IS that history, in a text file a person can read in a diff.

The motivating case is a SAVE GAME — a file written by a build the reader
no longer has, read by a build the writer never saw, years apart — and tool
output is the same case, not a second one: data that outlives the build
that wrote it. The invariant the check protects is the plain one: **no edit
may make data already written unreadable, or quietly change what it
means.**

### 18.1 The file

**It is `tables.baseline`, in the unit directory, and it is opt-in: no
file, no check.** It is a canonical text projection of the closure — the
members sorted, each member's fields in declaration order, one fact per
line: name, wire id, kind, an array's ELEMENT kind, array shape (fixed,
bounded or enum-keyed, with the bound's EVALUATED value and, for a keyed
array, the KEY enum it names), string and bytes capacity, presence of an
optional, the specified default as exact canonical text, and the `was`
alias; then each enum's variants in order with their ids, each flags'
variants in positional order, and each union's arms in order with their ids
and payloads.

**A field that names a declaration records WHICH KIND of declaration it
names** — a table, an `enum`, a `flags` or a `union` — because those four
are judged by four different identity rules (§18.3).

**The values are EVALUATED**, not the source text: a constant that moves
and flows through an expression into a default shows up as the value it now
produces, which is the whole point — the projection records what data will
mean, not how it was spelled.

**Presence is RECORDED and judged on nothing.** An optional's presence
companion is a fact in the file so a person reading a diff can see it, but
a field moving between `T`, `?T` and `*T` moves no byte (§3.1) and passes
in silence. Recording a fact and judging it are two different things, and
this one is only recorded.

It carries no protocol id and no packet fact: the type wire, the wire-shape
projection and the protocol id are untouched by all of it (§10).

```
schema-tables-baseline 2
package shipdemo

table ShipConfig
    field damage id=0x15a9 kind=10 default=21.0
    field speed id=0x2e46 kind=10 default=500.0 was=velocity
    field name id=0x30df kind=12 size=32

## history
### 2026-09-02 — first baseline before 1.0 ships
- baseline created over 1 table — data written BEFORE this point is not covered by it
```

### 18.2 The check

`schema tables-baseline <unit>` prints the projection. `schema check` — and
therefore every `schema generate` — diffs the live closure against the
committed file whenever one is there, and:

- **REFUSES, naming `table.field` and the edit** — a specified default
  changed, added or removed; a flags variant inserted, removed, reordered,
  or RENAMED IN PLACE (a rename moves no byte, and a new meaning on a spent
  bit remaps every stored file; nothing distinguishes the two, so the
  author says which); a field's wire kind or an array's ELEMENT kind
  changed; an array changed between the keyed and the positional spelling,
  or an enum-keyed array's KEY enum swapped for another; a field's REFERENT
  dropped, or replaced by one that cannot STAND IN for it — every identity
  survives AND, for a table or a union arm's payload, the facts under the
  shared field ids are unchanged (§18.3).
- **WARNS** — an array bound or a string/bytes capacity shrunk; an enum
  variant or a union arm removed; a DECLARATION renamed, or otherwise no
  longer in the closure under its baseline name (§18.3). The data survives
  and the read report already counts what is lost (`clamped`, `unknown`), so
  this reports rather than stops.
- **PASSES, in silence** — everything the wire absorbs: fields added,
  removed, reordered or renamed under `was`; enum variants and union arms
  added anywhere; flags variants APPENDED at the end; bounds and capacities
  grown; a bounded array made fixed or the reverse; a field moved between
  `T`, `?T` and `*T`, or between `bytes(N)` and `*bytes`.

### 18.3 What a name is worth, and what a referent is worth

**A DECLARATION NAME IS NOT ON THE WIRE, and renaming one must not take its
contents out of coverage.** Members match by name first; a baseline name
with no live namesake is then matched against the unmatched live
declarations of the same kind, scored on how many of its identities each
one carries, and paired only with one that carries AT LEAST HALF of them.

**Identity overlap alone cannot finish the job, so pairing asks a SECOND
question.** Overlap is blind to a brand-new declaration that happens to
carry the same field names — such a twin can outscore the real rename,
which may have dropped an identity in the same edit, and pairing the wrong
one would judge a fresh declaration against a history that is not its own.
So when more than one candidate reaches the half mark, the tiebreak asks
whose own FACTS UNDER THE SHARED IDS are closest: the count of judged facts
that differ, fewer being nearer, by the same rules a field's own facts are
judged by (§18.2 — a `pass` fact never separates candidates, because it
never means anything). **A candidate is paired only when it wins BOTH
questions STRICTLY**, most identities and nearest facts, with no tie in
either. The fact question is a TABLE's to answer: an enum, a union and a
flags declaration carry no per-variant facts to compare, so a contest among
them is never separated this way and is reported as the contest it is.

**Either way the vanished name WARNS**, and there are three unpaired
messages because there are three ways to fail, each naming what was
actually found:

- **too little overlap** — the closest declaration and its score, said to
  be below the half needed to call it a rename;
- **no overlap at all** — that no declaration carries any of its
  identities;
- **a contest that cannot be settled** — that two or more candidates each
  carry enough to be the rename and the evidence does not separate them, so
  **nothing is paired**, naming them all.

That last one is the point of the second question: a warning must never
assert a rename the evidence cannot distinguish. A paired name warns too,
naming the declaration that carries it on, so a hole in the coverage is
never silent either way. A removal stays legal; it stops being invisible.

**Two kinds of edit, and they never judge each other.** A declaration
edited IN PLACE is judged by its own walk, where the verdicts follow what
the runtime can report. A field REPOINTED at a different declaration is
judged by the referent rule below. A rename is the first kind wearing the
second's clothes, so a PAIRED rename is left to the walk: the same wire
loss draws the same verdict whether or not the author also renamed — an
enum variant or a union arm dropped from a renamed declaration WARNS,
exactly as it does when the name did not move.

**A FIELD'S REFERENT must be able to STAND IN for the one it replaces**,
and each vocabulary's standard is its own (§3):

- A **table** — a nested field, or a union arm's payload, which is the
  same fact one level in — is read by field id, so it stands in when every
  id the old one carried is still carried AND THE FACTS UNDER THOSE IDS
  ARE UNCHANGED. Id membership alone is not enough: a twin declaration
  carrying the same id under a different specified default rewrites the
  meaning of every stored body, and that is the flagship class this file
  exists to refuse. The facts are judged by the same rules a field's own
  facts are, so the two paths cannot disagree.
- An **enum** value is its variant NAME hash and a **union** body opens
  with its arm NAME hash, so those stand in when the names survive.
- A **flags** mask carries no names at all, so it stands in only when the
  old variants sit at the same bits.

**Dropping the referent entirely always refuses** — an enum-typed field
respelled as its raw `uint16`, say. §4 states that both ride as kind `7`,
so that is precisely the edit no reader can report, which is this file's
whole job.

### 18.4 Moving it is an explicit act

```
schema tables-baseline --update --reason "damage rebalanced in 2.0; saves from 1.x read the new value" configs/
```

`--update` rewrites the file AND appends a dated entry to the `## history`
section inside it, naming every edit it recorded, old value to new, beside
the reason. **`--update` without `--reason` is refused.** The history is
therefore the one record the wire lacks — the log of every intentional
break — and it is what a person consults when an old save or an old tool
file reads back wrong. The update is idempotent: a unit that has not moved
rewrites nothing.

**`--update` works on a baseline the checker cannot read.** A corrupt file,
another unit's file, or one written under a rendering version this compiler
does not write, all refuse on check and name `--update` as the remedy — so
the remedy runs: it salvages the `## history` lines verbatim, regenerates
the projection from the unit as it stands, and records in the history that
the previous projection could not be diffed. The one artifact in the file
that cannot be regenerated is never the price of repairing it.

### 18.5 What it does not cover

**The first baseline covers only what comes after it.** It is a snapshot of
whatever the schema is on the day it is written; data written before that
day was written against a shape nobody recorded, and no check can speak for
it. The created file says so in its own first history entry.

### 18.6 Held by test

Each refusal class has a fixture pair and its negative control — remove the
check and the edit passes. The projection over the corpus regenerates
byte-identical. The warn class warns and does not refuse. `--update`
without `--reason` refuses, and `--update` over an unreadable baseline
repairs it while keeping every history line.
