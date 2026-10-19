# Mutation testing for the Go table leg

This module pins the mutation tool and holds the report summariser. It is the
automated complement to the tree's hand-written sabotages (`tools/sabotage`,
`tools/negativecontrols`): each of those removes one named rule, while a
mutation run asks the generated-unit suite to kill every mutation the tool can
make in the read and write emitters.

## The pinned tool

`gremlins` is pinned by the `tool` directive in this module's `go.mod`, so the
root `go.mod` stays free of requirements. Run it from the repository root:

    go tool -modfile=tools/mutation/go.mod gremlins unleash ./internal/codegen/gotable ...

`go get -tool github.com/go-gremlins/gremlins/cmd/gremlins@latest` was run
inside `tools/mutation`; `go.sum` carries the checksums from
`proxy.golang.org`/`sum.golang.org`. The module is standard-library only
(`report.go`), apart from the pinned tool.

## The trial (target: `internal/codegen/gotable/messagewrite.go`)

Both candidates were installed with `go get -tool` from the module proxy, then
tried on the named emitter. "Mutants" are what the tool generated for the
target; the messagewrite row is a real, completed run (48.7 s after a 2.8 s
warm coverage pass; 0 survived).

| tool | version | mutants | killed | survived | timed out | minutes | reproducible? | per-target command? |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| gremlins | v0.6.0 | 42 | 21 | 0 | 13 | 0.86 | yes: two runs, same 21 killed / 8 not covered / 13 timed out | no: it runs the package suite; per-file scope comes from `--exclude-files`, and `targets.tsv` records the suite each target needs |
| go-mutesting (avito-tech) | latest pseudo-version | not run | - | - | - | - | - | yes (`--exec`) |
| go-mutesting (zimmski, pinned fallback) | v0.0.0-20210610104036 | not run | - | - | - | - | - | yes (`--exec`), but the exec hook must apply and restore the mutant itself |

The avito fork's latest commit requires Go 1.27 and this bench runs the version
the root `go.mod` names (1.26), so it could not be pinned here. The zimmski
ancestor installs and type-checks the target, but its built-in exec rewrites the
source file and runs one full package test per mutant; the package suite measured
55 s warm, which puts one emitter's run in hours, and its `--exec` path leaves
the apply/restore step to a helper. Gremlins was chosen because two runs of the
same target produced the same survivor set, it writes machine-readable JSON
(consumed by `report.go`), and its coverage-driven test selection keeps a
target's run inside the nightly budget. Its limitation is recorded honestly:
it cannot accept an arbitrary test command per target, so the command column in
`targets.tsv` names the suite an operator runs to confirm a target, and the make
target restricts each run to one file with `--exclude-files`.

## Targets

`targets.tsv` has three tab-separated columns: the emitter file, the test
command that kills its mutants, and a timeout in seconds. The command is always
the package's tests (`go test -count=1 -timeout 600s ./internal/codegen/gotable`),
never a narrower `-run` filter: the generated-unit tests in that package are
what exercise the emitters. `tables/goruntime/goruntime.go` is generated
(`DO NOT EDIT`); its mutants are measured there, and any fix belongs to the
emitter that writes it.

| target | minutes (bench, this run) |
| --- | --- |
| `internal/codegen/gotable/messagewrite.go` | 0.86 |
| `internal/codegen/gotable/read.go` | not measured (deadline) |
| `internal/codegen/gotable/messageread.go` | not measured (deadline) |
| `internal/codegen/gotable/write.go` | not measured (deadline) |
| `internal/codegen/gotable/cookwrite.go` | not measured (deadline) |
| `tables/goruntime/goruntime.go` | not measured (deadline) |

A full six-target run was not completed inside this card's wall. The nightly
workflow shards nothing yet because the measured target is under a minute; once
the remaining minutes are recorded, a target over six hours is split by target
(one job per `targets.tsv` row) rather than lengthening the job.

## The report

    make mutation-go                         # every target
    make mutation-go MUTATION_FILES=internal/codegen/gotable/messagewrite.go

runs gremlins per target into `build/mutation/<file>.json` and then:

    cd tools/mutation && go run report.go -root ../.. \
        -targets targets.tsv -equivalent equivalent ../../build/mutation/*.json

which prints one line per surviving mutant not listed as equivalent,

    SURVIVED <file>:<line>:<col> <mutator> <original> -> <<mutator>>

(the tool's JSON omits the substituted token, so the mutator names the mutant),
one `STALE` line for every `equivalent/*.tsv` row whose source line no longer
holds the original text, and then:

    mutation targets=<n> mutants=<n> killed=<n> survived=<n> equivalent=<n> timed_out=<n> score=<pct>

`score` is killed over killed+survived (equivalent mutants are excluded).
Equivalent rows are `file, line, mutator, original, why` in a tab-separated
file; see `equivalent/README.md`.

## Tests

    cd tools/mutation && go test -count=1 -timeout 600s ./...

`TestReportListsSurvivors` drives a canned gremlins report with one killed, one
survived and one equivalent mutant, plus a stale equivalent row, and asserts the
suppression, the STALE line and the summary.
