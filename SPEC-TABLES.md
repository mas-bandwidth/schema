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
  contain itself BY VALUE, directly or through any chain — that recursion
  is refused with the cycle named, because a by-value cycle has infinite
  size. (Inline anonymous subtables are a spelling follow-on; the named
  form is the feature.)
- **Tables point** (§2.1). `next *Node` declares a POINTER to a table.
  Recursion THROUGH a pointer edge is legal and expected — the by-value
  cycle rule exempts pointer edges, because a pointer carries no size.
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
for an OPTIONAL SECTION is the guarded field:

```
table Scene
{
    has_settings bool
    if has_settings
    {
        settings Settings   // an optional section: off the wire when the guard is false
    }
}
```

The guard is a field like any other, the section rides only when it is
true, and a reader whose writer left it out gets the declared defaults —
with no pointer, no allocation and no change of mode. Reach for `*` when
the structure genuinely needs it: a table that refers to ITSELF (a chain, a
tree), a large subtree you would rather not carry by value, or one node
several parents name (sharing is a named follow-on, §15 — v1 writes a
tree). One pointer anywhere in a table's by-value closure flips it to
VARIABLE-LENGTH (§2.2) and with it the whole builder lifecycle, so the
choice is a real one.

The spelling is C's, deliberately: it reads as what it is. The rules,
each refused by name (§11):

- **A pointer targets a `table`, and only a `table`.** `*SomeType`,
  `*SomeEnum`, `*SomeUnion` are refused: value-semantics data has no
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

- A table is **VARIABLE-LENGTH** if it declares a pointer, or if anything
  it nests by value — directly, or as a bounded array — is
  variable-length.
- Every other table is **FIXED-SIZE**.

Pointer edges do not propagate the mode: a table that is merely POINTED
AT stays fixed-size if it holds no pointer of its own. It gains an
allocation and a resolution entry, and nothing else.

**A fixed-size table pays nothing for any of this**, and that is a gate,
not a hope: for a unit whose tables are all fixed-size, the generated
output is byte-for-byte what it was before pointers existed — no
builder, no arena, no reference type, no lifecycle surface, not one extra
descriptor column, not one extra branch in a codec, not one extra
`#include`. The build fails if a single symbol of the pointer machinery
appears in a pointer-free unit's generated header.

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
  `10` f32, `11` f64, `12` string, `13` table, `14` array, `15` union.
- **Payloads.**
  - A scalar payload is its kind's fixed width: 1 byte for bool (0 or 1),
    1/2/4/8 for the integer kinds, 4 for f32 and 8 for f64 (IEEE-754 bit
    patterns).
  - **string** carries a **u32 byte length**, then that many bytes. No
    terminator; no encoding is imposed.
  - **table**, **array** and **union** carry a **u32 byte length**, then
    the body — so any reader can skip any field without understanding it,
    and any parent can hand each nested body to a different worker (§7).
  - An **array body** opens with its **element kind (u8)** and its
    **element count (u32)**, then the elements: fixed-width elements back
    to back for a scalar kind, each element preceded by its own **u32 byte
    length** when the element kind is `table`. A `bytes(N)` field rides as
    an array of `u8`. Fixed-extent scalar arrays are positional: absent
    trailing elements pad to the declared bound.
  - A **union body** opens with its **u16 arm id** — the hash of the arm's
    NAME, `0` for the empty union — and, when the id is not 0, a **u32 byte
    length** then the arm's value as a table body.
- **Writers elide what readers default**: a field holding its default, an
  empty string or array, and an all-default nested table are not written
  at all (fixed arrays of tables keep their elements — position is
  identity there). Elision is why old readers and new writers meet
  cleanly, and why measure and save agree byte for byte (§7). Elision
  makes the DECLARED DEFAULT part of the wire contract: see §4.
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
- **A pointer field and a by-value nesting are wire-identical.** A schema
  may change one into the other and no byte moves: old readers take the
  first body by value; new readers allocate one node from it. The corpus
  holds that as a both-directions evolution test.

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

The same bytes serve every use: a file on disk, a blob in memory, a
payload handed from a tool to a game, a message between services whose
deploys never align. Save and load are symmetric over caller-provided
buffers — message-ready by construction; generated code allocates
nothing on the reading path.

## 4. Versioning is wire tolerance

There are no version declarations — no fences, no version numbers, no
retained lineage. **The wire itself is evolution-tolerant**, and that
tolerance is the versioning model:

