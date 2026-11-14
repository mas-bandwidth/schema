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

- `relax/rust-write-refusal-debug-asserts`: 19 cases of the Rust leg (write-refusal
  wref_bits_wide, wref_enum, wref_range_high, wref_range_low,
  wref_text_illformed, wref_text_zero, wref_union_tag; write-refusal-fixed the
  same seven as fref_*; write-refusal-message mref_element, mref_range_high,
  mref_range_low, mref_text_illformed, mref_text_zero). PR 3282 moved the
  rows onto the fixed and message writers, which carry no debug-build checks.
  Rust is an assert leg, so this is owed work, not a missing release check.
  Files: `test/conformance/relaxed.txt`. Real fix: debug asserts of the write
  contract in `internal/codegen/rusttable/fixedform.go` and the message
  writer. WIP: none.
- `relax/deprecated-carried`: `test/conformance/go/rows/R20_test.go`, clause 2
  (`TestRowR20DroppedFieldUnknown`). The header is restored to the
  maintainer's ruling of 2026-11-10 (a `| deprecated` fixed-form field is
  carried: "if it's supplied, use the value there, otherwise, use the
  default"; docs/FIXED-FORM-ALGORITHM.md §5.1; the C++ R20), and the one
  assertion that the stored value lands (`B == 2`) is commented out, because
  Go's fixed reader still yields the declared default. Real fix: the ruling's
  rollout across Go's emitter (`fixedDeprecatedDefault`), the engine
  (tablewire), Lobster, Go's TestFixedDeprecatedSlotIsTheDeclaredDefault, and
  docs/SPEC-TABLES.md §3.4, §21.2 and §22.6. WIP: none.

## Negative controls

- `relax/luau-fuzz-b-design`: two plants of
  `test/conformance/luau/wire-fuzz-vectors-negative-control` are commented
  out. `list_count_cross_length` cannot be caught: the driver's size pre-check
  refuses the count before the load's own cap check (0 divergences in 222k
  mutants). `region_align_extent_uint128` only moves the driver-reported size,
  which the spec does not compare. Real fix: a design call for the maintainer,
  retire the two or compare driver size again. WIP: none (PR 3283 found it).
- `relax/nc-plan-write-refusal-js-lobster`: the JavaScript (PR 3291) and
  Lobster (PR 3294) write-contract controls landed with no group in
  `make/negative-controls.json`, which red the plan check; they are placed in
  two new nightly groups, `js-write-refusal` and `lobster-write-refusal`,
  unmeasured. Real fix: measure them and move each group to the pull-request
  tier. WIP: none.

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
- `relax/ci-time-java-tables-a`: median 123 s.
- `relax/ci-time-java-tables-more-a`: median 117 s.
- `relax/ci-time-base-c-write-refusal-a`: median 116 s.
- `relax/ci-time-rust-tables-1`: median 114 s.

Left per commit although over 110 s, because a test pins their tier or rows:
negative controls go-message, dart-tables-1, luau-conformance, luau-fuzz-b,
and big-endian (c-control). The conformance legs (cpp, rust, java, dart,
goose, squirrel, lua, elixir, luau, cs, lobster) stay per commit because the
proof job reads their results; their time is mostly the un-walled artifact
download, not a walled step.

## Proof cells

Each proof cell below is red at main 3b43e7075, computed from every conformance
leg run on that commit (the cells are the `relaxed` list of
`test/proof/in-progress.json`). Generated from `tools/matrix proof`'s red
list; each line names its cells and why they are red. Where the real fix is
an open PR: write-refusal on rust is
relax/rust-write-refusal-debug-asserts; the rest of the write-refusal slugs are
each leg answering the four write-refusal surfaces; I15 on elixir is PR 3290.
WIP for every proof slug: none unless named here.

