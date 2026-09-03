# 2026-09-03 — the tables bench gains a JavaScript row — PAIRING CHECK, not a sitting

**Read the label first.** This board is a PAIRING CHECK on a MacBook Air (M2):
an interactive desktop, no core reservation, macOS so no `taskset`. It says the
seven legs agree, that the gates fire, and roughly where the numbers stand.
**It certifies nothing**, and no number here may be quoted as the tables layer's
performance.

The certifying sitting is the box under the estate's bench rules — core 15, the
server stopped, not live, one bench at a time, blessed per run. Nothing about
registering this leg is in the way of it: `bench/tables/run.sh --rounds 7 --tag
<sitting>` is still the whole command, and it now renders seven rows.

Raw: `2026-09-03-pairing-js2-arm64-macbook.csv`.
Corpus: `bench/corpus/BenchTable.schema`, `corpus_id 2f7567e1e25ba918`,
2391 wire bytes per record, 64 records. Commit `8b6c125`.
Protocol: seven interleaved rounds (§2.4), median across them; the spread column
is (max − min) / median ACROSS rounds, and it is the MACHINE's — each leg's own
within-round spread reads 0.0%.

## The board

| bench | path | lang | B/msg | M msg/s (median) | MB/s | spread | ratio to C++ |
|---|---|---|---:|---:|---:|---:|---:|
| bench_table | write | cpp | 2391 | 2.366 | 5656.6 | 4.5% | 1.000x |
| bench_table | write | c | 2391 | 2.206 | 5274.7 | 14.7% | 0.932x |
| bench_table | write | rust | 2391 | 2.060 | 4926.2 | 19.8% | 0.871x |
| bench_table | write | java | 2391 | 1.550 | 3705.5 | 7.4% | 0.655x |
| bench_table | write | js | 2391 | 0.372 | 890.2 | 23.7% | 0.157x |
| bench_table | write | go | 2391 | 0.335 | 802.0 | 12.1% | 0.142x |
| bench_table | write | cs | 2391 | 0.333 | 796.2 | 7.2% | 0.141x |
| bench_table | round_trip | cpp | 2391 | 0.811 | 1938.2 | 6.6% | 1.000x |
| bench_table | round_trip | c | 2391 | 0.803 | 1920.9 | 10.0% | 0.990x |
| bench_table | round_trip | rust | 2391 | 0.657 | 1570.6 | 6.3% | 0.810x |
| bench_table | round_trip | java | 2391 | 0.445 | 1064.1 | 15.9% | 0.549x |
| bench_table | round_trip | go | 2391 | 0.218 | 521.0 | 10.0% | 0.269x |
| bench_table | round_trip | cs | 2391 | 0.209 | 500.2 | 3.1% | 0.258x |
| bench_table | round_trip | js | 2391 | 0.201 | 479.9 | 22.5% | 0.248x |

**JavaScript is 0.157x of C++ on write and 0.248x on round-trip** — the fastest
of the three managed legs on write, a shade behind them on round-trip. That is
the number, reported rather than defended.

## A DISCARDED SITTING, said rather than averaged away

An earlier sitting the same day (seven rounds, 10:23Z) read 0.126x / 0.196x for
JavaScript. It is **discarded whole** rather than averaged in: three of its ten
rows crossed §2.3's 40% INVALID line (cs write 40.6%, cs round_trip 40.3%, go
round_trip 41.7%), because the machine was carrying an hour-long soak and five
other builds at the time. The repo's own rule is to discard a contaminated run
whole and never to average one, and this is that rule applied to my own numbers.
What the two sittings agree on is the ORDERING and the magnitude; what moved is
every leg's absolute figure — C++ read 1.121 M msg/s on write there and 2.366
here — which is the machine and not the codec, and is the whole reason a laptop
board certifies nothing.

**The JavaScript rows carry this board's widest spreads** (23.7% and 22.5%,
against C#'s 7.2% and 3.1%). Both are inside §2.3's noisy band and clear of the
invalid line, and the shape is what a JIT does: a round's first iterations run
at a lower tier than its last, and seven short rounds sample that transient
seven times. A quiet box with longer rounds is where that goes away, and it is
the same box the certifying sitting waits for.

## What the number is, and what it is not

**The reading tier's bar is honest, not "same speed."** The ladder's
`same speed, or not significantly slower` is the bar for a language that can be
held to it — one that compiles a struct field access to an offset. C, Rust and
Java make it or come close. JavaScript compiles a field access to a property
load on an object whose shape the JIT has to have settled, runs under a JIT
rather than an optimizing AOT compiler, and has no exact 64-bit integer that is
not an allocation. A number wide of C++ here is the language's cost, stated; it
is not a defect concealed, and it is not a trade anyone licensed for the FIXED
table's per-byte codec, which is a separate claim the ledger holds against C++
alone.

**What IS worth reading is the shape of the gap.** JavaScript sits ahead of C#
and Go on WRITE and behind them on ROUND-TRIP, on a corpus carrying six 64-bit
fields per record (`session_id`, `nonce`, `world_time`, `frame_tick` as
`bits(48)`, `wide_key`, `flux`). Each of those is one BigInt per field READ and
none on write, which is exactly where the legs part company: the write ratio is
one runtime against another, and the round-trip ratio carries the language's one
unavoidable allocation on top of it. That is consistent with what
`make tables-js-alloc` measures directly — zero bytes per iteration on a table
with no 64-bit field, and one BigInt per 64-bit field where there is one.

**A per-BYTE claim is not made here.** §12.1's block form is where the per-frame
JavaScript path lives and it is not this leg; the block form's read allocates
nothing at all, which this leg's corpus cannot show because the tolerant wire is
not the block form.

## What moved to get here

Three allocations found by `make tables-js-alloc` before this board was taken,
on lines that would have shown up in exactly this measurement:

- a block array's row accessor read its `offset_of` with `getBigUint64` — one
  BigInt per row addressed (247 B/iter);
- the cook's `At` read its delta the same way — one per edge followed
  (37 B/iter);
- **a float field crossed a call boundary** — `w.putF32(x)` and `r.getF32()` —
  which V8 boxes into a heap number, sixteen bytes per call, unless it inlines
  the callee. The generated body reads and writes a float through the view
  itself now, so no double crosses a call on a codec path.

The first two are the accelerators' and not this leg's path; the third is
squarely on it, and both of this board's JavaScript rows are measured after it.
