# Schema against Protocol Buffers, FlatBuffers, Cap'n Proto and Avro

The standing comparison. It is written to be checked: every cell about a
competitor is cited to that project's own documentation, every cell about
schema is cited to a specification section or an issue, and the same test is
applied to all five columns. Row-level evidence for FlatBuffers and Protocol
Buffers lives in [COMPARISON-TABLES.md](COMPARISON-TABLES.md); the packet-size
measurement is [COMPARISON.md](COMPARISON.md); the versioning promises are
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
verifier ships in three of its thirteen languages.

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
not understand and never fails on data from another build, and a committed
baseline refuses at compile time the edits no reader could report. The same
declaration cooks to a memory image the runtime opens with a header check and
a pointer, and lays out as a row block one language writes and another reads
at frame rate. One language, nine targets, one conformance corpus that every
target must reproduce byte for byte.

When to use theirs, in one line each, as the [FAQ](FAQ.md) already says: a
service that versions independently of its callers is Protocol Buffers'
case; in-place access to a large buffer you will not deserialize is
FlatBuffers' or Cap'n Proto's; a data pipeline where the writer's schema must
travel with the record is Avro's.

## How to read the matrix

One test for all five columns. A cell is ✅ when the feature exists in the
format **and** in all of its official language implementations, 🔶 when it
exists in the format but only some implementations carry it or it carries a
material limitation (the footnote says which), ❌ when it is absent or
declined on the record, and ? when the project's own documentation did not
settle it and this page will not guess.

