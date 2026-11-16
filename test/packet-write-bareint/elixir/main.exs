# The Elixir packet bare-integer write gate (SPEC §5.1 row 1). Loads the
# generated BareInt.schema modules from build/packet-write-bareint/elixir, or
# from the directory given as the second argument (the negative control's
# sabotaged build), then runs suite.exs over the shared cases list.
cases =
  List.first(System.argv()) ||
    Path.expand("../cases.txt", __DIR__)

base =
  Enum.at(System.argv(), 1) ||
    Path.expand("../../../build/packet-write-bareint/elixir", __DIR__)

Code.require_file("PacketRuntime.ex", base)
Code.require_file("BareInt.ex", base)
Code.require_file("suite.exs", __DIR__)
PacketWriteBareIntTest.run(cases)
