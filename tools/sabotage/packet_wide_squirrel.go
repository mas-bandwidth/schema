package main

func init() {
	sabotages["packet-utf8-squirrel-read"] = []edit{{
		old: `m.Printf("%s  if (!%s(_tmp, _tmp.len())) return false;\n", ind, m.Helper(validText))`,
		new: `m.Printf("%s  if (false && !%s(_tmp, _tmp.len())) return false; // SABOTAGED\n", ind, m.Helper(validText))`,
	}}
	sabotages["packet-wide-squirrel-pairing"] = []edit{{
		old: `m.Printf("%s    if (high != lo) return false;\n", ind)`,
		new: `m.Printf("%s    if (false && high != lo) return false; // SABOTAGED\n", ind)`,
	}}
	sabotages["packet-defaults-squirrel-constructor-bytes"] = []edit{{
		old: `				m.Printf("%s%s[%d] = 0x%02x;\n", ind, target, i, b)`,
		new: `				m.Printf("%s// SABOTAGED: %s[%d] = 0x%02x;\n", ind, target, i, b)`,
	}}
}
