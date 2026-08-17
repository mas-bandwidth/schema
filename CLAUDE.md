# schema — working conventions for sessions in this repo

- **SPEC.md is the one source of truth, written as a clean reference.** It states the
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
- **What `make` proves, in full** (moved here from README 2026-08-06 — too dense for the
  human front page, load-bearing for a working session): all six backends live (the JS
  legs — `test/js`, `test/js-ludicrous` — joined 2026-08-16 with the sixth backend); `make`
  builds the compiler, generates C++ headers (`generated/cpp/`), C sources, a Go package,
  a Rust crate and C# sources from `examples/`, and runs twelve binaries — the C++ tests
  (both message representations plus a randomized round-trip suite) and the C, Go, Rust
  and C# wire tests, each byte-comparing against the same C++-pinned wire goldens
  (cross-language wire identity is a standing gate) — plus the fixed-point + 128-bit
  unit (`examples128/`, all five targets: the C leg landed 2026-08-13, the other four
  2026-08-12): its C++ test pins wire goldens DERIVED from serialize's STANDARD.md
  independently, and the C, Go, Rust and C# ludicrous legs
  (`test/{c,go,rust,cs}-ludicrous`) byte-compare the same pinned instance
  against them — fixed(I, F)/int128/uint128 wire identity is a standing gate too — then
  the break-the-language diagnostics suite (70+ refusal cases) and the
  source/id/wire golden pins. The data compiler's own encoder (`internal/pack`) packs
  those same pinned instances from JSON and byte-compares them too, so the Go-side wire
  oracle cannot drift from the generated ones. Each backend
  emits what a careful expert would write against its serialize runtime: split
  `Write`/`Read` per type; per-target dispatch (C++ tagged union or opt-in
  `std::variant`; C tagged union; Go interface + storage; Rust enum; C# abstract class + storage); object
  view families with deterministic `Quantize`/`Unquantize`; zero initialization with
  specified defaults; `schemafmt` canonicalizing every input in place.
- **Trajectory** (Glenn, 2026-08-05): once design settles and implementation starts, this
  repo represents the most recent state only, not the total history of everything —
  prune toward that; git history is the archive.

## The horizon — campaign context (relocated from SPEC §1, 2026-08-18)

The long arc (Glenn, 2026-08-04): schema as the single data-definition language for
Space Game — bitpacked realtime messages (v1, shipped), object and event type
definitions, flatbuffers-style versioned config/asset data, and delta encoding against
a baseline, all generated from one source. His one-sentence version: *"so much
boilerplate would just go away, replaced with a definition of what object types there
are, and what properties and attributes per-property."*

- **Ordering (Glenn, 2026-08-11): messages (landed) → table layer → delta pass.** The
  table layer's wire/reflection half landed as SPEC §4.11 and `schema pack`; the
  space-side `Config.schema`/`Assets.schema` migration is the open half.
- **The delta pass** changes the generated function signature (current object plus a
  baseline), not the wire model or compiler architecture. The surveyed hand-written
  delta code (`core_delta.h`, 2026-08-04) follows one grammar — per-field encoding
  tiers tried cheapest-first, one bit selecting the tier (small-window delta vs
  absolute; or error-vs-prediction, the prediction arithmetic over sibling fields plus
  an external parameter like `deltaFrames`) — so the pass needs three language
  surfaces: per-field tier lists, prediction expressions over sibling fields, and
  declared external parameters. Scope is wider than serialize functions — schema
  eventually owns the object TYPE: deep/shallow struct definitions, capacity
  constants, per-type dispatch cases, manager integration points, and further
  generator kinds (interpolation, render data, struct-to-struct mapping). The IR is a
  typed object model; backends generalize from language targets to generator kinds;
  per-field metadata attaches through SPEC §4.2's attribute mechanism — the
  attachment point already exists in v1's grammar.
- **The flatbuffers replacement (DECIDED in direction, Glenn, 2026-08-05):** config
  and assets move to `Config.schema`/`Assets.schema`; the Go pipeline
  (`cmd/update_schemas`, `cmd/update_config`) and the C++
  `ConfigManager`/`AssetsManager` boilerplate become schema compiler outputs. Not a
  flatbuffers equivalent — *"the minimal representation of the true thing in the
  schema language"*, scoped to the subset space game actually uses. Collections are
  one mechanism differentiated by declared PROPERTIES, not different kinds: reload
  semantics (config hot-swaps atomically mid-game; assets load once per level) and
  directional cross-collection references (config → assets, a DAG verified at
  data-compile time). Aspirationally `Config.bin`/`Assets.bin` are expressions of a
  general collection pattern — his `Constants.h` already carries FOUR data blobs
  (config, options, assets, user settings), so first-class `config`/`assets` keywords
  would undercount day one; the fallback is first-class concepts if the general form
  fails. The frame: **the schema compiler is a compiler-linker for data** — JSON
  instances are source files with human authors (designers, artists exporting from
  Maya/Blender), one source directory against one declared set of types into one
  versioned, hashed binary with generated loaders, accessors and derived enums;
  verification is a compile step. `schema pack` is the transition form. Structural
  shapes still owed from the .fbs survey: enums with explicit values at the table
  layer, unions at the type level, vectors of tables.
- **Enum convergence (his catch):** some hand-declared enums are flatbuffers residue;
  the table pass derives them from the definitions (`ShipType` from ship
  configs/assets) — the set-extraction move a third time (messages → `MessageType`,
  objects → `ObjectType`, definitions → their type enums). Genuinely hand-owned sets
  (`Team`) stay declared.
- **Constants migrate through a temporary duplicate set** (schema + flatbuffers) while
  .fbs consumers remain; `Constants.schema` is the one home, C++ reads generated
  `space::` values through name-preserving aliases, and a static_assert guard block
  makes drift a compile error. Enacted in space 2026-08-11.
- **Generated files are checked into a consumer repo exactly when consumers exist that
  cannot run the generator** (Unity, a Go module on fresh clone) — *"If it were just
  C++ only, I would not."* C++-only consumers generate at build (the space CMake
  `schema_generate` target).
