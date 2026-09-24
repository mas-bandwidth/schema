# gates/packet-read/java/identity — java: identity fixed read versus its own packet read

**Verdict: FIXED-FASTER (2.12x)**

The identity-lane fixed-form read is faster than java's own packet-wire read
of the same 64 logical records (BenchMixed). Both measurements from one sitting.

## Measurement

- **Host:** spacegame.losangeles
- **CPU:** AMD EPYC 9124 16-Core Processor (x86_64)
- **Revision:** ead804093232b3593facd987ebe2f726a10dd63d
- **Load before:** 99.20, 116.99, 84.90
- **Load after:** 82.49, 100.97, 83.47

### Run 1 (primary)

**Table wire (identity lane, bench_fixed):**
- Command: `bin/schema-paired -mode fast -langs java -out build/paired-fast/java-identity`
- Iterations: 1,563,712 per sample
- Corpus id: 066b2822b3e5d487
- Bytes per op: 1264.30
- write: 6,308,678 msg/s (158.5 ns/op), spread 0.0%
- round_trip: 1,935,933 msg/s (516.6 ns/op), spread 0.0%
- **read (derived): 358.1 ns/op**

**Packet wire (bench_mixed):**
- Command: `java -cp build/java-bench Main --csv --round 0` (from bench/java/)
- Iterations: 4,000,000 per sample
- Corpus id: fd0748942dc6ae09
- Bytes per op: 438
- write: 2,865,587 msg/s (349.0 ns/op), spread 0.0%
- round_trip: 902,402 msg/s (1108.2 ns/op), spread 0.0%
- **read (derived): 759.2 ns/op**

**Ratio: 759.2 / 358.1 = 2.12x → FIXED-FASTER**

### Run 2 (control)

**Table wire (identity lane):**
- Iterations: 1,563,712
- Corpus id: 066b2822b3e5d487
- write: 4,849,957 msg/s (206.2 ns/op), spread 0.0%
- round_trip: 1,541,979 msg/s (648.5 ns/op), spread 0.0%
- **read (derived): 442.3 ns/op**

**Packet wire:**
- Iterations: 4,000,000
- Corpus id: fd0748942dc6ae09
- write: 2,499,164 msg/s (400.1 ns/op), spread 0.0%
- round_trip: 751,779 msg/s (1330.2 ns/op), spread 0.0%
- **read (derived): 930.1 ns/op**

**Ratio: 930.1 / 442.3 = 2.10x → FIXED-FASTER**

### Reproducibility

Both runs produce FIXED-FASTER. The verdict is reproducible.

## Notes

- Java is a table-only language in the paired driver (bench/paired/main.go).
  The paired driver has no packet leg for java; the packet read comes from
  bench/java/Main.java (the type board's runner).
- The identity-lane read is derived from round_trip minus write (BENCH-STANDARD
  §2.9: "read is DERIVED"). The packet read is also derived the same way.
  A measured (not derived) read-only path would require `-mode plan-read`,
  which is documented in bench/paired/README.md but not yet implemented.
- The two measurements use different corpus ids (066b2822b3e5d487 for table,
  fd0748942dc6ae09 for packet) because they come from different invocations.
  The plan-read mode would produce all three rows (bench_fixed_read,
  bench_plan_read, bench_mixed_read) sharing one corpus id in a single
  invocation.
- Swarm bench: competing process go (PID 2471007) observed at 0.2% CPU during
  all measurement phases. Load averages 82–120 during measurement.
- The checks axis differs: table uses checks=contract, packet java uses
  checks=contract. Both have debug asserts compiled out and wire/API validation
  in every build.
- JDK: OpenJDK 21.0.12.1 Temurin (mixed mode, sharing).
- Compiler: javac --release 17 -Xlint:all -Werror.
