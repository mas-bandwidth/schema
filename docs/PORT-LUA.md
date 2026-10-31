# PORT-LUA — the Lua port's design

The Lua leg is vanilla **PUC-Rio Lua 5.4**, pinned at 5.4.8 and built from
source (`make dist-lua`). This page is the design every card of the
`schema-lua` stream follows; where it is silent, the JavaScript leg
(`internal/codegen/js`, `internal/codegen/jstable`) is the worked example.
It is written in the house order: the value model first, because every codec
stands on it, then the files, then how the leg is held.

WHAT EXISTS ON MAIN (2026-10-28, through schema#2237): the target
(`compiler/target_lua.go`, a builtin target of the compiler), the TYPE SURFACE (`internal/codegen/lua`: a module per schema file,
the type wire read and written against the C++-pinned goldens, over the
vendored serialize.lua), and the TABLE SURFACE (`internal/codegen/luatable`
and `runtime/*.lua`: form `1` load and save, the fixed form `3` against the
byte oracle, the message form `2`, retain-unknown on both, the JSON and
hand-editable text forms, base64 text, block read and build, cook open,
check and write). Every unit of the tables conformance manifest generates
(33 units, 0 refused) and stands alone (`TestLuaUnitStandsAlone`); the
conformance driver answers all eighteen surfaces; `make test-lua` runs the Go
tests of both packages, the wire-fuzz differential against the Go engine and
the shared hostile corpus. The open work is the `port/lua/open/*` entries of
`docs/roadmap.sexp`; THE REGISTER below says which techniques they are.

## THE TYPE MAPPING

Lua has no annotations and no declared field types. A field's kind therefore
lives in its DESCRIPTOR, never in the value: the type wire's kinds are in the
generated read and write functions (as in every target), and the table wire's
are in the table module's descriptors (`luatable`, "fields"). **A value of the
wrong Lua type on write is a named refusal, never a silent coercion**: a float
where an integer belongs, an integer out of its declared range, a string past
its bound, an enum name no variant carries. `math.type` tells an integer from
a float, so the check is exact.

Field keys: a TYPE's fields are keyed by `ir.GoExportName` (PascalCase, the
checker's one mapping, so its collision detection covers Lua as it covers Go,
C# and JavaScript); a TABLE's fields are keyed by the schema field name, as in
the JavaScript table classes. A key that is not a Lua name is written
`["key"]`; a PascalCase key never meets a Lua reserved word.

One row per field kind of [SPEC.md](SPEC.md) §4.3 and
[SPEC-TABLES.md](SPEC-TABLES.md) §2. The fixed-table column is §3.4's form
`3`: the same Lua value as the table column, for the kinds a fixed table
admits (the checker refuses the rest by name).

| kind | type (packet) | fixed table | table |
|---|---|---|---|
| `bool` | `boolean` | `boolean` | `boolean` |
| `int8`..`int64`, `uint8`..`uint64` | integer; a uint64 is the same 64 bits read unsigned | integer | integer |
| ranged `intN \| min, max` | integer; the range is the writer's refusal | integer | integer; descriptor `lo`, `hi` |
| `bits(N)`, N ≤ 64 | integer (bits(64) uses all 64) | integer | integer; descriptor `bits` |
| `int128`, `uint128` | `{ lo = <int>, hi = <int> }`, two's complement | carried: the sixteen-byte store (§3.4, §15) | carried: descriptor `shape = "wide"`, low half first |
| `fixed(I, F)`, `ufixed(I, F)` | the RAW scaled integer; I+F = 128 is `{ lo, hi }` | carried: the raw integer at its storage width (§3.4, §15) | carried: `shape = "int"`, or `shape = "wide"` at 128 bits |
| `float32` | float: the double that is exactly the float32 | float | float; descriptor `f32 = true` |
| `float64` | float | float | float |
| compressed `float32 \| min, max, resolution` | float; the wire is the step index | float | float |
| enum | integer (None = 0, variants from 1) | variant NAME string, nil for None | variant NAME string, nil for None (§5: identity is the name's hash) |
| `flags` | integer: the uint64's 64 bits | integer | integer; descriptor `bits` |
| `string(N)` | Lua string; its length is the used length | Lua string | Lua string; descriptor `bound` |
| `bytes(N)` | Lua string | Lua string | Lua string |
| `wstring(N)` | sequence of UTF-16 code units (integers) | the same | the same |
| `timestamp` | integer: int64 nanoseconds | integer | integer; descriptor `timestamp = true` |
| a `type` by value | its constructed table | — | — |
| a `table` by value | — | its constructed table | its constructed table |
| `union` | `{ Type = <tag>, <Arm> = ... }`, every payload arm pre-built | `{ tag = 0 }` and the selected arm under its name | the same |
| `[N]T` | sequence of N, pre-filled | sequence of N | sequence of N |
| `[..N]T`, `[A..N]T` | sequence pre-filled to N, plus `<Name>Count` born at A | sequence born with A elements | the same |
| `[]T` | — | — | sequence, born empty |
| `[E]T` (enum-keyed) | — | sequence of E.Max, key k at index k | the same |
| `?T` | — | the value plus `<field>_present = false` (§2.3) | the same |
| `*T`, `*bytes`, `*string` | — | — | nil until set; `false` is the null element of a sequence |
| `map[K]V` | — | — | Lua table keyed by the key (sorted on save, §2.8) |
| `[..N]string(M)` (text list) | — | — | sequence of strings |
| `const(V, B)`, `reserved(B)`, `align` | no storage | — | — |

Constructors: `New<Name>()` builds a value at its declared defaults (SPEC §5)
with every nested type and union arm pre-built, never one table aliased into
two slots (the type test checks that writing one union arm leaves the other at
its defaults). Constants are folded literals with the schema expression as a
comment.

## 64-BIT AND 128-BIT INTEGERS

Lua 5.4 has a native 64-bit integer subtype. Measured on the pinned
interpreter: `math.maxinteger` is `9223372036854775807`, `math.mininteger` is
`-9223372036854775808`, and `math.type(1)` is `integer`.

- **int64** is the integer.
- **uint64** is the same 64 bits read unsigned. Comparison is `math.ult`;
  `//` and `%` on a value above 2^63 go through the unsigned helpers of the
  card that needs them; the JSON writer prints the unsigned decimal with
  integer arithmetic, never `%d` and never a float. A uint64 literal above
  int64's ceiling is a wrapping hex literal (`0xffffffffffffffff` is `-1`,
  the same bits); `math.mininteger` is spelled by name, because the decimal
  `9223372036854775808` lexes as a float.
- **128-bit** values are two halves, `{ lo = <low 64>, hi = <high 64> }`, each
  an integer, two's complement across the pair. The type wire carries them and
  the table wire carries them too: the fixed form stores the sixteen-byte raw
  image and the message form its announced ranged packing (docs/
  SPEC-TABLES.md §3, §3.3, §3.4, §15).
- A wire id (§5) is a uint64 hash and is held as its 64 bits.

**LuaJIT is not covered.** LuaJIT is Lua 5.1: no integer subtype (every
number is a double), no `string.pack`, no `//`, no bitwise operators (its
`bit` library is 32-bit). None of the value model above holds there. A later
`luajit` leg would take the Luau leg's number model (doubles, with 64-bit
values as two 32-bit halves), not this one.

