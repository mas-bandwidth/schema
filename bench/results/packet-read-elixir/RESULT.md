# 2026-10-16, vision (hulk): elixir-fixed-read-beats-its-packet-read, an uncertified diagnostic

The probe for `gates/packet-read/elixir/identity` and `gates/packet-read/elixir/plan`
("elixir: identity / plan fixed read versus its own packet read"). This file is an
**uncertified diagnostic**, not a measurement under bench/BENCH-STANDARD.md. No driver
printed any number here, no GATE verdict was printed, and no sitting comes from a clean
checkpoint. Attempt 3 republished it after a cold read found that attempt 2's board
claimed a driver run that no committed code can produce.

## Verdict

**HOLD.** The card cannot be done at this base, because the instrument it needs does not
exist. The true dependency:

- **A real Elixir read arm, on its own card.** That means a runner mode and a driver mode
  of the kind the C one has on `sprint/schema-c` commit `8d028427b`. The runner side is
  `--read-arm` in `bench/tables/elixir/runner.exs` (identity lane `bench_fixed_read`, and
  the plan lane `bench_plan_read` over a lineage lock and `bench_lineage.bin`), plus the
  packet read `bench_mixed_read` timed alone. The driver side is `-mode plan-read` in
  `bench/paired` accepting `-langs elixir`, with `nine.sh --read-arm --leg elixir`.
  Both GATEs must be judged and printed by that driver, inside a window it certifies.

The roadmap cells `gates/packet-read/elixir/identity` and `gates/packet-read/elixir/plan`
stay `:unknown`. They were not flipped.

## How every number was produced

- **Elixir read rows** (`bench_fixed_read`, `bench_plan_read`, `bench_mixed_read`): these
  came from an **out-of-tree, uncommitted harness** that attempt 2's worker ran on `vision`.
  - Its script, command line and source are not in this repository and are on no GitHub
    branch. `git log --all -S bench_plan_read` (and `-S read-arm`) over
    `bench/tables/elixir` and `bench/elixir` returns nothing. Over `bench/paired` it finds
    only the Rust and C read-arm commits (`174e6950e`, `5ede1343e` on
    `sprint/schema-rust`, `8d028427b` on `sprint/schema-c`), and none of them touches Elixir.
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

- A gap that size is larger than the noise in that window, so it points at a real cost in
  the plan lane's reader.
- It is still an observation from an unknown harness. It is not a FAIL verdict, and it
  credits nothing.

The identity lane is not reported. Its best and the packet best differ by about 5%, which
is well inside the window's spread, and its producer is the same unknown harness.

**The profile that would find the plan-lane cause:** `:eprof` or `:fprof` over
`FixedRuntime.run_plan/4` and the generated `BenchFixed` reader, run through the real
read arm once it exists. Attempt 2 recorded an `:eprof` breakdown from 640 records. Its
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
  gate was wrong. The real read arm must settle this.
- Attempt 2 said each wire ran "in an isolated BEAM process via `Task.async`". That is a
  misstatement: a Task is an Erlang process inside the same VM, not a separate BEAM. The
  claim is withdrawn.

## Harness findings at the base (outside this card's PATHS, proposed only)

1. `bin/schema-paired -mode build -langs elixir` fails `mix format --check-formatted` at
   base. `generateAndBuild` (`bench/paired/main.go`) regenerates
   `generated/bench/paired/elixir/*.ex` and does not run `mix format` before `buildElixir`
   checks the formatting. `make/elixir.mk` formats right after generation. The proposed
   fix is to run `mix format` on the generated files in `generateAndBuild`. This red is
   present at base: it was confirmed by the cold read r2, and this card changes no code.
2. `bench/tables/elixir/runner.exs` and `bench/elixir/runner.exs` have no `--read-arm`
   mode and no `bench_plan_read` lane. That is the dependency named above.
