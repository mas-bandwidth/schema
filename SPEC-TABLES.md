# schema — views and tables (draft specification)

**Status: DRAFT, for adjudication.** This document specifies two features that
are not in the language: the **view** (a generated reflective descriptor
surface — [#157](https://github.com/mas-bandwidth/schema/issues/157)) and
**`table`** (versioned content declarations —
[#158](https://github.com/mas-bandwidth/schema/issues/158)). It is written in
SPEC.md's register and to its rigor, but it is not yet normative: SPEC.md
promises that where the compiler and the document disagree one of them is a
bug, and the compiler refuses `table` by name today (SPEC §4.11). The four
design questions this draft opened are now decided — §14 records the
rulings — and §15 lists the SPEC.md text that changes when this lands. §10
gathers the complete table source language for review. Syntax is marked
**PROPOSED** wherever the spelling, rather than the design, is still open.

## 1. One descriptor, three tenses

Every schema unit already has one canonical reified shape: **the projection**
(`ir.WireProjection`, SPEC §3.1) — a canonical text listing exactly the facts
that determine the bytes on the wire, printable, diffable, and hashed into the
protocol id. Everything in this document is a promotion of that artifact, not
an invention beside it. One descriptor, three tenses:

- **The `type` wire ERASES the descriptor.** Both ends hold it at compile
  time, so only its hash — the protocol id — remains. Same-or-refuse is
  descriptor equality (SPEC §3).
- **The view LOADS the descriptor at runtime.** Inspectors, editors, dumpers
  and exporters walk values generically, against generated metadata, without
  hand-written glue (§2).
- **The `table` RETAINS descriptors across time.** Rows written under old
  shapes stay readable by new builds, because the shape they were written
  under is kept, versioned, and enforced (§3).

Types are essentially fixed; **tables are the sanctioned evolution surface of
the language.** The wire contract of SPEC §3 — one id, same-or-refuse, no
evolution machinery — is untouched by everything below: nothing here adds a
byte, a tag, or a version field to any `type`'s wire.

The one-line model underneath all of it: **types are value semantics; tables
are pointer semantics.** A type embeds by value — it flattens inline, copies
as bytes, has no identity, cannot be null, cannot contain itself. A table is
reached by reference — it has directory identity, is shared, is nullable, is
swappable behind its index, and may participate in cycles. The versioning
split is a consequence, not a rule: values are timeless shapes, so types
never version; identities persist across time, so tables must. Types are
values, tables are objects, the directory (§4.1) is the heap — and it is a
heap you can memcpy.

## 2. The view

Every declaration gets a generated **view**: a reflective surface over its
shape — field names, kinds, widths, bounds, defaults, branch structure, in
wire order. Implicit, zero opt-in, no language change, no wire change.

### 2.1 The descriptor

The descriptor is the projection's facts, made loadable. For each declaration
the compiler already knows — and the projection already prints — everything a
generic walker needs:

```
type Body table=false message=false
  field kind kind=0 width=8 signed=false
  field health kind=0 width=32 signed=true intrange=[0,1000]
```

The view work is specifying this artifact as a **generated, runtime-loadable
descriptor** and emitting it in each target language. The descriptor format is
specced once, per the SPEC discipline, before any emission is written; the
facts it carries are exactly the projection's inclusion list (SPEC §3.1) plus
the names the projection already renders. Nothing is computed twice: a
descriptor and its hash derive from the same IR facts, so a descriptor that
disagrees with the generated wire code cannot exist without a hash moving.
**Which hash depends on the declaration** (§3.3): a type's facts print in
the unit's projection, so the protocol id moves; a table's do not — the
projection never prints table declarations — so its **lineage hash** moves,
and the protocol id stands still.

### 2.2 What is generated

Per target language:

1. **Per-declaration static metadata** — a constant descriptor value for each
   `type`, `enum`, `flags`, `union` and `table` in the unit: the declaration's
   name and kind, and its field list in wire order, each field carrying name,
   kind, width/signedness, declared bounds, string/bytes/array capacities,
   float range/resolution, fixed `I`/`F`, specified default, and branch
   structure. A `table`'s metadata is its lineage — one descriptor per
   fenced version (§3.5), hashed into the lineage and never into the
   unit's projection (§3.3). Generated as data, not code — one table the
   walker indexes.
2. **A generic walker** — one per target, living in the table runtime
   library (§9), never regenerated per schema: given a descriptor and a
   value, it visits the value's fields in wire order, dispatching on field
   kind, invoking a caller-supplied visitor. The walker is how a dumper
   prints any type, an editor edits any row, and an exporter emits any
   document, with zero per-type glue.
3. **The walker's write half** — set-field-by-descriptor, for the trusted
   tooling side: an editor that walks the view to read a row writes it back
   the same way. This sits squarely inside SPEC §5's doctrine — writes
   assume trusted data; the write half is a tool's surface, never a trust
   boundary — and it is what makes the live loop of §5.2 a runtime
   operation with no compiler in it.

### 2.3 Views are metadata about the wire, never new bytes on it

A view changes nothing about generated wire code and adds nothing to any
wire. The hot path stays the generated monomorphic read/write functions of
SPEC §6; the view is the general path for tooling, and the split is
deliberate: the family's performance work measured monomorphic generated code
at 2–5x faster than dispatched access, which is exactly why both surfaces
exist. Applications serialize with generated functions; tools walk with the
view. Nothing in the language chooses for you, and nothing makes the general
path load-bearing for the protocol.

### 2.4 The view across time

A view over a `type` is compiled in: the static metadata matches the build's
own shape, always. A view over a `table` is the same machinery pointed at
retained descriptors: reading a row written under an older version means
loading that version's descriptor (§3.5, §11) and walking the row through
it. This is the whole trick — tables need no second reflection system,
because the view already is one.

## 3. The table

### 3.1 Two nouns, two contracts

- **`type`** — the moment's contract: same-or-refuse, one protocol id, naked
  bitpacked wire, freely editable (any edit moves the id; both ends redeploy
  together) — free, that is, until fenced table rows use the type, which
  freezes its fenced fields (§3.5). Reader and writer are the same build,
  by contract.
- **`table`** — the archive's contract: versioned, append-only lineage,
  header'd wire (version and length per row), compiler-enforced evolution.
  The reader may be a different build than the writer; content outlives
  builds.

A table is not a type missing wire. It carries the *other* wire — rows that
must be readable by builds that did not write them. The two contracts share
one field grammar and one descriptor mechanism, and differ in exactly one
axis: what happens to the descriptor over time (§1).

### 3.2 Hard constraints

Two constraints govern every design decision below, stated first because they
refuse entire families of alternatives:

1. **Tables are relocatable.** No internal pointers anywhere, stored or in
   memory. A row is movable and copyable without fixup; a table is buildable
   into a preallocated slab and usable where it lands; a stored document is
   one memcpy-able region. Every reference is a value — an index — never an
   address (§4). This extends the storage discipline generated types already
   hold (trivially copyable, standard-layout, nothing dynamically sized —
   SPEC §6.1) from the value to the collection.
2. **Tables are parallel-buildable.** The build is never a serial builder
   through which every row must pass. Workers measure disjoint row index
   ranges in parallel (the measure stream), a prefix sum over the measured
   sizes yields every row's exact offset, and workers then scatter-write into
   the preallocated slab at known offsets — no shared mutable state, no
   contention, no fixup pass. The row-offset index that falls out is relative
   offsets from the table base, never pointers, so it preserves
   relocatability and doubles as random row access without sequential
   decode.

### 3.3 Declaration

**PROPOSED syntax** — `table` is already a reserved word (SPEC §4.1), held
for exactly this:

```
table Item
{
    id     uint32
    name   string(64)
    kind   ItemKind
    price  uint16 | min = 0, max = MaxPrice
}
```

**Tables inherit the field grammar wholesale.** A row may hold everything a
type may hold: bare and ranged integers, the 128-bit family, `bits(N)`,
`bool`, floats and compressed floats, `fixed`/`ufixed`, enums, flags,
strings, bytes, bounded arrays, unions, `if` branches, `const`/`reserved`/
`align` items — and declared types as field groups, in from day one
(ruled; §14 q4). That is
most of the table's power at zero new constructs, and it composes with the
versioning model because rows are length-prefixed and sequentially decoded:
variable-length fields do not fight prefix evolution (§3.5).

The composition rule that gives the one-language property: **the container
decides the contract; the type decides only the shape.** The same declared
type is bitpacked naked inside a packet and encoded under the table regime
inside a row. Types do NOT implicitly become tables — the explicit `table`
declaration is what carries the retention contract and licenses the compiler
to enforce append-only exactly where archives exist, while types stay freely
editable everywhere fenced rows do not use them (§3.5's fence ownership is
the one exception, and it is opt-in by using the type in a table).

**Table declarations do not enter the unit's wire shape projection.** The
protocol id covers the type wire alone: adding a table, fencing a new
version, or deleting one moves no `ProtocolId`, because peers whose
packets did not change must never be forced into a lockstep redeploy by a
content-shape edit — the two contracts version independently by
construction. Each table's shape hashes into its own lineage instead
(§3.5, §11), version by version. Table declarations still share the unit's
one flat namespace (SPEC §4.6), and the FROZEN-token drop of §13 remains a
`ProjectionVersion` bump — a change to the projection's rendering, not a
claim that table shapes ride in it.

### 3.4 The row wire

A row is a **header, then the payload**:

- **The header is byte-aligned and fixed-width**, so headers and row
  boundaries are readable without decoding any payload. PROPOSED widths:
  `version` as a little-endian `uint16`, then `length` as a little-endian
  `uint32` counting the payload's bytes. Fixed widths — not ranged encodings
  — because the header must be readable by builds that do not hold the
  writer's descriptor at all; the header is the one part of the row wire
  that can never version.
- **The payload is the version's flattened field list, bitpacked** — the
  ordinary wire model of SPEC §4.3, exactly as a `type` body encodes —
  zero-filled to the byte boundary (the stream's ceil(bits/8) rule). Rows
  are therefore byte-aligned end to end, which is what lets the offset index
  address them and workers scatter-write them.

A worked row. Take version 2 of a table whose flat shape is
`id uint16` (bare, 16 bits) then `health int32 | min = 0, max = 1000`
(10 bits); a row with `id = 7`, `health = 250` encodes as 26 payload bits →
4 bytes:

```
02 00                  version = 2      (uint16, little-endian)
04 00 00 00            length  = 4      (uint32, little-endian)
07 00 FA 00            payload: id = 7 in bits 0..15;
                       health − 0 = 250 in bits 16..25 (10 bits:
                       0xFA in bits 16..23, zero in bits 24..25);
                       zero-filled to the byte
```

Ten bytes total. An old reader that knows only version 1 reads the header,
decodes the prefix it knows, and advances by `length` regardless — skipping
tail bits it cannot name costs nothing, because the header already paid for
the skip.

**Reads validate everything — prefix-relative, never version-equal.** Rows
are untrusted input under SPEC §5's doctrine, unchanged: every range,
bound, count, tag and padding check the type wire performs, the row payload
performs. But validation is stated relative to the reader's own fence M and
the row's version N, because skew is the row wire's contract (§3.5, §6) —
a reader must never refuse a row merely for being newer than itself:

- **N ≤ M** — the reader holds N's exact shape in the lineage and
  validates the row in full; the decoded payload's consumed size must
  equal `length` exactly.
- **N > M** — the reader decodes the prefix it knows — M's flat shape —
  and skips the remainder by `length`. It refuses only if `length` is
  smaller than the decoded prefix consumed or a prefix field fails
  validation; payload bytes beyond the known prefix are skipped, never
  judged.

A row claiming version 0 refuses — versions begin at 1 (§10.5) — so the
two branches above are total over every accepted row. In both cases a
payload consuming beyond `length` refuses; consuming less than `length` is
legal exactly and only as the N > M skip. An offset-index entry out of
bounds or non-monotonic, and a row overrunning its slab, are read failures
always. Writes assume trusted data, as everywhere.

### 3.5 Versioning: fences over the flattened shape

**Types never version. Tables version at the row level, on the flattened
shape.**

A **version fence** snapshots the table's flat shape — the full wire-shape
rendering of the row, with types used in rows contributing their fields
*as of that moment* — into the table's **lineage**: an append-only sequence
of per-version descriptors, each hashed exactly as a unit's projection is
hashed (SPEC §3.1), so lineage is printable and diffable with the same
tooling discipline.

**The one evolution law: each version's flat shape must be a
prefix-extension of the previous.** Normatively: version N−1's canonical
rendering must be a literal line-prefix of version N's. Fields are only
appended — never removed, renumbered, retyped, re-bounded or reordered — and
the compiler enforces this with fences in the declaration, refusing edits
below the newest fence. No per-field tags, no TLV, no vtables: the problem
tags exist to solve — skipping unknown fields mid-stream — disappears when
unknown fields can only be a suffix, which is also what lets row interiors
stay bitpacked within each version's known prefix.

Reading across versions, both directions:

- **An old reader** (fenced at M < N) takes the prefix it knows and skips
  the rest of the row by `length` (§3.4).
- **A new reader** (fenced at N > M) decodes the prefix the row carries and
  materializes the missing tail at **construction values**: each absent
  field takes its specified default, or its zero value where none is
  specified — exactly what a freshly constructed object holds (SPEC §4.2).
  Specified defaults are projection facts, retained in every version's
  descriptor, so every reader build materializes the same value. (Contrast
  SPEC §5's untaken-branch rule, which reads *zero values, not defaults*:
  an untaken branch is wire semantics inside one shared shape, while a
  missing tail is a field the row never carried — construction semantics,
  not wire semantics.)

**The fence syntax is ruled (§14 q2): in-file `version N` markers,
enforced against the retained lineage** (§10.5 carries the full grammar):

```
type Vec3
{
    x float32
    y float32
    z float32
}

table Spawn
{
    position Vec3
    kind     uint8
    version 2
    yaw      float32 | min = -180.0, max = 180.0, resolution = 0.01
}
```

Fields above the first marker are version 1 — the unmarked head of the
body (§10.5); each marker opens the next version's appended tail; the
head version is the highest marker.
Version 1's flat shape is `position.x, position.y, position.z, kind`;
version 2 appends `yaw`. Both snapshots live in the lineage.

**A mid-row edit to a table-used type refuses the fence check.** Types stay
freely editable — until one contributes fields below a table's fence, at
which point the fence owns those fields' shape. The retained lineage is the
enforcement oracle (ruled — §14 q2): every compile re-renders each fenced
prefix from the current declarations and compares it against the retained
snapshot, so editing `Vec3.z` to `float64` after the fences above exist is
refused at the very next compile, with the shape's owner named:

```
Types.schema:6:5: Vec3.z changed from float32 to float64, but Vec3 is part
of fenced table rows: table Spawn fenced position.z as float32 at version 1,
and rows already written under that fence must decode unchanged forever.
Fields below a table's newest fence are frozen, and a type freezes wherever
fenced rows use it. To evolve Spawn, append fields after its newest fence
under a new version. To change this shape, revert the edit — or fork the
type: keep Vec3 as fenced, declare a new type with the new shape, and use
it in newly appended fields or new tables.
Vec3 is used below the newest fence of: Spawn (v1).
```

The diagnostic names every table that froze the type, because the author's
question is always "who is holding this shape still" — and every remedy it
offers is legal under this section's own law: append, revert, or fork;
never remove.

**Deleting a fenced table declaration is legal, with its lineage retained**
(PROPOSED) — and deletion **ends the freeze**: types that contributed
fields below the deleted table's fences unfreeze, except where another
fenced table still uses them. That is sound on both sides of time: no
current build writes the deleted table, so nothing new can be produced
under its shapes; and stored documents keep reading regardless, because
they carry their descriptors embedded by default (§14 q1) — the walker
needs the lineage, not the declaration. Deletion ends future fences and
future writes, never past readability.

### 3.6 Layouts and the parallel build

One logical table, one view, two blessed layouts:

- **Compacted with an offset index — the storage form.** Rows are
  variable-length, back to back; the offset index is one relative offset per
  row from the table base. Built wide by the §3.2 pattern: measure per row
  index range in parallel, prefix-sum, scatter-write. The index is part of
  the stored table and is validated on load (monotone, in-bounds, final
  offset consistent with the table's extent).
- **Fixed-slot — the in-memory form**, blessed for hot builds: every row
  occupies the head version's worst-case row size (the row's MaxBytes, the
  SPEC §6.1 bound), and offset = index × slot. Single-phase — no measuring,
  no prefix sum — trading bytes for build simplicity, per the family's
  allocate-the-maximum discipline. The degenerate offset function means the
  same random-access contract holds with no index stored.

Both layouts hold the same rows under the same descriptors and walk under
the same view; converting between them is a per-row copy, embarrassingly
parallel, with no reference in any row caring where any row sits (§4).

Tables introduce a **per-row measure** into the generated surface — the
measure stream the parallel build's first phase runs. This is a table-only
addition: SPEC §6.1's "no generated measure function" rule for types stands
unchanged, and the table build is exactly the real need that rule reserved
the door for.

### 3.7 Mutable instances: content and state

One distinction, kept deliberately out of the language: **content versus
state is a property of the instance, not the declaration.** Any table may be
**held immutable** — shared, hot-swappable (§5), hash-audited (§5.3) — or
**instantiated mutable** — a private working copy. Same declaration, same
descriptors, same view; nothing in the `.schema` file says which, because
the same table is legitimately both (a spawn table is content on the server
and a working copy in the editor).

The mutable form is the fixed-slot layout §3.6 already blesses, and it is
always available because the grammar bounds every field: the worst-case row
size is static. In memory, a mutable instance holds its rows as the
generated native structs the compiler already emits (SPEC §6.1), with
bitpacking at the boundaries — save, wire, transfer — so mutation is plain
field assignment on plain structs, at memory speed. A mutable table of
types is a component array with index joins: **ECS falls out as a special
case**, not a feature.

This vindicates §1's cornerstone rather than bending it: mutating shared
data through a directory identity is what pointer semantics *means*. And
versioning is untouched, because fences govern shape while mutation changes
values — which yields a free feature: **a packed document of state tables
is a save game**, and an old save loads through the fence machinery,
prefix-read with the tail construction-defaulted (specified default, else
zero; §3.5) — versioned save compatibility
across patches, from the content machinery, at no new cost (§6 takes this
further).

The honest costs of the mutable instance, named:

- **Declared capacity** — a mutable instance preallocates its slab, so
  state use wants the bound in the declaration. PROPOSED, the attribute
  grammar's own: `table Ships | capacity = MaxShips`. Consistent with
  no-unbounded-anything (§12).
- **A free list** for row lifecycle — create and destroy against the
  fixed-slot slab. Runtime machinery beside the instance, never format.
- **Optional generation counters** for stale-ref detection into mutable
  instances — again runtime level, never stored format.
- **Concurrency stays the application's**, via index-range ownership — the
  same discipline as the parallel build (§3.2). No locks in the format.

The hot-is-cold audit (§5.3) applies to **immutable instances only** — a
mutable instance diverges from the canonical document by definition; saving
it produces a new document. The keyed-lookup bonus of §4.2 survives
wherever keys stay fixed while values mutate.

## 4. Documents and references

### 4.1 The document

A stored artifact is a **document**: N tables plus a **directory** — the
table count and, per table, its identity, head version, row count, and
extent (offset and length within the document, relative like every offset).
The exact directory rendering is specced with the implementation; PROPOSED:
byte-aligned fixed-width fields, little-endian, for the same reason as the
row header.

**The document is the closed world.** Every reference in every row targets
something in the same document, which buys total load-time validation — a
document either loads with every reference resolved and every bound checked,
or it refuses; no dangling reference can exist in a loaded document — and it
kills the fixup pass: references are logical indices resolved at read, so a
worker can emit a reference to directory entry 7 before anyone knows where
table 7 will sit in the file. Large documents therefore build wide at two
levels: within each table by measure, prefix-sum and scatter-write (§3.2),
and across tables concurrently into separate slabs, with assembly reduced to
concatenation plus the directory.

### 4.2 References are values, never addresses

Three reference forms, all of them values under the inherited grammar —
and all of them spelled with existing constructs, deliberately: SPEC §4.8
refuses bare-scalar union payloads and keeps `if` out of array elements,
and the forms below respect both by using **wrapper types**, so no SPEC
edit is needed to express any of them (§10.6 carries worked examples):

- **A row ref** — a value naming a row within one table. For an unkeyed
  table it is the row index, a ranged integer already expressible today
  (`owner uint16 | min = 0, max = MaxShips - 1`); for a table that
  declares a `key` field (ruled — §14 q3) it is the key's value, and the
  ref field's type matches the key field's. §4.4 is what a *declared* ref
  adds.
- **A discriminated ref** — a `union` whose variants are wrapper types,
  each holding the target's index or key field: the tag names the target
  space, the payload carries the value. The existing §4.8 machinery,
  unchanged — the wrapper is the declared type the payload rule requires.
- **A table ref** — a directory index naming a whole table, nullable. The
  spelling is a **single-variant union over a wrapper type holding the
  directory index**: the tag over [0, 1] IS the presence bit — one bit,
  then the index when present — and `None` is the null, so the wire of
  §4.8 is exactly "one presence bit; if present, the directory index as a
  ranged integer". Arrays of nullable table refs follow for free, because
  a union field composes in arrays. This is what makes hierarchical
  content expressible — levels owning spawn tables, items owning affix
  tables, arbitrary nesting — while every constraint holds: relocatable,
  because an index through a directory points nowhere; validatable,
  because the target space is the document's own directory, shipped with
  the data.

The forms compose under the ordinary grammar, with no new constructs: an
**array of table refs** is a bounded array of the single-variant-union
spelling above; a **map** is a keyed table whose rows pair a `key` field
(ruled — §14 q3) with a table ref, and a **set** is a keyed table whose
rows are the keys. A runtime bonus falls out of
immutability: a loaded table never changes, so keyed lookup can be a
build-time perfect hash or a sorted key column, stored as plain relocatable
data inside the table — O(1) keyed access with no runtime hashing machinery
and no pointers.

**Document-level graphs between tables are permitted.** Serialization,
validation and relocation are all indifferent to sharing and cycles among
table refs; only recursive tooling walks care, and the view's walker carries
a visited set. No DAG restriction is needed or wanted. Recursion within one
table is likewise permitted: a row may reference a row in its own table,
which yields trees, DAGs and full graphs at unbounded depth.

Stated precisely, the design carries a complete data algebra: product types
via type composition, sum types via unions, collections via tables and
arrays, references via row and table indices through the directory, and
recursion via rows referencing rows in their own table. The one boundary is
deliberate and load-bearing: everything is expressible only in its **arena
spelling** — structures as rows in slabs joined by indices, never inline
self-nesting, never memory pointers. That is the one representation that is
simultaneously relocatable, parallel-buildable, serializable and
hot-swappable (§5), so the restriction selects the correct spelling rather
than limiting what can be said.

**Whether a ref may cross versions** — the sub-question the design record
left open — is answered by the reading model rather than by a rule, and is
stated here as derived, not decreed: a ref names an **identity** — a row
key or index, a directory entry — never a shape. Per-table version
independence (§5.1) already lets a target table advance fences while the
referring table stays old, so the target's version is orthogonal to the
ref by construction; validation is always against the target's **current
instance**, at build, load, or swap (§5.2). No cross-version rule exists
because nothing in a ref can see a version.

### 4.3 Refused by name

Continuing SPEC §4.11's discipline — what falls outside the contract is
refused by name, with the reason:

- **Cross-document references** — a reference cannot name anything outside
  its own document; the document boundary is the fence. Open-world identity
  is application data — keys the application declares, stores and resolves —
  never language machinery.
- **Open-world row refs** — a fully general "row anywhere" reference is
  refused: it is a pointer with extra steps, it cannot be validated at build
  or load time (the target is not on hand), and it exports a dispatch
  obligation to every consumer of the data forever. The minimal stopping
  point is the closed world of §4.1; anything further takes the standard
  instruments, one candidate at a time.

### 4.4 `ref` — the annotation, a named follow-on

A **declared ref** is wire-identical to the integer it decorates. It adds
exactly two things, neither of them bytes:

1. **Existence validation** at build and load time — the named target row
   or table exists in this document.
2. **Followability in the view** — the descriptor records the target space,
   so an inspector can jump from the referencing row to the row it names.

PROPOSED spelling, the attribute grammar's own:

```
owner uint16 | ref = Ships       // row index into table Ships
```

Because a declared ref changes no wire, **v1 may ship without it**: plain
indices and enums under the inherited grammar already express every
reference §4.2 names, which is how the first consumers solve this today.
`ref` is therefore specified here as its own subsection so it can land
separately — a follow-on that adds meaning, never representation. It is
legal on any integer field **wherever one appears — inside `type` bodies
included**, because types compose into rows (§10.2) and the annotated
field travels with the type; in pure packet use it is inert, exactly the
meaning-never-representation rule applied. Row identity is ruled
(§14 q3): a ref targets the declared `key` field where the table declares
one, and the bare row index otherwise.

## 5. Live documents: hot reload and patching

Hot reload falls out of the design rather than adding to it: every reference
is logical — a directory index or a row index — so no table's bytes point
into another table, and **replacing one table cannot invalidate any other**.
This is §1's pointer semantics doing its job: a table is swappable behind
its index because the index, not the bytes' location, is the identity.

### 5.1 The swap, the disk, and the patch

- **In memory**, updating one table in a huge document is a parallel rebuild
  of one slab (§3.6) plus an **atomic swap of one directory entry**, taken
  at a frame boundary; every other table is untouched, and game code
  observes nothing but new values.
- **On disk**, the directory does not require contiguity, so the update
  pattern is: append the new table's bytes, rewrite the small directory,
  compact later. The old document stays valid until the directory write
  commits.
- **The directory carries a content hash per table**, which buys three
  things in one small field: change detection (a file watch or a fleet
  publish compares hashes, nothing else), minimal patches (only tables whose
  hash moved travel, spliced as verbatim byte regions with no
  re-serialization), and integrity verification.
- **Versions are independent per table inside one document**: rows carry
  their own version headers (§3.4), so a patched table may advance to a
  newer fence while its neighbors stay old.

### 5.2 The live loop

The whole edit loop is runtime, not compile time. A designer edits values
through tooling that walks the view — the write half of §2.2 — and
**repacking one table is the parallel build of a single slab against an
existing descriptor**: milliseconds for a content table, no compiler
anywhere in the loop. Delivery is a file watch locally, or the changed
table's bytes over the wire identified by content hash. The swap validates
the closed world at the publish point — refs into and out of the new slab —
then atomically replaces one directory entry.

The versioning model gives the strongest property here for free: a table
whose declaration gained appended fields under a new fence **hot reloads
into a RUNNING build that has never seen the new shape**, because that
build reads the prefix it knows and skips the tail by length (§3.5).
Designers iterate on content and shape against a live game; the programmer
recompiles on their own schedule.

### 5.3 Hot is cold

Two requirements close the design: a swap must be fast, and the system must
detect when it is broken beyond working, because arbitrary hot changes
accumulate until they cannot work anymore. Both answers are structural:

- **A swap runs the full cold-load validation** — closed-world refs, the
  prefix rule, every bound — and on any failure **refuses and reports while
  the game keeps running on the old table**. A refused swap costs nothing.
  There is no apply-and-hope path.
- **Hot is defined as cold.** Every swap also updates the canonical
  document, and the invariant is that live state after any number of swaps
  is identical to cold-loading that document. Because loaded tables are
  flat, pointer-free slabs, the equivalence is a per-table hash compare —
  mechanical, and cheap enough to run continuously in development. Drift in
  the data layer has nowhere to hide; shape drift is bounded separately by
  the fence, which refuses anything but appends.

What genuinely can accumulate is the application's derived state — caches
computed from values that changed — the classic hot reload concern, and the
application's to solve. The data layer's contribution is making the reset cheap:
a full cold reload is the same parallel load path, so the escape hatch is a
**data reboot costing seconds**, with the hash audit proving the reboot was
a no-op for the data while it clears the application's caches.

### 5.4 Green and amber

The choice between working-forever and working-mostly-with-complaints is
resolved by **partitioning the change space mechanically**:

- **Green — provably always hot-appliable**: value edits within existing
  shapes, added rows, added tables, and appended-field fences. The subset
  is total by construction — the validator can prove every such change
  applies — and it covers the daily iteration loop.
- **Amber — everything else**: refused loudly and instantly, with the exact
  reason and the smallest remedy attached — a data reboot, seconds, on the
  same parallel load path.

**No migration machinery, anywhere, ever.** Systems that truly built
live-state migration teach that restarting beats writing migrations, and a
total system that cannot refuse degrades silently — the accumulation
failure this design exists to prevent. Because the boundary is mechanical,
tooling shows it before a save: an edit is green (applies hot) or amber
(needs reboot), learned from the editor rather than from a broken session.
The same partition governs production live-ops: a provably green content
patch hot-applies to running servers with no downtime; an amber patch
schedules a restart. One validator, one contract, from the dev loop to the
live fleet.

## 6. The row as a versioned message

A table row packed alone **is a versioned message**. The row encoding —
version plus length header, prefix evolution, construction-defaulted tails
(specified default, else zero; §3.4,
§3.5) — is self-delimiting and skew-tolerant, so one row sent over a socket
is a message two peers can exchange while on different builds. And a
build's generated table code carries its table's **full in-file lineage**
— every version at or below its head is derivable from the fence markers
alone — which is what licenses standalone-row reading of N < M: a bare
row needs no document and no embedded descriptor to be read by any build
at or past its version. Nothing new
is defined here; the property falls out of the row wire, and it completes
the wire story:

**Types are the wire for peers who deploy together; table rows are the wire
for peers who cannot.** The type wire (SPEC §3) is same-or-refuse — one
protocol id, both ends redeployed as one — and stays exactly that. The row
wire serves the peers the type wire deliberately refuses: an editor talking
to a running game, tools talking to pipelines, telemetry events read years
after they were written, through the lineage. The versioned-message niche —
the place a tag-based format would otherwise be reached for — is served by
the same machinery, with no tags and no vtables.

The proving use cases, named: **save games and per-user profile data**. They
are the same use case at different addresses — versioned state documents
that outlive every build that wrote them, in the player's hands and in the
studio's. Both ride the fence machinery unchanged: a years-old save or
profile loads through its lineage, prefix-read and construction-defaulted
(specified default, else zero; §3.5), and
hostile or corrupted data refuses cleanly on read (§3.4) — a property the
formats usually reached for here never offered.

## 7. Canonical JSON

The positioning this section serves: anywhere JSON would otherwise be
reached for, this design keeps JSON's virtues rather than trading them.
Any document dumps to JSON and loads back through the view with no per-type
code — hand-inspection, diffing and interop stay — while the stored and
wired form is no longer the text: typed, bounded, validated on every read,
versioned with enforced lineage, far smaller, and readable at memcpy speed.

**Canonical JSON is read and write through the type and table views,
specced as tightly as the wire.** The projection is the canonical text of
the shape; canonical JSON is the canonical text of the values. One JSON
spelling per document or type value, one parse back, through the view's
generic walker — one implementation per language, over descriptors, no
per-type code anywhere.

Canonical means, normatively — one spelling, deterministic, and **total
over all wire-legal values**: every value a generated reader accepts has
exactly one canonical dump, headroom values included:

- **Fields in declaration order** — the wire order, so a dump reads like
  the declaration. **No insignificant whitespace.** **Escapes are pinned**:
  the two-character short forms (`\"` `\\` `\b` `\f` `\n` `\r` `\t`) where
  they exist, `\u00xx` with lowercase hex for the remaining mandatory
  control characters, and nothing else escaped.
- **Untaken `if` branches are omitted from the dump**; import materializes
  their fields at construction values (specified default, else zero —
  §3.5's rule for absent fields, applied to absent branches).
- **Integers with storage wider than 32 bits as strings, uniformly** —
  `int64`/`uint64`, the 128-bit family, `bits(N > 32)` — such values do
  not survive double-precision JSON readers; 32-bit-and-narrower storage
  dumps as JSON numbers.
- **Finite floats by shortest round trip**: `float64` rendered by the
  ECMAScript Number-to-String algorithm — a published, deterministic
  algorithm — and `float32` by the same algorithm over the
  float32-shortest digits (the shortest decimal that parses back to the
  exact `float32`); lowercase `e` in exponents (PROPOSED). **Negative
  zero dumps as `-0.0`**, and import maps it to the negative-zero bits.
- **ALL non-finite values dump as an object carrying the exact bits**:
  `{"nonfinite": "0x7ff8000000000000"}` — 8 hex digits for `float32`, 16
  for `float64`, lowercase. One spelling class for every NaN payload and
  both infinities, canonical NaN included — no name-based special cases,
  and the round trip is byte-exact by construction.
- **`fixed`/`ufixed` as the raw scaled integer** — exact and
  deterministic; a decimal spelling would lose nothing but adds taste
  calls (PROPOSED as raw). Wide fixed storage rides the string rule above.
- **Bytes as base64.**
- **Enums by name when the value matches a declared variant** (`None`
  included), **as a number otherwise** — `| max` headroom values are
  wire-legal (SPEC §4.2) and must dump; a dump that says `"Railgun"`
  survives a renumbering the wire would not.
- **Flags as an array of set variant names in declaration order, plus one
  trailing STRING element carrying any set headroom bits, omitted when
  zero** — `["Firing", "Disabled"]`, or `["Firing", "96"]` when widened
  bits 5 and 6 are set. A string always: flags storage is `uint64`, the
  wider-than-32 class.
- **Unions as a single-variant object** — `{"sphere": {...}}`; `None` is
  the empty object.
- **A document dumps as an array of `{"table": name, "rows": [...]}` in
  directory order** (PROPOSED). Descriptors are not dumped: canonical
  JSON is **schema-relative** — the walker reads the declarations, so the
  spelling follows them (enum names, union variant objects, a nullable
  table ref by its union spelling, a bare index as the integer it is),
  and two schemas declaring the same bytes differently dump differently,
  by design. Refs are integers and dump as integers; the `ref` annotation
  changes meaning, never spelling (§4.4).

The conformance property is **golden-gated round trip: pack of import of
dump is byte-identical** — `pack(import(dump(x))) == pack(x)`, over the
full corpus including headroom, non-finite and negative-zero values — and
with every spelling above pinned, the property is total: every wire-legal
value has exactly one dump, and every dump packs back to the bytes it came
from. **Import is a validated read**, untrusted exactly like wire bytes
(SPEC §5): ranges, bounds, capacities and tags refuse loudly, so a
hand-edited file cannot smuggle a value the wire would reject.
**Versioning works in text**: a dumped table carries its version, and an
old file imports through the fences with the tail construction-defaulted
(specified default, else zero; §3.5), exactly as an old row reads.

The workflow this unlocks: **content lives as text in git** — diffable,
mergeable, reviewable — and is **packed at build, byte-reproducibly**
(canonical encoding is already a contract, SPEC §6.1). JSON keeps the jobs
it is good at — humans, version control, interop — and never ships.

## 8. What this buys, use case by machinery

The coverage, enumerated — each use case named with the machinery that
serves it, so a gap would be visible:

- **DCC exporters and asset conditioning** — the parallel pack (§3.2),
  immutable documents, content hashes (§5.1).
- **Editors and inspectors** — the view's walk and write half (§2.2)
  against the hot-reload loop (§5.2).
- **The editor-to-game protocol itself** — row messages, skew-tolerant
  (§6).
- **Analytics and telemetry** — versioned rows as events; mixed-version
  logs readable years later through lineage (§6, §11).
- **Build and patch pipelines** — per-table hashes, minimal verified
  patches (§5.1).
- **Save games and profiles** — state documents, fence-compatible across
  patches (§3.7, §6).
- **Config and live-ops** — immutable documents, green-and-amber hot
  patching (§5.4).
- **A universal exporter** — any document dumpable to canonical JSON
  through the view, with no per-type code (§7); tabular flattenings such
  as CSV are library affordances beside it, not specced here.

## 9. Architecture: a runtime library family

Tables and views ship as a **new runtime library family, named `table`** —
`table.js`, `table.cs` and siblings, one per target, each beside and
depending on its serialize counterpart — rather than as fully generated
per-language output. The library is forced by the design's own
requirements, not chosen:

- **Tooling must open documents its build has never seen.** Only a library
  walking retained descriptors — embedded in the document by default
  (§14 q1) — can do that; generated code is by definition the shapes the
  build knew.
- **Old fences exist only as data in a lineage.** Versioned reading is
  interpretation of retained descriptors — it requires the walker by
  construction.

The split follows the measured hot/cold line of §2.3. **The library owns
the generic machinery**: document load and the directory, the validation
driver, the atomic swap and the hot-is-cold hash audit (§5), the patch
splice, the view walker in both directions, canonical JSON (§7), free
lists and generation counters for mutable instances (§3.7), and the
measure/prefix-sum/scatter build engine (§3.2). **The compiler generates
only the thin fast layer**: descriptors as data (§2.2) plus monomorphic
current-version row codecs and accessors — exactly where generation
measurably earns its speed.

This is the serialize-to-schema relationship promoted one level: serialize
is each language's home for bitpacked streams, schema generates the code
that speaks them, and the table library is each language's home for
everything that is not bitpacked types — documents, lineage, walking,
swapping, building.

## 10. The table source language

The complete source surface for tables, gathered in one place for the
language review. Everything here lives in ordinary `.schema` files beside
types, compiled in the same unit under the same rules; a construct is
marked PROPOSED where its spelling is new, and unmarked where it is the
existing language doing its ordinary work.

The grammar, extending SPEC §4.2's productions (`table` is already a
reserved word — SPEC §4.1 — held for exactly this):

```
Declaration = Package | Const | Enum | Flags | TypeDecl | Union
            | TableDecl .                                  // TableDecl added (PROPOSED)
TableDecl   = "table" ident ( TableBlock
            | AttrSection NL TableBlock ) NL .             // qualifiers: capacity, below
TableBlock  = "{" { Item | Fence } "}" .                   // Item is §4.2's, unchanged
Fence       = "version" IntLiteral NL .                    // fence marker (PROPOSED);
                                                           // "version" is contextual;
                                                           // a LITERAL, never an expression
```

`version` is a contextual keyword, like `flags` and `union` — and a fence
number is an **integer literal, deliberately**: a fence is a lineage
ordinal, not a tunable, so a const-named or computed fence adds nothing
and is refused. The restriction is also what keeps the grammar
unambiguous: inside a table body, the identifier `version` followed by an
integer literal is a fence; followed by an identifier it is an ordinary
field of a declared type — `version SemVer` stays a legal field — and the
literal-versus-identifier split disambiguates in one token, so the grammar
stays LL(2). The attribute
vocabulary (SPEC §4.2, closed and checked) grows three entries: table
declarations take **`capacity`** (valued — §10.4, PROPOSED); table fields
additionally take the valueless **`key`** (§10.3, PROPOSED); and the
valued **`ref`** (the §4.4 follow-on) is legal on any integer field
**anywhere — inside `type` bodies too**, since types compose into rows
(§10.6's `ShipEntry` is the canonical spelling). In pure packet use the
annotation is inert: meaning, never representation (§4.4).

### 10.1 Declaration, and the inherited field grammar

A table body is a type body under the archive contract — every field kind
the language has, unchanged (§3.3):

```
table Item
{
    id         uint32 | key
    name       string(64)
    kind       ItemKind
    price      uint16 | min = 0, max = MaxPrice
    damage     DamageFlags
    durability ufixed(8, 8) | min = 0, max = 255
    sockets    [..4]SocketKind
    active     bool
    if active
    {
        cooldown float32 | min = 0.0, max = 60.0, resolution = 0.01
    }
}
```

Nothing in the body is new: ranged integers, enums, flags, strings,
bounded arrays, fixed point, branches — the §4.2 grammar of SPEC, hosted
by `table` instead of `type`. Only the hosting keyword and the attributes
of this chapter are PROPOSED.

### 10.2 Composition: types in rows

Composition is in from day one (ruled — §14 q4). A declared type is a
field group in a row, exactly as in a packet:

```
type Vec3
{
    x float32
    y float32
    z float32
}

table Prop
{
    position Vec3
    rotation Quat
    kind     PropKind
}
```

The row's flat shape is `position.x, position.y, position.z, rotation.x,
..., kind` — the flattening §3.5 fences. The same `Vec3` serializes naked
in a packet and under the row regime here; the container decides the
contract (§3.3). The price of composition is fence ownership: once `Prop`
is fenced, `Vec3`'s fields below that fence are frozen wherever `Prop`
uses them (§3.5).

### 10.3 The `key` attribute

PROPOSED — the ruled optional declared key (§14 q3): at most **one field
per table**, marked valueless:

```
table Ship
{
    id     uint32 | key
    class  ShipClass
    health int32  | min = 0, max = MaxHealth
}
```

A keyed table is addressed by its key: refs into it carry the key's value
(§10.6), lookup is by key, and hot reload survives row reordering and
compaction (§5). Key uniqueness is validated at build and at load, like
every document invariant (§4.1); the lookup column or perfect hash a
loaded instance uses is library machinery over relocatable data (§4.2),
never stored semantics. A table without a `key` field is addressed by row
index. `key` on a `type` field, on more than one field, or on a
non-scalar field is refused with the rule named.

### 10.4 The `capacity` attribute

PROPOSED — the declared bound a mutable instance preallocates (§3.7), on
the declaration line:

```
table Ship | capacity = MaxShips
{
    id     uint32 | key
    class  ShipClass
    health int32  | min = 0, max = MaxHealth
}
```

`capacity` bounds live rows in a mutable instance, consistent with
no-unbounded-anything (§12); it does not touch the row wire, the fences,
or an immutable instance's row count, which the directory carries per
document. A table never instantiated mutable needs no `capacity` and pays
nothing for its absence.

### 10.5 Version fences

The ruled fence syntax (§14 q2): in-file `version N` markers. Fields above
the first marker are version 1; each marker opens the next version's
appended tail; the head version is the highest marker. A complete
two-fence file:

```
// Spawns.schema
package content

const MaxSpawns = 4096

enum SpawnKind { Grunt, Turret, Boss }

type Vec3
{
    x float32
    y float32
    z float32
}

table Spawn | capacity = MaxSpawns
{
    id       uint32 | key
    position Vec3
    kind     SpawnKind
    version 2
    yaw      float32 | min = -180.0, max = 180.0, resolution = 0.01
    version 3
    elite    bool
}
```

Version 1 is `id, position.x, position.y, position.z, kind`; version 2
appends `yaw`; version 3 appends `elite`; the head version — what this
build writes — is 3. All three snapshots live in the lineage, and the
retained lineage is the enforcement oracle (§3.5): markers say where the
fences fall; the lineage says what they froze. Markers run consecutively
from 2 upward — version 1 is the unmarked head of the body — and a gap,
a repeat, or a marker out of order is refused with the expected number
named.

### 10.6 References in source

All reference forms are spelled with existing constructs plus the §4.4
`ref` annotation — wrapper types satisfy SPEC §4.8's payload rule, so no
SPEC edit is needed (§4.2):

```
const MaxTables = 16

// A row ref into a keyed table: the field's type matches the key's.
// `ref` is the §4.4 follow-on annotation — wire-identical to the bare int.
table Squad
{
    leader uint32 | key
    ships  [..8]ShipEntry
}

type ShipEntry
{
    ship uint32 | ref = Ship            // key-valued ref into table Ship
}

// A discriminated ref: a union over wrapper types, one per target space.
type ShipRowRef    { index uint16 | min = 0, max = MaxShips - 1 }
type StationRowRef { index uint16 | min = 0, max = MaxStations - 1 }

union TargetRef
{
    ship    ShipRowRef
    station StationRowRef
}

// A nullable table ref: a single-variant union over a wrapper type
// holding the directory index — the tag IS the presence bit (§4.2).
type LootTableRef
{
    directory uint8 | min = 0, max = MaxTables - 1
}

union LootRef
{
    loot LootTableRef
}

table Encounter
{
    spawn    uint32  | ref = Spawn
    target   TargetRef
    loot     LootRef
    alt_loot [..4]LootRef                // arrays compose for free
}
```

Every spelling above is today's grammar except the `ref` annotation
(§4.4, follow-on) — a union field in an array, a wrapper type as a
payload, a ranged integer as an index are all SPEC §4.8 and §4.3 doing
ordinary work.

### 10.7 Documents are not declared in source

**There is no document declaration.** A document — which tables, in which
directory order — is an **assembly artifact of the build or the runtime**
(§4.1): the packer's input, the swap's output, never the language's
concern. Tables are the source-level unit; the same `table Spawn` may
appear in a shipped content document, a save game, and an editor's
working document, and the declaration neither knows nor cares. This is
§1's semantics doing its job — declarations define shapes; documents are
the heap, and heaps are built, not declared.

## 11. Lineage, retention, and the descriptor across time

Each table version's descriptor hashes exactly as the unit's projection does
(SPEC §3.1): low 64 bits of SHA-256 over the canonical rendering. Lineage —
the ordered list of a table's version descriptors — is therefore printable
and diffable end to end, and "what changed between v3 and v4" is a text
diff, the projection discipline extended in time.

Where retained descriptors live is ruled (§14 q1): **embedded in every
document by default** — archives are self-contained, and any build with
the walker can read any document found years later, lineage on hand or
not — **with a strip flag** for size-critical pipelines that guarantee
lineage availability. A stripped document carries the version hashes and
is readable wherever the lineage is on hand; the safe form is the default
form. Everything else in this document is independent of that ruling: the
row wire, the fences, the validation rules and the view machinery are
identical under either form.

## 12. What tables refuse to be

Stated so the boundary survives contact with feature requests:

- **No runtime relational semantics.** A ref is integrity as a check, never
  a behavior: no joins, no cascades, no queries. Tables are a content
  system, not a database.
- **No in-place mutation of a SHARED table's bytes.** For an immutable
  instance the unit of change is the whole table: a swap replaces one slab
  and one directory entry (§5.1), and every change lands in the canonical
  document under the hot-is-cold invariant (§5.3) — nothing patches a row
  inside a shared slab. A mutable instance (§3.7) is a private working
  copy, and mutation there is the point; what is refused is mutating an
  instance others share.
- **No migration machinery, ever** (§5.4). A change is green and applies
  hot, or it is amber and the remedy is a data reboot; no transform code
  runs between versions, in either direction.
- **No unbounded anything.** Row counts, string capacities, array bounds and
  table counts are declared bounds, as everywhere in the family.
- **No self-describing `type` wire.** Documents embed their descriptors by
  default (§14 q1); the `type` wire of SPEC §3 stays an unattributed bit
  stream regardless. Descriptors travel with archives, never with packets.

## 13. The unfreeze

The projection carries FROZEN tokens today — `table=false` (and
`message=false`) on every type line — kept so the refusals of SPEC §4.11
moved no protocol id (SPEC §3.1). Landing `table` unfreezes deliberately:

**Dropping `table=false` from the projection is a `ProjectionVersion`
bump** — every protocol id moves, once, visibly, by the mechanism built for
exactly this — taken deliberately or not at all. `message=false` and
`round=nearest` are untouched; their refusals stand.

## 14. The rulings, recorded

The four questions this draft opened are decided. They keep their numbers
permanently — text above cites them as `SPEC-TABLES §14 qN` — and each
entry records the ruling and the reasoning that carried it, in the manner
of SPEC §9.

**q1 — Where retained descriptors live** — ruled: **embedded in every
document by default, with a strip flag** for size-critical pipelines that
guarantee lineage on hand. The safe form is the default form: an archive
found years later reads with no lineage anywhere; the lean form is an
explicit opt-in that accepts mortality for size. §11 carries the
consequence.

**q2 — Fence syntax and enforcement** — ruled: **in-file `version N`
markers (candidate A), enforced against the retained lineage.** The
declaration reads as the timeline it is, and the lineage is the oracle: a
fenced prefix that no longer matches its retained snapshot is an error at
the very next compile, which is what makes §3.5's refusal immediate rather
than deferred. In-file markers alone could never catch a mid-row type edit
(the source carries no history of the type); the lineage can, and does.

**q3 — Row identity for refs** — ruled: **an optional declared key** — a
`key` attribute on one field (§10.3). A table that declares a key is
addressed by it: refs carry the key's value, hot reload survives row
reordering and compaction, and maps and sets fall out as keyed tables. A
table without one is addressed by bare row index: free, dense, and exactly
right for content whose row order the build owns. The cost lands only
where the benefit is taken — uniqueness validation and a lookup column
exist only in keyed tables, softened by the immutability bonus (§4.2).

**q4 — Composition in tables** — ruled directly: **composition from day
one.** Declared types are field groups in rows from the first version;
the flattening and fence-ownership rules of §3.5 are the machinery that
makes it safe, and they ship together. The sequencing question this draft
posed is closed.

What remains PROPOSED is spelling, not design, held for the language
review of §10: the `table` declaration syntax and the §10 grammar
productions themselves; the row-header widths (§3.4); the table-deletion
rule (§3.5); the `capacity` attribute (§3.7, §10.4); the `key`
attribute's surface (§10.3); the `ref` annotation's spelling (§4.4); the
directory rendering (§4.1); and the canonical-JSON taste calls (raw
fixed-point spelling, `None` as the empty object, the float-rendering
algorithm, the document dump shape — §7).

## 15. What changes in SPEC.md when this lands

For the adjudication record — the present-state edits landing requires,
none of them taken by this draft:

- **§1 Scope and Non-goals**: "No wire-format versioning, anywhere",
  "schema is not an evolution system", and "data that must outlive builds
  is out of its scope entirely" all narrow to the `type` wire; "No
  self-describing wire data" gains the archive carve-out (documents embed
  descriptors by default — §14 q1). The scope statement adds `table` and
  the view.
- **§3.1**: gains the explicit statement that table declarations do NOT
  enter the wire shape projection — the protocol id covers the type wire
  alone, and table shapes hash into their own lineage (§3.3).
- **§4.11**: `table` moves from refused-by-name to a pointer at the table
  section; the `table=false` FROZEN token is dropped under §13's
  `ProjectionVersion` bump.
- **§1**: the runtime-library table gains the `table` family beside the
  serialize family (§9).
- **§6.1**: the no-generated-measure rule gains the table exception
  (§3.6), and the generated-output list gains descriptors-as-data and the
  current-version row codecs (§9).
- **§7.2**: the conformance program grows table gates — golden lineage,
  cross-version read matrices (old reader × new row, new reader × old
  row), document-validation refusal suites, and the canonical JSON round
  trip (`pack(import(dump(x))) == pack(x)`, golden-gated — §7).

This document then folds into SPEC.md as sections of the one normative
reference, and SPEC-TABLES.md retires.
