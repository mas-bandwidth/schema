# 2026-08-07 — four-language v5, EPYC leg (measured 2026-08-07, harvested 2026-08-11)

**The deferred x86 leg of the optimization program, run and landed.** The pass ran to
completion unattended on 2026-08-07 (legs `15:55:37Z → 15:59:30Z`, `PASS.done` stamped
`15:59:38Z`); the session that launched it ended mid-pass with the box's network flaking,
and the results sat on the box until this harvest (2026-08-11). Verified at harvest:
`PASS.done` present, **zero `FAILED-*` markers**, all nine between-leg quiet-window checks
`clean: none of ours running`, core-0 `/proc/stat` snapshots quiet. Raw log committed as
`2026-08-07-v5-epyc-pass.log` — it is the evidence that the serial gate held.

## What this pass is

The x86 composition leg, authorized by Glenn verbatim: *"You can do the 'ssh space' pass
now, as long as you gate one profile at a time it should work OK on the one available
core. That core does no work for the space game, it is reserved (core 0)."* Core 0 is
RESERVED for bench (his word); the game server owns isolated cores 1–15. Eight legs ran
STRICTLY SERIALLY (`bench/tools/epyc-pass-driver.sh`, each leg its own
`bench/run.sh --only LANG` invocation), with a ps quiet-window gate + core-0 snapshot
between legs.

Host: AMD EPYC 9124 (Zen 4), Linux 6.8.0, `taskset -c 0`, the sole general-purpose core
(irqs, ssh, systemd — the `BENCH_NOISE` label rides every preamble). Toolchains identical
to every prior table: g++ 13.3.0, clang 18.1.3, go1.26.5, cargo/rustc 1.97.1, dotnet
10.0.302.

## The pins (fresh-pulled mains, 2026-08-07 ~15:30Z)

| repo | main | note |
|---|---|---|
| schema | `914f43b96` | the v5 merge exactly — no lanes moved past v5, so this stays a **v5** pass |
| serialize | `040d28647` | #31 docs-only on top of #30 restrict |
| serialize.go | `68ce62432` | |
| serialize.rs | `422e4fa03` | |
| serialize.cs | `e2bda998b` | |

