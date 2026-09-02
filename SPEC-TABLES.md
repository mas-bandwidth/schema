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

**The SECTIONED BLOCK (§2.7, §19) is specified and unimplemented.** No backend
emits it yet: a unit declaring a section is refused by name, with §19 cited,
never emitted with the block surface missing. C++ and C# take it together,
because the construct is an ABI between two languages and one language alone
cannot hold the gate it exists for (§12.1).

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
`if` branches, and declared types as field groups. Seven additions:

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
- **A field may be a SECTION** (§2.7). `ships section [..MaxShips]RenderShip`
  declares a strided array stored out of line in one block, with an
  `(offset, count, stride)` triple in the declaring table — the header-plus-
  sections shape of a per-frame render block, made language (§19).
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
several parents name — sharing is preserved end to end, on the wire and
in both runtime forms (§3.1). One pointer anywhere in a table's by-value
closure flips it to VARIABLE-LENGTH (§2.2) and with it the whole builder
lifecycle, so the choice is a real one.

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
- **A pointer field takes no specified default.** A fresh pointer is
  null, and null is the only value a default could name.

**An array of pointers is a bounded array like any other.** `[..8]*Node`
declares eight reference slots and rides as an array of node indices
(§3.1); a null element is index `0`. It is the spelling for a node with a
fixed fan-out, and it costs four bytes a slot instead of a whole table by
value. An array of `*bytes` or `*string` stays refused (§2.5): a buffer
takes no index, so there is nothing to put in the slots.

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

**A SECTION is by-value storage, so a block root is FIXED-SIZE** (§2.7). A
section field's storage is the sixteen-byte `(offset, count, stride)`
triple, which holds no pointer and is blittable like every other by-value
field, so a table that declares sections is a plain struct and the mode
derivation has nothing to propagate: the ELEMENT of a section is required
to be fixed-size in the first place (§11), and the strided records live in
the block rather than in the header. A section edge is therefore not a
by-value edge for the derivation above — it reaches no variable-length
table, because it cannot name one.

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

**The BLOCK machinery takes the same gate, on the same terms** (§19): a
unit that declares no section carries no block storage type, no `Begin`, no
`Open`, no strided iterator and no layout constants — the build fails if
one symbol of it appears — while the section's descriptor COLUMNS (§8) ride
in every unit, because they describe the language and a fixed-size table
can declare a section. Machinery is gated; columns are not, which is the
rule the paragraph above already states.

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
default on one (a fresh reference is null); an array of them — a buffer
takes no node index, so there is nothing to put in the slots (§3.1); `?`
on one, because null already IS absence (§2.3).

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

### 2.7 Sections: `ships section [..MaxShips]RenderShip`

```
table RenderFrame
{
    cameras section [..1]RenderCamera
    ships   section [..MaxShips]RenderShip
    lasers  section [..MaxLasers]RenderLaser
}
```

A SECTION is a **strided array of a fixed-size element, stored out of line
in one block**, with an `(offset, count, stride)` triple in the declaring
table. A table that declares one is a **BLOCK ROOT**: its instance is the
HEADER at the base of a block, the sections follow it in that same block,
and §19 is the form — how the block is laid out, filled by many threads,
and pointed at from another language.

**What the construct is FOR is speed across a language boundary.** A
section's records sit at a DECLARED PITCH that both generated sides carry as
a constant and the header carries as data, so a consumer written in another
language points at a record and reads it: no marshalling, no copy, no parse,
no per-element call. That is the whole reason the pattern exists in
hand-written form and the whole reason it becomes a declaration; everything
below is in service of it.

- **Storage in the header is SIXTEEN BYTES**: `offset (u64)`, `count (u32)`,
  `stride (u32)`, in that order, with no interior padding. It is by value, it
  holds no pointer, and it is blittable, so a block root is FIXED-SIZE
  (§2.2) and its header is one memcpy-able struct like any other table.
- **The offset is BLOCK-RELATIVE** — the byte distance from the block's
  BASE, not from the slot that holds it. A region reference is self-relative
  (§6.3) because nothing there hands a walker a base; a block's consumer is
  handed the base by construction, and a header read BY VALUE — which is how
  a blittable consumer reads it — keeps working, where a self-relative delta
  would not survive the copy (§14).
- **The bound is a MAXIMUM, and the spelling is `[..N]`.** `N` is the ceiling
  the block's STORAGE is sized from (§19.1); `count` is the runtime fact, and
  it is the number the producer declares before the block is laid out. Every
  other array spelling is refused in section position (§11): a
  fixed `[N]T` would make the count a constant, and a keyed `[E]T` is
  complete by construction and has no count at all.
- **The STRIDE is derived, and may be declared.** Derived, it is `sizeof` of
  the element rounded up to the element's own alignment — which for a
  standard-layout struct (§9) is `sizeof` itself, so the common declaration
  needs no attribute and the dogfood's own render records express with none.
  `| stride = N` declares it instead, and `N` is refused when it is smaller
  than `sizeof` or is not a multiple of the element's alignment (§11).
  Declaring a larger stride buys HEADROOM — bytes past the record's end,
  inside its pitch — and it costs the consumer's fast path, which is the
  honest trade §19 states.
- **The ELEMENT is a fixed-size `table` or a declared `type`**, and nothing
  else. A pointer, a `*bytes`, a `*string`, a variable-length table and a
  table that itself declares a section are each refused by name (§11): a
  block is one flat pointer-free extent, and a record whose meaning depends
  on anything outside it is not a record a consumer can point at.
- **Sections do not nest.** A section's element declares no section, and a
  block root is not nested by value, pointed at, made a union arm or used as
  an array element (§11). One block, one header, one level of sections. A
  block root's section offsets are meaningful only when it sits at a block's
  base, and refusing every other placement is what makes that true by
  construction rather than by care.
- **`?` and `*` are refused on a section** (§11): a section with a count of
  zero already IS an empty one, and a block holds no pointers.

**A section has NO WIRE KIND, and a block root therefore has no table wire.**
§3's set is closed, so a section's triple cannot ride, and nothing pretends
otherwise: a block root gets no `Measure`, no `Save` and no `Load`, and the
BLOCK is its serialized form. Its ELEMENTS are ordinary fixed-size tables and
keep their whole table wire, so `RenderShip` saves and loads tolerantly like
any other record — it is only the header that is wire-less. That is the
fourth form's honest boundary and §19 states what it costs.

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
  `16` enum-keyed array, `17` pointer index.
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
  | `17` pointer index | 4 bytes, the u32 node index (§3.1) |

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
  optional field is framed exactly as the non-optional `T` (§2.3), and
  `*bytes` and `*string` are framed exactly as `bytes(N)` and
  `string(N)` (§2.5). Each family is ONE FRAMING under several
  declaration spellings. **`*T` naming a TABLE is the exception**: it
  rides as a node index under its own kind `17` (§3.1), because a body
  that may be named twice cannot also sit inline at one of its names.
  The distinct kind is what makes moving a field to or from `*T` a
  REPORTED edit rather than a quiet one, for the same reason kind `16`
  exists (§3.2): a node index and a plain `uint32` are the same four
  bytes, and only the kind can tell a reader which it is holding.

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
  declared default (`T`), which is correct in every direction. Moving a
  field ACROSS families — between `*T` and `T` or `?T` — is not a free
  edit: it changes kind, and §4 counts it (§3.1).
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
  cover the set: kinds `1`–`11` and `17` skip their fixed width; kinds
  `12`, `13`, `14` and `16` read `L` and skip `L` bytes; kind `15` reads
  the `u16` arm id and stops there if it is 0, else reads `L` and skips
  `L` bytes.
  **A kind a reader does not know at all is not skippable** and is framing
  damage, which is why the set is closed and why a new kind is a wire
  change rather than an addition.
- **A SECTION has no kind and never rides** (§2.7). The set above is closed,
  so the `(offset, count, stride)` triple has no framing here and a block
  root has no wire form at all — the BLOCK (§19) is its serialized form. A
  section's ELEMENTS are ordinary fixed-size tables and their own wire is
  untouched, so the records a block carries are savable and loadable
  tolerantly wherever something wants that; only the header is wire-less.
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

### 3.1 Pointers on the wire: a flat node table

A pointered save writes every reachable node ONCE, into a **node table**,
and a pointer field rides as a `u32` **index** into it under kind `17`.
The encoding is flat: no pointer edge is a nesting level, so a chain's
length is not a depth; two references to one node are one node; and an
array of pointers is an array of indices. It moves not one byte of a
value-only table: a fixed-size table has no pointer, therefore no node
table, therefore exactly the bytes §3 already describes.

**Kind `17` costs nothing and closes an edit that would otherwise be
silent.** A node index is four bytes and so is a `uint32`, so under a
shared kind an edit between the two would report nothing in either
direction — a stored index reading back as a plausible number, a number
read as an index. The kind byte already rides, so the distinct number
costs zero bytes and one row in the fixed-width skip rule, and it makes
that edit an ordinary kind mismatch (§4). §3's rule that an unknown kind
is not skippable is what makes spending a kind expensive AFTER readers
exist; the set is closed before any of them ship, and §14 records the
trade.

**What a pointer edge is, and what it is not.** Only a `*T` naming a
declared TABLE takes a node index. `*bytes` and `*string` are leaf
buffers and are framed inline exactly as `bytes(N)` and `string(N)` are
(§2.5) — they charge no node, take no index, and create no depth,
because a buffer has nothing inside it to descend into. A table-typed
UNION ARM is a by-value nesting and rides inline as §2.6 frames it; the
pointer fields INSIDE an arm are indices like any other.

**Node numbering.**

- **`0` is null.**
- **`1` is the ROOT** — the body that hosts the node table. A pointer may
  name it, so a child pointing back at its root is expressible.
- **`2` and up are the node table's records, in order**: record `k`
  (1-based) is node index `k + 1`.
- The numbering is the **first-visit order of a depth-first pre-order
  walk from the root over POINTER EDGES ONLY**: fields in declaration
  order, array elements in index order, and descending through every
  by-value edge there is — a nested table, an element of a bounded or
  enum-keyed array, a member of a true `if` group, a present optional's
  value, a union's set arm — to reach the pointer fields inside them. A
  node takes its index the first time it is reached and never again.
- **A field the writer does not write is not an edge.** A pointer under a
  false guard, or inside an absent optional, is not visited and its
  target takes no index, so a save never writes a record that no written
  field names.
- **The numbering is deterministic and re-derived, never carried.**
  Measure derives it from the graph and save derives the same one from
  the same graph; nothing passes between them, and that is what makes
  `measure == save` (§9) hold across a pointer graph.
- It is the same order `Lock` lays a region out in (§6.3), because it is
  the same walk. Neither depends on the other: nothing that checks a
  region reads that order (§7).

**The node table rides under a RESERVED field id**, framed so that a
reader which cannot name it skips it and says so:

```
one or more fields, in order, each:
    id = 0xFFFF, kind = 12, L (u32), then L bytes

the FIRST field's payload opens with the count; every field's payload
then carries WHOLE RECORDS, and the fields concatenate in order:

    node_count (u64)
    node_count records, back to back:
        type id (u64), length (u32), body
```

- **`0xFFFF` is a RESERVED field id.** §5's fold reaches it and ordinary
  names land there, so the compiler refuses a field name — or a `was` —
  whose id does (§11).
- **Kind `12` is §3's opaque byte payload**, so a reader that cannot name
  the id skips each field by its `L` and counts it **unknown** (§4). No
  new skip rule, and no ceiling: **the field repeats.** That is the one
  exception to §3's last-occurrence-wins, and it belongs to this reserved
  id alone — every other repeated id still keeps the last.
