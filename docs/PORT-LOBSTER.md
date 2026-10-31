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
`compiler/target_lobster.go` and `compiler/carries_lobster.go` (a builtin
target since the port came onto main, 2026-10-28: no build tag), `internal/codegen/lobster` (the type
surface), `internal/codegen/lobstertable` (the table surface),
`internal/codegen/lobster/lobsterrun` (the test helper), `make/lobster.mk`,
`test/conformance/lobster/driver` and `bench/tables/lobster/leg`. The skeleton
had no codec; every wire, form and text surface, and the bench legs, landed
as later cards (THE CARD MAP).

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
| `[N]uint8` | a `string` of N bytes, 0-based — the bulk path's own type | `schema_rt.zeros(N)` |
| `[N]T`, `[E]T` (key k at index k-1), any other element | a vector `[T]` of exactly N elements, 0-based | `[]`, then N `push`es of the element's construction |
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
| `[E]T` in a table or a type a table reaches (§2.4) | the same vector, and three functions in the table module that take the key (docs/PORTING.md M5): `<Owner><Field>Slot(key)` is the slot key names, refused by name for None, a negative key and any key past `EMax` in every build (`schema_rt.keyed_slot`, a VM bounds error that names the array and the key, which `--runtime-no-asserts` keeps); `<Owner><Field>At(v, key)` reads it; `<Owner><Field>Each(v) key, e:` yields every key beside its element | — |
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

## THE WRITE

`internal/codegen/lobster/write.go` (schema-lobster-type-wire-write) is the
type wire's write half: for every packet struct and packet union of a unit it
emits `Write<Name>(s:serialize_write_stream, v:<Name>) -> bool`, joined into
the emitter with `Register("write", writePart)` in its `init()`. A writer
rides the fields in declared order (SPEC §4.3) — const, reserved and align
keep their places — and makes every bit through the vendored serialize port's
methods (`serialize_bits`, `serialize_bits64`, `serialize_bool`,
`serialize_align`, `pack_bytes`, ...): there is no private bit writer, and a
refused value returns false before its bits ride. The module imports only the
serialize families its fields need (`serialize_stream` and
`serialize_bitpacker` always; `serialize_float`, `serialize_fixed` and
`serialize_int128` where a field of that kind rides); compressed floats and
byte buffers are written with the bit packer directly.

It rests on the vendored runtime (schema-lobster-serialize-vendor):
`runtime/serialize*.lobster` must be emitted beside the generated modules for
`import .serialize_stream` to resolve. `TestLobsterTypeWireWritesTheGoldens`
writes the 14 instances of `testdata/wire/*.bin` and compares bytes, naming
each golden; `TestLobsterTypeWireWriteNegativeControl` widens one
`serialize_bool` to two bits and holds the comparison red.

## THE RUNTIME LAYOUT

Fixed now for the pro cards; never renamed.

