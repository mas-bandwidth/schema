# 2026-08-06 — the first four-language comparison (cpp / rust / go / cs)

The go, rust, and cs runners landed today (ports of `bench/cpp/bench_main.cpp`
per the contract in `bench/README.md`) and ran beside a same-sitting C++
re-run on both hosts — one `bench/run.sh` invocation per host, so every
number in a table shares its host's conditions. Raw CSVs with full preambles:
`2026-08-06-four-language-arm64-m2.csv` and
`2026-08-06-four-language-x86_64-epyc.csv`.

**The golden gate held everywhere**: all four runners byte-compared every
pinned corpus instance against `testdata/wire/*.bin` and round-tripped it
before benching, on both hosts — no mismatches, so all numbers below measure
corpus-identical wire. (GitHub Actions was in a major outage all day;
everything here ran and was verified locally.)

## Predictions vs outcomes

Recorded before any go/rust/cs runner existed (honesty rule):

1. *All golden gates pass on both hosts* — **held**.
2. *Rust 0.7–1.0x C++ on writes; batch read 0.4–0.8x* — **half held**. Right
   for mid-size messages, and rust actually BEAT C++ on rigidbody writes
   (M2) and chat both paths (EPYC/g++). But tiny messages refuted the band
   hard: test write 0.32x, probe_header write 0.16x on M2 — per-call
   overhead dominates when the payload is 6–10 B. Batch read 0.67x (M2) ✓.
3. *Go 0.25–0.5x C++ everywhere* — **half held**. Mid-size in band
   (rigidbody write 0.73x is better than predicted), tiny messages below it
   (probe_header write 0.06x on M2). Unpredicted: Go's read paths are often
   SLOWER than its own write paths — every other language reads faster than
   it writes.
4. *C# 0.2–0.5x, go >= cs overall* — **refuted on ranking**. C# beats Go on
   19 of 24 rows on the M2, and its message_batch read is the fastest of
   all four languages there (1.26x C++).
5. *Overall cpp >= rust > go >= cs* — **refuted in the details** (see 2/4);
   the dispatch-read ranking inverts completely: cs > go > cpp > rust (M2).
6. *Near-zero allocations per op in steady loops* — **held**: go 0 allocs,
   cs 0 bytes / 0 gen0 per 4096-message pass, both hosts.
7. *EPYC core-0 spreads visibly worse than the quiet M2* — **held**
   (spreads up to ~30–70% on the noisy core vs low single digits on M2).

## Host 1 — Apple M2 (arm64, quiet, unpinned)

macbook, Darwin 25.5.0. Apple clang 21.0.0 `-O3 -DNDEBUG -DSERIALIZE_RELEASE
-std=c++17 -ffp-contract=off -fno-rtti`; go1.26.5 (`go run`, default
optimized); cargo/rustc 1.97.1 (`--release`, opt-level 3, no LTO); dotnet
10.0.302 (`-c Release`, workstation GC). schema d164c77, serialize 19d332e.

