# Using schema

Every feature of the language, with the code it generates.

The generated code in this guide is real compiler output, not an
approximation — if it looks surprising, it is because that is what the
compiler does.

- [The five minute version](#the-five-minute-version)
- [Declarations](#declarations)
  - [const](#const) · [enum](#enum) · [flags](#flags) · [type](#type) ·
    [union](#union)
- [Documenting and tagging: `///` and tags](#documenting-and-tagging--and-tags)
- [Field types](#field-types)
  - [Integers](#integers) · [Ranged integers](#ranged-integers) ·
    [bits(N)](#bitsn) · [bool](#bool) · [Floats](#floats) ·
    [Compressed floats](#compressed-floats) · [fixed(I, F)](#fixedi-f) · [ufixed(I, F)](#ufixedi-f) ·
    [Strings and bytes](#strings-and-bytes) · [wstring(N)](#wstringn) ·
    [Arrays](#arrays) · [Composition](#composition)
- [Branches: if / else](#branches-if--else)
- [Defaults](#defaults)
- [The wire](#the-wire)
- [Reading untrusted data](#reading-untrusted-data)
- [The protocol id](#the-protocol-id)
- [Tables: data that outlives builds](#tables-data-that-outlives-builds)
  - [Optional fields](#optional-fields-settings-gunnersettings) ·
    [Enum-keyed arrays](#enum-keyed-arrays-ships-shiptypeshipconfig) ·
    [Maps](#maps-ships-mapstring32shipconfig) ·
    [Unbounded arrays](#unbounded-arrays-placements-placement) ·
    [Pointers](#pointers-next-node) · [The block form](#the-block-form-rows-another-language-points-at) ·
    [The cooked form](#the-cooked-form-point-at-a-file-instead-of-parsing-it) ·
    [The text form](#the-text-form-json-in-and-out-of-one-table)
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

Your `.schema` files are read, never written. `schema fmt` is the only command
that rewrites one, so `check`, `generate`, `id` and the rest run against a
read-only checkout, and they answer the same way whether or not the source is
in canonical form.

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
enum class ShipType : uint8_t { None = 0, Fighter = 1, Corvette = 2, Bomber = 3, Count = 3, Max = 3 };
```

On the wire it costs `bitsRequired(variant count)` — 2 bits here, for four
values. `None` is one of those values, so an enum whose declared variant
count is a power of two pays one bit for it: four declared variants are five
wire values and cost 3 bits, not 2. Declaring `enum E | max = 15` (its variant
list on the next line) reserves
headroom so you can
add variants later without moving the field width.

The `Max` member is the enum's **extent** — the same number `E.Max` names in
schema expressions: the highest wire-legal value, and (headroom aside) the
count of real variants under the sentinel-zero convention. All nine targets
spell it their own way — `ShipType::Max` (C++), `ShipType.Max` (C#),
`ShipTypeMax` (Go), `ShipType::MAX` (Rust), `ShipType.Max` (JS),
`SHIP_TYPE_MAX` (C), `ShipType.max` (Dart and Java), `ShipType.max/0`
(Elixir) — and a union's generated `<Union>Type` tag enum
carries it too, so ranges and asserts reference the enum directly instead of
a hand-declared count constant. `Max` is consequently reserved as a variant
name, like `None`.

The `Count` member beside it is the **declared variant count**, `None`
excluded — `ShipType::Count` (C++), `ShipType.Count` (C#), `ShipTypeCount`
(Go), `ShipType::COUNT` (Rust), `ShipType.Count` (JS), `SHIP_TYPE_COUNT` (C),
`ShipType.count` (Dart and Java), `ShipType.count/0` (Elixir), and
`E.Count` in schema expressions. Without headroom `Count` and `Max` are the
same number; under `| max = 15` they are 3 and 15, and that difference is
what the two words are for. `Count` is a reserved variant name too.

**Two loop rules, and they are the whole story:**

- A loop over the **declared variants** runs from `1` to `Count` inclusive.
- A loop over **every ordinal**, `None` included, runs from `0` to `Max`
  inclusive.
- **Size storage and keyed arrays by `Max`** — the extent is what has to fit,
  headroom and all.

Every target also generates a **debug/log name function**, and the spelling is
each target's own: `EnumName(value)` in C++ (overloaded per enum),
`EnumNameShipType` in C#/Go/JS, `enumNameShipType` in Dart/Java, and
`enum_name_ship_type` in C/Rust/Elixir — returning the variant's declared
spelling for any wire value, out-of-set values included (`"???"`).

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
expressions as `Capabilities.Count` — the same word an enum carries, meaning
the same thing. Flags have no `.Max` — the variants are
independent bits, not a range with a top; the compiler refuses `.Max` on a
flags type and names `.Count` instead.

Flags get the same **debug/log name surface** as enums, in two forms: a
per-bit name — `FlagNameCapabilities(bit)` in C++/C#/Go/JS
(`flagNameCapabilities` in Dart/Java, `flag_name_capabilities` in
C/Rust/Elixir), out-of-range bits naming as `"???"` — and a set renderer,
`FlagNamesCapabilities(value)` under the same per-target spelling, which formats the set bits the way a log line
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
before it rides. The representation is per target, all nine covered: C++ and
C generate the tagged union above; Go, C#, JS, Dart, Java and Elixir lay the
tag beside one pre-allocated arm per variant; Rust gets a real
`enum ColliderShape { None, Box(BoxCollider), ... }`.

An arm is a field line: its type is any type a field's is, and an arm may
name no type at all, which is an arm that selects and carries nothing. A
union inside a `type` body takes arms that name declared types. A union with
any other arm belongs to a table closure (SPEC-TABLES.md §2.6). Unions ride
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

## Documenting and tagging: `///` and tags

*Specified, not yet emitted. The front end does not read a `///` block, and a
tag is carried on a `type` declaration alone (SPEC.md §4.1, §4.2,
SPEC-TABLES.md §8.1).*

### Doc comments are opt-in, and the marker is `///`

A schema tree is full of working notes written directly above things. Under an
implicit rule every one of them would become a descriptor string, a comment in
nine generated languages, and read-only data in the binary of every game that
links the table headers. So documentation is something you OPT INTO, and the
marker is `///`:

```
/// The hull's structural health. A hull at zero is destroyed.
health float32 = 100.0 | min = 0.0, max = 1000.0

// a working note, and it reaches nothing at all
armor int32 = 1
```

A DOC COMMENT is a contiguous run of `///` lines whose last line immediately
precedes a **declaration, a field, an enum or flags variant, or a union arm**.
A plain `//` block above the same item stays an ordinary comment. `///` is a
`//` line by ordinary lexing, so a reader that does not know the rule sees a
comment and every editor already highlights it as one, and a `/* */` block is
never a doc comment in any spelling.

**The text is the block verbatim, with the marker removed.** Each line
contributes what follows its `///`, with at most one leading space dropped and
trailing whitespace dropped, the lines joined with a single newline. Nothing
else is interpreted: no markup, no keywords, no reflow, no escape sequences,
so two leading spaces and an empty line in the middle are what a tool prints.

Where it goes: into the `doc` column of the reflection descriptors, and into
ordinary LINE comments above the declaration in the generated code, `//` in
C++, C, C#, Go, JS and Java and `///` in Rust and Dart. A line comment ends
where the line does, so a `*/` or a `<` in your text needs no rule at all,
which is why no target emits `/** */` or a `<summary>` element. Elixir carries
the descriptor column alone for a field and a variant, which have no attribute
to hang a doc on, and emits `@moduledoc` / `@typedoc` for a declaration.

**Every `///` line is part of a doc comment or is refused by name**, because
silently dropping an opt-in is the outcome opt-in exists to prevent. Refused:
a `///` block above `package`, above a `const( )`, `reserved( )` or `align`
item, or above a closing brace, none of which has a descriptor row to carry
one; a `///` block separated from its item by a blank line or by an ordinary
`//` line; and a `///` that TRAILS code on the item's own line. Each
diagnostic names the spelling that works, and `//` is always that spelling.
`| doc = "..."` is refused too: one text has one spelling.

### Tags: the vocabulary is yours

A TAG is one user-chosen identifier right of the pipe, in its own open
namespace, and **every declaration and every declared item may carry one**: a
`type`, a `table`, an `enum`, `flags`, a `union`, a `const`, a field, an enum
or flags variant, and a union arm.

```
const StarterGold = 500 | tuning

enum Rarity | loot
{
    Common
    Uncommon
    Rare      | celebrate
    Legendary | celebrate
}

table ShipConfig | designer_facing
{
    /// The hull's structural health. A hull at zero is destroyed.
    health   float32 = 100.0 | ui_slider, min = 0.0, max = 1000.0
    texture  uint64          | asset_ref
    nickname string(32)      | localized
}
```

A tag is **inert**: parsed, carried through the IR, carried into the
descriptors, and it changes zero generated wire code. Valueless markers come
first in a qualification and valued keys after, which the parser accepts in
any order and `schema fmt` writes in this one. A bare identifier that spells a
known valued key is refused by name rather than taken as a tag, so `| min`
draws "min takes a value: write min = 0" and the open namespace cannot swallow
a typo in the closed one. A repeated tag is refused, and so is a tag that
repeats a valued key already on the line.

**Both are free edits.** A doc comment and a tag are excluded from the wire
shape projection and from the cook projection, so neither moves the protocol
id or the build version. Neither enters a baseline row. And neither is in the
silent class, because nothing about a stored byte's meaning is in reach of
them. Annotating a shipped schema costs no redeploy and is safe at any time,
which is exactly what makes the tag namespace safe to leave open.

Meaning arrives by CLAIMING. A future compiler pass claims a tag and assigns
its semantics. Until one does, the compiler does not ask what a tag means, and
a tool that reads the tag out of a descriptor is the claiming pass you wrote
for yourself.

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

**`I + F` must equal a storage width — 8, 16, 32, 64 or 128.** The raw scaled
value IS the storage, so a sum that names no integer type is refused at
compile time (`ufixed(16, 8)`: "I + F = 24 must equal a storage width").
Split the width you want between the two: `fixed(24, 8)` and `fixed(16, 16)`
are both 32 bits, and which one you pick is where you want the point.

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
Everything else — the `I + F` storage-width rule, required whole-unit bounds,
exact round trips, defaults, degenerate ranges — works exactly as for
`fixed`.

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

The write refuses embedded NULs and any length past the maximum, and the
read validates both. In C and C++ a successful read also writes the zero
byte at `name[name_length]`, so `name` is a valid C string every time the
read succeeds, which is what the `+ 1` in the array is for. The other seven
targets carry the buffer and the used length with nothing written past it.

**`string(N)` also carries a UTF-8 rule, and the READER enforces it.** The
wire is byte-identical to `bytes(N)`. What the `string` spelling adds is
that a payload which is not well-formed UTF-8 fails the read, in every
target and in every build, and the failure is terminal like any other read
failure. So Latin-1 bytes in a `string(32)` are refused by every reader that
sees them. If your payload is genuinely arbitrary bytes, declare `bytes(N)`,
which is the same wire with no encoding rule (SPEC.md §4.7).

### wstring(N)

*Specified, not yet emitted. The front end refuses the spelling at parse, no
backend teaches the type, and the `examples/` declaration and the golden pins
land with the first implementation (SPEC.md §4.12,
[#522](https://github.com/mas-bandwidth/schema/issues/522)).*

```
title wstring(64)
```

Wide text: UTF-16 code units, with **N counted in CODE UNITS**, not code
points and not bytes, so a character outside the basic plane occupies two of
them. It exists for a host whose native text is already UTF-16, where the wire
and the string hold the same units and text crosses the boundary as a copy
rather than a transcode.

The wire is two steps and **no alignment in either**: the length as a ranged
integer over `[0, N]`, then each code unit as a 32-bit group, packed
LSB-first like every other value. `MaxBits` is therefore
`bits_required(0, N) + 32 * N`, with no padding term. And because it
introduces no alignment point, a `wstring` field does not make its type's
layout depend on the entry bit offset the way `string` and `bytes` do.

A `wstring` takes no attributes and no `= default`, and `wstring(N)` with N
below 2 is a compile error, the same floor `string(N)` carries.

**The reader enforces four rules, identically in all nine targets and in every
build mode.** A decoded length outside `[0, N]` fails. A group above `0xFFFF`
fails, whatever wide character type the host happens to have. A zero group
among the transmitted units fails, which is `string(N)`'s interior-null rule
in code-unit terms. And surrogates must PAIR: a high surrogate not
immediately followed by a low one fails, a low surrogate not immediately
preceded by a high one fails, and a high surrogate as the final group fails.
Well-formed pairs are how astral text travels. Nothing else about the text is
examined: noncharacters are accepted, `0xFFFF` included, and there is no
normalization, no case folding and no code-point count. A reader that adds a
check here is as wrong as one that drops a check above.

**The write side checks two things**, in each target's own idiom: the used
length is within `[0, N]`, because it guards the copy, and a zero code unit
among the used units is refused. Surrogate pairing is NOT checked on write.
It is a writer obligation the reader on the other end enforces.

Storage is a pre-allocated buffer of code units with a used length beside it,
and nothing transcodes inside the generated read or write path. What the
boundary costs is per language. On C#, Java, JavaScript, Dart, and C++ or C on
a UTF-16 host it is a copy of code units, which is the reason the type exists.
On Go, Rust and Elixir, whose native text is UTF-8, the transcode is real and
is paid in your own code: `utf16.Encode`/`utf16.Decode` in Go,
`str::encode_utf16`/`String::from_utf16` in Rust, and
`:unicode.characters_to_binary/3` in Elixir. In C and C++ a successful read
also writes the zero unit at index `length`, which is what the `+ 1` in
`char16_t[N + 1]` is for.

`wstring(N)` rides the TABLE wire too, under a wide-text kind of its own,
whose payload is a byte length holding half as many code units, two bytes each
little-endian. So respelling a field between `string(N)` and `wstring(N)` is
an ordinary counted kind mismatch and never UTF-8 bytes read as code units.
The validation above holds on both wires, and only the SHAPE of the refusal
differs: a packet read stops, while a table read gives the field its declared
default, counts one `malformed`, and reads on. It is refused as a MAP key,
with `string(N)` named as the key that works, because `memcmp` over UTF-8 is
a portable order and little-endian code units have none.


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

**The TYPE wire** is bit-packed and decided at compile time. Nothing on it
identifies fields — both sides know the layout because they were generated
from the same schema. That is what makes it small and fast, and why
versioning is by [protocol id](#the-protocol-id): one id, same-or-refuse,
with no evolution machinery anywhere. That is a deliberate choice for data
whose writer and reader ship together, and it is one of TWO wires this
language has. Data that has to survive schema drift is the other one's job:
declare a `table` and it rides the tolerant wire, where a field is identified
by the hash of its name, any reader reads any data, and every difference is
counted in a read report rather than being fatal (docs/SPEC-TABLES.md, and
[Tables](#tables-data-that-outlives-builds) below). Save games, config and
asset archives belong there; packets belong here.

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

*The rule below is specified and the compiler does not scope by it yet:
`ir.WireProjection` still renders the whole unit at `ProjectionVersion` 2, and
the scoping is owed as
[#524](https://github.com/mas-bandwidth/schema/issues/524) (SPEC.md §3.1).*

It covers the CLOSURE over your `type` declarations — every `type`, and every
enum and union a `type` reaches, by a field's type, an array's element, an
array's bound, a keyed array's key enum, a constant's value, a union arm's
payload or either side of a branch. An enum only a `table` reaches is not in
it. **`flags` is the one exception and projects whether a `type` reaches it or
not**, because a mask is positional on the table wire and the protocol id is
the only runtime frame that can refuse two peers whose bit assignments differ
(SPEC.md §3.1). Tables never touch it — edit a table and what moves is the
**build version**, which keys cooked assets and gates no connection (below).

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

A table lives on its own wire — self-describing and evolution-tolerant,
carried by **all nine targets**: C++ and C take both classes, and C#, Dart,
Elixir, Go, Java, JavaScript and Rust take the fixed class, wire and
text form both; the pointer surface ON THE WIRE is a follow-on in those seven,
whose cook and block accelerators read a pointered unit today.

**Each field is a reference, a kind byte and a payload**, and the file carries
every id it used once each in a trailer at the end, so a field header spends
one byte on the reference where the eight-byte id itself would have spent
eight. The file opens with a one-byte FORM version — `1` for a file — which a
reader that does not know it refuses by name. *That form is written by the C++
reference and by the tool; the eight other backends still write its previous
form, and each is a named follow-on
([#511](https://github.com/mas-bandwidth/schema/issues/511) to
[#518](https://github.com/mas-bandwidth/schema/issues/518), PORTING.md M20).*
Field identity is a hash of the
field NAME, so any reader takes any data, both directions: unknown fields are
skipped by their kind, absent fields take their declared defaults, a field
whose kind changed is skipped rather than misdecoded, a field whose kind
merely GREW — an integer to a wider integer of the same signedness, or
`float32` to `float64` — decodes exactly and counts `widened`, and
out-of-range values are clamped. *The widening rule is specified and nothing
counts `widened` yet, in any language
([#523](https://github.com/mas-bandwidth/schema/issues/523)).* Every such event is counted in a report;
framing damage stops the damaged nesting level, keeps what it decoded there
and reads on past the field's own length, so one bad subtable never takes
down the rest of the file. Tables never touch the protocol id —
add, edit or remove one and no packet byte and no id moves.

**An array's BOUND is not part of that identity either.** Resize a bounded
array — a literal, a constant, or an `E.Max` expression that moved when
the enum grew — and files written under the old bound still load: a count
past your bound keeps the bounded prefix and counts `clamped`, a count short
of it leaves your tail at its declared defaults. (`malformed` means something
else: a count the body cannot cover.) The storage struct's size and extent
change with the constant; the table on the wire does not, because identity is
the field name hash and the kind.

**Enum variants and union arms ride by name too.** An enum value rides under
its own kind carrying a reference to its variant NAME's id, and a union body
opens with a reference to its arm's, so you can insert a variant in the MIDDLE
and every stored value still reads back as itself:

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
code with **no serialize dependency**, includable from any translation unit
(a unit declaring a 128-bit field takes one thing from `serialize.h`: the
128-bit storage type, `serialize::int128_t` / `uint128_t`).
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
saves as ten bytes and loads back complete: the form byte, the body's zero
reference, and the eight-byte entry count of an id table with no entries. That
fixed cost is the price of 64-bit identity on a wire that trades bytes for
tolerance, and it is why a small same-build message belongs on the type wire
instead (SPEC-TABLES.md §3).

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

The JavaScript surface is the same functions, name first, exported from
`<Base>Table.js` beside the packet module — and it imports no runtime at all —
in the language's own shape: a result is a return, and a caller's error is an
exception. Storage is a class with public fields, every buffer allocated at
construction, so `Load` allocates nothing and overlays in place after restoring
the declared defaults. Buffers are caller-owned `Uint8Array`s:

```js
import { ShipConfig, ShipConfigMeasure, ShipConfigSave, ShipConfigSaveInto, ShipConfigLoad, TableReport }
  from "./ConfigTable.js";

const ship = new ShipConfig();
ship.Health = 250.0;
ship.SettingsPresent = true;           // ?T: presence decides whether it rides

const bytes = ShipConfigSave(ship);    // a fresh Uint8Array of exactly Measure's size
const size = ShipConfigMeasure(ship);  // exact, writes nothing
const buffer = new Uint8Array(size);   // or any storage you own
ShipConfigSaveInto(ship, buffer);      // returns size; a buffer too small, a
// count or length past its bound, or an enum value no variant names is YOUR
// error and throws a RangeError naming the field — never a -1

const report = new TableReport();
const loaded = ShipConfigLoad(bytes, report);        // the value, fresh
ShipConfigLoad(bytes, report, loaded);               // or your own, overlaid in place
if (report.Malformed) {
  // framing damage: the good prefix is kept — what the DATA did is always
  // the report, never an exception
}
if (report.Unknown || report.KindMismatch || report.Clamped) {
  // the data came from a different schema generation — loaded is still
  // fully usable; log the counts so drift is visible
}
report.reset();                        // a report accumulates across loads, as C++'s does
```

`Save` and `Load` each build one writer or reader plus its `DataView`, because
JavaScript has no stack object where C++ and C# have one. **Nothing per field
allocates, and that is a measured number** (`make tables-js-alloc`, on the Node
major this project pins, because a managed runtime's allocation floor is a
property of its optimizer as much as of the code): a table with no 64-bit field
reads, measures and writes at zero bytes per iteration, and so does the hoisted
Load→Save loop below over it; a block row walk and a cook deref are zero too. A
field declared `int64`, `uint64`, `bits(N > 32)` or a `flags` mask costs one
BigInt per field READ — JavaScript's only exact 64-bit integer is an object —
and nothing per field written: the loop below, over the corpus's root table
with its 64-bit fields, measures 67 bytes per iteration, which is Load's
BigInts at sixteen bytes each and not one byte more. That is the whole of the
floor. On a per-frame path, hoist the pair
out of the loop and call the `Body` half directly — the same code, with the two
objects reused:

```js
const reader = new TableReader(buffer, report);
const writer = new TableWriter(out);
for (const message of stream) {
  ShipConfigLoadBody(reader.reset(message, report), loaded);
  ShipConfigSaveBody(writer.reset(out), loaded);
}
```

A `string(N)` or a `bytes(N)` is a `Uint8Array` with a `<Name>Length` beside
it, and a bounded array a preallocated `Array` beside a `<Name>Count`, exactly
as C++ and C# hold them — and exactly as the packet emitter already spells them
for the unit's `type`s, which is not a free choice: a table's closure decodes
into those very classes, and one unit is one spelling. The codec never builds a
JavaScript string, so a read allocates nothing for one. Decode at the boundary,
where the allocation is your own choice:

```js
const name = new TextDecoder().decode(loaded.Name.subarray(0, loaded.NameLength));
loaded.NameLength = new TextEncoder().encodeInto("Rowan", loaded.Name).written;
```

The bytes are the same bytes: a shared golden corpus pins C++'s encoding of a
set of instances and the C# and C legs byte-compare their own `Save` against
it, then load those very files. `string(N)` and `bytes(N)` are a `byte[N]` beside an
`int` used length, arrays a `T[N]` beside an `int` used count, `?T` a value
beside a `<Name>Present` bool, and a union is its tag beside one pre-allocated
arm — the same spelling the packet backend uses, because a table's closure
decodes into the packet backend's own classes.

An enum-keyed array is a `TableKeyed<T, E>` holding `E.Max` slots — one per
named variant, nothing for `None`, the key `k` at index `k - 1`. Nothing
outside the array names its size: the type derives its extent from the enum's
own `Max`. **C# indexes it by the ENUM VALUE**, as every port does, but the
language has no non-boxing generic enum-to-int conversion — so the caller
writes the cast, `fleet.Ships[(int)ShipType.Bomber]`. The cast, never the
shift. The `None` refusal survives as a runtime guard on that indexer, and
it stands in every build, as the C++ abort does. Generated code
walks `.Slots` directly and never pays for the guard.

`foreach` walks every slot and yields the KEY, `1 .. E.Max`, beside the
element — the same currency the indexer takes, so a site that wants the key
as its enum writes `(ShipType)ship_type` there. The enumerator is a struct, so
the walk allocates nothing:

```csharp
foreach (var (ship_type, ship) in fleet.Ships)
{
    ship.Health *= 2.0f;   // ship_type is the KEY, never a storage index
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
an enum-keyed array's `KeyTypeName`/`KeyName`/`KeyId`, which are functions of
the KEY — `KeyId(0)` is `0`, the reserved id that says `None` names no slot.
`ArrayBound` is the storage extent, `E.Max`, and the key at index `i` is
`i + 1`.

**The C surface is the same three functions, name first, over buffers the
caller owns** — the same spelling as C++ with a pointer where C++ takes a
reference:

```c
#include "ConfigTable.h"

ShipConfig ship;
ship_config_reset( &ship );          /* C has no member initializers: this IS
                                      where the declared defaults live */
ship.health = 250.0f;

int64_t size = ship_config_measure( &ship );      /* exact, writes nothing */
uint8_t * buffer = malloc( (size_t) size );      /* or any storage you own */
ship_config_save( &ship, buffer, size );           /* returns size, or -1 */

TableReport report;
ShipConfig loaded;
memset( &report, 0, sizeof( report ) );
if ( !ship_config_load( &loaded, buffer, size, &report ) )
{
    /* framing damage: report.malformed is set, the good prefix is kept */
}
if ( report.unknown || report.kind_mismatch || report.clamped )
{
    /* the data came from a different schema generation — loaded is still
       fully usable; log the counts so drift is visible */
}
```

Generation produces `<Base>Table.h` and `<Base>Table.c`. **The header is the
whole wire** — the storage structs, `Reset`, `Measure`, `Save`, `Load` and
`<Name>Open`, all `static` and inlinable, with no serialize dependency and no
object file to link. **The `.c` carries the reflection descriptors and the
text form**, so a translation unit that only reads and writes the wire
compiles neither; compile it when you call `<Name>TableType`,
`<Name>FromJson` or `<Name>ToJson`.

Two schema units LINK together — every external the table backend emits
carries the package, `schema_<package>_<type>_<what>_` — so a program may hold
two generations of one schema, which is what the conformance driver does with
`tblv1::Cfg` and `tblv2::Cfg`. Two units cannot be INCLUDED into one
translation unit: C has no namespace, and that limit is the C target's
standing one rather than anything tables added.

An enum-keyed array is a plain `T slots[E_MAX]` — one slot per named variant,
nothing for `None`, the key `k` at index `k - 1`. **Index it by the ENUM
VALUE through `TableKeyedAt`**, which is where the left shift and the `None`
refusal live; the refusal is an assert plus an abort and it stands in every
build, exactly as C++'s accessor refuses:

```c
SCHEMA_TABLE_KEYED_AT( fleet.ships, SHIP_TYPE_BOMBER ).health *= 2.0f;

/* walking every slot: the key is 1 .. E_MAX, never a storage index */
int32_t key;
for ( key = 1; key <= SHIP_TYPE_MAX; key++ )
{
    ShipConfig * ship = &SCHEMA_TABLE_KEYED_AT( fleet.ships, key );
    ship->health *= 2.0f;
}
```

A `?T` is the value beside a `<name>_present` byte, a `string(N)` a
`char[N + 1]` beside an `int32_t <name>_length`, a `bytes(N)` a `uint8_t[N]`
beside its length, and a union its tag beside `as.<arm>` — the same spelling
the packet backend uses, because a table's closure decodes into the packet
backend's own structs.

`<Name>TableType()` returns the reflection descriptor. Its vocabulary columns
are TABLES rather than functions: `variants` is indexed by the enum's value
and `keys` by an enum-keyed array's KEY, each entry a `(name, id)` pair, with
a NULL name for a value the declared set does not name. C has no captureless
lambda, and a named function per enum would claim a name per enum, so the
same facts ride as constant data — every question the descriptors answer
elsewhere has an answer here, asked of an array instead of a call.

The Rust surface is the same three functions, name first, as free functions in
the generated crate, over slices the caller owns. Storage is a `#[repr(C)]`
struct of public fields — every buffer fixed at its declared capacity, so
`load` allocates nothing and overlays in place after restoring the declared
defaults:

```rust
use example::*;

let mut ship = ShipConfig::default();   // the DECLARED defaults, not zeros
ship.health = 250.0;
ship.settings_present = true;           // ?T: presence decides whether it rides

let size = ship_config_measure(&ship);  // exact, writes nothing
let mut buffer = vec![0u8; size as usize];
ship_config_save(&ship, &mut buffer);   // returns size, or -1

let mut report = TableReport::default();
let mut loaded = ShipConfig::default();
if !ship_config_load(&mut loaded, &buffer, &mut report) {
    // framing damage: report.malformed is set, the good prefix is kept
}
if report.unknown != 0 || report.kind_mismatch != 0 || report.clamped != 0 {
    // the data came from a different schema generation — loaded is still
    // fully usable; log the counts so drift is visible
}
```

**The Go surface is the same three functions again**, name first at package
scope over a `*T` the caller owns. Storage is a plain struct — the Go packet
emitter's own spelling, because a table's closure decodes into the structs that
emitter already wrote — so a value costs one declaration and `Load` overlays it
in place after restoring the declared defaults:

```go
import "example"

var ship example.ShipConfig
example.ShipConfigReset(&ship)        // the declared defaults, in place
ship.Health = 250.0
ship.SettingsPresent = true           // ?T: presence decides whether it rides

size := example.ShipConfigMeasure(&ship)   // exact, writes nothing
buffer := make([]byte, size)               // or any storage you own
example.ShipConfigSave(&ship, buffer)      // returns size, or -1

var report example.TableReport
var loaded example.ShipConfig
if !example.ShipConfigLoad(&loaded, buffer, &report) {
    // framing damage: report.Malformed is set, the good prefix is kept
}
if report.Unknown != 0 || report.KindMismatch != 0 || report.Clamped != 0 {
    // the data came from a different schema generation — loaded is still
    // fully usable; log the counts so drift is visible
}
```

The report is a PARAMETER rather than a member of the reader, which is the one
place the Rust codecs read differently from the C++ ones: a reader holding
`&mut TableReport` could not hand a sub-reader out of its own buffer while that
borrow stood. One report, one caller, the same six counters.

`string(N)` and `bytes(N)` are a `[u8; N]` beside an `i32` used length, arrays
a `[T; N]` beside an `i32` used count, `?T` a value beside a `<name>_present`
bool, and a union is a real Rust `enum` — the same spelling the packet backend
uses, because a table's closure decodes into the packet backend's own types.

An enum-keyed array is a `TableKeyed<T, { E::MAX.0 as usize }>` holding `E.Max`
slots — one per named variant, nothing for `None`, the key `k` at index
`k - 1`. Nothing outside the array names its size: the const generic IS the
enum's own `Max`, so there is no constant a consumer could put out of step with
it. **Rust indexes it by the KEY**, as every port does:

```rust
fleet.ships[ShipType::BOMBER.0 as u64].health *= 2.0;

for (ship_type, ship) in fleet.ships.iter_mut() {
    ship.health *= 2.0;    // ship_type is the KEY, 1 ..= E.Max, never a storage index
}
```

The `None` refusal is a panic from the indexer, and it stands in every build,
as the C++ abort does — the storage shifts left and holds no slot for `None`,
so a build that skipped the compare would index one element before the array.
Generated code walks `.slots` directly and never pays for it.

`<name>_table_type()` returns the reflection descriptor: field names, wire ids
and kinds, storage offsets, bounds, ranges, guards, `optional`, the enum/union
vocabulary, and an enum-keyed array's `key_type_name`/`key_name`/`key_id`,
which are functions of the KEY — `key_id(0)` is `0`, the reserved id that says
`None` names no slot. `array_bound` is the storage extent, `E.Max`, and the key
at index `i` is `i + 1`. It is `static` data, so any thread may read it and it
costs a lookup rather than a parse.

**Nothing on that path allocates**, and in Go that is measured rather than
asserted: `Load`, `Measure`, `Save`, `FromJson`, `ToJsonMeasure` and `ToJson`
each read zero under `testing.AllocsPerRun`, and a soak over the whole corpus
holds the allocation counter at zero. A nil `*TableReport` is allowed and every
tolerance event still decides the same way.

`string(N)` and `bytes(N)` are an `[N]byte` beside an `int32` used length,
arrays an `[N]T` beside an `int32` used count, `?T` a value beside a
`<Name>Present` bool, and a union its tag beside one arm per variant.

**An enum-keyed array in Go IS the plain `[E.Max]T` the schema means** — Go has
neither operator overloading nor a generic array extent, so there is no wrapper
type and the extent is the generated `E.Max` constant and no other number. The
shift and BOTH refusals live in one helper, so no call site spells any of them
— `None` keys no slot, and neither does a value past `E.Max`:

```go
*example.TableKeyed(fleet.Ships[:], int(example.ShipTypeBomber)) = ship
```

Iterating is the language's own `for i, ship := range fleet.Ships`, where the
KEY is `ShipType(i + 1)` — and `TableKeyed` is what a caller reaches for when it
holds a key rather than an index.

**An enum's wire identity is a METHOD** in Go, for the same reason: Go has no
overloading, and a free pair would have to mint a per-enum unit-level name §11
does not claim. `grade.TableEnumId()` gives the variant's hash and whether any
variant names the value; `(&grade).TableEnumValue(id)` resolves one back.

`<Name>TableType()` returns the reflection descriptor, with the same columns
C++ carries — including the memory ones, because `unsafe.Offsetof` and
`unsafe.Sizeof` are constant expressions of Go's own layout model rather than a
guess about it. That is what lets ONE generic walk drive the text form for
every table.

**Three of those columns are FUNCTIONS in Go where C++ has pointers**, and a
walker written from the C++ example above has to know which: `Table`, `Arms`
and a keyed field's `KeyId`/`KeyName` are called rather than dereferenced, so
C++'s `field.table->name` is `field.Table().Name` and a scalar field's column
is `nil` rather than `NULL`. The reason is Go's own: a descriptor graph is
allowed to be cyclic — a table may name another that names it back — and Go
refuses an initialization cycle among package-level variables, which a pointer
column would be.

**The two ACCELERATORS carry descriptors of their own**, and they are separate
types because they describe a different thing: `TableBlockInfo` /
`TableBlockFieldInfo` describe a block's projection and its rows (§19.2), and
`TableCookInfo` / `TableCookFieldInfo` describe a cooked record (§7). Reach
them through `block.Type()` and `cook.Type()`; every record hangs off the
element column, so one walk reaches the whole graph from either root. The wire's
`TableTypeInfo` is not those and does not substitute for them: it describes the
STORAGE a codec fills, and the accelerators describe bytes another build wrote.

**The Java surface is the same three functions once more**, name first and
lowerCamel — Java's one naming rule, the same one the packet half follows with
`writeVec3` — as `static` methods on the file's `<Base>Table` class, over
`byte[]` the caller owns. Storage is a `final` class with public fields, lowerCamel, every buffer
allocated at construction — the Java packet emitter's own spelling, because a
table's closure decodes into that emitter's own classes:

```java
import example.ConfigTable;
import example.TableReport;

ConfigTable.ShipConfig ship = new ConfigTable.ShipConfig();
ship.health = 250.0f;
ship.settingsPresent = true;          // ?T: presence decides whether it rides

long size = ConfigTable.shipConfigMeasure(ship);   // exact, writes nothing
byte[] buffer = new byte[(int) size];              // or any storage you own
ConfigTable.shipConfigSave(ship, buffer);          // returns size, or -1

TableReport report = new TableReport();
ConfigTable.ShipConfig loaded = new ConfigTable.ShipConfig();
if (!ConfigTable.shipConfigLoad(loaded, buffer, report)) {
    // framing damage: report.malformed is set, the good prefix is kept
}
if (report.unknown != 0 || report.kindMismatch != 0 || report.clamped != 0) {
    // the data came from a different schema generation — loaded is still
    // fully usable; log the counts so drift is visible
}
```

**A hot loop hoists the reader and reuses it**, which is the shape to reach for
when you are reading a stream of records rather than a config file once:

```java
TableReader reader = new TableReader();            // yours, not the codec's
TableWriter writer = new TableWriter();            // and the writer likewise
TableReport report = new TableReport();
for (byte[] record : records) {
    reader.reset(record, 0, record.length, report.clear());
    ConfigTable.shipConfigLoadBody(reader, loaded); // allocates nothing at all

    writer.reset(buffer, 0, buffer.length);
    ConfigTable.shipConfigSaveBody(writer, loaded); // nor does this
}
```

**The convenience forms allocate one object each, and the hoisted forms are how
you get to zero.** `shipConfigLoad` builds a `TableReader` per call and
`shipConfigSave` a `TableWriter`; `shipConfigLoadBody` and `shipConfigSaveBody`
take yours. Neither allocates anything else.

**Nothing on the hoisted path allocates, and in Java that is measured rather than
asserted** — the same standard the Go leg holds itself to, with the JVM's own
per-thread allocation counter in place of `testing.AllocsPerRun`. The wire's
read and save, the block's row walk and the cook's read each read **zero bytes
per record** across a measured window, a soak re-measures every path at every
sample for an hour, and a negative control adds one `new byte[1]` per record and
requires the gate to go red. A nested body is bounded by MOVING THE READER'S
LIMIT rather than by allocating a sub-reader, which is what makes the number
reachable at all. The text form allocates by nature and says what it allocates.

Java's unit scope is the PACKAGE and a public type lives in a file of its own
name, so the shared runtime is **one file per type** — `TableReport.java`,
`TableReader.java`, `TableWriter.java`, `TableJson.java` and the rest — rather
than one home file. Nothing about that varies with which schema file sorts
first, and a unit that declares no table emits not one of them.

`string(N)` and `bytes(N)` are a `byte[N]` beside an `int` used length, arrays a
`T[N]` beside an `int` used count, `?T` a value beside a `<name>Present` bool,
and a union its tag beside one pre-allocated arm per variant.

**An enum-keyed array in Java is a plain typed array of `E.max` slots** — Java
has no generic container that could hold a primitive slot without boxing, and an
`[E]int32` is exactly that case. **On a `table` it comes with an accessor pair
that takes the KEY**; on a plain `type` it is the bare array, because a `type`'s
storage is the packet emitter's and this wire changes nothing about it (C++ does
the same), so there a caller reaches through `TableKeyed.slot(key)` rather than
an accessor:

```java
hull.turrets(Weapon.missile).ammo = 20;            // by the key, never the slot
hull.scores(Grade.gold, 7);                        // the setter, for a value element

for (int i = 0; i < TableKeyed.count(hull.turrets); i++) {
    int key = TableKeyed.key(i);                   // the KEY, 1 .. E.max
    hull.turrets(key).health *= 2.0f;
}
```

No call site spells the shift: `TableKeyed` is the one place the `k - 1` lives —
on a table through the accessor above, on a `type` through
`board.perTeam[TableKeyed.slot(key)]` — and indexing by `None` throws from it, in
every build, as the C++ abort does. The generated codecs walk the raw array by
storage index and never pay for the guard.

`<Name>TableType()` returns the reflection descriptor. **Its memory columns are
ACCESSORS rather than offsets**, which is the one place a walker written from
the C++ example has to know the difference: a Java field has no offset and a
Java object has no meaningful sizeof, so the descriptor carries the reader and
the writer the emitter wrote — `getRaw`, `setRaw`, `getChild`, `getBuffer` and
the counted and presence companions beside them. Same role, one place, in the
language's own currency, and the generic text-form walk reaches storage through
those and through nothing else.

**A block row and a cooked record have no Java type at all.** There is no struct
to lay out, so `<Name>Row`'s generated accessors read each field at its offset
out of the array you hold, and `<Table>Block.open` / `<Root>Cook.open` take
`(byte[] data, int offset, long length)` and answer a handle or `null`. The
base's ALIGNMENT is that offset's residue rather than an address's: the same
arithmetic, so the same refusals.

**The per-frame path is the pair, not the record.** `<field>Count()` and
`<field>At(i)` read the triple out of the instance and answer ints, so a frame
loop allocates nothing; `<field>()` carries the three numbers together in a
`TableBlockRows` record and costs one per call, which is the convenience:

```java
for (int i = 0, n = frame.shipsCount(); i < n; i++) {
    int row = frame.shipsAt(i);                    // the pitch is inside, not here
    sum += RenderShipRow.objectId(frame.data(), row);
}
frame.ships().forEach(row -> { ... });             // the convenience, one record
```

**A reference resolves through `<Root>Cook.at(slot, size)`**, which answers the
target's offset, or `-1` for a null AND for a delta whose WHOLE RECORD does not
lie inside the region. The size is the pointee's `<Name>Row.size`. Bounding only
the target's start would pass and then throw one call later, on the first field
read past the end — an unchecked deref in Java is an exception escaping a reader
rather than a pointer into trusted bytes, so the bound is over the record.

**Both accelerators stop at 2 GiB**, because a `byte[]` does. §7 is built for
catalogs past that, and C# answers it with a pointer form beside its span
overload; Java at `--release 17` has no second spelling, since `MemorySegment`
is not stable before 22. The `long length` on both `open`s is the seat that
overload takes when the floor moves — it is a named follow-on, not an oversight,
and until it lands a cook past 2 GiB has no Java reader.

**The Elixir surface is the same three functions again**, module functions
on the declaring file's `<Base>Table`, over a struct the caller owns. There is no
buffer to pass and no instance to reset: `save` returns the binary and `load`
starts from the declared defaults, because a BEAM term is immutable and there is
nothing to clear.

```elixir
alias Example.ShipConfig
alias Example.ShipTable

ship = %ShipConfig{health: 250.0, settings: %Example.Settings{}}
#                                 ^ ?T: a value is presence, nil is absence

size = ShipTable.measure_ship_config!(ship)  # exact, writes nothing
wire = ShipTable.save_ship_config!(ship)     # byte_size(wire) == size

{loaded, report} = ShipTable.load_ship_config(wire)

if report.malformed do
  # framing damage: the good prefix is in `loaded`
end

if not Example.TableRuntime.silent?(report) do
  # unknown, kind_mismatch, clamped: the data did not match this build exactly
end

case ShipTable.save_ship_config(ship) do  # the plain form, for a value you did not build
  {:ok, wire} -> store(wire)
  :error -> :unspellable                   # an enum or key outside its named variants,
end                                        # or a storage invariant violated (§5)
```

**ONE REFUSAL SPELLING over the whole surface.** Everything that can refuse
answers `{:ok, result}` or `:error`, which is the packet emitter's own reader
verdict: `measure`, `save`, `to_json` and `to_json_measure` refuse a value with
no spelling — an enum, key or union tag outside its named variants, a storage
invariant violated, a non-finite float in the text form — and `block_open` and
`cook_open` refuse bytes that are not this build's. Each of the four writers has
a bang form beside it that answers the result or raises `ArgumentError`, which
is the packet emitter's writer contract; `_at` and `_put` raise the same way,
because a key no slot answers is misuse rather than data.

**THE REPORT IS A VALUE THE CALLER OWNS**, threaded rather than pointed at: the
BEAM has no mutable struct, so `load` hands it back beside the instance. One
report, one caller, the same six counters as everywhere else.

**Storage is SPEC §6.1's Elixir column and nothing new.** A `string(N)` and a
`bytes(N)` are binaries whose `byte_size` IS the used length; a `[..N]T` is a
list whose `length` IS the count; an enum is its ordinal and a `flags` its mask.
There is no `_length` and no `_count` companion, because there is nothing to
keep one in step with — the value cannot disagree with itself.

**An enum-keyed array's slots are a TUPLE on a `table`**, one per named variant,
so a slot is reached in constant time. **On a `type` they are the packet
emitter's LIST**, because a type's struct is the packet emitter's — it builds
the array with `List.duplicate/2` — and the table wire changes nothing about a
type's storage; `_at` is `Enum.at` there, a walk over the slots rather than a
reach, and the generated accessor says so at its site. In both, `None` keys no
slot and the storage shifts left, which is the same rule every backend follows.
**The shift is never written at a call site** — three generated functions are
the only place it appears, and they take the KEY:

```elixir
ship  = ShipTable.fleet_ships_at(fleet, Example.ShipType.bomber())
fleet = ShipTable.fleet_ships_put(fleet, Example.ShipType.bomber(), ship)

for {ship_type, ship} <- ShipTable.fleet_ships_each(fleet) do
  # ship_type is the KEY, never an index
end
```

`_at` and `_put` refuse `None` and a key past `E.Max` with an `ArgumentError`,
**in every build** — the BEAM has no compile-out assert, so the guard the other
ports have to argue for costs nothing to keep here. Iteration reads and `_put`
places, because a BEAM value is immutable.

**What Elixir does NOT do, and it is the language rather than the port:** it
never PRODUCES a block or a cook. A BEAM term has no layout a producer could
write. It OPENS one another build wrote and reads every slot at its offset —
`ExampleBlock.block_open_render_frame(bytes)` and
`ExampleCook.cook_open_scene(bytes)` — with a row handed back as a SUB-BINARY
the runtime shares rather than copies. Both take an optional `lead` beside the
bytes, which is how many bytes past an aligned base the caller's buffer begins:
§7 and §19 check the alignment of the BASE, and a BEAM binary has no address a
caller can observe or place, so the caller states it and the check stays a real
one — a block refuses any `lead` that is not a multiple of 64, a cook any that
is not a multiple of its own alignment word, and `make tables-elixir-block-lead`
holds the rule over every lead in 0..64.

**And what it cannot claim: "the read path allocates nothing."** There is no
caller-owned buffer and no mutable struct, so a decoded value IS an allocation.
What the backend holds instead is that the COUNT does not move — the heap words
and the reductions one iteration costs are pinned per case and re-pinned
deliberately, with a negative control that sabotages the emitter so every
generated load allocates more, and reds every case on both memory columns.

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
tables give you collections, and a bounded array of a union whose arms are
tables — `[..N]ToolBody` — is a batch of messages in one field. That is the
whole recipe for a config or asset
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
    tier     ?int32                          // scalars too
    loadouts ?[..MaxWeapons]WeaponConfig     // and bounded arrays of them
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

`?` goes on nested tables and types, enums, flags, scalars, and bounded
arrays of those — `?[..N]T` and `?[N]T`. An optional array rides whole when
present, its live count included even at zero, so "present and empty" and
"absent" stay two values — the thing a bare count cannot spell:

```cpp
ShipConfig ship;
ship.loadouts_present = true;   // present, and empty: rides as count 0
ship.loadouts_count   = 0;
```

`?` is refused on a pointer (already optional), a union (`None` is its
absence), and on strings and `bytes` — those carry a length whose framing
already rides when the field does; wrap one in a table and make that
optional. It is also refused where the value could hold pointer edges — a
variable-length closure, an array of pointers, an array of unions — until
the authoring walks gate on presence (a named follow-on), and on an
enum-keyed array, whose slots elide by name. An optional takes no
specified default: presence is its only default.

### Enum-keyed arrays: `ships [ShipType]ShipConfig`

An array bound that names a declared ENUM gives you exactly one slot per NAMED
variant, keyed by the variant:

```
enum ShipType { Fighter, Bomber, Scout }

table Fleet
{
    ships [ShipType]ShipConfig   // one slot per named variant, keyed by the variant
}
```

```cpp
Fleet fleet;

for ( auto [ ship_type, ship ] : fleet.ships )   // every slot; the KEY runs 1..Max
{
    ship.health = DefaultHealth( ship_type );    // the element is a reference
}

ShipType key = TypeFromConfig();                 // keys are runtime values
fleet.ships[key].health = 400.0f;                // asserts key != None
```

Storage is a generated keyed-array type wrapping a plain
`ShipConfig[ShipType.Max]` — no count companion, because every named slot
exists — so the memory is the array you would have written by hand, with the
accessor and the iteration on top of it. **Nothing is stored for `None`, and
the storage shifts left**: `None` is the enum's null, so it keys nothing and
takes no room, and the key `k` lives at index `k - 1`. The type is
`TableKeyed<T, E>` and derives its extent from the enum: nothing outside the
array names its size.

**Indexing by `None` is refused in every build.** Keys in a data-driven
program are runtime values — an enum read out of a file, a key a tool hands
you — so `operator[]` is the accessor every call site uses, and it ends the
program on `None` rather than reading something. This is **not** a debug
guard: `NDEBUG` does not remove it, so there is no configuration in which a
`None` key reads one element before the array. C++ raises `schema_assert` for
the message and then `schema_fatal` — your own handlers if you defined them
(see [the hooks](#the-c-runtimes-hooks-schema_assert-schema_fatal-schema_allocate)),
`assert` and `abort` otherwise; C# throws. The cost is one predictable compare.

**Iterate, and the key rule never reaches your code at all.** Iteration
yields the KEY, `1 .. Max`, so a consumer of the whole array writes no lower
bound, no cast, no shift and no `Max` of its own, and hands over no key to be
refused. Iteration is const-correct, and a const keyed array yields const
elements.

The entry is a **proxy handed out by value** — a key beside a reference — so
the spelling is `for ( auto [ key, element ] : keyed )`. `auto & [ ... ]` is a
compile error by design: a non-const lvalue reference cannot bind to the proxy.
Write `auto [ ... ]`, or `auto && [ ... ]` if you want the reference form; the
element is a reference to the slot in every case. `begin()`, `end()` and
`size()` are the whole iteration surface: the iterators carry no
`iterator_traits` typedefs, so a hand-written forward pass works and an STL
algorithm does not — a keyed table header includes no `<iterator>`.

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

In a `type` body the same spelling lowers to exactly `[E.Max]T` — positional
and bitpacked, the packet wire as always, with the same protocol id either
way — and there the storage is a **plain array**: `per_team [Team]int32` in a
`type` is `int32_t per_team[3]`, no accessor, no iteration surface and no
`None` guard, because there is no key to check and no `None` slot to guard.
Only the table wire keys the slots.

**And the positional spelling `[E.Max]T` is REFUSED in a table body, by
name**, with `[E]T` named as the fix. *The specification states that refusal
and the checker does not make it today: `[E.Max]T` in a table body still
compiles ([#540](https://github.com/mas-bandwidth/schema/issues/540)).*
An ordinal-indexed array is a positional
vocabulary, and a table has exactly one of those — `flags` — so the refusal is
what keeps the closed class closed: you cannot reopen it by spelling the array
the old way and then never touching the field again. `[E.Max]T` stays legal in
a `type` body, where it is a plain array (SPEC-TABLES.md §2.4, §11).

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
table OpenDocument
{
    path string(256)
    line uint32
}

table SaveDocument
{
    path  string(256)
    force bool
}

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
only — types stay value semantics. *The C++ reference and the tool carry the
form; every other backend refuses a unit that declares one, by name, and the
ports are a named follow-on
([#392](https://github.com/mas-bandwidth/schema/issues/392),
SPEC-TABLES.md §15).*

An arm is a field line, so an arm's type is any type a field's is — a scalar
with its bounds, a compressed float, a string, a bounded array, an enum, a
`flags` mask, a declared `type`, a `table`, a pointer, another union — and an
arm may carry **no payload at all**, written as its name alone:

```
union Value
{
    count   int32 | min = 0, max = 100
    label   string(64)
    samples [..8]float32
    doc     OpenDocument
    ping
}
```

`ping` selects and carries nothing, which is not the union's `None`: `None`
says no arm was selected. What an arm may NOT take is a specified default, a
`?`, a `was`, a `json`, an enum-keyed `[E]T`, an `if` guard, a `map` or an
unbounded `[]T` — each refused by name (SPEC-TABLES.md §2.6).

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
never a type body. `[..N]*T` and `[N]*T` are arrays of pointer slots, each
null until assigned and free to share a node with any other slot; a keyed
`[E]*T` and a specified default on a pointer are refused by name.

**Do not reach for a pointer to make a field optional.** Every field on this
wire is already optional — absence is the reader's default — and a group of
fields that is present or absent as a unit is spelled `?T`:

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
another worker allocated is your own synchronization. `Lock` and `Save`
are single-threaded. The reflection descriptors are immutable
constant data, so reading them needs no synchronization at all.

A `<Name>Builder` is about 8 KB (it carries the arena's segment table inline),
which is fine on the stack and fine as a member; it is not something to put in
an array of thousands.

Reading from the wire, the caller owns the allocation as always:

```cpp
int64_t attribution = 0;
int64_t need = SceneLoadMeasure( wire, wire_size, &attribution ); // exact, reads no values
uint8_t * region = your_allocator( need );
TableReport report;
const Scene * scene = SceneLoad( region, need, wire, wire_size, &report );
```

`LoadMeasure` is one scan of the wire's framing and it answers with both halves:
the DATA bytes and, in the out-parameter, the ATTRIBUTION bytes — the node
directory the load fills so an index resolves whichever way it points. The
answer is their sum, and the attribution can be released once `Load` returns.
Pass `NULL` (or nothing) if you only want the total.

On the wire a pointer rides as an **index into a flat node table**, under a
kind of its own, encoded as the same canonical variable-length integer every
length and count on that wire uses: every reachable node is written once, and
a pointer field carries the number and not the body. So **two pointers to one
node are one node**, on the wire as in a region, and a chain's length is not a
nesting depth — there is no depth cap in either direction. Null pointers are
simply absent.

A by-value `T` and an optional `?T` are one framing and a field may move
between them with no byte moving; a POINTER is its own kind, so moving a field
to or from `*T` is a reported kind mismatch rather than a silent
reinterpretation. A data cycle is refused at measure, save and `Lock`, naming
the reference that closes it.

**In C, the same shape without member functions.** C++'s builder methods are
free functions under the root's name, and the two things C++ distinguishes by
TYPE — which sink a node comes from, which encoding a walk is reading — are
distinguished by a nullable member:

```c
SceneBuilder builder;
TableSink sink;
TableCtx ctx;
Scene * root;
int64_t wire_bytes, region_bytes;

scene_builder_init( &builder );            /* the arena, and the root node */
root = scene_builder_root( &builder );     /* NULL once locked */

sink.region = NULL;                      /* allocate in the ARENA */
sink.worker = &builder.main;             /* one worker per thread (§6.4) */
ListNode * head = list_node_emplace( &sink, &root->head );
head->value = 1;

/* the wire, straight out of the MUTABLE form: the ctx names the arena */
ctx.arena = &builder.arena;
wire_bytes = scene_measure( &ctx, root );
scene_save( &ctx, root, buffer, wire_bytes );

scene_builder_lock( &builder );            /* one way; there is no unlock */
/* the const form is builder.region, builder.region_bytes bytes of it, and a
   NULL context is what says "a packed region" to every walk */
{
    const Scene * packed = (const Scene *) builder.region;
    const ListNode * first = list_node_at( NULL, &packed->head );
    (void) first;
}
scene_builder_shutdown( &builder );
```

Reading from the wire is the same three steps, and the caller owns the region:

```c
region_bytes = scene_load_measure( wire, wire_size );   /* exact, reads no values */
uint8_t * region = your_allocator( region_bytes );
TableReport report;
memset( &report, 0, sizeof( report ) );
const Scene * scene = scene_load( region, region_bytes, wire, wire_size, &report );
const ListNode * node = list_node_at( NULL, &scene->head );  /* one add */
```

**The C port still writes the EARLIER wire**, and that is the one place its
surface is not C++'s in a way a reader has to know: a pointee rides inline as a
nested body rather than as an index into a node table, so two pointers to one
node write two bodies there, a chain's length is a nesting depth, and
`scene_load_measure` takes no attribution out-parameter because no directory
rides. Carrying the flat form to it is schema#349. Everything else on this page
is the same shape in both.

**The builder's members ARE its accessors.** C++ has `AsConst`, `Region`,
`RegionBytes` and `Locked`; C has `builder.region` — the packed const form,
NULL until `Lock` succeeds — `builder.region_bytes`, and
`builder.arena.locked`. `scene_builder_root` returns NULL once locked, which is
what sends you to `builder.region`.

**`<name>_at` and `<name>_emplace` exist only for a table something POINTS
AT**, which is the same rule C++ follows: a table nobody points at needs
neither. The spelling is C's own — this target's packet half writes
`read_ship_config`, so its table half writes `ship_config_load`, and §11's
`<Name>At` is `<name>_at` here for the same reason it is `<name>_at` in Rust.

### The C++ runtime's hooks: `schema_assert`, `schema_fatal`, `schema_allocate`

The generated C++ is the dialect of `serialize.h`: C header spellings
(`<stdint.h>`, `<string.h>`, `<stddef.h>`), no STL, no `<type_traits>`, no
`<iterator>`, and every call the runtime makes into the C library goes
through a macro you can define first. A keyed table header pulls in 126
headers and preprocesses to 172 KB — 542 and 1 MB before — and 68 headers
and 110 KB once you define the hooks; a packet header is 36 headers. Define
them once, before the first generated header, and every unit in the program
picks them up:

```cpp
#define schema_assert( condition ) my_assert( condition )
#define schema_fatal() my_fatal()                      // must not return
#define schema_allocate( bytes ) my_calloc( bytes )    // ZEROED bytes, NULL on failure
#define schema_release( pointer ) my_free( pointer )
#include "SceneTable.h"
```

- **`schema_assert`** is the runtime's assert. The keyed accessor's refusal
  raises it with its message. The default is `assert` from `<assert.h>`, and
  `NDEBUG` removes it exactly as it removes `assert`. A program that already
  routes the packet half's asserts writes `#define schema_assert
  serialize_assert` and both halves land in one handler.
- **`schema_fatal`** is what stands after the assert on a path that cannot
  continue; `NDEBUG` does not remove it. The default is `abort` from
  `<stdlib.h>`. Define it and a fixed-class header includes no `<stdlib.h>`.
- **`schema_allocate` / `schema_release`** are the default allocator pair, in
  a unit with a variable-length table and in every block header.
  `schema_allocate` hands back ZEROED bytes — an arena segment is copied whole,
  padding included, into the packed region — and NULL on failure. The default
  is `calloc` and `free`. Define both and no generated header includes
  `<stdlib.h>`.

A unit with no keyed array emits no assert or fatal hook, and one with no
pointer emits no allocator hook in its `Table.h`; the block header always
carries the allocator hook, because `TableBlockDefaultAllocator()` lands in
it. There is no log hook, because the runtime never logs: every outcome is a
return value or a `TableReport` field.

**Per structure, hand a `TableAllocator` to the builder.** It is the shape
`TableBlockAllocator` already has — an alloc/free pair and a context — plus
the zeroed-bytes contract on `alloc`:

```cpp
static void * heap_alloc( void * context, int64_t bytes ) { return ((Heap *) context)->Calloc( bytes ); }
static void heap_free( void * context, void * pointer ) { ((Heap *) context)->Free( pointer ); }

TableAllocator pair;
pair.alloc = heap_alloc;
pair.free = heap_free;
pair.context = &my_heap;
SceneBuilder builder( pair );          // TableDefaultAllocator() when you name none
```

Everything the builder ever allocates goes through it: the arena's segments,
`Lock`'s identity map and the packed region, the numbering behind
`SceneMeasure` and `SceneSave`, and `SceneLoadBuilder`'s node directory.
`SceneMeasure( const Scene * )` and `SceneSave( const Scene *, ... )` over a
REGION take the pair as an optional last argument, because the numbering walk
is the one reading-side path that allocates. A counting allocator sees every
byte and the default pair sees none; `test/tables/hooks_main.cpp` is the unit
that holds both claims, and it is what a consumer's own translation unit
looks like. `LoadMeasure`/`Load`, `Open` and `BlockOpen` allocate nothing, as
always.

What is still not the dialect in a POINTERED unit's header — `<atomic>` and
`<new>` — and the builder's destructor are named follow-ons in
SPEC-TABLES §15.

### Maps: `ships map[string(32)]ShipConfig`

*Specified and carried by the C++ reference and the tool. Every other backend
refuses a unit that declares one, by name, because a map makes its holder
variable, and the ports are a named follow-on
([#380](https://github.com/mas-bandwidth/schema/issues/380),
SPEC-TABLES.md §11, §15).*

A map is a **lookup the runtime provides over entries the wire carries as a
sorted array**. It spends no wire kind and invents no encoding: on the wire,
in a region and in a cook a map is nothing new, just an array of one generated
`{ key, value }` table, held in ascending key order.

```
table Fleet
{
    ships    map[string(32)]ShipConfig       // a lookup by name
    by_id    map[uint32]*ShipConfig          // by number; two keys may share one node
    loadouts map[string(16)]map[uint8]Item   // a map is a value, so maps nest
}
```

**Keys are bounded strings and integers, and nothing else**: `string(N)`, or
one of `int8` through `int64` and `uint8` through `uint64`, bare. No `| min`,
no `| max` and no default, because a key is an identity and clamping an
identity merges two entries. Every other key is refused by name with its
reason. A `wstring(N)` key names `string(N)`, because `memcmp` over UTF-8 is a
portable order and little-endian code units have none. An enum key names
`[E]T`. A `bool`, a float, a `flags`, `bits(N)`, `bytes(N)`, a 128-bit
integer, a `fixed`/`ufixed`, a `type`, a `table`, a pointer, an optional and a
union are each refused too.

**A value is anything a table field can hold**, a map included, so a map of
maps is one declaration and nothing special.

**The order is total and it is the same in nine languages.** Integers compare
by value, signed for the signed kinds and unsigned for the unsigned. Strings
compare by BYTES, unsigned, a shorter string that is a prefix of a longer one
first. Never a locale, never a code point, never a case fold.

**A map makes its holder VARIABLE-LENGTH**, whatever its key and value are,
so a table declaring one rides in the arena with the pointers and has no block
form. A `type` body refuses one by name. There is no bound on the count, no
`| max` on a map, no `?map` and no default: a fresh map is empty, and an empty
map elides. What bounds a read is `LoadMeasure`, whose answer the caller owns
and can refuse.

The surface is a builder and a reader:

```cpp
FleetBuilder builder;
Fleet * fleet = builder.GetRoot();

// insert: the key is copied and the value comes back at its defaults to fill;
// a duplicate key REPLACES and hands the same entry back reset
ShipConfig * fighter = FleetShipsInsert( builder.main, fleet->ships, "fighter" );
fighter->health = 100;

ShipConfig * found = FleetShipsFind( builder.arena, fleet->ships, "fighter" );
bool erased      = FleetShipsErase( builder.arena, fleet->ships, "bomber" );
for ( auto [ key, ship ] : FleetShipsEach( builder.arena, fleet->ships ) ) { }

builder.Lock();                                  // sorts once; dead entries dropped
const Fleet * locked = builder.AsConst();

// the const form, a locked region or a loaded one or an opened cook, is ONE surface
const ShipConfig * ship = locked->ships.Find( "fighter" );  // log n, in place, NULL when absent
for ( auto [ key, ship ] : locked->ships ) { }              // ASCENDING key order
```

**`Insert` answers NULL for a key longer than `N`**, and for an arena that
cannot carve another segment, because a truncated key would be a merged entry:
NULL means not inserted, and a caller that needs the reason checks the key's
length before the call.

`Find` on a locked region, a loaded one and an opened cook is a binary search
in place, `floor(log2 n) + 1` compares with no allocation and no parse, because
the three are one encoding. For a map big enough that a cold array's compares
cost more than a hash and a probe, there is an OPTIONAL index built at load
into storage you own and never stored in a file:
`FleetShipsIndexMeasure`, `FleetShipsIndex` and `FleetShipsIndexFind`.

**A map claims eight names against its field**: `<Table><Field>` followed by
`Entry`, `Insert`, `Find`, `Erase`, `Each`, `IndexMeasure`, `Index` and
`IndexFind`. A `Fleet` with a `ships` map therefore claims `FleetShipsEntry`,
`FleetShipsInsert` and the rest, and a declaration spelling any of the eight
is refused naming the map.

**Evolution.** A repeated key on read is last-wins and counts `duplicate`. A
DESCENDING key is not something a conforming writer produces, so the map keeps
its ascending prefix, flags `malformed`, and the parent reads on. A key that
does not fit the reader's bound drops its entry and counts `clamped`, one per
entry, so keys never clamp into each other. A key kind that disagrees resets
the map to empty and counts one `kind_mismatch`, and a key kind the reader's
own WIDENS decodes exactly and counts one `widened` (*specified, and nothing
counts `widened` yet in any language,
[#523](https://github.com/mas-bandwidth/schema/issues/523)*). And a map is
byte-identical with `[..N]Pair` over a user's own
`table Pair { key ...; value ... }`, in both directions, which is the
migration path from the table-of-pairs idiom.

In JSON a map is a plain object keyed by the key, an integer key written as
its quoted decimal spelling, entries in ascending key order.

### Unbounded arrays: `placements []Placement`

*Specified, not yet emitted. The front end refuses the `[]T` spelling at
parse, no backend carries the construct, and the corpus holds no
`tables/lists` (SPEC-TABLES.md §2.9).*

**An unbounded array is a counted array whose count the DATA decides.** It is
the map with the key and the sort taken out: the same kind `14` body a
`[..N]T` writes, the same element kind, the same count. What it drops is the
declared BOUND, so the slot holds a reference and a count where a bounded
array stores its maximum inline.

```
table Save
{
    placements []Placement    // as many as the world has
    log        []*LogEntry    // pointer elements, two slots may name one node
    scores     []int32        // a scalar element is an element like any other
}
```

**`[]T` and `[..N]T` are the same bytes**, so the migration runs both ways: a
schema that outgrew its bound removes it and reads every file it ever wrote,
and one that discovers a bound adds it and reads every file too, clamping past
it.

The element set is `[..N]T`'s exactly: a scalar, an enum, a `flags` mask, a
declared `type`, a table by value, `*T` and a union, with nothing added and
nothing held back. So `[][]T` is refused because arrays of arrays are not in
v1, and the fix is a TABLE wrapper rather than a `type` one, because a `type`
body refuses a `[]T`:

```
table Row   { items []Sample }    // the inner array, wrapped
table Sheet { rows  []Row }       // the outer one, of that table
```

`[]?T` is refused too, and the answer that serves today is `[]*T` with a null
slot. `[..]T` and `[0..]T` are refused as spellings: an EMPTY bracket is the
absence of an extent, which is what the construct is.

Like a map, an unbounded array **makes its holder VARIABLE-LENGTH** and is
refused in a `type` body by name, which is what keeps "no unbounded
collections" true of the type wire. The order is INSERTION order, on the wire,
in a region and in a cook, so position is identity the way it is in a fixed
array, and there is no sort for a writer to hold and no order check for a
reader to run.

```cpp
SaveBuilder builder;
Save * save = builder.GetRoot();

Placement * placement = SavePlacementsAdd( builder.main, save->placements );
placement->x = 1.0f;
*SaveScoresAdd( builder.main, save->scores ) = 10;

bool erased = SavePlacementsErase( builder.arena, save->placements, placement );
for ( Placement * e : SavePlacementsEach( builder.arena, save->placements ) ) { }

builder.Lock();                                  // one pass, no sort
const Save * locked = builder.AsConst();

int32_t n = locked->placements.size();
for ( const Placement & p : locked->placements ) { }
```

**`Add` answers NULL when the arena cannot carve another segment**, exactly as
a map's `Insert` does, and a count past the `int32` extent cap is refused as
any array's count is.

**Erase is addressed by the POINTER `Add` handed back**, because a list has no
key and an element's address is the one thing the builder promises never
moves. **Indices are not stable across an erase**, and that is the whole of its
cost: erasing element 2 of five leaves four, and what was index 3 is index 2
in the next `Save`, `Lock` or `Cook`. A caller holding an index across an erase
is holding a stale one, and a caller holding the pointer is not.

**An unbounded array claims three names against its field** where a map claims
eight: `<Table><Field>` followed by `Add`, `Each` or `Erase`. A `Save` with a
`placements` list claims `SavePlacementsAdd`, `SavePlacementsEach` and
`SavePlacementsErase`, and a declaration spelling any of the three is refused
naming the field. It claims no `Entry`, because no entry table is generated,
no `Insert` or `Find`, because there is no key, and none of the three index
names, because there is no lookup to accelerate.

`operator[]` is bounds-checked in **every** build, `NDEBUG` included: the
extent is a number that came out of a file, so an index past it is not a
mistake a release build gets to make cheaply.

**Two counters cannot fire here, and their absence is the design.**
`duplicate` cannot, because there is no key equality, so two equal elements
are two elements. And **`clamped` cannot fire on the COUNT**, because there is
no reader bound to clamp against: a count is read, refused by `LoadMeasure`
before any decode, or damaged. A reader that declares `[..N]T` where the
writer declared `[]T` clamps at N and counts once, which is the price of
adding a bound.


### Byte buffers: `bytes(N)`, `*bytes`, `*string` and `*wstring`

A bounded blob is `bytes(N)` — N bytes of inline storage in every instance,
at a bound you declare, with its used length beside it. When the size varies
wildly per node, the bound costs you: a `bytes(65536)` field is 64 KB in
every instance whether it is used or not.

**A buffer at exactly its used size is `*bytes`, and `*string` is the same
node with a terminator** (SPEC-TABLES.md §2.5). `*wstring` is the third
spelling and does the same in UTF-16 code units — *specified, and no backend
emits it ([#522](https://github.com/mas-bandwidth/schema/issues/522),
SPEC-TABLES.md §2.5).* Each is a POINTER to a BLOB
NODE of exactly its bytes, and every pointer rule applies: table body only,
no default, no `?`, no array, no bound — and the field makes its holder
variable, so the builder is where the bytes are put:

```
table Asset
{
    name    string(32)
    data    *bytes   // the asset, at exactly its size; null when absent
    caption *string  // a string at its used length
    label   *wstring // the same, in UTF-16 code units
}
```

```cpp
AssetBuilder builder;
TableBytesSlot data = builder.AllocBytes( size );   // the node, and the bytes to write through
memcpy( data.data, bytes, size );
builder.GetRoot()->data = data;                     // the slot stores the reference
TableStringEmplace( builder.main, builder.GetRoot()->caption, text, length );
```

Reading is a VIEW — one add from the slot, no copy, off a loaded region, a
locked builder or an opened cook alike:

```cpp
TableBytesView data = TableBytesAt( asset->data );       // NULL/0 for a null reference
TableStringView caption = TableStringAt( asset->caption ); // .data is zero-terminated
```

**Null and empty are two values**: a null reference elides from the wire and
reads back as `NULL`/0, while `AllocBytes( 0 )` is a present blob of length
zero — non-NULL data, zero length. A blob is a node like any other: two
slots that store one reference name ONE node, written once on the wire and
laid out once in a region. In JSON a `*bytes` is inline base64 and a
`*string` is a string — `null` for a null reference, `""` for a present
empty blob — and a blob named from two slots has no text spelling (a string
has no first key to carry `&node`), so `ToJson` refuses that graph
(SPEC-TABLES.md §16.7).

Because schema never reads a blob, a `*bytes` is also where you put a LAZY
SUB-DOCUMENT — another document's wire bytes, carried opaquely and handed to
its own `Load` only when something asks for it. That is a pattern, not a
construct: `*bytes` plus a second call (SPEC-TABLES.md §2.5).

C++ and the tool carry the construct today; every other backend refuses a
unit that declares one, by name, and the ports are a named follow-on
(SPEC-TABLES.md §15).

### The block form: rows another language points at

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
`Save` and `Load` over the tolerant wire. **Every fixed table
also gets a third *form* of the same declaration**: one in which the table's
own bounded arrays sit out of line at a fixed pitch, and the instance at the
front of the block carries, per array, where its rows start, how many there
are, and how far apart they sit. The other side reads those three facts and
points.

You reach for it by INCLUDING it. The form is generated on the side, in
`<Base>Block.h` / `<Base>Block.cpp` beside the unit's `<Base>Table.h`, in
`<Base>Block.cs` plus the unit's one runtime home `<Package>Block.cs` for C#,
and in `<base>_block.rs` beside `block_runtime.rs` for Rust — so a project that
never blocks a table compiles none of it, and the table sources are the same
bytes either way:

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
// the storage: one extent, sized from the declared maxima, allocated ONCE
// through YOUR allocator — an alloc/free pair and a context, malloc semantics.
// TableBlockDefaultAllocator() is schema_allocate / schema_release — calloc
// and free unless you defined the hooks — for a caller with none of its own.
RenderFrameBlockStorage storage;
if ( !storage.Create( TableBlockDefaultAllocator() ) )
    return;

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

storage.Destroy();                            // through the same pair, when you are done
```

A block stays valid until the next `Begin` on that storage, which invalidates
every row pointer taken from it. Double-buffering is two storages, and it is
yours to own.

**Reading it is one check and then pointers:**

```csharp
if ( !RenderFrameBlock.Open( out RenderFrameBlock block, pointer, bytes ) )
    return;

// the blittable records are <Name>Row in the unit's own namespace — a claimed
// suffix, so nothing you declare can take it
foreach ( ref readonly RenderShipRow ship in block.Ships )
    Draw( ship );

// and the fast path a per-frame job takes: one contiguous reinterpret, at
// pitch == sizeof, with nothing per row
ReadOnlySpan<RenderShipRow> ships = block.ShipsSpan;
```

Rust reads it the same way, and its rows come back as a plain slice, because
the pitch IS the element's size and a slice is the honest view:

```rust
let Some(block) = (unsafe { RenderFrameBlock::open(pointer, bytes) }) else {
    return;
};
for ship in block.ships() {          // &[RenderShipRow]
    draw(ship);
}
```

`Open` verifies the magic, the byte order, the build version, the base's
alignment, every array's count against its declared maximum and every array's
extent, and then you index. On a refusal it hands back the reason beside the
null — the same `TableRefuseReason` a cook's `Open` fills, with `bad_layout`
for a pitch, a count, an offset or an extent the prologue states that
disagrees with this build's (SPEC-TABLES.md §19.2). *That enum is specified
and no backend writes it yet, so an `Open` answers the null alone today
([#523](https://github.com/mas-bandwidth/schema/issues/523)).*

**That alignment check binds the CONSUMER, and it is the one that surprises.**
A block's base is 64-byte aligned because the producer's storage is; a consumer
that did not allocate the block has to hand `Open` memory that is aligned too.
A pinned managed `byte[]` is not — the GC gives you no such guarantee — so a
C# consumer reading a block out of a file or a socket copies it once into
aligned native memory (`Marshal.AllocHGlobal` and round the address up, or a
`NativeArray` with a 64-byte alignment) and passes that pointer. A consumer
handed a pointer by the producer already has it. A generic consumer
does not even need the generated struct: the block descriptors carry the
projection's layout and each array's element layout beneath it, so a tool can
walk any block's rows without a type per table.

The generated C# is UNSAFE by nature, not by taste — a block is memory another
language wrote, and pointing at it without a copy is the whole point — so a
project that compiles a unit's `*Block.cs` sets `AllowUnsafeBlocks`. Nothing on
this side allocates: the bytes are yours, from wherever you got them.

**Three things to know before you reach for one.**

- **It is a same-build contract.** Both sides are generated from one
  declaration and ship together. Every row size and field offset is held
  against the compiler's own layout model on each side, in that language's own
  means: a `static_assert` in C++ and C, a const assert in Rust, a check at
  type initialization in C#, a refusing `init()` in Go, and an
  accessors-against-descriptors check in Java, which has no record layout to
  assert. A field that moves is caught there or refused by the open, not a
  garbled frame.
- **Every schema edit is a regenerate.** Append a field to the table, append
  one to a row, raise a maximum — each moves the build version, `BlockOpen`
  refuses bytes from the older build, and both sides rebuild from the one
  declaration. That is the trade for having no version machinery in the hot
  path: a block is same-build, so there is nothing to absorb and nothing to
  ask for by name. If you want data that outlives the build that wrote it,
  use the wire — which this same table still has.
- **The allocation is the maximum, once, through YOUR allocator.**
  `BlockMaxBytes` sums every array's declared maximum; `Create` takes it from
  the alloc/free pair you hand in, at build time, and the block is never grown
  and never pooled. The FILL path allocates nothing and locks nothing — that is
  an obligation on the generated code, not a promise, and the build fails if
  one appears. The bytes you hand off are only the frame's.

### The cooked form: point at a file instead of parsing it

*The TOOL, every READ side, and the C++ WRITE side are built (SPEC-TABLES.md
§7, §7.6, schema#251). `schema cook`, `schema cook-check` and `schema uncook`
produce, validate and read back cooked files, in either byte order; the C++
backend emits `<Root>Open` and the C# backend emits `<Root>Cook.Open`, for
every table; and C++ emits `<Root>Cook` and `<Root>CookMeasure` for every
table, fixed or pointered, whose bytes are the tool's. In every other language,
your tools write the cook with `schema cook` and your game opens it.*

**A cook is not a wire protocol — it is a load-trusted-data-from-tools
protocol.** A wire crosses a boundary between builds; a cook crosses none. Your
tools write one for one build, and that build loads it off its own disk. So
`Open`'s checks are IDENTITY checks — is this file for this build — and not a
trust boundary, and there is no per-node validation at load, ever: a pass over a
catalog-scale file is the parse the whole form exists to delete. A file that
did not come from your own pipeline is `schema cook-check`'s problem, run by a
person, once. If you want integrity, sign the file and verify the signature
before you open it; do not ask the loader to walk anything.

The tolerant wire is generic — it allocates, walks and parses, and any build
reads any data. When you want a big file to start instantly, cook it: the
locked region written verbatim behind a small header, laid out exactly as the
runtime reads it.

```sh
# your build writes the cook, beside the build that will read it
schema cook --root Scene --in config/ --out Scene.cook .
```

```cpp
#include "GraphTable.h"

// in the game — mmap the file or read it, then just point. Nothing is parsed,
// nothing is allocated, and nothing is walked: this is a header match and a
// cast, so a one-megabyte cook and a one-gigabyte cook open in the same time
// and a mapped file's pages are touched only as you use them.
const Scene * scene = graphdemo::SceneOpen( bytes, length );
if ( scene == NULL )
{
    // wrong build, corrupt, truncated, or a foreign byte order:
    // fall back to a wire load, which is the path that carries every version
    return load_from_wire();
}

// read it as it lies. A reference is one add through <T>At — the slot holds a
// signed self-relative delta — and a null reference is a null pointer.
printf( "%s v%d\n", scene->name, scene->version );
for ( const ListNode * n = graphdemo::ListNodeAt( scene->head ); n != NULL;
      n = graphdemo::ListNodeAt( n->next ) )
{
    printf( "  %s = %d\n", n->name, n->value );
}
```

`SceneOpen` takes `const void *` and a `uint64_t` length and returns
`const Scene *` or `NULL`. The length is unsigned because every number the
check compares comes out of the file, so all of its arithmetic is unsigned; a
caller holding an `int64_t` from a `stat` casts once, at the call site.

**The same cook, from C#.** The bytes are memory your engine already holds — an
mmap, a native allocation, or an array you pinned — and this side takes a pointer
and a length and points at it, so the generated source is `unsafe` and your
project sets `AllowUnsafeBlocks`:

```csharp
using Graphdemo;

// the region must stay put and stay ALIGNED for as long as you use the handle
// or anything you reach through it: nothing here copies, and nothing here pins
if ( !SceneCook.Open( out SceneCook cook, pointer, length ) )
{
    // wrong build, corrupt, truncated, or a foreign byte order:
    // fall back to a wire load, which is the path that carries every version
    return LoadFromWire();
}

SceneRow * scene = cook.RootPointer;

// a string is a SPAN over the region — no copy, no allocation — and a reference
// is one add through <T>Cook.At, which takes the SLOT because the delta is
// relative to the slot's own address
Console.WriteLine( Encoding.UTF8.GetString( SceneCook.Name( scene ) ) + " v" + scene->Version );
for ( ListNodeRow * n = ListNodeCook.At( &scene->Head ); n != null; n = ListNodeCook.At( &n->Next ) )
{
    Console.WriteLine( "  " + Encoding.UTF8.GetString( ListNodeCook.Name( n ) ) + " = " + n->Value );
}
```

There is a `ReadOnlySpan<byte>` overload beside the pointer form for a consumer
that already holds the bytes — the same contract, spelled differently. Its length
is an `int`, so a cook past 2 GiB is opened through the pointer form, which is
the one with the reach the cook is built for.

Two things about the C# side worth knowing before you reach for it. **A cooked
record is the same blittable `<Name>Row` struct the block form uses**, from the
same layout model, so the two accelerators share one set of records; the layout
is checked once, at start-up, and throws naming the record and the field if your
runtime disagrees. And **a pointered unit's C# WIRE surface is refused by name**:
you get the unit's `*Cook.cs` and `*Block.cs` and no Table sources at all, because the
codec for the variable class is a named follow-on and neither accelerator needs
one. Your cooked assets open in full; `Measure`, `Save` and `Load` for those
tables are C++'s or the tool's for now.

**The same cook, from Rust**, on the same two terms — the records are the same
`<Name>Row` structs the block form uses, and a pointered unit's WIRE surface is
refused by name while both accelerators are emitted in full. What differs is
where the layout is checked: Rust asserts it AT COMPILE TIME, with a const
assert per size and per offset, so a runtime that disagreed with the model
would not build rather than throw at start-up:

```rust
use graphdemo::*;

// the region must stay put and stay ALIGNED for as long as you use the handle
// or anything you reach through it: nothing here copies, and nothing here pins
let Some(cook) = (unsafe { SceneCook::open(pointer, length) }) else {
    // wrong build, corrupt, truncated, or a foreign byte order:
    // fall back to a wire load, which is the path that carries every version
    return load_from_wire();
};

let scene = cook.root();
unsafe {
    // a reference is one add through <name>_at, which takes the SLOT because
    // the delta is relative to the slot's own address, and a null reference is
    // a null pointer
    let mut node = list_node_at(&(*scene).head);
    while !node.is_null() {
        println!("  {}", (*node).value);
        node = list_node_at(&(*node).next);
    }
}
```

A cooked file is an ACCELERATOR, not an archive: it is build-locked by a
build version that covers the schema's layout and its meaning facts, and
target-locked by the byte order its header carries, so it refuses the moment
any of it moves and you
regenerate it. The tolerant wire stays the format of record.

`Open` checks the header and points — the magic, the byte order it
establishes, the build version, every reserved word zero, the region alignment
the header names, the two part lengths against the length you passed, the
root's own storage inside the data part, the base's alignment — and that is the
whole of it. On a match the bytes ARE what this build wrote, so there is
nothing to
validate and nothing to fix up, and open time does not grow with the file. It
is the only runtime entry point there is: a cook is your build's own
accelerator, and a file that is not your build's returns NULL and you load the
wire.

If you have a cooked file whose provenance you doubt — one that crossed a
machine boundary, or one you are diagnosing — check it with the tool:
`schema cook-check` scans the attribution the cook carries beside its data and
verifies every reference and every count against it, without following one
reference or decoding one value. That is a person's decision, made once, not a
flag on a load in the hot path.

A FIXED table cooks too, and its cook is the same idea with nothing in it:
one struct behind the header, so you memcpy it or point at it where it lies —
no node table and no graph, and the build version is the whole of what `Open`
checks. Its attribution part still names that one node, so `cook-check` can
bound its string lengths and array counts, which are as forgeable in one
struct as in a graph. **A root is any table**, so a fixed table has an `Open`
like every other, and it is the same call:

```cpp
const Settings * settings = graphdemo::SettingsOpen( bytes, length );
```

**And C++ WRITES a cook too.** Your tools do not have to shell out to the
compiler to lay one out: `CookMeasure` answers the whole file's length and
`Cook` writes exactly that many bytes into your buffer. The bytes are `schema
cook`'s, byte for byte — the tool is the reference and the conformance harness
holds the two to one file over every instance it carries — so a cook your
editor wrote and a cook your build farm ran the tool for are the same artifact.

```cpp
// measure, then write: the buffer is yours and nothing here allocates
const int64_t bytes = graphdemo::SettingsCookMeasure( settings );
std::vector<uint8_t> file( (size_t) bytes );
if ( !graphdemo::SettingsCook( settings, file.data(), file.size(),
                               graphdemo::TableByteOrder::Little ) )
{
    return false; // the only refusal: a capacity short of the measure
}
```

A pointered root is a region and a root pointer, never a value, so its `Cook`
takes the builder — locked or not — or a region root, the same two forms
`Measure` and `Save` take. The measure depends on the graph here, because it IS
the numbering: a node named from three places is written once, and a data cycle
is refused as it is everywhere else.

```cpp
// a pointered root is a region and a root pointer, so its Cook takes the
// builder — locked or not — or a region root, and never a value. What it
// allocates is the numbering, through the builder's own allocator.
const int64_t bytes = graphdemo::SceneCookMeasure( builder );
std::vector<uint8_t> file( (size_t) bytes );
if ( !graphdemo::SceneCook( builder, file.data(), file.size(),
                            graphdemo::TableByteOrder::Little ) )
{
    return false; // a capacity short of the measure, or a data cycle
}
```

The byte order is a PARAMETER and it is the TARGET's, not yours: pass
`TableByteOrder::Big` and a little-endian machine writes the file a big-endian
build will open. That is where the endian fix-up belongs — offline, once, on
the writing side — which is exactly why `Open` never fixes anything up.

What allocates, stated: a fixed root's writer allocates nothing. A pointered
root's allocates its numbering — the identity map, the node entries and one
offset per node — through the builder's own `TableAllocator`, or through the
optional last argument of the region overloads, and releases it before
returning; the output buffer is yours either way. One limit, stated rather than
found: `Cook` always writes the attribution part beside the data, which is what
`schema cook-check` reads; a data-only option is a named follow-on.

**The three commands, today.** They run over the same declarations the
compiler already read, and the input may be a wire file or the directory tree
`schema pack` reads (§17), so one command covers build-then-cook:

```sh
# a config tree straight to a cook, for this build, in this build's order
schema cook --root Scene --in config/ --out Scene.cook --verbose .

# ...or from a wire file, for a big-endian target
schema cook --root Scene --in Scene.bin --out Scene.cook --byte-order big .

# validate one whose provenance you doubt — offline, once, on purpose
schema cook-check --root Scene --verbose Scene.cook .

# and back to the wire, which is what proves the cook lost nothing
schema uncook --root Scene --in Scene.cook --out back.bin .
```

`--verbose` on a cook prints the header facts you key a store by — the build
version, the byte order, the root, the node count and the two part sizes. And
because the attribution is SEPARABLE, `--attribution <file>` writes the
directory beside the cook instead of inside it: the cook then carries data
alone for a build that ships no tooling, and `cook-check --attribution <file>`
puts the two back together when you want to check one.

### The build version: what a cooked asset is stored under

*`schema build-version [--facts]` prints the id and the projection it digests,
both pinned as goldens; every backend emits it — as a constant beside the
block form, or as a file of its own where the language wants one
(`BuildVersion.java`, `BuildVersion.ex`, `build_version.rs`) — and the
producing backends stamp it into every block's prologue while the reading
ones emit it to compare against. `schema cook` stamps the same id into every
cooked header, and `cook-check` and each port's cook entry point — the C++
`<Root>Open`, the C `<root>_open`, the C# `<Root>Cook.Open`, the Dart
`<Root>Cook.open`, the Elixir `cook_open_<root>`, the Go `<Root>Open`, the
Java `<Root>Cook.open`, the JavaScript `<Root>Cook.Open` and the Rust
`<Root>Cook::open` — read it back and compare. What is still owed is
SPEC-TABLES.md §20's status list.*

A cook is only ever produced for one build, so something has to name which
build. That is the **build version**: one digest over everything a cook's
bytes depend on — your protocol id, every record's layout as the compiler
computes it, and the declaration facts that decide what a load puts in a slot
(a specified default, a declared range, an enum's variant order, a union's arm
order). **It is TARGET-NEUTRAL**: the byte order rides its projection as a
generation input and is `little` for every target today, so one id is shared
by every target of one game and a `--byte-order big` cook of the same tree
stamps the same number as the little one. What tells two orders apart is the
cook's own header, and the third coordinate of the key below.

```
$ schema build-version tables/block/
0x6e4b803407267d82
```

```cpp
// generated into <Base>Block.h, and stamped into every block's prologue
printf( "%016llx\n", (unsigned long long) blockdemo::BuildVersion );
```

Your tools cook asset X to build version Y for byte order Z and write
`(X, Y, Z)` into the store;
your game asks the store for `(X, Y, Z)`. That is the whole protocol. You never
have to reason about which edits invalidate what — anything that would change
a cook's bytes moves Y, so the key moves with it, and a new Y is simply a new
cook the build cache absorbs. Z is there because the build version is
target-neutral: without it a store shared by a little-endian and a big-endian
target would key two different artifacts the same way, never serving wrong
bytes — the header still refuses — but missing forever. The asset hash is the
hash of the WIRE file you cooked from.

It is settled by the **compiler**, not by your C++ compiler, which is what
lets tooling cook before any game binary exists. The layout half comes from
the compiler's own C ABI model, and the generated code asserts it on every
side — but WHEN it tells you differs by language, and only C++ tells you at
build time. C++ `static_assert`s every `sizeof`, `alignof` and `offsetof`,
so a compiler that lays a record out differently fails the BUILD, naming the
type and the field. C# has no `static_assert`: the generated
`TableBlockLayout.Verify()` and `TableCookLayout.Verify()` run once at first
use and THROW, naming the type, the field, the offset it found and the offset
the C++ side asserts — loud and early, but at run time. Both cover every
cookable record of a unit that declares a table; what is still owed is
SPEC-TABLES.md §20's status list.

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

**`was` names the FIRST wire name, forever.** Rename `speed` again and the
attribute does not move with it — it still says `was = "velocity"`, because
that is the name every stored byte was written under. Aiming the new one at
the intermediate spelling instead (`max_speed | was = "speed"`) hashes a name
no file ever carried, and every stored value orphans; a committed baseline
refuses exactly that and names the spelling that is right. And `was` keeps
the WIRE id, not the TEXT key — the JSON key is the field's own name (see the
text form) — so pair `json = "velocity"` when an existing text file has to
keep reading.

### The tables baseline: catching the edits the wire cannot report

Think of a save game. A player's file was written two years ago by a build
nobody has any more, and today's build has to read it. Almost every schema
edit since is safe by construction — fields came and went, an enum grew,
bounds moved — and the wire reports whatever it cannot use. **Exactly four
edits are different**: they change what an OLD file MEANS, and nothing on the
wire can tell you. Two are below; the third is a field's REFERENT dropped or
swapped for one that cannot stand in for it — an enum-typed field respelled
as its raw `uint16`, say, which rides under the same kind either way — and it
is the one this file's whole job is; the fourth is a `fixed` field's `F`
moved, where `fixed(16, 16)` and `fixed(8, 24)` ride under one kind and a
stored raw value reads back at the new scale (SPEC-TABLES.md §4.1). A fifth
edit belongs to the class and the baseline cannot see it yet: REUSING a name
you retired, where a re-added field takes the wire id of the one it replaced
— the retired-names ledger is schema#441.

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
Also refused: a second `was` aimed at the field's intermediate spelling,
naming both names and the one that is correct.
Warned, because the read report already counts what is lost: a bound or a
string capacity shrunk, a declared range tightened from either end (or
declared where the field had none — every stored value outside it clamps on
load, and the next save writes the clamped value), an enum variant or a union
arm removed, a declaration renamed or otherwise no longer covered. Warned
too, because it is the shape a bare rename leaves: a field removed AND a
field added in one table in one edit, naming both and suggesting the `was`
and `json =` that declare it — a warning and not a refusal, because two
independent edits in one commit are perfectly ordinary. Passed in silence,
because the wire absorbs it: fields added, removed, reordered or renamed
under `was`; variants added anywhere; flags variants APPENDED; bounds,
capacities and ranges grown.

Renaming a declaration never raises a verdict of its own: it warns, saying
which declaration carries the old one's contents on, and the contents keep
being judged by their own walk — so a dropped variant draws the same warning
whether or not its enum was renamed in the same edit.

The whole thing is opt-in — no `tables.baseline`, no check — and the first
one you write covers only what comes after it: data written before it existed
was written against a shape nobody recorded. So `schema check` says so once
for a unit that declares a table and has no baseline:

```
$ schema check configs/
notice: shipdemo declares 1 table and configs holds no tables.baseline — save-game
evolution is unguarded (SPEC-TABLES.md §18); commit one with: schema tables-baseline
--update --reason "first baseline" configs
```

That is a notice, not a failure: the exit code is untouched, and committing a
baseline silences it.

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

Tables are generated for every `--lang` today — C++ and C carry both
classes, the other seven the fixed class, and a pointered unit's WIRE
surface is refused by name in those seven while their two accelerators read
one in full. Every scalar the type wire carries rides in a table: `fixed`/`ufixed`
as their raw scaled integer under a fixed kind of their storage width, with
the whole-unit bounds clamping on the raw scale and the text in whole units
(`1.5`), `int128`/`uint128` under kinds of their own, sixteen bytes low
half first, and `wstring(N)` under a wide-text kind of its own, its code units
two bytes each little-endian (SPEC-TABLES.md §3, §16.2). What stays off the
table wire: `const`/`reserved`/`align` (bit-position constructs — the table
wire has no bit positions). Extents have
no wire ceiling: every length, count and index rides as a canonical
variable-length integer with 64 bits of capability, so the only limit is the
language's own int32 storage cap.

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
layout. Every declared scalar arrives under its table-wire kind, the
fixed-point family and the 128-bit integers included: a fixed field carries
its `F` in `frac_bits` and its exact raw range in `wide`, which is what lets
the text form read and write it without a double on the path (SPEC-TABLES.md
§8.1).

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

The same three in C#, where they are members of the unit's `Schema` class and
the buffers are spans you own:

```csharp
TableReport report = new TableReport();
ShipConfig ship = new ShipConfig();
Schema.ShipConfigFromJson(ship, text, report);       // text is ReadOnlySpan<byte>

long size = Schema.ShipConfigToJsonMeasure(ship);    // exact, writes nothing
byte[] buffer = new byte[size];
Schema.ShipConfigToJson(ship, buffer);
```

There is nothing extra to add to a C# build: a unit's files compile together,
so the walk is already in the `<Package>Table.cs` you compile for the wire
codecs. The read path allocates nothing beyond the instance you passed in —
strings land in the field's own `byte[]` storage, and keys, names and number
tokens are handled in stack buffers.

The same three in Rust, as free functions over slices you own:

```rust
let mut report = TableReport::default();
let mut ship = ShipConfig::default();
ship_config_from_json(&mut ship, text, &mut report);   // text is &[u8]

let size = ship_config_to_json_measure(&ship);         // exact, writes nothing
let mut buffer = vec![0u8; size as usize];
ship_config_to_json(&ship, &mut buffer);
```

There is nothing extra to add to a Rust build either, for the same reason: a
unit is one crate, so the walk is already in the `table_runtime.rs` the crate
root declares. It reads through the descriptors' storage offsets — one of the
four places the generated table code is `unsafe`, all four listed under **Rust**
in [Per-language notes](#per-language-notes) — and it **allocates nothing**:
numbers format through a stack sink, strings and keys land in the field's own
storage, and `make tables-rust-alloc-audit` counts zero on every read and write
path of every instance in the corpus.

And the same three in Go, package-level over a `*T` and a `[]byte`:

```go
var report example.TableReport
var ship example.ShipConfig
example.ShipConfigFromJson(&ship, text, &report)     // fills ONE instance

size := example.ShipConfigToJsonMeasure(&ship)       // exact, writes nothing
buffer := make([]byte, size)
example.ShipConfigToJson(&ship, buffer)
```

Nothing to add to a Go build either, for C#'s reason: a package compiles as a
whole, so the walk is already there. It lands in its own `<Home>TableJson.go`
all the same, so what it costs is legible — and the LINKER drops what nothing
calls, so a binary that never reads or writes a text carries none of it. The
Go read and write paths allocate NOTHING at all, measured rather than asserted.

In JavaScript the text is a string — the language's own currency for it — so
there is no measure call and no buffer: `ToJson` answers the text, `FromJson`
takes it (or the `Uint8Array` of one read straight off a file) and hands the
value back, and anything that is not text is a `TypeError`, not a malformed
report:

```js
import { ShipConfigFromJson, ShipConfigToJson, TableReport } from "./ConfigTable.js";

const report = new TableReport();
const ship = ShipConfigFromJson(text, report);       // one instance, from a string
const back = ShipConfigToJson(ship);                 // the canonical text, as a string
```

Every writer ends the text with exactly one newline, and every reader accepts a
text with or without one — so a text is the same text in a file, in a diff and
in a pipe.

**A pointered table has the same text**, read into its builder and written
from a region's const root, in C++:

```cpp
SceneBuilder builder;
TableReport report;
SceneFromJson( builder, text, text_bytes, &report );   // every node lands in the arena
builder.Lock();

int64_t size = SceneToJsonMeasure( builder.AsConst() );
SceneToJson( builder.AsConst(), buffer, size );
```

A pointer is spelled as the nested object it points at, or `null`, so a
tree-shaped scene reads like any fixed table's text. The one thing a tree
cannot say is sharing, and it gets one construct: a node named more than once
is written once with `"&node": N` as its first key and its fields after it, and
every later slot that names it holds `{ "&node": N }` and nothing else.

```json
{
  "head": {
    "&node": 1,
    "value": 7,
    "next": null
  },
  "alias": {
    "&node": 1
  }
}
```

The label is the text's counterpart of the wire's node index: the text's own,
numbered from 1 in the order the writer meets shared nodes, written only where
a node is shared, and the definition comes before every reference in document
order; a label alone that the text never defined, or a field after a label it
has already defined, is malformed — so a typo is loud rather than a silent
extra node. No field may take a key beginning with `&`, which is
what lets a reader know the construct on sight. A cycle is refused on the way
out, as `Save` refuses it, and a pointer chain nests as deep as it is long, so
the reader's depth cap bounds it. `schema pack` and `schema unpack` take a
pointered root as one `<Root>.json`.

`FromJson` places what the text mentions and leaves the rest at its declared
defaults, exactly as an absent field on the wire does; unknown keys, wrong
JSON types and out-of-range numbers land in the same report the wire uses,
plus one counter the wire never raises — `duplicate`, for a key the text gave
twice. `ToJson` writes every field, defaults included — a text is for people —
and it is pretty-printed: one entry per line, two-space indent.

The mapping is the obvious one: enums and flags by variant NAME, a union as
an object with one key, an enum-keyed array as an object keyed by variant
name, a `?T` optional present exactly when its key is present, `bytes(N)` as
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
the root table, and writes the root's wire bytes. **The tree is a POSITIONAL
argument, never a `--in`** — `--in` and `--out` name the wire FILE on both
verbs, and the tree is what the verb walks:

```
$ schema pack   --root Config --out Config.bin  configs/    # tree -> wire file
$ schema unpack --root Config --in  Config.bin  configs/    # wire file -> tree
```

The unit's own schema files come after the tree, and with nothing there they
are the working directory — so `schema pack --root Config --out Config.bin
configs/ schema/` reads the declarations from `schema/` and the values from
`configs/`. Every `schema` command ends with that same declarations argument;
what `pack` and `unpack` put in front of it is the tree.

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

`Load` writes nothing, which is the CLI's policy too: `schema fmt` is the only
command that writes a schema file. A tool that wants the canonical form on disk
as a side effect of loading opts into it, and it is two fields:

```go
c.FormatInPlace = true
c.OnFormat = func(path string) { fmt.Printf("formatted %s\n", path) }
```

For formatting on its own there is `compiler.FormatFile(path)`, which
canonicalizes in place and reports whether the file actually changed, and
`compiler.Format(path, src)`, which formats bytes and touches nothing, for a
drift check that must not repair what it is measuring.

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

Dart also carries the **TABLE wire** (docs/SPEC-TABLES.md): a unit that
declares tables grows `<Base>Table.dart` beside its packet library, plus
`<Base>Block.dart` and `<Base>Cook.dart` for the two accelerators, and one
runtime home per unit and per surface. The verbs are **methods on the value**
— `measure()`, `save(out)`, `load(bytes, report)`, `fromJson`, `toJson` — on
a table's own class, and as extension methods on a `type`'s packet class —
and the caller owns everything: the value, the bytes, the report. The example
below is the conformance corpus's own `RootConfig` (`tables/examples`), and
`make tables-dart-usage` runs it verbatim off this page.

```dart
import 'TablesTable.dart';      // the file's own table library
import 'TabledemoTable.dart';   // the unit's runtime home, named for the package

final config = RootConfig();
final report = TableReport();

// read some bytes another build wrote
if (!config.load(wire, report)) {
  // framing damage; the instance holds what was placed before the stop
}
if (report.unknown > 0) {
  // newer data: fields this build does not know, skipped and counted
}

// and write it back
final size = config.measure(); // exact bytes, writes nothing
final out = Uint8List(size);
config.save(out); // returns size; -1 = refused
```

**A hot loop owns its reader and writer.** `config.load` allocates exactly one
`TableReader`; a caller that reads thousands of records a frame attaches its
own instead, and then the read path allocates NOTHING — no per-field object,
no sub-view, no temporary. The reader's one currency is the `Uint8List`: a
multi-byte scalar is assembled from bytes rather than read through a
`ByteData`, so there is no second object describing the same memory to lend,
to check, or to allocate:

```dart
final reader = TableReader(wire, report); // once

for (final record in records) {
  report.clear();
  reader.attach(record.bytes, report);
  config.loadBody(reader);
  // ... use config ...
}
```

That property is MEASURED rather than claimed: `make tables-dart-alloc` counts
the VM's own new-space scavenges over a steady phase of that loop — load,
measure and save through a caller-owned reader, writer and report, over the
conformance corpus — and holds the count at zero under `dart compile`'s AOT
snapshot, the configuration a shipping consumer runs; a planted allocation per
record turns it red (`make tables-dart-alloc-negative-control`). Under the
JIT the same instrument prints its count and does not gate on it: a `double`
crossing a conversion call the JIT's inlining budget left out of line is
boxed, and where that budget runs out is the optimizer's decision rather than
the codec's — one boxed double per pass of the eight-record corpus on the wire
phase, as measured. Two further costs are the JIT's and never AOT's: a
`float32` carrying a NaN with a payload costs one boxed double, and a 64-bit
integer field holding a value outside ±2⁶² costs one boxed integer per read.

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

**Rust** — no `unsafe` in the generated PACKET code, `Result`-returning read
and write. **The generated TABLE WIRE is safe Rust too**: `<name>_measure`,
`<name>_save`, `<name>_load` and their bodies index caller-owned slices and
contain no `unsafe` at all.

`unsafe` appears in FOUR places, each by nature rather than by taste and each
carrying a `# Safety` clause:

1. **The text form's one generic walk**, which reads and writes storage
   through the descriptors' offsets — an offset and a width are not a typed
   reference.
2. **Every table's REFLECTION DESCRIPTOR**, and this one rides in every table
   module rather than in an accelerator: §8.1's `reset: fn(*mut u8)` hook takes
   a raw pointer because a generic walker holds no type to spell, and a union
   field's four descriptor columns (`read_tag`, `clear`, `select`, `payload`)
   reach a Rust enum's payload the same way. Every table carries them always.
3. **The cooked form's `Open`**, which takes a pointer and a length and points.
4. **The block form's `Open`**, which does the same.

Sites 3 and 4 are behind **cargo features**, both on by default: build with
`default-features = false` and the block and cook modules are not compiled at
all — the Rust analogue of C++'s "include the header only if you use the form"
(§19). On the corpus's widest unit that is 7,181 of 23,290 generated lines not
compiled. `--features cook` and `--features block` take one without the other.
What stays either way is the wire, the text form, the reflection descriptors
and the blittable `<Name>Row` records — a cooked record IS the blittable row,
so the record family belongs to neither feature.

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

A unit that declares TABLES also gets `<Base>Table.js` — the tolerant wire's
codecs, the reflection descriptors and the text form — plus `<Base>Block.js` and
`<Base>Cook.js`, the READ side of the two accelerators. JavaScript controls no
struct layout, so it reads a block and a cook rather than producing one: every
field is read at the offset the compiler settled, through a `DataView` with an
explicit little-endian flag at every call. `Open` answers `null` for bytes that
are not this build's — a foreign magic, the other byte order, another build
version, forged framing — and THROWS for what the caller got wrong, with the
fix in the message. The one every Node program meets first: **the view's
`byteOffset` must be a multiple of 64 for a block, and of the region's
alignment for a cook**, because that offset is the only alignment fact a
JavaScript consumer can state, and a `Buffer` under 4 KiB — which is what
`readFileSync` hands back for a small file — is carved out of a shared pool at
an arbitrary offset. Copy it into a fresh `Uint8Array` first:

```js
import { readFileSync } from "node:fs";
import { RenderFrameBlock, RenderShipRow, RenderVector3Row } from "./RenderBlock.js";

const bytes = new Uint8Array(readFileSync("frame.block"));  // a COPY: byteOffset 0, never the pool's
const block = RenderFrameBlock.Open(bytes);  // null: not this build's block
if (block !== null) {
  for (let i = 0; i < RenderFrameBlock.ShipsCount(block); i++) {
    const at = RenderFrameBlock.ShipsAt(block, i);         // the pitch is the INSTANCE's
    use(RenderShipRow.ObjectId(block.View, at));
    const p = RenderShipRow.PositionAt(at);                // a nested type: its own offset
    use(RenderVector3Row.X(block.View, p));                // read through ITS row, same module
    if (RenderShipRow.FlagsHas(block.View, at, 1)) { boost(); }  // one bit, no BigInt
  }
}
```

Every `<Base>Block.js` and `<Base>Cook.js` re-exports the unit's whole surface
— every `<Name>Row`, the nested `type` rows included — so one import serves. A
64-bit row field reads as a `BigInt` through its accessor (`RenderShipRow.Flags`,
one allocation per read); a `flags` field carries `<Member>Has(view, at, bit)`
beside it, which reads one 32-bit word and allocates nothing — the bit is the
variant's ordinal, the number `FlagNameShipFlags(bit)` takes.

A cook reads the same way, with one addition: a reference is an eight-byte
SIGNED SELF-RELATIVE delta, so `At` takes the SLOT's own offset and answers the
target's — `null` for a null reference. A delta that leaves the region throws a
`RangeError` naming the cook as corrupt, because a cook is trusted input and
that is a file `schema cook-check` refuses:

```js
import { SceneCook, SceneRow, ListNodeRow } from "./GraphCook.js";

const cook = SceneCook.Open(bytes);          // null: not this build's cook
if (cook !== null) {
  const root = cook.Region;                  // the root sits at the region's base
  const name = SceneRow.Name(cook.Bytes, cook.View, root);   // the used bytes, no copy
  let at = SceneCook.At(cook, SceneRow.HeadSlot(root));
  while (at !== null) {
    use(ListNodeRow.Value(cook.View, at));
    at = SceneCook.At(cook, ListNodeRow.NextSlot(at));
  }
}
```

The casing you meet is one rule, the packet emitter's, because a table's
closure decodes into the classes that emitter wrote: types, functions,
constants and data members are UpperCamelCase (`ship.Health`, `report.Unknown`,
`cook.Region`), and methods are lowerCamelCase (`reader.reset(bytes)`,
`teams.get(key)`, `report.reset()`). A field's wire name, a JSON key and a
variant's name stay the schema's own spelling — they are data in a descriptor,
not identifiers.

All nine are generated from the same IR and compared against each other in CI
on every push. The wire is bit-packed, so the property being checked is
bit-identity — if they ever disagree by one bit, the build fails.

---

For the exact rules — grammar, wire law, every edge case — see
[SPEC.md](SPEC.md).
