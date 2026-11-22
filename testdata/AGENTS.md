# AGENTS.md — generated map of testdata/

Do not edit. `make map` regenerates this file. Root: [AGENTS.md](../AGENTS.md). Rules: [CONTRIBUTING.md](../docs/CONTRIBUTING.md).

| dir | purpose | guard | command |
| --- | --- | --- | --- |
| `conformance/` | table conformance dumps | `make conformance` | `make conformance` |
| `golangci-lint-classes/` | generated Go lint regression fixtures | `go test ./internal/goldens -run TestGolangciLintClassesShapes` | `make schema-go-generated-golangci-lint-gate-classes-negative-control` |
| `golden/` | generated-source goldens | `go test ./internal/goldens` | `make update-goldens` |
| `jsontestsuite/` | vendored JSONTestSuite (MIT) | `go test ./internal/tabletext` | `go test ./internal/tabletext -run JSONTestSuite` |
| `lua-nightly-fuzz/` | Lua fuzz replay controls | `make schema-lua-fuzz-replay-controls` | `make schema-lua-fuzz-replay` |
| `luau-nightly-fuzz/` | Luau nightly fuzz repros | `make luau-nightly-fuzz-replay` | `make luau-nightly-fuzz` |
| `moo-wire-fuzz-pinned-controls/` | moo pinned-list controls | `make tables-moo-wire-fuzz-pinned-list-controls` | `go test ./internal/ci -run TestMooPinnedListIsReadWhole` |
| `name-buildability/` | name-shadowing probe corpus | `go test ./internal/check -run TestNameBuildabilityCorpus` | `make name-buildability` |
| `name-buildability-emit/` | platform-shadowing names the emitters must express | `go test ./internal/codegen/c ./internal/codegen/cpp ./internal/codegen/csharp ./internal/codegen/elixir -run TestNameCorpusBuilds` | `SCHEMA_SLOW=1 go test ./internal/codegen/c ./internal/codegen/cpp ./internal/codegen/csharp ./internal/codegen/elixir -run TestNameCorpusBuilds` |
| `ruby-gems-lock-controls/` | Ruby gem lock controls | `make ruby-gems-lock-controls` | `go test ./internal/ci -run TestRubyGemsLockIsReadWhole` |
| `wire/` | pinned wire bytes | `go test ./internal/goldens` | `make update-goldens` |
