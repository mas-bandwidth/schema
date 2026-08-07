# 2026-08-07 — four-language comparison v4 (the post-restrict mains)

**What v4 is**: the pass after serialize #29/#30 and schema #10 landed. v3
(`2026-08-07-four-language-v3.md`) measured the day's optimization program at
the post-merge mains; two things landed since: serialize #30
(restrict-qualified `this` on the writer — isolated pairing measured
generated C++ writes up to +152%) with #29 (the C++14 gate on the
compile-time parameter surface, no codegen effect under -std=c++17), and
schema #10 (the batch read loop adopts rust `read_message_into` — paired
2.25x). v4 is the mains, fresh-pulled, at exactly these commits — the pin is
the definition, and any PR merged after the pull is out of v4:

| repo | main @ v4 | vs v3 |
|---|---|---|
| schema | `a36e1bd08c22fe66f348f61b1fab5fcc30de0647` | +#9 (v3 endpoint docs), +#10 (rust batch `read_message_into`) |
| serialize | `d042fc8c2fe8925fda0bb25bfe520cdf648d9815` | +#29 (cxx03 const-params gate), +#30 (restrict `this` on the writer) |
| serialize.go | `18b2439aeade8774b9539cb1d7e19aa0b944fe66` | unchanged |
| serialize.rs | `422e4fa031b58d6837ca0f10df8d43a66e0d5abe` | unchanged |
| serialize.cs | `e2bda998b234142fd0dae4fe7d845bc20ea06e8e` | unchanged |

By the pin rule: the serialize.go strings PR (in flight in another session)
had not landed at pull time and is out of v4 — go chat read still carries the
`SerializeString` allocation the ledger names. The yojimbo work is likewise
out. The sibling checkouts (`../serialize-cs-port/*`) were verified at
exactly these SHAs, clean, before the run.

**Host coverage: M2 only — EPYC still deferred on Glenn's word** ("stick
with m2 for now"); the v2 EPYC tables remain the latest x86 data.

Same methodology as v1–v3 (`bench/README.md`), same harness: golden +
round-trip self-checks gate every runner, escape barriers, LCG variation, 64
variant read buffers, 1 warmup + median-of-7, one `bench/run.sh` invocation.
**The golden gate held on all four runners**; the go/cs alloc notes read 0
allocs / 0 bytes on every bench. v2, v3 and v4 are directly comparable on
both paths (identical harness).

## Apple M2 (arm64, quiet, unpinned)

macbook, Darwin 25.5.0. Apple clang 21.0.0 `-O3 -DNDEBUG -DSERIALIZE_RELEASE
-std=c++17 -ffp-contract=off -fno-rtti`; go1.26.5 (`go run`, default
optimized); cargo/rustc 1.97.1 (`--release`, opt-level 3, no LTO); dotnet
10.0.302 (`-c Release`, workstation GC) — toolchains identical to v1–v3.
Raw CSV: `2026-08-07-four-language-v4-arm64-m2.csv`.

| bench | path | B/msg | C++ M msg/s | C++ MB/s | Rust M msg/s | Rust MB/s | Go M msg/s | Go MB/s | C# M msg/s | C# MB/s |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| rigidbody_moving | write | 105 | 58.63 | 5871 | 26.09 | 2613 | 17.57 | 1760 | 15.46 | 1548 |
| rigidbody_moving | read | 105 | 88.88 | 8901 | 47.92 | 4798 | 24.08 | 2411 | 17.20 | 1723 |
| rigidbody_at_rest | write | 57 | 105.22 | 5720 | 51.44 | 2796 | 29.27 | 1591 | 28.72 | 1561 |
| rigidbody_at_rest | read | 57 | 125.05 | 6798 | 90.08 | 4897 | 40.91 | 2224 | 33.57 | 1825 |
| chat | write | 13 | 106.69 | 1323 | 66.39 | 823 | 30.92 | 383 | 39.52 | 490 |
| chat | read | 13 | 135.79 | 1683 | 91.75 | 1137 | 69.48 | 861 | 79.40 | 984 |
| test | write | 6 | 743.73 | 4256 | 228.16 | 1306 | 65.43 | 374 | 109.58 | 627 |
| test | read | 6 | 566.59 | 3242 | 218.37 | 1250 | 84.76 | 485 | 135.08 | 773 |
| inputpacket | write | 61 | 65.02 | 3782 | 39.46 | 2295 | 12.59 | 732 | 14.03 | 816 |
| inputpacket | read | 61 | 45.38 | 2640 | 19.27 | 1121 | 16.13 | 939 | 15.44 | 898 |
| shipcreate | write | 28 | 101.92 | 2722 | 65.95 | 1761 | 17.43 | 465 | 28.15 | 752 |
| shipcreate | read | 28 | 106.68 | 2849 | 53.76 | 1435 | 23.52 | 628 | 35.66 | 952 |
| ship_shallow | write | 28 | 125.37 | 3348 | 84.51 | 2257 | 19.00 | 507 | 31.65 | 845 |
| ship_shallow | read | 28 | 118.75 | 3171 | 39.06 | 1043 | 26.15 | 698 | 36.90 | 985 |
| probe_header | write | 10 | 1042.00 | 9937 | 198.59 | 1894 | 67.24 | 641 | 88.33 | 842 |
| probe_header | read | 10 | 627.61 | 5985 | 158.13 | 1508 | 77.18 | 736 | 107.77 | 1028 |
| probebits | write | 26 | 180.91 | 4486 | 97.85 | 2426 | 44.12 | 1094 | 59.05 | 1464 |
| probebits | read | 26 | 488.91 | 12123 | 128.91 | 3196 | 62.65 | 1554 | 53.97 | 1338 |
| probearray | write | 47 | 73.46 | 3293 | 39.10 | 1753 | 15.00 | 672 | 20.74 | 930 |
| probearray | read | 47 | 67.69 | 3034 | 68.12 | 3053 | 17.51 | 785 | 23.77 | 1066 |
| testdata | write | 92 | 28.08 | 2464 | 14.90 | 1308 | 6.78 | 595 | 6.52 | 572 |
| testdata | read | 92 | 25.88 | 2271 | 10.16 | 891 | 7.44 | 653 | 7.55 | 663 |
| message_batch | write | 25 | 82.54 | 1968 | 75.36 | 1797 | 34.94 | 833 | 53.24 | 1269 |
| message_batch | read | 25 | 78.50 | 1872 | 50.24 | 1198 | 37.66 | 898 | 38.43 | 916 |

