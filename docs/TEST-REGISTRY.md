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
no reason), any `owed` line (there is no exemption list: a test a
language fails is an ordinary red row), and a group whose query is unknown or
matches nothing.

## Names

The instances are generated, never listed: every per-language test times every
`lang` row, less the pairs declared unrepresentable, plus one instance of each
language-free test.

- A per-language instance is `<lang>.<tier>.<type>.<family>.<case>`, for example
  `go.functional.roundtrip.align-arrays.phase63-ab-e-acute`.
- A language-free instance is `none.<tier>.<type>.<family>.<case>`, for example
  `none.unit.tooling.testreg.shards-cover-every-name-exactly-once`. `none` is a
  scope, not a language: it is never counted with the languages.
- There is no owed list (Glenn, 2026-11-25: no skips and no exemptions). A
  test a language answers wrongly runs in its tier and is red by name until
  it is fixed.
- A declared-unrepresentable pair has no instance. It is data, shown in the
  coverage matrix and counted, never a name in a run and never a pass. Running it
  by name fails ("declares it unrepresentable, so it has no such instance").

## Running

```
schema-test run  NAME|PATTERN... [--lang L]... [--tier T]... [--type Y]... [--family F]... [--ready] [--shard i/n]
schema-test run  --group G [--lang L] [--shard i/n]
schema-test list (the same)
schema-test coverage
schema-test extract [--json] [--write-ledger] [--shrinks-from LEDGER] [--since MAIN-TREE] [--move LAYER]
schema-test seconds [--registry PATH] LOG...
schema-test langs [--ready]
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
  `--lang L` narrows a group to one language (a CI row); a language the
  registry does not have, or one the group does not name, fails.
- `langs` prints the registry's languages as a JSON list, then `none`;
  `--ready` keeps the languages whose adapter has landed. It is the CI
  matrix, so no workflow writes a list of languages.
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
| an `owed` line: refused, since no list excuses a red test | `TestLoadRefusesAnOwedRecord` |
| an unknown language in a query, or one a group does not name | `TestTheCommandExitsNonZeroOnEveryFailRule` |
| expected != ran | `TestExpectedNotRanFails` |
| an adapter reports an instance it was not asked for | `TestAResultNobodyAskedForFails` |
| an adapter omits an instance it was asked for, or reports one twice | `TestExpectedNotRanFails`, `TestAResultReportedTwiceFails` |
| an adapter crashes | `TestAnAdapterCrashFails` |
| a call the driver's own deadline kills: named a timeout (TIMED OUT), with the call's limit and how long it ran, never a crash, and each row it left unreported fails by name | `TestADriverTimeoutNamesTheLimitAndTheRows` |
| go test's own `-timeout` firing (gotest and make-gotest): the running test's row fails TIMED OUT naming the limit and how long it had run, every row the package never reached fails naming the test that was running, and a test that passed before the limit keeps its pass | `TestAGoTestTimeoutIsNamedNotACrash`, `TestAMakeGoTestTimeoutIsNamed` |
| a make-gotest row: its Go test missing from the target's go test -json events, skipped (itself or a subtest), failed, a run of 0 tests, a Go test the target ran that no row names, or make failing outside every test | `TestAMakeGoTestRowIsReadByItsExactName`, `TestAMakeGoTestTargetWithoutGoTestIsRefused` |
| a make-gotest row whose test started more often than it finished, or whose package failed outside its tests; a skip only make's log reports fails every row, while a skip go test reported is the skipped row's alone; a target running go test outside `$(GO_TEST)` is refused before make runs; make killed by the driver's deadline names the Go tests still running | `TestAMakeGoTestReadsWhatTheEventsSayNotTheLogAlone`, `TestAMakeGoTestTargetRunningGoTestAnotherWayIsRefused`, `TestAKilledMakeGoTestNamesTheTestsRunning` |
| a row that needs a machine property no bench of the run provides, that no one bench provides together with its other needs and GOOS, or whose bench's declaration its probe refused: NOT RUN naming the need, never skipped, and the run exits non-zero (launch, watch, collect and merge all do on any red) | `TestAPlanNeverSendsARowToABenchThatLacksItsNeed`, `TestABenchDeclaresItsPropertiesAndTheProbeHoldsThem`, `TestANeedNoOneBenchProvidesWholeIsNamed`, `TestARunWithANeedNoBenchProvidesExitsNonZero` |
| an adapter times out (the deadline is an argument; the test hands in one already past) | `TestAnAdapterTimeoutFails` |
| a build a call needs (a leg, a harness) runs before any call, outside every call's deadline and under one of its own, the limit a call has (`testreg.Preparer`); a plant prepares the rows it runs; a step already built answers without waiting for the checkout | `TestPrepareRunsBeforeAnyCallOutsideItsDeadline`, `TestABuildStepRunsInPrepare`, `TestAPlantPreparesTheRowsItRunsInTheCheckout`, `TestABuildStepAlreadyRunNeverWaitsForTheCheckout` |
| a build that outlasts its deadline fails every row that needed it, naming the timeout | `TestAPrepareThatOutlastsItsDeadlineFailsByName` |
| a build that fails fails every row that needed it, by name, and its call never runs | `TestAFailedPrepareFailsItsGroupByName` |
| a Go test that opens a build output it does not make itself has that build declared in `test/registry/go-needs.txt` (`testreg.GoNeedsPath`), and every package whose tests open one by path has a line there; the gotest adapter and the union harness build it in Prepare, a failed build fails exactly the rows that read it, by name, and their go test never runs; tools/fullrun builds the same before a Go test the registry does not hold yet | `TestEveryBuiltReadIsDeclared`, `TestGoNeedsSelectByPackageAndWholeTestName`, `TestAGoTestsDeclaredBuildFailsOnlyTheRowsThatReadIt`, `TestAUnionRowsDeclaredBuildFailsOnlyTheRowsThatReadIt`, `TestAUnitBuildsWhatItsTestsReadFirst` |
| the adapters that take the checkout (make, golden, plant, model-check, corpus) run one after another, each deadline starting with its own call (`testreg.Serial`) | `TestSerialAdaptersRunOneAfterAnother`, `TestTheCheckoutAdaptersRunOneAfterAnother` |
| the env plants of one language are one block of a shard, as a golden writer's rows are | `TestAnEnvPlantsRowsShareOneShardByLanguage` |
| each golden writer, each language's env plants, every other plant and every make row is an adapter call of its own, under its own deadline | `TestEachWriterAndPlantIsACallOfItsOwn` |
| a row is weighed by what it cost: its adapter's seconds, else its share of what its call took beyond the rows the adapter timed, plus its share of its group's Prepare; a plant by its baseline and sabotaged run | `TestARowsSecondsAreItsCallsShareAndItsPrepares`, `TestARowsShareIsWhatTheCallTookBeyondTheTimedRows`, `TestAPlantResultCarriesItsMeasuredSeconds` |
| a verdict other than pass or fail: there is no skipped verdict | `TestThereIsNoSkippedVerdict` |
| a harness that logs a skip | `TestAHarnessSkipLineFails` |
| a Go test that skips | `TestGoTestEventsGiveOneVerdictEach` |
| the command's exit status: unknown name, 0 matches, no adapter, unknown group, empty shard | `TestTheCommandExitsNonZeroOnEveryFailRule` |
| the command's exit status on an adapter crash, timeout, stray result, omitted result and missing instance | `TestTheCommandExitsNonZeroOnEveryAdapterFault` |
| a run that selects nothing still prints its counts line, every count 0 | `TestARunThatSelectsNothingStillPrintsItsCounts` |

The loader's refusals have one test each (`TestLoadRefuses*`). `TestTheDriverHasNoSkips`
refuses any `Skip`, `SkipNow`, `Skipf` or `testing.Short()` in the driver's own
packages, and finds a planted one. Its repo-wide form is `TestNoSkipMechanismAnywhere`
(see "No skips" below).
`TestEveryDriverTestIsRegistered` holds every Go test in the driver's packages
to a registry row, and every such row to a test that exists.

## Tiers

- **unit**: no real time, no sockets, no network, no outside services or
  toolchains beyond the Go test binary; stays inside its working directory.
- **functional**: uses a real thing: a compiler or VM, generated code built and
  run. Per pull request, on runners that provide the toolchain. A missing
  toolchain there is a failure, never a skip.
- **nightly**: heavy or long: long fuzz, full matrices, soak, timing.

Where a test skipped because it needed a toolchain or a long run, that
condition is now its tier. Choosing what runs is the query, stated up front.

### Machine needs

A test that needs a property of the machine it runs on, beyond its tier's
toolchains, says so: `"needs": ["casefold"]` on its row (lowercase names,
each once; `Load` refuses anything else). `casefold` is a volume that folds
names, the fleet's `/srv/casefold/scratch` (or `$SCHEMA_CASEFOLD_DIR`;
`internal/testvolume`); captain's runners, Linux under WSL2, have no ext4
casefold and cannot provide it.

A bench declares what it provides where tools/fullrun reads machine facts,
its `--bench` value: `--bench HOST=SHARDS+casefold` (a word after `+` is a
property fullrun knows, or a GOOS). Before the plan is dealt, fullrun holds
each declaration to the machine (`fullrun probe casefold` on the bench), and
one the probe refuses is dropped with the probe's words. The plan never
deals a unit to a bench that lacks one of its needs; a unit whose need no
bench of the run provides is NOT RUN, naming the need, the refusals and the
`--bench` word that declares it. A unit's needs are the union of its rows'
(the Go tests of one package, or the rows of one make target, are one
unit). The nightly part jobs are generated (`schema-test ci`) with every
need the registry holds as a runner label (`runs-on: [..., schema,
casefold]`) and as a declared property (`--bench local=2+casefold`), so a
part lands only on a runner that carries the label, and fullrun still
probes it there. Captain's runners do not carry `casefold`.

## No skips

Glenn, 2026-11-25: "NO SKIPS", and "if you don't want to run a test, don't run
it (i mean at the root)". The only place a test is left out is the root: the
driver's query, expanded to names before the run. Below it nothing decides: no
test, helper, adapter, driver, script, Makefile recipe or workflow step checks a
condition and does not run, or turns a failure into a pass. A missing toolchain,
runtime or built artifact fails the test naming what is missing
(`slowtest.Tool`, `slowtest.Program`, `slowtest.Missing`); `slowtest.Gate`,
`SCHEMA_SLOW`, the `SCHEMA_REQUIRE_*` lane promises, the skip ledger and the
harness's `--skip` are gone.

`TestNoSkipMechanismAnywhere` (unit, every pull request) scans the repository
and names, by file, line and rule, every skip mechanism: in Go, a `Skip` call
on a testing value or a test double, `testing.Short()`, a skip or lane
variable, an early return on an environment, tool, file or platform check, and
a build tag that leaves a test out; in scripts, recipes and workflows, `|| true`,
`continue-on-error`, `if: false`, a branch-name condition, `go test -skip` or
`-short`, a skip variable and a skip announcement; in a driver of any language,
a skip verdict; and any skip ledger file. `TestNoSkipMechanismIsPlanted` plants
one of each and requires each named. The files it does not read are listed with
their reasons in `internal/testreg/noskip_test.go`.

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
| `corpus` | every language row: each per-language family a harness exists for, routed by family | below |
| `gotest` | language-free Go tests; inputs `{"package", "test", "subtest", "files", "make"}` (`make` optional: a make target, such as a generated tree's stamp, that Prepare makes once before any call) | `go test -count=1 -json -run '^(A|B|...)$' <package>` per package, cut into calls of at most `testreg.ShardBudget` measured seconds, run one after another within a package and several packages at once, each given the driver's remaining deadline less a 10 s margin as `-timeout`; a row a call never reports fails by name, and a call that exits non-zero with every test passed (a `TestMain` check) fails its rows, every event mapped to its row by full name. A row that names subtest runs `-run '^Test$/^(sub)$'` and takes that subtest's own pass or fail; the parent row still claims the top-level test. That call uses the same non-zero-exit rule: when every asked subtest passed, those rows fail with go test's words, and a subtest that failed does not flip a sibling that passed (a test's tier says what it needs; nothing in a test decides whether it runs). A test or subtest that skips fails; a row whose test is not in its package's source is MISSING; one that never reports fails. A package in a module of its own runs from that module. A file in `files` that the tree does not hold fails that row by name before go test runs. A row that does not name a subtest is still the top-level test, subtest events included |
| `plant` | negative controls; inputs `{"target", "env", "reason"}`, `{"edits", "red", "run", "reason"}` , `{"make", "file", "group"}` or `{"credits", "delete"}` (a read plant: see "Read credits") | env form: runs the target with `env` set; edit form: applies each edit (its anchor exactly once, every copy reached) to a copy of the tracked tree and runs the named tests there, so each builds with its own leg's flags. Passes only when every named row passes on the tree as it is, and then under the sabotage exactly the named targets fail with a reason whose first line holds `reason` (16 characters or more), every other name passes and the run has no run-level problem: a red for another cause (a missing tool, a broken build), or a row red already, fails the plant. The edit form's copy is a git worktree of HEAD with the checkout's files over it and the checkouts beside the checkout (`../serialize.go`) linked beside it, its run is that tree's own schema-test, and its verdict is read from that run's `--report` JSON. A make row cannot be named red (its reason is its exit status, which any failure gives); a copy of an anchor in a file of the same source family (C and C++ sources and headers are one) that no edit names fails the plant. Make form: a control of `make/negative-controls.json`, whose recipe holds its own gate red |
| `make` | make gates; inputs `{"file", "target"}` plus optional `"vars"` (make variables on the command line), `"tools"` (programs that must be on PATH) and `"siblings"` (checkouts such as `../serialize.go`) and `"reads"` (the data cases the target runs, which the extract counts covered beside any golden row of the same case) | the target must be a rule its file writes, else FAIL; a missing tool or sibling FAILS naming the runner before make runs; the target's dry run (`make -n -B`) must name every file of `reads` and run no `go test` without `-json` (plain go test output prints nothing for a skipped test), else FAIL before it runs; `make -jN target VAR=value...` passes on exit 0 with no skip in the log and no line of go test's plain output |
| `make-gotest` | the Go tests a make target's recipe runs, one row per test; inputs `{"file", "target", "package", "test"}` plus the make row's optional `"vars"`, `"tools"` and `"siblings"` (no `"seed"` and no `"reads"`) | the rows of one target are one call and one fullrun unit: the make row's checks hold first, and the target's recipes must run go test through `$(GO_TEST)`, else every row fails before make runs; make runs the target once with `SCHEMA_GOTEST_JSON` naming `build/registry-gotest/<target>.jsonl`, to which `$(GO_TEST)` appends its go test -json events, and each row's verdict is its own test's, read by import path and exact name: a test the events do not show run, a skip of the test or a subtest, a failure, a run of 0 tests, or a Go test the target ran that no row of it names fails by name; go test's own timeout fails as the gotest adapter's does; make failing with every row passed fails every row with make's words. The extract credits each row with its one Go test and the target with the rows together, never the package |
| `golden` | goldens and pins; inputs `{"writer", "files"}` | the `pins-*` writer runs once per run in a copy of the tracked tree, with every file its rows name aged first so a file it leaves untouched shows it was not written; each file is compared byte for byte; a file it no longer writes, or wrote differently, FAILS; a pin it writes that no row names FAILS every row of the writer; a row whose file is not committed is MISSING; a writer that fails FAILS its own rows with its error, and the other writers' rows are judged as usual; `run --update <names>` re-pins only those rows. A copy a killed run left beside the checkout (named with its process id) is removed by the next run |
| `model-check` | TLA+ model checks (type `model`); inputs `{"config", "workers"}`, or `{"controls": true}` | `go build ./tools/tlcrun`, then `tlcrun -adapter`: TLC over `tla/<config>`, judged against its `tla/RUNS.tsv` row (an exhaustive run explores the recorded distinct states with no error; a reversed witness violates exactly the invariant its row names). The controls row runs the judge's three broken-tree controls. No TLC jar or no java fails every instance naming the runner. `SCHEMA_TEST_TLC_PLANT_APPEND` is the plant hook: its text is appended to the config in a scratch copy of `tla/` |

The `corpus` adapter's harnesses, by family:

| family | harness | build | inputs |
|---|---|---|---|
| `align-arrays` | `verify-bitphase --adapter --lang L` | `make bin/schema build/verify-bitphase-bin` | the row's bytes (alignarrays.go) |
| `conformance-<surface>` | `build/conformance-harness-vm adapter --lang L` | `make conformance-leg-L` | `surface`, `case` (the file the driver writes), `files` (the corpus files the case reads), `driver` |
| any family whose rows' inputs carry `tests` (a union row) | `go-union`: L's own Go test, through the `gotest` adapter's calls | none (the runner provides L's toolchain, as the test's tier says) | `tests`: `{"<lang>": {"package", "test", "tags"}}` (`tags` optional) |
| `tables` | the driver's own generation step (tools/schema-test, tables.go): `bin/schema generate --lang L` for the case's schema (moo through a `schema_leg_moo` build) | `make bin/schema` | `schema`, `files` (the corpus files the case reads), `driver`, `verdict` (the corpus's accept, refuse or present) |
| `feature-witnesses` | the family's harness in tools/schema-test: L's own Go test(s) for the corpus directory, through the `gotest` adapter's calls | none (the runner provides L's toolchain, as the test's tier says) | `dir`, `files` (every file of the directory), `answers`: `{"<lang>": ["<Go test>", ...]}` in `internal/codegen/<lang>table` |

### The tables conformance corpus

Every case of every surface the tables conformance harness scores
(test/conformance/README.md) is one registry test,
`<type>.conformance-<surface>.<case>`, answered by every language through its
`test/conformance/<lang>/driver`. The corpus stays where it is
(testdata/conformance/tables, testdata/wire/tables, the units' schemas): a
row's inputs name the files it reads, and `schema-test extract` counts those
files and the drivers as the registry's, not the ledger's.

- `harness adapter --lang L` reads the request lines, runs L's leg over every
  surface exactly as `harness run --only L --claims` scores it, and answers
  each requested case from that run. The leg always runs whole: a refusal row
  is credited only beside its lawful twin, which may sit on another surface,
  so a partial run would score a case differently. A run is seconds once the
  leg's driver is built; the build (`make conformance-leg-L`: the harness
  built with every leg's tag, and what `make conformance` builds for L) is the
  cost.
- A case passes when the leg's answer is the corpus's (or the row's named
  writer refusal). An absent answer, a surface the leg does not list, a
  refusal with no answered twin, a wrong answer, a claimed case answered
  absent, a surface failure that names no case, and a case the harness does
  not have each fail, naming which.
- `harness registry-rows` prints the rows, and
  `TestTheRegistryHoldsEveryConformanceCase` holds the registry to them field
  for field: a corpus case with no row, or a row with no case, is red.
- A case a language answers wrongly today is red in its tier, by name, like
  every other: the harness keeps no list of cases a leg is excused.
- Plants: `plant.corpus-plants.go-wrong-byte-root-full` flips one byte of the
  go leg's wire answer for root_full, and `plant.corpus-plants.go-absent-root-full`
  drops that case from its answers (test/conformance/go/main.go, `plantEnv`);
  each must turn `go.functional.roundtrip.conformance-wire.root-full` red for
  its own reason (the wrong byte: "not the corpus's"; the dropped case: MISSING).
- Retired with this: the `--claims` corpus run inside each
  `_verify-<lang>-conformance` (make/verify-compiled.mk, make/verify-vm.mk),
  which ran exactly these cases, leg by leg. The rest of those targets (packet
  wire, defaults, UTF-8) stays in verify-leg until its own family moves in.

### The test/tables corpus

Every data case of `test/tables` and the places under it that the ledger
counted on layer L4 (the enum-bound shapes, the fnz vectors, `inner`, the
r14 and r17 documents and the value-default documents) is one registry
test, `codegen.tables.<case>`, answered by every language. A case's
answer is its schema generated for the language: a schema the corpus
holds as valid must generate, and one the enum-bound corpus holds as
refused must refuse at check, for the reason `internal/check` holds.
A case that is not itself a schema names the schema it belongs to and
is answered by that generation with its own file present; the view and
control sources are answered by their presence, as the file each
language's own view test reads. `TestTheRegistryHoldsEveryTablesCase`
holds the registry to the corpus field for field. A language whose
emitter refuses a valid schema, or accepts a refused one, is red by
name in the nightly tier.

### Union rows: the per-language Go tests

Before the registry, each language held its own hand-written Go tests of a
case set (internal/codegen/<lang>table and the like), named differently per
language, and a language that never wrote one was silent. A union row holds
the case once and names each language's test that answers it:

```
{"rec":"test","key":"roundtrip.wire-json-integers.wide-integers-are-strings", ...,
 "inputs":{"tests":{"go":{"package":"./internal/codegen/gotable","test":"TestGoJsonWideIntegersAreStrings"},
                    "lua":{"package":"./internal/codegen/luatable","test":"TestLuaJsonWideIntegersAreStrings"}, ...}}}
