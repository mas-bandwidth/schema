# Porting schema to Squirrel

This page is the Squirrel port's design: what every schema construct is in
Squirrel's value domain, the integer and float rules the codec relies on, the
byte buffers it reads and writes, where every file of the leg lives and how a
module reaches its runtime, how the leg's tests run the interpreter, the
port's column of the techniques register, and which card of the
`schema-squirrel` stream fills each surface. The skeleton it describes is in
the tree: `compiler/target_squirrel.go` (a built-in target), `internal/codegen/squirrel` (the type surface), `internal/codegen/squirreltable`
(the table surface) and `make/squirrel.mk`. Neither surface carries a codec
yet; the cards below add them without editing the skeleton's files.

The request this answers (2026-10-17): a full port of schema to Luau, to
vanilla Lua, and to Squirrel at the same time. It overrides the 2026-10-15
Go-first hold for these three languages only. The Luau and Lua designs are
their own pages; nothing here constrains them.

## THE PINNED INTERPRETER

Squirrel 3.2, from the upstream tag
`https://github.com/albertodemichelis/squirrel/archive/refs/tags/v3.2.tar.gz`,
sha256 `02805414cfadd5bbb921891d3599b83375a40650abd6404a8ab407dc5e86a996`.
`make dist-squirrel` fetches it into `dist/` (gitignored), refuses any other
sha256 by name, and builds `make -C dist/squirrel-3.2 sq64 DEFS=-DSQUSEDOUBLE`.
The pin is `SQ`, `$(CURDIR)/dist/squirrel-3.2/bin/sq` by default, exported to
every process make starts, and probed by `make toolchain-squirrel`.

Both build switches are load-bearing, and both were measured on the pin:

- **`sq64`** makes `SQInteger` 64 bits (`_intsize_` is 8). Without it the
  integer is 32 bits and no `int64`, `uint64` or flags mask fits.
- **`-DSQUSEDOUBLE`** makes `SQFloat` a double (`_floatsize_` is 8). Without it
  the float is 32 bits: `16777217.0 == 16777216.0` is true, and no `float64`
  field survives a load.

A port that embeds Squirrel in a host (a game engine) must build the VM the
same way, or the generated code is not this design's code. The runtime's
first file can check it at load (`_intsize_ == 8 && _floatsize_ == 8`) and
refuse by name; that check is owed to the first codec card (THE CARD MAP).

## THE TYPE MAPPING

One rule decides every row: **a record is a class instance**, not a table. A
Squirrel class instance has a fixed set of members (a misspelled member is a
runtime error, `the new slot operator` is refused), it is cheaper to construct
than a table with the same slots, and it gives the type a name `instanceof`
can test. Its members are the field's `ir.GoExportName` — the Go, C# and
JavaScript spelling, so the checker's collision registry already covers this
target, and since every Squirrel keyword is lower case no PascalCase member
can meet one. Every member is declared `null` in the class body and assigned
by the constructor, because a class-body initializer is evaluated ONCE and
SHARED by every instance in Squirrel: `Entries = array(2)` in the body would
hand every instance the same array.

Data that is not a record is a table: an enum is a table of integers, a
module's exports are a table, a descriptor is a table.

### The type surface (`type`, SPEC.md §4.3)

