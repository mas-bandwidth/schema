# 2026-08-07 — four-language comparison v5 (the endpoint of the optimization program)

**What v5 is**: the closing measurement of the two-day cross-language
optimization program — the pass after all three per-language lanes merged.
v4 (`2026-08-07-four-language-v4.md`) measured the post-restrict mains; since
then the C# lane (schema #12), the Go lane (serialize.go #19 + schema #13),
the Rust lane (schema #14) and the Go strings PR (serialize.go #18) all
landed. v5 is the mains, fresh-pulled, at exactly these commits — the pin is
the definition:

| repo | main @ v5 | vs v4 |
|---|---|---|
| schema | `3baa6fd52990cffb3da281c6738a1efc42c39bea` | +#12 (C# batch opt-in by scalar density + bulk-bytes + fold), +#13 (Go folded bit counts + bulk-bytes), +#14 (Rust folded bit counts + bulk-bytes), +CLAUDE.md perf docs |
| serialize | `040d286470bbceec1b0959e2f311e94ca9965bee` | +#31 (CLAUDE.md perf docs — no code change) |
| serialize.go | `68ce62432779a988f124e7a65419d7856def488e` | +#19 (write path: flat writer state + `tryWriteBits` fused at cost 64), +#18 (SerializeString reuses the value on equal incoming bytes — stable reads 0-alloc) |
| serialize.rs | `422e4fa031b58d6837ca0f10df8d43a66e0d5abe` | unchanged |
| serialize.cs | `e2bda998b234142fd0dae4fe7d845bc20ea06e8e` | unchanged |

All five pins were verified against the GitHub mains by API at pull time, and
the sibling checkouts the Makefile and runners build against
(`../serialize-cs-port/*`) were verified at exactly these SHAs before the run.

**Host coverage: M2 only — EPYC still deferred on Glenn's word** ("stick with
m2 for now"); the v2 EPYC tables remain the latest x86 data, and every x86
claim in the lane docs keeps its own pairing as the measurement of record.

Same methodology as v1–v4 (`bench/README.md`), same harness — unchanged since
v2: golden + round-trip self-checks gate every runner (a runner that does not
produce corpus-identical bytes refuses to bench), escape barriers, LCG
variation, 64 variant read buffers, 1 warmup + median-of-7, one
`bench/run.sh` invocation. **The golden gate held on all four runners**; the
go and cs alloc notes read **0 allocs / 0 bytes on every bench** including
batch. v2 through v5 are directly comparable on both paths.

## Apple M2 (arm64, quiet, unpinned)

macbook, Darwin 25.5.0. Apple clang 21.0.0 `-O3 -DNDEBUG -DSERIALIZE_RELEASE
-std=c++17 -ffp-contract=off -fno-rtti`; go1.26.5 (`go run`, default
optimized); cargo/rustc 1.97.1 (`--release`, opt-level 3, no LTO); dotnet
10.0.302 (`-c Release`, workstation GC) — toolchains identical to v1–v4.
Raw CSV: `2026-08-07-four-language-v5-arm64-m2.csv`.

| bench | path | B/msg | C++ M msg/s | C++ MB/s | Rust M msg/s | Rust MB/s | Go M msg/s | Go MB/s | C# M msg/s | C# MB/s |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| rigidbody_moving | write | 105 | 57.83 | 5791 | 26.00 | 2604 | 18.20 | 1822 | 32.68 | 3272 |
| rigidbody_moving | read | 105 | 87.32 | 8744 | 48.08 | 4814 | 24.00 | 2404 | 35.75 | 3580 |
| rigidbody_at_rest | write | 57 | 102.19 | 5555 | 51.40 | 2794 | 33.01 | 1794 | 52.47 | 2852 |
| rigidbody_at_rest | read | 57 | 118.19 | 6425 | 90.22 | 4904 | 40.64 | 2209 | 56.44 | 3068 |
| chat | write | 13 | 103.50 | 1283 | 66.22 | 821 | 33.46 | 415 | 39.73 | 493 |
| chat | read | 13 | 130.89 | 1623 | 91.97 | 1140 | 68.38 | 848 | 79.16 | 981 |
| test | write | 6 | 734.45 | 4203 | 227.62 | 1302 | 105.63 | 604 | 125.49 | 718 |
| test | read | 6 | 561.87 | 3215 | 215.75 | 1235 | 84.18 | 482 | 157.23 | 900 |
| inputpacket | write | 61 | 63.96 | 3721 | 39.57 | 2302 | 16.37 | 952 | 33.59 | 1954 |
| inputpacket | read | 61 | 45.92 | 2671 | 19.37 | 1127 | 16.00 | 931 | 28.64 | 1666 |
| shipcreate | write | 28 | 100.53 | 2684 | 66.37 | 1772 | 31.10 | 830 | 52.93 | 1413 |
| shipcreate | read | 28 | 110.37 | 2947 | 54.02 | 1443 | 23.38 | 624 | 51.47 | 1374 |
| ship_shallow | write | 28 | 124.23 | 3317 | 84.65 | 2260 | 34.72 | 927 | 54.63 | 1459 |
| ship_shallow | read | 28 | 116.49 | 3111 | 38.36 | 1024 | 26.25 | 701 | 54.73 | 1462 |
| probe_header | write | 10 | 1048.38 | 9998 | 197.10 | 1880 | 84.71 | 808 | 114.22 | 1089 |
| probe_header | read | 10 | 632.56 | 6033 | 157.13 | 1499 | 77.27 | 737 | 126.18 | 1203 |
| probebits | write | 26 | 177.19 | 4393 | 98.03 | 2431 | 61.51 | 1525 | 87.45 | 2168 |
| probebits | read | 26 | 503.41 | 12482 | 129.24 | 3205 | 62.55 | 1551 | 93.38 | 2315 |
| probearray | write | 47 | 69.72 | 3125 | 39.45 | 1768 | 20.19 | 905 | 39.75 | 1782 |
| probearray | read | 47 | 67.32 | 3018 | 68.69 | 3079 | 17.40 | 780 | 40.12 | 1798 |
| testdata | write | 92 | 27.70 | 2431 | 16.29 | 1429 | 9.29 | 815 | 13.90 | 1219 |
| testdata | read | 92 | 25.45 | 2233 | 15.15 | 1329 | 9.79 | 859 | 9.88 | 867 |
| message_batch | write | 25 | 81.00 | 1931 | 66.74 | 1591 | 39.74 | 948 | 57.66 | 1375 |
| message_batch | read | 25 | 74.21 | 1769 | 48.49 | 1156 | 37.51 | 894 | 42.35 | 1010 |

## THE RELATIVE TABLE (time relative to C++ — C++ = 100%, higher is slower)

Medians across the 11 corpus benches; the mixed-dispatch batch separately.
Two "was" columns: v4 (the pass before the per-language lanes) and v1 (where
the program started, 2026-08-06 morning). The v1 read and batch cells carry
the v1 harness artifact (a per-iteration allocation the v2 harness fix
removed — it depressed every language's reads and C++'s batch read, which is
why v1's sub-100% batch-read cells were retracted at v2); v1 is shown as the
honest starting record, not as a comparable measurement. v2–v5 are directly
comparable.

| backend | write | was v4 | was v1 | read | was v4 | was v1 | batch write | was v4 | was v1 | batch read | was v4 | was v1 |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| C++ | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% |
| Rust | **177%** | 188% | 173% | **204%** | 236% | 243% | **121%** | 110% | 159% | **153%** | 156% | 148% |
| C# | **199%** | 379% | 238% | **214%** | 343% | 306% | **140%** | 155% | 161% | **175%** | 204% | 80%* |
| Go | **323%** | 490% | 301% | **387%** | 387% | 722% | **204%** | 236% | 236% | **198%** | 208% | 99%* |

\* the v1 harness artifact: C++'s batch read was paying a per-iteration 2 KB
instance the other runners hoisted — the "C# wins batch read" headline died
with the v2 harness fix.

**C++ leads every column against every backend — and every backend's every
column improved v4→v5 except Go read (flat — its lane was write-only) and
rust batch write (110% → 121%, the layout-band cell, composition finding
4).** The write
column tells the program's story in one line: at v1 the three ports stood at
173–301% with an unoptimized C++; two days later C++'s own writes are
1.6–2.4x faster than v1 and the ports stand at 177–323% of the moved
reference — every lane closed most of the gap the C++ program had opened
(v4: 188–490%).

## Composition verdicts (the lane claims, measured at the composed mains)

1. **Go writes: the lane's 490% → 323% HOLDS EXACTLY composed — 323% on the
   nose.** Every go write row improved 1.08x–1.83x v4→v5 (ship_shallow
   1.83x, shipcreate 1.79x, test 1.61x, probebits 1.39x, testdata 1.37x,
   probearray 1.35x, inputpacket 1.30x, probe_header 1.26x), all beyond
   spread, matching serialize.go #19 + schema #13's paired table row for
   row. Batch write composed at 204% vs the lane's 198%.
2. **C# columns: the lane's "approx ~201% / ~219%" are now official at
   199% write / 214% read** — the composed mains came in a hair better than
   schema #12's approximation. The batch opt-in composed at scale: 16 of 22
   cs corpus rows improved beyond spread, up to 2.39x (inputpacket write),
   with the excluded-by-density-rule types (chat, message_batch stream
   path) flat or within band as designed.
3. **Rust testdata read: the lane's +49.8% composed at +49.1%** (10.16 →
   15.15 M msg/s; schema #14's pairing measured 10.10 → 15.13). testdata
   write +9.3% beside it (lane: +9.2%). Everything else rust moved ≤2%,
   exactly the lane's "neutral everywhere else" prediction. The one row
   relocating across the median moved the rust read column 236% → 204%.
4. **Full-sweep regression check: ZERO rows regressed beyond spread across
   all 96 rows.** The largest down-mover is rust batch write, −11.4%
   (75.36 → 66.74), inside its own v4 spread of 13.5% — this is the
   program's known layout-jumpy cell: it rose +12% v3→v4 unattributed on
   untouched code, and the rust lane's own pairing measured a −4.6% layout
   band; v5 puts it back at 0.99x its v3 value (67.52). Filed with the v4
   finding-4 family (layout-suspected, unproven), not chased.
5. **C++ drifted −1% to −5.5% across 19 of 24 rows, all within spread** —
   uniform, code-unchanged (serialize #31 is docs-only), consistent with
   today's higher ambient load (~2.6–2.9 at run start vs ~1.8 at v1–v4).
   It shaves a few points off every relative cell; none of the verdicts
   above depend on it (the Go 323% match is against the same-run
   denominator, the same shape as the lane's own pairing).

## v1 → v5 progression (M msg/s — the whole program, headline rows)

| lang | bench | path | v1 | v2 | v3 | v4 | v5 | v5/v1 | what did it |
|---|---|---|---:|---:|---:|---:|---:|---:|---|
| C++ | rigidbody_moving | write | 24.92 | 23.77 | 22.71 | 58.63 | 57.83 | **2.32x** | serialize #30 restrict |
| C++ | rigidbody_at_rest | write | 44.94 | 43.80 | 43.26 | 105.22 | 102.19 | **2.27x** | serialize #30 |
| C++ | probebits | write | 83.34 | 81.56 | 94.61 | 180.91 | 177.19 | **2.13x** | schema #8 const-emit, then serialize #30 |
| C++ | testdata | read | 13.70 | 14.02 | 25.54 | 25.88 | 25.45 | **1.86x** | schema #7 bulk-bytes |
| C++ | testdata | write | 17.11 | 16.84 | 20.36 | 28.08 | 27.70 | **1.62x** | schema #7/#8 + serialize #27, then #30 |
| C++ | message_batch | read | 31.37 | 67.24 | 66.95 | 78.50 | 74.21 | **2.37x** | v1→v2 harness fix; v3→v4 unattributed (v4 finding 4) |
| Rust | probearray | read | 28.98 | 31.61 | 66.32 | 68.12 | 68.69 | **2.37x** | serialize.rs #19 + schema #5 |
| Rust | message_batch | read | 21.15 | 20.61 | 22.12 | 50.24 | 48.49 | **2.29x** | schema #10 read_message_into |
| Rust | inputpacket | write | 19.42 | 18.72 | 39.87 | 39.46 | 39.57 | **2.04x** | serialize.rs #19 + schema #5 |
| Rust | chat | write | 38.96 | 37.54 | 67.51 | 66.39 | 66.22 | **1.70x** | serialize.rs #20 + schema #6 |
| Rust | testdata | read | 10.33 | 10.33 | 10.03 | 10.16 | 15.15 | **1.47x** | schema #14 bulk-bytes (this pass) |
| Go | chat | read | 19.80 | 64.16 | 70.15 | 69.48 | 68.38 | **3.45x** | harness fix (v1→v2) + serialize.go #17 |
| Go | inputpacket | read | 5.91 | 8.67 | 16.22 | 16.13 | 16.00 | **2.71x** | harness fix + serialize.go #17 |
| Go | rigidbody_at_rest | read | 17.64 | 22.86 | 41.06 | 40.91 | 40.64 | **2.30x** | harness fix + #17 |
| Go | ship_shallow | write | 19.47 | 19.02 | 19.04 | 19.00 | 34.72 | **1.78x** | serialize.go #19 + schema #13 (this pass) |
| Go | shipcreate | write | 17.81 | 17.51 | 17.49 | 17.43 | 31.10 | **1.75x** | #19 + #13 (this pass) |
| Go | test | write | 67.84 | 65.50 | 65.73 | 65.43 | 105.63 | **1.56x** | #19 + #13 (this pass) |
| Go | testdata | write | 6.94 | 6.62 | 6.67 | 6.78 | 9.29 | **1.34x** | #19 + #13 bulk-bytes (this pass) |
| C# | inputpacket | write | 14.35 | 13.46 | 14.13 | 14.03 | 33.59 | **2.34x** | schema #12 batch opt-in (this pass) |
| C# | testdata | write | 6.68 | 6.30 | 6.68 | 6.52 | 13.90 | **2.08x** | schema #12 bulk-bytes (this pass) |
| C# | rigidbody_moving | write | 16.15 | 15.18 | 15.52 | 15.46 | 32.68 | **2.02x** | schema #12 batch opt-in (this pass) |
| C# | shipcreate | write | 29.81 | 27.90 | 26.52 | 28.15 | 52.93 | **1.78x** | schema #12 (this pass) |
| C# | test | write | 111.85 | 93.11 | 109.91 | 109.58 | 125.49 | **1.12x** | serialize.cs #2 inlining recovered the v2 dip; schema #12 batch pushed past v1 |

## Per-language: what closed the gap (the program in four paragraphs)

**C++ (the reference — writes 1.6–2.4x v1, reads to 1.9x)**: schema #3
removed the union-zeroing memset from per-message construction (batch read
1.80–2.11x for that usage class); schema #7 emitted bulk-bytes for
statically aligned `[N]uint8` (testdata read +70–80%); schema #8 made the
generator the constexpr evaluator, folding bit counts at generation time
(+14.7% probebits write) after measuring and disqualifying the ratified
direct-template design; serialize #27 packed WriteBytes head/tail bytes;
and serialize #30 — Glenn's own instinct — restrict-qualified the writer's
`this`, the single biggest C++ write lever (+38%…+158% composed, v4). The
raised ceiling is what every other column is measured against.

**Rust (nearest chaser: 177/204/121/153)**: serialize.rs #19 put `#[inline]`
on the Stream trait's default methods (defaults don't inherit impl hints);
schema #5 inlined the generated wire fns and added `read_message_into`
(adopted by the harness in schema #10 — batch read 2.25x, the largest
single rust move); serialize.rs #20 + schema #6 killed the 256 B–2 KB
per-message array copy (chat write +74–78%); schema #14 (this pass) adopted
the two C++-proven emitter levers — folded bit counts and bulk-bytes —
closing testdata read +49%. Floors on record: the bounds-checked store
behind `unsafe_code = "forbid"` (policy, Glenn's to revisit) and
apple-clang's whole-header folding on tiny messages (probe_header write
0.19x same-run C++).

**C# (the biggest v4→v5 mover: 379→199 write, 343→214 read)**: serialize.cs
#2's AggressiveInlining recovered the v2 dip; serialize.cs #3 built the
register-resident WriteBatch/ReadBatch ref structs and priced the heap-field
floor by intervention (class 104.9 vs locals-ceiling 562.1 M ops/s); schema
#12 (this pass) made the emitter opt scalar-dense types into batch form
under the S ≥ 2 + 4·B density rule with both #3 hazards as law (inline-only
composition, per-type opt-in) plus bulk-bytes — 16 of 22 corpus rows past
spread, up to 2.39x. The honest negative stands: schema #8's bit-count
folding did nothing for C# (the JIT had already folded BitsRequired — kept
for simplicity, not speed).

**Go (write column closed 490→323, read parity held from v3)**: the v2
harness fix removed a per-iteration alloc the v1 loops charged Go hardest;
serialize.go #17 fit `readBits` under the gc inline budget (cost 83→64,
`tryReadBits` at exactly 80) — read parity on all 12 benches at v3, still
holding at v5; serialize.go #19 (this pass) did the same surgery on the
write side after the first-ever Go write profile convicted
`WriteStream.writeBits` at cost 107 — flat four-field writer state,
`tryWriteBits` fused at cost 64, ranged fields' double call boundary gone;
schema #13 (this pass) added folded bit counts (worth 1.35–1.37x in Go —
three languages, three magnitudes, one mechanism) and bulk-bytes.
serialize.go #18 (strings) landed too — neutral on this harness by
mechanism: its reuse fires on equal incoming bytes, and the harness's 64
variant buffers vary the string content (finding: the stable-read case it
optimizes is the common production case, not the benched one).