**Luau.** At the time this page was written `docs/PORT-LUAU.md` was not yet
on `origin/sprint/schema-luau`, so nothing is cited from it. The shapes the
two legs can share are the ones that do not depend on the number model: the
module-per-file layout, the descriptor-as-data table surface and its field
facts, the constructor discipline, the luarun-style harness and the card map.
The Lua leg never imports Luau code; a shared shape is written twice, once in
each leg, and each is held by its own tests.

## BYTE BUFFERS

- A buffer READ is a Lua string (immutable bytes) and an integer cursor, with a
  limit for a nested body (M1's limit form: narrowing an integer, never
  slicing a substring). Fixed-width little-endian values are
  `string.unpack("<i4", s, pos)` and friends, every format naming `<`, so the
  host's byte order is never consulted (M12).
- A buffer WRITE is a table of pieces joined once by `table.concat` at the
  end, never repeated string concatenation.
- The type wire's bit stream (SPEC §4.3: LSB-first, 64-bit scratch flushed as
  little-endian words) is integer arithmetic on the 64-bit scratch:
  `<<`, `>>` (logical), `&`, `|`.
- **float32 rounding and patterns.** A float32 field holds the double that is
  exactly its float32 value. Writing a bare float32 takes its 32-bit pattern
  by `string.pack("<f", v)` for an ordinary value, but a NaN's payload and a
  signalling NaN must ride bit for bit (SPEC §4.3; PORTING.md M21), and the
  C conversion behind `string.pack("<f")` is a float conversion, so the
  pattern is moved by integer bit surgery between the float32 and float64
  layouts and never through a conversion. The type-wire cards own the helper
  and its corpus check. The TABLE wire is the other case: it carries finite floats only
  (docs/SPEC-TABLES.md §3.3), every read tests a cell's raw bits for a NaN or an
  infinity before it converts, and the conversion is exact for every finite
  float32 (PORTING.md M21, retired; `TestLuaFloat32ConversionIsExactForEveryFinitePattern`).

