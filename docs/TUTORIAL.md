# The schema tutorial

You are going to build the network protocol and the data files for a small
space game, starting from an empty directory. By the end you will have packets
that cross the network in a handful of bytes, save games a build from next year
can still read, config files your designers edit as JSON, and cooked assets
your game opens without parsing a byte.

The game is called Starlight. It has ships. That is all you need to know.

Fourteen parts. Each part adds one thing, and each part ends with something
that builds and runs. Every command on this page is shown with the output it
actually prints.

1. [A file, a package, and your first constants](#part-1--a-file-a-package-and-your-first-constants)
2. [Naming things: enums and flags](#part-2--naming-things-enums-and-flags)
3. [The first packet: types and ranged integers](#part-3--the-first-packet-types-and-ranged-integers)
4. [The rest of the field types](#part-4--the-rest-of-the-field-types)
5. [Branches, unions, and your protocol](#part-5--branches-unions-and-your-protocol)
6. [Tables: data that outlives the build](#part-6--tables-data-that-outlives-the-build)
7. [Shaping real data: optionals, keyed arrays, maps, and a config format](#part-7--shaping-real-data-optionals-keyed-arrays-maps-and-a-config-format)
8. [Pointers: when data is a graph](#part-8--pointers-when-data-is-a-graph)
9. [The text form: designers edit JSON, the game loads bytes](#part-9--the-text-form-designers-edit-json-the-game-loads-bytes)
10. [Evolution you can trust: `was` and the baseline](#part-10--evolution-you-can-trust-was-and-the-baseline)
11. [The cook: point at a file instead of parsing it](#part-11--the-cook-point-at-a-file-instead-of-parsing-it)
12. [The block form: a frame another language reads in place](#part-12--the-block-form-a-frame-another-language-reads-in-place)
13. [One schema, every language, and tools that walk it](#part-13--one-schema-every-language-and-tools-that-walk-it)
14. [The tool belt, and where the edges are](#part-14--the-tool-belt-and-where-the-edges-are)

[USAGE.md](USAGE.md) describes every feature on its own terms. This page is
the path through the same material in the order a newcomer walks it.
[SPEC.md](SPEC.md) and [SPEC-TABLES.md](SPEC-TABLES.md) are the normative
references, and nothing here contradicts them.

---

## Part 1 — a file, a package, and your first constants

### The problem

Every game has numbers that two pieces of code must agree on. Starlight's
server simulates up to 64 players, the client allocates for 64 players, and
the matchmaker refuses the 65th. Write `64` three times in three codebases and
one day one of them says 32.

You want to declare that number once, in one place, and have every language in
the project read the same declaration.

### Get the compiler

The compiler is one Go program with no dependencies beyond the Go toolchain.
Clone the repository and run `make`:

```
$ make
go build -ldflags "-X github.com/mas-bandwidth/schema/v2/internal/version.version=v2.4.0-143-g1e3c53b" -o bin/schema ./cmd/schema
```

That is the whole install. `make` builds `bin/schema` and nothing else: no
language toolchains, no runtime checkouts, no generation. Ask the binary what
it is:

```
$ schema version
schema v2.4.0-143-g1e3c53b (go1.27.0)
```

Write that number down. The language moves, and a diagnostic that cites a
specification section cites it as it stands in the build that printed the
diagnostic. When this page and your binary disagree, `schema version` is the
first thing to check.

### A schema file

Make a directory for your protocol and put one file in it. The name of the
file is yours, the extension is `.schema`:

`starlight/Game.schema`

```
package starlight

const MaxPlayers = 64
const TickRate = 60
const TickInterval = 1.0 / TickRate
const Pi float32 = 3.14159265359
```

Two things about the shape of the file.

Every file opens with `package <name>`. A directory of schema files is one
**unit**, every file in the unit declares the same package, and the package
becomes the C++ namespace, the Go package, the C# namespace, and so on.

`const` declares a constant. An untyped constant infers its type the way Go
does: a bare integer is `int64`, and anything with a decimal point or an
exponent is `float64`. Name a type explicitly when you want a different one,
as `Pi` does above.

Constants may reference each other in any order, across any of the unit's
files. `TickInterval` divides by `TickRate` declared the line before, and it
would work as well declared the line after, or in another file.

### Check it

```
$ schema check .
$
```

Silence. That is the tool's habit and it is worth internalizing on day one:
**success is silent**, so a build that wraps these commands stays clean.
Errors always print, whatever the verbosity. When you want to see what a
command did, add `--verbose`.

Your file is not touched. `check`, `generate`, `id` and every other command
read the unit as it sits on disk, so a read-only checkout works and a check
never edits what it checked. One command writes schema files, and only when
you ask it to:

```
$ schema fmt --verbose .
formatted Game.schema
```

```
package starlight

const MaxPlayers   = 64
const TickRate     = 60
const TickInterval = 1.0 / TickRate
const Pi float32   = 3.14159265359
```

The `=` signs are aligned. There is one style and no options, the way gofmt
works, so you never argue about schema formatting because there is nothing to
argue with.

### Generate some code

```
$ schema generate --lang cpp --out gen --verbose .
wrote gen/Game.h
wrote gen/GameWire.h
```

One header of declarations per schema file, and one header of wire functions
beside it. Splitting them means code that only wants the shapes does not pay
to compile the codec.

Flags come before the paths. `schema generate --lang cpp --out gen . --verbose`
fails, because everything after the first path is read as another path.

Open `gen/Game.h`:

```cpp
// Code generated by the schema compiler from Game.schema. DO NOT EDIT.
// SPDX-License-Identifier: NONE — this generated output is yours, under terms of
// your choice. See the LICENSE exception in the schema compiler; the compiler is
// AGPL-3.0, its output is not.
// package starlight — protocol id 0x657f3fb8771fbe6e

#pragma once

#include <cstdint>

namespace starlight {

// The unit's protocol id — the hash of its wire shape (SPEC §3.1). Two
// sides at the same id speak identical bits; there is no other versioning.
inline constexpr uint64_t ProtocolId = 0x657f3fb8771fbe6eull;

inline constexpr int64_t MaxPlayers = 64;
inline constexpr int64_t TickRate = 60;
inline constexpr double TickInterval = 0.016666666666666666;
inline constexpr float Pi = 3.1415927f;

} // namespace starlight
```

Your constants are there as real `constexpr` values, so the bound you declared
is the bound your C++ compares against with no second copy to drift. Run the
same command with `--lang go`, `--lang cs`, `--lang rust`, or `c`, `dart`,
`elixir`, `java`, `js`, and the same constants land in that language.

Note that `TickInterval` arrives as `0.016666666666666666` and not as
`1.0 / TickRate`. A constant's own expression is folded at generation, so a
derived constant reaches your C++ as a number.

Ignore the `ProtocolId` line for now. Part 5 earns it. Note only that it
exists before you have declared a single message.

### A program

`main.cpp`:

```cpp
#include "gen/Game.h"
#include <cstdio>

int main()
{
    printf( "%lld players at %lld Hz, %f s per tick\n",
            (long long) starlight::MaxPlayers,
            (long long) starlight::TickRate,
            starlight::TickInterval );
    return 0;
}
```

```
$ c++ -std=c++17 -Wall -Wextra -Werror -o starlight main.cpp
$ ./starlight
64 players at 60 Hz, 0.016667 s per tick
```

`Game.h` includes `<cstdint>` and nothing else, so this builds with no runtime
and no include path beyond your own directory. The serialize runtime arrives
in Part 3, with `GameWire.h`.

### When you get it wrong

Reference a constant that does not exist:

```
const A = B + 1
```

```
$ schema check .
Bad.schema:3:11: undefined constant B
schema: 1 error(s)
```

Give an integer constant a float expression:

```
const C int32 = 1.5
```

```
$ schema check .
Bad.schema:3:1: constant C has integer type int32 but a float expression
schema: 1 error(s)
```

Declare one name twice, in any two kinds:

```
const Team = 1
enum Team { Red }
```

```
$ schema check .
Bad.schema:4:1: duplicate declaration "Team" (first declared at Bad.schema:3:1; all declaration kinds share one unit-level namespace — SPEC §4.6)
```

That last message states a rule worth knowing early. Constants, enums, flags,
types, unions and tables all share one namespace across the whole unit, so a
constant and an enum cannot both be named `Team`.

**You now have** a schema unit, and one source of truth for Starlight's
numbers in every language you generate.

---

## Part 2 — naming things: enums and flags

### The problem

Starlight has three kinds of ship: fighters, freighters and corvettes. The
obvious move, `const ShipFighter = 0` and `const ShipFreighter = 1`, gives you
integers that any code can mix up with any other integers, no way to iterate
the set, and no way to print a name in a log line.

### enum

```
enum ShipType { Fighter, Freighter, Corvette }
```

Generate again and look at what came out:

```cpp
// enum ShipType — None = 0 implicit, variants dense from 1, wire range [0, 3] (SPEC §4.2)
enum class ShipType : uint8_t {
    None = 0,
    Fighter = 1,
    Freighter = 2,
    Corvette = 3,
    Count = 3, // the declared variant count (SPEC §4.2)
    Max = 3, // the exported extent (SPEC §4.2)
};
```

Three members you did not declare.

**Every enum has an implicit `None = 0`.** You never assign values, there is
no `Fighter = 5` in this language, and variants pack densely from 1. Zero is
reserved for "no value", so a zero-initialized enum field is the null, in
band, and you never need a separate has-flag beside an enum field.

**`Count` is the number of variants you declared**, `None` excluded. A loop
over the real ship types runs `1` to `Count` inclusive.

**`Max` is the enum's extent**, the highest wire-legal value, and the same
number you can name in a schema expression as `ShipType.Max`. Size storage and
keyed arrays by `Max`. Here `Count` and `Max` are both 3. They stop being the
same number the moment you declare headroom, at the end of this part, and that
difference is what the two words are for.

Try to declare either name yourself and the compiler refuses:

```
enum ShipType { None, Fighter }
```

```
$ schema check .
Bad.schema:3:17: variant None is a compile error — every enum has None = 0 implicitly (SPEC §4.2)
Bad.schema:3:17: enum ShipType's generated variant constant collides with enum ShipType's generated None constant — both generate the symbol ShipTypeNone; rename at the source (SPEC §4.6)
Bad.schema:3:17: variant None collides with the implicit None variant inside enum ShipType (both become the associated constant NONE in Rust) — rename at the source (SPEC §4.6)
Bad.schema:3:17: enum ShipType's generated variant constants (C form) collides with enum ShipType's generated variant constants (C form) — both generate the symbol SHIP_TYPE_NONE; rename at the source (SPEC §4.6)
schema: 4 error(s)
```

One mistake, four errors. The first line is the rule and the other three are
the same collision seen through Go's, Rust's and C's generated spellings. Read
the first one.

You also get a name function for logging, free:

```cpp
// EnumName: debug/log name for any ShipType value, out-of-set included
inline const char * EnumName( ShipType value )
{
    switch ( value )
    {
        case ShipType::None: return "None";
        case ShipType::Fighter: return "Fighter";
        case ShipType::Freighter: return "Freighter";
        case ShipType::Corvette: return "Corvette";
        default: return "???";
    }
}
```

Any value outside the set names as `"???"`, so a log line can never crash on a
bad value. Every target gets this in its own spelling: `EnumNameShipType` in
Go, C#, and JavaScript, `enum_name_ship_type` in C, Rust and Elixir,
`enumNameShipType` in Dart and Java.

A multi-line variant list is fine, because variants are comma-separated either
way and a trailing comma is allowed:

```
enum Big
{
    A,
    B,
    C,
}
```

Write the variants one per line without commas, the way fields are written,
and the parser stops:

```
$ schema check .
Bad.schema:5:6: expected }, found "newline"
Bad.schema:6:5: unexpected "B" at file scope (declarations begin with package, const, enum, flags, type, table or union)
schema: 2 error(s)
```

Variants are a comma-separated list. Fields, which arrive in Part 3, are a
block of lines. That is the one place in the language where two body grammars
sit side by side, and it is worth meeting it here rather than at your first
packet.

### flags

Ships have systems that are on or off, several at once: shields, cloak, warp
drive, autopilot. An enum is one-of and you want any-of. That is `flags`:

```
flags SystemFlags { Shields, Cloak, WarpDrive, Autopilot }
```

```cpp
// flags SystemFlags — one bit per variant, consumed as masks; storage uint64 in every
// target, wire 4 bits (SPEC §4.2)
using SystemFlags = uint64_t;
inline constexpr SystemFlags SystemFlags_Shields = 1ull << 0;
inline constexpr SystemFlags SystemFlags_Cloak = 1ull << 1;
inline constexpr SystemFlags SystemFlags_WarpDrive = 1ull << 2;
inline constexpr SystemFlags SystemFlags_Autopilot = 1ull << 3;
inline constexpr int64_t SystemFlagsCount = 4; // the declared variant count (SPEC §4.2)
```

One bit per variant, from bit 0. Storage is `uint64` in every language, which
caps a flags declaration at 64 variants:

```
$ schema check .
Bad.schema:3:1: flags F has 65 variants — one bit per variant, up to 64 (SPEC §4.2)
schema: 1 error(s)
```

There is no implicit `None`, because the empty mask is `0` and needs no name.
The declared count is exported as `SystemFlagsCount` and is usable in a schema
expression as `SystemFlags.Count`, the same word an enum carries meaning the
same thing.

There is no `SystemFlags.Max`. Ask for it and the refusal explains why:

```
$ schema check .
Bad.schema:7:30: flags F has no .Max — a flags declaration is a set of independent bits, not a range with a top; F.Count names the declared variant count (SPEC §4.2)
schema: 1 error(s)
```

Flags get a logging surface one level richer than the enum's. There is a
per-bit name, `FlagNameSystemFlags( 2 )` is `"WarpDrive"`, and a set renderer
that formats a whole mask the way a log line wants it. In C and C++ the
renderer writes into your buffer and allocates nothing:

```cpp
char names[starlight::SystemFlagsNamesMax];
starlight::FlagNamesSystemFlags( mask, names, sizeof( names ) );
```

`SystemFlagsNamesMax` bytes always suffice. The empty set renders as `"0"`,
and any bits past the declared variants render honestly as hex.

Note the explicit `starlight::` on the flags calls. A flags type is a
`uint64_t` alias, so argument-dependent lookup has no namespace to find, and
the flags helpers must be qualified. `EnumName` takes a real enum class and is
found by lookup without qualification.

### Headroom: `| max`

One more enum form, for later use. The wire cost of an enum is the fewest bits
that cover `[0, Max]`, so 2 bits for our four `ShipType` values counting
`None`. Add a fourth variant later and the width grows to 3 bits, which as
Part 5 shows changes the protocol. When you know a set will grow, reserve
headroom at the declaration:

```
enum Weapon | max = 15
{ Laser, Missile }
```

```cpp
// enum Weapon — None = 0 implicit, variants dense from 1, wire range [0, 15] (SPEC §4.2)
enum class Weapon : uint8_t {
    None = 0,
    Laser = 1,
    Missile = 2,
    Count = 2, // the declared variant count (SPEC §4.2)
    Max = 15, // the exported extent (SPEC §4.2)
};
```

The wire is now 4 bits, room for 15 variants, and stays 4 bits as you add
them. This is where `Count` and `Max` part company: 2 variants declared, an
extent of 15. Size a keyed array by `Max` and loop the real variants by
`Count`. A read accepts any value in `[0, 15]`, values you have not named
included, so a `switch` over a headroom enum keeps a `default`.

The attribute after `|` is the language's one qualification syntax, and you
will meet it constantly from Part 3 on. On a declaration line the body brace
opens on the next line, as above.

One degenerate form is worth knowing about. `enum Pending { }` declares no
variants, only the implicit `None`, so its wire range is `[0, 0]` and it costs
**zero bits**. You can declare a kind before its variants are known, fields of
it round-trip as `None`, and nothing is spent on the wire until the first
variant arrives.

### A program

```cpp
    for ( int i = 1; i <= (int) starlight::ShipType::Count; i++ )
    {
        printf( "ship type %d is %s\n", i, EnumName( (starlight::ShipType) i ) );
    }

    starlight::SystemFlags mask = starlight::SystemFlags_Shields | starlight::SystemFlags_WarpDrive;
    char names[starlight::SystemFlagsNamesMax];
    printf( "systems: %s\n", starlight::FlagNamesSystemFlags( mask, names, sizeof( names ) ) );
    printf( "weapon extent %d, declared %d\n",
            (int) starlight::Weapon::Max, (int) starlight::Weapon::Count );
```

```
$ c++ -std=c++17 -Wall -Wextra -Werror -o starlight main.cpp
$ ./starlight
64 players at 60 Hz, 0.016667 s per tick
ship type 1 is Fighter
ship type 2 is Freighter
ship type 3 is Corvette
systems: Shields|WarpDrive
weapon extent 15, declared 2
```

**You now have** named ship types and system masks, with logging names, in
every language, and no hand-maintained constants.

---

## Part 3 — the first packet: types and ranged integers

### The problem

Starlight's server needs to tell every client about every ship: what kind it
is, how much health it has, which systems are up, whether it is docked. Sixty
times a second, for dozens of ships. Hand-rolled binary packing is a reader and
a writer that drift, and JSON at 60 Hz is a joke. You want to declare the
message once and get matching, fast, safe code on both ends.

### type

```
const MaxHealth = 1000

type ShipState
{
    ship_type ShipType
    health    int32 | min = 0, max = MaxHealth
    systems   SystemFlags
    docked    bool
}
```

A `type` is a plain struct: one field per line, no commas, the name first and
then the field's type. `ship_type` and `systems` use the declarations from Part
2, because an enum and a flags are field types like any other.

The interesting line is `health`. After the `|`, the same qualification syntax
as the enum's `| max`, you give the field a **range**, and the range is part of
the type. This is the most useful feature in the language, so look at what it
generates:

```cpp
struct ShipState {
    ShipType ship_type = ShipType::None;
    int32_t health = 0; // wire [0, 1000]
    SystemFlags systems = 0;
    bool docked = false;
};

inline constexpr int64_t ShipStateMaxBits = 17; // longest wire path; align pads at worst case (SPEC §6.1)
```

```cpp
SCHEMA_WRITE_INLINE bool WriteShipState( serialize::WriteStream & stream, const ShipState & value )
{
    serialize_assert( int32_t( value.ship_type ) >= int32_t( 0 ) && int32_t( value.ship_type ) <= int32_t( 3 ) );
    write_bits( stream, uint32_t( value.ship_type ), 2 );
    serialize_assert( int32_t( value.health ) >= int32_t( 0 ) && int32_t( value.health ) <= int32_t( MaxHealth ) );
    write_bits( stream, uint32_t( value.health ), 10 );
    serialize_assert( value.systems < ( 1ull << 4 ) );
    write_bits( stream, value.systems, 4 );
    write_bool( stream, value.docked );
    return true;
}
```

Count the bits. Two for the ship type, four values counting `None`. **Ten for
the health**, because 1001 values need ten. Four for the flags, one per
variant. One for the bool. Seventeen bits, which is what `ShipStateMaxBits`
says. A hand-written struct would have spent 32 bits on the health alone. You
declared what the value *is*, between 0 and `MaxHealth`, and the wire cost
followed from it. Storage stays `int32_t`, and only the wire is narrow.

The read side validates everything it reads:

```cpp
SCHEMA_READ_INLINE bool ReadShipState( serialize::ReadStream & stream, ShipState & value )
{
    {
        int32_t enum_value = 0;
        read_int( stream, enum_value, 0, 3 );
        value.ship_type = ShipType( enum_value );
    }
    read_int( stream, value.health, 0, MaxHealth );
    read_bits( stream, value.systems, 4 );
    read_bool( stream, value.docked );
    return true;
}
```

Those bare-looking `read_int` calls are serialize macros that `return false`
out of the function on failure, so the error handling is there, hidden in the
spelling.

### Run it

Wire code targets the [serialize](https://github.com/mas-bandwidth/serialize)
library, which is header only. Add its directory to your include path.

```cpp
#include "gen/GameWire.h"
#include <cstdio>

int main()
{
    using namespace starlight;

    ShipState ship;
    ship.ship_type = ShipType::Corvette;
    ship.health = 750;
    ship.systems = SystemFlags_Shields | SystemFlags_WarpDrive;

    uint8_t buffer[64];
    serialize::WriteStream writer( buffer, sizeof( buffer ) );
    WriteShipState( writer, ship );
    writer.Flush();
    printf( "wrote %lld bytes\n", (long long) writer.GetBytesProcessed() );

    ShipState copy;
    serialize::ReadStream reader( buffer, writer.GetBytesProcessed() );
    if ( ReadShipState( reader, copy ) )
    {
        char names[SystemFlagsNamesMax];
        printf( "read back: %s health=%d systems=%s docked=%d\n",
            EnumName( copy.ship_type ), copy.health,
            FlagNamesSystemFlags( copy.systems, names, sizeof( names ) ), copy.docked );
    }
}
```

```
$ c++ -std=c++17 -Wall -Wextra -Werror -ffp-contract=off -I ../serialize -o starlight main.cpp
$ ./starlight
wrote 3 bytes
read back: Corvette health=750 systems=Shields|WarpDrive docked=0
```

Three bytes for the whole ship state, which is seventeen bits flushed to byte
granularity.

Note `-ffp-contract=off`. Generated float code must produce identical bits on
both ends of a connection, and a contracted multiply-add produces different
bits from the pair, so build wire code with contraction off.

### The reader assumes the bytes are hostile

Your game will receive these packets from the internet. Forge one by writing
the same bit layout by hand with 1013 in the health bits:

```cpp
    uint8_t evil[64] = {};
    serialize::WriteStream forge( evil, sizeof( evil ) );
    write_bits( forge, 3, 2 );        // ship_type = Corvette
    write_bits( forge, 1013, 10 );    // health = 1013, out of range
    write_bits( forge, 0, 4 );
    write_bits( forge, 0, 1 );
    forge.Flush();

    ShipState hacked;
    serialize::ReadStream check( evil, forge.GetBytesProcessed() );
    printf( "hostile read: %s\n", ReadShipState( check, hacked ) ? "ACCEPTED" : "refused" );
```

```
$ ./starlight
wrote 3 bytes
read back: Corvette health=750 systems=Shields|WarpDrive docked=0
hostile read: refused
```

A value outside its declared range is **refused, not clamped**. The read
returns false and you drop the packet. The same goes for counts past an array
bound, string lengths past their maximum, enum values above the extent, and
reads that run past the end of the buffer. This holds in all nine languages,
because the same compiler wrote all nine readers.

One buffer contract to know before this ships. The C and C++ readers pull a
64-bit word at a time, so a receive buffer's **allocation** must extend at
least 8 bytes past the packet data. Size the buffer with slack and pass the
true length. The requirement is per target: Rust and JavaScript want 8 bytes,
Go wants 7 for its fast path, and C#, Dart, Java and Elixir need none. SPEC
§6.3 carries the table.

### The write side is yours

Notice that the writes were `serialize_assert`, not checks. Writing
`health = 2000` asserts in a debug build and truncates in a C++ release build.
That is deliberate. Your simulation already keeps health in `[0, MaxHealth]`,
and re-validating every field of every outgoing packet at 60 Hz is a cost with
no buyer. The guarantee is on *reads*, where hostile data arrives. Each
language reports writer misuse its own way, and keeping your values in bounds
means none of that machinery ever fires.

### When you get the range wrong

The compiler holds your ranges to account. Each of these is a mistake you will
make in your first hour, and each refusal names the rule:

```
h int32 | min = 0
    Bad.schema:5:5: field h: min without max (or vice versa) is a compile error (SPEC §4.6)

h int32 | min = 10, max = 5
    Bad.schema:5:15: inverted range [10, 5] — min must not exceed max (SPEC §4.6)

h int8 | min = 0, max = 1000
    Bad.schema:5:5: field h: range [0, 1000] does not fit its declared storage int8 (SPEC §4.6 — a legal wire value the storage truncates would be silent corruption)

h int32 = 2000 | min = 0, max = 1000
    Bad.schema:5:15: field h: default 2000 is outside its range [0, 1000]

count int32 | min = 1, max = 4
    Bad.schema:5:5: field count: its range [1, 4] excludes zero, so the implicit zero default is outside it — declare a default in range, count = 1 (SPEC §4.6)
```

The last one deserves a beat. Everything in this language zero-initializes, so
a range that excludes zero would leave a freshly constructed value outside its
own range. The compiler makes you pick the rest state:
`count int32 = 1 | min = 1, max = 4`. That is also your introduction to
**defaults**, the `= value` before the `|`, which exist for exactly the fields
whose rest state is not zero.

**You now have** a real network message, three bytes on the wire, hostile input
refused, in any of nine languages.

---

## Part 4 — the rest of the field types

The ship state is about to grow. Each addition below is a problem Starlight
actually has, and each introduces one field kind.

### Ships have names: `string(N)`

```
const MaxShipName = 24

    name string(MaxShipName)
```

```cpp
    char name[MaxShipName + 1] = {}; // string(MaxShipName): max length, used length beside it (SPEC §4.7)
    int32_t name_length = 0;
```

A string field is a fixed buffer at the bound you declare plus a used length.
No heap, no `std::string`, nothing to allocate. On the wire it is the length in
`[0, N]`, five bits here, followed by the bytes. The bound's only job on the
wire is sizing the length prefix.

The writer holds you to a contract, visible in the generated write:

```cpp
    for ( int32_t i = 0; i < value.name_length; i++ )
    {
        serialize_assert( value.name[i] != 0 );
    }
    serialize_assert( schema_utf8_valid( reinterpret_cast<const uint8_t *>( value.name ), value.name_length ) );
```

No interior NULs, and well-formed UTF-8. A `string` is text by contract, and C,
C++ and Rust assert it in a debug build. For raw bytes with no text contract,
use `bytes(N)`, which has the same shape and no UTF-8 rule. The reader
validates the length and rejects interior NULs.

### The throttle: compressed floats

A ship's throttle is a float in `[0, 1]`. A raw `float32` field costs 32 bits,
and you do not care about the 24th decimal place:

```
    throttle float32 | min = 0, max = 1, resolution = 0.01
```

Ranged floats take `min`, `max` **and** `resolution`, all three together:

```
a float32 | min = 0, max = 1
    Bad.schema:5:5: field a: a float range is min, max and resolution, all three together (SPEC §4.6)
```

The wire carries the step index, not the float. There are 101 steps here, so
**seven bits instead of 32**:

```cpp
    {
        float compressed_value = value.throttle;
        serialize_compressed_float_precomputed( stream, compressed_value, 100u, 7, 1.0f, 0.0f );
    }
```

The write rounds to the nearest step, half away from zero, one rounding rule in
every language. Plain `float32` and `float64` with no attributes ride whole, at
32 and 64 bits.

### Position: composition

```
type Vector3
{
    x float64
    y float64
    z float64
}

    position Vector3
```

Types nest freely, and a composed field writes its parts inline with no
pointers and no indirection:

```cpp
    if ( !WriteVector3( stream, value.position ) )
    {
        return false;
    }
```

If your engine already has a vector type, `type Vector3 | cpp_native = Vec3,
cpp_include = "vec.h"` maps the generated storage onto it, so simulation code
does math directly on generated structs. One rule is worth knowing before you
try it: the mapping applies where the type is referenced from **a different
schema file** than the one that declares it. Declare `Vec2` and use it in one
file and the generated output carries no trace of the mapping. Split the
declaration and its use across two files in the same unit and the storage
spells `::GameVec2` with the include beside it.

### Heading: fixed point

Floats differ by an ulp across compilers and architectures, which is fatal for
a lockstep simulation. When a value must be **bit-identical everywhere**, use
fixed point:

```
    heading fixed(16, 16) | min = -180, max = 180
```

```cpp
    int32_t heading = 0; // fixed(16, 16) — Q16.16, raw value scaled by 2^16; bounds in whole units; wire [-180, 180]
```

`fixed(I, F)` is I integer bits, sign included, and F fractional bits, stored
as the raw scaled integer. Bounds are required and given in whole units. It is
an integer, so it adds exactly, everywhere. `ufixed(I, F)` is the unsigned
sibling for values that cannot go negative.

`I + F` must land on a storage width:

```
a ufixed(16, 8) | min = 0, max = 100
    Bad.schema:5:7: ufixed(16, 8): I + F = 24 must equal a storage width — 8, 16, 32, 64 or 128 (SPEC §4.6)
```

### Cargo: arrays

```
    cargo [..8]uint32
```

Three array spellings, all bound first, in Go's order:

| spelling | meaning | wire |
|---|---|---|
| `[4]Vector3` | exactly 4 | 4 elements, no count |
| `[..8]uint32` | up to 8 | count in `[0, 8]`, 4 bits, then that many |
| `[2..8]uint32` | between 2 and 8 | count encoded relative to 2, 3 bits, then that many |

Storage is always the full-bound C array plus a `_count` companion:

```cpp
    uint32_t cargo[8] = {}; // used count beside it; wire count in [0, 8]
    int32_t cargo_count = 0;
```

An older `[<= N]` spelling is retired, and the parser names the replacement:

```
a [<= 8]uint32
    Bad.schema:5:8: the [<= N] bound is retired — spell it [..N], the range literal for a count in [0, N] (SPEC §4.3)
```

An element range that excludes zero is refused for the same reason a scalar's
is, and the message says so:

```
a [..8]int32 | min = 1, max = 4
    Bad.schema:5:5: field a: its range [1, 4] excludes zero, so every element is born outside it — an array takes no specified default, so widen the range to reach zero (SPEC §4.6)
```

The count bound is not held to that rule. `window [2..8]uint32` compiles, and a
freshly constructed value has `window_count = 0`, which is outside the count's
own wire range. Reach for `[A..B]` with A above zero only where your code sets
the count before the value is ever written.

### The odds and ends

`bits(12)` is an unsigned field of exactly 12 bits, for when the width **is**
the specification: sequence numbers, opaque tags. Storage is `uint32_t`.

`bytes(1024)` is `string` with no text contract: a length-prefixed run of raw
bytes in fixed storage.

`int128` and `uint128` are full-width 128-bit integers, `serialize::uint128_t`
in C++, for keys and hashes that ride through untouched.

### The grown ship state

```
type ShipState
{
    ship_type ShipType
    name      string(MaxShipName)
    health    int32 = MaxHealth | min = 0, max = MaxHealth
    throttle  float32           | min = 0, max = 1, resolution = 0.01
    position  Vector3
    heading   fixed(16, 16)     | min = -180, max = 180
    systems   SystemFlags
    docked    bool
    cargo     [..8]uint32
}
```

`health` grew a default of `= MaxHealth`. Ships start at full health, so the
rest state is not zero, and now `ShipState ship;` starts at 1000 with no
constructor anywhere:

```cpp
struct ShipState {
    ShipType ship_type = ShipType::None;
    char name[MaxShipName + 1] = {}; // string(MaxShipName): max length, used length beside it (SPEC §4.7)
    int32_t name_length = 0;
    int32_t health = MaxHealth; // wire [0, 1000]
    float throttle = 0.0f; // compressed float [0, 1] @ 0.01
    Vector3 position;
    int32_t heading = 0; // fixed(16, 16) — Q16.16, raw value scaled by 2^16; bounds in whole units; wire [-180, 180]
    SystemFlags systems = 0;
    bool docked = false;
    uint32_t cargo[8] = {}; // used count beside it; wire count in [0, 8]
    int32_t cargo_count = 0;
};

inline constexpr int64_t ShipStateMaxBits = 705; // longest wire path; align pads at worst case (SPEC §6.1)
inline constexpr int64_t ShipStateMaxBytes = 96; // 8-byte write granularity; read slack per the contract above
```

Everything is inline, fixed size and trivially copyable. You can `memcpy` a
`ShipState`, hold arrays of them, or share them between processes. That property
is by construction, and later parts build on it.

`ShipStateMaxBytes` is the number to size a send buffer with, and it accounts
for the read slack the contract asks for.

### A program

```cpp
    ShipState ship;
    ship.ship_type = ShipType::Freighter;
    strcpy( ship.name, "Kestrel" );
    ship.name_length = 7;
    ship.throttle = 0.63f;
    ship.position = { 100.0, 25.5, -3.0 };
    ship.heading = (int32_t) ( 42.5 * 65536 );
    ship.systems = SystemFlags_Shields;
    ship.cargo_count = 3;
    ship.cargo[0] = 11; ship.cargo[1] = 22; ship.cargo[2] = 33;

    uint8_t buffer[ShipStateMaxBytes];
    serialize::WriteStream writer( buffer, sizeof( buffer ) );
    WriteShipState( writer, ship );
    writer.Flush();

    ShipState copy;
    serialize::ReadStream reader( buffer, writer.GetBytesProcessed() );
    ReadShipState( reader, copy );

    printf( "%s \"%s\" health=%d throttle=%.2f heading=%.2f cargo=%d bytes=%lld of %lld max\n",
        EnumName( copy.ship_type ), copy.name, copy.health, copy.throttle,
        copy.heading / 65536.0, copy.cargo_count,
        (long long) writer.GetBytesProcessed(), (long long) ShipStateMaxBytes );
```

```
$ ./starlight
Freighter "Kestrel" health=1000 throttle=0.63 heading=42.50 cargo=3 bytes=51 of 96 max
```

Fifty-one bytes, most of it the three `float64` of the position, which is what
a `float64` costs when you ask for a whole one.

**You now have** the full vocabulary of scalar and buffer fields, and a ship
state that costs a fraction of its struct size on the wire.
