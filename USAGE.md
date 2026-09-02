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

It covers your `type` declarations and nothing else. Tables never touch it —
edit a table and what moves is the **build version**, which keys cooked
assets and gates no connection (below).

---

## Tables: data that outlives builds

Packets are for the network: exact-match, bit-packed, same protocol id or
refuse. **Tables are for data that outlives the build that wrote it** —
tools→game pipelines, editors that walk properties by name, save games, tool
output, save/load to memory or disk — where the writer and the reader are
routinely DIFFERENT builds and both must cope. Declare one with `table`; the
body grammar is the type body's:

```
table ShipConfig
{
    health      float32 = 100.0
    speed       float32 = 500.0
    armor_level int32 = 1 | min = 0, max = 10
    name        string(32)
}
```

**What it costs.** Tables are less performant than types, and that is the
trade: a type is a raw struct with a bit-packed same-build wire, a FIXED-size
table is a raw struct with a tolerant byte codec around it and runs as close
to its equivalent type as that wire allows, and the variable-length class
(pointers, an arena, a region) pays for what it buys. Allocation follows the
same ladder: types never allocate, a fixed table with no union never
allocates, a fixed table WITH a union may allocate for the arm in a language
that has no native union, and a variable-length table allocates by nature —
in C++ the caller owns it.

A table lives on its own wire — evolution-tolerant TLV, carried by C++ and by
C# (the fixed class; C#'s pointer surface and text form are follow-ons). Field
identity is a hash of the field NAME, so any reader takes any data, both
directions: unknown fields are skipped, absent fields take their declared
defaults, a field whose type changed is skipped rather than misdecoded,
out-of-range values are clamped. Every such event is counted in a report;
only structural damage stops a load. Tables never touch the protocol id —
add, edit or remove one and no packet byte and no id moves.

**An array's BOUND is not part of that identity either.** Resize a bounded
array — a literal, a constant, or an `E.Max + 1` expression that moved when
the enum grew — and files written under the old bound still load: a count
past your bound keeps the bounded prefix and counts `clamped`, a count short
of it leaves your tail at its declared defaults. (`malformed` means something
else: a count the body cannot cover.) The storage struct's size and extent
change with the constant; the table on the wire does not, because identity is
the field name hash and the kind.

**Enum variants and union arms ride by name too.** An enum value on the wire
is the hash of its variant's name and a union body opens with the hash of its
arm's name, so you can insert a variant in the MIDDLE and every stored value
still reads back as itself:

```
enum Grade { Bronze, Gold }          // v1
enum Grade { Bronze, Silver, Gold }  // v2 — every stored Gold still loads Gold
```

A variant a reader has no name for loads as `None` (enum) or empty (union)
and counts as `unknown` — never as its neighbour. There is no `was` for a
variant: renaming one is a new variant, and old data reads as unknown.

**`flags` is the exception: append at the END.** A mask rides as its raw
bits, so a variant's identity is its BIT POSITION. Inserting or reordering
remaps every stored file silently — nothing on the wire says the bits moved:

```
flags Perks { Shielded, Cloaked, Turbo }           // v1: bits 0, 1, 2
flags Perks { Shielded, Hardened, Cloaked, Turbo } // WRONG: stored Cloaked reads Hardened
flags Perks { Shielded, Cloaked, Turbo, Hardened } // RIGHT: appended, nothing moves
```

Removing a variant frees no bit either — retire the name, keep the position.

### Save and load

Generation produces `<Base>Table.h` beside the packet headers — plain byte
code with **no serialize dependency**, includable from any translation unit.
The encode surface is a measure/save split, and the caller owns every buffer
(generated code allocates nothing):

```cpp
#include "ConfigTable.h"

ShipConfig ship;
ship.health = 250.0f;

int64_t size = ShipConfigMeasure( ship );     // exact, writes nothing
std::vector<uint8_t> buffer( size );               // or any storage you own
ShipConfigSave( ship, buffer.data(), size );  // returns size — a buffer
// of exactly Measure's answer always suffices; -1 means the buffer is
// too small, or the value violates a storage bound (a _count or _length
// outside its declared range, or an enum value or union tag no variant
// names — measure returns -1 for those too)

TableReport report;
ShipConfig loaded;
if ( !ShipConfigLoad( loaded, buffer.data(), size, &report ) )
{
    // framing damage: report.malformed is set, the good prefix is kept
}
if ( report.unknown || report.kind_mismatch || report.clamped )
{
    // the data came from a different schema generation — loaded is still
    // fully usable; log the counts so drift is visible
}
```

Values at their defaults stay off the wire entirely — an all-default table
saves as 2 bytes and loads back complete.

The C# surface is the same three functions, name first, on the unit's `Schema`
class, over spans the caller owns. Storage is a sealed class with public
fields — every buffer allocated at construction, so `Load` allocates nothing
and overlays in place after restoring the declared defaults:

