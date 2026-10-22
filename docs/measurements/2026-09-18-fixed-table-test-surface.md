# Fixed-table test surface measured at main

This document records a mechanical count of the tracked files that make up the
fixed-table test surface for each of the nine legs (cpp, c, go, rust, dart, js,
java, cs, elixir) at `main`. For every leg it gives the number of tracked files
under `test/<leg>-tables` and the number of tracked files under `test/<leg>`,
both counted with `git ls-files`. It is a measurement only: it makes no claim
about whether any leg is correct, complete, fast, or at parity with any other
leg, and it does not compare legs against one another beyond reporting the raw
counts.

Head: `ed0c7917`

| leg | files under `test/<leg>-tables` | files under `test/<leg>` |
| --- | --- | --- |
| cpp | 0 | 0 |
| c | 20 | 1 |
| go | 7 | 2 |
| rust | 0 | 2 |
| dart | 1 | 1 |
| js | 1 | 1 |
| java | 1 | 1 |
| cs | 24 | 2 |
| elixir | 0 | 2 |

The legs whose `test/<leg>-tables` directory does not exist at all are: cpp, rust, elixir.

Totals: `tables_total=54` and `test_total=372`.

## How these numbers were made

```
for l in cpp c go rust dart js java cs elixir; do echo "$l tables=$(git ls-files test/$l-tables | wc -l) leg=$(git ls-files test/$l | wc -l)"; done | tee ../scratch/counts.txt
```

```
echo "tables_total=$(git ls-files test/*-tables | wc -l) test_total=$(git ls-files test | wc -l) head=$(git rev-parse --short HEAD)" | tee ../scratch/totals.txt
```
