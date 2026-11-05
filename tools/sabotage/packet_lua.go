package main

// The Lua packet gates\x27 controls (make/lua.mk): each removes one read or
// construction rule from the Lua generator, and the gate it controls must fail
// its own marker.
func init() {
	sabotages["packet-utf8-lua-read"] = []edit{{
		old: `m.Printf("%sif not utf8valid(%s) then return false end -- malformed UTF-8 (SPEC §4.7)\n", ind, name)`,
		new: `m.Printf("%sif false and not utf8valid(%s) then return false end -- SABOTAGED\n", ind, name)`,
	}}
	sabotages["packet-wide-lua-pairing"] = []edit{{
		old: `m.Printf("%s  if low ~= expectLow then return false end\n", ind)`,
		new: `m.Printf("%s  if false and low ~= expectLow then return false end -- SABOTAGED\n", ind)`,
	}}
	sabotages["packet-defaults-lua-constructor-bytes"] = []edit{{
		old: "case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:\n\t\treturn ByteString(f.DefBytes)",
		new: "case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:\n\t\treturn `\"\"` // SABOTAGED",
	}}
}
