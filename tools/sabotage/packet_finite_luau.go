package main

// The Luau packet target enforces SPEC §4.3's finite-float rule through its
// vendored serialize.luau runtime (internal/codegen/luau/runtime, v2.1.3). Each
// control removes one direction of serialize_float.luau's check at both
// widths, in a COPY of the runtime the generator emitted beside the generated
// module: test/packet-finite/luau/main.luau must fail exactly the bare-float
// vectors of that direction (read.expected, write.expected), while the other
// direction, the quantized float's write refusal and every bit-exact round
// trip stay green.
func init() {
	sabotages["packet-finite-luau-read"] = []edit{{
		old: "\tlocal bits = s:_readBits(32)\n\tif not floatBitsAreFinite(bits) then\n\t\treturn s:_fail(Stream.SerializeError.NonFiniteFloat)\n\tend\n",
		new: "\tlocal bits = s:_readBits(32)\n\t-- SABOTAGED: the float32 read takes every pattern\n",
	}, {
		old: "\tlocal high = s:_readBits(32)\n\tif not doubleBitsAreFinite(high) then\n\t\treturn s:_fail(Stream.SerializeError.NonFiniteFloat)\n\tend\n",
		new: "\tlocal high = s:_readBits(32)\n\t-- SABOTAGED: the float64 read takes every pattern\n",
	}}
	sabotages["packet-finite-luau-write"] = []edit{{
		old: "\tlocal u = f32bits(value)\n\tif not floatBitsAreFinite(u) then\n\t\treturn s:_misuse(RAISE_NONFINITE)\n\tend\n",
		new: "\tlocal u = f32bits(value)\n\t-- SABOTAGED: the float32 write takes every pattern\n",
	}, {
		old: "\tlocal lo, hi = doubleBits(value)\n\tif not doubleBitsAreFinite(hi) then\n\t\treturn s:_misuse(RAISE_NONFINITE)\n\tend\n",
		new: "\tlocal lo, hi = doubleBits(value)\n\t-- SABOTAGED: the float64 write takes every pattern\n",
	}}
}