## The relative table (time relative to C++ — C++ = 100%, higher is slower)

Medians across the 11 corpus benches; the mixed-dispatch batch separately.
"was" is v3 at the same harness on the same host.

| backend | write | read | batch write | batch read |
|---|---:|---:|---:|---:|
| C++ | 100% | 100% | 100% | 100% |
| Rust | **188%** (was 100%) | 236% (was 238%) | **110%** (was 121%) | **156%** (was 303%; 138% in the schema#10 pairing) |
| C# | **379%** (was 251%) | 343% (was 353%) | 155% (was 151%) | **204%** (was 172%) |
| Go | **490%** (was 305%) | 387% (was 386%) | 236% (was 239%) | **208%** (was 176%) |

Read the widened write column correctly: **no backend regressed** — every
non-C++ write row moved at most ±6% v3→v4 (go writes 1.00–1.04x, cs
0.97–1.06x, rust 0.98–1.06x). The whole movement is the denominator:
serialize #30 made the generated C++ writes 1.4–2.6x faster, and every
other language's relative write cost widened by exactly that. Same story
for the batch read column: rust/go/cs batch reads are at or above their
absolute v3 levels; C++ batch read itself rose 17% (unattributed — see
finding 4). C++ again leads every column outright; v3's "rust write parity
at the corpus median" is superseded by the C++ writer moving, not by any
rust change.

## v3 → v4: the restrict composition (finding 1's evidence)

C++ writes, v3 vs v4 vs serialize #30's own isolated pairing (schema
harness, cpp runner):

| bench | v3 | v4 | v4/v3 | #30 isolated |
|---|---:|---:|---:|---:|
| rigidbody_moving | 22.71 | 58.63 | **+158%** | +152% |
| rigidbody_at_rest | 43.26 | 105.22 | **+143%** | +128% |
| probebits | 94.61 | 180.91 | **+91%** | +86% |
| inputpacket | 36.84 | 65.02 | **+76%** | +76% |
| ship_shallow | 74.50 | 125.37 | **+68%** | +74% |
| probearray | 44.51 | 73.46 | **+65%** | +68% |
| shipcreate | 66.42 | 101.92 | **+53%** | +52% |
| testdata | 20.36 | 28.08 | **+38%** | +38% |
| chat | 104.21 | 106.69 | +2% | ~0 (predicted: byte-copy dominated) |
| test | 730.00 | 743.73 | +2% | ~0 (predicted: fully inlined) |
| probe_header | 990.53 | 1042.00 | +5% | ~0 (predicted) |
| message_batch | 81.80 | 82.54 | +1% | noise (predicted) |

## v1 → v4 progression (M msg/s — the whole day, headline rows)

