# Fixed Table

The paired pass compares Fixed Table with Packet Wire using the same logical
records. Each results page has only two percentages per language:

- **Fixed Table %**: fastest measured table implementation = 100%.
- **vs Packet Wire %**: that language's packet implementation = 100%.

Both use blended read/write cost (the measured round trip), and lower is better.
The raw timings and methodology live in the result directory's `DETAILS.md`.

```sh
# Build every leg and verify both wires; no timing.
go run ./bench/paired -mode gate

# During an exclusive measurement window, use those hashed binaries.
go run ./bench/paired -mode run -reuse-build -out bench/paired/results/<sitting>
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
their order. There is one discarded warmup per measured path. Existing iteration
counts stay fixed: 4,000,000 packet operations and 400,000 table operations, both
whole rotations of the 64 records. Each row must exceed 200 ms; a spread above
15% refuses publication of the complete pass. ARM and x64 are separate sittings
and separate results pages.

C++ controls for both wires bracket the measured rounds; a movement above 5%
refuses the complete sitting. Controls remain separate from the seven samples
and cannot improve a headline by adding extra attempts.

`build.json` records the architecture, compiler/runtime versions, source revision,
binary hashes and corpus hashes. `load.json` records load before each round and
after the pass. Failed passes keep their raw evidence in the printed temporary
directory; a completed result directory appears only after every gate succeeds.
Run `go run ./bench/paired -mode render -out <sitting>` to regenerate the compact
page from its raw per-round CSVs. It refuses missing, duplicate or incompatible
rows and never substitutes timings from a different sitting.

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
