# The shared hostile-input corpus

The hostile-input corpus (schema-shared-hostile-corpus) is one committed set of
adversarial inputs for schema, shared by every port. Each input's expected
verdict — accept with a decoded value digest, or refuse (the engine's reason
is recorded beside it as a diagnostic) —
is taken from the **Go engine** (`internal/tablewire`, `internal/tabletext`),
never from a generated reader.

## Where it lives

```
internal/ci/hostilecorpus/corpus/manifest.tsv
internal/ci/hostilecorpus/corpus/inputs/<file>
    the committed corpus: one raw input file per case plus one line-oriented
    manifest. This is the portable form every port reads.
internal/ci/hostilecorpus/corpus.go
    the engine verdict, the generator that rebuilds the manifest, the drift
    check, and the recursive source inventory.
internal/ci/schema_shared_hostile_corpus_test.go
    the drift, reason-coverage, source, generator and negative-control tests.
tools/hostilecorpus/main.go
    the Go engine replay and the worked example every per-port card follows.
test/go-hostile/
    a separate native runner calling the generated Go public readers.
make/checks/schema-shared-hostile-corpus.mk
    the gate and generator targets (included explicitly with the other
    make/checks gates).
.github/workflows/schema-shared-hostile-corpus.yml
    the nightly (schedule) and workflow_dispatch run of the drift gate and of
    its negative controls; no pull request starts this workflow. The same
    tests are Go tests of internal/ci, and ci-fast.yml's ci-policy job runs
    `go test ./internal/ci/` on every pull request and every push to main.
```

`internal/ci/hostilecorpus/wide/` is a second corpus directory in the same format: its own `manifest.tsv`, `inputs/` and schema, built and checked by the same `Generate` and `Verify`.
Each leg's own card reads it in its own test; a later card folds it into the main manifest once every leg holds it.

The corpus directory is the contract: a port needs only a line reader, access
to the raw `inputs/` files, and its own decoder. It does not need Go or JSON.

## The format

The manifest is tab-separated, one case per line, with `#` comments and blank
lines ignored.

| field | meaning |
| ----- | ------- |
| `name` | the case's unique name |
| `carrier` | how the input is handed to the engine (below) |
| `schema` | the schema file or directory, relative to the repository root |
| `table` | the root table the input targets |
| `input` | the payload's path, `inputs/<file>` |
| `provenance` | where the input was gathered from |
| `verdict` | `accept` or `refuse` |
| `reason` | `-` for an accept, else the engine's refusal reason (diagnostic only) |
| `digest` | SHA-256 of the engine's canonical text for an accept, else `-` |

A port runs a case by decoding the payload as `carrier` describes and comparing
its verdict and (for an accept) decoded value with the manifest. A refusal
conforms on its verdict alone: the `reason` column is the Go engine's
diagnostic, kept for debugging, and the reason a port names is its own detail,
printed but never compared (`docs/rulings.json` `refusal-verdict-only`,
SPEC-TABLES §6.5 and §13.10). A runner still fails a case its port does not
answer: a crash, a hang, an error or a missing answer is never a refusal. The
canonical decoded value is the engine's text form (`tabletext.Model.Write`),
which is the same JSON shape every generated backend writes.
`tools/hostilecorpusexec/testdata/scorer-outcomes.tsv` holds the scorer's own
outcome cases (an equal and an alternate refusal pass, a changed verdict either
way, a different accepted value or a worker exit fails), run by
`TestScorerReadsVerdictsAndAcceptedValues` for the C and C++ scorer and graded
by every other port's runner through the comparison its corpus run uses: the
runner's `scorer` mode (Dart, Elixir, generated Go, Goose, Java, JavaScript,
Rust, C# and C# (unity)), run by its hostile target before the corpus, or a Go
test beside the runner (`TestSchemaLuaHostileScorerOutcomes`,
`TestLuauHostileScorerOutcomes`, `TestSchemaRubyHostileScorerOutcomes`,
`TestSquirrelHostileScorerOutcomes`, `TestPythonHostileScorerOutcomes`, and
Lobster's `TestScorerOutcomes`), which needs no toolchain and runs in the fast
lane. A runner's own failure label (a writer that refuses the value the read
accepted, a raise, a carrier it cannot bind) is an error, never a refusal.

