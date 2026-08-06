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
corpus benchmarks; the mixed-dispatch batch shown separately because its ranking inverts.
Apple M2, quiet host, 2026-08-06 baseline — full tables, raw CSVs and methodology in
[bench/results/](bench/results/):

| backend | write | read | batch write | batch read |
|---|---:|---:|---:|---:|
| C++ | 100% | 100% | 100% | 100% |
| Rust | 173% | 243% | 159% | 148% |
| C# | 238% | 306% | 161% | **80%** |
| Go | 300% | 719% | 236% | 99% |

The spread behind the medians is wide and systematic: C++'s lead is an inlining margin that
grows as messages shrink (on 6–10 byte messages the others take 600–1600% of C++'s time), while
on the mixed batch the gap closes and C# reads it in 80% of C++'s time. Numbers move as
optimization lands — the Go read median predates a read-path fix measured at +14–69%, and the
C++ batch read has a 2x fix in flight — so treat the table as a dated snapshot, not a verdict.

`notes/` holds extracted API references for the three target runtimes, gathered 2026-08-04 as
design inputs — re-verify against each library's source at implementation time.

## License

AGPL-3.0. See [LICENSE](LICENSE).

## Author

Glenn Fiedler and Rowan Claude, Más Bandwidth LLC.
