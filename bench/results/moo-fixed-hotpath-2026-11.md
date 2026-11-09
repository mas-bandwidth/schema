# moo fixed form — hot-path before/after (fleet, hetzner2)

Date: 2026-11-09 (UTC). Measured sitting 02:58:22–02:58:43Z. Repeat
02:59:35–02:59:55Z. Code under measure is b79de070 (this commit adds only
this receipt).

Host: hetzner2, x86_64, AMD Ryzen 9 7950X3D, 16 cores, 32 logical CPUs
(nproc 32), 124 GiB RAM, Ubuntu 26.04.1 LTS, kernel 7.0.0-43-generic.
Toolchain: go1.26.10 linux/amd64 (`/home/nova/sdk/go1.26.10/bin/go`),
`GOFLAGS=-mod=readonly`. Interpreter: moo 1.3.0 (Luau 0.741), the pinned
mas-bandwidth/moo v1.3.0 `f60f0239f41c6c0a827ecee44a7f9539c5798068`,
invoked as `$MOO` =
`/home/nova/jobs/schema-moo-cards.w8/repo/dist/moo/moo`
(binary sha256 `9c0b11404b4cdbc2c93482322de7814c55430e924e349467fb4480bb38b5950e`).
That binary was not copied into this tree and is not part of the commit.

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

The bench schema's optional field is `opt ?uint16 | min = 0, max = 9000` on
this branch (parent commit b79de070). At the base it was `min = 1`, which
this base refuses because an absent optional reads as the type's zero. The
timed program always sets `OptPresent = true` and `Opt = 42`, so the
measured record is unchanged. This commit does not touch that line.

## Command

The measurement now lives in the moo runner directory, `bench/tables/moo/`
(the file and its baseline fixture moved there unchanged, so the card
`moo-bench-fixed-hot-path`'s workload and numbers stay reproducible).

    env SCHEMA_SLOW=1 SCHEMA_MOO_FIXED_BENCH=1 go test -count=1 -v -timeout 900s \
      -tags schema_leg_moo ./bench/tables/moo/ \
      -run '^TestMooFixedHotPathBench$'

`GOFLAGS=-mod=readonly` and `MOO` set as above. The 1-minute load was read
before the command and the run was not started at 8 or higher.

## Result — the measured sitting (02:58:22–02:58:43Z)

Host load at start 3.52 / 7.09 / 9.51 (1 / 5 / 15 min); at finish
3.16 / 6.78 / 9.36. The 15-minute figure is the hour before this window
(the host had been busier); the 1-minute load at the start was 3.52.
Wall time 20.253s, exit 0.

    load ns/record: before 5576 [5468, 5643] -> after 5537 [5459, 5787]  -0.69%
    save ns/record: before 3659 [3604, 3790] -> after 3656 [3631, 3733]  -0.06%

Ratio after/before: load 5537/5576 = 0.993, save 3656/3659 = 0.999.
Not a regression. The cut is not slower.

## Repeat sitting (02:59:35–02:59:55Z)

Host load at start 4.29 / 6.41 / 9.09; at finish 3.62 / 6.12 / 8.93.
Wall time 19.830s, exit 0.

    load ns/record: before 5407 [5333, 5509] -> after 5397 [5305, 5668]  -0.18%
    save ns/record: before 3540 [3498, 3843] -> after 3531 [3502, 3931]  -0.27%

Ratio after/before: load 5397/5407 = 0.998, save 3531/3540 = 0.997.
Same direction as the measured sitting: after is marginally faster, not
slower. No measured regression on either sitting.

## Earlier sitting already on this branch (vision, not this measurement)

b79de070 recorded a vision sitting (64 logical CPUs, 1-minute load 11.60 at
start) of load 12,305 -> 12,251 ns/record (-0.44%, ratio 0.996) and save
8,299 -> 8,153 (-1.76%, ratio 0.982), plus a repeat at load 11.66 and a
contended first pass at a finish load of 25.65 that printed load +5.06%.
Those numbers stay the record of that busier host. They agree in direction
with the quiet hetzner2 sittings above. Absolute ns/record is lower here
because this CPU is faster; the ratio is the comparison. This receipt does
not re-run vision.

## DONE-WHEN

    env SCHEMA_SLOW=1 SCHEMA_MOO_FIXED_BENCH=1 go test -count=1 -v -timeout 900s \
      -tags schema_leg_moo ./bench/tables/moo/ \
      -run '^TestMooFixedHotPathBench$'

PASS, exit 0, on hetzner2, on the code this receipt measures (20.253s and
19.830s). This commit changes only this file.
