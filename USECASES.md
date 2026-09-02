# schema — supported use cases

**"We aim to build the best cross-language data type system for games."** —
*"This includes save games, tooling, cooking to runtime efficient structures
and so on."*

The use cases schema is designed for. Find your situation in the list, and
each entry names the **form** that serves it, the **versioning contract**
that form carries, and the **proof** it has today — a corpus case, a gate,
or a dogfood — or says plainly that it is designed and not yet proven, with
the issue that tracks it.

**The four forms**, each defined in the specs and referenced by section
below:

| form | what it is | contract |
|---|---|---|
| **the type wire** (SPEC.md) | bitpacked, positional, generated in nine languages | same-or-refuse by protocol id |
| **the table wire** (SPEC-TABLES.md §3) | neutral bytes, ids and kinds and lengths | tolerant, every difference reported |
| **the cook** (SPEC-TABLES.md §7) | a table's region written for one build, memory-mapped and pointed at | one exact build version |
| **the block form** (SPEC-TABLES.md §19) | a fixed table's rows laid out of line at a fixed pitch, both sides generated | one exact layout, asserted at compile time in both languages |

Entries 1–8 are the owner's list. Entries 9 and 10 are additions from the
same pages, marked where they begin.

---

## 1. Client and server on the same build, across languages

A client and a server that deploy together, written in different languages,
must agree on every bit of every packet.

- **Form** — the type wire (SPEC.md §4), generated for C, C++, C#, Dart,
  Elixir, Go, Java, JavaScript and Rust (SPEC.md §6).
- **Contract** — same-or-refuse by protocol id (SPEC.md §3). The id is the
  low 64 bits of SHA-256 over the unit's wire-shape projection; two sides
  holding different ids do not exchange a byte, and nothing on the wire
  carries a version.
- **Proof** — `examples/` generates to all nine backends under `make`, and
  every backend byte-compares its output against the C++-pinned goldens in
  `testdata/wire/`; the same holds for the fixed-point and 128-bit unit in
  `examples128/`. The id's two directions — an edit that moves no bytes must
  not move it, an edit that moves bytes must — are held by
  `internal/check/projection_test.go`. The 70+ case diagnostics suite holds
  the refusals.

## 2. Messages between tooling, websites and backends on their own release cycles

Tools, a website and a backend exchange data structures and messages, and
none of them ships on the client and server's release cycle, so protocol-id
versioning cannot serve them.

- **Form** — the table wire, fixed-size tables (SPEC-TABLES.md §2, §6.1). No
  pointer, no arena, no allocation on either side.
- **Contract** — tolerant, with a report (SPEC-TABLES.md §4). An unknown
  field is skipped by its length and counted; an absent field takes the
  reader's declared default; a rename survives through `was` (§5); an
  out-of-range value is clamped and counted. Every event lands in the read
  report, and nothing is fatal in either direction.
- **Proof** — the two-generation evolution corpus (`test/tables/V1.schema`
  and `V2.schema`) reads both directions under `make`, over fields, enum
  variants, union arms and array bounds; the C# leg byte-compares the same
  C++-pinned table goldens in `testdata/wire/tables/`, so both languages
  read the bytes the other writes. The MESSAGE SET form — a union whose arms
  are tables, so adding a message is adding an arm (§2.6) — is stated on the
  page and refused by the compiler today; it is tracked by **#258**.

## 3. Save games

A save file written by a build nobody has any more, read by a build its
writer never saw, years apart — and the property has to survive every edit
made to the schema in between.

- **Form** — the table wire, a fixed root table (SPEC-TABLES.md §2, §12).
- **Contract** — tolerant with a report, plus the committed **tables
  baseline** (§18) for the two edits the wire cannot report. §4.1 names them
  in full: a specified default changed, added or removed, and a flags
  variant inserted, removed, reordered or renamed in place. A
  `tables.baseline` file in the unit refuses both at compile time, and
  moving it requires `--update --reason`, which appends a dated entry to the
  file's own history.
- **Proof** — the tolerance is the same corpus entry 2 cites. Each baseline
  refusal class carries a fixture pair and its negative control, the warn
  class warns without refusing, and the projection regenerates
  byte-identical (§18.6); `tables/examples/tables.baseline` and
  `tables/pointers/tables.baseline` are committed over the corpus. No real
  save game has been carried across builds yet — the machinery is proven,
  the use case is not dogfooded.

## 4. Assets and data built by tooling, cooked to a build version

Tools build asset and data files; a cook converts one to a specific build's
layout; the game memory-maps it and points at it with minimal fix-up.

