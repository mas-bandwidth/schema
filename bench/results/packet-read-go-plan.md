# Go Plan-Read vs Packet-Read Measurement

**Host:** vision  
**Revision:** ead804093232b3593facd987ebe2f726a10dd63d  
**Corpus ID:** fd0748942dc6ae09 (packet), 859b1d73dcce275b (table)  
**Load:** swarm bench, load average 21.30, 48.56, 67.13  
**Date:** 2026-09-23

## Command
```
bin/schema-paired -mode fast -langs go -out build/paired-fast/go-plan -noise-note "swarm bench, load 21.30, 48.56, 67.13"
```

## Finding
The measurement cannot be completed because:
1. The `-mode plan-read` referenced in BENCH-STANDARD.md §1.10 is not implemented in bench/paired/main.go
2. The `-mode fast` refuses to run with a single published language (Go), requiring all four (cpp,c,go,cs) or one unpublished language alone
3. The existing table runners produce DERIVED read rows, not measured reads as required by the plan-read arm

## Available Data (DERIVED, not measured)
From existing runners with 4,000,000 iterations, 7 rounds:

| Row | Rate (M msg/s) | Type |
|-----|----------------|------|
| bench_mixed read (packet) | 1.44 | DERIVED |
| bench_fixed read (table) | 0.525 | DERIVED |

**Note:** These are derived reads (round_trip - write), not measured reads. The plan-read arm requires measured reads per BENCH-STANDARD.md §1.10.

## Verdict
**ABSTAIN** - Infrastructure for plan-read measurement not available

## Reproduction
```
cd repo && git rev-parse HEAD  # must print ead804093232b3593facd987ebe2f726a10dd63d
./bin/schema-paired -mode gate -langs go  # passes
./bin/schema-paired -mode fast -langs go -out build/paired-fast/go-plan -noise-note "..."  # refuses: fast mode requires all four paired languages
```