| file | what it is | who writes it |
|---|---|---|
| `compiler/target_lobster.go` | the target: the shared refusals of the constructs no codec has met, then the type surface, then the table surface; a builtin (the `schema_leg_lobster` tag was dropped when the port joined the builtins on main) | this card |
| `compiler/carries_lobster.go` | the carried-construct registration, ONCE in its `init()`, from the package-level slice `lobsterCarried` | this card only; a later card adds `var _ = carryLobster(<construct>)` in its own new file (package-level variables initialise before any `init()`) |
| `internal/codegen/lobster/lobster.go` | the TYPE emitter (`Gen`, `Slots`, `TypeRef`, the literal renderers) and `Register`, the hook a later file joins from its own `init()` | this card only |
| `internal/codegen/lobster/read.go`, `write.go` | the type wire, joined through `lobster.Register` | type-wire-read, type-wire-write |
| `internal/codegen/lobster/runtime/*.lobster` | the type-wire runtime, `go:embed` by glob, emitted beside the modules: `schema_runtime.lobster` now, the vendored `serialize*.lobster` beside it | one file per card |
| `internal/codegen/lobster/runtime/serialize*.lobster`, `internal/codegen/lobster/runtime/SERIALIZE_PIN` | the serialize.lobster port, copied VERBATIM from `https://github.com/mas-bandwidth/serialize.lobster` at the commit `SERIALIZE_PIN` names (the entry `serialize.lobster`, the streams `serialize_stream.lobster`, and the family modules), with one sha256 per file | schema-lobster-serialize-vendor; `make/lobster-serialize.mk` (`make vendor-serialize-lobster`) bumps it |
| `internal/codegen/lobstertable/lobstertable.go` | the TABLE emitter: classes, constructors and one DESCRIPTOR per table as data, and `lobstertable.Register` | this card only |
| `internal/codegen/lobstertable/fixedform.go`, `messageform.go`, … | the forms that need emitted code (the per-table accessor), joined through `lobstertable.Register` | fixed-form, message-form |
| `internal/codegen/lobstertable/runtime/*.lobster` | the table runtime by glob, one file per card: `schema_tables.lobster` now; `load.lobster`, `save.lobster`, `fixed.lobster`, `message.lobster`, `json.lobster`, `text.lobster`, `refusal.lobster` later | one file per card |
| `internal/codegen/lobster/lobsterrun` | THE HARNESS's helper | this card |
| `test/conformance/lobster/driver`, `ci.json`, `main.lobster` | the conformance leg | this card (driver, ci.json); conformance-leg (`main.lobster`) |
| `make/lobster.mk` | the leg's build: `dist-lobster`, `toolchain-lobster`, `test-lobster`, `build/conformance-lobster` | this card; later cards add their own `make/lobster-*.mk` |
| `bench/lobster/run`, `main.lobster`, `harness.lobster`, `route.sh` | the type-bench leg (BenchMixed, family gen) and the harness and output routing both legs share | schema-lobster-bench |
| `bench/tables/lobster/leg`, `table_main.lobster` | the tables-bench leg (TableMixed on the tolerant wire, family table) | schema-lobster-bench |
| `make/lobster-bench.mk` | `bench-lobster-gate` (both legs' gates, no clock) and its negative control | schema-lobster-bench |

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

**What the target refuses today.** The table runtime's load and save walk
every construct the shared refusals name (a string, bytes or flags default in
a table closure, a `was` row, a union in a table closure, an array of unions, a
byte blob, an optional array, a map, an unbounded list), so the target applies
none of them, and every unit of the tables manifest generates
(`TestLobsterGeneratesEveryManifestUnit`, whose witness is that the shared
refusals still refuse units of that manifest). A PACKET type's string, bytes
or flags default is carried too: the type wire reads and writes it over serialize.lobster v2.1.1 (`write.go`, `read.go`;
the packet gates are `internal/codegen/lobster/packet_*_test.go`). The constructs of `compiler/carries.go` are held back
centrally unless a `carries_lobster*.go` file registers them; lobster carries
the timestamp, the one-sided range, text lists, their total, unbounded text
and `?T` over a variable closure.

## THE LOAD

`runtime/load.lobster` (schema-lobster-table-wire-load) is the variable
wire's read half: a port of the Go reference engine (`internal/tablewire`
decode.go, decodenodes.go, nodetable.go, text.go, leb.go) onto the
descriptors, under `namespace schema_rt`. `schema_rt.load(root, data, report)` reads a form-1 wire at the table named `root` and returns a
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
| `Value { b, i, w, f, s, tab, arm, node, blob }` | one cell; only the member the shape names is read: `i` every integer of 64 bits or fewer as its bit pattern, an enum value, a union tag; `w` the raw `Int128` of a wide kind (18–29, fixed point as units × 2^F); `f` a float (a float32 as its exact double); `s` string or bytes at the used extent; `tab` a by-value body, a body arm, an element record, a map entry; `arm` a non-body arm's `Slot`; `node` a pointee; `blob` a blob's bytes | `Cell` |

**Absent, present-empty and present.** A pointer that is null holds `node = nil`, and a null `*bytes` or `*string` holds `blob = nil`; an EMPTY blob is
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

**Carried by the descriptor card.** The descriptor names an enum-keyed
array's KEY enum (`schema_rt.keyed_enum`) beside the variants
(`schema_rt.keyed_keys`), a map field's ENTRY TABLE (`schema_rt.map_entry`),
and each deprecated enum variant's flag on `ViewVariant`; the load's
`DeprecatedEnumShape` yields the field's declared default for such a variant
(§2.15), never the variant itself. A text-list clamp, of an element or of the
count (§2.13), is counted but does not set `reason` to
`clamped: <Holder.field>` as Go's does, and the report line the tests compare
carries no `reason`; the text-lists card (schema-lobster-vt-text-lists)
carries it. Retention (§6.6) and the region read are later cards'
(retain-unknown).

