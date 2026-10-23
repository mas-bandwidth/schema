# Dart: the fixed read against its own packet read — receipted window, both lanes FAIL

Card `dart-fixed-read-beats-its-packet-read` (attempt 7), roadmap tasks
`gates/packet-read/dart/identity` and `gates/packet-read/dart/plan`. Two sittings of the read arm
(`bench/paired/nine.sh --read-arm --leg dart --receipt <receipt> --pin 7 --out <scratch>`) inside a
receipted operator window. They replace, for credit, attempt 5's `certified/` rows, whose receipt was
a self-written `ps -A` claim. Those rows are kept unchanged as history. Both GATE verdicts are **FAIL**
in both sittings, as the driver printed them, so neither roadmap task is flipped.

| sitting | identity read | plan read | packet read | GATE identity | GATE plan | plan/identity (reported) | window | control delta |
|---|---:|---:|---:|---|---|---:|---|---:|
| 1 | 546.1 | 1665.6 | 247.0 | FAIL | FAIL | 3.05 | OK | 0.15% |
| 2 | 548.9 | 1669.4 | 247.2 | FAIL | FAIL | 3.04 | OK | 2.42% |

ns/record, each row's BEST round of 7. Best / median / worst and spread of rates:

| row | sitting 1 | spread | sitting 2 | spread |
|---|---|---:|---|---:|
| `bench_fixed_read` (identity lane, family table) | 546.1 / 548.0 / 567.5 | 3.8% | 548.9 / 556.7 / 570.8 | 3.9% |
| `bench_plan_read` (plan lane, lineage file) | 1665.6 / 1674.9 / 1682.1 | 1.0% | 1669.4 / 1708.8 / 1714.0 | 2.7% |
| `bench_mixed_read` (packet read, family gen) | 247.0 / 247.5 / 250.1 | 1.2% | 247.2 / 253.6 / 254.4 | 2.9% |

Identity is 2.2x the packet read and plan is 6.7x it. This agrees with attempt 5 on b1 within 1%.

## Declared conditions

- **Host:** `b2` (fleet member amd-2), linux/amd64, AMD Ryzen 7 255, pinned to core 7.
- **Revision:** `96ef7606a7d27454e0095adde34fef88cf1837af` on
  `sprint/dart-fixed-read-beats-its-packet-read.w7.g1.e21`, pushed to origin before either sitting.
  It is attempt 5's runnable support (0542b5b49) merged cleanly with sprint/schema-dart c5b83298a.
  The board marks it dirty with build-regenerated `generated/` files only. Those files were restored
  between the two sittings, because the driver refuses a second sitting over them.
- **SDK:** Dart 3.13.2 (stable), AOT executables. **Corpus:** corpus_id 0930f96c8320a75d.
  **Iterations:** 7 rounds × 4,000,000 records, 1 warmup + 1 measured run per round.
- **Load (1-minute average, before / after):**
  - sitting 1 (22:08:39–22:11:10Z): 0.35 / 1.10; driver range 0.40–1.14.
  - sitting 2 (22:19:18–22:21:49Z): 0.08 / 0.95; driver range 0.16–0.95.

## The window and its receipt

The authority is Glenn Fiedler's live ruling to Rowan on 2026-10-16: "you can simply hold a fleet
machine and work there. go."

The coordinator set up the window:
- held amd-2 (op hold-dm6lc6q26oio-24125-80d9f0f5aee3bfa9~21-1);
- quieted it until 23:46:33Z (op fleet-quiet-dm6lc6r3zqz4-24125-48e507d968a6c611~21-1), from 21:46:33Z;
- stopped its four CI runner services after their last job drained, at 21:49:17Z
  (nova-runner-1, nova-runner-nova-sprint-1, nova-runner-rocketnet-1, nova-runner-schema-1).

That record is the READY/START receipt. It was passed verbatim as `--receipt` and is archived as
`receipt.txt` beside each sitting's `read.json` in `window-2026-10-16/sitting-N/`.

Three refused second sittings are not here. The driver stopped each one before the clock on the
leftover regenerated files.

## The profile that would find the cause

A Dart AOT profile of the identity and plan lanes. Run the runner with `--observe` and the VM
service's CPU profiler (or `perf record` over the AOT binary with `--generate-perf-events-symbols`).
Then look at:
- per-field accessor calls that were not inlined;
- `ByteData` bounds checks on each read;
- the boxed doubles the plan lane widens into.

## Not done

- The roadmap tasks stay `:unknown`. No `make roadmap` was run, since nothing was flipped.