## Findings and refutations

1. **The three lanes composed without interference.** Go's 323% and C#'s
   199/214% and Rust's +49% all landed within a few points of their
   isolated pairings — unlike the serialize #27 precedent (v3 finding 5,
   the win that vanished composed). Backend-partitioned lanes (separate
   emitters, separate runtimes) compose by construction; the #27 case was
   two C++ changes sharing one header.
2. **serialize.go #18 did not move the composed chat read** (69.48 → 68.38,
   noise) — expected refutation, mechanism above. Its value is the 0-alloc
   stable-read path, which this harness's variation deliberately defeats.
3. **C# now beats Go on 23 of 24 rows** (v1: 19 of 24; the 24th, testdata
   read, is a statistical tie at 9.88 vs 9.79) — and it reaches Rust for
   the first time on individual rows: ahead outright on rigidbody_moving
   write (32.68 vs 26.00), inputpacket read and ship_shallow read, at
   parity on rigidbody_at_rest and probearray writes. Dispatch-surface
   shape, not language, keeps deciding.
4. **Rust batch write at 121% is the one cell above its v4 value** —
   finding 4 in the composition section: the v4 +12% layout drift reversed;
   the cell sits at its v3 level and inside every demonstrated band.
5. **Go read column flat at 387% — by design**, the go lane was write-only;
   its read program finished at v3. The go read floor question (one fused
   call per field, tiny-message folding — the rows Rust also floors on) is
   the ledger's, not this pass's.