### Carriers

| carrier | engine call |
| ------- | ----------- |
| `wire` | `tablewire.Decode(model, instance, input, report)` |
| `text` | `tabletext.Model.Read(instance, input, report)` |
| `message` | announce the unit's vocabulary, then `tablewire.DecodeMessages` into one instance |
| `message_novocab` | `tablewire.DecodeMessages` with no vocabulary |
| `announce` | `Vocabulary.AnnounceRead(input, report)` |
| `announce_second` | read the announcement twice on one vocabulary |
| `announce_bytes` | announce with `MaxBytes = 8` |
| `announce_entries` | announce with `MaxEntries = 1` |
| `packet` | none: the port's packet reader, pinned by hand (below) |

A `packet` row is the one kind whose verdict the generator cannot rebuild: no
Go engine reads the packet wire (the C++ reference is its oracle). Its verdict
is pinned by hand, and the only one a packet row may carry is `refuse
packet_refused -`. `Generate` keeps the row after checking what it can (the
input is there and not empty, the schema loads, the named type is declared in
it), and refuses one that carries any other verdict; every port's runner holds
its packet reader to the row.

The `schemas/probe.schema` case is the one schema not in the checkout: a
variable root whose collection count can pass the int32 storage cap, so the
engine names `count_refusal`.

## Running it

```
make schema-shared-hostile-corpus                 # the drift gate
make schema-shared-hostile-corpus-negative-control # planted controls (nightly)
make schema-shared-hostile-corpus-generate        # regenerate after an engine change
go run ./tools/hostilecorpus run                  # the Go engine replay: every verdict
```

`tools/hostilecorpus run` is the worked example: it loads the manifest, decodes
every raw input, asks the Go engine for the verdict, prints one `PASS` line per
case and fails if any verdict differs from the committed manifest. A per-port
card (`schema-<port>-runs-hostile-corpus`) follows the same shape with that
port's decoder and the same manifest.

The generated Go gate is `make schema-go-runs-hostile-corpus`, included in
`test-go` and therefore in the full nightly build. It generates each distinct
unit once and reads each manifest row once through generated strict text,
file, message and announcement APIs. Variable wire reads use the generated
region measure and load pair; text reads use generated builders. It compares
verdicts (a refusal on its verdict alone) and canonical JSON digests without
calling the reference engine, and its negative control changes only a scratch
manifest verdict.

