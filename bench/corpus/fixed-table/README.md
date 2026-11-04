# Native fixed table benchmark corpus

These are the same 64 populated `benchtable.TableMixed` values produced by
`fill()` in `test/bench/table_main.cpp`, written through the generated C++
`TableMixedFixedSave` API. Each variant is a complete form-3 file, including
its header, layout and one record. Variant zero is `bench_table.bin`.

- File size: **2,531 bytes** = 20-byte header and layout length + 1,279-byte
  layout + 1,232-byte record; variant corpus: **64 × 2,531 bytes**.
- `corpus_id`: **`65b15368d40f6544`**, computed under BENCH-STANDARD §1.6
  from both basenames and file contents, in sorted basename order.
- Historical form-1 files remain **2,147 bytes** each, with corpus ID
  `b51387f36d9b59c4`. Their original files remain
  in `testdata/wire/bench_table.bin` and `bench/corpus/variants/` unchanged.

Run `make bench-fixed-table-check` to regenerate all values in memory and
compare both files. The native producer checks equal sizes, distinct variants,
load/save byte identity, and refusal of a form-1 control. `go test ./bench/corpus`
independently decodes and reproduces every fixed file through the Go engine.
`make bench-fixed-table-corpus` deliberately rewrites only these two fixed pins.

The historical producer modes and benchmark runner defaults are unchanged.
These files establish correctness; they contain no performance measurement,
and results with different corpus IDs must not be divided against each other.
