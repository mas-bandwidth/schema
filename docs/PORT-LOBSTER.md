# The Lobster port: the design every Lobster card copies

Lobster (github.com/aardappel/lobster, Wouter van Oortmerssen) is a
game-oriented, statically typed language with Python-like syntax: flow typing,
compile-time reference counting, a JIT through TinyCC by default and an
ahead-of-time `--cpp` path that writes C++. Glenn Fiedler, 2026-10-19: "Please
add 'Lobster' as a language as well. For fun." It is held to every surface the
other legs carry (Glenn, 2026-10-18: "We do all features across all
languages"); the oracle is the Go reference (`sprint/schema-go`), and
docs/SPEC.md and docs/SPEC-TABLES.md are the standard as written. The layers
copy the Luau port's (docs/PORT-LUAU.md on `sprint/schema-luau`): a type
surface, a table surface of descriptors as data, a test helper, a driver that
answers ABSENT, a bench leg that answers ABSENT.

This page fixes the design. The skeleton it describes is in the tree:
`compiler/target_lobster.go` and `compiler/carries_lobster.go` (behind the
`schema_leg_lobster` build tag), `internal/codegen/lobster` (the type
surface), `internal/codegen/lobstertable` (the table surface),
`internal/codegen/lobster/lobsterrun` (the test helper), `make/lobster.mk`,
`test/conformance/lobster/driver` and `bench/tables/lobster/leg`. The skeleton
has no codec: the type and table surfaces are values, constructors and
descriptors, and every wire, form and text surface is a later card (THE CARD
MAP).

**The pin.** Lobster v2026.8 (tag v2026.8, commit 0921f7dd5745), `make dist-lobster`: the release `lobster_linux_release.zip`, sha256
`2f03fb96fdaf6935469c99e43ffb182cab0d797faec7b26eccc9a732df32ad31`, checked
before anything is unpacked (any other sha256 is refused by name), its `bin/`
and `modules/` unzipped into `dist/lobster-v2026.8/`. `$LOBSTER` overrides the
toolchain; `make toolchain-lobster` refuses a pin that does not resolve, by
name (the toolchain gate, issue #599). The fleet installs the same pin.

## THE TYPE MAPPING

One module per schema file, `<Base>.lobster`, and `<Base>Table.lobster` for a
file with tables (or with a packet type a table reaches). Every module of a
unit declares the same `namespace <package>`, so a consumer spells a
declaration `<package>.<Name>` and one module names another's declarations
bare. A value is a Lobster `class` (a mutable object, copied by reference)
with EVERY field present from construction (the storage principle, SPEC
§6.1), each named by `ir.GoExportName` — the Go, C# and JavaScript spelling,
so the checker's collision claims already cover Lobster. `New<Name>()` builds
a value at its defaults (SPEC §5): a specified default where one is declared,
the zero form otherwise, nested types and union arms pre-allocated, vectors
grown to their bound, and a `[A..B]` count born at `A`. Every module forward
declares its classes (`class <Name>`) so a class may name one defined below
it; enums and constants are emitted before every class, because Lobster checks
a type name where it is used.

### Packet types (SPEC §4.3, §4.7, §4.8, §4.12, §4.13)

| schema | Lobster value | construction |
|---|---|---|
| `bool` | `bool` (the predeclared enum) | `false`, or the default |
| `int8` … `int64`, `uint8` … `uint64`, `timestamp`, ranged or bare | `int` — the two's-complement bit pattern (THE NUMBER MODEL) | `0`, or the default; past int64 the hex bit pattern (`0xffffffffffffffff`) |
| `int128` (ranged), `uint128` | `schema_rt.Int128`, a struct `{ lo:int, hi:int }`, the low lane first, as the wire orders it | `schema_rt.Int128 { lo: …, hi: … }` |
| `bits(N)` | `int` | `0` |
| `fixed(I, F)`, `ufixed(I, F)` | the RAW scaled integer at its storage width: `int` to 64, `Int128` at 128 | the default's raw value |
| `float32` | `float` holding the float32 value exactly (f32 ⊂ f64) | the default rounded as Go's `float32(v)`, written as the shortest positional literal that reads back to that double (Lobster has no exponent literal) |
| `float32 \| min, max, resolution` | `float` — the attributes describe the wire | as `float32` |
| `float64` | `float` | `0.0`, or the default; ±inf as `(1.0 / 0.0)`, a NaN through `schema_rt.f64frombits` |
| an enum `E` | a Lobster `enum E` with `ENone = 0` and `E<Variant> = 1..` (the Go spelling); `ECount`, `EMax` and `EnumNameE(v)` beside it; a `\| max` headroom value is `E(v)` | the default variant, else `ENone` |
| a `flags` set `F` | `int` (uint64 storage in every target, SPEC §4.2); masks `F<Variant>`, `FCount`, `FlagNameF(bit)` | `0`, or the default mask |
| a type `T` | the class `T` | `NewT()` |
| a union `U` (§4.8) | a class `U` with `Type:UType` (an enum `UTypeNone`, arms from 1, `UTypeCount`, `UTypeMax`, `EnumNameUType`) and one slot per payload arm | `Type = UTypeNone`, every payload arm pre-allocated; a payload-free arm has no slot |
| `string(N)`, `bytes(N)` | a `string` of N bytes used as a buffer, and `<Name>Length:int` beside it (SPEC §4.7) | `schema_rt.zeros(N)`; a default is written in with `schema_rt.put` and its length set |
| `wstring(N)` (§4.12) | a `string` of 2N bytes holding UTF-16LE code units, and `<Name>Length` in code units | `schema_rt.zeros(2N)` |
| `[N]uint8` | a `string` of N bytes, 0-based — the bulk path's own type | `schema_rt.zeros(N)` |
| `[N]T`, `[E]T` (key k at index k), any other element | a vector `[T]` of exactly N elements, 0-based | `[]`, then N `push`es of the element's construction |
| `[..N]T`, `[A..N]T` | the same vector of N, and `<Name>Count:int` beside it | the count born at A (SPEC §4.6) |
| `[N]string(M)` | `[string]` of N buffers, `<Name>Length:[int]` beside it | N fresh buffers, N zeros |
| `const` | a top-level `let`: `int`, `float` or `Int128`, folded to its literal with the schema spelling as a trailing comment | — |
| `reserved`, `align` | no storage | — |
| `if` / `else` branches (§4.4) | both sides' fields are stored; a read zeroes the untaken side (SPEC §5) | both at their defaults |

### Tables (docs/SPEC-TABLES.md §2), for `table` and `fixed table` alike

A table is the same value shape as a type: one class of storage serves both
the fixed form and the variable form, so the mapping above holds field for
field and the rows below are what tables add. The declared class is a
descriptor fact (`fixed`), not a storage difference.

| schema (§2) | Lobster value | construction |
|---|---|---|
| a table nested by value | its class, in its `<Base>Table` module | `New<T>()` |
| `*T` (§2.1) | `T?` — nil is null | `nil` |
| `*bytes`, `*string` (§2.5) | `string?` — nil is absent; the string is exactly the blob's bytes | `nil` |
| a bare `string` element or map key (§2.13.1) | `string?` | `nil` |
| `?T` (§2.3) | the value, and `<Name>Present:bool` beside it | the value at its zero, `Present = false` |
| `[]T` (§2.9) and text lists (§2.13) | a vector `[T]`, its length the data's | `[]` |
| `[..N]*T`, `[N]*T` | `[T?]`, nil-filled, and the count where counted | N `nil`s |
| `map[K]V` (§2.8) | `[Entry]`, a vector of the generated `<Table><Field>Entry` class `{ Key, Value }`, which the save walk sorts by key | `[]` |
| a union with table arms (§2.6) | as a packet union, in the table module | `Type = UTypeNone` |
| `\| required`, `\| deprecated`, `was`, `json = "…"`, `forms`, `integrity` | no storage: descriptor facts and generation facts | — |

**The descriptor.** Beside every table's class and constructor,
`<Name>Descriptor` is the table AS DATA (docs/SPEC-TABLES.md §8.1): a
`schema_rt.TableDescriptor` holding its node type id (the FNV-1a 64 of its
wire name, §3.1), whether it is a table and its class (`fixed`, `variable`),
what a pointer walk from it may place, and one `schema_rt.FieldDescriptor`
per field — the field's wire id, name, Lobster slot, text key, ordinal,
header kind (§3), its SHAPE (`bool int float str text bytes enum ptr sblob bblob tab union arr map`) and what the shape needs: an element descriptor, the
array kind and bounds, an enum's variants and their ids, the integer's kind
width, signedness and domain (`int`, `int128`), its range as Int128 lanes, a
fixed field's (I, F), and the default (`def_bool`, `def_int`, `def_float`,
`def_str`). Unions get a `schema_rt.UnionDescriptor` with their arms; a packet
type a table reaches by value gets a descriptor too, because it rides as a
table body. Descriptors are registered by name when their module loads
(`schema_rt.lookup_table(name)`, `schema_rt.lookup_union(name)`), and every
module whose descriptor names another imports the module that registers it.

**Lobster has no reflection.** A program cannot read or write a class field
by its name, so the ONE runtime walk per form reads the descriptors for every
FACT (ids, kinds, bounds, defaults, the order) and reaches the record through
a small per-table accessor the form's card emits — one function per table
that hands the walk each slot in descriptor order, joined through
`lobstertable.Register` — never through per-field codec logic. That accessor
is the only per-table code a form emits; the walk itself is one runtime file.

## THE NUMBER MODEL

Measured on the pin (hetzner2 and hulk, 2026-10-19): Lobster has ONE integer
type, `int`, signed 64-bit; NO unsigned type and NO logical shift — `>>` is
arithmetic (`0x8000000000000000 >> 63` is `-1`) and the shift count is taken
to 6 bits (`1 << 64` is `1`); hex literals are bit patterns
(`0xFFFFFFFFFFFFFFFF` is `-1`) and `-9223372036854775808` is refused as an int
constant overflow; `float` is only f64; there is no 128-bit integer;
`popcount` exists.

**The rule.** Lobster's `int` (signed 64-bit) holds every value of 64 bits or
fewer, a u64 as its two's-complement bit pattern; every right shift of a word
is followed by its mask (no logical shift exists), every shift count is
guarded for 0 and 64, unsigned order is the sign-flipped compare, and a
128-bit value is two int lanes { lo, hi }; float32 rounding and bits go
through a byte string (write_float32_le/read_uint32_le).

The runtime spells each piece once (`schema_rt.shr`, `shl`, `ult`) and
`TestLobsterNumberModel` holds every helper to Go's own answer over the
boundary words and the counts 0, 1, 7, 63, 64 and 65, with the raw operators
as the control. Generated code spells a value past int64's range as its hex
bit pattern (`lobster.IntLit`), never in decimal.

## BYTE BUFFERS

Bytes are a Lobster `string` used as a buffer. `ensure_size("", n, 0)` makes a
fresh string of n zero bytes (the empty literal is a constant, so the result
is always new), and the `write_int8/16/32/64_le`, `write_float32/64_le` and
`write_substring` builtins write into it IN PLACE, visible through every
reference; the `read_uint8/16/32/64_le`, `read_int*_le` and
`read_float32/64_le` builtins read it. All of them are little-endian by
definition, so no read consults the host (PORTING M12). Two rules every codec
card keeps: a write returns the string, and the codec always stores what it
returns, because a write past the end grows the string and may move it; and a
string constant is copied before it is written, so storage is always made by
`schema_rt.zeros`, never a literal. Files go through `read_file` and
`write_file`; arguments through `command_line_arguments()`.

## FLOAT32

A `float32` field stores a double. When a write is handed a double that is
not exactly a float32, it converts the value THE WAY THE GO REFERENCE WRITER
DOES — `float32(v)`, round to nearest, ties to even — so the bytes are the
reference's, byte for byte; strict writing means canonical output, not
refusing ordinary rounding. `write_float32_le` is that conversion (the C++
`float` cast), and `schema_rt.fround` exposes it as `read_float32_le` of what
`write_float32_le` wrote; a finite double past float32's range becomes
±infinity. Construction writes each default already rounded.
`TestLobsterNumberModel` pins fround against Go on 0.1, both round-half-even
ties above 1.0, 3.5e38, −inf and −0, and pins a float32 default of 0.1 to
`float32(0.1)` while a float64 sibling keeps 0.1. **A float32 read from the
wire never goes through a conversion** (SPEC §4.3, PORTING M21): the read
path moves its 32 bits with `read_uint32_le` and builds the double by bit
surgery (`schema_rt.f32bits` and `f64frombits` are the pieces), so a
signalling NaN and its payload survive a round trip.

## THE RUNTIME LAYOUT

Fixed now for the pro cards; never renamed.

| file | what it is | who writes it |
|---|---|---|
| `compiler/target_lobster.go` | the target: the shared refusals of the constructs no codec has met, then the type surface, then the table surface, behind the `schema_leg_lobster` tag | this card; the tag is dropped when the port joins the builtins |
| `compiler/carries_lobster.go` | the carried-construct registration, ONCE in its `init()`, from the package-level slice `lobsterCarried` | this card only; a later card adds `var _ = carryLobster(<construct>)` in its own new file (package-level variables initialise before any `init()`) |
| `internal/codegen/lobster/lobster.go` | the TYPE emitter (`Gen`, `Slots`, `TypeRef`, the literal renderers) and `Register`, the hook a later file joins from its own `init()` | this card only |
| `internal/codegen/lobster/read.go`, `write.go` | the type wire, joined through `lobster.Register` | type-wire-read, type-wire-write |
| `internal/codegen/lobster/runtime/*.lobster` | the type-wire runtime, `go:embed` by glob, emitted beside the modules: `schema_runtime.lobster` now, `bitreader.lobster` and `bitwriter.lobster` later | one file per card |
| `internal/codegen/lobstertable/lobstertable.go` | the TABLE emitter: classes, constructors and one DESCRIPTOR per table as data, and `lobstertable.Register` | this card only |
| `internal/codegen/lobstertable/fixedform.go`, `messageform.go`, … | the forms that need emitted code (the per-table accessor), joined through `lobstertable.Register` | fixed-form, message-form |
| `internal/codegen/lobstertable/runtime/*.lobster` | the table runtime by glob, one file per card: `schema_tables.lobster` now; `load.lobster`, `save.lobster`, `fixed.lobster`, `message.lobster`, `json.lobster`, `text.lobster`, `refusal.lobster` later | one file per card |
| `internal/codegen/lobster/lobsterrun` | THE HARNESS's helper | this card |
| `test/conformance/lobster/driver`, `ci.json`, `main.lobster` | the conformance leg | this card (driver, ci.json); conformance-leg (`main.lobster`) |
| `make/lobster.mk` | the leg's build: `dist-lobster`, `toolchain-lobster`, `test-lobster`, `build/conformance-lobster` | this card; later cards add their own `make/lobster-*.mk` |
| `bench/tables/lobster/leg` | the tables-bench leg, ABSENT until a codec exists | schema-lobster-bench |

**How a module reaches its runtime.** The runtime files are emitted beside
the generated modules, and every import a generated module or runtime makes
is `import .<module>` — the leading dot resolves against the importing file's
OWN directory and never the `modules` directories or the toolchain's install
(the pin's language reference), so nothing outside the unit directory can
resolve, whatever the working directory. The runtime declares its names in
`namespace schema_rt`; a unit's package may not be `schema_rt` or a Lobster
keyword (refused by name, `lobster.CheckNames`), nor may a declaration be
named for a keyword. A schema file whose module would collide with a runtime
module, folding case as Windows and macOS do, is refused by name, as is a
schema file `X` beside a file `XTable`. Lobster merges an import at its first
occurrence and skips the second, so an import CYCLE would declare a type after
its first use: a cycle between modules is refused by name
(`lobster.ImportCycle`); a cross-file TABLE cycle never reaches it, because
the checker refuses that first (docs/SPEC-TABLES.md §11). One program holds
one copy of the runtime: two units generated into separate directories and
imported into one program would define `schema_rt` twice, so a program that
uses several units generates them into one directory.
`TestLobsterImportsResolveInsideTheUnitOnly` holds the import rule.

**What the target refuses today.** The leg has no wire codec, so the target
gives the shared, by-name refusals every non-carrier gives (a string, bytes or
flags default; a `was` row; a union in a table closure; an array of unions; a
byte blob; an optional array; a map; an unbounded list), and the central
refusals of `compiler.Generate` hold back every construct of
`compiler/carries.go` (lobster carries none yet). The descriptor surface
itself describes every manifest unit: `TestLobsterTableDescriptorsLoad` runs
`lobster.Generate` and `lobstertable.Generate` directly over all 33.

## THE LOAD

`runtime/load.lobster` (schema-lobster-table-wire-load) is the variable
wire's read half: a port of the Go reference engine (`internal/tablewire`
decode.go, decodenodes.go, nodetable.go, text.go, leb.go) onto the
descriptors, under `namespace schema_rt`. `schema_rt.load(root, data,
report)` reads a form-1 wire at the table named `root` and returns a
`Record`, or nil when the form byte is refused (docs/SPEC-TABLES.md §3, §4).
Every construct card reads and extends this section; none edits
`load.lobster`.

**The storage.** Lobster has no reflection, so a load never writes a
generated class: it fills the table AS DATA, mirroring Go's
`tabletext.Instance` cell for cell, and a form card's per-table accessor moves
a `Record` into the class.

| class | what it is | Go's |
|---|---|---|
| `Record { d, fields }` | one table or packet type: its descriptor and one `Slot` per field, descriptor order | `Instance` |
| `Slot { fd, present, count, cell, elems, entries }` | one field: its descriptor, the `?T` presence companion, the used extent (a counted array's count, a string's or bytes' used length), the value, the array slots (`len` is the bound; a list's is its length), the map entries (each a `Value` whose `tab` is a `{ key, value }` record, ascending) | `Field` |
| `Value { b, i, w, f, s, units, tab, arm, node, blob }` | one cell; only the member the shape names is read: `i` every integer of 64 bits or fewer as its bit pattern, an enum value, a union tag; `w` the raw `Int128` of a wide kind (18–29, fixed point as units × 2^F); `f` a float (a float32 as its exact double); `s` string or bytes at the used extent; `units` wstring code units; `tab` a by-value body, a body arm, an element record, a map entry; `arm` a non-body arm's `Slot`; `node` a pointee; `blob` a blob's bytes | `Cell` |

**Absent, present-empty and present.** A pointer that is null holds `node =
nil`, and a null `*bytes` or `*string` holds `blob = nil`; an EMPTY blob is
the non-nil `""`, because null and empty are two values (§2.5). A `?T` field
is absent while its `Slot.present` is false, whatever its cell holds, and
present from the moment it rides with its own kind (§2.3; an optional array
whose element kind is foreign stays absent). A union is None while `i` is 0,
and then `tab` and `arm` are nil. A field the wire never names keeps the
value `new_record` built: the generated constructor's default (§4).

**The flat node table.** `LoadState.nodes` is the numbering (§3.1): wire
index 0 is null, 1 the root (`LoadState.root`), and index `k + 2` names
`nodes[k]`, a `Node { kind, rec, blob }` — kind 1 a table record, 2 a
`*string` blob, 3 a `*bytes` blob, and 0 a record this root cannot place,
which keeps its index while every pointer naming it reads null. Pass one
fills it from the framing, pass two decodes each record's body, then the root
body; a pointer slot stores the resolved `Record` or blob, never the index.
`good` is false when the node table could not be read whole, and then every
pointer reads null. A second slot naming one blob counts `blob_shared`.

**The dispatch table.** A field, an array element and a union arm are read
by the `Shape` registered under their shape key — `scalar`, `enum`, `ptr`
(with `sblob` and `bblob`), `str` (with `text`), `wstr`, `tab`, `textlist`
(a plain text list's element), `arr`, `bytes`, `keyed`, `union`, `map`.
`Shape` has three methods: `field(r, slot)`, `element(r, slot, i, ek)` and
`arm(r, slot, n)`; the base reads a fixed-width scalar, the reference's own
fall-through. A construct card adds a shape, or replaces one, with ONE line
in its own runtime file:

```
class MyShape : schema_rt.Shape
    def field(r:schema_rt.Reader, sl:schema_rt.Slot) -> bool: ...
let _my = schema_rt.register_shape(MyShape { key: "my" })
```

The last registration of a key wins, and a shape reads its report through
`r.st.report`.

**The report.** `LoadReport` holds the six counters reports.txt names
(`unknown`, `kind_mismatch`, `widened`, `clamped`, `duplicate`, `malformed`),
`blob_shared`, the `reason` a named rule gives and the verdict `refused` (a
form byte this reader does not carry; a collection count past the int32 cap
on a variable root, which Go returns as `CountRefusal`).

**Not carried yet, by name.** The descriptor does not name an enum-keyed
array's KEY enum, so `schema_rt.keyed_keys(table, field, variants)` registers
it; until an emitter emits that call (a `lobstertable.Register` part, or a
`keyvs` member on the descriptor), a program registers it itself, as the
load test does. A DEPRECATED enum variant (§2.15) is not on `EnumVariant`, so
the load reads it as its value rather than the default; no manifest unit has
one. Retention (§6.6) and the region read are later cards' (retain-unknown).

**The test.** `TestLobsterTableLoadMatchesTheReports` loads every report and
instance row of the tables manifest, one program per unit, and holds each
row's report to reports.txt and to the Go engine's `tablewire.Decode` on the
same wire, and each loaded value to Go's through THE VALUE DUMP — every slot,
count, presence bit, element, entry, arm and node, floats by their bits and
a graph by first-visit labels. `TestLobsterTableLoadNegativeControl` patches
the unknown-field count out of the emitted runtime and holds the comparison
red.

## THE HARNESS

A Lobster program reads files (`read_file`) and arguments
(`command_line_arguments()`, after `--` on the command line) and prints to
stdout, so the shape is: **inputs go IN as files, results come OUT on stdout
as lines (lowercase hex for bytes).**

- **Unit tests** use `lobsterrun.Run(t, dir, main, inputs)`: it writes each
  input as a file of that name in `dir`, runs `dir/main` under `$LOBSTER`,
  else `<repo root>/dist/lobster-v2026.8/bin/lobster`, with `dir` as the
  working directory — `$LOBSTER <file>.lobster`, the default JIT through
  TinyCC — and returns stdout. A toolchain that does not resolve FAILS the
  test naming `make dist-lobster`; it never skips. `lobsterrun.HexProbe` is
  the Lobster `hex(b)` that prints a byte string as one hex line and
  `lobsterrun.HexLines` reads the lines back. The toolchain prints compiler
  warnings on stdout, and the tests compare the whole of stdout, so a warning
  in a generated module fails them. Every test generates into a
  `t.TempDir()`, uses no real time, no socket and no network.
- **Anything timed** (the bench leg, the allocation instrument) runs BOTH the
  default JIT and the ahead-of-time build: `lobster --cpp <file>` writes the
  program as C++, which is compiled with `clang -O2` against a checkout of
  aardappel/lobster at the pin's commit (its `dev/` tree, as the project's own
  `LOBSTER_TOCPP` build does), and the row names which build it measured.
- **The conformance driver** (`test/conformance/lobster/driver`) runs
  `main.lobster -- <manifest> <surface>` under the pinned toolchain and splits
  stdout — one line per case: the case name, a space, the answer's bytes in
  hex, or the word `absent` — into one file per case in `<outdir>`
  (`<case>.absent` for the absent word). While `main.lobster` is absent the
  leg answers ABSENT: `list` prints nothing and every surface exits 2.
  `make build/conformance-lobster` generates every manifest unit for lobster
  and records each refusal by name.
- **The wire-fuzz pipe** (test/conformance/README.md, one process on a pipe)
  is spoken by a Lobster program that reads its stdin; whether the pinned
  console build exposes a blocking stdin read is the fuzz-differential card's
  first measurement, and its fallback is a small Go host shim that writes each
  mutant as a file and runs the program once per mutant.

## THE REGISTER

The `lobster` column of docs/PORTING.md is filled, every cell with its
reason:

- **✅ carried now:** I4 (per-case absent, in the driver's line grammar) and
  I10 (the zero-cost gate, `TestLobsterTableFreeUnitAddsNothing`).
- **— the platform cannot:** M3 (a `float` is an unboxed 64-bit slot, so a
  float crossing a call allocates nothing), M4 (no inlining directive or
  aliasing qualifier; M4b is the shape), M10 (memory is the VM's compile-time
  reference counting over its own allocator), M11 (a Lobster class has no
  layout to assert).
- **❌ not yet:** every other technique, each naming the card of THE CARD MAP
  that carries it. M13 is among them: Lobster
  has no frozen object, so a descriptor is a `let` global that cannot be
  rebound but whose fields stay assignable. Each cites #381, the request for
  languages beyond the nine: no Lobster carry-across issue exists yet, and
  filing one is a public act this card does not take. When it is filed, its
  number replaces #381 in the column.

## THE CARD MAP

The stream schema-lobster holds these cards after this one, every id carded
(`nova-sprint view cards --stream schema-lobster`). Each fills one surface in
the files THE RUNTIME LAYOUT names, and none edits `lobster.go`,
`lobstertable.go` or `carries_lobster.go`. A technique marked * is assigned
to the card nearest its surface, but that card's brief does not yet name the
gate; the card adds it or the register cell moves.

| card | surface | its files |
|---|---|---|
| schema-lobster-serialize-vendor | serialize.lobster vendored verbatim as the type wire's bottom layer, pinned by SERIALIZE_PIN | `lobster/runtime/serialize*.lobster`, `lobster/runtime/SERIALIZE_PIN`, `make/lobster-serialize.mk` |
| schema-lobster-type-wire-read | the type wire's read and measure paths against the C++-pinned goldens, refusals as the Go reader refuses (M2) | `lobster/read.go` |
| schema-lobster-type-wire-write | the type wire's write path, byte for byte; takes lobster off `codecless` in compiler/countedbirth_test.go | `lobster/write.go` |
| schema-lobster-table-wire-load | the variable table wire's load and its report, and the per-kind hook every construct card joins (M1, M5, M12, M18, M20, M21, I7) | `lobstertable/runtime/load.lobster` |
| schema-lobster-table-wire-save | the variable wire's save, every instance byte for byte, through the same hook (M6, M15, M16, M19) | `lobstertable/runtime/save.lobster` |
| schema-lobster-fixed-form | the fixed form, form byte 3, against the byte oracle (M22) | `lobstertable/fixedform.go`, `lobstertable/runtime/fixed.lobster` |
| schema-lobster-fixed-form-evolution | the fixed form's versioning, widening and every refusal by Go's name | `lobstertable/fixedevolve.go`, `lobstertable/runtime/fixedevolve.lobster` |
| schema-lobster-message-form | the message form, form byte 2, against its goldens | `lobstertable/messageform.go`, `lobstertable/runtime/message.lobster` |
| schema-lobster-block-form | the block form's builder and reflective read, 19.1 and 19.2 (M7, I5) | `lobstertable/block_form.go`, `lobstertable/runtime/block_form.lobster` |
| schema-lobster-cook | the cooked form: Cook, CookMeasure and Open (M8) | `lobstertable/cook.go`, `lobstertable/runtime/cook.lobster` |
| schema-lobster-json-form | the JSON text form both ways and base64 text (M9, M14, I13) | `lobstertable/json_form.go`, `lobstertable/runtime/json_form.lobster`, `compiler/carries_lobster_json_form.go` |
| schema-lobster-text-form | the hand-editable text form, read, save and leaf edits (16.9) | `lobstertable/text_form.go`, `lobstertable/runtime/text_form.lobster` |
| schema-lobster-vt-variable-class | pointers, the flat node table and unions with Go's framing | `lobstertable/variable_class.go`, `lobstertable/runtime/variable_class.lobster` |
| schema-lobster-vt-collections | maps, blobs and unbounded arrays (M17*) | `lobstertable/collections.go`, `lobstertable/runtime/collections.lobster` |
| schema-lobster-vt-text-lists | text lists, the total bound and unbounded text | `lobstertable/text_lists.go`, `lobstertable/runtime/text_lists.lobster`, `compiler/carries_lobster_text_lists.go` |
| schema-lobster-vt-optionals | optionals, enum-keyed arrays and `\| required` | `lobstertable/optionals.go`, `lobstertable/runtime/optionals.lobster`, `compiler/carries_lobster_optionals.go` |
| schema-lobster-vt-wide-kinds | timestamps, one-sided ranges, the enum and escape kinds, wstring | `lobstertable/wide_kinds.go`, `lobstertable/runtime/wide_kinds.lobster`, `compiler/carries_lobster_wide_kinds.go` |
| schema-lobster-vt-defaults-renames | defaults, renames, deprecation and the build version | `lobstertable/defaults_renames.go`, `lobstertable/runtime/defaults_renames.lobster` |
| schema-lobster-vt-descriptors | reflection descriptors, doc comments, the unit registry and type views (M13*, J1*) | `lobstertable/descriptors.go`, `lobstertable/runtime/descriptors.lobster` |
| schema-lobster-vt-integrity-forms | the integrity trailer and the forms declaration | `lobstertable/integrity_forms.go`, `lobstertable/runtime/integrity_forms.lobster`, `compiler/carries_lobster_integrity_forms.go` |
| schema-lobster-vt-retain-unknown | retain-unknown on load and save | `lobstertable/retain_unknown.go`, `lobstertable/runtime/retain_unknown.lobster` |
| schema-lobster-vt-strict-and-refusals | the strict read and every refusal at compile time and read time by Go's name (I6) | `lobstertable/strict_and_refusals.go`, `lobstertable/runtime/strict_and_refusals.lobster` |
| schema-lobster-conformance-leg | `main.lobster`, `make conformance` and the CI matrix row (M4b, I2, I11, I12*, J2) | `test/conformance/lobster/main.lobster`, `make/lobster-conformance.mk`, the CI step keyed on ci.json's `lobster` field |
| schema-lobster-fuzz-differential | the wire fuzzer against the Go engine (I3, I15) | `test/conformance/lobster/wirefuzz.lobster`, `test/conformance/lobster/wirefuzz-driver`, `make/lobster-fuzz.mk` |
| schema-lobster-bench | the packet and table bench legs, JIT and `--cpp` (I8, J5, I1*, I9*, I14*) | `bench/lobster/`, `bench/tables/lobster/` |
| schema-lobster-ci-on-fleet | the schema-lobster workflow on the fleet runners (J3*, J4*) | `.github/workflows/schema-lobster.yml` |
| schema-lobster-roadmap-column | the lobster column of ROADMAP.md from recorded evidence | `docs/roadmap.sexp`, `docs/roadmap-evidence/port-lobster.sexp` |

Each carry-across construct of compiler/carries.go joins with its own
`var _ = carryLobster(...)` file when its card lands.
