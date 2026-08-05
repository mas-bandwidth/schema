# examples/ — the corpus

**The corpus tests that the language can actually express real protocols, on paper, before a
line of compiler exists** (Glenn, 2026-08-04: *"once we have the spec and some examples of
types from those type examples, we can have a corpus that we can use to test that the language
can actually do those things"*). When the compiler exists, these graduate into `testdata/` and
`conformance/` as its first test suite.

**Provenance:** the shapes are drawn from Space Game's real serialize usage, surveyed
2026-08-04 (message inventory, ranges, branch patterns, quantization idioms). Names and values
are genericized — these files carry the *structure* of a real protocol, not the game's actual
protocol.

**The standing check: every file here must compile under SPEC.md as written.** A corpus
example the spec's own rules reject is a finding — in one document or the other.

## The files

| file | exercises |
|---|---|
| `Input.schema` | counted array of composed structs, raw-bit fields, self-counting `T[<= N]` |
| `Messages.schema` | enum-dispatched message union: `switch`, empty case, byte blocks, per-case fields |
| `ObjectCreate.schema` | quantized-int vector/quaternion structs, constants composing in ranges, bool-gated optional fields, enums |

## What the corpus already found

Writing even three files against the real inventory surfaced language findings — which is the
corpus doing its job:

1. **Enum subranges.** The real create path writes the object kind as `[1, MAX]` — excluding
   the `None` variant from the wire. schema's enum wire is always `[0, max]`, so the corpus
   files spend one unused wire value where the hand-written code does not. A ranged-enum form
   (or `None`-exclusion) is an open-question candidate.
2. **Quantized ints are the real float idiom.** `serialize_compressed_float` exists in the
   library and is used *nowhere* in the game — floats that matter are pre-quantized to
   integers at a fixed scale and sent as ranged ints. The corpus therefore leans on
   quantized-int structs; `compressed_float`'s seat in v1 is worth a second look.
3. **Sentinel-terminated collections.** The real baseline/delta/explosion packets do not use
   counted arrays — they are bool/sentinel-terminated streams (serialize.go even ships
   `Continue`/`Until` helpers for the pattern), sized by mid-stream packet splitting. Not
   expressible in v1; recorded as an open question rather than silently absent.

## Not in this corpus yet, by design

The horizon patterns (SPEC.md, "The horizon"): delta-against-baseline with prediction
expressions, `int_relative` ascending-id streams, mid-stream packet splitting, external-state
gating. Each is real in the surveyed code and each is a design pass of its own.
