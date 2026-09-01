# tables POINTER SEMANTICS — ROUND-LOG

One line per unit: what landed, the ruling it serves, the decision taken. A
future resume reads the round's state from this file plus the branch commits.

Base: origin/main @ 5e11117 (the tables-framing PR #241 had already merged, so
no rebase debt).

## Owner rulings this round (the design's authority)

1. "types can remain value semantics. tables should ALLOW pointer semantics" —
   "we can't be a generic system if we don't have pointers to tables."
2. Spelling: "literally same C++-like syntax for pointers is fine" → `next *Node`.
   Table-to-table only; pointer-to-`type` refused by name.
3. Modes are COMPILER-DERIVED, never declared: "i wouldn't want to manually have
   to specify this… the compiler can work it out and go, oh it's the variable
   table mode."
4. "i think variable tables need to live in a growable array."
5. Const vs mutable is a LIFECYCLE: "maybe you 'lock' a table and it is constant
   from that point forward… since how else will you construct Assets.bin."
6. "mutable vs. non-mutable tables may be different at runtime."
7. "the builder needs to be able to be multithreaded" ("That's very flatbuffers");
   then "I prefer lockless if possible."
8. Naming: `<Name><Verb>` — `ChatMessageSave`, `ChatMessageLoad`,
   `ChatMessageMeasure`, `ChatMessageBuilder` with `Alloc<T>()`/`Lock()` methods.
   Shared symbol table ratified: "do tables and types share the same symbol
   table then, I vote yes."
9. Zero-cost gate: "Make sure we don't pay this cost when we have nice, small
   messages… when the table is inferred to be value types only."
10. "Let's assume larger types will probably have pointers to things."
11. File-format-scale tables "can't be a struct" — pointer-bearing tables are
    never held by value; their const surface is region + root view.
12. "you don't need to recreate sizeof" — no generated size constants of any
    kind. `sizeof(X)` is the memory answer, `XMeasure(value)` the wire answer.

## Units

- UNIT 1 — the name-first rename (ruling 8). `TableMeasureX` → `XMeasure`,
  `TableSaveX` → `XSave`, `TableWriteX` → `XSaveBody`, `TableReadX(reader)` →
  `XLoadBody`, `TableReadX(buffer)` → `XLoad( value, buffer, bytes, &report )`
  (value first, report by pointer, per the owner's approved snippet),
  `TableTypeX` → `XTableType`. The claimed-name registry grew the full
  name-first suffix set for EVERY closure member — including the mutable-life
  suffixes a value-only table does not emit — so a table gaining pointers later
  never turns a legal declaration into a collision. Deliberate asymmetry
  recorded: the TYPE wire stays verb-first (`WriteX`/`ReadX`), tables are
  name-first; the verb position tells a reader which wire the call site is on.
  Nine legs + tables green after the rename.

- UNIT 2 — the language: `next *Node` (rulings 1, 2, 3). Scanner already had
  `Star`; the parser accepts it in type position and the CHECKER owns every
  rule, so a bad pointer names the real problem instead of "expected a field
  type". Refused by name: pointer to a `type`/enum/flags/union (the founding
  line — types remain value semantics), a pointer outside a table body, an
  array of pointers (§12 follow-on), a specified default on a pointer (null is
  the only value one could name). The by-value composition-cycle refusal now
  EXEMPTS pointer edges — `table Node { next *Node }` is legal and finite,
  while `table Node { self Node }` still refuses. Mode derivation lives in
  `ir.VariableTables` as a least-fixed-point over BY-VALUE edges only:
  FIXED-SIZE = no pointer in the by-value closure; VARIABLE-LENGTH = a pointer
  anywhere in it, propagating up through by-value nesting and bounded arrays.
  `ir.PointerTargets` names the tables that need an allocation surface.
  Formatter: the pointer star binds tight to its target (`next *Node`) while
  multiplication keeps its spaces (`max = K * 2`) — type position at index 1
  is what tells them apart. The C++ emitter REFUSES a pointer-bearing unit for
  now, loudly, so the tree stays green until unit 3 emits the backend.

