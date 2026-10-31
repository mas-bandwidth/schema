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
| ✅ `internal/codegen/cpptable/codecs.go:1113` | ✅ `internal/codegen/ctable/codecs.go:1074` | ✅ `internal/codegen/rusttable/variablecodec.go:1110-1114` (the limit form: a nested body is read over the caller's one slice to the integer bound `sub`, into the caller's field) and `tables-rust-alloc` (0 allocator calls on the fixed form's load, measure and save into caller storage, the block open and row walk, the cook write, open and deref walk, and the variable wire's scalar-only loads; a variable-wire record with a `String` or `Vec` field, and the save that answers a `Vec<u8>`, allocate by their declared storage and are reported at their rows) | ✅ `internal/codegen/gotable/wire.go:178-187` | ✅ `internal/codegen/cstable/cstable.go:958` (a `ref struct`) | ❌ #517 after 3.0.0: performance — the block row walk and the cook read are 0 beyond one handle per open (`tables-java-allocator` rows 1 and 2, localized by `tables-java-alloc-negative-control`), loadMeasure through a kept Measure is exact 0 per call and per node (row 3, and 128 bytes for the one-argument Measure/Report/Reader handle, 0 per node), and save through a kept Saver allocates only the returned byte[] (row 4, 0 per node); what remains for M1 on the variable wire is load, which answers a fresh Value tree until a caller-owned-storage form exists | ❌ #516 (`tables-js-alloc` measures only the block row walk and the cook deref: no row reads a table-wire Load, Measure or Save, and the id-table `<T>Load` builds the value it returns) | ❌ #514 (closed without it) after 3.0.0: performance — the fixed form's load, measure and save into caller storage, the block row walk and the cook read hold 0 scavenges under AOT (`tables-dart-alloc`, `tables-dart-alloc-negative-control`), but the id-table wire's reader bounds a nested body with a new `TableWireReader.sub` object rather than narrowing the one reader's limit (`internal/codegen/darttable/wireform.go:1031`), and its Load answers a fresh value tree; the limit form and a load into caller storage are owed | — a decoded BEAM term is an allocation and no buffer is caller-owned (docs/SPEC-TABLES.md); the leg that pinned a per-case count went with the table wire (#515) | ✅ `TestGooseTableWireMatchesTheCorpus` — the load fills the caller's own flat value table and node table (`internal/codegen/goosetable/runtime/wire_load.goose:1626`), every text and blob a view of the caller's bytes; a nested body is the reader window `Rd` narrowed and restored, never a sub-view (`internal/codegen/goosetable/runtime/wire_load.goose:140`); Goose has no heap: the storage is data-stack arrays the caller's scope owns | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2246 — the variable-wire load answers a fresh `Record` tree from the caller's byte string (`internal/codegen/lobstertable/runtime/load.lobster`, a cursor whose limit is an int); no instrument (I1) measures a read path yet | — a load answers a tree of class instances and interned VM strings, which the VM allocates, and sq offers a script no allocation count to hold a zero to (I1) | ❌ #381 — every Luau read hands back fresh tables (the variable `L.load`, the fixed `F.read`, the batch `M.LoadMessages`), so no read is into caller-owned storage, and no allocation instrument (I1) measures the block and cook walks; the reader is a cursor over the caller's buffer, its limit an integer | ✅ `tables-lua-alloc` `tables-lua-alloc-negative-control` `TestLuaLoadNarrowsTheReaderInPlace` `TestLuaResetEqualsTheConstructor` `TestLuaWireFuzzAgreesWithTheGoEngine` `internal/codegen/luatable/runtime/load.lua:71` (the limit form: the reader is one Lua string with an integer cursor and limit, and a nested body, an arm, an array body, a map entry and a keyed slot are bounded by `narrow` and `restore` on that one reader, never a view, so the read walk builds no reader, held by a scan and its planted sub-view; Load fills caller-owned storage, `load.Load(T, name, bytes, report, state, into)` with `state` from `load.NewState()` (the readers, the id table, the queue of pointer slots, the context) and `into` the caller's value, put back to its declared defaults in place by `D.reset` (deep-equal to the constructor over every table, TestLuaResetEqualsTheConstructor) and read into, a nested table, a fixed or keyed array and a union it already holds being the storage that is read into the first time a field rides and a repeat starting again from its defaults; what a Lua load cannot read into is a counted or unbounded array, a list, a map, a union arm, a pointer's node, a wide integer and a string past 40 bytes, each a table or object the load builds, and each row says what it licenses; the 20000-mutant differential against the engine reads every mutant a second time into the value and state the one before left and must answer the same report, root and re-saved bytes, and found a clamped wide bound aliased into the value (fixed, held by the descriptor gate)) |

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
| ✅ `internal/codegen/cpptable/cpptable.go:513` | ✅ `internal/codegen/ctable/ctable.go:514-518` | — a Rust `u64` is a register value and nothing boxes it, so composing halves buys nothing; `u64::from_le_bytes` is byte-order-neutral by itself (M12), which is the other half of the reason (`internal/codegen/rusttable/fixedruntime.go:385`) | ✅ `internal/codegen/gotable/wire.go:152` | ✅ `internal/codegen/cstable/cstable.go:986-991` | — a Java `long` is a primitive held unboxed in every field and local, and the reader composes the word from bytes into one (`internal/codegen/javatable/variableform.go:153`, `get64`); nothing boxes it, so there is no composed form to keep a deref allocation-free. The one boxed 64-bit value is an unbounded list's `List<Long>` element, which is storage and M1's row (after 3.0.0: performance) | — the accelerator paths compose (`internal/codegen/jstable/block.go:384`, `cook.go:513`); a 64-bit wire FIELD's value is a BigInt or loses precision, so those rows read one under a stated ceiling (`test/js-tables/main.mjs`, the RootConfig rows) | — an `int` is a 64-bit machine word and nothing boxes (`internal/codegen/darttable/block.go:213`) | ✅ `tables-elixir-offsets` `tables-elixir-offsets-negative-control` `tables-elixir-cook-negative-control` `TestOffsetsComposeFromHalvesElixir` (the block's `offset_of` and the cook's delta compose from 32-bit halves, refused from the high half before any bignum forms; every corpus open, row and cook slot allocates no more than an immediate-value control, against a forced-boxing control that does; a 64-bit FIELD value of 2^59 or more is a bignum on the BEAM whatever a reader does) | ✅ `TestGooseTableWireMatchesTheCorpus` — a loaded value's bits are two unboxed u64 lanes (`internal/codegen/goosetable/runtime/wire_load.goose:120`), i64 and u64 are Goose scalars stored unboxed in every struct field (`TestGooseEveryScalarFieldIsBornAtItsIRDefault`), and the 128-bit and fixed-point kinds ride the scalars and tblscalars2 rows | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | — a Lobster `int` is a 64-bit two's-complement word held unboxed in every slot (docs/PORT-LOBSTER.md, THE NUMBER MODEL), and a 64-bit field is one `read_int64_le` (`internal/codegen/lobstertable/runtime/load.lobster`); nothing boxes, so there is no composed form to keep a deref allocation-free | — the sq64 integer is a 64-bit value in the object slot and nothing boxes (docs/PORT-SQUIRREL.md, 64-BIT AND 128-BIT INTEGERS) | ❌ #381 — card schema-luau-type-wire-read (docs/PORT-LUAU.md, THE CARD MAP); expected moot: a 64-bit value is the pinned Luau's primitive `integer`, and the allocation instrument (I1) has to say so | — a Lua 5.4 integer is a native unboxed 64-bit value and string.unpack reads one whole, so there is no box to compose around (docs/PORT-LUA.md) |

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
| ✅ `internal/codegen/cpptable/cpptable.go:572-573` | ✅ `internal/codegen/ctable/ctable.go:565-568` | ✅ `internal/codegen/rusttable/variablecodec.go:913-918` `internal/codegen/rusttable/fixedform.go:706-709` (the variable codec reads the raw integer and bit-casts it at the assignment, `f32::from_bits(raw as u32)`, and its save writes `to_bits()`; the fixed form's scatter decodes the bytes at the store; the float rung tests a float32's raw bits for a NaN or an infinity and then takes the ordinary conversion; and a Rust float is never boxed, so no call shape can cost one) | ✅ `internal/codegen/gotable/read.go` (`emitReadScalar`) | ✅ `TestCsFloatBitCastIsAtTheAssignment` (`BitConverter` at the store; helpers `[MethodImpl(AggressiveInlining)]`) | — a Java `float` or `double` is a primitive passed and returned in registers, and the JVM boxes one only on conversion to an Object, never at a call; the variable form bit-casts at the assignment (`internal/codegen/javatable/variableform.go:4369`, `Double.longBitsToDouble` over the raw bits), and the block's `f32`/`f64` helpers (`internal/codegen/javatable/block.go:133`) cost a call and no allocation | ✅ the allowance above, held by its instruments: `TestBlockFloat32AccessorCallsNoClosure` (every block and record float32 accessor calls the unit's one module-level `TableBlockReadF32`, never a closure), `tables-js-alloc` (the RenderFrame ships walk reads its float32 `thrust` per row at 0.0 bytes per iteration) and `tables-js-alloc-negative-control`, and `TestJsFloat32CellsAreBitTransparent` (`TableVariableWidenF32` is retired: the variable codec decodes the raw bits at the read); the variable codec's own walk is the generic path, held under M1 | ✅ `TestDartFloatBitsAreAtTheAssignment` (the float32 conversion inline at each assignment in the packet, block and message emitters; no helper in the output), `make tables-dart-float-jit` (0 steady JIT scavenges, 3.13.2) and `tables-dart-float-jit-negative-control` | — a BEAM float is a boxed term whatever the call shape; `R.f32_bits` costs what the term costs | — a Goose f32 or f64 is a C scalar passed by value; nothing crossing a call allocates, there being no heap | — a moo number is an unboxed Q48.16 value in every slot, and moo has no float: a float32 field is its binary32 pattern held as a number and a float64 field a frozen `Uint64`, moved by `serializeFloatBits` / `serializeDoubleBits` with no conversion (docs/PORT-MOO.md) | — a Lobster `float` is an unboxed 64-bit slot, so a float crossing a call allocates nothing; the float32 bits are read in place (`read_uint32_le` beside `read_float32_le`, `internal/codegen/lobstertable/runtime/load.lobster`) | — a SQUSEDOUBLE float is an unboxed value in the object slot, so a call costs it nothing | — a Luau number is an unboxed double in every value slot, so a float crossing a call allocates nothing; `buffer.readf32` / `readf64` read the bits in place | — a Lua number is an unboxed value across every call and PUC Lua has no JIT threshold to box at (docs/PORT-LUA.md) |

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
| ✅ `tables-keyed-none-refusal-ndebug` `tables-keyed-max-refusal-ndebug` | ✅ `TestCTableKeyedBounds` `tables-c-keyed-none-refusal-ndebug` `tables-c-keyed-max-refusal-ndebug` | ✅ `tables-rust-keyed` `tables-rust-keyed-negative-control` (`TestKeyedAccessorRust`, `TestKeyedAccessorRustNegativeControl`): `table_enums.rs` carries `table_key_slot` (one unsigned compare, `key.wrapping_sub(1) >= slots`, covering None and past E.Max) and `table_keyed` / `table_keyed_mut` / `table_keyed_iter` over a keyed array's slice, with the typed `<Enum>::keyed`, `keyed_mut` and `keyed_iter` beside each enum; the refusal is a panic naming the KEY in every build (the gate's probe runs under `rustc -O`), and the control takes the None half, the E.Max half and the shift out in turn, each red on exactly its own clause | ✅ `TestKeyedRefusesNone` `TestKeyedRefusesPastMax` `TestKeyedPlacesByKey` `tables-go-keyed` `tables-go-keyed-negative-control` (each test requires the guard's own refusal, which names the key, and not the runtime's index panic; the control removes the None compare, the past-Max compare and the k-1 shift from the generated `TableKeyed` in turn, each turning exactly its own test red) | ✅ `tables-cs-leg` `tables-cs-leg-keyed-negative-control` `TestCsKeyedAccessorCoversBothEnds` (`RefuseKey`: one unsigned compare at both ends, an `ArgumentOutOfRangeException` naming the key; the leg's keyed indexer refuses None, past Max, -1, int min and int max, and the control plants the None-only guard back, so the past-Max key reaches the slot array) | ✅ `TestKeyedArrayIndexingJava` `tables-java-keyed` `tables-java-keyed-negative-control` (`v.ships(key)` / `v.ships(key, value)`, `FleetRow.shipsByKey(data, at, key)`, `FleetRow.shipsAtKey(at, key)`; `int` key, `Integer.compareUnsigned`) | ✅ `TestKeyedArrayAccessorsJavaScript` `tables-js-keyed` `tables-js-keyed-shift-negative-control` (`internal/codegen/jstable/keyed.go`: the variable and fixed classes' static `T.<field>At(value, key)` / `T.set<Field>At(value, key, element)` / `T.<field>Entries(value)`, the block row's `<Field>ByKey(block, key)` / `<Field>AtKey(key)`; an integer compare at both ends and a `RangeError` naming the field and the key, in every build; the test plants the None-only guard, the Max-only guard and the missing shift back) | ✅ `TestDartKeyedArraysAreHeldAtBothEnds` `tables-dart-keyed-negative-control` (Dart indexes by slot: key k rides at slot k-1, and the language's own RangeError — the typed list's bound — is the refusal) | ✅ `TestKeyedAccessorCoversBothEndsElixir` `tables-elixir-keyed` `tables-elixir-keyed-none-negative-control` `tables-elixir-keyed-max-negative-control` | ❌ #2236 — card schema-goose-vt-optionals (docs/PORT-GOOSE.md, THE CARD MAP) | ✅ `TestMooKeyedAccessorCoversBothEnds` `TestMooKeyedNegativeControls` (`internal/codegen/mootable/runtime/schema_tables.moo` `S.keyed(table, field)`: `get(array, key)` and `set(array, key, value)` take an integer check and one range check of the key against 1..`E.Max` (a moo number is Q48.16, with no unsigned wrap to lean on), so None, a negative or non-integer key and any key past Max are refused by name (`keyed_refused`) in every build, `each(array)` yields the key beside the element, and `key(name)` is the key of a variant name; the array stays a plain array with key k at index k. The control plants the None guard removed, the past-Max guard removed and the key misplaced by one, each turning exactly its own rows. The fixed reader matches a slot by its key's id across versions (`TestMooFixedVersioningNewReadsOld`)) | ✅ `tables-lobster-keyed` `tables-lobster-keyed-negative-control` (`<Owner><Field>Slot`/`At`/`Each` take the key through `schema_rt.keyed_slot`, `internal/codegen/lobstertable/runtime/descriptors.lobster`: None, a negative key and a key past `<Enum>Max` refused by name with asserts compiled away; the controls take each end of the guard out and misplace the shift) | ✅ `TestSquirrelKeyedAccessorCoversBothEnds` `TestSquirrelKeyedNegativeControls` (`internal/codegen/squirreltable/runtime/descriptor.nut` `keyed`, reached as the generated module's `Keyed(table, field)`: `get(array, key)` and `set(array, key, value)` refuse None, a negative or non-integer key and any key past `E.Max` by name, `keyed_refused`, in every build; `each(array, fn)` hands the key beside the element and `key(name)` is the key of a variant name; the array stays a plain array with key k at slot k-1, so a bare index still names a slot and the accessor is the one place a key is checked; three plants, the None guard removed, the past-Max guard removed and the key misplaced by one, each turn their own line), with `TestSquirrelKeyedArraysAreHeldAtBothEnds` `TestSquirrelFixedFormUnionsAndKeyedArrays` on the wire and fixed forms | ✅ `tables-luau-keyed` `tables-luau-keyed-negative-control` (`internal/codegen/luautable/runtime/schema_tables.luau` `S.keyed(table, field)`: `get(array, key)` and `set(array, key, value)` take an integer check and one range check of the key against 1..`E.Max` (a Luau number is a double, with no unsigned wrap to lean on), so None, a negative, non-integer, NaN or infinite key and any key past Max are refused by name (`keyed_refused`) in every build, `each(array)` yields the key beside the element, and `key(name)` is the key of a variant name; the array stays a plain array with key k at index k. `TestLuauKeyedNegativeControls` plants the None guard removed, the past-Max guard removed and the key misplaced by one, each turning exactly its own rows). The fixed reader matches a slot by its key's id across versions (`TestLuauFixedVersioningNewReadsOld`, keyed_array_enum_append) | ✅ `TestLuaKeyedAccessorCoversBothEnds` `TestLuaKeyedNegativeControls` `internal/codegen/luatable/runtime/descriptor.lua:564` (`D.keyed`, reached as the generated module's `Keyed(table, field)`: `get(array, key)` and `set(array, key, value)` take one unsigned compare of `key - 1` against `E.Max`, so None, a negative or non-integer key and any key past Max are refused by name in every build, `each(array)` yields the key beside the element, and `key(name)` is the key of a variant name; the array stays a plain sequence with key k at index k, so a bare `array[key]` still answers nil at both ends and the accessor is the one place the refusal lives; three plants, the None guard removed, the past-Max guard removed and the key misplaced by one, each turn exactly their own rows) |

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
| ✅ `tables-flat-wire` `tables-flat-wire-negative-control` | ✅ `TestCTableWireGraphIdentity` `tables-c-wire-fuzz` `tables-c-wire-fuzz-negative-control` | ✅ `internal/codegen/rusttable/variable.go` (`TableNodeReader`, the flat node table; `<t>_number` the pre-order numbering with one node per `Rc`) `TestVariableClassBeyondCardConstructsRust` (graph_shared, list_shared byte for byte against `internal/tablewire`) `tables-rust-wire-fuzz-all` `tables-rust-wire-fuzz-negative-control` | ✅ `TestRegionGraphs` `tables-go-wire-fuzz` `tables-go-wire-fuzz-negative-control` | ✅ `tables-cs-leg` `tables-cs-leg-identity-negative-control` `tables-cs-region-fuzz` (flat native and managed graphs; the managed numbering's identity map writes a node two pointers name once, and the control makes it forget a closed node, so list_shared's reference bytes go red) | ✅ `TestJavaPointerGraphSharesOnceRefusesCyclesAndLabelsNodes` `TestVariableClassBeyondCardConstructsJava` `tables-java-wire-fuzz` (graph_shared re-saves to the reference bytes with each shared node written once; a self-pointer and a two-node loop are refused at the OPEN entry, `NodeGraph.recordPointer` in `internal/codegen/javatable/variableform.go`, and a diamond saves and loads back as one node; reversed witnesses plant out the open check and the once-only index and must go red) | ✅ `tables-js-variable-class` `tables-js-variable-class-identity-negative-control` `tables-js-variable-arms` `TestVariableClassBeyondCardConstructsJavaScript` `TestMapsAndUnionArmsOnTheTableWireJavaScript` (`internal/codegen/jstable/variableruntime.go:185`: pre-order at first visit over an identity `Map`, a cycle refused at an OPEN entry, the node table under `TableNodeTableId` at kind 12, pointers at kind 17) | ✅ `tables-dart-identity` `tables-dart-identity-negative-control` `TestWireIdentityMapDart` `tables-dart-variable-class` `TestVariableClassBeyondCardConstructsDart` `conformance` (`internal/codegen/darttable/wireform.go`: pre-order at first visit through an identity map, a node two pointers reach is one record and reads back as one object, two equal nodes stay two, and a cycle is refused at an OPEN entry by name; graph_shared and list_shared byte for byte on the wire surface; the control, `TestWireIdentityMapDartNegativeControl`, takes the OPEN test out of the generated runtime and the probe goes red on the cycle alone) | ✅ `tables-elixir-variable-class` `conformance-negative-control-elixir-shared` (the flat node table under the reserved id, numbered pre-order at first visit; a node two pointers name is one record, byte for byte against the oracle over graph_shared, list_shared and the stream and map instances; an Elixir term cannot hold a cycle, so the OPEN refusal has no input to meet; the control writes the shared node twice and reds wire and json-read) | ✅ `TestGooseTableWireMatchesTheCorpus` `TestGooseTableWireNegativeControl` (its save-numbering blade numbers each node one past its first-visit index and requires the wire rows red) — one flat node table under the reserved id, numbered depth-first pre-order at first visit by an index map keyed by the node, a cycle refused at an OPEN entry by name (`internal/codegen/goosetable/runtime/wire_save.goose:262`); the graphdemo and tblg1 rows hold it | ✅ `TestMooTableSaveRoundTripsEveryInstance` `TestMooTableSaveRefusesACycle` `TestMooTableSaveFlatTableNegativeControl` (`internal/codegen/mootable/runtime/table_save.moo` `number`: one node table numbered depth-first in pre-order at first visit over an identity map keyed by the moo reference, a node whose descent is still open refused as `cycle`; every instance, `graph_shared` and `list_shared` among them, saves byte for byte as the Go engine wrote it. The control ignores the identity map (both shared-node instances leave their wire) and removes the open-descent check (a self-cycle and a two-node cycle save)) | ✅ `TestLobsterVariableClass` `TestLobsterVariableClassNegativeControl` (every pointered manifest unit loads, saves and re-loads over the flat node table byte for byte and report for report against `internal/tablewire`; the control removes the node-index shift, §3.1) | ✅ `TestSquirrelTableSaveRoundTripsEveryInstance` `TestSquirrelTableSaveIdentityNegativeControl` `TestSquirrelBlobNamedTwice` `TestSquirrelWireFuzzAgreesWithTheGoEngine` (the node table read into class instances, and the save's identity map keyed by the instance, `internal/codegen/squirreltable/runtime/save.nut`; with the identity map forgetting a node it numbered, graph_shared and every other shared-node row turn) | ✅ `TestLuauTableSaveRoundTripsEveryInstance` `TestLuauTableSaveNegativeControl` `TestLuauLoadNodeIndexesDoNotShiftAgreesWithTheEngine` (every instance row, pointered roots included, saves byte-identical to the Go engine's corpus wire and loads back; the save's open set refuses a data cycle, `internal/codegen/luautable/runtime/schema_save.luau:390`) | ✅ `TestLuaTableSaveRoundTripsEveryInstance` `TestLuaTableSaveKeepsBlobIdentity`, control `TestLuaNodeIdentityNegativeControl` (`internal/codegen/luatable/runtime/descriptor.lua`, `D.number_nodes`: one node table numbered depth-first pre-order at first visit over an identity map keyed by the Lua reference, a node still open refused as a cycle; the control numbers a node again at every pointer that names it, and `graph_shared` and `list_shared` go red; the cycle refusal has no test of its own yet) |

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
| ✅ `internal/codegen/cpptable/block.go:120` | ✅ `internal/codegen/ctable/block.go:109-125` | ✅ `internal/codegen/rusttable/block.go:583-598` (a slice over the region) | ✅ `internal/codegen/gotable/block.go:243-256` | ✅ `internal/codegen/cstable/block.go:781-790` | ✅ `internal/codegen/javatable/block.go:420-432` | ✅ `internal/codegen/jstable/block.go:382-386` | ✅ `internal/codegen/darttable/block.go:217-247` | ✅ `internal/codegen/elixirtable/block.go:173` (`<F>_count`/`<F>_at`, and a lazy `Stream` walk) `TestElixirBlockRowsAreReachedWithoutAllocatingTheArray` | ✅ `internal/codegen/goosetable/block_form.go` (`<T>Block<F>At(b, i)`, an offset out of the instance; `<T>Block<F>Put`, the typed row) `TestGooseBlockBuilderMatchesTheGoldens` | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2248 — the block reader reaches row i at `off + r * stride` but builds a `Value` per row (`internal/codegen/lobstertable/runtime/block_form.lobster`, `block_read`); no per-row-object-free accessor is emitted | ✅ `internal/codegen/squirreltable/runtime/blockbuild.nut` (`Rows`: base + offset_of, the descriptor's stride) `TestSquirrelBlockReadMatchesTheCorpus` | ❌ #381 — the block read is carried (`TestLuauBlockReadAgreesWithGo`, `TestLuauBlockBuilderMatchesGo`) but lands every row as a fresh table (`internal/codegen/luautable/runtime/schema_blockread.luau:193`): no offset accessor and no caller-owned cursor | ✅ `internal/codegen/luatable/runtime/block.lua:282` (Open keeps one `(offsetOf, count, stride)` per array and a row is `offsetOf + r * stride`, `block.lua:400`; no per-row object) `TestLuaBlockRead` |

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
| ✅ `internal/codegen/cpptable/cook.go:150-173` `tables-cook-open` | ✅ `internal/codegen/ctable/cook.go:157-178` | ✅ `internal/codegen/rusttable/cook.go:206` | ✅ `internal/codegen/gotable/cook.go:542-624` | ✅ `internal/codegen/cstable/cook.go:575-660` `tables-cook-open-cs` | ✅ `internal/codegen/javatable/cook.go:372-455` (an offset residue; the deref bounded to the record) | ✅ `internal/codegen/jstable/cook.go:438-483` | ✅ `internal/codegen/darttable/cook.go:390-446` (an offset residue) | ✅ `internal/codegen/elixirtable/cook.go:145` (a `lead` parameter; a binary has no address) | ✅ `internal/codegen/goosetable/runtime/cook_form.goose` (`cook_check`: a `lead` residue, a slice has no address; the deref bounded to the region) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2249 — no cook yet (docs/PORT-LOBSTER.md, THE CARD MAP, schema-lobster-cook) | ✅ `internal/codegen/squirreltable/runtime/cook.nut` (`Cook`: the header words and the extent, no walk; a `lead` parameter, because a blob has no address) `TestSquirrelCookReadMatchesTheCorpus` | ✅ `internal/codegen/luautable/runtime/schema_cookread.luau` (`C.Check`: the header alone in §7.4's order, the base's residue last, nothing per node; `C.open` then reads the region into a value) | ✅ `internal/codegen/luatable/runtime/cook.lua:527` `TestLuaCookOpenIsTheHeader` (Open is the header and the base alone; Check runs cook-check's two passes) |

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
| ✅ `tables-json-walk` `tables-json-graph-walk` | ✅ `tables-c-json-walk` | ❌ #518 (closed without it) the text form is a codec per table (`<t>_json_write` and `<t>_json_read`, `internal/codegen/rusttable/variabletext.go:1014`), not one generic walk over the descriptors, so there is no unit-independent walk for a json-walk target to compare; the generic walk is a redesign of the text path, owed its own card and its own measurement | ✅ `tables-go-json-walk` | ✅ `tables-cs-json-walk` | ❌ #517 after 3.0.0: performance | ✅ `tables-js-json-walk` (`internal/codegen/jstable/variableform.go`, the text form and the arms' text half between the markers in every `<Package>Variable.js`; `tables-js-json-walk-negative-control`) | ❌ #514 (closed without it) after 3.0.0: performance — the text form is a codec per table (`<t>ToJson` and `<t>FromJson`, `internal/codegen/darttable/textform.go:627`), with maps, unions, fixed and keyed arrays, optionals and flags through a per-unit runtime in `internal/codegen/darttable/textmore.go`, not one generic walk over the descriptors, so there is no unit-independent walk for a json-walk target to compare; the generic walk is a redesign of the text path, owed its own card and its own measurement | ❌ #515 | ✅ `tables-goose-json-walk` (`TestGooseJSONWalkIsOneRuntime`: `internal/codegen/goosetable/runtime/schema_json.goose` byte-identical in every unit that carries the form; the per-table code is the accessor `json_form.go` emits), control `tables-goose-json-walk-negative-control`. The variable class (pointers, `&node`) is not carried yet: M14 | ✅ `TestMooJsonOneWalk` (`internal/codegen/mootable/json_golden_test.go`: one `internal/codegen/mootable/runtime/json.moo`, byte-identical in every unit that carries json; the modules carry three calls into it) `tables-moo-json`; control `TestMooJsonWalkNegativeControl` | ❌ #2250 — no JSON text form yet (docs/PORT-LOBSTER.md, THE CARD MAP, schema-lobster-json-form) | ✅ `TestSquirrelJsonAgreesWithTheCorpus` (one descriptor-driven walk, `internal/codegen/squirreltable/runtime/json.nut`, for every table of the unit) | ✅ `tables-luau-json-walk` `tables-luau-json-walk-negative-control` (`TestLuauJsonWalkIsOneRuntime`: the walk is one runtime module, `internal/codegen/luautable/runtime/schema_json.luau`, byte-identical to the embedded runtime in every unit that carries the text form, and no generated module defines a walk of its own; `TestLuauJsonNegativeControl` sabotages the walk's float writer) | ✅ `TestLuaTextWalkIsOneFileAcrossUnits` (`json.lua`, `textform.lua` and `textformwrite.lua` are one descriptor walk, byte-identical across every manifest unit; a planted byte in one unit's copy must be seen) |

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
| ✅ `tables-hooks` | ✅ `TestCTableWireAllocatorFailures` `tables-c-soak` | — the allocator is the process's `#[global_allocator]`: stable Rust has no per-call allocator parameter (`allocator_api` is unstable), so the variable class's `Rc`, `Vec` and `String` allocate through the caller's global allocator with no context; the fixed, cook and block paths allocate nothing, counted path by path (`tables-rust-alloc` `tables-rust-alloc-negative-control`). Exempt by "Other backends allocate inside their runtime and say so" (docs/SPEC-TABLES.md, What allocates, and what never does): the Rust runtime is not one of the four that thread a caller's pair | ✅ `tables-go-allocator` `tables-go-allocator-negative-controls` (typed activation frames stated below) | ✅ `tables-cs-leg` `tables-cs-leg-allocator-negative-control` (`TableAllocator` pairs, growth failures and native scratch ownership; the control drops the pair's free and the balanced-hooks check goes red) | ✅ the Method's managed-backend clause: the JVM allocates every object and a program cannot hand `new` an allocator, so the generated Java allocates only through the JVM, the caller owns every accelerator `byte[]`, and the COUNT is pinned instead: `tables-java-allocator` (every row a real path through the generated code, gated on its per-iteration ceiling) with `tables-java-alloc-negative-control` (one allocation planted per row reds that row by name) and `tables-java-allocator-runtime-negative-control`, and `tables-java-soak` with `tables-java-soak-negative-control` | — the runtime allocates inside itself and says so; every unavoidable allocation is named in the floor. Exempt by "a managed backend allocates inside its runtime and says so" (docs/PORTING.md M10, Method) | ✅ `tables-dart-alloc` `tables-dart-alloc-negative-control` (a managed backend: the Dart runtime allocates inside itself and says so, docs/SPEC-TABLES.md; the counting test is the allocation gate, which pins zero allocations per record on the fixed load and save and goes red on a planted allocation per record) | — the BEAM allocates every term itself and a program cannot hand it an allocator; the count is pinned instead (`tables-elixir-alloc-audit`). Exempt by "a managed backend allocates inside its runtime and says so" (docs/PORTING.md M10, Method). | — Goose has no allocator to hook: storage is the compiler's data stacks, grown by a pointer bump and freed by a scope exit, so the runtime allocates inside itself. Exempt by "a managed backend allocates inside its runtime and says so" (docs/PORTING.md M10, Method). | — the allocator is the host's (`lua_Alloc`, set at `lua_newstate`), out of a script's reach; generated moo allocates only tables and buffers, through the VM Exempt by "Other backends allocate inside their runtime and say so" (docs/SPEC-TABLES.md, What allocates, and what never does). | — the runtime allocates inside itself: memory is Lobster's compile-time reference counting over the VM's own allocator, out of a program's reach. Exempt by "a managed backend allocates inside its runtime and says so" (docs/PORTING.md M10, Method). | — the VM allocates inside itself and offers a script no allocator to hook. Exempt by "a managed backend allocates inside its runtime and says so" (docs/PORTING.md M10, Method). | — the allocator is the host's (`lua_Alloc`, set at `lua_newstate`), out of a script's reach; generated Luau allocates only tables and buffers, through the VM. Exempt by "a managed backend allocates inside its runtime and says so" (docs/PORTING.md M10, Method) | ✅ (a managed backend: the Lua VM allocates every table and string itself and a script cannot hand it an allocator, so the leg says so and pins the count, docs/PORT-LUA.md) `tables-lua-alloc` `tables-lua-alloc-negative-control` (the VM's own allocation counter over every load row, exact zero where nothing is licensed; a planted allocation per queued slot and per array body turns exactly the rows that pass through it) |

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
| ✅ `internal/codegen/cpptable/cook.go:334-336` | ✅ `tables-c-fuzz` `tables-c-block-layout-model-negative-control` (the block unit's `SCHEMA_TABLE_STATIC_ASSERT`s, `internal/codegen/ctable/block.go`, hold size, alignment, every offset and every pitch to the compiler's model in every C build of it; the control moves one offset in `ir/blocklayout.go` and the C compiler refuses) | ✅ `tables-rust-cook-layout` `tables-rust-cook-layout-negative-control` (`TestCookLayoutIsAssertedAtCompileTime`: every cooked record's size, alignment and field offsets are const asserts in the generated code, evaluated by every cargo build of a unit and for s390x by `tables-rust-big-endian`; the control takes the size and offset asserts out of the emitter) | ✅ `TestBlockLayoutContractRefusesADisagreeingRuntime` `tables-go-layout-contract` `tables-go-layout-contract-negative-control` `internal/codegen/gotable/block.go:491-572` (at package init: the unit as emitted loads, and the same unit with one projection field widened stops at init naming it; the control makes the init's two refusing helpers stop panicking) | ✅ `tables-block` `tables-block-pitch-negative-control` `tables-block-layout-model-negative-control` `tables-block-padding-negative-control` (`internal/codegen/cstable/block.go:436`, the layout check: each record's size and each field's offset against the compiler's model, run once at type initialization, which `tables-block` runs through test/cs-block) | ✅ `tables-java-layout-contract` `tables-java-layout-contract-negative-control`: `TableBlockLayout` and `TableCookLayout` (`emitBlockLayoutFile` in `internal/codegen/javatable/block.go`, its cook half in `cook.go`) hold every record's accessor offsets against its descriptor offsets, and each array's pitch against its row size, once before the first block or cook opens; the gate opens this build's block and cook, then compiles a copy with one accessor offset moved four bytes and requires both opens to throw naming the record. Java has no record layout of its own, so the two generated derivations are the two models | ✅ `tables-js-leg` `tables-js-block-pitch-negative-control` (`internal/codegen/jstable/block.go`, `emitStrideChecks` and `TableBlockLayout.verify`: the first Open checks each array's generated pitch constant against its row object's size, and every record's size, alignment and field extents, and throws naming the block when they disagree; the control moves one pitch constant in the emitter and the leg must go red on that throw) | ✅ `tables-dart-agree` `tables-dart-accessor-negative-control` `tables-dart-slot-negative-control` (the Dart form: the generated row and block getters read at LITERAL offsets and the `TableBlockInfo` / `TableCookInfo` descriptors carry the same layout as constants, two independently emitted views of one layout; `test/dart-tables/agree.dart` reads every record of the block and cook images and a seeded synthetic buffer through both and requires every offset, width and kind to agree; the accessor control moves one getter four bytes in `internal/codegen/darttable/block.go` and the slot control moves one descriptor offset in `cook.go`, each red on its named field alone; at first open the block `Open` checks each array's stride against the build's own, `conformance-negative-control-dart-block`) | ✅ `tables-elixir-accessor-descriptor-agreement` `tables-elixir-accessor-negative-control` `tables-elixir-slot-negative-control` (the Elixir form: the BEAM has no struct layout to assert, so the two derivations the compiler emits, the typed block and cook accessors and the block-record and cook descriptors, read the same synthetic bytes and must agree; scope is plain scalar block fields and pointer cook slots, every other class named `not yet held` by the script; the controls move one accessor four bytes and one slot eight) | ✅ `tables-goose-accessor-descriptor-agreement` `tables-goose-accessor-negative-control` (the Goose form: a record is read where it lies through byte-offset accessors, never a host struct, so there is no compiler-chosen layout to assert; each Open holds an array's pitch against this build's row size from the descriptors, `internal/codegen/goosetable/runtime/block_form.goose`, and the gate holds every baked accessor offset to the descriptors' offset for offset) | ✅ `TestMooLayoutAgreesWithTheCompiler` `TestMooLayoutAgreementNegativeControl` under `test-moo-tables` (the moo form: moo emits no accessor, so a cook node has one read path, the reflective walk `internal/codegen/mootable/runtime/table_cookread.moo`, at the layout each emitted `<Base>Table.moo` registers; the gate loads every cook-carrying unit and holds every registered record's size, alignment, field count and field offsets to the compiler's model, the one every other leg's cook is checked against, `internal/codegen/mootable/layoutagree_test.go`; the control moves `TreeNode.left` eight in the emitted `GraphTable.moo` and that field alone goes red; a cook of another build is refused `wrong_build_version` before any row is read) | ✅ `TestLobsterBlockForgeries` `TestLobsterBlockForgeriesNegativeControl` (a Lobster class has no layout to `static_assert`: the block reader reads at the offsets the emitter writes from the compiler's own model, and `block_open_reason` holds each array's pitch against this build's row before any row is read, `internal/codegen/lobstertable/runtime/block_form.lobster`; the control removes the pitch clause and exactly `block_pitch` goes red) | — no struct layout to check against: a blob is read at the descriptor's offsets, which ARE the model, and the build version refuses a producer that disagrees (`internal/codegen/squirreltable/runtime/block.nut`). Exempt by "has no second model to check the first against in a language with no layout" (docs/SPEC-TABLES.md §19.3). | — a Luau table has no layout to assert; the accelerators read buffers at the compiler's offsets, and the build version refuses a producer that disagrees with them. Exempt by "the LAYOUT CONTRACT (§19.3) has no second model to check the first against in a language with no layout" (docs/SPEC-TABLES.md, What allocates, and what never does) | ✅ `tables-lua-accessor-descriptor-agreement` (the Lua form: a record's layout is derived at run time from the descriptors, so there is no constant baked into generated code to assert; `B.BlockOpen` checks each array's pitch against the row's own derived size, `internal/codegen/luatable/runtime/block.lua`, and the gate holds that derivation to the compiler's, offset for offset) |

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
| ✅ `internal/codegen/cpptable/cpptable.go:513-538` | ✅ `tables-c-soak` `tables-c-big-endian` `tables-c-little-endian-negative-control` `tables-c-big-endian-negative-control` (`table_writer_finish`'s explicit little-endian stores, `internal/codegen/ctable/wire.go`: reversed id words in the memcpy arm turn the host soak red, and a host-order fallback turns the s390x soak red while the host stays green) | ✅ `tables-rust-byte-order` `tables-rust-byte-order-negative-control` (`TestBlockBuildAndCookWriteRust`: the cook writer takes the target order and writes both, each header naming its order in its own byte order, and the C++ reader opens the Rust block; every wire read and write is `to_le_bytes`/`from_le_bytes`; the control makes the cook writer's word composition ignore the target order) | ✅ `conformance-big-endian` `conformance-big-endian-negative-control` `internal/codegen/gotable/wire.go:149-152` (the Go driver cross-built for s390x, under qemu, over every byte-order-neutral surface — wire, message, framing, report, retain, both text directions and the hostile battery — against the goldens a little-endian host wrote; the control puts each generated unit's 32-bit read back to host order and requires the leg green on the host and red on the target) | ✅ `tables-cs-byte-order` `tables-cs-byte-order-negative-control` (every multi-byte wire access in the generated C# is a `BinaryPrimitives` …LittleEndian call, or a fixed-form flat array copy through `TableFixedWire.ReadLittle` / `WriteLittle`, `internal/codegen/cstable/fixedruntime.go`, one bulk copy on a little-endian host and a per-element swap on a big-endian one; the cook and block are native-order forms refused across orders by their magic, I7; the control puts one flat array read back to an unguarded host-order copy and the scan goes red; the native big-endian run is unproven until a big-endian .NET runs here) | ✅ `tables-java-order` `tables-java-order-le-negative-control`: `TableBytes` (`internal/codegen/javatable/block.go`) composes every multi-byte read and write little-endian byte by byte, the cook Open refuses on the magic and then on the order word, and the cook writer takes the target's order (`cookFrom(value, buffer, order)`); with `i64` read through a `ByteBuffer` in the JVM's default order, this reader's own cook refuses and the gate goes red | ✅ `tables-js-leg` `tables-js-byte-order-negative-control` (`internal/codegen/jstable/record.go`, `internal/codegen/jstable/block.go`: every DataView read passes `true`; the control drops the flag from two accessor reads in the emitter and the leg goes red on the accessor/descriptor agreement) | ✅ `tables-dart-byte-order` `tables-dart-byte-order-negative-control` (`TestDartByteOrderIsExplicit`: every multi-byte ByteData call in every corpus unit's generated Dart names `Endian.little`, or, in the cook writer, the `order` parameter the target's order arrives in; the control strips one call's order and the scan names it) and the foreign-file half `tables-dart-order` `tables-dart-order-negative-control` (the magic, then the order word, `internal/codegen/darttable/block.go` and `cook.go`) | ✅ `TestElixirSegmentsNeverConsultTheHost` `TestElixirSegmentOrderScanGoesRed` (on the BEAM a segment's order is a modifier and `native` is the one spelling that reads the host's order; no segment in the generated corpus spells it, every wire read names `little` (`internal/codegen/elixirtable/block.go`), the cook writer takes the target's order as a parameter (`cookwrite.go`), and the control plants `native` in both modifier positions) | ✅ `TestGooseTableWireMatchesTheCorpus` `TestGooseTableWireNegativeControl` (its save-byte-order blade writes a multi-byte value high byte first and requires the wire rows red) — on the table wire every multi-byte read and write is little-endian composition by shifts (`internal/codegen/goosetable/runtime/wire_load.goose:185`, `internal/codegen/goosetable/runtime/wire_save.goose:69`); the accelerators take the order as the write's parameter, held by `tables-goose-accel-negative-control` (cook-order: cook-write red) | ✅ `TestMooCookWriteMatchesTheTool` `TestMooCookWriteNegativeControls` `TestMooCookFixturesAndForgeries` `TestMooCookNegativeControls` (every table read and write goes through the pinned moo's `buffer` library, little-endian on every host by definition, so no read consults the host's order; the cook writer takes the target's order as a parameter (`table_cookwrite.moo` `W.cook(root, value, big)`) and both orders equal the tool's files; the cook read compares the magic in the machine's order and then the order word (`table_cookread.moo` `C.open`). The controls ignore the order in the writer and remove the order-word check in the reader) | ✅ `TestLobsterBlockForm` `tables-lobster-block-lead` `tables-lobster-order-negative-control` (every multi-byte read and write is a `read_*_le` / `write_*_le` builtin, little-endian by definition, so no read consults the host; `block_open_reason` and the cook's Open refuse on the magic and then the order word; the control blinds the order word and the block and cook forgery batteries go red) | ✅ `TestSquirrelBlockReadMatchesTheCorpus` `TestSquirrelBlockByteOrderNegativeControl` `TestSquirrelCookReadMatchesTheCorpus` (`internal/codegen/squirreltable/runtime/load.nut` and `block.nut`: every word composed from its bytes, little-endian; a blob's `readn` is the host's order and no reader calls it; with the block's 32-bit word composed big-endian the block gate turns) | — the pinned Luau's `buffer` library reads and writes little-endian on every host by definition, so no read can consult the host's order; the table runtime reads only through it, and the `cook-foreign` and `block-foreign` surfaces hold the refusal. Exempt by "no order gate stands beside them because there is no platform query for one to catch" (docs/SPEC-TABLES.md, What allocates, and what never does) | ✅ `TestLuaWireByteOrderIsExplicit`, control `TestLuaWireByteOrderNegativeControl` (every `string.pack`/`string.unpack` in the variable, retained, message and fixed wire runtimes names `<`; the control turns one of `load.lua`'s reads to the host's order and the scan names it). The cook writer takes the target's order (`internal/codegen/luatable/runtime/cookwrite.lua`), and the block and cook read the magic in the machine's order then the order word, held by I7's gate |

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
| ✅ `tables-json-graph-walk` | ✅ `TestCTableWireGraphIdentity` `tables-c-json-label-negative-control` `TestCTableWireBlobIdentity` (a shared node written in full twice, no label, turns the probe's JSON round trip red on the engine's bytes) | ✅ `tables-rust-node-label` `tables-rust-node-label-negative-control` (`TestVariableClassBeyondCardConstructsRust`: graph_shared, list_shared and classprobe byte for byte against the oracle's text, and conformance json-read/json-write over graph_shared; the control renames the `&node` label the emitted writer opens a shared node with) | ✅ `TestRegionGraphs` `tables-go-json-walk` `tables-go-graph-label-negative-control` (the control makes the emitter's text writer write a shared node as plain copies with no `&node` label, and the text read stops sharing it) | ✅ `tables-cs-leg` `tables-cs-leg-label-negative-control` (shared graph labels and forward references; the control never writes the `&node` label, so list_shared's text reads back as two nodes and its round trip goes red) | ✅ `TestJavaPointerGraphSharesOnceRefusesCyclesAndLabelsNodes` `TestJavaPointerGraphNegativeControl` (graph_shared's text is the committed golden byte for byte, `"&node": N` at the first appearance and the bare label after; the control writes every shared node in full and must go red on toJson) | ✅ `tables-js-variable-class` `tables-js-variable-class-label-negative-control` `TestVariableClassBeyondCardConstructsJavaScript` `TestMapsAndUnionArmsOnTheTableWireJavaScript` (`internal/codegen/jstable/variableruntime.go:1080` writes `&node` at the first appearance and the label after; `graph_shared` and `maps_pointers` read back to the oracle wire, and a label inside its own definition is refused) | ✅ `conformance-negative-control-dart-share` (the gate is the conformance matrix's dart json-write surface over graph_shared and list_shared against the corpus text; `internal/codegen/darttable/textform.go`'s `_writePtr` writes a shared node with `&node` at its first appearance and names it by its label after; the control takes the label out, so a shared node is written in full twice, and json-write goes red on the shared-node rows alone) | ✅ `tables-elixir-variable-class` `conformance-negative-control-elixir-label` (`internal/codegen/elixirtable/variableform.go` `json_node`: a shared node is defined once under `&node` and named by its label after; graph_shared and list_shared read and write byte for byte through json-read and json-write; the control writes the node in full at every pointer and reds json-write alone) | ❌ #2236 — the fixed class's text form is carried (`json_form.go`); a pointer, and so the `&node` label, is refused by name in `jsonUncarried` until the variable class lands (card schema-goose-vt-variable-class, then this row) | ✅ `TestMooJsonWriteAndReadGates` `TestMooJsonNodeLabelNegativeControl` `TestMooJsonHostileVerdicts` `tables-moo-json` (`internal/codegen/mootable/runtime/json.moo`: a shared node is defined once as `{ "&node": N, ... }` and referenced by its label after; `graph_shared`, `list_shared` and `map_full` read and write byte for byte, and the label json-hostile rows refuse. The control writes a shared node in full at every appearance (json-write red on `graph_shared`) and resolves a label's reference to null (json-read red on `graph_shared`)) | ✅ `TestLobsterJsonMatchesTheReference` `TestLobsterJsonNegativeControl` (`internal/codegen/lobstertable/runtime/json.lobster`: a shared node is written once with `&node` at its first appearance and named by its label after; every graphdemo instance reads and writes against the corpus text; the control respells the label and exactly the texts that refer back to a shared node go red) | ✅ `TestSquirrelJsonAgreesWithTheCorpus` `TestSquirrelJsonLabelNegativeControl` (`internal/codegen/squirreltable/runtime/json.nut` `writePointer`: graphdemo's shared nodes and forward references, read and written; with a shared node written in full at every appearance, graph_shared's json-write turns) | ✅ `TestLuauJsonAgreesWithTheCorpus` `TestLuauJsonSharedNodeNegativeControl` (`graph_shared` reads to the oracle wire and writes the oracle text byte for byte, `&node` at the first appearance and the label after; a label inside its own definition is refused, `read_pointer` in `internal/codegen/luautable/runtime/schema_json.luau`; the control writes a shared node inline at every pointer in a scratch runtime and requires graph_shared's json-write red) | ✅ `TestLuaJsonAgreesWithTheCorpus`, control `TestLuaJsonNodeLabelNegativeControl` (`internal/codegen/luatable/runtime/json.lua`: a shared node is defined once as `{ "&node": N, ... }` and referenced by its label after; `graph_shared`, `list_shared` and `map_full` read and write byte for byte, and the `label-*` json-hostile rows refuse; the control writes every node in full and `graph_shared` and `list_shared` go red on json-write) |

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
| ✅ `internal/codegen/cpptable/pointers.go:150` `conformance` | ✅ `internal/codegen/ctable/graph.go:24` `TestCTableCookCanonical` `tables-c-wire-fuzz` | ✅ `internal/codegen/rusttable/variablecodec.go:527` `tables-rust-number-walk-negative-control` `TestMapsAndUnionArmsOnTheTableWireRust` `TestCookWriteRust` (Rust has no Lock, so there is no pack measure or pack; the numbering walk `<t>_number` is ONE walk in declaration order that descends by-value edges, maps and set arms in place and skips an absent optional, and the save and the cook writer (`internal/codegen/rusttable/cookwrite.go:469`) lay nodes in its list; the grouped walk, planted by overlay, turns `arms_ptr` and `maps_pointers` red on the wire; `stream_arm_first` is green on `wire`, `json-read` and `cook-write`) | ✅ `internal/codegen/gotable/regions.go` (`tableRegionEdges`, `tableNumber`) `TestAllocatorOwnershipAndStandaloneWriters` `tables-go-builders` | ✅ `tables-cs-pack-negative-control` `tables-cs-leg` (root and node extent equality) | ❌ #349 after 3.0.0: performance — Java has only the numbering walk (`buildGraph` in `internal/codegen/javatable/variableform.go`); it has no pack-measure or pack walk to share it with (`loadMeasure`, `variableform.go:1689`, measures a load region, not a pack), so no Java test can hold numbering, pack measure and pack to one declaration-order DFS; `TestVariableClassBeyondCardConstructsJava` round-trips the wire and text only | ✅ `internal/codegen/jstable/variableruntime.go:207` (`TableVariableNumber`) `TestMapsAndUnionArmsOnTheTableWireJavaScript` `TestVariableClassBeyondCardConstructsJavaScript` (ONE edge walk in declaration order, by-value edges and set arms descended in place, numbers the nodes; `Measure` is the save's length, and the save and the cook lay nodes in that walk's list; `arms_ptr`, `maps_pointers` and `list_before_pointer` go red under the grouped walk; the OPTIONAL presence gate, schema#526: an absent `?T` or `?[N]T` is `null` and the walk returns on it before it descends, `TestOptionalVariableClosureJavaScript` against the Go oracle for save, measure, load, text and cook, red with the gate removed, and G1's four `wire` rows in `conformance`) | ✅ `internal/codegen/darttable/wireform.go:2659` `tables-dart-variable-arms` `conformance` (`<T>Number` is ONE edge walk in declaration order that descends by-value edges, map entries and set arms in place; the save lays nodes in that walk's list and the cook writer numbers through the same walk, `internal/codegen/darttable/cookwrite.go:329`; stream_arm_first and list_before_pointer on the wire, json-read and cook-write surfaces) | ❌ #349 | ❌ #2236 — the numbering walk is carried (`internal/codegen/goosetable/runtime/wire_save.goose:262`, held by `TestGooseTableWireMatchesTheCorpus`); Lock's sizing and pack are owed with the block and cook cards | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2252 — the save numbers the nodes in one walk (`sg_visit_edges`) and writes in another over a growing buffer with no pack measure (`internal/codegen/lobstertable/runtime/schema_save.lobster`); no gate holds the two to one skeleton | ✅ `internal/codegen/squirreltable/runtime/save.nut` (`number` once; `measure` is the save's own length) `TestSquirrelTableSaveRoundTripsEveryInstance` | ❌ #381 — the variable save is carried (`TestLuauTableSaveRoundTripsEveryInstance`); that the numbering, the measure and the pack share one walk is not yet held by a gate | ✅ `TestLuaOnePointerWalk` `TestLuaOnePointerWalkRowsAgreeWithTheEngine` `TestLuaOnePointerWalkNegativeControl` `internal/codegen/luatable/runtime/descriptor.lua:508` (ONE declaration-order walk, `D.number_nodes`, that descends every by-value edge in place; the save, the retaining save, the cook's layout and pack, and the JSON text form's reference count each call it, so the four copies the audit found (and retain.lua's, which had already lost the optional's presence gate) are gone; Measure is the length of Save, and the cook's sizing and pack share `carve_extent` with the writer nil or set; the structural test holds one definition and four callers, the rows `stream_arm_first`, `list_before_pointer` and `optional_absent_no_edge` hold the saved wire and the cook to the engine's bytes, and the grouped-walk plant turns `stream_arm_first` red on its saved wire and its cook) |

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
| ✅ `internal/codegen/cpptable/codecs.go:588` `test/tables/main.cpp:6814` `TestReportRowsDecodeThroughTheEngine` | ✅ `TestCTableWireDefaultRecovery` `tables-c-wire-fuzz` | ✅ `TestMapsAndUnionArmsOnTheTableWireRust` `internal/codegen/rusttable/mapsarms.go:159` `internal/codegen/rusttable/mapsarms.go:551` (presence is the `Option` beside the array and never the count: `None` writes nothing, `Some` rides at its live count, zero included — `opt_present`'s `o_list: []` — and a fixed array whole; load sets `Some` only after the element kind passed (`mapsarms.go:1015`), so a foreign element kind leaves the field absent; the Rust leg answers `message_trace`, both spellings at three depths, byte for byte on `wire`, `json-read`, `json-write` and `cook-write` in the conformance matrix) | ✅ `TestDefaultsAndOptionalArrays` `conformance` | ✅ `tables-cs-leg` (optional arrays across file, message and cook walks) | ❌ #392 after 3.0.0: performance | ✅ `tables-js-variable-arms` `TestMapsAndUnionArmsOnTheTableWireJavaScript` `TestNestedTypesAndFixedArraysOnTheTableWireJavaScript` (`internal/codegen/jstable/variablearms.go:248`: absent writes nothing and a present `?[..N]T` or `?[N]T` rides at count zero or all-zero elements, `opt_present` and `opt_trio_zero` against the oracle; a foreign element kind returns before presence is set) | ✅ `tables-dart-variable-arms` `TestMapsAndUnionArmsOnTheTableWireDart` `conformance` (presence is the optional field's null and never the count: an absent optional array writes nothing and a present one rides at count zero or as a whole fixed array; a foreign element kind puts the field back to absent, `internal/codegen/darttable/wirearrays.go:213`; the optional rows byte for byte against the oracle) | ❌ #392 | ✅ `TestGooseTableWireMatchesTheCorpus` — the save gates an optional array's whole framing on presence (the counted spelling at its live count, zero included; the fixed one whole) and the load marks presence where the field rode, never on a foreign element kind (the tblm1/tblm2 rows) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ✅ `TestLobsterTableLoadMatchesTheReports` `TestLobsterTableSaveRoundTripsEveryInstance` (a `Slot`'s `present` flag rides beside the array walk in `internal/codegen/lobstertable/runtime/load.lobster`; every optional-array instance of the manifest loads, reports and saves as the reference does) | ✅ `TestSquirrelCarriesOptionalArrays` `TestSquirrelTableSaveRoundTripsEveryInstance` | ❌ #381 — optional arrays and maps load and save against the engine (`TestLuauMapPresence`, `TestLuauOptionalVariableClosureTable`); the presence companion beside the array walks is not yet held by a negative control | ✅ `TestLuaTableSaveRoundTripsEveryInstance` (`internal/codegen/luatable/runtime/save.lua:407`: an optional array rides on `<field>_present`, never on its count, so `chain_optional_empty` rides present and empty) `TestLuaOptionalVariableClosure` |
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
| ✅ `tables-blob-span-negative-control` | ✅ `tables-c-blob-span` `tables-c-blob-span-negative-control` (`internal/codegen/ctable/arena.go`, `table_arena_grab_span`: a 66,000-byte blob with nodes allocated after it, read back from the arena, the locked region and the wire, plain and under ASan + UBSan; the control denies both span branches and the nodes after the blob overwrite it) `TestCTableWireBlobIdentity` | — no arena and no slab: the Rust leg's storage owns every node and blob through `Rc` (a `*bytes` is an `Option<Rc<[u8]>>`, `internal/codegen/rusttable/variablecodec.go:252`), one allocation at exactly its length, so there is no slab for a large blob to outgrow and the bump-into-a-bigger-slab shape cannot arise; `blob_large`'s 66,000-byte `*bytes`, past the reference's 64 KB slab, round-trips the oracle bytes in `TestVariableClassBeyondCardConstructsRust`. Exempt by "Other backends allocate inside their runtime and say so" (docs/SPEC-TABLES.md, What allocates, and what never does) | ✅ `TestBlobSpanHoldsAfterLaterAllocations` `tables-go-blob-span-negative-control` | ✅ `tables-cs-leg` `tables-cs-leg-blob-span-negative-control` (a node larger than a slab takes its own span, `TableWorker.Raw`; the leg fills a 100,000-byte blob, allocates after it and requires its bytes intact, and the control makes the span test never fire) | — no arena and no slab: a Java `*bytes` is its own `byte[]` and a `*string` its own `String`, each allocated by the JVM at exactly its length, so there is no slab for a large blob to outgrow and the bump-into-a-bigger-slab shape cannot arise. Exempt by "Other backends allocate inside their runtime and say so" (docs/SPEC-TABLES.md, What allocates, and what never does). | — no arena and no slab: every blob is its own `Uint8Array` or string at exactly its length, so there is no slab for a large blob to outgrow, and `blob_large`'s 66,000-byte `*bytes`, past the reference's 64 KB slab, round-trips the oracle bytes in `TestVariableClassBeyondCardConstructsJavaScript`. Exempt by "Other backends allocate inside their runtime and say so" (docs/SPEC-TABLES.md, What allocates, and what never does) | — no arena and no slab: every blob is its own `Uint8List` or `String` at exactly its length (`internal/codegen/darttable/wireform.go`), so there is no slab for a large blob to outgrow; exempt by "a managed backend allocates inside its runtime and says so" (docs/PORTING.md M10, Method). blob_large's 66,000-byte `*bytes` round-trips the oracle bytes on the conformance wire and cook-write surfaces | — the BEAM owns all memory: a blob is one managed binary of exactly its length, so there is no arena and no slab for a large blob to outgrow. Exempt by "Other backends allocate inside their runtime and say so" (docs/SPEC-TABLES.md, what allocates). | — no arena and no slab: a loaded blob is a view of its bytes in the input (`LoadNode` off and len, `internal/codegen/goosetable/runtime/wire_load.goose`), nothing copied, so there is no slab for a large blob to outgrow; `blob_large`'s 66,000-byte `*bytes` rides the wire surface. The runtime allocates inside itself. Exempt by "a managed backend allocates inside its runtime and says so" (docs/PORTING.md M10, Method). | — no arena and no slab: every blob is one moo `buffer` of exactly its length (`internal/codegen/mootable/runtime/table_load.moo`), so there is no slab for a large blob to outgrow; `blob_large`'s 66,000-byte `*bytes` rides the wire, json and cook-write surfaces (conformance, every moo surface passing) Exempt by "Other backends allocate inside their runtime and say so" (docs/SPEC-TABLES.md, What allocates, and what never does). | — no caller-visible arena: a blob is one Lobster `string` the VM allocates at its exact size, so there is no slab to outgrow. Exempt by "Other backends allocate inside their runtime and say so" (docs/SPEC-TABLES.md, what allocates). | — no arena and no slab: every blob is its own Squirrel blob or string at exactly its length, so there is no slab for a large node to outgrow. Exempt by "a managed backend allocates inside its runtime and says so" (docs/PORTING.md M10, Method). | — no arena and no slab: every blob is one Luau `buffer` of exactly its length (`internal/codegen/luautable/runtime/schema_load.luau` `copy_out`), so there is no slab for a large blob to outgrow; `blob_large`'s 66,000-byte `*bytes` rides the wire, json and cook-write surfaces. Exempt by "a managed backend allocates inside its runtime and says so" (docs/PORTING.md M10, Method) | — no arena and no slab: every blob is one Lua string of exactly its length, allocated by the Lua VM, so there is no slab for a large blob to outgrow; `blob_large`'s 66,000-byte `*bytes` rides the wire surface. Exempt by "Other backends allocate inside their runtime and say so" (docs/SPEC-TABLES.md, What allocates, and what never does). |


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
| ✅ `internal/codegen/cpptable/arms.go` `tables-wire-fuzz-arm-width-negative-control` `tables-wire-fuzz-arm-terminator-negative-control` | ✅ `tables-c-collections-fuzz` `tables-c-wire-fuzz` | ✅ `tables-rust-variable-arms` `TestMapsAndUnionArmsOnTheTableWireRust` `tables-rust-wire-fuzz-all` `internal/codegen/rusttable/mapsarms.go:425` (an arm header is a field header — the arm id reference, the arm's own kind, `L`, the payload — and `emitArmPayload` dispatches into the field's own body, array, string, enum and scalar emitters; a payload-free arm is kind 32 at `L = 0`; the harness's arm-length and terminator passes run over every root this leg carries, the message and stream unions included, with 0 divergences against the engine; still refused by name: a `wstring(N)` array arm) | ✅ `TestRegionUnionAndBlobValues` `TestUnionPointerArrayReaders` `tables-go-measure-negative-controls` `tables-go-wire-fuzz` | ✅ `tables-cs-leg` `conformance-negative-control-cs` (all field-shaped arms) | ✅ `tables-java-variable-arms` `TestMapsAndUnionArmsOnTheTableWireJava` `TestJavaFixedGeneralArmPayloads` `tables-java-wire-fuzz` (a union's arms are one struct's fields, `variableUnionStruct` in `internal/codegen/javatable/variableunions.go:18`, emitted by the field emitters under a field header; the harness's arm-length and terminator passes run over every root this leg carries, 0 divergences against the engine) | ✅ `tables-js-variable-arms` `tables-js-variable-arms-length-negative-control` `TestMapsAndUnionArmsOnTheTableWireJavaScript` `TestNestedTypesAndFixedArraysOnTheTableWireJavaScript` (an arm is the field line's own descriptor under a field header, and reads through the field's array and body readers; its scalar payloads are `internal/codegen/jstable/variablearms.go:391`, the port of the tool's `encodeArm`; every arm kind in the probes, flags, `[N]T` and nested-type arms included, rides byte for byte; still refused by name: an optional arm, a pointer to a union, `wstring(N)` and string/bytes arrays) | ✅ `tables-dart-variable-arms` `TestMapsAndUnionArmsOnTheTableWireDart` `tables-dart-wire-fuzz` (`internal/codegen/darttable/wireunion.go`: an arm header is a field header — the arm id reference, the arm's own kind, `L`, then the payload through the field's own emitters; a payload-free arm is kind 32 at `L = 0`; the harness's arm-length and terminator passes run over every root this leg carries) | ✅ `tables-elixir-variable-arms` `conformance-negative-control-elixir-arm` (an arm header is a field header and its payload reads through the field reader, `read_arm` hands it to `read_payload` in `internal/codegen/elixirtable/variableform.go`; conformance over messagedemo, tblm1/tblm2 and tbla1/tbla2; the control reads an arm under another kind as the declared arm and reds the report surface; the arm-width and arm-terminator framing-damage controls ride with I15, whose damaged-input comparison is being rewritten to the discard-on-damage ruling) | ✅ `TestGooseTableWireMatchesTheCorpus` `TestGooseTableWireNegativeControl` — an arm's payload is written by the field's own value writers (`internal/codegen/goosetable/runtime/wire_save.goose:728`), behind an arm header that is a field header (`internal/codegen/goosetable/runtime/wire_save.goose:713`); a retyped arm is a counted kind mismatch on load | ❌ #381 — union arms load and save against the engine on every surface and both wire-fuzz arms, but `table_load.moo` `read_arm` still reads string, text and pointer arms with its own payload code and `read_union` reads a table arm's body itself, beside `read_field`; the method is one reader. Owed: the arm read through `read_field` (the Luau leg's `read_arm` over `read_field`, rowan/luau-300-wip a540a6717, is the shape), a source scan and its control, about 2 hours | ✅ `TestLobsterVariableClass` `TestLobsterUnionArmNegativeControl` (union arms of any field type, payload-free arms and arrays of unions read through the field-line code, `arm_read` over `shape(arm_key(a)).arm`, and are held report for report to the Go engine, a retyped arm counted as a kind mismatch; the control lands no value from an arm read and the value comparison goes red) | ❌ #366 — an arm is a descriptor field line (`internal/codegen/squirreltable/squirreltable.go`) and the save writes an arm payload and a field through one value writer (`runtime/save.nut`), byte for byte against the engine (`TestSquirrelCarriesUnionsInTableClosures`, `TestUnionArmRefusalsNameTheUnion`, `TestSquirrelWireFuzzAgreesWithTheGoEngine`); but the load reads an arm through `readArmPayload`, a second per-shape reader beside `readField` (`runtime/load.nut`), the shape this row refuses, and no structural check or control holds the two to one reader | ✅ `TestLuauArmIsAFieldLine` `tables-luau-arms` `tables-luau-arms-negative-control` `TestLuauArmFieldLineNegativeControl` (`read_arm` in `internal/codegen/luautable/runtime/schema_load.luau` checks framing alone and reads every payload through `read_field`, the field's own reader, with the arm's L standing in for the field's; a nested union arm hands `read_field` its payload, the arm line the outer L frames; the control plants a per-shape reader and requires the scan to name it) and against the engine `TestLuauLoadUnionFramingAgreesWithTheEngine`, the conformance `message_arms` and `nested_arm_trailing` rows, and the fixed reader by id across versions (`TestLuauFixedVersioningUnionTagBothPlans`) | ✅ `TestLuaArmIsAFieldLine` `TestLuaArmFramingAgreesWithTheEngine` `TestLuaArmFramingNegativeControl` `tables-lua-wire-fuzz-differential` `internal/codegen/luatable/runtime/load.lua:1358` (`read_payload` reads a field's payload ONCE, and both `read_field` and `read_arm` call it: the arm's L stands in for the field's own length where the kind has one and frames the fixed width where it does not, the arm header is a field header so a retyped arm is a kind mismatch, and what a field would default or stop on the union reads as None; the structural scan holds that neither function names a per-shape reader, and a per-shape string branch planted into `read_arm` turns it; arms of `tables/arms` Carry framed hostilely, a scalar arm one byte long, a body arm with slack after its terminator, are read by the Lua load and by internal/tablewire and agree on report and re-saved bytes, and removing the scalar width check or the body's consumed-equals-L check turns exactly its own rows) |

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
| ✅ `tables-maps` `tables-json-map-walk` `tables-maps-negative-controls` `tables-maps-cook-check-negative-control`, and the TOOL's wire and text halves and its cook-check clause (`TestTheToolWritesTheReferencesMapBytes`, `TestCookCheckMapSlot`) | ✅ `TestCTableWireMaps` `tables-c-map-sort-negative-control` `TestCTableMapWideningCount` `tables-c-collections-fuzz` (the save's ascending sort removed turns the probe red on the engine's bytes) | ❌ #502 the reader keeps every rule — ascending against the last key that landed, a duplicate last-wins whole, a descending key stops the map, a key past its bound skipped and counted `clamped`, a foreign key kind empties it once (`internal/codegen/rusttable/mapsarms.go`) — and the writer holds the sort at save and in the text (`tables-rust-map-sort` `tables-rust-map-sort-negative-control`: map_full's Fleet with every map reversed at every depth saves the pinned bytes and writes the same text, and each writer's sort made a no-op reds its own clause); what is owed is the LAYOUT: a map is a `Vec` of entries, the cooked row carries the holder as a raw `[u64; 2]` with no `Find`, and the cook writer emits no `<T>_cook_write` for a map-bearing root, so the sixteen-byte holder, the entries in the holder's node extent and the refusal of an UNREACHED map slot are not built | ✅ `tables-go-containers` `tables-go-containers-negative-controls` `tables-go-wire-fuzz` | ✅ `tables-cs-leg` `tables-cs-leg-map-order-negative-control` `tables-cs-wire-fuzz` `tables-cs-builder-fuzz` (stable sorted entries and native holder extents; the control makes the builder map append an entry instead of inserting it at its key, and the ordered-iteration check goes red) | ❌ #502 — the value form keeps every rule: the reader is ascending against the last key that landed, a duplicate last-wins whole, a descending key stops the map with `malformed`, a key past its bound is skipped and counted `clamped`, and a foreign key kind empties it once (`internal/codegen/javatable/variablemaps.go`), held by `tables-java-variable-arms` and `tables-java-wire-fuzz`; the writer holds the sort (`TestJavaMapWriterHoldsTheSort`, with a reversed witness). What is not carried is the region: the cook lays a map holder out as a sixteen-byte RECORD slot with no entry layout and a one-byte accessor (`FleetRow.ships`), so there is no holder reference and count and no entry array in the node extent, and no cook-write row exercises a map | ✅ `tables-js-variable-arms` (`TestMapsAndUnionArmsOnTheTableWireJavaScript`, `TestNestedTypesAndFixedArraysOnTheTableWireJavaScript`: the ascending entry array, `[N]T` map values in `map_cells` and `map_pairs` included, byte for byte against the oracle; `TestMapWriterSortJavaScript`: the WRITER holds the sort, a Map built out of key order saves, measures and writes text in ascending order, with a reversed witness that drops the sort), the cook's entry arrays and holder extents through the conformance cook-write rows, `tables-js-wire-fuzz-all`. The value form is a JavaScript Map; the region (sixteen-byte holder, entries in the node extent) is the cook's. A key other than `string(N)`, `string` or an integer up to 64 bits is refused by name as §2.8 refuses it | ✅ `tables-dart-map-sort` `tables-dart-map-sort-negative-control` `TestMapWriterSortDart` `tables-dart-variable-arms` `TestWireMapRepeatKeepsEarlierDart` `TestWireMapStringKeyOverBoundDart` `tables-dart-cook-map-field` `conformance` (the ascending entry array byte for byte against the oracle; the WRITER holds the sort — a Dart map field is a List of entries in the caller's order, and `_stableSorted` orders it in the save, the numbering and the text, `internal/codegen/darttable/wireunion.go`; the control, `TestMapWriterSortDartNegativeControl`, makes the sort a no-op and the probe goes red on the saved bytes; a duplicate text key is last-wins and counted, list-map-dup-key on json-hostile) | ✅ `tables-elixir-variable-arms` `conformance-negative-control-elixir-mapsort` (maps read and write on the variable wire and the text, ascending entry arrays byte for byte against the oracle over every mapdemo instance on wire, json-read and json-write, with map_key_widen and map_depth_root_kind on report; the writer holds the sort, `map_entries` in `internal/codegen/elixirtable/variableform.go`, and the control sorts descending and reds wire; the cook writer refuses a map-bearing root as `schema cook` itself does, docs/SPEC-TABLES.md §7.2; the reader's damaged-entry behavior rides with I15, under the discard-on-damage ruling) | ❌ #2236 — the table wire carries maps in ascending key order (`TestGooseTableWireMatchesTheCorpus`, the mapdemo rows); the holder's node-extent layout is the region's, owed with the block and cook cards | ✅ `TestMooMapWriterHoldsTheSort` `TestMooMapWriterSortNegativeControl` (the WRITER holds the sort: every mapdemo instance saved as read and saved with every map's entries reversed is the same committed wire, `internal/codegen/mootable/map_sort_test.go`; with `table_save.moo` `map_order`'s sort removed the multi-entry rows turn) and `tables-moo-wire-fuzz` (the reader's ascending, duplicate, key-kind and header rules against the engine; the map and keyed header counts are read against the enclosing body, pinned as `crews_map_count_past_l`, `slots_map_count_past_l` and `fleet_map_count_past_l` in `tables-moo-wire-fuzz-pinned`) | ✅ `tables-lobster-maps` `tables-lobster-maps-sort-negative-control` `TestLobsterCollections` (the WRITER holds the sort: every map of every mapdemo instance reversed at every depth, saved, and held to the engine's save; the reader's ascending check against the conformance corpus; the control takes the sort's compare out) | ✅ `TestSquirrelMapWriterSort` `TestSquirrelMapWriterSortNegativeControl` `TestSquirrelMapsMatchTheWire` `TestSquirrelMapsInJson` `TestSquirrelMapWireCountRefusal` (the WRITER holds the sort: every mapdemo row saved with its root maps reversed is the corpus wire, and the numbering walks the same ascending order, `internal/codegen/squirreltable/runtime/save.nut` `mapWriteOrder`; with the sort removed 11 of 13 rows turn; and the mapdemo corpus unit on every surface) | ✅ `tables-luau-maps` `tables-luau-maps-negative-control` (the WRITER holds the sort: every mapdemo instance saved as read and saved with every map's entries reversed is the same committed wire, `internal/codegen/luautable/map_sort_test.go`; with `schema_save.luau` `map_order`'s sort removed 11 of the 13 map rows turn) and `tables-luau-wire-fuzz` (the reader's ascending, duplicate, key-kind and header rules against the engine; the map header is read against the parent, pinned as `map_header_count_past_length` in `tables-luau-wire-fuzz-vectors`) | ✅ `TestLuaTableSaveRoundTripsEveryInstance`, control `TestLuaMapWriterSortNegativeControl` (the writer sorts every map's keys, `D.map_entries` in `internal/codegen/luatable/runtime/descriptor.lua`; the control reverses the sort and `map_full`, `map_pairs` and `map_docs` go red) and the reader's ascending check in `load.lua` |

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
`LoadMessages`, `MeasureMessages` and `SaveMessages` for every `fixed table`
root, and none for a variable table (the form is fixed tables only, §3.3),
`TableVocabulary` and the three unit-scope entry points `Announce`,
`AnnounceMeasure` and `AnnounceRead`, each in that language's own naming
convention. **The verbs are PLURAL** because the form's primitive is a batch of
bodies of one root and a single message is the batch of one. C++, C and the
independent tool engine carry the bitpacked form-`2` path. The C conformance
and mutation drivers exercise fixed message roots. Retained
unknowns use caller-owned byte and identity stores. Replacing an occurrence
scans the retained-record log, whose size is bounded by the caller's storage.
`tables-c-retain` checks
file load retention and retained file saves, and
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
| ✅ `tables-wire-fuzz` `tables-wire-fuzz-negative-control` | ✅ file and message forms: `tables-c-wire-fuzz`, `tables-c-wire-fuzz-negative-control`, `tables-c-message-negative-control` | ✅ `tables-rust-variable-class` `tables-rust-variable-arms` `tables-rust-variable-fixed-class` (the file form, byte for byte against the Go oracle) `tables-rust-message-batch` (the message form) `tables-rust-wire-fuzz-all` `tables-rust-wire-fuzz-negative-control`; conformance wire 97/97 and report 61/61 (#518 closed) | ✅ `tables-go-wire-fuzz` `tables-go-wire-fuzz-negative-control` `tables-go-retain-wire-fuzz` (the fixed message form and the file form) | ✅ `tables-cs-wire-fuzz` `tables-cs-region-fuzz` `tables-cs-retain-fuzz` `conformance-negative-control-cs` | ✅ `tables-java-variable-class` `tables-java-variable-arms` (the file form, byte for byte against the reference) `tables-java-message-batch` (the message form) `tables-java-wire-fuzz` `tables-java-wire-fuzz-negative-control` | ✅ `tables-js-wire-fuzz-all` `tables-js-wire-fuzz-negative-control` `cards-js-wire-fuzz` `cards-js-retain-fuzz` `tables-js-variable-class` `tables-js-variable-arms` `tables-js-variable-fixed-class` (the file form, byte for byte against the Go oracle) and `TestMessageFormBatchJavaScript` `TestMessageFormConformanceJavaScript` (the message form) | ✅ `tables-dart-wire-fuzz` `tables-dart-wire-fuzz-negative-control` `tables-dart-variable-class` `tables-dart-variable-arms` `tables-dart-variable-fixed-class` (the file form, byte for byte against the Go oracle) and `tables-dart-message-batch` (the message form) | ✅ `tables-elixir-variable-class` `conformance-negative-control-elixir-ids` (the file form, every id-table instance byte for byte against the oracle on wire, json-read and retain-save: first-use order, 1-based references, canonical LEB128, the node table under the reserved id; the message form is `tables-elixir-message-conformance`; the control writes the id table in reverse first-use order and reds wire) | ✅ `TestGooseTableWireMatchesTheCorpus` `TestGooseTableWireNegativeControl` — form byte, root body and the id table read from the end, canonical LEB128, first-use ids with an elided field's id rolled back (`internal/codegen/goosetable/runtime/wire_save.goose:818`); the conformance driver answers wire and report. The message form (form 2): `TestGooseMessageFormAnnounceRead` (AnnounceRead verdict for verdict against the Go reference over every connection golden cut and byte-flipped), `TestGooseMessageFormVersioning` (the typed read by peer entry, widened, kind_mismatch, unknown and clamped against Go over VOLD/VNEW both ways), `TestGooseMessageFormHostileBatches`; the driver answers message 12/12 (graph_tree's plain `table`, §3.3, absent as Go's is) | ✅ `TestMooTableLoadMatchesTheReports` `TestMooTableSaveRoundTripsEveryInstance` `TestMooMessageFormMatchesTheManifest` (the id-table wire, every report and instance row byte for byte against the Go oracle, and the message form's batches) with `TestMooTableLoadNegativeControl` and `TestMooTableSaveNegativeControl`, and `tables-moo-wire-fuzz` with `tables-moo-wire-fuzz-negative-control` (the file form and the retaining pair against `internal/tablewire` on the pipe) | ✅ `TestLobsterTableLoadMatchesTheReports` `TestLobsterTableLoadNegativeControl` `TestLobsterTableSaveRoundTripsEveryInstance` `TestLobsterTableSaveNegativeControl` (form byte, root body and the id table read from the end; every instance of the manifest byte for byte and report for report against the Go engine; the conformance `wire` and `report` surfaces) | ✅ `TestSquirrelTableLoadMatchesTheReports` `TestSquirrelMessageFormMatchesTheGoldens` `TestSquirrelMessageDepthBoundary` `TestSquirrelWireFuzzAgreesWithTheGoEngine` `conformance-negative-control-squirrel` (file and message forms) | ✅ `TestLuauTableLoadMatchesTheReports` `TestLuauTableLoadRoundTripsEveryCleanInstance` `TestLuauTableSaveRoundTripsEveryInstance` (the id-table wire, every instance byte for byte against the Go oracle), `TestLuauMessageFormMatchesTheGoldens`, and `tables-luau-wire-fuzz` with `tables-luau-wire-fuzz-negative-control` (158,445 mutants on seed 1, 0 divergences, 2026-10-31) and `tables-luau-wire-fuzz-vectors` with its control (each pinned Luau red, its fix reverted, goes red again). The message form's batch read hands back each body as its bit range in the batch, and the `message` surface's 13 cases pass on it | ✅ `TestLuaTableSaveRoundTripsEveryInstance` `TestLuaTableLoadMatchesTheReports` (the id-table wire, every instance byte for byte against the Go oracle) and `TestLuaMessageFormMatchesTheGoldens`, control `TestLuaIdTableNegativeControl` (the id table's first-use lookup in `save.lua` dropped, so a repeated id takes a second slot: `map_full` and `graph_shared` go red) |

C# carries the complete id-table file surface, the fixed-table message surface, native regions,
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
| — retired: the C++ leg refuses a NaN or an infinity on its raw bits and widens a finite float32 by the ordinary conversion (`tables-float-nan`, `tables-float-nan-negative-control`) Exempt by "the NaN bit surgery this paragraph used to require is retired with the NaN" (docs/SPEC-TABLES.md §3.3). | ✅ `tables-c-wire-fuzz` | — retired: the Rust leg refuses a NaN or an infinity on its raw bits and widens a finite float32 by the ordinary conversion (`tables-rust-finite-float`, `tables-rust-finite-float-negative-control`; `table_widen_f32` and `table_message_widen_f32` are deleted). Exempt by "the NaN bit surgery this paragraph used to require is retired with the NaN" (docs/SPEC-TABLES.md §3.3) | — retired: the Go leg refuses a NaN or an infinity on its raw bits and widens a finite float32 by the ordinary conversion (`TestWireSignalingNaNWideningAndLEBOverflow`, `TestGoPlainFloatSpecialValuesAgreeAcrossForms`, `tables-go-finite-float-negative-control`). Exempt by "the NaN bit surgery this paragraph used to require is retired with the NaN" (docs/SPEC-TABLES.md §3.3). | — retired: the C# leg refuses a NaN or an infinity on its raw bits and widens a finite float32 by the ordinary conversion (`internal/codegen/cstable/wire.go:1115`, `WidenFloat`; `tables-cs-finite-float-negative-control`). Exempt by "the NaN bit surgery this paragraph used to require is retired with the NaN" (docs/SPEC-TABLES.md §3.3). | ✅ the finite rule (docs/SPEC-TABLES.md §3.3) that retires this method: the Java leg refuses a NaN or an infinity on its raw bits and widens a finite float32 by the ordinary conversion, which is exact for every finite value (`tables-java-finite-float` `tables-java-finite-float-negative-control`; the kind-10 rung in `internal/codegen/javatable/variableform.go`) | ✅ `TestJsFloat32CellsAreBitTransparent` `tables-js-float-bits-negative-control` (the block accessor, the form-3 decode and write, and the f32-to-f64 rung; its node half runs under SCHEMA_SLOW=1) | ✅ `tables-dart-finite-float` `tables-dart-finite-float-negative-control` `TestDartFloat32CellsAreBitTransparent` (the method is retired for the finite rule, docs/SPEC-TABLES.md §3.3: the Dart leg refuses a NaN or an infinity on its raw bits in every read and writer, and widens a finite float32 by the ordinary conversion, which is exact for every finite value) | ✅ `tables-elixir-finite-float` `tables-elixir-finite-float-negative-control` (retired by the finite rule, SPEC.md §4.3 and docs/SPEC-TABLES.md §3.3: the Elixir leg refuses a NaN or an infinity on its raw bits and widens a finite float32 by the ordinary conversion, which is exact, so no value the wire carried is rewritten) | ✅ `tables-goose-wire-fuzz` `tables-goose-wire-fuzz-negative-control` `schema-goose-runs-hostile-corpus` `schema-goose-runs-hostile-corpus-negative-control` (the finite rule has landed: a NaN or an infinity is refused on its raw bits before they become a value, in kind 10 and kind 11, `internal/codegen/goosetable/runtime/wire_load.goose:560`, and the hostile corpus's control lets a NaN land and requires wire-non-finite-float red, and only it; a finite float32 widens into a float64 cell by `widen_f32`, exact bit for bit, never through a host conversion, and the wire-fuzz differential holds every re-save against the Go engine's) | ✅ `TestMooFloat32WidensBitForBit` `TestMooFloat32WidenNegativeControl` (moo has no float: a float32 is its binary32 pattern held as a number and a float64 a `Uint64` of its binary64 pattern, so where kind `10` widens into kind `11` (the variable, fixed and message reads) the runtime moves the bits itself, `internal/codegen/mootable/runtime/table_values.moo` `V.f32to64`, and no conversion exists to move one; the gate holds it to the exact widening for 40,000 finite patterns, every exponent of both signs with the mantissa corners, the subnormals at a stride and seeded random ones; the control drops the fraction's low three bits and normalizes a subnormal one exponent off. NaN and the infinities are refused on their raw bits before any rung, `floats_nan` in the report rows) | — retired: the lobster leg is finite only, a NaN or an infinity is refused on its raw bits in every form (`TestLobsterTablesAreFiniteOnly`, `TestLobsterTablesAreFiniteOnlyNegativeControl`). Exempt by "the NaN bit surgery this paragraph used to require is retired with the NaN" (docs/SPEC-TABLES.md §3.3). | — retired: the Squirrel leg is finite only, a NaN or an infinity is refused on its raw bits in every form (`TestSquirrelTableFloatsAreFinite`, `TestSquirrelTableFiniteFloatForms`, `TestSquirrelTableFiniteFloatNegativeControl`, `TestSquirrelTableFiniteFloatFormsNegativeControl`); a finite float32 still widens by bit surgery, `internal/codegen/squirreltable/runtime/load.nut` `widenF32`. Exempt by "the NaN bit surgery this paragraph used to require is retired with the NaN" (docs/SPEC-TABLES.md §3.3). | — retired: the Luau leg refuses a NaN or an infinity on its raw bits and widens a finite float32 by the ordinary conversion (`TestLuauTablesAreFiniteOnly`, `TestLuauPlainFloatSpecialValues`). Exempt by "the NaN bit surgery this paragraph used to require is retired with the NaN" (docs/SPEC-TABLES.md §3.3) | ✅ (the finite rule, docs/SPEC-TABLES.md §3.3, which retires the bit-surgery method) `tables-lua-finite-float` `tables-lua-finite-float-negative-control`: the Lua leg refuses a NaN or an infinity on its raw bits in every read (variable, retained, message, fixed, cook check and uncook, block dump) and every writer refuses one; a finite float32 widens by the ordinary conversion, and the conversion moves no bit of a finite pattern on the pinned interpreter (`TestLuaFloat32ConversionIsExactForEveryFinitePattern`) |

### M22 — Typed table serialization fast path

**Method.** Fixed tables emit typed save entry points and bodies (`<Name>Save`, `<Name>CollectTyped`, `<Name>BodySizeTyped`, `<Name>WriteBodyTyped`) where scalar leaves and child scalar arrays run with direct typed field reads, compile-time ordinal indexing, and pre-sized buffer operations without dynamic reflection or descriptor delegate dispatches, while everything else remains descriptor-driven. In C#, generated typed `<Name>Save` entry points read fields directly and use compile-time ordinal indexing (scalar leaves and child scalar arrays are typed; everything else is descriptor-driven), and therefore do not observe runtime mutations of TableFieldInfo / TableTypeInfo descriptors for those fields. Polymorphic `TableWire.Save(object)` remains the descriptor-driven entry point that observes runtime descriptor mutations across all fields.

**Reference.** `internal/codegen/cstable/wire.go`

**Proven in.** C# (#814, #816), Go (#795, #803), C (#775), C++ (#775).

**Measured effect.** Bypasses descriptor delegate dispatches and hash probes on repeat lookups (committed receipt: `bench/tables/results/2026-09-08-csharp-vocabulary-index-arm64`; official post-merge sitting will record the sealed baseline).

**Negative control.** `TestCsRefAt` and `TestCsTypedVsDynamicWireEquivalence`.

**Targets:** none

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `internal/codegen/cpptable/codecs.go` | ✅ `internal/codegen/ctable/wire.go` | ✅ `internal/codegen/rusttable/fixedform.go:405` `internal/codegen/rusttable/variablecodec.go:612` `tables-rust-fixedform` `tables-rust-variable-fixed-class` (every save is typed per table: the fixed form's `<t>_fixed_write_body` stores each field at its constant offset and the variable form's `<t>_save_body` reads each field directly; Rust emits no descriptor-driven save at all, so there is no dispatch on the save path and no runtime descriptor for it to observe) | ✅ `internal/codegen/gotable/wire.go` | ✅ `internal/codegen/cstable/wire.go` | ❌ #517 after 3.0.0: performance | ❌ #516 (every id-table `<T>Save` is the descriptor walk TableVariableSave; only the form-3 `<T>FixedWriteBody` is typed) | ✅ `internal/codegen/darttable/wireform.go:1777` `internal/codegen/darttable/fixeddart.go:283` `tables-dart-variable-class` `tables-dart-fixed-form` (every save is typed per table: the id-table form's save body writes each field through its own emitted statement with a direct field read, and the fixed form stores each field at its constant offset; no save goes through the descriptors) | ❌ #515 | ❌ #2236 — card schema-goose-fixed-form (docs/PORT-GOOSE.md, THE CARD MAP) | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2255 — every variable-wire save is the descriptor walk (`internal/codegen/lobstertable/runtime/schema_save.lobster`); only the form-3 `<Name>FixedSave` is typed (`internal/codegen/lobstertable/fixedform.go`) | ❌ #366 (every `<T>Save` is the descriptor walk in `internal/codegen/squirreltable/runtime/save.nut`; only the form-3 `<T>FixedWriteBody` is typed) | ❌ #381 — the fixed form is one descriptor-driven reader and writer over the layouts (`tables-luau-fixed-form`, `tables-luau-versioning`); no typed per-table save or body entry point | ✅ `TestLuaTypedSaveEqualsDescriptorSave` `TestLuaTypedSaveNegativeControl` `TestLuaTypedSaveRunsFewerInstructions` `internal/codegen/luatable/typedsave.go` (every unit's module carries `TypedBodies[<Name>]` per table and `SaveTyped<Name>(value)` per declared table, run by `save.SaveTyped`: the scalar leaves, bool, integer, float and string fields with no guard, presence or `required`, are written straight from the value with their id, kind, width and default as constants in the body and the field header as one call, and every other field of the table goes through the descriptor walk's `write_field` at its constant ordinal, so the body is the walk with its scalar part unrolled; `save.Save` stays the descriptor-driven entry; the equivalence holds the two to the same bytes and the same refusals over every manifest instance, every table at its defaults, every scalar leaf moved off its default in turn and a NaN float; three plants into the typed bodies, a wrong string kind, a wrong integer width and the default elision against a wrong value, each turn it red; and with no clock, the Lua VM's own instruction count over the same saves is 64% of the walk's) |

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
| ✅ `tables-flat-wire-negative-control` | ✅ `conformance-negative-control-c` | ✅ `tables-rust-names-negative-control` | ✅ `tables-go-fuzz-extent-negative-control` | ✅ `conformance-negative-control-cs` | ✅ `tables-java-fuzz-negative-control` | ✅ `tables-js-accessor-negative-control` | ✅ `tables-dart-fuzz-negative-control` | ✅ `tables-elixir-fuzz-negative-control` `tables-elixir-block-lead-negative-control` (`ELIXIR_SABOTAGED_BUILD`) | ✅ `tables-goose-strict-emitter-negative-control` (fixedform.go's fixed-root load names `schema_rt::FixedReportUnreachable`, a binding no module declares, through `go build -overlay`; the strict gate must red exactly the Table modules that name it, each on that line) `tables-goose-runtime-home-negative-control` `packet-utf8-goose-negative-control` `packet-wide-goose-negative-control` `packet-defaults-goose-negative-control` `packet-finite-goose-negative-control` (make/goose-packet.mk: each a named `tools/sabotage` edit of one Goose emitter, `go build -overlay` into a private compiler, the fixture regenerated and the gate required red on exactly its rows; the finite controls diff the failing vectors against `test/packet-finite/goose/read.expected` and `test/packet-finite/goose/write.expected`) | ✅ `conformance-negative-control-moo` `conformance-negative-control-moo-walk` (`test/conformance/moo/negative-control`: one runtime file the table emitter embeds is sedded into a `go build -overlay` compiler, checked to have matched, the manifest's units regenerated from it and the harness run over them: the load counting every unknown field twice turns exactly `report` and `retain` red with `wire`, `json-read` and `retain-save` green, and the text walk's key index missing the first field turns exactly `json-read` and `json-hostile` red) | ✅ `conformance-negative-control-lobster` (`test/conformance/lobster/negative-control`: a sed over a copy of the runtime file the table emitter embeds, `go build -overlay` into a private compiler, every manifest unit regenerated, exactly `report` and `retain` red) | ✅ `conformance-negative-control-squirrel` `conformance-negative-control-squirrel-walk` `conformance-negative-control-squirrel-block` `conformance-negative-control-squirrel-retain` | ✅ `conformance-negative-control-luau-emitter` `conformance-negative-control-luau-walk` (`test/conformance/luau/negative-control`: the embedded runtime sedded into a `go build -overlay` compiler, the units regenerated under a mirror of the repository because `main.luau` requires them by relative path) | ✅ `conformance-negative-control-lua` `conformance-negative-control-lua-walk` (`test/conformance/lua/negative-control`: the embedded runtime sedded into a `go build -overlay` compiler) |

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
| ✅ `tables-block-fuzz` `tables-block-fuzz-extent-negative-control` | ✅ `tables-c-fuzz` `tables-c-fuzz-negative-control` | ✅ `tables-rust-fuzz` (in `make test-rust`: 409,746 mutants, 89,545 opened, none read outside the extent) `tables-block-fuzz-extent-negative-control` (reds the Rust leg on the oracle) | ✅ `tables-go-fuzz` `tables-go-fuzz-extent-negative-control` | ✅ `tables-block-fuzz` `tables-block-fuzz-extent-negative-control` | ✅ `tables-java-fuzz` `tables-java-fuzz-negative-control` | ✅ `tables-js-fuzz` `tables-js-fuzz-negative-control` | ✅ `tables-dart-fuzz` `tables-dart-fuzz-negative-control` | ✅ `tables-elixir-fuzz` `tables-elixir-fuzz-negative-control` | ✅ `tables-goose-fuzz` `tables-goose-fuzz-negative-control` (`test/conformance/goose/accel.goose`, block-fuzz and cook-fuzz: the two block images and the six cooks of the corpus, 20,000 mutants each at the leg's seed; the oracle re-derives every bound from the records as data and reads every byte of every row, region and reached node, and every Goose slice index is checked, so a read past the extent aborts rather than reading a neighbour; the control removes both halves of each extent bound, and either half alone leaves both fuzzers green) | ❌ #381 — no forgery fuzz oracle on the moo leg yet: the block and cook forgeries of the manifest (`forgery`, `cook-forgery`, `block-reason`, `cook-reason`) all pass, but no mutated image is walked whole against an oracle that re-derives its bounds from `ir.Blocks` and the tool's cook-check. Owed: the Lua leg's `internal/codegen/luatable/forgeryfuzz_test.go` carried onto moo (a `buffer` read past the end raises, which is the bounds instrument), with its control removing the block's extent bound and the cook check's region scan, about 3 hours | ✅ `tables-lobster-fuzz` `tables-lobster-fuzz-negative-control` (`internal/codegen/lobstertable/forgeryfuzz_test.go`: every block and cook of the corpus mutated; a mutant refuses, or opens and is walked in full without one read leaving the bytes, a used length past its field's maximum refused by name; the controls remove the block's extent bounds, the used-length check and Open's reserved-word clause) | ❌ #413 (the block and cook forgery batteries ride the conformance surfaces; no forgery fuzzer walks what it opened) | ❌ #381 — card schema-luau-fuzz-differential (docs/PORT-LUAU.md, THE CARD MAP) | ✅ `tables-lua-fuzz` `tables-lua-fuzz-negative-control` (`internal/codegen/luatable/forgeryfuzz_test.go`: a mutated block must refuse or open whole against the compiler's layout, a mutated cook must agree with the tool's cook-check, and a full walk of what opened must read inside the bytes; a `string.unpack` past the end raises, which is the bounds instrument. 24,000 mutants over 2 blocks and 4 cooks at the pinned seed `0x4c5541464f524745`: 15,982 refused, 8,018 opened and walked whole, 0 findings; `LUA_FORGERY_SEED` and `LUA_FORGERY_N` are the knobs. The control removes BOTH halves of the block's extent bound (2,809 findings) and, separately, the cook Check's region scan (289). Its first run found `Check` opening a cook whose alignment word was forged to 64, which the tool refuses: fixed in `internal/codegen/luatable/runtime/cook.lua`) |

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
| ✅ the reference leg answers every case and may not answer absent at either grain (`test/conformance/README.md`): the harness refuses a reference absence, `TestReferenceCaseAbsent` `TestReferenceSurfaceMissingFromList` `TestReferenceSurfaceExitsTwo`, and the live C++ driver sabotaged into absence turns the harness red, `conformance-negative-control-absent` `conformance-negative-control-reference-surface` | ✅ `TestPortAbsenceStaysAllowed` `conformance-negative-control-c-absent` (`test/conformance/c/main.c`, `spill_absent_reason`: blob_shared is a named per-case absence today; a driver that drops it turns the harness red) | ✅ `conformance-rust-absent` `conformance-rust-absent-negative-control` (`spill_absent` in `test/conformance/rust/src/main.rs` writes `<case>.absent`; the leg is green with its absences counted, and a driver writing an answer where each `.absent` belongs is red) | ✅ `conformance-go-absent` `conformance-negative-control-go-absent` `test/conformance/go/main.go:163` (the Go leg alone answers the cases it cannot carry absent one by one, `+Na` on the wire surface; the control makes `spillAbsent` write nothing and the harness goes red on the missing answer) | ✅ `conformance-cs-absent` `conformance-cs-absent-negative-control` (`test/conformance/cs/src/Program.cs:54`, `SpillAbsent`, and the wire surface's blob_shared rows answer `<case>.absent`; the harness counts them while the surface passes, and the control removes those answers and the wire surface goes red) | ✅ `conformance-java-absent` `conformance-java-absent-negative-control`: the Java leg alone through the harness is green and answers absent per case (`spillAbsent` and `spillAbsentReason` in `test/conformance/java/src/Driver.java`); with both writing no marker the harness goes red on "the driver wrote nothing" | ✅ `conformance-negative-control-js-absent` (`test/conformance/js/main.mjs`: `messageAbsent`, `retainAbsent`, `wireAbsent`, `cookWriteAbsent` and the per-surface absent loop answer `<case>.absent` one case at a time; the control makes them write nothing and the leg's conformance run goes red on a case left with no answer) | ✅ `conformance-negative-control-dart-absent` (the gate is the conformance matrix's dart leg: `test/conformance/dart/main.dart` writes `<case>.absent` for a row it cannot carry, by name where the row owes a named refusal; the control renames every `.absent` write out of the harness's sight and requires the matrix red on exactly those rows, each as "the driver wrote nothing", with every answered surface green) | ✅ `conformance-negative-control-absent` (`test/conformance/elixir/driver_impl.ex:849`: every case the leg cannot answer is `<case>.absent`, and a refused construct says why on stderr) | ✅ `conformance-negative-control-absent` (the harness's per-case count; `test/conformance/goose/driver` writes a `<case> absent` line of `test/conformance/goose/main.goose` as `<case>.absent`, so json-hostile answers pass 76/76 +59a) | ✅ `test/conformance/moo/split.pl:24` (`<case> absent` from `test/conformance/moo/main.moo` becomes the empty `<case>.absent`; a unit the target refuses leaves `<unit>.refused` and every case naming it is absent) with the harness's own `conformance-negative-control-absent`, which holds the count the harness reads from that file | ✅ `conformance-negative-control-absent` (`test/conformance/lobster/driver`: a `<case> absent [reason]` line becomes `<case>.absent`, a refused unit answers every case absent, and `blob_shared`'s named writer refusal is `blob_shared.absent` holding its name) | ✅ `conformance-negative-control-squirrel-absent` (`test/conformance/squirrel/main.nut:72` `writeAbsent`: a refused unit's cases, and blob_shared's named writer refusal; the control writes those answers under another name and requires the wire surface red) | ✅ `TestLuauDriverWritesAbsentPerCase` `TestLuauDriverAbsentNegativeControl` (`test/conformance/luau/driver`: a `<case> absent <reason>` line becomes `<case>.absent` holding the reason, beside the answered cases' bytes, run in a scratch tree under a stand-in interpreter; the control removes the absent branch and requires no `.absent` written) | ✅ `conformance-negative-control-lua-absent` (`test/conformance/lua/main.lua`: every case it cannot answer is <case>.absent with its reason; the control runs a copy of the driver whose absent() writes nothing, and the harness goes red on the lua surfaces that carry an absence) |

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
| ✅ `block_base_unaligned` (`testdata/conformance/tables/MANIFEST.txt`) and `tables-block-fuzz` (the enumerated 1..63 pass, `test/tables/block_fuzz_main.cpp:939`) | ✅ `block_base_unaligned` and `tables-c-fuzz` (the enumerated 1..63 pass, `test/c-tables/fuzz_main.c:165`) | ✅ `block_base_unaligned` (`test/conformance/rust/src/main.rs`) | ✅ `block_base_unaligned` and `tables-go-fuzz` (the enumerated 1..63 pass, `test/go-tables/fuzz_test.go:205`) | ✅ `block_base_unaligned` (`test/conformance/cs/src/Program.cs`) and `tables-block-fuzz` (`test/cs-block/src/Fuzz.cs:749-753`) | ✅ `block_base_unaligned` (`test/conformance/java/src/Driver.java`, `openBlock` takes the lead) and `tables-java-block-lead` `tables-java-block-lead-negative-control` | ✅ `block_base_unaligned` (`test/conformance/js/main.mjs`, `openBlock` takes the lead) and `tables-js-block-lead` `tables-js-block-lead-negative-control` | ✅ `block_base_unaligned` (`test/conformance/dart/main.dart`) and `tables-dart-fuzz` (the enumerated 1..63 pass, `test/dart-tables/fuzz.dart`) `tables-dart-fuzz-lead-negative-control` | ✅ `tables-elixir-block-lead` `tables-elixir-block-lead-negative-control` | ✅ `block_base_unaligned` (`test/conformance/goose/accel.goose`, the open takes the lead) and `tables-goose-block-lead` `TestGooseBlockLeadGate` `TestGooseBlockNegativeControl` | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2259 — `block_read` checks the magic, the build version and the byte order only; no base-alignment check for the lead gate to hold | ✅ `block_base_unaligned` (`test/conformance/squirrel/main.nut`, `accelOpen` passes the lead) `TestSquirrelBlockReadMatchesTheCorpus` (an unaligned lead refused by name) | ✅ `block_base_unaligned` (the `forgery` and `block-reason` surfaces: `BlockOpen` takes the lead and refuses `unaligned_base` by name) and the harness's `cook_lead_1..63` rows (the `cook-forgery` surface, every row passing); `conformance-negative-control-luau` cuts the block's pitch clause and requires `forgery` and `block-reason` red | ✅ `block_base_unaligned` (`test/conformance/lua/main.lua:471`, BlockOpen takes the lead) and `TestLuaCookOpenIsTheHeader` (the cook at leads 1, 7 and one alignment) |

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
| ✅ `TestCppTableRuntimeNamesAreClaimed` (both ways over the Table and Block sources, 624 names registered for the Cpp backend) `TestCppRuntimeNameScanGoesRed` `tables-cpp-names-negative-control`; the packet emitter's fixed vocabulary is `TestCppPacketRuntimeNamesAreAccountedFor` (its unclaimed macros and helpers are a recorded ceiling in `compiler/tablescppnames_test.go`) | ✅ `TestCTableRuntimeNameScanGoesRed` | ✅ `tables-rust-names-negative-control` | ✅ `TestGoRuntimeNameScanGoesRed` | ✅ `TestCsRuntimeNameScanGoesRed` | ✅ `TestJavaRuntimeNameScanGoesRed` | ✅ `TestJsTableModuleScopeNamesAreClaimed` `TestJsModuleScopeScanSeesEveryConvention` `tables-js-names-negative-control` (the control plants one unclaimed module-scope binding in the emitted fixed runtime and the claim test goes red naming it) | ✅ `tables-dart-names-negative-control` | ✅ `TestElixirRuntimeNameCollisionRepro` `TestElixirRuntimeNamesAreClaimed` `TestElixirRuntimeNameScanGoesRed` (the scan reads every unit-level module segment the emitter spells, both ways against `internal/tablenames`; the control plants an unregistered module and one in a comment) | ✅ `tables-goose-names` `tables-goose-names-negative-control` (`TestGooseEveryPackageNameIsClaimed`: the Goose form of the claim. The runtime declares into schema_rt, which no package can take; every name the target writes into the package namespace is claimed through `goose.Gen.Declare`, which refuses a second declaration by name. The gate declares each emitted top-level name of every manifest unit in turn as a schema type beside the unit, after a fresh name proves the probe generates, and requires the unit refused: 13,366 names. The control lets every <T>CookOpen through Declare and requires the gate to name those and nothing else.) | ✅ `TestMooTableModuleKeysAreClaimed` `TestMooModuleKeyScanGoesRed` `TestTableRuntimeNamesAreClaimedMoo` (`compiler/tablesmoonames_test.go`: every key of every module table the moo table emitter returns, in the `function M.k(`, `function M:k(` and `M.k =` spellings and the keys of a returned literal, over every unit of the manifest, is in `internal/tablenames/moo.go`'s list and every listed key is spelled, both ways; the control plants an unlisted key in each convention the scan reads and a listed name nothing emits. A moo module key claims nothing: every runtime name is a key of the table its file returns, and a schema file's modules are `<Base>.moo` and `<Base>Table.moo`) | ✅ `tables-lobster-names` `tables-lobster-names-negative-control` (every name the runtime defines in `namespace schema_rt` is registered in `internal/tablenames/lobster.go` and every registered name is defined, and a schema file named for a runtime module is refused; the control plants an unregistered name in each spelling and a registered name nothing defines) | ✅ `TestSquirrelTableRuntimeNamesAreClaimed` `TestSquirrelRuntimeNameScanGoesRed` `TestSquirrelRefusesADeclarationOnAModuleKey` (`compiler/tablessquirrelnames_test.go`: every runtime file is its own module table, so a runtime export claims nothing; the unit's table module is one exports table holding the declarations and the unit's own names, and the emitter refuses a unit whose module would assign one key twice, naming it, `internal/codegen/squirreltable/squirreltable.go` `refuseKeyCollisions`, so a table named `Roots`, `Descriptors`, `Package` or `Keyed` is refused beside a table and kept in a table-free unit; the scan holds `internal/tablenames/squirrel.go` `SquirrelModuleKeys` to the emitted text of every manifest unit and every runtime file, both ways; the control plants unlisted keys in a module and a runtime file and drops a listed one, each reported) | ✅ `tables-luau-names` `tables-luau-names-negative-control` (`compiler/tablesluaunames_test.go`: every key of every module table the Luau emitter spells is in `internal/tablenames/luau.go`'s list and every listed key is spelled, both ways; the control plants an unregistered key in each convention the scan reads and a listed name nothing emits. A Luau module key claims nothing, and no Luau FILE name is claimable: the runtime is `schema_*.luau` and a schema file's modules are `<Base>Table`, `View` and `Block`) | ✅ `TestLuaTableRuntimeNamesAreClaimed` `TestLuaRuntimeFileNamesAreClaimed` `TestLuaRuntimeNameScanGoesRed` `tables-lua-names` `tables-lua-names-negative-control` (a Lua runtime spells everything it shares as a key of its returned module table, so the scan reads every such key out of the emitted text, in `function M.k(`, `function M:k(` and `M.k =` spellings, for every unit of the corpus, and holds it to `internal/tablenames/lua.go` both ways; the one claimable Lua spelling is a runtime module's FILE name, refused beside a table and kept in a table-free unit. The control plants an unregistered key in each convention and a registered name nothing emits) |

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
| ✅ `tables-big-endian-tables` `tables-big-endian-collections` | ✅ `tables-c-big-endian` `conformance-negative-control-c-foreign` | ✅ `tables-rust-order-word` `tables-rust-order-word-negative-control` (`TestRustOtherOrderWordIsRefused`: a cook and a block with an intact magic whose order word names the other order are each refused by their own Open; the control takes each order-word check out in turn) and `tables-rust-big-endian` (a `cargo check` for s390x evaluating every layout assert; skips cleanly without the target) | ✅ `conformance-big-endian` `conformance-big-endian-negative-control` (the Go driver under qemu-s390x: the cook-foreign and block-foreign refusals and every byte-order-neutral surface) | ✅ `tables-cook-open-cs` `tables-cook-open-cs-order-negative-control` (the refuse half: a cook of the other byte order is refused by its magic as `foreign_order`, and the control makes the magic not refuse; the native big-endian half is stated unproven until a big-endian .NET exists) | ✅ `tables-java-order` `tables-java-order-negative-control`: a cook of the other order refuses on its magic, and this reader's own cook with the magic intact and the other order's word refuses on the order word; with the order-word check planted out of the emitter the gate goes red on that cook alone | ✅ `tables-js-leg` `tables-js-order-word-negative-control` (a block whose magic is intact and whose order word says the other order is refused; the control stops the emitted Open reading the order word) | ✅ `tables-dart-order` `tables-dart-order-negative-control` (Dart's typed data is read at an explicit endianness everywhere (blockruntime.go:34), so the order word is the whole of the byte-order check) | ✅ `tables-elixir-variable-class` `conformance-negative-control-elixir-order` (the host's order is never consulted, M12; a byte-swapped magic is refused by name as `foreign_order` on the block-reason and cook-reason surfaces, and the cook-foreign and block-foreign surfaces refuse the other order's files; the control drops the block's foreign-magic arm and reds block-reason on block_magic_swapped) | ✅ `tables-goose-order-negative-control` (the forgery surface's block_byte_order and cook_byte_order_0/2/3 rows: an intact magic with the other order word is refused by name in each Open, `internal/codegen/goosetable/runtime/block_form.goose:252`, `cook_form.goose:52`; the control takes each clause out of a copy of the generated units and requires forgery or cook-forgery red with block-foreign and cook-foreign green) | ✅ `TestMooCookFixturesAndForgeries` `TestMooBlockForgeriesAnswerTheirReasons` `TestMooCookNegativeControls` (an intact magic with the other order word is refused `not_a_cook`: the three `cook_byte_order_*` forgeries and `block_byte_order`; the control removes the cook's order-word check and the forgery opens), beside the `cook-foreign` and `block-foreign` surfaces | ✅ `tables-lobster-block-lead` `tables-lobster-order-negative-control` (every block image at every lead 0..64, and an intact magic with the other order word refused as a named block and cook forgery; the control blinds the order word) | ✅ `TestSquirrelBlockReadMatchesTheCorpus` `TestSquirrelBlockOrderWordNegativeControl` `TestSquirrelCookReadMatchesTheCorpus` (the corpus's block_byte_order and cook_byte_order_* forgeries, an intact magic beside the other order word, refused by name beside the `cook-foreign` and `block-foreign` surfaces; with the block's order-word check dropped, block_byte_order opens and the gate turns) | ✅ `TestLuauCookOpenAgreesWithGo` `TestLuauCookOrderWordNegativeControl` (its order_word damage case: an intact magic with the other order word is refused `not_a_cook`, Go's own verdict; the control removes the order-word test from a scratch runtime and requires that case to open) and `TestLuauBlockReadAgreesWithGo` (`byteorder`, the same on a block), beside the `cook-foreign` and `block-foreign` surfaces | ✅ `TestLuaBlockRead` `TestLuaCookOpenAndCheck`, control `TestLuaForeignOrderNegativeControl` (an intact magic with the other order word is refused by name, beside the `cook-foreign` and `block-foreign` surfaces; the control drops BlockOpen's order-word check and the refusal probe goes red on it) |

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
| ✅ `tables-cpp-soak` `tables-cpp-soak-negative-control` | ✅ `tables-c-soak` `tables-c-soak-negative-control` | ✅ `tables-rust-soak` `tables-rust-soak-negative-control` | ✅ `TestSoak` `TestSoakIdentifierCanGoRed` | ✅ `tables-cs-soak` `tables-cs-soak-negative-control` | ✅ `tables-java-soak` (524288 passes from `test-java`, 2^27 — about an hour — the release act, a count and never a clock, over the blob, list and pointer corpora) `tables-java-soak-negative-control` | ✅ `tables-js-soak` (the allocation gate's paths for `TABLES_JS_SOAK_ITERATIONS` passes, the bytes per iteration gated at every one of sixteen samples, live-heap drift beside it; `tables-js-soak-short` from `test-js` at 2^22 passes, and 2^35, about forty minutes on mini-m5, the release act) `tables-js-soak-negative-control` | ✅ `tables-dart-soak` `tables-dart-soak-negative-control` | ✅ `tables-elixir-soak` `tables-elixir-soak-negative-control` (`test/elixir-soak/main.exs`: TABLES_ELIXIR_SOAK_ITERATIONS passes, a count and never a clock, 400 from `test-elixir` and 9000 — about an hour — the release act, cut into samples of 200; every sample re-measures every pinned case of the allocation audit in its own fresh process, heap words and binary calls, and holds each to its budget in `test/conformance/elixir/alloc-budget.txt`; the VM's binary, ETS, persistent-term and atom drift is kept beside it; the control plants a matched allocate-and-free in every generated load, and the count reds at every sample while drift stays silent) | ❌ #2236 — not yet carded in the schema-goose stream (docs/PORT-GOOSE.md, THE CARD MAP); the soak | ❌ #381 — the moo port's table layers are not built yet (docs/PORT-MOO.md, THE LAYERS) | ❌ #2263 — no soak yet (docs/PORT-LOBSTER.md, THE CARD MAP, schema-lobster-bench) | — no allocation count to gate a soak on (I1) | ❌ #381 — not yet carded in the schema-luau stream (docs/PORT-LUAU.md, THE CARD MAP); the soak | ✅ `tables-lua-soak` `tables-lua-soak-negative-control` (the load soak: one state and one value, `TABLES_LUA_SOAK_ITERATIONS` PASSES (a count, never a clock; 4000 by default, the release act the long one), every 500-pass sample re-measuring the allocation count of the rows that license nothing, which must be exact zero, and of the licensed row, which must stay inside its ceiling, with the live heap after a full collection read beside it as the weaker instrument; a table built and dropped per load turns the count and leaves the drift gate silent) |

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
| ✅ `conformance-negative-control-cpp` | ✅ `conformance-negative-control-c` | ✅ `conformance-negative-control-rust-walk` | ✅ `conformance-negative-control-go-walk` | ✅ `conformance-negative-control-cs` | ✅ `conformance-negative-control-java-walk` | ✅ `conformance-negative-control-js-walk` | ✅ `conformance-negative-control-dart-walk` `conformance-negative-control-dart-zero` `conformance-negative-control-dart-block` | ✅ `conformance-negative-control-elixir-walk` (the emitted text reader stores the negation of every bool, `internal/codegen/elixirtable/variableform.go` `json_read_field`; the matrix goes red on json-read alone, json-write and wire green) | ❌ #2236 — card schema-goose-conformance-leg; the json-read cell waits on the Goose table wire. The text walk's own control sabotages the GENERATED read accessor and requires exactly its rows red: `tables-goose-json-walk-negative-control` | ✅ `conformance-negative-control-moo-walk` (the text walk's field-key index never holds the first declared field, in the emitter through `go build -overlay`: moo `json-read` and `json-hostile` red, `json-write`, `wire` and `cook-write` green) | ✅ `conformance-negative-control-lobster-walk` (the text walk's field-key match sabotaged in the emitter: exactly `json-read` and `json-hostile` red, `json-write`, `wire` and `cook-write` green) | ✅ `conformance-negative-control-squirrel-walk` `conformance-negative-control-squirrel` | ✅ `conformance-negative-control-luau-walk` (the `FromJson` field-key match never matches the first declared field: luau json-read and json-hostile red, `json-write`, `wire` and `cook-write` green) | ✅ `conformance-negative-control-lua-walk` (the `FromJson` field-key match; lua json-read and json-hostile red, `json-write` and `wire` green) |

### I12 — The documented surface compiles and runs

**Method.** `docs/USAGE.md`'s example for the language is compiled and run
by a gate, so the documented surface goes red with the code rather than
drifting from it.

**Reference.** `tables-cpp-usage`: the page's three marked C++ cook examples
are extracted verbatim and compiled as function bodies in
`test/tables/usage_cpp_main.cpp` against `tables/pointers`, then run over the
cooked Scene; `tables-dart-usage` runs the page's Dart verbatim over the
graph_tree corpus wire, with infos fatal under the analyzer.

**Proven in.** C++.

**Measured effect.** The Elixir reader ran the page's example by hand and
found a module name the packet emitter refuses — the drift a gate catches.

**Negative control.** `tables-cpp-usage-negative-control`: the emitter moves
one documented name (a fixed root's `<Root>CookMeasure` becomes
`<Root>CookSize`, its one internal caller with it) through `go build -overlay`,
and the page's example must fail to compile naming `SettingsCookMeasure`.

**Targets:** usage

| cpp | c | rust | go | cs | java | js | dart | elixir | goose | moo | lobster | squirrel | luau | lua |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ✅ `tables-cpp-usage` `tables-cpp-usage-negative-control` (docs/USAGE.md's three marked C++ cook examples, verbatim, compiled against `tables/pointers` and run over the cooked Scene; `tables-cook-open`'s `usage` mode carries the same text) | ✅ `tables-c-usage` `tables-c-usage-negative-control` | ✅ `tables-rust-usage` `tables-rust-usage-negative-control` (`TestUsageRust`: docs/USAGE.md's `rust-table-usage` example compiled against `tables/examples` and run; the control's two edited copies of the page, a renamed call and a wrong declared default, each fail it) | ✅ `tables-go-usage` `tables-go-usage-negative-control` | ✅ `tables-cs-usage` `tables-cs-usage-negative-control` (docs/USAGE.md's C# cook example, pulled from the page between the cs-table-usage markers, compiled against tables/pointers and run over the Scene cook; the control moves `SceneCook.Open` and the page stops compiling) | ✅ `tables-java-usage` `tables-java-usage-negative-control` (docs/USAGE.md's `java-table-usage` example compiled under `-Xlint:all -Werror` against `tables/pointers` and run over the Scene cook; with the documented `root()` renamed in the emitter the example stops compiling and the gate goes red) | ✅ `tables-js-usage` `tables-js-usage-negative-control` | ✅ `tables-dart-usage` `tables-dart-usage-negative-control` | ✅ `tables-elixir-usage` `tables-elixir-usage-negative-control` | ✅ `tables-goose-usage` `tables-goose-usage-negative-control` (docs/USAGE.md's Goose fragment, between the goose-table-usage markers, compiled and run by the pinned compiler beside the tables/pointers unit over a Scene cook `schema cook-check` accepts; it must walk at least one node, and the control's two edited copies of the page, an accessor renamed and a walk from a null field, each turn the gate red) | ✅ `tables-moo-usage` `tables-moo-usage-negative-control` (docs/USAGE.md's moo fragment, run against `tables/examples` under the pinned moo: a keyed accessor, a save, the strict read and the walk, the damage written as its binary32 pattern; the control renames the save and walks nothing) | ✅ `tables-lobster-usage` `tables-lobster-usage-negative-control` (docs/USAGE.md's Lobster fragment run verbatim over the tables/pointers unit and a Scene cook) | ✅ `tables-squirrel-usage` `tables-squirrel-usage-negative-control` (docs/USAGE.md's Squirrel fragment, a two-node Scene saved and walked back through the strict read, run under the pinned interpreter against the generated `tables/pointers` unit; the control renames the read the page calls and walks a member that reaches one node, each turning the gate) | ✅ `tables-luau-usage` `tables-luau-usage-negative-control` (docs/USAGE.md's Luau fragment, run against `tables/examples` under the pinned interpreter: a keyed accessor, a save and the strict read; the control renames the save and walks nothing) | ✅ `tables-lua-usage` `tables-lua-usage-negative-control` |

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
| ✅ `tables-cpp-text-differential` `tables-cpp-text-differential-negative-control` (`TestCppTextDifferentialAgainstTheGoEngine`, `internal/codegen/cpptable/textdifferential_test.go`; the engine is the third implementation) | ✅ `tables-c-text-differential` `tables-c-text-differential-negative-control` (`TestCTextDifferentialAgainstTheGoEngine`, `internal/codegen/ctable/textdifferential_test.go`; the engine is the third implementation, 200 random finite float32/float64 instances, both directions) | ✅ `tables-rust-json-differential`, control `tables-rust-json-differential-negative-control` (`TestTextDifferentialRust`, `TestTextDifferentialRustNegativeControl`; variable root and a fixed-class unit's root, 200 instances at each of two seeds; the form-3 fixed record has no Rust text surface and is not claimed; first run: no divergence) | ✅ `tables-go-text-differential`, `tables-go-text-differential-negative-control` (`TestTextDifferential`, `TestTextDifferentialNegativeControl`, `internal/codegen/gotable/textdifferential_test.go`; 200 random float-only instances on a fixed root, the emitted `RootToJSON` against `internal/tabletext`, which shares no code with it, then the leg's text packed back; the control caps float32 at six digits) | ✅ `tables-cs-json-differential` `tables-cs-json-differential-negative-control` `TestCsTextDifferential` (`internal/codegen/cstable/textdifferential_test.go`; its first run found float64 written at fifteen digits, fixed in `internal/codegen/cstable/json.go`) | ✅ `tables-java-json-differential` (`TestTextDifferentialJava`, `TestTextDifferentialJavaNegativeControl`) | ✅ `tables-js-text-differential` | ✅ `tables-dart-text-differential`, control `tables-dart-text-differential-negative-control` (variable and fixed roots; first run: 6 of 200 on the Go seed, ties `toStringAsFixed` broke away from even) | ✅ `tables-elixir-json-differential`, `tables-elixir-json-differential-negative-control` (`TestTextDifferentialElixir`) | ✅ `tables-goose-json-differential` `tables-goose-json-differential-negative-control` (`TestGooseTextDifferential`, `internal/codegen/goosetable/textdifferential_test.go`: 200 random instances, the leg's text against the engine's built from the same float bits, then the engine reading the leg's text back to the same wire; the leg's wire stands in as bits until the Goose table wire lands) | ✅ `tables-moo-json-differential` (`TestMooJsonTextDifferential`, `internal/codegen/mootable/json_differential_test.go`: 576 draws, the corpus redrawn plus the float-only unit, entering moo as a literal until the moo table wire lands; both directions), control `TestMooJsonTextDifferentialNegativeControl` (the magnitude tie-break: 4 of 576) | ✅ `tables-lobster-json-differential` `tables-lobster-json-differential-negative-control` (`TestLobsterTextDifferential`: random values written and read by the Lobster text form and held to the Go engine; the control caps a float32 at six digits) | ✅ `TestSquirrelTextDifferentialAgainstTheGoEngine` `TestSquirrelTextDifferentialNegativeControl` (200 random float instances drawn in Squirrel from the Lua and Elixir legs' seed, the emitted save and ToJson against the compiler's engine, internal/tabletext and internal/tablepack, both directions; the control writes a float32 at six significant digits and the text comparison turns) | ✅ `tables-luau-text-differential` `tables-luau-text-differential-negative-control` (`TestLuauTextDifferentialAgainstTheGoEngine`, `internal/codegen/luautable/textdifferential_test.go`: the Go cell's splitmix64 at its seed and seed 1, a variable and a fixed root, the emitted save and ToJson against `internal/tabletext` both directions; the control writes a float32 at six digits) | ✅ `tables-lua-json-differential` `tables-lua-json-differential-negative-control` `TestLuaTextDifferential` (`internal/codegen/luatable/textdifferential_test.go`: 200 random float instances drawn in Lua from the Elixir leg's splitmix64 seed, the engine's text byte-compared with the leg's and the engine's pack of the leg's text byte-compared with the leg's wire; 5,000 more at a second seed were clean on the first run; the control caps a float32 at six digits and goes red on 199 of 200) |

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
| ✅ `tables-wire-fuzz` `tables-wire-fuzz-negative-control` | ✅ `tables-c-wire-fuzz` `tables-c-wire-fuzz-negative-control` | ✅ `tables-rust-wire-fuzz-all` (every root, plain and `--retain`, 0 divergences) `tables-rust-wire-fuzz-negative-control` (the node index's malformed verdict removed: red on the report) `tables-rust-retain-negative-control` | ✅ `tables-go-wire-fuzz` `tables-go-wire-fuzz-negative-control` `tables-go-builders` `tables-go-retain-wire-fuzz` | ✅ `tables-cs-wire-fuzz` `tables-cs-region-fuzz` `tables-cs-builder-fuzz` `tables-cs-retain-fuzz` `conformance-negative-control-cs` | ✅ `tables-java-wire-fuzz` runs every root over every enumerated pass and every pinned vector in BOTH arms, plain and `--retain` (0 divergences; 89,897 retaining mutants), and replays the Java-only vectors (`keyed_header_count_past_length`; the retaining `retain_declared_default`, `retain_node_record_unknown`, `retain_node_flags`, `retain_count_over_length_refused`, `retain_scalar_array_default`, `retain_map_emptied_unplaced`); `tables-java-wire-fuzz-negative-control`, `tables-java-map-count-negative-control`, `tables-java-keyed-count-negative-control`, `tables-java-retain-fuzz-negative-control` and `tables-java-retain-node-negative-control` are each red on their own planted fault. The seeded random pass's first finding in each arm is damaged input (stream_arm_first #22918, parts_count_past_bound #4485), parked under the discard-on-damage ruling | ✅ `tables-js-wire-fuzz-all` `tables-js-wire-fuzz-plain` `tables-js-wire-fuzz-retain` `tables-js-wire-fuzz-negative-control` `cards-js-wire-fuzz` `cards-js-retain-fuzz` | ✅ `tables-dart-wire-fuzz` `tables-dart-wire-fuzz-negative-control` (`test/conformance/dart/main.dart`, the `wire-fuzz` mode: 54 file roots bound by unit and root, the message arm through the message codecs, a refused Save answered as the `-1`; 159,457 enumerated and 100,000 random mutants, 0 divergences; the `--retain` arm is bound but not gated, red at its first `graph_tree` mutant) | ❌ #492 — `tables-elixir-wire-fuzz-all` drives the harness over the default roster (plain arm) and is red: a fixed-class root it cannot open is refused where the engine reads its defaults (tabledemo.WideBlob, mutant 0), and the reader drifts from `internal/tablewire` on several framing rules past it (#2413); 31 roots that reach a map, a union or an enum-keyed array are pinned out of the roster (`test/conformance/elixir/wire-fuzz-absent.txt`); there is no wire-fuzz negative control; `cards-elixir-wire-fuzz` and `cards-elixir-retain-fuzz` hold the card root, the only root the `--retain` arm runs on | ✅ `tables-goose-wire-fuzz` `tables-goose-wire-fuzz-leb-negative-control` `tables-goose-wire-fuzz-utf8-negative-control` (one leg per unit, `test/conformance/goose/wirefuzz.goose`: the `--builder` arm over the variable roots, the region arm over the value-only roots; every arm green at seed 1, N 2000, none held in `test/conformance/goose/wirefuzz-known.tsv`) | ✅ `tables-moo-wire-fuzz` `tables-moo-wire-fuzz-negative-control` (the file form and the retaining arm on the pipe under the native host `tools/moofuzzhost`, built from the moo pin's source; the two message-form entries over variable roots are pinned absent by name in `test/conformance/moo/wire-fuzz-absent.txt`, and the mutants the leg diverged on are replayed by `tables-moo-wire-fuzz-pinned`. The control plants a string frame past its body that is not counted malformed, and the harness stops on that verdict. Both arms run on seeds 1-3 at the release count in the release wire-fuzz gate, make/wire-fuzz-release.mk, which `tables-moo-release` runs) | ✅ `tables-lobster-wire-fuzz` `tables-lobster-wire-fuzz-negative-control` (the harness's wire fuzz over every variable root, the plain and --retain arms and the message roots, through the Go host shim, `test/conformance/lobster/wirefuzzhost/main.go`) | ✅ `TestSquirrelWireFuzzAgreesWithTheGoEngine` `TestSquirrelWireFuzzNegativeControl` (the harness's mutants against the Go engine: the plain arm over fixed roots, the builder arm over variable ones) | ✅ `tables-luau-wire-fuzz` `tables-luau-wire-fuzz-negative-control` `TestLuauWireFuzzAgreesWithTheGoEngine` `TestLuauWireFuzzNegativeControl` (the file form on the pipe, under the native host `tools/luaufuzzhost`; the nine message-form roots are pinned absent by name in `test/conformance/luau/wire-fuzz-absent.txt`, and the `--retain` arm is driven but not yet gated) | ✅ `tables-lua-wire-fuzz-differential` (`TestLuaWireFuzzAgreesWithTheGoEngine` against `internal/tablewire` on a pipe, `TestLuaWireFuzzNegativeControl`) |

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
| ✅ `tables-accessor-descriptor-agreement` `tables-accessor-negative-control` `tables-slot-negative-control` | ✅ `tables-c-accessor-descriptor-agreement` `tables-c-accessor-negative-control` `tables-c-slot-negative-control` | ✅ `tables-rust-accessor-descriptor-agreement` `tables-rust-accessor-negative-control` `tables-rust-slot-negative-control` (`test/rust-agree`: every block row, cook node and reachable record type read through the `#[repr(C)]` row struct and `<record>_at`, and through `TableBlockInfo`/`TableCookInfo` offsets; the controls swap `RenderCamera.camera_id`/`camera_type` and move `TreeNode.left`'s described slot, via `tools/sabotage`) | ✅ `tables-go-accessor-descriptor-agreement` `tables-go-accessor-negative-control` `tables-go-slot-negative-control` | ✅ `tables-cs-accessor-descriptor-agreement` `tables-cs-accessor-negative-control` `tables-cs-slot-negative-control` | ✅ `tables-java-accessor-agreement` `tables-java-accessor-negative-control` `tables-java-slot-negative-control` | ✅ `tables-js-accessor-negative-control` `tables-js-slot-negative-control` | ✅ `tables-dart-agree` `tables-dart-accessor-negative-control` `tables-dart-slot-negative-control` | ✅ `tables-elixir-accessor-descriptor-agreement` `tables-elixir-accessor-negative-control` `tables-elixir-slot-negative-control` (plain scalar block-record fields and pointer cook slots (make/elixir.mk:279)) | ✅ `tables-goose-accessor-descriptor-agreement` `tables-goose-accessor-negative-control` (`TestGooseAccessorsAgreeWithDescriptors`: every field of every cooked record, block row and block projection of the pointers and block units read through the generated typed accessor and through CookRecords or BlockRecords, over a buffer whose every byte differs from its neighbours, at a row offset of 64; the control moves every pointer accessor eight bytes in the emitter and requires the gate red on ListNode.next) | ✅ `TestMooLayoutAgreesWithTheCompiler` `TestMooLayoutAgreementNegativeControl` (moo generates no accessor: the cook is read by one reflective walk, `table_cookread.moo`, at the layouts each `<Base>Table.moo` registers, so what is held is the layout the loaded runtime reads at against the compiler's model, `ir.RecordLayout`, for every record of every cook-carrying unit, size, alignment, field count and each field's offset, `internal/codegen/mootable/layoutagree_test.go`; the control moves `TreeNode.left`'s registered offset eight and exactly that field turns. The block's layouts are emitted from `ir.BlockLayout` itself, one derivation, and the block surfaces hold the values read at them) | ✅ `TestLobsterDescriptors` `TestLobsterDescriptorsNegativeControl` (`accessor_descriptor`: a fresh row read through the generated accessor agrees with the descriptor's defaults) | ❌ #421 (the block descriptors carry the only accessors; no agreement check or slot control yet) | ✅ `tables-luau-accessor-descriptor-agreement` `tables-luau-accessor-negative-control` (Luau generates no accessor: the cook reader derives every record's size and alignment and every field's offset and size from the descriptors by its own arithmetic, `schema_cookread.luau` `C.layout`, and `internal/codegen/luautable/layoutagree_test.go` holds it to `ir.RecordLayout` for every record of every cook-carrying unit; the control widens the pointer slot by eight and only records that hold one turn. The block's descriptors are emitted from `ir.BlockLayout` itself, one derivation) | ✅ `tables-lua-accessor-descriptor-agreement` `tables-lua-accessor-negative-control` `tables-lua-slot-negative-control` (Lua generates no accessor: the block and cook are read through the descriptors alone, `internal/codegen/luatable/runtime/block.lua`, so what is held is the layout's two independent derivations, the compiler's model against the Lua runtime's own arithmetic, for every block projection, block row and cook record of the corpus's 33 units, 104 and 203 of them, in `internal/codegen/luatable/layoutagree_test.go`; the controls move `RenderCamera.camera_id` four bytes in `block.lua` and `TreeNode.left`'s descriptor offset eight, and each reds on that field alone. Its first run found `B.Layout` sizing a 128-bit integer, a 128-bit fixed and a `bits(N)` slot wrong, fixed in `block.lua`) |

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
| ✅ `tables-cpp-runtime-home` `tables-cpp-runtime-home-negative-control` `tables-cpp-runtime-home-bytes-negative-control` (the C++ runtime has no one file: every `<Base>Table.h` carries it behind package-named include guards, so the home is the guard set, and within a unit every header carries it byte-identical, `test/cpp-tables/runtime_home_digest.sh`) | ✅ `tables-c-runtime-home` `tables-c-runtime-home-negative-control` (every `<Base>Table.h` carries the runtime behind package-named guards) | ✅ `tables-rust-runtime-home` `tables-rust-runtime-home-negative-control` (a crate is the package: the homes are module constants off the crate, and a file that sorts first moves nothing) | ✅ `tables-go-runtime-home` `tables-go-runtime-home-negative-control` | ✅ `tables-runtime-home` `tables-runtime-home-negative-control` | ✅ `tables-java-runtime-home` `tables-java-runtime-home-negative-control` | ✅ `tables-js-runtime-home` `tables-js-runtime-home-negative-control` | ✅ `tables-dart-runtime-home` `tables-dart-runtime-home-negative-control` | ✅ `tables-elixir-runtime-home` `tables-elixir-runtime-home-negative-control` (every runtime module's file is named by the module: TableRuntime.ex, BlockRuntime.ex, CookRuntime.ex and FixedRuntime.ex stay byte-identical, and Table.ex, BuildVersion.ex and UnitView.ex keep their names, when an earlier-sorting file joins the unit; the control names the table runtime by `ir.ProtocolIdHome` and the gate goes red on TableRuntime.ex) | ✅ `tables-goose-runtime-home` `tables-goose-runtime-home-negative-control` (the runtime modules are emitted once under their own names beside the modules and imported relative to the importing file; the gate holds them and every original Table module byte-identical when an earlier-sorting file joins) | ✅ `tables-moo-runtime-home` `tables-moo-runtime-home-negative-control` (`make/moo-open-gates.mk`: the runtime is the fixed set of modules the emitters embed and each schema file's modules are `<Base>.moo` and `<Base>Table.moo`, so when an earlier-sorting file joins `tables/examples` no module moves, none appears but the joining file's own, and all 19 runtime modules stay byte-identical; the control puts the file-order rule back through an overlay and the home moves) | ✅ `tables-lobster-runtime-home` `tables-lobster-runtime-home-negative-control` (the runtime is fixed-name modules named by no schema file, so an earlier-sorting schema file relocates nothing; the control plants a file-order rule in the emitter) | ✅ `tables-squirrel-runtime-home` `tables-squirrel-runtime-home-negative-control` (the unit's table module is `<Package>Tables.nut`, `internal/codegen/squirreltable/squirreltable.go` `ModuleName`, beside the `schema_table_*.nut` runtime files: a schema file that sorts first leaves the module's name and every runtime file byte-identical; the control puts the file-order rule back and the module moves) | ✅ `tables-luau-runtime-home` `tables-luau-runtime-home-negative-control` (`make/luau-open-gates.mk`: the runtime is the fixed `schema_*.luau` set and each schema file's module is `<Base>Table.luau`, so when an earlier-sorting file joins the unit no module moves, no module appears but the joining file's own, and every runtime module but `schema_view.luau` (the unit's declarations) is byte-identical; the control puts the file-order rule back through an overlay and the home moves) | ✅ `tables-lua-runtime-home` `tables-lua-runtime-home-negative-control` (`make/lua-open-gates.mk`: the table module stays `TabledemoTables.lua` and every embedded runtime file stays byte-identical when an earlier-sorting schema file joins the unit; the control puts the file-order rule back through `go build -overlay` and the module moves from `GuardedTables.lua` to `AaaTables.lua`) |

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
| ✅ `tables-cpp-release` (the long wire fuzz, two seeds, and the INT32_MAX base64 reproduction; and `tables-cpp-release-battery`: the tables, maps, lists, arms and retain suites, the soak and the block fuzz built `-O2 -DNDEBUG`, with `tables-cpp-release-flags-negative-control`) | ✅ `tables-c-release` `tables-c-soak-negative-control` `tables-c-fuzz-negative-control` (the hour soak and the 500,000-mutant forged-block fuzz at two seeds, each with its planted control in the same target) | ✅ `tables-rust-release` `tables-rust-release-negative-control` (the all-roots wire fuzzer and the forgery fuzzer at N=1,000,000 on a second seed, the wire fuzzer's control, the soak at 16,777,216 passes and its control; the target refuses to run unless it scales the PR tier, and the control takes each scaling away in turn) | ✅ `tables-go-release` `tables-go-release-negative-control` (the release fuzz half, `tables-go-release-fuzz`, red over a driver whose readers accept a noncanonical LEB128) | ✅ `tables-cs-release` `conformance-negative-control-cs` (the four wire-fuzz bindings at 500,000 under two seeds and the leg's planted controls; `conformance-negative-control-cs` drives the same wire-fuzz binding against a codec with duplicate identities accepted and requires it red) | ✅ `tables-java-release` `tables-java-release-negative-control` (the whole-tree compile under `-Xlint:all -Werror`, the usage example and the planted controls `make test` cannot afford; the control plants a redundant cast in every unit's emitted `TableBytes` and the release compile gate goes red on the warning) | ✅ `tables-js-release` `tables-js-release-negative-control` | ✅ `tables-dart-release` `tables-dart-wire-fuzz-negative-control` `tables-dart-fuzz-negative-control` `tables-dart-alloc-negative-control` (the release target scales the PR-tier instruments, the wire fuzzer at N=500,000 on two seeds, the forgery fuzzer on two seeds, the allocation gate, and runs each instrument's own control inside itself, so a blind instrument turns the release gate red; `certify.yml` derives it from the Makefile) | ✅ `tables-elixir-release` `tables-elixir-fuzz-negative-control` `tables-elixir-block-lead-negative-control` `tables-elixir-runtime-pin-negative-control` (the expensive half: the runtime pin, the long fuzz over both readers and the block lead gate, each with its control, and the usage gate) | ✅ `tables-goose-release` (`test-goose` and every Goose control on the certification schedule) | ✅ `tables-moo-release` (the release wire-fuzz gate, both arms on seeds 1-3 at the release count, with `tables-moo-wire-fuzz-negative-control`, and the documented example with `tables-moo-usage-negative-control`; `certify.yml` installs the moo pin for it) | ✅ `tables-lobster-release` `tables-lobster-wire-fuzz-negative-control` (the release tier scales the wire fuzz the PR tier runs, certify.yml derives it; the control is that fuzz's own) | ✅ `tables-squirrel-release` `tables-squirrel-release-negative-control` (the wire pin, the wire fuzzer over both arms with the builder arm scaled to seeds 2 and 3 at ten times the PR count, and the four conformance controls; the control overlays a load that reports a stopped builder finished and requires the release's builder arm red) | ✅ `tables-luau-release` `tables-luau-release-negative-control` (the wire fuzz at ten times the PR count on two seeds with its control, and the documented example with its witnesses; `certify.yml` installs the pin for it; the control runs the release gate over a scratch copy of the units with a reader bug planted and requires it red on the report) | ✅ `tables-lua-release` `tables-lua-release-negative-control` (the release's wire fuzz at its release count on seed 2 over units whose load counts every unknown field twice goes red at the first divergence) |

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
| ✅ `tables-cpp-clean` `tables-cpp-warnings-negative-control` (`clang-format` in the repo's `.clang-format`, and `clang++ -Weverything` less the c++98-compat family only, over every generated C++ file: the table corpus, the wide corpus and the committed `generated/` tables and packets; required by `make test` and by the cpp leg of `ci-fast`; the packet emitter's files are format-clean and carry only the three kinds that cannot be cleared without altering the packet wire (`padded`, and the contradictory `switch-default` / `covered-switch-default` pair), the table emitter's ride the shrink-only ratchets `make/cpp-format-allow.txt` and `make/cpp-warn-allow.txt`) | ✅ `tables-c-clean` `tables-c-format-negative-control` (`clang-format`, engine `make/checks/c-clean-format.sh`) | ✅ `tables-rust-clippy` `tables-rust-clippy-negative-control` (a clippy-only lint planted in a copy of a generated unit turns the gate red) | ✅ `tables-go-clean` (`gofmt`, `go vet`) `tables-go-clean-negative-control` (the emitter skipping go/format reds the gofmt half; a printf call one argument short reds the vet half) | ✅ `tables-cs-clean` (`dotnet format` and the SDK analyzers over the table units, required by `test-cs`; 152,044 WHITESPACE findings in 295 units fixed in the emitters, #547; `tables-cs-clean-negative-control`; the packet units are held by `packet-clean-cs`) | ✅ `tables-java-clean` (google-java-format 1.37.0 `--aosp`, every table and packet file, at least 585, required by `test-java`) with `tables-java-clean-drift-negative-control` and `tables-java-clean-formatter-missing-negative-control` (docs/JAVA-DIALECT.md); `tables-java-compile-all` (`--release 17 -Xlint:all -Werror`, all 25 units, from `tables-java-release`) | ✅ `tables-js-clean` (`eslint`, over the table and packet units) | ✅ `tables-dart-clean` `tables-dart-clean-negative-control` (a planted off-format unused declaration: the formatter rewrites it, the analyzer names `unused_element`) | ✅ `tables-elixir-clean` `tables-elixir-emit-format-clean` `tables-elixir-emit-format-negative-control` (`mix format --check-formatted` over the generated table units and over every emitter's raw output; the control drops a call's parentheses in the cook emitter and the raw-format gate goes red on CookRuntime.ex) | ✅ `tables-goose-strict` (every generated module and the runtime modules beside it, each checked on its own by the pinned compiler, zero warnings, warnings as errors; v2026.2 ships no formatter, so the compiler's warnings are its analyzer) `tables-goose-strict-negative-control` | ✅ `schema-moo-generated-analyze-strict-gate` `schema-moo-generated-analyze-strict-gate-negative-control` (`moo-analyze --mode=strict` silent, no error and no lint warning, over the packet AND table modules of every corpus unit and both vendored runtimes, the analyzer held to the moo pin, `TestMooGeneratedAnalyzeStrictGate`; the controls plant a type error and an unused local, and `schema-moo-generated-analyze-strict-gate-pin-negative-control` refuses another analyzer). The format half: moo has no formatter (the pin builds moo, moo-analyze, moo-ast, moo-compile, moo-bytecode and moo-reduce, none of which formats, as Luau upstream ships none), so the emitter's layout is the canonical form | ✅ `tables-lobster-clean` (`make/schema-lobster-generated-strict-gate.mk`: every unit of examples/, examples128/, examples-wide/ and the conformance manifest compiles under the pinned v2026.8 with zero warnings; `schema-lobster-generated-strict-gate-negative-control` plants a `var` that must be a `let` and requires red on that line alone; Lobster ships no formatter, so the format half has no tool to run) | ✅ `tables-squirrel-clean` `schema-squirrel-generated-lint-gate-negative-control` (`schema-squirrel-generated-lint-gate`, `make/schema-squirrel-generated-lint-gate.mk`: every generated table unit of the table corpora and the conformance manifest, and the packet trees, compiles under the pinned Squirrel 3.2 with `sq -c` and no output, and holds the canonical text the emitters write; the control plants a refused statement, a trailing space and a blank-line run and requires each named by file and rule; upstream ships no Squirrel formatter or analyzer, so the format half is those rules) | ✅ `tables-luau-clean` `schema-luau-generated-analyze-strict-gate` (`luau-analyze --mode=strict` over every manifest unit, the examples and bench corpora, the packet and table modules and the runtime; warnings are errors and a `--!nolint`, `--!nonstrict` or `--!nocheck` is refused; `schema-luau-generated-analyze-strict-gate-negative-control` plants a type error, a lint warning and a `--!nolint`); no formatter: StyLua 2.5.2 and 2.6.0 cannot parse Luau 0.741 integer literals (`0i`) | ✅ `tables-lua-luacheck` `tables-lua-luacheck-negative-control` (luacheck over the generated tree; no Lua formatter is pinned, so the format half is open; the gate is the schema-port-lua-gates sub-worker's) |

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
| ✅ `cards-cpp-conformance` (`tables/cards/cpp/driver.cpp`) | ✅ `cards-c-conformance` (`tables/cards/c/driver.c`) | ✅ `cards-rust-conformance` (`tables/cards/rust/src/bin/cards-driver.rs`) | ✅ `cards-conformance-go` `cards-conformance-go-negative-control` (`tables/cards/cmd/cards-driver/main.go`; the control widens one separator in the Go card renderer and the check goes red on render, green on load) | ✅ `cards-cs-conformance` (`tables/cards/cs/Driver/Program.cs`) | ✅ `cards-java-conformance` (`tables/cards/java/src/cards/Driver.java`) | ✅ `cards-js-conformance` (`tables/cards/js/driver.mjs`) | ✅ `cards-dart-conformance` (`tables/cards/dart/bin/driver.dart`) | ✅ `cards-elixir-conformance` (`tables/cards/elixir/driver.exs`) | ❌ #2236 — not yet carded in the schema-goose stream (docs/PORT-GOOSE.md, THE CARD MAP); no `tables/cards/goose` leg exists, so the goose leg is not in `make cards-conformance-all` | ❌ #381 — `tables/cards` has no moo driver: the card surfaces (load, save, render, parse, bounds, upgrade, edit, taskhash, view) need a moo card library beside the generated units and a driver on a pipe (the pinned moo CLI has no io library, so through a host like `tools/moofuzzhost`), about 6 hours | ❌ #2272 — `tables/cards` has no lobster driver, so the leg is not in `cards-conformance-all` | ❌ #366 (no cards driver is in the squirrel leg, so `cards-conformance-all` runs none) | ❌ #381 — not yet carded in the schema-luau stream (docs/PORT-LUAU.md, THE CARD MAP); `tables/cards` has no Luau driver | ✅ `cards-lua-conformance` (`tables/cards/lua/driver.lua`) |


