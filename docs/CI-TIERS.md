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
| `ruby-leg.yml` | fast | `strict-ruby`, `hostile-ruby`, `bounds-ruby` | the strict gate's fast form, below; `verify-leg` (ruby) runs the leg's conformance, bench and fuzz rows. test-ruby itself now runs nightly without the silent fallback |
| `certify.yml` | covered | `go-test-touched`, `verify-leg`, `lang-lint`, `lang-clean`, `block-gate`, `generated-current`, `nightly-fast` | the release certificate: the whole suite at one SHA, Linux and macOS. Only the night sees macOS and the full matrix at once |
| `schema-dart-generated-strict-analyze-gate.yml` | fast | `strict-dart-all` | `lang-lint` (dart) runs the packet trees (11 s); the whole-corpus `-all` half is 69 s (run 44998125720) |
| `schema-go-generated-golangci-lint-gate.yml` | covered | `lang-lint` | the packet-corpus ratchet runs per Go diff; the table-corpus `-nightly` half (95 s job, run 44980872816) is too close to the cap and stays nightly |
| `schema-lua-generated-luacheck-gate.yml` | covered | `lang-lint` | lang-lint (lua) runs the same four targets |
| `schema-luau-generated-analyze-strict-gate.yml` | fast | `strict-luau` | 21 s with its control (run 44979785127) |
| `schema-moo-generated-analyze-strict-gate.yml` | fast | `strict-moo` | 23 s (run 44981804000); the pin control stays nightly |
| `schema-lobster-generated-strict-gate.yml` | fast | `strict-lobster` | 19 s (run 44999452012) |
| `schema-dart-nightly-fuzz.yml` | fast | `replay-dart`, `verify-leg` | replay 16 s (run 44980533495); the 24M-case campaign (127 s) stays nightly; verify-leg (dart, fuzz) runs the seeded smoke |
| `schema-lua-nightly-fuzz.yml` | fast | `replay-lua`, `verify-leg` | the 600 s campaign stays nightly |
| `schema-luau-nightly-fuzz.yml` | fast | `replay-luau`, `verify-leg` | the 600 s campaign stays nightly |
| `schema-moo-nightly-fuzz.yml` | covered | `verify-leg`, `ci-policy` | verify-leg (moo, fuzz) runs the seeded wire, JSON and message fuzz; ci-policy runs the wiring test; the 100-round campaign (195 s) stays nightly |
| `schema-python-nightly-fuzz.yml` | covered | `verify-leg` | verify-leg (python, fuzz) runs the seeded wire fuzz on both arms; the 500k-case matrix (130-430 s a row) stays nightly |
| `schema-ruby-nightly-fuzz.yml` | covered | `verify-leg` | as python: 170-410 s a row nightly |
| `schema-squirrel-nightly-fuzz.yml` | covered | `verify-leg`, `ci-policy` | ci-policy runs the harness tests; the 128 s campaign stays nightly |
| `schema-squirrel-all-port-differential.yml` | fast | `differential-squirrel` | the comparison and its planted control, 8 s; the whole differential (721 s) stays nightly |
| `schema-lua-version-differential.yml` | held | `make schema-lua-version-differential` (32 s) | RED ON MAIN: run 44994938828, Lua 5.3 fails beyond the documented refusal. The static half runs in `ci-policy`. Add the row when the nightly is green |
| `schema-engine-is-the-oracle.yml` | covered | `ci-policy` | TestSchemaEngineIsTheOracle* are not slow-gated, so ci-policy runs them on every pull request |
| `lobster-fixed-proof-qualification.yml` | fast | `lobster-fixed-proof` | the four fixed-proof gates against the live C++ corpus: 63 s nightly (run 44994152761), 50 s cold on amd-2 |
| `tla-nightly.yml` | covered | `ci-policy` | TestTLARunsHashEveryModelAndConfig: a model, config or EXTENDed module that changes without its TLC run recorded again is red on the pull request; TLC itself (148 s) runs nightly |
| `nightly-conformance.yml` | covered | `verify-leg` | verify-leg runs each touched leg's conformance with --claims. Only the night sees a diff that touches more than four legs or the shared roots (verify-leg then selects no rows), and the cpp-writes job (1333 s) |
| `ci-full.yml` | covered | `go-test-touched`, `verify-leg`, `lang-clean`, `lang-lint`, `generated-current`, `block-gate`, `vet`, `golangci-lint-fast`, `lint-fast`, `nightly-fast` | ci-fast is ci-full selected by path. Only the night sees windows, msvc, big-endian, the negative-controls matrix, pins and vuln: none has a two-minute form |
| `python-leg-nightly.yml` | covered | `verify-leg`, `hostile-python`, `bounds-python` | test-python on macOS (2041 s) is nightly; the Linux rows run per Python diff |
| `python-leg.yml` | covered | `verify-leg`, `hostile-python`, `bounds-python` | the dispatch form of the above |
| `leg.yml` | not-a-gate | | reusable (workflow_call); certify.yml calls it |
| `release.yml` | not-a-gate | | cuts a release |

## The strict Ruby gate's fast form

`make schema-ruby-generated-strict-gate-fast` (row `strict-ruby`) runs over the
same 57 generated units as the nightly gate:

- each unit loads under `ruby -w` on both pins with an empty stderr;
- **one** RuboCop process lints every unit;
- the RuboCop and `ruby -w` planted controls run with it.

Every unit carries the same runtime under its own module name, so a runtime file
whose text equals one already queued, once the module name is swapped, is linted
once.

On amd-2 with four cores it takes 40 s. The whole nightly gate takes 296 s.
RBS validation and Steep strict mode stay nightly.
