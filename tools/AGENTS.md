# AGENTS.md — generated map of tools/

Do not edit. `make map` regenerates this file. Root: [AGENTS.md](../AGENTS.md). Rules: [CONTRIBUTING.md](../docs/CONTRIBUTING.md).

| dir | purpose | guard | command |
| --- | --- | --- | --- |
| `agentsmap/` | AGENTS.md generator and drift guard | `go test ./tools/agentsmap` | `make map` |
| `cellmerge/` | three-way PORTING.md row merge for diff3 hunks | `go test ./tools/cellmerge` | `go run ./tools/cellmerge docs/PORTING.md` |
| `compilebudget/` | holds every C++ conformance driver unit to its compile-seconds budget; a missing timing is red | `go test ./tools/compilebudget` | `make conformance-cpp-tu-budget` |
| `conformancerows/` | runs every conformance row file of a leg; any failing row is red | `go test ./tools/conformancerows` | `make conformance-rows-LEG` |
| `errdiscard/` | refuses an error dropped in cmd/ or tools/ with no reason | `go test ./internal/errdiscard` | `make errdiscard` |
| `fixedtwin/` | fixed-form twin | `go test ./tools/fixedtwin` | `go test ./tools/fixedtwin` |
| `fullrun/` | runs the whole test set on the fleet benches with no CI and collects the reds | `go test ./tools/fullrun` | `go run ./tools/fullrun launch --ref REF --bench HOST=SHARDS --out DIR` |
| `gatecontrol/` | one shared gate on one leg with its oracle planted wrong | `go test ./tools/gatecontrol` | `go run ./tools/gatecontrol -leg LEG -plant PLANT -target TARGET -want RE` |
| `gotestreport/` | go test -json row report; every skip is red | `go test ./tools/gotestreport` | `go run ./tools/gotestreport build/go-test-json` |
| `gotestrows/` | ci-fast go-test matrix rows | `go test ./tools/gotestrows` | `go run ./tools/gotestrows ./...` |
| `gotestrun/` | go test wrapper: refuses a run in which no test ran or passed | `go test ./tools/gotestrun` | `make test-run PKG=./pkg RUN='^TestX$'` |
| `gotestselect/` | prints the -run pattern naming a selection by test name; fails when it names none | `go test ./internal/testselect` | `go run ./tools/gotestselect -not X ./pkg` |
| `hostilecorpus/` | shared hostile-input corpus generator | `make schema-shared-hostile-corpus` | `make schema-shared-hostile-corpus-generate` |
| `hostilecorpusexec/` | native-port hostile-corpus driver | `make schema-c-runs-hostile-corpus` | `make schema-c-runs-hostile-corpus` |
| `hostileledger/` | hostile-corpus runner ledger check | `go test ./tools/hostileledger` | `go run ./tools/hostileledger` |
| `inputaccount/` | inotify input-file accounting wrapper for reader legs | `go test ./tools/inputaccount` | `go run ./tools/inputaccount -family NAME -leg LEG` |
| `inputfamilies/` | ci-fast input-family matrix selector and checker | `go test ./internal/inputfamilies` | `go run ./tools/inputfamilies check` |
| `luaufuzzhost/` | Luau nightly fuzz host (C++) | `make luau-nightly-fuzz-replay` | `make luau-nightly-fuzz` |
| `matrix/` | 3.0.0 proof gate | `go test ./tools/matrix` | `make proof` |
| `moofuzzhost/` | moo wire-fuzz native host (C++) | `make tables-moo-wire-fuzz` | `make tables-moo-wire-fuzz` |
| `mustrun/` | harness wrapper: exit status plus a registered ran N count | `go test ./tools/mustrun` | `build/mustrun -name X -count N -- cmd` |
| `namebuild/` | name-buildability nightly, every target | `go test ./tools/namebuild` | `make name-buildability` |
| `negativecontrols/` | negative-control enumerator | `go test ./tools/negativecontrols` | `go run ./tools/negativecontrols check` |
| `nightlyrows/` | ci-fast nightly-fast matrix row selector | `go test ./internal/nightlyrows` | `go run ./tools/nightlyrows -files diff-files.txt -broad no` |
| `pgroup/` | process-group runner for CI steps | `go test ./tools/pgroup` | `go run ./tools/pgroup -- make test` |
| `recipescan/` | CLI for internal/recipescan | `go test ./internal/recipescan` | `go run ./tools/recipescan` |
| `releasebin/` | release binaries for six platforms | `go test ./tools/releasebin` | `make release-binaries` |
| `releasediff/` | previous-release differential (#463) | `make release-differential-negative-control` | `make release-differential` |
| `releasetag/` | promotion and release tags | `go test ./tools/releasetag` | `make release` |
| `releasetargets/` | certify release-gates planner from make's database | `go test ./tools/releasetargets` | `go run ./tools/releasetargets -fixtures tools/releasetargets/testdata/f066` |
| `retiredskips/` | fails a fixed-form gate on any skip line it prints | `go test ./tools/retiredskips` | `build/retiredskips -leg LEG -- cmd` |
| `roadmap/` | ROADMAP.md table honesty | `go test ./tools/roadmap` | `go test ./tools/roadmap` |
| `sabotage/` | negative-control sabotages | `go test ./tools/negativecontrols` | `go run ./tools/sabotage` |
| `schema-test/` | the one test driver: run, list, coverage, extract | `go run ./tools/schema-test run --group pr` | `go run ./tools/schema-test run --group nightly` |
| `testguard/` | holds a row's test results to its suites, baseline and coverage tables; every skip is red | `go test ./tools/testguard` | `go run ./tools/testguard check --config CONFIG.tsv RECORD...` |
| `tlcrun/` | TLC runner held to tla/RUNS.tsv | `go test ./tools/tlcrun` | `bash tla/run-tlc.sh` |
| `treelock/` | generated-tree lock | `go test ./tools/treelock` | `go test ./tools/treelock` |
| `vacuoustests/` | vacuous-test analyzer command | `go test ./internal/vacuous` | `go run ./tools/vacuoustests -all` |
| `vuln/` | govulncheck wrapper | `make vuln-selftest` | `make vuln-selftest` |
| `wiredtargets/` | refuses a test-like make target no CI workflow reaches | `go test ./tools/wiredtargets` | `go run ./tools/wiredtargets` |
| `wirefuzzrelease/` | release-tier wire-fuzz gate scorer | `go test ./tools/wirefuzzrelease` | `go run ./tools/wirefuzzrelease` |
