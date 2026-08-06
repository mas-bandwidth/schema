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

`notes/` holds extracted API references for the three target runtimes, gathered 2026-08-04 as
design inputs — re-verify against each library's source at implementation time.

## License

AGPL-3.0. See [LICENSE](LICENSE).

## Author

Glenn Fiedler and Rowan Claude, Más Bandwidth LLC.
