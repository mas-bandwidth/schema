# Tail-only union-arm zeroing — SPEC proposal

**STATUS: PROPOSAL — DO NOT MERGE. The decision is Glenn's.** No code changes ride this
branch; this document is the exact wording change, the safety argument, the honestly
bounded win, and the open questions. It assumes PR #3 (`optimize-read-zeroing`) merges
first — everything below builds on its rule that an arm's storage is zero-established at
selection.

## 1. The residual this prices

PR #3 removed the whole-union memset (2016 B per `Message` construction) and moved zeroing
to arm selection: `message.chat = Chat{};` before `ReadChat`. That was the #1 hotspot
(60.6% of batch-read self-cycles in `rep stos` on the EPYC; batch read improved 1.80-2.11x
across the three host/compiler pairs).

The residual: **selection still zeroes `sizeof(arm)` regardless of how little the decode
will write.** Measured shapes (generated `Messages.h`, arm sizes by `sizeof` on arm64/x86_64
clang):

| arm | sizeof | typical wire payload | zeroed today at selection |
|---|---:|---|---:|
| `Chat` | 264 B | ~13 B pinned chat; 16-31 B batch chats | 264 B |
| `Block` | 2004 B | 64-191 B in the batch mix | 2004 B |
| `Test` | 8 B | 6 B | 8 B |
| `Synchronize` | 16 B | ~10 B | 16 B |
| `Timescale` | 16 B | ~13 B | 16 B |
| `Heartbeat` | 1 B | tag only | 1 B |

After PR #3, the EPYC g++ whole-run profile still shows `__memset_avx512_unaligned_erms`
at **1.69%** of cycles (down from 6.86%) — the per-arm re-init is the bulk of what remains.

## 2. The tail-only variant

**Zero only the bytes of the selected arm that the successful read will not write.** Every
other byte is written from the wire by the decode itself. Concretely, per arm, from the
schema (this is the field-coverage analysis of §4):

- fields on the unconditional decode path (fixed-width scalars, length/count prefixes,
  nested composites) are always written by a successful read — **no pre-zeroing needed**;
- `string(N)` / `bytes(N)` buffers: the decode writes bytes `[0, length)` (plus the
  terminator for strings); bytes `[length, N)` must read as zero on success (§5), so the
  generated read zeroes exactly that span **after** the length prefix is decoded;
- counted arrays `[<= N]T`: elements `[count, N)` element-wise zeroed after the count is
  decoded (§5's corner-pins);
- fields in untaken branches: explicitly zeroed on the not-taken path (§5).

For arms whose coverage is total (`Test`, `Synchronize`, `Timescale`, `Heartbeat` — every
byte written on success), selection zeroing disappears entirely. For buffer arms (`Chat`,
`Block`), the semantics-required tail memset remains; what is saved is only the
double-touch of the used prefix.

The observable post-condition on **read success is unchanged**: every byte of the arm is
either the decoded wire value or zero. PR #3's pinned stale-leak test (write a maxed
`Block` of `0xFF`, then a 2-byte `Chat` into the same reused `Message`; assert every
`chat.text` byte past the terminator is zero) **must stay green unchanged** — it is the
acceptance gate for this proposal, not a casualty of it.

## 3. The exact SPEC wording change

Two edits, both building on PR #3's text.

