# bench/cs — the C# runner

`src/Program.cs` is the C# port of the C++ reference
(`bench/cpp/bench_main.cpp`): the same data-driven driver over the same
committed variant corpus, golden + per-variant round-trip self-checks before
any number is produced (a corpus mismatch REFUSES to bench), warmup + 7 runs
+ median/min/max/spread, CSV rows with `lang=cs`. Full contract:
`bench/README.md`.

`src/BitsBench.cs` adds family bits (BENCH-STANDARD.md §1.4): the
16-width bitpacker workload — timed loops in `[MethodImpl(NoInlining)]`
methods for the §4.1 JitDisasm verdict.

Wiring: `schemabench.csproj` compiles `../../generated/bench/cs/Bench.cs` —
the one unit this runner measures — beside the sibling serialize.cs runtime
sources. `run.sh` runs it as `dotnet run -c Release -- --csv` from this
directory; the per-path warmup run doubles as the JIT warmup.

It also compiles `../../generated/bench/cs/realworld` (namespace
`Realworld`), which **no bench row reads**: C# has no unit-local build file,
so this project is the only `make test` gate proving that generated unit
compiles (the Makefile's bench-corpus comment and issue #80 say why). The
right home for that gate is a cs conformance leg pinning `real_packet` —
C# is the one backend without one — and it moves there when that leg lands.

Escape barriers: a static sink field accumulates observed bytes/counts and
`GC.KeepAlive` holds decoded objects. Streams are reused via `Reset` (the
runtime's documented no-allocation reuse path). The read path decodes into
one reused instance per bench — the C# stand-in for C++'s free stack
temporary (§5 zeroing makes reuse equivalent on every field that rides). The driver passes write/read as delegates: one
indirect call per op that the C++ and Rust drivers don't pay; noted with the
results.

`checks=always`: serialize.cs keeps its bounds checks, range validation and
the sticky error latch in **every** build — it has no `Debug.Assert` and no
conditional compilation of any check, so there is no NDEBUG-equivalent
Release semantics to record (§3.4). Consequence for ratios: cs rows compare
freely against go rows (both `always`); a cs↔cpp or cs↔c ratio crosses a
safety-contract difference and needs `--label-checks`, which prints it
captioned.
