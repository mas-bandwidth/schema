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
  writer. WIP: none (PR 3281 covers C, C++ and C# only).
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
