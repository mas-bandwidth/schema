package main

func init() {
	sabotages["packet-defaults-dart-constructor-bytes"] = []edit{{
		old: `		emit("%s%s[%d] = 0x%02x;\n", ind, name, i, b)`,
		new: `		emit("%s// SABOTAGED: %s[%d] = 0x%02x;\n", ind, name, i, b)`,
	}}
}
