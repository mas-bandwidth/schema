# Reading the Atlas evaluation — what we take, what we answer, what we leave

*Rowan, 2026-08-05. Response to `2026-08-05-atlas-schema-evaluation-tycho.md` — Patrick's
AI's evaluation of our spec (draft 4) against Atlas's `core/data/schema` + Loom stack.
Analysis only; nothing here is DECIDED and no spec text moves without Glenn's word.*

## The frame, first

The document answers "does the mas-bandwidth spec have use *in Atlas*" — not "is the spec
right for what it is for." Its own accounting concedes the scope: *"Atlas has one language
and one reader; this buys nothing here."* Our product IS the part that buys nothing there —
four independent runtimes agreeing byte-for-byte, a compiler an outside team can hold, a
conformance matrix. Their verdict ("not the spec, and not its language") is the right call
*for Atlas* and says nothing about schema's own goals. Read that way, the document is
useful, sharp, and mostly good news.

## The strongest item for us: convergent validation of the object layer

Their `net/sync` pain list is, item for item, our feature list — written by a codebase that
never saw our design:

| Their hand-written pain (net/sync) | Our declared answer |
|---|---|
| serialize/deserialize/interpolate triples as "per-field metadata pretending to be code" | one declaration; generated read/write/interpolate |
| symmetric-pair drift — "nothing checks it" | read/write generated from one source; golden wire bytes |
| layout-by-comment (`correctionFloats` needs smoothed floats front-loaded, "enforced by prose") | `[interpolate]` markers; derived ordering |
| hand-bumped protocol id, "super unsafe" (our words) / "of little concern" (theirs) | hashed id, §3 |

Their own sentence: *"the object-view design... independently arrives at exactly the
artifacts net/sync's examples hand-write. Two codebases deriving the same shape is decent
evidence the declaration-plus-codegen form is right."* That is independent confirmation of
the layer Glenn designed on 2026-08-04/05, from production experience we have never seen.
Worth quoting in any future motivation/README text, attributed as independent.

## The one technical challenge, checked against the spec: the protocol id

**Their claim:** *"file hashing moves the id on a whitespace edit; `schemaLayoutHash` moves
it on layout/vocabulary change... Atlas's answer is better than the spec's."*

**Checked against SPEC.md §3.1:** the whitespace sensitivity is not a discovery — the spec
states it as an accepted property with its direction argued: a spurious id move costs a
redeploy under the ship-together model and **fails safe** (sides refuse to talk when they
actually could). The two designs bracketing theirs are both recorded as killed, with
reasons: draft 1 (canonical IR, rename-invariant) died because name-stripped hashes let two
builds swap `health`/`armor` and read each other's slots crosswise; draft 3 (generated
code) died because the target set was in the id.

**What their alternative actually is:** a canonical-form hash that KEEPS names — a middle
point between our killed draft 1 and our shipped §3.1. It is a real design point, and the
honest comparison is:

- **Their gain:** comment/whitespace edits do not move the id.
- **Their cost, which the evaluation does not engage:** §3.1's second property — *the id
  does not move when the compiler upgrades* — is **structural** with file bytes and becomes
  a **maintained promise** with any canonicalized hash. Every parser/canonicalization change
  risks moving deployed ids, which is exactly the silent class the golden-wire gate exists
  to catch, now with a second surface. Atlas can afford this: their hash is computed from a
  live in-engine registry, ids are "of little concern at current sizes anyway" (their
  words), and nothing ships to strangers. We are specifying a constant that gates every
  packet of shipped games across compiler versions.
- **Failure directions differ:** ours fails safe (spurious refusal), a canonical hash fails
  *convenient* — and the day canonicalization changes, it can fail *compatible-looking*.

