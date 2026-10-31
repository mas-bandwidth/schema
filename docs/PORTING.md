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

**Where a FIXED-FORM port starts.** `docs/FIXED-FORM-ALGORITHM.md` is the
language-neutral algorithm for form `3` — the layout, the framings, the record
and its writer, the one reader path, evolution, the bounds, and what a port
takes from its own packet codec and must not take from form `1`. A form-`3` leg
is written from that page and proved against the byte oracle it names, not from
the C++ reference's source. This register is still read first, for the methods.

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
`internal/codegen/cpptable/codecs.go:1113`. The limit form was
`internal/codegen/jstable/jstable.go` in JavaScript's previous-form wire, which
#760 removed (Java's, the first, went with its previous-form wire too;
schema#517 brings it back with the id-table form).

**Proven in.** C++; the limit form in Java, then JavaScript and Dart.

**Measured effect.** Exact zero on Load, Measure and Save in Go
(`testing.AllocsPerRun`); Java (`getCurrentThreadAllocatedBytes`,
`tables-java-allocator`): the block row walk and the cook read are exactly one
32-byte handle per open and 0 per row and per node, loadMeasure through a
kept Measure is exact 0 per call and per node (128 bytes for the one-argument
handle, 0 per node), save through a kept Saver allocates the output byte[]
alone (0 per node), and load is held at its measured floor because it answers
a fresh Value tree;
JavaScript
(0.0 bytes per iteration on KeyedConfig Load/Measure/Save at 300k iterations,
on the previous-form wire #760 removed; the rebuilt leg's gate measures no
Load path) and Dart (0 scavenges over 20,000 × 8 records under AOT). The instrument is
M1's second half, section I1.

**Negative control.** I1's: a planted allocation per record turns the gate
red on the one row it was planted in.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `internal/codegen/cpptable/codecs.go:1113` | ✅ `internal/codegen/ctable/codecs.go:1074` | ✅ `internal/codegen/rusttable/variablecodec.go:1110-1114` (the limit form: a nested body is read over the caller's one slice to the integer bound `sub`, into the caller's field) and `tables-rust-alloc` (0 allocator calls on the fixed form's load, measure and save into caller storage, the block open and row walk, the cook write, open and deref walk, and the variable wire's scalar-only loads; a variable-wire record with a `String` or `Vec` field, and the save that answers a `Vec<u8>`, allocate by their declared storage and are reported at their rows) | ✅ `internal/codegen/gotable/wire.go:178-187` | ✅ `internal/codegen/cstable/cstable.go:958` (a `ref struct`) | ❌ #517 — the block row walk and the cook read are 0 beyond one handle per open (`tables-java-allocator` rows 1 and 2, localized by `tables-java-alloc-negative-control`), loadMeasure through a kept Measure is exact 0 per call and per node (row 3, and 128 bytes for the one-argument Measure/Report/Reader handle, 0 per node), and save through a kept Saver allocates only the returned byte[] (row 4, 0 per node); what remains for M1 on the variable wire is load, which answers a fresh Value tree until a caller-owned-storage form exists | ❌ #516 (`tables-js-alloc` measures only the block row walk and the cook deref: no row reads a table-wire Load, Measure or Save, and the id-table `<T>Load` builds the value it returns) | ❌ #514 (closed without it) after 3.0.0: performance — the fixed form's load, measure and save into caller storage, the block row walk and the cook read hold 0 scavenges under AOT (`tables-dart-alloc`, `tables-dart-alloc-negative-control`), but the id-table wire's reader bounds a nested body with a new `TableWireReader.sub` object rather than narrowing the one reader's limit (`internal/codegen/darttable/wireform.go:1031`), and its Load answers a fresh value tree; the limit form and a load into caller storage are owed | — a decoded BEAM term is an allocation and no buffer is caller-owned (docs/SPEC-TABLES.md); the leg that pinned a per-case count went with the table wire (#515) | ❌ #2236 — card schema-goose-table-wire-load (docs/PORT-GOOSE.md, THE CARD MAP); Goose has no heap: a load's storage is data-stack arrays the caller's scope owns, freed at scope exit | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2246 — the variable-wire load answers a fresh `Record` tree from the caller's byte string (`internal/codegen/lobstertable/runtime/load.lobster`, a cursor whose limit is an int); no instrument (I1) measures a read path yet | — a load answers a tree of class instances and interned VM strings, which the VM allocates, and sq offers a script no allocation count to hold a zero to (I1) | ❌ #381 — every Luau read hands back fresh tables (the variable `L.load`, the fixed `F.read`, the batch `M.LoadMessages`), so no read is into caller-owned storage, and no allocation instrument (I1) measures the block and cook walks; the reader is a cursor over the caller's buffer, its limit an integer | ✅ `tables-lua-alloc` `tables-lua-alloc-negative-control` `TestLuaLoadNarrowsTheReaderInPlace` `TestLuaResetEqualsTheConstructor` `TestLuaWireFuzzAgreesWithTheGoEngine` `internal/codegen/luatable/runtime/load.lua:71` (the limit form: the reader is one Lua string with an integer cursor and limit, and a nested body, an arm, an array body, a map entry and a keyed slot are bounded by `narrow` and `restore` on that one reader, never a view, so the read walk builds no reader, held by a scan and its planted sub-view; Load fills caller-owned storage, `load.Load(T, name, bytes, report, state, into)` with `state` from `load.NewState()` (the readers, the id table, the queue of pointer slots, the context) and `into` the caller's value, put back to its declared defaults in place by `D.reset` (deep-equal to the constructor over every table, TestLuaResetEqualsTheConstructor) and read into, a nested table, a fixed or keyed array and a union it already holds being the storage that is read into the first time a field rides and a repeat starting again from its defaults; what a Lua load cannot read into is a counted or unbounded array, a list, a map, a union arm, a pointer's node, a wide integer and a string past 40 bytes, each a table or object the load builds, and each row says what it licenses; the 20000-mutant differential against the engine reads every mutant a second time into the value and state the one before left and must answer the same report, root and re-saved bytes, and found a clamped wide bound aliased into the value (fixed, held by the descriptor gate)) |

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
On Elixir (`TestOffsetsComposeFromHalvesElixir`, OTP 29, heap words per
call from a garbage-collection trace in a fresh process): every corpus block
open, every row of its arrays and every reference slot of three corpus cooks
allocate exactly what a same-shaped immediate-value control allocates, and a
forced-boxing control that passes the composition through a bignum allocates
more (18 words per open), so the meter is shown to see one.

**Negative control.** The JavaScript allocation gate's row for the path: a
`getBigUint64` on it reads as bytes per iteration. The Elixir control
(`tables-elixir-offsets-negative-control`) sabotages the block's composed read
back to the whole-word read; the forged-offset assertion must go red. The
cook control (`tables-elixir-cook-negative-control`) does the same to the
cook's delta read; the forged-delta assertion must go red. Both count minor
collections over 50000 refusals against an immediate-high-half twin.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `internal/codegen/cpptable/cpptable.go:513` | ✅ `internal/codegen/ctable/ctable.go:514-518` | — a Rust `u64` is a register value and nothing boxes it, so composing halves buys nothing; `u64::from_le_bytes` is byte-order-neutral by itself (M12), which is the other half of the reason (`internal/codegen/rusttable/fixedruntime.go:385`) | ✅ `internal/codegen/gotable/wire.go:152` | ✅ `internal/codegen/cstable/cstable.go:986-991` | ❌ #517 | — the accelerator paths compose (`internal/codegen/jstable/block.go:384`, `cook.go:513`); a 64-bit wire FIELD's value is a BigInt or loses precision, so those rows read one under a stated ceiling (`test/js-tables/main.mjs`, the RootConfig rows) | — an `int` is a 64-bit machine word and nothing boxes (`internal/codegen/darttable/block.go:213`) | ✅ `tables-elixir-offsets` `tables-elixir-offsets-negative-control` `tables-elixir-cook-negative-control` `TestOffsetsComposeFromHalvesElixir` (the block's `offset_of` and the cook's delta compose from 32-bit halves, refused from the high half before any bignum forms; every corpus open, row and cook slot allocates no more than an immediate-value control, against a forced-boxing control that does; a 64-bit FIELD value of 2^59 or more is a bignum on the BEAM whatever a reader does) | ❌ #2236 — card schema-goose-type-wire-read (docs/PORT-GOOSE.md, THE CARD MAP); expected moot: i64 and u64 are Goose scalars, stored unboxed in every struct field (TestGooseEveryScalarFieldIsBornAtItsIRDefault) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | — a Lobster `int` is a 64-bit two's-complement word held unboxed in every slot (docs/PORT-LOBSTER.md, THE NUMBER MODEL), and a 64-bit field is one `read_int64_le` (`internal/codegen/lobstertable/runtime/load.lobster`); nothing boxes, so there is no composed form to keep a deref allocation-free | — the sq64 integer is a 64-bit value in the object slot and nothing boxes (docs/PORT-SQUIRREL.md, 64-BIT AND 128-BIT INTEGERS) | ❌ #381 — card schema-luau-type-wire-read (docs/PORT-LUAU.md, THE CARD MAP); expected moot: a 64-bit value is the pinned Luau's primitive `integer`, and the allocation instrument (I1) has to say so | — a Lua 5.4 integer is a native unboxed 64-bit value and string.unpack reads one whole, so there is no box to compose around (docs/PORT-LUA.md) |

### M3 — A float never crosses a call

**Method.** The generated body reads the bits through the view and bit-casts
inline; only integers cross into the reader and writer. There is no `getF32`
primitive, because a double crossing a call boundary on a JIT threshold boxes,
and a generated codec must not depend on the compiler's inlining budget.

A port may take a per-language allowance from this where performance needs it
in an extreme case (Glenn, 2026-10-15), and JavaScript takes one: the block
form's and the record objects' float accessors hand the view and the offset
to the unit's ONE module-level reader per width (`TableBlockReadF32`,
`TableBlockReadF64`, `internal/codegen/jstable/record.go` jsF32ReaderSource),
which tests the raw bits (a NaN or an infinity is refused and latched on the
view, docs/SPEC-TABLES.md §3.3) and whose double comes back out of the call.
The allowance is held by its instruments, not by trust: `make tables-js-alloc`
reads the block walk at zero bytes per iteration, and `test/js-tables/main.mjs`'s
float32 oracle check proves the read moved no finite bit and refused every
non-finite pattern.

**Reference.** `internal/codegen/cpptable/cpptable.go:572-573`
(`table_bits_to_float` and its three siblings, `inline` free functions over
`get32`/`get64`); elevated to a cross-port rule at docs/SPEC-TABLES.md's
JavaScript allocation paragraph.

**Proven in.** C++; measured in JavaScript and Dart.

**Measured effect.** On one node major a float crossing a helper was steady at
sixteen bytes a call, and invisible on another — which is why the gate is
pinned to one (I14). Dart (issue 404), under the JIT on the pinned SDK 3.13.2
(linux x64), `make tables-dart-float-jit`: a steady loop writing and reading
five float32 fields (`Input`) and three float64 fields (`Vec3`) through the
generated packet codec scavenged 76 times per 2,000,000 passes with the
float32 conversion in `@pragma('vm:prefer-inline')` helpers, 76 with the
codec called from two different callers, and 0 with the conversion
hand-inlined; the float64 helpers alone scavenged 0 (the pragma held for
them, not for the float32 helpers with their NaN branch). With the conversion
emitted at each assignment all three shapes read 0. The count is taken only
between the steady window's two markers, at 2,000,000 and at 4,000,000 passes
(76 and 152 with the helpers), and only off a run proven whole: the target
refuses, by name, a Dart exit other than 0, a missing completion line, a
missing main-isolate GC trace (an absent or unsupported `--verbose_gc` is not
a zero) and a missing window marker, and before it measures it requires a
planted run that exits 3 and an untraced run to be refused.

