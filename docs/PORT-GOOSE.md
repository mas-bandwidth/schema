# The Goose port: the design every Goose card copies

Goose (github.com/aardappel/goose, Apache-2.0, Wouter van Oortmerssen) is a
memory-safe systems language with no heap: compiler-managed data stacks,
narrow relative references, built-in verified serialization, share-nothing
threads, the whole program compiled to one C file. Glenn Fiedler,
2026-10-19: "I'd like to front load the serialize and schema port to goose
because it has interesting properties for gamedev and is supposedly faster
than C++ and Rust. Let's put it to the test." The port is held to every
surface the other legs carry ("We do all features across all languages",
2026-10-18), and the oracle is the Go reference: every expected answer is
schema-go's, and docs/SPEC.md and docs/SPEC-TABLES.md are the standard as
written.

This page fixes the design. The skeleton it describes is in the tree:
`compiler/target_goose.go` (behind the `schema_leg_goose` build tag),
`compiler/carries_goose.go`, `internal/codegen/goose` (the type surface),
`internal/codegen/goosetable` (the table surface),
`internal/codegen/goose/gooserun` (the test helper), `make/goose.mk`,
`test/conformance/goose/driver` and `bench/tables/goose/leg`. The skeleton
has no codec: the type and table surfaces are values, constructors and
descriptors, and every wire, form and text surface is a later card (THE CARD
MAP).

