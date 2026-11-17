package main

// The Lua packet bare-integer write gate's control (make/lua.mk): it removes
// the declared-width test of a bare intN/uintN below 64 bits from the Lua
// generator, keeping the integer test, so an out-of-width value writes its
// low bits and packet-write-bareint-lua must name the refusal it lost.
func init() {
	sabotages["packet-write-bareint-lua-width"] = []edit{{
		old: `			fmt.Fprintf(b, "%sif math.type(%s) ~= \"integer\" or %s < %s or %s > %s then return false end -- the declared width (SPEC §5.1 row 11)\n",
				indent, val, val, IntLiteral(lo), val, IntLiteral(hi))`,
		new: `			fmt.Fprintf(b, "%sif math.type(%s) ~= \"integer\" then return false end -- SABOTAGED: no width test %s %s\n",
				indent, val, IntLiteral(lo), IntLiteral(hi))`,
	}}
}
