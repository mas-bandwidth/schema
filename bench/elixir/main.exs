# schema bench — the Elixir runner (minimal form): the generated monomorphic
# codecs over the four family `rt` shapes (BENCH-STANDARD.md §1.3), measured
# with serialize.elixir's own bench methodology so the rows read side by side
# with that library's — same LCG, same vary-function field mappings, same
# iteration counts, same 64 read variants, same best-of-five discipline,
# same units and reporting format (serialize.elixir bench/bench.exs is the
# reference for all of it). This is the issue #167 prediction instrument:
# generated single-function accumulator-threaded code vs the library's
# per-op immutable streams (the measured 7x allocation ceiling).
#
# GOLDEN GATED (§1.5): before any row is timed, every pinned corpus
# instance is written and byte-compared against the C++-pinned
# testdata/wire golden, and all 64 LCG variant buffers of every shape are
# decoded back with every field verified. A runner that mismatches REFUSES
# to bench.
#
#   cd bench/elixir && PATH="<dist toolchain>:$PATH" elixir main.exs
#
# Iteration counts are overridable, the library bench's own env names:
#   BENCH_STREAM_PACKETS=100000 elixir main.exs
#
# Full run.sh/CSV-preamble integration per bench/README.md is a named
# follow-on (the same standing as the Dart runner); this runner carries the
# golden gate, the methodology and the rows.
#
# This file only loads — an .exs compiles as a whole before it runs, so the
# struct literals live in runner.exs, required after the generated modules.
Code.require_file("Bench.ex", Path.join(__DIR__, "../../generated/bench/elixir"))
Code.require_file("runner.exs", __DIR__)
