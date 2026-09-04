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
