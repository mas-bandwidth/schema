[goldens | rest] = System.argv()

defaults =
  List.first(rest) || Path.expand("../../../build/packet-defaults/elixir/defaults", __DIR__)

Code.require_file("Defaults.ex", defaults)
Code.require_file("Plain.ex", Path.expand("../../../build/packet-defaults/elixir/plain", __DIR__))
Code.require_file("suite.exs", __DIR__)
PacketDefaultTest.run(goldens)
