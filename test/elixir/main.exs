# The Elixir cross-language wire test: the generated Elixir modules write the
# SAME pinned instances the C++ test pins in testdata/wire/*.bin and
# byte-compare against those files — cross-language wire identity is the
# §7.2 gate this leg carries. Plus round-trips through the Elixir reader, the
# §5 branch-zeroing checks, the specified-defaults checks, the measure
# functions held to the written wire, the refusal vectors (reject, never
# clamp — and never raise on hostile bytes), the always-on writer contracts
# (ArgumentError — the BEAM has no compile-out assert, so there is no
# checked/release twin), the {:nonfinite, bits} float convention, and the
# bench-corpus pins (bench_*, real_packet) the C++ bench pinner authored.
#
# Prints OK and exits 0, exactly like its C++/Go/JS/Dart twins. Run from
# test/elixir (the Makefile does): the wire goldens are at
# ../../testdata/wire.
#
# This file only loads: an .exs compiles as a whole before it runs, so a
# struct literal of a generated module can appear only in a file compiled
# AFTER the generated modules exist — suite.exs, required last. Generated
# files load in dependency order (a defstruct default holding %Other{} needs
# Other compiled first).
for f <- ["Constants.ex", "Enums.ex", "Types.ex", "Render.ex", "Wire.ex"] do
  Code.require_file(f, Path.join(__DIR__, "../../generated/elixir"))
end

Code.require_file("Bench.ex", Path.join(__DIR__, "../../generated/bench/elixir"))
Code.require_file("RealWorld.ex", Path.join(__DIR__, "../../generated/bench/elixir/realworld"))
Code.require_file("suite.exs", __DIR__)
