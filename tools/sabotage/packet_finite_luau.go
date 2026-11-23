package main

// The Luau packet target enforces SPEC §4.3's finite-float rule on read
// through its vendored serialize.luau runtime (internal/codegen/luau/runtime,
// v2.1.3) and on write through the generated writer's own guard, which returns
// false before the runtime's trap is reached (LUAU-4). The read control removes
// serialize_float.luau's check at both widths, in a COPY of the runtime the
// generator emitted beside the generated module; the write control removes
// the generator's finite guards at both widths (internal/codegen/luau/write.go,
// built through -overlay), so a non-finite value reaches the runtime and
// raises. test/packet-finite/luau/main.luau must fail exactly the vectors of
// that direction (read.expected, write.expected), while the other direction
// and every bit-exact round trip stay green.
func init() {
	sabotages["packet-finite-luau-read"] = []edit{{
		old: "\tlocal bits = s:_readBits(32)\n\tif not floatBitsAreFinite(bits) then\n\t\treturn s:_fail(Stream.SerializeError.NonFiniteFloat)\n\tend\n",
		new: "\tlocal bits = s:_readBits(32)\n\t-- SABOTAGED: the float32 read takes every pattern\n",
	}, {
		old: "\tlocal high = s:_readBits(32)\n\tif not doubleBitsAreFinite(high) then\n\t\treturn s:_fail(Stream.SerializeError.NonFiniteFloat)\n\tend\n",
		new: "\tlocal high = s:_readBits(32)\n\t-- SABOTAGED: the float64 read takes every pattern\n",
	}}
	sabotages["packet-finite-luau-write"] = []edit{{
		old: `g.Pf("%sif type(%s) ~= \"number\" or not (math.abs(%s) < 3.4028235677973366e38) then return false end -- finite at float32 (SPEC §5.1)\n",
		indent, val, val)`,
		new: `g.Pf("%s-- SABOTAGED: the float32 writer takes every value %s %s\n",
		indent, val, val)`,
	}, {
		old: `g.Pf("%sif type(%s) ~= \"number\" or %s ~= %s or %s == math.huge or %s == -math.huge then return false end -- finite (SPEC §5.1)\n",
			indent, val, val, val, val, val)`,
		new: `g.Pf("%s-- SABOTAGED: the float64 writer takes every value %s %s %s %s %s\n",
			indent, val, val, val, val, val)`,
	}}
}
