# Using schema

Every feature of the language, with the code it generates.

The generated code in this guide is real compiler output, not an
approximation — if it looks surprising, it is because that is what the
compiler does.

- [The five minute version](#the-five-minute-version)
- [Declarations](#declarations)
  - [const](#const) · [enum](#enum) · [flags](#flags) · [type](#type) ·
    [message](#message) · [table](#table) · [object](#object)
- [Field types](#field-types)
  - [Integers](#integers) · [Ranged integers](#ranged-integers) ·
    [bits(N)](#bitsn) · [bool](#bool) · [Floats](#floats) ·
    [Compressed floats](#compressed-floats) · [fixed(I, F)](#fixedi-f) ·
    [Strings and bytes](#strings-and-bytes) · [Arrays](#arrays) ·
    [Composition](#composition)
- [Branches: if / else](#branches-if--else)
- [Defaults](#defaults)
- [The two wires](#the-two-wires)
- [Reading untrusted data](#reading-untrusted-data)
- [Packing JSON into binary](#packing-json-into-binary)
- [The protocol id](#the-protocol-id)
- [Per-language notes](#per-language-notes)

---

## The five minute version

Write a `.schema` file:

```
package tour

const MaxHealth = 1000

enum ShipType { Fighter, Corvette, Bomber }

type Vector3 {
    x float64
    y float64
    z float64
}

message ShipCreate {
    ship_type ShipType
    position  Vector3
    health    int32 [min = 0, max = MaxHealth]
}
```

Generate:

```
schema generate --lang cpp --out generated/cpp .
```

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

Run the same command with `--lang cs`, `--lang go` or `--lang rust` and you
get the equivalent in that language — writing the **same bits**.

---

## Declarations

### const

```
const MaxHealth = 1000
const Pi float64 = 3.14159265359
```

Constants are exported into every generated language, so the bound you
declared is the bound your code compares against — no second copy to drift.
They may reference each other in any order, across files.

An untyped constant takes the type its use needs. Name a type explicitly
when you care.

### enum

```
enum ShipType { Fighter, Corvette, Bomber }
```

**Every enum has an implicit `None = 0`.** Variants pack from 1. A
zero-initialized enum field is therefore the null, in band, and you never
need a separate has-flag beside it:

```cpp
enum class ShipType : uint8_t { None = 0, Fighter = 1, Corvette = 2, Bomber = 3 };
```

On the wire it costs `bitsRequired(variant count)` — 2 bits here, for four
values. Declaring `enum E [max = 15] { ... }` reserves headroom so you can
add variants later without moving the field width.

### flags

```
flags Capabilities { Cloak, Shield, Warp }
```

One bit per variant, consumed as a mask. Storage is `uint64` in every
language; the wire is exactly as many bits as there are variants:

```cpp
using Capabilities = uint64_t;
inline constexpr Capabilities Capabilities_Cloak  = 1ull << 0;
inline constexpr Capabilities Capabilities_Shield = 1ull << 1;
inline constexpr Capabilities Capabilities_Warp   = 1ull << 2;
```

### type

A plain struct. Composes into messages, tables and other types:

```
type Vector3 {
    x float64
    y float64
    z float64
}
```

### message

A message is a type that also joins the unit's **dispatch surface**: the
compiler emits a `MessageType` enum, tag read/write functions, and
`WriteMessage`/`ReadMessage` that dispatch on it. That is how you get a
single entry point for a packet stream.

### table

Declaring `table` instead of `type` puts a type on the **table wire** — the
evolution-tolerant encoding for data that outlives builds. See
[The two wires](#the-two-wires).

### object

An object declares state that is replicated in more than one form — a full
state, a deep wire, a lightweight shallow wire, and an interpolation view.
Fields carry `[interpolate]` and `[local]` markers to say which views they
belong to. This is the delta/snapshot machinery; if you are not writing a
replication layer you will not need it.

---

## Field types

### Integers

`int8` `int16` `int32` `int64` `int128` and their `uint` counterparts. Full
width on the wire unless you give a range.

### Ranged integers

```
health int32 [min = 0, max = MaxHealth]
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
throttle float32 [min = 0, max = 1, resolution = 0.01]
```

A float quantized onto a grid: the wire carries the step index, not the
float. 101 steps here, so 7 bits instead of 32. Add `round = up` / `down` /
`nearest` to control the write rounding.

### fixed(I, F)

```
position fixed(48, 16) [min = -30000, max = 30000]
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
counted      [<= 16]uint32
ranged_count [2..8]uint32
```

A fixed array always writes N elements. A counted array writes the count
first, in the fewest bits that can express the bound, then that many
elements.

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
message ShipCreate {
    at_rest bool
    if !at_rest {
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
health int32 [min = 0, max = 1000] = 100
w      fixed(2, 30) [min = -1, max = 1] = 1.0
```

Everything zero-initializes unless a default says otherwise. Defaults are
for values whose *rest state is not zero* — a quaternion's `w`, a health
that starts full.

A fixed default must be **exactly representable** in its format; the
compiler refuses one that would silently round.

---

## The two wires

**The message wire** is bit-packed and decided at compile time. Nothing on
it identifies fields — both sides know the layout because they were
generated from the same schema. That is what makes it small and fast, and
why versioning is by [protocol id](#the-protocol-id).

**The table wire** is for data that outlives builds — config, assets, saved
settings. Fields are identified by name hash, defaults elide, unknown fields
skip, removed fields default, changed types skip rather than misdecode, and
out-of-range values clamp. Every such event is counted in a report you can
inspect. Add or remove a property and older readers keep working.

Declaring `table` puts a type and everything it references on that wire, and
generates:

- **Codecs** — `TableWriteX` / `TableReadX`.
- **Reflection** — `TableTypeX()` field descriptors: names, wire ids,
  bounds, enum value names. Flat arrays, no per-instance weight, no RTTI, no
  `System.Reflection`, and no schema files shipped at runtime.

Table storage is **relocatable by construction** — trivially copyable,
standard layout, no pointers — so instances can be memcpy'd, memory-mapped,
shared between processes, or built in parallel across threads and gathered
by concatenation.

Use the message wire for gameplay; use tables for anything you will still be
reading after three more builds ship.

---

## Reading untrusted data

Generated readers are written on the assumption that the bytes are hostile.
A value outside its declared range is **refused, not clamped** — the read
returns failure and you drop the packet:

```cpp
ShipCreate value;
serialize::ReadStream stream( buffer, bytes );
if ( !ReadShipCreate( stream, value ) )
{
    // malformed or hostile — drop it
}
```

That covers ranges, counts past an array bound, string lengths past their
maximum, enum values that are not variants, and reads that run past the end
of the buffer. The same rules hold in all four languages, because the same
compiler wrote all four.

The one deliberate exception is the table wire, where out-of-range values
*clamp* and are counted in the report — because table data is meant to
survive schema drift, and refusing to load a config over one stale field
would be worse than clamping it.

---

## Packing JSON into binary

`schema pack` compiles directories of JSON into a single binary container,
per a manifest:

```
schema pack PackManifest.json
```

The manifest names the output and the collections that fill it — a single
JSON file for a singleton, or a directory keyed by an enum:

```json
{
  "unit": ".",
  "outputs": [
    {
      "file": "Config.bin",
      "collections": [
        { "type": "GlobalConfig", "file": "Config/Global.json" },
        { "type": "TeamConfig", "dir": "Config/Teams",
          "key_enum": "Team", "key_field": "team" }
      ]
    }
  ]
}
```

Packing validates as it goes: values are checked against the schema's
ranges, a keyed collection missing a variant's file is an error, and so is a
stray file that matches no variant — which catches both a forgotten config
and a stale one.

The result is one file with a schema hash stamped in it, read through the
generated table codecs in any language.

---

## The protocol id

The compiler derives a **protocol id** from the schema and exports it. Two
peers at the same id speak identical bits; two peers at different ids should
not talk to each other at all. Check it during your handshake.

There is no version tag on the wire — that is the point. The id is how you
find out, once, at connect time, instead of paying for it on every packet.

---

## Per-language notes

**C++** — header-only generated output, targeting
[serialize](https://github.com/mas-bandwidth/serialize). A type can map to
your own math type with `[cpp_native = Vector3, cpp_include = "vec.h"]`, so
simulation code does math directly on generated storage.

**C#** — C# 9 / netstandard2.1-clean, so it runs on Unity-class runtimes.
Reads scalars without boxing; `AppendTableX` writers reuse a buffer.

**Go** — `AppendTableX` append-form writers over a reused buffer; accessors
avoid allocation.

**Rust** — no `unsafe` in generated code, `Result`-returning read and write.

All four are generated from the same IR and compared against each other in CI
on every push. The wire is bit-packed, so the property being checked is
bit-identity — if they ever disagree by one bit, the build fails.

---

For the exact rules — grammar, wire law, every edge case — see
[SPEC.md](SPEC.md).
