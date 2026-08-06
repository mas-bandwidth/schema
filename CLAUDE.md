# schema — working conventions for sessions in this repo

- **SPEC.md is the one source of truth.** Decisions carry **DECIDED** with a date and the
  authorizing words; proposals carry **PROPOSED**; open questions live in §9 and get a
  numbered row, never an inline aside.
- **This repo describes our own work.** External collaborators, their people, and their
  codebases are not named in the tree; external feedback is folded in as learnings
  (`notes/external-feedback-learnings.md`) and as evidence attached to spec sections and
  §9 rows. The full exchange records live in Rowan's own repo (private).
- **Implementation started 2026-08-05** (SPEC §7.3 step 3; the Go compiler lives in
  `cmd/` + `internal/`, per SPEC §8). Nothing in `notes/` is normative — the spec is.
  The corpus in `examples/` must always compile under the spec as written; that invariant
  caught a real gap every time it ran by hand, and it is now mechanical: `make check`
  runs the compiler over the corpus, and `make` proves the generated C++ compiles, links
  and runs.
- **What `make` proves, in full** (moved here from README 2026-08-06 — too dense for the
  human front page, load-bearing for a working session): all four v1 backends live; `make`
  builds the compiler, generates C++ headers (`generated/cpp/`), a Go package, a Rust
  crate and C# sources from `examples/`, and runs seven binaries — the C++ tests (both
  message representations plus a randomized round-trip suite) and the Go, Rust and C#
  wire tests, each byte-comparing against the same eleven C++-pinned wire goldens
  (cross-language wire identity is a standing gate) — plus the fixed-point + 128-bit
  unit's C++ test (`examples128/` → `generated/cpp/ludicrous/`, wire goldens DERIVED
  from serialize's STANDARD.md independently; the go/rs/cs backends REFUSE that unit by
  name until their serialize ports carry the phase-1 surface, and the refusal is itself
  a pinned test), the break-the-language diagnostics suite (70+ refusal cases) and the
  source/id/wire golden pins. **Until serialize's fixed-point surface merges into the
  sibling checkout, build with `make SERIALIZE=../serialize`.** Each backend
  emits what a careful expert would write against its serialize runtime: split
  `Write`/`Read` per type; per-target dispatch (C++ tagged union or opt-in
  `std::variant`; Go interface + storage; Rust enum; C# abstract class + storage); object
  view families with deterministic `Quantize`/`Unquantize`; zero initialization with
  specified defaults; `schemafmt` canonicalizing every input in place.
- **Trajectory** (Glenn, 2026-08-05): once design settles and implementation starts, this
  repo represents the most recent state only, not the total history of everything —
  prune toward that; git history is the archive.
