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
- **§6.1: Derives PROPOSED** — `Equal` / `Checksum` / `Print`, each funded by a real
  networking need (delta detection, desync detection, desync diagnosis).
- **§4.8: generated layout is the generator's** — layout rules kept alive by prose are
  exactly what generated view structs absorb.
- **§9 movement:** q13 (interpolation policy vocabulary) and q14 (the replication-policy
  boundary) opened with external evidence attached; q5 (doc comments) and q9 (explicit
  enum values, flag enums) gained external need votes; q6 (root/reachability marker)
  externally corroborated; q15/q16 hold the interop and adoption directions, gathered and
  deliberately not designed.

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
