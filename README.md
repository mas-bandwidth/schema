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
corpus benchmarks; the mixed-dispatch batch shown separately. Apple M2, quiet host, 2026-08-07
(v5 — the optimization program's closing pass: the three per-language lanes composed on top of
the post-restrict C++ ceiling — C# batch opt-in + bulk-bytes (schema#12), the Go write program
(serialize.go#19 + schema#13), and the Rust emitter levers (schema#14) — with zero rows
regressing beyond spread anywhere) — full tables, raw CSVs and methodology in
[bench/results/](bench/results/):

| backend | write | read | batch write | batch read |
|---|---:|---:|---:|---:|
| C++ | 100% | 100% | 100% | 100% |
| Rust | 177% | 204% | 121% | 153% |
| C# | 199% | 214% | 140% | 175% |
| Go | 323% | 387% | 204% | 198% |

The spread behind the medians is wide and systematic: C++'s lead is an inlining margin that
grows as messages shrink — on 6–10 byte messages the others take roughly 3–12x C++'s
time. C++ leads every column; Rust is the nearest chaser overall (it briefly held write parity
at the v3 mains, until the C++ writer's restrict pass moved the reference), with C# two points
behind on the write median after its batch opt-in landed. An earlier draft of this table showed C# winning the batch read, and that turned out
to be a benchmark-harness artifact, which is why every number here is gated on wire-golden
verification before it is believed. The remaining gaps are decomposed, cause by cause, in
[bench/results/2026-08-07-gap-ledger.md](bench/results/2026-08-07-gap-ledger.md) — treat the
table as a dated snapshot, not a verdict.

**And it is a snapshot of a (compiler, microarchitecture) pair, not of the languages.**
The x86 leg (AMD EPYC 9124 / Zen 4, serial on a reserved core, measured 2026-08-07,
harvested 2026-08-11) tells a different story: the C++ writer's restrict win — the
biggest C++ lever on arm64/apple-clang — measured **1.00x under both g++-13 and
clang-18** there, so against g++ the ports sit near write parity while reads sit further
away; and clang-18 beats g++ by up to 4.3x on tiny-message writes, which moves every
relative cell by double digits when the reference compiler changes. Same code, same wire,
different target, different table:

| backend (EPYC, vs g++ 13.3) | write | read | batch write | batch read |
|---|---:|---:|---:|---:|
| C++ | 100% | 100% | 100% | 100% |
| Rust | 108% | 274% | 103% | 125% |
| C# * | 137% | 198% | 120% | 150% |
| Go | 177% | 370% | 196% | 108% |

\* C# from a labelled steady-state diagnostic — the default-config run surfaced a benching
law worth knowing: a tiered-JIT runtime benched on a single shared core measures tier-up
contention, not generated code. Full tables, both compilers, the restrict A/B and the
artifact record: [bench/results/2026-08-07-four-language-v5-epyc.md](bench/results/2026-08-07-four-language-v5-epyc.md).

`notes/` holds extracted API references for the three target runtimes, gathered 2026-08-04 as
design inputs — re-verify against each library's source at implementation time.

## License

AGPL-3.0. See [LICENSE](LICENSE).

## Author

Glenn Fiedler and Rowan Claude, Más Bandwidth LLC.