**Negative control.** The allocation gate's wire rows: a helper put back
reads as bytes per iteration on the pinned runtime.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `internal/codegen/cpptable/cpptable.go:572-573` | ✅ `internal/codegen/ctable/ctable.go:565-568` | ✅ `internal/codegen/rusttable/variablecodec.go:913-918` `internal/codegen/rusttable/fixedform.go:706-709` (the variable codec reads the raw integer and bit-casts it at the assignment, `f32::from_bits(raw as u32)`, and its save writes `to_bits()`; the fixed form's scatter decodes the bytes at the store; the float rung's `table_widen_f32` takes the bits as a `u32`; and a Rust float is never boxed, so no call shape can cost one) | ✅ `internal/codegen/gotable/read.go` (`emitReadScalar`) | ✅ `TestCsFloatBitCastIsAtTheAssignment` (`BitConverter` at the store; helpers `[MethodImpl(AggressiveInlining)]`) | ❌ #517 | ✅ the allowance above, held by its instruments: `TestBlockFloat32AccessorCallsNoClosure` (every block and record float32 accessor calls the unit's one module-level `TableBlockReadF32`, never a closure), `tables-js-alloc` (the RenderFrame ships walk reads its float32 `thrust` per row at 0.0 bytes per iteration) and `tables-js-alloc-negative-control`, and `TestJsFloat32CellsAreBitTransparent` (`TableVariableWidenF32` is retired: the variable codec decodes the raw bits at the read); the variable codec's own walk is the generic path, held under M1 | ✅ `TestDartFloatBitsAreAtTheAssignment` (the float32 conversion inline at each assignment in the packet, block and message emitters; no helper in the output), `make tables-dart-float-jit` (0 steady JIT scavenges, 3.13.2) and `tables-dart-float-jit-negative-control` | — a BEAM float is a boxed term whatever the call shape; `R.f32_bits` costs what the term costs | — a Goose f32 or f64 is a C scalar passed by value; nothing crossing a call allocates, there being no heap | — a moo number is an unboxed Q48.16 value in every slot, and moo has no float: a float32 field is its binary32 pattern held as a number and a float64 field a frozen `Uint64`, moved by `serializeFloatBits` / `serializeDoubleBits` with no conversion (docs/PORT-MOO.md) | — a Lobster `float` is an unboxed 64-bit slot, so a float crossing a call allocates nothing; the float32 bits are read in place (`read_uint32_le` beside `read_float32_le`, `internal/codegen/lobstertable/runtime/load.lobster`) | — a SQUSEDOUBLE float is an unboxed value in the object slot, so a call costs it nothing | — a Luau number is an unboxed double in every value slot, so a float crossing a call allocates nothing; `buffer.readf32` / `readf64` read the bits in place | — a Lua number is an unboxed value across every call and PUC Lua has no JIT threshold to box at (docs/PORT-LUA.md) |

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
variable class). C: `internal/codegen/ctable/ctable.go:275-283`, pinned by
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

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `internal/codegen/cpptable/cpptable.go:358-364` `TestPointerSurfaceEmitted` | ✅ `internal/codegen/ctable/ctable.go:275-283` `TestCForceInlineStopsAtTheVariableClass` | ❌ #518 (closed without it) no `#[inline(always)]` on the read and write primitives (`get32`/`get64`, `internal/codegen/rusttable/fixedruntime.go:378`) or on the fixed-class bodies (`<t>_fixed_write_body`, `<t>_fixed_scatter`), and no stop at the variable class to hold; the `&mut` noalias half holds by construction. The directive is a hot-path change owed a measurement against the C++ arm, not an audit | ❌ #362 — after 3.0.0: performance | ✅ (primitives only) `internal/codegen/cstable/wire.go:463-659` `TestCsInliningStopsAtTheVariableClass` (`bench/results/cs-inlining-405.md`: Fixed-class body attribute reverted after measurement) | — the JIT takes no inlining directive and no aliasing qualifier; M4b is the shape | — no inlining directive and no aliasing qualifier for V8; M4b is the shape | — no inlining directive and no aliasing qualifier for the AOT compiler; M4b is the shape | — no inlining directive and no aliasing qualifier for the BEAM; M4b is the shape | ❌ #2236 — card schema-goose-type-wire-read (docs/PORT-GOOSE.md, THE CARD MAP); the generated Goose is C through `goose build -O2`; `inline fn` and asserts that feed bounds-check elimination are its levers | — no inlining directive and no aliasing qualifier in moo (the Luau compiler inlines local functions at -O2 on its own judgment); M4b is the shape | — Lobster has no inlining directive and no aliasing qualifier (the compiler inlines non-escaping function values on its own); M4b is the shape | — an interpreter with no inlining directive and no aliasing qualifier; M4b is the shape | — no inlining directive and no aliasing qualifier in Luau (its compiler inlines local functions at -O2 on its own judgment); M4b is the shape | — no inlining directive and no aliasing qualifier in PUC Lua; M4b is the shape |

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

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| — native class: the primitives are force-inlined instead (M4) | — native class: the primitives are force-inlined instead (M4) | — native class: `#[inline(always)]` on the primitives instead (M4) | ✅ `tables-go-standalone` | ✅ `tables-cs-standalone` | ✅ `tables-java-standalone` | ✅ `tables-js-standalone` | ✅ `tables-dart-standalone` `tables-dart-standalone-negative-control` | ❌ #406 | ❌ #2236 — card schema-goose-conformance-leg (docs/PORT-GOOSE.md, THE CARD MAP); the generated modules import only their own runtime modules, emitted beside them | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ✅ `TestLobsterImportsResolveInsideTheUnitOnly` (every import a generated or runtime module makes is `import .<module>` of a module the unit emits, the vendored serialize.lobster included; a decoy Wire.lobster in a modules directory beside the unit must not resolve) | ✅ `TestSquirrelLoaderResolvesOnlyItsDirectory` (a unit's module requires only the runtime emitted beside it and resolves nothing outside its directory; docs/PORT-SQUIRREL.md, THE RUNTIME LAYOUT) | ❌ #381 — card schema-luau-conformance-leg (docs/PORT-LUAU.md, THE CARD MAP); the generated modules require only their own runtime modules, emitted beside them | ✅ `TestLuaUnitStandsAlone` (every manifest unit, generated as `schema generate --lang lua` writes it, loads every module with package.path confined to the unit's directory and package.cpath empty; removing `descriptor.lua` must fail by name) and `test/conformance/lua/main.lua:147` (every conformance surface loads its unit the same way) |

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
`tables-keyed-shift-negative-control` in the reference.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `tables-keyed-none-refusal-ndebug` `tables-keyed-max-refusal-ndebug` | ✅ `TestCTableKeyedBounds` `tables-c-keyed-none-refusal-ndebug` `tables-c-keyed-max-refusal-ndebug` | ❌ #518 (closed without it) a keyed array is a plain `[T; N]` or `Vec` indexed by slot (`internal/codegen/rusttable/block.go:575`, `variablecodec.go:363`): no accessor takes the key, so the caller does the shift, and the language's own bounds check — which panics in every build — names an index, not a key, which the Method rules out as the technique | ✅ `TestKeyedRefusesNone` `TestKeyedRefusesPastMax` `TestKeyedPlacesByKey` | ✅ `TestCsKeyedAccessorCoversBothEnds` (`RefuseKey`, both ends) | ✅ `TestKeyedArrayIndexingJava` `tables-java-keyed` `tables-java-keyed-negative-control` (`v.ships(key)` / `v.ships(key, value)`, `FleetRow.shipsByKey(data, at, key)`, `FleetRow.shipsAtKey(at, key)`; `int` key, `Integer.compareUnsigned`) | ✅ `TestKeyedArrayAccessorsJavaScript` `tables-js-keyed` (`internal/codegen/jstable/keyed.go`: the variable and fixed classes' static `T.<field>At(value, key)` / `T.set<Field>At(value, key, element)` / `T.<field>Entries(value)`, the block row's `<Field>ByKey(block, key)` / `<Field>AtKey(key)`; an integer compare at both ends and a `RangeError` naming the field and the key, in every build; the test plants the None-only guard, the Max-only guard and the missing shift back) | ✅ `TestDartKeyedArraysAreHeldAtBothEnds` `tables-dart-keyed-negative-control` (Dart indexes by slot: key k rides at slot k-1, and the language's own RangeError — the typed list's bound — is the refusal) | ✅ `TestKeyedAccessorCoversBothEndsElixir` `tables-elixir-keyed` `tables-elixir-keyed-none-negative-control` `tables-elixir-keyed-max-negative-control` | ❌ #2236 — card schema-goose-vt-optionals (docs/PORT-GOOSE.md, THE CARD MAP) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2247 — `[E]T` is a plain vector indexed by slot (docs/PORT-LOBSTER.md, THE TYPE MAPPING); no accessor takes the key, so key 0 and a key past `E.Max` are not refused by name | ✅ `TestSquirrelKeyedAccessorCoversBothEnds` `TestSquirrelKeyedNegativeControls` `tables-squirrel-keyed-negative-control` (`internal/codegen/squirreltable/runtime/descriptor.nut` `keyed`, reached as the generated module's `Keyed(table, field)`: `get(array, key)` and `set(array, key, value)` refuse None, a negative or non-integer key and any key past `E.Max` by name, `keyed_refused`, in every build; `each(array, fn)` hands the key beside the element and `key(name)` is the key of a variant name; the array stays a plain array with key k at slot k-1, so a bare index still names a slot and the accessor is the one place a key is checked; three plants, the None guard removed, the past-Max guard removed and the key misplaced by one, each turn their own line), with `TestSquirrelKeyedArraysAreHeldAtBothEnds` `TestSquirrelFixedFormUnionsAndKeyedArrays` on the wire and fixed forms | ❌ #381 — a keyed array lands as a 1-based Luau array by slot (key k at index k); no accessor takes the key and refuses None or a key past `E.Max`. The fixed reader matches a slot by its key's id across versions (`TestLuauFixedVersioningNewReadsOld`, keyed_array_enum_append) | ✅ `TestLuaKeyedAccessorCoversBothEnds` `TestLuaKeyedNegativeControls` `internal/codegen/luatable/runtime/descriptor.lua:564` (`D.keyed`, reached as the generated module's `Keyed(table, field)`: `get(array, key)` and `set(array, key, value)` take one unsigned compare of `key - 1` against `E.Max`, so None, a negative or non-integer key and any key past Max are refused by name in every build, `each(array)` yields the key beside the element, and `key(name)` is the key of a variant name; the array stays a plain sequence with key k at index k, so a bare `array[key]` still answers nil at both ends and the accessor is the one place the refusal lives; three plants, the None guard removed, the past-Max guard removed and the key misplaced by one, each turn exactly their own rows) |

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

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `tables-flat-wire` `tables-flat-wire-negative-control` | ✅ `TestCTableWireGraphIdentity` `tables-c-wire-fuzz` `tables-c-wire-fuzz-negative-control` | ✅ `internal/codegen/rusttable/variable.go` (`TableNodeReader`, the flat node table; `<t>_number` the pre-order numbering with one node per `Rc`) `TestVariableClassBeyondCardConstructsRust` (graph_shared, list_shared byte for byte against `internal/tablewire`) `tables-rust-wire-fuzz-all` `tables-rust-wire-fuzz-negative-control` | ✅ `TestRegionGraphs` `tables-go-wire-fuzz` `tables-go-wire-fuzz-negative-control` | ✅ `tables-cs-leg` `tables-cs-region-fuzz` (flat native and managed graphs) | ❌ #349 the flat pointer wire round-trips (`TestVariableClassBeyondCardConstructsJava`) and the Java identity map writes a shared node once, but a cycle is not refused (its open-guard checks only un-indexed nodes and so is unreachable) and no named Java test pins either property | ✅ `tables-js-variable-class` `tables-js-variable-arms` `TestVariableClassBeyondCardConstructsJavaScript` `TestMapsAndUnionArmsOnTheTableWireJavaScript` (`internal/codegen/jstable/variableruntime.go:185`: pre-order at first visit over an identity `Map`, a cycle refused at an OPEN entry, the node table under `TableNodeTableId` at kind 12, pointers at kind 17) | ✅ `tables-dart-identity` `TestWireIdentityMapDart` `tables-dart-variable-class` `TestVariableClassBeyondCardConstructsDart` `conformance` (`internal/codegen/darttable/wireform.go:686`: pre-order at first visit through an identity map, a node two pointers reach is one record and reads back as one object, two equal nodes stay two, and a cycle is refused at an OPEN entry by name; graph_shared and list_shared byte for byte on the wire surface; the identity probe's reversed witness takes the OPEN test out and goes red on the cycle alone) | ✅ `tables-elixir-variable-class` | ❌ #2236 — card schema-goose-vt-variable-class (docs/PORT-GOOSE.md, THE CARD MAP) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ✅ `TestLobsterVariableClass` `TestLobsterVariableClassNegativeControl` (every pointered manifest unit loads, saves and re-loads over the flat node table byte for byte and report for report against `internal/tablewire`; the control removes the node-index shift, §3.1) | ✅ `TestSquirrelTableSaveRoundTripsEveryInstance` `TestSquirrelTableSaveIdentityNegativeControl` `tables-squirrel-save-identity-negative-control` `TestSquirrelBlobNamedTwice` `TestSquirrelWireFuzzAgreesWithTheGoEngine` (the node table read into class instances, and the save's identity map keyed by the instance, `internal/codegen/squirreltable/runtime/save.nut`; with the identity map forgetting a node it numbered, graph_shared and every other shared-node row turn) | ✅ `TestLuauTableSaveRoundTripsEveryInstance` `TestLuauTableSaveNegativeControl` `TestLuauLoadNodeIndexesDoNotShiftAgreesWithTheEngine` (every instance row, pointered roots included, saves byte-identical to the Go engine's corpus wire and loads back; the save's open set refuses a data cycle, `internal/codegen/luautable/runtime/schema_save.luau:390`) | ✅ `TestLuaTableSaveRoundTripsEveryInstance` `TestLuaTableSaveKeepsBlobIdentity` (`internal/codegen/luatable/runtime/save.lua:108`: one node table numbered depth-first pre-order at first visit over an identity map keyed by the Lua reference, a node still open refused as a cycle; the cycle refusal has no test of its own yet) |

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

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `internal/codegen/cpptable/block.go:120` | ✅ `internal/codegen/ctable/block.go:109-125` | ✅ `internal/codegen/rusttable/block.go:583-598` (a slice over the region) | ✅ `internal/codegen/gotable/block.go:243-256` | ✅ `internal/codegen/cstable/block.go:781-790` | ✅ `internal/codegen/javatable/block.go:420-432` | ✅ `internal/codegen/jstable/block.go:382-386` | ✅ `internal/codegen/darttable/block.go:217-247` | ✅ `internal/codegen/elixirtable/block.go:173` (`<F>_count`/`<F>_at`, and a lazy `Stream` walk) `TestElixirBlockRowsAreReachedWithoutAllocatingTheArray` | ❌ #2236 — card schema-goose-block-form (docs/PORT-GOOSE.md, THE CARD MAP) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2248 — the block reader reaches row i at `off + r * stride` but builds a `Value` per row (`internal/codegen/lobstertable/runtime/block_form.lobster`, `block_read`); no per-row-object-free accessor is emitted | ✅ `internal/codegen/squirreltable/runtime/blockbuild.nut` (`Rows`: base + offset_of, the descriptor's stride) `TestSquirrelBlockReadMatchesTheCorpus` | ❌ #381 — the block read is carried (`TestLuauBlockReadAgreesWithGo`, `TestLuauBlockBuilderMatchesGo`) but lands every row as a fresh table (`internal/codegen/luautable/runtime/schema_blockread.luau:193`): no offset accessor and no caller-owned cursor | ✅ `internal/codegen/luatable/runtime/block.lua:282` (Open keeps one `(offsetOf, count, stride)` per array and a row is `offsetOf + r * stride`, `block.lua:400`; no per-row object) `TestLuaBlockRead` |

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

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `internal/codegen/cpptable/cook.go:150-173` `tables-cook-open` | ✅ `internal/codegen/ctable/cook.go:157-178` | ✅ `internal/codegen/rusttable/cook.go:206` | ✅ `internal/codegen/gotable/cook.go:542-624` | ✅ `internal/codegen/cstable/cook.go:575-660` `tables-cook-open-cs` | ✅ `internal/codegen/javatable/cook.go:372-455` (an offset residue; the deref bounded to the record) | ✅ `internal/codegen/jstable/cook.go:438-483` | ✅ `internal/codegen/darttable/cook.go:390-446` (an offset residue) | ✅ `internal/codegen/elixirtable/cook.go:145` (a `lead` parameter; a binary has no address) | ❌ #2236 — card schema-goose-cook (docs/PORT-GOOSE.md, THE CARD MAP) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2249 — no cook yet (docs/PORT-LOBSTER.md, THE CARD MAP, schema-lobster-cook) | ✅ `internal/codegen/squirreltable/runtime/cook.nut` (`Cook`: the header words and the extent, no walk; a `lead` parameter, because a blob has no address) `TestSquirrelCookReadMatchesTheCorpus` | ✅ `internal/codegen/luautable/runtime/schema_cookread.luau` (`C.Check`: the header alone in §7.4's order, the base's residue last, nothing per node; `C.open` then reads the region into a value) | ✅ `internal/codegen/luatable/runtime/cook.lua:527` `TestLuaCookOpenIsTheHeader` (Open is the header and the base alone; Check runs cook-check's two passes) |

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

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `tables-json-walk` `tables-json-graph-walk` | ✅ `tables-c-json-walk` | ❌ #518 (closed without it) the text form is a codec per table (`<t>_json_write` and `<t>_json_read`, `internal/codegen/rusttable/variabletext.go:1014`), not one generic walk over the descriptors, so there is no unit-independent walk for a json-walk target to compare; the generic walk is a redesign of the text path, owed its own card and its own measurement | ✅ `tables-go-json-walk` | ✅ `tables-cs-json-walk` | ❌ #517 | ✅ `tables-js-json-walk` (`internal/codegen/jstable/variableform.go`, the text form and the arms' text half between the markers in every `<Package>Variable.js`; `tables-js-json-walk-negative-control`) | ❌ #514 (closed without it) after 3.0.0: performance — the text form is a codec per table (`<t>ToJson` and `<t>FromJson`, `internal/codegen/darttable/textform.go:627`), with maps, unions, fixed and keyed arrays, optionals and flags through a per-unit runtime in `internal/codegen/darttable/textmore.go`, not one generic walk over the descriptors, so there is no unit-independent walk for a json-walk target to compare; the generic walk is a redesign of the text path, owed its own card and its own measurement | ❌ #515 | ❌ #2236 — card schema-goose-text-form (docs/PORT-GOOSE.md, THE CARD MAP) | ✅ `TestMooJsonOneWalk` (`internal/codegen/mootable/json_golden_test.go`: one `internal/codegen/mootable/runtime/json.moo`, byte-identical in every unit that carries json; the modules carry three calls into it) `tables-moo-json`; control `TestMooJsonWalkNegativeControl` | ❌ #2250 — no JSON text form yet (docs/PORT-LOBSTER.md, THE CARD MAP, schema-lobster-json-form) | ✅ `TestSquirrelJsonAgreesWithTheCorpus` (one descriptor-driven walk, `internal/codegen/squirreltable/runtime/json.nut`, for every table of the unit) | ✅ `tables-luau-json-walk` `tables-luau-json-walk-negative-control` (`TestLuauJsonWalkIsOneRuntime`: the walk is one runtime module, `internal/codegen/luautable/runtime/schema_json.luau`, byte-identical to the embedded runtime in every unit that carries the text form, and no generated module defines a walk of its own; `TestLuauJsonNegativeControl` sabotages the walk's float writer) | ✅ `TestLuaTextWalkIsOneFileAcrossUnits` (`json.lua`, `textform.lua` and `textformwrite.lua` are one descriptor walk, byte-identical across every manifest unit; a planted byte in one unit's copy must be seen) |

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

Go uses one optional final writer argument, `TableWriteContext`: a
`*TableArena` carries the authoring address space and its pair, while a
`*TableAllocator` supplies scratch for a loaded/locked root. An allocator never
supplies an address space. JSON and retaining file writers take the pair
directly. C++ takes `root, out, capacity, order, allocator` for cook writing
and `root, allocator` for its measure; the builder overload takes ownership
from the builder. These are argument-order adaptations of the same contract.
The Go ownership gate returns exact original slices, including oversized ones,
injects failure at each request, and checks that packing performs two independent
numbering passes with the measurement storage released between them. Arena
segment descriptors are embedded in the arena. With caller-backed scratch on
Go 1.26.0, wire, message and retaining file measure/save each use one managed,
typed activation frame; JSON and cook measure/save use zero. Node-proportional
storage all goes through the pair. On a 64-bit target the arena occupies
163,888 bytes, including 4096 embedded segment descriptors; Shutdown requires
all readers and workers to have stopped. A typed frame contains a unit-sized
id table: 2,240 bytes for the lists unit, rather than a fixed byte cost per
object. The three ownership controls remove the
original-slice guarantee, bypass the pair, and add a managed allocation.

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `tables-hooks` | ✅ `TestCTableWireAllocatorFailures` `tables-c-soak` | — no allocating path exists: a pointered unit's wire is refused and the fixed class allocates nothing (`tables-rust-alloc`) | ✅ `tables-go-allocator` `tables-go-allocator-negative-controls` (typed activation frames stated below) | ✅ `tables-cs-leg` (`TableAllocator` pairs, growth failures and native scratch ownership) | ❌ #517 | — the runtime allocates inside itself and says so; every unavoidable allocation is named in the floor (docs/SPEC-TABLES.md) | — the runtime allocates inside itself and says so (docs/SPEC-TABLES.md) | — the BEAM allocates every term; the count is pinned instead (docs/SPEC-TABLES.md) | — Goose has no allocator to hook: storage is the compiler's data stacks, grown by a pointer bump and freed by a scope exit | — the allocator is the host's (`lua_Alloc`, set at `lua_newstate`), out of a script's reach; generated moo allocates only tables and buffers, through the VM | — memory is Lobster's compile-time reference counting over the VM's own allocator, out of a program's reach; generated Lobster allocates only classes, vectors and strings, through the VM | — the VM allocates inside itself and offers a script no allocator to hook | — the allocator is the host's (`lua_Alloc`, set at `lua_newstate`), out of a script's reach; generated Luau allocates only tables and buffers, through the VM | — the Lua VM allocates every table and string itself and a script cannot hand it an allocator; the count is pinned instead (docs/PORT-LUA.md) |

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

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `internal/codegen/cpptable/cook.go:334-336` | ✅ `internal/codegen/ctable/ctable.go:291-293` | ✅ `internal/codegen/rusttable/block.go:87-93` `TestCookLayoutIsAssertedAtCompileTime` | ✅ `internal/codegen/gotable/block.go:491-572` (at package init) | ✅ `internal/codegen/cstable/block.go:431-437` | ✅ `internal/codegen/javatable/block.go:479` | ✅ `internal/codegen/jstable/block.go:610-639` | — no second model to check against: the generated offsets ARE the model and the build version refuses a producer that disagrees (docs/SPEC-TABLES.md) | — no struct layout control, so the accelerators are read-only at the compiler's offsets and the build version is the contract (`internal/codegen/elixirtable/elixirtable.go:15-18`) | ❌ #2236 — not yet carded in the schema-goose stream (docs/PORT-GOOSE.md, THE CARD MAP); Goose structs are packed in declaration order with a specified layout (goose_spec.md Appendix C), so the assertion is expressible | — a moo table has no layout to assert; the accelerators, when built, read buffers at the compiler's offsets | — a Lobster class has no layout a program can assert; the block reader reads byte strings at the compiler's offsets, and the build version is the contract (`internal/codegen/lobstertable/runtime/block_form.lobster`) | — no struct layout to check against: a blob is read at the descriptor's offsets, which ARE the model, and the build version refuses a producer that disagrees (`internal/codegen/squirreltable/runtime/block.nut`) | — a Luau table has no layout to assert; the accelerators, when carded, read buffers at the compiler's offsets | ✅ `tables-lua-accessor-descriptor-agreement` (the Lua form: a record's layout is derived at run time from the descriptors, so there is no constant baked into generated code to assert; `B.BlockOpen` checks each array's pitch against the row's own derived size, `internal/codegen/luatable/runtime/block.lua`, and the gate holds that derivation to the compiler's, offset for offset) |

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

