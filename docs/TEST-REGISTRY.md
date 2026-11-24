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
| `type` | `name`, `what` | one kind of check (roundtrip, load-refusal, golden, lint, model, plant, ...) |
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
- `--shard i/n` runs the i-th of n parts of the expanded list: name k goes to
  part k mod n + 1, so the parts cover every name exactly once. An empty part fails.
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
| the command's exit status on each of the above | `TestTheCommandExitsNonZeroOnEveryFailRule` |

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
| `gotest` | language-free Go tests; inputs `{"package", "test"}` | `go test -json -run '^(...)$' <package>`, one verdict per top-level test; skip is fail |
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

## Adding a test

Add a `test` line. A corpus row of a family a harness already runs needs nothing
else: every ready language runs it on the next pull request, and every other
language shows it MISSING. A language that cannot express it adds an
`unrepresentable` line with the reason. A Go test in the driver's packages needs
its `gotest` row, or `TestEveryDriverTestIsRegistered` is red.

## Adding a language

Add one `lang` line with `"adapter": ""`. From then on every per-language test
has an instance in it, each MISSING: `coverage` shows the column red and the
nightly run fails on every one. That red count is the language's remaining work.
When its adapter lands, set `adapter`: the `pr` group (`--ready`) then runs it on
every pull request.

## CI

- Per PR: ci-fast.yml job `schema-test` prints `coverage`, then runs
  `--group pr`: unit and functional, every ready language, every language-free test.
- Nightly: `schema-test-nightly.yml` runs `--group nightly` (every tier, every
  language, no timeout) and names every MISSING instance red.

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
- Cost: the `pr` group (110 names: 72 per-language, 38 language-free) runs in
  about 7 s warm on a bench.

## Next layers

1. Move today's tests in from the extraction (`raw.jsonl`), family by family.
2. One adapter per language that routes every family to its harness.
3. The adapters for goldens, lint, TLC and traces, benches.
4. The repo-wide skip policy, and the policy that a workflow step only calls the
   driver; then retire the old harness jobs.
