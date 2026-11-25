# CI tiers: what a pull request runs, and what waits for the night

The pull-request tier (`.github/workflows/ci-fast.yml`) keeps every job within
two minutes. Anything heavier runs in a workflow that starts only on a schedule
or on `gh workflow run` (nightly-only). A gate that runs **only** there checks
nothing a pull request changes until the night after it lands.

That happened on 2026-11-22. The strict Ruby gate
(`schema-ruby-generated-strict-gate`: `ruby -w`, RuboCop, RBS, Steep) ran only in
`ruby-leg.yml` and `certify.yml`, and a RuboCop offense in the generated Ruby
reached main. Worse, the fleet job of `ruby-leg.yml` had been falling back to
`make ruby-toolchain-check` whenever `make -n test-ruby` failed, and it did: run
44994557579 ran the toolchain check alone. The strict gate ran nowhere.

## Every job and its tier

The owner, 2026-11-24: "please fix this properly now. I have zero patience for
tests not running when they are supposed to." Before that day every job of
ci-fast.yml, roadmap.yml, caa-self-test.yml and python-leg.yml opened its `if:`
with `startsWith(github.head_ref, 'sprint/') == false`, and cla.yml's CAA did
the same for a same-repository branch. Pull requests #4999, #5003 and #5004
merged that night with all 29 checks SKIPPED, and GitHub showed nothing red.