**The pin.** Goose v2026.2 (tag v2026.2, commit 7ad96d6d47f9), `make dist-goose`: the release `goose-v2026.2-linux-x64.tar.gz`, sha256
`f9f0b448800b27b9d86fcd8341a08e8cda790ff86cd500c90e287fa1a616197c`, checked
before anything is unpacked (any other is refused by name) and untarred into
`dist/` — `dist/goose-v2026.2-linux-x64/goose`, its `stdlib/` and `tcclib/`
beside it. `$GOOSE` overrides the compiler; `make toolchain-goose` refuses a
pin that does not resolve, by name (the toolchain gate, issue #599).

## THE TYPE MAPPING

Goose's value model is C's — packed structs, inline fixed arrays, no
pointers into a heap — so the storage is the Go and C targets' field for
field. Every type is a Goose `struct` with EVERY field present from
construction (the storage principle, SPEC §6.1), each named by
`ir.GoExportName` — the Go, C# and JavaScript spelling, so the checker's
collision claims already cover Goose. A field's declared default lives in
the struct declaration (`Retries: i32 = -1`), so the struct literal
`T { .. }` builds the value at its defaults (SPEC §5): a specified default
where one is declared, the type's own default otherwise — zero, a nested
type at ITS defaults (every element of a fixed array of them too), an
empty slice, null. `New<Name>()` returns that literal. Measured on the pin
(hulk, 2026-10-19): `struct Q { ps: P[2], p: P }` with `P { a: i32 = -1 }`
constructs both `ps` elements and `p` at `a = -1`.

Every module of a unit declares into ONE namespace, the unit's package —
the Go target's one package per unit — so a cross-file reference is a bare
name and an import of the declaring module (`import .Wire;`). A name the
unit would declare twice in that namespace (an enum's `EnumName<E>`
beside a type spelled the same, a descriptor name a type already holds)
is refused by name, as is a package that is no Goose identifier, a Goose
keyword, a scalar type name, `std`, `main` or the runtime's namespace
`schema_rt` (`TestGooseRefusesByName`).

### Packet types (SPEC §4.3, §4.7, §4.8, §4.12, §4.13)

| schema | Goose value | construction |
|---|---|---|
| `bool` | `bool` | `false`, or the default |
| `int8` … `int64`, `uint8` … `uint64`, ranged or bare, `timestamp` | `i8` … `i64`, `u8` … `u64` at the declared width (THE NUMBER MODEL) | `0`, or the default |
| `int128` (ranged), `uint128` | `schema_rt::Int128` = `{ lo: u64, hi: u64 }`, the low lane first, as the wire orders it | the default's two's-complement lanes |
| `bits(N)` | `u32` for N ≤ 32, `u64` above (the Go target's storage) | `0` |
| `fixed(I, F)`, `ufixed(I, F)` | the RAW scaled integer in the narrowest of `i8/16/32/64` (`u…` for ufixed) holding I+F bits; `Int128` past 64 | the default's raw value |
| `float32` | `f32` | the default rounded as Go's `float32(v)` (FLOAT32) |
| `float32 \| min, max, resolution` | `f32` — the attributes describe the wire, not the storage | as `float32` |
| `float64` | `f64` | `0.0`, or the default |
| an enum `E` | `type E = u8/u16/…;` sized to the declared max; typed constants `ENone = 0`, `E<Variant>` from 1, `EMax: E` (no `Count`: SPEC §4.2, `Max` is an enum's only exported number); `EnumNameE(v) -> const u8[:]` | the default variant's constant, else `ENone` |
| a `flags` set `F` | `type F = u64;` (uint64 storage in every target, SPEC §4.2); masks `F<Variant>`, `FCount`, `FlagNameF(bit)` | `0`, or the default mask |
| a type `T` | `T`, a nested struct, inline | `T { .. }` |
| a union `U` (§4.8) | `struct U { Type: UType, <Arm>: <arm value>... }`; `type UType = u8;` with `UTypeNone`, arms from 1, `UTypeMax`, `EnumNameUType` | `Type = 0`, every payload arm pre-allocated; a payload-free arm has no slot |
| `string(N)`, `bytes(N)` | `u8[N]` and `<Name>Length: i32` beside it (SPEC §4.7) | the default's bytes in place, length set |
| `wstring(N)` (§4.12) | `u16[N]` of UTF-16 code units and `<Name>Length: i32` | the default's code units |
| `[N]T`, `[E]T` (enum-keyed: key k at index k) | `T[N]` inline | each element at its default |
| `[..N]T`, `[A..N]T` | `T[N]` and `<Name>Count: i32` beside it | the count born at A (SPEC §4.6) |
| `const` | a typed global `let` at its storage, folded to its literal, the schema spelling kept in a trailing comment | — |
| `reserved`, `align` | no storage | — |
| `if` / `else` branches (§4.4) | both sides' fields are stored; a read zeroes the untaken side (SPEC §5) | both at their defaults |

**Why not Goose's own types.** A union is NOT a Goose `enum` (ADT): an ADT
value is one variant's payload, while a schema union keeps every arm's
storage, unselected arms holding what they last held — the reused-storage
discipline every target shares. A schema enum is NOT a Goose enum either:
the wire carries any value up to the declared max, headroom values included,
which only an integer holds. And `string(N)` is `u8[N]` plus a length rather
than Goose's limited array `u8[..N]`: the length's type and the bytes past it
are the Go target's, so the codec ports field for field.

### Tables (docs/SPEC-TABLES.md §2), for `table` and `fixed table` alike

A table is the same value shape as a type: one class of storage serves both
the fixed form and the variable form, so the mapping above holds field for
field and the rows below are what tables add. Goose has no heap, so a
record holds no owned dynamic data: a list, a blob or a pointed-to record is
a SLICE or a nullable REFERENCE into storage the load (or the builder) owns
— a data-stack array whose scope bounds the record's, which the compiler
proves (no reference outlives its root). Records stay fixed-size, so arrays
of them index.

| schema (§2) | Goose value | construction |
|---|---|---|
| a table nested by value | its struct, in its `<Base>Table` module | `T { .. }` |
| `*T` (§2.1) | `T?` — null is null | `null` |
| `*bytes`, `*string` (§2.5) | `const u8[:]?` — a view of the blob's bytes; null is absent | `null` |
| a bare `string` element or map key (§2.13.1) | `const u8[:]?` | `null` |
| `?T` (§2.3) | the value, and `<Name>Present: bool` beside it | the value at its zero, `Present = false` |
| `[]T` (§2.9) and text lists (§2.13) | a slice `T[:]` | empty |
| `[..N]*T`, `[N]*T` | `T?[N]`, null-filled, and the count where counted | nulls |
| `map[K]V` (§2.8) | `<Entry>[:]`, a slice of the generated `<Table><Field>Entry` `{ Key, Value }` struct, which the save walk sorts by key | empty |
| a union with table arms (§2.6) | as a packet union, in the table module | `Type = 0` |
| `\| required`, `\| deprecated`, `was`, `json = "…"`, `forms`, `integrity` | no storage: descriptor facts and generation facts | — |

**The descriptor.** Beside every table's struct and constructor,
`<Name>Descriptor` is the table AS DATA (docs/SPEC-TABLES.md §8.1): a global
`let` of `schema_rt::TableDesc` — its node type id (the FNV-1a 64 of its
wire name, §3.1), whether it is a table and its class (`fixed`, `variable`),
what a pointer walk from it may place, and `fields`, one
`schema_rt::FieldDesc` per field: wire id, name, Goose slot, text key,
ordinal, header kind (§3), its SHAPE (`ShapeBool … ShapeVoid` in
`schema_tables.goose`) and what the shape needs — an element descriptor
(`elem`), the array kind and bounds, an enum's variant ids, the integer's
kind width, signedness and clamp range (as Int128 lanes, one shape for every
domain), a fixed field's (I, F), a compressed float's range, and the
default. Unions get `<Name>Descriptor` of `schema_rt::UnionDesc` with their
arms; a packet type a table reaches by value gets a descriptor too, because
it rides as a table body. The helper globals a descriptor names
(`_<Name>Fields`, `_<Name>_e<o>`, `_<Enum>Variants`) start with an
underscore, which no schema name does.

**How a walk reaches a field.** Goose has no reflection, no `offsetof` and
no function pointers: function values are compile-time entities passed as
generic parameters, and everything is monomorphized. So the ONE runtime walk
per form is a generic function in that form's runtime file, specialized per
table at compile time: it reads the descriptor for every decision (kinds,
bounds, defaults, ids, keys) and reaches the record's storage through a
per-table accessor the form's card emits (a `match` on the field ordinal),
the only per-table code a form writes.

## THE NUMBER MODEL

**The rule: Goose's own `i8 … i64` and `u8 … u64` hold every value of 64
bits or fewer, at the declared width; a 128-bit value is
`schema_rt::Int128 { lo, hi }`, two u64 lanes; every shift by a computed
count is guarded for 0 and 64, because Goose MASKS the count; float32 and
float64 reach their bit patterns by the one route serialize-goose's
DESIGN.md fixes (FLOAT32), never by arithmetic.**

Measured on the pin (hetzner2 and hulk, 2026-10-19):

- Arithmetic is at the operands' own width with no promotion: `u32 + i32`,
  and `u64` with any signed operand, is a compile error asking for a cast.
  Narrowing needs `as` (range-checked under `--debug`) or `as!` (never
  checked); the generated code uses `as!` only where the value is known to
  fit and says why.
- Unsigned arithmetic wraps in every build. Signed overflow aborts under
  `--debug` and wraps otherwise, so the codec never relies on signed
  overflow: two's-complement work is done on the unsigned type.
- `>>` is logical on unsigned and arithmetic on signed.
- A shift count is masked to the width: `(1 as u64) << 64` is `1` and
  `x >> 64` is `x` (measured: `print(one << n)` with `n: u64 = 64` printed
  `1`), where Go gives 0. Every mask of N bits built as `(1 << N) - 1` and
  every word-boundary split of the bit packer guards N = 64 (and N = 0 for a
  right shift by `64 - N`) explicitly.
- There is no 128-bit integer: `Int128` is two lanes and its arithmetic is
  lane arithmetic in the runtime, exactly the Go reference's
  `serialize.Int128` pair.
- `popcount`, `clz` and `ctz` are in std.
- Integer literals adapt to the destination: decimal reaches the full u64
  range (`18446744073709551615` is a `u64`); int64's minimum is spelled
  `(-9223372036854775807 - 1)`, its magnitude being no i64.

## BYTE BUFFERS

Bytes are Goose's array family: a codec WRITES into a `u8[>..]` growable
builder (grow-only, a pointer bump, references into it never invalidated),
and READS a `const u8[:]` slice — a view of the caller's bytes, nothing
copied, every index checked (and the checks the compiler can prove away,
proved away; `--bce-lines` reports the survivors). A loaded record's text
and blob fields are views into the input where the wire's bytes are the
value's bytes, so a load copies only what it must. Files and stdin reach a
program through the standard library's `os` module (`read_file`,
`write_file`, `read_stdin`, `args`), which needs no import beyond
`import os;`. Byte order is assembled with shifts, never through a
host-order view, so no read consults the host (PORTING M12).

An `extern fn` calls C directly, and slices cross as `{ data, len }`, so the
C serialize library (serialize.c) can link into one Goose program where a
card measures that it should; the port's own codec is Goose.

## FLOAT32

**Storage and construction.** A `float32` field is an `f32`. A default is
written as the shortest decimal of `float32(v)` held as a double — the value
Go's `float32(v)` computes, round to nearest, ties to even — so a double
parse then a float conversion, or a direct float parse, both land on exactly
`float32(v)` and no double rounding can move it (`goose.FormatFloat32`;
pinned by `TestGooseEveryScalarFieldIsBornAtItsIRDefault`, whose expected
values are computed from the IR as `float32(DefFloat)`). Non-finite defaults
are the constant divisions Goose folds (`(1.0 / 0.0)`, `(0.0 / 0.0)`).

**Writes round as the Go writer does.** A write handed a double that is not
exactly a float32 converts it the way the Go reference writer does,
`float32(v)` — Goose's `as f32` is the C conversion, IEEE round to nearest
even — and no write refuses a double for having more precision (the
coordinator's ruling from the Lua design read, 2026-10-17, held for every
port).

**Bits.** Goose has NO float-to-bits cast. Two routes were measured on the
pin, both under `goose run` (TinyCC) and `goose build -O2` (gcc 13.3):

1. a C sidecar — a `<root>.build` naming `c_sources = ["…c"];` and two
   `extern fn`s whose C is a `memcpy` each way. It moves f32 and f64 bits
   BOTH ways, and preserves a signalling NaN's payload (`0x7f800001` round
   trips);
2. `bytes_of` over a one-element `f32[1]` array: a read-only view of the
   bits, so it gives float-to-bits only; bits-to-float has no pure-Goose
   route.

The port uses ONE route, the one serialize-goose's DESIGN.md fixes, vendored
by schema-goose-serialize-vendor into `internal/codegen/goose/runtime/` — by
the measurements above, the C sidecar, the only route that runs both
directions. A float32 read from the wire never goes through a conversion
(SPEC §4.3, PORTING M21): its bits move into the `f32` through the route, so
a signalling NaN and its payload survive a round trip.

## THE RUNTIME LAYOUT

Fixed now for the pro cards; never renamed.

| file | what it is | who writes it |
|---|---|---|
| `compiler/target_goose.go` | the target: the shared refusal chain (THE CARRIES PATTERN), then the type surface, then the table surface, behind the `schema_leg_goose` tag until the packet surface has claims | this card; the tag is dropped when the port joins the builtins |
| `compiler/carries_goose.go` | the carried constructs, registered ONCE in its `init()` from the package-level slice `gooseCarried` | this card only; a later card adds `var _ = carryGoose(<construct>)` in its own `compiler/carries_goose_<surface>.go` (tag `schema_leg_goose`): package-level variables initialise before any `init()` |
| `internal/codegen/goose/goose.go` | the TYPE emitter (`Gen`, `Slots`, `TypeRef`, the literal renderers) and `Register`, the hook a later file joins from its own `init()` | this card only |
| `internal/codegen/goose/read.go`, `write.go` | the type wire, joined through `goose.Register` | type-wire-read, type-wire-write |
| `internal/codegen/goose/runtime/*.goose` | the type-wire runtime, `go:embed` by glob, emitted beside the modules: `schema_rt.goose` now (the `Int128` lanes), `serialize*.goose` and the float sidecar later | one file per card |
| `internal/codegen/goosetable/goosetable.go` | the TABLE emitter: structs, constructors and one DESCRIPTOR per table as data, and `goosetable.Register` | this card only |
| `internal/codegen/goosetable/<form>.go` | a form's emitted code (the per-table accessor), joined through `goosetable.Register` | fixedform.go, messageform.go, … — one per card |
| `internal/codegen/goosetable/runtime/*.goose` | the table runtime by glob, one file per card: `schema_tables.goose` now (the descriptor types); `load.goose`, `save.goose`, `fixed.goose`, `message.goose`, … later | one file per card |
| `internal/codegen/goosetable/runtime/registry/<construct>.goosereg` | THE REGISTRY's fragments: a card's dispatch registrations (an enum variant, a table row), assembled onto the end of `schema_tables.goose` when the target emits it (`goosetable/registry.go`) | one file per card; none edits another's, and none edits a registry's enum or array in a `.goose` file |
| `internal/codegen/goose/gooserun` | THE HARNESS's helper | this card |
| `test/conformance/goose/driver`, `ci.json`, `main.goose` | the conformance leg; `main.goose` lists a surface only once a card carries it | this card (driver, ci.json); the conformance driver (`main.goose`); each surface's card adds its arm |
| `make/goose.mk` | the leg's build: `dist-goose`, `toolchain-goose`, `test-goose`, `build/conformance-goose` | this card; later cards add their own `make/goose-*.mk` |
| `bench/tables/goose/leg` | the tables-bench leg, ABSENT until a codec exists | schema-goose-bench |

**The registry.** Goose has no function values in data, and an enum or an
array literal is ONE declaration, so a dispatch table every construct card
extends (a `Loader` variant and its `load_registry` row per kind, the same for
the save walk) would be one shared list that fourteen cards edit, and their
landings would conflict row against row. So a registry is never written as a
literal in a `.goose` file. Each card writes its own fragment,
`goosetable/runtime/registry/<construct>.goosereg`, and `goosetable.Runtime`
assembles every fragment onto the end of `schema_tables.goose` (namespace
`schema_rt`, so every walker that imports `.schema_tables` sees it). A
fragment is lines (blank and `//` lines ignored): `declare enum <Name>`,
`declare array <name> <ElemType>`, and `add <registry> <rank> <entry>`, an
entry being one variant identifier or one element's Goose text, with an
optional trailing `//` comment. A registry is declared once, by the card that
owns its walker (table-wire-load declares `Loader` and `load_registry`;
table-wire-save its own), and takes entries from any fragment. Entries are
ordered by rank, then fragment name, then line, so a first-match table states
its precedence by rank and never by landing order: a narrower row takes a
lower rank than the wider row it shadows. The element type, the enum's case
functions and the walker stay in the owning card's runtime file. Strict on
write, each refused by file and line: an unknown directive, a registry
declared twice or left without an entry, an entry for an undeclared registry,
a non-integer rank, a repeated or non-identifier variant. No fragment, no
change: `schema_tables.goose` is then emitted byte for byte as written
(`TestGooseRegistry*` in `goosetable/registry_test.go`).

**Modules.** One module per schema file, `<Base>.goose` (the type surface),
and `<Base>Table.goose` for a file with tables or with a packet type a table
reaches. Every module opens `namespace <package>;` and imports what it
names: `import .schema_rt;` (and `import .schema_tables;`), and `import .<Other>;` for each module it references. The leading dot resolves the
import RELATIVE TO THE IMPORTING FILE (§11.1 of the pinned v2026.2 release's
goose_spec.md, which is not a page of this tree), so the runtime
is emitted beside the modules and nothing is searched for: a generated
module never spells an undotted import (which resolves against the root
file's directory, then declared packages, then the standard library) and
never `..`. Goose loads each file once and allows import cycles, so no
module order is refused. The runtime declares into its own namespace,
`schema_rt`, so no schema name can collide with it. A schema file whose
module would collide with a runtime module, folding case as Windows and
macOS do, is refused by name, as is a schema file `X` beside a file
`XTable`.

**A program's root.** Goose compiles whole programs from one root file. A
generated unit is a set of modules a root imports; the root is the user's
(a game, a tool, a test), and when a later card adds C (the float sidecar),
the root's `<root>.build` names it — `goose run` builds it into a library
the in-process run loads, `goose build` links it.

## THE CARRIES PATTERN

The skeleton target refuses every construct it does not carry yet, BY NAME,
as the Luau port did at its skeleton. It does that through the shared
refusal chain every port calls, never through refusals of its own, so a
Goose refusal reads the same as every other port's and names the carriers
as the way through. Nothing is refused by editing a list in Goose's files,
and nothing is carried by deleting a refusal.

**The set starts empty.** `compiler/carries_goose.go` registers the goose
carries set ONCE, from `gooseCarried`, which the skeleton leaves empty: no
construct keyed in `compiler/carries.go` (timestamp, required, forms,
integrity, unbounded text, text lists and their total, the array root, the
one-sided range, the optional variable closure, the optional map) is carried,
so the driver (`compiler/generate.go`) refuses each by name.

**The chain.** `compiler/target_goose.go`'s `Generate` calls, in the order
`schema new-leg` writes them: `ir.RefuseWideTableKinds` (fixed-point and
128-bit table kinds), `refuseWideText` (`wstring(N)`), `refuseUnported`
(string, bytes and flags defaults; was-rows; a union in a table closure and
its payload-free arm; an array of unions; `*bytes`; `*string`),
`refuseOptionalArrays`, `refuseMaps` and `refuseLists`. Each refusal reads its
construct's carrier registry, so each stands down by itself when goose joins
that registry. It does not call `refuseTables`: the table surface
(descriptors and constructors) is emitted now, and the codecs fill it.

**What the skeleton carries.** The type surface, constructed at its defaults:
every packet field kind except `wstring(N)` and a string, bytes or flags
default (both refused above), enums, flags, unions (the
payload-free arm included, which the packet surface carries in every
target), and a `[A..B]` count born at `A`. Its claims sit in
`compiler/*_test.go` beside the other nine targets' (`bornAtMinimum`,
`guardComment`, `tagEnumSurface`, the payload-free arm's `carried`). The
packet wire has landed since (THE PACKET WIRE): goose left
`packetWriterOwed` and carries its write-side claims in `refusesEveryBuild`
and `tagRefusesEveryBuild`, and `compiler/carries_goose_packet.go` registers
wide text on the packet wire and the string, bytes and flags packet
defaults.

**How a later card carries a construct.** In its OWN new file
`compiler/carries_goose_<surface>.go`, tag `schema_leg_goose`, never by
editing `target_goose.go` or `carries_goose.go`:

- a construct keyed in `compiler/carries.go` is a package-level line,
  `var _ = carryGoose(carryForms)`. Package-level variables initialise
  before any `init()`, so it is in `gooseCarried` when `carries_goose.go`
  registers the set;
- a construct with a register function joins from the file's `init()`:
  `registerListCarrier("goose")`, `registerMapCarrier`,
  `registerOptionalArrayCarrier`, `registerWideTextCarrier`,
  `registerStringCarrier`, `registerPacketValueDefaultCarrier`;
- a construct whose registry has no register function (`tableArmTargets`,
  `unionArrayTargets`, `blobTargets`, `valueDefaultTargets`, `wasRowTargets`,
  `tableWideTextTargets`) is appended to from the same `init()`, and
  `registerBuiltin`'s flags in `target_goose.go` stay `false`.

The same card turns the matching cross-target gate from "goose refuses" to
"goose carries" (each gate's carrier list, e.g. `TestListCarriers`) and adds
the claim the gate wants; a gate that is not told fails naming the new
backend, which is how a carried construct is never silent.

## THE HARNESS

A Goose program reads files and writes stdout through the standard library
(`os`), and the compiler's own progress lines go to stderr. So the shape is:
**inputs go IN as files beside the program, results come OUT on stdout as
lowercase hex lines.**

- **Unit tests** use `gooserun.Run(t, dir, main, inputs)`: it writes each
  input as the file `dir/<name>`, runs `$GOOSE run <main>` — else
  `<repo root>/dist/goose-v2026.2-linux-x64/goose` — with `dir` as the
  working directory, and returns stdout. `goose run` compiles into the
  compiler's own process with the bundled TinyCC at -O1 (measured: a
  four-module program compiles and runs in about 70 ms on hulk).
  `gooserun.RunDebug` is `goose run --debug`, for the overflow and range
  checks. A compiler that does not resolve FAILS the test naming `make dist-goose` — it never skips. `gooserun.HexProbe` is the Goose `hex(b)`
  that prints bytes as one hex line, and `gooserun.HexLines` reads the lines
  back. Every test generates into a `t.TempDir()`, uses no real time, no
  socket and no network.
- **Anything timed** — the bench, a soak — builds with
  `$GOOSE build -O2 <file>.goose -o <exe>`: the bench machine's C compiler
  (gcc), never TinyCC.
- **The conformance driver** (`test/conformance/goose/driver`) runs
  `goose run main.goose -- <surface> <manifest>` (the program's arguments
  follow `--`; without it `goose run` takes them as more input files and
  refuses "multiple input files given"), passes the program's exit status
  through (2 is ABSENT), and splits stdout — one line per case: the case
  name, a space, the answer's bytes in hex, or the word `absent` — into one
  file per case in `<outdir>` (`<case>.absent` for the absent word). The
  leg's CI row (`ci.json`) installs the pin with `make dist-goose` before the
  harness runs. **`main.goose` answers no surface yet**, and `list` is empty:
  every harness surface reads a form the port does not carry (the id-table
  wire, the message form, JSON, the cook, the block form), and the one codec
  it does carry, the fixed form, is judged against the C++ reference's byte
  oracle by `make test-goose`, which builds `build/fixedform-corpus` and runs
  under `SCHEMA_REQUIRE_CORPUS=1`. A card that carries a surface adds it to
  `list` and answers it there; `compiler/carries_goose.go` claims no
  construct, so the two agree.
- **The wire-fuzz pipe** (one process, little-endian frames, a reply flushed
  per mutant) is a `goose build -O2` executable reading stdin with
  `read_stdin` and writing with `write_stdout` and `flush_stdout`: Goose
  speaks it directly, with no host shim (schema-goose-fuzz-differential).

## THE PACKET WIRE

The type surface's codec (`internal/codegen/goose/wire.go`, `write.go`,
`read.go`), bit-packed over serialize.goose v2.1.1, vendored verbatim into
`internal/codegen/goose/runtime/serialize*.goose` and held to
`runtime/SERIALIZE_PIN` (repository, tag, commit, one sha256 per file) by
`TestGooseVendoredSerializeIsThePin`. Per type and union of a module:

```goose
let <T>MaxBits: i64; let <T>MaxBytes: i64
fn Zero<T>(value: <T>&)                         // the §5 zero form, not the defaults
fn Write<T>(s: WriteStream&, value: <T>&) -> i32 // SERIALIZE_ERROR_NONE or the refusal kind
fn Read<T>(s: ReadStream&, value: <T>&) -> bool  // false: the reason is serialize_error(s)
```

Every bit count is folded at generation time from the declaration, as the
Go and C targets fold it. A plain bit field (bits, bool, a ranged or bare
integer, an enum, a flags set, a count, a length, a union tag) moves through
`schema_rt::wire_bits`, `wire_bits64` and `wire_bool`
(`runtime/schema_wire.goose`, schema's own, emitted beside the vendored
runtime): serialize.goose's `serialize_bits`, `serialize_bits64` and
`serialize_bool` with the same checks, refusals and stream state, declared
`inline fn` so that each call site's constant width folds the count checks,
the mask and the shifts, and the 8-byte word moves through a slice window
that carries no per-byte bounds check. Everything else moves through the
runtime's own entries (`serialize_bytes`, the fixed, int128, float and
compressed-float entries, a 64-bit write and a read past 56 bits). On the
packet-wire bench (`bench/run.sh --only goose`, intel, Core Ultra 9 285H,
pinned, four interleaved sittings, 2026-10-29) the inline moves took
bench_mixed from 0.89 to 1.84 M msg/s on write and from 0.34 to 0.94 M msg/s
on round trip, the wire byte-identical.

