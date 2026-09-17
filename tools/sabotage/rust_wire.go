package main

// Removing canonical spelling must change the differential read report: a
// non-minimal length, index or reference then reads where the oracle calls it
// damage, so the counters disagree rather than the leg crashing. Keep this
// control beside the port without touching another language's anchors.
func init() {
	sabotages["rust-wire-nonminimal"] = []edit{{
		old: "                if i > 0 && b == 0 {\n                    break;\n                }",
		new: "                if false {\n                    break;\n                } // SABOTAGED: accept a nonminimal LEB128",
	}}
}