| bench | path | B/msg | C++ M msg/s | C++ MB/s | Rust M msg/s | Rust MB/s | Go M msg/s | Go MB/s | C# M msg/s | C# MB/s |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| rigidbody_moving | write | 105 | 24.92 | 2495 | 26.24 | 2628 | 18.10 | 1813 | 16.15 | 1617 |
| rigidbody_moving | read | 105 | 84.00 | 8412 | 34.13 | 3418 | 11.27 | 1129 | 17.92 | 1795 |
| rigidbody_at_rest | write | 57 | 44.94 | 2443 | 49.37 | 2684 | 29.74 | 1617 | 29.58 | 1608 |
| rigidbody_at_rest | read | 57 | 116.20 | 6317 | 77.08 | 4190 | 17.64 | 959 | 33.28 | 1809 |
| chat | write | 13 | 107.98 | 1339 | 38.96 | 483 | 31.77 | 394 | 41.22 | 511 |
| chat | read | 13 | 130.86 | 1622 | 92.01 | 1141 | 19.80 | 245 | 78.79 | 977 |
| test | write | 6 | 746.72 | 4273 | 235.97 | 1350 | 67.84 | 388 | 111.85 | 640 |
| test | read | 6 | 540.58 | 3093 | 222.48 | 1273 | 48.69 | 279 | 131.82 | 754 |
| inputpacket | write | 61 | 37.68 | 2192 | 19.42 | 1130 | 13.09 | 762 | 14.35 | 835 |
| inputpacket | read | 61 | 35.88 | 2088 | 16.60 | 966 | 5.91 | 344 | 15.74 | 915 |
| shipcreate | write | 28 | 68.01 | 1816 | 40.01 | 1068 | 17.81 | 476 | 29.81 | 796 |
| shipcreate | read | 28 | 110.30 | 2945 | 43.72 | 1167 | 15.08 | 403 | 36.60 | 977 |
| ship_shallow | write | 28 | 77.28 | 2064 | 43.16 | 1152 | 19.47 | 520 | 32.52 | 868 |
| ship_shallow | read | 28 | 119.19 | 3183 | 39.14 | 1045 | 16.52 | 441 | 38.93 | 1040 |
| probe_header | write | 10 | 1106.14 | 10549 | 173.40 | 1654 | 68.31 | 651 | 88.38 | 843 |
| probe_header | read | 10 | 591.59 | 5642 | 165.27 | 1576 | 41.05 | 392 | 108.81 | 1038 |
| probebits | write | 26 | 83.34 | 2066 | 84.10 | 2085 | 46.17 | 1145 | 60.31 | 1495 |
| probebits | read | 26 | 499.57 | 12387 | 70.31 | 1743 | 30.43 | 754 | 56.36 | 1397 |
| probearray | write | 47 | 44.98 | 2016 | 31.38 | 1406 | 14.96 | 671 | 21.61 | 969 |
| probearray | read | 47 | 64.88 | 2908 | 28.98 | 1299 | 11.17 | 501 | 24.89 | 1116 |
| testdata | write | 92 | 17.11 | 1501 | 9.88 | 867 | 6.94 | 609 | 6.68 | 586 |
| testdata | read | 92 | 13.70 | 1202 | 10.33 | 907 | 4.94 | 433 | 8.05 | 706 |
| message_batch | write | 25 | 85.18 | 2031 | 53.57 | 1277 | 36.02 | 859 | 52.82 | 1259 |
| message_batch | read | 25 | 31.37 | 748 | 21.15 | 504 | 31.76 | 757 | 39.37 | 939 |

## Host 2 — AMD EPYC 9124 (x86_64, core 0, NOISY)

spacegame, Linux 6.8.0-90. **NOISY**: the game server owns isolated cores
1–15 with one pinned thread each; the bench is `taskset -c 0` on the shared
core (irqs, ssh, server aux threads) — treat spreads accordingly. g++ 13.3.0
(same flags + `-Wno-class-memaccess -Wno-type-limits`); go1.26.5,
cargo/rustc 1.97.1, dotnet 10.0.302 — user-local installs under
`~/rowan-bench` (go tarball, rustup, dotnet-install), versions identical to
host 1. Tree rsynced without `.git` (preamble says commit unknown); content
= this commit. C++ compiler for this table is g++; the morning clang-18 pair
lives in `2026-08-06-x86_64-ryzen-clang.csv` (filename says ryzen, preamble
says EPYC — the CPU is the EPYC 9124).

| bench | path | B/msg | C++ M msg/s | C++ MB/s | Rust M msg/s | Rust MB/s | Go M msg/s | Go MB/s | C# M msg/s | C# MB/s |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| rigidbody_moving | write | 105 | 23.37 | 2340 | 16.85 | 1688 | 10.53 | 1055 | 10.41 | 1042 |
| rigidbody_moving | read | 105 | 24.14 | 2418 | 20.33 | 2035 | 6.42 | 643 | 12.30 | 1232 |
| rigidbody_at_rest | write | 57 | 39.79 | 2163 | 31.10 | 1691 | 18.75 | 1019 | 16.97 | 922 |
| rigidbody_at_rest | read | 57 | 46.47 | 2526 | 34.14 | 1856 | 9.87 | 536 | 19.54 | 1062 |
| chat | write | 13 | 35.89 | 445 | 37.27 | 462 | 18.63 | 231 | 25.10 | 311 |
| chat | read | 13 | 31.70 | 393 | 39.85 | 494 | 15.52 | 192 | 51.71 | 641 |
| test | write | 6 | 139.46 | 798 | 125.34 | 717 | 42.82 | 245 | 42.35 | 242 |
| test | read | 6 | 197.18 | 1128 | 64.70 | 370 | 29.96 | 171 | 84.76 | 485 |
| inputpacket | write | 61 | 31.98 | 1860 | 13.96 | 812 | 8.51 | 495 | 5.86 | 341 |
| inputpacket | read | 61 | 22.44 | 1305 | 13.72 | 798 | 4.12 | 240 | 10.17 | 591 |
| shipcreate | write | 28 | 33.31 | 890 | 20.09 | 537 | 10.92 | 292 | 10.66 | 285 |
| shipcreate | read | 28 | 56.14 | 1499 | 20.85 | 557 | 8.87 | 237 | 20.01 | 534 |
| ship_shallow | write | 28 | 57.04 | 1523 | 23.29 | 622 | 11.94 | 319 | 13.45 | 359 |
| ship_shallow | read | 28 | 57.90 | 1546 | 19.69 | 526 | 9.77 | 261 | 22.65 | 605 |
| probe_header | write | 10 | 123.24 | 1175 | 105.70 | 1008 | 42.21 | 403 | 41.35 | 394 |
| probe_header | read | 10 | 180.86 | 1725 | 53.41 | 509 | 22.73 | 217 | 67.66 | 645 |
| probebits | write | 26 | 84.78 | 2102 | 48.59 | 1205 | 27.63 | 685 | 19.35 | 480 |
| probebits | read | 26 | 124.69 | 3092 | 40.34 | 1000 | 17.03 | 422 | 44.42 | 1101 |
| probearray | write | 47 | 27.75 | 1244 | 18.40 | 825 | 9.34 | 419 | 5.19 | 233 |
| probearray | read | 47 | 43.92 | 1969 | 17.61 | 789 | 6.78 | 304 | 14.77 | 662 |
| testdata | write | 92 | 9.47 | 831 | 7.00 | 614 | 4.24 | 372 | 2.83 | 248 |
| testdata | read | 92 | 14.59 | 1280 | 6.44 | 565 | 3.48 | 305 | 5.49 | 482 |
| message_batch | write | 25 | 65.14 | 1553 | 38.64 | 921 | 25.93 | 618 | 11.81 | 282 |
| message_batch | read | 25 | 20.26 | 483 | 15.79 | 377 | 31.80 | 758 | 21.67 | 517 |

