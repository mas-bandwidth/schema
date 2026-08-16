# ANALYSIS.md — the C ↔ C++ parity ledger (Phase 4, compile-only)

2026-08-16, on the M2 MacBook Air (host `macbook`), Apple clang version 21.0.0
(clang-2100.1.1.101). **No timing was performed and none appears here** — a
serialize.js build campaign owned this machine for the whole session, so every
claim below is remarks, disassembly, or symbol-table evidence from compile-only
builds. Anything that needs a clock is staged, not concluded.

## Acceptance criterion (the doctrine this ledger answers to)

Every C-vs-C++ gap row ends in exactly one of:

1. **NAMED** — a named compiler / language / contract mechanism, with the remark
   or disassembly line that proves it, and a statement of why no candidate is
   warranted (contract-by-design, or family doctrine); or
2. **CANDIDATE** — a switch-guarded patch, **default OFF and byte-identical when
   OFF**, whose effect is remark-proven, staged on a branch, timing-pending; or
3. **RESOLVED-UPSTREAM** — closed by an already-landed upstream change, with the
   remark evidence that the mechanism is gone, re-measure pending.

"We haven't ported the approach" is not a legal terminal state. No cold or
noinline hints anywhere — the machine outliner failure (bits write −25%) is a
family-measured constant, and every armed build below was checked for
`OUTLINED_FUNCTION_*` symbols (zero, in every configuration).

## Provenance

| tree | main at analysis | candidate branch |
|---|---|---|
| schema | `34b917d` | `cpp-read-spine-demand` @ `d6e3b21` |
| serialize (C++) | `3594508` (v1.9.0) | `read-spine-demand` @ `86f9690` |
| serialize.c | `32fedea` (v1.3.0) | none needed (see rows 3–5) |

The measured baseline this ledger reasons against is the 2026-08-15 Studio
postlane O3 pass (`bench/results/2026-08-15-arm64-studio-postlane-O3-pass.csv`
and `.inline`), which built serialize @ `35abfc5` and serialize.c
@ `cecaa04` (**v1.2.0** — one release behind current main; rows 3–5 turn on
exactly that).

Remark builds reproduce the recorded leg flags exactly (repo-relative, with
`$SERIALIZE` / `$SERIALIZE_C` the runtime checkouts):

```
c++ -O3 -DNDEBUG -DSERIALIZE_RELEASE -DBENCH_OPT="O3" -std=c++17 -Wall -Wextra -Werror \
    -ffp-contract=off -fno-rtti -Igenerated/cpp -Itest -I$SERIALIZE \
    "-Rpass=inline|loop-unroll|loop-vectorize" "-Rpass-missed=inline|loop-unroll|loop-vectorize" \
    -g bench/cpp/bench_main.cpp -o <out>

cc  -O3 -DNDEBUG -DBENCH_OPT="O3" -std=c99 -Wall -Wextra -Werror \
    -Igenerated/c -I$SERIALIZE_C \
    "-Rpass=inline|loop-unroll|loop-vectorize" "-Rpass-missed=inline|loop-unroll|loop-vectorize" \
    -g bench/c/bench_main.c $SERIALIZE_C/serialize.c -o <out> -lm
```

One toolchain sharp edge, learned live: clang keeps only the LAST `-Rpass=`
value — separate `-Rpass=inline -Rpass=loop-unroll` flags silently emit ZERO
inline remarks. The families must ride in a single regex alternation, as above.

---

## THE QUESTION: is the C batch-read lead (a) a named C++/clang limitation, or (b) C's remedy not yet applied to C++?

**Answer: (b), with the confounder confirmed and the mechanism named more
precisely than the prediction.** Rowan's registered prediction — (b), with the
C TU-boundary as partial confounder (outlined functions get fresh entry
frequencies) — is CONFIRMED at remark level, with one refinement: the thing
that flattens C's dispatcher is not the `SERIALIZE_ALWAYS_INLINE` spine (that
governs the per-field ops *inside* it) but **LLVM's last-call-to-static bonus,
which C's internal-linkage packaging earns and C++'s `linkonce_odr` header
functions cannot**. Timing decides whether closing that boundary closes the
30%; the candidates are staged and remark-proven.

The attribution evidence, site by site:

**C++ batch read** (`bench/cpp/bench_main.cpp` `bench_batch`, timed read loop):

