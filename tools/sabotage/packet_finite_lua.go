package main

// The Lua packet target enforces SPEC §4.3's finite-float rule through its
// vendored serialize.lua runtime (internal/codegen/lua/runtime, v2.1.1). Each
// control removes one direction of serialize_float.lua's check at both widths,
// in a COPY of the runtime the generator emitted beside the generated module:
// test/packet-finite/lua/main.lua must fail exactly the bare-float vectors of
// that direction (read.expected, write.expected), while the other direction,
// the quantized float's write refusal and every bit-exact round trip stay
// green.
func init() {
	sabotages["packet-finite-lua-read"] = []edit{{
		old: "      if bits == nil then return false end\n      if not floatBitsAreFinite(bits) then return s:_fail(NON_FINITE_FLOAT) end\n",
		new: "      if bits == nil then return false end\n      -- SABOTAGED: the float32 read takes every pattern\n",
	}, {
		old: "      if bits == nil then return false end\n      if not doubleBitsAreFinite(bits) then return s:_fail(NON_FINITE_FLOAT) end\n",
		new: "      if bits == nil then return false end\n      -- SABOTAGED: the float64 read takes every pattern\n",
	}}
	sabotages["packet-finite-lua-write"] = []edit{{
		old: "      local bits = float32Bits(value)\n      if not floatBitsAreFinite(bits) then return s:_fail(NON_FINITE_FLOAT) end\n",
		new: "      local bits = float32Bits(value)\n      -- SABOTAGED: the float32 write takes every pattern\n",
	}, {
		old: "      local bits = doubleBits(value)\n      if not doubleBitsAreFinite(bits) then return s:_fail(NON_FINITE_FLOAT) end\n",
		new: "      local bits = doubleBits(value)\n      -- SABOTAGED: the float64 write takes every pattern\n",
	}}
}
