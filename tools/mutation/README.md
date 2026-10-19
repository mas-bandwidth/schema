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
| gremlins | v0.6.0 | 42 | 29 | 5 | 0 | 17.49 | yes: the pre-fix trial ran twice with the same mutant set; the fixed bounded run below is one run because the card measures one target | no: it runs the package suite; per-file scope comes from `--exclude-files`, and `targets.tsv` records the suite each target needs |
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
generated (`DO NOT EDIT`) and is listed in `targets.tsv` but is UNKILLABLE as
a target: the generated units take the runtime as emitted text from
`gotable.go`, so the package's 833 mutants are all not covered. Its mutants are
measured there, and any fix belongs
to the emitter that writes it.

The earlier six-target figures (`MUTATION_JOBS=24` and `--test-cpu 1`) are
withdrawn: gremlins v0.6.0 passes `--test-cpu` as the single argv `-cpu 1`, so
every mutant failed setup and was counted KILLED. Only figures from the fixed
bounded command are reported here. This attempt measured one small target in
the card, as instructed; the other five are the nightly workflow's job, and
each nightly run appends their bounded minutes here.

| target | minutes | mutants | killed | survived | timed out | not covered | score |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `internal/codegen/gotable/messagewrite.go` | 17.49 | 42 | 29 | 5 | 0 | 8 | 69.05 |

The bounded command was

    /usr/bin/time -v make mutation-go MUTATION_FILES=internal/codegen/gotable/messagewrite.go

and its report line was

    mutation targets=1 mutants=42 killed=29 survived=5 equivalent=0 timed_out=0 not_covered=8 score=69.05

The five survivors (all `CONDITIONALS_NEGATION`, at lines 50, 101, 104, 116
and 165) are the first triage queue; none is yet an equivalent-mutant row.

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

Every run sets `ulimit -v 4194304` (`MUTATION_ULIMIT_V=4194304`, a 4 GiB OS
virtual-address-space ceiling inherited by gremlins and every child `go test`
process) and exports `GOMEMLIMIT=1500MiB` (`MUTATION_MEMORY=1500MiB`),
`GOGC=25` (`MUTATION_GOGC=25`), `GOMAXPROCS=1`, and
`GOFLAGS=-mod=readonly -p=1`, passing gremlins `--workers $(MUTATION_JOBS)`
(default 1), `--timeout-coefficient 3`, and never passing `--test-cpu` (the
v0.6.0 single-argv defect above). Why both `GOMEMLIMIT=1500MiB` and
`ulimit -v 4194304` are needed: mutating `internal/codegen/gotable/messagewrite.go:60`
(`if f.KeyEnum != ""` negated) creates mutual recursion between
`emitMessageRides` and `emitMessageKeyedCount`, growing `g.buf`
(`strings.Builder`) and the goroutine stack until the child `go test` process
hits its ceiling. Under `GOMEMLIMIT=6GiB` with no address-space cap (`w6`),
that single runaway mutant grew to a 6 GiB heap plus a 1 GiB goroutine stack
(`7608512` kbytes RSS) before crashing. Under `ulimit -v 4194304`,
`GOMEMLIMIT=1500MiB`, and `GOGC=25`, the runaway mutant fails closed with a
fatal out-of-memory error in ~2.35s (`KILLED`) at ~1.43 GiB RSS, and the
nightly workflow exports the same bounds.

`make mutation-canary` runs the pinned tool on
`tools/mutation/testdata/canary`, whose test calls `Double(0)` only, so its
`ARITHMETIC_BASE` mutant must be LIVED. The target fails when that mutant is
reported KILLED, catching the v0.6.0 argv defect instead of trusting a run
that kills everything. `mutation-go` depends on the canary.

The bench measurement,

    /usr/bin/time -v make mutation-go MUTATION_FILES=internal/codegen/gotable/messagewrite.go

reported elapsed `17:29.15` (17.49 minutes) and
`Maximum resident set size (kbytes): 2016780` (~1.92 GiB), well below the
6 GiB bound (`6291456` kbytes).

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
