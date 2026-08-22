# certified-space-1 — the family's first certified era on the space box

**Era:** `certified-space-1` · **Date:** 2026-08-18 (NZ; measurement window
2026-08-17 09:49–10:32 UTC) · **Tier:** CERTIFIED (BENCH-STANDARD §2 in full:
§2.6 control legs, §2.6.1 A/A twin gates + per-row historical bands, §2.4
interleaving, §2.2 best+median, golden gate every leg-round)

**Host:** `spacegame` — AMD EPYC 9124 16-core Zen 4, Ubuntu 24.04, kernel
6.8.0-90. Cores 1–15 isolated (`isolcpus=1-15 nohz_full=1-15 irqaffinity=0
nosmt`); every timed run `taskset -c 15` on the tickless isolated core; builds
and driver on housekeeping core 0. Game server **stopped** for each window
(player check first: journal empty 2 h, zero game-protocol UDP flows),
**restarted and receipted** after each — see Receipts.

**Toolchain:** gcc/g++ 13.3.0 (Ubuntu), shipped Release flags, `-O3`, no LTO,
no extra codegen flags. gcc warning accommodations only (`-Wno-class-memaccess
-Wno-type-limits` C++, `-Wno-stringop-truncation` C) — suppressions, not
codegen. Full flag lines are in the CSV preambles beside this file.

