# probe-concurrency — the last experiment of the night (era probe-concurrency; never rel-tabled)

## Design and result
Original conf_cpp_armed bytes, 6 alternating blocks (Q quiet / T with a live
clang compile loop), 2 execs per block, victims + flat controls.

| block | bits write | mixed write | probebits read | ints write (ctl) |
|---|---:|---:|---:|---:|
| Q1 | 38.4M | 42.7M | 94.3M | 185M |
| T1 | 36.9M | 40.5M | 89.6M | 181M |
| Q2 | 37.4M | 41.5M | 94.2M | 189M |
| T2 | 37.2M | 40.8M | 89.5M | 182M |
| Q3 | 37.6M | 41.5M | 94.3M | 185M |
| T3 | 36.8M | 40.5M | 89.5M | 181M |

**Arm (b): CANDIDATE FALSIFIED — and harder than designed.** The collapse was
present in EVERY block including Q1, which ran before any churn had ever
started in that process tree. Treatment vs quiet differs only by ordinary
contention (2-5%). The compile worker is not the mechanism.

## The flip timeline (the decisive new fact)
Same conf_cpp_armed bytes, same machine, same night:
- probe-layout window: HEALTHY x3 execs (180-190 / 152-154 / 773-779M)
- probe-concurrency window (~1h later, 15 min): COLLAPSED x12 execs at the
  exact confirmation-era plateaus (37-38 / 40-43 / 89-94M)
- minutes after the script ended: HEALTHY again — and off + rebuild healthy
  too (782/780/778M probebits read).

The state is BISTABLE with EXACT per-row plateaus, row-selective (controls
healthy throughout), temporally clustered, and immune to: env-string size
(T1), code shift +0x64 (T2), function alignment 128 (T3), concurrent compile
pressure (this experiment), binary bytes (same md5 both states), and process
freshness (fresh exec + fresh ASLR per round in every state).

## What was MISSED, and the first question for the next window
Tonight's collapsed window ran only the armed binary; the state flipped back
before an off-binary exec could test selectivity INSIDE the collapsed state.
(In confirmation, off and armed interleaved in the same rounds: off healthy,
armed collapsed — which read as binary-keyed. Tonight the same armed bytes
were spared in one window and hit in another, so binary-keying is now in
doubt.) NEXT WINDOW: a canary loop — run the armed victims' rows cheaply; on
detecting the collapsed plateau, IMMEDIATELY run bench_off's victims in the
same state. One exec answers whether the state selects binaries or rows.

## §2.6 amendment recommendation (enactable form)
A control leg certifies a window only for its own binary's shape: this
campaign measured three rows collapsed 4-8x across two windows whose control
deltas were 0.0-0.3%. Two instruments close the blind spot:
1. A/A TWIN LEGS: any pass introducing a NEW binary configuration MUST run
   that binary as two interleaved legs (same file, same inode, alternating
   round positions). A row whose twin ratio departs 1.0 beyond its spread
   band is marked `state-suspect`; the tool refuses to ratio it against other
   configurations, captioned "twin disagreement — state-selective
   interference detected in-window."
2. PER-ROW HISTORICAL BANDS: each host-era carries a rolling per-row band
   (min/max of prior VALID same-config rows). A row outside its band by more
   than the §2.3 noisy threshold is marked `band-break` and publishes only
   with the mark. (Tonight's plateaus sit 4-8x below band — instantly caught.)
Twins catch state within a pass; bands catch it between passes; both are
refusals-with-names, not silent corrections; neither needs new hardware.

## #55 disposition
REMAINS HELD. The victims' collapse is real, bistable, and bound to an
unidentified machine state that no staged perturbation controls. No single
window can certify the switch; the canary experiment above is the retrial's
precondition. The switch's wins (+33..+929% on generated writes) are equally
real and wait on the same answer.
