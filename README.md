# schema

[![CI](https://github.com/mas-bandwidth/schema/actions/workflows/ci.yml/badge.svg)](https://github.com/mas-bandwidth/schema/actions/workflows/ci.yml)
[![License: AGPL v3](https://img.shields.io/badge/license-AGPL--3.0-blue.svg)](LICENSE)

**Schema. The data schema language for games**.

If you write a game in more than one language, or ship a client and server that
have to agree on every bit, schema is a language that will help you do this
without ever having to hand-code definitions in each language ever again.

**schema** is meant to serve all your needs for data types across all languages used when developing a game:

* The packet between a client and a server, where every bit counts and both sides ship together.
* The message between a tool and a backend that ship months apart.
* The save game that has to load in a build its writer never saw.
* The asset file the tools build and cook to an efficient runtime binary format per-build version.
* The render data C++ writes and C# reads sixty times a second.

One system does all of it, so you never end up with schema for the packets and something else for everything else.

If this work helps you, please support it: **[Become a supporter](https://www.patreon.com/MasBandwidth/membership)**

## Features

1. Define constants, enums, flags, types and tables in one language.
2. Generate fast bit-packed serialization for struct types that don't need versioning (eg. client/server messages and state)
3. Generate versioned for messages, data, assets, save game files and everything else!
4. Supports recursive table definitions (pointer to table in table) for full data structure support.
5. Cooks to fast binary formats at runtime for tool pipelines and asset loading.

Supported languages: C, C++, C#, Rust, Golang, Java, JavaScript, Dart and Elixir.

## Examples

Write your data types once and generate code to read and write them in nine languages.

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

This declaration compiles to C, C++, C#, Go, Java, Rust, JavaScript, Dart and Elixir code that 
reads and writes your data types and agrees on every bit. Now your native plugin, your Unity client, your Go backend, your browser client, and your tooling all speak the same language.

- **Bit-packed, not byte-packed** — `| min = 0, max = 1000` costs 10 bits, not
  4 bytes. Bounds are part of the type, and the wire cost follows from them.
- **Branches that cost nothing** — `if !at_rest { … }` omits whole field groups
  from the wire, back-referencing a bool already sent.
- **Compressed floats** — `| min, max, resolution` sends a step index, not a
  float. A 0–1 throttle at 0.01 costs 7 bits.
- **Fixed point is a type in the language** — `fixed(48, 16)` and its unsigned
  sibling `ufixed(48, 16)` are declared like any other field, and the compiler
  owns both the storage and the wire for them.
- **128-bit integers**, ranged like any other, in every target language.
- **Zero allocation, no runtime reflection** — straight-line code reading and
  writing your own buffers.
- **Reads validate, always — in every language.** Out-of-range values are
  refused, not clamped or trusted.
- **Relocatable by construction** — generated types are trivially copyable
  and standard-layout, so raw-struct blobs and parallel scatter/gather are
  safe by design; see [The wire](docs/USAGE.md#the-wire).

Nothing on the wire carries a version: the unit's protocol id is the
handshake, and two sides at different ids do not exchange a byte.

```cpp
if ( peer_id != example::ProtocolId )
    return false;                                   // different build: do not talk
example::ShipState state;
serialize::ReadStream stream( buffer, bytes );
if ( !example::ReadShipState( stream, state ) )
    return false;                                   // out of range: refused, not clamped
```

Depth: [USAGE.md — The protocol id](docs/USAGE.md#the-protocol-id); SPEC.md §3.

## Messages between tools, backends and websites

Tools, the backend and the website each ship on their own schedule. A file
written by last month's tool has to load in this month's backend. That is
the job people reach for Protocol Buffers to do, and schema does it with a
`table`.

A table rides a second wire. Each field carries an id, a kind and a length,
so a reader that does not know a field skips it, a field that is missing
takes its declared default, a field that was renamed is found under its old
name, and a value out of range is clamped. Every one of those events is
counted in a read report. Nothing is fatal in either direction.

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

```csharp
long size = Schema.BuildRequestMeasure(request);       // exact, writes nothing
byte[] buffer = new byte[size];
Schema.BuildRequestSave(request, buffer);

TableReport report = new TableReport();
if (!Schema.BuildRequestLoad(request, bytes, report))
    return false;                                      // framing damage only
// report.Unknown, .KindMismatch and .Clamped name every difference from the
// writer's generation; the value is usable either way
```

Read more: [tables](docs/USAGE.md#tables-data-that-outlives-builds) in USAGE, and SPEC-TABLES.md §4.

## Save games

A save file is a table too, so it gets the same tolerance. It also gets a
guard the wire cannot give: a committed baseline file that refuses, at
compile time, the two edits that change what old data means without changing
a byte of it. Changing a field's default is one. Reordering a `flags` variant
is the other. When you mean the change, you record it with a reason, and the
reason stays in the file.

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

```cpp
// schema tables-baseline --update --reason "first baseline before 1.0 ships" .
TableReport report;
if ( !SaveGameLoad( save, bytes, length, &report ) )
    return false;      // only structural damage stops a load
// an older build's file: unknown fields skipped, absent fields at their
// declared defaults, `velocity` found again under its new name
```

Read more: [the tables baseline](docs/USAGE.md#the-tables-baseline-catching-the-edits-the-wire-cannot-report) in USAGE, and SPEC-TABLES.md §18.

## Assets cooked for a build

Tools build the assets. The game should not parse them at load. It should
map the file and point at it.

`schema cook` writes a table's data in the exact memory layout of one build,
in that build's byte order. Opening it is a header check and a cast: magic,
byte order, build version, lengths, alignment. Nothing is walked, so a
gigabyte opens as fast as a kilobyte. A cook only opens in the build it was
cooked for. Any other build gets NULL and loads the wire instead, which every
build can read.

The cook is a preview in this release.

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
schema build-version .                          # 0x6d7787f793cf0d6a — the store key
```

```cpp
const MeshCatalogue * catalogue = MeshCatalogueOpen( mapped, length );
if ( catalogue == NULL )
    return NULL;      // not this build's cook: fall back to a wire load
return catalogue;     // nothing parsed, nothing allocated, nothing walked
```

Read more: [the cooked form](docs/USAGE.md#the-cooked-form-point-at-a-file-instead-of-parsing-it) in USAGE, and SPEC-TABLES.md §7 and §20.

## Render data from C++ to C#

Every frame, C++ writes a large block of render data and C# reads it. This is
the seam between a native plugin and Unity, and neither side can afford a
copy or a parse.

Every fixed table has a block form. Its arrays are laid out at a fixed pitch
with the offsets at the front. C++ fills it from as many threads as it likes,
with no lock and no allocation. C# opens it and reads the rows as spans over
the same memory. Both sides are generated from the one declaration, and the
row sizes and field offsets are asserted at compile time in both languages.
A field that moves is a build error, not a garbled frame.

```
package renderdemo

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

```csharp
if (!RenderFrameBlock.Open(out RenderFrameBlock block, pointer, bytes))
    return;                                              // wrong build, or misaligned

ReadOnlySpan<RenderShipRow> ships = block.ShipsSpan;     // one contiguous reinterpret
foreach (ref readonly RenderShipRow ship in block.Ships)
    Draw(ship);
```

Read more: [the block form](docs/USAGE.md#the-block-form-rows-another-language-points-at) in USAGE, and SPEC-TABLES.md §19.

## JSON packed into binary tables

Data gets authored or exported as JSON. It needs to become a compact binary
that several languages can read.

`schema pack` takes a directory of JSON files shaped like the table and
writes the table's wire bytes, nothing else around them. `schema unpack`
writes the JSON back out. Both fail the build when the read report is not
silent, so drift between the JSON and the schema does not slip through.

```
package configdemo

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

```cpp
TableReport report;
GameConfigLoad( config, bytes, length, &report );            // the packed bytes
GameConfigFromJson( config, text, text_bytes, &report );     // or one text, in place
```

Read more: [packing a directory into a table](docs/USAGE.md#packing-a-directory-into-a-table) in USAGE, and SPEC-TABLES.md §16 and §17.

## Editors and debug views

An editor wants to show any table as a property tree. A debug view wants to
inspect what the game is holding this frame.

Every table comes with reflection descriptors beside its code: each field's
name, type, offset, range, and the names of every enum and union variant. A
tool walks a table it has never seen. A debug view walks the live instance in
memory. There is no RTTI and no schema file at run time.

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

```cpp
const TableTypeInfo * type = ShipConfigTableType();
for ( int32_t i = 0; i < type->num_fields; i++ )
{
    const TableFieldInfo & f = type->fields[i];
    printf( "%s %s @%u", f.name, f.type_name, f.offset );
    if ( f.has_range ) printf( " [%g, %g]", f.range_min, f.range_max );
    if ( f.enum_name ) printf( " %s", f.enum_name( 1 ) );
    printf( "\n" );
}
```

Read more: [walking fields at runtime](docs/USAGE.md#walking-fields-at-runtime) in USAGE, and SPEC-TABLES.md §8.

## Which one to use

| you want | declare | you get |
|---|---|---|
| the smallest packet, both ends ship together | `type` | bitpacked wire, protocol id |
| data that survives schema changes | `table` | tolerant wire, read report |
| structures with pointers, trees, graphs | `table` with `*T` fields | the same wire, built in bulk through your allocator |
| rows shared between C++ and C# every frame | any fixed `table` | the block form, pointed at |
| a file the game maps and points at | `table`, then `schema cook` | the cook, one build |

A `type` and a fixed `table` are the same struct. The difference is the wire
around it. The type wire is bitpacked and tied to a build. The table wire
spends bytes on ids and lengths so any build can read any data. The block
form and the cook are accelerators beside the table wire, not replacements
for it.

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

## Languages

| language | runtime | type wire | tables | block form | cook reader | JSON text form |
|---|---|---|---|---|---|---|
| C | [serialize.c](https://github.com/mas-bandwidth/serialize.c) | yes | — | — | — | — |
| C++ | [serialize](https://github.com/mas-bandwidth/serialize) | yes | fixed and variable | yes | yes | yes |
| C# | [serialize.cs](https://github.com/mas-bandwidth/serialize.cs) | yes | fixed | yes | yes | — |
| Dart | none, self-contained | yes | — | — | — | — |
| Elixir | none, self-contained | yes | — | — | — | — |
| Go | [serialize.go](https://github.com/mas-bandwidth/serialize.go) | yes | — | — | — | — |
| Java | none, self-contained | yes | — | — | — | — |
| JavaScript | [serialize.js](https://github.com/mas-bandwidth/serialize.js) | yes | — | — | — | — |
| Rust | [serialize.rs](https://github.com/mas-bandwidth/serialize.rs) | yes | — | — | — | — |

The type wire is bit-identical across all nine languages, held by shared
golden bytes in CI. A language without a tables column refuses a unit that
declares a table, by name.

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
| **[USECASES.md](docs/USECASES.md)** | What people use schema for, in plain words. |
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

## Author

Glenn Fiedler and Rowan Claude, Más Bandwidth LLC.
