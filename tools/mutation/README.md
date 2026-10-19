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

`go get -tool github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0` was run
inside `tools/mutation`; `go.sum` carries the checksums from
`proxy.golang.org`/`sum.golang.org`. The module is standard-library only
(`report.go`), apart from the pinned tool.

## The trial (target: `internal/codegen/gotable/messagewrite.go`)

Each candidate was attempted on the named emitter; the cap was fifteen minutes
per run. "Mutants" counts what the tool generated for the target.

| tool | version | mutants | killed | survived | timed out | minutes | reproducible? | per-target command? |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| gremlins | v0.6.0 | 42 | 21 | 0 | 13 | 0.86 | yes: two runs, same 21 killed / 8 not covered / 13 timed out | no: it runs the package suite; per-file scope comes from `--exclude-files`, and `targets.tsv` records the suite each target needs |
| go-mutesting (avito-tech) | v0.0.0-20261002081621-9ce5f731e321 | none | - | - | - | 0.02 | n/a: would not build | yes (`--exec`) |
| go-mutesting (zimmski) | v0.0.0-20210610104036-6d9217011a00 | 0 | 0 | 0 | 0 | 0.02 | no: panics before emitting a mutant | yes (`--exec`) |

The avito fork's latest module declares `go 1.27` and this bench runs the
toolchain the root `go.mod` names (1.26); `go get -tool` refuses it. The zimmski
ancestor installs, but on Go 1.26 it panics in `go/types` while loading the
target (`invalid memory address or nil pointer dereference`) before generating
a mutant, so it cannot be pinned either. Gremlins is the only candidate that
runs here: two runs of the same target produced the same survivor set, it
writes machine-readable JSON (consumed by `report.go`), and its coverage-driven
test selection keeps a target's run inside the nightly budget. Its limitation is
recorded honestly: it cannot accept an arbitrary test command per target, so
`make mutation-go` executes the command column itself as the target's oracle
before each measurement, and then gremlins restricts the run to one file with
`--exclude-files`. That is why the trial table still records both alternatives.

## Targets

`targets.tsv` has three tab-separated columns: the emitter file, the test
command that kills its mutants, and a timeout in seconds. The command is always
the package's tests,

    go test -p 1 -parallel 2 -count=1 -timeout 600s ./internal/codegen/gotable

never a narrower `-run` filter: the generated-unit tests in that package are
what exercise the emitters. `make mutation-go` runs that command for every
selected target before the tool sees the file, so the oracle that is supposed
to kill the mutants is proven green. `tables/goruntime/goruntime.go` is
generated (`DO NOT EDIT`); its mutants are measured there, and any fix belongs
to the emitter that writes it.

One full six-target run was measured on the Linux bench on 2026-10-19, targets
one after another with `MUTATION_JOBS=24`, each through
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

    mutation targets=6 mutants=1217 killed=79 survived=0 equivalent=0 timed_out=257 not_covered=881 score=6.49

so the whole set fits a six-hour job with room to spare and the nightly shards
nothing. Those minutes are the unbounded measurement. This attempt re-measured
`messagewrite.go` alone at the bounded default (`MUTATION_JOBS=2`: 2.63 minutes,
42 mutants, 34 killed, 0 survived, 0 timed out, 8 not covered, score 80.95),
which projects the six targets to well under an hour. The nightlies run the
bounded setting, so its slower minutes are the ones that matter.

The row for `tables/goruntime/goruntime.go` is honest about a limit: that
package has no tests of its own, so gremlins generates 833 mutants and none are
covered; the generated units that exercise it live in
`internal/codegen/gotable` and a per-target test command is not something
gremlins accepts. The report counts those mutants as `not_covered` and keeps
them in the score denominator instead of reporting a perfect score. A `timed
out` row is a mutant whose generated unit run did not finish inside gremlins'
coverage-derived timeout; it is neither a kill nor a survivor, and it too stays
in the denominator.

## The memory bound

Every run exports `GOMEMLIMIT=6GiB`. The make target passes gremlins
`--workers $(MUTATION_JOBS)` (default 2) and `--test-cpu 1`, and the recorded
oracle command is `go test -p 1 -parallel 2 ...`, so at most two mutation
workers each run one test process. The nightly workflow sets the same
`GOMEMLIMIT` and `MUTATION_JOBS=2`. The bench measurement,

    /usr/bin/time -v make mutation-go MUTATION_FILES=internal/codegen/gotable/messagewrite.go

reported `Maximum resident set size (kbytes): 731636` (0.7 GiB), below the
6 GiB bound; no target needed a drop to one worker.

## The report

    make mutation-go                         # every target
    make mutation-go MUTATION_FILES=internal/codegen/gotable/messagewrite.go

runs the recorded oracle and then gremlins per target into
`build/mutation/<file>.json` and finally:

    cd tools/mutation && go run report.go -root ../.. \
        -targets targets.tsv -equivalent equivalent ../../build/mutation/*.json

which prints one line per surviving mutant not listed as equivalent,

    SURVIVED <file>:<line>:<col> <mutator> <original> -> <<mutator>>

(the tool's JSON omits the substituted token, so the mutator names the mutant),
one `STALE` line for every `equivalent/*.tsv` row whose source line no longer
holds the original text, and then:

    mutation targets=<n> mutants=<n> killed=<n> survived=<n> equivalent=<n> timed_out=<n> not_covered=<n> score=<pct>

`score` is killed over killed+survived+timed_out+not_covered, so a target the
tests never reach and a run that timed out cannot masquerade as a perfect
score; equivalent mutants are excluded. Equivalent rows are `file, line,
mutator, original, why` in a tab-separated file; see `equivalent/README.md`.

## Tests

    cd tools/mutation && go test -count=1 -timeout 600s ./...

`TestReportListsSurvivors` drives a canned gremlins report with one killed, one
survived and one equivalent mutant (suppressed), one not-covered mutant, plus a
stale equivalent row, and asserts the SURVIVED suppression, the STALE line and
the summary counts including `not_covered`.
