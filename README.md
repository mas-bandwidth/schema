# schema

**schema** is a language for describing game network data, and a compiler — written in Go —
that translates `*.schema` files into generated C++, C#, Go and Rust code. Define your types
and their wire encoding once; get minimal, straight-line, allocation-free code for every
platform, byte-identical on the wire across all four languages.

Start with [SPEC.md](SPEC.md).

## Two wires

**Messages** ride the bitpacked realtime wire, targeting
[serialize](https://github.com/mas-bandwidth/serialize),
[serialize.cs](https://github.com/mas-bandwidth/serialize.cs),
[serialize.go](https://github.com/mas-bandwidth/serialize.go) and
[serialize.rs](https://github.com/mas-bandwidth/serialize.rs). Versioning is by
**protocol id** — a hash of the schema itself. Two sides at the same protocol id speak
identical bits; there is no versioning overhead on the wire.

**Tables** ride the table wire: an evolution-tolerant encoding for data that outlives
builds — config, assets, settings. Fields are identified by name hash; defaults elide;
unknown fields skip; removed fields default; changed types skip instead of misdecoding;
out-of-range values clamp — every event counted in a report, and reads validate everything,
in every language, so table data is safe to accept on untrusted surfaces. Add or remove a
property and older readers keep working.

```
enum ShipType { Fighter, Corvette, Bomber }

type Vector3 {
    x float64
    y float64
    z float64
}

message ShipCreate {
    ship_type ShipType
    position  Vector3
}

table ShipConfig {
    ship_type ShipType
    health    float32 [min = 0, max = 1000] = 100.0
    name      string(32)
}
```

## Tables: reflection, relocatability, parallelism

Declaring `table` (instead of `type`) makes a type a table-wire root. It and everything it
references get, in every generated language:

- **Codecs** — `TableWriteX` / `TableReadX`, plain byte code with no runtime dependency.
- **Reflection** — `TableTypeX()` static field descriptors: names, wire ids, bounds,
  declared ranges, enum value names, branch guards. Flat arrays, separate from instance
  data, zero per-instance weight — enough to walk, print, diff, edit or bind any table
  value at runtime with no RTTI, no `System.Reflection`, and no schema files shipped.

Generated storage is **relocatable by enforced construction** — trivially copyable,
standard layout, no pointers — so instances can be memcpy'd, memory-mapped, shared across
processes, and **built in parallel across threads then gathered by concatenation**, a
pattern offset-based formats cannot express. The corpus test proves parallel scatter/gather
byte-identical to serial.

`schema pack` compiles directories of JSON instances into single-file binary containers
per a manifest — one data file, native typed readers in every language.

## Getting started

```
go build -o /usr/local/bin/schema ./cmd/schema
schema check  <dir of .schema files>
schema generate --lang cpp --out <outdir> <dir>
schema pack   <PackManifest.json>
```

`schema` commands canonically reformat schema files in place; the protocol id is a hash of
the canonical form. Generated C# is C# 9 / netstandard2.1-clean and runs on Unity-class
runtimes.

## Performance

Generated-code performance as time relative to C++ (100%; higher is slower), medians across
the corpus on Apple M2:

| backend | write | read | batch write | batch read |
|---|---:|---:|---:|---:|
| C++ | 100% | 100% | 100% | 100% |
| Rust | 177% | 204% | 121% | 153% |
| C# | 199% | 214% | 140% | 175% |
| Go | 323% | 387% | 204% | 198% |

Relative numbers move with compiler and microarchitecture — treat the table as a dated
snapshot, not a verdict. Full tables, an x86 leg, and per-gap analysis:
[bench/results/](bench/results/).

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
