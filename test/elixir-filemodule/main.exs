# THE FILE-MODULE STEP-ASIDE GATE (tables-elixir-file-module): tables/arms/Carry.schema
# declares union Carry, so the packet emitter's file-scope module for that file steps
# aside from Armdemo.Carry to Armdemo.CarryFile. This script calls a function of the
# renamed file module and a function of the union's own module, and asserts both resolve
# — two distinct modules.
protocol_id = Armdemo.CarryFile.protocol_id()
true = is_integer(protocol_id)

carry = %Armdemo.Carry{type: 0}
true = is_struct(carry) and carry.type == 0

# the two are DISTINCT: the union's own module must not carry the file-scope
# codecs that stepped aside to CarryFile
try do
  Armdemo.Carry.protocol_id()
  raise "Carry.protocol_id/0 defined: the file-scope codec did not step aside to CarryFile"
rescue
  _ in UndefinedFunctionError -> :ok
end

IO.puts("tables-elixir-file-module: CarryFile.protocol_id()=#{protocol_id} and %Carry{} both resolve, and Carry has no protocol_id/0 (two distinct modules)")