- **A RECORD NEVER STRADDLES A FIELD.** A writer opens the next field
  when the record it is about to write would not fit in this one, so
  every field holds a whole number of records and every multi-byte read a
  reader makes lies inside one contiguous payload. A reader may therefore
  scan the fields one at a time, in order, exactly as it would scan one
  stream — no segmented cursor, no copy to make a body contiguous, and so
  §6.5's "the load path allocates nothing" stays literally true and the
  generated body decoder never learns that chunking exists. The cost is
  under one record of slack per field plus the field's own seven bytes.
- **The chunking is deterministic**, which is what `measure == save`
  needs: a writer fills each field as far as whole records allow, up to
  `0xFFFFFFFF` bytes, and opens another. A reader accepts any chunking a
  writer chose — a short field is legal input — and byte-identical output
  against this implementation requires matching the fill rule, as
  matching declaration order does (§3).
- **The node table is whole or it is nothing.** Numbering is positional
  across the concatenation, so a field that cannot be read cannot be
  dropped without renumbering every record after it. A node-table field
  arriving under a kind other than `12`, a record whose length runs past
  its field, or bytes left over inside a field make the whole table
  **malformed**: every pointer in the save reads null and one event is
  counted. A reader never salvages part of a numbering.

  **So resolution cannot be inline**, and that is a consequence worth
  stating rather than leaving to be discovered: the node table is written
  last (below) and found by id, so a reader has already read `head = 2`
  before it learns whether the table can be read at all. A conforming
  reader therefore either DEFERS every index until the table is known
  good, or nulls every index it stored when the table turns out
  malformed. No index ever resolves against a numbering that failed.
- **A save's node bodies have NO aggregate ceiling**, and the only thing
  record-aligned chunking cannot frame is a single record larger than a
  field — which a node body may not be in any case (below).
- **Only the ROOT body carries the node table.** No nested body ever does
  — not a by-value nesting, not an array element, not a union arm, not a
  variable-length table nested by value inside another (§2.2), and not a
  record. A save has one numbering and every index anywhere in it names
  that one.
- This implementation writes the node-table fields LAST in the root body,
  after the root's own declared fields, so that **a reader which gives up
  inside the node table has already decoded the ROOT'S OWN FIELDS** — the
  node table is the large part and the part most likely to be damaged,
  and a reader that dies a gigabyte into it still holds the root's real
  values. It buys nothing for a reader that gives up EARLIER, which is
  the ordinary case for a build that does not have kind `17` (§14): that
  one stops at the first pointer field and never reaches these at all.
  Field order is not part of the contract (§3), so a reader finds them by
  id.
- A root that reaches no nodes writes none of them, like every other
  empty thing (§3).
- **The record scan is authoritative.** `node_count` is data from the
  wire: a reader scans records until the fields are consumed and takes
  what it finds, and a `node_count` that disagrees with the scan is
  **malformed**. Nothing — no directory, no region, no buffer — is sized
  from `node_count` before the scan has confirmed it.
- **The `unknown` count is per TRANSPORT FIELD, not per schema
  difference.** A reader that cannot name `0xFFFF` counts one for each
  field the node table rode in, so a large save reports several unknowns
  where a small one reports a single unknown, and neither number is a
  count of things the schemas disagree about. A tool reporting evolution
  differences should read the count that way.

**A node record.**

- The **type id** is the target table's NAME under `fnv1a64`, with a
  result of 0 rebounding to 1 — 64 bits because a table name is the one
  vocabulary scoped to a WHOLE unit closure rather than to a single table
  or enum, so its collision population is the largest on the wire and the
  cost of ending the question is eight bytes a node. Two tables in one
  closure whose ids collide are still a compile error naming both (§11);
  at 64 bits, in a closure of a thousand tables, the chance is about
  `3 × 10⁻¹⁴`.
- The type id is what makes the node table decodable by a linear SCAN
  instead of a traversal, and that is why it is on the wire at all.
- The **length** is a `u32`, and **a node body that would exceed
  `0xFFFFFFFF` bytes is a SAVE-TIME REFUSAL** naming the node: measure
  and save return failure, and nothing truncates. The case is reachable —
  two 2 GiB `*bytes` in one table are four gigabytes of body under §2.5 —
  and it is refused rather than widened, because the repair is more
  nodes, which is the shape the flat encoding wants anyway, and a `u64`
  length would cost four bytes on every node in every save to frame a
  structure nobody should build. The ceiling that had to go was the
  AGGREGATE one, and the repeating field removed it.
- The **body** is an ordinary table body — fields, then the `u16` zero
  terminator, exactly as §3 describes. Everything inside is ordinary:
  by-value nesting still nests, arrays are arrays, guards still guard,
  buffers ride inline.

**A pointer field, and the constructs that ride on it.**

- A pointer to a table rides as `id (u16), kind = 17, index (u32)`.
- **Null is index `0`, and null is elided.** Absence and null are one
  value: a pointer takes no specified default (§2.1), so null is the only
  thing an absence could mean. §3's presence rule is unchanged — a
  non-null pointer ALWAYS rides, even when its node's body is entirely
  default, because otherwise null and "points at an empty node" would be
  one value on the wire.
- **`*T` and a by-value `T` are no longer one framing.** §3's
  three-spellings family becomes two: `T` and `?T` share kind `13`, and
  `*T` is kind `17`. An edit between a pointer and either of the others,
  or between a pointer and a plain `uint32`, is §4's kind mismatch —
  counted, never misdecoded, the field taking its default. The framings
  cannot merge while identity holds: a body that may be named twice
  cannot also sit inline at one of its names.
- **An array of pointers rides as an array of indices**: `kind = 14`,
  element kind `17`, `N`, then `N` `u32` indices. `[..8]*Node` is a legal
  declaration (§2.1) — an array of `*bytes` or `*string` is still refused
  (§2.5), because a buffer has no index to put in a slot. An array whose
  elements are all null elides like any other all-default array; an array
  with any non-null element rides as §3 frames arrays, so a null inside
  it is index `0` and every position is preserved. §3's element-kind rule
  applies unchanged, so an array of indices and an array of `uint32` do
  not decode each other either.

**Reading: every failure is one of §4's events, and none is new.**

- **An index above `node_count + 1`** — the valid indices are `0` for
  null, `1` for the root and `2 … node_count + 1` for the records:
  **malformed**, and the pointer stays null.
- **An index of `1` where the field's declared target is not the reader's
  own ROOT table**: **kind mismatch**, pointer null. The root carries no
  record and therefore no wire type id, so the reader's own root type is
  what the claim is checked against, and it is checked.
- **A node whose type id this reader cannot name**: skipped by its
  length, counted **unknown**. It KEEPS ITS INDEX — numbering is
  positional in the table, so one unnameable node never shifts the rest —
  and every pointer naming it reads null.
- **A node whose type id is not the one the field's declared target
  requires**: **kind mismatch**, pointer null.
- **A pointer field arriving as any other kind** — `13` from a schema
  that holds the field by value or as `?T`, `8` from one that holds a
  plain `uint32`: **kind mismatch**, skipped by its own kind's rule,
  counted, pointer null.
- **A node table that cannot be read whole** — a record whose length runs
  past its field, leftover bytes inside a field, a node-table field under
  another kind, or a `node_count` the scan does not match: **malformed**,
  and every pointer in the save reads null. The root body still reads on
  past the fields, so the root's own values survive — §4's
  framing-damage rule, applied to a numbering that has to be whole.

**LOAD IS A SCAN, and that is the whole of its bound.** Reading follows
no reference. `LoadMeasure` walks the records once — a record's type id
gives its storage size, its length gives the next record — and sums the
region. `Load` walks them twice: once to fill the region's node directory
(§6.3) from the framing, so that an index resolves whichever way it
points, and once to decode each body into its own storage. Every record
is visited a fixed number of times, in index order, and each is consumed
in full before the next begins, so the work is linear in the wire's bytes
and termination needs no argument beyond the record lengths and the end
of the stream. A pointer field's payload is a NUMBER: it is
bounds-checked and stored, never followed. There is no traversal on the
load path, and therefore no traversal bound — no depth cap, no visited
set, no ordering rule on the indices. The nesting that remains is
by-value nesting, whose depth is fixed by the SCHEMA and cannot be driven
by data, because §2 refuses by-value cycles. §14 records the two
traversal bounds that were weighed and rejected, and why a type id
removes the walk instead of bounding it.

**Identity is preserved: one index, one node.** The numbering is by first
visit, so a node three parents name takes one index and writes one body,
and a loader materializes one node and stores that index in all three
slots. §2.1's own example — a large `*Palette` several parents share — is
one node on the wire, in a builder and in a region alike (§6.3).

**A data cycle is refused at SAVE FROM A BUILDER, and the refusal is
free.** The numbering walk carries one entry per reachable node — that
map IS identity; a node must know its index to be named twice — so
colouring each entry while its descent is open costs one bit: a reference
to an entry still open is a cycle, named, and measure, save, `Cook` and
`Lock` all return failure. Nothing recurses away. The map is proportional
to NODES, never to bytes, and it lives on the AUTHORING side, where §6.5
licenses allocation.

**A region is not re-proved, and the claim stops there.** A save from a
LOCKED region needs no map — the region's node directory (§6.3) already
is the numbering — so it reproduces the structure it was handed, cycle
and all. A region `Lock` produced is acyclic because `Lock` refused
otherwise; a region LOADED from a wire is exactly as acyclic as its
writer made it. Load itself is safe on any input: it scans, it
terminates, it fabricates nothing. What a cyclic structure costs is paid
by whatever WALKS it, and a consumer walking untrusted table data — a
reflection dump, a text export (§16) — carries its own visit bound, the
way any graph walker must. §14 prices the reader-side check this version
does not spend.

**Framing, worked.** Given

```
table Palette { id int32 }
table Node    { value int32  next *Node  owner *Scene  palette *Palette }
table Scene   { head *Node   palette *Palette }
```

with `scene.head = A`, `A.next = B`, both nodes' `owner` naming the root,
and `A.palette`, `B.palette` and `scene.palette` all naming one `Palette`
P, a save writes:

```
root body (Scene)
  head     kind 17  2
  palette  kind 17  4
  0xFFFF   kind 12  L    the node table, in one field here
                    node_count = 3
                    rec 1  type Node     len  { value=1, next=3,
                                                owner=1, palette=4 }
                    rec 2  type Node     len  { value=2, owner=1,
                                                palette=4 }
                    rec 3  type Palette  len  { id=7 }
  0x0000                 terminator
```

The root is index 1; A is 2 (`scene.head`); B is 3 (`A.next`); P is 4 —
reached while descending B, BEFORE `A.palette` is read, because the walk
is depth-first. P is written once and named three times. `B.next` is
null, so it is not written at all. `owner = 1` names an index BELOW the
node that carries it: indices run in the walk's order, and no reference
has to respect that order.

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
  the content (§2.3). A field moved between `?T` and a plain nesting is
  not an evolution event at all — the bytes do not move. Moving one to or
  from `*T` IS one: a table pointer is kind `17` (§3.1), so the edit is a
  kind mismatch, below.
- **Kind mismatch** (a field changed type between builds): skipped, never
  misdecoded, counted. The kinds are a coarser vocabulary than the
  declaration side, so this catches a change of KIND, not every change of
  type: an enum field and a plain `uint16` field are both kind `7`, so an
  edit between the two is not a kind mismatch — the raw value is read as a
  variant hash, and lands on `None` unless it happens to name one. **A
  table pointer and a plain `uint32` field are NOT such a pair**, though
  both carry four bytes: a pointer index has its own kind `17` (§3.1), so
  an edit between them is an ordinary kind mismatch, counted in both
  directions. **An
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
change between `T` and `?T`, or between `bytes(N)` and `*bytes` — all of
it either invisible to the wire or counted in the report. Moving a field
to or from `*T` is a kind change and is counted (§3.1).

