# Schema against Protocol Buffers, FlatBuffers, Cap'n Proto and Avro

The standing comparison. It is written to be checked: every cell about a
competitor is cited to that project's own documentation, every cell about
schema is cited to a specification section, one of the repository's registers
or an issue, and the same test is applied to all five columns. Row-level
evidence for FlatBuffers and Protocol Buffers lives in
[COMPARISON-TABLES.md](COMPARISON-TABLES.md) and those cells cite its rows
rather than repeat them; the packet-size measurement is
[COMPARISON.md](COMPARISON.md); the versioning promises are
[VERSIONING.md](VERSIONING.md). This page adds Cap'n Proto and Avro, a
versioning group, and the summary a reader can take in ninety seconds.

## What each of these is for

No straw men. Each of the four makes a trade on purpose, and the trade is the
thing to compare.

**Protocol Buffers** identifies a field by a number you assign and never
reuse, carries every field with a tag, preserves the fields it does not know
through a read and a rewrite, and disclaims byte stability across builds. That
is the right shape for a service API versioned independently over years by
teams that never deploy together. Its own guidance is honest about the costs:
never change a field's type, never change a default, reserve every retired
number.

**FlatBuffers** puts every scalar at its declared width and alignment behind a
vtable so that a reader touches one field without parsing the buffer. The
buffer is its own cooked form: little-endian, position-independent, build-
independent, memory-mappable. The price is that a buffer from an untrusted
source needs a separate verification pass before it is safe to touch, and the
verifier does not ship in every one of its thirteen languages.

**Cap'n Proto** reads in place too, and adds an RPC system. Its message is a
tree by construction, one pointer per object, which is what lets its reader
bound traversal against a hostile message. Its scalars are stored XOR their
declared default, so "unset" and "set to the default" are the same bytes and
a default can never change. Those are not gaps; they are the encoding.

**Avro** carries the writer's schema with the data, in the container file or
as a fingerprint on each object, and resolves it against the reader's schema
at read time. Nothing is elided, so a changed default can never reinterpret
stored bytes. A mismatch fails the whole read, on purpose, because Avro's
users are pipelines where a wrong row is worse than no row.

**Schema** is for a game. A `type` produces a packet wire where the declared
bounds decide the bits and both sides ship together, refusing each other on a
protocol id if they do not match. A `table` produces a tolerant wire where a
field is identified by the hash of its name, a reader counts what it could
not understand and never fails on data from another build, and an opt-in
committed baseline refuses at compile time the edits no reader could report. The same
declaration cooks to a memory image the runtime opens with a header check and
a pointer, and lays out as a row block one language writes and another reads
at frame rate. One language, nine targets, one conformance corpus every target
is held to, case by case, with what a target cannot yet answer named rather
than passed over.

When to use theirs, in one line each: a service that versions independently
of its callers is Protocol Buffers' case; in-place access to a large buffer
you will not deserialize is FlatBuffers' or Cap'n Proto's; a data pipeline
where the writer's schema must travel with the record is Avro's.

## How to read the matrix

**One test for all five columns, and a row is answered from the wire the row
is about.** A cell is ✅ when the feature exists in the format **and** in all
of that project's official language implementations, 🔶 when it exists but
only some of them carry it or it carries a material limitation the footnote
names, ❌ when it is absent or declined on the record, and ? when the
project's own documentation neither makes the claim nor denies it. **Absence
from a complete list in a project's own documentation** — a scalar table, a
declaration list, a set of evolution rules — **is ❌, not ?**, and that
reading is the same in every column.

**A feature that is decided but not built is ❌**, with the issue in the
footnote. 🔶 describes something shipped that falls short, never something on
the way; the four competitors are scored on the format they ship and so is
this one.

**Schema has two wires, and each row says which one answers it.**

- **Declarations and the compiler** are nine languages deep, and a row is ✅
  when CI holds all nine.
- **The packet wire** is ✅ when all nine carry the construct.
- **The table wire** is scored against the repository's own registers:
  [PORTING.md](PORTING.md), the techniques register, which carries one cited
  cell per language and an issue behind every gap, and
  [the conformance contract](../test/conformance/README.md), which says which
  surfaces and which cases each port answers. The **fixed class** is carried
  by all nine on the wire, text, cook-open, block-read and descriptor
  surfaces. The **variable class** (pointers), the **message class**, the
  **wide scalars** and the runtime cook's **write** side are the C++
  reference's alone, and the block's **builder** is C++ and C — the eight
  ports answer ABSENT on twenty-eight wire cases and twenty-six text ones,
  and that matrix is the completion tracker. So a table-wire row is ✅ only
  where the register shows all nine, and 🔶 otherwise with the scope named:
  which class, which languages, which issue.
- **Where a feature is wire-specific** — schema has it on one wire and not
  the other — the footnote says which wire, the mark is judged on that wire's
  backends, and the cell is the honest one for the pair rather than the
  better wire's.

A reader who wants the per-language state today reads
[PORTING.md](PORTING.md) and the conformance matrix rather than this page.
When #366 closes — full feature parity across all nine languages, each at its
language's performance floor — the schema column is re-scored from those two,
not by hand.

Six rows where all five columns agree are listed at the end rather than
shown. Editor support and big-endian hosts are not rows, because neither
could be sourced across five columns.

## The matrix

### Declarations and the schema language

| feature | schema | Protocol Buffers | FlatBuffers | Cap'n Proto | Avro |
|---|---|---|---|---|---|
| Numeric expressiveness beyond int32/int64/float/double: declared `min`/`max` bounds, sub-32-bit and 128-bit integers, fixed point | ✅ [s1] | ❌ [2] | 🔶 [3] | 🔶 [4] | 🔶 [5] |
| Compile-time constants usable inside the schema | ✅ [s2] | ❌ [6] | ❌ [6] | ✅ [7] | ❌ [8] |
| Nested value type with no per-instance framing | 🔶 [s3] | ❌ [9] | ✅ [10] | ✅ [11] | ✅ [12] |
| Bit-flag sets as a declaration | ✅ [s4] | ❌ [13] | ✅ [14] | ❌ [15] | ❌ [8] |
| Bounded arrays, strings and bytes: a declared capacity in the type | ✅ [s5] | ❌ [16] | 🔶 [17] | ❌ [18] | 🔶 [19] |
| Enum-keyed arrays (`[E]T`, one slot per variant, by name) | ✅ [s6] | ❌ [20] | ❌ [21] | ❌ [15] | ❌ [8] |
| Maps | ❌ [s7] | ✅ [23] | 🔶 [24] | ❌ [25] | 🔶 [26] |
| Shared subtrees: one node referenced by many parents, written once | 🔶 [s8] | ❌ [27] | 🔶 [28] | ❌ [29] | ❌ [30] |
| Union arms may be any field type, not only a named record | ❌ [s9] | 🔶 [32] | 🔶 [33] | ✅ [34] | ✅ [35] |
| Defaults for strings, bytes and composites | ❌ [s9] | 🔶 [36] | ❌ [37] | ✅ [38] | ✅ [39] |
| Explicit presence on a scalar (set vs unset, distinct from the default) | ✅ [s10] | ✅ [40] | 🔶 [41] | ❌ [42] | 🔶 [43] |
| Required fields: a read fails if the field is absent | ❌ [s11] | 🔶 [45] | 🔶 [46] | ❌ [47] | ✅ [48] |
| Dynamic typing: generics, a type-erased `Any`, or schema-less values | ❌ [s12] | ✅ [50] | 🔶 [51] | ✅ [52] | ❌ [53] |
| User-extensible annotations or custom options | ❌ [s13] | ✅ [55] | ✅ [56] | ✅ [57] | 🔶 [58] |

### The wire

