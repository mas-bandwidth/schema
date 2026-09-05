# Porting: the techniques register

This page is the register of techniques the nine table backends carry. A
technique is a METHOD OF IMPLEMENTATION first — how a read path is made
allocation-free, how a 64-bit value avoids boxing, how a codec is shaped for
the optimizer, how a keyed array indexes — and an instrument or a gate second.
Each has one section here: the method in a paragraph, where it lives in the
reference, the language it was proven in, its measured effect where one was
taken, its negative control, and a cell per language saying whether that
language carries it.

**The law, in the owner's words:** *"Make sure we don't do silly things again
like, put some cool technique in language X, and then forget it for all other
languages and we have to rediscover it."* A technique proven in one language's
port belongs to every language. When it lands anywhere, it is written into
this register the same day, and every other port either carries it or states
here why the platform cannot.

**How the page is used.** It is read BEFORE a port brief is written, so the
brief carries every row and the port PR is not ready until its column is
filled. It is read BEFORE a performance round is started, so a lever found in
one language is tried in every language that can express it, and its absence
elsewhere is a stated decision rather than an oversight. And it is read by
every blind reader, who checks the column against the tree and not against
memory. A lever proven in one language is only a THEORY in the next — the
register says where to look, and the port's own measurement says whether it
holds there.

## How a technique enters

Three rules, and the gate holds the second.

1. **Same-PR entry.** A port PR or a reference PR that uses a method this
   register lacks adds the method's section in that PR — its own cell carried,
   the rest not yet — exactly as a spec section lands with its code. A
   technique with no section is a technique the next port will rediscover.
   A new section takes the next free number of its run: methods continue
   `M`, instruments continue `I`, gates a port found continue `J`.
2. **A not-yet cell cites the carry-across.** Every ❌ cell names the issue
   that carries the technique to that language (`❌ #NNN`), or the cell is a
   `—` with the reason the platform cannot. The gate goes red on a bare ❌.
   Adding a section therefore files the carry-across issue at the moment of
   discovery, titled `<technique>: carry to <languages>`, naming the proving
   language and the reference site in the body.
3. **Every blind read reports two lists against this register:** methods the
   port uses that the register lacks (rule 1 applies — they enter in the fix
   reply), and methods the register has that the port lacks (rule 2 applies —
   each becomes a carried cell or a cited one). `test/conformance/README.md`
   says so where it points ports at this page.

## The grammar the gate reads

`compiler/porting_test.go` parses this page, the `Makefile` and every
`make/<lang>.mk` it includes, the driver registry and the workflows, and holds
them to each other. Its own negative
control plants a carried cell naming no target, a carried cell naming a target
nothing runs, a bare ❌ and a `—` with no reason into a copy of the register,
and requires the finding that names each.

- A technique is a `###` section. Its **Targets:** line names the slugs of its
  Makefile-checkable form, or `none` for a method the tree holds by inspection
  and by the instruments of other sections.
- The cell table's header names the columns, the reference first; the first
  table's header is the order every table repeats, and its set is exactly the
  languages the harness discovers as `test/conformance/<lang>/driver` — a
  port's column is one of the shared edits docs/CONTRIBUTING.md tolerates.
- A cell starts with one of three marks. **✅** carried — followed by what
  proves it. **❌ #NNN** not yet — the carry-across issue. **—** the platform
  cannot — followed by the reason.
- **What a carried cell names.** In backticks: a Makefile target
  (`tables-…`, `conformance-…`), a Go test (`TestName`, in the root module or
  in `test/go-tables`, both of which `make test` runs), or a source path with its
  `:line` citation. The gate holds every named target to exist and to be RUN
  by the tree, every named test to exist, and every cited path to be in the
  tree — a line may drift, a path may not. In a section whose Targets line is not `none`, a
  carried cell must name at least one target or test; a cell that names none
  is held to the conventional names, `tables-<lang>-<slug>` for a port and
  `tables-<slug>` for the reference.
- **A target counts only if the tree RUNS it**: reached from `make test` (a
  prerequisite, or a `$(MAKE) <target>` line, transitively — every leg's
  `test-<lang>` is one, through `TEST_LEGS`), from a `tables-<lang>-release`
  target (which `certify.yml` discovers from the same files and runs by
  name), or by name from a workflow. A target that exists
  and runs nowhere is a green light nobody has tested, and the gate says so.
- **A measured row with a null ceiling cites why.** An allocation gate that
  reports a path without gating it names the allocation it licenses at the
  row, and the sentence is re-read when the emitter moves: the JavaScript
  `Scene head deref` row carried a BigInt rationale for a path that had
  stopped reading BigInts.

## The register

### M1 — The read path allocates nothing

