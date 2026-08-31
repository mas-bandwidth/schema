# bench/go — the Go runner

`main.go` is the Go port of the C++ reference (`bench/cpp/bench_main.cpp`):
the same data-driven driver over the same committed variant corpus, golden +
per-variant round-trip self-checks before any number is produced (a corpus
mismatch REFUSES to bench), warmup + 7 runs + median/min/max/spread, CSV rows
with `lang=go`. Full contract: `bench/README.md`.

`bits.go` adds family bits (BENCH-STANDARD.md §1.4): the 16-width
bitpacker workload — timed loops in `//go:noinline` symbols for the §4.1
verdict.

Wiring: `go.mod` (module `benchgo`) replaces `bench` →
`../../generated/bench/go` and `github.com/mas-bandwidth/serialize.go` → the
sibling port checkout. One generated unit, the one this runner measures. `run.sh` runs it as `go run . --csv` from this
directory (optimized by default — Go has no debug/release split).

Escape barriers: a package-level sink accumulator plus `runtime.KeepAlive`
on decoded values. Streams are reused via `Reset` (the runtime's documented
no-allocation reuse path) — the Go equivalent of C++'s free stack
construction. The driver passes write/read as function values: one indirect
call per op that the C++ and Rust template/generic drivers don't pay; noted
with the results.
