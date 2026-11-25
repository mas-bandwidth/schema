# AGENTS.md — generated map of internal/

Do not edit. `make map` regenerates this file. Root: [AGENTS.md](../AGENTS.md). Rules: [CONTRIBUTING.md](../docs/CONTRIBUTING.md).

| dir | purpose | guard | command |
| --- | --- | --- | --- |
| `ast/` | parsed *.schema form | `go test ./internal/parser` | `go test ./internal/parser` |
| `atomicfile/` | the one publisher every CLI write goes through | `go test ./internal/atomicfile` | `go test ./internal/atomicfile` |
| `baseline/` | tables baseline pins | `go test ./internal/baseline` | `go test ./internal/baseline` |
| `check/` | resolve, check, lower to IR | `go test ./internal/check` | `go test ./internal/check` |
| `ci/` | workflow pin and SDK gates | `go test ./internal/ci` | `go test ./internal/ci` |
| [codegen/](codegen/AGENTS.md) | per-language emitters | `go test ./internal/codegen/...` | `go test ./internal/codegen/...` |
| `docgate/` | link-and-citation gate over the pages | `go test ./internal/docgate` | `make docs-check` |
| `errdiscard/` | errors dropped by the Go programs under cmd/ and tools/, type-checked | `go test ./internal/errdiscard` | `make errdiscard` |
| `execref/` | os/exec references resolved by binding, for the child-process policies | `go test ./internal/execref` | `go test ./internal/execref` |
| `fixedproof/` | leg-independent fixed-table proof suite | `go test ./internal/fixedproof` | `go test ./internal/fixedproof` |
| `fixedunlawful/` | fixed form's shared unlawful-lineage corpus | `go test ./internal/fixedunlawful` | `go test ./internal/fixedunlawful` |
| `fixedvector/` | fixed form's shared cross-version vectors | `go test ./internal/fixedvector` | `go test ./internal/fixedvector` |
| `format/` | schemafmt | `go test ./internal/format` | `go test ./internal/format` |
| `fuzz/` | seeded fuzz corpus | `go test ./internal/fuzz` | `go test ./internal/fuzz` |
| `githooks/` | temp-repo tests for the .githooks/pre-push hook range and no-verify rules | `go test ./internal/githooks` | `go test ./internal/githooks` |
| `goldens/` | source, id, and wire pins | `go test ./internal/goldens` | `make update-goldens` |
| `goname/` | Go member spelling (initialisms) | `go test ./internal/goname` | `go test ./internal/goname` |
| `idcapwire/` | the retain-writer id cap test file builder | `go test ./internal/tablewire` | `go test ./internal/tablewire` |
| `inputfamilies/` | shared test-input families and derived reader legs | `go test ./internal/inputfamilies` | `go test ./internal/inputfamilies` |
| `irtest/` | test helper: a unit renamed past the checker | `go vet ./internal/irtest` | `go test ./internal/codegen/luau` |
| `jsontestsuite/` | JSONTestSuite oracle harness | `go test ./internal/tabletext` | `go test ./internal/tabletext -run JSONTestSuite` |
| `languages/` | the supported-language list, derived | `go test ./internal/languages` | `go test ./internal/languages` |
| `listwalk/` | unbounded-array walk | `go test ./internal/listwalk` | `go test ./internal/listwalk` |
| `lockfile/` | schema.lock lineage | `go test ./internal/lockfile` | `go test ./internal/lockfile` |
| `mapwalk/` | map walk | `go test ./internal/mapwalk` | `go test ./internal/mapwalk` |
| `memceiling/` | memory ceiling for allocating tests | `go test ./internal/memceiling` | `go test ./internal/memceiling` |
| `modulemove/` | pin and release boundary of the /v3 module move | `go test ./internal/modulemove` | `go test ./internal/modulemove` |
| `nameemit/` | name-corpus reader for the emit legs | `go test ./internal/codegen/c ./internal/codegen/cpp ./internal/codegen/csharp ./internal/codegen/elixir -run TestNameCorpusBuilds` | `go test ./internal/codegen/c ./internal/codegen/cpp ./internal/codegen/csharp ./internal/codegen/elixir -run TestNameCorpusBuilds` |
| `nightlyrows/` | PR-tier nightly-fast row parser and selector | `go test ./internal/nightlyrows` | `go test ./internal/nightlyrows` |
| `parser/` | recursive-descent parser | `go test ./internal/parser` | `go test ./internal/parser` |
| `pathid/` | where a path lands, by the file system's identity | `go test ./internal/pathid` | `go test ./internal/pathid` |
| `provesran/` | zero-match and unknown-candidate refusal for scans | `go test ./internal/provesran` | `go test ./internal/provesran` |
| `publicapi/` | external-module API gate | `go test ./internal/publicapi` | `go test ./internal/publicapi` |
| `recipescan/` | recipe and script failure-swallowing scan | `go test ./internal/recipescan` | `go run ./tools/recipescan` |
| `rfc3339/` | strict RFC 3339 timestamp parser | `go test ./internal/rfc3339` | `go test ./internal/rfc3339` |
| `runmatch/` | go test -run selection, as Go matches it | `go test ./internal/runmatch` | `go test ./internal/runmatch` |
| `scanner/` | tokenizer | `go test ./internal/parser` | `go test ./internal/parser` |
| `slowtest/` | names the tool or program a test needs, and fails when it is missing | `go test ./internal/slowtest` | `go test ./internal/slowtest` |
| `tablecook/` | cook form | `go test ./internal/tablecook` | `go test ./internal/tablecook` |
| `tablenames/` | per-language claimed names | `go test ./compiler` | `go test ./compiler` |
| `tablepack/` | pack/unpack | `go test ./internal/tablepack` | `go test ./internal/tablepack` |
| `tabletext/` | table JSON text | `go test ./internal/tabletext` | `go test ./internal/tabletext` |
| `tablewire/` | table wire codecs | `go test ./internal/tablewire` | `go test ./internal/tablewire` |
| `testinputs/` | repository files a test's child process reads, recorded for the go test cache | `go test ./internal/testinputs` | `go test ./internal/testinputs` |
| `testproc/` | bounded child processes for tests | `go test ./internal/testproc` | `go test ./internal/testproc` |
| `testreg/` | the test registry: load, select, shard, run, cover, extract | `go run ./tools/schema-test run --group pr` | `go run ./tools/schema-test coverage` |
| `testselect/` | a -run/-not selection listed as test names at the root, less the nightly tier | `go test ./internal/testselect` | `go run ./tools/gotestselect -not X ./pkg` |
| `vacuous/` | Go tests that pass checking nothing | `go test ./internal/vacuous` | `go test ./internal/ci -run TestEveryGoTestCanFail` |
| `version/` | binary version stamp | `go test ./internal/version` | `go test ./internal/version` |
| `viewlisting/` | view listing vs IR | `go test ./internal/viewlisting` | `go test ./internal/viewlisting` |
