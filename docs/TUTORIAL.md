# The schema tutorial

You are going to build the network protocol and the data files for a small
space game, starting from an empty directory. By the end you will have packets
that cross the network in a handful of bytes, save games a build from next year
can still read, config files your designers edit as JSON, and cooked assets
your game opens without parsing a byte.

The game is called Starlight. It has ships.

Fourteen parts. Each part adds one thing to **one growing schema unit**, and
each part ends with something that builds and runs. Every command on this page
is shown with the output it actually prints.

1. [A file, a package, and your first constants](#part-1-a-file-a-package-and-your-first-constants)
2. [Naming things: enums and flags](#part-2-naming-things-enums-and-flags)
3. [The first packet: types and ranged integers](#part-3-the-first-packet-types-and-ranged-integers)
4. [The rest of the field types](#part-4-the-rest-of-the-field-types)
5. [Branches, unions, and your protocol](#part-5-branches-unions-and-your-protocol)
6. [Tables: data that outlives the build](#part-6-tables-data-that-outlives-the-build)
7. [Shaping real data: optionals, keyed arrays, and a second unit](#part-7-shaping-real-data-optionals-keyed-arrays-and-a-second-unit)
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

### Get the tools

The compiler is one Go program. You need a Go toolchain, and nothing else:

```
$ go version
go version go1.27.0 darwin/arm64
```

Clone the repository and build:

```
$ git clone https://github.com/mas-bandwidth/schema.git
$ cd schema
$ make
go build -ldflags "-X github.com/mas-bandwidth/schema/v2/internal/version.version=v2.4.0-148-gbafdb69" -o bin/schema ./cmd/schema
```

That is the whole install. `make` builds `bin/schema` and nothing else: no
language toolchains, no runtime checkouts, no generation. Put `bin` on your
path, because every command on this page is spelled `schema`:

```
$ export PATH="$PWD/bin:$PATH"
$ schema version
schema v2.4.0-148-gbafdb69 (go1.27.0)
```

Write that number down. The language moves, and a diagnostic that cites a
specification section cites it as it stands in the build that printed the
diagnostic. When this page and your binary disagree, `schema version` is the
first thing to check.

**Two more checkouts, before Part 3.** The generated C++ and C read and write
bits through the serialize runtimes, which are header-only siblings of this
repository. Clone them beside it, at the tags this build is pinned to:

```
$ cd ..
$ git clone --branch v1.16.2 https://github.com/mas-bandwidth/serialize.git
$ git clone --branch v1.9.2  https://github.com/mas-bandwidth/serialize.c.git
```

Nothing to build in either. Part 3 adds `-I ../serialize` to a compile line and
Part 13 adds `-I ../serialize.c`. Parts 6 through 12 need neither, because
generated table code depends on nothing.

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
becomes the C++ namespace, the Go package, the C# namespace, and so on. This
tutorial grows one unit, `starlight`, from that file to nine of them.

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
argue with. Run `schema fmt` after every edit on this page, and the files you
have will be the files the page shows.

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

### About the ids on this page

`ProtocolId` is a hash of the **whole unit's wire shape**, so it changes every
time you add or edit a declaration. Part 5 earns it. What matters here is that
every id, digest and byte count printed on this page belongs to the unit **as
it stands at that point in the tutorial**, and the unit grows in every part.

Follow along and yours will match. If one does not, the two commands that say
why are:

```
$ schema id .
0x657f3fb8771fbe6e
```

and `schema projection .`, which prints the exact text the id hashes. Diff
your projection against the page's and the line that differs is the
declaration you typed differently.

### A program

`main.cpp`, beside your schema files:

```cpp
#include "Game.h"
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
$ c++ -std=c++17 -Wall -Wextra -Werror -I gen -o starlight main.cpp
$ ./starlight
64 players at 60 Hz, 0.016667 s per tick
```

`Game.h` includes `<cstdint>` and nothing else, so this builds with no runtime
and no include path beyond `gen`. The serialize runtime arrives in Part 3, with
`GameWire.h`.

### When you get it wrong

Each of these is its own file in its own directory, so the mistake does not
have to live in your `starlight` unit. `bad/Bad.schema`:

```
package bad

const A = B + 1
```

```
$ schema check .
Bad.schema:3:11: undefined constant B
schema: 1 error(s)
```

```
package bad

const C int32 = 1.5
```

```
$ schema check .
Bad.schema:3:1: constant C has integer type int32 but a float expression
schema: 1 error(s)
```

```
package bad

const Team = 1
enum Team { Red }
```

```
$ schema check .
Bad.schema:4:1: duplicate declaration "Team" (first declared at Bad.schema:3:1; all declaration kinds share one unit-level namespace — SPEC §4.6)
schema: 1 error(s)
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

Add to `Game.schema`, so the whole file now reads:

```
package starlight

const MaxPlayers   = 64
const TickRate     = 60
const TickInterval = 1.0 / TickRate
const Pi float32   = 3.14159265359

enum ShipType { Fighter, Freighter, Corvette }

enum Pending { }

enum Weapon | max = 15
{ Laser, Missile }

flags SystemFlags { Shields, Cloak, WarpDrive, Autopilot }
```

Regenerate and look at what came out:

```
$ schema fmt . && schema generate --lang cpp --out gen .
```

```cpp
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

Try to declare either name yourself and the compiler refuses. `bad/Bad.schema`:

```
package bad

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

Write the variants one per line without commas, the way fields are written, and
the parser stops. `bad/Bad.schema`:

```
package bad

enum Big
{
    A
    B
}
```

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
drive, autopilot. An enum is one-of and you want any-of. That is
`flags SystemFlags`, from the file above:

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
caps a flags declaration at 64 variants. `bad/Bad.schema` with 65 of them:

```
$ schema check .
Bad.schema:3:1: flags F has 65 variants — one bit per variant, up to 64 (SPEC §4.2)
schema: 1 error(s)
```

There is no implicit `None`, because the empty mask is `0` and needs no name.
The declared count is exported as `SystemFlagsCount` and is usable in a schema
expression as `SystemFlags.Count`, the same word an enum carries meaning the
same thing.

There is no `SystemFlags.Max`. Ask for it in `bad/Bad.schema`:

```
package bad

flags F { A, B }

type T
{
    x int32 | min = 0, max = F.Max
}
```

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
inline constexpr int SystemFlagsNamesMax = 54;
```

`SystemFlagsNamesMax` bytes always suffice. The empty set renders as `"0"`, and
any bits past the declared variants render honestly as hex.

Note the `using namespace starlight;` in the program below. A flags type is a
`uint64_t` alias, so argument-dependent lookup has no namespace to find and the
flags helpers must be reachable by name. `EnumName` takes a real enum class and
is found by lookup wherever its argument comes from.

### Headroom: `| max`

The wire cost of an enum is the fewest bits that cover `[0, Max]`, so 2 bits
for our four `ShipType` values counting `None`. Add a fourth variant later and
the width grows to 3 bits, which as Part 5 shows changes the protocol. When you
know a set will grow, reserve headroom at the declaration, the way `Weapon`
does above:

```cpp
enum class Weapon : uint8_t {
    None = 0,
    Laser = 1,
    Missile = 2,
    Count = 2, // the declared variant count (SPEC §4.2)
    Max = 15, // the exported extent (SPEC §4.2)
};
```

The wire is now 4 bits, room for 15 variants, and stays 4 bits as you add them.
This is where `Count` and `Max` part company: 2 variants declared, an extent of
15. Size a keyed array by `Max` and loop the real variants by `Count`. A read
accepts any value in `[0, 15]`, values you have not named included, so a
`switch` over a headroom enum keeps a `default`.

The attribute after `|` is the language's one qualification syntax, and you
will meet it constantly from Part 3 on. On a declaration line the body brace
opens on the next line, as `Weapon` shows.

`enum Pending { }` declares no variants, only the implicit `None`:

```cpp
enum class Pending : uint8_t {
    None = 0,
    Count = 0, // the declared variant count (SPEC §4.2)
    Max = 0, // the exported extent (SPEC §4.2)
};
```

Its wire range is the degenerate `[0, 0]`, so it costs **zero bits**. You can
declare a kind before its variants are known, fields of it round-trip as
`None`, and nothing is spent on the wire until the first variant arrives.

### A program

`main.cpp`, entire:

```cpp
#include "Game.h"
#include <cstdio>

int main()
{
    using namespace starlight;

    printf( "%lld players at %lld Hz, %f s per tick\n",
            (long long) MaxPlayers, (long long) TickRate, TickInterval );

    for ( int i = 1; i <= (int) ShipType::Count; i++ )
    {
        printf( "ship type %d is %s\n", i, EnumName( (ShipType) i ) );
    }

    SystemFlags mask = SystemFlags_Shields | SystemFlags_WarpDrive;
    char names[SystemFlagsNamesMax];
    printf( "systems: %s\n", FlagNamesSystemFlags( mask, names, sizeof( names ) ) );
    printf( "weapon extent %d, declared %d\n", (int) Weapon::Max, (int) Weapon::Count );
    printf( "pending extent %d, declared %d\n", (int) Pending::Max, (int) Pending::Count );
    return 0;
}
```

```
$ c++ -std=c++17 -Wall -Wextra -Werror -I gen -o starlight main.cpp
$ ./starlight
64 players at 60 Hz, 0.016667 s per tick
ship type 1 is Fighter
ship type 2 is Freighter
ship type 3 is Corvette
systems: Shields|WarpDrive
weapon extent 15, declared 2
pending extent 0, declared 0
```

The unit's id moved when you added those declarations, as promised in Part 1:

```
$ schema id .
0x4864d311d2321f9f
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

Add one constant and one declaration to `Game.schema`. The constants now read:

```
const MaxPlayers   = 64
const MaxHealth    = 1000
const TickRate     = 60
const TickInterval = 1.0 / TickRate
const Pi float32   = 3.14159265359
```

and at the end of the file:

```
type ShipState
{
    ship_type ShipType
    health    int32       | min = 0, max = MaxHealth
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
inline constexpr int64_t ShipStateMaxBytes = 8; // rounded up to the 8-byte write-buffer granularity; a read buffer's allocation must extend at least 8 bytes past the data — the reader loads 64-bit windows
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

`GameWire.h` includes `serialize.h`, which is the header-only checkout you made
in Part 1. `main.cpp`, entire:

```cpp
#include "GameWire.h"
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

    uint8_t evil[64] = {};
    serialize::WriteStream forge( evil, sizeof( evil ) );
    write_bits( forge, 3, 2 );        // ship_type = Corvette
    write_bits( forge, 1013, 10 );    // health = 1013, out of range
    write_bits( forge, 0, 4 );        // systems
    write_bits( forge, 0, 1 );        // docked
    forge.Flush();

    ShipState hacked;
    serialize::ReadStream check( evil, forge.GetBytesProcessed() );
    printf( "hostile read: %s\n", ReadShipState( check, hacked ) ? "ACCEPTED" : "refused" );
    return 0;
}
```

```
$ c++ -std=c++17 -Wall -Wextra -Werror -ffp-contract=off -I gen -I ../serialize -o starlight main.cpp
$ ./starlight
wrote 3 bytes
read back: Corvette health=750 systems=Shields|WarpDrive docked=0
hostile read: refused
```

Three bytes for the whole ship state, which is seventeen bits flushed to byte
granularity. That compile line is the one every later part uses, so keep it.

Note `-ffp-contract=off`. Generated float code must produce identical bits on
both ends of a connection, and a contracted multiply-add produces different
bits from the pair, so build wire code with contraction off.

### The reader assumes the bytes are hostile

The second half of that program forges a packet: the same bit layout, written
by hand, with 1013 in the health bits. A value outside its declared range is
**refused, not clamped**. The read returns false and you drop the packet. The
same goes for counts past an array bound, string lengths past their maximum,
enum values above the extent, and reads that run past the end of the buffer.
This holds in all nine languages, because the same compiler wrote all nine
readers.

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

Each of these is a `bad/Bad.schema` of the form

```
package bad

type T
{
    <the field line below>
}
```

and each refusal names the rule:

```
h int32 | min = 0
    Bad.schema:5:5: field h: min without max (or vice versa) is a compile error (SPEC §4.6)
    schema: 1 error(s)

h int32 | min = 10, max = 5
    Bad.schema:5:15: inverted range [10, 5] — min must not exceed max (SPEC §4.6)
    schema: 1 error(s)

h int8 | min = 0, max = 1000
    Bad.schema:5:5: field h: range [0, 1000] does not fit its declared storage int8 (SPEC §4.6 — a legal wire value the storage truncates would be silent corruption)
    schema: 1 error(s)

h int32 = 2000 | min = 0, max = 1000
    Bad.schema:5:15: field h: default 2000 is outside its range [0, 1000]
    schema: 1 error(s)

count int32 | min = 1, max = 4
    Bad.schema:5:5: field count: its range [1, 4] excludes zero, so the implicit zero default is outside it — declare a default in range, count = 1 (SPEC §4.6)
    schema: 1 error(s)
```

The last one deserves a beat. Everything in this language zero-initializes, so
a range that excludes zero would leave a freshly constructed value outside its
own range. The compiler makes you pick the rest state:
`count int32 = 1 | min = 1, max = 4`. That is also your introduction to
**defaults**, the `= value` before the `|`, which exist for exactly the fields
whose rest state is not zero.

```
$ schema id .
0xf1e2edc1eb4168d8
```

**You now have** a real network message, three bytes on the wire, hostile input
refused, in any of nine languages.

---

## Part 4: The rest of the field types

The ship state is about to grow. Each addition below is a problem Starlight
actually has, and each introduces one field kind. At the end of the part
`Game.schema` is shown entire.

### Ships have names: `string(N)`

```
const MaxShipName  = 24

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

Ranged floats take `min`, `max` **and** `resolution`, all three together.
`bad/Bad.schema` leaving one off:

```
package bad

type T
{
    a float32 | min = 0, max = 1
}
```

```
$ schema check .
Bad.schema:5:5: field a: a float range is min, max and resolution, all three together (SPEC §4.6)
schema: 1 error(s)
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

### Aim: storage that speaks your math

Your engine already has a 2D vector with operators on it. `cpp_native` maps the
generated storage onto it, so simulation code does vector arithmetic directly
on packet fields. Put the declaration in its own file, `Vectors.schema`, in the
same unit:

```
package starlight

type Vector2 | cpp_native = GameVector2, cpp_include = "game_vector2.h"
{
    x float32
    y float32
}
```

and give `ShipState` an `aim Vector2` field. `Game.h` now carries your include
and your type:

```cpp
#pragma once

#include <cstdint>

#include "Vectors.h"

// native type mapping (cpp_native, SPEC §4.2): the hand types storage speaks
#include "game_vector2.h"

namespace starlight {
```

```cpp
    ::GameVector2 aim;
```

`game_vector2.h` is yours to write, and it is short. Your type derives from the
generated basis struct, so the layout is the schema's and the operators are
yours:

```cpp
#pragma once

#include "Vectors.h"

// Your own type. It derives from the generated basis struct, so the layout is
// the schema's and the operators are yours.
struct GameVector2 : starlight::Vector2
{
    GameVector2 & operator+=( const GameVector2 & o ) { x += o.x; y += o.y; return *this; }
};
```

Two rules govern the mapping. It applies where the type is referenced from **a
different schema file** than the one that declares it, which is why `Vector2`
lives in `Vectors.schema` and `ShipState` in `Game.schema`. Declare and use it
in one file and the generated output carries no trace of the mapping. And the
name is a **global** identifier, spelled `::GameVector2` in the output, so a
namespaced engine type needs a global alias.

From here on the compile line gains `-I .`, so the compiler finds
`game_vector2.h` beside your schema files.

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

`I + F` must land on a storage width. `bad/Bad.schema`:

```
package bad

type T
{
    a ufixed(16, 8) | min = 0, max = 100
}
```

```
$ schema check .
Bad.schema:5:7: ufixed(16, 8): I + F = 24 must equal a storage width — 8, 16, 32, 64 or 128 (SPEC §4.6)
schema: 1 error(s)
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

An element range that excludes zero is refused for the same reason a scalar's
is. `bad/Bad.schema`:

```
package bad

type T
{
    a [..8]int32 | min = 1, max = 4
}
```

```
$ schema check .
Bad.schema:5:5: field a: its range [1, 4] excludes zero, so every element is born outside it — an array takes no specified default, so widen the range to reach zero (SPEC §4.6)
schema: 1 error(s)
```

The **count** bound is not held to that rule, and it is worth seeing why that
matters. `window [2..8]uint32` compiles:

```
$ schema check .
$ schema generate --lang cpp --out g .
```

```cpp
struct T {
    uint32_t window[8] = {}; // used count beside it; wire count in [2, 8]
    int32_t window_count = 0;
};
```

```cpp
SCHEMA_WRITE_INLINE bool WriteT( serialize::WriteStream & stream, const T & value )
{
    serialize_assert( int32_t( value.window_count ) >= int32_t( 2 ) && int32_t( value.window_count ) <= int32_t( 8 ) );
    write_bits( stream, uint32_t( value.window_count ) - uint32_t( 2 ), 3 );
```

A freshly constructed `T` has `window_count = 0`, outside the count's own wire
range, and the write subtracts the low bound before it packs. Reach for
`[A..B]` with A above zero only where your code sets the count before the value
is ever written.

### The odds and ends

`bits(12)` is an unsigned field of exactly 12 bits, for when the width **is**
the specification: sequence numbers, opaque tags. Storage is `uint32_t`.

`bytes(1024)` is `string` with no text contract: a length-prefixed run of raw
bytes in fixed storage.

`int128` and `uint128` are full-width 128-bit integers, `serialize::uint128_t`
in C++, for keys and hashes that ride through untouched.

### The grown unit

`Game.schema`, entire:

```
package starlight

const MaxPlayers   = 64
const MaxHealth    = 1000
const MaxShipName  = 24
const TickRate     = 60
const TickInterval = 1.0 / TickRate
const Pi float32   = 3.14159265359

enum ShipType { Fighter, Freighter, Corvette }

enum Pending { }

enum Weapon | max = 15
{ Laser, Missile }

flags SystemFlags { Shields, Cloak, WarpDrive, Autopilot }

type Vector3
{
    x float64
    y float64
    z float64
}

type ShipState
{
    ship_type ShipType
    name      string(MaxShipName)
    health    int32 = MaxHealth   | min = 0, max = MaxHealth
    throttle  float32             | min = 0, max = 1, resolution = 0.01
    position  Vector3
    aim       Vector2
    heading   fixed(16, 16)       | min = -180, max = 180
    systems   SystemFlags
    docked    bool
    cargo     [..8]uint32
}
```

`health` grew a default of `= MaxHealth`. Ships start at full health, so the
rest state is not zero, and now a fresh `ShipState` starts at 1000 with no
constructor anywhere:

```cpp
struct ShipState {
    ShipType ship_type = ShipType::None;
    char name[MaxShipName + 1] = {}; // string(MaxShipName): max length, used length beside it (SPEC §4.7)
    int32_t name_length = 0;
    int32_t health = MaxHealth; // wire [0, 1000]
    float throttle = 0.0f; // compressed float [0, 1] @ 0.01
    Vector3 position;
    ::GameVector2 aim;
    int32_t heading = 0; // fixed(16, 16) — Q16.16, raw value scaled by 2^16; bounds in whole units; wire [-180, 180]
    SystemFlags systems = 0;
    bool docked = false;
    uint32_t cargo[8] = {}; // used count beside it; wire count in [0, 8]
    int32_t cargo_count = 0;
};

inline constexpr int64_t ShipStateMaxBits = 769; // longest wire path; align pads at worst case (SPEC §6.1)
inline constexpr int64_t ShipStateMaxBytes = 104; // 8-byte write granularity; read slack per the contract above
```

Everything is inline, fixed size and trivially copyable. You can `memcpy` a
`ShipState`, hold arrays of them, or share them between processes. That property
is by construction, and later parts build on it. `ShipStateMaxBytes` is the
number to size a send buffer with, and it accounts for the read slack the
contract asks for.

### A program

`main.cpp`, entire:

```cpp
#include "GameWire.h"
#include <cstdio>
#include <cstring>

int main()
{
    using namespace starlight;

    ShipState ship;
    ship.ship_type = ShipType::Freighter;
    strcpy( ship.name, "Kestrel" );
    ship.name_length = 7;
    ship.throttle = 0.63f;
    ship.position = { 100.0, 25.5, -3.0 };
    ship.aim.x = 1.0f;
    ship.aim.y = 0.0f;
    GameVector2 drift;
    drift.x = 0.5f;
    drift.y = 0.25f;
    ship.aim += drift;                 // your operator, on generated storage
    ship.heading = (int32_t) ( 42.5 * 65536 );
    ship.systems = SystemFlags_Shields;
    ship.cargo_count = 3;
    ship.cargo[0] = 11;
    ship.cargo[1] = 22;
    ship.cargo[2] = 33;

    uint8_t buffer[ShipStateMaxBytes];
    serialize::WriteStream writer( buffer, sizeof( buffer ) );
    WriteShipState( writer, ship );
    writer.Flush();

    ShipState copy;
    serialize::ReadStream reader( buffer, writer.GetBytesProcessed() );
    ReadShipState( reader, copy );

    printf( "%s \"%s\" health=%d throttle=%.2f aim=(%g, %g) heading=%.2f cargo=%d\n",
        EnumName( copy.ship_type ), copy.name, copy.health, copy.throttle,
        copy.aim.x, copy.aim.y, copy.heading / 65536.0, copy.cargo_count );
    printf( "%lld bytes of %lld max\n",
        (long long) writer.GetBytesProcessed(), (long long) ShipStateMaxBytes );
    return 0;
}
```

```
$ c++ -std=c++17 -Wall -Wextra -Werror -ffp-contract=off -I gen -I . -I ../serialize -o starlight main.cpp
$ ./starlight
Freighter "Kestrel" health=1000 throttle=0.63 aim=(1.5, 0.25) heading=42.50 cargo=3
59 bytes of 104 max
```

Fifty-nine bytes, most of it the three `float64` of the position, which is what
a `float64` costs when you ask for a whole one. The `aim` came back through your
own `operator+=`.

```
$ schema id .
0xd795780496e81500
```

**You now have** the full vocabulary of scalar and buffer fields, a ship state
that costs a fraction of its struct size on the wire, and one field whose
storage is your engine's own type.

---

## Part 5: Branches, unions, and your protocol

### The problem

A docked ship has no velocity, no throttle and no radar contacts, and sending
those fields for every ship every tick is paying for fields that mean nothing.
Beyond that, Starlight has more than one **kind** of message. A fire command is
not a chat line is not a snapshot, and so far a type is one fixed layout.

Everything in this part goes in a new file of the same unit, `Net.schema`,
shown entire at the end. Three constants join `Game.schema` first:

```
const MaxPlayers           = 64
const MaxHealth            = 1000
const MaxShipName          = 24
const MaxChatText          = 120
const MaxShipsPerSnapshot  = 8
const MaxPayloadsPerPacket = 4
const TickRate             = 60
const TickInterval         = 1.0 / TickRate
const Pi float32           = 3.14159265359
```

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
comes next, and it is exactly a field name with no expressions. `bad/Bad.schema`
breaking each rule:

```
package bad

type T
{
    if moving
    {
        a int32
    }
    moving bool
}
```

```
$ schema check .
Bad.schema:5:8: if condition moving must be a bool field declared earlier in the same or an enclosing block (the dominance rule, SPEC §4.5)
schema: 1 error(s)
```

```
package bad

type T
{
    kind uint8
    if kind
    {
        a int32
    }
}
```

```
$ schema check .
Bad.schema:6:8: if condition kind must be a bool field (SPEC §4.6)
schema: 1 error(s)
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
a stream of unions can end on `None`. The program below prints that tag.

The tag enum is not a full enum. It carries `None` and `Max`, and it does not
carry `Count` or a name function, so `EnumName( cmd.fire.type )` does not
compile. Log the tag by switching on it, which the program below does.

To select an arm in C++, set the tag and value-establish the arm, both, in that
order of importance:

```cpp
    first.type = PayloadType::Fire;
    first.fire = FireCommand{};            // zero-establish the arm
    first.fire.ship_id = 7;                // then fill it
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

In a `type` body every arm names a declared type. `bad/Bad.schema` with a
scalar arm:

```
package bad

type A { x uint16 }

union U
{
    a A
    ping uint32
}

type T { u U }
```

```
$ schema check .
Bad.schema:11:12: U has the arm ping uint32, and a union in a `type` body takes `type` payloads only — types are value semantics, and an arm that is not a declared type has no packet wire yet; hold the union in a table body, or make the arm a type (docs/SPEC-TABLES.md §2.6, §11, §15)
Bad.schema:5:1: union U: the arm ping uint32 is not a declared type, and no table reaches U — such an arm is a table-closure construct, and a union outside one takes `type` payloads only; hold the union in a table body, or make the arm a type (docs/SPEC-TABLES.md §2.6, §11, §15)
schema: 2 error(s)
```

The second message names the way out: a union whose arms are field lines of any
type belongs to a table closure. Part 7 builds one.

### Your protocol is a union away

A union of message payloads plus your own framing is a protocol:

```
type ChatLine
{
    speaker uint16
    text    string(MaxChatText)
}

type Snapshot
{
    tick  uint32
    ships [..MaxShipsPerSnapshot]ShipState
}

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

One declaration buys the tag enum, the validated tag wire, and `WritePacket`
and `ReadPacket` end to end. If you prefer a terminated stream to a counted
array, write `Payload` values back to back and end on the in-band `None`.

### Framing: `const`, `reserved`, `align`

A real packet usually opens with framing: a magic byte so junk is rejected in
the first 8 bits, a version nibble, and room to grow. Three storage-less
constructs write wire with no field behind them:

```
type PacketHeader
{
    const(0xC7, 8)
    version bits(3)
    reserved(5)
    align
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

`const` is written always and a read **rejects any other value**. `reserved(N)`
rides as zeros, a read rejects nonzero, and you can claim the bits later.
`align` zero-pads to the byte boundary and a read rejects a nonzero pad, which
earns its keep before bulk data because an aligned `bytes(N)` body can be
copied instead of bit-shifted. Strings and bytes align internally already:
length, align, then the raw bytes.

### A program

`main.cpp`, entire:

```cpp
#include "NetWire.h"
#include <cstdio>
#include <cstring>

int main()
{
    using namespace starlight;

    // one packet: a fire command and a chat line
    Packet packet;
    packet.sequence = 1000;
    packet.payloads_count = 2;

    Payload & first = packet.payloads[0];
    first.type = PayloadType::Fire;
    first.fire = FireCommand{};
    first.fire.ship_id = 7;
    first.fire.fire.type = WeaponFireType::Laser;
    first.fire.fire.laser = LaserFire{};
    first.fire.fire.laser.target_id = 12;
    first.fire.fire.laser.power = 0.75f;

    Payload & second = packet.payloads[1];
    second.type = PayloadType::Chat;
    second.chat = ChatLine{};
    second.chat.speaker = 7;
    strcpy( second.chat.text, "on my way" );
    second.chat.text_length = 9;

    uint8_t buffer[PacketMaxBytes];
    serialize::WriteStream w( buffer, sizeof( buffer ) );
    WritePacket( w, packet );
    w.Flush();

    Packet back;
    serialize::ReadStream r( buffer, w.GetBytesProcessed() );
    ReadPacket( r, back );

    printf( "packet %u: %d payloads in %lld bytes\n",
        back.sequence, back.payloads_count, (long long) w.GetBytesProcessed() );
    for ( int32_t i = 0; i < back.payloads_count; i++ )
    {
        const Payload & p = back.payloads[i];
        switch ( p.type )
        {
            case PayloadType::Fire:
                printf( "  fire: ship %u laser at %u power %.2f\n",
                    p.fire.ship_id, p.fire.fire.laser.target_id, p.fire.fire.laser.power );
                break;
            case PayloadType::Chat:
                printf( "  chat: %u says \"%s\"\n", p.chat.speaker, p.chat.text );
                break;
            case PayloadType::Snapshot:
                printf( "  snapshot: tick %u, %d ships\n", p.snapshot.tick, p.snapshot.ships_count );
                break;
            case PayloadType::None:
                printf( "  none\n" );
                break;
            default:
                printf( "  unknown tag\n" );
                break;
        }
    }

    // the empty union, in band
    Payload empty;
    printf( "a default Payload's tag is %d (None)\n", (int) empty.type );

    // framing
    PacketHeader header;
    header.version = 2;
    header.sequence = 1000;
    uint8_t frame[PacketHeaderMaxBytes];
    serialize::WriteStream hw( frame, sizeof( frame ) );
    WritePacketHeader( hw, header );
    hw.Flush();
    printf( "header %lld bytes, first byte 0x%02X\n",
        (long long) hw.GetBytesProcessed(), frame[0] );

    frame[0] = 0xC6;
    PacketHeader bad;
    serialize::ReadStream hr( frame, hw.GetBytesProcessed() );
    printf( "wrong magic: %s\n", ReadPacketHeader( hr, bad ) ? "ACCEPTED" : "refused" );
    return 0;
}
```

```
$ c++ -std=c++17 -Wall -Wextra -Werror -ffp-contract=off -I gen -I . -I ../serialize -o starlight main.cpp
$ ./starlight
packet 1000: 2 payloads in 20 bytes
  fire: ship 7 laser at 12 power 0.75
  chat: 7 says "on my way"
a default Payload's tag is 0 (None)
header 4 bytes, first byte 0xC7
wrong magic: refused
```

Twenty bytes for a two-message packet, and `PacketMaxBytes` is 3104, which is
what four snapshots of eight ships would cost at the worst case.

### The protocol id: same or refuse

Now the promise from Part 1 comes due. Look at the id, then add one arm to
`WeaponFire` and look again:

```
$ schema id .
0xb786cca203ebb6ea
$ # add:  railgun LaserFire  to union WeaponFire, then schema fmt .
$ schema id .
0x799890f44a60c51f
```

Take the arm back out and the id returns to `0xb786cca203ebb6ea`, because the
id is a pure function of the unit's text. `schema projection` prints the exact
text that gets hashed:

```
$ schema projection .
schema-wire-projection 2
schema-wire-law 1
package starlight
enum Pending max=0 storage=8 variants=0
enum ShipType max=3 storage=8 variants=3
  variant 1 name=Fighter
  variant 2 name=Freighter
  variant 3 name=Corvette
enum Weapon max=15 storage=8 variants=2
  variant 1 name=Laser
  variant 2 name=Missile
flags SystemFlags wirebits=4
  bit 0 name=Shields
  bit 1 name=Cloak
  bit 2 name=WarpDrive
  bit 3 name=Autopilot
```

and it continues through every type, field kind, width and range in the unit.
Nothing on the wire identifies fields, and both sides simply know the layout
because they were generated from the same schema. That is what makes the wire
this small, and it means both sides must **be** the same. The contract:

- Exchange ids in your connection handshake. `starlight::ProtocolId` is in the
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

### Which language reads a table

State this before you build on it, because it decides where you put your
tables.

**The table wire is C++'s within one build.** The other eight targets generate
a table surface, and its wire is a different form, so a table saved by C++ does
not read in them and a table saved by them does not read in C++. Issues #511 to
#518 carry the port, one per language, and each names the work.

**The packet wire, the cook and the block cross all nine.** Parts 3 through 5
are the packet wire, Part 11 is the cook and Part 12 is the block, and Part 13
runs each of the three from C against C++'s own bytes.

So: a save format, a config format and a message channel are C++'s to read
today. Data another language must read this week rides the packet wire when
both sides ship together, and the cook or the block when they share a build.

### Declaring one

`Config.schema`, a new file in the same unit:

```
package starlight

table ShipConfig
{
    display_name string(32)
    max_health   float32 = 100.0
    max_speed    float32 = 500.0
    armor        int32 = 1       | min = 0, max = 10
    ship_type    ShipType
}
```

The body grammar is the type body's: same fields, same defaults, same ranges,
and `ship_type` is the enum from Part 2, because a table may reference types
freely. What changes is the wire and the lifecycle.

### The notice

`check` is no longer silent:

```
$ schema check .
notice: starlight declares 1 table and . holds no tables.baseline — save-game evolution is unguarded (docs/SPEC-TABLES.md §18); commit one with: schema tables-baseline --update --reason "first baseline" .
$ echo $?
0
```

It is a **notice**, not an error: the exit status is 0 and nothing stops. The
tool has spotted that you now have a format that outlives builds and no record
of what shipped. Part 10 writes that record, and until then this line rides
along with every `check` and every `generate`. It is the one thing that prints
on success anywhere in the tool.

### What generation adds

```
$ schema generate --lang cpp --out gen --verbose .
...
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
// package starlight — protocol id 0xb786cca203ebb6ea (packets only: tables version by field id, not by protocol id)
// The TABLE wire (evolution-tolerant, docs/SPEC-TABLES.md): no serialize
// dependency — includable from any TU.
```

Note that banner. Adding a whole table to the unit did **not** move the
protocol id: it is still `0xb786cca203ebb6ea`, the number Part 5 printed. The
id covers what the packet wire reaches, and a table is not on it.

The storage struct lives in the table header, and it looks exactly like a
type's:

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

### Save

The encode surface is a measure and save split, and you own every buffer,
because generated table code allocates nothing. `save.cpp`, entire:

```cpp
#include "ConfigTable.h"
#include <cstdio>
#include <cstring>
#include <vector>

int main()
{
    using namespace starlight;

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

    FILE * f = fopen( "ship.bin", "wb" );
    fwrite( buffer.data(), 1, wrote, f );
    fclose( f );
    printf( "wrote ship.bin\n" );

    // a file whose first byte is a framing form this reader does not carry
    buffer[0] = 0x7F;
    FILE * g = fopen( "forged.bin", "wb" );
    fwrite( buffer.data(), 1, wrote, g );
    fclose( g );
    printf( "wrote forged.bin\n" );
    return 0;
}
```

```
$ c++ -std=c++17 -Wall -Wextra -Werror -I gen -I . -o save save.cpp gen/ConfigTable.cpp
$ ./save
measured 89, saved 89 bytes
an all-default ShipConfig measures 10 bytes
wrote ship.bin
wrote forged.bin
```

That is the only C++ program on this page that needs a `.cpp` from `gen`
alongside it: the table walk lives in `ConfigTable.cpp` so a header-only
include stays cheap.

Measure is exact, so a buffer of exactly its answer always suffices. `Save`
returns the size, or -1 for a buffer too small or a value that violates a
storage bound.

One fact explains the first number: **values at their defaults stay off the
wire.** `max_health = 250` rides, and a ship left at 100 would not.

The second number is the floor. An all-default table is not empty on this wire:
it is a form byte, the body's own terminator, and an eight-byte count of a
trailer with no entries. Ten bytes, and every one of them is framing.

`ship.bin` and `forged.bin` are used for the rest of this part and again in
Part 13, so leave them where they are.

### The point: any build reads any data

Time passes. Copy the whole unit to `starlight-2.0`, the way a branch of your
game would, and evolve it there. `Game.schema` gains a variant in the middle:

```
enum ShipType { Fighter, Interceptor, Freighter, Corvette }
```

and `Config.schema` becomes:

```
package starlight

table ShipConfig
{
    display_name string(32)
    max_health   float32 = 100.0
    armor        int32 = 1       | min = 0, max = 5
    ship_type    ShipType
    shield       float32 = 50.0
}
```

`max_speed` is gone, `shield` is new, the armor cap dropped from 10 to 5, and
`Interceptor` landed in the middle of the enum. On the packet wire any one of
those edits is a new protocol id.

`load.cpp` in `starlight-2.0`, entire:

```cpp
#include "ConfigTable.h"
#include <cstdio>
#include <cstdlib>
#include <vector>

static std::vector<uint8_t> Slurp( const char * path )
{
    FILE * f = fopen( path, "rb" );
    std::vector<uint8_t> bytes;
    uint8_t chunk[256];
    size_t n;
    while ( ( n = fread( chunk, 1, sizeof( chunk ), f ) ) > 0 )
    {
        bytes.insert( bytes.end(), chunk, chunk + n );
    }
    fclose( f );
    return bytes;
}

int main( int argc, char ** argv )
{
    using namespace starlight;

    std::vector<uint8_t> buffer = Slurp( argv[1] );
    int64_t bytes = (int64_t) buffer.size();
    if ( argc > 2 ) { bytes = atoi( argv[2] ); }

    TableReport report;
    ShipConfig loaded;
    bool ok = ShipConfigLoad( loaded, buffer.data(), bytes, &report );
    printf( "load: %s\n", ok ? "ok" : "stopped" );
    printf( "  name=%s health=%g armor=%d type=%s shield=%g\n",
        loaded.display_name, loaded.max_health, loaded.armor,
        EnumName( loaded.ship_type ), loaded.shield );
    printf( "  report: unknown=%d kind_mismatch=%d clamped=%d duplicate=%d malformed=%d refused=%d\n",
        report.unknown, report.kind_mismatch, report.clamped, report.duplicate,
        (int) report.malformed, (int) report.refused );
    return 0;
}
```

```
$ c++ -std=c++17 -Wall -Wextra -Werror -I gen -I . -o load load.cpp gen/ConfigTable.cpp
$ ./load ../starlight/ship.bin
load: ok
  name=Kestrel health=250 armor=5 type=Corvette shield=50
  report: unknown=1 kind_mismatch=0 clamped=1 duplicate=0 malformed=0 refused=0
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

It works in the other direction too. Save a `Dart` from the 2.0 unit, and read
it with the 1.0 one:

```
$ ./save2
wrote dart.bin, 58 bytes
$ cd ../starlight && ./load ../starlight-2.0/dart.bin
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

`TableReport` is five counters and a verdict:

```cpp
struct TableReport
{
    int32_t unknown = 0;       // unknown field ids skipped (newer data)
    int32_t kind_mismatch = 0; // known id, changed type — skipped, never misdecoded
    int32_t clamped = 0;       // out-of-range values clamped to declared bounds
    // a key the TEXT form saw twice: last wins, and the repeat is counted
    // (docs/SPEC-TABLES.md §16.2). The wire never raises it — a body carrying an
    // id twice is legal input whose last occurrence wins, silently (§3).
    int32_t duplicate = 0;
    bool malformed = false;    // framing damage; decode stopped, partial result kept
    // THE REFUSAL VERDICT, which is not one of §4's events and moves no counter
    // (docs/SPEC-TABLES.md §3): a FORM BYTE this reader does not carry. Five
    // zero counters and a false flag are what a clean read prints too, so the
    // verdict is what tells the two apart.
    bool refused = false;
};
```

`duplicate` belongs to the text form, which Part 9 reaches. The wire never
raises it.

You can see `kind_mismatch` by copying the unit again to `starlight-3.0` and
making `armor` a `float32 = 1.0`:

```
$ ./load3 ../starlight/ship.bin
load: ok  armor=1 (a float now)
  report: unknown=1 kind_mismatch=1 clamped=0
```

The stored int under the `armor` id is not an int as far as this reader is
concerned, so it is skipped and the field keeps its declared default. Never
misdecoded.

`malformed` is the counter that stops a load. The 2.0 `load` takes an optional
byte count, so you can cut the file anywhere:

```
$ ./load ../starlight/ship.bin 17
load: stopped
  name= health=100 armor=1 type=None shield=50
  report: unknown=0 kind_mismatch=0 clamped=0 duplicate=0 malformed=1 refused=0
$ ./load ../starlight/ship.bin 40
load: stopped
  name= health=100 armor=1 type=None shield=50
  report: unknown=0 kind_mismatch=0 clamped=0 duplicate=0 malformed=1 refused=0
$ ./load ../starlight/ship.bin 88
load: stopped
  name= health=100 armor=1 type=None shield=50
  report: unknown=0 kind_mismatch=0 clamped=0 duplicate=0 malformed=1 refused=0
```

Every field sits at its declared default, at every cut. That is the shape of
the wire showing through: the file's **id table is its trailer**, and the
reader locates it from the end, so a cut file has no vocabulary to name its own
fields with. There is no useful prefix to keep, one byte short of the whole
file or seventy.

`refused` is not a counter and is not damage. The wire opens with a **form
byte**, read before anything else, and a reader that meets a form it does not
carry stops without decoding a thing. That is what `forged.bin` is: `ship.bin`
with its first byte replaced.

```
$ ./load ../starlight/forged.bin
load: stopped
  name= health=100 armor=1 type=None shield=50
  report: unknown=0 kind_mismatch=0 clamped=0 duplicate=0 malformed=0 refused=1
```

Five zero counters and `malformed=0`, which is exactly what a clean read prints
too. The verdict is what tells the two apart, so read `refused` before you read
the counters. A file written by a build whose framing is newer than yours says
so rather than pretending to be broken.

Only structural damage and an unknown form stop a load. Every schema-drift
event above is a counter rather than a failure.

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
flags SystemFlags { Shields, Cloak, WarpDrive, Autopilot }             // as declared
flags SystemFlags { Shields, Stealth, Cloak, WarpDrive, Autopilot }    // wrong: stored Cloak reads Stealth
flags SystemFlags { Shields, Cloak, WarpDrive, Autopilot, Stealth }    // right: appended, nothing moves
```

Enums insert anywhere because they are name hashed. Flags append at the end
because they are positional. Two declarations, two laws. Removing a flags
variant frees no bit either, so retire the name and keep the position. Part 10's
baseline turns this law into a compile error.

### Where tables stand next to types

Edit a table, add fields, grow enums, reshape at will, and your protocol id
**does not move**, as the banner above showed. Tables never touch it, and
packets never pay for tolerance. The costs live where the tolerance lives: a
table's wire is byte granular rather than bit packed, 89 bytes here against
Part 3's three, and the read walks and matches ids instead of streaming bits. A
fixed-size table like this one still allocates nothing and stays a plain
struct. What "fixed size" means, and what the other class costs, is Part 8's
story.

Two structural rules stand: a `type` cannot reference a table, because packets
stay exact match while a table may reference types freely, and a table cannot
nest itself by value.

**You now have** a save format. Write it with any build of Starlight, read it
with any other, and every difference between them is absorbed, counted and
reported.
