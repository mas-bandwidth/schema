# AGENTS.md — generated map of tools/

Do not edit. `make map` regenerates this file. Root: [AGENTS.md](../AGENTS.md). Rules: [CONTRIBUTING.md](../docs/CONTRIBUTING.md).

| dir | purpose | guard | command |
| --- | --- | --- | --- |
| `agentsmap/` | AGENTS.md generator and drift guard | `go test ./tools/agentsmap` | `make map` |
| `cellmerge/` | three-way PORTING.md row merge for diff3 hunks | `go test ./tools/cellmerge` | `go run ./tools/cellmerge docs/PORTING.md` |
| `errdiscard/` | refuses an error dropped in cmd/ or tools/ with no reason | `go test ./internal/errdiscard` | `make errdiscard` |
| `fixedtwin/` | fixed-form twin | `go test ./tools/fixedtwin` | `go test ./tools/fixedtwin` |
| `gotestreport/` | go test -json row report and skip ledger | `go test ./tools/gotestreport` | `go run ./tools/gotestreport -skips .github/skip-ledger.txt build/go-test-json` |
| `gotestrows/` | ci-fast go-test matrix rows | `go test ./tools/gotestrows` | `go run ./tools/gotestrows ./...` |
| `hostilecorpus/` | shared hostile-input corpus generator | `make schema-shared-hostile-corpus` | `make schema-shared-hostile-corpus-generate` |
| `hostilecorpusexec/` | native-port hostile-corpus driver | `make schema-c-runs-hostile-corpus` | `make schema-c-runs-hostile-corpus` |
| `luaufuzzhost/` | Luau nightly fuzz host (C++) | `make luau-nightly-fuzz-replay` | `make luau-nightly-fuzz` |
| `matrix/` | 3.0.0 proof gate | `go test ./tools/matrix` | `make proof` |
| `moofuzzhost/` | moo wire-fuzz native host (C++) | `make tables-moo-wire-fuzz` | `make tables-moo-wire-fuzz` |
| `mustrun/` | harness wrapper: exit status plus a registered ran N count | `go test ./tools/mustrun` | `build/mustrun -name X -count N -- cmd` |
| `negativecontrols/` | negative-control enumerator | `go test ./tools/negativecontrols` | `go run ./tools/negativecontrols check` |
| `pgroup/` | process-group runner for CI steps | `go test ./tools/pgroup` | `go run ./tools/pgroup -- make test` |
| `recipescan/` | CLI for internal/recipescan | `go test ./internal/recipescan` | `go run ./tools/recipescan` |
| `releasebin/` | release binaries for six platforms | `go test ./tools/releasebin` | `make release-binaries` |
| `releasediff/` | previous-release differential (#463) | `make release-differential-negative-control` | `make release-differential` |
| `releasetag/` | promotion and release tags | `go test ./tools/releasetag` | `make release` |
| `releasetargets/` | certify release-gates planner from make's database | `go test ./tools/releasetargets` | `go run ./tools/releasetargets -fixtures tools/releasetargets/testdata/f066` |
| `roadmap/` | ROADMAP.md table honesty | `go test ./tools/roadmap` | `go test ./tools/roadmap` |
| `sabotage/` | negative-control sabotages | `go test ./tools/negativecontrols` | `go run ./tools/sabotage` |
| `slowgatescan/` | SCHEMA_SLOW recipe scan | `make slow-gate-scan` | `make slow-gate-scan` |
| `tlcrun/` | TLC runner held to tla/RUNS.tsv | `go test ./tools/tlcrun` | `bash tla/run-tlc.sh` |
| `treelock/` | generated-tree lock | `go test ./tools/treelock` | `go test ./tools/treelock` |
| `vacuoustests/` | vacuous-test analyzer command | `go test ./internal/vacuous` | `go run ./tools/vacuoustests -all` |
| `vuln/` | govulncheck wrapper | `make vuln-selftest` | `make vuln-selftest` |
