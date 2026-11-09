# moo fixed form — hot-path before/after (fleet, vision)

Date: 2026-11-09 (UTC sittings 02:32–02:41 on vision)
Host: vision (nova-fleet row, 100.127.21.49), x86_64, AMD Ryzen Threadripper
2990WX 32-Core Processor, 64 logical CPUs, 121 GiB RAM, Ubuntu 26.04.1 LTS,
kernel 7.0.0-43-generic.
Toolchain: go1.26.10 linux/amd64. Interpreter: moo 1.3.0 (Luau 0.741), the
pinned mas-bandwidth/moo v1.3.0 `f60f0239f41c6c0a827ecee44a7f9539c5798068`
(binary sha256 `9c0b11404b4cdbc2c93482322de7814c55430e924e349467fb4480bb38b5950e`).

## The change under measure

#2890 (moo fixed form: the one read, MOO-1), merged as e328c525e. The test
builds one generated unit twice from the same schema and swaps only the
runtime: `testdata/table_fixed_before.moo` (the runtime before the cut,
sha256 `5c18749305062aade552862f1d1745df172a923adeaf11810197839555bad290`,
pinned in the test) against the runtime emitted at this base. The timed
program constructs a 64-record batch, saves it once, then runs 2,000
loads or saves over it; each arm's zero-iteration run is subtracted from its
2,000-iteration run, and the two arms alternate inside each of 8 interleaved
pairs. The reported number is the median ns per record over the 8 pairs.

## Harness repair (this card only)

At base 65bc77bc4e the bench schema does not load. Its optional field was
declared `opt ?uint16 | min = 1, max = 9000`; an absent optional reads as the
type's zero, so a range that excludes zero is refused at this base
(internal/check/check.go, "the implicit zero default is outside it"). The
one-line repair in `fixed_hotpath_bench_test.go` widens the declared range to
`min = 0`, which keeps the field optional and keeps the presence byte in the
timed record. The timed program always sets `OptPresent = true` and `Opt = 42`,
so the measured record is unchanged; the original line was simply not
buildable at this base.

The card's sandbox did not grant the fleet Go toolchain, so the same command
ran in a local ubuntu:24.04 container on vision with the host
`/home/glenn/nova-bench/sdk/go1.26.10` and the pinned moo 1.3.0 mounted
read-only. No other process of the measurement ran in that container.

## Command

    env SCHEMA_SLOW=1 SCHEMA_MOO_FIXED_BENCH=1 go test -count=1 -v -timeout 900s \
      -tags schema_leg_moo ./internal/codegen/mootable/ \
      -run '^TestMooFixedHotPathBench$'

## Result — the measured sitting (02:40:20–02:41:17 UTC)

Host load at start 11.60 / 19.87 (1-min / 5-min); at finish 7.98 / 17.36.

    load ns/record: before 12,305 [11,882, 13,136] -> after 12,251 [12,044, 13,693]  -0.44%
    save ns/record: before  8,299 [ 7,817,  8,417] -> after  8,153 [ 7,943,  8,269]  -1.76%

Ratio after/before: load 0.996, save 0.982. The cut is not slower.

## Repeat sitting (02:39:08–02:40:13 UTC)

Host load at start 11.66 / 21.85; at finish 12.26 / 20.14.

    load ns/record: before 13,859 [13,017, 18,308] -> after 13,813 [13,119, 17,210]  -0.33%
    save ns/record: before  9,703 [ 9,114, 10,155] -> after  9,482 [ 9,045,  9,740]  -2.28%

Ratio after/before: load 0.997, save 0.977. Same direction and magnitude as
the measured sitting.

## Noise note

A first sitting (02:32:53–02:34:12 UTC) started at 1-min load 3.10 but other
fleet work raised it to 25.65 by the finish; it printed load +5.06% and save
+0.67% with wide ranges and absolute times roughly 1.6x the two quiet
sittings (load before 20,356 [13,155, 24,845], after 21,386 [13,964, 24,056]).
That sitting is reported as co-tenant noise, not as evidence about the cut.
The two later sittings above are the measurement; both were tight and agree
that the after runtime is marginally faster, not slower. A certified sitting
on a fully idle bench is still owed if the coordinator wants one; this is the
quietest window the assigned row gave, and it is not a gate.

## DONE-WHEN

    env SCHEMA_SLOW=1 SCHEMA_MOO_FIXED_BENCH=1 go test -count=1 -v -timeout 900s \
      -tags schema_leg_moo ./internal/codegen/mootable/ \
      -run '^TestMooFixedHotPathBench$'

PASS, exit 0 (44.83s on the measured sitting; 52.05s and 67.27s on the two
other sittings).
