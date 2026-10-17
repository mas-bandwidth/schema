# PORT-LUA — the Lua port's design

The Lua leg is vanilla **PUC-Rio Lua 5.4**, pinned at 5.4.8 and built from
source (`make dist-lua`). This page is the design every card of the
`schema-lua` stream follows; where it is silent, the JavaScript leg
(`internal/codegen/js`, `internal/codegen/jstable`) is the worked example.
It is written in the house order: the value model first, because every codec
stands on it, then the files, then how the leg is held.

What exists after the design card (schema-lua-port-design): the target
(`compiler/target_lua.go`, behind the `schema_leg_lua` build tag), the TYPE
SURFACE (`internal/codegen/lua`: a module per schema file with constants,
enums, flags, union tags and constructors), the TABLE SURFACE
(`internal/codegen/luatable`: one descriptor per table and per table-side
union, and constructors), the test helper (`internal/codegen/lua/luarun`), the
build leg (`make/lua.mk`), the conformance driver and the register column.
There is **no codec** on either wire yet; THE CARD MAP below names who writes
each one.

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
| `int128`, `uint128` | `{ lo = <int>, hi = <int> }`, two's complement | refused today (§15) | refused today (§15) |
| `fixed(I, F)`, `ufixed(I, F)` | the RAW scaled integer; I+F = 128 is `{ lo, hi }` | refused today (§15) | refused today (§15) |
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
  an integer, two's complement across the pair. The type wire carries them;
  the table descriptors refuse the 128-bit and fixed-point kinds by name
  today (docs/SPEC-TABLES.md §15, schema#366), exactly as the other ports did
  before they landed them.
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
  and its corpus check.

## THE RUNTIME LAYOUT

Fixed now, for the pro cards. Never rename it.

| path | what |
|---|---|
| `compiler/target_lua.go` | the target, behind `//go:build schema_leg_lua` until the wires have claims |
| `internal/codegen/lua/lua.go` | the TYPE emitter; `RegisterPass` (a later file's `init()` adds its emission step) and `RegisterRuntime` (its embedded `runtime/*.lua`), so no later card edits lua.go |
| `internal/codegen/lua/read.go`, `write.go` | the type wire, later; each embeds `runtime/*.lua` by glob and registers it |
| `internal/codegen/lua/runtime/*.lua` | the type-wire runtime (`bitreader.lua`, `bitwriter.lua`), emitted beside the modules |
| `internal/codegen/lua/luarun/luarun.go` | the test helper |
| `internal/codegen/luatable/luatable.go` | ONE DESCRIPTOR per table as data a single runtime walks (the `jstable/variableruntime.go` shape), the constructors, `RegisterPass` for `fixedform.go` and `messageform.go`, and the `//go:embed runtime/*.lua` glob |
| `internal/codegen/luatable/runtime/*.lua` | the table runtime, one file per card; `descriptor.lua` (the shared descriptor walk) is the first |
| `test/conformance/lua/driver` | hands every surface to `test/conformance/lua/main.lua`; ABSENT (empty list, exit 2) while that file is absent |
| `test/conformance/lua/ci.json` | the conformance targets and the `lua` toolchain field |
| `make/lua.mk` | `dist-lua`, `toolchain-lua`, `test-lua`, `build/schema-lua`, `build/conformance-lua` |

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

## THE REGISTER

The lua column of [PORTING.md](PORTING.md) is filled for every technique.
Six cells are `—`, the platform's reason stated: M2 and M3 (a Lua 5.4
integer and float are unboxed values, so there is no box to avoid), M4 (no
inlining directive or aliasing qualifier), M10 (the VM owns allocation; the
count is pinned instead), M11 (no struct layout), J5 (no table-wire bench).
Every other cell is `❌` and names the stream card that carries it, or says
no card carries it yet. The register's grammar requires a `#NNN` on a not-yet
cell; the only issue that names the Lua port is #381 (closed, the language
list), so the cells cite it beside the card. The cards, not the issue, are
the carry-across record (the sprint retired per-technique issues).

## THE CARD MAP

| card | surface | files |
|---|---|---|
| schema-lua-port-design | this page, the skeleton, both type and table surfaces without codecs | the files above |
| schema-lua-type-wire-read | the type wire's read side: every type, union and array; the C++-pinned goldens | `lua/read.go`, `lua/runtime/bitreader.lua` |
| schema-lua-type-wire-write | the type wire's write side, byte for byte with the goldens | `lua/write.go`, `lua/runtime/bitwriter.lua` |
| schema-lua-table-wire-load | the variable form's tolerant load and its report (§3.1, §4) | `luatable/runtime/load.lua` |
| schema-lua-table-wire-save | the variable form's save, every instance byte for byte | `luatable/runtime/save.lua` |
| schema-lua-fixed-form | form `3` against the byte oracle (docs/FIXED-FORM-ALGORITHM.md) | `luatable/fixedform.go`, `luatable/runtime/fixed.lua` |
| schema-lua-message-form | form `2` against its goldens | `luatable/messageform.go`, `luatable/runtime/message.lua` |
| schema-lua-json-text-form | the JSON text form, both ways (§16) | `luatable/runtime/json.lua` |
| schema-lua-base64-text | base64 text over a text channel | `luatable/runtime/text.lua` |
| schema-lua-refusals-by-name | refusals at compile time and at read time, the claimed names | `luatable/runtime/refusal.lua`, `internal/tablenames/lua.go`, `compiler/tableslua_test.go` |
| schema-lua-conformance-leg | `make conformance` and CI: `test/conformance/lua/main.lua`, the CI step keyed on `ci.json`'s `lua` field | `test/conformance/lua/main.lua`, `make/lua-conformance.mk` |
| schema-lua-fuzz-differential | the wire fuzzer drives the Lua load against the Go engine | `test/conformance/lua/wirefuzz.lua`, `make/lua-fuzz.mk` |

Owed and named, not carried by any card yet: the block and cooked
accelerators, the allocation gate (`collectgarbage("count")` is the
instrument), the bench leg, the release tier and the analyzer-clean gate
(`luac -p`). `make test` now probes the `lua` toolchain: a CI job that runs it
needs `make dist-lua` or `LUA=lua` with a 5.4 interpreter, which is the
conformance-leg card's workflow step.

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
