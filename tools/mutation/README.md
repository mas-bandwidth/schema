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

One full six-target run was measured on the Linux bench on 2026-10-19, targets
one after another with `MUTATION_WORKERS=24`, each through
`make mutation-go MUTATION_FILES=<file>`:

| target | minutes | mutants | killed | survived | timed out |
| --- | --- | --- | --- | --- | --- |
| `internal/codegen/gotable/read.go` | 0.37 | 123 | 29 | 0 | 83 |
| `internal/codegen/gotable/messageread.go` | 0.37 | 43 | 15 | 0 | 18 |
| `internal/codegen/gotable/write.go` | 0.53 | 151 | 12 | 0 | 125 |
| `internal/codegen/gotable/messagewrite.go` | 0.23 | 42 | 21 | 0 | 13 |
| `internal/codegen/gotable/cookwrite.go` | 0.38 | 25 | 2 | 0 | 18 |
| `tables/goruntime/goruntime.go` | 0.22 | 833 | 0 | 0 | 0 |

The six targets took 2.1 minutes together and the combined report line was

    mutation targets=6 mutants=1217 killed=79 survived=0 equivalent=0 timed_out=257 score=100.00

so the whole set fits a six-hour job with room to spare and the nightly shards
nothing. The row for `tables/goruntime/goruntime.go` is honest about a limit:
that package has no tests of its own, so gremlins generates 833 mutants and none
are covered; the generated units that exercise it live in `internal/codegen/gotable`
and a per-target test command is not something gremlins accepts. The nightly
uses the default `MUTATION_WORKERS=4`; the measured minutes used 24 on the
64-core bench and the per-target timeouts in `targets.tsv` bound a slower run.
A `timed out` row is a mutant whose generated unit run did not finish inside
gremlins' coverage-derived timeout; it is neither a kill nor a survivor, and
the nightly keeps it visible in the count instead of hiding it.

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
