# Fixed Table

The paired pass compares Fixed Table with Packet Wire using the same logical
records. Each results page has only two percentages per language:

- **Fixed Table %**: fastest measured table implementation = 100%.
- **vs Packet Wire %**: that language's packet implementation = 100%.

Both use blended read/write cost (the measured round trip), and lower is better.
Below 100% in the packet comparison means the table is faster than packet wire.
The compact page also labels the checks difference: packet C/C++/C# removes
bounds/range checks, packet Go always checks, and table keeps wire/API validation.
The driver refuses any different checks axes. Full semantics, raw timings and
methodology live in the result directory's `DETAILS.md`.

```sh
# Build every leg and verify both wires; no timing.
go run ./bench/paired -mode gate

# During an exclusive measurement window, use those hashed binaries.
go run ./bench/paired -mode run -reuse-build \
  -quiet-window 'operator READY/START coordination reference' \
  -out bench/paired/results/<sitting>
```

Run from the repository root. The tool needs the C, C++, Go and .NET toolchains
and the same sibling serialize runtimes as the standard packet pass. `CC`,
`CXX`, `SERIALIZE`, `SERIALIZE_C`, `SERIALIZE_GO`, `SERIALIZE_CS` and
`BENCH_OPT_LEVEL` select the builders and runtimes. Every requested leg is
required. `-langs cpp,go,cs` permits a partial build or correctness check, but
publishing requires all four languages and at least seven rounds.

The standard packet runners keep their existing generated storage, release
flags and timed loops. Table runners use a separately generated closure so a
table representation cannot slow the packet reference. All binaries are built
before the first clock. Each round runs both wires for each language, alternating
their order. There is one discarded warmup per measured path, using the full 4,000,000 packet
or 400,000 table operations. The measured iteration counts are the same, both
whole rotations of the 64 records. Seven rounds follow the packet reporting
convention, including the 4/3 wire-order imbalance. Table round trips reset the
target inside the clock; packet round trips do not need that reset. Each row must exceed 200 ms; a spread above
15% refuses publication of the complete pass. ARM and x64 are separate sittings
and separate results pages.

The comparison deliberately retains each packet runner's fastest release mode.
The CSV semantics match `bench/tools/relative.go`: `removed` means debug asserts
and bounds/range checks compile out; `always` means bounds, range and sticky-error
checks stay in every build by contract; `contract` means debug asserts compile
out while wire/API validation stays. These are labelled comparisons of the
fastest correct implementations, with different validation work.

C++ controls for both wires bracket the measured rounds; a movement above 5%
refuses the complete sitting. Controls remain separate from the seven samples
and cannot improve a headline by adding extra attempts.

`build.json` records architecture, compiler/runtime versions, source HEAD,
binary hashes and corpus hashes. Reuse refuses a changed HEAD, host, runtime
setting, binary or corpus. `window.json` requires the operator's READY/START
receipt or reference, records START/END and an `OK` or `INVALID` verdict, and
contains process names, PIDs, parent PIDs, CPU percentages and load. Every
control and measured runner must have matching before/after samples. Arguments,
executable paths and process environments are never collected.

The macOS/Linux monitor samples before clocks, every two seconds during each
runner, and at runner boundaries. Missing coverage beyond ten seconds refuses
publication. Known foreign compiler/build/benchmark processes refuse even when
momentarily idle; this includes external `dotnet` processes because their name
alone cannot establish that they are harmless. Only the coordinator, its actual
ancestors and descendants are excluded. Other agents under the same shell/app
are still foreign. The monitor stops with its owned runner and never kills a
competing process. Windows timing refuses unsupported monitoring; build/gate
modes remain available.

Normal desktop CPU and load are recorded without a universal rejection threshold.
CPU percentages retain `ps`'s platform-specific meaning. Sampling can miss short
or unusually named work, including a workload behind a generic interpreter name.
An `OK` verdict means these checks passed; it is not proof of exclusive CPU use.
The operator receipt and bracketing drift checks remain necessary. Samples are
bounded to 10,000; exceeding that limit refuses the sitting. The monitor forks
`ps` every two seconds and at runner boundaries, plus `sysctl` on macOS. It also
allocates process snapshots and appends them to disk, so it perturbs the measured
machine. Samples remain in memory; this count limit is not a byte budget, and
memory use grows with both sample and process counts.

During collection, each complete sample is appended once as compact JSONL to
`window.samples.jsonl`, with no per-sample `fsync`. The small `window.json` marker
stays incomplete; the growing history is never rewritten before a runner.
After END or INVALID, the complete final `window.json` is assembled once, synced,
closed and renamed into place. Only then is the journal removed. Interruptions
and failed final writes retain the journal and cannot seal; failed windows remain
invalid even after their final evidence is saved. `load.json` is the load-only
view of the completed samples.

Failed passes keep their raw evidence in the printed temporary directory; a
completed result directory appears only after every gate succeeds.
`completion.json` seals the exact build/window/load/control/round file set with
SHA-256 hashes after checking the actual binaries and corpus again. Run
`go run ./bench/paired -mode render -out <sitting>` to regenerate the compact page.
It verifies that seal before deriving any numbers and refuses missing, extra,
changed or incompatible evidence. Archived rendering does not require the old
local binaries. The seal detects altered evidence; it is not cryptographic
protection against an operator deliberately rewriting both data and manifest.

## One definition and one set of values

`bench/corpus/Bench.schema` remains the sole definition of `BenchMixed`, and
its canonical packet golden and 64-record variant corpus are unchanged.
`FixedTable.schema` adds a wrapper that reaches `BenchMixed`, causing the
existing compiler to emit table codecs for that type. The measured table root
is **BenchMixed itself**; the wrapper is never serialized or timed.

`test/bench/paired_main.cpp` reads the canonical packet records, decodes each
once, and saves that same object on the table wire. It verifies table load and
re-save byte identity, then writes the table-decoded object back onto the packet
wire and requires the original packet bytes. There is no second field list or
independent value generator. Fixed-point and 128-bit values retain their original
types and bounds. Packet constants, reserved bits and alignment carry no logical
fields and have no table payload.

Table default elision and enum ID sets vary the encoded record lengths even
though the schema is a fixed table. The paired table corpus therefore has a
64-entry little-endian uint32 length index beside its concatenated records.
Each runner validates every length, the exact record count and total, the first
record's golden, and all 64 load/re-save results before timing. Timed operations
check the corresponding record length. `bytes_per_op` reports the exact mean;
staggered buffer padding never counts as serialized bytes.

`make bench-paired-corpus` deliberately re-pins the table half; ordinary builds
only verify it. The historical `BenchTable.schema`, table corpus and results
remain intact. They were representative counterparts with different logical
types and values, so their historical ratios cannot supply this second column.