| schema (SPEC.md §4.3, §4.7, §4.12) | Squirrel storage, born at (SPEC §5, §6.1) |
|---|---|
| `bool` | `bool`, `false` or the default |
| `intN`, `uintN`, N ≤ 64, with or without a range | the 64-bit integer; a `uint64` above 2^63 − 1 is the same 64 bits, negative when read signed |
| `bits(N)`, N ≤ 64 | the 64-bit integer |
| `int128`, `uint128` | the pair `[lo, hi]`, each half a 64-bit integer holding that half's bits, the low half first (the wire's own order) |
| `fixed(I, F)`, `ufixed(I, F)` | the RAW value, as the integer of its storage width (I+F ≤ 64), or the pair (I+F = 128) |
| `float32` | the 64-bit float, holding a value a float32 can represent (a default is emitted as the double that IS that float32, never the shortest decimal that rounds to it) |
| `float64` | the 64-bit float |
| compressed `float32` (min, max, resolution) | the 64-bit float; the step count is the codec's |
| an enum | the integer: 0 is None, variants dense from 1 |
| a flags mask | the 64-bit integer, bit 63 its sign bit (flags store uint64 in every target) |
| a `type` | its class instance, pre-allocated |
| a `union` | its class instance: `Type` (0 = None, then each arm in declared order) beside one pre-allocated member per arm with a payload; a payload-free arm has no member |
| `string(N)`, `bytes(N)` | a `blob` of N bytes beside a `<Member>Length` integer (the used length), the default's bytes written in |
| `wstring(N)` | an array of N integers (UTF-16 code units) beside `<Member>Length` |
| `[N]uint8` | a `blob` of N bytes |
| `[N]T`, other T | an array of N elements, each its element's zero or a pre-allocated instance |
| `[..N]T`, `[A..B]T` | the same N-element array beside `<Member>Count`, born at A (SPEC §4.6) |
| `const(…)`, `reserved(…)`, `align` | no storage: the codec's |

A string is a byte buffer rather than a Squirrel string because the type
surface is the storage principle (SPEC §6.1): every buffer exists at
construction, a read fills it in place, and a Squirrel string is immutable and
interned — every new one is an allocation and a hash.

### The table surface (`table` and `fixed table`, docs/SPEC-TABLES.md §2)

A table and a fixed table have the SAME storage: the class is a fact of the
wire form (§2.2), not of the value. Each is a class instance whose members are
the same `ir.GoExportName` spelling, and its descriptor names the schema name,
the member and the text key side by side.

| schema (docs/SPEC-TABLES.md §2) | table storage, born at its declared default |
|---|---|
| a scalar (bool, integers, bits, floats, timestamp §2.11) | as the type surface |
| an enum | the variant's NAME as a string, `null` for None: a variant's identity on the table wire is its name's hash (§5), and a reader meeting an unknown id has no number to give it |
| a flags mask | the 64-bit integer |
| `string(N)`, a text-list element, bare `string` (§2.13) | a Squirrel string: a table value is a value, not a reused buffer |
| `bytes(N)` | a `blob` holding the default's bytes |
| a nested table or `type` | its class instance |
| a `union` (§2.6) | a union class of the table module: `Type`, the arm tag in the type surface's numbering, beside a member per arm with a payload, each arm a field line |
| `*T` (§2.1), `*bytes`, `*string` (§2.5) | `null`; a load places the node |
| `?T` (§2.3) | the value beside `<Member>Present`, born `false` |
| `[E]T` (§2.4) | an array of E.Max elements, key k at index k − 1 |
| `[N]T`, `[A..B]T`, `[..N]T`, `[]T` (§2.9) | an array born at its born length (N, A, 0, 0), each element its zero |
| `map[K]V` (§2.8) | not mapped: refused by name (THE REGISTER) |
| `wstring(N)` in a table (table kind 33) | an array of integers, the UTF-16 CODE UNITS, whose length is the used length; born empty (SPEC §4.12 takes no default). The wire carries the units two bytes each little-endian (§3), the message form sixteen bits each (§3.3), the fixed form an int32 length and N two-byte units (§3.4), and the text forms one JSON string transcoded at the boundary (§16.2) |

Every field's descriptor is a table with every key present, `null` where the
shape has no such fact, so a runtime walk reads a slot and never asks whether
it exists: `name member key id ordinal kind shape bound min list counted signed width bits lo hi f32 def table variants elem optional required deprecated` (`internal/codegen/squirreltable/squirreltable.go`, `fieldDesc`).
A table's descriptor adds `name typeId table fixed variable make fields`, and
the module's `link` indexes each by wire id, schema name and text key.

## 64-BIT AND 128-BIT INTEGERS

Squirrel has one integer type, `SQInteger`, which `sq64` makes the C
`long long`, and every rule below is a consequence of that.

