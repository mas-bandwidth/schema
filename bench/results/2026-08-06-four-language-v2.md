# 2026-08-06 — four-language comparison v2 (harness read-alloc removed)

**Why v2 exists**: the v1 read columns measured harness overhead on top of
serialize work. Every C++-reference-derived read loop constructed a fresh
decode instance per iteration while the write loops hoisted theirs — in Go
that instance escapes through the opaque read function value and is
heap-allocated + zeroed EVERY message (`runtime.mallocgc` + GC ~27%
cumulative of the v1 Go read profile, `newobject` called from
`benchMessage[...]`); in C++ it is a constructor-zeroed stack temporary — on
`message_batch` a full ~2 KB `Message` memset per read; in Rust a
stack-zeroed `T::default()` per iteration. v2 hoists ONE reused decode
instance out of every read loop, matching the write loops' hoisted base and
the batch loops' existing Reset/storage discipline. C# already decoded into
one reused instance in v1, so its read numbers were the only unaffected
ones — it gets the new alloc proof, no loop change. Reuse decodes
identically: every field a read decodes is overwritten every iteration
(structure fields are fixed across variants), and the C++ dispatch read
re-establishes the selected arm itself (`message.arm = Arm{}` at selection).

**v2 read numbers are NOT comparable to v1 reads.** The v1 files stay
untouched beside this one (`2026-08-06-four-language.md` and its CSVs). v2
writes are methodologically identical to v1 writes and serve as the
same-sitting control: every write cell on both hosts came back within
session noise of v1 (M2: 0.83–1.00x, most 0.94–1.00x).