- **Form** — the table wire as the **format of record**, with the **cook**
  (§7) beside it as an accelerator, produced per asset where load time asks
  for one. The pair is the rule: a huge mesh or texture catalogue is a cook;
  a config small enough that its load time does not matter stays the wire
  and keeps the tolerance that comes with it.
- **Contract** — the wire's tolerance for the record; **one exact build
  version** for the cook (§20). The build version is a compiler-settled
  64-bit digest over every fact a cook's bytes depend on — the type wire's
  protocol id, every record's layout keyed by wire id, kind and referent, the
  facts that decide what a load PUTS in a slot, and the target's byte order —
  so schema drift refuses at `Open` and ABI drift is meant to fail the build
  (§20.3). `Open` checks the magic, the byte order, the build version, every
  reserved word zero, the two part lengths and the base alignment — that is
  the whole check, and it is the runtime's only entry point — returning NULL
  on any mismatch and leaving the caller to fall back to a wire load, which
  carries every version. Validating a file whose provenance the caller does
  not trust is `schema cook-check`, a tool over the same descriptors.
- **Proof** — the wire half is **proven**: `Config.bin` and `Assets.bin` are
  each one fixed root table down to the leaves, the corpus carries a
  config-format example holding gate 1 (§12), the C# backend reads the same
  bytes the C++ tools write, and the dogfood ran a real game's two files
  through the wire end to end: 803 values came back byte-identical, and every
  injected bit flip was refused rather than read as data. The cook half
  is proven in the C++ corpus for what exists — `CookMeasure`, `Cook` and
  `Open` round-trip over the pointer unit under `make` — while addressing a
  cooked artifact by **(asset hash, build version)** is designed and not yet
  built: the build version is **#292**, `schema cook-check` is not built, and
  the variable class's flat node encoding (§3.1) is spec with its emitter
  tracked by **#251**.

## 5. Render data written and read across two languages at runtime

A C++ producer writes a block of per-frame render data and a C# consumer
points at it and reads rows in place — large structures, both directions,
sixty times a second or better.

- **Form** — the **block form** (SPEC-TABLES.md §19): every fixed table has a
  third projection beside its wire and its cook, in which the table's own
  bounded arrays are laid out of line at a fixed pitch and the instance at
  the front of the block carries, per array, an `(offset_of, count, stride)`
  triple. Nothing declares it; it is emitted on the side, and a consumer that
  does not include it pays nothing.
- **Contract** — one exact layout, held as a **two-language layout contract**
  (§19.3). The compiler derives every offset and size, C++ asserts them with
  `static_assert` and C# with a blittable `Sequential` struct plus generated
  padding fields and a generated size and offset check, so neither side can
  drift silently. `BlockOpen` matches the **build version** (§20) and is the
  only entry point there is: a block is same-build, so an edit that moves the
  version refuses and both sides regenerate. The block carries no field ids,
  no lengths, no elision and no read report — §4's counters do not exist here,
  because none of the events they count can occur.
- **Proof** — designed, not yet built, and the layout asserts above are the
  largest missing piece: the tree carries no `sizeof`/`offsetof`
  `static_assert` and the C# fixed class is not blittable yet (§20.3). §12.1
  states the gate: both sides generated, the multi-threaded fill held by a
  refuser, and the speed of the hand-written scatter it replaces.
  **#288** tracks the gate; **#287** tracks the C# blittable struct form the
  consumer needs.

## 6. JSON parsed and packed into tables

Author or export data as JSON text, transform it into a binary,
version-friendly structure, and read that structure from more than one
language.

- **Form** — the **text form** (§16) and `schema pack` / `schema unpack`
  (§17), producing the table wire. A directory tree mirrors the root table's
  shape; the text in it is §16's; the output is §3's bytes and nothing else
  — no magic, no hash, no envelope.
- **Contract** — the table wire's, unchanged: tolerant, with the report
  aggregated across the tree and a non-silent report exiting non-zero, so a
  build pipeline can fail on it.