| lang | bench | path | v1 | v2 | v3 | v4 | v4/v1 | what did it |
|---|---|---|---:|---:|---:|---:|---:|---|
| C++ | rigidbody_moving | write | 24.92 | 23.77 | 22.71 | 58.63 | **2.35x** | serialize #30 restrict |
| C++ | rigidbody_at_rest | write | 44.94 | 43.80 | 43.26 | 105.22 | **2.34x** | serialize #30 |
| C++ | probebits | write | 83.34 | 81.56 | 94.61 | 180.91 | **2.17x** | schema #8 const-emit, then #30 |
| C++ | testdata | read | 13.70 | 14.02 | 25.54 | 25.88 | **1.89x** | schema #7 bulk-bytes |
| C++ | testdata | write | 17.11 | 16.84 | 20.36 | 28.08 | **1.64x** | #7/#8/serialize #27, then #30 |
| C++ | message_batch | read | 31.37 | 67.24 | 66.95 | 78.50 | **2.50x** | v1→v2 harness fix; v3→v4 unattributed (finding 4) |
| Rust | probearray | read | 28.98 | 31.61 | 66.32 | 68.12 | **2.35x** | serialize.rs #19 + schema #5 |
| Rust | message_batch | read | 21.15 | 20.61 | 22.12 | 50.24 | **2.37x** | schema #10 read_message_into |
| Rust | inputpacket | write | 19.42 | 18.72 | 39.87 | 39.46 | **2.03x** | #19 + #5 |
| Rust | chat | write | 38.96 | 37.54 | 67.51 | 66.39 | **1.70x** | serialize.rs #20 + schema #6 |
| Go | chat | read | 19.80 | 64.16 | 70.15 | 69.48 | **3.51x** | harness fix (v1→v2); strings PR still pending |
| Go | inputpacket | read | 5.91 | 8.67 | 16.22 | 16.13 | **2.73x** | harness fix + serialize.go #17 |
| Go | rigidbody_at_rest | read | 17.64 | 22.86 | 41.06 | 40.91 | **2.32x** | harness fix + #17 |
| C# | test | write | 111.85 | 93.11 | 109.91 | 109.58 | 0.98x | serialize.cs #2 recovered the v2 dip — the honest C# day: absolutes ended near v1, relatives widened as C++ moved |

## Findings

1. **Serialize #30's restrict composed — the +152% survived the full
   pipeline.** Unlike serialize #27 (whose isolated chat-write +13.1%
   vanished at the composed mains — v3 finding 5), every #30 row landed
   within a few points of its isolated pairing, and the rows the mechanism
   predicted flat (chat, test, probe_header, batch write) stayed flat. The
   banked expectation "C++ writes rise sharply" is CONFIRMED, at
   +38%…+158% on the eight predicted benches.
2. **C++ retakes the write column outright.** v3's headline — rust write
   parity at the corpus median — lasted the morning: rust's write median is
   back to 188% of C++ (its absolutes unchanged; the reference moved). Rust
   still beats go/cs on every write row and keeps the closest batch write
   (110%).
3. **Schema #10 composed exactly**: rust batch read 22.12 → 50.24 (2.27x
   v3→v4; the paired run measured 2.25x, 50.10). The banked expectation
   "rust batch read holds near 138%" is **half-refuted**: the absolute
   held to within 0.3%, but the *cell* reads 156% because same-run C++
   batch read rose (next finding). The ratio moved; rust did not.
4. **C++ batch read +17% (66.95 → 78.50) with an untouched reader —
   unattributed.** #30 verified its read rows instruction-identical in
   serialize's own bench, and no schema C++ read code changed; yet batch
   read moved well outside its v3 value (v4 min 71.94 > v3 median 66.95,
   spread 10.4%). Same class as the batch-write side movement schema #10
   recorded ("byte-identical write loop rose 1.15x, layout the suspicion")
   — and v4 indeed reproduces that too: rust batch write 67.52 → 75.36
   (1.12x, code untouched). Both filed as layout-suspected, unproven;
   worth a profile only if a later pass contradicts them.
5. **Reads elsewhere: stable, as banked.** 42 of 44 non-batch read rows
   moved less than ±6% v3→v4. The exceptions: rust probebits read +11%
   (unattributed drift, near its v3 spread — v3's worst-spread row class),
   and cs shipcreate read +13% (write +6% beside it) — which *resolves*
   the v3 caveat that flagged cs shipcreate's apparent v2→v3 dip as
   spread, not regression: v4 puts both rows back at their v1 levels.
6. **Go and C# were spectators this pass** (their runtimes' pins are
   unchanged): every go/cs row is within noise of v3 except the cs
   shipcreate recovery above. Their widened write relatives are purely the
   C++ denominator — the ledger's write items (go write column, cs batch
   emitter adoption, bulk-bytes for both) are now worth more, not less.

## Caveats

- 3 of 96 rows carry within-run spreads over 20%, all C#: probebits write
  37.0%, message_batch write 26.8%, message_batch read 24.7%. In probebits
  write and batch read the median sits within ~1% of the max and the min is
  a lone outlier run (occasional desched on the unpinned M2, absorbed by
  median-of-7); cs batch write's median sits 5.5% below its max — read
  that row with a ±5% band.
- Host load averaged ~1.8 at run start (idle desktop, unpinned) — same
  conditions as v1–v3.
- EPYC absent by instruction (see preamble); the v2 EPYC tables remain the
  latest x86 data. The restrict win is Apple-clang-arm64-measured here;
  #30's own CI covered g++/upstream-clang/MSVC for correctness, not perf —
  the x86 composition waits for the EPYC leg's return.
- v4 is pinned to the SHAs above by definition: the in-flight serialize.go
  strings PR and the yojimbo work are out of v4 and wait for the next pass.
