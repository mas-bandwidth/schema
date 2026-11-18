# The Python port on the shared hostile corpus and its resource bounds

This page holds the Python leg's P12 notes (docs/PORT-PYTHON.md, THE CARD MAP):
the shared hostile-corpus runner (docs/HOSTILE-CORPUS.md) and the bounded
memory/time gate on hostile text. No wire, text or runtime codec changed here;
the generated readers and the runtime are the ones the other Python cards
landed.

## The corpus runner

`TestPythonRunsHostileCorpus` (internal/codegen/pythontable/hostile_corpus_test.go)
reads the committed manifest and one raw input per case. It emits one generated
package per schema the manifest names — through the compiler driver, so a fixed
root keeps the recorded layouts its `schema.lock` carries — and runs one probe
under both pinned interpreters. The probe answers with the carrier's own API:

| carrier | generated call |
| --- | --- |
| `wire` | `Root.load(payload)` |
| `text` | `Root.from_json(text)` |
| `message` | `announce_read(vocab, ANNOUNCEMENT)`, then `load_messages(root, vocab, payload, capacity=1)` |
| `message_novocab` | `load_messages(root, fresh_vocab, payload, capacity=1)` |
| `announce`, `announce_second`, `announce_bytes`, `announce_entries` | `announce_read(vocab, payload[, max_bytes=8][, max_entries=1])`, the second announcement on the same vocabulary |
| `packet` | `Root.read(payload)` and `Root.write()` |

A refusal is mapped to the corpus's named reason: `text` answers `damaged`;
`wire` keeps `message_form_as_file`, `newer_form` and `non_finite_float`,
maps `count_over_extent_cap` to `count_refusal`, and answers `damaged` for
every other read refusal; a packet answers `packet_refused`; the message and
announcement carriers keep the runtime's own name (`no_vocabulary`,
`batch_too_large`, `second_announcement`, `vocabulary_too_large`). An accept's
digest is SHA-256 of `to_json()` without its trailing newline, which is the
engine's canonical text, or of the packet writer's bytes.

`TestPythonHostileCorpusPlantedReader` is the control: it admits form byte
`0xff` at the fixed-form gate in a scratch copy of the emitted runtime and
requires the comparison red by name (`wire-form-ff`: want `refuse/newer_form`,
got `refuse/damaged`). The tree is untouched.

`make schema-python-runs-hostile-corpus` is the gate and
`make schema-python-runs-hostile-corpus-negative-control` its control;
`.github/workflows/schema-python-runs-hostile-corpus.yml` runs the sweep and
the control nightly and on dispatch.

## The resource bounds

`TestPythonResourceBoundsHostileShapes` reads five hostile documents against one
generated table under both pins. Allocation is `tracemalloc`'s peak above the
read's start, and time is the count of executed line events (`sys.settrace`),
never wall clock. A read may allocate at most 256 times its input plus 1 MiB,
and may execute at most 128 line events per input byte plus 2^20.

| shape | input | verdict | allocated | budget | line events |
| --- | --- | --- | --- | --- | --- |
| deep nesting (132 nested lists) | 264 B | `Refused malformed` | 90,886 B | 1,116,160 B | 3,250 |
| huge count (100,000 elements past `[..1000]`) | 200,010 B | `Refused count_over_bound` | 13,020,312 B | 52,251,136 B | 6,400,156 |
| recursion (132 pointer bodies) | 3,050 B | `Refused malformed` | 125,571 B | 1,829,376 B | 29,737 |
| giant string (70,000 bytes) | 70,002 B | `Refused kind_mismatch` | 634,905 B | 18,969,088 B | 560,048 |
| many unknown fields (2,000 keys) | 20,891 B | accepted, counted | 407,416 B | 6,396,672 B | 279,169 |

The numbers are one run on the development bench; the assertions use the
budgets above and the documented refusal names, so a slower or faster machine
does not change the gate. There is no hot codec path in this card, so there is
no shape comparison or bench to report.

`TestPythonResourceBoundsPlantedPattern` raises the scanner's depth cap
(`if depth > 128:`) in a scratch copy of the emitted `json_scan.py` and
requires the deep shape to stop refusing as `malformed`; it changes to
`kind_mismatch`, and the control is red. `make schema-python-resource-bounds`
and `make schema-python-resource-bounds-negative-control` run the two gates;
`.github/workflows/schema-python-resource-bounds.yml` runs the sweep and the
control nightly and on dispatch.