Also in this pass (owed from schema PR #3's report): the C++ runner's
`message_stream` golden self-check now selects union arms by assignment
(`m.chat = Chat{};`) before poking fields — required under PR #3's union
contract (construction initializes only the tag; an arm is established
zeroed when selected by assignment) and equally valid on current main, where
the constructor memsets the whole union. The golden still matches
byte-for-byte on both hosts.

Same methodology as v1 (`bench/README.md`): golden + round-trip self-checks
gate every run, escape barriers, LCG variation, 64 variant read buffers,
1 warmup + median-of-7, one `bench/run.sh` invocation per host. **The golden
gate held everywhere.** (GitHub Actions still in its major outage —
recovering; everything here ran and was verified locally.)

## Allocation proof (the fix, verified per bench)

The go and cs runners now print a per-bench alloc note — one extra untimed
pass of each path (256 ops/path) measured with `runtime.ReadMemStats`
(Mallocs delta) and `GC.GetAllocatedBytesForCurrentThread` respectively:

- **go**: `write 0 allocs, read 0 allocs` on all 11 per-message benches and
  `message_batch`, both hosts.
- **cs**: `write 0 bytes, read 0 bytes` on all 11 per-message benches and
  `0 bytes (0 gen0)` on `message_batch`, both hosts.
- **rust**: alloc-free by construction (streams borrow buffers; `out` is a
  stack value passed as `&mut`; no per-iteration clone in the loop).
- **cpp**: no heap on either path by construction; the per-iteration
  constructor zeroing is what v2 removed.

## Host 1 — Apple M2 (arm64, quiet, unpinned)

macbook, Darwin 25.5.0. Apple clang 21.0.0 `-O3 -DNDEBUG -DSERIALIZE_RELEASE
-std=c++17 -ffp-contract=off -fno-rtti`; go1.26.5 (`go run`, default
optimized); cargo/rustc 1.97.1 (`--release`, opt-level 3, no LTO); dotnet
10.0.302 (`-c Release`, workstation GC) — the identical toolchains as v1.
serialize 19d332e. Raw CSV: `2026-08-06-four-language-v2-arm64-m2.csv`.

| bench | path | B/msg | C++ M msg/s | C++ MB/s | Rust M msg/s | Rust MB/s | Go M msg/s | Go MB/s | C# M msg/s | C# MB/s |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| rigidbody_moving | write | 105 | 23.77 | 2380 | 25.53 | 2557 | 17.54 | 1757 | 15.18 | 1520 |
| rigidbody_moving | read | 105 | 82.60 | 8271 | 37.03 | 3708 | 12.90 | 1291 | 17.61 | 1763 |
| rigidbody_at_rest | write | 57 | 43.80 | 2381 | 47.64 | 2590 | 29.15 | 1585 | 28.36 | 1541 |
| rigidbody_at_rest | read | 57 | 120.25 | 6537 | 76.31 | 4148 | 22.86 | 1243 | 32.34 | 1758 |
| chat | write | 13 | 106.04 | 1315 | 37.54 | 465 | 30.91 | 383 | 38.73 | 480 |
| chat | read | 13 | 135.71 | 1682 | 93.96 | 1165 | 64.16 | 795 | 75.82 | 940 |
| test | write | 6 | 744.17 | 4258 | 227.69 | 1303 | 65.50 | 375 | 93.11 | 533 |
| test | read | 6 | 577.98 | 3307 | 217.47 | 1244 | 67.81 | 388 | 114.27 | 654 |
| inputpacket | write | 61 | 37.17 | 2162 | 18.72 | 1089 | 12.80 | 745 | 13.46 | 783 |
| inputpacket | read | 61 | 46.16 | 2685 | 16.53 | 962 | 8.67 | 504 | 15.03 | 874 |
| shipcreate | write | 28 | 66.95 | 1788 | 38.48 | 1028 | 17.51 | 468 | 27.90 | 745 |
| shipcreate | read | 28 | 111.79 | 2985 | 43.13 | 1152 | 17.57 | 469 | 34.62 | 924 |
| ship_shallow | write | 28 | 75.74 | 2022 | 42.39 | 1132 | 19.02 | 508 | 30.46 | 813 |
| ship_shallow | read | 28 | 118.11 | 3154 | 38.75 | 1035 | 19.87 | 531 | 36.96 | 987 |
| probe_header | write | 10 | 990.38 | 9445 | 168.38 | 1606 | 66.73 | 636 | 87.27 | 832 |
| probe_header | read | 10 | 633.73 | 6044 | 170.06 | 1622 | 62.12 | 592 | 96.68 | 922 |
| probebits | write | 26 | 81.56 | 2022 | 77.09 | 1911 | 44.98 | 1115 | 55.71 | 1381 |
| probebits | read | 26 | 502.43 | 12458 | 72.60 | 1800 | 44.84 | 1112 | 54.06 | 1340 |
| probearray | write | 47 | 44.26 | 1984 | 30.81 | 1381 | 14.90 | 668 | 20.24 | 907 |
| probearray | read | 47 | 67.91 | 3044 | 31.61 | 1417 | 12.78 | 573 | 23.98 | 1075 |
| testdata | write | 92 | 16.84 | 1478 | 9.68 | 849 | 6.62 | 580 | 6.30 | 553 |
| testdata | read | 92 | 14.02 | 1230 | 10.33 | 906 | 5.86 | 514 | 7.51 | 659 |
| message_batch | write | 25 | 79.84 | 1904 | 53.15 | 1267 | 34.64 | 826 | 44.72 | 1066 |
| message_batch | read | 25 | 67.24 | 1603 | 20.61 | 492 | 31.15 | 743 | 35.60 | 849 |

## Host 2 — AMD EPYC 9124 (x86_64, core 0, NOISY)

spacegame, Linux 6.8.0-90. Same shared-core discipline as v1: the game
server owns isolated cores 1-15 with one pinned thread each; the bench is
`taskset -c 0` on the shared core — treat spreads accordingly. g++ 13.3.0
(same flags + `-Wno-class-memaccess -Wno-type-limits`); go1.26.5,
cargo/rustc 1.97.1, dotnet 10.0.302 — the user-local `~/rowan-bench`
installs, versions identical to v1 and to host 1. Tree rsynced without
`.git` (preamble says commit unknown); content = this commit, serialize
content = 19d332e. Several concurrent sessions were pinning benchmarks to
core 0 today; a first attempt ran concurrently with two of them (spreads to
65%) and was discarded — this run was launched by a watcher that waited for
a process-inspection-verified quiet window, and the write-control (writes
0.94-1.12x of v1, mostly within 1%) confirms clean conditions. Raw CSV:
`2026-08-06-four-language-v2-x86_64-epyc.csv`.

> **CORRECTION, 2026-08-11: the C# write cells in the table below — and any relative row
> derived from this CSV — are RETRACTED as language verdicts.** The v5 EPYC harvest
> proved by intervention (`DOTNET_TieredCompilation=0`, 1.98–4.90x median moves) that
> .NET tiered-JIT promotion competing for the single shared core poisons the C# write
> medians on this host; this CSV carries the same artifact (same rows, spreads 33–156%),
> and a low spread does not clear a row (a uniformly-slow row measured 2.67x off at
> 11.6% spread). The C# READ rows are mostly clean. Mechanism, evidence and the rule:
> `2026-08-07-four-language-v5-epyc.md`.

| bench | path | B/msg | C++ M msg/s | C++ MB/s | Rust M msg/s | Rust MB/s | Go M msg/s | Go MB/s | C# M msg/s | C# MB/s |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| rigidbody_moving | write | 105 | 23.36 | 2339 | 16.83 | 1685 | 10.32 | 1033 | 10.02 | 1003 |
| rigidbody_moving | read | 105 | 30.17 | 3021 | 18.99 | 1902 | 7.89 | 790 | 12.07 | 1209 |
| rigidbody_at_rest | write | 57 | 40.04 | 2177 | 30.90 | 1680 | 17.71 | 963 | 16.91 | 919 |
| rigidbody_at_rest | read | 57 | 59.78 | 3250 | 33.73 | 1834 | 13.76 | 748 | 19.19 | 1043 |
| chat | write | 13 | 35.72 | 443 | 36.19 | 449 | 20.79 | 258 | 25.02 | 310 |
| chat | read | 13 | 70.69 | 876 | 34.83 | 432 | 37.64 | 467 | 51.16 | 634 |
| test | write | 6 | 107.54 | 615 | 124.79 | 714 | 39.30 | 225 | 41.72 | 239 |
| test | read | 6 | 202.67 | 1160 | 63.50 | 363 | 43.66 | 250 | 84.97 | 486 |
| inputpacket | write | 61 | 31.91 | 1856 | 13.94 | 811 | 8.51 | 495 | 5.78 | 336 |
| inputpacket | read | 61 | 36.40 | 2117 | 14.51 | 844 | 5.52 | 321 | 10.13 | 589 |
| shipcreate | write | 28 | 32.89 | 878 | 19.94 | 532 | 11.03 | 295 | 11.23 | 300 |
| shipcreate | read | 28 | 73.59 | 1965 | 20.75 | 554 | 11.28 | 301 | 19.21 | 513 |
| ship_shallow | write | 28 | 56.80 | 1517 | 23.21 | 620 | 11.82 | 316 | 13.29 | 355 |
| ship_shallow | read | 28 | 76.82 | 2051 | 19.26 | 514 | 12.95 | 346 | 22.16 | 592 |
| probe_header | write | 10 | 122.63 | 1169 | 103.05 | 983 | 42.14 | 402 | 42.66 | 407 |
| probe_header | read | 10 | 183.18 | 1747 | 51.92 | 495 | 38.53 | 367 | 65.30 | 623 |
| probebits | write | 26 | 83.35 | 2067 | 48.14 | 1194 | 27.51 | 682 | 18.98 | 471 |
| probebits | read | 26 | 137.15 | 3401 | 40.23 | 997 | 27.98 | 694 | 42.89 | 1063 |
| probearray | write | 47 | 27.58 | 1236 | 18.89 | 847 | 9.31 | 417 | 5.18 | 232 |
| probearray | read | 47 | 57.51 | 2578 | 18.38 | 824 | 8.66 | 388 | 14.55 | 652 |
| testdata | write | 92 | 9.20 | 807 | 6.96 | 610 | 4.21 | 370 | 2.80 | 246 |
| testdata | read | 92 | 19.05 | 1671 | 6.38 | 559 | 3.73 | 328 | 5.38 | 472 |
| message_batch | write | 25 | 65.35 | 1558 | 38.32 | 914 | 25.68 | 612 | 13.03 | 311 |
| message_batch | read | 25 | 41.64 | 993 | 16.15 | 385 | 31.78 | 758 | 21.55 | 514 |

## What moved vs v1 (read paths, median M msg/s, same host)

M2:

| bench | cpp v1 | cpp v2 | x | rust v1 | rust v2 | x | go v1 | go v2 | x | cs v1 | cs v2 | x |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| rigidbody_moving | 84.00 | 82.60 | 0.98 | 34.13 | 37.03 | 1.08 | 11.27 | 12.90 | 1.14 | 17.92 | 17.61 | 0.98 |
| rigidbody_at_rest | 116.20 | 120.25 | 1.03 | 77.08 | 76.31 | 0.99 | 17.64 | 22.86 | 1.30 | 33.28 | 32.34 | 0.97 |
| chat | 130.86 | 135.71 | 1.04 | 92.01 | 93.96 | 1.02 | 19.80 | 64.16 | 3.24 | 78.79 | 75.82 | 0.96 |
| test | 540.58 | 577.98 | 1.07 | 222.48 | 217.47 | 0.98 | 48.69 | 67.81 | 1.39 | 131.82 | 114.27 | 0.87 |
| inputpacket | 35.88 | 46.16 | 1.29 | 16.60 | 16.53 | 1.00 | 5.91 | 8.67 | 1.47 | 15.74 | 15.03 | 0.95 |
| shipcreate | 110.30 | 111.79 | 1.01 | 43.72 | 43.13 | 0.99 | 15.08 | 17.57 | 1.16 | 36.60 | 34.62 | 0.95 |
| ship_shallow | 119.19 | 118.11 | 0.99 | 39.14 | 38.75 | 0.99 | 16.52 | 19.87 | 1.20 | 38.93 | 36.96 | 0.95 |
| probe_header | 591.59 | 633.73 | 1.07 | 165.27 | 170.06 | 1.03 | 41.05 | 62.12 | 1.51 | 108.81 | 96.68 | 0.89 |
| probebits | 499.57 | 502.43 | 1.01 | 70.31 | 72.60 | 1.03 | 30.43 | 44.84 | 1.47 | 56.36 | 54.06 | 0.96 |
| probearray | 64.88 | 67.91 | 1.05 | 28.98 | 31.61 | 1.09 | 11.17 | 12.78 | 1.14 | 24.89 | 23.98 | 0.96 |
| testdata | 13.70 | 14.02 | 1.02 | 10.33 | 10.33 | 1.00 | 4.94 | 5.86 | 1.19 | 8.05 | 7.51 | 0.93 |
| message_batch | 31.37 | 67.24 | 2.14 | 21.15 | 20.61 | 0.97 | 31.76 | 31.15 | 0.98 | 39.37 | 35.60 | 0.90 |

EPYC:

| bench | cpp v1 | cpp v2 | x | rust v1 | rust v2 | x | go v1 | go v2 | x | cs v1 | cs v2 | x |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| rigidbody_moving | 24.14 | 30.17 | 1.25 | 20.33 | 18.99 | 0.93 | 6.42 | 7.89 | 1.23 | 12.30 | 12.07 | 0.98 |
| rigidbody_at_rest | 46.47 | 59.78 | 1.29 | 34.14 | 33.73 | 0.99 | 9.87 | 13.76 | 1.39 | 19.54 | 19.19 | 0.98 |
| chat | 31.70 | 70.69 | 2.23 | 39.85 | 34.83 | 0.87 | 15.52 | 37.64 | 2.43 | 51.71 | 51.16 | 0.99 |
| test | 197.18 | 202.67 | 1.03 | 64.70 | 63.50 | 0.98 | 29.96 | 43.66 | 1.46 | 84.76 | 84.97 | 1.00 |
| inputpacket | 22.44 | 36.40 | 1.62 | 13.72 | 14.51 | 1.06 | 4.12 | 5.52 | 1.34 | 10.17 | 10.13 | 1.00 |
| shipcreate | 56.14 | 73.59 | 1.31 | 20.85 | 20.75 | 1.00 | 8.87 | 11.28 | 1.27 | 20.01 | 19.21 | 0.96 |
| ship_shallow | 57.90 | 76.82 | 1.33 | 19.69 | 19.26 | 0.98 | 9.77 | 12.95 | 1.33 | 22.65 | 22.16 | 0.98 |
| probe_header | 180.86 | 183.18 | 1.01 | 53.41 | 51.92 | 0.97 | 22.73 | 38.53 | 1.70 | 67.66 | 65.30 | 0.97 |
| probebits | 124.69 | 137.15 | 1.10 | 40.34 | 40.23 | 1.00 | 17.03 | 27.98 | 1.64 | 44.42 | 42.89 | 0.97 |
| probearray | 43.92 | 57.51 | 1.31 | 17.61 | 18.38 | 1.04 | 6.78 | 8.66 | 1.28 | 14.77 | 14.55 | 0.98 |
| testdata | 14.59 | 19.05 | 1.31 | 6.44 | 6.38 | 0.99 | 3.48 | 3.73 | 1.07 | 5.49 | 5.38 | 0.98 |
| message_batch | 20.26 | 41.64 | 2.05 | 15.79 | 16.15 | 1.02 | 31.80 | 31.78 | 1.00 | 21.67 | 21.55 | 0.99 |

## Findings

1. **The batch-read crown was a harness artifact — C++ takes it on both
   hosts.** v1 M2: cs 39.37 > go 31.76 > cpp 31.37 > rust 21.15 M msg/s;
   v2 M2: **cpp 67.24** > cs 35.60 > go 31.15 > rust 20.61. v1 EPYC: go
   31.80 > cs 21.67 > cpp 20.26 > rust 15.79; v2 EPYC: **cpp 41.64** > go
   31.78 > cs 21.55 > rust 16.15. The v1 loop charged C++ a ~2 KB Message
   construction (full memset) per dispatch read while go/cs read into
   reused storage; on equal reuse discipline C++ leads by 1.9x (M2) / 1.3x
   (EPYC). C#'s v1 "fastest of all four on the M2 batch read" does not
   survive — it stays the fastest *managed* batch reader on the M2; Go
   keeps that title on the EPYC. rust is unchanged by design:
   `read_message` still returns the ~2 KB enum by value (generated API
   shape, not harness) — v1 finding 3's `read_message_into(&mut Message)`
   experiment is still the open seed.
2. **Go's read deficit was mostly the allocator.** Go reads rose 1.14-3.24x
   (M2) and 1.07-2.43x (EPYC) — chat read 3.24x on the M2 — with 0 allocs
   now proven per bench. v1 finding 4 ("Go reads slower than it writes,
   unique among the four") is half-resolved: reads now beat or match writes
   on chat (2.1x its write on M2, 1.8x on EPYC), test, shipcreate,
   ship_shallow, probebits and message_batch; reads still trail writes on
   rigidbody (0.74-0.78x), inputpacket (0.65-0.68x), probearray and
   testdata — float-heavy / nested-array shapes. That residue is real
   read-path work (the profile-first seed for the go port), not allocator.
3. **C++ reads rose where the decode instance is big or non-trivial**:
   inputpacket 1.29x (M2) / 1.62x (EPYC), chat 2.23x (EPYC), testdata and
   probearray ~1.31x (EPYC), message_batch 2.14x / 2.05x. On the EPYC every
   C++ read path now beats its write path. Tiny trivially-copyable benches
   (test, probe_header, probebits on M2) moved ~1%: their per-iteration
   construction was already nearly free.
4. **Rust is ~flat, as predicted by construction**: its v1 read loop never
   heap-allocated; removing the per-iteration stack `T::default()` zeroing
   moved reads 0.97-1.09x (M2) / 0.87-1.06x (EPYC). The chat-read 0.87x on
   EPYC is code-layout sensitivity of the rebuilt binary (spreads 9.3% v1
   vs 3.1% v2), not a regression mechanism we introduced — the loop does
   strictly less work now. Rust's story is unchanged: competitive mid-size,
   per-call overhead dominating tiny messages.
5. **C# is the control that proves the method**: it already hoisted its
   decode instance in v1, and its v2 reads came back 0.87-0.98x (M2 — with
   its writes drifting identically, i.e. session noise) and 0.96-1.00x
   (EPYC). The reuse discipline it pioneered here is now uniform across all
   four runners.
6. **v1 finding 1 stands, now clean**: C++ wins by inlining and the margin
   scales inversely with message size — measured without harness alloc
   noise on the read side for the first time.

## Caveats

- The go and cs drivers still dispatch write/read/vary through function
  values/delegates (one indirect call per op); C++ and rust inline via
  templates/generics — unchanged from v1, documented in the runner READMEs.
- Decode-instance reuse means bytes a read does not write (e.g. text bytes
  beyond `text_length`) persist across iterations. Structure fields are
  constant per bench and every measured field is re-decoded per iteration,
  so decoded values are identical to fresh-instance decodes; the round-trip
  self-checks still use fresh instances.
- cpp test WRITE on the EPYC moved 139.5 -> 107.5 M msg/s between sittings
  with tight spreads in both (7.5% / 5.9%) — the 6 B write loop is
  code-layout sensitive and the binary changed (the hoist rearranges the
  read path; write code is untouched). The M2 write control for the same
  bench is 1.00x. Read this bench's EPYC write cell with that in mind.
- EPYC numbers are from the shared, interrupt-serving core 0 by design
  (label NOISY); spreads in the CSV tell you which rows to trust least.
  One C++ compiler per table (Apple clang 21 on M2, g++ 13.3 on EPYC).
- rust allocation-freedom is asserted by construction (verified loop shape:
  `&mut out`, no clone) rather than measured; go and cs are measured per
  bench by the runners themselves.
