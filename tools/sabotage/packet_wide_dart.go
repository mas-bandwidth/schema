package main

func init() {
	sabotages["packet-wide-dart-pairing"] = []edit{{
		old: `if (low != expectLow) {`,
		new: `if (low && !low && low != expectLow) { // SABOTAGED`,
	}}
}
