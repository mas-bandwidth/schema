# The Elixir packet end-check gate (SPEC §5). Loads the generated End codec
# from build/packet-end/elixir, or from the directory given as the second
# argument (the negative control's sabotaged build), then runs suite.exs over
# the shared packet-end vectors.
vectors =
  List.first(System.argv()) ||
    Path.expand("../../../testdata/wire/packet-end", __DIR__)

base =
  Enum.at(System.argv(), 1) ||
    Path.expand("../../../build/packet-end/elixir", __DIR__)

Code.require_file("PacketRuntime.ex", base)
Code.require_file("End.ex", base)
Code.require_file("suite.exs", __DIR__)
PacketEndTest.run(vectors)