**Three edits that would otherwise be silent are made REPORTABLE by
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
- **Changing a table field between `*T` and a plain `uint32`** is the
  same shape one more time: a node index and a number are the same four
  bytes, and under a shared kind an index would read back as a plausible
  count in every case. The pointer index's own kind `17` (§3.1) turns
  that edit into a kind mismatch too. Each of the three cost a kind
  number and no bytes, and each closed a class that discipline alone
  cannot.

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

Each of the two has its own answer:

- **Flags** are answered by DISCIPLINE, stated as law: **append at the end,
  never insert or reorder**, and retire a name in place rather than freeing
  its bit.
- **Both** are answered by MACHINERY, opt-in: the committed tables
  baseline (§18) is the history the compiler does not keep, and it
  refuses either edit until the baseline moves with a recorded reason. It
  refuses the spelling changes above too, at compile time, ahead of the
  reader's report.

## 5. Identity: the name hash, `was`, and the collision refusal

**One hash serves three vocabularies.** A field's wire id, an enum
variant's id and a union arm's id are all `fold16(fnv1a32(name))` — the
fnv1a32 of the name, xored with its own high half and truncated to 16 bits,
with a result of 0 rebounding to 1. The rebound reserves `0`: it is the
field terminator, the enum's `None` and the union's empty arm, and no
declared name can ever land on it.

**A fourth vocabulary rides at a different width, and the reason is
scope.** A TABLE's own name is a node's type id on the wire (§3.1), and
it is `fnv1a64( name )` with the same 0-rebound — 64 bits, not 16,
because a table name is the only id scoped to a WHOLE unit closure rather
than to one table's fields or one vocabulary's variants, so its collision
population is the largest the wire has. Two tables in one closure whose
ids collide are refused the way two fields are (§11), and at 64 bits that
refusal is a formality rather than a schedule risk. The field id `0xFFFF`
is reserved as well, for the node-table field (§3.1): the 16-bit fold
reaches it and ordinary names land there, so a field name — or a `was` —
whose id does is refused naming the field.

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

**A block root has a THIRD life and it is §19's.** A table that declares a
section (§2.7) is fixed-size, so its header is a value like any other, but
its instance lives at the base of a block rather than on its own and it has
no wire form at all. The surface below is the wire's; the block's is stated
where the block is.

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

**Where the generated code lives.** A table unit file emits two files:
`<Base>Table.h`, which a consumer includes, and `<Base>Table.cpp`, which a
consumer COMPILES when it uses what is in it. The header carries the
storage structs, the wire codecs and the reflection descriptors — inlineable
code, templates over a context, and constant data. The `.cpp` carries the
table RUNTIME: today that is the text form's walk (§16), and anything else
that is neither a template nor constant data is a candidate for it on
measured evidence (§13.5). The TYPE wire has no such file and does not want
one: a type is a struct and its codec, and it stays header-only.

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
  slack, the root at its base. The walk is the same depth-first pre-order
  numbering the wire uses (§3.1), carrying the same one-entry-per-node
  map, so it terminates in one visit per node, a shared node is packed
  ONCE, and a data cycle is refused here exactly as it is at save. The
  mutable life is released. Locking
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

**A region reference has no required sign.** A region is packed in the
same depth-first pre-order the wire numbers nodes in (§3.1), so a node's
FIRST reference points forward; every later reference to that node points
BACK at the one body it already has, which is what makes one node one
node in a region as well as on the wire. Sharing and a back-reference are
the same fact, and nothing validates a reference by its sign (§7).

**A region carries a NODE DIRECTORY**, and it is the wire's numbering
made resident: a trailer of one entry per numbered node, in index order,
each `offset (u64), type id (u64)` — position `i` describing node index
`i + 1`, so position `0` is the root at offset `0`. A node's extent runs
to the next entry's offset. The offsets of MATERIALIZED nodes ascend;
`0xFFFFFFFFFFFFFFFF` is the not-materialized sentinel — a record whose
type id the loading build could not name (§3.1) — distinct from every
real offset including the root's `0`, so an index resolving through it
yields NULL and can never fabricate the root.

Every node starts at its own type's alignment and the directory's offsets
are those padded starts, so "is a directory entry" and "is aligned" are
one check rather than two. A node's own buffers (§2.5) are packed inside
its extent, so a buffer reference is checked against the extent of the
node whose slot names it — no search at all.

**The directory is ATTRIBUTION, and attribution is separable.** Nothing
that READS a structure touches it: a deref is one add on a self-relative
offset, in a locked region and a loaded one alike. It exists for three
jobs and all three are finished before the first read — `Load` resolves
wire indices through it, `Lock` fills it from the pack it is already
doing, and a validating reader or a tool checks a region against it (§7).
`LoadMeasure` reports the data bytes and the attribution bytes
separately, so a caller may place them together or apart and may release
the attribution once `Load` returns. `Cook` writes them as two parts for
the same reason (§7).

**The price, stated.** Twelve bytes a node on the wire (an 8-byte type id
and a 4-byte length per record) and sixteen a node of attribution, which
a shipped build need not carry at all:

| node size | nodes / GiB | wire record headers | attribution |
|---|---|---|---|
| 32 B | 33,554,432 | 384.0 MiB (37.5 %) | 512.0 MiB |
| 64 B | 16,777,216 | 192.0 MiB (18.75 %) | 256.0 MiB |
| 128 B | 8,388,608 | 96.0 MiB (9.38 %) | 128.0 MiB |
| 256 B | 4,194,304 | 48.0 MiB (4.69 %) | 64.0 MiB |
| 1 KiB | 1,048,576 | 12.0 MiB (1.17 %) | 16.0 MiB |

The lever is node size, not encoding. A structure of a great many tiny
nodes pays the most and the answer to that is fewer, larger nodes; the
64-bit type id is the deliberate purchase of a question that never has to
be asked again (§5).

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
  wire_size, &report )` decodes into it and returns the root. Under
  §3.1's node table that measure is ONE SCAN — a record's type id gives
  its storage size, its length gives the next record — and it reports the
  DATA bytes and the ATTRIBUTION bytes separately (§6.3), because the
  caller may place them apart and may release the attribution once `Load`
  returns. `Load` is two passes over the same records: it fills the node
  directory from the framing, then decodes each body into its own
  storage, so a forward index resolves without scratch. The load path
  allocates nothing.
- **`LoadMeasure`'s answer is also the DEFENCE, and a caller is expected
  to bound it.** The smallest legal record is fourteen wire bytes and it
  commands `sizeof( T )` region bytes, so a wire can ask for far more
  memory than it occupies. The caller owns the allocation precisely so it
  can refuse a number it did not expect; nothing in the runtime decides
  that for it.
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

**Cooking is fundamentally an optimization.** Every rule in this section
is a consequence of that one sentence, and none of it is a second format
of record.

**What it is for**: reducing the load time of assets, and nothing else —
don't parse, just point at an mmap'd data structure loaded as it stands,
and have it work.

**What it is**: converting a table data structure into a SPECIFIC VERSION
that can be memory mapped, endian-fixed and loaded quickly by a build at
the exact version it was cooked to. That is cooking.

```cpp
int64_t data, attribution;
SceneCookMeasure( builder, &data, &attribution );
SceneCook( builder, buffer, data, attribution );   // write it