```csharp
using Example;

ShipConfig ship = new ShipConfig();
ship.Health = 250.0f;
ship.SettingsPresent = true;          // ?T: presence decides whether it rides

long size = Schema.ShipConfigMeasure(ship);       // exact, writes nothing
byte[] buffer = new byte[size];                    // or any storage you own
Schema.ShipConfigSave(ship, buffer);               // returns size, or -1

TableReport report = new TableReport();
ShipConfig loaded = new ShipConfig();
if (!Schema.ShipConfigLoad(loaded, buffer, report))
{
    // framing damage: report.Malformed is set, the good prefix is kept
}
if (report.Unknown != 0 || report.KindMismatch != 0 || report.Clamped != 0)
{
    // the data came from a different schema generation — loaded is still
    // fully usable; log the counts so drift is visible
}
```

The bytes are the same bytes: a shared golden corpus pins C++'s encoding of a
set of instances and the C# leg byte-compares its own `Save` against it, then
loads those very files. `string(N)` and `bytes(N)` are a `byte[N]` beside an
`int` used length, arrays a `T[N]` beside an `int` used count, `?T` a value
beside a `<Name>Present` bool, and a union is its tag beside one pre-allocated
arm — the same spelling the packet backend uses, because a table's closure
decodes into the packet backend's own classes.

An enum-keyed array is a `TableKeyed<T>` holding `E.Max + 1` slots. **C#
indexes it by the slot index**, because the language has no non-boxing generic
enum-to-int conversion — so the caller writes the cast,
`fleet.Ships[(int)ShipType.Bomber]`. The `None` refusal survives as a runtime
guard on that indexer, and unlike C++'s `assert` it is **not** compiled out in
release. Generated code walks `.Slots` directly and never pays for the guard.

`foreach` walks the valid slots, `1 .. E.Max`, and yields the slot index beside
the element — the same currency the indexer takes, so a site that wants the key
as its enum writes `(ShipType)ship_type` there. The enumerator is a struct, so
the walk allocates nothing:

```csharp
foreach (var (ship_type, ship) in fleet.Ships)
{
    ship.Health *= 2.0f;   // slot 0 is not in the range
}
```

**The entry's element is a value, so what the loop can WRITE depends on the
element type.** A class element — a nested table, which is the common case — is
the live instance, and mutating it through the iteration is visible. A scalar
or enum element is a copy: **C# iteration reads those, and the indexer writes
them**, `fleet.Thresholds[(int)Difficulty.Hard] = 3`. C++ yields a reference
either way; the difference is C#'s, and a ref-yielding enumerator is a
follow-on rather than part of this construct.

`<Name>TableType()` returns the reflection descriptor: field names, wire ids
and kinds, bounds, ranges, guards, `Optional`, the enum/union vocabulary, and
an enum-keyed array's `KeyTypeName`/`KeyName`/`KeyId` — where `KeyId(0)` is
`0`, the reserved id that marks slot 0 as `None`'s and never valid.

**Which makes a default part of the wire contract.** An absent field means
"the reader's declared default", so changing a default changes what every
file already written says. A v1 writer elided `damage = 21.0`; a v2 reader
declaring `damage = 25.0` loads 25.0 from that same file, and no report event
fires — nothing was lost or skipped. A default change is a semantic edit to
every stored file, and `was` does not cover it: `was` preserves an identity,
not a value. Change a default the way you would change data, or add a new
field and leave the old one alone.

### Nesting: a root table IS a format

A field whose type is another table nests it by value; bounded arrays of
tables give you collections. That is the whole recipe for a config or asset
bin — declare the root, and the format falls out (the same bytes work as a
file on disk or a message on a socket):

```
table WeaponConfig
{
    damage float32 = 21.0
    homing bool
}

table GameConfig
{
    friendly_fire bool
    weapons       [..MaxWeapons]WeaponConfig
    level_name    string(64)
}
```

Whatever envelope you want around it — a magic, a content hash, several roots
in one file — is a few lines of your code on top of `<Name>Save`/`<Name>Load`.
schema deliberately does not prescribe one. Tables may also reference plain
`type`s, enums and flags; everything a table reaches gets table codecs too.
A `type` cannot reference a table (packets stay exact-match), and a table
cannot nest itself.

### Optional fields: `settings ?GunnerSettings`

A `?` before the type makes a field PRESENT or ABSENT. Storage is the value
plus a generated `<field>_present` bool, so the table stays a fixed-size
struct — no pointer, no allocation:

```
table ShipConfig
{
    health   float32 = 100.0
    settings ?GunnerSettings
    tier     ?int32            // scalars too
}
```

```cpp
ShipConfig ship;
ship.settings_present = true;          // absent until you say so
ship.settings.sensitivity = 0.4f;
```

**Presence decides whether it rides, not content.** An absent optional is not
written; a present one always is, even at its defaults — so "absent" and
"present with nothing to say" stay two different values, and a reader that
sees the field sets `_present` whatever it contains.

The framing is the ordinary field's, which makes `?T`, `*T` and a plain `T`
nesting **three spellings of one field**: for any value that is not entirely
default, move a field among the three and no byte changes, in either
direction. The one difference is at the empty end — a plain `T` holding
nothing but defaults writes nothing, while a present `?T` and a non-null `*T`
write a body — and no direction misdecodes: an absent field reads as the
declared default, as null, or as absent, each of which is right.