**Writes refuse in every build, and never abort.** serialize.goose aborts on
write misuse (its U9), and Goose's `assert` aborts in every build: the
language has no build-removable check. So the generated writer checks every
value the wire cannot carry BEFORE the runtime sees it and returns the kind,
the form Go makes native: `SERIALIZE_ERROR_VALUE_OUT_OF_RANGE` for a value
outside its range, a count or length past its bound, a mask bit above the
flags width, an interior null in wide text, an out-of-set union tag (which
writes nothing); `SERIALIZE_ERROR_NON_FINITE_FLOAT` for a NaN or an infinity
(SPEC §4.3). The claims are `refusesEveryBuild` and `tagRefusesEveryBuild` in
`compiler/countedbirth_test.go` and `compiler/uniontagwrite_test.go`.

**Reads refuse, never clamp,** latching the reason the C target latches:
`value_out_of_range` for a value in a range's bit headroom, a const or
reserved mismatch, a tag above the arm count or a broken surrogate pair;
`invalid_utf8` and `embedded_nul` for string content; the runtime's own
reasons (`past_end`, `non_finite_float`, `nonzero_padding`) otherwise. A
read zeroes the untaken side of a branch through `Zero<T>` (SPEC §5), and a
union selection builds its arm fresh at its declared defaults (SPEC §4.8).
A direct read overlays the received prefix of a string and keeps the tail.

