# AGENTS.md — generated map

Do not edit. `make map` regenerates this file. Rules: [docs/CONTRIBUTING.md](docs/CONTRIBUTING.md). Glenn, 2026-09-18: AGENTS.md alone — no `CLAUDE.md`, no pointer, no symlink.

Schema is the data language for games. One definition compiles to bit-packed codecs in C, C++, C#, Dart, Elixir, Go, Java, JavaScript and Rust. The compiler is AGPL-3.0; generated code is yours ([LICENSE](LICENSE)). Public Go API: `compiler/`, `ir/`.

```
make
make test
bin/schema check <dir>
```

| dir | purpose | guard | command |
| --- | --- | --- | --- |
| `.github/` | CI workflows | `go test ./internal/ci` | `make test` |
| [bench/](bench/AGENTS.md) | cross-language serialize bench | `go test ./bench/corpus` | `bench/run.sh` |
| `cmd/` | schema CLI | `go test ./cmd/schema` | `go test ./cmd/schema` |
| `comparison/` | Cap'n/PB/FlatBuffers packet numbers | `none` | `comparison/measure.sh` |
| `compiler/` | public driver API | `go test ./compiler` | `go test ./compiler` |
| `docs/` | spec, tutorial, contributing | `go test ./compiler` | `go test ./compiler` |
| `examples/` | type-wire corpus | `go test ./internal/goldens` | `make check` |
| `examples-wide/` | wide-text corpus | `go test ./internal/goldens` | `make check` |
| `examples128/` | int128/fixed corpus | `go test ./internal/goldens` | `make check` |
| `generated/` | committed nine-language output | `test/generated-tree/verify` | `make generated-current` |
| `images/` | README art | `none` | `none` |
| [internal/](internal/AGENTS.md) | compiler internals | `go test ./internal/...` | `go test ./internal/...` |
| `ir/` | public IR | `go test ./ir` | `go test ./ir` |
| `make/` | per-language make fragments | `go test ./tools/negativecontrols` | `make registry` |
| `notes/` | non-normative history | `none` | `none` |
| [tables/](tables/AGENTS.md) | table-wire corpora | `make check` | `make check` |
| [test/](test/AGENTS.md) | language test harnesses | `make test` | `make test` |
| [testdata/](testdata/AGENTS.md) | goldens, wire pins, conformance | `go test ./internal/goldens` | `make update-goldens` |
| [tools/](tools/AGENTS.md) | maintainer tools | `go test ./tools/...` | `go test ./tools/...` |
| `working/` | scratch; not normative | `none` | `none` |
