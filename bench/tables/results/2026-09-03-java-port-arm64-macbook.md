# 2026-09-03 — the tables bench gains its fifth leg, Java — PAIRING CHECK, not a sitting

**Read the label first.** This board is a PAIRING CHECK on a MacBook Air (M2):
an interactive desktop, no core reservation, macOS so no `taskset`. It says the
five legs agree, that the gates fire, and roughly where the numbers stand. **It
certifies nothing**, and no number here may be quoted as the tables layer's
performance. The certifying sitting is the box under the estate's bench rules.

One sitting, otherwise quiet machine, all five legs, 1 warmup + 7 measured runs
each, every leg through its golden and 64-variant round-trip gates before its
clock started.

| leg | write M msg/s | spread | round_trip M msg/s | spread |
|---|---|---|---|---|
| cpp  | 2.270 | 2.9%  | 0.775 | 6.5%  |
| rust | 2.060 | 0.8%  | 0.640 | 1.8%  |
| java | 1.638 | 1.3%  | 0.468 | 18.1% |
| go   | 0.347 | 4.4%  | 0.217 | 26.0% |
| cs   | 0.303 | 5.9%  | 0.203 | 30.1% |

**The ratio the Java port is held to, against the C++ arm as it now stands** —
the force-inlined one from #350, which is the bar a port meets today rather than
the one it would have met before:

| | java / cpp |
|---|---|
| write      | **0.72x** |
| round_trip | **0.60x** |

Stated plainly rather than rounded toward the ladder's *"same speed, or not
significantly slower"*: Java writes at rather better than seven tenths of C++'s
rate and round-trips at three fifths of it. **round_trip is the row with the
gap**, and the derived read — round-trip minus write, informational — is where
it sits.

## What is worth knowing before anyone divides these

- **The three JIT/GC legs carry the wide spreads** — java 18.1%, go 26.0%, cs
  30.1% on round_trip — and the two AOT legs do not (cpp 6.5%, rust 1.8%). That
  is tier-up and collection noise on a shared laptop, which
  BENCH-STANDARD.md already records for C#, and it is the first thing a box
  sitting should settle.
- **`--rounds` is the wrong instrument for a JIT leg, and this leg has the
  evidence.** An earlier interleaved pass on the same machine, `--rounds 3`,
  gives each leg ONE warmup and ONE measured run per process; the Java
  round_trip row came back 0.138, 0.283 and 0.305 M msg/s across the three — a
  2.2x span between rounds of byte-identical code. The numbers above are from
  the default pass, where one process carries the warmup and all seven measured
  runs.
- **Every leg passed its gates before its clock started**: variant 0
  byte-compared against the pinned golden, then all 64 variants loaded,
  re-saved at the same length and byte-compared. A leg that fails emits no rows.

## What the Java leg does with its own contract inside the clock

The read arm resets, loads and re-saves into ONE reused instance, and the reader
and the writer are hoisted out of the loop — which is not the runner taking a
liberty but the port's own contract: a nested body moves the reader's limit
instead of slicing a sub-reader, so a hoisted reader allocates nothing.
`make tables-java-alloc` measures that at **0 bytes per record** on both the read
and the save, with the JVM's own per-thread allocation counter and a negative
control behind it. The rows above are codec time, not allocator time.