**Storage crossing.** A 128-bit value's storage is `schema_rt::Int128`, the
runtime's is serialize.goose's global `Int128`/`Uint128`; the codec moves the
two lanes, `lo` and `hi`, by name. An unsigned fixed field rides the next
wider signed fixed entry with `I = width - F`, zero extended, which writes
the same bytes (the wire depends only on the raw span) and never reads a raw
value above the signed half as negative.

**The gates.** `TestGooseExamplesAgreeWithTheWireGoldens` and
`TestGooseLudicrousAgreesWithTheWireGoldens` (`make test-goose`) run
`test/goose/main.goose` and `test/goose-ludicrous/main.goose`, the Go legs'
golden tests in Goose, against `testdata/wire`, at `-O1` and under
`--debug`. `make/goose-packet.mk` holds the shared packet gates, each with
its registered control (group `goose-packet`): `packet-utf8-goose`,
`packet-wide-goose`, `packet-defaults-goose` and `packet-finite-goose`.

## THE REGISTER

The `goose` column of docs/PORTING.md is filled, every cell with its reason:

- **✅ carried now:** M13 (descriptors are global constants:
  `schema_tables.goose`, held by `TestGooseTableDescriptorsLoad`), I4
  (per-case absent, in the driver's line grammar), I10 (the zero-cost gate,
  `TestGooseTableFreeUnitAddsNothing`); and the gates over what the leg emits,
  make/goose-gates.mk: J4 (`tables-goose-strict`: every generated module and
  runtime module checked on its own by the pinned compiler, zero warnings),
  J2 (`tables-goose-runtime-home`), J3 (`tables-goose-release`) and I2 (the
  strict gate's and the runtime home's controls sabotage the emitter through
  `go build -overlay`).
