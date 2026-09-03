# 2026-09-03 — the Rust leg joins the tables bench — PAIRING CHECK, not a sitting

**Read the label first.** This board is a PAIRING CHECK on a MacBook Air (M2):
an interactive desktop, no core reservation, three other workers building on
the same machine — one of them holding a core at 100% for the whole window —
macOS so no `taskset`. It says the three legs agree, that the gates fire, and
roughly where the numbers stand. **It certifies nothing**, and no number here
may be quoted as the tables layer's performance.

The certifying sitting is the box under the estate's bench rules — core 15,
the server stopped, not live, one bench at a time, blessed per run. The target
is ready for it: `bench/tables/run.sh --rounds 7 --tag <sitting>` is the whole
command.

Raw: `2026-09-03-pairing-rust-arm64-macbook.csv`.
Corpus: `bench/corpus/BenchTable.schema`, `corpus_id 2f7567e1e25ba918`,
2391 wire bytes per record, 64 records.
Rounds: **7 interleaved** (§2.4) — every leg once per round, so every leg saw
the same window of the machine's mood, which is what a shared laptop most
needs.

## The board

| lang | path | M msg/s (median) | MB/s | ratio to C++ |
|---|---|---:|---:|---:|
| cpp | write | 0.4615 | 1052.4 | 1.00 |
| **rust** | **write** | **1.0306** | **2350.1** | **2.23** |
| cs | write | 0.3385 | 771.8 | 0.73 |
| cpp | round_trip | 0.3285 | 749.1 | 1.00 |
| **rust** | **round_trip** | **0.4949** | **1128.5** | **1.51** |
| cs | round_trip | 0.2130 | 485.7 | 0.65 |

`read` is derived (round-trip minus write) and is not a row: cpp 1.140,
rust 0.952, cs 0.554 M msg/s — **rust/cpp 0.84 on read**.

Every leg's per-round spread was 0.0–0.1%, which is the interleave doing its
job rather than the machine being quiet: the background core never moved
between rounds.

The rust/cpp ratio is printed across a linkage difference in the same sense
the cs/cpp one is, and it is the smaller of the two: cpp compiles the
generated table codec inline into one translation unit and links no runtime at
all; rust compiles it as one crate in a graph the bench monomorphizes over,
with `lto = true` and `codegen-units = 1`, and links no runtime either — the
Rust table modules carry no serialize dependency. Both legs are `-O3` with the
whole codec visible to the optimizer, which is as near as two languages get.

## The one finding worth the owner's attention

**The Rust write path is 2.23x the C++ one on identical bytes**, and that is a
C++-side finding rather than a Rust-side win. The previous board on this
corpus already flagged it from the other direction: *"C++'s write path pays
2.90x per byte … 2.90x is worth an explanation before the ladder's fixed-table
clause is called satisfied."* A second implementation of the same wire, at the
same optimization level, with the same gates, doing the job at 2.23x the rate
localises that cost to **the C++ writer** and not to the wire.

**The hypothesis, stated as one and not as a conclusion.** Both writers keep
`offset` and `capacity` as members of a struct whose `buffer` is a
`uint8_t *`. In C++ a `uint8_t *` may alias anything, so every store through
`buffer` obliges the compiler to reload `offset` and `capacity` from memory
before the next `put`; in Rust `&mut TableWriter` and the `&mut [u8]` inside it
are `noalias`, so the offset lives in a register across the whole body. That
is the same mechanism the estate has already measured once — the round log's
`restrict` finding, +152% composed, and the note that *"Rust had restrict's
benefit all along (`&mut` is noalias)"*.

**It is untested here, and testing it is a C++-side pass, not this one.** The
experiment is one line each way: make `raw` take `offset` into a local, or mark
`buffer` `__restrict`, rebuild the C++ leg alone, and re-take this window. If
the gap closes, the finding is the aliasing; if it does not, the hypothesis is
wrong and the profile is the next instrument. `internal/codegen/cpptable` is
not this branch's file to edit, and a port branch quietly optimising the
reference leg would make the comparison it exists to publish meaningless.

## The Rust read path, and the one change made on a measured conviction

**Rust reads at 0.84x of C++**, so the read arm is the port's own item rather
than the reference's. One change was made to it inside this window, against a
banked prediction:

- **Prediction, written before measuring**: reading a multi-byte scalar as ONE
  range-checked chunk rather than as N checked byte indexes moves `round_trip`
  by 5–15% and `write` by ~0.
- **Measured, paired, same sitting**: `round_trip` 0.4694 → 0.4949 M msg/s
  (**+5.4%**), derived read 0.862 → 0.952 (**+10.4%**), `write` 1.0307 →
  1.0306 (**0.0%**).

The prediction held in direction and magnitude on all three. The change is
`first_chunk::<N>()` in `TableReader::get16/get32/get64` — still safe Rust,
still bounds-checked, one check and one load instead of N checks and N loads —
and it moved no byte of any golden, which the conformance matrix and the
forgery fuzzer both re-proved after it.

**What is NOT closed**: the remaining 1.19x on read. It is inside the ladder's
*"as close to its equivalent type as the neutral wire allows"* in spirit and
it is not explained, so it is named here rather than declared fine. The next
instrument is a profile, not another guess.

## What moved in the tree after this window, and why it was not re-taken

Three changes landed after this board and before the port did: the generated
Rust made clippy-clean (a bool's elision test reads as the bool, three
`% n != 0`s became `is_multiple_of`, the finite test became `is_finite`), the
block descriptors' row-walk columns, and the clamp elision main had already
ruled for the other two backends (#342 — a declared bound AT the storage limit
emits no comparison). **None of them is on the wire path this board measures**
except the last, which can only remove work, so the rust rows here are
conservative rather than stale.

**It was not re-taken because the machine had four workers on it** — a
sibling's dotnet, two Go test binaries and a C soak — and a run in that window
would have replaced a pairing check with a worse one. The same paragraph the
board opens with is why: this is a laptop, and the box sitting is what
certifies. The re-take is one command.

For the record, the clippy pass WAS paired before it landed, alternately in one
window, old binary against new: write 0.73 both, round_trip 0.34–0.36 both, new
marginally ahead. That pairing is in the commit that made the change.

## What the numbers do not cover

The same split the corpus's own README states: this leg is the tolerant wire
and nothing else. The block form's fill and read, the cook's open cost and the
JSON walk are not here by design — the first two are C++/C# numbers taken in
the game on real render data, and the third is tooling.