**No job decides whether it runs by a branch name, a ref or a label.** Every
pull request gets the same per-PR tier, whatever its head or base branch is
called; every push to main gets the main tier. The one condition about where a
pull request comes from is the fork guard,
`github.event.pull_request.head.repo.full_name == github.repository`: a fork's
code never runs on the fleet's self-hosted runners. It is keyed on the
repository, never on a branch name. What a job checks may still follow the
diff (the `plan` job's selection) and the base (`.github/actions/merge-main`).
A pull-request trigger carries no `branches:` filter.

Each per-PR group is one job, or a matrix of jobs, and every one of them
finishes within two minutes on a fleet runner (`timeout-minutes: 2`). A group
that grows past that is cut into more rows or shards. It is never given a
longer timeout, made report-only or skipped.

The tiers:

- **per-PR**: a same-repository pull request.
- **fork-PR**: a pull request from a fork.
- **main**: a push to main.
- **nightly**: the schedule.
- **dispatch**: `gh workflow run`.
- **comment**: a pull-request comment.

A workflow that runs only on a schedule or a dispatch is listed whole in the
next table. Every job of every other workflow is a row here.

`internal/ci` holds the workflows to this table in both directions:

- **`TestNoJobRunsOnABranchName`** refuses any `if:`, in a job or a step, that
  reads `github.head_ref`, `github.base_ref`, `github.ref`, `github.ref_name`, a
  pull request's `head.ref` or `base.ref`, or its labels. It also refuses a
  `branches:` filter on a pull-request trigger. It then evaluates every
  pull-request job for heads named `sprint/...`, `sprint-group/...`, `rowan/...`
  and `feature`, for bases main, `sprint/...` and dev, and with and without a
  label, and fails on any job whose answer changes.
- **`TestEveryJobHasItsTierInCITiers`** fails for:
  - a job with no row;
  - a row naming no job;
  - a row whose tiers differ from the tiers the workflow gives the job, as
    evaluated from its triggers and `if:` with every plan output set.
- **`TestEveryPerPRGroupRunsOnEveryPullRequest`** fails for a per-PR row that
  is not a ci-fast.yml job, or that a pull request from a `sprint/` branch does
  not run.

| Job | Workflow | Tiers | Selected by |
|---|---|---|---|
| `fork-pull-request` | `ci-fast.yml` | fork-PR | always red: a fork's pull request runs no code here |
| `plan` | `ci-fast.yml` | per-PR, main, dispatch | always: names the diff, the packages and the legs |
| `ci-policy` | `ci-fast.yml` | per-PR, main, dispatch | always: internal/ci and the policy packages, sharded |
| `lint-fast` | `ci-fast.yml` | per-PR, main, dispatch | always |
| `vet` | `ci-fast.yml` | per-PR, main, dispatch | always |
| `schema-test` | `ci-fast.yml` | per-PR, main, dispatch | always: the registry's coverage matrix and `schema-test run --group pr`; the pr group holds the goldens (once the `pins` job) and the block zero-cost gate (once `block-gate`) |
| `golangci-lint-fast` | `ci-fast.yml` | per-PR, main, dispatch | plan: the diff's Go packages |
| `generated-current` | `ci-fast.yml` | per-PR, main, dispatch | plan: a codegen diff |
| `lang-clean` | `ci-fast.yml` | per-PR, main, dispatch | plan: the touched legs' clean gates |
| `lang-lint` | `ci-fast.yml` | per-PR, main, dispatch | plan: the touched legs' lints |
| `nightly-fast` | `ci-fast.yml` | per-PR, main, dispatch | plan: rows of `.github/nightly-fast-rows.txt` |
| `verify-leg` | `ci-fast.yml` | per-PR, main, dispatch | plan: the touched legs' verify rows |
| `verify-elixir-conformance` | `ci-fast.yml` | per-PR, main, dispatch | plan: the elixir leg |
| `verify-dart-conformance` | `ci-fast.yml` | per-PR, main, dispatch | plan: the dart leg or its corpus account |
| `verify-cpp-conformance` | `ci-fast.yml` | per-PR, main, dispatch | plan: the cpp leg; two rows, `make verify-cpp-conformance-1` (the standalone C++ test binaries) and `-2` (the harness --claims run), which together are `make verify-cpp-conformance` |
| `verify-dart-fuzz` | `ci-fast.yml` | per-PR, main, dispatch | plan: the dart leg; `_verify-dart-fuzz` whole (seed 1, both 2500-mutant wire passes, JSON and message fuzz) on the same-run Dart build verify-dart-conformance uploads |
| `input-family-dart-conformance` | `ci-fast.yml` | per-PR, main, dispatch | plan: the dart leg or its corpus account |
| `build-cpp-conformance-account` | `ci-fast.yml` | per-PR, main, dispatch | plan: the cpp corpus account |
| `input-family-cpp-conformance` | `ci-fast.yml` | per-PR, main, dispatch | plan: the cpp corpus account |
| `input-family` | `ci-fast.yml` | per-PR, main, dispatch | plan: the input-family rows |
| `stage-compiler` | `ci-fast.yml` | per-PR, main, dispatch | plan: the cpp corpus account; builds bin/schema once for build-cpp-conformance-account and checks nothing |
| `newleg-scaffold` | `ci-fast.yml` | per-PR, main, dispatch | plan: a new leg in the diff |
| `go-fixed-corpus` | `ci-fast.yml` | per-PR, main, dispatch | plan: the tables-go-fixedform row |
| `go-test-touched` | `ci-fast.yml` | per-PR, main, dispatch | plan: the touched packages' test rows |
| `verdict` | `ci-fast.yml` | per-PR, main, dispatch | always: the one required check |
| `leg-gate` | `ci-fast.yml` | dispatch | the legs' fixed form and versioning (whole, in ci-full.yml nightly) |
| `leg-gate-lobster` | `ci-fast.yml` | dispatch | as leg-gate |
| `leg-gate-rust` | `ci-fast.yml` | dispatch | as leg-gate |
| `leg-gate-squirrel` | `ci-fast.yml` | dispatch | as leg-gate |
| `leg-gate-cpp` | `ci-fast.yml` | dispatch | as leg-gate |
| `caa-self-test` | `caa-self-test.yml` | per-PR, main, dispatch | trigger paths: the CAA workflow, its script, tools/pgroup, go.mod |
| `caa` | `cla.yml` | per-PR, fork-PR, comment | pull_request_target, and a signing or `recheck` comment |
| `roadmap` | `roadmap.yml` | per-PR, main, nightly, dispatch | trigger paths: ROADMAP.md, docs/roadmap*, tools/roadmap |
| `page` | `roadmap.yml` | nightly, dispatch | always |
| `tag` | `promotion-tag.yml` | main | always |

## The rule

Every nightly-only workflow gets one decision, recorded in the table below.

| Decision | What it means |
|---|---|
| fast | The gate has a **pull-request form**: rows of [`.github/nightly-fast-rows.txt`](../.github/nightly-fast-rows.txt). The `nightly-fast` job of ci-fast.yml runs a row when the diff touches the row's leg, a path it names, or the broad rule. Each row fits two minutes. The nightly workflow still runs the whole gate and its planted controls. |
| covered | Named ci-fast.yml jobs (or rows) already run the same check on the pull-request tier. The notes say what, if anything, only the night still sees. |
| held | A fast form exists but is kept off the pull-request tier because the gate is red on main. The row names the red run; it joins the PR tier when main is green. |
| not-a-gate | A release, a tag, or a reusable workflow, not a check. |

`internal/ci` enforces it:

- **`TestEveryNightlyOnlyGateHasAPRTierForm`** fails by name for:
  - a nightly-only workflow with no row here;
  - a row naming a job or registry row that does not exist;
  - a registry row that stands for no workflow;
  - a held row that names no red run.
- **`TestNightlyFastRowsNameDefinedTargets`** fails for a registry row whose make target no rule defines.
- `internal/nightlyrows` parses the registry and picks the rows. A malformed line fails the plan by line number.

Adding a nightly-only workflow means adding its row here in the same change.

## The table

Nightly times are the newest completed scheduled run of each workflow on the fleet (2026-11-22), job wall time including setup.

| Workflow | Decision | Pull-request form | Notes |
|---|---|---|---|
| `schema-c-runs-hostile-corpus.yml` | fast | `hostile-c` | corpus 61 s (run 44981851584); negative control nightly |
| `schema-cpp-runs-hostile-corpus.yml` | fast | `hostile-cpp` | corpus 86 s (run 44982111522); negative control (128 s) nightly |
| `schema-cs-runs-hostile-corpus.yml` | fast | `hostile-cs` | corpus 34 s (run 44982338085) |
| `schema-dart-runs-hostile-corpus.yml` | fast | `hostile-dart` | corpus 40 s (run 44981703537) |
| `schema-elixir-runs-hostile-corpus.yml` | fast | `hostile-elixir` | corpus 44 s (run 44981220951) |
| `schema-java-runs-hostile-corpus.yml` | fast | `hostile-java` | corpus 44 s (run 44981946729) |
| `schema-js-runs-hostile-corpus.yml` | fast | `hostile-js` | corpus 13 s (run 44982312636) |
| `schema-lua-runs-hostile-corpus.yml` | fast | `hostile-lua` | 22 s with the planted control (run 44981181866) |
| `schema-luau-runs-hostile-corpus.yml` | fast | `hostile-luau` | corpus 21 s (run 44981201086) |
| `schema-moo-runs-hostile-corpus.yml` | fast | `hostile-moo` | the workflow has no completed run at all; the gate is 1.2 s on amd-2 |
| `schema-python-runs-hostile-corpus.yml` | fast | `hostile-python` | 20 s (run 44980609046) |
| `schema-ruby-runs-hostile-corpus.yml` | fast | `hostile-ruby` | 38 s with its control (run 44980926937) |
| `schema-rust-runs-hostile-corpus.yml` | fast | `hostile-rust` | debug profile 32 s (run 44982389122); release profile and control nightly |
| `schema-squirrel-runs-hostile-corpus.yml` | fast | `hostile-squirrel` | 24 s (run 45004138819) |
| `schema-unity-runs-hostile-corpus.yml` | fast | `hostile-unity` | corpus 25 s (run 44982450182) |
| `schema-shared-hostile-corpus.yml` | covered | `ci-policy` | its tests (drift and control) are not slow-gated, so ci-policy runs them in internal/ci on every pull request |
| `schema-lua-resource-bounds.yml` | fast | `bounds-lua` | 9 + 10 s (run 44980779456) |
| `schema-luau-resource-bounds.yml` | fast | `bounds-luau` | 15 s (run 44983802065) |
| `schema-python-resource-bounds.yml` | fast | `bounds-python` | 25 s (run 44980674960) |
| `schema-ruby-resource-bounds.yml` | fast | `bounds-ruby` | 19 s (run 44981451380) |
| `schema-squirrel-resource-bounds.yml` | fast | `bounds-squirrel` | fast half 9 s; whole campaign 14 s nightly (run 44988950875) |
| `ruby-leg.yml` | fast | `strict-ruby`, `strict-ruby-2`, `strict-ruby-3`, `strict-ruby-4`, `strict-ruby-5`, `strict-ruby-6` (the six shards of the strict gate), `hostile-ruby`, `bounds-ruby` | the strict gate's fast form, below; `verify-leg` (ruby) runs the leg's conformance, bench and fuzz rows. test-ruby itself now runs nightly without the silent fallback |
| `certify.yml` | covered | `go-test-touched`, `verify-leg`, `lang-lint`, `lang-clean`, `schema-test`, `generated-current`, `nightly-fast` | the release certificate: the whole suite at one SHA, Linux and macOS. Only the night sees macOS and the full matrix at once |
| `schema-dart-generated-strict-analyze-gate.yml` | fast | `strict-dart-all` | `lang-lint` (dart) runs the packet trees (11 s); the whole-corpus `-all` half is 69 s (run 44998125720) |
| `schema-go-generated-golangci-lint-gate.yml` | covered | `lang-lint` | the packet-corpus ratchet runs per Go diff; the table-corpus `-nightly` half (95 s job, run 44980872816) is too close to the cap and stays nightly |
| `schema-lua-generated-luacheck-gate.yml` | covered | `lang-lint` | lang-lint (lua) runs the same four targets |
| `schema-luau-generated-analyze-strict-gate.yml` | fast | `strict-luau` | 21 s with its control (run 44979785127) |
| `schema-moo-generated-analyze-strict-gate.yml` | fast | `strict-moo` | 23 s (run 44981804000); the pin control stays nightly |
| `schema-lobster-generated-strict-gate.yml` | fast | `strict-lobster` | 19 s (run 44999452012) |
| `schema-dart-nightly-fuzz.yml` | fast | `replay-dart`, `verify-dart-fuzz` | replay 16 s (run 44980533495); the 24M-case campaign (127 s) stays nightly; verify-dart-fuzz runs the seeded smoke |
| `schema-lua-nightly-fuzz.yml` | fast | `replay-lua`, `verify-leg` | the 600 s campaign stays nightly |
| `schema-luau-nightly-fuzz.yml` | fast | `replay-luau`, `verify-leg` | the 600 s campaign stays nightly |
| `schema-moo-nightly-fuzz.yml` | covered | `verify-leg`, `ci-policy` | verify-leg (moo, fuzz) runs the seeded wire, JSON and message fuzz; ci-policy runs the wiring test; the 100-round campaign (195 s) stays nightly |
| `schema-python-nightly-fuzz.yml` | covered | `verify-leg` | verify-leg (python, fuzz) runs the seeded wire fuzz on both arms; the 500k-case matrix (130-430 s a row) stays nightly |
| `schema-ruby-nightly-fuzz.yml` | covered | `verify-leg` | as python: 170-410 s a row nightly |
| `schema-squirrel-nightly-fuzz.yml` | covered | `verify-leg`, `ci-policy` | ci-policy runs the harness tests; the 128 s campaign stays nightly |
| `schema-squirrel-all-port-differential.yml` | fast | `differential-squirrel` | the comparison and its planted control, 8 s; the whole differential (721 s) stays nightly |
| `schema-lua-version-differential.yml` | held | `make schema-lua-version-differential` (32 s) | RED ON MAIN: run 44994938828, Lua 5.3 fails beyond the documented refusal. The static half runs in `ci-policy`. Add the row when the nightly is green |
| `schema-engine-is-the-oracle.yml` | covered | `ci-policy` | TestSchemaEngineIsTheOracle* are not slow-gated, so ci-policy runs them on every pull request |
| `schema-name-buildability.yml` | covered | `go-test-touched`, `verify-leg` | `go-test-touched` runs `internal/check` (`TestNameBuildabilityCorpus`, holding every probe row's checker verdict) and per-leg codegen tests; building and loading all 18 targets across every accepted probe row (`make name-buildability`) stays nightly |
| `lobster-fixed-proof-qualification.yml` | fast | `lobster-fixed-proof` | the four fixed-proof gates against the live C++ corpus: 63 s nightly (run 44994152761), 50 s cold on amd-2 |
| `schema-test-nightly.yml` | covered | `schema-test` | the one test driver (docs/TEST-REGISTRY.md). The `schema-test` job runs the registry's `pr` group (unit and functional, every language whose adapter has landed, every language-free test) on every pull request and prints the coverage matrix. Only the night runs the `nightly` group: every tier in every language, where each instance of a language with no adapter is MISSING and red by design, the work list |
| `tla-nightly.yml` | covered | `ci-policy` | TestTLARunsHashEveryModelAndConfig: a model, config or EXTENDed module that changes without its TLC run recorded again is red on the pull request; TLC itself (148 s) runs nightly |
| `nightly-conformance.yml` | covered | `verify-leg` | verify-leg runs each touched leg's conformance with --claims. Only the night sees a diff that touches more than four legs or the shared roots (verify-leg then selects no rows), and the cpp-writes job (1333 s) |
| `ci-full.yml` | covered | `go-test-touched`, `verify-leg`, `lang-clean`, `lang-lint`, `generated-current`, `schema-test`, `vet`, `golangci-lint-fast`, `lint-fast`, `nightly-fast`, `appendix-java-announcement-budgets`, `appendix-go-fixed`, `appendix-python-fixed` | ci-fast is ci-full selected by path. Only the night sees windows, msvc, big-endian, the negative-controls matrix, pins and vuln: none has a two-minute form |
| `python-leg-nightly.yml` | covered | `verify-leg`, `hostile-python`, `bounds-python` | test-python on macOS (2041 s) is nightly; the Linux rows run per Python diff |
| `python-leg.yml` | covered | `verify-leg`, `hostile-python`, `bounds-python` | the dispatch form of the above |
| `leg.yml` | not-a-gate | | reusable (workflow_call); certify.yml calls it |
| `release.yml` | not-a-gate | | cuts a release |

## The strict Ruby gate's fast form

`make schema-ruby-generated-strict-gate-fast` (rows `strict-ruby` to `strict-ruby-6`)
runs over the same 64 generated units as the nightly gate:

- each unit loads under `ruby -w` on both pins with an empty stderr;
- RuboCop (`--parallel`, its result cache on) lints every unit;
- the RuboCop and `ruby -w` planted controls run with it.

The run is cut into six parts, `RUBY_STRICT_SHARD=i/6`. The generated Ruby is
about 14 MB, and RuboCop over all of it is about 125 s of CPU on one core even
after the two fixes below (937 s before them), more than a push job's whole
budget. Part i lints
the i-th of six disjoint, size-balanced parts of the file set and loads every
sixth unit under `ruby -w`. `internal/ci` holds the rows to all six parts and
the split to a cover with no file lost or doubled
(`TestRubyStrictShardRows`, `TestRubyStrictShardSplit`), and holds the whole set
to its pins in every part (`TestRubyStrictFastGuard`: units, files, the cops
`rubocop.yml` turns off, no Exclude, NewCops on).

Two costs were removed from the generated text, not from the checks. RuboCop
walks every element of an array once for each hash in it
(`Style/HashAsLastArrayItem`), so the fixed-form data's arrays of thousands of
field hashes took 41 s on one file; those arrays are now written as arrays of at
most 32 elements joined by `+`. And RuboCop indexes a source String by
character, which is linear in the offset once the file holds any non-ASCII
character, so the one em dash in the generated header made
`Layout/SpaceAroundOperators` take 19 s on a 1 MB file (0.3 s without it); the
lint reads a copy whose comment-only lines are ASCII (`rubyLintCopy`), and a
line of code is never touched.

The lint copy is kept in the user cache under a hash of its bytes
(`rubyStrictFastLintTree`), because RuboCop's result cache is keyed by absolute
path: an unchanged part is answered from the cache (35-37 s the whole run, from
74-93 s cold, with the host at load 160-210), and a changed byte is a new
directory and a cold lint.

Every unit carries the same runtime under its own module name, so a runtime file
whose text equals one already queued, once the module name is swapped, is linted
once.

Measured on hetzner1 pinned to four cores (`taskset -c 0-3`) at a host load of
160 to 210, `go test` of the heaviest parts: part 3 cold 74 s, warm 35 s; part 6
cold 93 s, warm 37 s; the run before this change was killed at its 100 s cap.
The whole nightly gate takes 296 s.
RBS validation and Steep strict mode stay nightly.
