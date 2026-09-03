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
  half of it; the bench leg holds the codec half. **A fixed table and a
  `type` are semantically the same thing — a struct.** What separates them
  is the wire each rides and the versioning that comes with it, and nothing
  else; the constructs below are the table wire's, not a different idea of
  what a value is.
- **The variable-length class pays for exactly what it buys** — pointers,
  an arena on the building side, a region on the reading side, a
  reference indirection per edge. This is where "less performant" lives,
  and it is deliberate.

**Where the effort goes is decided by which SIDE is hot, and the forms
differ.** The wire and the cooked form are **read-hot and write-cold**: they
are written once, offline, by a tool or a build pipeline (§7's pipeline
paragraph says where that runs and how its output is addressed), and read by
the game. Optimise their readers; their writers are a build cost. **The
BLOCK FORM (§19) is the one form hot on BOTH sides** — written every frame in
one language and read every frame in another — so it is the hottest path
tables have, and the fastest-correct mission bites hardest there. §12.1 is
the gate that decides whether it earns its place.

**THREE FAST PATHS, and one deliberately slower one** — the owner's framing:
*"notice how we now have fast paths for three things? types, cooked tables,
and fast write/read tables like render data."* Each is fast for its own
reason, and the reasons do not transfer:

- **Types (SPEC.md)** — same build on both ends, so the bytes ARE the struct
  under a hardcoded bitpacked wire and one protocol id. Nothing is negotiated
  because nothing may differ.
- **Cooked tables (§7)** — the work is done OFFLINE and once: tooling builds,
  cooks, and endian-fixes for a known target, and the game memory-maps the
  result and points at it. The reader does no parse because the writer
  already did every part of it.
- **The block form (§19)** — both sides GENERATED against one layout the
  compiler settled, so the producer goes wide with nothing to synchronise and
  the consumer indexes rows at a pitch it reads. Nothing is discovered at
  frame time because everything was decided at build time.

**And the tolerant wire (§3) is slower on purpose**, because it is the one
form that CROSSES VERSIONS: ids, kinds, lengths and a read report are what
let any reader read any data, years apart, and every one of them is a byte
the three fast paths do not spend. That is the trade this document exists to
make, and it is why the fast paths are accelerators BESIDE the wire rather
than replacements for it.

**How much slower is a BYTE COUNT, and the count is measured.** The tables
bench's one shape, `TableMixed`, mirrors the type corpus's `BenchMixed` field
for field, so the two wires carry the same declared content and the ratio
between them is the price of tolerance and nothing else
(`bench/tables/README.md`). A record rides in **2391 bytes against the type
wire's 438**, and **1487 of those 2391 — 62% — are framing**: field ids, kind
bytes, lengths, element counts and body terminators, not values. That count is
the trade, and it is where the per-MESSAGE factor comes from. What it does not
license is a codec slower than those bytes require: the fixed-table rung above
holds the per-BYTE cost to the bench, so a per-byte gap is a defect to explain
or close and never the wire's price.

**And one path is generic on purpose and may allocate** — the reflection
surface (§8): the field walk, the text form (§16), the packer (§17), a
viewer. In the owner's words: *"we also have the flexibility of the
reflection based stuff that is more generic and does allocations if we want
it for tooling, editor, whatever where perf and allocations aren't such a
big deal."* It buys REACH — walk any declaration by name without knowing its
type — and it pays in speed and in allocation, which is the right trade
where a tool or an editor is the caller and the wrong one anywhere the
three fast paths run. **That is the ladder whole**: three fast paths, one
slower path that crosses versions, one generic path that trades speed for
reach.

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
  otherwise is foolish: the arena on build, the region on load. It allocates
  **at BUILD time, in bulk, never per record on the fill path**: a
  worker carves its nodes out of a slab the arena already holds (§6.4), so
  going wide costs no allocation and no lock per node.

  **WHO CALLS THE ALLOCATOR IS PER FORM, and it is worth saying exactly.** The
  arena's segments and `Lock`'s packed region are the RUNTIME's own calls —
  `calloc` and `malloc` by name in both C++ and C — and a caller-provided
  allocator is not threaded through them today. What the caller does own is the
  REGION a load fills: `LoadMeasure` sizes it and the caller supplies it, which
  is allocation with the caller holding the pointer. The BLOCK form (§19.1) is
  the one surface that takes a caller-provided allocator with malloc semantics,
  and there it is real: C++ takes an alloc/free pair with a context, C the same
  three as a struct, used once at build time and never on the fill path. Other
  backends allocate inside their runtime and say so. No port contorts itself
  toward zero allocation for a variable-length table.
- **EVERY READ PATH ALLOCATES NOTHING**, in every class, on every form. A
  load fills caller-owned storage, a region is caller-owned, `Open` and
  `BlockOpen` point at bytes the caller already has, and the reflection walk
  reads in place. The allocation in this document is a BUILDING cost, and
  building is TOOLING's path — the game points at the cook (§7).

**Backend status: C++ and C, and C#, Go, Rust, Java, JavaScript and Elixir for
the FIXED class.** C++ is the reference, and its generated text is the C-like
dialect of `serialize.h` — C header spellings, no STL, every call into the C
library behind a hook the program can define (§13.9). C++ and C carry both
classes; C#, Go, Rust, Java, JavaScript and Elixir carry the fixed class (§6.1)
— optionals, enum-keyed arrays, the text form (§16) and all — and each refuses
a unit whose closure declares a pointer, naming its variable class as a
follow-on. Every other backend refuses a unit that declares tables at all, by
name, with this document cited. The remaining per-language backends are named
follow-ons (§15).

**ELIXIR IS THE READING TIER, and the tier is a property of the LANGUAGE rather
than of the port.** A BEAM term has no layout a producer could write, so this
backend never produces a block or a cook: it OPENS one another build wrote and
reads every slot at its offset. The tolerant wire and the text form are whole —
measure, save, load, the report, `FromJson` and `ToJson` — because those are
about bytes and not about addresses. Seven spellings are the language's and
each is named where it is spelled: the READ REPORT is a value the caller
threads, because the BEAM has no mutable struct; STORAGE is SPEC §6.1's Elixir
column, so a `string(N)` is a binary whose `byte_size` IS the used length and an
array is a list whose `length` IS the count, with no companion to keep in step;
a `?T` is `nil` when absent; an enum-keyed array's slots are a TUPLE on a
`table`, so a slot is reached in constant time, and the packet emitter's LIST on
a `type`, whose struct is the packet emitter's and whose storage this wire
changes nothing about — a walk rather than a reach, stated at the accessor's
own site; a non-finite float travels as `{:nonfinite, bits}`, the convention
the packet emitter already established; every refusal is `{:ok, result}` or
`:error` with a bang form beside each writer, the packet emitter's reader
verdict and its writer contract; and `Open` — both the block's and the cook's —
takes a `lead` beside the bytes — how many bytes past an aligned base the
caller's buffer begins — because §7 and §19.2 check the alignment of the BASE
and a BEAM binary has no address a caller can observe or place. That last one
is an ADDITION and not a subtraction: stating the fact makes the check a real
one, where the alternative is a leg that cannot refuse an unaligned base at
all, and `make tables-elixir-block-lead` holds the rule over every lead in
0..64 with a negative control that drops the check from the emitter.

**And what a reading-tier leg cannot claim, said plainly.** "The read path
allocates nothing" is a claim about a language with caller-owned buffers, and
Elixir has none: a decoded value IS an allocation and a sub-binary over the
caller's bytes is a small one. What the leg holds instead is that the COUNT per
iteration does not move — heap words, refc-binary allocations and reductions,
pinned per case, re-pinned deliberately, with a negative control that reds it —
which is the same instrument the Rust leg's allocation audit uses and a
different number.

**THE SOAK BESIDE IT GATES ON A FLOOR, and the reason generalizes past this
port.** A managed runtime's memory readings are not levels: a process's heap
size is CAPACITY a collection may leave grown, and a binary allocator's figure
counts CARRIERS, so a corpus with one large instance in it reads bimodally
forever without leaking a byte. A gate on the last sample, or on any sample,
therefore reds for the runtime rather than for the code. A LEAK does one thing
no carrier and no grown heap can imitate: it lifts the MINIMUM. So the gate
compares the floor of the first third of the samples against the floor of the
last third, and the two negative controls are different sabotages on purpose —
a generated load that allocates more and frees it reds the COUNT and lifts no
floor, and one that retains a copy of its bytes reds the FLOOR. Both sabotage
the EMITTER through `go build -overlay`, as the fuzz controls do, because a
control that sabotages the instrument's own loop proves the reading responds
and not that a leak in generated code would be found. Two instruments, two
questions, and neither one answers the other's.

**JAVASCRIPT IS THE READING TIER TOO, and by the same rule.** A backend that
controls no struct layout — no offsetof, no sizeof, no way to place a field —
cannot PRODUCE a block or a cook, and reads both instead: every field is read at the
offset the compiler settled, through a `DataView` with an explicit little-endian
flag at every call, and every generated accessor is a function of
(view, offset). The tolerant wire (§3) is the one form such a backend both
writes and reads, because that wire is a byte stream rather than a layout. What
the tier costs is stated rather than implied: the LAYOUT CONTRACT (§19.3) has no
second model to check the first against in a language with no layout, so what
runs once before any Open is the generated constants' own consistency plus the
one pair that IS two derivations — each array's pitch constant against the size
of the row it names, which is §19.5's named negative control. And a reference is
BOUNDED before it is followed (§6.3): C++ trusts the delta and adds it, where a
JavaScript read past a view throws, so a delta that leaves the region is a
refusal rather than an escaping exception.

**The RUST backend's own three divergences**, each forced by the language and
each named where it is spelled rather than discovered in the source. The
READ REPORT is the codecs' own parameter rather than a member of the reader: a
Rust reader holding `&mut TableReport` could not hand a sub-reader out of its
own buffer while that borrow stood. And the COOKED and BLOCKED records are
their own `<Name>Row` structs, as they are in C#, because SPEC §6.1's Rust
storage column spells `string(N)` as `[u8; N]` where the layout model spells
`char[N + 1]`; the layout contract is asserted over them AT COMPILE TIME, with
const asserts over `core::mem::offset_of!`, where C++ has `static_assert` and
C# has a check run at type initialization. A UNION in a cooked record is a
named follow-on there for the same reason it is absent from the block form: a
Rust union is a real enum with no committed payload layout, and the
`#[repr(C)] union` twin is a pass of its own.

And the THIRD: **the two accelerators are cargo features**, both on by
default. §19's rule is that the block form costs nothing unless you reach for
it, which C++ answers by not including the header and C# cannot answer at all
— one assembly compiles every file of the unit. Rust answers it with
`default-features = false`, which compiles no block module and no cook module;
`--features cook` and `--features block` take one without the other. What is
NOT behind either feature is the unit's BUILD VERSION (§20 answers "which
build?", not "which form?", and both forms compare against it) and the
blittable `<Name>Row` records, because a cooked record IS the blittable row
(§7.2, §19.3) and a block-only build wants the same structs a cook-only build
does.

**GO emits four sources per unit file**: `<Base>Table.go` (the storage structs,
the codecs, the reflection descriptors), `<Base>Block.go` and `<Base>Cook.go`
(the two accelerators, §19 and §7), and one `<Home>TableJson.go` per unit (the
generic text walk). Two spellings are Go's own and the reason is at each site:
an enum's identity pair (`TableEnumId` / `TableEnumValue`) is a METHOD on the
enum type, because Go has no overloading and a free pair would have to mint a
per-enum unit-level name §11 does not claim; and an enum-keyed array's storage
IS the plain `[E.Max]T` the schema means, with the shift and both refusals in
one `TableKeyed` helper, because Go has neither operator overloading nor a
generic array extent. Its layout contract is a generated `init()` that REFUSES,
naming the record, the field and both numbers, where C++ has `static_assert`,
Rust has a const assert and C# a check at type initialization.

**JAVA's six divergences**, each forced by the language and each named where it
is spelled. **The method names are lowerCamelCase** — `patrolMeasure`,
`patrolSave`, `patrolLoad`, `patrolReset` — which leaves §6.1's NAME-FIRST order
exactly as it is and spells the case the way Java's one naming rule and this
backend's own packet half (`writeVec3`, `readVec3`) already do. **The unit's namespace is the PACKAGE and a public type lives in
a file of its own name**, so the shared runtime is ONE FILE PER TYPE —
`TableReport.java`, `TableReader.java`, `TableJson.java` and the rest — rather
than one home file: file-order independent by construction rather than by a
rule, and a table-free unit emits not one of them. **There are no unsigned
types**, so a decode local widens to the smallest signed type holding the wire
kind's whole range (u8/u16 to `int`, u32 to `long`) and u64 compares through
`Long.compareUnsigned`, while storage stays bit-transparent in the same-width
signed type, the packet emitter's own convention. **There are no ref structs**,
so a nested body is bounded by MOVING THE READER'S LIMIT rather than by slicing
a sub-reader — which is what lets a hoisted reader allocate nothing at all.
And **there are no structs and no pointers**: a block row and a cooked record
have no Java type to lay out, so `<Name>Row`'s generated accessors read each
field at its offset out of the caller's `byte[]`, and the base's ALIGNMENT is
the OFFSET's residue rather than an address's — the same arithmetic, so the
same refusals.

**Which makes Java's half of the layout contract a DIFFERENT half, and it says
so rather than pretending.** C++, C# and Rust each have a runtime layout that
can disagree with the compiler's model, and each asserts against it —
`static_assert`, a check at type initialization, a const assert over
`offset_of!`. Java has no record layout at all, so there is no second model to
check against and a check that claimed one would be theatre.
`TableBlockLayout` and `TableCookLayout` assert the one disagreement the
language CAN have: the ACCESSORS' offsets against the DESCRIPTORS', two
derivations the generator makes separately, so a walker reading a row through
the descriptors and a consumer reading it through the accessors cannot be
reading two different records. What refuses a FOREIGN block or cook is `open`,
which compares every number the instance carries against this build's.

**AND THE SIXTH IS A CEILING RATHER THAN A SPELLING: the two accelerators read
out of a `byte[]`, which stops at 2 GiB.** §7 states the scale it is built for in
the owner's own words — *"100mbs or many gigabytes of data in Assets.bin"* — and
the scale fixtures reach a gigabyte, so this is the one Java divergence that
costs a stated requirement. C# meets the same `int` ceiling on its span overload
and answers it with the pointer form beside it; Java at `--release 17` has no
second spelling, because the foreign-memory API (`MemorySegment`) is not stable
before 22. **The FFM overload is the named follow-on** (§15), and until it lands
a catalog past 2 GiB has no Java reader. Both `open`s take a `long` length
that a `byte[]` can never fill: it is the seat that overload takes, and the
generated source says so at the site rather than leaving a reader to meet the
ceiling by hitting it.

**One RULING rather than a spelling, in the cook**: a reference resolves through
`<Root>Cook.at(slot, size)`, which BOUNDS THE WHOLE RECORD — `[target, target +
size)` inside the region — and answers "no target" otherwise, the same answer it
gives a null. C++, C# and Rust hand back a pointer and let the walk decide,
because a cook is trusted input and an out-of-region deref there is undefined
behaviour a sanitizer catches. Java has none to preserve: an unchecked deref is
an exception escaping into a caller that asked a question, which is what the
readers' fuzz oracle forbids.

**Bounding the target's START is not enough, and the size parameter is why.** A
start bound passes every check the reader makes and then throws one call later,
in the caller's first field read past the region's end — §7.1 blesses a cook
carrying data alone, so there are no attribution bytes to absorb the overrun.
The size is the pointee's own `<Name>Row.size`, which every call site knows.
`make tables-java-cook-extent` is that forgery as a gate and its negative
control puts the start-only bound back and requires the gate to go red.

**BIG-ENDIAN, per backend.** The C++ leg proves the wire, the block form and
the cooked form crossing the byte order on a real big-endian target under an
emulator. The Rust surface is CHECKED for that same target — the pinned
toolchain cross-compiles to it — which is more than a compile: every cooked
record's and every block projection's layout contract is a const assert over
`size_of` and `offset_of`, so the check EVALUATES the whole layout model for
big-endian. The RUN-time half is a named follow-on: the codecs are
order-neutral by construction, and a cross-and-emulate Rust leg would turn that
from a construction into a proof. **The GO leg RUNS**, like C++'s: the driver
cross-builds for s390x and answers the wire, the report and both text surfaces
under the same pinned emulator, plus the two FOREIGN surfaces (§7, §19.1) —
a cook and a block whose magic word is byte-reversed, which every leg on every
host must refuse.

**JAVA's byte order is the READER's, not the host's**, and that is the whole of
its answer here: every multi-byte read goes through `TableBytes`, which is
explicitly little-endian, so `TableBlockInfo.byteOrder` and
`TableCookInfo.byteOrder` are CONSTANTS rather than a query of the platform. A
file of the other order is then refused twice — its magic reads back
byte-swapped and its order word is not this reader's — which is what the two
FOREIGN surfaces ask for, and `make tables-java-order` holds the same property
over a whole big-endian cook the tool wrote.

**ELIXIR's byte order is the reader's too, and by the language's own means**:
every bitstring segment the generated code matches names its endianness —
`little-unsigned-64` and the rest — so `BlockRuntime`'s magic and byte-order
words and `CookRuntime`'s are constants, and the host's order is never
consulted anywhere in the port. A big-endian BEAM reads the same bytes to the
same terms. The two FOREIGN surfaces hold the refusal, and no order gate stands
beside them because there is no platform query for one to catch.

