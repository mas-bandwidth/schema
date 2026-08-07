# tiny-floor lane — RED checkpoint (session ended mid-implementation)

**Lane**: the shared Go/Rust tiny-message floor (test 6 B, probe_header 10 B,
probebits where relevant) — Go ~696%/1245% of C++, Rust several multiples, at
the v5 tables. Branches: schema `tiny-floor` (this), serialize.go
`optimize-tiny` (runtime half, COMPLETE and locally green — see below).
serialize.rs untouched so far (branch `tiny-floor` exists at main, empty).

**State: RED — do not merge this schema branch.** The IR planner and the Go
runtime are done and tested; the Go emitter chunk lowering is WRITTEN but NOT
WIRED (generated output is unchanged at this commit — that is why the suite
is green here). Nothing is benchmarked. The Rust emitter half is designed,
not written.

## Banked predictions (written before profiling — quote against outcomes)

- P1: per-field outlined-call machinery carries >= 40% of tiny write
  samples; read side adds a per-call err load + runtime BitsRequired.
- P2: Try-form fused inline emission (zero calls/field) worth test write
  +40–80%, probe_header write +30–60%.
- P3: read twin (flatten ReadStream + poison + TryReadBits) worth test read
  +30–70%, probe_header read +25–50%.
- P4: even at zero-call Go does NOT reach C++ tiny rows (harness fixed cost +
  non-fused stores) — the residual is the floor to write into the ledger.
- P5 (rust): no outlined serialize.rs calls remain in the tiny write path;
  the gap is per-field state-machine memory round-trips + no cross-field
  folding, behind an Fn-shim outlined call per message.
- P6 (rust): WriteStream::new / ReadStream::new per message < 10% each.
- P7 (rust): a folded generated shape (chunked writes) reaches 1.5–2.5x on
  tiny rows within safe Rust; `read_bits_group` (runtime 1.1.0, unused by
  the emitter) is the adoption-shaped read lever.

## Profile convictions (M2, go1.26.5 pprof over bench-shaped loops — clean,
no kevent artifact; rates from profiled runs never quoted)

- test write: outlined `(*WriteStream).writeBits` 39.6% cum (4 calls/msg;
  fused tryWriteBits body 20.7% flat inside it), Flush 19.6%, Reset 4.9%,
  loop/LCG/indirect 13.4%, WriteTest body 8.6%. P1 CONFIRMED.
- test read: outlined `SerializeInt` 49.0% cum — and it recomputes
  BitsRequired(0,1000) at RUNTIME per field (schema#13 folded writes only);
  outlined readBits 11.8%; ReadTest body 21.8%.
- probe_header write: tryWriteBits 30.4% flat, outlined writeBits x3 24.4%,
  **outlined SerializeAlign 12.2% for an align that writes ZERO bits**,
  outlined SerializeBits64 27.0% cum.
- probe_header read: outlined readBits x3 38% cum, SerializeBits64 17.6%,
  zero-bit SerializeAlign+AlignBits ~8.4%, Err loads 2.5%.

Rust (disassembly of bench binary at mains, no LTO): P5 CONFIRMED with a
twist — write_test/read_test bodies are fully inlined (~110 live instrs, only
cold panic calls) BUT sit behind one outlined Fn-shim call per message, and
every field round-trips scratch/scratch_bits/bits_written through MEMORY
(unwind edges from the bounds-checked store). scratch_bits=0 never
constant-folds because the body is compiled entry-position-unknown. That is
the whole-header-folding gap mechanically.

## What landed where

### serialize.go branch `optimize-tiny` (COMPLETE, locally green: build,
vet, full test suite incl fuzz corpus)

1. `TryWriteBits` exported (inline cost 64), NEW `TryWriteBits64` (62; one
   spill store max, wire identical to any smaller-piece split), `Fail` (20)
   — write side.
2. **ReadStream FLATTENED** (BitReader fields directly on the stream — the
   read twin of #19's flat writer; wrapper forms cost 84–93 vs budget 80,
   flat `TryReadBits` lands at exactly 80). `Fail` (21). `fail` now POISONS
   numBits=-1 (read twin of the write-side poison; all public methods still
   consult s.err first — behavior unchanged, verified by suite).
