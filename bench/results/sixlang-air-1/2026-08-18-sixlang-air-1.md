# sixlang-air-1 — the first six-language generated-code pass

**Era:** `sixlang-air-1` · **Date:** 2026-08-18 (NZ; measurement window
2026-08-17 12:53–13:59 UTC, 66 min wall) · **Tier:** quick, **UNCERTIFIED**
(§2.6 control window only; no A/A twins, no historical bands — those are the
certified tier's instruments)

**Host:** `macbook` — Apple M2 (MacBook Air), macOS Darwin 25.5.0, AC power.
Unpinned by construction (§7: no taskset on macOS). Load1 1.46–2.23 across the
window; two qemu VMs (~14% CPU each) resident throughout, Spotlight/cache
daemons active in the first minutes. 3 interleaved rounds × 6 legs (§2.4:
one warmup + one measured run per round), control legs fore and aft.

**Toolchains:** Apple clang 21.0.0 (cpp + c legs, `-O3`, shipped Release
flags, no LTO) · go 1.26.5 · cargo/rustc 1.97.1 (release, no LTO) · dotnet
10.0.302 (Release, workstation GC) · node 26.7.0 (`NODE_ENV=production` —
serialize.js caller-trust release mode).

**Commits measured:** schema `c6bd97f` (main, first commit with all six
generated backends — #71 merged). Every runtime pinned to its released tag,
§3.5 build-verified in every leg:

| runtime | tag | commit |
|---|---|---|
| serialize (C++) | v1.10.0 | `719e10e` |
| serialize.c | v1.4.0 | `25221cc` |
| serialize.go | v1.10.1 | `9e2942c` |
| serialize.rs | v2.1.1 | `e82eecd` |
| serialize.cs | v1.3.1 | `08caf8a` |
| serialize.js | v1.1.0 | `2a3aef0` |

**Window:** control medians 295.23 → 288.57 M msg/s, **delta 2.3%, window
OK** (≤ 5%). Golden gate emitted on every leg-round — all 20 legs loaded
their wire goldens, byte-compared every benched write, and stamped every row
with the corpus id. Zero wire drift (see the corpus note below — the ONE
gate event of the night, and it is coverage, not drift).

---

## The headline: six languages, generated code, C++ = 100

Blended geomean of per-row **time** ratios vs C++ (`100 × cpp_rate /
lang_rate`, best-of per §2.2), over the 24 rows comparable across **all
six** languages: the 11 gen benches present in every runner + message_batch,
write and read. Lower = faster. real_packet is excluded from the blend
because three runners don't emit it yet (below); rt rows and bitpacker are
excluded because they are hand-written runtime benches, not generated code
(reported at the bottom).

| language | blend (time, C++ = 100) | rows | noisy rows (>15% spread, still blended) |
|---|---:|---:|---|
| **C++** | **100.0** | 24 | message_batch/read (15.8%) |
| **C** | **99.4** | 24 | message_batch/write (20.1%) |
| **Rust** | **405.0** | 24 | — |
| **C#** | **564.2** | 24 | — |
| **Go** | **916.2** | 24 | probe_header/read (20.4%) |
| **JavaScript** | **6867.8** | 24 | — |

C and C++ are at parity on generated code — the cairn-12 result reproduced on
the sixth-language pass. Rust is ~4× the C++ time, C# ~5.6×, Go ~9.2×, and
the brand-new js backend lands at ~69× — a JIT dynamic-typed runtime playing
the same fixed-iteration game as AOT natives, measured honestly under the
same golden gates.

## real_packet — the real-world case

The §1.7 realistic snapshot (RealWorld.schema, 97 fields, 204-byte pinned
wire). **Only the C++, C, and js runners emit real_packet rows today** —
#61 added it to C/C++, #71 to js; the go/rust/cs runners still lack it (the
gap this pass surfaced — see the corpus note).

| language | write M msg/s | write (time, C++=100) | read M msg/s | read (time, C++=100) |
|---|---:|---:|---:|---:|
| C++ | 21.74 | 100.0 | 27.16 | 100.0 |
| C | 26.34 | **82.5** | 30.88 | **88.0** |
| JavaScript | 0.51 | 4252.3 | 0.61 | 4466.5 |
| Rust / Go / C# | — | — | — | — |

C beats C++ on the real-world case in both directions on the M2 — write by
17.5%, read by 12% — consistent with the space-box certified era's shape.

## The full grid — max M msg/s | time ratio (C++=100) | spread

Family gen + message_batch, 3 rounds, best-of. go/rust/cs real_packet: not
emitted.

| bench | path | C++ | C | Rust | Go | C# | JS |
|---|---|---|---|---|---|---|---|
| rigidbody_moving | write | 627.07 · 100.0 · 2.5% | 627.94 · 99.9 · 8.2% | 42.74 · 1467.1 · 0.6% | 18.65 · 3362.2 · 2.3% | 36.90 · 1699.2 · 0.7% | 3.47 · 18075.1 · 2.1% |
| rigidbody_moving | read | 474.74 · 100.0 · 3.0% | 473.75 · 100.2 · 4.4% | 43.30 · 1096.3 · 1.0% | 24.83 · 1911.6 · 0.1% | 36.98 · 1283.6 · 0.4% | 3.15 · 15066.2 · 0.9% |
| rigidbody_at_rest | write | 851.10 · 100.0 · 2.8% | 847.86 · 100.4 · 3.2% | 76.28 · 1115.7 · 0.3% | 32.49 · 2619.5 · 1.8% | 58.66 · 1450.9 · 0.9% | 5.31 · 16021.9 · 0.9% |
| rigidbody_at_rest | read | 498.55 · 100.0 · 2.8% | 498.74 · 100.0 · 2.7% | 79.05 · 630.7 · 0.1% | 42.25 · 1180.1 · 0.2% | 58.25 · 855.9 · 0.5% | 5.74 · 8692.9 · 0.7% |
| chat | write | 132.41 · 100.0 · 2.5% | 127.00 · 104.3 · 1.5% | 68.08 · 194.5 · 0.2% | 34.78 · 380.7 · 0.6% | 41.16 · 321.7 · 1.9% | 7.65 · 1731.6 · 0.4% |
| chat | read | 218.47 · 100.0 · 0.2% | 233.11 · 93.7 · 3.0% | 95.09 · 229.8 · 0.1% | 76.12 · 287.0 · 2.4% | 81.40 · 268.4 · 0.9% | 11.81 · 1849.2 · 0.7% |
| test | write | 764.07 · 100.0 · 0.4% | 768.04 · 99.5 · 0.4% | 230.69 · 331.2 · 0.6% | 109.08 · 700.5 · 0.1% | 136.23 · 560.9 · 0.3% | 11.01 · 6941.7 · 0.1% |
| test | read | 1046.97 · 100.0 · 0.9% | 1066.93 · 98.1 · 3.0% | 210.91 · 496.4 · 0.2% | 86.92 · 1204.5 · 0.2% | 160.85 · 650.9 · 0.1% | 18.26 · 5732.9 · 0.7% |
| inputpacket | write | 115.06 · 100.0 · 2.9% | 104.51 · 110.1 · 3.0% | 49.88 · 230.7 · 0.0% | 16.88 · 681.4 · 0.4% | 38.58 · 298.2 · 0.5% | 2.03 · 5678.1 · 0.6% |
| inputpacket | read | 293.04 · 100.0 · 0.7% | 249.68 · 117.4 · 3.4% | 40.75 · 719.2 · 0.1% | 16.81 · 1743.4 · 0.0% | 29.68 · 987.4 · 0.2% | 2.36 · 12417.7 · 1.6% |
| shipcreate | write | 468.20 · 100.0 · 0.3% | 461.29 · 101.5 · 0.9% | 76.29 · 613.7 · 0.1% | 32.15 · 1456.3 · 0.0% | 59.20 · 790.8 · 0.6% | 3.36 · 13924.5 · 1.3% |
| shipcreate | read | 258.05 · 100.0 · 2.6% | 258.53 · 99.8 · 2.9% | 80.94 · 318.8 · 0.1% | 22.74 · 1134.9 · 0.2% | 52.00 · 496.2 · 2.5% | 4.52 · 5703.5 · 1.0% |
| ship_shallow | write | 424.78 · 100.0 · 0.1% | 433.44 · 98.0 · 0.4% | 82.88 · 512.6 · 0.3% | 35.76 · 1187.8 · 0.7% | 61.84 · 686.9 · 0.1% | 3.24 · 13097.8 · 1.3% |
| ship_shallow | read | 290.15 · 100.0 · 3.4% | 294.21 · 98.6 · 2.4% | 76.99 · 376.9 · 0.2% | 26.61 · 1090.5 · 0.1% | 56.64 · 512.3 · 0.4% | 5.10 · 5688.2 · 0.3% |
| probe_header | write | 1083.14 · 100.0 · 1.9% | 1071.70 · 101.1 · 1.8% | 214.07 · 506.0 · 0.3% | 86.76 · 1248.4 · 0.0% | 123.72 · 875.5 · 0.3% | 5.59 · 19371.8 · 1.1% |
| probe_header | read | 1431.40 · 100.0 · 0.3% | 1430.01 · 100.1 · 1.0% | 182.64 · 783.7 · 0.3% | 79.83 · 1793.0 · 20.4% | 129.56 · 1104.8 · 3.4% | 9.54 · 14996.4 · 0.3% |
| probebits | write | 601.49 · 100.0 · 1.0% | 596.44 · 100.8 · 0.7% | 124.50 · 483.1 · 0.1% | 63.32 · 949.9 · 0.1% | 94.77 · 634.7 · 0.2% | 2.87 · 20989.4 · 1.0% |
| probebits | read | 780.79 · 100.0 · 1.0% | 776.93 · 100.5 · 0.6% | 137.14 · 569.3 · 0.1% | 64.59 · 1208.9 · 0.1% | 97.76 · 798.7 · 0.2% | 4.06 · 19214.4 · 1.1% |
| probearray | write | 85.02 · 100.0 · 2.5% | 81.25 · 104.6 · 2.6% | 54.45 · 156.1 · 0.1% | 20.58 · 413.2 · 0.1% | 45.23 · 188.0 · 0.3% | 2.18 · 3898.5 · 2.5% |
| probearray | read | 144.96 · 100.0 · 2.8% | 141.69 · 102.3 · 0.2% | 63.57 · 228.0 · 0.1% | 17.64 · 821.7 · 0.8% | 42.49 · 341.1 · 0.7% | 2.61 · 5554.6 · 0.5% |
| testdata | write | 48.55 · 100.0 · 2.9% | 37.69 · 128.8 · 1.7% | 25.39 · 191.2 · 0.4% | 9.46 · 513.1 · 0.9% | 15.07 · 322.1 · 0.4% | 1.27 · 3810.1 · 1.9% |
| testdata | read | 71.43 · 100.0 · 2.0% | 61.50 · 116.2 · 1.1% | 24.04 · 297.1 · 0.6% | 10.73 · 665.4 · 0.1% | 10.30 · 693.3 · 0.5% | 1.45 · 4941.0 · 1.4% |
| real_packet | write | 21.74 · 100.0 · 2.7% | 26.34 · 82.5 · 2.7% | — | — | — | 0.51 · 4252.3 · 0.3% |
| real_packet | read | 27.16 · 100.0 · 2.0% | 30.88 · 88.0 · 2.8% | — | — | — | 0.61 · 4466.5 · 0.7% |
| message_batch | write | 88.38 · 100.0 · 10.6% | 204.23 · 43.3 · 20.1% | 82.86 · 106.7 · 5.4% | 40.80 · 216.6 · 1.6% | 68.98 · 128.1 · 13.2% | 10.88 · 812.1 · 0.9% |
| message_batch | read | 154.81 · 100.0 · 15.8% | 152.81 · 101.3 · 8.4% | 50.19 · 308.5 · 1.6% | 38.04 · 407.0 · 0.1% | 44.56 · 347.4 · 1.7% | 9.99 · 1550.4 · 0.7% |

## rt + bitpacker — hand-written, reported separately

Family rt (§1.3) and the raw bitpacker (§1.4) measure the hand-written
runtimes, not generated code — excluded from the blend by the brief.

| bench | path | C++ | C | Rust | Go | C# | JS |
|---|---|---|---|---|---|---|---|
| bench_packet | write | 93.03 · 100.0 | 118.23 · 78.7 | 75.60 · 123.1 | 27.55 · 337.6 | 31.95 · 291.1 | 3.41 · 2725.8 |
| bench_packet | read | 137.62 · 100.0 | 140.84 · 97.7 | 93.37 · 147.4 | 34.40 · 400.1 | 34.04 · 404.3 | 4.06 · 3387.0 |
| bench_ints | write | 185.78 · 100.0 | 191.85 · 96.8 | 122.58 · 151.6 | 44.67 · 415.9 | 60.80 · 305.6 | 6.00 · 3096.5 |
| bench_ints | read | 191.53 · 100.0 | 178.87 · 107.1 | 105.65 · 181.3 | 39.77 · 481.6 | 52.27 · 366.4 | 6.66 · 2874.1 |
| bench_bits | write | 185.10 · 100.0 | 192.42 · 96.2 | 123.08 · 150.4 | 61.27 · 302.1 | 66.56 · 278.1 | 4.17 · 4434.1 |
| bench_bits | read | 241.26 · 100.0 | 225.16 · 107.2 | 114.65 · 210.4 | 53.78 · 448.6 | 60.64 · 397.9 | 7.14 · 3377.9 |
| bench_mixed | write | 152.81 · 100.0 | 159.39 · 95.9 | 101.63 · 150.4 | 43.69 · 349.8 | 47.30 · 323.1 | 3.55 · 4299.3 |
| bench_mixed | read | 181.83 · 100.0 | 174.30 · 104.3 | 92.57 · 196.4 | 38.38 · 473.8 | 42.55 · 427.4 | 5.56 · 3268.4 |
| bitpacker (K ops/s) | write | 62.2 · 100.0 | 62.3 · 99.9 | 33.8 · 184.2 | 9.7 · 641.6 | 15.6 · 398.0 | 6.2 · 1001.5 |
| bitpacker (K ops/s) | read | 95.9 · 100.0 | 59.1 · 162.4 | 32.7 · 293.6 | 11.5 · 831.3 | 15.8 · 605.3 | 4.8 · 1982.4 |

(bitpacker's unit is one whole many-field pack/unpack op, so its absolutes
are K ops/s, not M msg/s; the rt rows above are M msg/s like the main grid.)

## Honest notes

1. **The one gate event — corpus_id split, proven benign.** The aggregate
   refused to merge all six languages into one pass: cs/go/rust rows carry
   corpus_id `6b118770d85af4f6`, cpp/c/js carry `7a7c343f1a446f4c`. Cause:
   the corpus id hashes the goldens each runner LOADS (§1.6), and the
   go/rust/cs runners do not emit real_packet rows yet, so they load one
   golden fewer. Proof of no drift: recomputing FNV-1a-64 over the on-disk
   goldens reproduces `7a7c343f…` for the 16-file set and `6b118770…` for
   the same set minus `real_packet.bin` — byte-identical goldens, different
   coverage. The pass was aggregated as two internally-consistent groups
   (cpp/c/js and go/rust/cs); rows keep their own ids in the archived CSV.
   Cross-group ratios in this report are the era's own reduction and would
   be refused by the rel tool under §5.3 — labeled here, exactly once.
   **Follow-up owed:** real_packet rows for the go/rust/cs runners, after
   which one pass carries one id again.
2. **js is inline=unknown / linkage=esm by design.** A JIT leg has no AOT
   artifact for the §4.1 inline verdict, so the rel tool only ever shows js
   absolutes; every js ratio above (and every ratio in this report) is the
   aggregate/abs + own-reduction path the brief called for. JIT warmup is
   absorbed by §2.2 best-of and each round's warmup run.
3. **Quick tier means quick-tier trust.** No twins, no bands: a
   state-selective effect (§2.6.1's class) would not have been caught.
   Window delta 2.3% (OK). Noisy rows (>15% spread, 3-sample) stayed in the
   blend, named in the headline table; nothing crossed the §2.3 40%
   invalidity line (worst: 20.4%).
4. **Machine state.** Interactive M2 Air, unpinned, AC power: two resident
   qemu VMs (~14% CPU each, not killed — not mine to kill) and macOS
   daemons active early. The interleaved design spreads that noise across
   all six languages per round rather than letting it masquerade as one
   language being slow.
5. **No §3.5 refusals.** All six legs build-verified against the pinned
   released tags in every leg-round; the preamble records tag commits and
   resolved paths.
6. **One operational stumble, no data lost:** the first js round-0 attempt
   was killed at a 10-minute tool watchdog before rows flushed (js buffers
   CSV rows until exit); the leg was cleanly re-run. A mid-pass ground
   survey raced a live js leg and read its zero-byte CSV as a dead driver —
   the leg completed normally and stamped its rows.