**The test.** `TestLobsterTableLoadMatchesTheReports` loads every report and
instance row of the tables manifest, one program per unit, and holds each
row's report to reports.txt and to the Go engine's `tablewire.Decode` on the
same wire, and each loaded value to Go's through THE VALUE DUMP — every slot,
count, presence bit, element, entry, arm and node, floats by their bits and
a graph by first-visit labels. `TestLobsterTableLoadNegativeControl` patches
the unknown-field count out of the emitted runtime and holds the comparison
red. `TestLobsterTableLoadBytesCountPastItsBody` raises a bytes body's count
past what its L frames (§4) and holds the leg to Go's refusal: damage
discards the whole load, so the read says malformed and nothing of the value
is compared.

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
- **The bench legs as they run.** `bench/lobster/run` (BenchMixed through
  `generated/bench/lobster`, family `gen`) and `bench/tables/lobster/leg`
  (TableMixed through `generated/bench/tables/lobster` on the tolerant wire,
  family `table`; the timed codec is the port's one runtime walk,
  `load_tolerant` then `save`) run their program under the pin's DEFAULT JIT
  with `--runtime-no-asserts`, every row `linkage=jit,checks=contract`. The
  clock is `seconds_elapsed()`. A Lobster program has one output stream and
  the compiler warns on it, so every line the runners print is tagged `csv `
  or `log ` and `bench/lobster/route.sh` routes them to stdout and stderr and
  refuses any other line: a warning fails the leg. Both are golden-gated
  before the clock (`make bench-lobster-gate`, on the ci-fast lobster row;
  `bench-lobster-gate-negative-control` flips each golden, plants a warning,
  and plants a `seconds_elapsed()` clock and a corpus name in `.lobster` files
  for `make shape-gate`, which scans `.lobster` and refuses that clock
  outside the runner directories). **The `--cpp` row is owed**
  (`port/lobster/bench-cpp` in docs/roadmap.sexp, card
  schema-lobster-bench-cpp): `lobster --cpp` writes into the toolchain's
  `dev/` tree, which the release archive does not carry, and its output builds
  only with the engine's full build (SDL, FreeType, OpenGL) from a checkout at
  the pin — a heavy build and a fleet job. Both legs print the card on stderr
  and no `--cpp` number until it lands. No timed number is taken on the
  studio; the first sitting is a fleet pass.
- **The bench found a type-wire gap, now closed.** serialize.lobster's fixed
  operations take a SIGNED Q format and refuse a declaration whose bounds the
  signed storage cannot hold, so the generated code passing a `ufixed(8, 8)`
  with max 250 as `(8, 8)` refused BenchMixed's own golden. A ufixed now rides
  the widest signed declaration of the same fraction bits (`fixedDeclaration`,
  write.go; the wire depends only on F and the raw bounds): storage 64 while
  its raw maximum stays below 2^63, else storage 128 with the 64-bit pattern
  zero-extended. `TestLobsterUnsignedFixedGoldens` reads and rewrites
  `bench_mixed.bin` and `unsigned_probe.bin` (span's raw values fill uint64's
  high half) byte for byte; its negative control restores the (I, F)
  declarations and requires both to refuse.
- **The conformance driver** (`test/conformance/lobster/driver`) runs
  `test/conformance/lobster/main.lobster` under the pinned toolchain. Lobster
  resolves imports when it compiles, so the program runs once per generated
  unit, in parallel: the driver writes it into `build/conformance-lobster/<unit>/`
  as `conformance_main.lobster`, after one `import .<Base>Table` line per table
  module (a module of imports alone does not compile), and runs
  `conformance_main.lobster -- <repo root> <manifest> <surface> <unit>`. Inputs
  are read by absolute path, because a relative `read_file` resolves against
  the program's directory. Stdout is one or more lines per case: the case name,
  a space, the answer's bytes in hex (a long answer continues under the same
  name), or the word `absent`. The driver writes one file per case in
  `<outdir>` (`<case>.absent` for the absent word) and refuses any other line,
  so a compiler warning fails the leg. `make build/conformance-lobster`
  generates every manifest unit for lobster and records each refusal by name
  as `<unit>.refused`; the driver answers that unit's cases `absent` without
  running anything. **The leg answers every surface the harness asks:**
  `wire` (`load_tolerant` then `save_reasoned`; a graph naming one blob from
  two slots is refused by name, `blob_shared`, as §2.5 requires of every
  writer, and answered `absent`), `report`, `message`, `retain`,
  `retain-save`, `cook`, `cook-write`, `cook-foreign`, `cook-forgery`,
  `cook-reason`, `block`, `block-foreign`, `block-dump`, `forgery`,
  `block-reason`, `json-read`, `json-write` and `json-hostile`. An `absent`
  case names its reason in the leg's own refusal (`blob_shared`, a message
  or retain row the reference itself answers absent).