- **The event set is the nearest edge:** its wire half is already expressible in v1
  (an enum-dispatched union with per-case fields — the `Messages.schema` shape; the
  current `events.fbs` union maps onto it directly); only its table-layer half waits.
- **Reserved surface for these passes:** `packet`, `delta`, `baseline`, `index`
  (SPEC §1); `lerp`/`slerp`/`snap`/`angle`/`smooth` informally reserved as attribute
  values for the interpolation pass; SPEC §9 q11 banks the cut `enum_index` design;
  §9 q13's design input — measured interpolation policy is 100% type-derived across
  all four real objects (lerp floats, shortest-arc nlerp rotations, snap discrete),
  no field overrides its type's policy.
- **The v1 scope sentences (Glenn, 2026-08-05, verbatim):** *"goal for v1 is schema
  fully defines generated code for the constants, enums, types, messages, object
  definitions. the delta serialization is out of scope of v1."* Object layer v1
  deliverable: *"just build the structs from the definition in schema lang to start.
  (shallow, deep, interpolated, quantize)."*

## SPEC maintenance ledger (from the 2026-08-18 human-readability rewrite)

SPEC.md was rewritten 2026-08-18 into plain reference register on Glenn's directive —
most recent state only, no history, no decision narration. Nothing normative changed.
The material that left the spec lives in three places: **git** (the last pre-rewrite
revision carries every DECIDED date, verbatim ruling and repair note inline),
**notes/road-to-v1.md** (the road-to-v1 decision list), and **this file**. Section
numbers §1–§9 and the §9 q-rows are frozen — code, corpus and docs cite them.