const Scene * scene = SceneOpen( bytes, size );    // point at it, or NULL
```

**The pipeline.** `schema pack` writes the WIRE, and the wire is the
format of record (§17). A cook is produced beside it, next to the build
that will read it — on the machine or on a distributed cook farm — for
exactly the layouts that build knows. A game loads its own cooked assets
and never a foreign one; tooling reads and writes the generic wire,
because the generic form is the one that carries every version. And if
load time does not demand the accelerator, the game just uses the generic
table: cooking is a choice made per asset, never a requirement of the
format.

The example pair is the whole rule. A huge data file naming every mesh in
the game, or every texture — that is a cook. A configuration file small
enough that the cost of loading it does not matter is the wire, and stays
the wire, and keeps the flexibility that comes with it.

- **The header build-locks it**: a magic (which is also the byte-order
  check), a LAYOUT ID, and the lengths of the two parts below. The
  reserved words are reserved: a non-zero one means a writer used a form
  this build does not understand, and `Open` refuses rather than ignoring
  it.

  A cooked file never crosses builds, so most of the header's shape is
  the implementation's business. **Three widths are not**, because each
  one decides something semantic:
  - **The magic is read BYTEWISE, before anything else**, since it is
    what establishes the byte order every other header field is written
    in.
  - **The LAYOUT ID is 64 bits.** Under the rule below a matching id
    means `Open` checks nothing further, so the id is the sole guard
    between a runtime and a foreign region; it is sized like a digest,
    not like a version counter.
  - **Both part lengths are 64 bits.** `CookMeasure` answers in
    `int64_t`, and a 32-bit header field would reimpose at cook time the
    ceiling §3.1 just removed — on exactly the huge mesh or texture
    catalogue a cook exists for.

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

  A format that rarely changes therefore keeps its id across builds by
  construction: the id follows wire ids and offsets, so a layout nothing
  touched does not move, and a mesh catalogue cooked last year opens
  against this year's build if nothing in its closure changed.
- **A cooked file is TWO PARTS, and the header locates both.** The DATA
  comes first, at the region's alignment: it is `Lock`'s region written
  verbatim, the root at its base, and it is what the runtime points at.
  The ATTRIBUTION follows it: the node directory of §6.3, which says
  where every node starts and what type it is. Nothing that reads the
  structure touches the attribution, so a shipped build need not carry it
  at all — the header records its length as zero and the file is just
  data.
- **`Open` checks the header and points, and this is the WHOLE check**,
  because nothing else is checked at all: the magic, the byte order it
  establishes, the layout id, the two part lengths against the `size` the
  caller passed — a truncated file refuses — and the alignment of the
  base. On a match the bytes ARE what this build wrote, in this build's
  layout and this build's byte order, so there is nothing to validate and
  nothing to fix up: `Open` returns the root. That is the runtime path
  and it is the default. On any failure it returns NULL and the caller
  falls back to a wire load, which is the path that carries every
  version.
- **`OpenValidated` is the other entry point, and it is named rather than
  implied.** It takes a cooked file whose provenance the caller does not
  trust, or one a tool is diagnosing, and checks the data against the
  attribution before returning anything. There is no SILENT bypass
  anywhere: a caller either gets the layout-id guarantee, or asks for the
  check by name. A file with no attribution part cannot be checked, so
  `OpenValidated` refuses it and says which part is missing. **A cook
  that arrived from somewhere else is where it earns its keep**: the
  pipeline above contemplates a distributed cook farm, and a file that
  crossed a machine boundary carries a matching layout id and nothing
  else — a caller that wants more than the id's word takes the check by
  name.
- **The check is a SCAN of the attribution, not a traversal of the
  graph.** Two passes, in order:
  1. **The directory itself**, linearly and with no state: it lies inside
     the file, every type id names a table this build has, the
     materialized offsets ascend, each is aligned for its own type, and
     each node's storage fits before the next entry. A sentinel entry
     (§6.3) refuses here — a cooked file is an accelerator and cannot
     carry a hole.
  2. **Every node, in directory order.** An entry's type id says which
     walk to run over that node; each pointer slot must resolve to an
     offset the directory NAMES, with the type the declaration requires;
     each buffer reference must lie inside its own node's extent; each
     count companion must sit inside its declared bound — including the
     companions of fixed-size tables and plain types nested by value,
     whose counts bound a walker just as a table's do. It reads no field
     value and decodes no payload.

  Every node is checked exactly once because the scan visits each entry
  once, and no reference is ever FOLLOWED, so no reference can cause a
  second visit. A forged file whose references alias into a
  legal-looking DAG costs nothing extra, and neither does a cycle. The
  scan also checks the nodes NOTHING POINTS AT, which no traversal from
  the root can reach. The cost is `O(R + P log N)` — linear in the
  region, plus one search per pointer slot — with no allocation and no
  per-node state, and it terminates on every input.
- **Pack order and checking are INDEPENDENT.** The region is packed in
  pre-order (§6.3) because that is the order the wire numbers nodes in
  and one order is simpler than two, but nothing in the check reads that
  order, so the layout can change without silently weakening it.
- **A member's walk lives in the file that DECLARES it.** A variable table
  may nest a plain type, a fixed table or another variable table declared
  anywhere in the unit, and the walk for each is emitted once, by its
  declaring file — including by a file that declares no variable table of
  its own, and for a member nothing points at. The referencing file picks it
  up through the header it already includes. Emitting per referencing file
  would define each walk twice; emitting only where pointers are declared
  leaves the by-value members of a value-only file undefined.
- **The cook of a FIXED root table is the same idea with nothing in it.**
  A fixed-size table is one struct (§6.1), so its cooked form is the
  struct's bytes behind the header: memcpy it, or point at it where it
  lies. There is no region, no node table and no attribution, and the
  layout id is the whole of the check.
- **Every refusal is loud and the fallback is a real wire load**: wrong
  magic or byte order, a layout id this build did not produce, a
  truncated file, an unaligned base — and, under `OpenValidated`, an
  attribution part that is missing, leaves the file, carries a sentinel
  entry, names a type this build does not have, does not ascend, or
  overlaps a node with the next; a reference that leaves the region, that
  the directory does not name, or that it names as another type; a
  misaligned reference; a buffer outside its node's extent; a count
  companion outside its declared bound.
- **Alignment.** The header pads the data part to the region's alignment,
  so a base the allocator or `mmap` gave you is already aligned; `mmap`
  gives page alignment for free.
- **Endianness is part of the COOK, not of `Open`.** A cook is produced
  in the byte order of the build it is cooked for — the cook knows its
  target — so a matching layout id already means a matching byte order
  and `Open` never fixes anything up. Cooking for a foreign target is
  where a byte swap would live if one is ever wanted (§15); a cooked file
  whose recorded order is not this build's is simply not this build's
  file, and refuses.

Prior art gets one sentence, and it is the contrast: systems that made
pointed-at access their ONLY wire coupled access to evolution and paid
for it. Here the tolerant wire stays the format of record and the cooked
form is a build-locked accelerator beside it, produced only where load
time asks for one. The two-form split is the design.

**And prior art gets one MEASURED sentence, from the case §19 exists for.**
The render data this document's second gate is held to (§12) was built with
flatbuffers once and the build was abandoned — in the owner's words, *"I
used to use flatbuffers to build render data, but it was too slow because it
was not parallizable."* That is the specific failure the sectioned block is
shaped against: a per-frame producer that has to go wide cannot afford a
builder with a serialization point in it, whatever the read side costs. A
cook does not answer it either — a cook is produced from a builder by a
single-threaded `Lock` (§6.2), which is exactly the shape that lost.

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

**A SECTION field carries its element's descriptor and the three offsets of
its triple** — `section` marks the field, `table` names the ELEMENT's
descriptor, and `offset_offset`, `count_offset` and `stride_offset` name the
three members inside the sixteen-byte triple (§2.7) — so a tool handed a
block and its header's descriptor walks every section without knowing the
spelling that produced it, reading the pitch from the data rather than
assuming it. The declared MAXIMUM rides beside them in the column an array's
bound already uses, and a table's own **`block`** flag says it is a block
root, so a tool can tell a header from an ordinary fixed table (§2.7).

These columns exist in every unit, whatever its mode — they describe the
LANGUAGE, and a fixed-size table can declare all of them. Only the two
POINTER columns below are conditional (§2.2).

A unit that has pointers carries three more facts, and a unit that has
none carries not one of them (§2.2): a field's **`is_pointer`** flag —
whose `table` member then names the TARGET table's descriptor, NULL for a
`*bytes` or `*string` because a buffer is not a declared table, and whose
`elem_size` is the reference slot's width; a type's derived
**`variable`** mode, so a tool can tell at runtime which of §6's two
lives a table has without being told; and a table's own **node type id**
(§3.1), so a tool can map a node table's records — or a region's node
directory — onto descriptors with no schema files on hand. The
compile-time refusals those ids bring (§11) apply to EVERY unit, as the
27 generated spellings do, because a unit gains and loses pointers as an
edit and a name that was free yesterday must not become a collision
tomorrow. A self-referential pointer resolves to its own type's
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
  guarantees the option; callers choose whether to take it. **A pointer
  graph pays a serial prologue first**: the node numbering (§3.1) is one
  single-threaded depth-first pass over every reachable node, and no
  record can be written until it has run. It is a pointer-chase over
  nodes, not a pass over bytes, and it leaves the scatter-write itself
  untouched — records are top-level and their sizes are independent, so
  the prefix-sum is simpler than it was over nested bodies.
  **measure == save at exact capacity** is a hard invariant, held by a
  mandatory battery across the corpus and across pointer graphs: saving
  into a buffer of exactly measure's answer always succeeds and
  byte-matches a roomy save, and one byte short always refuses.
- **Going wide on the BUILDING side** is §6.4: allocation is thread-local
  and nothing ever moves, so N workers fill one arena with no lock and no
  per-node atomic.
- **The BLOCK is the strongest form both properties take** (§19), and it is
  where the requirement for them came from. A block is one flat extent whose
  every reference is a block-relative offset, so it relocates by plain
  `memcpy` with no fix-up at all — no `Lock`, because it is born compacted.
  And it goes wide with nothing to synchronize: its storage is sized from the
  declared maxima and its layout is fixed in one pass over the SECTIONS
  before any worker runs, so every record's address is settled ahead of the
  scatter, N workers fill disjoint records, and there is no arena, no
  allocation, no atomic and no per-node prologue pass. The
  builder's four models (§14) all exist because a general structure cannot
  know its bound; a block declares one, which is why item 3 there — reserve
  the max and never resize — is exactly what a block does.

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
  outside a table body; a specified default on a pointer field. An array
  of table pointers is legal (§3.1); an array of `*bytes` or `*string`
  is not (§2.5).
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
  specified default on one; an array of them — a buffer takes no node
  index (§3.1), unlike an array of table pointers, which is legal; `?` on
  one, because a null reference already IS absence.
- **Sections** (§2.7), each naming the section field and, where a second
  declaration is at fault, that one too:
  - **a section outside a table body** — a `type` body refuses one as it
    refuses a pointer, and so does a `union` arm's payload;
  - **a section whose bound is not `[..N]`** — a fixed `[N]T` makes the count
    a constant and a keyed `[E]T` has no count at all;
  - **a section whose element is a pointer, a `*bytes`, a `*string`, a
    VARIABLE-LENGTH table, or a table that declares a section of its own** —
    a block is one flat pointer-free extent, and sections do not nest;
  - **`| stride = N` where `N` is smaller than the element's `sizeof`, or is
    not a multiple of the element's alignment** — the first would overlap
    records, the second would misalign every record after the first;
  - **`?` or `*` on a section** — a count of zero is already an empty
    section, and a block holds no pointer;
  - **a BLOCK ROOT nested by value, pointed at, used as an array or section
    element, or named as a union arm's payload** — its section offsets are
    block-relative and mean nothing anywhere but a block's base;
  - **a specified default on a section** — the triple is written by the block
    surface (§19), never by a declaration;
  - **`section` as a declaration name** — it is a contextual keyword in type
    position (SPEC.md §4.2);
  - **a field of a block root named `magic` or `layout_id`** — those two are
    the header's generated prologue (§19.1), as `<field>_present` is an
    optional's generated companion;
  - **a section under a backend that carries none** — which today is every
    backend (status, above), refused with §19 cited and never emitted with
    the block surface missing.
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
  ride; and, for a BLOCK (§2.7), a field inserted before the end of a
  section element, reordered, removed or retyped, a field appended PAST its
  stride, a stride shrunk, or a section's element swapped, removed or moved
  earlier. Overridden only by moving the baseline with a recorded reason.
- **A save-time data cycle reached from a builder** (§3.1): measure,
  save, cook and `Lock` all return failure with the cycle named. Nothing
  recurses away. A region loaded from a wire is not re-proved, and a save
  from it reproduces what it was given.
- **A node body past `0xFFFFFFFF` bytes** (§3.1): a record's length is a
  `u32`, so measure and save return failure naming the node rather than
  truncating it. Two 2 GiB `*bytes` in one table reach it; the repair is
  more nodes.
- **A field id colliding with the reserved node-table id `0xFFFF`**
  (§3.1) — by hash accident or through `was` — naming the field. **Two
  tables in one unit's closure whose NAME ids collide** (§5), naming
  both: a node's type id is its table's name hash.
- **Cooking a region that carries a not-materialized node** (§6.3): the
  region loaded correctly and reads correctly, but a cooked file is an
  accelerator and cannot carry a hole. `Cook` returns failure naming the
  index.
- **A cooked file this build cannot point at** (§7): wrong magic or byte
  order, a foreign layout id, truncation, or an unaligned base. `Open`
  returns NULL; the caller falls back to a wire load. Under
  `OpenValidated` the attribution's own refusals join it: a missing or
  out-of-file attribution part, a sentinel entry, a type this build does
  not have, a directory that does not ascend or overlaps a node with the
  next, a reference that leaves the region or that the directory does not
  name or names as another type, a misaligned reference, a buffer outside
  its node's extent, or a count companion outside its declared bound.
- **A declaration colliding with a generated table spelling.** Tables and
  types share one symbol table (§13.1), which is what makes the generated
  surface unprefixed and collision-free — so every name a closure member
  claims is refused to everything else. A member `X` claims `X` followed by
  each of these **27 suffixes**, and a declaration spelling one of them is
  refused naming the collision:

  ```
  Measure  MeasureBody  Save  SaveBody  Load  LoadBody
  LoadMeasure  LoadMeasureBody  LoadBuilder  TableType  Builder
  At  Root  Emplace  Pack  PackMeasure  OpenWalk
  Cook  CookMeasure  Open  OpenValidated  LayoutId  TableFields  TableInfo
  FromJson  ToJson  ToJsonMeasure
  Block  BlockStorage  BlockBegin  BlockBytes  BlockMaxBytes  BlockOpen
  BlockOpenCompatible  BlockLayoutId  Counts
  ```

  The set is claimed for EVERY closure member, not only pointer-bearing
  ones: a table gains or loses pointers as an edit, and a name that was
  free yesterday must not become a collision tomorrow. The nine block
  spellings are claimed on the same terms, for the same reason: a table
  gains a section as an edit.

  **A BLOCK ROOT claims a little more, because its section accessors are
  named after its fields.** `<Header>` followed by the PascalCase of each
  section field's name is the accessor that hands back that section
  (§19.2), so `RenderFrame` with a `ships` section claims
  `RenderFrameShips`, and a declaration spelling it is refused naming both.
  A language whose accessors are members spells the same name on the block
  type instead, and claims nothing at file scope for it. This part of the
  set moves with the declaration, which is why it is stated as a rule
  rather than as a list.
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

**There are TWO gates, and a real game's data is both of them.** The first
is the content pipeline — the config and asset files tools write and the
game loads — and it is held below. The second is the per-frame render data
the game hands its host engine, and it is §12.1. They test different halves
of the same claim: the first says the tolerant wire can carry a format
nobody prescribed, and the second says the language can express a
performance-critical ABI between two languages with nothing left over. A
construct that clears one and not the other has not cleared the gate.

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

### 12.1 Gate 2: the render data

**The second gate is SPEED, and it is measured.** A real game's per-frame
render data — the block a simulation hands its host engine sixty or more
times a second — must be expressible as declared tables with nothing left
over, with BOTH sides generated: a C++ producer filling the block wide, and
a C# consumer POINTING at that block and reading records in place. The bar
is not that it works. The bar is that it is at least as fast as the
hand-written scatter it replaces, on both sides, and that nothing about the
declaration made it slower.

**Why the bar is stated that way.** The producer this gate is held to is
multi-threaded by design, and the previous attempt at a general answer was
abandoned for exactly this reason — flatbuffers built the render data once
and lost, *"because it was not parallizable"* (§7). So a construct that
serializes the build, allocates per record, or forces a copy on the read
side has already failed the gate, however clean the declaration reads. The
sectioned block (§2.7, §19) is the shape that answers it, and the pitch is
the load-bearing part: **striding is what makes the interop fast** —
blittable records at a fixed, declared pitch that both generated sides
point at, with no marshalling and no copy anywhere in the frame.

**The shape the gate is held to.** One block root (§2.7), fixed-size down to
the leaves: a header of sections, each section a strided array of a
fixed-size record, no pointer anywhere in the closure. The block's storage is
sized from the declared maxima and its layout is fixed before any worker
starts, so N workers fill disjoint records with no lock, no atomic and no
per-record synchronisation (§9, §19). The consumer maps the block, checks
the header once, and
iterates each section at the stride the header gives it. The layout contract
— every record's size, every field's offset, every section's stride — is
asserted by GENERATED code on both sides (§19.4), which is the half that
replaces a hand-kept mirror with a compiler's word.

**What "with nothing left over" means here, concretely.** The dogfood's
render path today holds its layout contract by hand on both sides: a wall of
`static_assert`s naming each record's `sizeof` on the C++ side, and a
hand-written blittable mirror on the C# side that a person must edit in the
same commit. Clearing the gate deletes both — the mirror because the C#
backend generates the blittable struct (#287), and the asserts because the
generated pair asserts the same facts from one declaration. A field added at
the end of a record must reach both languages from one edit to one schema
file, and the compiler must be what says so.

**The measured bar, and where it is taken.** A timed parallel build of a
representative frame, generated block against the current hand-written
scatter, paired in one sitting under the bench rules this repo already runs
under: gated goldens first, medians paired, contaminated runs discarded
whole. Two numbers, both required — **the producer's per-frame build time,
and the consumer's per-frame read** — and the generated form clears the gate
only when neither is slower. A regression on either is a defect to explain
or close, not a trade to license: this is the fastest-correct mission
applied to the rung the block sits on.

**Its PROVENANCE, recorded because it explains two older requirements.**
§9's relocatability and §6.4's multi-threaded, lock-free builder were
written FOR this case — *"this is where the relocatable and multithreaded
builder requirement came from"* (§13.1) — not for the config and asset files
gate 1 holds. Gate 1 proved the tolerant wire could carry a content
pipeline; gate 2 is the original requirement made explicit, and the block is
the form in which both properties are strongest (§9).

**This gate is per-language too, and it takes TWO languages at once**, which
gate 1 does not: a block whose producer and consumer are the same language
proves nothing about the ABI the construct exists to be. C++ and C# together
are the gate; a third language joins it as its backend lands (§15).

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
  be multithreaded"; then "I prefer lockless if possible." **And where the
  requirement came from**, ruled 2026-09-02: "this is where the relocatable
  and multithreaded builder requirement came from" — the render data
  (§12.1), not the config and asset files. Both properties were written for
  a per-frame block scattered into by N workers and handed across a language
  boundary to be pointed at; §19 is that case made a construct, and §9 says
  why the block is the strongest form each property takes.
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

### 13.4 The cooked form, ruled

- **What it is for**: "cooking is fundamentally an optimization", and
  "the only time we need the optimization is to reduce load times for
  assets. in short, it's the flatbuffer style optimization, don't parse,
  just make something that points at the mmap'd data structure loaded as
  is and *works*."
- **What it is**: "convert the table data structure into a specific
  version that can be memory mapped, endian fixed up and just loaded
  quickly by a build at the specific version it is cooked to" — "that's
  cooking."
- **Where it runs and what it binds**: "the cook would be LOCAL to the
  version your game needs", "or on some distributed system", "your game
  will only ever load the locally cooked asset specific to the versions
  that build knows", and "the generic table (tooling) needs to handle all
  versions."
- **When it does not run at all**: "or, the game, if cooking is not
  required for speed, would just use the generic table."
- **Which files earn one**: "imagine a huge data file that specifies all
  the meshes used in the game for example, or all the texture files" —
  "that would be a cook"; against "Config.bin and Assets.bin are small
  enough that the overhead of loading doesn't require a cook", so the
  dogfood's own two files stay on the generic wire.
- **The version rule**: "cooked data at a specific version should just
  load. It should probably only be loadable at that specific version", "or
  a subset of versions AT MOST", "consider an asset format that rarely
  changes over time." Exact layout-id match is what §7 states; the
  declared compatible set is the only widening contemplated, and it is a
  named follow-on (§15).
- **Where the per-node bookkeeping lives**: "Can we keep the overhead and
  tracking down for the non-cooked version only, and then when cooking,
  split it into two parts, the cooked data, and the cooked attribution
  data so we can separate if needed. We may not need at runtime in a
  shipped build for example (it is read only and not mutable)."
- **On sizing the wire's ids**: "u64 now", and "u64 now, why fuck around."
- **On optimizing ahead of evidence**: "we can worry about optimization
  later, unless there are specific decisions we need to make now that will
  knowingly make things slower."

### 13.5 Header versus translation unit, ruled

The JSON walk (§16) first shipped as inline code in every table header, and
it cost every translation unit that included one — measured at +11% to +15%
compile time on an empty unit including a single corpus header, whether or
not that unit ever read a text.

- **The ruling**: "It would be best if it were written to a corresponding
  cpp file so it doesn't need to be included every time."
- **Why now and not before**: "As we get more complex stuff in
  types/tables, needing a .cpp file now makes more sense."
- **And the line that does NOT move**: "I think it's OK for types to remain
  header only."

So the split is by WIRE, and it is a real boundary rather than a
convenience:

- **The TYPE wire stays header-only.** A type is a struct and its codec —
  generated code that a compiler folds into the caller, with nothing to
  compile once and link. Nothing in SPEC.md's generated output gains a
  translation unit.
- **A table unit gains `<Base>Table.cpp` for its RUNTIME.** The JSON walk
  is what lives there today: a body of non-template, non-constant code that
  every consumer would otherwise re-parse. The storage structs, the wire
  codecs and the reflection descriptors stay in the header, because they
  are respectively inlineable, template-parameterised over a context, and
  constant data a tool reads directly.
- **What may follow it, and on what evidence.** Any further table runtime
  that is neither a template nor constant data is a CANDIDATE for the same
  file, decided on measurement when a measurement exists. The arena and the
  cooked form are not moved: their surfaces are templates over a sink or a
  context (§6.4, §7), so they have no single definition to emit, and no
  number has been taken for them.

### 13.6 The sectioned block, ruled

Owner rulings, 2026-09-02, in the order given.

- **The requirement**: "New requirement just dropped. Using tables, or
  types, we should be able to implement the render data" — and the scope,
  "*including* the blittable arrays with stride."
- **Tables, not types, and why**: "probably with tables, I would imagine.
  since render data is BIG", "so it's more table like than for example, a
  type." A block root is a table (§2.7), and its mode derives FIXED (§2.2).
- **The producer is multi-threaded by design**: "note that the hand-written
  code that walked and generated render data from C++ is multi-threaded by
  design" — which is the constraint the layout is shaped by (§19.2).
- **Where two older requirements came from**: "this is where the
  relocatable and multithreaded builder requirement came from" (§13.1).
- **The prior attempt, and why it lost**: "I used to use flatbuffers to
  build render data, but it was too slow because it was not parallizable"
  (§7) — which is what makes gate 2's bar measured rather than stylistic.
- **The pattern, named**: "Do you see how the render data is sort of its own
  structure, a header with sections that it points to" / "each section being
  a strided array of some type?" / "This is its own pattern." §2.7 is that
  pattern as a declaration.
- **What the stride buys, first**: "striding is necessary for fast interop
  between C++ and C#" — "so that's the real thing here." The pitch is the
  point: blittable records both generated sides point at, no marshalling and
  no copy (§12.1, §19).
- **What the stride ALSO buys, and its reduced weight**: "the benefit of
  striding is that i can add new fields at the end of types/tables, without
  C# exploding", "because C# just doesn't know about the new fields yet, and
  stride is bigger than struct width" — then, refining: "It may be obsolete
  now, but this was the original intent", "If we generate both render data
  C++ and C# side now, it's less of a concern." So headroom is a real
  property and a secondary one; the layout contract and the baseline are the
  guard of record (§19.4, §19.5).
- **The purpose, in one line**: "this is a 'nice' property to get some sort
  of more robust structure (ABI) between C++ and C# without hardcore
  versioning", "because both sides were previously manually updated."

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

**The node table's shape** (§3.1). The whole design turns on one
decision — a TYPE ID on every record — and the rest follows from it:

1. **A type id per record — KEPT.** It is what makes a load a SCAN rather
   than a traversal: the reader learns a node's type from the record, not
   from the pointer field that names it, so it never follows a reference
   to know what it is looking at. It also keeps §6.5's promise literally
   true — `LoadMeasure` reads framing and no field value, and a type id
   is framing — and it lets a tool decode a node table with descriptors
   alone (§8).
2. **A visited-index guard on load — REJECTED.** It is what an untyped
   node table would force: type the nodes by traversing, and bound the
   traversal with a set. That is state proportional to the graph on the
   READING path, which §6.5 forbids. A type id removes the walk instead
   of bounding it.
3. **An index-monotonic rule — REJECTED.** Requiring every reference to
   name a larger index would bound a traversal with no state at all, but
   it holds only under a TOPOLOGICAL numbering, and pre-order numbering
   is not topological: `Scene { a *X, b *Y }` with `X.p` and `Y.p` naming
   one `P` numbers `X = 2, P = 3, Y = 4`, and `Y`'s reference names 3
   from 4. Topological order also cannot be assigned in one descent — a
   node's position is known only once its whole subtree is — and it would
   give the node table a different order from the one the region is
   packed in, for no property either form needs.
4. **A hybrid — inline the singly-referenced pointee, table only the
   shared ones — REJECTED.** It would have kept a pointer field and a
   by-value nesting under one framing for the common case. It also
   reintroduces the exact problem the flat table removes: a 200-node
   chain is singly referenced at every link, so it would still nest 200
   deep.
5. **A NEW KIND for the node table — REJECTED.** Skipping is defined only
   over §3's closed set, so a reader that does not have a new kind cannot
   skip past it. That objection has a scope worth stating exactly,
   because the pointer index below spends a kind on the same page: it
   bites for a kind added AFTER readers exist, and the node table's kind
   would have to be skippable by a reader that has never heard of it,
   which is the whole reason it rides under an existing one. **Kind `14`,
   an array of tables — also rejected**: an array element's framing has
   room for a length and nothing else, so the type id would have to ride
   inside each body as a reserved FIELD, and every node body would carry
   a field no schema declared. Kind `12` is §3's opaque byte payload,
   which is exactly what the node table is to a reader that cannot name
   it.
6. **A DISTINCT KIND for the pointer index — TAKEN, kind `17`.** The
   opposite case, and the difference is who has to skip it. A node index
   is four bytes and so is a `uint32`; under a shared kind an edit
   between them reports nothing in either direction, and the direction
   that matters — an index read back as a number — is silent always,
   not merely often. A kind costs zero bytes, since the kind byte
   already rides, and one row in the fixed-width skip rule. The set is
   closed before any reader ships, so nothing has to skip a kind it does
   not have; a kind spent now is simply part of the set every reader will
   ever have, and spending one later would not be available. That last
   clause is the reason this is decided here rather than deferred as an
   optimization: the window closes at ship. It puts §4.1's silent class
   back at two, beside `[E]T`'s kind `16`, which closed the previous one
   the same way.

   **The cost that is not zero, stated**: §3's rule cuts both ways, so a
   reader built before this kind existed meets kind `17` as framing
   damage and stops that body rather than skipping the field and reading
   on. That is the price of the number, it is why item 5 refuses one for
   the node table — which must stay skippable by a reader that never
   heard of it — and it is bounded by the same fact that makes the kind
   affordable: no such reader ships. A pointered save was already not
   readable by a build of the tree encoding, since the node table carries
   what the bodies used to.
7. **A `u64` LENGTH on the reserved field, to lift the aggregate ceiling
   — REJECTED, because it breaks the very reader it exists for.** A
   skipper reads kind `12` as `L (u32)` then `L` bytes; given a `u64` it
   takes the low half, skips that far, and lands four bytes short of the
   payload's end, where it reads two payload bytes as the next field id.
   The whole point of riding under an existing kind is that an old reader
   frames the body and counts `unknown`, and a wider length forfeits it.
   **The repeating field was taken instead**: same `u32`, same skip rule,
   one narrow exception to last-occurrence-wins for one reserved id, and
   no ceiling at all. **Its chunks close at RECORD boundaries**, and that
   is the load-bearing half. A straddling record has no contiguous view,
   so a reader would need a segmented cursor for every multi-byte read
   AND a copy to hand a body to the generated decoder — an allocation on
   the reading path, which §6.5 forbids, propagating into code that has
   nothing to do with chunking. Record alignment costs under one record
   of slack per field, keeps the chunking deterministic, and makes the
   naive reader the correct reader. The one thing it cannot frame is a
   record larger than a field, which the `u32` record length already
   refuses (§11).
8. **A reader-side acyclicity check — PRICED, NOT TAKEN.** Pre-order
   numbering gives every node a contiguous index interval `[i, extent]`,
   so with a `max index in subtree` on each record, a reference to a
   LOWER index is an ancestor edge — a cycle — exactly when the
   referrer's index is inside the target's interval: an `O(1)` test per
   reference. It costs bytes a node plus a pass to prove the intervals
   are genuinely nested, for a property the writer already guarantees
   (§3.1) and no schema-generated walk depends on. It is the shape the
   check would take if untrusted-input hardening ever demands one.

**Region references stay OFFSETS, not indices.** The node directory
(§6.3) could have replaced the self-relative delta in every slot, making
the region and the wire one encoding. It was rejected on the hot path: a
deref would become a directory load plus a base pointer where today it is
one add. The directory is kept for what only it can do — resolving wire
indices on load, and checking a file whose provenance is in doubt — and
the runtime's read path never touches it, which is what makes it
separable at cook time (§7).

**Why the cooked check is a SCAN and not a walk.** Three models were
weighed:

1. **A stateless traversal from the root — REJECTED.** Bounds and
   forwardness are not enough: a forged region can be a legal-looking
   DAG, forward and in range and aligned, and a walk with no memory
   re-visits shared subtrees exponentially (26 nodes, 312 ms; ~60 nodes,
   never).
2. **A traversal with a monotonic high-water mark — REJECTED, and it is
   worth saying why it FAILS rather than merely costing.** The mark
   advances over region BYTES, not over entered NODES, so a forward
   reference may jump past a node the walk never enters. That node is
   then below the mark with its slots unchecked, and a later reference to
   it passes an "already visited" test that was never true of it. A
   48-byte forgery is enough: the root names the third node, the third
   names the second, and the second's slot — never checked — points far
   outside the region. Any repair keeps the traversal and adds state to
   make "below the mark" and "already entered" the same statement.
3. **A linear scan over the node directory — THE DESIGN OF RECORD.** The
   directory already names every node start and its type. Spending that
   the way §3.1 spends the wire's type id — removing the walk instead of
   bounding it — makes each node checked exactly once, follows no
   reference at all, and needs no order invariant, so the pack order and
   the check are independent. It also checks the nodes NOTHING points at,
   which no traversal from the root can, and that is precisely where the
   marked traversal's hole was. `O(R + P log N)`, no allocation, and less
   machinery than the walk it replaces.

**The sectioned block: the forms that already existed, and why none of them
carried the render data** (§2.7, §19). Four were weighed before a construct
was added at all, because adding one to a language whose whole claim is
"nothing left over" needs the alternatives eliminated by name:

1. **A bounded array BY VALUE inside a fixed table — REJECTED, and it is the
   near miss worth stating.** `ships [..MaxShips]RenderShip` in a fixed table
   is already a strided array at stride `sizeof`, at a compile-time offset,
   filled by N workers with no synchronisation — the storage half of the
   block, for free. Two things defeat it and neither is cosmetic. **The
   header stops being small**: the array's storage is INSIDE the struct, so
   the consumer's header type is megabytes and cannot be read by value,
   which is the ergonomic the whole pattern is built on. And **the layout
   facts stay COMPILE-TIME CONSTANTS on both sides** — the consumer assumes
   an offset, a count companion's placement and a pitch rather than reading
   them, so every drift garbles silently and nothing on the boundary can
   say so. The section's real content is not the storage: it is the
   `(offset, count, stride)` triple that turns three assumptions into three
   facts a consumer reads (§19.4).
2. **A pointer and a region (§2.1, §6.3) — REJECTED.** That is the
   variable-length class: an arena, a `Lock` that compacts, a node
   directory, self-relative derefs. The producer would pay a
   single-threaded compaction per frame and the consumer would need the
   region surface, which C# does not have and would not want at sixty hertz.
   The block needs no arena because it declares its bound.
3. **The cooked form (§7) — REJECTED, and for the same reason flatbuffers
   lost.** A cook is produced FROM A BUILDER by a single-threaded `Lock`,
   which is precisely the serialization point §12.1's bar refuses; and a
   cook is an optimization OF the tolerant wire, whereas a block has no wire
   to optimize. The two are not competitors: a cook accelerates a file read
   once at load, a block is rebuilt sixty times a second.
4. **The tolerant table wire, per frame — REJECTED with a number implied.**
   It is a parse and a copy on every frame on both sides; the abandoned
   flatbuffers build is that rejection already paid for once (§7).

**And four decisions inside the construct.**

- **Block-relative offsets, not self-relative — TAKEN, and it is a stated
  exception to §6.3.** A region reference is self-relative because nothing
  hands a walker a base pointer down inside a region. A block's consumer is
  handed the base — it is the thing it mapped — and, decisively, a blittable
  consumer reads the header BY VALUE (a struct copy out of the mapped
  bytes), which a self-relative delta does not survive: the copy's address is
  not the original's. Block-relative keeps relocation by `memcpy` intact,
  since every offset is relative to a base that moves with the block.
- **Storage sized from the declared maxima; offsets laid out ONCE per block
  from the counts — TAKEN.** The storage half is the allocate-max law (§14's
  builder item 3, which a general builder could not require and a block
  can): one fixed extent, never grown, never pooled. The layout half is a
  single pass over the SECTIONS — nine of them, not thousands of records —
  run before any worker starts, which is all the property needs: every
  record's address is fixed before the scatter, so workers write disjoint
  records with nothing to synchronise. **Compile-time offsets from the
  maxima were considered and rejected**: they would let workers run before
  the counts exist, which nothing in the case wants — the counts come from
  the same gather that produces the work — and they would make every block
  nearly its maximum extent, which a boundary handoff that copies would pay
  for on every frame. **And the header carries the offsets either way**, so
  a consumer reads them rather than assuming them: that is what makes a
  raised maximum or an appended section absorbable rather than a break
  (§19.5).
- **A stride DERIVED by default, declarable for headroom — TAKEN.** Derived,
  it is `sizeof`, so the pattern's own home schema needs no attribute and a
  consumer gets the contiguous fast path (§19.3). A declared larger stride
  buys append headroom and costs that fast path, and the trade is stated
  where a person choosing it will read it rather than discovered later.
  **A MANDATORY declared stride was considered and rejected**: it would make
  every declaration carry a number whose right value is `sizeof` in every
  case anyone has yet had, and a number a person maintains is exactly the
  hand-kept fact this construct exists to delete.
- **An exact layout id, plus a NAMED compatible entry point — TAKEN, after
  an append-tolerant digest was found to be impossible.** A single number
  cannot be verified against a PREFIX of the facts that produced it: a
  consumer that knows fewer fields cannot recompute the producer's digest,
  and any fold that ignored the difference would ignore a real break too.
  So the id is exact, like a cooked file's (§7), and the append-only path is
  a second entry point a caller asks for BY NAME — §7's `Open` /
  `OpenValidated` shape, for the same reason: no silent bypass, ever.

**No decision here knowingly costs TIME.** The `u64` type id and the
repeating node-table field cost BYTES and nothing else — the record scan
is linear either way, and a wider id compares no slower than a narrow
one. The directory scan replaced a traversal with a scan of the same
asymptotic class and a smaller constant, and it moved off the runtime
path entirely (§7): a matching layout id points, and checks nothing.
Where a cost is real it is stated with its number (§6.3) rather than
deferred, and the optimization work that follows real profiles is not
pre-empted here.

## 15. Named follow-ons

- **A DECLARED COMPATIBLE SET of layout ids for the cooked form.** §7
  loads a cooked file at exactly one layout id. The owner's "or a subset
  of versions AT MOST" is the only widening ever contemplated: a build
  declaring the ids it will accept, so an asset format that rarely
  changes need not be re-cooked for a build whose layout did not move.
  Everything about it is a decision — who declares the set, what proves
  two layouts interchangeable — and none of that is decided here.
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
- **Cross-endian COOKING**: producing a cook for a target whose byte order
  is not the cooking machine's, by swapping as the region is written. The
  swap belongs at cook time because a cook is made for a known target
  (§7), and the descriptors already carry the kinds and offsets it would
  need. `Open` never swaps: a recorded order that is not this build's
  means the file is not this build's, and it refuses.
- **A hash-guarded fallback loader** — open the cooked form, else load
  the wire — as a convenience helper.
- **NESTED SECTIONS** (§2.7) — a section whose element is itself a block
  root, so one block can carry a header per view or per layer instead of one
  header per block. It wants a decision about whose base a nested header's
  offsets are relative to before it is a construct, and the flat form is
  what the case in hand needs.
- **A section of UNIONS**, which the general array-of-unions follow-on
  already covers, plus a blittable answer for a union's storage in a
  language with no native one. A section element declares no union today.
- **A block's own TEXT FORM and packer support** (§16, §17). A block has no
  wire (§2.7), so `pack` has nothing to write and `ToJson` has no framing to
  walk; a block's records are ordinary tables and can be textualised one at
  a time. Whether a whole block should have a text projection at all is the
  open part.
- **A TOLERANT WIRE for a block root**, by giving a section a wire kind. It
  is not obviously wanted — a block is a same-build ABI by construction and
  the wire is the other contract — and spending a kind is expensive once
  readers exist (§14), so it is named here rather than reserved.
- **Cross-endian blocks.** A block carries its byte order in its magic and
  refuses a foreign one, exactly as a cook does; swapping one would be the
  cook's cross-endian follow-on applied to a flatter shape.
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
every generated `.cpp` — but that is C++'s way of meeting the rules, not
the definition of them. Another backend, and the compiler's own packer
(§17), implement the same rules over the same IR, and goldens are what make
the implementations one form: for every instance in the corpus, every
implementation's text is byte-identical and every implementation's read of
that text produces the same wire bytes.

**The walk is a generated TRANSLATION UNIT, not header content.** A table
unit file emits `<Base>Table.cpp` beside `<Base>Table.h`: the header declares
the three functions per table and carries the descriptors, and the `.cpp`
holds the walker and the definitions. A project that reads or writes a text
compiles that file; a project that never does compiles nothing for it, and
its headers carry neither the walker nor the number-conversion includes the
walker needs. The form is still available to every FIXED-SIZE table in the
closure with no opt-out at the DECLARATION level — nothing is gated on a
unit's mode — but paying for it is a build decision, not an include.

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

**These are declarations in `<Base>Table.h` and definitions in
`<Base>Table.cpp`** (§6.1, §13.5). Compile the generated `.cpp` to call
them:

```
c++ -c ShipTable.cpp        # once, not once per including translation unit
```

A project that never reads or writes a text simply does not compile that
file, and its headers carry neither the walk nor the number-conversion
includes the walk needs.

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
| `[E]T` enum-keyed array | object keyed by VARIANT NAME | `{ "Fighter": {...}, "Bomber": {...} }`; an absent key keeps that slot's defaults; a **repeated variant key is last-wins and counted**, as any duplicate key is; an unknown key is skipped and counted, and **`"None"` is such a key** — it names no slot (§2.4) |
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

**The text form is a TREE, and a pointer graph is not.** A pointee is
written in place, so a node several parents name is written once per
parent and reads back as that many nodes: the identity §3.1 preserves on
the wire does not survive a round trip through text. And a walker over a
structure whose provenance it does not trust carries its own visit bound,
because a cyclic structure is expressible (§3.1) and writing one in place
does not terminate. Both are properties of writing a graph as a tree, not
of this mapping; the binary wire is where identity lives.

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
  walker, that walker's source is identical in every generated `.cpp`.

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
  backend's `ToJson` writes for the same instance. `unpack --one-file` is what
  makes that a comparison of two whole texts rather than of a tree against an
  object: it writes §17.2's last shape, one `<Root>.json`, from the same
  instance through the same writer.

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
trip §1 promises, and `unpack` → `pack` is byte-stable, WITH §16.3's ONE
CARVE-OUT: a string holding bytes that are not well-formed UTF-8 is written as
`U+FFFD`, one per bad byte, so its text can be longer than the bytes were and
the field's own bound then clamps it. That lap is not byte-identical, it is
COUNTED (`clamped`, which §17.4 makes a nonzero exit), and the FIXED POINT IS
REACHED IN ONE LAP — after the first write every string is well-formed, so the
second text and the third agree. The alternative is emitting a text no
conforming parser can read, which §16.3 already refuses.

§17.2 lets a field's value live in a file or in a directory, and `unpack`
takes the EXPANDED form by default (`--one-file` takes the last rule's
instead: one `<Root>.json`, which is the shape §17.1's third golden compares): one `<field>.json` per field of the root, and one
`<field>/<Variant>.json` per slot of an enum-keyed array. An absent `?T` and
a guarded-out field write no file at all, because omission is how a tree says
absence — **and that is why `unpack` PRUNES**: an entry naming a field of the
table it just wrote, that it did not write this time, is removed. Without it,
unpacking a newer `.bin` over yesterday's tree would leave the file an absent
optional used to have standing beside the new one, and byte-stability would
hold only into an empty directory, which is not the directory the verb is
pointed at. An entry naming NO field is left exactly where it is: it is not
the tool's, and `pack` refuses it by name (§17.4).

### 17.4 Refusals and the report

A tree that does not mirror the table is reported rather than guessed at: a
directory or file naming no field, two files claiming one enum-keyed slot,
a variant name the enum does not have, a `None.json`, a file that is not
JSON. Everything §16 counts is counted here too, aggregated across the
tree, so a pack of a hundred files reports once.

Three rules complete it, because a packer is a TOOL and a tool's edges are
part of what it promises:

- **A hidden file that is not JSON is passed over, and NAMED.** It is the one
  thing a tree walk does not refuse — a tool that died on `.DS_Store` would be
  a tool nobody could run on a checkout — and the skip is narrow enough that it
  cannot swallow a value: a hidden `.json` file and a hidden directory still
  name something, and are refused if they name no field.
- **A report that is not silent is a nonzero exit.** "Reported, never fatal"
  (§4) is about the walk not stopping, not about a tool's exit code: a value
  that was skipped, renamed away or cut down is a thing a build pipeline has to
  be able to fail on. `--tolerate` is how a caller says the report is expected.
- **Neither verb writes to the unit's schema sources.** Every other command
  canonicalizes them in place because formatting is part of what it is doing to
  them; these two are pointed at a config tree and only READ the declarations.

### 17.5 Held by test

A directory corpus packs to bytes identical to `Save` of the same instance
built by hand; `unpack` → `pack` is byte-stable, INTO A TREE THAT ALREADY
HOLDS ONE and ACROSS BOTH SHAPES — unpacking either shape over the other packs
back to the same bytes, because the prune covers the root's whole shape and not
just the one being written; §17.3's UTF-8 carve-out is pinned by a corpus row
rather than assumed, fixed point included; the goldens of §17.1 hold the engine
to every backend that implements the form; and the hostile tree above is
refused or counted per §16's rules.

**And a HOSTILE-VALUE corpus beside the hostile tree**: one tree per rule §16
states — every row of the number grammar, a value past a `bits(N)` width, a
lone surrogate, a `null` at every kind, a `"None"` key, a duplicate key, a
union with two keys — each carrying the outcome the rule requires. It is a
TWO-SIDED differential: the same text goes through the packer and through the
backend's `FromJson`, and their REPORTS must agree counter for counter and
their WIRE BYTES byte for byte, with a refusal one side refused by both. A
tree that packs carries one further invariant: its bytes load clean in that
backend and re-save byte-identically, because a read either implementation
calls clean must not be one the backend then cuts down. A corpus of
well-formed trees proves the happy path and nothing else, and the rules are
where implementations drift apart.

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

**A BLOCK ROOT and its section elements record LAYOUT FACTS, because a block
has no wire and its contract is the layout** (§2.7, §19.4). A section's line
carries its element, its declared MAXIMUM, and its STRIDE as the evaluated
number, marked derived or declared; and every table a section names records,
per field, the BYTE OFFSET and SIZE the layout rule gives it (§19.1), beside
the element's own `sizeof` and alignment. Those are the only lines in this
file that are not wire facts, and they are here for the same reason every
other line is: they are what an edit can break and the compiler cannot
remember. A table no section names records none of them, so a unit with no
block is projected exactly as it was — but the RENDERING VERSION on the file's
first line moves, because the projection can now carry lines an older reader
does not know, and §18.4's repair path is what a baseline written under the
older version takes.

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
schema-tables-baseline 3
package shipdemo

table ShipConfig
    field damage id=0x15a9 kind=10 default=21.0
    field speed id=0x2e46 kind=10 default=500.0 was=velocity
    field name id=0x30df kind=12 size=32

block RenderFrame sizeof=32
    section ships elem=RenderShip max=4096 stride=64 declared

table RenderShip sizeof=32 alignof=8
    field position id=0x4c31 kind=13 offset=0 size=24
    field object_id id=0x6b0e kind=8 offset=24 size=4

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

**A BLOCK's edits are judged by a different standard, because a block has no
wire to absorb anything** (§2.7). The wire's rules above are about what a
tolerant reader can report; a block's are about what a POINTED-AT record can
survive, and the two do not overlap:

- **REFUSES, naming `table.field` and the edit** — a field INSERTED before
  the end of a section element, or REORDERED, or removed, or changed to a
  type of a different size or alignment: each moves a byte offset a consumer
  reads at, and nothing in a block can report it. A STRIDE shrunk, whether
  by declaring a smaller number or by an element growing under a derived
  one. A section's ELEMENT swapped for another declaration. A section's
  spelling changed to or from an ordinary array. A section removed, or moved
  earlier among the header's sections.
- **WARNS** — a section's declared maximum LOWERED (records that used to fit
  no longer do, and the producer's own bound is what says so), and a block
  root that vanished under its baseline name (§18.3's rule, unchanged).
- **PASSES, in silence** — a field APPENDED at the end of a section element
  while its offset is past every offset the baseline records and its end
  still lies inside the stride; a section APPENDED at the end of a header; a
  declared maximum RAISED; a stride declared larger. These are the four
  edits the construct is shaped to absorb (§19.5), and they are silent
  because a consumer reading its own prefix at the header's own offsets is
  unaffected by every one of them.

**A field appended PAST the stride refuses**, and it is worth separating
from the pass above: appending is only free while the record still fits its
pitch, so the same edit passes at a stride with headroom and refuses at one
without. The diagnostic says which, and names the stride and the two sizes,
because "add headroom or accept the break" is the decision the author is
actually making.

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

## 19. The sectioned block

**A block is an ABI, and the fourth form this document describes.** A table
that declares a section (§2.7) is a BLOCK ROOT; its instance is the HEADER at
the base of one flat extent, and the strided records its sections name follow
it in that same extent. The producer fills the block; the consumer — usually
in another language — maps it and POINTS at the records. There is no parse on
either side, no copy, and nothing between a record's bytes and the struct
that reads them.

**Where it sits among the other three, stated precisely:**

| form | contract | who reads it | cost |
|---|---|---|---|
| the type wire (SPEC.md) | same build, exact, bitpacked | a peer on the same protocol id | a codec per value |
| the table wire (§3) | any version, tolerant, reported | anything, years apart | ids, kinds, lengths, a report |
| the cooked form (§7) | one layout id, pointed at | a build at the cooked version | a cook, from a builder |
| **the sectioned block (§19)** | **one layout, both sides generated, pointed at** | **a consumer generated from the same schema** | **a declared pitch** |

**What a block does NOT carry, in full**, because a reader coming from the
table wire will look for all of it: no field ids, no kind bytes, no lengths,
no terminators, no elision — a record's fields are at their offsets whether
they hold defaults or not, and a section's records are at their pitch whether
they are set or not. No `was` tolerance, no unknown-variant handling, no
clamping, and **no read report of any kind**: §4's counters do not exist here
because none of the events they count can occur. No node table, no pointers,
no arena, no region, no node directory and no attribution part. A record is
its bytes at its offsets, and the guarantee that both sides agree on those
offsets is §19.4's, held at compile time rather than reported at runtime.

**A block is a same-build contract by construction**, in the plain sense: the
producer and the consumer are generated from one schema and ship together. It
is the type wire's contract wearing a different shape — an ABI across a
language boundary rather than a protocol across a network — and the
robustness the construct adds on top of that is §19.5's, which is narrower
than the wire's and says so.

### 19.1 The layout

**One extent, 64-byte aligned at its base**, laid out in this order:

- **The HEADER at offset 0.** It opens with a generated PROLOGUE of two
  `uint64`s — `magic`, which is a constant identifying a schema block and is
  also the byte-order check, and `layout_id`, the digest §19.4 defines — and
  the block root's declared fields follow it. The prologue is generated, as
  an optional's presence companion is, and a field of a block root may not be
  named after either half (§11).
- **Then each SECTION, in declaration order**, each starting at an offset
  aligned to `max( 64, alignof( element ) )`. Sixty-four is a cache line, and
  it is the number rather than the element's own alignment for one reason:
  two workers filling adjacent sections must never share a line. The slack is
  under 64 bytes per section, in an extent whose sections are the size of the
  frame.
- **A section's records sit at its STRIDE** (§2.7): record `i` begins at
  `section.offset + i * section.stride`. Because the stride is a multiple of
  the element's alignment and the section's start is aligned, every record
  start is aligned for its own type — the arithmetic needs no case.
- **The bytes between a record's end and the next record's start are
  UNSPECIFIED** where a stride carries headroom, and a consumer never reads
  them. A producer is not required to zero them, and a block is therefore not
  a hash-stable artifact when its strides have headroom; that is stated so
  nobody builds a content hash on one.

**The block's STORAGE is sized from the declared maxima** — a compile-time
constant, `<Header>BlockMaxBytes`, over the header plus every section at its
declared `[..N]`. That is the allocate-max law: one extent, allocated once,
never grown and never pooled.

**The LAYOUT is computed once per block, from the counts.** The producer
declares each section's count, and one pass over the SECTIONS — not over the
records — settles every offset. The pass is bounded by the number of declared
sections, which is a property of the schema; nothing per-record happens in it,
and every record's address is settled before any worker starts.

**The USED extent is what a handoff copies**: the greatest
`offset + count * stride` over the sections, rounded up to 64, and never less
than the header's own size. `<Header>BlockBytes( block )` returns it. Because
the layout follows the COUNTS, that extent is proportional to the frame and
not to the declared maxima — a frame with three ships in it is a small block
inside a large allocation, and a consumer that copies pays for the frame it
was given. The maxima size the ALLOCATION; the counts size the block.

**Worked, from three sections and their real numbers:**

```
table RenderFrame
{
    cameras section [..1]RenderCamera
    ships   section [..4096]RenderShip | stride = 128
    lasers  section [..2048]RenderLaser
}
```

| fact | cameras | ships | lasers |
|---|---|---|---|
| element `sizeof` / `alignof` | 72 / 8 | 88 / 8 | 64 / 8 |
| stride | 72, derived | 128, declared | 64, derived |
| headroom per record | 0 | 40 | 0 |
| declared maximum | 1 | 4096 | 2048 |
| triple's offset in the header | 16 | 32 | 48 |

`sizeof( RenderFrame )` is 64 — the 16-byte prologue and three 16-byte
triples — and `RenderFrameBlockMaxBytes` is 655,552, which is the layout AT
THE MAXIMA: the header, then `cameras` at 64, `ships` at 192, `lasers` at
524,480. A real frame's offsets are that same walk over its own counts, so
`lasers` sits far earlier whenever fewer than 4096 ships were declared. Every
section start is 64-aligned in either walk, every stride is a multiple of its
element's alignment, and no section overlaps the next.

### 19.2 Building: wide, with nothing to synchronise

**Two types, and the difference between them is the whole of the surface.**
`<Header>BlockStorage` is the producer's ALLOCATION — one max-sized,
64-byte-aligned extent, owned by the caller. `<Header>Block` is a BLOCK IN
HAND — the base of an extent beside the header that sits at it, with a
section accessor per declared section. A producer gets one from `Begin`, a
consumer gets one from `Open` (§19.3), and everything downstream of either
takes the same type. The pairing is what makes a section addressable at all:
a header read alone is a copy with block-relative offsets in it and no base
to add them to.

```cpp
RenderFrameBlockStorage storage;              // the allocation: max-sized, 64-aligned