Applied to schema that test is strict, and the page applies it anyway. The
packet wire and the compiler are nine languages deep and held there by CI, so
those rows are ✅. The table wire, the cook, the block and the text form are
complete in the C++ reference and the compiler and are being carried to the
other eight languages under one gate, full feature parity at each language's
performance floor before 3.0.0 (#366). Every 🔶 in the schema column that
cites #366 is that one gate, and when it closes the column is re-scored from
CI, not by hand. A reader who wants the per-language state today reads
[PORTING.md](PORTING.md), the register with a gate behind it.

Nine rows where all five columns agree are listed at the end rather than
shown. Editor support and big-endian hosts are not rows, because neither
could be sourced across five columns.

## The matrix

### Declarations and the schema language

| feature | schema | Protocol Buffers | FlatBuffers | Cap'n Proto | Avro |
|---|---|---|---|---|---|
| Numeric expressiveness beyond int32/int64/float/double: declared `min`/`max` bounds, sub-32-bit and 128-bit integers, fixed point | ✅ [s1] | ❌ [2] | 🔶 [3] | 🔶 [4] | 🔶 [5] |
| Compile-time constants usable inside the schema | ✅ [s2] | ❌ [6] | ❌ [6] | ✅ [7] | ❌ [8] |
| Nested value type with no per-instance framing | ✅ [s3] | ❌ [9] | ✅ [10] | 🔶 [11] | ✅ [12] |
| Bit-flag sets as a declaration | ✅ [s4] | ❌ [13] | ✅ [14] | ❌ [15] | ❌ [8] |
| Bounded arrays, strings and bytes: a declared capacity in the type | ✅ [s5] | ❌ [16] | 🔶 [17] | ❌ [18] | 🔶 [19] |
| Enum-keyed arrays (`[E]T`, one slot per variant, by name) | ✅ [s6] | ❌ [20] | ❌ [21] | ❌ [15] | ❌ [8] |
| Maps | 🔶 [s7] | ✅ [23] | 🔶 [24] | ❌ [25] | 🔶 [26] |
| Shared subtrees: one node referenced by many parents, written once | 🔶 [s8] | ❌ [27] | 🔶 [28] | ❌ [29] | ❌ [30] |
| Union arms may be any field type, not only a named record | 🔶 [s9] | 🔶 [32] | 🔶 [33] | ✅ [34] | ✅ [35] |
| Defaults for strings, bytes and composites | 🔶 [s9] | 🔶 [36] | ❌ [37] | ✅ [38] | ✅ [39] |
| Explicit presence on a scalar (set vs unset, distinct from the default) | ✅ [s10] | ✅ [40] | 🔶 [41] | ❌ [42] | 🔶 [43] |
| Required fields: a read fails if the field is absent | ❌ [s11] | ❌ [45] | ✅ [46] | ❌ [47] | ✅ [48] |
| Dynamic typing: generics, a type-erased `Any`, or schema-less values | ❌ [s12] | ✅ [50] | 🔶 [51] | ✅ [52] | ❌ [53] |
| User-extensible annotations or custom options | ❌ [s13] | ✅ [55] | ✅ [56] | ✅ [57] | ? |

### The wire

| feature | schema | Protocol Buffers | FlatBuffers | Cap'n Proto | Avro |
|---|---|---|---|---|---|
| Self-describing: the bytes alone are enough to walk the data | 🔶 [s14] | 🔶 [59] | ❌ [60] | ❌ [61] | ✅ [62] |
| Values compacted below their declared storage width | ✅ [s15] | ✅ [64] | ❌ [65] | 🔶 [66] | ✅ [67] |
| Scalars aligned inside the buffer | 🔶 [s16] | ❌ [69] | ✅ [70] | ✅ [71] | ❌ [72] |
| Data above 2 GiB | 🔶 [s17] | ❌ [74] | 🔶 [75] | 🔶 [76] | ✅ [77] |
| Byte-identical output: one input, one schema, one byte sequence, across implementations | ✅ [s18] | ❌ [79] | ? [80] | 🔶 [81] | 🔶 [82] |

### Evolution

| feature | schema | Protocol Buffers | FlatBuffers | Cap'n Proto | Avro |
|---|---|---|---|---|---|
| Add, remove or reorder a field freely | 🔶 [s19] | 🔶 [83] | ❌ [84] | ❌ [85] | 🔶 [86] |
| Rename a field without orphaning stored data | 🔶 [s20] | ✅ [88] | ✅ [89] | ✅ [90] | 🔶 [91] |
| Change a field's type with no silent misdecode | 🔶 [s21] | ❌ [92] | ❌ [93] | ❌ [94] | ✅ [95] |
| Change a declared default without reinterpreting stored bytes | 🔶 [s22] | ❌ [97] | ❌ [98] | ❌ [94] | ✅ [99] |
| Unknown fields preserved through a read-and-rewrite | ❌ [s23] | ✅ [101] | 🔶 [102] | ? | ❌ [103] |
| A per-event read report rather than pass/fail | 🔶 [s24] | ❌ [104] | ❌ [105] | ❌ [106] | ❌ [107] |
| An unknown enum value has a defined, non-fatal landing | 🔶 [s25] | 🔶 [108] | 🔶 [109] | ? | 🔶 [110] |

### Runtime forms, allocation, performance

| feature | schema | Protocol Buffers | FlatBuffers | Cap'n Proto | Avro |
|---|---|---|---|---|---|
| Read one field without parsing the whole buffer | 🔶 [s26] | ❌ [112] | ✅ [113] | ✅ [114] | ❌ [115] |
| No allocation on read | ✅ [s27] | ❌ [116] | 🔶 [117] | ✅ [118] | ❌ [115] |
| Arena or single-buffer allocation on write, never per node | 🔶 [s28] | 🔶 [119] | 🔶 [120] | ✅ [121] | ? |
| Exact serialized size known before writing | ✅ [s29] | ✅ [122] | ❌ [123] | ? | ? |
| Mutate a serialized buffer in place | ❌ [s30] | ❌ [125] | 🔶 [126] | ✅ [127] | ? |
| A same-build fast form: an O(1)-open cooked file and a fixed-stride cross-language row block | 🔶 [s31] | ❌ [128] | 🔶 [129] | 🔶 [130] | ❌ [115] |
| Generated code with no library runtime to link | 🔶 [s32] | ❌ [131] | ✅ [132] | ❌ [133] | ? |

### Validation of untrusted data

| feature | schema | Protocol Buffers | FlatBuffers | Cap'n Proto | Avro |
|---|---|---|---|---|---|
| Untrusted bytes are safe with no separate verification pass | 🔶 [s33] | ✅ [135] | ❌ [136] | ✅ [137] | ? |
| Value ranges enforced from the schema | ✅ [s1] | ❌ [2] | ❌ [3] | ❌ [4] | ❌ [5] |
| Depth and resource bounds against a hostile message | 🔶 [s34] | 🔶 [139] | 🔶 [140] | 🔶 [141] | ? |
| One cross-language conformance corpus that includes hostile cases | ✅ [s35] | 🔶 [142] | ? | ? | ? |

### Reflection, text, tooling

| feature | schema | Protocol Buffers | FlatBuffers | Cap'n Proto | Avro |
|---|---|---|---|---|---|
| Runtime reflection with no schema file present at runtime | 🔶 [s36] | ✅ [144] | ❌ [145] | 🔶 [146] | ✅ [147] |
| JSON or text form, in and out | 🔶 [s37] | ✅ [149] | 🔶 [150] | 🔶 [151] | ✅ [152] |
| Doc comments carried into generated code | ❌ [s38] | ✅ [154] | ✅ [155] | ? | ✅ [156] |
| Official language implementations at feature parity | 🔶 [s39] | 🔶 [158] | ❌ [159] | ❌ [160] | 🔶 [161] |

### Versioning

| feature | schema | Protocol Buffers | FlatBuffers | Cap'n Proto | Avro |
|---|---|---|---|---|---|
| Rename an enum variant, union arm or named type without orphaning stored data | 🔶 [s40] | ✅ [88] | ✅ [89] | 🔶 [163] | 🔶 [91] |
| A retired name or number cannot be silently reused | 🔶 [s41] | ✅ [165] | 🔶 [166] | 🔶 [167] | ❌ [168] |
| Defined widening or promotion of a field's type on read | ❌ [s42] | 🔶 [170] | 🔶 [93] | 🔶 [171] | ✅ [172] |
| The writer's schema is recoverable at read time | ❌ [s43] | ❌ [174] | 🔶 [175] | ❌ [61] | ✅ [176] |
| A revision marker in the bytes says which format version wrote them | 🔶 [s44] | ❌ [178] | 🔶 [179] | ❌ [180] | ✅ [181] |
| A first-party compile-time gate on breaking edits | ✅ [s45] | ❌ [183] | ✅ [184] | ❌ [185] | ? |
| Wire bytes promised stable across releases of the compiler | 🔶 [s46] | ❌ [79] | ? | 🔶 [187] | 🔶 [82] |

### Agreed, not shown

All five have enums, a discriminated union, recursive declarations,
multi-file schemas with imports, length-prefixed variable-length payloads,
contiguous scalar arrays, little-endian fixed-width scalars, a code generator
producing native types, and comments invisible to the wire.

## Where they are ahead, and what happens about it

- **Renames beyond fields.** Protocol Buffers, FlatBuffers and Cap'n Proto
  rename a variant or a type freely because the wire carries numbers; schema's
  `was` is a field attribute today. Tables get `was` under #396 and variants
  and arms under #442, both before 3.0.0.
- **Retired names.** Protocol Buffers' `reserved` is the right mechanism and
  schema lacks it: a removed name can be re-added years later and decode old
  bytes under a new meaning. The retired-names ledger in the baseline is
  #441, before 3.0.0.
- **Default changes.** Avro elides nothing, so a changed default cannot
  reinterpret stored bytes. Schema elides a field equal to its default, which
  is why its baseline refuses a default change at compile time; that is a
  guard where Avro has a guarantee, and the guard is opt-in until the nudge
  (#445) lands.
- **Unknown fields through a rewrite.** Protocol Buffers keeps them; schema
  drops them by decision (everything in schema has a schema) and counts them,
  and the versioning page names the one rule a studio must write for it:
  never overwrite a file whose read report is not silent.
- **Promotion.** Avro promotes `int` to `long` on read; schema reads a
  changed kind as the default and counts a kind mismatch. Declined, with the
  reason below.
- **Doc comments** reach generated code in three of the four; in schema they
  are deferred with the design pinned (SPEC §4.1).
- **Ecosystems.** Protocol Buffers has ten first-party languages and fifty-odd
  third-party ones; FlatBuffers thirteen; Avro publishes six SDKs and claims
  more. Schema has nine, and the claim it makes about them, byte-identical
  output held by one corpus in CI, is the one none of the four makes.
- **Tools.** `buf breaking`, `flatc --proto`, `capnp convert`. Schema's
  compile-time gate is first-party (`tables.baseline`); an importer from
  `.proto` or `.fbs` is not declined and not built (#467).

## Where schema is ahead

- **Bounds are bits.** `health int32 | min = 0, max = 1000` is ten bits on
  the packet wire; 28 bytes for a packet the others encode in 52, 56 and 72
  ([COMPARISON.md](COMPARISON.md)). None of the four can infer a range it was
  never told.
- **One language for all five kinds of data.** Packets, backend messages,
  saves, render blocks and cooked assets from one declaration, so there is no
  second copy of the types to drift.
- **The read is the report.** Every load counts what it did not know, what
  had the wrong kind, what it clamped and whether the bytes were malformed,
  and never fails on data from another build. The others return a boolean or
  throw.
- **The silent class is enumerated and refused.** The edits no reader can
  report, a changed default, a moved fixed-point scale, an enum respelled as
  its integer, are refused at compile time by a committed baseline with a
  reasoned, dated history. The nearest equivalents, `flatc --conform` and
  `buf breaking`, check structure, not meaning.
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
| Required fields | FlatBuffers, Avro in effect | Every field optional with a declared default is the ground rule. Protocol Buffers removed `required` and says why; schema never had it. |
| `Any`, generics, schema-less values | Protocol Buffers, Cap'n Proto, FlexBuffers | The typed bag is a union whose arms are tables. A type-erased value is a type the compiler cannot check. |
| Custom annotations | Protocol Buffers, FlatBuffers, Cap'n Proto | The attribute vocabulary is closed and an unknown attribute is a compile error; type tags are the claiming mechanism. |
| Unknown fields preserved through a rewrite | Protocol Buffers | Everything in schema has a schema. The old file kept whole is the preservation, and the never-clobber rule is the policy. |
| A widening path on read | Avro, Protocol Buffers' pairs | A whitelist of compatible pairs silently misdecodes everything outside it. A changed kind reads as the default and is counted; the pattern is a new field beside the old one and a load shim. |
| The writer's schema travelling with the data | Avro | One schema per build, identified by the protocol id and the build version, neither of which rides in a file. A tolerant wire that skips by kind needs no schema to walk, and a file that must say what wrote it puts a `format` field on its root table. |
| A canonical form of the data | Cap'n Proto | A load followed by a save through the reader's own schema is the canonical form: elision, order and duplicates are all resolved by it. Byte identity across schema's own writers is held by goldens. |
| Mutating a serialized buffer in place | Cap'n Proto, FlatBuffers | A cook is immutable and regenerated by the cache; the block form is the every-frame mutable answer. |

## Versioning, mechanism by mechanism

Each competitor defends against a failure with a mechanism. The question for
each is whether schema covers the same failure, declines it with a reason, or
has a gap.

**Protocol Buffers: field numbers, `reserved`, unknown-field preservation,
open enums.** Numbers identify a field for life; `reserved` stops a retired
number or name from being reused; unknown fields survive a rewrite; an open
enum keeps a value it does not know. Schema covers identity with the name
hash and `was`, and covers the unknown-value case with `None` counted. The
retired-name ledger is the gap (#441). Unknown-field preservation is declined.

**FlatBuffers: append-only tables, `deprecated` never removed, `--conform`,
`file_identifier`.** Position is identity, so a field is never removed and
new ones go at the end; `flatc --conform` refuses an edit that breaks the
rule; a four-character identifier says which schema wrote a buffer. Schema
covers the first two with name identity, which makes removal and reorder
free, and the gate with the baseline. The identifier is covered by the
`format` field pattern and, for the format's own revision, by the form byte
(#435).

**Cap'n Proto: ordinals, explicit type ids, the list-of-structs upgrade,
canonicalization.** Ordinals never change; a renamed or moved type pins its
id explicitly or silently gets a new one; one promotion path exists; a
canonical form makes messages comparable. Schema covers ordinals with names,
covers the type-id case with table `was` (#396), declines promotion, and
answers canonicalization with load-then-save.

**Avro: the writer's schema with the data, resolution, aliases, promotion,
defaults that never reinterpret.** The reader always has the writer's schema
and resolves against it; aliases carry renames; promotion widens on read; no
elision means no default hazard. Schema declines carrying the schema, covers
resolution with the tolerant read, covers aliases with `was` (fields today,
every vocabulary before 3.0.0), declines promotion, and guards the default
hazard with the baseline instead of a guarantee.

The gaps, all before 3.0.0: the retired-names ledger (#441), `was` for
variants and arms (#442), the form byte (#435). The one row where a
competitor's guarantee is strictly stronger than schema's guard is Avro's on
defaults, and the page says so above.

## Footnotes

Schema cells cite the specifications and issues; competitor cells cite the
project's own documentation by the short names in the source list at the end,
with the section that establishes the claim.

**Schema**

- s1. `| min, max` is part of the type; the type wire sizes bits from the bound and the table wire clamps and counts on load (SPEC §4.3, SPEC-TABLES §4). `fixed(I,F)`, `ufixed(I,F)`, int128 and uint128 ride under table-wire kinds of their own (SPEC-TABLES §3).
- s2. `const`, folded at compile time and usable in bounds, defaults and capacities (SPEC §4.4).
- s3. A `type` inside a `type`, or a fixed table inside a table, is inline storage with no framing on the packet wire; on the table wire a nested table is one length-framed node (SPEC-TABLES §3).
- s4. `flags` declarations, one bit per variant (SPEC §4.2).
- s5. `[N]T`, `[..N]T`, `string(N)`, `bytes(N)`: the capacity is in the type and the packet wire sizes the count from it (SPEC §4.3).
- s6. `[E]T`, a slot per variant addressed by name, with a keyed wire kind (SPEC-TABLES §2.4, §3.2).
- s7. Designed for tables after 3.0.0; a map makes the table variable (SPEC-TABLES §2.8, #380).
- s8. A pointer field `*T` names a node once in a flat node table and every reference is an index (SPEC-TABLES §3.1). C++ reference and the compiler today; the other backends under #366. The amplification bound on an untrusted read is #466.
- s9. Union arms of any field type and defaults for string, bytes and flags are decided and landing (#396).
- s10. `?T` on both wires; on the packet wire it is one presence bit (SPEC §4.2).
- s11. Declined: every field optional with a declared default (SPEC-TABLES §4).
- s12. Declined on #396 and in COMPARISON-TABLES.md; the typed bag is a union whose arms are tables.
- s13. Declined: the attribute vocabulary is closed and an unknown attribute is a compile error (SPEC §4.2).
- s14. Kinds and lengths ride on the tolerant wire, so any reader skips anything it does not know; names ride as hashes, not text, so the bytes cannot be rendered with names without the schema (SPEC-TABLES §3). Table wire, #366.
- s15. The packet wire sizes each field from its bounds (SPEC §4.3); the table wire deliberately does not compact.
- s16. No alignment on the tolerant wire; the cook and the block are the aligned forms (SPEC-TABLES §7.2, §19.3). #366.
- s17. Eight-byte region references and 64-bit cook part lengths, no aggregate ceiling (SPEC-TABLES §6.3, §7.1); the table wire's lengths go 64-bit under #435. Table forms, #366.
- s18. The packet wire: one standard, nine languages, pinned goldens in CI, `Measure` equals `Save` (SPEC §3, VERSIONING.md). The table wire has the same rule in the C++ reference and the compiler, #366.
- s19. Name identity: add anywhere, remove, reorder, all reported by the read (SPEC-TABLES §4). #366.
- s20. `| was = "old"` keeps the wire id; a bare rename is a removal and an addition the compiler cannot see, and the baseline is being taught to warn on the pair (#444). Fields today; every vocabulary before 3.0.0 (#396, #442). #366.
- s21. A changed kind reads as the default and is counted `kind_mismatch`; never truncated, never misread (SPEC-TABLES §4). #366.
- s22. Silent on the wire, refused at compile time by a committed baseline (SPEC-TABLES §18); 🔶 because the baseline is opt-in until #445.
- s23. Declined by decision; the read report counts what a rewrite would drop (VERSIONING.md, the never-clobber rule).
- s24. Four counters and a `malformed` flag on every load (SPEC-TABLES §4). #366.
- s25. An unknown variant reads `None` and is counted (SPEC-TABLES §4). #366.
- s26. The cook: `Open` is a header match and a pointer (SPEC-TABLES §7). C++ reference and the compiler; #366.
- s27. The packet wire reads into the caller's struct with no allocation, in every language, held by an allocation gate (PORTING.md I1, I12). The table wire's fixed class likewise; the variable class reads through a region the caller sizes with `LoadMeasure` (SPEC-TABLES §6.5).
- s28. The variable class builds through an arena (SPEC-TABLES §9). C++ reference; #366.
- s29. `Measure` before `Save`, both wires, equal by test (SPEC-TABLES §9).
- s30. Declined: a cook is immutable and regenerated by the cache; the block form is the mutable answer (SPEC-TABLES §7, §19).
- s31. The cook (SPEC-TABLES §7) and the block (§19), with the block's two-sided layout assertion at open. C++ writes and C# reads today; #366.
- s32. Six of the nine targets link a small `serialize` runtime for the packet wire; Dart, Java and Elixir generate self-contained output (VERSIONING.md).
- s33. The tolerant read is the validator: every length bounds-checked, every count against its body, ranges clamped, a bad sub-table stopping only itself (SPEC-TABLES §4, §6.5). The independent-oracle fuzzer that proves it is landing for the C++ reference (#429) and owed to every port (#391).
- s34. By-value depth is fixed by the schema and the pointer graph is flat, so a long chain is not a recursion (SPEC-TABLES §3.1); the stated bound on materialization is #466. #366.
- s35. One corpus, every language, hostile rows included; a port answers ABSENT per case where it lacks a construct rather than passing by omission (test/conformance, #383).
- s36. Descriptors in every table's generated header, no schema files, no RTTI (SPEC-TABLES §8.1). #366.
- s37. JSON in and out by one walk, `| json = "key"`, the read report, `&node` for a shared node (SPEC-TABLES §16). #366.
- s38. Deferred with the design pinned (SPEC §4.1).
- s39. Nine languages byte-identical on the packet wire in CI; tables carried under #366, which is the declared 3.0.0 gate and not the present state.
- s40. Table `was` (#396) and variant and arm `was` (#442), before 3.0.0.
- s41. The retired-names ledger in the baseline (#441), before 3.0.0.
- s42. Declined, with the reason in the table above.
- s43. Declined, with the reason in the table above.
- s44. The form byte at the head of every table-wire file (#435), before 3.0.0; the cook and the block already refuse on the build version.
- s45. `tables.baseline` with `--update --reason` (SPEC-TABLES §18) for the table wire; for the packet wire the protocol id and the pinned goldens.
- s46. The promise is on VERSIONING.md and the goldens hold it across a schema's edits; across compiler releases it needs the codec-law line and the differential gate (#463).

**Protocol Buffers, FlatBuffers, Cap'n Proto, Avro**

2. Protocol Buffers has int32/int64/uint32/uint64/sint/fixed/sfixed/float/double/bool and no sub-32-bit integer, 128-bit integer, fixed point or declared range (PB-proto3, Scalar Value Types).
3. FlatBuffers has `byte` through `ulong`, so sub-32-bit yes; no 128-bit, no fixed point, no range (FB-schema, Tables).
4. Cap'n Proto has Int8 to Int64 and the unsigned set; no 128-bit, no fixed point, no range (CP-lang, Built-in Types).
5. Avro's primitives stop at int, long, float, double; `decimal` is a logical type over bytes or fixed, an arbitrary-precision annotation whose per-language support varies (AV-spec, Primitive Types; Logical Types).
6. Neither Protocol Buffers nor FlatBuffers has a constant declaration (PB-proto3 and FB-schema, top-level declaration lists).
7. `const` exists and may be referenced inside another value including a field default (CP-lang, Constants).
8. Avro's schema is JSON; no constant, flags or keyed-array construct exists in the specification (AV-spec, Complex Types).
9. Every nested message is a length-delimited record: a tag and a length (PB-encoding, Length-Delimited Records).
10. A FlatBuffers `struct` is stored inline in its parent (FB-internals, Structs).
11. A Cap'n Proto struct field is a pointer into the message; structs are inline only as elements of a composite list, a deliberate trade for random access and the list-upgrade rule (CP-encoding, Structs; Lists).
12. An Avro record is the concatenation of its fields' encodings with no framing (AV-spec, Binary Encoding).
13. No flags construct; a bitmask is an integer by convention (PB-proto3).
14. `bit_flags`: an unsigned value N in the schema represents 1<<N (FB-schema, attributes).
15. No flags or keyed-array construct; enumerants are numbered from zero and are not numeric (CP-lang, Enums).
16. `repeated`, `string` and `bytes` are unbounded up to the message ceiling (PB-proto3; PB-limits).
17. `[T:N]` fixed-length arrays are supported only in a `struct` (FB-schema, Fixed-length Arrays).
18. `List(T)`, `Text` and `Data` take no bound (CP-lang, Built-in Types).
19. Avro's `fixed` is an exact-size byte type; `string`, `bytes`, `array` and `map` are unbounded (AV-spec, Fixed).
20. Enum keys are refused in a map (PB-proto3, Maps).
21. No enum-keyed array; the idiom is a sorted vector with `LookupByKey`, by value (FB-cpp, Sorting a vector of tables).
23. `map<K,V>` in every official language; keys integral or string; iteration and wire order undefined (PB-proto3, Maps).
24. No map type; the sorted-vector idiom is documented on the C++ page, not as a cross-language feature (FB-cpp; FB-schema, `key`).
25. `Map(K,V)` appears only as an example of a user-defined generic (CP-lang, Generic Types).
26. Map keys are strings (AV-spec, Maps).
27. A tree of length-delimited records; no reference type (PB-encoding).
28. A table may point at the same value twice if the builder serializes the same offset twice; object cycles are not allowed (FB-internals).
29. One pointer per object, a tree not a graph; cyclic or overlapping pointers can send a reader into an infinite loop, which is what the traversal limit defends (CP-encoding, Messages).
30. No reference type; recursion is a union naming the record, which yields a tree (AV-spec, Complex Types).
32. `oneof` admits any type except map and repeated fields (PB-proto3, Oneof).
33. Union members are tables; other member types and vectors of unions are experimental, the latter C++ only (FB-schema, Unions).
34. A union is two or more fields sharing storage, so any field type is an arm, including `Void` (CP-lang, Unions).
35. A union is a JSON array of schemas, any schema a branch, with two restrictions (AV-spec, Unions).
36. proto3 has no user-declared defaults; proto2 and editions do (PB-proto3, Default Values; PB-presence).
37. Only scalars take explicit defaults; strings, vectors and tables are null when absent (FB-schema, Tables).
38. Defaults for `Text`, `Data`, `List` and nested structs (CP-lang, Structs).
39. A default for every field type (AV-spec, Record).
40. `optional` fields track presence (PB-proto3, Field Labels).
41. Optional scalars via `= null`, marked No for Python, PHP and Dart in the support matrix (FB-schema, Optional Scalars; FB-support).
42. No scalar presence: data-section fields are stored XOR their default, so unset and set-to-default are one encoding; a pointer's null is the only absence (CP-encoding, Value Encoding).
43. The idiom is a union with `null`, a value not a presence flag (AV-spec, Unions).
45. `required` was removed in proto3; the guidance is never to add one (PB-dos).
46. `(required)` is checked by the verifier (FB-schema, attributes).
47. No required marker (CP-lang).
48. A reader field with no default and no writer counterpart signals an error for the whole read (AV-spec, Schema Resolution).
50. `Any` with a type URL, and `Struct`/`Value` in the well-known types (PB-proto3, Any; PB-wkt).
51. FlexBuffers, marked Yes for C++, Java, Rust and Swift and ? for most others (FB-support).
52. Generics are first class; an omitted parameter is `AnyPointer` (CP-lang, Generic Types).
53. No generics and no `Any`; the writer's schema is always available, a different solution to the same problem (AV-spec).
55. Custom options with retention and target restrictions (PB-editions, Custom Options).
56. User-defined attributes, declared, queryable from a parsed schema at runtime (FB-schema, Attributes).
57. `annotation foo(struct, enum) :Text;` with fourteen targets plus `*` (CP-lang, Annotations).
59. Field numbers and wire types ride; names and declared types need the definition; the overview lists "don't inherently self-describe" (PB-encoding, Message Structure; PB-overview).
60. No format identification or versioning information in the buffer, by design (FB-internals).
61. Field positions are computed from the schema; the message carries no field identity (CP-encoding).
62. The container file embeds the writer's schema; single-object encoding carries `C3 01` plus a schema fingerprint. The bare value encoding is untagged (AV-spec, Object Container Files; Single Object Encoding; Binary Encoding).
64. Varints and ZigZag (PB-encoding).
65. Every scalar at its declared width, aligned; that is what buys in-place access (FB-internals).
66. Packing is an optional pass over the unpacked layout (CP-encoding, Packing; CP-faq).
67. int and long are zig-zag varints (AV-spec, Binary Encoding).
69. No alignment (PB-encoding).
70. Scalars aligned to their own size; `force_align` overrides (FB-internals; FB-schema).
71. Objects aligned to word boundaries, primitives to a multiple of their size (CP-encoding).
72. A byte stream with no alignment (AV-spec, Binary Encoding).
74. A serialized proto must be under 2 GiB (PB-limits, Message Size).
75. `uoffset_t` is a `uint32_t`; 64-bit offsets are C++ only (FB-internals; FB-64).
76. Segment ids are 32 bits, so the aggregate is not 32-bit capped; a single list is capped at 2^29 elements; the C++ reader's traversal limit defaults to 64 MiB (CP-encoding, Lists; Far Pointers; CP-cxx, Security Tips).
77. Block counts and lengths are 64-bit varints; the specification states no ceiling and per-implementation ceilings were not checked (AV-spec, Binary Encoding).
79. Serialization stability is not guaranteed across binaries or builds, and default serialization is not deterministic (PB-dos; PB-encoding).
80. No claim either way; the builder's freedom to share an offset means two conforming builders can emit different bytes for the same data (FB-internals). Marked ? rather than ❌ because it is an inference from absence.
81. Canonicalization is fully specified and is a separate conversion, not the default output (CP-encoding, Canonicalization; CP-tool).
82. A record's bytes are determined by the schema, but array and map block boundaries are the writer's choice; Avro's Parsing Canonical Form canonicalizes schemas, not data (AV-spec, Binary Encoding; Parsing Canonical Form).
83. Adding and reordering are free; removal reserves the number forever, with the consequences of reuse spelled out (PB-proto3, Updating A Message Type; Reserved Fields).
84. New fields go at the end and fields are never removed, unless `id` is used on every field (FB-evolution; FB-schema).
85. New members take larger numbers; source order is free; numbers never change; no removal (CP-lang, Evolving Your Protocol).
86. Fields match by name and a writer's extras are ignored, but a reader field without a default is a hard error against older data (AV-spec, Schema Resolution).
88. Names are not on the binary wire; reusing an old name is generally safe except in TextProto and JSON (PB-proto3, Updating A Message Type).
89. Renaming tables and fields is fine because names are not serialized; same JSON caveat (FB-evolution).
90. Any symbolic name can change as long as the type id and ordinals stay (CP-lang, Evolving Your Protocol).
91. Aliases exist on named types and fields; an implementation may optionally use them (AV-spec, Aliases).
92. Almost never change a field's type; the compatible-pairs list is narrow and a change inside it can still truncate silently (PB-dos; PB-proto3, Updating A Message Type).
93. Maybe OK, only for the same width (FB-evolution).
94. A field's type or default value cannot change (CP-lang, Evolving Your Protocol).
95. Resolution promotes or signals an error; never a misdecode, and the failure is the whole read (AV-spec, Schema Resolution).
97. Almost never change a default; it causes version skew (PB-dos).
98. Not OK: data written without the value relied on generated code for the default (FB-evolution).
99. Every field is always written; a default is used only when the writer's schema lacks the field, so a default change cannot reinterpret stored bytes (AV-spec, Record).
101. proto3 messages preserve unknown fields through parsing and serialization; lost through JSON (PB-proto3, Unknown Fields).
102. A buffer forwarded whole keeps everything; the object API's unpack-and-repack does not (FB-evolution; FB-cpp).
103. A writer's field not in the reader's record is ignored (AV-spec, Schema Resolution).
104. Success or failure plus the unknown-field set (PB-proto3; PB-message).
105. The verifier is a boolean (FB-cpp, Verifying a buffer).
106. Limits throw; nothing reports what was skipped (CP-cxx, Security Tips).
107. Errors are signalled; no counted report (AV-spec).
108. Open enums keep the raw value; closed enums move it to unknown fields and re-serialize it out of place. C#, Go, JSPB and Ruby treat all enums as open and Dart as closed regardless of syntax (PB-enum).
109. No defined landing; handling is the application's (FB-schema, Enums).
110. A declared enum `default` is used, otherwise an error is signalled (AV-spec, Enums; Schema Resolution).
112. Messages are parsed; equality requires full parsing (PB-overview).
113. Access the data directly without unpacking or parsing (FB-home).
114. No encoding step; one field readable without parsing the whole; mmap (CP-home).
115. Values are variable-length and untagged, decoded into objects; no random access, no cooked form, deliberately (AV-spec, Binary Encoding).
116. Per message, per string, per repeated field; arenas are C++ (PB-message; PB-arenas).
117. No heap for in-place access; the object API unpacks into `std::string` and `std::vector` (FB-home; FB-cpp).
118. Reads are pointer arithmetic over the buffer (CP-cxx).
119. Arenas are a C++ feature (PB-arenas).
120. The builder grows one buffer by reallocation (FB-cpp).
121. Messages are built arena-style, sequentially in a segment (CP-cxx, Tips and Best Practices).
122. `ByteSizeLong()` (PB-message).
123. The size is known after `Finish` (FB-cpp).
125. Parse, mutate the object, re-serialize; no in-place editing (PB-encoding; PB-message).
126. `--gen-mutable` scalar mutators, Yes for C++, Java, C#, Go and Swift (FB-flatc; FB-support).
127. Builders write in place; orphans move subtrees within a message (CP-cxx, Orphans).
128. No cooked or block form (PB-overview; PB-encoding).
129. The buffer is the cooked form; nothing asserts both languages' native layout at open (FB-internals).
130. mmap is documented; there is no fixed-stride, layout-asserted row block (CP-home; CP-lang).
131. Generated C++ derives from `Message` in `libprotobuf` (PB-message).
132. The C++ library is header-only (FB-cpp).
133. Link `libcapnp` and `libkj` (CP-cxx, Generating Code).
135. The parser validates structure; UTF-8 is verified for `string`; recursion limits bound a hostile message (PB-encoding; PB-features; PB-limits).
136. Offsets are not verified at run time and a malformed buffer can crash the reader; a separate verifier ships for C++, C and Swift of thirteen (FB-cpp, Verifying a buffer; FB-support).
137. Designed to be safe against malicious input, with the caveat that it has not undergone a formal security review (CP-faq).
139. Recursion limits vary: Java 100, C++ 100, Go 10,000 with a planned reduction (PB-limits).
140. Verifier defaults of depth 64 and a million tables, where a verifier ships (FB-cpp).
141. Nesting depth 64 and a 64 MiB traversal limit, documented for C++; other implementations unreviewed by the project (CP-cxx, Security Tips; CP-otherlang).
142. A conformance suite exists; whether it carries hostile inputs is not established from the documentation (PB-conformance).
144. `Reflection` and `DynamicMessage` are in the runtime (PB-message).
145. Full reflection needs the binary schema as a separate artifact; C++, with Basic for C (FB-flatc, `--schema`; FB-support).
146. `capnp/schema.h`, `capnp/dynamic.h` and `SchemaLoader`, documented for C++ (CP-cxx, Dynamic Reflection).
147. The writer's schema is always present and the generic API reads any record without code generation (AV-java).
149. ProtoJSON and the text format in every runtime (PB-json; PB-text).
150. JSON parsing is Yes for C++, C and Lobster; `flatc --json` converts offline (FB-support; FB-flatc).
151. `capnp convert` moves between binary, packed, flat, canonical, text and json, documented in the 0.7 release notes rather than the tool page; the library JSON is C++ (CP-tool; CP-0.7).
152. The JSON encoding is part of the specification; `avro-tools` converts offline (AV-spec, JSON Encoding; AV-java).
154. Comments are preserved in `SourceCodeInfo` and emitted by the generators (PB-wkt).
155. `///` comments reach generated code and the binary schema (FB-schema, Comments & Documentation).
156. The `doc` attribute on records and fields (AV-spec, Record).
158. Ten first-party languages plus fifty-odd third-party ones, with the enum nonconformance documented by the project itself (PB-overview; PB-3p; PB-enum).
159. Thirteen languages; JSON parsing in three, reflection in two, the verifier in three, mutation in five; C++ has the richest feature set (FB-support).
160. The C++ reference is the only reviewed implementation; the C implementation is no longer maintained; several are serialization-only (CP-otherlang).
161. Six documented SDKs and a home page claiming more; no per-feature matrix (AV-docs; AV-home).
163. An omitted type id is derived from the parent scope's id and the declaration's name, so a rename or move silently changes it unless pinned explicitly (CP-lang, Unique IDs).
165. `reserved` for numbers and names, with the consequences of reuse enumerated (PB-proto3, Reserved Fields).
166. Removal is forbidden outright, so reuse cannot arise while the rule holds; no reserved list if it is broken (FB-evolution; FB-schema, `deprecated`).
167. Ordinals never change and removal is not permitted; no reserved mechanism (CP-lang, Evolving Your Protocol).
168. Aliases point forward; nothing marks a name retired (AV-spec, Aliases).
170. The compatible-pairs list; a truncation inside it is silent (PB-proto3, Updating A Message Type).
171. One path: a list of primitives may become a list of structs whose first field is that primitive (CP-lang, Evolving Your Protocol; CP-encoding, Lists).
172. int to long, float or double; long to float or double; float to double; string and bytes either way (AV-spec, Schema Resolution).
174. `Any` carries a type URL, a name resolved elsewhere (PB-proto3, Any).
175. `flatc -b --schema` emits a binary schema as a separate artifact; `file_identifier` is four hand-chosen characters (FB-flatc; FB-schema).
176. Container files embed the schema; single-object encoding carries its fingerprint (AV-spec).
178. Six wire types, frozen; no revision marker, a deliberate trade (PB-encoding).
179. `file_identifier` identifies the schema author's format, not the FlatBuffers revision (FB-schema).
180. A segment table and segments; no magic, no version field (CP-encoding).
181. Container files begin `Obj` `0x01`; single-object encoding begins `C3 01` and names its version (AV-spec).
183. `buf breaking` is built by Buf Technologies, not the Protocol Buffers project, which offers prose guidance (buf; PB-3p).
184. `flatc --conform` gives errors if a schema is not an evolution of the one given (FB-flatc).
185. Evolution rules are prose; no tool checks them (CP-lang; CP-tool).
187. The evolution rules preserve the canonical encoding across schema changes; no cross-release byte-stability promise is made (CP-lang, Evolving Your Protocol).

## Sources

All pages fetched 2026-09-04.

| short | page |
|---|---|
| PB-proto3 | https://protobuf.dev/programming-guides/proto3/ |
| PB-encoding | https://protobuf.dev/programming-guides/encoding/ |
| PB-presence | https://protobuf.dev/programming-guides/field_presence/ |
| PB-enum | https://protobuf.dev/programming-guides/enum/ |
| PB-limits | https://protobuf.dev/programming-guides/proto-limits/ |
| PB-overview | https://protobuf.dev/overview/ |
| PB-dos | https://protobuf.dev/best-practices/dos-donts/ |
| PB-editions | https://protobuf.dev/programming-guides/editions/ |
| PB-features | https://protobuf.dev/editions/features/ |
| PB-json | https://protobuf.dev/programming-guides/json/ |
| PB-text | https://protobuf.dev/reference/protobuf/textformat-spec/ |
| PB-wkt | https://protobuf.dev/reference/protobuf/google.protobuf/ |
| PB-message | https://protobuf.dev/reference/cpp/api-docs/google.protobuf.message/ |
| PB-arenas | https://protobuf.dev/reference/cpp/arenas/ |
| PB-conformance | https://github.com/protocolbuffers/protobuf/tree/main/conformance |
| PB-3p | https://github.com/protocolbuffers/protobuf/blob/main/docs/third_party.md |
| buf | https://buf.build/docs/breaking/ |
| FB-schema | https://flatbuffers.dev/schema/ |
| FB-evolution | https://flatbuffers.dev/evolution/ |
| FB-internals | https://flatbuffers.dev/internals/ |
| FB-cpp | https://flatbuffers.dev/languages/cpp/ |
| FB-flatc | https://flatbuffers.dev/flatc/ |
| FB-support | https://flatbuffers.dev/support/ |
| FB-home | https://flatbuffers.dev/ |
| FB-64 | https://github.com/google/flatbuffers/issues/7537 |
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
