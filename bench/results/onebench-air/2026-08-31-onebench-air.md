# 2026-08-31 — one bench, one shape: the c/cpp reproduction sitting

The commit this records deletes the hand-coded `bench_packet` / `bench_ints` /
`bench_bits` measurement code from all nine runners, leaving the data-driven
`bench_mixed` driver as the only Bench-corpus measurement. bench/LOCK's
procedural control 2 requires the unchanged shapes' c/cpp rows to reproduce
in-sitting in any PR touching the c/cpp legs.

Machine: M2 MacBook Air, macOS 15 (Darwin 25.6.0), Apple clang 21.0.0, no
pinning (macOS has no `taskset` worth trusting — BENCH-STANDARD §2.6). One
sitting, 12:04–12:09 UTC, full c and cpp legs, `-O3`, `--reuse-build` over
binaries compiled back to back from both trees.

Order: **before → after → before again**. The second before-tree run is the
control: it says whether the window itself was stable, which is the only thing
that separates a real shift from drift.

| file | tree |
|---|---|
| `2026-08-31-onebench-before1-{c,cpp}-arm64-macbook.csv` | PR #199 head (`6eaa333`) |
| `2026-08-31-onebench-after-{c,cpp}-arm64-macbook.csv`   | this commit |
| `2026-08-31-onebench-before2-{c,cpp}-arm64-macbook.csv` | PR #199 head again, the control |
| `2026-08-31-onebench-quick-ninelang-arm64-macbook.csv`  | all nine legs, `run.sh --quick`, after tree |

## THE FINDING: C moved this time, and it is code layout again

Every row below is byte-identical library code and byte-identical generated
code across the diff. `generated/` does not change at all — no schema type was
touched — and `serialize.h` / `serialize.c` are untouched. The only thing that
moved is how much source sits in the same translation unit.

Rows whose after-tree delta exceeds the control's, median M msg/s:

| lang | row | before1 | after | before2 (control) | after vs before1 | control vs before1 |
|---|---|---:|---:|---:|---:|---:|
| c   | testdata write   | 35.5 | 43.1 | 35.6 | **+21.4%** | +0.2% |
| c   | probearray write | 81.3 | 75.5 | 81.3 | **-7.1%**  | +0.0% |
| c   | chat write       | 121.2 | 127.1 | 121.6 | +4.9%   | +0.3% |
| cpp | probearray write | 77.6 | 81.6 | 76.9 | +5.2%      | -0.9% |
| cpp | testdata read    | 68.9 | 66.2 | 68.6 | -3.9%      | -0.4% |

Every other c and cpp row reproduces within ~2%. One row moved in the window
rather than across the diff and is called out as such: **c bench_mixed write**
reads -9.9% after, but the control reads -8.2%, so the before tree did not
reproduce itself there — that is window drift, not the diff.

PR #199 measured the mirror image of this: **C++** `bench_packet read` -11% and
`bench_bits read` -9.6% from removing ~580 lines of family-`rt` code, with C
reproducing within 2.6%. This commit removes a comparable volume and **C** is
the leg that moves, on rows with no relationship to what was deleted. That is
the signature of translation-unit code layout, not of a serializer change.

## Consequence for the ledger

**This commit is a #194 absolute-ledger SUB-ERA BOUNDARY.** Measurement
continuity breaks here for the c and cpp legs. The curve must not read the
movement above as a regression or an improvement, because neither happened:
the diff deletes measurement code and touches no code that runs inside a timed
loop of a surviving row.

Do not smooth it and do not chase it. Layout sensitivity on frozen code is the
expected finding — it is the same class as BENCH-STANDARD §2.9's F1, and it is
now the second time in one day that removing dead harness code moved a frozen
row by ~10% in one language while leaving the other alone.

## corpus_id

- `--quick` (the published table): **`6b213fbfa1a03a99`, unchanged.** bench_mixed's
  golden and variant corpus are untouched.
- Full sweep: `8e0f582f66343ad0` → `a4bc52bae5343e60`. Expected and required —
  the full leg no longer loads `testdata/wire/bench_{packet,ints,bits}.bin`,
  because no row reads them. The goldens themselves are byte-unchanged and
  still gated by the conformance suite under `make test`.