RenderFrameCounts counts = {};
counts.ships  = numShips;
counts.lasers = numLasers;

RenderFrameBlock block;                                          // destination first (§6.1)
if ( !RenderFrameBlockBegin( block, storage, counts ) )          // a count is over its maximum
    return;
```

`Begin` is the whole of the layout: it refuses counts past the declared
maxima, stamps the prologue, writes every section's offset, count and stride,
and hands back the block. It touches no record.

```cpp
RenderShip * ships = RenderFrameShips( block );   // the section's typed base

// N workers, disjoint index ranges, no synchronisation of any kind:
ships[i].position = ...;
```

- **A section accessor is one add**, `block base + section.offset`, typed as
  the element. A worker holds it and indexes; there is no allocation, no
  atomic, no lock and no per-record bookkeeping anywhere in the fill.
- **The contract is ownership, exactly as §6.4's is**: disjoint index ranges
  into one section are safe concurrently, and two workers writing one record
  is the caller's problem, not the runtime's. `Begin` and `BlockBytes` are
  single-threaded — call `Begin` before the workers and `BlockBytes` after
  they join.
- **Nothing moves and nothing grows**, so a pointer taken from an accessor is
  valid for the block's whole life. That is the reserve-max model (§14) with
  the bound declared, which is why it needs no arena.
- **A record a producer does not fill holds whatever the storage held.** The
  block is not zeroed for you: `count` is the contract, and records past it
  are not part of the block. A producer that wants zeros writes them.

### 19.3 Reading: point at it

```csharp
if ( !RenderFrame.BlockOpen( out RenderFrameBlock block, pointer, bytes ) )
    return;                                   // and the caller falls back, or reports

