package main

// The Luau packet gates' controls (make/luau-packet.mk): each removes one
// read or construction rule from the Luau generator, and the gate it controls
// must fail its own marker.
func init() {
	sabotages["packet-utf8-luau-read"] = []edit{{
		old: `if not utf8valid(%s, n) then return false end`,
		new: `if false and not utf8valid(%s, n) then return false end -- SABOTAGED`,
	}}
	sabotages["packet-wide-luau-pairing"] = []edit{{
		old: `if not high then return false end -- a low surrogate not preceded by a high one`,
		new: `if false then return false end -- SABOTAGED: a lone low surrogate passes`,
	}}
	sabotages["luau-ludicrous-fixed-contract"] = []edit{{
		old: `			guardNum(g, indent, val, rawMin, rawMax, "")`,
		new: `			_ = rawMax // SABOTAGED: a fixed(I, F) of 32 bits or fewer writes unchecked`,
	}}
	sabotages["packet-defaults-luau-constructor-bytes"] = []edit{{
		old: `fmt.Sprintf("buffer.writestring(%s, 0, \"%s\")", slot, b.String()),`,
		new: `fmt.Sprintf("-- SABOTAGED: %s takes no default bytes %q", slot, b.String()),`,
	}}
}
