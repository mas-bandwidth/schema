[goldens | rest] = System.argv()

defaults =
  List.first(rest) || Path.expand("../../../build/packet-defaults/elixir/defaults", __DIR__)

plain = Path.expand("../../../build/packet-defaults/elixir/plain", __DIR__)
Code.require_file("PacketRuntime.ex", defaults)
Code.require_file("Defaults.ex", defaults)
Code.require_file("PacketRuntime.ex", plain)
Code.require_file("Plain.ex", plain)
Code.require_file("suite.exs", __DIR__)
PacketDefaultTest.run(goldens)
