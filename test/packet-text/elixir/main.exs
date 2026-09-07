base =
  List.first(System.argv()) || Path.expand("../../../build/packet-text/elixir", __DIR__)

Code.require_file("Text.ex", base)
Code.require_file("suite.exs", __DIR__)
PacketTextTest.run()
