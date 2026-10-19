# The shared hostile-input corpus

The hostile-input corpus (schema-shared-hostile-corpus) is one committed set of
adversarial inputs for schema, shared by every port. Each input's expected
verdict — accept with the decoded value, or refuse with the named reason — is
taken from the **Go engine** (`internal/tablewire`, `internal/tabletext`), never
from a generated reader.

## Where it lives

The corpus and the drift check live together in
`internal/ci/schema_shared_hostile_corpus_test.go`. The gate runs as part of
`go test ./internal/ci` (so `make test` and the ci-fast go-test job carry it),
and on its own schedule in `.github/workflows/schema-shared-hostile-corpus.yml`.

## The format

A case is a raw carrier payload plus the committed expectation. There is no JSON
dependency: the corpus is a typed Go table in the test file, one case per entry,
and every port that reads it reads the same field names:

| field | meaning |
| ----- | ------- |
| `name` | the case's name (uniquely identifies it) |
| `table` | the table the input targets (e.g. `ShipEntry`) |
| `carrier` | `wire` (raw table-wire bytes) or `text` (raw table-text bytes) |
| `input` | the raw carrier payload |
| `proven` | where the input was gathered from (fuzz crash, conformance vector, hand-built edge) |
| `expected` | the committed verdict: `accept` + `reason` |

The verdict is a pair:

- `accept: true` — the engine accepts the payload and decodes a value.
- `accept: false` — the engine refuses. The `reason` is either the engine's
  own named refusal message, or `damaged` for a read that reports damage
  (malformed / kind-mismatch) without naming a storage refusal.

## The drift check

`TestSchemaSharedHostileCorpus` regenerates every verdict from the engine with
the same code that produced the committed manifest and fails when any committed
verdict differs. `TestSchemaSharedHostileCorpusNegativeControl` plants a bad
verdict and asserts the gate goes red, which proves the gate has a blade.

## Gathering the sources

The corpus is assembled from:

- go fuzz corpora under `internal/fuzz/testdata` (per-table fuzz targets),
- conformance hostile vectors (the fixed-form hostile-byte cases,
  `test/tables/fixedform_hostile_bytes.cpp`),
- hand-built edge cases from the engine's own bounds: one at each bound, one
  below, one above, and a case for each refusal reason the engine names.
