# 2026-08-31 — retiring the §1.2 example corpus: the c/cpp reproduction sittings

The commit this records deletes the §1.2 example-corpus rows — their
hand-written pins, vary functions, field checks and per-shape drivers — from
the c, cpp, go, rust, cs and js runners. `generated/` is byte-unchanged, no
emitter is touched, and no code inside a surviving row's timed loop moves.
bench/LOCK's procedural control 2 requires the c/cpp legs to reproduce
in-sitting in any PR touching them.

Machine: M2 MacBook Air, macOS 15 (Darwin 25.6.0), Apple clang 21.0.0, no
pinning (macOS has no `taskset` worth trusting — BENCH-STANDARD §2.6). Full
c and cpp legs, `-O3`, binaries recompiled per leg from each tree.

Order in every sitting: **before → after → before again**, the second
before-tree run being the control that says whether the window itself was
stable. Sittings 2 and 3 extend that to **before → after → before → after**.

| file | tree |
|---|---|
| `2026-08-31-exrows-s1-{before1,after,before2}-{c,cpp}-arm64-macbook.csv` | sitting 1, both legs, 13:12–13:17 UTC |
| `2026-08-31-exrows-s2-{before1,after1,before2,after2}-cpp-arm64-macbook.csv` | sitting 2, cpp reproduction, 13:18–13:21 UTC |
| `2026-08-31-exrows-s3-{before1,after1,before2,after2}-c-arm64-macbook.csv` | sitting 3, c reproduction, 13:21–13:25 UTC |
| `2026-08-31-exrows-quick-ninelang-arm64-macbook.csv` | all nine legs, `run.sh --quick`, after tree |

`before` is `origin/main` at `ced73c1` (PR #199 merged); `after` is this
branch.

**Why three sittings.** A macOS media-indexing daemon held roughly one core
for the whole session. Sitting 1's cpp control came back with 11–19% spreads
on rows that normally read under 2% — over §2.3's 15% `noisy` threshold, so
those rows do not publish. The cpp leg was re-run (sitting 2) and the c leg
was re-run alongside it (sitting 3) with a doubled A/B/A/B order. The noisy
sitting-1 cpp CSVs are landed rather than dropped: a control that failed is
the record of why the leg was re-measured.

## THE FINDING: C's `bench_mixed write` moved +7%, and it is code layout

Every row below is byte-identical library code and byte-identical generated
code across the diff. The only thing that changed is how much source sits in
the same translation unit — `bench/c/bench_main.c` loses 480 lines,
`bench/cpp/bench_main.cpp` loses 535.

Sitting 3, c, median M msg/s:

| row | before1 | after1 | before2 (control) | after2 | after vs before |
|---|---:|---:|---:|---:|---:|
| bench_mixed write | 6.43 | 6.90 | 6.43 | 6.85 | **+7.4% / +6.4%** |
| bench_mixed round_trip | 3.48 | 3.43 | 3.44 | 3.42 | -1.3% / -0.7% |
| bitpacker write (k pass/s) | 57.0 | 56.8 | 56.5 | 56.4 | -0.4% / -0.2% |
| bitpacker read (k pass/s) | 53.7 | 53.6 | 54.1 | 54.1 | -0.2% / -0.1% |

The control reproduces within 0.9% on every row, so `bench_mixed write`'s
+7% is across the diff, not across the window — and it reproduces in both
A/B pairs. Sitting 1's c leg, an independent window, read +7.5% on the same
row with a 0.17% control.

Sitting 2, cpp, median M msg/s (the clean back-to-back pair — sitting 2's
first pair carried 8–22% spreads and does not publish under §2.3):

| row | before2 | after2 | delta |
|---|---:|---:|---:|
| bench_mixed write | 7.07 | 7.08 | +0.2% |
| bench_mixed round_trip | 3.46 | 3.37 | -2.8% |
| bitpacker read (k pass/s) | 87.4 | 84.9 | -2.8% |

**`bench_bitpacker` is byte-identical in both trees in both languages** —
the diff moves only its call site's position in the file. cpp's bitpacker
read still moves -2.8%. That is the negative control: source that provably
did not change, measuring differently because the TU around it shrank.

PR #199 measured the same class one merge earlier: C++ `bench_packet read`
-11% from removing ~580 lines of family-`rt` code, with C reproducing within
2.6%; then C's `testdata write` +21.4% and `probearray write` -7.1% from
removing the hand-coded Bench-corpus shapes, with C++ nearly still. This
commit removes a comparable volume again, and C is the leg that moves, on a
row with no relationship to what was deleted.

## Consequence for the ledger

**This commit is a #194 absolute-ledger SUB-ERA BOUNDARY.** Measurement
continuity breaks here for the c and cpp legs. The curve must not read C's
+7% as an improvement or cpp's -2.8% as a regression, because neither
happened: the diff deletes measurement code and touches nothing that runs
inside a timed loop of a surviving row.

Do not smooth it and do not chase it. Layout sensitivity on frozen code is
the expected finding, and this is the third time in one day that removing
dead harness code moved a frozen row by several percent in one language
while leaving the other alone.

## corpus_id

- `--quick` (the published table): **`6b213fbfa1a03a99`, unchanged**, on all
  nine legs. bench_mixed's golden and variant corpus are untouched.
- Full c/cpp sweep: `a4bc52bae5343e60` → `6b213fbfa1a03a99`. Expected and
  required — the full leg no longer loads the ten example goldens or
  `testdata/wire/real_packet.bin`, because no row reads them, so the full
  sweep's id now equals the `--quick` id. §5.3 rule 2 therefore refuses every
  ratio across this boundary, mechanically, which is the loud re-pricing
  §1.7 rule 3 demands. The goldens themselves are byte-unchanged and still
  gated by the conformance suite under `make test`.
