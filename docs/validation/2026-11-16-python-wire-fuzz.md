# Python wire-fuzz qualification, 2026-11-16

Source: `1aa01ab6f62d9e454870bafd1c4422f85159633c`. Linux x86-64, CPython 3.15.2 and 3.11.18, mypy 1.18.2, pinned sibling checkouts and a fresh generated corpus. Each row below was run separately under the default-capacity reservation. The default and nightly rows use both pins, normal and `-X dev` modes, and plain, mixed-builder and retaining arms.

Plain and mixed rows each cover 327 seeds across 63 roots (224113 enumerated mutations). Retain rows cover 130 applicable seeds across 29 roots (135353 enumerated mutations); 197 seeds lie outside that arm. Each row below has zero divergences and no absent seeds. Release runs are plain and retain, seeds 1, 2 and 3; the registered release aggregate passed. A synthetic aggregate RED control exited nonzero.

| Row | Seed | Random mutations | Total mutations |
| --- | ---: | ---: | ---: |
| default-current-normal-plain | 24845619678 | 100000 | 324113 |
| default-current-normal-mixed | 24845619678 | 100000 | 324113 |
| default-current-normal-retain | 24845619678 | 100000 | 235353 |
| default-current-dev-plain | 24845619678 | 100000 | 324113 |
| default-current-dev-mixed | 24845619678 | 100000 | 324113 |
| default-current-dev-retain | 24845619678 | 100000 | 235353 |
| default-floor-normal-plain | 24845619678 | 100000 | 324113 |
| default-floor-normal-mixed | 24845619678 | 100000 | 324113 |
| default-floor-normal-retain | 24845619678 | 100000 | 235353 |
| default-floor-dev-plain | 24845619678 | 100000 | 324113 |
| default-floor-dev-mixed | 24845619678 | 100000 | 324113 |
| default-floor-dev-retain | 24845619678 | 100000 | 235353 |
| nightly-default-current-normal-plain | 24845619678 | 500000 | 724113 |
| nightly-default-current-normal-mixed | 24845619678 | 500000 | 724113 |
| nightly-default-current-normal-retain | 24845619678 | 500000 | 635353 |
| nightly-default-current-dev-plain | 24845619678 | 500000 | 724113 |
| nightly-default-current-dev-mixed | 24845619678 | 500000 | 724113 |
| nightly-default-current-dev-retain | 24845619678 | 500000 | 635353 |
| nightly-default-floor-normal-plain | 24845619678 | 500000 | 724113 |
| nightly-default-floor-normal-mixed | 24845619678 | 500000 | 724113 |
| nightly-default-floor-normal-retain | 24845619678 | 500000 | 635353 |
| nightly-default-floor-dev-plain | 24845619678 | 500000 | 724113 |
| nightly-default-floor-dev-mixed | 24845619678 | 500000 | 724113 |
| nightly-default-floor-dev-retain | 24845619678 | 500000 | 635353 |
| nightly-seed-2-current-normal-plain | 2 | 500000 | 724113 |
| nightly-seed-2-current-normal-mixed | 2 | 500000 | 724113 |
| nightly-seed-2-current-normal-retain | 2 | 500000 | 635353 |
| nightly-seed-2-current-dev-plain | 2 | 500000 | 724113 |
| nightly-seed-2-current-dev-mixed | 2 | 500000 | 724113 |
| nightly-seed-2-current-dev-retain | 2 | 500000 | 635353 |
| nightly-seed-2-floor-normal-plain | 2 | 500000 | 724113 |
| nightly-seed-2-floor-normal-mixed | 2 | 500000 | 724113 |
| nightly-seed-2-floor-normal-retain | 2 | 500000 | 635353 |
| nightly-seed-2-floor-dev-plain | 2 | 500000 | 724113 |
| nightly-seed-2-floor-dev-mixed | 2 | 500000 | 724113 |
| nightly-seed-2-floor-dev-retain | 2 | 500000 | 635353 |
| release-plain-1 | 1 | 100000 | 324113 |
| release-plain-2 | 2 | 100000 | 324113 |
| release-plain-3 | 3 | 100000 | 324113 |
| release-retain-1 | 1 | 100000 | 235353 |
| release-retain-2 | 2 | 100000 | 235353 |
| release-retain-3 | 3 | 100000 | 235353 |

Focused controls passed for the verdict pipe, retaining loss, partition, array widening, root node-table ordering, scalar precedence and release roster. The previous release plain seed 2 failure on map_crews mutation SHA-256 `9181751c54d3d2ef4d8d91a8198da820285cda81a96c5ad60fd144bcc98e8f43` now passes. These results qualify the wire-fuzz lane at this source; they do not assert that a separate full Python package run or any unmerged prerequisite has passed.

Several default-row harness times were 121–132 seconds before setup. These results do not establish that the rows fit the fleet's 120-second per-job budget; CI readiness needs a coverage-preserving timing repair. Focused control results are from a successful package-level log, rather than individually verbose receipts.

The later assembled Python source `f5acc48a62f69fd53075d31ebb13d0d6dd313d35` was separately generated and compared with this qualified source: all 34 selected package names and all 1,466 generated package files were byte-identical, with zero generation refusals. The later output also has an auxiliary `tblr1/_framing` directory outside the selected unit-loader packages. The engine, IR, harness, corpus fixture inputs and Python fuzz driver are unchanged between these sources. The campaign above remains the `1aa` run; its tested revision is not relabeled. The integrated CI source adds independently checked lossless sharding and controls.
The two sorted per-file SHA-256 inventories each contain 1,466 entries and are byte-identical; their inventory SHA-256 is `1d02380137eeddbb660176a592e9429667a9a62fd1d93806b1e1cd3c6e5b48c4`. The local raw inventories and generation receipts are retained under `python-acceptance-final/` in the landing workspace.
