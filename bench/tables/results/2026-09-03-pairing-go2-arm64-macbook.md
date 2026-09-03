# 2026-09-03 — the Go leg against the force-inlined C++ arm (#350) — PAIRING CHECK, not a sitting

**Read the label first.** This board is a PAIRING CHECK on a MacBook Air (M2):
an interactive desktop, no core reservation, other workers building on the same
machine, macOS so no `taskset`. It says the three legs agree, that the gates
fire, and roughly where the numbers stand. **It certifies nothing**, and no
number here may be quoted as the tables layer's performance.

The certifying sitting is the box under the estate's bench rules — core 15, the
server stopped, not live, one bench at a time, blessed per run.
`bench/tables/run.sh --rounds 7 --tag <sitting>` is the whole command.

It **supersedes `2026-09-03-pairing-go-arm64-macbook.md`**, which measured the
Go leg against the pre-#350 C++ arm. That board's go/cpp ratios (0.82 write,
0.78 round-trip) describe a C++ arm that no longer exists; the C++ numbers here
are 4.5x its write rate.

Raw: `2026-09-03-pairing-go2-arm64-macbook.csv`.
Corpus: `bench/corpus/BenchTable.schema`, `corpus_id 2f7567e1e25ba918`,
2391 wire bytes per record, 64 records.
Seven interleaved rounds (§2.4), medians below.

## The board

| lang | path | M msg/s (median) | MB/s | ratio to C++ |
|---|---|---:|---:|---:|
| cpp | write | 1.454 | 3315.7 | 1.00 |
| cs | write | 0.246 | 561.9 | **0.17** |
| go | write | 0.294 | 669.9 | **0.20** |
| cpp | round_trip | 0.493 | 1123.4 | 1.00 |
| cs | round_trip | 0.155 | 354.6 | **0.32** |
| go | round_trip | 0.186 | 423.3 | **0.38** |

`read` is derived (round-trip minus write) and is not a row.

**Go leads C# on both rows, and both are now far behind C++.** The change is
entirely in the denominator: #350 forced the generated C++ save/load bodies and
the writer primitives inline, and the C++ write arm went from 0.322 to 1.454 M
msg/s. Nothing about the Go leg moved.

## Why the lever did not transfer, measured rather than assumed

#350's finding was that out-of-line puts make the writer's cursor round-trip
through memory: `buffer`, `capacity` and `offset` are reloaded and `offset`
stored back at every put, because a store through a `uint8_t *` may alias the
writer. Forcing everything inline let clang disambiguate and keep the cursor in
registers — worth 5.4x there.

**Go already inlines the primitives.** `go build -gcflags=-m` over the
generated bench package reports `inlining call to (*TableWriter).Put16` and so
on at every callsite; the nested `…SaveBody` calls are the ones that stay out
of line, and `-gcflags=all=-l=4` inlines those too. Measured over three rounds
each, `-l=4` moves the Go leg by nothing distinguishable from this laptop's own
noise (write 0.168–0.212 M msg/s across repeats of the *same* binary).

**The round-trip survives inlining in Go.** Disassembling an inlined
`TableStatSaveBody`, every put still emits

    MOVD 24(R0), R2      // load w.Offset
    ADD  $1, R2, R2
    MOVD R2, 24(R0)      // store w.Offset back
    MOVD 24(R0), R2      // and load it again for the next put
    LDP  (R0), (R4, R5)  // reloading w.Buffer's pointer and length too

which is #350's diagnosis exactly, unaffected by inlining. Go has no `restrict`
and its SSA does not separate the `[]byte` payload from the struct that names
it, so the store `MOVB R3, (R4)(R2)` is assumed to alias `*w`. **There is no
flag for this in Go**, which is the honest answer to whether the lever exists:
it does not exist as a flag.

**It exists as a codegen shape, and it is worth about 1.4x.** A probe of the
mechanism alone — 92 fields of `tag16, kind8, value32`, once with the cursor in
the writer struct and once carried in locals across the body and written back
at the end, byte-identical output asserted — measures 846.7 ns against 595.3
ns per record on this M2: **1.42x** on the write body. That is the size of the
prize, and it is a generated-body restructure (hoist the cursor, merge the
per-field bounds checks into one), not a switch. It is named as a follow-on
pass rather than taken here.

So the gap decomposes: roughly 1.4x of it is a Go codegen pass this port has
not done, and the rest is the language's — bounds discipline the format's
contract requires, paid per framing read, where C++'s arm now pays almost
nothing.

## How much the machine itself moves

Repeats of one unchanged Go binary spread 0.168–0.212 M msg/s (26%) while the
other port workers were building. That is the size of the effect the box
sitting exists to remove, and it is why nothing here is a certification — and
why the `-l=4` comparison above claims only "no first-order win".
