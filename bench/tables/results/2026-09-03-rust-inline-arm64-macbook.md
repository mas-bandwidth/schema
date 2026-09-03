# 2026-09-03 — the Rust leg, against the inlined C++ arm — PAIRING CHECK, not a sitting

**Read the label first.** A PAIRING CHECK on a MacBook Air (M2): an interactive
desktop, no core reservation, macOS so no `taskset`. It says the three legs
agree, that the gates fire, and roughly where the numbers stand. **It certifies
nothing.** The certifying sitting is the box under the estate's bench rules.

Raw: `2026-09-03-rust-inline-arm64-macbook.csv`.
Corpus: `bench/corpus/BenchTable.schema`, `corpus_id 2f7567e1e25ba918`,
2391 wire bytes per record, 64 records.
Rounds: **7 interleaved** (§2.4). The window was quiet — no sibling worker on
the machine — and every leg's per-round spread was 0.0%.

## The board

| lang | path | M msg/s (median) | MB/s | ratio to C++ |
|---|---|---:|---:|---:|
| cpp | write | 2.4107 | 5497.0 | 1.00 |
| **rust** | **write** | **2.0148** | **4594.1** | **0.84** |
| cs | write | 0.3193 | 728.1 | 0.13 |
| cpp | round_trip | 0.8226 | 1875.8 | 1.00 |
| **rust** | **round_trip** | **0.6211** | **1416.2** | **0.76** |
| cs | round_trip | 0.2076 | 473.5 | 0.25 |

`read` is derived (round-trip minus write) and is not a row: cpp 1.249,
rust 0.898, cs 0.593 M msg/s — **rust/cpp 0.72 on read**.

## THE PREVIOUS BOARD'S FINDING WAS RIGHT AND MY ATTRIBUTION OF IT WAS WRONG

The board this replaces had Rust's write at **2.23x the C++ one** and said so
was a C++-side finding rather than a Rust-side win. That half held. The
hypothesis attached to it did not:

> a `uint8_t *` may alias the writer's own `offset` member in C++ and a
> `&mut [u8]` may not in Rust … **Untested, and testing it is a C++-side pass.**

It was tested — #343/#350 — and the answer is that the aliasing was the
MECHANISM but not the CAUSE. The cause was **the inliner's budget**: clang ran
out partway through the big save body, so the nested bodies and fifteen `put32`
/ `put64` calls stayed out of line, and *out of line* is where the aliasing
rule makes the cursor round-trip through memory. Force the primitives and the
fixed-class bodies inline and the reload storm is gone. I named a language
difference where the answer was a missing compiler directive, and a hypothesis
that names the wrong layer is wrong even when its mechanism is real.

With that directive in the C++ arm, C++ leads every row.

## The same rule, applied to Rust, with a prediction that MISSED

The C++ rule is force-inline on the writer/reader primitives and on the
fixed-class `SaveBody`/`LoadBody` — the variable class excluded, because it
recurses. Rust's mirror is `#[inline(always)]` on the same set; the recursion
guard is the same class line, and this backend emits no variable-class wire
surface at all (§11), so there is no recursive body here to force.

**Predicted, written down before measuring**: write +10% to +60%, round_trip
+5% to +40% — far below C++'s 4.35x, *because Rust's `&mut` is already noalias,
so the cursor never round-tripped and only call overhead and store merging are
left*. And explicitly: **if Rust moves more than 2x, that reasoning is wrong.**

**Measured**, five alternating rounds of old binary against new, one quiet
window, medians:

| row | before | after | move |
|---|---:|---:|---:|
| write | 1.031 | 2.091 | **+103% (2.03x)** |
| round_trip | 0.489 | 0.659 | +35% |
| read (derived) | 0.944 | 0.962 | +1.9% |

**Round_trip landed inside the band. Write did not — +103% against a predicted
ceiling of +60%, and past the 2x line I set as the refutation.** A
wrong-magnitude prediction is a refutation, so: the noalias reasoning was only
partly right. Being noalias did make Rust's out-of-line case better than C++'s
— that is why Rust led the old board by 2.23x — but it left far more on the
table than I predicted. Flattening the body is worth about a doubling on its
own, from call overhead and from adjacent constant framing bytes merging into
single stores, and I under-weighted both.

The read row barely moving (+1.9%) is consistent and is the check on that
reading: a read body's work is a per-field match, not a stream of constant
stores, so there is little for flattening to merge.

**What it cost**: the widest unit's release compile went 1.16 s → 1.83 s
(+58%), same machine, cold target, median of the runs after a warm one. That is
the price of the directive and it is paid at build time, once.

## What is open

**Rust is at 0.84 / 0.76 / 0.72 of C++ on write, round trip and read.** The
ladder's bar is *same speed, or not significantly slower*, and whether ~1.2x
–1.4x clears it is the owner's call rather than mine. What I can say is that it
is not explained: the force-inline rule is now the same on both sides, so the
remainder is something else. **The next instrument is a profile, not another
prediction** — this board has already spent one guess badly, and the doctrine's
order is unit test, soak, profile, then optimize on a conviction.

## What moved after this window

Three changes landed after the board and before the port did, and **none is on
the wire path it measures**: §11's claim in the Rust constant space (a
front-end refusal), the cook runtime's move to its own per-unit module
(#351's rule — a file's home, not a byte of a codec), and the registry
bookkeeping that went with both. The generated `<name>_measure`,
`<name>_save_body` and `<name>_load_body` are byte-identical across all three.

It was not re-taken because the machine had two workers at 100% by then, and
the board's own first paragraph is why that matters. The re-take is one
command.

## What the numbers do not cover

The tolerant wire and nothing else, by the corpus's own design. The block
form's fill and read, the cook's open cost and the JSON walk are not here: the
first two are C++/C# numbers taken in the game on real render data, and the
third is tooling.
