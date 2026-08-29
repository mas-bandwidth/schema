# schema — views and tables (draft specification)

**Status: DRAFT, for adjudication.** This document specifies two features that
are not in the language: the **view** (a generated reflective descriptor
surface — [#157](https://github.com/mas-bandwidth/schema/issues/157)) and
**`table`** (versioned content declarations —
[#158](https://github.com/mas-bandwidth/schema/issues/158)). It is written in
SPEC.md's register and to its rigor, but it is not yet normative: SPEC.md
promises that where the compiler and the document disagree one of them is a
bug, and the compiler refuses `table` by name today (SPEC §4.11). §13 lists the
open questions this draft deliberately does not decide, and §14 lists the
SPEC.md text that changes when this lands. Syntax shown in examples is marked
**PROPOSED** wherever it is not already in the language.

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
type Spawn table=false message=false
  field kind kind=0 width=8 signed=false
  field health kind=0 width=32 signed=true intrange=[0,1000]
```

The view work is specifying this artifact as a **generated, runtime-loadable
descriptor** and emitting it in each target language. The descriptor format is
specced once, per the SPEC discipline, before any emission is written; the
facts it carries are exactly the projection's inclusion list (SPEC §3.1) plus
the names the projection already renders. Nothing is computed twice: the
descriptor and the protocol id derive from the same IR facts, so a descriptor
that disagrees with the generated wire code cannot exist without the id
moving.

### 2.2 What is generated

Per target language:

1. **Per-declaration static metadata** — a constant descriptor value for each
   `type`, `enum`, `flags`, `union` and `table` in the unit: the declaration's
   name and kind, and its field list in wire order, each field carrying name,
   kind, width/signedness, declared bounds, string/bytes/array capacities,
   float range/resolution, fixed `I`/`F`, specified default, and branch
   structure. Generated as data, not code — one table the walker indexes.
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
loading that version's descriptor (§3.5, §13 q1) and walking the row through
it. This is the whole trick — tables need no second reflection system,
because the view already is one.

## 3. The table

### 3.1 Two nouns, two contracts

- **`type`** — the moment's contract: same-or-refuse, one protocol id, naked
  bitpacked wire, freely editable (any edit moves the id; both ends redeploy
  together). Reader and writer are the same build, by contract.
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
`align` items — and declared types as field groups, pending §13 q4. That is
most of the table's power at zero new constructs, and it composes with the
versioning model because rows are length-prefixed and sequentially decoded:
variable-length fields do not fight prefix evolution (§3.5).

The composition rule that gives the one-language property: **the container
decides the contract; the type decides only the shape.** The same declared
type is bitpacked naked inside a packet and encoded under the table regime
inside a row. Types do NOT implicitly become tables — the explicit `table`
declaration is what carries the retention contract and licenses the compiler
to enforce append-only exactly where archives exist, while types stay freely
editable.

Tables and their versions contribute to the unit like any declaration:
a table's shape projects and hashes (§3.6), and table declarations share the
unit's one flat namespace (SPEC §4.6).

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
07 00 FA 03            payload: id = 7 in bits 0..15;
                       health − 0 = 250 in bits 16..25 (10 bits);
                       zero-filled to the byte
```

Ten bytes total. An old reader that knows only version 1 reads the header,
decodes the prefix it knows, and advances by `length` regardless — skipping
tail bits it cannot name costs nothing, because the header already paid for
the skip.

**Reads validate everything.** Rows are untrusted input under SPEC §5's
doctrine, unchanged: every range, bound, count, tag and padding check the
type wire performs, the row payload performs; a `length` that disagrees with
the decoded payload's consumed size, a `version` outside the reader's known
lineage (subject to §13 q1's descriptor availability), an offset-index entry
out of bounds or non-monotonic, and a row overrunning its slab are all read
failures. Writes assume trusted data, as everywhere.

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

**PROPOSED fence syntax** (the candidates and the enforcement question are
§13 q2; this example uses candidate A):

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

Fields above the first `version` marker are version 1; each marker opens the
next version's appended tail; the head version is the highest marker.
Version 1's flat shape is `position.x, position.y, position.z, kind`;
version 2 appends `yaw`. Both snapshots live in the lineage.

**A mid-row edit to a table-used type refuses the fence check.** Types stay
freely editable — until one contributes fields below a table's fence, at
which point the fence owns those fields' shape. Editing `Vec3.z` to
`float64` after the fences above exist is refused the next time the fence
check runs against the retained lineage — at the declaration of the next
fence at the latest, and at every compile where the lineage is on hand
(§13 q2) — with the shape's owner named:

```
Types.schema:6:5: Vec3.z changed from float32 to float64, but Vec3 is part
of fenced table rows: table Spawn fenced position.z as float32 at version 1,
and rows already written under that fence must decode unchanged forever.
Fields below a table's newest fence are frozen, and a type freezes wherever
fenced rows use it. To evolve Spawn, append fields after its newest fence
under a new version; to change Vec3, remove it from fenced tables first.
Vec3 is used below the newest fence of: Spawn (v1).
```

The diagnostic names every table that froze the type, because the author's
question is always "who is holding this shape still."

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
prefix-read with the tail zero-defaulted — versioned save compatibility
across patches, from the content machinery, at no new cost (§6 takes this
further).

The honest costs of the mutable instance, named:

- **Declared capacity** — a mutable instance preallocates its slab, so
  state use wants the bound in the declaration. PROPOSED, the attribute
  grammar's own: `table Ships | capacity = MaxShips`. Consistent with
  no-unbounded-anything (§11).
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

Three reference forms, all of them integers under the inherited grammar:

- **A row index** — a ranged integer naming a row within one table. An index
  is already expressible today (`owner uint16 | min = 0, max = MaxShips - 1`);
  §4.5 is what a *declared* ref adds.
- **A discriminated ref** — a union of typed refs, when the target may be a
  row in one of several tables: the union's tag names the target space, the
  payload carries the index. The existing `union` machinery, unchanged.
- **A table ref** — a directory index naming a whole table, nullable via a
  presence bit. Wire: one presence bit; if present, the directory index as a
  ranged integer. This is what makes hierarchical content expressible —
  levels owning spawn tables, items owning affix tables, arbitrary nesting —
  while every constraint holds: relocatable, because an index through a
  directory points nowhere; validatable, because the target space is the
  document's own directory, shipped with the data.

The forms compose under the ordinary grammar, with no new constructs: an
**array of table refs** is a bounded array of nullable directory indices —
the array grammar composed with the reference forms above; a **map** is a
keyed table whose rows pair a key with a table ref (keyed lookup arrives
with the row-identity ruling, §13 q3). A runtime bonus falls out of
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
separately — a follow-on that adds meaning, never representation. Whether a
ref's row identity is the index or a declared key field is §13 q3.

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
version plus length header, prefix evolution, zero-defaulted tails (§3.4,
§3.5) — is self-delimiting and skew-tolerant, so one row sent over a socket
is a message two peers can exchange while on different builds. Nothing new
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
profile loads through its lineage, prefix-read and zero-defaulted, and
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

Canonical means, normatively:

- **Fields in declaration order** — the wire order, so a dump reads like
  the declaration.
- **64-bit and wider integers as strings** — `uint64`, the 128-bit family
  and wide `fixed` raws do not survive double-precision JSON readers;
  narrower integers are JSON numbers.
- **Doubles by shortest round-trip**; **non-finite floats as `"NaN"`,
  `"Infinity"`, `"-Infinity"` strings** (raw-bit float fields can carry
  them; JSON cannot, unquoted).
- **Bytes as base64.**
- **Enums and flags by name** — the descriptor carries the names; a dump
  that says `"Railgun"` survives a renumbering the wire would not.
- **Unions as a single-variant object** — `{"sphere": {...}}`; `None` is
  the empty object.
- **Table refs as indices, `null` for optional-empty** — the presence bit
  spelled the JSON way.

The conformance property is **golden-gated round trip: pack of import of
dump is byte-identical** — `pack(import(dump(x))) == pack(x)` — which pins
every spelling rule above the same way golden wire bytes pin the encodings.
**Import is a validated read**, untrusted exactly like wire bytes (SPEC
§5): ranges, bounds, capacities and tags refuse loudly, so a hand-edited
file cannot smuggle a value the wire would reject. **Versioning works in
text**: a dumped table carries its version, and an old file imports through
the fences with the tail zero-defaulted, exactly as an old row reads
(§3.5).

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
  logs readable years later through lineage (§6, §10).
- **Build and patch pipelines** — per-table hashes, minimal verified
  patches (§5.1).
- **Save games and profiles** — state documents, fence-compatible across
  patches (§3.7, §6).
- **Config and live-ops** — immutable documents, green-and-amber hot
  patching (§5.4).
- **A universal exporter** — any document dumpable to CSV or JSON through
  the view, with no per-type code (§7).

## 9. Architecture: a runtime library family

Tables and views ship as a **new runtime library family, named `table`** —
`table.js`, `table.cs` and siblings, one per target, each beside and
depending on its serialize counterpart — rather than as fully generated
per-language output. The library is forced by the design's own
requirements, not chosen:

- **Tooling must open documents its build has never seen.** Only a library
  walking retained descriptors — carried in the document or fetched by
  hash, whichever §13 q1 rules — can do that; generated code is by
  definition the shapes the build knew.
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

## 10. Lineage, retention, and the descriptor across time

Each table version's descriptor hashes exactly as the unit's projection does
(SPEC §3.1): low 64 bits of SHA-256 over the canonical rendering. Lineage —
the ordered list of a table's version descriptors — is therefore printable
and diffable end to end, and "what changed between v3 and v4" is a text
diff, the projection discipline extended in time.

Where retained descriptors live — embedded in every document, referenced by
hash, or embedded by default with an opt-out — is §13 q1, and it is the one
open question that shapes the stored bytes. Everything else in this document
is independent of its answer: the row wire, the fences, the validation
rules and the view machinery are identical under either.

## 11. What tables refuse to be

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
- **No self-describing `type` wire.** Whatever §13 q1 decides for documents,
  the `type` wire of SPEC §3 stays an unattributed bit stream. Descriptors
  travel with archives, never with packets.

## 12. The unfreeze

The projection carries FROZEN tokens today — `table=false` (and
`message=false`) on every type line — kept so the refusals of SPEC §4.11
moved no protocol id (SPEC §3.1). Landing `table` unfreezes deliberately:

**Dropping `table=false` from the projection is a `ProjectionVersion`
bump** — every protocol id moves, once, visibly, by the mechanism built for
exactly this — taken deliberately or not at all. `message=false` and
`round=nearest` are untouched; their refusals stand.

## 13. Open questions, for ruling

Numbered for citation as `SPEC-TABLES §13 qN`; each is presented, not
decided.

**q1 — Where retained descriptors live.** (a) **Embed** each referenced
version's descriptor in every document: archives are self-contained — any
build with the walker can read any document found on a disk years later,
lineage on hand or not — at a per-document size cost that amortizes poorly
for tiny documents and beautifully for large ones. (b) **Reference by
hash**: documents carry only version hashes; leaner files, but reading
requires the lineage to be on hand, so a document separated from its schema
history is opaque, and the failure arrives years after the choice.
(c) **Embed by default, with a flag** to strip for size-critical pipelines
that guarantee lineage availability: the safe default and the lean option,
at the cost of two document forms existing in the wild. Both honest costs on
the table: (a) and (c) make documents bigger; (b) makes them mortal.

**q2 — Fence syntax, and where the check runs.** Three candidates:

- **A: flat markers.** `version 2` as an item between field runs (§3.5's
  example) — the declaration reads as the timeline it is, one body, no
  nesting; `version` becomes a contextual keyword like `flags` and `union`.
- **B: version blocks.** `version 2 { price uint16 }` — appended fields
  grouped explicitly per version; more structure, more indentation, and the
  common case (read the current shape top to bottom) reads worse.
- **C: no syntax.** Fences live only in retained lineage (a committed
  snapshot the compiler compares against); the declaration stays clean, but
  the source no longer shows where the fences fall, and a reader of the
  `.schema` file cannot see what is frozen.

The enforcement half: check against in-file fences, against the retained
previous projection, or both. In-file fences alone cannot catch a mid-row
type edit (the source carries no history of the type); lineage alone is
candidate C.

*Recommendation — marked as a recommendation:* **A, enforced by both** —
markers in the file so the frozen boundary is visible where the fields are
declared, retained lineage as the oracle so every edit below a fence is
caught (§3.5's worked refusal), and the two cross-checked so a fence marker
that disagrees with lineage is itself an error.

**q3 — Row identity for refs: index or declared key.** An **index** is
free (it is the offset-index ordinal), dense, and validated by a bounds
check — but it is positional: any tool that reorders, compacts or merges
rows must rewrite every inbound ref, and identity does not survive across
documents (consistent with §4.3, which refuses cross-document refs anyway).
A **declared key field** is stable under reordering and meaningful to
humans and merge tooling — but it costs a per-table uniqueness check, a
lookup structure at load or build, and it edges toward the relational slope
§4.3 fences off. The reading model should decide this, not an added rule.

Hot reload (§5) is ammunition on the key side of the scale, presented as
such and not as the decision: row refs into a swapped table go stale when
the replacement dropped or reordered rows. The closed world makes
revalidation at the publish point cheap and loud either way, but **stable
declared keys make hot reload robust where bare indices make it fast and
fragile** — an index-addressed ref is only as durable as the target table's
row order, and the daily live loop swaps tables constantly. Keys also carry
the collection story: maps and sets are keyed tables and arrive with this
ruling (§4.2). Against that stands everything in the previous paragraph,
plus the immutability bonus that softens the key cost (build-time perfect
hash or sorted key column, stored as relocatable data — §4.2).

**q4 — v1 composition in tables.** Ship **record composition** (declared
types as field groups in rows) in v1, or ship **fields-only** rows with
composition as the named follow-on? Composition is the one-language
property delivered immediately — the same `Vec3` in a packet and a row —
and §3.5's flattening and freezing rules exist precisely to make it safe;
but those rules are also v1's subtlest machinery (a type edit refusing a
fence two declarations away), and fields-only rows ship the archive
contract with none of it. The follow-on costs nothing on the wire either
way: a flattened composed field and a directly declared field render
identically in the descriptor, so composition can land later without moving
any fenced shape.

## 14. What changes in SPEC.md when this lands

For the adjudication record — the present-state edits landing requires,
none of them taken by this draft:

- **§1 Scope and Non-goals**: "No wire-format versioning, anywhere" and
  "data that must outlive builds is out of its scope entirely" narrow to
  the `type` wire; "No self-describing wire data" gains the archive carve-
  out §13 q1 decides. The scope statement adds `table` and the view.
- **§4.11**: `table` moves from refused-by-name to a pointer at the table
  section; the `table=false` FROZEN token is dropped under §12's
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
