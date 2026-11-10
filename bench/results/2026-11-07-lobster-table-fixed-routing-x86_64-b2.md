# Lobster table bench — fixed-root routing before/after (fleet, bench-4/b2)

Date: 2026-11-07 (UTC, `date -u` on b2: Sat Nov 7 10:04:12 AM UTC 2026)
Host: b2 (fleet host bench-4), x86_64, AMD Ryzen 7 255 w/ Radeon 780M Graphics, 16 cores
Bench: `bench/tables/lobster/leg` — TableMixed (fixed table) load/save through the
Lobster runtime walk (`load_tolerant` / `save`), 64-record variant corpus,
corpus_id b51387f36d9b59c4, 2147 bytes/op, JIT, checks=contract.

## The change under measure

Commit 84280f7b0 ("WIP: route Lobster fixed roots and account JSON totals",
admitted: "packet-table performance acceptance remain held") added the form-3
hot path: `load_fixed` in internal/codegen/lobstertable/runtime/load.lobster
(routed from `load_walk` at load.lobster:2392-2393, `if d.fixed: return
load_fixed(d, data, report)`) and `save_fixed` in schema_save.lobster (routed
from `save_reasoned` at schema_save.lobster:978-979). A root DECLARED fixed is
now read/written as form 3 only; a form-1 wire at a fixed root is refused as
`previous_form`. Base is the pre-routing parent a007c180b; head is 318ccc0cf.

## Method (same host, both commits)

The committed generated unit (generated/bench/tables/lobster) predates the
routing, so each side was measured with its own compiler's regenerated unit —
measuring head against the committed unit would time the old code:

    git checkout -f <sha>
    go build -o bin/schema ./cmd/schema
    ./bin/schema generate --lang lobster --out generated/bench/tables/lobster bench/corpus/BenchTable.schema
    bench/tables/lobster/leg run --csv --iterations 65536

(65536 = the harness's required multiple of 64, in place of the 400000 default
so the sitting fits the fleet window; identical on both sides. The generated
unit at base contains no `load_fixed` (grep count 0); at head it does (3).)

## Base a007c180b — numbers (form-1 walk)

CSV rows (7 measured runs each), two CSV repeats plus two human-table repeats
at the same command without --csv (write 0.014 / round_trip 0.004 M msg/s both):

    lobster,bench_table,write,65536,2147,7,12743,9458,13098,26.09,28.56,b51387f36d9b59c4,table,jit,contract,default,unknown
    lobster,bench_table,round_trip,65536,2147,7,3748,3699,3785,7.67,2.29,b51387f36d9b59c4,table,jit,contract,default,unknown
    lobster,bench_table,write,65536,2147,7,12874,12647,13290,26.36,5.00,b51387f36d9b59c4,table,jit,contract,default,unknown
    lobster,bench_table,round_trip,65536,2147,7,3684,2549,3799,7.54,33.93,b51387f36d9b59c4,table,jit,contract,default,unknown

Median of the CSV repeats: write 12808 msg/s (26.2 MB/s), round_trip 3716 msg/s
(7.6 MB/s). Noise: b2 carried other fleet work (load 1.2-14.5 across the
sitting); one write repeat spread 28.56% and one round_trip repeat 33.93% —
over the standard's 15% noisy line, so these are a before-picture, not a
certified sitting.

## Head 318ccc0cf — no numbers: the bench refuses, by design of the change

    $ bench/tables/lobster/leg run --gate        # exit 1
    schema tables bench (lobster, JIT)
    FAILED: bench_table: load of variant 0 failed
    refusing to emit CSV rows from a failing run
    BENCH FAILED (corpus_id b51387f36d9b59c4)

The timed run exits 1 with the same three lines. Direct probe of the head
runtime against testdata/wire/bench_table.bin (form byte 01):

    rec_nil=true refused=true malformed=false reason=previous_form

That is the routing working as specified (318ccc0cf: "a form-1 wire there is
refused as previous_form"), and it means the committed form-1 bench corpus
cannot produce a head number. A form-3 variant corpus for TableMixed — or a
bench root declared variable — is owed before any after-number exists; none is
estimated here. Fleet numbers for the head side therefore remain owed, not
because no fleet host answered (bench-3/bench-4/bench-5/bench-1/bench-2 all
answered SSH from the bench-11 on 2026-11-07, contrary to the earlier refusal
note), but because the measured bench and the new routing disagree about the
wire form.

## DONE-WHEN (bench-11, head 318ccc0cf)

    make dist-lobster
    SCHEMA_SLOW=1 go test -count=1 -timeout 600s ./internal/codegen/lobstertable/
    ok  	github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable	74.724s
