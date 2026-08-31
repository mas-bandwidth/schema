# BENCH-STANDARD.md — the serialize estate profiling standard

**Status:** normative. Lives at `schema/bench/BENCH-STANDARD.md`.
**Governs:** `schema`, `serialize`, `serialize.c`, `serialize.go`, `serialize.rs`, `serialize.cs`.
**Relationship to `STANDARD.md`:** each repo's `STANDARD.md` is the wire contract — what
bytes mean. This is the measurement contract — what a number means. Both are
cross-repo, both are pinned, both refuse rather than guess.

---

## §0 Why

Every cross-language number published by this estate today is either wrong or
unmeasurable, and always for the same reason. Six harnesses, six methodologies:

| | corpus | statistic | flags | linkage | verifies work | records build |
|---|---|---|---|---|---|---|
| `schema/bench` | 12 generated msgs | median-of-7 | `-O3`, no fast-math | generated `inline` | **yes** | **yes** |
| `serialize/bench.cpp` | 49 B packet + 3 shapes | best-of-5 | `-O3 -ffast-math` | external comdat | no | no |
| `serialize.c/bench.c` | same 49 B packet | best-of-5 | **`-O2`** | file-`static` | no | no |
| `serialize.rs/throughput.rs` | same 49 B packet | best-of-5 | opt-level 3 | single crate | no | no |
| `serialize.go/bench_test.go` | **133 B packet** | benchstat mean | Go default | same package | no | no |
| `serialize.go/bench/cpp/bench.cpp` | same 133 B packet | best-of-5 | `-O3`, **lib v1.4.3** | external comdat | size only | version only |

The consequences, all measured:

- Rust reported stream read **17878 MB/s**, C reported **4581** on the same machine
  for the *same source-level packet*. The 4x is `-O3`-single-crate versus
  `-O2`-across-a-TU-boundary, plus a reader contract difference. It was published
  as a language result.
- Adding an anonymous namespace to `bench.cpp` — one keyword, zero library change —
  moved C++ reads **187.2 → 368.0 M packets/s**. The published table was comparing
  linkage.
- At `-O3` the C-vs-C++ read row **inverts** with no source change. The table was
  also comparing flags.
- C's write function has two call sites, one in untimed setup, so GCC cannot delete
  it and it drops into the 15-instruction auto-inline budget. C's read function has
  one. The table was also comparing harness structure.
- "Go is ~2x behind C" is to this day **unmeasured**. The Go agent correctly refused
  to compute the ratio, because the two corpora are unrelated.

Underneath all five of those is one variable:

> **Every language's throughput depends on the serialize chain inlining end to end,
> and not one harness reports whether it did.**

Measured on the M2 today, with `go build -a -gcflags=-m=2` in `serialize.go`:

```
bitpacker.go:270:  can inline    (*BitReader).tryReadBits        cost  80   budget 80   <-- zero headroom
write_stream.go:116: can inline  (*WriteStream).SerializeBits    cost  80   budget 80   <-- zero headroom
read_stream.go:63:  can inline   (*ReadStream).SerializeBits     cost  73   budget 80
write_stream.go:81: can inline   (*WriteStream).tryWriteBits     cost  64   budget 80
bitpacker.go:311:  CANNOT inline (*BitReader).ReadBits           cost  90   budget 80   <-- already over
bitpacker.go:85:   CANNOT inline (*BitWriter).WriteBits          cost  93   budget 80   <-- already over
read_stream.go:72: CANNOT inline (*ReadStream).SerializeBits64   cost 278   budget 80
write_stream.go:125: CANNOT inline (*WriteStream).SerializeBits64 cost 268  budget 80
```

Two functions sit at exactly 80 of 80. One added line silently halves throughput
and no test fails. Two more are already off the cliff and nobody noticed.

A harness that does not report inlining is reporting noise. That is the gap this
standard exists to close.

---

## §1 The corpus

### §1.1 One definition, in schema, generated

Message shapes are defined **once**, in schema source, and generated into all five
languages. Corpus drift becomes structurally impossible rather than a rule people
follow. schema exists precisely to emit the same types in five languages; this is
that capability pointed at the benchmark.

```
schema/bench/corpus/Bench.schema      <- the runtime-API shapes (family B), new
schema/examples/*.schema              <- the generated-code shapes (family A), exists
schema/testdata/wire/*.bin            <- byte goldens for both, exists
```

Three families. Every row in every CSV belongs to exactly one.

| family | subject | corpus source | binding |
|---|---|---|---|
| `gen` | generated code + runtime | `schema/examples/*.schema` | generated types, per-language |
| `rt` | the runtime API called by hand | `schema/bench/corpus/Bench.schema` | hand-written, **oracle-gated** |
| `bits` | the raw bit packer | this document, §1.4 | hand-written, width table normative |

### §1.2 Family `gen` — shapes unchanged, iteration counts rescaled

The 10 corpus benchmarks: 10 pinned corpus shapes. Shapes and pinned
instances are identical across the runners and gated against
`testdata/wire/*.bin`; iteration counts are identical across the runners.
They keep their names and their `bytes_per_op`: 105, 57, 13, 6, 61, 28,
10, 26, 47, 92. (Rows over the retired protocol constructs — the object
view `ship_shallow` and the dispatch `message_batch` — retired with them;
their historical CSVs and the dated audit records below read as what they
are.)

This section originally froze the iteration counts too. Measured on the M2 on
2026-08-14, those counts finished the fastest legs in 7.5–44 ms — every gen
(bench, path) leg, in every language, sat under §2.1's 200 ms floor (cpp probebits
read: ~8 ms), and the 80–165% spreads seen on a loaded machine are the direct
consequence of timing legs the clock and scheduler dominate. **§2.1 wins over this
section's freeze**: the floor is a methodology invariant, the counts are only its
instrument. Rescaled so every gen leg exceeds 200 ms in every language on the M2
(sized against the fastest measured leg per bench, with margin), identical across
all five runners, carried in the `iters` column:

| bench | old iters | new iters | × |
|---|---:|---:|---:|
| rigidbody_moving | 2,000,000 | 24,000,000 | 12 |
| rigidbody_at_rest | 4,000,000 | 32,000,000 | 8 |
| chat | 4,000,000 | 48,000,000 | 12 |
| test | 16,000,000 | 192,000,000 | 12 |
| inputpacket | 2,000,000 | 16,000,000 | 8 |
| shipcreate | 4,000,000 | 32,000,000 | 8 |
| probe_header | 16,000,000 | 256,000,000 | 16 |
| probebits | 4,000,000 | 128,000,000 | 32 |
| probearray | 2,000,000 | 20,000,000 | 10 |
| testdata | 1,000,000 | 8,000,000 | 8 |

Historical CSVs carry the old counts in their `iters` column and are readable as
what they are: legs measured under §2.1's floor, whose spreads say so. This is
the same correction §1.4 already records for the bitpacker, whose inherited
4096 passes violated the same floor and were rescaled to 24576 for the same
reason.

