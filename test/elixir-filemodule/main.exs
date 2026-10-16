# THE FILE-MODULE STEP-ASIDE GATE (tables-elixir-file-module): tables/arms/Carry.schema
# declares union Carry, so the packet emitter's file-scope module for that file steps
# aside from Armdemo.Carry to Armdemo.CarryFile. This script calls a function of the
# renamed file module and a function of the union's own module, and asserts both resolve
# — two distinct modules.
protocol_id = Armdemo.CarryFile.protocol_id()
true = is_integer(protocol_id)

carry = %Armdemo.Carry{type: 0}
true = is_struct(carry) and carry.type == 0

IO.puts("tables-elixir-file-module: CarryFile.protocol_id()=#{protocol_id} and %Carry{} both resolve (two distinct modules)")
