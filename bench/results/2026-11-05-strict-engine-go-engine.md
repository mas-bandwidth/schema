# strict-engine layer 2: the Go engine before and after the strict read

**Date:** 2026-11-05
**Before:** `3604de0ff8d73b85d6906455dab716f455862495` (the staged `sprint/schema-100` tip attempt 4 measured against), and the attempt-4 strict carry for STEP 5's prepass
**After:** this branch, with the wire-alone check folded into the body read's one walk
**Host:** `hetzner2`, a fleet bench (`Linux/amd64`, AMD Ryzen 9 7950X3D), never the studio; the runs below were made on that host, in one job, with no other bench started by this card
**Runs:** the paired drivers below, `-benchtime=500ms`, `-count=8` for the tolerant base versus this branch and `-count=6` for the strict prepass versus this branch; the statistic is the median of the printed rows

## Reachability

`bench/paired` and `bench/tables` both need the sibling `serialize.go`
checkout (`bench/*/go.mod` replaces `github.com/mas-bandwidth/serialize.go/v2`
with a path beside the repository). That checkout is not staged in this job,
so neither release driver can start here. The table numbers below therefore
come from the committed generated `benchtable` package (the same
`generated/bench/tables/go` the release driver builds) in a scratch module
whose only stand-in is a two-method `serialize` stub: the table codec itself
names no packet symbol. This is a fleet bench (`hetzner2`), not the studio,
and the table surface measured is the release surface.

## Commands

The engine harness is committed at `internal/tablewire/engine_bench_test.go`;
it uses only `compiler`, `tabletext` and `tablewire`:

```
nice -n 19 go test -count=1 -timeout 600s ./internal/tablewire/ \
  -run '^$' -bench 'BenchmarkEngine' -benchmem -benchtime=1s -count=5
```

The table pair builds three copies of `generated/bench/tables/go` — the
tolerant base at the staged tip, the attempt-4 strict carry, and this branch
— imports the base and this branch side by side in one test binary, and
loads the 64 records of `bench/corpus/variants/bench_table.variants.bin`
with:

```
nice -n 19 go test -count=1 -timeout 600s -run '^$' \
  -bench 'BenchmarkLoad|BenchmarkSave' -benchmem -benchtime=500ms -count=8
```

## The engine wire (`internal/tablewire`)

STEP 5 changes no engine source. The after numbers on `hetzner2` are:

| row | after ns/op (median of 5) |
|---|---:|
| decode (id-table read) | 1356 |
| encode (id-table save) | 1336 |
| text read | 1519 |

Attempt 4's ledger on its worker recorded decode 1242 -> 1348, encode
827 -> 1213, text read 1095 -> 1300. The row is unchanged by this card.

## The generated table (`generated/bench/tables/go`, 64 records)

The strict prepass (attempt 4) versus this branch's one walk, same binary,
same corpus:

| row | strict prepass ns/op | one walk ns/op | ratio |
|---|---:|---:|---:|
| table load | 16454 | 5431 | 0.33x |
| table save | 3684 | 3774 | 1.02x |

The tolerant base at the staged tip versus this branch, same binary, same
corpus (the comparison the card's "no slower than base" asks for):

| row | base ns/op | this branch ns/op | ratio |
|---|---:|---:|---:|
| table load | 6998 | 6388 | 0.91x |
| table save | 4292 | 4083 | 0.95x |

The base and after rows were not taken in the same minute as the prepass row,
so compare the ratios inside each pair, not the absolute values across pairs.
Allocations are unchanged: 0 B/op and 0 allocs/op on every row.

## Finding

Attempt 4 measured the strict load at 3.08x the tolerant load because
`tableOpenChecked` ran `tableWireVerify` over the whole file and then
`LoadBody` read it again. STEP 5 folds the wire-alone checks into the body
walk for a file with no node table: each field's id is checked for repetition
as it is read, an unknown payload is walked by the same wire-alone rules as it
is skipped, and a keyed slot's key is checked as it is read. The whole-file
conditions that genuinely need every record — reachability, blob sharing and
cycles — still run `tableWireVerify`, because only a file with a node table has
them. The table load is now 0.91x the tolerant base and 0.33x the prepass;
the save is at parity. Every refusal the prepass made over a node-free file is
still made by the body read, at the same first violation.
