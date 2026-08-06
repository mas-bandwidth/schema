# bench/cs — STUB: the C# runner lands with the serialize.cs port

Entry point `run.sh` expects: `bench/cs/schemabench.csproj` + `src/`
(wired like `test/cs/schematest.csproj` — `<Compile Include>` items for
`../../generated/cs` and the sibling serialize.cs runtime sources). Run as
`dotnet run -c Release -- --csv`.

Port the C++ reference exactly (`bench/cpp/bench_main.cpp`): the `pin_*`
instances, the `vary_*` field mappings, the LCG (unchecked arithmetic), the
batch builder, the golden + round-trip self-checks, warmup + 7 runs +
median/min/max/spread, and the CSV row format with `lang=cs`. Keep results
observed (e.g. a static sink field + `GC.KeepAlive`) so the JIT cannot
eliminate the work; do a real JIT warmup run per path. Full contract:
`bench/README.md`.
