# Map-key widening reset (Lobster load): before/after map-load measurement

Card schema-lobster-map-key-2731 carries one runtime change in the Lobster
variable-form load
(`internal/codegen/lobstertable/runtime/load.lobster`, `map_key`, ~line 1590):

```lobster
if kind != want and widens(kind, want):
    kind_bad = false   // added: the final widened key wins
    widened = true
```

Before the change, a repeated map key whose first kind was incompatible left
`kind_bad` set even when a later key widened to a valid kind, so the entry was
discarded though the last key must win. The added store clears the flag when
the final key widens, so the repaired entry is kept.

## Hot-path effect

The added instruction is a single store of `false` into `kind_bad`, *inside*
the `if kind != want and widens(kind, want)` branch. For every map key that
does not take that branch — every ordinary key, and every key on the whole
non-repeated map-load path — the instruction never executes. The measured
warm path is therefore structurally unchanged by the fix.

The one workload whose *behaviour* changes is the exact case the fix repairs: a
repeated key that widens. There the load now does more work (it keeps the
entry instead of dropping it), which is the intended semantic repair, not an
instruction-cost regression.

## Measurements

Local runs of the pinned toolchain (`lobster v2026.8`, JIT,
`--runtime-no-asserts`, `dist/lobster-v2026.8`), 200,000 loads per run of
`load_tolerant("Pairs", data, rep)` over the committed fuzz vectors, on this
machine. These are studio runs, not a fleet bench.

Shared vector `testdata/wire/tables/fuzz-vectors/map_repeated_key_widen.bin`
(the repeated-key-widening case the fix targets):

| | run1 | run2 | run3 | run4 | median |
| --- | --- | --- | --- | --- | --- |
| with fix  | 0.920s | 0.877s | 0.890s | 0.885s | 0.887s |
| without fix | 0.448s | 0.448s | 0.452s | 0.538s | 0.450s |

The difference is the repair: with the fix the widened entry is kept (a map
insert is performed); without it the entry is discarded early. It is the cost
of correctness on the formerly-broken wire, and it is confined to that wire.

Single-key widen vector
`testdata/wire/tables/fuzz-vectors/map_key_widen.bin` (no repeated key, so
before/after do identical work): runs were dominated by host noise on this
machine (0.66–1.65s across runs on both sides) and were not treated as
conclusive. On this path the added line is unreachable, so no throughput change
is possible.

## Verdict

The hot map-load decoder is not slower for any workload whose behaviour the fix
does not change; where behaviour changes, it is the intended repair. A fleet
bench was not runnable in the card's sandbox; these numbers are local, pinned-
toolchain runs reported openly.