## THE RUNTIME LAYOUT

Fixed now, for the pro cards. Never rename it.

| path | what |
|---|---|
| `compiler/target_lua.go` | the target, a builtin of the ordinary `bin/schema` |
| `internal/codegen/lua/lua.go` | the TYPE emitter; `RegisterPass` (a later file's `init()` adds its emission step) and `RegisterRuntime` (its embedded `runtime/*.lua`), so no later card edits lua.go |
| `internal/codegen/lua/read.go`, `write.go` | the type wire, later; each embeds `runtime/*.lua` by glob and registers it |
| `internal/codegen/lua/runtime/*.lua` | the type-wire runtime (`bitreader.lua`, `bitwriter.lua`), emitted beside the modules |
| `internal/codegen/lua/runtime/serialize*.lua` | the serialize port vendored verbatim from `mas-bandwidth/serialize.lua` at release `v2.1.1` (format 2.0; `SERIALIZE_LUA_PIN` and `SERIALIZE_LUA_COMMIT` in `make/lua-serialize.mk`), pin `runtime/SERIALIZE_PIN` (the tag, the commit, one `sha256 <file>` line per file); `make vendor-serialize-lua` refreshes it. `wstring(N)` is composed by the generated code (SPEC §4.12), since format 2.0 removed it from the runtime. The type-wire cards call this library (serialize.WriteStream/ReadStream, serializeInt/serializeFloat/...) rather than a private bit reader or writer |
| `internal/codegen/lua/luarun/luarun.go` | the test helper |
| `internal/codegen/luatable/luatable.go` | ONE DESCRIPTOR per table as data a single runtime walks (the `jstable/variableruntime.go` shape), the constructors, `RegisterPass` for `fixedform.go` and `messageform.go`, and the `//go:embed runtime/*.lua` glob |
| `internal/codegen/luatable/runtime/*.lua` | the table runtime, one file per card; `descriptor.lua` (the shared descriptor walk) is the first |
| `test/conformance/lua/driver` | hands every surface to `test/conformance/lua/main.lua`; ABSENT (empty list, exit 2) while that file is absent |
| `test/conformance/lua/ci.json` | the conformance targets and the `lua` toolchain field |
| `make/lua.mk` | `dist-lua`, `toolchain-lua`, `test-lua`, `generated/lua` and `generated/lua-ludicrous`, `build/conformance-lua` |

**How a module requires its runtime.** A unit is generated into one
directory, and that directory goes on `package.path` (`<dir>/?.lua`). A
module requires its siblings and its runtime by basename —
`require("Enums")`, `require("descriptor")` — binding each to a local, and
returns one table, `_M`. Nothing is global. A schema file whose basename
collides with a runtime file, with `_M`, or with the unit's table module
(`<Package>Tables.lua`) is refused by name at generation.

The type surface emits `<Base>.lua` per schema file; the table surface emits
one self-contained `<Package>Tables.lua` per unit (it requires no packet
module) plus the table runtime.

## THE MESSAGE FORM'S ANNOUNCEMENT

`M.announceRead` reads the announcement as the ordinary form-`1` file
[SPEC-TABLES.md](SPEC-TABLES.md) §3.3 defines, not as a byte-for-byte match
with the unit's own constant: the build version keys the vocabulary and gates
nothing, so an engine announcement at another build reads. It walks the
trailer, applies the two strict body checks (one build version, kind `9`,
eight bytes; one vocabulary, kind `14` over element kind `6`), and counts the
entries before resolving the unit's projection-order vocabulary. The two
receiver bounds are arguments: the vocabulary field's byte length is checked
before an entry is touched and the entry count is refused at the entry past
the caller's capacity, both as `vocabulary_too_large`. `_M.AnnounceRead`
forwards them, and the shared hostile corpus's runner states `MaxBytes=8` and
`MaxEntries=1` for its two bound cases.

## THE HARNESS

Lua has the `io` library, so the conformance driver is ONE lua process per
surface: `driver <manifest> <surface> <outdir>` execs
`$LUA test/conformance/lua/main.lua` with the same arguments, and main.lua
reads the manifest and its wire, JSON and dump files itself and writes its
answers to outdir. No shell glue per case.

The Go tests use `luarun`: `luarun.Run(t, dir, main, inputs)` writes the
inputs as the module `inputs.lua` (byte strings), runs `dir/main` under `$LUA`
or `<repo>/dist/lua-5.4.8/src/lua`, and returns stdout; an interpreter that
does not resolve FAILS the test naming `make dist-lua`, never a skip. A probe
prints bytes as lowercase hex lines (`hexline.lua`, `luarun.HexLines`), or a
value as the canonical dump (`dump.lua`, `luarun.Dump*`) that the Go side
computes independently from the IR. `luarun.LoadUnit` builds a unit from
schema paths the manifest's way. Everything runs in `t.TempDir()`: no
network, no clock, no service.

## THE TABLE WIRE'S COUNT REFUSAL

A collection count is an unsigned 64-bit number ([SPEC-TABLES.md](SPEC-TABLES.md) §3).
A **map** (§2.8) or an **unbounded array** (§2.9) whose count is past the
int32 storage cap is the engine's **CountRefusal** (`internal/tablewire/decode.go`,
`decodenodes.go`): the tolerant load keeps nothing for the field and sets
`report.reason = "count_refusal"`, and meeting the cap adds no malformed event
or other counter. A count AT the cap decodes normally. The strict read
(`L.Load`, §6.5) answers nil with `report.reason = "count_refusal"` rather
than accepting the wire, because the engine's CountRefusal is the load's own
result, not a report counter. The hostile-corpus runner's wire branch returns
a report's named reason before it calls the answer `damaged`, so
`wire-count-refusal` lands the manifest's `refuse/count_refusal`; damage that
names no reason stays `damaged`.

