# AGENTS.md — generated map of internal/

Do not edit. `make map` regenerates this file. Root: [AGENTS.md](../AGENTS.md). Rules: [CONTRIBUTING.md](../docs/CONTRIBUTING.md).

| dir | purpose | guard | command |
| --- | --- | --- | --- |
| `ast/` | parsed *.schema form | `go test ./internal/parser` | `go test ./internal/parser` |
| `baseline/` | tables baseline pins | `go test ./internal/baseline` | `go test ./internal/baseline` |
| `check/` | resolve, check, lower to IR | `go test ./internal/check` | `go test ./internal/check` |
| `ci/` | workflow pin and SDK gates | `go test ./internal/ci` | `go test ./internal/ci` |
| [codegen/](codegen/AGENTS.md) | nine language emitters | `go test ./internal/codegen/...` | `go test ./internal/codegen/...` |
| `format/` | schemafmt | `go test ./internal/format` | `go test ./internal/format` |
| `fuzz/` | seeded fuzz corpus | `go test ./internal/fuzz` | `go test ./internal/fuzz` |
| `goldens/` | source, id, and wire pins | `go test ./internal/goldens` | `make update-goldens` |
| `listwalk/` | unbounded-array walk | `go test ./internal/listwalk` | `go test ./internal/listwalk` |
| `lockfile/` | schema.lock lineage | `go test ./internal/lockfile` | `go test ./internal/lockfile` |
| `parser/` | recursive-descent parser | `go test ./internal/parser` | `go test ./internal/parser` |
| `publicapi/` | external-module API gate | `go test ./internal/publicapi` | `go test ./internal/publicapi` |
| `scanner/` | tokenizer | `go test ./internal/parser` | `go test ./internal/parser` |
| `slowtest/` | 1-2 minute unit-test gate | `make slow-gate-scan` | `make slow-gate-scan` |
| `tablecook/` | cook form | `go test ./internal/tablecook` | `go test ./internal/tablecook` |
| `tablenames/` | per-language claimed names | `go test ./compiler` | `go test ./compiler` |
| `tablepack/` | pack/unpack | `go test ./internal/tablepack` | `go test ./internal/tablepack` |
| `tabletext/` | table JSON text | `go test ./internal/tabletext` | `go test ./internal/tabletext` |
| `tablewire/` | table wire codecs | `go test ./internal/tablewire` | `go test ./internal/tablewire` |
| `version/` | binary version stamp | `go test ./internal/version` | `go test ./internal/version` |
| `viewlisting/` | view listing vs IR | `go test ./internal/viewlisting` | `go test ./internal/viewlisting` |
