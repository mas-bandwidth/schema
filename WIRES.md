# The two wires

schema generates two different encodings, for two different jobs. Gameplay
traffic and data that outlives builds have opposite requirements, and one
format cannot serve both without losing to a specialist at each.

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
property and older readers keep working. Table writes are zero-allocation in every
target: C++ writes into a caller-owned buffer by construction, Go and C# add append-form
writers (`AppendTableX`) over a reused buffer, and the C#/Go accessor surfaces read
scalars without boxing.

```
package example

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
    health    int32 [min = 0, max = 1000] = 100
    name      string(32)
}
```

## Tables: reflection, relocatability, parallelism

Declaring `table` (instead of `type`) makes a type a table-wire root. It and everything it
references get, **in C++, C# and Go**:

- **Codecs** — `TableWriteX` / `TableReadX`, plain byte code with no runtime dependency.
- **Reflection** — `TableTypeX()` static field descriptors: names, wire ids, bounds,
  declared ranges, enum value names, branch guards. Flat arrays, separate from instance
  data, zero per-instance weight — enough to walk, print, diff, edit or bind any table
  value at runtime with no RTTI, no `System.Reflection`, and no schema files shipped.

**Rust does not have a table backend yet.** `--lang rust` generates the message
wire only; a unit whose types are all tables generates no codecs and no
reflection there. The message wire is complete in all four languages — this gap
is tables specifically.

Generated storage is **relocatable by enforced construction** — trivially copyable,
standard layout, no pointers — so instances can be memcpy'd, memory-mapped, shared across
processes, and **built in parallel across threads then gathered by concatenation**, a
pattern offset-based formats cannot express. The corpus test proves parallel scatter/gather
byte-identical to serial.

`schema pack` compiles directories of JSON instances into single-file binary containers
per a manifest — one data file, native typed readers in every language.

`schema` commands canonically reformat schema files in place. Generated C# is C# 9 /
netstandard2.1-clean and runs on Unity-class runtimes.

---

See [USAGE.md](USAGE.md) for how to declare each, and [SPEC.md](SPEC.md) for
the normative rules.