## THE CALLER-OWNED LOAD, THE KEYED ACCESSOR, THE TYPED SAVE

The table runtime's open techniques (2026-10-29; PORTING.md M1, M5, M13, M15,
M18, M21, M22), each held by its own gate.

**The load fills storage the caller owns** (M1, I1, I9, I14).
`load.Load(T, name, bytes, report, state, into)` and
`load.LoadTolerant(...)` take, after the report, an optional loader `state`
(`load.NewState()`: the readers, the id table and its seen-set, the queue of
pointer slots, the context) and an optional `into`, the caller's value, which
the load puts back to its declared defaults IN PLACE (`D.reset`, deep-equal to
the constructor over every table) and reads into. A caller that loads many
records keeps one of each. Both are nil in the plain form, which answers what it
always answered. A nested body, an arm, an array body, a map entry and a keyed
slot are bounded by narrowing the one reader's limit (`narrow`, `restore`), never
by building a view. What a Lua load cannot read into is a counted or unbounded
array, a list, a map, a union arm, a pointer's node, a wide integer and a string
past 40 bytes: each is a table or an object the load builds, and each row of the
allocation gate says what it licenses (`internal/codegen/luatable/alloc_test.go`,
`make tables-lua-alloc`): the by-value rows are held to exact zero bytes a load.
The soak (`make tables-lua-soak`) is a pass count, `TABLES_LUA_SOAK_ITERATIONS`,
never a clock. A load never writes through a table a descriptor owns: a clamped
wide bound is copied.