- **Unknown field** (newer writer): skipped by its length, counted.
- **Absent field** (older writer): the reader's value takes the field's
  default — the specified default, else zero.
- **Unknown enum variant or union arm** (a name this reader does not have):
  the enum value reads as `None`, the union reads as empty, the arm's body
  is skipped by its length, and the event is counted as **unknown** —
  the same counter an unknown field id uses, because it is the same event:
  the writer named something this reader cannot name.
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
clamped, malformed. `unknown` counts every id this reader cannot name: a
field id, an enum variant id, a union arm id. Silence (all zero) means the
data matched this reader's schema exactly. Tools surface the report; games decide their own
policy over it. Nothing crashes on data from a different schema version,
in either direction, and that property is held by a both-directions
evolution test in the corpus.

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
- **`| max = K` headroom on an enum in a table closure is refused.** A
  headroom value is reserved by number and named by nothing, so it has no
  identity to ride under — and the table wire needs no headroom, because a
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
may allocate; the reading path never does.

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

**A vocabulary field carries its vocabulary and the ids it rides under.**
An enum field and a union field both describe a named set indexed by
`[0, enum_max]` — an enum's values, a union's arms — with a value→name
function beside a **value→wire-id** function, so a tool can enumerate the
names AND the ids without the schema files (§5). For every other kind those
columns are absent: a flags field carries none, because its variants have
no per-variant wire id to carry (§4).

A unit that has pointers carries two more facts, and a unit that has none
carries neither (§2.2): a field's **`is_pointer`** flag — whose `table`
member then names the TARGET table's descriptor, and whose `elem_size` is
the reference slot's width — and a type's derived **`variable`** mode, so
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
  union, whose name hashes collide, with both named (§5).
- **`| max = K` headroom on an enum in a table closure** — a headroom value
  has no name, and the table wire identifies a variant by name (§5).
- Tables under any backend but C++ (status, above) — refused with the
  follow-on named, never silently ignored.
- **Pointers** (§2.1): `*T` where T is a `type`, enum, flags or union —
  value-semantics data has no identity to point at; a pointer declared
  outside a table body; an array of pointers (§15); a specified default
  on a pointer field.
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
  each of these **23 suffixes**, and a declaration spelling one of them is
  refused naming the collision:

  ```
  Measure  MeasureBody  Save  SaveBody  Load  LoadBody
  LoadMeasure  LoadMeasureBody  LoadBuilder  TableType  Builder
  At  Root  Emplace  Pack  PackMeasure  OpenWalk
  Cook  CookMeasure  Open  LayoutId  TableFields  TableInfo
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

The feature's acceptance test: a real game's binary config and asset
archive formats — a root table of nested collections of typed records,
built by tools, loaded by the game — must be expressible as declared
tables with nothing left over, without schema prescribing any of their
structure. The corpus carries a config-format example holding that gate.

## 13. Rulings, recorded

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

- Per-language backends beyond C++ (the refusal in §11 names this).
- **Graph and DAG identity**: preserving aliasing and sharing across the
  wire, so two references to one node stay one node. Wire v1 is a tree
  (§3.1) and says so.
- **Arrays of pointers** (§2.1).
- **Lifting the depth cap** with a flat, indexed node encoding, so a
  pointer chain's length stops being a nesting depth (§3.1).
- **Cross-endian `Open`**: an in-place, descriptor-driven byte-swap pass
  sharing `Open`'s existing traversal — the same nodes and fields are
  already visited, and kinds and offsets are already in the descriptors.
  v1 refuses a foreign byte order instead (§7).
- **A hash-guarded fallback loader** — open the cooked form, else load
  the wire — as a convenience helper.
- **A generic JSON walk over the reflection descriptors.** A text
  authoring form is a pattern the owner described from practice: humans
  and tools edit text, a build cooks it, the runtime points at the
  result. Because the descriptors carry every field's name, kind and
  offset, that walk is ONE implementation over the reflection surface —
  no per-table codecs — which is what makes it worth doing as its own
  round. It pairs with the field-level name-mapping attribute already
  filed below.
- A generic dump/diff tool over the reflection surface.
- Keyed lookup conveniences over loaded collections (library-side, never
  stored semantics).
- Arrays of unions in table bodies.
- `fixed` and 128-bit table-wire kinds, if a need ever materializes.
- A field-level name-mapping attribute for text-format tooling (the
  JSON-authoring surface real pipelines pair with binary tables) —
  tool-side, never wire.
