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


**A push to main and a dispatch run every per-PR job** (the owner, 2026-11-24:
"zero patience for tests not running when they are supposed to"). On a push to
refs/heads/main, and on a dispatch whose `all` input is left at its default
true, the plan sets `all=yes` and selects every group whatever the diff; the
only job that may show SKIPPED then is `fork-pull-request`. A dispatch diffs
against main, never dev. **`TestNoFastJobSkipsOnAMainPush`** evaluates every
ci-fast.yml job for a main push with the plan's outputs empty and fails on any
job but the fork guard that does not run; **`TestFastPlanDiffsAgainstMain`**
holds the base and the dispatch input.

**A pull request may still select by path, and says what it left out.** The
plan's last step prints one line per group it does not run and the rule that
left it out, for example `input-family left out: no rule for make/rust.mk`.
Every file under `make/`, `test/`, `internal/codegen/` and `tools/` maps to a
job (`make/<lang>*.mk` selects that leg; a shared `.mk` selects everything);
**`TestPlanOmissionLogNamesEveryJob`** fails on a left-out job the log does not
name, and **`TestEveryTreePathMapsToAJob`** on a file no rule maps. Path
selection is temporary: `rowan/test-driver` replaces it with queries of the
test registry (every test named, typed and tiered), and this paragraph changes
with it.

**Groups cut on 2026-11-24 to fit the cap** (main push run 45431445496 cut
four jobs at two minutes; PR run 45437384482 two go-test-touched rows):

- `verify-leg (goose, fuzz)` (130 s, cut) is `verify-goose-fuzz`: two halves of
  the passes at 5000 cases each; the whole 60000-case gate runs nightly as
  `goose-fuzz-whole` in `leg-gates-nightly.yml`.
- `verify-leg (rust, fuzz)` (121 s, 79 s of it the driver's cargo build) builds
  no driver now: it runs the wire passes (`verify-rust-fuzz-wire`), and the JSON
  and message passes, which need the driver, run in verify-leg (rust,
  conformance) (`verify-rust-conformance-and-driver-fuzz`), which already
  builds it.
- `input-family (conformance-corpus, squirrel)` (121 s) runs as three shards
  (`squirrel 1/3` to `3/3`, round-robin over the surfaces);
  `input-family-merge` folds the shards' records and holds them to the whole
  account: nothing owed, opened or planted is dropped.
- `input-family (conformance-corpus, elixir)` (123 s, about 80 s compiling) is
  `input-family-elixir-conformance`, which reuses the elixir build `lang-clean`
  uploads.
- go-test-touched's `TestRustDriverAnswersWriteRefusal` row (105 s, a cold
  driver build) rides `verify-leg (rust, conformance)` through the `rides`
  directive of `.github/go-test-rows.txt`; the harness complement row (111 s)
  is split into `^TestShared`, `^Test(WireFuzz|Framing|Cook|Canonical)` and the
  rest.