- UNIT 3 — the C++ variable-length backend (rulings 4, 5, 6, 7, 9, 11). The
  MUTABLE form is a segmented slab arena: equal-size 4 MiB segments whose count
  saturates the u32 reference space exactly (1024 -> 4 GiB), handed to workers
  in 64 KiB slabs with ONE compare-exchange per slab and zero atomics per node.
  Nodes are born at final offsets and segments never move, so a `T*` from Alloc
  stays valid while other workers allocate and while the arena grows. The
  REJECTED model is named in the emitted comment: one buffer under a lock grown
  by realloc, whose realloc moves memory under a mid-write worker. Slack: one
  slab tail per worker plus one per segment (<2% + threads x 64 KiB).
  `Lock()` is one-way and IS the compaction: the arena packs into one exact
  region, root at base, references rewritten SELF-RELATIVE — so a deref is one
  add with no base pointer and a region relocates by pure memcpy with zero
  fix-up. Lock's output and Load's output are the SAME representation, one view
  API. Aliasing is not preserved anywhere (two pointers to one node pack and
  ride as two), stated in the emitted comments and the spec.
  Wire: a pointer rides as a nested table body — framing IDENTICAL to a
  by-value nesting, so a field can change between the two without moving a
  byte. Null elides; a non-null pointer rides even when the pointee is
  all-default, or null and empty would be one value. Depth cap kTableMaxDepth
  = 128 on every walk (save, load, cook, open): a data cycle is an ERROR, never
  a hang. Cooked form: 32-byte header carrying magic (which is also the
  byte-order check), a layout id digesting the schema's packed-layout facts
  mixed with this build's own sizeof for every closure type, and the region
  length; `<Name>Open` runs a bounds walk over the REFERENCE GRAPH only —
  pointer slots and the count companions that bound a traversal — before
  handing out the root. Packed references are strictly forward, so that walk is
  cycle-free with no visited set.
  ZERO-COST GATE, PROVEN: the whole pointer-free corpus (tables/examples,
  test/tables/V1, test/tables/V2) regenerates BYTE-IDENTICAL to the
  rename-applied baseline at 974ff0f — `diff -r` clean. Three leaks were found
  and closed to get there: the descriptor's `is_pointer` column, TableTypeInfo's
  `variable` column, and a reworded relocatability comment are now emitted only
  in a unit that actually has pointers.

- UNIT 4 — corpus, tests, gates. The pointer corpus lives in its OWN unit
  (`tables/pointers/Graph.schema`, package graphdemo) so `tables/examples`
  stays pointer-free and keeps proving the zero-cost gate. Graph carries a
  value-only table (Meta), a value-only POINTER TARGET (Settings), a
  self-recursive list (ListNode), a tree (TreeNode), a variable table nested by
  value (Layer) and a bounded array of them — the derived mode has to get all
  six right in one file. The evolution pair `test/tables/P1|P2.schema` turns one
  field from a by-value nesting into a pointer and gives its target a pointer of
  its own: both directions read, because a pointer's framing IS a by-value
  nesting's.
  Battery (12 new cases, all in test/tables/main.cpp): lifecycle
  (build/measure/save/lock/relocate-by-memcpy/load/re-save byte-identical),
  exact-capacity across a pointer graph, Lock layout determinism (two builds
  memcmp equal; two cooks byte-identical), aliasing (two references, two nodes,
  in the packed form AND across the wire), the data-cycle refusal (measure,
  save, cook and Lock all refuse; nothing recurses away), the depth cap at and
  past kTableMaxDepth, arena growth across 200k allocations with a pointer held
  the whole time, four workers single-threaded plus a real four-thread
  concurrent build joined before Lock, the cooked form (cook/open/point,
  cook-open-cook stable) with a nine-case refusal battery (magic, layout id,
  truncation, sub-header size, unaligned base, out-of-region reference, BACKWARD
  reference, misaligned reference, out-of-bound count companion), evolution both
  directions including load-into-builder, the null-vs-empty-pointee distinction,
  and reflection (derived mode surfaced, is_pointer, the target descriptor,
  a self-referential pointer resolving to its own type).
  NEGATIVE CONTROL run: inverting the aliasing assertion turns the leg red at
  the expected line and exit 1 — the battery is wired in, not decorative.
  Standing gates: `make tables-zero-cost` greps the pointer-free corpus's
  generated headers for every pointer-machinery symbol and fails the build on a
  hit; it runs inside `make test`. Go-side: TestZeroCostForValueOnlyTables,
  TestPointerSurfaceEmitted (per-TABLE, not per-file: a value-only table sharing
  a pointered unit still gets no builder), TestPointerGenerationDeterministic,
  TestLayoutIdMovesWithTheSchema.

- UNIT 5 — SPEC-TABLES.md, USAGE.md, README. Spec gained §2.1 (pointers, with
  every refusal named), §2.2 (the mode derived as a least-fixed-point over
  by-value edges, the zero-cost gate stated as a gate, and the stated
  assumption that size and mode correlate), §3.1 (the pointer wire: tree
  semantics loud, null elides, a non-null pointer always rides, pointer and
  by-value nesting wire-identical, the depth cap and what it costs), a NEW §6
  (the two lives: value surface vs Builder→Lock→region, the two reference
  encodings, the threading contract, both load paths, the authoring-vs-reading
  allocation split), a NEW §7 (the cooked form — accelerator not archive,
  layout id, the bounds walk as reads-validate-always, every refusal, alignment,
  endianness refusing in v1). §8 reflection gained the pointer kind and the
  derived mode; §9 relocatability now covers both forms and states measure==save
  at exact capacity as a hard invariant; §11 grew the pointer, cycle, depth-cap
  and cooked-file refusals; §13.1 records the rulings verbatim; a NEW §14
  weighs all four builder-storage models with lock+realloc REJECTED and the
  moved-node hazard named; §15 grew graph/DAG identity, arrays of pointers,
  lifting the depth cap, cross-endian Open, the fallback loader, and the generic
  JSON walk over the descriptors. Sections 6..15 renumbered; every code citation
  updated to match. USAGE gained the pointer, builder/threading and cooked-form
  sections; README gained the pointers bullet.