**Recommendation (mine, for Glenn's call):** keep §3.1 as decided. If schemafmt lands
(§9), the cheap refinement is available and can be an open-question row then: hash the
canonical formatted form, which kills whitespace moves while keeping names — accepting that
id stability across compiler versions becomes a promise pinned by golden ids rather than a
structural fact. My own lean: the structural property is worth more than avoiding an
occasional harmless redeploy. Not re-opened tonight; the decision is one day old and their
argument adds no failure case we have not already priced.

## The process jab, and the half we take

*"Four protocol-id designs in two days, retrofitted horizons — the cost of designing the
language before the model."* Two things are true at once:

- The count is roughly fair, and the lesson has a real half: for the DELTA/OBJECT phase,
  settle the semantic model (IR, view derivation, quantize pairing) before its syntax.
  Banked as method for the next phase.
- The reversals they read are our provenance discipline working — every DECIDED block
  carries its authorization and its date, which is the only reason an outside reader could
  reconstruct the sequence at all. A design history you can audit is not a defect of the
  design. We keep the discipline and take the sequencing lesson.

## The two approaches, settled in the main channel (2026-08-05 morning)

Glenn, reading this evaluation: *"We are focused on the networking and delta encoding, and
then maybe moving down to more general purpose stuff. I think this explains the difference
between our two approaches."* And on Patrick: *"Patrick seems to be focused mostly on the
tools/render/assets side, and over time moving up towards networking."*

**Same hub, entered from opposite ends.** We start at the wire — serialize family,
byte-identity, quantized realtime state — and move down toward general-purpose data if/when
it earns its turn. Atlas starts at tools/assets — the inspector, ADN, the asset layer — and
moves up toward networking, last in their own dependency order. Each stack is mature where
the other is early, so each side's evaluation of the other undervalues exactly the half its
author hasn't reached yet. Their doc concedes this for the wire catalog ("relevant only
once codegen exists"); the mirror holds for us on their asset patterns.

**What this sets for our roadmap:**

- **Delta encoding is the next major design phase**, and it is where the banked
  model-before-grammar lesson lands immediately: settle the semantic model (baseline
  selection, prediction arithmetic, the surveyed inexpressibles — delta/baseline,
  sentinel-terminated streams, int_relative) before any syntax. Tycho's net/sync
  description is a direct model input: interpolate functions, smoothed-prefix ordering,
  step-quantized floats are the artifacts the delta layer must generate.
- **The general-purpose side (Config.schema/Assets.schema) is explicitly "maybe, later"** —
  direction stands from 08-04, priority does not. When it comes up, Atlas's shipped asset
  patterns (Config/Content/Both channels, per-field UI hints, emit-only-when-differs
  defaults, layered composition, schema-driven generic editor) are our reference vocabulary
  — the exact reciprocal of them using our encoding table.
- **The trade with Atlas is reciprocal with a time lag**: their wire needs arrive when they
  reach networking; our asset-side needs arrive if we descend to general-purpose data.
  Nothing to coordinate now; worth remembering when the broader collaboration next moves.

## Also worth keeping

- Their read-back of what the catalog is for — *"a worked catalog of wire-encoding
  constructs... reference material for Atlas's per-field wire-attribute vocabulary"* —
  confirms the §encoding table + corpus is an asset with standalone value beyond our own
  compiler. Keep it precise; someone else is already reading it as reference.
- `schemaLayoutHash` / `schemaFieldEquals` as "derive primitives" is a nice name for a
  category we generate implicitly; no action.
- Expect no serialize-family adoption from Atlas: their direction is codegen over their own
  substrate via Loom/shaderc patterns. Good outcome anyway — the wire-attribute vocabulary
  travels, and two independent stacks converging on declaration-plus-codegen strengthens
  both.

## Actions

1. Evaluation banked beside the spec (`notes/2026-08-05-atlas-schema-evaluation-tycho.md`).
2. This analysis committed beside it. No spec edit made.
3. For Glenn: whether anything goes back to Patrick, and in what register — his call on
   the relationship; the technical reply drafts easily from this note if wanted.
