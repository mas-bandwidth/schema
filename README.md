# schema

[![CI](https://github.com/mas-bandwidth/schema/actions/workflows/ci.yml/badge.svg)](https://github.com/mas-bandwidth/schema/actions/workflows/ci.yml)
[![License: AGPL v3](https://img.shields.io/badge/license-AGPL--3.0-blue.svg)](LICENSE)

Write your data types once and generate code to read and write them in five languages.

```
package example

const MaxHealth = 1000

enum ShipType { Fighter, Corvette, Bomber }

type Vec3 {
    x float64
    y float64
    z float64
}

message ShipState {
    ship_type ShipType
    position  Vec3
    health    int32 [min = 0, max = MaxHealth]
    at_rest   bool
    if !at_rest {
        velocity Vec3
    }
}
```

This declaration compiles to C, C++, C#, Go and Rust code that 
reads and writes your data types and agrees on every bit. Now your native plugin, your Unity client, your Go backend, and your tooling all speak the same language.

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
- **General-purpose formats** fix the drift by paying for it on the wire.
  On one representative gameplay message that is **28 bytes against Cap'n Proto's
  52, Protobuf's 56 and FlatBuffers' 72** — every number measured by running the
  real encoder, [reproducible from COMPARISON.md](COMPARISON.md).

Generating the code takes the fourth path. One declaration produces the reader
*and* the writer, in every language, so they cannot disagree — and because the
format is decided at compile time, what comes out is the straight-line code you
would have hand-written, not an interpreter walking a schema at runtime.

## Features

- **One declaration, five languages** — C, C++, C#, Go and Rust, bit-identical on
  the wire, reader and writer generated together so they cannot drift.
- **Bit-packed, not byte-packed** — `[min = 0, max = 1000]` costs 10 bits, not
  4 bytes. Bounds are part of the type, and the wire cost follows from them.
- **Branches that cost nothing** — `if !at_rest { … }` omits whole field groups
  from the wire, back-referencing a bool already sent.
- **Compressed floats** — `[min, max, resolution]` sends a step index, not a
  float. A 0–1 throttle at 0.01 costs 7 bits.
- **Fixed point is a type in the language** — `fixed(48, 16)` is declared like
  any other field, and the compiler owns both the storage and the wire for it.
- **128-bit integers**, ranged like any other, in every target language.
- **Zero allocation, no runtime reflection** — straight-line code reading and
  writing your own buffers.
- **Reads validate, always — in every language.** Out-of-range values are
  refused, not clamped or trusted.
- **A second, evolution-tolerant wire** for config, assets and settings, with
  reflection and relocatable storage — in all five languages; see
  [WIRES.md](WIRES.md).
- **`schema pack`** compiles directories of JSON into one binary container,
  validating every value against the schema as it goes.
- **Canonical source format** — every command formats in place.
- **Generated code is yours**, under whatever licence you ship. See
  [License](#license).

## How do I use it?

```
go build -o /usr/local/bin/schema ./cmd/schema

schema check    <dir of .schema files>
schema generate --lang c|cpp|cs|go|rust --out <outdir> <dir>
schema pack     <PackManifest.json>
```

**[USAGE.md](USAGE.md)** is the guide — every language feature, with real
examples and the code each one generates.

[SPEC.md](SPEC.md) is the normative reference for when you need the exact
rule.

Building the tests needs the five serialize runtimes checked out beside this
repo — [serialize](https://github.com/mas-bandwidth/serialize),
[serialize.c](https://github.com/mas-bandwidth/serialize.c),
[serialize.cs](https://github.com/mas-bandwidth/serialize.cs),
[serialize.go](https://github.com/mas-bandwidth/serialize.go),
[serialize.rs](https://github.com/mas-bandwidth/serialize.rs) — then
`make test`. The Makefile's `SERIALIZE*` variables override the locations.

## Future work: entropy coding

Bit-packing spends **exactly** the bits a declared range implies: a field in
`[0, 1000]` costs 10 bits whether its value is 3 or 997. That is optimal only
if every value in the range is equally likely — and in real game data they
never are. Positions cluster, healths sit near full, most deltas are small.

The planned next step is **rANS entropy coding**, which is mathematically
equivalent to arithmetic/range coding but far faster on modern hardware: decode
is a multiply and a table lookup with no division in the fast path, and
because several coder states can be interleaved over one buffer, the serial
dependency that limits a range coder disappears and the decoder saturates the
pipeline.

**A schema compiler is an unusually good place to put it.** Entropy coding only
pays if you have a probability model, and that is normally the hard part — you
have to describe your data twice, once as types and again as statistics. Here
the compiler already knows every field's exact domain, so static per-field
models can be compiled straight from the schema and shipped inside the
generated code, in every language, with the same cross-language byte-identity
guarantee the current wire has.

It will be **optional and opt-in, per field** — bit-packing stays the default,
and nothing you have today changes shape because a coder exists. The goal is
narrow and specific: the next step in **CPU-efficient compression for wire
types in multiplayer games**, where the budget is measured in microseconds per
packet and a general-purpose compressor is not in the running.

It is **researched and not implemented** — the design record, including the
LIFO constraint rANS imposes on a single-pass serializer and an honest read of
the patent situation, lives in
[serialize](https://github.com/mas-bandwidth/serialize)'s notes. It is on the
roadmap, not in the box.

## Documentation

| Document | What's in it |
|---|---|
| **[USAGE.md](USAGE.md)** | Every language feature, with the code it generates. Start here. |
| **[WIRES.md](WIRES.md)** | The two wires, tables, reflection, relocatable storage. |
| **[PERFORMANCE.md](PERFORMANCE.md)** | Generated-code benchmarks across the five languages. |
| **[SPEC.md](SPEC.md)** | The normative reference — grammar, wire law, every edge case. |
| **[COMPARISON.md](COMPARISON.md)** | The same message in schema, Cap'n Proto, Protobuf and FlatBuffers — 28 vs 52 vs 56 vs 72 bytes, measured, with a script to re-run it. |
| **[FAQ.md](FAQ.md)** | Isn't this just FlatBuffers / Protobuf / Cap'n Proto? And other blunt questions. |
| **[VERSIONING.md](VERSIONING.md)** | What a version number promises — chiefly that a 1.x upgrade will not move your wire. |
| **[CONTRIBUTING.md](CONTRIBUTING.md)** | How to build it, the gates a change has to pass, and what a golden change means. |
| **[SECURITY.md](SECURITY.md)** | The threat model, and how to report a vulnerability privately. |

## License

**The compiler is AGPL-3.0 — and will stay that way. The code it generates is
yours.**

- The schema compiler (everything in this repository) is licensed under the
  GNU Affero General Public License v3.0, with an **explicit additional
  permission for generated output** written into [LICENSE](LICENSE) itself —
  not only described here. Every generated file also carries that statement in
  its own header. If you
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
