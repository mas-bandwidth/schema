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
3. _coming soon_ -- Generate versioned tables for messages, data, assets, save game files and everything else.
4. _coming soon_ -- Tables can point at tables, so trees and graphs are tables too.
5. _coming soon_ -- Cook tables to a binary format the game runtime loads for tool pipelines and asset loading.

Supported languages: C, C++, C#, Dart, Elixir, Go, Java, JavaScript and Rust.

## Example

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

## Install and build

```
make                # builds the compiler at bin/schema
bin/schema check    <dir of .schema files>
bin/schema generate --lang c|cpp|cs|dart|elixir|go|java|js|rust --out <outdir> <dir>
```
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

## Author

Glenn Fiedler and Rowan Claude, Más Bandwidth LLC.
