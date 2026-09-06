# schema

[![CI](https://github.com/mas-bandwidth/schema/actions/workflows/ci.yml/badge.svg)](https://github.com/mas-bandwidth/schema/actions/workflows/ci.yml)
[![License: AGPL v3](https://img.shields.io/badge/license-AGPL--3.0-blue.svg)](LICENSE)

**Schema. The data language for games.**

Schema is a data language where you define your constants, enums, flags, types and tables for your game. 

From this one definition, schema generates code in multiple languages: C, C++, C#, Dart, Elixir, Go, Java, JavaScript and Rust.

Schema is currently under active development. See **[ROADMAP.md](ROADMAP.md)** for details.

If this work helps you, please support it: **[Become a supporter](https://www.patreon.com/MasBandwidth/membership)**

## Design

Schema is designed to support the following use-cases common in game development:

* Packets sent between a client and a server.
* Messages sent between your server and backend.
* Save game files that must load in a future game build.
* Game assets cooked to an efficient runtime binary format.
* Render data written in one language and read in another - sixty times per-second.

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

Write your data types once and generate bit-packed serialization code to read and write them. Best for client/server messages and state where the client speaks the same binary protocol as the server or won't be allowed to connect.

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

Measured by [the benchmark](bench/). One 438-byte packet exercising every construct. 

## Install and build

```
make                # builds the compiler at bin/schema
bin/schema check    <dir of .schema files>
bin/schema generate --lang c|cpp|cs|dart|elixir|go|java|js|rust --out <outdir> <dir>
bin/schema fmt      <dir of .schema files>
```

`fmt` is the only command that writes a `.schema` file. Every other command
reads your schema files and leaves them alone, so a read-only checkout, a
sandboxed build and an editor integration all work.

## Documentation

| Document | What's in it |
|---|---|
| **[ROADMAP.md](ROADMAP.md)** | Where schema stands, feature by feature and language by language, what we are building next, and how to support the work. |
| **[TUTORIAL.md](docs/TUTORIAL.md)** | Fourteen parts, from an empty directory to a program that uses every feature. Start here. |
| **[USAGE.md](docs/USAGE.md)** | Every language feature, with the code it generates. |
| **[SPEC.md](docs/SPEC.md)** | The normative reference for the type wire: grammar, wire law, every edge case. |
| **[SPEC-TABLES.md](docs/SPEC-TABLES.md)** | The normative reference for tables: the wire, the cook, the block form, reflection, the build version. |

Beside them: [PORTING.md](docs/PORTING.md) (the techniques register: every
method and instrument a table backend carries, with a cell per language and a
gate), [PERFORMANCE.md](docs/PERFORMANCE.md),
[COMPARISON-TABLES.md](docs/COMPARISON-TABLES.md) (tables against FlatBuffers and
Protobuf, feature by feature, with the verdict on every gap),
[COMPARISON.md](docs/COMPARISON.md) (the same packet against Cap'n Proto, Protobuf
and FlatBuffers), [COMPETITION.md](docs/COMPETITION.md) (the standing comparison
against Protocol Buffers, FlatBuffers, Cap'n Proto and Avro),
[FAQ.md](docs/FAQ.md), [VERSIONING.md](docs/VERSIONING.md),
[CONTRIBUTING.md](docs/CONTRIBUTING.md) and [SECURITY.md](docs/SECURITY.md).

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

Glenn Fiedler, Rowan Claude and Stella Codex, Más Bandwidth LLC.
