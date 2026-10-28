# AGENTS.md — generated map of testdata/

Do not edit. `make map` regenerates this file. Root: [AGENTS.md](../AGENTS.md). Rules: [CONTRIBUTING.md](../docs/CONTRIBUTING.md).

| dir | purpose | guard | command |
| --- | --- | --- | --- |
| `conformance/` | table conformance dumps | `make conformance` | `make conformance` |
| `golden/` | generated-source goldens | `go test ./internal/goldens` | `make update-goldens` |
| `jsontestsuite/` | vendored JSONTestSuite (MIT) | `go test ./internal/tabletext` | `go test ./internal/tabletext -run JSONTestSuite` |
| `luau-nightly-fuzz/` | Luau nightly fuzz repros | `make luau-nightly-fuzz-replay` | `make luau-nightly-fuzz` |
| `wire/` | pinned wire bytes | `go test ./internal/goldens` | `make update-goldens` |