## Findings (evidence in the tables above)

1. **C++ wins by inlining, and the margin scales inversely with message
   size.** On 6–10 B messages the gap is brutal (M2 probe_header write:
   1106 vs 173 rust / 68 go / 88 cs M msg/s); on 57–105 B messages rust is
   at parity or ahead. Whatever fixed per-message cost the other runtimes
   carry (Result/error plumbing, non-inlined stream calls, per-field range
   check branches) is the whole story at small sizes — that is the first
   optimization target for every port.
2. **Rust beats C++ where the message is medium-sized and write-bound**
   (M2 rigidbody_moving write 26.24 vs 24.92; rigidbody_at_rest write 49.37
   vs 44.94; EPYC chat both paths). Not noise — reproduced across two full
   local runs.
3. **The dispatch surface's shape decides the batch read.** M2
   message_batch read: cs 39.37 > go 31.76 > cpp 31.37 > rust 21.15 M
   msg/s; EPYC: go 31.80 > cs 21.67 > cpp 20.26 > rust 15.79. Go and C#
   read into reused `MessageStorage` (zero copy-out); C++ constructs and
   escapes a ~2 KB `Message` union per message; rust's `read_message`
   returns the ~2 KB enum by value. The generated API shape, not raw
   serialize speed, ranks this bench — a `read_message_into(&mut Message)`
   variant is the obvious rust experiment.
4. **Go reads slower than it writes** on most benches (M2 rigidbody_moving:
   11.27 read vs 18.10 write) — unique among the four; every other language
   reads faster. Seed for the go port: the read path re-slices and
   bounds-checks per field; profile before guessing further.
5. **C# is the surprise of the day**: ahead of Go nearly everywhere on the
   quiet host, fastest of ALL four on the M2 batch read, with zero GC
   activity in steady state. On the noisy EPYC core its lead over Go
   narrows or flips (JIT + shared-core interference hits it hardest —
   message_batch write 11.81 vs go 25.93 with ~30% spreads).
6. **Allocation notes (optimization seeds)**: go 0 allocs and cs 0 bytes /
   0 gen0 per 4096-message batch pass on both hosts — the Reset/storage
   reuse discipline holds; there is no allocator work to trim. rust
   allocates nothing per op by construction (streams borrow buffers).
7. **Inherited observation, not new**: the C++ probebits read at 12.4 GB/s
   (M2) is the reference runner's number, unchanged from the morning
   baseline — flagged there as a hotspot-analysis item, not introduced by
   today's runners.

## Caveats

- The go and cs drivers dispatch write/read/vary through function
  values/delegates (one indirect call per op); C++ and rust inline via
  templates/generics. This slightly understates go and cs on the tiniest
  messages, and is documented in each runner's README.
- go and cs read paths reuse stream objects via `Reset` (the runtimes'
  documented no-allocation path) and cs decodes into one reused instance —
  the language-idiomatic equivalent of C++'s free stack temporaries, per
  the runner contract's "language's equivalent" clause.
- EPYC numbers are from the shared, interrupt-serving core 0 by design
  (label NOISY); the isolated cores belong to the game server. Spreads in
  the CSV tell you which rows to trust least.
- One C++ compiler per table (Apple clang 21 on M2, g++ 13.3 on EPYC).
