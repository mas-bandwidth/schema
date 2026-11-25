# The test registry and the one driver

Every test is a row of one data file, [`test/registry/registry.jsonl`](../test/registry/registry.jsonl),
and runs through one driver, `tools/schema-test`, by its name. This page is the
contract: the format, the names, the fail rules, the adapters, and how each kind
of test fits. The package is `internal/testreg`.

This is layer 1. It holds the structure, the driver, the per-PR and nightly jobs,
and one family wired end to end (the tracer, below). The tests that exist today
move in as later layers; the old harness jobs keep running until the registry
covers what they run.

## The format

JSON Lines: one JSON object per line, each with `"rec"` naming its kind. The repo
already keeps case data this way (`testdata/conformance/.../cases.jsonl`), the
extraction of today's tests is written in it, it needs no parser beyond the
standard library, and one record per line makes every refusal name its line.
Lines may come in any order.

| rec | fields | what it is |
|---|---|---|
| `lang` | `name`, `write` (`assert` or `refuse`), `adapter` | one supported language. `adapter` is `""` until its adapter lands. |
| `tier` | `name`, `what` | `unit`, `functional`, `nightly` |
| `type` | `name`, `what`, `contract` | one kind of check (roundtrip, load-refusal, golden, lint, model, plant, ...); `contract` is what an adapter does to answer a test of the type, which the spec addenda write out |
| `test` | `key`, `type`, `family`, `case`, `tier`, `checks`, `inputs`, `expect`, `source`, and for a language-free test `scope: "none"` and `adapter` | one test |
| `unrepresentable` | `test`, `lang`, `reason` | a language cannot express a test |
| `group` | `name`, `what`, `query` | a stored query a workflow runs by name |

A test's key is `type.family.case`. `inputs` is the data its adapter is handed;
`expect` says what passing establishes.

The language list is only ever read from the registry: no count of languages is
written in code, docs or CI. `TestTheRegistryLoads` holds the `lang` rows to the
generators the compiler registers (`internal/languages`), so a generator with no
row, or a row with no generator, is red.

`testreg.Load` refuses, naming the file and line: a malformed line, an unknown
record kind or field, a duplicate language, tier, type, test key or group, a test
whose type or tier is not declared, a key that is not `type.family.case`, an
unrepresentable declaration for a test or language that does not exist (or with
no reason), and a group whose query is unknown or matches nothing.

## Names

The instances are generated, never listed: every per-language test times every
`lang` row, less the pairs declared unrepresentable, plus one instance of each
language-free test.

- A per-language instance is `<lang>.<tier>.<type>.<family>.<case>`, for example
  `go.functional.roundtrip.align-arrays.phase63-ab-e-acute`.
- A language-free instance is `none.<tier>.<type>.<family>.<case>`, for example
  `none.unit.tooling.testreg.shards-cover-every-name-exactly-once`. `none` is a
  scope, not a language: it is never counted with the languages.
- A declared-unrepresentable pair has no instance. It is data, shown in the
  coverage matrix and counted, never a name in a run and never a pass. Running it
  by name fails ("declares it unrepresentable, so it has no such instance").

## Running

```
schema-test run  NAME|PATTERN... [--lang L]... [--tier T]... [--type Y]... [--family F]... [--ready] [--shard i/n]
schema-test run  --group G [--shard i/n]
schema-test list (the same)
schema-test coverage
schema-test extract [--json] [--write-ledger] [--shrinks-from LEDGER] [--since MAIN-TREE] [--move LAYER]
schema-test seconds [--registry PATH] LOG...
schema-test addenda [--check] [--root DIR]
```

You run a test by its name or you do not. A query expands to an explicit list of
full names before anything starts, and the list is printed with its count. Every
name in it runs and passes or fails; nothing downstream drops a name.

- A NAME with `*` is a pattern: `*` matches any run of characters, dots included
  (`squirrel.*`, `*.unit.roundtrip.align-arrays.*`). Names and patterns are
  alternatives; the filters then narrow them (within a filter the values are
  alternatives, across filters all must hold). `--ready` keeps the languages
  whose adapter has landed. `--lang none` selects the language-free tests.
