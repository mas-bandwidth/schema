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

### §1.2 Family `gen` — unchanged

The 12 existing benchmarks: 11 pinned corpus messages plus the 4096-message dispatch
batch. Shapes, iteration counts and pinned instances are already identical across the
five runners and already gated against `testdata/wire/*.bin`. Nothing in §1 changes
them. They keep their names and their `bytes_per_op`: 105, 57, 13, 6, 61, 28, 28, 10,
26, 47, 92, 25.

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

// serialize/bench.cpp:517-528. 168 bits = 21 wire bytes.
type BenchMixed {
    sequence  int32          [min = 0, max = 65535] // 16
    ack_bits  bits(32) // 32
    entity_id bits(12) // 12
    pos_x     int32          [min = -16384, max = 16383] // 15
    pos_y     int32          [min = -16384, max = 16383] // 15
    pos_z     int32          [min = -16384, max = 16383] // 15
    yaw       bits(9) //  9
    moving    bool //  1
    firing    bool //  1
    timestamp bits(48) // 48
    weapon    int32          [min = 0, max = 15] //  4
} // = 168 bits, 21 bytes
```

The byte sizes above are the standard's claim. **The goldens are the authority.**
If generation disagrees with the arithmetic, the goldens win and this table is
corrected — that is what §1.5 is for. Measured 2026-08-14 against the goldens
produced by the generated C++ (test/bench/main.cpp) and independently by the
generated C (test/bench/c_main.c): **49, 14, 20, 21 bytes — the table above is
confirmed.** The only correction §1.5 forced was syntactic: BenchBits is one
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

Recorded as `# control_delta_pct: 2.3` and `# window: OK | INVALID`.

### §2.7 Escape barriers and variation — unchanged, now universal

Already correct in family `gen` and in H2/H3/H4; **violated by both harnesses behind
the published Go-vs-C++ table**. Restated as normative:

- **Per-iteration variation.** Every write loop mutates fields through the serially
  dependent Knuth-MMIX LCG `rng * 6364136223846793005 + 1442695040888963407`.
  Structure fields (counts, lengths, branch bools) stay fixed, and the runner
  **asserts `bytes_per_op` did not change**. Constant data lets the optimizer
  precompute scratch words; `serialize/bench.cpp:187-190` says so in its own comment,
  and `serialize.go/bench_test.go:117` and `serialize.go/bench/cpp/bench.cpp:185`
  both do it anyway.
- **64 rotating read buffers.** One buffer is memorised by the branch predictor and
  the caches. `bench_test.go:158` and `bench/cpp/bench.cpp:220` re-read one.
- **Escape barriers.** Empty-asm memory clobber (C/C++), `std::hint::black_box`
  (Rust), `runtime.KeepAlive` + package sink (Go), `GC.KeepAlive` + static sink (C#).
  Write barriers observe the **buffer**, not `buffer[0]`.
- **The decode target is hoisted out of the timed loop** and reused, in every
  language. Constructing a fresh zeroed message per iteration is harness overhead
  charged to the library. `bench/cpp/bench_main.cpp:250-256` already documents this;
  `throughput.rs:222` violates it and C/C++ do not.

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
> translation unit, no LTO), `tu-lto`, `crate` (single crate), `pkg` (same package),
> `asm` (separate assembly).

You cannot give C and C++ the same linkage — `serialize.h` is a header and
`serialize.c` is a compiled TU. That difference is the largest single term in the C
row and it is a property of the runtimes, not the languages. So it is recorded, and
§5.3 makes the tool refuse to divide across it without an explicit flag.

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
> Go and C# emit one row each, marked `opt=default`.
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

> **`checks` is a recorded column with two values.** `removed` — the build compiles
> its bounds and range checks away (C, C++, C# with `NDEBUG`-equivalent semantics).
> `always` — the library keeps bounds checks, range validation and the sticky error
> check in **every** build by contract (Go by design; Rust because
> `serialize.rs/Cargo.toml:22` is `unsafe_code = "forbid"`).
>
> **The tool MUST refuse to present a ratio across differing `checks` values as a
> language comparison.** It may print it, labelled: *"C++ `checks=removed` vs Go
> `checks=always` — this ratio includes the cost of a different safety contract."*

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
> A fully inlined chain has **zero** calls into the serialize runtime inside the loop
> body. This is language-independent and compiler-version-independent, and it is the
> number the standard actually cares about.

### §4.2 The `inline` column

Every row carries a verdict:

| value | meaning |
|---|---|
| `full` | zero runtime calls in the timed loop body |
| `partial:N` | N runtime calls remain |
| `none` | the chain is entirely out-of-line |
| `unknown` | the verdict pass did not run — **the tool treats this as a refusal to ratio** |

`unknown` being a refusal, not a shrug, is the point. A number without an inline
verdict is not comparable to a number with one.

Alongside the CSV, each pass writes `results/<name>.inline` — the full per-symbol
ledger (symbol, cost, budget, verdict) for every language, so a regression can be
diagnosed rather than merely detected.

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

### §5.1 CSV v2 — append-only

New columns are **appended**, so `relative.go`'s existing positional parse
(`fs[0..2]`, `num(3..10)`, `relative.go:98`) keeps working unchanged and historical
CSVs still load.

```
lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline
```

| column | values |
|---|---|
| 1–11 | unchanged from v1 |
| `corpus_id` | 16 hex digits, §1.6 |
| `family` | `gen` \| `rt` \| `bits` \| `local` |
| `linkage` | `hdr` \| `tu` \| `tu-lto` \| `crate` \| `pkg` \| `asm` |
| `checks` | `removed` \| `always` |
| `opt` | `O2` \| `O3` \| `default` |
| `inline` | `full` \| `partial:N` \| `none` \| `unknown` |

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
> 4. `opt`
> 5. `checks` (unless `--label-checks`, which prints the ratio with the §3.4 caption)
> 6. `linkage` (unless `--cross-linkage`, likewise captioned)
> 7. `inline` is `unknown` on either side
> 8. the source CSVs' `# cpu` lines, or the flag lines for the languages involved
> 9. either row's `spread_pct` > 40, or `# window: INVALID`
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
