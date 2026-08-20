# bench/cs — the C# runner

`src/Program.cs` is the C# port of the C++ reference
(`bench/cpp/bench_main.cpp`): same pinned corpus instances, same `vary_*`
field mappings, same LCG (unchecked ulong arithmetic), same batch builder,
golden + round-trip self-checks before any number is produced (a corpus
mismatch REFUSES to bench), warmup + 7 runs + median/min/max/spread, CSV
rows with `lang=cs`. Full contract: `bench/README.md`.

`src/RtBench.cs` adds families rt and bits (BENCH-STANDARD.md §1.3/§1.4):
the four Bench.schema shapes hand-written over the plain stream Serialize*
surface, §1.5 oracle-gated against `testdata/wire/bench_*.bin`, plus the
16-width bitpacker workload — timed loops in `[MethodImpl(NoInlining)]`
methods for the §4.1 JitDisasm verdict.

Wiring: `schemabench.csproj` compiles `../../generated/cs` and
`../../generated/bench/cs/realworld` (namespace `Realworld` — the unit the
`real_packet` row measures, referenced qualified so the two units'
same-named table types never collide) beside the sibling serialize.cs
runtime sources, exactly like `test/cs`. `run.sh` runs it as
`dotnet run -c Release -- --csv` from this directory; the per-path warmup
run doubles as the JIT warmup. This project is also the realworld unit's
compile gate in `make test` — its one consumer (the Makefile's bench-corpus
comment says why).

Escape barriers: a static sink field accumulates observed bytes/counts and
`GC.KeepAlive` holds decoded objects. Streams are reused via `Reset` (the
runtime's documented no-allocation reuse path). The read path decodes into
one reused instance per bench — the `MessageStorage` discipline, the C#
stand-in for C++'s free stack temporary (§5 zeroing makes reuse equivalent
on every field that rides). The driver passes write/read/vary as delegates:
one indirect call per op that the C++ and Rust drivers don't pay; noted
with the results.

`checks=always`: serialize.cs keeps its bounds checks, range validation and
the sticky error latch in **every** build — it has no `Debug.Assert` and no
conditional compilation of any check, so there is no NDEBUG-equivalent
Release semantics to record (§3.4). Consequence for ratios: cs rows compare
freely against go rows (both `always`); a cs↔cpp or cs↔c ratio crosses a
safety-contract difference and needs `--label-checks`, which prints it
captioned.
