# 2026-08-07 — the attributed gap ledger

The study's capstone: per language, the remaining gap to C++ decomposed into
line items, per the doctrine — *"if a language is slower we should know why,
have a theory for why and prove it."* Every item below carries either a
proven cause with its measurement and PR, or an honest **unattributed, next
profile target**. Numbers are from v3 (`2026-08-07-four-language-v3.md`,
M2, the post-merge mains pinned there) unless a PR pairing is the only
measurement of an isolated effect — then the PR is cited and said so. Where
v3 moved a number relative to an earlier doc, v3 is preferred and the move
is stated.

**The v3 scoreboard** (M2, time relative to C++ = 100%, medians across the
11 corpus benches; batch separately; EPYC deferred on Glenn's word):

| backend | write | read | batch write | batch read |
|---|---:|---:|---:|---:|
| Rust | 100% | 238% | 121% | 303% → **138%** |
| C# | 251% | 353% | 151% | 172% |
| Go | 305% | 386% | 239% | 176% |

**Cell updated after v3** (2026-08-07, schema#10): the Rust batch read moved
303% → 138% when the harness batch read loop adopted `read_message_into`
(the ledger item below, now closed) — paired same-sitting M2 run beside
same-run C++, CSVs `2026-08-07-rust-read-into-{before,after}-arm64-m2.csv`.
Every other cell is v3's.

---

## Rust

**Bottom line: the write gap is CLOSED at the corpus median (174% → 100%),
and the batch-read gap closed most of the way when `read_message_into` was
adopted (303% → 138%, schema#10); what remains is tiny-message C++
constant-folding (measured, partly a policy floor) and a read column that
never got a read-shaped pass.**

### Closed today

| fix | measured | PR |
|---|---|---|
| `#[inline]` on Stream trait default methods + `bits_required*` | ship_shallow write 1.96x, inputpacket write 1.89x, probebits read 1.85x, testdata write 1.57x, rigidbody_moving read 1.34x (paired M2) | serialize.rs#19 |
| `#[inline]` on every generated wire function | combined with #19: inputpacket write 2.11x, probearray read 2.16x, shipcreate write 1.71x, chat write 1.66x vs pre-both baseline | schema#5 |
| `WriteStream::write_bytes(&[u8])` + emitter borrow-in-place (kills the 256 B–2 KB per-message array copy, `sub sp, #0x7d0` + memcpy gone) | chat write +74–78%, message_batch write +32–49% (paired M2) | serialize.rs#20 + schema#6 |
| `read_message_into` adoption in the batch read loop (ONE hoisted `Message`, the Go/C# MessageStorage discipline; the into-path now byte-verified against the pinned wire in `test/rust` and the bench self-check) | message_batch read 22.29 → 50.10 M msg/s (**2.25x** paired M2), batch read 306% → **138%** of same-run C++. Refutation recorded: the banked ~2.6x/~114% (schema#5's 58.57 side experiment) was NOT reached at the composed mains — 2.25x/138% is the measured close; the residual to 58.57 is unattributed (composition/layout; #5's number was beside a different runtime state). Side movement, also unattributed: batch WRITE rose 1.15x (68.29 → 78.28) on a byte-identical write loop with flat cpp/go/cs controls — binary layout is the suspicion, recorded, not claimed | schema#10 |

v3 confirms the composition against v2: inputpacket write 2.13x, probearray
read 2.10x, ship_shallow write 1.94x, chat write 1.80x, probebits read
1.60x, testdata write 1.55x, batch write 1.27x. Nothing regressed beyond
spread.

### Intrinsic floors, with evidence

- **Bounds-checked word store behind `unsafe_code = "forbid"` — a floor BY
  POLICY.** PR #20: safe Rust cannot produce `&mut` from `&T` without a
  copy, and the forbid stands "in the runtime and in generated code"; PR #5
  states the write path's bounds-checked shape is "a deliberate boundary
  this pass does not cross." The policy is Glenn's ruling to revisit, not a
  compiler limit.
- **apple-clang's whole-header constant folding on tiny messages —
  measured.** Post-#19/#5, probe_header write sits at 0.19x same-run C++
  (schema#5's banked-prediction refutation); v3 reproduces it exactly:
  192.82 vs 990.53 M msg/s = 0.19x, test write 0.31x. The C++ header
  compiles into the caller as one folded unit; rust's crate boundary +
  bounds checks do not, even fully inlined.

### Named-but-open

- **The read column (238%) never had a read-shaped pass — unattributed,
  next profile target.** The day's rust wins were write-heavy; ship_shallow
  read did not move all day (38.27 v3 vs 38.75 v2) while its write doubled,
  and reads now trail writes on ship_shallow (0.47x), inputpacket (0.48x),
  shipcreate (0.83x), testdata (0.67x). serialize#25 measured the branchless
  reader gains nothing from const folding, so the cause is elsewhere. Next
  step: perf the rust read runners on a quiet core (EPYC when it returns,
  or Instruments on the M2) before theorizing.
- **Bulk-bytes for the rust emitter.** C++ took testdata read +70–80% by
  replacing the `[17]uint8` per-byte loop (schema#7); the rust generated
  code still loops byte-at-a-time (named "adjacent, untouched" in schema#6),
  and v3 testdata read is 10.03 vs C++ 25.54 (255%). The alignment proof
  lives target-independent in `internal/ir` (schema#7 put it there for
  exactly this adoption); the runtime write half (`write_bytes`) already
  landed in serialize.rs#20. Next step: rust emitter emits the bulk path at
  statically-aligned sites.

---

## Go

**Bottom line: the read program worked — read parity is CLOSED (reads beat
writes on all 12 benches, read median 594% → 386%) — and the write column
(305%) is now the honest frontier, with no profile on record.**

### Closed today

| fix | measured | PR |
|---|---|---|
| Read path: zero-padded tail window + `readBits` under the inline budget (cost 83→64; `tryReadBits` at exactly 80) — one call per field, parity with writes | reads +14–61% M2 paired (rigidbody_moving +61%, rigidbody_at_rest +48%, inputpacket +43%) | serialize.go#17 |
| (context) harness v2 removed the per-iteration heap alloc + zeroing the v1 read loops charged Go (~27% of the v1 read profile) | go reads 1.14–3.24x v1→v2 | schema#1 / v2 doc |

v3 vs v2 composition: inputpacket read 1.87x, rigidbody_moving 1.81x,
rigidbody_at_rest 1.80x, probebits 1.40x, test 1.25x, batch 1.22x.

**The v2 doc's read-parity residue is refuted by v3 — prefer v3.** The v2
doc listed rigidbody/inputpacket/probearray/testdata reads as still slower
than their writes; at the v3 mains all twelve benches read at 1.09–2.26x
their write speed. #17 plus the harness fix together did what neither did
alone. Closed, by measurement.

### Intrinsic floors, with evidence

Nothing on record is *proven* as a Go floor yet — stated plainly rather
than invented. Two evidenced candidates, unproven as floors:

- **The gc inliner budget shapes the ceiling.** PR #17's whole mechanism
  was fitting under cost 80 (`cannot inline (*BitReader).readBits: cost 83
  exceeds budget 80`); the port's hot paths live at the budget's edge by
  construction. Evidence that the constraint binds; not yet evidence that
  no further headroom exists under it.
- **No `unsafe` in the repo** (PR #17: "No unsafe (repo has none)") — a
  policy stance parallel to Rust's forbid, never yet measured as the
  binding constraint on any row.

### Named-but-open

- **The write column — unattributed, next profile target.** The day's Go
  program was read-only: v3 writes are 0.97–1.01x v2 on every bench, and
  the write median stands at 305% of C++. No write profile exists. Next
  step: pprof the write runners on Linux in a quiet window (the macOS
  profile mis-attributes ~80% of samples to `runtime.kevent` on Apple
  Silicon — PR #17 — so this waits on the EPYC or another Linux box).
- **chat read absolute: `SerializeString` allocation.** PR #17 predicted
  and measured chat read +0% because the string read is
  allocation-dominated; v3 chat read is 70.15 vs C++ 133.12 (190%). A
  serialize.go strings PR is in flight in another session — out of v3 by
  the pin rule; re-bench when it lands.
- **Bulk-bytes adoption** (same item as rust): v3 testdata read 7.25 vs
  C++ 25.54 (352%); the per-byte `[17]uint8` loop is generated Go too.
  Next step: adopt `ir.AlignedFixedByteArrays` in the go emitter.

---

## C#

**Bottom line: inlining recovered the v2 dip and more (test write back to
1.18x its v2 level), the batch runtime that fixes the *proven* remainder is
landed but unadopted by the emitter, and the heap-field shape beyond batch
scope is the measured floor.**

### Closed today

| fix | measured | PR |
|---|---|---|
| AggressiveInlining on hot stream/packer methods | test write 1.18x, probe_header read 1.19x, probebits write 1.20x, message_batch write 1.17x (paired M2) | serialize.cs#2 |
| WriteBatch/ReadBatch: register-resident stream state (runtime, additive, opt-in) + fixed-point/128-bit parity | shipped batch 1.7x the class path on the probe_header-shaped loop (174.5 vs 104.9 M ops/s); zero effect on stock generated code by design (A-vs-B sweep 0.977–1.020) | serialize.cs#3 |

v3 vs v2: test write 1.18x, test read 1.17x, probe_header read 1.14x,
message_batch write 1.21x, probebits write 1.07x (the PR's 1.20x was paired
against a lower same-sitting before; the cross-session v2→v3 number is the
honest one).

### Intrinsic floors, with evidence

- **Residual JIT heap-field shape beyond the batch scope.** PR #2's
  refutation: after full inlining, `_scratch`/`_scratchBits`/`_bitsWritten`
  are heap fields "reloaded and stored around every inlined write, where
  C++ keeps them in registers" — probe_header write moved only 1.02x under
  inlining alone. PR #3 priced the whole remainder: class 104.9 vs
  locals-ceiling 562.1 M ops/s on the probe_header shape; the shipped batch
  reaches 174.5, and the gap to the ceiling is the per-batch fixed cost
  (stream Reset, state capture/restore, Flush — heap hits once per
  message), which only widening batch scope beyond one message can
  amortize. At per-message scope this is a measured floor.
- **Batch is inline-only and per-type — hazard findings, PR #3.** An
  address-exposed `ref WriteBatch` kills enregistration for the whole
  scope: 0.71x, slower than no batch (64.75 vs 90.87 M msg/s) — generated
  batch cores MUST carry AggressiveInlining. And chat (bulk-dominated)
  measured 0.91x write / 0.94x read in batch form — bulk types must stay
  on the stream path. Both constraints bind any emitter adoption.

### Named-but-open

- **Emitter batch opt-in by scalar density.** The prototype emitter
  measured probe_header write 1.285x, test read 1.168x, test write 1.106x,
  probe_header read 1.144x over the #2 state (PR #3, config C/A); nothing
  shipped emits batch-form yet, so v3 contains none of it. Next step: the
  schema C# emitter emits AggressiveInlining batch-form cores for
  scalar-dense types only (PR #3's two rules), then the batch-scope
  widening experiment (one Begin/End per packet).
- **Bulk-bytes adoption** (the third emitter on the same item): v3
  testdata 6.68 write / 7.55 read vs C++ 20.36 / 25.54 (305% / 338%).
- **The read column (353%) beyond the items above — unattributed, next
  profile target.** C# reads improved only via #2's inlining; no C# read
  profile post-#2 exists. The batch-read *bench* column (172%) is C#'s
  best, but per-message reads on mid-size shapes (rigidbody_moving read
  16.94 vs C++ 85.47 = 505%) have no attributed decomposition. Next step:
  perf with `DOTNET_PerfMapEnabled=1` on Linux (PR #2's method) over the
  read runners, quiet window.

---

## C++ (the reference — what it banked, and its open items)

### Closed today

| fix | measured | PR |
|---|---|---|
| Union dispatch: construction = None, arm zeroes at selection | batch read 1.80–2.11x for per-message-construction usage (M2 2.11x, EPYC g++ 1.87x, clang 1.80x); memset 6.86%→1.69% of cycles | schema#3 |
| Bulk-bytes for statically aligned `[N]uint8` | testdata read +70–80%; write +5.6% alone, +14.4% paired with serialize#27 | schema#7 |
| Const-emit fold (generation-time min/max/bits) | probebits write +14.7%, testdata write +12.5%, probearray +6.5%, inputpacket +5.2%, batch +4.3%, shipcreate +3.3%; the template variant (V1) disqualified by measurement on both compilers | schema#8, `2026-08-06-const-emit.md` |
| WriteBytes head/tail packing | chat write +13.1% isolated M2 pairing (but see open item 1), +22.9% EPYC g++; testdata write +8.8% Linux clang | serialize#27 |

Note on schema#3 vs the harness: v3 batch read is 1.00x of v2 because the
v2+ harness hoists one reused `Message` — the constructor memset #3 removed
was already amortized out of the measured loop. #3's 1.80–2.11x is what
code constructing a `Message` per read gains; the PR pairing is its
measurement of record.

### Open items

- **serialize#27's chat-write win did not survive composition —
  unattributed, next profile target.** Isolated pairing: +13.1% M2
  (102.88→116.37). v3, all merges composed: chat write 104.21 = 0.98x v2 —
  the win is absent. Suspicion (labelled as suspicion): inlining/layout
  interaction in the composed `serialize.h` (#25's templates + #26's new
  operations + #27 together). Next step: profile chat write at the v3
  mains against #27's isolated base; check WriteBytes' inlining state in
  the composed header. testdata write, the other #27 beneficiary, DID
  compose (+21% v2→v3 with #7/#8).
- **serialize force-inline follow-up.** `2026-08-06-const-emit.md` names
  it: force-inline attributes on serialize's const-param templates "would
  make the template forms viable for generators and humans alike" — the
  lever that could reopen #25's direct-call design (V1 lost to outlining,
  not to the constants). A serialize-repo decision.
- **Chat-arm re-init: tail-only union-arm zeroing.** schema#4, OPEN,
  Glenn's decision — priced at ~1–2% batch read for the measured mix
  (selection still zeroes `sizeof(arm)`: Chat 264 B for a ~13 B message);
  the proposal document is the pricing.
- **g++ rigidbody_at_rest layout artifact** (const-emit md follow-up): a
  repetition-stable ~-15% on byte-identical generated write source —
  wants a quiet-x86 profile; EPYC deferred, so it waits.

---

## Sources

PR bodies quoted: serialize#25/#26/#27; serialize.go#16/#17;
serialize.rs#18/#19/#20; serialize.cs#1/#2/#3; schema#1–#8 (schema#4 open
by design), schema#10 (the post-v3 batch-read close). Results docs:
`2026-08-06-baseline.md`, `2026-08-06-four-language.md` (v1),
`2026-08-06-four-language-v2.md` (v2), `2026-08-06-const-emit.md`,
`2026-08-07-four-language-v3.md` (v3) and its CSV, and the paired
`2026-08-07-rust-read-into-{before,after}-arm64-m2.csv`. Every ratio quoted
from v3/v2/v1 is recomputable from the committed CSVs; every
isolated-effect ratio cites its PR pairing.