schema main vs the M2 v5 pin (`3baa6fd`) differs by results/docs only, so the code
measured here is exactly the code the M2 v5 tables measured. Suite gate was GREEN on the
mac at this pin before anything was staged. `serialize-pre-restrict` = serialize @
`561e81d` (the commit before the #30 merge), verified 0 restrict sites vs main's 3.

## Write-control (window stability): STABLE, one row flagged

`cpp-gcc-start` vs `cpp-gcc-end` (legs 1 and 8, the pass's bookends): **23 of 24 rows at
0.98–1.05x** — the window held across all eight legs. The one mover: **test write 1.13x**
(118.14 → 133.20 M msg/s, beyond both spreads 3.1%/8.4%) — the known unattributed
layout-band class (v4 finding-4 family; the v2 EPYC doc flagged this same bench's write
cell moving 1.30x between sittings). Filed, not chased. Raw:
`2026-08-07-four-language-v5-x86_64-epyc-write-control.csv`.

## THE ARTIFACT FIRST: the C# default-config write rows are not a language measurement

**The evidence.** In the main-table cs leg, ten of twelve write rows carry within-run
spreads of 67–385%, and on the worst rows the MEDIAN sits against the MIN while one or
two runs hit 2–4x higher — the exact inverse of the M2 desched class (where medians sat
against max and one outlier run sat low). inputpacket write: med 6.29 M msg/s, max 22.94.
probearray write: med 5.02, max 24.24. shipcreate write: med 9.88, max 34.43.

**The intervention (2026-08-11, labeled DIAGNOSTIC, serial, alone on core 0):** one cs
leg re-run with `DOTNET_TieredCompilation=0`, nothing else changed. Every poisoned row
snapped to the neighborhood of its former max and the spreads collapsed:

| bench | path | default M msg/s | notiered M msg/s | notiered/default | spread default% | spread notiered% |
|---|---|---:|---:|---:|---:|---:|
| rigidbody_moving | write | 11.83 | 23.48 | **1.98x** | 80.9 | 3.1 |
| rigidbody_moving | read | 13.36 | 25.39 | **1.90x** | 105.2 | 12.4 |
| rigidbody_at_rest | write | 35.03 | 35.62 | **1.02x** | 2.8 | 9.2 |
| rigidbody_at_rest | read | 39.89 | 38.66 | **0.97x** | 12.2 | 9.0 |
| chat | write | 24.97 | 24.58 | **0.98x** | 85.4 | 7.8 |
| chat | read | 54.66 | 46.87 | **0.86x** | 4.1 | 3.8 |
| test | write | 76.21 | 79.14 | **1.04x** | 72.4 | 5.0 |
| test | read | 105.13 | 96.44 | **0.92x** | 2.0 | 7.4 |
| inputpacket | write | 6.29 | 23.20 | **3.69x** | 270.6 | 4.7 |
| inputpacket | read | 20.90 | 19.38 | **0.93x** | 3.5 | 7.7 |
| shipcreate | write | 9.88 | 33.74 | **3.42x** | 255.2 | 8.3 |
| shipcreate | read | 39.15 | 34.29 | **0.88x** | 15.0 | 2.5 |
| ship_shallow | write | 11.27 | 30.85 | **2.74x** | 226.7 | 7.8 |
| ship_shallow | read | 40.18 | 37.54 | **0.93x** | 7.7 | 4.0 |
| probe_header | write | 70.15 | 71.30 | **1.02x** | 67.8 | 4.9 |
| probe_header | read | 81.84 | 83.97 | **1.03x** | 6.1 | 1.5 |
| probebits | write | 21.01 | 56.15 | **2.67x** | 11.6 | 13.4 |
| probebits | read | 68.62 | 71.65 | **1.04x** | 74.4 | 4.8 |
| probearray | write | 5.02 | 24.60 | **4.90x** | 385.3 | 15.5 |
| probearray | read | 29.42 | 26.51 | **0.90x** | 14.1 | 4.7 |
| testdata | write | 3.01 | 9.99 | **3.32x** | 236.4 | 0.9 |
| testdata | read | 8.76 | 13.97 | **1.60x** | 13.5 | 12.8 |
| message_batch | write | 24.75 | 51.27 | **2.07x** | 174.9 | 5.2 |
| message_batch | read | 20.67 | 27.90 | **1.35x** | 8.5 | 2.1 |

**The mechanism.** .NET tiered compilation promotes hot methods to optimized code on
BACKGROUND threads. Pinned to one shared core, the tier-up work competes with the bench
loop for the only core there is: runs where promotion landed early hit the fast steady
state, runs where it landed late (or not at all inside the window) measured tier-0 code.
On the unpinned multi-core M2 the promotion always completed benignly beside the loop,
which is why four M2 passes never saw this.

**Three consequences, stated plainly:**

1. **The default-config C# write column on this host is a measurement of tier-up
   contention, not of the generated code.** It is retracted as a language verdict.
2. **Low spread does NOT clear a row**: probebits write sat at 11.6% spread and still
   moved 2.67x under the intervention — all seven runs were uniformly pre-tier. Spread
   filtering cannot identify the affected rows; only the intervention could.
3. **RETROACTIVE: the v2 EPYC C# rows carried the same artifact** (same write rows,
   spreads 33–156%, `2026-08-06-four-language-v2-x86_64-epyc.csv`), so the published v2
   EPYC C# relative row (293/320/502/193) is likewise retracted as a language verdict — a
   dated correction note now sits in the v2 doc.

The diagnostic is **not comparable to the M2 tables** (different runtime config: M2 ran
default tiering, benignly) and tiering-off is not uniformly better — tier-1's
profile-guided reads are visibly lost on some rows (chat read 0.86x, test read 0.92x). It
is *stable*, which is what a single-core table needs. Raw:
`2026-08-11-cs-tiered-jit-diagnostic-x86_64-epyc.csv`.

## Absolute table (as measured — C++ = g++ 13.3; C# = default-config leg, write rows ✝)

✝ the C# write cells below (except rigidbody_at_rest and probebits — and probebits is
poisoned too, see consequence 2) are the artifact, kept as measured; steady-state C#
writes are in the diagnostic table above. Raw: `2026-08-07-four-language-v5-x86_64-epyc.csv`
(legs 1–4 concatenated, per-leg preambles kept).

