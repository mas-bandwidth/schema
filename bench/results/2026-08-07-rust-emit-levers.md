# 2026-08-07 — rust-emit-levers: the two proven emitter levers adopted in the Rust backend

The Rust backend adopts the two levers the C++ emitter proved this week:
generation-time bound folding (schema #8's mechanism) and bulk-bytes for
statically byte-aligned `[N]u8` arrays (schema #7's reuse design, on
serialize.rs #20's `write_bytes` surface). Plus the cheap config-only
experiment the earlier profile queued: LTO / codegen-units on bench/rust.

Wire identity held everywhere: `testdata/wire` untouched under forced
regeneration, the bench runner's golden self-check gate passed on every run
of every binary, and `test/rust`'s into-path wire gate is green.

## What changed

- **Fold (86 write sites)**: ranged ints (32/64 families), counted-array
  counts, string/bytes lengths, enums, message/object tags, quantized
  components, projected floats now emit the offset from min in a
  generation-time literal bit count through `serialize_bits`/
  `serialize_bits64` — no runtime `bits_required`, no min/max parameter
  traffic, the 32/64 dword split resolved by the generator. Reads stay on
  the runtime methods (serialize #25: the branchless reader has nothing to
  gain). Deliberately NOT const-generic call forms — C++ #8 measured the
  template twin of that design and shared instantiations got outlined at
  -10..-33%; the Rust hazard is the same shape, so the emitter emits
  literals, never generics.
- **Bulk-bytes (1 corpus field, write + read)**: `TestData.fixed_bytes
  [17]u8` (declared after an explicit `align`) takes
  `WriteStream::write_bytes(&value.fixed_bytes)` /
  `stream.serialize_bytes(&mut value.fixed_bytes)` instead of the 17x
  per-byte loop, gated by `ir.AlignedFixedByteArrays` — the same
  target-independent proof C++ consumes, byte-identical by construction
  (the internal align is zero bits at a proven boundary).

## Predictions banked before measuring

| prediction | outcome |
|---|---|
| testdata read +60..+120% (bulk-bytes; C++ analog +70..80%) | **+49.8%** — direction confirmed, magnitude REFUTED low (see below) |
| testdata write +5..+15% | **+9.2%** CONFIRMED |
| fold on other write rows neutral-to-small-positive (0..+5%) | CONFIRMED neutral (±1.1% everywhere) |
| non-testdata reads unchanged | CONFIRMED (±1.0%) |
| batch +0..+5% | REFUTED: -4.6% write / -3.0% read — attributed to binary layout, see below |
| no regressions beyond spread | HELD on per-message rows; batch inside its demonstrated layout band |

## M2 (Apple clang / rustc 1.97.1, ambient desktop load only) — paired interleave x3, median-of-7 per run, msgs/sec

Method: `main` (bb077af) vs `rust-emit-levers` (8956046) binaries built at
the default release profile, run alternately three times each from
`bench/rust` in one sitting; each run is the runner's own warmup +
median-of-7, golden-gated. Committed CSVs:
`2026-08-07-rust-emit-levers-{before,after}-arm64-m2.csv` (three runs per
file; the table takes the median across runs).

| bench | path | main | levers | delta |
|---|---|---:|---:|---:|
| testdata | write | 14.92M | 16.29M | **+9.2%** |
| testdata | read | 10.10M | 15.13M | **+49.8%** |
| shipcreate | write | 66.21M | 66.93M | +1.1% |
| probearray | write | 39.21M | 39.65M | +1.1% |
| probebits | read | 127.58M | 128.83M | +1.0% |
| all other per-message rows | | | | within ±0.8% |
| message_batch | write | 71.58M | 68.27M | -4.6% |
| message_batch | read | 49.82M | 48.30M | -3.0% |

**The testdata read refutation, plain**: banked +60..+120% from the C++
analog (+70..80%); measured +49.8%. Against the same-sitting C++ context
(25.60M / 24.74M across the two quiet-window context runs, appended to
the LTO CSV), rust testdata read moves from ~253% of C++ time to ~168%
— the gap narrowed by a third but read remains the open Rust column, as
the gap ledger already records: the read path has never had a read-shaped
pass, and the residual is not the byte loop anymore.

**The batch counter-movement, attributed**: the batch READ loop executes
byte-identical decode code in both builds (no read function of any batch
message type changed), yet moved -3.0% repetition-stable — that is binary
layout, the exact phenomenon already on record for this bench (v3: ±20%
same-binary swings; schema #10: batch write +15% on a byte-identical write
loop). The write's -4.6% sits inside the same envelope, and the
per-message benches of the very message types the batch carries (chat
-0.6%, test +0.1%) show the fold itself is not the cause.

**Why the fold measured neutral where C++ gained** (the honest reading,
with the `nm` evidence): the base binary DID carry outlined
`WriteStream::serialize_int` / `serialize_int64` instantiations — the
Rust twin of the outlined call C++ #8 killed — but the hot generated
writes had already inlined and constant-folded them (`#[inline]` landed in
serialize.rs #19), so the outlined copies served only cold paths. The
levers binary no longer contains either symbol: the fold removes the
outlined-call hazard structurally instead of removing a hot cost this
host was still paying. Emitted code is also simpler (no dead range
plumbing for the optimizer to chew), which is worth having at zero
measured cost.

## LTO / codegen-units experiment (bench/rust, config-only)

Predictions banked before measuring: thin LTO +0..+10% best on tiny
messages; fat ≈ thin; `codegen-units = 1` alone ≈ neutral (a release
build already runs per-crate ThinLTO across its own CGUs); combined ≈
LTO alone.

Method: six `[profile.release]` variants of bench/rust built at the
levers tree — default (lto off, cu 16), `lto="thin"`, `lto=true`,
`codegen-units=1`, thin+cu1, fat+cu1 — interleaved x3 reps in a verified
quiet window (sibling lanes idle), each run the runner's own
median-of-7, golden-gated. CSV:
`2026-08-07-rust-lto-experiment-arm64-m2.csv` (all 18 runs; every delta
below is recomputable from it).

**Outcome: the thin-LTO prediction is REFUTED — no variant wins
coherently, and bench/rust keeps the default profile.**

- Every variant lands mixed ±2..4% cells: e.g. thin takes chat write
  +2.2% and inputpacket +2.4% but pays ship_shallow write -3.2% and
  probe_header write -2.9%; fat pays probe_header write -4.3%; cu1 pays
  testdata write -4.1% and shipcreate write -4.0% while taking chat
  +3.8%. No column moves the corpus median.
- The only large positive cells are message_batch write (+7.7% thin,
  +9.6% cu1, +8.3% fat-cu1) — with 11..14% same-side spreads, on the row
  already demonstrated to swing on binary layout between byte-identical
  binaries. Not credited, per the layout-noise law.
- One repetition-stable regression worth naming: fat-cu1 puts
  ship_shallow read at **-14.8%** (B spread 1.2%) — fat LTO plus one CGU
  is not a free lunch on this corpus.
- testdata (the row with the newest code) is flat across all six
  variants (±1.6% max) — the bulk-bytes and fold paths need no LTO help.

**The reading**: the earlier profile's concern — generated wire
functions get no cross-crate caller inlining without LTO — does not
bind on the current tree: serialize.rs #19 + schema #5 put `#[inline]`
on the runtime methods and every generated wire function, and those
hints already deliver the cross-crate inlining that matters. LTO has
nothing left to buy on this corpus, on this host. Guidance for
consumers of the generated crate (documented in bench/rust/README.md):
the crate does not need LTO to perform; choose your profile on your own
whole-program evidence, not on this crate's account.

## Verification

- `make SERIALIZE=../serialize test` green at 8956046: all four C++
  binaries (incl. wire goldens + oracle), go/rust/cs conformance runners
  (rust includes the into-path wire gate), `go test ./...`.
- Source goldens re-pinned via `make update-goldens`; `testdata/wire`
  untouched (checked after forced regeneration).
- Every bench run above passed the runner's own golden self-check before
  producing numbers.
