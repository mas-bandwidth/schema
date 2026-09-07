package main

func init() {
	sabotages["packet-defaults-js-constructor-bytes"] = []edit{{
		old: `		g.pf("%s%s[%d] = 0x%02x;\n", ind, name, i, b)`,
		new: `		g.pf("%s// SABOTAGED: %s[%d] = 0x%02x;\n", ind, name, i, b)`,
	}}
}
