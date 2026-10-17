# The Luau port: the design every Luau card copies

Luau is Roblox's dialect of Lua: gradually typed, a `buffer` type for bytes,
and — on the pinned release — a native 64-bit `integer`. It is the scripting
language of Virtual Go, the space game and lego (Glenn Fiedler, 2026-10-17:
"We will use luau as scripting in virtualgo, space game and lego"), so the
schema port is held to every surface the other legs carry. Vanilla Lua and
Squirrel follow it, and copy this page's shape.

This page fixes the design. The skeleton it describes is in the tree:
`compiler/target_luau.go` (behind the `schema_leg_luau` build tag),
`internal/codegen/luau` (the type surface), `internal/codegen/luautable` (the
table surface), `internal/codegen/luau/luaurun` (the test helper),
`make/luau.mk`, `test/conformance/luau/driver` and `bench/tables/luau/leg`.
The skeleton has no codec: the type and table surfaces are values,
constructors and descriptors, and every wire, form and text surface is a later
card (THE CARD MAP).

**The pin.** Luau 0.741, `make dist-luau`: the luau-lang release
`luau-ubuntu.zip`, sha256
`134dc762ad26232af83e43f98dec03ff6030dd3a4452f9408b9d50ccea025503`, unpacked
into `dist/luau-0.741/` (`luau`, `luau-analyze`, `luau-ast`, `luau-compile`);
any other sha256 is refused before anything is unpacked. `$LUAU` overrides the
interpreter and `$LUAU_ANALYZE` the analyzer; `make toolchain-luau` refuses a
pin that does not resolve, by name (the toolchain gate, issue #599).

## THE TYPE MAPPING

Every generated module is `--!strict` and exports a Luau `type` for every
type, union, enum, flags set and table, so a consumer's own code is checked
against the schema by `luau-analyze`; every module the tests generate passes
it with no errors (`TestLuauTypesConstructAtTheirDefaults`,
`TestLuauTableDescriptorsLoad`). A value is a Luau table with EVERY field
present from construction (the storage principle, SPEC §6.1), each named by
`ir.GoExportName` — the Go, C# and JavaScript spelling, so the checker's
collision claims already cover Luau. `New<Name>()` builds a value at its
defaults (SPEC §5): a specified default where one is declared, the zero form
otherwise, nested types and union arms pre-allocated, and a `[A..B]` count
born at `A`.

The value domain has three integer widths and one rule: **a `number` (an
IEEE double) holds every integer of 32 bits or fewer, an `integer` holds 64,
and an `Int128` (two `integer` halves) holds 128.** The rule follows the
storage width, not the wire width, so a ranged `int64` is an `integer` however
few bits it rides in.

### Packet types (SPEC §4.3, §4.7, §4.8, §4.12, §4.13)

| schema | Luau value | construction |
|---|---|---|
| `bool` | `boolean` | `false`, or the default |
| `int8` … `int32`, `uint8` … `uint32`, ranged or bare | `number` (exact: every 32-bit integer is a double) | `0`, or the default |
| `int64`, `uint64`, `timestamp` | `integer` — the two's-complement bit pattern; a `uint64` past 2^63 is the same 64 bits, compared with `integer.ult` and friends | `0i`, or the default as an integer literal (`0x…i` past int64) |
| `int128` (ranged), `uint128` | `Int128` = `{ lo: integer, hi: integer }`, the low half first, as the wire orders it | `_Rt.int128(lo, hi)` |
| `bits(N)` | `number` for N ≤ 32, `integer` for N > 32 | `0` / `0i` |
| `fixed(I, F)`, `ufixed(I, F)` | the RAW scaled integer at its storage width I+F: `number` ≤ 32, `integer` at 64, `Int128` at 128 | the default's raw value |
| `float32` | `number` holding the float32 value exactly (f32 ⊂ f64) | the default rounded as Go's `float32(v)`, rendered as the shortest literal that parses AS A DOUBLE to it (never the shortest float32 text, which Luau reads as the unrounded double); pinned by TestLuauFloat32DefaultsRoundAsGo |
| `float32 \| min, max, resolution` | `number` — the attributes describe the wire, not the storage | as `float32` |
| `float64` | `number` | `0`, or the default |
| an enum `E` | `number`; `M.E` is a frozen table `{ None = 0, <variants> = 1.., Count, Max }` and `M.EnumNameE(v)` names a value | the default variant's number, else `0` (None) |
| a `flags` set `F` | `integer` (uint64 storage in every target, SPEC §4.2); masks are `M.F<Variant>` integers, `M.FCount` the declared count, `M.FlagNameF(bit)` | `0i`, or the default mask |
| a type `T` | `T`, a nested table | `New<T>()` |
| a union `U` (§4.8) | `U` = `{ Type: number, <Arm>: <arm value>... }`; `M.UType` is the tag table (`None`, arms from 1, `Count`, `Max`) | `Type = 0`, every payload arm pre-allocated; a payload-free arm has no slot |
| `string(N)`, `bytes(N)` | a `buffer` of N bytes and `<Name>Length: number` beside it (SPEC §4.7) | `buffer.create(N)`, the default's bytes written in, length set |
| `wstring(N)` (§4.12) | a `buffer` of 2N bytes holding UTF-16LE code units, and `<Name>Length` in code units | `buffer.create(2N)` |
| `[N]uint8` | a `buffer` of N bytes, 0-based — the bulk path's own type | `buffer.create(N)` |
| `[N]T`, `[E]T` (enum-keyed: key k at index k), any other element | a 1-based array `{T}` of exactly N elements | `table.create(N, zero)` for scalars, `_Rt.array(N, New<T>)` for tables and 128-bit values |
| `[..N]T`, `[A..N]T` | the same array of N, and `<Name>Count: number` beside it | the count born at A (SPEC §4.6) |
| `const`, `reserved`, `align` | no storage | — |
| `if` / `else` branches (§4.4) | both sides' fields are stored; a read zeroes the untaken side (SPEC §5) | both at their defaults |

A schema `const` is a field of the module: a `number` for a float and for an
integer neither declared 64-bit nor past ±(2^53 − 1), an `integer` otherwise,
an `Int128` for 128-bit storage. Every constant expression is FOLDED to its
literal, with the schema spelling kept as a trailing comment where it differs:
Luau has no constant expressions to keep it in, and a fold is always exact.

### Tables (docs/SPEC-TABLES.md §2), for `table` and `fixed table` alike

A table is the same value shape as a type: one class of storage serves both
the fixed form and the variable form, as in every other leg, so the mapping
above holds field for field and the rows below are what tables add. The
declared class is a descriptor fact (`fixed`), not a storage difference.

| schema (§2) | Luau value | construction |
|---|---|---|
| a table nested by value | its type, in its `<Base>Table` module | `New<T>()` |
| `*T` (§2.1) | `T?` — nil is null | `nil` |
| `*bytes`, `*string` (§2.5) | `buffer?` — nil is absent; the buffer is exactly the blob's bytes | `nil` |
| a bare `string` element or map key (§2.13.1) | `buffer?` | `nil` |
| `?T` (§2.3) | the value, and `<Name>Present: boolean` beside it | the value at its zero, `Present = false` |
| `[]T` (§2.9) and text lists (§2.13) | a 1-based array `{T}`, its length the data's | `{}` |
| `[..N]*T`, `[N]*T` | `{T?}`, nil-filled, and the count where counted | `table.create(N)` |
| `map[K]V` (§2.8) | `{Entry}`, an array of the generated `<Table><Field>Entry` table `{ Key, Value }`, which the save walk sorts by key | `{}` |
| a union with table arms (§2.6) | as a packet union, in the table module; an arm is a field line, so it takes that field's value | `Type = 0` |
| `\| required`, `\| deprecated`, `was`, `json = "…"`, `forms`, `integrity` | no storage: descriptor facts (`required`, `deprecated`, the id's source name, `json`) and generation facts | — |

**The descriptor.** Beside every table's type and constructor,
`<Name>Descriptor` is the table AS DATA (docs/SPEC-TABLES.md §8.1): its node
type id (`integer`, the FNV-1a 64 of its wire name, §3.1), whether it is a
table and its class (`fixed`, `variable`), what a pointer walk from it may
place, and one entry per field — the field's wire id, name, Luau slot, text
key, ordinal, header kind (§3), its SHAPE (`bool int float str text bytes enum ptr sblob bblob tab union arr map`) and what the shape needs: an element
descriptor, the array kind and bounds, an enum's variant ids, the integer's
kind width, signedness and value domain, its clamp range in that domain, a
fixed field's (I, F), and the default. Unions get `<Name>Descriptor` with
their arms; a packet type a table reaches by value gets a descriptor too,
because it rides as a table body. Descriptors are frozen and registered by
name in `schema_tables.luau` (`S.lookup(name)`), and every module whose
descriptor names another requires the module that registers it. The ONE
runtime walk per form reads them; no form emits per-table code except where
its card says so.

## 64-BIT AND 128-BIT INTEGERS

Luau numbers are doubles, so a 64-bit value past 2^53 needs a representation.
Measured on the pin (hulk, 2026-10-17): Luau 0.741 has the `integer` type
(`typeof(integer.create(1))` is `integer`; RFC luau-lang/rfcs
`type-long-integer.md`), integer literals with an `i` suffix (`5i`;
`0xffffffffffffffffi` is the bit pattern of −1), the `integer` library (`add sub mul div udiv rem urem band bor bxor bnot lshift rshift arshift lt le ult ule gt ge ugt uge tonumber fromstring create …` — arithmetic operators do NOT
apply to integers, the library does), integers as table keys by value, and
`buffer.readinteger` / `buffer.writeinteger`. `integer.fromstring` SATURATES
at int64's maximum (`"18446744073709551615"` reads as `9223372036854775807`),
so generated code never spells an unsigned value in decimal: it uses the hex
bit pattern (`luau.IntegerLit`).

**The choice: the native `integer`.** It is a value, not an object, it holds
every 64-bit pattern, it is what the space game's and Virtual Go's own scripts
will use for 64-bit ids, and it reads and writes the wire's little-endian 8
bytes in one call. The portable alternatives were weighed and refused: a
two-halves pair allocates a table per value and makes every comparison a
function, and an 8-byte buffer per value allocates and cannot be a table key.
128-bit values are the `Int128` pair because no runtime has a 128-bit scalar.

**A runtime without it.** `schema_runtime.luau` checks `typeof(integer) == "table"` when it is required and raises `schema: this Luau has no integer type …` if not — a refusal by name at load, never a wrong value later. Roblox
is the open question: the type is a 2026 addition to luau-lang, and whether
Roblox's shipped Luau has enabled it is NOT measured here (no Roblox runtime
is reachable from the bench). If it has not, the port refuses on Roblox by
that message until it does; a two-halves fallback is a design option this
page records and does not build. The space game, Virtual Go and lego embed
the pinned luau-lang release, so they have it.

## BYTE BUFFERS

Bytes are Luau's `buffer`: fixed-size, mutable, 0-based, little-endian by
definition (so no read consults the host, PORTING M12). The codec uses
`buffer.create`, `readu8 … readu32`, `readi8 … readi32`, `readf32`,
`readf64`, `readinteger`, `readbits` / `writebits` (the type wire's bit
packing), `readstring` / `writestring`, `copy`, `fill`, `len`, and
`buffer.fromstring` / `tostring` at the edges.

**float32 writes (coordinator ruling from the Lua design read, 2026-10-17).**
A `float32` field stores a double. When a write is handed a double that is
not exactly a float32, it converts the value THE WAY THE GO REFERENCE WRITER
DOES — `float32(v)`, round to nearest, ties to even — so the bytes are the
same as the reference's, byte for byte. Strict writing means canonical,
deterministic output; it does not mean refusing ordinary rounding, so no
write refuses a double for having more precision than a float32. Luau's
`buffer.writef32` is that conversion (the C++ `float` cast, IEEE
round-to-nearest-even), and `_Rt.fround` exposes it. Non-finite values follow
the Go writer's rule as well: a finite double past float32's range becomes
±infinity, ±infinity stays itself, and a NaN converts as Go's `float32(v)`
converts it. Construction writes each default already rounded (the shortest
literal for `float32(v)`). **A float32 read from the wire never goes through
a conversion** (SPEC §4.3, PORTING M21): the read path, and a write of a
value that came off the wire, move a float32's bits by bit surgery between
`readu32` and the double's two 32-bit halves, as `internal/codegen/jstable`
does, so a signalling NaN and its payload survive a round trip.

## THE RUNTIME LAYOUT

Fixed now for the pro cards; never renamed.

| file | what it is | who writes it |
|---|---|---|
| `compiler/target_luau.go` | the target: the type surface, then the table surface, behind the `schema_leg_luau` tag until the packet surface has claims | this card; the tag is dropped when the port joins the builtins |
| `internal/codegen/luau/luau.go` | the TYPE emitter (`Gen`, `Slots`, `TypeRef`, the literal renderers) and `Register`, the hook a later file joins from its own `init()` | this card only |
| `internal/codegen/luau/read.go`, `write.go` | the type wire, joined through `luau.Register` | type-wire-read, type-wire-write |
| `internal/codegen/luau/runtime/*.luau` | the type-wire runtime, `go:embed` by glob, emitted beside the modules: `schema_runtime.luau` now, `bitreader.luau` and `bitwriter.luau` later | one file per card |
| `internal/codegen/luautable/luautable.go` | the TABLE emitter: types, constructors and one DESCRIPTOR per table as data, and `luautable.Register` | this card only |
| `internal/codegen/luautable/fixedform.go`, `messageform.go` | the two forms that need emitted code, joined through `luautable.Register` | fixed-form, message-form |
| `internal/codegen/luautable/runtime/*.luau` | the table runtime by glob, one file per card: `schema_tables.luau` now; `load.luau`, `save.luau`, `fixed.luau`, `message.luau`, `json.luau`, `text.luau`, `refusal.luau` later | one file per card |
| `internal/codegen/luau/luaurun` | THE HARNESS's helper | this card |
| `test/conformance/luau/driver`, `ci.json`, `main.luau` | the conformance leg | this card (driver); conformance-leg (`main.luau`) |
| `make/luau.mk` | the leg's build: `dist-luau`, `toolchain-luau`, `test-luau`, `build/conformance-luau` | this card; later cards add their own `make/luau-*.mk` |
| `bench/tables/luau/leg` | the tables-bench leg, ABSENT until a codec exists | the bench card, when carded |

**Modules.** One module per schema file, `<Base>.luau` (the type surface),
and `<Base>Table.luau` for a file with tables or with a packet type a table
reaches. A module binds its own declarations to `_M` (`_T` in a table module)
and every other module to `_<Base>`: no schema name starts with an
underscore, so no alias shadows a declaration. A module requires its runtime
as `require("./schema_runtime")` (and `require("./schema_tables")`) — Luau's
require is relative to the requiring file, so the runtime is emitted beside
the modules, with no search path and no package. A schema file whose module
would collide with a runtime module, folding case as Windows and macOS do, is
refused by name, as is a schema file `X` beside a file `XTable`. A require
cycle does not load in Luau, so a cycle between modules is refused by name
(`luau.RequireCycle`); a cross-file TABLE cycle never reaches it, because the
checker refuses that first (docs/SPEC-TABLES.md §11).

**Security: the unit directory and the runtime REPLACE the module search
path; they are never prepended to it** (coordinator ruling from the Lua
design read, 2026-10-17). Lua's form of the rule is `package.path` replaced
and `package.cpath` cleared. Luau has neither: its `require` has no search
path at all — a bare name is refused by the interpreter (measured: `require path must start with a valid prefix: ./, ../, or @`) — so the Luau form is
a rule on what generated code spells. **Every require a generated module or
runtime makes is `"./<module>"`, naming a module emitted into the same
directory**: never a bare name, never `../`, and never an `@alias`, because an
alias comes from a `.luaurc` the unit does not own (measured: a `.luaurc`
alias above the unit directory DOES resolve to a file outside it), and `../`
walks out of the directory. Nothing outside the unit directory and its
runtime can resolve, whatever the working directory or the files around it.
No native modules load: the pinned interpreter has no `package`, `dofile`,
`loadfile` or `io` (measured: all `nil`) and no C module loader, and an
embedding host (Virtual Go, the space game, lego) is told the same: its
require resolver for a schema unit resolves `./` against the unit directory
alone and refuses everything else. `TestLuauRequiresResolveInsideTheUnitOnly`
holds it: every require in the generated unit is `./` to a module the unit
emits, and with a shadowing `schema_runtime.luau`, `Wire.luau` and
`ArmDefaults.luau` planted one level outside the unit (each raising if
loaded) and a `.luaurc` aliasing that directory, the unit loads its own
modules; the control requires the shadow through `../` and sees it raise.

## THE HARNESS

The `luau` CLI has no `io` library (measured: `typeof(io)` is `nil`; `os`,
`require` and `loadstring` exist; program arguments follow `-a` and arrive as
`...`). So a Luau program can neither open a file nor read stdin, and the
shape is: **inputs go IN as source, results come OUT on stdout as lowercase
hex lines.**

- **Unit tests** use `luaurun.Run(t, dir, main, inputs)`: it writes `inputs`
  as the module `inputs.luau` (each input a `buffer.fromstring("\x..")`
  literal), runs `dir/main` under `$LUAU`, else
  `<repo root>/dist/luau-0.741/luau`, with `dir` as the working directory,
  and returns stdout. An interpreter that does not resolve FAILS the test
  naming `make dist-luau` — it never skips. `luaurun.HexProbe` is the Luau
  `hex(b, n?)` that prints a buffer as one hex line and `luaurun.HexLines`
  reads the lines back; `luaurun.Analyze` runs `luau-analyze` and fails on
  any error. Every test generates into a `t.TempDir()`, uses no real time, no
  socket and no network.
- **The conformance driver** (`test/conformance/luau/driver`) is a shell host
  shim. It writes the derived manifest as `build/conformance-luau/driver/ manifest.luau`, a module whose value is the manifest's text as one string
  literal, which `main.luau` requires; it runs `main.luau -a <surface>`, and
  splits stdout — one line per case: the case name, a space, the answer's
  bytes in hex, or the word `absent` — into one file per case in `<outdir>`
  (`<case>.absent` for the absent word). While `main.luau` is absent the leg
  answers ABSENT: `list` prints nothing and every surface exits 2.
- **The wire-fuzz pipe** (test/conformance/README.md, one process on a pipe,
  little-endian frames, a reply flushed per mutant) cannot be spoken by a
  Luau program at all. Its host shim is a small Go program of the
  fuzz-differential card (`go run`, the one process the harness talks to): it
  reads the roster and answers the codec bytes, then for each mutant writes
  the mutant into `inputs.luau` and runs the pinned interpreter on
  `wirefuzz.luau`, which prints the reply's fields as one hex line the shim
  frames and flushes. Measured: the interpreter starts in about 4 ms (100
  runs of a one-line script in 0.43 s on hulk), so a 10,000-mutant run costs
  about a minute of process starts. If that is too slow, the measured upgrade
  is ONE long-lived `luau` REPL process fed one `M("\x..")` line per mutant
  on its stdin (the REPL reads stdin; a script cannot), decided by that card
  with the timing in hand.

## THE REGISTER

The `luau` column of docs/PORTING.md is filled, every cell with its reason:

- **✅ carried now:** M13 (descriptors are constants: frozen at registration,
  asserted by `TestLuauTableDescriptorsLoad`), I4 (per-case absent, in the
  driver's line grammar), I10 (the zero-cost gate,
  `TestLuauTableFreeUnitAddsNothing`).
- **— the platform cannot:** M3 (a number is an unboxed double, so a float
  crossing a call costs nothing), M4 (no inlining directive or aliasing
  qualifier; M4b is the shape), M10 (the allocator is the host's `lua_Alloc`),
  M11 (a Luau table has no layout to assert).
- **❌ not yet:** every other technique, each naming the card of THE CARD MAP
  that carries it, or saying it is not yet carded. Each cites #381, the
  request that named the Lua family among the next languages: no Luau
  carry-across issue exists yet, and filing one is a public act this card does
  not take. When it is filed, its number replaces #381 in the column.

## THE CARD MAP

The stream schema-luau has eleven pro cards after this one; each fills one
surface in the files THE RUNTIME LAYOUT names, and none edits `luau.go` or
`luautable.go`.

| card | surface | its files |
|---|---|---|
| schema-luau-type-wire-read | the type wire's read path against the C++-pinned goldens (M2, the bit reader) | `luau/read.go`, `luau/runtime/bitreader.luau` |
| schema-luau-type-wire-write | the type wire's write path, byte for byte | `luau/write.go`, `luau/runtime/bitwriter.luau` |
| schema-luau-table-wire-load | the variable table wire's tolerant load and its report (M1, M5, M12, M18, M20, M21, I7) | `luautable/runtime/load.luau` |
| schema-luau-table-wire-save | the variable wire's save, every instance byte for byte (M6, M15, M16, M19) | `luautable/runtime/save.luau` |
| schema-luau-fixed-form | the fixed form, form byte 3, against the byte oracle (M22) | `luautable/fixedform.go`, `luautable/runtime/fixed.luau` |
| schema-luau-message-form | the message form, form byte 2, against its goldens | `luautable/messageform.go`, `luautable/runtime/message.luau` |
| schema-luau-json-text-form | the JSON text form both ways (M9, M14, I13) | `luautable/runtime/json.luau` |
| schema-luau-base64-text | base64 text over a text channel | `luautable/runtime/text.luau` |
| schema-luau-refusals-by-name | refusals at compile time and read time; the claimed names (I6) | `luautable/runtime/refusal.luau`, `internal/tablenames/luau.go`, `compiler/tablesluau_test.go` |
| schema-luau-conformance-leg | `main.luau`, `make conformance` and CI (M4b, I2, I11, J2) | `test/conformance/luau/main.luau`, `make/luau-conformance.mk`, the CI step keyed on ci.json's `luau` field |
| schema-luau-fuzz-differential | the wire fuzzer against the Go engine, and its host shim (I3, I15) | `test/conformance/luau/wirefuzz.luau`, `make/luau-fuzz.mk` |

Not yet carded, and named so in the register: the block and cook
accelerators (M7, M8, I5), the allocation instrument and its pin (I1, I14),
the soak (I9), the bench leg (I8, J5), the release tier (J3), the documented
surface (I12), accessor and descriptor agreement (J1), a node larger than a
slab (M17) and the formatter half of J4.

## DEEPEN

- **The pro move.** Treat Luau as an engine's scripting language, not a
  server language: the generated code is what a game script holds every
  frame, so the storage principle (every field exists from construction, no
  allocation per read) and the native `integer` matter more than brevity.
- **Raw material.** The pinned luau-lang 0.741 release, its `integer` RFC,
  the `buffer` library, the corpus under testdata/, and the JavaScript leg
  (`internal/codegen/js`, `jstable`) as the closest worked example.
- **Methods.** Generate into a temp dir and run under the pinned interpreter;
  `luau-analyze` on every generated module; byte goldens shared with every
  other leg; descriptor-driven runtime walks ported from the Go reference
  engine (`internal/tablewire`, `internal/tabletext`).
- **The outside judge.** The C++ reference's bytes and the harness's
  matrix, not this leg's own opinion of itself; and, for the Roblox question,
  a run on Roblox's own runtime, which this card could not reach.