- **int64 is the integer.** Every signed width up to 64 is its value.
- **uint64 is the same bits.** A `uint64` value above 2^63 − 1 is stored as
  the negative integer with the same 64 bits. Equality, `&`, `|`, `^`, `<<`
  and storing are the same on both readings. Three operations are not, and
  the codec spells each: an unsigned COMPARISON is `(a ^ MIN) < (b ^ MIN)`
  with `MIN = 0x8000000000000000`; an unsigned right shift is `>>>` (Squirrel
  has the operator); and an unsigned DIVISION or a range offset `value − A`
  wider than 63 bits is done in two halves. Text (the JSON form) prints and
  parses a uint64 digit by digit, because `tostring()` and `tointeger()` read
  signed.
- **Overflow is the C compiler's, so the codec never overflows.** The VM does
  `SQInteger` arithmetic in C, where signed overflow is undefined. The codec
  composes values from bytes with shifts and masks only, keeps every shift
  count in [0, 63] (a shift by 64 is undefined too), and computes a range's
  span in the compiler where it can, emitting it as a constant.
- **Literals.** A value in int64 is emitted in decimal, except −2^63, whose
  magnitude the lexer cannot read: it and every uint64 above 2^63 − 1 are
  emitted as the sixteen-digit hex of their bits (`squirrel.IntLit`). A wire
  id is a 64-bit hash and is always emitted in hex (`squirrel.HexLit`).
- **128 bits is two halves.** An `int128`, a `uint128` and a 128-bit fixed
  point value are the array `[lo, hi]`, each half the bits of that half
  (`squirrel.Int128Lit`). The type wire writes the low half first, so a
  128-bit field is two 64-bit writes; a range offset is the codec's two-lane
  subtraction with an explicit borrow. The table wire's 128-bit kinds are
  refused today (`ir.RefuseWideTableKinds`), as every port without them does.
- **Floats.** `SQUSEDOUBLE` makes the float a double, so a `float64` is
  exact. A `float32` is held in that double. Packet and table floats are
  FINITE only (SPEC §4.3, docs/SPEC-TABLES.md §3.3): every finite `float32`
  widens to the double exactly and narrows back exactly, `-0` and the
  subnormals included, so the ordinary conversion is enough and no NaN bit
  surgery is left. A writer refuses a NaN or an infinity and a reader refuses
  one on its RAW BITS, before any value exists, as `non_finite_float`: the
  packet wire through the vendored serialize.nut (v2.x), the table forms in
  schema's own runtime. `castf2i(f)` and `casti2f(i)` (the blob library's
  globals) reinterpret the 64 bits of a double as an integer and back with no
  conversion, which is how both test the bits.
- **A float32 write rounds exactly as the Go reference writer does**
  (coordinator ruling, 2026-10-17, shared with the Lua design). A double that
  is not exactly a float32 is NOT refused: it is narrowed by IEEE-754
  round-to-nearest, ties-to-even — Go's `float32(x)` — so the bytes are the
  reference's, byte for byte, and the output is canonical and deterministic.
  Strict means that, not refusing ordinary rounding. The narrowing is done in
  integer arithmetic on the bits from `castf2i`: the 29 dropped mantissa bits
  decide the round (above half rounds up, below down, exactly half to the even
  mantissa), a carry out of the mantissa bumps the exponent, a result below
  the normal range is rounded into the subnormals the same way, and one at or
  past `MaxFloat32 + ½ ulp` becomes the signed infinity — the cases where a C
  `(float)` cast is undefined or depends on the host's rounding mode, so the
  blob's `'f'` view is never the writer. The type-wire-write card holds the
  narrowing to Go's `float32()` over the edges (ties both ways, the subnormal
  boundary, overflow, negative zero) and over a seeded sweep; the
  table-wire-save card shares the same function.

## BYTE BUFFERS

The standard `blob` class is the codec's buffer: a growable byte array that is
also a stream, with `readn(type)` and `writen(value, type)` for `'b' 'c' 's' 'w' 'i' 'l' 'f' 'd'`, `readblob(n)`, `writeblob(b)`, `seek`, `tell`, `len`,
and indexing (`b[i]` reads and writes one byte, 0..255).

- **`readn` and `writen` are the HOST's byte order.** They `memcpy` the C
  value. The table wire is little-endian and the type wire is a bit stream,
  so a codec that calls `readn('i')` reads the wire correctly only on a
  little-endian host. The codec therefore composes every multi-byte value
  from bytes (`b[i] | b[i + 1] << 8 | …`), or reads it with `readn` behind a
  one-time check of the host's order taken from a known blob; which of the
  two is faster on the pin is the first codec card's measurement, and the
  other is its control (docs/PORTING.md M12, I7).
