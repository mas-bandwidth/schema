# elixir round two — ROUND-LOG

One line per milestone: lever name, paired numbers, decision. A future resume
reads the round's state from this file plus the branch commits alone.

- RESUME 2026-09-01: process death mid-round; branch had 2e71846 (fr fast path,
  Veltkamp/Dekker) only. Its profile findings died with the worker — re-profiling.
  Standing: elixir 1364% (bench/results/2026-09-01-sitting3-arm64-macbook.csv:
  write 449772 msg/s MEDIAN, round_trip 262329 msg/s MEDIAN — the same row's
  round_trip MAX is 262385, which is the number the §2.9 statistic and the
  certification bullet below both quote; the two figures are the same sitting
  read off different columns, not a disagreement). Board home: issue #174.
- PROFILE (fresh, branch head, eprof 30k + tprof 200k iters, write and rt
  separately, outputs in session scratch): WRITE — w_bench_mixed_stats 26.7%,
  write_bench_mixed own 21.8%, w_bench_mixed_entities 16.0%, cf_quantize 14.8%
  (0.27us own/call), fr 13.0% (15 calls/msg), cf_clamp01 2.4%, f32/f64_bits 1.8%.
  RT — r_bench_mixed_stats 29.6% (0.09us/call, 41 calls/msg), w_stats 11.6%,
  fr 10.1% (27/msg), write_bm 7.7%, read_bm own 7.5%, w_entities 6.9%,
  cf_quantize 6.2%, r_entities 5.0%, cf_decode 4.5%, Enum.reverse+lists:reverse
  3.7%, binary:match 1.2%. The profile names: (1) stats read clause, (2) float
  pipeline (fr/cf_quantize), (3) reverse elimination, (4) write barrier appends.
  rd/rdw are compiler-inlined (absent from traces) — their cost rides the callers.