| feature | schema | Protocol Buffers | FlatBuffers | Cap'n Proto | Avro |
|---|---|---|---|---|---|
| Self-describing: the bytes alone are enough to walk the data | 🔶 [s14] | 🔶 [59] | ❌ [60] | ❌ [61] | ✅ [62] |
| Values compacted below their declared storage width | 🔶 [s15] | ✅ [64] | ❌ [65] | 🔶 [66] | ✅ [67] |
| Scalars aligned inside the buffer | 🔶 [s16] | ❌ [69] | ✅ [70] | ✅ [71] | ❌ [72] |
| Data above 2 GiB | 🔶 [s17] | ❌ [74] | 🔶 [75] | 🔶 [76] | 🔶 [77] |
| Byte-identical output: one input, one schema, one byte sequence, across implementations | ✅ [s18] | ❌ [79] | ❌ [80] | 🔶 [81] | 🔶 [82] |

### Evolution

| feature | schema | Protocol Buffers | FlatBuffers | Cap'n Proto | Avro |
|---|---|---|---|---|---|
| Add, remove or reorder a field freely | ✅ [s19] | 🔶 [83] | 🔶 [84] | 🔶 [85] | 🔶 [86] |
| Rename a field without orphaning stored data | 🔶 [s20] | ✅ [88] | ✅ [89] | ✅ [90] | 🔶 [91] |
| Change a field's type with no silent misdecode | 🔶 [s21] | ❌ [92] | ❌ [93] | ❌ [94] | ✅ [95] |
| Change a declared default without reinterpreting stored bytes | 🔶 [s22] | 🔶 [97] | ❌ [98] | ❌ [94] | ✅ [99] |
| Unknown fields preserved through a read-and-rewrite | ❌ [s23] | ✅ [101] | 🔶 [102] | 🔶 [100] | ❌ [103] |
| A per-event read report rather than pass/fail | ✅ [s24] | ❌ [104] | ❌ [105] | ❌ [106] | ❌ [107] |
| An unknown enum value has a defined, non-fatal landing | ✅ [s25] | 🔶 [108] | 🔶 [109] | ? | 🔶 [110] |

### Runtime forms, allocation, performance

| feature | schema | Protocol Buffers | FlatBuffers | Cap'n Proto | Avro |
|---|---|---|---|---|---|
| Read one field without parsing the whole buffer | ✅ [s26] | ❌ [112] | ✅ [113] | ✅ [114] | ❌ [115] |
| No allocation on read | 🔶 [s27] | ❌ [116] | 🔶 [117] | ✅ [118] | ❌ [115] |
| Arena or single-buffer allocation on write, never per node | 🔶 [s28] | 🔶 [119] | ✅ [120] | ✅ [121] | ? |
| Exact serialized size known before writing | 🔶 [s29] | ✅ [122] | ❌ [123] | ? | ? |
| Mutate a serialized buffer in place | ❌ [s30] | ❌ [125] | 🔶 [126] | ❌ [127] | ? |
| Fixed-stride rows in one contiguous image, written by one language and read by another | 🔶 [s31] | ❌ [128] | ✅ [134] | ✅ [138] | ❌ [115] |
| A layout assertion in generated code on both sides, refusing a producer that disagrees | 🔶 [s31b] | ❌ [128] | ❌ [129] | ❌ [130] | ❌ [115] |
| Generated code with no library runtime to link | 🔶 [s32] | ❌ [131] | 🔶 [132] | ❌ [133] | ? |

### Validation of untrusted data

| feature | schema | Protocol Buffers | FlatBuffers | Cap'n Proto | Avro |
|---|---|---|---|---|---|
| Untrusted bytes are safe with no separate verification pass | 🔶 [s33] | ✅ [135] | ❌ [136] | ✅ [137] | ? |
| Value ranges enforced from the schema | ✅ [s1] | ❌ [2] | ❌ [3] | ❌ [4] | ❌ [5] |
| Depth and resource bounds against a hostile message | 🔶 [s34] | 🔶 [139] | 🔶 [140] | ✅ [141] | ? |
| One cross-language conformance corpus that includes hostile cases | ✅ [s35] | ? [142] | ? | ? | ? |

### Reflection, text, tooling

| feature | schema | Protocol Buffers | FlatBuffers | Cap'n Proto | Avro |
|---|---|---|---|---|---|
| Runtime reflection with no schema file present at runtime | 🔶 [s36] | ✅ [144] | 🔶 [145] | ✅ [146] | ✅ [147] |
| JSON or text form, in and out | ✅ [s37] | 🔶 [149] | 🔶 [150] | ✅ [151] | ✅ [152] |
| Doc comments carried into generated code | ❌ [s38] | ? [154] | ✅ [155] | ? | ✅ [156] |
| Official language implementations at feature parity | 🔶 [s39] | 🔶 [158] | ❌ [159] | ❌ [160] | 🔶 [161] |

### Versioning

| feature | schema | Protocol Buffers | FlatBuffers | Cap'n Proto | Avro |
|---|---|---|---|---|---|
| Rename an enum variant, union arm or named type without orphaning stored data | ❌ [s40] | ✅ [88] | ✅ [89] | 🔶 [163] | 🔶 [91] |
| A retired name or number cannot be silently reused | ❌ [s41] | ✅ [165] | 🔶 [166] | 🔶 [167] | ❌ [168] |
| Defined widening or promotion of a field's type on read | ❌ [s42] | 🔶 [170] | 🔶 [93] | 🔶 [171] | ✅ [172] |
| The writer's schema is recoverable at read time | ❌ [s43] | 🔶 [174] | 🔶 [175] | ❌ [61] | ✅ [176] |
| A revision marker in the bytes says which format version wrote them | 🔶 [s44] | ❌ [178] | 🔶 [179] | ❌ [180] | ✅ [181] |
| A first-party compile-time gate on breaking edits | 🔶 [s45] | ❌ [183] | ✅ [184] | ❌ [185] | ? |
| Wire bytes promised stable across releases of the compiler | 🔶 [s46] | ❌ [79] | ? | 🔶 [187] | 🔶 [82] |

### Agreed, not shown

All five have enums, a discriminated union, length-prefixed variable-length
payloads, contiguous scalar arrays, a code generator producing native types,
and comments invisible to the wire.

## Where they are ahead

- **Renames beyond fields.** Protocol Buffers and FlatBuffers rename a
  variant or a named type freely because their wires carry numbers rather
  than names, and Cap'n Proto does where the type pins an explicit id;
  schema's `was` is a field attribute today [s40].
- **Retired names.** Protocol Buffers' `reserved` is the right mechanism and
  schema lacks it, so a removed name can be re-added years later and decode
  old bytes under a new meaning [s41].
- **Default changes.** Avro writes every field of the writer's schema, so a
  changed default cannot reinterpret stored bytes, where schema elides a
  field equal to its default and answers the hazard with a guard rather than
  a guarantee [s22].
- **Unknown fields through a rewrite.** Protocol Buffers keeps them; schema
  drops them by decision — everything in schema has a schema — and counts
  what it dropped [s23].
- **Promotion.** Avro promotes `int` to `long` on read; schema reads a
  changed kind as the default and counts a kind mismatch [s42].
- **Doc comments.** FlatBuffers and Avro both carry documentation from the
  schema, and Protocol Buffers' and Cap'n Proto's own pages settle it neither
  way; schema's are deferred [s38].
- **Ecosystems.** Protocol Buffers has ten first-party languages and
  fifty-odd third-party ones, FlatBuffers thirteen of uneven coverage, and
  Avro six published SDKs; schema has nine [s39].
- **Tools.** `buf breaking`, `flatc --proto` and `capnp convert` exist and
  schema's own gate is first-party rather than an ecosystem [s45].

## Where schema is ahead

- **Bounds are bits.** `health int32 | min = 0, max = 1000` is ten bits on
  the packet wire, and the same gameplay packet measured against all four is
  [COMPARISON.md](COMPARISON.md). None of the four can infer a range it was
  never told.