- `relax/proof-cook-write-java`: 1 cell(s): `surface cook-write / java` (60 case(s) answered absent with no ruling: cook-write/archive, cook-write/archive-be, cook-write/chain_optional and 57 more). Real fix: the leg answers cook-write for every unit.
- `relax/proof-i15-elixir`: 1 cell(s): `porting I15 / elixir` (❌ #492 — `tables-elixir-wire-fuzz-all` drives the harness over the default roster (plain arm) and is red: a fixed-class root it cannot o...). Real fix: the porting register's I15 cell for elixir is proven (docs/PORTING.md).
- `relax/proof-i3-luau`: 1 cell(s): `porting I3 / luau` (❌ #381 — card schema-luau-fuzz-differential (docs/PORT-LUAU.md, THE CARD MAP)). Real fix: the porting register's I3 cell for luau is proven (docs/PORTING.md).
- `relax/proof-i3-moo`: 1 cell(s): `porting I3 / moo` (❌ #381 — partial: `tables-moo-block-forgery-fuzz` and its negative control validate production BlockOpen over 8,000 mutants of two block...). Real fix: the porting register's I3 cell for moo is proven (docs/PORTING.md).
- `relax/proof-i3-squirrel`: 1 cell(s): `porting I3 / squirrel` (❌ #413 (the block and cook forgery batteries ride the conformance surfaces; no forgery fuzzer walks what it opened)). Real fix: the porting register's I3 cell for squirrel is proven (docs/PORTING.md).
- `relax/proof-json-java`: 15 cell(s): `surface json-read / java` (60 case(s) answered absent with no ruling: json-read/archive, json-read/chain_optional, json-read/chain_optional_empty and 57 more); `surface json-write / java` (61 case(s) answered absent with no ruling: json-write/archive.json, json-write/chain_optional.json, json-write/chain_optional_empty.json and...); `cases tblv1 / java` (6 case(s) answered absent with no ruling: cook-write/v1_cfg, cook-write/v1_seams, json-read/v1_cfg and 3 more); `cases tblv2 / java` (6 case(s) answered absent with no ruling: cook-write/v2_cfg, cook-write/v2_seams, json-read/v2_cfg and 3 more); `cases tblp1 / java` (6 case(s) answered absent with no ruling: cook-write/chain_value, cook-write/chain_value_empty, json-read/chain_value and 3 more); `cases tblp3 / java` (6 case(s) answered absent with no ruling: cook-write/chain_optional, cook-write/chain_optional_empty, json-read/chain_optional and 3 more); `cases messagedemo / java` (30 case(s) answered absent with no ruling: cook-write/message_ack, cook-write/message_arm_edits, cook-write/message_arms and 27 more); `cases tblm1 / java` (5 case(s) answered absent with no ruling: cook-write/m1_open, json-read/m1_open, json-read/m1_save and 2 more); `cases tblm2 / java` (5 case(s) answered absent with no ruling: cook-write/m2_open, json-read/m2_close, json-read/m2_open and 2 more); `cases tblr1 / java` (4 case(s) answered absent with no ruling: json-read/r1_cfg, json-read/r1_ping, json-write/r1_cfg.json and 1 more); `cases tblr2 / java` (2 case(s) answered absent with no ruling: json-read/r2_cfg, json-write/r2_cfg.json); `cases scalars / java` (9 case(s) answered absent with no ruling: cook-write/scalars_default, cook-write/scalars_edges, cook-write/scalars_full and 6 more); `cases backenddemo / java` (14 case(s) answered absent with no ruling: json-read/login_default, json-read/login_full, json-read/match_default and 11 more); `cases vocabdemo / java` (6 case(s) answered absent with no ruling: json-read/peer_b, json-read/vocab_low, json-read/vocab_wide and 3 more); `cases vocab9demo / java` (4 case(s) answered absent with no ruling: json-read/vocab9_low, json-read/vocab9_wide, json-write/vocab9_low.json and 1 more). Real fix: the Java leg answers the JSON surfaces.
- `relax/proof-rust-fixed-roots`: 21 cell(s): `surface wire / rust` (4 case(s) answered absent with no ruling: wire/feature_array_root, wire/feature_full, wire/feature_key_order and 1 more); `surface report / rust` (1 case(s) answered absent with no ruling: report/text_total_and_count); `surface json-read / rust` (64 case(s) answered absent with no ruling: json-read/archive, json-read/chain_optional, json-read/chain_optional_empty and 61 more); `surface json-write / rust` (65 case(s) answered absent with no ruling: json-write/archive.json, json-write/chain_optional.json, json-write/chain_optional_empty.json and...); `surface json-hostile / rust` (4 case(s) answered absent with no ruling: json-hostile/text-total-after-clamp, json-hostile/text-total-and-count, json-hostile/text-total-at...); `surface cook-write / rust` (64 case(s) answered absent with no ruling: cook-write/archive, cook-write/archive-be, cook-write/blob_shared and 61 more); `cases featurewitnesses / rust` (18 case(s) answered absent with no ruling: cook-write/feature_full, json-hostile/text-total-after-clamp, json-hostile/text-total-and-count a...); `cases tblv1 / rust` (6 case(s) answered absent with no ruling: cook-write/v1_cfg, cook-write/v1_seams, json-read/v1_cfg and 3 more); `cases tblv2 / rust` (6 case(s) answered absent with no ruling: cook-write/v2_cfg, cook-write/v2_seams, json-read/v2_cfg and 3 more); `cases tblp1 / rust` (6 case(s) answered absent with no ruling: cook-write/chain_value, cook-write/chain_value_empty, json-read/chain_value and 3 more); `cases tblp3 / rust` (6 case(s) answered absent with no ruling: cook-write/chain_optional, cook-write/chain_optional_empty, json-read/chain_optional and 3 more); `cases messagedemo / rust` (30 case(s) answered absent with no ruling: cook-write/message_ack, cook-write/message_arm_edits, cook-write/message_arms and 27 more); `cases blobdemo / rust` (1 case(s) answered absent with no ruling: cook-write/blob_shared); `cases tblm1 / rust` (5 case(s) answered absent with no ruling: cook-write/m1_open, json-read/m1_open, json-read/m1_save and 2 more); `cases tblm2 / rust` (5 case(s) answered absent with no ruling: cook-write/m2_open, json-read/m2_close, json-read/m2_open and 2 more); `cases tblr1 / rust` (4 case(s) answered absent with no ruling: json-read/r1_cfg, json-read/r1_ping, json-write/r1_cfg.json and 1 more); `cases tblr2 / rust` (2 case(s) answered absent with no ruling: json-read/r2_cfg, json-write/r2_cfg.json); `cases scalars / rust` (9 case(s) answered absent with no ruling: cook-write/scalars_default, cook-write/scalars_edges, cook-write/scalars_full and 6 more); `cases backenddemo / rust` (14 case(s) answered absent with no ruling: json-read/login_default, json-read/login_full, json-read/match_default and 11 more); `cases vocabdemo / rust` (6 case(s) answered absent with no ruling: json-read/peer_b, json-read/vocab_low, json-read/vocab_wide and 3 more); `cases vocab9demo / rust` (4 case(s) answered absent with no ruling: json-read/vocab9_low, json-read/vocab9_wide, json-write/vocab9_low.json and 1 more). Real fix: the Rust driver reads and writes the declared fixed roots (now form 3 instances, a187ff158 and PR 3282) on every surface: text, cook-write, report and the feature witnesses, instead of answering them absent.
- `relax/proof-write-refusal-dart`: 5 cell(s): `surface write-refusal / dart` (19 case(s) answered absent with no ruling: write-refusal/wref_bits_wide, write-refusal/wref_bits_wide_ok, write-refusal/wref_count and 16 mo...); `surface write-refusal-fixed / dart` (19 case(s) answered absent with no ruling: write-refusal-fixed/fref_bits_wide, write-refusal-fixed/fref_bits_wide_ok, write-refusal-fixed/fr...); `surface write-refusal-message / dart` (13 case(s) answered absent with no ruling: write-refusal-message/mref_element, write-refusal-message/mref_element_ok, write-refusal-message/...); `surface write-refusal-json / dart` (1 case(s) answered absent with no ruling: write-refusal-json/jref_zero_byte); `cases tabledemo / dart` (39 case(s) answered absent with no ruling: write-refusal/wref_bits_wide, write-refusal/wref_bits_wide_ok, write-refusal/wref_count and 36 mo...). Real fix: the leg answers the write-refusal surfaces (docs/SPEC.md §5.1: the write contract; assert legs in debug, refusing legs in every build).
- `relax/proof-write-refusal-goose`: 5 cell(s): `surface write-refusal / goose` (19 case(s) answered absent with no ruling: write-refusal/wref_bits_wide, write-refusal/wref_bits_wide_ok, write-refusal/wref_count and 16 mo...); `surface write-refusal-fixed / goose` (19 case(s) answered absent with no ruling: write-refusal-fixed/fref_bits_wide, write-refusal-fixed/fref_bits_wide_ok, write-refusal-fixed/fr...); `surface write-refusal-message / goose` (13 case(s) answered absent with no ruling: write-refusal-message/mref_element, write-refusal-message/mref_element_ok, write-refusal-message/...); `surface write-refusal-json / goose` (1 case(s) answered absent with no ruling: write-refusal-json/jref_zero_byte); `cases tabledemo / goose` (39 case(s) answered absent with no ruling: write-refusal/wref_bits_wide, write-refusal/wref_bits_wide_ok, write-refusal/wref_count and 36 mo...). Real fix: the leg answers the write-refusal surfaces (docs/SPEC.md §5.1: the write contract; assert legs in debug, refusing legs in every build).
- `relax/proof-write-refusal-java`: 5 cell(s): `surface write-refusal / java` (19 case(s) answered absent with no ruling: write-refusal/wref_bits_wide, write-refusal/wref_bits_wide_ok, write-refusal/wref_count and 16 mo...); `surface write-refusal-fixed / java` (19 case(s) answered absent with no ruling: write-refusal-fixed/fref_bits_wide, write-refusal-fixed/fref_bits_wide_ok, write-refusal-fixed/fr...); `surface write-refusal-message / java` (13 case(s) answered absent with no ruling: write-refusal-message/mref_element, write-refusal-message/mref_element_ok, write-refusal-message/...); `surface write-refusal-json / java` (1 case(s) answered absent with no ruling: write-refusal-json/jref_zero_byte); `cases tabledemo / java` (87 case(s) answered absent with no ruling: cook-write/archive, cook-write/keyed_config, cook-write/keyed_default and 84 more). Real fix: the leg answers the write-refusal surfaces (docs/SPEC.md §5.1: the write contract; assert legs in debug, refusing legs in every build).
- `relax/proof-write-refusal-luau`: 5 cell(s): `surface write-refusal / luau` (19 case(s) answered absent with no ruling: write-refusal/wref_bits_wide, write-refusal/wref_bits_wide_ok, write-refusal/wref_count and 16 mo...); `surface write-refusal-fixed / luau` (19 case(s) answered absent with no ruling: write-refusal-fixed/fref_bits_wide, write-refusal-fixed/fref_bits_wide_ok, write-refusal-fixed/fr...); `surface write-refusal-message / luau` (13 case(s) answered absent with no ruling: write-refusal-message/mref_element, write-refusal-message/mref_element_ok, write-refusal-message/...); `surface write-refusal-json / luau` (1 case(s) answered absent with no ruling: write-refusal-json/jref_zero_byte); `cases tabledemo / luau` (39 case(s) answered absent with no ruling: write-refusal/wref_bits_wide, write-refusal/wref_bits_wide_ok, write-refusal/wref_count and 36 mo...). Real fix: the leg answers the write-refusal surfaces (docs/SPEC.md §5.1: the write contract; assert legs in debug, refusing legs in every build).
- `relax/proof-write-refusal-moo`: 5 cell(s): `surface write-refusal / moo` (19 case(s) answered absent with no ruling: write-refusal/wref_bits_wide, write-refusal/wref_bits_wide_ok, write-refusal/wref_count and 16 mo...); `surface write-refusal-fixed / moo` (19 case(s) answered absent with no ruling: write-refusal-fixed/fref_bits_wide, write-refusal-fixed/fref_bits_wide_ok, write-refusal-fixed/fr...); `surface write-refusal-message / moo` (13 case(s) answered absent with no ruling: write-refusal-message/mref_element, write-refusal-message/mref_element_ok, write-refusal-message/...); `surface write-refusal-json / moo` (1 case(s) answered absent with no ruling: write-refusal-json/jref_zero_byte); `cases tabledemo / moo` (39 case(s) answered absent with no ruling: write-refusal/wref_bits_wide, write-refusal/wref_bits_wide_ok, write-refusal/wref_count and 36 mo...). Real fix: the leg answers the write-refusal surfaces (docs/SPEC.md §5.1: the write contract; assert legs in debug, refusing legs in every build).
- `relax/proof-write-refusal-rust`: 5 cell(s): `surface write-refusal / rust` (7 case(s) fail: write-refusal/wref_bits_wide, write-refusal/wref_enum, write-refusal/wref_range_high and 4 more); `surface write-refusal-fixed / rust` (7 case(s) fail: write-refusal-fixed/fref_bits_wide, write-refusal-fixed/fref_enum, write-refusal-fixed/fref_range_high and 4 more); `surface write-refusal-message / rust` (5 case(s) fail: write-refusal-message/mref_element, write-refusal-message/mref_range_high, write-refusal-message/mref_range_low and 2 more); `surface write-refusal-json / rust` (1 case(s) answered absent with no ruling: write-refusal-json/jref_zero_byte); `cases tabledemo / rust` (14 case(s) fail: write-refusal/wref_bits_wide, write-refusal/wref_enum, write-refusal/wref_range_high and 11 more). Real fix: the leg answers the write-refusal surfaces (docs/SPEC.md §5.1: the write contract; assert legs in debug, refusing legs in every build).
- `relax/proof-write-refusal-squirrel`: 5 cell(s): `surface write-refusal / squirrel` (19 case(s) answered absent with no ruling: write-refusal/wref_bits_wide, write-refusal/wref_bits_wide_ok, write-refusal/wref_count and 16 mo...); `surface write-refusal-fixed / squirrel` (19 case(s) answered absent with no ruling: write-refusal-fixed/fref_bits_wide, write-refusal-fixed/fref_bits_wide_ok, write-refusal-fixed/fr...); `surface write-refusal-message / squirrel` (13 case(s) answered absent with no ruling: write-refusal-message/mref_element, write-refusal-message/mref_element_ok, write-refusal-message/...); `surface write-refusal-json / squirrel` (1 case(s) answered absent with no ruling: write-refusal-json/jref_zero_byte); `cases tabledemo / squirrel` (39 case(s) answered absent with no ruling: write-refusal/wref_bits_wide, write-refusal/wref_bits_wide_ok, write-refusal/wref_count and 36 mo...). Real fix: the leg answers the write-refusal surfaces (docs/SPEC.md §5.1: the write contract; assert legs in debug, refusing legs in every build).