- **The wire-fuzz differential** (test/conformance/README.md, "The
  wire-fuzz driver"; `make tables-lobster-wire-fuzz`, make/lobster-fuzz.mk) is
  a Go host shim, `test/conformance/lobster/wirefuzzhost`: it speaks the
  harness's stream and runs `test/conformance/lobster/wirefuzz.lobster` once
  per generated unit over a file of mutants, because the pinned console build
  has no blocking stdin read. The oracle is the Go engine
  (internal/tablewire). The budget is a count: `test-lobster` runs the
  enumerated pass and a small random count, `tables-lobster-release` the
  release count. Three arms run: the ordinary one over every form-1 root
  (variable roots answer `load_measure`, a port of `FileRegionMeasure`) and
  the manifest's fixed message batches, `--builder`, and `--retain` over the
  variable roots. A VARIABLE message root is not carried: the port's
  message form is fixed roots only, as Go's is (§3.3), so the arm answers 0
  for it. The retain arm and the message batches each have a pinned-vector replay and a
  negative control in `tables-lobster-wire-fuzz-negative-control`.
  `test/conformance/lobster/wirefuzz-known-leaks.txt` is empty:
  a loaded cyclic graph releases every node. The packet wire's hostile bytes
  are `packet-text-fuzz[lobster]`, and the shared hostile corpus runs through
  the port's own readers in `schema-lobster-runs-hostile-corpus`
  (make/lobster-hostile.mk), each with its negative control.
- **The strict gate** (`make tables-lobster-clean`,
  make/schema-lobster-generated-strict-gate.mk) compiles every generated unit
  of examples/, examples128/ and the conformance manifest under
  the pin and fails on any compiler output, because Lobster prints a warning
  and still exits 0.
- **Every toolchain run is the slow half** (internal/slowtest): lobsterrun
  skips by name unless SCHEMA_SLOW=1 or SCHEMA_REQUIRE_CORPUS is set, and
  `make test-lobster` and ci-full always set one.

## THE REGISTER

The `lobster` column of docs/PORTING.md is filled, every cell with its
reason, and `compiler/porting_test.go` holds it to the tree:

- **✅ carried now:** M4b (`TestLobsterImportsResolveInsideTheUnitOnly`),
  M6 and M18 (`TestLobsterVariableClass` and its control), M12 (the
  `read_*_le` / `write_*_le` builtins; `block_read` refuses on the magic and
  then the order word), M13 and J1 (`TestLobsterDescriptors`: the
  `constant_descriptor` and `accessor_descriptor` planted bugs turn it red),
  M16 and M20 (the variable-wire load and save, every manifest instance byte
  for byte and report for report against the Go engine), I4 (per-case absent,
  in the driver's line grammar), I10 (`TestLobsterTableFreeUnitAddsNothing`)
  and J4 (`tables-lobster-clean`, the strict gate below).
- **— the platform cannot:** M2 (an `int` is an unboxed 64-bit word), M3 (a
  `float` is an unboxed 64-bit slot), M4 (no inlining directive or aliasing
  qualifier; M4b is the shape), M10 (memory is the VM's compile-time reference
  counting over its own allocator), M11 (a Lobster class has no layout to
  assert), M17 (no caller-visible arena: a blob is one VM string at its exact
  size).
- **❌ not yet:** every other technique, each citing its own carry-across
  issue, titled `<technique>: carry to lobster` (#2246 to #2272, filed
  2026-10-28), and saying what the port has today.

**THE STRICT GATE** (`make/schema-lobster-generated-strict-gate.mk`, J4).
Lobster has no separate linter: its compiler type-checks every function of
every imported module and warns on stdout with exit status 0. So
`make tables-lobster-clean` generates examples/, examples128/
and every unit of the conformance manifest, compiles each unit
under the pin with `--pak` through a main that imports every module, and fails
on ANY output. `schema-lobster-generated-strict-gate-negative-control` (in the
`lobster` group of make/negative-controls.json) requires a clean baseline,
plants a never-reassigned `var` in a copy of the generated `Types.lobster` and
requires exactly that one warning. The vendored serialize.lobster before
v2.1.1 carries three warnings (`serialize_compressed_float.lobster` 118,
`serialize_bytes.lobster` 264), so the gate is red until the 2.1.1 re-vendor
lands; v2.1.1 compiles clean.

**CI.** `make dist-lobster` installs the pin (the linux archive, or the macOS
arm or x64 one, each refused on any sha256 but the release's own) wherever a
job runs the Lobster packages: ci-fast's go-test-touched and the `lobster`
leg-gate row (`test-lobster`, `tables-lobster-clean`), ci-full's unit-codegen
row (plus `test-lobster` against the byte oracle), the pins job, the
conformance and negative-control rows keyed on a row's `lobster` field,
certify's `make test` jobs and leg.yml. A hosted ubuntu runner first installs
`libopengl0` and `libglx0`, which the linux release binary links.

## THE CARD MAP

The port reached main in mas-bandwidth/schema#2277 (2026-10-28), which folded
dev's leg (#2151), #2176, #2185 and every sprint/schema-lobster-* card that
had not landed on dev. What each surface is, where it lives, and where it
stands:

| surface | files | status |
|---|---|---|
| serialize.lobster vendored verbatim, pinned by SERIALIZE_PIN (v2.1.1) | `lobster/runtime/serialize*.lobster`, `make/lobster-serialize.mk` | landed |
| the type wire's write, byte for byte against the C++ goldens; write misuse traps in the runtime, the schema's own contracts are `assert` | `lobster/write.go` | landed |
| the type wire's read, every refusal latched by name, `serialize_end` | `lobster/read.go`, `lobster/runtime/schema_wire.lobster` | landed |
| packet floats finite only; packet defaults | `lobster/` | landed |
| the variable table wire's load (strict, and `load_tolerant`) and save | `lobstertable/runtime/load.lobster`, `schema_save.lobster` | landed |
| the fixed form and its evolution under the lock's lineage | `lobstertable/fixedform.go`, `fixedevolve.go` | landed |
| table floats finite only in every form | `lobstertable/` | landed |
| defaults, renames, deprecation, the build version; optionals and `\| required`; collections; text lists; wide kinds; descriptors | `lobstertable/*.go` and their runtime modules | landed |
| the message form | `lobstertable/messageform.go` | landed |
| the block form, Open refusing a forged layout | `lobstertable/block_form.go` | landed |
| the cooked form: Cook, CookMeasure, Open | `lobstertable/cook.go` | landed |
| retain-unknown | `lobstertable/retain_unknown.go` | landed |
| the JSON text form both ways | `lobstertable/json.go` | landed |
| the hand-editable text form | `lobstertable/text_form.go` | landed |
| the conformance leg, every surface answered | `test/conformance/lobster/` | landed |
| the card facade and its conformance leg (docs/PORTING.md J6): load, save, render, parse, bounds, upgrade, edit, taskhash and view over the Card unit, SHA-256 written in Lobster; the driver stages the facade into the generated unit, where Lobster resolves imports | `tables/cards/lobster/`, `make/lobster.mk` (`cards-lobster-*`) | landed |
| the wire-fuzz differential and the hostile corpus | `make/lobster-fuzz.mk`, `make/lobster-hostile.mk` | landed, with the retain and message arms; a variable message root is not carried (as Go) |
| the strict gate, CI, the register column, the roadmap column | `make/schema-lobster-generated-strict-gate.mk`, `.github/workflows/*`, `docs/PORTING.md`, `docs/roadmap.sexp` | landed |
| the integrity trailer and the `forms` declaration (§2.12, §2.14) | — | owed |
| the bench legs, JIT (packet and tables, golden-gated, `bench-lobster-gate`) | `bench/lobster/`, `bench/tables/lobster/`, `make/lobster-bench.mk` | landed; the first sitting is a fleet pass |
| the bench legs' `--cpp` row | — | owed (`port/lobster/bench-cpp`, schema-lobster-bench-cpp) |
| native snake_case naming (Lobster's own idiom; the port still spells names as Go does) | `lobster/`, `lobstertable/` | owed |

Each carry-across construct of compiler/carries.go joins with its own
`var _ = carryLobster(...)` file when it lands.
