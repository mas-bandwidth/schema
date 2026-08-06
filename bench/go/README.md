# bench/go — STUB: the Go runner lands with the serialize.go port

Entry point `run.sh` expects: `bench/go/main.go` (+ `go.mod` wired like
`test/go/go.mod` — module `benchgo`, replace directives for `example` →
`../../generated/go` and `github.com/mas-bandwidth/serialize.go` → the
sibling port checkout). Run as `go run . --csv`.

Port the C++ reference exactly (`bench/cpp/bench_main.cpp`): the `pin_*`
instances, the `vary_*` field mappings, the LCG, the batch builder, the
golden + round-trip self-checks, warmup + 7 runs + median/min/max/spread,
and the CSV row format with `lang=go`. Use `runtime.KeepAlive` (or a
package-level sink) as the escape barrier. Full contract: `bench/README.md`.
