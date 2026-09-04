# schema

[![CI](https://github.com/mas-bandwidth/schema/actions/workflows/ci.yml/badge.svg)](https://github.com/mas-bandwidth/schema/actions/workflows/ci.yml)
[![License: AGPL v3](https://img.shields.io/badge/license-AGPL--3.0-blue.svg)](LICENSE)

**Schema: the data schema language for games.**

If you write a game in more than one language, or ship a client and server that
have to agree on every bit, schema is a language that will help you do this
without ever having to hand-code definitions in each language ever again.

## Design

**schema** is meant to serve all your needs for data types across all languages used when developing a game:

* The packet between a client and a server, where every bit counts and both sides ship together.
* The message between the server and a backend that needs versioning.
* The save game that has to load in a build its writer never saw.
* The render data C++ writes and C# reads sixty times a second.
* The asset that tools build and cook to an efficient runtime binary format.

One system does all of it, so you never end up with schema for the packets and something else for everything else.

If this work helps you, please support it: **[Become a supporter](https://www.patreon.com/MasBandwidth/membership)**

## Features

1. Define constants, enums, flags, types and tables in one language.
2. Generate fast bit-packed serialization for struct types that do not need versioning, such as client/server packets and state.
3. Generate versioned tables for messages, data, assets, save game files and everything else.
4. Tables can point at tables, so trees and graphs are tables too.
5. Cook tables to a binary format the game runtime loads for tool pipelines and asset loading.

Supported languages: C, C++, C#, Dart, Elixir, Go, Java, JavaScript and Rust.

## Examples

### 1. Bit-packed serialization for types

```
package example

const MaxHealth = 1000

enum ShipType { Fighter, Corvette, Bomber }

flags ShipFlags { Firing, Thrusting, Disabled }

type Vec3
{
    x float64
    y float64
    z float64
}

type Quaternion
{
    x float64
    y float64
    z float64
    w float64 = 1.0
}

type ShipState
{
    ship_type  ShipType
    ship_flags ShipFlags
    position   Vec3
    rotation   Quaternion
    health     int32      | min = 0, max = MaxHealth
    at_rest    bool
    if !at_rest
    {
        linear_velocity  Vec3
        angular_velocity Vec3
    }
}
```

Write your data types once and generate bit-packed serialization code to read and write them. Best for client/server messages and state where the client speaks the same binary protocol or won't be allowed to connect to the server.

_This feature is production ready_.

### 2. Versioned messages passed between tools, backends and websites

```
package tools

enum Platform { Windows, Mac, Linux }

table BuildRequest
{
    branch   string(64)
    platform Platform
    revision uint64
    shard    int32 = 0  | min = 0, max = 63
    verbose  bool
}
```

Tables are versioned. Each field carries an id, a kind and a length,
so a reader that does not know a field skips it, a field that is missing
takes its declared default, a field that was renamed is found under its old
name, and a value out of range is clamped. Every one of those events is
counted in a read report. Nothing is fatal in either direction unless
you want it to be.

_The tables feature is new in this release_.

### 3. Save games

```
package savegame

table Hero
{
    name  string(32)
    level int32 = 1       | min = 1, max = 99
    speed float32 = 500.0 | was = "velocity"
}

table SaveGame
{
    party [..8]Hero
}
```

A save file is a table too, so it gets the same tolerance. It also gets a
guard the wire cannot give: a committed baseline file that refuses, at
compile time, the two edits that change what old data means without changing
a byte of it. Changing a field's default is one. Reordering a `flags` variant
is the other. When you mean the change, you record it with a reason, and the
reason stays in the file.

_The version baseline feature is in preview in this release_.

### 4. Assets cooked for a build

```
package assets

table Mesh
{
    name      string(64)
    vertices  uint32
    triangles uint32
}

table MeshCatalogue
{
    meshes [..4096]Mesh
}
```

```sh
schema pack --root MeshCatalogue --out Assets.bin assets/ .
schema cook --root MeshCatalogue --in Assets.bin --out Assets.cook .
schema build-version .
```

Tools build the assets. The game should not parse them at load. It should map the file and point at it.

`schema cook` writes a table's data in the exact memory layout of one build,
in that build's byte order. Opening it is a header check and a cast: magic,
byte order, build version, lengths, alignment. Nothing is walked, so a
gigabyte opens as fast as a kilobyte. A cook only opens in the build it was
cooked for. Any other build gets NULL and loads the wire instead, which every
build can read.

_The cook feature is in preview in this release_.

### 5. Render data from C++ to C#

```
package render

table RenderShip
{
    position_x float64
    position_y float64
    position_z float64
    object_id  uint32
}

table RenderFrame
{
    ships [..4096]RenderShip
}
```

Every frame, C++ writes a large block of render data and C# reads it. This is
the seam between a native plugin and Unity, and neither side can afford a
copy or a parse.

Every fixed table has a block form. Its arrays are laid out at a fixed pitch
with the offsets at the front. C++ fills it from as many threads as it likes,
with no lock and no allocation. C# opens it and reads the rows as spans over
the same memory. Both sides are generated from the one declaration, and the
row sizes and field offsets are asserted at compile time in both languages.
A field that moves is a build error, not a garbled frame.

_This feature is new in this release_.

### 6. JSON packed into binary tables

```
package config

table WeaponConfig
{
    damage float32 = 21.0
    homing bool
}

table GameConfig
{
    level_name string(64)
    weapons    [..16]WeaponConfig
}
```

```sh
schema pack   --root GameConfig --out Config.bin configs/ .
schema unpack --root GameConfig --in  Config.bin --one-file configs/ .
```

Data gets authored or exported as JSON. It needs to become a compact binary
that several languages can read.

`schema pack` takes a directory of JSON files shaped like the table and
writes the table's wire bytes, nothing else around them. `schema unpack`
writes the JSON back out. Both fail the build when the read report is not
silent, so drift between the JSON and the schema does not slip through.

_This feature is new in this release_.

### 7. Editors and debug views

```
package editor

enum Grade { Bronze, Silver, Gold }

table ShipConfig
{
    health float32 = 100.0
    armor  int32 = 1       | min = 0, max = 10
    grade  Grade = Silver
}
```

An editor wants to show any table as a property tree. A debug view wants to
inspect what the game is holding this frame.

Every table comes with reflection descriptors beside its code: each field's
name, type, offset, range, and the names of every enum and union variant. A
tool walks a table it has never seen. A debug view walks the live instance in
memory. There is no RTTI and no schema file at run time.

_This feature is new in this release_.

## Performance

Cost to serialize a representative game packet, relative to generated C++ at
100%. Lower is faster.

| Language | % |
|---|---:|
| C | 100% |
| C++ | 100% |
| Rust | 154% |
| Java | 162% |
| Go | 210% |
| C# | 225% |
| Dart | 227% |
| JavaScript | 264% |
| Elixir | 1283% |

Measured by [the benchmark](bench/): the real generated code in every
language, over one 438-byte packet exercising every construct. Raw sweeps are
committed under [bench/results](bench/results/); reproduce with
`./bench/run.sh`.

Tables ride their own wire and have their own board: the same shape declared
as a fixed table, written and read in every language that carries tables
([bench/tables](bench/tables/), `make bench-tables`).

## Install and build

```
make            # builds the compiler at bin/schema — needs only Go

bin/schema check    <dir of .schema files>
bin/schema generate --lang c|cpp|cs|dart|elixir|go|java|js|rust --out <outdir> <dir>
```

`make` builds `bin/schema` and nothing else. Success is silent; `--verbose`
lists every file written. The nine-language conformance chain is `make test`,
which needs the serialize runtime checkouts beside this repo and the pinned
Dart, Java and Elixir toolchains unpacked into `dist/`. The Makefile header
carries every version and hash, and [CONTRIBUTING.md](docs/CONTRIBUTING.md) has
the clone list.

## Documentation

| Document | What's in it |
|---|---|
| **[USAGE.md](docs/USAGE.md)** | Every language feature, with the code it generates. Start here. |
| **[SPEC.md](docs/SPEC.md)** | The normative reference for the type wire: grammar, wire law, every edge case. |
| **[SPEC-TABLES.md](docs/SPEC-TABLES.md)** | The normative reference for tables: the wire, the cook, the block form, reflection, the build version. |

Beside them: [PERFORMANCE.md](docs/PERFORMANCE.md),
[COMPARISON.md](docs/COMPARISON.md) (the same packet against Cap'n Proto, Protobuf
and FlatBuffers), [FAQ.md](docs/FAQ.md), [VERSIONING.md](docs/VERSIONING.md),
[CONTRIBUTING.md](docs/CONTRIBUTING.md) and [SECURITY.md](docs/SECURITY.md).

Where a guide and a spec cover the same ground: the spec keeps the spelling a
consumer needs to write from the page alone, even when USAGE also shows it.

## License

**The compiler is AGPL-3.0, and will stay that way. The code it generates is
yours.**

The compiler is licensed under the GNU Affero General Public License v3.0,
with an explicit additional permission for generated output written into
[LICENSE](LICENSE) itself and carried in every generated file's own header.
The output the compiler produces from your schema files belongs to you, under
whatever terms you choose, including in closed-source projects. That grant is
intentional and permanent. If you modify the compiler and run it as a service
or distribute it, the AGPL's terms apply to those modifications.

Contributing is a separate matter. Contributions are made under a Contributor
Assignment Agreement, described in
[CONTRIBUTING.md](docs/CONTRIBUTING.md#the-contributor-assignment-agreement).

## Author

Glenn Fiedler and Rowan Claude, Más Bandwidth LLC.
