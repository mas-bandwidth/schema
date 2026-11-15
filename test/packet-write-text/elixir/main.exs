# The Elixir packet Write text gate (SPEC §5.1 row 8): the writer holds the
# used bytes of string(N) to no zero byte and well-formed UTF-8, by a refusal
# in every build. Loads the generated modules from build/packet-write-text/elixir,
# or from the directory given as the first argument (the negative control's
# sabotaged build), then runs suite.exs over the shared cases beside it.
# Prints OK and exits 0; prints a FAILED line per broken case and exits 1.
base =
  List.first(System.argv()) ||
    Path.expand("../../../build/packet-write-text/elixir", __DIR__)

Code.require_file("PacketRuntime.ex", base)
Code.require_file("Text.ex", base)
Code.require_file("suite.exs", __DIR__)
PacketWriteTextTest.run()
