# Relaxed checks

The maintainer, 2026-11-14: "I suggest that you simply relax or comment out
tests to get to green now, lest you have to finish the entirety of schema just
to get to green."

Every check below was relaxed on 2026-11-14 to get `main` green, and nothing
else. Each one is tracked so it can be undone: the site carries a comment
`RELAXED 2026-11-14 (maintainer: get main green): ...; re-enable: relax/<slug>`,
and the slug names the entry here and the sprint card that re-enables it. No
test was deleted and no library code was changed. To re-enable one: land the
real fix, undo the site (search the tree for its slug), and remove its entry
here in the same change.

The proof's relaxed cells are the `relaxed` list of
`test/proof/in-progress.json`. A relaxed cell does not hold the proof red, it
does hold a release (`tools/matrix` refuses a release with any relaxed cell),
and a relaxed cell that is no longer red refuses the proof until its entry is
removed, so a fixed cell cannot hide in the list. `tools/matrix` refuses a
relaxed cell whose slug has no entry here.

## Wire fuzz

- `relax/dead-bytes`: the C# wire fuzz (`make tables-cs-wire-fuzz`, and its
  smoke) passes `--relax-dead-bytes`, so a fixed-class mutant that the leg
  refuses and the oracle accepts, and whose oracle re-encoding has the input's
  length but different bytes (the array tail past a counted array's live
  count, an absent optional's payload, a text tail, an inactive union arm), is
  counted and not compared. Every other mutant is compared. Files:
  `test/conformance/harness/main.go`, `test/conformance/harness/wirefuzz.go`,
  `make/cs.mk`. Real fix: the engine and every reader refuse nonzero dead
  bytes (the maintainer's ruling: absent-optional dead bytes are refused);
  then drop the flag. WIP: `fix/main-red/dead-bytes`.

## Conformance cases and rows

Relaxed conformance cases are `test/conformance/relaxed.txt`, one line per
case (`<lang> <surface> <case> relax/<slug>`), read by
`test/conformance/harness/relaxed.go`. A listed case that fails stays "fail"
in the leg's results (so the proof names it) and does not fail the leg; a
listed case that passes fails the leg until its line is removed.

- The hostile rows, one slug per leg. #3486 brought the corpus up to three
  landed rulings: json-hostile `base64-unpadded` is refused
  (json-base64-canonical), `str-zero-escape-unknown-key` is refused (the
  skipped value is validated), and write-refusal-json `jref_zero_byte` is
  refused (the text writer refuses a zero byte, docs/SPEC.md §5.1 row 8). The
  Go engine and the Go JSON walk carry all three; the legs that still read
  a hostile row (or write the zero byte) have their cases in
  `test/conformance/relaxed.txt`. The table-base64 harness
  (`test/table-base64/harness`) expects the canonical rule too; its `-relax
  <slug>` flag, which keeps the old accept for the non-canonical cases only,
  is no longer passed by the C++ run (2026-11-15) and goes from the C and C# runs with the card sm-a1-main-green-relax-and-conformance (`make/checks/table-base64.mk`). Real fix, per
  leg: its JSON read refuses unpadded Base64 and set pad bits, and validates a
  skipped value; its text writer refuses a zero byte (a debug assert in C,
  C++, C#, Rust, Java and Dart; a refusal in every build elsewhere). Then the
  leg's lines go. WIP: none.
  Each slug below marked "gated again" has no relaxed site left: its rows
  passed on main on 2026-11-15 and their relax lines were removed. It stays
  listed only because `test/proof/in-progress.json` still names its proof
  cells (`tools/matrix` refuses a relaxed cell whose slug is not listed here);
  the entry goes with those cells at the next `make proof-write`.
  - `relax/hostile-rows-c`: gated again (both hostile rows, jref_zero_byte, table-base64; the relaxed.txt lines and the table-base64 flag go with the card sm-a1-main-green-relax-and-conformance).
  - `relax/hostile-rows-cpp`: gated again (both hostile rows, jref_zero_byte, table-base64, the hostile-corpus run, and the pack hostile-value gate, `Makefile`).
  - `relax/hostile-rows-cs`: gated again (both hostile rows, jref_zero_byte, table-base64, and the hostile-corpus run; the relaxed.txt lines and the table-base64 flag go with the card sm-a1-main-green-relax-and-conformance).
  - `relax/hostile-rows-elixir`: gated again (str-zero-escape-unknown-key, jref_zero_byte, and the hostile-corpus run).
  - `relax/hostile-rows-go`: gated again (json-hostile str-zero-escape-unknown-key and write-refusal-json jref_zero_byte in the go conformance leg, and the row its hostile-corpus run used to drop from the staged manifest, `make/schema-go-runs-hostile-corpus.mk`).
  - `relax/hostile-rows-goose`: gated again (both hostile rows; the relaxed.txt lines go with the card sm-a1-main-green-relax-and-conformance).
  - `relax/hostile-rows-java`: neither hostile row is relaxed: the JSON read refuses unpadded Base64 and validates a skipped value, so both are gated in the conformance leg and in the port's hostile-corpus run. Its proof cell (`test/proof/in-progress.json`, surface json-hostile/java) still names the slug while that cell's other json-hostile cases fail; it goes, with this entry, in the change that removes that cell.
  - `relax/hostile-rows-js`: gated again. The JSON read refuses unpadded base64 and nonzero pad bits, and validates skipped values; the writer refuses zero bytes.
  - `relax/hostile-rows-lobster`: gated again (jref_zero_byte). The JSON read refuses unpadded Base64 and set pad bits, and validates a skipped value, so neither hostile row is relaxed.
  - `relax/hostile-rows-lua`: gated again (str-zero-escape-unknown-key and jref_zero_byte); the JSON read validates skipped values and refuses invalid Base64.
  - `relax/hostile-rows-luau`: gated again (base64-unpadded, in the conformance leg and in the port's hostile-corpus run).
  - `relax/hostile-rows-moo`: gated again (both hostile rows).
  - `relax/hostile-rows-squirrel`: gated again (base64-unpadded, in the conformance leg and in the port's hostile-corpus run, `internal/ci/schema_squirrel_runs_hostile_corpus_test.go`); the JSON walk refuses a U+0000 in an object key or a skipped string (read.go scanKey).

## Negative controls

- `relax/nc-plan-write-refusal-js-lobster`: the JavaScript (PR 3291) and
  Lobster (PR 3294) write-contract controls landed with no group in
  `make/negative-controls.json`, which red the plan check; they are placed in
  two new nightly groups, `js-write-refusal` and `lobster-write-refusal`,
  unmeasured. Real fix: measure them and move each group to the pull-request
  tier. WIP: none.
- `relax/nc-elixir-wire-fuzz-array-tail`: the group `elixir-wire-fuzz`
  (`tables-elixir-wire-fuzz-negative-control`, registered by PR 3290) moved
  from the pull-request tier to nightly. Two causes, both measured in main CI
  full 43638688121 attempt 2 on aws-schema-1-2: the plant, which removes the
  rule that bytes left inside an array's L are damage from a copy of the
  featurewitnesses reader, does not turn the Elixir wire fuzz red ("a reader
  that keeps bytes left inside an array's L left the wire fuzz green"); and the
  step takes 176 s against the 110 s per-commit wall. Files:
  `make/negative-controls.json`. Real fix: re-aim the plant at a check the
  fuzz's mutants reach, fit the control under 110 s, then return the group to
  the pull-request tier. WIP: none.
- `relax/nc-elixir-discard-masked`: the group `elixir-discard`
  (`tables-elixir-discard-negative-control`) moved from the pull-request
  tier to nightly. PR 3290 made Elixir's load the one strict read: its
  framing check (`strict_syntax!`) now refuses the damaged vector before the
  load reaches the discard the control removes, so with the discard gone
  `TestDamageDiscardsTheWholeLoadElixir` stays green and the control cannot go
  red. Files: `make/negative-controls.json`. Real fix: aim the sabotage at the
  check that now holds the whole-load discard (or drop the discard if it is
  dead code), then return the group to the pull-request tier. WIP: none.
- `relax/cook-dead-slots-zero-ports`: the Go cook writer is gated again.
  `TestCookWriterMatchesIndependentLayout/fixed` runs, the ten Go cook-write
  rows are removed from `test/conformance/relaxed.txt`, and all 142 Go
  cook-write rows pass in both byte orders. The writer ignores dead counted
  slots and absent optional payloads, and a generated-artifact negative
  control restores the dead writes and fails the independent byte oracle.
  This discharges the Go relaxation; other ports' whole-cell proofs remain
  tracked by their own roadmap evidence.

## CI time (the 110 s per-commit wall)

Each job below had a median job wall over 110 s across four CI full runs on
2026-11-13/14 (43478876654, 43605193716, 43605777296, 43606021116). It is
taken off the push and pull-request events (the events `PGROUP_WALL=110s`
applies to) and runs unchanged, at full strength, on the nightly schedule and
on a dispatch. Real fix for each: make it fit 110 s (parallel first, or split
the row), then return it to the per-commit tier. WIP: none unless named.

go-test rows (the `exclude:` of `go-test` in `.github/workflows/ci-full.yml`):

- `relax/ci-time-unit-codegen`: go-test (unit-codegen), median 642 s (the
  C++ table compile; PR 3275 is the partial fix, `fix/main-red/cpp-codegen-timeout`).
- `relax/ci-time-versioning-cs`: go-test (versioning-cs), median 193 s.
- `relax/ci-time-versioning-rust`: go-test (versioning-rust), median 131 s.
- `relax/ci-time-versioning-java`: go-test (versioning-java), median 129 s.
- `relax/ci-time-versioning-c`: go-test (versioning-c), median 128 s.
- `relax/ci-time-unit-rest-ci`: go-test (unit-rest-ci), median 118 s.
- `relax/ci-time-unit-rest-fuzz-2`: go-test (unit-rest-fuzz-2), median 115 s.
- `relax/ci-time-unit-elixir-3`: go-test (unit-elixir-3), killed at the 110 s
  wall in main CI full 43630522257 with TestElixirCellsStrictNegativeControl and
  TestNodeRecordListCountRefusalElixir still running, on runner captain-8
  (host captainamerica, 64 cores, no core cap on the runner service, load1
  96.5 at the end of the job).

Fuzz rows (`.github/workflows/ci-full.yml`):

- `relax/ci-time-js-fuzz-retain`: JavaScript fuzz (retain), median 261 s,
  excluded per commit (its cost is the enumerated pass, which a smaller N does
  not cut).
- `relax/ci-time-dart-fuzz-wire`: Dart fuzz (wire), median 132 s, the job
  skipped per commit (same reason).
- `relax/ci-time-cs-fuzz-region`: C# fuzz (region), median 127 s, runs
  `tables-cs-region-fuzz-smoke` (CS_FUZZ_SMOKE_N random mutants) per commit
  and the full N nightly.

Negative-control groups moved from `pull-request` to `nightly` in
`make/negative-controls.json` (each group's `why` carries the marker; restore
the text after "Before: " and the tier):

- `relax/ci-time-dart-conformance-a`: median 298 s.
- `relax/ci-time-squirrel-tools-a`: median 255 s.
- `relax/ci-time-go-proof-3`: median 165 s.
- `relax/ci-time-goose-gates`: median 153 s.
- `relax/ci-time-dart-tables-2-a`: median 142 s.
- `relax/ci-time-block-fuzz-maximum`: median 139 s.
- `relax/ci-time-lobster-conformance`: median 139 s.
- `relax/ci-time-java-proof`: median 137 s.
- `relax/ci-time-base-tool-cook-differential-a`: median 136 s.
- `relax/ci-time-dart-conformance-2-a`: median 136 s.
- `relax/ci-time-java-proof-2`: median 135 s.
- `relax/ci-time-rust-retain-soak`: median 135 s.
- `relax/ci-time-js-tables-2`: median 135 s.
- `relax/ci-time-base-3-3`: median 126 s.
- `relax/ci-time-java-wire-fuzz-2`: median 123 s.
- `relax/ci-time-java-wire-fuzz-retain`: 109 to 125 s, killed at 120 s twice.
- `relax/ci-time-java-wire-fuzz-map-count`: 112 to 125 s, killed at 120 s twice.
- `relax/ci-time-java-tables-a`: median 123 s.
- `relax/ci-time-java-tables-more-a`: median 117 s.
- `relax/ci-time-base-c-write-refusal-a`: median 116 s.
- `relax/ci-time-rust-conformance-framing`: not a whole group; the one
  control `conformance-negative-control-rust-framing`, moved out of
  `rust-conformance-framing` into the nightly group
  `rust-conformance-framing-control`. It builds two sabotaged drivers in
  sequence, about 67 s each on an AWS runner (main CI full 43635368439), so it
  is over 110 s alone; the rest of its old group is split into
  `rust-conformance-framing` and `rust-conformance-absent`, both per commit.
  Real fix: build the two drivers in parallel or from one build, then return
  the control to a pull-request group.
- `relax/ci-time-rust-tables-1`: median 114 s.

Left per commit although over 110 s, because a test pins their tier or rows:
negative controls go-message, dart-tables-1, luau-conformance, luau-fuzz-b,
and big-endian (c-control). The conformance legs (cpp, rust, java, dart,
goose, squirrel, lua, elixir, luau, cs, lobster) stay per commit because the
proof job reads their results; their time is mostly the un-walled artifact
download, not a walled step.

## Go tests (2026-11-15)

Relaxed on 2026-11-15 to get the fast lane green on `main` after a day of
landings with no gate; each site carries `RELAXED 2026-11-15 (get main
green)` and its slug.


## Proof cells

Each proof cell below is red at main 3b43e7075, computed from every conformance
leg run on that commit (the cells are the `relaxed` list of
`test/proof/in-progress.json`). Generated from `tools/matrix proof`'s red
list; each line names its cells and why they are red. The write-refusal proof
slugs are each leg answering the four write-refusal surfaces; I15 on elixir is
PR 3290. WIP for every proof slug: none unless named here.

- `relax/proof-i15-elixir`: 1 cell(s): `porting I15 / elixir` (❌ #492 — `tables-elixir-wire-fuzz-all` drives the harness over the default roster (plain arm) and is red: a fixed-class root it cannot o...). Real fix: the porting register's I15 cell for elixir is proven (docs/PORTING.md).
- `relax/proof-i3-luau`: 1 cell(s): `porting I3 / luau` (❌ #381 — card schema-luau-fuzz-differential (docs/PORT-LUAU.md, THE CARD MAP)). Real fix: the porting register's I3 cell for luau is proven (docs/PORTING.md).
- `relax/proof-i3-moo`: 1 cell(s): `porting I3 / moo` (❌ #381 — partial: `tables-moo-block-forgery-fuzz` and its negative control validate production BlockOpen over 8,000 mutants of two block...). Real fix: the porting register's I3 cell for moo is proven (docs/PORTING.md).
- `relax/proof-i3-squirrel`: 1 cell(s): `porting I3 / squirrel` (❌ #413 (the block and cook forgery batteries ride the conformance surfaces; no forgery fuzzer walks what it opened)). Real fix: the porting register's I3 cell for squirrel is proven (docs/PORTING.md).
- `relax/proof-write-refusal-dart`: 5 cell(s): `surface write-refusal / dart` (19 case(s) answered absent with no ruling: write-refusal/wref_bits_wide, write-refusal/wref_bits_wide_ok, write-refusal/wref_count and 16 mo...); `surface write-refusal-fixed / dart` (19 case(s) answered absent with no ruling: write-refusal-fixed/fref_bits_wide, write-refusal-fixed/fref_bits_wide_ok, write-refusal-fixed/fr...); `surface write-refusal-message / dart` (13 case(s) answered absent with no ruling: write-refusal-message/mref_element, write-refusal-message/mref_element_ok, write-refusal-message/...); `surface write-refusal-json / dart` (1 case(s) answered absent with no ruling: write-refusal-json/jref_zero_byte); `cases tabledemo / dart` (39 case(s) answered absent with no ruling: write-refusal/wref_bits_wide, write-refusal/wref_bits_wide_ok, write-refusal/wref_count and 36 mo...). Real fix: the leg answers the write-refusal surfaces (docs/SPEC.md §5.1: the write contract; assert legs in debug, refusing legs in every build).
- `relax/proof-write-refusal-goose`: 5 cell(s): `surface write-refusal / goose` (19 case(s) answered absent with no ruling: write-refusal/wref_bits_wide, write-refusal/wref_bits_wide_ok, write-refusal/wref_count and 16 mo...); `surface write-refusal-fixed / goose` (19 case(s) answered absent with no ruling: write-refusal-fixed/fref_bits_wide, write-refusal-fixed/fref_bits_wide_ok, write-refusal-fixed/fr...); `surface write-refusal-message / goose` (13 case(s) answered absent with no ruling: write-refusal-message/mref_element, write-refusal-message/mref_element_ok, write-refusal-message/...); `surface write-refusal-json / goose` (1 case(s) answered absent with no ruling: write-refusal-json/jref_zero_byte); `cases tabledemo / goose` (39 case(s) answered absent with no ruling: write-refusal/wref_bits_wide, write-refusal/wref_bits_wide_ok, write-refusal/wref_count and 36 mo...). Real fix: the leg answers the write-refusal surfaces (docs/SPEC.md §5.1: the write contract; assert legs in debug, refusing legs in every build).
- `relax/proof-write-refusal-java`: 5 cell(s): `surface write-refusal / java` (19 case(s) answered absent with no ruling: write-refusal/wref_bits_wide, write-refusal/wref_bits_wide_ok, write-refusal/wref_count and 16 mo...); `surface write-refusal-fixed / java` (19 case(s) answered absent with no ruling: write-refusal-fixed/fref_bits_wide, write-refusal-fixed/fref_bits_wide_ok, write-refusal-fixed/fr...); `surface write-refusal-message / java` (13 case(s) answered absent with no ruling: write-refusal-message/mref_element, write-refusal-message/mref_element_ok, write-refusal-message/...); `surface write-refusal-json / java` (1 case(s) answered absent with no ruling: write-refusal-json/jref_zero_byte); `cases tabledemo / java` (87 case(s) answered absent with no ruling: cook-write/archive, cook-write/keyed_config, cook-write/keyed_default and 84 more). Real fix: the leg answers the write-refusal surfaces (docs/SPEC.md §5.1: the write contract; assert legs in debug, refusing legs in every build).
- `relax/proof-write-refusal-luau`: 5 cell(s): `surface write-refusal / luau` (19 case(s) answered absent with no ruling: write-refusal/wref_bits_wide, write-refusal/wref_bits_wide_ok, write-refusal/wref_count and 16 mo...); `surface write-refusal-fixed / luau` (19 case(s) answered absent with no ruling: write-refusal-fixed/fref_bits_wide, write-refusal-fixed/fref_bits_wide_ok, write-refusal-fixed/fr...); `surface write-refusal-message / luau` (13 case(s) answered absent with no ruling: write-refusal-message/mref_element, write-refusal-message/mref_element_ok, write-refusal-message/...); `surface write-refusal-json / luau` (1 case(s) answered absent with no ruling: write-refusal-json/jref_zero_byte); `cases tabledemo / luau` (39 case(s) answered absent with no ruling: write-refusal/wref_bits_wide, write-refusal/wref_bits_wide_ok, write-refusal/wref_count and 36 mo...). Real fix: the leg answers the write-refusal surfaces (docs/SPEC.md §5.1: the write contract; assert legs in debug, refusing legs in every build).
- `relax/proof-write-refusal-moo`: 5 cell(s): `surface write-refusal / moo` (19 case(s) answered absent with no ruling: write-refusal/wref_bits_wide, write-refusal/wref_bits_wide_ok, write-refusal/wref_count and 16 mo...); `surface write-refusal-fixed / moo` (19 case(s) answered absent with no ruling: write-refusal-fixed/fref_bits_wide, write-refusal-fixed/fref_bits_wide_ok, write-refusal-fixed/fr...); `surface write-refusal-message / moo` (13 case(s) answered absent with no ruling: write-refusal-message/mref_element, write-refusal-message/mref_element_ok, write-refusal-message/...); `surface write-refusal-json / moo` (1 case(s) answered absent with no ruling: write-refusal-json/jref_zero_byte); `cases tabledemo / moo` (39 case(s) answered absent with no ruling: write-refusal/wref_bits_wide, write-refusal/wref_bits_wide_ok, write-refusal/wref_count and 36 mo...). Real fix: the leg answers the write-refusal surfaces (docs/SPEC.md §5.1: the write contract; assert legs in debug, refusing legs in every build).
- `relax/proof-write-refusal-rust`: 5 cell(s): `surface write-refusal / rust` (7 case(s) fail: write-refusal/wref_bits_wide, write-refusal/wref_enum, write-refusal/wref_range_high and 4 more); `surface write-refusal-fixed / rust` (7 case(s) fail: write-refusal-fixed/fref_bits_wide, write-refusal-fixed/fref_enum, write-refusal-fixed/fref_range_high and 4 more); `surface write-refusal-message / rust` (5 case(s) fail: write-refusal-message/mref_element, write-refusal-message/mref_range_high, write-refusal-message/mref_range_low and 2 more); `surface write-refusal-json / rust` (1 case(s) answered absent with no ruling: write-refusal-json/jref_zero_byte); `cases tabledemo / rust` (14 case(s) fail: write-refusal/wref_bits_wide, write-refusal/wref_enum, write-refusal/wref_range_high and 11 more). Real fix: the leg answers the write-refusal surfaces (docs/SPEC.md §5.1: the write contract; assert legs in debug, refusing legs in every build).
- `relax/proof-write-refusal-squirrel`: 5 cell(s): `surface write-refusal / squirrel` (19 case(s) answered absent with no ruling: write-refusal/wref_bits_wide, write-refusal/wref_bits_wide_ok, write-refusal/wref_count and 16 mo...); `surface write-refusal-fixed / squirrel` (19 case(s) answered absent with no ruling: write-refusal-fixed/fref_bits_wide, write-refusal-fixed/fref_bits_wide_ok, write-refusal-fixed/fr...); `surface write-refusal-message / squirrel` (13 case(s) answered absent with no ruling: write-refusal-message/mref_element, write-refusal-message/mref_element_ok, write-refusal-message/...); `surface write-refusal-json / squirrel` (1 case(s) answered absent with no ruling: write-refusal-json/jref_zero_byte); `cases tabledemo / squirrel` (39 case(s) answered absent with no ruling: write-refusal/wref_bits_wide, write-refusal/wref_bits_wide_ok, write-refusal/wref_count and 36 mo...). Real fix: the leg answers the write-refusal surfaces (docs/SPEC.md §5.1: the write contract; assert legs in debug, refusing legs in every build).
