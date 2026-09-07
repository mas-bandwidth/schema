package main

func init() {
	sabotages["packet-wide-cs-pairing"] = []edit{{
		old: `if (low != expectLow) return false;`,
		new: `if (low && !low && low != expectLow) return false; // SABOTAGED`,
	}}
}
