# strict-engine layer 2: the Go engine before and after the strict read

**Date:** 2026-11-05
**Before:** `3604de0ff8d73b85d6906455dab716f455862495` (the staged `sprint/schema-100` tip)
**After:** this branch, the layer-2 carry onto that tip
**Host:** the job's worker node, `linux/amd64`, 16 vCPU AMD Ryzen 7 255, no
  other bench running; the fleet studio is **not reachable from the job** (see
  "Reachability" below)
**Runs:** 5 measured after a `go test` warmup, `-benchtime=1s`
**Statistic:** the 5-run median; the raw rows are the
  `BenchmarkEngine*` and `BenchmarkTable*` lines printed by the two commands
  below

## Reachability

`bench/paired` and `bench/tables` both need the sibling `serialize.go`
checkout (`bench/*/go.mod` replaces `github.com/mas-bandwidth/serialize.go/v2`
with a path beside the repository) and the generated variant corpora that
driver produces. That checkout is not staged in this job, so neither the
packet-wire nor the tables release bench can start here. This file therefore
records engine-level `go test -bench` runs on the job's own worker node, never
a fleet studio, and it is **not** a fleet release number. STEP 5 of the card
says that when no bench is reachable the hot-path change is not shipped; the
numbers below are the supplementary evidence for that decision, not a
certificate.

## Commands

The same harness was placed as `internal/tablewire/zz_engine_bench_test.go`
in both trees (it is not committed; it uses only `compiler`, `tabletext` and
`tablewire`) and run as:

```
nice -n 19 go test -count=1 -timeout 600s ./internal/tablewire/ \
  -run '^$' -bench 'BenchmarkEngine' -benchmem -benchtime=1s -count=5
```

The table-load/save rows build the generated `benchtable.TableMixed` codec of
each tree (the `generated/bench/tables/go` files) in a scratch module with a
stub for the missing `serialize.go` packet symbols, load
`testdata/wire/bench_table.bin` (64 records), and run:

```
nice -n 19 go test -count=1 -timeout 600s -run '^$' \
  -bench 'BenchmarkTable' -benchmem -benchtime=1s -count=5
```

## The engine wire (`internal/tablewire`)

| row | before ns/op | after ns/op | ratio |
|---|---:|---:|---:|
| decode (id-table read) | 1242 | 1348 | 1.09x |
| encode (id-table save) | 827 | 1213 | 1.47x |
| text read | 1095 | 1300 | 1.19x |

## The generated table (`generated/bench/tables/go`, `bench_table.bin`)

| row | before ns/op | after ns/op | ratio |
|---|---:|---:|---:|
| table load | 4070 | 12525 | 3.08x |
| table save | 2310 | 2313 | 1.00x |

Allocations are unchanged on the corpus rows (0 B/op, 0 allocs/op on both
sides).

## Finding

The save path is at parity, but the strict load is 3.08x the tolerant load it
replaces on the same corpus. A CPU profile of the after build puts 71% of the
load under `tableOpenChecked` -> `tableWireVerify`: the strict read verifies
the whole wire before `LoadBody` reads it, so the record is walked twice. The
wire `encode` delta is the same extra validation on the save side; the
generated `Save` path, which is the measured release surface, stays at parity.

Per the card's STEP 5 this hot-path change is **not shipped** as no-slower:
the fleet bench that could certify it is not reachable, and the engine bench
that is reachable shows a load regression. The follow-up is to make the
strict open walk once (fold `tableWireVerify`'s wire-alone conditions into the
`LoadBody` walk, or cache its result) and then re-run the same two commands
and the fleet tables bench.
