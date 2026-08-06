# schema

**schema** is a language for describing bitpacked network data, and a compiler — written in Go —
that translates `*.schema` files into generated C++, C#, Go and Rust serialization code targeting
[serialize](https://github.com/mas-bandwidth/serialize),
[serialize.cs](https://github.com/mas-bandwidth/serialize.cs),
[serialize.go](https://github.com/mas-bandwidth/serialize.go) and
[serialize.rs](https://github.com/mas-bandwidth/serialize.rs).

Define your types and their wire encoding once; get minimal, straight-line read/write code for
every platform, byte-identical on the wire across all four — with none of the compile-time cost
of doing this inside a C++ compiler with template metaprogramming.

The idea is extracted from
[serialize.modern](https://github.com/mas-bandwidth/serialize.modern)'s compile-time schema
language, moved out of the templates and into an external compiler.

> **Status: all four v1 backends — C++, Go, Rust and C# — are live**, and
> cross-language wire identity is a standing test gate. Start with [SPEC.md](SPEC.md).

Versioning is by **protocol id** — a hash of the schema itself, computed by the compiler. Two
sides at the same protocol id speak identical bits; there is no versioning overhead on the wire.

## Performance

Generated-code performance of the four backends as **time taken relative to C++** — C++ is
100%, higher is slower: 200% means the same work takes twice as long as C++. Medians across the
corpus benchmarks; the mixed-dispatch batch shown separately. Apple M2, quiet host, 2026-08-06
(v2 harness) — full tables, raw CSVs and methodology in [bench/results/](bench/results/):

| backend | write | read | batch write | batch read |
|---|---:|---:|---:|---:|
| C++ | 100% | 100% | 100% | 100% |
| Rust | 174% | 259% | 150% | 326% |
| C# | 249% | 323% | 179% | 189% |
| Go | 297% | 594% | 230% | 216% |

The spread behind the medians is wide and systematic: C++'s lead is an inlining margin that
grows as messages shrink — on 6–10 byte messages the others take roughly 300–1500% of C++'s
time. C++ leads every column; an earlier draft of this table showed C# winning the batch read,
and that turned out to be a benchmark-harness artifact, which is why every number here is gated
on wire-golden verification before it is believed. Numbers move as optimization lands — several
measured fixes (Go reads, Rust dispatch, C++ byte paths) are in review — so treat the table as
a dated snapshot, not a verdict.

`notes/` holds extracted API references for the three target runtimes, gathered 2026-08-04 as
design inputs — re-verify against each library's source at implementation time.

## License

AGPL-3.0. See [LICENSE](LICENSE).

## Author

Glenn Fiedler and Rowan Claude, Más Bandwidth LLC.