```

- It is a per-language row: every language row has an instance. The
  `go-union` harness runs a language's instance as that language's Go test,
  by its exact name, in the `gotest` adapter's `go test -json` calls (cut by
  measured seconds, a package in a module of its own run from that module,
  `-tags` when the language's entry names them). A skip fails, as everywhere.
- A language the row names no test for is red, naming the languages that do
  answer the case: the case is unanswered there until its test is written.
  A named test its package's source does not declare is red as MISSING.
- `testreg.Load` refuses a union row naming a language the registry does not
  list (or `none`), a Go test two union rows name, inputs that do not read,
  and a language-free union row (`TestLoadRefusesUnion*`,
  `TestLoadRefusesAGoTestTwoUnionRowsName`). `schema-test extract` credits
  each named Go test to its row, so it leaves the ledger
  (`TestAUnionRowCoversEachLanguagesGoTest`).
- A row's tier is the highest of its tests' needs (a toolchain makes it
  functional), and its seconds the slowest language's.
- Moving a test in is a move, not a rewrite: the Go test is unchanged, and
  only the row is new. Families are named for the data set or behaviour
  (`wire-<stem>` for a testdata/wire stem).

### Language-intrinsic tests

A test that exists in one language only is sorted by its subject (Rowan's
ruling, 2026-11-25, easy for Glenn to overrule):

- **Format behaviour every language must show** (a refusal, a wire or JSON
  rule, a limit, a round trip, a corpus, evolution, damage, a differential
  against the engine, or a negative control of one of these) is a union row
  that names only that language's test. Every other language is red until it
  answers the case. These reds are the point: they are the parity gaps.
- **Intrinsic to that language's own generated source or runtime**, with no
  counterpart elsewhere (a Lua string-escape emitter, a C# attribute shape, a
  Rust lifetime, a formatter's or analyser's verdict on the generated code,
  module layout, allocation counts, a timing bench of the language's own code
  shapes): the row carries `"intrinsic":{"lang":L,"reason":R}`. It has L's
  instance only. Every other language is declared unrepresentable with R, in
  `schema-test coverage` and the spec addenda's "Intrinsic to one language"
  tables. It runs and is red when it fails. It is no exemption: the subject
  does not exist elsewhere.
- When in doubt, a test is format behaviour. A cold reader checks the split.
- `testreg.Load` refuses an intrinsic row:
  - naming a language the registry does not list;
  - with an empty or boilerplate reason (under four words, or fewer than three
    distinct words that are not filler such as "language-specific", "only",
    "code", "TODO", "placeholder"),
    so every reason says what the subject is;
  - that is language-free, or is not a union row (only a row naming the
    language's own Go test can be intrinsic);
  - whose inputs name another language's test, or not its own;
  - whose family is not named `<lang>-<subject>` (an intrinsic family is one
    language's), or whose case another language's intrinsic row also has (a
    case two languages answer is one union row, and splitting it would drop
    every other language's red);
  - with an `unrepresentable` line of its own.

  A count of words cannot judge whether a reason is true: the cold reader of
  the split does.

  These are `TestAnIntrinsicTestHasOneInstance` and `TestLoadRefusesABadIntrinsic`.
- A union row takes no `unrepresentable` line: a language with no Go test
  for the case is red, never left out
  (`TestLoadRefusesAnUnrepresentableLineOnAUnionRow`).

### The hostile corpus

Every case of the shared hostile corpus (internal/ci/hostilecorpus/corpus,
docs/HOSTILE-CORPUS.md) is one registry test per language:
`<type>.hostile.<case>` for manifest.tsv, the corpus's one manifest. The
copies of the wide integers written as bare number tokens
(testdata/conformance/tables/JSON-HOSTILE-NUMBER-TOKEN.txt) are cases of it
like any other. The type is load-refusal for a refuse verdict and decode for
an accept. The rows are nightly: each needs the language's toolchain, and
each run is the whole manifest (tools/schema-test/hostile.go).

- A language's instances are answered by one run of its make target
  (`make schema-<lang>-runs-hostile-corpus`), which runs every case once.
  The run prints a block of verdict lines (`FAIL <case> ...`,
  `hostile-ledger ran <case>`, `hostile-ledger end`); a runner written as a Go
  test writes the same lines to the file `$SCHEMA_HOSTILE_VERDICTS` names
  (hostilecorpus.WriteVerdicts). A case passes when the block says it ran and
  no FAIL line names it. Lines after the last `end` are the target's own
  controls, never the corpus run's.
- A case no line names fails ("gave the case no verdict"). A run with no
  block (a target that does not exist yet, a build that broke) or with a line
  in its block that is no case's verdict (a FIX-CARD line, a ledger that
  did not close) fails every asked case with what the run said. A target that
  exits non-zero with no case failing by name is the run's error.
- No case runs twice: the per-case rows are the make target's only rows, and
  their inputs name the Go test a Go-written target runs (`go_tests`), so
  `schema-test extract` counts the target and the test as the rows'.
- `TestTheRegistryHoldsEveryHostileCase` holds the rows to the manifests field
  for field (`-update-hostile` rewrites them). A row whose case copies a
  bare number-token tree names that tree (`files`), so the tree is a data
  case the registry reads (`TestNumberTokenCasesAreRowsThatReadTheirTrees`).
- Retired with this: the per-language schema-<lang>-runs-hostile-corpus.yml
  workflows and schema-shared-hostile-corpus.yml; each control is its own
  registry row.


### The feature-witness corpora

Every directory of the feature-witness corpus
(testdata/conformance/feature-witnesses) is one registry test,
`roundtrip.feature-witnesses.<dir>`, answered by every language through
its own Go test in internal/codegen/<lang>table. The corpus stays where it
is: a row's inputs name the directory and every file in it, and
`schema-test extract` counts those files as the registry's (a row naming a
`.jsonl` file reads every line case in it). The `full` directory's cases
are the tables corpus's, and two directories take no row here
(compressed-f32-ends and compressed-negzero-go are answered per language
by the compressed-float union rows; their files are indexed by the Go
engine's gotest rows for the same corpora, which name every file).

- The family's harness (tools/schema-test, featurewitness.go) runs a
  language's instance as that language's Go test(s) for the directory
  (inputs `answers`), through the gotest adapter's `go test -json` calls. A
  corpus answered by two tests passes only when both do.
- A language with no Go test for a directory is red by name, naming the
  languages that do answer it; a named test its package's source does not
  declare is red as MISSING. These reds are the parity gaps.
- `TestTheRegistryHoldsEveryFeatureWitnessCase` holds the rows to the
  corpus field for field (`-update-feature-witnesses` rewrites them): a
  corpus directory with no row, a row with no directory, a row whose files
  are not the directory's files, and a held-elsewhere directory with no
  answering row are each red.

### The JSONTestSuite corpus

Every file of the vendored JSONTestSuite corpus
(testdata/jsontestsuite/test_parsing, docs/SPEC-TABLES.md section 16.12)
is one registry test per language: `<type>.jsontestsuite.<case>`. The
type is decode for a file the JSON layer accepts and load-refusal for
one it refuses: a y_ file accepts, an n_ file refuses, and an i_ file
takes the verdict its section 16.12 row declares. The rows are nightly:
each run is the whole corpus (tools/schema-test/jsontest.go).

- A language's instances are answered by one run of its whole-corpus
  Go test, which runs every file once: go by
  internal/codegen/gotable's TestJSONTestSuiteGenerated, python by
  internal/codegen/pythontable's TestPythonJsonScanPolicy, moo by
  internal/codegen/mootable's TestMooJSONTestSuite and luau by
  internal/codegen/luautable's TestLuauJSONTestSuite. Each instance's
  verdict is the case's line in that run: a case passes when the run
  names no disagreement for it, and a mismatch line and a pending line
  each fail it, since the registry allows no pending list. A run that
  never reports its test, or that fails with no case failing by name,
  fails every asked case with what the run said.
- A language with no whole-corpus test yet gives each case no
  verdict, and each instance fails by name (no JSONTestSuite runner
  for the language). Those reds are the per-language work list: the
  case is unanswered there until its test is written.
- A case is its file name without .json, lower case, each underscore
  a separator and each hyphen, dot, plus and hash named (dash, dot,
  plus, hash), so two files never share a case by folding alone;
  jsontestKeys refuses any two that still meet, naming both. A row's
  inputs name its one corpus file in files, and schema-test extract
  indexes that file where it stands.
- TestTheRegistryHoldsEveryJSONTestSuiteCase holds the rows to the
  corpus field for field (-update-jsontest rewrites them, keeping
  each row's measured seconds). The loader checks every byte against
  SHA256SUMS, so a removed case file is refused by name there and by
  the held test's row with no case.
- Plants: SCHEMA_TEST_PLANT_JSONTEST_<LANG> as flip:<file> turns
  that case's answer around and as absent:<file> drops it; the
  driver must turn exactly that instance red, by its own reason.
- Retired with this: nothing. The whole-corpus Go tests are the
  runners this harness runs, and the make gate that also runs one
  (make/luau.mk's tables-luau-jsontestsuite) stays until L5 counts
  its run against the driver's.

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
| negative control | plant | none | `plant.align-arrays.go-valid-row-run-as-refuse` | `plant`: target name plus the env that breaks it and the reason it fails for; passes only on the target's red for that reason |
| TLA model check, trace check | model | none | `model.card-hops.divergent-leg` | `model-check`: TLC on the named config, held to its `tla/RUNS.tsv` row (family: the module; case: the config's variant, `holds` for the exhaustive one); a trace check feeds a code trace to TLC. Nightly: the configs together take minutes, past a two-minute job |
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

For each Go test it also records its build constraint and every `Skip` it can
reach through its package's own helpers, with the condition around it. Check
refuses every such skip: the driver has no skip verdict, and nothing below the
root decides whether a test runs.

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

A Go test that has no row yet and is not unit has a tier line in the ledger:
`tier <package dir>:<TestName> 1 <layer> functional: <what it needs>` (or
`nightly: ...`). It is where a removed skip's condition went: a test that
needed a toolchain, a sibling runtime or a built corpus is functional, and one
that needed a long run is nightly, so the root selects it by tier and nothing
inside it decides. Check holds every tier line to a Go test that exists and has
no row (a renamed test takes its line along; a test that gets its row drops it,
since the row carries the tier). A tier line is a fact, not a count, so it may
be added or removed against main's ledger. There is no line that permits a
skip: the `skip-red` kind is gone, and every test it held now runs and is red
until it is fixed.

`schema-test extract --json` prints every test, one JSON line each.
`--write-ledger` rewrites the ledger to what is unmoved now, and keeps every
tier line whose test has no row yet (a moved test's row carries its tier).
It refuses to write while any file does not parse.
Use it only when tests move in or go away.
`--move L` gives every test on layer L's ledger lines that the driver can
run its registry row (a language-free Go test becomes a `gotest` row, tier
from its gates) and rewrites the ledger.

A registry row's test never skips when the driver runs it: the gotest adapter
has no skipped verdict, so a skip, or a subtest's, fails by name.
`TestEveryTestIsARowOrInTheLedger` refuses a row whose test opens the slow
gate (it needs a toolchain) with tier unit.

### Read credits: proposed by text, proven by a plant

A make, gotest or union row may name in `"reads"` the data cases (and, by a
directory read, the drivers) it reads, and the extract then counts them run.
Text matching cannot show that a row reads a file: only deleting the file and
seeing the row go red can. So a read is a proposal, and the credit counts only
with its plant:

- the row names its read plant in its inputs (`"read_plant":
  "plant.read-credits.<row key, dots as dashes>"`);
- the plant is a `plant` row, tier nightly, inputs `{"credits": <row key>,
  "delete": [[<file>...]...]}`, one deletion per credited item, each that item's
  files (a case's files; a driver's source files);
- the plant runs the row green in the checkout and in a copy of the tracked
  tree, then for each deletion takes its files out of the copy and requires
  every instance of the row red, by name (a make row's red must also name a
  deleted file by its path, since its exit status is what any failure gives),
  then puts them back; with every file back the row must be green again.

`schema-test extract` counts a read's item only where the row's plant deletes
exactly that item's files (`TestAReadCreditCountsOnlyWithItsPlant`); a read with
no plant credits nothing and its item stays in the ledger. A plant off the
nightly tier, one its row does not name, and a deletion that is no credit of its
row are problems by name. A deletion that leaves the row green fails the
nightly naming it: take it out of the plant, and its item goes back to the
ledger at the next extract (`TestAReadPlantIsTheRowRedForEachDeletion`).

`schema-test reads` is the proposer: it regenerates the make rows' file reads
from make's database (files only, never a directory), prints the Go reads a
row's own test function names whole as suggestions (written by hand), and
prints every read credit with no plant. `--plant <row>` writes the row's read
plant from what its reads credit.

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
two-minute job less its setup). `schema-test ci` picks each language's N
from the measured seconds, and `TestTheMatrixIsThePRGroup` holds ci-fast's
shard matrix to exactly 1/N..N/N per language, with every part within the
budget. When the pr group grows, `schema-test ci` adds shards. A test with
no `seconds` weighs 1 s until a run measures it, so a shard of unmeasured
tests is held to the budget only once they are measured.

**A weight is per test, not per call.** A corpus row's seconds are its
language leg's wall time divided by the rows the leg answered, and the
heaviest language sets it. The conformance harness runs a whole leg per call,
so a shard of a language's corpus rows does not take its share of the leg's
time: on amd-2 cpp's whole leg took 192 s, and one quarter of its rows,
with the bench loaded, still ran past the 90 s adapter limit. Until the harness runs only the rows it is
asked for, a language's corpus shards are not bounded by the budget.

**A test heavier than one shard** is red by its own name. The fix is to split
it into rows that each fit, such as one row per package or per make target:
- `go vet ./...` becomes a `go vet` row per package.
- A gate over every target becomes one row per target.
- A Go test over every target becomes one top-level test per target, each
  its own row (L4 splits `TestNameBuildability` into
  `TestNameBuildability_<target>` this way, and owns their rows).

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
When its adapter lands, set `adapter` (`corpus` routes every family a harness
exists for): the `pr` group (`--ready`) then runs every test of its pr tiers
on every pull request, in a CI row of its own, and each one it does not answer
yet is red by name until it does.

## CI

CI is generated from the registry. `schema-test ci` writes
`.github/workflows/ci-fast.yml` and `docs/CI-TIERS.md`; `schema-test ci
--check` (and `TestTheCIFilesAreCurrent`) regenerates both and fails, by file
and line, on any difference, so a hand edit is red.

- Per PR: ci-fast.yml. Job `schema-test` runs `ci --check`, prints `coverage`
  and runs `extract` against main's ledger. Jobs `schema-test-pr-1`,
  `schema-test-pr-2`, ... are one matrix cut at GitHub's limit of 256 jobs
  per matrix (a language's shards kept in one job where they fit;
  `TestTheShardJobsFitGitHubsMatrixLimit`, and the policy names any inline
  matrix past the limit by file and line), with one row per language (and
  `none`) and shard: `schema-test run --group
  pr --lang L --shard i/N`, N the fewest parts that each carry at most 60 s of
  measured tests (L3's `Parts`, the one sharder). `TestTheMatrixIsThePRGroup`
  holds the matrix to the pr group exactly: every name in one job, none
  twice, no shard past its budget. Two jobs build what the shards read and
  run no test: `fixed-corpus` (the go fixed corpus, through the
  `fixed-corpus` setup action) and `produce`, one job per producer (the
  conformance-leg-go and conformance-leg-goose trees the env plants run),
  each built once per run through the `produce` setup action, kept in the
  runner's cache by `tools/produce.sh identity`, and fetched by every shard
  before its run; a shard needs both green
  (`TestThePRShardsRunOnTheProducedLegs`). Job `verdict` is green only when
  every job passed, and the needs hold exactly the shard jobs the registry
  cuts (`schema-test ci --verdict`,
  `TestTheVerdictNeedsEveryJobGreen`); each shard's run already fails on
  expected != ran. A workflow job that runs the driver is not itself an
  unmoved test: its tests are the rows it selects.
- On pull requests that touch the roadmap, and pushes to main, with history:
  roadmap.yml runs the one row that reads the commit
  range (`tooling.roadmap.check`) with full history, because ci-fast's shards
  check out one commit. The row is tier nightly, so the pr group leaves it to
  that job; its control and suites are pr-group rows.
- Nightly: `schema-nightly.yml` (the full-run layer's) runs the nightly group,
  every tier and every language, as `go run ./tools/fullrun launch` parts
  and one `fullrun merge` verdict; it replaces schema-test-nightly.yml, which
  this layer deletes. A language that does not answer a row is an ordinary
  red row by name (there are no owed records: `Load` refuses one as an
  exemption list), so its red list is the per-language work list. Only a
  red name outside that list is a regression.
- Workflows that run no test: cla.yml, release.yml (its fast lane reads a
  green ci-fast.yml run at the commit) and promotion-tag.yml, pinned as
  below.

**Generated or pinned** (`schema-test ci --check`, run by ci-fast.yml;
`TestEveryGitHubFileIsGeneratedOrPinned`): every file under `.github` is one
of two.
- Generated by `schema-test ci`: ci-fast.yml from the registry's pr group,
  roadmap.yml and schema-nightly.yml from their templates
  (`tools/schema-test/ci-templates/`) and the registry, and docs/CI-TIERS.md.
  `ci --check` regenerates each and fails on any difference, by file and
  line; a change goes through the registry or the template.
- Pinned in `test/registry/ci-pins.tsv` at its content's sha256: the
  workflows that run no test, the setup actions and the data files beside
  them (`pinned`), and the ten test workflows a later layer generates
  (`not-yet-generated`: certify, ci-full, leg-gates-nightly, leg,
  lobster-fixed-proof-qualification, nightly-conformance, python-leg-nightly,
  python-leg, ruby-leg, tla-nightly). A changed pinned file is a reviewed
  change: `ci --check` names the file, the pin's line and the line to write.
  The not-yet-generated list only shrinks: `extract --since MAIN --pins-base
  BASE` holds it to the list of BASE, the tree of the commit the change
  merges into (main for a change to main, the integration branch's previous
  tip otherwise; ci-fast.yml's main-tree action exports it from the event),
  and the two flags only go together
  (`TestTheNotYetGeneratedListOnlyShrinks`). A file leaves the list by being
  generated or deleted, never by becoming pinned. A base from before
  ci-pins.tsv has every workflow it holds not yet generated, but the
  generated ones and the ones that run no test, so there is always a list
  to hold to.
A file that is neither, a pin of a file that is gone and a pin of a
generated file are each red by name; so are a pinned workflow that is none
of the workflows that run no test (nonTestWorkflows), and a not-yet-generated file that is no
workflow.

**The rule the generated workflows keep**
(`TestEveryWorkflowStepCallsOnlySchemaTest`, file:line on every red): every
`run:` line is a comment, a shell setting, a plain `echo` or `exit`, or calls
`go run ./tools/schema-test` (run, ci, coverage or extract) or the full run that
calls it (`go run ./tools/fullrun launch|merge`), with nothing chained after
it (`;`, `&&`, `||`, a pipe, a redirect, `$(` or a backtick is red), no GitHub
expression but a matrix value in the line, and no flag that re-pins, raises
a limit or points elsewhere (`--update`, `--timeout`, `--registry`, `--root`
other than `.`, the write flags); the step shell is bash or pgroup; every
action is checkout, setup-go, the cache and artifact actions or a setup
action; only schema-nightly.yml has a schedule or a workflow_run trigger,
and it runs the nightly group. Generation fixes what the files say; this
holds the templates to the rule. A check that is not a registry row cannot
run in CI.

## The retired workflows

`test/registry/retired.tsv` names every check a deleted workflow ran and the
row that runs it now (workflow, job, kind, check, row, language).
`TestEveryRetiredCheckIsStillRun` holds every line: a row removed, a
language's gate dropped from a per-language row, a target renamed, a
retired `go test` step left with no row, or a check that ran on every pull
request (ci-fast.yml, caa-self-test.yml, roadmap.yml's roadmap job) whose row
is outside the pr group and run by name by no per-PR workflow, is red by the
line that names it, so a coverage drop, including a move to the night,
cannot pass unseen.

A check no row runs yet has a line with row `-`, and the test is red on it
by name until a row runs it; nothing excuses it. The old ci-fast.yml's own
jobs (counted per job below) are named this way, because the generated
ci-fast.yml runs only rows; each layer that gives a check its row shrinks
the red list. The strict lint gates are rows of tier nightly: their whole
run is past a pr shard's budget, so they wait on a faster gate to come back
per PR.

A language that never ran a check before is not left out: the per-language
row names a gate for every language, and a language with no gate fails its
instance by name (`langmake`), red in the nightly until a gate answers it.

The counts below compare, at one commit on one bench (amd-2) and with no CI,
the deleted workflows' own commands with the registry rows that replace
them. "Checks it ran" counts the old commands' checks; "Registry instances
now" counts the rows that run them (one row can run several old commands);
a red in both columns is the same red before and after.

| Workflow | Checks it ran | Passed (its commands) | Registry instances now | Passed (schema-test) | Notes |
|---|---|---|---|---|---|
| caa-self-test.yml | 1 | 1 | 1 | 0 | no token on the bench: none.functional.tooling.caa.self-test FAIL naming GITHUB_TOKEN (the shards supply it) |
| roadmap.yml | 4 | 3 | 4 | 3 | old red: roadmap-test; new red: none.functional.tooling.roadmap.suites FAIL |
| schema-c-runs-hostile-corpus.yml | 2 | 2 | 2 | 2 |  |
| schema-cpp-runs-hostile-corpus.yml | 2 | 2 | 2 | 2 |  |
| schema-cs-runs-hostile-corpus.yml | 2 | 2 | 2 | 2 |  |
| schema-dart-generated-strict-analyze-gate.yml | 3 | 3 | 2 | 2 |  |
| schema-dart-nightly-fuzz.yml | 4 | 4 | 4 | 4 |  |
| schema-dart-runs-hostile-corpus.yml | 2 | 2 | 2 | 2 |  |
| schema-elixir-runs-hostile-corpus.yml | 2 | 2 | 2 | 2 |  |
| schema-engine-is-the-oracle.yml | 1 | 1 | 1 | 1 |  |
| schema-go-generated-golangci-lint-gate.yml | 2 | 1 | 1 | 0 | old red: schema-go-generated-golangci-lint-gate-nightly; new red: go.nightly.lint.make-schema-go-generated-golangci-lint-gate.schema-go-generated-golangci-lint-gate FAIL |
| schema-java-runs-hostile-corpus.yml | 2 | 2 | 2 | 2 |  |
| schema-js-runs-hostile-corpus.yml | 2 | 2 | 2 | 2 |  |
| schema-lobster-generated-strict-gate.yml | 3 | 3 | 3 | 3 |  |
| schema-lua-generated-luacheck-gate.yml | 4 | 4 | 3 | 3 |  |
| schema-lua-nightly-fuzz.yml | 6 | 6 | 5 | 5 |  |
| schema-lua-resource-bounds.yml | 4 | 4 | 3 | 3 |  |
| schema-lua-runs-hostile-corpus.yml | 5 | 5 | 3 | 3 |  |
| schema-lua-version-differential.yml | 4 | 4 | 4 | 4 |  |
| schema-luau-generated-analyze-strict-gate.yml | 2 | 2 | 2 | 2 |  |
| schema-luau-nightly-fuzz.yml | 4 | 4 | 3 | 3 |  |
| schema-luau-resource-bounds.yml | 3 | 3 | 3 | 3 |  |
| schema-luau-runs-hostile-corpus.yml | 4 | 4 | 3 | 3 |  |
| schema-moo-generated-analyze-strict-gate.yml | 3 | 3 | 2 | 2 |  |
| schema-moo-nightly-fuzz.yml | 4 | 4 | 4 | 4 |  |
| schema-moo-runs-hostile-corpus.yml | 2 | 2 | 2 | 2 |  |
| schema-python-resource-bounds.yml | 4 | 4 | 3 | 3 |  |
| schema-python-runs-hostile-corpus.yml | 5 | 5 | 3 | 3 |  |
| schema-ruby-resource-bounds.yml | 2 | 2 | 2 | 2 |  |
| schema-ruby-runs-hostile-corpus.yml | 2 | 2 | 2 | 2 |  |
| schema-rust-runs-hostile-corpus.yml | 2 | 1 | 2 | 1 | the same red: schema-rust-runs-hostile-corpus-negative-control, run as rust.nightly.plant.make-schema-rust-runs-hostile-corpus.schema-rust-runs-hostile-corpus-negative-control |
| schema-shared-hostile-corpus.yml | 2 | 2 | 2 | 2 |  |
| schema-squirrel-all-port-differential.yml | 4 | 4 | 3 | 3 |  |
| schema-squirrel-nightly-fuzz.yml | 5 | 5 | 5 | 5 |  |
| schema-squirrel-resource-bounds.yml | 4 | 4 | 3 | 3 |  |
| schema-squirrel-runs-hostile-corpus.yml | 4 | 3 | 3 | 2 | old red: schema-squirrel-runs-hostile-corpus; new red: the squirrel hostile rows (make schema-squirrel-runs-hostile-corpus) FAIL |
| schema-unity-runs-hostile-corpus.yml | 2 | 2 | 2 | 2 |  |

Staging retired most of these workflows before L7 (the hostile family and L6b), with rows of its own: one row per make target, and the hostile corpus one row per case. Where L7 had a family row for the same target, the merge kept staging's row and dropped L7's, so each target is run by one row; retired.tsv names the kept row. The make targets are the ones measured above, so the counts stand for the commands; the instance count of a hostile workflow is now its cases.

What does not match yet, and why:

- The hostile-corpus rows of c, cpp, cs, dart, elixir, java, js, rust and
  unity were red only from `testreg.RefuseSkips` reading the word in lines
  that report no skip; with that fixed on staging (303142ba49) they match.
  Re-run at one sha with the 90 s adapter limit: cs, js and unity on
  hetzner1, and the rest under `nova-reserve run --exclusive` on amd-2 after
  one warm run, each row at most 30 s there. rust's planted control is red
  in both columns, the same red.
- caa self-test: the row (`tooling.caa.self-test`, tier functional, so per
  PR as caa-self-test.yml ran it) runs `make caa-self-test`, which reads
  main's tip from GitHub and fails naming `GITHUB_TOKEN` when it is not set.
  The bench run above had no token, so it failed by that name; ci-fast.yml's
  pr shards supply `github.token`, as the old workflow did, and with a token
  the row passes. TestCAASelfTestFailsNamingTheMissingToken holds the
  recipe to failing by that name before it reads GitHub.
- schema-squirrel-nightly-fuzz.yml's harness job also ran
  `TestSquirrelNightlyFuzzTestsStayInTheCampaign`, which held each fuzz test
  behind its `nightlyCampaign` gate. The no-skip layer (30556c3a36) removed
  the gate and that test with it, so it has no line: the check it made no
  longer exists to run.
- schema-python-nightly-fuzz.yml and schema-ruby-nightly-fuzz.yml are not
  in the table: the wire-fuzz layer retired them before this one
  (7002f68d86), with its own counts match, and their streams run as the
  per-language rows `fuzz.wire-fuzz.plain`, `.mixed` and `.retain`.
- The old ci-fast.yml's own jobs: the generated ci-fast.yml runs only rows,
  so every check a removed job ran is a line of `retired.tsv`, mapped to the
  row that runs it or named red until a pr-group row runs it (row `-`, or a
  row the pr group does not run: 22 checks map to nightly rows, so they are
  red by the per-PR rule above, which reads a row's tier and language).
  The matrices are
  expanded from their sources (the plan's leg lists, the nightly-fast rows
  file, the input families' full selection, the unmoved ledger's Go tests):

  | Job | Checks | Run per PR by a row | Named red |
  |---|---|---|---|
  | build-cpp-conformance-account | 1 | 0 | 1 |
  | ci-policy | 2 | 1 | 1 |
  | conformance-rows | 8 | 0 | 8 |
  | conformance-rows-elixir | 1 | 0 | 1 |
  | generated-current | 1 | 0 | 1 |
  | golangci-lint-fast | 1 | 0 | 1 |
  | go-test-touched | 64 | 0 | 64 |
  | input-family | 100 | 0 | 100 |
  | input-family-cpp-conformance | 1 | 0 | 1 |
  | input-family-dart-conformance | 1 | 0 | 1 |
  | input-family-elixir-conformance | 1 | 0 | 1 |
  | input-family-merge | 1 | 0 | 1 |
  | lang-clean | 12 | 0 | 12 |
  | lang-lint | 7 | 7 | 0 |
  | lint-fast | 5 | 0 | 5 |
  | newleg-scaffold | 1 | 1 | 0 |
  | nightly-fast | 52 | 17 | 35 |
  | verify-cpp-conformance | 2 | 0 | 2 |
  | verify-dart-conformance | 1 | 0 | 1 |
  | verify-dart-fuzz | 1 | 0 | 1 |
  | verify-elixir-conformance | 1 | 0 | 1 |
  | verify-goose-fuzz | 2 | 0 | 2 |
  | verify-leg | 67 | 0 | 67 |
  | vet | 1 | 0 | 1 |
  | all | 334 | 26 | 308 |

  The jobs that ran no check of their own: plan (it chose the legs),
  stage-compiler (it built the compiler the cpp legs read) and
  go-fixed-corpus (now the `fixed-corpus` job). The kept jobs are
  `schema-test`, the `schema-test-pr-N` shard jobs, `produce` and `verdict`.
- schema-test-nightly.yml ran one check, `schema-test run --group nightly`;
  schema-nightly.yml runs the same group through `go run ./tools/fullrun`,
  so it has no retired line: the check moved, whole, to the workflow that
  replaces it.

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

  There is no exemption list. A test that is red in its tier (it needs a
  toolchain or a built corpus its runner does not have yet) is an ordinary
  row, and it runs red until it is fixed. A test heavier
  than a shard is split into rows that fit. `TestNameBuildability` stays on
  the ledger's `tools/namebuild` line until L4 splits it into one test per
  target and gives each its row.
- **L4:** one adapter per language over the conformance drivers' protocol,
  corpus families first.
- **L5:** the per-language Go tests, through each language's adapter.
- **L6:** plants, make gates, goldens, lint, fuzz, benches and TLC models.
- **L7:** CI generated from the registry (the CI section above). The
  per-language scheduled workflows are retired into rows; ci-full.yml,
  certify.yml, the leg workflows and the other nightlies remain until every
  check they run is a row (`TestEveryWorkflowStepCallsOnlySchemaTest` names
  each remaining step, by file and line).

An old CI job is retired only after a run at one SHA shows the driver ran
every test that job ran, in the same tier, with the same count.
