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

## The files — aspect-oriented layout

One aspect per file (Glenn, 2026-08-05: *"I like aspect oriented programming, eg. all
constants here, all messages there, all objects there and so on"* — **his hedge kept: "This
is not a hard requirement, just a personal preference."** A convention `schemafmt` and the
docs follow, never compiler-enforced; cross-file order-free resolution is what makes it
free):

| file | holds | exercises |
|---|---|---|
| `Constants.schema` | every `const` | constants composing (`const C = A * 2 + B`), order-free references |
| `Enums.schema` | every hand-declared `enum` | enum wire; notes which enums are flatbuffers residue destined to be GENERATED from Config/Assets definitions |
| `Types.schema` | every `type` | quantized-int types, bare `uintN` fields, prefix arrays `[<= N]T`, bool-gated `if`, the explicit `switch` dispatch idiom, storage-typed ranged ints |
| `Messages.schema` | every `message` | the implicit `MessageType` set (SPEC §4.8): empty message, byte blocks |
| `Objects.schema` | every `object` | **the worked Ship** — one definition driving ShipState / Deep / Shallow / Interpolate + Quantize, view markers `[interpolate]`/`[local]` (deep is the default) |

*Coming when their design passes open: `Config.schema` and `Assets.schema` (SPEC, "The
horizon" — the flatbuffers replacement).*

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
   quantized-int types. *(Outcome, 2026-08-05: the `compressed_float` keyword was dissolved
   when attributes arrived — the compressed wire is now `float32 [min, max, resolution]`,
   so the rarely-used construct stopped costing a keyword.)*
3. **Sentinel-terminated collections.** The real baseline/delta/explosion packets do not use
   counted arrays — they are bool/sentinel-terminated streams (serialize.go even ships
   `Continue`/`Until` helpers for the pattern), sized by mid-stream packet splitting. Not
   expressible in v1; recorded as an open question rather than silently absent.

## Not in this corpus yet, by design

The horizon patterns (SPEC.md, "The horizon"): delta-against-baseline with prediction
expressions, `int_relative` ascending-id streams, mid-stream packet splitting, external-state
gating. Each is real in the surveyed code and each is a design pass of its own.