- `--group G` runs a stored query. The workflows run groups, so which tests a
  job runs is data here, never a list in a workflow: `pr` and `nightly`.
- `--shard i/n` runs the i-th of n parts of the expanded list. The parts are
  contiguous in registry order (one Go package's tests stay together) and cut
  by each test's measured seconds (its `"seconds"`; a test with none weighs
  1 s): the heaviest part is as light as a contiguous cut allows, and the
  others end near their share of what is left. The parts cover every name
  exactly once. An empty part fails.
- `--write-seconds` records each passing test's measured seconds in the
  registry; `schema-test seconds LOG...` does the same from the logs of runs
  on fleet runners (each outcome line carries `(1.23s)`), which is where the
  numbers come from: a bench's seconds are not a runner's.
- Adapters for different languages run in parallel within one run.
- `coverage` prints the matrix of every per-language test times every language,
  each cell `ok` (answered), `un` (declared unrepresentable) or `MISS`, then
  the language-free tests, then each language's counts with its MISSING cells
  grouped by type: the per-language work list.

Every run prints `expected, ran, passed, failed, missing, unrepresentable`.

## The fail rules

Running a test that does not exist always fails. Each of these is a named
failure and a non-zero exit, never a pass, a skip or an empty result:

| rule | unit test (red when planted) |
|---|---|
| an unknown full name | `TestUnknownFullNameFails` |
| a query, or one pattern of it, that matches 0 tests | `TestAQueryThatMatchesNothingFails` |
| a language with no adapter: every instance MISSING | `TestALanguageWithNoAdapterFails` |
| a language naming an adapter schema-test does not have | `TestAnAdapterSchemaTestDoesNotHaveFails` |
| a test with no instance for a language and no unrepresentable declaration | `TestAnInstanceWithNoAnswerAndNoDeclarationFails` |
| expected != ran | `TestExpectedNotRanFails` |
| an adapter reports an instance it was not asked for | `TestAResultNobodyAskedForFails` |
| an adapter omits an instance it was asked for, or reports one twice | `TestExpectedNotRanFails`, `TestAResultReportedTwiceFails` |
| an adapter crashes | `TestAnAdapterCrashFails` |
| an adapter times out (the deadline is an argument; the test hands in one already past) | `TestAnAdapterTimeoutFails` |
| a verdict other than pass or fail: there is no skipped verdict | `TestThereIsNoSkippedVerdict` |
| a harness that logs a skip | `TestAHarnessSkipLineFails` |
| a Go test that skips | `TestGoTestEventsGiveOneVerdictEach` |
| the command's exit status: unknown name, 0 matches, no adapter, unknown group, empty shard | `TestTheCommandExitsNonZeroOnEveryFailRule` |
| the command's exit status on an adapter crash, timeout, stray result, omitted result and missing instance | `TestTheCommandExitsNonZeroOnEveryAdapterFault` |
| a run that selects nothing still prints its counts line, every count 0 | `TestARunThatSelectsNothingStillPrintsItsCounts` |

The loader's refusals have one test each (`TestLoadRefuses*`). `TestTheDriverHasNoSkips`
refuses any `Skip`, `SkipNow`, `Skipf` or `testing.Short()` in the driver's own
packages, and finds a planted one; the repo-wide form comes with the migration.
`TestEveryDriverTestIsRegistered` holds every Go test in the driver's packages
to a registry row, and every such row to a test that exists.

## Tiers

- **unit**: no real time, no sockets, no network, no outside services or
  toolchains beyond the Go test binary; stays inside its working directory.
- **functional**: uses a real thing: a compiler or VM, generated code built and
  run. Per pull request, on runners that provide the toolchain. A missing
  toolchain there is a failure, never a skip.
- **nightly**: heavy or long: long fuzz, full matrices, soak, timing.

