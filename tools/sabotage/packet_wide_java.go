package main

func init() {
	sabotages["packet-wide-java-pairing"] = []edit{{
		old: `if (low != expectLow) return -1;`,
		new: `if (false && low != expectLow) return -1; // SABOTAGED`,
	}}
}