`?` goes on nested tables and types, enums, flags and scalars. It is refused
on a pointer (already optional), a union (`None` is its absence), and on
arrays, strings and `bytes` — those already carry a count or length whose
zero is emptiness; wrap one in a table and make that optional. An optional
takes no specified default: presence is its only default.

### Enum-keyed arrays: `ships [ShipType]ShipConfig`

An array bound that names a declared ENUM gives you exactly one slot per
variant, indexed by the variant:

```
enum ShipType { Fighter, Bomber, Scout }

table Fleet
{
    ships [ShipType]ShipConfig   // one slot per variant, indexed by the variant
}
```

```cpp
Fleet fleet;

for ( auto [ ship_type, ship ] : fleet.ships )   // every VALID slot, 1..Max
{
    ship.health = DefaultHealth( ship_type );    // the element is a reference
}

ShipType key = TypeFromConfig();                 // keys are runtime values
fleet.ships[key].health = 400.0f;                // asserts key != None
```

Storage is a generated keyed-array type wrapping a plain `ShipConfig[ShipType.Max
+ 1]` — no count companion, because every slot exists — so the memory is the
array you would have written by hand, with the accessor and the iteration on
top of it. **Slot 0 exists and is never valid**: `None` is the enum's
null, so it keys nothing and only `ShipType.Max` slots ever hold data. The
slot is kept so indexing stays unbiased, and reaching it is an error — an
assert on the accessor, and **not in the iteration's range at all**.

**Iterate, and the slot rule never reaches your code.** Keys in a
data-driven program are runtime values — an enum read out of a file, a key a
tool hands you — so `operator[]` is the accessor every call site uses, and its
assert is a debug guard that `NDEBUG` compiles out. A shipped build carries no
check on a keyed index, which is why iteration, not the assert, is where the
safety lives: the range is `1 .. Max`, so a consumer of the whole array writes
no lower bound, no cast and no `Max` of its own, and cannot reach `None`'s
slot by accident. Iteration is const-correct, and a const keyed array yields
const elements.

The entry is a **proxy handed out by value** — a key beside a reference — so
the spelling is `for ( auto [ key, element ] : keyed )`. `auto & [ ... ]` is a
compile error by design: a non-const lvalue reference cannot bind to the proxy.
Write `auto [ ... ]`, or `auto && [ ... ]` if you want the reference form; the
element is a reference to the slot in every case. The iterators carry the
`iterator_traits` typedefs, so `std::distance` and a forward pass work.

**What changes is the wire: the slots ride by NAME.** Each
present slot carries its variant's id, so inserting a variant in the middle,
removing one, or reordering them leaves every surviving slot in its own home:

```
enum ShipType { Fighter, Bomber, Scout }           // v1
enum ShipType { Fighter, Heavy, Bomber, Scout }    // v2 — every stored Bomber
                                                   // slot still loads as Bomber
```

Under the positional spelling every slot after the insert would shift one
place, silently. A slot the writer left at its default is elided; a slot this
reader has no name for is skipped and counted `unknown`; a slot the writer
never sent keeps its declared default. A `None` key never rides at all.

In a `type` body the same spelling is exactly `[E.Max + 1]T` — positional and
bitpacked, the packet wire as always, with the same protocol id either way —
and there the storage is a **plain array**: `per_team [Team]int32` in a `type`
is `int32_t per_team[4]`, no accessor, no iteration surface and no `None`
guard, because there is no key to check. Only the table wire keys the slots.

A key enum counts as part of the table closure: it rides by variant name, so
`| max` headroom and colliding variant names are refused for it too, with the
diagnostic naming the field that keys on it.

**On the TABLE wire the two spellings are different encodings**, and changing
a table field from one to the other is a wire break, not a refactor: the keyed
body has its own wire kind, so an old file read under the new spelling is
reported as a kind mismatch rather than decoded with keys taken for values.
A committed baseline refuses the edit outright.

### Messages: a union whose arms are tables

Inside a table closure a union's arm may name a `table`, which is what makes
a tool message set evolve safely:

```
table OpenDocument  { path string(256), line uint32 }
table SaveDocument  { path string(256), force bool   }

union ToolBody
{
    open OpenDocument
    save SaveDocument
}

table ToolMessage
{
    sequence uint32
    body     ToolBody
}
```

Arms ride under their name hash, so adding a message is adding an arm; a
reader that lacks it reads the union empty, skips the body by its length and
counts `unknown`; arms may be removed and reordered freely. Unlike a
`type`-only payload, a message may carry a nested table or a whole
collection. A union declared for the packet wire still takes `type` payloads
only — types stay value semantics.

### Pointers: `next *Node`

Types are value semantics. Tables ALLOW pointer semantics, because a generic
system needs pointers to tables:

```
table Node
{
    value int32
    next  *Node          // recursion through a pointer is legal
}

table Scene
{
    head    *Node
    palette *Palette     // shared and large: one copy, pointed at
}
```