The ports with runners include Java: `make schema-java-runs-hostile-corpus`
(`test/java-hostile/Runner.java`, the units the manifest names compiled with
`javac --release 17 -Xlint:all -Werror`). Elixir: `make schema-elixir-runs-hostile-corpus`
(`test/elixir-hostile/runner.exs`). Dart: `make schema-dart-runs-hostile-corpus`
(`test/dart-hostile/runner.dart`, the generated `<Package>Wire.dart` and
`<Package>Fixed.dart` readers, every carrier). Rust: `make schema-rust-runs-hostile-corpus`
(`test/rust-hostile`, one generated crate per schema the manifest names, run
in the release and the debug profile, each case in a child process on a
512 KiB stack with a 5 s budget). C#: `make schema-cs-runs-hostile-corpus`
(`test/cs-hostile/Runner.cs`; `test/cs-hostile/gen` writes the dispatch from the
manifest, and each case runs in a pooled worker process on a 512 KiB stack
thread, so a stack overflow or a hang fails only its own case; a variable
root's `wire` case must agree on the managed and the native region load).
JavaScript: `make schema-js-runs-hostile-corpus` (`test/js-hostile/runner.mjs`,
every carrier, each row on its own worker thread under the per-case time budget;
a packet case runs both read tiers, runtime and flat, under NODE_ENV development
and production, one row each). C and C++:
`make schema-c-runs-hostile-corpus` and `make schema-cpp-runs-hostile-corpus`
(the units in `test/c-hostile` and `test/cpp-hostile`, linked into one case
binary per port under AddressSanitizer and UndefinedBehaviorSanitizer and
driven by `tools/hostilecorpusexec`, which runs each case in its own process
with a 512 KiB case-thread stack and the per-case budget, so a crash, a
sanitizer report or a hang fails only that case; a retaining load of a
fixed-storage root, which neither port has, answers `SKIP`, and a skip passes only
when the port's `known-skips.tsv` names its case; none is named today). Each carries the disagreements
its port still owes in a `known-findings.tsv` beside the runner and prints them
as FIX-CARDs. A disagreement no FIX-CARD names fails the run, and so does a
FIX-CARD whose case has gone green.

## Reasons

`damaged` is every other refusal of the one read (damage, a kind mismatch, a
value past its bound, a text that is not this dialect's JSON): the verdict is
compared and the reason is per-language detail (SPEC-TABLES §6.5). Every other
reason is the Go engine's own name, read off its constants:

`newer_form`, `no_vocabulary`, `second_announcement`, `vocabulary_too_large`,
`message_form_as_file`, `batch_too_large`, and `count_refusal` (the builder-path
storage refusal). `TestSchemaSharedHostileCorpusReasons` proves every one has
an input and that no other reason is named.

## The drift check

`TestSchemaSharedHostileCorpus` (and `make schema-shared-hostile-corpus`)
regenerates every verdict from the engine and fails on any difference from the
committed manifest. After a deliberate engine change, regenerate with
`make schema-shared-hostile-corpus-generate`.

`TestSchemaSharedHostileCorpusNegativeControl` plants a changed verdict in the
committed manifest and requires the same drift gate to go red; without the
plant it requires green, and an accept planted on a packet row must be refused
as well. `TestSchemaSharedHostileCorpusSourcesNegativeControl`
plants an uncited conformance vector and requires the source walk to see it.
Both controls run in `make schema-shared-hostile-corpus-negative-control`, which
the nightly workflow invokes. On a pull request they run as Go tests of
internal/ci: ci-fast.yml's ci-policy job runs `go test ./internal/ci/` on every
pull request and every push to main, and with it the drift, source and
excluded-source tests.

`TestSchemaSharedHostileCorpusSources` holds the source claim two ways: every
cited source must still exist in the checkout as a regular file, and every leaf
file in the seed/vector trees below must be cited by the manifest, so a new fuzz
crash or conformance vector cannot silently fall out of the corpus. The source
trees are walked recursively and provenance cites the actual vector file, and its expected-value golden where it has one (for a
conformance vector, the case directory's `*.json`, not the directory itself), so
the 131 `json-hostile` case subdirectories are inventoried too.

## Sources

The corpus is gathered from:

- **go fuzz corpora** under `internal/tabletext/testdata/fuzz/FuzzReadText`
  (the committed crash/minimisation seeds),
- **conformance hostile vectors**: every `json-hostile` tree named by
  `testdata/conformance/tables/MANIFEST.txt` (the `test/tables/*_hostile*.cpp`
  fixed-form vectors target the C++ fixed-form reader, a different wire and
  engine, and stay with the fixed-form gate; they are not table-wire inputs),
- **hand-built edge cases** from the engine's own bounds and refusal reasons:
  the empty wire, a truncated tail, the form byte above the one the reader
  carries, the message form offered as a file, the two vocabulary bounds, a
  second announcement, a body before an announcement, a batch above the
  caller's storage, and a collection count past the int32 storage cap.

### Excluded compiler fuzz seeds

The 10 tracked files under `internal/fuzz/testdata/fuzz/` are **not** part of
this corpus. They are compiler seeds — `FuzzFormatIdempotent`,
`FuzzGeneratedCompiles` and `FuzzPipelineTwoFiles` — whose interesting result is
a schema-compiler build/format/pipeline outcome, not a tablewire/tabletext
decode. There is no engine verdict of the shape this manifest records for them.
They are named as a closed set in `hostilecorpus.ExcludedSourceDirs`, and
`TestSchemaSharedHostileCorpusExcludedSources` requires each to exist and to be
uncited. Moving one into the corpus therefore fails the gate until the
exclusion list and this section change together, so the omission is auditable
rather than silent. Adding them would need a separate compiler-carrier card with
its own engine-backed verdict; that is not claimed here.
