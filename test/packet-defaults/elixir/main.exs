[goldens | rest] = System.argv()

defaults =
  List.first(rest) || Path.expand("../../../build/packet-defaults/elixir/defaults", __DIR__)

plain = Path.expand("../../../build/packet-defaults/elixir/plain", __DIR__)
Code.require_file("PacketRuntime.ex", defaults)
Code.require_file("Defaults.ex", defaults)
Code.require_file("PacketRuntime.ex", plain)
Code.require_file("Plain.ex", plain)

# One directory per constant-default pin: both schemas declare package
# arithmetic, so the make recipe generates each on its own and renames its modules.
for pin <- ["hexadecimal-addition", "unsigned-subtraction"] do
  constant = Path.expand("../../../build/packet-defaults/elixir/constant-" <> pin, __DIR__)
  Code.require_file("PacketRuntime.ex", constant)
  Code.require_file("Defaults.ex", constant)
end

Code.require_file("suite.exs", __DIR__)
PacketDefaultTest.run(goldens)
