args = System.argv()
contracts = args == ["--contracts"]

base =
  if args == [] or contracts,
    do: Path.expand("../../../build/packet-wide/elixir", __DIR__),
    else: hd(args)

Code.require_file("WideText.ex", base)
Code.require_file("../../../build/packet-wide/elixir/shapes/Shapes.ex", __DIR__)
Code.require_file("suite.exs", __DIR__)
if contracts, do: PacketWideTest.contracts(), else: PacketWideTest.run()
