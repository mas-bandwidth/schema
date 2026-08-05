# schema — working conventions for sessions in this repo

- **SPEC.md is the one source of truth.** Decisions carry **DECIDED** with a date and the
  authorizing words; proposals carry **PROPOSED**; open questions live in §9 and get a
  numbered row, never an inline aside.
- **This repo describes our own work.** External collaborators, their people, and their
  codebases are not named in the tree; external feedback is folded in as learnings
  (`notes/external-feedback-learnings.md`) and as evidence attached to spec sections and
  §9 rows. The full exchange records live in Rowan's own repo (private).
- **Design stage.** Nothing is implemented yet; nothing in `notes/` is normative — the
  spec is. The corpus in `examples/` must always compile under the spec as written; that
  invariant has caught a real gap every time it has been exercised.
- **Trajectory** (Glenn, 2026-08-05): once design settles and implementation starts, this
  repo represents the most recent state only, not the total history of everything —
  prune toward that; git history is the archive.
