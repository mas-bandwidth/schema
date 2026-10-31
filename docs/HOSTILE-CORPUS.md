# The shared hostile-input corpus

The hostile-input corpus (schema-shared-hostile-corpus) is one committed set of
adversarial inputs for schema, shared by every port. Each input's expected
verdict — accept with a decoded value digest, or refuse with the named reason —
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
    the Go port runner and the worked example every per-port card follows.
make/checks/schema-shared-hostile-corpus.mk
    the gate and generator targets (included explicitly with the other
    make/checks gates).
.github/workflows/schema-shared-hostile-corpus.yml
    push/pull request fast gate; schedule/workflow_dispatch negative control.
```

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
| `reason` | `-` for an accept, else the named refusal reason |
| `digest` | SHA-256 of the engine's canonical text for an accept, else `-` |

A port runs a case by decoding the payload as `carrier` describes and comparing
its verdict, reason and (for an accept) decoded value with the manifest. The
canonical decoded value is the engine's text form (`tabletext.Model.Write`),
which is the same JSON shape every generated backend writes.

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

The `schemas/probe.schema` case is the one schema not in the checkout: a
variable root whose collection count can pass the int32 storage cap, so the
engine names `count_refusal`.

## Running it

```
make schema-shared-hostile-corpus                 # fast gate (push/PR)
make schema-shared-hostile-corpus-negative-control # planted controls (nightly)
make schema-shared-hostile-corpus-generate        # regenerate after an engine change
go run ./tools/hostilecorpus run                  # the Go port runner: every verdict
```

`tools/hostilecorpus run` is the worked example: it loads the manifest, decodes
every raw input, asks the Go engine for the verdict, prints one `PASS` line per
case and fails if any verdict differs from the committed manifest. A per-port
card (`schema-<port>-runs-hostile-corpus`) follows the same shape with that
port's decoder and the same manifest.

Three ports carry one today. Java: `make schema-java-runs-hostile-corpus`
(`test/java-hostile/Runner.java`, the units the manifest names compiled with
`javac --release 17 -Xlint:all -Werror`). Elixir: `make schema-elixir-runs-hostile-corpus`
(`test/elixir-hostile/runner.exs`). Dart: `make schema-dart-runs-hostile-corpus`
(`test/dart-hostile/runner.dart`, the generated `<Package>Wire.dart` and
`<Package>Fixed.dart` readers, every carrier). Each carries the disagreements
its port still owes in a `known-findings.tsv` beside the runner and prints them
as FIX-CARDs. A disagreement no FIX-CARD names fails the run, and so does a
FIX-CARD whose case has gone green.

## Reasons

`damaged` is a read that reports a malformed/kind-mismatch event without naming
a refusal. Every other reason is the Go engine's own name, read off its
constants:

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
plant it requires green. `TestSchemaSharedHostileCorpusSourcesNegativeControl`
plants an uncited conformance vector and requires the source walk to see it.
Both controls run in `make schema-shared-hostile-corpus-negative-control`, which
the nightly workflow invokes; the fast gate also runs the source test and the
excluded-source accounting test, so an uncited new vector or a moved compiler
seed fails on every push and pull request.

`TestSchemaSharedHostileCorpusSources` holds the source claim two ways: every
cited source must still exist in the checkout as a regular file, and every leaf
file in the seed/vector trees below must be cited by the manifest, so a new fuzz
crash or conformance vector cannot silently fall out of the corpus. The source
trees are walked recursively and provenance cites the actual vector file (for a
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
