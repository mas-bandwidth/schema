package main

func init() {
	sabotages["packet-wide-squirrel-pairing"] = []edit{{
		old: `m.Printf("%s    if (high != lo) return false;\n", ind)`,
		new: `m.Printf("%s    if (false && high != lo) return false; // SABOTAGED\n", ind)`,
	}}
}
