# Elixir read arm (`elixir-fixed-read-beats-its-packet-read`)

## Sittings and Numbers

Two pinned 7-round sittings (`taskset -c 5`, `ERL_FLAGS="+S 1:1 +SDio 1"`, 64,000 records per sample per wire, 1 discarded warmup + 1 measured run per round, rotating wire order per round, each wire run in an isolated BEAM process via `Task.async` so initial heap sizing and GC state are identical across wires) were recorded on `vision` (`hulk`, Linux `amd64`, AMD Ryzen Threadripper 2990WX 32-Core Processor, Elixir 1.20.4 / Erlang/OTP 29 `erts-17.0.5` `[jit:ns]`) at revision `35af2ddb9dd8ba934e331f224ecbf74305c81e9e` (`sprint/schema-elixir`). The full sittings and per-round CSVs are in [`board.txt`](./board.txt).

| Sitting | `bench_fixed_read` (identity, ns/record) | `bench_plan_read` (plan, ns/record) | `bench_mixed_read` (packet, ns/record) | `GATE identity` | `GATE plan` | `plan/identity` |
|---|---:|---:|---:|---|---|---:|
| Sitting 1 | 4520.9 (median 5334.1) | 43421.6 (median 46136.1) | 4283.9 (median 4541.7) | FAIL | FAIL | 9.60x |
| Sitting 2 | 4420.5 (median 4777.8) | 42251.1 (median 43894.3) | 3838.7 (median 4136.1) | FAIL | FAIL | 9.56x |

Both `gates/packet-read/elixir/identity` and `gates/packet-read/elixir/plan` in `docs/roadmap.sexp` remain `:unknown` because `GATE plan` (and `GATE identity`) did not pass and because the codegen and runner changes required to close the gap and wire `--read-arm` into `bench/tables/elixir/runner.exs` and `bench/elixir/runner.exs` lie outside this card's `PATHS` (`docs/roadmap.sexp,ROADMAP.md`).

## Harness and Build Findings on `sprint/schema-elixir` (`35af2ddb9`)

1. **`bench/paired/main.go` (`bin/schema-paired -mode build -langs elixir`) fails `mix format --check-formatted`**:
   - `generateAndBuild` (`bench/paired/main.go:539`) regenerates `generated/bench/paired/elixir/*.ex` via `elixirtable.Generate` / `elixir.Generate` without running `mix format` first, and then `buildElixir` (`bench/paired/main.go:714`) runs `mix format --check-formatted` which fails on `generated/bench/paired/elixir/bench_fixed.ex` and `fixed_table_fixed.ex` (whereas `make/elixir.mk:602` runs `mix format` right after `schema generate` before checking).
2. **`bench/tables/elixir/runner.exs` and `bench/elixir/runner.exs` have no `--read-arm` mode and no `bench_plan_read` lane**:
   - Both runners explicitly state `READ is DERIVED, never measured` (`bench/tables/elixir/runner.exs:281`, `bench/elixir/runner.exs:230`) and emit only `write` and `round_trip` CSV rows.
   - `bench/tables/elixir/schema.lock` is not yet staged on `sprint/schema-elixir` (staged on `sprint/schema-c-cpp` at `bench/tables/c/lineage/`).

## `:eprof` Profile of `bench_fixed_read` (Identity) and `bench_plan_read` (Plan)

Profiling 640 records under `:eprof` on `vision` shows where the time is spent in `generated/bench/paired/elixir/bench_fixed.ex` and `fixed_runtime.ex` (`internal/codegen/elixirtable/`):

### 1. Plan Lane (`bench_plan_read`, ~42,251 ns/record vs ~3,838 ns/record packet read — `9.56x` identity)
- **`FixedRuntime.live_at/2` + `List.keyfind/3` + `:lists.keyfind/3` (23.84% of calls/time)**:
  - `FixedRuntime.run_plan/4` represents the working canonical image as a linked list of `{dst, bytes}` chunks (`placed`) and calls `live_at(placed, guard)` (`Enum.find` over `placed`) on every guarded plan entry (72 plan entries per record), turning plan execution into an $O(N^2)$ linear list scan per record.