A pointer targets a `table`, never a `type` — and lives in a table body,
never a type body. Arrays of pointers, and a specified default on a pointer,
are refused by name.

**Do not reach for a pointer to make a field optional.** Every field on this
wire is already optional — absence is the reader's default — and a section
that is present or absent as a unit is spelled `?T`:

```
table Scene
{
    settings ?Settings   // off the wire entirely when it is absent
}
```

No pointer, no allocation, and the table stays fixed-size. Use `*` when the
structure needs it: recursion (a chain, a tree), one large subtree you would
rather not carry by value, or a node several parents name. One pointer
anywhere in a table's by-value closure turns it variable-length, and that
changes the whole build-and-load lifecycle below.

**The compiler derives the mode; you never declare it.** A table with no
pointer anywhere in its by-value closure is FIXED-SIZE — a plain struct with
the three functions above, and it pays nothing at all for what follows. A
table with a pointer in that closure is VARIABLE-LENGTH, and it is never held
by value: you build it, lock it, and read it through a root pointer.

```cpp
SceneBuilder builder;                       // MUTABLE
Scene * root = builder.GetRoot();
Node * first = builder.Alloc<Node>();
first->value = 10;
root->head = builder.Alloc<Node>();         // the slot is both node and reference

builder.Lock();                             // ONE WAY, and it compacts
const Scene * scene = builder.AsConst();      // CONST: one packed region
const Node * head = NodeAt( scene->head );  // one add; NULL when null
```

`Lock()` walks the arena, measures it exactly and lays every node back to
back into one region with the root at its base — zero slack, references
rewritten self-relative, so the whole region relocates by plain `memcpy`.
There is no unlock: to edit again, load the const form into a fresh builder.

Building goes wide with no lock and no per-node atomic — one worker per
thread, each allocating on its own front:

```cpp
SceneBuilder builder;
std::thread workers[4];
for ( int t = 0; t < 4; t++ )
    workers[t] = std::thread( [&]{
        TableWorker worker = builder.Worker();     // one per THREAD
        Node * n = worker.Alloc<Node>();           // no lock, no atomic per node
        ...
    } );
for ( auto & w : workers ) w.join();               // join, THEN lock
builder.Lock();
```

Allocating on your own worker is safe concurrently. Writing fields of a node
another worker allocated is your own synchronization. `Lock`, `Save`, `Cook`
and `Open` are single-threaded. The reflection descriptors are immutable
constant data, so reading them needs no synchronization at all.

