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
| an adapter times out (the deadline is an argument; the test hands in one already past) | `TestAnAdapterTimeoutFails` |
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
| `gotest` | language-free Go tests; inputs `{"package", "test"}` | `go test -count=1 -json -run '^(A|B|...)$' <package>` per package, cut into calls of at most `testreg.ShardBudget` measured seconds, run one after another within a package and several packages at once, each given the driver's remaining deadline less a 10 s margin as `-timeout`; a row a call never reports fails by name, and a call that exits non-zero with every test passed (a `TestMain` check) fails its rows, every event mapped to its row by full name (a test's tier says what it needs; nothing in a test decides whether it runs). A test or subtest that skips fails; a row whose test is not in its package's source is MISSING; one that never reports fails. A package in a module of its own runs from that module |
| `plant` | negative controls; inputs `{"target", "env", "reason"}` (`reason` optional) | runs the target by name with `env` set, which breaks what the target checks; passes only when the target runs and fails, and, when `reason` is given, its fail reason holds it |
| `model-check` | TLA+ model checks (type `model`); inputs `{"config", "workers"}`, or `{"controls": true}` | `go build ./tools/tlcrun`, then `tlcrun -adapter`: TLC over `tla/<config>`, judged against its `tla/RUNS.tsv` row (an exhaustive run explores the recorded distinct states with no error; a reversed witness violates exactly the invariant its row names). The controls row runs the judge's three broken-tree controls. No TLC jar or no java fails every instance naming the runner. `SCHEMA_TEST_TLC_PLANT_APPEND` is the plant hook: its text is appended to the config in a scratch copy of `tla/` |

The `corpus` adapter's harnesses, by family:

| family | harness | build | inputs |
|---|---|---|---|
| `align-arrays` | `verify-bitphase --adapter --lang L` | `make bin/schema build/verify-bitphase-bin` | the row's bytes (alignarrays.go) |
| `conformance-<surface>` | `build/conformance-harness-vm adapter --lang L` | `make conformance-leg-L` | `surface`, `case` (the file the driver writes), `files` (the corpus files the case reads), `driver` |
| any family whose rows' inputs carry `tests` (a union row) | `go-union`: L's own Go test, through the `gotest` adapter's calls | none (the runner provides L's toolchain, as the test's tier says) | `tests`: `{"<lang>": {"package", "test", "tags"}}` (`tags` optional) |

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

- Per PR: ci-fast.yml job `plan` reads the matrix (`schema-test langs
  --ready`); job `schema-test` runs `--group pr --lang L`, one row per ready
  language, each building only its own leg, and its `none` row prints
  `coverage` and holds the extract to the ledger. Job `schema-test-pr` is a
  matrix of shards, each `schema-test run --group pr --lang none --shard i/N`:
  the language-free tests of tiers unit and functional.
  `TestThePRShardsFitTheirJobs` holds the matrix to exactly 1/N..N/N, each
  shard within its 60 s of measured tests. The two jobs together are the `pr`
  group exactly: every ready language less what it owes, every
  language-free test. A workflow job that runs the driver is not itself an
  unmoved test: its tests are the rows it selects.
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
- **L7:** CI generated from the registry.

An old CI job is retired only after a run at one SHA shows the driver ran
every test that job ran, in the same tier, with the same count.