- **`FixedRuntime.step/4` (20.31% of time)**:
  - Each of the 72 plan entries per record dispatches through `Enum.reduce_while(entries, ...)` and pattern-matches the entry map `%{op: op, src: src, dst: dst, size: size, aux: aux, guard: guard, arg: arg, meta: meta, argw: argw}`.
- **`FixedRuntime.splice/4` + `:lists.keysplit_1/8` + `:lists.rkeymergel/4` + `:erlang.iolist_to_binary/1` (16.15% of time)**:
  - Each of the 72 plan entries inserts `{dst, chunk}` into `placed`, and at the end of every record `splice/4` sorts `placed` with `Enum.sort_by(placed, &elem(&1, 0))` and walks `(0, [])` building a list of sub-binaries and zero-fill binaries before calling `:erlang.iolist_to_binary/1` to materialize the 1,236-byte canonical image binary.
- **`FixedRuntime.bump/2` + `Map.update!/3` (9.63% of time)**:
  - Every widened field (64 widened fields per record = 4,096 across 64 records) allocates a new `report` map via `Map.update!(report, :widened, &(&1 + 1))`.

### 2. Identity Lane (`bench_fixed_read`, ~4,420.5 ns/record vs ~3,838.7 ns/record packet read)
- **`FixedRuntime.clamps/4` (46.06% of time, 96,000 calls / 640 records = 150 calls/record)**:
  - Even when zero fields are out of range (`clamped == 0`), every ranged scalar in `BenchMixed` (including inside the 8-element `MixedEntity` and 16-element `MixedStat` arrays) calls `FixedRuntime.clamps(raw, lo, hi, report)` across module boundaries and returns a `{val, report}` tuple.
- **`BenchFixed.mixed_stat_fixed_list/4` (16.93%) and `BenchFixed.mixed_entity_fixed_list/4` (11.14%)**:
  - Recursive list builders with `Enum.reverse(acc)` per array and binary sub-slicing (`binary_part` / `<<_::binary-size(at), ...>>`) per field instead of matching contiguous struct fields in a single binary pattern per element.
- **`FixedRuntime.text_ok?/2` (9.30%)**:
  - `String.valid?(slice)` and `:binary.match(slice, <<0>>)` called on every string field.

## Proposed Diffs (Outside `PATHS`)

1. **`internal/codegen/elixirtable/` (`fixedruntime.go`, `fixedmodule.go`, `fixedform.go`)**:
   - Emit a generated per-plan function (or pre-sorted flat tuple/compiled binary comprehension) for each locked lineage plan in `schema.lock` that constructs the 1,236-byte canonical image binary in a single `<<...>>` expression (reading source offsets via binary pattern matching and widening in-place without `placed` chunk lists, `Enum.sort_by`, `live_at` linear scans, or per-field `Map.update!` on `report`).
   - Inline range checks and accumulate `clamped` as an integer counter rather than threading `{val, report}` tuples through `FixedRuntime.clamps/4` 150 times per record.
   - Match all scalar fields of each `MixedStat` (16 B) and `MixedEntity` (52 B) element in a single binary pattern match per element.
2. **`bench/paired/main.go`, `bench/tables/elixir/runner.exs`, `bench/elixir/runner.exs`**:
   - Run `mix format` on generated Elixir files in `generateAndBuild` before `buildElixir` runs `mix format --check-formatted`.
   - Add `--read-arm` support to `bench/tables/elixir/runner.exs` (`bench_fixed_read` and `bench_plan_read` over `lineage/bench_lineage.bin`) and `bench/elixir/runner.exs` (`bench_mixed_read`), wired into `bench/paired/nine.sh`.