- HARNESS-VS-CODEC SPLIT (owner question, eprof by module): nothing was
  filtered from the profile summaries — the only non-codec entries eprof
  recorded inside the timed region are the driver loop frames themselves:
  write 1.58% (Prof.write_loop, the elem/&&&/byte_size/acc frame mirroring
  the runner's dd_write_loop), rt 1.03% (Prof.rt_loop); erlang:apply +
  compiler shims 0.00%. Everything else, including binary:decode_unsigned
  (rd/rdw tail fallback), binary:match (the reader's player_name NUL check,
  Bench.ex:2339), and lists:reverse/Enum.reverse (element-loop terminals), is
  the generated codec + emitted helpers: codec share 98.4% of write, 99.0% of
  rt. The certification runner's golden gates run OUTSIDE its timed region and
  its per-run sink is one Process.put per run, not per iteration. Harness is
  immaterial (<2% both paths, well under the 5% bar) — no harness-parity
  exclusion applies; lever math stands on codec time alone.
- INSTRUMENT FINDING: a paired run that measures write then rt in ONE VM
  poisons the rt comparison (heap state from the write loop; rt read 4500-4800
  ns/msg vs 3826 clean, matching certification's 262k msg/s). Lever decisions
  now use single-path pair runs. Between-invocation write deltas can flip sign
  (+4.8/-3.4%) — decisions use repeated invocations + max statistic.
- INSTRUMENT CALIBRATION (A/A null, and the correction it forces): the pair
  instrument timed arm A then arm B every round, with no rotation. It now
  ALTERNATES arm order by round parity. The A/A null — both arms the same
  module, a pure rename, wire gate 64/64 — is what says whether a ratio
  means anything, and it was run on all three paths at 400k iters x 7 rounds:
    read  B/A 0.998, 1.003        -> +/-0.3%, trustworthy
    rt    B/A 1.012, 0.995        -> +/-1.2%, trustworthy well above that
    write B/A 1.045, 0.959, 1.057 -> +/-5%,   NOT trustworthy at lever scale
  Rotation did not rescue the write path. The bias there is not a slot effect:
  within one invocation every round of one arm is faster than every round of
  the other, and which arm that is flips between invocations — a whole-VM
  layout/GC-regime effect the max statistic cannot average out. So the write
  path's null band is the size of every write lever this round decided, and
  the honest statement is that this instrument DOES NOT RESOLVE write-time
  differences of a few percent, in either direction.
  Allocation is the axis that survives: across every null run the two arms'
  words reclaimed agree to under 0.001% on write (794760324 vs 794763984) and
  under 0.02% on read and rt. Word counts are what the write-path decisions
  below now rest on. The read and rt TIMES stand on their own: read gains of
  11-14% sit ~40x above a 0.3% null, and rt gains of 7-8% sit ~6x above a
  1.2% null.
- LEVER fr (2e71846, inherited from dead worker): KEEP, ON ALLOCATION.
  Decisive metric — GC words reclaimed on the write loop 1021.8M -> 601.3M,
  -41% (the same -41% the round first measured at its own smaller scale,
  167M vs 284M): the JIT keeps the Veltkamp/Dekker temporaries unboxed, so
  the arithmetic path allocates one float where construct/match allocated a
  binary plus a float. That axis is deterministic and the A/A null puts the
  two arms within 0.001% of each other, so a 41% gap is real.
  Supporting: micros cf_decode 89.2->62.3 ns (1.43x), cf_quantize 108.9->83.4
  (1.31x).
  WITHDRAWN: the full-bench write timing (best-vs-best 2222 vs 2329 ns,
  1.048x, faster in 6/7 invocations) is INSIDE the write null's +/-5% band and
  is UNRESOLVED BY THE INSTRUMENT — it is not evidence for or against the
  lever. rt 1.0002x and read 1.003x were reported as neutral and remain so;
  both sit inside their own (much tighter) nulls, which is what "neutral"
  should mean.
- LEVER B (single-match-context stats read), PROTOTYPE VERDICT: build. Design:
  4 stats per 9-byte head-match (<<w1::little-40, w2::little-32, rest::binary>>,
  constant carry c = (8 - start_phase) mod 8; lo = carry ||| w1 <<< c up to 47
  bits, hi = lo >>> 36 ||| w2 <<< (c+4) up to 43 bits — fixnum envelope holds),
  body-recursive so the list builds IN ORDER on unwind (no Enum.reverse for the
  fast portion); scalar rd() path kept as entry fallback, tail (<4), and
  truncated-buffer fallback. +bin_opt_info: "OPTIMIZED: match context reused"
  at all three fast-loop sites. Paired vs head: read 1833.8 -> 1647.7 ns
  (1.113x), rt 3916.1 -> 3743.9 ns (1.046x); GC words down ~7%. Tail-call acc
  variant alone was read 1.071x/rt 1.03x; body-recursion added 1.036x/1.011x.
- LEVER B LANDED (emitter form): fastReadClauses in internal/codegen/elixir/
  functions.go — aligning entry + body-recursive single-match-context clause
  behind every qualifying array read loop (static element width <= 20 bits,
  plain fields only — no branches/unions/strings/arrays; m elements per
  iteration with m*eb = 0 mod 8, capped 72 bits/16 elems/ArrayBound). Feeds
  the existing element emission through readFeed (chained fixnum registers,
  runtime carry only ever `c + literal`). Emitter-form paired vs pre-lever
  head: read 1746.8 -> 1571.7 ns (1.111x, matching the 1.113x prototype), rt
  4173.0 -> 3867.8 best (1.079x best-vs-best on a thermally noisy sitting;
  prototype said 1.046x), write neutral (1.016x, noise). Wire gate 64/64
  byte-identical; test/elixir + test/elixir-ludicrous OK; mix format clean;
  goldens re-pinned (text only — wire bytes untouched). Also fires for
  bench_packet blob (9x8-bit) and examples arrays (Wire/Clauses/Degenerate).
- HYPOTHESIS A (iolist emission) REFUTED. Writers building [data | <<segs>>]
  trees with one :erlang.iolist_to_binary at return, wire gate 64/64 green.
  RE-MEASURED with the rotating instrument at 400k iters x 7 rounds:
  write 2596.3 -> 2943.7 ns/msg (1.134x SLOWER), rt 4051.2 -> 4359.5 (1.076x
  SLOWER), words reclaimed on the write loop 794.6M -> 1019.4M (1.283x MORE).
  The refusal stands on the timing, which is the one place a write-time claim
  survives the null: 13.4% is well outside the +/-5% band, and the direction
  reproduced in every arm order tried.
  CORRECTED: the first write-up said words 267M -> 582M (2.2x). That figure
  does not reproduce; the allocation penalty is 1.28x, not 2.2x. Direction
  unchanged, magnitude overstated.
  Mechanism: bs_append grows a writable binary in place (the segment copy is
  paid either way), while the iolist pays a fresh heap binary + cons per
  barrier AND the final flatten traversal+copy. Zero-intermediate-append
  emission is a loss on the BEAM; the lever-M multi-segment barrier append
  stands as the floor's write shape.
- LEVER u8 (write unroll 4->8) REFUSED: write 2462.4 vs 2496.8 ns (1.4%
  AGAINST), rt 0.99x noise — the append count per stat was already amortized
  at 4-wide; the fixnum cap bounds what wider grouping can remove (#202's law
  reconfirmed at the next scale).
- LEVER rc (combined range check, masked one-branch form) REFUSED: write
  2349.0 vs 2363.4 ns (0.6% against, noise) — predicted branches were free.
- LEVER C (cf decode tables) LANDED: a compressed-float read whose quantum
  count is <= 1024 decodes through a module-attribute tuple (elem/2) computed
  at GENERATED-module compile time by literally the cf_decode chain (full fr
  with guard + fr_slow + nonfinite mapping) — equality by construction, and
  verified: 201-entry domain === cf_decode on all values, wire gate 64/64.
  Prototype: read 1638.3 -> 1431.9 ns (1.144x), rt 1.069x. Emitter form:
  read 1652.6 -> 1446.6 (1.142x), rt 1.070x. Tables: Bench 1 (aim, 201
  entries), RealWorld 5, Wire 1. cf_decode stays the shipped path past 1024.
- LEVER cfq (per-declaration cf_quantize specialization, constants folded)
  REFUSED: write 2449.1 vs 2453.2 ns (0.2%, noise) — BEAM argument passing
  and literal loading are already free; the quantize cost is the float chain
  itself, which the wire contract pins step-for-step.
- REFUSALS UNDER THE NULL (u8, rc, cfq): all three are write-path deltas of
  0.2-1.4%, so all three sit deep inside the +/-5% write null and none of them
  was ever resolved by the instrument either. They STAND, unchanged, and the
  null makes them MORE secure rather than less: each was refused on a number
  pointing slightly AGAINST the candidate, and a bias that can manufacture a
  few percent in either direction cannot manufacture evidence for refusing —
  the worst it can do is hide a small win, and a win this instrument cannot
  see is not one worth carrying the complexity for. Each refusal also has a
  mechanism behind it (fixnum cap, free predicted branches, free literal
  loading), which is what the decision actually rests on.
- CONSIDERED, NOT BUILT: direct float segments in barrier appends (skip
  f32/f64_bits construct/match, ~1.8% of write) — refused by reasoning: the
  {:nonfinite, bits} write form makes every float segment conditional, which
  breaks the one-construction-per-barrier shape lever M bought; revisit only
  if a nonfinite-free declaration class ever exists.
- CERTIFICATION PAIR (quick legs, one sitting, --out scratch; §2.9 statistic
  is round_trip over MAX rates): before (clean worktree at origin/main
  b63c22e): write 427700 max / 426651 median, rt 253090 max / 242109 median
  (warm sitting — spreads 2.3/4.7%). after (branch head): write 449568 max /
  449244 median, rt 275088 max / 274908 median (spreads 0.1%). In-sitting rt:
  1.087x on max (medians 1.135x — the before rt max is a single cool-run
  outlier). The rt gain is the one that stands: it is corroborated by the
  paired instrument, whose rt null is +/-1.2%, and by the adversarial
  reviewer's independent re-run.
  WITHDRAWN: the in-sitting WRITE figure (1.051x on max, 1.053x on median) is
  the size of this round's demonstrated write-path measurement uncertainty and
  is UNRESOLVED. The certification runner is not the pair instrument, but the
  before leg's own write spread was 2.3% and the A/A null puts the pair
  instrument's write band at +/-5%; nothing this round measured can separate a
  5% write move from the noise. No write-time improvement is claimed. What the
  branch does claim on the write path is allocation: -41% words on the fr
  lever (above), an axis that reproduces to 0.001%.
  Against the standing sitting3 ledger point (rt max 262385, cpp rt max
  3579797, = 1364%): after rt max 275088
  puts elixir at ~1301% of generated C++ — cross-sitting projection, a full
  sitting re-certifies. CSVs: scratchpad/prof/cert-before-quick.csv,
  cert-after-quick.csv (session scratch, never bench/results/).
- GATES: make test green (nine legs), shape-gate clean (19 exemptions, all
  on ledger), generated-current green (tree clean post-test), mix format
  clean, gofmt/vet/modernize clean, ZERO warnings from every generated Elixir
  module (each required standalone; the whole generated tree, not just the
  legs make test compiles).
- DRAFT PR: https://github.com/mas-bandwidth/schema/pull/240 — round closed;
  left in draft per the round brief (never marked ready, never merged).
- ADVERSARIAL REVIEW, repaired: (1) cf_decode/4 was emitted dead in Bench.ex —
  needCf gated quantize and decode together and the write path set it, so a
  module whose every compressed-float read went through a table still carried
  the function, and the compiler said so. Split into needCfQ/needCfD; fr/1 and
  fr_slow/1 back both halves. Bench.ex is the only module affected, 16 lines
  removed, wire bytes untouched. (2) the pair instrument's missing rotation and
  the A/A null it hid — instrument fixed, null published above, every write-time
  claim in this log restated as unresolved. (3) the iolist allocation figure
  re-measured and corrected, 2.2x -> 1.28x. (4) the feed exclusion invariant
  and the fast clause's ceil(n/m) frame depth written into the emitter.
  (5) the sitting-3 median/max pair annotated at the RESUME line.