**Commits measured:** schema `22a4604` (main, contains 6cd08a1) · serialize
`dd24915` (main) · serialize.c `dcbf47a` (main, #26–#29 all in). Corpus
`7a7c343f1a446f4c` (the §1.7 corpus with `real_packet` — RealWorld.schema, 97
fields, 1629-bit / 204-byte pinned wire). Runtime paths §3.5 build-verified in
every leg.

**Two windows were run.** Window 1's first adjudication tripped two instrument
defects (both fixed on this branch, both disclosed below) and caught one
genuine machine-state event; window 2 ran end-to-end clean on the fixed
instruments. **Window 2 is this era's reference table.** Window 1 is its
replication: every clean row agrees with window 2 inside spread.

---

## The certified table — window 2, C = 100

Convention (§5): C is the reference; every percentage is **time as % of C**
(`c_rate / cpp_rate × 100`) — **below 100 = C++ faster, above 100 = C faster.**
Headline on best-of (§2.2), median beside it. 14 samples per row per language
(7 interleaved rounds × A/A twins, same-inode binaries), aggregated by the
driver, never the runner. Window: controls 114.16 → 113.98 M msg/s, **delta
0.2%, window OK**. Twin gate **OK, 72/72**. Bands vs quick-tier history:
**72/72 band-ok, zero breaks**.

| bench | path | family | C max M/s | C++ max M/s | C++ % of C (best) | C++ % of C (median) | C spread | C++ spread | marks |
|---|---|---|---:|---:|---:|---:|---:|---:|---|
| rigidbody_moving | write | gen | 127.58 | 132.13 | 96.6 | 96.6 | 0.0% | 0.0% |  |
| rigidbody_moving | read | gen | 197.48 | 180.84 | 109.2 | 109.0 | 1.1% | 1.9% |  |
| rigidbody_at_rest | write | gen | 208.36 | 308.32 | 67.6 | 72.4 | 0.8% | 15.4% | noisy(§2.3) |
| rigidbody_at_rest | read | gen | 97.33 | 97.34 | 100.0 | 100.0 | 0.0% | 0.0% |  |
| chat | write | gen | 66.02 | 69.80 | 94.6 | 94.6 | 0.9% | 3.2% |  |
| chat | read | gen | 99.96 | 99.96 | 100.0 | 100.0 | 4.3% | 3.9% |  |
| test | write | gen | 403.96 | 246.68 | 163.8 | 163.7 | 0.2% | 0.0% |  |
| test | read | gen | 740.66 | 737.13 | 100.5 | 100.5 | 1.9% | 1.3% |  |
| inputpacket | write | gen | 55.22 | 59.67 | 92.5 | 99.4 | 0.0% | 15.0% | noisy(§2.3) |
| inputpacket | read | gen | 22.91 | 40.20 | 57.0 | 56.6 | 7.9% | 0.3% |  |
| shipcreate | write | gen | 84.07 | 122.92 | 68.4 | 65.2 | 7.0% | 0.1% |  |
| shipcreate | read | gen | 138.85 | 145.67 | 95.3 | 94.9 | 3.6% | 3.2% |  |
| ship_shallow | write | gen | 98.63 | 145.03 | 68.0 | 64.9 | 8.9% | 0.2% |  |
| ship_shallow | read | gen | 156.61 | 157.53 | 99.4 | 100.2 | 1.9% | 1.2% |  |
| probe_header | write | gen | 388.32 | 336.35 | 115.5 | 112.5 | 5.2% | 0.0% |  |
| probe_header | read | gen | 896.23 | 902.44 | 99.3 | 98.8 | 3.0% | 1.2% |  |
| probebits | write | gen | 246.65 | 223.17 | 110.5 | 123.7 | 0.1% | 13.6% |  |
| probebits | read | gen | 430.72 | 441.37 | 97.6 | 97.4 | 2.3% | 0.2% |  |
| probearray | write | gen | 49.33 | 25.50 | 193.5 | 194.8 | 0.0% | 2.8% |  |
| probearray | read | gen | 78.68 | 31.10 | 253.0 | 254.2 | 5.3% | 6.5% |  |
| testdata | write | gen | 21.00 | 22.67 | 92.6 | 95.1 | 0.7% | 4.7% |  |
| testdata | read | gen | 48.52 | 30.46 | 159.3 | 158.7 | 2.6% | 2.1% |  |
| **real_packet** | **write** | gen | **6.35** | **6.96** | **91.2** | **91.4** | 4.0% | 1.6% |  |
| **real_packet** | **read** | gen | **7.92** | **9.44** | **83.9** | **84.0** | 0.5% | 0.3% |  |
| message_batch | write | gen | 91.83 | 105.69 | 86.9 | 86.9 | 1.5% | 1.3% |  |
| message_batch | read | gen | 62.52 | 63.29 | 98.8 | 98.7 | 3.4% | 4.1% |  |
| bench_packet | write | rt | 68.51 | 123.33 | 55.6 | 55.5 | 2.2% | 0.0% |  |
| bench_packet | read | rt | 104.15 | 241.70 | 43.1 | 45.5 | 0.1% | 13.8% |  |
| bench_ints | write | rt | 99.60 | 213.45 | 46.7 | 45.6 | 4.9% | 0.8% |  |
| bench_ints | read | rt | 142.26 | 260.09 | 54.7 | 55.0 | 1.0% | 1.5% |  |
| bench_bits | write | rt | 298.60 | 281.89 | 105.9 | 105.2 | 4.0% | 2.2% |  |
| bench_bits | read | rt | 359.27 | 365.22 | 98.4 | 101.0 | 1.1% | 3.8% |  |
| bench_mixed | write | rt | 86.04 | 211.00 | 40.8 | 41.5 | 1.6% | 3.8% |  |
| bench_mixed | read | rt | 132.09 | 256.41 | 51.5 | 51.6 | 0.2% | 0.7% |  |
| bitpacker | write | bits | 0.06 | 0.07 | 97.9 | 97.9 | 0.3% | 0.1% |  |
| bitpacker | read | bits | 0.06 | 0.06 | 101.4 | 101.4 | 0.2% | 0.2% |  |

**Column medians (clean rows):** write — all-family C++ 93.6 (best) / 94.8
(median), family gen 94.6 / 95.1. read — all-family 99.1 / 99.4, family gen
99.4 / 100.0.

## Verdicts against 2026-08-17's quick tier

1. **C++ write parity HOLDS at certification tier.** The gen write column
   median is C++ at 94.6% of C's time (best) / 95.1% (median) — the quick
   tier's "write ≈ 95, C++ slightly ahead" claim, reproduced under controls,
   twins, and bands. The write rows where C leads are the same three the quick
   tier named, none new: `test` write (163.8 — the one still-unattributed C
   lead, carried on the board), `probe_header` write (115.5), `probebits`
   write (110.5 best / 123.7 median — the C++ leg is two-moded on this row).
   Read column: parity (gen median 99.4 / 100.0).

2. **The real_packet C++ lead on gcc/x86 HOLDS at certification tier, and
   tightly.** Certified: C++ at **91.2 (write) / 83.9 (read)** — C++ leads by
   9.6% / 19.2% on the row that models the actual game packet. Quick tier
   claimed 90 / 84. Window 1 independently measured 90.7 / 83.8. Three
   measurements, two certified windows, all within 1%. On this compiler and
   architecture the real-world case belongs to C++, exactly as the quick tier
   said — while on Air/clang the same row belongs to C (25.7/29.9 vs
   21.2/26.7, quick tier, unchallenged tonight). Both leads stand attributed
   (Air: C's cleaner spine post-#27/28/29; space: C's residual `floorf@plt`
   strand + native `__int128` economics).

3. **No certified row contradicts the quick tier beyond noise.** Window 2's
   72 rows sit 72/72 inside the quick-era band envelope (realworld-space,
   realworld2-space, pr29 quick-check). The rt-family C++ ~2x leads
   (bench_packet/ints/mixed — the attributed TU-call-boundary-era class
   [label era-scoped 2026-08-22, issue #66: both legs linkage=hdr since
   serialize.c #25, so the literal TU boundary is gone; the leads reproduce
   and their attribution is the timing era's, pending re-attribution]) and
   the C ~2x leads on probearray/testdata reads (the #29 lane-inline wins,
   C++ mirror candidate still on the board) all reproduce.

4. **New at certification tier — C-leg state sensitivity on gcc/x86, caught
   and contained by the instruments.** Window 1 round 5: one whole C round
   collapsed on 8 rows (23–70% of neighboring rounds — rigidbody_moving read
   40%, bench_bits write 27%/read 23%, shipcreate 47/57%, ship_shallow 46/52%,
   rigidbody_at_rest read 70%), recovered by round 6; C++ was clean in the
   same round, and the same C binary (same inode) was clean in 13 of 14
   samples. Row- and binary-selective — the write-demand-collapse phenomenon
   class §2.6.1 documents, now measured on the second architecture. §2.3 did
   its job: the 7 worst rows carry spreads 44–78% in window 1 and their
   ratios are REFUSED there; window 2 measured all 7 clean (spreads ≤ 9%)
   and those are the published values. Beside the collapse, two milder
   two-attractor patterns are now on the record: C `ship_shallow`/`shipcreate`
   writes alternate between two stable modes ~9% apart (90.2 vs 98.6 M/s,
   both windows), and C++ `rigidbody_at_rest`/`inputpacket` writes carry a
   ~15% two-mode spread (the "cpp layout variance" the quick tier flagged,
   now marked by discipline instead of eyeballed). Window 1 also sampled a
   fast C `inputpacket` read attractor once (26.8 M/s vs 21–23 everywhere
   else — its only band-break, +17% ABOVE history); window 2 never saw it.
   None of these move any verdict; all of them are why certification runs 14
   samples with refusal rules instead of 1–4 samples with good intentions.

## Certification receipts

- **Controls (§2.6):** four control legs across the night — 114.25 / 114.07
  (window 1, delta 0.2%, OK) and 114.16 / 113.98 M msg/s (window 2, delta
  0.2%, OK). Total drift across both windows: 0.24%.
- **Twin gates (§2.6.1):** window 2: **72/72 twin-ok** (fixed gate, live).
  Window 1: **72/72 twin-ok** re-adjudicated on the same twin data after the
  band-precision fix; the original false-SUSPECT verdict is preserved in
  `-window1-twingate-original.txt` and `-window1-original.csv`.
- **Bands (§2.6.1):** window 2 vs quick history: **72/72 band-ok**. Window 1:
  71/72 band-ok + 1 break (`c/inputpacket/read` +17% ABOVE — the fast
  attractor, published with its mark). Window 2 vs window 1: 65/65 band-ok on
  every row window 1 published (the 7 §2.3-invalid window-1 rows band nothing,
  by the validity rule).
- **Golden gates:** every one of the 60 leg-rounds (2×30) ran the golden
  byte-compare + round-trip gate before emitting a number; corpus
  `7a7c343f1a446f4c` confirmed in every leg. Zero failures.
- **Refusal log:** (1) window 1 first pass REFUSED by run.sh's §3.5 gate
  (bystander-language inversion; fixed, 3.5 min downtime lost); (2) window 1
  twin gate SUSPECT on 2 rows — adjudicated an instrument rounding defect
  (twins agreed to ~1 ppm), fixed at full precision, re-adjudicated clean;
  (3) window 1: 7 C rows §2.3-INVALID (spreads 44–78%, the round-5 event) —
  ratios refused there, published from window 2; (4) the §5.3 rel tool
  REFUSES cross-language ratios from both windows by rule 7
  (`inline=unknown` — no x86 §4.1 adapter exists), as designed; the ratio
  arithmetic here is the same §5 formula computed outside the tool and
  marked. Receipt (verbatim): *"they disagree on: inline: A="unknown"
  B="unknown" (a number without an inline verdict is not comparable to one
  with it)"*.

## Server and wall-clock receipts

| event | UTC |
|---|---|
| player check (journal 2 h empty, no game UDP flows) | 09:45–09:48 |
| server stopped (window 1) | 09:49:01 |
| window 1 pass (30 legs incl. 3.5 min lost to the §3.5 refusal) | 09:52:38–10:09:35 |
| server restarted, `is-active` = active | 10:10:25 |
| server stopped (window 2; journal showed only our restart) | 10:13:22 |
| window 2 pass (30 legs, clean end-to-end) | 10:13:29–10:30:14 |
| **server restarted, `is-active` = active, simulation logging** | **10:32:27** |

Total production downtime 40 m 29 s across two windows. Load receipts are in
the CSV preambles (§2.5, driver-captured): window 1 `load_start 0.20`
`load_max 1.07` `core_busy_pct 98.0` (the bench itself owns the pinned core)
`foreign_procs 1`; window 2 `load_max 4.83` (decaying server-restart residue
at window open on core 0; the pinned core's legs are the twin/control/band
evidence above) `foreign_procs 1`.

## Named gaps, carried honestly

1. **`inline=unknown` on every row.** The §4.1 inline verdict has no
   x86/objdump adapter (inline-verdict.sh refuses on non-otool hosts rather
   than faking `full` — its own documented choice). The §5.3 rel tool
   therefore refuses machine ratios from these CSVs by rule 7, and the
   arithmetic in this report is marked as computed outside it. The adapter is
   the standing follow-up; a future era fills the column and the tool takes
   over the table.
2. **Band history is two days deep.** Prior same-configuration rows exist only
   from 2026-08-17's quick realworld eras (corpus `7a7c343f1a446f4c`). Eras
   before schema #61 carry corpus `6b118770d85af4f6` and correctly cannot
   band these rows (§1.6). This era is the first VALID-window anchor; bands
   thicken from here.
3. **Quick-era CSVs need read-time conversion to band at all:** their
   annotated `# build:` lines are silently dropped by the loader (every row —
   no error), and their 18th `roundN` column is refused. Derived band-input
   copies live on the bench (`~/rowan-bench-quick/results/bands-prior/`);
   originals untouched. The silent half of that deserves a loader fixture.
4. **The round-5 collapse is measured, not attributed.** Named for the next
   session: same binary, same inode, one round, 8 rows, 1.4–4.3x — the
   §2.6.1 phenomenon class on its second architecture. The twin/band
   instruments contain it; nobody has yet explained it.

## Instrument fixes this era forced (all on this branch, each with receipts in its commit)

1. `bench/run.sh` — C leg on gcc: `-Wno-stringop-truncation` conditional,
   mirroring the C++ accommodations (`fa5e03a`).
2. `bench/tools/pass-driver.sh` — `BENCH_TOOLS` prebuilt-binary escape: the
   box's go 1.22.2 cannot build go.mod-1.26 tools; instruments cross-compiled
   from the same schema commit (`fa5e03a`).
3. `bench/tools/pass-driver.sh` — the §3.5 gate verifies only `--langs`
   (`fa5e03a`); `bench/run.sh` — non-bare `--only` verifies the leg it RUNS,
   bystanders are marked `[UNVERIFIED — leg not run this invocation]`
   (`5834e75`). Three §3.5-family inversions of the same
   skipped-leg-is-a-fact contract in one night; a fixture for the class is
   the obvious follow-up.
4. `bench/tools/twinband.go` — the twin band is recomputed at full precision
   from the rate columns, not read from the 2-decimal `spread_pct` print
   (`8e32b70`). The print collapsed to a 0.00 band on rows whose twins agree
   to ~1 ppm — the gate refused the QUIETEST rows on the quietest machine, an
   inversion on exactly the surface §2.6.1 was written for.
5. `bench/tools/relative.go` + `aggregate.go` — `real_packet` joins the order
   list, and aggregate now REFUSES when accumulated keys go unprinted
   (`d103b3e`). Schema #61 added the rows to the runners but not the list;
   the first real outing of the aggregate pipeline silently dropped the
   campaign's headline row, precisely as the list's own comment warned.

## Files of this era

- `2026-08-18-certified-space-1-window2.csv` — **the reference table's data**
- `2026-08-18-certified-space-1-window1.csv` — replication window (corrected
  aggregation; provenance notes in its preamble)
- `2026-08-18-certified-space-1-window1-original.csv` — window 1 as the
  pre-fix driver emitted it (false twin-SUSPECT, real_packet missing) — the
  honest record of what the instruments did before they were fixed
- `-window{1,2}-twingate*.txt`, `-window{1,2}-bands.txt`,
  `-window2-vs-window1.txt` — gate outputs verbatim
- `-window{1,2}-pass.log` — driver logs: pscheck quiet-window checks, leg
  timings, load boundaries

*Measured and written by Rowan (worker session 7974800c), night of
2026-08-17→18 NZ. This table is the family's gcc/x86-64 reference until the
next certified era.*
