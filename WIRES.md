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
references get, **in five of the six languages** (C, C++, C#, Go and Rust —
JavaScript's message wire landed first; its table wire does not exist yet):

- **Codecs** — `TableWriteX` / `TableReadX`, plain byte code with no runtime dependency.
- **Reflection** — `TableTypeX()` static field descriptors: names, wire ids, bounds,
  declared ranges, enum value names, branch guards. Flat arrays, separate from instance
  data, zero per-instance weight — enough to walk, print, diff, edit or bind any table
  value at runtime with no RTTI, no `System.Reflection`, and no schema files shipped.

Rust reached parity here in v1.5.0; before that, `--lang rust` emitted the
message wire only. Its codecs are `table_write_x` / `table_read_x` and its
descriptors `table_type_x()`, following Rust naming rather than the other
three's — the bytes are identical, and the cross-language goldens are what say
so.

Generated storage is **relocatable by enforced construction** — trivially copyable,
standard layout, no pointers — so instances can be memcpy'd, memory-mapped, shared across
processes, and **built in parallel across threads then gathered by concatenation**, a
pattern offset-based formats cannot express. The corpus test proves parallel scatter/gather
byte-identical to serial.

`schema pack` compiles directories of JSON instances into single-file binary containers
per a manifest — one data file, native typed readers in every language.

`schema` commands canonically reformat schema files in place. Generated C# is C# 9 /
netstandard2.1-clean and runs on Unity-class runtimes.

## Renaming a table field changes its identity

A table field's wire identity is `fold16(fnv1a32(name))` — **the hash of its
name**. That is what lets unknown fields skip and removed fields default
without a hand-maintained field-number registry.

The cost is the mirror image: **renaming a field is a wire-breaking change.**
Rename `hp` to `health` and every already-written container still carries the
old id, so the new reader does not recognise it and the field defaults. The
data is not corrupted — it is silently forgotten, which is worse to diagnose.

There is no `[id = N]` attribute to pin an identity independently of the name.
If you need to rename a field whose data already exists in the wild, the
migration is yours to write: read with the old schema, write with the new one.

Two practical consequences:

- **Name table fields as if renaming them is expensive**, because it is.
- Ids are 16-bit, and the compiler refuses a collision at compile time rather
  than letting two fields share an id. That check is the right one, but note
  what resolving a collision costs: renaming *either* field changes *that*
  field's identity, so a collision introduced by a new field can force a
  data-breaking rename of an old one. Wide tables make this more likely — at
  100 fields the birthday probability of some collision is a few percent.

None of this applies to the message wire, which has no field identity at all.

---

See [USAGE.md](USAGE.md) for how to declare each, and [SPEC.md](SPEC.md) for
the normative rules.
