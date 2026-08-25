# The wire

schema generates one encoding, for one job: realtime network data, bitpacked
tight, decided entirely at compile time.

**Messages** ride the bitpacked wire, targeting
[serialize](https://github.com/mas-bandwidth/serialize),
[serialize.cs](https://github.com/mas-bandwidth/serialize.cs),
[serialize.go](https://github.com/mas-bandwidth/serialize.go) and
[serialize.rs](https://github.com/mas-bandwidth/serialize.rs). Versioning is by
**protocol id** — a hash of the schema itself. Two sides at the same protocol
id speak identical bits; there is no versioning overhead on the wire, no
optional-field tags, no evolution machinery. That is a deliberate position,
not a missing feature: for realtime traffic, client and server ship together
and agree exactly, or they refuse to talk — see
[FAQ.md](FAQ.md) for why this wins against evolution-tolerant formats at this
job.

```
package example

enum ShipType { Fighter, Corvette, Bomber }

type Vector3
{
    x float64
    y float64
    z float64
}

message ShipCreate
{
    ship_type ShipType
    position  Vector3
}
```

## Relocatable storage, parallelism

Generated storage is **relocatable by enforced construction** — trivially
copyable, standard layout, no pointers — so instances can be memcpy'd,
memory-mapped, shared across processes, and **built in parallel across
threads then gathered by concatenation**, a pattern offset-based formats
cannot express. The corpus test proves parallel scatter/gather byte-identical
to serial.

`schema` commands canonically reformat schema files in place. Generated C# is
C# 9 / netstandard2.1-clean and runs on Unity-class runtimes.

---

See [USAGE.md](USAGE.md) for every declaration, and [SPEC.md](SPEC.md) for
the normative rules.