- **— the platform cannot:** M3 (an `f32`/`f64` is a C scalar by value and
  nothing allocates), M10 (Goose has no allocator to hook: data stacks and
  scope exit).
- **❌ not yet:** every other technique, each naming the card of THE CARD
  MAP that carries it, or saying it is not yet carded. Each cites #2236,
  the Goose port's carry-across issue, which lists what parity with the nine
  main ports still needs, bottom layer first.

## THE CARD MAP

The stream schema-goose has these cards after this one; each fills one
surface in the files THE RUNTIME LAYOUT names, and none edits `goose.go`,
`goosetable.go` or `carries_goose.go`.

| card | surface | its files |
|---|---|---|
| schema-goose-serialize-vendor | serialize.goose vendored as the type wire's bottom layer, with the float-bits route | `goose/runtime/serialize*.goose`, `make/goose-serialize.mk` |
| schema-goose-type-wire-read | the type wire's read path against the C++-pinned goldens (M2) | `goose/read.go` |
| schema-goose-type-wire-write | the type wire's write path, byte for byte | `goose/write.go` |
| schema-goose-table-wire-load | the variable table wire's tolerant load and its report (M1, M12, M20, I7) | `goosetable/runtime/load.goose` |
| schema-goose-table-wire-save | the variable wire's save, every instance byte for byte (M15, M16) | `goosetable/runtime/save.goose` |
| schema-goose-vt-variable-class | pointers, the flat node table and unions (M6, M18) | `goosetable/variable_class.go`, its runtime file, `compiler/carries_goose_variable_class.go` |
| schema-goose-vt-collections | maps, blobs and unbounded arrays (M19) | `goosetable/collections.go`, its runtime file, `compiler/carries_goose_collections.go` |
| schema-goose-vt-optionals | optionals, enum-keyed arrays and `\| required` (M5) | `goosetable/optionals.go`, its runtime file, `compiler/carries_goose_optionals.go` |
| schema-goose-vt-text-lists | text lists, the total bound and unbounded text | `goosetable/text_lists.go`, its runtime file, `compiler/carries_goose_text_lists.go` |
| schema-goose-vt-wide-kinds | timestamps, one-sided ranges, enum and escape kinds, wstring | `goosetable/wide_kinds.go`, its runtime file, `compiler/carries_goose_wide_kinds.go` |
| schema-goose-vt-defaults-renames | defaults, renames, deprecation and the build version | `goosetable/defaults_renames.go`, its runtime file, `compiler/carries_goose_defaults_renames.go` |
| schema-goose-vt-retain-unknown | retain-unknown on load and save | `goosetable/retain_unknown.go`, its runtime file |
| schema-goose-vt-integrity-forms | the integrity trailer and the forms declaration | `goosetable/integrity_forms.go`, its runtime file, `compiler/carries_goose_integrity_forms.go` |
| schema-goose-vt-strict-and-refusals | the strict read and every refusal by name (I6) | `goosetable/strict_and_refusals.go`, its runtime file |
| schema-goose-vt-descriptors | reflection descriptors, doc comments, the unit registry and type views (J1) | `goosetable/descriptors.go`, its runtime file |
| schema-goose-fixed-form | the fixed form, form byte 3, against the byte oracle (M22) | `goosetable/fixedform.go`, `goosetable/runtime/fixed.goose` |
| schema-goose-fixed-form-evolution | fixed tables: versioning, widening and every refusal by Go's name | `goosetable/fixedevolve.go`, its runtime file |
| schema-goose-message-form | the message form, form byte 2, against its goldens | `goosetable/messageform.go`, `goosetable/runtime/message.goose` |
| schema-goose-block-form | the block form: builder and reflective read (M7, I5) | `goosetable/block_form.go`, its runtime file |
| schema-goose-cook | the cooked form: Cook, CookMeasure and Open (M8) | `goosetable/cook.go`, its runtime file |
| schema-goose-json-form | the JSON text form and base64 text (I13) | `goosetable/json_form.go`, its runtime file, `compiler/carries_goose_json_form.go` |
| schema-goose-text-form | the hand-editable text form (M9, M14) | `goosetable/text_form.go`, its runtime file |
| schema-goose-conformance-leg | `main.goose`, `make conformance` and the hostile corpus (M4b, I2, I11, J2) | `test/conformance/goose/main.goose`, `make/goose-conformance.mk` |
| schema-goose-fuzz-differential | the wire fuzzer against the Go engine (I3, I15) | `test/conformance/goose/wirefuzz.goose`, `make/goose-fuzz.mk` |
| schema-goose-ci-on-fleet | CI on the fleet runners (J3) | `.github/workflows/schema-goose.yml` |
| schema-goose-bench | the packet and table bench legs beside C++, Rust and Go (I8, J5) | `bench/goose/`, `bench/tables/goose/` |
| schema-goose-bench-report | Goose vs C++ vs Rust (and Lobster) on one machine | `docs/BENCH-GOOSE-LOBSTER.md` |
| schema-goose-roadmap-column | the ROADMAP column | `docs/roadmap.sexp`, `docs/roadmap-evidence/port-goose.sexp` |

Not yet carded, and named so in the register: the layout assertion (M11), a
node larger than a slab (M17), the allocation instrument and its pin (I1,
I14), the soak (I9) and the documented surface (I12).

## DEEPEN

- **The pro move.** Treat Goose as the game's systems language: a record is
  one contiguous block with no pointer in it, so the codec's job is to move
  bytes between a wire and a packed struct with as few branches as the
  shape allows — the claim under test is that this beats C++ and Rust.
- **Raw material.** The pinned v2026.2 release, its spec
  (`docs/goose_spec.md`: §3.3 the array family, §6.2 integer semantics,
  §7.10 `extern fn`, §11.1 modules), the corpus under testdata/, and the C
  leg (`internal/codegen/c`, `ctable`) as the closest worked example.
- **Methods.** Generate into a temp dir and compile under the pinned
  compiler; byte goldens shared with every other leg; descriptor-driven
  generic walks ported from the Go reference engine (`internal/tablewire`,
  `internal/tabletext`), monomorphized per table.
- **The outside judge.** The C++ reference's bytes and the harness's
  matrix, not this leg's own opinion of itself; and for speed, the bench
  beside C++, Rust and Go on one machine.
