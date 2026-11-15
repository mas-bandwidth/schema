package main

// SPEC §5: the Lobster public read's one end check after the top-level
// object, so a trailing byte and set padding read as the value again.
func init() {
	sabotages["packet-end-lobster"] = []edit{{
		old: `	g.Pf("    return stream.serialize_end() // one packet's end: no trailing byte, no set padding (SPEC §5)\n\n")`,
		new: `	g.Pf("    return true // SABOTAGED: the end check removed\n\n")`,
	}}
}
