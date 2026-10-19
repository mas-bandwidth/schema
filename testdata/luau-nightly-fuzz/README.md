# Luau nightly fuzz saved inputs

Every finding the nightly fuzzer (`make luau-nightly-fuzz`) makes is written
to `build/luau-nightly-fuzz/findings/` as `<kind>-<unit>-<root>-<seed>-<iteration>.bin`
with a sibling `.bin.meta` (`seed=`, `iteration=`, `kind=`, `unit=`, `root=`,
`reason=`, and `minimised=yes` once the minimiser has replaced the raw input).
On a red night the workflow uploads that directory and the fuzz log as the
`luau-nightly-fuzz-findings` artifact (30 days). Nothing is committed by the
job: a person copies the input here under a name of its fix card, with its
`.meta`, in the fix card's or the fuzz card's own change. The fast lane
replays every saved input with `make luau-nightly-fuzz-replay`; the seed and
iteration reproduce the original (unminimised) input from the fuzzer's own
arguments, and `harness=` names the harness variant that found it.

A `pending=<fix-card>` line marks a real reader bug that is carded on its own
and not yet fixed: its red prints `REPLAY-PENDING` by name and does not fail
the replay. When that card lands the replay prints `REPLAY-PENDING-GREEN`;
then delete the `pending=` line so the input gates like every other.

| input | found at | reader | replay at the current base |
| --- | --- | --- | --- |
| `luau-load-negative-id-count.bin` | 088e3e73 | load.luau `open()`: a high-bit id count passed the bound | green (fixed by luau-load-negative-id-count) |
| `luau-fixed-layout-bounds.bin` | 088e3e73 | fixed.luau: a forged layout read past the record | green |
