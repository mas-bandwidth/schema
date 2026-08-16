# probe-layout — the discriminating run, 2026-08-17 (era probe-layout; NEVER rel-tabled against corpus eras)

## What ran
7 configs x 3 interleaved rounds, victims (bench_bits write, bench_mixed write,
probebits read) + flat controls (bench_ints write, bench_packet write):
off / armed / armed+PAD{16,1024,4080} (T1, zero rebuild, SP-only) /
armed_pad64 (T2, all code +0x64) / armed_align128 (T3, -falign-functions=128,
verified 43/45 functions 128-aligned). Then the decisive control: the ORIGINAL
confirmation-era binary (conf_cpp_armed, md5 46cd8192..., distinct from the
probe rebuild 5a34c685...), 3 fresh executions.

## The result NONE of the tree arms anticipated
THE PHENOMENON DID NOT REPRODUCE — in any configuration, including the
original bytes:

| config | bits write | mixed write | probebits read |
|---|---:|---:|---:|
| off | 181M | 153M | 782M |
| armed (rebuild) | 186M | 154M | 778M |
| armed PAD 16/1024/4080 | 189-190M | 150-155M | 776-780M |
| armed pad64 (code shift) | 188M | 154M | 777M |
| armed align128 | 185M | 155M | 779M |
| **conf_cpp_armed (original), 3 execs** | **180-190M** | **152-154M** | **773-779M** |

Confirmation-era record for the SAME conf_cpp_armed bytes: 38M / 42M / 94M
across 14 fresh executions in two passes, spreads 0.4-7%.

- T1 invariant => not stack-residue keyed; AND the env-size audit PASSES
  (recorded numbers did not depend on environment size tonight).
- T2 invariant, T3 moot (baseline already healthy).
- Same bytes, healthy tonight => not PC-keyed within the binary either.

## Verdict: arm (d) — system-state-bound, not binary-bound
The collapse required system state present during the confirmation window and
absent tonight. It was execution-stable WITHIN that state (14 fresh processes,
fresh ASLR each, all collapsed) and is gone WITH it. Leading candidate, the
one declared difference of that window: the CONCURRENT COMPILE-ONLY
ATTRIBUTION WORKER (shared L2 / cluster scheduling pressure that a
row-selective bits-heavy loop feels 4-8x while every other row and the control
leg sail) — plausible, UNPROVEN.

## The standards exposure (real, and different from the anticipated one)
BENCH-STANDARD S2.6's control legs certified those windows (deltas 0.0-0.3%)
while three rows sat collapsed 4-8x: A CONTROL LEG ONLY CERTIFIES THE WINDOW
FOR ITS OWN BINARY'S SHAPE. Row-and-binary-selective state passes the gate
undetected. The standard needs a clause: cross-binary A/A legs (the same
source built twice / run twice) or per-row historical bands as a second gate.

## The one experiment that discriminates next
Re-run conf_cpp_armed victims INTERLEAVED WITH a live compile worker
(recreate the suspected state): collapse returns => named (concurrent-compile
cache/scheduler state); stays healthy => candidate falsified, hunt continues.

## Disposition recommendation for #55
REMAINS HELD, with the reason UPDATED: not "reproducible collapse" but
"victim rows bound to unidentified system state — no single window can
certify the switch either way until the state is named." The retrial follows
the worker-concurrency experiment, not another clean-window pass.