```
'example::ReadMessage(serialize::ReadStream&, example::Message&)' not inlined into 'main'
    because too costly to inline (cost=1055, threshold=45)     [×2 call sites]
```

Threshold **45 is clang's cold-callsite threshold** (hot sites in the same
function get 250–325+). The batch read loop is a fallible chain — LLVM prices
each Ok/Err split at ~even odds, block frequency decays geometrically, and the
per-message dispatch call site is priced cold. Cost 1055 with no bonus: the
call stays. The measured Studio binary shows the same shape: the timed loop
performs one `bl` to `ReadMessage` per message (2 emitted call sites for the
symbol: timed loop + untimed golden check).

**C batch read** (`bench/c/bench_main.c` `bench_batch`, the same loop shape):

```
'read_message' inlined into 'bench_batch' with (cost=-13285, threshold=250)
    at callsite bench_batch:91:23
```

Function-relative line 91 is the timed read loop (verified against source).
Cost **−13285** is the last-call-to-static bonus (~15000): `read_message` is
`static` in the single bench TU, its other call sites inline first, the timed
site becomes the last call to a local function, and inlining it deletes the
out-of-line copy — so LLVM flattens the whole dispatcher into the loop.
**C's timed batch-read loop contains zero per-message calls.** C++ pays a call,
a prologue, and the death of `__restrict`-derived facts at the boundary, once
per message — and C leads 30% while carrying always-on read validation C++
compiles out.

**The confounder, verified**: inside C++'s out-of-line `ReadMessage`, every arm
reader inlines at FRESH thresholds — `ReadChat` (cost=240, threshold=325),
`ReadTest` (195/325), `ReadHeartbeat` (−40/487), `ReadBlock` (190/325),
`ReadSynchronize` (110/325), `ReadTimescale` (150/325), `ReadMessageType`
(30/325). The decay does not reach inside the outlined dispatcher — outlined
functions restart at fresh entry frequency, exactly as predicted. (C shows the
mirror image: inside its flattened dispatcher, `read_chat` is refused at
cost=385 vs 250 — C wins the row while still paying a per-Chat-message call on
25% of messages, which sharpens the attribution further.)

**Why both rows' `.inline` verdicts said `full`**: the §4.2 verdict counts
calls into the *serialize runtime*; a call to a *generated* dispatcher is
invisible to it. Both sides are runtime-`full`; only C is generated-flat. The
verdict column was never wrong — it measures a different boundary than the one
that differs here.

---

## The ledger

