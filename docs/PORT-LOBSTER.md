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

One packet module per schema file, `<Base>.lobster`, and a separate
`<Base>Table.lobster` carrying its reflection declarations and any table codecs.
Every module of a unit declares the same `namespace <package>`, so a consumer spells a
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

## THE UNIT REGISTRY

Import `<package>View.lobster` and call `<package>.UnitView()` to enumerate
all six declaration sets. The entry point imports every file's reflection
module, including constant-only files and packet types no table reaches.
Table-free units keep all metadata in the view entry point and emit only its
descriptor dependencies (`descriptors` and `schema_tables`), with no `*Table`
module or load runtime. Deprecated-enum read hooks live in `descriptors_load`
and are imported only by table units. Packet modules keep their packet
imports; a consumer opts into reflection by importing the view module.

The extra output is part of generation, so runtime module file names are
reserved only when the unit emits those modules. A schema basename that
collides with `<package>View.lobster` is refused before output is written.
Generated `UnitView`, `<Name>Descriptor`, and enum-keyed `Slot`/`At`/`Each`
accessor names are also claimed before emission; conflicting declarations
or fields must be renamed. Flattened enum and flags member names participate
in the same collision check. Package names matching a namespace-scope binding
in an emitted packet or descriptor/table runtime are refused conservatively:
Lobster can interpret a global's member access as a package-qualified name.
The scan reserves top-level `def`, `class`, `let` and `var` names only;
parameters, locals and callback variables are function-scoped and reserve
nothing, so ordinary names such as `tag` and `t` still generate. Comments,
literal contents and plain class fields are also ignored.
`TestLobsterUnitViewRuntimePackages` checks the remaining namespace-scope
refusals and that `package tag` and `package t` still generate; `package tag`
also runs under the native compiler.
`TestLobsterUnitViewIncludesUnreachedFiles` checks table-free and mixed units,
all six sets, and missing constant metadata as its negative control.