Where a test today skips because it needs a toolchain or a long run, that
condition becomes its tier. Choosing what runs is the query, stated up front.

## Adapters

An adapter is thin. It is handed the instances (each with its full name, key,
family, case, inputs and expect), builds and invokes the code that checks them,
and returns one result per instance: `{"instance", "verdict": "pass"|"fail",
"reason"}`. It holds no case list and no skip logic.

<!-- example: fragment -->
```go
type Adapter interface {
    Answers(t Test) bool // has a harness for this test; false makes it MISSING
    Run(ctx context.Context, lang string, instances []Instance) ([]Result, error)
}
```

A harness that runs in its own process speaks the shared protocol: request lines
(JSON, one per instance) on standard input, and on standard output one line per
instance, `SCHEMA_TEST_RESULT {"instance":...,"verdict":...,"reason":...}`. Other
output lines are its log; a log line that reports a skip fails the call.

Built today:

| adapter | for | how |
|---|---|---|
| `go`, `lua` | per-language rows of family `align-arrays` | `make bin/schema build/verify-bitphase-bin`, then `verify-bitphase --adapter --lang L` |
| `gotest` | language-free Go tests; inputs `{"package", "test"}` | one `go test -count=1 -json -run '^(A|B|...)$' <package>` per package, several packages at once, every event mapped to its row by full name, with `SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1` set (a test's tier says what it needs; under the driver no gate skips). A test or subtest that skips fails; a row whose test is not in its package's source is MISSING; one that never reports fails. A package in a module of its own runs from that module |
| `plant` | negative controls; inputs `{"target", "env"}` | runs the target by name with `env` set, which breaks what the target checks; passes only when the target runs and fails |

## How each kind of test fits

Every test of every kind is a registry row run by this driver; a kind that fits
no type gets a new type and its adapter contract here. A workflow job will only
call the driver, and a make target or script that checks something becomes an
adapter step for a named test.

| kind | type | scope | key example | adapter contract |
|---|---|---|---|---|
| language code over a corpus row (conformance rows, hostile inputs, packet-write-refusal lines, bit-phase family rows, JSONTestSuite files) | roundtrip, encode, decode, write-refusal, load-refusal, evolution | per language | `roundtrip.align-arrays.phase63-ab-e-acute` | the language's adapter routes the family to its harness; inputs name the corpus row (the files stay where they are; the registry indexes them) |
| golden | golden | per language | `golden.wire.wire-go` | generate schema S for L, compare with the pinned file; `-update` re-pins only through a run of that test; a missing pin is MISSING |
| codegen builds | codegen | per language | `codegen.examples.monster` | generate, build, report the compiler's verdict |
| lint and strict-analyse gates | lint | per language | `lint.generated.packet-corpus` | run the analyser over the named tree; any finding fails |
| fuzz, soak, resource bounds | fuzz, soak, resource-bounds | per language | `fuzz.wire.seed-1` | seeded and bounded by inputs; the seed and budget are inputs, never time |
| differential | differential | per language or none | `differential.squirrel.all-port` | run both sides, compare |
| negative control | plant | none | `plant.align-arrays.go-valid-row-run-as-refuse` | `plant`: target name plus the env that breaks it; passes only on the target's red |
| TLA model check, trace check | model | none | `model.tla.reliable-endpoint` | TLC on the named model and config; a trace check feeds a code trace to TLC; a counterexample fails |
| benchmarks and timing gates | bench | per language or none | `bench.packet.write-ns` | measure, compare with the recorded bound in inputs |
| compiler and IR | compiler | none | `compiler.check.reserved-words` | `gotest` |
| Go tests, subtests, table rows | the test's own type | none | `tooling.testreg.wildcards-expand-to-an-explicit-list` | `gotest`, one row per top-level test, or one per subtest with `-run '^T$/^sub$'` |
| CI-policy, docs, roadmap, release checks, workflow scripts that pass or fail | tooling | none | `tooling.ci.every-nightly-only-gate-has-a-pr-tier-form` | `gotest`, or an exec adapter wrapping the script with the protocol |

## Every test is in the registry

`schema-test extract` (package `internal/testreg/extract`) finds every test the
repository holds by reading its source and data. It runs nothing and builds
nothing. These are the kinds it finds, each exactly:

| kind | what counts as one |
|---|---|
| `go-test` | a top-level `Test`, `Fuzz` or `Benchmark` function. Subtests and table rows are cases inside it and run with it. |
| `make-target` | a rule named as a check (`test-`, `check-`, `verify-`, `-check`, `-test`, `-gate`, `-probe`, `-negative-control`), or a rule a workflow runs by name |
| `negative-control` | a target of `make/negative-controls.json` |
| `workflow-job` | a job of a workflow |
| `data-case` | a line of a `.jsonl` file, or a data file grouped by stem, under `test/` or a `testdata` directory |
| `driver` | a directory under `test/` holding code that is not Go: a language's harness |
| `model` | a TLC configuration under `tla/` |

For each Go test it also records the facts that decide its tier, each through
the package's own helpers:
- its build constraint;
- whether it opens `slowtest.Gate` or reads `SCHEMA_SLOW`, which makes it
  functional;
- every `Skip` it can reach, with the condition around it.

The driver has no skip verdict. So when a test moves in, each of these facts
becomes either its tier or a tool its runner must carry.

Every test the extract finds is either a registry row or counted in the unmoved
ledger, [`test/registry/unmoved.tsv`](../test/registry/unmoved.tsv). The extract
reads the files git tracks or would track: what the repository's `.gitignore`
files ignore (build output, a build's lock files, scratch) is not a test. A
ledger line is a kind, a place, an exact count, the layer that moves those
tests in, and the reason they wait.
Two checks hold this:

- `TestEveryTestIsARowOrInTheLedger` (unit tier, every pull request) holds the
  repository, the registry and the ledger to each other exactly. It goes red on:
  - a new test without a row;
  - a count that is too high or too low;
  - a stale line;
  - a `gotest` row naming a test that does not exist;
  - a file that does not parse.
- The `schema-test` job of ci-fast.yml holds the ledger to main's copy, and it
  only shrinks. A new test gets a registry row, never a ledger line. The job
  also extracts main's tree (`--since`), so a new test without a row is red
  by its own name, not only by its place's count.

A skip that hides a red is in the ledger too. A `skip-red` line names one Go
test (`<package dir>:<TestName>`). Its count is the number of `Skip` sites the
extract finds in that test. Its reason names the red the skip hides and the run
that measured it. Check holds the count to the extract, so the line must
shrink as the test's skips go, and be deleted when the last one goes. Against
main's ledger, a `skip-red` line may not be added or grow, and it may not leave
while its test still skips. A test renamed or moved takes its line along: set
the place to the new name. The line may leave with its test only when no test
reaches its skips (the same file and condition) any more. A skip that hides a
red is therefore never a silent pass. The extract sees only the skips a test
reaches through its own package; a skip inside another package's helper (for
example `slowtest`) is not counted, so such a red cannot be ledgered until
that helper's skip moves into the test.

`schema-test extract --json` prints every test, one JSON line each.
`--write-ledger` rewrites the ledger to what is unmoved now, and keeps every
`skip-red` line as it is. It refuses to write while any file does not parse.
Use it only when tests move in or go away.
`--move L` gives every test on layer L's ledger lines that the driver can
run its registry row (a language-free Go test becomes a `gotest` row, tier
from its gates) and rewrites the ledger.

A registry row's test never skips when the driver runs it: the gotest adapter
has no skipped verdict, so a skip, or a subtest's, fails by name.
`TestEveryTestIsARowOrInTheLedger` refuses a row whose test opens the slow
gate (it needs a toolchain) with tier unit.

## Sharding: the per-PR shards, by measured seconds

This is the one sharding design for every per-PR group. Other layers
(L6's make and golden rows among them) use it as it is. Its API and its
seconds format stay stable. A change to either changes this section in the
same commit.

**The format.** A `test` line may end with `"seconds":S`. S is that test's
measured run time in seconds, as a JSON number, on a fleet runner where one
was measured, else on a bench (the run that measured it is named in the
commit that wrote it).
- A test with no `seconds` weighs `testreg.DefaultSeconds` (1 s).
- A measured test weighs at least 0.01 s.
- Every instance of a per-language test weighs its row's seconds.

`schema-test run ... --write-seconds` writes the field for every passing
test it ran. `schema-test seconds LOG...` writes it from the logs of driver
runs (outcome lines such as `PASS <name> (1.23s)`). Neither tool touches any other byte of the
file (`TestWriteSecondsKeepsEveryOtherByte`).

**The API** (package `internal/testreg`):
- `Weight(Instance) float64` is what one instance costs a shard.
- `Parts(instances []Instance, n int) [][]Instance` cuts the expanded list, in
  registry order, into n contiguous parts. The heaviest part is as light as a
  contiguous cut allows, and the rest are as even as that allows. Every
  instance is in exactly one part (`TestShardsCoverEveryNameExactlyOnce`), and
  one heavy test leaves no part empty (`TestAHeavyTestLeavesNoPartEmpty`).
- `PartSeconds([][]Instance) []float64` is each part's weight.
- `schema-test run --group G --shard i/n` runs part i of n.

**The budget.** A per-PR shard carries at most 60 s of measured tests (the
two-minute job less its setup). `TestThePRShardsFitTheirJobs` holds ci-fast's
`schema-test-pr` matrix to exactly 1/N..N/N, with every part within the
budget, and N is the least count that fits: the same list cut into N-1 parts
must overflow. When the pr group grows past N parts, add shards; when it
shrinks or speeds up so that N-1 fit, remove one. Either way the test is red
until the matrix changes.

**A test heavier than one shard** is red by its own name. The fix is to split
it into rows that each fit, such as one row per package or per make target:
- `go vet ./...` becomes a `go vet` row per package.
- A gate over every target becomes one row per target.

Moving such a test to nightly to get it under the budget is not a fix. Its tier
says what the test needs, never what fits.

## Adding a test

Add a `test` line. A corpus row of a family a harness already runs needs nothing
else: every ready language runs it on the next pull request, and every other
language shows it MISSING. A language that cannot express it adds an
`unrepresentable` line with the reason. A Go test in the driver's packages needs
its `gotest` row, or `TestEveryDriverTestIsRegistered` is red. Any other new
test needs its row too, or `TestEveryTestIsARowOrInTheLedger` is red (see
"Every test is in the registry"). Then regenerate the spec addenda (below) in
the same change.

## The spec addenda

The registry is also part of the specification. `schema-test addenda` writes it
out under `docs/`: [SPEC-TESTS.md](SPEC-TESTS.md), the adapter contract (how an
adapter receives instances, invokes its language's code and returns results),
the languages, the types with each one's contract, and the stored queries; and
one page per tier row (`SPEC-TESTS-UNIT.md`, `SPEC-TESTS-FUNCTIONAL.md`,
`SPEC-TESTS-NIGHTLY.md`) listing every test of that tier with its full name,
type, what it checks (`checks`), its inputs and its expected result. Together
they let a new language implement every test, write its adapter and run under
this driver from the spec alone.

- The pages are generated, never edited. `TestTheSpecAddendaAreCurrent`
  regenerates them in memory in the `pr` group and fails, naming the page and
  line, on a hand edit, on a row added without regenerating, on a missing page
  and on a page the registry no longer generates.
- Generation refuses, by name, a test with no `checks` or `expect` text and a
  type with no `contract`.
- Inputs are written as the row holds them; a value longer than 96 bytes is
  named by the row field that holds it, never copied. Large data stays in the
  file the row names.
- A refusal's expected result is its verdict only (refused on load, or refused
  on write under the language's write rule). Reason text is never written.

## Adding a language

Add one `lang` line with `"adapter": ""`. From then on every per-language test
has an instance in it, each MISSING: `coverage` shows the column red and the
nightly run fails on every one. That red count is the language's remaining work.
When its adapter lands, set `adapter`: the `pr` group (`--ready`) then runs it on
every pull request.

## CI

- Per PR: ci-fast.yml job `schema-test` prints `coverage` and runs `extract`
  against main's ledger. Job `schema-test-pr` is a matrix of shards, each
  `schema-test run --group pr --shard i/N`: unit and functional, every ready
  language, every language-free test. `TestThePRShardsFitTheirJobs` holds the
  matrix to exactly 1/N..N/N, each shard within its 60 s of measured tests.
  A workflow job that runs the driver is not itself an unmoved test: its tests
  are the rows it selects.
- Nightly: `schema-test-nightly.yml` runs `--group nightly` (every tier, every
  language, no timeout) and names every MISSING instance red. It is red by
  intent until every language has an adapter: its MISSING list is the
  per-language work list that test-registry/ci-through-driver,
  test-registry/union and test-registry/fix-missing close. Only a red name
  outside that list is a regression.

## What the tracer taught

Family `align-arrays` of `test/verify-bitphase` (23 roundtrip rows and 13
load-refusal rows), through the `go` and `lua` adapters, plus a plant for each.

- **The wrapped drivers stop at their first failing row.** A per-instance verdict
  needs the harness to say which row failed. The adapter reads the driver's
  `FAILED: <verb> <file>` line, records that row failed and the rows before it
  passed, and runs the rest again; a failure that names no requested row is a
  crash. Drivers that report every row would make this one run.
- **The case data lived in Go code.** The rows are built bit by bit in
  `alignarrays.go`. The registry carries each row's bytes as inputs, and both the
  harness (at run time) and `TestTheRegistryHoldsTheAlignArraysRows` (per PR)
  refuse a disagreement. For corpora already on disk, inputs can point at the
  file and need no copy.
- **Every harness call needs its own work folder.** A plant runs its target
  while the same language's own instances run; one shared folder broke both runs
  on the first try, and the driver named it as a crash, not a pass.
- **The driver's own tests belong in the registry too**: they run through the
  `gotest` adapter in the same `pr` group, so the per-PR job is one command.
- **Plants are tests like any other**, which needs one hook per harness (here an
  environment variable that turns one valid row's expectation around). Real
  sabotage of generated code (tools/sabotage) fits the same `plant` contract.
- **The nightly red list is the coverage matrix.** Every language without an
  adapter shows 36 MISSING (23 roundtrip, 13 load-refusal), and that is the work.
- Cost: the `pr` group (`schema-test list --group pr` prints its count) runs
  in about 7 s warm on a bench.

## Next layers

Each layer is one pull request, and each moves its tests out of the unmoved
ledger and into rows:

- **L3 (done):** the language-free Go tests, through `gotest`, batched by
  package. Left in the ledger:
  - Go benchmarks (L6, the bench adapter).
  - Go tests in modules of their own beside generated Go (L5).
  - Go tests behind a leg's build tag (L5).
  - Go tests built only for another platform (L6).
  - The tests that still skip without a build, environment or volume a make
    target or runner provides (L6).
  - The tests red once their slow gate opens (`redSlowGate`, each with its red).
  - The one-test gates heavier than a shard (`heavyGate`), until each is split
    into rows that fit.

  A test that leaves either list is counted on its package's L3 line by
  `--write-ledger`, and `--move L3` gives it its row.
- **L4:** one adapter per language over the conformance drivers' protocol,
  corpus families first.
- **L5:** the per-language Go tests, through each language's adapter.
- **L6:** plants, make gates, goldens, lint, fuzz, benches and TLC models.
- **L7:** CI generated from the registry.

An old CI job is retired only after a run at one SHA shows the driver ran
every test that job ran, in the same tier, with the same count.
