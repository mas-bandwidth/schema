# bench/go — the Go runner

`main.go` is the Go port of the C++ reference (`bench/cpp/bench_main.cpp`):
same pinned corpus instances, same `vary_*` field mappings, same LCG, same
batch builder, golden + round-trip self-checks before any number is produced
(a corpus mismatch REFUSES to bench), warmup + 7 runs +
median/min/max/spread, CSV rows with `lang=go`. Full contract:
`bench/README.md`.

`rt.go` adds families rt and bits (BENCH-STANDARD.md §1.3/§1.4): the
four Bench.schema shapes hand-written over the Serialize* surface,
§1.5 oracle-gated against `testdata/wire/bench_*.bin`, plus the 16-width
bitpacker workload — timed loops in `//go:noinline` symbols for the §4.1
verdict.

Wiring: `go.mod` (module `benchgo`) replaces `example` →
`../../generated/go`, `bench` → `../../generated/bench/go` (the realworld
unit the `real_packet` row measures), and
`github.com/mas-bandwidth/serialize.go` → the sibling port checkout,
exactly like `test/go`. `run.sh` runs it as `go run . --csv` from this
directory (optimized by default — Go has no debug/release split).

Escape barriers: a package-level sink accumulator plus `runtime.KeepAlive`
on decoded values. Streams are reused via `Reset` (the runtime's documented
no-allocation reuse path) — the Go equivalent of C++'s free stack
construction. The driver passes write/read/vary as function values: one
indirect call per op that the C++ and Rust template/generic drivers don't
pay; noted with the results.
