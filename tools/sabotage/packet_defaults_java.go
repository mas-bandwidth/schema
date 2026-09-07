package main

func init() {
	sabotages["packet-defaults-java-constructor-bytes"] = []edit{{
		old: `		lines = append(lines, fmt.Sprintf("%s%s[%d] = (byte) 0x%02x;\n", ind, name, i, b))`,
		new: `		lines = append(lines, fmt.Sprintf("%s// SABOTAGED: %s[%d] = (byte) 0x%02x;\n", ind, name, i, b))`,
	}}
}
