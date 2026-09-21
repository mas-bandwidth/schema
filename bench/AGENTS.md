# AGENTS.md — generated map of bench/

Do not edit. `make map` regenerates this file. Root: [AGENTS.md](../AGENTS.md). Rules: [CONTRIBUTING.md](../docs/CONTRIBUTING.md).

| dir | purpose | guard | command |
| --- | --- | --- | --- |
| `c/` | C serialize runner | `bench/run.sh --only c` | `bench/run.sh --only c` |
| `corpus/` | bench schemas and weighting gate | `go test ./bench/corpus` | `go test ./bench/corpus` |
| `cpp/` | C++ reference runner | `bench/run.sh --only cpp` | `bench/run.sh --only cpp` |
| `cs/` | C# runner | `bench/run.sh --only cs` | `bench/run.sh --only cs` |
| `dart/` | Dart runner | `bench/run.sh --only dart` | `bench/run.sh --only dart` |
| `elixir/` | Elixir runner | `bench/run.sh --only elixir` | `bench/run.sh --only elixir` |
| `go/` | Go runner | `bench/run.sh --only go` | `bench/run.sh --only go` |
| `java/` | Java runner | `bench/run.sh --only java` | `bench/run.sh --only java` |
| `js/` | JavaScript runner | `bench/run.sh --only js` | `bench/run.sh --only js` |
| `paired/` | paired before/after + fixed-form bytes | `make bench-paired-check` | `make bench-paired-gate` |
| `results/` | committed CSV ledgers | `none` | `bench/run.sh` |
| `rust/` | Rust runner | `bench/run.sh --only rust` | `bench/run.sh --only rust` |
| `tables/` | tables bench pass | `make bench-tables` | `make bench-tables` |
| `tools/` | bench aggregation and shape gate | `go test ./bench/tools` | `go test ./bench/tools` |
