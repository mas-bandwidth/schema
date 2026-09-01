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