- **One language for all five kinds of data.** Packets, backend messages,
  saves, render blocks and cooked assets from one declaration, so there is no
  second copy of the types to drift.
- **The read is the report.** Every load counts what it did not know, what
  had the wrong kind, what it clamped and whether the bytes were malformed,
  and never fails on data from another build. The others return a boolean or
  throw.
- **The silent class is enumerated and refused.** The edits no reader can
  report — a changed default, a moved flags variant, a referent respelled as
  something the kinds cannot tell apart, a moved fixed-point scale — are
  refused at compile time by a committed baseline with a reasoned, dated
  history. The nearest equivalents, `flatc --conform` and `buf breaking`,
  check structure, not meaning.
- **Shared subtrees on the wire.** A node written once and referenced many
  times, which none of the four does by declaration. Cap'n Proto forbids it
  to bound traversal; schema's flat node table materializes each node once
  and `LoadMeasure` prices the region before allocation, and the bound is
  being stated and tested as such (#466).
- **Per-field tolerance.** Avro fails the whole read on a mismatch; schema
  counts the field and reads the rest. The right answer for a save game and
  the wrong one for a warehouse, and the page says which is which.

## Judged not needed, with the reason

| feature | who has it | why schema declines |
|---|---|---|
| Required fields | FlatBuffers, Avro in effect, Protocol Buffers as a legacy carry-over | Every field optional with a declared default is the ground rule. Protocol Buffers removed `required` and says why; schema never had it. |
| `Any`, generics, schema-less values | Protocol Buffers, Cap'n Proto, FlexBuffers | The typed bag is a union whose arms are tables. A type-erased value is a type the compiler cannot check. |
| Custom annotations | Protocol Buffers, FlatBuffers, Cap'n Proto, Avro as tolerated metadata | The attribute vocabulary is closed and an unknown attribute is a compile error; type tags are the claiming mechanism. |
| Unknown fields preserved through a rewrite | Protocol Buffers | Everything in schema has a schema. The old file kept whole is the preservation, and the never-clobber rule is the policy. |
| A widening path on read | Avro, Protocol Buffers' pairs | A whitelist of compatible pairs silently misdecodes everything outside it. A changed kind reads as the default and is counted; the pattern is a new field beside the old one and a load shim. |
| The writer's schema traveling with the data | Avro | One schema per build, identified by the protocol id and the build version, neither of which rides in a file. A tolerant wire that skips by kind needs no schema to walk, and a file that must say what wrote it puts a `format` field on its root table. |
| A canonical form of the data | Cap'n Proto | A load followed by a save through the reader's own schema is the canonical form: elision, order and duplicates are all resolved by it. Byte identity across schema's own writers is held by goldens. |
| Mutating a serialized buffer in place | FlatBuffers | A cook is immutable and regenerated by the cache; the block form is the every-frame mutable answer. |

## Footnotes

Schema cells cite a specification section, one of the repository's registers
([PORTING.md](PORTING.md), [the conformance contract](../test/conformance/README.md))
or an issue. **FlatBuffers and Protocol Buffers cells cite the row that
carries the evidence in [COMPARISON-TABLES.md](COMPARISON-TABLES.md)** rather
than restating it; a footnote stays here only where that page does not carry
the fact, and it says so. Cap'n Proto and Avro cells cite the project's own
documentation by the short names in the source list at the end, with the
section that establishes the claim.

**Schema**

- s1. `| min, max` is part of the type: the packet wire sizes the field's bits from the bound and refuses a value outside it, and the table wire clamps and counts `clamped` on load (SPEC §4.3, SPEC-TABLES §4). `fixed(I,F)`, `ufixed(I,F)`, `int128` and `uint128` ride on the packet wire in all nine targets, on the cross-language wire gate like any other unit (`examples128/Ludicrous.schema`, SPEC §4.3), and under table-wire kinds 18–29 (SPEC-TABLES §3), where the wide-scalar cases are the reference's alone today (#366).
- s2. `const`, folded at compile time and usable in bounds, defaults and capacities (SPEC §4.2 — the `Const` production, and "Constants and enums are exported"; §4.4 is `if`, not constants).
- s3. **Packet wire, not the table wire.** A `type` nested inside a `type` is inline storage with no framing at all (SPEC §4.3). On the table wire a nested table is kind `13` — a `u32` length, then the body — and a fixed table nested in a table rides the same way (SPEC-TABLES §3), which is the framing Protocol Buffers is marked ❌ [9] for. Two wires, two answers.
- s4. `flags` declarations, one bit per variant (SPEC §4.2).
- s5. `[N]T`, `[..N]T`, `string(N)`, `bytes(N)`: the capacity is in the type, and the packet wire sizes the count from it (SPEC §4.3).
- s6. `[E]T`, a slot per variant addressed by name, under its own wire kind `16` (SPEC-TABLES §2.4, §3.2). The declaration and the keyed kind are fixed class and carried by all nine — the corpus's keyed instances are answered by every registered leg — while the accessor's both-ends refusal is untested in Java, Dart and Elixir and unchecked past `Max` in C (PORTING M5, #407, #377).
- s7. Decided and not built. A map makes the table variable and lands after 3.0.0 (SPEC-TABLES §2.8, #380).
- s8. A pointer field `*T` names a node once in a flat node table and every reference is an index (SPEC-TABLES §3.1). The variable class is the C++ reference's alone on the wire: PORTING M6 is ✅ for cpp and ❌ #349 in the other eight columns, and the eight ports answer ABSENT on the corpus's four pointered instances. The amplification bound on an untrusted read is #466.
- s9. Decided and not built. Union arms of any field type, and defaults for `string`, `bytes` and `flags`, are #396; SPEC §4.2's `Default` production admits a constant expression or an identifier and nothing else.
- s10. **Table wire only.** `?T` is the value plus a generated presence bool, so the holder stays fixed size (SPEC-TABLES §2.3). SPEC §4.2's grammar admits `?` in TABLE BODIES ONLY and a `type` body refuses one by name, so the packet wire has no presence at all — there is no presence bit there. On the table backends `?T` is fixed class and all nine carry it (`chain_optional` and `chain_optional_empty` in the conformance corpus); `?[N]T` is one of the message-class cases the reference answers alone (PORTING M16, #392).
- s11. Declined: every field optional with a declared default (SPEC-TABLES §4).
- s12. Declined on #396 and in COMPARISON-TABLES.md; the typed bag is a union whose arms are tables.
- s13. Declined: the attribute vocabulary is closed and an unknown attribute is a compile error (SPEC §4.2).
- s14. The packet wire is not self-describing, by decision — a stated non-goal, with all knowledge in the generated code on both ends (SPEC §1). On the table wire kinds and lengths ride, so any reader skips anything it does not know (SPEC-TABLES §3); but a name rides as a 16-bit hash rather than as text (§5), so the bytes cannot be rendered with names without the schema.
- s15. **Packet wire, not the table wire.** The packet wire sizes each field from its bounds (SPEC §4.3). The table wire deliberately does not compact — a scalar rides at its storage width and nothing is aligned or padded (SPEC-TABLES §3) — which is what FlatBuffers is marked ❌ [65] for.
- s16. No alignment and no padding on the tolerant wire (SPEC-TABLES §3). The cook (§7.2) and the block (§19.3) are the aligned forms; their `Open` is carried by all nine (PORTING M8, M7) and the block's builder by the C++ and C backends (§19.1).
- s17. Table wire. Eight-byte region references and 64-bit cook part lengths, with no aggregate ceiling (SPEC-TABLES §6.3, §7.1). The tolerant wire's own lengths and counts are `u32` today — a node body is capped at 4 GiB and refused at save (§2.1, §3) — and go uniformly 64-bit under #435.
- s18. One standard, one corpus. The packet wire is bit-for-bit compatible across nine runtimes, pinned in CI with shared golden bytes, and a compiler change that breaks a wire golden is a stop-the-line event (SPEC §1, §3.2, §7.2). On the table wire the `wire` surface byte-compares every registered leg's `Save` against one golden, and `measure == save at exact capacity` is a hard invariant held by a mandatory battery (SPEC-TABLES §9). Constructs a leg does not carry are answered ABSENT per case rather than passed over, so no cell claims agreement it did not test.
- s19. Table wire. Name identity: add anywhere, remove, reorder, each reported by the read rather than refused (SPEC-TABLES §4, §5). Carried by all nine — the `wire` and `report` surfaces over the fixed class are answered by every registered leg.
- s20. `| was = "old"` keeps the wire id; a bare rename is a removal and an addition the compiler cannot see, and the baseline is being taught to warn on the pair (#444). Fields today; every other vocabulary is #396 and #442, which is why the versioning group's first row is ❌ rather than this one.
- s21. A changed kind reads as the default and is counted `kind_mismatch` (SPEC-TABLES §4), in all nine over the fixed class. It is not the whole story, and SPEC-TABLES §4.1 item 3 says so: an enum-typed field respelled as its raw `uint16` rides under kind `7` either way, so the stored value is read as a variant hash and lands on `None` — or on a real variant — with no counter to fire. That respelling is guarded only by the opt-in baseline (§18) until #435 gives an enum a kind of its own.
- s22. Silent on the wire, refused at compile time by a committed baseline (SPEC-TABLES §4.1, §18.2). 🔶 because the baseline is opt-in by design — "no file, no check" (§18.1) — and #445 adds a one-line notice to a unit that has none rather than a block, so the limitation does not close when it lands.
- s23. Declined by decision; the read report counts what a rewrite would drop, and the one rule a studio writes for it is never to overwrite a file whose read report is not silent (VERSIONING.md).
- s24. Four counters and a `malformed` flag on every load (SPEC-TABLES §4). Carried by all nine: the `report` surface is answered by every registered leg, and each leg with a text form carries its own walk negative control.
- s25. An unknown variant reads `None` and is counted `unknown` (SPEC-TABLES §4, §5), in all nine on the `report` surface.
- s26. The cook: `Open` is a header match and a pointer, with nothing per node (SPEC-TABLES §7). Carried by all nine — PORTING M8 is ✅ in every column, and a gigabyte cook opens in the same time as a small one. The cook's WRITE side is the C++ reference's and the tool's; every other language's writer is a named follow-on (§7.6, §15).
- s27. The read path fills caller-owned storage and the reader is a cursor over the caller's buffer, never a sub-view (SPEC-TABLES §6.5, PORTING M1). 🔶 for three named reasons. Elixir cannot make the claim at all — a decoded BEAM term is an allocation and no buffer is caller-owned, so the leg pins the per-case count instead (M1's Elixir cell). The independent allocation gate does not yet run on the C++ or the C# read path (PORTING I1, #412). And a union inside a table allocates per arm in a backend whose language has no native union, the one carve-out §6.5 states.
- s28. The fixed class writes into the caller's buffer and allocates nothing, in any backend. The variable class builds through an arena in bulk, thread-local, never per node (SPEC-TABLES §6.4, §9), and that arena is the C++ reference's (PORTING M6, #349); the block takes a caller-provided allocator once at build and never on the fill path (§19.1). The union carve-out (§6.5) is the one per-node allocation, and it belongs to the language rather than to the format.
- s29. **Table wire, not the packet wire.** `Measure` computes a value's exact encoded size writing nothing, and `measure == save at exact capacity` is a hard invariant held by a mandatory battery across the corpus (SPEC-TABLES §9); all nine backends generate it. The packet wire has no exact measure and says so in terms — "There is no generated measure function" — `MaxBits`/`MaxBytes` are conservative worst-case constants for sizing a buffer, and `Write` returns the actual size afterwards (SPEC §6.1).
- s30. Declined: a cook is immutable and regenerated by the cache, and the block form is the mutable answer (SPEC-TABLES §7, §19).
- s31. The block form: one contiguous extent of rows at a compiler-computed stride, every reference block-relative, so it relocates by `memcpy` with no fix-up (SPEC-TABLES §19, §19.1). Its READ half is generated by all nine backends, and PORTING M7 carries eight of them with Elixir's per-row allocation as #409. Its BUILDER is emitted by the C++ and C backends alone (§19.1).
- s31b. Every cooked record and every block projection carries a compile-time assertion of size, alignment and every field offset against the numbers the compiler computed, each array's pitch constant included — two independent derivations held against each other, so a producer that disagrees is refused before a row is read (SPEC-TABLES §19.3). Seven backends carry it; Dart and Elixir have no second layout model to check against and hold the contract with the build version instead (PORTING M11).
- s32. Six of the nine targets link a small `serialize` runtime for the packet wire; Dart, Java and Elixir generate self-contained output (VERSIONING.md). The C++ tables dialect takes no STL and routes every C-library call through a hook the program can define (SPEC-TABLES §13.9).
- s33. The tolerant read is the validator: every length bounds-checked against its body, every count against the body, ranges clamped, a bad sub-table stopping only itself, and `LoadMeasure` letting the caller refuse a region size before anything is allocated (SPEC-TABLES §4, §6.5). The independent-oracle fuzzer that would prove it is landing for the C++ reference (#429) and owed to every port (#391), which is what holds this at 🔶.
- s34. By-value depth is fixed by the schema and the pointer graph is flat, so a long chain is not a recursion (SPEC-TABLES §3.1). Until #466 states and tests the amplification bound, the bound is the caller's own refusal of `LoadMeasure`'s answer (§6.5) rather than a number the format states.
- s35. One corpus, every registered leg, hostile rows included — two forgery batteries, the cook battery's 111 rows and the `json-hostile` trees, with seven negative controls behind the harness (test/conformance). A leg that lacks a construct answers ABSENT per case rather than passing by omission: the eight ports read `+28a` on the wire surfaces and `+26a` on the text surfaces today, and the matrix is the completion tracker (#383, #349).
- s36. Static descriptors in every table's generated header — name, kind, id, offset, bounds, guards, nesting — with no schema file present and no RTTI (SPEC-TABLES §8.1), carried by all nine (PORTING M13; C# holds them in a cache rather than as constants, #411). The type view and the unit registry (§8.2, §8.3) are C++ and C# only, and every other backend emits no view file (§15).
- s37. JSON in and out by one generic walk over the descriptors, `| json = "key"`, the read report on the way in, `&node` for a shared node (SPEC-TABLES §16). Carried by all nine (PORTING M9), with the text surfaces answered per case and a per-backend walk negative control behind each.
- s38. Deferred with the design pinned (SPEC §4.1); doc strings in the view are a named follow-on (SPEC-TABLES §15).
- s39. Nine languages byte-identical on the packet wire in CI (SPEC §1). Tables are carried under #366 — the declared 3.0.0 gate, full feature parity across all nine languages at each language's performance floor — and PORTING.md, not this page, is the present state.
- s40. Decided and not built. Table `was` is #396 and variant and arm `was` is #442, both before 3.0.0; `was` is a field attribute today (SPEC-TABLES §5).
- s41. Decided and not built. The retired-names ledger in the baseline is #441, before 3.0.0; nothing today marks a removed name retired, so it can be re-added and decode old bytes under a new meaning.
- s42. Declined, with the reason in the table above.
- s43. Declined, with the reason in the table above: one schema per build, identified by the protocol id and the build version, neither of which rides in a file, and a file that must say what wrote it puts a `format` field on its root table.
- s44. The cook's and the block's prologues carry the build version and refuse a file another build wrote (SPEC-TABLES §7.1, §19.1, §20), and all nine read them (PORTING M8, M7). The tolerant wire carries no marker of its own: the form byte at the head of every table-wire file is #435, before 3.0.0. It is also the answer to FlatBuffers' `file_identifier` [179] — the schema's own revision is a `format` field on the root table, the format's own revision is the form byte.
- s45. `tables.baseline` with `--update --reason` and a dated history (SPEC-TABLES §18), first-party where `buf breaking` is not. 🔶 for the same reason as [s22]: the file is opt-in and no file means no check (§18.1). The packet wire has no compile-time gate at all — the protocol id refuses at connect time, which is a runtime refusal (SPEC §3) — and an importer from `.proto` or `.fbs` is neither declined nor built (#477).
- s46. The promise is on VERSIONING.md, and SPEC §6.1 states it as a contract: for a given schema and target, equal post-quantization values produce identical bytes, deterministically, across compiler versions, held by the golden-wire gate across a schema's edits. Across compiler RELEASES it needs the codec-law line and an N-1 to N differential gate (#463).

**Protocol Buffers and FlatBuffers**

Row-level evidence for these two lives in
[COMPARISON-TABLES.md](COMPARISON-TABLES.md), and a citation below names its
section and its row rather than repeating the quote. Where a mark here rests
on a fact that page does not carry, the footnote says so and cites the
project's own page directly.

2. COMPARISON-TABLES.md, Declarations and the schema language — "Scalar types", "Declared numeric bounds", "Fixed point".
3. COMPARISON-TABLES.md, Declarations and the schema language — "Scalar types", "Declared numeric bounds", "Fixed point"; sub-32-bit yes, no 128-bit, no fixed point, no declared range.
6. COMPARISON-TABLES.md, Declarations and the schema language — "Constants" ("—" in both columns).
9. COMPARISON-TABLES.md, Declarations and the schema language — "Inline struct" ("— ; every message is length-delimited").
10. COMPARISON-TABLES.md, Declarations and the schema language — "Inline struct".
13. COMPARISON-TABLES.md, Declarations and the schema language — "Flags" ("—").
14. COMPARISON-TABLES.md, Declarations and the schema language — "Flags" (`enum (bit_flags)`).
16. COMPARISON-TABLES.md, Declarations and the schema language — "Arrays", "Strings", "Bytes".
17. COMPARISON-TABLES.md, Declarations and the schema language — "Arrays" (`[T:N]` in structs only).
20. COMPARISON-TABLES.md, Declarations and the schema language — "Maps"; and "One scene, three ways" — "keyed by Slot" (enum keys refused).
21. COMPARISON-TABLES.md, Declarations and the schema language — "Enum-keyed arrays" ("—"); "Sorted lookup in a buffer" gives the idiom, a sorted vector looked up by VALUE.
23. COMPARISON-TABLES.md, Declarations and the schema language — "Maps".
24. COMPARISON-TABLES.md, Declarations and the schema language — "Maps"; "Sorted lookup in a buffer".
27. COMPARISON-TABLES.md, Declarations and the schema language — "Pointers, graphs, sharing" ("tree only; no sharing, no cycles").
28. COMPARISON-TABLES.md, Declarations and the schema language — "Pointers, graphs, sharing" (an `Offset` may be reused by the builder; cycles impossible).
32. COMPARISON-TABLES.md, Declarations and the schema language — "Union" (`oneof`). **Not carried there:** the arm restriction. `oneof` admits any type except map fields and repeated fields (PB-proto3, Oneof), which is what holds this at 🔶.
33. COMPARISON-TABLES.md, Declarations and the schema language — "Union" (union of tables; structs and strings experimental; vectors of unions C++ only).
36. COMPARISON-TABLES.md, Declarations and the schema language — "Defaults" (proto3 has none; proto2 and editions do).
37. COMPARISON-TABLES.md, Declarations and the schema language — "Defaults" (scalar defaults); "Optional fields" (references null when absent).
40. COMPARISON-TABLES.md, Declarations and the schema language — "Optional fields" (explicit presence: proto2 all, proto3 `optional`, editions explicit by default).
41. COMPARISON-TABLES.md, Declarations and the schema language — "Optional fields" (optional scalars via `= null`). **Not carried there:** the per-language coverage. FB-support marks optional scalars Yes in ten of the thirteen languages listed and blank for Python, PHP and Dart — and that table says of itself "NOTE: this table is a start, it needs to be extended", so every count taken from it is a floor rather than a fact.
45. COMPARISON-TABLES.md, Declarations and the schema language — "Required" (removed; `LEGACY_REQUIRED` only). 🔶 rather than ❌ because the construct exists on the record in two places: proto2's `required`, and editions' `features.field_presence = LEGACY_REQUIRED`, "required for parsing and serialization" (PB-features). The project's guidance is never to add one (PB-dos).
46. COMPARISON-TABLES.md, Declarations and the schema language — "Required" (`(required)`, verifier-checked). 🔶 rather than ✅ because the check is the verifier's, and the verifier does not ship in every language [136].
50. COMPARISON-TABLES.md, Declarations and the schema language — "Schema-less data" (`Struct`, `Value`); "Well-known types" (`Any`).
51. COMPARISON-TABLES.md, Declarations and the schema language — "Schema-less data" (FlexBuffers). **Not carried there:** the coverage. FB-support marks FlexBuffers Yes for C++, Java, Rust and Swift of the thirteen listed, under that table's own caveat [41].
55. COMPARISON-TABLES.md, Declarations and the schema language — "Attributes" (custom options with retention and targets).
56. COMPARISON-TABLES.md, Declarations and the schema language — "Attributes" (user attributes declarable, read via reflection).
59. COMPARISON-TABLES.md, The wire — "Self-describing" ("no; needs descriptors"). 🔶 rather than ❌ because field numbers and wire types do ride, so a parser skips what it does not know; names and declared types need the definition (PB-encoding, Message Structure).
60. **Not carried in COMPARISON-TABLES.md** — that page's "Version identity" row covers `file_identifier`, not the format's own silence. FB-internals: the format "doesn't contain information for format identification and versioning, which is also by design".
64. COMPARISON-TABLES.md, The wire — "Varint compaction" (varints, zigzag).
65. COMPARISON-TABLES.md, The wire — "Varint compaction" ("—"); "Alignment" (every scalar aligned to its size), which is what buys in-place access.
69. COMPARISON-TABLES.md, The wire — "Alignment" ("none").
70. COMPARISON-TABLES.md, The wire — "Alignment" (aligned to size; `force_align`).
74. COMPARISON-TABLES.md, The wire — "Size ceilings" (2 GiB).
75. COMPARISON-TABLES.md, The wire — "Size ceilings" (32-bit offsets: 2 GiB; `vector64` for tail vectors, not in every port).
79. COMPARISON-TABLES.md, The wire — "Byte-identical output across implementations" ("don't assume serialization stability across builds").
80. COMPARISON-TABLES.md, The wire — "Byte-identical output across implementations" ("no cross-language claim"). ❌ and not ?, and the reason is **not carried there**: FB-internals states the denial outright — "This may mean two different implementations may produce different binaries given the same input values, and this is perfectly valid." That is the format declining the property on the record, not an inference from absence.
83. COMPARISON-TABLES.md, Evolution — "Add a field", "Remove a field", "Reorder", "Rename" (add anywhere with an unused number; remove and reserve; reorder free).
84. COMPARISON-TABLES.md, Evolution — "Add a field", "Remove a field", "Reorder". 🔶 and not ❌, on the same reading as [83] and [85]: new fields go at the end, `id` on every field makes source position free, and removal is forbidden with the slot kept by `deprecated`. All three formats add, all three reorder in source, and none of the three frees a retired slot; the differences are in the footnotes, not in the marks.
85. **Not carried in COMPARISON-TABLES.md** (Cap'n Proto is not on that page). New members take numbers exceeding all previous ones, source position is free, numbers never change and there is no removal (CP-lang, Evolving Your Protocol). 🔶 on the same reading as [83] and [84].
88. COMPARISON-TABLES.md, Declarations and the schema language — "Rename" (free on the binary wire; reserve the old name for JSON).
89. COMPARISON-TABLES.md, Declarations and the schema language — "Rename" (free; names are not serialized).
92. COMPARISON-TABLES.md, Evolution — "Change a type" (a fixed list of compatible pairs; anything else misdecodes silently).
93. COMPARISON-TABLES.md, Evolution — "Change a type" (only at identical width, with careful handling).
97. COMPARISON-TABLES.md, Evolution — "Change a default" (proto3 has none; proto2 reader-side). 🔶 rather than ❌, and the reason is **not carried there:** under explicit presence — proto2, and the default in every edition — "Any explicitly-set value is serialized onto the wire (even if it is the same as the default value)" (PB-features), so a changed default reinterprets only the fields a writer never set. Under implicit presence, proto3's default, a value equal to the default is elided and the hazard is whole. Avro keeps ✅ [99] because it writes every field of the writer's schema unconditionally, so no stored byte can be reinterpreted at all.
98. COMPARISON-TABLES.md, Evolution — "Change a default" ("don't": V1 data that did not write the value relied on generated code for it).
101. COMPARISON-TABLES.md, Evolution — "Unknown fields on read", "Unknown fields on rewrite" (retained and re-serialized).
102. COMPARISON-TABLES.md, Evolution — "Unknown fields on rewrite" (the buffer keeps them if forwarded whole; the object API's unpack-and-repack does not).
104. COMPARISON-TABLES.md, Evolution — "Read report" (success or failure, plus the unknown set).
105. COMPARISON-TABLES.md, Evolution — "Read report" (verifier pass or fail).
108. COMPARISON-TABLES.md, Evolution — "Enum evolution" (open enums keep the int; closed enums move it to unknown fields). **Not carried there:** the per-language divergence. C#, Go, JSPB and Ruby treat every enum as open and Dart as closed, whatever the syntax says (PB-enum).
109. COMPARISON-TABLES.md, Evolution — "Enum evolution" (append or explicit values; the application handles unknowns itself).
112. COMPARISON-TABLES.md, Runtime forms, allocation, performance — "Point at the bytes" ("never").
113. COMPARISON-TABLES.md, Runtime forms, allocation, performance — "Point at the bytes" ("always").
116. COMPARISON-TABLES.md, Runtime forms, allocation, performance — "Allocation on read" (per message, string and repeated field; arenas mitigate).
117. COMPARISON-TABLES.md, Runtime forms, allocation, performance — "Allocation on read" (none in place; the object API allocates).
119. COMPARISON-TABLES.md, Runtime forms, allocation, performance — "Allocation on write". **Not carried there:** arenas are a C++ feature (PB-arenas), which is what holds this at 🔶.
120. COMPARISON-TABLES.md, Runtime forms, allocation, performance — "Allocation on write" (the builder grows a buffer; custom allocator). ✅ and not 🔶: one growable buffer with a caller's allocator is the row's property, and nothing is allocated per node — the same reading Cap'n Proto's segments get [121].
122. COMPARISON-TABLES.md, Runtime forms, allocation, performance — "Exact size before writing" (`ByteSizeLong()`).
123. COMPARISON-TABLES.md, Runtime forms, allocation, performance — "Exact size before writing" (after `Finish`).
125. COMPARISON-TABLES.md, Runtime forms, allocation, performance — "Mutate in place" ("—").
126. COMPARISON-TABLES.md, Runtime forms, allocation, performance — "Mutate in place" (`--gen-mutable` scalars; reflection resizes). **Not carried there:** the coverage. FB-support marks simple mutation Yes for C++, Java, C#, Go and Swift of the thirteen listed, under that table's own caveat [41].
128. COMPARISON-TABLES.md, Runtime forms, allocation, performance — "Cross-language rows at a pitch" ("—"); "Offline cook for a build" ("—").
129. COMPARISON-TABLES.md, Runtime forms, allocation, performance — "Cross-language rows at a pitch" (a vector of structs is inline, but the schema does not assert both sides' native layout).
131. COMPARISON-TABLES.md, Runtime forms, allocation, performance — "Generated code dependencies" (`libprotobuf`).
132. COMPARISON-TABLES.md, Runtime forms, allocation, performance — "Generated code dependencies" says "header-only runtime", and **no cited FlatBuffers page says it**; this footnote replaces that claim with what FB-cpp does say. The generated header "relies on `flatbuffers/flatbuffers.h`, which should be in your include path", so the C++ core is an include rather than a link; text and schema parsing "requires you to link a few more files into your program" (FB-cpp, Prerequisites; Text & schema parsing). Every other FlatBuffers language ships a runtime library, so the cell is 🔶.
134. **Not carried in COMPARISON-TABLES.md**, whose row states only the second half. Structs "are always stored inline in their parent (a struct, table, or vector)", and their layout is defined "independent of the alignment rules of the underlying compiler to guarantee a cross platform compatible layout" (FB-internals, Structs), so a vector of structs is a fixed-stride row image two languages compute identically. That is this row's property, and it is ✅.
135. COMPARISON-TABLES.md, Validation of untrusted data — "Untrusted bytes on the tolerant wire" (the parser validates structure; recursion limit; UTF-8 verify).
136. COMPARISON-TABLES.md, Validation of untrusted data — "Untrusted bytes on the tolerant wire" (a separate `Verifier`, required before access). **Corrected here:** FB-support's matrix marks the buffer verifier Yes for C++, C and Swift of thirteen, and FB-rust adds a fourth the matrix does not show — "The safe Rust functions to interpret a slice as a table (`root`, `size_prefixed_root`, `root_with_opts`, and `size_prefixed_root_with_opts`) verify the data first." The count is at least four of thirteen, and FB-support says of itself "NOTE: this table is a start, it needs to be extended", so it is a floor.
139. COMPARISON-TABLES.md, Validation of untrusted data — "Depth and DoS bounds" (recursion depth 100). **Not carried there:** the limits differ by implementation — Java 100, C++ 100, Go 10,000 with a planned reduction (PB-limits) — which is what holds this at 🔶.
140. COMPARISON-TABLES.md, Validation of untrusted data — "Untrusted bytes on the tolerant wire" (`max_depth` 64, `max_tables` 1M), where a verifier ships [136].
142. COMPARISON-TABLES.md, Validation of untrusted data — "Cross-language agreement on refusal" ("a conformance suite"). ? rather than 🔶: the suite's own page says it tests "completeness and correctness of Protocol Buffers implementations" and says nothing about malformed or hostile input either way (PB-conformance).
144. COMPARISON-TABLES.md, Reflection, text, tooling — "Runtime reflection" (descriptors, `Reflection`, `DynamicMessage`).
145. COMPARISON-TABLES.md, Reflection, text, tooling — "Runtime reflection" (binary schema plus `reflection.h` in C++, basic in C; MINI-REFLECTION). 🔶 rather than ❌ on the strength of that last: "A more limited form of reflection is available for direct inclusion in generated code, which doesn't do any (binary) schema access at all", behind `--reflect-types` and `--reflect-names` (FB-cpp, Mini Reflection).
149. COMPARISON-TABLES.md, Reflection, text, tooling — "Text form" says "ProtoJSON in every runtime", and **no cited page establishes the count**; this footnote replaces it. PB-json specifies the format and names its own gaps — "As of v25.x, the C++, Java, and Python implementations are nonconformant" on one flag — so the mark is 🔶.
150. COMPARISON-TABLES.md, Reflection, text, tooling — "Text form" (JSON in `flatc`; parsing in a few languages). **Corrected here:** FB-support marks JSON parsing Yes for C++, C and Lobster of thirteen, under that table's own caveat [41]; `flatc --json` converts offline (FB-flatc).
154. COMPARISON-TABLES.md, Reflection, text, tooling — "Doc comments" says "preserved in descriptors' source info", and **no cited page carries it**; the source list's PB-wkt is the well-known types reference and does not contain `SourceCodeInfo`. PB-proto3's "Adding Comments" states the accepted comment styles and says nothing about generated code, and the C++ generated-code reference does not mention comments either (PB-proto3, Adding Comments; PB-cpp-gen). Neither made nor denied, so ? by the legend.
155. COMPARISON-TABLES.md, Reflection, text, tooling — "Doc comments" (`///` into generated code and the binary schema).
158. COMPARISON-TABLES.md, Reflection, text, tooling — "Languages" (ten first-party, forty-plus third-party). 🔶 because the project documents its own enum nonconformance across those runtimes (PB-enum).
159. COMPARISON-TABLES.md, Reflection, text, tooling — "Languages" (thirteen listed, coverage uneven). **Not carried there:** the shape of the unevenness. Of the thirteen, FB-support marks JSON parsing in three, reflection in two, the verifier in three (four with FB-rust, [136]) and mutation in five, and says C++ "has the richest feature set, and is likely most robust" — all of it under that table's own caveat [41].
165. COMPARISON-TABLES.md, Declarations and the schema language — "Reserved names" (`reserved` numbers and names).
166. COMPARISON-TABLES.md, Declarations and the schema language — "Reserved names" ("— ; never remove, deprecate instead"); "Deprecation" (`(deprecated)`: accessors dropped, slot kept).
170. COMPARISON-TABLES.md, Evolution — "Change a type" (the compatible-pairs list; a truncation inside it is silent).
174. **Not carried in COMPARISON-TABLES.md.** The project documents a self-describing pattern — a message carrying a `FileDescriptorSet` beside the payload — and says in the same breath that "the reason that this functionality is not included in the Protocol Buffer library is because we have never had a use for it inside Google" (PB-techniques, Self-describing Messages). The schema is recoverable only out of band, which is the same shape as FlatBuffers' `.bfbs` [175] and takes the same mark.
175. COMPARISON-TABLES.md, Reflection, text, tooling — "Runtime reflection" (the binary schema as a separate artifact); The wire — "Version identity" (`file_identifier`, four hand-chosen characters). Out of band, like [174], and the same mark.
178. COMPARISON-TABLES.md, The wire — "Version identity" ("none; editions version the language"); the six wire types are frozen with no revision marker (PB-encoding).
179. COMPARISON-TABLES.md, The wire — "Version identity" (`file_identifier`, by hand) — it identifies the schema author's format, not the FlatBuffers revision.
183. COMPARISON-TABLES.md, Evolution — "Compile-time guard" (`buf breaking`, third party: built by Buf Technologies, not the Protocol Buffers project — buf).
184. COMPARISON-TABLES.md, Evolution — "Compile-time guard" (`flatc --conform`).

**Cap'n Proto and Avro**

Neither is on COMPARISON-TABLES.md, so both cite their own documentation
here.

4. Cap'n Proto has Int8 to Int64 and the unsigned set; no 128-bit, no fixed point, no declared range (CP-lang, Built-in Types).
5. Avro's primitives stop at int, long, float, double; `decimal` is a logical type over bytes or fixed, an arbitrary-precision annotation whose per-language support varies (AV-spec, Primitive Types; Logical Types).
7. `const` exists and may be referenced inside another value, including a field default (CP-lang, Constants).
8. Avro's schema is JSON; no constant, flags or keyed-array construct exists in the specification (AV-spec, Complex Types).
11. A GROUP's fields "are numbered in the same space as the containing struct's fields, and are laid out exactly the same as if they hadn't been grouped at all" (CP-lang, Groups), which is a nested value with no framing of its own; a struct as an element of a composite list is inline too (CP-encoding, Lists). A struct FIELD is a pointer into the message, the deliberate trade for random access and the list-upgrade rule (CP-encoding, Structs).
12. An Avro record is the concatenation of its fields' encodings with no framing (AV-spec, Binary Encoding).
15. No flags or keyed-array construct; enumerants are numbered from zero and are not numeric (CP-lang, Enums).
18. `List(T)`, `Text` and `Data` take no bound (CP-lang, Built-in Types).
19. Avro's `fixed` is an exact-size byte type; `string`, `bytes`, `array` and `map` are unbounded (AV-spec, Fixed).
25. `Map(K,V)` appears only as an example of a user-defined generic (CP-lang, Generic Types).
26. Map keys are strings (AV-spec, Maps).
29. One pointer per object, a tree and not a graph; cyclic or overlapping pointers can send a reader into an infinite loop, which is what the traversal limit defends (CP-encoding, Messages).
30. No reference type; recursion is a union naming the record, which yields a tree (AV-spec, Complex Types).
34. A union is two or more fields sharing storage, so any field type is an arm, including `Void` (CP-lang, Unions).
35. A union is a JSON array of schemas, any schema a branch, with two restrictions (AV-spec, Unions).
38. Defaults for `Text`, `Data`, `List` and nested structs (CP-lang, Structs).
39. A default for every field type (AV-spec, Record).
42. No scalar presence: data-section fields are stored XOR their default, so unset and set-to-default are one encoding, and a pointer's null is the only absence (CP-encoding, Value Encoding).
43. The idiom is a union with `null`, a value rather than a presence flag (AV-spec, Unions).
47. No required marker (CP-lang).
48. A reader field with no default and no writer counterpart signals an error for the whole read (AV-spec, Schema Resolution).
52. Generics are first class; an omitted parameter is `AnyPointer` (CP-lang, Generic Types).
53. No generics and no `Any`; the writer's schema is always available, a different solution to the same problem (AV-spec).
57. `annotation foo(struct, enum) :Text;`, with twelve targets — `file`, `struct`, `field`, `union`, `group`, `enum`, `enumerant`, `interface`, `method`, `param`, `annotation`, `const` — and `*` to cover them all (CP-lang, Annotations).
58. "Attributes not defined in this document are permitted as metadata, but must not affect the format of serialized data" (AV-spec, Schema Declaration). That is user-extensible annotation in the format, so not ❌; 🔶 rather than ✅ because the specification requires only that an implementation tolerate the attributes, not that it surface them to generated code or to a runtime API, which is what the other three columns' marks rest on.
61. Field positions are computed from the schema; the message carries no field identity (CP-encoding).
62. The container file embeds the writer's schema, and single-object encoding carries `C3 01` plus a schema fingerprint. The bare value encoding is untagged (AV-spec, Object Container Files; Single Object Encoding; Binary Encoding).
66. Packing is an optional pass over the unpacked layout (CP-encoding, Packing; CP-faq).
67. int and long are zig-zag varints (AV-spec, Binary Encoding).
71. Objects aligned to word boundaries, primitives to a multiple of their size (CP-encoding).
72. A byte stream with no alignment (AV-spec, Binary Encoding).
76. Segment ids are 32 bits, so the aggregate is not 32-bit capped; a single list is capped at 2^29 elements, and the C++ reader's traversal limit defaults to 64 MiB (CP-encoding, Lists; Far Pointers; CP-cxx, Security Tips).
77. Array and map blocks carry `long` counts and file data blocks a `long` object count and byte size, and the specification states no aggregate ceiling (AV-spec, Binary Encoding; Object Container Files). 🔶 rather than ✅ because no per-implementation ceiling was sourced, and this page's test asks for the feature in every official implementation.
81. Canonicalization is fully specified and is a separate conversion, not the default output (CP-encoding, Canonicalization; CP-tool).
82. A record's bytes are determined by the schema, but array and map block boundaries are the writer's choice, and Avro's Parsing Canonical Form canonicalizes schemas rather than data (AV-spec, Binary Encoding; Parsing Canonical Form).
86. Fields match by name and a writer's extras are ignored, but a reader field without a default is a hard error against older data (AV-spec, Schema Resolution).
90. Any symbolic name can change as long as the type id and the ordinals stay (CP-lang, Evolving Your Protocol).
91. Aliases exist on named types and fields; "An implementation may optionally use aliases to map a writer's schema to the reader's" (AV-spec, Aliases).
94. A field's type or default value cannot change (CP-lang, Evolving Your Protocol).
95. Resolution promotes or signals an error, never a misdecode, and the failure is the whole read (AV-spec, Schema Resolution).
99. "Avro encodes a field even if its value is equal to its default"; a default is used only when the writer's schema lacks the field, so a default change cannot reinterpret stored bytes (AV-spec, Record; Binary Encoding).
100. Copying a struct with a `set` method keeps the original's size, because "the original could have been built with an older version of the protocol which lacked some fields", so fields the copier does not know ride through the copy (CP-cxx, Tips and Best Practices). 🔶 rather than ✅ because it is a property of that copy idiom rather than of every read-and-rewrite: a builder filled field by field keeps nothing.
103. A writer's field not in the reader's record is ignored (AV-spec, Schema Resolution).
106. Limits throw; nothing reports what was skipped (CP-cxx, Security Tips).
107. Errors are signaled; no counted report (AV-spec).
110. A declared enum `default` is used, otherwise an error is signaled (AV-spec, Enums; Schema Resolution).
114. No encoding step; one field readable without parsing the whole; mmap (CP-home).
115. Values are variable-length and untagged, decoded into objects; no random access and no cooked form, deliberately (AV-spec, Binary Encoding).
118. Reads are pointer arithmetic over the buffer (CP-cxx).
121. Messages are built arena-style, sequentially in a segment, and a new segment is allocated when one fills — never per object (CP-cxx, Tips and Best Practices).
127. ❌, corrected from ✅. Orphans move subtrees WITHIN a message and exist only in memory a `MessageBuilder` owns; the project states the limitation directly — Cap'n Proto "is not well-suited for _writing_ via `mmap()`, only reading", because no mutable segment framing format has been designed. A received message is read through a `MessageReader` and rebuilt through a builder to change it (CP-cxx, Orphans; Serialization/Deserialization).
130. mmap is documented and a composite list is structs at a fixed stride, but nothing asserts two languages' native layouts against each other at open (CP-home; CP-encoding, Lists).
133. Link `libcapnp` and `libkj`, and `libcapnp-rpc` and `libkj-async` with RPC (CP-cxx, Generating Code).
137. Designed to be safe against malicious input, with the caveat that it has not undergone a formal security review (CP-faq).
138. A composite list is a tag word followed by its elements laid out inline at one stride, and a struct's layout is fixed by the encoding rather than by a compiler (CP-encoding, Lists; Structs), so a list of structs is a fixed-stride row image any implementation computes identically.
141. Nesting depth 64 and a 64 MiB traversal limit, both configurable through `capnp::ReaderOptions` (CP-cxx, Security Tips). ✅ and not 🔶: "documented for C++" is not a limitation for this project, because the C++ reference IS the official implementation set — "Implementations in other languages are maintained by respective authors and have not been reviewed by me" (CP-otherlang).
146. `capnp/schema.h`, `capnp/dynamic.h` and `SchemaLoader`, documented for C++ (CP-cxx, Dynamic Reflection) — the whole official set, as [141] says.
147. The writer's schema is always present and the generic API reads any record without code generation (AV-java).
151. `capnp convert` moves between binary, packed, flat, canonical, text and json, documented in the 0.7 release notes rather than the tool page, and the library JSON is C++ (CP-tool; CP-0.7) — the whole official set, as [141] says.
152. The JSON encoding is part of the specification; `avro-tools` converts offline (AV-spec, JSON Encoding; AV-java).
156. The `doc` attribute on records and fields (AV-spec, Record).
160. The C++ reference is the only reviewed implementation and every other is a third party's; the C implementation is "no longer maintained", one JavaScript implementation is "abandoned", and several are serialization-only (CP-otherlang).
161. Six documented SDKs and a home page claiming more; no per-feature matrix (AV-docs; AV-home).
163. An omitted type id is derived from the parent scope's id and the declaration's name — "You cannot change the name of a type that doesn't have an explicit ID" — so a rename or a move changes it unless the id is pinned (CP-lang, Unique IDs).
167. Ordinals never change and removal is not permitted; no reserved mechanism (CP-lang, Evolving Your Protocol).
168. Aliases point forward; nothing marks a name retired (AV-spec, Aliases).
171. One path: a list of primitives may become a list of structs whose first field is that primitive (CP-lang, Evolving Your Protocol; CP-encoding, Lists).
172. int to long, float or double; long to float or double; float to double; string and bytes either way (AV-spec, Schema Resolution).
176. Container files embed the schema; single-object encoding carries its fingerprint (AV-spec).
180. A segment table and segments; no magic, no version field (CP-encoding).
181. Container files begin `Obj` `0x01`; single-object encoding begins `C3 01` and names its version (AV-spec).
185. Evolution rules are prose; no tool checks them (CP-lang; CP-tool).
187. The evolution rules preserve the canonical encoding across schema changes; no cross-release byte-stability promise is made (CP-lang, Evolving Your Protocol).

## Sources

All pages fetched 2026-09-04.

| short | page |
|---|---|
| PB-proto3 | https://protobuf.dev/programming-guides/proto3/ |
| PB-encoding | https://protobuf.dev/programming-guides/encoding/ |
| PB-enum | https://protobuf.dev/programming-guides/enum/ |
| PB-limits | https://protobuf.dev/programming-guides/proto-limits/ |
| PB-dos | https://protobuf.dev/best-practices/dos-donts/ |
| PB-features | https://protobuf.dev/editions/features/ |
| PB-json | https://protobuf.dev/programming-guides/json/ |
| PB-wkt | https://protobuf.dev/reference/protobuf/google.protobuf/ |
| PB-arenas | https://protobuf.dev/reference/cpp/arenas/ |
| PB-techniques | https://protobuf.dev/programming-guides/techniques/ |
| PB-cpp-gen | https://protobuf.dev/reference/cpp/cpp-generated/ |
| PB-conformance | https://github.com/protocolbuffers/protobuf/tree/main/conformance |
| buf | https://buf.build/docs/breaking/ |
| FB-internals | https://flatbuffers.dev/internals/ |
| FB-cpp | https://flatbuffers.dev/languages/cpp/ |
| FB-flatc | https://flatbuffers.dev/flatc/ |
| FB-support | https://flatbuffers.dev/support/ |
| FB-rust | https://flatbuffers.dev/languages/rust/ |
| CP-lang | https://capnproto.org/language.html |
| CP-encoding | https://capnproto.org/encoding.html |
| CP-cxx | https://capnproto.org/cxx.html |
| CP-faq | https://capnproto.org/faq.html |
| CP-otherlang | https://capnproto.org/otherlang.html |
| CP-home | https://capnproto.org/ |
| CP-tool | https://capnproto.org/capnp-tool.html |
| CP-0.7 | https://capnproto.org/news/2018-08-28-capnproto-0.7.html |
| AV-spec | https://avro.apache.org/docs/1.12.0/specification/ |
| AV-java | https://avro.apache.org/docs/1.12.0/getting-started-java/ |
| AV-docs | https://avro.apache.org/docs/1.12.0/ |
| AV-home | https://avro.apache.org/ |

A fact on this page lives here or in the page it links to, never in both. A
pull request that changes a fact changes this page in the same pull request.