**Keyed arrays are indexed by key** (M5). `T.Keyed(table, field)` answers
`get(array, key)`, `set(array, key, value)`, `each(array)` and `key(name)`; one
unsigned compare refuses None and any key past `E.Max`.

**The descriptors are constants** (M13). The module ends with `D.seal`: derived
facts built once at require, every data table frozen against new keys; the
overwrite of an existing key is the fingerprint gate's.

**One pointer walk** (M15): `D.number_nodes`, shared by the save, the retaining
save, the cook and the text form. **A union arm is a field line** (M18):
`read_payload`, called by `read_field` and by the arm. **The typed save**
(M22): `T.SaveTyped<Name>(value)` and `save.SaveTyped`, the scalar leaves written
straight from the value.

## THE REGISTER

The lua column of [PORTING.md](PORTING.md) is checked against the tree, cell by
cell (2026-10-29). Carried, each naming its proof: M1, M4b, M5, M6, M7, M8, M9,
M11, M12, M13, M14, M15, M16, M18, M19, M20, M22, I1, I3, I4, I5, I6, I7, I9,
I10, I13, I14, I15, J1, J2 and J4. `—`, the platform's reason stated: M2 and M3
(a Lua 5.4 integer and float are unboxed values), M4 (no inlining directive or
aliasing qualifier), M10 (the VM owns allocation), M17 (no arena: a blob is one
string of its own length), M21 (retired by the finite-only rule: the conversion
only ever sees a finite float32) and J5 (no table-wire bench). M11 and J1 are
carried in the Lua form: Lua has no generated accessor, so the gate holds the
runtime's layout derivation to the compiler's rather than an accessor to a
descriptor. Every other cell is `❌`: the register's grammar requires a `#NNN`,
the only issue that names the Lua port is #381 (closed, the language list), so
the cells cite it beside the `port/lua/open/*` roadmap entry that carries the
work (roadmap entries, never new issues). I2 and I11 are the negative controls,
which the gates work carries.

## THE CARD MAP