**Method.** Load fills caller-owned storage; the reader is a cursor over the
caller's buffer, with no per-field object and no sub-view. A nested body is
bounded by narrowing the reader's LIMIT and restoring it after — never by
slicing a new view, because in a managed language a sub-view is an allocation
and a limit is an integer. Where the language can hold a reader on the stack
(a C++ value, a C struct, a Rust reborrow, a C# `ref struct`), a stack-value
sub-reader costs nothing and the reference uses one. Where it cannot (Java,
Dart, JavaScript), the limit is the technique in its stated form.

**Reference.** `internal/codegen/cpptable/cpptable.go:524-538` (the reader is
`buffer/size/offset`); the stack sub-reader at
`internal/codegen/cpptable/codecs.go:1113`. The limit form:
`internal/codegen/javatable/runtime.go:151-186`, with the reason written at
`:151-158`.

**Proven in.** C++; the limit form in Java, then JavaScript and Dart.

**Measured effect.** Exact zero on Load, Measure and Save in Go
(`testing.AllocsPerRun`), Java (`getCurrentThreadAllocatedBytes`), JavaScript
(0.0 bytes per iteration on KeyedConfig Load/Measure/Save at 300k iterations)
and Dart (0 scavenges over 20,000 × 8 records under AOT). The instrument is
M1's second half, section I1.

**Negative control.** I1's: a planted allocation per record turns the gate
red on the one row it was planted in.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `internal/codegen/cpptable/codecs.go:1113` | ✅ `internal/codegen/ctable/codecs.go:1074` | ✅ `internal/codegen/rusttable/runtime.go:364` | ✅ `internal/codegen/gotable/gotable.go:717` | ✅ `internal/codegen/cstable/cstable.go:958` (a `ref struct`) | ✅ `internal/codegen/javatable/codecs.go:1016` (the limit) | ✅ `internal/codegen/jstable/jstable.go:960` (the limit) | ✅ `internal/codegen/darttable/codecs.go:762` (the limit) | — a decoded BEAM term is an allocation and no buffer is caller-owned; the leg pins the per-case COUNT instead (`tables-elixir-alloc-audit`, docs/SPEC-TABLES.md) |

### M2 — 64-bit values without boxing

**Method.** A 64-bit word on the wire is read and written as two composed
32-bit halves, so a language that boxes wide integers never sees one on the
hot path. The reference does it too, though C++ boxes nothing: the composed
form is byte-order-neutral by construction (M12) and one shape ports mirror.
On an accelerator path (a block's `offset_of`, a cook's self-relative delta)
the composed form is what keeps a deref allocation-free in JavaScript.

**Reference.** `internal/codegen/cpptable/cpptable.go:513` (`put64` as two
`put32`) and `:538` (`get64` as two `get32`). The JavaScript accelerator
paths: `internal/codegen/jstable/block.go:384` (`offset_of` from two u32
reads, the reason at `:375-382`) and `internal/codegen/jstable/cook.go:513-517`
(the delta from a u32 low half and an i32 high half).

**Proven in.** C++; measured in JavaScript.

**Measured effect.** JavaScript's `Scene head deref` row went from a BigInt
per edge to 0.7 bytes per iteration (ceiling 8) once the delta was composed.

**Negative control.** The JavaScript allocation gate's row for the path: a
`getBigUint64` on it reads as bytes per iteration.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `internal/codegen/cpptable/cpptable.go:513` | ✅ `internal/codegen/ctable/ctable.go:514-518` | ✅ `internal/codegen/rusttable/runtime.go:352-356` (one `from_le_bytes`; nothing boxes) | ✅ `internal/codegen/gotable/gotable.go:710-714` | ✅ `internal/codegen/cstable/cstable.go:986-991` | ✅ `internal/codegen/javatable/runtime.go:206-210` | — the accelerator paths compose (`internal/codegen/jstable/block.go:384`, `cook.go:513`); a 64-bit wire FIELD's value is a BigInt or loses precision, so those rows read one under a stated ceiling (`test/js-tables/main.mjs`, the RootConfig rows) | — an `int` is a 64-bit machine word and nothing boxes (`internal/codegen/darttable/block.go:213`) | ❌ #403 |

### M3 — A float never crosses a call

**Method.** The generated body reads the bits through the view and bit-casts
inline; only integers cross into the reader and writer. There is no `getF32`
primitive, because a double crossing a call boundary on a JIT threshold boxes,
and a generated codec must not depend on the compiler's inlining budget.

**Reference.** `internal/codegen/cpptable/cpptable.go:572-573`
(`table_bits_to_float` and its three siblings, `inline` free functions over
`get32`/`get64`). The JavaScript statement of the rule:
`internal/codegen/jstable/jstable.go:979-985` and `codecs.go:897-902`; elevated
to a cross-port rule at docs/SPEC-TABLES.md's JavaScript allocation paragraph.

**Proven in.** C++; measured in JavaScript.

**Measured effect.** On the pinned node major a float crossing a helper was
steady at sixteen bytes a call, and invisible on a newer V8 — which is why the
gate is pinned (I14).

**Negative control.** The allocation gate's wire rows: a helper put back
reads as bytes per iteration on the pinned runtime.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `internal/codegen/cpptable/cpptable.go:572-573` | ✅ `internal/codegen/ctable/ctable.go:565-568` | ✅ `internal/codegen/rusttable/runtime.go:181-199` | ✅ `internal/codegen/gotable/codecs.go:781-786` | ❌ #404 | ✅ `internal/codegen/javatable/codecs.go:919-921` | ✅ `internal/codegen/jstable/codecs.go:1209-1219` | ❌ #404 | — a BEAM float is a boxed term whatever the call shape; `R.f32_bits` costs what the term costs |

### M4 — The codec is shaped for the optimizer

**Method.** The read and write primitives and the FIXED-class bodies are
force-inlined, so the cursor stays in registers across a body, and the
directive stops at the variable class, where a recursive `always_inline` is a
compile error. Measure is left plain. Where the language has an aliasing
qualifier or gives noalias by construction (Rust's `&mut`), the body takes it.
Where the language has neither a directive nor a qualifier, the shape it can
take is M4b's.

**Reference.** `internal/codegen/cpptable/cpptable.go:358-364` (the macro,
with the reason at `:350-357`) and `codecs.go:732` (the stop at the
variable class). Rust: `internal/codegen/rusttable/runtime.go:220-363` and
`codecs.go:663-669`. C: `internal/codegen/ctable/ctable.go:275-283`, pinned by
`TestCForceInlineStopsAtTheVariableClass`.

**Proven in.** C++ (#350).

**Measured effect.** #350's arm is the C++ reference every pairing board
measures against. The lever did not transfer to Go as written: the Go leg
writes at 0.20× C++ after #350, and the Go-shaped restructure measured 1.42×
(#362). A lever is a theory in the next language.

**Negative control.** `TestCForceInlineStopsAtTheVariableClass` and
`TestPointerSurfaceEmitted`: the macro on a variable-class body, or missing
from a fixed one, is red.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `internal/codegen/cpptable/cpptable.go:358-364` `TestPointerSurfaceEmitted` | ✅ `internal/codegen/ctable/ctable.go:275-283` `TestCForceInlineStopsAtTheVariableClass` | ✅ `internal/codegen/rusttable/runtime.go:220` (`#[inline(always)]`; noalias by construction) | ❌ #362 | ❌ #405 | — the JIT takes no inlining directive and no aliasing qualifier; M4b is the shape | — no inlining directive and no aliasing qualifier for V8; M4b is the shape | — no inlining directive and no aliasing qualifier for the AOT compiler; M4b is the shape | — no inlining directive and no aliasing qualifier for the BEAM; M4b is the shape |

### M4b — The codec is self-contained

**Method.** The VM-class twin of M4: the generated unit stands alone on its
platform library — no import of a sibling runtime, no import outside the unit
— and the bit machinery a body needs is emitted with the unit rather than
reached through a dependency, so the JIT's inlining budget is spent inside one
compilation unit. A `standalone` gate compiles the generated sources against
the platform alone.

**Reference.** `tables-cs-standalone` (a `<Package>Table.cs` against the BCL
alone); `tables-js-standalone` (no `import` of `serialize`, none outside the
unit); `tables-dart-standalone` and its control. The Elixir form inlines the
bitstring segments into the body itself
(`internal/codegen/elixirtable/elixirtable.go:12-13`).

**Proven in.** C#.

**Measured effect.** Structural: the gate holds a property, and the read
floors of I1 are measured over the unit it holds.

**Negative control.** `tables-dart-standalone-negative-control` plants a
`package:` import and requires red.

**Targets:** standalone

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| — native class: the primitives are force-inlined instead (M4) | — native class: the primitives are force-inlined instead (M4) | — native class: `#[inline(always)]` on the primitives instead (M4) | ❌ #406 | ✅ `tables-cs-standalone` | ✅ `tables-java-standalone` | ✅ `tables-js-standalone` | ✅ `tables-dart-standalone` `tables-dart-standalone-negative-control` | ❌ #406 |

### M5 — Keyed arrays index by key, refused at both ends

**Method.** An enum-keyed array has one slot per named variant: key 1 lives in
slot 0, `E.Max` in the last, and the accessor refuses None (key 0) and any key
past `E.Max` in EVERY build — an unsigned compare covering both ends, then an
abort that survives NDEBUG. Iteration yields the key beside the element, so
the caller never does the shift. The runtime's own bounds check is not the
technique: it is absent in C, silent past the array in JavaScript, and names
an index rather than a key everywhere else.

**Reference.** `internal/codegen/cpptable/cpptable.go:222-260`
(`TableKeyed<T,E>`, `RefuseKey`); golden
`testdata/golden/tables/examples/KeyedTable.h:279-333`.

**Proven in.** C++; the None-only guard was found and fixed in JavaScript
(#377 names the same gap in C).

**Measured effect.** Structural. JavaScript's `get(4)` on a three-variant
enum answered `undefined` before the guard was made symmetric.

**Negative control.** `tables-keyed-none-refusal-negative-control`,
`tables-keyed-max-refusal-negative-control` and
`tables-keyed-shift-negative-control` in the reference;
`tables-js-keyed-negative-control` puts a None-only guard back and requires
"accepted E.Max + 1 as a key".

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `tables-keyed-none-refusal-ndebug` `tables-keyed-max-refusal-ndebug` | ❌ #377 (None refused; past Max is an unchecked plain array) | ✅ `internal/codegen/rusttable/runtime.go:133-169` (panics in every build) | ✅ `TestKeyedRefusesNone` `TestKeyedRefusesPastMax` `TestKeyedPlacesByKey` | ✅ `test/cs-tables/src/Program.cs:1672` (None refused; past Max is the CLR's, always on) | ❌ #407 (refuses both ends; no test holds it) | ✅ `tables-js-keyed-negative-control` | ❌ #407 (refuses both ends; no test holds it) | ❌ #407 (guards refuse both ends; no test holds it) |

### M6 — The variable class on the wire: a flat node table and an identity map

**Method.** A pointered root packs as ONE flat node table under the reserved
id `0xFFFFFFFFFFFFFFFF`, every node numbered depth-first in pre-order at first
visit, and a pointer on the wire is a canonical LEB128 node index under kind
17 (M20). The pack walk keeps an
identity map from node address to index, so a node shared by two pointers is
written once and a cycle is refused at an OPEN entry rather than by a depth
cap. A fixed-class reader that has no variable class still carries kind 17 in
its fixed-width skip row, so a pointered wire reads as unknown fields rather
than as damage.

**Reference.** `internal/codegen/cpptable/pointers.go:545` (the table),
`:245` (the numbering), `:257-394` (the map and the OPEN refusal);
`TablePackMap` at `internal/codegen/cpptable/arena.go:310`. The skip row:
`case 4: case 8: case 10: case 17:` in every fixed-class runtime.

**Proven in.** C++ (#373 the identity map, #376 the flat table).

**Measured effect.** The wire is byte-identical between the C++ codec and
the compiler's Go engine in both directions (`tables-flat-wire`), which the
nested form could not be.

**Negative control.** `tables-flat-wire-negative-control` numbers the nodes
in post-order in the emitter and requires the lock red;
`tables-shared-node-negative-control` writes a shared node twice.

**Targets:** flat-wire

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `tables-flat-wire` `tables-flat-wire-negative-control` | ❌ #408 (the earlier nested form: a depth cap, no identity map) | ❌ #349 | ❌ #349 | ❌ #349 | ❌ #349 | ❌ #349 | ❌ #349 | ❌ #349 |

### M7 — A block row is reached by stride, with no per-row object

**Method.** A block array is `(base, count, stride)`; reaching row i is
`base + i * stride`, and iterating is advancing by the stride. No handle is
built per row. In a managed language the same shape is an offset (`<F>At(i)`
returning an integer) or one caller-owned cursor moved per row — a
per-row object is what the technique excludes, and where a convenience
iterator allocates one, the emitter says so at the site and offers the
allocation-free spelling beside it.

**Reference.** `internal/codegen/cpptable/block.go:120`
(`TableBlockRows<T>`, `:626-627` the accessor). The managed spellings:
`internal/codegen/javatable/block.go:420-432` (`<F>Count()`/`<F>At(int)` as an
offset; the allocating record iterator named at `:130-136`),
`internal/codegen/darttable/block.go:217-247` (`<F>Cursor()`, `<F>At(i,
cursor)`), `internal/codegen/jstable/block.go:382-386` (an offset).

**Proven in.** C++.

**Measured effect.** The block row walk is exactly 0 in Java's allocation
table and 0.0 bytes per iteration in JavaScript's (`RenderFrame ships walk`).

**Negative control.** I1's rows for the walk.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `internal/codegen/cpptable/block.go:120` | ✅ `internal/codegen/ctable/block.go:109-125` | ✅ `internal/codegen/rusttable/block.go:583-598` (a slice over the region) | ✅ `internal/codegen/gotable/block.go:243-256` | ✅ `internal/codegen/cstable/block.go:781-790` | ✅ `internal/codegen/javatable/block.go:420-432` | ✅ `internal/codegen/jstable/block.go:382-386` | ✅ `internal/codegen/darttable/block.go:217-247` | ❌ #409 (an eager list of `count` sub-binaries per call) |

### M8 — The cook opens in O(1)

**Method.** Open reads the magic in the machine's own order and compares,
then the order word, the build version, both reserved words, the alignment
word (a power of two, in range, a multiple of the root's), DERIVES the data
offset rather than reading it, checks the file equation over both part lengths
by subtraction, requires the data part to hold the root, and checks the base's
alignment as a residue. Nothing per node. A deref is bounded where the
language gives the reader a region to bound it against.

**Reference.** `internal/codegen/cpptable/cook.go:150-173`; golden
`testdata/golden/tables/examples/TablesTable.h:492-540`.

**Proven in.** C++.

**Measured effect.** `tables-cook-open-1gb` and `tables-cook-open-cs-1gb`
open a gigabyte cook in the same time as a small one.

**Negative control.** `tables-cook-open-lengths-negative-control` and
`tables-cook-open-root-negative-control` remove one check each in the emitter;
the harness's cook forgery battery (111 rows) holds every port.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `internal/codegen/cpptable/cook.go:150-173` `tables-cook-open` | ✅ `internal/codegen/ctable/cook.go:157-178` | ✅ `internal/codegen/rusttable/cook.go:206` | ✅ `internal/codegen/gotable/cook.go:542-624` | ✅ `internal/codegen/cstable/cook.go:575-660` `tables-cook-open-cs` | ✅ `internal/codegen/javatable/cook.go:372-455` (an offset residue; the deref bounded to the record) | ✅ `internal/codegen/jstable/cook.go:438-483` | ✅ `internal/codegen/darttable/cook.go:390-446` (an offset residue) | ✅ `internal/codegen/elixirtable/cook.go:145` (a `lead` parameter; a binary has no address) |

### M9 — One text walk for the whole unit

**Method.** The text form is one generic walk over the descriptors, emitted
once per unit and identical across every unit of the corpus, rather than a
codec per table. A byte comparison across units holds it. The variable class
rides the same walk through adapters (#388), with a shared node labeled once
as `&node` and named by its label after (M14).

**Reference.** `internal/codegen/cpptable/json.go:166-2192` between the
`---- json walk: begin/end ----` markers; `tables-json-walk` compares every
generated unit's walk; `tables-json-graph-walk` the variable class's.

**Proven in.** C++.

**Measured effect.** Structural; the walk's cost is I1's text rows.

**Negative control.** `tables-json-negative-control` sabotages the walk's
offset arithmetic and requires red.

**Targets:** json-walk

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `tables-json-walk` `tables-json-graph-walk` | ✅ `tables-c-json-walk` | ✅ `tables-rust-walk` | ✅ `tables-go-json-walk` | ✅ `tables-cs-json-walk` | ✅ `tables-java-json-walk` | ✅ `tables-js-json-walk` | ✅ `tables-dart-json-walk` | ✅ `tables-elixir-walk` |

### M10 — Hooks and the allocator contract

**Method.** `schema_assert` and `schema_fatal` are overridable behind
`#ifndef`; `schema_allocate` and `schema_release` are emitted only into a unit
with a variable-length table; every allocation on the pointer path goes
through the caller's alloc/free pair with a context, and a counting test
proves nothing falls through to the default. Who calls the allocator is per
form (docs/SPEC-TABLES.md): the block form takes a caller-provided allocator;
a managed backend allocates inside its runtime and says so.

**Reference.** `internal/codegen/cpptable/cpptable.go:141-168`;
`TableAllocator` at `internal/codegen/cpptable/arena.go:59-74`;
`test/tables/hooks_main.cpp:18-90`.

**Proven in.** C++ (#386).

**Measured effect.** Structural.

**Negative control.** `tables-cook-write-hooks-negative-control`.

**Targets:** hooks

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `tables-hooks` | ❌ #410 (raw `calloc`/`free` on the pointer path) | — no allocating path exists: a pointered unit's wire is refused and the fixed class allocates nothing (`tables-rust-alloc-audit`) | — the runtime allocates inside itself and says so (docs/SPEC-TABLES.md, "who calls the allocator is per form") | — the runtime allocates inside itself and says so (docs/SPEC-TABLES.md) | — the runtime allocates inside itself and says so; where it does is named per path at `tables-java-alloc` | — the runtime allocates inside itself and says so; every unavoidable allocation is named in the floor (docs/SPEC-TABLES.md) | — the runtime allocates inside itself and says so (docs/SPEC-TABLES.md) | — the BEAM allocates every term; the count is pinned instead (docs/SPEC-TABLES.md) |

### M11 — The layout contract is asserted in generated code

**Method.** Every cooked record and every block projection carries a
compile-time (or first-open) assertion of size, alignment and every field
offset against the numbers the compiler computed, including each array's pitch
constant against the row's own size — two independent derivations held against
each other. A producer that disagrees is refused before any row is read.

**Reference.** `internal/codegen/cpptable/cook.go:334-336` and
`block.go:491-492` (`static_assert`); C's C89-portable macro at
`internal/codegen/ctable/ctable.go:291-293`.

**Proven in.** C++.

**Measured effect.** Structural; `tables-rust-big-endian` checks the Rust
asserts on a foreign target.

**Negative control.** `tables-block-layout-model-negative-control` and
`tables-block-pitch-negative-control`.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `internal/codegen/cpptable/cook.go:334-336` | ✅ `internal/codegen/ctable/ctable.go:291-293` | ✅ `internal/codegen/rusttable/block.go:87-93` `TestCookLayoutIsAssertedAtCompileTime` | ✅ `internal/codegen/gotable/block.go:491-572` (at package init) | ✅ `internal/codegen/cstable/block.go:431-437` | ✅ `internal/codegen/javatable/block.go:479` | ✅ `internal/codegen/jstable/block.go:610-639` | — no second model to check against: the generated offsets ARE the model and the build version refuses a producer that disagrees (docs/SPEC-TABLES.md) | — no struct layout control, so the accelerators are read-only at the compiler's offsets and the build version is the contract (`internal/codegen/elixirtable/elixirtable.go:15-18`) |

### M12 — Byte order is the reader's, not the host's

**Method.** Every multi-byte wire read and write is explicit little-endian
byte composition; the host's order is never consulted. An accelerator refuses
a foreign file twice over: the magic, read in the machine's own order and
compared, and then the order word. The cook writer takes the target's order
as a parameter.

**Reference.** `internal/codegen/cpptable/cpptable.go:513-538`; the cook
magic and order word at `testdata/golden/tables/examples/TablesTable.h:435-449`
and `:501-502`.

**Proven in.** C++ (#303).

**Measured effect.** The s390x battery (`tables-big-endian`, `conformance-big-endian`)
is green under emulation.

**Negative control.** `tables-big-endian-negative` puts one store back to
host order and requires red on the target while green on the host.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `internal/codegen/cpptable/cpptable.go:513-538` | ✅ `internal/codegen/ctable/ctable.go:503-518` | ✅ `internal/codegen/rusttable/runtime.go:334-354` | ✅ `internal/codegen/gotable/gotable.go:698-714` | ✅ `internal/codegen/cstable/cstable.go:971-991` | ✅ `internal/codegen/javatable/block.go:101-117` | ✅ `internal/codegen/jstable/jstable.go` (every DataView read passes `true`) | ✅ `internal/codegen/darttable/block.go` `internal/codegen/darttable/cook.go` (`Endian.little` on every read) | ✅ `internal/codegen/elixirtable/block.go:242` (every segment names `little`) |

### M13 — Descriptors are constants

**Method.** The reflection descriptors — field tables, type info, the block
and cook graphs — are constants in the binary or safely published finals:
read without allocation, initialized without a lazy path, and immutable. Where
a language needs a factory to break an initialization cycle, the factory is
the one exception and is named. The plain-cache idiom (read a static, build on
null, store back, no lock) is banned by name.

**Reference.** `testdata/golden/tables/examples/KeyedTable.h:2245-2249`
(`static const` inside an `inline` accessor). The safe-publication form:
`internal/codegen/javatable/codecs.go:1334` (the holder class), gated by
`TestJavaDescriptorsAreSafelyPublished`.

**Proven in.** C++; the publication hazard found and fixed in Java.

**Measured effect.** `TestToJsonAllocatesNothing` / `TestFromJsonAllocatesNothing`
exact zero in Go; the Go soak found a lazily built descriptor once.

**Negative control.** `TestJavaDescriptorsAreSafelyPublished` fails on the
plain-cache line.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `testdata/golden/tables/examples/KeyedTable.h:2245-2249` | ✅ `internal/codegen/ctable/ctable.go:45-50` (defined in `<Base>Table.c`) | ✅ `internal/codegen/rusttable/descriptors.go` (`&'static`) | ✅ `internal/codegen/gotable/codecs.go:1116-1138` | ❌ #411 (the plain-cache idiom) | ✅ `TestJavaDescriptorsAreSafelyPublished` | ✅ `internal/codegen/jstable/codecs.go:1301-1324` (built once on first use, frozen; one thread) | ✅ `internal/codegen/darttable/descriptors.go:75` (`const` descriptors, static tear-offs in the constant pool) | ✅ `internal/codegen/elixirtable/descriptors.go:4-12` (module attributes) |

### M14 — The `&node` label in the text form

**Method.** A pointered tree reads and writes as nested tables; a node shared
by two pointers is written once, labeled `&node` at its first appearance, and
named by its label after, so the text form round-trips the graph's identity
without a node-table syntax the reader has to learn.

**Reference.** `internal/codegen/cpptable/json.go`, the variable-class
adapters; `tables-json-graph-walk`.

**Proven in.** C++ (#388).

**Measured effect.** Structural.

**Negative control.** `tables-shared-node-negative-control`.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `tables-json-graph-walk` | ❌ #408 | ❌ #349 | ❌ #349 | ❌ #349 | ❌ #349 | ❌ #349 | ❌ #349 | ❌ #349 |

### M15 — One walk for the numbering, the pack measure and the pack

**Method.** The three pointer-graph walkers — the numbering (docs/SPEC-TABLES.md
§3.1), Lock's sizing and Lock's pack (§6.3) — are ONE depth-first walk over a
member's fields in DECLARATION ORDER that descends every by-value edge in place
(a nested variable table, an element of a bounded or keyed array of them, a
union's set arm) to reach the pointer fields inside it. The three emitters
share the walk's skeleton and differ only in what they do at an edge, so a
node's first visit is the same in all three by construction and the region a
Lock lays out is in the order the wire numbers. The shape to refuse is the
grouped one — every pointer field, then every by-value nesting, then every
union arm — because it is self-consistent: the backend re-reads its own bytes,
the wire surface is green, and every corpus seed numbers the same under both
orders until a by-value edge declared before a pointer field reaches a shared
node first. Then the same value has two wires, and the tool's is the page's.

**Reference.** `internal/codegen/cpptable/pointers.go:150` (`emitEdgeWalk`,
with `edgeVisitor` at `:95` and the per-field classification at `:116`),
shared by `emitNumber`, `emitPackMeasure` and `emitPack`; the tool's walk is
`internal/tablewire/nodes.go:112` (`visitEdges`).

**Proven in.** C++ (#433 — found by #429's wire fuzzer at the `stream_parts`
seed's id pass, where dropping one field separated the two orders).

**Measured effect.** Structural: on `stream_arm_first`, the corpus value whose
numbering differs between the two orders, the numbering, the region layout and
the cook's node order agree with the tool byte for byte.

**Negative control.** The corpus row itself: under the grouped walk
`stream_arm_first` is red on every surface a numbering reaches — `wire`,
`json-read` and `cook-write` in the harness, the golden pin and the
region-layout check in `test/tables/main.cpp` — and green under the one walk.
`tables-flat-wire-negative-control` reds the cross-implementation lock on a
post-order numbering the same way.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `internal/codegen/cpptable/pointers.go:150` `conformance` | ❌ #433 (its `pack_measure` and `pack` take every pointer field before every by-value nesting, `internal/codegen/ctable/pointers.go:220`, `:280`) | ❌ #349 | ❌ #349 | ❌ #349 | ❌ #349 | ❌ #349 | ❌ #349 | ❌ #349 |

### M16 — The presence companion rides beside the array walks

**Method.** An optional array — `?[..N]T`, `?[N]T` (docs/SPEC-TABLES.md §2.3)
— is the presence companion carried BESIDE the array walks rather than a
second array walk. Measure and save gate the whole array framing on
`<field>_present` and never on the count: absent writes nothing, present
always rides, a counted array as its live count with ZERO INCLUDED and a
fixed array whole. Load sets the companion where the field rode, and the ONE
place it does not is the element-kind mismatch, which returns before the
assignment so a foreign element kind leaves the field at its declared default
— absent. The descriptors carry `optional` and `present_offset` on an ARRAY
field beside the array columns, so a generic walker reaches presence the same
way on every field shape. The text form reads the KEY's presence as presence
and writes a present empty array as `[]`. The shape to refuse is presence
derived from the count, because it makes "present and empty" and "absent" one
value — the exact thing the presence bit exists to spell.

**Reference.** `internal/codegen/cpptable/codecs.go:588` (the optional branch
of `emitTableMeasureField`), the write side at `:894`, the read side's
`_present` assignment at `:1229`; the tool's are
`internal/tablewire/encode.go:131` and `internal/tablewire/decode.go:319`.

**Proven in.** C++ and the tool (#392).

**Measured effect.** Structural: the `message_trace` instance carries the
construct at three depths in both spellings — counted over tables, fixed over
tables, fixed over scalars, fixed over enums, counted over enums — and the
wire, the text and the cook agree with the tool byte for byte.

**Negative control.** `TestReportRowsDecodeThroughTheEngine` pins the
optional-field state of every hostile row, so a load that sets presence on an
element-kind mismatch goes red where no counter moves; dropping the fixed
spelling's measure arm reds `test/tables/main.cpp`'s `test_optional_arrays`
on the corpus rows that carry it.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `internal/codegen/cpptable/codecs.go:588` `test/tables/main.cpp:6814` `TestReportRowsDecodeThroughTheEngine` | ❌ #392 | ❌ #392 | ❌ #392 | ❌ #392 | ❌ #392 | ❌ #392 | ❌ #392 | ❌ #392 |
### M17 — A node larger than a slab takes a span of the address space

**Method.** An arena hands out nodes by bumping inside a fixed-size slab, and a
BYTE BUFFER (docs/SPEC-TABLES.md §2.5) is the one node whose size the schema
does not bound — it is exactly the length the caller asked for. A blob that
does not fit a slab is not chunked and does not enlarge the slab: it takes a
SPAN of the arena's address space. Whole segment indices are reserved from the
cursor, starting at the index AFTER the cursor's so nothing is ever handed out
inside the span, and one contiguous block is published under the first of them;
the indices the span covers past that one stay null, which is enough because
only a node's START is resolved through the segment table and a blob's bytes
follow its header inside the one allocation. The tail of the segment the cursor
was in is slack, as a slab tail is. Exhaustion is a loud refusal — never a
smaller blob. The shape to refuse is bump allocation with a bigger slab: it
turns every node's cost into the largest blob the program ever writes.

**Reference.** `internal/codegen/cpptable/arena.go:290-320`
(`TableArenaGrabSpan`), taken at `internal/codegen/cpptable/arena.go:397`
(`AllocBlob`, on the one branch a blob past `kTableSlabBytes` reaches).

**Proven in.** C++ (#259).

**Measured effect.** Structural: a `bytes(65536)` field costs 64 KB in every
instance; a `*bytes` costs the eight-byte slot plus what the node holds, at any
size, with one allocation per blob and no slab growth.

**Negative control.** `tables-blob-span-negative-control` — the size test that
sends a large blob to the span is made never to fire, so the blob is
bump-allocated in a slab it does not fit; `test/tables/blob_span_main.cpp`
reads it back after the nodes allocated behind it and the bytes are gone.

**Targets:** blob-span-negative-control

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `tables-blob-span-negative-control` | ❌ #259 | ❌ #259 | ❌ #259 | ❌ #259 | ❌ #259 | ❌ #259 | ❌ #259 | ❌ #259 |


### M18 — A union arm is a field line

**Method.** An arm of any field type is emitted by the SAME code that emits
a field of that type, one level down (docs/SPEC-TABLES.md §2.6): the arm's
payload is exactly the bytes a field puts after its own framing prefix, with
the arm's `L` standing in for the field's own length where that kind has one
and framing the fixed width where it does not. **AN ARM HEADER IS A FIELD
HEADER** (M20): the arm id reference, the arm's own KIND byte, `L`, then the
payload — so a retyped arm is an ordinary KIND MISMATCH, counted, the union
left `None` and the parent reading on past `L`, exactly as a retyped field
is. What the length then checks is framing alone: a fixed-width arm whose `L`
is not its kind's width, and a length-shaped arm whose payload is damaged
inside its own `L`, are each `malformed` with that same outcome, and an arm
whose payload is a body ends at its terminator or it is damage. An arm with
NO PAYLOAD takes no storage: the tag alone is the value, kind `32` with
`L = 0` on the wire, `null` in the
text. Arm storage sits at offsets from the union's own storage base with the
tag at 0, and the descriptors carry the arm's `field` row and its `size`, so
one generic walker reaches an arm exactly as it reaches a field. The shape to
refuse is a second emitter for arms: an arm codec written beside the field
codec drifts from it the first time a kind changes, and the drift is silent
on both wires.

**Reference.** `internal/codegen/cpptable/arms.go` (`emitArmStorage`,
`emitArmMeasure`, `emitArmSave`, `emitArmLoad` — each one dispatching into
the field emitters), the descriptor columns at
`internal/codegen/cpptable/codecs.go` (`unionArmsLambda`); the tool's are
`internal/tablewire/decode.go` (`arm`) and `internal/tablewire/encode.go`
(`encodeArm`).

**Proven in.** C++ and the tool (#396 item 1, #392).

**Measured effect.** Structural: a union of table arms leaves its holder
FIXED, and so does a union of general arms — every arm's storage is the
field's storage, so `tables/messages` stays in the zero-cost class with a
flags arm, a fixed-array arm, a nested-union arm and a payload-free arm in
its root union. The corpus carries an arm of every shape at three depths and
the wire, the text and the cook agree with the tool byte for byte.

**Negative control.** `tables-wire-fuzz-arm-width-negative-control` — the
arm's length check is removed from the emitter, and the fuzzer's arm-length
pass (every fixed width the closed set has, and zero) turns the leg's report
red against the engine's. `tables-wire-fuzz-arm-terminator-negative-control`
— the arm body's consumed-equals-`L` check is removed, and the fuzzer's
terminator pass, which moves the one-byte zero reference ahead of the
payload's last byte, decodes a body that ends inside its own length.

**Targets:** wire-fuzz-arm-width-negative-control wire-fuzz-arm-terminator-negative-control

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `internal/codegen/cpptable/arms.go` `tables-wire-fuzz-arm-width-negative-control` `tables-wire-fuzz-arm-terminator-negative-control` | ❌ #392 | ❌ #392 | ❌ #392 | ❌ #392 | ❌ #392 | ❌ #392 | ❌ #392 | ❌ #392 |

### M19 — A map is a sorted entry array in the holder's node extent

**Method.** A map (docs/SPEC-TABLES.md §2.8) is a LOOKUP the runtime provides
over an array of one generated `{ key, value }` ENTRY table held in ascending
key order — no new wire kind, no new framing, no new skip rule. The holder's
record carries SIXTEEN BYTES: a self-relative reference to the entry array and
an `int32` count. The ENTRIES are by-value records inside the HOLDER'S NODE
EXTENT, laid after the record's own storage, PRE-ORDER: a map's whole entry
array first, then, entry by entry in key order, the arrays of any map an
entry's value holds by value. `LoadMeasure`'s term is `N x sizeof( Entry )` at
`alignof( Entry )` AT EVERY DEPTH, summed from the FRAMING alone, because `N`
is framing and not a value. An UNREACHED non-empty map slot — a counted
array's slot past its live count — is refused by `Cook` and by `Lock`, the same
refusal §7.6 gives a pointer there. The WRITER holds the sort — `Measure`, `Save`,
`Lock` and `Cook` each derive the ascending order from the builder's entries
and nothing passes between them, so `measure == save` over a map is a real
check on two sorts agreeing. The READER trusts nothing and spends one compare
per entry: the key is read by a SCAN of the entry's field headers before the
slot is chosen, ascending against the key of the last entry that LANDED, a
duplicate resets the slot it took so last wins WHOLE, a descending key stops
the map and lets the PARENT read on, a key past the bound is skipped by its `L`
and counted `clamped`, and a key kind the reader does not declare empties the
map for ONE `kind_mismatch`. The shape to refuse is closed addressing: a node
per entry is an allocation per entry on the authoring side, a directory entry
per entry in every cook and a pointer chase per probe on the read side.

**Reference.** `internal/codegen/cpptable/maps.go` — the runtime at
`tableMapRuntime` (the sixteen-byte `TableMap` and its binary search,
`TableKeyOrder`, the builder's head and segments, the ordered cursor the four
writing walks read, the load-side fill), the extent's two walks at
`emitMapExtent` and `emitMapPack`, the framing scan at `emitMapWireExtent`, and
the reader at `emitMapReadField`. The walk's map edge is
`internal/codegen/cpptable/pointers.go`'s `edgeMap`.

**Proven in.** C++ (#380), and the TOOL's wire and text halves (#435): the
compiler's own engine sorts, writes and reads a map at three depths — a map of
maps with a keyed array and a union arm holding one included — and agrees with
the reference byte for byte on all three pinned wires and on the text round
trip. The tool's COOK half is what remains, and its surfaces refuse a
map-bearing unit by name until it lands.

**Measured effect.** Zero bytes past the entries themselves in a region and a
cook, `Open` still O(1), `Find` in place at `floor( log2 n ) + 1` key compares
with no allocation and no pointer chase, and a byte-stable image because a
sorted array of records has exactly one. The wire pays twelve bytes an entry
for the unspent kind, which is what keeps the `[..N]Pair` migration.

**Negative control.** Ten, through I2's overlay sabotage, each turning
`test/tables/maps_main.cpp` red on a CHECK: the writer emitting insertion
order, `Save` emitting a dead entry, the ascending check dropped, the duplicate
rule dropped, the key-kind rule decoding anyway, the reader clamping a key
instead of dropping its entry, the `N`-against-`L` fit check dropped,
`LoadMeasure` summing the extent at one depth only, `ToJson` writing the
entries in any order but ascending, and `Lock` writing an UNREACHED non-empty
map slot instead of refusing it.

**Targets:** maps, json-map-walk, maps-sort-negative-control, maps-dead-entry-negative-control, maps-ascending-negative-control, maps-duplicate-negative-control, maps-key-kind-negative-control, maps-clamp-negative-control, maps-fit-negative-control, maps-depth-negative-control, maps-text-order-negative-control, maps-unreached-negative-control

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `tables-maps` `tables-json-map-walk` `tables-maps-negative-controls`, and the TOOL's wire and text halves (`TestTheToolWritesTheReferencesMapBytes`) | ❌ #502 | ❌ #502 | ❌ #502 | ❌ #502 | ❌ #502 | ❌ #502 | ❌ #502 | ❌ #502 |

### M20 — The id-table wire

**Method.** A saved table is THREE PARTS (docs/SPEC-TABLES.md §3): the FORM
BYTE, the ROOT BODY, and the ID TABLE the reader finds from the END. A body is
`id reference, kind, payload` terminated by the ZERO REFERENCE, every length,
count, index and reference is one CANONICAL LEB128, and identity is
`fnv1a64( name )` at sixty-four bits for a field, an enum variant, a union arm
and a table's own name alike. The table holds every id the body used, once
each, in FIRST-USE order, and the body names them by 1-based position — so a
file that names forty ids across ten thousand fields carries forty ids and
spends one byte a header on the reference. A length whose own width moves
cannot be patched in place, so every body's length is MEASURED before it rides.
Three kinds ride with it: `30` an enum, carrying the reference to its variant
name's id; `31` the escape; `32` the payload-free arm. An ARM HEADER IS A FIELD
HEADER and carries the arm's kind. The node table rides in ONE field under the
reserved id `0xFFFFFFFFFFFFFFFF`, whose 64-bit `L` frames a numbering of any
size. The form byte is read FIRST, so a byte a reader does not know is a
REFUSAL by name and never damage, and the read report carries that verdict
beside its six counters.

**Reference.** `internal/codegen/cpptable/cpptable.go` — `putleb`/`getleb` with
the canonical check, `TableIds` (the writer's table, its capacity a
compile-time fact of the unit's closure so a save still allocates nothing),
`TableIdTable`, `TableOpen` and `TableBodyEndsEarly`; the codecs at
`internal/codegen/cpptable/codecs.go` and `arms.go`; the node table's framing at
`arena.go`. The compiler's own engine is `internal/tablewire`, written from §3
rather than from the reference, and §3.1's worked save is its golden.

**Proven in.** C++ and the tool (#435). All 46 text-carrying conformance
instances agree byte for byte between the two, and the wire fuzzer runs 103,286
enumerated mutants over 112 seeds with no divergence, plain and sanitized.

**THE MESSAGE FORM IS THIS MILESTONE's FOLLOW-ON** (docs/SPEC-TABLES.md §3.3).
Form byte `2` moves the id table one level up, to the CONNECTION, and BITPACKS
what is left: a peer announces its unit's whole vocabulary once a direction as
an ordinary form-`1` file, and every BATCH after it is a form byte, a body count
and the bodies as one continuous bit stream. What a port owes is
`LoadMessages`, `MeasureMessages` and `SaveMessages` beside the file form's
three, the message overload of `LoadMeasure` for a pointered batch's one region,
`TableVocabulary` and the three unit-scope entry points `Announce`,
`AnnounceMeasure` and `AnnounceRead`, each in that language's own naming
convention. **The verbs are PLURAL** because the form's primitive is a batch of
bodies of one root and a single message is the batch of one. C++ and the tool
carry a form-`2` path today, byte framed until the codec change lands §3.3, and
the harness's `message` surface prints ABSENT for every port, so the cell is
where the work is counted. The BODY's rules are the ones a port already has,
read off a bit stream instead of a byte one: references resolve against the
announced vocabulary instead of a trailer, elision and every tolerance rule
above are unchanged, and the two rules that DO move are named on the page,
damage being terminal for a batch and a `flags` mask riding at its declared
width.

**Measured effect.** 0.98x the corpus's bytes without its 210 KB blob and 0.99x
over the whole of it, against 1.80x over the tiny class: an empty table is ten
bytes where it was two. The win grows with the file — a pointer-heavy graph of
7,301 bytes becomes 3,319 — and a file with many distinct ids and few fields
under each pays.

**The message form's negative controls** are
`tables-message-form-negative-control`: the tail's node-table id, the
projection order the vocabulary takes, the substitution the resolved form IS,
the announcement's bound, the second announcement's refusal and the writer's
slot constant, each removed and each turning one named gate red, and the same
slot removed from the C++ EMITTER, which turns the pinned message wire red.

**Negative control.** `tables-wire-fuzz-negative-control`: the string read's
unsigned fit check, the numbering's index range check, an arm's `L` against its
kind's width, and an arm body's terminator, each removed from the emitter
through `go build -overlay` and each turning the fuzzer red on its own verdict.

**Targets:** tables-wire-fuzz, tables-wire-fuzz-negative-control, tables-message-form-negative-control, tables-vocab-schema

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `tables-wire-fuzz` `tables-wire-fuzz-negative-control` | ❌ #512 | ❌ #518 | ❌ #511 | ❌ #513 | ❌ #517 | ❌ #516 | ❌ #514 | ❌ #515 |

### I1 — The independent allocation gate

**Method.** The read path's "allocates nothing" is MEASURED, with an
instrument the emitter cannot influence: the runtime's own allocation counter
(Go `AllocsPerRun`, Java `getCurrentThreadAllocatedBytes`, a Rust counting
global allocator, C's interposed `malloc`), or where the platform has no
per-thread counter, its garbage collector observed over a steady phase against
an idle loop of the same shape (JavaScript's sampled heap at a calibrated
interval, warmed until the rate settles; Dart's scavenge count under
`--verbose_gc` with the semi-space pinned to 1 MB so one small object per
record is a collection every ~30,000 records; Elixir's per-process
`garbage_collection` trace and `binary_alloc` calls). The gate is split by
build configuration where the language ships one and develops in another:
gated under AOT, printed under the JIT. A row reported without a ceiling
cites why.

**Reference.** `test/go-tables/alloc_test.go:1-13` (why a grep proves
nothing); `test/java-tables/src/Main.java:209-223`;
`test/js-tables/main.mjs:1124-1153` (settle); `test/dart-tables/gcgate.dart`
and `DART_GC_FLAGS` in the Makefile; `test/conformance/elixir/driver_impl.ex:1032-1086`.

**Proven in.** Go; the managed forms in Java, JavaScript, Dart and Elixir.

**Measured effect.** Go: exact 0 on six paths. Java: wire read/save exactly
0, block row walk and cook read exactly 0, open one handle per file. JavaScript:
0.0 bytes per iteration on the KeyedConfig rows, 67 on RootConfig Load (a
stated ceiling of 512). Dart: 0/0/0/0/0 scavenges under AOT over 20,000 × 8
records. Elixir: heap words within ±11 of the pin against a tolerance of 64.

**Negative control.** One planted allocation per record, and LOCALIZATION:
the row it was planted in goes red and the row beside it stays green
(`tables-java-alloc-negative-control` requires wire-read red and wire-save
green; Go's `TestAllocationGateCanGoRed` plants two escapes and must see both).

**Targets:** alloc, alloc-negative-control

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ❌ #412 (the cook WRITE is counted under `tables-cook-write`; the read path is a static scan) | ✅ `tables-c-soak` `tables-c-soak-negative-control` | ✅ `tables-rust-alloc-audit` `tables-rust-alloc-negative-control` | ✅ `TestLoadAllocatesNothing` `TestRoundTripAllocatesNothing` `TestAllocationGateCanGoRed` | ❌ #412 | ✅ `tables-java-alloc` `tables-java-alloc-negative-control` | ✅ `tables-js-alloc` `tables-js-alloc-negative-control` | ✅ `tables-dart-alloc` `tables-dart-alloc-negative-control` | ✅ `tables-elixir-alloc-audit` `tables-elixir-alloc-negative-control` |

### I2 — Emitter sabotage through `go build -overlay`

**Method.** A negative control sabotages the EMITTER, not the driver and not
a generated file: a `sed` over a copy of one emitter source, checked to have
matched (a sed that silently matched nothing is a green light and not a
control), `go build -overlay` into a private compiler, the corpus regenerated
from it, and the gate required red — on the named finding, with the rows
beside it green. No tracked file is written, so an interrupt leaves no
sabotaged tree. Where a control must sabotage something else (an environment
flag in a leg, a byte of a fixture), the target says why.

**Reference.** `tables-flat-wire-negative-control`,
`tables-keyed-*-negative-control`, `tables-block-fuzz-*-negative-control` in
the Makefile; Elixir's named macro `ELIXIR_SABOTAGED_BUILD`, which four
controls share.

**Proven in.** C++.

**Measured effect.** Structural.

**Negative control.** Is one.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `tables-flat-wire-negative-control` | ✅ `conformance-negative-control-c` | ✅ `tables-rust-names-negative-control` | ✅ `tables-go-fuzz-extent-negative-control` | ✅ `conformance-negative-control-cs` | ✅ `tables-java-fuzz-negative-control` | ✅ `tables-js-accessor-negative-control` | ✅ `tables-dart-fuzz-negative-control` | ✅ `tables-elixir-alloc-negative-control` |

### I3 — The forgery fuzz oracle walks what it opened

**Method.** A mutated block or cook must REFUSE, or OPEN and be WHOLE: the
oracle re-derives every row's bounds from the descriptors, never from Open's
own arithmetic, and reads every byte of every row of anything that opened.
`SEED` and `N` are knobs, and the leg prints the seed it ran. The control
removes BOTH halves of the extent bound, because removing the rows bound alone
leaves the reader correct — the padding check downstream computes `bytes -
used` and refuses on the negative slack — and a control whose verdict depends
on which mutant the seed reaches first is not a control.

**Reference.** `test/tables/block_fuzz_main.cpp:1-25`; the both-bounds sed
`BLOCK_FUZZ_SED_CPP_extent` in the Makefile; the reason stated at
`tables-c-fuzz-negative-control` and `ELIXIR_FUZZ_SED`.

**Proven in.** C++; the both-bounds rule measured in C, Java and Elixir.

**Measured effect.** Elixir: 20,000 mutants over 7 fixtures, 9,641 opened and
walked inside the buffer, none escaped. JavaScript: 12,519 refused / 7,481
opened at the pinned seed. Rust: 409,746 mutants on the leg's long form.

**Negative control.** The both-bounds sabotage; red is required on the named
walk finding (or under ASan as a `heap-buffer-overflow` in C).

**Targets:** fuzz, fuzz-negative-control

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `tables-block-fuzz` `tables-block-fuzz-extent-negative-control` | ✅ `tables-c-fuzz` `tables-c-fuzz-negative-control` | ❌ #413 (the oracle and both-bounds sabotage exist in `tables-rust-fuzz`; nothing runs it) | ✅ `tables-go-fuzz` `tables-go-fuzz-extent-negative-control` | ✅ `tables-block-fuzz` `tables-block-fuzz-extent-negative-control` | ✅ `tables-java-fuzz` `tables-java-fuzz-negative-control` | ✅ `tables-js-fuzz` `tables-js-fuzz-negative-control` | ❌ #413 (the oracle runs; the control removes one bound) | ✅ `tables-elixir-fuzz` `tables-elixir-fuzz-negative-control` |

### I4 — Per-case absent

**Method.** A driver that cannot answer one case of a surface it otherwise
implements writes `<case>.absent` — an empty file beside where the answer
would go — and the harness counts it: `pass 16/16 +4a`. A missing FEATURE per
case, not a failing test, so a backend with no variable class still answers
the wire surface over every fixed instance. The reference leg may not answer
absent AT EITHER GRAIN — a whole surface left out of `list` or exited 2 on, or
one case — because an absence from the first driver in the registry is the
corpus losing its own expectation.

**Reference.** `test/conformance/README.md`; `test/conformance/harness/run.go:36-43`.

**Proven in.** C# (the first port with a text surface absent).

**Measured effect.** Structural; the matrix is the completion tracker.

**Negative control.** `conformance-negative-control-absent` for a case, and
`conformance-negative-control-reference-surface` for a whole surface the
reference leg never registers.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| — the reference leg may not answer absent (`test/conformance/README.md`) | ✅ `test/conformance/c/main.c:224-230` | ✅ `test/conformance/rust/src/main.rs:337-342` | ✅ `test/conformance/go/main.go:147` | ✅ `test/conformance/cs/src/Program.cs:54` | ✅ `test/conformance/java/src/Driver.java:216` | ✅ `test/conformance/js/main.mjs:74-76` | ✅ `test/conformance/dart/main.dart:337` | ✅ `test/conformance/elixir/driver_impl.ex:687` |

### I5 — The block lead gate

**Method.** Every committed block image is placed at every lead 0..64 past
an aligned base, and the reader must open at 0 and 64 and refuse 1..63 by
name — a base-alignment check that is never exercised is dead code as far as
any gate is concerned, and unaligned reads succeed on every host this repo
builds on, so the fuzz oracle alone cannot see the check go missing. The cook's
lead is held for every port by the harness's `cook_lead_1..63` forgery rows;
the block's is not (#387), so a port holds it itself.

**Reference.** `test/conformance/elixir/driver_impl.ex:1298`
(`BlockLead.run/1`: every image × every lead); the enumerated pass in the
reference fuzzer at `test/tables/block_fuzz_main.cpp:940`.

**Proven in.** Elixir (#369).

**Measured effect.** 2 block images × 65 leads — 0 and 64 open, 1..63 refuse.

**Negative control.** `tables-elixir-block-lead-negative-control` rewrites
the residue check to `lead < 0` in the emitter and requires "block_render at
lead 1 answered open, wanted refuse".

**Targets:** block-lead

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `tables-block-fuzz` (the enumerated 1..63 pass, `test/tables/block_fuzz_main.cpp:939`) | ❌ #387 (a random lead one mutant in four) | ❌ #387 (enumerated in the unreached `tables-rust-fuzz`) | ❌ #387 (a random lead; the oracle cannot see the check go missing) | ✅ `tables-block-fuzz` (`test/cs-block/src/Fuzz.cs:749-753`) | ❌ #387 (every `open` in the leg passes offset 0) | ❌ #387 (the block battery's pointer column is 0) | ❌ #387 (same) | ✅ `tables-elixir-block-lead` `tables-elixir-block-lead-negative-control` |

### I6 — Claimed names, both ways, with a control

**Method.** Every name the generated runtime spells at package or module
scope is registered in `internal/tablenames`, and a schema that declares one
is refused beside a table and kept in a table-free unit. The scan runs both
ways — every emitted name registered, every registered name emitted, because a
claim nothing needs takes a name away from every schema for free — in every
spelling the language maps to (Go's unexported package-scope names, Elixir's
unscoped one and two added, Dart's per-library privacy, JavaScript's every
module-scope binding). A negative control plants an unregistered runtime name
and requires the scan to see it.

**Reference.** `internal/tablenames/tablenames.go`;
`TestTableRuntimeNamesAreClaimed` (`compiler/tables_test.go:918`, with the
refusal loop for every language); `TestJavaRuntimeNameScanGoesRed`.

**Proven in.** C++; the control in Java.

**Measured effect.** A blind Rust scan left 44 crate items unregistered
before `tables-rust-names-negative-control` (the Makefile records it).

**Negative control.** A planted unregistered name; `go test -overlay` with
the claim loop emptied.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ❌ #414 (the scan, no control) | ❌ #414 (the scan, no control) | ✅ `tables-rust-names-negative-control` | ❌ #414 (the scan, no control) | ❌ #414 (the scan, no control) | ✅ `TestJavaRuntimeNameScanGoesRed` | ✅ `TestJsModuleScopeScanSeesEveryConvention` | ✅ `tables-dart-names-negative-control` | ✅ `TestElixirRuntimeNameCollisionRepro` |

### I7 — Cross-endian refusal as a named gate

**Method.** The two foreign surfaces (`cook-foreign`, `block-foreign`) are
the refusal every leg can answer on any host: the driver reverses the magic
and opens. Beside them, a NAMED per-language gate holds the half no harness
surface forges — a file whose magic is intact and whose order word says the
other order, exactly the file a reader leaning on one check would open — or
the register states why the platform needs none.

**Reference.** `tables-big-endian` (wire, block both ways, cook accept and
refuse, under s390x emulation); `tables-java-order`;
`test/js-tables/main.mjs:417` (`checkForeignByteOrder`, the gap named at
`:382-386`).

**Proven in.** C++ (#303); the order-word half in Java and JavaScript.

**Measured effect.** Structural.

**Negative control.** `tables-big-endian-negative`;
`conformance-negative-control-c-foreign` neuters the byte swap and requires
both foreign rows red with `cook` and `block` green.

**Targets:** order

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `tables-big-endian` | ✅ `tables-c-big-endian` `conformance-negative-control-c-foreign` | ✅ `tables-rust-big-endian` (a `cargo check` for s390x; skips cleanly without the target) | ✅ `conformance-big-endian` (the Go driver under qemu-s390x) | ✅ `tables-cook-open-cs` (the refuse half; the native big-endian half is stated unproven until a big-endian .NET exists) | ✅ `tables-java-order` | ✅ `tables-js-leg` | ❌ #415 (reads `Endian.little`; the order word is untested and no sentence says why) | — the host's order is never consulted and no platform query exists for a gate to catch; the two foreign surfaces hold it (docs/SPEC-TABLES.md) |

### I8 — A bench row is labeled a pairing check

**Method.** A bench board taken on a laptop, or on any box that was not
quiet and blessed, says so in its first line — "PAIRING CHECK, not a sitting"
— and "it certifies nothing" in its third. The row proves the leg pairs with
the reference on the same numbers; a sitting on the box is what certifies.

**Reference.** `bench/tables/results/2026-09-03-pairing-arm64-macbook.md:1-3`;
`bench/tables/legs.txt`.

**Proven in.** C++ and C# (the first board).

**Measured effect.** Structural.

**Negative control.** None; a board without the label is a review finding.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `bench/tables/results/2026-09-03-pairing-arm64-macbook.md` | ✅ `bench/tables/results/2026-09-03-pairing-c-arm64-macbook.md` | ✅ `bench/tables/results/2026-09-03-rust-inline-arm64-macbook.md` | ✅ `bench/tables/results/2026-09-03-pairing-go2-arm64-macbook.md` | ✅ `bench/tables/results/2026-09-03-pairing-arm64-macbook.md` | ✅ `bench/tables/results/2026-09-03-java-port-arm64-macbook.md` | ✅ `bench/tables/results/2026-09-03-pairing-js2-arm64-macbook.md` | ✅ `bench/tables/results/2026-09-03-pairing-dart-arm64-macbook.md` | ✅ `bench/tables/results/2026-09-03-pairing-elixir-arm64-macbook.md` |

### I9 — The soak is gated on the allocation count or a floor

**Method.** A soak that gates only on heap drift cannot see a path that
allocates and frees the same bytes every iteration: it reads +0 forever. The
soak gates on the allocation COUNT re-measured every sample (or the rate
before and after in the same process, or the live-heap and binary-memory
FLOORS, which a carrier cannot move), with drift kept beside it as the weaker
instrument that sees retention. Two seconds ride `make test`; the hour is the
release act.

**Reference.** `test/c-tables/soak_main.c:262-334` (allocator calls counted
over the measured loop); `test/go-tables/soak_test.go:216-264` (`Mallocs`
with site classification); `tables-java-soak`'s comment in the Makefile;
`test/js-tables/main.mjs:1457` (the rate before and after);
`test/conformance/elixir/driver_impl.ex:922-964` (the floors).

**Proven in.** C.

**Measured effect.** The Go soak found a lazily built descriptor (M13);
the Rust one is what made the audit's count exact.

**Negative control.** A matched `malloc`/`free` pair per iteration
(`tables-c-soak-negative-control`): the drift gate stays silent and the count
goes red. Elixir's is a RETAINING sabotage, deliberately different from the
audit's freed one, and requires both floors to rise.

**Targets:** soak

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ❌ #416 | ✅ `tables-c-soak` `tables-c-soak-negative-control` | ❌ #416 (`tables-rust-soak` gates on the count; nothing runs it) | ✅ `TestSoak` `TestSoakIdentifierCanGoRed` | ❌ #416 | ✅ `tables-java-soak` `tables-java-soak-negative-control` | ✅ `tables-js-soak` | ✅ `tables-dart-soak` `tables-dart-soak-negative-control` (correctness under reuse; the allocation gate runs inside it) | ✅ `tables-elixir-soak` `tables-elixir-soak-negative-control` |

### I10 — The zero-cost gate

**Method.** Adding a table to a unit changes not one byte of the non-table
generated code, and grows only the table, block and cook files; a table-free
unit emits none of them; no Table source carries one symbol of either
accelerator. Two forms: the byte-identity test in the compiler package and the
symbol scan over the generated tree.

**Reference.** `TestTablesMoveNoGeneratedPacketByte`
(`compiler/tables_test.go:464`); `tables-zero-cost`; `tables-block-zero-cost`.

**Proven in.** C++.

**Measured effect.** Structural.

**Negative control.** `TestZeroCostForValueOnlyTables`.

**Targets:** zero-cost

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `tables-zero-cost` `tables-block-zero-cost` `TestTablesMoveNoGeneratedPacketByte` | ✅ `tables-c-zero-cost` `TestCEmitsTableSources` | ✅ `tables-rust-features` `TestTableFreeUnitEmitsNothing` | ✅ `TestTablesMoveNoGeneratedGoByte` `TestGoEmitsTableSources` | ✅ `tables-block-zero-cost` `TestCsEmitsTableSources` | ✅ `tables-java-zero-cost` `TestJavaTablesMoveNoGeneratedPacketByte` | ✅ `TestJsEmitsTableSources` `tables-js-standalone` | ✅ `tables-block-zero-cost` `TestDartEmitsTableSources` | ✅ `TestElixirEmitsTableModules` |

### I11 — The conformance negative control sabotages the emitter

**Method.** The harness's per-leg control breaks the leg's WALK in the
emitter (I2), regenerates, and requires exactly `<lang> / json-read` red while
`json-write` and `wire` stay green — so the break is the reader's and the
matrix localizes it. A control that sabotages a copy of the driver localizes
the text form but cannot show the harness finding a defect in GENERATED code.

**Reference.** `conformance-negative-control-c` (the field index in the C
walk); `conformance-negative-control-go-walk`; `test/conformance/README.md`'s
table of controls.

**Proven in.** C#.

**Measured effect.** Structural.

**Negative control.** Is one.

**Targets:** conformance-negative-control

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ❌ #417 (`conformance-negative-control` sabotages a copy of the driver) | ✅ `conformance-negative-control-c` | ❌ #417 (no conformance control) | ✅ `conformance-negative-control-go-walk` | ✅ `conformance-negative-control-cs` | ✅ `conformance-negative-control-java` | ✅ `conformance-negative-control-js` | ✅ `conformance-negative-control-dart` | ✅ `conformance-negative-control-elixir` |

### I12 — The documented surface compiles and runs

**Method.** `docs/USAGE.md`'s example for the language is compiled and run
by a gate, so the documented surface goes red with the code rather than
drifting from it.

**Reference.** `test/tables/cook_main.cpp:1394` (`mode_usage`,
under `tables-cook-open`); `tables-dart-usage` runs the page's Dart verbatim
against the golden.

**Proven in.** C++.

**Measured effect.** The Elixir reader ran the page's example by hand and
found a module name the packet emitter refuses — the drift a gate catches.

**Negative control.** None beyond the example itself.

**Targets:** usage

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `tables-cook-open` | ❌ #418 | ❌ #418 | ❌ #418 | ✅ `tables-cook-open-cs` | ❌ #418 | ❌ #418 | ✅ `tables-dart-usage` | ❌ #418 |

### I13 — The text differential against a third implementation

**Method.** The leg emits N random instances as (wire, text); the compiler's
own Go engine (`schema unpack`, written from the spec and from neither
backend) reads the same wire and writes its text; the two texts are
byte-compared, then the other direction. Pinned goldens reach eighteen
instances; a random differential reaches the float ties they never do.

**Reference.** `tables-js-json-differential` and its control in the
Makefile; the engine in `internal/tabletext`.

**Proven in.** JavaScript.

**Measured effect.** Twelve of forty instances differed on the first run —
a float32 at `-266744.625` rendering as an eight-digit tie.

**Negative control.** `tables-js-json-differential-negative-control` restores
the magnitude tie-break and requires red (19 of 60 on the pinned seed).

**Targets:** json-differential

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ❌ #419 (a WIRE differential, `tables-flat-wire`; no text one) | ❌ #419 | ❌ #419 | ❌ #419 | ❌ #419 | ❌ #419 | ✅ `tables-js-json-differential` `tables-js-json-differential-negative-control` | ❌ #419 | ❌ #419 |

### I14 — The allocation gate refuses to certify off the pinned runtime

**Method.** The gate reads the runtime's version and, on any other than the
pinned one, sets failed and returns without measuring; an escape hatch reports
and does not certify. A newer JIT optimizes generated bodies an older one
leaves on its threshold, where a double store boxes, so a floor measured on
whatever binary a PATH lookup found says nothing about the runtime the claim
is for. A native codec's allocations are in its source and a compiler adds
none, so the native legs state that reason here rather than a pin.

**Reference.** `test/js-tables/main.mjs:1412-1430` (`PinnedNodeMajor`, the
refusal, `SCHEMA_JS_ALLOC_ANY_NODE`).

**Proven in.** JavaScript.

**Measured effect.** The allocation the gate exists to catch is invisible on
a newer V8 and steady at sixteen bytes a call on the pinned one.

**Negative control.** Running the gate on another major must refuse.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| — a native codec's allocations are in its source; the counting allocator sees the same calls under any compiler | — a native codec's allocations are in its source; the interposed allocator sees the same calls under any compiler | — a native codec's allocations are in its source; the counting global allocator sees the same calls under any toolchain | ❌ #420 (escape analysis moves between compiler versions; nothing pins it) | ❌ #420 | ❌ #420 (pinned JDK; the gate SKIPS rather than refuses without the counter) | ✅ `test/js-tables/main.mjs:1412` | ❌ #420 (pinned SDK; nothing refuses off it) | ❌ #420 (pinned OTP; the audit does not read it) |

### I15 — The tolerant wire's differential fuzzer with an independent oracle

**Method.** Every pinned wire in the corpus, mutated by enumerated passes
aimed at each number the framing carries plus a seeded random pass, read by
the language's leg on a pipe and by the compiler's own engine
(`internal/tablewire` — a third reading of §3 no backend was written from),
with four requirements per mutant: the read returns, the report matches, the
re-saved bytes match, and `LoadMeasure` never asks past what the framing
commands (docs/SPEC-TABLES.md §4.2). The mutators, the oracle and the
comparison live in the harness once; a port's leg is the thin reader
`test/conformance/README.md` states. The reference runs it plain and under
ASan/UBSan; a failure writes the mutant and prints the one command that
replays it.

**Reference.** `test/conformance/harness/wirefuzz.go`,
`test/conformance/harness/wireframe.go`, `test/tables/wire_fuzz_main.cpp`.

**Proven in.** C++.

**Measured effect.** 62,179 enumerated + 3,000,000 random mutants over 63
seeds in 89 s plain (34,490 mutants/s), 562,179 under ASan/UBSan, 0
divergences. Its sweeps caught the reference's §3.1 walk-order deviation
(#433) and six deviations in the engine, each ruled by the page.

**Negative control.** Two, through I2's overlay sabotage: the string read's
`has( len )` removed reds on the decoded value; the numbering resolve's range
check removed reds on the report.

**Targets:** wire-fuzz, wire-fuzz-negative-control

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ✅ `tables-wire-fuzz` `tables-wire-fuzz-negative-control` | ❌ #492 | ❌ #492 | ❌ #492 | ❌ #492 | ❌ #492 | ❌ #492 | ❌ #492 | ❌ #492 |

### J1 — Accessor and descriptor agreement

**Method.** The leg reads every row and every node of the block and cook
fixtures both ways — through the generated accessor and through the
descriptor — and requires agreement. Two independent derivations of one
layout, held against each other, so a moved accessor or a moved descriptor
offset is seen without a pinned dump that happens to cover the field.

**Reference.** `test/js-tables/main.mjs` (the leg's two-way read);
`tables-js-accessor-negative-control` and `tables-js-slot-negative-control`.

**Proven in.** JavaScript.

**Measured effect.** Structural.

**Negative control.** A scalar accessor moved four bytes in
`internal/codegen/jstable/record.go`, and a pointer slot's offset moved eight,
each through `go build -overlay`; each must turn the leg red.

**Targets:** accessor-negative-control

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ❌ #421 | ❌ #421 | ❌ #421 | ❌ #421 | ❌ #421 | ❌ #421 | ✅ `tables-js-accessor-negative-control` `tables-js-slot-negative-control` | ❌ #421 | ❌ #421 |

### J2 — The runtime home, with a file-order control

**Method.** A unit's shared runtime lives in one file named by the PACKAGE
(`<Package>Table.cs`, `<Package>Table.js`), never by the file that happens to
sort first, so a schema file that sorts earlier relocates nothing and the
table runtime is byte-identical across the two trees. The gate adds such a
file to a copy of the unit and requires the homes not to move; the control
puts the file-order rule back and requires them to move (#347).

**Reference.** `tables-runtime-home` and `tables-runtime-home-negative-control`
in the Makefile; `tables-js-runtime-home` and its control.

**Proven in.** C#.

**Measured effect.** Adding a file that sorted earlier relocated ~2,000
lines of correct output into a diff nobody could read (#347).

**Negative control.** The file-order rule put back.

**Targets:** runtime-home

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ❌ #422 | ❌ #422 | ❌ #422 | ❌ #422 | ✅ `tables-runtime-home` `tables-runtime-home-negative-control` | ❌ #422 | ✅ `tables-js-runtime-home` `tables-js-runtime-home-negative-control` | ❌ #422 | ❌ #422 |

### J3 — The release tier

**Method.** A port whose `make test` half must stay cheap puts its expensive
half — the hour soak, the fuzz oracle's own control, the allocation gate at
scale, a second fuzz seed — behind one `tables-<lang>-release` target that
SCALES the PR-tier instruments rather than adding new ones. `certify.yml`
derives the list from the Makefile, so a port lands its release gate by adding
the target and nothing else, and no target exists that runs nowhere.

**Reference.** `tables-java-release` (the first); the derivation in
`.github/workflows/certify.yml`.

**Proven in.** Java.

**Measured effect.** `tables-java-release` was a target that ran nowhere
before the job derived it.

**Negative control.** The certify job on a tree with no release target says
so and passes.

**Targets:** release

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ❌ #423 | ❌ #423 | ❌ #423 | ❌ #423 | ❌ #423 | ✅ `tables-java-release` | ✅ `tables-js-release` | ✅ `tables-dart-release` | ✅ `tables-elixir-release` |

### J4 — Emitted text is analyzer-clean and format-canonical

**Method.** Every generated table unit is held to what the language's
analyzer or linter accepts and what its formatter writes, as a gate — so an
emitter that drifts from the language's own conventions goes red on the
diff rather than on a reviewer.

**Reference.** `tables-dart-clean` (analyzer and `dart format
--set-exit-if-changed`); `tables-rust-clippy`.

**Proven in.** Dart.

**Measured effect.** Structural.

**Negative control.** None; the formatter is its own oracle.

**Targets:** clean

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ❌ #424 | ❌ #424 | ✅ `tables-rust-clippy` | ❌ #424 | ❌ #424 | ❌ #424 | ❌ #424 | ✅ `tables-dart-clean` | ❌ #424 (the packet units are format-checked; the table units are not) |

### J5 — The bench leg's golden gate runs before the clock

**Method.** Before any timing, the bench leg round-trips every variant of
the corpus and byte-compares against the golden; a leg whose codec does not
reproduce the corpus refuses to time it rather than posting a number.

**Reference.** `tables-elixir-bench-gate` (`leg run --gate`, all 64 variants);
the Dart leg's `WIRE GOLDEN MISMATCH` in `bench/tables/dart/leg`.

**Proven in.** Elixir.

**Measured effect.** Structural.

**Negative control.** None beyond the golden itself.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|
| ❌ #425 | ❌ #425 | ❌ #425 | ❌ #425 | ❌ #425 | ❌ #425 | ❌ #425 | ✅ `bench/tables/dart/leg` | ✅ `tables-elixir-bench-gate` |