- **Testing status at the rewrite** (was SPEC §7.2's status paragraph): gates 1, 2 and 7
  live across the matrix (`testdata/golden/`, `testdata/wire/` — C++ writes the pins,
  every other leg byte-compares them; both C++ message representations proven
  byte-identical); gate 4 live in pinned-instance form plus the randomized C++
  round-trip suite; gate 3 in seed form (RigidBody and string classic twins in
  `test/main.cpp`), growing with the corpus; gate 6 live as the break-the-language
  diagnostics suite (70+ cases) plus `internal/fuzz` (compile, oracle, property and
  pack fuzzers); gate 5 and gate 4's full random matrix are the remaining conformance
  work. A fmt-drift gate asserts the corpus stays formatter-canonical.
- **Held-loosely tells** (the spec states these as law; the confidence metadata lives
  here): the bracket attribute syntax carries Glenn's hedge ("just a personal
  preference, could be wrong but let's see in time"); the Contexts mechanism and the
  §4.8 view-encoding rules are Rowan-drafted transcriptions of the measured
  hand-written referents (`Ship.h`/`core_quantize.h`/`core_delta.h`) ratified in use;
  the §5 reused-output tail rule is priced-later (tail-clearing on read would be a
  generated-code change); whether untaken branches should restore specified defaults
  instead of zeros is Glenn's call if a real case ever wants it; an exact-length
  `bytes` form and a validated-UTF-8 read mode can return if a need appears.
- **Open follow-ups carried out of the spec text:** the C# `Debug.Assert` half of the
  §4.7 UTF-8 writer contract is a recorded follow-up (C#, like Go, asserts nothing
  today); the scanner's BOM diagnostic still says "it would silently move the protocol
  id" — the source-hash rationale, stale since the §3.1 projection ruling — a cleanup
  that must re-pin its exact-message diagnostics test. The same stale class is wider
  than the scanner: all six codegen backends emit "the hash of its schema files
  (SPEC §3.1)" into every generated file's header (38 occurrences across `generated/`
  and the goldens today), and `internal/check/check.go` still cites §3.1 for
  sorted-basename *hashing* — pre-projection language throughout; fixing it is one
  deliberate pass with a golden re-pin. WIRES.md's message-wire paragraph lists four
  runtimes where six exist.
- **§6.1 gaps equal in the old text** (surfaced by the rewrite's verification pass,
  not introduced by it): the symbol-naming paragraph gives five targets' conventions
  but not C's (lower_snake free functions — `write_ship_data_deep(stream, value)` —
  with `schema_`-prefixed internal helpers), and neither output-layout bullet names
  the third per-file header (`<Base>Table.h`) that C and C++ closure files emit.
- **Removed as dead grammar residue** (not relocated — it described nothing the
  grammar can express): §4.6's `bytes(<= N)` error row; the `<=` marker on
  string/bytes died in the §4.7 unification. The EBNF gained the `ufixed(I, F)`
  production and string-valued attributes (`cpp_include`) — both were already live
  language, stale in the grammar block only.
- **Deployment doctrine** (Glenn, 2026-08-15): *"I will always deploy client and
  server together on any breakage. so this is no concern."* The 2b rounding
  unification (half away from zero, superseding the pure-shift form) moved shipped
  narrowing bytes only on exact negative ties; the version note lives in
  VERSIONING.md, and the discriminating tie vector is pinned in the conformance
  corpus.
- **Union vs variant, the measured basis for the §4.8 C-flavored-default principle:**
  isolating pure header cost (one representation-agnostic TU, arm64 clang), union
  0.09 s vs variant 0.13 s — the variant surface alone costs +44% header compile
  time; Glenn's ruling on the earlier, cruder number stands as the principle ("0.17
  -> 0.27 is not trivial for me... you almost always pay through the nose for it at
  compile time"). The union arm-zeroing shape (zero at arm selection, not
  whole-union memset at construction — the memset measured 60.6% of batch-read
  self-cycles) is pinned by the stale-leak test.
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
tables — emitted by the compiler into the generated C/C++/C#/Go/Rust the same way the bitpacking
ranges already are — are the cheapest first experiment, need no adaptive state, and keep the
decoder branch-free. That is a far better fit than a general-purpose adaptive model, and it is a
thing this project can do that a standalone serializer cannot.

**Two constraints to design against before any of that:**

- **rANS is LIFO** — the encoder emits in reverse decode order, so an entropy stage means
  buffering a message or a bounded block rather than a single forward streaming pass.
- **A schema-compiled table is a WIRE FORMAT COMMITMENT.** Change the frequencies and you change
  the bytes. Versioning and cross-language byte-exactness (the property this project already
  guards hardest) both have to survive it, across all five backends.

**And the licensing constraint is real**: Microsoft's `US11234023B2` covers specific rANS
refinements, and their public permission is scoped to open source that **does not charge a
license fee** — which the declared MBSL direction would break. Any entropy stage must therefore
be **optional and versioned**, never welded into the format. Details and the recommended shape
are in the serialize note.
