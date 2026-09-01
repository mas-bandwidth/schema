# elixir round two — ROUND-LOG

One line per milestone: lever name, paired numbers, decision. A future resume
reads the round's state from this file plus the branch commits alone.

- RESUME 2026-09-01: process death mid-round; branch had 2e71846 (fr fast path,
  Veltkamp/Dekker) only. Its profile findings died with the worker — re-profiling.
  Standing: elixir 1364% (bench/results/2026-09-01-sitting3-arm64-macbook.csv:
  write 449772 msg/s, round_trip 262329 msg/s). Board home: issue #174.
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