A `<Name>Builder` is about 8 KB (it carries the arena's segment table inline),
which is fine on the stack and fine as a member; it is not something to put in
an array of thousands.

Reading from the wire, the caller owns the allocation as always:

```cpp
int64_t need = SceneLoadMeasure( wire, wire_size ); // exact, reads no values
uint8_t * region = your_allocator( need );
TableReport report;
const Scene * scene = SceneLoad( region, need, wire, wire_size, &report );
```

On the wire a pointer rides as its pointee's table body — framing identical
to a by-value nesting AND to an optional (`?T`), so a field may change among
the three and no byte moves for any non-default value (at the empty end they
differ: a by-value `T` at its defaults elides, a non-null pointer does not).
Null pointers are simply absent. **Wire v1 is a tree**: two pointers to one
node write two bodies and load as two nodes.

### Byte buffers: `data *bytes`

`bytes(N)` is N bytes of inline storage in every instance. `*bytes` is a
pointer to a buffer that has no bound and takes exactly the size you give it
— which is how an image or an asset lives inside a table:

```
table Asset
{
    format AssetFormat
    data   *bytes        // sized per node, not per declaration
    label  *string       // the sibling
}
```

```cpp
Asset * asset = builder.Alloc<Asset>();
asset->data = builder.AllocBytes( png_bytes );
memcpy( BytesAt( asset->data ), png, png_bytes );
```

In a million-node table a `bytes(65536)` field costs 64 KB a node whether it
is used or not; a `*bytes` costs the reference plus what each node holds.
Like any pointer it makes the holder variable-length. On the wire it is
framed exactly as `bytes(N)` is, so a field that outgrows its inline bound
moves to a blob and no byte changes for any non-empty value — the same empty-end
asymmetry as above: an empty `bytes(N)` elides, a non-null zero-length `*bytes`
writes an empty payload.

Two sentences that go together: **a tolerant wire load COPIES the blob** — a
gigabyte on the wire path is a gigabyte read — and **the cooked path is the
zero-copy one**, where a pointer into a mapped file IS the asset.

### The block form: rows another language points at

*Specified, not yet implemented — no backend emits the Block files yet
(SPEC-TABLES.md §2.7, §19).*

Some data is not a file and not a message. It is a block you rebuild every
frame and hand to something in another language, which points at it and reads
it in place. You declare nothing for it:

```
table RenderFrame
{
    version uint64
    cameras [..1]RenderCamera
    ships   [..MaxShips]RenderShip
    lasers  [..MaxLasers]RenderLaser
}
```

**Nothing there is new.** Those are ordinary bounded arrays of ordinary fixed
tables, and `RenderFrame` is an ordinary table — it still has `Measure`,
`Save` and `Load` over the tolerant wire, and still cooks. **Every fixed table
also gets a third *form* of the same declaration**: one in which the table's
own bounded arrays sit out of line at a fixed pitch, and the instance at the
front of the block carries, per array, where its rows start, how many there
are, and how far apart they sit. The other side reads those three facts and
points.

You reach for it by INCLUDING it. The form is generated on the side, in
`<Base>Block.h` / `<Base>Block.cpp` beside the unit's `<Base>Table.h`, and in
`<Base>Block.cs` for C# — so a project that never blocks a table compiles
none of it, and the table headers are the same bytes either way:

```cpp
#include "RenderTable.h"                      // the ordinary table surface
#include "RenderBlock.h"                      // the block form, only if you use it
```

```
c++ -c RenderBlock.cpp                        // and link it, once, if you did
```

Which arrays move out of line is the one rule to know: **the table's own
`[..N]` arrays whose element is a struct, and nothing else** — a fixed `[N]T`,
an enum-keyed `[E]T`, a bounded array of scalars, and any array inside a row
all stay where they are. A row's pitch is its `sizeof`. A variable-length
table has no block form at all: a pointer means no fixed pitch.

**Building it goes wide, and that is required, not merely allowed.** Declare
the counts, lay the block out once, then let N workers fill disjoint ranges:

```cpp
RenderFrameBlockStorage storage;              // max-sized allocation, made once

RenderFrameCounts counts = {};
counts.ships = numShips;                      // clamp to the maximum yourself

RenderFrameBlock block;
if ( !RenderFrameBlockBegin( block, storage, counts ) )   // names the array, count and maximum
    return;

RenderShip * ships = RenderFrameShips( block );   // the array's typed base

// worker w handles [begin, end) — no lock, no atomic, no allocation
for ( int i = begin; i < end; i++ )
    ships[i].position = ...;

int64_t bytes = RenderFrameBlockBytes( block );   // the used extent, for the handoff
```

A block stays valid until the next `Begin` on that storage, which invalidates
every row pointer taken from it. Double-buffering is two storages, and it is
yours to own.

**Reading it is one check and then pointers:**

```csharp
if ( !RenderFrame.BlockOpen( out RenderFrameBlock block, pointer, bytes ) )
    return;

foreach ( ref readonly RenderShip ship in block.Ships )
    Draw( ship );
```

`BlockOpen` verifies the magic, the byte order, the build version and the
extent, and then you index. A generic consumer does not even need the generated
struct: the reflection descriptors carry the projection's layout, so a tool
can walk any block's rows without a type per table.

**Three things to know before you reach for one.**

- **It is a same-build contract.** Both sides are generated from one
  declaration and ship together. Every row size and field offset is asserted
  by generated code in both languages, so a field that moves is a build
  error, not a garbled frame.
- **Every schema edit is a regenerate.** Append a field to the table, append
  one to a row, raise a maximum — each moves the build version, `BlockOpen`
  refuses bytes from the older build, and both sides rebuild from the one
  declaration. That is the trade for having no version machinery in the hot
  path: a block is same-build, so there is nothing to absorb and nothing to
  ask for by name. If you want data that outlives the build that wrote it,
  use the wire — which this same table still has.
- **The allocation is the maximum, once.** `BlockMaxBytes` sums every array's
  declared maximum; the block is allocated once at that size and never grown.
  The bytes you hand off are only the frame's.

### The cooked form: point at a file instead of parsing it

The wire is generic — it allocates, walks and parses, and any build reads any
data. When you want a big file to start instantly, cook it: the locked region
written verbatim behind a small header, laid out exactly as the runtime reads
it.

```cpp
int64_t size = SceneCookMeasure( builder );
SceneCook( builder, buffer, size );                  // write Scene.bin

// later, in the game — mmap it or read it, then just point:
const Scene * scene = SceneOpen( bytes, size );
if ( scene == NULL )
{
    // wrong build, corrupt, or foreign byte order: fall back to the wire
}
```

A cooked file is an ACCELERATOR, not an archive: it is build-locked by a
build version that covers the schema's layout, its meaning facts and your
target's byte order, so it refuses the moment any of it moves and you
regenerate it. The tolerant wire stays the format of record.

`Open` checks the header and points — the magic, the byte order it
establishes, the build version, the two part lengths, the base's alignment —
and that is the whole of it. On a match the bytes ARE what this build wrote,
so there is nothing to validate and nothing to fix up. It is the only runtime
entry point there is: a cook is your build's own accelerator, and a file that
is not your build's returns NULL and you load the wire.

If you have a cooked file whose provenance you doubt — one that crossed a
machine boundary, or one you are diagnosing — check it with the tool:
`schema cook-check` — *specified, not yet built* — scans the attribution the
cook carries beside its data and verifies every reference, buffer and count
against it, without following one reference or decoding one value. That is a
person's decision, made once, not a flag on a load in the hot path.

Value-only tables get no `Cook`/`Open` of their own: they are structs, and
`sizeof` plus `memcpy` already is their region form.

### The build version: what a cooked asset is stored under

*Specified, not yet implemented — no backend emits this constant and
`schema build-version` does not exist yet (SPEC-TABLES.md §20).*

A cook is only ever produced for one build, so something has to name which
build. That is the **build version**: one digest over everything a cook's
bytes depend on — your protocol id, every record's layout as the compiler
computes it, and the declaration facts that decide what a load puts in a slot
(a specified default, a declared range, an enum's variant order, a union's arm
order) — plus the target's byte order.

```
$ schema build-version tables/examples/
package tabledemo
build version 0x................
```

```cpp
// generated, beside ProtocolId
printf( "%016llx\n", (unsigned long long) tabledemo::BuildVersion );
```

Your tools cook asset X to build version Y and write `(X, Y)` into the store;
your game asks the store for `(X, Y)`. That is the whole protocol. You never
have to reason about which edits invalidate what — anything that would change
a cook's bytes moves Y, so the key moves with it, and a new Y is simply a new
cook the build cache absorbs. The asset hash is the hash of the WIRE file you
cooked from.

It is settled by the **compiler**, not by your C++ compiler, which is what
lets tooling cook before any game binary exists. The layout half comes from
the compiler's own C ABI model, and the generated code asserts it on every
side: if your compiler lays a record out differently, the build fails and
names the field.

Two things it does not do:

- **It never gates a connection.** Peers connect on the protocol id, and two
  peers at the same protocol id may run different build versions all day —
  their tables ride the tolerant wire between them, and each side loads its
  own cooked assets out of its own store.
- **It never moves your protocol id.** Edit a table and only the build version
  moves; edit a `type` and both move. That is the same independence the tables
  page opens with, seen from the cook's end.

Two ids, and there is no third: the protocol id for the type wire, and the
build version for everything cooked or blocked.

### Renaming a field: `was`

Wire identity is the name hash, so a rename would orphan stored data. `was`
keeps the old identity through the rename:

```
table ShipConfig
{
    speed float32 = 500.0 | was = "velocity"
}
```

Old files that carry `velocity` load into `speed`; new files keep writing the
old id. The compiler refuses `was` naming the field's own current name, and
refuses any two fields of one table whose effective ids collide.

### The tables baseline: catching the edits the wire cannot report

Think of a save game. A player's file was written two years ago by a build
nobody has any more, and today's build has to read it. Almost every schema
edit since is safe by construction — fields came and went, an enum grew,
bounds moved — and the wire reports whatever it cannot use. **Exactly two
edits are different**: they change what an OLD file MEANS, and nothing on the
wire can tell you.

```
table ShipConfig
{
    damage float32 = 21.0    // v1 elided this: absence MEANS 21.0
}

table ShipConfig
{
    damage float32 = 25.0    // v2: every stored save now reads 25.0 out of
}                            // the same bytes, and no report event fires
```

`flags` has the same shape of hazard one level down: bits carry no names, so
inserting a variant silently remaps every stored mask.

Commit a baseline and the compiler remembers for you:

```
$ schema tables-baseline --update --reason "first baseline before 1.0 ships" configs/
$ ls configs/
ShipConfig.schema   tables.baseline
```

`tables.baseline` is a text projection of the unit's whole table closure —
one fact per line, made to be read in a diff — and from then on `schema
check`, and so `schema generate`, diffs against it:

```
$ schema check configs/
configs/tables.baseline: ShipConfig.damage: specified default 21.0 -> 25.0 — this
edit changes what data already written MEANS, and no reader can report it; if you
mean it, record it: schema tables-baseline --update --reason "..." (SPEC-TABLES.md §18)
schema: 1 error(s)
```

Sometimes you mean it. The override is one command and it is never silent:

```
$ schema tables-baseline --update --reason "damage rebalanced in 2.0; saves from 1.x read the new value" configs/
```

That appends to the file's own history — which is what someone reads years
later when an old save loads wrong:

```
## history
### 2026-09-02 — first baseline before 1.0 ships
- baseline created over 1 table — data written BEFORE this point is not covered by it

### 2026-11-14 — damage rebalanced in 2.0; saves from 1.x read the new value
- ShipConfig.damage: specified default 21.0 -> 25.0 [refuse]
```

`--update` without `--reason` is refused: an intentional break gets declared
or it does not happen.

**What refuses, what warns, what passes.** Refused: a specified default
changed, added or removed; a flags variant inserted, removed, reordered or
renamed in place; a field's wire kind or an array's ELEMENT kind changed; an
array changed between the keyed and positional spellings, or a keyed array's
key enum swapped; a field pointed at a different enum, flags, union or table
that cannot stand in for the old one — or at none at all, like an enum-typed
field respelled as its raw `uint16`, which the runtime cannot report because
both ride as kind 7. **"Stand in" means every identity survives AND, for a
table, the facts under the shared field ids are unchanged**: a twin
declaration carrying the same id under a different default is refused,
because every stored body's elided value would quietly change meaning.
Warned, because the read report already counts what is lost: a bound or a
string capacity shrunk, an enum variant or a union arm removed, a declaration
renamed or otherwise no longer covered. Passed in silence, because the wire
absorbs it: fields added, removed, reordered or renamed under `was`; variants
added anywhere; flags variants APPENDED; bounds grown.

Renaming a declaration never raises a verdict of its own: it warns, saying
which declaration carries the old one's contents on, and the contents keep
being judged by their own walk — so a dropped variant draws the same warning
whether or not its enum was renamed in the same edit.

The whole thing is opt-in — no `tables.baseline`, no check — and the first
one you write covers only what comes after it: data written before it existed
was written against a shape nobody recorded.

### Going wide

Every nested table on the wire is length-prefixed, so building a large file
parallelizes: measure the subtables on N workers, prefix-sum the offsets,
and scatter-write disjoint ranges — no worker touches another's bytes.

```cpp
// one worker per profile; shown serially
int64_t sizes[kProfiles], total = 0;
for ( int i = 0; i < kProfiles; i++ )
{
    sizes[i] = ProfileConfigMeasure( profiles[i] ); // parallel-safe: read-only
    total += sizes[i];
}
uint8_t * buffer = arena_alloc( total );
int64_t offsets[kProfiles], at = 0;
for ( int i = 0; i < kProfiles; i++ ) { offsets[i] = at; at += sizes[i]; }
for ( int i = 0; i < kProfiles; i++ ) // each iteration is an independent job
{
    ProfileConfigSave( profiles[i], buffer + offsets[i], sizes[i] );
}
```

The same framing means a reader CAN fan nested decodes out to workers — each
length-prefixed body is a self-contained decode.

### Walking fields at runtime

Every closure type gets a reflection descriptor — name, wire id and kind,
storage offset, array bounds and count companions, declared ranges, branch
guards, and for an enum or union field the whole vocabulary: `[0, enum_max]`
indexed, `enum_name` for each name and `variant_id` for the wire id it rides
under. An optional field carries `optional` and `present_offset`, the
presence companion's offset, so a walker can read and set presence; an
enum-keyed array carries `key_type_name`, `key_name` and `key_id`, so it can
print `ships[Bomber]` instead of `ships[2]`. Enough to write a generic
editor, printer or differ with no RTTI and no schema files at runtime:

```cpp
const TableTypeInfo * type = ShipConfigTableType();
for ( int32_t i = 0; i < type->num_fields; i++ )
{
    const TableFieldInfo & field = type->fields[i];
    printf( "%s %s @%u", field.name, field.type_name, field.offset );
    if ( field.has_range )
        printf( "  [%g, %g]", field.range_min, field.range_max );
    for ( int64_t v = 0; field.enum_name && v <= field.enum_max; v++ )
        printf( "  %s=0x%04x", field.enum_name( v ), field.variant_id( v ) );
    if ( field.table )
        printf( "  -> %s", field.table->name );   // recurse for nested tables
    printf( "\n" );
}
```

Decoded tables are **relocatable**: trivially copyable, standard-layout,
pointer-free, arrays inline with their `_count`/`_length` companions — so a
value can be memcpy'd, mmap'd or shared across processes and still walked
through descriptor offsets. Generated `static_assert`s enforce it.

Tables are generated for `--lang cpp` only today; every other target refuses
a unit that declares them, by name. What stays off the table wire: `fixed`,
`int128`/`uint128` (no neutral table kind), and `const`/`reserved`/`align`
(bit-position constructs — the table wire has no bit positions). Extents have
no wire ceiling: string and bytes byte lengths and array counts ride in
uint32, so the only limit is the language's own int32 storage cap.

### Inspecting a whole build: the view

*Specified, not yet implemented — no backend emits the view file yet
(SPEC-TABLES.md §8). The per-table descriptors above are live today.*

The descriptors above answer "what is in this table". The **view** answers
"what is in this build" — every declaration the schema made, walkable by a
tool that has the generated code and no schema files at all. Nothing asks
for it: every unit generates one more pair of files beside the rest,

```
generated/TabledemoView.h
generated/TabledemoView.cpp
```

named for the unit's package, one pair for the whole unit however many
schema files it has, holding a registry of every type, table, enum, flags,
union and constant the schema declared. **You pay for it by compiling it.**
An editor, an inspector or a debug build includes the header and compiles
the source; a game does neither and carries none of it. There is no flag and
no marker on a declaration — a tool that inspects a build wants everything
in it, and the one declaration it needs is the one nobody would have marked.

```cpp
#include "TabledemoView.h"

const UnitViewInfo * unit = UnitView();
printf( "package %s  protocol 0x%016llx\n",
        unit->package, (unsigned long long) unit->protocol_id );

for ( int32_t i = 0; i < unit->num_constants; i++ )
{
    const ViewConstant & c = unit->constants[i];
    if ( c.is_float ) printf( "const %s %s = %g\n", c.type_name, c.name, c.float_value );
    else              printf( "const %s %s = %lld\n", c.type_name, c.name, (long long) c.int_value );
}

for ( int32_t i = 0; i < unit->num_enums; i++ )    // flags and unions walk the same way
{
    const ViewVocabulary & e = unit->enums[i];
    printf( "enum %s (max %lld)\n", e.name, (long long) e.max );
    for ( int32_t v = 0; v < e.num_variants; v++ ) // row 0 is None, then declared order
        printf( "    %llu %s  id 0x%04x\n",
                (unsigned long long) e.variants[v].value, e.variants[v].name, e.variants[v].id );
}

for ( int32_t i = 0; i < unit->num_tables; i++ )   // then unit->types, the same shape
{
    const ViewType & entry = unit->tables[i];
    printf( "table %s  (%s)\n", entry.name, entry.file );
    const TableTypeInfo * type = entry.type;
    for ( int32_t f = 0; f < type->num_fields; f++ )
    {
        const TableFieldInfo & field = type->fields[f];
        printf( "    %s %s", field.type_name, field.name );
        if ( field.is_array )  printf( "[%d]", field.array_bound );
        if ( field.optional )  printf( " ?" );
        if ( field.has_range ) printf( "  [%g, %g]", field.range_min, field.range_max );
        if ( field.table )     printf( "  -> %s", field.table->name ); // recurse
        printf( "\n" );
    }
}
```

`unit->tables` is every table in the unit and `unit->types` every type —
both complete, and every entry points at the same `TableTypeInfo` the
section above walks, so one printer serves both. C# is the same registry
through `Schema.UnitView()`.

Each set is ordered by declaration name, so a listing does not churn when a
file is renamed or a declaration moves between files.

What the view does not carry: descriptions, UI hints, semantics for a type
tag (the tags are listed; their meaning is yours), and the packet wire's bit
layout. And one field kind is listed but not decodable: `fixed`, `ufixed`,
`int128` and `uint128` have no table-wire kind, so they arrive with
`kind == 0` — name, declared spelling, offset and size, enough to show the
field and not enough to read or write its value generically.

### The text form: JSON in and out of one table

The same descriptors drive a JSON text form, so every table reads from and
writes to text with no per-table codec:

```cpp
#include "ShipTable.h"

TableReport report;
ShipConfig ship;
ShipConfigFromJson( ship, text, text_bytes, &report );   // fills ONE instance

int64_t size = ShipConfigToJsonMeasure( ship );          // exact, writes nothing
ShipConfigToJson( ship, buffer, size );
```

The header DECLARES these; the walk itself lives in the generated
`ShipTable.cpp`, so add it to your build once:

```
c++ -c ShipTable.cpp
```

That is the whole cost of the text form, and it is opt-in: a project that
never reads or writes a text does not compile that file, and including
`ShipTable.h` for the wire codecs or the descriptors carries none of it.

`FromJson` places what the text mentions and leaves the rest at its declared
defaults, exactly as an absent field on the wire does; unknown keys, wrong
JSON types and out-of-range numbers land in the same report the wire uses,
plus one counter the wire never raises — `duplicate`, for a key the text gave
twice. `ToJson` writes every field, defaults included — a text is for people —
and it is pretty-printed: one entry per line, two-space indent.

The mapping is the obvious one: enums and flags by variant NAME, a union as
an object with one key, an enum-keyed array as an object keyed by variant
name, a `?T` optional present exactly when its key is present, `*bytes` as
base64. **JSON has one number type**, so `2`, `2.0` and `1e3` all read into an
integer field; a genuinely fractional value there is a kind mismatch, and a
token that is not a JSON number at all — `1-2`, `1.2.3` — is malformed rather
than quietly clamped. Trailing commas are accepted on read and never written.
A field can meet an existing text with `| json = "type"`, which changes no
byte on the wire.

**The writer refuses rather than lie**: `ToJson` returns -1 for an enum value
or a set flags bit no variant names, an invalid union tag, or a non-finite
float. The text it does emit is always valid JSON and valid UTF-8.

### Packing a directory into a table

`schema pack` builds ONE table instance from a directory tree that mirrors
the root table, and writes the root's wire bytes:

```
$ schema pack --root Config --out Config.bin configs/
$ schema unpack --root Config --in Config.bin configs/
```

A directory named after a field holds that field's value; an enum-keyed array
takes one `<Variant>.json` per variant (there is no `None.json` — `None` keys
no slot); a bounded array takes files in name order; anything can collapse to
a single `<field>.json`. The output is the table's wire bytes and **nothing
else** — no magic, no hash, no protocol id. If you want an envelope, write
your own few lines around them. `unpack` is the inverse, and `unpack` → `pack`
is byte-stable.

`unpack` writes the expanded shape — one `<field>.json` per field of the root
and one `<field>/<Variant>.json` per keyed slot, or one `<Root>.json` with
`--one-file` — and PRUNES the files it owns
and did not write, so a stale value cannot survive into a newer tree. Neither
verb writes to your `.schema` sources, and both exit nonzero when the read
report is not silent; `--tolerate` accepts it, `--verbose` prints it either way
and names any hidden non-JSON file the walk passed over.

`pack` reads the texts with its own engine inside the compiler rather than by
calling generated code — the compiler is a Go program — so the tree carries a
second implementation of the same wire, held to the backends by goldens.

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
