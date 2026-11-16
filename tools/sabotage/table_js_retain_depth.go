package main

func init() {
	// A too-shallow retained walk drops a value exactly at the legal boundary.
	sabotages["js-retain-depth-127"] = []edit{{
		old: "const depthLimit = 128;",
		new: "const depthLimit = 127; // SABOTAGED: reject the legal depth-128 boundary",
	}}
}
