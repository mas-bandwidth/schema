package main

// The Luau packet gates' controls (make/luau-packet.mk): each removes one
// read or construction rule from the Luau generator, and the gate it controls
// must fail its own marker.
func init() {
	sabotages["packet-write-cfloat-luau"] = []edit{{
		old: `g.Pf("%sif %s ~= %s or %s < %s or %s > %s then return false end\n", indent, val, val, val, FloatLiteral(f.FMin), val, FloatLiteral(f.FMax))`,
		new: `g.Pf("-- SABOTAGED %s %s %s %s %s %s %s\n", indent, val, val, val, FloatLiteral(f.FMin), val, FloatLiteral(f.FMax))`,
	}}
	sabotages["packet-utf8-luau-read"] = []edit{{
		old: `if not utf8valid(%s, n) then return false end`,
		new: `if false and not utf8valid(%s, n) then return false end -- SABOTAGED`,
	}}
	sabotages["luau-ludicrous-fixed-contract"] = []edit{{
		old: `			guardNum(g, indent, val, rawMin, rawMax, "")`,
		new: `			_ = rawMax // SABOTAGED: a fixed(I, F) of 32 bits or fewer writes unchecked`,
	}}
	sabotages["packet-defaults-luau-constructor-bytes"] = []edit{{
		old: `fmt.Sprintf("buffer.writestring(%s, 0, \"%s\")", slot, b.String()),`,
		new: `fmt.Sprintf("-- SABOTAGED: %s takes no default bytes %q", slot, b.String()),`,
	}}
	sabotages["packet-write-bareint-luau-width"] = []edit{{
		old: `			guardNum(g, indent, val, new(big.Int).Neg(half), new(big.Int).Sub(half, big.NewInt(1)), "")`,
		new: `			// SABOTAGED: a bare signed field of 32 bits or fewer writes unchecked`,
	}}
	sabotages["packet-end-luau-read"] = []edit{{
		old: `g.Pf("  return stream:serializeEnd()\n")`,
		new: `g.Pf("  return true -- SABOTAGED: no end check\n")`,
	}}
}