- **`'f'` is a conversion**, as the float rule above says; the codec reads a
  float32 as its 32 bits (`'i'`, masked) and widens it by bit surgery.
- **A blob does not read past its end**: `readn` past the end throws. A load
  that meets a short buffer catches nothing: it checks the length before the
  read and refuses by name, because a thrown error is a runtime error, and a
  runtime error is what the harness counts as a failed probe.
- **Strings are not buffers.** A Squirrel string holds bytes (indexing gives
  a signed char), but it is immutable and interned, so the type surface keeps
  `string(N)` in a blob, and the table surface converts at the codec edge.

## THE RUNTIME LAYOUT

Fixed now for the pro cards; nothing below is renamed.

| path | what it is | who adds to it |
|---|---|---|
| `compiler/target_squirrel.go` | the built-in target; its refusals | the card that lifts a refusal |
| `internal/codegen/squirrel/squirrel.go` | the TYPE emitter: one `<Base>.nut` per schema file, the constants, enums (`Max`, no `Count`), flags, unions and constructors, each type's and union's `<Name>MaxBits`/`<Name>MaxBytes`; `RegisterSurface` | no later card edits it |
| `internal/codegen/squirrel/read.go`, `write.go` | the type wire's read and write, joining every module through `RegisterSurface` from their `init` | type-wire-read, type-wire-write |
| `internal/codegen/squirrel/runtime/*.nut` | the type runtime, `go:embed` by glob, emitted beside the modules as `schema_<name>.nut`: `module.nut` (the loader) now, `bitreader.nut` and `bitwriter.nut` later | one file per card |
| `internal/codegen/squirrel/serialize/*.nut`, `SERIALIZE_PIN` | the vendored serialize port (schema-squirrel-serialize-vendor), the type wire's BOTTOM LAYER: its src files copied verbatim from the repository `SERIALIZE_PIN` names, placed beside the modules under their OWN names (`serialize.nut`, `serialize_bits.nut`, ...) by the caller, never by the generator: a generated module requires no serialize file, it calls the methods of the stream it is handed, as generated JavaScript never imports serialize.js; `serialize.nut` runs as a module with (exports, require) in vargv, which the port's design allows | serialize-vendor |
| `internal/codegen/squirrel/sqrun/sqrun.go` | the test helper (THE HARNESS) | |
| `internal/codegen/squirreltable/squirreltable.go` | the TABLE emitter: one `<Package>Tables.nut` per unit with a DESCRIPTOR per table (data the runtime walks, as `internal/codegen/jstable/variableruntime.go` walks its codec descriptors), the constructors, the unit registry; `RegisterSurface` | no later card edits it |
| `internal/codegen/squirreltable/fixedform.go`, `messageform.go` | the forms that need emitted code, joining through `RegisterSurface` | fixed-form, message-form |
| `internal/codegen/squirreltable/json.go` | the text form's descriptor facts the wire walks do not need, joining through `RegisterSurface`: a fixed field's F (`frac`), a flags mask's names (`flagNames`), a compressed float's range (`fmin`, `fmax`), `json = "array"` (`jsonArray`), a guarded field's composed `if` condition (`guard`, which the write elides by, §16.2) | json-text-form |
| `internal/codegen/squirreltable/runtime/*.nut` | the table runtime, by glob, emitted as `schema_table_<name>.nut`: `descriptor.nut` now; `load.nut`, `save.nut`, `json.nut`, `text.nut`, `fixed.nut`, `message.nut`, `refusal.nut` later | one file per card |
| `test/conformance/squirrel/driver` | the conformance driver: hands every surface to `test/conformance/squirrel/main.nut`, and answers ABSENT (an empty list, exit 2) while that file is absent | |
| `test/conformance/squirrel/main.nut` | the driver's one script | conformance-leg |
| `test/conformance/squirrel/ci.json` | the CI row: its targets and the `squirrel` toolchain field | |
| `make/squirrel.mk` | `dist-squirrel`, `toolchain-squirrel`, `build/schema-squirrel` (the tagged compiler), `build/conformance-squirrel` (every manifest unit generated for squirrel, refusals in `REFUSED.txt`), `test-squirrel` | |
| `bench/tables/squirrel/leg` | the tables-bench leg, ABSENT | |

