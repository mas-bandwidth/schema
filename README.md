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

> **Status: v1 implementation underway** (started 2026-08-05). The C++ and Go backends are
> live: `make` builds the compiler, generates C++ headers and a Go package from
> [`examples/`](examples/) into `generated/`, and runs the test suite — the C++ binaries
> (both message representations plus a randomized round-trip suite), the Go wire test
> (byte-compares the generated Go's output against the C++-pinned wire goldens —
> cross-language wire identity is a standing gate), the break-the-language diagnostics
> suite, and the source/id/wire golden pins. Landed so far: scanner, parser,
> resolver/checker, protocol id, schemafmt (every command formats in place before
> processing), and full C++ + Go emission — storage, split `Write`/`Read` per type and
> message, the message dispatch surfaces (C++ tagged union or opt-in `std::variant`; Go
> interface + type switch), the object view families with `Quantize`/`Unquantize`, and
> zero initialization with specified defaults. Next: the Rust backend, then C#. Start
> with [SPEC.md](SPEC.md).

Versioning is by **protocol id** — a hash of the schema itself, computed by the compiler. Two
sides at the same protocol id speak identical bits; there is no versioning overhead on the wire.

`notes/` holds extracted API references for the three target runtimes, gathered 2026-08-04 as
design inputs — re-verify against each library's source at implementation time.

## License

AGPL-3.0. See [LICENSE](LICENSE).

## Author

Glenn Fiedler and Rowan Claude, Más Bandwidth LLC.
