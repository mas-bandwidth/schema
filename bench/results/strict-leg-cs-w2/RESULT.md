# strict-leg-cs w2: C# table load/save, before/after the strict (refuse) read

Finding (2) of card strict-leg-cs.w2: the w1 regenerated goldens change hot
load/save (clamp-and-keep becomes refuse-the-whole-load) with no bench
numbers attached. This is the paired fleet measurement for that change.

## Verdict

Within noise — the hot-byte change ships. Median of six sittings each side
(each sitting is the harness's own median of 7 runs, 400,000 iterations,
2,147 bytes/op, corpus_id b51387f36d9b59c4, golden gate `OK` before every
clock):

| row | before (msg/s) | after (msg/s) | delta |
|---|---:|---:|---:|
| bench_table write | 209,979 | 208,679 | -0.62% |
| bench_table round_trip (load+save) | 85,960 | 85,305 | -0.76% |

Per-sitting spreads run 1.2%-11.8% (one noisy sitting each side: a 158k
write before, a 77k round-trip after at 46% spread), so a sub-1% median
delta is inside the sitting-to-sitting noise. Raw rows: rows.csv in this
directory (ABAB interleaving: before x3, after x3, before x3, after x3,
pinned `taskset -c 7`).

## Conditions

- Host: `b2` (fleet member bench-4), linux/amd64, AMD Ryzen 7 255 w/ Radeon
  780M Graphics, pinned to core 7. Not the bench-11.
- Harness: the repo's C# tables bench, `bench/tables/cs/leg build` then
  `leg run --gate` then `leg run --csv` (bench/tables/README.md protocol:
  golden gate before the clock, 1 warmup + 7 measured runs).
- Before: 2a97049920a2a73db04e8b9e77b034e1aef28170 — the pre-strict base,
  parent of w1's first commit (01c4b1629). This is the revision whose
  generated codec clamps-and-keeps; ec3b500^ already carries the strict
  generator, so the clamp-to-refuse delta is measured against this base.
- After: 3faa32287b5dab6981e503d39369daacea562782 — w2 head at bench time
  (strict generator, regenerated bench codec, strict bench driver).
- Runtime: dotnet 10.0.401 (fleet SDK), serialize.cs sibling from the
  fleet's existing checkout (SERIALIZE_CS), Release build, 0 warnings.
- First attempt note: the first fleet pass was discarded — the bench
  driver still referenced the retired `TableReport.Malformed`, its build
  failed behind a pipe, and the "after" sittings timed a stale binary.
  The driver fix (3faa32287's parent commit on this branch) plus
  `set -o pipefail` in the run script make this pass the first valid one;
  the local gate (`leg run --gate`: OK, corpus_id b51387f36d9b59c4)
  confirms the strict codec reproduces the corpus before timing.
