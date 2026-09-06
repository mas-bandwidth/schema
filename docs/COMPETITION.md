# Schema against Protocol Buffers, FlatBuffers, Cap'n Proto and Avro

The standing comparison. It is written to be checked: every cell about a
competitor is cited to that project's own documentation, every cell about
schema is cited to a specification section, one of the repository's registers
or an issue, and the same test is applied to all five columns. Row-level
evidence for FlatBuffers and Protocol Buffers lives in
[COMPARISON-TABLES.md](COMPARISON-TABLES.md) and those cells cite its rows
instead of repeating them; the packet-size measurement is
[COMPARISON.md](COMPARISON.md); the versioning promises are
[VERSIONING.md](VERSIONING.md). This page adds Cap'n Proto and Avro, a
versioning group, and three lists at the end: where they are ahead, where
schema is ahead, and what schema declines with the reason.

## What each of these is for

Each of the four makes a trade on purpose, and the trade is the thing to
compare.

**Protocol Buffers** identifies a field by a number you assign and never
reuse, carries every field with a tag, preserves the fields it does not know
through a read and a rewrite, and disclaims byte stability across builds. That
is the right shape for a service API versioned independently over years by
teams that never deploy together. Its own guidance is honest about the costs:
never change a field's type, never change a default, reserve every retired
number.

**FlatBuffers** puts every scalar at its declared width and alignment behind a
vtable so that a reader touches one field without parsing the buffer. The
buffer is its own cooked form: little-endian, position-independent,
build-independent, memory-mappable. The price is that a buffer from an
untrusted source needs a separate verification pass before it is safe to
touch, and the verifier does not ship in every one of its thirteen languages.

**Cap'n Proto** reads in place too, and adds an RPC system. Its message is a
tree by construction, one pointer per object, and that is what lets its reader
bound traversal against a hostile message. Its scalars are stored XOR their
declared default, so "unset" and "set to the default" are the same bytes and a
default can never change. Both properties are the encoding doing its job, not
gaps in it.

**Avro** carries the writer's schema with the data, in the container file or
as a fingerprint on each object, and resolves it against the reader's schema
at read time. Nothing is elided, so a changed default can never reinterpret
stored bytes. A mismatch fails the whole read, on purpose, because Avro's
users are pipelines where a wrong row is worse than no row.

**Schema** is for a game. One declaration produces two wires and three runtime
forms, so packets, backend messages, saves, render blocks and cooked assets
come from one source with no second copy of the types to drift. One language,
nine targets, one conformance corpus every target is held to case by case,
with what a target cannot yet answer named instead of passed over. What the
two wires are and how they are scored is the next section.

When to use theirs, in one line each: in-place access to a large buffer you
will not deserialize is FlatBuffers' or Cap'n Proto's; an RPC system with
promise pipelining is Cap'n Proto's; a data pipeline where the writer's schema
must travel with the record is Avro's; and a service whose callers you cannot
rebuild, where the ECOSYSTEM around the format is most of what you are buying,
gRPC and `buf` and a schema registry and forty-plus third-party language
implementations, is Protocol Buffers'.

**A service versioned independently of its callers is no longer on that list**,
and this page says so where it used to decline the case. That is what a
`table` is for and what the table wire guarantees: a field, an enum variant, a
union arm and a table are identified by the hash of their name, so fields are
added anywhere, removed and reordered, variants are inserted in the middle, a
reader counts what it cannot name and never fails on another build's data, and
`was` carries a rename. The MESSAGE FORM is the shape that makes it competitive
by the byte rather than only by the feature: a peer announces its unit's whole
vocabulary once a connection and every message after it carries references
instead of ids, which turns a 106-byte login into 58 against proto3's 49
(SPEC-TABLES §3.3). What schema still declines beside Protocol Buffers is RPC.
There is no service definition, no stub generation and no request-response
machinery.