| card | surface | files |
|---|---|---|
| schema-lua-port-design | this page, the skeleton, both type and table surfaces without codecs | the files above |
| schema-lua-type-wire-read | the type wire's read side: every type, union and array; the C++-pinned goldens | `lua/read.go`, `lua/runtime/bitreader.lua` |
| schema-lua-type-wire-write | the type wire's write side, byte for byte with the goldens | `lua/write.go`, `lua/runtime/bitwriter.lua` |
| schema-lua-table-wire-load | the variable form's tolerant load `LoadTolerant` and its report (§3.1, §4) | `luatable/runtime/load.lua` |
| schema-lua-plain-load-is-strict | the plain `Load` is the strict read; `LoadTolerant` is the tolerant one (§6.5) | `luatable/runtime/load.lua`, `luatable/runtime/refusal.lua` |
| schema-lua-table-wire-save | the variable form's save, every instance byte for byte | `luatable/runtime/save.lua` |
| schema-lua-fixed-form | form `3` against the byte oracle (docs/FIXED-FORM-ALGORITHM.md) | `luatable/fixedform.go`, `luatable/runtime/fixed.lua` |
| schema-lua-message-form | form `2` against its goldens | `luatable/messageform.go`, `luatable/runtime/message.lua` |
| schema-lua-json-text-form | the JSON text form, both ways (§16) | `luatable/runtime/json.lua` |
| schema-lua-base64-text | base64 text over a text channel | `luatable/runtime/text.lua` |
| schema-lua-refusals-by-name | refusals at compile time and at read time, the claimed names | `luatable/runtime/refusal.lua`, `internal/tablenames/lua.go`, `compiler/tableslua_test.go` |
| schema-lua-conformance-leg | `make conformance` and CI: `test/conformance/lua/main.lua`, the CI step keyed on `ci.json`'s `lua` field | `test/conformance/lua/main.lua`, `make/lua-conformance.mk` |
| schema-lua-fuzz-differential | the wire fuzzer drives the Lua load against the Go engine | `test/conformance/lua/wirefuzz.lua`, `make/lua-fuzz.mk` |

The conformance driver answers all eighteen surfaces. The block and cooked
accelerators are read on the reading tier's terms (docs/SPEC-TABLES.md §7,
§19): a Lua string has no address, so `BlockOpen` and the cook's `Open` take
the base's placement as an argument (`base_offset`, the Elixir leg's `lead`),
the cook's `Open` is the header and the base alone (O(1)) and `Check` runs
`cook-check`'s two passes, and `Cook` writes either byte order (`order` 1 or
2). One row is named and left: a `retain-message` batch another build wrote,
because `announceRead` counts the announced entries but resolves the
receiver's own vocabulary rather than the sender's (§3.3).

Owed and named, each a `port/lua/open/*` roadmap entry: the cross-build
announcement read above (`retain-message-cross-build`); the message form's
refusal of a plain `table` root (`message-form-plain-table`: `loadMessages`
does not check `MessageRoots`, so `graph_tree` on the plain `Scene` answers
bytes where the Go reference answers absent, and the refusal needs a reason
name SPEC-TABLES §11 does not yet give); the labelled bench board
(`bench-board`: the leg and its gate are in, a measured sitting is not); and a
union arm named `tag`, whose payload shares the discriminator's key
(`arm-named-tag`, found by the M18 gate).

Closed 2026-10-29: the USAGE example (`tables-lua-usage`, docs/USAGE.md's
`lua-table-usage` fragment run against `tables/pointers`, with a two-witness
control); the bench leg and the release tier (`bench/tables/lua`,
`tables-lua-bench-gate`, `tables-lua-release`, which certify.yml derives from
the Makefile); and the card facade (`tables/cards/lua`: `cards.lua`, its driver,
`make cards-lua-conformance` on all nine surfaces, three controls). The card
leg found two faults in `runtime/retain.lua`, fixed there: `wire_nodes` read the
node table's leading count as a node, and the capture counted the unit's own
string and bytes blob nodes as `retain_lost`.

## DEEPEN

- **The pro move.** Descriptors as data and one runtime that walks them:
  in Lua a generated function per field costs load time and memory and buys
  little, while one walk over constant tables is what LuaJIT-free Lua runs
  fastest and what the Luau leg can mirror.
- **Raw material.** The C++-pinned goldens (`testdata/wire`), the tables
  manifest and its JSON and report files, `docs/FIXED-FORM-ALGORITHM.md`, and
  the JavaScript leg as the closest worked port.
- **Methods.** Red first on the manifest; the canonical dump compared with a
  Go-side expectation from the IR; an in-memory emitter sabotage per codec
  (the read card's 31-bit control); the Go engine as the fuzz oracle.
- **The outside judge.** The C++ reference's bytes, never the Lua leg's own
  output: every golden a Lua codec meets was written by another language.