**The fixed form + versioning gates are nightly.** The five `leg-gate` jobs
were ci-fast.yml jobs that ran on a dispatch only, so no pull request and no
push to main ever ran them, and their rows take 54 to 900 s. They moved whole
to `leg-gates-nightly.yml`; their pull-request form is held (the table below).

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
| `ci-policy` | `ci-fast.yml` | per-PR, main, dispatch | always: the policy checks that are not Go tests (slow-gate scan, bench parse, python tooling tests); the policy Go tests are `schema-test-pr` rows |
| `lint-fast` | `ci-fast.yml` | per-PR, main, dispatch | always |
| `vet` | `ci-fast.yml` | per-PR, main, dispatch | always |
| `schema-test` | `ci-fast.yml` | per-PR, main, dispatch | always: the registry's coverage matrix and `schema-test extract` |
| `schema-test-pr` | `ci-fast.yml` | per-PR, main, dispatch | always: `schema-test run --group pr --shard i/N`, the registry's language-free unit and functional tests cut by measured seconds, among them the goldens (once the `pins` job) and the block zero-cost gate (once `block-gate`) |
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
| `verify-goose-fuzz` | `ci-fast.yml` | per-PR, main, dispatch | plan: the goose leg; two rows, `make verify-goose-fuzz-part-1` and `-2`, which together run every goose fuzz pass at 5000 cases |
| `conformance-rows` | `ci-fast.yml` | per-PR, main, dispatch | plan: every verified leg with a rows directory (all of them when the row runner, make/conformance-rows.mk or a rows directory changes); one row per leg, `make conformance-rows-<leg>`; every row file runs and any failing row is red |
| `conformance-rows-elixir` | `ci-fast.yml` | per-PR, main, dispatch | plan: the elixir leg, as for conformance-rows; `make conformance-rows-elixir` on the corpus the lang-clean agreement shards compile |
| `input-family-merge` | `ci-fast.yml` | per-PR, main, dispatch | plan: a sharded corpus account (`corpus_shards`); folds the shards' records into the whole account |
| `input-family-elixir-conformance` | `ci-fast.yml` | per-PR, main, dispatch | plan: the elixir corpus account; runs on lang-clean's uploaded elixir build |
| `stage-compiler` | `ci-fast.yml` | per-PR, main, dispatch | plan: the cpp corpus account; builds bin/schema once for build-cpp-conformance-account and checks nothing |
| `newleg-scaffold` | `ci-fast.yml` | per-PR, main, dispatch | plan: a new leg in the diff |
| `go-fixed-corpus` | `ci-fast.yml` | per-PR, main, dispatch | always: the schema-test-pr shards and the tables-go-fixedform row read it |
| `produce` | `ci-fast.yml` | per-PR, main, dispatch | always: one row per producer (the go, goose and cpp conformance legs), restored from the runner's cache by the tree's identity (the merged tree and the Go toolchain), built cold on a miss inside two minutes; the schema-test-pr shards unpack them (tools/produce.sh), which refuses a tree built from any other source. The cpp row also holds every translation unit of the C++ conformance driver to a compile budget of 15 CPU seconds (`make conformance-cpp-tu-budget`, tools/compilebudget; the per-unit seconds measured on captain, 9.17 s the slowest, are recorded beside CONFORMANCE_DRIVER_TUS in the Makefile), red by name on a unit past it or a unit with no timing |
| `go-test-touched` | `ci-fast.yml` | per-PR, main, dispatch | plan: the touched packages' test rows |
| `verdict` | `ci-fast.yml` | per-PR, main, dispatch | always: the one required check |
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