The same correction fired a third time on 2026-08-23: the §1.7 batch
rebalance (issue #64) cut the batch's wire bytes 2.6x, and the smaller
messages raised its msgs/s enough that all four C/C++ batch legs measured
80–121 ms at 6,400 passes on the Studio (M3 Ultra, quiet) — under the floor
again. Passes rescale **6,400 → 25,600** (26,214,400 → 104,857,600 msgs),
sized against the fastest measured leg (cpp read, 328.5 M msgs/s → 319 ms)
with margin, identical in all six runners, recorded in the `iters` column
like both corrections before it.

### §1.3 Family `rt` — new, and the reason the bespoke harnesses can be retired

The four shapes the bespoke harnesses already measure, transcribed from
`serialize/bench.cpp` into schema so all five languages inherit one definition.

```
// schema/bench/corpus/Bench.schema
package bench

// The stream packet. Transcribed from serialize/bench.cpp:141-182 —
// 12 operations in that exact order. 392 bits = 49 wire bytes.
type BenchPacket {
    a      int32          [min = -100, max = 100] //  8 bits
    b      int32          [min = 0, max = 65535] // 16
    c      int32          [min = -1000000, max = 1000000] // 21
    bits7  bits(7) //  7
    bits13 bits(13) // 13
    bits23 bits(23) // 23
    flag   bool //  1
    x      float32 // 32
    y      float32 // 32
    z      float32 // 32
    big    uint64 // 64
    align //  7 pad -> 256
    blob [17]uint8 // 136
} // = 392 bits, 49 bytes

// serialize/bench.cpp:391-403. 110 bits = 14 wire bytes.
type BenchInts {
    f0 int32 [min = -100, max = 100] //  8
    f1 int32 [min = 0, max = 65535] // 16
    f2 int32 [min = -1000000, max = 1000000] // 21
    f3 int32 [min = 0, max = 3] //  2
    f4 int32 [min = -15, max = 15] //  5
    f5 int32 [min = 0, max = 1000] // 10
    f6 int32 [min = -2048, max = 2047] // 12
    f7 int32 [min = 0, max = 255] //  8
    f8 int32 [min = -600000, max = 600000] // 21
    f9 int32 [min = 0, max = 100] //  7
} // = 110 bits, 14 bytes

// serialize/bench.cpp:449-457. 156 bits = 20 wire bytes.
// (One field per line: the grammar requires a newline after every field —
// this section's original four-per-line shorthand did not parse.)
type BenchBits {
    b7  bits(7)
    b13 bits(13)
    b23 bits(23)
    b3  bits(3)
    b32 bits(32)
    b11 bits(11)
    b19 bits(19)
    b48 bits(48)
} // = 156 bits, 20 bytes

// BenchMixed — THE canonical benchmark (issue #184). Full text:
// schema/bench/corpus/Bench.schema. 3504 bits = 438 wire bytes.
```

### §1.3a BenchMixed — THE one benchmark (issue #184)

**DECIDED (Glenn, 2026-08-31, verbatim):** *"Let's extend it to support all
serialization_* methods"* / *"in such a way that most of the bytes serialized
are integers, and while we can have strings and wstrings and byte arrays, they
should be relatively short and should not dominate. If strings and arrays
dominate, then just put in 10X more of the other types per-message."* / *"I'd
rather we just have ONE good benchmark we can apply to all serialize and schema
implementations."*

BenchMixed is **one representative game message reaching every construct the
schema language expresses**: a header (const magic, bit windows, bare 32/64-bit
raw integers, a full-unsigned ranged integer, a 64-bit ranged integer, a
48-bit window, fixed point), a counted array of 8 nested entity updates
(ranged positions and velocities, bit windows, an enum, a flags mask, bools),
a counted array of 80 integer-only stat deltas, an event section (a union
pinned to its integer arm, a fixed byte array, a short string, a short byte
block), and coverage singles (compressed floats, raw float32/float64, a bare
`uint128`, an `int128` over a range wider than 64 bits, `ufixed`, `reserved`,
an explicit `align`, and an `if`/`else` block behind a pinned gate).

**THE WEIGHTING LAW IS A GATE, NOT A HOPE.** Integer-class bits — ranged
integers of every width, bit windows, bare `intN`/`uintN`, the 128-bit family,
fixed/ufixed, enums, flags, `const`/`reserved`, and the length and count
prefixes — **must be at least 90% of the pinned wire**.
`schema/bench/corpus/budget_test.go` computes the share from the schema itself
and FAILS the build below the floor, printing the full bit-accounting table;
its own oracle is that the accounted total, rounded up to bytes, must equal the
size of `testdata/wire/bench_mixed.bin` — byte-granular, so it catches every
undercount and every overcount of a byte or more, while an overcount of one to
seven bits hides in the final flush padding. Measured 2026-08-31: **3504 bits =
438 wire bytes, integer share 91.87%** (bool 0.51%, float 3.42%, bulk 3.65%,
padding 0.54% — `bool` is deliberately excluded from the numerator, the
stricter reading). Tune the `stats` pinned count to hold the floor; never
shrink the string, byte block or floats below realism.

**Not expressible in schema v1, NAMED rather than skipped** (SPEC §4.10, both
deferred with their wire already decided): `serialize_wstring` and
`serialize_int_relative`. Wire-identical alternate spellings of covered
operations, not separate constructs: the `*_compile_time` / `*_runtime_form`
families and `serialize_compressed_float_precomputed`; `serialize_copy_string`
/ `serialize_copy_wstring` are buffer helpers, not stream operations.

**The rt legs' one stated deviation.** `string(N)` and `bytes(N)` ride as their
§4.3 decomposition — the length prefix then the used bytes — in every
hand-written leg. `serialize_string` is wire-identical, but its C++ form pays
`strlen` plus UTF-8 validation while the Go and C# ports allocate a string per
read; the decomposition is what every GENERATED target emits, so gen-vs-rt and
language-vs-language both stay apples to apples.

**Iteration counts rescaled with the shape.** bench_mixed grew from 21 to 438
wire bytes (20.9x), so its fixed count drops **40,000,000 → 4,000,000** in
every leg, and quick mode's reduced elixir count **8,000,000 → 400,000**.
Measured on the M2, 2026-08-31: at 4,000,000 the fastest row (C `rt` write,
5.7 M msg/s) takes 0.70 s per measured run — above §2.1's 200 ms floor — and
at 400,000 elixir's 0.07 M msg/s takes 5.7 s, inside §2.8's one-minute-per-leg
bound. Rows across this change are not comparable to earlier ones: the corpus
content moved, and `corpus_id` says so.

The byte sizes above are the standard's claim. **The goldens are the authority.**
If generation disagrees with the arithmetic, the goldens win and this table is
corrected — that is what §1.5 is for. Measured 2026-08-14 against the goldens
produced by the generated C++ (test/bench/main.cpp) and independently by the
generated C (test/bench/c_main.c): **49, 14, 20 bytes for the three stress
shapes — the table above is confirmed** (bench_mixed's own byte count moved to
438 with the #184 redefinition, confirmed the same way on 2026-08-31 by both
producers). The only correction §1.5 forced was syntactic: BenchBits is one
field per line because the grammar admits nothing else. The block above is the
canonical `schema fmt` form of the file as it compiles.

`align` before `blob` is required: the C++ `serialize_bytes` aligns internally, so
the schema must say so explicitly to reproduce the same 49 bytes. `TestData` in
`examples/Wire.schema` already uses this exact idiom.

### §1.4 Family `bits` — normative constants, no schema binding

The raw bit packer has no message shape, so it is specified here directly. There is
exactly **one** bitpacker workload in the estate from now on — the 16-width table,
because three of the six harnesses already use it and it exercises boundary
crossings that `i % 32 + 1` does not.

```
widths  = { 1, 32, 7, 13, 3, 25, 8, 19, 4, 28, 11, 16, 2, 30, 6, 22 }   // 227 bits/group
buffer  = 65536 bytes
passes  = 24576 per measured run
```

`passes` was originally written here as 4096 — the constant the three bespoke
harnesses inherited from `serialize/bench.cpp`. Measured on the M2 on
2026-08-14, 4096 passes finish the C++ read leg in ~170 ms, under §2.1's
200 ms floor, so this document contradicted itself. §2.1 wins: one measured
run is 24576 passes (6 × the historical constant), identical in every
language, recorded in the `iters` column. The workload itself — the width
table and the 65536-byte buffer — is unchanged.

`serialize.go/bench_test.go`'s `i % 32 + 1` workload and `serialize.rs`'s extra
`read_bits_group` row are **removed** from cross-language reporting. They may survive
as in-repo diagnostics under a `local` family, which the tool never ratios.

### §1.5 The oracle gate — how a hand-written harness binds to the corpus

Family `rt` is measured by hand-written code, because measuring the runtime API is
the point. Hand-written code drifts. The gate makes drift impossible to publish:

> A family `rt` runner MUST, before producing any number, write each pinned corpus
> instance through its hand-written path and byte-compare the result against
> `testdata/wire/bench_*.bin` — the goldens produced by the *generated* code for the
> same schema types. It MUST then round-trip write → read → re-write → memcmp. On any
> mismatch it MUST print the failure and exit non-zero without emitting rows.

This is the existing family `gen` gate (`bench/cpp/bench_main.cpp:165-203`) pointed at
a second target. It gives the estate something it has never had: a mechanical proof
that `serialize.c/bench.c` and `serialize.rs/throughput.rs` measure the same work,
rather than a header comment asserting it.

The pinned instances are defined in `test/bench/main.cpp` (the golden producer;
BenchPacket's pin is `serialize/bench.cpp` `Init()` verbatim, the other three are
pinned there) and transcribed into every runner. The goldens keep the
transcriptions honest — a wrong transcription cannot pass the gate.

The gate's residual, named: the 64 varied read buffers are length-checked
(bytes_per_op must not move under variation) but not value-checked — safe while
every rt shape is branch-free and fixed-width, because a wrong decode of such a
shape cannot change which fields ride without changing the byte count. A future
branchy or variable-width rt shape needs value checks on the variant decodes,
not only on the pinned instance.

### §1.6 `corpus_id`

Each runner computes, at runtime, from the goldens it actually loaded:

```
corpus_id = FNV-1a-64 over, for each golden file in sorted name order:
              the file's basename bytes, then a 0x00 byte, then the file's contents
```

Rendered as 16 lowercase hex digits and emitted in every row. FNV-1a-64 is used
rather than SHA-256 because it is fifteen lines in C99 with no dependency; this is
a drift detector, not a security boundary.

Two rows may be divided only if their `corpus_id` matches (§5.3). A runner that read
a stale golden reports a different id, and the tool refuses. Corpus drift becomes a
tool error rather than a published ratio.

### §1.7 Corpus composition — the real use case, by bits sent

**DECIDED (Glenn, 2026-08-16, Auckland, verbatim):** *"we are profiling the REAL use
case, which is, lots and lots of individual serialize statements"* / *"maybe a real
packet is like 1000-2000 individually serialized bits"* / *"and very rarely is it
wstring, string or byte array"* / *"I want to make sure that our profiling is not
biased towards most data going through byte arrays, by number of bits sent."*

The metric this section governs is **bulk share by bits sent**: for a bench row, the
fraction of wire bits that flow through bulk byte copies — `string(N)`, `bytes(N)`,
and `[N]uint8` payloads, whose per-byte cost is memcpy — versus bits produced by
individual serialize statements, whose per-field cost is the bitpacking dispatch this
estate exists to measure. Bulk-heavy rows converge on memory bandwidth and flatten
every language toward parity; a headline column dominated by them measures memcpy,
not the serializer.

**The audit that seeded the rule (2026-08-16, receipts in the row definitions and
`bench_main` variation code):**

| row(s) | bulk share by bits | mechanism |
|---|---:|---|
| batch write / batch read | **≈74%** | the 4096-message mix draws 25% Chat (avg ~24-byte string) and 10% Block (avg ~128-byte blob): ~149 bulk bits/message vs ~51 individually serialized |
| chat | ≈85% | 11-byte pinned string in a 13-byte message |
| bench_packet | ≈35% | the 17-byte blob in a 49-byte packet |
| ints / bits / mixed / probes / rigidbodies / bitpacker | 0% | all individual statements |

So the write/read medians are mostly honest, and the batch columns — the rows the
C-vs-C++ read campaign was fought over — are three-quarters memcpy by bits. (The
dispatch-per-message differences those columns exposed were real; but their MB/s is
inflated by bulk traffic, and by bits they do not resemble a real packet.) The
table above is the audit's record and stays as written; the batch rows were
rebalanced 2026-08-23 under rule 3's latitude — the enactment block below the
law carries the before/after.

**The law going forward:**

1. **The corpus owes a realistic snapshot shape**: 1000–2000 wire bits from
   **hundreds of individually serialized small fields of all scalar types** — ranged
   ints of assorted widths, `bits(N)`, bool, float32/float64, compressed float,
   fixed/ufixed, enums — in a **seeded-random mixed order, generated once and then
   PINNED** like every other golden (never regenerated; the seed and generator are
   recorded beside the shape). Bulk fields appear rarely or not at all, matching
   real packets.
2. **Headline rows must be dominated by individual serialize statements.**
   Bulk-heavy rows stay — memcpy throughput is worth tracking — but a table that
   leads with a bulk-dominated row must caption the bulk share, the same way §3's
   captions disclose checks and linkage asymmetries.
3. **Additive only — with Glenn's own latitude for bulk-dominated rows.**
   Existing rows and mixes are never rebalanced in place — that would silently
   re-price every cross-era comparison. New realistic rows join under new
   names with a new `corpus_id`, era-marked like any corpus change.
   **Amended 2026-08-23 (issue #64), quoting the latitude Glenn granted in
   the same 2026-08-16 ruling, verbatim:** *"of course, if any of the current
   test cases are being dominated too much by IO for the string, wstring or
   bytes types, feel free to update them if you want too."* A row or mix this
   section's audit names as bulk-dominated MAY therefore be rebalanced in
   place, under three conditions that keep the re-pricing loud instead of
   silent: (a) identical constants land in every runner in the same change;
   (b) the change is era-marked and moves the row's `bytes_per_op`, so §5.3
   rule 2 already refuses every cross-era ratio mechanically; (c) the
   rebalance states its bulk share before and after — measured over the
   actual draws, not estimated from the design weights. Additive stays the
   default; the latitude reaches exactly the rows the audit names, not every
   row someone would like to retune.
4. **Any future corpus addition states its bulk share by bits** in its definition
   comment, so this audit never has to be re-derived from variation code.

**The batch rebalance — enacted 2026-08-23 (issue #64; the RealPacket
campaign's chunk 7, under rule 3's latitude).** The audit's worst offender
was the pair of rows the C-vs-C++ read campaign was fought over: the
4096-message batch at ≈74% bulk by bits. The mix is rebalanced in every
runner (six today — chunk 7 predates the js runner and said "identical
constants x5"; the constants are identical x6), one change:

| arm | weight before | weight after | payload draw before | payload draw after |
|---|---:|---:|---|---|
| Chat | 25% | 5% | 16–31 chars (avg 23.5) | 8–15 chars (avg 11.5) |
| Test | 25% | 30% | — | — |
| Synchronize | 15% | 25% | — | — |
| Timescale | 15% | 25% | — | — |
| Heartbeat | 10% | 10% | — | — |
| Block | 10% | 5% | 64–191 bytes (avg 127.5) | 8–23 bytes (avg 15.5) |

Measured over the actual 4096 LCG draws (seed 12345 — the batch every runner
builds, so these are the mix's numbers, not the design's): bulk share by
bits **75.95% → 14.20%**, wire bits per message 206.7 → 80.6, `bytes_per_op`
25 → 10. The batch now resembles this section's real packet — dominated by
individual serialize statements, with the bulk paths still present and
deliberately rare (≈5% short chat strings, ≈5% small blocks) — while staying
the estate's only steady-state dispatch-surface row. The moved `bytes_per_op`
is the era mark: the tool refuses every ratio across the rebalance (§5.3
rule 2), which is exactly the loud re-pricing rule 3 demands. The smaller
messages raised the row's msgs/s enough to put every measured batch leg
under §2.1's 200 ms floor at the old pass count, so `BatchPasses` rescaled
6,400 → 25,600 in the same change — measured first, then rescaled, per
§1.2's own precedent (the receipt is in §1.2).

### §1.8 The string and wstring rows — defined measure-first (added 2026-08-23, issue #64)

**DECIDED (Glenn, 2026-08-17, verbatim): "You can't improve what you don't
measure."** Said the morning the estate's first string-body fix (serialize.c
#26) landed and there was no string row anywhere in the family's benches to
see it move. Enacted as an ORDERING rule, not a sentiment: a measurement row
lands BEFORE or WITH the optimization it would measure, never after — and a
row-landing change carries NO runtime, emitter, or generated-code serialize
changes, because a row that arrives fused to a fix has measured nothing. The
rows this section defines are pure instruments; the remaining string and
wstring work (the wstring/bytes sweep, the C++ WriteBytes candidate, every
port's equivalents) lands against them, never ahead of them.

**The string row — `bench_string`, family `rt`.** Transcribed from the
reference implementation's own string row (`serialize/bench.cpp`, landed
under this issue) the same way §1.3 transcribed the four original shapes:

```
// joins schema/bench/corpus/Bench.schema when the rows land
type BenchString {
    text string(63)   // 6-bit length prefix, align to byte, then the bytes
} // pinned instance: 24-byte payload -> 6 + 2 pad + 192 = 200 bits = 25 wire bytes
```

What a conforming row measures: the runtime's string path end to end —
length dispatch, byte alignment, the bulk body copy, and the read side's
contract validation (interior-NUL rejection, and UTF-8 well-formedness where
the runtime checks it). The byte sizes above are the standard's claim; the
goldens are the authority (§1.3's rule).

- **Bulk share by bits: ≈96% (192 of 200), stated per rule 4 — and that is
  the point.** This row is a DELIBERATE bulk-path instrument under rule 2:
  it exists so the string body's copy loop has a number of its own, it never
  leads a table, it never joins headline corpus medians, and any table that
  shows it captions the bulk share.
- The pinned payload is 24 bytes — the §1.7 audit's average chat string —
  printable ASCII, no interior NUL. **Length is STRUCTURE (§2.7)**: variation
  mutates payload bytes through the standard LCG at pinned length, every
  variant buffer is the same wire size, and the runner asserts
  `bytes_per_op` did not move.
- Golden: `BenchString` compiles in schema (`string(N)` exists), so the §1.5
  gate binds unchanged — `testdata/wire/bench_string.bin` produced by the
  generated C++, independently confirmed by the generated C, every runner
  byte-compares and round-trips before emitting rows.

**The wstring row — `bench_wstring`, family `rt`, corpus source: this
document.** schema defers wide strings (SPEC §4.10), so no schema type and
no generated golden can exist; like §1.4's bitpacker, the shape is specified
here directly:

```
buffer   = 64 UTF-16 code units  -> 6-bit length field
payload  = 24 code units, pinned, BMP only (each code point one unit,
           so the wire size is deterministic), no surrogates, no NUL
wire     = 6-bit length, then one unaligned 32-bit group per code unit
         = 6 + 24 x 32 = 774 bits = 97 bytes
```

What a conforming row measures: the per-unit group dispatch plus the read
side's contract validation (group range, surrogate pairing, interior NUL).

- **Bulk share by bits: 0%, stated per rule 4 — the opposite pole from the
  string row.** The wstring wire is one individual 32-bit dispatch per code
  unit, not a bulk byte copy, so this row is NOT a bulk instrument and may
  ride headline tables like any 0%-bulk row.
- Content varies per iteration through the standard LCG at pinned length,
  values pinned inside one BMP block so validation cost is uniform across
  variants. Astral and unpaired-surrogate handling is correctness territory
  owned by the conformance suites, never exercised by this row.
- Golden: produced by the C++ reference runtime and byte-confirmed by the
  serialize.c twin before first check-in — §1.3's two-independent-producers
  confirmation pattern with the runtimes standing in for the generated code —
  then every runner byte-compares and round-trips per §1.5's mechanics.
- **A runtime with no wstring path emits NO row.** An honest null, never an
  emulation; the row's absence is itself the record that the port lacks the
  path.

Mechanics both rows share when they land in the schema runners: additive
under rule 3 (new names, new `corpus_id`, era-marked), iteration counts
sized at landing per §2.1 (every leg over 200 ms, identical across
languages, recorded in `iters`), timed loops in noinline symbols with §3.2's
two call sites, like every other `rt` row. This section is the definition
only — the schema/bench implementation (corpus type, goldens, six runner
rows) lands as its own additive change, and the runtime repos' in-repo
benches carry their own local rows under the same measure-first rule.

---

## §2 Methodology

The only quiet machine in the estate is a production game server whose cores 1–15 are
isolated for the game and whose quiet window is Glenn's call per run and explicitly
not to become a habit. Everything below assumes the M2 laptop with agents running.

### §2.1 Warmup and runs

- **1 discarded warmup run per (bench, path), then 7 measured runs.** The warmup
  absorbs first-touch page faults, cold i-cache, C#'s tier-0 → tier-1 transition and
  the M2's turbo ramp.
- Iteration counts are **fixed per benchmark, chosen by the author, identical across
  every language**, and recorded in the `iters` column. Auto-scaling (`go test`'s
  `b.N`) is forbidden in cross-language rows: the estate's own three invocations of
  it disagree (`-benchtime=0.5s`, `0.2s`, `100x`).
- Each run must exceed **200 ms**. Below that the clock and the scheduler dominate.

### §2.2 The reported statistic: best, with median beside it

> **The headline statistic is the maximum rate over the 7 measured runs.**
> The median, minimum and spread are reported alongside and are not optional.

Rationale, stated plainly because this reverses the current schema/bench headline:
interference only ever makes a run *slower*. On a contended machine the fastest run
is the maximum-likelihood estimate of the uncontended cost; the median is an estimate
of the cost *plus the typical contention*, which is a property of the agents, not of
the code. The median stays in the output as the robustness check — a large
best-to-median gap is the signal that the window was dirty.

Cost of this clause is near zero: `max_msgs_per_sec` is **already** a column in the
current CSV. The change is which column `relative.go` reads, plus §2.4.

### §2.3 Spread policy — a threshold that invalidates, not merely annotates

`spread_pct = (max - min) / median * 100`.

| spread | disposition |
|---|---|
| ≤ 15% | normal; the row publishes |
| > 15%, ≤ 40% | row is marked `noisy`; excluded from corpus-median tables; may still be published individually with the mark |
| > 40% | **row is invalid**; the tool refuses to use it in any ratio and prints it as a failure |

Today `cpp,inputpacket,read` publishes at **53.4% spread** in
`results/2026-08-14-arm64-macbook.csv:26`, beside a row at 0.85%, with equal weight.
Under this clause that row does not publish.

### §2.4 Interleaving — every leg sees the same load

Running cpp to completion, then c, then go, gives each language its own private noise
window. A load spike during the go leg is indistinguishable from Go being slow.

> **Runners MUST support `--round K`: perform exactly one warmup plus one measured
> run of every benchmark and exit, emitting per-round rates. The driver MUST loop
> rounds 0..6 and invoke every language once per round.**

Consequences, accepted deliberately:

- Process startup happens 7× per language instead of once. Go and the native runners
  cost ~10 ms; `dotnet` costs ~200 ms. Irrelevant against a ≥200 ms × 12 × 2 workload.
- Every round carries its own warmup, so C# re-tiers each round. This is *better*
  than one warmup at pass start, not worse: each measured run is independently warm.
- The driver, not the runner, computes max/median/min/spread across rounds.

### §2.5 Load recording — automatic, never operator-typed

`# noise:` is whatever the operator felt like typing (`run.sh:141`). The real
discipline exists but lives outside the harness in a one-off script with a hardcoded
`ROOT` that logs to `pass.log` rather than the CSV
(`bench/tools/epyc-pass-driver.sh:41-64`). It moves into the driver and into the
preamble.

Recorded automatically, per round, into the preamble:

```
# load_start:  1.42 1.31 1.18          (Linux /proc/loadavg; Darwin sysctl -n vm.loadavg)
# load_end:    1.55 1.40 1.22
# load_max:    2.10                    (max 1-min average observed across rounds)
# core_busy_pct: 3.2                   (Linux only: pinned core /proc/stat delta over the window)
# foreign_procs: 0                      (count of non-bench CPU consumers > 5% at round boundaries)
```

`BENCH_NOISE` survives as a free-text *supplement*, never as the only load record.

### §2.6 Control legs — certifying the window

> **A pass MUST begin and end with the same control leg: the C++ family `gen` runner,
> same compiler, same flags. The pass is VALID only if the corpus-median rate of the
> two control legs agrees within 5%. Otherwise the pass is marked
> `# window: INVALID` and the tool refuses to publish ratios from it.**

The EPYC driver already does this by hand (legs 1 `cpp-gcc-start` and 8
`cpp-gcc-end`). This clause promotes it from convention to gate and applies it to the
laptop, where it matters more.

The corpus median here is taken over the control leg's family `gen` rows only — the
rt and bits rows the same runner emits are not part of the window gate.

Recorded as `# control_delta_pct: 2.3` and `# window: OK | INVALID`.

**§2.6.1 — the window gate's second instrument (adopted 2026-08-17, from the
write-demand-collapse investigation).** A control leg certifies a window only for
its own binary's shape. State that is row- and binary-selective walks through the
gate undetected — measured this campaign: three rows sat collapsed 4–8x at exact,
reproducible plateaus across two windows whose control deltas read 0.0–0.3%, on a
machine state that later proved bistable on identical bytes and immune to
environment-size, code-shift, alignment, and compile-pressure perturbations.
Therefore, two additions, both refusals-with-names:

1. **A/A twin legs.** Any pass introducing a NEW binary configuration MUST run that
   binary as two interleaved legs — same file, same inode, alternating positions in
   the round order. A row whose twin ratio departs 1.0 beyond its spread band is
   marked `state-suspect`, and the tool refuses to ratio it against other
   configurations, with the caption "twin disagreement — state-selective
   interference".
2. **Per-row historical bands.** Each host-era carries a rolling per-row band (the
   min/max of prior VALID same-configuration rows in that era). A row landing
   outside its band by more than §2.3's noisy threshold is marked `band-break` and
   publishes only with the mark.

Twin legs catch state within a pass; bands catch state between passes. Neither
requires hardware access beyond what the harness has, and either instrument would
have caught the measured phenomenon every time it fired.

### §2.7 Escape barriers and variation — universal, and the variant stride staggered

Already correct in family `gen` and in H2/H3/H4; **violated by both harnesses behind
the published Go-vs-C++ table**. Restated as normative (the variant-buffer
stride clause was added 2026-08-15, with the measurement that demanded it):

- **Per-iteration variation.** Every write loop mutates fields through the serially
  dependent Knuth-MMIX LCG `rng * 6364136223846793005 + 1442695040888963407`.
  Structure fields (counts, lengths, branch bools) stay fixed, and the runner
  **asserts `bytes_per_op` did not change**. Constant data lets the optimizer
  precompute scratch words; `serialize/bench.cpp:187-190` says so in its own comment,
  and `serialize.go/bench_test.go:117` and `serialize.go/bench/cpp/bench.cpp:185`
  both do it anyway.
- **64 rotating read buffers.** One buffer is memorised by the branch predictor and
  the caches. `bench_test.go:158` and `bench/cpp/bench.cpp:220` re-read one.
- **Variant-buffer stride is staggered: each of the 64 read buffers occupies a
  `BufferSize + 64` slot (4160 bytes), never packed at exact 4096.** Measured
  on the M2, 2026-08-15: at exact stride 4096 the 64 buffer head lines all
  map into **4 L1 set-groups** — the M2's L1D indexes on address bits
  [13:6] (256 sets), stride 4096 steps the head-line set index by 64, and
  64·k mod 256 cycles through {0, 64, 128, 192} — 16 head lines per 8-way
  set-group. A fully-inlined, memory-bound read loop touches every head
  line every 64 iterations and feels every background conflict miss in
  those four sets: cpp read spreads ran 8–18% in the same window where the
  out-of-line C reads sat near 0.1%, because the C rows' extra call
  overhead hid the misses. At stride 4160 the step is 65 sets and
  gcd(65, 256) = 1, so the 64 head lines occupy 64 distinct sets. The
  stagger is IDENTICAL in all five runners; C# additionally carries CLR
  object headers between its heap arrays, so its stride was never exactly
  4096 — the same +64 pad applies there for uniformity of policy. The
  `bits` family's 65536-byte buffers are unchanged: a bitpacker pass
  streams the whole buffer, so head-line set conflicts are not its failure
  mode. The buffer the streams see stays `BufferSize`; the pad is address
  spacing only.

  **Rates move with this change — memory behavior is the point.** Rows
  measured across this amendment are NOT comparable even though
  `corpus_id` is unchanged: the corpus hash pins the wire bytes, not the
  harness's memory layout. Compare only within a pass, as §7 already
  requires of absolutes.
- **Escape barriers.** Empty-asm memory clobber (C/C++), `std::hint::black_box`
  (Rust), `runtime.KeepAlive` + package sink (Go), `GC.KeepAlive` + static sink (C#).
  Write barriers observe the **buffer**, not `buffer[0]`.
- **Read-side sink discipline (#175): every read loop observes the FULL
  decoded struct per iteration, by one of two mechanisms, each meeting one
  bar — every decoded field store materializes in memory every iteration
  and cannot be elided.** The native legs get it free: the C/C++ clobber
  and Rust's `black_box(&out)` are zero-cost memory barriers over the whole
  struct, and the Go and C# read calls are opaque out-of-line calls taking
  the decode target's pointer (verified in the emitted assembly — an
  indirect call through a function value / delegate per iteration), with
  KeepAlive pinning liveness. The JIT/managed legs whose decode call the
  compiler inlines (java, dart, js, elixir) have no free barrier, so their
  idiom is a per-iteration fold of every decoded field into the sink —
  numbers added, booleans as 0/1, arrays element-by-element over the
  decoded extent, floats bitcast or truncated, JS BigInt observed by
  allocation-free comparison. **One named per-language deviation:** the JS
  BigInt comparison observes a 64-bit field as one bit (nonzero-ness), where
  java/dart/elixir fold the full value — a uniform full-value rule would tax
  JS's allocator per iteration and measure the allocator instead; so the JS
  read numbers are a FLOOR under a uniform fold rule, and the deviation is
  stated here rather than smoothed (#181 review). The fold is real work the
  barrier languages do not pay; those legs' read numbers are therefore
  **upper bounds** carrying the observation cost (measured on bench_mixed,
  M2, max rates per §2.2: java -20.9%, js-flat -27.7%, dart -6.4%,
  elixir -26.1% read rate against the pre-#175 narrow sinks). The JS sink's
  finite-gate catches plain-number field typos only; wrapped terms (boolBit,
  bigBit) and deleted terms pass it, so the gate is a partial guard and the
  sinks' completeness rests on review-time auditing (a recorded-read-set
  gate is the named strengthening, #175).
- **The decode target is hoisted out of the timed loop** and reused, in every
  language. Constructing a fresh zeroed message per iteration is harness overhead
  charged to the library. `bench/cpp/bench_main.cpp:250-256` already documents this;
  `throughput.rs:222` violates it and C/C++ do not.

### §2.8 Quick mode — PROPOSED

`run.sh --quick` is the ITERATION instrument, never the certification
instrument, and every leg's stderr says so. It exists so a nine-language
comparison costs minutes, not an evening; nothing it prints publishes.

- Every runner's `--quick` runs `bench_mixed` ONLY: 1 discarded warmup run,
  then **3** measured runs. The §1.5 golden gate stays unconditional.
- Iteration counts are **4,000,000 for bench_mixed** (amended 2026-08-31 with
  the #184 redefinition — see §1.3a: the shape grew 20.9x, so the count drops
  10x to keep a measured run above §2.1's 200 ms floor and the leg inside the
  one-minute bound), except where a language still cannot hold that bound:
  elixir runs 400,000 (the BEAM is ~2 orders behind the native legs). Every
  count is recorded in the `iters` column as always.
- The driver prints **two blended sections, one per subject, never ranked
  against each other** (the profiling doctrine's apples-to-apples rule,
  enacted for the table by #177/#178): the headline section is family
  `gen` — every language's schema-GENERATED code, the native legs included
  since their generated Bench legs landed — and the second labeled section
  is family `rt`, the serialize runtime API called by hand. Within a
  section: per-language per-message time `t = (1/w + 1/r) / 2` over the
  §2.2 headline (max) rates, sorted ascending, fastest = 100%, every other
  language as its time multiple. Family and `checks` mode print per row
  (checks is a recorded, NOT equalized property — §3.4), the js blend takes
  the `codec=flat` tier only (THE js path; `codec=runtime` rows never
  blend), and a caption above the sections names what is held constant:
  contract, corpus, machine, sitting, and the equalized full-struct sink
  discipline (#175).
- A language whose leg cannot run prints as an **ABSENT row with the
  reason** in the headline section, and an `--only` invocation that
  produced zero data rows **exits non-zero** — a skipped leg is a refusal,
  never a quietly green empty table (#175).
- Warmup stays adequate for the JIT legs — the discarded run is tens of
  millions of iterations, which carries the JVM to C2/OSR; a quick leg that
  shortened warmup below that would measure the interpreter and be wrong
  rather than fast.

PROPOSED means: the constants above (3 runs, the reduced elixir count, the
blended statistic) are working values pending the owner's ruling, marked
here so a table produced by quick mode can name its contract. Certification
and every published ratio remain governed by §2.1–§2.7.

---

## §3 The controls today's failures demand

### §3.1 Linkage — mandated, not accidental

One keyword moved a number 2x. The fix is not to pick the fast one; it is to stop the
harness from deciding by accident.

> **Benchmark-local message types and their serialize functions MUST have internal
> linkage in every language:**
>
> | language | required form |
> |---|---|
> | C++ | `namespace { ... }` around the packet types and their `Serialize` |
> | C | `static` on every packet write/read/measure function |
> | Rust | no `pub` on the bench packet types or their impls |
> | Go | unexported identifiers, harness in its own package |
> | C# | `internal sealed` types |

Rationale: a game's packet type is compiled into the game, not exported from a
library. Internal linkage models the real caller. The *library* side keeps whatever
packaging it ships with — that is not the harness's choice to make.

> **The library's packaging is a recorded property, not a matched one.** It goes in
> the `linkage` column: `hdr` (header-only, same TU as caller), `tu` (separate
> translation unit, no LTO), `hdr+tu` (the hot spine lives in the header and
> inlines into the caller's TU, while cold and bulk paths stay in a separate
> compiled TU, no LTO), `tu-lto`, `crate` (single crate), `pkg` (same package),
> `asm` (separate assembly).
>
> `hdr+tu` was added 2026-08-15, when serialize.c stopped being plain `tu`:
> its bit-level hot spine moved into `serialize.h` as `static inline` (that
> night's PRs), with the cold and bulk-bytes paths remaining in the compiled
> `serialize.c` TU. Recording it as `tu` would claim a call boundary the hot
> path no longer crosses; recording it as `hdr` would hide the boundary the
> bulk paths still cross. As with every linkage value, this is a property of
> the runtime's packaging, recorded, never matched.

You cannot give C and C++ the same linkage — `serialize.h` is a header and
`serialize.c` is a compiled TU (since 2026-08-15: a header hot spine over a
compiled TU, `hdr+tu`). That difference is the largest single term in the C
row and it is a property of the runtimes, not the languages. So it is recorded, and
§5.3 makes the tool refuse to divide across it without an explicit flag.
(Amended 2026-08-22, issue #66: serialize.c #25 ended this on 2026-08-17 —
the C runtime is header-only and both legs now record `linkage=hdr`, which
§5.3 compares freely. The paragraph above is kept as the rule's reason and
governs any port whose packaging still differs; for C-vs-C++ specifically it
is history, and present-tense TU attributions in any doc dated after
2026-08-17 are wrong.)

> **Every pass MUST additionally emit a labelled external-linkage diagnostic leg for
> C++ and C**, so the size of the linkage term is a published number rather than a
> thing discovered once and forgotten. Precedent: the `-flto` C diagnostic and the
> `DOTNET_TieredCompilation=0` diagnostic, both already in `bench/results/`.

### §3.2 Call sites — the same on both sides

C's write function has two call sites (untimed setup `bench.c:367`, timed loop
`bench.c:388`); its read function has one (`bench.c:408`). Two call sites keep the
out-of-line copy alive and change GCC's inlining arithmetic. That asymmetry is a term
in the published C-vs-C++ table.

> **Every benched operation MUST be called from exactly two sites: once in the
> untimed self-check/setup pass, once in the timed loop. Write and read must match,
> and every language must match.**

Two, not one, because §1.5's oracle gate already requires the setup call. This is
free to comply with and mechanically checkable — the setup call already exists in
every runner except where the gate is missing.

### §3.3 Flags — matched, and published at two levels

> **Two optimization levels per pass, for every language that has one:**
>
> | language | level A | level B |
> |---|---|---|
> | C | `-O2` | `-O3` |
> | C++ | `-O2` | `-O3` |
> | Rust | `opt-level = 2` | `opt-level = 3` |
> | Go | — (no level) | — |
> | C# | — (no level) | — |
>
> Go and C# emit one row each, marked `opt=default`. That one row rides in
> **both** levels' tables — it is the language's only build, and §5.3 rule 4
> ratios it against either level with an always-on caption naming the mix.
>
> **If the ranking of any two languages differs between level A and level B, the tool
> MUST publish both tables and MUST NOT publish a single ranking.**

This is not hypothetical: at `-O3` the C-vs-C++ read row inverts with no source
change. Publishing only one level publishes a coin flip.

Beyond the level, flags are matched as far as the languages allow, and every
difference is recorded verbatim in the preamble (already done, `run.sh:125-144`).

> **`-ffast-math` is FORBIDDEN in any standard leg.** It changes float results, so it
> changes the work. It exists today in exactly one place — `serialize/CMakeLists.txt:63`
> — and that is why the same `bench.cpp` file gets two incompatible `-O3` builds in
> this estate. `-ffp-contract=off` is required for C and C++, matching schema's wire
> determinism requirement.

> **LTO is off in every standard leg**, because Go, C# and `cargo --release` have no
> equivalent enabled by default. `-flto` and `lto = "fat"` are labelled diagnostic
> legs, never the default pass.

### §3.4 Assert state — matched where possible, named where not

> **`NDEBUG` on both sides or neither.** C and C++ legs build `-DNDEBUG`
> (which implies `SERIALIZE_RELEASE`, `serialize.h:70-75`); Rust builds with
> `debug-assertions = false`, `overflow-checks = false`.

That is as far as matching goes, and the standard says so out loud:

> **`checks` is a recorded column with three values.**
>
> | value | meaning |
> |---|---|
> | `removed` | the build compiles its debug asserts AND its bounds/range checks away (C++ with `NDEBUG`/`SERIALIZE_RELEASE`) |
> | `always` | the library keeps bounds checks, range validation and the sticky error check in **every** build by contract (Go by design; Rust because `serialize.rs` is `unsafe_code = "forbid"`; C# by its runtime's nature) |
> | `contract` | the library's debug asserts compile out like `removed`, **but** validation that is part of the wire/API contract stays in every build — serialize.c: caller-error asserts vanish under `NDEBUG`, while the write-capacity check that doubles as the sticky-error flag is unconditional (`serialize.h`, `serialize_write_bits`: "kept as a real check where the C++ BitWriter only asserts") |
>
> `contract` was added 2026-08-15 because the two-value column could not
> express serialize.c at all, and the hybrid it could not express was the
> entire C-vs-C++ write residual measured that night: calling the C write
> path `removed` claimed work it does not skip, calling it `always` claimed
> checks it does not keep, and either label laundered a per-op capacity
> check into or out of a language claim.
>
> **The tool MUST refuse to present a ratio across ANY two differing `checks`
> values as a language comparison** — `removed` vs `always`, `removed` vs
> `contract`, `contract` vs `always`, all of them. It may print the ratio
> under `--label-checks`, and the caption MUST name **both sides' semantics**,
> not just the values: *"C `checks=contract` (asserts compile out; the write
> capacity + sticky-error check stays in every build) vs C++ `checks=removed`
> (asserts and checks compile out) — this ratio includes the cost of a
> different safety contract."*

Most of the published 6x Go read gap is this. It is a real difference and it should be
measurable — but it is a *contract* difference, not a *language* difference, and the
table must say which it is.

### §3.5 Library version

> **Every row records the runtime commit** its leg was built against, in the preamble
> (already done for C++ and C, `run.sh:143`; extend to go/rust/cs).
> **Pinning a library version inside a harness is forbidden.**

`serialize.go/bench/cpp/bench.cpp:8-20` pins `serialize.h` **v1.4.3** while the
serialize repo is at **1.6.2**, and does not vendor the header, so the harness cannot
be built without manually fetching a two-year-old file. The estate currently
benchmarks two different versions of the same C++ library and publishes both.

> **Recorded provenance must be MECHANICALLY tied to the build.** The preamble
> line and the build input must come from one variable, or the line must be
> checked against the toolchain's own resolution (`cargo pkgid`, `go list -m`,
> `msbuild -getItem:Compile`) before it is printed — **and a leg that cannot
> prove its runtime path REFUSES (no rows) rather than reporting.**

This clause was earned on **2026-08-15**: `bench/rust/Cargo.toml`,
`bench/go/go.mod` and `bench/cs/schemabench.csproj` hardcoded their runtime
paths while `run.sh` recorded `$SERIALIZE_RS`/`GO`/`CS` in the preamble — so a
candidate pass recorded a fix branch's sha (`ee5bd52`) while measuring the
default checkout, a CSV lying about its own provenance. Recording the env var
was not the fix; *feeding the env var to the build and verifying the build took
it* was (`bench/tools/runtime-paths.sh`; every runtime preamble line now
carries `[build-verified: <resolved path>]`, and `run.sh`/`pass-driver.sh`
refuse on mismatch).

---

## §4 Inline reporting — the novel part

The dominant variable is invisible to every harness in the estate. This section makes
it a column and a gate.

### §4.1 Per language: how to obtain the verdict

All four mechanisms below were **verified working on the M2 on 2026-08-14**.

**Go** — `go build -a -gcflags=-m=2 ./... 2>&1`

```
can inline (*WriteStream).SerializeBits with cost 80 as: ...
cannot inline (*BitReader).ReadBits: function too complex: cost 90 exceeds budget 80
inlining call to (*BitReader).tryReadBits
```

> **`-a` is MANDATORY.** Without it the build cache serves a cached object and prints
> **nothing** — which reads as a clean bill of health. This nearly produced a false
> pass today.

**clang** — `-Rpass=inline -Rpass-missed=inline` (add `-g` for useful locations)

```
remark: 'big' inlined into 'use' with (cost=280, threshold=375) at callsite use:0:34; [-Rpass=inline]
```

> **Do not use `-fopt-info-inline` with clang.** It is a GCC spelling; Apple clang 21
> errors with `unknown argument`.

> **Only the LAST `-Rpass=` on a command line has any effect, and the ones before it fail
> silently.** `-Rpass=` takes a regex and each occurrence REPLACES the previous one rather
> than adding to it, so a command meaning to ask two questions answers only the last —
> with no warning, no error, and a perfectly plausible empty section in the report.
> Measured on Apple clang 21.0.0 against a two-call inline case:
>
> ```
> clang -O2 -c t.c -Rpass=loop-vectorize -Rpass=inline   # 2 inline remarks
> clang -O2 -c t.c -Rpass=inline -Rpass=loop-vectorize   # SILENT: no remarks at all
> ```
>
> The second line is the trap: it reads as "no inlining happened" when it means "you did
> not ask". **Run one `-Rpass=` per invocation, or spell the alternation into one regex** —
> `-Rpass='inline|loop-vectorize'` is measured to report the inline remarks, so the regex
> route works.
>
> **`-Rpass-missed=` behaves the same way and is a separate slot.** Measured on the same
> compiler with a `noinline` case: `-Rpass-missed=inline` alone reports 2 remarks, and
> `-Rpass-missed=inline -Rpass-missed=loop-vectorize` reports 0 — self-overriding, exactly
> like `-Rpass=`. The two families do not clobber each other: `-Rpass=inline
> -Rpass-missed=inline` still reports the `-Rpass=inline` remarks. *(`-Rpass-analysis=`
> was NOT measured and no claim is made about it; assume the same and verify before
> relying on it.)*
>
> **The general rule this is an instance of: an absent remark is evidence only when the
> flag that would have produced it was the last one of its kind on the line.** Silence
> from a diagnostic that was never asked the question is indistinguishable from silence
> from a clean result ([[instrument-with-no-failure-state]]).

**GCC** (the EPYC box, 13.3) — `-fopt-info-inline-optimized=FILE -fopt-info-inline-missed=FILE`

**Rust** — `RUSTFLAGS="-Cremark=inline -Cdebuginfo=1" cargo build --release`

```
note: ...: inline (success): '<callee>' inlined into '<caller>' with (cost=-15030, threshold=487) ...
note: ...: inline (missed): '<callee>' not inlined into '<caller>' because ... (cost=never)
```

Symbols are v0-mangled; filter to the `serialize` crate by mangled substring or pipe
through `rustfilt`. Note that `RUSTFLAGS` participates in cargo's fingerprint, so a
flag change forces a rebuild — but two identical invocations hit the cache, the same
hazard as Go's `-a`. Use `cargo build --release --offline -q` after `touch`ing the
crate root, or set `CARGO_INCREMENTAL=0` and clean the target dir for the verdict pass.

**C#** — release runtime, no checked JIT needed:

```
DOTNET_TieredCompilation=0 \
DOTNET_JitDisasm='SchemaBench:BenchWriteLoop' \
DOTNET_JitStdOutFile=$PWD/jit.txt \
dotnet run -c Release --no-build
```

The emitted method header is directly parseable:

```
; Assembly listing for method P:Main() (FullOpts)
; 0 inlinees with PGO data; 1 single block inlinees; 0 inlinees without PGO data
```

`DOTNET_JitDisasmSummary=1` additionally lists every separately-JIT-compiled method
with its tier — a method inlined into all its callers does not appear.

**Universal fallback, and the ground truth all five must agree with:**

> **Count call instructions in the emitted body of the timed loop.**
> C/C++/Rust: `objdump -d` (or `otool -tv` on Darwin) over the symbol, count `bl`/`blr`
> on arm64 and `call` on x86-64. Go: `go tool objdump -s <symbol>`. C#: count `bl`/`blr`
> in the `DOTNET_JitDisasm` output.
>
> A tail branch counts as a call (issue #86): a `b` on arm64 (`jmp` on x86-64,
> `JMP name(SB)` in Go objdump) whose target is **another function's symbol** is a
> transfer into a callee that was *not* inlined — it just reused the caller's frame.
> A branch to a local label or raw address inside the same function is control flow,
> not a call, and never counts.
>
> A fully inlined chain has **zero** calls into the serialize runtime inside the loop
> body. This is language-independent and compiler-version-independent, and it is the
> number the standard actually cares about.

### §4.2 The `inline` column

Every row carries a verdict:

| value | meaning |
|---|---|
| `full` | zero **hot** runtime calls in the timed loop body |
| `partial:N` | N **hot** runtime calls remain **per op** |
| `none` | the chain is entirely out-of-line |
| `unknown` | the verdict pass did not run — **the tool treats this as a refusal to ratio** |

**`N` counts HOT calls only. Cold calls are recorded beside the verdict, never
inside it.** Amended 2026-08-15 after the same misread happened four times in
one night: a `#[cold]` error constructor, a guarded `load_window_slow`
fallback, and a deliberate bulk-bytes boundary all counted into `partial:N`
identically to a stranded hot call — debt indistinguishable from design. The
measured proof: the serialize.rs read-path fix took the bench_packet read
chain's hot path to **zero** remaining calls while the raw count went
`partial:11 → partial:28`, because every newly inlined window load carries
one `#[cold]` slow-path call site. A verdict that gets *worse* when the hot
path gets *clean* is measuring the wrong thing.

A call is **cold** when its **target** carries a signal the toolchain
actually emits — one of:

1. the target lives in a compiler-split cold section or symbol
   (ELF `.text.unlikely`; Mach-O's spelling is a split `foo.cold` /
   `foo.cold.N` symbol);
2. the target is a function marked cold at source in the runtime the leg
   built against (`#[cold]` in Rust — matched by its mangled-name token,
   scanned from the crate the bench manifest points at), or the compiler's
   remarks state the callee is cold;
3. the call is reached only behind a guarded fallback edge the ledger can
   itself identify and name.

The signals in use per language, on this host:

| language | live cold signals |
|---|---|
| C, C++ | split `.cold`/`.cold.N` symbols (signal 1) |
| Rust | `#[cold]`-marked fns in the built-against crate source, plus split symbols (signals 1 + 2) |
| Go | none — gc emits no cold attribute, section, or remark |
| C# | none — JitDisasm names no cold blocks or targets |

**Where a signal is unavailable the call stays hot. The classifier never
guesses cold**, because the two failure directions are not symmetric:
counting a cold call hot overstates debt and invites a pointless
investigation; counting a hot call cold publishes a false `full` — the
exact class of wrong number this standard exists to kill. Signal 3 is
admissible under this definition but not implemented by the current pipeline;
until a ledger mechanism can *prove* an edge is guard-only, such calls count
hot. Consequence, stated honestly: serialize.c's deliberate bulk-bytes
boundary carries no cold marking today, so it still counts hot — if it is
truly cold by contract, the fix is a cold annotation in serialize.c, not a
guess in the verdict pass.

The per-symbol `.inline` ledger carries **both** counts — hot and cold — per
symbol and per (bench, path) row, so the cold debt stays visible and
diagnosable without contaminating the comparison column.

**`N` is per op, not per emitted call site.** An unrolled timed loop repeats
its per-op calls once per unrolled iteration — clang unrolled the C
`rt_bench_packet_read_loop` 4x, and counting raw sites published `partial:12`
where each op performs 3 runtime calls. When the loop's per-op work flows
through an out-of-line helper, the verdict counts the calls one op makes —
the helper's own transitive count plus the loop's per-op direct calls — never
the raw sites in the loop body. Mechanically: §3.2 makes every out-of-line
helper called from a timed loop a once-per-op call in source, so the smallest
per-helper emitted edge count out of the loop body is the unroll factor, and
the loop's transitive count divides by it. A count that does not divide
cleanly is not attributable and stays `unknown`; a loop with no helper edges
has no unroll witness and reports its body count unchanged. This definition
applies to every language's rows equally — `partial:N` must mean the same
thing in every row, or the column cannot be compared at all.

**Per-row attribution, where the entry symbol is gone.** A gen row whose
generated entry inlined away entirely cannot be counted through its symbol.
The verdict pass then attributes every remaining runtime call site by walking
`atos -i` inline stacks over a deterministic `-g` shadow build (same compiler,
same flags — clang's inlining is deterministic, `-g` adds only metadata, and a
drift guard compares the shadow's total call structure against the measured
binary). A call site attributes to (bench, path) when its inline stack passes
through a bench's timed write or read loop; setup, self-check and variant
code is excluded as untimed. This is live for C++ and — since 2026-08-15 —
for C, whose per-bench driver became an `#include` template
(`bench/c/bench_message.inc`, token-identical to the old macro, verified by
preprocessor token comparison) precisely so its expansions carry real line
numbers. The unroll rule above has an attribution analog: an unrolled loop's
copies of one source-level call site carry identical inline stacks and
target, so attribution counts **distinct (stack, target) signatures** — the C
read loops measure unrolled 4x, and raw site counting published `partial:36`
where the per-op truth is `partial:9`.

`unknown` being a refusal, not a shrug, is the point. A number without an inline
verdict is not comparable to a number with one.

Alongside the CSV, each pass writes `results/<name>.inline` — the full per-symbol
ledger (symbol, hot and cold call counts, cost, budget, verdict) for every
language, so a regression can be diagnosed rather than merely detected.

### §4.3 The regression gate

Detecting the cliff after the fact is not enough; Go is sitting on it right now.

> **Each runtime repo carries `tending/inline-budget.txt`: a checked-in ledger of
> `<symbol> <metric> <limit>`. `make inline-gate` recomputes the metric and exits
> non-zero on any regression. It runs in that repo's CI on every commit.**

Ledger format — one line per guarded symbol, `#` comments allowed:

```
# serialize.go/tending/inline-budget.txt
# metric: gc-cost   limit: max inline cost the compiler may report
# Two symbols sit at exactly 80 of 80. Zero headroom is deliberate and guarded:
# one added line takes them out of line and halves throughput.
(*WriteStream).SerializeBits    gc-cost   80
(*BitReader).tryReadBits        gc-cost   80
(*ReadStream).SerializeBits     gc-cost   80
(*WriteStream).tryWriteBits     gc-cost   80
(*WriteStream).SerializeFloat32 gc-cost   80
(*WriteStream).SerializeFloat64 gc-cost   80
# Already over budget. Pinned at current cost so they cannot get worse, and
# tracked as debt — these are the reason the bitpacker rows are what they are.
(*BitWriter).WriteBits          gc-cost   93
(*BitReader).ReadBits           gc-cost   90
(*WriteStream).SerializeBits64  gc-cost  268
(*ReadStream).SerializeBits64   gc-cost  278
```

For C, C++, Rust and C# the metric is `loop-calls` — the call count from §4.1's
universal fallback, measured over the family `rt` timed loop:

```
# serialize.c/tending/inline-budget.txt
# metric: loop-calls   limit: max calls into the runtime in the timed loop body
bench_packet_write   loop-calls  12   # -O2, no LTO: one call per field, the known baseline
bench_packet_read    loop-calls   0
```

Two properties make this a gate rather than a report:

1. **It fails on regression, not on absolute badness.** `WriteBits` at cost 93 is
   already broken; the gate's job is to stop it becoming 120 while nobody looks.
2. **It fails loudly at the cliff.** Adding one line to `SerializeBits` takes it from
   80 to 81 and CI goes red with `cost 81 exceeds budget 80` — before the number
   moves, not after someone notices a 2x in a table three weeks later.

Improving a budget is a deliberate ledger edit in the same commit. That edit is the
record that the improvement happened.

---

## §5 Reporting format

**The reference language is C. C is the 100%; every other language, C++
included, is measured against C.** (Glenn, 2026-08-17, verbatim: *"make C the
reference. It is the 100%. C++ is measured against C."* — flipped from C++,
which had been the table baseline since the table existed.) A relative table
presents each language's **time** as a percentage of C's, computed on the §2.2
headline rate as `c_rate / lang_rate × 100` — higher is slower, 200% takes
twice as long as C. Two things this ruling does NOT move: the §2.6 control leg
stays the C++ family `gen` runner (the window instrument is not the table
reference), and historical CSVs and published tables that predate the ruling
stay in their recorded convention — a band or delta against them converts at
read time rather than rewriting history.

### §5.1 CSV v2 — append-only

New columns are **appended**, so `relative.go`'s existing positional parse
(`fs[0..2]`, `num(3..10)`, `relative.go:98`) keeps working unchanged and historical
CSVs still load.

```
lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline[,codec]
```

| column | values |
|---|---|
| 1–11 | unchanged from v1 |
| `corpus_id` | 16 hex digits, §1.6 |
| `family` | `gen` \| `rt` \| `bits` \| `local` |
| `linkage` | `hdr` \| `tu` \| `hdr+tu` \| `tu-lto` \| `crate` \| `pkg` \| `asm` (§3.1) |
| `checks` | `removed` \| `always` \| `contract` (§3.4) |
| `opt` | `O2` \| `O3` \| `default` |
| `inline` | `full` \| `partial:N` \| `none` \| `unknown` |
| `codec` | `flat` \| `runtime` — appended (2026-08-18) only on rows of a language that ships more than one generated codec. Today that is JavaScript: `flat` marks the flat tier, THE js path under the ruling ("whichever correct implementation is fastest is the one we use for JavaScript"), and `runtime` marks the runtime-call generated tier riding as labeled supplementary rows. Rows without the column (the five AOT languages, and the js `rt`/`bits` families, which measure the runtime library itself) stay 17 fields — append-only holds. |

The **headline rate is `max_msgs_per_sec`** (§2.2). `median_mb_per_sec` keeps its name
and its MiB/s meaning; a `max_mb_per_sec` is not added because it is
`max_msgs_per_sec * bytes_per_op / 1048576` and derivable.

> **MB/s means MiB/s (1024×1024) everywhere.** `serialize.go/bench/cpp/bench.cpp:60`
> emits decimal MB/s against everyone else's MiB/s — a silent 4.9% offset inside a
> published cross-language table.

Rows loaded from a v1 CSV get empty `corpus_id` and `inline=unknown`, and are
therefore un-ratioable by §5.3. That is correct: legacy data cannot be trusted to be
comparable, and now it says so.

### §5.2 Preamble

Everything `run.sh:125-144` already records, plus §2.5's automatic load capture,
§2.6's window verdict, and the runtime commit for every language:

```
# schema bench results
# date / host / arch / os / cpu
# build: Release   opt: O3
# cpp compiler: ...      # cpp flags: ...
# c compiler: ...        # c flags: ...
# go: ...  # rust: ...  # dotnet: ...
# pinning: taskset -c 0 | none
# schema commit / serialize commit / serialize.c commit / serialize.go commit /
#   serialize.rs commit / serialize.cs commit
# corpus_id: 3f8a1c22b90de471
# rounds: 7   interleaved: yes
# load_start / load_end / load_max / core_busy_pct / foreign_procs
# control_delta_pct: 2.3   window: OK
# noise: <free text supplement>
```

### §5.3 The tool must refuse

`bench/tools/relative.go` today merges by `(lang, bench, path)`, **discards the
preamble** (`relative.go:103-116`), and lets a later file silently overwrite rows from
a different host, compiler or flag set. `abTable` (`relative.go:219`) computes a B/A
ratio across two arbitrary CSVs without comparing their builds. The data to catch this
is in the file; nothing reads it.

> **The tool MUST carry the preamble through the merge, and MUST refuse to compute
> any ratio when any of the following differ between the two rows:**
>
> 1. `corpus_id` — or either is empty
> 2. `bytes_per_op`
> 3. `family`
> 4. `opt` — when both sides carry **explicit levels** that differ (`O2` vs
>    `O3`, the §0 coin flip). `default` is not a level, it is §3.3's record
>    that the language *has* no level (Go, C#): that row is the language's
>    only truth and ratios against any single table's level, captioned,
>    always, no flag. (Amended 2026-08-15: the first full five-language
>    table refused on `O3` vs `default` — a literal reading made the
>    cross-language table unprintable *by construction*, since Go and C#
>    can never carry anything but `default`. A rule that can only ever
>    refuse is not a check, it is a lock.)
> 5. `checks` (unless `--label-checks`, which prints the ratio with the §3.4 caption)
> 6. `linkage` (unless `--cross-linkage`, likewise captioned)
> 7. `inline` is `unknown` on either side
> 8. the source CSVs' `# cpu` lines, or the flag lines for the languages involved
> 9. either row's `spread_pct` > 40, or `# window: INVALID`
> 10. `codec` — a runtime-call generated number and a flat number measure
>     different shipped code, so a ratio across them is a code-change delta
>     wearing a language-delta costume. An empty codec (a one-codec language,
>     or a pre-codec CSV — the old js rows were runtime-call) never matches a
>     labeled tier either. (Appended 2026-08-18 with the js flat tier.)
>
> A refusal prints both rows and the differing field, and exits non-zero. It never
> prints a number.

That list is a direct transcription of the failures in §0. Rule 1 stops the
Go-vs-C ratio that was correctly refused by hand. Rules 4 and 6 stop the
Rust-17878-vs-C-4581 comparison. Rule 8 stops rows from two machines silently
overwriting each other. Rule 7 stops the entire class this standard exists to close.

`--force` does not exist. If two rows are not comparable, the fix is another
measurement, not a flag.

---

## §6 Migration path

Seven steps. Each is independently valuable and lands on its own; nothing here is a
big-bang. Ordered cheapest-and-highest-value first.

### Step 1 — `relative.go`: refusal and preamble enforcement
*One file, one repo, no measurement changes, no runner changes.*

Carry the preamble through `merge`. Implement §5.3's nine refusal rules and §2.3's
spread policy. Switch the headline from `med` to `mx`. Existing v1 CSVs still load and
become un-ratioable, which is the correct outcome.

**This is the highest-value hour in the whole plan.** It stops the estate publishing
another wrong ratio, before a single benchmark is rewritten.

### Step 2 — CSV v2 columns from the five schema runners
*Five small diffs, mechanical.*

Append the six columns. `corpus_id` is ~15 lines of FNV-1a per language. `family`,
`linkage`, `checks`, `opt` are compile-time constants per runner. `inline` is
`unknown` until step 3.

### Step 3 — inline capture in `run.sh` and the driver
*One script, plus env vars for the C# runner.*

Add a verdict pass per language using §4.1's verified commands. Write
`results/<name>.inline`. Backfill the `inline` column. No runner source changes for
C/C++/Rust/Go; C# needs `DOTNET_TieredCompilation=0` + `JitDisasm` + `JitStdOutFile`
plumbed into its invocation.

**After step 3 the estate can answer, for the first time, "did it inline?" — which is
most of the value of this document.**

### Step 4 — the inline budget gate, per repo
*Five implementations. This is the honest cost of five wire-compatible libraries.*

Order matters — go first, because Go is on the cliff today:

1. `serialize.go` — `make inline-gate`, ledger from §4.3's measured values, wired into
   `.github/workflows/ci.yml`. Guards `SerializeBits` and `tryReadBits` at 80/80.
2. `serialize` (C++) — `loop-calls` gate over the family `rt` loop.
3. `serialize.c` — same, and it finally makes the two-call-site asymmetry visible.
4. `serialize.rs` — same, via `-Cremark=inline` plus `objdump`.
5. `serialize.cs` — same, via `JitDisasm`. This repo has no benchmark at all today, so
   it lands with step 6.

Each is ~80 lines of shell plus a ledger. Five times. There is no way around it.

### Step 5 — the interleaved driver, load capture, control legs
*Replaces `bench/tools/epyc-pass-driver.sh` with something that runs on both machines.*

Add `--round K` to all five runners (the loop already exists; this exposes it). Driver
loops rounds, invokes every language per round, aggregates max/median/min/spread,
captures load per round, runs the start/end control legs, computes
`control_delta_pct`, and stamps `# window:`. The EPYC driver's `pscheck`, quiet-window
wait and `/proc/stat` snapshot move in wholesale; the hardcoded `ROOT` goes away.

macOS pinning stays absent — there is no `taskset` equivalent worth trusting — and the
preamble keeps saying so. The M2 legs are unpinned by construction and the standard
does not pretend otherwise.

### Step 6 — family `rt`: the schema corpus and the oracle
*The step that lets the bespoke harnesses be retired.*

1. Add `schema/bench/corpus/Bench.schema` (§1.3) and wire it into `SCHEMAS`.
2. Generate into all five languages; generate `testdata/wire/bench_*.bin` goldens.
3. Add family `rt` benchmarks to the five schema runners, measuring the runtime API by
   hand, gated by §1.5 against those goldens.
4. Verify the byte sizes: 49, 14, 20, 21. If generation disagrees with §1.3's
   arithmetic, the goldens win and §1.3 is corrected.

### Step 7 — retire the six harnesses
*In this order, each once its replacement is measuring.*

| harness | disposition |
|---|---|
| `serialize.go/bench/cpp/bench.cpp` | **DELETE.** It pins `serialize.h` v1.4.3, does not vendor it, serializes constant data, re-reads one buffer, and emits decimal MB/s. Family `rt` replaces it entirely. Its published table goes with it. |
| `serialize/bench.cpp` | Becomes the C++ family `rt` runner under the contract. Keeps its compile-time-vs-runtime matched pairs as a `local` family — that is a genuine C++-only question and nothing else can answer it. Drops `-ffast-math`. |
| `serialize.c/bench.c` | Becomes the C family `rt` runner. Its header comment is the best statement of intent in the estate and should survive nearly verbatim. Gains the oracle gate, which is what it has always been asserting by hand. |
| `serialize.rs/throughput.rs` | Becomes the Rust family `rt` runner. Loses the orphan `read_bits_group` row from cross-language reporting. Gains the goldens — and note that its 410 M packets/s stream read is ~2.4 ns for a 12-field decode, which nothing currently verifies actually happened. |
| `serialize.go/bench_test.go` | **Keeps** its `go test` rows, because `allocs/op` is a real and unique signal and `go test` is the right tool for it. **Loses** its cross-language table. Fixes its `SetBytes` denominators (`bench_test.go:17` vs `:125` vs `:293`, and `:368`/`:394` which have none). Marked `family=local`. |
| `serialize.cs` | Gains its first benchmark, as a family `rt` runner. |
| `schema/bench` | Becomes the standard. |

Then **un-publish what the standard cannot back**: the Go-vs-C++ table in
`serialize.go/docs/performance.md`, and the C++ column in
`serialize.rs/README.md:146-152` whose build is recorded nowhere and whose three
candidate builds are mutually incompatible. Replace both with a pointer to a dated
pass under this standard, or with nothing. Nothing is better than wrong.

### Honest cost

Five wire-compatible libraries means five implementations of everything. Steps 2, 4, 6
and 7 are each five-times work. Roughly:

- Steps 1 + 3: hours, one repo each. **Do these regardless.**
- Step 2: an afternoon.
- Step 5: a day, one script.
- Step 4: a day per repo, five repos.
- Steps 6 + 7: several days, mostly in the four runtime repos.

Steps 1–3 alone deliver: no more wrong ratios, and the inline verdict. That is the
majority of the value for a small fraction of the cost. Steps 4–7 are what make it a
standard instead of a fix.

---

## §7 What this standard does NOT fix

Stated plainly, so a legitimate difference stays distinguishable from a measurement
artifact.

**GC and JIT impose a floor no harness change removes.** Go's allocator and write
barriers, and C#'s tiered JIT and GC, are properties of those runtimes. Interleaving
and best-of-N reduce *contention* noise; they do not remove a GC pause from the
distribution or make tier-1 arrive sooner. Where a Go or C# number is floored by its
runtime, best-of-N will show it as a *stable* number, not a fast one — a low spread
with a high absolute is a floor, not noise. The standard makes the two
distinguishable. It does not make the floor go away.

**The safety-contract difference is permanent, and it is not a measurement error.**
Go keeps bounds checks, range validation and the sticky error check in every build by
design. `serialize.rs` is `unsafe_code = "forbid"`, so every load is bounds-checked in
every build, forever. A `-DNDEBUG` C++ build compiles all of that away. §3.4 records
it and §5.3 refuses to launder it into a language claim — but the work genuinely
differs, and no harness can equalise it without changing one of the libraries.

**Inline verdicts are compiler-version-specific.** Go's cost-80 budget, GCC's
15-instruction auto budget and clang's 375-cost threshold are implementation details
that move between releases. The ledger will need re-baselining on a toolchain bump,
and that re-baselining is a judgement call, not a mechanical one. The gate catches
*your* regressions; it also fires on *their* changes, and telling those apart is
manual work.

**Absolutes remain non-portable.** Only ratios measured back to back on one machine in
one window mean anything — which `serialize.c/bench.c:47-48` already says better than
this document does. Cross-machine comparison of absolute rates is out of scope and
always will be.

**Best-of-N is still biased by turbo and thermals.** On the M2, an early run can be
genuinely faster because the chip has not heated up. The discarded warmup and the
control legs bound this; they do not eliminate it. A `control_delta_pct` near 5% is a
warning that the window drifted even though it passed.

**macOS has no pinning.** The M2 legs are unpinned by construction. `taskset` has no
Darwin equivalent worth trusting, so the laptop's numbers carry more variance than the
EPYC box's and always will.

**The standard cannot make "Go is 2x behind C" a language claim.** After all seven
steps, that sentence still is not measurable. What becomes measurable is: *"on this
machine, in this window, at `-O3`, with `checks=removed` for C and `checks=always` for
Go, with C linking `serialize.c` as a separate TU without LTO, over corpus
`3f8a1c22b90de471`, with C fully inlined and Go partially, Go's family `rt` write is
N× C's."* That is a longer sentence and a smaller claim. It is also the only one that
has ever been true.

**Nothing here makes a benchmark measure what you meant.** The oracle gate proves the
bytes are right; the escape barriers prove the work happened; the inline verdict
proves how it was compiled. None of that proves the benchmark asks a useful question.
That judgement stays with whoever writes it.