**(a) §6.1 C++ union surface (the sentence PR #3 rewrote).** Current (post-#3):

> constructed as the None message (the tag initializes to `None`; an arm's storage is
> established ZEROED when the arm is selected — by `ReadMessage` before it decodes, per
> §5's zero-baseline read rule, or by a writer assigning the arm)

Proposed replacement:

> constructed as the None message (the tag initializes to `None`; when an arm is selected
> the implementation establishes §5's zero baseline for exactly that arm — either by
> zeroing `sizeof(arm)` at selection, or **tail-only**: zeroing precisely the bytes the
> decode will not write, derived per arm from the schema by field-coverage analysis
> (buffer bytes past the decoded length, elements past the decoded count, untaken branch
> fields). Both are conforming; the post-condition on read success is identical — every
> byte of the arm is the decoded wire value or zero. A writer assigning the arm
> establishes the baseline by whole-arm zeroing, as before.)

**(b) §5, the scope paragraph.** Current text ends:

> Whole-object comparison in the conformance matrix is defined over a fresh output or the
> used prefix; if tail-clearing on read is ever wanted instead, it is a generated-code
> change, priced then. Write reads only taken fields.

Proposed addition (immediately before "Write reads only taken fields."):

> For a union arm there is no fresh-output baseline to lean on — the previously active
> arm's bytes are not zero — so the union read establishes the baseline itself (§6.1),
> either whole-arm at selection or tail-only; a successful union read therefore never
> exposes bytes it did not itself write or zero.

Note what is deliberately **not** changed: §5's reused-plain-object stale-tail convention
("priced then") stays priced-not-taken for non-union reads; read failure stays
"unspecified state"; §4.8's behavioral-only contract across targets is untouched (field
values after a successful read are bit-identical under both zeroing strategies, so
Go/Rust/C# need no change).

## 4. The safety argument

**No stale-byte exposure on success.** The field-coverage analysis is a compile-time
partition of the arm's bytes, per arm, from the schema alone: (written unconditionally) ∪
(written-or-zeroed conditionally: buffer/array/branch spans, each with its zeroing emitted
adjacent to the decode of its length/count/branch flag) ∪ (padding — open question 1).
Every byte is in exactly one class; the emitter can assert the partition covers
`sizeof(arm)` at generation time. A successful read touches every class. This is
mechanical, auditable, and testable — the existing stale-leak pin plus a per-arm
exhaustive variant (previous arm filled `0xFF`, every arm type read over it, every
non-written byte asserted zero) makes it regression-proof.

**Read failure.** §5 already says failure leaves the output unspecified and callers may
use it only on success. Under whole-arm zeroing, a failed read still happened to leave no
previous-arm bytes visible (the zero ran first). Under tail-only, a decode that fails
midway can leave previous-arm bytes in spans whose zeroing had not yet been reached. Two
options, priced:

- **(i) keep §5 as is** — failure = unspecified, full stop. Zero cost. The no-exposure
  property on failure was never contractual, only incidental.
- **(ii) zero the arm on the failure path** — `ReadMessage`'s failure return re-zeroes the
  selected arm (or resets the tag to `None`). Failure is the cold path (malicious or
  corrupt input), so this costs nothing at steady state and keeps the incidental property
  as a real one. **Recommended**, because Chat/Block payloads are exactly the kind of data
  (user text, opaque blobs) whose cross-message leakage through a buggy caller would be a
  real disclosure bug, and the price is a memset on a path that never runs hot.

## 5. Expected win — bounded by PR #3's measurements, not promised

Honesty first: **for the motivating example itself (a ~13 B chat in a 264 B arm) the
strict tail-only win is small**, because §5 requires the buffer tail zero on success —
~235 of Chat's 264 B still get zeroed; only the used prefix stops being double-touched.
The full win cases are total-coverage arms (zeroing drops entirely — but those arms are
8-16 B) and buffer arms decoded near capacity.

Arithmetic over the measured batch mix (bench `message_batch`: 25% Chat 16-31 B text, 25%
Test, 15% Synchronize, 15% Timescale, 10% Heartbeat, 10% Block 64-191 B):

- selection-zero traffic today: 0.25·264 + 0.25·8 + 0.15·16 + 0.15·16 + 0.10·1 + 0.10·2004
  ≈ **273 B/msg**;
- under tail-only: ≈ 0.25·234 + 0.10·1872 ≈ **246 B/msg** — a **~10% reduction in zeroing
  traffic** for this mix.

Scale: PR #3's after-state batch read is 67.55 M msg/s (M2), 40.86 (EPYC g++), 26.03
(EPYC clang) — 14.8/24.5/38.4 ns per message — and the EPYC profile attributes ~1.69% of
whole-run cycles to the remaining memset. If the entire selection-zeroing cost vanished,
the recovery is roughly that memset share concentrated in batch read — order 5-15% of
batch-read time. Strict tail-only recovers ~10% of the zeroing, so the estimate is
**~1-2% on batch read for this mix** — within the noise band of the EPYC box and barely
outside it on the M2. A mix heavy in total-coverage arms or near-full Blocks does better;
a mix of short chats in big buffers does not.

If that number is the whole story, the honest conclusion may be "not worth the emitter
complexity" — that conclusion is Glenn's to draw, which is why this is a proposal and not
a PR with code. Any implementation would repeat PR #3's methodology: predictions banked,
paired before/after, both hosts, stale-leak pins green.

## 6. Open questions for Glenn

1. **Padding bytes.** Whole-arm zeroing zeroes struct padding (Chat has 3 pad bytes);
   field-wise tail-only leaves padding unspecified after an arm assignment. Nothing
   contractual compares padding today (conformance compares fields; goldens compare wire
   bytes), but if any future tooling memcmps arms, tail-only breaks it. Is field-level
   equality the contract (my reading of §4.8 "behavioral only"), or should arms stay
   memcmp-clean?
2. **Failure path** — option (i) or (ii) of §4? Recommended (ii): re-zero on failure,
   cold path, keeps the no-exposure property real instead of incidental.
3. **Is ~1-2% on the measured mix worth the emitter surface?** The tail-only emission
   (post-length memsets, past-count element zeroing, untaken-branch zeroing inside union
   arms) is new generated-code machinery in one target. If the answer is "not at that
   size", is the sharper variant even on the table: extending §5's used-prefix stale-tail
   convention to union arms (zero nothing semantics-required-only)? That recovers the
   full 5-15% but changes observable behavior, breaks the new stale-leak pin, and
   re-opens cross-message byte exposure through reused arms — the exact trade PR #3's
   test was written to forbid. I do not recommend it; it is listed because the pricing
   would be dishonest without naming where the rest of the cycles live.
4. **Scope across surfaces.** The C++ variant surface (`emplace<T>()`) value-initializes
   the whole arm today — should it stay whole-arm (simplest; its cost was never the
   hotspot) while the union surface goes tail-only? Go/Rust/C# have different storage
   baselines and need no change for field-level parity — confirm that field-level parity
   is the bar (§4.8).

— Rowan, 2026-08-06. Companion evidence: PR #3 body and its run CSVs; bench-harness
baseline doc `bench/results/2026-08-06-baseline.md` (hotspot 1); the EPYC perf residual
quoted from PR #3's "Perf confirmation" section.