The lint, fuzz, resource-bounds and differential workflows that ran one gate each are retired: their jobs are registry rows (docs/TEST-REGISTRY.md), run by `schema-test-pr` per pull request (tier functional: the lint gates, the replays, the fast resource bounds and the squirrel differential's fast half) and by `schema-test-nightly.yml` (tier nightly: the campaigns, the whole differentials and the controls that need them). `internal/ci` TestRetiredWorkflowsAreRegistryRows holds each retired workflow to its rows.

## The table

Nightly times are the newest completed scheduled run of each workflow on the fleet (2026-11-22), job wall time including setup.

| Workflow | Decision | Pull-request form | Notes |
|---|---|---|---|
| `certify.yml` | covered | `go-test-touched`, `verify-leg`, `lang-lint`, `lang-clean`, `schema-test`, `generated-current`, `nightly-fast` | the release certificate: the whole suite at one SHA, Linux and macOS. Only the night sees macOS and the full matrix at once. Its nightly-gates job runs every row of .github/nightly-gates.txt (every leg's fixed form and versioning, and the gates no workflow ran before), and its negative-controls groups gate-controls-<gate> run each shared gate's control on all 18 legs; none of these runs per pull request (heavy is nightly only, 2026-11-15) |
| `ruby-leg.yml` | fast | `strict-ruby`, `strict-ruby-2`, `strict-ruby-3`, `strict-ruby-4`, `strict-ruby-5`, `strict-ruby-6` (the six shards of the strict gate), `hostile-ruby` | the strict gate's fast form, below; `verify-leg` (ruby) runs the leg's conformance, bench and fuzz rows. test-ruby itself now runs nightly without the silent fallback |
| `schema-dart-nightly-fuzz.yml` | fast | `replay-dart`, `verify-dart-fuzz` | replay 16 s (run 44980533495); the 24M-case campaign (127 s) stays nightly; verify-dart-fuzz runs the seeded smoke |
| `schema-moo-nightly-fuzz.yml` | covered | `verify-leg`, `schema-test-pr` | verify-leg (moo, fuzz) runs the seeded wire, JSON and message fuzz; the pr group runs the wiring test; the 100-round campaign (195 s) stays nightly |
| `schema-engine-is-the-oracle.yml` | covered | `schema-test-pr` | TestSchemaEngineIsTheOracle* are registry rows, so the pr group runs them on every pull request |
| `schema-nightly.yml` | covered | `schema-test` | the whole test set through the one launcher (tools/fullrun): every registry instance of every tier and language, and every test the unmoved ledger still holds, by its own command. The `schema-test` job runs the registry's `pr` group on every pull request; the rest of the set runs only at night, and a fixer re-runs a night's reds on a bench with `fullrun retest` |
| `lobster-fixed-proof-qualification.yml` | fast | `lobster-fixed-proof` | the four fixed-proof gates against the live C++ corpus: 63 s nightly (run 44994152761), 50 s cold on amd-2 |
| `schema-test-nightly.yml` | covered | `schema-test`, `hostile-c`, `hostile-cpp`, `hostile-cs`, `hostile-dart`, `hostile-elixir`, `hostile-java`, `hostile-js`, `hostile-lua`, `hostile-luau`, `hostile-moo`, `hostile-python`, `hostile-ruby`, `hostile-rust`, `hostile-squirrel`, `hostile-unity` | the one test driver (docs/TEST-REGISTRY.md). The `schema-test` job runs the registry's `pr` group (unit and functional, every language whose adapter has landed, every language-free test) on every pull request and prints the coverage matrix. Only the night runs the `nightly` group: every tier in every language, where each instance of a language with no adapter is MISSING and red by design, the work list. The shared hostile corpus is its `hostile` family: one row per manifest case, which each language answers from one run of its corpus target (tools/schema-test/hostile.go; it replaced the per-language hostile-corpus workflows and the shared one). Each `hostile-<lang>` row is that target's pull-request form, run when a diff touches the corpus or the port's runner |
| `tla-nightly.yml` | covered | `schema-test-pr` | TestTLARunsHashEveryModelAndConfig: a model, config or EXTENDed module that changes without its TLC run recorded again is red on the pull request; TLC itself (148 s) runs nightly |
| `nightly-conformance.yml` | covered | `verify-leg` | verify-leg runs each touched leg's conformance with --claims. Only the night sees a diff that touches more than four legs or the shared roots (verify-leg then selects no rows), and the cpp-writes job (1333 s) |
| `ci-full.yml` | covered | `go-test-touched`, `verify-leg`, `lang-clean`, `lang-lint`, `generated-current`, `schema-test`, `vet`, `golangci-lint-fast`, `lint-fast`, `nightly-fast`, `appendix-java-announcement-budgets`, `appendix-go-fixed`, `appendix-python-fixed`, `cs-block-compile`, `cs-block-compile-negative-control`, `hostile-lobster` | ci-fast is ci-full selected by path. `hostile-lobster` (24 s cold on a bench) is the lobster hostile corpus's pull-request form; go's (106 s, not light) and goose's (red on main) run nightly as certify.yml nightly-gates rows. Only the night sees windows, msvc, big-endian, the negative-controls matrix, pins and vuln: none has a two-minute form |
| `python-leg-nightly.yml` | covered | `verify-leg`, `hostile-python` | test-python on macOS (2041 s) is nightly; the Linux rows run per Python diff |
| `python-leg.yml` | covered | `verify-leg`, `hostile-python` | the dispatch form of the above |
| `leg-gates-nightly.yml` | held | `verify-leg`, `input-family`, `go-fixed-corpus`, `nightly-fast` run parts of it per pull request | RED ON MAIN: run 43759152936 (dispatch on main, 2026-11-15): fifteen fixed form + versioning rows failed. The five leg-gate jobs moved here from ci-fast.yml, where they ran on a dispatch only; the whole goose fuzz gate (`goose-fuzz-whole`) runs here too. A per-leg fast form of each fixed-form harness, inside two minutes, joins ci-fast.yml when the nightly is green |
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
