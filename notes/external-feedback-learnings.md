# What the external feedback round firmed up — 2026-08-05

*Design-stage feedback on draft 4 from an experienced engine team, received privately as
two documents in one morning. This repo describes our own work, so this note is kept
deliberately source-light: it holds the learnings and what they firmed up; the full
exchange record lives outside this repo.*

## Firmed up — no change, now better-founded

- **The object layer and the declaration-plus-codegen direction.** Their hand-written
  replication layer's cost list matches the object layer's feature list item for item:
  serialize/deserialize/interpolate triples as per-field metadata transcribed into code,
  symmetric read/write pairs that drift with nothing checking them, struct layout rules
  enforced by comment, a hand-bumped protocol id. Independent convergence from a codebase
  with a completely different starting point is the strongest external evidence yet that
  the destination is right.
- **The protocol id (§3.1).** Challenged twice in one morning, held twice. The
  sharpened understanding: with raw file bytes, id stability across compiler upgrades is
  close to structural (a frozen three-line definition, no parser in the id path); any
  canonicalized hash puts the whole front end in the id path and makes that stability a
  maintained promise. The composed-world variant of the challenge is real and is recorded
  at q16 as binding only if cross-unit composition ever exists.
- **The compile-time/runtime split is a product boundary, not a maturity gap.** Their
  world is reflection-driven because editors and generic tooling want live schemas; ours
  is codegen because hot per-tick paths want straight-line generated code. The comparison
  sharpened *why* the non-goals (no runtime interpretation, no self-describing wire) are
  correct for this product rather than merely deferred.
- **No target pays for the others.** Raised from their side as a single-target concern;
  already this design's property, and worth keeping true as targets grow.

## Moved — new spec text, all within the network-focused goals

- **§6.1: canonical encoding stated as a CONTRACT** — equal post-quantization values
  produce identical bytes, deterministically, across compiler versions. Always true by
  construction; stated because a consumer pattern exists (byte-compare dirty detection)
  for which it is correctness, not hygiene.
- **§4.8: generated layout is the generator's** — layout rules kept alive by prose are
  exactly what generated view structs absorb.
- **§9 movement — and the 2026-08-05 pruning:** several rows opened or gained votes from
  the round; the same day, Glenn ruled the externally-derived concepts (replication-policy
  knobs, interop/adoption asks, the derives helpers) **DISCARDED** — *"those concepts are
  from [the external engine]. Discard. we don't use them."* What survives of the round is this file's
  first section: the independent convergent validation of the object layer, and the
  protocol-id challenge §3.1 held against, twice.

## Method learnings

- **Model before grammar.** The protocol-id design churn (four designs in two days, each
  reversal recorded with reasons) was the tuition; the delta pass settles its semantic
  model — per-field tier lists, prediction expressions, external parameters — before any
  syntax.
- **An evaluation's frame determines its verdict.** "Is this useful to us as-is?" and
  "what would adoption need?" produced opposite verdicts from the same reader in the same
  morning. Read external feedback with its frame named first; answer the frame, not just
  the findings.
- **Outbound drafts drift a few degrees past the stated direction** — toward more
  commitment, more imminence, tidier quotes. Both outbound documents in this round were
  gated cold before leaving, and both gates paid for themselves.
