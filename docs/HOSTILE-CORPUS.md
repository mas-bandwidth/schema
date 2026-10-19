# The shared hostile-input corpus

The hostile-input corpus (schema-shared-hostile-corpus) is one committed set of
adversarial inputs for schema, shared by every port. Each input's expected
verdict — accept with a decoded value digest, or refuse with the named reason —
is taken from the **Go engine** (`internal/tablewire`, `internal/tabletext`),
never from a generated reader.

## Where it lives

```
internal/ci/schema_shared_hostile_corpus_test.go
    the raw inputs, the one line-oriented manifest, and the generator
make/schema-shared-hostile-corpus.mk
    the gate and the generator targets
.github/workflows/schema-shared-hostile-corpus.yml
    push/pull request fast gate; schedule/workflow_dispatch negative control
```

The corpus is committed in the one test file the card's PATHS admit outside
`make/` and `.github/`. The inputs are base64 (so arbitrary bytes survive a Go
source file) and the manifest is the same tab-separated text a port reads; the
format below is unchanged and a port still needs only a line reader, a base64
decoder and its own decoder. The generator writes the manifest back between the
`HOSTILE-MANIFEST-BEGIN` and `HOSTILE-MANIFEST-END` marker lines.

The gate is `make schema-shared-hostile-corpus` (fast: the drift check and the
reason-coverage test) and
`make schema-shared-hostile-corpus-negative-control` (nightly: plants a bad
verdict in the committed manifest and proves the gate goes red). The workflow
`.github/workflows/schema-shared-hostile-corpus.yml` runs the first on push and
pull request and the second on schedule and workflow_dispatch.

## The format

The manifest is tab-separated, one case per line, with `#` comments and blank
lines ignored. There is no JSON dependency, so every port reads it with a line
reader and feeds the decoded payload to its own decoder.

| field | meaning |
| ----- | ------- |
| `name` | the case's unique name |
| `carrier` | how the input is handed to the engine (below) |
| `schema` | the schema file or directory, relative to the repository root |
| `table` | the root table the input targets |
| `input` | the payload's name, `inputs/<file>` |
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

### Reasons

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
plant it requires green.

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
