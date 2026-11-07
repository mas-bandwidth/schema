# The Elixir packet finite-float gate (SPEC §4.3). Loads the generated
# Finite.schema modules from build/packet-finite/elixir, or from the directory
# given as the first argument (the negative control's sabotaged build), then
# runs suite.exs, which compiles after them because it holds struct literals.
# Prints OK and exits 0; prints a FAILED line per broken check and exits 1.
base =
  List.first(System.argv()) || Path.expand("../../../build/packet-finite/elixir", __DIR__)

Code.require_file("PacketRuntime.ex", base)
Code.require_file("Finite.ex", base)
Code.require_file("suite.exs", __DIR__)
PacketFiniteTest.run()
