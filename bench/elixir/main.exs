# schema bench — the Elixir runner's entry point. The generated monomorphic
# codecs over the four bench/corpus/Bench.schema shapes, measured under the
# BENCH-STANDARD contract: fixed iteration counts identical to every other
# runner's rows, 1 discarded warmup run then 7 measured runs per
# (bench, path) (--round K: one measured run; --quick: bench_mixed only,
# 3 runs at a reduced count), per-iteration LCG variation, 64 rotating
# variant read buffers, CSV v2 rows on stdout under --csv. The contract,
# the golden gate, and the full-struct read sink live in runner.exs — this
# file only loads, because an .exs compiles as a whole before it runs, so
# the struct literals must live in a file required after the generated
# modules.
#
# run.sh invokes it from bench/elixir under the repo-pinned BEAM toolchain:
#
#   cd bench/elixir && PATH="<dist otp>:<dist elixir>:$PATH" \
#       elixir main.exs [--csv] [--round K] [--quick]
Code.require_file("Bench.ex", Path.join(__DIR__, "../../generated/bench/elixir"))
Code.require_file("runner.exs", __DIR__)