| # | row (Studio O3, C as % of C++ time) | disposition |
|---|---|---|
| 1 | **batch read — C 70% (C leads)** | **CANDIDATE ×2** (schema `cpp-read-spine-demand`, serialize `read-spine-demand`), remark-proven, timing-pending |
| 2 | read — C 142% | **NAMED**: read-validation contract (§3.4 `checks` contract-vs-removed, §7 permanent) + `hdr+tu` bulk packaging (deliberate); C-LTO diagnostic leg isolates the packaging term. C++-side stranded read sites additionally closed by the serialize candidate |
| 3 | write — C 163% | **RESOLVED-UPSTREAM (expected)**: serialize.c v1.3.0 checkless writes (`23dfd3f`, issue #52 ruling) removed the per-op capacity+sticky check the Studio pass measured (v1.2.0). Re-measure; remaining residual (if any) gets a fresh remarks pass at the new baseline |
| 4 | batch write — C 126% | same as row 3; dispatcher shapes are already at parity (both `write_message`/`WriteMessage` out-of-line: C cost=1860 refused at 250/525, C++ cost=1555 refused at 325; both rows `partial:2` on the deliberate bulk-bytes boundary) |
| 5 | bitpacker write — O2 parity, C++ 2.6x at O3 (PERFORMANCE.md flip) | **RESOLVED-UPSTREAM (expected)**: against v1.3.0 the C 16-width write group now FULLY UNROLLS — `bench/c/bench_main.c:1338: peeled loop by 2 iterations` + `completely unrolled loop with 14 iterations` — the checkless write body fits the unroll budget the v1.2.0 capacity check overflowed. C++ unrolls 16 clean. Re-measure |
| 6 | bitpacker read | **NAMED**: C's inner group stays rolled (`bench_main.c:1361`: peel 2 only, no full-unroll remark) while C++ fully unrolls 16 (`bench_main.cpp:1253`). C's `serialize_read_bits` body carries the always-on bounds-safe window + sticky check (contract), pushing the unrolled size over the budget. Contract-by-design: no candidate |
| 7 | rt `bench_packet` read (C++ strand) | **CANDIDATE** (serialize branch): `ReadStream::SerializeBytes` refused into `RtBenchPacket::Serialize<ReadStream>` at cost=115 vs threshold 45 — cleared by `SERIALIZE_READ_SPINE_DEMAND` (verified armed) |
| 8 | cpp `testdata` read `partial:5` | **CANDIDATE** (serialize branch): the five strands are `SerializeInteger64` (cost=70 vs 45), `SerializeBytes` (115 and 130 vs 45), `SerializeAlign` (80 vs 45), `serialize_compressed_float_internal<ReadStream>` (50 vs 45) — all cold-held, the family's decay disease on serialize.h's own read spine, which had declined the demand on a "reads were all full" claim this row falsifies. Four of five cleared by the switch; compressed-float deliberately stays (doctrine: branchy body, cost is the work — recorded residual) |

Rows 3–5 carry a standing caution: PERFORMANCE.md's table crosses safety
contracts (`checks` captions) and the v1.3.0 write ruling *changes the
contract* on the C write side — the re-measure must let the harness re-derive
the `checks` column value rather than inheriting `contract`.

---

## The candidates (staged, default OFF, byte-identical OFF, remark-proven ON)

### Candidate A — schema `cpp-read-spine-demand` @ `d6e3b21`

`SCHEMA_READ_SPINE_DEMAND`, a compile-time switch in the **generated** C++.
Every generated Read function is now spelled `SCHEMA_READ_INLINE`, which
expands to plain `inline` (token-identical to what the emitter always
produced) unless `-DSCHEMA_READ_SPINE_DEMAND` is given, in which case the
generated read path — per-type readers, arm readers, `ReadMessageType`,
`ReadMessage`, object views, `ReadObjectType` — demands inlining
(`always_inline`/`__forceinline`). Emitter (`internal/codegen/cpp/`),
regenerated headers, and goldens land together; the dispatch-surface test now
pins the new spelling.

Proof:
- OFF: full-binary `otool -tv` diff vs main's generated tree — **identical**
  (only the binary's name line differs).
- ON: `'example::ReadMessage(...)' inlined into 'bench_batch()' with
  (cost=always)`; the `ReadMessage` symbol **vanishes** from the armed binary
  (0 emitted call sites, vs 1 symbol + 2 `bl` sites default) — C's flat shape,
  achieved on C++.
- ON: zero `OUTLINED_FUNCTION_*` symbols. The demand is a demand, not a hint;
  no branch weights were touched.

### Candidate B — serialize `read-spine-demand` @ `86f9690`

`SERIALIZE_READ_SPINE_DEMAND`, the same switch discipline in serialize.h.
`SERIALIZE_READ_INLINE` expands to nothing by default; armed, it is
`SERIALIZE_ALWAYS_INLINE` on the read spine — `BitReader::ReadBits`,
`BitReader::ReadAlign`, and `ReadStream::SerializeInteger / Integer64 /
Integer128 / Bits / Bytes / Align` — the exact mirror of the already-landed
write-spine demand set. The header's rationale comment is amended: the "every
read row was already full" claim now names its falsifying exception (row 8).
`__restrict` qualifiers untouched; compressed-float/string bodies deliberately
undemanded (the serialize.c boundary, mirrored).

Proof:
- OFF: bench-leg disasm byte-identical vs serialize main.
- ON: all four demandable strands of row 8 report inlined (`cost=always`);
  row 7's strand cleared; zero outlined functions; test suite (`test.cpp`,
  `SERIALIZE_ENABLE_TESTS`) passes armed and unarmed.

### Combined (A+B armed) — the full candidate shape

One remaining read-side refusal in the whole C++ leg:
`serialize_compressed_float_internal<ReadStream>` into `ReadTestData` — the
recorded doctrine residual. Zero outlined functions. This is the configuration
the timing pass runs as its principal candidate leg.

### The confounder / diagnostic legs (no source change)

- **C+LTO diagnostic** (isolates C's residual `hdr+tu` packaging term, rows
  2/3/4): the C leg built with `-flto` appended to its recorded flags,
  published as a labelled diagnostic leg, never a standard row — precedent:
  `bench/results/2026-08-14-c-lto-diagnostic-arm64-macbook.csv` and
  BENCH-STANDARD §3.1's required external-linkage diagnostic.
- **C++ dispatcher-outlined leg**: this is the DEFAULT build (the boundary
  exists today); the A/B against candidate A isolates the dispatcher-boundary
  term with no hint pollution. No `noinline` legs — forbidden and unnecessary.

---

## Timing protocol (staged; runs on THIS Air in a quiet window after the serialize.js campaign ends)

**Era discipline**: the pass opens a new era on this machine. Its CSV carries
`# era:` comment lines naming the harness commit and both candidate branch
shas (the `1390a41` / issue #20 discipline). It is NEVER ratioed against the
Studio postlane table: BENCH-STANDARD §5.3 rule 8 refuses on differing `# cpu`
lines — verified live this session (`go run ./bench/tools ab <studio-csv>
<macbook-csv>` refuses, naming host `studio`/`macbook` and cpu `Apple M3
Ultra`/`Apple M2`). Air-vs-Air only, within one pass.

**Legs** (each at O3; repeat at O2 per §3.3 if any ranking flips):

| leg | tree | arming |
|---|---|---|
| control-start | schema branch generated tree, serialize main | none (OFF — byte-identical to main, proven) |
| cand-A | schema branch, serialize main | `-DSCHEMA_READ_SPINE_DEMAND` |
| cand-B | schema branch, serialize branch | `-DSERIALIZE_READ_SPINE_DEMAND` |
| cand-AB | schema branch, serialize branch | both defines |
| c-main | serialize.c v1.3.0 main | none — this re-measures rows 3/4/5 against checkless writes |
| c-lto | serialize.c v1.3.0 main | `-flto`, labelled diagnostic |
| control-end | as control-start | none |

Mechanics: `bench/run.sh` with `SERIALIZE`/`SERIALIZE_C` pointing at the
branch checkouts (the §3.5 build-verified plumbing already refuses on
mismatch); arm the defines by passing `--compiler 'c++ -D<SWITCH>'` (run.sh
expands `$CXX_BIN` unquoted, and the compiler line lands in the preamble) and
record the arming in `BENCH_NOISE` as well. Seven interleaved rounds + warmup
per §2.1/§2.4, window gate §2.6 (5% control delta), spread rules §2.3, then
`bench/tools/inline-verdict.sh` for the `.inline` ledger. One heavy process at
a time on this machine; nothing else running.

**Decision rule — what the pass concludes:**

- **cand-AB's batch read reaches or passes the c-main batch read row** (same
  window, same round set) → verdict **(b)** stands: the gap was C's shape not
  yet applied to C++. The switches graduate to a family decision (default-on
  is a separate, measured question — always_inline on a 1000-cost dispatcher
  is a real code-size trade for real consumers).
- **cand-AB is no better (or worse) than control** → the flattening does not
  pay on M-class cores; verdict **(a)**: the named limitation stands as the
  terminal attribution — LLVM prices fallible-chain block frequency
  geometrically and grants the last-call-to-static bonus only to internal
  linkage, and C++ header packaging (linkonce_odr) cannot earn it, while
  flattening by demand buys nothing the boundary was costing. Both branches
  then close unmerged, and PERFORMANCE.md's gap note cites this ledger.
- **Between** → attribute the residual with the isolating legs: A-vs-control
  (dispatcher boundary term), B-vs-control (runtime spine strands term),
  c-lto-vs-c-main (C's packaging term), and the row-8/row-7 verdict flips in
  the `.inline` ledger. Whatever remains after those three named terms is a
  new row in this ledger and starts the loop again.
- Rows 3/4/5 are decided independently by c-main vs the Studio-era C rows'
  *mechanism* (not their numbers — cross-era ratios are refused): if the write
  rows' verdicts and remark signatures now match C++'s shape and the rates
  rank at-or-better within this pass, the rows close as RESOLVED-UPSTREAM.

**PRs**: both branches ride as PRs (default-off, remark evidence in the body,
no timing claims), opened DRAFT — nothing merges until the pass runs. The
branches are the artifact; the PRs are the review surface.

---

## Register

- schema branch `cpp-read-spine-demand` @ `d6e3b21` — pushed; PR mas-bandwidth/schema#47 (DRAFT, default-off).
- serialize branch `read-spine-demand` @ `86f9690` — pushed; PR mas-bandwidth/serialize#74 (DRAFT, default-off).
- serialize.c — untouched; rows 3/4/5 ride on already-released v1.3.0.
- This file is the Phase-4 deliverable and stays out of every repo's main.
