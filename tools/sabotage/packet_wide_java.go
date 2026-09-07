package main

func init() {
	sabotages["packet-wide-java-pairing"] = []edit{{
		old: `if (low != expectLow) return false;`,
		new: `if (false && low != expectLow) return false; // SABOTAGED`,
	}}
}