## Caveats

- 4 of 96 rows carry within-run spreads over 20%: cs probebits write 56.1%
  (median 87.45 within 0.4% of max 87.84; min 38.75 is one desched run —
  the v4 caveat class exactly), cs message_batch write 27.6% / read 32.6%
  (same shape: medians within 4.2% / 1.5% of max), go rigidbody_moving
  read 41.1% (median 24.00 vs max 24.18; min 14.33 one outlier run). All
  four medians sit against their max; median-of-7 absorbed the outliers.
- Ambient load ~2.6–2.9 at run start (idle desktop, unpinned; v1–v4 ran
  near ~1.8) — the uniform small C++ down-drift in composition finding 5.
  Eight C++ spreads sit at 5–11% vs v4's typical 1–8%.
- EPYC absent by instruction; v2 EPYC tables remain the latest x86 data.
  The lane wins are Apple-clang/arm64-composed here; x86 composition waits
  for the EPYC leg's return.
- v5 is pinned by SHA above; anything merged after the pull is out of v5.

## What remains open

The per-item decomposition, floors and next profile targets live in
`2026-08-07-gap-ledger.md` (updated with the v5 scoreboard this commit; its
analysis reads against v3/v4 and stands). The headline open items: the go/rust
shared tiny-message floor (one fused call per field vs C++'s whole-header
folding), the C# batch-scope widening experiment (one Begin/End per packet),
the rust read column beyond testdata (204% median, never had a read-shaped
pass), C++'s two unattributed layout suspicions (batch read +17% at v4, rust
batch write ±12% band), schema #4 (tail-only union-arm zeroing, Glenn's
decision), and the EPYC composition pass when the box returns.