`make tables-lobster-view` runs `TestLobsterUnitViewListing`: the native view
printer byte-compares every `VIEW_CORPUS` and table-manifest unit with the
shared IR listing (§8.7), including field type spellings, source filenames,
closure-only wire identity, vocabulary widths, union payloads, docs and tags.
Descriptor-ID and arm-offset mutations must change the listing. The union
arm and payload offsets describe the shared region/cook projection, not VM
addresses; the printer compares those independently emitted columns.
The package-wide `test-lobster` gate includes this test as well.

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
or flags default and a packet `wstring(N)` are carried too: the type wire
reads and writes both over serialize.lobster v2.1.1 (`write.go`, `read.go`;
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
| `Value { b, i, w, f, s, units, tab, arm, node, blob }` | one cell; only the member the shape names is read: `i` every integer of 64 bits or fewer as its bit pattern, an enum value, a union tag; `w` the raw `Int128` of a wide kind (18–29, fixed point as units × 2^F); `f` a float (a float32 as its exact double); `s` string or bytes at the used extent; `units` wstring code units; `tab` a by-value body, a body arm, an element record, a map entry; `arm` a non-body arm's `Slot`; `node` a pointee; `blob` a blob's bytes | `Cell` |

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
  of examples/, examples128/, examples-wide/ and the conformance manifest under
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
  in the driver's line grammar), I10 (`TestLobsterTableFreePacketIdentity`)
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
`make tables-lobster-clean` generates examples/, examples128/,
examples-wide/ and every unit of the conformance manifest, compiles each unit
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
| packet floats finite only; packet defaults; packet wstring | `lobster/` | landed |
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


## Unit-registry runtime measurements (2026-11-05)

Measured source: `39a1ade05b749479cb378b987262eb6e61988adc`. The required
`key_reader` and `nested_writer` repairs remain unchanged. Restoring `probe`
causes the generated package named `probe` to fail compilation.

The fleet Linux release archive is Lobster v2026.8 (`0921f7dd5745`), SHA256
`2f03fb96fdaf6935469c99e43ffb182cab0d797faec7b26eccc9a732df32ad31`.
Commands used the fleet slot guard, nice 19 and timeout 600 seconds; Go width
was at most four. Timing processes inherited affinity to logical CPU 0.
Shared fleet load was approximately 24 on 32 logical CPUs. These are noisy
shared-fleet observations, not a speedup claim or an isolated-core result.

### Affected paths

Both sides were generated from the measured source. The before overlay
replaced only `key_reader` with `probe` in `json.lobster` and `nested_writer`
with `probe` in `message.lobster`. Packages `mapdemo` and `backenddemo` avoid
the namespace collision, so both versions compile. This comparison isolates
the repair; it is not a full pre-PR comparison.

JSON uses `tables/maps` Chunks: load `map_chunks.bin`, write JSON once, then
time JSON read and wire save. Its signed numeric keys execute j_place_map_key.
Messages use `tables/backend` MatchResult: load `match_full.bin`, then time
save_messages of that record. Its ten nested PlayerRow records exercise
mw_cell_rides. Before timing, saved bytes are compared against the committed
map_chunks.bin and match_full_message.bin goldens, respectively.

Each invocation uses one discarded warmup plus seven measured rounds of
20,000 operations, folding output lengths into a printed sink. Assertions
remain enabled. Ten pairs alternate AB/BA. Below: invocation medians, ns/op.
All pairs are included; no favorable-run selection was performed.

| Pair | JSON before | JSON after | Message before | Message after |
| --- | ---: | ---: | ---: | ---: |
| 0 | 10771.20 | 6683.60 | 10085.20 | 6419.55 |
| 1 | 9695.95 | 8578.80 | 8889.75 | 13195.35 |
| 2 | 8711.80 | 8696.05 | 9908.15 | 9892.00 |
| 3 | 7411.35 | 8194.55 | 10177.65 | 9024.05 |
| 4 | 7488.70 | 8103.80 | 10735.80 | 11367.25 |
| 5 | 5571.55 | 8979.10 | 5772.05 | 7561.25 |
| 6 | 5096.20 | 7410.10 | 7008.35 | 9142.45 |
| 7 | 5428.40 | 5725.15 | 6447.50 | 6423.80 |
| 8 | 5741.60 | 5785.35 | 7311.40 | 6621.60 |
| 9 | 7476.40 | 5648.25 | 8578.25 | 6849.20 |

Median paired after/before ratios: JSON **1.031143**, messages **0.997347**.
Ranges: 0.620507–1.611598 and 0.636532–1.484333. These measurements do **not**
establish the strict no-slowdown timing clause. They were not rerun to seek a
favorable median.

For both programs, generated C is identical after normalizing only the two
local identifiers, build timestamp and program fingerprint. Normalized SHA256:
JSON `014b057218db0d6840c3ffd2f62810592f2483935458148f0e057941705f1290`;
messages `623fbe77805f4ecd2cb1e5d964906796815199a2e18bf3cef9eca784e176bb10`.
This proves equal generated C operations, not identical unnormalized artifacts
or a measured timing bound.

### Reproduce the affected-path comparison

Build `go build -p 4 -o .git/schema ./cmd/schema`. Generate
`.git/hot-after/maps` from `tables/maps` and `.git/hot-after/backend` from
`tables/backend`, using `.git/schema generate --lang lobster --out DIR INPUT`.
Copy `.git/hot-after` to `.git/hot-before` and apply the two substitutions
above. Save these programs as hot.lobster in their respective directories;
replace ROOT with the absolute checkout path. Run the pinned archive binary
with default JIT and assertions enabled, without --runtime-no-asserts.

JSON:

```lobster
import .mapdemoView
import .json
import .load
let wire = read_file("ROOT/testdata/wire/tables/map_chunks.bin")
assert wire
let rec = schema_rt.load_tolerant("Chunks", wire, schema_rt.LoadReport {})
assert rec
let text, why = schema_rt.json_write(rec)
assert why == ""
let check = schema_rt.json_read("Chunks", text, schema_rt.LoadReport {})
assert check
assert schema_rt.save(check) == wire
var sink = 0
for(8) round:
    let start = seconds_elapsed()
    for(20000) i:
        sink += i & 1
        let back = schema_rt.json_read("Chunks", text, schema_rt.LoadReport {})
        assert back
        sink += length(schema_rt.save(back))
    print("json,{round}," + string((seconds_elapsed() - start) * 1000000000.0 / 20000.0))
print("sink {sink}")
```

Messages:

```lobster
import .BackendTable
import .message
import .load
let wire = read_file("ROOT/testdata/wire/tables/match_full.bin")
assert wire
let rec = schema_rt.load_tolerant("MatchResult", wire, schema_rt.LoadReport {})
assert rec
let values = [rec]
let expected = read_file("ROOT/testdata/wire/tables/match_full_message.bin")
assert expected
let packed, count = schema_rt.save_messages(values, schema_rt.LoadReport {})
assert count == length(expected)
assert packed == expected
var sink = 0
for(8) round:
    let start = seconds_elapsed()
    for(20000) i:
        sink += i & 1
        let data, n = schema_rt.save_messages(values, schema_rt.LoadReport {})
        sink += length(data) + n
    print("message,{round}," + string((seconds_elapsed() - start) * 1000000000.0 / 20000.0))
print("sink {sink}")
```

Set os.sched_setaffinity(0, {0}) in the invoking Python process. For pairs
0–9 invoke before/after in AB, BA alternating order, discard round 0 and take
the median of rounds 1–7. Refuse compiler warnings, nonzero status or missing
sink. `lobster --c-out hot.lobster` writes compiled_lobster_jit.c beside the
program. Normalize identifier tokens, build timestamp and the fingerprint
immediately before subfunctions_to_function.

### Correctness gates

`make tables-lobster-view`: PASS 7.107s, all 15 VIEW_CORPUS and 35 manifest
units, including descriptor-ID and arm-offset controls. The focused
`go test -p 4 -parallel 4 -count=1 -timeout 600s ./internal/codegen/lobstertable
-run 'TestLobster(UnitView|RuntimePackage)' -v`: PASS 4.863s, SCHEMA_SLOW=1 and
the pinned archive. Standard packet and table byte gates both pass, corpus
IDs fd0748942dc6ae09 and b51387f36d9b59c4. Three refreshed schema_tables copies
equal the source runtime byte for byte. No source/generated runtime changed
in this documentation-only repair attempt. Complete regeneration was not rerun.

The full touched-package gate on the carried source passes lobster (0.864s)
and tablenames (0.005s), but lobstertable fails (168.399s): JsonMatchesTheReference,
JsonNegativeControl, TableLoadUnlinksWhatTheRootDoesNotReach and its control,
TableReleaseEndsAReachedCycle and its control, and Cook (all prefixed
TestLobster). Examples: feature_full saves 233 bytes versus 313, text-total
clamp counts differ, release tests get a nil root, and the unlink control finds
zero anchors. These are failures in the carried source before this attempt;
they are not claimed fixed or green.

<details><summary>All affected-path rounds (round 0 is warmup; ns/op)</summary>

```text
pair,side,path,rounds0..7,sink
0,before,maps,7265.1;8012.5;10771.19999999;16960.75;10070.24999999;8379.8;13504.75;12425.79999999,sink 17200000
0,before,backend,16296.2;14874.8;12708.8;10085.19999999;9687.8;10125.64999999;6502.3;6697.2,sink 47440000
0,after,maps,14551.5;5851.35;5154.1;7280.5;8315.2;8710.25;6683.6;6114.549999999,sink 17200000
0,after,backend,10293.55;6394.65;6419.549999999;6453.999999999;6380.1;6154.399999999;6785.749999999;6571.799999999,sink 47440000
1,after,maps,5479.85;5620.549999999;11310.6;8118.3;8578.799999999;8191.949999999;8825.649999999;10947.24999999,sink 17200000
1,after,backend,18867.15;36254.75;13195.35;10936.24999999;12833.75;10432.69999999;41366.65;19272.39999999,sink 47440000
1,before,maps,9138.6;9695.949999999;10618.79999999;10696.84999999;8889.649999999;9801.7;8037.549999999;8221.25,sink 17200000
1,before,backend,10186.3;8889.749999999;9801.749999999;6118.75;5814.199999999;5876.899999999;9282.999999999;9876.649999999,sink 47440000
2,before,maps,8799.15;8488.35;8625.05;8708.25;8761.75;8711.8;8750.25;8742.249999999,sink 17200000
2,before,backend,9939.599999999;9908.15;9910.799999999;10043.95;9953.2;9881.2;9813.05;9769.3,sink 47440000
2,after,maps,8572.7;8615.75;8558.7;8463.1;8805.049999999;8702.549999999;8696.049999999;8778.949999999,sink 17200000
2,after,backend,10134.54999999;10067.29999999;9851.149999999;10054.55;7740.849999999;6591.1;9892.0;10996.34999999,sink 47440000
3,after,maps,9698.45;7368.9;7403.1;7422.299999999;8245.049999999;9709.1;9705.699999999;8194.549999999,sink 17200000
3,after,backend,8521.899999999;9024.049999999;12361.29999999;12119.64999999;8705.6;8439.949999999;8334.15;11887.34999999,sink 47440000
3,before,maps,8759.95;7339.0;7411.35;7304.899999999;8929.0;9487.85;9289.749999999;7308.099999999,sink 17200000
3,before,backend,8457.649999999;10177.65;12041.65;11013.75;8500.95;8566.149999999;10020.55;12163.99999999,sink 47440000
4,before,maps,7534.35;7368.8;7441.999999999;7459.15;9753.4;9696.799999999;9008.6;7488.7,sink 17200000
4,before,backend,8489.599999999;10863.65;12227.14999999;10724.89999999;8598.5;8621.599999999;10735.79999999;12113.3,sink 47440000
4,after,maps,7670.85;7697.85;8103.799999999;9475.95;9638.85;8897.45;7394.25;7426.8,sink 17200000
4,after,backend,10843.74999999;12165.74999999;10400.14999999;8780.15;11367.25;12720.79999999;12146.15;8823.999999999,sink 47440000
5,after,maps,8068.149999999;9960.299999999;10007.09999999;8979.1;7945.049999999;7972.349999999;8670.95;9068.2,sink 17200000
5,after,backend,6684.05;7221.2;7874.1;7624.2;7814.75;7561.249999999;5747.0;6048.849999999,sink 47440000
5,before,maps,5215.15;6224.45;7243.95;7285.549999999;5334.45;5528.6;5433.949999999;5571.55,sink 17200000
5,before,backend,6144.7;5791.3;5751.349999999;5889.649999999;5750.05;5772.05;5751.05;5788.799999999,sink 47440000
6,before,maps,5122.0;5520.95;5003.4;5004.55;5096.2;5134.149999999;5025.65;5196.349999999,sink 17200000
6,before,backend,6638.649999999;6104.749999999;6364.55;6163.2;7008.35;7175.799999999;7208.599999999;7188.65,sink 47440000
6,after,maps,6365.699999999;7410.1;7252.299999999;9010.299999999;9331.45;9373.8;6210.5;5255.549999999,sink 17200000
6,after,backend,6656.0;7779.55;9142.449999999;10195.7;10311.29999999;10778.89999999;7220.599999999;7183.549999999,sink 47440000
7,after,maps,5522.65;5441.749999999;5503.1;5764.3;5962.199999999;5840.649999999;5302.8;5725.15,sink 17200000
7,after,backend,7244.95;7516.699999999;7030.65;6113.8;6423.8;5967.95;6260.3;6629.649999999,sink 47440000
7,before,maps,5300.85;5423.15;5827.65;5539.05;5428.4;5188.049999999;5320.75;5822.649999999,sink 17200000
7,before,backend,6433.749999999;6159.3;6506.55;6638.25;8893.9;6007.65;6204.349999999;6447.499999999,sink 47440000
8,before,maps,5238.85;5809.999999999;7307.05;5292.9;5676.85;5211.349999999;6654.1;5741.6,sink 17200000
8,before,backend,7145.95;6384.55;7962.999999999;7330.4;6237.9;7311.4;10416.29999999;6974.95,sink 47440000
8,after,maps,6201.9;5616.7;5588.55;5886.5;6195.699999999;5759.8;6487.799999999;5785.35,sink 17200000
8,after,backend,6130.75;6494.45;6621.6;6408.949999999;6624.399999999;6654.199999999;5844.85;9159.35,sink 47440000
9,after,maps,9111.2;6702.7;5648.249999999;5366.5;5300.6;5236.449999999;6136.95;6324.35,sink 17200000
9,after,backend,7175.8;6849.199999999;5932.399999999;8619.299999999;6433.25;6294.349999999;10481.59999999;10468.1,sink 47440000
9,before,maps,8834.149999999;7773.7;7499.749999999;7564.05;7476.4;7423.35;7412.25;7435.649999999,sink 17200000
9,before,backend,8433.6;8572.25;8464.65;8597.749999999;9279.55;8578.249999999;6745.999999999;8676.75,sink 47440000
```
</details>

### Standard packet and tolerant-table comparison

Baseline: `3bf4ff638b7a` (the pre-carry base); after: measured source above.
Both use the committed benchmark harness, generated modules and 64-record
variant corpus, each invocation running its byte/round-trip gate before timing.
Pin, affinity and alternating ten-pair order are the same as above. Commands:
`bench/lobster/run --csv --round PAIR --iterations 40000` and
`bench/tables/lobster/leg run --csv --round PAIR --iterations 4096`, with
LOBSTER set to the pinned archive binary. Release defaults: JIT, contract
checks, runtime assertions disabled by the existing route. Counts deliberately
bound fleet cost; they are recorded here, not full standard-count results.
Packet family is gen, table family table. No C++/AOT result is claimed.
The initial table timing attempt used 4000, which the harness correctly refused
because counts must be multiples of 64; its one completed packet observation
is preserved below, and no table timing came from that failed attempt.

| Workload | Before median ns/op | After median ns/op | Median paired ratio |
| --- | ---: | ---: | ---: |
| packet write | 6082.49 | 5825.57 | 0.979029 |
| packet round_trip | 14832.22 | 15287.48 | 1.034875 |
| table write | 96153.89 | 106637.21 | 1.217771 |
| table round_trip | 368367.19 | 346622.10 | 0.976606 |

These additional shared-fleet observations do not remove the timing uncertainty
above; no favorable rerun or speedup claim is made.

<details><summary>All standard paired rows (ns/op)</summary>

```text
pair,side,leg,path,iterations,corpus,ns
0,before,packet,write,40000,fd0748942dc6ae09,5987.557855
0,before,packet,round_trip,40000,fd0748942dc6ae09,13589.356816
0,before,table,write,4096,b51387f36d9b59c4,99552.015928
0,before,table,round_trip,4096,b51387f36d9b59c4,353731.871242
0,after,packet,write,40000,fd0748942dc6ae09,5825.333209
0,after,packet,round_trip,40000,fd0748942dc6ae09,18464.151849
0,after,table,write,4096,b51387f36d9b59c4,150966.183575
0,after,table,round_trip,4096,b51387f36d9b59c4,482392.667631
1,after,packet,write,40000,fd0748942dc6ae09,8340.631386
1,after,packet,round_trip,40000,fd0748942dc6ae09,19767.533802
1,after,table,write,4096,b51387f36d9b59c4,143678.160920
1,after,table,round_trip,4096,b51387f36d9b59c4,492610.837438
1,before,packet,write,40000,fd0748942dc6ae09,8493.650996
1,before,packet,round_trip,40000,fd0748942dc6ae09,19471.921489
1,before,table,write,4096,b51387f36d9b59c4,81906.790073
1,before,table,round_trip,4096,b51387f36d9b59c4,306842.589751
2,before,packet,write,40000,fd0748942dc6ae09,4891.889248
2,before,packet,round_trip,40000,fd0748942dc6ae09,11659.224195
2,before,table,write,4096,b51387f36d9b59c4,71751.452967
2,before,table,round_trip,4096,b51387f36d9b59c4,283286.118980
2,after,packet,write,40000,fd0748942dc6ae09,8191.685439
2,after,packet,round_trip,40000,fd0748942dc6ae09,14052.048789
2,after,table,write,4096,b51387f36d9b59c4,107342.206956
2,after,table,round_trip,4096,b51387f36d9b59c4,351246.926589
3,after,packet,write,40000,fd0748942dc6ae09,5477.261150
3,after,packet,round_trip,40000,fd0748942dc6ae09,15570.988135
3,after,table,write,4096,b51387f36d9b59c4,105853.710173
3,after,table,round_trip,4096,b51387f36d9b59c4,434971.726838
3,before,packet,write,40000,fd0748942dc6ae09,7582.708392
3,before,packet,round_trip,40000,fd0748942dc6ae09,18056.408219
3,before,table,write,4096,b51387f36d9b59c4,118119.536971
3,before,table,round_trip,4096,b51387f36d9b59c4,437636.761488
4,before,packet,write,40000,fd0748942dc6ae09,6720.430108
4,before,packet,round_trip,40000,fd0748942dc6ae09,17827.854239
4,before,table,write,4096,b51387f36d9b59c4,130191.381331
4,before,table,round_trip,4096,b51387f36d9b59c4,455788.514129
4,after,packet,write,40000,fd0748942dc6ae09,5154.692316
4,after,packet,round_trip,40000,fd0748942dc6ae09,12301.182144
4,after,table,write,4096,b51387f36d9b59c4,72469.019494
4,after,table,round_trip,4096,b51387f36d9b59c4,298062.593145
5,after,packet,write,40000,fd0748942dc6ae09,5825.808331
5,after,packet,round_trip,40000,fd0748942dc6ae09,12438.739209
5,after,table,write,4096,b51387f36d9b59c4,84488.002704
5,after,table,round_trip,4096,b51387f36d9b59c4,306278.713629
5,before,packet,write,40000,fd0748942dc6ae09,5968.617012
5,before,packet,round_trip,40000,fd0748942dc6ae09,13459.493654
5,before,table,write,4096,b51387f36d9b59c4,102785.486689
5,before,table,round_trip,4096,b51387f36d9b59c4,408830.744072
6,before,packet,write,40000,fd0748942dc6ae09,4873.555600
6,before,packet,round_trip,40000,fd0748942dc6ae09,12468.050620
6,before,table,write,4096,b51387f36d9b59c4,92755.774047
6,before,table,round_trip,4096,b51387f36d9b59c4,298240.381748
6,after,packet,write,40000,fd0748942dc6ae09,6779.752946
6,after,packet,round_trip,40000,fd0748942dc6ae09,17625.804177
6,after,table,write,4096,b51387f36d9b59c4,138064.337981
6,after,table,round_trip,4096,b51387f36d9b59c4,472143.531634
7,after,packet,write,40000,fd0748942dc6ae09,6770.114009
7,after,packet,round_trip,40000,fd0748942dc6ae09,16813.504607
7,after,table,write,4096,b51387f36d9b59c4,105932.203390
7,after,table,round_trip,4096,b51387f36d9b59c4,321750.321750
7,before,packet,write,40000,fd0748942dc6ae09,6177.415369
7,before,packet,round_trip,40000,fd0748942dc6ae09,15436.863229
7,before,table,write,4096,b51387f36d9b59c4,87873.462214
7,before,table,round_trip,4096,b51387f36d9b59c4,501002.004008
8,before,packet,write,40000,fd0748942dc6ae09,6912.713171
8,before,packet,round_trip,40000,fd0748942dc6ae09,18053.148469
8,before,table,write,4096,b51387f36d9b59c4,160000.000000
8,before,table,round_trip,4096,b51387f36d9b59c4,356506.238859
8,after,packet,write,40000,fd0748942dc6ae09,5515.446007
8,after,packet,round_trip,40000,fd0748942dc6ae09,14748.609944
8,after,table,write,4096,b51387f36d9b59c4,102870.075095
8,after,table,round_trip,4096,b51387f36d9b59c4,341997.264022
9,after,packet,write,40000,fd0748942dc6ae09,5777.541541
9,after,packet,round_trip,40000,fd0748942dc6ae09,15003.976054
9,after,table,write,4096,b51387f36d9b59c4,111074.086416
9,after,table,round_trip,4096,b51387f36d9b59c4,339558.573854
9,before,packet,write,40000,fd0748942dc6ae09,5676.915391
9,before,packet,round_trip,40000,fd0748942dc6ae09,14227.584441
9,before,table,write,4096,b51387f36d9b59c4,90301.607369
9,before,table,round_trip,4096,b51387f36d9b59c4,380228.136882
```
</details>

Initial invalid-count attempt, completed packet rows only:

```csv
lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline
lobster,bench_mixed,write,40000,438,1,136847,136847,136847,57.16,0.00,fd0748942dc6ae09,gen,jit,contract,default,unknown
lobster,bench_mixed,round_trip,40000,438,1,58946,58946,58946,24.62,0.00,fd0748942dc6ae09,gen,jit,contract,default,unknown
```
