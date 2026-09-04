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

1. [A file, a package, and your first constants](#part-1-a-file-a-package-and-your-first-constants)
2. [Naming things: enums and flags](#part-2-naming-things-enums-and-flags)
3. [The first packet: types and ranged integers](#part-3-the-first-packet-types-and-ranged-integers)
4. [The rest of the field types](#part-4-the-rest-of-the-field-types)
5. [Branches, unions, and your protocol](#part-5-branches-unions-and-your-protocol)
6. [Tables: data that outlives the build](#part-6-tables-data-that-outlives-the-build)
7. [Shaping real data: optionals, keyed arrays, and a config format](#part-7-shaping-real-data-optionals-keyed-arrays-and-a-config-format)
8. [Pointers and maps: when data is a graph](#part-8-pointers-and-maps-when-data-is-a-graph)
9. [The text form: designers edit JSON, the game loads bytes](#part-9-the-text-form-designers-edit-json-the-game-loads-bytes)
10. [Evolution you can trust: `was` and the baseline](#part-10-evolution-you-can-trust-was-and-the-baseline)
11. [The cook: point at a file instead of parsing it](#part-11-the-cook-point-at-a-file-instead-of-parsing-it)
12. [The block form: a frame another language reads in place](#part-12-the-block-form-a-frame-another-language-reads-in-place)
13. [One schema, every language, and tools that walk it](#part-13-one-schema-every-language-and-tools-that-walk-it)
14. [The tool belt, and where the edges are](#part-14-the-tool-belt-and-where-the-edges-are)

[USAGE.md](USAGE.md) describes every feature on its own terms. This page is
the path through the same material in the order a newcomer walks it.
[SPEC.md](SPEC.md) and [SPEC-TABLES.md](SPEC-TABLES.md) are the normative
references, and nothing here contradicts them.

---

## Part 1: A file, a package, and your first constants

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
language toolchains, no runtime checkouts, no generation. Put `bin` on your
path, because every command on this page is spelled `schema`, and ask the
binary what it is:

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

## Part 2: Naming things: enums and flags

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

## Part 3: The first packet: types and ranged integers

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

## Part 4: The rest of the field types

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

---

## Part 5: Branches, unions, and your protocol

### The problem

A docked ship has no velocity, no throttle and no radar contacts, and sending
those fields for every ship every tick is paying for fields that mean nothing.
Beyond that, Starlight has more than one **kind** of message. A fire command is
not a chat line is not a snapshot, and so far a type is one fixed layout.

### if / else: pay only for what is true

```
type Contact
{
    on_radar bool
    if on_radar
    {
        bearing  bits(9)
        distance ufixed(24, 8) | min = 0, max = 60000
    }
    else
    {
        last_seen uint32
    }
}
```

The condition must be a `bool` field declared **earlier** in the same or an
enclosing block, because the reader has to know it before it can decide what
comes next, and it is exactly a field name with no expressions. Break either
rule and the compiler says so:

```
Bad.schema:5:8: if condition moving must be a bool field declared earlier in the same or an enclosing block (the dominance rule, SPEC §4.5)
Bad.schema:6:8: if condition kind must be a bool field (SPEC §4.6)
```

Storage holds both sides and the wire carries only the taken one:

```cpp
struct Contact {
    bool on_radar = false;

    // if on_radar — wire branch; storage holds both sides, a read zeroes the
    // untaken side (SPEC §5)
    uint32_t bearing = 0;
    uint32_t distance = 0; // ufixed(24, 8) — UQ24.8, raw value scaled by 2^8; bounds in whole units; wire [0, 60000]

    // if on_radar else — wire branch; storage holds both sides, a read zeroes the
    // untaken side (SPEC §5)
    uint32_t last_seen = 0;
};
```

The read side shows the detail that saves you a debugging session:

```cpp
SCHEMA_READ_INLINE bool ReadContact( serialize::ReadStream & stream, Contact & value )
{
    read_bool( stream, value.on_radar );
    if ( value.on_radar )
    {
        read_bits( stream, value.bearing, 9 );
        read_fixed( stream, value.distance, 24, 8, 0, 60000 );
        value.last_seen = 0;
    }
    else
    {
        read_bits( stream, value.last_seen, 32 );
        value.bearing = 0;
        value.distance = 0;
    }
    return true;
}
```

**A read zeroes the untaken side.** Reuse one `Contact` for packet after packet
and you never inherit a stale `bearing` from the last packet through a branch
that was not taken this time.

### union: one of a set

`if`/`else` picks between two layouts under a bool. When the choice is one of N
named payloads, that is a union. Starlight's fire command is a laser or a
missile:

```
type LaserFire
{
    target_id uint16
    power     float32 | min = 0, max = 1, resolution = 0.01
}

type MissileFire
{
    target_id uint16
    count     int32 = 1 | min = 1, max = 4
}

union WeaponFire
{
    laser   LaserFire
    missile MissileFire
}

type FireCommand
{
    ship_id uint16
    fire    WeaponFire
}
```

That `count int32 = 1 | min = 1, max = 4` is Part 3's zero-excluding-range rule
paying off, because missiles fire at least one.

The compiler generates a tag enum and a real C++ union:

```cpp
enum class WeaponFireType : uint8_t {
    None = 0,
    Laser = 1,
    Missile = 2,
    Max = 2, // the exported extent (SPEC §4.2)
};

struct WeaponFire
{
    WeaponFireType type;

    union
    {
        LaserFire laser;
        MissileFire missile;
    };

    WeaponFire() : type( WeaponFireType::None ) {} // the tag only — arms are zero-established at selection
};
```

Like the enum, **every union has an implicit `None = 0`**, the empty union, in
band. A zero-initialized union field carries "nothing" without a has-flag, and
a stream of unions can end on `None`.

The tag enum is not a full enum. It carries `None` and `Max`, and it does not
carry `Count` or a name function, so `EnumName( cmd.fire.type )` does not
compile. Log the tag by switching on it.

To select an arm in C++, set the tag and value-establish the arm, both, in that
order of importance:

```cpp
    cmd.fire.type = WeaponFireType::Laser;
    cmd.fire.laser = LaserFire{};          // zero-establish the arm
    cmd.fire.laser.target_id = 12;         // then fill it
```

Forget the tag and the write emits `None`, tag only, with your payload silently
absent, because tag `None` is a legal value and nothing asserts. Forget the
establishment and the arm's bytes are whatever was there. Treat the pair as one
motion. Rust ties the two together by construction with a real
`enum WeaponFire { None, Laser(LaserFire), ... }`.

The wire is the tag in minimal bits for `[0, variant count]`, two bits here,
followed by **the selected payload only**. A read rejects a tag above the
count, and a write validates the tag before it rides. Compare the bool-guard
version you would otherwise write, a `has_laser bool` and an `if has_laser`
block per weapon: that spends a bit per absent arm and lets "two weapons at
once" exist as a representable state for every consumer to police. The union
deletes the illegal state and costs less.

An arm may also name no type at all, an arm that selects and carries nothing.
Seven targets generate it today and C and C++ refuse the unit:

```
$ schema generate --lang cpp --out gen .
schema: unit declares a union with a payload-free arm (WeaponFire) — the packet wire's payload-free arm is cs, dart, elixir, go, java, js and rust only today, and the cpp form is a named follow-on; generate with --lang cs, --lang dart, --lang elixir, --lang go, --lang java, --lang js and --lang rust, or give the arm a payload (SPEC §4.8, docs/SPEC-TABLES.md §11)
```

In a `type` body, every other arm names a declared type. Arms of any other
field type belong to a table closure, which Part 6 introduces.

### Your protocol is a union away

A union of message payloads plus your own framing is a protocol:

```
union Payload
{
    fire     FireCommand
    chat     ChatLine
    snapshot Snapshot
}

type Packet
{
    sequence uint16
    payloads [..MaxPayloadsPerPacket]Payload
}
```

One declaration buys the tag enum, the validated tag wire, and
`WritePacket` and `ReadPacket` end to end. If you prefer a terminated stream to
a counted array, write `Payload` values back to back and end on the in-band
`None`.

### Framing: `const`, `reserved`, `align`

A real packet usually opens with framing: a magic byte so junk is rejected in
the first 8 bits, a version nibble, and room to grow. Three storage-less
constructs write wire with no field behind them:

```
type PacketHeader
{
    const(0xC7, 8) // written always, and a read rejects any other value
    version bits(3)
    reserved(5)    // zeros, and a read rejects nonzero, claimable later
    align          // zero-pad to the byte boundary, and a read rejects nonzero pad
    sequence uint16
}
```

```cpp
struct PacketHeader {
    uint32_t version = 0;
    uint16_t sequence = 0;
};
```

Storage holds only the real fields. The write carries the rest:

```cpp
SCHEMA_WRITE_INLINE bool WritePacketHeader( serialize::WriteStream & stream, const PacketHeader & value )
{
    write_bits( stream, 199ull, 8 ); // const(199, 8) — SPEC §4.3
    write_bits( stream, value.version, 3 );
    write_bits( stream, 0ull, 5 ); // reserved(5) — zeros on the wire
    write_align( stream );
    write_bits( stream, value.sequence, 16 );
    return true;
}
```

`align` also earns its keep before bulk data, because an aligned `bytes(N)` body
can be copied instead of bit-shifted. Strings and bytes align internally
already: length, align, then the raw bytes.

### A program

```cpp
    FireCommand cmd;
    cmd.ship_id = 7;
    cmd.fire.type = WeaponFireType::Laser;
    cmd.fire.laser = LaserFire{};
    cmd.fire.laser.target_id = 12;
    cmd.fire.laser.power = 0.75f;

    uint8_t buffer[FireCommandMaxBytes];
    serialize::WriteStream w( buffer, sizeof( buffer ) );
    WriteFireCommand( w, cmd );
    w.Flush();

    FireCommand back;
    serialize::ReadStream r( buffer, w.GetBytesProcessed() );
    ReadFireCommand( r, back );
    printf( "fire command %lld bytes, tag %d", (long long) w.GetBytesProcessed(), (int) back.fire.type );
    if ( back.fire.type == WeaponFireType::Laser )
    {
        printf( ", laser at %d power %.2f", back.fire.laser.target_id, back.fire.laser.power );
    }
    printf( "\n" );

    PacketHeader header;
    header.version = 2;
    header.sequence = 1000;
    uint8_t frame[PacketHeaderMaxBytes];
    serialize::WriteStream hw( frame, sizeof( frame ) );
    WritePacketHeader( hw, header );
    hw.Flush();
    printf( "header %lld bytes, first byte 0x%02X\n", (long long) hw.GetBytesProcessed(), frame[0] );

    frame[0] = 0xC6;
    PacketHeader bad;
    serialize::ReadStream hr( frame, hw.GetBytesProcessed() );
    printf( "wrong magic: %s\n", ReadPacketHeader( hr, bad ) ? "ACCEPTED" : "refused" );
```

```
$ ./p5
fire command 6 bytes, tag 1, laser at 12 power 0.75
header 4 bytes, first byte 0xC7
wrong magic: refused
```

### The protocol id: same or refuse

Now the promise from Part 1 comes due. Look at the id, then grow the union by
one arm and look again:

```
$ schema id .
0x7bf979deb4f093ff
$ # add:  railgun LaserFire  to union WeaponFire
$ schema id .
0x70a64000c20675a3
```

The id is a hash of the unit's entire wire shape. `schema projection` prints
the exact text that gets hashed:

```
$ schema projection .
schema-wire-projection 2
schema-wire-law 1
package net5
type Contact table=false message=false
  field on_radar kind=2
  branch cond=on_radar neg=false
   then
    field bearing kind=1 width=9 signed=false
    field distance kind=7 width=32 signed=false I=24 F=8 intrange=[0,60000]
   else
    field last_seen kind=0 width=32 signed=false
type FireCommand table=false message=false
  field ship_id kind=0 width=16 signed=false
  field fire kind=8 type=WeaponFire
```

Field kinds, widths, ranges, variant counts, all of it. Nothing on the wire
identifies fields, and both sides simply know the layout because they were
generated from the same schema. That is what makes the wire this small, and it
means both sides must **be** the same. The contract:

- Exchange ids in your connection handshake. `net5::ProtocolId` is in the
  generated header.
- Same id: every packet decodes, bit for bit.
- Different id: refuse the connection. Do not talk.

There is no version tag on any packet, so you pay for version agreement once at
connect instead of on every message. This wire is deliberately not an evolution
system. When you need data that survives schema drift, that is what tables are
for, and they are the next part.

**You now have** a complete network protocol: conditional layouts, typed
messages, framing, and a handshake rule that makes wire mismatch impossible by
construction.

---

## Part 6: Tables: data that outlives the build

### The problem

Everything so far is exact match: same protocol id or refuse. That is perfect
for packets, because both ends redeploy together. Now Starlight needs a save
game. A player's file gets written today and read by the build you ship next
year, and "same or refuse" would mean every update deletes every save. Config
files have the same shape, because designers tune `ShipConfig` weekly and last
week's files must keep loading.

The packet wire cannot do this, on purpose. For data that outlives the build
that wrote it, the language has a second declaration: `table`.

### Declaring one

```
table ShipConfig
{
    display_name string(32)
    max_health   float32 = 100.0
    max_speed    float32 = 500.0
    armor        int32 = 1 | min = 0, max = 10
    ship_type    ShipType
}
```

The body grammar is the type body's: same fields, same defaults, same ranges.
What changes is the wire and the lifecycle. Generation produces more files
beside the packet headers:

```
$ schema generate --lang cpp --out gen --verbose .
wrote gen/Config.h
wrote gen/ConfigBlock.cpp
wrote gen/ConfigBlock.h
wrote gen/ConfigTable.cpp
wrote gen/ConfigTable.h
wrote gen/ConfigWire.h
```

`ConfigTable.h` and `ConfigTable.cpp` are the table surface. The Block pair is
Part 12, so ignore it until then. The table header is includable from any
translation unit and depends on nothing but the C standard headers:

```cpp
// The TABLE wire (evolution-tolerant, docs/SPEC-TABLES.md): no serialize
// dependency — includable from any TU.
```

The storage struct lives there, and it looks exactly like a type's:

```cpp
struct ShipConfig {
    char display_name[32 + 1] = {}; // string(32): max length, used length beside it
    int32_t display_name_length = 0;
    float max_health = 100.0f;
    float max_speed = 500.0f;
    int32_t armor = 1;
    ShipType ship_type = ShipType::None;
};
```

### Save and load

The encode surface is a measure and save split, and you own every buffer,
because generated table code allocates nothing:

```cpp
    ShipConfig ship;
    memcpy( ship.display_name, "Kestrel", 7 );
    ship.display_name_length = 7;
    ship.max_health = 250.0f;
    ship.max_speed = 900.0f;
    ship.armor = 8;
    ship.ship_type = ShipType::Corvette;

    int64_t size = ShipConfigMeasure( ship );          // exact, writes nothing
    std::vector<uint8_t> buffer( size );
    int64_t wrote = ShipConfigSave( ship, buffer.data(), size );
    printf( "measured %lld, saved %lld bytes\n", (long long) size, (long long) wrote );

    ShipConfig blank;
    printf( "an all-default ShipConfig measures %lld bytes\n", (long long) ShipConfigMeasure( blank ) );
```

```
$ c++ -std=c++17 -Wall -Wextra -Werror -o save save.cpp gen/ConfigTable.cpp
$ ./save
measured 42, saved 42 bytes
an all-default ShipConfig measures 2 bytes
```

Measure is exact, so a buffer of exactly its answer always suffices. `Save`
returns the size, or -1 for a buffer too small or a value that violates a
storage bound.

One fact explains both numbers: **values at their defaults stay off the wire.**
`max_health = 250` rides, and a ship left at 100 would not. An all-default
table saves as 2 bytes and loads back complete.

### The point: any build reads any data

Time passes, and Starlight 2.0's schema has evolved:

```
enum ShipType { Fighter, Interceptor, Freighter, Corvette }   // new variant, in the MIDDLE

table ShipConfig
{
    display_name string(32)
    max_health   float32 = 100.0
    armor        int32 = 1 | min = 0, max = 5     // bound shrunk from 10
    ship_type    ShipType
    shield       float32 = 50.0                   // new field
}
```

`max_speed` is gone, `shield` is new, the armor cap dropped from 10 to 5, and
`Interceptor` landed in the middle of the enum. On the packet wire any one of
those edits is a new protocol id. Here, load the 1.0 file with the 2.0 build:

```cpp
    TableReport report;
    ShipConfig loaded;
    bool ok = ShipConfigLoad( loaded, buffer.data(), bytes, &report );
```

```
$ ./load ../v1/ship.bin
load: ok
  name=Kestrel health=250 armor=5 type=Corvette shield=50
  report: unknown=1 kind_mismatch=0 clamped=1 malformed=0
```

Read that slowly, because each value is a rule.

`name=Kestrel` and `health=250`: fields that still exist load as written.

`shield=50`: a field the file never heard of takes its declared default.

`armor=5`: the stored 8 is **clamped** to the new `[0, 5]`, and counted.

`type=Corvette`: an enum value rides as the hash of its variant's **name**, so
inserting `Interceptor` in the middle moved nothing. On a packet wire, stored
value 3 would now silently mean a different ship.

`unknown=1`: the file's `max_speed` has no home here, so it was skipped by its
length and counted, never misdecoded.

It works in the other direction too, with the **old** build reading a **new**
file:

```
$ ./load ../v2/dart.bin
load: ok  name=Dart type=None speed=500
  report: unknown=2 kind_mismatch=0 clamped=0
```

The 1.0 build skipped `shield` as unknown, and the `Interceptor` variant it has
no name for loaded as `None`, also counted, never as a neighbor. Both
directions, any distance apart.

Field identity on the table wire is a hash of the field name, every field is
length prefixed, and enum values and union arms ride under their name hashes
too. Unknown is skippable, absent is defaultable, and out of range is
clampable. Contrast that with the packet wire, which **refuses** out-of-range
values because the sender is an untrusted peer. Tables **clamp and count**,
because the data is yours and half a config is better than none.

### The report is the witness

`TableReport` is five counters:

```cpp
struct TableReport
{
    int32_t unknown = 0;       // unknown field ids skipped (newer data)
    int32_t kind_mismatch = 0; // known id, changed type — skipped, never misdecoded
    int32_t clamped = 0;       // out-of-range values clamped to declared bounds
    int32_t duplicate = 0;     // the text form saw a key twice: last wins, and the repeat is counted
    bool malformed = false;    // framing damage; decode stopped, partial result kept
};
```

You can see `kind_mismatch` by changing `armor` to a `float32` in a third
revision and loading the 1.0 file again:

```
$ ./load ../v1/ship.bin
load: ok  armor=1 (float now)
  report: unknown=1 kind_mismatch=1 clamped=0
```

The stored int under the `armor` id is not an int as far as this reader is
concerned, so it is skipped and the field keeps its declared default. Never
misdecoded.

`malformed` is the only counter that stops a load. Truncate the file at byte 17:

```
$ ./load ../v1/ship.bin 17
load: malformed
  name=Kestrel health=100 armor=1 type=None shield=50
  report: unknown=0 kind_mismatch=0 clamped=0 malformed=1
```

`Load` returned false and the good prefix was kept, so `Kestrel` survived and
the fields after the cut sit at their defaults. Only structural damage does
this, and every schema-drift event above is a counter rather than a failure.

Adopt this discipline on day one: **log the counters**. A nonzero report means
the data came from a different schema generation. It is still fully usable, but
that is drift you want visible on a dashboard rather than invisible in a save
file.

### Two rules the wire cannot enforce for you

First, **a default is part of the wire contract.** An absent field means "the
reader's declared default", and elision means fields at their default are
absent. So changing `max_health = 100.0` to `= 120.0` changes what every
already-written file says, silently, with no report event, because files that
elided a genuine 100 now load 120. Change a default the way you would change
data, or add a new field instead. Part 10 shows the tool that catches this
edit.

Second, **`flags` appends at the end, forever.** A mask rides as raw bits and
there are no per-bit names on the wire, so a variant's identity is its bit
position:

```
flags Perks { Shielded, Cloaked, Turbo }           // v1: bits 0, 1, 2
flags Perks { Shielded, Hardened, Cloaked, Turbo } // wrong: stored Cloaked reads Hardened
flags Perks { Shielded, Cloaked, Turbo, Hardened } // right: appended, nothing moves
```

Enums insert anywhere because they are name hashed. Flags append at the end
because they are positional. Two declarations, two laws. Removing a flags
variant frees no bit either, so retire the name and keep the position. Part 10's
baseline turns this law into a compile error.

### Where tables stand next to types

Edit a table, add fields, grow enums, reshape at will, and your protocol id
**does not move**. Tables never touch it, and packets never pay for tolerance.
The costs live where the tolerance lives: a table's wire is byte granular
rather than bit packed, 42 bytes here against Part 3's three, and the read
walks and matches ids instead of streaming bits. A fixed-size table like this
one still allocates nothing and stays a plain struct. What "fixed size" means,
and what the other class costs, is Part 8's story.

Every target generates the table surface:

```
$ for l in c cpp cs dart elixir go java js rust; do printf "%-8s " $l; schema generate --lang $l --out /tmp/gg-$l . && echo ok; done
c        ok
cpp      ok
cs       ok
dart     ok
elixir   ok
go       ok
java     ok
js       ok
rust     ok
```

Every scalar the packet wire carries rides in a table too, `fixed`, `ufixed`
and the 128-bit integers included. Two structural rules stand: a `type` cannot
reference a table, because packets stay exact match while a table may reference
types freely, and a table cannot nest itself by value.

**You now have** a save format. Write it with any build of Starlight, read it
with any other, and every difference between them is absorbed, counted and
reported.

---

## Part 7: Shaping real data: optionals, keyed arrays, and a config format

### The problem

One `ShipConfig` is not a game. Starlight's designers want one file that holds
everything: global switches, a weapon list, and a per-ship-type tuning block.
Parts of it are genuinely optional, because gunner settings exist only for
ships that have a gunner seat.

### A root table is a format

Tables nest by value, and bounded arrays of tables give you collections:

```
const MaxWeapons = 8

table WeaponConfig
{
    damage float32 = 21.0
    homing bool
}

table GameConfig
{
    friendly_fire bool
    weapons       [..MaxWeapons]WeaponConfig
    ships         [ShipType]ShipConfig
    level_name    string(64)
}
```

Declare the root and the file format falls out. The bytes `GameConfigSave`
writes to disk **are** the format, and the same bytes work as a message on a
socket. Nothing adds an envelope, so if you want a magic number or a content
hash around it, that is a few lines of yours on top.

Two field spellings up there are new. Take them one at a time.

### `?T`: present or absent

```
table ShipConfig
{
    display_name string(32)
    max_health   float32 = 100.0
    settings     ?GunnerSettings
    tier         ?int32              // scalars can be optional too
}
```

A `?` before the type makes the field **present or absent**. Storage is the
value plus a generated `_present` bool, so the table stays a fixed-size struct
with no pointer and no allocation:

```cpp
struct ShipConfig {
    char display_name[32 + 1] = {}; // string(32): max length, used length beside it
    int32_t display_name_length = 0;
    float max_health = 100.0f;
    GunnerSettings settings;
    bool settings_present = false; // ?GunnerSettings: absent until set
    int32_t tier = 0;
    bool tier_present = false; // ?int32: absent until set
};
```

**Presence decides whether it rides, not content.** Watch the measure:

```cpp
    ShipConfig & fighter = config.ships[ShipType::Fighter];
    printf( "fighter measures %lld with settings absent\n", (long long) ShipConfigMeasure( fighter ) );
    fighter.settings_present = true;
    printf( "fighter measures %lld with settings present and all-default\n", (long long) ShipConfigMeasure( fighter ) );
```

```
fighter measures 2 with settings absent
fighter measures 11 with settings present and all-default
```

That is the difference between `?T` and a plain nested `T`. A plain nesting at
all defaults elides entirely, so you cannot tell "not there" from "there, all
defaults". An optional keeps "absent" and "present with nothing to say" as two
distinct values. A reader that meets the field sets `_present` whatever the
content, and an absent one leaves it false.

`?` applies to a nested table, a nested type, an enum, a flags mask, any
scalar, and a bounded array of those. Where it is refused, the refusal explains
the logic:

```
u ?U        (a union)
    Bad.schema:7:14: field u: ?U marks a union optional, and a union is ALREADY optional — its None arm IS the absence, and an empty union elides exactly as an absent optional does; drop the ? (docs/SPEC-TABLES.md §2.3)

a ?*N       (a pointer)
    Bad.schema:4:14: field a: ?*N marks a pointer optional, and a pointer is ALREADY optional — null is its absence, and it rides exactly as an absent optional does; drop the ? (docs/SPEC-TABLES.md §2.3)

a ?string(8)
    Bad.schema:3:14: field a: ? on string(N) is a named follow-on — the generated length companion already carries emptiness, and a second presence bit beside it would be two answers to one question; wrap it in a table and make that optional (docs/SPEC-TABLES.md §15)

a ?int32 = 3
    Bad.schema:3:11: field a: an optional field takes no specified default — PRESENCE is the only default an optional has, and an absent optional reads as absent with its value at the type's own zero (docs/SPEC-TABLES.md §2.3)
```

### `[ShipType]ShipConfig`: one slot per named variant

The tuning block wants exactly one `ShipConfig` per ship type. You could write
`[ShipType.Max]ShipConfig` and index by `int( type ) - 1`, and the day someone
inserts a variant mid-enum every stored file shifts its slots by one, silently.
The keyed spelling exists so that cannot happen:

```
    ships [ShipType]ShipConfig
```

```cpp
    TableKeyed<ShipConfig, ShipType> ships; // [ShipType]: one slot per named variant, keyed by the value
```

There is no count companion, because every named slot exists, and no slot for
`None`, because `None` is the null and key `k` lives at index `k - 1`. The
surface is an accessor and iteration:

```cpp
    config.ships[ShipType::Corvette].max_health = 400.0f; // index by the key itself

    for ( auto [ ship_type, ship ] : config.ships )       // every slot, and the KEY runs 1 to Max
    {
        printf( "slot %s health=%g\n", EnumName( ship_type ), ship.max_health );
    }
```

```
slot Fighter health=100
slot Freighter health=100
slot Corvette health=400
```

Iteration yields the **key**, never a storage index, so consuming the whole
array involves no `- 1`, no cast, and no bound of your own. Write
`auto [ k, v ]` and not `auto & [ k, v ]`, because the entry is a proxy handed
out by value and the compiler refuses the reference spelling by design. The
element inside it is a real reference either way.

**Indexing by `None` ends the program, in every build.** Keys in data-driven
code are runtime values, an enum read out of a file or a key a tool hands you:

```cpp
    ShipType key = ShipType::None;
    printf( "about to index by None...\n" );
    fflush( stdout );
    config.ships[key].max_health = 1.0f;
    printf( "still here\n" );
```

```
$ c++ -std=c++17 -O2 -DNDEBUG -o none none.cpp gen/ConfigTable.cpp
$ ./none
about to index by None...
$ echo $?
134
```

This is not a debug assert, and `NDEBUG` does not remove it, because there is
no configuration in which a `None` key silently reading one element **before**
the array is acceptable. The cost is one predictable compare, and iteration,
which hands over no key, never pays even that.

**On the wire, slots ride by variant name**, like every enum value in Part 6.
Save a config under `{ Fighter, Freighter, Corvette }`, insert `Interceptor`
second, rebuild, and load the old file:

```
$ ./load ../p7/config.bin
slot Fighter      health=100
slot Interceptor  health=100
slot Freighter    health=100
slot Corvette     health=400
report: unknown=0
```

The new slot arrives at its declared default and the tuned slot is still
Corvette's. A slot the writer left at its default is elided, a slot this reader
has no name for is skipped and counted `unknown`, and a `None` key never rides
at all.

One caution for later. In a `type` body the same spelling `[Team]int32` is
legal but positional: a plain array, on the packet wire, with no accessor and
no guard. On the table wire, changing a field between the keyed and positional
spellings is a wire break the report calls `kind_mismatch`, because the two are
different encodings and not a refactor. Part 10's baseline refuses that edit
outright.

### Messages: a union in a table

Starlight's editor tools talk to the game over a socket, and tool messages must
evolve without lockstep deploys, because new tools talk to old games all week.
A union inside a table closure is that message system, and here the arms are
not restricted to declared types. An arm may be a table, a scalar, or nothing
at all:

```
union ToolBody
{
    open  OpenDocument   // a table
    save  SaveDocument
    ping  uint32         // a scalar
    close                // no payload
}

table ToolMessage
{
    sequence uint32
    body     ToolBody
}
```

```cpp
enum class ToolBodyType : uint8_t {
    None = 0,
    Open = 1,
    Save = 2,
    Ping = 3,
    Close = 4,
    Max = 4, // the exported extent (SPEC §4.2)
};

struct ToolBody
{
    ToolBodyType type;

    union
    {
        OpenDocument open;
        SaveDocument save;
        uint32_t ping;
    };

    ToolBody() : type( ToolBodyType::None ) {} // the tag only — arms are established at selection
};
```

The payload-free `close` arm is a value of the tag enum and no member at all.

```cpp
    ToolMessage msg;
    msg.sequence = 3;
    msg.body.type = ToolBodyType::Open;
    msg.body.open = OpenDocument{};
    memcpy( msg.body.open.path, "ship.cfg", 8 );
    msg.body.open.path_length = 8;
    msg.body.open.line = 42;
```

```
$ ./p7m
seq 3, tag 1, path ship.cfg line 42, 42 bytes
a ping message measures 22 bytes
a payload-free close measures 18 bytes
```

On the table wire a union's arm rides under its **arm-name hash** with a length
prefix, so adding a message is adding an arm. A reader that lacks the arm reads
the union empty as `None`, skips the body by its length and counts `unknown`,
and arms may be removed and reordered freely. Everything Part 6 taught about
fields applies to messages.

Table arms and scalar arms are a table-closure privilege. A union used in a
`type` still takes declared type payloads only, because packets stay value
semantics.

Unions array like anything else, so a heterogeneous event log is one bounded
array of one union:

```
table MatchLog
{
    events [..64]Event      // union Event { damage DamageEvent, dock DockEvent }
}
```

### A program

```cpp
    GameConfig config;
    config.friendly_fire = true;
    memcpy( config.level_name, "orbital", 7 );
    config.level_name_length = 7;
    config.weapons_count = 2;
    config.weapons[0].damage = 35.0f;
    config.weapons[1].homing = true;
    config.ships[ShipType::Corvette].max_health = 400.0f;

    int64_t size = GameConfigMeasure( config );
    std::vector<uint8_t> buffer( size );
    GameConfigSave( config, buffer.data(), size );
    printf( "GameConfig is %lld bytes\n", (long long) size );
```

```
$ ./p7
fighter measures 2 with settings absent
fighter measures 11 with settings present and all-default
slot Fighter health=100
slot Freighter health=100
slot Corvette health=400
GameConfig is 99 bytes
```

**You now have** `GameConfig`: one declared root, one binary format, with
optional blocks, per-ship-type tuning that survives enum surgery, and a message
channel that tolerates version skew.

---

## Part 8: Pointers and maps: when data is a graph

### The problem

Starlight's patrol routes are linked chains of waypoints, and a waypoint knows
its next. A save-file scene wants one palette shared by fifty props, not fifty
copies. Value semantics cannot say either of those things, because types and
the tables so far nest by value, and a value cannot contain itself or be in two
places at once.

### `*Table`: pointer fields

```
table Waypoint
{
    name string(24)
    x    float32
    y    float32
    next *Waypoint       // recursion through a pointer is legal
}

table Route
{
    head  *Waypoint
    stops [..4]*Waypoint
    loops bool
}
```

Pointers are a **table** construct, with rules the compiler walks you through
if you cross them:

```
type T { p *V }        (a pointer in a type body)
    Bad.schema:4:12: field p: *V is a pointer, and pointers are a TABLE construct — types remain value semantics, tables allow pointer semantics; nest the field by value, or move the declaring type to a `table` (docs/SPEC-TABLES.md)

table T { p *V }       (V is a type)
    Bad.schema:4:13: field p: *V points at a type, and a pointer may only target a `table` — V is value-semantics data with no independent identity to point at; nest it by value, or declare V as a table (docs/SPEC-TABLES.md)

table T { p *V = 0 }
    Bad.schema:4:11: field p: a pointer field takes no specified default — a fresh pointer is null, and null is the only value a default could name (docs/SPEC-TABLES.md)
```

One rule you do not declare: **the compiler derives the mode.** A table with no
pointer anywhere in its by-value closure is fixed size, which is everything
Parts 6 and 7 did, a plain struct with zero overhead from this part. One
pointer anywhere in that closure makes the table variable length, and it is
never held by value again. You build it, lock it, and read it through a root
pointer.

Resist reaching for `*T` to mean "optional". That is `?T`, with no allocation
and still fixed size. A pointer is for structure: recursion, sharing, a subtree
you would rather not carry by value.

### The builder life

```cpp
    RouteBuilder builder;                        // the MUTABLE life
    Route * route = builder.GetRoot();
    route->loops = true;

    auto a = builder.Alloc<Waypoint>();          // TableSlot<Waypoint>: node AND reference
    auto b = builder.Alloc<Waypoint>();
    auto c = builder.Alloc<Waypoint>();
    SetName( a, "Gate" );    a->x = 0;    a->y = 0;
    SetName( b, "Belt" );    b->x = 120;  b->y = 40;
    SetName( c, "Station" ); c->x = 300;  c->y = -75;

    route->head = a;                             // assigning a slot links it
    a->next = b;
    b->next = c;

    builder.Lock();                              // ONE WAY, and it compacts
    const Route * locked = builder.AsConst();    // the CONST life: one packed region
```

`Alloc` hands back a slot usable both as the node pointer, so you write its
fields through it, and as the reference, so you assign it to a pointer field.
`Lock` walks the graph from the root, measures exactly, and lays every
reachable node back to back into one region with the root at its base, with
references rewritten self-relative so the whole region relocates by plain
`memcpy`. There is no unlock. To edit again, load the region into a fresh
builder.

Reading the locked form is one add per hop, and `NULL` for null:

```cpp
    for ( const Waypoint * w = WaypointAt( locked->head ); w != NULL; w = WaypointAt( w->next ) )
    {
        printf( "  %.*s (%g, %g)\n", w->name_length, w->name, w->x, w->y );
    }
```

```
  Gate (0, 0)
  Belt (120, 40)
  Station (300, -75)
```

Before `Lock`, a reference resolves through the arena instead, with the same
`WaypointAt` taking the builder's arena as its first argument. One slot, two
encodings, and the overload set keeps you honest.

### On the wire: sharing survives

The variable class saves and loads like any table, with the same wire, the same
tolerance and the same report, through the root pointer:

```cpp
    int64_t size = RouteMeasure( locked );
    std::vector<uint8_t> wire( size );
    RouteSave( locked, wire.data(), size );

    int64_t need = RouteLoadMeasure( wire.data(), size );  // exact, reads no values
    std::vector<uint8_t> region( need );                   // YOUR allocation, as always
    TableReport report;
    const Route * loaded = RouteLoad( region.data(), need, wire.data(), size, &report );
```

On the wire a pointer rides as an index into a flat node table, which buys the
property graphs actually need: identity. Point at one node twice and it is
still one node after a round trip:

```cpp
    route->stops_count = 3;
    route->stops[0] = a;
    route->stops[1] = a;   // the same node, a second reference
                           // stops[2] left null, and a null slot rides
```

```
$ ./p8
  Gate (0, 0)
  Belt (120, 40)
  Station (300, -75)
wire bytes: 172, region bytes: 256
after round trip: shared=YES null=yes, head is Gate
report: unknown=0 malformed=0
sizeof(RouteBuilder) = 8264
```

Nodes the root cannot reach do not ride, and null pointers are null on the far
side. Nothing you build can express a cycle the loader must detect, because you
cannot assign a slot you have not allocated, but the **reader** still assumes
hostile bytes, so a forged wire with a reference loop is refused rather than
followed.

### Maps

A pointer is one node reached by one edge. A map is many values reached by a
key, and it is the same machinery: a map makes its holder variable length, so
it lives in a builder too.

```
table Fleet
{
    ships map[string(32)]ShipConfig
    tiers map[int16]int32
}
```

```cpp
struct Fleet {
    TableMap<FleetShipsEntry> ships; // map[string(32)]ShipConfig — the sorted entry array, empty until an insert
    TableMap<FleetTiersEntry> tiers; // map[int16]int32 — the sorted entry array, empty until an insert
};
```

The key is a bounded string or an integer, and the value is anything a field
can be, including another map. Build through a worker and read through the
locked map:

```cpp
    FleetBuilder builder;
    Fleet * fleet = builder.GetRoot();
    TableWorker worker = builder.Worker();

    FleetShipsInsert( worker, fleet->ships, "kestrel" )->max_health = 250.0f;
    FleetShipsInsert( worker, fleet->ships, "dart" )->max_health = 80.0f;
    FleetShipsInsert( worker, fleet->ships, "anvil" )->max_health = 900.0f;
    *FleetTiersInsert( worker, fleet->tiers, -3 ) = 7;
    *FleetTiersInsert( worker, fleet->tiers, 2 ) = 9;

    builder.Lock();
    const Fleet * locked = builder.AsConst();

    for ( auto [ name, ship ] : locked->ships )
    {
        printf( "  %-8s health=%g\n", name, ship->max_health );
    }

    const ShipConfig * found = locked->ships.Find( "dart" );
```

```
$ ./p8map
  anvil    health=900
  dart     health=80
  kestrel  health=250
  tier -3 -> 7
  tier 2 -> 9
Find("dart") health=80, Find("ghost") is null
3 ships, 2 tiers, 165 bytes on the wire
```

Iteration is in **ascending key order**, not insertion order, because `Lock`
sorts the entries once and every later lookup is a binary search over the
sorted array. That is why `anvil` comes first and why `-3` sorts before `2`.
`Find` returns the value or `NULL`, in place, with no allocation. Inserting a
key that already exists replaces the value, and `Erase` marks an entry dead
without moving anything.

The builder builds no index, deliberately: the sort happens once, at `Lock`,
`Save` or the cook, so a build that inserts a million keys pays no hashing at
insert time. For a map large enough that binary search over a cold array hurts,
each map also generates an optional caller-owned index you build after load and
never store.

### Building goes wide

Lock-free parallel construction is designed in rather than bolted on. One
worker per thread, each allocating on its own front, with no lock and no
per-node atomic:

```cpp
    RouteBuilder builder;
    std::thread workers[4];
    for ( int t = 0; t < 4; t++ )
    {
        workers[t] = std::thread( [&builder, &heads, t] {
            TableWorker worker = builder.Worker();   // one per THREAD
            TableSlot<Waypoint> head = worker.Alloc<Waypoint>();
            /* ... build this thread's chain ... */
            heads[t] = head;
        } );
    }
    for ( auto & w : workers ) { w.join(); }         // join, THEN lock
    builder.Lock();
```

```
$ ./wide
walked 4000 waypoints; the wire measures 111989 bytes
```

Allocating on your own worker is safe concurrently. Writing fields of a node
another worker allocated is your synchronization problem, and `Lock` and `Save`
are single-threaded. A builder is about 8 KB, because it carries the arena's
segment table inline: fine on the stack, fine as a member, and not something
for an array of thousands.

### Who allocates

The builder's arena grows through an allocator. The default is
`schema_allocate` and `schema_release`, overridable macros that fall back to
`calloc` and `free`, and a `TableAllocator`, an alloc and free pair plus a
context, can be handed to any builder to route its memory through your own
system. Everything outside the builder path stays allocation free, as before:
the fixed class allocates nothing, `Load` regions are yours, and the generated
code never allocates behind your back.

The same style of macro governs `schema_assert`, the debug refusal, and
`schema_fatal`, what stands after it in release. Define any of the four before
including the header and the generated code uses yours. That is the whole C++
customization surface.

**You now have** patrol routes as real linked structure and fleets keyed by
name: built in parallel, locked into one relocatable region, saved on the same
tolerant wire as everything else, with sharing preserved.

---

## Part 9: The text form: designers edit JSON, the game loads bytes

### The problem

`GameConfig` is a binary format now, and binary is exactly what a designer
cannot open. Starlight's tuning workflow wants text that is diffable,
mergeable, and editable in anything, without maintaining a hand-written JSON
mapping beside the real schema.

### Every table reads and writes JSON

The reflection descriptors every table carries, which Part 13 walks directly,
drive a generic JSON codec, so there is no per-table code to write:

```cpp
    int64_t size = ShipConfigToJsonMeasure( ship );   // exact, writes nothing
    ShipConfigToJson( ship, text.data(), size );
```

```json
{
  "display_name": "Kestrel",
  "max_health": 250,
  "max_speed": 500,
  "armor": 8,
  "ship_type": "Corvette"
}
```

`ToJson` writes **every field, defaults included**, because a text is for
people and a person reading a config wants the whole state. `max_speed: 500`
above is a default the binary wire would have elided. Enums render by variant
name. The output is pretty-printed with a two-space indent and one trailing
newline.

In C++ the walk lives in the generated `ConfigTable.cpp`, so add that to your
build once. That is the whole cost, and a project that never touches text does
not compile it.

Reading text uses the same report as the wire, with one extra counter in play.
Feed it a designer's hand-edited file with a typo and an accidental repeat:

```json
{
  "display_name": "Kestrel II",
  "max_helth": 300,
  "armor": 4,
  "armor": 6,
  "ship_type": "Corvette"
}
```

```
from json: ok  name=Kestrel II health=100 armor=6
report: unknown=1 duplicate=1 clamped=0
```

`max_helth` is `unknown`, and `max_health` stayed at its default, so the typo
did not vanish silently. The doubled `armor` reads last wins and counts
`duplicate`. Mentioned-fields-only semantics match the wire: what the text
omits takes its declared default.

The writer refuses to lie. Hand `ToJson` an enum value no variant names and it
returns -1 rather than inventing JSON:

```cpp
    ShipConfig bad;
    bad.ship_type = (ShipType) 99;
    printf( "ToJsonMeasure on an unnamed enum value: %lld\n", (long long) ShipConfigToJsonMeasure( bad ) );
```

```
ToJsonMeasure on an unnamed enum value: -1
```

If your text has to meet an existing convention, `| json = "type"` renames the
key in text only, and no wire byte moves:

```
table Prop
{
    kind  string(16) | json = "type"
    solid bool
}
```

```json
{
  "type": "rock",
  "solid": false
}
```

### Graphs in text: `&node`

The variable class has a text form too, and it handles what plain JSON cannot:
sharing. Here is the `Route` from Part 8 with one waypoint referenced twice:

```json
{
  "head": null,
  "stops": [
    {
      "&node": 1,
      "name": "Gate",
      "x": 5,
      "y": 0,
      "next": null
    },
    {
      "&node": 1
    },
    null
  ],
  "loops": false
}
```

The first reference is the node's body, labeled `"&node": 1`. The second
reference is the label alone. A null pointer is `null`. `RouteFromJson` reads
into a **builder**, because text becomes a graph and graphs are built, and the
shared node comes back shared:

```
from json: ok  shared=YES null=yes
```

### `schema pack` and `schema unpack`: the tree workflow

One JSON file per root works. A **directory** per root works better with
version control, because designers touch one small file per edit and diffs stay
readable. The tool speaks that shape directly. Start from the binary you
already have and let `unpack` show you the layout:

```
$ schema unpack --root GameConfig --in game.bin --verbose tree .
report: silent — the data matched the schema exactly
unpacked game.bin into tree
$ find tree -type f | sort
tree/friendly_fire.json
tree/level_name.json
tree/ships/Corvette.json
tree/ships/Fighter.json
tree/ships/Freighter.json
tree/weapons.json
```

A directory named for a field holds that field's value, an enum-keyed array is
one `<Variant>.json` per slot, and there is no `None.json` because a `None` key
names no slot. Anything can collapse to a single `<field>.json`:

```
$ cat tree/weapons.json
[
  {
    "damage": 35,
    "homing": false
  },
  {
    "damage": 21,
    "homing": true
  }
]
```

Designers edit:

```
$ cat tree/ships/Corvette.json
{
  "display_name": "Corvette",
  "max_health": 500,
  "settings": { "sensitivity": 0.8 }
}
```

and the build packs the tree back into the root's wire bytes:

```
$ schema pack --root GameConfig --out packed.bin tree .
```

The game loads `packed.bin` with the same `GameConfigLoad` as ever:

```
$ ./load packed.bin
ok: level=Kuiper Run weapons=2 w0.damage=35 corvette=500 sens=0.8
```

The output is the table's wire bytes and nothing else. No magic, no hash,
because envelopes stay yours. `--one-file` gives you the single-JSON shape
instead:

```
$ schema unpack --root GameConfig --in packed.bin --one-file --verbose one .
report: silent — the data matched the schema exactly
unpacked packed.bin into one
$ ls one
GameConfig.json
```

`unpack` prunes files it owns and did not write, so a deleted field's stale
`.json` cannot haunt the tree.

Both verbs are strict by default, and any non-silent report is a nonzero exit.
Tune a Freighter to `"max_health": "very strong"`:

```
$ schema pack --root GameConfig --out packed2.bin tree .
report: unknown 0, kind_mismatch 1, clamped 0, duplicate 0, malformed false
schema: the report is not silent — pass --tolerate to accept it
$ echo $?
1
```

A string where a float belongs is a kind mismatch, and your CI stops until
someone fixes the text or explicitly tolerates it. JSON has one number type, so
`2` and `2.0` both read into an integer field, a genuinely fractional value
into an integer field is a mismatch, and `1.2.3` is malformed. Numbers never
get quietly mangled.

**You now have** the designer loop: binary for the game, text for people, one
schema driving both, and drift counted at every crossing.

---

## Part 10: Evolution you can trust: `was` and the baseline

### The problem

Part 6 ended with two hazards the wire cannot see. A changed default rewires
the meaning of every stored file, and a reordered flags declaration remaps every
stored mask, both silently, with clean reports. There is a third: renaming a
field. Identity is the name hash, so a rename orphans every byte ever stored
under the old name. Starlight has two years of player saves, and "be careful"
is not a plan.

### Renames: `was`

The rename hazard has an in-language fix. Say v1 shipped
`velocity float32 = 500.0` and you want it called `speed`:

```
table ShipConfig
{
    speed float32 = 500.0 | was = "velocity"
}
```

`was` carries the old identity through the rename. On the wire the field keeps
riding under `velocity`'s hash, so old files load into `speed` and new files
stay readable by old builds:

```
$ ./l ../w1/v1.bin
speed=777 unknown=0
```

No unknown, no loss, and the rename is invisible on the wire. The guard rails:

```
speed float32 | was = "speed"
    Config.schema:8:31: field damage: was = "damage" names the field's own current name — was records the OLD name after a rename; drop the attribute until one happens (docs/SPEC-TABLES.md)
```

```
type T { a int32 | was = "b" }
    T.schema:5:5: field a: was is a table-wire concept — it aliases a renamed field's wire id, and only table fields have wire ids; a `type`'s wire is positional, so a rename there moves no bit (docs/SPEC-TABLES.md)
```

Any two fields of one table whose effective ids collide are refused too.

There is no `was` for enum variants or union arms. Renaming a variant makes a
new variant, and old data reads as `unknown`. Rename fields freely, and treat
variant names as permanent.

### The baseline: the compiler remembers what shipped

`was` solves renames and the wire absorbs almost everything else. A small set
of edits change what an old file **means** with nothing on the wire able to say
so: a specified default changed, flags inserted or removed or reordered or
renamed in place, and a field's referent swapped for one that cannot stand in
for it. For those, opt into the check:

```
$ schema tables-baseline --update --reason "first baseline before 1.0 ships" .
$ ls
Config.schema	tables.baseline
```

`tables.baseline` is a text projection of the unit's whole table closure, one
fact per line, made to be read in a diff:

```
schema-tables-baseline 6
package starlight

table ShipConfig
    field velocity id=0x2e46 kind=10 default=500.0
    field damage id=0x15a9 kind=10 default=21.0
    field perks id=0x2fc5 kind=9 flags=Perks

flags Perks
    variant Shielded bit=0
    variant Cloaked bit=1
    variant Turbo bit=2

## history
### 2026-09-04 — first baseline before 1.0 ships
- baseline created over 1 table — data written BEFORE this point is not covered by it
```

Commit it. From now on every `schema check`, and so every `generate`, diffs the
closure against it. Change `damage` from 21.0 to 25.0:

```
$ schema check .
tables.baseline: ShipConfig.damage: specified default 21.0 -> 25.0 — this edit changes what data already written MEANS, and no reader can report it; if you mean it, record it: schema tables-baseline --update --reason "..." (docs/SPEC-TABLES.md §18)
schema: 1 error(s)
```

Insert a flags variant in the middle and the refusal names every moved bit:

```
$ schema check .
tables.baseline: flags Perks: variant Cloaked moved from bit 1 to bit 2 — every stored file's bits are remapped, and nothing on the wire says so — this edit changes what data already written MEANS, and no reader can report it; if you mean it, record it: schema tables-baseline --update --reason "..." (docs/SPEC-TABLES.md §18)
tables.baseline: flags Perks: variant Turbo moved from bit 2 to bit 3 — every stored file's bits are remapped, and nothing on the wire says so — this edit changes what data already written MEANS, and no reader can report it; if you mean it, record it: schema tables-baseline --update --reason "..." (docs/SPEC-TABLES.md §18)
schema: 2 error(s)
```

Append it at the end instead, as `{ Shielded, Cloaked, Turbo, Hardened }`, and
check passes in silence. The law from Part 6, now enforced.

Sometimes you **mean** the break. The override is one command and it is never
silent:

```
$ schema tables-baseline --update .
schema: --update needs --reason: moving the baseline declares an intentional break with data already written, and the reason is what a person reads years later when an old file refuses (docs/SPEC-TABLES.md §18.4)

$ schema tables-baseline --update --reason "damage rebalanced for 2.0; saves from 1.x read the new value" .
```

That appends to the baseline's own history, which is the changelog someone
reads in three years when a 1.x save loads oddly:

```
## history
### 2026-09-04 — first baseline before 1.0 ships
- baseline created over 1 table — data written BEFORE this point is not covered by it

### 2026-09-04 — damage rebalanced for 2.0; saves from 1.x read the new value
- ShipConfig.damage: specified default 21.0 -> 25.0 [refuse]
```

### Three verdicts

The check sorts every edit into refuse, warn or pass.

**Refused**, because the meaning changes silently: defaults changed, added or
removed, flags moved, a field's wire kind or an array's element kind changed,
keyed and positional array spellings swapped, and a referent swapped for one
that cannot stand in.

**Warned**, because the runtime report already counts the loss but you should
know at edit time. Warnings print and exit 0:

```
$ schema check .
warning: tables.baseline: ShipConfig.name: capacity 32 -> 16 (a stored value longer than the new capacity is truncated and counts clamped)
```

A bare rename is a warning that hands you the fix:

```
$ schema check .
warning: tables.baseline: table ShipConfig: velocity removed and speed added in one edit — if that is a rename the wire id moved with the name and every stored value orphans: declare it `speed ... | was = "velocity"`, and pair `json = "velocity"` if the text key must survive (docs/SPEC-TABLES.md §5, §16.4)
```

Take the fix and one warning remains, about the half `was` does not carry:

```
$ schema check .
warning: tables.baseline: ShipConfig.speed: renamed under was = "velocity", which keeps the wire id and NOT the text key — the JSON key is now "speed"; pair json = "velocity" if an existing text must still read (docs/SPEC-TABLES.md §16.4)
```

**Passed in silence**, because the wire absorbs it: fields added, removed or
reordered, enum variants added anywhere, flags appended, and bounds grown.

No `tables.baseline` means no check, so the whole mechanism is opt in, and the
first baseline covers only what comes after it. Write it the day your format
first ships to anyone.

**You now have** the full evolution discipline: rename with `was`, append
flags, and a committed baseline that turns the silent hazards into compile
errors with recorded reasons.

---

## Part 11: The cook: point at a file instead of parsing it

### The problem

`GameConfigLoad` walks the wire, matches ids, and applies defaults. It is
tolerant, and priced accordingly. Starlight's shipped build loads the **same**
config off its **own** disk ten thousand times a day, and the data never
surprises it, because the build that reads the file is the build whose pipeline
wrote it. Paying the tolerant parse there buys nothing. You want the load to be:
check it is yours, then point at it.

### Cook it

A cook is the locked, laid-out-for-reading region written verbatim behind a
small header. Your pipeline produces it with the tool, from the designer tree
or from a wire file, one command either way:

```
$ schema cook --root GameConfig --in tree --out Game.cook --verbose .
report: silent — the data matched the schema exactly
cooked Game.cook: 408 bytes, build version 0x2a9be0e9cc087f60, little-endian, root GameConfig, 1 nodes, 328 data bytes, 16 attribution bytes
```

And the game opens it:

```cpp
    const GameConfig * config = GameConfigOpen( bytes.data(), (int64_t) bytes.size() );
    if ( config == NULL )
    {
        // wrong build, corrupt, truncated, or a foreign byte order:
        // fall back to a wire load, the path that carries every version
        printf( "not this build's cook\n" );
        return 0;
    }
    printf( "opened: level=%.*s corvette health=%g\n",
        config->level_name_length, config->level_name,
        config->ships[ShipType::Corvette].max_health );
```

```
$ ./open Game.cook
opened: level=Kuiper Run corvette health=500
```

`Open` verifies the header, which is the magic, the byte order, the **build
version**, the region alignment and the part lengths, and returns a pointer
**into your bytes**. Nothing is parsed, nothing is allocated, and nothing is
walked, so a one-megabyte cook and a one-gigabyte cook open in the same time,
and a mapped file's pages are touched only as you use them.

Every table gets an `Open`, fixed and variable alike. A variable root, Part 8's
`Route`, opens the same way and you chase pointers through `WaypointAt` exactly
as you did on the locked region, because the cook's data part **is** a locked
region.

### It is an accelerator, not an archive

The header check is an **identity** check and not a trust boundary. A cook is
data your own pipeline produced for exactly one build. Add one field to
`GameConfig`, regenerate, and rerun:

```
$ schema build-version .
0xa83400462ba5e852
$ ./open Game.cook
not this build's cook
```

Adding the field moved the build version, so the new build refuses the old cook
and falls back to the wire, which still loads it fine and absorbs the new field
as a default while your pipeline recooks. A big-endian cook refuses on a
little-endian build the same way:

```
$ schema cook --root GameConfig --in tree --out GameBig.cook --byte-order big --verbose .
cooked GameBig.cook: 408 bytes, build version 0x2a9be0e9cc087f60, big-endian, root GameConfig, 1 nodes, 328 data bytes, 16 attribution bytes
$ ./open GameBig.cook
not this build's cook
```

The tolerant wire remains the format of record, and the cook is the fast lane
your build key can always regenerate.

If you hold a cooked file whose provenance you doubt, because it crossed a
machine boundary or because you are diagnosing one, the **tool** validates it
offline, once, on purpose:

```
$ schema cook-check --root GameConfig --verbose Game.cook .
ok: build version 0x2a9be0e9cc087f60, little-endian, root GameConfig, 1 nodes, 328 data bytes, 16 attribution bytes, 0 reference slots
```

`cook-check` verifies every reference and count against the attribution, a
small directory the cook carries beside its data and which `--attribution
<file>` can separate out for builds that ship none. That is a person's
decision, not a flag on the hot path.

`schema uncook` turns a cook back into wire bytes, which is also the proof the
cook lost nothing:

```
$ schema uncook --root GameConfig --in Game.cook --out back.bin .
$ cmp back.bin packed.bin && echo identical
identical
```

### Cooking from code

Your C++ tools do not need the CLI. Every table also gets the write side, and
its bytes are pinned to the tool's, so the two producers are interchangeable:

```cpp
    int64_t size = GameConfigCookMeasure( value );
    std::vector<uint8_t> mine( size );
    GameConfigCook( value, mine.data(), size, TableByteOrder::Little );
```

```
$ ./open Game.cook
opened: level=Kuiper Run corvette health=500
cooked 408 bytes from code
$ cmp Game.cook Mine.cook && echo BYTE-IDENTICAL
BYTE-IDENTICAL
```

A variable root cooks straight from its builder, with `RouteCook( builder, ... )`,
and `RouteOpen` walks it in place.

### The build version, precisely

Two ids now exist in your project, and there is no third.

The **protocol id** is the packet wire's identity, and it gates connections.

The **build version** is everything a cook's or a block's bytes depend on that
is target neutral: the protocol id, every record's layout as the compiler's C
ABI model computes it, and the meaning facts, which are the defaults, ranges,
variant order and arm order. It keys cooked assets and gates no connection.

```
$ schema build-version .
0x2a9be0e9cc087f60
```

`schema build-version --facts` prints the entire digest input, which is the
file to diff when you want to know **why** the version moved:

```
$ schema build-version --facts .
schema-build-version 2
protocol 47a0d7e5d5c67c4c
byteorder little
block prologue=magic:8,build_version:8,byte_order:8
record GameConfig sizeof=324 alignof=4
    field cb1b kind=1 offset=0 size=1
    field 91cd kind=13 offset=4 size=68 type=WeaponConfig elem=8 array=bounded bound=8
    field 2d39 kind=13 offset=72 size=180 type=ShipConfig elem=60 array=keyed bound=3 key=ShipType
    field 1c74 kind=12 offset=252 size=72 bound=64
```

The same number is exported as `BuildVersion` in the generated code and stamped
into every cook header and block prologue.

The store contract is one line: your pipeline stores assets under the triple
**(asset hash, build version, byte order)**. The asset hash is the hash of the
wire file you cooked from. The build version is target neutral, which the
big-endian cook above shows by stamping the same `0x2a9be0e9cc087f60`, and
`byteorder little` appearing in the facts is a generation input that never
varies. Byte order is the third coordinate, carried in the header and refused
by `Open` on a mismatch. Your game asks for exactly that triple, anything that
would change the bytes moves the key, and you never reason about which edits
invalidate what.

Edit a table and the build version moves while the protocol id does not:

```
$ schema id .                  # before and after the new field
0x47a0d7e5d5c67c4c
0x47a0d7e5d5c67c4c
$ schema build-version .       # before and after
0x2a9be0e9cc087f60
0xa83400462ba5e852
```

Edit a `type` and both move. That is the packet and table independence from
Part 6, seen from the asset store's end.

**You now have** an asset pipeline: designer tree, then wire, then cook, opened
by pointer in constant time, keyed so staleness is impossible, with the tolerant
wire as the fallback that never breaks.

---

## Part 12: The block form: a frame another language reads in place

### The problem

Starlight's simulation is C++ and its renderer is not. Say the tools overlay is
C, or the UI layer is C#. Sixty times a second the sim produces "what to draw",
thousands of ships and lasers, and the other side wants to **point at** that
memory and iterate rather than deserialize it. Serializing 60 Hz bulk data that
never leaves the machine is paying a tolerance tax on a same-build handoff.

### You already declared it

```
const MaxShips = 4096
const MaxLasers = 8192

table RenderShip
{
    x     float32
    y     float32
    angle float32
    hull  float32 = 1.0
}

table RenderFrame
{
    frame  uint64
    ships  [..MaxShips]RenderShip
    lasers [..MaxLasers]RenderLaser
}
```

Nothing there is new: ordinary bounded arrays of ordinary fixed tables, and
`RenderFrame` still has `Measure`, `Save`, `Load` and a cook like any table.

But every fixed table also gets a third form of the same declaration, the
**block**. The instance at the front carries, per array, where its rows start,
how many there are, and how far apart they sit. The rows follow out of line at
a fixed pitch. The other side reads three numbers and points.

You reach for it by including it. `RenderBlock.h` and `RenderBlock.cpp` sit
beside `RenderTable.h`, and a project that never blocks a table compiles none
of it. Which arrays move out of line is one rule: the table's own bounded
arrays of structs, and nothing else. A row's pitch is its `sizeof`. A
variable-length table has no block form, because a pointer means no fixed
pitch.

### Produce it, wide and allocation free

```cpp
    // one extent sized from the declared maxima, allocated ONCE
    // through YOUR alloc and free pair
    RenderFrameBlockStorage storage;
    if ( !storage.Create( TableBlockDefaultAllocator() ) ) { return 1; }

    RenderFrameCounts counts = {};
    counts.ships = 3000;                          // this frame's counts
    counts.lasers = 500;

    RenderFrameBlock block;
    if ( !RenderFrameBlockBegin( block, storage, counts ) ) { return 1; }
    block.projection->frame = 1207;

    RenderShip * ships = RenderFrameShips( block );   // the array's typed base
    for ( int32_t i = 0; i < counts.ships; i++ )      // worker t fills [begin, end)
    {
        ships[i].x = (float) i;
        ships[i].y = (float) -i;
    }

    int64_t bytes = RenderFrameBlockBytes( block );   // the used extent, for the handoff
```

```
$ ./produce
block: 56064 bytes of 196672 max (frame 1207, 3000 ships)
build version 0x1cb2e5a6286e7557
```

`Begin` lays the block out for **this frame's** counts, so the handoff is 56 KB
and not the 196 KB maximum, which `RenderFrameBlockMaxBytes` names and which is
the once-ever allocation. Fill from four threads over disjoint ranges, because
nothing in the fill path locks or allocates. That is an obligation on the
generated code and not a hope.

Ask for more than you declared and the refusal names everything:

```cpp
    RenderFrameCounts too_many = counts;
    too_many.ships = 5000;
    TableBlockRefusal refusal;
    if ( !RenderFrameBlockBegin( block, storage, too_many, &refusal ) )
    {
        printf( "refused: array %s count %lld max %lld\n", refusal.array,
            (long long) refusal.count, (long long) refusal.maximum );
    }
```

```
refused: array ships count 5000 max 4096
```

A block stays valid until the next `Begin` on that storage, so double-buffer
with two storages if the consumer reads while you fill.

### Consume it, from another language, with one check

The same declaration generates the read half everywhere. Here is **C reading
the block C++ just wrote**. The bytes went through a file for the demo, and a
pointer handoff is the production path:

```c
    RenderFrameBlock block;
    if ( !render_frame_block_open( &block, aligned, (int64_t) bytes ) )
    {
        printf( "not this build's block\n" );
        return 0;
    }

    RenderShip * ships = RenderFrameships_span( &block );
    TableBlockRows rows = RenderFrameShips( &block );
    printf( "c read: frame=%llu ships=%d ships[2999]=(%g, %g)\n",
        (unsigned long long) block.projection->frame, rows.count,
        ships[2999].x, ships[2999].y );
```

```
$ cc -std=c99 -Wall -Wextra -Werror -o consume consume.c genc/RenderBlock.c genc/RenderTable.c
$ ./consume
c read: frame=1207 ships=3000 ships[2999]=(2999, -2999)
c build version 0x1cb2e5a6286e7557
```

The two builds carry the same build version, `0x1cb2e5a6286e7557` in the C++
header and the C source alike. `BlockOpen` checks the prologue, which is the
magic, the byte order, the **build version**, the base's 64-byte alignment,
every count against its declared maximum, and every extent, and then you index.
Both sides assert every row size and field offset against the compiler's shared
layout model at compile time, so a toolchain that disagrees stops and names the
record and the field rather than garbling a frame.

Two consumer obligations that the first integration always trips on.

**Alignment binds the consumer.** The producer's storage is 64-byte aligned, so
a consumer that got the bytes from a file or a socket must hand `Open` 64-byte
aligned memory too. The C demo above uses `posix_memalign`, and a C# consumer
uses native memory because a pinned `byte[]` guarantees nothing.

**It is a same-build contract.** Append one field to `RenderShip`, rebuild one
side only, and:

```
$ ./consume
not this build's block
```

Every schema edit is a regenerate on both sides. That is the trade for zero
version machinery in a 60 Hz path: nothing to absorb, nothing to ask for by
name. Data that outlives builds rides the wire, which this same table still
has.

**You now have** the simulation to renderer handoff: filled in parallel at
frame rate, read in place from another language, guarded by one comparison.

---

## Part 13: One schema, every language, and tools that walk it

### The problem

Starlight is not one program. The server is C++, the tools are C, and maybe the
launcher is C#. Every one of them needs the same constants, the same packets
and the same save format, and the tools team wants a generic config editor
without hand-writing a UI per table.

### The same bits, from any generator

Everything this tutorial has built holds across all nine targets, because one
compiler wrote all nine backends and CI compares them bit for bit. Two verified
handoffs, using the artifacts from earlier parts.

**The packet wire.** Part 4's C++ writes a `ShipState`, and C reads it with its
generated code:

```
$ schema generate --lang c --out genc .
```

```c
    serialize_read_stream_t stream;
    serialize_read_stream_init( &stream, buffer, (int) bytes );

    ShipState ship;
    ship = new_ship_state();
    if ( !read_ship_state( &stream, &ship ) )
    {
        printf( "c refused the packet\n" );
        return 1;
    }
    char names[SYSTEM_FLAGS_NAMES_MAX];
    printf( "c read the C++ packet: %s \"%s\" health=%d throttle=%.2f systems=%s\n",
        enum_name_ship_type( ship.ship_type ), ship.name, ship.health,
        ship.throttle, flag_names_system_flags( ship.systems, names, sizeof( names ) ) );
```

```
$ ./writepacket
c++ wrote 39 bytes
$ cc -std=c99 -Wall -Wextra -Werror -ffp-contract=off -I ../serialize.c -o readpacket readpacket.c ../serialize.c/serialize.c -lm
$ ./readpacket
c read the C++ packet: Corvette "Kestrel" health=750 throttle=0.63 systems=Shields|WarpDrive
```

Same Corvette, same 750, same mask. The same bits.

Note the shape of the C names. Constants become macros, `SHIP_STATE_MAX_BYTES`
and `SYSTEM_FLAGS_NAMES_MAX`, functions are `snake_case`, and a type's defaults
arrive through `new_ship_state()`, because C structs have no member
initializers. Each target gets its own idiom. Note the read slack too: the C
buffer is `SHIP_STATE_MAX_BYTES + 8`, since C and C++ read 64-bit windows.

**The table wire.** Part 6's C++ saved `ship.bin`, and C loads it:

```c
    TableReport report = { 0, 0, 0, 0, 0 };
    ShipConfig ship;
    ship_config_reset( &ship );
    int ok = ship_config_load( &ship, buffer, (int64_t) bytes, &report );
```

```
$ ./ctable
c load: ok=1 name=Kestrel health=250 armor=8 type=Corvette unknown=0
```

Same surface, C spelling, same report counters. A table's defaults arrive
through `ship_config_reset( &value )` in place rather than through a
`new_<table>()` returning a value, so the two declaration kinds have two
spellings for the same idea in this target.

Part 12 already showed the third wire, the block, crossing C++ to C by pointer.
Cook, block, JSON and reflection all ride the same goldens, which keep every
port byte identical to the C++ reference.

### Reflection: tools without hand-written mirrors

Every type in a table closure carries a static descriptor with names, wire ids
and kinds, storage offsets, ranges, enum vocabularies, optionals and branch
guards. That is enough to write a generic editor, printer or differ with no
RTTI and no schema files shipped at runtime:

```cpp
    const TableTypeInfo * type = ShipConfigTableType();
    printf( "table %s (%u bytes, %d fields)\n", type->name, type->size, type->num_fields );
    for ( int32_t i = 0; i < type->num_fields; i++ )
    {
        const TableFieldInfo & field = type->fields[i];
        printf( "  %-14s %-10s id=0x%04x kind=%-2d @%u", field.name, field.type_name,
            field.id, field.kind, field.offset );
        if ( field.has_range ) { printf( "  [%g, %g]", field.range_min, field.range_max ); }
        for ( int64_t v = 1; field.enum_name && v <= field.enum_max; v++ )
        {
            printf( " %s", field.enum_name( (uint64_t) v ) );
        }
        printf( "\n" );
    }
```

```
$ ./reflect
table ShipConfig (56 bytes, 5 fields)
  display_name   string     id=0xcb8b kind=12 @0
  max_health     float32    id=0x7770 kind=10 @40
  max_speed      float32    id=0xae53 kind=10 @44
  armor          int32      id=0x7c9d kind=4  @48  [0, 10]
  ship_type      ShipType   id=0x38e9 kind=7  @52 Fighter Freighter Corvette
```

The descriptor carries more than the walk above prints: the JSON key beside the
name, the offsets of `_count` and `_present` companions, a union's tag and arm
table, an enum-keyed array's key vocabulary, the exact 128-bit range of a wide
field, and a `reset` that puts an instance back at its declared defaults in
place. Decoded tables are relocatable, trivially copyable, standard layout and
pointer free, with generated static assertions enforcing it, so a walker can
read any instance through those offsets wherever it sits, mapped files
included.

This is the machinery the JSON codec and `schema pack` ride, and it is yours
too.

### Embedding the compiler

The `schema` binary is a thin client of a Go library, so anything the CLI does
your build tools can do in process, and you can register your own generator
beside the nine:

```go
type docs struct{}

func (docs) Names() []string { return []string{"docs"} }

func (docs) Generate(u *ir.Unit, opts compiler.Options) (map[string][]byte, error) {
	out := []byte{}
	for _, s := range u.Structs {
		out = append(out, fmt.Sprintf("## %s — %d bits max\n", s.Name, ir.MaxBitsStruct(s))...)
	}
	return map[string][]byte{"TYPES.md": out}, nil
}

func main() {
	c := compiler.New()
	c.Register(docs{})                      // your own target: --lang docs
	fmt.Println(c.Targets())

	paths, _ := compiler.GatherPaths([]string{"../starlight"})
	unit, _ := c.Load(paths)                // format free: writes nothing
	fmt.Printf("package %s, protocol id 0x%016x\n", unit.Package, unit.ProtocolId)

	files, _ := c.Generate(unit, "docs", nil)   // returns bytes, writes nothing
	fmt.Print(string(files["TYPES.md"]))
}
```

```
$ go build -o docsgen . && ./docsgen
[c cpp cs dart docs elixir go java js rust]
package starlight, protocol id 0xc353869c04a1ae19
## Vector3 — 192 bits max
## ShipState — 705 bits max
```

Your generator receives the same resolved IR the built-in backends read, with
types resolved, constants folded and widths derived. `ir.MaxBitsStruct` says
705 bits for `ShipState`, which is the same number Part 4's header carried. The
exported `compiler` and `ir` surfaces are under semantic versioning, and
`internal/` is not.

**You now have** Starlight's whole estate on one schema: a C++ simulation, C
tools, generic editors, plus a compiler you can embed when the build gets
opinionated.

---

## Part 14: The tool belt, and where the edges are

### Every command you now know

All of these appeared on the way, and this is the map. `fmt` is the only one
that writes a `.schema` file, so every other command reads the unit as it sits
on disk and a read-only checkout works. Every one is silent on success:

```
schema check      [--verbose] [dir|files...]          compile, no output
schema generate   [--lang ...] [--out ...] [dir]      code for one target
schema fmt        [--verbose] [dir|files...]          format, the only writer
schema id         [dir]                               the protocol id
schema projection [dir]                               the text the id hashes
schema build-version [--facts] [dir]                  the cook and block key, and its inputs
schema tables-baseline [--update --reason "..."]      commit the evolution baseline
schema pack       --root T --out f.bin <tree>         directory tree to wire bytes
schema unpack     --root T --in f.bin [--one-file]    wire bytes to directory tree
schema cook       --root T --in x --out f.cook        wire or tree to cooked file
                  [--byte-order little|big] [--attribution f]
schema cook-check [--root T] f.cook                   offline validation, on purpose
schema uncook     --root T --in f.cook --out f.bin    cook to wire, the no-loss proof
schema version                                        which build of the tool
```

Flags come before the paths. A flag after the first path is read as another
path, and the failure says so awkwardly:

```
$ schema generate --lang cpp --out gen . --verbose
schema: stat --verbose: no such file or directory
```

`pack`, `unpack` and `cook` exit nonzero on any non-silent report, `--tolerate`
accepts it, and `--verbose` prints it regardless.

When something refuses and you want to know which schema fact is in play,
`schema projection` for packets and `schema build-version --facts` for tables,
cooks and blocks print the exact text the ids digest. Diff those between two
checkouts and the moved line is your answer.

One nudge you will meet the first time you declare a table without committing a
baseline:

```
notice: bad declares 1 table and . holds no tables.baseline — save-game evolution is unguarded (docs/SPEC-TABLES.md §18); commit one with: schema tables-baseline --update --reason "first baseline" .
```

It is a notice and not an error, so nothing stops. Take it the day your format
first ships to anyone.

One habit the ids reward: check `schema version` against the docs you are
reading. The language moves, and a refusal that cites a specification section
cites the tool's specification at its build, not necessarily the page you have
open.

### Where the edges are

Three deliberate boundaries you will meet, so you do not read a refusal as a
defect.

**Packet-wire framing stays off the table wire.** `const`, `reserved` and
`align` are bit positions, and a table's wire has none:

```
table T { const(0xC7, 8) ... }
    Bad.schema:3:11: const(value, bits) is a packet-wire construct — a table's wire is field-tagged TLV with no bit positions; remove it from table T (docs/SPEC-TABLES.md)
```

**A `type` never reaches a table.** Packets are exact match and value
semantics, so a `type` cannot hold a table, a pointer or a map. A table may
hold types freely. That one-way rule is what keeps a packet's cost independent
of everything tables buy.

**`bytes` needs a bound.** `bytes(N)` is a bounded buffer, and a bare `bytes`
does not parse. For a buffer whose size is a runtime fact, point at it, since
`*bytes` and `*string` are pointer fields like any other.

One thing the specification describes that no backend emits yet: the **unit
registry**, SPEC-TABLES §8.3's `UnitView()`, one entry point listing every
declaration in the build. The per-table and per-type descriptors Part 13 walked
are live in every target. The unit-wide root is not:

```
$ schema generate --lang cpp --out gen --verbose .
wrote gen/Config.h
wrote gen/ConfigBlock.cpp
wrote gen/ConfigBlock.h
wrote gen/ConfigTable.cpp
wrote gen/ConfigTable.h
wrote gen/ConfigWire.h
```

No view file, in any of the nine. Build your tool on the per-type descriptors
and hold the type list yourself.

### Where to go next

[SPEC.md](SPEC.md) is the packet language precisely: the grammar, every field
kind's wire encoding, the trust model, and the refusal catalogue.

[SPEC-TABLES.md](SPEC-TABLES.md) is the table system end to end: the wire, the
two classes, the cook, the block, the baseline, and every ruling with its
reasons.

[USAGE.md](USAGE.md) is every feature with the code it generates, per language.

[PORTING.md](PORTING.md) is the per-language register, with a cell for every
method and instrument a backend carries.

`examples/` in the repository holds canonical schemas exercising the whole
surface, and `make check` runs the compiler over them.

### What you built

Starlight's protocol, from one constant to a nine-language estate: packets that
refuse hostile bytes in three bytes a ship, saves that survive two years of
schema drift and say exactly what they absorbed, a designer JSON loop, fleets
keyed by name, cooked configs that open by pointer, and a 60 Hz block handoff
read in place from another language.

Every byte of it came out of declarations short enough to read aloud.
