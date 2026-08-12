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
  crate and C# sources from `examples/`, and runs ten binaries — the C++ tests (both
  message representations plus a randomized round-trip suite) and the Go, Rust and C#
  wire tests, each byte-comparing against the same eleven C++-pinned wire goldens
  (cross-language wire identity is a standing gate) — plus the fixed-point + 128-bit
  unit (`examples128/`, all four targets since 2026-08-12): its C++ test pins wire
  goldens DERIVED from serialize's STANDARD.md independently, and the Go, Rust and C#
  ludicrous legs (`test/{go,rust,cs}-ludicrous`) byte-compare the same pinned instance
  against them — fixed(I, F)/int128/uint128 wire identity is a standing gate too — then
  the break-the-language diagnostics suite (70+ refusal cases) and the
  source/id/wire golden pins. Each backend
  emits what a careful expert would write against its serialize runtime: split
  `Write`/`Read` per type; per-target dispatch (C++ tagged union or opt-in
  `std::variant`; Go interface + storage; Rust enum; C# abstract class + storage); object
  view families with deterministic `Quantize`/`Unquantize`; zero initialization with
  specified defaults; `schemafmt` canonicalizing every input in place.
- **Trajectory** (Glenn, 2026-08-05): once design settles and implementation starts, this
  repo represents the most recent state only, not the total history of everything —
  prune toward that; git history is the archive.

## The performance program — learnings that bind future optimization work here
*(2026-08-06/07: the four-language profile-and-optimize program; full evidence in
`bench/results/` — the v1→v4 docs and the gap ledger. These are the paid-for rules.)*

- **The doctrine and its order**: unit test → soak test → profile → optimize on a
  profile conviction, never a vibe. Every optimization PR carries: the convicting
  profile/codegen evidence, banked predictions written BEFORE measuring, paired
  before/after (median-of-7, same sitting), and refutations reported plainly. A
  wrong-magnitude prediction is a refutation.
- **The bench golden gate is law**: a runner byte-compares every pinned instance against
  `testdata/wire/` and round-trips it BEFORE producing any number — a runner that
  mismatches REFUSES to bench. This gate caught a real miscompile candidate and, twice,
  harness defects. Never bench ungated.
- **An isolated win must re-prove itself in composition.** serialize's WriteBytes +13%
  chat win vanished in the composed pipeline; restrict's +152% composed cleanly; the C#
  batch 1.285x prototype composed at 1.306x. Landing the PR is not the number — the next
  four-language pass is.
- **A lever proven in one language is only a THEORY in the next.** Generation-time
  bit-count folding: C++ +14.7% (killed an outlined runtime call), C# 0.97–1.04x (the
  JIT had already inlined + lzcnt-folded it — nothing to kill). Rust had restrict's
  benefit all along (`&mut` is noalias). Go's inliner works on a cost budget you can read
  (`-gcflags=-m`). Re-convict per language; never port a win on faith.
- **Emitter levers live in the IR for reuse**: `ir.AlignedFixedByteArrays` (bulk-bytes —
  only where alignment is PROVEN; the wire is law, never a silent re-pin), generation-time
  folded bounds, union tag-only construction (an arm zero-establishes at selection —
  SPEC §5 semantics, guarded by the stale-leak pinned test). C# batch emission: cores are
  INLINE-ONLY (an address-exposed ref-struct measured WORSE than no batch) and OPT-IN by
  scalar density (bulk-dominated types lose; rule in `internal/codegen/csharp/batch.go`).
  Rust dispatch: `read_message` for one-shot, `read_message_into` for reuse loops
  (2.25x); NEVER delegate one to the other (measured −23%, defeats in-place return).
- **Instrument honesty**: harness defects crowned the wrong winner once (v1's "C# beats
  C++ batch read" was a per-iteration alloc in every OTHER runner) — the harness is code
  and rots too. Relative tables move when the DENOMINATOR moves (v4's widening was C++
  accelerating, zero regressions). `message_batch` swings ±20% between byte-identical
  binaries (layout noise) — pair same-sitting, discard contaminated runs whole, file
  unattributed movements instead of claiming them. One bench at a time per machine:
  check for sibling bench processes and wait for a quiet window. **A tiered-JIT runtime
  benched on a single shared core measures tier-up contention, not codegen** (EPYC v5:
  ten of twelve C# write medians sat at tier-0, spreads to 385%, and one row was
  uniformly slow at 11.6% spread — so a low spread does not clear a row; proven by a
  labelled `DOTNET_TieredCompilation=0` intervention, 1.98–4.90x). On a single core,
  settle or disable the tier and LABEL the config divergence; medians-against-min is
  the tell to check first.

## Future work: rANS entropy coding (researched 2026-08-13, NOT implemented)

Glenn asked to look into rANS and record it for whenever we implement. **Nothing here is built.**
The full decision record — including the patent analysis, which is the part that decides whether
we may use it at all — lives in
[serialize/CLAUDE.md](https://github.com/mas-bandwidth/serialize/blob/main/CLAUDE.md).
**Read that before writing any coder.** The short version and the schema-specific angle:

**rANS is mathematically equivalent to a range coder** (same ratio, to a rounding error) **and
much faster on modern hardware** — one multiply and a table lookup per symbol, no division in
the fast path, and critically it **interleaves**, so several independent coder states over one
buffer break the serial renormalisation dependency and let SIMD actually help (~6 clocks/symbol,
~540 MB/s for an 8-way SSE4.1 decoder, per Fabian Giesen).

**WHY THIS IS A SCHEMA QUESTION AND NOT ONLY A SERIALIZE ONE.** An entropy coder is worthless
without a probability model, and **the schema is where a model could come from for free.** We
already know each field's type, range and semantics at compile time. Static per-field frequency
tables — emitted by the compiler into the generated C++/C#/Go/Rust the same way the bitpacking
ranges already are — are the cheapest first experiment, need no adaptive state, and keep the
decoder branch-free. That is a far better fit than a general-purpose adaptive model, and it is a
thing this project can do that a standalone serializer cannot.

**Two constraints to design against before any of that:**

- **rANS is LIFO** — the encoder emits in reverse decode order, so an entropy stage means
  buffering a message or a bounded block rather than a single forward streaming pass.
- **A schema-compiled table is a WIRE FORMAT COMMITMENT.** Change the frequencies and you change
  the bytes. Versioning and cross-language byte-exactness (the property this project already
  guards hardest) both have to survive it, across all four backends.

**And the licensing constraint is real**: Microsoft's `US11234023B2` covers specific rANS
refinements, and their public permission is scoped to open source that **does not charge a
license fee** — which the declared MBSL direction would break. Any entropy stage must therefore
be **optional and versioned**, never welded into the format. Details and the recommended shape
are in the serialize note.
