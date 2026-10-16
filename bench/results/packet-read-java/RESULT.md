# gates/packet-read/java — the identity fixed read versus Java's own packet read, both MEASURED

The receipt for `gates/packet-read/java/identity` (BENCH-STANDARD.md §1.10, "The
gate is the packet wire, not a ratio"). Both reads are timed ALONE; neither is
round-trip minus write. Every number below is copied from `board.txt` beside
this file, which carries the runners' own CSV and stderr for every round.

## Verdict

| gate | verdict | numbers (7 rounds per sitting) |
|---|---|---|
| `gates/packet-read/java/identity` | **PASS: the fixed read is FASTER** | sitting 1: identity read best **162.0** / median 163.1 ns/record vs packet read best **205.5** / median 205.9 ns/record = **1.27x** (best) / 1.26x (median). Sitting 2: 163.9 / 164.9 vs 205.7 / 205.9 = **1.25x** / 1.25x |
| `gates/packet-read/java/plan` | **NOT RUN** | no lineage corpus at BASE: `bench/paired/corpus/bench_lineage.bin` and `bench_lineage.layout` (and `bench/corpus/VOLD_bench_lineage.schema`, `bench/corpus/schema.lock`) are absent, so there is no older-layout file for `bench_plan_read` to read. No estimate is made. |

Every round of both sittings has the identity read faster: its WORST round
(167.4 ns) is faster than the packet read's BEST round (205.5 ns).

## The sittings

Two sittings back to back on the same host at the same revision, each a valid
window by the paired driver's own criteria (§2.6): the reference C++ control
legs moved under 5% between the start and the end, and no foreign build or
bench process was sampled.

| | sitting 1 | sitting 2 |
|---|---|---|
| UTC | 2026-10-16T02:41:46Z – 02:42:22Z | 02:42:22Z – 02:42:58Z |
| window | **OK** | **OK** |
| C++ control `bench_mixed` round_trip / write | -0.00% / +0.05% | +0.11% / +0.32% |
| C++ control `bench_fixed` round_trip / write | +0.43% / -0.05% | +0.00% / -0.58% |
| load (1/5/15) start → end | 0.47 0.47 0.27 → 0.82 0.56 0.31 | 0.82 0.56 0.31 → 0.97 0.63 0.34 |
| `bench_fixed_read` best / median / worst ns/record | 162.0 / 163.1 / 167.4 | 163.9 / 164.9 / 165.1 |
| spread (rates, §2.3) | 3.2% | 0.8% |
| `bench_mixed_read` best / median / worst ns/record | 205.5 / 205.9 / 206.3 | 205.7 / 205.9 / 206.2 |
| spread | 0.4% | 0.2% |
| packet / fixed (best, median) | 1.27x, 1.26x | 1.25x, 1.25x |
| GATE identity | PASS | PASS |

Two earlier trial sittings on the uncommitted tree (the same runner source)
gave 163.1 / 163.2 ns against 205.4 / 205.6 ns (1.26x both), windows OK; they
are not the receipt.

- **host**: `amd-2` (hostname `b2`), Linux x86_64, AMD Ryzen 7 255 (8 cores / 16 threads), governor `powersave`, uptime 5 days. No isolated cores on this host, so no lane: every process (both Java runners and the C++ controls) pinned with `taskset -c 6,14`, one physical core and its SMT sibling, so the JVM sees two CPUs and keeps its ordinary server-class ergonomics.
- **load, declared**: load1 0.47–0.97 across both sittings. That figure includes the bench's own single busy thread. The 5-second process samples (8 per sitting, every process at or above 1% CPU) show only this sitting's `java` and `packet-cpp`, plus one short `git-remote-http` in sitting 1. The host was shared with idle agent sessions (`opencode.bin`, `nova-swarm`), none of which was sampled at or above 1% during either sitting.
- **revisions**: schema `a3a91f406ea81620982bcc4605636ac4a7640c14` (BASE `7d41193b3` + the runner commit). serialize `93b8ea2a958ea3deeef9d4aff16101171ac49e15` (v1.16.2, the CI pin), used only by the C++ controls. The tree was dirty in `generated/` alone: `go run ./bench/paired -mode build -langs cpp,java` and `make build/java-bench/.stamp` regenerate `generated/bench/paired/{java,cpp}` and `generated/bench/java`, because the committed copies at BASE are older than the committed emitter. So the code measured is the CURRENT emitter's output. The board lists the files.
- **JDK**: OpenJDK 21.0.12.1 Temurin (build 21.0.12.1+1-LTS, 64-Bit Server VM, mixed mode, sharing). Default JVM flags, no `-ea`. Compiled with `javac --release 17 -Xlint:all -Werror`.
- **corpus**: identity read `corpus_id` `066b2822b3e5d487`, which is the table runner's goldens: `bench/paired/corpus/bench_fixed.{bin,layout}` plus the tolerant form's `bench_table.{bin,lengths,variants.bin}`, which ride the id. Packet read `corpus_id` `fd0748942dc6ae09`: `testdata/wire/bench_mixed.bin` plus `bench/corpus/variants/bench_mixed.variants.bin`. They are the same 64 logical records on each wire. The two ids differ because Java's two wires are two runners, so two invocations. They are NOT one union id the way C++'s single-binary `--read-arm` is, and the rows divide only inside this receipt's sittings.
- **iterations**: 4,000,000 records per sample on both wires (the packet wire's fixed `MIXED_ITERS`; `TableMain`'s `--read-arm` default `FIXED_READ_ITERS` is set equal to it). Each invocation (`--round K`) does 1 discarded warmup run of the full count, then 1 measured run. 7 rounds per sitting, the two wires alternating which runs first each round. Every sample is 0.65–0.83 s, above the 200 ms floor.
- **bytes per record**: fixed 1264.30 (the record plus the layout once, over 64), packet 438. §5.3 rule 2 refuses this division on bytes; the gate is per record, as the driver's board prints it.
- **checks**: both `contract` (caller-error asserts dormant without `-ea`, wire-contract validation unconditional in each reader). Unlike C++, Java's packet read is not a `checks=removed` build, so the two sides carry the same checks axis here.

## What each row times

- `bench_fixed_read` (`TableMain --read-arm`, family `table`): one `FixedTableFixed.load` of the whole reference file per 64 records, into the reused `fixedOut` destination. Inside the clock: the header and hash check, the identity plan's run, and the per-record scatter with its bounds pass. Before the clock: the same three-gate golden check as the ordinary pass. The landed count drains into the static sink, and the destination is a static the JIT cannot prove unread.
- `bench_mixed_read` (`Main --read-only`, family `gen`): one `Bench.readBenchMixed` per record into the reused `mixedDecoded`, rotating the 64 variant buffers. This is exactly the read half of the published `bench_mixed` round trip, timed alone. Before the clock: the same golden gate as the ordinary pass.
- Neither flag moves a published row. A `--quick` run before and after the change printed the same `bench_fixed` write and round_trip rows and the same `bench_mixed` write and round_trip rows, column for column except the rates: lang, bench, path, iters, bytes_per_op, runs, corpus_id, family, linkage, checks, opt and inline are identical, and so is the stderr layout including the derived read line.

## Against the superseded receipt

`bench/results/packet-read-java-identity.md` (2.12x, EPYC 9124, load 82–120)
divided two DERIVED reads, round-trip minus write, which the contract forbids.
On this host the derived numbers come out near 240 ns (fixed) and 196 ns
(packet), the opposite order. Measured alone they are 163 ns and 206 ns. The
subtraction misattributes the round trip's cost in both directions, which is
why §2.9 never lets a derived read be divided.

## Not done here

- The plan lane: NOT RUN (above). `gates/packet-read/java/plan` stays `:unknown`.
- `bench/paired/main.go -mode plan-read` and `nine.sh`'s board are not on BASE. They are the shared half of the arm and outside this card. This sitting ran the two Java runners and the C++ controls by script on the same pin, with the driver's window rules (5% controls, process sampling) applied by hand; the driver did not print this board.
- No hardware profile was needed: the lane passes.
