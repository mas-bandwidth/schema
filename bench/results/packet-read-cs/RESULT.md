# C#: the fixed read against its own packet read — both lanes FAIL

Card `cs-fixed-read-beats-its-packet-read` (attempt 4), roadmap tasks `gates/packet-read/cs/identity`
and `gates/packet-read/cs/plan`. Two quiet-window sittings of the landed read arm
(`bench/paired/nine.sh --read-arm --leg cs --receipt <receipt> --pin 7 --out <scratch>`, which runs
`schema-paired -mode plan-read -langs cs`). Both GATE verdicts are **FAIL** in both sittings, as the
driver printed them, so neither roadmap task is flipped; both stay `:unknown`.

## The C# line of the board

ns/record, each row's BEST round of 7 (BENCH-STANDARD §2.2); the GATE compares best against best, per
lane, against C#'s own packet read. plan/identity is REPORTED, never a verdict.

| sitting | identity read | plan read | packet read | GATE identity | GATE plan | plan/identity (reported) | window | control delta |
|---|---:|---:|---:|---|---|---:|---|---:|
| 1 | 2353.3 | 2145.1 | 298.0 | FAIL | FAIL | 0.91 | OK | 3.47% |
| 2 | 2293.1 | 2164.9 | 300.1 | FAIL | FAIL | 0.94 | OK | 2.80% |

Best / median / worst ns/record and spread of rates (§2.3):

| row | sitting 1 | spread | sitting 2 | spread |
|---|---|---:|---|---:|
| `bench_fixed_read` (identity lane, family table) | 2353.3 / 2387.0 / 2461.1 | 4.4% | 2293.1 / 2423.3 / 2517.7 | 9.4% |
| `bench_plan_read` (plan lane, lineage file) | 2145.1 / 2269.0 / 2374.9 | 10.2% | 2164.9 / 2254.5 / 2322.0 | 7.0% |
| `bench_mixed_read` (packet read, family gen) | 298.0 / 302.1 / 303.3 | 1.8% | 300.1 / 301.8 / 304.9 | 1.6% |

- The identity lane is 7.6x-7.9x slower than the packet read (2353.3 vs 298.0 in sitting 1, 2293.1 vs 300.1 in sitting 2).
- The plan lane is 7.2x slower than the packet read (2145.1 vs 298.0 in sitting 1, 2164.9 vs 300.1 in sitting 2).
- Every row is measured alone inside one `--read-arm` invocation per round; none is derived from a
  round trip minus a write (§2.9).

## Declared conditions

- **Host:** `b2` (fleet member amd-2), linux/amd64, AMD Ryzen 7 255 w/ Radeon 780M Graphics, pinned to
  core 7 (`taskset -c 7`, driver `-lane 7`).
- **Revision:** `2bf6bcf46fe1f5087c1b69b333cf4d58779ce204` (sprint/schema-cs tip, on origin). The board
  marks it dirty with build-regenerated files only: `generated/bench/paired/cpp/BenchTable.h`,
  `generated/bench/paired/cs/FixedTableTable.cs`.
- **Siblings:** serialize 00b0c3ba634a, serialize.c 7303fa47cada, serialize.go e3e994f991d2,
  serialize.cs bbf55d401ded (the board's `serialize` revision).
- **Runtime:** dotnet 10.0.401, `dotnet build -c Release -property:UseSharedCompilation=false; DefineConstants=BENCH_MATCHED;BENCH_PLAN AllowUnsafeBlocks=true`.
- **Corpus:** corpus_id 0930f96c8320a75d for identity, lineage and packet files (one invocation, one
  corpus); fixed 1264.3 B/record, plan (lineage) 995.4 B/record, packet 438 B/record.
- **Iterations:** 7 rounds, 4,000,000 records per sample per wire, 1 discarded warmup + 1 measured run
  per round.
- **Load (1-minute average, /proc/loadavg):** sitting 1 0.44 before, 1.00 after, driver range
  0.53–1.04; sitting 2 0.45 before, 1.14 after, driver range 0.65–1.26.

## The window and its receipt

Authority: Glenn Fiedler's live ruling to Rowan, 2026-10-16: "you can simply hold a fleet machine
and work there. go." The quiet window is a fleet machine the coordinator held for the run; the
coordinator's hold and quiet records are the operator READY/START receipt, passed verbatim as
`--receipt` and recorded by the driver in each sitting's `read.json` and `READ.md` (see board.txt).

- Sitting 1 (START 2026-10-16T20:38:34Z, END 20:44:18Z): window 1, amd-2 held (op
  hold-dm6jnle9y8i8-24125-9fe96d4c771df790~21-1) and quieted (op
  fleet-quiet-dm6jnlg2we48-24125-c946efc0a0e107bc~21-1) at 20:27:25Z. No CI job ran during it.
- Sitting 2 (START 2026-10-16T22:01:12Z, END 22:06:57Z): window 2, amd-2 re-held (op
  hold-dm6lc6q26oio-24125-80d9f0f5aee3bfa9~21-1) and re-quieted (op
  fleet-quiet-dm6lc6r3zqz4-24125-48e507d968a6c611~21-1) at 21:46:33Z; the four CI runner services
  stopped after their last job drained at 21:49:17Z.
- Between them, eight attempts at sitting 2 were refused INVALID by the driver's window monitor
  (competing go, make or dotnet processes from CI jobs, and once a tools release build), and one
  window-OK attempt at 21:49Z was discarded because its receipt still named window 1's ops. No
  ratio was taken from any of them; they are not on this page.

## Where the time goes, and how to see it

Both fixed-read lanes are 7.2–7.9x slower than C# packet read in these sittings. These
measurements do not isolate the cause; profile the read bodies, allocations and hash work
before attributing the gap.
The profile that would find it: run the identity lane under `DOTNET_JitDisasmSummary=1` and
`DOTNET_JitStdOutFile` to list the read bodies the JIT did not inline (the attribute card made the
class bodies `AggressiveInlining`; check the accessor chain the LOAD walks), then `dotnet-trace`
with the `Microsoft-DotNETCore-SampleProfiler` provider over the pinned run to see the hot frames.
Look for: a `Span<byte>` bounds check per field read (visible as `CORINFO_HELP_RNGCHKFAIL` paths in the
disassembly), boxed doubles on the plan lane's widened fields (allocations in a
`dotnet-counters` gc-heap-alloc trace), and the hash check inside the clock on the identity lane.

## Not done

- The roadmap tasks stay `:unknown`; no `make roadmap` was run, since nothing was flipped.
