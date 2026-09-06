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
// package starlight — protocol id 0xff86e6e5b9a15fa5

#pragma once

#include <cstdint>

namespace starlight {

// The unit's protocol id — the hash of its wire shape (SPEC §3.1). Two
// sides at the same id speak identical bits; there is no other versioning.
inline constexpr uint64_t ProtocolId = 0xff86e6e5b9a15fa5ull;

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
0xff86e6e5b9a15fa5
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

The **count** bound is the stated exception to that rule, and it is worth
seeing why. `window [2..8]uint32` compiles:

```
$ schema check .
$ schema generate --lang cpp --out g .
```

```cpp
struct T {
    uint32_t window[8] = {}; // used count beside it; wire count in [2, 8]
    int32_t window_count = 2;
};
```

```cpp
SCHEMA_WRITE_INLINE bool WriteT( serialize::WriteStream & stream, const T & value )
{
    if ( int32_t( value.window_count ) < int32_t( 2 ) || int32_t( value.window_count ) > int32_t( 8 ) )
    {
        return false; // a count outside its wire range is refused in every build (SPEC §4.6)
    }
    write_bits( stream, uint32_t( value.window_count ) - uint32_t( 2 ), 3 );
```

Two things are going on there, and they are the same rule from both ends.

A freshly constructed `T` has `window_count = 2`, the declared minimum. Zero is
outside the count's own wire range, so a fresh `T` would otherwise be born
carrying a count no reader accepts, and an array takes no `= value` default to
name a birth value another way. The bound names it instead. This is the one
place in the language where storage starts at something other than zero without
a declaration saying so, and `[..8]` is born at 0 like everything else.

The write refuses an out-of-range count in **every build**, release included.
It is not an assert and there is no build flag that removes it. The reason is
the line under it: the pack subtracts the low bound, so `window_count = 0`
would wrap `0 - 2` to a large unsigned value, and the loop below writes
`window_count` elements. An unchecked write would report success on bytes no
reader takes. Every one of the nine targets refuses it, each in its own
convention, among them `false` here, `0` in C, `-1` in Dart and Java,
`ErrValueOutOfRange` in Go, an `ArgumentError` in Elixir.

Reach for `[A..B]` with A above zero wherever the count genuinely has a floor.
The tool holds both ends of it for you.

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
    Count = 2, // the declared variant count (SPEC §4.2)
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

The tag enum is a full enum. It carries `None`, the variants, `Count` and
`Max`, and the debug-name function beside them, so `EnumName( first.fire.fire.type )`
compiles and prints `"Laser"`. It is the same call a declared enum takes, found
by lookup the same way. Log a tag with it. The switch the program below writes is
there to reach the payload, which a name cannot do. Because the enum carries
`Count`, `count` is a refused arm name on a packet union, alongside `none` and
`max`.

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
0x60d7cbad6bb296f2
$ # add:  railgun LaserFire  to union WeaponFire, then schema fmt .
$ schema id .
0x31c8d2175ac80ef7
```

Take the arm back out and the id returns to `0x60d7cbad6bb296f2`, because the
id is a pure function of the unit's text. `schema projection` prints the exact
text that gets hashed:

```
$ schema projection .
schema-wire-projection 3
schema-wire-law 1
package starlight
enum ShipType max=3 storage=8 variants=3
  variant 1 name=Fighter
  variant 2 name=Freighter
  variant 3 name=Corvette
flags SystemFlags wirebits=4
  bit 0 name=Shields
  bit 1 name=Cloak
  bit 2 name=WarpDrive
  bit 3 name=Autopilot
```

and it continues through every type, field kind, width and range in the unit.

`Pending` and `Weapon` are declared in this unit and are not in that text,
because **the projection lists what a `type` REACHES**. Every `type` is a root,
since any of them may be handed to a writer, and the walk goes from there
through field types, array elements, array bounds, keyed-array keys, constants,
union arms and both sides of every branch. Nothing in Starlight's types names
`Pending` or `Weapon`, so neither puts a byte on a packet and neither moves the
id: add a variant to `Weapon` today and every deployed client still connects.
Give `ShipState` a `weapon Weapon` field and it joins the text, with the id move
that field was going to cost anyway. `flags` is the one exception and rides
whether a type reaches it or not, which is why `SystemFlags` is there: a flags
bit is a position, and Part 6's table wire has no way to report one moving.
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
along with every `check`. It is the one thing `check` prints on success.

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
// package starlight — protocol id 0x60d7cbad6bb296f2 (packets only: tables version by field id, not by protocol id)
// The TABLE wire (evolution-tolerant, docs/SPEC-TABLES.md): no serialize
// dependency — includable from any TU.
```

Note that banner. Adding a whole table to the unit did **not** move the
protocol id: it is still `0x60d7cbad6bb296f2`, the number Part 5 printed. The
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

It works in the other direction too. `save2.cpp`, in `starlight-2.0`:

```cpp
#include "ConfigTable.h"
#include <cstdio>
#include <cstring>
#include <vector>

int main()
{
    using namespace starlight;
    ShipConfig ship;
    memcpy( ship.display_name, "Dart", 4 );
    ship.display_name_length = 4;
    ship.ship_type = ShipType::Interceptor;   // the variant 1.0 has never heard of
    ship.shield = 75.0f;                      // the field 1.0 does not have
    int64_t size = ShipConfigMeasure( ship );
    std::vector<uint8_t> buffer( size );
    ShipConfigSave( ship, buffer.data(), size );
    FILE * f = fopen( "dart.bin", "wb" );
    fwrite( buffer.data(), 1, size, f );
    fclose( f );
    printf( "wrote dart.bin, %lld bytes\n", (long long) size );
    return 0;
}
```

Save a `Dart` from the 2.0 unit and read it with the 1.0 one, whose `load.cpp`
is the same program with `max_speed` in place of `shield`:

```
$ c++ -std=c++17 -Wall -Wextra -Werror -I gen -I . -o save2 save2.cpp gen/ConfigTable.cpp
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

`TableReport` is six counters and a verdict:

```cpp
struct TableReport
{
    int32_t unknown = 0;       // unknown field ids skipped (newer data)
    int32_t kind_mismatch = 0; // known id, changed type — skipped, never misdecoded
    int32_t widened = 0;       // a kind that grew since the writer, decoded exactly
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
making `armor` a `float32 = 1.0`. `load3.cpp` there is `load.cpp` with one
printf, because `armor` is a different C++ type now:

```cpp
    printf( "load: %s  armor=%g (a float now)\n", ok ? "ok" : "stopped", loaded.armor );
    printf( "  report: unknown=%d kind_mismatch=%d clamped=%d\n",
        report.unknown, report.kind_mismatch, report.clamped );
```

```
$ c++ -std=c++17 -Wall -Wextra -Werror -I gen -I . -o load3 load3.cpp gen/ConfigTable.cpp
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

---

## Part 7: Shaping real data: optionals, keyed arrays, and a second unit

### The problem

One `ShipConfig` is not a game. Starlight's designers want one file that holds
everything: global switches, a weapon list, and a per-ship-type tuning block.
Parts of it are genuinely optional, because gunner settings exist only for
ships that have a gunner seat.

One constant joins `Game.schema`, `const MaxWeapons = 8`, and `Config.schema`
becomes:

```
package starlight

table GunnerSettings
{
    sensitivity float32 = 1.0
}

table ShipConfig
{
    display_name string(32)
    max_health   float32 = 100.0
    max_speed    float32 = 500.0
    armor        int32 = 1       | min = 0, max = 10
    ship_type    ShipType
    settings     ?GunnerSettings
    tier         ?int32
}

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

A `?` before the type makes the field **present or absent**. Storage is the
value plus a generated `_present` bool, so the table stays a fixed-size struct
with no pointer and no allocation:

```cpp
struct ShipConfig {
    char display_name[32 + 1] = {}; // string(32): max length, used length beside it
    int32_t display_name_length = 0;
    float max_health = 100.0f;
    float max_speed = 500.0f;
    int32_t armor = 1;
    ShipType ship_type = ShipType::None;
    GunnerSettings settings;
    bool settings_present = false; // ?GunnerSettings: absent until set
    int32_t tier = 0;
    bool tier_present = false; // ?int32: absent until set
};
```

**Presence decides whether it rides, not content**, which the program below
shows: an all-default `GunnerSettings` still costs bytes once it is present.
That is the difference between `?T` and a plain nested `T`. A plain nesting at
all defaults elides entirely, so you cannot tell "not there" from "there, all
defaults". An optional keeps "absent" and "present with nothing to say" as two
distinct values. A reader that meets the field sets `_present` whatever the
content, and an absent one leaves it false.

`?` applies to a nested table, a nested type, an enum, a flags mask, any
scalar, and a bounded array of those. Where it is refused, the refusal explains
the logic. Each of these is its own `bad/Bad.schema`:

```
package bad

table A { x int32 }
table B { y int32 }

union U
{
    a A
    b B
}

table T { u ?U }
```

```
$ schema check .
Bad.schema:12:14: field u: ?U marks a union optional, and a union is ALREADY optional — its None arm IS the absence, and an empty union elides exactly as an absent optional does; drop the ? (docs/SPEC-TABLES.md §2.3)
Bad.schema:6:1: union U: the arm a A is not a declared type, and no table reaches U — such an arm is a table-closure construct, and a union outside one takes `type` payloads only; hold the union in a table body, or make the arm a type (docs/SPEC-TABLES.md §2.6, §11, §15)
schema: 2 error(s)
```

Read the first line. The second is the reachability walk losing sight of `U`
because the `?` made the field unusable, and it prescribes a fix the schema
already has.

```
package bad

table N { v int32 }
table T { a ?*N }
```

```
$ schema check .
Bad.schema:4:14: field a: ?*N marks a pointer optional, and a pointer is ALREADY optional — null is its absence, and it rides exactly as an absent optional does; drop the ? (docs/SPEC-TABLES.md §2.3)
schema: 1 error(s)
```

```
package bad

table T { a ?string(8) }
```

```
$ schema check .
Bad.schema:3:14: field a: ? on string(N) is a named follow-on — the generated length companion already carries emptiness, and a second presence bit beside it would be two answers to one question; wrap it in a table and make that optional (docs/SPEC-TABLES.md §15)
schema: 1 error(s)
```

```
package bad

table T { a ?int32 = 3 }
```

```
$ schema check .
Bad.schema:3:11: field a: an optional field takes no specified default — PRESENCE is the only default an optional has, and an absent optional reads as absent with its value at the type's own zero (docs/SPEC-TABLES.md §2.3)
schema: 1 error(s)
```

### `[ShipType]ShipConfig`: one slot per named variant

The tuning block wants exactly one `ShipConfig` per ship type. You could write
`[ShipType.Max]ShipConfig` and index by `int( type ) - 1`, and the day someone
inserts a variant mid-enum every stored file shifts its slots by one, silently.
The keyed spelling exists so that cannot happen:

```cpp
struct GameConfig {
    bool friendly_fire = false;
    WeaponConfig weapons[8]; // used count beside it; count in [0, 8]
    int32_t weapons_count = 0;
    TableKeyed<ShipConfig, ShipType> ships; // [ShipType]: one slot per named variant, keyed by the value
    char level_name[64 + 1] = {}; // string(64): max length, used length beside it
    int32_t level_name_length = 0;
};
```

There is no count companion, because every named slot exists, and no slot for
`None`, because `None` is the null and key `k` lives at index `k - 1`.
Iteration yields the **key**, never a storage index, so consuming the whole
array involves no `- 1`, no cast, and no bound of your own. Write
`auto [ k, v ]` and not `auto & [ k, v ]`, because the entry is a proxy handed
out by value and the compiler refuses the reference spelling by design. The
element inside it is a real reference either way.

### A program

`config.cpp`, entire:

```cpp
#include "ConfigTable.h"
#include <cstdio>
#include <cstring>
#include <vector>

int main()
{
    using namespace starlight;

    GameConfig config;
    config.friendly_fire = true;
    memcpy( config.level_name, "orbital", 7 );
    config.level_name_length = 7;
    config.weapons_count = 2;
    config.weapons[0].damage = 35.0f;
    config.weapons[1].homing = true;

    ShipConfig & fighter = config.ships[ShipType::Fighter];
    printf( "fighter measures %lld with settings absent\n",
        (long long) ShipConfigMeasure( fighter ) );
    fighter.settings_present = true;
    printf( "fighter measures %lld with settings present and all-default\n",
        (long long) ShipConfigMeasure( fighter ) );

    config.ships[ShipType::Corvette].max_health = 400.0f;

    for ( auto [ ship_type, ship ] : config.ships )
    {
        printf( "slot %-10s health=%g\n", EnumName( ship_type ), ship.max_health );
    }

    int64_t size = GameConfigMeasure( config );
    std::vector<uint8_t> buffer( size );
    GameConfigSave( config, buffer.data(), size );
    printf( "GameConfig is %lld bytes\n", (long long) size );

    FILE * f = fopen( "config.bin", "wb" );
    fwrite( buffer.data(), 1, size, f );
    fclose( f );
    printf( "wrote config.bin\n" );
    return 0;
}
```

```
$ c++ -std=c++17 -Wall -Wextra -Werror -I gen -I . -o config config.cpp gen/ConfigTable.cpp
$ ./config
fighter measures 10 with settings absent
fighter measures 22 with settings present and all-default
slot Fighter    health=100
slot Freighter  health=100
slot Corvette   health=400
GameConfig is 142 bytes
wrote config.bin
```

`config.bin` is used again in Parts 9 and 11, so leave it there.

**On the wire, slots ride by variant name**, like every enum value in Part 6.
The `starlight-2.0` copy from Part 6 already has `Interceptor` inserted in the
middle. Give it this `Config.schema` too, and read the 1.0 file with
`keyed.cpp`, entire:

```cpp
#include "ConfigTable.h"
#include <cstdio>
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

int main( int, char ** argv )
{
    using namespace starlight;
    std::vector<uint8_t> buffer = Slurp( argv[1] );
    TableReport report;
    GameConfig config;
    GameConfigLoad( config, buffer.data(), (int64_t) buffer.size(), &report );
    for ( auto [ ship_type, ship ] : config.ships )
    {
        printf( "slot %-12s health=%g\n", EnumName( ship_type ), ship.max_health );
    }
    printf( "report: unknown=%d\n", report.unknown );
    return 0;
}
```

```
$ c++ -std=c++17 -Wall -Wextra -Werror -I gen -I . -o keyed keyed.cpp gen/ConfigTable.cpp
$ ./keyed ../starlight/config.bin
slot Fighter      health=100
slot Interceptor  health=100
slot Freighter    health=100
slot Corvette     health=400
report: unknown=0
```
Corvette's. A slot the writer left at its default is elided, a slot this reader
has no name for is skipped and counted `unknown`, and a `None` key never rides
at all.

### Indexing by `None` ends the program, in every build

Keys in data-driven code are runtime values: an enum read out of a file, a key
a tool hands you. `none.cpp`, entire:

```cpp
#include "ConfigTable.h"
#include <cstdio>

int main()
{
    using namespace starlight;
    GameConfig config;
    ShipType key = ShipType::None;      // a key read out of a file, say
    printf( "about to index by None...\n" );
    fflush( stdout );
    config.ships[key].max_health = 1.0f;
    printf( "still here\n" );
    return 0;
}
```

```
$ c++ -std=c++17 -O2 -DNDEBUG -I gen -I . -o none none.cpp gen/ConfigTable.cpp
$ ./none
about to index by None...
$ echo $?
134
```

This is not a debug assert, and `NDEBUG` does not remove it, because there is
no configuration in which a `None` key silently reading one element **before**
the array is acceptable. The cost is one predictable compare, and iteration,
which hands over no key, never pays even that.

One caution for later. In a `type` body the same spelling `[ShipType]int32` is
legal but positional: a plain array, on the packet wire, with no accessor and
no guard. On the table wire, changing a field between the keyed and positional
spellings is a wire break the report calls `kind_mismatch`, because the two are
different encodings and not a refactor. Part 10's baseline refuses that edit
outright.

### A second unit: the C++-only forms

Part 6's rule said the table wire is C++'s. Some table **declarations** are
C++'s too, and the compiler refuses them by name on the other eight targets.
Rather than lose the whole `starlight` unit's eight backends, put those
declarations in a unit of their own. That is what the refusal tells you to do:

```
$ schema generate --lang go --out gen .
schema: unit declares a union a TABLE CLOSURE holds (ToolBody) — the table side of a union, whose arms are field lines of any type, is cpp only today, and the go form is a named follow-on; generate with --lang cpp, or move the union and its tables to their own unit (docs/SPEC-TABLES.md §2.6, §11)
```

`starlight/tools/Tools.schema`, its own directory and its own package:

```
package starlighttools

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
    ping uint32
    close
}

table ToolMessage
{
    sequence uint32
    body     ToolBody
}

table ToolLog
{
    events [..8]ToolBody
}

table Attachment
{
    label *string
    data  *bytes
}

table AttachmentSet
{
    items [..4]Attachment
}
```

Four C++-only forms live in there, and Part 8's maps make a fifth.

**A union a table closure holds.** In a `type` body every arm names a declared
type, which is what Part 5's refusal said. In a table closure an arm is a field
line of any type, so `ToolBody` has a table arm, a **scalar** arm, and an arm
with **no payload at all**:

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

`Close` is a value of the tag enum and no member: it selects and carries
nothing, which is what a one-of wants for a case that is a fact rather than a
value.

**An array of unions in a table closure**, which is what `ToolLog` is: a
heterogeneous event log as one bounded array of one union.

**`*string` and `*bytes`**, buffers at their used size rather than at a
declared bound:

```cpp
struct Attachment {
    TableRef label; // *string — a byte buffer at its used size, null until assigned
    TableRef data; // *bytes — a byte buffer at its used size, null until assigned
};
```

A reference is not a value, so a table that holds one is built rather than
filled, which is Part 8's builder arriving early. `tools.cpp`, entire:

```cpp
#include "ToolsTable.h"
#include <cstdio>
#include <cstring>

using namespace starlighttools;

int main()
{
    // an arm that is a table
    ToolMessage open;
    open.sequence = 3;
    open.body.type = ToolBodyType::Open;
    open.body.open = OpenDocument{};
    memcpy( open.body.open.path, "ship.cfg", 8 );
    open.body.open.path_length = 8;
    open.body.open.line = 42;
    printf( "open  measures %lld bytes\n", (long long) ToolMessageMeasure( open ) );

    // an arm that is a bare scalar
    ToolMessage ping;
    ping.sequence = 4;
    ping.body.type = ToolBodyType::Ping;
    ping.body.ping = 99;
    printf( "ping  measures %lld bytes\n", (long long) ToolMessageMeasure( ping ) );

    // an arm with no payload at all: a value of the tag enum and no member
    ToolMessage close;
    close.sequence = 5;
    close.body.type = ToolBodyType::Close;
    printf( "close measures %lld bytes\n", (long long) ToolMessageMeasure( close ) );

    // an array of unions
    ToolLog log;
    log.events_count = 2;
    log.events[0].type = ToolBodyType::Ping;
    log.events[0].ping = 7;
    log.events[1].type = ToolBodyType::Close;
    printf( "a log of %d events measures %lld bytes\n",
        log.events_count, (long long) ToolLogMeasure( log ) );

    // *string and *bytes: buffers at their used size, so their holder is built
    AttachmentSetBuilder builder;
    AttachmentSet * set = builder.GetRoot();
    set->items_count = 1;
    TableStringSlot label = builder.AllocString( 5 );
    memcpy( label.data, "notes", 5 );
    set->items[0].label = label;
    TableBytesSlot data = builder.AllocBytes( 3 );
    data.data[0] = 1;
    data.data[1] = 2;
    data.data[2] = 3;
    set->items[0].data = data;

    builder.Lock();
    const AttachmentSet * locked = builder.AsConst();
    TableStringView name = TableStringAt( locked->items[0].label );
    TableBytesView blob = TableBytesAt( locked->items[0].data );
    printf( "attachment \"%.*s\" carries %lld bytes, the set measures %lld\n",
        (int) name.length, name.data, (long long) blob.length,
        (long long) AttachmentSetMeasure( locked ) );
    return 0;
}
```

```
$ cd tools && schema generate --lang cpp --out gen .
$ c++ -std=c++17 -Wall -Wextra -Werror -I gen -o tools tools.cpp gen/ToolsTable.cpp
$ ./tools
open  measures 79 bytes
ping  measures 49 bytes
close measures 45 bytes
a log of 2 events measures 49 bytes
attachment "notes" carries 3 bytes, the set measures 87
```

On the table wire a union's arm rides under its **arm-name hash** with a length
prefix, so adding a message is adding an arm. A reader that lacks the arm reads
the union empty as `None`, skips the body by its length and counts `unknown`,
and arms may be removed and reordered freely. Everything Part 6 taught about
fields applies to messages.

The `starlight` unit itself still generates for all nine:

```
$ cd .. && for l in c cpp cs dart elixir go java js rust; do printf "%-8s " $l; schema generate --lang $l --out /tmp/g-$l . && echo ok; done
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

**You now have** `GameConfig`, one declared root and one binary format, with
optional blocks and per-ship-type tuning that survives enum surgery, plus a
second unit where the C++-only forms live without costing the first unit its
eight backends.

---

## Part 8: Pointers and maps: when data is a graph

### The problem

Starlight's patrol routes are linked chains of waypoints, and a waypoint knows
its next. A save-file scene wants one palette shared by fifty props, not fifty
copies. Value semantics cannot say either of those things, because types and
the tables so far nest by value, and a value cannot contain itself or be in two
places at once.

### `*Table`: pointer fields

`Scene.schema`, a new file in the `starlight` unit:

```
package starlight

table Waypoint
{
    name string(24)
    x    float32
    y    float32
    next *Waypoint
}

table Route
{
    head  *Waypoint
    stops [..4]*Waypoint
    loops bool
}
```

Pointers are a **table** construct, with rules the compiler walks you through
if you cross them. Each is its own `bad/Bad.schema`:

```
package bad

table V { x int32 }

type T { p *V }
```

```
$ schema check .
Bad.schema:5:12: field p: *V is a pointer, and pointers are a TABLE construct — types remain value semantics, tables allow pointer semantics; nest the field by value, or move the declaring type to a `table` (docs/SPEC-TABLES.md)
schema: 1 error(s)
```

```
package bad

type V { x int32 }

table T { p *V }
```

```
$ schema check .
Bad.schema:5:13: field p: *V points at a type, and a pointer may only target a `table` — V is value-semantics data with no independent identity to point at; nest it by value, or declare V as a table (docs/SPEC-TABLES.md)
schema: 1 error(s)
```

```
package bad

table V { x int32 }

table T { p *V = 0 }
```

```
$ schema check .
Bad.schema:5:11: field p: a pointer field takes no specified default — a fresh pointer is null, and null is the only value a default could name (docs/SPEC-TABLES.md)
schema: 1 error(s)
```

One rule you do not declare: **the compiler derives the mode.** A table with no
pointer anywhere in its by-value closure is fixed size, which is everything
Parts 6 and 7 did, a plain struct with zero overhead from this part. One
pointer anywhere in that closure makes the table variable length, and it is
never held by value again. You build it, lock it, and read it through a root
pointer. `Route` and `Waypoint` are variable, and `ShipConfig` and `GameConfig`
are still fixed, in the same unit.

Resist reaching for `*T` to mean "optional". That is `?T`, with no allocation
and still fixed size. A pointer is for structure: recursion, sharing, a subtree
you would rather not carry by value.

Pointers cost the `starlight` unit none of its backends:

```
$ for l in c cpp cs dart elixir go java js rust; do printf "%-8s " $l; schema generate --lang $l --out /tmp/g-$l . && echo ok; done
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

### The builder life

`route.cpp`, entire:

```cpp
#include "SceneTable.h"
#include <cstdio>
#include <cstring>
#include <vector>

using namespace starlight;

static void SetName( Waypoint * w, const char * text )
{
    int32_t n = (int32_t) strlen( text );
    memcpy( w->name, text, n );
    w->name_length = n;
}

int main()
{
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

    route->stops_count = 3;
    route->stops[0] = a;
    route->stops[1] = a;                         // the same node, a second reference
                                                 // stops[2] left null, and a null slot rides
    builder.Lock();                              // ONE WAY, and it compacts
    const Route * locked = builder.AsConst();    // the CONST life: one packed region

    for ( const Waypoint * w = WaypointAt( locked->head ); w != NULL; w = WaypointAt( w->next ) )
    {
        printf( "  %.*s (%g, %g)\n", w->name_length, w->name, w->x, w->y );
    }

    int64_t size = RouteMeasure( locked );
    std::vector<uint8_t> wire( size );
    RouteSave( locked, wire.data(), size );

    int64_t need = RouteLoadMeasure( wire.data(), size );  // exact, reads no values
    std::vector<uint8_t> region( need );                   // YOUR allocation, as always
    TableReport report;
    const Route * loaded = RouteLoad( region.data(), need, wire.data(), size, &report );

    const Waypoint * s0 = WaypointAt( loaded->stops[0] );
    const Waypoint * s1 = WaypointAt( loaded->stops[1] );
    const Waypoint * s2 = WaypointAt( loaded->stops[2] );
    printf( "wire %lld bytes, region %lld bytes\n", (long long) size, (long long) need );
    printf( "after the round trip: shared=%s null=%s, head is %.*s\n",
        s0 == s1 ? "YES" : "no", s2 == NULL ? "yes" : "no",
        WaypointAt( loaded->head )->name_length, WaypointAt( loaded->head )->name );
    printf( "report: unknown=%d malformed=%d refused=%d\n",
        report.unknown, (int) report.malformed, (int) report.refused );
    printf( "sizeof( RouteBuilder ) = %lld\n", (long long) sizeof( RouteBuilder ) );
    return 0;
}
```

```
$ c++ -std=c++17 -Wall -Wextra -Werror -I gen -I . -o route route.cpp gen/SceneTable.cpp
$ ./route
  Gate (0, 0)
  Belt (120, 40)
  Station (300, -75)
wire 163 bytes, region 256 bytes
after the round trip: shared=YES null=yes, head is Gate
report: unknown=0 malformed=0 refused=0
sizeof( RouteBuilder ) = 8264
```

`Alloc` hands back a slot usable both as the node pointer, so you write its
fields through it, and as the reference, so you assign it to a pointer field.
`Lock` walks the graph from the root, measures exactly, and lays every
reachable node back to back into one region with the root at its base, with
references rewritten self-relative so the whole region relocates by plain
`memcpy`. There is no unlock. To edit again, load the region into a fresh
builder.

Reading the locked form is one add per hop, and `NULL` for null. Before `Lock`,
a reference resolves through the arena instead, with the same `WaypointAt`
taking the builder's arena as its first argument. One slot, two encodings, and
the overload set keeps you honest.

On the wire a pointer rides as an index into a flat node table, which buys the
property graphs actually need: **identity**. `stops[0]` and `stops[1]` are the
same node before the round trip and the same node after it, and the null slot
is still null. Nodes the root cannot reach do not ride.

### What the loader does and does not promise

Nothing you **build** can express a cycle: you cannot assign a slot you have
not allocated. Bytes that arrive from somewhere else are another matter, and it
is worth knowing exactly where the line is before you walk a graph you did not
write.

Take the two-node route, flip one bit, and try every bit in the file.
`damage.cpp`, entire:

```cpp
#include "SceneTable.h"
#include <cstdio>
#include <cstring>
#include <vector>

using namespace starlight;

int main()
{
    // build a two-waypoint route and save it
    RouteBuilder builder;
    Route * route = builder.GetRoot();
    auto a = builder.Alloc<Waypoint>();
    auto b = builder.Alloc<Waypoint>();
    memcpy( a->name, "Gate", 4 ); a->name_length = 4;
    memcpy( b->name, "Belt", 4 ); b->name_length = 4;
    route->head = a;
    a->next = b;
    builder.Lock();

    int64_t size = RouteMeasure( builder.AsConst() );
    std::vector<uint8_t> clean( size );
    RouteSave( builder.AsConst(), clean.data(), size );

    int64_t stopped = 0, opened = 0, cycles = 0, longest = 0;
    for ( int64_t i = 0; i < size; i++ )
    {
        for ( int bit = 0; bit < 8; bit++ )
        {
            std::vector<uint8_t> wire = clean;
            wire[i] ^= (uint8_t) ( 1 << bit );

            int64_t need = RouteLoadMeasure( wire.data(), size );
            if ( need <= 0 ) { stopped++; continue; }
            std::vector<uint8_t> region( need );
            TableReport report;
            const Route * loaded = RouteLoad( region.data(), need, wire.data(), size, &report );
            if ( loaded == NULL || report.malformed || report.refused ) { stopped++; continue; }

            opened++;
            int64_t hops = 0;
            bool terminated = true;
            for ( const Waypoint * w = WaypointAt( loaded->head ); w != NULL; w = WaypointAt( w->next ) )
            {
                if ( ++hops > 1000 ) { terminated = false; break; }
            }
            if ( !terminated ) { cycles++; continue; }
            if ( hops > longest ) { longest = hops; }
        }
    }
    printf( "%lld single-bit edits to an %lld-byte wire\n", (long long) ( size * 8 ), (long long) size );
    printf( "  %lld stopped at the door\n", (long long) stopped );
    printf( "  %lld opened as a clean read\n", (long long) opened );
    printf( "  of those, %lld produced a chain that never ends\n", (long long) cycles );
    printf( "  longest finite chain: %lld hops (the clean wire's is 2)\n", (long long) longest );
    return 0;
}
```

```
$ c++ -std=c++17 -Wall -Wextra -Werror -I gen -I . -o damage damage.cpp gen/SceneTable.cpp
$ ./damage
640 single-bit edits to an 80-byte wire
  295 stopped at the door
  345 opened as a clean read
  of those, 1 produced a chain that never ends
  longest finite chain: 2 hops (the clean wire's is 2)
```

Read all four numbers.

**295 stopped at the door.** Framing damage is caught, and `Load` says so.

**345 opened as a clean read**, which is right: most bits in a save file are
payload, and a wrong `x` is a wrong number and not damage. No read went out of
bounds and nothing crashed across all 640.

**One of them cycles.** A single flipped bit produced a `next` pointing at its
own node, with an all-zero report: `malformed=0`, `refused=0`, `unknown=0`. So
the guarantee to rely on is memory safety, not acyclicity. When you walk a
graph whose bytes you did not write, **bound the walk** the way `damage.cpp`
does, or count nodes against `LoadMeasure`'s answer. This is filed as
schema#521 G-19.

### Building goes wide

Lock-free parallel construction is designed in rather than bolted on. One
worker per thread, each allocating on its own front, with no lock and no
per-node atomic. `wide.cpp`, entire:

```cpp
#include "SceneTable.h"
#include <cstdio>
#include <thread>

using namespace starlight;

int main()
{
    RouteBuilder builder;
    Route * route = builder.GetRoot();

    TableSlot<Waypoint> heads[4];                    // one chain head per thread
    std::thread workers[4];
    for ( int t = 0; t < 4; t++ )
    {
        workers[t] = std::thread( [&builder, &heads, t] {
            TableWorker worker = builder.Worker();   // one per THREAD
            TableSlot<Waypoint> head = worker.Alloc<Waypoint>();
            Waypoint * previous = head;
            for ( int i = 1; i < 1000; i++ )
            {
                TableSlot<Waypoint> next = worker.Alloc<Waypoint>();
                next->x = (float) i;
                previous->next = next;
                previous = next;
            }
            heads[t] = head;
        } );
    }
    for ( auto & w : workers ) { w.join(); }         // join, THEN lock

    route->stops_count = 4;
    for ( int t = 0; t < 4; t++ ) { route->stops[t] = heads[t]; }
    builder.Lock();
    const Route * locked = builder.AsConst();

    int64_t walked = 0;
    for ( int t = 0; t < 4; t++ )
    {
        for ( const Waypoint * w = WaypointAt( locked->stops[t] ); w != NULL; w = WaypointAt( w->next ) )
        {
            walked++;
        }
    }
    printf( "walked %lld waypoints; the wire measures %lld bytes\n",
        (long long) walked, (long long) RouteMeasure( locked ) );
    return 0;
}
```

```
$ c++ -std=c++17 -Wall -Wextra -Werror -I gen -I . -o wide wide.cpp gen/SceneTable.cpp
$ ./wide
walked 4000 waypoints; the wire measures 51904 bytes
```

Allocating on your own worker is safe concurrently. Writing fields of a node
another worker allocated is your synchronization problem, and `Lock` and `Save`
are single-threaded. A builder is about 8 KB, because it carries the arena's
segment table inline: fine on the stack, fine as a member, and not something
for an array of thousands.

Note the shape of that number against Part 6's ten-byte floor. A pointer-heavy
graph is where this wire is cheapest, because four thousand nodes name the same
handful of field ids and the file carries each id once, in its trailer. A tiny
message pays the framing instead.

### Maps

A pointer is one node reached by one edge. A map is many values reached by a
key, and it is the same machinery. A map is one of Part 7's C++-only forms, so
it goes in the second unit, `starlight/tools/Fleet.schema`:

```
package starlighttools

table ShipTuning
{
    max_health float32 = 100.0
}

table Fleet
{
    ships map[string(32)]ShipTuning
    tiers map[int16]int32
}
```

```cpp
struct Fleet {
    TableMap<FleetShipsEntry> ships; // map[string(32)]ShipTuning — the sorted entry array, empty until an insert
    TableMap<FleetTiersEntry> tiers; // map[int16]int32 — the sorted entry array, empty until an insert
};
```

The key is a bounded string or an integer, and the value is anything a field
can be, including another map. A map makes its holder variable length, so it
lives in a builder too. `fleet.cpp`, entire:

```cpp
#include "FleetTable.h"
#include <cstdio>
#include <vector>

using namespace starlighttools;

int main()
{
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
    for ( auto [ tier, value ] : locked->tiers )
    {
        printf( "  tier %d -> %d\n", tier, *value );
    }

    const ShipTuning * found = locked->ships.Find( "dart" );
    printf( "Find(\"dart\") health=%g, Find(\"ghost\") is %s\n",
        found->max_health, locked->ships.Find( "ghost" ) == NULL ? "null" : "not null" );

    int64_t size = FleetMeasure( locked );
    std::vector<uint8_t> wire( size );
    FleetSave( locked, wire.data(), size );
    printf( "%d ships, %d tiers, %lld bytes on the wire\n",
        locked->ships.size(), locked->tiers.size(), (long long) size );
    return 0;
}
```

```
$ cd tools && schema generate --lang cpp --out gen .
$ c++ -std=c++17 -Wall -Wextra -Werror -I gen -o fleet fleet.cpp gen/FleetTable.cpp
$ ./fleet
  anvil    health=900
  dart     health=80
  kestrel  health=250
  tier -3 -> 7
  tier 2 -> 9
Find("dart") health=80, Find("ghost") is null
3 ships, 2 tiers, 145 bytes on the wire
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

**You now have** patrol routes as real linked structure in the `starlight` unit
and fleets keyed by name in the tools unit: built in parallel, locked into one
relocatable region, saved on the same tolerant wire as everything else, with
sharing preserved and a clear line around what the loader promises.

---

## Part 9: The text form: designers edit JSON, the game loads bytes

### The problem

`GameConfig` is a binary format now, and binary is exactly what a designer
cannot open. Starlight's tuning workflow wants text that is diffable,
mergeable, and editable in anything, without maintaining a hand-written JSON
mapping beside the real schema.

Nothing in this part changes the schema. It is all surface you already
generated.

### Every table reads and writes JSON

The reflection descriptors every table carries, which Part 13 walks directly,
drive a generic JSON codec, so there is no per-table code to write. In C++ the
walk lives in the generated `ConfigTable.cpp`, so add that to your build once.
That is the whole cost, and a project that never touches text does not compile
it.

`json.cpp`, entire:

```cpp
#include "ConfigTable.h"
#include "SceneTable.h"
#include <cstdio>
#include <cstring>
#include <vector>

using namespace starlight;

int main()
{
    // a fixed table, out and back
    ShipConfig ship;
    memcpy( ship.display_name, "Kestrel", 7 );
    ship.display_name_length = 7;
    ship.max_health = 250.0f;
    ship.armor = 8;
    ship.ship_type = ShipType::Corvette;

    int64_t size = ShipConfigToJsonMeasure( ship );   // exact, writes nothing
    std::vector<char> text( size + 1, 0 );
    ShipConfigToJson( ship, text.data(), size );
    printf( "%s", text.data() );

    const char * edited =
        "{\n"
        "  \"display_name\": \"Kestrel II\",\n"
        "  \"max_helth\": 300,\n"
        "  \"armor\": 4,\n"
        "  \"armor\": 6,\n"
        "  \"ship_type\": \"Corvette\"\n"
        "}\n";
    TableReport report;
    ShipConfig loaded;
    bool ok = ShipConfigFromJson( loaded, edited, (int64_t) strlen( edited ), &report );
    printf( "from json: %s  name=%s health=%g armor=%d\n", ok ? "ok" : "stopped",
        loaded.display_name, loaded.max_health, loaded.armor );
    printf( "report: unknown=%d duplicate=%d clamped=%d\n",
        report.unknown, report.duplicate, report.clamped );

    ShipConfig bad;
    bad.ship_type = (ShipType) 99;
    printf( "ToJsonMeasure on an unnamed enum value: %lld\n",
        (long long) ShipConfigToJsonMeasure( bad ) );

    // a graph, out and back
    RouteBuilder builder;
    Route * route = builder.GetRoot();
    auto gate = builder.Alloc<Waypoint>();
    memcpy( gate->name, "Gate", 4 );
    gate->name_length = 4;
    gate->x = 5;
    route->stops_count = 3;
    route->stops[0] = gate;
    route->stops[1] = gate;
    builder.Lock();
    const Route * locked = builder.AsConst();

    int64_t graph_size = RouteToJsonMeasure( locked );
    std::vector<char> graph( graph_size + 1, 0 );
    RouteToJson( locked, graph.data(), graph_size );
    printf( "%s", graph.data() );

    RouteBuilder back;
    TableReport graph_report;
    bool graph_ok = RouteFromJson( back, graph.data(), graph_size, &graph_report );
    back.Lock();
    const Route * again = back.AsConst();
    printf( "from json: %s  shared=%s null=%s\n", graph_ok ? "ok" : "stopped",
        WaypointAt( again->stops[0] ) == WaypointAt( again->stops[1] ) ? "YES" : "no",
        WaypointAt( again->stops[2] ) == NULL ? "yes" : "no" );
    return 0;
}
```

```
$ c++ -std=c++17 -Wall -Wextra -Werror -I gen -I . -o json json.cpp gen/ConfigTable.cpp gen/SceneTable.cpp
$ ./json
{
  "display_name": "Kestrel",
  "max_health": 250,
  "max_speed": 500,
  "armor": 8,
  "ship_type": "Corvette"
}
from json: ok  name=Kestrel II health=100 armor=6
report: unknown=1 duplicate=1 clamped=0
ToJsonMeasure on an unnamed enum value: -1
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
from json: ok  shared=YES null=yes
```

Four things in that output.

**`ToJson` writes every field, defaults included**, because a text is for
people and a person reading a config wants the whole state. `max_speed: 500`
is a default the binary wire would have elided. The absent optionals,
`settings` and `tier`, are the exception: absence has nothing to print. Enums
render by variant name, the output is pretty-printed with a two-space indent,
and it ends in one newline.

**Reading text uses the same report as the wire.** `max_helth` is `unknown`,
and `max_health` stayed at its declared 100, so the typo did not vanish
silently. The doubled `armor` reads last wins and counts `duplicate`, which is
the counter Part 6 said belongs to the text form. Mentioned-fields-only
semantics match the wire: what the text omits takes its declared default.

**The writer refuses to lie.** Hand `ToJson` an enum value no variant names and
it returns -1 rather than inventing JSON.

**Graphs have a text form too**, and it handles what plain JSON cannot:
sharing. The first reference to a node is its body, labeled `"&node": 1`. The
second reference is the label alone. A null pointer is `null`. `RouteFromJson`
reads into a **builder**, because text becomes a graph and graphs are built,
and the shared node comes back shared.

If your text has to meet an existing convention, `| json = "type"` renames the
key in text only, and no wire byte moves. A one-file unit of its own,
`jname/Prop.schema`:

```
package jname

table Prop
{
    kind  string(16) | json = "type"
    solid bool
}
```

`j.cpp`, entire:

```cpp
#include "PropTable.h"
#include <cstdio>
#include <cstring>
#include <vector>

int main()
{
    using namespace jname;
    Prop p;
    memcpy( p.kind, "rock", 4 );
    p.kind_length = 4;
    int64_t n = PropToJsonMeasure( p );
    std::vector<char> t( n + 1, 0 );
    PropToJson( p, t.data(), n );
    printf( "%s", t.data() );
    return 0;
}
```

```
$ schema generate --lang cpp --out gen .
$ c++ -std=c++17 -Wall -Wextra -Werror -I gen -o j j.cpp gen/PropTable.cpp
$ ./j
{
  "type": "rock",
  "solid": false
}
```
### `schema pack` and `schema unpack`: the tree workflow

One JSON file per root works. A **directory** per root works better with
version control, because designers touch one small file per edit and diffs stay
readable. The tool speaks that shape directly. Start from `config.bin`, which
Part 7 wrote, and let `unpack` show you the layout:

```
$ schema unpack --root GameConfig --in config.bin --verbose tree .
report: silent — the data matched the schema exactly
unpacked config.bin into tree
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

Now be the designer. Rename the level and tune the corvette:

```
$ cat tree/level_name.json
"Kuiper Run"
$ cat tree/ships/Corvette.json
{
  "display_name": "Corvette",
  "max_health": 500,
  "max_speed": 500,
  "armor": 1,
  "ship_type": "Corvette",
  "settings": { "sensitivity": 0.8 }
}
```

and the build packs the tree back into the root's wire bytes:

```
$ schema pack --root GameConfig --out packed.bin tree .
$ echo $?
0
```

Silent, as always. The game loads `packed.bin` with the same `GameConfigLoad`
as ever. `loadpacked.cpp`, entire:

```cpp
#include "ConfigTable.h"
#include <cstdio>
#include <vector>

static std::vector<uint8_t> Slurp( const char * path )
{
    FILE * f = fopen( path, "rb" );
    std::vector<uint8_t> bytes;
    uint8_t chunk[512];
    size_t n;
    while ( ( n = fread( chunk, 1, sizeof( chunk ), f ) ) > 0 )
    {
        bytes.insert( bytes.end(), chunk, chunk + n );
    }
    fclose( f );
    return bytes;
}

int main( int, char ** argv )
{
    using namespace starlight;
    std::vector<uint8_t> buffer = Slurp( argv[1] );
    TableReport report;
    GameConfig config;
    bool ok = GameConfigLoad( config, buffer.data(), (int64_t) buffer.size(), &report );
    const ShipConfig & corvette = config.ships[ShipType::Corvette];
    printf( "%s: level=%s weapons=%d w0.damage=%g corvette=%g sensitivity=%g (present=%d)\n",
        ok ? "ok" : "stopped", config.level_name, config.weapons_count,
        config.weapons[0].damage, corvette.max_health,
        corvette.settings.sensitivity, (int) corvette.settings_present );
    return 0;
}
```

```
$ c++ -std=c++17 -Wall -Wextra -Werror -I gen -I . -o loadpacked loadpacked.cpp gen/ConfigTable.cpp
$ ./loadpacked packed.bin
ok: level=Kuiper Run weapons=2 w0.damage=35 corvette=500 sensitivity=0.8 (present=1)
```

The optional the designer wrote is present, with the value they typed. The
output is the table's wire bytes and nothing else. No magic, no hash, because
envelopes stay yours.

`--one-file` gives you the single-JSON shape instead:

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
Tune a Freighter badly:

```
$ cat tree/ships/Freighter.json
{
  "max_health": "very strong"
}
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

Put the Freighter back before you go on, because Part 11 cooks this tree:

```
$ cat tree/ships/Freighter.json
{
  "display_name": "",
  "max_health": 100,
  "max_speed": 500,
  "armor": 1,
  "ship_type": "None"
}
$ schema pack --root GameConfig --out packed.bin tree .
$ echo $?
0
```

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

One field joins `ShipConfig` first, so the unit has a flags field on a table
and the hazards below are its own:

```
table ShipConfig
{
    display_name string(32)
    max_health   float32 = 100.0
    max_speed    float32 = 500.0
    armor        int32 = 1       | min = 0, max = 10
    ship_type    ShipType
    systems      SystemFlags
    settings     ?GunnerSettings
    tier         ?int32
}
```

### The baseline: the compiler remembers what shipped

This is the record Part 6's notice has been asking for since you declared your
first table:

```
$ schema tables-baseline --update --reason "first baseline before 1.0 ships" .
$ schema check .
$ echo $?
0
```

The notice is gone and `check` is silent again, because the unit now has a
record of what shipped.

`tables.baseline` is a text projection of the unit's whole table closure, one
fact per line, made to be read in a diff. Here is the part of it that covers
`ShipConfig` and the flags it now carries:

```
schema-tables-baseline 7
package starlight

table ShipConfig
    field display_name id=0x2d21d7cd66bd5a5d kind=12 size=32
    field max_health id=0xbe5aae184d021138 kind=10 default=100.0
    field max_speed id=0x65052cb2d1409505 kind=10 default=500.0
    field armor id=0xd19988b67e699194 kind=4 min=0 max=10 default=1
    field ship_type id=0x1f7d2e86a2e77268 kind=7 enum=ShipType
    field systems id=0x17dc1c552754cefd kind=9 flags=SystemFlags
    field settings id=0xee5f6d7b48b44de8 kind=13 type=GunnerSettings optional=true
    field tier id=0x1e6f84ef2eb65989 kind=4 optional=true

flags SystemFlags
    variant Shields bit=0
    variant Cloak bit=1
    variant WarpDrive bit=2
    variant Autopilot bit=3

## history
### 2026-09-04 — first baseline before 1.0 ships
- baseline created over 6 tables — data written BEFORE this point is not covered by it
```

Commit it. From now on every `schema check`, and so every `generate`, diffs the
closure against it.

### Three verdicts

**Refused**, because the meaning changes silently. Change `max_health` from
`100.0` to `120.0`:

```
$ schema check .
tables.baseline: ShipConfig.max_health: specified default 100.0 -> 120.0 — this edit changes what data already written MEANS, and no reader can report it; if you mean it, record it: schema tables-baseline --update --reason "..." (docs/SPEC-TABLES.md §18)
schema: 1 error(s)
```

Insert a flags variant in the middle, `{ Shields, Stealth, Cloak, WarpDrive,
Autopilot }`, and the refusal names every moved bit:

```
$ schema check .
tables.baseline: flags SystemFlags: variant Cloak moved from bit 1 to bit 2 — every stored file's bits are remapped, and nothing on the wire says so — this edit changes what data already written MEANS, and no reader can report it; if you mean it, record it: schema tables-baseline --update --reason "..." (docs/SPEC-TABLES.md §18)
tables.baseline: flags SystemFlags: variant WarpDrive moved from bit 2 to bit 3 — every stored file's bits are remapped, and nothing on the wire says so — this edit changes what data already written MEANS, and no reader can report it; if you mean it, record it: schema tables-baseline --update --reason "..." (docs/SPEC-TABLES.md §18)
tables.baseline: flags SystemFlags: variant Autopilot moved from bit 3 to bit 4 — every stored file's bits are remapped, and nothing on the wire says so — this edit changes what data already written MEANS, and no reader can report it; if you mean it, record it: schema tables-baseline --update --reason "..." (docs/SPEC-TABLES.md §18)
schema: 3 error(s)
```

Append it at the end instead, `{ Shields, Cloak, WarpDrive, Autopilot, Stealth }`:

```
$ schema check .
$ echo $?
0
```

Silence. Part 6's law, now enforced.

The rest of the refused list: a field's wire kind or an array's element kind
changed, keyed and positional array spellings swapped, and a referent swapped
for one that cannot stand in.

**Warned**, because the runtime report already counts the loss but you should
know at edit time. Warnings print and exit 0. Rename `max_speed` to
`top_speed`:

```
$ schema check .
warning: tables.baseline: table ShipConfig: max_speed removed and top_speed added in one edit — if that is a rename the wire id moved with the name and every stored value orphans: declare it `top_speed ... | was = "max_speed"`, and pair `json = "max_speed"` if the text key must survive (docs/SPEC-TABLES.md §5, §16.4)
```

Shrink a capacity:

```
$ schema check .
warning: tables.baseline: ShipConfig.display_name: capacity 32 -> 16 (a stored value longer than the new capacity is truncated and counts clamped)
```

**Passed in silence**, because the wire absorbs it: fields added, removed or
reordered, enum variants added anywhere, flags appended, and bounds grown.

### Renames: `was`

The rename hazard has an in-language fix, and the warning above named it. Take
the fix:

```
table ShipConfig
{
    display_name string(32)
    max_health   float32 = 100.0
    top_speed    float32 = 500.0 | was = "max_speed"
    armor        int32 = 1       | min = 0, max = 10
    ship_type    ShipType
    systems      SystemFlags
    settings     ?GunnerSettings
    tier         ?int32
}
```

One warning remains, about the half `was` does not carry:

```
$ schema check .
warning: tables.baseline: ShipConfig.top_speed: renamed under was = "max_speed", which keeps the wire id and NOT the text key — the JSON key is now "top_speed"; pair json = "max_speed" if an existing text must still read (docs/SPEC-TABLES.md §16.4)
```

`was` carries the old identity through the rename. On the wire the field keeps
riding under `max_speed`'s hash, so `ship.bin`, written back in Part 6 when the
field was still called `max_speed`, loads into `top_speed`. `was.cpp`, entire:

```cpp
#include "ConfigTable.h"
#include <cstdio>
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

int main( int, char ** argv )
{
    using namespace starlight;
    std::vector<uint8_t> buffer = Slurp( argv[1] );
    TableReport report;
    ShipConfig config;
    ShipConfigLoad( config, buffer.data(), (int64_t) buffer.size(), &report );
    printf( "top_speed=%g unknown=%d\n", config.top_speed, report.unknown );
    return 0;
}
```

```
$ c++ -std=c++17 -Wall -Wextra -Werror -I gen -I . -o was was.cpp gen/ConfigTable.cpp
$ ./was ship.bin
top_speed=900 unknown=0
```

The 900 that Part 6 saved under `max_speed` arrives in `top_speed`. No unknown,
no loss, and the rename is invisible on the wire.

The guard rails. `bad/Bad.schema`:

```
package bad

table T
{
    damage float32 = 21.0 | was = "damage"
}
```

```
$ schema check .
Bad.schema:5:29: field damage: was = "damage" names the field's own current name — was records the OLD name after a rename; drop the attribute until one happens (docs/SPEC-TABLES.md)
schema: 1 error(s)
```

```
package bad

type T
{
    a int32 | was = "b"
}
```

```
$ schema check .
Bad.schema:3:1: type T: field a carries was = "b", but no table reaches T — was is a table-wire concept, and a field of a type outside a table closure has no wire id for it to keep; the packet wire is positional, so a rename there orphans nothing (docs/SPEC-TABLES.md §5)
schema: 1 error(s)
```

Any two fields of one table whose effective ids collide are refused too.

An enum variant and a union arm take `was` on the same terms, `Argent | was =
"Silver"` and `pong | was = "ping"`, and so does a field of a `type` a table
reaches. Renaming any of them bare makes a new name, and old data reads as
`unknown` (the renaming section below walks through it).

Put `max_speed` back before you go on, because the rest of the tutorial uses
that name.

### When you mean the break

The override is one command and it is never silent:

```
$ schema tables-baseline --update .
schema: --update needs --reason: moving the baseline declares an intentional break with data already written, and the reason is what a person reads years later when an old file refuses (docs/SPEC-TABLES.md §18.4)
$ echo $?
1

$ schema tables-baseline --update --reason "health rebalanced for 2.0; saves from 1.x read the new value" .
```

That appends to the baseline's own history, which is the changelog someone
reads in three years when a 1.x save loads oddly:

```
## history
### 2026-09-04 — first baseline before 1.0 ships
- baseline created over 6 tables — data written BEFORE this point is not covered by it

### 2026-09-04 — health rebalanced for 2.0; saves from 1.x read the new value
- ShipConfig.max_health: specified default 100.0 -> 120.0 [refuse]
```

and `check` is silent again:

```
$ schema check .
$ echo $?
0
```

No `tables.baseline` means no check, so the whole mechanism is opt in, and the
first baseline covers only what comes after it. Write it the day your format
first ships to anyone, which is what the notice has been saying.

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
of Part 9 or from a wire file, one command either way:

```
$ schema cook --root GameConfig --in tree --out Game.cook --verbose .
report: silent — the data matched the schema exactly
report: silent — the data matched the schema exactly
cooked Game.cook: 464 bytes, build version 0x8a4897ed86a715f6, little-endian, root GameConfig, 1 nodes, 384 data bytes, 16 attribution bytes
```

And the game opens it. `cook.cpp`, entire:

```cpp
#include "ConfigTable.h"
#include <cstdio>
#include <vector>

static std::vector<uint8_t> Slurp( const char * path )
{
    FILE * f = fopen( path, "rb" );
    std::vector<uint8_t> bytes;
    uint8_t chunk[512];
    size_t n;
    while ( ( n = fread( chunk, 1, sizeof( chunk ), f ) ) > 0 )
    {
        bytes.insert( bytes.end(), chunk, chunk + n );
    }
    fclose( f );
    return bytes;
}

int main( int, char ** argv )
{
    using namespace starlight;

    std::vector<uint8_t> bytes = Slurp( argv[1] );
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

    // the same bytes, from code instead of the tool
    GameConfig value;
    TableReport report;
    std::vector<uint8_t> wire = Slurp( "packed.bin" );
    GameConfigLoad( value, wire.data(), (int64_t) wire.size(), &report );
    int64_t size = GameConfigCookMeasure( value );
    std::vector<uint8_t> mine( size );
    GameConfigCook( value, mine.data(), size, TableByteOrder::Little );
    FILE * f = fopen( "Mine.cook", "wb" );
    fwrite( mine.data(), 1, size, f );
    fclose( f );
    printf( "wrote Mine.cook, %lld bytes\n", (long long) size );
    return 0;
}
```

```
$ c++ -std=c++17 -Wall -Wextra -Werror -I gen -I . -o cook cook.cpp gen/ConfigTable.cpp
$ ./cook Game.cook
opened: level=Kuiper Run corvette health=500
wrote Mine.cook, 464 bytes
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

### Cooking from code

The second half of that program is the write side, and its bytes are pinned to
the tool's, so the two producers are interchangeable:

```
$ cmp Game.cook Mine.cook && echo "Game.cook and Mine.cook are identical"
Game.cook and Mine.cook are identical
```

A variable root cooks straight from its builder, with `RouteCook( builder, ... )`,
and `RouteOpen` walks it in place.

### It is an accelerator, not an archive

The header check is an **identity** check and not a trust boundary. A cook is
data your own pipeline produced for exactly one build. Add one field to
`GameConfig`:

```
table GameConfig
{
    friendly_fire bool
    weapons       [..MaxWeapons]WeaponConfig
    ships         [ShipType]ShipConfig
    level_name    string(64)
    par_time      float32 = 0.0
}
```

```
$ schema check .
$ echo $?
0
$ schema id .
0x60d7cbad6bb296f2
$ schema build-version .
0x8272fb7f3068abdf
$ ./cook Game.cook
not this build's cook
```

Three facts in four commands. Adding a field to a table **passes** the
baseline, because the wire absorbs it. The **protocol id does not move**, because
a table is not on the packet wire. The **build version does**, so the new build
refuses the old cook and falls back to the wire, which still loads it fine and
absorbs the new field as a default while your pipeline recooks.

Take `par_time` back out before you go on, and `./cook Game.cook` opens again.

A big-endian cook refuses on a little-endian build the same way:

```
$ schema cook --root GameConfig --in tree --out GameBig.cook --byte-order big --verbose .
cooked GameBig.cook: 464 bytes, build version 0x8a4897ed86a715f6, big-endian, root GameConfig, 1 nodes, 384 data bytes, 16 attribution bytes
$ ./cook GameBig.cook
not this build's cook
```

The tolerant wire remains the format of record, and the cook is the fast lane
your build key can always regenerate.

### Checking one you did not write

If you hold a cooked file whose provenance you doubt, because it crossed a
machine boundary or because you are diagnosing one, the **tool** validates it
offline, once, on purpose:

```
$ schema cook-check --root GameConfig --verbose Game.cook .
ok: build version 0x8a4897ed86a715f6, little-endian, root GameConfig, 1 nodes, 384 data bytes, 16 attribution bytes, 0 reference slots
```

`cook-check` verifies every reference and count against the attribution, a
small directory the cook carries beside its data and which `--attribution
<file>` can separate out for builds that ship none. That is a person's
decision, not a flag on the hot path.

`schema uncook` turns a cook back into wire bytes, which is also the proof the
cook lost nothing:

```
$ schema uncook --root GameConfig --in Game.cook --out back.bin .
$ cmp back.bin packed.bin && echo "identical to packed.bin"
identical to packed.bin
```

### The build version, precisely

Two ids now exist in your project, and there is no third.

The **protocol id** is the packet wire's identity, and it gates connections.

The **build version** is everything a cook's or a block's bytes depend on that
is target neutral: the protocol id, every record's layout as the compiler's C
ABI model computes it, and the meaning facts, which are the defaults, ranges,
variant order and arm order. It keys cooked assets and gates no connection.

`schema build-version --facts` prints the entire digest input, which is the
file to diff when you want to know **why** the version moved:

```
$ schema build-version --facts .
schema-build-version 3
protocol 60d7cbad6bb296f2
byteorder little
block prologue=magic:8,build_version:8,byte_order:8
record GameConfig sizeof=384 alignof=8
    field 79cee25d9004059f kind=1 offset=0 size=1
    field 41cbd901b87fabb6 kind=13 offset=4 size=68 type=WeaponConfig elem=8 array=bounded bound=8
    field 294a5c4913e1ad44 kind=13 offset=72 size=240 type=ShipConfig elem=80 array=keyed bound=3 key=ShipType
    field 38b921b4ce4e20c5 kind=12 offset=312 size=72 bound=64
block GameConfig sizeof=360 alignof=8
    slot 79cee25d9004059f offset=24 size=1
```

and it continues through every record and block in the unit. The same number is
exported as `BuildVersion` in the generated code and stamped into every cook
header and block prologue.

The store contract is one line: your pipeline stores assets under the triple
**(asset hash, build version, byte order)**. The asset hash is the hash of the
wire file you cooked from. The build version is target neutral, which the
big-endian cook above shows by stamping the same `0x8a4897ed86a715f6`, and
`byteorder little` appearing in the facts is a generation input that never
varies. Byte order is the third coordinate, carried in the header and refused
by `Open` on a mismatch. Your game asks for exactly that triple, anything that
would change the bytes moves the key, and you never reason about which edits
invalidate what.

**You now have** an asset pipeline: designer tree, then wire, then cook, opened
by pointer in constant time, keyed so staleness is impossible, with the
tolerant wire as the fallback that never breaks.

---

## Part 12: The block form: a frame another language reads in place

### The problem

Starlight's simulation is C++ and its tools overlay is C. Sixty times a second
the sim produces "what to draw", thousands of ships and lasers, and the other
side wants to **point at** that memory and iterate rather than deserialize it.
Serializing 60 Hz bulk data that never leaves the machine is paying a tolerance
tax on a same-build handoff.

### You already declared it

Two constants join `Game.schema`, `const MaxShips = 4096` and
`const MaxLasers = 8192`, and `Render.schema` is a new file in the same unit:

```
package starlight

table RenderShip
{
    x     float32
    y     float32
    angle float32
    hull  float32 = 1.0
}

table RenderLaser
{
    x0 float32
    y0 float32
    x1 float32
    y1 float32
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
Adding three tables passes the baseline, and the unit still generates for all
nine:

```
$ schema check .
$ echo $?
0
$ for l in c cpp cs dart elixir go java js rust; do printf "%-8s " $l; schema generate --lang $l --out /tmp/g-$l . && echo ok; done
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

But every fixed table also gets a third form of the same declaration, the
**block**. The instance at the front carries, per array, where its rows start,
how many there are, and how far apart they sit. The rows follow out of line at
a fixed pitch. The other side reads three numbers and points.

You reach for it by including it. `RenderBlock.h` and `RenderBlock.cpp` sit
beside `RenderTable.h`, and a project that never blocks a table compiles none
of it. Which arrays move out of line is one rule: the table's own bounded
arrays of structs, and nothing else. A row's pitch is its `sizeof`. A
variable-length table has no block form, because a pointer means no fixed
pitch, so `Route` from Part 8 has none and `RenderFrame` does.

### Produce it, wide and allocation free

`produce.cpp`, entire:

```cpp
#include "RenderBlock.h"
#include <cstdio>

int main()
{
    using namespace starlight;

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
    printf( "block: %lld bytes of %lld max (frame %llu, %d ships)\n",
        (long long) bytes, (long long) RenderFrameBlockMaxBytes,
        (unsigned long long) block.projection->frame, counts.ships );
    printf( "build version 0x%016llx\n", (unsigned long long) BuildVersion );

    FILE * f = fopen( "frame.block", "wb" );
    fwrite( storage.base, 1, bytes, f );
    fclose( f );
    printf( "wrote frame.block\n" );

    RenderFrameCounts too_many = counts;
    too_many.ships = 5000;
    TableBlockRefusal refusal;
    if ( !RenderFrameBlockBegin( block, storage, too_many, &refusal ) )
    {
        printf( "refused: array %s count %lld max %lld\n", refusal.array,
            (long long) refusal.count, (long long) refusal.maximum );
    }
    return 0;
}
```

```
$ c++ -std=c++17 -Wall -Wextra -Werror -I gen -I . -o produce produce.cpp gen/RenderBlock.cpp gen/RenderTable.cpp
$ ./produce
block: 56064 bytes of 196672 max (frame 1207, 3000 ships)
build version 0x5042e348da4b84ea
wrote frame.block
refused: array ships count 5000 max 4096
```

`Begin` lays the block out for **this frame's** counts, so the handoff is 56 KB
and not the 196 KB maximum, which `RenderFrameBlockMaxBytes` names and which is
the once-ever allocation. Fill from four threads over disjoint ranges, because
nothing in the fill path locks or allocates. That is an obligation on the
generated code and not a hope.

Ask for more than you declared and the refusal names everything, through a
three-field struct:

```cpp
struct TableBlockRefusal
{
    const char * array = NULL;
    int64_t count = 0;
    int64_t maximum = 0;
};
```

A block stays valid until the next `Begin` on that storage, so double-buffer
with two storages if the consumer reads while you fill.

### Consume it, from another language, with one check

The same declaration generates the read half everywhere. Here is **C reading
the block C++ just wrote**. The bytes went through a file for the demo, and a
pointer handoff is the production path. `consume.c`, entire:

```c
#include "RenderBlock.h"
#include <stdio.h>
#include <stdlib.h>

int main( void )
{
    FILE * f = fopen( "frame.block", "rb" );
    fseek( f, 0, SEEK_END );
    long bytes = ftell( f );
    fseek( f, 0, SEEK_SET );

    /* the base must be 64-byte aligned, whatever the transport was */
    void * aligned = NULL;
    if ( posix_memalign( &aligned, 64, (size_t) bytes ) != 0 ) { return 1; }
    if ( fread( aligned, 1, (size_t) bytes, f ) != (size_t) bytes ) { return 1; }
    fclose( f );

    RenderFrameBlock block;
    if ( !render_frame_block_open( &block, aligned, (int64_t) bytes ) )
    {
        printf( "not this build's block\n" );
        return 0;
    }

    RenderShip * ships = render_frame_ships_span( &block );
    TableBlockRows rows = render_frame_ships_rows( &block );
    printf( "c read: frame=%llu ships=%d ships[2999]=(%g, %g)\n",
        (unsigned long long) block.projection->frame, rows.count,
        ships[2999].x, ships[2999].y );
    printf( "c build version 0x%016llx\n", (unsigned long long) SCHEMA_STARLIGHT_BUILD_VERSION_VALUE );
    free( aligned );
    return 0;
}
```

```
$ schema generate --lang c --out genc .
$ cc -std=c99 -Wall -Wextra -Werror -I genc -o consume consume.c genc/RenderBlock.c genc/RenderTable.c
$ ./consume
c read: frame=1207 ships=3000 ships[2999]=(2999, -2999)
c build version 0x5042e348da4b84ea
```

The two builds carry the same build version, `0x5042e348da4b84ea` in the C++
header and the C source alike. `BlockOpen` checks the prologue, which is the
magic, the byte order, the **build version**, the base's 64-byte alignment,
every count against its declared maximum, and every extent, and then you index.
Both sides assert every row size and field offset against the compiler's shared
layout model at compile time, so a toolchain that disagrees stops and names the
record and the field rather than garbling a frame.

Two consumer obligations that the first integration always trips on.

**Alignment binds the consumer.** The producer's storage is 64-byte aligned, so
a consumer that got the bytes from a file or a socket must hand `Open` 64-byte
aligned memory too. The C program above uses `posix_memalign`, and a C#
consumer uses native memory because a pinned `byte[]` guarantees nothing.

**It is a same-build contract.** Give `RenderShip` one more field, rebuild only
the C side, and:

```
table RenderShip
{
    x      float32
    y      float32
    angle  float32
    hull   float32 = 1.0
    shield float32 = 0.0
}
```

```
$ schema generate --lang c --out genc .
$ cc -std=c99 -Wall -Wextra -Werror -I genc -o consume consume.c genc/RenderBlock.c genc/RenderTable.c
$ ./consume
not this build's block
```

Every schema edit is a regenerate on both sides. That is the trade for zero
version machinery in a 60 Hz path: nothing to absorb, nothing to ask for by
name. Data that outlives builds rides the wire, which this same table still
has. Take `shield` back out and `./consume` reads the frame again.

**You now have** the simulation to renderer handoff: filled in parallel at
frame rate, read in place from another language, guarded by one comparison.

---

## Part 13: One schema, every language, and tools that walk it

### The problem

Starlight is not one program. The server is C++, the tools are C, and maybe the
launcher is C#. Every one of them needs the same constants, the same packets
and the same assets, and the tools team wants a generic config editor without
hand-writing a UI per table.

### Three wires cross to C

Part 6 drew the line: the packet wire, the cook and the block cross all nine
targets, and the table wire is C++'s within one build. This part runs all three
crossings, from the same `starlight` unit, into C.

Generate the C side of the unit you already have:

```
$ schema generate --lang c --out genc .
```

### The packet wire

`writepacket.cpp`, entire:

```cpp
#include "NetWire.h"
#include <cstdio>
#include <cstring>

int main()
{
    using namespace starlight;

    ShipState ship;
    ship.ship_type = ShipType::Corvette;
    strcpy( ship.name, "Kestrel" );
    ship.name_length = 7;
    ship.health = 750;
    ship.throttle = 0.63f;
    ship.aim.x = 1.5f;
    ship.aim.y = 0.25f;
    ship.systems = SystemFlags_Shields | SystemFlags_WarpDrive;

    uint8_t buffer[ShipStateMaxBytes];
    serialize::WriteStream w( buffer, sizeof( buffer ) );
    WriteShipState( w, ship );
    w.Flush();

    FILE * f = fopen( "packet.bin", "wb" );
    fwrite( buffer, 1, w.GetBytesProcessed(), f );
    fclose( f );
    printf( "c++ wrote packet.bin, %lld bytes\n", (long long) w.GetBytesProcessed() );
    return 0;
}
```

`readpacket.c`, entire:

```c
#include "GameWire.h"
#include <stdio.h>
#include <string.h>

int main( void )
{
    /* the read slack the contract asks for: 8 bytes past the packet data */
    uint8_t buffer[SHIP_STATE_MAX_BYTES + 8];
    memset( buffer, 0, sizeof( buffer ) );

    FILE * f = fopen( "packet.bin", "rb" );
    size_t bytes = fread( buffer, 1, SHIP_STATE_MAX_BYTES, f );
    fclose( f );

    serialize_read_stream_t stream;
    serialize_read_stream_init( &stream, buffer, (int) bytes );

    ShipState ship = new_ship_state();
    if ( !read_ship_state( &stream, &ship ) )
    {
        printf( "c refused the packet\n" );
        return 1;
    }

    char names[SYSTEM_FLAGS_NAMES_MAX];
    printf( "c read the C++ packet: %s \"%s\" health=%d throttle=%.2f aim=(%g, %g) systems=%s\n",
        enum_name_ship_type( ship.ship_type ), ship.name, ship.health, ship.throttle,
        ship.aim.x, ship.aim.y,
        flag_names_system_flags( ship.systems, names, sizeof( names ) ) );
    return 0;
}
```

```
$ c++ -std=c++17 -Wall -Wextra -Werror -ffp-contract=off -I gen -I . -I ../serialize -o writepacket writepacket.cpp
$ ./writepacket
c++ wrote packet.bin, 47 bytes
$ cc -std=c99 -Wall -Wextra -Werror -ffp-contract=off -I genc -I ../serialize.c -o readpacket readpacket.c ../serialize.c/serialize.c -lm
$ ./readpacket
c read the C++ packet: Corvette "Kestrel" health=750 throttle=0.63 aim=(1.5, 0.25) systems=Shields|WarpDrive
```

Same Corvette, same 750, same compressed throttle, same mask. The same bits.

Note the shape of the C names. Constants become macros, `SHIP_STATE_MAX_BYTES`
and `SYSTEM_FLAGS_NAMES_MAX`, functions are `snake_case`, and a type's defaults
arrive through `new_ship_state()`, because C structs have no member
initializers. The read slack is in the buffer, because C and C++ read 64-bit
windows.

`aim` came through as a plain `Vector2`. The `cpp_native` mapping is a C++
facility, so C generates the schema's own struct and the wire is identical
either way.

### The cook

Part 12 added `Render.schema` to the unit, which moved the build version, so
re-cook before crossing:

```
$ schema cook --root GameConfig --in tree --out Game.cook --verbose .
cooked Game.cook: 464 bytes, build version 0x5042e348da4b84ea, little-endian, root GameConfig, 1 nodes, 384 data bytes, 16 attribution bytes
```

`opencook.c`, entire:

```c
#include "ConfigTable.h"
#include <stdio.h>
#include <stdlib.h>

int main( void )
{
    FILE * f = fopen( "Game.cook", "rb" );
    fseek( f, 0, SEEK_END );
    long bytes = ftell( f );
    fseek( f, 0, SEEK_SET );

    void * aligned = NULL;
    if ( posix_memalign( &aligned, 64, (size_t) bytes ) != 0 ) { return 1; }
    if ( fread( aligned, 1, (size_t) bytes, f ) != (size_t) bytes ) { return 1; }
    fclose( f );

    const GameConfig * config = game_config_open( aligned, (uint64_t) bytes );
    if ( config == NULL )
    {
        printf( "not this build's cook\n" );
        return 0;
    }
    printf( "c opened the C++ cook: level=%.*s corvette health=%g\n",
        config->level_name_length, config->level_name,
        config->ships[SHIP_TYPE_CORVETTE - 1].max_health );
    free( aligned );
    return 0;
}
```

```
$ cc -std=c99 -Wall -Wextra -Werror -I genc -o opencook opencook.c genc/ConfigTable.c
$ ./opencook
c opened the C++ cook: level=Kuiper Run corvette health=500
$ ./cook Game.cook
opened: level=Kuiper Run corvette health=500
wrote Mine.cook, 464 bytes
```

The same 464 bytes, opened by pointer from both languages. In C a keyed array
is the plain array the comment describes, `ShipConfig ships[SHIP_TYPE_MAX]`
with the key `k` at index `k - 1`, so the `- 1` that C++ hides in
`TableKeyed` is visible here.

### The block

Part 12 already ran this crossing end to end: `produce.cpp` fills a
`RenderFrame` block in C++, `consume.c` opens it in C at the same build
version, and one flipped field on either side turns it into
`not this build's block`.

So the three crossings are the packet wire for data both ends ship together,
the cook for assets one pipeline produces for one build, and the block for a
60 Hz handoff inside one build.

### Reflection: tools without hand-written mirrors

Every type in a table closure carries a static descriptor with names, wire ids
and kinds, storage offsets, ranges, enum vocabularies, optionals and branch
guards. That is enough to write a generic editor, printer or differ with no
RTTI and no schema files shipped at runtime. `reflect.cpp`, entire:

```cpp
#include "ConfigTable.h"
#include <cstdio>

int main()
{
    using namespace starlight;

    const TableTypeInfo * type = ShipConfigTableType();
    printf( "table %s (%u bytes, %d fields)\n", type->name, type->size, type->num_fields );
    for ( int32_t i = 0; i < type->num_fields; i++ )
    {
        const TableFieldInfo & field = type->fields[i];
        printf( "  %-13s %-15s id=0x%016llx kind=%-2d @%u",
            field.name, field.type_name, (unsigned long long) field.id,
            field.kind, field.offset );
        if ( field.optional ) { printf( " optional" ); }
        if ( field.has_range ) { printf( " [%g, %g]", field.range_min, field.range_max ); }
        if ( field.enum_name != NULL )
        {
            // an enum's vocabulary starts at 1, because 0 is None. A FLAGS
            // field's is indexed by BIT and starts at 0, and the tell is that
            // flags variants have no per-variant wire id.
            const int64_t first = field.variant_id != NULL ? 1 : 0;
            for ( int64_t v = first; v <= field.enum_max; v++ )
            {
                printf( " %s", field.enum_name( (uint64_t) v ) );
            }
        }
        printf( "\n" );
    }
    return 0;
}
```

```
$ c++ -std=c++17 -Wall -Wextra -Werror -I gen -I . -o reflect reflect.cpp gen/ConfigTable.cpp
$ ./reflect
table ShipConfig (80 bytes, 8 fields)
  display_name  string          id=0x2d21d7cd66bd5a5d kind=12 @0
  max_health    float32         id=0xbe5aae184d021138 kind=10 @40
  max_speed     float32         id=0x65052cb2d1409505 kind=10 @44
  armor         int32           id=0xd19988b67e699194 kind=4  @48 [0, 10]
  ship_type     ShipType        id=0x1f7d2e86a2e77268 kind=30 @52 Fighter Freighter Corvette
  systems       SystemFlags     id=0x17dc1c552754cefd kind=9  @56 Shields Cloak WarpDrive Autopilot
  settings      GunnerSettings  id=0xee5f6d7b48b44de8 kind=13 @64 optional
  tier          int32           id=0x1e6f84ef2eb65989 kind=4  @72 optional
```

Those are the same ids Part 10's baseline printed, which is the point: one
identity, in the file the compiler diffs and in the descriptor your tool walks.

The comment in that loop earns its place. `enum_name` serves both enums and
flags, and the two are indexed differently, so the descriptor gives you a tell:
`variant_id` is non-NULL for an enum, whose values start at 1 because 0 is
`None`, and NULL for a flags field, whose vocabulary is indexed by bit from 0.
Get it wrong and `Shields` quietly vanishes from your editor.

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
beside the nine. A directory of its own, `docsgen/`, with a `go.mod`:

```
module docsgen

go 1.26

require github.com/mas-bandwidth/schema/v2 v2.4.0

replace github.com/mas-bandwidth/schema/v2 => ../schema
```

`main.go`, entire:

```go
package main

import (
	"fmt"
	"sort"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// docs is a generator of our own, selected by --lang docs.
type docs struct{}

func (docs) Names() []string { return []string{"docs"} }

func (docs) Generate(u *ir.Unit, opts compiler.Options) (map[string][]byte, error) {
	names := make([]string, 0, len(u.Structs))
	bits := map[string]int64{}
	for _, s := range u.Structs {
		names = append(names, s.Name)
		bits[s.Name] = ir.MaxBitsStruct(s)
	}
	sort.Strings(names) // deterministic output is a generator's contract
	out := []byte{}
	for _, name := range names {
		out = append(out, fmt.Sprintf("## %s — %d bits max\n", name, bits[name])...)
	}
	return map[string][]byte{"TYPES.md": out}, nil
}

func main() {
	c := compiler.New()
	if err := c.Register(docs{}); err != nil {
		panic(err)
	}
	fmt.Println(c.Targets())

	paths, err := compiler.GatherPaths([]string{"../starlight"})
	if err != nil {
		panic(err)
	}
	unit, err := c.Load(paths) // format free: writes nothing
	if err != nil {
		panic(err)
	}
	fmt.Printf("package %s, protocol id 0x%016x\n", unit.Package, unit.ProtocolId)

	files, err := c.Generate(unit, "docs", nil) // returns bytes, writes nothing
	if err != nil {
		panic(err)
	}
	fmt.Print(string(files["TYPES.md"]))
}
```

```
$ go build -o docsgen . && ./docsgen
[c cpp cs dart docs elixir go java js rust]
package starlight, protocol id 0x60d7cbad6bb296f2
## ChatLine — 990 bits max
## Contact — 34 bits max
## FireCommand — 41 bits max
## LaserFire — 23 bits max
## MissileFire — 18 bits max
## Packet — 24779 bits max
## PacketHeader — 39 bits max
## ShipState — 769 bits max
## Snapshot — 6188 bits max
## Vector2 — 64 bits max
## Vector3 — 192 bits max
```

Your generator receives the same resolved IR the built-in backends read, with
types resolved, constants folded and widths derived. `ir.MaxBitsStruct` says
769 bits for `ShipState`, which is the same number Part 4's header carried, and
the protocol id is the one Part 5 printed.

The `sort.Strings` is not decoration. `u.Structs` is a map, Go randomizes map
iteration, and the golden gate compares a backend's output byte for byte. A
generator that emits in map order passes on Monday and fails on Tuesday.

The exported `compiler` and `ir` surfaces are under semantic versioning, and
`internal/` is not.

**You now have** Starlight's whole estate on one schema: a C++ simulation, C
tools reading its packets, cooks and blocks, generic editors over the
descriptors, and a compiler you can embed when the build gets opinionated.

---

## Part 14: The tool belt, and where the edges are

### Every command you now know

All of these appeared on the way, and this is the map. `fmt` is the only one
that writes a `.schema` file, so every other command reads the unit as it sits
on disk and a read-only checkout works:

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

### Silence, and the one exception

Success is silent, with one exception, and Part 6 met it: a unit that declares
a table and has no `tables.baseline` beside it draws a **notice** from every
`check`.

```
notice: starlight declares 1 table and . holds no tables.baseline — save-game evolution is unguarded (docs/SPEC-TABLES.md §18); commit one with: schema tables-baseline --update --reason "first baseline" .
```

The exit status is 0 and nothing stops. Part 10 answered it by committing a
baseline, and from there `check` is silent again. Read the rule as: **success
is silent except while your table format is unguarded**, which is a state you
end by writing the record.

Warnings print too, and also exit 0. Those are Part 10's middle verdict.

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

One habit the ids reward: check `schema version` against the docs you are
reading. The language moves, and a refusal that cites a specification section
cites the tool's specification at its build, not necessarily the page you have
open.

### Where the edges are

Three boundaries you will meet, so you do not read a refusal as a defect.

**Packet-wire framing stays off the table wire.** `const`, `reserved` and
`align` are bit positions, and a table's wire has none. `bad/Bad.schema`:

```
package bad

table T
{
    const(0xC7, 8)
    x int32
}
```

```
$ schema check .
Bad.schema:5:5: const(value, bits) is a packet-wire construct — a table's wire is field-tagged TLV with no bit positions; remove it from table T (docs/SPEC-TABLES.md)
schema: 1 error(s)
```

```
package bad

table T
{
    x int32
    align
    y int32
}
```

```
$ schema check .
Bad.schema:6:5: align is a packet-wire construct — a table's wire is field-tagged TLV with no bit positions; remove it from table T (docs/SPEC-TABLES.md)
schema: 1 error(s)
```

**A `type` never reaches a table.** Packets are exact match and value
semantics, so a `type` cannot hold a table, a pointer or a map. A table may
hold types freely, which is why `ShipConfig` names `ShipType` and `ShipState`
cannot name `ShipConfig`. That one-way rule is what keeps a packet's cost
independent of everything tables buy.

**`bytes` needs a bound.** `bytes(N)` is a bounded buffer, and a bare `bytes`
does not parse. `bad/Bad.schema`:

```
package bad

table T { b bytes }
```

```
$ schema check .
Bad.schema:3:19: expected (, found "}"
Bad.schema:3:19: expected an expression, found "}"
Bad.schema:4:1: expected ), found "end of file"
Bad.schema:4:1: unexpected end of file inside block (missing } )
schema: 4 error(s)
```

Four errors for one missing `(N)`, because the parser keeps trying. Read the
first. For a buffer whose size is a runtime fact, point at it: `*bytes` and
`*string` are the Part 7 spellings, in the second unit.

### The unit registry, and where it is not yet

SPEC-TABLES §8.3 describes a **unit registry**, `UnitView()`, one entry point
listing every declaration in the build. The per-table and per-type descriptors
Part 13 walked are in every target. The unit-wide root is in C++, for a unit
that declares a table — this one does, so the pair is right there in the
listing:

```
$ schema generate --lang cpp --out gen --verbose .
wrote gen/Config.h
wrote gen/ConfigBlock.cpp
wrote gen/ConfigBlock.h
wrote gen/ConfigTable.cpp
wrote gen/ConfigTable.h
wrote gen/ConfigWire.h
wrote gen/Game.h
wrote gen/GameTable.h
wrote gen/GameWire.h
wrote gen/Net.h
wrote gen/NetTable.h
wrote gen/NetWire.h
wrote gen/Render.h
wrote gen/RenderBlock.cpp
wrote gen/RenderBlock.h
wrote gen/RenderTable.h
wrote gen/RenderWire.h
wrote gen/Scene.h
...
wrote gen/StarlightView.cpp
wrote gen/StarlightView.h
```

One pair for the whole unit, named for the package. Nothing includes it and
nothing asks for it: compile `StarlightView.cpp` in a tool and it walks the
build; leave it out of the game and the binary carries none of it.

The other eight targets have no view file yet, and neither does a C++ unit
that declares no table at all — a packet-only unit's inspector still builds on
the per-type descriptors and holds the type list itself.

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

One unit, `starlight`, nine schema files, and a second unit beside it for the
C++-only table forms. Out of that: packets that refuse hostile bytes in three
bytes a ship, a save format that survives two years of schema drift and says
exactly what it absorbed, a designer JSON loop, fleets keyed by name, cooked
configs that open by pointer from C++ and from C, and a 60 Hz block handoff
read in place from another language.

Every byte of it came out of declarations short enough to read aloud.
