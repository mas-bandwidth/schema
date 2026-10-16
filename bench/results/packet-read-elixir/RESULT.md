# 2026-10-16, vision (hulk): elixir-fixed-read-beats-its-packet-read, an uncertified diagnostic

The probe for `gates/packet-read/elixir/identity` and `gates/packet-read/elixir/plan`
("elixir: identity / plan fixed read versus its own packet read"). This file is an
**uncertified diagnostic**, not a measurement under bench/BENCH-STANDARD.md. No driver
printed any number here, no GATE verdict was printed, and no sitting comes from a clean
checkpoint. Attempt 3 republished it after a cold read found that attempt 2's board
claimed a driver run that the stamped revision, `35af2ddb9`, could not produce.

## Verdict

**HOLD.** Neither historical sitting is a certified read-arm measurement. The real Elixir
read arm subsequently landed at `41adc9c2ee415ba8f51253eda0d1ac9ba42705af` and is
an ancestor of current `sprint/schema-elixir` (`f69749efd2df9b8e22c53e152c677ec68952f16b`).
Its runner has `--read-arm` for identity, plan, and packet reads; the paired driver has
`-mode plan-read -langs elixir`, called by `nine.sh --read-arm --leg elixir`. This staged
card branch is carried from the older `8d0a2ea7` measurement commit, so it does not
contain that driver. A new run on a clean checkpoint containing `41adc9c2`, under an
allocated quiet Linux bench window, must supply the driver's own board and both printed
GATE verdicts. No such window or run was allocated for this correction.

The roadmap cells `gates/packet-read/elixir/identity` and `gates/packet-read/elixir/plan`
stay `:unknown`. They were not flipped.

## How every number was produced

- **Elixir read rows** (`bench_fixed_read`, `bench_plan_read`, `bench_mixed_read`): these
  came from an **out-of-tree, uncommitted harness** that attempt 2's worker ran on `vision`.
  - Its script, command line and source are not recorded in the repository or this
    artifact. The later committed read arm at `41adc9c2` did not exist at the worker's
    named revision `35af2ddb9`, and cannot certify these earlier rows retroactively.
  - The command was not recorded and cannot be recovered.
  - The rows are labelled path `read`, and no write or round-trip rows for these benches
    exist. So they are **not** round trip minus write by any arithmetic in this file.
    Whether the harness timed the read directly cannot be checked without its code.
- **The ns/record figures** are `1e9 / median_msgs_per_sec` of the raw rows in
  [`board.txt`](./board.txt). Attempt 3 checked this by hand. A "best" figure is the
  highest-rate round of the seven.
- **cpp control rows**: these are in the CSV shape the in-tree C++ runners write. The
  command that ran them was not recorded. They are kept only because the window verdicts
  are computed from them.
- **Window verdicts**: computed by hand by attempt 3 under BENCH-STANDARD.md §2.6, which
  takes the C++ gen control leg at the start and end and requires them to agree within 5%.
  No driver judged them.
- **ERL_FLAGS and pinning**: the worker reported `ERL_FLAGS="+S 1:1 +SDio 1"` (one
  scheduler online, one dirty-IO scheduler), `taskset -c 5`, and Elixir 1.20.4 on Erlang/OTP 29
  `[erts-17.0.5] [jit:ns]` (JIT on). No driver recorded these settings.
- **Iterations**: each round is one run of 64,000 records, over 7 rounds. That is not the
  §2 shape of one discarded warmup plus 7 runs per row, as a driver would run it.
- **Revision**: the worker named `35af2ddb9dd8ba934e331f224ecbf74305c81e9e`
  (`sprint/schema-elixir`). The rows were not produced by code at that revision, so
  checking out that revision does not reproduce them.

## Windows

| Sitting | gen control delta (write / round_trip) | Window | Ratios published |
|---|---|---|---|
| 1 (13:48:42Z to 13:50:28Z) | 2.21% / 3.20% | OK by the §2.6 arithmetic, uncertified | none as a verdict (see below) |
| 2 (13:52:31Z to 13:54:06Z) | **7.94%** / 0.60% (bitpacker read 8.09%) | **INVALID** | **none** |

Neither window was quiet:

- **Sitting 1**: load1 ran from 63.16 to 131.56 on a 32-core / 64-thread host. The table
  control (`cpp bench_fixed` write) moved 13.84%. Every Elixir row spread 67.7% to 86.9%.
- **Sitting 2**: INVALID. Attempt 2 marked it `OK` beside `control_delta_pct 8.09 (bound 5)`,
  which was wrong. Its lane rows are withheld from `board.txt` and none of its numbers are
  used here. They remain in git history at `8d0a2ea7`.

## Diagnostic observation (plan lane, sitting 1 only, not a GATE)

In sitting 1's raw rows, the best plan-lane round (`bench_plan_read`, 43421.6 ns/record) is
about 10x slower than the best packet-read round (`bench_mixed_read`, 4283.9 ns/record).

- The large apparent gap is a lead for profiling, but the harness and noisy window do
  not establish a performance verdict.
- It is not a driver-printed FAIL and credits nothing.

The identity lane is not reported. Its best and the packet best differ by about 5%, which
is well inside the window's spread, and its producer is the same unknown harness.

**The profile that would find the plan-lane cause:** `:eprof` or `:fprof` over
`FixedRuntime.run_plan/4` and the generated `BenchFixed` reader, run through the real
read arm in a valid window. Attempt 2 recorded an `:eprof` breakdown from 640 records. Its
script is not committed, so its percentages are left out. Its qualitative pointers are
kept only as places to look first:

- `live_at/2` does a linear scan of the `placed` chunk list on every guarded plan entry.
- `splice/4` sorts and builds an iolist per record.
- `Map.update!` bumps the report map once per widened field.
- In the identity lane, `FixedRuntime.clamps/4` threads a `{val, report}` tuple through
  every ranged scalar.

## Open questions

- The worker's plan-lane pre-clock gate is recorded as `widened=4096` (64 per record).
  BENCH-STANDARD.md (read-arm gate, near line 790) asks for 80 `widened` per record.
  Without the harness, attempt 3 cannot tell whether the lineage corpus differs or the
  gate was wrong. A fresh run of the landed read arm must settle this.
- Attempt 2 said each wire ran "in an isolated BEAM process via `Task.async`". That is a
  misstatement: a Task is an Erlang process inside the same VM, not a separate BEAM. The
  claim is withdrawn.

## Historical harness findings at `35af2ddb9` (outside this card's PATHS)

1. `bin/schema-paired -mode build -langs elixir` fails `mix format --check-formatted` at
   base. `generateAndBuild` (`bench/paired/main.go`) regenerates
   `generated/bench/paired/elixir/*.ex` and does not run `mix format` before `buildElixir`
   checks the formatting. `make/elixir.mk` formats right after generation. The proposed
   fix is to run `mix format` on the generated files in `generateAndBuild`. This red is
   present at the old measurement base according to cold read r2; this card changes no code.
2. At that old revision, `bench/tables/elixir/runner.exs` and `bench/elixir/runner.exs`
   had no `--read-arm` mode or `bench_plan_read` lane. Commit `41adc9c2` later added the
   Elixir read arm to the current integration branch.