**The type wire's bottom layer.** The type-wire-read and type-wire-write cards call the
vendored serialize library above rather than a private bit reader or writer: a
`serializeBits` / `serializeInt` / `serializeFloat` call emits the reference serializer's
bytes, byte for byte, so the wire needs no second bitpacker. The serialize files live
beside the modules under their own names, where schema's loader resolves them, and
`serialize.nut` itself runs as a module with (exports, require) in vargv, which the port's
design allows. `SERIALIZE_PIN` and `make vendor-serialize-squirrel` (make/squirrel-serialize.mk) pin
the vendored commit and re-copy the files on a bump.

**The pinned runtime is serialize.nut v2.1.1** (commit d5fc46fe2, format 2.0, standard 2.1),
as the main ports pin their serialize release by tag. What it asks of the generated code:
a write or a measure TRAPS on misuse (a value outside its range or bit width, a non-finite
float, a bad declaration, an operation past the buffer) by throwing
`serialize: write misuse: ...` and latches nothing; only a reader latches, with the
nine format 2.0 reasons (`past_end`, `value_out_of_range`, `nonzero_padding`, ...). A
`serializeInt` whose bounds leave int32 traps on write, so a range of 32 bits that is
not inside int32 (`[0, 2^32-1]`) rides `serializeInt64`, the read's own selection, whose
wire for 32 bits or fewer is the same single group. `serializeEnd` is optional and, as in
the nine main ports, not emitted.

**How a module reaches its runtime.** Squirrel has no `require`; it has
`dofile` and `loadfile`. The loader, `schema_module.nut`, is the one file a
caller runs:

    local schema = dofile(dir + "/schema_module.nut", true);
    local Types = schema.require("Types");

It finds its own directory from `getstackinfos(1).src`, so every name
resolves beside the generated files wherever the working directory is. A
generated module — a schema file's `<Base>.nut`, the unit's
`<Package>Tables.nut`, a runtime file — is a script the loader runs with two
arguments, its exports table and `require`:

    local _M = vargv[0];
    local _require = vargv[1];
    local _rt_bitreader = _require("schema_bitreader");
    local _Wire_ = _require("Wire");