**The gaps in that claim are named rather than argued away**, and each is an
issue this page's rows already carry: the message form is specified and nothing
writes it yet ([#523](https://github.com/mas-bandwidth/schema/issues/523)), and
the `widened` promotion rule and the `TableRefuseReason` enum are the C++
reference's and the tool's with the eight ports owing them
([#366](https://github.com/mas-bandwidth/schema/issues/366)); unknown
fields are dropped on rewrite unless a caller opts into retention, which is
specified and unbuilt
([#525](https://github.com/mas-bandwidth/schema/issues/525)); a retired name can
be re-added and decode old bytes under a new meaning
([#441](https://github.com/mas-bandwidth/schema/issues/441)); and the id-table
wire is the C++ reference's and the tool's, with the eight ports still writing
the previous form
([#511](https://github.com/mas-bandwidth/schema/issues/511) to
[#518](https://github.com/mas-bandwidth/schema/issues/518)). Every one of them
is before 3.0.0 ([VERSIONING.md](VERSIONING.md)).

## How to read the matrix

### The marks

**One test for all five columns, and a row is answered from the wire the row
is about.** A cell is ✅ when the feature exists in the format **and** in all
of that project's official language implementations, 🔶 when it exists but
only some of them carry it or it carries a material limitation the footnote
names, ❌ when it is absent or declined on the record, and ? when the
project's own documentation neither makes the claim nor denies it. **Absence
from a complete list in a project's own documentation**, a scalar table, a
declaration list or a set of evolution rules, **is ❌, not ?**, and that
reading is the same in every column.

**A feature that is decided but not built is ❌**, with the issue in the
footnote. 🔶 describes something shipped that falls short, never something on
the way; the four competitors are scored on the format they ship and so is
this one.

**"All of a project's official implementations" means more than one only where
there is more than one.** Cap'n Proto's official set is the C++ reference
alone, by its own statement that implementations in other languages are
maintained by their authors and unreviewed
([CP-otherlang](https://capnproto.org/otherlang.html)). A property documented
for C++ is therefore ✅ in that column, and the parity row, which asks how many
official implementations there are, is where the single set is answered ❌.

**On the add, remove and reorder row, a retired identifier is not the
question.** Every one of the five keeps a retired field's identifier out of
circulation, or fails to and says so: Protocol Buffers reserves the number,
FlatBuffers deprecates and keeps the slot, Cap'n Proto's numbers only ever
grow, Avro's aliases point forward, and schema frees the name hash and is ❌
for it. That fact is the versioning group's own row, so no column is marked
down for it twice. The add, remove and reorder row asks three things instead:
whether a field can be added without moving anything else, whether it can stop
being carried, and whether source order is free.

### The words this page uses

- **The packet wire** is what a `type` produces: the declared bounds decide
  the bits, both sides ship together, and they refuse each other on a protocol
  id if they do not match.
- **The table wire** is what a `table` produces: a field is identified by the
  hash of its name, a reader counts what it could not understand and never
  fails on data from another build.
- **Protocol id.** The number two peers compare before they talk. It versions
  the packet wire and moves when a `type`, `enum`, `flags` or `union`
  declaration moves ([VERSIONING.md](VERSIONING.md), promise 4).
- **Kinds.** Every table-wire value rides behind a one-byte kind from a closed
  numbered set (SPEC-TABLES §3), so a reader skips a value it does not know by
  its stated length.
- **The form byte** is the first byte of a table-wire file, naming the wire's
  own revision so a reader that meets a later form refuses by name
  ([#435](https://github.com/mas-bandwidth/schema/issues/435)).
- **The committed baseline** is `tables.baseline`, a canonical text projection
  of a unit's tables kept beside the schema. `schema check` compares the
  declaration against it and refuses the edits the wire cannot report. It is
  opt-in: no file, no check (SPEC-TABLES §18).
- **The never-clobber rule**: a rewriting tool never overwrites a file whose
  read report is not silent ([VERSIONING.md](VERSIONING.md)).
- **The cook** is an offline memory image of one table tree. `Open` is a
  header check and a pointer, with nothing per node (SPEC-TABLES §7).
- **The row block** is a contiguous extent of fixed-size rows at a stride the
  compiler computes, written by one language and read by another
  (SPEC-TABLES §19).
- **`LoadMeasure`** prices a wire's region before anything is allocated, so a
  caller refuses a size instead of meeting it (SPEC-TABLES §6.5).
- **Descriptors** are the constant tables of a declaration's facts, name,
  kind, id, offset, bounds, guards and nesting, emitted into the generated
  header so a walker reads any table with no schema file present
  (SPEC-TABLES §8.1).
- **Classes.** The compiler derives a table's class from its declaration
  (SPEC-TABLES §2.2): a **fixed** table has no pointer and is a plain struct,
  a **variable** table declares one. Three further groups of constructs are
  named by the conformance corpus and scored separately here. The **message**
  class is a union whose arms are tables, an array of unions and an optional
  array (SPEC-TABLES §2.6, §2.3); the **wide** class is the 128-bit and
  fixed-point scalars (§3); the **blob** class is the `*bytes` buffer node
  (§2.5).
- **The tool** is `schema`, the command-line compiler. **The reference** is
  the C++ backend, first in the conformance harness's registry and the one
  every other leg is compared against.
- **A registered leg** is one backend's conformance driver, discovered by the
  harness at `test/conformance/<lang>/driver`. A leg that does not carry a
  construct answers **ABSENT** for that case instead of passing by omission,
  and its cell reads `pass 16/16 +4a`. The reference leg may not answer ABSENT
  at all.
- **Goldens** are bytes pinned in the repository that the tests compare
  against. A change that moves one is **stop-the-line**: the work stops until
  the movement is explained and deliberately re-pinned (SPEC §3.2).
- **The codec-law line** is the line a protocol id would need for a compiler
  release that changes the bytes a schema produces to be visible in it
  ([#463](https://github.com/mas-bandwidth/schema/issues/463)).
- **3.0.0** is the release at which the table wire's promises are made; before
  it that wire is pre-release ([VERSIONING.md](VERSIONING.md)).
  [#366](https://github.com/mas-bandwidth/schema/issues/366) is its gate,
  complete feature parity across all nine languages, each at its language's
  performance floor.

### Schema's two wires, and how the column is scored

- **Declarations and the compiler** are nine languages deep, and a row is ✅
  when CI holds all nine.
- **The packet wire** is ✅ when all nine carry the construct.
- **The table wire** is scored against the repository's own registers:
  [PORTING.md](PORTING.md), the techniques register, which carries one cited
  cell per language and an issue behind every gap, and
  [the conformance contract](../test/conformance/README.md), which says which
  surfaces and which cases each port answers. The fixed class is carried by
  all nine on the wire, text, cook-open, block-read and descriptor surfaces.
  The variable class, the message, wide and blob classes and the runtime
  cook's write side are the reference's alone today, and the block's builder
  is emitted by the C++ and C backends. **And the WIRE FORM itself now
  divides them**: the id-table wire (SPEC-TABLES §3) is written by the C++
  reference and the tool, the eight ports still write its previous form, and
  their conformance legs are dormant until they carry it
  ([#511](https://github.com/mas-bandwidth/schema/issues/511) to
  [#518](https://github.com/mas-bandwidth/schema/issues/518), PORTING M20). So
  a table-wire row is ✅ only where
  the register shows all nine, and 🔶 otherwise with the scope named: which
  class, which languages, which issue.
- **The eight ports answer ABSENT on twenty-eight wire cases and twenty-six
  text ones**, and that matrix is the completion tracker
  ([the conformance contract](../test/conformance/README.md),
  [#349](https://github.com/mas-bandwidth/schema/issues/349)). A reader who
  wants the per-language state today reads [PORTING.md](PORTING.md) and that
  matrix instead of this page.
- **Where a feature is wire-specific**, meaning schema has it on one wire and
  not the other, the footnote says which wire the row is answered from and the
  mark is judged on that wire's backends.

Editor support and big-endian hosts are not rows, because neither could be
sourced across five columns.

## The matrix

### Declarations and the schema language

| feature | schema | Protocol Buffers | FlatBuffers | Cap'n Proto | Avro |
|---|---|---|---|---|---|
| Numeric expressiveness beyond int32/int64/float/double: declared `min`/`max` bounds, sub-32-bit and 128-bit integers, fixed point | ✅ [s1] | ❌ [2] | 🔶 [3] | 🔶 [4] | 🔶 [5] |
| Compile-time constants usable inside the schema | ✅ [s2] | ❌ [6] | ❌ [6] | ✅ [7] | ❌ [8] |
| Nested value type with no per-instance framing | ✅ [s3] | ❌ [9] | ✅ [10] | ✅ [11] | ✅ [12] |
| Bit-flag sets as a declaration | ✅ [s4] | ❌ [13] | ✅ [14] | ❌ [15] | ❌ [8] |
| Bounded arrays, strings and bytes: a declared capacity in the type | ✅ [s5] | ❌ [16] | 🔶 [17] | ❌ [18] | 🔶 [19] |
| Enum-keyed arrays (`[E]T`, one slot per variant, by name) | ✅ [s6] | ❌ [20] | ❌ [21] | ❌ [15] | ❌ [8] |
| Maps | 🔶 [s7] | ✅ [23] | 🔶 [24] | ❌ [25] | 🔶 [26] |
| Shared subtrees: one node referenced by many parents, written once | 🔶 [s8] | ❌ [27] | 🔶 [28] | ❌ [29] | ❌ [30] |
| Union arms may be any field type, not only a named record | 🔶 [s9b] | 🔶 [32] | 🔶 [33] | ✅ [34] | ✅ [35] |
| Defaults for strings, bytes and composites | 🔶 [s9] | 🔶 [36] | ❌ [37] | ✅ [38] | ✅ [39] |
| Explicit presence on a scalar (set vs unset, distinct from the default) | ✅ [s10] | ✅ [40] | 🔶 [41] | ❌ [42] | 🔶 [43] |
| Required fields: a read fails if the field is absent | ❌ [s11] | 🔶 [45] | 🔶 [46] | ❌ [47] | ✅ [48] |
| Dynamic typing: generics, a type-erased `Any`, or schema-less values | ❌ [s12] | ✅ [50] | 🔶 [51] | ✅ [52] | ❌ [53] |
| User-extensible annotations or custom options | 🔶 [s13] | ✅ [55] | ✅ [56] | ✅ [57] | 🔶 [58] |

### The wire

| feature | schema | Protocol Buffers | FlatBuffers | Cap'n Proto | Avro |
|---|---|---|---|---|---|
| Self-describing: the bytes alone are enough to walk the data | 🔶 [s14] | 🔶 [59] | ❌ [60] | ❌ [61] | ✅ [62] |
| Values compacted below their declared storage width | ✅ [s15] | ✅ [64] | ❌ [65] | 🔶 [66] | ✅ [67] |
| Scalars aligned inside the buffer | ✅ [s16] | ❌ [69] | ✅ [70] | ✅ [71] | ❌ [72] |
| Data above 2 GiB | 🔶 [s17] | ❌ [74] | 🔶 [75] | 🔶 [76] | ✅ [77] |
| Byte-identical output: one input, one schema, one byte sequence, across implementations | 🔶 [s18] | ❌ [79] | ❌ [80] | 🔶 [81] | 🔶 [82] |

### Evolution

| feature | schema | Protocol Buffers | FlatBuffers | Cap'n Proto | Avro |
|---|---|---|---|---|---|
| Add, remove or reorder a field freely | ✅ [s19] | ✅ [83] | 🔶 [84] | 🔶 [85] | 🔶 [86] |
| Rename a field without orphaning stored data | ✅ [s20] | ✅ [88] | ✅ [89] | ✅ [90] | 🔶 [91] |
| Change a field's type with no silent misdecode | 🔶 [s21] | ❌ [92] | ❌ [93] | ❌ [94] | ✅ [95] |
| Change a declared default without reinterpreting stored bytes | 🔶 [s22] | 🔶 [97] | ❌ [98] | ❌ [94] | ✅ [99] |
| Unknown fields preserved through a read-and-rewrite | ❌ [s23] | ✅ [101] | 🔶 [102] | 🔶 [100] | ❌ [103] |
| A per-event read report rather than pass/fail | ✅ [s24] | ❌ [104] | ❌ [105] | ❌ [106] | ❌ [107] |
| An unknown enum value has a defined, non-fatal landing | ✅ [s25] | 🔶 [108] | 🔶 [109] | 🔶 [111] | 🔶 [110] |

### Runtime forms, allocation, performance

| feature | schema | Protocol Buffers | FlatBuffers | Cap'n Proto | Avro |
|---|---|---|---|---|---|
| Read one field without parsing the whole buffer | ✅ [s26] | ❌ [112] | ✅ [113] | ✅ [114] | ❌ [115] |
| No allocation on read | 🔶 [s27] | ❌ [116] | 🔶 [117] | ✅ [118] | ❌ [115] |
| Arena or single-buffer allocation on write, never per node | 🔶 [s28] | 🔶 [119] | ✅ [120] | ✅ [121] | ? |
| Exact serialized size known before writing | ✅ [s29] | ✅ [122] | ❌ [123] | ? | ? |
| Mutate a serialized buffer in place | ❌ [s30] | ❌ [125] | 🔶 [126] | ❌ [127] | ? |
| Fixed-stride rows in one contiguous image, written by one language and read by another | 🔶 [s31] | ❌ [128] | ✅ [134] | ✅ [138] | ❌ [115] |
| Every field offset and every array pitch asserted in generated code on both sides, refusing a producer that disagrees | 🔶 [s31b] | ❌ [128] | 🔶 [129] | ❌ [130] | ❌ [115] |
| Generated code with no library runtime to link | 🔶 [s32] | ❌ [131] | 🔶 [132] | ❌ [133] | ? |

### Validation of untrusted data

| feature | schema | Protocol Buffers | FlatBuffers | Cap'n Proto | Avro |
|---|---|---|---|---|---|
| Untrusted bytes are safe with no separate verification pass | ✅ [s33] | ✅ [135] | ❌ [136] | ✅ [137] | ? |
| Value ranges enforced from the schema | ✅ [s1] | ❌ [2] | ❌ [3] | ❌ [4] | ❌ [5] |
| Depth and resource bounds against a hostile message | 🔶 [s34] | 🔶 [139] | 🔶 [140] | ✅ [141] | ? |
| One cross-language conformance corpus that includes hostile cases | ✅ [s35] | ? [142] | ? | ? | ? |

### Reflection, text, tooling

| feature | schema | Protocol Buffers | FlatBuffers | Cap'n Proto | Avro |
|---|---|---|---|---|---|
| Runtime reflection with no schema file present at runtime | 🔶 [s36] | ✅ [144] | 🔶 [145] | ✅ [146] | ✅ [147] |
| JSON or text form, in and out | 🔶 [s37] | 🔶 [149] | 🔶 [150] | ✅ [151] | ✅ [152] |
| Doc comments carried into generated code | ❌ [s38] | 🔶 [154] | ✅ [155] | ? | 🔶 [156] |
| Official language implementations at feature parity | 🔶 [s39] | 🔶 [158] | ❌ [159] | ❌ [160] | 🔶 [161] |

### Versioning

| feature | schema | Protocol Buffers | FlatBuffers | Cap'n Proto | Avro |
|---|---|---|---|---|---|
| Rename an enum variant, union arm or named type without orphaning stored data | ✅ [s40] | ✅ [88] | ✅ [89] | 🔶 [163] | 🔶 [91] |
| A retired name or number cannot be silently reused | ❌ [s41] | ✅ [165] | 🔶 [166] | 🔶 [167] | ❌ [168] |
| Defined widening or promotion of a field's type on read | ❌ [s42] | 🔶 [170] | 🔶 [93] | 🔶 [171] | ✅ [172] |
| The writer's schema is recoverable at read time | ❌ [s43] | 🔶 [174] | 🔶 [175] | ❌ [61] | ✅ [176] |
| A revision marker in the bytes says which format version wrote them | 🔶 [s44] | ❌ [178] | 🔶 [179] | ❌ [180] | ✅ [181] |
| A first-party compile-time gate on breaking edits | 🔶 [s45] | ❌ [183] | ✅ [184] | ❌ [185] | ? |
| Wire bytes promised stable across releases of the compiler | 🔶 [s46] | ❌ [79] | ? | 🔶 [187] | 🔶 [82] |

## Where they are ahead

- **Renames beyond fields.** Protocol Buffers and FlatBuffers rename a variant
  or a named type freely because their wires carry numbers instead of names,
  and Cap'n Proto does where the type pins an explicit id; schema keeps the
  data through every one of those renames, but asks the author for one
  attribute, `was`, because its wire carries the name's hash [s40].
- **Retired names.** Protocol Buffers' `reserved` is the right mechanism and
  schema lacks it, so a removed name can be re-added years later and decode
  old bytes under a new meaning [s41].
- **Default changes.** Avro writes every field of the writer's schema, so a
  changed default cannot reinterpret stored bytes, where schema elides a field
  equal to its default and answers the hazard with a guard instead of a
  guarantee [s22].
- **Unknown fields through a rewrite.** Protocol Buffers keeps them without
  being asked; schema drops them BY DEFAULT and counts what it dropped, under
  the never-clobber rule [s23]. The opt-in that answers the case is specified
  and unbuilt: a caller hands `LoadRetain` a bounded side buffer it declares
  and owns, and `SaveRetain` writes the unknown FIELDS back into the body they
  came from — the `unknown` class alone, never a variant, an arm, a keyed slot
  or a node record, each gap counted `retain_lost` (SPEC-TABLES §6.6,
  [#525](https://github.com/mas-bandwidth/schema/issues/525)).
- **Promotion.** Avro promotes `int` to `long` on read, in every direction its
  resolution rules allow. Schema's own widening rule runs one way: an integer
  kind into a wider one of the same signedness, and `f32` into `f64`, decoding
  exactly and counting `widened` in the C++ reference and the tool, with every
  other kind pair reading the default and counting a mismatch [s42].
- **Doc comments.** FlatBuffers and Avro both carry documentation in the
  schema, Protocol Buffers carries it in the descriptor, and Cap'n Proto's own
  pages settle it neither way; schema's `///` block is specified down to its
  refusals and its descriptor column, and no backend emits it [s38].
- **Ecosystems.** Protocol Buffers has ten first-party languages and
  forty-plus third-party ones, FlatBuffers thirteen of uneven coverage, and
  Avro six published SDKs; schema has nine [s39].

## Where schema is ahead

- **Bounds are bits.** `health int32 | min = 0, max = 1000` is sized on the
  packet wire from its range and not from `int32`, and the same gameplay
  packet measured against Cap'n Proto, Protocol Buffers and FlatBuffers is
  [COMPARISON.md](COMPARISON.md), field by field. None of the four can infer a
  range it was never told.
- **One language for all five kinds of data.** Packets, backend messages,
  saves, render blocks and cooked assets from one declaration.
- **The read is the report.** Every load counts what it did not know, what had
  the wrong kind, what it clamped and whether the bytes were malformed, and
  never fails on data from another build. The others return a boolean or
  throw.
- **The silent class is enumerated and refused.** The edits no reader can
  report, a changed default, a moved flags variant, a referent respelled as
  something the kinds cannot tell apart, a moved fixed-point scale, are
  refused at compile time by a committed baseline with a reasoned, dated
  history. The nearest equivalents, `flatc --conform` and `buf breaking`,
  check structure, not meaning.
- **Shared subtrees on the wire.** A node written once and referenced many
  times, which none of the four does by declaration. Cap'n Proto forbids it to
  bound traversal; schema's flat node table materializes each node once and
  `LoadMeasure` prices the region before allocation, with the bound being
  stated and tested as such
  ([#466](https://github.com/mas-bandwidth/schema/issues/466)).
- **Per-field tolerance.** Avro fails the whole read on a mismatch; schema
  counts the field and reads the rest. That is the right answer for a save
  game and the wrong one for a warehouse.

## Judged not needed, with the reason

| feature | who has it | why schema declines |
|---|---|---|
| Required fields | FlatBuffers, Avro in effect, Protocol Buffers as a legacy carry-over | Every field optional with a declared default is the ground rule. Protocol Buffers removed `required` and says why; schema never had it. |
| `Any`, generics, schema-less values | Protocol Buffers, Cap'n Proto, FlexBuffers | The typed bag is a union whose arms are tables. A type-erased value is a type the compiler cannot check. |
| Custom annotations on fields | Protocol Buffers, FlatBuffers, Cap'n Proto, Avro as tolerated metadata | The field-attribute vocabulary is closed on purpose ([COMPARISON-TABLES.md](COMPARISON-TABLES.md), "Extensions and custom options"). The open namespace is the type tag [s13]. |
| Unknown fields preserved through a rewrite | Protocol Buffers | [COMPARISON-TABLES.md](COMPARISON-TABLES.md), "Unknown fields preserved on write". The old file kept whole is the preservation, under the never-clobber rule. |
| A widening path on read | Avro, Protocol Buffers' pairs | Protocol Buffers' own list of compatible pairs silently misdecodes everything outside it [92]. A changed kind reads as the default and is counted; the pattern is a new field beside the old one and a load shim. |
| The writer's schema traveling with the data | Avro | One schema per build, identified by the protocol id and the build version, neither of which rides in a file. A tolerant wire that skips by kind needs no schema to walk, and a file that must say what wrote it does so with the root-table `format` field [VERSIONING.md](VERSIONING.md) defines. |
| A canonical form of the data | Cap'n Proto | A load followed by a save through the reader's own schema is the canonical form: elision, order and duplicates are all resolved by it. Byte identity across schema's own writers is held by goldens. |
| Mutating a serialized buffer in place | FlatBuffers | [COMPARISON-TABLES.md](COMPARISON-TABLES.md), "In-place mutation of a serialized buffer". |

## Footnotes

Schema cells cite a specification section, one of the repository's registers
([PORTING.md](PORTING.md), [the conformance contract](../test/conformance/README.md))
or an issue. **FlatBuffers and Protocol Buffers cells cite the row that
carries the evidence in [COMPARISON-TABLES.md](COMPARISON-TABLES.md)** instead
of restating it; a footnote stays here only where that page does not carry the
fact, and it says so. Cap'n Proto and Avro are not on that page, so their
cells cite the project's own documentation by the short names in the source
list at the end, with the section that establishes the claim.

**Schema**

- s1. `| min, max` is part of the type: the packet wire sizes the field's bits from the bound and refuses a value outside it, and the table wire clamps and counts `clamped` on load (SPEC §4.3, SPEC-TABLES §4). `fixed(I,F)`, `ufixed(I,F)`, `int128` and `uint128` are declared in all nine targets and ride the packet wire in all nine, on the cross-language wire gate like any other unit (`examples128/Ludicrous.schema`, SPEC §4.3). ✅ is the declaration and the packet wire, the two the row asks about. The same scalars have table-wire kinds 18 to 29 (SPEC-TABLES §3), where the three wide-class cases are answered by the reference and the eight ports answer ABSENT (SPEC-TABLES §15).
- s2. `const`, folded at compile time and usable in bounds, defaults and capacities (SPEC §4.2, the `Const` production and "Constants and enums are exported").
- s3. **The packet wire answers this row.** A `type` nested inside a `type` is inline storage with no framing at all (SPEC §4.3), carried by all nine. On the table wire a nested table is kind `13`, a canonical LEB128 length then the body, which is the framing Protocol Buffers is marked ❌ [9] for.
- s4. `flags` declarations, one bit per variant (SPEC §4.2).
- s5. `[N]T`, `[..N]T`, `string(N)`, `bytes(N)`: the capacity is in the type, and the packet wire sizes the count from it (SPEC §4.3).
- s6. `[E]T`, a slot per variant addressed by name, under its own wire kind `16` (SPEC-TABLES §2.4, §3.2). The declaration and the keyed kind are fixed class and carried by all nine: the corpus's keyed instances are answered by every registered leg. The accessor's both-ends refusal is untested in Java, Dart and Elixir and unchecked past `Max` in C (PORTING M5, [#407](https://github.com/mas-bandwidth/schema/issues/407), [#377](https://github.com/mas-bandwidth/schema/issues/377)).
- s7. `map[K]V` in a table body: a lookup the runtime provides over entries the wire carries as a sorted array of one generated `{ key, value }` table, spending no wire kind, with `Find` a binary search in place over a locked region, a loaded one or an opened cook (SPEC-TABLES §2.8). Keys are bounded strings and the integer kinds; every other key is refused by name, an enum key naming `[E]T`. 🔶 because a map makes its holder VARIABLE, so it is the reference's and the tool's alone: the eight ports refuse a variable unit's wire by name ([#380](https://github.com/mas-bandwidth/schema/issues/380), [#349](https://github.com/mas-bandwidth/schema/issues/349), SPEC-TABLES §11).
- s8. A pointer field `*T` names a node once in a flat node table and every reference is an index (SPEC-TABLES §3.1). The variable class has one wire implementation: PORTING M6 is ✅ for cpp, ❌ [#408](https://github.com/mas-bandwidth/schema/issues/408) for C, whose earlier nested form has a depth cap and no identity map, and ❌ [#349](https://github.com/mas-bandwidth/schema/issues/349) in the other seven columns; the eight ports answer ABSENT on the corpus's four pointered instances. The amplification bound on an untrusted read is [#466](https://github.com/mas-bandwidth/schema/issues/466).
- s9. Defaults for `string(N)`, `bytes(N)` and `flags` fields are built ([#396](https://github.com/mas-bandwidth/schema/issues/396)): SPEC §4.2's `Default` production admits a quoted string and a brace list of flags variant names, a field at its declared default elides on the table wire and an absent field reads as it (SPEC-TABLES §4), and the baseline refuses a change to one (SPEC-TABLES §18.2). 🔶 because the C++ reference and the tool carry the three and every other backend refuses a unit that declares one, naming the follow-on, and because a composite default is the adopt-later row of #396.
- s9b. An arm IS a field line, so an arm's type is any type a field's is — a scalar with its bounds, a compressed float, a string, a bounded array, an enum, a `flags` mask, a declared `type`, a `table` inside a table closure, a pointer, another union — and an arm may carry no payload at all, which rides under kind `32` (SPEC §4.8, SPEC-TABLES §2.6, §3). What an arm may not take is a default, a `?`, a `json`, an `[E]T`, an `if` guard, a `map` or an unbounded `[]T`, each refused by name. 🔶 because it is the reference's and the tool's: the eight ports answer ABSENT on the message-class cases ([#392](https://github.com/mas-bandwidth/schema/issues/392), SPEC-TABLES §15).
- s10. **The table wire answers this row**, because the packet wire has no presence at all. `?T` is the value plus a generated presence bool, so the holder stays fixed size (SPEC-TABLES §2.3). SPEC §4.2's grammar admits `?` in table bodies only, and a `type` body refuses one by name. On the table backends `?T` is fixed class and all nine carry it (`chain_optional` and `chain_optional_empty` in the conformance corpus); `?[N]T` is one of the message-class cases the reference answers alone (PORTING M16, [#392](https://github.com/mas-bandwidth/schema/issues/392)).
- s11. Declined: every field optional with a declared default (SPEC-TABLES §4).
- s12. Declined. SPEC §4.2 declares no generic parameter and no type-erased value, and the adoption question is closed on [#396](https://github.com/mas-bandwidth/schema/issues/396); the typed bag is a union whose arms are tables (SPEC-TABLES §2.6).
- s13. The FIELD-attribute vocabulary is closed and an unknown attribute is a compile error (SPEC §4.2). The TYPE TAG is the open namespace beside it: "one user-chosen identifier, in its own namespace", any identifier legal there, parsed, carried through the IR and emitted as an annotation on the generated type, with the unit registry's `ViewType` listing a declaration's tags (SPEC §4.2, SPEC-TABLES §8.3). 🔶 because a tag is inert in v1 and changes zero generated code beyond the annotation, where the three ✅ columns let an annotation reach a runtime API. It is more than Avro's 🔶 [58], which requires only that an implementation tolerate the attribute.
- s14. The packet wire is not self-describing, by decision, a stated non-goal with all knowledge in the generated code on both ends (SPEC §1). On the table wire kinds and lengths ride, so any reader skips anything it does not know (SPEC-TABLES §3); a name rides as a 64-bit hash in a trailing id table and a field header carries a REFERENCE to it rather than text (§3, §5), so the bytes cannot be rendered with names without the schema.
- s15. **The packet wire answers this row.** It sizes each field from its bounds (SPEC §4.3) in all nine targets. The table wire deliberately does not compact: a scalar rides at its storage width and nothing is aligned or padded (SPEC-TABLES §3), the property FlatBuffers is marked ❌ [65] for.
- s16. **The cook and the block answer this row**, and they are the aligned forms (SPEC-TABLES §7.2, §19.3). The tolerant wire has no alignment and no padding (§3). All nine open both: the harness's `cook` and `block` surfaces are answered by every registered leg, and the cook's `Open` is PORTING M8, ✅ in every column. The block's row reach is M7, where Elixir allocates per row ([#409](https://github.com/mas-bandwidth/schema/issues/409)).
- s17. Table wire. Eight-byte region references and 64-bit cook part lengths, with no aggregate ceiling in the format (SPEC-TABLES §6.3, §7.1). 🔶 for two reasons, one in the format and one in the implementations. The tolerant wire's own lengths, counts, indices and references are canonical LEB128 with 64 bits of capability, so no body, count or index has a ceiling below `2^64 − 1` (§3) — but that wire is the reference's and the tool's, and the eight ports still write the previous form ([#511](https://github.com/mas-bandwidth/schema/issues/511) to [#518](https://github.com/mas-bandwidth/schema/issues/518)). And the two accelerators are read out of a `byte[]` in the managed ports, which stops at 2 GiB: SPEC-TABLES' ladder states it as the one Java divergence that costs a stated requirement, C# meets the same `int` ceiling on its span overload and answers it with the pointer form beside it, and the foreign-memory overload is a named follow-on (§15).
- s18. One standard, one corpus, and byte identity is proven where nine backends produce bytes. The packet wire is bit-for-bit compatible across all nine runtimes, pinned in CI with shared golden bytes, and a compiler change that breaks a wire golden is stop-the-line (SPEC §1, §3.2, §7.2). On the table wire the `wire` surface byte-compares every registered leg's `Save` against one golden over the FIXED class, and `measure == save at exact capacity` is a hard invariant held by a mandatory battery (SPEC-TABLES §9). 🔶 because the variable, message, wide and blob classes have one writer: the eight ports produce no bytes for those cases and answer ABSENT per case, so no cell claims agreement it did not test.
- s19. Table wire. Name identity: add anywhere, remove, reorder, each reported by the read instead of refused (SPEC-TABLES §4, §5). Carried by all nine, on the `wire` and `report` surfaces over the fixed class. Whether a retired name can be silently reused is [s41], not this row.
- s20. `| was = "old"` keeps the wire id, and a bare rename is a removal and an addition the compiler cannot see. The committed baseline warns on that pair: `internal/baseline/diff.go`'s `renamePair` reports a wire id removed and a wire id added in one edit and names the `was` and `json =` spellings that keep the data ([#444](https://github.com/mas-bandwidth/schema/issues/444), SPEC-TABLES §5, §18.2). It warns and never refuses, because two independent edits in one commit are legitimate. `was` covers every name the table wire carries: a table's own fields, a table declaration, enum variants, union arms and the fields of a `type` a table reaches (SPEC-TABLES §5).
- s21. A changed kind reads as the default and is counted `kind_mismatch` (SPEC-TABLES §4), in all nine over the fixed class. The respellings a shared kind once left open are closed: an enum has kind `30` and a pointer index kind `17`, so an enum-typed field respelled as its raw `uint16`, and a `*T` respelled as a `uint32`, are ordinary counted mismatches in both directions (§3, §3.1, §4.1). It is still not the whole story, and SPEC-TABLES §4.1 says so: a field's REFERENT dropped or swapped for a twin that cannot stand in for it, and a `fixed` field's `F` moved under the same storage width, each keep the kind and change what the bytes mean with no counter to fire. Both are guarded only by the committed baseline (§18).
- s22. Silent on the wire, refused at compile time by the committed baseline (SPEC-TABLES §4.1, §18.2). 🔶 because the baseline is opt-in by design, "no file, no check" (§18.1). A unit that declares a table and holds no baseline draws a one-line stderr notice from `schema check` naming what is unguarded and the command that commits one ([#445](https://github.com/mas-bandwidth/schema/issues/445), §18.1). The notice never touches the exit code, so the limitation stands.
- s23. Decided and not built. The DEFAULT is a drop, by decision, with the read report counting what a rewrite would lose under the never-clobber rule ([VERSIONING.md](VERSIONING.md)). Retain-unknown is the opt-in beside it, a REGION round trip whose buffer the caller sizes and owns, covering unknown FIELDS and no other class and no other counter, with `retained` and `retain_lost` on the same report struct (SPEC-TABLES §6.6). The C++ reference carries it and no port does ([#525](https://github.com/mas-bandwidth/schema/issues/525)).
- s24. Six counters on every load (SPEC-TABLES §4) — `unknown`, `kind_mismatch`, `widened`, `clamped`, `duplicate` and the `malformed` flag — plus `retained` and `retain_lost`, which ride the same struct and stay zero until a caller opts into retention (§6.6). The `report` surface is answered by the C++ leg, and by no port yet: every port writes the wire's previous form and the corpus is pinned in the id-table form, so each says absent rather than failing ([#511](https://github.com/mas-bandwidth/schema/issues/511) to [#518](https://github.com/mas-bandwidth/schema/issues/518)). `widened` is counted by the C++ reference and the tool on the same terms. Retention is carried by the C++ reference and by no port ([#525](https://github.com/mas-bandwidth/schema/issues/525)). The harness's per-leg negative control sabotages the emitter's walk and requires the matrix to localize it, and it is carried by seven of the nine, with cpp and rust at ❌ [#417](https://github.com/mas-bandwidth/schema/issues/417) (PORTING I11).
- s25. An unknown variant reads `None` and is counted `unknown` (SPEC-TABLES §4, §5), in all nine on the `report` surface. The witness in the corpus is the evolution seam `v2_cfg_as_v1`: V2 adds `Silver` to `Grade` and `Omega` and `Sigma` to `Slot`, and V1 reading V2's `Cfg` counts 5 unknown, the number every registered leg must produce (`testdata/conformance/tables/MANIFEST.txt`, `reports.txt`).
- s26. The cook: `Open` is a header match and a pointer, with nothing per node (SPEC-TABLES §7). Carried by all nine, PORTING M8 being ✅ in every column, and a gigabyte cook opens in the same time as a small one. The cook's write side is the reference's and the tool's; every other language's writer is a named follow-on (§7.6, §15).
- s27. The read path fills caller-owned storage and the reader is a cursor over the caller's buffer, never a sub-view (SPEC-TABLES §6.5, PORTING M1). 🔶 for two reasons. Elixir cannot make the claim at all, because a decoded BEAM term is an allocation and no buffer is caller-owned, so that leg pins the per-case count instead (M1's Elixir cell). And a union inside a table allocates per arm in a backend whose language has no native union, the one carve-out §6.5 states.
- s28. The fixed class writes into the caller's buffer and allocates nothing, in any backend. The variable class builds through an arena in bulk, thread-local, never per node (SPEC-TABLES §6.4, §9), and that arena is the reference's (PORTING M6); the block takes a caller-provided allocator once at build and never on the fill path (§19.1). The union carve-out (§6.5) is the one per-node allocation, and it belongs to the language and not to the format.
- s29. **The table wire answers this row.** `Measure` computes a value's exact encoded size writing nothing, and `measure == save at exact capacity` is a hard invariant held by a mandatory battery across the corpus (SPEC-TABLES §9); all nine backends generate it. The packet wire has no exact measure and says so in terms, "There is no generated measure function": `MaxBits` and `MaxBytes` are conservative worst-case constants for sizing a buffer, and `Write` returns the actual size afterwards (SPEC §6.1).
- s30. Declined: a cook is immutable and regenerated by the cache, and the block form is the mutable answer (SPEC-TABLES §7, §19).
- s31. The block form: one contiguous extent of rows at a compiler-computed stride, every reference block-relative, so it relocates by `memcpy` with no fix-up (SPEC-TABLES §19, §19.1). Its read half is generated by all nine backends, and PORTING M7 carries eight of them with Elixir's per-row allocation at [#409](https://github.com/mas-bandwidth/schema/issues/409).
- s31b. Every cooked record and every block projection carries a compile-time assertion of size, alignment and every field offset against the numbers the compiler computed, each array's pitch constant included: two independent derivations held against each other, so a producer that disagrees is refused before a row is read (SPEC-TABLES §19.3). Seven backends carry it; Dart and Elixir have no second layout model to check against and hold the contract with the build version instead (PORTING M11).
- s32. Six of the nine targets link a small `serialize` runtime for the packet wire; Dart, Java and Elixir generate self-contained output ([VERSIONING.md](VERSIONING.md)). The C++ tables dialect takes no STL and routes every C-library call through a hook the program can define (SPEC-TABLES §13.9).
- s33. The tolerant read is the validator: every length bounds-checked against its body, every count against the body, ranges clamped, a bad sub-table stopping only itself, and `LoadMeasure` letting the caller refuse a region size before anything is allocated (SPEC-TABLES §4, §6.5). SPEC-TABLES §4.2 is the gate on that claim, "The read is the verifier": every pinned wire in the corpus, mutated by enumerated passes plus a seeded random pass, is read by the leg and by the compiler's own engine, an independent third reading of §3 no backend was written from, and four requirements per mutant must hold. C++ is proven, at 62,179 enumerated plus 3,000,000 random mutants over 63 seeds with 0 divergences, plain and under ASan/UBSan (`test/tables/wire_fuzz_main.cpp`, PORTING I15). The per-port carry is I15's ❌ cells.
- s34. By-value depth is fixed by the schema and the pointer graph is flat, so a long chain is not a recursion (SPEC-TABLES §3.1). Until [#466](https://github.com/mas-bandwidth/schema/issues/466) states and tests the amplification bound, the bound is the caller's own refusal of `LoadMeasure`'s answer (§6.5) and not a number the format states.
- s35. One corpus, every registered leg, hostile rows included: two forgery batteries, the cook battery's 111 rows and the `json-hostile` trees, with seven negative controls behind the harness (`test/conformance`). A leg that lacks a construct answers ABSENT per case instead of passing by omission.
- s36. Static descriptors in every table's generated header, name, kind, id, offset, bounds, guards and nesting, with no schema file present and no RTTI (SPEC-TABLES §8.1), carried by all nine (PORTING M13; C# holds them in a cache and not as constants, [#411](https://github.com/mas-bandwidth/schema/issues/411)). The type view and the unit registry (§8.2, §8.3) are C++ and C# only, and every other backend emits no view file (§15).
- s37. JSON in and out by one generic walk over the descriptors, `| json = "key"`, the read report on the way in, `&node` for a shared node (SPEC-TABLES §16). The walk itself is carried by all nine (PORTING M9), and each backend's is compared unit by unit. 🔶 because the text surfaces are where the eight ports answer ABSENT, on `json-read` and `json-write` alike: the message, wide, blob and variable classes have no port text form ([the conformance contract](../test/conformance/README.md), SPEC-TABLES §15).
- s38. Decided and not built. The design is the OPT-IN `///` block: a contiguous run of `///` lines binding to the declaration, field, variant or arm below it, carried verbatim into the `doc` descriptor column beside a `tags` column and into ordinary line comments in the generated code, with `| doc = "..."` refused by name so one text has one spelling (SPEC §4.1, §4.11, SPEC-TABLES §8.1). A plain `//` above the same item stays a comment and reaches nothing, which is what keeps a tree of working notes out of every game's binary. No backend emits either column ([#523](https://github.com/mas-bandwidth/schema/issues/523)).
- s39. Nine languages byte-identical on the packet wire in CI (SPEC §1). Tables are carried under [#366](https://github.com/mas-bandwidth/schema/issues/366).
- s40. A table declaration takes `was` ([#396](https://github.com/mas-bandwidth/schema/issues/396)): the node type id every stored record carries is the hash of the first name, so a renamed pointer target still reads, and the rename moves neither id (SPEC-TABLES §5). An enum variant and a union arm take it on their own line ([#442](https://github.com/mas-bandwidth/schema/issues/442)), and so does a field of a `type` a table reaches ([#478](https://github.com/mas-bandwidth/schema/issues/478)): the id is the old name's hash in every case, and a `flags` variant, whose identity is its bit, needs none.
- s41. Decided and not built. The retired-names ledger in the baseline is [#441](https://github.com/mas-bandwidth/schema/issues/441), before 3.0.0; nothing today marks a removed name retired, so it can be re-added and decode old bytes under a new meaning.
- s42. Built in the C++ reference and the tool, owed by the eight ports with the wire form they lack. An integer kind read into a WIDER integer kind of the same signedness, and `f32` read into `f64`, decode EXACTLY and count `widened`; the signed ladder is kinds `2`, `3`, `4`, `5`, `18` and the unsigned one `6`, `7`, `8`, `9`, `19`, and every other pair stays `kind_mismatch` because each is a value the wider kind would accept and the schema does not mean (SPEC-TABLES §4). The path runs FORWARD only — an old build meeting the wider kind narrows and reads its default — so the baseline refuses the edit like any kind change. The reference counts it at a field, an arm, an element kind and a map key, and the wire fuzzer plants a widening and its reverse at each.
- s43. Declined, with the reason in the table above.
- s44. The cook's and the block's prologues carry the build version and refuse a file another build wrote (SPEC-TABLES §7.1, §19.1, §20), and all nine read them (PORTING M8; the harness's `block` surface). The tolerant wire's own marker is the FORM BYTE, the first byte of a file, `1` for a file and `2` for a message, read before the trailer and before any body so a form a reader does not know is a REFUSAL by name and never damage (§3, §3.3). 🔶 because it is the reference's and the tool's and the eight ports still write the previous form ([#511](https://github.com/mas-bandwidth/schema/issues/511) to [#518](https://github.com/mas-bandwidth/schema/issues/518)), and because form `2` is specified and unwritten ([#523](https://github.com/mas-bandwidth/schema/issues/523)). It is also the answer to FlatBuffers' `file_identifier` [179]: the schema's own revision is the `format` field on the root table, the format's own revision is the form byte.
- s45. `tables.baseline` with `--update --reason` and a dated history (SPEC-TABLES §18), first-party where `buf breaking` is not. 🔶 for the same reason as [s22]: the file is opt-in and no file means no check (§18.1). The packet wire has no compile-time gate at all, the protocol id refusing at connect time, which is a runtime refusal (SPEC §3).
- s46. The promise is on [VERSIONING.md](VERSIONING.md), and SPEC §6.1 states it as a contract: for a given schema and target, equal post-quantization values produce identical bytes, deterministically, across compiler versions, held by the golden-wire gate across a schema's edits. Across compiler RELEASES it needs the codec-law line and an N-1 to N differential gate ([#463](https://github.com/mas-bandwidth/schema/issues/463)).

**Protocol Buffers and FlatBuffers**

2. COMPARISON-TABLES.md, Declarations and the schema language, "Scalar types", "Declared numeric bounds", "Fixed point".
3. COMPARISON-TABLES.md, Declarations and the schema language, "Scalar types", "Declared numeric bounds", "Fixed point": sub-32-bit yes, no 128-bit, no fixed point, no declared range.
6. COMPARISON-TABLES.md, Declarations and the schema language, "Constants" ("—" in both columns).
9. COMPARISON-TABLES.md, Declarations and the schema language, "Inline struct" ("— ; every message is length-delimited").
10. COMPARISON-TABLES.md, Declarations and the schema language, "Inline struct".
13. COMPARISON-TABLES.md, Declarations and the schema language, "Flags" ("—").
14. COMPARISON-TABLES.md, Declarations and the schema language, "Flags" (`enum (bit_flags)`).
16. COMPARISON-TABLES.md, Declarations and the schema language, "Arrays", "Strings", "Bytes".
17. COMPARISON-TABLES.md, Declarations and the schema language, "Arrays" (`[T:N]` in structs only).
20. COMPARISON-TABLES.md, Declarations and the schema language, "Maps"; and "One scene, three ways", "keyed by Slot" (enum keys refused).
21. COMPARISON-TABLES.md, Declarations and the schema language, "Enum-keyed arrays" ("—"); "Sorted lookup in a buffer" gives the idiom, a sorted vector looked up by VALUE.
23. COMPARISON-TABLES.md, Declarations and the schema language, "Maps".
24. COMPARISON-TABLES.md, Declarations and the schema language, "Maps"; "Sorted lookup in a buffer".
27. COMPARISON-TABLES.md, Declarations and the schema language, "Pointers, graphs, sharing" ("tree only; no sharing, no cycles").
28. COMPARISON-TABLES.md, Declarations and the schema language, "Pointers, graphs, sharing" (an `Offset` may be reused by the builder; cycles impossible).
32. COMPARISON-TABLES.md, Declarations and the schema language, "Union" (`oneof`). The arm restriction is not on that page: `oneof` admits any type except map fields and repeated fields (PB-proto3, Oneof), and that limitation holds this at 🔶.
33. COMPARISON-TABLES.md, Declarations and the schema language, "Union" (union of tables; structs and strings experimental; vectors of unions C++ only).
36. COMPARISON-TABLES.md, Declarations and the schema language, "Defaults" (proto3 has none; proto2 and editions do).
37. COMPARISON-TABLES.md, Declarations and the schema language, "Defaults" (scalar defaults); "Optional fields" (references null when absent).
40. COMPARISON-TABLES.md, Declarations and the schema language, "Optional fields" (explicit presence: proto2 all, proto3 `optional`, editions explicit by default).
41. COMPARISON-TABLES.md, Declarations and the schema language, "Optional fields" (optional scalars via `= null`). The per-language coverage is not on that page: FB-support marks optional scalars Yes in ten of the thirteen languages listed and blank for Python, PHP and Dart, and that table says of itself "NOTE: this table is a start, it needs to be extended", so every count taken from it is a floor and not a fact.
45. COMPARISON-TABLES.md, Declarations and the schema language, "Required" (removed; `LEGACY_REQUIRED` only). The construct exists on the record in two places, proto2's `required` and editions' `features.field_presence = LEGACY_REQUIRED`, "required for parsing and serialization" (PB-features), and the project's guidance is never to add one (PB-dos).
46. COMPARISON-TABLES.md, Declarations and the schema language, "Required" (`(required)`, verifier-checked). The check is the verifier's, and the verifier does not ship in every language [136].
50. COMPARISON-TABLES.md, Declarations and the schema language, "Schema-less data" (`Struct`, `Value`); "Well-known types" (`Any`).
51. COMPARISON-TABLES.md, Declarations and the schema language, "Schema-less data" (FlexBuffers). The coverage is not on that page: FB-support marks FlexBuffers Yes for C++, Java, Rust and Swift of the thirteen listed, under that table's own caveat [41].
55. COMPARISON-TABLES.md, Declarations and the schema language, "Attributes" (custom options with retention and targets).
56. COMPARISON-TABLES.md, Declarations and the schema language, "Attributes" (user attributes declarable, read via reflection).
59. COMPARISON-TABLES.md, The wire, "Self-describing" ("no; needs descriptors"). Field numbers and wire types do ride, so a parser skips what it does not know; names and declared types need the definition (PB-encoding, Message Structure).
60. Not on COMPARISON-TABLES.md, whose "Version identity" row covers `file_identifier` and not the format's own silence. FB-internals: the format "doesn't contain information for format identification and versioning, which is also by design".
64. COMPARISON-TABLES.md, The wire, "Varint compaction" (varints, zigzag).
65. COMPARISON-TABLES.md, The wire, "Varint compaction" ("—"); "Alignment" (every scalar aligned to its size), the property that buys in-place access.
69. COMPARISON-TABLES.md, The wire, "Alignment" ("none").
70. COMPARISON-TABLES.md, The wire, "Alignment" (aligned to size; `force_align`).
74. COMPARISON-TABLES.md, The wire, "Size ceilings" (2 GiB).
75. COMPARISON-TABLES.md, The wire, "Size ceilings" (32-bit offsets: 2 GiB; `vector64` for tail vectors, not in every port).
79. COMPARISON-TABLES.md, The wire, "Byte-identical output across implementations" ("don't assume serialization stability across builds").
80. COMPARISON-TABLES.md, The wire, "Byte-identical output across implementations" ("no cross-language claim"). The denial itself is not on that page: FB-internals states it outright, "This may mean two different implementations may produce different binaries given the same input values, and this is perfectly valid." That is the format declining the property on the record, not an inference from absence.
83. COMPARISON-TABLES.md, Evolution, "Add a field", "Remove a field", "Reorder": add anywhere with an unused number, remove and reserve, reorder free. All three of what the row asks are documented.
84. COMPARISON-TABLES.md, Evolution, "Add a field", "Remove a field", "Reorder". 🔶: `id` on every field makes source position free, but a new field goes at the end without it, and the project's own evolution guidance is never to remove a field, deprecating it in place instead.
85. Not on COMPARISON-TABLES.md; Cap'n Proto is not on that page. A new member's number must be larger than all previous members', so a field is added only at the top of the number space; members may be rearranged in source with their numbers preserved; and the evolution rules say nothing about removing a field at all, which is ❌ by this page's absence reading (CP-lang, Evolving Your Protocol).
88. COMPARISON-TABLES.md, Declarations and the schema language, "Rename" (free on the binary wire; reserve the old name for JSON).
89. COMPARISON-TABLES.md, Declarations and the schema language, "Rename" (free; names are not serialized).
92. COMPARISON-TABLES.md, Evolution, "Change a type" (a fixed list of compatible pairs; anything else misdecodes silently).
93. COMPARISON-TABLES.md, Evolution, "Change a type" (only at identical width, with careful handling).
97. COMPARISON-TABLES.md, Evolution, "Change a default" (proto3 has none; proto2 reader-side). The reason for 🔶 is not on that page: under explicit presence, proto2 and the default in every edition, "Any explicitly-set value is serialized onto the wire (even if it is the same as the default value)" (PB-features), so a changed default reinterprets only the fields a writer never set. Under implicit presence, proto3's default, a value equal to the default is elided and the hazard is whole. Avro keeps ✅ [99] because it writes every field of the writer's schema unconditionally, so no stored byte can be reinterpreted at all.
98. COMPARISON-TABLES.md, Evolution, "Change a default" ("don't": V1 data that did not write the value relied on generated code for it).
101. COMPARISON-TABLES.md, Evolution, "Unknown fields on read", "Unknown fields on rewrite" (retained and re-serialized).
102. COMPARISON-TABLES.md, Evolution, "Unknown fields on rewrite" (the buffer keeps them if forwarded whole; the object API's unpack-and-repack does not).
104. COMPARISON-TABLES.md, Evolution, "Read report" (success or failure, plus the unknown set).
105. COMPARISON-TABLES.md, Evolution, "Read report" (verifier pass or fail).
108. COMPARISON-TABLES.md, Evolution, "Enum evolution" (open enums keep the int; closed enums move it to unknown fields). The per-language divergence is on that page's same row: C#, Go, JSPB and Ruby treat every enum as open and Dart as closed, whatever the syntax says (PB-enum).
109. COMPARISON-TABLES.md, Evolution, "Enum evolution" (append or explicit values; the application handles unknowns itself).
112. COMPARISON-TABLES.md, Runtime forms, allocation, performance, "Point at the bytes" ("never").
113. COMPARISON-TABLES.md, Runtime forms, allocation, performance, "Point at the bytes" ("always").
116. COMPARISON-TABLES.md, Runtime forms, allocation, performance, "Allocation on read" (per message, string and repeated field; arenas mitigate).
117. COMPARISON-TABLES.md, Runtime forms, allocation, performance, "Allocation on read" (none in place; the object API allocates).
119. COMPARISON-TABLES.md, Runtime forms, allocation, performance, "Allocation on write". Not on that page: arenas are a C++ feature (PB-arenas), and that limitation holds this at 🔶.
120. COMPARISON-TABLES.md, Runtime forms, allocation, performance, "Allocation on write" (the builder grows a buffer; custom allocator). One growable buffer with a caller's allocator is the row's property, and nothing is allocated per node, the reading Cap'n Proto's segments also get [121].
122. COMPARISON-TABLES.md, Runtime forms, allocation, performance, "Exact size before writing" (`ByteSizeLong()`).
123. COMPARISON-TABLES.md, Runtime forms, allocation, performance, "Exact size before writing" (after `Finish`).
125. COMPARISON-TABLES.md, Runtime forms, allocation, performance, "Mutate in place" ("—").
126. COMPARISON-TABLES.md, Runtime forms, allocation, performance, "Mutate in place" (`--gen-mutable` scalars; reflection resizes). The coverage is not on that page: FB-support marks simple mutation Yes for C++, Java, C#, Go and Swift of the thirteen listed, under that table's own caveat [41].
128. COMPARISON-TABLES.md, Runtime forms, allocation, performance, "Cross-language rows at a pitch" ("—"); "Offline cook for a build" ("—").
129. COMPARISON-TABLES.md, Runtime forms, allocation, performance, "Cross-language rows at a pitch" (a vector of structs is inline, but the schema does not assert both sides' native layout). 🔶, and the half that is asserted is not on that page: a struct's layout is fixed "independent of the alignment rules of the underlying compiler", and "This layout is then enforced in the generated code" (FB-internals, Structs). The C++ generated struct is bracketed by `FLATBUFFERS_MANUALLY_ALIGNED_STRUCT(4)` and `FLATBUFFERS_STRUCT_END(Vec3, 12)`, which turns the compiler's padding off and asserts the struct's total SIZE. What schema asserts and FlatBuffers does not is every field's OFFSET and every array's PITCH, in both the producing and the consuming language's generated code, so the two sides refuse each other rather than agreeing only on a total [s31b].
131. COMPARISON-TABLES.md, Runtime forms, allocation, performance, "Generated code dependencies" (`libprotobuf`).
132. COMPARISON-TABLES.md, Runtime forms, allocation, performance, "Generated code dependencies". The generated header "relies on `flatbuffers/flatbuffers.h`, which should be in your include path", so the C++ core is an include and not a link; text and schema parsing "requires you to link a few more files into your program" (FB-cpp, Prerequisites; Text & schema parsing). Every other FlatBuffers language ships a runtime library, so the cell is 🔶.
134. Not on COMPARISON-TABLES.md, whose row states only the second half. Structs "are always stored inline in their parent (a struct, table, or vector)", and their layout is defined "independent of the alignment rules of the underlying compiler to guarantee a cross platform compatible layout" (FB-internals, Structs), so a vector of structs is a fixed-stride row image two languages compute identically. That is this row's property.
135. COMPARISON-TABLES.md, Validation of untrusted data, "Untrusted bytes on the tolerant wire" (the parser validates structure; recursion limit; UTF-8 verify).
136. COMPARISON-TABLES.md, Validation of untrusted data, "Untrusted bytes on the tolerant wire" (a separate `Verifier`, required before access, in C++, C, Swift and Rust). FB-rust is the fourth, which FB-support's matrix does not show: "The safe Rust functions to interpret a slice as a table (`root`, `size_prefixed_root`, `root_with_opts`, and `size_prefixed_root_with_opts`) verify the data first." The count is at least four of thirteen, under that table's own caveat [41].
139. COMPARISON-TABLES.md, Validation of untrusted data, "Depth and DoS bounds" (recursion depth 100). Not on that page: the limits differ by implementation, Java 100, C++ 100, Go 10,000 with a planned reduction (PB-limits), and that divergence holds this at 🔶.
140. COMPARISON-TABLES.md, Validation of untrusted data, "Untrusted bytes on the tolerant wire" (`max_depth` 64, `max_tables` 1M), where a verifier ships [136].
142. COMPARISON-TABLES.md, Validation of untrusted data, "Cross-language agreement on refusal" ("a conformance suite"). The suite's own page says it tests "completeness and correctness of Protocol Buffers implementations" and says nothing about malformed or hostile input either way (PB-conformance), so ? by the legend.
144. COMPARISON-TABLES.md, Reflection, text, tooling, "Runtime reflection" (descriptors, `Reflection`, `DynamicMessage`).
145. COMPARISON-TABLES.md, Reflection, text, tooling, "Runtime reflection" (binary schema plus `reflection.h` in C++, basic in C; MINI-REFLECTION). The last of those is what lifts this off ❌: "A more limited form of reflection is available for direct inclusion in generated code, which doesn't do any (binary) schema access at all", behind `--reflect-types` and `--reflect-names` (FB-cpp, Mini Reflection).
149. COMPARISON-TABLES.md, Reflection, text, tooling, "Text form" (ProtoJSON, with the implementations that do not conform to it named; text format). PB-json specifies the format and names its own gaps, "As of v25.x, the C++, Java, and Python implementations are nonconformant" on one flag, so the mark is 🔶.
150. COMPARISON-TABLES.md, Reflection, text, tooling, "Text form" (JSON in `flatc`; parsing in C++, C and Lobster of the thirteen listed, under that table's own caveat [41]); `flatc --json` converts offline (FB-flatc).
154. COMPARISON-TABLES.md, Reflection, text, tooling, "Doc comments" (the descriptor carries source info). `SourceCodeInfo` in PB-descriptor carries "any comments appearing before and after the declaration which appear to be attached to the declaration", as `optional string leading_comments = 3`, `optional string trailing_comments = 4` and `repeated string leading_detached_comments = 6`. 🔶: the descriptor carries them and a generator may read them, and whether any generator emits them into generated code is undocumented on protobuf.dev.
155. COMPARISON-TABLES.md, Reflection, text, tooling, "Doc comments" (`///` into generated code and the binary schema).
158. COMPARISON-TABLES.md, Reflection, text, tooling, "Languages" (ten first-party, forty-plus third-party). 🔶 because the project documents its own enum nonconformance across those runtimes (PB-enum).
159. COMPARISON-TABLES.md, Reflection, text, tooling, "Languages" (thirteen listed, coverage uneven). The shape of the unevenness is not on that page: of the thirteen, FB-support marks JSON parsing in three, reflection in two, the verifier in three (four with FB-rust, [136]) and mutation in five, and says C++ "has the richest feature set, and is likely most robust", all of it under that table's own caveat [41].
165. COMPARISON-TABLES.md, Declarations and the schema language, "Reserved names" (`reserved` numbers and names).
166. COMPARISON-TABLES.md, Declarations and the schema language, "Reserved names" ("— ; never remove, deprecate instead"); "Deprecation" (`(deprecated)`: accessors dropped, slot kept).
170. COMPARISON-TABLES.md, Evolution, "Change a type" (the compatible-pairs list; a truncation inside it is silent).
174. Not on COMPARISON-TABLES.md. The project documents a self-describing pattern, a message carrying a `FileDescriptorSet` beside the payload, and says in the same breath that "the reason that this functionality is not included in the Protocol Buffer library is because we have never had a use for it inside Google" (PB-techniques, Self-describing Messages). The schema is recoverable only out of band, the same shape as FlatBuffers' `.bfbs` [175] and the same mark.
175. COMPARISON-TABLES.md, Reflection, text, tooling, "Runtime reflection" (the binary schema as a separate artifact); The wire, "Version identity" (`file_identifier`, four hand-chosen characters). Out of band, like [174], and the same mark.
178. COMPARISON-TABLES.md, The wire, "Version identity" ("none; editions version the language"); the six wire types are frozen with no revision marker (PB-encoding).
179. COMPARISON-TABLES.md, The wire, "Version identity" (`file_identifier`, by hand): it identifies the schema author's format, not the FlatBuffers revision.
183. COMPARISON-TABLES.md, Evolution, "Compile-time guard" (`buf breaking`, third party: built by Buf Technologies and not by the Protocol Buffers project, buf).
184. COMPARISON-TABLES.md, Evolution, "Compile-time guard" (`flatc --conform`).

**Cap'n Proto and Avro**

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
29. One pointer per object, a tree and not a graph; cyclic or overlapping pointers can send a reader into an infinite loop, and the traversal limit is what defends against that (CP-encoding, Messages).
30. No reference type; recursion is a union naming the record, which yields a tree (AV-spec, Complex Types).
34. A union is two or more fields sharing storage, so any field type is an arm, including `Void` (CP-lang, Unions).
35. A union is a JSON array of schemas, any schema a branch, with two restrictions (AV-spec, Unions).
38. Defaults for `Text`, `Data`, `List` and nested structs (CP-lang, Structs).
39. A default for every field type (AV-spec, Record).
42. No scalar presence: data-section fields are stored XOR their default, so unset and set-to-default are one encoding, and a pointer's null is the only absence (CP-encoding, Value Encoding).
43. The idiom is a union with `null`, a value and not a presence flag (AV-spec, Unions).
47. No required marker (CP-lang).
48. A reader field with no default and no writer counterpart signals an error for the whole read (AV-spec, Schema Resolution).
52. Generics are first class; an omitted parameter is `AnyPointer` (CP-lang, Generic Types).
53. No generics and no `Any`; the writer's schema is always available, a different solution to the same problem (AV-spec).
57. `annotation foo(struct, enum) :Text;`, declarable by the user with twelve targets (`file`, `struct`, `field`, `union`, `group`, `enum`, `enumerant`, `interface`, `method`, `param`, `annotation`, `const`) and `*` to cover them all (CP-lang, Annotations).
58. "Attributes not defined in this document are permitted as metadata, but must not affect the format of serialized data" (AV-spec, Schema Declaration). That is user-extensible annotation in the format, so not ❌. 🔶 because the specification requires only that an implementation tolerate the attributes, not that it surface them to generated code or to a runtime API, and the three ✅ columns rest on exactly that.
61. Field positions are computed from the schema; the message carries no field identity (CP-encoding).
62. The container file embeds the writer's schema, and single-object encoding carries `C3 01` plus a schema fingerprint. The bare value encoding is untagged (AV-spec, Object Container Files; Single Object Encoding; Binary Encoding).
66. Packing is an optional pass over the unpacked layout (CP-encoding, Packing; CP-faq).
67. int and long are zig-zag varints (AV-spec, Binary Encoding).
71. Objects aligned to word boundaries, primitives to a multiple of their size (CP-encoding).
72. A byte stream with no alignment (AV-spec, Binary Encoding).
76. Segment ids are 32 bits, so the aggregate is not 32-bit capped; a single list is capped at 2^29 elements, and the C++ reader's traversal limit defaults to 64 MiB (CP-encoding, Lists; Far Pointers; CP-cxx, Security Tips).
77. Array and map blocks carry `long` counts, file data blocks carry a `long` object count and a `long` byte size, and the specification states no aggregate ceiling (AV-spec, Binary Encoding; Object Container Files). The specification is what every other Avro cell is answered from and it answers this one; no per-implementation ceiling was sourced either way.
81. Canonicalization is fully specified and is a separate conversion, not the default output (CP-encoding, Canonicalization; CP-tool).
82. A record's bytes are determined by the schema, but array and map block boundaries are the writer's choice, and Avro's Parsing Canonical Form canonicalizes schemas and not data (AV-spec, Binary Encoding; Parsing Canonical Form).
86. Fields match by name and a writer's extras are ignored, but a reader field without a default is a hard error against older data (AV-spec, Schema Resolution).
90. Any symbolic name can change as long as the type id and the ordinals stay (CP-lang, Evolving Your Protocol).
91. Aliases exist on named types and fields; "An implementation may optionally use aliases to map a writer's schema to the reader's" (AV-spec, Aliases).
94. A field's type or default value cannot change (CP-lang, Evolving Your Protocol).
95. Resolution promotes or signals an error, never a misdecode, and the failure is the whole read (AV-spec, Schema Resolution).
99. "Avro encodes a field even if its value is equal to its default"; a default is used only when the writer's schema lacks the field, so a default change cannot reinterpret stored bytes (AV-spec, Record; Binary Encoding).
100. Copying a struct with a `set` method keeps the original's size, because "the original could have been built with an older version of the protocol which lacked some fields", so fields the copier does not know ride through the copy (CP-cxx, Tips and Best Practices). 🔶 because it is a property of that copy idiom and not of every read-and-rewrite: a builder filled field by field keeps nothing.
103. A writer's field not in the reader's record is ignored (AV-spec, Schema Resolution).
106. Limits throw; nothing reports what was skipped (CP-cxx, Security Tips).
107. Errors are signaled; no counted report (AV-spec).
110. A declared enum `default` is used, otherwise an error is signaled (AV-spec, Enums; Schema Resolution).
111. The value survives the read: "In C++11, enums are allowed to have any value that is within the range of their base type, which for Cap'n Proto enums is `uint16_t`", and the project tells the caller to expect one, "Keep in mind when writing `switch` blocks that an enum read off the wire may have a numeric value that is not listed in its definition" (CP-cxx, Tips and Best Practices). 🔶 because the landing is defined and non-fatal but is the caller's own default case: nothing reports that an unlisted value arrived.
114. No encoding step; one field readable without parsing the whole; mmap (CP-home).
115. Values are variable-length and untagged, decoded into objects; no random access and no cooked form, deliberately (AV-spec, Binary Encoding).
118. Reads are pointer arithmetic over the buffer (CP-cxx).
121. Messages are built arena-style, sequentially in a segment, and a new segment is allocated when one fills, never per object (CP-cxx, Tips and Best Practices).
127. Orphans move subtrees WITHIN a message and exist only in memory a `MessageBuilder` owns; the project states the limitation directly: Cap'n Proto "is not well-suited for _writing_ via `mmap()`, only reading", because no mutable segment framing format has been designed. A received message is read through a `MessageReader` and rebuilt through a builder to change it (CP-cxx, Orphans; Serialization/Deserialization).
130. mmap is documented and a composite list is structs at a fixed stride, but nothing asserts two languages' native layouts against each other at open (CP-home; CP-encoding, Lists).
133. Link `libcapnp` and `libkj`, and `libcapnp-rpc` and `libkj-async` with RPC (CP-cxx, Generating Code).
137. Designed to be safe against malicious input, with the caveat that it has not undergone a formal security review (CP-faq).
138. A composite list is a tag word followed by its elements laid out inline at one stride, and a struct's layout is fixed by the encoding and not by a compiler (CP-encoding, Lists; Structs), so a list of structs is a fixed-stride row image any implementation computes identically.
141. Nesting depth 64 and a 64 MiB traversal limit, both configurable through `capnp::ReaderOptions` (CP-cxx, Security Tips).
146. `capnp/schema.h`, `capnp/dynamic.h` and `SchemaLoader`, documented for C++ (CP-cxx, Dynamic Reflection).
147. The writer's schema is always present and the generic API reads any record without code generation (AV-java).
151. `capnp convert` moves between binary, packed, flat, canonical, text and json, documented in the 0.7 release notes and not on the tool page, and the library JSON is C++ (CP-tool; CP-0.7).
152. The JSON encoding is part of the specification; `avro-tools` converts offline (AV-spec, JSON Encoding; AV-java).
156. The `doc` attribute is defined on records, fields and enums, and the specification says a schema's `doc` fields are ignored for schema resolution (AV-spec, Record; Enum). 🔶 because the specification says nothing about emitting a `doc` string into generated code, and that is what this row asks.
160. The C++ reference is the only reviewed implementation and every other is a third party's; the C implementation is "no longer maintained", one JavaScript implementation is "abandoned", and several are serialization-only (CP-otherlang). One official implementation is ❌ on a row asking for parity among several.
161. Six documented SDKs and a home page claiming more; no per-feature matrix (AV-docs; AV-home).
163. An omitted type id is derived from the parent scope's id and the declaration's name, "You cannot change the name of a type that doesn't have an explicit ID", so a rename or a move changes it unless the id is pinned (CP-lang, Unique IDs).
167. A retired number cannot be reused, because every new member's number must be larger than all previous members' and no member's number may change (CP-lang, Evolving Your Protocol). 🔶 because that is the number space growing rather than a reserved mechanism: nothing records that a number was retired or why, and the rules say nothing about retiring one in the first place.
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
| PB-arenas | https://protobuf.dev/reference/cpp/arenas/ |
| PB-techniques | https://protobuf.dev/programming-guides/techniques/ |
| PB-descriptor | https://github.com/protocolbuffers/protobuf/blob/main/src/google/protobuf/descriptor.proto |
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
