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

Wiring: `schemabench.csproj` compiles `../../generated/cs` beside the
sibling serialize.cs runtime sources, exactly like `test/cs`. `run.sh` runs
it as `dotnet run -c Release -- --csv` from this directory; the per-path
warmup run doubles as the JIT warmup.

Escape barriers: a static sink field accumulates observed bytes/counts and
`GC.KeepAlive` holds decoded objects. Streams are reused via `Reset` (the
runtime's documented no-allocation reuse path). The read path decodes into
one reused instance per bench — the `MessageStorage` discipline, the C#
stand-in for C++'s free stack temporary (§5 zeroing makes reuse equivalent
on every field that rides). The driver passes write/read/vary as delegates:
one indirect call per op that the C++ and Rust drivers don't pay; noted
with the results.
