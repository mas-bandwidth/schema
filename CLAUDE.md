# schema — working conventions for sessions in this repo

- **docs/SPEC.md is the one source of truth, written as a clean reference.** It states the
  most recent specification only — present tense, reference register, no history, no
  decision narration (Glenn's directive, 2026-08-18: SPEC must read for a human
  implementer, not like a CLAUDE.md). Decision provenance — who ruled what, when, in
  which words — lives in git history and `notes/road-to-v1.md`; maintainer context
  lives here. Open questions still get a numbered row in SPEC §9, never an inline
  aside; §9 rows keep their numbers forever because code and corpus cite them.
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
- **The public Go API is `compiler/` and `ir/`; everything else stays `internal/`**
  (issue #85, 2026-08-20). The compiler is a library as well as a binary: `compiler` is the
  driver (load, generate, format) plus the `Generator` interface every backend
  registers through, and `ir` is the checked unit with the derived parameters the backends
  share. `cmd/schema` is a client of that API and must stay one — `internal/publicapi`
  rebuilds the CLI inside an external module, so the day it reaches past the public surface
  the build fails and names the package. Adding an export is a semver commitment
  (docs/VERSIONING.md); the rule the issue set is that an export justifies itself on schema's own
  needs or it stays internal. The per-language emitters are implementations, not API.

- **What `make` proves, in full** (moved here from README 2026-08-06 — too dense for the
  human front page, load-bearing for a working session): all nine backends live (the JS
  legs — `test/js`, `test/js-ludicrous` — joined 2026-08-16 with the sixth backend; the
  Dart legs — `test/dart`, `test/dart-ludicrous`, each run twice: JIT --enable-asserts
  and AOT release — joined 2026-08-30 with the seventh, whose generated output is
  self-contained and needs only the pinned Dart SDK, no runtime sibling; the Java legs —
  `test/java`, `test/java-ludicrous`, each run twice: -ea and default — joined
  2026-08-30 with the eighth, likewise self-contained, needing only the pinned JDK; the Elixir legs —
  `test/elixir`, `test/elixir-ludicrous`, on the pinned Erlang/OTP 29 + Elixir 1.20
  with `mix format --check-formatted` as a refuser — joined 2026-08-30 as the ninth,
  self-contained on the pinned BEAM toolchain); `make`
  builds the compiler, generates C++ headers (`generated/cpp/`), C sources, a Go package,
  a Rust crate, C#, JS, Dart, Java and Elixir sources from `examples/`, and runs the test binaries — the C++ tests
  (plus a randomized round-trip suite) and the C, Go, Rust,
  C#, JS, Dart, Java and Elixir wire tests, each byte-comparing against the same C++-pinned wire goldens
  (cross-language wire identity is a standing gate) — plus the fixed-point + 128-bit
  unit (`examples128/`, all targets: the C leg landed 2026-08-13, the other four
  2026-08-12): its C++ test pins wire goldens DERIVED from serialize's STANDARD.md
  independently, and the C, Go, Rust, C#, JS, Dart, Java and Elixir ludicrous legs
  (`test/{c,go,rust,cs,js,dart,java,elixir}-ludicrous`) byte-compare the same pinned instance
  against them — fixed(I, F)/int128/uint128 wire identity is a standing gate too — then
  the break-the-language diagnostics suite (70+ refusal cases) and the
  source/id/wire golden pins. Each backend
  emits what a careful expert would write against its serialize runtime: split
  `Write`/`Read` per type; per-target union representations (C++/C tagged union;
  Go/C#/JS tag beside pre-allocated arms; Rust enum); zero initialization with
  specified defaults; `schemafmt` canonicalizing every input in place.
- **Trajectory** (Glenn, 2026-08-05): once design settles and implementation starts, this
  repo represents the most recent state only, not the total history of everything —
  prune toward that; git history is the archive.

## The horizon — campaign context (relocated from SPEC §1, 2026-08-18)

The long arc (Glenn, 2026-08-04): schema as the single data-definition language, with
the opinionated layers built on top of it living elsewhere. The boundary, in Glenn's
words (2026-08-25): *"schema is types and bitpacking and enums and constants."*

- **The table layer left the language (2026-08-25).** Tables, collections and
  the JSON data compiler are not part of schema: the language is the realtime
  wire — hardcoded structs, one protocol id, same-or-refuse — and content
  pipelines are out of scope. `table` stays a reserved word and the parser
  refuses it by name.
- **The protocol layer left the language (2026-08-26).** Messages, objects,
  the view markers, quantize, round and contexts are not part of schema: the
  free offering is types, enums, flags, unions, constants and bitpacking — a
  pure data contract, zero protocol conventions. `message` and `object` stay
  reserved words and the parser refuses them by name; the projection carries
  frozen `message=false` and `round=nearest` tokens beside `table=false` so
  the refusals moved no protocol-free unit's id. The positioning is
  empowerment: build your own message types with enums and unions.
- **The delta pass** stays out of scope here (SPEC's non-goals): schema
  declares types; delta encoding is an application's own layer.
- **Constants migrate through a temporary duplicate set** (schema + flatbuffers) while
  .fbs consumers remain; `Constants.schema` is the one home, C++ reads generated
  `space::` values through name-preserving aliases, and a static_assert guard block
  makes drift a compile error. Enacted in space 2026-08-11.
- **Generated files are checked into a consumer repo exactly when consumers exist that
  cannot run the generator** (Unity, a Go module on fresh clone) — *"If it were just
  C++ only, I would not."* C++-only consumers generate at build (the space CMake
  `schema_generate` target).
- **Reserved surface for these passes:** `packet`, `delta`, `baseline`, `index`
  (SPEC §1); `lerp`/`slerp`/`snap`/`angle`/`smooth` informally reserved as attribute
  values for a claiming pass; SPEC §9 q11 banks the cut `enum_index` design.
- **The current scope sentence (Glenn, 2026-08-25, verbatim):** *"schema is types
  and bitpacking and enums and constants."* The opinionated protocol layers live
  above the language, built from its primitives.

## SPEC maintenance ledger (from the 2026-08-18 human-readability rewrite)

docs/SPEC.md was rewritten 2026-08-18 into plain reference register on Glenn's directive —
most recent state only, no history, no decision narration. Nothing normative changed.
The material that left the spec lives in three places: **git** (the last pre-rewrite
revision carries every DECIDED date, verbatim ruling and repair note inline),
**notes/road-to-v1.md** (the road-to-v1 decision list), and **this file**. Section
numbers §1–§9 and the §9 q-rows are frozen — code, corpus and docs cite them.

- **Testing status at the rewrite** (was SPEC §7.2's status paragraph): gates 1, 2 and 7
  live across the matrix (`testdata/golden/`, `testdata/wire/` — C++ writes the pins,
  every other leg byte-compares them); gate 4 live in pinned-instance form plus the randomized C++
  round-trip suite; gate 3 in seed form (RigidBody and string classic twins in
  `test/main.cpp`), growing with the corpus; gate 6 live as the break-the-language
  diagnostics suite (70+ cases) plus `internal/fuzz` (compile, oracle and property
  fuzzers); gate 5 and gate 4's full random matrix are the remaining conformance
  work. A fmt-drift gate asserts the corpus stays formatter-canonical.
- **Held-loosely tells** (the spec states these as law; the confidence metadata lives
  here): the bracket attribute syntax carries Glenn's hedge ("just a personal
  preference, could be wrong but let's see in time");
  the §5 reused-output tail rule is priced-later (tail-clearing on read would be a
  generated-code change); whether untaken branches should restore specified defaults
  instead of zeros is Glenn's call if a real case ever wants it; an exact-length
  `bytes` form and a validated-UTF-8 read mode can return if a need appears.
- **Open follow-ups carried out of the spec text:** the C# `Debug.Assert` half of the
  §4.7 UTF-8 writer contract is a recorded follow-up (C#, like Go, asserts nothing
  today). (The stale-§3.1 class — the scanner BOM diagnostic, the six banners'
  "hash of its schema files", check.go's sorted-basename-hashing language — was
  cleaned in the pre-v1.0.0 review pass, 2026-08-25, with its golden re-pin;
  WIRES.md folded into USAGE "The wire" in the same pass, and SPEC §6.1/§6.3
  gained C's naming conventions and the C/JS columns.)
- **Removed as dead grammar residue** (not relocated — it described nothing the
  grammar can express): §4.6's `bytes(<= N)` error row; the `<=` marker on
  string/bytes died in the §4.7 unification. The EBNF gained the `ufixed(I, F)`
  production and string-valued attributes (`cpp_include`) — both were already live
  language, stale in the grammar block only.
- **Deployment doctrine** (Glenn, 2026-08-15): *"I will always deploy client and
  server together on any breakage. so this is no concern."* The 2b rounding
  unification (half away from zero, superseding the pure-shift form) moved shipped
  narrowing bytes only on exact negative ties; the version note lives in
  docs/VERSIONING.md, and the discriminating tie vector is pinned in the conformance
  corpus.
- **Union vs variant, the measured basis for the §4.8 C-flavored-default principle:**
  isolating pure header cost (one representation-agnostic TU, arm64 clang), union
  0.09 s vs variant 0.13 s — the variant surface alone costs +44% header compile
  time; Glenn's ruling on the earlier, cruder number stands as the principle ("0.17
  -> 0.27 is not trivial for me... you almost always pay through the nose for it at
  compile time"). The union arm-zeroing shape (zero at arm selection, not
  whole-union memset at construction — the memset measured 60.6% of batch-read
  self-cycles) is pinned by the stale-leak test.
- **The tables JSON walk, header vs translation unit — the paired before/after**
  (schema#283, owner: *"It would be best if it were written to a corresponding cpp
  file so it doesn't need to be included every time."*). Same instrument as the
  union/variant row above — one empty TU including a single generated table
  header, arm64 clang, arms interleaved in one sitting, n=15 after warmup:

  | arm | min | median |
  |---|---|---|
  | before the walk existed | 0.0478 s | 0.0493 s |
  | walk inline in the header | 0.0538 s | 0.0559 s |
  | walk in `<Base>Table.cpp` | 0.0485 s | 0.0498 s |

  **−9.8% against the inline form, and +1.5% against the pre-walk tree** — the
  walker's share is gone and the header is back where it started; the header
  itself went 3656 → 1986 lines. Three sittings agreed (−10.8%, −9.9%, −9.8%),
  two of which had one contaminated arm (spreads to 83%) and were discarded
  whole rather than averaged. An independent reviewer reproduced it at −10.3% /
  −10.7% across two sittings, with the inline form at +12.9–13.4% over pre-walk,
  which is what §13.5's "+11% to +15%" records as the cost that motivated the
  ruling. The AFTER half lives here because a number is a ledger entry, not a
  spec sentence.
- **Build system**: plain Makefile for now, graduating to CMake as needed (Glenn,
  2026-08-05).

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
- **A generated codec must not depend on the compiler's INLINING BUDGET.** A
  table of a realistic field count emits one large body per type; clang's
  inliner ran out partway through `TableMixedSaveBody` and left the writer
  primitives and the nested bodies out of line, and from there the cursor
  round-tripped through memory at every put — a `uint8_t *` store may alias the
  writer, so `offset` and `capacity` were re-read after each one. Force-inlining
  the primitives ALONE bought 24% and the bodies alone 19%; both together bought
  5.4x, because only a call-free body lets the cursor stay in registers and
  adjacent constant framing bytes merge. The qualifier is portable through a
  per-package macro (`__forceinline` / `always_inline` / `inline`) and stops at
  the class whose call graph cannot cycle. Read the emitted assembly for `bl` to
  a primitive before believing a header-only codec is inlined.
- **An `Open` that only VALIDATES cannot be fuzzed by opening it.** The forgery
  fuzzer's first shape asserted "no crash over random damage" and stayed GREEN
  with the block reader's extent guard deleted: a match-and-point reader reads
  no row, so a removed guard yields a wrong `open` and no symptom. A forgery
  fuzzer must WALK what it opened. Two corollaries fell out of the same chase:
  aim the damage (a block image is 2,816 bytes and the words a check reads are
  the first hundred, so uniform offsets test the row bytes and nothing else; a
  uniform 64-bit value is never a legal-but-escaping count), and delete a
  guard that is actually load-bearing when you build the control — the block
  reader's padding check catches the extent forgery as a side effect, so
  deleting the extent check ALONE proves nothing.
- **C has a namespace the other backends do not: the PREPROCESSOR.** A schema's
  constants, enum variants and flag masks are `#define`s in the C target, and the
  generated sources define macros beside them. A collision there is not a
  redeclaration error — it is a SILENT REWRITE: the generator's `#ifndef` sees
  the user's definition standing, skips its own, and every later use expands to
  something else with nothing in the build saying so. The C target therefore
  reserves `SCHEMA_`/`schema_` and the front end refuses a declaration that
  spells one (`internal/check`'s `cReservedMacros`). The set is ENUMERATED, not a
  bare prefix test: `enum SchemaKind` folds to `SCHEMA_KIND_ALPHA`, which
  collides with nothing, and refusing it would take a name from every schema for
  free. `compiler`'s `TestCGeneratorMacrosAreOwned` holds the two halves
  together by naming every declaration with one prefix and looking at what is
  left — a scan that recognizes no spelling and no family.
- **Instrument honesty**: harness defects crowned the wrong winner once (v1's "C# beats
  C++ batch read" was a per-iteration alloc in every OTHER runner) — the harness is code
  and rots too. Relative tables move when the DENOMINATOR moves (v4's widening was C++
  accelerating, zero regressions). Batch-shaped rows swing ±20% between byte-identical
  binaries (layout noise) — pair same-sitting, discard contaminated runs whole, file
  unattributed movements instead of claiming them. One bench at a time per machine:
  check for sibling bench processes and wait for a quiet window. **A tiered-JIT runtime
  benched on a single shared core measures tier-up contention, not codegen** (EPYC v5:
  ten of twelve C# write medians sat at tier-0, spreads to 385%, and one row was
  uniformly slow at 11.6% spread — so a low spread does not clear a row; proven by a
  labeled `DOTNET_TieredCompilation=0` intervention, 1.98–4.90x). On a single core,
  settle or disable the tier and LABEL the config divergence; medians-against-min is
  the tell to check first.

