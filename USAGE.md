# Using schema

Every feature of the language, with the code it generates.

The generated code in this guide is real compiler output, not an
approximation — if it looks surprising, it is because that is what the
compiler does.

- [The five minute version](#the-five-minute-version)
- [Declarations](#declarations)
  - [const](#const) · [enum](#enum) · [flags](#flags) · [type](#type) ·
    [union](#union)
- [Field types](#field-types)
  - [Integers](#integers) · [Ranged integers](#ranged-integers) ·
    [bits(N)](#bitsn) · [bool](#bool) · [Floats](#floats) ·
    [Compressed floats](#compressed-floats) · [fixed(I, F)](#fixedi-f) · [ufixed(I, F)](#ufixedi-f) ·
    [Strings and bytes](#strings-and-bytes) · [Arrays](#arrays) ·
    [Composition](#composition)
- [Branches: if / else](#branches-if--else)
- [Defaults](#defaults)
- [The wire](#the-wire)
- [Reading untrusted data](#reading-untrusted-data)
- [The protocol id](#the-protocol-id)
- [Embedding the compiler](#embedding-the-compiler)
- [Per-language notes](#per-language-notes)

---

## The five minute version

Write a `.schema` file:

```
package tour

const MaxHealth = 1000

enum ShipType { Fighter, Corvette, Bomber }

type Vector3
{
    x float64
    y float64
    z float64
}

type ShipCreate
{
    ship_type ShipType
    position  Vector3
    health    int32 | min = 0, max = MaxHealth
}
```

Generate:

```
schema generate --lang cpp --out generated/cpp .
```

Success is silent — the files land in the output directory and nothing is
printed, so a build wrapping this command stays clean. Add `--verbose` to
list every file written (`check` and `fmt` take the same flag).
Errors always print, whatever the verbosity.

You get a struct and a pair of wire functions:

```cpp
struct ShipCreate {
    ShipType ship_type = ShipType::None;
    Vector3 position;
    int32_t health = 0; // wire [0, 1000]
};

inline bool WriteShipCreate( serialize::WriteStream & stream, const ShipCreate & value );
inline bool ReadShipCreate( serialize::ReadStream & stream, ShipCreate & value );
```

Run the same command with `--lang c`, `--lang cs`, `--lang elixir`, `--lang go`, `--lang java`,
`--lang js`, `--lang dart` or `--lang rust` and you get the equivalent in that
language — writing the
**same bits**.

---

## Declarations

### const

```
const MaxHealth = 1000
const TickInterval = 1.5
const Pi float32 = 3.14159265359
```

Constants are exported into every generated language, so the bound you
declared is the bound your code compares against — no second copy to drift.
They may reference each other in any order, across files.

An untyped constant infers its type from its expression, Go's rule: a
literal with a decimal point or exponent makes it `float64`, a bare integer
makes it `int64`. Name a type explicitly when you want a different one.

### enum

```
enum ShipType { Fighter, Corvette, Bomber }
```

**Every enum has an implicit `None = 0`.** Variants pack from 1. A
zero-initialized enum field is therefore the null, in band, and you never
need a separate has-flag beside it:

```cpp
enum class ShipType : uint8_t { None = 0, Fighter = 1, Corvette = 2, Bomber = 3, Max = 3 };
```

On the wire it costs `bitsRequired(variant count)` — 2 bits here, for four
values. Declaring `enum E | max = 15` (its variant list on the next line) reserves
headroom so you can
add variants later without moving the field width.

The `Max` member is the enum's **extent** — the same number `E.Max` names in
schema expressions: the highest wire-legal value, and (headroom aside) the
count of real variants under the sentinel-zero convention. Every target
spells it its own way — `ShipType::Max` (C++), `ShipType.Max` (C#),
`ShipTypeMax` (Go), `ShipType::MAX` (Rust), `ShipType.Max` (JS),
`SHIP_TYPE_MAX` (C) — and a union's generated `<Union>Type` tag enum
carries it too, so ranges and asserts reference the enum directly instead of
a hand-declared count constant. `Max` is consequently reserved as a variant
name, like `None`.

Every target also generates a **debug/log name function** — `EnumName(value)`
in C++ (overloaded per enum), `EnumNameShipType` in C#/Go/JS,
`enum_name_ship_type` in Rust/C — returning the variant's declared spelling
for any wire value, out-of-set values included (`"???"`).

### flags

```
flags Capabilities { Cloak, Shield, Warp }
```

One bit per variant, consumed as a mask. Storage is `uint64` in every
language (more than 64 variants is a compile error); the wire is exactly as
many bits as there are variants:

```cpp
using Capabilities = uint64_t;
inline constexpr Capabilities Capabilities_Cloak  = 1ull << 0;
inline constexpr Capabilities Capabilities_Shield = 1ull << 1;
inline constexpr Capabilities Capabilities_Warp   = 1ull << 2;
inline constexpr int64_t CapabilitiesCount = 3;
```

The declared variant count is exported as `Count` and usable in schema
expressions as `Capabilities.Count`. Flags have no `.Max` — the variants are
independent bits, not a range with a top; the compiler refuses `.Max` on a
flags type and names `.Count` instead.

Flags get the same **debug/log name surface** as enums, in two forms: a
per-bit name — `FlagNameCapabilities(bit)` (`flag_name_capabilities` in
Rust/C), out-of-range bits naming as `"???"` — and a set renderer,
`FlagNamesCapabilities(value)`, which formats the set bits the way a log line
wants them: `"Cloak|Warp"`, `"0"` for the empty set, and any bits past the
declared variants rendered honestly as hex. In C and C++ the renderer writes
into a caller-provided buffer (`CapabilitiesNamesMax` /
`CAPABILITIES_NAMES_MAX` bytes always suffice) and allocates nothing.

### type

A plain struct. Composes into other types:

```
type Vector3
{
    x float64
    y float64
    z float64
}
```

### union

A first-class one-of: at most one of a named set of payloads, discriminated
by a generated tag.

```
union ColliderShape
{
    box     BoxCollider
    sphere  SphereCollider
    capsule CapsuleCollider
    hull    HullCollider
}

type Collider
{
    armor uint8
    shape ColliderShape
}
```

**Every union has an implicit `None = 0`** — the empty union, in band, so a
zero-initialized union field carries "no shape" without a has-flag. The
compiler generates the tag enum `ColliderShapeType` (`None = 0`, variants in
declared order, plus `Max`), and the wire is the tag in minimal bits for
`[0, variant count]` followed by **the selected payload only**:

```cpp
enum class ColliderShapeType : uint8_t { None = 0, Box = 1, Sphere = 2, Capsule = 3, Hull = 4, Max = 4 };

struct ColliderShape
{
    ColliderShapeType type;

    union
    {
        BoxCollider box;
        SphereCollider sphere;
        CapsuleCollider capsule;
        HullCollider hull;
    };

    ColliderShape() : type( ColliderShapeType::None ) {}
};
```

That replaces the bool-guard idiom — `has_box bool` / `if has_box { box
BoxCollider }` repeated per shape — which spends a bit per absent arm and
lets illegal states (two shapes at once) exist for every consumer to police.
A read **rejects a tag above the count**, and a write validates the tag
before it rides. C++ generates the tagged union above; Go, C# and JS lay
the tag beside one pre-allocated arm per variant; Rust gets a real
`enum ColliderShape { None, Box(BoxCollider), ... }`.

Payloads are declared types — wrap a scalar or enum in a type. Unions ride
`type` bodies, arrays included.

**Building your protocol.** A union of payload types is a message system
waiting for your framing, and the primitives compose into whichever framing
your protocol wants:

```
union Payload
{
    input    InputPacket
    chat     Chat
    snapshot Snapshot
}

type Packet
{
    sequence uint16
    payloads [..MaxPayloadsPerPacket]Payload
}
```

One declaration gives you the tag enum, the validated tag wire, and
`WritePacket`/`ReadPacket` end to end. Prefer a terminated stream to a
counted array? Write `Payload` values back to back and end on the in-band
`None` — the tag costs the same bits either way, and the loop is yours. The
protocol id (see below) covers every payload shape, so both peers agree on
the whole set or refuse to talk — the same guarantee, whatever framing you
choose.

---

## Field types

### Integers

`int8` `int16` `int32` `int64` `int128` and their `uint` counterparts. Full
width on the wire unless you give a range.

### Ranged integers

```
health int32 | min = 0, max = MaxHealth
```

The range is **part of the type**, and the wire cost follows from it. This
is the single most useful feature in the language: 1001 values need 10 bits,
so that is what it writes.

```cpp
serialize_assert( value.health >= 0 && value.health <= MaxHealth );
write_bits( stream, uint32_t( value.health ), 10 );
```

A read outside the range **fails** — see
[Reading untrusted data](#reading-untrusted-data).

### bits(N)

```
tag bits(12)
```

An unsigned field of exactly N bits, when you want to say the width
directly.

### bool

One bit.

### Floats

`float32` and `float64`, written whole (32 and 64 bits).

### Compressed floats

```
throttle float32 | min = 0, max = 1, resolution = 0.01
```

A float quantized onto a grid: the wire carries the step index, not the
float. 101 steps here, so 7 bits instead of 32. The write rounds to the
nearest step, half away from zero — the one rounding rule, everywhere.

### fixed(I, F)

```
position fixed(48, 16) | min = -30000, max = 30000
```

Signed fixed point: `I` integer bits (the sign bit counts), `F` fractional
bits, stored as a raw scaled integer of exactly `I + F` bits. Bounds are in
**whole units**.

Fixed point is what you use when a value must be **bit-identical across
machines**. Floating point is not: the same expression can differ by an ulp
between compilers and architectures, which is fatal for lockstep simulation
or any determinism-dependent design. A fixed value is an integer — it adds
exactly, everywhere.

A fixed component is also **its own quantization**: storage and wire share
one integer domain, so there is no separate quantize step to keep in sync.

### ufixed(I, F)

```
distance ufixed(48, 16) | min = 0, max = 60000
```

The unsigned sibling: no sign bit, whole-unit domain `[0, 2^I)`, same `F`
semantics, stored as an **unsigned** integer of exactly `I + F` bits. Use it
when the value cannot be negative and you want the storage type to say so —
unsigned Q8.8 reaches 255 whole units where signed Q8.8 tops out at 127.
Everything else — required whole-unit bounds, exact round trips, defaults,
degenerate ranges — works exactly as for `fixed`.

### Strings and bytes

```
name string(32)
blob bytes(1024)
```

Both are length-prefixed with a maximum. Storage carries the buffer and the
used length beside it:

```cpp
char name[MaxName + 1] = {};
int32_t name_length = 0;
```

The write refuses embedded NULs and any length past the maximum; the read
validates both.

### Arrays

```
fixed_size   [4]Vector3
counted      [..16]uint32
ranged_count [2..8]uint32
```

A fixed array always writes N elements. A counted array writes the count
first, in the fewest bits that can express the bound, then that many
elements. The bound is a range literal: `[..N]` reads "up to N" and is sugar
for `[0..N]`; `[A..B]` is a count in [A, B], encoded relative to A. (A
retired `[<= N]` spelling is refused with `[..N]` named.)

### Composition

Types nest freely. A composed field writes its parts inline — no pointers,
no offsets, no indirection:

```cpp
if ( !WriteVector3( stream, value.position ) )
{
    return false;
}
```

---

## Branches: if / else

```
type ShipCreate
{
    at_rest bool
    if !at_rest
    {
        velocity Vector3
    }
}
```

The condition must be a `bool` field **already written** — the reader has to
know it before it can decide what comes next. Storage holds both sides; the
wire carries only the taken one:

```cpp
write_bool( stream, value.at_rest );
if ( !value.at_rest )
{
    if ( !WriteVector3( stream, value.velocity ) )
    {
        return false;
    }
}
```

**A read zeroes the untaken side.** You never inherit a stale value from a
previous packet through a branch that was not taken this time.

---

## Defaults

```
health int32 = 100 | min = 0, max = 1000
w      fixed(2, 30) = 1.0 | min = -1, max = 1
```

Everything zero-initializes unless a default says otherwise. Defaults are
for values whose *rest state is not zero* — a quaternion's `w`, a health
that starts full.

A fixed default must be **exactly representable** in its format; the
compiler refuses one that would silently round.

---

## The wire

**The wire** is bit-packed and decided at compile time. Nothing on it
identifies fields — both sides know the layout because they were generated
from the same schema. That is what makes it small and fast, and why
versioning is by [protocol id](#the-protocol-id): one id, same-or-refuse,
with no evolution machinery anywhere. schema is deliberately not an
evolution system; data that must survive schema drift wants a different
tool.

Generated storage is **relocatable by construction** — trivially copyable,
standard layout, no pointers — so instances can be memcpy'd, memory-mapped,
shared between processes, or built in parallel across threads and gathered
by concatenation, a pattern offset-based formats cannot express. The corpus
test proves parallel scatter/gather byte-identical to serial.

---

## Reading untrusted data

Generated readers are written on the assumption that the bytes are hostile.
A value outside its declared range is **refused, not clamped** — the read
returns failure and you drop the packet:

```cpp
// NOTE the buffer contract: in C++ and C the ALLOCATION must extend at least
// 8 bytes past the packet data. The reader pulls a 64-bit word at a time, so a
// buffer sized exactly to the packet is read past its end — on every packet,
// not just malformed ones. Size the receive buffer with slack; pass the true
// length.
uint8_t buffer[MaxPacketBytes + 8];
int bytes = recv( ... );

ShipCreate value;
serialize::ReadStream stream( buffer, bytes );
if ( !ReadShipCreate( stream, value ) )
{
    // malformed or hostile — drop it
}
```

The slack requirement differs per language: **C ≥8 bytes, C++ ≥8, Go ≥7,
Rust ≥8, C# none, Dart none, Java none, Elixir none, and JavaScript's flat tier ≥8 past
the payload.** The
per-target columns are normative in [SPEC.md](SPEC.md) §6.3. Write
buffers are a multiple of 8 in every language.

That covers ranges, counts past an array bound, string lengths past their
maximum, enum values outside the declared range, and reads that run past the
end of the buffer.

One precision worth having: an enum read is bounded by the enum's declared
**max**, not by its variant count. For a plain `enum E { A, B, C }` those are
the same thing and a non-variant cannot survive a read. But `enum E | max = 15`
(variants `{ A, B }` on the next line) deliberately reserves headroom so
variants can be added later without
moving the field width — and a read of that enum accepts anything in `[0, 15]`.
That is the point of the headroom, but it means a value you have not defined
yet can arrive, and your `switch` should have a default. The same VALIDATION rules hold in all nine languages, because
the same compiler wrote all nine — the buffer-slack contract above is the one
thing that differs per language.

---

## Writes are the caller's responsibility

**The validation guarantee is on reads.** That is where hostile input arrives,
and it holds in every language.

**Each language uses its own correctness idiom on the write side.** C++ has
`assert`/`NDEBUG`, a check that disappears in a release build, so the C++
backend uses `serialize_assert`. Go has no assert idiom, so it returns
`ErrValueOutOfRange`; C, C#, Rust and JavaScript likewise return failure
rather than invent an assert. Dart has `assert` — active under
`--enable-asserts`, compiled out of release and AOT builds — so the Dart
writer asserts, exactly like C++; Java's `assert` is active under `-ea` and
dormant otherwise, so the Java writer asserts too (one predicate call per
write, issue #156's inline-threshold discipline); the BEAM has no dormant
assert, so the Elixir writer raises `ArgumentError` on a violated contract in
every build. The rule is that a language should verify
correctness the way that language verifies correctness — not that every
target behaves identically here.

So writing `health = 2000` into a field declared `| min = 0, max = 1000`
asserts in a C++, checked-Dart or `-ea` Java build, raises in Elixir, silently
writes the truncated low bits in a C++ release, Dart AOT or default-JVM build,
and returns failure in the others.

Do not build on any of it. **Keep your values inside their declared bounds on
the write side** — your simulation already knows they are, and that is the only
assumption the wire makes. The C++ behaviour in particular is the right one for
a game shipping at 60 Hz, where re-checking every field of every packet in
release is a cost with no buyer.

## The protocol id

The compiler derives a **protocol id** from the schema and exports it. Two
peers at the same id speak identical bits; two peers at different ids should
not talk to each other at all. Check it during your handshake.

There is no version tag on the wire — that is the point. The id is how you
find out, once, at connect time, instead of paying for it on every packet.

---

## Embedding the compiler

The compiler is a Go library as well as a binary. `cmd/schema` is a client of
that library and holds nothing else — the flags, the printing, the exit codes —
so anything the CLI does, your program can do.

```go
import (
	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

c := compiler.New() // the nine built-in targets, registered

paths, err := compiler.GatherPaths([]string{"schemas"}) // one directory is one unit
unit, err := c.Load(paths)                              // format-free: nothing is written

fmt.Printf("package %s, protocol id 0x%016x\n", unit.Package, unit.ProtocolId)

files, err := c.Generate(unit, "cpp", nil)
for name, data := range files {
	os.WriteFile(filepath.Join(outDir, name), data, 0o644)
}
```

`Generate` writes nothing: it returns the emitted files keyed by name, so the
bytes can go to a build directory, an archive, or straight into a comparison.
`Options` carries per-target settings for registered generators; the
built-in targets read none today.

A unit that does not compile comes back as `compiler.Diagnostics`, the whole
error list rather than the first one:

```go
unit, err := c.Load(paths)
if diags, ok := errors.AsType[compiler.Diagnostics](err); ok {
	for _, e := range diags {
		fmt.Fprintln(os.Stderr, e)
	}
}
```

The CLI formats every file in place before reading it (schemafmt, one style, no
options). That is a policy, not a law, and it is two fields:

```go
c.FormatInPlace = true
c.OnFormat = func(path string) { fmt.Printf("formatted %s\n", path) }
```

For formatting on its own there is `compiler.FormatFile(path)` — canonicalize
in place, reporting whether the file actually changed — and `compiler.Format(path,
src)`, which formats bytes and touches nothing, for a drift check that must not
repair what it is measuring.

### Your own generator

A generator is an interface, and the built-in backends have no private path in:
they are registered exactly the way yours is.

```go
// Markdown docs for every type in the unit.
type docs struct{}

func (docs) Names() []string { return []string{"docs"} } // `--lang docs`

func (docs) Generate(u *ir.Unit, _ compiler.Options) (map[string][]byte, error) {
	var b strings.Builder
	names := slices.Sorted(maps.Keys(u.Structs))
	for _, name := range names {
		st := u.Structs[name]
		fmt.Fprintf(&b, "## %s — %d bits max\n", name, ir.MaxBitsStruct(st))
		for _, f := range st.Fields {
			fmt.Fprintf(&b, "- `%s` (%d bits)\n", f.Name, ir.MaxBitsField(f))
		}
	}
	return map[string][]byte{"types.md": []byte(b.String())}, nil
}

c := compiler.New()
if err := c.Register(docs{}); err != nil { // refuses a name a target already holds
	return err
}
fmt.Println(c.Targets()) // [c cpp cs dart docs elixir go java js rust]
files, err := c.Generate(unit, "docs", nil)
```

The unit your generator receives is the same fully-resolved [`ir`](https://pkg.go.dev/github.com/mas-bandwidth/schema/v2/ir)
the built-in backends read: types resolved, constants folded, ranges and bit
widths derived, wire order fixed. The derived parameters the nine emitters
share are functions there rather than per-backend arithmetic — `ir.BitsRequired`,
`ir.MaxBitsStruct`, `ir.MaxBytes`, `ir.CompressedFloatParams`,
`ir.AlignedFixedByteArrays`, `ir.FieldId` — so a new target computes the same
widths as the old ones by construction, not by care.

Constants fold, but the author's spelling survives: every declared bound,
size, and default keeps its source expression on the IR (`Const.Expr`,
`Field.IntMinExpr`/`IntMaxExpr`, `ArrayExpr`, `DefExpr`,
`FieldType.SizeExpr`), and the expression surface renders it, so generated
output can say `MaxObjects - 1` where the resolved field alone could only
say `9999`:

```go
// object_id int32 | min = 0, max = MaxObjects - 1
f := handle.Fields[0]
ir.RenderExpr(f.IntMaxExpr)                        // "MaxObjects - 1" — schema spelling, for comments
ir.RenderExprIdent(f.IntMaxExpr, nil)              // "MaxObjects - 1" — a target that keeps the name
ir.RenderExprIdent(f.IntMaxExpr, ir.RustConstName) // "MAX_OBJECTS - 1" — a SCREAMING_SNAKE target
```

Whether a bound should render symbolically at all is your target's call, made
on two facts: `ir.ExprConsts` lists the constants an expression references
(check each against `unit.Consts` by your target's own rules — the built-in
backends disagree with each other here), and `ir.ExprHasEnumMax` reports an
`E.Max` reference, which has no twin in any generated target — fold those to
the resolved value and keep the schema form in a comment, as the built-in
backends do.

`compiler.Version()` and `compiler.UserAgent()` report which build of the
compiler is running, for a tool that stamps its own output.

The exported surface of `compiler` and `ir` is under semver from the release
that first carries it; everything under `internal/` is not
([VERSIONING.md](VERSIONING.md)).

---

## Per-language notes

**C++** — header-only generated output, targeting
[serialize](https://github.com/mas-bandwidth/serialize). A type can map to
your own math type with `| cpp_native = Vector3, cpp_include = "vec.h"`, so
simulation code does math directly on generated storage.

**C#** — C# 9 / netstandard2.1-clean, so it runs on Unity-class runtimes.
Reads scalars without boxing.

**Dart** — Dart 3, VM and AOT (Flutter release ships AOT — the deployment
the backend is tuned for). Generated code is **self-contained**: it imports
`dart:typed_data` and its sibling generated files, never a runtime package.
The serialize.dart bitpacker is inlined at every field with literal constant
widths and masks, and every type gets monomorphic `write<Name>` /
`read<Name>` / `measure<Name>` / `zero<Name>` functions — no streams, no
dispatch. Buffers are caller-owned `ByteData` views; the writer needs
`<name>MaxBytes` (a multiple of 8), the reader needs **no slack** past the
payload. Reads validate everything and never throw on hostile bytes; writer
contracts are `assert`s that compile out of release builds. Integer widths
through 64 ride bit-transparently in `int`; the 128-bit widths ride an
emitted `Int128`/`UInt128` pair (`Int128.dart`, generated beside a unit that
needs it):

```dart
import 'Wire.dart';

final buf = Uint8List(shipCreateMaxBytes);
final view = ByteData.sublistView(buf);
final bytes = writeShipCreate(ship, view); // bytes written; contracts asserted

final ok = readShipCreate(out, ByteData.sublistView(packet), packet.length * 8);
if (!ok) {
  // malformed or hostile — drop it
}
```

**Go** — accessors avoid allocation; reads and writes run on caller-owned
buffers.

**Elixir** — Elixir 1.20 on Erlang/OTP 29, built to issue #167's measured
directives from the serialize.elixir port. Generated code is
**self-contained**: it defines its own modules and touches nothing beyond the
standard library. Each schema file becomes one module of `defstruct` types
plus a file-scope module of wire functions — `write_<name>(value)` returns
the wire binary, `read_<name>(data, num_bits)` returns the family verdict as
`{:ok, value} | :error` and never raises on hostile bytes,
`measure_<name>(value)` and `zero_<name>()` complete the surface. Every
packing intermediate stays under the BEAM's small-integer boundary (32-bit
groups through a byte-granular scratch on the write side, the port's 40-bit
windows on the read side), and loops are tail-recursive function heads.
Integers of every width — 128-bit included — ride plain BEAM integers;
non-finite float patterns, which no BEAM float term can hold, travel as
`{:nonfinite, bits}`. Validation is always on: there is no compile-out
assert on the BEAM, so writer contracts raise `ArgumentError` in every
build (the O(1) checks only) and readers validate everything:

```elixir
wire = Example.Types.write_ship_create(ship)

case Example.Types.read_ship_create(packet, byte_size(packet) * 8) do
  {:ok, value} -> handle(value)
  :error -> :drop # malformed or hostile
end
```



**Java** — Java 17 on the JVM, built to issue #156's measured directives.
Generated code is **self-contained**: it references only `java.lang`,
`VarHandle` little-endian word access and `java.util.Arrays`, never a runtime
package. Each schema file becomes one public class of the same name (the
protobuf outer-class shape) with the file's types nested inside; every type
gets monomorphic static `write<Name>` / `read<Name>` / `measure<Name>` /
`zero<Name>` functions with the bitpacker inlined at every field, literal
constant widths and masks — no streams, no dispatch (the unified stream
pattern measured ~2x on the JVM). Writer contracts live in one private
`checkWrite<Name>` predicate called through a single `assert` — active under
`-ea`, dormant bytecode otherwise, and small enough that it never counts
against the JIT's inline thresholds. Buffers are caller-owned byte arrays;
the writer needs `<name>MaxBytes` (a multiple of 8), the reader needs **no
slack** past the payload. Integer storage is the same-width signed type,
bit-transparent (the protobuf convention); the 128-bit widths ride an
emitted immutable `Int128`/`UInt128` pair, generated beside a unit that
needs it:

```java
import example.Types;

byte[] buf = new byte[Types.shipCreateMaxBytes];
int bytes = Types.writeShipCreate(ship, buf); // bytes written; contracts asserted under -ea

boolean ok = Types.readShipCreate(out, packet, packet.length * 8);
if (!ok) {
    // malformed or hostile — drop it
}
```

**Rust** — no `unsafe` in generated code, `Result`-returning read and write.

**JavaScript** — ES modules, zero dependencies, Number storage for widths of
32 bits or fewer, BigInt for 64 and 128. Two codecs are generated over the
same classes, selected per call site by import path:

The **flat tier** (`WireFlat.js`, emitted beside every `Wire.js`) is the
production packet path — the fastest correct implementation, which is the one
we use. The serialize.js bitpacker is inlined at every field with constant
widths and masks, zero function calls; it imports nothing, runs on caller-owned
buffers, and measures 8–10x the runtime tier (within a few x of native C):

```js
import { ShipCreate } from "./Wire.js";
import { WriteShipCreateFlat, ReadShipCreateFlat, FLAT_READ_SLACK } from "./WireFlat.js";
import { ShipCreateMaxBytes } from "./Wire.js";

const buf = new Uint8Array(ShipCreateMaxBytes);        // write: MaxBytes suffices
const view = new DataView(buf.buffer);
const bytes = WriteShipCreateFlat(ship, view);          // bytes written, or -1 (checked refusal)

const rbuf = new Uint8Array(bytes + FLAT_READ_SLACK);   // read: 8 bytes of slack past the payload
rbuf.set(buf.subarray(0, bytes));                       // (copy exactly-sized receive buffers in)
const ok = ReadShipCreateFlat(out, new DataView(rbuf.buffer), bytes * 8);
```

Reads validate in every mode; writes fork checked/production once at module
load on `NODE_ENV` (bundlers tree-shake the checked writer out of production
bundles).

The **runtime tier** (`Wire.js` + [serialize.js](https://github.com/mas-bandwidth/serialize.js)
streams) is the diagnostic and reference surface: sticky latched errors that
say which operation failed and why, `MeasureStream`, and per-op checked
granularity. Both tiers emit identical
bytes for identical values — a standing CI gate — so the debugging story is
one import away: re-read a failing buffer through the runtime tier and read
`stream.error`. The flat modules are pure spec'd ECMAScript with no node
APIs.

All nine are generated from the same IR and compared against each other in CI
on every push. The wire is bit-packed, so the property being checked is
bit-identity — if they ever disagree by one bit, the build fails.

---

For the exact rules — grammar, wire law, every edge case — see
[SPEC.md](SPEC.md).