ulong version = block.Header.version;         // the block root's own declared fields

foreach ( ref readonly RenderShip ship in block.Ships )
    Draw( ship );

ReadOnlySpan<RenderShip> ships = block.ShipsSpan;   // available when stride == sizeof
```

- **`BlockOpen` checks the header once and points, and this is the WHOLE
  check**: the magic read bytewise, the byte order it establishes, the layout
  id against this build's own, the used extent against the `bytes` the caller
  passed, the base's alignment, and each section's offset and extent inside
  the block. On a match the bytes are what a build with this layout wrote, so
  there is nothing to validate and nothing to fix up. On any failure it
  returns false and points at nothing — §7's shape, for §7's reason.
- **A section is ITERATED, not indexed by hand.** The accessor yields a
  reference to each record where it lies, at the header's stride, for
  `count` records — a range-for in C++, an enumerator in C#, the equivalent
  per port. A call site never writes a loop over an integer bound with the
  stride arithmetic spelled out in it, for the same reason a keyed array's
  call sites should not re-derive their own slot rule: the idiom that gets
  written at every call site is the one that gets written wrong somewhere.
  A typed base pointer is available beside it for the producer, which needs
  to address a record by index (§19.2).
- **A contiguous view is offered exactly when the pitch allows it.** Where
  `stride == sizeof( element )` the section IS a contiguous array and the
  accessor hands back a span; where a declared stride carries headroom it
  cannot, and the strided iterator is the only form. That is the cost of
  headroom, stated at the place a person pays it: a job that wants a span
  over its records wants a derived stride.
- **The consumer reads OFFSETS and STRIDES from the header, never from its
  own constants.** Its constants exist to be asserted against (§19.4), not to
  index with. This is the difference between a generated pair of structs and
  an ABI, and it is what makes §19.5's four absorbable edits absorbable.

### 19.4 The layout contract

**The compiler computes the layout, and both backends assert it.** A record's
layout is the C ABI's natural one: each field at its own alignment, the
struct's alignment the greatest of its fields', the size rounded up to it.
The compiler derives every offset and size from the declaration, and each
backend emits code asserting that ITS OWN compiler agrees:

- **C++**: `static_assert` on `sizeof` and `offsetof` for every record and
  every field, plus the section stride constants, plus the standard-layout
  and trivially-copyable asserts §9 already requires.
- **C#**: a blittable `[StructLayout(LayoutKind.Sequential, Pack = 1, Size =
  N)]` struct with GENERATED PADDING FIELDS wherever the C ABI layout has
  interior padding, so the offsets match by construction rather than by the
  author having ordered fields carefully; and a generated check, run once,
  asserting each type's size and each field's offset against the same
  constants the C++ side asserts. Explicit padding is chosen over
  `LayoutKind.Explicit` because Sequential is the form every blittable path
  handles best, and over relying on a padding-free field order because that
  is discipline, and discipline is what this construct exists to delete.

**`Size` is the element's `sizeof`, never its stride**, and the distinction
matters: making the C# struct as wide as the pitch would buy a contiguous
span at every stride and would make the two sides' size asserts compare
different numbers, so a record that had quietly grown past its declared
width would still pass. The struct is the RECORD and the header carries the
PITCH; the iterator is what puts them together (§19.3).

**A disagreement is a BUILD ERROR on the side that disagrees**, naming the
type, the field, the expected offset and the one its compiler produced.
Neither side can drift silently, and neither side's layout is inferred from
the other's — both are checked against the compiler's own model, which is the
only way a two-language contract can be held by a compiler that generates
both halves.

**The `layout_id` is a 64-bit digest** over every fact the baseline refuses to
move (§18.2): each section's element and stride, and each field's offset,
size and kind. It is the region form's layout id (§7) applied to a flatter
shape, and it is sized like a digest rather than a version counter for the
same reason. **It replaces a hand-bumped version field**: the case this
construct comes from carries one today, incremented by a person who
remembers, and a generated digest is what removes the remembering.

### 19.5 Evolution: append-only, inside the pitch

**This is the secondary property, and its weight has changed.** When the two
sides were kept by hand, the stride's headroom was the versioning model: a
producer that knew a new field wrote it past the old consumer's struct width,
and the old consumer, reading its own prefix at a pitch that had not moved,
was unaffected. With both sides generated from one schema that case is much
rarer — the guard of record is now §19.4's asserted layout contract and
§18's baseline, which refuse the breaking edits at compile time. Headroom
remains a real property, and it is stated as a convenience rather than as the
model.

**Four edits are absorbed**, and §18.2 passes each in silence:

1. **A field appended at the END of a record, inside its stride.** Every
   earlier offset is unchanged and the pitch has not moved, so a consumer
   built before the edit reads its own prefix correctly at the same
   addresses.
2. **A section appended at the END of a header.** The header grows, every
   section offset moves — and the consumer READS those offsets, so it finds
   its sections where the producer actually put them. Its own header struct
   is a prefix of the producer's, and it never reads past its own size.
3. **A declared maximum raised.** The storage grows and the offsets move;
   the consumer reads them.
4. **A stride declared larger.** The pitch is in the header too.

**Everything else is a break, and the baseline refuses it** (§18.2): a field
inserted before the end, reordered, removed, or retyped; a stride shrunk,
whether by declaration or by an element outgrowing a derived one; a section's
element swapped, removed, or moved earlier. Each moves a byte a consumer
reads at, and a block has nothing that could report it — which is exactly why
the refusal is at compile time and loud.

**And the runtime has ONE more entry point, taken by name.**
`<Header>BlockOpenCompatible` checks the magic, the byte order, the extent
and the alignment, and each section's STRIDE against this build's own
constant with `sizeof( element ) <= stride` beside it — and does not check
the layout id. It is the append-only path made available to a caller who
deliberately runs a consumer older than its producer: a transitional deploy,
a hot reload, a tool built against last week's schema. **There is no silent
bypass**: a caller either gets the layout id's guarantee from `BlockOpen`, or
asks for the weaker one by name, exactly as §7 splits `Open` from
`OpenValidated`. A single number cannot be checked against a prefix of the
facts that produced it, and pretending otherwise is what the second entry
point exists to avoid (§14).

### 19.6 Held by test

- **A dogfood-shaped `RenderFrame` in the corpus** — a header of several
  sections over fixed-size records with a nested `type` apiece, one section
  carrying a declared stride with headroom and the rest deriving theirs —
  compiled, built and read by every backend that carries the construct.
- **A PARALLEL-FILL test.** N workers fill disjoint index ranges of every
  section; the resulting block is byte-identical to a serial fill of the same
  data over the records each count covers. Run under the sanitizer leg, where
  a data race in the fill is what the leg exists to find.
- **A TWO-LANGUAGE layout test.** A C++ producer writes a block; a C#
  consumer opens it and compares every field of every record against the
  values that were written, plus every section's offset, count and stride.
  Sizes and offsets are asserted by generated code on both sides, and the
  test proves the two agree on the bytes and not merely on the constants.
- **A NEGATIVE CONTROL for each half.** Perturb one record's stride constant
  on one side only and the two-language test goes red; perturb one field's
  offset in the compiler's layout model and the generated asserts go red on
  both backends. A layout test that shares its layout model with the code it
  checks proves nothing, and these two are what separate them.
- **The refusal battery**: one fixture per §11 section refusal, each with its
  negative control.
- **The baseline battery**: one fixture per §18.2 block row — a field
  inserted in the middle refuses, a field appended inside the stride passes,
  a field appended PAST the stride refuses naming both sizes, a stride shrunk
  refuses, a section appended passes, a maximum raised passes, a maximum
  lowered warns.
- **The zero-cost gate** (§2.2): a unit that declares no section carries not
  one symbol of the block machinery, and the build fails if one appears.
- **The measured leg is §12.1's**, and it is the gate rather than a
  regression test: the generated producer against the hand-written scatter,
  and the generated consumer against the hand mirror, paired in one sitting
  under the bench rules.
