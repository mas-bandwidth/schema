# The Elixir packet compressed-float write gate (SPEC §5.1 row 10). Loads the
# generated CFloat.schema modules from build/packet-write-cfloat/elixir, or
# from the directory given as the first argument (the negative control's
# sabotaged build), then runs suite.exs, which compiles after them because it
# holds struct literals. Prints OK and exits 0; prints a FAILED line per broken
# verdict and exits 1.
base =
  List.first(System.argv()) || Path.expand("../../../build/packet-write-cfloat/elixir", __DIR__)

cases = Path.expand("../cases.txt", __DIR__)

Code.require_file("PacketRuntime.ex", base)
Code.require_file("CFloat.ex", base)
Code.require_file("suite.exs", __DIR__)
PacketWriteCFloatTest.run(cases)