- **Proof** — **proven**. Three goldens hold the compiler's own Go pack
  engine and the C++ generated walk to one wire and one text (§17.1); the
  hostile-value corpus is a two-sided differential over 67 trees whose
  reports must agree counter for counter and whose bytes must agree byte for
  byte (`tables/pack/hostile-values`, **#272**); `unpack` → `pack` is
  byte-stable across both tree shapes, with §17.3's UTF-8 carve-out pinned
  rather than assumed. The generated readers exist in C++ and C# (**#267**,
  **#266**), and the dogfood packed and unpacked a real game's config and
  asset trees with 803 values byte-identical on the round trip.

## 7. Reflection and introspection over tables and types

Walk a value's fields by name at runtime — so property editors, tree
editors, dumps and diffs build themselves out of the declarations instead of
carrying per-table UI code.

- **Form** — the reflection descriptors (§8), generated for every table in a
  unit's closure beside its codecs, with no RTTI and no schema files on hand
  at runtime.
- **Contract** — the table wire's. The descriptors describe the same
  declaration the tolerant codec reads, so a tool built on them and a game
  reading the same bytes never disagree about what a field is.
- **Proof** — the descriptors are generated and used today, in C++ and in C#
  (**#267**, **#266**): field name, wire id and kind, storage offset and
  element size, array bound and count companion, declared bounds, branch
  guards, the nested table's descriptor, and the enum and union vocabularies
  with the id each variant rides under. The JSON text form (§16) and `schema
  pack` (§17) are built on them, which is the working proof that the surface
  is enough to walk any table generically. **No editor is built on them
  yet** — a generic dump and diff tool is a named follow-on (§15).

## 8. Debug visualization of live data in the engine

Walk and display a running game's table data in the engine — an inspector
over what the game is actually holding this frame, not over a file.

- **Form** — the same reflection descriptors (§8), read against live
  storage rather than against bytes on their way in or out.
- **Contract** — none is needed. The descriptors carry the field's storage
  offset, element size and count companion, so a visualizer reads the
  instance a game already has in memory; no wire, no report, no version
  boundary is crossed.
- **Proof** — the descriptors and the generic walk exist and are exercised
  by the text form (entry 7's proof). **No visualizer is built on them
  yet.** Types have no such surface today: the type wire ERASES the
  descriptor — both ends hold it at compile time and only its hash remains —
  and giving every declaration a generated **view** so types can be walked
  the same way is designed and not built, tracked by **#157**.

---

## Rowan's additions

The two entries below come from the same pages and are not in the owner's
list.

## 9. Pointer-bearing data structures saved and loaded whole

A scene, a tree or a graph built in memory — with sharing, with recursion —
written out and read back as the same structure, not as a copy per
reference.

- **Form** — the table wire, **variable class** (§2.1, §2.2, §6.2). One
  pointer anywhere in a table's by-value closure derives the class: built
  through a lock-free arena, `Lock()`ed once into a single packed region,
  and read through a root pointer, with references self-relative so the
  region relocates by plain `memcpy`.
- **Contract** — tolerant with a report, like every table. Identity is
  preserved end to end: on the wire every reachable node is written once
  into a flat node table and a pointer rides as a `u32` index under kind
  `17` (§3.1), so two references to one node are one node and a chain's
  length is not a depth.
- **Proof** — partial. The pointer corpus (`tables/pointers`) builds,
  locks, saves, loads and cooks in C++ under `make`, with the save-time
  cycle refusal and the stale-leak and zero-cost gates beside it. §3.1's
  flat node encoding is the landed spec; the emitter still writes the
  nested form, and moving it over is tracked by **#251**. The variable
  class exists in C++ only — C# refuses a pointered unit by name (§11) —
  and the variable class's text form is a named follow-on (§15).

## 10. Config delivered from a backend into a running server

A game server takes its configuration from a backend and reloads it without
redeploying the build, so the data crosses a boundary the protocol id does
not cover.

- **Form** — the table wire, a fixed root table — entry 4's `Config.bin`,
  read from a backend rather than from disk. The bytes are the same bytes;
  the table wire imposes no envelope, so the transport is the caller's few
  lines around them (§1, §17.3).
- **Contract** — tolerant with a report. This is the case the tolerance is
  load-bearing for: the backend and the server build and deploy separately,
  so a config written by one build is read by another as a matter of course,
  and the report is what says which fields the reader could not name.
- **Proof** — **proven** in the dogfood: a real game carries its config from
  its backend into the running server on this wire, 803 values byte-identical
  across the hop and every injected bit flip refused. The gate
  behind it is entry 4's — the fixed root, the evolution corpus, and the
  C++/C# shared goldens.

---

## The standard this list is measured against

Everything you would do with FlatBuffers or Protocol Buffers should be
expressible here — the tolerant, evolving, self-describing-enough wire is
the table wire, and entries 2 through 10 are where each of those
capabilities lands. What schema adds beside it is game-specific and is not in that class
at all: **bitpacked types under a protocol id** (entry 1), where a bound is
part of the type and the wire cost follows from it; **cooking** (entry 4),
where an asset is converted to one build's exact layout and pointed at
rather than parsed; and **the block form** (entry 5), where a per-frame
structure crosses a language boundary at a generated, compile-time-asserted
layout.

The claim is held to this page's own standard: where an entry says
**designed, not yet proven**, that capability is not being claimed. Today
those are the message set (#258), the cook key (#292), the block form (#288,
#287), and the flat node encoding's emitter (#251).