**Negative control.** `tables-big-endian-negative-control` puts one store back to
host order and requires red on the target while green on the host.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `internal/codegen/cpptable/cpptable.go:513-538` | ✅ `internal/codegen/ctable/ctable.go:503-518` | ✅ `internal/codegen/rusttable/block.go:105` `internal/codegen/rusttable/cook.go:174` (`TABLE_BLOCK_BYTE_ORDER` and `TABLE_COOK_BYTE_ORDER`) | ✅ `internal/codegen/gotable/wire.go:149-152` | ✅ `internal/codegen/cstable/cstable.go:971-991` | ✅ `internal/codegen/javatable/block.go:101-117` | ✅ `internal/codegen/jstable/jstable.go` (every DataView read passes `true`) | ✅ `internal/codegen/darttable/block.go` `internal/codegen/darttable/cook.go` (`Endian.little` on every read) | ✅ `internal/codegen/elixirtable/block.go:242` (every segment names `little`) | ❌ #2236 — card schema-goose-table-wire-load (docs/PORT-GOOSE.md, THE CARD MAP); reads assemble bytes with shifts, never through a host-order view | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ✅ `internal/codegen/lobstertable/runtime/load.lobster` `internal/codegen/lobstertable/runtime/block_form.lobster` (every multi-byte read and write is a `read_*_le` / `write_*_le` builtin, little-endian by definition, so no read consults the host; `block_read` refuses on the magic and then the order word) `TestLobsterBlockForm` | ✅ `TestSquirrelBlockReadMatchesTheCorpus` `TestSquirrelBlockByteOrderNegativeControl` `tables-squirrel-block-byte-order-negative-control` `TestSquirrelCookReadMatchesTheCorpus` (`internal/codegen/squirreltable/runtime/load.nut` and `block.nut`: every word composed from its bytes, little-endian; a blob's `readn` is the host's order and no reader calls it; with the block's 32-bit word composed big-endian the block gate turns) | — the pinned Luau's `buffer` library reads and writes little-endian on every host by definition, so no read can consult the host's order; the table runtime reads only through it | ✅ `internal/codegen/luatable/runtime/load.lua:149` (every `string.pack`/`string.unpack` on the table, fixed and message wires names `<`), `internal/codegen/luatable/runtime/cookwrite.lua:44` (the cook writer takes the target's order), and the block and cook read the magic in the machine's order then the order word (`block.lua:248`, `cook.lua:66`) |

### M13 — Descriptors are constants

**Method.** The reflection descriptors — field tables, type info, the block
and cook graphs — are constants in the binary or safely published finals:
read without allocation, initialized without a lazy path, and immutable. Where
a language needs a factory to break an initialization cycle, the factory is
the one exception and is named. The plain-cache idiom (read a static, build on
null, store back, no lock) is banned by name.

**Reference.** `testdata/golden/tables/examples/KeyedTable.h:2245-2249`
(`static const` inside an `inline` accessor). The safe-publication form:
`internal/codegen/javatable/rows.go` (the `BlockInfoHolder` and
`CookInfoHolder` classes), gated by `TestJavaDescriptorsAreSafelyPublished`.

**Proven in.** C++; the publication hazard found and fixed in Java.

**Measured effect.** `TestToJsonAllocatesNothing` / `TestFromJsonAllocatesNothing`
exact zero in Go; the Go soak found a lazily built descriptor once.

