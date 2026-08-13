# schema

[![CI](https://github.com/mas-bandwidth/schema/actions/workflows/ci.yml/badge.svg)](https://github.com/mas-bandwidth/schema/actions/workflows/ci.yml)
[![License: AGPL v3](https://img.shields.io/badge/license-AGPL--3.0-blue.svg)](LICENSE)

Write down your data types once and generate code to read and write them in four languages automatically.

```
const MaxHealth = 1000

enum ShipType { Fighter, Corvette, Bomber }

message ShipState {
    ship_type   ShipType
    position    Vec3
    orientation Quat
    health      int32 [min = 0, max = MaxHealth]
    at_rest     bool
    if !at_rest {
        linear_velocity  Vec3
        angular_velocity Vec3
    }
}
```

This declaration compiles to C++, C#, Go and Rust code that 
reads and writes your data types and agree on every bit. Now your native plugin, your Unity client 
and your Go backend all speak the same language.

## Why it exists

Multiplayer games serialize the same data in several languages at once — an
engine client here, a dedicated server there, tools and services around them.
Every way of solving that costs something:

- **Hand-written serializers drift.** Someone widens a field on one side; the
  other side keeps reading the old width, and an afternoon goes to a bug that
  is one bit wide. **Separate read and write paths drift too** — even within a
  single language, the reader and the writer are two expressions of one format
  that nothing forces to agree.
- **A unified read/write function fixes the drift and costs you elsewhere.**
  Templating one function over a read stream and a write stream is a good C++
  answer, and it is still slower than the hand-written pair — and it is not
  available at all in Go, or in most of the languages a game actually has to
  ship in.
- **Solving it with heavier template machinery costs compile time**, in headers
  every translation unit includes.
- **General-purpose formats** fix the drift by paying for it on the wire, in
  bytes and allocations you cannot afford at 60 Hz.

Generating the code takes the fourth path. One declaration produces the reader
*and* the writer, in every language, so they cannot disagree — and because the
format is decided at compile time, what comes out is the straight-line code you
would have hand-written, not an interpreter walking a schema at runtime.

The agreement is proven rather than promised: every build compiles the corpus
in all four languages and byte-compares the results against pinned goldens. If
two languages ever differ by one bit, CI says so before you do.

## Features

- **One declaration, four languages** — C++, C#, Go and Rust, bit-identical on
  the wire, reader and writer generated together so they cannot drift.
- **Bit-packed, not byte-packed** — `[min = 0, max = 1000]` costs 10 bits, not
  4 bytes. Bounds are part of the type, and the wire cost follows from them.
- **Branches that cost nothing** — `if !at_rest { … }` omits whole field groups
  from the wire, back-referencing a bool already sent.
- **Compressed floats** — `[min, max, resolution]` sends a step index, not a
  float. A 0–1 throttle at 0.01 costs 7 bits.
- **Fixed point is a type in the language** — `fixed(48, 16)` is declared like
  any other field, and the compiler owns both the storage and the wire for it.
  This is the one you cannot get by being careful with a format that lacks it:
  floating point is *not* bit-identical across compilers and architectures, so
  a simulation that must agree everywhere — lockstep, rollback, deterministic
  replay, server/client prediction — cannot be built on floats. A fixed value
  is an integer, and integers add the same on every machine. It is also its own
  quantization: storage and wire share one integer domain, so there is no
  separate quantize step to keep in sync.
- **128-bit integers**, ranged like any other, in every target language.
- **Zero allocation, no runtime reflection** — straight-line code reading and
  writing your own buffers.
- **Reads validate, always** — out-of-range values are refused, not clamped or
  trusted. Generated readers are built to face hostile packets.
- **A second, evolution-tolerant wire** for config, assets and settings, with
  reflection and relocatable storage — see [WIRES.md](WIRES.md).
- **`schema pack`** compiles directories of JSON into one binary container,
  validating every value against the schema as it goes.
- **A canonical source format** — every command formats in place, so the
  protocol id hashes one true form.
- **Generated code is yours**, under whatever licence you ship. See
  [License](#license).

## How do I use it?

```
go build -o /usr/local/bin/schema ./cmd/schema

schema check    <dir of .schema files>
schema generate --lang cpp --out <outdir> <dir>
schema pack     <PackManifest.json>
```

**[USAGE.md](USAGE.md)** is the guide — every language feature, with real
examples and the code each one generates.

[SPEC.md](SPEC.md) is the normative reference for when you need the exact
rule.

Building the tests needs the four serialize runtimes checked out beside this
repo — [serialize](https://github.com/mas-bandwidth/serialize),
[serialize.cs](https://github.com/mas-bandwidth/serialize.cs),
[serialize.go](https://github.com/mas-bandwidth/serialize.go),
[serialize.rs](https://github.com/mas-bandwidth/serialize.rs) — then
`make test`. The Makefile's `SERIALIZE*` variables override the locations.

## Documentation

| | |
|---|---|
| **[USAGE.md](USAGE.md)** | Every language feature, with the code it generates. Start here. |
| **[WIRES.md](WIRES.md)** | The two wires, tables, reflection, relocatable storage. |
| **[PERFORMANCE.md](PERFORMANCE.md)** | Generated-code benchmarks across the four languages. |
| **[SPEC.md](SPEC.md)** | The normative reference — grammar, wire law, every edge case. |

## License

**The compiler is AGPL-3.0 — and will stay that way. The code it generates is
yours.**

- The schema compiler (everything in this repository) is licensed under the
  GNU Affero General Public License v3.0. See [LICENSE](LICENSE). If you
  modify the compiler and run it as a service or distribute it, the AGPL's
  terms apply to those modifications.
- **Generated code is explicitly NOT covered by the AGPL.** The output the
  compiler produces from YOUR schema files belongs to YOU, under whatever
  terms you choose — including in closed-source projects. Running the
  compiler over schemas you own does not make your generated serializers,
  table codecs, or reflection descriptors derivative works of the compiler,
  and this grant is intentional and permanent: schema is meant to be useful
  to people shipping proprietary software. Only the compiler itself is open
  source.

## Author

Glenn Fiedler and Rowan Claude, Más Bandwidth LLC.