3. `SerializeAlign` both streams reshaped to inline (write 78 / read 77 via
   `int(-bits)&7` two's-complement align math): an ALIGNED stream pays zero
   call — kills the 12%/8% probe_header zero-bit-align line item with no
   emitter special-casing, correct at any embedding offset.

Verified mechanism (scratch main, `-gcflags=-m`): generated-style call sites
inline TryWriteBits/TryWriteBits64/TryReadBits(+rawReadBits recursively)/
Fail — ZERO calls per field. Round-trip byte-correct.

### schema branch `tiny-floor` (this branch)

1. `internal/ir/runs.go` + `runs_test.go` (green): target-independent
   fixed-run planner. RunEligible (const/reserved/ranged-int32/64-family/
   bare ints/bits/bool/bare floats/enums/flags; arrays, strings, aligns,
   compressed floats, nested structs, 128s, full-range-unsigned BREAK runs).
   PlanRun packs onto chunks: write cap 64, read cap 32 with
   **cutAtFallible** — a chunk never extends past the end of a fallible
   element, which preserves read error identity EXACTLY (argument in the
   file header; SPEC line ~1650 "truncation surfaces as the stream's own
   error" is the doctrine; the bit-flip malformed-agreement gate is
   full-length and unaffected).
2. `internal/codegen/golang/runs.go` (compiles, UNWIRED): emitWriteRun /
   emitReadRun — write: refusal checks in wire order per chunk, masked
   uint64 temps runV<i>, chunk build, `if !stream.TryWriteBits64(c, N) {
   stream.Fail(ErrOverflow) }` LATCH-AND-FALL-THROUGH (matches existing
   writes-latch/checks-return precedence and final `return stream.Err()`);
   read: `runChunk<k>, ok := stream.TryReadBits(N)` + immediate
   `return stream.Fail(ErrOverflow)` on false, then per-op extract/validate/
   assign in wire order (const/reserved -> ErrValidation returns; ranged/
   enum -> stream.Fail(serialize.ErrValueOutOfRange); unsigned-domain
   min-add reconstruction matching SerializeInt/Int64 exactly, two's
   complement uint32(min)/uint64(min) literals).

## Remaining (in order)

1. WIRE the run emission: in `functions.go` emitWriteItems/emitReadItems,
   gather maximal runs via `gatherRun` and dispatch to
   emitWriteRun/emitReadRun; non-eligible items keep existing paths. Arrays/
   strings/objects/dispatch stay untouched this pass (scope).
2. Point bench/go go.mod + test/go at the serialize.go branch worktree
   (../../../serialize-cs-port/serialize.go is the sibling the harness
   builds against — for local measurement use a replace to
   /Users/glenn/rowan-working/serialize-gotiny, RESTORE before PR).
   Regenerate (`make generate` or the schema compiler over examples/),
   `git diff --stat -- testdata/wire/` MUST be empty, full test suites
   (test/go against BOTH old and new runtime for compat), then paired
   median-of-7 bench legs: A=mains, B=runtime alone, C=emitter alone,
   D=composed (the go-writes doc's four-leg shape,
   bench/results/2026-08-07-go-writes.md is the template).
3. Rust emitter twin in `internal/codegen/rust/`: same planner, write
   chunks via existing `serialize_bits`/`serialize_bits64` (trusted write
   path: no Try needed; read chunks via serialize_bits with `?` and the
   same cutAtFallible rule + validations). Then measure; only if the
   state-machine-per-chunk still dominates, consider serialize.rs
   `BitWriter::write_bits64` (semver-minor; `read_bits_group` exists
   already for reads) and/or `#[inline(always)]` scoped to tiny wire fns.
4. Full four-language `bench/run.sh` sweep at composed state, zero-regression
   check across all 96 rows, results doc + gap-ledger update (floor
   verdict for whatever residual remains vs C++ — P4).
5. PRs: serialize.go `optimize-tiny` (CI incl spec-sync), schema
   `tiny-floor` (suite-green), serialize.rs only if 3 demands it. Merge on
   green per the standing grant, commits as Rowan.

## Documented divergences to carry into the PR bodies (all argued unpinned;
suite/goldens/malformed-gate unaffected)

- Truncated-AND-invalid packets inside one read chunk would have reported
  the validation error where they now report ErrOverflow — PREVENTED by
  cutAtFallible (kept exact). What DOES change: post-error contents of
  later fields (old code zero-assigned them, chunked code leaves them
  stale) and BitsProcessed after a failed read (chunk granularity).
- A refused write (range) simultaneous with buffer exhaustion in the same
  chunk returns ErrValueOutOfRange as today, but the stream may not carry
  the ErrOverflow latch the old interleaved write would have latched.
- Bench context: baseline reproduced v5 within noise same-sitting (test
  105.02 w / 85.24 r; probe_header 80.91 w / 72.87 r M msg/s, load ~5.6
  with other lanes running — all comparisons must stay paired same-sitting).
