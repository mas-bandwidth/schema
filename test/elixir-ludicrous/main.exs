# The Elixir cross-language wire test for the fixed-point + 128-bit unit
# (examples128/): the generated Elixir module writes the SAME pinned instance
# test/ludicrous_main.cpp pins in testdata/wire/ludicrous_state*.bin and
# byte-compares against those files — cross-language wire identity for the
# serialize-phase-1 families (fixed(I, F), ufixed(I, F), int128, uint128) is
# the §7.2 gate this leg carries. Plus round-trips, the §5 branch-zeroing
# check over a 128-bit field, the specified-defaults checks (one default no
# 64-bit literal can spell — a plain BEAM integer here), and the hostile
# reads (reject, never clamp — STANDARD.md).
#
# Prints OK and exits 0. Run from test/elixir-ludicrous (the Makefile does).
# Mirrors test/ludicrous_main.cpp block for block; 128-bit values ride plain
# BEAM integers. Split loader/suite for the reason test/elixir/main.exs
# states.
Code.require_file("Ludicrous.ex", Path.join(__DIR__, "../../generated/elixir-ludicrous"))
Code.require_file("suite.exs", __DIR__)