**The BLOCK FORM (§2.7, §19) is live in C++, C, C#, Go, Rust, Java and Elixir,
and READ by JavaScript**, and it took C++ and C# TOGETHER to land, because the
form is an ABI between two languages and one
language alone cannot hold the gate it exists for (§12.1). C++ emits
`<Base>Block.h` (the projection, the generated layout asserts, the fill path
inline) and `<Base>Block.cpp` (the open path and the block descriptors); C#
emits `<Base>Block.cs` per declaring file (the block handle with its span
accessors and the table's projection record) beside `<Package>Block.cs`, the
unit's one runtime home (§19.2), which carries the blittable records with their
generated padding, the layout check and the same descriptors; Rust emits
`<base>_block.rs` per declaring file beside `block_runtime.rs`, the unit's own
runtime home — the `#[repr(C)]` projection with no generated padding, because
the representation IS the layout, the const asserts that hold it, the open
path, rows as slices over the region, and the same descriptors; Go emits
`<Base>Block.go`, the same shape again, with the blittable records carrying
generated padding as C#'s do and the layout contract as the refusing `init()`
above; Java emits `<Table>Block.java` per block-form table and `<Name>Row.java`
per record, whose generated ACCESSORS read each field at its offset because
there is no struct to lay out, with the layout contract as the
accessors-against-descriptors check above; C emits `<Base>Block.h` and
`<Base>Block.c` on the C++ pair's terms, with the layout contract asserted at
compile time under C99's own means (§20.3); and **Elixir emits `<Base>Block.ex`
beside `BlockRuntime.ex`, and it is the READING TIER** — there is no projection
record and no layout assert, because a BEAM term has no layout to assert, so the
descriptors ARE the mechanism and the typed accessors read each row field at its
offset out of a SUB-BINARY the runtime shares rather than copies. **The unit's
shared runtime is named by the PACKAGE in every port, so file order cannot
reach it (§19.2)** — and in Java the package IS the unit scope, so each runtime
type is a file of its own name and no rule is needed at all. The READ side is what the ported backends carry: a block is
laid out by a producer, and the producer's half is C++'s. The Table sources
carry not one symbol of any of it, and the build fails if one appears.

**What is absent, and by absence rather than by refusal** (§2.7): a
VARIABLE-LENGTH table has no block form — no fixed pitch anywhere in its
closure — and neither does a table whose closure carries a UNION, because
§19.3 pins the C# side to Sequential with generated padding and Sequential
cannot overlay arms. Each says so in the Block header rather than going
missing — and in the JavaScript module, which says the same thing in its own
file. Every other backend emits no Block file at all, as it emits no Table
file; those are the same named follow-on (§15).

**The VIEW's type half and unit registry (§8.2–§8.7) are specified and
unimplemented.** What ships today is §8.1: a table's descriptors, built in,
and the same descriptors for every type a table reaches. No backend emits a
view file yet; a backend that does not emit one simply does not have it,
because nothing in a schema requests it and no flag selects it (§8.4). C++
and C# take it together, because the gate it exists for is one listing both
backends reproduce, and the remaining backends are a named follow-on (§15).

**One front-end change comes BEFORE any emitter.** The generated table
runtime's names are claimed in every unit rather than only in a unit that
declares a table (§11), because the view file defines them in units that
declare none. It does not wait for a view file to exist: a name a unit may
legally declare today must not become a collision the day its view is
emitted.

## 1. Purpose

**Table is king.** There is no document that things are put into, and no
root that is anything other than a table. A file is a tree of nodes and every
node is a table: a table of tables, as deep as the data goes. Anything the
data needs is expressed as tables of tables, and where it cannot be, the
answer is to make `table` better rather than to add a construct beside it.
That rule has a consequence the rest of this page is held to: **everything
must recurse perfectly.** Every slot that can hold a value can hold a table,
at every depth, in every form (wire, text, cook, block): a table by value, a
pointer to a table, an array of tables, an array of pointers, a union whose
arms are tables, a keyed array of tables, an optional table. A slot that
refuses a table today is a gap, named in §15, not a design.

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
  exactly one slot per NAMED variant, keyed by the variant, and nothing for
  `None`.
- **A union arm may be a table** (§2.6), which is what makes an evolvable
  message set expressible.
- **`was` — the rename attribute** (§5).

And one addition that is not a field spelling at all: **every fixed table has
a BLOCK FORM** (§2.7), a third form beside its wire and its cook — the same
declaration laid out so another language points at its rows (§19). Nothing
declares it, and it adds no field type, no wire kind and no byte on the wire.

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
for an OPTIONAL GROUP is `?T` (§2.3):

```
table Scene
{
    settings ?Settings   // an optional group: off the wire when it is absent
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

- **A pointer targets a `table`, and nothing else.** `*Node` names a declared
  table. Everything else is refused — `*SomeType`, `*SomeEnum`, `*SomeUnion`,
  and the buffer spellings `*bytes` and `*string`, which do not parse (§15) —
  because value-semantics data has no independent identity to point at. Nest
  it by value instead.
- **A pointer is declared inside a table body, and nowhere else.** A
  `type` body refuses one: types remain value semantics, and that is the
  founding line of the split.
- **A pointer field takes no specified default.** A fresh pointer is
  null, and null is the only value a default could name.

**An ARRAY OF POINTERS is refused, and it is a named follow-on** (§15).
`[..8]*Node` and `[8]*Node` are both refused by name, and the diagnostic says
what to write instead: a bounded array of tables BY VALUE, or a pointer to a
table that holds the array.

A pointer's STORAGE is an EIGHT-byte relocatable reference — never a
machine address — which is what keeps §9's relocatability true with
pointers in the struct. Its meaning depends on the form it sits in
(§6.3), and its width is that section's: a four-byte slot bounded a region
at 2 GiB, and the scale a cook exists for is larger than that.

### 2.2 The mode is derived, never declared

The compiler works out which class a table belongs to; the schema never
says. The rule is a least-fixed-point over BY-VALUE edges:

- A table is **VARIABLE-LENGTH** if it declares a pointer, or if anything it
  nests by value is variable-length. "Nests by value" reaches through every
  by-value edge there is: a plain nested table, an element of a bounded
  array, an element of an enum-keyed array, a member of a guarded (`if`)
  group, an optional's value (§2.3), and a UNION ARM that is a table (§2.6).
- Every other table is **FIXED-SIZE**.

Pointer edges do not propagate the mode: a table that is merely POINTED
AT stays fixed-size if it holds no pointer of its own. It gains an
allocation and a resolution entry, and nothing else.

**The BLOCK FORM does not enter this derivation — it READS it** (§2.7). The
form declares nothing, so there is nothing for the rule above to take account
of; the rule instead decides which tables have the form at all. Every FIXED
table has one and a variable-length table has none, so "which arrays are laid
out at a fixed pitch" and "which tables can be" are both answered by the mode.
A form is not a mode, and this one is derived from it.

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

**The BLOCK machinery takes the same gate, and it is held by SEPARATION**
(§19): the block storage type, `Begin`, `Open`, the row accessors and the
layout constants live in a unit's Block files and nowhere else, so a Table
header is byte-identical whether those files exist or not and the build fails
if one symbol of them appears in it. The descriptor COLUMNS (§8) the block
form reads ride in every unit as every other column does, because they
describe the language. Machinery is gated; columns are not, which is the rule
the paragraph above already states.

The assumption behind the split, stated so nobody quietly designs against
it: size and mode correlate in practice. Value-only tables are assumed
SMALL — messages, records, config fragments — so they are passed by
struct and loaded directly, and no large-flat-struct machinery is
warranted for them without a real case forcing it. Pointer-bearing tables
are where size lives, and the arena and the region are the size answer.

**LARGE DECLARED MAXIMA ARE a case forcing it, and the declaration is where
they are visible** (§2.7). A table's bounded arrays are storage like any
other — `T[N]` inline beside an `int32` count — so its BY-VALUE struct is
the sum of its declared maxima, and for the case the block form comes from
that is about 7.5 MiB. Nothing announces it but the maxima themselves, and
nothing else could: a table that declares megabytes of bounded arrays is not
covered by the assumption above, and everything about its by-value form
follows from the numbers in its own declaration rather than from a surprise.
§19.1 prices the block form's own storage; the by-value form's price is here,
where the assumption it breaks is stated.

**Three consequences, so a reader meets them here rather than in a profile.**
A `Load` of such a table needs a destination struct of that size, and the
load path allocates nothing, so the CALLER owns it. Its cook (§7) is the
struct's bytes behind a header, which means mostly-unused array tails on
disk. And the WIRE is small where the struct is not — elision writes only the
live rows (§3) — so "the file is a few hundred kilobytes and the struct is
seven megabytes" is the normal state of affairs, not a defect. The block form
exists precisely so a game never materialises that struct: it reads the
projection (§19.2). The by-value form is for tooling and for the wire.

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

The framing is exactly the non-optional field's, which makes `?T` and a
plain `T` nesting **one framing with two spellings**: for any value that is
not entirely default, a schema may move a field between the two and no byte
moves, in either direction.

**`*T` IS NOT IN THAT FAMILY, and the difference is deliberate.** A pointer
rides as a node index under its own kind `17` (§3.1), because a body that may
be named twice cannot also sit inline at one of its names. So moving a field
between a pointer and either of the other two is a REPORTED edit: §4's kind
mismatch, counted, with the field taking its declared default and the rest of
the body reading on. A shared kind would have made that edit silent — a stored
index reading back as a plausible length — and the wire spends a kind rather
than allow it.

**The remaining difference between `T` and `?T` is at the empty end**, and it
follows from the two elision rules above: a plain `T` holding nothing but
defaults is not written at all, while a PRESENT `?T` is written even then. No
direction misdecodes — an absent field reads as the declared default by value
and as absent through an optional, each of which is the right answer — but the
bytes are not identical for an all-default value, and an implementer should not
be promised that they are. The corpus holds both directions, and the
byte-identity of the two writers **over non-default content**.

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

- **Storage** is a generated KEYED-ARRAY TYPE wrapping `T slots[E.Max]` —
  ONE SLOT PER NAMED VARIANT and not one more, no count companion because
  every named slot exists. The wrapper is what gives the accessor and the
  iteration below a home; the array inside it is its ONE member, and the
  iteration surface holds no state, so the type stays trivially copyable
  and standard-layout (§9).
- **NOTHING IS STORED FOR `None`, AND THE STORAGE SHIFTS LEFT.** `None` is
  the enum's null, so it keys nothing and it takes no room: **the key `k`
  lives at storage index `k − 1`**, the extent is `E.Max` elements, and
  `sizeof( TableKeyed<T, E> ) == E.Max * sizeof( T )`. Nothing is stored
  for `None` and nothing is packed for it (§3.2, §16.2, §17.2).

  **NOTHING OUTSIDE THE ARRAY NAMES ITS SIZE**, and that is what enforces
  the rule rather than restating it. The storage type is
  `TableKeyed<T, E>`: it takes the ELEMENT and the KEY ENUM and derives its
  own extent from `E.Max`, so there is no size parameter on the type, no
  count in a constructor, and no generated constant beside it. A count a
  consumer can spell is a count that can stand one out of step with the
  storage; the number has ONE home and it is inside the type. The
  descriptors' count column (§8.1) derives the same way.

  **The accessor is `operator[]( E )`**: it takes a runtime key, REFUSES
  EVERY KEY THAT NAMES NO SLOT — `None` below the storage and anything past
  `E.Max` above it — and SUBTRACTS ONE. A key in a data-driven program IS a runtime
  value — an enum field read out of a file, a key handed in by a tool, the
  key an iteration just yielded — so this is the form call sites use, and a
  compile-time accessor taking the key as a template parameter is not
  offered: it would serve only literal keys, which is not where keys come
  from. **The shift is never written at a call site**: the accessor is the
  only place it appears.

  ```cpp
  fleet.ships[ ship_type ]   // runtime key: refuses None and past Max, reads slots[ key - 1 ]
  ```

  **THE REFUSAL STANDS IN EVERY BUILD, in every port, AT BOTH ENDS.** It is
  not a debug guard and `NDEBUG` does not remove it: **indexing by a key that
  names no slot is a PROGRAM ERROR in every configuration**, and the accessor
  ends the program rather than reading something. **THE GUARD IS SYMMETRIC**
  because the storage is: one slot per NAMED variant and nothing else, so
  `None` names none of them and neither does a key past `E.Max`. **There is
  NO undefined-behaviour path here in any build** — which is the whole reason
  the compare is unconditional, because a build that skipped it would read one
  element BEFORE the array at one end and past its END at the other.
  The cost is one perfectly-predicted compare on a path that reads config —
  ONE compare covers both ends, since the storage index is `key − 1` and
  `None`'s is `−1`, which wraps above the extent unsigned — and that is not a
  price worth a class of silent corruption.

  What varies is only how a language ends a program: C++ asserts — for the
  message, where a debugger can read it — and then ABORTS, and the abort is
  what stands under `NDEBUG`; C# throws; Rust panics; Go panics too, and
  `-gcflags=all=-B` does not remove it because it is a written compare and not
  a bounds check. A port that leans on its runtime's own bounds check has met
  half the rule: the check must be the ACCESSOR's, so the behavior is the
  reference's rather than whatever the language does at the end of an array.

  **AND WHAT THE KEY IS SPELLED AS varies too, because a language's own
  vocabulary decides it.** Every port indexes by the KEY and never by the
  storage position, and the difference is only what the key's type is at the
  call site: C++ takes the enum itself; C# takes an `int`, because the
  language has no non-boxing generic enum-to-int conversion, so the caller
  writes `(int)ShipType.Bomber`; Rust takes a `u64`, because the enum is a
  `#[repr(transparent)]` newtype and the caller writes `ShipType::BOMBER.0 as
  u64`; Go takes an `int`, and the caller writes `int(ShipTypeBomber)`. The
  cast is over the KEY in every one of them — never over the shift.

  **WHAT IS WEAKER IN GO, and the page states it rather than letting a reader
  find it.** Go has no operator overloading and no generic array extent, so a
  keyed array's storage IS the bare `[E.Max]T` the declaration means and there
  is no wrapper type to route an index through: `hull.Turrets[0]` compiles and
  reads the slot the key `1` owns. `TableKeyed( hull.Turrets[:], key )` is the
  accessor, it is where the shift and both refusals live, and it is what a
  call site holding a KEY must use — but nothing stops a call site holding an
  INDEX. Go's own bounds check still stands behind the array, so the failure
  mode is a wrong slot rather than a wrong page.

  **ITERATION is still the surface a
  consumer of a whole array should reach for**, below, because it needs no
  key from the caller at all.

- **ITERATION IS THE SAFETY**, and it is the form a consumer of a whole
  keyed array should reach for. The keyed type ITERATES EVERY SLOT — keys
  `1 .. E.Max` over storage `0 .. E.Max − 1` — yielding the KEY beside the
  ELEMENT:

  ```cpp
  for ( auto [ ship_type, ship ] : fleet.ships )   // ship_type is the KEY, never an index
  {
      ship.health *= 2.0f;                         // the element is a reference
  }
  ```

  In C++ the element is a REFERENCE to the slot, so iterating is how a whole
  array is filled as well as read, and the iteration is CONST-CORRECT: a
  const keyed array yields const elements. **A storage index never appears
  at a call site**, and neither does a lower bound, an `E.Max` nor the
  shift a consumer had to spell for itself — the pieces of the slot rule
  that were re-derived by hand at every one of them before.

  **What the range guarantees is the same in every port; what the entry
  hands out is the port's own.** A port spells the walk in its own idiom
  over exactly this range — C++ gives the type `begin()`/`end()` so a
  range-`for` works, C# a struct enumerator so `foreach` works, every other
  port the equivalent — and two things vary with the language, both of
  which a port must state:

  - **The KEY's currency is the ENUM VALUE, in every port.** Every port's
    accessor takes the enum value and every port's iteration yields the
    enum value; the STORAGE INDEX is the type's own business and reaches no
    surface anywhere. What varies is only how a language spells an enum:
    C# has no non-boxing generic enum conversion, so its indexer and its
    entry carry the value as an `int` and the CAST is written at the call
    site — the cast, never the shift. One convention across languages is
    worth the cast: a rule learned in one port is the rule in the next, and
    a port that handed out storage indices would make the number `1` mean
    the second variant in one language and the first in another.
  - **Whether the ELEMENT is a reference.** C++ yields a reference for every
    element type. Where a port's entry holds a VALUE — C#'s does — a class
    element (a nested table) is the live instance and mutating it through
    the iteration is visible, while a scalar or enum element is a COPY:
    there, ITERATION READS AND THE INDEXER WRITES. Iterating is a read in
    every port and a write in the ports that can express one.

  Nothing about iteration is stateful: the yielded pair holds a key and one
  element handle and nothing else, and the storage type is untouched.

  **The entry is a PROXY, handed out by value**, which decides the spelling:
  `for ( auto [ key, element ] : keyed )`. In C++ `auto & [ ... ]` does not
  compile — a non-const lvalue reference cannot bind to the proxy — and the
  refusal is by design rather than an omission; `auto && [ ... ]` binds if a
  reference form is wanted. C++ iterators carry NO `iterator_traits`
  typedefs: `begin()`, `end()` and `size()` are the surface, a hand-written
  forward pass works, and an STL algorithm does not, because the header
  includes no `<iterator>` (§13.9).

  **Held by test**: every keyed array in the corpus is iterated in both
  backends and every walk yields `E.Max` entries whose keys run `1 .. E.Max`;
  one negative control moves `begin()` off the first stored slot and another
  restores the `None` slot — storage `E.Max + 1` with no shift — and the
  tables suite, the layout gate and the `sizeof` assertion go red. **The
  refusal is held in the configuration that would drop it, at both ends**:
  one translation unit compiled `-DNDEBUG` indexes a keyed array by `None`
  and another by `E.Max + 1`, and both must die — so a refusal that ever
  became an assert again, or narrowed back to `None` alone, fails the gate
  rather than the reader. Each has its own negative control: the debug-only
  guard for the first, the `None`-only compare for the second.

  The wire enforces the key rule from the other side regardless: a `None`
  key never rides (§3.2).
- **In a `type` body the same spelling is a PLAIN ARRAY.** `per_team [Team]int32`
  inside a `type` generates `int32_t per_team[3]` — no wrapper, no keyed
  accessor, no iteration surface, and no `None` guard of any kind. It needs
  none: the shift means there is no `None` slot to guard, index `i` holds
  the key `i + 1`, and every index the array has is a named variant's. The
  type wire is positional (below), so there is no key to check and nothing
  to protect. **The wrapper is generated only in TABLE bodies**, and only
  the TABLE-wire ENCODING is keyed; a porter reading this section should
  not emit the wrapper for a `type`. What a `type` body does share is the
  EXTENT — `E.Max`, one slot per named variant — because that is the
  construct, not the wrapper's convenience.
- **On the TABLE wire the slots ride by NAME** (§3.2): the body carries
  `(variant id, element)` pairs, so inserting, removing or reordering
  variants leaves every surviving slot in its own home. This is the whole
  point of the construct: an ordinal-indexed array is the last positional
  vocabulary the table wire had, and it failed silently.
- **On the TYPE wire the spelling IS `[E.Max]T`** — positional,
  bitpacked, same-build, the protocol id moving exactly as that spelling
  moves it. The two spellings project identically and share one protocol
  id, and that is held by test.
- **Fixed-size when `T` is**, so the zero-cost gate holds.

**The two spellings are ONE FIELD on the type wire and TWO DIFFERENT
ENCODINGS on the table wire.** `[E]T` is kind `16` and `[E.Max]T` is
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

### 2.5 Byte buffers

**There is no buffer primitive: `*bytes` and `*string` do not parse.** A
pointer targets a declared `table` (§2.1), and `bytes` and `string` are
keywords rather than table names, so the spelling is a parse failure and not
a construct with rules. An unbounded byte buffer at its used size is a NAMED
FOLLOW-ON (§15), tracked as schema#259; today a blob is `bytes(N)` at a
declared bound, and a very large one is a table of its own pointed at.

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

**Backend status for this section: no backend, and no declaration reaches
it.** A union arm naming a `table` is refused by the checker, before any
emitter sees it — `<Name> is a table, not a union payload` — so a unit
declaring the form above does not compile today and no generated code
carries an arm that is a table. The construct is stated here rather than
withheld because the framing it lands under is already the framing a `type`
arm rides (below), so what lands is the checker's refusal lifting, not a
second decision about the wire; it is tracked as schema#258.

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

### 2.7 The block form is not declared

**Nothing in a declaration selects it, and nothing is refused for asking.**
Every FIXED table has a block form — a THIRD projection beside its wire (§3)
and its cook (§7), in which the table's own bounded arrays are laid out of
line at a fixed pitch so a consumer in another language points at their rows
instead of parsing them. A VARIABLE-LENGTH table has none, and the mode (§2.2)
is the whole of the rule: a pointer anywhere in the closure means no fixed
pitch anywhere in it.

**It needs no marker because it costs a declaration nothing.** The form is
emitted ON THE SIDE, in files of its own, and a consumer that does not include
them pays for none of it — the same shape §16's text form already takes, one
step further (§19). What a fixed table's declaration owes the form is two
names it may not spell, `magic` and `build_version` (§11), and nothing else.

The form itself — which arrays move out of line, what the projection is, what
the pitch is, what it costs and what it refuses — is §19.

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
  optional field is framed exactly as the non-optional `T` (§2.3), so the
  two are ONE FRAMING under two declaration spellings. **`*T` naming a
  TABLE is the exception**: it rides as a node index under its own kind
  `17` (§3.1), because a body that may be named twice cannot also sit
  inline at one of its names.
  The distinct kind is what makes moving a field to or from `*T` a
  REPORTED edit rather than a quiet one, for the same reason kind `16`
  exists (§3.2): a node index and a plain `uint32` are the same four
  bytes, and only the kind can tell a reader which it is holding.

  **What differs inside the family is ELISION, and only at the empty end.**
  Content decides for a by-value spelling and presence decides for a
  pointer-shaped one (above), so a by-value `T` at its defaults writes
  nothing while a present `?T` at its defaults writes its body. **For any
  content that is not entirely default, `T` and `?T` are byte-identical**,
  and that is the scope of the claim: a schema may move a field between
  them and no byte moves for such a value. At the empty end the bytes
  differ and no reader misdecodes — an elided field reads as absent (`?T`),
  null (`*T`) or the declared default (`T`), which is correct in every
  direction. Moving a
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
- **The BLOCK FORM moves no byte on this wire, and spends no kind** (§2.7,
  §19). A block-form table's bounded arrays ride here exactly as every other
  bounded array of tables does — kind `14`, element kind `13`, the LIVE
  count — and the block form is not a wire fact at all: it is a second
  projection of the same declaration, the way a cook (§7) is a third. The
  `(offset_of, count, stride)` triple is a block-form artifact and has
  nothing to ride here, because a wire form has no triple. So a tool can
  `Save` and `Load` a frame, and a diff of two frames is an ordinary table
  diff.
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
  guard an optional GROUP on the wire and not merely in the language, and
  the text form defers to this rule rather than restating it (§16.2).
  **PRESENCE, not content, decides the two pointer-shaped spellings.** An
  absent `?T` and a null `*T` are not written; a present optional and a
  non-null pointer are ALWAYS written, even when the value is entirely
  default — otherwise "absent" and "present with nothing to say" would be
  one value on the wire (§2.3, §3.1).
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
length is not a depth, and two references to one node are one node. It
moves not one byte of a value-only table: a fixed-size table has no
pointer, therefore no node table, therefore exactly the bytes §3 already
describes.

**Backend status for this section: the TOOL and the C++ REFERENCE write it;
the C port still writes the NESTED form, and every other backend refuses a
pointered unit's wire by name (§11).**
`schema pack`'s engine — the compiler-side encoder and decoder this repo runs
as tooling (§17.1) — and the generated C++ codecs are two implementations of
this section, written from it rather than from each other, and each reads what
the other wrote: the cross-implementation lock runs a graph carrying a shared
node, a chain, a diamond and a by-value nesting through both directions and
requires byte identity, and it checks the SIZES too, because the region and
attribution bytes C++ derives from the framing alone are the two parts the
tool's cook writes.

**And the CONFORMANCE CORPUS carries the class now** (schema#357), which is
what makes the matrix able to see it at all: four instances over a pointered
root — a tree, a graph whose shared node is named three times and whose tree
closes a diamond, an empty root, and a chain of 260 nodes — pinned by the
reference and read by the tool. They ride the `wire` and `report` surfaces, and
the first three ride the TEXT surfaces too (§16.7); the chain is marked
`no-text` on its line, because it nests in the text as deep as it is long and
that is past the form's depth cap. Every backend that refuses a pointered
unit's wire answers ABSENT for them, per case, so its FIXED-class pass is
untouched and the gap is a number in the matrix rather than a paragraph
somewhere. That is the row schema#349 fills, one language at a time.

**The C port is the one backend that carries a pointered unit and does not
carry this form yet**, because it was mirrored from the C++ backend while that
backend still wrote the earlier NESTED one: a pointee inline as a body under
kind `13`, no node table, and the three consequences that are the reasons this
form is the law — a node two parents name written once per parent, a pointer
chain that IS a nesting depth, and a cap on that depth. Its wire is part of
schema#349 with the seven that have no variable class at all. What every
backend DOES carry already is kind `17`'s row in the fixed-width skip rule,
because a fixed-class reader meeting a pointered writer's field has to step
over it whether or not it can write one.

**What that edit MOVED, stated once.** A pointer field's kind changed from `13`
to `17` and a pointered save gained the node table, so every pointered unit's
wire bytes moved and the TABLES BASELINE (§18) was moved with a recorded
reason. No value-only table moved one byte, which is the zero-cost property
(§2.2) doing its job, and no BUILD VERSION moved: §20.2's projection already
rendered a pointer slot as kind `17`, so the id was never describing the
nested form.

**Kind `17` costs nothing and closes an edit that would otherwise be
silent.** A node index is four bytes and so is a `uint32`, so under a
shared kind an edit between the two would report nothing in either
direction — a stored index reading back as a plausible number, a number
read as an index. The kind byte already rides, so the distinct number
costs zero bytes and one row in the fixed-width skip rule, and it makes
that edit an ordinary kind mismatch (§4). §3's rule that an unknown kind
is not skippable is what makes spending a kind expensive AFTER readers
exist; the set is closed before any of them ship.

**What a pointer edge is, and what it is not.** Only a `*T` naming a
declared TABLE takes a node index, and it is the only pointer spelling the
language has (§2.1). A table-typed UNION ARM is a by-value nesting and rides
inline as §2.6 frames it; the pointer fields INSIDE an arm are indices like
any other.

**Node numbering.**

- **`0` is null.**
- **`1` is the ROOT** — the body that hosts the node table. A pointer may
  RESOLVE to it, so a reader meeting index `1` reads the root; but a WRITER
  cannot produce one, because the root's entry is open for the whole walk and a
  reference to an open entry is a cycle (below). The index is spent on the root
  so that the numbering has one origin and a reader needs no second rule, not
  so that a child may point back.
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
  the ordinary case for a build that does not have kind `17`: that
  one stops at the first pointer field and never reaches these at all.
  Field order is not part of the contract (§3), so a reader finds them by
  id.
- A root that reaches no nodes writes none of them, like every other
  empty thing (§3).
- **The record scan is authoritative.** `node_count` is data from the
  wire: a reader scans records until the fields are consumed and takes
  what it finds, and a `node_count` that disagrees with the scan is
  **malformed**. Nothing — no directory, no region, no allocation — is sized
  from `node_count` before the scan has confirmed it.
- **The `unknown` count is per TRANSPORT FIELD, not per schema
  difference.** A reader that cannot name `0xFFFF` counts one for each
  field the node table rode in, so a large save reports several unknowns
  where a small one reports a single unknown, and neither number is a
  count of things the schemas disagree about. A tool reporting evolution
  differences should read the count that way.
  **A reader that CAN name it counts nothing**, and that is not a special
  case in the counter but a fact about what the field is: `0xFFFF` is not a
  field of the table, it is the transport the numbering rides in, and a
  reader holding the numbering has already consumed it before it decodes a
  body. An `unknown` here means "a build without kind `17`", which is
  exactly the difference §4 exists to report.

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
  two `bytes(2147483647)` fields in one table are four gigabytes of body —
  and it is refused rather than widened, because the repair is more
  nodes, which is the shape the flat encoding wants anyway, and a `u64`
  length would cost four bytes on every node in every save to frame a
  structure nobody should build. The ceiling that had to go was the
  AGGREGATE one, and the repeating field removed it.
- The **body** is an ordinary table body — fields, then the `u16` zero
  terminator, exactly as §3 describes. Everything inside is ordinary:
  by-value nesting still nests, arrays are arrays, guards still guard, and
  `string(N)` and `bytes(N)` ride inline.

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
- **No array carries indices**, because an array of pointers is refused and
  is a named follow-on (§2.1, §15). Kind `17` is a field's kind and never an
  array's element kind, so an array of `uint32` is the only four-byte array
  the wire has and there is no pair for §3's element-kind rule to hold
  apart. What the follow-on would frame — `kind = 14`, element kind `17`,
  `N`, then `N` `u32` indices — is what it costs to state, and nothing
  writes it.

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
table Node    { value int32  next *Node  palette *Palette }
table Scene   { head *Node   palette *Palette }
```

with `scene.head = A`, `A.next = B`, and `A.palette`, `B.palette` and
`scene.palette` all naming one `Palette` P, a save writes:

```
root body (Scene)
  head     kind 17  2
  palette  kind 17  4
  0xFFFF   kind 12  L    the node table, in one field here
                    node_count = 3
                    rec 1  type Node     len  { value=1, next=3,
                                                palette=4 }
                    rec 2  type Node     len  { value=2, palette=4 }
                    rec 3  type Palette  len  { id=7 }
  0x0000                 terminator
```

The root is index 1; A is 2 (`scene.head`); B is 3 (`A.next`); P is 4 —
reached while descending B, BEFORE `A.palette` is read, because the walk
is depth-first. P is written once and named three times. `B.next` is
null, so it is not written at all. **`B.palette = 4` names an index ABOVE
the node that carries it and `A.palette = 4` names one BELOW**: indices run
in the walk's order, and no reference has to respect that order. What no
reference in a save can do is name index `1`, because reaching the root
again is reaching an entry still open — the cycle refusal, above.

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
  bound on `N` is therefore `E.Max`, which is the whole extent of the
  storage (§2.4): every stored slot can ride and no stored slot cannot.
- **A `None` key NEVER RIDES.** `None` is the enum's null and keys no slot
  (§2.4), so there is no `None` slot to write — the storage holds one
  element per named variant and not one more — and **a stored key of
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

The contrast is the point. A plain `[E.Max]T` array is POSITIONAL:
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
  default — the specified default, else zero. That fallback is always
  inside the field's declared range: a range excluding zero requires a
  declared default in range (SPEC §4.6), so an absent field never lands
  out of bounds and never clamps.
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
- **A changed array BOUND** (a literal, a constant, or an `E.Max`
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

Almost every edit lands in the read report. **Exactly three do not**, and
naming the whole set is the point of this subsection — a person reading a
save game that came back wrong needs the whole of it, and the third member is
the one the committed baseline (§18) exists to refuse:

1. **A specified DEFAULT changed, added or removed.** An elided field means
   "the reader's declared default", so the same bytes now mean something
   else. Nothing was lost or skipped, so no counter can fire.
2. **A FLAGS variant inserted, removed, reordered, or renamed in place.** A
   mask is the wire's one positional vocabulary (§3), so a variant's
   identity is its bit position; moving one remaps every stored file and
   the wire carries nothing that could say so.
3. **A field's REFERENT dropped, or replaced by one that cannot STAND IN for
   it.** An enum-typed field respelled as its raw `uint16` rides under kind
   `7` either way (§4), so the stored value is read as a variant hash and
   lands on `None` — or on a real variant — with no counter to fire; and a
   nested table swapped for a twin that carries the same field ids under a
   different specified default rewrites what every stored body means while
   every id survives. The kinds are coarser than the declaration side, so
   the wire cannot see WHICH declaration a field names. This is the class
   §18 exists for, and §18.3 states the standard each vocabulary is held to.

Everything else is either reported or safe. Fields may be added, removed,
reordered and renamed under `was`; enum variants and union arms may be
added anywhere, removed and reordered; array bounds may move; a field may
change between `T` and `?T` — all of it either invisible to the wire or
counted in the report. Moving a field to or from `*T` is a kind change and
is counted (§3.1).

**Three edits that would otherwise be silent are made REPORTABLE by
construction, and it is worth saying how, because the claim above depends
on it.**

- **An enum-ordinal-indexed array** was the last positional vocabulary
  besides flags: insert a variant in the middle and every later slot lands
  one place off. `[E]T` (§2.4) closes it — keyed slots ride by name, so a
  middle insert moves no slot.
- **Changing a table field between `[E]T` and `[E.Max]T`** would then
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
at three; it is a round-trip hazard, and a tool that loads, edits and stores
a file — the save-game cycle §18 exists for — should be read as carrying
it. A person whose file came back wrong needs the three above; a person whose
tool rewrote a file needs this one.

Each of the three has its own answer:

- **Flags** are answered by DISCIPLINE, stated as law: **append at the end,
  never insert or reorder**, and retire a name in place rather than freeing
  its bit.
- **All three** are answered by MACHINERY, opt-in: the committed tables
  baseline (§18) is the history the compiler does not keep, and it
  refuses every one of them until the baseline moves with a recorded reason. It
  refuses the spelling changes above too, at compile time, ahead of the
  reader's report.

**THE THREE FRAMES, ONCE.** Three sections judge an edit and they answer
three different questions, which is why their lists differ: the READ REPORT
says what a reader can tell you happened (§4), the BASELINE says what the
compiler refuses to let you do to data already written (§18.2), and the BUILD
VERSION says whether a cooked or blocked file this build wrote is still this
build's (§20.1). The table is the reconciliation, and §18 and §20.1 cite it
rather than restate it. "Silent" in this subsection means the first column
only.

| the edit | the read report | the baseline | the build version |
|---|---|---|---|
| a specified DEFAULT changed, added or removed | silent | **refuses** | **moves** |
| a FLAGS variant inserted, removed, reordered or renamed in place | silent | **refuses** | no — a mask rides raw and a load copies it verbatim |
| a field's REFERENT dropped, or swapped for one that cannot stand in | silent | **refuses** | **moves** |
| a field's wire KIND, or an array's ELEMENT kind, changed | `kind_mismatch` | **refuses** | **moves** |
| an array changed between keyed and positional, or its KEY enum swapped | `kind_mismatch` | **refuses** | **moves** |
| a declared RANGE tightened | `clamped` | passes | **moves** |
| an `enum`'s or a `union`'s variant order or names moved | `unknown` for a name this reader lacks; a reorder is silent and safe | warns on a removal or a vanished name | **moves** |
| a field added, removed or reordered | `unknown` for an id this reader lacks; an absent field defaults | passes | **moves** |
| a field renamed under `was` | silent, and nothing is lost | passes | no — `was` holds the wire id fixed |
| an array BOUND or a string/`bytes` capacity moved | `clamped` past the reader's bound | warns on a shrink | **moves** |
| a field moved between `T` and `?T` | silent — no byte moves | passes | **moves** — the presence companion is storage |
| a field moved to or from `*T` | `kind_mismatch` | passes | **moves** |
| an `if` GUARD added or removed | silent, and the read is faithful; the cost is the next WRITE | passes | no |
| a DECLARATION renamed — a `type` or a table | silent: a declaration name is not on the wire | **warns** when a table closure reaches it, naming what carries its contents on and how many identities that candidate carries (§18.3) | **moves** |
| a `type`'s FIELD renamed, where `was` is refused (SPEC.md §4.2) | `unknown` on the table wire, whose field id is the name's hash | passes in silence | **moves**, and through the protocol id as well (SPEC.md §3.1) |

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
  field's own name is refused; `was` outside a table body is refused,
  because a `type` field has no stored identity for it to preserve — the
  packet wire is positional, so a renamed `type` field loses no data. It
  is not a free edit: field NAMES ride in the wire-shape projection, so a
  `type` rename MOVES THE PROTOCOL ID and buys a lockstep redeploy
  (SPEC.md §3.1).
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

The block form (§19) is not a third life: a block-form table is fixed-size
and has exactly the life below. What changes is how one instance is laid
out, not what kind of thing it is.

### 6.1 Fixed-size: a value

A fixed-size table is a struct. **A fixed table and a `type` are the same
thing semantically — both are structs — and what differs between them is the
wire each rides and the versioning that comes with it.** Everything below is
that wire's surface, not a different notion of a value; and a third form of
the same struct is §19's.

Its whole surface is three free functions, name first:

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

**The shared runtime blocks, and the identity their guards owe.** Several
blocks are the same in every file of a unit and are emitted into every one of
them behind a per-package `#ifndef` guard — the table primitives, the arena,
the block primitives, and the text form's walk in the `.cpp`. The guard is
what lets a consumer include one header alone AND include several of them in
one translation unit: the first copy defines, the rest fall through.

**A guard of that shape is a CLAIM, and the claim is that every block emitted
under one guard name is interchangeable with every other.** It is the same
one-definition claim the walk makes, and the walk is the one that is proven:
§16.5's generic-walk gate byte-compares the walker across every generated
`.cpp` in the corpus, and the page says why — ODR requires those definitions
to be token-identical. The other blocks make the claim and nothing proves it.

**The case that reaches a translation unit is ONE UNIT, PARTIALLY
REGENERATED**: a consumer holding `<A>Table.h` from one build of the compiler
and `<B>Table.h` from another, both of the same unit, includes them together
and gets two different block texts under one guard name. Two DIFFERENT units
of one package cannot reach this — each emits `ProtocolId` into the package
namespace, so the second is a redefinition and the build stops — so the guard
owes an answer for the partial-regeneration case and for nothing else.

**Whether that case is loud today depends on WHAT moved in the block, which
is not a property anyone should have to rely on.** A block whose STRUCTS
moved fails, because the stale header's descriptor initializers no longer fit
the fresh header's `TableTypeInfo` — loudly, but as a cascade naming
initializer elements rather than the mismatch. A block in which only a
FUNCTION BODY moved compiles clean in either include order, under
`-Wall -Wextra -Werror`, and the first-included definition wins for the whole
translation unit.

**The rule: each guarded runtime block carries an identity constant taken
over its own text, and every generated file that carries the block asserts —
UNGUARDED, so the assert always compiles — that the identity it was generated
against is the identity in scope.** Mixing two generations of a unit in one
translation unit is then a compile error naming the file, never a silent
resolution. The identity is not a version number and not the compiler's
version: **VERSIONING.md rules that generated files do not record the compiler
version, and that ruling stands here** — a release that changes no block text
must move no generated byte. An identity taken over the block's own text
moves exactly when the block moves, which is also exactly when a golden
moves.

C# needs none of it and gets none: a unit's C# files compile together into one
assembly, so the runtime is emitted into ONE file per unit (§19.2) and a
second copy is a duplicate-definition error already. There is no include order
to resolve.

**Backend status: OWED, not emitted.** No guarded block carries an identity
today and no generated file asserts one, so the silent case above is live in
every one of the C++ table emitter's guarded blocks. Tracked as schema#301,
with the negative control it must carry: perturb one header's copy of a block
and show the translation unit red, naming that header — the same control that
measured the silence, run in the other direction.

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

**The map is what makes both claims one claim.** Sharing and a cycle are the
same event seen at different times — a reference arriving at a node the walk
has already reached — and the entry's open bit is what separates them: a
reference to an entry whose descent is still OPEN is a cycle, and a reference
to one already CLOSED is sharing, resolved to the body that node already has.
A pack without the map has neither answer and gets both wrong in the same
direction: it packs a shared node again, and it discovers a cycle only by
running out of depth. The ROOT's entry is open for the whole walk, so it is
one rule and not a case (§3.1).

**Its cost, stated.** One entry per reachable node — an address, an offset and
the bit — held for the length of `Lock` and released before it returns, on the
AUTHORING side where §6.5 licenses allocation. It is proportional to NODES,
never to bytes, and nothing on the reading path ever builds one: `Load`
resolves indices through the wire's own numbering and a deref is still one add.
**Measure and pack each derive the map from the graph, and neither carries the
other's** — that is what leaves `used == total` a real check on the two walks
agreeing rather than a tautology (§3.1).

**Held by test.** The pointered corpus carries a shared leaf named twice from
one node, a shared node reached through a chain and named again from the root,
and a diamond whose closing reference is a BACK-reference; each is checked by
identity — the two references resolve to one address — and by the region's
exact BYTE COUNT, which is the half a duplicating pack cannot fake. A
self-cycle and a two-node cycle are refused by `Lock`, `Measure` and `Save`
alike, and the region relocates by `memcpy` with a back-reference in it. **The
negative control** turns the identity map into a permanent miss and requires
the tables suite to go red.

**The C spelling of all of this** (§6.1's C column). C has no member
functions, no overloading and no templates, so the same surface is free
functions under the root's own name, and the two things C++ distinguishes by
TYPE are distinguished by a nullable member here:

```c
SceneBuilder builder;                       /* the mutable life */
scene_builder_init( &builder );               /* the arena, and the root node */
Scene * root = scene_builder_root( &builder );   /* NULL once locked */

TableSink sink;                             /* WHERE a node comes from */
sink.region = NULL;                         /* exactly one of the two is set */
sink.worker = &builder.main;
ListNode * node = list_node_emplace( &sink, &root->head );

scene_builder_lock( &builder );               /* one way; builder.region is the
                                               packed region, builder.region_bytes
                                               its length */
scene_builder_shutdown( &builder );           /* releases the arena and the region */
```

- **`TableSink`** carries a `region` and a `worker` and exactly one is
  non-NULL: a region sink bump-allocates into the caller's exact region and
  leaves slots self-relative, a worker allocates in the arena and leaves them
  holding the arena offset. It is C's form of the SINK the reference threads
  through its load path as a template parameter.
- **`TableCtx`** carries an `arena` and says which encoding a WALK is reading:
  a NULL ctx, or one whose arena is NULL, is a packed region. `<T>Measure` and
  `<T>Save` take it, which is how one function covers what C++ spells as two
  overloads — one for a region root, one for a builder.
- **`<T>At( ctx, &slot )`** is the deref, and **`<T>Emplace( sink, &slot )`**
  the allocation. Both are emitted only for a table something POINTS AT.
- **The builder's members are the accessors.** C++ has `AsConst`, `Region`,
  `RegionBytes` and `Locked`; C has `builder.region` (the packed const form,
  NULL until `Lock` succeeds), `builder.region_bytes`, and
  `builder.arena.locked`. Reading the const root after `Lock` is
  `(const Scene *) builder.region` with a NULL context.

### 6.3 Two reference encodings, one slot

A pointer's eight-byte slot means different things in the two forms, and
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

**NULL IN A REGION IS A DELTA OF ZERO.** A slot can never hold the address
of the node that contains it — a node starts at its own offset and the slot
sits somewhere at or after it, and a reference to the node it lives in
would name the node's START, never the slot — so zero names no node and is
free to mean null. It is the same value null takes on the wire, where index
`0` is null (§3.1), so the two encodings agree on their one reserved value
and a walk that meets a zero does the same thing on both sides.

**THE SLOT IS EIGHT BYTES AND SIGNED, so ONE REGION REACHES EVERYTHING.**
Every delta runs from a slot to a node inside the same region, so a slot as
wide as the offsets it subtracts can express every reference any region can
hold: there is no reach to check, no refusal to write and no case to get
wrong. It is what the scale §7 is built for asks for — *"Assume we have say,
100mbs or many gigabytes of data in Assets.bin at some point."* — and a
four-byte slot would have bounded ONE REGION at `2 GiB`, which is a ceiling in
exactly the place a mesh or texture catalog meets it (§13.4).

**The cost, stated.** Eight bytes a reference instead of four, in every
VARIABLE-LENGTH record, whether the structure needs the reach or not; and the
edit moves every record that holds a pointer, so every affected unit's BUILD
VERSION moves and every cooked file in existence is re-cooked. Pre-release
that is a regeneration, and it is the whole of the price. A value-only table
still pays nothing at all: it has no pointer, therefore no slot (§3.1).
**A cook's PART LENGTHS are 64 bits for the same reason** (§7), because the
FILE is not the region: a part length has to frame whatever a form puts beside
the data, and a 32-bit field there would reimpose at cook time the aggregate
ceiling §3.1 removed.

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
one check rather than two.

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

**Backend status: `Load` writes the directory and `Lock` does not yet.** A
region a C++ `Load` produces carries the trailer this section describes, and it
has to: the directory IS the load path's numbering, filled from the framing in
pass one so that an index resolves whichever way it points, and `LoadMeasure`
reports its bytes separately so the caller may place it apart and release it
once `Load` returns (§6.5). `schema cook` writes the same form as the cooked
file's attribution part and `schema cook-check` reads it. What `Lock` produces
is still data alone — a locked region and a loaded one agree byte for byte on
the DATA and differ in that the loaded one carries attribution beside it — and
filling the directory from the pack `Lock` is already doing is the remaining
half, named in §15.

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
  not arbitrate it. `Lock` and `Save` are
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

**IT IS NOT A WIRE PROTOCOL. It is a LOAD-TRUSTED-DATA-FROM-TOOLS protocol**,
in the owner's words — *"It's not a 'wire' protocol, it's a load trusted data
from tools protocol."* That is the name and the whole frame: a wire is what
crosses a boundary between builds, and this crosses none. Tooling writes a cook
for one build and that build loads it off its own disk, so nothing here
negotiates, tolerates or defends; the tolerant table wire (§3) is what carries
data across versions, and it stays the format of record.

**What it is for**: reducing the load time of assets, and nothing else —
don't parse, just point at an mmap'd data structure loaded as it stands,
and have it work.

**What it is**: converting a table data structure into a SPECIFIC VERSION
that can be memory mapped, endian-fixed and loaded quickly by a build at
the exact version it was cooked to. That is cooking.

```cpp
const Scene * scene = SceneOpen( bytes, length ); // point at it, or NULL
```

`schema cook` writes the file; the runtime only ever points at one.

**Backend status: the TOOL, EVERY READ SIDE, and the C++ WRITE side for BOTH
classes are built (schema#251).** `schema cook`, `schema cook-check` and
`schema uncook` produce, validate and read back the form below, in both byte
orders, over the same IR the emitters consume — and `wire → cook → wire` is
byte-identical over the corpus, which is what proves the accelerator loses no
fact. Every port emits the entry point for EVERY TABLE (below) — a root is any
table — in its own idiom: the C++ backend `<Root>Open`, the C# backend
`<Root>Cook.Open`, the Java backend `<Root>Cook.open`, and the Elixir backend
`cook_open_<root>`, which takes a `lead` beside the bytes for the base-alignment
check a BEAM binary cannot carry (§7.1's alignment word, and the backend status
in §2). A game points at a cook the tooling produced, whichever language it
reads from. The WRITE side (`Cook` and `CookMeasure`, §7.6) is emitted by the
C++ backend for every table, fixed or pointered, and its bytes are the tool's,
byte for byte, in both byte orders over every instance the corpus carries;
every other language's writer is a named follow-on (§15), and until it lands
that build runs the tool.

**THE SCALE THE COOK IS BUILT FOR, stated as the requirement it is** —
*"Assume we have say, 100mbs or many gigabytes of data in Assets.bin at some
point."* / *"We would want this to be fast :)"*. Three consequences bind the
emitter. `Open` is **O(1) in the file's size**: the header match, the reserved
words, the lengths and the base's alignment, and nothing per node — which is
what the match-and-point rule below already states and what a walk of any
shape forfeits. The **byte order is settled at cook time** for the target
build (below), so the reading side runs no fix-up pass at all. And a mapped
file's **pages are touched only as they are used**, which is a property of
touching nothing at open rather than a separate mechanism. The gate is open
time flat across a 1 MB, a 100 MB and a 1 GB cook, and it rides with the
emitter.

**The pipeline, in the owner's words**: *"the optimized path is still
available, it is tooling does the build, then cooks to the rad binary format.
and the game just points at it and works."* — *"(plus endian fixups)"* —
*"this is the way."* `schema pack` writes the WIRE, and the wire is the format
of record (§17). A cook is produced beside it, next to the build that will
read it — on the machine or on a distributed cook farm — for exactly the
layouts that build knows. **TOOLING BUILDS; THE GAME POINTS.** A game loads
its own cooked assets and never a foreign one; tooling reads and writes the
generic wire, because the generic form is the one that carries every version.
And if load time does not demand the accelerator, the game just uses the
generic table: cooking is a choice made per asset, never a requirement of the
format.

**The endian fix-up is part of the COOK, and it stays.** A cook is produced in
the byte order of the build it is cooked for, so the fixing happens where the
target is known — offline, once, on the writing side — and never on the
reading side, which is what makes `Open` a match and a point rather than a
pass over the region (below). The byte order is a fact of the build version
(§20.1), so a cook for a foreign order is not this build's file and refuses.

**A cooked artifact is CONTENT-ADDRESSED by a pair — the hash of its source
asset, and the unit's BUILD VERSION (§20)** — and that pair is the tuple the
runtime searches for, the tuple a distributed build cache produces under and
serves from. It is why the cooking side is a build cost rather than a runtime
one: the work happens offline, once per (asset, build version), and the game
does a lookup. That is the fact the performance ladder cites when it calls the
wire and the cook read-hot and write-cold.

**The ASSET HASH is the hash of the WIRE FILE the cook was produced from.**
The wire is the format of record and a cook is produced beside one (below,
§17), so naming the wire file is what makes the pair well defined: a
pipeline that ran a text tree through `schema pack` and then cooked in one
step still keys on the bytes the cook actually read, and an edit upstream of
those bytes reaches the key through them.

**The build version both ADDRESSES a cooked artifact and REFUSES one**: it is
what the store is indexed by, and it is what `Open` checks out of the header.
There is no second VERSION id — the design has two in total, the protocol id
for the type wire and this one for everything cooked or blocked (§20).

The example pair is the whole rule. A huge data file naming every mesh in
the game, or every texture — that is a cook. A configuration file small
enough that the cost of loading it does not matter is the wire, and stays
the wire, and keeps the flexibility that comes with it.

- **The header build-locks it**: a magic (which is also the byte-order
  check), the unit's BUILD VERSION (§20), the lengths of the two parts
  below, and two reserved words (§7.1).

  A cooked file never crosses builds, so most of the header's shape is
  the implementation's business. **Three widths are not**, because each
  one decides something semantic:
  - **The magic is read BYTEWISE, before anything else**, since it is
    what establishes the byte order every other header field is written
    in. It is also what separates a cook from a BLOCK (§19): the two
    accelerators carry the same build version and different magics, and a
    form's identity belongs in its magic rather than in a second digest.
  - **The BUILD VERSION is 64 bits.** Under the rule below a matching id
    means `Open` checks nothing further, so the id is the sole guard
    between a runtime and a foreign region; it is sized like a digest,
    not like a version counter.
  - **Both part lengths are 64 bits.** `CookMeasure` answers in
    `int64_t`, and a 32-bit header field would reimpose at cook time the
    ceiling §3.1 just removed — on exactly the huge mesh or texture
    catalog a cook exists for.

  The build version is a compiler-settled digest over every fact a cook's
  bytes depend on: the type wire's protocol id, every record's layout as the
  compiler's own C ABI model computes it, and the facts that decide what a
  load PUTS in those slots (§20). It KEYS A FIELD BY ITS WIRE ID, not its
  source name — the identity `was` preserves (§5), because a `was` rename
  moves no byte and must not invalidate every cooked file in existence.
  Reordering two same-shaped fields does move bytes, and it moves which id
  sits at which offset, so it still refuses. Both directions are held by test.

  **ABI drift is a BUILD ERROR, not a refusal here.** The compiler computes
  the layout and both backends assert it, so a compiler that lays a record
  out differently fails to build, naming the type and the field (§20.3). That
  is what lets the id be settled from the schema alone, which is what tooling
  needs: a cook is produced before any game binary exists.

  **The id is UNIT-WIDE, so a cook outlives only what the unit does.** A mesh
  catalog is re-cooked when any table in its unit moves, not only when its
  own closure does. That is the model rather than a cost — the work is
  offline and a build cache exists to absorb it (§20.6) — and it is the price
  of two ids in the whole design instead of three.

- **A cooked file is TWO PARTS, and the header locates both.** The DATA
  comes first, at the region's alignment: it is `Lock`'s region written
  verbatim, the root at its base, and it is what the runtime points at.
  The ATTRIBUTION follows it: the node directory of §6.3, which says
  where every node starts and what type it is. **Nothing that READS the
  structure touches it** — it is written beside the data for the TOOL
  (`schema cook-check`, below), so a build that ships no
  tooling need not carry it at all: the header records its length as zero and
  the file is just data.
- **A COOK IS TRUSTED INPUT, LOADED FROM DISK, and that is the sentence every
  clause below descends from.** A cook is an artifact the owner's own pipeline
  produced for the owner's own build and put on the owner's own disk beside it;
  the game loads it. **`Open`'s checks are IDENTITY checks — is this file for
  THIS build — and not a trust boundary**: the magic, the byte order, the build
  version, the lengths and the alignment answer "did we write this?", and
  nothing answers "is this hostile?" because that is not the question a load is
  asking. **There is NO PER-NODE VALIDATION AT LOAD, ever**, and that is not a
  cost saved but the design: a per-node pass over a catalog-scale file is the
  parse the whole form exists to delete.

  **A file that did not come from your own pipeline is a TOOL's problem, not a
  loader's**: `schema cook-check` (below), run by a person, once, offline. And
  **if integrity is wanted, it is a SIGNATURE** — over the whole file, verified
  once before `Open`, with sodium — never per-node checks smuggled into the
  loader. It costs the lazy paging, and §15 carries both that and the shape
  that keeps it; not built.

  The forgery battery and the fuzzer harden the REFUSAL PATH and shape nothing
  here; §7.5 states what they hold and what they do not.

- **`Open` checks the header and points, and this is the WHOLE check**,
  because nothing else is checked at all: **the magic, the byte order it
  establishes, the build version, every RESERVED word zero, the region
  ALIGNMENT the header names, the two part lengths against the `length` the
  caller passed — a truncated file refuses — the ROOT's own storage inside the
  data part, and the alignment of the base.** That is the enumeration, and it is
  stated once: §11 and §20.6 cite it rather than repeat it.

  **Two of those clauses are there because the header's own words are DATA**,
  and a reader that took them on trust would do arithmetic with a forgery:

  - **The `alignment` word is the one field the check computes WITH** rather
    than only compares against — the data part begins at `align_up( 64,
    alignment )` and the base's alignment is measured against it — so a word
    that is not an alignment rounds nothing and aligns nothing. It must be a
    POWER OF TWO, at least EIGHT (§7.1's floor) and at most SIXTY-FOUR — the
    same sixty-four a block's base takes (§19.1), and past which the derived
    data offset would no longer be the 64 every unit this language can declare
    produces — and a multiple of the ROOT's own `alignof`, since the root sits
    at the region's base. A zero there is a division by
    zero inside the check, which is the defect the check exists to prevent.
  - **The DATA PART MUST HOLD THE ROOT.** The two part lengths frame the FILE;
    they do not say the region is at least `sizeof( root )`. Without that
    clause a forged short data part describes a root partly outside the file,
    and a match-and-point reader would hand back storage the caller never gave
    it — the one way this design could read past the length it was passed.

  On a match the bytes ARE
  what this build wrote, in this build's layout and this build's byte order,
  so there is nothing to validate and nothing to fix up: `Open` returns the
  root. That is the runtime path and there is no other. On any failure it
  returns NULL and the caller falls back to a wire load, which is the path
  that carries every version.

- **`Open` is the RUNTIME's only entry point.** There is no second one: a
  build either wrote a file or it did not, and the build version is what says
  which. A cook is an accelerator for a build's own assets, and a runtime
  that wanted to reason about a foreign one has already left the fast path.

- **THE C++ SURFACE, because a consumer written from this page needs the
  spelling and not a description of one.** One free function per root, name
  first (§6.1), taking the caller's bytes and returning the root or `NULL`:

  ```cpp
  const Scene * SceneOpen( const void * bytes, uint64_t length );
  ```

  **The length is UNSIGNED, and that is a decision the check makes rather than
  a style.** Every number `Open` compares comes out of the file, so all of its
  arithmetic is unsigned and each term is bounded before it is added; a signed
  length would put one signed value into that arithmetic and one negative case
  into every comparison. A caller holding an `int64_t` from a `stat` casts once,
  at the call site, where the sign is still its own business.

  **A ROOT IS ANY TABLE, and every table gets one.** Nothing on this page
  narrows which table a cook may be rooted at, and two things say it is any of
  them: `schema cook --root` names a TABLE and refuses anything else — a `type`
  is not a node and has no type id (§3.1) — and §11 claims the spelling on every
  closure member. So the emission is per table, in every unit that declares one,
  and a FIXED root is not a second case: its cook is one region of one node
  (below), which is this same header match and then the one record.

  **The price is bytes of header and it is measured, not assumed.** The shared
  read runtime plus one `Open` per table plus §20.3's layout asserts grew the
  corpus's fourteen Table headers by 207 KB in total — a mean of 14.8 KB, about
  a fifth of each — and the paired instrument §13.5 uses (one empty translation
  unit including one generated Table header, arms interleaved in one sitting,
  medians of 15) puts the compile-time cost at **+0.2 ms, +0.2%, inside a 4–6%
  spread**. It is comment and constant-folded arithmetic, and it does not reach
  the clock.

  **None of it is POINTER MACHINERY**, which is what keeps §2.2's question
  answerable with the form emitted everywhere: a value-only table still gets no
  arena, no builder, no reference slot and no lifecycle surface. What it gets is
  a header match, and a header match is what a cook is.

  **A REFERENCE IS DEREFERENCED THROUGH `<T>At`**, which is emitted for every
  pointer target and is the same call in a locked region and an opened cook,
  because they are the same encoding (§6.3):

  ```cpp
  const Node * next = NodeAt( node->next );   // one add; NULL when the delta is zero
  ```

  The slot is eight bytes, signed, self-relative from the slot's own address, so
  a deref needs no base pointer and no bounds test, and NULL is a delta of zero.
  **Nothing about that call is the cook's**: it is what a region reference is,
  and a cook is a region written verbatim.

  **THE SPELLING, PER BACKEND.** A consumer written from this page needs the
  names, and the three ports do not spell them alike — §11 fixes the claimed
  VERBS and leaves each language its own shape for them:

  | | C++ | C# | Go |
  |---|---|---|---|
  | open | `bool XOpen( const XCook *& cook, const void * base, int64_t bytes )` | `bool XCook.Open(out XCook cook, IntPtr p, long n)` | `bool XOpen(cook *XCook, base unsafe.Pointer, length int64) bool` |
  | the handle | the root pointer itself | `readonly struct XCook` | `type XCook struct { Region unsafe.Pointer; RegionLength int64 }` |
  | the root | the return | `cook.Root` / `cook.RootPointer` | `cook.Root() *XRow` |
  | deref | `const T * t = XAt( slot )` | `XRow* r = Schema.XAt(slot)` | `r := XAt(slot)`, `slot *int64` |
  | the record | the generated struct | `XRow` (§11's claimed suffix) | `XRow`, the same claim |
  | the descriptors | `XCook::Type()` | `XCook.Type` | `cook.Type() *TableCookInfo` |
  | §7.1's facts | `XCook::RegionAlignment` etc. | the same constants | `cook.RegionAlignment()`, `RootSize()`, `RootAlign()` — methods, which §11 leaves a language free to make them

  **THE GENERATED STRUCT IS THE COOKED RECORD**, and the backend asserts it
  rather than assuming it: every record a unit's files declare carries
  `static_assert`s on its `sizeof`, its `alignof` and every field's `offsetof`
  against the numbers the compiler folded into the build version, so a compiler
  that lays a record out differently fails to BUILD, naming the record and the
  field (§20.3).
- **THE C# SURFACE, for the same reason the C++ one is here**: a consumer
  written from this page needs the spelling, and §19.2 already sets the shape a
  C# accelerator takes. **C# has no free functions**, so the claimed verbs are
  MEMBERS of a claimed type — which is the rule §11 already gives the block
  form's accessors — and the type is `<Root>Cook`, a name §11's list has claimed
  on every closure member all along:

  ```csharp
  if ( !SceneCook.Open( out SceneCook cook, pointer, length ) )
      return;                       // wrong build, corrupt, truncated, foreign order

  SceneRow * scene = cook.RootPointer;          // the root, where it lies
  ```

  **`Open` TAKES A POINTER AND A LENGTH, and the generated source is `unsafe`** —
  §19.2's contract, for §19.2's reason: a cook is memory the tooling wrote and
  the consumer mapped, and pointing at it without a copy is the whole point. A
  `ReadOnlySpan<byte>` overload sits beside it for a consumer that already holds
  the bytes; it is the same contract in a different spelling, and its length is
  an `int`, so **the pointer form is the one with the reach the cook is built
  for** (§6.3) and a catalog past 2 GiB is opened through it.

  **THE LENGTH IS SIGNED HERE, and that is a decision and not a drift from the
  C++ rule above.** C# has no unsigned-length idiom — `Span.Length`, `Array.Length`
  and a file size all arrive signed — so an unsigned parameter would move a cast
  to every call site instead of removing one. The check converts ONCE, at the
  top, after refusing a negative, and every comparison below it is unsigned with
  each term bounded before it is added, which is the property the C++ rule was
  protecting.

  **THE MEMORY IS THE CONSUMER'S, and the contract is the block form's**: the
  region must stay put and stay aligned for as long as the handle, or anything
  reached through it, is used — an mmap, a native allocation, or an array the
  consumer pinned. A span handed back over the region has the REGION's lifetime
  and not the call's, and a `fixed` block that ends before the handle does is the
  one way to hold this wrong.

  **A REFERENCE IS DEREFERENCED THROUGH `<T>Cook.At`**, which takes the SLOT and
  not its value, because a self-relative delta means nothing without the address
  it is relative to:

  ```csharp
  ListNodeRow * next = ListNodeCook.At( &node->Next );   // one add; null when the delta is zero
  ```

  **A COOKED RECORD IS THE BLITTABLE ROW** — the same `<Name>Row` struct §19.2
  and §19.3 already define, from the same layout model, with generated padding
  fields and a `Size` that pins the trailing padding. The two accelerators SHARE
  one set of records rather than growing a second ABI, and a record the block
  form already emits is emitted there and not again. Two consequences the block
  form never had to state, because a block-form table has no pointer by
  construction:

  - **A `*T` SLOT IS A PLAIN `long`**, never a managed reference and never a
    typed pointer field: it holds the signed self-relative delta of §6.3, and
    `<T>Cook.At` is what turns one into an address.
  - **A string, a `bytes` and an array are read as SPANS over the region**,
    handed back by static accessors on `<Root>Cook` named after the field — so
    reading one copies nothing and allocates nothing, and neither accelerator
    adds a member to the other's structs. **A table claims one member per field
    on its `Cook` type**, on the same rule that claims its `Block` type's row
    accessors (§11), and a language whose accessors are members claims nothing
    at file scope for them.

  **WHERE IT IS EMITTED is §19.2's rule, for §19.2's reason**: C# has no include
  guard and one assembly sees every file, so the unit's shared cook runtime —
  the descriptors, the layout check and the constants — is emitted ONCE, into
  `<Package>Cook.cs`, and every blittable record the block form does not already
  carry is emitted there too. The unit's BUILD VERSION constant is defined by whichever
  accelerator's runtime the unit has: the block form's when it has one, the
  cook's otherwise. `Schema` is one partial class across a unit's files, so
  exactly one definition of each constant is the whole requirement.

  **THE CONSEQUENCE FOR A CONSUMER: the two accelerator files are a PAIR.**
  Sharing one set of records is what stops a second ABI, and it is also what
  makes `<Base>Block.cs` and `<Base>Cook.cs` compile together or not at all — a
  consumer that takes one without the other has records defined in a file it
  left out. Both, or neither; the zero-cost rule is unchanged, because neither
  costs anything to a consumer that takes neither, and `<Base>Table.cs` still
  carries not one symbol of either.

  **THE LAYOUT CONTRACT IS §20.3's, over the COOK CLOSURE**, and C# has no
  `static_assert`: `TableCookLayout.Verify()` runs once, before any cook opens,
  and THROWS naming the record, the field, the offset it found and the offset the
  compiler's model gives. Loud and early, but a first-use failure and not a
  compile-time one — the honest exception §19.3 already records, now covering
  every record a cooked region is laid out from and not only a block's rows.

  **ONE ABSENCE, STATED RATHER THAN HIDDEN: a table whose closure carries a
  UNION gets no C# `Open`.** §19.3 pins a blittable C# record to `Sequential`
  with generated padding, and `Sequential` cannot overlay arms — which is the
  same sentence that keeps a union out of the block form. The generated file
  says which table and why, the table's cook and its wire are untouched, and a
  C# reader for a union-bearing cook is a named follow-on (§15). C++ has no such
  absence: its cooked record is the ordinary generated struct, tagged union and
  all.

  **AND THE C# WIRE HALF IS STILL REFUSED FOR A POINTERED UNIT** (§11), which is
  not a contradiction but the point: the variable class C# lacks is the WIRE
  codec — the arena, the builder, the region, the node table — and an
  accelerator needs none of it. A cook is POINTED AT, not parsed. So a pointered
  unit's C# cooks open in full while its `Measure`, `Save` and `Load` do not
  exist, and the refusal is named in every source the unit does emit rather than
  left as a missing symbol.

- **Validating an untrusted cook is a TOOL, not a runtime surface**:
  `schema cook-check`, over the same reflection descriptors
  (§8) the runtime already carries, checking the DATA against the ATTRIBUTION.
  That is where the case lives — a file whose provenance a person doubts, or
  one a tool is diagnosing — and it is a person's decision to run it, not a
  parameter on a load. A file with no attribution part cannot be checked, so
  the command refuses it and says which part is missing. The ATTRIBUTION is
  written beside the data for exactly this reader.
- **The check is a SCAN of the attribution, not a traversal of the
  graph.** Two passes, in order:
  1. **The directory itself**, linearly and with no state: it lies inside
     the file, every type id names a table the unit has, the
     materialized offsets ascend, each is aligned for its own type, and
     each node's storage fits before the next entry. A sentinel entry
     (§6.3) refuses here — a cooked file is an accelerator and cannot
     carry a hole.
  2. **Every node, in directory order.** An entry's type id says which
     walk to run over that node; each pointer slot must resolve to an
     offset the directory NAMES, with the type the declaration requires;
     each count companion must sit inside its declared bound — including the
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
  lies. There is no node table and no graph, and the build version is the
  whole of what `Open` checks — which is the whole of what `Open` checks
  for any cook. **The generated `Open` reads one**, on exactly the terms above:
  a root is any table, and this is a root.
  **It is ONE REGION OF ONE NODE and not a second shape**, and the
  attribution part names that node like any other: sixteen bytes, the root
  at offset zero. One shape is what lets `schema cook-check` bound a fixed
  root's COUNT COMPANIONS — a `string(N)` used length, a bounded array's
  count, the companions of anything it nests by value — which are exactly as
  forgeable in one struct as in a graph, and which nothing else would check.
  A build that wants the bytes alone omits the attribution part the way any
  cook does (below): the header records its length as zero and the file is
  just data.
- **Every refusal is loud and the fallback is a real wire load**: wrong
  magic or byte order, a build version this build did not produce, a
  truncated file, a non-zero reserved word, an unaligned base. **And `schema cook-check`'s refusals are
  the tool's, not the runtime's**: an attribution part that is missing,
  leaves the file, carries a sentinel entry, names a type the unit does not
  have, does not ascend, or overlaps a node with the next; a reference that
  leaves the region, that the directory does not name, or that it names as
  another type; a misaligned reference; or a count companion outside its
  declared bound.

- **Alignment.** The header pads the data part to the region's alignment,
  so a base the allocator or `mmap` gave you is already aligned; `mmap`
  gives page alignment for free.
- **Endianness is part of the COOK, not of `Open`** (above): a matching
  build version already means a matching byte order, so `Open` never fixes
  anything up. Cooking for a foreign target is where a byte swap would live
  if one is ever wanted (§15).

Prior art gets one sentence, and it is the contrast: systems that made
pointed-at access their ONLY wire coupled access to evolution and paid
for it. Here the tolerant wire stays the format of record and the cooked
form is a build-locked accelerator beside it, produced only where load
time asks for one. The two-form split is the design.

**A COOK and a BLOCK are different accelerators and a runtime must never
accept one where the other was written.** Both are build-locked projections
of one declaration, but they lay it out differently — a cook writes the
by-value form verbatim, a block writes the projection with its rows out of
line (§2.7). **What separates them is the MAGIC**, which each form has its
own of and which is read bytewise before anything else. They share the BUILD
VERSION, because a build version answers "which build?" and not "which
form?", and the projection carries both forms' facts (§20.2): a block's
`slot` lines sit beside its record's `field` lines, so an edit to either
moves the one id.

**And prior art gets one MEASURED sentence, from the case §19 exists for.**
The render data this document's second gate is held to (§12) was built with
flatbuffers once and the build was abandoned — in the owner's words, *"I
used to use flatbuffers to build render data, but it was too slow because it
was not parallizable."* That is the specific failure the block form is
shaped against: a per-frame producer that has to go wide cannot afford a
builder with a serialization point in it, whatever the read side costs. A
cook does not answer it either — a cook is produced from a builder by a
single-threaded `Lock` (§6.2), which is exactly the shape that lost.

### 7.1 The file, byte for byte

A cooked file is a HEADER, a DATA part and an ATTRIBUTION part, in that
order. Every word of the header is a `u64` written in the byte order the
cook was produced in, and the header is 64 bytes:

| at | word | what it is |
|---|---|---|
| 0 | `magic` | `0x4B4F4F434D484353` — read BYTEWISE, before anything else |
| 8 | `build_version` | the unit's id (§20) |
| 16 | `byte_order` | `1` little, `2` big — the order that WROTE the file |
| 24 | `data_length` | the region's bytes |
| 32 | `attribution_length` | the directory's bytes, or `0` |
| 40 | `alignment` | the region's alignment |
| 48 | `reserved` | zero |
| 56 | `reserved` | zero |

- **The MAGIC's value is `0x4B4F4F434D484353`**, which is `SCHMCOOK` read as
  ASCII in the byte order a little-endian store produces — the same shape
  §19.1's `SCHMABLK` takes, so a hex dump of a little-endian cook is legible
  and the two accelerators sit in one vocabulary. It is stored in the
  PRODUCER's order: a consumer reads back this build's constant, or that
  constant byte-reversed, which identifies a cook of the other order, or
  something that is not a cook. **A consumer written from this page needs the
  constant, not a description of one.**
- **The `byte_order` word does the OTHER job**, exactly as a block's does
  (§19.1): the magic is what REFUSES a foreign order and this word is what
  RECORDS which order wrote it, so a refusal names the order rather than
  inferring it and a tool dumping a cook reads the fact. A file whose magic
  matched and whose order word did not is corrupt, and there is no reading
  that recovers it. The BUILD VERSION cannot do either job: §20.1 digests
  `byteorder` as a GENERATION input, `little` for every target schema
  generates for today, so two builds of one schema for two orders emit the
  same id.
- **`alignment` is the REGION's alignment**: the greatest `alignof` of any
  record in it, never below EIGHT. The floor is what puts the attribution
  part on an eight-byte boundary without a second padding rule, since a
  region of byte-only records would otherwise align at one.
- **The DATA part begins at `align_up( 64, alignment )`**, which is 64 for
  every unit this language can declare — the largest alignment it has is 16.
  It is DERIVED and not a header field, because a fact a reader computes is a
  fact two writers cannot disagree about, and because `Open` must stay O(1)
  whatever it is. A mapped file's page alignment covers the base for free.
- **`data_length` is ROUNDED UP to `alignment`**, so the attribution part
  begins at `data_offset + data_length` and needs no rule of its own. The
  bytes between the last node and that rounding are zero, like every other
  byte no field covers (§7.2).
- **`attribution_length` is `16 × nodes`, or zero.** The directory carries no
  count of its own: the node count is the length divided by sixteen, and a
  length that is not a whole number of entries refuses. A cook that carries
  data alone records zero here, and `schema cook-check` refuses it saying
  which part is missing.
- **The whole file is `data_offset + data_length + attribution_length`**, and
  a `size` that is not exactly that refuses — a truncated file and a file with
  trailing bytes are the same refusal.
- **The two RESERVED words are reserved**: a non-zero one means a writer used
  a form this build does not understand, and `Open` refuses rather than
  ignoring it.

**The ATTRIBUTION part is the node directory of §6.3 and nothing else**: one
entry per numbered node, in index order, each `offset (u64), type id (u64)`,
position `i` describing node index `i + 1`, so position `0` is the root at
offset `0`. A node's extent runs to the next entry's offset, and the LAST
node's runs to `data_length`. The parts are SEPARABLE: a tool may write the
attribution beside the cook rather than inside it, and rejoining is the
length word and a concatenation, because a split moves nothing else.

### 7.2 The region, byte for byte

The data part is `Lock`'s region written verbatim (§6.3). **Nodes are laid
out in the DEPTH-FIRST PRE-ORDER §3.1 numbers them in**, the root at offset
`0`, each starting at `align_up( offset, alignof )` for its own type, with
zero slack between them.

**A record's layout is §20.3's model and no other**: each field at its own
alignment, the record's alignment the greatest of its fields', the size
rounded up to it. The compiler computes it, both backends assert it, and a
cook's bytes come from the same computation as the build version's `record`
lines — a second walk here would be a second ABI.

**A field is laid out as its STORAGE PIECES, each aligned on its own**, which
is what a generated record declares and what a port that padded only BETWEEN
fields would get wrong:

| declaration | pieces, in order |
|---|---|
| a scalar, an enum, a `flags` | the value at its storage width |
| `string(N)` | `char[N + 1]`, then `int32` used length |
| `bytes(N)` | `uint8[N]`, then `int32` used length |
| `[N]T` | `N` elements at the element's `sizeof` |
| `[..N]T` | `N` elements, then `int32` used count |
| `[E]T` | `E.Max` elements, one per named variant, nothing for `None` |
| `*T` | `int64` self-relative delta, eight bytes at eight |
| `?T` | the value's own pieces, then `bool` present |

- **An ENUM slot holds the ORDINAL**, at the enum's own derived storage width
  (SPEC.md §4.2) — not the wire's variant-name hash. What group 3 of the build
  version captures is what a slot HOLDS (§20.1), and the two vocabularies meet
  in the enum's own values.
- **An ENUM-KEYED array's slots are POSITIONAL in a region**, at the same
  SHIFTED positions the storage has (§2.4): index `v − 1` is variant `v`'s,
  the extent is `E.Max`, and nothing is stored for `None`. A region is a
  memory image of the storage type, so it can be nothing else. The wire
  rides the same array BY NAME (§3.2); a region rides it by position, and
  the cook is where the two are reconciled.
- **A UNION is a TAG beside its arms**: the tag at the union's own base at its
  storage width, the arms overlaid at `align_up( tag width, greatest arm
  alignment )`, the whole rounded to the union's alignment. **Only the SET
  arm's bytes are written**; every other byte of the extent is zero, which is
  the arm-zeroing shape §13.2 already pins, taken to a region.
- **A COUNTED array writes all `N` slots.** The storage is allocate-max, and a
  slot past the live count holds the VALUE-INITIALIZED element — zero for a
  scalar, the element type's own declared defaults for a record. That is what
  `T x[N] = {}` produces and what a wire load leaves there (§6.1), so a cook
  of a loaded wire and a cook of a built structure agree.
- **A `*T` SLOT IS EIGHT BYTES AT EIGHT**, holding the signed self-relative
  delta of §6.3, and **NULL IS ZERO**. It is as wide as the offsets it is the
  difference of, so a region of any size expresses every reference it holds and
  a cook has no reach to refuse.

**EVERY BYTE NO FIELD COVERS IS ZERO** — interior padding, a record's trailing
padding, a string's or `bytes`' unused tail, the bytes of a union outside its
set arm, and the slack between the last node and the rounded `data_length`.
It is not tidiness: a cooked artifact is CONTENT-ADDRESSED by (asset hash,
build version) (§7), so two cooks of one wire have to be one artifact, and one
uninitialized pad byte would make them two.

### 7.3 A cook, worked to the byte

Every number below derives from a rule on this page; none of it is declared.

```
package demo

table Palette { id int32 }
table Node    { value int32  next *Node }
table Scene   { name string(4)  head *Node  palette *Palette }
```

with `scene.name = "hi"`, `scene.head = A`, `A.next = B`, `A.value = 1`,
`B.value = 2`, and `scene.palette = P` with `P.id = 7`.

- **The layouts** are §20.3's: `Palette` is `sizeof=4 alignof=4`; `Node` is
  `value` at 0 and `next` — an eight-byte slot, so eight-aligned — at 8,
  `sizeof=16 alignof=8`; `Scene` is `name`'s `char[5]` at 0 and its `int32`
  length at 8 — the buffer aligns at one and the length at four — then `head`
  at 16 and `palette` at 24, `sizeof=32 alignof=8`. `schema build-version
  --facts` prints those same numbers, and the id over them is
  `0x4efe97313c704bb5`.
- **The numbering** is §3.1's walk: the root is 1; `head` reaches A, so A is
  2; descending A, `next` reaches B, so B is 3; then `palette` reaches P, so
  P is 4.
- **The offsets** follow: `Scene` at 0, A at 32, B at 48, P at 64. The
  region's extent is 68, its alignment `max( 8, 8 ) = 8`, and 68 rounds to 72.
- **The deltas** are self-relative: `head`'s slot is at 16 and A is at 32, so
  it holds 16; `palette`'s slot is at 24 and P is at 64, so it holds 40;
  `A.next`'s slot is at 40 and B is at 48, so it holds 8; `B.next` is null, so
  it holds 0.
- **The directory** is four entries: `(0, Scene)`, `(32, Node)`, `(48, Node)`,
  `(64, Palette)`, and the type ids are `fnv1a64` over the table's name (§3.1).
- **The file** is `64 + 72 + 64 = 200` bytes.

```
0000  53 43 48 4d 43 4f 4f 4b   magic "SCHMCOOK"
0008  b5 4b 70 3c 31 97 fe 4e   build_version 0x4efe97313c704bb5
0010  01 00 00 00 00 00 00 00   byte_order = 1 (little)
0018  48 00 00 00 00 00 00 00   data_length = 72
0020  40 00 00 00 00 00 00 00   attribution_length = 64
0028  08 00 00 00 00 00 00 00   alignment = 8
0030  00 00 00 00 00 00 00 00   reserved
0038  00 00 00 00 00 00 00 00   reserved

0040  68 69 00 00 00            Scene.name  char[5] = "hi", zero tail
0045  00 00 00                  padding to the length's alignment
0048  02 00 00 00               Scene.name  used length = 2
004c  00 00 00 00               padding to the reference slot's alignment
0050  10 00 00 00 00 00 00 00   Scene.head     delta +16 -> node 2 at 32
0058  28 00 00 00 00 00 00 00   Scene.palette  delta +40 -> node 4 at 64
0060  01 00 00 00               A.value = 1
0064  00 00 00 00               padding to the reference slot's alignment
0068  08 00 00 00 00 00 00 00   A.next  delta +8 -> node 3 at 48
0070  02 00 00 00               B.value = 2
0074  00 00 00 00               padding
0078  00 00 00 00 00 00 00 00   B.next  = 0, and zero is null
0080  07 00 00 00               P.id = 7
0084  00 00 00 00               the region ends at 68 and rounds to 72

0088  00 00 00 00 00 00 00 00   node 1: offset 0
0090  13 f2 b5 3a 62 31 9a 4a   node 1: type id, Scene
0098  20 00 00 00 00 00 00 00   node 2: offset 32
00a0  8d b6 f6 d2 c6 1c bd 66   node 2: type id, Node
00a8  30 00 00 00 00 00 00 00   node 3: offset 48
00b0  8d b6 f6 d2 c6 1c bd 66   node 3: type id, Node
00b8  40 00 00 00 00 00 00 00   node 4: offset 64
00c0  e0 ad 84 20 6e 53 af c8   node 4: type id, Palette
```

**The BIG-ENDIAN cook of the same structure is the same 200 bytes with every
scalar reversed in place** — the header's words, the region's lengths, counts,
deltas and values, and the directory's pairs — and nothing else moves: no
offset, no size, no node order, and the same directory read in its own order.
The magic's first eight bytes then read `4b 4f 4f 43 4d 48 43 53`, which is
what tells a little-endian consumer it is holding a foreign file.

### 7.4 What the check reads, and what it does not

§7's two passes are stated above; this is what each one touches.

**Pass one, the directory, needs no field of the region at all.** It refuses a
sentinel entry, a type id no table has, a first entry that is not the root at
offset zero, an offset below the previous node's end, an offset not aligned
for its own type, and a node whose `sizeof` does not fit before the next entry
or before `data_length`. Because every entry is then known aligned and inside
the region, PASS TWO NEEDS NO BOUNDS CHECK OF ITS OWN for a reference that
lands on a directory entry — which is the economy §6.3 buys by making the
directory's offsets the padded starts.

**Pass two reads exactly three things**, and it decodes no payload and follows
no reference:

1. **Every `*T` slot.** A delta of zero is null. Any other resolves to
   `slot + delta`, which must be an offset the directory NAMES — one binary
   search, which is the `P log N` — and the entry's type id must be the one
   the declaration requires.
2. **Every COUNT COMPANION**, against its declared bound: a `string(N)` or
   `bytes(N)` used length, and a bounded array's used count. A NEGATIVE one
   refuses too, because an extent is never negative and a walker handed one
   indexes backwards out of the region. The companions of fixed-size tables
   and plain types NESTED BY VALUE are in scope, because they bound a walker
   just as a table's do.
3. **Every UNION TAG.** It is the one field VALUE the scan reads, and it is
   read because it is a DISCRIMINANT rather than a payload: a scan that
   skipped it would either check no arm — leaving every reference and every
   companion inside one unchecked — or check bytes no runtime will ever read
   as an arm. It is bounds-checked against the union's arm count for the same
   reason a companion is: a tag past the last arm steers a walker into storage
   no declaration describes.

**Nothing else is read.** Not a scalar, not a string's bytes, not an enum's
ordinal, not a `flags` mask — none of them can steer a walker, so none of them
is the check's business, and a value outside a declared RANGE is not a
forgery: §4 clamps a range on the wire, and a cook of a build's own data has
already been through that.

**A cook this build wrote always passes**, which is what makes the check
usable as a gate rather than only as a diagnostic: the writer refuses a
partial region and a delta it cannot express, so the file it produces satisfies
every clause above by construction.

### 7.5 Held by test

- **THE ROUND TRIP, and it is the gate**: `wire → cook → wire`,
  byte-identical, in BOTH byte orders, over every pointered graph in the
  corpus — aliasing, a back-reference, a chain, a tree, a variable table
  nested by value, a bounded array of them, an enum-keyed array of them, an
  optional, and a null in every pointer-shaped slot — and over every pinned
  `schema pack` tree for the FIXED class. The wire is the format of record, so
  a fact the accelerator cannot give back is a fact it lost.
- **The BUILD VERSION stamped in the header equals `schema build-version`**,
  and a cook opened against any other id refuses. There is no second version
  id (§20).
- **A BIG-ENDIAN cook byte-swaps every scalar**, proved three ways rather than
  by the writer's own claim: the two files are the same length and differ; each
  header word is the other's byte reversal, except the `byte_order` word, whose
  VALUE differs; and a named scalar field, at the offset the compiler's layout
  model puts it, reads `03 00 00 00` in one and `00 00 00 03` in the other.
  The big-endian file then opens, and its directory is entry-for-entry the
  little-endian one's.
- **A HOSTILE BATTERY over `cook-check`**, on §19.5's shape: valid cooks in
  both orders, mutated by seeded byte flips, by boundary-value overwrites at
  every `u32` and `u64` the format has, and by one DIRECTED edit per refusal
  this section names — a forged magic, each reserved word, a foreign build
  version, an order word the magic contradicts, truncation, extension, each
  part length, a non-power-of-two alignment, the sentinel, an unknown type id,
  a directory that does not ascend, a root off the base, an overlap, a node
  past the region, a misaligned entry, a reference that names no entry, one
  that leaves the region, one the directory names as another type, and each
  count companion past its bound and below zero. The bar is **REFUSE, OR
  ACCEPT AND BE WHOLE**: an accepted file must uncook and re-cook into a file
  that checks, and NOTHING MAY PANIC — an out-of-range read inside the check is
  the failure it exists to prevent.
- **Its NEGATIVE CONTROL**: pass one, the directory scan, removed from
  `cook-check` through a build overlay, and the battery must go red. A checker
  whose battery has never gone red is watching nothing.
- **THE READING TIER's cook pair** (§19.5's twin, on this form): every field of
  every node of a real cook is read through the generated ACCESSORS and through
  the DESCRIPTORS and compared, with a pointer compared as its RAW DELTA and as
  its SLOT's own offset before any resolution — and the fuzzer's oracle over
  `Open` requires refuse-or-be-whole with no exception escaping the reader.
- **THE SCALE FIXTURES**: a synthetic region generator writes a 1 MB, a 100 MB
  and a 1 GB cook, streaming, so the open-cost gate — open time FLAT across the
  three — has inputs a person regenerates rather than a gigabyte in the tree.
  CI runs the first two under the two-minute rule; the gigabyte is run by hand.
  **The gate itself belongs to the RUNTIME and not to this tool**: `Open` is
  what has to be O(1), and a tool that walks a directory is measuring its own
  scan. It is held over the C++ `Open`, below.

- **THE CROSS-IMPLEMENTATION LOCK, and it is what makes THREE implementations
  of one page worth having.** The tool writes a cook in Go, the C++ `Open`
  points at it and the C# `Open` points at the very same bytes, and none of the
  three was written from either of the others. (A FOURTH reader now points at
  the same bytes: the JavaScript one, whose canonical node dump the conformance
  harness byte-compares against the pinned C++ walk over all six of its
  fixtures. It carries the DUMP half of this lock and not the directory half —
  a reading-tier backend gets its fixtures from the harness, which holds the
  dump and not the attribution part.) The lock is the ATTRIBUTION part:
  every node a reader reaches by following its OWN derefs, through its own
  record layouts, must be a node the directory names, at that offset, with that
  type id — and the two SETS must be equal, so an edge the reader stops
  following is as loud as one it invents. A record laid out one byte differently
  on either side lands a deref off a directory entry and the gate says which
  node and which type. It runs over SEVEN roots, which is the shapes a region
  has: a pointer chain, a tree, a keyed array of variable tables beside an
  optional, a cross-file graph through a by-value variable table, a chain node
  as its own root, and TWO FIXED ROOTS — one region of one node — of which one
  is pointed at and one is declared in a file with no variable table of its own.
  Each reader holds it independently, over the same fixtures.

  Beside it, and from the same walk: **every byte no field covers is ZERO**
  (§7.2), checked over the slack the directory frames; and every COUNT
  COMPANION inside its declared bound, which is pass two's own reading (§7.4)
  done by the other implementation.

  **AND THE DUMP, which is what the directory lock cannot do.** The lock proves
  the readers agree on WHERE every node is and WHAT it is; it says nothing about
  the bytes inside one, and a record laid out one byte differently INSIDE a node
  moves no node offset and no directory entry. So both readers write their walk
  out as CANONICAL TEXT — one line per leaf, with a dotted path and the value
  read at that offset, a reference as its target offset, a string as its used
  bytes with the tail dropped, a counted array with its companion, an optional
  with its presence — and the two files are BYTE-COMPARED, over all seven roots.
  It is the value crossing the variable class could not otherwise have, and it
  costs one mode in each harness. A FLOAT has no canonical cross-language
  spelling this gate is willing to fix in passing, so meeting one is a failure
  rather than a drift.

- **THE VALUE CROSSING, and the FIXED class is where it lives today.** A fixed
  table has no pointer, so it has no node table and no kind `17`, so this
  backend's wire and the tool's are the SAME BYTES for one. The gate rides that:
  the C++ side writes a known instance to the wire, `schema cook` cooks it —
  reading that wire with its own model of the record's layout — and the C++
  side opens the cook and reads the fields back. Value for value, including a
  string's used length and the zero tail past it (§7.2). The tool's own read
  report over a wire this backend wrote is SILENT, which is the same crossing in
  the other direction and comes free.

  **For the VARIABLE class it does not cross yet**, and the reason is §3.1's
  backend status rather than anything about the cook: the tool writes the flat
  node table and no backend does, so a tool-written wire's pointer fields reach
  a generated reader as a kind it cannot skip and the decode stops. It lands
  with that emitter work. What stands in for it meanwhile is that a value is its
  bytes at its offset, and the directory lock pins the offsets while §20.3's
  asserts pin the layout they come from.

- **THE C++ `Open`'s OWN GATES**, on §19.5's shape and with §7's oracle rather
  than §19's:

  - **THE DIRECTED BATTERY**: one forgery per fact the enumeration names — each
    byte of the magic, the magic byte-reversed (a cook of the other order), the
    BLOCK form's magic, every byte-order word the magic contradicts, three
    build versions, both reserved words, thirteen alignment words that are not
    alignments, both part lengths long and short and saturated, a data part too
    short to hold the root with the file's total kept exact, six truncations
    and an extension, and every unaligned base — each refused, under ASan and
    UBSan with `-fno-sanitize-recover=all`, over a buffer allocated at EXACTLY
    the length claimed so a redzone sits on the next byte.
  - **AND ONE PER FACT THE ENUMERATION DOES NOT NAME**, each of which must
    OPEN: a reference slot with an enormous forward delta, a negative delta
    past the base, a directory entry naming an offset outside the region.
    **Those are `cook-check`'s refusals and not the runtime's** (§7.4), and a
    battery that expected `Open` to catch them would be holding this code to a
    design this page does not have. A cook is trusted input loaded from disk
    (§7): the battery asserting that these OPEN is that ruling written as a
    test, so a walk cannot be put back without the gate saying so.

  **WHAT THE BATTERY AND THE FUZZER ARE FOR, stated because it is easy to read
  them as a threat model and they are not one.** They harden the REFUSAL PATH.
  `Open` runs on whatever bytes a disk hands back — including a corrupt one, a
  truncated one, a file from a build that moved — and what these hold is that
  refusing is CLEAN: no crash, no read past the `length` the caller passed, no
  undefined behaviour inside the check. They do not shape the runtime, and they
  ask `Open` to validate nothing.
  - **THE FUZZER**, seeded, with the oracle §7 actually promises. A mutation
    inside the HEADER must be refused, or open onto a data part that is still
    this build's own bytes — and then the whole graph must agree with the
    directory, exactly as the lock above requires. A mutation inside the DATA
    PART must not change `Open`'s answer AT ALL, which is the O(1) promise
    written as a property a fuzzer can falsify rather than as a timing. On
    every path, opened or refused, nothing outside the length the caller passed
    is read; and an opened cook's ROOT STORAGE is read whole, so the sanitizer
    proves it lies inside that length rather than the oracle computing that it
    does.
  - **ITS TWO NEGATIVE CONTROLS**, each removing ONE clause of the check
    through a build overlay on the emitter and requiring the battery to go RED:
    the part-length equation, which is what refuses a truncated file, and the
    root-fits clause, which is what refuses a data part too short to hold the
    root. A battery that has never gone red is watching nothing.
  - **THE O(1) GATE**: the 1 MB and the 100 MB fixture, opened the same number
    of times, paired in one sitting, reported as medians — the bar is that the
    two are the same time. A walk of any shape over the larger one would be
    orders of magnitude out, so the band cannot pass one by accident. The
    gigabyte arm is run by hand.
  - **THE BYTE-ORDER LEG**: on the big-endian target, a cook written
    `--byte-order big` opens NATIVELY — header, deltas and the whole graph
    walk, with no fix-up pass anywhere — and a little-endian cook is refused by
    the MAGIC; on this host, the mirror image.
  - **THE ZERO-COST HALF**: a value-only unit emits no `Open`, no build-version
    constant and no cook runtime, and its Table sources stay byte-identical to
    the pins (§2.2).
  - **THE DOCUMENTED SURFACE COMPILES**: USAGE's cook example is a translation
    unit in the gate and it runs against a real cook, so the day the surface
    moves the documentation goes red with the code rather than a release later.
  - **AND `cl /W4 /WX` COMPILES IT**: the msvc leg generates the pointered unit
    and compiles its header, so the cook runtime meets the estate's hard
    requirement on every pull request (SPEC.md's Visual C++ rule).

- **THE C# `Open`'s OWN GATES**, which are the C++ leg's list held by a runtime
  that has neither a sanitizer nor a `static_assert`. What each one gives up and
  what stands in for it is stated rather than implied, because a gate whose
  instrument is weaker and whose wording is not is the kind of green nobody
  should trust.

  - **THE DIRECTED BATTERY, the same one**: one forgery per fact the
    enumeration names, each refused; and one per fact it does not — an enormous
    forward delta, a negative delta past the base, a directory entry outside the
    region — each of which must OPEN, because those are `cook-check`'s refusals
    and not the runtime's (§7.4). It runs over the pointered roots and over both
    FIXED roots, whose regions are too small to carry the reference forgeries and
    which therefore run two fewer.
  - **ITS INSTRUMENT IS A GUARD PAGE, and its granularity is stated.** C# has
    no ASan, so the buffer is placed in an `mmap`'d region with a `PROT_NONE`
    page immediately after it: a read past the end faults, and the site line the
    harness flushes before every attempt is what names the forgery, exactly as
    the C++ leg's death callback does. A guard page is PAGE-granular, so placing
    the buffer's last byte against it and placing its base at a chosen ALIGNMENT
    are one knob; the harness spends it on the alignment and takes the slack that
    leaves — at most `alignment - 1` bytes, and EXACTLY ZERO for a valid cook,
    whose total is the header plus a data part rounded to `alignment` plus a
    whole number of sixteen-byte entries. **The byte-exact proof is the C++
    leg's**, and this one is what a runtime without a sanitizer can hold.
  - **THE FUZZER**, seeded, with the same oracle: a mutation inside the HEADER
    is refused or opens onto a data part that still agrees with the directory; a
    mutation inside the DATA PART does not change the answer at all, which is
    the O(1) promise as a property rather than a timing; and an opened cook's
    ROOT STORAGE is read whole. Its N is set by MEASUREMENT under the budget
    rather than inherited: this leg runs without a sanitizer under it, so it
    buys about ten times the shared count in the same time.
  - **ITS TWO NEGATIVE CONTROLS**, the C++ leg's two, for the same reason —
    they are the two clauses that decide whether `Open` can hand back storage the
    caller never gave it. Each removes ONE from the EMITTER through a build
    overlay, regenerates the unit and requires the battery to go RED: the
    part-length equation, which is what refuses a truncated file, and the
    root-fits clause, which is what refuses a data part too short to hold the
    root. A sabotage must leave every local IN USE, because generated C# with an
    unused local does not compile under `TreatWarningsAsErrors` and a control
    that fails to BUILD is not a control that went red.
  - **THE O(1) GATE**, the same paired medians over the same two fixtures. A
    tiered runtime is warmed before it is measured, because a first pass measures
    tier-up and not codegen.
  - **THE BYTE-ORDER LEG IS HALF A LEG HERE, and the page says so.** A cook
    written `--byte-order big` is REFUSED by the magic, read bytewise, which is
    the half this leg can hold. The other half — a big-endian consumer opening a
    big-endian cook NATIVELY — is **UNPROVEN in C# and stays unproven until a
    big-endian .NET exists**; there is no such runtime to run it on. The C++ leg
    proves that half on s390x, and nothing about a C# consumer's big-endian
    behaviour may be inferred from it.
  - **THE LAYOUT CONTRACT AT START-UP**: `TableCookLayout.Verify()` run as its
    own mode, before any cook is opened, so §20.3's C# half is a gate on every
    run rather than a throw the first time somebody opens a cook in a game.
  - **THE ZERO-COST HALF, unchanged and re-measured**: the C# `<Base>Table.cs`
    sources stay BYTE-IDENTICAL to their pins with the cook's read side emitted,
    and none of them carries the build version — it is their accelerator file's
    (§2.2, §20.7).
  - **THE WHOLE-ASSEMBLY COMPILE**: every corpus unit's C# — the cook sources
    included, the pointered unit's among them — compiled into one assembly with
    `TreatWarningsAsErrors`. Compiling IS half the gate: C# has one namespace
    across a unit's files, so a blittable record emitted twice, a record emitted
    into a file that gets no Cook source, or a shared constant defined by both
    accelerators produces a unit that does not compile at all, and this is where
    that is caught.
  - **THE DOCUMENTED SURFACE RUNS**: USAGE's C# cook example is a mode of the
    gate and it runs against a real cook, so the day the surface moves the
    documentation goes red with the code rather than a release later.

### 7.6 The write side: `Cook` and `CookMeasure`

**A BUILD CAN WRITE A COOK, AND THE BYTES ARE THE TOOL'S.** `schema cook` is
the reference and stays it: a generated writer is held to the tool's output
BYTE FOR BYTE, in both byte orders, over every instance the conformance harness
carries. That is the whole contract, and it is not a courtesy — a cooked
artifact is CONTENT-ADDRESSED by (asset hash, build version) (§7), so two
writers of one instance must produce ONE artifact or the pair addresses
nothing.

**Why a runtime writes one at all**: tooling is written in whatever language
the tool is written in, and a game's runtime in another. An editor, an importer
or a cook farm that already holds the structure should not have to shell out to
the compiler to lay it out — *"game developers could write tools in any
language and a runtime in any other."*

**THE C++ SURFACE**, name first (§6.1), one pair per table beside `<Root>Open`
— a FIXED table's takes the value:

```cpp
int64_t SettingsCookMeasure( const Settings & value );
bool    SettingsCook( const Settings & value, void * out, uint64_t capacity, TableByteOrder order );
```

- **IT IS A MEASURE/WRITE SPLIT, exactly as the wire's is** (§6.1).
  `CookMeasure` answers the whole file's length — the header, the data part and
  the attribution part — in `int64_t`, because a cook's part lengths are 64
  bits and the scale this form exists for is a catalog. `Cook` writes exactly
  that many bytes into the caller's buffer, or writes nothing and returns
  `false` when the capacity is short. **THE CALLER OWNS THE BUFFER AND THE
  WRITER ALLOCATES NOTHING TOWARD IT**, which is the rule the generated codecs
  already live under; a fixed root's writer allocates nothing at all, and a
  pointered root's allocates its numbering through the caller's pair (below).
  Both are measured rather than claimed.
- **THE BYTE ORDER IS A PARAMETER, AND IT IS THE TARGET'S RATHER THAN THE
  HOST'S.** The order is settled at cook time for the build the file is for
  (§7), so passing `TableByteOrder::Big` on a little-endian machine produces a
  big-endian build's file and nothing about the writing host reaches the bytes.
  It is the same choice `schema cook --byte-order` makes, in the runtime.
- **A RECORD IS WRITTEN PIECE BY PIECE AND NEVER MEMCPY'D**, for two reasons
  that are both the format's. A swap has to know where every scalar begins; and
  EVERY BYTE NO FIELD COVERS IS ZERO (§7.2), while a live struct's padding, a
  string's tail and the bytes of a union outside its set arm carry whatever the
  program left there. The extent is zeroed once and each field's storage pieces
  are written at the offsets §20.3's model gives — one zeroing rather than a
  rule per padding site, and the same model the region's bytes, the
  `static_assert`s and the build version all come from.
- **WHAT THE STORAGE HOLDS IS WHAT IS WRITTEN.** A counted array writes all `N`
  slots (§7.2), so a slot past the live count carries what the storage carries:
  for a value a wire load or `Reset` produced, that is the value-initialized
  element the page names, which is why a cook of a loaded wire and a cook of a
  built structure agree.
- **THE ATTRIBUTION PART IS WRITTEN.** One entry per node in index order —
  `offset (u64), type id (u64)`, the root first at offset zero, a type id the
  `fnv1a64` of the table's name (§3.1, §6.3) — so what comes out is a file
  `schema cook-check` can check. A fixed root's is that one entry. A cook that
  carries DATA ALONE is the tool's `--attribution` option and a named
  follow-on here (§15), not a parameter on this call.
- **IT IS WRITE-COLD, AND THE GENERATED CODE SAYS SO**: the writer is ordinary
  `inline` rather than the force-inlined shape the wire codecs take (§9),
  because a cook is produced offline once per (asset, build version) and read
  every time a build starts. The performance ladder puts the two halves in
  different places and the emitter follows it.

**A FIXED TABLE'S COOK IS ONE REGION OF ONE NODE.** A fixed table is one
struct (§6.1), so its cook is the header, the record and one directory entry,
and `CookMeasure` is a constant the compiler folded from the layout — it does
not depend on the value.

**A POINTERED ROOT'S COOK IS THE REGION OF §7.2**, every reachable node once,
and the surface takes the two forms the wire's own entries take (§6.2) — a
region root, or a builder — never a value, because a pointered root is a region
and a root pointer rather than a struct you copy:

```cpp
int64_t SceneCookMeasure( const Scene * root, TableAllocator allocator = TableDefaultAllocator() );
bool    SceneCook( const Scene * root, void * out, uint64_t capacity, TableByteOrder order,
                   TableAllocator allocator = TableDefaultAllocator() );
int64_t SceneCookMeasure( const SceneBuilder & builder );
bool    SceneCook( const SceneBuilder & builder, void * out, uint64_t capacity, TableByteOrder order );
```

- **`CookMeasure` IS VALUE-DEPENDENT HERE, because the answer is the
  numbering.** `CookMeasure` and `Cook` each derive it from the graph — the
  depth-first first-visit walk over pointer edges of §3.1, carrying the
  identity map of §6.2 — and neither carries the other's, the rule the wire's
  measure and save already live under. A node named from three places is one
  node in the answer, and a reference to an entry whose descent is still open
  is the cycle refusal, here as at save and at `Lock`: the measure answers `-1`,
  the write answers `false`, and nothing partial is written.
- **THE LAYOUT IS THE TOOL'S (§7.2).** The root at zero; each numbered node at
  `align_up( offset, alignof )` for its OWN type, in index order, with zero
  slack; the data length rounded to the region's alignment, which is the
  greatest of the nodes' and never below eight; a `*T` slot holding the signed
  self-relative delta from the slot's own address to the node's start, and
  zero for null. The nodes are the numbering's entries, so the directory is
  filled from the same pass that placed them.
- **A REFERENCE THE NUMBERING DOES NOT CARRY IS A REFUSAL.** The walk visits
  what a writer writes (§3.1) — it does not descend a counted array's slot past
  the count, an absent optional's value or an unset union arm — while the record
  writer writes the whole storage (§7.2). A non-null slot in storage the walk
  did not reach names a node the region will not hold, and `Cook` returns
  `false` rather than writing a partial region, which is what the tool does with
  the same input. A region `Load` produced holds null in every such slot, so a
  cook of a loaded wire never meets the case; a built structure that does has a
  pointer in storage its own count says is dead.
- **WHAT ALLOCATES, AND THROUGH WHAT.** The caller still owns the OUTPUT
  buffer and nothing is allocated toward it. What the pointered writer
  allocates is the NUMBERING — the identity map, the entry array, and one
  region offset per node — proportional to nodes, never to bytes, and released
  before the call returns; it is the same allocation the wire's `Measure` and
  `Save` over a region make (§6.5, §13.9). Every byte of it goes through the
  `TableAllocator` the call is handed: the builder's own for the builder
  overloads, the optional last argument for the region overloads, defaulting
  to the hook pair. Nothing on the path reaches the C library directly, and
  that is measured rather than claimed (below).

**Held by test**:

- **THE HARNESS SURFACE, and it is the gate**: `cook-write` — a language writes
  the cook from an instance's WIRE and the harness byte-compares the result
  against `schema cook`'s file, in BOTH byte orders, over EVERY instance the
  corpus carries, the four variable ones included: a tree, a graph whose
  shared node is named three times and whose tree closes a diamond, an empty
  root, and a chain of 260 nodes. Every leg that has no writer prints ABSENT,
  which is the distinction the matrix exists for (test/conformance/README.md).
- **THE TWO FIXED FIXTURES, against the tool's own files**: the same
  `Settings` and `Stamp` cooks §7.5's value crossing already reads, written by
  the runtime this time and compared byte for byte — and then OPENED by
  `<Root>Open`, which is the writer and the reader of one implementation
  meeting over the tool's format.
- **THE POINTERED FIXTURE, from THREE sources to ONE file**: a `Scene` built in
  a builder — a list node named from `head`, from `alias` and from a by-value
  layer, a tree, a pointed-at fixed `Settings`, and a counted array of layers
  with heads of their own — is saved to the wire by this runtime and cooked by
  the tool in both orders. The runtime then cooks the same graph from the
  UNLOCKED builder (the arena encoding), from the LOCKED region, and from a
  region `Load` produced out of that wire, and each of the three is byte for
  byte the tool's file. `<Root>Open` then opens what was written and walks the
  chain through `<T>At`.
- **ZERO ALLOCATION FOR THE FIXED CLASS, MEASURED**: a counting `operator new`
  around the measure and both writes, requiring not one allocation, with a
  negative control that puts one inside the measured region and requires the
  gate to go red.
- **EVERY POINTERED ALLOCATION THROUGH THE PAIR, MEASURED**: the pointered
  fixture is cooked under a counting `TableAllocator`, whose allocations must
  match its frees and whose count is reported, while the same `operator new`
  counter stays at zero; and the hooks unit (§13.9) cooks a builder's graph with
  the DEFAULT pair defined to a separate counter, which must read zero — the
  place a writer that ignored the pair it was handed would land. Its negative
  control makes the builder overload cook through the default pair instead of
  the builder's own, through a `go build -overlay`, regenerates the corpus, and
  requires the hooks unit to go red. A raw `calloc` planted in the same writer
  is the dialect scan's to catch (§13.9), and it does.
- **A SHORT CAPACITY WRITES NOTHING**: one byte less than the measure returns
  false, and the buffer is untouched — for both classes.

## 8. Reflection: the view

**One vocabulary, one walker, three things it describes.** A TABLE carries
its descriptors BUILT IN, always — they are what the table runtime, the text
form (§16) and the block form's read side (§19.2) are written against. A
TYPE gets the SAME descriptors generated ALONGSIDE it, for every type, in
every unit (§8.2). And a UNIT gets a REGISTRY (§8.3) — one entry point
listing every declaration in the build, so a tool that knows the unit and
nothing else enumerates the types, the tables, the enums, the flags, the
unions and the constants, and walks each one's properties with no schema
files on hand.

**ONE WALKER, PER UNIT — and in a language whose units are separate
namespaces that is a real limit worth stating.** The descriptor TYPES are
generated per unit, not shared from a runtime library: C++ puts them in
`namespace <package>`, C# in `namespace <Package>`, and Rust in the unit's own
crate. So a tool that walks TWO units holds two nominally distinct
`TableFieldInfo` types describing one vocabulary, and it writes its walk once
per unit — through a template in C++, a generic or reflection in C#, a macro
or a trait in Rust — rather than once. The VOCABULARY is one; the TYPE is per
unit. A shared descriptor library would make it one type and would put a
runtime dependency in generated code that today has none, which is the trade
this side of it takes.

**Nothing selects any of it.** There is no attribute, no generate flag and
no mode: the registry and the type views are emitted ON THE SIDE (§8.5) —
one generated file per unit, carrying everything — and what a build pays is
decided by whether it COMPILES that file (§8.4). An editor does; a game
build never includes it. Nothing here rides a wire either: the view moves no
protocol id, no generated wire byte and no baseline row (§10, §18).

### 8.1 The descriptors

For every table in the unit's closure the generated header carries static
descriptors: field name, type name, wire id and kind, storage offset and
element size, array bound and count-companion offset, declared bounds,
branch guards, and the nested table's descriptor. `<Name>TableType()`
returns that table's descriptor.

**A type descriptor also carries a RESET hook** — put one instance back at
its declared defaults, in place. A generic walker that FILLS a value has to
be able to establish the defaults an absent field takes, and it holds no
type to spell; this is the one thing the columns cannot express without a
function. It calls `<Name>Reset`, the same named prefill the wire's read
path calls, and neither materialises a temporary.

**`<Name>Reset` establishes the defaults ONE MEMBER AT A TIME**, and fills
an array from its own first element rather than initialising the array as a
whole. A table is a bounded record whose declared maximum can be
megabytes, and the cost of establishing its defaults has to grow with the
number of DECLARATIONS, not with the number of bytes — some compilers
expand a whole-object initialisation of a large aggregate element by
element, and charge tens of seconds for it per translation unit. For the
same reason an array of a self-initialising element type carries no
redundant `= {}`: the element type's own member initializers already say
what an element starts as.

**A field carries its TEXT KEY** beside its name — the `json = "..."`
attribute's value, else the field's own name (§16.4) — so a walker over the
text form spells keys without a second table.

**A field carries WHERE IT LIVES, and the spelling is the language's.** C++
carries an offset and a width, because its storage is one flat struct; a
language whose fields have no address carries the pair that reads and writes
the field instead — a getter and a setter over one element, the object a
nested value is stored as, the byte buffer a `string(N)` is, and the counted
and presence companions. It is the SAME column doing the same job, and a
generic walker reaches storage through it and through nothing else. Two
consequences follow and both are law: a backend that spells the column as
accessors carries no storage-struct size, because the number describes
nothing it can use; and a walk in such a backend allocates no more than a
walk in C++, because the accessors are built once with the descriptor and
cached with it.

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

**An enum-keyed array carries its KEY's vocabulary, and the columns speak
in KEYS.** `key_type_name` names the keying enum, and `key_name( key )` and
`key_id( key )` map an ENUM VALUE to that variant's name and to the wire id
it rides under — so a tool prints `ships[Bomber]` rather than `ships[2]`,
with no schema files on hand. The element's own vocabulary columns are
unaffected: a keyed array OF enums carries both (§2.4). A positional array
leaves all three NULL.

**The public currency is the KEY; the storage index is private** (§2.4).
`array_bound` on a keyed field is the STORAGE EXTENT, `E.Max` — derived
from the enum exactly as the storage type derives it, so nothing outside
the array names its size — and a walker steps `[0, array_bound)` over
storage and asks the columns about **`index + 1`**, the key that index
holds. That one rule is the whole mapping, it lives in this paragraph, and
it is why no other column and no other section has to mention a storage
index again. **There is no invalid slot**: every index in the range is a
named variant's, so a walker enumerating a keyed array skips nothing.

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

**`key_id( 0 )` is still `0`**, the reserved id no declared name can fold
to (§5), with `key_name( 0 )` reading `"None"` beside it — because the
columns are functions of the KEY and `None` is a key the enum has. No
storage index maps to it (§2.4), so nothing a walker enumerates reaches
it; the row exists so that a tool holding a key from somewhere else — a
wire body, a text key, a user's input — can ask about `None` and be told
`0` rather than be left to a rule about slot indices. A key whose id is `0`
names no slot on this wire, and that is the one test a tool needs.

**A FIXED table carries a second set of positions for the same fields, and
this is what makes the block form READABLE BY REFLECTION** (§19.2). No flag
says it has the form — every fixed table does (§2.7), and the mode is already
in the descriptors.

**THOSE POSITIONS ARE A DESCRIPTOR OF THEIR OWN, not extra columns on the
table field's.** A block's projection is a different struct from the by-value
one (§19.3), so its positions belong to a different record: every backend
emits a separate block field descriptor — C++'s and Rust's are both spelled
`TableBlockFieldInfo` — reached from the block's own type descriptor, and a
reader looking for a `projection_offset` column on the TABLE field descriptor
will not find one. Each out-of-line array field carries there, beside the
offset its by-value storage has, the **projection offset** of its
`(offset_of, count, stride)` triple and the offsets of the three members
inside it, with the ELEMENT's own descriptor in the column a nested table
uses. Every other field of the projection carries its own position there too.

**`bytes(N)` AND `[..N]uint8` ARE THE SAME KIND AND THE TYPE NAME SEPARATES
THEM.** Both carry `kind` u8 with the array column set — the wire framing is
identical, which is the point (§3) — so nothing about the kind tells a walker
which one it has. The **type name** does, and it is the discriminator by
design: `bytes` is a keyword no declaration can claim, so a field whose type
name is exactly `bytes` is a `bytes(N)` and every other u8 array is an array
of numbers. The text form needs the difference and nothing else does: §16.2
writes a `bytes(N)` as base64 and a `[..N]uint8` as an array of numbers.

**A block field carries the SAME row-walk columns a table field does**, and
in the same vocabulary, so ONE generic walker reads a cooked node and a block
row without learning a second one. Where the field starts is the offset
above; these are everything after it — whether the storage holds an ARRAY and
how many SLOTS at what ELEMENT SIZE, the COUNT COMPANION of a `string` or a
`bytes`, and the PRESENCE COMPANION of a `?T`. The count column does one job
in both spellings: for an out-of-line array it is the triple's `count`
member, and for an inline string or `bytes` it is the int32 used length.
Without them the descriptors name a `string(15)` as twenty bytes at an offset
and no reader can tell where the sixteen-byte buffer stops and the length
begins — which is exactly the gap that kept a block's rows unreadable by
reflection while the triples alone were readable.

That is the whole mechanism behind the block form's read side: a consumer
with the descriptors reads the triples out of an instance, points at rows and
reads every field of one, with no hand-written struct per table and no
knowledge of the spelling that produced any of it. The descriptors are
constant data, so this costs a lookup, not a parse.

These columns exist in every unit, whatever its mode — they describe the
LANGUAGE, and a fixed-size table can declare all of them. Only the two
POINTER columns below are conditional (§2.2).

A unit that has pointers carries three more facts, and a unit that has
none carries not one of them (§2.2): a field's **`is_pointer`** flag —
whose `table` member then names the TARGET table's descriptor and whose
`elem_size` is the reference slot's width; a type's derived
**`variable`** mode, so a tool can tell at runtime which of §6's two
lives a table has without being told; and a table's own **node type id**
(§3.1), so a tool can map a node table's records — or a region's node
directory — onto descriptors with no schema files on hand. The
compile-time refusals those ids bring (§11) apply to EVERY unit, as the
25 generated spellings do, because a unit gains and loses pointers as an
edit and a name that was free yesterday must not become a collision
tomorrow. A self-referential pointer resolves to its own type's
descriptor. Where pointers exist the descriptors are CONSTANT-INITIALISED
data and a target is the ADDRESS of another descriptor, so `Node` naming
itself through `*Node` is expressible directly: no first-use link, which
could not have been written both race-free and recursion-safe, and no
mutable state anywhere on the surface. This is the surface
editors and tools build on — walk properties by name, print a value, diff
two, bind a property grid — with no RTTI and no schema files at runtime.

### 8.2 The type view: every type, on the side

**Every type in the unit has a view, and nothing declares it.** A `type` a
table reaches has carried these descriptors all along — a walk must be able
to enter a nested type — and a `type` no table reaches now carries them too,
in the view file rather than beside the table runtime (§8.5). There is no
marker: an editor that must inspect everything in the build cannot be served
by a set someone remembered to mark, and a set that is complete needs no
spelling to select it. The cost question the marker would have answered is
answered by placement instead (§8.4), and answered better, because a game
build then pays nothing for a type a tools build views.

That also settles the questions a partial rule would have raised and this
one does not: nothing is closure-transitive, because there is no subset for
a closure to be taken of; a union arm's payload always has a descriptor; and
a walker recursing through the nested column never meets a NULL where a
declared type stands.

**The view IS §8.1's surface.** `<Name>TableType()` returns the same
`TableTypeInfo`, with the same columns, filled by the same rules. There is
no second vocabulary and no second walker: a printer, a differ or a property
grid written against §8.1 walks a viewed type without being told it is not a
table. That is the whole of the answer to "a view on that type" — the view
of a type is the descriptor surface a table carries, generated for a
declaration that carries no table wire.

**A view describes; it does not add a wire.** A type gains no codec, no
`Measure`/`Save`/`Load`, no text form and no baseline row from having one.
The view says what may be inspected, and nothing about what may be written.

**A field with no table-wire kind is DESCRIBED, never refused.** The
no-table-wire-kind set is `fixed`, `ufixed` AND the 128-bit family
(`int128`, `uint128`) — the exclusions §2 names, refused inside a table body
(§11) and declared freely by a `type` outside every table closure. A view
over such a type describes them: `kind` is `0`, the reserved value no
declared kind spells; `type_name` carries the declared spelling
(`fixed(2, 30)`, `uint128`); and `offset` and `elem_size` locate the storage.
A unit generates its view whatever it declares, so a packet-only declaration
is a listing rather than a refusal — the corpus's fixed-point and 128-bit
unit is 27 such fields.

**The kind function must be corrected before it can answer that, and the
correction belongs with the view.** The rule that fills the kind column
dispatches an integer on its width and lets the widths it does not name fall
to the 64-bit answer — correct while every caller was a table body, where a
128-bit field cannot appear, and wrong the moment a view over a packet-only
type calls it: eight fields of the corpus's 128-bit unit answer a 64-bit
kind today, each describing a 16-byte field as an 8-byte one. The view is
what makes that path reachable, so `0` for the whole set above is this
section's rule, and the emitters' shared kind function names 128 explicitly
rather than defaulting it.

**Kind 0 DESCRIBES; it does not decode.** What a kind-0 field hands a
generic walker is its name, its declared spelling as text, its offset, its
size and its declared range as two `double`s — enough to LIST the field and
show where it lives, and not enough to read or write its value. There is no
numeric-shape column: no signedness, no `I`, no `F`, no storage width, so
nothing decodes those bytes without parsing `type_name`, which is a schema
file in disguise. The range columns do not close it either: they are
`double`s, so a bound past 2^53 — a `uint128`'s declared maximum, say — does
not survive the column, and it rounds UP, which is the one direction an
editor must never clamp to. So: a kind-0 field is DESCRIBABLE and is not
generically readable or writable, and the typed descriptor form that would
close it is a named follow-on (§15). Nothing in this section claims
sufficiency for it.

**A field of a type NO TABLE CLOSURE REACHES carries no table-wire
identity**: its `id` column is `0` and its `json` column is NULL. Those two
columns describe the table wire, and such a type has none — its field ids
were never checked for collisions (§5's refusal is scoped to the closure, as
§16.4's key refusal is), so filling them would hand a tool two fields under
one id and a text-form key for a text form that does not exist. A type
inside a closure carries both as it always did.

**The same rule governs EVERY id column the view carries, for the same
reason.** §5's refusals are scoped to the closure throughout: the variant
refusal reaches a vocabulary through a closure member's field, and through
the KEY of a closure member's keyed array, so a declaration no table closure
reaches has ids nothing ever checked. So **an enum, a flags or a union no
table closure reaches carries `id = 0` on every variant row** — the
registry's `ViewVariant.id` (§8.3) and a descriptor's `variant_id()`
function (§8.1) alike — and **an enum-keyed array whose KEY enum no table
closure reaches carries `key_id( key )` of `0` for every key**. `0` is the
reserved id no declared name folds to (§5), and it already spells "no
table-wire identity here" in the two places the columns state it: a flags
bit's id, and the `None` key of a keyed array. A vocabulary a closure
reaches carries its checked ids exactly as it did before.

**The compiler is unchanged, and is right to be.** Inside a closure it
refuses a collision by name; outside one it accepts the unit, because the
packet wire identifies a variant by its ORDINAL and carries no name hash
(§5) — an enum nothing in a table reaches keeps every spelling it ever had,
and widening the refusal unit-wide would reject a packet-only schema over an
identity its wire does not have. The consequence is the rule above, stated
so a tool never has to know it: **outside a table closure an id is not
unique**, and the view hands out none.

**The vocabulary keeps its `Table` spellings** — `TableFieldInfo`,
`TableTypeInfo`, `<Name>TableType()` — in a unit that declares no table at
all — and every unit now has a view, so every unit spells them. They are one
surface, and renaming them for the view would fork the walker every tool is
already written against. A table-free unit carries the descriptor primitives
in its view file behind the same include guard the table headers use, so a
unit that has both carries one definition of them.

### 8.3 The unit registry

**`UnitView()` is the entry point, and it returns constant data.** One
function per unit, name-first in the unit's own namespace beside
`ProtocolId` (SPEC §6.1), answering with the set of everything the build
declared:

```cpp
struct ViewConstant
{
    const char * name;
    const char * file;       // the declaring schema file's basename
    const char * type_name;  // declared storage: "int64", "float64", "int32", ...
    bool is_float;
    int64_t int_value;       // the folded value; float_value when is_float
    double float_value;
};

struct ViewVariant          // one enum variant, one flags bit, one union arm
{
    uint64_t value;         // an enum's value, a flags BIT INDEX, a union's tag
    const char * name;
    uint16_t id;            // the table-wire id (§5); 0 for a flags bit, and 0
                            // throughout a vocabulary no table closure reaches
                            // (§8.2)
    const char * payload_name;      // a union arm's payload TYPE name, else NULL
    const TableTypeInfo * payload;  // that payload's descriptor — never NULL for
                                    // a declared payload (§8.2); NULL on tag 0
                                    // and on an enum or flags row
};

struct ViewVocabulary       // an enum, a flags or a union declaration
{
    const char * name;
    const char * file;
    int64_t max;            // highest value / highest bit index / highest tag
    int32_t storage_bits;
    int32_t num_variants;
    const ViewVariant * variants;
};

struct ViewType             // one declaration: a type, or a table
{
    const char * name;
    const char * file;
    bool table;                     // declared `table`
    const TableTypeInfo * type;     // §8.1's descriptor: the properties
    int32_t num_tags;               // the declaration's type tags (SPEC §4.2)
    const char * const * tags;
};

struct UnitViewInfo
{
    const char * package;
    uint64_t protocol_id;
    int32_t num_types;      const ViewType * types;
    int32_t num_tables;     const ViewType * tables;
    int32_t num_enums;      const ViewVocabulary * enums;
    int32_t num_flags;      const ViewVocabulary * flags;
    int32_t num_unions;     const ViewVocabulary * unions;
    int32_t num_constants;  const ViewConstant * constants;
};

const UnitViewInfo * UnitView();
```

- **TYPES and TABLES are two sets, walked separately**, because they are two
  wires and a tool acts differently on each: a table can be loaded from a
  file, a type cannot. `types` holds every `type` the unit declares and
  `tables` every `table` — both complete, because completeness is what an
  editor inspecting a build needs and nothing selects a subset (§8.2). Each
  entry's `type` is the descriptor §8.1 already specifies —
  the properties, their kinds, their bounds and their nested descriptors —
  so walking a declaration's properties is one dereference from the
  registry.
- **An ENUM lists its NAMED values in order, beside its `max`.** Row 0 is
  `None`, the reserved id (§5), then the variants in declared order with the
  wire id each rides under — `0` throughout where no table closure reaches
  the enum, because nothing checked those ids (§8.2). A value inside
  `[0, max]` that no row names is `| max = K` headroom (SPEC §4.2), and a
  tool that shows such a value unnamed is showing the truth rather than
  inventing a name for it.
- **A FLAGS row's `value` is a BIT INDEX and its `id` is 0**, because a
  mask's variants ride by position and have no per-variant wire id (§4) —
  the same rule §8.1's columns state, in data rather than in functions.
- **A UNION lists tag 0, the empty arm, then its arms in declared order**
  (SPEC §4.8), each with its payload type's name and that payload's own
  descriptor. The descriptor is never NULL for a declared payload: every
  type in the unit has a view (§8.2), so an arm is walkable to its bottom.
  Tag 0 carries no payload and says so with both columns NULL.
- **A CONSTANT carries its name, its declared storage and its folded
  value** — the one declaration kind with no runtime surface of its own
  before this section. An implicitly typed constant reports the storage it
  folded to (`int64`, `float64`), a declared one reports what it declared
  (SPEC §4.2).
- **Every entry names the schema FILE it was declared in**, by basename, as
  every generated file already names its source (SPEC §6.1) — a tool
  grouping a build's declarations the way a person navigates them needs no
  second table to do it.
- **Each set is ordered by DECLARATION NAME**, and deliberately not by where
  the declaration lives. File layout and declaration order move nothing in
  this language — not an id, not a wire byte (SPEC §3.2) — so a listing that
  reordered when a file was renamed or a declaration moved between files
  would be the one artifact in the tree that did. Name order is stable under
  both, it is still one byte comparison for §8.7, and grouping a listing by
  file stays one pass over the `file` column.
- **It is constant data.** In C++ the whole registry is constant-initialised
  and `UnitView()` returns its address: a lookup, never a parse, and no
  mutable state on the surface — the property §8.1 already holds for
  descriptors, carried up to the unit. In C# the registry is a static
  instance on `Schema`, and a `ViewType`'s descriptor is reached through a
  factory rather than held as a value, because C# gives no initialization
  order across a partial class's files and a registry names every
  declaration in the unit — the same reason the C# field descriptor reaches
  its nested table through one. The entry point is `Schema.UnitView()`.

### 8.4 What it costs: the include rule

**Nothing selects the view, so nothing has to decide what a build deserves.**
The file is emitted for every unit and carries every declaration; what a
build pays is what it COMPILES. A tool that walks declarations includes the
header and compiles the source. A game includes neither, and its binary
carries not one descriptor the type wire did not already need — which is a
stronger answer than a flag, because it is per CONSUMER rather than per
generate: one unit's output serves the editor and the game at once, with no
second generate and no second tree to keep in step.

That is the same trade §13.5 already made for the text form's walker, and it
is why this file exists rather than a header addition (§8.5): a translation
unit nobody compiles costs a build nothing, while a header everybody
includes costs every one of them.

**The gate, in §2.2's shape**: not one REGISTRY symbol appears in any
generated file except the view pair. The registry's symbols are the six
§8.3 names — `UnitView`, `UnitViewInfo`, `ViewType`, `ViewVocabulary`,
`ViewVariant`, `ViewConstant` — and nothing else, because the descriptor
vocabulary a table carries is §8.1's and rides where it always did. The gate
asks "did the registry leak out of its file?", never "is there a descriptor
here?" — §2.2's own distinction between machinery and columns.

### 8.5 On the side

**The view is one generated file per UNIT, not per schema file** —
`<Package>View.h` and `<Package>View.cpp` in C++, `<Package>View.cs` in C#.
**The name is `capitalize(package)` followed by `View`** in every target
that emits one: `package tabledemo` gives `TabledemoView.h`. It is a FILE
name, and generated file names are basename-shaped in every target, so it
takes the same shape here even where the language spells the package
itself verbatim — C++ emits `TabledemoView.h` and, inside it,
`namespace tabledemo` unchanged. The registry is a unit-level fact, the set
of everything declared, and a per-file split would leave it homeless or force
one file to reach across the others and back. **The name is the UNIT's
because two units may generate into one directory** — one package per unit
(SPEC §3.2), two units side by side in one output tree is a layout the
corpus itself uses — and a fixed name would collide there while every
per-schema-file name does not.

- **The header declares, the translation unit holds the data.** The header
  declares `UnitView()`, the registry's structs and the `<Name>TableType()`
  of every type outside the table closure; the `.cpp` holds the definitions. That is
  §13.5's ruling applied where it applies again —
  an editor compiles one translation unit, and an including translation unit
  carries no descriptor table it does not use.
- **It sits at the LEAF of the unit's include graph.** It includes the
  unit's data headers `<Base>.h` and its table headers `<Base>Table.h`, and
  nothing includes it — so it introduces no cross-file cycle (§11), and it
  pulls no WIRE header: a tool that walks declarations never inherits the
  serialize runtime it does not use, which is the reason SPEC §6.1 splits
  data from wire in the first place.
- **A descriptor a TABLE needs stays where the table is** (§8.1). A table's
  descriptor names its nested types' descriptors, so a type a table reaches
  keeps its `<Name>TableType()` in the table header, where a game that loads
  tables links it and where it is today. The view file adds the types no
  table reached, plus the registry that lists everything; it duplicates
  nothing and moves nothing.
- **The text form's walker stays in `<Base>Table.cpp`** where §13.5 put it.
  The view file is a second on-the-side file for the same reason and with no
  relationship to the first: a project compiles either, both or neither.

### 8.6 What the view does not carry

- **No semantics.** A type tag is an identifier the language assigns no
  meaning (SPEC §4.2); the registry lists a declaration's tags because they
  are part of what was declared, and claims nothing about them. A claiming
  pass that gives a tag meaning changes what a consumer does with the
  listing, never the listing.
- **No UI hints.** Names, kinds, ids, bounds, extents, offsets, presence
  companions and key vocabularies — the facts the declaration states, and
  those alone. No widget, no grouping, no ordering hint, no unit of measure.
- **No description strings, and no `| doc` attribute.** Doc comments are
  deferred with their design pinned (SPEC §4.1, §9 q5); when they land the
  view carries them as one more column. A second spelling for the same text
  is not introduced ahead of them.
- **No per-field default VALUE.** A walker that fills a value establishes
  defaults through the descriptor's `reset` hook (§8.1) — one instance put
  back at its declared defaults, which is what a filler actually needs and
  what the wire's read path itself does. A per-field default column would be
  a second spelling of one fact.
- **No packet-wire layout.** Bit offsets, field widths and `MaxBits` are the
  hardcoded wire's business (SPEC §6.1); the descriptors describe
  DECLARATIONS. A view over the packet encoding itself is a named follow-on
  (§15).
- **No build identity beyond the unit's own.** The registry carries the
  package name and the protocol id, and nothing else about the build that
  produced it; what else might identify a build is a named follow-on (§15),
  so the two pages meet there rather than each inventing a column.

### 8.7 Held by test

**The editor gate, and it is a dogfood rather than a thought
experiment** — the shape §12's gates take. A test program that has the
generated view file and NOTHING ELSE — no schema files on disk, no
compiler, no knowledge of a single declaration's name — calls `UnitView()`
and prints the whole build: every constant with its value, every enum and
flags and union with its variants in order, every type and every table with
every property, recursing through each property's nested descriptor. If it
cannot reach a declaration or a property of one, the view is incomplete and
the gate says so.

**The corpus gate makes that mechanical.** For every unit in the corpus the
listing the generated program prints is byte-identical to the listing the
compiler produces from its own IR. The compiler's listing is the PIN — the
IR is what was declared — and each backend's program byte-compares against
it FOR THE UNITS THAT BACKEND ACCEPTS, so two backends that both carry a
unit produce one view of it the way they produce one wire (§16's goldens,
applied to reflection). The scope is per backend because acceptance already
is: C# refuses a pointered unit by name (§11), and a gate cannot ask a
backend for a listing of a unit it declines to emit at all. Completeness is
the count the pin carries: every declaration, every field of every type,
every variant of every enum, flags and union, and every constant, each set
in DECLARATION-NAME order (§8.3) — a variant list keeps its declared order,
which is the one order that is a property of the declaration itself. How the
compiler produces its half is the implementation's business — a test that
walks the checked unit is enough, and nothing in this section asks for a new
command.

**A gate that cannot go red proves nothing.** Dropping one declaration, one
property, one variant or one constant from an emitter's registry turns the
corpus gate red, and that is established by doing it rather than assumed.

**The INDEPENDENCE gate, and it is what the whole section rests on.** The
view adds a file and moves nothing else: every generated file other than
`<Package>View.*` is byte-identical to what the same unit emitted before the
view existed, and `schema id` reports the same protocol id. That is what
proves reflection costs the type wire's generated code not one byte — §10's
independence, claimed for a table edit, holding for a view.

**Its left-hand side is the RECORDED SOURCE GOLDENS**, because with nothing
selecting the view there is no second generate to compare against: "before
the view existed" is a snapshot, and the repo keeps one — the source goldens
pin every backend's generated text byte for byte, with the protocol id
beside them, so the gate reduces to "landing the view moves nothing in the
recorded goldens", which is mechanical and already red-capable. Today those
goldens cover the `examples` and `examples128` units, and **extending them
to the two TABLE corpora is a named follow-on (§15)** — that is the half
where movement is plausible, since §8.5 draws the boundary between the table
header and the view file straight through `<Base>Table.h`, and until the
snapshot exists the gate has no referent for those units.

Beside it, the containment gate of §8.4: not one of the six registry symbols
appears in any generated file but the view pair.

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
- **The BLOCK FORM is the strongest form both properties take** (§19), and it
  is where the requirement for them came from. A block is one flat extent
  whose every reference is a block-relative offset, so it relocates by plain
  `memcpy` with no fix-up at all — no `Lock`, because it is born compacted.
  **And going wide there is an OBLIGATION, not a permission**: the layout is
  settled from the counts in one pass over the table's out-of-line arrays
  before any worker runs, so every row's address is known ahead of the fill;
  N workers then own disjoint index ranges and write with no per-row
  synchronisation and no lock. A generated fill path that allocates, locks or
  takes an atomic does not conform, and §19.1 states that as a refuser rather
  than as an aspiration. The builder's rejected models (§14) all exist because a
  general structure cannot know its bound; a block-form table declares one,
  which is why item 3 there — reserve the max and never resize — is exactly
  what the block form does.

## 10. Independence from the hardcoded wire

Table declarations do not enter the unit's wire-shape projection. Adding,
editing or deleting a table moves no `ProtocolId` and no generated `type`
byte: peers whose hardcoded wire did not change are never forced into a
lockstep redeploy by a table edit. This independence is held by test.

**A table edit moves the BUILD VERSION instead** (§20) — the id a cooked
asset is addressed by — and the two directions are the whole of the
relationship: a table edit moves the build version and never the protocol id;
a type edit moves both. Peers connect on the protocol id alone and may differ
in build version (§20.5).

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
- **A VARIABLE-LENGTH table's WIRE SURFACE under the C#, Go, Rust, Java and
  Elixir backends** — every port carries the fixed class on the wire; their
  variable class there (the arena, the builder, the region, the node-table
  codec) is a named follow-on, and a pointered unit gets no `<Base>Table.cs`, no
  `<Base>Table.go`, no `<base>_table.rs`, no `<Base>Table.java` and no
  `<Base>Table.ex` at all, with the refusal NAMED in every source the unit does
  emit rather than left as a missing symbol.

  **The refusal is of the WIRE and of nothing else, and the distinction is the
  design rather than an exception to it.** The two ACCELERATORS are POINTED AT,
  not parsed: a block (§19) and a cook (§7) are blittable records plus a header
  match, and neither needs one line of the codec the variable class is missing.
  So a pointered unit's block and cook sources ARE emitted in every port —
  `<Base>Block.cs` and `<Base>Cook.cs`, `<Base>Block.go` and `<Base>Cook.go`,
  `<base>_block.rs` and `<base>_cook.rs`, Java's `<Table>Block.java`,
  `<Table>Cook.java` and `<Name>Row.java`, and Elixir's `<Base>Block.ex` and
  `<Base>Cook.ex`; its `<Root>Cook.Open`, its `<Root>Open`, its
  `<Root>Cook.open` and its `cook_open_<root>` open its cooked assets in full,
  and what a consumer cannot do in any of those languages is `Measure`, `Save`
  and `Load` over the tolerant wire.
  **This is what lets §7's "a root is any table, and every table gets one" hold
  in every port**, which a whole-unit refusal made impossible to say.
- **Pointers** (§2.1): `*T` where T is a `type`, enum, flags or union —
  value-semantics data has no identity to point at; a pointer declared
  outside a table body; a specified default on a pointer field; and an
  ARRAY of pointers — `[..N]*T` and `[N]*T` — which is a named follow-on
  (§15), the diagnostic naming the two spellings that serve today.
- **Optional fields** (§2.3): `?T` outside a table body; `?` on a pointer
  (already optional); `?` on a union (its `None` IS the absence); `?` on an
  array, a string or `bytes` (a named follow-on, §15 — the count or length
  already carries emptiness); a specified default on an optional; and a
  field whose name collides with an optional's `<field>_present` companion.
- **Enum-keyed arrays** (§2.4): a bound naming a `flags` declaration (a mask
  names no single slot); a bounded keyed array, `[..E]` or `[A..E]` (a keyed
  array is complete by construction); an element that is a pointer, as for
  any array (§15); an index of `E::None`, which names no slot — asserted
  through `operator[]( E )` in a debug build, and not reachable at all
  through the iteration surface, which offers the valid slots only (§2.4);
  and, on the KEY ENUM itself because a key is a reaching edge into the
  table closure, `| max = K` headroom and variant id collisions, each
  diagnostic naming the keying field that pulled the enum in. A slot value no variant names is a SAVE failure, not a silent `None`
  (§3.2).
- **Byte buffers** (§2.5): `*bytes` and `*string` reach no refusal of their
  own — a pointer takes a declared table's name and these are keywords, so
  the parser refuses the spelling where it stands. The construct is a named
  follow-on (§15).
- **The block form** (§2.7), each refusal naming the table and the field or
  declaration at fault. **Nothing declares the form, so nothing is refused
  FOR it** — a table that cannot have one simply has none (§19) — and there
  is one refusal, of a DECLARATION that collides with what the form
  generates: **a field of a FIXED table named `magic`, `build_version` or
  `byte_order`** — those three are the projection's generated prologue
  (§19.1), as `<field>_present` is an optional's generated companion.
  Claimed on every fixed table, for the reason the generated spellings
  below are: the form is not opted into, so a name free today must not
  become a collision tomorrow.
  **`| stride` is refused as an UNKNOWN ATTRIBUTE**, like any other spelling
  the closed vocabulary does not carry (SPEC.md §4.2), and not by name: the
  pitch is derived and there is no declared stride to reserve a word for
  (§19).

  **What is NOT refused, and it is worth stating because the block form's own
  need for a base invites the opposite guess**: a block-form table nested by
  value, pointed at, used as an array element or named as a union arm's
  payload. Its BY-VALUE form is an ordinary fixed table and behaves like one
  everywhere; only its block form requires a base, exactly as a cooked file's
  does.
- **The view** (§8) refuses nothing a schema can write, and that is a
  property of it rather than an omission: no attribute selects it, no flag
  configures it and no declaration can ask for it wrongly, so there is no
  spelling left to refuse. What the view does add to this section is the two
  name claims below — which are refusals of a NAME, not of a construct — and
  one file-name collision, and nothing else.
- **A `table` union arm outside a table closure** (§2.6) — a union declared
  for the type wire takes `type` payloads only, because types are value
  semantics.
- **The text form's key** (§16): `json = "..."` on a field no table closure
  reaches — keys are a table-wire construct; two fields of one table whose
  JSON keys collide, naming both, as wire ids do (§5); and a key beginning
  with `&`, the prefix §16.7 reserves to the sharing construct — the whole
  prefix, so a fixed table's text can never carry one and a reader meeting it
  never has to guess.
- **An edit the committed TABLES BASELINE forbids** (§18), when a unit has
  one: a specified default changed, added or removed; a flags variant
  inserted, removed, reordered or renamed in place; a field's wire kind or
  an array's ELEMENT kind changed; an array changed between the keyed and
  the positional spelling; an enum-keyed array's key enum swapped; a
  field's referent dropped, or swapped for one whose identities do not
  ride. Overridden only by moving the baseline with a recorded reason.
- **A save-time data cycle reached from a builder** (§3.1): measure,
  save and `Lock` all return failure with the cycle named. Nothing
  recurses away. A region loaded from a wire is not re-proved, and a save
  from it reproduces what it was given.
- **A node body past `0xFFFFFFFF` bytes** (§3.1): a record's length is a
  `u32`, so measure and save return failure naming the node rather than
  truncating it. Two `bytes(2147483647)` fields in one table reach it; the
  repair is more nodes.
- **A field id colliding with the reserved node-table id `0xFFFF`**
  (§3.1) — by hash accident or through `was` — naming the field. **Two
  tables in one unit's closure whose NAME ids collide** (§5), naming
  both: a node's type id is its table's name hash.
- **A declaration colliding with a generated table spelling.** Tables and
  types share one symbol table (§13.1), which is what makes the generated
  surface unprefixed and collision-free — so every name a closure member
  claims is refused to everything else. A member `X` claims `X` followed by
  each of these **36 suffixes**, and a declaration spelling one of them is
  refused naming the collision — the block form's nine and the C backend's
  seven follow below, for **52 in all**:

  ```
  Measure  MeasureBody  Save  SaveBody  SaveBodyFields  Load  LoadBody
  Reset  LoadMeasure  LoadBuilder  TableType  Builder
  At  Emplace  Pack  PackMeasure
  Number  NumberFrom  MeasureWire  SaveWire
  NodeStorage  NodePlace  NodeAlloc  NodeBody
  Cook  CookMeasure  CookBody  CookLayout  CookMeasureFrom  CookFrom
  Open  TableFields  TableInfo
  FromJson  ToJson  ToJsonMeasure
  ```

  The set is claimed for EVERY closure member, not only pointer-bearing
  ones: a table gains or loses pointers as an edit, and a name that was
  free yesterday must not become a collision tomorrow. That list is the
  checker's own, and this section is held to it: the three lists here — 32, then
  the block form's nine, then the C backend's seven — are `tableGeneratedVerbs`
  entire, spelling for spelling and 48 in all, because a claim the page states
  and the checker does not make is a name a user may take.

  **`Open` AND `Cook` ARE BOTH EMITTED NOW — in different languages, and that is
  what the C# rule below is for. `OpenWalk` was RETIRED.** The C++ table backend
  emits `<X>Open` for every TABLE (§7) — a root is any table — so that spelling
  is a definition and not only a claim, and it is claimed for every closure
  member all the same, on this list's own rule.

  **THIS LIST IS A SUFFIX SET, NOT A CASING.** Each backend spells the set in
  ITS OWN LANGUAGE, and specifically in the convention that language's PACKET
  half already uses, because the two halves land in one project and a reader
  should not be able to tell which emitter wrote a line. C++, C# and Go write
  `<X>Load`; Rust writes `<x>_load`; C writes `<x>_load` too — types PascalCase,
  functions and file-scope constants snake_case, macros SCREAMING_SNAKE under
  `SCHEMA_`, which is exactly what `generated/c/` carries for the packet wire.
  The CLAIM is unaffected: a closure member `X` claims `X` followed by each
  suffix in the schema's own spelling, and the checker refuses the declaration,
  not the emitted identifier.

  **GO SPELLS THE SAME VERBS AS FREE FUNCTIONS, because Go has them**:
  `<X>Open` and `<X>At` are package-level, exactly as C++'s are, and `<X>Cook`
  names the READ HANDLE — a struct of a region pointer and a length. What Go
  makes a MEMBER is only what this list already leaves a language free to make
  one: a block's row accessors and its per-array constants, and a cook's `Type`,
  `Root` and the three §7.1 facts. A member claims nothing at file scope.

  **C# HAS NO FREE FUNCTIONS, so its claimed verbs are MEMBERS of a claimed
  TYPE** — the rule this list already gives the block form's accessors, applied
  to the cook. The C# table backend emits `<X>Cook`, a `readonly struct` over an
  opened region, carrying `Open` and `At` as members and one accessor per field
  named after that field. So `Cook` names the READ handle in C# and the
  WRITE function in C++ (§7.6), one claimed name in two backends — and a C#
  cook WRITER, if one is ever emitted, is `<X>Cook`'s members too and claims
  nothing further. A table also **claims one member per field on its `Cook`
  type**, exactly as it claims two per out-of-line array on its `Block` type,
  and a language whose accessors are members claims nothing at file scope for
  them.

  **`CookMeasure` AND THE WRITE HALF OF `Cook` ARE EMITTED NOW TOO**, by the
  C++ backend, for every table of either class (§7.6) — so `Cook` names a
  definition in two backends and two different things: the READ handle in C#,
  and the write function in C++. Both are claimed on every closure member all
  the same, on this list's own rule; the other languages' writers are named
  follow-ons (§15), and a build that wants one today runs the tool. The write
  side's own spellings — `CookBody`, the record writer every closure member
  gets, and the pointered root's `CookLayout`, `CookMeasureFrom` and
  `CookFrom` — are on the list for the same reason `Number` and `NodeBody` are.
  **`OpenWalk` LEFT THIS LIST**, and it is the one entry ever removed: it named
  wire v1's validating walk, and §7's `Open` is a header match with no walk in
  it, so the name went with the design rather than being held for an emitter
  nothing will write. A claim nothing needs takes a name away from every schema
  for free, and freeing one is cheap before release and never again — the count
  above moved with it, and a test asserts `<X>OpenWalk` is legal, because an
  unclaimed name is invisible unless something asks for it. `<X>Root` is NOT
  claimed and needs no claim: the builder's accessor is the member `GetRoot`,
  renamed for the reason below, so no emitter ever spells it.

  **The BLOCK FORM claims nine more, and the checker claims them.** They are
  law on the same terms and for a stronger reason — every fixed table has a
  block form (§2.7), so every fixed table claims them:

  ```
  Block  BlockStorage  BlockBegin  BlockBytes  BlockMaxBytes  BlockOpen
  Counts  Row  BlockProjection
  ```

  **THE C BACKEND CLAIMS SEVEN MORE**, and the checker claims them on the same
  terms. C++ and C# put these on a class — a builder's `Lock`, a storage's
  `Create`, a block type's `Type` — and a member function claims nothing; C has
  no members, so each is a free function under its owner's name (§6.1's C
  column):

  ```
  BuilderInit  BuilderShutdown  BuilderLock  BuilderRoot
  BlockStorageCreate  BlockStorageDestroy  BlockType
  ```

  **`Row` and `BlockProjection` are the C# BLITTABLE records' names** (§19.2),
  and they are claimed for the reason the rest of this list exists rather than
  planted in a namespace of their own: a generated namespace named by a common
  noun collides with declarations in OTHER units of the same assembly, which a
  compiler that sees one unit cannot refuse.

  **A table also claims TWO PER OUT-OF-LINE ARRAY**, because its row
  accessors are named after its fields: `<Table>` followed by the PascalCase
  of the field's name hands back that field's rows, and the same name with
  `Span` appended is the contiguous view (§19.2) — so `RenderFrame` with a
  `ships` array claims `RenderFrameShips` AND `RenderFrameShipsSpan`, and a
  declaration spelling either is refused naming both. That part of the set
  moves with the declaration, which is why it is a rule here rather than a
  list. A language whose accessors are members spells the same two names on
  the block type and claims nothing at file scope for them.

  **THE DESCRIPTOR SURFACE'S CLAIMS ARE TO BE UNCONDITIONAL — every
  declaration, every unit, tables or not — AND ARE NOT IN FORCE YET.** Every
  unit is to emit a view file and that file defines the descriptor surface
  (§8.2), so a name a table-free unit may declare today would collide with
  its own generated code the day its view is emitted — a legal schema whose
  generated code does not compile, which is the one defect this whole list
  exists to prevent. **What ships today claims these names only in a unit
  that declares a table**: a table-free unit still accepts `type TableReport`
  and `const TABLE_COOK_MAGIC`, and it must stop doing so BEFORE the first
  view file is emitted, not after. This paragraph is the obligation, and the
  gap between it and the checker is stated here rather than left for a port
  to find. Two sets follow, and both are FRONT-END LAW rather than one
  target's inventory:

  - **The three per-declaration spellings the descriptors emit** —
    `<Name>TableFields`, `<Name>TableInfo` and `<Name>TableType` — claimed
    for every declaration in every unit. All three are in the suffix list
    above and are claimed today only for closure members; the descriptor emission
    spells all three per declaration, so widening one of them would leave
    two open.
  - **The unit-level TABLE-RUNTIME names**, claimed in every unit rather
    than only in a unit that declares a table: the descriptor primitives
    `TableTypeInfo` and `TableFieldInfo` at their head, with
    `TableUnionInfo` and `TableUnionArmInfo` beside them — a union field's
    column has to NAME a type, in both backends, and a walk holds a
    descriptor by value so neither can hide inside another — and beside them
    the rest of the one registry the checker and the emitters share —
    `TableKeyed` (an enum-keyed array's storage, and a keyed array occurs in
    a `type` body: this document's own `ScoreBoard` declares one),
    `TableRef`, `TableReport`, `TableWriter`, `TableReader`, `TableEnumId`,
    `TableEnumValue`, the COOKED form's read runtime (`TableCookOpen`,
    `TableCookMagic`, `TableCookByteOrder`, `TableCookMaxAlign`,
    `table_cook_read64` — §7 — its WRITE runtime beside it (`TableByteOrder`,
    `table_cook_put`, `table_cook_bytes` — §7.6, three names because a store
    takes its width as an argument rather than minting one name per width) —
    and the C# half's `TableCookLayout`,
    `TableCookInfo`, `TableCookFieldInfo` and `TableCookStorage`, with
    `TableCookHeaderBytes` and `TableCookRead64` riding as members of `Schema`
    and so claiming nothing at file scope), `BuildVersion` (§20, which both
    accelerators carry) and the rest of that list. §8.2 has a table-free unit's
    view file DEFINING those primitives, so a unit that declares no table
    can no longer be allowed to declare their names.

    **JAVA WIDENS TWO MORE AND ADDS ONE, on exactly the rule Go's widening
    states: a port's spelling decides the claim, and the claim is the UNION.**
    `TableJson` is one nested class of `Schema` in C#, where every function,
    sink and scanner it spells is a member reached through its owner and claims
    nothing — and it is a PACKAGE-LEVEL class in Java, whose unit scope is the
    package. So the name is claimed. `TableBlockLayout` widens for the same
    reason. And `TableBytes` is Java's own: C++ reads a record through its type,
    C# through a pointer cast and Rust through a transmute, and Java has none of
    those, so every multi-byte read of a block or a cook goes through one
    package-level class of explicit little-endian readers — which is also what
    settles the byte order of both accelerators without asking the host.

    **A schema FILE named for one of those runtime types is refused too, under
    the Java backend.** A file base is what names the packet emitter's class,
    and a public Java type lives in a file of its own name, so a unit with a
    table and a `TableReport.schema` in it would have two `TableReport.java` to
    write. It is a file-name collision rather than a declaration collision, so
    the refusal names the FILE, exactly as the view file's does below.

    **GO WIDENS TWO OF THESE AND ADDS EIGHT, and both moves are the port's
    spelling rather than a new construct.** `TableCookHeaderBytes` is a member
    of `Schema` in C# and a package-level constant in Go, so the claim is the
    union and the name is claimed; and `TableCookStorage`'s eight MEMBERS —
    `TableCookStorageRecord`, `…Reference`, `…Bool`, `…Signed`, `…Unsigned`,
    `…Float`, `…String`, `…Bytes` — are scoped inside a C# enum and FLAT
    package-level constants in Go, which is exactly what the Go packet emitter
    already does with a declared enum's variants and exactly what the checker
    already claims for those. A port's spelling decides the claim. **Go's own
    text walk claims nothing**, for C#'s reason in Go's spelling: every function
    the walk defines is unexported, and a schema declaration always generates an
    exported name, so the two sets cannot meet.

    **ELIXIR UNSCOPES ONE AND ADDS TWO, and the collision class is the
    language's own.** A declaration lowers to a MODULE under the unit's
    namespace, so what a schema can collide with is exactly the set of
    unit-level module segments the emitter defines — not a `Table*` prefix,
    which would have been blind to three of the four this backend spells.
    `TableRuntime` is a private crate module in Rust, which no declaration can
    reach, and a `<Package>.TableRuntime` module in Elixir, which one lowers to
    exactly: the claim is the UNION, so the name is claimed for every target.
    The two additions are `BlockRuntime` and `CookRuntime`, the two
    accelerators' shared runtimes, which are their own modules because a
    VARIABLE unit gets no table runtime at all (above) and still has both
    accelerators. **Elixir's own text walk claims nothing beyond
    `TableRuntime`**: every function it spells is a function of that module,
    reached through its owner, and a declaration lowers to a module rather than
    to a function of one.

  **The view's own unit-scope spellings are refused as declaration names in
  every unit, always** (§8.3): `UnitView`, `UnitViewInfo`, `ViewType`,
  `ViewVocabulary`, `ViewVariant` and `ViewConstant`. They belong to the
  unit rather than to a declaration, so they are claimed once at unit scope,
  as `ProtocolId` is (SPEC §6.1), on the same terms as everything above.

  **A schema file whose generated name would be the view file's is refused**
  (§8.5): a file named `<capitalize(package)>View.schema` — `TabledemoView`
  in `package tabledemo` — emits `TabledemoView.h` from two sources at once.
  It is a file-name collision rather than a declaration collision, so it is
  refused naming the FILE, and it is stated here because the view is what
  introduces a generated name that is not derived from a schema file's own.
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
schema prescribing any of their structure.

**Where the gate is held.** The corpus half is a config format declared and
packed end to end: `tables/examples/Pack.schema` is the root — an
enum-keyed collection of records, an optional group inside a record, a
global block nested by value, a bounded array of records and a keyed array
of scalars, fixed-size down to the leaves — and `tables/pack/config/` is the
directory tree `schema pack` assembles into it and `unpack` writes back out
(§17), byte-stable in both directions under `make`. That half proves the
form; it does not prove the game. The dogfood half is a real game taking its
config and asset archives on this wire: 803 values byte-identical end to end,
and every injected bit flip refused rather than read as data.

**The shape the gate is held to.** `Config.bin` and `Assets.bin` are each
ONE root table, and each root is FIXED-SIZE down to the leaves: no pointer
anywhere in the closure. `?T` (§2.3) expresses an optional group by
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
times a second, and faster than it simulates, because rendering need not
wait for a tick — must be expressible as declared tables with nothing left
over, with BOTH sides generated: a C++ producer writing the block, and a C#
consumer POINTING at it and reading rows in place. The bar is not that it
works. The bar is that it is as fast as the hand-written scatter it
replaces, on both sides, and that nothing about the declaration made it
slower.

**This is the hottest path tables have, and the page ranks it that way.**
The other table paths — a save game, and above all an asset cooked to a
build's own layout — are read-hot and write-cold: written once, offline, by
tooling and a build cache (§7), and read fast by the game, so the reader is
where the effort goes and the writer is a build cost. **The block form is
the one form hot on both sides**: written every frame in one language, read
every frame in another, at 60 Hz or better. The fixed class's performance
doctrine (the ladder) bites hardest here, and this gate is what decides
whether tables earn the render data at all.

**It is PRE-COOKED AT BUILD, and that is the property that makes it fast.**
Every layout fact — the projection's own offsets, each row's size, each
pitch — is settled by the compiler and asserted into both generated sides
(§19.3). Nothing is negotiated, discovered or checked at frame time: the
producer writes at known offsets, the consumer points at known offsets, and
the contract lives in generated asserts and the build version rather than
in a runtime check on the hot path. A cook does the same thing for an asset
at load time; the block form does it for a frame, every frame.

**Why the bar is stated that way.** The producer this gate is held to is
multi-threaded by design, and the previous attempt at a general answer was
abandoned for exactly this reason — flatbuffers built the render data once
and lost, *"because it was not parallizable"* (§7). So a construct that
serializes the build, allocates per row, or forces a copy at the boundary
has already failed the gate, however clean the declaration reads. The block
form (§2.7, §19) is the shape that answers it, and the pitch is the
load-bearing part: **striding is what makes the interop fast** — blittable
rows at a fixed pitch that both generated sides index with, with no
marshalling and no copy AT THE BOUNDARY. (What a consumer then does with a
row is its own business: the one that exists copies rows into a pool so its
jobs can take them, and that copy is the consumer's design, not the form's.)

**The shape the gate is held to.** One fixed table (§2.7), fixed down to the
leaves: bounded arrays of fixed-size records, no pointer anywhere in the
closure. The block's storage is sized from the declared maxima and its layout
is settled from the counts before any worker starts, so N workers fill
disjoint row ranges with no lock, no atomic and no per-row synchronisation —
and that is an OBLIGATION on the implementation, not a permission to the
caller (§9, §19.1). The consumer maps the block,
checks it once, and reads each array at the pitch the instance gives it. The
layout contract — the projection's own offsets, every row's size, every
field's offset, every pitch — is asserted by GENERATED code on both sides
(§19.3), which is the half that replaces a hand-kept mirror with a
compiler's word.

**What "with nothing left over" means here, concretely.** The dogfood's
render path today holds its layout contract by hand on both sides: a wall of
`static_assert`s naming each record's `sizeof` on the C++ side, and a
hand-written blittable mirror on the C# side that a person must edit in the
same commit. Clearing the gate deletes both — the mirror because the
descriptors carry the layout and the C# backend generates the blittable
struct beside them (#287), and the asserts because the generated pair
asserts the same facts from one declaration. A field added at the end of a
record must reach both languages from one edit to one schema file, and the
compiler must be what says so.

**The measured bar, and where it is taken.** Two numbers, both required:
**the per-frame C++ WRITE and the per-frame C# READ**, generated form
against the hand-written scatter and the hand mirror, paired in one sitting
under the bench rules this repo already runs under — gated goldens first,
medians paired, contaminated runs discarded whole. **The bar is SAME SPEED,
or not significantly slower**, and it is stated that way deliberately: a
generated form that lands inside the noise of the hand-written one has
cleared it, and a measurable regression on either number is a defect to
explain or close, not a trade to license. This is the fastest-correct mission
applied to the rung the block form sits on, and the rung is the top one
tables have.

**Its PROVENANCE, recorded because it explains two older requirements.**
§9's relocatability and §6.4's multi-threaded, lock-free builder were
written FOR this case — *"this is where the relocatable and multithreaded
builder requirement came from"* (§13.1) — not for the config and asset files
gate 1 holds. Gate 1 proved the tolerant wire could carry a content
pipeline; gate 2 is the original requirement made explicit, and the block
form is where both properties are strongest (§9).

**This gate is per-language too, and it takes TWO languages at once**, which
gate 1 does not: a block whose producer and consumer are the same language
proves nothing about the ABI the form exists to be. C++ and C# together are
the gate; a third language joins it as its backend lands (§15).

## 13. Rulings, recorded

**THE AIM, 2026-09-02, in the owner's words, and every ruling below is
measured against it**: *"We aim to build the best cross-language data type
system for games."* — *"This includes save games, tooling, cooking to runtime
efficient structures and so on."* It spans the whole use-case list
(the examples in README.md), not the wire alone: the tolerant wire, the cook and the block
form are three answers under one aim rather than three products.

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
- **Why very large tables want a blob node** (§15): "the key with variable
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
  Optional is part of the design: `[E.Max]T` stays legal and positional;
  `[E]T` is the keyed form a user chooses.
- **Enum arrays shift left** (§2.4, 2026-09-03): "I think there is really
  just a rule here for enum arrays" / "enum arrays should not pack index 0"
  / "and shift left." The basis was given the day before: "the 0th entry in
  the array is not valid, and is 'null', so you really only store n-1
  entries… If you look across the usage in ConfigManager and AssetsManager
  in C++, you'll see this is historically how I've always done it. This
  saves memory." So the storage is `E.Max` elements, the key `k` lives at
  index `k − 1`, and nothing is stored or packed for `None`. The rule is
  the ARRAY's, not the wrapper's: it holds in a `type` body, where the same
  spelling is a plain array, exactly as it holds in a table body.
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
- **The cook is ON the ladder, and the dogfood not using it is not a verdict
  on it**, 2026-09-02: "We do cook. We just aren't using it in space game
  right now." The two files the dogfood carries are small enough to stay on
  the generic wire (below); the cook is built for the catalog-scale files
  beside them (§7).
- **The scale, and when its gate binds**: "We can keep the gigabyte scale
  stuff for v2, but think about it as we work now." The 1 GB open-time gate
  belongs to the COOK's emitter (§7, schema#251) and is held over it: the
  design constraint it comes from — nothing per node at open — is what §7's
  `Open` is, a match and a point, and open time is flat from a megabyte to a
  gigabyte.
- **What it is NOT**, and it is the name: "It's not a 'wire' protocol, it's a
  load trusted data from tools protocol." A wire crosses a boundary between
  builds; a cook crosses none.
- **A cook is TRUSTED INPUT**, 2026-09-03: "OK to be clear, cooked data is
  generally trusted and will be loaded from disk." That is what makes `Open`'s
  checks IDENTITY checks rather than a trust boundary, and it is why there is no
  per-node validation at load at any scale (§7).
- **And if trust is in question, the answer is a SIGNATURE and not a walk**:
  "If we have concerns, then we should sign it." / "We just have to load the
  data." Integrity is one verification over the whole file before `Open`, never
  per-node checks in the loader; banked as a named follow-on (§15).
- **Which files earn one**: "imagine a huge data file that specifies all
  the meshes used in the game for example, or all the texture files" —
  "that would be a cook"; against "Config.bin and Assets.bin are small
  enough that the overhead of loading doesn't require a cook", so the
  dogfood's own two files stay on the generic wire.
- **The version rule**: "cooked data at a specific version should just
  load. It should probably only be loadable at that specific version", "or
  a subset of versions AT MOST", "consider an asset format that rarely
  changes over time." Exact build-version match is what §7 states; the
  declared compatible set is the only widening contemplated, and it is a
  named follow-on (§15).
- **Where the per-node bookkeeping lives**: "Can we keep the overhead and
  tracking down for the non-cooked version only, and then when cooking,
  split it into two parts, the cooked data, and the cooked attribution
  data so we can separate if needed. We may not need at runtime in a
  shipped build for example (it is read only and not mutable)."
- **On sizing the wire's ids**: "u64 now", and "u64 now, why fuck around."
- **THE REGION REFERENCE SLOT IS EIGHT BYTES**, 2026-09-03: "move now." The
  slot had been four, which bounded ONE REGION at `2 GiB` — a self-relative
  delta cannot reach further than the width it is stored in — and the ruling
  applies the two facts already on this page to it: the sizing rule above,
  "u64 now, why fuck around", and the scale the cook exists for, "Assume we
  have say, 100mbs or many gigabytes of data in Assets.bin at some point."
  A ceiling that a mesh or texture catalog is exactly the thing to meet is
  not a ceiling to keep pre-release. **The cost is stated rather than
  discovered** (§6.3): eight bytes a reference in every variable-length
  record, needed or not, and every affected unit's BUILD VERSION moves, so
  every cooked file is re-cooked — which pre-release is a regeneration. A
  value-only table pays nothing: no pointer, no slot.
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

### 13.6 The block form, ruled

Owner rulings, 2026-09-02, in the order given.

- **The requirement**: "New requirement just dropped. Using tables, or
  types, we should be able to implement the render data" — and the scope,
  "*including* the blittable arrays with stride."
- **Tables, not types, and why**: "probably with tables, I would imagine.
  since render data is BIG", "so it's more table like than for example, a
  type." The block form is a form a fixed TABLE takes (§2.7), and §2.2's mode
  derivation needs no edit to say so.
- **The producer is multi-threaded by design**: "note that the hand-written
  code that walked and generated render data from C++ is multi-threaded by
  design" — which is the constraint the layout and the fill are shaped by (§19.1).
- **Where two older requirements came from**: "this is where the
  relocatable and multithreaded builder requirement came from" (§13.1).
- **The prior attempt, and why it lost**: "I used to use flatbuffers to
  build render data, but it was too slow because it was not parallizable"
  (§7) — which is what makes gate 2's bar measured rather than stylistic.
- **The pattern, named**: "Do you see how the render data is sort of its own
  structure, a header with sections that it points to" / "each section being
  a strided array of some type?" / "This is its own pattern."
- **And then dissolved into tables, which is where it landed.** "is render
  data a table? are the render types being strided types or tables?" / "can
  render data root be a table?" / "if it is, then we can make tables more
  powerful." / "i would suggest that render data is a root table, with
  strided types or fixed tables being allowed. strided making no sense for
  non-fixed tables or types." / "in fact, fixed tables and types are
  semantically the same (structs) they just have different wires and
  versioning stuffs." / "why even have a 'section'?" / "feels like the
  strided array and the section are the same thing, what you really need is a
  new concept of header with offset_of" / "header itself could be a table or
  type?" / "yes, i guess header is root table expressed in some way that
  C++/C# can read header data, and get data about the strided arrays." / "so
  in this context header just collapses into stuff that a table knows about
  its own rows." / **"It's tables all the way down."** That sequence is why
  §2.7 declares no construct and §19 describes a FORM: the strided array is a
  bounded array, the header is the table's own instance laid out so the other
  side can read it, and nothing new is declared.
- **The builder, as a requirement rather than a capability**: "And the
  builder must be able to be multi-threaded. Hard requirement." §9 and §19.1
  state it as an obligation on the implementation, held by a refuser.
- **The path's rank and its shape**: "Yes, the render struct is interesting
  in that it is a structure that must be fast and effectively, pre-cooked on
  build. It must be fast to both write in C++ and read in C# since it is
  large and it is 60HZ or greater (we can render faster than we simulate)."
  And: "It is the hottest path for tables to be sure."
- **Why the other paths are ranked below it**: "The other paths for tables
  like save games, or especially assets -> cooked -> binary format for
  specific build version, these only need fast *readers*" — because "the
  building will be done by tooling offline", "(and generally, it will be done
  in a distributed build cache sort of way... this is how it is usually
  done)". The ladder reads that as read-hot and write-cold, and §7 carries
  the addressing fact it rests on.
- **How a cooked artifact is addressed** — the rulings that opened the build
  version are recorded together in §13.8, and §20 is the section they
  produced.
- **What C++ and C# each actually need**: "The way we currently read render
  data header in C++ and C# is just a builder issue in C++ and a reflection
  issue in C#." §19.1 is the builder half and §19.2 the reflective half, and
  neither is new machinery.
- **What the stride buys, first**: "striding is necessary for fast interop
  between C++ and C#" — "so that's the real thing here." The pitch is the
  point: blittable records both generated sides point at, no marshalling and
  no copy (§12.1, §19).
- **What a DECLARED stride would also have bought**: "the benefit of striding
  is that i can add new fields at the end of types/tables, without C#
  exploding", "because C# just doesn't know about the new fields yet, and
  stride is bigger than struct width" — then, refining: "It may be obsolete
  now, but this was the original intent", "If we generate both render data
  C++ and C# side now, it's less of a concern."
- **The declared stride is CUT, 2026-09-02**: **"OK got it. We cut it."** The
  pitch is `sizeof` rounded to alignment, derived, always, and it rides in
  the triple (§19); there is no `| stride`, no headroom follow-on and no
  reserved spelling — an unknown attribute is refused as unknown. The
  condition the cut is held to is the hard requirement: "I mean as long as we
  can make it work with the fast blitted C# stuff." / "that's the hard
  requirement. so if we don't need stride anymore then c'est la vie." A
  derived pitch is what MAKES that work — rows at `sizeof` are contiguous, so
  the C# read is a span reinterpreted over blittable storage, and a declared
  stride is exactly what would have broken the contiguity it needs (§19.2).
  The layout contract and the baseline are the guard of record (§19.3,
  §19.4), and gate 2 on the box is the proof, at the owner's bar: same speed,
  or not significantly slower (§12.1).
- **The purpose, in one line**: "this is a 'nice' property to get some sort
  of more robust structure (ABI) between C++ and C# without hardcore
  versioning", "because both sides were previously manually updated."
- **No per-table marker, 2026-09-02.** Asked "what is it about render data
  that requires a specific per-table declaration?", the ruling was **"KISS"**.
  Nothing does: the `| block` marker was a cost switch and not a semantic, so
  it is gone. Every fixed table has a block form and the form is emitted on
  the side (§19) — a consumer that includes it pays for it, one that does not
  pays nothing — which is what the marker was buying and the only thing it
  was buying. §2.7 declares nothing at all now, and §14's decision below
  records what the marker cost.

### 13.7 The view, ruled

Owner rulings, 2026-09-02, in the order given.

- **The question that opened it, and both halves are yes**: "We should
  revisit the 'view' concept. Does a type have a view generated for it,
  alongside it, and a table has the view built in?" A table's descriptors
  are built in (§8.1); a type's view is generated alongside it, for every
  type, always (§8.2). That is the section's whole shape.
- **What has to be walkable, beyond fields**: "I would find it valuable to
  be able to walk enums, constants at runtime via some reflection." Constants
  had no runtime surface at all before §8.3, and enums had one only through
  the field that named them.
- **Per enum, and then the sets**: "I would find it valuable to be able to
  walk per-enum the set of values and names." / "I would find it valuable to
  be able to walk the set of types." / "And the set of tables." The registry
  is those three sentences: a vocabulary's values and names in order, the set
  of types, the set of tables (§8.3).
- **Down into each, and the qualification that decided the design**: "And
  then be able to walk each table set of properties." / "And be able to walk
  types and properties too!" / **"(but with a view on that type?)"** — which
  is why a type's view is the SAME descriptor surface a table carries rather
  than a second vocabulary (§8.2, §14).
- **The acceptance test, in his own framing**: "Let's imagine you were an
  editor and you wanted to just be able to inspect everything that was in
  the schema built." §8.7's gate is that sentence made mechanical — a
  program that knows only the unit, reaching every declaration and every
  property of each.
- **And what it is for**: "Walk it and display it somehow."
- **The placement, granted rather than argued for**: "This can be *on the
  side* if you want." §8.5 takes the grant literally — one file per unit
  that an editor links and a game build never compiles.
- **And then the subtraction, which is the ruling that shaped the rest.**
  The marker and the generate flag are gone: the view file carries
  everything, for every unit, always, and a consumer pays by compiling it.
  The grant above is what makes that free, and completeness is what the
  editor sentence above asks for — so the attribute, the three-valued flag,
  the reserved word they needed, and every refusal they brought with them
  went out together (§8.4, §14).

### 13.8 The build version, ruled

Owner rulings, 2026-09-02, in the order given. §20 is the section they
produced.

- **How a cooked artifact is addressed**: "so the hash of the asset and the
  cooked version is the asset the runtime searches for." — "asset hash,
  cooked version hash." / "(as in, build version, schema table protocol id
  effectively...)".
- **The concept, named as new**: "so this is a new concept. some sort of
  'build version' that depends both on wire protocol id, and protocol id for
  current set of tables, as they would cook."
- **The invariant**: "Any cook binary divergence = new build version."
- **The name**: "call it 'build version'"
- **The trick, and it is the line the whole section turns on**: "now, the
  trick is, protocol id remains as schema and types only" — §10's
  independence, restated from the other side.
- **The connect gate**: "we will only let a client connect/disconnect with
  same protocol id as server" / "but it COULD potentially have different
  table versions" (§20.5).
- **And what carries the difference**: "and versioning would be expected to
  save us." The protocol id refuses where nothing could save two peers; where
  tables cross builds the tolerant wire (§4) is expected to, and the baseline
  (§18) is what keeps that expectation honest.
- **What the rule is FOR**: "so actual assets, like meshes, textures and
  whatever." / "they are cooked to some build version, and can only be
  loaded by a binary with that same build version"
- **The tuple, and the ONLY meaning the name carries**: "consider the case of
  tooling generating data from the same source assets, it needs to generate
  and write asset X to a specific build version Y, thus the uniqueness tuple
  in the data store cache of cooked assets is indexed by (X,Y) tuple. this is
  the only meaning of build version that I had in mind, something that could
  be Y." / "or that the game uses to *IDENTIFY* a cooked asset in a
  distributed data store." / "during development." Tooling writes `(X, Y)`
  before any game binary exists and the game asks for `(X, Y)`, which is why
  Y is compiler-settled (§20).
- **And the whole design's id count**: "yes on protocol id and build version"
  — two ids, and §20 states them as the two.

### 13.9 The C++ dialect, ruled

Owner ruling, 2026-09-03: *"It should also feel good and look native to a
C++ game programmer (C-like C++, no modern C++ features, not pulling in STL or
a lot of big headers from modern C++, ability to pass in custom assert
function, log function, allocate/free functions)."* Then, against the
findings on the generated corpus (schema#382): *"fully go C-like C++ dialect
with minimal modern feature usage, like serialize"* and *"save any modern
stuff for cpp_modern... Just don't."*

**The reference is `serialize.h`**, and the generated table text follows what
it does rather than a list of its own:

- **The C header spellings** — `<stdint.h>`, `<string.h>`, `<stddef.h>` —
  never `<cstdint>`, `<cstring>`, `<cstddef>`. No STL header: no
  `<type_traits>`, no `<iterator>`, no `<algorithm>`, and no `std::` name in
  a fixed-class header at all.
- **The layout guarantees read the compiler**, not a library:
  `static_assert( __is_trivially_copyable( T ) )` and
  `static_assert( __is_standard_layout( T ) )` are the intrinsics every C++
  standard library implements the traits with, and every compiler this repo
  builds under — gcc, clang, MSVC — takes them without a header. The asserts
  still bite: a `virtual` member fails them (held by test).
- **Iteration over a keyed array is `begin()`, `end()` and `size()`.** The
  iterators carry no `iterator_traits` typedefs; `std::distance` bought
  `<iterator>`, which measured as the most expensive include in the corpus
  (536 headers, 986 KB) for an audience that does not call it.
- **Compile-time constants are `static const`**, the C++98 spelling serialize
  uses, not `inline constexpr`. In-class integral constants stay
  `static constexpr`.
- **Every call into the C library goes through a hook**, spelled the way
  `serialize_assert` is: a plain `#ifndef` the program wins, and the C header
  included only when the program supplied nothing. The names carry no
  package: a program defines them once, before its first generated header,
  and every unit picks them up.

  | hook | where it fires | default | NDEBUG |
  |---|---|---|---|
  | `schema_assert( condition )` | the keyed accessor's refusal (§2.4), with its message | `assert`, from `<assert.h>` | removed, as `assert` is |
  | `schema_fatal()` | after the assert, on the path that cannot continue | `abort`, from `<stdlib.h>` | kept |
  | `schema_allocate( bytes )` | the default allocator pair: ZEROED bytes, NULL on failure | `calloc`, from `<stdlib.h>` | — |
  | `schema_release( pointer )` | the default allocator pair | `free` | — |

  A unit whose closure declares no keyed array emits no assert hook and
  no fatal hook; one that declares no pointer emits no allocator hook in its
  `Table.h`. The block header emits the allocator hook, because
  `TableBlockDefaultAllocator()` lands in it (§19.1). Define `schema_fatal`
  and `schema_allocate` and no generated header includes `<stdlib.h>`.

- **A `TableAllocator` per structure, on top of the default.** It is the
  shape `TableBlockAllocator` already had (§19.1) — `alloc` and `free`
  function pointers and a caller context — with one contract added:
  `alloc` returns ZEROED bytes, because a packed region carries node padding.
  A builder takes one (`<Name>Builder( allocator )`, defaulting to
  `TableDefaultAllocator()`, which is the hook pair) and everything the
  builder ever allocates goes through it: the arena's segments, `Lock`'s
  identity map and the packed region, the wire walks' numbering, and
  `LoadBuilder`'s node directory. `<Name>Measure` and `<Name>Save` over a
  REGION take the pair as an optional last argument, because the numbering
  walk behind them is the one reading-side path that allocates (§6.5). The
  numbering's entry array grows by copy, never by `realloc`: a game's heap is
  not required to have a resize primitive.
- **There is no log hook, because the runtime never logs.** Every outcome is
  a return value or a `TableReport` field; nothing writes to a stream.

**Held by test.** One translation unit (`test/tables/hooks_main.cpp`) defines
all four hooks before its first generated header and observes them: the
keyed refusal raises its assert and then its fatal, which escapes by
`longjmp` rather than ending the process; a counting `TableAllocator` handed
to a builder sees every allocation the pointer path makes, and the default
pair — defined there to a separate counter, which is where a bypassing
`malloc`, `calloc`, `realloc` or `free` would land — reads ZERO. The Go tests
scan a fixed, a keyed, a pointered and a block header for the `<c*>`
spellings, the STL headers, `std::`, `inline constexpr` and a raw C-library
call outside the hook blocks, and a negative control plants each spelling
and shows the scan go red.

**Measured, Apple clang 21 on arm64, one translation unit including one
header.** `KeyedTable.h`: 542 headers and 1,051,525 preprocessed bytes before;
126 headers and 172,428 bytes after, and 68 headers and 110,378 bytes with
`schema_assert` and `schema_fatal` supplied. `KeyedBlock.h`: 543 / 1,076,817
before, 136 / 203,913 after. `GraphTable.h` (pointered): 612 / 1,446,328
before, 384 / 898,010 after — what remains there is `<atomic>` and `<new>`.
A packet header (`Types.h`) is 36 headers and 30,856 bytes.

**Named follow-ons (§15).** `<atomic>` in a pointered unit's header — the
arena's per-slab atomics need a gcc/clang `__atomic_*` against MSVC
`_Interlocked*` shim, and that is a portability piece with its own tests;
`<new>` — the placement new is load-bearing for the object model and the
header swap is the game-engine `operator new( size_t, void * )` spelling; the
builder's destructor, which is the one RAII construct in the corpus and now
routes through the hook, so what is left of it is an ownership model rather
than an un-hookable call; and `inline constexpr` in the PACKET half, which is
the packet emitter's own change.

## 14. Design notes: the shapes rejected

One paragraph a rejected shape: what it was, and the reason it lost. The
designs of record are stated where they belong and are not restated here.

**The builder's storage**, against the owner's criterion — lockless if
possible, and minimize copying (§6.2):

1. **One buffer under a lock, grown by `realloc` — REJECTED.** A `realloc`
   moves the buffer under a worker that is mid-write. Offsets fix identity but
   not the raw references already resolved from them, and the corruption is
   invisible until much later.
2. **Sharded builders merged at `Lock` — REJECTED.** Each worker owns a
   private growable array and references are (worker, slot) handles.
   Contention-free, but it pays a full memcpy of every node at merge and a
   handle-to-offset resolution pass on top of it.
3. **Reserve-max with an atomic bump — NOT THE DEFAULT.** One buffer sized to
   a declared bound, allocation one atomic add, nothing ever resizes. It is
   the right answer for a caller who genuinely knows the bound, and it is kept
   as the documented alternative rather than the default because a general
   builder cannot require one.

**The node table's shape** (§3.1):

4. **A visited-index guard on load — REJECTED.** It is what an untyped node
   table forces: type the nodes by traversing, and bound the traversal with a
   set. That is state proportional to the graph on the READING path, which
   §6.5 forbids. The type id removes the walk instead of bounding it.
5. **An index-monotonic reference rule — REJECTED.** Requiring every reference
   to name a larger index would bound a traversal with no state at all, but it
   holds only under a TOPOLOGICAL numbering and pre-order is not one:
   `Scene { a *X, b *Y }` with `X.p` and `Y.p` naming one `P` numbers
   `X = 2, P = 3, Y = 4`, and `Y`'s reference names 3 from 4. Topological
   order also cannot be assigned in one descent — a node's position is known
   only once its whole subtree is.
6. **A hybrid: inline the singly-referenced pointee, table only the shared
   ones — REJECTED.** It reintroduces the exact problem the flat table
   removes: a 200-node chain is singly referenced at every link, so it would
   still nest 200 deep.
7. **A NEW KIND for the node table — REJECTED.** Skipping is defined only over
   §3's closed set, so a reader that does not have the kind cannot skip past
   it, and the node table must stay skippable by a reader that never heard of
   it. **Kind `14`, an array of tables — also rejected**: an array element's
   framing has room for a length and nothing else, so the type id would have
   to ride inside each body as a reserved FIELD, and every node body would
   carry a field no schema declared.
8. **A `u64` LENGTH on the reserved field, to lift the aggregate ceiling —
   REJECTED, because it breaks the very reader it exists for.** A skipper
   reads kind `12` as `L (u32)` then `L` bytes; given a `u64` it takes the low
   half, skips that far, and lands four bytes short of the payload's end,
   where it reads two payload bytes as the next field id. The whole point of
   riding under an existing kind is that an old reader frames the body and
   counts `unknown`.
9. **A reader-side acyclicity check — PRICED, NOT TAKEN.** Pre-order numbering
   gives every node a contiguous index interval, so with a `max index in
   subtree` on each record a cycle is an `O(1)` test per reference. It costs
   bytes a node plus a pass to prove the intervals are genuinely nested, for a
   property the writer already guarantees (§3.1). It is the shape the check
   would take if untrusted-input hardening ever demands one.
10. **Node-directory INDICES in place of self-relative deltas** — making the
    region and the wire one encoding — **REJECTED on the hot path.** A deref
    would become a directory load plus a base pointer where today it is one
    add (§6.3).

**The cooked check** (§7.4):

11. **A stateless traversal from the root — REJECTED.** Bounds and forwardness
    are not enough: a forged region can be a legal-looking DAG, forward and in
    range and aligned, and a walk with no memory re-visits shared subtrees
    exponentially (26 nodes, 312 ms; ~60 nodes, never).
12. **A traversal with a monotonic high-water mark — REJECTED, and it FAILS
    rather than merely costing.** The mark advances over region BYTES, not
    over entered NODES, so a forward reference may jump past a node the walk
    never enters; that node is then below the mark with its slots unchecked,
    and a later reference to it passes an "already visited" test that was
    never true of it. A 48-byte forgery is enough. Any repair keeps the
    traversal and adds state to make "below the mark" and "already entered"
    the same statement.

**The block form: the shapes it is not** (§19). The form adds no declaration
(§2.7), which is a strong claim, so the alternatives that would have added one
are priced here.

13. **A `section` CONSTRUCT** — a field spelling whose storage is an
    `(offset, count, stride)` triple, in a table with no wire because a triple
    has no wire kind — **REJECTED, and the argument for it is circular.** The
    triple is an artifact of ONE PROJECTION of a declaration; a wire form has
    no triple to ride, only a bounded array of fixed tables, which already
    rides as kind `14` with element kind `13` and the live count. With the
    wire objection gone the construct has nothing left to justify it.
14. **A bounded array BY VALUE, with the layout left to compile-time constants
    — REJECTED, and it is the near miss.** Two things defeat the plain form:
    the struct is the size of its maxima (§2.2) and cannot be read by value
    across a boundary, and the layout facts stay constants a consumer ASSUMES
    rather than reads, so every drift garbles silently and nothing on the
    boundary can say so.
15. **A pointer and a region (§2.1, §6.3) — REJECTED.** That is the
    variable-length class. The producer would pay a single-threaded compaction
    per frame and the consumer would need the region surface, which C# does
    not have and would not want at sixty hertz.
16. **The cooked form (§7), per frame — REJECTED, and for the same reason
    flatbuffers lost.** A cook is produced FROM A BUILDER by a single-threaded
    `Lock`, which is precisely the serialization point §12.1's bar refuses.
17. **The tolerant table wire, per frame — REJECTED with a number implied.**
    It is a parse and a copy on every frame on both sides; the abandoned
    flatbuffers build is that rejection already paid for once (§7).
18. **SELF-RELATIVE offsets inside a block — REJECTED**, which is a stated
    exception to §6.3. A blittable consumer reads the projection BY VALUE, a
    struct copy out of the mapped bytes, which a self-relative delta does not
    survive: the copy's address is not the original's. A block's consumer is
    handed the base, so block-relative costs it nothing and keeps relocation
    by `memcpy` intact.
19. **Compile-time block offsets from the maxima — REJECTED.** They would let
    workers run before the counts exist, which nothing in the case wants — the
    counts come from the same gather that produces the work — and every block
    would be near its maximum extent, which a boundary handoff that copies
    pays for on every frame.
20. **Deeper out-of-line rules, and per-field opt-ins — REJECTED** for the
    reason a keyword is: they put the answer somewhere other than the one line
    a reader is already looking at. The genuine cost is that a small array
    beside the strided ones in one root cannot stay inline; the answer is to
    wrap it in a nested type, whose arrays are at depth two and therefore
    inline (§19).
21. **A per-table MARKER selecting the form — REJECTED** once the question it
    answered was named (owner, §13.6: "KISS"): the marker bought cost control,
    not meaning, and cost is answered better by emitting the form on the side
    (§19), where a consumer that does not include it pays nothing and no
    declaration has to predict who will.
22. **A per-FIELD trigger, "any field carrying `| stride`" — REJECTED on its
    own evidence.** The case this form comes from has nine strided arrays and
    **zero** declared strides — every pitch there is `sizeof` — and a trigger
    the primary case never fires is not a trigger.
23. **A `| stride` attribute at all — CUT, not deferred** (§13.6). The one
    consumer that exists cannot read a strided array: it reads rows by casting
    the byte range to a row type, which requires pitch `== sizeof`, and it
    drops any array whose pitch differs. That consumer's fast path is the hard
    requirement the form exists to serve, so headroom trades it away for a
    property both generated sides already give. Nothing is held for it: no
    follow-on, no refusal by name, no reserved word.
24. **A tolerant second entry point, or a tolerant block id — REJECTED,
    because a block is same-build.** Both sides are generated from one
    declaration by one compiler run, so a consumer older than its producer is
    half a shipped build rather than a case to absorb. The id could not have
    been made tolerant in any case: a single number cannot be verified against
    a PREFIX of the facts that produced it, and any fold that ignored the
    difference would ignore a real break too.

**The view's shape** (§8), against the owner's picture of it — an editor that
inspects everything in the schema built:

25. **A SECOND descriptor vocabulary for types — REJECTED.** A `type` is not a
    table, so a type-shaped `TypeFieldInfo` beside `TableFieldInfo` looked
    tidy. It is two vocabularies, therefore two walkers, therefore every
    printer, differ and property grid written twice — and the JSON walker
    (§16) and the block form's reflective read (§19.2) are already written
    against the first one.
26. **A MARKER on the declaration plus a `--views` flag to select what is
    viewed — REJECTED, and this is the subtraction the design turns on.** It
    was the shape this section was first written in: `| view` on a type, a
    three-valued flag, a closure rule for what a marker reaches, a refusal for
    each declaration kind the marker does not fit, a reserved word taken out
    of the type-tag namespace to make the marker parse, and a gate proving the
    flag moved nothing. All of it existed to answer one question — what does
    this build pay for — that placement answers by itself (§8.4). Every one of
    those constructs went with the question.
27. **A view file per SCHEMA FILE — REJECTED.** It mirrors §6.1's
    one-file-per-schema-file layout and leaves the registry homeless: the
    registry is the set of everything the UNIT declares, so it would either
    live in one arbitrarily chosen file's output or force each file's view to
    reach into the others and back — the cross-file cycle §11 refuses.

## 15. Named follow-ons

- **A DECLARED COMPATIBLE SET of build versions for the cooked form.** §7
  loads a cooked file at exactly one build version. The owner's "or a subset
  of versions AT MOST" is the only widening ever contemplated: a build
  declaring the ids it will accept, so an asset format that rarely changes
  need not be re-cooked for a build whose facts did not move. It matters
  more under a UNIT-wide id than it would have under a narrower one, because
  a catalog is re-cooked when any table in its unit moves (§7).
  Everything about it is a decision — who declares the set, what proves two
  versions interchangeable — and none of that is decided here.
- **THE COOK's WRITE SIDE in every other language.** C++ writes a cook of
  either class today and its bytes are the tool's (§7.6). Every other
  language's writer is the same feature in that language's own idiom, held to
  the same `cook-write` surface — a language writes, the harness compares to
  the tool's bytes — and a pointered root's needs what the C++ one needed: the
  depth-first numbering (§3.1), the region layout (§7.2) and the identity map
  that writes a shared node once (§6.2).
- **A DATA-ONLY COOK from the runtime.** `schema cook --attribution` writes the
  node directory beside the file instead of inside it, so a build that ships no
  tooling carries just data (§7, §7.1). The generated `Cook` always writes the
  attribution part; the option is a parameter with no caller, and
  adding one changes a call site rather than a byte of the format.
- **A SIGNED COOK.** A cook is trusted input loaded from disk (§7), so nothing
  in the load path asks whether it is hostile. Where integrity IS wanted, it is
  a SIGNATURE over the whole file, verified ONCE before `Open`, and never
  per-node checks in the loader — which is the parse the form exists to delete.
  Owner: *"If we have concerns, then we should sign it."* / *"We just have to
  load the data."* The library is **sodium**, and the reason is the scale this
  form is built for: *"sodium is faster and we could be loading large data"* —
  verification is one hash pass over the whole file.

  **The cost is stated because it is the one thing a signature takes away.** A
  pass over the whole file at load forfeits the lazy paging §7 counts as a
  property of touching nothing at open: a mapped cook whose pages are read only
  as they are used becomes a cook read whole, once, before the first field. If
  that matters, the shape that keeps both is a SIGNED TABLE OF PER-CHUNK
  HASHES — the signature covers the table, and a page verifies as it faults in.
  Not built.
- **A FOREIGN-MEMORY overload for the Java accelerators.** `<Table>Block.open`
  and `<Root>Cook.open` read a `byte[]`, which stops at 2 GiB, so the Java reader
  cannot reach the scale §7 is built for. `MemorySegment` is the spelling that
  does, and it is not stable before JDK 22 where this backend compiles at
  `--release 17`; both `open`s already take a `long` length, which is the seat
  that overload takes when the floor moves.
- **Per-language backends beyond C, C++, C#, Go, Java, JavaScript and Rust**
  (the refusal in §11 names this).
  C# came first, because the dogfood's game engine reads the same config and
  asset bytes the C++ tools write (§12); Rust, Go, C and Java followed; and
  JavaScript is the first of the READING TIER — a backend with no struct
  layout at all, which is what proves the two accelerators can be READ by a
  language that could never produce one. The FIXED class is what those need:
  storage structs, measure/save/load over caller-owned buffers, the report,
  the reflection descriptors, `?T`, `[E]T`, name-hashed vocabularies and the
  text form (§16) — the variable class is still ahead of it. A port mirrors
  this document and invents no contract of its own; where a language forces a
  shape — a pseudo-union for a language with no native union — the ladder
  above already says what is licensed.
- **The JavaScript VARIABLE class on the WIRE** — the arena as a growable
  buffer reserved once, references as node indices, and the flat node table
  (kind 17). Refused by name today, on the same terms as C#'s: a pointered
  unit gets no `<Base>Table.js`, and both accelerators are emitted all the
  same, because neither needs a codec.
- **The C# VARIABLE class on the WIRE** — the arena, the region, the builder
  and the node-table codec, on top of the fixed class C# carries today (§11).
  The cooked form is NOT in it: a cook is pointed at, not parsed, so
  `<Base>Cook.cs` and `<Root>Cook.Open` are emitted for a pointered unit
  already (§7, §11). The text form is not in it either — §16 lands in C# with
  the fixed class, and a pointered unit has no text form for the same reason
  it has no wire codec.
- **The variable class in a ported backend** — the arena, the region, the
  cooked form and the pointer surface — after that port's fixed class.
- **THE VARIABLE CLASS's TEXT FORM in a ported backend** (§16.7), with its
  wire: the pointer adapters and the graph half beside the port's walk, the
  builder-form `FromJson`, the region-form `ToJson`, and the three corpus texts
  plus the hostile rows the C++ leg answers today. schema#349's row beside the
  wire.
- **`ToJson` FROM AN UNLOCKED BUILDER.** §16.7 writes from a region's const
  root, so a builder writes its text after `Lock`; a write over the arena
  would resolve slots through the arena context the wire's `Save( builder )`
  already carries, and nothing in the form changes.
- **THE EXPANDED TREE of a variable root** (§17.2). A variable root packs and
  unpacks as one `<Root>.json`, because labels are a text's own. A tree of
  `<field>.json` files that shared nodes across its files would need the pack
  to number across the tree in an order the files fix, which is the tree
  becoming a format; nothing here decides whether that is wanted.
- **THE DEPTH CAP AGAINST A CHAIN'S LENGTH** (§16.7). A pointer chain nests in
  the text as deep as it is long, and §16.2's cap of 128 bounds it in every
  walk, so the corpus's 260-node chain, `graph_deep`, has a wire and no text.
  Raising the cap is one
  number in six walks and the hostile row that pins it; what the number should
  be is a question about every port's stack, and it is the owner's.
- **`Lock`'S IDENTITY MAP, PRESIZED.** The map (§6.2) grows by rehashing, and
  rehashing is what it spends: on a 131,071-node tree with NO sharing at all —
  where the map is pure overhead and buys nothing — `Lock` costs 5.8 ms against
  0.98 ms before the map existed. Quadrupling the capacity instead of doubling
  it took 1.35x of that, and a map presized to the graph measured 6.6 ms against
  12.0 ms on the doubling schedule, so the headroom left is real. What it needs
  is a NODE COUNT the arena does not keep, because counting allocations costs
  an atomic per node and §6.4 refuses one; the shapes are a per-worker
  non-atomic counter summed at `Lock`, or a bound derived from the arena's
  high-water mark and the closure's smallest node, and neither is decided here.
  **What the map buys where it buys anything is not close**: a 24-node graph
  whose every node is named twice packed 671 MB in 189 ms without it and 1,136
  bytes in 0.009 ms with it.
- **`Lock`'S NODE DIRECTORY.** §6.3's trailer is what a `Load`ed region carries
  and a `Lock`ed one does not, so the two agree on the DATA and differ in what
  sits beside it. Filling it costs `Lock` nothing it is not already doing — the
  pack knows every node's offset and the numbering already holds the order — and
  what it buys is a check a tool can run over a locked region as it runs one
  over a cook (§7.4). `Cook` does not wait on it: handed a locked region, it
  re-derives the numbering from the graph, as the wire's `Measure` and `Save`
  over a region do (§7.6).
- **THE NUMBERING WALK, ITERATIVE.** The walk recurses once per POINTER EDGE on
  the authoring side (§3.1), so a graph deeper than the C stack is a build-time
  crash rather than a refusal. The wire has no such bound — a chain's length is
  not a depth and LOAD is a scan — and a builder's graph is a build's own data
  and not hostile input, which is why it is priced here rather than fixed now.
  What it needs is a resumable per-type field cursor, because the ORDER is
  first-visit depth-first pre-order and a breadth-first walk would number a
  different graph.
- **`?` on an array, a string or `bytes`** (§2.3): a presence bit beside an
  existing count or length wants a decision about what the pair means before
  it becomes wire. Wrap the field in a table and make that optional today.
- **An array of `?T`** — the same question one level down: an element's
  presence bit beside the array's own count.
- **AN UNBOUNDED BYTE BUFFER — `*bytes`, and `*string` beside it** (§2.5),
  tracked as schema#259. Neither spelling parses today. The case for it is
  the owner's and is two sentences: *"yes, i like byte buffer as primitive"*
  / *"since it can be nulled."* — a buffer that is a REFERENCE has an absent
  state a bounded `bytes(N)` has to spend a field to express; and
  *"instantly, it turns a table into variable"*, which is the price, because
  one such field flips its holder's whole closure to the variable class
  (§2.2) and with it the arena, the region and the lifecycle. What it buys is
  size: in a table of a million nodes a `bytes(65536)` field costs 64 KB a
  node whether it is used or not, while a reference costs four bytes plus
  what each node actually holds — the same argument as a sub-document stored
  as its own wire bytes and decoded only when something asks for it. What it
  needs decided is the framing it rides under and the elision at the empty
  end, since a null buffer and a zero-length one are different values.
- **AN ARRAY OF POINTERS** — `[..N]*T` and `[N]*T`. It is refused by name
  today (§11), and the diagnostic carries the two spellings that serve
  instead: *"declare a
  bounded array of tables by value, or a pointer to a table that holds the
  array"*. It is the spelling a node with a fixed fan-out wants, and it costs
  four bytes a slot where a by-value array costs a whole table. What it needs
  decided first is the wire: an array whose ELEMENT kind is the pointer index
  `17` (§3.1) is one shape, and it wants the null element, the all-null
  elision and the element-kind separation from an array of `uint32` settled
  together rather than one at a time.
- **Cross-endian COOKING**: producing a cook for a target whose byte order
  is not the cooking machine's, by swapping as the region is written. The
  ENDIAN FIX-UP ITSELF IS NOT DEFERRED — it is part of the cook and §7 states
  it — and what is deferred is producing one for a FOREIGN target. The swap
  belongs at cook time because a cook is made for a known target (§7), and
  the descriptors already carry the kinds and offsets it would need. `Open`
  never swaps: a recorded order that is not this build's means the file is
  not this build's, and it refuses. A big-endian target is held by an
  emulated CI leg rather than by argument (schema#303).

- **A hash-guarded fallback loader** — open the cooked form, else load
  the wire — as a convenience helper.
- **A SHARED BOUND across several out-of-line arrays.** `BlockMaxBytes` sums
  each array's declared maximum, and several arrays commonly draw from one
  pool — so the sum is loose by construction and reserves extent that can
  never be occupied at once. It is affordable in the case at hand and stated
  in §19.1 rather than hidden; a way to declare "these arrays share a bound
  of N" is what a tighter case would want, and nothing about it is decided
  here.
- **DEPTH past one in the block form** (§2.7) — an out-of-line array inside a
  row type. It wants a decision about whose base a nested projection's
  offsets are relative to before it is anything, and depth one is what the
  case in hand needs. **A block-form table used as another table's row type
  is not part of this question: it is permitted today** (§11), and depth one
  already answers it — its own arrays stay INLINE there, so such a row is a
  by-value struct the size of its maxima (§2.2) and is unremarkable except
  for that size. Making those arrays out-of-line is what this follow-on
  would decide.
- **A WIRE LOAD STRAIGHT INTO THE PROJECTION.** Today `Load` fills the
  by-value struct (§2.2's price), and a consumer that wants a block must then
  build one. A load path that decodes a wire directly into a block's storage
  would let a tool read a file and hand a block across the boundary without
  materialising the large struct at all. It is a second decoder, and nothing
  about it is decided here.
- **A block's own TEXT FORM** (§16). A block-form table has a wire and
  therefore already has `ToJson`/`FromJson` over its by-value form; whether a
  BLOCK in hand should be textualisable without first loading it by value is
  the open part, and it is a convenience rather than a gap.
- **Cross-endian blocks.** A block carries its byte order in its magic and
  refuses a foreign one, exactly as a cook does; swapping one would be the
  cook's cross-endian follow-on applied to a flatter shape.
- **A generic dump/diff tool over the reflection surface** — and the unit
  registry (§8.3) is what makes it a tool over a whole BUILD rather than one
  per table: walk the registry, print or compare every declaration, with the
  instance-level walk of §8.1 underneath it. The listing §8.7's gate already
  produces is the dump half of it in embryo.
- **THE SOURCE GOLDENS EXTENDED TO THE TABLE CORPORA.** §8.7's independence
  gate compares against a recorded snapshot of generated text, and the
  snapshot covers the `examples` and `examples128` units only — nothing
  records what the two table corpora emit, so for exactly the units whose
  table headers the view's boundary runs through, the gate has nothing to
  compare. It is the same harness pointed at two more directories, and it
  belongs with the emitters rather than with this page, because a snapshot
  taken before the code that could move it is a snapshot of nothing.
- **The view in a ported backend** — C++ and C# take it together (§8); every
  other backend emits no view file until it emits the same registry against the
  same pin (§8.7). Nothing is refused meanwhile, because nothing in a schema
  asks for one (§8.4): a backend without the emitter is a backend whose
  users have no registry, and the status paragraph says so.
- **DOC STRINGS in the view.** Doc comments are deferred with their design
  pinned (SPEC §4.1, §9 q5); when they land they become one more descriptor
  column and one more registry column, and nothing else about §8 moves. No
  competing `| doc = "..."` attribute is introduced ahead of them.
- **A view over the PACKET wire's own layout** — bit offsets, field widths
  and the compressed-float parameters a `type` rides under (SPEC §6.1),
  beside the declaration facts §8 carries. It is what a packet inspector
  would want and what a table-shaped descriptor cannot express.
- **THE TYPED DESCRIPTOR FORM for the kind-0 set** (§8.2), which is what
  would turn describing a `fixed`, `ufixed` or 128-bit field into reading
  and writing one. Three things it needs, and none is decided here: the
  numeric SHAPE as columns rather than as text — signedness, `I`, `F` and
  the storage width, so a walker decodes the bytes without parsing
  `type_name`; BOUNDS that survive the value — the range columns are
  `double`s, and a 128-bit bound is not representable in one, so the pair
  either widens or grows a second form; and a decision about whether the
  same columns serve the packet wire's own kinds, which is the follow-on
  above. Until it lands, §8.2's rule stands as written: kind 0 describes,
  and does not decode.
- **BUILD IDENTITY in the registry** (§8.6). It carries the unit's package
  and protocol id and nothing else about the build that produced it; what
  else identifies a build — a compiler version, a build stamp — is settled
  where build versioning is settled, and the registry gains a column then
  rather than inventing one first.
- Keyed lookup conveniences over loaded collections (library-side, never
  stored semantics).
- Arrays of unions in table bodies.
- `fixed` and 128-bit table-wire kinds, if a need ever materializes.
- **THE REST OF THE C++ DIALECT** (§13.9), four pieces, each its own change:
  `<atomic>` out of a pointered unit's header, behind a `__atomic_*` /
  `_Interlocked*` shim the msvc and big-endian legs prove; `<new>` out of the
  same header, behind the `operator new( size_t, void * )` spelling; an
  explicit `<Name>BuilderShutdown` in place of the builder's destructor,
  which is the shape the C column's `<name>_builder_shutdown` has already; and
  `static const` for the packet half's `inline constexpr` constants.
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
the definition of them. The C# backend holds the same walk under the shape
its language forces, the compiler's own packer (§17) implements the same
rules over the same IR, and goldens are what make the implementations one
form: for every instance in the corpus, every implementation's text is
byte-identical and every implementation's read of that text produces the
same wire bytes.

**Where a backend's spelling is FORCED, the rule does not move — the
spelling does, and the site says so.** C# is the worked example, and it
forces exactly four: storage is reached through the descriptor's accessors
rather than an offset and a width, because a C# field has no address (§8.1);
number conversion goes through the invariant culture, so the walk consults
no locale where C++ must; a string is written from two sources, a
descriptor's key and a `string(N)`'s bytes, where C++ has one pointer type
for both; and the reader is a plain cursor beside the span rather than one
`ref struct` over it, because C# refuses to hand a stack buffer to a method
that also takes a `ref struct` by reference and the walk needs a buffer for
every key it compares. None of the four changes a byte of any text.

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

The same three, in C#:

```csharp
TableReport report = new TableReport();
ShipConfig ship = new ShipConfig();
if (!Schema.ShipConfigFromJson(ship, text, report))
{
    // the text is not JSON (report.Malformed) — the instance holds what
    // was placed before the stop
}

long size = Schema.ShipConfigToJsonMeasure(ship);   // exact bytes, writes nothing
byte[] buffer = new byte[size];
Schema.ShipConfigToJson(ship, buffer);              // returns size; -1 = refused
```

**`text` is a `ReadOnlySpan<byte>` and `buffer` a `Span<byte>`**, so the
caller owns both and the read path allocates nothing beyond the instance —
a string lands in the field's own `byte[N]` storage, and every key, name and
number token is compared and converted in a stack buffer. C# has no free
functions, so all three are members of the unit's `Schema` class (§6.1's
naming), and the capacity C++ passes as an argument is the span's own
length. **A unit's C# files compile into ONE assembly**, so the walk is
emitted once per UNIT — into the file that already carries the unit's shared
table runtime — rather than once per translation unit behind a guard; there
is no second file to compile and no build decision to make, and a consumer
that never calls the form pays the assembly's share of one walker.

A VARIABLE-LENGTH table reads through its builder, because that is where
its storage comes from (§6.5):

```cpp
SceneFromJson( builder, text, text_bytes, &report );
```

**Backend status for this section: the FIXED class in C++, C, C#, Go, Rust,
Java and Elixir; the VARIABLE class in C++ (§16.7).** A pointered unit's text
form is the C++ reference's, through the builder, and carrying it to the other
backends is schema#349's row beside the wire. In C#, Go, Rust, Java and Elixir
the absence is already made one level up: a pointered unit gets no table source
at all (§11), so it has no text form for the same reason it has no wire codec;
the C port has the wire's earlier form (§3.1) and its text form follows it.

**The FLOAT SPELLING is C's `%.*g`, byte for byte, in every port**, and each
says how it gets there rather than reaching for its runtime's default. C++
calls `snprintf`; C# builds a custom format string and converts under the
invariant culture; Java rounds the double's EXACT decimal expansion through
`BigDecimal` at HALF_EVEN, because `java.util.Formatter` rounds HALF_UP and the
two answers differ on a tie; JavaScript does the same in BigInt, because
`toExponential` and `toFixed` break a tie by MAGNITUDE. A tie is discarded by
the shortest-round-trip loop at every precision but the last — and at the last
there is no loop left to save it, which is why the mode is named rather than
left to the default.

**The JavaScript walk is NOT `JSON.parse`/`JSON.stringify`, and could not be.**
This section's clamping, counting, duplicate-key and trailing-comma rules ARE
the form, and the output must be byte-identical to the goldens down to a float's
`%.*g` spelling — so the port spells the C conversion out, as the Rust one does.
**Both ends of that conversion bit in JavaScript, and both are worth recording,
because the next port meets them too.**

**THE TOOL TAKES BOTH DIRECTIONS.** `schema pack` and `schema unpack` take a
variable root as one text (§16.7, §17.2), and the round trip is the gate: a
text that lost a pointer, or a node's identity, cannot pack back to the bytes
it came from.

**Rust's walk allocates nothing, and that is a gate rather than a claim**:
numbers format through a stack sink the size of the C++ walker's own
`char[64]`, strings and keys land in the field's own storage, and
`make tables-rust-alloc-audit` counts allocations at the global allocator over
every read and write path of every instance in the conformance corpus. It
reads zero. The instrument matters as much as the number: an earlier soak
gated on LIVE BYTES, which answers "does this leak", and a formatter that
allocated and freed the same bytes every iteration read +0 there for an hour.
The count is what the claim is about.

**ELIXIR'S WALK CANNOT CLAIM ZERO AND DOES NOT**, and the same instrument is
what it holds instead. The BEAM owns allocation: there is no caller-owned
buffer for a text to be written into and no mutable struct for a read to fill,
so a decoded value IS an allocation. `make tables-elixir-alloc-audit` counts
what ONE iteration costs on three counters the BEAM keeps — heap words per
PROCESS, through `:erlang.trace/3` on `:garbage_collection` summed over the
loop's own collections; refc-binary allocations, through `binary_alloc`'s own
call counter in `:erlang.system_info/1`, which is the payload the heap count
cannot see; and reductions — and gates each case against a PINNED budget,
re-pinned deliberately the way a wire golden is, with
`make tables-elixir-alloc-negative-control` sabotaging the emitter so every
generated load allocates sixteen refc binaries and a thousand-cell list more
and requiring both memory columns of every case to red. A number that cannot go red is not a gate, and a
claim a language cannot make is better stated than approximated. The readers'
fuzz oracle beside it, `make tables-elixir-fuzz`, takes `SEED=` and
`ELIXIR_FUZZ_N=` as every other leg's does and prints the seed it ran, so a
find reproduces from its own output; its negative control removes both extent
bounds from the emitted block reader, because removing one leaves the oracle
green under some seeds and a control whose verdict depends on the seed is not a
control.

**Elixir's float spelling is C's, digit for digit, and its READ is a single
correctly-rounded conversion at the field's own width.** Erlang has no `strtof`
and converting a decimal through a double rounds TWICE — the two roundings part
company between `FLT_MAX` and the float32 rounding midpoint, and again at the
subnormal end — so the walk converts over exact integers instead. `%.*g` is
reproduced through Erlang's own scientific and fixed formatters, because the
goldens are its bytes.

**Rust's walk is a UNIT's, on the same terms C#'s is**, and for the same
reason: a unit is one crate, so a second copy would be a duplicate definition
rather than C++'s harmless re-inclusion behind a guard. It lives in
`table_runtime.rs` and `make tables-rust-walk` is the gate — the walker's
source, byte-identical across every unit of the corpus, with nothing
normalised away but the generated banner. Where C++ must cross the runtime's
DECIMAL POINT twice, Rust's float formatting and parsing are locale-free and
the walk consults no locale at all; C's `%.*g` is reproduced digit for digit
so the goldens are the same bytes. Storage is reached the way C++ reaches it —
an offset and a width, because a `#[repr(C)]` Rust record has both — with one
exception: a UNION's payload address comes out of a match rather than off an
offset, since Rust spells a union as a real enum, so the union descriptor
carries three accessors where C++ carries a tag offset and an arm offset.


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
- **THE CANONICAL TEXT ENDS WITH EXACTLY ONE NEWLINE.** Every writer emits
  it — `ToJson` in every backend, and `schema unpack` — and every reader
  ACCEPTS a text with or without one, because the trailing whitespace a read
  already skips is what makes the two the same text. The byte belongs to the
  FORM rather than to a file convention: a text is written to a file, pasted
  into a diff, piped between tools and handed back, and it has to be one text
  in all four places. A buffer whose last byte is `}` is the one shape that is
  not — it is the shape that makes `unpack` and `ToJson` disagree by a byte,
  and it forced every comparison between them to strip before comparing,
  which is a concession each new port would have had to rediscover. The
  goldens carry the byte; a text carrying two newlines, or none, is a text a
  reader still accepts and a writer never produces.

### 16.2 The mapping, field kind by field kind

The JSON form of a table is an object whose keys are field KEYS — a
field's name, or the `json` attribute's value where one is declared (§16.4).
Per kind:

| declaration | JSON | notes |
|---|---|---|
| integers, `bits(N)` | number | see **Numbers** below; a `bits(N)` value over its implied `[0, 2^N − 1]` clamps and counts |
| `float32`, `float64`, compressed floats | number | a value a float32 field cannot hold is `kind_mismatch`, never stored as infinity |
| `bool` | `true` / `false` | |
| `string(N)` | string | longer than N is CLAMPED to N bytes at a code point boundary, counted |
| `bytes(N)` | string, base64 | standard alphabet, PADDED on write; padded and unpadded both read. Longer than N is clamped, counted |
| enum | string, the variant NAME | `"Silver"`; `None` writes as `"None"`; an unknown name → None, counted |
| flags | array of variant names | `["Shielded", "Turbo"]`; an empty mask writes as `[]`; an unknown name is skipped, counted |
| `[N]T` fixed array | array | fewer elements pad with defaults; more are dropped, counted |
| `[..N]T` bounded array | array | count = length; more than N are dropped, counted |
| `[E]T` enum-keyed array | object keyed by VARIANT NAME | `{ "Fighter": {...}, "Bomber": {...} }`; an absent key keeps that slot's defaults; a **repeated variant key is last-wins and counted**, as any duplicate key is; an unknown key is skipped and counted, and **`"None"` is such a key** — it names no slot (§2.4) |
| nested `type` / `table` | object | the same walk, recursively |
| `?T` optional | the value, or the key absent | **presence of the KEY is presence**: a key present sets the field present, whatever its value; an absent key leaves it absent. `ToJson` writes present optionals only |
| union | object with ONE key, the arm name | `{ "buff": { "multiplier": 2.0 } }`; `None` writes as `{}`; `{}` or absent reads as None; two keys is malformed. A `table` arm (§2.6) is the same object form |
| pointer `*T` | object, or `null` | the pointee's object in place; `null` is a null pointer. A node named MORE THAN ONCE is defined once under `&node`, with its fields, and named by `&node` alone after — §16.7's one construct, and the only thing this form adds for the variable class |

**One entry above describes a construct no declaration reaches yet**: the
`table` union arm named in the union row lands with its own (schema#258). It
is stated here rather than added later because a text mapping is a property
of the KIND — it lands as its declaration lands, not as a second decision
about text. The `*T` pointer row is the variable class's, covered by the
status in §16.1.

**The text form writes a graph as a tree, and `&node` is what keeps identity
across the seam** (§16.7). A pointee is written in place, so without the
construct a node several parents name would be written once per parent and read
back as that many nodes; with it, the second and later occurrences are
references and the identity §3.1 preserves on the wire survives the round trip.
The writer carries its own visit bound — a cyclic structure is refused at save
(§3.1) but expressible in a region, and writing one in place would not
terminate — and it carries this section's depth cap, because a pointer chain
nests as deep as it is long.

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
- **Negative control, per backend, on the same arithmetic.** With the
  walker's offset arithmetic sabotaged by one field, the round trip goes red
  on the first table that has two fields. A backend whose fields have no
  offset sabotages what stands in for one — in C#, the FIELD INDEX the read
  path looks a descriptor up by, so one key's value lands in its
  neighbour's — and the control's second half is what makes it a control:
  the conformance matrix must go red on `json-read` and stay green
  everywhere else, `json-write` included, which is what says the break is
  the READER's.
- The one-walk gate: within a backend that holds the form as a single
  walker, that walker's source is identical in every unit of the corpus —
  in every generated `.cpp` where the walk is a translation unit, and in the
  one file per unit that carries it where an assembly makes a second copy a
  duplicate definition.
- **The conformance matrix.** Every registered backend answers `json-read`
  and `json-write` over the same data: the text a backend writes is compared
  against the text the harness holds, and the wire it reads back out of that
  text against the pinned wire. A backend without the form prints ABSENT
  rather than passing vacuously, which is the distinction that keeps a
  missing feature and a failing test apart.

### 16.6 What this is not

Not a file format, not a directory layout, not a manifest, not an envelope.
Those are a packer's business; §17 is one packer, and it is a TOOL over
this section rather than a second definition of it.

### 16.7 The variable class: the same text, and one construct for sharing

**The FIXED form above is the whole of this form too**: the JSON syntax for
fixed and variable tables is the same where possible and completely consistent
otherwise. So nothing in §16.2's table changes, a fixed table's text does not move a byte, and a
pointered table whose graph is a TREE reads and writes exactly as if every
pointer were a nested table. Sharing — the one fact a tree cannot say — gets
**exactly one construct**, spelled the same way at every site. Nothing else is
added.

**One declaration, and the two texts it produces.** §3.1's own example:

```
table Palette { id int32 }
table Node    { value int32  next *Node  palette *Palette }
table Scene   { name string(16)  head *Node  palette *Palette }
```

A TREE — `scene.head = A`, `A.next = B`, and each of the three naming a
`Palette` of its own — is §16.2's pointer row and no more: the pointee's object
in place, `null` for a null pointer, pretty-printed as every object is.

```json
{
  "name": "level1",
  "head": {
    "value": 1,
    "next": {
      "value": 2,
      "next": null,
      "palette": {
        "id": 8
      }
    },
    "palette": {
      "id": 7
    }
  },
  "palette": {
    "id": 6
  }
}
```

**A reader who has only ever seen a fixed table's text can read that**, and
that is the point of the ruling: a pointer looks like a nesting, because in a
tree it is one.

Now the SAME scene with one `Palette` P shared by all three. The writer reaches
P while descending B — before `A.palette` is read, because the walk is
depth-first, exactly as §3.1 numbers it — so P is DEFINED there, `&node` first
and its fields after, and NAMED twice after by `&node` alone:

```json
{
  "name": "level1",
  "head": {
    "value": 1,
    "next": {
      "value": 2,
      "next": null,
      "palette": {
        "&node": 1,
        "id": 6
      }
    },
    "palette": {
      "&node": 1
    }
  },
  "palette": {
    "&node": 1
  }
}
```

**`&node` IS THE ONE CONSTRUCT, and it is a LABEL: put on a node where it is
written, and referred to elsewhere by the same mark.** A DEFINITION is the
node's object with `"&node": N` as its FIRST key and the node's fields after
it; a REFERENCE is an object holding `"&node": N` and nothing else. One
spelling at both sites, and what follows the label says which half it is — so
the rules below are what keep a typo loud: a label alone that the text has not
defined is refused rather than read as a default node, a field after a label
the text has already defined is refused rather than read as a second node, and
labels run from `1` in first-write order, so a stray number in a hand-edited
text is most often one never defined. The residual is stated plainly: a typo
that lands on a label already defined, of the right type, is an ordinary wrong
number, as any wrong number in JSON is. A node named once carries no label,
which is why the tree above is untouched.

**What the label is.** It is the text's counterpart of the wire's node index
(§3.1): where the wire numbers every node it writes and carries a pointer as an
index into that numbering, the text nests a node where it is named and needs
a number only where nesting cannot say it — at the second and later slots that
hold one node. So a label is written only where a node is shared, it is the
text's own — not the wire's index, not anything a schema declares, and
meaningless outside the document that carries it — and a tree writes none at
all.

**THE CONSTRUCT IS ONE A FIXED TABLE'S TEXT CAN NEVER PRODUCE, and that is a
requirement and not an accident.** The `&` prefix is refused to every `json` key
at compile time (§11) — the whole prefix, not the one spelling, so this form
keeps room to grow without a later construct colliding with a field a schema
already declares. No fixed table has a key that begins with one and no writer
of the fixed form can emit one, so a reader never has to guess whether an `&`
key it meets is sharing or a field it does not know: it cannot be a field. **A
`&`-prefixed key among the keys a reader PLACES, where it does not expect one,
is `malformed`, refused and counted — never `unknown`.** `unknown` means "a
field this build does not have", which is a difference §4 exists to survive;
this is "a construct this build cannot honor", and honoring it silently would
read a shared graph as a tree with an empty object where every later reference
stood. A FIXED reader handed a variable table's text refuses on the first `&node`
it would have placed and says so, which is the whole point of spending the
prefix. **What it does not place it does not police**: a value it skips — an
unknown key's, a wrong shape's — is skipped whole with the prefix inside it,
because that is §4's tolerance, and a newer schema's pointer field arriving at
an older reader is exactly the unknown key §4 exists to survive.

**The rules, in full.**

- **The label is a positive integer, spelled as one**: digits, no sign, no
  fraction, no exponent, no leading zero. `1`, never `1.0` or `01`. A writer
  numbers the nodes it shares from `1` in the order it first writes them, and
  a reader takes whatever it is given.
- **`&node` is the FIRST key of a pointer's object.** After a field it is
  `malformed`.
- **A label the text has not defined, followed by the node's fields, DEFINES
  it.** A definition carries at least one field: a label alone that the text
  has not defined is `malformed` — never a default node — and a shared node
  with nothing to write has no definition this form can spell, so the writer
  refuses it as it refuses any value it cannot spell (§16.3).
- **A label the text has defined, alone, REFERS to it.** A field after it is
  a second definition and `malformed`. **Definition before reference, in
  document order** is the whole ordering rule, and it costs nothing: a data
  cycle is refused at save (§3.1), so a writer always reaches a node before
  anything that shares it. **A label is defined when its object CLOSES**, so
  a bare `{ "&node": N }` met inside N's own definition — at any depth of
  by-value nesting — is `malformed`, exactly as a reference before its
  definition is: it names a node whose descent is still open, which is the
  cycle the wire refuses (§3.1), and the text refuses it where it is written
  rather than later, at `Lock` or `Save`.
- **A reference resolves against the labeled node's TABLE.** A `*Node` slot
  naming a label that defined a `Palette` is `kind_mismatch`, the pointer null —
  the wire's rule for a record of another type (§3.1).
- **A DROPPED DEFINITION KEEPS ITS LABEL.** §16.2 drops an array element past its
  bound and counts it, skips an unknown key's value and counts it, skips a value
  of the wrong shape and counts it. A definition inside a dropped value still
  takes its label — with no node — so a reference to it reads NULL, and nothing
  more is counted, because the drop was counted where it happened. It is the
  wire's rule for a node a reader cannot name (§3.1): the index stays, every
  pointer naming it reads null, one event.
- **Anywhere else, the prefix is `malformed`**: at the ROOT, which takes no
  label because nothing may name it (a reference to the root is a reference to a node
  whose descent is open, which is the cycle §3.1 refuses); in a by-value
  nesting, a union arm, an array element, which are values and not nodes; under
  any spelling but `&node`.
- **`null` is unchanged**: it is a null pointer, as §16.2 says, and it never
  carries a label.
- **A writer emits the construct only where it must** — for a node it will name
  more than once — so the text of any tree-shaped graph is byte-identical to the
  text the same data would have as by-value nesting. That is the ruling's "same
  where possible", made mechanical.
- **A CHAIN NESTS AS DEEP AS IT IS LONG.** The wire indexes where the text
  nests (§3.1), so a pointer chain of N nodes is N levels of nesting, and
  §16.2's depth cap of 128 bounds it exactly as it bounds a by-value nesting
  — the WRITER carries the reader's cap and refuses a graph past it, so it
  never produces a text the reader would refuse. A graph deeper than the cap
  has a wire and no text: the conformance corpus's `graph_deep`, a 260-node
  chain, is that graph, its wire-only instance, marked `no-text` on its own
  line. It is the one thing this form does not carry
  of the variable class, and the cap is the fixed form's, held as the fixed
  form holds it (§15 names the ruling that would move it).

**What a reader builds.** The variable class is not held by value (§6.2), so
its text form reads through the BUILDER, and writes from the CONST root of a
region — what `Lock` and `Load` both produce. The surface is §16.1's with the
class's own forms in the first argument:

```cpp
SceneBuilder builder;
TableReport report;
if ( !SceneFromJson( builder, text, text_bytes, &report ) )
{
    // the text is not JSON, or its labels do not resolve (report.malformed) —
    // the builder holds what was placed before the stop
}
builder.Lock();

int64_t size = SceneToJsonMeasure( builder.AsConst() );
SceneToJson( builder.AsConst(), buffer, size );
```

`FromJson` reads into the builder's ROOT and allocates every node the text
names through the field's own `<T>Emplace`, so a node the builder already held
is left in the arena unreferenced; `ToJson` takes a const root because a text
is written from a structure that is finished, and a builder writes its text
after `Lock`. Reading a shared node places ONE node behind every reference —
the two slots hold one arena offset — which is the identity §3.1 preserves on
the wire, surviving the seam.

**What the writer refuses** (§16.3's list, extended by one): a graph the WIRE
refuses is refused here for the same reason — a data cycle, met as a reference
to a node whose descent is still open, the ROOT's included — and a graph past
the depth cap, above. There is no text-only refusal beyond that, because there
is no text-only construct.

**What allocates.** Every node comes from the builder's arena, through the
same `Alloc` a program uses (§6.2). Beside it, each direction carries ONE map
on the authoring side, on the terms §6.2 sets for `Lock`'s: reading, the
label map — a label to the node it defined; writing, the identity map — a node
to how many slots name it and the label it took. Both are proportional to NODES,
never to bytes, released before the call returns, and allocated through the
caller's pair (§6.5): the reader's through the builder's, the writer's through
the `TableAllocator` handed to `ToJson`, defaulted to the program's pair
exactly as `Measure` and `Save` default it. Writing is TWO passes
over one walk — the first counts how many slots name each node and refuses a
cycle, the second writes — because a node's first occurrence has to know
whether it will be named again; `ToJsonMeasure` runs both, as `ToJson` does,
so measure and write agree byte for byte.

**Still ONE walk.** The generic walk of §16.1 places every kind but the pointer
by itself, and meets a pointer through three adapters it declares and does not
define — one reads a pointer's object into a slot, one writes the node a slot
names, one takes an `&`-prefixed key the walk is skipping. A unit that declares
no pointer defines them as stubs no field ever reaches, so its walk is
byte-identical to every other unit's (`make tables-json-walk`); a pointered
unit defines them in a GRAPH HALF that follows the walk in its `.cpp`, and that
half is byte-identical across the pointered units of the corpus and reaches no
pointer-free one (`make tables-json-graph-walk`) — the zero-cost property
(§2.2), holding for the text form.

**`schema pack` and `schema unpack` take a variable root, as ONE text** (§17.2).
Labels are a text's own, so a tree of `<field>.json` files — the expanded shape —
would split a root across texts that cannot name each other's nodes: `unpack`
writes `<Root>.json` for a variable root whichever shape is asked for, and
`pack` refuses a tree of fields under one by name, before a file is read. The
round trip is the gate: a text that lost a pointer, or a node's identity,
cannot pack back to the bytes it came from.

**Held by test.**

- Every pointered instance the conformance corpus carries a text for
  round-trips `ToJson` → `FromJson` → `Save` and byte-matches the wire it came
  from — the fixed class's gate (§16.5) over the variable class's instances —
  and the shared-node instance is the one that cannot pass it by writing a
  tree: four nodes held by seven slots come back as four records, or the
  bytes differ — and the test asserts those counts.
- **A PINNED TEXT** of the shared graph, against a literal: the construct
  spelled at every site — `&node` at the two definitions and at the three
  references, five in all, asserted — and nowhere in the tree beside them. Reader and writer share
  the spelling, so a round trip could not see it wrong.
- The compiler's engine (§17.1) writes the same bytes: `harness generate`
  packs each text back to the wire it came from before it lands, and the C++
  leg's `json-write` is compared against that text byte for byte.
- A cycle is refused by `ToJsonMeasure` as it is by `Measure`, on a region
  that holds one — `Lock` refuses to make one, so the test makes it by hand.
- A hostile battery over the construct's own edges, in the corpus every leg
  runs (§17.5): a label alone the text never defined, a reference before its
  definition, a label defined twice, `&node` after a field, `&node` on the
  root, `&node` in a by-value nesting, a label that is not an integer spelled
  as one, a label of zero — each refused; a reference to a node of another table, a definition
  past an array's bound, a definition under an unknown key — each counted the
  way the page says, with the bytes both engines then write compared byte for
  byte; and a shared node read back as one.
- **The one that proves the prefix was worth spending**: a FIXED table's
  `FromJson` handed a text carrying `&node` among the keys it places refuses
  with `malformed`, and handed one inside a value it skips counts the skip and
  reads on — both rows of the corpus battery, answered by every leg. The
  negative control is that reader treating the placed key as unknown instead,
  which reads a wrong instance and calls the report clean.
- `graph_deep`, the 260-node chain, has a wire and no text, in the reference
  and in the tool alike, and the corpus says so on its line.
- A label named from inside its own definition is refused, in both engines and
  in the corpus battery, beside a label that overflows a `u64`, a signed one
  and one with a leading zero.

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
- the **root may simply be one `<Root>.json`**;
- a **VARIABLE root is that one file and nothing else** (§16.7): a shared node
  is named by a label a TEXT owns, and a tree of fields would split the root
  across texts that cannot name each other's nodes. `unpack` writes the one
  file for such a root whichever shape is asked for, and `pack` refuses a tree
  of fields under one by name.

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
exactly three — a changed specified default, a moved flags variant, and a
referent that cannot stand in for the one it replaces. The compiler retains
no history and cannot see any of them on its own. The baseline IS that
history, in a text file a person can read in a diff. It refuses more than
those three (§18.2), because an edit the wire DOES report is still an edit a
save game may not survive, and a refusal a person overrides deliberately is
cheaper than a counter nobody reads. **§4.1's table is where the three frames
are set beside each other** — what a reader is told, what this file refuses,
and what moves a build version — and this section states only its own column.

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

**Every line in it is a WIRE fact.** The block form's layout — the
projection's offsets, each row's size, each pitch — is not recorded and not
judged here (§19.3): **the baseline guards what the wire cannot report, and
layout is a compile-time contract**, asserted in both generated sides and
stamped with the build version, so a drift is a build error rather than
something data quietly outlives.

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
  `T`, `?T` and `*T`.

**The BLOCK FORM takes no row here at all** (§18.1). A table's layout is a
same-build contract that a compiler holds (§19.3), so an edit that moves an
offset, a size or a pitch is a build error on both generated sides before it
is anything else; a baseline row would only repeat the compiler, and a
baseline cannot know which tables a project actually blocks. Every verdict
above is the wire's.

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


## 19. The block form

**A fixed table writes into its own instance what it knows about its own
rows — for each of its bounded arrays, where the rows start, how many there
are, and how far apart they sit — so another language reads those facts and
points at the rows.** That is the block form, and it is the whole idea. It
adds no declaration (§2.7): **every FIXED table has one**, a THIRD projection
of the same schema beside its wire (§3) and its cook (§7).

```
table RenderFrame
{
    version uint64
    cameras [..1]RenderCamera
    ships   [..MaxShips]RenderShip
    lasers  [..MaxLasers]RenderLaser
}
```

Nothing there is new. Those are ordinary fields of an ordinary fixed table: a
scalar and three bounded arrays of structs, which this document has always
had. One declaration, three projections of it.

| form | contract | who reads it | cost |
|---|---|---|---|
| the wire (§3) | any version, tolerant, reported | anything, years apart | ids, kinds, lengths, a report |
| the cook (§7) | one build version, pointed at | a build at that version | a cook, from a builder |
| **the block (§19)** | **one build version, both sides generated, pointed at** | **a consumer generated from the same schema** | **rows at a fixed pitch** |

**What a block does NOT carry, in full**, because a reader coming from the
wire will look for all of it: no field ids, no kind bytes, no lengths, no
terminators, no elision — a field is at its offset whether it holds a default
or not, and a row is at its pitch whether it is set or not. No `was`
tolerance, no unknown-variant handling, no clamping, and **no read report of
any kind**: §4's counters do not exist here because none of the events they
count can occur. No node table, no pointers, no arena, no region, no
attribution. A row is its bytes at its offsets, and the guarantee that both
sides agree on those offsets is §19.3's — held at compile time, not reported
at runtime.

**It is PRE-COOKED AT BUILD.** Every layout fact is settled by the compiler
and asserted into both generated sides, so nothing is decided, discovered or
checked at frame time. That is what makes it the fastest thing tables have
and why §12.1 ranks it as the hottest path.

**IT IS EMITTED ON THE SIDE, and that is why no declaration selects it.** A
table unit emits `<Base>Block.h` and `<Base>Block.cpp` beside its
`<Base>Table.h` and `<Base>Table.cpp` (§13.5), and `<Base>Block.cs` for C#.
The Block header carries the whole surface — the projection struct, the
generated layout asserts (§19.3), the builder API (§19.1), and the
`BlockOpen` declaration (§19.2) — and the Block
translation unit carries their definitions. **The Table headers carry nothing
of it.** A consumer that uses the form includes the Block header and links the
Block unit; a consumer that does not includes nothing and compiles nothing for
it. This is §16's split taken one step further: the text form declares in the
Table header and defines in the Table unit, because its cost is a body of code
to compile; the block form's cost is in the header too — a projection struct
and a wall of asserts per table — so its declarations move out with it.

**The zero-cost gate, and it is a gate rather than a hope** (§2.2): **the
Table headers are BYTE-IDENTICAL with or without the Block files existing**,
and the Block files cost nothing unless they are included. It is held in two
halves, because they answer different questions: the build fails if one symbol
of the block machinery — a storage type, a `Begin`, an `Open`, a row accessor,
a layout constant — appears in a Table source, AND every Table source is
byte-compared against a pin the PRE-BLOCK compiler wrote, so the identity is
measured against a build that could not emit a Block file at all rather than
against the emitter's own output. The descriptor
COLUMNS (§8) the block form reads are not machinery and ride in every unit as
every other column does, because they describe the language.

**A VARIABLE-LENGTH table has no block form, and it is refused by ABSENCE**:
no Block declarations are emitted for it, so a consumer that reaches for one
gets a missing name from its own compiler rather than a diagnostic from this
one. The reason is one line — a pointer anywhere in the closure means no fixed
pitch anywhere in it — and there is nothing for the schema compiler to refuse,
because nothing in the declaration asked.

**The rule, in full, because it is the one thing a reader has to know that the
declaration does not spell:**

- **DEPTH ONE, BOUNDED ONLY.** In the block form, every **bounded** array
  field **of the table itself** — `[..N]T` — is laid out of line, in
  declaration order, each at its own pitch, subject to the element rule below.
  Everything else stays exactly where it is: a fixed `[N]T`, an enum-keyed
  `[E]T` — **whose inline extent is `E.Max` elements, one per named
  variant, nothing for `None`** (§2.4), so it occupies `E.Max * sizeof( T )`
  bytes of the projection and not one more — and **every
  array at any depth inside an element** — which is a rule about the EMITTERS
  as much as about the form: a backend that projects a bounded array inside a
  nested record writes sixteen bytes where the other side wrote the whole
  array, and lands every field after it somewhere else. An element is one flat record or it
  is not a record another language can point at, and one level is what makes
  "which arrays move" answerable by reading one declaration.
- **THE INSTANCE IS WHAT MOVES, not the schema.** The block form generates
  the table's struct as a PROJECTION: each out-of-line array's inline storage
  — `T[N]` and its count companion — is replaced, AT THAT FIELD'S POSITION,
  by `(offset_of u64, count u32, stride u32)`, sixteen bytes with no interior
  padding. Every other field keeps its by-value storage at its natural offset.
  That projection is what sits at the front of the block, and nothing declares
  it: it is what the table already knows about its own rows, written down
  where the other side can read it.
- **ONLY ARRAYS OF STRUCTS MOVE.** A bounded array takes the out-of-line form
  only when its element is a fixed-class `table` or a declared `type` — the
  same thing, semantically (the ladder). A bounded array of scalars, of
  `string(N)` or of `bytes(N)` stays INLINE in the projection, exactly as a
  fixed `[N]T` does: striding what another language cannot point at as a
  record buys nothing, and the projection is a struct either way.
- **THE PITCH IS `sizeof`, DERIVED, ALWAYS.** A row's stride is the element's
  `sizeof` rounded up to its alignment — which for a standard-layout struct
  (§9) is `sizeof` itself. Nothing declares it and nothing adjusts it: there
  is no `| stride`, no headroom and no reserved spelling for one (§13.6). The
  pitch RIDES in the triple all the same, because it is what the consumer
  indexes with and it must come from the data rather than from the consumer's
  own constant (§19.2) — and because the pitch is `sizeof`, a consumer's rows
  are CONTIGUOUS, which is what lets C# reinterpret the byte range as a span
  of blittable rows with no marshalling and no copy (§12.1, §19.2).
- **A TABLE, NOT A NEW KIND OF TABLE.** A table in its block form keeps
  everything an ordinary fixed table has: `Measure`, `Save` and `Load` over
  the tolerant wire (§3), its cook (§7), its reflection descriptors (§8), its
  place as a nested field or an array element in someone else's table. The
  form adds a projection; it removes nothing. That is what makes "is the
  render data a table?" answerable with one word.

**Two halves, and neither is new machinery.** Building a block is §6.4's
builder in its block form — reserve from the counts, write the facts, let the
workers run. Reading one is §8's descriptors doing what they already do —
walk a declaration's fields by name and position. The C++ side is a builder
question; the C# side is a reflection question; and the form is what falls
out when both are asked of one table.

### 19.1 The builder

**One extent, 64-byte aligned at its base**, laid out in this order:

- **The PROJECTION at offset 0** (above). It opens with a generated PROLOGUE
  of three `uint64`s — `magic`, a constant identifying a schema block;
  `build_version`, the unit's id (§20); and `byte_order`, `1` little and `2`
  big, which the producer stamps with its own — and the table's own fields
  follow, each at its natural offset, with every out-of-line array's storage
  replaced in place by its sixteen-byte `(offset_of, count, stride)`. The
  prologue is generated, as an optional's presence companion is, and a field
  may not be named after any of the three (§11).

  **The magic's value is `0x4b4c42414d484353`**, which is `SCHMABLK` read as
  ASCII in the byte order a little-endian store produces, so a hex dump of a
  block is legible. A consumer written from this page needs the constant, not
  a description of one.

  **What each of the three does, stated because two of them look like one.**
  The MAGIC is what REFUSES a foreign byte order: read bytewise it either is
  this build's constant, or is that constant byte-reversed — which identifies
  a block of the other order — or is not a block at all. The `byte_order`
  word is what RECORDS which order wrote it, so the refusal can name the
  order rather than infer it and a tool dumping a block can read the fact
  instead of deducing it from a constant. `BlockOpen` checks both: a block
  whose magic matched and whose order word did not is a corrupt or
  hand-edited artifact, and there is no reading that recovers it. The
  BUILD VERSION cannot do either job — §20 digests `byteorder` as a
  GENERATION input (§20.1), `little` for every target schema generates for
  today, so two builds of one schema for two byte orders emit the SAME id.

  **The projection is a record like any other and follows the same C ABI
  rule** (§19.3): its own offsets are part of the contract, not scaffolding
  around it.
- **Then each out-of-line ARRAY, in declaration order**, starting at an
  offset aligned to `max( 64, alignof( element ) )`.
- **Rows sit at the pitch**: row `i` of an array begins at
  `offset_of + i * stride`. Because the pitch is a multiple of the element's
  alignment and the array's start is aligned, every row start is aligned for
  its own type — the arithmetic needs no case.
- **The tail is UNSPECIFIED**: the bytes between the last row and the 64-byte
  rounding of the extent are not written, and a handoff that copies the extent
  copies them. A caller that needs a byte-stable artifact zeroes the storage
  once; the form does not, because zeroing megabytes per frame is exactly the
  cost it exists to avoid.

**Why 64, stated for what it actually buys.** Sixty-four is a cache line, and
the guarantee is PER ARRAY: two workers filling different arrays never share
a line. Inside one array the pitch is the element's `sizeof`, which is
commonly not a multiple of 64, so two workers meeting at a range boundary do
share the one line that straddles it. That is bounded at one line per
boundary — not one per row — because ranges are contiguous runs, and it is
stated rather than implied so nobody reads the alignment as a promise it does
not make.

**The STORAGE COMES FROM THE CALLER'S ALLOCATOR, and it is asked EXACTLY
ONCE.** `<Table>BlockStorage` is a handle, not a buffer: `Create` takes an
alloc/free function pair plus an opaque context — malloc semantics, the
estate's usual shape — allocates `<Table>BlockMaxBytes + 63` bytes through it,
and aligns the 64-byte base inside them, because a malloc guarantee is not a
cache line's. `Destroy` releases through the same pair.

```cpp
struct TableBlockAllocator
{
    void * ( *alloc )( void * context, int64_t bytes );
    void ( *free )( void * context, void * pointer );
    void * context;
};
```

**One call, at BUILD time, per storage — never at frame time and never per
row.** `Begin` asks for nothing; the accessors ask for nothing; the fill asks
for nothing. That is the whole of what the refuser (below) claims, and stating
the allocator here is what makes the claim checkable rather than a slogan
about a form that "allocates nothing" — it allocates once, and the caller
holds the pointer. `TableBlockDefaultAllocator()` is the `schema_allocate` /
`schema_release` pair (§13.9) — `calloc` and `free` unless the program defined
the hooks — for a caller with none of its own; nothing in the generated
surface reaches for it.

**The block's STORAGE is sized from the declared maxima** —
`<Table>BlockMaxBytes`, a compile-time constant over the projection plus every
out-of-line array at its declared `[..N]`. That is the allocate-max law: one
extent, allocated once, never grown, never pooled. **The sum is loose by
construction**, and it is worth knowing why before a case is tight: arrays
commonly draw from one shared pool, so their maxima can add to more than can
ever be occupied at once — in the case this form comes from, the maxima sum
to 7,879,488 bytes (7.51 MiB) against a 10 MiB budget, comfortably, with
about a quarter of that extent unoccupiable in principle. A way to declare a
shared bound is a follow-on (§15); today the answer is that the allocation
happens once and is never grown.

**The LAYOUT is settled once per block, from the counts**, before any worker
starts:

```cpp
RenderFrameBlockStorage storage;              // the allocation: max-sized, 64-aligned
if ( !storage.Create( TableBlockDefaultAllocator() ) )
    return;                                   // ONE call to the caller's allocator, at build time

RenderFrameCounts counts = {};                // gathered before Begin, which is single-threaded
counts.ships  = numShips;
counts.lasers = numLasers;

RenderFrameBlock block;                                          // destination first (§6.1)
if ( !RenderFrameBlockBegin( block, storage, counts ) )
    return;
```

`Begin` refuses counts past the declared maxima, stamps the prologue, writes
every array's `offset_of`, `count` and `stride`, and hands back the block. It
touches no row, and it is `O( out-of-line arrays )` — a handful, not
thousands.

**A refusal NAMES the array, its count and its maximum**, because a producer
at sixty hertz that silently drops a frame is worse than one that does not.

**AND `Begin` DOES NOT CLAMP. It refuses.** A producer that gathered more rows
than a declared maximum — a spawn wave past `MaxShips`, a level with more props
than the bound admits — must clamp AT THE CALL SITE, before it fills the
`Counts`. Nothing downstream will do it: `Begin` returns false and the frame is
lost, which is the loud failure, and no accessor silently truncates, which
would be the quiet one. The declared maximum is a contract the producer keeps
and `Begin` checks; it is not a policy `Begin` applies. A gather buffer sized
larger than the bound is the ordinary case and clamping it is one line, and the
page says so here rather than leaving a caller to find it at sixty hertz.

**Then the fill, and the fill is the requirement:**

```cpp
RenderShip * ships = RenderFrameShips( block );   // the array's typed base

// N workers, disjoint index ranges, no synchronisation of any kind:
ships[i].position = ...;
```

- **THE MULTI-THREADED FILL IS AN OBLIGATION ON THE IMPLEMENTATION, not a
  permission to the caller.** The generated fill path — `Begin`, the array
  accessors, and the row storage they hand back — contains **no allocation,
  no lock and no atomic**, and the build fails if one appears, on the model
  §2.2 already uses for the zero-cost gate. A backend may not satisfy this
  requirement with a serial `Begin` that allocates, or an accessor that
  synchronises. It is held twice over: by that refuser, and by a real
  multi-threaded fill in the corpus (§19.5) whose result is byte-identical to
  a serial one.
- **An accessor is one add**, `block base + offset_of`, typed as the element.
  A worker holds it and indexes.
- **The contract is ownership, exactly as §6.4's is**: disjoint index ranges
  into one array are safe concurrently, and two workers writing one row is
  the caller's problem. `Begin` and `BlockBytes` are single-threaded — call
  `Begin` before the workers and `BlockBytes` after they join.
- **A row the producer does not fill holds whatever the storage held.**
  `count` is the contract; rows past it are not part of the block.

**The USED extent** is the greatest `offset_of + count * stride`, rounded up
to 64, never less than the projection's own size; `<Table>BlockBytes( block )`
returns it. Because the layout follows the counts, it is proportional to the
frame rather than to the maxima.

**Storage lifetime, stated because a per-frame handoff lives or dies on it.**
A block is valid until the next `Begin` on the SAME storage. `Begin`
invalidates every block over that storage and every row pointer taken from
one — it rewrites the facts and the rows underneath them. **Double-buffering
is therefore two storages, and it is the caller's**, exactly as the arena's
slack is in §6.4: a producer that refills while a consumer still reads owns N
storages and alternates. The form allocates nothing and will not allocate a
second buffer for you.

**Worked, over nine arrays and a representative frame.** With the projection
at 176 bytes — the 24-byte prologue, a `uint64`, and nine triples — and rows
of 72 / 88 / 64 / 72 / 72 / 80 / 80 / 64 / 80 bytes:

| array | count | pitch | start | extent |
|---|---|---|---|---|
| cameras | 1 | 72 | 192 | 72 |
| ships | 300 | 88 | 320 | 26,400 |
| turrets | 900 | 64 | 26,752 | 57,600 |
| missiles | 120 | 72 | 84,352 | 8,640 |
| dynamic_props | 40 | 72 | 92,992 | 2,880 |
| static_props | 5,000 | 80 | 95,872 | 400,000 |
| cosmetic_props | 800 | 80 | 495,872 | 64,000 |
| lasers | 200 | 64 | 559,872 | 12,800 |
| explosions | 60 | 80 | 572,672 | 4,800 |

`BlockBytes` is **577,472**. **The agreement is with the ARITHMETIC, not with
one frame**: the rule stated here and the hand-written layout this form
replaces are the same walk over the same pitches, so they land every array at
the same offset for ANY counts — the table above is one instance of that,
chosen to be legible rather than measured. The prologue is free in this
shape: 152, 168 and 176 all round to 192.

### 19.2 The reflective read

**The descriptors are the mechanism, and they are what retires a hand-kept
mirror.** §8's reflection carries, for a block-form table, the projection
offset of every field and of the three members inside each triple, with the
element's own descriptor beside them — and, for every field of a projection or
a row, the row-walk columns §8.1 states: the array class, the slot count and
element size, the count companion and the presence companion. A consumer
holding the descriptors reads the facts out of an instance, points at rows and
reads every field of one — no hand-written struct per table, no knowledge of
the spelling that produced any of it, and nothing to maintain when a field is
added. The mirror died because the layout became data, not because someone
generated a replacement for it.

**That claim is gated value for value, not merely exercised.** The tables
conformance harness's `block-dump` surface walks a block image through the
descriptors alone and writes the canonical ROW DUMP
(testdata/conformance/tables/FORMAT.md), pinned from the C++ leg and
byte-compared by every other. A reader that opens an image and reads a row
wrong passes `block` — which reads the prologue and the triples and stops — and
fails this.

**The typed fast path is generated beside them, and it is what a per-frame
job uses.** For a consumer that wants named fields rather than a generic
walk, the backend emits the blittable struct (#287) from the same projection
the descriptors describe, with the accessors below. Both come from one
declaration, the contract of §19.3 is asserted either way, and a consumer
picks by what it is doing: reflection to walk anything, the struct to read
one thing fast.

```csharp
if ( !RenderFrameBlock.Open( out RenderFrameBlock block, pointer, bytes ) )
    return;               // and the caller falls back, or reports

ulong version = block.Projection.Version;     // the table's own declared fields

foreach ( ref readonly RenderShipRow ship in block.Ships )
    Draw( ship );

ReadOnlySpan<RenderShipRow> ships = block.ShipsSpan;   // contiguous: the pitch is sizeof
```

**The same read in Go**, because a consumer written from this page needs the
spelling and Go's differs where §11 leaves it free to: the claimed verbs stay
free functions, and the row accessors and per-array constants are members.

```go
var block blockdemo.RenderFrameBlock
if !blockdemo.RenderFrameBlockOpen( &block, pointer, bytes ) {
    return                // and the caller falls back, or reports
}

version := block.Projection.Version            // the table's own declared fields

rows := block.Ships()                          // iterated at the pitch the INSTANCE gives
for i := int32( 0 ); i < rows.Len(); i++ {
    Draw( rows.At( i ) )
}

ships := block.ShipsSpan()                     // contiguous: the pitch is sizeof
```

**A BLOCK ROW'S ENUM SLOT HOLDS THE ORDINAL, at the enum's own derived storage
width**, exactly as a cooked slot does (§7.2) — a block row is a by-value
projection and an enum's by-value storage is `uintN` for the smallest N that
fits `E.Max`. The descriptors' `kind` column is the TABLE-WIRE kind, which for
an enum is `u16` because identity on the wire is the variant-name hash (§5), so
a walker that read a row's enum slot at the KIND's width would read two bytes
where the storage has one. Read it at `elem_size`, which is what the row-walk
columns carry it for; the canonical row dump does exactly that.

**THE BLITTABLE RECORDS TAKE A CLAIMED SUFFIX, NEVER A NAMESPACE OF THEIR
OWN.** A row is `<Name>Row` and a projection is `<Table>BlockProjection`, in
the unit's own namespace, both claimed in §11 like every other generated
spelling — so a declaration that would collide is refused at the source, which
is the doctrine the whole generated surface already follows.

**A generated NAMESPACE named by a common noun is a collision class no refusal
can close, and that is the reason.** A nested `<Pkg>.Block` reads well until a
unit declares a `Block` — and `Block` is a common noun a game will have. Worse,
the collision is not even the DECLARING unit's to see: C# compiles many units
into one assembly, so a `Block` in ANOTHER unit of the same package namespace
collides with the namespace this unit planted, and a compiler that checks one
unit at a time cannot refuse what it cannot see. A claimed suffix has neither
problem: it collides only inside the unit that declares it, which is exactly
where §11's claim reaches.

**WHERE THE C# SURFACE IS EMITTED, because C# has no include guard and the
answer is not "beside the declaration".** **The unit's shared runtime lives in
`<Package><Surface>.cs`** — the table runtime and the text form's walk (§16) in
`<Package>Table.cs`, the block runtime and every blittable record the FORM
TOUCHES in `<Package>Block.cs`, the cook runtime and the records the block form
does not already carry in `<Package>Cook.cs`. One home per surface per unit,
named by the PACKAGE, and it is emitted for the unit when no file of the unit
is named for the package — so the home always exists and no reference to it can
dangle. A file basename that differs from the package only by case is the same
name on a case-insensitive filesystem, so the match is case-insensitive and the
file's own spelling wins.

The home is the package and NOT A FILE because every file-order rule relocates
the runtime: the day a unit gains a file that sorts earlier, ~2,000 lines move
to it — correct output, and a diff nobody can read. It is likewise deliberately
NOT the protocol id's home, which in an ordinary unit is a constants file that
declares no table and therefore gets no Block file at all; and a record is
deliberately NOT emitted beside its declaration, because a record a block form
reaches is often declared in a file of `type`s alone, which gets no Block file
either. Both of those roads lead to a unit that does not compile, with every
reference undefined and no diagnostic. What stays per DECLARATION is what
belongs to one: a table's projection record and its block handle ride in its
declaring file's `<Base>Block.cs`. The cook takes the records the block form
already carries rather than spelling them again — a cooked record IS the
blittable row, and one declaration cannot be two types. One assembly sees every
file, so "emitted once, anywhere it exists" is the whole requirement.

**The GO backend follows both rules exactly**, for the reason C# does — a Go
package is one namespace across its files, so "emitted once, anywhere" is the
whole requirement there too — and it adds one of its own: the DESCRIPTOR GRAPHS
are one package-level slice per unit (`tableBlockRecords`, `tableCookRecords`,
`tableUnionArms`) rather than one variable per record, because a name derived
from a declaration's own spelling is a name a declaration can collide with, and
§11's registry has no shape for a prefix-and-name product.

**C++ takes the other road for its own reason**: a C++ consumer may include one
`<Base>Block.h` alone, so its primitives ride in EVERY one behind a `#ifndef`
guard.

**Three spellings a reader has to have, because a consumer written from this
page alone needs all three.** `Open` is a static on the block type, taking the
destination first (§6.1) and then an `IntPtr` and a `long` — a block is memory
another language wrote, so the C# side takes a pointer and a length and the
generated source is `unsafe`. The ROWS are `[StructLayout(Sequential, Pack = 1,
Size = N)]` STRUCTS named `<Name>Row`, never the sealed CLASS of the bare name
beside them: that is the table wire's storage, and binding one here is the
mistake the suffix exists to stop. A field's C# member is the PascalCase of its
schema name, as everywhere else in that backend.

- **`BlockOpen` checks once and points, and this is the WHOLE check**: the
  magic read bytewise, the byte order it establishes, the build version
  against this build's own, the base's alignment, each array's PITCH against
  this build's own, its COUNT against the declared maximum, its `offset_of`
  and its extent inside the block, and the used extent against the `bytes` the
  caller passed. A count past the maximum is checked HERE as well as at
  `Begin`, because a consumer that sizes anything by the maximum overflows on
  a count the maximum does not bound. **OVERLAP is not refused**: two arrays
  whose ranges cross both open, because every row of each still lies inside
  the region and a block has nothing an overlap could corrupt. On a match the
  bytes are what a build with this layout wrote, so there is nothing to
  validate and nothing to fix up. On any failure it returns false and points
  at nothing — §7's shape, for §7's reason.
- **An array is ITERATED, not indexed by hand.** The accessor yields a
  reference to each row where it lies, at the pitch the instance gives, for
  `count` rows — a range-for in C++, an enumerator in C#, the equivalent per
  port. A call site never spells the pitch arithmetic itself, for the same
  reason a keyed array's call sites should not re-derive their own slot rule:
  the idiom written at every call site is the one written wrong somewhere. A
  typed base pointer sits beside it for the producer, which addresses rows by
  index (§19.1).
- **A contiguous view is available because the pitch IS `sizeof`** (§19), so
  a consumer that casts a byte range to a row type — which is how the fast
  path is actually written — is always able to. That is a property of
  deriving the pitch, and it is the hard requirement the derived pitch is
  held to (§13.6): a declaration that could widen the pitch would cost this
  read its contiguity, which is why there is no such declaration.
- **The consumer reads `offset_of`, `count` and `stride` FROM THE INSTANCE,
  never from its own constants.** Its constants exist to be asserted against
  (§19.3), not to index with. That is the difference between a generated pair
  of structs and an ABI, and it is what makes §19.4's absorbed edits
  absorbable.

### 19.3 The layout contract

**The compiler computes the layout, and both backends assert it.** A record's
layout is the C ABI's natural one: each field at its own alignment, the
struct's alignment the greatest of its fields', the size rounded up to it.
**That rule covers the projection as much as a row** — the table at the front
of the block is a record too. The compiler derives every offset and size from
the declaration, and each backend emits code asserting that ITS OWN compiler
agrees:

- **C++**: `static_assert` on `sizeof` and `offsetof` for the projection and
  every row type and every field of each, plus the pitch constants, plus the
  standard-layout and trivially-copyable asserts §9 already requires.
- **C#**: a blittable `[StructLayout(LayoutKind.Sequential, Pack = 1, Size =
  N)]` struct with GENERATED PADDING FIELDS wherever the C ABI layout has
  interior padding — **`Size = N` pins the TRAILING padding and the generated
  fields pin the INTERIOR padding**, and both are needed — plus a generated
  check, run once, asserting each type's size and each field's offset against
  the same constants the C++ side asserts. Explicit padding is chosen over
  `LayoutKind.Explicit` because Sequential is the form every blittable path
  handles best, and over relying on a padding-free field order because that
  is discipline, and discipline is what this form exists to delete.

**The C# asserts run against the MANAGED unmanaged-struct model**, and naming
it is not pedantry — C# has two layout models and they disagree on the field
kinds this form uses. The asserts and the accessors use the model that
`Span` and pointer arithmetic actually index with (`Unsafe.SizeOf<T>` and its
equivalents), **not** the interop marshalling model (`Marshal.SizeOf`,
`Marshal.OffsetOf`). The consequence to state plainly: **a `bool` in a row is
ONE byte** — one byte in C++ and one in the managed model, four under default
marshalling. A contract that did not say which model it asserted could pass
on one measurement and garble on the other. **`Pack` and `Size` set the
MANAGED layout too**, despite reading as interop attributes, which is exactly
why they are the mechanism here and not a contradiction of the sentence
above.

**And C++'s `sizeof( bool ) == 1` is ASSERTED, not assumed.** The standard
leaves it implementation-defined, so the generated `static_assert`s carry it
like every other layout fact: a platform where it differs fails the build
loudly instead of writing rows the other side cannot read.

**`Size` is the element's `sizeof`, never its pitch.** Making the struct as
wide as the pitch would make the two sides' size asserts compare different
numbers, so a row that had quietly grown past its width would still pass. The
struct is the ROW and the instance carries the PITCH; the iterator puts them
together (§19.2).

**A disagreement is a BUILD ERROR on the side that disagrees**, naming the
type, the field, the expected offset and the one its compiler produced — with
one honest exception: **C# has no `static_assert`**, so its generated check
runs ONCE at type initialization and THROWS, naming the same four things. It
is loud and it is early — before any block is opened — but it is a first-use
failure and not a compile-time one, and a port should not read "build error"
as a promise C# can keep.
Neither side can drift silently, and neither side's layout is inferred from
the other's — both are checked against the compiler's own model, which is the
only way a two-language contract can be held by a compiler that generates
both halves.

**The id in the prologue IS the unit's BUILD VERSION** (§20), and it is not a
digest of its own. §20's cook projection carries a block's facts beside every
other record's: the projection's own fields with their offsets and sizes, each
out-of-line array's element and pitch, and every row field's offset, size and
kind — the `block` and `slot` lines of §20.2. A cook and a block share the id
and differ in their MAGIC, which is where a form's identity belongs (§7).

**A declared MAXIMUM moves NO `slot` line**, and for the block's own facts
that is exactly right: a maximum sizes the storage and moves the `offset_of`s
written into the instance, and it moves no offset a consumer reads AT, because
a consumer takes every `offset_of` from the instance (§19.2). A triple is
sixteen bytes whatever the maximum is. **But it is a `bound=` on the table's
own `field` line, and the by-value `record` lines move with it**, because
raising a maximum grows the inline array's storage and every later field's
offset — and those lines are what a COOK of that table is opened against. So
the build version moves, and `BlockOpen` refuses the edit like any other
(§19.4).

**What the id sees, and what it does not.** It sees layout and it sees
meaning — the facts that decide what a load puts in a slot are §20's group 3.
It does not see INTENT: a field whose units, frame of reference or
interpretation changed while its offset, width, kind and declared range did
not moves nothing, because no stated fact moved. A semantic version is
therefore an ordinary field an author declares and owns, and the id neither
replaces nor covers it.

### 19.4 Evolution

**A block is SAME-BUILD, and that is the whole of its evolution story.**
Both sides are generated from one declaration by one compiler run, and the
build version says whether the bytes in hand came from it. There is ONE
entry point — `BlockOpen`, checking §19.2's whole list: the magic, the byte
order it establishes, the build version, the base's alignment, each array's
pitch, count, `offset_of` and extent, and the used extent against the `bytes`
the caller passed. A match points; anything else refuses.

**So every edit that moves the build version refuses at open, and both sides
regenerate.** That is not a gap in a tolerance story, it is the absence of
one: a producer and a consumer generated together cannot be at different
versions unless someone shipped half a build, and a refusal is what should
happen then. The three edits worth naming, because a reader coming from §4's
tolerant wire will look for them:

1. **A field appended at the END of a table with a block form** — a scalar, or a
   new out-of-line array. Every earlier offset is unchanged, but the id moves
   (a new offset and size), so `BlockOpen` refuses.
2. **A field appended at the END of a row type.** The row grows and so does
   its derived pitch; the id moves (a changed row size and pitch), so
   `BlockOpen` refuses.
3. **A declared maximum raised.** The storage grows and the `offset_of`s
   move; the consumer reads them, so nothing it reads AT has moved and its
   own `slot` facts are unchanged. **The id moves anyway**, because the
   table's by-value layout moved and the id covers a cook of that
   table too (§19.3, §20.4) — so `BlockOpen` refuses like the other two, and
   the three are one rule instead of three cases.

**Everything else is a break, and the GENERATED ASSERTS refuse it** (§19.3):
a field inserted before the end, reordered, removed or retyped, in the table
or in a row type; an array's element swapped; an array moved between the
out-of-line and inline classes, or removed, or moved earlier. Each moves a
byte the other side reads at, and a block has nothing that could report it —
which is why the refusal is at compile time and loud, and why the baseline
(§18) carries none of it.

**The STRIDE still rides in the instance, and it still must be read from
there** (§19.2). It is not a tolerance mechanism — the id already settled
that both sides are one build — it is what keeps the consumer's indexing a
fact of the data rather than a constant it compiled in, which is the
difference between a form and a convention.

### 19.5 Held by test

- **A dogfood-shaped table with a block form in the corpus** — a root of several
  bounded arrays over fixed-size row types with a nested `type` apiece —
  compiled, built and read by every backend that carries the form.
- **A REAL multi-threaded fill.** N workers fill disjoint index ranges of
  every array; the resulting block is byte-identical to a serial fill of the
  same data over the rows each count covers. Run under the THREAD sanitizer leg, which is
  the one that can see a race — an address sanitizer cannot, and neither can
  byte-identity between a serial fill and a deterministic wide one. That leg
  carries its own negative control: every worker filling the WHOLE of every
  array instead of its own share must turn it red. **Beside it, the
  conformance refuser** (§19.1): the build fails if the generated fill path
  contains an allocation, a lock or an atomic. That is the whole of what the
  refuser claims — the generated surface is `Begin` plus accessors, and
  keeping it free of those three is the property that MAKES a caller's wide
  fill possible. The parallelism itself lives in the caller's loop, so no
  gate here could assert it; the obligation stands on this refuser, the fill
  test above, and §12.1's measured leg together.
- **A TWO-LANGUAGE layout test.** A C++ producer writes a block; a C#
  consumer opens it and compares every field of every row against what was
  written, plus each array's `offset_of`, `count` and `stride`, and the
  projection's own fields. Sizes and offsets are asserted by generated code
  on both sides, so the test proves the two agree on the BYTES and not merely
  on the constants. It runs twice on the C# side — through the descriptors
  and through the generated struct — because §19.2 offers both and both must
  land the same values.
- **A NEGATIVE CONTROL for each half.** Perturb one row type's pitch constant
  on one side only and the two-language test goes red; perturb one field's
  offset in the compiler's layout model and the generated asserts go red on
  both backends. A layout test that shares its layout model with the code it
  checks proves nothing, and these two are what separate them.
- **A `bool` row.** A row type carrying two `bool`s beside its scalars, whose
  C# size and offsets are asserted under the managed model (§19.3) — the case
  where the two C# layout models disagree, pinned so a port cannot pick the
  wrong one and pass.
- **A FORGERY FUZZER over `BlockOpen` AND the cook's `Open`, on every backend
  that carries a reader, as a standing gate.** In a language where a read past
  a view THROWS, "refuse, or open and be whole" and "no exception escapes a
  reader" are the same sentence, and the reading tier's leg states it that way:
  a forged block or cook either refuses, or opens and is walked in full through
  its descriptors without one read leaving the bytes the caller passed.
- **A FORGERY FUZZER over `BlockOpen`, on both backends, as a standing gate**:
  valid blocks from the generated builder over several count vectors — zero,
  mixed and every array at its declared maximum — mutated by seeded byte flips,
  field overwrites at every boundary value, swapped and overlapped triples,
  truncated and extended lengths and unaligned bases, and the bar is REFUSE, OR
  OPEN AND BE WHOLE: a refusal reads no byte outside the extent the caller
  passed, and an opened block has every row of every array addressable inside
  it, at this build's pitch, inside its declared maximum, with a full walk that
  reads every byte of every row. Its own negative controls remove the extent
  check and the declared-maximum check from the emitters and require it to go
  red on both backends.

- **The refusal battery**: one fixture per §11 block refusal, each with its
  negative control.
- **The evolution battery** (§19.4), against the generated asserts and the one
  entry point rather than against a baseline: a field inserted in the middle
  fails the build; a field appended at the end of the table, a field appended
  at the end of a row, and a maximum raised each move the build version and
  each REFUSE at `BlockOpen`, which is the same rule three times and is what
  same-build means.

- **WHAT ALLOCATES, AS A RATE.** A soak with a flat heap is a LEAK instrument
  and nothing more: an allocation made and collected every iteration leaves the
  heap exactly as flat as no allocation at all. So a backend states its
  allocation floor as BYTES PER ITERATION, per path, measured — and gates the
  paths that must be zero, with a negative control that puts one extra
  allocation per iteration in and requires them to go red. Every unavoidable
  allocation is named rather than absorbed: in the reading tier's first port
  that is one BigInt per 64-bit FIELD read, and nothing else — a 64-bit number
  the FORM's own framing carries (a block array's `offset_of`, a cook
  reference's delta) gets no such licence, because those are the two hottest
  lines the accelerators have and a per-row or per-edge allocation on them is
  the defect this instrument exists to find. It found both.

  **AND THE FLOOR IS A PROPERTY OF THE RUNTIME, NOT ONLY OF THE CODE.** In a
  managed language the same source allocates differently at different
  optimization tiers, so the floor is claimed for a NAMED runtime: the reading
  tier's first port measures on the version its CI runs, refuses to certify on
  another major, and warms until the rate settles rather than for a fixed
  count. What that instrument found, pointed at the right runtime, is one rule
  worth stating for every port that follows: **a float field is read and
  written by the generated body itself, never across a call.** A double that
  crosses a JavaScript call boundary is a heap number — sixteen bytes, per call
  — unless the callee is inlined, and whether it is inlined depends on the size
  of the body around it. A `putF32` helper therefore allocates in the codecs of
  big tables and not in those of small ones: the estate's law that a generated
  codec must not depend on the compiler's inlining budget, arriving in a JIT.
  What a port CANNOT close this way is its own public accessor — a float
  returned to a caller boxes on the same terms if that caller's compiler
  declines to inline it — so the tier states that where the accessors are
  documented rather than claiming immunity it does not have.
- **THE READING TIER's own pair, in every backend that has no layout to
  assert.** (a) The generated ACCESSORS and the DESCRIPTORS are two spellings
  of one layout, so every field of every row of every block is read both ways
  and compared — the harness proves the descriptors against C++, and this
  proves the accessors against the descriptors. (b) The layout check runs the
  one comparison such a language still has that is TWO derivations: each
  array's pitch constant against the size of the row object it names. Each half
  carries its own negative control — an accessor four bytes off, and a pointer
  slot eight bytes off, since the first sabotage cannot reach the second's code
  path.
- **The zero-cost gate** (§19): the Table headers are byte-identical with the
  Block files generated and with them absent, and the build fails if one
  symbol of the block machinery appears in a Table header. A backend whose
  modules are separate files holds the same property as a file-set identity:
  the packet modules are byte-identical with a table added, and the only
  modules a table adds are the table ones.
- **The wire is untouched** (§3): a table's wire goldens are byte-identical
  whether or not its block form is generated, and its `Save` and `Load` are
  the ones any fixed table has. That is the test that keeps the form a form.
- **The measured leg is §12.1's**, and it is the gate rather than a
  regression test: the per-frame C++ write and the per-frame C# read, against
  the hand-written scatter and the hand mirror, paired in one sitting under
  the bench rules.

**And the shape of the whole thing, in one line.** A block is a fixed table,
its rows are fixed tables, the facts about its rows are fields of the table
that holds them, and every one of those is a struct with a wire beside it.
Nothing here is a new kind of thing: **it's tables all the way down.**

## 20. The build version

**Tooling cooks asset X to build version Y, and `(X, Y)` is the tuple a
distributed store is indexed by** — written by the tools that produce a cook,
asked for by the game that loads one. That is what the build version is for,
and everything below follows from it.

**The invariant is one sentence: any divergence in the bytes a cook would
produce is a new build version.**

**There are TWO UNIT-WIDE VERSION IDS in this design and no others** — the
per-node type id §6.3 writes into a directory is an identity, not a version.
The PROTOCOL ID is the type wire's, and it is the connect gate (SPEC.md §3):
same-or-refuse, tables never in it. The BUILD VERSION is everything cooked or
blocked: the cooked header carries it (§7), the block form's prologue carries
it (§19), and the store is keyed by it. **A table edit moves the build version
and never the protocol id; a type edit moves both.**

**It is COMPILER-SETTLED, and that is the property the tuple rests on.**
Tooling cooks before any game binary exists, so Y has to be knowable from the
schema alone. The compiler owns every fact in it, including the layout: it
computes each record's layout from its own C ABI model — the model §19.3
states and both backends MUST assert against (§20.3) — and emits the id as one
constant. A build whose compiler lays a record out differently fails to BUILD,
loudly, naming the type and the field; §20.3 states the asserts in full.

**Backend status: the id, its projection and both READ sides are LIVE.**
`schema build-version` prints the id and `schema build-version --facts` prints
the projection of §20.2, both pinned as goldens over the corpus. The BLOCK
backends emit the constant and stamp it into every block's prologue (§19.1), and
`BlockOpen` compares it; `schema cook` stamps it into the cooked header, `schema
cook-check` reads it back, and every port's cook entry point compares it (§7) —
the C++ `<Root>Open`, the C# `<Root>Cook.Open`, the Java `<Root>Cook.open`. What remains owed, largest first:

1. **The constant rides in the TABLE-bearing sources only.** §20.7 asks for
   one beside `ProtocolId` in every backend; today the block backends emit
   it into `<Base>Block.h` / `<Package>Block.cs`, the C++ table backend emits it
   into every `<Base>Table.h` — where the cook's reader is — the Java backend
   gives it a package-level file of its own, `BuildVersion.java`, emitted for
   any unit with a table and for no other, the C# cook emits
   it into `<Package>Cook.cs` when the unit has no block form to carry it already,
   and the seven backends that carry no table emit none. The C# Table sources
   carry none, which is the zero-cost gate (§2.2) rather than an omission: the
   C# cook reader is in the accelerator's own file. **In C# exactly one
   accelerator defines it** — `Schema` is one partial class across a unit's
   files, so a second definition is a compile error rather than C++'s harmless
   re-inclusion behind a guard.
2. **The C# check is a THROW at first use, not a build error.** §20.3 asks
   for a build error on the side that disagrees; C# has no `static_assert`,
   so the generated check runs once at type initialization and throws naming
   the type, the field, the offset it found and the offset the compiler's model
   gives. Loud and early, but not at compile time. The gate runs both `Verify()`
   halves as their own start-up mode rather than waiting for a first open, which
   is the most a runtime with no compile-time assert can do.
3. **The COOK's WRITE side is the C++ backend's and the TOOL's.** `Cook` and
   `CookMeasure` are emitted for every FIXED table in C++ and their bytes are
   `schema cook`'s, byte for byte, in both byte orders (§7.6). A pointered
   root's writer, and every other language's, are named follow-ons (§15). BOTH
   read sides are emitted and gated.
4. **A UNION-BEARING closure has no C# cook reader**, for the reason it has no
   block form: §19.3 pins a blittable C# record to `Sequential`, which cannot
   overlay arms. The generated file names the table and the reason, the table's
   cook and its wire are untouched, and C++ reads one. A named follow-on (§15).

### 20.1 What it digests

Three groups, and the grouping IS the definition — a cook's bytes depend on
the type wire, on what the region looks like, and on what a load puts in it.

1. **THE TYPE WIRE: the unit's protocol id** (SPEC.md §3.1). Types nest in
   tables BY VALUE and their bytes are written into the region verbatim, so
   the wire-shape projection rides in whole, already reviewed, as one id.
2. **THE LAYOUT of every record in the unit's table closure** — each record's
   `sizeof` and `alignof`, and per field its WIRE ID, kind, offset, size, the
   DECLARATION it names, its array class, bound and key, an out-of-line
   array's pitch, and a presence companion (§20.2). Keyed by wire id and not
   by source name, because a `was` rename moves no byte and must not
   invalidate a cooked file. **Kind is a fact in its own right**: a `uint32`
   retyped to `float32` moves no offset, no size and no wire id, and a load
   that kind-mismatches leaves the slot at its default (§4) — different bytes
   under an id that had not moved. **And so is the REFERENT**: a kind number
   says a record is nested and not WHICH one, so two same-shaped records are
   interchangeable to a digest that stops there, and the nested body then
   decodes under different field ids. Every referent has a name on its line.

3. **THE MEANING: what a wire load PUTS in those slots.** A cook is produced
   from a builder that a load filled, so a fact that changes what a load
   produces changes the cook's bytes while moving no offset at all. **Four
   kinds of declaration do it**:
   - every field's **specified default** — an elided field means the reader's
     declared default (§3), so the same bytes materialize a different value;
   - every field's **effective declared RANGE** — a `min`/`max`, a compressed
     float's declared range and resolution, and the `[0, 2^N − 1]` a `bits(N)`
     field declares by its width (§8). §4 clamps an out-of-range value to the
     READER's bounds and counts it, so tightening a range changes what a load
     stores while the offset, the size, the storage kind and the wire id all
     stay put — *"the bound is not part of identity"* (§4) is precisely why
     nothing else sees it;
   - every `enum`'s **variant order and names** — an enum rides as the hash of
     its VARIANT NAME (§3) and a slot stores the ORDINAL, so a rename makes a
     stored variant unknown and a reorder lands it on a different ordinal;
   - every `union`'s **arm order and names** — the same two facts one level
     up: the wire resolves an arm by name hash and the slot stores its tag.

**And the BYTE ORDER**, one line, because a cook is produced in the byte order
of the build it is cooked for (§7): two builds alike in every other fact
produce different cook bytes, and a tuple that addresses two different
artifacts is a defect in the tuple. It is a generation input, `little` for
every target schema generates for today; a big-endian cook is the cross-endian
question §15 owns.

**The set is closed, and the table below is the proof.** Every
declaration-side fact this language has appears in it exactly once, assigned
to the group that carries it or to `none` with the reason it carries nothing.
A meaning fact is one that changes a stored VALUE while moving no offset, no
size and no wire id; there is no fourth kind of such fact. **§4.1's table sets
this column beside the other two** — what the read report tells a reader, and
what the baseline refuses — so a reader asking "what does this edit do?" reads
one table rather than three lists.

**Every fact below has a TOKEN that carries it** (§20.2), and that is the rule
the table is held to: a fact with no token is a promise the digest does not
keep, so it belongs in `none` with its reason or the token belongs on the line.

| declaration-side fact | carried by | the token, or the reason there is none |
|---|---|---|
| `package` | wire | the `protocol` line — it is in the wire-shape projection (SPEC.md §3.1) |
| every fact of a `type` declaration's wire | wire | the `protocol` line, in full |
| a record's NAME | layout | the `record` line |
| a field's name | layout | `field <id>`, through its wire id |
| a `was` alias | layout | none needed: it holds the wire id fixed, so the rename pair moves no line |
| a field's kind, storage width, declaration order | layout | `kind=`, `size=`, and the `offset=`s the order produces |
| the DECLARATION a field or its element names | layout | `type=` / `enum=` / `union=` — the name, not just the kind number |
| a union arm's payload type | layout + wire | `payload=` on the `arm` line, and the type's own facts through the protocol id |
| array class and bound; string/`bytes` capacity | layout | `array=`, `bound=`, and the `size=` they produce |
| a keyed array's KEY enum | layout | `key=` — its slots ride by that enum's variant-name hashes (§3.2) |
| an out-of-line array's pitch | layout | `stride=` on the `slot` line |
| `?T` presence companion | layout | `optional=true`, and the `size=`/`offset=`s the companion moves |
| `*T` reference slot | layout | kind `17` with `type=` naming the pointee |
| a `flags` field's storage width | layout | `size=` |
| a specified DEFAULT | **meaning** | `default=` — an elided field materializes it (§3) |
| a declared `min`/`max`; a compressed float's range; `bits(N)`'s implied range | **meaning** | `min=` / `max=` — §4 clamps a load to the reader's bounds |
| a compressed float's RESOLUTION | **meaning** | `step=` |
| an `enum`'s variant order and names | **meaning** | the `variant` lines — the wire carries a name hash, the slot the stored value (§3) |
| a `union`'s arm order and names | **meaning** | the `arm` lines, the same one level up |
| a `flags`' variant order and names, and WHICH flags declaration a field names | none | a mask rides raw and a load copies it VERBATIM — no masking rule, no report counter (§3, §4). The stronger basis is the storage: SPEC.md §4.2 makes a `flags` field a `uint64` in EVERY target and a flags field carries no specified default, so a slot is a raw `u64` copied through, and no declaration-side flags fact can reach a cook's bytes. A reorder changes what the bits MEAN and not one cook byte; §4.1's discipline and §18's baseline are its guard. Hence no `flags=` token |
| `cpp_native` / `cpp_include` | none | SPEC.md §6.1 guarantees layout identity by DERIVATION — the native type is the one the emitted struct would have had — so there is nothing for a token to distinguish |
| an `if` GUARD added or removed | none | a load finds a field by its id whatever branch encloses it, with every counter zero (§4.1). The cost is on the next WRITE, which is §18's case and not a cook's |
| the `json` key (§16.4) | none | a cook is produced from the WIRE file, whose hash is the tuple's other half (§7) |
| a `const` declaration | none of its own | its value flows into a bound (layout) or a default or range (meaning) and is EVALUATED into that token |
| a type tag | none | it claims meaning no generated byte depends on (SPEC.md §3.1) |
| comments, whitespace, file names, file layout, declaration ORDER ACROSS records | none | none of it reaches the projection |
| a `tables.baseline`, its history, a `--reason` | none | a record of what an edit may do, not a fact a byte depends on |

**The closure is TRANSITIVE** — every table, `type`, `enum` and `union` the
unit's tables reach, at any depth, including an enum-keyed array's KEY. A
declared `type` is a record here exactly as a table is, because on the table
wire *"an arm's body is a table body whether the arm names a declared `type`
or a `table`"* (§3), and because *"fixed tables and types are semantically the
same (structs)"* (§13.6). A unit that declares no table has a projection of
its header lines alone — which still carries the block form's prologue shape,
because that is a fact of the BUILD (§20.2).

### 20.2 The cook projection, and the id taken over it

**The build version is the low 64 bits of SHA-256 — the final eight bytes,
interpreted big-endian — over the unit's COOK PROJECTION**, which is exactly
how the protocol id is taken over the wire-shape projection (SPEC.md §3.1),
for exactly its reason: what an id depends on has to be printable, readable
and diffable, and a fact missing from it has to be a review question rather
than an implementation detail. **One id, one instrument, one text** —
`schema build-version --facts` prints it.

**The projection is ASCII, every line terminated by one `\n`, no blank
lines.** Tokens are separated by exactly one space; a nested line is indented
four spaces. Ids are four lowercase hex digits; sizes, offsets and bounds are
decimal; and every value is the schemafmt-canonical text of the EVALUATED
value (§18.1) — what a constant now produces, never how it was spelled.

```
schema-build-version <N>
protocol <16 lowercase hex>
byteorder little|big
block prologue=<word>:<width>[,<word>:<width>]...
```

**The `block` header line is the BLOCK FORM's own shape, and it rides
UNCONDITIONALLY** — in a unit with no block-form table, and in one whose
tables have no out-of-line array at all. It has to: nothing selects the form,
every fixed table has one (§2.7), and a table whose arrays are all inline has
a projection that is PURE PROLOGUE, so its shape appears in no `block` record
line below. Two builds either side of a change to the prologue would otherwise
share an id and write incompatible blocks, which the invariant does not
permit. The words are NAMED and WIDTHED rather than counted, so the line moves
when the shape moves and there is no counter for anyone to forget.

Then the records, SORTED BY NAME byte-wise over UTF-8, each followed by its
fields in DECLARATION ORDER — which is a layout fact, and the offsets on the
lines are what it produces:

```
record <Name> sizeof=<n> alignof=<n>
    field <id> kind=<n> offset=<n> size=<n>
        [ type=<Name> | enum=<Name> | union=<Name> ]
        [ elem=<n> ] [ array=fixed|bounded|keyed ] [ bound=<n> ] [ key=<Name> ]
        [ stride=<n> ] [ optional=true ]
        [ default=<v> ] [ min=<v> ] [ max=<v> ] [ step=<v> ]
```

**The optional tokens appear in that order and only where the fact exists**,
and they are one line on the wire — the display above is wrapped for reading.
The REFERENT tokens are the load-bearing half and they mirror §18.1's, for
§18.1's stated reason — *"a field that names a declaration records WHICH KIND
of declaration it names"*:

- **`type=` / `enum=` / `union=`** name the declaration this field, or its
  ARRAY ELEMENT, refers to. Without them a retype between two same-shaped
  records moves nothing: `Buff { multiplier float32 }` and
  `Debuff { amount int32 }` are both `sizeof=4 alignof=4`, so a field swapped
  from one to the other keeps its kind, size, offset and wire id while the
  nested body decodes under different field ids. The kind number says a
  record is nested; the name says WHICH.
- **`key=`** names a keyed array's KEY enum, because its slots ride by that
  enum's variant-name hashes (§3.2): `[Difficulty]int32` and `[Team]int32` are
  the same kind, the same element and the same three slots at the same offsets,
  and they are not the same data.
- **`array=` and `bound=`** name the array's CLASS and its evaluated extent,
  so a fixed array and a bounded one of the same width are distinguishable and
  a moved bound is visible beside the `size` it produced.
- **`optional=true`** marks a presence companion, which is a slot the other
  side reads.
- **There is deliberately NO `flags=` token.** A flags field's referent is not
  a cook fact: the slot holds a raw mask, a load copies it verbatim, and
  swapping one flags declaration for a same-width other changes no byte
  (§20.1). Its WIDTH is carried by `size=` like any other field's.
- **A plain integer with no declared bound carries no `min=`/`max=`**; a
  `bits(N)` field carries the range it declares by its width, `min=0
  max=<2^N − 1>` (§8).

**A variable-length table is a record like any other.** Its node is a struct
(§6.1) with a `sizeof` and an `alignof`, and a pointer field is a kind `17`
slot whose `type=` names the pointee — so a pointered unit projects exactly as
a fixed one does, and nothing about the arena or the region appears here.

Every record whose block form LAYS AN ARRAY OUT OF LINE (§19) is followed by
its PROJECTION, whose slots are the other side's contract. A record whose
arrays are all inline has a projection that is the prologue and its own
by-value layout, both of which the header line and the `record` line above
already carry, and it contributes no `block` line:

```
block <Name> sizeof=<n> alignof=<n>
    slot <id> offset=<n> size=<n>[ out_of_line stride=<n>]
```

Then the enums and then the unions, each set sorted by name, variants and arms
in declaration order:

```
enum <Name>
    variant <stored value> <name>
union <Name>
    arm <stored tag> <name> payload=<Name>
```

**The number is the STORED VALUE, not a positional index**, because what group
3 captures is what a slot holds: `None = 0` is implicit on both (SPEC.md
§4.2), it is never listed, and declared variants and arms therefore start at
`1`. An arm carries its `payload=` for the same reason a field carries
`type=`: two arms of the same shape are not the same arm.

**There is no `flags` line and no `flags=` token** (§20.1): a flags field has
a `field` line like any other, and neither its declaration's identity nor its
variants are facts a cook's bytes depend on.

**The first line is the projection's own FORM VERSION**, and it is the COOK
FORM's too. Bump `N` when this rendering changes, and bump it when the cook's
own form changes — the region's pack order (§6.3), the node directory's
encoding, the header's shape. Those are compiler and runtime facts rather than
declaration facts, and without a version for them a cook's bytes could diverge
with the id unmoved, which the invariant does not permit.

**Worked, so a second implementation reproduces the number.** Take:

```
package demo

enum Grade { Bronze, Silver, Gold }

table ShipConfig
{
    damage float32 = 21.0
    speed  float32 = 500.0 | was = "velocity"
    armor  uint8 | min = 0, max = 100
    grade  Grade = Silver
}
```

Every number below derives from a rule on a page; none of it is declared.

- **Wire ids** are `fold16(fnv1a32(name))` over the EFFECTIVE name (§5), so
  `speed` keys on `velocity`: `damage` `15a9`, `velocity` `2e46`, `armor`
  `7c9d`, `grade` `d272`.
- **Kinds** are §3's closed set: `10` f32, `6` u8, and an enum rides as u16 —
  kind `7` — whatever its declaration-side storage.
- **`Grade`'s STORAGE is `uint8`**, and it is derived rather than chosen:
  `None = 0` is implicit, the three declared variants pack from 1, `max`
  derives as the count, and *"enum storage derives from the enum's own max —
  the smallest unsigned integer that fits"* (SPEC.md §4.2). `max = 3` fits in
  a byte.
- **The layout** is the C ABI's natural one (§20.3): `damage` at 0 and
  `speed` at 4, both four wide; `armor` at 8, one wide; `grade` at 9, one
  wide; the record's alignment the greatest of its fields' — 4 — and its size
  10 rounded up to it. `sizeof=12 alignof=4`.
- **The variants' numbers are STORED VALUES**, so they run 1, 2, 3 and `None`
  is not listed.

With the protocol id `0x0123456789abcdef` and a little-endian target, the
whole projection is:

```
schema-build-version 1
protocol 0123456789abcdef
byteorder little
block prologue=magic:8,build_version:8,byte_order:8
record ShipConfig sizeof=12 alignof=4
    field 15a9 kind=10 offset=0 size=4 default=21.0
    field 2e46 kind=10 offset=4 size=4 default=500.0
    field 7c9d kind=6 offset=8 size=1 min=0 max=100
    field d272 kind=7 offset=9 size=1 enum=Grade default=Silver
enum Grade
    variant 1 Bronze
    variant 2 Silver
    variant 3 Gold
```

and the build version is **`0xc211ce2f3414aa7c`**. `ShipConfig` gets no
`block` line: it declares no bounded array, so its block form lays nothing out
of line and its projection is the prologue the header already carries plus the
`record` line's own layout. The same unit with no table at all — its four
header lines, that same protocol id, and nothing else — is
**`0xe2eeb510ec9621cb`**, deliberately not equal to the protocol id, so no
caller can substitute one for the other by accident.


### 20.3 The compiler computes the layout; the backends must assert it

**§19.3's model, unit-wide.** A record's layout is the C ABI's natural one:
each field at its own alignment, the struct's alignment the greatest of its
fields', the size rounded up to it. The compiler derives every offset and size
from the declaration and folds them into the build version, and each backend
OWES code asserting that ITS OWN compiler agrees — C++ `static_assert`s on
`sizeof` and `offsetof`, C# blittable storage (`StructLayout(Sequential,
Pack = 1, Size = N)`) with generated padding and a once-run layout check
(§19.3 states both in full). **A disagreement is a BUILD ERROR on the side
that disagrees**, naming the type, the field, the expected offset and the one
its compiler produced.

**Those asserts are in the tree, for EVERY COOKABLE RECORD of every unit that
declares a table.** C++ `static_assert`s each block projection's and each
row type's `sizeof`, `alignof` and every field `offsetof`, and asserts the same
three facts for every record a file declares — the records a cook's region is
laid out from, asserted in the file that DECLARES them, which is the same rule
§7 gives a member's walk. C# emits blittable storage with generated padding and
asserts the block facts under the managed model in a once-run check that THROWS
rather than failing the build, because C# has no `static_assert`, and asserts
every record of the unit's cook closure in `TableCookLayout`. A VALUE-ONLY unit
is not an exception: its Table header carries the asserts like any other, and
the bytes they added are pinned by the zero-cost gate rather than excluded from
it (§2.2). The model is not self-evidently right either — on 32-bit System V
`alignof(uint64_t)` is 4, not 8 — which is precisely why it is asserted rather
than assumed, and why "it never reaches a cook" is a claim the asserts make
true rather than one the model makes true on its own.

**That is what replaces an ABI term in the id.** ABI drift as a runtime
refusal — a cooked file that opens as NULL and degrades to a wire load nobody
sees — becomes a build failure, which is louder, earlier and cheaper, and it
is the only way Y can be settled before a binary exists. The price is stated
rather than hidden: the compiler's layout model is committed to **every
cookable closure**, not only to blocks and rows, so a platform the model gets
wrong fails to build instead of degrading.


### 20.4 What moves it, and what does not

**It MOVES on:**

- any edit that moves the unit's protocol id — every fact SPEC.md §3.1
  includes. **A type edit moves both ids**, and that is the whole of the
  overlap between them;
- **a record added, removed or renamed**, and an enum or union likewise;
- any edit that moves an offset, a size, an alignment, a KIND or a field's
  effective WIRE ID: a field added, removed, retyped or reordered; a string or
  `bytes` capacity changed; a field moved between `T`, `?T` and `*T`; a nested
  record changing shape; a field renamed WITHOUT `was`;
- **any edit that changes WHICH declaration a field names**, even between two
  of identical shape: a nested record swapped for a same-`sizeof` other, an
  enum-keyed array's KEY enum swapped, an enum field retyped to the raw
  integer it rides as, a union arm's payload swapped. Each keeps the kind, the
  size, the offset and the wire id and changes what the bytes MEAN, and each
  moves a `type=`, `enum=`, `union=`, `key=` or `payload=` token;
- **a declared MAXIMUM raised or lowered.** It is a `bound=` on the field
  line, and the inline storage it sizes moves with it: the array field's
  `size` grows, every later field's `offset` moves and the record's `sizeof`
  moves with them. It could not be excluded even if a block would rather it
  were — a build that read another's cook under a larger declared maximum
  would read past the region — and under one same-build entry point per form
  there is nothing for the exclusion to buy: `BlockOpen` refuses this edit
  exactly as it refuses an appended field (§19.4), and both sides regenerate.
  The block's own `slot` lines still do not move, because a triple is sixteen
  bytes whatever the maximum is;

- **a specified default changed, added or removed**, and **a declared range
  tightened, loosened, added or removed** — group 3, and the reason group 3
  exists;
- **an `enum` variant inserted, removed, reordered or renamed**; a `union` arm
  inserted, removed, reordered or renamed. **An enum a keyed array KEYS moves
  layout as well as meaning**: the array's extent is `E.Max` (§2.4), so
  inserting or removing a variant moves the field's `bound=` and `size`, every
  later field's `offset`, and the record's `sizeof` — the same shape as a
  declared maximum moving, arrived at through the enum;
- **the target's BYTE ORDER**;
- a `ProjectionVersion` bump, or a bump of the cook projection's own form
  version (§20.2).

**It does NOT move on:**

- comments, whitespace, file names, file layout, and declaration order ACROSS
  records — the projection sorts records by name, so only order WITHIN a
  record is a fact, and it is a fact because it produces the offsets;
- **a `was` rename** — a layout line carries the wire id, so the rename pair
  moves nothing, which is §7's stated obligation: a `was` rename must not
  invalidate every cooked file in existence;
- **any `flags` edit — a variant renamed, inserted, removed or reordered.** A
  mask rides as raw bits and a load copies it verbatim: the page states no
  mask-to-width rule and §4's report has no counter for a bit outside the
  declared set, so no cook byte moves. What a reorder changes is what the bits
  MEAN, which is §4.1's silent class, answered by its discipline (append at
  the end) and by §18's baseline refusing the edit at compile time. Putting it
  in the id would be over-refusal on a fact no cook's bytes depend on. (The
  field's storage WIDTH is a different thing and it is a layout fact.);
- **a GUARD added or removed** around an existing field. A load finds a field
  by its id whatever branch now encloses it, with every counter zero (§4.1);
  the cost lands on the next WRITE, which is the baseline's case and not a
  cook's. A reader who has just read §4.1 will look for this row, so it is
  here;
- **the `json` key** (§16.4) and anything else the TEXT form owns. A cook is
  produced from the WIRE file, and §7 defines the tuple's other half as that
  file's hash;
- **baseline-only facts** (§18): whether a `tables.baseline` exists at all,
  its recorded history, a `--reason`, its rendering version. §18 is untouched
  by this section and untouched by the build version;
- **a `flags` field's REFERENT** — swapping one flags declaration for a
  same-width other. There is no `flags=` token, and that is deliberate: the
  slot holds a raw mask and a load copies it verbatim, so no cook byte moves
  (§20.1);

- **adding a target LANGUAGE**, which changes no fact the compiler folds.

### 20.5 The connect gate is the protocol id, and nothing else

**Peers connect on equal protocol ids; they may differ in build version.**
The type wire is same-or-refuse because nothing could save two peers whose
hardcoded bits disagree. Tables between the same two peers ride the tolerant
wire (§4), where any reader reads any data and the differences are reported —
so a build-version difference is not a connection question, and §10's
independence is the reason: a table edit never forces a lockstep redeploy.

**Cooked assets are LOCAL, on both sides.** Each peer loads assets cooked to
its own build version, out of its own store, and neither ever sees the
other's. Nothing about a cook crosses a connection; what crosses builds is the
asset's WIRE form, through tooling, which is the form that carries every
version.

### 20.6 The tuple

**`(asset hash, build version)` and nothing finer.** Tooling produces a cook
under it, the store is indexed by it, and the game asks for it. §7 defines the
asset hash as the hash of the WIRE file the cook was produced from, which is
what makes the pair well defined.

**A new build version is a new cook, and that is the model rather than a
cost.** The work is offline and the store absorbs it: a build cache exists to
make re-cooking cheap, which is why the cooking side is a build cost and not a
runtime one (§7). Nothing finer than the unit is keyed, and nothing needs to
be — a finer key buys a smaller re-cook and pays for it with a second id, and
this design has two ids in total.

**The cooked header carries the build version, and `Open` is a match** — the
check §7 states, in one place, with the build version among them. On
a match the bytes ARE what a build at this version wrote, so there is nothing
to validate and nothing to fix up (§7). A hit in the store under the right
tuple therefore cannot be refused by `Open`, save on a corrupt or truncated
file, where it returns NULL and the caller falls back to a wire load — the
path that carries every version.

**The block form carries the same id in its prologue** (§19.1), and `BlockOpen`
checks it the same way. A cook and a block are still different accelerators
and a runtime still cannot accept one where the other was written: what
separates them is the MAGIC, which is where a form's identity belongs, and not
a second digest.

### 20.7 The surface

- **`schema build-version <unit>`** prints the build version.
- **`schema build-version --facts <unit>`** prints the cook projection of
  §20.2, in the tradition of `schema projection`: the facts are printable,
  readable and diffable, or a fact missing from them is invisible.
- **Every backend emits one constant** beside `ProtocolId` — `BuildVersion` /
  `BUILD_VERSION`, in that backend's own naming convention (SPEC.md §6.1),
  as a literal, because the compiler settled it.
- **It never enters a projection, and nothing derived from it does.** The
  wire-shape projection (SPEC.md §3.1) and §20.2's cook projection are
  INPUTS; the tables baseline (§18) is independent of it in both directions.
  A derived id that fed back into what derives it would be a cycle, and the
  protocol id in particular must stay the type wire's alone.
- **`schema id` is unchanged**, in every respect. It prints the protocol id,
  it depends on the wire-shape projection and nothing else, and no table fact
  reaches it.

### 20.8 Held by test

- **The independence pair, both directions** (§10's test, extended): a table
  edit moves the build version and does not move the protocol id; a type edit
  moves both. The second is the one that must never regress.
- **The meaning group's negative controls**, each isolating a fact no layout
  line sees: a specified default changed; **a declared range tightened**; **a
  `bits(N)` narrowed within one storage width** — the case where the implied
  range moves and the storage kind, the size and the wire id do not; an `enum`
  variant renamed; two `enum` variants swapped; **a `union` arm RENAMED** —
  the rename and not a reorder, because SPEC.md §3.1 already puts union
  variant ORDER in the protocol id, so a reorder would pass through group 1
  with group 3's union fact deleted.
- **The layout group's own controls**: a field's KIND changed with its width
  unmoved; a field's offset moved with the record's `sizeof` unmoved; **a
  declared maximum raised** — which moves it, and whose §19.4 consequence is
  tested there.
- **The REFERENT controls, each a same-shape swap that every other fact
  survives** — these are the ones a digest without `type=`/`enum=`/`union=`/
  `key=`/`payload=` passes in silence: a field retyped between two records of
  identical `sizeof` and `alignof`; a keyed array's KEY enum swapped for
  another of the same variant count; an enum field retyped to the raw integer
  it rides as; a union arm's payload swapped for a same-shaped record. Each
  must move the build version with kind, size, offset and wire id all
  unchanged.

- **The exclusions, each with the edit that proves it**: a `was` rename moves
  nothing; **a `flags` variant REORDERED moves nothing** — the row the
  discipline of §4.1 and the baseline of §18 own instead; a `flags` variant
  renamed moves nothing; **a flags field's REFERENT swapped for a same-width
  other moves nothing** — the negative control for the missing `flags=` token;
  a guard added or removed moves nothing; a `json` key changed moves nothing;
  a comment, a file split and a reorder of two records' declarations move
  nothing.

- **The inclusions the sort order could hide**: a record renamed, added or
  removed moves it; the target byte order moves it.
- **The worked example of §20.2 is a golden**, projection text and digest
  both, so a port reproduces the text and not only the number.
- **Goldens over the corpus** (SPEC.md §7.2 gate 2's sibling): `schema
  build-version` pinned as an exact value per unit, and the `--facts` text
  pinned beside it, so any change to how it is computed breaks every pinned
  value loudly.
- **The layout model is held by its asserts, not by agreement**: perturb one
  field's offset in the compiler's model and BOTH backends go red; that is
  §19.3's test obligation, now owed for every record in the closure and not
  only for blocks and rows.