| bench | path | B/msg | C++ M msg/s | C++ MB/s | Rust M msg/s | Rust MB/s | Go M msg/s | Go MB/s | C# M msg/s | C# MB/s |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| rigidbody_moving | write | 105 | 24.59 | 2462 | 19.99 | 2002 | 13.86 | 1387 | 11.83✝ | 1185 |
| rigidbody_moving | read | 105 | 30.43 | 3047 | 36.18 | 3623 | 15.77 | 1579 | 13.36✝ | 1338 |
| rigidbody_at_rest | write | 57 | 40.92 | 2224 | 32.02 | 1741 | 24.89 | 1353 | 35.03 | 1904 |
| rigidbody_at_rest | read | 57 | 59.71 | 3246 | 64.67 | 3515 | 27.70 | 1506 | 39.89 | 2168 |
| chat | write | 13 | 35.71 | 443 | 41.91 | 520 | 24.44 | 303 | 24.97✝ | 310 |
| chat | read | 13 | 75.21 | 932 | 41.74 | 518 | 47.79 | 592 | 54.66 | 678 |
| test | write | 6 | 118.14 | 676 | 132.93 | 761 | 69.48 | 398 | 76.21✝ | 436 |
| test | read | 6 | 226.01 | 1293 | 64.08 | 367 | 54.98 | 315 | 105.13 | 602 |
| inputpacket | write | 61 | 31.88 | 1855 | 28.63 | 1665 | 11.91 | 693 | 6.29✝ | 366 |
| inputpacket | read | 61 | 36.48 | 2122 | 12.29 | 715 | 11.78 | 685 | 20.90 | 1216 |
| shipcreate | write | 28 | 34.32 | 917 | 41.00 | 1095 | 19.72 | 527 | 9.88✝ | 264 |
| shipcreate | read | 28 | 74.55 | 1991 | 26.81 | 716 | 14.70 | 392 | 39.15 | 1045 |
| ship_shallow | write | 28 | 55.86 | 1492 | 50.64 | 1352 | 22.32 | 596 | 11.27✝ | 301 |
| ship_shallow | read | 28 | 77.89 | 2080 | 19.79 | 529 | 16.83 | 449 | 40.18 | 1073 |
| probe_header | write | 10 | 120.98 | 1154 | 112.50 | 1073 | 51.92 | 495 | 70.15✝ | 669 |
| probe_header | read | 10 | 180.68 | 1723 | 55.88 | 533 | 48.83 | 466 | 81.84 | 781 |
| probebits | write | 26 | 83.35 | 2067 | 55.09 | 1366 | 38.03 | 943 | 21.01✝ | 521 |
| probebits | read | 26 | 142.00 | 3521 | 51.75 | 1283 | 38.69 | 959 | 68.62✝ | 1701 |
| probearray | write | 47 | 25.70 | 1152 | 23.97 | 1075 | 12.94 | 580 | 5.02✝ | 225 |
| probearray | read | 47 | 57.29 | 2568 | 29.15 | 1307 | 11.01 | 493 | 29.42 | 1319 |
| testdata | write | 92 | 9.18 | 805 | 10.30 | 903 | 6.78 | 595 | 3.01✝ | 264 |
| testdata | read | 92 | 25.42 | 2230 | 9.70 | 851 | 6.26 | 550 | 8.76 | 768 |
| message_batch | write | 25 | 61.41 | 1464 | 59.36 | 1415 | 31.31 | 747 | 24.75✝ | 590 |
| message_batch | read | 25 | 41.96 | 1000 | 33.59 | 801 | 38.68 | 922 | 20.67 | 493 |

## THE RELATIVE TABLE — and the headline: it is TARGET-DEPENDENT

Time relative to C++ (C++ = 100%, higher is slower), medians across the 11 corpus
benches, batch separately. The M2 v5 finals sit beside for the cross-host read.