The loader caches the table BEFORE running the script, so two schema files
that name each other each load once and a cycle meets the table it is still
filling. A declaration is a slot of `_M` (`_M.ShipCreate <- class { … }`), a
reference to another file's declaration goes through that file's local
(`_Wire_.ProbeConfig()`; the trailing underscore keeps every file's local apart from the locals the generator declares, none of which ends in one, so a file named `M` or `h` cannot shadow `_M` or the holder), and a constructor reads the slot when it runs, so
emission order matters only for top-level initializers, which the emitter
folds to constants. A schema file whose base starts with `schema_` is refused
(it would take a runtime file's name), as is one named `<Package>Tables`.

**The codec's shape.** `Write<Name>(stream, value)` and `Read<Name>(stream,
value)` are the family's target-first pair (SPEC §6.1) and return `false` on a
refusal. Every serialize call goes through one module-scope holder,
`local _h = { value = 0 };`, which a call consumes in the statement that fills
it, so neither path allocates per field (generated JavaScript's
`NUMBER_SCRATCH`). A module declares a helper only where its body names it:
`_h`; `_usedBytes(b, n)`, the used prefix of a string or bytes buffer as the
blob a write hands over whole (the buffer itself when it is full, else one
native `readblob`); and `_validText(b, n)`, the one UTF-8 and interior-NUL check
every string read shares. A read copies a string's bytes into its buffer with
one `writeblob`. Union cases are integer literals commented with the arm's
name: a case that named `_M.<Union>Type.<Arm>` would cost a table get each.
A ranged integer takes the same serialize call on both sides: `serializeInt`
inside int32, `serializeInt64` inside int64, else raw bits over `value - min`
checked in the unsigned domain, where a bound at the domain's edge is not
emitted.

## LOADING IS CLOSED

Coordinator ruling, 2026-10-17 (shared with the Lua design): script loading
resolves only the unit's directory and the runtime beside it. Nothing on any
search path may be found, and no native module may load.

Squirrel's mechanism makes this a property of one function. The language has
no `import`, no `require`, no module search path and no native-module loader:
a script reaches another file only through the io library's `dofile` and
`loadfile`, which take a path, and the VM loads native code only when the
HOST registers it in C (`sq_newclosure` on a C function), never from a
script. The compile hooks a host has (`sq_compile` with its own reader,
`sq_setcompilererrorhandler`) are the embedding program's, not the script's,
and nothing generated calls them. So the one resolver is the loader,
`schema_module.nut`:

- it resolves a name to `<dir>/<name>.nut`, where `dir` is the loader's own
  directory (from `getstackinfos(1).src`), never the working directory and
  never a list of places;
- it refuses, by name, any name that is not a basename of letters, digits and
  underscores, so no `..`, no `/`, no absolute path reaches `loadfile`;
- every generated module reaches other files only through the `require` it is
  handed; it never calls `dofile`, `loadfile` or `system` itself.

A host that embeds the VM keeps the same closure by registering only the
`blob`, `math` and `string` libraries plus `dofile`/`loadfile`, and NOT the
`system` library (`system()` runs a shell command). The test
`TestSquirrelLoaderResolvesOnlyItsDirectory` plants shadowing scripts — a
`Wire.nut`, an `ArmDefaults.nut` and a `schema_module.nut` that each print
`SHADOW` — in the working directory the probe runs from, loads the unit
through the loader in another directory, and requires that no shadow ran,
that the real modules answered, and that a traversal name (`../cwd/Wire`) is
refused by name.

## THE HARNESS

`sq` is a command-line interpreter with the `io`, `blob`, `math`, `string` and
`system` libraries: `file(path, "rb")` and its `readblob`, `stdin`, `stdout`,
`print`. So a driver is one `sq` process per surface: it reads the manifest's
files itself, answers the surface, and writes its results — no host program
and no second language. `sqrun.Run(t, dir, main, inputs)` is the unit tests'
form of the same thing: it writes `inputs` as the module `inputs.nut` in
`dir` (a script returning name → blob, which also defines `::hexline(b)`, the
probe's lowercase-hex line), runs `dir/main` under `$SQ` or
`<repo root>/dist/squirrel-3.2/bin/sq` with `dir` as its working directory,
and returns stdout. It fails the test naming `make dist-squirrel` when the
interpreter does not resolve, never `t.Skip`.

**`sq` exits 0 on a runtime error.** Measured on the pin: an uncaught `throw`
prints `AN ERROR HAS OCCURRED [boom]` and the call stack to stderr and exits
0; only a compile error exits nonzero. So `sqrun` fails a run that exits
nonzero OR writes anything to stderr, and the conformance driver must do the
same: a driver that trusted the exit status would report a crashed surface as
answered. A probe reports on stdout and nowhere else.

The unit tests stay unit tests: no real time, no sockets, no network; a
generated unit run under the pinned interpreter in a `t.TempDir()` is the
house pattern. `make dist-squirrel` is setup, run once, never from a test.

## THE REGISTER

The Squirrel column of [PORTING.md](PORTING.md) is written cell by cell and
held to the tree by `go test ./compiler -run Porting`: a `✅` cell names a test
or a target the tree runs, a `❌` cell cites the carry-across issue its row
already uses for the other ports (#366 where the row has none that fits), and
a `—` cell states why the platform cannot carry the method. In summary:

- **Carried:** M4b, M5, M6, M7, M8, M9, M12, M14, M15, M16, M18, M19, M20,
  I2, I4, I5, I10, I11, I15, J3; M2 and M3 by the platform (the 64-bit integer
  and the double are unboxed values in the object slot).
- **Cannot:** M1 (a load answers class instances and VM strings), M4 (no
  inlining directive and no aliasing qualifier), M10 (no allocator a script
  can hook), M11 (the descriptor offsets ARE the layout), M13 (no immutable
  table; the descriptors are built once at module load), M17 (no arena: every
  blob is its own), I1, I9 and I14 (no allocation count a script can read),
  I7 (the host's order is never consulted), I8 and J5 (no table-wire bench).
- **Not yet:** M21 (retired by the finite rule, tables-finite/squirrel), M22
  (a typed save), I3 (a forgery fuzzer), I6 (the claimed-name scan), I12
  (USAGE), I13 (a third-implementation text differential), J1, J2, J4, J6.
  Each has a `port/squirrel/...` entry in docs/roadmap.sexp.

## THE CARD MAP

What is on the branch, by component. Every codec lifts the target's refusals
for exactly what it carries; what no component carries stays refused by name
(no corpus unit is refused today).

| component | carries | files |
|---|---|---|
| type wire | the packet wire's read and write, byte for byte against the C++ goldens (SPEC §5) | `internal/codegen/squirrel/read.go`, `write.go`, `runtime/` |
| table wire | the variable and fixed classes' load, report and save (docs/SPEC-TABLES.md §3, §4), the strict read and its refusals by name (§6.5, §11) | `internal/codegen/squirreltable/runtime/load.nut`, `save.nut`, `refusal.nut` |
| retain-unknown | LoadRetain / MeasureRetain / SaveRetain with both capacities and the §6.6 exclusions | `retainform.go`, `runtime/retain.nut` |
| text forms | JSON both ways (§16), base64 text, the hand-editable document (§16.9) | `json.go`, `textform.go`, `runtime/json.nut`, `text.nut`, `document.nut` |
| fixed form | form 3 against the byte oracle (§3.4) | `fixedform.go`, `fixedlineage.go`, `runtime/fixed.nut` |
| message form | form 2: the announcement and the batch verbs, the unknown skip bounded at depth 128 (§3.3) | `messageform.go`, `runtime/message.nut` |
| accelerators | the block read and build (§19), the cook read and write (§7) | `block.go`, `blockbuild.go`, `cook.go`, `cookwrite.go`, `runtime/block.nut`, `blockbuild.nut`, `cook.nut`, `cookwrite.nut` |
| reflection | the unit view (§8.3) | `view.go` |
| conformance leg | every surface the harness asks, four negative controls, the release tier | `test/conformance/squirrel/main.nut`, `driver`, `negative-control`, `make/squirrel-conformance.mk` |
| fuzz differential | the tolerant wire's fuzzer against the Go engine, plain and builder arms | `test/conformance/squirrel/wirefuzz.nut`, `make/squirrel-fuzz.mk` |
| packet wide text | the shared wstring corpus and its bit flips against C++, the composition contracts, the pairing control | `test/packet-wide/squirrel/main.nut`, `make/squirrel-packet-wide.mk` |

## DEEPEN

- **The pro move.** Treat the descriptor as the codec. One runtime that walks
  data is how jstable's variable wire was made correct across every shape at
  once, and in an interpreter it is also the fast path: a generated function
  per table costs a compile per module and buys nothing an interpreter can
  inline. Generated code is kept for what data cannot say (the fixed form's
  record layout, the message form's batch).
- **Raw material.** The Go engine (`internal/tablewire`, `internal/tabletext`)
  is the oracle; the C++ goldens and the conformance manifest are the corpus;
  jstable's variable runtime is the nearest worked example of a descriptor
  walk; an unmerged foundation from an earlier attempt,
  `rowan/port-squirrel-foundation`
  (`test/conformance/squirrel/lib/wire.nut`, the §3 primitives with a red-first
  test), is prior art for the load and save cards to read, not to merge.
- **Methods and tools.** The pinned `sq64` + `SQUSEDOUBLE` interpreter;
  `castf2i`/`casti2f` for bit surgery; the probe pattern of `sqrun`; the
  sabotage controls each surface's test already demonstrates (a default
  forced to zero turns `TestSquirrelTypesConstructAtTheirDefaults` and
  `TestSquirrelTableDescriptorsLoad` red).
- **The outside judge.** The conformance harness, which does not know which
  language answered; the Go engine's differential fuzzer; and the shipping
  target, Squirrel as embedded by game engines, whose VM must be built with
  the two switches above or this design does not hold.
