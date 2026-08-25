# examples/ — the corpus

**The corpus tests that the language can actually express real protocols, on paper, before a
line of compiler exists** (Glenn, 2026-08-04: *"once we have the spec and some examples of
types from those type examples, we can have a corpus that we can use to test that the language
can actually do those things"*). When the compiler exists, these graduate into `testdata/` and
`conformance/` as its first test suite.

**Status 2026-08-05 evening: regenerated under the final v1 language** — every design
question landed the same day (SPEC §9; `notes/road-to-v1.md` is the decision record) — for
Glenn's review: *"Before we implement, let's land all design questions, generate final
language example source code in \*.schema in repo and i'll review."*

**Provenance:** the shapes are drawn from Space Game's real serialize usage and object
headers, surveyed and measured 2026-08-04/05 (message inventory, ranges, branch patterns,
quantization idioms, all four object field inventories). Names and tuning values are
genericized — these files carry the *structure* of a real protocol, not the game's actual
protocol.

**The standing check: every file here must compile under SPEC.md as written.** A corpus
example the spec's own rules reject is a finding — in one document or the other. This check
has caught something real every time it has run.

## The files — aspect-oriented layout

One aspect per file (Glenn: *"I like aspect oriented programming"* — his hedge kept: *"not a
hard requirement, just a personal preference"*; order-free cross-file resolution makes it free):

| file | holds | exercises |
|---|---|---|
| `Contexts.schema` | the build contexts | `contexts { client, server }` — user-declared sides, per-context generated types |
| `Constants.schema` | every `const` | composition (`const C = A * B`), `Team.Max` enum references, order-free cross-file refs |
| `Enums.schema` | the enum family | both forms, comma-separated variants: `enum` (with-None) and `flags` (uint64, bit-per-variant) |
| `Types.schema` | every `type` | tagged user types (`Vec3 | vec3`, `Quat | quat4` — tags inert in v1, claimed by the delta pass), quantized-int types, prefix arrays `[..N]T`, bool-gated `if` with a `flags` field, and `RigidBody` — the serialize README's own example, schema-specified: the velocity group rides only when `!at_rest`, and §5's zero-on-untaken-branch rule is the hand-written `else if ( Stream::IsReading )` zeroing made contract |
| `Messages.schema` | every `message` | the implicit `MessageType` set, sorted-by-name tags, empty message, unified `string(N)`/`bytes(N)` (fixed buffers, used-length wire) |
| `Objects.schema` | all four `object`s | the view markers (`interpolate`/`local`, deep by default), explicit-bound composite `quantize`, ranged-int projection with `round = up`, `| local, context = ...` scoped fields, per-field wire treatment divergences between objects — and the FULL working ship state: the wrapper class's sim-local fields (previous position, collider armor table) folded in, so the generated per-context state structs could serve as the simulation struct outright |

*Coming when their design passes open: `Config.schema` and `Assets.schema` (SPEC, "The
horizon" — the flatbuffers replacement), and the delta pass (out of v1 scope by decision).*

## What the corpus found, and what became of it

1. **Enum subranges** (the `[1, MAX]` create-path wire) — designed as `enum_index`
   2026-08-05, then **CUT for now on Glenn's word the same evening**; the design is
   recorded at SPEC §9 q11. Kind fields are plain enums spending one unused wire value —
   the honest v1 cost, back to this finding's original state.
2. **Quantized ints are the real float idiom** — *(outcome, 2026-08-05: the
   `compressed_float` keyword dissolved into `float32 | min, max, resolution`; the
   ranged-int projection in object views carries the same triple plus `round`.)*
3. **Sentinel-terminated collections** — **deferred into the delta pass by decision**
   (three terminator idioms measured in the real packets; inseparable from budget-driven
   packet splitting).

## Not in this corpus yet, by design

The delta pass (out of v1 scope — Glenn: *"the delta serialization is out of scope of v1.
we will hit that once we lay the foundation of types/objects."*): delta-against-baseline
with prediction expressions, `int_relative` ascending-id streams, mid-stream packet
splitting, sentinel-terminated streams. The table layer: `Config.schema`/`Assets.schema`
and the derived type enums. *(Side-conditional fields, once an omission note here, are
now first-class: `Contexts.schema` + `| local, context = ...` — both former omission
sites are declared fields in `Objects.schema`.)*
