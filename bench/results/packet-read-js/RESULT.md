# JavaScript: the fixed read against its own packet read (identity lane)

**Verdict: FAIL**, in both sittings. The fixed form's identity read is slower
than the same leg's packet read of the same 64 logical records, each side
measured alone (BENCH-STANDARD §1.10, Glenn's ruling of 2026-09-11: slower is a
FAIL for that leg).

| sitting | order | `bench_fixed_read` ns/record (best / median) | `bench_mixed_read` ns/record (best / median) | spread fixed / packet | gate |
|---|---|---|---|---|---|
| 1 | table, then packet | 861.4 / 903.5 | 364.4 / 369.4 | 10.47% / 3.16% | **FAIL** |
| 2 (control) | packet, then table | 881.9 / 908.1 | 362.2 / 372.8 | 8.19% / 5.81% | **FAIL** |

Every number above is copied from the runners' own output in
[board.txt](board.txt); none is recomputed. Both spreads are under §2.3's 15%,
so both rows publish and the verdict stands.

## The sitting

- **Host:** hetzner1, x86_64, AMD Ryzen 7 PRO 8700GE, kernel 6.8.0-139-generic.
  Never the studio Mac.
- **Node:** v26.7.0. **serialize.js:** v1.4.2
  (de0591c9e6fddbbc5d4512b4558f091ffa2158ad), the sibling checkout at
  `SERIALIZE_JS=../serialize.js`. Packet runner in `NODE_ENV=production`
  (checks `contract`), the standard leg.
- **Pin:** `taskset -c 11` for both processes of both sittings.
- **Window:** sitting 1 2026-10-16T01:56:28Z–01:56:43Z, load 14.06 → 12.80
  (1-min); sitting 2 01:56:49Z–01:57:04Z, load 11.94 → 9.96. The host is a
  shared bench (other jobs on other cores, named in board.txt's `top` lines);
  the pinned core's busy-jiffy delta equals its total delta in both sittings,
  so core 11 was ours for the whole window. The load is declared, not hidden:
  this is not the quiet game-server window, and the verdict is not close
  (2.4x), so the load does not decide it.
- **Two processes.** The table runner imports no runtime and the packet runner
  imports the serialize.js sibling, so the two halves are two node processes
  run back to back by one script invocation on one host and one core. They
  therefore carry two `corpus_id`s:
  - `bench_fixed_read`: `066b2822b3e5d487` (the five paired table files,
    `--indexed --wire-dir bench/paired/corpus --variant-dir bench/paired/corpus`,
    exactly what bench/paired passes the js table leg);
  - `bench_mixed_read`: `fd0748942dc6ae09` (`testdata/wire/bench_mixed.bin` and
    `bench/corpus/variants/bench_mixed.variants.bin`).
  The paired corpus is derived from the packet variants (bench/paired, §1.9),
  so the 64 logical records are the same ones.
- **Iterations:** each runner's fixed standard count, 1 discarded warmup plus 7
  measured runs in-process (§2.1): table 400000 records per run (bytes_per_op
  1264.296875, one LOAD per 64 records), packet 4000000 records per run
  (bytes_per_op 438). Every run is over the 200 ms floor. Interleaving (§2.4)
  is across the two sittings: the order of the two processes is reversed in
  the control.
- **Gates before the clock:** the table runner's four (layout is the
  corpus's, the file loads, it writes back byte for byte, reused storage
  round-trips) plus the tolerant corpus's framing; the packet runner's two
  (variant 0 is the wire golden; all 64 variants decode flat, agree with the
  runtime tier field for field, and re-encode byte for byte). Both runners
  also prove after the clock, untimed, that the timed destination still writes
  back to what was read. `--gate --plan-read` passes on both runners on
  hetzner1.

## The cause, from a timing split

A scratch split (not committed: it re-imports the generated modules and times
the pieces of `FixedTableFixedLoad` separately, pinned to the same core, two
runs) on hetzner1, 2026-10-16T01:57Z:

| arm | run 1 best / median | run 2 best / median |
|---|---|---|
| A: the whole shipped LOAD | 899.6 / 915.6 | 901.6 / 926.2 |
| B: the plan run alone (the identity COPY of the 1236-byte body into the plan image, 32-bit DataView lanes) | 414.6 / 420.8 | 423.7 / 433.0 |
| C: the object projection alone (`FixedTableFixedDecode` from the image into the JS objects) | 392.4 / 399.6 | 403.0 / 414.4 |

(ns/record.) The JS read is **two full passes over the record**: it copies the
whole body into the plan's image and then projects the image into JavaScript
objects. Either pass alone is already slower than the packet wire's entire
read (about 363 ns), which decodes the bits straight into the objects in one
pass. So this is not noise and not one hot spot: the read pays a copy the C
legs read in place, and its projection alone costs more than the whole
bitpacked read.

What would move it, named for the follow-up and not done here (it is
generated code, outside this card's PATHS):

1. on the identity lane, project straight from the source view at the
   record's offset and skip the copy into the image (the hole list is already
   empty; arm B is the price of not doing this);
2. then profile the projection itself (`node --cpu-prof` on arm C, and
   `--trace-deopt` on `BenchMixedFixedDecode`) for per-field DataView calls,
   text validation and nested-object stores; even with the copy gone, arm C
   alone does not beat the packet read.

## The plan lane

`gates/packet-read/js/plan` stays `:unknown`. It needs BENCH-STANDARD §1.10's
lineage corpus (items 4 to 6 there: the VOLD schema's records written by the
C++ dump, re-pinned under `bench/paired/corpus/`, and the lock) and the paired
driver's `-mode plan-read`, and neither is on this branch. The Rust card's
corpus (`bench/tables/rust/lineage/` at 337aaacf5: the VOLD schema, the
969-byte-record `bench_lineage.bin` and `.layout`, and a `schema.lock` with the
VOLD layout before the identity) has the §1.10 shape, but it is not on this
branch, and the JS codec cannot read it as generated: this tree's
`generated/bench/paired/js/FixedTableTable.js` knows ONE layout (the identity,
`FixedTableFixedKnown` has one entry), so a JS plan read needs the paired JS
codec regenerated against that lock (`generated/`, the Makefile's paired
generation), which is outside this card's PATHS. So the JS plan lane is
reported absent and no second corpus is authored. Proposed as a card: promote
that corpus to `bench/paired/corpus/` as §1.10 items 1 to 7 name, generate
every leg's paired codec against its lock, and give the paired driver its
plan-read mode; then the JS `bench_plan_read` row is the same loop as
`bench_fixed_read` over `bench_lineage.bin`.

## The roadmap

`gates/packet-read/js/identity` is **not** flipped: the brief flips it only on
two FIXED-FASTER sittings, and both are FAIL. docs/roadmap.sexp and ROADMAP.md
are unchanged.
