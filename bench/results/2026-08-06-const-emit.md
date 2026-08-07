# 2026-08-06 — const-emit: schema-constant bounds folded on the C++ write path

The last queued C++ ceiling lever: serialize PR #25 (`const-params`) proved
min/max/bits can move to compile time with byte-identical wire. This pass
makes the schema C++ emitter deliver that for generated code — and the
measurements overturned the expected design along the way. Two variants were
built and measured; the second one shipped.

- **V1 — template forms** (not shipped): generated code calls
  `serialize::SerializeIntConst<Min,Max>` / `SerializeBits*Const<Bits>`
  directly, emitting its own return-false. The design ratified from #25's
  results.
- **V2 — generation-time fold** (shipped): the GENERATOR is the constexpr
  evaluator. Ranged-int writes emit the offset from min in a bit count
  computed at generation time through the always-inline `write_bits` macro,
  plus the range assert the runtime form carried (debug parity):

      serialize_assert( int32_t( value.x ) >= int32_t( -8388608 ) && ... );
      write_bits( stream, uint32_t( value.x ) - uint32_t( -8388608 ), 25 );

  No runtime `bits_required`, no min/max parameter traffic, no new call
  boundary. Reads stay on the runtime macros — #25 measured the branchless
  reader has nothing to gain, and nothing here contradicted that.

**Wire identity held everywhere**: `git diff -- testdata/wire/` empty after
`SCHEMA_UPDATE_WIRE_GOLDENS=1` regeneration, and every bench run's golden
self-check gate passed on both hosts, both compilers, all three builds.

## Predictions banked before measuring (from #25's M2 numbers)

| prediction | outcome |
|---|---|
| bits-heavy writes (probebits, probe_header) up double digits | probebits **+14.7%** CONFIRMED (but see attribution below — the win is the outlined runtime `SerializeInteger64` dying, not raw-bits fields); probe_header +0.6% = neutral, REFUTED for that row |
| ranged-int-heavy writes neutral on M2/clang | CONFIRMED for V2 (test +0.0%, ship_shallow +0.9%); V1 REFUTED it spectacularly (-33.5% ship_shallow) |
| mixed (message_batch) +0..10% | +4.3% CONFIRMED |
| reads unchanged | CONFIRMED (deltas within spread / layout noise) |
| g++ outlining theory: constant-specialized instantiations rescue EPYC tiny writes, the biggest expected payoff | **REFUTED** — V1 on g++ loses 19-27% on the very rows it was meant to rescue; see below |

## M2 (Apple clang 21, quiet, no pinning) — median-of-7, msgs/sec, WRITE path

| bench | main | V1 templates | V2 fold (shipped) |
|---|---|---|---|
| rigidbody_moving | 23.7M | +2.1% | +0.7% |
| rigidbody_at_rest | 45.1M | -2.0% | -2.0% (before-spread 4.8%) |
| chat | 109.1M | -0.4% | -0.6% |
| test (tiny: 16 bits + 3 ranged ints) | 749.2M | +1.7% | +0.0% |
| inputpacket | 37.6M | **-16.6%** | **+5.2%** |
| shipcreate | 68.1M | **-19.3%** | **+3.3%** |
| ship_shallow (10 ranged ints) | 75.0M | **-33.5%** | +0.9% |
| probe_header | 1068.5M | -1.5% | +0.6% |
| probebits | 83.1M | **+17.7%** | **+14.7%** |
| probearray | 44.9M | -10.0% | **+6.5%** |
| testdata (everything message) | 18.3M | **-31.5%** | **+12.5%** |
| message_batch | 82.4M | -12.8% | **+4.3%** |

Read rows: V2 within noise everywhere (largest excursions carry the largest
spreads; the read code is byte-identical to main).

**Why V1 lost on clang**: `nm` on the V1 binary shows 14 outlined
instantiations (`SerializeIntConst<-8388608, 8388608>`,
`SerializeBitsConst<1>`, ...). Instantiations shared by several call sites —
repeated bounds are the norm in real schemas (3 position components, 4
rotation components...) — fall out of clang's inlining, and the by-reference
value parameter then forces a stack round-trip per field, where main's macro
expansion kept the bit writer's scratch state in registers across
consecutive fields. The `main` binary has exactly ONE outlined write-path
stream call: `WriteStream::SerializeInteger64` (runtime `bits_required64`
loop inside) — and probebits, the one bench that uses it (the
`sensor [0, 4294967295]` field), is exactly where both variants win big.
The V2 binary has ZERO outlined write-path serialize calls.

## EPYC (g++ 13.3, shared core 0, NOISY) — best-of-reps, WRITE path

NOISY window: the game server held isolated cores 1-15 at ~372% CPU
throughout; same-binary medians swing up to 23% across repetitions
(test/main: 108M, 108M, 133M). Only repetition-stable deltas are called.

| bench | main (best of 3) | V1 templates (best of 2) | V2 fold (best of 2) |
|---|---|---|---|
| chat | 35.9M | +0.6% | -0.3% |
| inputpacket | 32.0M | **-19.4%** | -0.1% |
| message_batch | 65.5M | **-10.8%** | -6.3% |
| probe_header | 124.3M | -0.3% | -2.5% |
| probearray | 29.3M | -4.9% | +2.2% |
| probebits | 84.5M | **-27.2%** | -1.1% |
| rigidbody_at_rest | 48.7M | -13.4% | -15.7% |
| rigidbody_moving | 25.8M | **-24.3%** | -5.7% |
| ship_shallow | 57.1M | -1.1% | -2.0% |
| shipcreate | 33.2M | -0.2% | -0.1% |
| test | 133.2M | +0.5% | -11.6% |
| testdata | 9.4M | +0.6% | -0.9% |

**The g++ theory, tested directly and refuted**: the four-language bench
found g++ outlines per-field stream calls (up to 4x on tiny writes), and the
program's hypothesis was that constant-specialized template instantiations
are the fix. Measured: g++ 13.3 actually INLINED nearly every V1
instantiation (`nm`: one isra clone left) — outlining was not even the
operative mechanism — and V1 still loses 19-27% on bits- and mixed-heavy
rows. Whatever g++ does with the by-reference temps and the folded-dead
error branches, the template forms are not the rescue on this corpus.

**V2 on g++**: mostly within the (large) noise. Two rows are
repetition-stable down: rigidbody_at_rest (~-15%) and message_batch (~-6%).
rigidbody's generated write source is BYTE-IDENTICAL to main (all doubles +
bool + nested writes; zero fold sites) — a whole-TU layout/inlining-context
artifact, reproducible because binary layout is deterministic, not
attributable to the changed instruction stream. test-write swings 108-133M
on the same main binary, so its -11.6% against best-of-3 is inside the
demonstrated same-binary swing. Verdict: V2 on EPYC is
noise-dominated-neutral; no repetition-stable win was demonstrated there.

## Decision

V2 shipped: on the quiet, mandatory M2 gate it is a clean win (five write
rows +3..+15%, the rest neutral, no write regressions beyond spread) with
byte-identical wire; on the noisy EPYC it is neutral within the window's
demonstrated noise. V1 is disqualified on both compilers by measurement —
the ratified design intent ("generated code calls the template forms
directly") did not survive contact with the inliner, and the fold delivers
the same constants with none of the call-boundary risk.

Follow-up candidates, deliberately not taken here: force-inline attributes
on serialize's const-param templates (a serialize-repo change that would
make the template forms viable for generators and humans alike); a g++
profile of the rigidbody_at_rest layout artifact on a quiet x86 host.