**Against g++ 13.3 (this pass's primary reference), C# from the steady-state diagnostic:**

| backend | write | M2 v5 | read | M2 v5 | batch write | M2 v5 | batch read | M2 v5 |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| C++ (g++) | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% |
| Rust | **108%** | 177% | **274%** | 204% | **103%** | 121% | **125%** | 153% |
| C# * | **137%** | 199% | **198%** | 214% | **120%** | 140% | **150%** | 175% |
| Go | **177%** | 323% | **370%** | 387% | **196%** | 204% | **108%** | 198% |

\* C# row from the labeled `TieredCompilation=0` diagnostic — the default-config row
computes to 305/195/248/203 and its write/batch-write cells are the artifact above, not
the language. (Reads barely differ between configs: 195 vs 198.)

**Against clang-18 as the C++ reference (same go/rust legs, C# steady-state):**

| backend | write | read | batch write | batch read |
|---|---:|---:|---:|---:|
| C++ (clang-18) | 100% | 100% | 100% | 100% |
| Rust | **147%** | **287%** | **102%** | **154%** |
| C# * | **156%** | **221%** | **118%** | **185%** |
| Go | **267%** | **477%** | **193%** | **133%** |

**What the two tables say together:** the M2 relative story does not transfer to this
host. Against g++, the ports sit near WRITE parity (Rust 108%!) — because the C++
reference never received its restrict boost here (verdict (a) below) — while READS sit
further away than on the M2 (Go 370–477%). And "C++ = 100%" names a different C++
depending on compiler: clang-18 beats g++ by up to 4.3x on tiny-message writes (verdict
(c)), so every relative cell moves by double-digit points when the denominator changes
compiler. **A relative table is a fact about a (code, compiler, microarchitecture)
triple, never about a language.** The README's "dated snapshot, not a verdict" warning
was written for time; this pass proves it for space too.

## Verdict (a): the restrict win is REFUTED on Zen 4 — under BOTH compilers

serialize #30 (restrict-qualifying the writer's `this`) measured +38%…+158% composed on
the C++ write rows on arm64/apple-clang — the single biggest C++ write lever of the
program. The A/B here isolates exactly #30 (`serialize` main vs `561e81d`), write rows:

| bench (write) | g++ pre | g++ post | post/pre | clang pre | clang post | post/pre |
|---|---:|---:|---:|---:|---:|---:|
| rigidbody_moving | 24.24 | 24.59 | **1.01x** | 24.45 | 24.69 | **1.01x** |
| rigidbody_at_rest | 40.96 | 40.92 | **1.00x** | 50.46 | 50.54 | **1.00x** |
| chat | 35.69 | 35.71 | **1.00x** | 65.38 | 65.52 | **1.00x** |
| test | 118.37 | 118.14 | **1.00x** | 505.49 | 505.04 | **1.00x** |
| inputpacket | 31.79 | 31.88 | **1.00x** | 31.13 | 31.13 | **1.00x** |
| shipcreate | 31.63 | 34.32 | **1.09x** | 52.52 | 52.63 | **1.00x** |
| ship_shallow | 55.68 | 55.86 | **1.00x** | 61.36 | 61.35 | **1.00x** |
| probe_header | 122.34 | 120.98 | **0.99x** | 496.36 | 501.82 | **1.01x** |
| probebits | 83.35 | 83.35 | **1.00x** | 89.49 | 89.99 | **1.01x** |
| probearray | 29.71 | 25.70 | **0.87x** | 36.54 | 35.27 | **0.97x** |
| testdata | 9.06 | 9.18 | **1.01x** | 14.09 | 14.09 | **1.00x** |
| message_batch | 61.06 | 61.41 | **1.01x** | 60.58 | 60.37 | **1.00x** |

The rows that moved +128%…+152% isolated on arm64 (the rigidbody pair) move **1.01x**
here. Nothing anywhere approaches the arm64 class; the three small movers (shipcreate
+9%, probearray −13%, testdata read +11% in the full table) are the layout band. The
theory going in was "boundary-aliasing, compiler-dependent" — the measurement is stronger:
**absent on this target under both g++-13 and clang-18.** The M2 write table's C++
dominance is in material part an apple-clang/arm64 aliasing win, not a property of the
generated code. (Confirmed independently by the v2→v5 composed C++ rows here: mostly
1.00–1.12x, vs the M2's 1.6–2.4x writes — the C++ program's composed effect on this host
is bulk-bytes and const-emit, not restrict.) Raws:
`2026-08-07-four-language-v5-x86_64-epyc-restrict-ab-{gcc,clang}.csv`.

## Verdict (c): the g++-vs-clang tiny-message gap PERSISTS post-const-emit

Pre-everything (2026-08-06 baseline) the gap was probe_header write **4.01x**, test write
**3.62x**. Post const-emit (schema #8, generation-time bit-count folding) it is
probe_header **4.15x**, test **4.27x** — unchanged to slightly wider. clang-18 leads 19
of 24 rows, up to 1.90x on reads (rigidbody_moving 1.90x, probebits 1.89x); g++ holds
only testdata read (1.19x the other way), probearray read, and two batch-write ties.
Full pairing in the harvest analysis; raw legs 1 vs 5.

**Interpretation:** the folding schema's generator does (bit counts known at generation
time) is not the folding clang does (whole-header store merging across consecutive
fields) — const-emit removed the outlined runtime calls and the 4x remained, so the 4x
lives in instruction selection and store merging, not in bit-count arithmetic. **Open
question routed to the gap ledger:** whether x86 tables should adopt clang-18 as the
reference compiler (g++ stays primary in this pass per the original baseline; both are
committed).

## Verdict (b): lane composition on x86 — Go transfers almost row for row

Per-language v2→v5 absolute deltas on this host (v2 EPYC, 2026-08-06 pre-everything, is
the only prior x86 record; v2 predates the go READ lane as well, so go reads state that
provenance):

- **Go writes: the lane transfers.** ship_shallow **1.89x** (M2 composed: 1.83x),
  shipcreate **1.79x** (1.79x — exact), test 1.77x (1.61x), testdata 1.61x (1.37x),
  inputpacket 1.40x (1.30x), probebits 1.38x (1.39x), probearray 1.39x (1.35x),
  probe_header 1.23x (1.26x). Same mechanism, same shape, second microarchitecture —
  serialize.go #19 + schema #13 are portable, unlike restrict. Go reads gained
  1.26–2.13x v2→v5 (that includes serialize.go #17's read lane, which is v2→v3).
- **Rust: the write lane transfers, one x86-specific regression filed.** The
  serialize.rs #19/#20 + schema #5/#6 rows: inputpacket write 2.05x, shipcreate 2.06x,
  ship_shallow 2.18x; batch read **2.08x** (read_message_into). But chat write composed
  only **1.16x** here vs 1.70–1.78x on M2 — the 256B–2KB array-copy kill pays less on
  Zen 4 — and **inputpacket read regressed 0.85x** (14.51 → 12.29, beyond spreads
  2.3%/1.9%), unattributed, filed with the ledger's rust-read items.
- **C#: write composition is UNMEASURABLE on this host under default config** (the
  artifact sits on both sides of the v2→v5 compare). The read rows, which are mostly
  clean both sides, composed 1.60–2.08x on seven of twelve (rigidbody_at_rest 2.08x,
  inputpacket 2.06x, shipcreate 2.04x, probearray 2.02x, ship_shallow 1.81x, testdata
  1.63x, probebits 1.60x†spready). The steady-state diagnostic cannot be compared to v2
  (different runtime config).
- **C++: quiet, as verdict (a) predicts.** Mostly ≤1.12x; testdata read **1.33x** (the
  bulk-bytes #7 win, portable), test read 1.12x, test write 1.10x; probearray write
  0.93x / batch write 0.94x small down-moves within the band.

## Caveats

- Everything here shares core 0 with irqs, ssh and systemd — the label rides every
  preamble. This is the quietest x86 we have, not a quiet x86.
- The clang leg's reads are spready in places (rigidbody_at_rest 25.9%, chat 26.6%,
  probebits 32.1%) — medians sit against max; read its read cells with that in mind.
- One write-control row moved beyond spread (test write +13%) — the same bench's write
  cell moved 1.30x between sittings in the v2 EPYC doc; treated as the layout band.
- The diagnostic leg ran 2026-08-11, four days after the pass, same box, same pins,
  serial and alone; it is mechanism proof, not a fifth table.
- v5 is pinned by SHA above; anything merged after the pull is out of v5.

## Files

- `2026-08-07-four-language-v5-x86_64-epyc.csv` — main table (legs 1–4 concatenated)
- `2026-08-07-four-language-v5-x86_64-epyc-clang.csv` — leg 5 (clang-18 C++)
- `2026-08-07-four-language-v5-x86_64-epyc-restrict-ab-gcc.csv` — leg 6
- `2026-08-07-four-language-v5-x86_64-epyc-restrict-ab-clang.csv` — leg 7
- `2026-08-07-four-language-v5-x86_64-epyc-write-control.csv` — leg 8
- `2026-08-11-cs-tiered-jit-diagnostic-x86_64-epyc.csv` — the labeled diagnostic
- `2026-08-07-v5-epyc-pass.log` — the serial-gate evidence (quiet windows, core-0 snapshots)