**Negative control.** `TestJavaDescriptorsAreSafelyPublished` fails on the
plain-cache line.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `testdata/golden/tables/examples/KeyedTable.h:2245-2249` | ✅ `internal/codegen/ctable/ctable.go:45-50` (defined in `<Base>Table.c`) | ✅ `TestRustDescriptorsAreConstants` `TestRustDescriptorsAreConstantsGoesRed` (every block, cook, view and card-registry descriptor is a plain `static`, returned by an accessor that computes nothing; the named factories are the block field's `element` and the cook field's `record` column, `fn() -> &'static …` so a record that reaches itself stays a constant table; the one `LazyLock` in the output is the fixed form's per-lineage READ PLANS, compiled from the lock and not a descriptor; the witnesses plant a OnceLock, the plain cache, a LazyLock descriptor, a computing accessor and a third factory, and each is named) | ✅ `internal/codegen/gotable/codecs.go` (`emitTableDescriptor`) | ✅ `TestCsDescriptorsAreSafelyPublished` | ✅ `TestJavaDescriptorsAreSafelyPublished` | ✅ `TestJavaScriptDescriptorsAreConstants` (`tables-js-descriptors`): block and cook descriptors are `Object.freeze`d as they are made; the variable codec and union descriptors are frozen whole by `TableVariableFreeze` once `TableVariableLink` has filled them at module load (`internal/codegen/jstable/variableform.go` `emitCodecs`), and the exported `<Name>TableType`, `<Package>UnitRegistry` and `TableRetainKnown` are frozen as they are made; the test's reversed witness drops the freeze and must find a descriptor open | ✅ `tables-dart-descriptors` `TestDartDescriptorsAreConstants` (the card registry `cardDescriptors()`, the unit view `UnitView()`, the extent scan's field and arm tables and the retained-id set are `const`, and a field's storage accessors are static tear-offs, beside the block and cook descriptors' `static const` members; a Dart top-level or static `final` is initialized lazily behind a check and a `final` collection is writable, so `const` is the form; the probe requires every registry identical across reads and unmodifiable, and its reversed witness turns the registry and the view back into finals and must see them writable; the named exception is the fixed form's layout, prefill, cover and identity tables, typed lists Dart cannot make constant, built once from const literals) | ❌ #515 | ✅ `internal/codegen/goosetable/runtime/schema_tables.goose` (every descriptor a global `let` of the runtime's descriptor types; `TestGooseTableDescriptorsLoad` holds each to a fresh record) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ✅ `TestLobsterDescriptors` `TestLobsterDescriptorsNegativeControl` (`constant_descriptor`: the emitted `<Name>Descriptor` is a `let` global built once at load and never rebuilt; Lobster has no frozen object, so the binding cannot be rebound but its fields stay assignable) | — Squirrel has no immutable table: the descriptors are tables built once when the module loads, never lazily (`internal/codegen/squirreltable/squirreltable.go`) | ✅ `internal/codegen/luautable/runtime/schema_tables.luau` (every descriptor and field entry frozen at registration; `TestLuauTableDescriptorsLoad` asserts `table.isfrozen`) | ✅ `TestLuaDescriptorsAreConstants` `TestLuaDescriptorsAreSealed` `TestLuaDescriptorsConstantNegativeControl` `TestLuaRuntimeBuildsNoDescriptorOnFirstUse` `internal/codegen/luatable/runtime/descriptor.lua:645` (the generated module ends with `D.seal`: `D.prepare` builds every derived fact once at require, a table's pointer reachability, the type-id index and the fixed form's layout trees, after which every data table the module holds carries a metatable whose `__newindex` refuses, so the banned read-a-static, build-on-nil, store-back idiom fails where it is written; Lua cannot refuse the overwrite of an existing key without a proxy on every read, so that half is a gate: every instance of every manifest unit goes through load, strict load, measure, save, JSON, text, cook and the fixed form and the whole tree's `D.fingerprint` must be unchanged. The two lazy builds the audit found, the CRC table and the fixed layout tree, are built at require. The reversed witnesses plant a lazy cache write (raises) and a value overwrite (fingerprint turns) into the load, and an overwrite at each of seven entry points to prove the gate reaches them) |

**C# Table Serialization Contract.** In C#, generated typed `<Name>Save` entry points read fields directly and use compile-time ordinal indexing: scalar leaves and child scalar arrays are typed; everything else is descriptor-driven. For those direct-read paths (scalar leaves and child scalar arrays), typed `<Name>Save` does not observe runtime mutations of TableFieldInfo / TableTypeInfo descriptors (such as getter swapping, custom defaults, guard overrides, or field id alterations). Polymorphic `TableWire.Save(object)` remains the descriptor-driven entry point that observes runtime descriptor mutations across all fields.

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

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `tables-json-graph-walk` | ✅ `TestCTableWireGraphIdentity` `TestCTableWireBlobIdentity` | ✅ `internal/codegen/rusttable/variabletext.go` (the `&node` definition and reference) `TestVariableClassBeyondCardConstructsRust` (graph_shared, list_shared, classprobe) and conformance json-read/json-write over graph_shared | ✅ `TestRegionGraphs` `tables-go-json-walk` | ✅ `tables-cs-leg` (shared graph labels and forward references) | ❌ #349 no named Java test pins the &node label (`TestVariableClassBeyondCardConstructsJava` round-trips the text against golden but does not assert the shared-node label) | ✅ `tables-js-variable-class` `TestVariableClassBeyondCardConstructsJavaScript` `TestMapsAndUnionArmsOnTheTableWireJavaScript` (`internal/codegen/jstable/variableruntime.go:1080` writes `&node` at the first appearance and the label after; `graph_shared` and `maps_pointers` read back to the oracle wire, and a label inside its own definition is refused) | ✅ `tables-dart-variable-class` `conformance` (`internal/codegen/darttable/textform.go:402`, the `_Share` that writes a shared node with `&node` at its first appearance and names it by its label after; graph_shared and list_shared json-read and json-write against the corpus text) | ❌ #349 | ❌ #2236 — card schema-goose-text-form (docs/PORT-GOOSE.md, THE CARD MAP) | ✅ `TestMooJsonWriteAndReadGates` (the graphdemo instances, `&node` defined and referenced, both directions) `TestMooJsonHostileVerdicts` (the 16 graphdemo trees) `tables-moo-json` | ❌ #2251 — no JSON text form yet (docs/PORT-LOBSTER.md, THE CARD MAP, schema-lobster-json-form) | ✅ `TestSquirrelJsonAgreesWithTheCorpus` `TestSquirrelJsonLabelNegativeControl` `tables-squirrel-json-label-negative-control` (`internal/codegen/squirreltable/runtime/json.nut` `writePointer`: graphdemo's shared nodes and forward references, read and written; with a shared node written in full at every appearance, graph_shared's json-write turns) | ✅ `TestLuauJsonAgreesWithTheCorpus` (`graph_shared` reads to the oracle wire and writes the oracle text byte for byte, `&node` at the first appearance and the label after; a label inside its own definition is refused, `read_pointer` in `internal/codegen/luautable/runtime/schema_json.luau`) | ✅ `TestLuaJsonAgreesWithTheCorpus` (`internal/codegen/luatable/runtime/json.lua:12`: a shared node is defined once as `{ "&node": N, ... }` and referenced by its label after; `graph_shared`, `list_shared` and `map_full` read and write byte for byte, and the `label-*` json-hostile rows refuse) |

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
seed's id pass, where dropping one field separated the two orders). **An
OPTIONAL over a variable-length value is the same walk's presence gate**
(schema#526): the walk skips a field whose presence companion is false before
it descends, so an absent optional's table is not an edge and a pointer left in
its slots costs no record. `test/tables/G1.schema`'s `optional_absent_no_edge`
and `optional_present_edge` are the corpus and the tool-written pins; the C
port's `internal/codegen/ctable/graph.go` `emitGraphEdge` gates the same way.
The remaining ports' walker row is schema#366.

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

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `internal/codegen/cpptable/pointers.go:150` `conformance` | ✅ `internal/codegen/ctable/graph.go:24` `TestCTableCookCanonical` `tables-c-wire-fuzz` | ✅ `internal/codegen/rusttable/variablecodec.go:527` `tables-rust-number-walk-negative-control` `TestMapsAndUnionArmsOnTheTableWireRust` `TestCookWriteRust` (Rust has no Lock, so there is no pack measure or pack; the numbering walk `<t>_number` is ONE walk in declaration order that descends by-value edges, maps and set arms in place and skips an absent optional, and the save and the cook writer (`internal/codegen/rusttable/cookwrite.go:469`) lay nodes in its list; the grouped walk, planted by overlay, turns `arms_ptr` and `maps_pointers` red on the wire; `stream_arm_first` is green on `wire`, `json-read` and `cook-write`) | ✅ `internal/codegen/gotable/regions.go` (`tableRegionEdges`, `tableNumber`) `TestAllocatorOwnershipAndStandaloneWriters` `tables-go-builders` | ✅ `tables-cs-pack-negative-control` `tables-cs-leg` (root and node extent equality) | ❌ #349 Java has only the numbering walk (`buildGraph` in `internal/codegen/javatable/variableform.go`); it has no pack-measure or pack walk to share it with (`loadMeasure`, `variableform.go:1689`, measures a load region, not a pack), so no Java test can hold numbering, pack measure and pack to one declaration-order DFS; `TestVariableClassBeyondCardConstructsJava` round-trips the wire and text only | ✅ `internal/codegen/jstable/variableruntime.go:207` (`TableVariableNumber`) `TestMapsAndUnionArmsOnTheTableWireJavaScript` `TestVariableClassBeyondCardConstructsJavaScript` (ONE edge walk in declaration order, by-value edges and set arms descended in place, numbers the nodes; `Measure` is the save's length, and the save and the cook lay nodes in that walk's list; `arms_ptr`, `maps_pointers` and `list_before_pointer` go red under the grouped walk; the OPTIONAL presence gate, schema#526: an absent `?T` or `?[N]T` is `null` and the walk returns on it before it descends, `TestOptionalVariableClosureJavaScript` against the Go oracle for save, measure, load, text and cook, red with the gate removed, and G1's four `wire` rows in `conformance`) | ✅ `internal/codegen/darttable/wireform.go:2659` `tables-dart-variable-arms` `conformance` (`<T>Number` is ONE edge walk in declaration order that descends by-value edges, map entries and set arms in place; the save lays nodes in that walk's list and the cook writer numbers through the same walk, `internal/codegen/darttable/cookwrite.go:329`; stream_arm_first and list_before_pointer on the wire, json-read and cook-write surfaces) | ❌ #349 | ❌ #2236 — card schema-goose-table-wire-save (docs/PORT-GOOSE.md, THE CARD MAP) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2252 — the save numbers the nodes in one walk (`sg_visit_edges`) and writes in another over a growing buffer with no pack measure (`internal/codegen/lobstertable/runtime/schema_save.lobster`); no gate holds the two to one skeleton | ✅ `internal/codegen/squirreltable/runtime/save.nut` (`number` once; `measure` is the save's own length) `TestSquirrelTableSaveRoundTripsEveryInstance` | ❌ #381 — the variable save is carried (`TestLuauTableSaveRoundTripsEveryInstance`); that the numbering, the measure and the pack share one walk is not yet held by a gate | ✅ `TestLuaOnePointerWalk` `TestLuaOnePointerWalkRowsAgreeWithTheEngine` `TestLuaOnePointerWalkNegativeControl` `internal/codegen/luatable/runtime/descriptor.lua:508` (ONE declaration-order walk, `D.number_nodes`, that descends every by-value edge in place; the save, the retaining save, the cook's layout and pack, and the JSON text form's reference count each call it, so the four copies the audit found (and retain.lua's, which had already lost the optional's presence gate) are gone; Measure is the length of Save, and the cook's sizing and pack share `carve_extent` with the writer nil or set; the structural test holds one definition and four callers, the rows `stream_arm_first`, `list_before_pointer` and `optional_absent_no_edge` hold the saved wire and the cook to the engine's bytes, and the grouped-walk plant turns `stream_arm_first` red on its saved wire and its cook) |

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

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `internal/codegen/cpptable/codecs.go:588` `test/tables/main.cpp:6814` `TestReportRowsDecodeThroughTheEngine` | ✅ `TestCTableWireDefaultRecovery` `tables-c-wire-fuzz` | ✅ `TestMapsAndUnionArmsOnTheTableWireRust` `internal/codegen/rusttable/mapsarms.go:159` `internal/codegen/rusttable/mapsarms.go:551` (presence is the `Option` beside the array and never the count: `None` writes nothing, `Some` rides at its live count, zero included — `opt_present`'s `o_list: []` — and a fixed array whole; load sets `Some` only after the element kind passed (`mapsarms.go:1015`), so a foreign element kind leaves the field absent; the Rust leg answers `message_trace`, both spellings at three depths, byte for byte on `wire`, `json-read`, `json-write` and `cook-write` in the conformance matrix) | ✅ `TestDefaultsAndOptionalArrays` `conformance` | ✅ `tables-cs-leg` (optional arrays across file, message and cook walks) | ❌ #392 | ✅ `tables-js-variable-arms` `TestMapsAndUnionArmsOnTheTableWireJavaScript` `TestNestedTypesAndFixedArraysOnTheTableWireJavaScript` (`internal/codegen/jstable/variablearms.go:248`: absent writes nothing and a present `?[..N]T` or `?[N]T` rides at count zero or all-zero elements, `opt_present` and `opt_trio_zero` against the oracle; a foreign element kind returns before presence is set) | ✅ `tables-dart-variable-arms` `TestMapsAndUnionArmsOnTheTableWireDart` `conformance` (presence is the optional field's null and never the count: an absent optional array writes nothing and a present one rides at count zero or as a whole fixed array; a foreign element kind puts the field back to absent, `internal/codegen/darttable/wirearrays.go:213`; the optional rows byte for byte against the oracle) | ❌ #392 | ❌ #2236 — card schema-goose-table-wire-save (docs/PORT-GOOSE.md, THE CARD MAP) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ✅ `TestLobsterTableLoadMatchesTheReports` `TestLobsterTableSaveRoundTripsEveryInstance` (a `Slot`'s `present` flag rides beside the array walk in `internal/codegen/lobstertable/runtime/load.lobster`; every optional-array instance of the manifest loads, reports and saves as the reference does) | ✅ `TestSquirrelCarriesOptionalArrays` `TestSquirrelTableSaveRoundTripsEveryInstance` | ❌ #381 — optional arrays and maps load and save against the engine (`TestLuauMapPresence`, `TestLuauOptionalVariableClosureTable`); the presence companion beside the array walks is not yet held by a negative control | ✅ `TestLuaTableSaveRoundTripsEveryInstance` (`internal/codegen/luatable/runtime/save.lua:407`: an optional array rides on `<field>_present`, never on its count, so `chain_optional_empty` rides present and empty) `TestLuaOptionalVariableClosure` |
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

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `tables-blob-span-negative-control` | ✅ `internal/codegen/ctable/arena.go:312` `TestCTableWireBlobIdentity` | — no arena and no slab: the Rust leg's storage owns every node and blob through `Rc` (a `*bytes` is an `Option<Rc<[u8]>>`, `internal/codegen/rusttable/variablecodec.go:252`), one allocation at exactly its length, so there is no slab for a large blob to outgrow and the bump-into-a-bigger-slab shape cannot arise; `blob_large`'s 66,000-byte `*bytes`, past the reference's 64 KB slab, round-trips the oracle bytes in `TestVariableClassBeyondCardConstructsRust` | ✅ `TestBlobSpanHoldsAfterLaterAllocations` `tables-go-blob-span-negative-control` | ✅ `tables-cs-leg` (large spans, descending addresses and failed blob allocation) | ❌ #259 | — no arena and no slab: every blob is its own `Uint8Array` or string at exactly its length, so there is no slab for a large blob to outgrow; the property holds by the platform, and `blob_large`'s 66,000-byte `*bytes`, past the reference's 64 KB slab, round-trips the oracle bytes in `TestVariableClassBeyondCardConstructsJavaScript` | — no arena and no slab: every blob is its own `Uint8List` or `String` at exactly its length (`internal/codegen/darttable/wireform.go:661`), so there is no slab for a large blob to outgrow; the property holds by the platform, and blob_large's 66,000-byte `*bytes` round-trips the oracle bytes on the conformance wire and cook-write surfaces | — the BEAM owns all memory; a blob lives in a managed binary and there is no caller-visible arena or address-space span (docs/SPEC-TABLES.md) | ❌ #2236 — not yet carded in the schema-goose stream (docs/PORT-GOOSE.md, THE CARD MAP); a data stack is a multi-gigabyte address-space reservation, so the span has to be measured, not assumed | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | — no caller-visible arena: a blob is one Lobster `string` the VM allocates at its exact size, so there is no slab to outgrow and no address-space span to reserve | — no arena and no slab: every blob is its own Squirrel blob or string at exactly its length, so there is no slab for a large node to outgrow | ❌ #381 — not yet carded in the schema-luau stream (docs/PORT-LUAU.md, THE CARD MAP); a Luau buffer is one allocation at most 1 GiB, so the span has to be designed | — no arena and no slab: every blob is one Lua string of exactly its length, so there is no slab for a large blob to outgrow; `blob_large`'s 66,000-byte `*bytes` rides the wire surface |


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

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `internal/codegen/cpptable/arms.go` `tables-wire-fuzz-arm-width-negative-control` `tables-wire-fuzz-arm-terminator-negative-control` | ✅ `tables-c-collections-fuzz` `tables-c-wire-fuzz` | ✅ `tables-rust-variable-arms` `TestMapsAndUnionArmsOnTheTableWireRust` `tables-rust-wire-fuzz-all` `internal/codegen/rusttable/mapsarms.go:425` (an arm header is a field header — the arm id reference, the arm's own kind, `L`, the payload — and `emitArmPayload` dispatches into the field's own body, array, string, enum and scalar emitters; a payload-free arm is kind 32 at `L = 0`; the harness's arm-length and terminator passes run over every root this leg carries, the message and stream unions included, with 0 divergences against the engine; still refused by name: a `wstring(N)` array arm) | ✅ `TestRegionUnionAndBlobValues` `TestUnionPointerArrayReaders` `tables-go-measure-negative-controls` `tables-go-wire-fuzz` | ✅ `tables-cs-leg` `conformance-negative-control-cs` (all field-shaped arms) | ❌ #392 | ✅ `tables-js-variable-arms` `TestMapsAndUnionArmsOnTheTableWireJavaScript` `TestNestedTypesAndFixedArraysOnTheTableWireJavaScript` (an arm is the field line's own descriptor under a field header, and reads through the field's array and body readers; its scalar payloads are `internal/codegen/jstable/variablearms.go:391`, the port of the tool's `encodeArm`; every arm kind in the probes, flags, `[N]T` and nested-type arms included, rides byte for byte; still refused by name: an optional arm, a pointer to a union, `wstring(N)` and string/bytes arrays) | ✅ `tables-dart-variable-arms` `TestMapsAndUnionArmsOnTheTableWireDart` `tables-dart-wire-fuzz` (`internal/codegen/darttable/wireunion.go`: an arm header is a field header — the arm id reference, the arm's own kind, `L`, then the payload through the field's own emitters; a payload-free arm is kind 32 at `L = 0`; the harness's arm-length and terminator passes run over every root this leg carries) | ❌ #392 | ❌ #2236 — card schema-goose-vt-variable-class (docs/PORT-GOOSE.md, THE CARD MAP) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ✅ `TestLobsterVariableClass` (union arms of any field type, payload-free arms and arrays of unions read through the field-line code and are held report for report to the Go engine, a retyped arm counted as a kind mismatch) | ✅ `TestSquirrelCarriesUnionsInTableClosures` `TestUnionArmRefusalsNameTheUnion` `TestSquirrelWireFuzzAgreesWithTheGoEngine` (an arm is a descriptor field line, `internal/codegen/squirreltable/squirreltable.go`) | ❌ #381 — union arms load and save as field lines against the engine (`TestLuauLoadUnionFramingAgreesWithTheEngine`) and the fixed reader matches arms by id across versions (`TestLuauFixedVersioningUnionTagBothPlans`); the wire-fuzz arm width and terminator controls are the fuzz card's | ✅ `TestLuaArmIsAFieldLine` `TestLuaArmFramingAgreesWithTheEngine` `TestLuaArmFramingNegativeControl` `tables-lua-wire-fuzz-differential` `internal/codegen/luatable/runtime/load.lua:1358` (`read_payload` reads a field's payload ONCE, and both `read_field` and `read_arm` call it: the arm's L stands in for the field's own length where the kind has one and frames the fixed width where it does not, the arm header is a field header so a retyped arm is a kind mismatch, and what a field would default or stop on the union reads as None; the structural scan holds that neither function names a per-shape reader, and a per-shape string branch planted into `read_arm` turns it; arms of `tables/arms` Carry framed hostilely, a scalar arm one byte long, a body arm with slack after its terminator, are read by the Lua load and by internal/tablewire and agree on report and re-saved bytes, and removing the scalar width check or the body's consumed-equals-L check turns exactly its own rows) |

C# carries the same field-shaped arm descriptors through file, message, JSON,
native region and cook walks. `tables-cs-leg` and the three ordinary read
fuzzer arms cover the shapes; retention has its own path-bearing walk and gate.

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
trip, and `schema cook-check`'s MAP-SLOT clause (#380): the four clauses a
list's slot takes, against `alignof( Entry )` and `count × sizeof( Entry )`,
plus the fifth a list has no analogue for — the KEYS read ascending with no
repeat, because a cook a `Find` cannot search is a forgery. The tool's COOK
and UNCOOK halves are what remains, and those two surfaces refuse a map-bearing
unit by name until they land.

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
map slot instead of refusing it. And the tool's map-slot clause has two of its
own (`tables-maps-cook-check-negative-control`): the SHARED containment test
dropped, which must turn the map's test red beside the list's, and the KEYS'
ascending test dropped, which is the map's alone and would go untested under a
shared control.

**Targets:** maps, json-map-walk, maps-sort-negative-control, maps-dead-entry-negative-control, maps-ascending-negative-control, maps-duplicate-negative-control, maps-key-kind-negative-control, maps-clamp-negative-control, maps-fit-negative-control, maps-depth-negative-control, maps-text-order-negative-control, maps-unreached-negative-control, maps-cook-check-negative-control

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `tables-maps` `tables-json-map-walk` `tables-maps-negative-controls` `tables-maps-cook-check-negative-control`, and the TOOL's wire and text halves and its cook-check clause (`TestTheToolWritesTheReferencesMapBytes`, `TestCookCheckMapSlot`) | ✅ `TestCTableWireMaps` `TestCTableMapWideningCount` `tables-c-collections-fuzz` | ❌ #502 (closed without it) the reader keeps every rule — ascending against the last key that landed, a duplicate last-wins whole, a descending key stops the map, a key past its bound skipped and counted `clamped`, a foreign key kind empties it once (`internal/codegen/rusttable/mapsarms.go:749`) — and the writer sorts at save (`mapsarms.go:348`), byte for byte in `tables-rust-variable-arms`; but a map is a `Vec` of entries, with no sixteen-byte holder record, no holder node extent and no Lock to lay one, and no test pins the writer's sort: every probe saves entries it loaded in key order, so dropping the sort stays green | ✅ `tables-go-containers` `tables-go-containers-negative-controls` `tables-go-wire-fuzz` | ✅ `tables-cs-leg` `tables-cs-wire-fuzz` `tables-cs-builder-fuzz` (stable sorted entries and native holder extents) | ❌ #502 | ✅ `tables-js-variable-arms` (`TestMapsAndUnionArmsOnTheTableWireJavaScript`, `TestNestedTypesAndFixedArraysOnTheTableWireJavaScript`: the ascending entry array, `[N]T` map values in `map_cells` and `map_pairs` included, byte for byte against the oracle; `TestMapWriterSortJavaScript`: the WRITER holds the sort, a Map built out of key order saves, measures and writes text in ascending order, with a reversed witness that drops the sort), the cook's entry arrays and holder extents through the conformance cook-write rows, `tables-js-wire-fuzz-all`. The value form is a JavaScript Map; the region (sixteen-byte holder, entries in the node extent) is the cook's. A key other than `string(N)`, `string` or an integer up to 64 bits is refused by name as §2.8 refuses it | ✅ `tables-dart-variable-arms` `tables-dart-map-sort` `TestMapWriterSortDart` `TestWireMapRepeatKeepsEarlierDart` `TestWireMapStringKeyOverBoundDart` `tables-dart-cook-map-field` `conformance` (the ascending entry array byte for byte against the oracle; the WRITER holds the sort — a Dart map field is a List of entries in the caller's order, and `_stableSorted` orders it in the save, the numbering and the text, `internal/codegen/darttable/wireunion.go:899`, held by a probe whose reversed witness drops the sort; a duplicate text key is last-wins and counted, list-map-dup-key on json-hostile) | ❌ #502 | ❌ #2236 — card schema-goose-vt-collections (docs/PORT-GOOSE.md, THE CARD MAP) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2253 — maps load and save on the variable wire (`TestLobsterCollections`), but no block or cook carries the sorted entry array in the holder's node extent | ✅ `TestSquirrelMapWriterSort` `TestSquirrelMapWriterSortNegativeControl` `tables-squirrel-map-sort-negative-control` `TestSquirrelMapsMatchTheWire` `TestSquirrelMapsInJson` `TestSquirrelMapWireCountRefusal` (the WRITER holds the sort: every mapdemo row saved with its root maps reversed is the corpus wire, and the numbering walks the same ascending order, `internal/codegen/squirreltable/runtime/save.nut` `mapWriteOrder`; with the sort removed 11 of 13 rows turn; and the mapdemo corpus unit on every surface) | ❌ #381 — maps load, save and cook-read against the engine (`TestLuauTableLoadKeyedMapAndClampRules`, `TestLuauLoadMapKeyRulesAgreeWithTheEngine`, `TestLuauCookOpenAgreesWithGo`); no `tables-luau-maps` target and none of its sort, dead-entry or ascending controls | ✅ `TestLuaTableSaveRoundTripsEveryInstance` (the writer sorts every map's keys, `internal/codegen/luatable/runtime/save.lua:163`; with the sort removed every multi-entry map row goes red, measured 2026-10-28) and the reader's ascending check, `load.lua:770` |

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
`LoadMessages`, `MeasureMessages` and `SaveMessages` beside the variable form's
three, the message overload of `LoadMeasure` for a pointered batch's one region,
`TableVocabulary` and the three unit-scope entry points `Announce`,
`AnnounceMeasure` and `AnnounceRead`, each in that language's own naming
convention. **The verbs are PLURAL** because the form's primitive is a batch of
bodies of one root and a single message is the batch of one. C++, C and the
independent tool engine carry the bitpacked form-`2` path. The C conformance
and mutation drivers exercise fixed and variable message roots. Retained
unknowns use caller-owned byte and identity stores. Replacing an occurrence
scans the retained-record log, whose size is bounded by the caller's storage.
`tables-c-retain` checks
file/message load retention and retained file saves, and
`tables-c-retain-negative-control` drops a field and fails the public report. The BODY's
rules are the ones a port already has,
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

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `tables-wire-fuzz` `tables-wire-fuzz-negative-control` | ✅ file and message forms: `tables-c-wire-fuzz`, `tables-c-wire-fuzz-negative-control`, `tables-c-message-negative-control` | ✅ `tables-rust-variable-class` `tables-rust-variable-arms` `tables-rust-variable-fixed-class` (the file form, byte for byte against the Go oracle) `tables-rust-message-batch` (the message form) `tables-rust-wire-fuzz-all` `tables-rust-wire-fuzz-negative-control`; conformance wire 97/97 and report 61/61 (#518 closed) | ✅ `tables-go-wire-fuzz` `tables-go-wire-fuzz-negative-control` `tables-go-retain-wire-fuzz` (fixed and variable file/message forms) | ✅ `tables-cs-wire-fuzz` `tables-cs-region-fuzz` `tables-cs-retain-fuzz` `conformance-negative-control-cs` | ❌ #517 | ✅ `cards-js-wire-fuzz` `cards-js-retain-fuzz` `tables-js-variable-class` `tables-js-variable-arms` `tables-js-variable-fixed-class` (the file form, byte for byte against the Go oracle) and `TestMessageFormBatchJavaScript` `TestMessageFormConformanceJavaScript` (the message form) | ✅ `tables-dart-wire-fuzz` `tables-dart-wire-fuzz-negative-control` `tables-dart-variable-class` `tables-dart-variable-arms` `tables-dart-variable-fixed-class` (the file form, byte for byte against the Go oracle) and `tables-dart-message-batch` (the message form) | ❌ #515 | ❌ #2236 — card schema-goose-table-wire-load (docs/PORT-GOOSE.md, THE CARD MAP) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ✅ `TestLobsterTableLoadMatchesTheReports` `TestLobsterTableLoadNegativeControl` `TestLobsterTableSaveRoundTripsEveryInstance` `TestLobsterTableSaveNegativeControl` (form byte, root body and the id table read from the end; every instance of the manifest byte for byte and report for report against the Go engine; the conformance `wire` and `report` surfaces) | ✅ `TestSquirrelTableLoadMatchesTheReports` `TestSquirrelMessageFormMatchesTheGoldens` `TestSquirrelMessageDepthBoundary` `TestSquirrelWireFuzzAgreesWithTheGoEngine` `conformance-negative-control-squirrel` (file and message forms) | ❌ #381 — the id-table load and save and the message form are carried (`TestLuauTableLoadRoundTripsEveryInstance`, `TestLuauTableSaveRoundTripsEveryInstance`, `TestLuauMessageFormMatchesTheGoldens`), but the batch read hands back each body as a bit range and decodes no value, and the wire-fuzz targets are the fuzz card's | ✅ `TestLuaTableSaveRoundTripsEveryInstance` `TestLuaTableLoadMatchesTheReports` (the id-table wire, every instance byte for byte against the Go oracle) and `TestLuaMessageFormMatchesTheGoldens` (`internal/codegen/luatable/runtime/save.lua`, `load.lua`) |

C# carries the complete id-table file and message surface, native regions,
mutable builders and retaining reads. Its storage choices and allocator
contracts are documented in [C# tables](CS-TABLES.md).

### M21 — A float crosses two widths by bit surgery, never by conversion

**Retiring (2026-10-27).** Floats are finite only on both wires (SPEC.md
§4.3, docs/SPEC-TABLES.md §3.3), so a NaN never reaches a cell and the
ordinary conversion is exact for every finite value. This method stays only
until each target lands the finite rule. **Method.** A float rode as its
IEEE-754 BIT PATTERN with no canonicalisation, so a backend that holds a `float32` in
a WIDER cell moves the pattern BIT FOR BIT and never through a float
conversion. The hardware conversion sets the QUIET BIT on a signalling NaN and
drops a payload the narrower cell would have kept, so a port that models a
float32 in a float64 rewrites values the wire carried — silently, on the read
side, where a byte comparison of its own writing catches nothing. Two
functions, inverse over every float32 NaN: widening takes the sign, an
all-ones exponent and the 23 payload bits into the TOP of the double's 52;
narrowing takes them back, and a payload that would round away is forced to
the quiet bit rather than to an INFINITY. Ordinary values take the ordinary
conversion, exact in both directions. The rule reaches three places in a port:
the read of kind `10`, §4's FLOAT RUNG where `10` widens into `11`, and any
storage between them that is wider than the field, such as a cook's region
slot. The text form is not one of them: JSON spells no NaN, the text writer
refuses a non-finite float and the reader counts a NaN token as a kind
mismatch (§16.2), so a NaN payload never rides through a text cell.

**Reference.** None left in the tree. The compiler's own engines
(`internal/tablewire`, `internal/tablecook`), the Go leg and the C++ leg refuse
a non-finite float on its raw bits and widen a finite one by the ordinary
conversion (2026-10-27); the C++ leg's `TableWidenF32` is deleted with the
NaN it existed for.

**Proven in.** C++ (#480). The eight ports were a row on #366; under the finite
rule a port retires the method instead of carrying it, as the Go leg, the C++ leg and the
tool have.

**Measured effect.** Nothing on the ordinary path: the branch is one mask and
one compare against an exponent word already loaded, and every finite value
takes the conversion it took before. What it buys is the byte-identity promise
holding over a class of values the corpus can now pin.

**Negative control.** `tables-float-nan-negative-control` now holds the
finite rule that retires this method: it blinds the C++ read's mask test
(`TableBitsFinite32`/`TableBitsFinite64`) through an overlay sabotage (I2),
regenerates only the F1/F2 pair, and requires the pin to go RED on the
refusal: `floats_nan.bin`, read at both widths, and a forged fixed record and
message are no longer refused as `non_finite_float`.

**Targets:** tables-float-nan, tables-float-nan-negative-control

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| — retired: the C++ leg refuses a NaN or an infinity on its raw bits and widens a finite float32 by the ordinary conversion (`tables-float-nan`, `tables-float-nan-negative-control`) | ✅ `tables-c-wire-fuzz` | ✅ `tables-rust-float-nan` `tables-rust-float-nan-negative-control` (the Rust wire has not landed the finite rule — `tables-rust-wire-fuzz-all` runs `--finite-owed` — so the method binds: `table_widen_f32` and `table_message_widen_f32`, cut from the generated runtimes as emitted, keep every float32 NaN's payload and quiet bit and convert every other value exactly, and the codec's kind-10 rung into a float64 cell goes through the first; the control puts the hardware conversion back and reads `0x7ffc000020000000` for `0x7ff4000020000000`; the fixed form's surgery is held by t04R21) | — retired: the Go leg refuses a NaN or an infinity on its raw bits and widens a finite float32 by the ordinary conversion (`TestWireSignalingNaNWideningAndLEBOverflow`, `TestGoPlainFloatSpecialValuesAgreeAcrossForms`) | ✅ `tables-cs-leg` (integer-only float widening preserves NaN payload bits) | ❌ #366 | ✅ `TestJsFloat32CellsAreBitTransparent` (the block accessor, the form-3 decode and write, and the f32-to-f64 rung; its node half runs under SCHEMA_SLOW=1) | — retired: the Dart leg refuses a NaN or an infinity on its raw bits and widens a finite float32 by the ordinary conversion (`tables-dart-finite-float`, `tables-dart-finite-float-negative-control`, `TestDartFloat32CellsAreBitTransparent`) | ❌ #366 | ❌ #2236 — card schema-goose-serialize-vendor (docs/PORT-GOOSE.md, THE CARD MAP); a float's bits move by the one route serialize-goose fixes (the C sidecar's memcpy), never by arithmetic (docs/PORT-GOOSE.md, FLOAT32) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | — retired: the lobster leg is finite only, a NaN or an infinity is refused on its raw bits in every form (`TestLobsterTablesAreFiniteOnly`, `TestLobsterTablesAreFiniteOnlyNegativeControl`) | — retired: the Squirrel leg is finite only, a NaN or an infinity is refused on its raw bits in every form (`TestSquirrelTableFloatsAreFinite`, `TestSquirrelTableFiniteFloatForms`, `TestSquirrelTableFiniteFloatNegativeControl`, `TestSquirrelTableFiniteFloatFormsNegativeControl`); a finite float32 still widens by bit surgery, `internal/codegen/squirreltable/runtime/load.nut` `widenF32` | — retired: the Luau leg refuses a NaN or an infinity on its raw bits and widens a finite float32 by the ordinary conversion (`TestLuauTablesAreFiniteOnly`, `TestLuauPlainFloatSpecialValues`) | — retired: the Lua leg refuses a NaN or an infinity on its raw bits in every read (variable, retained, message, fixed, cook check and uncook, block dump) and every writer refuses one; a finite float32 widens by the ordinary conversion (`tables-lua-finite-float`, `tables-lua-finite-float-negative-control`), and the conversion moves no bit of a finite pattern on the pinned interpreter (`TestLuaFloat32ConversionIsExactForEveryFinitePattern`: every exponent of both signs with the mantissa corners, the subnormals at a stride and two hundred thousand pseudo-random patterns, bits to double to bits) |

### M22 — Typed table serialization fast path

**Method.** Fixed tables emit typed save entry points and bodies (`<Name>Save`, `<Name>CollectTyped`, `<Name>BodySizeTyped`, `<Name>WriteBodyTyped`) where scalar leaves and child scalar arrays run with direct typed field reads, compile-time ordinal indexing, and pre-sized buffer operations without dynamic reflection or descriptor delegate dispatches, while everything else remains descriptor-driven. In C#, generated typed `<Name>Save` entry points read fields directly and use compile-time ordinal indexing (scalar leaves and child scalar arrays are typed; everything else is descriptor-driven), and therefore do not observe runtime mutations of TableFieldInfo / TableTypeInfo descriptors for those fields. Polymorphic `TableWire.Save(object)` remains the descriptor-driven entry point that observes runtime descriptor mutations across all fields.

**Reference.** `internal/codegen/cstable/wire.go`

**Proven in.** C# (#814, #816), Go (#795, #803), C (#775), C++ (#775).

**Measured effect.** Bypasses descriptor delegate dispatches and hash probes on repeat lookups (committed receipt: `bench/tables/results/2026-09-08-csharp-vocabulary-index-arm64`; official post-merge sitting will record the sealed baseline).

**Negative control.** `TestCsRefAt` and `TestCsTypedVsDynamicWireEquivalence`.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `internal/codegen/cpptable/codecs.go` | ✅ `internal/codegen/ctable/wire.go` | ✅ `internal/codegen/rusttable/fixedform.go:405` `internal/codegen/rusttable/variablecodec.go:612` `tables-rust-fixedform` `tables-rust-variable-fixed-class` (every save is typed per table: the fixed form's `<t>_fixed_write_body` stores each field at its constant offset and the variable form's `<t>_save_body` reads each field directly; Rust emits no descriptor-driven save at all, so there is no dispatch on the save path and no runtime descriptor for it to observe) | ✅ `internal/codegen/gotable/wire.go` | ✅ `internal/codegen/cstable/wire.go` | ❌ #517 | ❌ #516 (every id-table `<T>Save` is the descriptor walk TableVariableSave; only the form-3 `<T>FixedWriteBody` is typed) | ✅ `internal/codegen/darttable/wireform.go:1777` `internal/codegen/darttable/fixeddart.go:283` `tables-dart-variable-class` `tables-dart-fixed-form` (every save is typed per table: the id-table form's save body writes each field through its own emitted statement with a direct field read, and the fixed form stores each field at its constant offset; no save goes through the descriptors) | ❌ #515 | ❌ #2236 — card schema-goose-fixed-form (docs/PORT-GOOSE.md, THE CARD MAP) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2255 — every variable-wire save is the descriptor walk (`internal/codegen/lobstertable/runtime/schema_save.lobster`); only the form-3 `<Name>FixedSave` is typed (`internal/codegen/lobstertable/fixedform.go`) | ❌ #366 (every `<T>Save` is the descriptor walk in `internal/codegen/squirreltable/runtime/save.nut`; only the form-3 `<T>FixedWriteBody` is typed) | ❌ #381 — the fixed form is one descriptor-driven reader and writer over the layouts (`tables-luau-fixed-form`, `tables-luau-versioning`); no typed per-table save or body entry point | ✅ `TestLuaTypedSaveEqualsDescriptorSave` `TestLuaTypedSaveNegativeControl` `TestLuaTypedSaveRunsFewerInstructions` `internal/codegen/luatable/typedsave.go` (every unit's module carries `TypedBodies[<Name>]` per table and `SaveTyped<Name>(value)` per declared table, run by `save.SaveTyped`: the scalar leaves, bool, integer, float and string fields with no guard, presence or `required`, are written straight from the value with their id, kind, width and default as constants in the body and the field header as one call, and every other field of the table goes through the descriptor walk's `write_field` at its constant ordinal, so the body is the walk with its scalar part unrolled; `save.Save` stays the descriptor-driven entry; the equivalence holds the two to the same bytes and the same refusals over every manifest instance, every table at its defaults, every scalar leaf moved off its default in turn and a NaN float; three plants into the typed bodies, a wrong string kind, a wrong integer width and the default elision against a wrong value, each turn it red; and with no clock, the Lua VM's own instruction count over the same saves is 64% of the walk's) |

### I1 — The independent allocation gate

**Method.** The read path's "allocates nothing" is MEASURED, with an
instrument the emitter cannot influence: the runtime's own allocation counter
(Go `AllocsPerRun`, Java `getCurrentThreadAllocatedBytes`, a Rust counting
global allocator, C++ and C interposed `operator new`/`malloc`), or where the
platform has no
per-thread counter, its garbage collector observed over a steady phase against
an idle loop of the same shape (JavaScript's sampled heap at a calibrated
interval, warmed until the rate settles; Dart's scavenge count under
`--verbose_gc` with the semi-space pinned to 1 MB so one small object per
record is a collection every ~30,000 records). The gate is split by
build configuration where the language ships one and develops in another:
gated under AOT, printed under the JIT. A row reported without a ceiling
cites why.

**Reference.** `test/go-tables/alloc_test.go:1-13` (why a grep proves
nothing); `test/java-tables/src/AllocPaths.java` (settle, floor, rows);
`test/js-tables/main.mjs:1124-1153` (settle); `test/dart-tables/gcgate.dart`
and `DART_GC_FLAGS` in the Makefile; C++'s counting `operator new` and
`test_allocation_audit` in `test/tables/main.cpp` (with `tables-cpp-alloc`).

**Proven in.** Go and C++; the managed forms in Java, JavaScript and Dart.

**Measured effect.** Go: exact 0 on six paths. Java (Temurin 21.0.12.1): block row walk
and cook read 0 per row and per node beyond one 32-byte handle per open; over
six committed variable wires, loadMeasure through a kept Measure is exact 0
bytes a call (128 bytes a call for the one-argument handle, 0 per node), save
through a kept Saver is the output byte[] alone (104 to 3336 bytes a call, 0
per node), and load is 536 to 46400 bytes a call (the fresh Value tree). JavaScript:
0.0 bytes per iteration on the KeyedConfig rows, 67 on RootConfig Load (a
stated ceiling of 512). Dart: 0/0/0/0/0 scavenges under AOT over 20,000 × 8
records. C++: 0 allocations over Load, the indexed and iterated reads,
Measure and Save on the root.

**Negative control.** One planted allocation per record, and LOCALIZATION:
the row it was planted in goes red and the row beside it stays green
(Java's `tables-java-alloc-negative-control` plants one allocation per row in
the block walk and requires that row red and the cook walk green; Go's
`TestAllocationGateCanGoRed` plants two escapes and must see both; C++'s
`test_allocation_audit_can_go_red` requires the counting `operator new` to see
a planted allocation).

**Targets:** alloc, alloc-negative-control

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `tables-cpp-alloc` (a counting global `operator new` over Load, the indexed/iterated reads, Measure and Save on the root; #562's `tables-lists` and #615/#679's `tables-retain` hold the list and retain paths, and the cook WRITE is under `tables-cook-write`) | ✅ `tables-c-soak` `tables-c-soak-negative-control` | ✅ `tables-rust-alloc` `tables-rust-alloc-negative-control` (`test/rust-alloc/src/main.rs`: a counting `#[global_allocator]`, 15 rows of 1000 calls, every gated row exact 0; the reported rows are the variable wire's TeamConfig load, 1 a call for its `banner: String`, RootConfig load, 2 a call for its `String` and `Vec`, and save, 5 a call for the `Vec<u8>` it answers and its id table; the control plants one Box per walked block row and requires that row red at exactly 192 a call and every other row green) | ✅ `TestLoadAllocatesNothing` `TestRoundTripAllocatesNothing` `TestAllocationGateCanGoRed` | ✅ `tables-cs-alloc` | ✅ `tables-java-allocator` `tables-java-alloc-negative-control` (`test/java-tables/src/AllocPaths.java`: block walk and cook read 0 per row and per node beyond one handle; variable loadMeasure exact 0 per call and per node through a kept Measure, save the output byte[] alone with 0 per node through a kept Saver, and load at its measured Value-tree floor under #517) | ✅ `tables-js-alloc` `tables-js-alloc-negative-control` | ✅ `tables-dart-alloc` `tables-dart-alloc-negative-control` | ✅ `tables-elixir-alloc-audit` `tables-elixir-alloc-negative-control` — a PINNED BUDGET per case, not a zero floor (a decoded BEAM term is an allocation): 23 `instance` cases of the variable wire, one iteration a wire load, save, text read and text write, averaged over 200; the ceiling is the pinned heap words + 64 + 1/10000 (per process, `:erlang.trace/3` on `:garbage_collection`) and the pinned refc binary calls + 8 (VM-wide `binary_alloc`), over `test/conformance/elixir/alloc-budget.txt`; the control's sixteen refc binaries and thousand-cell list per load red both columns of every case | ❌ #2236 — not yet carded in the schema-goose stream (docs/PORT-GOOSE.md, THE CARD MAP); Goose has no heap, so the instrument counts data-stack growth instead; to be designed | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2256 — no allocation instrument yet (docs/PORT-LOBSTER.md, THE CARD MAP, schema-lobster-bench) | — no allocation count: sq offers a script none, so no gate can read one | ❌ #381 — not yet carded in the schema-luau stream (docs/PORT-LUAU.md, THE CARD MAP); the allocation instrument | ✅ `tables-lua-alloc` `tables-lua-alloc-negative-control` (`collectgarbage("count")` with the collector stopped over a steady phase, after a collection and a warm-up that re-interns the strings: seven rows on the pinned Lua, the by-value rows (keyed arrays and strings, scalars and strings, nested records, a variable root that reads nothing) held to EXACT ZERO bytes a load, the rows that license a table per counted element and union arm (RootConfig, 3744 against 4096), per node and slot (Scene, 3440 against 3840) and per list element (1440 against 1536) to a pinned ceiling; one planted allocation per queued pointer slot turns exactly the pointer and the list rows and one per array body turns every row that reads an array and neither the pointer row nor the empty root) |

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
the Makefile; Elixir's named macro `ELIXIR_SABOTAGED_BUILD`, which its two
reader controls share.

**Proven in.** C++.

**Measured effect.** Structural.

**Negative control.** Is one.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `tables-flat-wire-negative-control` | ✅ `conformance-negative-control-c` | ✅ `tables-rust-names-negative-control` | ✅ `tables-go-fuzz-extent-negative-control` | ✅ `conformance-negative-control-cs` | ✅ `tables-java-fuzz-negative-control` | ✅ `tables-js-accessor-negative-control` | ✅ `tables-dart-fuzz-negative-control` | ✅ `tables-elixir-fuzz-negative-control` `tables-elixir-block-lead-negative-control` (`ELIXIR_SABOTAGED_BUILD`) | ✅ `tables-goose-strict-emitter-negative-control` (fixedform.go's `import .fixed;` dropped through `go build -overlay`; the strict gate must red exactly the Table modules that name the fixed runtime) `tables-goose-runtime-home-negative-control` `packet-utf8-goose-negative-control` `packet-wide-goose-negative-control` `packet-defaults-goose-negative-control` `packet-finite-goose-negative-control` (make/goose-packet.mk: each a named `tools/sabotage` edit of one Goose emitter, `go build -overlay` into a private compiler, the fixture regenerated and the gate required red on exactly its rows; the finite controls diff the failing vectors against `test/packet-finite/goose/read.expected` and `test/packet-finite/goose/write.expected`) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2257 — the Lobster controls patch the emitted runtime in a scratch unit (`TestLobsterTableLoadNegativeControl`), not the emitter through `go build -overlay` | ✅ `conformance-negative-control-squirrel` `conformance-negative-control-squirrel-walk` `conformance-negative-control-squirrel-block` `conformance-negative-control-squirrel-retain` | ❌ #381 — card schema-luau-conformance-leg (docs/PORT-LUAU.md, THE CARD MAP) | ✅ `conformance-negative-control-lua` `conformance-negative-control-lua-walk` (`test/conformance/lua/negative-control`: the embedded runtime sedded into a `go build -overlay` compiler) |

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

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `tables-block-fuzz` `tables-block-fuzz-extent-negative-control` | ✅ `tables-c-fuzz` `tables-c-fuzz-negative-control` | ✅ `tables-rust-fuzz` (in `make test-rust`: 409,746 mutants, 89,545 opened, none read outside the extent) `tables-block-fuzz-extent-negative-control` (reds the Rust leg on the oracle) | ✅ `tables-go-fuzz` `tables-go-fuzz-extent-negative-control` | ✅ `tables-block-fuzz` `tables-block-fuzz-extent-negative-control` | ✅ `tables-java-fuzz` `tables-java-fuzz-negative-control` | ✅ `tables-js-fuzz` `tables-js-fuzz-negative-control` | ✅ `tables-dart-fuzz` `tables-dart-fuzz-negative-control` | ✅ `tables-elixir-fuzz` `tables-elixir-fuzz-negative-control` | ❌ #2236 — card schema-goose-fuzz-differential (docs/PORT-GOOSE.md, THE CARD MAP) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2258 — no forgery fuzz leg yet (docs/PORT-LOBSTER.md, THE CARD MAP, schema-lobster-fuzz-differential); `block_read` checks only the magic, the build version and the byte order | ❌ #413 (the block and cook forgery batteries ride the conformance surfaces; no forgery fuzzer walks what it opened) | ❌ #381 — card schema-luau-fuzz-differential (docs/PORT-LUAU.md, THE CARD MAP) | ✅ `tables-lua-fuzz` `tables-lua-fuzz-negative-control` (`internal/codegen/luatable/forgeryfuzz_test.go`: a mutated block must refuse or open whole against the compiler's layout, a mutated cook must agree with the tool's cook-check, and a full walk of what opened must read inside the bytes; a `string.unpack` past the end raises, which is the bounds instrument. 24,000 mutants over 2 blocks and 4 cooks at the pinned seed `0x4c5541464f524745`: 15,982 refused, 8,018 opened and walked whole, 0 findings; `LUA_FORGERY_SEED` and `LUA_FORGERY_N` are the knobs. The control removes BOTH halves of the block's extent bound (2,809 findings) and, separately, the cook Check's region scan (289). Its first run found `Check` opening a cook whose alignment word was forged to 64, which the tool refuses: fixed in `internal/codegen/luatable/runtime/cook.lua`) |

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

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| — the reference leg may not answer absent (`test/conformance/README.md`) | ✅ `test/conformance/c/main.c:224-230` | ✅ `test/conformance/rust/src/main.rs:337-342` | ✅ `test/conformance/go/main.go:147` | ✅ `test/conformance/cs/src/Program.cs:54` | ✅ `test/conformance/java/src/Driver.java:646-649` (the five wire surfaces, absent at the `list` grain) | ✅ `test/conformance/js/main.mjs:74-76` | ✅ `test/conformance/dart/main.dart:337` | ❌ #515 | ✅ `test/conformance/goose/driver` `test/conformance/goose/main.goose` (a `<case> absent` line becomes `<case>.absent`; no surface answers yet: every harness surface reads a form the port does not carry, and the fixed form is judged by `make test-goose` against build/fixedform-corpus) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ✅ `test/conformance/lobster/driver` (a `<case> absent` line becomes `<case>.absent`; a refused unit answers every case absent) | ✅ `conformance-negative-control-squirrel-absent` (`test/conformance/squirrel/main.nut:72` `writeAbsent`: a refused unit's cases, and blob_shared's named writer refusal; the control writes those answers under another name and requires the wire surface red) | ✅ `test/conformance/luau/driver` (a `<case> absent` line becomes `<case>.absent`; no surface answers yet) | ✅ `test/conformance/lua/main.lua:94` (every case it cannot answer is <case>.absent with its reason) |

### I5 — The block lead gate

**Method.** Every committed block image is placed at every lead 0..64 past
an aligned base, and the reader must open at 0 and 64 and refuse 1..63 by
name — a base-alignment check that is never exercised is dead code as far as
any gate is concerned, and unaligned reads succeed on every host this repo
builds on, so the fuzz oracle alone cannot see the check go missing. The cook's
lead is held for every port by the harness's `cook_lead_1..63` forgery rows;
the block's is held by `block_base_unaligned` (#387), one image one byte past an
aligned base, so the reference holds the check and a port still holds it itself.

**Reference.** `testdata/conformance/tables/MANIFEST.txt`
(`block_base_unaligned`); `test/conformance/cpp/main.cpp` (the pointer column
of the block forgery battery); `test/conformance/elixir/driver_impl.ex:740-741`
(`BlockLead.run/1`: every image × every lead); the enumerated pass in the
reference fuzzer at `test/tables/block_fuzz_main.cpp:940`.

**Proven in.** Elixir (#369), JavaScript (`test/conformance/js/main.mjs` `block-lead`); the reference row in every leg (#387).

**Measured effect.** 2 block images × 65 leads — 0 and 64 open, 1..63 refuse.

**Negative control.** `tables-elixir-block-lead-negative-control` rewrites
the residue check to `lead < 0` in the emitter and requires "block_render at
lead 1 answered open, wanted refuse".

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `block_base_unaligned` (`testdata/conformance/tables/MANIFEST.txt`) and `tables-block-fuzz` (the enumerated 1..63 pass, `test/tables/block_fuzz_main.cpp:939`) | ✅ `block_base_unaligned` and `tables-c-fuzz` (the enumerated 1..63 pass, `test/c-tables/fuzz_main.c:165`) | ✅ `block_base_unaligned` (`test/conformance/rust/src/main.rs`) | ✅ `block_base_unaligned` and `tables-go-fuzz` (the enumerated 1..63 pass, `test/go-tables/fuzz_test.go:205`) | ✅ `block_base_unaligned` (`test/conformance/cs/src/Program.cs`) and `tables-block-fuzz` (`test/cs-block/src/Fuzz.cs:749-753`) | ✅ `block_base_unaligned` (`test/conformance/java/src/Driver.java`, `openBlock` takes the lead) and `tables-java-block-lead` `tables-java-block-lead-negative-control` | ✅ `block_base_unaligned` (`test/conformance/js/main.mjs`, `openBlock` takes the lead) and `tables-js-block-lead` `tables-js-block-lead-negative-control` | ✅ `block_base_unaligned` (`test/conformance/dart/main.dart`) and `tables-dart-fuzz` (the enumerated 1..63 pass, `test/dart-tables/fuzz.dart`) `tables-dart-fuzz-lead-negative-control` | ✅ `tables-elixir-block-lead` `tables-elixir-block-lead-negative-control` | ❌ #2236 — card schema-goose-block-form (docs/PORT-GOOSE.md, THE CARD MAP) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2259 — `block_read` checks the magic, the build version and the byte order only; no base-alignment check for the lead gate to hold | ✅ `block_base_unaligned` (`test/conformance/squirrel/main.nut`, `accelOpen` passes the lead) `TestSquirrelBlockReadMatchesTheCorpus` (an unaligned lead refused by name) | ❌ #381 — not yet carded in the schema-luau stream (docs/PORT-LUAU.md, THE CARD MAP); the block lead gate | ✅ `block_base_unaligned` (`test/conformance/lua/main.lua:471`, BlockOpen takes the lead) and `TestLuaCookOpenIsTheHeader` (the cook at leads 1, 7 and one alignment) |

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
refusal loop for every language); `TestJavaRuntimeNameScanGoesRed`;
`TestGoRuntimeNameScanGoesRed`.

**Proven in.** C++; the control in Java.

**Measured effect.** A blind Rust scan left 44 crate items unregistered
before `tables-rust-names-negative-control` (the Makefile records it).

**Negative control.** A planted unregistered name; `go test -overlay` with
the claim loop emptied.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `TestCppTableRuntimeNamesAreClaimed` (both ways over the Table and Block sources, 624 names registered for the Cpp backend) `TestCppRuntimeNameScanGoesRed` `tables-cpp-names-negative-control`; the packet emitter's fixed vocabulary is `TestCppPacketRuntimeNamesAreAccountedFor` (its unclaimed macros and helpers are a recorded ceiling in `compiler/tablescppnames_test.go`) | ✅ `TestCTableRuntimeNameScanGoesRed` | ✅ `tables-rust-names-negative-control` | ✅ `TestGoRuntimeNameScanGoesRed` | ✅ `TestCsRuntimeNameScanGoesRed` | ✅ `TestJavaRuntimeNameScanGoesRed` | ✅ `TestJsModuleScopeScanSeesEveryConvention` | ✅ `tables-dart-names-negative-control` | ✅ `TestElixirRuntimeNameCollisionRepro` | ❌ #2236 — card schema-goose-vt-strict-and-refusals (docs/PORT-GOOSE.md, THE CARD MAP) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2260 — the runtime's names live in `namespace schema_rt` and its module names, both refused by name (`TestLobsterRefusesByName`), but they are not registered in `internal/tablenames` nor scanned both ways | ❌ #366 (no claimed-name scan of the emitted modules and no control yet) | ❌ #381 — card schema-luau-refusals-by-name (docs/PORT-LUAU.md, THE CARD MAP) | ✅ `TestLuaTableRuntimeNamesAreClaimed` `TestLuaRuntimeFileNamesAreClaimed` `TestLuaRuntimeNameScanGoesRed` `tables-lua-names` `tables-lua-names-negative-control` (a Lua runtime spells everything it shares as a key of its returned module table, so the scan reads every such key out of the emitted text, in `function M.k(`, `function M:k(` and `M.k =` spellings, for every unit of the corpus, and holds it to `internal/tablenames/lua.go` both ways; the one claimable Lua spelling is a runtime module's FILE name, refused beside a table and kept in a table-free unit. The control plants an unregistered key in each convention and a registered name nothing emits) |

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

**Negative control.** `tables-big-endian-negative-control`;
`tables-c-big-endian-negative-control`;
`tables-c-little-endian-negative-control`;
`conformance-negative-control-c-foreign` neuters the byte swap and requires
both foreign rows red with `cook` and `block` green.

**Targets:** order

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `tables-big-endian-tables` `tables-big-endian-collections` | ✅ `tables-c-big-endian` `conformance-negative-control-c-foreign` | ✅ `tables-rust-big-endian` (a `cargo check` for s390x; skips cleanly without the target) | ✅ `conformance-big-endian` (the Go driver under qemu-s390x) | ✅ `tables-cook-open-cs` (the refuse half; the native big-endian half is stated unproven until a big-endian .NET exists) | ✅ `tables-java-order` | ✅ `tables-js-leg` | ✅ `tables-dart-order` `tables-dart-order-negative-control` (Dart's typed data is read at an explicit endianness everywhere (blockruntime.go:34), so the order word is the whole of the byte-order check) | — the host's order is never consulted and no platform query exists for a gate to catch; the two foreign surfaces hold it (docs/SPEC-TABLES.md) | ❌ #2236 — card schema-goose-table-wire-load (docs/PORT-GOOSE.md, THE CARD MAP) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2261 — the driver answers `block-foreign`; no named gate holds an intact magic with the other order word | ✅ `TestSquirrelBlockReadMatchesTheCorpus` `TestSquirrelBlockOrderWordNegativeControl` `tables-squirrel-block-order-word-negative-control` `TestSquirrelCookReadMatchesTheCorpus` (the corpus's block_byte_order and cook_byte_order_* forgeries, an intact magic beside the other order word, refused by name beside the `cook-foreign` and `block-foreign` surfaces; with the block's order-word check dropped, block_byte_order opens and the gate turns) | ❌ #381 — card schema-luau-table-wire-load (docs/PORT-LUAU.md, THE CARD MAP) | ✅ `TestLuaBlockRead` `TestLuaCookOpenAndCheck` (an intact magic with the other order word is refused by name, beside the `cook-foreign` and `block-foreign` surfaces) |

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

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `bench/tables/results/2026-09-03-pairing-arm64-macbook.md` | ✅ `bench/tables/results/2026-09-03-pairing-c-arm64-macbook.md` | ✅ `bench/tables/results/2026-09-03-rust-inline-arm64-macbook.md` | ✅ `bench/tables/results/2026-09-03-pairing-go2-arm64-macbook.md` | ✅ `bench/tables/results/2026-09-03-pairing-arm64-macbook.md` | ✅ `bench/tables/results/2026-09-03-java-port-arm64-macbook.md` | ✅ `bench/tables/results/2026-09-03-pairing-js2-arm64-macbook.md` | ✅ `bench/tables/results/2026-09-03-pairing-dart-arm64-macbook.md` | ✅ `bench/tables/results/2026-09-03-pairing-elixir-arm64-macbook.md` | ❌ #2236 — card schema-goose-bench (docs/PORT-GOOSE.md, THE CARD MAP) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2262 — the leg is in the tree (`bench/tables/lobster/leg`, gated by `bench-lobster-gate`) but no labeled board yet: its first sitting is a fleet pass, never the studio | — no table-wire bench | ❌ #381 — the leg is built (docs/PORT-LUAU.md, THE BENCH LEGS); no committed board carries the pairing-check label yet | ❌ #381 roadmap `port/lua/open/bench-board` (the leg `bench/tables/lua` and its gate `tables-lua-bench-gate` are in; the labelled board needs a measured sitting, which only a certify run takes) |

### I9 — The soak is gated on the allocation count or a floor

**Method.** A soak that gates only on heap drift cannot see a path that
allocates and frees the same bytes every iteration: it reads +0 forever. The
soak gates on the allocation COUNT re-measured every sample (or the rate
before and after in the same process, or the live-heap and binary-memory
FLOORS, which a carrier cannot move), with drift kept beside it as the weaker
instrument that sees retention. **The soak's length is a PASS COUNT, never a
clock** (`TABLES_<LANG>_SOAK_ITERATIONS`): the count is what the gate reads
across, a count runs the same loop on every machine, and `make shape-gate`
refuses a timing primitive outside the sanctioned bench. A short count rides
`make test`; the default count, about an hour, is the release act.

**Reference.** `test/c-tables/soak_main.c:263-309` (allocator calls counted
over the measured loop); `test/tables/soak_main.cpp` (the counting global
`operator new`, and the C allocator's entry points on glibc, read at every
sample); `test/go-tables/soak_test.go:187-260` (`Mallocs`
with site classification); `test/cs-tables/src/SoakChecks.cs` (`GC.GetAllocatedBytesForCurrentThread`
re-sampled over the measured loop); `test/js-tables/main.mjs` (`soak`: the bytes per iteration
re-sampled every sample of the measured loop);
`test/java-tables/src/Soak.java` (the per-iteration allocation count,
re-measured every sample, with heap drift beside it).

**Proven in.** C.

**Measured effect.** The Go soak found a lazily built descriptor (M13);
the Rust one is what made the audit's count exact.

**Negative control.** A matched `malloc`/`free` pair per iteration
(`tables-c-soak-negative-control`), a matched `new`/`delete` pair in
C++ (`tables-cpp-soak-negative-control`, a `-DSCHEMA_SOAK_PLANT_PAIR` build),
or a short-lived managed allocation per sample in C#
(`tables-cs-soak-negative-control`): the drift gate stays silent and the count
goes red.

**Targets:** soak

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `tables-cpp-soak` `tables-cpp-soak-negative-control` | ✅ `tables-c-soak` `tables-c-soak-negative-control` | ✅ `tables-rust-soak` `tables-rust-soak-negative-control` | ✅ `TestSoak` `TestSoakIdentifierCanGoRed` | ✅ `tables-cs-soak` `tables-cs-soak-negative-control` | ✅ `tables-java-soak` (524288 passes from `test-java`, 2^27 — about an hour — the release act, a count and never a clock, over the blob, list and pointer corpora) `tables-java-soak-negative-control` | ✅ `tables-js-soak` (the allocation gate's paths for `TABLES_JS_SOAK_ITERATIONS` passes, the bytes per iteration gated at every one of sixteen samples, live-heap drift beside it; `tables-js-soak-short` from `test-js` at 2^22 passes, and 2^35, about forty minutes on mini-m5, the release act) `tables-js-soak-negative-control` | ✅ `tables-dart-soak` `tables-dart-soak-negative-control` | ❌ #515 | ❌ #2236 — not yet carded in the schema-goose stream (docs/PORT-GOOSE.md, THE CARD MAP); the soak | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2263 — no soak yet (docs/PORT-LOBSTER.md, THE CARD MAP, schema-lobster-bench) | — no allocation count to gate a soak on (I1) | ❌ #381 — not yet carded in the schema-luau stream (docs/PORT-LUAU.md, THE CARD MAP); the soak | ✅ `tables-lua-soak` `tables-lua-soak-negative-control` (the load soak: one state and one value, `TABLES_LUA_SOAK_ITERATIONS` PASSES (a count, never a clock; 4000 by default, the release act the long one), every 500-pass sample re-measuring the allocation count of the rows that license nothing, which must be exact zero, and of the licensed row, which must stay inside its ceiling, with the live heap after a full collection read beside it as the weaker instrument; a table built and dropped per load turns the count and leaves the drift gate silent) |

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

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `tables-zero-cost` `tables-block-zero-cost` `TestTablesMoveNoGeneratedPacketByte` | ✅ `tables-c-zero-cost` `TestCEmitsTableSources` | ✅ `tables-rust-features` `TestTableFreeUnitEmitsNothing` | ✅ `TestTablesMoveNoGeneratedGoByte` `TestGoEmitsTableSources` | ✅ `tables-block-zero-cost` `TestCsEmitsTableSources` | ✅ `tables-java-zero-cost` `TestJavaTablesMoveNoGeneratedPacketByte` | ✅ `TestJsEmitsTableSources` `tables-js-standalone` | ✅ `tables-block-zero-cost` `TestDartEmitsTableSources` | ✅ `TestElixirEmitsTableModules` | ✅ `TestGooseTableFreeUnitAddsNothing` | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ✅ `TestLobsterTableFreeUnitAddsNothing` | ✅ `TestSquirrelTableFreeUnitEmitsNothing` | ✅ `TestLuauTableFreeUnitAddsNothing` | ✅ `tables-lua-zero-cost` `tables-lua-zero-cost-negative-control` `TestLuaTablesMoveNoGeneratedPacketByte` `TestLuaZeroCostGateGoesRed` (`compiler/tableslua_test.go`: adding a table moves no byte of the non-table output and grows only the table module and the embedded runtime, a table-free unit emits none of them, and the table module spells no block or cook symbol) |

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

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `conformance-negative-control-cpp` | ✅ `conformance-negative-control-c` | ✅ `conformance-negative-control-rust-walk` | ✅ `conformance-negative-control-go-walk` | ✅ `conformance-negative-control-cs` | ✅ `conformance-negative-control-java-walk` | ✅ `conformance-negative-control-js-walk` | ✅ `conformance-negative-control-dart-walk` `conformance-negative-control-dart-zero` `conformance-negative-control-dart-block` | ❌ #515 | ❌ #2236 — card schema-goose-conformance-leg (docs/PORT-GOOSE.md, THE CARD MAP) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2264 — no JSON text form yet, so there is no `json-read` for the control to localize | ✅ `conformance-negative-control-squirrel-walk` `conformance-negative-control-squirrel` | ❌ #381 — card schema-luau-conformance-leg (docs/PORT-LUAU.md, THE CARD MAP) | ✅ `conformance-negative-control-lua-walk` (the `FromJson` field-key match; lua json-read and json-hostile red, `json-write` and `wire` green) |

### I12 — The documented surface compiles and runs

**Method.** `docs/USAGE.md`'s example for the language is compiled and run
by a gate, so the documented surface goes red with the code rather than
drifting from it.

**Reference.** `test/tables/cook_main.cpp:1394` (`mode_usage`,
under `tables-cook-open`); `tables-dart-usage` runs the page's Dart verbatim
over the graph_tree corpus wire, with infos fatal under the analyzer.

**Proven in.** C++.

**Measured effect.** The Elixir reader ran the page's example by hand and
found a module name the packet emitter refuses — the drift a gate catches.

**Negative control.** None beyond the example itself.

**Targets:** usage

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `tables-cook-open` | ✅ `tables-c-usage` | ✅ `tables-rust-usage` `TestUsageRust` (docs/USAGE.md's `rust-table-usage` example compiled against `tables/examples` and run) | ✅ `tables-go-usage` | ✅ `tables-cook-open-cs` | ✅ `tables-java-usage` | ✅ `tables-js-usage` | ✅ `tables-dart-usage` `tables-dart-usage-negative-control` | ✅ `tables-elixir-usage` | ❌ #2236 — not yet carded in the schema-goose stream (docs/PORT-GOOSE.md, THE CARD MAP); the documented surface | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2265 — docs/USAGE.md has no Lobster example yet | ❌ #418 (docs/USAGE.md has no Squirrel section yet) | ❌ #381 — not yet carded in the schema-luau stream (docs/PORT-LUAU.md, THE CARD MAP); the documented surface | ✅ `tables-lua-usage` `tables-lua-usage-negative-control` |

### I13 — The text differential against a third implementation

**Method.** The leg emits N random instances as (wire, text); the compiler's
own Go engine (`schema unpack`, written from the spec and from neither
backend) reads the same wire and writes its text; the two texts are
byte-compared, then the other direction. Pinned goldens reach eighteen
instances; a random differential reaches the float ties they never do.

**Reference.** The engine in `internal/tabletext` (the original JavaScript
reference was pruned in #516).

**Proven in.** JavaScript (#516).

**Measured effect.** Twelve of forty instances differed on the first run —
a float32 at `-266744.625` rendering as an eight-digit tie.

**Negative control.** Restoring the magnitude tie-break requires red (19 of 60 on the pinned seed; #419 tracks remaining targets).

**Targets:** json-differential

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `tables-cpp-text-differential` `tables-cpp-text-differential-negative-control` (`TestCppTextDifferentialAgainstTheGoEngine`, `internal/codegen/cpptable/textdifferential_test.go`; the engine is the third implementation) | ✅ `tables-c-text-differential` `tables-c-text-differential-negative-control` (`TestCTextDifferentialAgainstTheGoEngine`, `internal/codegen/ctable/textdifferential_test.go`; the engine is the third implementation, 200 random finite float32/float64 instances, both directions) | ✅ `tables-rust-json-differential`, control `tables-rust-json-differential-negative-control` (`TestTextDifferentialRust`, `TestTextDifferentialRustNegativeControl`; variable root and a fixed-class unit's root, 200 instances at each of two seeds; the form-3 fixed record has no Rust text surface and is not claimed; first run: no divergence) | ✅ `tables-go-text-differential`, `tables-go-text-differential-negative-control` (`TestTextDifferential`, `TestTextDifferentialNegativeControl`, `internal/codegen/gotable/textdifferential_test.go`; 200 random float-only instances on a fixed root, the emitted `RootToJSON` against `internal/tabletext`, which shares no code with it, then the leg's text packed back; the control caps float32 at six digits) | ✅ `tables-cs-json-differential` `tables-cs-json-differential-negative-control` `TestCsTextDifferential` (`internal/codegen/cstable/textdifferential_test.go`; its first run found float64 written at fifteen digits, fixed in `internal/codegen/cstable/json.go`) | ✅ `tables-java-json-differential` (`TestTextDifferentialJava`, `TestTextDifferentialJavaNegativeControl`) | ✅ `tables-js-text-differential` | ✅ `tables-dart-text-differential`, control `tables-dart-text-differential-negative-control` (variable and fixed roots; first run: 6 of 200 on the Go seed, ties `toStringAsFixed` broke away from even) | ✅ `tables-elixir-json-differential`, `tables-elixir-json-differential-negative-control` (`TestTextDifferentialElixir`) | ❌ #2236 — card schema-goose-json-form (docs/PORT-GOOSE.md, THE CARD MAP) | ✅ `tables-moo-json-differential` (`TestMooJsonTextDifferential`, `internal/codegen/mootable/json_differential_test.go`: 576 draws, the corpus redrawn plus the float-only unit, entering moo as a literal until the moo table wire lands; both directions), control `TestMooJsonTextDifferentialNegativeControl` (the magnitude tie-break: 4 of 576) | ❌ #2266 — no JSON text form yet (docs/PORT-LOBSTER.md, THE CARD MAP, schema-lobster-json-form) | ❌ #419 (`TestSquirrelJsonAgreesWithTheCorpus` compares with the text the reference wrote; no third-implementation differential runs) | ✅ `tables-luau-text-differential` `tables-luau-text-differential-negative-control` (`TestLuauTextDifferentialAgainstTheGoEngine`, `internal/codegen/luautable/textdifferential_test.go`: the Go cell's splitmix64 at its seed and seed 1, a variable and a fixed root, the emitted save and ToJson against `internal/tabletext` both directions; the control writes a float32 at six digits) | ✅ `tables-lua-json-differential` `tables-lua-json-differential-negative-control` `TestLuaTextDifferential` (`internal/codegen/luatable/textdifferential_test.go`: 200 random float instances drawn in Lua from the Elixir leg's splitmix64 seed, the engine's text byte-compared with the leg's and the engine's pack of the leg's text byte-compared with the leg's wire; 5,000 more at a second seed were clean on the first run; the control caps a float32 at six digits and goes red on 199 of 200) |

### I14 — The allocation gate refuses to certify off the pinned runtime

**Method.** The gate reads the runtime's version and, on any other than the
pinned one, sets failed and returns without measuring; an escape hatch reports
and does not certify. One major's JIT inlines a generated body another leaves
over its threshold, where a double store boxes, so a floor measured on
whatever binary a PATH lookup found says nothing about the runtime the claim
is for. A native codec's allocations are in its source and a compiler adds
none, so the native legs state that reason here rather than a pin.

**Reference.** `test/js-tables/main.mjs:1412-1430` (`PinnedNodeMajor`, the
refusal, `SCHEMA_JS_ALLOC_ANY_NODE`).

**Proven in.** JavaScript.

**Measured effect.** The allocation the gate exists to catch is invisible on
one V8 major and steady at sixteen bytes a call on another.

**Negative control.** Running the gate on another major must refuse. Go uses
`tables-go-allocator-runtime-negative-control`; `SCHEMA_GO_ALLOC_ANY_GO=1`
prints observations without certification. The default requires Go 1.26.0.
Java uses `tables-java-allocator-runtime-negative-control` (the pin overridden
to 17: it must refuse naming both majors and print no row);
`SCHEMA_JAVA_ALLOC_ANY_JDK=1` prints NOT CERTIFIED. The default requires JDK 21.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| — a native codec's allocations are in its source; the counting allocator sees the same calls under any compiler | — a native codec's allocations are in its source; the interposed allocator sees the same calls under any compiler | — a native codec's allocations are in its source; the counting global allocator sees the same calls under any toolchain | ✅ `tables-go-allocator` `tables-go-allocator-runtime-negative-control` (Go 1.26.0) | ✅ `tables-cs-alloc` `tables-cs-alloc-runtime-negative-control` (.NET 10, `.github/dotnet-version`; `SCHEMA_CS_ALLOC_ANY_RUNTIME=1` observes, never certifies) | ✅ `tables-java-allocator` `tables-java-allocator-runtime-negative-control` (JDK 21; refuses before measuring off the pin and with no counter; `SCHEMA_JAVA_ALLOC_ANY_JDK=1` prints NOT CERTIFIED) | ✅ `test/js-tables/main.mjs:903` | ✅ `tables-dart-alloc` `tables-dart-alloc-runtime-negative-control` (Dart 3.13.2) | ✅ `tables-elixir-alloc-audit` `tables-elixir-alloc-runtime-negative-control` (OTP 29.0.5 with Elixir 1.20.4: the audit's first act is `test/elixir/runtime_pin.exs`, so on any other runtime it prints FAILED and measures nothing; SCHEMA_ELIXIR_ANY_OTP=1 prints the 23 cases' figures under NOT CERTIFIED, and the pin target refuses to write a budget under it) | ❌ #2236 — not yet carded in the schema-goose stream (docs/PORT-GOOSE.md, THE CARD MAP); the allocation instrument it guards | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2267 — no allocation gate yet to refuse off the pinned v2026.8 toolchain | — no allocation gate to refuse with (I1); make dist-squirrel refuses any tarball but the pinned sha256 | ❌ #381 — not yet carded in the schema-luau stream (docs/PORT-LUAU.md, THE CARD MAP); the allocation instrument it guards | ✅ `tables-lua-alloc` `tables-lua-alloc-runtime-negative-control` `TestLuaLoadAllocationRefusesOffThePin` (the gate reads `lua -v` first and, on any version but the pinned 5.4.8, refuses by name before it measures; `SCHEMA_LUA_ALLOC_ANY_LUA=1` prints the rows under NOT CERTIFIED and asserts nothing; the control refuses 5.4.7, 5.3.6 and 5.5.0 naming both versions) |

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

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `tables-wire-fuzz` `tables-wire-fuzz-negative-control` | ✅ `tables-c-wire-fuzz` `tables-c-wire-fuzz-negative-control` | ✅ `tables-rust-wire-fuzz-all` (every root, plain and `--retain`, 0 divergences) `tables-rust-wire-fuzz-negative-control` (the node index's malformed verdict removed: red on the report) `tables-rust-retain-negative-control` | ✅ `tables-go-wire-fuzz` `tables-go-wire-fuzz-negative-control` `tables-go-builders` `tables-go-retain-wire-fuzz` | ✅ `tables-cs-wire-fuzz` `tables-cs-region-fuzz` `tables-cs-builder-fuzz` `tables-cs-retain-fuzz` `conformance-negative-control-cs` | ❌ #492 — `tables-java-wire-fuzz` `tables-java-wire-fuzz-negative-control` run every root over the enumerated passes and the pinned vectors (0 divergences); the seeded random pass and the retention arm are owed (roadmap `java/java-wire-fuzz-every-root`) | ✅ `tables-js-wire-fuzz-all` `tables-js-wire-fuzz-plain` `tables-js-wire-fuzz-retain` `tables-js-wire-fuzz-negative-control` `cards-js-wire-fuzz` `cards-js-retain-fuzz` | ✅ `tables-dart-wire-fuzz` `tables-dart-wire-fuzz-negative-control` (`test/conformance/dart/main.dart`, the `wire-fuzz` mode: 54 file roots bound by unit and root, the message arm through the message codecs, a refused Save answered as the `-1`; 159,457 enumerated and 100,000 random mutants, 0 divergences; the `--retain` arm is bound but not gated, red at its first `graph_tree` mutant) | ❌ #492 | ❌ #2236 — card schema-goose-fuzz-differential (docs/PORT-GOOSE.md, THE CARD MAP) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2268 — no wire-fuzz leg yet (docs/PORT-LOBSTER.md, THE HARNESS: the pipe or a Go host shim) | ✅ `TestSquirrelWireFuzzAgreesWithTheGoEngine` `TestSquirrelWireFuzzNegativeControl` (the harness's mutants against the Go engine: the plain arm over fixed roots, the builder arm over variable ones) | ✅ `tables-luau-wire-fuzz` `tables-luau-wire-fuzz-negative-control` `TestLuauWireFuzzAgreesWithTheGoEngine` `TestLuauWireFuzzNegativeControl` (the file form on the pipe, under the native host `tools/luaufuzzhost`; the nine message-form roots are pinned absent by name in `test/conformance/luau/wire-fuzz-absent.txt`, and the `--retain` arm is driven but not yet gated) | ✅ `tables-lua-wire-fuzz-differential` (`TestLuaWireFuzzAgreesWithTheGoEngine` against `internal/tablewire` on a pipe, `TestLuaWireFuzzNegativeControl`) |

### J1 — Accessor and descriptor agreement

**Method.** The leg reads every row and every node of the block and cook
fixtures both ways — through the generated accessor and through the
descriptor — and requires agreement. Two independent derivations of one
layout, held against each other, so a moved accessor or a moved descriptor
offset is seen without a pinned dump that happens to cover the field.

**Reference.** `test/js-tables/main.mjs` (the leg's two-way read);
`tables-js-accessor-negative-control` and `tables-js-slot-negative-control`.

**Proven in.** JavaScript and Dart.

**Measured effect.** Structural.

**Negative control.** A scalar accessor moved four bytes in
`internal/codegen/jstable/record.go`, and a pointer slot's offset moved eight,
each through `go build -overlay`; each must turn the leg red.

**Dart.** `test/dart-tables/agree.dart` (`tables-dart-agree`, reached from
`test-dart`) reads every row of `block_render.bin` and `block_padded.bin`, every
node of the five `build/cook-fuzz` images (each reference also resolved from the
descriptor's own slot offset), and every record type either descriptor graph
reaches over seeded noise, through the generated accessor and through the
descriptor's offset, kind, width and signedness, and prints the counts it read.
`tables-dart-accessor-negative-control` moves `RenderCamera.camera_id` four bytes
in `darttable/block.go` and `tables-dart-slot-negative-control` moves
`TreeNode.left`'s descriptor offset eight in `darttable/cook.go`, each through
`go build -overlay`; each must turn the gate red on that field and on nothing
else. Its first run found two real disagreements, both in `block.go`'s one
scalar emitter: a `flags` field is a `uint64` slot in every target
(`ir/blocklayout.go`) and the accessor read one byte of it (`RenderShip.flags`),
and a pointer slot is an eight-byte delta that the row getter also reads one
byte of, which the gate pins as the low byte of the delta until the getter is
settled.

**Lua.** There is no accessor to hold against the descriptor, so the second
derivation is the compiler's: `internal/codegen/luatable/layoutagree_test.go`
dumps the layout the Lua runtime derives (`B.Layout` for every block projection
and row, the descriptors' `at` for every cook record) and requires it line for
line equal to `ir.Blocks` and `ir.RecordLayout`. `tables-lua-accessor-negative-control`
moves `RenderCamera.camera_id` four bytes in `block.lua` and
`tables-lua-slot-negative-control` moves `TreeNode.left`'s descriptor offset eight
in the emitted module; each must turn the gate red on that field and on nothing
else. Its first run found three real disagreements in `B.Layout`: a 128-bit
integer and a 128-bit fixed were sized as one byte at one, and a `bits(N)` slot
took its wire width where the block row holds a `uint32` (up to 32 bits) or a
`uint64`.

**Targets:** accessor-negative-control

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `tables-accessor-descriptor-agreement` `tables-accessor-negative-control` `tables-slot-negative-control` | ✅ `tables-c-accessor-descriptor-agreement` `tables-c-accessor-negative-control` `tables-c-slot-negative-control` | ✅ `tables-rust-accessor-descriptor-agreement` `tables-rust-accessor-negative-control` `tables-rust-slot-negative-control` (`test/rust-agree`: every block row, cook node and reachable record type read through the `#[repr(C)]` row struct and `<record>_at`, and through `TableBlockInfo`/`TableCookInfo` offsets; the controls swap `RenderCamera.camera_id`/`camera_type` and move `TreeNode.left`'s described slot, via `tools/sabotage`) | ✅ `tables-go-accessor-descriptor-agreement` `tables-go-accessor-negative-control` `tables-go-slot-negative-control` | ✅ `tables-cs-accessor-descriptor-agreement` `tables-cs-accessor-negative-control` `tables-cs-slot-negative-control` | ✅ `tables-java-accessor-agreement` `tables-java-accessor-negative-control` `tables-java-slot-negative-control` | ✅ `tables-js-accessor-negative-control` `tables-js-slot-negative-control` | ✅ `tables-dart-agree` `tables-dart-accessor-negative-control` `tables-dart-slot-negative-control` | ✅ `tables-elixir-accessor-descriptor-agreement` `tables-elixir-accessor-negative-control` `tables-elixir-slot-negative-control` (plain scalar block-record fields and pointer cook slots (make/elixir.mk:279)) | ❌ #2236 — card schema-goose-vt-descriptors (docs/PORT-GOOSE.md, THE CARD MAP) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ✅ `TestLobsterDescriptors` `TestLobsterDescriptorsNegativeControl` (`accessor_descriptor`: a fresh row read through the generated accessor agrees with the descriptor's defaults) | ❌ #421 (the block descriptors carry the only accessors; no agreement check or slot control yet) | ❌ #381 — not yet carded in the schema-luau stream (docs/PORT-LUAU.md, THE CARD MAP); accessor and descriptor agreement | ✅ `tables-lua-accessor-descriptor-agreement` `tables-lua-accessor-negative-control` `tables-lua-slot-negative-control` (Lua generates no accessor: the block and cook are read through the descriptors alone, `internal/codegen/luatable/runtime/block.lua`, so what is held is the layout's two independent derivations, the compiler's model against the Lua runtime's own arithmetic, for every block projection, block row and cook record of the corpus's 33 units, 104 and 203 of them, in `internal/codegen/luatable/layoutagree_test.go`; the controls move `RenderCamera.camera_id` four bytes in `block.lua` and `TreeNode.left`'s descriptor offset eight, and each reds on that field alone. Its first run found `B.Layout` sizing a 128-bit integer, a 128-bit fixed and a `bits(N)` slot wrong, fixed in `block.lua`) |

### J2 — The runtime home, with a file-order control

**Method.** A unit's shared runtime lives in one file named by the PACKAGE
(`<Package>Table.cs`, `<Package>Table.js`), never by the file that happens to
sort first, so a schema file that sorts earlier relocates nothing and the
table runtime is byte-identical across the two trees. The gate adds such a
file to a copy of the unit and requires the homes not to move; the control
puts the file-order rule back and requires them to move (#347).

**Reference.** `tables-runtime-home` and `tables-runtime-home-negative-control`
in the Makefile; `tables-js-runtime-home` and its control.

**Proven in.** C#; the Dart leg.

**Measured effect.** Adding a file that sorted earlier relocated ~2,000
lines of correct output into a diff nobody could read (#347).

**Negative control.** The file-order rule put back.

**Targets:** runtime-home

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `tables-cpp-runtime-home` `tables-cpp-runtime-home-negative-control` `tables-cpp-runtime-home-bytes-negative-control` (the C++ runtime has no one file: every `<Base>Table.h` carries it behind package-named include guards, so the home is the guard set, and within a unit every header carries it byte-identical, `test/cpp-tables/runtime_home_digest.sh`) | ✅ `tables-c-runtime-home` `tables-c-runtime-home-negative-control` (every `<Base>Table.h` carries the runtime behind package-named guards) | ✅ `tables-rust-runtime-home` `tables-rust-runtime-home-negative-control` (a crate is the package: the homes are module constants off the crate, and a file that sorts first moves nothing) | ✅ `tables-go-runtime-home` `tables-go-runtime-home-negative-control` | ✅ `tables-runtime-home` `tables-runtime-home-negative-control` | ✅ `tables-java-runtime-home` `tables-java-runtime-home-negative-control` | ✅ `tables-js-runtime-home` `tables-js-runtime-home-negative-control` | ✅ `tables-dart-runtime-home` `tables-dart-runtime-home-negative-control` | ❌ #422 | ✅ `tables-goose-runtime-home` `tables-goose-runtime-home-negative-control` (the runtime modules are emitted once under their own names beside the modules and imported relative to the importing file; the gate holds them and every original Table module byte-identical when an earlier-sorting file joins) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2269 — the runtime is emitted once beside the modules, with no file-order control yet | ❌ #422 (the unit's table runtime is one module, <Package>Tables.nut, beside its runtime files; no file-order control yet) | ❌ #381 — card schema-luau-conformance-leg (docs/PORT-LUAU.md, THE CARD MAP); the runtime is emitted once beside the modules, with no file-order control yet | ✅ `tables-lua-runtime-home` `tables-lua-runtime-home-negative-control` (`make/lua-open-gates.mk`: the table module stays `TabledemoTables.lua` and every embedded runtime file stays byte-identical when an earlier-sorting schema file joins the unit; the control puts the file-order rule back through `go build -overlay` and the module moves from `GuardedTables.lua` to `AaaTables.lua`) |

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

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `tables-cpp-release` (the long wire fuzz, two seeds, and the INT32_MAX base64 reproduction; and `tables-cpp-release-battery`: the tables, maps, lists, arms and retain suites, the soak and the block fuzz built `-O2 -DNDEBUG`, with `tables-cpp-release-flags-negative-control`) | ✅ `tables-c-release` | ✅ `tables-rust-release` (the all-roots wire fuzzer and the forgery fuzzer at N=1,000,000 on a second seed, the wire fuzzer's control, the soak at 16,777,216 passes and its control) | ✅ `tables-go-release` | ✅ `tables-cs-release` | ✅ `tables-java-release` | ✅ `tables-js-release` | ✅ `tables-dart-release` | ✅ `tables-elixir-release` | ✅ `tables-goose-release` (`test-goose` and every Goose control on the certification schedule) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2270 — no `tables-lobster-release` target yet | ✅ `tables-squirrel-release` `tables-squirrel-release-negative-control` (the wire pin, the wire fuzzer over both arms with the builder arm scaled to seeds 2 and 3 at ten times the PR count, and the four conformance controls; the control overlays a load that reports a stopped builder finished and requires the release's builder arm red) | ❌ #381 — not yet carded in the schema-luau stream (docs/PORT-LUAU.md, THE CARD MAP); the release tier | ✅ `tables-lua-release` |

### J4 — Emitted text is analyzer-clean and format-canonical

**Method.** Every generated table unit is held to what the language's
analyzer or linter accepts and what its formatter writes, as a gate — so an
emitter that drifts from the language's own conventions goes red on the
diff rather than on a reviewer. Each leg runs its own authority over the
generated table tree: `clang-format` for C++ and C, `gofmt` and `go vet` for
Go, `dotnet format` for C#, `google-java-format` for Java, `eslint` for
JavaScript, `dart format` and the analyzer for Dart, `cargo clippy` for Rust,
and `mix format` for Elixir.
The Java leg's formatter pin, style, lint flags and gates are on
[docs/JAVA-DIALECT.md](JAVA-DIALECT.md).

**Reference.** `tables-dart-clean` (analyzer and `dart format
--set-exit-if-changed`); `tables-rust-clippy`.

**Proven in.** Dart; carried to every other leg by the targets in the row
below.

**Measured effect.** Structural.

**Negative control.** None for the C-family formatter, which is its own oracle. The C++ gate's two halves each go red on a planted fault in the packet emitter, built under a Go overlay: one K&R struct brace for the format half, one C-style placement-new cast for the warnings half (`tools/sabotage/cpp_clean.go`; both registered in `make/negative-controls.json`), each behind a guard that the unsabotaged output is green.

**Targets:** clean

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `tables-cpp-clean` (`clang-format` in the repo's `.clang-format`, and `clang++ -Weverything` less the c++98-compat family only, over every generated C++ file: the table corpus, the wide corpus and the committed `generated/` tables and packets; required by `make test` and by the cpp leg of `ci-fast`; the packet emitter's files are format-clean and carry only the three kinds that cannot be cleared without altering the packet wire (`padded`, and the contradictory `switch-default` / `covered-switch-default` pair), the table emitter's ride the shrink-only ratchets `make/cpp-format-allow.txt` and `make/cpp-warn-allow.txt`) | ✅ `tables-c-clean` (`clang-format`) | ✅ `tables-rust-clippy` | ✅ `tables-go-clean` (`gofmt`, `go vet`) | ✅ `tables-cs-clean` (`dotnet format` and the SDK analyzers over the table units, required by `test-cs`; 152,044 WHITESPACE findings in 295 units fixed in the emitters, #547; `tables-cs-clean-negative-control`; the packet units are held by `packet-clean-cs`) | ✅ `tables-java-clean` (google-java-format 1.37.0 `--aosp`, every table and packet file, at least 585, required by `test-java`); `tables-java-compile-all` (`--release 17 -Xlint:all -Werror`, all 25 units, from `tables-java-release`); its two negative controls are on [JAVA-DIALECT.md](JAVA-DIALECT.md) | ✅ `tables-js-clean` (`eslint`, over the table and packet units) | ✅ `tables-dart-clean` `tables-dart-clean-negative-control` (a planted off-format unused declaration: the formatter rewrites it, the analyzer names `unused_element`) | ✅ `tables-elixir-clean` (`mix format`) | ✅ `tables-goose-strict` (every generated module and the runtime modules beside it, each checked on its own by the pinned compiler, zero warnings, warnings as errors; v2026.2 ships no formatter, so the compiler's warnings are its analyzer) `tables-goose-strict-negative-control` | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS). The analyzer half already holds every generated packet module and the vendored runtime: `schema-moo-generated-analyze-strict-gate` (`moo-analyze --mode=strict` silent, no error and no lint warning, the analyzer held to the moo pin; `TestMooGeneratedAnalyzeStrictGate`, controls `schema-moo-generated-analyze-strict-gate-negative-control` and `-pin-negative-control`), and the table units join its corpus when they land. The format half is — : moo has no formatter (the pin builds moo, moo-analyze, moo-ast, moo-compile, moo-bytecode and moo-reduce, none of which formats, as Luau upstream ships none), so the emitter's layout is the canonical form | ✅ `tables-lobster-clean` (`make/schema-lobster-generated-strict-gate.mk`: every unit of examples/, examples128/, examples-wide/ and the conformance manifest compiles under the pinned v2026.8 with zero warnings; `schema-lobster-generated-strict-gate-negative-control` plants a `var` that must be a `let` and requires red on that line alone; Lobster ships no formatter, so the format half has no tool to run) | ❌ #424 (no Squirrel analyzer or formatter is pinned) | ✅ `tables-luau-clean` `schema-luau-generated-analyze-strict-gate` (`luau-analyze --mode=strict` over every manifest unit, the examples and bench corpora, the packet and table modules and the runtime; warnings are errors and a `--!nolint`, `--!nonstrict` or `--!nocheck` is refused; `schema-luau-generated-analyze-strict-gate-negative-control` plants a type error, a lint warning and a `--!nolint`); no formatter: StyLua 2.5.2 and 2.6.0 cannot parse Luau 0.741 integer literals (`0i`) | ✅ `tables-lua-luacheck` `tables-lua-luacheck-negative-control` (luacheck over the generated tree; no Lua formatter is pinned, so the format half is open; the gate is the schema-port-lua-gates sub-worker's) |

### J5 — The bench leg's golden gate runs before the clock

**Method.** Before any timing, the bench leg round-trips every variant of
the corpus and byte-compares against the golden; a leg whose codec does not
reproduce the corpus refuses to time it rather than posting a number.

**Reference.** `tables-go-bench-gate` in the Makefile. Elixir proved the
technique in and lost the leg with its table wire (#515).

**Proven in.** C++, C, C# and Go; Elixir proved the technique in before it lost
the leg with its table wire (#515); Lobster (`bench-lobster-gate`, both of its
legs, with `bench-lobster-gate-negative-control` flipping each golden).

**Measured effect.** Structural.

**Negative control.** None beyond the golden itself.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `tables-cpp-bench-gate` (its negative control, tables-cpp-bench-gate-negative-control, runs in the negative-controls leg: one changed byte in the golden or in variant 0 turns it red) | ✅ `tables-c-bench-gate` | — no table-wire bench | ✅ `tables-go-bench-gate` | ✅ `tables-cs-bench-gate` | — no table-wire bench | — no table-wire bench | — no table-wire bench | — no table-wire bench | ❌ #2236 — card schema-goose-bench (docs/PORT-GOOSE.md, THE CARD MAP) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ✅ `bench-lobster-gate` `bench-lobster-gate-negative-control` | — no table-wire bench | ✅ `bench-luau-gate` (both legs, no clock: `bench/luau/run` and `bench/tables/luau/leg`, each run with --gate) `bench-luau-gate-negative-control` | — no table-wire bench |

### J6 — The card conformance check

**Method.** Every language leg that carries `tables/cards` drives the
43-case shared corpus in `tables/cards/conformance/MANIFEST.txt` (`./build/conformance-harness cards`
in `test/conformance/harness/cards.go`, with `SCHEMA_CARDS_SURFACES` and
`SCHEMA_REQUIRE_CARDS_CORPUS=1`) across all nine surfaces (`load` 4/4,
`save` 4/4, `render` 4/4, `parse` 2/2, `bounds` 12/12, `upgrade` 5/5,
`edit` 8/8, `taskhash` 3/3, `view` 1/1), comparing every byte and refusal
verdict against the Go reference answers (`tla/CardVersioning.tla`,
`tla/CardHops.tla`). `make cards-conformance-all` runs the Go leg and all
nine non-Go legs with all nine surfaces required.

**Reference.** `test/conformance/harness/cards.go`, `tables/cards/cmd/cards-driver/main.go`,
and `make cards-conformance-all` in `make/go.mk`.

**Proven in.** Go, C, C++, C#, Rust, Java, JavaScript, Dart, Elixir, Lua.

**Measured effect.** Structural: every leg's card codec, facade bounds, brief
parser/renderer, `v1` to `v2` upgrader, field editors, task hash, and
descriptor view agree byte for byte on the 43-case corpus.

**Negative control.** `cards-conformance-negative-control`,
`cards-c-conformance-negative-control`, `cards-cpp-conformance-negative-control`,
`cards-cs-conformance-negative-control`, `cards-rust-conformance-negative-control`,
`cards-java-conformance-negative-control`, `cards-js-conformance-negative-control`,
`cards-dart-conformance-negative-control`, `cards-elixir-conformance-negative-control`,
`cards-lua-conformance-negative-control`.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `cards-cpp-conformance` (`tables/cards/cpp/driver.cpp`) | ✅ `cards-c-conformance` (`tables/cards/c/driver.c`) | ✅ `cards-rust-conformance` (`tables/cards/rust/src/bin/cards-driver.rs`) | ✅ `cards-conformance-go` (`tables/cards/cmd/cards-driver/main.go`) | ✅ `cards-cs-conformance` (`tables/cards/cs/Driver/Program.cs`) | ✅ `cards-java-conformance` (`tables/cards/java/src/cards/Driver.java`) | ✅ `cards-js-conformance` (`tables/cards/js/driver.mjs`) | ✅ `cards-dart-conformance` (`tables/cards/dart/bin/driver.dart`) | ✅ `cards-elixir-conformance` (`tables/cards/elixir/driver.exs`) | ❌ #2236 — not yet carded in the schema-goose stream (docs/PORT-GOOSE.md, THE CARD MAP); no `tables/cards/goose` leg exists, so the goose leg is not in `make cards-conformance-all` | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2272 — `tables/cards` has no lobster driver, so the leg is not in `cards-conformance-all` | ❌ #366 (no cards driver is in the squirrel leg, so `cards-conformance-all` runs none) | ❌ #381 — not yet carded in the schema-luau stream (docs/PORT-LUAU.md, THE CARD MAP); `tables/cards` has no Luau driver | ✅ `cards-lua-conformance` (`tables/cards/lua/driver.lua`) |


